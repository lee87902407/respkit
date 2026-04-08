# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`respkit` is a high-performance Go library for building Redis-protocol compatible network servers. It provides zero-copy RESP (Redis Serialization Protocol) parsing, connection pooling, session management, and command routing.

## Key Commands

### Testing
```bash
# Run all tests
go test ./...

# Run tests with race detector
go test -race ./...

# Run tests with coverage
go test -cover ./...

# Run specific test
go test -v -run TestConnIOHelpers ./internal/conn/
```

### Building
```bash
# Build the example server
go build -o bin/server example/basic-server.go

# Run the example server (default :6380)
./bin/server
```

### Benchmarking
```bash
# Start the demo server
go run example/basic-server.go

# Run redis-benchmark (in another terminal)
redis-benchmark -h 127.0.0.1 -p 6380 -t set,get -n 10000 -c 50

# Test with different payload sizes
redis-benchmark -h 127.0.0.1 -p 6380 -t set,get -d 1024 -n 10000
redis-benchmark -h 127.0.0.1 -p 6380 -t set,get -d 4096 -n 10000
```

## Architecture

### Layer Structure
- **`respkit.go`**: Public API, `Server` type, `Config`, session allocator interface
- **`internal/conn/`**: Connection management, pooling, listener abstraction
- **`internal/resp/`**: Zero-copy RESP parser and buffered writer
- **`internal/session/`**: Per-connection session state
- **`mux.go`**: Command routing (case-insensitive, with fallback)
- **`pubsub.go`**: Publish/subscribe with pattern matching

### Key Types
- `respkit.Server`: Main server with lifecycle (`ListenAndServe`, `Shutdown`)
- `respkit.Mux`: Command multiplexer routing to handlers
- `respkit.Handler`: Interface for command handlers
- `conn.ConnectionManager`: Accepts connections, manages pool, handles graceful shutdown
- `conn.Conn`: Connection wrapper with buffers, parser, writer, session binding
- `resp.Parser`: Zero-copy RESP parser (resettable, incremental)
- `resp.Writer`: Buffered RESP writer (flushable, sanitizes errors)

### Threading Model
- One goroutine per connection (`handleConnection`)
- Accept loop in `Start()` registers connections before spawning
- `WaitGroup` tracks connection goroutines
- `atomic.Bool` for shutdown flag
- Mutex-protected connection state (write buffer, detached/closed flags)

### Session Lifecycle
1. Connection accepted → `newConn()` gets from pool
2. First command → `connHandler` allocates session via `SessionAllocator`
3. Session bound to connection for lifetime
4. On close → `removeConnection()` deletes from `sessions` map, returns to pool

## Important Patterns

### Zero-Copy RESP
- Parser returns slices into read buffer (`Command.Raw`, `Command.Args`)
- Caller must consume before next read
- Writer buffers and flushes in one system call

### Connection Pooling
- `sync.Pool` with custom `New` allocates read/write buffers
- `Pool.Get()` resets all fields (fd, netConn, session, commands, state flags)
- `Pool.Put()` also resets; closed/detached connections NOT pooled

### Graceful Shutdown
1. `Stop()` sets `done` flag, copies connection map, releases lock
2. For each connection: `Close()`, which closes underlying `net.Conn`
3. `WaitGroup.Wait()` for all handler goroutines to exit
4. Accept loop checks `done` flag before and after `Accept()`

## Testing Constraints

- Tests using `net.Pipe()` must read incrementally (buffer is ~32KB)
- Prefer `internal/conn` package-level tests over integration tests for I/O-heavy code
- Mock `net.Listener` with channel-based implementations for accept loop tests
- `go test -race` must pass before pushing

## Coverage Targets

- Overall: 80%+ (current: 57%)
- Hot spots: `internal/conn/conn.go`, `respkit.go`
- Well covered: `internal/session` (100%), `internal/resp` (84%)
