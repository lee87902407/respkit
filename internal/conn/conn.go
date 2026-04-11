package conn

import (
	"net"
	"sync"
	"time"

	"github.com/lee87902407/basekit/mempool"
	"github.com/lee87902407/respkit/internal/protocol"
)

const defaultWriteBufferSize = 64

// Conn is a thin scope-driven boundary around net.Conn.
type Conn struct {
	netConn    net.Conn
	remoteAddr net.Addr
	reader     *protocol.Reader
	writer     *protocol.Writer

	mu     sync.Mutex
	closed bool
}

// NewConn wraps a net.Conn with protocol helpers.
func NewConn(netConn net.Conn, reader *protocol.Reader, writer *protocol.Writer) *Conn {
	if reader == nil {
		reader = protocol.NewReader()
	}
	if writer == nil {
		writer = protocol.NewWriter(defaultWriteBufferSize)
	}

	return &Conn{
		netConn:    netConn,
		remoteAddr: netConn.RemoteAddr(),
		reader:     reader,
		writer:     writer,
	}
}

// Read reads and parses the next RESP value into the caller-provided scope.
func (c *Conn) Read(scope *mempool.Scope) (protocol.RespValue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.reader.Read(c.netConn, scope)
}

// RawRead preserves the legacy server path while the new runtime is not yet wired in.
func (c *Conn) RawRead(buf []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.netConn.Read(buf)
}

// Write buffers a RESP value for a later flush.
func (c *Conn) Write(value protocol.RespValue) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.writer.Write(value)
}

// Flush writes any buffered response bytes to the connection.
func (c *Conn) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.writer.Flush(c.netConn)
}

// NetConn exposes the wrapped connection for legacy call sites during the transition.
func (c *Conn) NetConn() net.Conn {
	return c.netConn
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

// SetWriteDeadline sets the write deadline.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.netConn.SetWriteDeadline(t)
}
