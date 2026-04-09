package respkit_test

import (
	"bufio"
	"net"
	"testing"
	"time"

	"github.com/lee87902407/respkit"
)

func TestNewServerAcceptsTask1ConfigShape(t *testing.T) {
	server := respkit.NewServer(&respkit.Config{
		Addr:                  "127.0.0.1:0",
		DispatcherWorkers:     3,
		QueueSize:             128,
		MaxInFlightPerSession: 9,
	})

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
		t.Fatal("server did not start with task 1 config shape")
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

func TestServer_StartAndStop(t *testing.T) {
	server := respkit.NewServer(&respkit.Config{
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
	server := respkit.NewServer(&respkit.Config{
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
	server := respkit.NewServer(&respkit.Config{Addr: "127.0.0.1:0"})

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
	server := respkit.NewServer(&respkit.Config{Addr: "127.0.0.1:0"})

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
	server := respkit.NewServer(&respkit.Config{
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
	handler := respkit.NewMux()
	handler.HandleFunc("ping", func(ctx *respkit.Context) error {
		return ctx.Conn.WriteString("PONG")
	})

	server := respkit.NewServer(&respkit.Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	}, handler)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

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
	handler := respkit.NewMux()
	handler.HandleFunc("ping", func(ctx *respkit.Context) error {
		return ctx.Conn.WriteString("PONG")
	})

	server := respkit.NewServer(&respkit.Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
	}, handler)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

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

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() failed = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after Stop")
	}
}
