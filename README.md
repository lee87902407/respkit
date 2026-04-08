# respkit

`respkit` 是一个面向 Redis 协议兼容服务端场景的高性能 Go 库，提供零拷贝 RESP 解析、缓冲写出、连接复用、会话管理、命令路由以及 Pub/Sub 能力。

## 功能特性

- 零拷贝 RESP 解析，尽量减少分配与复制
- 缓冲写出，降低系统调用次数
- 连接池与缓冲区复用，减轻 GC 压力
- 会话管理，便于维护每个连接的状态
- 大小写不敏感的命令路由与兜底处理
- 支持频道订阅与模式订阅
- 支持优雅关闭，便于服务平滑退出

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
    mux := respkit.NewMux()

    mux.HandleFunc("ping", func(ctx *respkit.Context) error {
        return ctx.Conn.WriteString("PONG")
    })

    mux.HandleFunc("set", func(ctx *respkit.Context) error {
        return ctx.Conn.WriteString("OK")
    })

    server := respkit.NewServer(
        &respkit.Config{Addr: ":6380"},
        mux,
    )

    log.Fatal(server.ListenAndServe())
}
```

## 使用 `redis-cli` 验证

```bash
# 启动示例服务
go run example/basic-server.go

# 另开一个终端执行
redis-cli -p 6380 PING
redis-cli -p 6380 SET mykey myvalue
redis-cli -p 6380 GET mykey
```

## API 概览

### 服务生命周期

```go
server := respkit.NewServer(&respkit.Config{
    Addr:           ":6379",
    Network:        "tcp",
    IdleTimeout:    30 * time.Second,
    MaxConnections: 1000,
}, handler)

err := server.ListenAndServe()
err = server.Shutdown()
```

### 命令处理器

```go
type Handler interface {
    Handle(ctx *Context) error
}

type Context struct {
    Conn    Conn
    Command Command
    Session *session.Session
}
```

### 连接接口

```go
type Conn interface {
    Session() *session.Session
    SetData(interface{})

    WriteString(s string) error
    WriteBulk(b []byte) error
    WriteInt(n int64) error
    WriteArray(n int) error
    WriteNull() error
    WriteError(msg string) error
    WriteAny(v interface{}) error

    Close() error
    RemoteAddr() net.Addr
    Detach() DetachedConn
}
```

## 性能说明

`respkit` 通过零拷贝解析与缓冲写出减少分配与系统调用，在高并发、小包场景下具备较好的吞吐表现。

```text
$ redis-benchmark -h 127.0.0.1 -p 6380 -t set,get -n 100000 -c 50
SET: 85000 requests/sec
GET: 92000 requests/sec
```

更详细的数据见 `BENCHMARK_REPORT.md`。

## 许可证

MIT
