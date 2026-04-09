package respkit

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	blog "github.com/lee87902407/basekit/log"
	iconn "github.com/lee87902407/respkit/internal/conn"
	"github.com/lee87902407/respkit/internal/protocol"
	"github.com/lee87902407/respkit/internal/resp"
	"github.com/lee87902407/respkit/internal/session"
)

const stopPollInterval = 100 * time.Millisecond

// commandHandler processes a command.
type commandHandler interface {
	Handle(ctx *Context) error
}

// commandFactory creates commands from raw parsed data.
type commandFactory interface {
	CreateCommand(name string, raw []byte, args [][]byte) (Command, error)
}

// Server represents a Redis protocol server.
// It manages listening, session creation, command parsing, and shutdown.
type Server struct {
	config  *Config
	handler commandHandler
	factory commandFactory

	mu        sync.RWMutex
	listener  net.Listener
	serveErr  error
	started   bool
	serveDone chan struct{}

	sessions   sync.Map // uint64 -> *session.Session
	nextSessID atomic.Uint64
	done       atomic.Bool
}

// NewServer creates a new server with an optional command handler.
func NewServer(config *Config, handler ...interface{ Handle(*Context) error }) *Server {
	if config == nil {
		config = &Config{}
	}
	cfg := defaultConfig(config)
	s := &Server{config: cfg}
	if len(handler) > 0 && handler[0] != nil {
		s.handler = handler[0]
	}
	return s
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
	return &cfg
}

// Register sets the command factory for the server.
func (s *Server) Register(factory interface {
	CreateCommand(name string, raw []byte, args [][]byte) (Command, error)
}) {
	s.factory = factory
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
// It is an alias for Start().
func (s *Server) ListenAndServe() error {
	return s.Start()
}

// Shutdown gracefully shuts down the server.
// It is an alias for Stop().
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

	serveDone := make(chan struct{})
	s.listener = ln
	s.serveDone = serveDone
	s.serveErr = nil
	s.started = true
	s.done.Store(false)
	s.mu.Unlock()

	err = s.serve(ln)

	s.mu.Lock()
	s.serveErr = err
	s.listener = nil
	s.serveDone = nil
	s.started = false
	s.mu.Unlock()

	close(serveDone)
	return err
}

// Stop gracefully stops the server. It stops accepting new connections
// and signals all active sessions to exit through their normal read loop.
func (s *Server) Stop() error {
	return s.shutdown(false)
}

// Close forcefully stops the server. It immediately closes all sessions.
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

	s.sessions.Range(func(_, value interface{}) bool {
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
	s.sessions.Range(func(_, _ interface{}) bool {
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
			log.Printf("accept: %v", err)
			continue
		}

		if s.done.Load() {
			_ = netConn.Close()
			continue
		}

		conn := iconn.NewConn(netConn)
		sessID := s.nextSessID.Add(1)
		sess := session.NewSession(sessID)
		sess.SetCloser(conn)
		sess.SetOnRemove(func(ss *session.Session) {
			s.sessions.Delete(ss.ID)
		})

		s.sessions.Store(sessID, sess)
		sess.Start(s.makeHandle(sess, conn))
	}
}

func (s *Server) makeHandle(sess *session.Session, conn *iconn.Conn) func() {
	return func() {
		buf := make([]byte, s.config.ReadBufferSize)
		parser := resp.NewParser(nil)
		writer := resp.NewWriter(s.config.WriteBufferSize)
		sc := &serverConn{
			conn:         conn,
			writer:       writer,
			sess:         sess,
			writeBufSize: s.config.WriteBufferSize,
		}

		for {
			if sess.ShouldStop() {
				return
			}

			if err := conn.SetReadDeadline(s.readDeadline()); err != nil {
				return
			}

			n, err := conn.Read(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					if sess.ShouldStop() {
						return
					}
					continue
				}
				return
			}
			if n == 0 {
				continue
			}

			sess.UpdateLastSeen()
			sess.AddBytesRead(uint64(n))
			parser.Reset(buf[:n])

			for {
				result := parser.ParseNextCommand()
				if !result.Complete {
					break
				}
				if len(result.Args) == 0 {
					continue
				}

				sess.IncrementCommands()

				if s.handler != nil {
					ctx := &Context{
						Conn:    sc,
						Command: Command{Raw: result.Raw, Args: result.Args},
						Session: sess,
					}
					if err := s.handler.Handle(ctx); err != nil {
						sc.WriteError(err.Error())
					}
					_ = sc.Flush()
					continue
				}

				if s.factory != nil {
					name := protocol.NormalizeCommandNameBytes(result.Args[0])
					if _, err := s.factory.CreateCommand(name, result.Raw, result.Args); err != nil {
						writer.AppendError(err.Error())
						bytesToWrite := len(writer.Bytes())
						if flushErr := writer.Flush(conn); flushErr != nil {
							return
						}
						sess.AddBytesWritten(uint64(bytesToWrite))
					}
				}
			}
		}
	}
}

func (s *Server) readDeadline() time.Time {
	if s.config.IdleTimeout > 0 {
		return time.Now().Add(s.config.IdleTimeout)
	}
	return time.Now().Add(stopPollInterval)
}

// serverConn adapts internal conn.Conn to the public Conn interface.
type serverConn struct {
	conn         *iconn.Conn
	writer       *resp.Writer
	sess         *session.Session
	writeBufSize int
	mu           sync.Mutex
}

func (c *serverConn) Session() *session.Session { return c.sess }
func (c *serverConn) SetData(v interface{})     { c.sess.SetData(v) }

func (c *serverConn) WriteString(s string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writer.AppendString(s)
	return nil
}

func (c *serverConn) WriteBulk(b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writer.AppendBulk(b)
	return nil
}

func (c *serverConn) WriteInt(n int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writer.AppendInt(n)
	return nil
}

func (c *serverConn) WriteArray(n int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writer.AppendArray(n)
	return nil
}

func (c *serverConn) WriteNull() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writer.AppendNull()
	return nil
}

func (c *serverConn) WriteAny(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writer.AppendAny(v)
	return nil
}

func (c *serverConn) WriteError(msg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if msg == "" {
		msg = "ERR"
	}
	if !strings.HasPrefix(strings.ToUpper(msg), "ERR") {
		msg = fmt.Sprintf("ERR %s", msg)
	}
	c.writer.AppendError(msg)
	return nil
}

func (c *serverConn) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.flushLocked()
}

func (c *serverConn) flushLocked() error {
	n := len(c.writer.Bytes())
	if n == 0 {
		return nil
	}
	if err := c.writer.Flush(c.conn); err != nil {
		return err
	}
	c.sess.AddBytesWritten(uint64(n))
	return nil
}

func (c *serverConn) Close() error         { return c.conn.Close() }
func (c *serverConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

func (c *serverConn) Detach() DetachedConn {
	return &detachedConn{
		conn:   c.conn,
		writer: resp.NewWriter(c.writeBufSize),
		sess:   c.sess,
	}
}

// detachedConn wraps an internal connection for use after handler exit.
type detachedConn struct {
	conn   *iconn.Conn
	writer *resp.Writer
	sess   *session.Session
	mu     sync.Mutex
}

func (d *detachedConn) Session() *session.Session { return d.sess }
func (d *detachedConn) SetData(v interface{})     { d.sess.SetData(v) }

func (d *detachedConn) WriteString(s string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writer.AppendString(s)
	return nil
}

func (d *detachedConn) WriteBulk(b []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writer.AppendBulk(b)
	return nil
}

func (d *detachedConn) WriteInt(n int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writer.AppendInt(n)
	return nil
}

func (d *detachedConn) WriteArray(n int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writer.AppendArray(n)
	return nil
}

func (d *detachedConn) WriteNull() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writer.AppendNull()
	return nil
}

func (d *detachedConn) WriteAny(v interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writer.AppendAny(v)
	return nil
}

func (d *detachedConn) WriteError(msg string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if msg == "" {
		msg = "ERR"
	}
	if !strings.HasPrefix(strings.ToUpper(msg), "ERR") {
		msg = fmt.Sprintf("ERR %s", msg)
	}
	d.writer.AppendError(msg)
	return nil
}

func (d *detachedConn) Flush() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.flushLocked()
}

func (d *detachedConn) flushLocked() error {
	n := len(d.writer.Bytes())
	if n == 0 {
		return nil
	}
	if err := d.writer.Flush(d.conn); err != nil {
		return err
	}
	d.sess.AddBytesWritten(uint64(n))
	return nil
}

func (d *detachedConn) Close() error         { return d.conn.Close() }
func (d *detachedConn) RemoteAddr() net.Addr { return d.conn.RemoteAddr() }
func (d *detachedConn) Detach() DetachedConn { return d }
