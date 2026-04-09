package respkit

import (
	"bufio"
	"net"
	"testing"
	"time"
)

func TestDefaultConfigPreservesDispatcherSettings(t *testing.T) {
	cfg := defaultConfig(&Config{
		DispatcherWorkers:     3,
		QueueSize:             128,
		MaxInFlightPerSession: 9,
	})

	if cfg.DispatcherWorkers != 3 {
		t.Fatalf("DispatcherWorkers = %d, want %d", cfg.DispatcherWorkers, 3)
	}
	if cfg.QueueSize != 128 {
		t.Fatalf("QueueSize = %d, want %d", cfg.QueueSize, 128)
	}
	if cfg.MaxInFlightPerSession != 9 {
		t.Fatalf("MaxInFlightPerSession = %d, want %d", cfg.MaxInFlightPerSession, 9)
	}
}

func TestServer_StartAndStop(t *testing.T) {
	server := NewServer(&Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed = %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() failed = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after Stop")
	}
}

func TestServer_Close(t *testing.T) {
	server := NewServer(&Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	if err := server.Close(); err != nil {
		t.Fatalf("Close() failed = %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() failed = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after Close")
	}
}

func TestServer_StopIdempotent(t *testing.T) {
	server := NewServer(&Config{Addr: "127.0.0.1:0"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	if err := server.Stop(); err != nil {
		t.Fatalf("First Stop() failed = %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() failed = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after first Stop")
	}

	if err := server.Stop(); err != nil {
		t.Fatalf("Second Stop() failed = %v", err)
	}
}

func TestServer_ActiveSessions(t *testing.T) {
	server := NewServer(&Config{Addr: "127.0.0.1:0"})

	if count := server.ActiveSessions(); count != 0 {
		t.Errorf("ActiveSessions() = %d, want 0", count)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed = %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() failed = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after Stop")
	}
}

func TestServer_Addr(t *testing.T) {
	server := NewServer(&Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	})

	if addr := server.Addr(); addr != nil {
		t.Errorf("Addr() before start = %v, want nil", addr)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if server.Addr() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if server.Addr() == nil {
		t.Fatal("Addr() still nil after start")
	}

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() failed = %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() failed = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after Stop")
	}
}

func TestServer_ListenAndServe(t *testing.T) {
	handler := HandlerFunc(func(ctx *Context) error {
		return nil
	})

	server := NewServer(&Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	}, handler)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	// Wait for listener and connect.
	deadline := time.Now().Add(time.Second)
	var addr string
	for time.Now().Before(deadline) {
		if a := server.Addr(); a != nil {
			addr = a.String()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("server did not expose an address in time")
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	conn.Close()

	if err := server.Shutdown(); err != nil {
		t.Fatalf("Shutdown() failed = %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ListenAndServe() failed = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ListenAndServe() did not return after Shutdown")
	}
}

func TestServer_HandlerPingPong(t *testing.T) {
	handler := HandlerFunc(func(ctx *Context) error {
		return ctx.Conn.WriteString("PONG")
	})

	server := NewServer(&Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	}, handler)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	// Wait for listener.
	deadline := time.Now().Add(time.Second)
	var addr string
	for time.Now().Before(deadline) {
		if a := server.Addr(); a != nil {
			addr = a.String()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("server did not start in time")
	}

	// Connect and send PING.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send RESP PING command.
	_, err = conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Read response.
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

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() failed = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after Stop")
	}
}
