package main

import (
	"bufio"
	"net"
	"testing"
	"time"

	"github.com/lee87902407/respkit"
)

func TestBasicServerRespondsToPing(t *testing.T) {
	server := respkit.NewServer(&respkit.Config{
		Addr:    "127.0.0.1:0",
		Network: "tcp",
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
		t.Fatal("server did not start in time")
	}

	conn, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}
	if line != "+PONG\r\n" {
		t.Fatalf("response = %q, want %q", line, "+PONG\r\n")
	}

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Start() returned error = %v", err)
	}
}
