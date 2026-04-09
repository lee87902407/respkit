package conn

import (
	"net"
	"sync"
	"time"
)

// Conn is a thin wrapper around net.Conn for raw byte I/O.
// It does not own buffers or protocol logic.
type Conn struct {
	netConn    net.Conn
	remoteAddr net.Addr
	mu         sync.Mutex
	closed     bool
}

// NewConn wraps a net.Conn.
func NewConn(netConn net.Conn) *Conn {
	addr := netConn.RemoteAddr()
	return &Conn{
		netConn:    netConn,
		remoteAddr: addr,
	}
}

// Read reads data from the connection into buf.
func (c *Conn) Read(buf []byte) (int, error) {
	return c.netConn.Read(buf)
}

// Write writes data to the connection.
func (c *Conn) Write(data []byte) (int, error) {
	return c.netConn.Write(data)
}

// Close closes the underlying connection.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.netConn.Close()
}

// IsClosed reports whether the connection has been closed.
func (c *Conn) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// RemoteAddr returns the remote address.
func (c *Conn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

// SetDeadline sets the read and write deadlines.
func (c *Conn) SetDeadline(t time.Time) error {
	return c.netConn.SetDeadline(t)
}

// SetReadDeadline sets the read deadline.
func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.netConn.SetReadDeadline(t)
}
