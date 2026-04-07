# NetGo - Implementation Plan

**Version:** 1.1
**Date:** 2026-04-07
**Author:** Architect Agent (Autopilot Phase 1)
**Status:** Critic Reviewed - Approved with Fixes

---

## Executive Summary

This implementation plan breaks down the NetGo high-performance Redis protocol networking library into concrete, executable tasks. The plan is organized into 5 phases matching the specification, with 18 total tasks covering core protocol implementation, session management, advanced features, CGO optimization, and testing.

**Key Design Decisions from Analysis:**
- Zero-copy parsing is achievable by returning slices into the read buffer (as seen in redcon.go:955-973)
- Connection pooling via sync.Pool reduces GC pressure
- Per-connection sessions enable clean state isolation
- CGO is optional with pure Go fallback

---

## Project Structure

```
network_test/
├── go.mod                          # Task 1
├── go.sum
├── netgo.go                        # Task 2 - Main API surface
├── mux.go                          # Task 11 - Command multiplexer
├── pubsub.go                       # Task 12 - Pub/Sub support
├── internal/
│   ├── resp/
│   │   ├── types.go                # Task 3 - RESP type definitions
│   │   ├── parser.go               # Task 4 - Zero-copy parser
│   │   ├── parser_c.go             # Task 15 - CGO SIMD parser (optional)
│   │   ├── parser_go.go            # Task 15 - Pure Go fallback
│   │   ├── writer.go               # Task 5 - RESP writer
│   │   └── parser_test.go          # Task 16 - Parser tests
│   ├── conn/
│   │   ├── conn.go                 # Task 6 - Connection struct
│   │   ├── manager.go              # Task 7 - Connection manager
│   │   ├── pool.go                 # Task 8 - Connection pooling
│   │   ├── listener.go             # Task 9 - TCP/Unix listener
│   │   └── detach.go               # Task 13 - Detached connections
│   ├── session/
│   │   ├── session.go              # Task 10 - Session types
│   │   └── allocator.go            # Task 10 - Session allocator
│   └── server/
│       └── dispatcher.go           # Task 11 - Command dispatcher
├── tests/
│   ├── integration_test.go         # Task 17 - Integration tests
│   └── benchmark_test.go           # Task 18 - Benchmarks
├── example/
│   └── basic-server.go             # Task 14 - Usage example
├── Makefile                        # Task 15 - Build configuration
├── README.md
└── .omc/
    └── plans/
        └── autopilot-impl.md       # This file
```

---

## Task List

### Phase 1: Core Protocol (Foundation)

| ID | Task | Complexity | Model | Dependencies |
|----|------|------------|-------|--------------|
| 1 | Initialize Go module | Simple | Haiku | None |
| 2 | Define main API surface | Standard | Sonnet | 1 |
| 3 | Define RESP types | Simple | Haiku | 1 |
| 4 | Implement zero-copy RESP parser | Complex | Opus | 3 |
| 5 | Implement RESP writer | Standard | Sonnet | 3 |
| 6 | Implement Connection struct | Standard | Sonnet | 2, 3 |
| 7 | Implement ConnectionManager | Complex | Opus | 6 |
| 8 | Implement connection pooling | Standard | Sonnet | 6 |
| 9 | Implement TCP/Unix listener | Standard | Sonnet | 7 |

### Phase 2: Session Management

| ID | Task | Complexity | Model | Dependencies |
|----|------|------------|-------|--------------|
| 10 | Implement session system | Standard | Sonnet | 6, 8 |

### Phase 3: Advanced Features

| ID | Task | Complexity | Model | Dependencies |
|----|------|------------|-------|--------------|
| 11 | Implement command dispatcher/mux | Standard | Sonnet | 2, 4 |
| 12 | Implement Pub/Sub | Complex | Opus | 11 |
| 13 | Implement detached connections | Standard | Sonnet | 6, 7 |
| 14 | Create usage example | Simple | Haiku | 9, 11 |

### Phase 4: CGO Optimization

| ID | Task | Complexity | Model | Dependencies |
|----|------|------------|-------|--------------|
| 15 | Implement CGO SIMD parser | Complex | Opus | 4 |

### Phase 5: Testing & Documentation

| ID | Task | Complexity | Model | Dependencies |
|----|------|------------|-------|--------------|
| 16 | Implement parser tests | Standard | Sonnet | 4, 5 |
| 17 | Implement integration tests | Standard | Sonnet | 1-14 |
| 18 | Implement performance benchmarks | Standard | Sonnet | 16, 17 |

---

## Detailed Task Specifications

### Task 1: Initialize Go Module
**ID:** T001
**File:** go.mod
**Complexity:** Simple
**Model:** Haiku

**Description:**
Initialize a new Go module with appropriate dependencies.

**Requirements:**
- Module name: github.com/yanjie/netgo (placeholder, adjust as needed)
- Go version: 1.21+
- No external dependencies for core (pure Go standard library)

**Command:**
\`\`\`bash
go mod init github.com/yanjie/netgo
\`\`\`

**Verification:**
- go.mod exists with correct module path
- go build completes with no errors (empty main)

---

### Task 2: Define Main API Surface
**ID:** T002
**File:** netgo.go
**Complexity:** Standard
**Model:** Sonnet
**Dependencies:** T001

**Description:**
Create the primary public API surface following the spec at spec.md:319-414. Define core types: Server, Config, Handler, Context, Command, Conn.

**Key Types to Define:**
\`\`\`go
package netgo

type Server struct {
    config    *Config
    handler   Handler
    sessions  *SessionManager
    listener  net.Listener
    // ... internal fields
}

type Config struct {
    Addr           string
    Network        string  // "tcp", "tcp4", "tcp6", "unix"
    ReadBufferSize int     // Default: 16384
    WriteBufferSize int    // Default: 4096
    MaxPipeline    int     // Default: 1024
    IdleTimeout    time.Duration
    MaxConnections int
    SessionAllocator SessionAllocator
    UseCGOParser   bool
}

type Handler interface {
    Handle(ctx *Context) error
}

type HandlerFunc func(ctx *Context) error

type Context struct {
    Conn    *Conn
    Command Command
    Session *Session
}

type Command struct {
    Raw  []byte
    Args [][]byte
}

type Conn interface {
    Session() *Session
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

type DetachedConn interface {
    Conn
    Flush() error
}

func NewServer(config *Config, handler Handler) *Server
func (s *Server) ListenAndServe() error
\`\`\`

**Reference:**
- redcon.go:39-118 - Conn interface definition
- redcon.go:569-584 - Server structure

**Verification:**
- All types compile without errors
- Interface methods match specification
- Default Config values are sensible

---

### Task 3: Define RESP Types
**ID:** T003
**File:** internal/resp/types.go
**Complexity:** Simple
**Model:** Haiku
**Dependencies:** T001

**Description:**
Define core RESP protocol types and constants.

**Types to Define:**
\`\`\`go
package resp

type RespType byte

const (
    SimpleString RespType = '+'
    Error        RespType = '-'
    Integer      RespType = ':'
    BulkString   RespType = '$'
    Array        RespType = '*'
)

type ParseResult struct {
    Type     RespType
    Raw      []byte      // View into parser buffer (zero-copy)
    Args     [][]byte    // Slices referencing Raw
    Complete bool
}
\`\`\`

**Reference:**
- resp.go:11-28 - Type definitions

**Verification:**
- Types compile
- Constants match RESP specification

---

### Task 4: Implement Zero-Copy RESP Parser
**ID:** T004
**File:** internal/resp/parser.go
**Complexity:** Complex
**Model:** Opus
**Dependencies:** T003

**Description:**
Implement a zero-copy RESP parser that returns slices into the read buffer rather than allocating new memory. This is the most critical performance component.

**Core Implementation:**
\`\`\`go
package resp

type Parser struct {
    buf     []byte      // Read buffer (owned by connection, not copied)
    pos     int         // Current read position
    mark    int         // Mark position for slicing
    tmpBuf  []byte      // Temporary buffer for telnet commands
}

func NewParser(buf []byte) *Parser {
    return &Parser{buf: buf}
}

func (p *Parser) ParseNextCommand() ParseResult {
    // State machine returning views into p.buf
    // NO allocations for bulk strings or arrays
    // Return slices pointing directly into p.buf
}

// Helper functions (allocate minimally)
func parseInt(b []byte) (int, bool)  // Inline-able
func findCRLF(b []byte) int          // Inline-able
\`\`\`

**Zero-Copy Strategy (Critical):**
1. For RESP bulk strings: Return buf[start:end] slice (no copy)
2. For RESP arrays: Return slice of slices (all referencing original buffer)
3. Only allocate for telnet command transformation
4. Parser must NOT modify the input buffer
5. Caller owns buffer lifecycle - buffer MUST live for duration of command processing
6. **Important**: Zero-copy only applies when parsing from pre-read byte slice.
   When reading from bufio.Reader, a copy is made (see redcon.go:955-962 pattern).
   Implementation must explicitly document which code paths allocate.

**Reference Implementation Study:**
- redcon.go:802-1010 - Reader implementation showing zero-copy pattern
- redcon.go:955-973 - Zero-copy slice assignment: cmd.Args[h/2] = cmd.Raw[marks[h]:marks[h+1]]

**Verification:**
- Unit test with runtime.MemStats proving zero allocations for RESP parsing
- Test with incomplete commands (buffer waiting for more data)
- Test with multiple commands in single buffer
- Test with all RESP types (SimpleString, Error, Integer, BulkString, Array)

---

### Task 5: Implement RESP Writer
**ID:** T005
**File:** internal/resp/writer.go
**Complexity:** Standard
**Model:** Sonnet
**Dependencies:** T003

**Description:**
Implement buffered RESP response writer with optimizations for common cases.

**Core Implementation:**
\`\`\`go
package resp

type Writer struct {
    buf    []byte      // Write buffer
    offset int         // Current write position
}

func NewWriter(size int) *Writer

// Append methods - grow buffer as needed
func (w *Writer) AppendBulk(data []byte)
func (w *Writer) AppendArray(count int)
func (w *Writer) AppendError(msg string)
func (w *Writer) AppendString(msg string)
func (w *Writer) AppendInt(n int64)
func (w *Writer) AppendNull()
func (w *Writer) AppendAny(v interface{}) []byte

// Flush to underlying connection
func (w *Writer) Flush(conn io.Writer) error

// Buffer access
func (w *Writer) Bytes() []byte
func (w *Writer) Reset()
\`\`\`

**Optimizations:**
1. Pre-allocate common response sizes in buffer pool
2. String interning for common error messages ("ERR", "WRONGTYPE", etc.)
3. Inline small integer appends (0-9 single digit)
4. Batch multiple responses before flush

**Reference:**
- redcon.go:592-692 - Writer implementation
- resp.go:424-522 - Append functions

**Verification:**
- All RESP types write correctly
- Buffer growth works correctly
- Flush writes complete data

---

### Task 6: Implement Connection Struct
**ID:** T006
**File:** internal/conn/conn.go
**Complexity:** Standard
**Model:** Sonnet
**Dependencies:** T002, T003

**Description:**
Implement the per-connection state structure with zero-copy friendly buffers.

**Core Implementation:**
\`\`\`go
package conn

import (
    "net"
    "sync"
    "time"
    "github.com/yanjie/netgo/internal/resp"
    "github.com/yanjie/netgo/internal/session"
)

type Conn struct {
    fd           int
    remoteAddr   net.Addr
    readBuf      []byte      // Fixed-size read buffer (default 16KB)
    writeBuf     []byte      // Write aggregation buffer (starts 1KB, grows to max 64KB, 2x growth factor)
    parser       *resp.Parser
    writer       *resp.Writer
    session      *session.Session
    cmds         []Command   // Reusable command slice
    idleDeadline time.Time
    detached     bool
    closed       bool
    mu           sync.Mutex
}

// Implement netgo.Conn interface
func (c *Conn) Session() *session.Session
func (c *Conn) SetData(data interface{})
func (c *Conn) WriteString(s string) error
func (c *Conn) WriteBulk(b []byte) error
func (c *Conn) WriteInt(n int64) error
func (c *Conn) WriteArray(n int) error
func (c *Conn) WriteNull() error
func (c *Conn) WriteError(msg string) error
func (c *Conn) WriteAny(v interface{}) error
func (c *Conn) Close() error
func (c *Conn) RemoteAddr() net.Addr
func (c *Conn) Detach() netgo.DetachedConn
\`\`\`

**Reference:**
- redcon.go:456-501 - Connection struct
- spec.md:89-110 - Conn design notes

**Verification:**
- All interface methods implemented
- Thread-safe for concurrent writes
- Buffer lifecycle correct

---

### Task 7: Implement Connection Manager
**ID:** T007
**File:** internal/conn/manager.go
**Complexity:** Complex
**Model:** Opus
**Dependencies:** T006

**Description:**
Implement the connection lifecycle manager with accept loop, connection tracking, and graceful shutdown.

**Core Implementation:**
\`\`\`go
package conn

import (
    "net"
    "sync"
    "github.com/yanjie/netgo/internal/session"
)

type ConnectionManager struct {
    connPool     *Pool       // From T008
    sessions     sync.Map    // conn ID -> *session.Session
    acceptCh     chan net.Conn
    closeCh      chan *Conn
    config       *netgo.Config
    handler      netgo.Handler
    conns        map[*Conn]bool
    mu           sync.RWMutex
    done         bool
}

func NewManager(config *netgo.Config, handler netgo.Handler) *ConnectionManager
func (m *ConnectionManager) Start(ln net.Listener) error
func (m *ConnectionManager) Stop() error
func (m *ConnectionManager) handleConnection(c *Conn)
func (m *ConnectionManager) removeConnection(c *Conn)
\`\`\`

**Key Design Decisions:**
1. One goroutine per connection (simpler than event loop, proven in redcon)
2. Connection tracking for graceful shutdown
3. Session lifecycle tied to connection lifecycle
4. Accept errors logged but don't stop server

**Reference:**
- redcon.go:341-390 - Serve loop
- redcon.go:393-453 - Connection handler
- spec.md:78-111 - ConnectionManager spec

**Verification:**
- Accepts connections continuously
- Handler called for each command
- Connections cleaned up on close
- Graceful shutdown works

---

### Task 8: Implement Connection Pooling
**ID:** T008
**File:** internal/conn/pool.go
**Complexity:** Standard
**Model:** Sonnet
**Dependencies:** T006

**Description:**
Implement sync.Pool based connection object pooling to reduce GC pressure.

**Core Implementation:**
\`\`\`go
package conn

import "sync"

var globalPool = &Pool{
    p: sync.Pool{
        New: func() interface{} {
            return &Conn{
                readBuf:  make([]byte, 16384),
                writeBuf: make([]byte, 4096),
                cmds:     make([]Command, 0, 16),
            }
        },
    },
}

type Pool struct {
    p sync.Pool
}

func (p *Pool) Get() *Conn
func (p *Pool) Put(c *Conn)
\`\`\`

**Reference:**
- spec.md:113-118 - Pool design notes

**Verification:**
- Pool returns initialized connections
- Put resets connection state
- No memory leaks from pool

---

### Task 9: Implement TCP/Unix Listener
**ID:** T009
**File:** internal/conn/listener.go
**Complexity:** Standard
**Model:** Sonnet
**Dependencies:** T007

**Description:**
Implement network listener supporting TCP and Unix domain sockets.

**Core Implementation:**
\`\`\`go
package conn

import (
    "net"
    "crypto/tls"
)

type Listener interface {
    Accept() (net.Conn, error)
    Close() error
    Addr() net.Addr
}

func Listen(network, addr string) (net.Listener, error)
func ListenTLS(network, addr string, config *tls.Config) (net.Listener, error)
\`\`\`

**Integration with Server:**
\`\`\`go
// In netgo.go
func (s *Server) ListenAndServe() error {
    ln, err := conn.Listen(s.config.Network, s.config.Addr)
    if err != nil {
        return err
    }
    defer ln.Close()
    return s.manager.Start(ln)
}
\`\`\`

**Reference:**
- redcon.go:295-310 - ListenAndServe pattern

**Verification:**
- TCP listener works
- Unix socket listener works
- TLS listener works (Phase 3)

---

### Task 10: Implement Session System
**ID:** T010
**Files:** internal/session/session.go, internal/session/allocator.go
**Complexity:** Standard
**Model:** Sonnet
**Dependencies:** T006, T008

**Description:**
Implement per-connection session allocation and lifecycle management.

**Core Implementation:**
\`\`\`go
// session.go
package session

import "time"

type Session struct {
    ID                uint64
    Data              interface{}     // User-defined session data
    Conn              interface{}     // Back-reference to connection
    CreatedAt         time.Time
    LastSeen          time.Time
    CommandsProcessed uint64
    BytesRead         uint64
    BytesWritten      uint64
    // Transaction state
    MultiActive       bool
    WatchedKeys       map[string]bool
    // Subscription state
    SubscribedChannels map[string]bool
    PatternSubs        map[string]bool
}

func (s *Session) Reset()
func (s *Session) UpdateActivity()

// allocator.go
package session

import "sync"

type SessionAllocator interface {
    Allocate(conn interface{}) *Session
    Release(session *Session)
}

type DefaultSessionAllocator struct {
    pool sync.Pool
    nextID uint64  // Uses atomic.AddUint64 for lock-free ID generation
    mu     sync.Mutex  // Only used for pool operations
}

func NewDefaultSessionAllocator() *DefaultSessionAllocator
func (a *DefaultSessionAllocator) Allocate(conn interface{}) *Session
func (a *DefaultSessionAllocator) Release(session *Session)

// Session ID overflow behavior:
// IDs are generated using atomic.AddUint64(&nextID, 1)
// On overflow (wraps to 0), continues generating - uniqueness is probabilistic
// For practical purposes, overflow at 2^64 connections is not a concern
\`\`\`

**Reference:**
- spec.md:264-316 - Session specification

**Verification:**
- Sessions allocated per connection
- Sessions released on connection close
- Metrics tracked correctly
- No memory leaks

---

### Task 11: Implement Command Dispatcher/Mux
**ID:** T011
**Files:** internal/server/dispatcher.go, mux.go
**Complexity:** Standard
**Model:** Sonnet
**Dependencies:** T002, T004

**Description:**
Implement command routing to registered handlers with middleware support.

**Core Implementation:**
\`\`\`go
// internal/server/dispatcher.go
package server

import "github.com/yanjie/netgo"

type Dispatcher struct {
    handlers       map[string]netgo.Handler
    defaultHandler netgo.Handler
    middleware     []Middleware
}

type Middleware func(netgo.Handler) netgo.Handler

func NewDispatcher() *Dispatcher
func (d *Dispatcher) Register(cmd string, handler netgo.Handler)
func (d *Dispatcher) RegisterFunc(cmd string, handler func(*netgo.Context) error)
func (d *Dispatcher) SetDefault(handler netgo.Handler)
func (d *Dispatcher) Use(mw Middleware)
func (d *Dispatcher) Dispatch(ctx *netgo.Context) error

// Middleware chain execution:
// Middleware wraps the handler in layers, executed in reverse order of registration.
// Example: Use(mw1).Use(mw2) -> mw2(mw1(handler))
// Execution flow: request -> mw2 -> mw1 -> handler -> mw1 -> mw2 -> response

// mux.go (public API)
package netgo

type Mux struct {
    d *server.Dispatcher
}

func NewMux() *Mux
func (m *Mux) Handle(cmd string, handler Handler)
func (m *Mux) HandleFunc(cmd string, handler func(*Context) error)
func (m *Mux) ServeRESP(ctx *Context) error
\`\`\`

**Reference:**
- redcon.go:1074-1119 - ServeMux implementation
- spec.md:189-213 - Dispatcher spec

**Verification:**
- Commands route to correct handlers
- Case-insensitive command matching
- Unknown commands return error
- Middleware executes in order

---

### Task 12: Implement Pub/Sub
**ID:** T012
**File:** pubsub.go
**Complexity:** Complex
**Model:** Opus
**Dependencies:** T011, T013

**Description:**
Implement Redis-compatible publish/subscribe with detached connections.

**Core Implementation:**
\`\`\`go
package netgo

import (
    "sync"
    "github.com/tidwall/btree"  // Or alternative without external dep
)

type PubSub struct {
    mu       sync.RWMutex
    channels map[string]*channel
    patterns map[string]*pattern
}

type channel struct {
    name    string
    conns   map[*Conn]struct{}
}

func NewPubSub() *PubSub
func (ps *PubSub) Subscribe(conn *Conn, channel string) error
func (ps *PubSub) PSubscribe(conn *Conn, pattern string) error
func (ps *PubSub) Publish(channel, message string) (int, error)
func (ps *PubSub) Unsubscribe(conn *Conn, channel string) error
func (ps *PubSub) PUnsubscribe(conn *Conn, pattern string) error
\`\`\`

**Design Notes:**
- Requires detached connections to handle blocking PubSub
- Each PubSub subscriber runs in its own goroutine
- Pattern matching uses glob patterns
- Uses map-based channel lookup (optimization to btree if benchmarks show benefit with 10K+ channels)

**Reference:**
- redcon.go:1121-1454 - PubSub implementation
- spec.md:436-453 - PubSub API spec

**Verification:**
- Subscribe receives published messages
- PSubscribe receives pattern-matched messages
- Unsubscribe stops messages
- Detached connection works correctly

---

### Task 13: Implement Detached Connections
**ID:** T013
**File:** internal/conn/detach.go
**Complexity:** Standard
**Model:** Sonnet
**Dependencies:** T006, T007

**Description:**
Implement detached connection support for operations like PubSub.

**Core Implementation:**
\`\`\`go
package conn

import "github.com/yanjie/netgo"

type DetachedConn struct {
    conn   *Conn
    cmds   []netgo.Command
    closed bool
}

func (dc *DetachedConn) Flush() error
func (dc *DetachedConn) ReadCommand() (netgo.Command, error)
func (dc *DetachedConn) Close() error

// Implement netgo.DetachedConn interface methods
func (dc *DetachedConn) Session() *session.Session
func (dc *DetachedConn) SetData(data interface{})
func (dc *DetachedConn) WriteString(s string) error
// ... other netgo.Conn methods
\`\`\`

**Reference:**
- redcon.go:511-559 - DetachedConn implementation
- spec.md:403-407 - DetachedConn interface

**Verification:**
- Detach removes connection from server loop
- Flush writes buffered data
- Close cleans up resources
- PubSub works with detached connections

---

### Task 14: Create Usage Example
**ID:** T014
**File:** example/basic-server.go
**Complexity:** Simple
**Model:** Haiku
**Dependencies:** T009, T011

**Description:**
Create a simple example server demonstrating the API.

**Implementation:**
\`\`\`go
package main

import (
    "log"
    "github.com/yanjie/netgo"
)

func main() {
    mux := netgo.NewMux()

    // Register basic commands
    mux.HandleFunc("ping", func(ctx *netgo.Context) error {
        ctx.Conn.WriteString("PONG")
        return nil
    })

    mux.HandleFunc("set", func(ctx *netgo.Context) error {
        if len(ctx.Command.Args) != 3 {
            ctx.Conn.WriteError("ERR wrong number of arguments")
            return nil
        }
        // Store value...
        ctx.Conn.WriteString("OK")
        return nil
    })

    mux.HandleFunc("get", func(ctx *netgo.Context) error {
        if len(ctx.Command.Args) != 2 {
            ctx.Conn.WriteError("ERR wrong number of arguments")
            return nil
        }
        // Retrieve value...
        ctx.Conn.WriteNull()  // or WriteBulk(value)
        return nil
    })

    server := netgo.NewServer(&netgo.Config{
        Addr: ":6380",
    }, mux)

    log.Println("Server listening on :6380")
    if err := server.ListenAndServe(); err != nil {
        log.Fatal(err)
    }
}
\`\`\`

**Verification:**
- Example compiles
- Can connect with redis-cli
- PING works
- SET/GET work

---

### Task 15: Implement CGO SIMD Parser
**ID:** T015
**Files:** internal/resp/parser_c.go, internal/resp/parser_go.go, Makefile
**Complexity:** Complex
**Model:** Opus
**Dependencies:** T004

**Description:**
Implement optional CGO-accelerated parser with SIMD optimizations.

**CGO Implementation (parser_c.go):**
\`\`\`go
// +build cgo

package resp

/*
#include <stdlib.h>
#include <string.h>
#ifdef __x86_64__
#include <x86intrin.h>
#endif

// Fast integer parsing using SIMD
static long fast_atoi(const char* str, size_t len) {
    // SIMD implementation for x86_64
    // Fallback to regular implementation for other arch
}

// Fast CRLF search using memchr
static const char* find_crlf(const char* data, size_t len) {
    return memchr(data, '\\r', len);
}
*/
import "C"

func fastAtoi(str []byte) (int, bool) {
    // Call C.fast_atoi
}

func findCRLF(data []byte) int {
    // Call C.find_crlf
}
\`\`\`

**Pure Go Fallback (parser_go.go):**
\`\`\`go
// +build !cgo

package resp

func fastAtoi(str []byte) (int, bool) {
    // Pure Go implementation
}

func findCRLF(data []byte) int {
    // Pure Go implementation
}
\`\`\`

**Build Configuration (Makefile):**
\`\`\`makefile
.PHONY: all clean test benchmark

# Build with CGO
all:
	go build -tags cgo ./...

# Build pure Go
pure:
	go build -tags=!cgo ./...

# Run tests
test:
	go test -tags cgo ./...

# Run benchmarks
benchmark:
	go test -bench=. -benchmem -tags cgo ./...

# Compare CGO vs pure Go
compare:
	@echo "CGO version:"; go test -bench=. -tags cgo ./... | grep Benchmark
	@echo "Pure Go version:"; go test -bench=. -tags=!cgo ./... | grep Benchmark

clean:
	go clean ./...
\`\`\`

**Reference:**
- spec.md:457-524 - CGO integration spec

**Verification:**
- CGO version builds
- Pure Go version builds
- Benchmark shows CGO improvement
- Fallback works on non-x86

---

### Task 16: Implement Parser Tests
**ID:** T016
**File:** internal/resp/parser_test.go
**Complexity:** Standard
**Model:** Sonnet
**Dependencies:** T004, T005

**Description:**
Comprehensive tests for parser including zero-copy verification.

**Test Cases:**
\`\`\`go
package resp

import (
    "runtime"
    "testing"
)

func TestParserZeroCopy(t *testing.T) {
    data := []byte("*3\\r\\n$3\\r\\nSET\\r\\n$3\\r\\nkey\\r\\n$5\\r\\nvalue\\r\\n")

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

    if !result.Complete {
        t.Error("Expected complete command")
    }
    if len(result.Args) != 3 {
        t.Errorf("Expected 3 args, got %d", len(result.Args))
    }
}

func TestParserIncomplete(t *testing.T) {
    // Test handling of incomplete commands
}

func TestParserMultipleCommands(t *testing.T) {
    // Test parsing multiple commands from single buffer
}

func TestParserAllRESPTypes(t *testing.T) {
    // Test SimpleString, Error, Integer, BulkString, Array
}

func TestParserInvalid(t *testing.T) {
    // Test invalid RESP returns error
}
\`\`\`

**Verification:**
- All tests pass
- Zero-copy test proves no allocations
- Coverage >80%

---

### Task 17: Implement Integration Tests
**ID:** T017
**File:** tests/integration_test.go
**Complexity:** Standard
**Model:** Sonnet
**Dependencies:** T001-T014

**Description:**
End-to-end tests against a running server.

**Test Cases:**
\`\`\`go
package tests

import (
    "net"
    "testing"
    "github.com/yanjie/netgo"
)

func startTestServer(t *testing.T) *netgo.Server {
    // Start server on random port
}

func TestRedisProtocolCompatibility(t *testing.T) {
    // Test with RESP commands
}

func TestPipelining(t *testing.T) {
    // Test multiple commands in single write
}

func TestConcurrentConnections(t *testing.T) {
    // Test multiple clients
}

func TestPubSub(t *testing.T) {
    // Test subscribe/publish flow
}

func TestDetachedConnections(t *testing.T) {
    // Test detach behavior
}
\`\`\`

**Verification:**
- All integration tests pass
- Server handles redis-cli
- No connection leaks

---

### Task 18: Implement Performance Benchmarks
**ID:** T018
**File:** tests/benchmark_test.go
**Complexity:** Standard
**Model:** Sonnet
**Dependencies:** T016, T017

**Description:**
Performance benchmarks comparing against targets in spec.

**Benchmarks:**
\`\`\`go
package tests

import (
    "testing"
    "github.com/yanjie/netgo"
)

func BenchmarkParserSET(b *testing.B) {
    cmd := []byte("*3\\r\\n$3\\r\\nSET\\r\\n$3\\r\\nkey\\r\\n$5\\r\\nvalue\\r\\n")
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        parser := NewParser(cmd)
        _ = parser.ParseNextCommand()
    }
}

func BenchmarkParserPipeline(b *testing.B) {
    // Build pipeline of 100 SET commands
}

func BenchmarkThroughput(b *testing.B) {
    // Full stack benchmark
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
\`\`\`

**Targets (from spec):**
| Operation | Target (ops/sec) |
|-----------|------------------|
| PING      | > 5,000,000      |
| SET       | > 3,000,000      |
| GET       | > 4,000,000      |
| Pipeline  | > 10,000,000     |

**Verification:**
- Benchmarks run successfully
- Targets met or documented why not
- CGO vs pure Go comparison

---

## Dependency Graph

\`\`\`
T001 (go.mod)
 ├── T002 (API surface)
 ├── T003 (RESP types)
 │    ├── T004 (Parser) ────────────────────────┬────┐
 │    │                                          │    │
 │    ├── T005 (Writer) ────────────────┬───────┘    │
 │    │                                 │            │
 │    └── T006 (Conn) ──┬───────────────┴──────┐     │
 │                     │                      │     │
 │              T007 (Manager) ──────────┐    │     │
 │                     │                  │    │     │
 │              T008 (Pool) ─────────────┘    │     │
 │                                        │     │
 │              T009 (Listener) ──────────┘     │
 │                                             │
 │              T010 (Session) ─────────────────┤
 │                                             │
 └── T011 (Dispatcher/Mux) ────────────────────┘
          │
          ├── T012 (Pub/Sub) ────────────┐
          │                              │
          ├── T013 (Detached) ───────────┤
          │                              │
          └── T014 (Example) ────────────┘
          │
          └── T015 (CGO Parser) ───────────────┐
                                               │
          T016 (Parser Tests) ──┬──────────────┤
                                │              │
          T017 (Integration) ───┴──────────────┤
                                               │
          T018 (Benchmarks) ───────────────────┘
\`\`\`

---

## Parallel Execution Opportunities

### Fully Parallel Tasks (No Dependencies)
- None (all depend on T001)

### Phase 1 Parallel (after T001, T003)
- T002, T004, T005 can be developed in parallel
- T006 depends on T002, T003
- T008 can be done alongside T007

### Phase 2-3 Parallel (after T006, T011)
- T009, T010 can be done in parallel
- T012, T013 can be done in parallel (both depend on T011)

### Phase 4-5 Parallel (after core is done)
- T015, T016, T017, T018 can all be done in parallel

### Recommended Batches
**Batch 1:** T001, T002, T003 (Haiku - quick setup)
**Batch 2:** T004, T005, T006 (Opus/Sonnet - core protocol)
**Batch 3:** T007, T008, T009, T010 (Opus/Sonnet - connections)
**Batch 4:** T011, T013, T014 (Sonnet/Haiku - routing)
**Batch 5:** T012 (Opus - PubSub, complex)
**Batch 6:** T015, T016, T017, T018 (Opus/Sonnet - optimization and testing)

---

## Risk Mitigation Strategies

| Risk | Mitigation |
|------|------------|
| Zero-copy buffer lifecycle issues | Strict ownership model: parser never owns buffer, connection owns it until command completes. Tests with valgrind/go races. |
| CGO portability | Build tags with pure Go fallback. Document platform limitations. |
| Performance targets not met | Profile-first approach. Optimize hot paths identified by pprof. Consider more aggressive CGO usage. |
| Memory leaks from sync.Pool | Integration tests with leak detection. Monitor pool Get/Put ratios. |
| Detached connection complexity | Clear documentation. Example in example/basic-server.go. Comprehensive tests. |
| PubSub goroutine management | WaitGroup for tracking. Context-based cancellation. Tests for subscribe/unsubscribe cycles. |
| RESP parsing edge cases | Fuzz testing. Redis-benchmark compatibility testing. |

---

## Phase Completion Criteria

### Phase 1: Core Protocol
- [ ] Parser passes zero-copy allocation test
- [ ] Writer handles all RESP types
- [ ] Server accepts TCP connections
- [ ] Handler receives commands
- [ ] Example server works with redis-cli PING

### Phase 2: Session Management
- [ ] Each connection has unique session
- [ ] Session metrics track correctly
- [ ] Sessions are released on connection close
- [ ] No memory leaks in session pool

### Phase 3: Advanced Features
- [ ] Mux routes commands correctly
- [ ] PubSub subscribe/publish works
- [ ] Detached connections work
- [ ] TLS connections work

### Phase 4: CGO Optimization
- [ ] CGO version builds
- [ ] Pure Go fallback works
- [ ] Benchmark shows improvement (or documents why not)
- [ ] Cross-platform build works

### Phase 5: Testing & Documentation
- [ ] Test coverage >80%
- [ ] All benchmarks run
- [ ] Integration tests pass
- [ ] README is complete
- [ ] Performance targets met or documented

---

## Success Metrics

1. **Functional:**
   - redis-cli can connect and execute basic commands (PING, SET, GET)
   - PubSub works with multiple subscribers
   - Detached connections don't leak

2. **Performance:**
   - Zero allocations for RESP parsing (verified in test)
   - Single-threaded >2M requests/second for PING
   - Pipeline processing >10M ops/second

3. **Quality:**
   - Test coverage >80%
   - No data races (go test -race)
   - No memory leaks (testing with leak detection)

4. **API:**
   - Clean, ergonomic API
   - Example code runs without modification
   - Compatible with redcon-style handlers

---

## Reference Mapping

| Component | Reference File:Lines |
|-----------|---------------------|
| Conn Interface | redcon.go:39-118 |
| Server Structure | redcon.go:569-584 |
| Reader/Parser | redcon.go:762-1010, resp.go:107-198 |
| Writer | redcon.go:592-692, resp.go:424-522 |
| ServeMux | redcon.go:1074-1119 |
| PubSub | redcon.go:1121-1454 |
| DetachedConn | redcon.go:511-559 |

---

## Next Steps

1. **Executor agent** should start with Batch 1 (T001, T002, T003)
2. After Batch 1 completes, proceed to Batch 2 (T004, T005, T006)
3. Continue through batches in order
4. After each phase, run phase-specific completion criteria
5. After Phase 5, run full test suite and verify performance targets

**Note to Executor:** This plan prioritizes getting a working server quickly (Phase 1) before adding complexity. The zero-copy parser (T004) is the most critical component - invest time here to get it right.

---

*End of Implementation Plan*
