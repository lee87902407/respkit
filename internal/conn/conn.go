package conn

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/lee87902407/basekit/mempool"
	"github.com/lee87902407/respkit/internal/protocol"
)

const defaultReadBufferSize = 4096

// Conn is a thin scope-driven boundary around net.Conn.
type Conn struct {
	netConn    net.Conn
	remoteAddr net.Addr

	mu      sync.Mutex
	closed  bool
	readBuf []byte
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

// ReadValue reads and parses the next RESP value into the caller-provided scope.
func (c *Conn) ReadValue(scope *mempool.Scope) (protocol.RespValue, error) {
	if scope == nil {
		return protocol.RespValue{}, fmt.Errorf("conn: read scope is nil")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for {
		value, consumed, ok := protocol.ParseValue(c.readBuf)
		if ok {
			value = copyValueIntoScope(scope, value)
			c.compactReadBuffer(consumed)
			return value, nil
		}

		chunk := scope.Get(defaultReadBufferSize)
		n, err := c.netConn.Read(chunk)
		if n > 0 {
			c.readBuf = append(c.readBuf, chunk[:n]...)
		}
		if err != nil {
			return protocol.RespValue{}, err
		}
	}
}

func (c *Conn) compactReadBuffer(consumed int) {
	if consumed >= len(c.readBuf) {
		c.readBuf = c.readBuf[:0]
		return
	}
	copy(c.readBuf, c.readBuf[consumed:])
	c.readBuf = c.readBuf[:len(c.readBuf)-consumed]
}

func copyValueIntoScope(scope *mempool.Scope, value protocol.RespValue) protocol.RespValue {
	switch value.Type {
	case protocol.TypeBulkString:
		if value.Bulk == nil {
			return value
		}
		dup := scope.Get(len(value.Bulk))
		copy(dup, value.Bulk)
		value.Bulk = dup[:len(value.Bulk)]
		return value
	case protocol.TypeArray:
		if len(value.Array) == 0 {
			return value
		}
		arr := make([]protocol.RespValue, len(value.Array))
		for i := range value.Array {
			arr[i] = copyValueIntoScope(scope, value.Array[i])
		}
		value.Array = arr
		return value
	default:
		return value
	}
}

// Write writes data to the connection.
func (c *Conn) Write(data []byte) (int, error) {
	return c.netConn.Write(data)
}

// WriteValue serializes a RESP value into the caller-provided scope and writes it.
func (c *Conn) WriteValue(scope *mempool.Scope, value protocol.RespValue) error {
	if scope == nil {
		return fmt.Errorf("conn: write scope is nil")
	}

	payload := protocol.SerializeValue(value)
	buf := scope.Get(len(payload))
	copy(buf, payload)
	_, err := c.netConn.Write(buf[:len(payload)])
	return err
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
