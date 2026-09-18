package rl

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// slowStreamModel: GenerateContent returns a channel the TEST controls —
// simulating an in-flight LLM stream that outlives the GenerateContent call
// (the exact window the old in-flight counter missed).
type slowStreamModel struct {
	mu       sync.Mutex
	ch       chan *model.Response
	closed   int
	closeErr error
}

func (m *slowStreamModel) Info() model.Info { return model.Info{Name: "slow"} }

func (m *slowStreamModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan *model.Response, 4)
	m.ch = ch
	return ch, nil
}

func (m *slowStreamModel) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed++
	return m.closeErr
}

func (m *slowStreamModel) send(r *model.Response) {
	m.mu.Lock()
	ch := m.ch
	m.mu.Unlock()
	if ch != nil {
		ch <- r
	}
}

func (m *slowStreamModel) endStream() {
	m.mu.Lock()
	ch := m.ch
	m.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (m *slowStreamModel) closedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// FAIL-BEFORE (resident-readiness-plan 4.8): swapping while the OLD model's
// response stream is still open must NOT close the old model — responses in
// flight stay readable until the stream ends/cancels; only then is it closed
// exactly once. The old counter released the lease when GenerateContent
// RETURNED (stream still open), so a concurrent Swap closed a live stream.
func TestSwappableModel_StreamLeaseOutlivesGenerateReturn(t *testing.T) {
	old := &slowStreamModel{}
	newM := &slowStreamModel{}
	sm := NewSwappableModel(old)

	ch, err := sm.GenerateContent(context.Background(), &model.Request{})
	require.NoError(t, err)
	require.NotNil(t, ch)

	old.send(&model.Response{Done: false}) // stream mid-flight

	sm.Swap(newM) // swap while old stream still open
	require.Equal(t, 0, old.closedCount(),
		"old model must stay open while its stream is unconsumed/unclosed")

	// Drain the rest of the old stream: responses stay readable.
	old.send(&model.Response{Done: true})
	old.endStream()
	n := 0
	for range ch {
		n++
	}
	require.Equal(t, 2, n, "all in-flight responses remain readable after swap")

	// Stream ended → the retired model becomes closable on the next sweep.
	require.Eventually(t, func() bool { return old.closedCount() == 1 },
		time.Second, 5*time.Millisecond, "retired model closed exactly once after stream end")
	require.Equal(t, 0, newM.closedCount())
}

// FAIL-BEFORE: A→B→A with A still in use — the current inner must NEVER be
// closed by a retirement sweep (the old sweep closed every retired entry).
func TestSwappableModel_ReselectDoesNotCloseLiveInstance(t *testing.T) {
	a := &slowStreamModel{}
	b := &slowStreamModel{}
	sm := NewSwappableModel(a)

	_, _ = sm.GenerateContent(context.Background(), &model.Request{}) // a in use
	sm.Swap(b)                                                        // a → retired
	sm.Swap(a)                                                        // b → retired, a back as current

	require.Equal(t, 0, a.closedCount(), "reselected current instance must not be swept")

	a.endStream() // old a-stream ends → its FIRST retirement may now close? NO:
	// a is the CURRENT inner again — closing it would kill live usage.
	require.Equal(t, 0, a.closedCount(),
		"current instance is never a retirement candidate")
	b.endStream()
	require.Eventually(t, func() bool { return b.closedCount() == 1 },
		time.Second, 5*time.Millisecond)
}

// Error / nil streams release the lease immediately (no goroutine leak).
func TestSwappableModel_ErrorAndNilStreams(t *testing.T) {
	em := &errModel{}
	sm := NewSwappableModel(em)
	_, err := sm.GenerateContent(context.Background(), &model.Request{})
	require.Error(t, err)
	// (release path exercised; no panic, no leak — asserted via swap safety below)

	repl := &slowStreamModel{}
	sm.Swap(repl) // err 路径已立即释放 lease → 旧 model 可安全回收（恰一次）
	require.Eventually(t, func() bool { return em.closed == 1 },
		time.Second, 5*time.Millisecond,
		"error stream releases the lease immediately — retired model is closable")
}

type errModel struct{ closed int }

func (m *errModel) Info() model.Info { return model.Info{Name: "err"} }
func (m *errModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	return nil, context.Canceled
}
func (m *errModel) Close() error { m.closed++; return nil }
