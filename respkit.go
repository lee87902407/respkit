package respkit

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	iconn "github.com/lee87902407/respkit/internal/conn"
	"github.com/lee87902407/respkit/internal/session"
)

// Server represents a Redis protocol server
type Server struct {
	config    *Config
	handler   Handler
	done      chan struct{}
	sessionID atomic.Uint64

	mu       sync.RWMutex
	listener net.Listener
	manager  *iconn.ConnectionManager
}

// Config configures the server
type Config struct {
	// Network configuration
	Addr    string
	Network string // "tcp", "tcp4", "tcp6", "unix"

	// Performance tuning
	ReadBufferSize  int // Default: 16384
	WriteBufferSize int // Default: 4096
	MaxPipeline     int // Default: 1024

	// Connection management
	IdleTimeout    time.Duration
	MaxConnections int

	// Session configuration
	SessionAllocator SessionAllocator

	// CGO options
	UseCGOParser bool // Use C parser if available
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
	Conn    Conn
	Command Command
	Session *session.Session
}

// Command represents a parsed command
type Command struct {
	Raw  []byte   // Original raw bytes
	Args [][]byte // Parsed arguments
}

// Conn represents a client connection
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

// DetachedConn is a connection managed by the caller
type DetachedConn interface {
	Conn
	Flush() error
}

// SessionAllocator manages session allocation
type SessionAllocator interface {
	Allocate(conn Conn) *session.Session
	Release(session *session.Session)
}

// DefaultSessionAllocator is the default session allocator
type DefaultSessionAllocator struct {
	nextID atomic.Uint64
}

// Allocate creates a new session
func (a *DefaultSessionAllocator) Allocate(conn Conn) *session.Session {
	id := a.nextID.Add(1)
	sess := session.NewSession(id)
	sess.Conn = conn
	return sess
}

// Release releases a session back to the pool
func (a *DefaultSessionAllocator) Release(sess *session.Session) {
	if sess == nil {
		return
	}
	sess.Data = nil
	sess.Conn = nil
	sess.WatchedKeys = nil
	sess.SubscribedChannels = nil
	sess.PatternSubs = nil
	sess.MultiActive = false
}

// NewServer creates a new server
func NewServer(config *Config, handler Handler) *Server {
	if config == nil {
		config = &Config{
			Addr:            ":6379",
			Network:         "tcp",
			ReadBufferSize:  16384,
			WriteBufferSize: 4096,
			MaxPipeline:     1024,
		}
	}

	return &Server{
		config:  config,
		handler: handler,
		done:    make(chan struct{}),
	}
}

// connHandler adapts the internal connection manager to the public handler API.
type connHandler struct {
	server *Server
}

func (h *connHandler) Handle(c *iconn.Conn, cmd iconn.Command) error {
	if len(cmd.Args) == 0 {
		return nil
	}
	publicConn := &serverConn{conn: c}
	sess := c.Session()
	if sess == nil {
		sess = h.server.config.SessionAllocator.Allocate(publicConn)
		c.SetSession(sess)
	}
	ctx := &Context{
		Conn: publicConn,
		Command: Command{
			Raw:  cmd.Raw,
			Args: cmd.Args,
		},
		Session: sess,
	}
	if h.server.handler == nil {
		return nil
	}
	return h.server.handler.Handle(ctx)
}

// serverConn adapts internal conn.Conn to the public Conn interface.
type serverConn struct {
	conn *iconn.Conn
}

func (c *serverConn) Session() *session.Session  { return c.conn.Session() }
func (c *serverConn) SetData(v interface{})      { c.conn.SetData(v) }
func (c *serverConn) WriteString(s string) error { return c.conn.WriteString(s) }
func (c *serverConn) WriteBulk(b []byte) error   { return c.conn.WriteBulk(b) }
func (c *serverConn) WriteInt(n int64) error     { return c.conn.WriteInt(n) }
func (c *serverConn) WriteArray(n int) error     { return c.conn.WriteArray(n) }
func (c *serverConn) WriteNull() error           { return c.conn.WriteNull() }
func (c *serverConn) WriteError(msg string) error {
	if msg == "" {
		msg = "ERR"
	}
	if !strings.HasPrefix(strings.ToUpper(msg), "ERR") {
		msg = fmt.Sprintf("ERR %s", msg)
	}
	return c.conn.WriteError(msg)
}
func (c *serverConn) WriteAny(v interface{}) error { return c.conn.WriteAny(v) }
func (c *serverConn) Close() error                 { return c.conn.Close() }
func (c *serverConn) RemoteAddr() net.Addr         { return c.conn.RemoteAddr() }
func (c *serverConn) Detach() DetachedConn         { return &detachedAdapter{inner: c.conn.Detach()} }

type detachedAdapter struct {
	inner iconn.DetachedConn
}

func (d *detachedAdapter) Session() *session.Session    { return d.inner.Session() }
func (d *detachedAdapter) SetData(v interface{})        { d.inner.SetData(v) }
func (d *detachedAdapter) WriteString(s string) error   { return d.inner.WriteString(s) }
func (d *detachedAdapter) WriteBulk(b []byte) error     { return d.inner.WriteBulk(b) }
func (d *detachedAdapter) WriteInt(n int64) error       { return d.inner.WriteInt(n) }
func (d *detachedAdapter) WriteArray(n int) error       { return d.inner.WriteArray(n) }
func (d *detachedAdapter) WriteNull() error             { return d.inner.WriteNull() }
func (d *detachedAdapter) WriteError(msg string) error  { return d.inner.WriteError(msg) }
func (d *detachedAdapter) WriteAny(v interface{}) error { return d.inner.WriteAny(v) }
func (d *detachedAdapter) Close() error                 { return d.inner.Close() }
func (d *detachedAdapter) RemoteAddr() net.Addr         { return d.inner.RemoteAddr() }
func (d *detachedAdapter) Detach() DetachedConn         { return d }
func (d *detachedAdapter) Flush() error                 { return d.inner.Flush() }

// Addr returns the bound listener address after the server has started.
func (s *Server) Addr() net.Addr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// ListenAndServe starts the server
func (s *Server) ListenAndServe() error {
	if s.config.SessionAllocator == nil {
		s.config.SessionAllocator = &DefaultSessionAllocator{}
	}
	ln, err := iconn.Listen(s.config.Network, s.config.Addr)
	if err != nil {
		return err
	}
	manager := iconn.NewManager(&iconn.Config{
		IdleTimeout:    s.config.IdleTimeout,
		MaxConnections: s.config.MaxConnections,
	}, &connHandler{server: s})

	s.mu.Lock()
	s.listener = ln
	s.manager = manager
	s.mu.Unlock()

	return manager.Start(ln)
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() error {
	select {
	case <-s.done:
	default:
		close(s.done)
	}

	s.mu.RLock()
	manager := s.manager
	listener := s.listener
	s.mu.RUnlock()

	if manager != nil {
		_ = manager.Stop()
	}
	if listener != nil {
		return listener.Close()
	}
	return nil
}
