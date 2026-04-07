package conn

import (
	"net"
	"sync"
	"time"

	"github.com/yanjie/netgo/internal/resp"
	"github.com/yanjie/netgo/internal/session"
)

// DetachedConn is a connection managed by the caller
type DetachedConn interface {
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
	Flush() error
}

const (
	// DefaultReadBufferSize is the default size of the read buffer (16KB)
	DefaultReadBufferSize = 16384
	// DefaultWriteBufferSize is the initial size of the write buffer (1KB)
	DefaultWriteBufferSize = 1024
	// MaxWriteBufferSize is the maximum size the write buffer can grow to (64KB)
	MaxWriteBufferSize = 65536
	// WriteBufferGrowthFactor is the factor by which the write buffer grows
	WriteBufferGrowthFactor = 2
)

// Conn represents a client connection with zero-copy friendly buffers
type Conn struct {
	fd           int              // File descriptor (for epoll/kqueue)
	netConn      net.Conn         // Underlying network connection
	remoteAddr   net.Addr         // Remote network address
	readBuf      []byte           // Fixed-size read buffer (default 16KB)
	writeBuf     []byte           // Write aggregation buffer (starts 1KB, grows to max 64KB)
	parser       *resp.Parser     // Zero-copy RESP parser
	writer       *resp.Writer     // RESP protocol writer
	session      *session.Session // Session state
	cmds         []Command        // Reusable command slice for pipeline
	idleDeadline time.Time        // Idle timeout deadline
	detached     bool             // Whether connection is detached
	closed       bool             // Whether connection is closed
	mu           sync.Mutex       // Protects write operations and state changes
}

// Command represents a parsed RESP command
type Command struct {
	Raw  []byte   // Original raw bytes (zero-copy into readBuf)
	Args [][]byte // Parsed arguments (zero-copy slices into readBuf)
}

// NewConn creates a new connection with the given file descriptor and remote address
func NewConn(fd int, remoteAddr net.Addr) *Conn {
	readBuf := make([]byte, DefaultReadBufferSize)
	writer := resp.NewWriter(DefaultWriteBufferSize)

	return &Conn{
		fd:         fd,
		remoteAddr: remoteAddr,
		readBuf:    readBuf,
		writeBuf:   writer.Bytes(),
		parser:     resp.NewParser(nil),
		writer:     writer,
		cmds:       make([]Command, 0, 16),
	}
}

// Init initializes a pooled connection with a file descriptor and remote address.
func (c *Conn) Init(fd int, remoteAddr net.Addr) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fd = fd
	c.remoteAddr = remoteAddr
	c.netConn = nil
	c.parser.Reset(nil)
	c.writer.Reset()
	c.writeBuf = c.writer.Bytes()
	c.session = nil
	c.cmds = c.cmds[:0]
	c.idleDeadline = time.Time{}
	c.detached = false
	c.closed = false
}

// Attach binds the wrapper to an underlying network connection.
func (c *Conn) Attach(netConn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.netConn = netConn
	if netConn != nil {
		c.remoteAddr = netConn.RemoteAddr()
	}
}

// NetConn returns the underlying network connection.
func (c *Conn) NetConn() net.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.netConn
}

// SetSession sets the session for this connection
func (c *Conn) SetSession(sess *session.Session) {
	c.session = sess
}

// Session returns the session associated with this connection
func (c *Conn) Session() *session.Session {
	return c.session
}

// SetData sets user-defined data in the session
func (c *Conn) SetData(data interface{}) {
	if c.session != nil {
		c.session.Data = data
	}
}

// WriteString writes a simple string to the connection
// Thread-safe: acquires mutex for write buffer access
func (c *Conn) WriteString(s string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.writer.AppendString(s)
	c.writeBuf = c.writer.Bytes()
	return nil
}

// WriteBulk writes a bulk string to the connection
// Thread-safe: acquires mutex for write buffer access
func (c *Conn) WriteBulk(b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.writer.AppendBulk(b)
	c.writeBuf = c.writer.Bytes()
	return nil
}

// WriteInt writes an integer to the connection
// Thread-safe: acquires mutex for write buffer access
func (c *Conn) WriteInt(n int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.writer.AppendInt(n)
	c.writeBuf = c.writer.Bytes()
	return nil
}

// WriteArray writes an array header to the connection
// Thread-safe: acquires mutex for write buffer access
func (c *Conn) WriteArray(n int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.writer.AppendArray(n)
	c.writeBuf = c.writer.Bytes()
	return nil
}

// WriteNull writes a null value to the connection
// Thread-safe: acquires mutex for write buffer access
func (c *Conn) WriteNull() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.writer.AppendNull()
	c.writeBuf = c.writer.Bytes()
	return nil
}

// WriteError writes an error message to the connection
// Thread-safe: acquires mutex for write buffer access
func (c *Conn) WriteError(msg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.writer.AppendError(msg)
	c.writeBuf = c.writer.Bytes()
	return nil
}

// WriteAny writes any value to the connection using appropriate RESP type
// Thread-safe: acquires mutex for write buffer access
func (c *Conn) WriteAny(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.writer.AppendAny(v)
	c.writeBuf = c.writer.Bytes()
	return nil
}

// Close closes the connection and releases resources
// Thread-safe: acquires mutex to prevent concurrent close/write
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	netConn := c.netConn
	c.mu.Unlock()

	if netConn != nil {
		return netConn.Close()
	}
	return nil
}

// RemoteAddr returns the remote network address
func (c *Conn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

// Detach detaches the connection from the event loop
// Returns a DetachedConn that the caller must manage
func (c *Conn) Detach() DetachedConn {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.detached = true
	return &detachedConn{conn: c}
}

// Flush writes the buffered RESP response to the underlying connection.
func (c *Conn) Flush() error {
	c.mu.Lock()
	netConn := c.netConn
	c.mu.Unlock()
	if netConn == nil {
		return nil
	}
	if err := c.writer.Flush(netConn); err != nil {
		return err
	}
	c.mu.Lock()
	c.writeBuf = c.writer.Bytes()
	c.mu.Unlock()
	return nil
}

// FD returns the file descriptor for the connection
func (c *Conn) FD() int {
	return c.fd
}

// ReadBuf returns the read buffer for network I/O
func (c *Conn) ReadBuf() []byte {
	return c.readBuf
}

// Parser returns the RESP parser
func (c *Conn) Parser() *resp.Parser {
	return c.parser
}

// Writer returns the RESP writer
func (c *Conn) Writer() *resp.Writer {
	return c.writer
}

// ParseCommands parses commands from the read buffer
// Returns the number of commands parsed
func (c *Conn) ParseCommands() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cmds = c.cmds[:0] // Clear command slice

	for {
		// Reset parser with current read buffer
		// Note: In real implementation, we'd track how much was consumed
		result := c.parser.ParseNextCommand()

		if !result.Complete {
			// Incomplete command, wait for more data
			break
		}

		// Create command with zero-copy slices
		cmd := Command{
			Raw:  result.Raw,
			Args: result.Args,
		}
		c.cmds = append(c.cmds, cmd)
	}

	return len(c.cmds)
}

// Commands returns the parsed commands
func (c *Conn) Commands() []Command {
	return c.cmds
}

// GrowWriteBuf grows the write buffer if needed
// Returns the current write buffer
func (c *Conn) GrowWriteBuf(required int) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	currentCap := cap(c.writeBuf)
	currentLen := len(c.writeBuf)

	// Check if we need to grow
	if currentCap-currentLen < required {
		// Calculate new capacity
		newCap := currentCap * WriteBufferGrowthFactor
		if newCap > MaxWriteBufferSize {
			newCap = MaxWriteBufferSize
		}

		// Check if even max size is insufficient
		if newCap-currentLen < required {
			// Return nil to indicate buffer overflow
			return nil
		}

		// Allocate new buffer and copy
		newBuf := make([]byte, currentLen, newCap)
		copy(newBuf, c.writeBuf)
		c.writeBuf = newBuf
	}

	return c.writeBuf
}

// WriteBuf returns the write buffer
func (c *Conn) WriteBuf() []byte {
	return c.writeBuf
}

// ResetWriteBuf resets the write buffer after flushing
func (c *Conn) ResetWriteBuf() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.writer.Reset()
	c.writeBuf = c.writer.Bytes()
}

// IsClosed returns whether the connection is closed
func (c *Conn) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.closed
}

// IsDetached returns whether the connection is detached
func (c *Conn) IsDetached() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.detached
}

// SetIdleDeadline sets the idle timeout deadline
func (c *Conn) SetIdleDeadline(deadline time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.idleDeadline = deadline
}

// IdleDeadline returns the idle timeout deadline
func (c *Conn) IdleDeadline() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.idleDeadline
}

// detachedConn represents a detached connection managed by the caller
type detachedConn struct {
	conn *Conn
}

// Flush writes the buffered data to the network
func (dc *detachedConn) Flush() error {
	return dc.conn.Flush()
}

// Session returns the session (implements netgo.Conn)
func (dc *detachedConn) Session() *session.Session {
	return dc.conn.Session()
}

// SetData sets user data (implements netgo.Conn)
func (dc *detachedConn) SetData(data interface{}) {
	dc.conn.SetData(data)
}

// WriteString writes a simple string (implements netgo.Conn)
func (dc *detachedConn) WriteString(s string) error {
	return dc.conn.WriteString(s)
}

// WriteBulk writes a bulk string (implements netgo.Conn)
func (dc *detachedConn) WriteBulk(b []byte) error {
	return dc.conn.WriteBulk(b)
}

// WriteInt writes an integer (implements netgo.Conn)
func (dc *detachedConn) WriteInt(n int64) error {
	return dc.conn.WriteInt(n)
}

// WriteArray writes an array header (implements netgo.Conn)
func (dc *detachedConn) WriteArray(n int) error {
	return dc.conn.WriteArray(n)
}

// WriteNull writes a null value (implements netgo.Conn)
func (dc *detachedConn) WriteNull() error {
	return dc.conn.WriteNull()
}

// WriteError writes an error message (implements netgo.Conn)
func (dc *detachedConn) WriteError(msg string) error {
	return dc.conn.WriteError(msg)
}

// WriteAny writes any value (implements netgo.Conn)
func (dc *detachedConn) WriteAny(v interface{}) error {
	return dc.conn.WriteAny(v)
}

// Close closes the connection (implements netgo.Conn)
func (dc *detachedConn) Close() error {
	return dc.conn.Close()
}

// RemoteAddr returns the remote address (implements netgo.Conn)
func (dc *detachedConn) RemoteAddr() net.Addr {
	return dc.conn.RemoteAddr()
}

// Detach is not supported on detached connections
func (dc *detachedConn) Detach() DetachedConn {
	return dc // Already detached
}
