package session

import (
	"testing"
	"time"
)

func TestNewSessionInitializesFields(t *testing.T) {
	before := time.Now()
	sess := NewSession(42)
	after := time.Now()

	if sess.ID != 42 {
		t.Fatalf("ID = %d, want 42", sess.ID)
	}
	if sess.CreatedAt.Before(before) || sess.CreatedAt.After(after) {
		t.Fatalf("CreatedAt = %v, want between %v and %v", sess.CreatedAt, before, after)
	}
	if !sess.LastSeen.Equal(sess.CreatedAt) {
		t.Fatalf("LastSeen = %v, want %v", sess.LastSeen, sess.CreatedAt)
	}
}

func TestSessionCountersAndLastSeen(t *testing.T) {
	sess := NewSession(7)
	firstSeen := sess.LastSeen
	time.Sleep(time.Millisecond)

	sess.UpdateLastSeen()
	sess.IncrementCommands()
	sess.IncrementCommands()
	sess.AddBytesRead(12)
	sess.AddBytesWritten(34)

	if !sess.LastSeen.After(firstSeen) {
		t.Fatalf("LastSeen = %v, want after %v", sess.LastSeen, firstSeen)
	}
	if sess.CommandsProcessed != 2 {
		t.Fatalf("CommandsProcessed = %d, want 2", sess.CommandsProcessed)
	}
	if sess.BytesRead != 12 {
		t.Fatalf("BytesRead = %d, want 12", sess.BytesRead)
	}
	if sess.BytesWritten != 34 {
		t.Fatalf("BytesWritten = %d, want 34", sess.BytesWritten)
	}
}
