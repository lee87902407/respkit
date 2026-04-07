package conn

import (
	"sync"
	"time"
)

// Global pool for connection reuse.
var globalPool = &Pool{
	p: sync.Pool{
		New: func() interface{} {
			return NewConn(0, nil)
		},
	},
}

// Pool manages connection reuse using sync.Pool.
type Pool struct {
	p sync.Pool
}

// GlobalPool returns the package-level connection pool.
func GlobalPool() *Pool {
	return globalPool
}

// Get retrieves a connection from the pool or creates a new one.
func (p *Pool) Get() *Conn {
	c := p.p.Get().(*Conn)
	c.mu.Lock()
	c.fd = 0
	c.netConn = nil
	c.remoteAddr = nil
	c.parser.Reset(nil)
	c.writer.Reset()
	c.writeBuf = c.writer.Bytes()
	c.session = nil
	c.cmds = c.cmds[:0]
	c.idleDeadline = time.Time{}
	c.detached = false
	c.closed = false
	c.mu.Unlock()
	return c
}

// Put returns a connection to the pool for reuse.
func (p *Pool) Put(c *Conn) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.fd = 0
	c.netConn = nil
	c.remoteAddr = nil
	c.parser.Reset(nil)
	c.writer.Reset()
	c.writeBuf = c.writer.Bytes()
	c.session = nil
	c.cmds = c.cmds[:0]
	c.idleDeadline = time.Time{}
	c.detached = false
	c.closed = false
	c.mu.Unlock()
	p.p.Put(c)
}
