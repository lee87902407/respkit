package session

import (
	"time"
)

// Session holds per-connection allocated state
type Session struct {
	ID                uint64
	Data              interface{}     // User-defined session data
	Conn              interface{}     // Back-reference to connection
	CreatedAt         time.Time
	LastSeen          time.Time
	CommandsProcessed uint64
	BytesRead         uint64
	BytesWritten      uint64
	// Transaction state
	MultiActive       bool
	WatchedKeys       map[string]bool
	// Subscription state
	SubscribedChannels map[string]bool
	PatternSubs        map[string]bool
}

// NewSession creates a new session with the given ID
func NewSession(id uint64) *Session {
	now := time.Now()
	return &Session{
		ID:        id,
		CreatedAt: now,
		LastSeen:  now,
	}
}

// UpdateLastSeen updates the LastSeen timestamp to now
func (s *Session) UpdateLastSeen() {
	s.LastSeen = time.Now()
}

// IncrementCommands increments the command counter
func (s *Session) IncrementCommands() {
	s.CommandsProcessed++
}

// AddBytesRead adds bytes to the read counter
func (s *Session) AddBytesRead(n uint64) {
	s.BytesRead += n
}

// AddBytesWritten adds bytes to the write counter
func (s *Session) AddBytesWritten(n uint64) {
	s.BytesWritten += n
}
