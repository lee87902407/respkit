# NetGo - Extreme Performance Redis Protocol Networking Library

## Executive Summary

This specification defines a high-performance Redis protocol (RESP) networking library designed for extreme throughput and low-latency operations. The library implements the Redis Serialization Protocol (RESP) with zero-copy optimizations, memory pooling, and efficient parsing strategies.

**Key Design Goals:**
- **Extreme Performance**: Target 2-4x Redis throughput on equivalent hardware
- **Zero-Copy Parsing**: Minimize memory allocations and data copying
- **Per-Connection Sessions**: Support connection-scoped session allocation
- **Clean API**: Simple, ergonomic interface for building Redis-compatible servers
- **CGO Integration**: Optional CGO components where C provides measurable performance benefits

**Target Performance:**
- Single-threaded: >2M requests/second for simple GET/SET operations
- Pipeline support: Process 512+ pipelined commands efficiently
- Sub-microsecond average latency for in-memory operations

---

## Architecture Overview

### System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Server Layer                             │
│  ┌───────────────┐  ┌───────────────┐  ┌─────────────────────┐  │
│  │   TCP/TLS    │  │    Unix       │  │   Custom Network   │  │
│  │   Listener   │  │   Socket      │  │     Transport      │  │
│  └───────────────┘  └───────────────┘  └─────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Connection Manager                          │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │  Accept Loop    │  │  Connection     │  │  Session        │  │
│  │  (epoll/kqueue) │  │  Pool          │  │  Allocator      │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                       Protocol Layer                             │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │  RESP Parser    │  │  RESP Writer    │  │  Command        │  │
│  │  (Zero-Copy)    │  │  (Buffered)     │  │  Dispatcher     │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                       Application Layer                          │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │  Handler        │  │  Pub/Sub        │  │  Detached       │  │
│  │  Interface      │  │  Support        │  │  Connections    │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Key Architectural Decisions

1. **Zero-Copy Parsing**: Parser returns views into the read buffer rather than allocating new slices
2. **Memory Pooling**: Reuse buffers and connection structures to reduce GC pressure
3. **Per-Connection Sessions**: Each connection maintains its own session state for isolation
4. **Optional CGO**: Critical parsing paths can use C for SIMD optimizations

---

## Core Components

### 1. Connection Manager

**File**: `internal/conn/manager.go`

```go
// ConnectionManager manages the lifecycle of all client connections
type ConnectionManager struct {
    // epoll on Linux, kqueue on macOS/BSD
    poller       *Poller
    connPool     sync.Pool  // Reusable connection objects
    sessions     sync.Map   // per-connection sessions
    acceptCh     chan net.Conn
    closeCh      chan *conn
    config       *Config
}

// Per-connection state (zero-copy friendly)
type conn struct {
    fd           int
    remoteAddr   net.Addr
    readBuf      []byte      // Fixed-size read buffer
    writeBuf     []byte      // Write aggregation buffer
    session      *Session    // Session-scoped data
    cmds         []Command   // Reusable command slice
    idleDeadline time.Time
}

// Session holds per-connection allocated state
type Session struct {
    ID        uint64
    Data      interface{}     // User-defined session data
    CreatedAt time.Time
    LastSeen  time.Time
    // Metrics
    CommandsProcessed uint64
    BytesRead         uint64
    BytesWritten      uint64
}
```

**Design Notes**:
- Uses `sync.Pool` for connection object reuse
- Session allocation happens once per connection
- Read buffers are sized to `MaxBufferSize` (default 16KB)
- Write buffers use exponential growth starting at 1KB

### 2. RESP Parser

**File**: `internal/resp/parser.go`

```go
// Parser implements zero-copy RESP parsing
type Parser struct {
    buf     []byte      // Read buffer (owned by connection)
    pos     int         // Current read position
    mark    int         // Mark position for slicing
    tmpBuf  []byte      // Temporary buffer for telnet commands
}

// ParseResult is a zero-copy view into the buffer
type ParseResult struct {
    Type     RespType
    Raw      []byte      // View into parser.buf
    Args     [][]byte    // Slices referencing Raw
    Complete bool
}

// ParseNextCommand parses without allocations
func (p *Parser) ParseNextCommand() ParseResult {
    // State machine that returns views into p.buf
    // No allocations for bulk strings or arrays
}
```

**Zero-Copy Strategy**:
- For RESP bulk strings and arrays, return slices pointing directly into the read buffer
- Only allocate for telnet command parsing (which requires transformation)
- Use `unsafe.Slice` where safe and beneficial for performance

**CGO Integration Point**:
- `internal/resp/parser_c.go` - C implementation using SIMD for integer parsing
- Fallback to pure Go implementation when CGO is disabled

### 3. RESP Writer

**File**: `internal/resp/writer.go`

```go
// Writer buffers RESP responses
type Writer struct {
    buf    []byte      // Write buffer
    offset int         // Current write position
}

// Append methods grow the buffer as needed
func (w *Writer) AppendBulk(data []byte) {
    // Direct buffer append, no intermediate allocations
}

func (w *Writer) AppendArray(count int) {
    // Optimized for common array sizes
}

func (w *Writer) AppendError(msg string) {
    // No string allocation for common errors
}

// Flush writes the buffer to the connection
func (w *Writer) Flush(conn *conn) error
```

**Optimizations**:
- Pre-allocate common response sizes
- Use string interning for common error messages
- Batch multiple responses before flush

### 4. Command Dispatcher

**File**: `internal/server/dispatcher.go`

```go
// Dispatcher routes commands to handlers
type Dispatcher struct {
    handlers    map[string]Handler
    defaultHandler Handler
    middleware  []Middleware
}

type Handler interface {
    Handle(ctx *Context) error
}

type Context struct {
    Conn     *Conn
    Cmd      Command
    Session  *Session
}

type Middleware func(Handler) Handler
```

---

## Performance Optimization Strategy

### Memory Optimization

1. **Buffer Reuse**
   - Fixed-size read buffers pooled per connection
   - Write buffers grow on demand but never shrink below minimum
   - Command slices reused across handler calls

2. **Allocation Elimination**
   - Zero-copy parsing for RESP protocol
   - Escape analysis for stack allocation
   - `sync.Pool` for frequently allocated types

3. **String Optimization**
   - Use `[]byte` internally, convert to string only at boundaries
   - Intern common strings ("OK", "PONG", error messages)
   - Avoid string concatenation in hot paths

### CPU Optimization

1. **Syscall Reduction**
   - Batch reads using larger buffers
   - Aggregate writes before flush
   - Use `splice`/`sendfile` for large data transfer

2. **Branch Prediction**
   - Separate hot and cold paths
   - Use jump tables for RESP type dispatch
   - Inline small functions

3. **CGO Acceleration**
   - SIMD integer parsing in C
   - Vectorized CRLF searching
   - Memcmp-based command comparison

### Lock-Free Operations

1. **Connection State**
   - Use atomic operations for state transitions
   - Lock-free read for commonly accessed fields

2. **Session Access**
   - RCU-like pattern for session reads
   - Mutex only for session mutations

---

## Session Management

### Session Lifecycle

```go
// SessionAllocator manages per-connection sessions
type SessionAllocator interface {
    // Allocate creates a new session for the connection
    Allocate(conn *Conn) *Session

    // Release is called when the connection closes
    Release(session *Session)
}

// Default implementation
type DefaultSessionAllocator struct {
    pool sync.Pool
}

func (a *DefaultSessionAllocator) Allocate(conn *Conn) *Session {
    session := a.pool.Get().(*Session)
    session.ID = nextSessionID()
    session.CreatedAt = time.Now()
    session.Conn = conn
    return session
}

func (a *DefaultSessionAllocator) Release(session *Session) {
    // Reset session state
    session.Data = nil
    session.CommandsProcessed = 0
    a.pool.Put(session)
}
```

### Session-Scoped Features

1. **Transaction State**
   - `MULTI`/`EXEC` queue
   - Watched keys
   - Dirty flags

2. **Subscription State**
   - Subscribed channels
   - Pattern subscriptions
   - Message buffers

3. **Client Attributes**
   - Selected database
   - Client name
   - Custom metadata

---

## API Design

### Primary API Surface

**File**: `netgo.go`

```go
package netgo

// Server represents a Redis protocol server
type Server struct {
    config    *Config
    handler   Handler
    sessions  *SessionManager
    listener  net.Listener
}

// Config configures the server
type Config struct {
    // Network configuration
    Addr        string
    Network     string  // "tcp", "tcp4", "tcp6", "unix"

    // Performance tuning
    ReadBufferSize    int     // Default: 16384
    WriteBufferSize   int     // Default: 4096
    MaxPipeline       int     // Default: 1024

    // Connection management
    IdleTimeout       time.Duration
    MaxConnections    int

    // Session configuration
    SessionAllocator  SessionAllocator

    // CGO options
    UseCGOParser      bool    // Use C parser if available
}

// Handler processes a command
type Handler interface {
    Handle(ctx *Context) error
}

// HandlerFunc is an adapter for functions
type HandlerFunc func(ctx *Context) error

func (f HandlerFunc) Handle(ctx *Context) error {
    return f(ctx)
}

// Context provides context for command handling
type Context struct {
    Conn     *Conn
    Command  Command
    Session  *Session
}

// Command represents a parsed command
type Command struct {
    Raw   []byte      // Original raw bytes
    Args  [][]byte    // Parsed arguments
}

// Conn represents a client connection
type Conn interface {
    // Session access
    Session() *Session
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

// DetachedConn is a connection managed by the caller
type DetachedConn interface {
    Conn
    Flush() error
}

// NewServer creates a new server
func NewServer(config *Config, handler Handler) *Server

// ListenAndServe starts the server
func (s *Server) ListenAndServe() error
```

### Extended API

**File**: `mux.go` - Command Multiplexer

```go
// Mux routes commands to registered handlers
type Mux struct {
    handlers map[string]Handler
    notFound Handler
}

func NewMux() *Mux

func (m *Mux) Handle(cmd string, handler Handler)

func (m *Mux) HandleFunc(cmd string, handler func(*Context) error)

func (m *Mux) ServeRESP(ctx *Context) error
```

**File**: `pubsub.go` - Pub/Sub Support

```go
// PubSub manages publish/subscribe
type PubSub struct {
    mu       sync.RWMutex
    channels map[string]*channel
    patterns map[string]*pattern
}

func (ps *PubSub) Subscribe(conn *Conn, channel string) error

func (ps *PubSub) PSubscribe(conn *Conn, pattern string) error

func (ps *PubSub) Publish(channel, message string) (int, error)

func (ps *PubSub) Unsubscribe(conn *Conn, channel string) error
```

---

## CGO Integration

### When to Use CGO

**Recommended CGO Usage:**
1. **Integer Parsing**: SIMD-accelerated atoi for RESP bulk length parsing
2. **CRLF Search**: memchr for finding `\r\n` delimiters
3. **Bulk Compare**: memcmp for command comparison

**Not Recommended for CGO:**
1. Network I/O: Go's stdlib is highly optimized
2. Memory management: Go's GC is better than manual management
3. Protocol encoding: Pure Go is sufficiently fast

### CGO Implementation

**File**: `internal/resp/parser_c.go`

```go
// +build cgo

package resp

/*
#include <stdlib.h>
#include <string.h>
#include <x86intrin.h>  // For SIMD on x86

// Fast integer parsing using SIMD
static long fast_atoi(const char* str, size_t len) {
    // SIMD implementation
}

// Fast CRLF search
static const char* find_crlf(const char* data, size_t len) {
    return memchr(data, '\r', len);
}
*/
import "C"
```

**File**: `internal/resp/parser_go.go` (Fallback)

```go
// +build !cgo

package resp

// Pure Go implementations
func fastAtoi(str []byte) (int, bool) {
    // Go implementation
}

func findCRLF(data []byte) int {
    // Go implementation
}
```

### Build Tags

```bash
# Build with CGO optimization
go build -tags cgo

# Build pure Go
go build -tags !cgo
```

---

## Testing Strategy

### Unit Tests

**File**: `internal/resp/parser_test.go`

```go
func TestParserZeroCopy(t *testing.T) {
    // Verify no allocations during parsing
    data := []byte("*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n")

    var m runtime.MemStats
    runtime.GC()
    runtime.ReadMemStats(&m)
    allocs := m.Alloc

    parser := NewParser(data)
    result := parser.ParseNextCommand()

    runtime.ReadMemStats(&m)
    if m.Alloc != allocs {
        t.Errorf("Parser allocated memory: %d -> %d", allocs, m.Alloc)
    }
}

func TestParserIncomplete(t *testing.T) {
    // Test handling of incomplete commands
    // Test buffer growth
    // Test multiple commands in single buffer
}
```

### Benchmark Tests

**File**: `internal/resp/parser_bench_test.go`

```go
func BenchmarkParserSET(b *testing.B) {
    cmd := []byte("*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n")
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        parser := NewParser(cmd)
        _ = parser.ParseNextCommand()
    }
}

func BenchmarkParserPipeline(b *testing.B) {
    // Build a pipeline of 100 SET commands
    pipeline := buildPipeline(100, "SET", "key", "value")
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        parser := NewParser(pipeline)
        for j := 0; j < 100; j++ {
            result := parser.ParseNextCommand()
            if !result.Complete {
                b.Fatal("Incomplete parse")
            }
        }
    }
}

func BenchmarkCGOvsPureGo(b *testing.B) {
    // Compare CGO vs pure Go performance
}
```

### Integration Tests

**File**: `tests/integration_test.go`

```go
func TestRedisProtocolCompatibility(t *testing.T) {
    // Test against redis-benchmark
    // Test with real redis-cli
    // Test pipeline behavior
    // Test error handling
}

func TestPubSub(t *testing.T) {
    // Test subscribe/publish
    // Test pattern matching
    // Test unsubscribe
}

func TestDetachedConnections(t *testing.T) {
    // Test detach behavior
    // Test pub/sub with detached connections
}
```

### Performance Tests

**File**: `tests/performance_test.go`

```go
func BenchmarkThroughput(b *testing.B) {
    server := startTestServer()
    defer server.Close()

    client := newTestClient(server.Addr())
    defer client.Close()

    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            client.Do("SET", "key", "value")
            client.Do("GET", "key")
        }
    })
}

func BenchmarkPipeline(b *testing.B) {
    // Benchmark pipelined operations
}
```

---

## Implementation Phases

### Phase 1: Core Protocol (Week 1-2)

**Deliverables:**
- [ ] Zero-copy RESP parser
- [ ] RESP writer with buffering
- [ ] Basic connection management
- [ ] TCP listener
- [ ] Command dispatcher

**Files:**
- `internal/resp/parser.go`
- `internal/resp/writer.go`
- `internal/conn/conn.go`
- `internal/server/server.go`
- `netgo.go`

### Phase 2: Session Management (Week 3)

**Deliverables:**
- [ ] Session allocator
- [ ] Per-connection session storage
- [ ] Session lifecycle management
- [ ] Session metrics

**Files:**
- `internal/session/allocator.go`
- `internal/session/session.go`

### Phase 3: Advanced Features (Week 4)

**Deliverables:**
- [ ] Pub/Sub support
- [ ] Detached connections
- [ ] Multiplexer
- [ ] TLS support

**Files:**
- `pubsub.go`
- `mux.go`
- `internal/conn/detach.go`

### Phase 4: CGO Optimization (Week 5)

**Deliverables:**
- [ ] C parser with SIMD
- [ ] Build configuration
- [ ] Performance benchmarks
- [ ] Fallback pure Go implementation

**Files:**
- `internal/resp/parser_c.go`
- `internal/resp/parser_go.go`
- `Makefile`

### Phase 5: Testing & Documentation (Week 6)

**Deliverables:**
- [ ] Comprehensive test suite
- [ ] Benchmark suite
- [ ] API documentation
- [ ] Usage examples
- [ ] Performance report

---

## Performance Targets

### Benchmarks (Target)

| Operation | Target (ops/sec) | Redis Baseline |
|-----------|------------------|----------------|
| PING      | > 5,000,000      | ~1,000,000     |
| SET       | > 3,000,000      | ~1,000,000     |
| GET       | > 4,000,000      | ~1,200,000     |
| Pipeline  | > 10,000,000     | ~2,000,000     |

### Allocation Targets

| Operation | Allocations per op |
|-----------|-------------------|
| Parse     | 0 (for RESP)      |
| Write     | 1 (buffer growth)  |
| Handler   | User-defined      |

---

## Trade-offs

| Decision | Pros | Cons |
|----------|------|------|
| **Zero-Copy Parser** | Eliminates allocations, faster | Requires careful buffer lifecycle management |
| **CGO for SIMD** | 2-3x faster integer parsing | Build complexity, platform-specific |
| **Fixed Read Buffers** | Predictable memory usage | May waste memory for small connections |
| **sync.Pool** | Reduces GC pressure | Slight latency for pool access |
| **Per-Connection Sessions** | Clean isolation, easy cleanup | More memory per connection |

---

## References

- **redcon** (`/Users/yanjie/data/code/open/redcon/`):
  - `redcon.go:39-118` - Conn interface definition
  - `redcon.go:569-584` - Server structure and configuration
  - `redcon.go:762-1010` - Reader implementation with command parsing
  - `redcon.go:592-692` - Writer implementation for RESP responses
  - `resp.go:107-198` - RESP parsing logic

- **Redis Protocol Specification**: https://redis.io/topics/protocol
- **RESP3 Specification**: https://github.com/antirez/RESP3/blob/master/spec.md
