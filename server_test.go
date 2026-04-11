package respkit_test

import (
	"bufio"
	"net"
	"testing"
	"time"

	blog "github.com/lee87902407/basekit/log"
	"github.com/lee87902407/basekit/mempool"
	"github.com/lee87902407/respkit"
)

const (
	testWaitTimeout = time.Second
	pollInterval    = 10 * time.Millisecond
)

func startServerAsync(server *respkit.Server, start func() error) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- start()
	}()
	return errCh
}

func waitForServerAddr(t *testing.T, server *respkit.Server, failureMessage string) string {
	t.Helper()

	if addr := server.Addr(); addr != nil {
		return addr.String()
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	timeout := time.NewTimer(testWaitTimeout)
	defer timeout.Stop()

	for {
		select {
		case <-ticker.C:
			if addr := server.Addr(); addr != nil {
				return addr.String()
			}
		case <-timeout.C:
			t.Fatal(failureMessage)
		}
	}
}

func waitForServerExit(t *testing.T, errCh <-chan error, failureMessage string) {
	t.Helper()

	timeout := time.NewTimer(testWaitTimeout)
	defer timeout.Stop()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server exited with error: %v", err)
		}
	case <-timeout.C:
		t.Fatal(failureMessage)
	}
}

func TestNewServerAcceptsTask1ConfigShape(t *testing.T) {
	bytePool := mempool.New(mempool.DefaultOptions())
	logger := blog.L()

	server := respkit.NewServer(&respkit.Config{
		Addr:                  "127.0.0.1:0",
		Network:               "tcp",
		ReadBufferSize:        2048,
		WriteBufferSize:       1024,
		DispatcherWorkers:     3,
		QueueSize:             128,
		MaxInFlightPerSession: 9,
		IdleTimeout:           250 * time.Millisecond,
		MemPool:               bytePool,
		Logger:                logger,
	})

	errCh := startServerAsync(server, server.Start)

	waitForServerAddr(t, server, "server did not start with full task 1 config shape")

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed = %v", err)
	}

	waitForServerExit(t, errCh, "Start() did not return after Stop")
}

func TestServer_StartAndStop(t *testing.T) {
	server := respkit.NewServer(&respkit.Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	})

	errCh := startServerAsync(server, server.Start)

	waitForServerAddr(t, server, "server did not start before Stop")

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed = %v", err)
	}

	waitForServerExit(t, errCh, "Start() did not return after Stop")
}

func TestServer_Close(t *testing.T) {
	server := respkit.NewServer(&respkit.Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	})

	errCh := startServerAsync(server, server.Start)

	waitForServerAddr(t, server, "server did not start before Close")

	if err := server.Close(); err != nil {
		t.Fatalf("Close() failed = %v", err)
	}

	waitForServerExit(t, errCh, "Start() did not return after Close")
}

func TestServer_StopIdempotent(t *testing.T) {
	server := respkit.NewServer(&respkit.Config{Addr: "127.0.0.1:0"})

	errCh := startServerAsync(server, server.Start)

	waitForServerAddr(t, server, "server did not start before first Stop")

	if err := server.Stop(); err != nil {
		t.Fatalf("First Stop() failed = %v", err)
	}

	waitForServerExit(t, errCh, "Start() did not return after first Stop")

	if err := server.Stop(); err != nil {
		t.Fatalf("Second Stop() failed = %v", err)
	}
}

func TestServer_ActiveSessions(t *testing.T) {
	server := respkit.NewServer(&respkit.Config{Addr: "127.0.0.1:0"})

	if count := server.ActiveSessions(); count != 0 {
		t.Errorf("ActiveSessions() = %d, want 0", count)
	}

	errCh := startServerAsync(server, server.Start)

	waitForServerAddr(t, server, "server did not start before Stop")

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed = %v", err)
	}

	waitForServerExit(t, errCh, "Start() did not return after Stop")
}

func TestServer_Addr(t *testing.T) {
	server := respkit.NewServer(&respkit.Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	})

	if addr := server.Addr(); addr != nil {
		t.Errorf("Addr() before start = %v, want nil", addr)
	}

	errCh := startServerAsync(server, server.Start)

	waitForServerAddr(t, server, "Addr() still nil after start")

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed = %v", err)
	}

	waitForServerExit(t, errCh, "Start() did not return after Stop")
}

func TestServer_ListenAndServe(t *testing.T) {
	handler := respkit.NewMux()
	handler.HandleFunc("ping", func(ctx *respkit.Context) error {
		return ctx.Conn.WriteString("PONG")
	})

	server := respkit.NewServer(&respkit.Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	}, handler)

	errCh := startServerAsync(server, server.ListenAndServe)

	addr := waitForServerAddr(t, server, "server did not expose an address in time")

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	conn.Close()

	if err := server.Shutdown(); err != nil {
		t.Fatalf("Shutdown() failed = %v", err)
	}

	waitForServerExit(t, errCh, "ListenAndServe() did not return after Shutdown")
}

func TestServer_HandlerPingPong(t *testing.T) {
	handler := respkit.NewMux()
	handler.HandleFunc("ping", func(ctx *respkit.Context) error {
		return ctx.Conn.WriteString("PONG")
	})

	server := respkit.NewServer(&respkit.Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	}, handler)

	errCh := startServerAsync(server, server.Start)

	addr := waitForServerAddr(t, server, "server did not start in time")

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if line != "+PONG\r\n" {
		t.Fatalf("response = %q, want %q", line, "+PONG\r\n")
	}

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed = %v", err)
	}

	waitForServerExit(t, errCh, "Start() did not return after Stop")
}

func TestServer_DefaultSessionPathRegistersSessionAndRespondsToPing(t *testing.T) {
	server := respkit.NewServer(&respkit.Config{
		Addr:              "127.0.0.1:0",
		Network:           "tcp",
		DispatcherWorkers: 1,
		QueueSize:         8,
	})

	errCh := startServerAsync(server, server.Start)
	addr := waitForServerAddr(t, server, "server did not start in time")

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(testWaitTimeout)
	for server.ActiveSessions() != 1 && time.Now().Before(deadline) {
		time.Sleep(pollInterval)
	}
	if server.ActiveSessions() != 1 {
		t.Fatalf("ActiveSessions() = %d, want 1", server.ActiveSessions())
	}

	if _, err := conn.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		t.Fatalf("failed to write ping: %v", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read ping response: %v", err)
	}
	if line != "+PONG\r\n" {
		t.Fatalf("response = %q, want %q", line, "+PONG\r\n")
	}

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed = %v", err)
	}

	waitForServerExit(t, errCh, "Start() did not return after Stop")
}
