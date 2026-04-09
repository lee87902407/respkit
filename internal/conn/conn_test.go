package conn

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/lee87902407/basekit/mempool"
	"github.com/lee87902407/respkit/internal/protocol"
)

func TestConnReadUsesExternalScopeAndRetainsPipelinedBytes(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	conn := NewConn(server, protocol.NewReader(), protocol.NewWriter(64))
	pool := mempool.New(mempool.DefaultOptions())

	payload := append(
		protocol.SerializeValue(protocol.ArrayOf(protocol.BulkFromString("PING"))),
		protocol.SerializeValue(protocol.ArrayOf(
			protocol.BulkFromString("SET"),
			protocol.BulkFromString("key"),
			protocol.BulkFromString("value"),
		))...,
	)

	writeDone := make(chan error, 1)
	go func() {
		_, err := client.Write(payload)
		writeDone <- err
	}()

	scope1 := mempool.NewScope(pool)
	defer scope1.Close()

	first, err := conn.Read(scope1)
	if err != nil {
		t.Fatalf("Read(first) error = %v", err)
	}

	if !first.Equal(protocol.ArrayOf(protocol.BulkFromString("PING"))) {
		t.Fatalf("Read(first) = %#v", first)
	}

	scope2 := mempool.NewScope(pool)
	defer scope2.Close()

	second, err := conn.Read(scope2)
	if err != nil {
		t.Fatalf("Read(second) error = %v", err)
	}

	wantSecond := protocol.ArrayOf(
		protocol.BulkFromString("SET"),
		protocol.BulkFromString("key"),
		protocol.BulkFromString("value"),
	)
	if !second.Equal(wantSecond) {
		t.Fatalf("Read(second) = %#v, want %#v", second, wantSecond)
	}

	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("client write error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pipelined write to finish")
	}
}

func TestConnWriteBuffersUntilFlush(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	conn := NewConn(server, protocol.NewReader(), protocol.NewWriter(64))
	value := protocol.ArrayOf(
		protocol.BulkFromString("SET"),
		protocol.BulkFromString("key"),
		protocol.Integer(42),
	)
	want := protocol.SerializeValue(value)

	readDone := make(chan []byte, 1)
	go func() {
		buf := make([]byte, len(want))
		_, _ = client.Read(buf)
		readDone <- buf
	}()

	if err := conn.Write(value); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := conn.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	select {
	case got := <-readDone:
		if !bytes.Equal(got, want) {
			t.Fatalf("Flush() wrote %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response bytes")
	}
}
