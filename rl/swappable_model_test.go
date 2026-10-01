package rl

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// iterCapableBase 同时实现 model.Model 与 model.IterModel，并记录走了哪个入口、
// 以及在开始迭代之前是否已被触达（用于判惰性）。
type iterCapableBase struct {
	// iterEntry 记录内层 GenerateContentIter 是否被调用。
	iterEntry int32
	// iterStart 记录返回的 Seq 是否真正开始迭代（用于判惰性）。
	iterStart int32
	// chanEntry 记录内层 GenerateContent 是否被调用（被调用即说明降级成了通道路）。
	chanEntry int32
	responses []*model.Response
}

func (b *iterCapableBase) Info() model.Info { return model.Info{Name: "iter-base"} }

func (b *iterCapableBase) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	atomic.AddInt32(&b.chanEntry, 1)
	ch := make(chan *model.Response, len(b.responses))
	for _, r := range b.responses {
		ch <- r
	}
	close(ch)
	return ch, nil
}

func (b *iterCapableBase) GenerateContentIter(_ context.Context, _ *model.Request) (model.Seq[*model.Response], error) {
	atomic.AddInt32(&b.iterEntry, 1)
	return func(yield func(*model.Response) bool) {
		atomic.AddInt32(&b.iterStart, 1)
		for _, r := range b.responses {
			if !yield(r) {
				return
			}
		}
	}, nil
}

// channelOnly 只暴露 model.Model（无 IterModel）——装饰器必须真实桥接通道的场景。
type channelOnly struct{ inner model.Model }

func (c channelOnly) Info() model.Info { return c.inner.Info() }

func (c channelOnly) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	return c.inner.GenerateContent(ctx, req)
}

func mkIterResp(text string) *model.Response {
	return &model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: text}}}}
}

// TestSwappableModel_PreservesIterModel 钉住内层 IterModel 能力不被隐藏、构造迭代器不触内层、迭代结束即释放租约（本文件承载该模型租约与回收语义）。
//
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestSwappableModel_PreservesIterModel(t *testing.T) {
	var _ model.IterModel = (*SwappableModel)(nil)

	base := &iterCapableBase{responses: []*model.Response{mkIterResp("a"), mkIterResp("b")}}
	sw := NewSwappableModel(base)

	seq, err := sw.GenerateContentIter(context.Background(), &model.Request{})
	require.NoError(t, err)
	require.NotNil(t, seq)
	require.Equal(t, int32(0), atomic.LoadInt32(&base.iterEntry), "creating the iterator must not call the base")
	require.Equal(t, int32(0), atomic.LoadInt32(&base.chanEntry), "creating the iterator must not downgrade to channel")
	require.Equal(t, int64(0), sw.inFlight.Load(), "no lease before iteration starts")

	var got []string
	seq(func(r *model.Response) bool {
		if r != nil && len(r.Choices) > 0 {
			got = append(got, r.Choices[0].Message.Content)
		}
		return true
	})
	require.Equal(t, []string{"a", "b"}, got)
	require.Equal(t, int32(1), atomic.LoadInt32(&base.iterEntry), "must delegate to the base iterator (fast path)")
	require.Equal(t, int32(0), atomic.LoadInt32(&base.chanEntry), "must NOT downgrade an iterator base to channel")
	require.Equal(t, int64(0), sw.inFlight.Load(), "iteration lease must be released when the Seq finishes")
}

// TestSwappableModel_BridgesChannelBase 钉住通道型内层被真实桥接（全部响应送达），而非伪造接口。
func TestSwappableModel_BridgesChannelBase(t *testing.T) {
	base := &iterCapableBase{responses: []*model.Response{mkIterResp("x"), mkIterResp("y")}}
	sw := NewSwappableModel(channelOnly{base})

	seq, err := sw.GenerateContentIter(context.Background(), &model.Request{})
	require.NoError(t, err)
	var got []string
	seq(func(r *model.Response) bool {
		if r != nil && len(r.Choices) > 0 {
			got = append(got, r.Choices[0].Message.Content)
		}
		return true
	})
	require.Equal(t, []string{"x", "y"}, got, "channel base must be bridged into the iterator")
	require.Equal(t, int64(0), sw.inFlight.Load(), "bridge lease released on completion")
}

// TestSwappableModel_IterEarlyStopReleasesLease 钉住提前停止仍释放租约且不卡住生产方。
func TestSwappableModel_IterEarlyStopReleasesLease(t *testing.T) {
	base := &iterCapableBase{responses: []*model.Response{mkIterResp("1"), mkIterResp("2"), mkIterResp("3")}}
	sw := NewSwappableModel(base)
	seq, err := sw.GenerateContentIter(context.Background(), &model.Request{})
	require.NoError(t, err)
	count := 0
	seq(func(*model.Response) bool {
		count++
		return false
	})
	require.Equal(t, 1, count)
	require.Equal(t, int64(0), sw.inFlight.Load(), "early-stop must release the lease")
}

// slowStreamModel 的返回通道由测试掌控，用来模拟"存活时间超过 GenerateContent 返回"
// 的在途流——正是只按调用计数会漏掉的那个窗口。
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
	m.ch = nil
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

// TestSwappableModel_StreamLeaseOutlivesGenerateReturn 钉住租约覆盖整条流：老流未结束前不得关闭其模型，结束后恰好关一次。
func TestSwappableModel_StreamLeaseOutlivesGenerateReturn(t *testing.T) {
	old := &slowStreamModel{}
	newM := &slowStreamModel{}
	sm := NewSwappableModel(old)

	ch, err := sm.GenerateContent(context.Background(), &model.Request{})
	require.NoError(t, err)
	require.NotNil(t, ch)

	old.send(&model.Response{Done: false})

	sm.Swap(newM)
	require.Equal(t, 0, old.closedCount(),
		"old model must stay open while its stream is unconsumed/unclosed")

	old.send(&model.Response{Done: true})
	old.endStream()
	n := 0
	for range ch {
		n++
	}
	require.Equal(t, 2, n, "all in-flight responses remain readable after swap")

	require.Eventually(t, func() bool { return old.closedCount() == 1 },
		time.Second, 5*time.Millisecond, "retired model closed exactly once after stream end")
	require.Equal(t, 0, newM.closedCount())
}

// TestSwappableModel_ReselectDoesNotCloseLiveInstance 钉住当前内层永不作为回收候选，即便它曾退役过（A→B→A）。
func TestSwappableModel_ReselectDoesNotCloseLiveInstance(t *testing.T) {
	a := &slowStreamModel{}
	b := &slowStreamModel{}
	sm := NewSwappableModel(a)

	_, _ = sm.GenerateContent(context.Background(), &model.Request{})
	sm.Swap(b)
	sm.Swap(a)

	require.Equal(t, 0, a.closedCount(), "reselected current instance must not be swept")

	a.endStream()
	require.Equal(t, 0, a.closedCount(),
		"current instance is never a retirement candidate")
	b.endStream()
	require.Eventually(t, func() bool { return b.closedCount() == 1 },
		time.Second, 5*time.Millisecond)
}

// TestSwappableModel_ErrorAndNilStreams 钉住出错与 nil 流立即释放租约，因而旧模型可被安全回收。
func TestSwappableModel_ErrorAndNilStreams(t *testing.T) {
	em := &errModel{}
	sm := NewSwappableModel(em)
	_, err := sm.GenerateContent(context.Background(), &model.Request{})
	require.Error(t, err)

	repl := &slowStreamModel{}
	sm.Swap(repl)
	require.Eventually(t, func() bool { return em.closed == 1 },
		time.Second, 5*time.Millisecond,
		"error stream releases the lease immediately — retired model is closable")
}

// errModel 直接返回错误，用于验证出错路径立即释放租约。
type errModel struct{ closed int }

func (m *errModel) Info() model.Info { return model.Info{Name: "err"} }
func (m *errModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	return nil, context.Canceled
}
func (m *errModel) Close() error { m.closed++; return nil }

// closeableModel 统计被 Close 的次数，用于验证"恰好一次"。
type closeableModel struct {
	closed int
}

func (m *closeableModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	close(ch)
	return ch, nil
}
func (m *closeableModel) Info() model.Info { return model.Info{Name: "closeable"} }
func (m *closeableModel) Close() error     { m.closed++; return nil }

// plainModel 没有 Close 方法——回收必须容忍这种模型。
type plainModel struct{}

func (plainModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	close(ch)
	return ch, nil
}
func (plainModel) Info() model.Info { return model.Info{Name: "plain"} }

// TestSwappableModel_OldModelRecycledAfterSwap 钉住换出的可关闭模型在无在途调用后被关闭，重复清扫保持幂等。
func TestSwappableModel_OldModelRecycledAfterSwap(t *testing.T) {
	first := &closeableModel{}
	sm := NewSwappableModel(first)

	sm.Swap(plainModel{})
	if first.closed != 1 {
		t.Fatalf("first.closed = %d, want 1 after swap with no in-flight", first.closed)
	}
	second := &closeableModel{}
	sm.Swap(second)
	if second.closed != 0 {
		t.Fatal("current model must never be closed by Swap")
	}
	sm.Swap(second)
	if second.closed != 0 {
		t.Fatalf("current model closed = %d, want 0", second.closed)
	}
}

// TestSwappableModel_InFlightGatesRecycle 钉住租约未归零时不得关闭换出的模型；直接置计数以模拟在途调用。
func TestSwappableModel_InFlightGatesRecycle(t *testing.T) {
	first := &closeableModel{}
	sm := NewSwappableModel(first)

	sm.inFlight.Store(1)
	sm.Swap(plainModel{})
	if first.closed != 0 {
		t.Fatalf("retired model closed during in-flight call: %d", first.closed)
	}
	sm.inFlight.Store(0)
	sm.sweepRetired()
	if first.closed != 1 {
		t.Fatalf("retired model closed = %d, want 1 after quiescence", first.closed)
	}
}

// TestSwappableModel_InfoTracksCurrentInner 钉住 Info 委托给当前内层模型：每次 Swap 之后身份随之改变，不存在缓存的旧身份。
func TestSwappableModel_InfoTracksCurrentInner(t *testing.T) {
	sm := NewSwappableModel(&mockModel{info: model.Info{Name: "first"}})
	require.Equal(t, "first", sm.Info().Name)

	sm.Swap(&mockModel{info: model.Info{Name: "second"}})
	require.Equal(t, "second", sm.Info().Name)

	sm.Swap(&mockModel{info: model.Info{Name: "third"}})
	require.Equal(t, "third", sm.Info().Name)
}

// TestSwappableModel_ReuseDuringReleaseWindowNotClosed 钉住 A→B→A 的回收竞态窗口：
// 旧实现在写锁**之前**快照 current——release 腿（sweep 开跑时 inner 仍是 B）与
// Swap(A) reuse 落地交错时，锁内扫描拿着陈旧值 B 把刚回归现任的 A 关掉。
// 确定性部分：reuse 与租约释放都完成后，终局 sweep 必须以锁内新鲜值保 A；
// 交错窗口本身由下面的 Concurrency 压测例覆盖（-race 下高频命中）。
func TestSwappableModel_ReuseDuringReleaseWindowNotClosed(t *testing.T) {
	a := &slowStreamModel{}
	b := &slowStreamModel{}
	sm := NewSwappableModel(a)

	ctx := context.Background()
	chA, err := sm.GenerateContent(ctx, &model.Request{})
	require.NoError(t, err)
	require.NotNil(t, chA, "an open stream holds a lease")

	sm.Swap(b) // retired=[A]，A 仍被在途流引用
	sm.Swap(a) // A 回归现任；retired=[A,B]

	a.endStream() // 租约释放，release 腿触发 sweep
	require.Eventually(t, func() bool { return sm.inFlight.Load() == 0 }, time.Second, 10*time.Millisecond)

	sm.sweepRetired() // 终局清扫：current 必须取锁内新鲜值（A），retired 中的 B 回收
	require.Zero(t, a.closedCount(), "the reused current inner must never be closed by a sweep")
	require.Equal(t, 1, b.closedCount(), "B (retired, no lease) gets recycled once")

	_, err = sm.GenerateContent(ctx, &model.Request{})
	require.NoError(t, err, "requests keep working on the reused model")
	a.endStream()
	require.Eventually(t, func() bool { return sm.inFlight.Load() == 0 }, time.Second, 10*time.Millisecond)
	require.Zero(t, a.closedCount(), "quiescence must not close the current inner either")
}
