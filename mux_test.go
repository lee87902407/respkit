package respkit

import (
	"net"
	"strings"
	"testing"

	"github.com/lee87902407/respkit/internal/session"
)

type muxTestConn struct {
	lastError string
}

func (c *muxTestConn) Session() *session.Session   { return nil }
func (c *muxTestConn) SetData(interface{})         {}
func (c *muxTestConn) WriteString(string) error    { return nil }
func (c *muxTestConn) WriteBulk([]byte) error      { return nil }
func (c *muxTestConn) WriteInt(int64) error        { return nil }
func (c *muxTestConn) WriteArray(int) error        { return nil }
func (c *muxTestConn) WriteNull() error            { return nil }
func (c *muxTestConn) WriteError(msg string) error { c.lastError = msg; return nil }
func (c *muxTestConn) WriteAny(interface{}) error  { return nil }
func (c *muxTestConn) Close() error                { return nil }
func (c *muxTestConn) RemoteAddr() net.Addr        { return nil }
func (c *muxTestConn) Detach() DetachedConn        { return nil }

func TestMuxDispatchesKnownAndUnknownCommands(t *testing.T) {
	mux := NewMux()
	called := false
	mux.HandleFunc("ping", func(ctx *Context) error {
		called = true
		return nil
	})

	ctx := &Context{Command: Command{Args: [][]byte{[]byte("PING")}}}
	if err := mux.Handle(ctx); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !called {
		t.Fatal("expected registered handler to be called")
	}
}

func TestMuxUnknownCommandUsesDefaultHandler(t *testing.T) {
	mux := NewMux()
	conn := &muxTestConn{}
	ctx := &Context{Conn: conn, Command: Command{Args: [][]byte{[]byte("NOPE")}}}

	if err := mux.Handle(ctx); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got, want := conn.lastError, "ERR unknown command 'nope'"; got != want {
		t.Fatalf("default error = %q, want %q", got, want)
	}
}

func TestMuxSetNotFoundOverridesDefault(t *testing.T) {
	mux := NewMux()
	mux.SetNotFound(HandlerFunc(func(ctx *Context) error {
		return ctx.Conn.WriteError("custom not found")
	}))
	conn := &muxTestConn{}
	ctx := &Context{Conn: conn, Command: Command{Args: [][]byte{[]byte("MISS")}}}

	if err := mux.HandleCommand(ctx); err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}
	if conn.lastError != "custom not found" {
		t.Fatalf("custom not found error = %q", conn.lastError)
	}
}

func TestMuxRegisterPanicsOnNilHandler(t *testing.T) {
	mux := NewMux()
	defer func() {
		r, ok := recover().(string)
		if !ok {
			t.Fatal("Register() should panic with a string")
		}
		if !strings.Contains(r, "nil handler") {
			t.Fatalf("panic = %q, want nil handler", r)
		}
	}()
	mux.Register("ping", nil)
}
