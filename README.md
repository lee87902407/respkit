# net-resp

A high-performance Go library for building Redis-protocol compatible network servers.

## Features

- **Zero-copy RESP Parsing** - Efficient parsing with minimal allocations
- **Buffered Writing** - Aggregates responses before flush
- **Connection Pooling** - Reuses connections and buffers
- **Session Management** - Per-connection state tracking
- **Command Routing** - Case-insensitive multiplexer with fallback
- **Pub/Sub Support** - Channel and pattern subscription
- **Graceful Shutdown** - Clean connection termination

## Installation

```bash
go get github.com/yanjie/netgo
```

## Quick Start

```go
package main

import (
    "log"
    "github.com/yanjie/netgo"
)

func main() {
    mux := netgo.NewMux()
    
    mux.HandleFunc("ping", func(ctx *netgo.Context) error {
        return ctx.Conn.WriteString("PONG")
    })
    
    mux.HandleFunc("set", func(ctx *netgo.Context) error {
        // Handle SET command
        return ctx.Conn.WriteString("OK")
    })
    
    server := netgo.NewServer(
        &netgo.Config{Addr: ":6380"},
        mux,
    )
    
    log.Fatal(server.ListenAndServe())
}
```

## Testing with redis-cli

```bash
# Start the example server
go run example/basic-server.go

# In another terminal
redis-cli -p 6380 PING
redis-cli -p 6380 SET mykey myvalue
redis-cli -p 6380 GET mykey
```

## API Overview

### Server Lifecycle

```go
server := netgo.NewServer(&netgo.Config{
    Addr:           ":6379",
    Network:        "tcp",
    IdleTimeout:    30 * time.Second,
    MaxConnections: 1000,
}, handler)

// Start (blocks until shutdown)
err := server.ListenAndServe()

// Graceful shutdown
err := server.Shutdown()
```

### Command Handler

```go
type Handler interface {
    Handle(ctx *Context) error
}

type Context struct {
    Conn    Conn        // Connection interface
    Command Command     // Parsed command
    Session *session.Session
}
```

### Connection Methods

```go
type Conn interface {
    // Session access
    Session() *session.Session
    SetData(interface{})
    
    // I/O operations
    WriteString(s string) error
    WriteBulk(b []byte) error
    WriteInt(n int64) error
    WriteArray(n int) error
    WriteNull() error
    WriteError(msg string) error
    WriteAny(v interface{}) error
    
    // Connection control
    Close() error
    RemoteAddr() net.Addr
    Detach() DetachedConn
}
```

## Performance

Zero-copy parsing and buffered writes minimize allocations:

```
$ redis-benchmark -h 127.0.0.1 -p 6380 -t set,get -n 100000 -c 50
SET: 85000 requests/sec
GET: 92000 requests/sec
```

See `BENCHMARK_REPORT.md` for detailed performance data.

## License

MIT
