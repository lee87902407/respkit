package dispatcher

import (
	"sync"

	"github.com/lee87902407/respkit/internal/command"
	"github.com/lee87902407/respkit/internal/protocol"
)

// Request is a pending command to be dispatched.
type Request struct {
	SessionID uint64
	Name      string
	Args      [][]byte
	Result    chan protocol.RespValue
}

// Dispatcher routes commands to worker goroutines via a shared queue.
type Dispatcher struct {
	registry *command.Registry
	queue    chan *Request
	workers  int
	wg       sync.WaitGroup
	done     chan struct{}
}

// NewDispatcher creates a new dispatcher.
func NewDispatcher(registry *command.Registry, workers int, queueSize int) *Dispatcher {
	if workers <= 0 {
		workers = 1
	}
	if queueSize <= 0 {
		queueSize = 4096
	}
	return &Dispatcher{
		registry: registry,
		queue:    make(chan *Request, queueSize),
		workers:  workers,
		done:     make(chan struct{}),
	}
}

// Start launches worker goroutines.
func (d *Dispatcher) Start() {
	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
}

// Stop drains the queue and waits for workers to finish.
func (d *Dispatcher) Stop() {
	close(d.done)
	close(d.queue)
	d.wg.Wait()
}

// Enqueue adds a request to the dispatch queue.
// Blocks if the queue is full (natural backpressure).
func (d *Dispatcher) Enqueue(req *Request) error {
	select {
	case d.queue <- req:
		return nil
	case <-d.done:
		return ErrDispatcherStopped
	}
}

// ErrDispatcherStopped is returned when enqueueing on a stopped dispatcher.
var ErrDispatcherStopped = dispatcherError("dispatcher stopped")

type dispatcherError string

func (e dispatcherError) Error() string { return string(e) }

func (d *Dispatcher) worker() {
	defer d.wg.Done()
	for req := range d.queue {
		d.processRequest(req)
	}
}

func (d *Dispatcher) processRequest(req *Request) {
	ctx := &command.Context{
		Name:       req.Name,
		Args:       req.Args,
		SessionID:  req.SessionID,
	}

	factory := d.registry.Lookup(req.Name)
	if factory == nil {
		req.Result <- command.NotFoundError(req.Name)
		return
	}

	cmd, err := factory(req.Name, req.Args)
	if err != nil {
		req.Result <- protocol.Error("ERR " + err.Error())
		return
	}

	result := cmd.Execute(ctx)
	req.Result <- result
}
