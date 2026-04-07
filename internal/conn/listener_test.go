package conn

import (
	"net"
	"os"
	"testing"
	"time"
)

func TestListen(t *testing.T) {
	tests := []struct {
		name    string
		network string
		addr    string
	}{
		{"TCP localhost", "tcp", "127.0.0.1:0"},
		{"TCP4", "tcp4", "127.0.0.1:0"},
		{"Unix socket", "unix", "/tmp/test-listener.sock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up unix socket file
			if tt.network == "unix" {
				os.Remove(tt.addr)
			}

			ln, err := Listen(tt.network, tt.addr)
			if err != nil {
				t.Fatalf("Listen() failed = %v", err)
			}
			defer ln.Close()

			if ln.Addr() == nil {
				t.Error("Listener address is nil")
			}

			// Verify we can accept a connection
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					t.Errorf("Accept() failed = %v", err)
					return
				}
				conn.Close()
			}()

			// Connect to the listener
			d := net.Dialer{Timeout: time.Second}
			conn, err := d.Dial(tt.network, ln.Addr().String())
			if err != nil {
				t.Fatalf("Dial() failed = %v", err)
			}
			conn.Close()

			// Clean up unix socket file
			if tt.network == "unix" {
				os.Remove(tt.addr)
			}
		})
	}
}

func TestListenTLS(t *testing.T) {
	// TLS listener test - minimal config
	ln, err := ListenTLS("tcp", "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTLS() failed = %v", err)
	}
	defer ln.Close()

	if ln.Addr() == nil {
		t.Error("TLS listener address is nil")
	}
}
