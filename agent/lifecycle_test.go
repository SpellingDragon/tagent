package agent

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// closeTrackingCloser tracks when Close() is called for testing close ordering.
type closeTrackingCloser struct {
	mu        sync.Mutex
	closed    bool
	closeTime time.Time
	closeErr  error
}

func (m *closeTrackingCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.closeTime = time.Now()
	return m.closeErr
}

func (m *closeTrackingCloser) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func TestTagentAgent_CloseWithClosers(t *testing.T) {
	closer1 := &closeTrackingCloser{}
	closer2 := &closeTrackingCloser{}

	ta := &TagentAgent{
		closers: []Closer{closer1, closer2},
	}

	if closer1.isClosed() || closer2.isClosed() {
		t.Fatal("nothing should be closed before Close()")
	}

	if err := ta.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	if !closer1.isClosed() {
		t.Error("closer1 should be closed")
	}
	if !closer2.isClosed() {
		t.Error("closer2 should be closed")
	}
}

func TestTagentAgent_CloseWithNoClosers(t *testing.T) {
	ta := &TagentAgent{closers: nil}

	if err := ta.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
}

func TestTagentAgent_CloseCloserError(t *testing.T) {
	failingCloser := &closeTrackingCloser{closeErr: fmt.Errorf("closer failed")}

	ta := &TagentAgent{
		closers: []Closer{failingCloser},
	}

	err := ta.Close()
	if err == nil {
		t.Fatal("expected error from failing closer")
	}
}

func TestTagentAgent_RegisterCloser(t *testing.T) {
	ta := &TagentAgent{}

	if len(ta.closers) != 0 {
		t.Fatalf("expected 0 closers, got %d", len(ta.closers))
	}

	closer := &closeTrackingCloser{}
	ta.RegisterCloser(closer)

	if len(ta.closers) != 1 {
		t.Fatalf("expected 1 closer, got %d", len(ta.closers))
	}

	if err := ta.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	if !closer.isClosed() {
		t.Error("registered closer should be closed")
	}
}
