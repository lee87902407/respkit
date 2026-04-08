package conn

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lee87902407/respkit/internal/session"
)

var (
	// ErrManagerClosed is returned when the manager is closed
	ErrManagerClosed = errors.New("connection manager closed")
)

// Handler processes parsed commands from a connection.
type Handler interface {
	Handle(conn *Conn, cmd Command) error
}

// Config holds configuration for the connection manager.
type Config struct {
	IdleTimeout    time.Duration
	MaxConnections int
}

// ConnectionManager manages the lifecycle of client connections.
type ConnectionManager struct {
	connPool   *Pool
	sessions   sync.Map
	config     *Config
	handler    Handler
	conns      map[*Conn]bool
	mu         sync.RWMutex
	done       atomic.Bool
	wg         sync.WaitGroup
	nextConnID atomic.Uint64
	acceptErr  func(error)
}

// NewManager creates a new connection manager.
func NewManager(config *Config, handler Handler) *ConnectionManager {
	if config == nil {
		config = &Config{}
	}
	return &ConnectionManager{
		connPool: GlobalPool(),
		config:   config,
		handler:  handler,
		conns:    make(map[*Conn]bool),
		acceptErr: func(err error) {
			log.Printf("accept error: %v", err)
		},
	}
}

// SetAcceptError sets the accept error callback.
func (m *ConnectionManager) SetAcceptError(fn func(error)) {
	m.acceptErr = fn
}

// Start begins accepting connections on the listener and blocks until shutdown.
func (m *ConnectionManager) Start(ln net.Listener) error {
	defer func() {
		_ = ln.Close()
		m.cleanup()
	}()

	for {
		netConn, err := ln.Accept()
		if err != nil {
			if m.done.Load() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			if m.acceptErr != nil {
				m.acceptErr(err)
			}
			continue
		}

		c := m.newConn(netConn)
		if c == nil {
			_ = netConn.Close()
			continue
		}

		m.mu.Lock()
		if m.done.Load() {
			m.mu.Unlock()
			_ = c.Close()
			return nil
		}
		m.conns[c] = true
		m.wg.Add(1)
		m.mu.Unlock()

		go m.handleConnection(c)
	}
}

// Stop gracefully shuts down the connection manager.
func (m *ConnectionManager) Stop() error {
	if !m.done.CompareAndSwap(false, true) {
		return nil
	}

	m.mu.Lock()
	conns := make([]*Conn, 0, len(m.conns))
	for c := range m.conns {
		conns = append(conns, c)
	}
	m.mu.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}

	m.wg.Wait()
	return nil
}

func (m *ConnectionManager) handleConnection(c *Conn) {
	defer m.wg.Done()
	defer m.removeConnection(c)
	defer m.connPool.Put(c)

	sessID := m.nextConnID.Add(1)
	sess := session.NewSession(sessID)
	sess.Conn = c
	c.SetSession(sess)
	m.sessions.Store(sessID, sess)

	buf := c.ReadBuf()
	for {
		if m.done.Load() || c.IsClosed() {
			return
		}

		if m.config.IdleTimeout > 0 {
			_ = c.NetConn().SetReadDeadline(time.Now().Add(m.config.IdleTimeout))
		}

		n, err := c.NetConn().Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			return
		}
		if n == 0 {
			continue
		}

		sess.UpdateLastSeen()
		sess.AddBytesRead(uint64(n))
		c.Parser().Reset(buf[:n])
		for {
			result := c.Parser().ParseNextCommand()
			if !result.Complete {
				break
			}
			cmd := Command{Raw: result.Raw, Args: result.Args}
			sess.IncrementCommands()
			if m.handler != nil {
				if err := m.handler.Handle(c, cmd); err != nil {
					_ = c.WriteError(err.Error())
				}
			}
			if err := c.Flush(); err != nil {
				return
			}
		}
	}
}

func (m *ConnectionManager) removeConnection(c *Conn) {
	m.mu.Lock()
	delete(m.conns, c)
	m.mu.Unlock()
	if sess := c.Session(); sess != nil {
		m.sessions.Delete(sess.ID)
	}
}

func (m *ConnectionManager) cleanup() {
	m.mu.Lock()
	conns := m.conns
	m.conns = make(map[*Conn]bool)
	m.mu.Unlock()
	for c := range conns {
		_ = c.Close()
	}
}

// ActiveConnections returns the number of active connections.
func (m *ConnectionManager) ActiveConnections() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.conns)
}

// CloseConn closes a connection immediately.
func (m *ConnectionManager) CloseConn(c *Conn) {
	if c == nil {
		return
	}
	_ = c.Close()
}

func (m *ConnectionManager) newConn(netConn net.Conn) *Conn {
	c := m.connPool.Get()
	c.Attach(netConn)
	if m.config.IdleTimeout > 0 {
		c.SetIdleDeadline(time.Now().Add(m.config.IdleTimeout))
	}
	return c
}
