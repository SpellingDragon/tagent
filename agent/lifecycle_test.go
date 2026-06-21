package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// closeTrackingRunner implements runner.Runner and tracks Close() call time.
type closeTrackingRunner struct {
	mu        sync.Mutex
	closed    bool
	closeTime time.Time
}

func (m *closeTrackingRunner) Run(ctx context.Context, userID string, sessionID string, message model.Message, opts ...agent.RunOption) (<-chan *event.Event, error) {
	return nil, nil
}

func (m *closeTrackingRunner) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.closeTime = time.Now()
	return nil
}

func (m *closeTrackingRunner) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

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

// ==================== Task 4.8: TagentAgent.Close() 先关 ActionTool 再关 Runner ====================

func TestTagentAgent_CloseOrder_ClosersBeforeRunner(t *testing.T) {
	mockR := &closeTrackingRunner{}
	closer1 := &closeTrackingCloser{}
	closer2 := &closeTrackingCloser{}

	ta := &TagentAgent{
		runner:  mockR,
		closers: []Closer{closer1, closer2},
	}

	// Before Close: nothing should be closed
	if closer1.isClosed() || closer2.isClosed() || mockR.isClosed() {
		t.Fatal("nothing should be closed before Close()")
	}

	if err := ta.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	// After Close: everything should be closed
	if !closer1.isClosed() {
		t.Error("closer1 should be closed")
	}
	if !closer2.isClosed() {
		t.Error("closer2 should be closed")
	}
	if !mockR.isClosed() {
		t.Error("runner should be closed")
	}

	// Verify ordering: closers should be closed before runner
	runnerCloseTime := mockR.closeTime
	if closer1.closeTime.After(runnerCloseTime) {
		t.Error("closer1 should be closed before runner")
	}
	if closer2.closeTime.After(runnerCloseTime) {
		t.Error("closer2 should be closed before runner")
	}
}

func TestTagentAgent_CloseWithNoClosers(t *testing.T) {
	mockR := &closeTrackingRunner{}

	ta := &TagentAgent{
		runner:  mockR,
		closers: nil,
	}

	if err := ta.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	if !mockR.isClosed() {
		t.Error("runner should be closed even with no closers")
	}
}

func TestTagentAgent_CloseCloserError_StillClosesRunner(t *testing.T) {
	mockR := &closeTrackingRunner{}
	failingCloser := &closeTrackingCloser{
		closeErr: fmt.Errorf("closer failed"),
	}

	ta := &TagentAgent{
		runner:  mockR,
		closers: []Closer{failingCloser},
	}

	err := ta.Close()
	if err == nil {
		t.Fatal("expected error from failing closer")
	}

	if !mockR.isClosed() {
		t.Error("runner should be closed even if closer fails")
	}
}

func TestTagentAgent_RegisterCloser(t *testing.T) {
	mockR := &closeTrackingRunner{}

	ta := &TagentAgent{
		runner: mockR,
	}

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
