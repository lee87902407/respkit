# 架构说明

当前 `respkit` 的默认运行时已经切换到一条明确的主路径：

```text
net.Conn
  -> internal/conn.Conn
  -> Session.readLoop
  -> Dispatcher
  -> Session.responses
  -> Session.writeLoop
  -> RESP write back
```

这份文档只描述**当前主线代码真实实现**，不再混用历史 mux/handler 路径说明。

---

## 1. 当前分层

```text
Public API (respkit.go)
  - Server
  - Config
  - ScopedRequest / ScopedResponse
  - SessionAllocator
  - PubSub compatibility interfaces (Conn / DetachedConn)

Runtime Layer
  - server.go
  - internal/session/
  - internal/dispatcher/

Protocol Layer
  - internal/protocol/
  - internal/resp/ (仍保留，作为历史协议实现与测试资产)

Transport Layer
  - internal/conn/

Command Layer
  - internal/command/
```

---

## 2. Server 默认主路径

`Server` 是唯一公开入口。

### 启动流程

1. `NewServer(config)` 创建实例
2. `Start()` 打开 listener
3. 创建 `command.Registry` 并注册内置命令（当前是 `PING` / `ECHO`）
4. 创建并启动 `Dispatcher`
5. accept 新连接
6. 为每个连接创建 `Session`
7. 绑定：
   - request reader
   - scope factory
   - dispatcher submitter
   - session writer
8. 调用 `Session.StartLoops(...)`

### 关闭流程

- `Stop()`：优雅停机，停止 accept，通知所有 session 走 graceful stop
- `Close()`：强制关闭，直接关闭连接并等待 runtime 退出

`Server.ActiveSessions()` 通过 session map 统计当前活跃连接。

---

## 3. Session 运行时模型

`Session` 是每个连接的生命周期主对象，当前已经具备这些能力：

- `readLoop`
  - 创建 per-request scope
  - 读取 `RespValue`
  - 执行 inflight gating
  - 提交到 dispatcher

- `writeLoop`
  - 从 `responses` 队列取响应
  - 批量写入 writer
  - 单次 flush
  - flush 成功后释放对应 scope

- 生命周期方法
  - `StopGracefully()`
  - `CloseNow()`
  - `StartLoops(writer)`
  - `HandleResponse(...)`
  - `UseDispatcher(...)`

### inflight gating

`Session.maxInFlight` 控制单连接允许的最大并发请求数。当前默认值来自：

```go
Config.MaxInFlightPerSession
```

当 inflight 达到上限时，`readLoop` 会暂停继续读取，直到已有请求完成回投并降低 inflight。

---

## 4. Dispatcher 路径

`internal/dispatcher.Dispatcher` 当前已经真实接入默认 server 主路径。

它的职责是：

1. 接收 `dispatcher.Request`
2. 根据命令名从 `command.Registry` 查找命令工厂
3. 执行命令
4. 把结果通过 `Result chan protocol.RespValue` 返回给 session

当前 `Session.UseDispatcher(...)` 做了两件关键事：

- 在提交请求前拷贝 `Args`，避免异步执行时引用到可变底层缓冲
- 在拿到执行结果后，把响应回投到 `Session.responses`

---

## 5. 命令层

命令层在 `internal/command/`。

当前默认内置命令通过 `RegisterBuiltins(...)` 注册：

- `PING`
- `ECHO`

这意味着默认 server 不再依赖外部 mux/handler 注册，就能提供最小可工作的 Redis 协议路径。

---

## 6. 连接层

`internal/conn.Conn` 当前职责很窄：

- 持有底层 `net.Conn`
- 负责 RESP 读写调用
- 提供 deadline / close 等连接控制

它不再承担旧文档里那种“server 主逻辑 + handler dispatch”的职责。

---

## 7. 仍然保留的兼容层

当前仓库里还有一块**有意保留**的兼容层：

- `Conn`
- `DetachedConn`
- `PubSub`

保留原因很简单：`PubSub` 现在仍通过 `WriteArray / WriteBulk / WriteInt / Flush` 这类接口推送消息。

所以当前状态是：

- **legacy server 主路径已删除**
- **PubSub 写接口兼容层仍保留**

这也是当前代码和最终“完全纯新架构”之间最后一块明显的历史残留。

---

## 8. 不再适用的旧描述

以下描述已不再适用于当前主线：

- `Mux` / `HandleFunc` 作为 server 主入口
- `Server.Register(factory)` / `CommandFactory` 主分发路径
- `makeHandle()` 单协程 read-parse-handle-flush 主循环
- `serverConn` / `detachedConn` 作为默认请求运行时核心

这些内容已经从主路径删除。

---

## 9. 当前最准确的总结

当前 `respkit` 最准确的描述是：

> 一个默认采用 **Session 双循环 + Dispatcher** 的 Redis 协议兼容服务端库。Server 在连接建立后为每个连接创建 Session，Session 负责读取、限流、回投、批量 flush 和生命周期关闭；命令执行通过 dispatcher 与 command registry 完成；PubSub 仍暂时依赖公共写接口兼容层。
