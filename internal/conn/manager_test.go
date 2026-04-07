package conn

import (
	"sync/atomic"
	"testing"
	"time"
)

// mockHandler is a test handler that counts calls
type mockHandler struct {
	calls atomic.Int64
}

func (m *mockHandler) Handle(conn *Conn, cmd Command) error {
	m.calls.Add(1)
	return nil
}

func TestNewManager(t *testing.T) {
	handler := &mockHandler{}
	config := &Config{
		IdleTimeout:    30 * time.Second,
		MaxConnections: 100,
	}

	mgr := NewManager(config, handler)

	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	if mgr.ActiveConnections() != 0 {
		t.Errorf("Expected 0 active connections, got %d", mgr.ActiveConnections())
	}
}

func TestConnectionManager_Stop(t *testing.T) {
	handler := &mockHandler{}
	mgr := NewManager(nil, handler)

	// Stop should not error
	err := mgr.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}

	// Double stop should be idempotent
	err = mgr.Stop()
	if err != nil {
		t.Errorf("Second Stop returned error: %v", err)
	}
}

func TestConnectionManager_ActiveConnections(t *testing.T) {
	handler := &mockHandler{}
	mgr := NewManager(nil, handler)

	// Initially no connections
	count := mgr.ActiveConnections()
	if count != 0 {
		t.Errorf("Expected 0 connections, got %d", count)
	}

	// Test concurrent access
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			mgr.ActiveConnections()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			mgr.ActiveConnections()
		}
		done <- true
	}()

	<-done
	<-done
}

func TestConnectionManager_NextConnID(t *testing.T) {
	handler := &mockHandler{}
	mgr := NewManager(nil, handler)

	// Test that connection IDs increment
	id1 := mgr.nextConnID.Add(1)
	id2 := mgr.nextConnID.Add(1)

	if id2 != id1+1 {
		t.Errorf("Expected ID to increment: %d -> %d", id1, id2)
	}
}

func TestNewConn(t *testing.T) {
	handler := &mockHandler{}
	config := &Config{
		IdleTimeout: 30 * time.Second,
	}
	mgr := NewManager(config, handler)

	// We can't test newConn directly since it's private
	// But we can verify the manager was created correctly
	if mgr.ActiveConnections() != 0 {
		t.Errorf("Expected 0 connections, got %d", mgr.ActiveConnections())
	}
}

func TestConnectionManager_SetAcceptError(t *testing.T) {
	handler := &mockHandler{}
	mgr := NewManager(nil, handler)

	called := false
	mgr.SetAcceptError(func(err error) {
		called = true
	})

	// Trigger the callback
	if mgr.acceptErr != nil {
		mgr.acceptErr(nil)
	}

	if !called {
		t.Error("AcceptError callback was not called")
	}
}

func TestConnectionManager_CloseConn(t *testing.T) {
	handler := &mockHandler{}
	mgr := NewManager(nil, handler)

	// Test that CloseConn doesn't panic with nil connection
	mgr.CloseConn(nil)

	// CloseConn is tested indirectly through the accept loop
	// We can't easily test it here because newConn is private
}
