package dispatcher

import (
	"sync"
	"testing"
	"time"

	"github.com/lee87902407/respkit/internal/command"
	"github.com/lee87902407/respkit/internal/protocol"
)

func TestDispatcherSchedulerMovesIncomingRequestsToReadyQueue(t *testing.T) {
	d := NewDispatcher(command.NewRegistry(), 1, 1)
	d.wg.Add(1)
	go d.schedulerLoop()
	defer func() {
		close(d.incomingQueue)
		d.wg.Wait()
	}()

	req := &Request{
		SessionID: 7,
		Name:      "ping",
		Result:    make(chan protocol.RespValue, 1),
	}

	d.enqueueWG.Add(1)
	go func() {
		defer d.enqueueWG.Done()
		d.incomingQueue <- req
	}()

	select {
	case got := <-d.readyQueue:
		if got != req {
			t.Fatalf("scheduler forwarded %p, want %p", got, req)
		}
		d.completeQueue <- req
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

func TestDispatcherStopDrainsQueuedWork(t *testing.T) {
	registry := command.NewRegistry()
	registry.Register("ping", command.FuncFactory(func(ctx *command.Context) protocol.RespValue {
		return protocol.SimpleString("PONG")
	}))

	d := NewDispatcher(registry, 1, 2)
	d.Start()

	resultCh := make(chan protocol.RespValue, 1)
	if err := d.Enqueue(&Request{Name: "ping", Result: resultCh}); err != nil {
		t.Fatalf("enqueue request: %v", err)
	}

	d.Stop()

	select {
	case got := <-resultCh:
		want := protocol.SimpleString("PONG")
		if !got.Equal(want) {
			t.Fatalf("result = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for drained request result")
	}
}

func TestDispatcherRejectsNilResultChannel(t *testing.T) {
	d := NewDispatcher(command.NewRegistry(), 1, 2)

	err := d.Enqueue(&Request{Name: "ping", Result: nil})
	if err != ErrResultUnavailable {
		t.Fatalf("Enqueue() error = %v, want %v", err, ErrResultUnavailable)
	}
}

func TestDispatcherRejectsFullResultChannel(t *testing.T) {
	d := NewDispatcher(command.NewRegistry(), 1, 2)
	full := make(chan protocol.RespValue, 1)
	full <- protocol.SimpleString("occupied")

	err := d.Enqueue(&Request{Name: "ping", Result: full})
	if err != ErrResultUnavailable {
		t.Fatalf("Enqueue() error = %v, want %v", err, ErrResultUnavailable)
	}
}

func TestDispatcherHandlesQueueSaturation(t *testing.T) {
	registry := command.NewRegistry()
	registry.Register("ping", command.FuncFactory(func(ctx *command.Context) protocol.RespValue {
		return protocol.SimpleString("PONG")
	}))

	d := NewDispatcher(registry, 1, 1)
	d.Start()
	defer d.Stop()

	results := []chan protocol.RespValue{
		make(chan protocol.RespValue, 1),
		make(chan protocol.RespValue, 1),
		make(chan protocol.RespValue, 1),
	}

	for i := range results {
		if err := d.Enqueue(&Request{Name: "ping", Result: results[i]}); err != nil {
			t.Fatalf("enqueue request %d: %v", i, err)
		}
	}

	for i, resultCh := range results {
		select {
		case got := <-resultCh:
			want := protocol.SimpleString("PONG")
			if !got.Equal(want) {
				t.Fatalf("result %d = %#v, want %#v", i, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for result %d", i)
		}
	}
}

func TestDispatcherConcurrentStopUnblocksEnqueue(t *testing.T) {
	registry := command.NewRegistry()
	registry.Register("ping", command.FuncFactory(func(ctx *command.Context) protocol.RespValue {
		return protocol.SimpleString("PONG")
	}))

	d := NewDispatcher(registry, 1, 1)
	d.Start()

	first := make(chan protocol.RespValue, 1)
	second := make(chan protocol.RespValue, 1)
	third := make(chan protocol.RespValue, 1)
	fourth := make(chan protocol.RespValue, 1)

	for _, resultCh := range []chan protocol.RespValue{first, second, third} {
		if err := d.Enqueue(&Request{Name: "ping", Result: resultCh}); err != nil {
			t.Fatalf("prefill enqueue failed: %v", err)
		}
	}

	enqueueErr := make(chan error, 1)
	go func() {
		enqueueErr <- d.Enqueue(&Request{Name: "ping", Result: fourth})
	}()

	var stopWG sync.WaitGroup
	stopWG.Add(1)
	go func() {
		defer stopWG.Done()
		d.Stop()
	}()

	select {
	case err := <-enqueueErr:
		if err != nil && err != ErrDispatcherStopped {
			t.Fatalf("Enqueue() error = %v, want nil or ErrDispatcherStopped", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocked enqueue to complete")
	}

	stopWG.Wait()
}
