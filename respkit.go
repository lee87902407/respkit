package respkit

import (
	"net"
	"sync/atomic"
	"time"

	blog "github.com/lee87902407/basekit/log"
	"github.com/lee87902407/basekit/mempool"
	"github.com/lee87902407/respkit/internal/protocol"
	"github.com/lee87902407/respkit/internal/session"
	"go.uber.org/zap"
)

// Config configures the server.
type Config struct {
	// Network configuration.
	Addr    string
	Network string // "tcp", "tcp4", "tcp6", "unix"

	// Buffer sizing.
	ReadBufferSize  int // Default: 16384
	WriteBufferSize int // Default: 4096

	// Dispatcher configuration.
	DispatcherWorkers     int // Default: 1
	QueueSize             int // Default: 4096
	MaxInFlightPerSession int // Default: 1

	// Connection management.
	IdleTimeout time.Duration

	// Optional shared dependencies.
	MemPool mempool.BytePool
	Logger  *zap.Logger

	// Session configuration.
	SessionAllocator SessionAllocator
}

// Command represents a parsed command with zero-copy argument slices.
type Command struct {
	Raw  []byte   // Original raw bytes
	Args [][]byte // Parsed arguments (zero-copy slices into read buffer)
}

// ScopedRequest carries a scope-managed request value for a session.
type ScopedRequest struct {
	Session *session.Session
	Value   protocol.RespValue
	Scope   *mempool.Scope
}

// ScopedResponse carries a scope-managed response value for a session.
type ScopedResponse struct {
	Session *session.Session
	Value   protocol.RespValue
	Scope   *mempool.Scope
}

// DefaultLogger returns the basekit logger when no explicit logger is supplied.
func DefaultLogger() *zap.Logger {
	return blog.L()
}

// Context provides context for command handling.
type Context struct {
	Conn    Conn
	Command Command
	Session *session.Session
}

// Conn represents a client connection.
type Conn interface {
	// Session access.
	Session() *session.Session
	SetData(interface{})

	// I/O operations.
	WriteString(s string) error
	WriteBulk(b []byte) error
	WriteInt(n int64) error
	WriteArray(n int) error
	WriteNull() error
	WriteError(msg string) error
	WriteAny(v interface{}) error
	Flush() error

	// Connection control.
	Close() error
	RemoteAddr() net.Addr
	Detach() DetachedConn
}

// DetachedConn is a connection managed by the caller.
type DetachedConn interface {
	Conn
}

// SessionAllocator manages session allocation.
type SessionAllocator interface {
	Allocate(conn Conn) *session.Session
	Release(session *session.Session)
}

// DefaultSessionAllocator creates sessions with incrementing IDs.
type DefaultSessionAllocator struct {
	nextID atomic.Uint64
}

// Allocate creates a new session.
func (a *DefaultSessionAllocator) Allocate(conn Conn) *session.Session {
	id := a.nextID.Add(1)
	return session.NewSession(id)
}

// Release releases a session back to the pool.
func (a *DefaultSessionAllocator) Release(sess *session.Session) {
	if sess == nil {
		return
	}
	sess.SetData(nil)
}
