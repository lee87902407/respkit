package netgo

import (
	"bytes"
	"net"
	"strconv"
	"testing"

	"github.com/yanjie/netgo/internal/session"
)

type stubAddr string

func (a stubAddr) Network() string { return "test" }
func (a stubAddr) String() string  { return string(a) }

type stubDetachedConn struct {
	buf    bytes.Buffer
	closed bool
	addr   net.Addr
}

func (c *stubDetachedConn) Session() *session.Session { return nil }
func (c *stubDetachedConn) SetData(interface{})       {}

func (c *stubDetachedConn) WriteString(s string) error {
	c.buf.WriteString("+" + s + "\r\n")
	return nil
}

func (c *stubDetachedConn) WriteBulk(b []byte) error {
	c.buf.WriteString("$")
	c.buf.WriteString(strconv.Itoa(len(b)))
	c.buf.WriteString("\r\n")
	c.buf.Write(b)
	c.buf.WriteString("\r\n")
	return nil
}

func (c *stubDetachedConn) WriteInt(n int64) error {
	c.buf.WriteString(":" + strconv.FormatInt(n, 10) + "\r\n")
	return nil
}

func (c *stubDetachedConn) WriteArray(n int) error {
	c.buf.WriteString("*" + strconv.Itoa(n) + "\r\n")
	return nil
}

func (c *stubDetachedConn) WriteNull() error {
	c.buf.WriteString("$-1\r\n")
	return nil
}

func (c *stubDetachedConn) WriteError(msg string) error {
	c.buf.WriteString("-" + msg + "\r\n")
	return nil
}

func (c *stubDetachedConn) WriteAny(interface{}) error {
	_, _ = c.buf.WriteString("any")
	return nil
}

func (c *stubDetachedConn) Close() error {
	c.closed = true
	return nil
}

func (c *stubDetachedConn) RemoteAddr() net.Addr { return c.addr }
func (c *stubDetachedConn) Detach() DetachedConn { return c }
func (c *stubDetachedConn) Flush() error         { return nil }

type stubConn struct {
	detached *stubDetachedConn
}

func (c *stubConn) Session() *session.Session  { return nil }
func (c *stubConn) SetData(interface{})        {}
func (c *stubConn) WriteString(string) error   { return nil }
func (c *stubConn) WriteBulk([]byte) error     { return nil }
func (c *stubConn) WriteInt(int64) error       { return nil }
func (c *stubConn) WriteArray(int) error       { return nil }
func (c *stubConn) WriteNull() error           { return nil }
func (c *stubConn) WriteError(string) error    { return nil }
func (c *stubConn) WriteAny(interface{}) error { return nil }
func (c *stubConn) Close() error               { return nil }
func (c *stubConn) RemoteAddr() net.Addr       { return stubAddr("source") }
func (c *stubConn) Detach() DetachedConn       { return c.detached }

func TestPubSubPublishAndUnsubscribe(t *testing.T) {
	ps := NewPubSub()
	client := &stubDetachedConn{addr: stubAddr("subscriber")}
	conn := &stubConn{detached: client}

	if err := ps.Subscribe(conn, "jobs"); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if got := client.buf.String(); got != "*3\r\n$9\r\nsubscribe\r\n$4\r\njobs\r\n:1\r\n" {
		t.Fatalf("Subscribe() payload = %q", got)
	}

	client.buf.Reset()
	sent, err := ps.Publish("jobs", "ready")
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if sent != 1 {
		t.Fatalf("Publish() sent = %d, want 1", sent)
	}
	if got := client.buf.String(); got != "*3\r\n$7\r\nmessage\r\n$4\r\njobs\r\n$5\r\nready\r\n" {
		t.Fatalf("Publish() payload = %q", got)
	}

	client.buf.Reset()
	if err := ps.Unsubscribe(conn, "jobs"); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	if got := client.buf.String(); got != "*3\r\n$11\r\nunsubscribe\r\n$4\r\njobs\r\n:0\r\n" {
		t.Fatalf("Unsubscribe() payload = %q", got)
	}
}

func TestPubSubPatternSubscribeAndPublish(t *testing.T) {
	ps := NewPubSub()
	client := &stubDetachedConn{addr: stubAddr("pattern-subscriber")}
	conn := &stubConn{detached: client}

	if err := ps.PSubscribe(conn, "jobs:*"); err != nil {
		t.Fatalf("PSubscribe() error = %v", err)
	}
	if got := client.buf.String(); got != "*3\r\n$10\r\npsubscribe\r\n$6\r\njobs:*\r\n:1\r\n" {
		t.Fatalf("PSubscribe() payload = %q", got)
	}

	client.buf.Reset()
	sent, err := ps.Publish("jobs:1", "ready")
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if sent != 1 {
		t.Fatalf("Publish() sent = %d, want 1", sent)
	}
	if got := client.buf.String(); got != "*4\r\n$8\r\npmessage\r\n$6\r\njobs:*\r\n$6\r\njobs:1\r\n$5\r\nready\r\n" {
		t.Fatalf("Publish() pattern payload = %q", got)
	}

	client.buf.Reset()
	if err := ps.PUnsubscribe(conn, "jobs:*"); err != nil {
		t.Fatalf("PUnsubscribe() error = %v", err)
	}
	if got := client.buf.String(); got != "*3\r\n$12\r\npunsubscribe\r\n$6\r\njobs:*\r\n:0\r\n" {
		t.Fatalf("PUnsubscribe() payload = %q", got)
	}
}
