# 架构说明

本文档说明 `respkit` 的整体架构。该库以分层方式组织，用于构建兼容 Redis 协议的 Go 服务端，核心覆盖连接生命周期、零拷贝 RESP 解析、typed command 分发、每连接双协程收发与会话管理。

## 分层结构

```text
┌──────────────────────────────────────────────────────────┐
│ Public API (respkit.go)                                  │
│ Server · Config · Conn · Command · CommandFactory        │
│ Context · RawCommand · Mux · PubSub                      │
└────────────────────────┬─────────────────────────────────┘
                         │
┌────────────────────────▼─────────────────────────────────┐
│ Connection Layer (internal/conn/)                        │
│ ConnectionManager（accept loop、会话创建、关闭编排）    │
│ Conn（transport：读缓冲、写缓冲、parser、writer、      │
│       send queue）                                       │
│ Pool（sync.Pool + 完整状态重置）                        │
└────────────────────────┬─────────────────────────────────┘
                         │
┌────────────────────────▼─────────────────────────────────┐
│ Protocol Layer (internal/resp/)                          │
│ Parser（零拷贝、增量解析）                              │
│ Writer（缓冲编码、错误内容规整）                        │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│ Session Layer (internal/session/)                        │
│ Session（单连接状态：统计、订阅、事务、用户数据）      │
└──────────────────────────────────────────────────────────┘
```

## 核心设计

### 以 Server 为中心的模型

`Server` 是唯一的公开入口，直接持有命令注册表和生命周期控制：

```text
Server
 ├── config          *Config
 ├── router          *commandRouter    // 命令注册表
 ├── listener        net.Listener
 ├── manager         *ConnectionManager
 └── started         bool
```

- `NewServer(config)` 创建实例
- `Register(name, CommandFactory)` 注册 typed command
- `HandleFunc(name, handler)` 注册原始 handler（兼容层）
- `Start()` 监听并异步启动 accept loop
- `Stop()` 幂等关闭：listener → 连接 → 发送协程 → socket

### Typed Command Pipeline

命令处理采用"原始零拷贝命令 → typed command struct → Execute(ctx)"的流水线：

```text
RESP 字节流
  → Parser.ParseNextCommand()      // 零拷贝解析，产出 Raw/Args
  → CommandFactory(RawCommand)     // 根据命令名查找 factory，解码为 typed command
  → Command.Execute(*Context)      // 执行具体逻辑，通过 ctx.Conn 写响应
  → Conn.Flush()                   // 投递到发送队列
```

**注册方式：**

```go
// 方式一：typed command（推荐）
server.Register("ping", func(raw respkit.RawCommand) (respkit.Command, error) {
    return PingCommand{}, nil
})

// 方式二：原始 handler（兼容层）
server.HandleFunc("get", func(ctx *respkit.Context) error {
    return ctx.Conn.WriteString("OK")
})
```

### 每连接双协程模型

每个连接由两个 goroutine 协同工作：

```text
                    ┌──────────────────┐
  net.Conn.Read ──► │  Receive Goroutine│
                    │  读 → 解析 → 执行 │
                    └────────┬─────────┘
                             │ Flush()
                             ▼
                    ┌──────────────────┐
                    │  Send Queue      │ chan flushRequest (64)
                    └────────┬─────────┘
                             │
                    ┌────────▼─────────┐
                    │  Send Goroutine   │
                    │  唯一 socket writer│
                    └────────┬─────────┘
                             │ net.Conn.Write
                             ▼
                          Client
```

- **Receive goroutine**：`handleConnection()` 循环读网络 → 解析命令 → 调用 handler → `Flush()` 投递响应
- **Send goroutine**：`runSendLoop()` 串行消费 send queue，执行唯一 socket write
- **Flush 语义**：`Write*()` 追加到写缓冲，`Flush()` 拷贝缓冲并投递到 send queue，同步等待发送完成

### Session 作为连接生命周期主对象

Session 在 accept 后立即创建并绑定到 transport：

```text
Accept 成功
  → pool.Get() 获取 Conn transport
  → Attach(netConn)
  → StartSendLoop() 启动发送协程
  → session.NewSession(id) 创建 Session
  → SetSession(sess) 建立 Conn ↔ Session 双向绑定
  → 注册到 ConnectionManager.sessions

连接关闭
  → Conn.Close() 关闭 send queue → 等待发送协程退出 → 关闭 socket
  → Session.SetTransport(nil) 解绑
  → 从 sessions map 删除
  → pool.Put() 回收 transport
```

## 组件职责

### Public API（`respkit.go`）

**Server**
- 直接持有命令注册表，提供 `Register` / `HandleFunc` / `SetNotFound`
- `Start()` 创建 listener 和 ConnectionManager，异步启动服务
- `Stop()` 幂等关闭，顺序：listener → 所有连接 → 发送协程
- `ListenAndServe()` / `Shutdown()` 为阻塞/兼容入口

**Config**
- `Addr`、`Network`：监听地址
- `ReadBufferSize`、`WriteBufferSize`：缓冲区大小
- `MaxPipeline`：单次解析最大命令数
- `IdleTimeout`、`MaxConnections`：连接管理参数

**Conn 接口**
- Session 访问：`Session()`、`SetData()`
- RESP 写入：`WriteString`、`WriteBulk`、`WriteInt`、`WriteArray`、`WriteNull`、`WriteError`、`WriteAny`
- 发送控制：`Flush()` — 将缓冲投递到发送队列
- 连接控制：`Close()`、`RemoteAddr()`、`Detach()`

**Command / CommandFactory**
- `Command` 接口：`Execute(ctx *Context) error`
- `CommandFactory`：`func(raw RawCommand) (Command, error)` — 零拷贝参数校验与解码
- `RawCommand`：持有 `Raw []byte` 和 `Args [][]byte`（零拷贝切片），`Name()` 返回小写命令名

**Context**
- `Conn`：当前连接
- `Command`：解码后的 typed command
- `Raw`：原始零拷贝命令
- `Session`：当前会话

**Mux**
- 命令路由的薄委托层，内部持有 `commandRouter`
- 支持大小写不敏感命令名匹配
- 未命中时走 `notFound` factory

**PubSub**
- 维护 `Conn → pubSubClient` 的订阅关系
- 通过 `Conn` 的 `Write*` + `Flush()` 发送订阅确认和消息推送
- 所有异步推送统一走连接的 send queue，保证单 writer 语义
- 支持精确匹配和 glob 模式匹配

### 连接层（`internal/conn/`）

**ConnectionManager**
- `Start(ln)`：accept 循环，为每个连接启动 `handleConnection` goroutine
- `Stop()`：设置 `done` 标记，关闭所有连接，`WaitGroup` 等待退出
- `handleConnection()`：接收协程主循环 — 读 → 解析 → 分发 → 刷写
- 使用 `atomic.Bool` 控制关闭，`sync.RWMutex` 保护连接集合

**Conn（Transport）**
- 固定 16KB 读缓冲区，可增长写缓冲区（1KB → 64KB）
- `StartSendLoop()`：创建 buffered channel（64）和发送协程
- `Flush()`：拷贝写缓冲 → 投递 `flushRequest` → 同步等待发送完成
- `Close()`：关闭 send queue → 等待发送协程退出 → 解绑 session → 关闭 socket
- `sync.Mutex` 保护 writer、sendQueue 和状态变更

**Pool**
- `sync.Pool` 复用 `Conn` 对象
- `Get()` / `Put()` 完整重置所有字段（含 sendQueue/sendDone/sendClosed），避免脏数据

### 协议层（`internal/resp/`）

**Parser**
- `ParseNextCommand()` 逐条解析 RESP 命令
- 返回对读缓冲区的零拷贝切片（`Raw`、`Args`）
- `Reset(buf)` 支持增量解析

**Writer**
- `Append*()` 写入不同 RESP 类型到内部缓冲
- `Bytes()` 返回当前缓冲内容
- `Reset()` 清空缓冲，复用内存
- 对简单字符串与错误消息中的换行做规整处理

### 会话层（`internal/session/`）

**Session**
- 连接级状态容器：`ID`、`CreatedAt`、`Data`
- 统计字段（通过 accessor 线程安全访问）：`LastSeen`、`CommandsProcessed`、`BytesRead`、`BytesWritten`
- 订阅状态：`SubscribedChannels`、`PatternSubs`
- 事务状态：`MultiActive`、`WatchedKeys`
- Transport 反向引用：`SetTransport()` / `Transport()`

## 数据流

### 完整请求生命周期

```text
1. 网络读取
   netConn.Read(readBuf) → [16KB buffer]

2. 零拷贝解析
   parser.Reset(buf[:n])
   result := parser.ParseNextCommand()
   → result.Raw  = buf[start:end]        // 零拷贝切片
   → result.Args = [buf[x:y], buf[a:b]]  // 零拷贝切片

3. 命令解码
   factory(raw) → typed Command{raw.Args[1], raw.Args[2]}

4. 命令执行
   cmd.Execute(ctx) → ctx.Conn.WriteString("OK")

5. 响应投递
   conn.Flush()
   → payload = copy(writer.Bytes())
   → writer.Reset()
   → sendQueue <- flushRequest{payload, done}

6. 发送协程写出
   payload := <-sendQueue
   netConn.Write(payload)   // 单次系统调用
```

### 关闭流程

```text
Server.Stop()
  → listener.Close()           // 停止 accept
  → manager.Stop()
      → 设置 done 标记
      → 逐个调用 conn.Close()
          → close(sendQueue)   // 通知发送协程退出
          → <-sendDone         // 等待发送协程完成
          → sess.SetTransport(nil)
          → netConn.Close()
      → wg.Wait()              // 等待所有接收协程退出
  → <-serveDone                // 等待 accept loop 退出
```

## 零拷贝约束

1. `RawCommand.Args` 指向读缓冲区切片，只在下一次 `Reset()` 前有效
2. Typed command struct 中的 `[]byte` 字段同样引用读缓冲区，不得在请求处理结束后持有
3. `Flush()` 会拷贝写缓冲内容，因此之后的缓冲区重置不影响已投递的响应
4. 放回连接池前必须完整重置状态（包括 sendQueue/sendDone/sendClosed），确保对象可安全复用

## 并发安全

| 资源 | 保护方式 | 说明 |
|------|---------|------|
| Conn 写缓冲 / 状态 | `sync.Mutex` | Write*/Flush/Close 互斥 |
| Send queue | channel | 发送协程是唯一 consumer |
| ConnectionManager.conns | `sync.RWMutex` | 连接集合增删 |
| ConnectionManager.done | `atomic.Bool` | 关闭标记 |
| commandRouter.factories | `sync.RWMutex` | 命令注册表 |
| PubSub.clients/entries | `sync.RWMutex` | 订阅关系 |
| pubSubClient | `sync.Mutex` | 单客户端写入串行化 |
| Session 字段 | `sync.RWMutex` + accessor | 统计/订阅/事务状态 |
| Parser / Writer | 无锁 | 单 goroutine 上下文，不跨协程共享 |

## 扩展点

1. **自定义命令**：实现 `Command` 接口 + 注册 `CommandFactory`
2. **自定义路由**：替换 `commandRouter` 或设置 `SetNotFound`
3. **PubSub 集成**：通过 `Conn` 接口订阅/发布，自动走 send queue
4. **会话数据**：通过 `Session.SetData()` / `Session.Data()` 存储连接级状态
5. **协议扩展**：在 `internal/resp/` 中演进解析与编码逻辑
