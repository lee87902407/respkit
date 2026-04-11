# respkit

`respkit` 是一个面向 Redis 协议兼容服务端场景的 Go 库。当前默认运行时已经切到 **Session + Dispatcher** 主路径：连接建立后，`Session` 负责读写循环、请求限流和响应排队，内置命令通过 dispatcher 执行并回写 RESP 响应。

## 当前状态

- 默认服务入口：`respkit.NewServer(config)`
- 默认命令路径：`PING`、`ECHO`
- 默认运行时：`Session.readLoop` + `Session.writeLoop` + `Dispatcher`
- 仍保留的兼容层：`Conn` / `DetachedConn` 写接口，供 `PubSub` 使用

> 目前 **legacy mux/handler server 主路径已经删除**。如果你之前基于 `NewMux()` / `HandleFunc()` 使用 `respkit`，请改用默认服务端运行时或后续 typed command 扩展路径。

## 安装

```bash
go get github.com/lee87902407/respkit
```

## 快速开始

```go
package main

import (
	"log"

	"github.com/lee87902407/respkit"
)

func main() {
	server := respkit.NewServer(&respkit.Config{
		Addr: ":6380",
	})

	log.Fatal(server.ListenAndServe())
}
```

启动后可以直接验证内置命令：

```bash
# 启动示例服务
go run example/basic-server.go

# 另开一个终端执行
redis-cli -p 6380 PING
redis-cli -p 6380 ECHO hello
```

## 服务生命周期

```go
server := respkit.NewServer(&respkit.Config{
	Addr:                  ":6379",
	Network:               "tcp",
	IdleTimeout:           30 * time.Second,
	DispatcherWorkers:     1,
	QueueSize:             4096,
	MaxInFlightPerSession: 1,
})

err := server.ListenAndServe()
err = server.Shutdown()
```

## Config 重点字段

```go
type Config struct {
	Addr                  string
	Network               string
	ReadBufferSize        int
	WriteBufferSize       int
	DispatcherWorkers     int
	QueueSize             int
	MaxInFlightPerSession int
	IdleTimeout           time.Duration
	MemPool               mempool.BytePool
	Logger                *zap.Logger
	SessionAllocator      SessionAllocator
}
```

这些字段里最关键的是：

- `DispatcherWorkers`：dispatcher worker 数
- `QueueSize`：调度队列长度
- `MaxInFlightPerSession`：每连接最大并发请求数
- `IdleTimeout`：连接空闲读超时

## PubSub 说明

`PubSub` 仍然可用，但它目前依赖 `Conn` 的写接口兼容层。也就是说：

- server 默认请求处理已经走新 runtime
- PubSub 仍通过 `WriteArray / WriteBulk / Flush` 这类接口推送消息

这部分兼容层会在后续阶段继续收敛。

## 测试

```bash
go test ./...
go test -race ./...
```

## 性能说明

`respkit` 仍然保留零拷贝 RESP 解析、缓冲写出和会话级统计能力。更详细的 benchmark 结果见 `BENCHMARK_REPORT.md`。

## 许可证

MIT
