package respkit

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	blog "github.com/lee87902407/basekit/log"
	"github.com/lee87902407/basekit/mempool"
	icommand "github.com/lee87902407/respkit/internal/command"
	iconn "github.com/lee87902407/respkit/internal/conn"
	idispatcher "github.com/lee87902407/respkit/internal/dispatcher"
	"github.com/lee87902407/respkit/internal/protocol"
	"github.com/lee87902407/respkit/internal/session"
	"go.uber.org/zap"
)

const stopPollInterval = 100 * time.Millisecond

// Server represents a Redis protocol server.
// It manages listening, session creation, dispatcher wiring, and shutdown.
type Server struct {
	config           *Config
	sessionAllocator SessionAllocator
	dispatcher       *idispatcher.Dispatcher

	mu        sync.RWMutex
	listener  net.Listener
	serveErr  error
	started   bool
	serveDone chan struct{}

	sessions sync.Map // uint64 -> *session.Session
	done     atomic.Bool
}

// NewServer creates a new server using the default session runtime path.
func NewServer(config *Config) *Server {
	if config == nil {
		config = &Config{}
	}
	cfg := defaultConfig(config)
	return &Server{
		config:           cfg,
		sessionAllocator: cfg.SessionAllocator,
	}
}

func defaultConfig(config *Config) *Config {
	cfg := *config
	if cfg.Addr == "" {
		cfg.Addr = ":6379"
	}
	if cfg.Network == "" {
		cfg.Network = "tcp"
	}
	if cfg.ReadBufferSize == 0 {
		cfg.ReadBufferSize = 16384
	}
	if cfg.WriteBufferSize == 0 {
		cfg.WriteBufferSize = 4096
	}
	if cfg.DispatcherWorkers == 0 {
		cfg.DispatcherWorkers = 1
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 4096
	}
	if cfg.MaxInFlightPerSession == 0 {
		cfg.MaxInFlightPerSession = 1
	}
	if cfg.Logger == nil {
		cfg.Logger = blog.L()
	}
	if cfg.SessionAllocator == nil {
		cfg.SessionAllocator = &DefaultSessionAllocator{}
	}
	return &cfg
}

// Addr returns the bound listener address after the server has started.
func (s *Server) Addr() net.Addr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// ListenAndServe starts the server and blocks until shutdown.
func (s *Server) ListenAndServe() error {
	return s.Start()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown() error {
	return s.Stop()
}

// Start starts the listener and blocks until the server stops.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("respkit: server already started")
	}

	ln, err := iconn.Listen(s.config.Network, s.config.Addr)
	if err != nil {
		s.mu.Unlock()
		return err
	}

	registry := icommand.NewRegistry()
	icommand.RegisterBuiltins(registry)
	dispatcher := idispatcher.NewDispatcher(registry, s.config.DispatcherWorkers, s.config.QueueSize)
	dispatcher.Start()

	serveDone := make(chan struct{})
	s.listener = ln
	s.dispatcher = dispatcher
	s.serveDone = serveDone
	s.serveErr = nil
	s.started = true
	s.done.Store(false)
	s.mu.Unlock()

	err = s.serve(ln)

	dispatcher.Stop()

	s.mu.Lock()
	s.serveErr = err
	s.listener = nil
	s.dispatcher = nil
	s.serveDone = nil
	s.started = false
	s.mu.Unlock()

	close(serveDone)
	return err
}

// Stop gracefully stops the server.
func (s *Server) Stop() error {
	return s.shutdown(false)
}

// Close forcefully stops the server.
func (s *Server) Close() error {
	return s.shutdown(true)
}

func (s *Server) shutdown(force bool) error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	listener := s.listener
	serveDone := s.serveDone
	s.listener = nil
	s.started = false
	s.mu.Unlock()

	s.done.Store(true)

	if listener != nil {
		_ = listener.Close()
	}

	s.sessions.Range(func(_, value any) bool {
		sess, ok := value.(*session.Session)
		if !ok {
			return true
		}
		if force {
			_ = sess.Close()
		} else {
			sess.Stop()
		}
		return true
	})

	if serveDone != nil {
		<-serveDone
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.serveErr
}

// ActiveSessions returns the number of active sessions.
func (s *Server) ActiveSessions() int {
	count := 0
	s.sessions.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func (s *Server) serve(ln net.Listener) error {
	defer ln.Close()

	for {
		netConn, err := ln.Accept()
		if err != nil {
			if s.done.Load() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			if s.config.Logger != nil {
				s.config.Logger.Warn("respkit: accept failed", zap.Error(err))
			}
			continue
		}

		if s.done.Load() {
			_ = netConn.Close()
			continue
		}

		conn := iconn.NewConn(netConn, nil, nil)
		if !s.serveDefaultRuntime(conn) {
			_ = conn.Close()
		}
	}
}

func (s *Server) serveDefaultRuntime(conn *iconn.Conn) bool {
	allocator := s.sessionAllocator
	if allocator == nil {
		allocator = &DefaultSessionAllocator{}
	}

	placeholder := &runtimeConn{conn: conn}
	sess := allocator.Allocate(placeholder)
	if sess == nil {
		return false
	}
	placeholder.sess = sess

	sess.SetCloser(conn)
	sess.SetOnClose(func(ss *session.Session) {
		s.sessions.Delete(ss.ID)
		allocator.Release(ss)
	})
	if s.config.MemPool != nil {
		sess.SetScopeFactory(func() *mempool.Scope {
			return mempool.NewScope(s.config.MemPool)
		})
	}
	sess.SetRequestReader(func(scope *mempool.Scope) (protocol.RespValue, error) {
		if err := conn.SetReadDeadline(s.readDeadline()); err != nil {
			return protocol.RespValue{}, err
		}
		value, err := conn.Read(scope)
		if err == nil {
			sess.UpdateLastSeen()
			sess.IncrementCommands()
			sess.AddBytesRead(uint64(len(protocol.SerializeValue(value))))
		}
		return value, err
	})
	if s.config.MaxInFlightPerSession > 0 {
		sess.SetMaxInFlight(int32(s.config.MaxInFlightPerSession))
	}
	sess.UseDispatcher(s.dispatcher)

	s.sessions.Store(sess.ID, sess)
	sess.StartLoops(&sessionConnWriter{conn: conn, sess: sess})
	return true
}

func (s *Server) readDeadline() time.Time {
	if s.config.IdleTimeout > 0 {
		return time.Now().Add(s.config.IdleTimeout)
	}
	return time.Now().Add(stopPollInterval)
}

type sessionConnWriter struct {
	conn         *iconn.Conn
	sess         *session.Session
	pendingBytes int
}

func (w *sessionConnWriter) Write(value protocol.RespValue) error {
	if err := w.conn.Write(value); err != nil {
		return err
	}
	w.pendingBytes += len(protocol.SerializeValue(value))
	return nil
}

func (w *sessionConnWriter) Flush() error {
	if err := w.conn.Flush(); err != nil {
		return err
	}
	if w.pendingBytes > 0 {
		w.sess.AddBytesWritten(uint64(w.pendingBytes))
		w.pendingBytes = 0
	}
	return nil
}

// runtimeConn is the lightweight adapter kept for SessionAllocator compatibility.
type runtimeConn struct {
	conn *iconn.Conn
	sess *session.Session
}

func (c *runtimeConn) Session() *session.Session  { return c.sess }
func (c *runtimeConn) SetData(v interface{})      { c.sess.SetData(v) }
func (c *runtimeConn) WriteString(string) error   { return nil }
func (c *runtimeConn) WriteBulk([]byte) error     { return nil }
func (c *runtimeConn) WriteInt(int64) error       { return nil }
func (c *runtimeConn) WriteArray(int) error       { return nil }
func (c *runtimeConn) WriteNull() error           { return nil }
func (c *runtimeConn) WriteError(string) error    { return nil }
func (c *runtimeConn) WriteAny(interface{}) error { return nil }
func (c *runtimeConn) Flush() error               { return nil }
func (c *runtimeConn) Close() error               { return c.conn.Close() }
func (c *runtimeConn) RemoteAddr() net.Addr       { return c.conn.RemoteAddr() }
func (c *runtimeConn) Detach() DetachedConn       { return c }
