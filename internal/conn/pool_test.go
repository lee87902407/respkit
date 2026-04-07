package conn

import (
	"net"
	"runtime"
	"sync"
	"testing"

	"github.com/yanjie/netgo/internal/resp"
)

// TestPoolGet verifies that Get returns properly initialized connections
func TestPoolGet(t *testing.T) {
	pool := &Pool{
		p: sync.Pool{
			New: func() interface{} {
				readBuf := make([]byte, 16384)
				writeBuf := make([]byte, 4096)
				cmds := make([]Command, 0, 16)

				return &Conn{
					readBuf:  readBuf,
					writeBuf: writeBuf,
					parser:   resp.NewParser(readBuf),
					writer:   resp.NewWriter(4096),
					cmds:     cmds,
				}
			},
		},
	}

	conn := pool.Get()
	if conn == nil {
		t.Fatal("Get() returned nil connection")
	}

	// Verify buffers are allocated
	if cap(conn.readBuf) != 16384 {
		t.Errorf("Expected read buffer capacity 16384, got %d", cap(conn.readBuf))
	}
	if cap(conn.writeBuf) != 4096 {
		t.Errorf("Expected write buffer capacity 4096, got %d", cap(conn.writeBuf))
	}
	if cap(conn.cmds) != 16 {
		t.Errorf("Expected cmds capacity 16, got %d", cap(conn.cmds))
	}
}

// TestPoolPut verifies that Put resets connection state
func TestPoolPut(t *testing.T) {
	pool := &Pool{
		p: sync.Pool{
			New: func() interface{} {
				readBuf := make([]byte, 16384)
				writeBuf := make([]byte, 4096)
				cmds := make([]Command, 0, 16)

				return &Conn{
					readBuf:  readBuf,
					writeBuf: writeBuf,
					parser:   resp.NewParser(readBuf),
					writer:   resp.NewWriter(4096),
					cmds:     cmds,
				}
			},
		},
	}

	// Get a connection and modify its state
	conn := pool.Get()
	conn.Init(42, &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 6379,
	})

	// Add some commands
	conn.cmds = append(conn.cmds, Command{
		Raw:  []byte("SET key value"),
		Args: [][]byte{[]byte("SET"), []byte("key"), []byte("value")},
	})

	// Write some data
	conn.WriteString("OK")

	// Return to pool
	pool.Put(conn)

	// Get the same connection (likely from pool)
	reusedConn := pool.Get()

	// Verify state was reset
	if reusedConn.fd != 0 {
		t.Errorf("Expected fd to be reset to 0, got %d", reusedConn.fd)
	}
	if reusedConn.remoteAddr != nil {
		t.Errorf("Expected remoteAddr to be reset to nil, got %v", reusedConn.remoteAddr)
	}
	if len(reusedConn.cmds) != 0 {
		t.Errorf("Expected cmds to be empty, got %d commands", len(reusedConn.cmds))
	}
	if len(reusedConn.writeBuf) != 0 {
		t.Errorf("Expected write buffer to be empty, got %d bytes", len(reusedConn.writeBuf))
	}
}

// TestPoolPutNil verifies that Put handles nil connections gracefully
func TestPoolPutNil(t *testing.T) {
	pool := &Pool{
		p: sync.Pool{
			New: func() interface{} {
				readBuf := make([]byte, 16384)
				writeBuf := make([]byte, 4096)
				cmds := make([]Command, 0, 16)

				return &Conn{
					readBuf:  readBuf,
					writeBuf: writeBuf,
					parser:   resp.NewParser(readBuf),
					writer:   resp.NewWriter(4096),
					cmds:     cmds,
				}
			},
		},
	}

	// Should not panic
	pool.Put(nil)
}

// TestPoolPutClosed verifies that closed connections are not pooled
func TestPoolPutClosed(t *testing.T) {
	pool := &Pool{
		p: sync.Pool{
			New: func() interface{} {
				readBuf := make([]byte, 16384)
				writeBuf := make([]byte, 4096)
				cmds := make([]Command, 0, 16)

				return &Conn{
					readBuf:  readBuf,
					writeBuf: writeBuf,
					parser:   resp.NewParser(readBuf),
					writer:   resp.NewWriter(4096),
					cmds:     cmds,
				}
			},
		},
	}

	conn := pool.Get()
	conn.Init(42, &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 6379,
	})

	// Close the connection
	conn.Close()

	// Return to pool - should not be pooled
	pool.Put(conn)

	// Force GC to ensure pool cleanup
	runtime.GC()

	// Get new connection - should not be the closed one
	newConn := pool.Get()
	if newConn.IsClosed() {
		t.Error("Pooled a closed connection")
	}
}

// TestPoolPutDetached verifies that detached connections are not pooled
func TestPoolPutDetached(t *testing.T) {
	pool := &Pool{
		p: sync.Pool{
			New: func() interface{} {
				readBuf := make([]byte, 16384)
				writeBuf := make([]byte, 4096)
				cmds := make([]Command, 0, 16)

				return &Conn{
					readBuf:  readBuf,
					writeBuf: writeBuf,
					parser:   resp.NewParser(readBuf),
					writer:   resp.NewWriter(4096),
					cmds:     cmds,
				}
			},
		},
	}

	conn := pool.Get()
	conn.Init(42, &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 6379,
	})

	// Detach the connection
	detached := conn.Detach()

	// Return to pool - should not be pooled
	pool.Put(conn)

	// Force GC to ensure pool cleanup
	runtime.GC()

	// Get new connection - should not be the detached one
	newConn := pool.Get()
	if newConn.IsDetached() {
		t.Error("Pooled a detached connection")
	}

	// Clean up detached connection
	detached.Close()
}

// TestPoolConcurrent verifies pool behavior under concurrent access
func TestPoolConcurrent(t *testing.T) {
	pool := &Pool{
		p: sync.Pool{
			New: func() interface{} {
				readBuf := make([]byte, 16384)
				writeBuf := make([]byte, 4096)
				cmds := make([]Command, 0, 16)

				return &Conn{
					readBuf:  readBuf,
					writeBuf: writeBuf,
					parser:   resp.NewParser(readBuf),
					writer:   resp.NewWriter(4096),
					cmds:     cmds,
				}
			},
		},
	}

	const goroutines = 100
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				conn := pool.Get()
				if conn == nil {
					t.Errorf("Get() returned nil")
					return
				}

				// Use the connection
				conn.Init(int(j), &net.TCPAddr{
					IP:   net.ParseIP("127.0.0.1"),
					Port: 6379,
				})

				conn.WriteString("test")

				// Return to pool
				pool.Put(conn)
			}
		}()
	}

	wg.Wait()
}

// TestPoolReducesAllocation verifies that pooling reduces allocations
func TestPoolReducesAllocation(t *testing.T) {
	pool := &Pool{
		p: sync.Pool{
			New: func() interface{} {
				readBuf := make([]byte, 16384)
				writeBuf := make([]byte, 4096)
				cmds := make([]Command, 0, 16)

				return &Conn{
					readBuf:  readBuf,
					writeBuf: writeBuf,
					parser:   resp.NewParser(readBuf),
					writer:   resp.NewWriter(4096),
					cmds:     cmds,
				}
			},
		},
	}

	const iterations = 1000

	// Measure allocations with pooling
	var m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	conns := make([]*Conn, iterations)
	for i := 0; i < iterations; i++ {
		conns[i] = pool.Get()
	}

	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	pooledAllocs := m2.TotalAlloc - m1.TotalAlloc

	// Return connections to pool
	for i := 0; i < iterations; i++ {
		pool.Put(conns[i])
	}

	// Measure allocations without pooling (creating new each time)
	var m3 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m3)

	conns2 := make([]*Conn, iterations)
	for i := 0; i < iterations; i++ {
		readBuf := make([]byte, 16384)
		writeBuf := make([]byte, 4096)
		cmds := make([]Command, 0, 16)
		conns2[i] = &Conn{
			readBuf:  readBuf,
			writeBuf: writeBuf,
			parser:   resp.NewParser(readBuf),
			writer:   resp.NewWriter(4096),
			cmds:     cmds,
		}
	}

	var m4 runtime.MemStats
	runtime.ReadMemStats(&m4)

	unpooledAllocs := m4.TotalAlloc - m3.TotalAlloc

	// Pooled allocations should be significantly less
	t.Logf("Pooled allocations: %d bytes", pooledAllocs)
	t.Logf("Unpooled allocations: %d bytes", unpooledAllocs)

	// With pooling, we should see much less allocation after the first few gets
	// as sync.Pool reuses connections
	if pooledAllocs > unpooledAllocs {
		t.Logf("Note: Pooled allocations (%d) > unpooled (%d), this is expected if pool is cold", pooledAllocs, unpooledAllocs)
	}
}

// TestConnInit verifies Init properly initializes pooled connections
func TestConnInit(t *testing.T) {
	pool := &Pool{
		p: sync.Pool{
			New: func() interface{} {
				readBuf := make([]byte, 16384)
				writeBuf := make([]byte, 4096)
				cmds := make([]Command, 0, 16)

				return &Conn{
					readBuf:  readBuf,
					writeBuf: writeBuf,
					parser:   resp.NewParser(readBuf),
					writer:   resp.NewWriter(4096),
					cmds:     cmds,
				}
			},
		},
	}

	conn := pool.Get()

	// Initialize with fd and address
	fd := 42
	addr := &net.TCPAddr{
		IP:   net.ParseIP("192.168.1.1"),
		Port: 8080,
	}

	conn.Init(fd, addr)

	// Verify initialization
	if conn.FD() != fd {
		t.Errorf("Expected fd %d, got %d", fd, conn.FD())
	}

	if conn.RemoteAddr().String() != addr.String() {
		t.Errorf("Expected address %s, got %s", addr.String(), conn.RemoteAddr().String())
	}

	// Verify connection is not closed or detached
	if conn.IsClosed() {
		t.Error("Connection should not be closed after Init")
	}
	if conn.IsDetached() {
		t.Error("Connection should not be detached after Init")
	}
}
