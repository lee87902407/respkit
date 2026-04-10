package dispatcher

import (
	"testing"
	"time"

	"github.com/lee87902407/respkit/internal/command"
	"github.com/lee87902407/respkit/internal/protocol"
)

func TestDispatcherSchedulerMovesIncomingRequestsToReadyQueue(t *testing.T) {
	d := NewDispatcher(command.NewRegistry(), 1, 1)
	d.wg.Add(1)
	go d.schedulerLoop()
	defer close(d.done)

	req := &Request{
		SessionID: 7,
		Name:      "ping",
		Result:    make(chan protocol.RespValue, 1),
	}

	if err := d.Enqueue(req); err != nil {
		t.Fatalf("enqueue request: %v", err)
	}

	select {
	case got := <-d.readyQueue:
		if got != req {
			t.Fatalf("scheduler forwarded %p, want %p", got, req)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for scheduler to forward request")
	}
}

func TestDispatcherExecutesRequestsThroughSchedulerAndWorkers(t *testing.T) {
	registry := command.NewRegistry()
	registry.Register("ping", command.FuncFactory(func(ctx *command.Context) protocol.RespValue {
		if ctx.Name != "ping" {
			t.Fatalf("command context name = %q, want ping", ctx.Name)
		}
		if ctx.SessionID != 42 {
			t.Fatalf("command context session = %d, want 42", ctx.SessionID)
		}
		if len(ctx.Args) != 1 || string(ctx.Args[0]) != "hello" {
			t.Fatalf("command context args = %q, want [hello]", ctx.Args)
		}
		return protocol.BulkFromString("scheduled")
	}))

	d := NewDispatcher(registry, 1, 4)
	d.Start()
	defer d.Stop()

	resultCh := make(chan protocol.RespValue, 1)
	if err := d.Enqueue(&Request{
		SessionID: 42,
		Name:      "ping",
		Args:      [][]byte{[]byte("hello")},
		Result:    resultCh,
	}); err != nil {
		t.Fatalf("enqueue request: %v", err)
	}

	select {
	case got := <-resultCh:
		want := protocol.BulkFromString("scheduled")
		if !got.Equal(want) {
			t.Fatalf("result = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatcher result")
	}
}

func TestDispatcherReturnsNotFoundError(t *testing.T) {
	d := NewDispatcher(command.NewRegistry(), 1, 2)
	d.Start()
	defer d.Stop()

	resultCh := make(chan protocol.RespValue, 1)
	if err := d.Enqueue(&Request{
		SessionID: 11,
		Name:      "missing",
		Result:    resultCh,
	}); err != nil {
		t.Fatalf("enqueue request: %v", err)
	}

	select {
	case got := <-resultCh:
		want := command.NotFoundError("missing")
		if !got.Equal(want) {
			t.Fatalf("result = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for not-found response")
	}
}
