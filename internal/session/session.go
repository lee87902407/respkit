package session

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lee87902407/basekit/mempool"
	"github.com/lee87902407/respkit/internal/dispatcher"
	"github.com/lee87902407/respkit/internal/protocol"
)

const defaultResponseQueueSize = 16

// ErrSessionStopped reports attempts to queue work after graceful shutdown starts.
var ErrSessionStopped = errors.New("session stopped")

type queuedResponse struct {
	value protocol.RespValue
	scope any
}

type responseWriter interface {
	Write(protocol.RespValue) error
	Flush() error
}

type requestReader func(*mempool.Scope) (protocol.RespValue, error)
type requestSubmitter func(protocol.RespValue, *mempool.Scope) error
type scopeFactory func() *mempool.Scope

// Session holds per-connection state and is the primary lifecycle object.
type Session struct {
	mu sync.RWMutex

	ID        uint64
	CreatedAt time.Time

	// Lifecycle management.
	done     chan struct{}  // closed when handle goroutine exits
	stopCh   chan struct{}  // closed when graceful shutdown starts
	stopFlag atomic.Bool    // set by Stop for graceful shutdown
	closer   io.Closer      // underlying connection for forced close
	onRemove func(*Session) // called when handle goroutine exits
	closed   bool
	stopOnce sync.Once

	// User data.
	data interface{}

	// Runtime skeleton for the redesign path.
	responses     chan queuedResponse
	maxInFlight   int32
	inflight      atomic.Int32
	newScope      scopeFactory
	readRequest   requestReader
	submitRequest requestSubmitter

	// Statistics.
	lastSeen   time.Time
	commands   uint64
	bytesRead  uint64
	bytesWrite uint64

	// Transaction state.
	multiActive bool
	watchedKeys map[string]bool

	// Subscription state.
	subscribedChannels map[string]bool
	patternSubs        map[string]bool
}

// NewSession creates a new session with the given ID.
func NewSession(id uint64) *Session {
	now := time.Now()
	pool := mempool.New(mempool.DefaultOptions())
	return &Session{
		ID:          id,
		CreatedAt:   now,
		lastSeen:    now,
		done:        make(chan struct{}),
		stopCh:      make(chan struct{}),
		responses:   make(chan queuedResponse, defaultResponseQueueSize),
		maxInFlight: 1,
		newScope: func() *mempool.Scope {
			return mempool.NewScope(pool)
		},
	}
}

// SetCloser sets the underlying closer for forced close.
func (s *Session) SetCloser(c io.Closer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closer = c
}

// SetOnRemove sets the callback invoked when the handle goroutine exits.
func (s *Session) SetOnRemove(fn func(*Session)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onRemove = fn
}

// SetOnClose sets the callback invoked when session processing exits.
func (s *Session) SetOnClose(fn func(*Session)) {
	s.SetOnRemove(fn)
}

// SetScopeFactory overrides how the session allocates per-request scopes.
func (s *Session) SetScopeFactory(fn func() *mempool.Scope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.newScope = fn
}

// SetRequestReader overrides how the session reads requests.
func (s *Session) SetRequestReader(fn func(*mempool.Scope) (protocol.RespValue, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readRequest = fn
}

// SetRequestSubmitter overrides how the session submits requests for execution.
func (s *Session) SetRequestSubmitter(fn func(protocol.RespValue, *mempool.Scope) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.submitRequest = fn
}

// UseDispatcher binds the session request submit path to a dispatcher instance.
func (s *Session) UseDispatcher(d *dispatcher.Dispatcher) {
	if d == nil {
		s.SetRequestSubmitter(nil)
		return
	}

	s.SetRequestSubmitter(func(value protocol.RespValue, scope *mempool.Scope) error {
		resultCh := make(chan protocol.RespValue, 1)
		request := &dispatcher.Request{
			SessionID: s.ID,
			Name:      value.CommandName(),
			Args:      copyCommandArgs(value.CommandArgs()),
			Result:    resultCh,
		}
		if err := d.Enqueue(request); err != nil {
			return err
		}

		go func(scope *mempool.Scope) {
			result, ok := <-resultCh
			if !ok {
				s.decrementInflight()
				return
			}
			_ = s.HandleResponse(result, scope)
			s.decrementInflight()
		}(scope)

		return nil
	})
}

// Start runs the given handle function in a goroutine.
// The done channel is closed when the handle function returns.
func (s *Session) Start(handle func()) {
	go func() {
		defer close(s.done)
		defer func() {
			if fn := s.getOnRemove(); fn != nil {
				fn(s)
			}
		}()
		defer s.closeConn()
		handle()
	}()
}

// Stop performs a graceful stop by signaling the handle loop to exit.
// It blocks until the handle goroutine has finished.
func (s *Session) Stop() {
	s.StopGracefully()
	<-s.done
}

// StopGracefully signals the session to stop without forcing the connection closed.
func (s *Session) StopGracefully() {
	s.stopFlag.Store(true)
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

// Close performs a forced close by closing the underlying connection.
// It blocks until the handle goroutine has finished.
func (s *Session) Close() error {
	return s.CloseNow()
}

// CloseNow forcefully closes the underlying connection and waits for exit.
func (s *Session) CloseNow() error {
	s.StopGracefully()
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		if s.closer != nil {
			s.closer.Close()
			s.closer = nil
		}
	}
	s.mu.Unlock()
	<-s.done
	return nil
}

// HandleResponse enqueues a response for a future write loop.
func (s *Session) HandleResponse(value protocol.RespValue, scope any) error {
	if s.ShouldStop() {
		return ErrSessionStopped
	}

	queued := queuedResponse{value: value, scope: scope}
	select {
	case s.responses <- queued:
		return nil
	case <-s.stopCh:
		return ErrSessionStopped
	}
}

func (s *Session) writeLoop(writer responseWriter) {
	if writer == nil {
		return
	}

	batch := make([]queuedResponse, 0, defaultResponseQueueSize)
	for {
		select {
		case <-s.stopCh:
			if len(s.responses) == 0 {
				return
			}
		default:
		}

		var first queuedResponse
		select {
		case first = <-s.responses:
		case <-s.stopCh:
			if len(s.responses) == 0 {
				return
			}
			first = <-s.responses
		}

		batch = append(batch[:0], first)
		if err := writer.Write(first.value); err != nil {
			s.releaseBatch(batch)
			return
		}

		for len(s.responses) > 0 {
			next := <-s.responses
			batch = append(batch, next)
			if err := writer.Write(next.value); err != nil {
				s.releaseBatch(batch)
				return
			}
		}

		if err := writer.Flush(); err != nil {
			s.releaseBatch(batch)
			return
		}
		s.releaseBatch(batch)
	}
}

func (s *Session) readLoop() {
	reader, submitter, scopeFactory := s.getReadDependencies()
	if reader == nil || submitter == nil || scopeFactory == nil {
		return
	}

	for {
		if s.ShouldStop() {
			return
		}
		if !s.waitForInflightSlot() {
			return
		}

		scope := scopeFactory()
		if scope == nil {
			return
		}

		value, err := reader(scope)
		if err != nil {
			releaseScope(scope)
			if errors.Is(err, io.EOF) || errors.Is(err, netErrClosed) || s.ShouldStop() {
				return
			}
			return
		}

		s.inflight.Add(1)
		if err := submitter(value, scope); err != nil {
			s.inflight.Add(-1)
			releaseScope(scope)
			if s.ShouldStop() {
				return
			}
			return
		}
	}
}

func (s *Session) releaseBatch(batch []queuedResponse) {
	for _, resp := range batch {
		releaseScope(resp.scope)
	}
}

func releaseScope(scope any) {
	switch v := scope.(type) {
	case interface{ Close() }:
		v.Close()
	case interface{ Close() error }:
		_ = v.Close()
	}
}

func copyCommandArgs(args [][]byte) [][]byte {
	if len(args) == 0 {
		return nil
	}
	copied := make([][]byte, len(args))
	for i, arg := range args {
		if arg == nil {
			continue
		}
		dup := make([]byte, len(arg))
		copy(dup, arg)
		copied[i] = dup
	}
	return copied
}

func (s *Session) waitForInflightSlot() bool {
	for {
		if s.ShouldStop() {
			return false
		}
		if s.inflight.Load() < s.maxInFlight {
			return true
		}
		select {
		case <-s.stopCh:
			return false
		case <-time.After(time.Millisecond):
		}
	}
}

func (s *Session) getReadDependencies() (requestReader, requestSubmitter, scopeFactory) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readRequest, s.submitRequest, s.newScope
}

func (s *Session) decrementInflight() {
	for {
		current := s.inflight.Load()
		if current <= 0 {
			return
		}
		if s.inflight.CompareAndSwap(current, current-1) {
			return
		}
	}
}

var netErrClosed = errors.New("use of closed network connection")

// ShouldStop returns true if the handle loop should exit.
func (s *Session) ShouldStop() bool {
	return s.stopFlag.Load() || s.IsClosed()
}

// IsClosed returns whether the session has been closed.
func (s *Session) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

func (s *Session) closeConn() {
	s.mu.Lock()
	if s.closer != nil {
		s.closer.Close()
		s.closer = nil
	}
	s.closed = true
	s.mu.Unlock()
}

func (s *Session) getOnRemove() func(*Session) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.onRemove
}

// SetData stores user-defined session data.
func (s *Session) SetData(data interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = data
}

// DataValue returns the stored user-defined session data.
func (s *Session) DataValue() interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data
}

// UpdateLastSeen updates the LastSeen timestamp to now.
func (s *Session) UpdateLastSeen() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSeen = time.Now()
}

// LastSeenAt returns the last-seen timestamp.
func (s *Session) LastSeenAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSeen
}

// IncrementCommands increments the command counter.
func (s *Session) IncrementCommands() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands++
}

// CommandsCount returns the processed command count.
func (s *Session) CommandsCount() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.commands
}

// AddBytesRead adds bytes to the read counter.
func (s *Session) AddBytesRead(n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bytesRead += n
}

// BytesReadCount returns the number of read bytes.
func (s *Session) BytesReadCount() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bytesRead
}

// AddBytesWritten adds bytes to the write counter.
func (s *Session) AddBytesWritten(n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bytesWrite += n
}

// BytesWrittenCount returns the number of written bytes.
func (s *Session) BytesWrittenCount() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bytesWrite
}
