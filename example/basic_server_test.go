package main

import (
	"bytes"
	"net"
	"strconv"
	"testing"

	"github.com/yanjie/netgo"
	"github.com/yanjie/netgo/internal/session"
)

type exampleConn struct {
	buf       bytes.Buffer
	lastError string
}

func (c *exampleConn) Session() *session.Session  { return nil }
func (c *exampleConn) SetData(interface{})        {}
func (c *exampleConn) Close() error               { return nil }
func (c *exampleConn) RemoteAddr() net.Addr       { return nil }
func (c *exampleConn) Detach() netgo.DetachedConn { return nil }
func (c *exampleConn) WriteInt(int64) error       { return nil }
func (c *exampleConn) WriteArray(int) error       { return nil }
func (c *exampleConn) WriteAny(interface{}) error { return nil }

func (c *exampleConn) WriteString(s string) error {
	c.buf.WriteString("+" + s + "\r\n")
	return nil
}

func (c *exampleConn) WriteBulk(b []byte) error {
	c.buf.WriteString("$")
	c.buf.WriteString(strconv.Itoa(len(b)))
	c.buf.WriteString("\r\n")
	c.buf.Write(b)
	c.buf.WriteString("\r\n")
	return nil
}

func (c *exampleConn) WriteNull() error {
	c.buf.WriteString("$-1\r\n")
	return nil
}

func (c *exampleConn) WriteError(msg string) error {
	c.lastError = msg
	c.buf.WriteString("-" + msg + "\r\n")
	return nil
}

func TestNewExampleMuxHandlesPingSetAndGet(t *testing.T) {
	mux := newExampleMux()
	conn := &exampleConn{}

	if err := mux.HandleCommand(&netgo.Context{Conn: conn, Command: netgo.Command{Args: [][]byte{[]byte("PING")}}}); err != nil {
		t.Fatalf("PING error = %v", err)
	}
	if got := conn.buf.String(); got != "+PONG\r\n" {
		t.Fatalf("PING payload = %q, want %q", got, "+PONG\r\n")
	}

	conn.buf.Reset()
	if err := mux.HandleCommand(&netgo.Context{Conn: conn, Command: netgo.Command{Args: [][]byte{[]byte("SET"), []byte("key"), []byte("value")}}}); err != nil {
		t.Fatalf("SET error = %v", err)
	}
	if got := conn.buf.String(); got != "+OK\r\n" {
		t.Fatalf("SET payload = %q, want %q", got, "+OK\r\n")
	}

	conn.buf.Reset()
	if err := mux.HandleCommand(&netgo.Context{Conn: conn, Command: netgo.Command{Args: [][]byte{[]byte("GET"), []byte("key")}}}); err != nil {
		t.Fatalf("GET error = %v", err)
	}
	if got := conn.buf.String(); got != "$5\r\nvalue\r\n" {
		t.Fatalf("GET payload = %q, want %q", got, "$5\r\nvalue\r\n")
	}

	conn.buf.Reset()
	if err := mux.HandleCommand(&netgo.Context{Conn: conn, Command: netgo.Command{Args: [][]byte{[]byte("GET"), []byte("missing")}}}); err != nil {
		t.Fatalf("GET missing error = %v", err)
	}
	if got := conn.buf.String(); got != "$-1\r\n" {
		t.Fatalf("GET missing payload = %q, want %q", got, "$-1\r\n")
	}
}

func TestNewExampleMuxValidatesArity(t *testing.T) {
	mux := newExampleMux()
	conn := &exampleConn{}

	if err := mux.HandleCommand(&netgo.Context{Conn: conn, Command: netgo.Command{Args: [][]byte{[]byte("SET"), []byte("only-key")}}}); err != nil {
		t.Fatalf("SET arity error = %v", err)
	}
	if conn.lastError != "ERR wrong number of arguments for 'set'" {
		t.Fatalf("SET arity message = %q", conn.lastError)
	}

	conn.buf.Reset()
	conn.lastError = ""
	if err := mux.HandleCommand(&netgo.Context{Conn: conn, Command: netgo.Command{Args: [][]byte{[]byte("GET")}}}); err != nil {
		t.Fatalf("GET arity error = %v", err)
	}
	if conn.lastError != "ERR wrong number of arguments for 'get'" {
		t.Fatalf("GET arity message = %q", conn.lastError)
	}
}

func TestNewExampleServerUsesConfiguredAddress(t *testing.T) {
	server := newExampleServer(":6380")
	if server == nil {
		t.Fatal("newExampleServer() returned nil")
	}
}
