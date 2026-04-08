# 架构说明

本文档说明 `respkit` 的整体架构。该库以分层方式组织，用于构建兼容 Redis 协议的 Go 服务端，核心覆盖连接生命周期、RESP 解析、命令分发与会话管理。

## 分层结构

```text
┌─────────────────────────────────────────────────────┐
│ Public API (respkit.go)                            │
│ - Server, Config, Handler, Context                 │
│ - Mux（命令路由）                                  │
│ - Pub/Sub                                          │
└─────────────────────┬───────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────┐
│ Connection Layer (internal/conn/)                  │
│ - ConnectionManager（accept、连接池、关闭）        │
│ - Conn（包装连接、缓冲区、解析器、写入器）         │
│ - Pool（带状态重置的 sync.Pool）                   │
└─────────────────────┬───────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────┐
│ Protocol Layer (internal/resp/)                    │
│ - Parser（零拷贝、增量解析）                       │
│ - Writer（缓冲写出、错误内容规整）                 │
└──────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│ Session Layer (internal/session/)                  │
│ - Session（单连接状态）                            │
│ - DefaultSessionAllocator                          │
└──────────────────────────────────────────────────────┘
```

## 组件职责

### Public API（`respkit.go`）

**Server**
- 负责创建监听器与连接管理器
- 负责启动与关闭流程
- 对外暴露 `Addr()` 用于获取绑定地址

**Config**
- 定义网络类型、缓冲区大小、超时与会话分配器等配置

**Handler / Context**
- `Handler` 负责处理命令
- `Context` 封装连接、命令与会话，作为处理器入参

**Mux**
- 负责命令名的大小写不敏感匹配
- 支持未命中命令的兜底处理器

**PubSub**
- 维护连接到频道或模式的订阅关系
- 负责向匹配订阅者广播消息

### 连接层（`internal/conn/`）

**ConnectionManager**
- `Start()` 负责 accept 循环并启动连接 goroutine
- `Stop()` 负责设置关闭标记、关闭所有连接并等待退出
- `handleConnection()` 负责读取请求、解析命令、调用处理器与刷写响应

**Conn**
- 维护固定大小读缓冲区与可增长写缓冲区
- 组合 RESP parser 与 writer
- 绑定连接会话与复用命令切片
- 使用互斥锁保护写操作与连接状态

**Pool**
- 基于 `sync.Pool` 复用连接对象
- `Get()` / `Put()` 都会重置关键状态，避免脏数据泄漏到下一次使用

### 协议层（`internal/resp/`）

**Parser**
- 通过 `ParseNextCommand()` 逐条解析命令
- 返回对输入缓冲区的切片引用，实现零拷贝
- 通过 `Reset()` 支持增量解析流程

**Writer**
- 通过 `Append*()` 写入不同 RESP 类型
- `Flush()` 一次性刷到底层 `io.Writer`
- 对简单字符串与错误消息中的换行做规整处理

### 会话层（`internal/session/`）

**Session**
- 保存连接级状态、统计信息与 Pub/Sub 相关元数据

**SessionAllocator**
- 控制会话分配与释放的生命周期
- 默认实现通过原子递增生成会话 ID

## 数据流

### 建连流程

```text
Listener.Accept()
  -> newConn(pool.Get())
  -> Attach(netConn)
  -> 注册到 conns map，增加 WaitGroup
  -> 启动 handleConnection()
```

### 命令处理流程

```text
handleConnection()
  -> 从 net.Conn 读取到 readBuf
  -> parser.Reset(readBuf)
  -> 反复调用 ParseNextCommand()
  -> 更新会话统计
  -> handler.Handle(ctx)
  -> conn.Flush()
```

### 关闭流程

```text
Stop()
  -> 设置 done 标记
  -> 复制当前连接集合
  -> 逐个关闭连接
  -> WaitGroup 等待所有连接 goroutine 退出
```

## 零拷贝约束

1. `Command.Args` 指向读缓冲区切片，只在下一次 `Reset()` 前有效
2. 写缓冲区在 `Flush()` 后复用，避免重复分配
3. 放回连接池前必须完整重置状态，确保对象可安全复用

## 并发安全

- `ConnectionManager` 使用互斥锁保护连接集合
- `Conn` 使用互斥锁保护写缓冲与连接状态
- `Mux` 与 `PubSub` 都通过互斥锁保护内部映射
- `Parser` / `Writer` 为单连接上下文对象，不应跨 goroutine 共享

## 扩展点

1. 自定义命令处理器：实现 `Handler` 接口
2. 自定义会话分配器：实现 `SessionAllocator`
3. 扩展连接管理：包装 `ConnectionManager` 或扩展 `Conn`
4. 扩展协议能力：在 `internal/resp/` 中演进解析与编码逻辑
