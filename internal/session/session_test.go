package session

import (
	"io"
	"testing"
	"time"

	"github.com/lee87902407/basekit/mempool"
	"github.com/lee87902407/respkit/internal/protocol"
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

func TestSessionHandleResponseQueuesValue(t *testing.T) {
	sess := NewSession(1)
	want := protocol.SimpleString("PONG")

	if err := sess.HandleResponse(want, nil); err != nil {
		t.Fatalf("HandleResponse() error = %v", err)
	}

	select {
	case got := <-sess.responses:
		if !got.value.Equal(want) {
			t.Fatalf("queued response = %#v, want %#v", got.value, want)
		}
	case <-time.After(time.Second):
		t.Fatal("HandleResponse() did not enqueue response")
	}
}

func TestSessionHandleResponseRejectsStoppedSession(t *testing.T) {
	sess := NewSession(1)
	sess.StopGracefully()

	err := sess.HandleResponse(protocol.SimpleString("PONG"), nil)
	if err != ErrSessionStopped {
		t.Fatalf("HandleResponse() error = %v, want %v", err, ErrSessionStopped)
	}
}

func TestSessionWriteLoopFlushesBatchAndReleasesScopes(t *testing.T) {
	sess := NewSession(1)
	writer := &mockResponseWriter{}
	scope1 := &mockScope{}
	scope2 := &mockScope{}
	done := make(chan struct{})

	if err := sess.HandleResponse(protocol.SimpleString("ONE"), scope1); err != nil {
		t.Fatalf("HandleResponse(ONE) error = %v", err)
	}
	if err := sess.HandleResponse(protocol.SimpleString("TWO"), scope2); err != nil {
		t.Fatalf("HandleResponse(TWO) error = %v", err)
	}

	go func() {
		defer close(done)
		sess.writeLoop(writer)
	}()

	select {
	case <-time.After(time.Second):
		t.Fatal("writeLoop did not drain queued responses in time")
	case <-waitForCondition(func() bool { return writer.flushes == 1 && scope1.closed && scope2.closed }):
	}

	sess.StopGracefully()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("writeLoop did not exit after graceful stop")
	}

	if len(writer.values) != 2 {
		t.Fatalf("write count = %d, want 2", len(writer.values))
	}
	if writer.flushes != 1 {
		t.Fatalf("flush count = %d, want 1", writer.flushes)
	}
	if !scope1.closed || !scope2.closed {
		t.Fatal("scopes should be released after flush")
	}
}

func TestSessionReadLoopSubmitsRequestWithScope(t *testing.T) {
	sess := NewSession(1)
	pool := mempool.New(mempool.DefaultOptions())
	sess.SetScopeFactory(func() *mempool.Scope { return mempool.NewScope(pool) })

	reads := 0
	sess.SetRequestReader(func(scope *mempool.Scope) (protocol.RespValue, error) {
		reads++
		if scope == nil {
			t.Fatal("readLoop passed nil scope to reader")
		}
		if reads == 1 {
			return protocol.ArrayOf(protocol.BulkFromString("PING")), nil
		}
		return protocol.RespValue{}, io.EOF
	})

	submitted := make(chan protocol.RespValue, 1)
	sess.SetRequestSubmitter(func(value protocol.RespValue, scope *mempool.Scope) error {
		if scope == nil {
			t.Fatal("submitter received nil scope")
		}
		submitted <- value
		sess.inflight.Add(-1)
		return nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		sess.readLoop()
	}()

	select {
	case got := <-submitted:
		want := protocol.ArrayOf(protocol.BulkFromString("PING"))
		if !got.Equal(want) {
			t.Fatalf("submitted request = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("readLoop did not submit request")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readLoop did not exit after reader EOF")
	}
}

func TestSessionReadLoopHonorsInflightLimit(t *testing.T) {
	sess := NewSession(1)
	pool := mempool.New(mempool.DefaultOptions())
	sess.SetScopeFactory(func() *mempool.Scope { return mempool.NewScope(pool) })
	sess.maxInFlight = 1

	secondReadAttempted := make(chan struct{}, 1)
	readCount := 0
	value1 := protocol.ArrayOf(protocol.BulkFromString("PING"))
	value2 := protocol.ArrayOf(protocol.BulkFromString("ECHO"), protocol.BulkFromString("hi"))

	sess.SetRequestReader(func(scope *mempool.Scope) (protocol.RespValue, error) {
		readCount++
		switch readCount {
		case 1:
			return value1, nil
		case 2:
			secondReadAttempted <- struct{}{}
			return value2, nil
		default:
			return protocol.RespValue{}, io.EOF
		}
	})

	firstSubmitted := make(chan struct{}, 1)
	secondSubmitted := make(chan protocol.RespValue, 1)
	allowSecond := make(chan struct{})
	submitCount := 0
	sess.SetRequestSubmitter(func(value protocol.RespValue, scope *mempool.Scope) error {
		submitCount++
		switch submitCount {
		case 1:
			firstSubmitted <- struct{}{}
			<-allowSecond
			sess.inflight.Add(-1)
		case 2:
			secondSubmitted <- value
			sess.inflight.Add(-1)
		}
		return nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		sess.readLoop()
	}()

	select {
	case <-firstSubmitted:
	case <-time.After(time.Second):
		t.Fatal("first request was not submitted")
	}

	select {
	case <-secondReadAttempted:
		t.Fatal("second read should not happen while inflight is full")
	case <-time.After(20 * time.Millisecond):
	}

	close(allowSecond)

	select {
	case got := <-secondSubmitted:
		if !got.Equal(value2) {
			t.Fatalf("second submitted request = %#v, want %#v", got, value2)
		}
	case <-time.After(time.Second):
		t.Fatal("second request was not submitted after inflight released")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readLoop did not exit after processing queued reads")
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

type mockScope struct {
	closed bool
}

func (m *mockScope) Close() {
	m.closed = true
}

type mockResponseWriter struct {
	values  []protocol.RespValue
	flushes int
}

func (m *mockResponseWriter) Write(value protocol.RespValue) error {
	m.values = append(m.values, value)
	return nil
}

func (m *mockResponseWriter) Flush() error {
	m.flushes++
	return nil
}

func waitForCondition(cond func() bool) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if cond() {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	return done
}
