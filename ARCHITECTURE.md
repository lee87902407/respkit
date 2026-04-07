# Architecture

This document describes the architecture of the net-resp library.

## Overview

net-resp is a layered networking library for building Redis-protocol servers in Go. It handles connection lifecycle, RESP parsing, command routing, and session management.

## Layer Diagram

```
┌─────────────────────────────────────────────────────┐
│ Public API (netgo.go)                              │
│ - Server, Config, Handler, Context                   │
│ - Mux (command routing)                              │
│ - Pub/Sub                                             │
└─────────────────────┬───────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────┐
│ Connection Layer (internal/conn/)                    │
│ - ConnectionManager (accept loop, pool, shutdown)    │
│ - Conn (wrapper with buffers, parser, writer)        │
│ - Pool (sync.Pool with state reset)                   │
└─────────────────────┬───────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────┐
│ Protocol Layer (internal/resp/)                     │
│ - Parser (zero-copy, incremental)                     │
│ - Writer (buffered, sanitizes errors)                 │
└──────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│ Session Layer (internal/session/)                    │
│ - Session (per-connection state)                      │
│ - DefaultSessionAllocator                             │
└──────────────────────────────────────────────────────┘
```

## Component Responsibilities

### Public API (`netgo.go`)

**Server**: Main entry point
- Creates listener, connection manager
- Manages shutdown signaling
- Exposes `Addr()` for bound address

**Config**: Server configuration
- Network type (tcp, unix)
- Buffer sizes, timeouts
- Session allocator

**Handler Interface**: Command processing
```go
type Handler interface {
    Handle(ctx *Context) error
}
```

**Context**: Request context passed to handlers
- `Conn`: Public connection interface
- `Command`: Parsed RESP command
- `Session`: Allocated session state

**Mux**: Command router
- Case-insensitive command matching
- Fallback handler for unknown commands
- Delegates to registered handlers

**PubSub**: Publish/subscribe registry
- Maps connections to channels/patterns
- Writes messages to all subscribers
- Thread-safe with mutex

### Connection Layer (`internal/conn/`)

**ConnectionManager**: Accepts and manages connections
- `Start()`: Accept loop, spawns handler goroutines
- `Stop()`: Sets shutdown flag, closes all connections, waits for goroutines
- `newConn()`: Gets from pool, attaches net.Conn, sets idle deadline
- `handleConnection()`: Parses commands, calls handler, updates session

**Conn**: Connection wrapper
- `readBuf`: Fixed-size read buffer (16KB default)
- `writeBuf`: Aggregated write buffer (starts 1KB, grows to 64KB)
- `parser`: RESP parser instance
- `writer`: RESP writer instance
- `session`: Bound session
- `cmds`: Reusable command slice (for pipelining)
- Thread-safe: mutex protects writes and state changes

**Pool**: sync.Pool for Conn reuse
- `New`: Allocates buffers and initializes Conn
- `Get()`: Returns reset Conn
- `Put()`: Resets Conn state and returns to pool (unless closed/detached)

### Protocol Layer (`internal/resp/`)

**Parser**: Zero-copy RESP parser
- `ParseNextCommand()`: Parses one command from buffer
- `Reset()`: Switches to new buffer, resets position
- `Position()`: Returns consumption offset (for incremental reads)
- Returns slices into input buffer (zero-copy)

**Writer**: Buffered RESP writer
- `Append*()`: Methods to write RESP types
- `AppendAny()`: Generic value encoding (handles maps, slices, structs, errors)
- `Flush()`: Writes buffer to underlying io.Writer
- `Reset()`: Clears buffer for reuse
- Sanitizes `\r\n` and `\n` in simple strings/errors

### Session Layer (`internal/session/`)

**Session**: Per-connection state
- `ID`: Unique identifier
- `Data`: User-defined data
- `CreatedAt`, `LastSeen`: Timestamps
- `CommandsProcessed`, `BytesRead`, `BytesWritten`: Metrics
- `MultiActive`: Transaction flag
- `WatchedReader`, `SubscribedChannels`, `PatternSubs`: Pub/sub state

**SessionAllocator**: Interface for session lifecycle
```go
type SessionAllocator interface {
    Allocate(conn Conn) *session.Session
    Release(session *session.Session)
}
```

**DefaultSessionAllocator**: Increments atomic ID, clears fields on release

## Data Flow

### Accept Flow
```
Listener.Accept()
  → newConn(pool.Get())
  → Attach(netConn)
  → Register in conns map, add to WaitGroup
  → go handleConnection()
```

### Command Flow
```
handleConnection():
  loop:
    → Read from net.Conn into readBuf
    → parser.Reset(readBuf)
    → parser.ParseNextCommand() (until incomplete)
    → For each command:
        → Update session metrics
        → handler.Handle(ctx)
        → conn.Flush()
```

### Shutdown Flow
```
Stop():
  → Set done flag (atomic.Bool)
  → Copy conns map
  → For each conn: Close()
  → wg.Wait() (wait for all handleConnection goroutines)
```

## Zero-Copy Invariants

1. **Parser slices**: `Command.Args` are slices into `readBuf`
   - Valid only until next `Reset()`
   - Caller must copy if needed beyond that

2. **Write buffer**: `Conn.WriteBuf()` grows as needed
   - Written to `net.Conn` in `Flush()`
   - Reset after flush to `writer.Bytes()`

3. **Pool reuse**: Connections returned to pool are fully reset
   - Buffers reused (not reallocated)
   - All fields cleared (fd, netConn, session, commands)

## Thread Safety

- **ConnectionManager**: Mutex protects `conns` map
- **Conn**: Mutex protects `writeBuf`, `closed`, `detached`, `netConn`
- **Mux**: Mutex protects `handlers` map
- **PubSub**: Mutex protects clients and entries maps
- **Pool**: sync.Pool is thread-safe by design
- **Parser/Writer**: Not thread-safe (per-connection)

## Extension Points

1. **Custom Handlers**: Implement `Handler` interface
2. **Session Allocation**: Provide `SessionAllocator` implementation
3. **Connection Hooks**: Wrap `ConnectionManager` or extend `Conn`
4. **Protocol Extensions**: Extend parser/writer for new RESP types
