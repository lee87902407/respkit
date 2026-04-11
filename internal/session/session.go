package session

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lee87902407/respkit/internal/protocol"
)

const defaultResponseQueueSize = 16

type queuedResponse struct {
	value protocol.RespValue
	scope any
}

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
	responses   chan queuedResponse
	maxInFlight int32
	inflight    atomic.Int32

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
	return &Session{
		ID:          id,
		CreatedAt:   now,
		lastSeen:    now,
		done:        make(chan struct{}),
		stopCh:      make(chan struct{}),
		responses:   make(chan queuedResponse, defaultResponseQueueSize),
		maxInFlight: 1,
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
