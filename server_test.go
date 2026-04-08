package respkit

import (
	"net"
	"testing"
	"time"
)

func TestServer_ListenAndServe(t *testing.T) {
	// Create a test handler
	handler := HandlerFunc(func(ctx *Context) error {
		return nil
	})

	// Create server with random port
	config := &Config{
		Addr:    "127.0.0.1:0", // Random port
		Network: "tcp",
	}

	server := NewServer(config, handler)

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	// Wait for server to start
	var addr string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if listenerAddr := server.Addr(); listenerAddr != nil {
			addr = listenerAddr.String()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("server did not expose an address in time")
	}

	// Test connection
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	conn.Close()

	// Shutdown server
	if err := server.Shutdown(); err != nil {
		t.Fatalf("Shutdown() failed = %v", err)
	}

	// Wait for ListenAndServe to return
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ListenAndServe() failed = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ListenAndServe() did not return after Shutdown")
	}
}

func TestServer_ListenAndServe_Unix(t *testing.T) {
	// Create a test handler
	handler := HandlerFunc(func(ctx *Context) error {
		return nil
	})

	// Create server with unix socket
	socketPath := "/tmp/test-respkit-server.sock"
	config := &Config{
		Addr:    socketPath,
		Network: "unix",
	}

	server := NewServer(config, handler)

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if server.Addr() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if server.Addr() == nil {
		t.Fatal("server did not start unix listener in time")
	}

	// Test connection
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	conn.Close()

	// Shutdown server
	if err := server.Shutdown(); err != nil {
		t.Fatalf("Shutdown() failed = %v", err)
	}

	// Wait for ListenAndServe to return
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ListenAndServe() failed = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ListenAndServe() did not return after Shutdown")
	}
}
