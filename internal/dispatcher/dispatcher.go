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

// Dispatcher routes requests through a scheduler before workers execute them.
type Dispatcher struct {
	registry *command.Registry

	incomingQueue chan *Request
	readyQueue    chan *Request
	completeQueue chan *Request
	waitingQueue  chan *Request

	workers int

	done      chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
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
		registry:      registry,
		incomingQueue: make(chan *Request, queueSize),
		readyQueue:    make(chan *Request, queueSize),
		completeQueue: make(chan *Request, queueSize),
		waitingQueue:  make(chan *Request, queueSize),
		workers:       workers,
		done:          make(chan struct{}),
	}
}

// Start launches the scheduler and worker goroutines.
func (d *Dispatcher) Start() {
	d.startOnce.Do(func() {
		d.wg.Add(1)
		go d.schedulerLoop()

		for i := 0; i < d.workers; i++ {
			d.wg.Add(1)
			go d.workerLoop()
		}
	})
}

// Stop stops the scheduler and workers.
func (d *Dispatcher) Stop() {
	d.stopOnce.Do(func() {
		close(d.done)
		d.wg.Wait()
	})
}

// Enqueue adds a request to the scheduler's incoming queue.
// Blocks if the queue is full to provide natural backpressure.
func (d *Dispatcher) Enqueue(req *Request) error {
	select {
	case <-d.done:
		return ErrDispatcherStopped
	default:
	}

	select {
	case d.incomingQueue <- req:
		return nil
	case <-d.done:
		return ErrDispatcherStopped
	}
}

// ErrDispatcherStopped is returned when enqueueing on a stopped dispatcher.
var ErrDispatcherStopped = dispatcherError("dispatcher stopped")

type dispatcherError string

func (e dispatcherError) Error() string { return string(e) }

func (d *Dispatcher) schedulerLoop() {
	defer d.wg.Done()

	for {
		select {
		case <-d.done:
			return
		case req := <-d.incomingQueue:
			if req == nil {
				continue
			}
			select {
			case d.readyQueue <- req:
			case <-d.done:
				return
			}
		case <-d.completeQueue:
			// Completion handling will promote waiting requests in later stages.
		}
	}
}

func (d *Dispatcher) workerLoop() {
	defer d.wg.Done()

	for {
		select {
		case <-d.done:
			return
		case req := <-d.readyQueue:
			if req == nil {
				continue
			}
			d.processRequest(req)
			select {
			case d.completeQueue <- req:
			case <-d.done:
				return
			default:
			}
		}
	}
}

func (d *Dispatcher) processRequest(req *Request) {
	ctx := &command.Context{
		Name:      req.Name,
		Args:      req.Args,
		SessionID: req.SessionID,
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
