package respkit

import (
	"net"
	"sync/atomic"
	"time"

	"github.com/lee87902407/respkit/internal/session"
)

// Config configures the server.
type Config struct {
	// Network configuration.
	Addr    string
	Network string // "tcp", "tcp4", "tcp6", "unix"

	// Performance tuning.
	ReadBufferSize  int // Default: 16384
	WriteBufferSize int // Default: 4096
	MaxPipeline     int // Default: 1024

	// Connection management.
	IdleTimeout    time.Duration
	MaxConnections int

	// Session configuration.
	SessionAllocator SessionAllocator

	// CGO options.
	UseCGOParser bool // Use C parser if available
}

// Command represents a parsed command with zero-copy argument slices.
type Command struct {
	Raw  []byte   // Original raw bytes
	Args [][]byte // Parsed arguments (zero-copy slices into read buffer)
}

// CommandFactory creates commands from raw parsed data.
type CommandFactory interface {
	CreateCommand(name string, raw []byte, args [][]byte) (Command, error)
}

// CommandFactoryFunc is a function adapter for CommandFactory.
type CommandFactoryFunc func(name string, raw []byte, args [][]byte) (Command, error)

func (f CommandFactoryFunc) CreateCommand(name string, raw []byte, args [][]byte) (Command, error) {
	return f(name, raw, args)
}

// Handler processes a command.
type Handler interface {
	Handle(ctx *Context) error
}

// HandlerFunc is an adapter for functions.
type HandlerFunc func(ctx *Context) error

func (f HandlerFunc) Handle(ctx *Context) error {
	return f(ctx)
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
