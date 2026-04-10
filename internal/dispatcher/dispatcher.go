package dispatcher

import (
	"errors"
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
	waitingQueue  []*Request
	queueSize     int

	workers int

	enqueueDone chan struct{}
	stopped     bool
	mu          sync.Mutex
	enqueueWG   sync.WaitGroup
	startOnce   sync.Once
	stopOnce    sync.Once
	wg          sync.WaitGroup
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
		waitingQueue:  make([]*Request, 0, queueSize),
		queueSize:     queueSize,
		workers:       workers,
		enqueueDone:   make(chan struct{}),
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

// Stop stops the scheduler and workers after draining accepted work.
func (d *Dispatcher) Stop() {
	d.stopOnce.Do(func() {
		d.mu.Lock()
		d.stopped = true
		d.mu.Unlock()
		close(d.enqueueDone)
		d.enqueueWG.Wait()
		close(d.incomingQueue)
		d.wg.Wait()
	})
}

// Enqueue adds a request to the scheduler's incoming queue.
// Blocks if the queue is full to provide natural backpressure.
func (d *Dispatcher) Enqueue(req *Request) error {
	if req == nil {
		return ErrInvalidRequest
	}
	if req.Result == nil || cap(req.Result) == 0 || len(req.Result) != 0 {
		return ErrResultUnavailable
	}

	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return ErrDispatcherStopped
	}
	d.enqueueWG.Add(1)
	d.mu.Unlock()
	defer d.enqueueWG.Done()

	select {
	case d.incomingQueue <- req:
		return nil
	case <-d.enqueueDone:
		return ErrDispatcherStopped
	}
}

// ErrDispatcherStopped is returned when enqueueing on a stopped dispatcher.
var ErrDispatcherStopped = dispatcherError("dispatcher stopped")

// ErrResultUnavailable reports a request result channel that cannot safely receive.
var ErrResultUnavailable = errors.New("dispatcher result channel unavailable")

// ErrInvalidRequest reports an invalid nil request.
var ErrInvalidRequest = errors.New("dispatcher request is nil")

type dispatcherError string

func (e dispatcherError) Error() string { return string(e) }

func (d *Dispatcher) schedulerLoop() {
	defer d.wg.Done()

	pendingWorkers := 0
	incomingQueue := d.incomingQueue

	for {
		if incomingQueue == nil && pendingWorkers == 0 && len(d.waitingQueue) == 0 {
			close(d.readyQueue)
			return
		}

		var (
			acceptIn <-chan *Request
			readyOut chan *Request
			nextReq  *Request
		)

		if incomingQueue != nil && len(d.waitingQueue) < d.queueSize {
			acceptIn = incomingQueue
		}
		if len(d.waitingQueue) > 0 {
			readyOut = d.readyQueue
			nextReq = d.waitingQueue[0]
		}

		select {
		case req, ok := <-acceptIn:
			if !ok {
				incomingQueue = nil
				continue
			}
			if req == nil {
				continue
			}
			pendingWorkers++
			select {
			case d.readyQueue <- req:
			default:
				d.waitingQueue = append(d.waitingQueue, req)
			}
		case readyOut <- nextReq:
			d.waitingQueue = d.waitingQueue[1:]
		case req := <-d.completeQueue:
			if req != nil && pendingWorkers > 0 {
				pendingWorkers--
			}
		}
	}
}

func (d *Dispatcher) workerLoop() {
	defer d.wg.Done()

	for req := range d.readyQueue {
		if req == nil {
			continue
		}
		d.processRequest(req)
		d.completeQueue <- req
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
		d.deliverResult(req, command.NotFoundError(req.Name))
		return
	}

	cmd, err := factory(req.Name, req.Args)
	if err != nil {
		d.deliverResult(req, protocol.Error("ERR "+err.Error()))
		return
	}

	result := cmd.Execute(ctx)
	d.deliverResult(req, result)
}

func (d *Dispatcher) deliverResult(req *Request, result protocol.RespValue) {
	req.Result <- result
}
