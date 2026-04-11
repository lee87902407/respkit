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
	if !sess.LastSeenAt().Equal(sess.CreatedAt) {
		t.Fatalf("LastSeen = %v, want %v", sess.LastSeenAt(), sess.CreatedAt)
	}
	if sess.responses == nil {
		t.Fatal("responses channel should be initialized")
	}
	if cap(sess.responses) == 0 {
		t.Fatal("responses channel should be buffered")
	}
	if sess.maxInFlight != 1 {
		t.Fatalf("maxInFlight = %d, want 1", sess.maxInFlight)
	}
	if sess.stopCh == nil {
		t.Fatal("stopCh should be initialized")
	}
}

func TestSessionCountersAndLastSeen(t *testing.T) {
	sess := NewSession(7)
	firstSeen := sess.LastSeenAt()
	time.Sleep(time.Millisecond)

	sess.UpdateLastSeen()
	sess.IncrementCommands()
	sess.IncrementCommands()
	sess.AddBytesRead(12)
	sess.AddBytesWritten(34)

	if !sess.LastSeenAt().After(firstSeen) {
		t.Fatalf("LastSeen = %v, want after %v", sess.LastSeenAt(), firstSeen)
	}
	if sess.CommandsCount() != 2 {
		t.Fatalf("CommandsProcessed = %d, want 2", sess.CommandsCount())
	}
	if sess.BytesReadCount() != 12 {
		t.Fatalf("BytesRead = %d, want 12", sess.BytesReadCount())
	}
	if sess.BytesWrittenCount() != 34 {
		t.Fatalf("BytesWritten = %d, want 34", sess.BytesWrittenCount())
	}
}

func TestSessionCloserAccessor(t *testing.T) {
	sess := NewSession(1)
	if sess.closer != nil {
		t.Fatal("expected nil closer on new session")
	}

	c := &mockCloser{}
	sess.SetCloser(c)
	if sess.closer != c {
		t.Fatal("SetCloser did not set closer")
	}
}

func TestSessionStartAndStop(t *testing.T) {
	sess := NewSession(1)

	done := make(chan struct{})
	sess.Start(func() {
		<-done
	})

	if sess.IsClosed() {
		t.Fatal("session should not be closed after Start")
	}

	close(done)
	sess.Stop() // should return immediately since handle exited
}

func TestSessionGracefulStop(t *testing.T) {
	sess := NewSession(1)
	running := make(chan struct{})
	exited := make(chan struct{})

	sess.Start(func() {
		close(running)
		for {
			if sess.ShouldStop() {
				close(exited)
				return
			}
			time.Sleep(time.Millisecond)
		}
	})

	<-running
	sess.Stop()

	select {
	case <-exited:
		// handle loop exited
	case <-time.After(time.Second):
		t.Fatal("handle loop did not exit after Stop")
	}
}

func TestSessionForcedClose(t *testing.T) {
	sess := NewSession(1)
	closer := &mockCloser{}
	sess.SetCloser(closer)

	sess.Start(func() {
		for {
			if sess.ShouldStop() {
				return
			}
			time.Sleep(time.Millisecond)
		}
	})

	err := sess.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !closer.closed {
		t.Fatal("Close() did not close the closer")
	}
	if !sess.IsClosed() {
		t.Fatal("session should be closed after Close()")
	}
}

func TestSessionOnRemove(t *testing.T) {
	sess := NewSession(1)
	removeCh := make(chan struct{}, 1)
	sess.SetOnRemove(func(ss *Session) {
		if ss.ID != 1 {
			t.Errorf("onRemove got session ID %d, want 1", ss.ID)
		}
		close(removeCh)
	})

	sess.Start(func() {})

	select {
	case <-removeCh:
		// onRemove was called
	case <-time.After(time.Second):
		t.Fatal("onRemove was not called")
	}
}

func TestSessionStopGracefullyUsesStopChannel(t *testing.T) {
	sess := NewSession(1)
	running := make(chan struct{})
	exited := make(chan struct{})

	sess.Start(func() {
		close(running)
		<-sess.stopCh
		close(exited)
	})

	<-running
	sess.StopGracefully()

	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("StopGracefully() did not close stopCh")
	}
}

func TestSessionCloseNowClosesCloser(t *testing.T) {
	sess := NewSession(1)
	closer := &mockCloser{}
	sess.SetCloser(closer)

	sess.Start(func() {
		for {
			if sess.ShouldStop() {
				return
			}
			time.Sleep(time.Millisecond)
		}
	})

	if err := sess.CloseNow(); err != nil {
		t.Fatalf("CloseNow() error = %v", err)
	}
	if !closer.closed {
		t.Fatal("CloseNow() did not close the closer")
	}
}

func TestSessionShouldStop(t *testing.T) {
	sess := NewSession(1)
	if sess.ShouldStop() {
		t.Fatal("new session should not ShouldStop")
	}

	sess.stopFlag.Store(true)
	if !sess.ShouldStop() {
		t.Fatal("session should ShouldStop after stopFlag set")
	}
}

func TestSessionStopIdempotent(t *testing.T) {
	sess := NewSession(1)
	sess.Start(func() {})

	// Multiple stops should not panic
	sess.Stop()
	sess.Stop()
}

func TestSessionCloseIdempotent(t *testing.T) {
	sess := NewSession(1)
	sess.Start(func() {})

	// Multiple closes should not panic
	_ = sess.Close()
	_ = sess.Close()
}

type mockCloser struct {
	closed bool
}

func (m *mockCloser) Close() error {
	m.closed = true
	return nil
}
