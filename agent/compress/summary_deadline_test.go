// 本文件钉住「同步摘要可界定等待」：一轮真折叠的卡片浓缩与滚动叙事共用一个带
// deadline 的子 context；父 ctx 取消让当轮摘要立即失败而不是挂起；超时与迟到结果
// 一律丢弃并走纯工程降级（卡片下沉、旧叙事保留），晚到的模型文本不回写投影。
// 摘要不重试、不异步。
// 契约: docs/wiki/agent/compression-and-telemetry.md#summary-deadline
package compress

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	memory "github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const dlPriorNarrative = "PRIOR_NARRATIVE 早期对话已归档"

// dlRecord is one observed summary call.
type dlRecord struct {
	hasDeadline bool
	deadline    time.Time
	start       time.Time
}

// dlStallingModel never delivers a response and never closes its channel: the
// only way the fold can finish is by honouring the caller's context.
type dlStallingModel struct {
	mu    sync.Mutex
	calls []dlRecord
}

func (m *dlStallingModel) GenerateContent(ctx context.Context, _ *model.Request) (<-chan *model.Response, error) {
	rec := dlRecord{start: time.Now()}
	rec.deadline, rec.hasDeadline = ctx.Deadline()
	m.mu.Lock()
	m.calls = append(m.calls, rec)
	m.mu.Unlock()
	return make(chan *model.Response, 1), nil
}

func (m *dlStallingModel) Info() model.Info { return model.Info{Name: "dl-stalling"} }

func (m *dlStallingModel) snapshot() []dlRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]dlRecord(nil), m.calls...)
}

// dlLateModel answers AFTER the shared deadline has already passed. The answer
// must be dropped by the caller, never written into the projection.
type dlLateModel struct {
	delay time.Duration
	reply string

	mu    sync.Mutex
	calls int
}

func (m *dlLateModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	go func() {
		time.Sleep(m.delay)
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant, Content: m.reply,
		}}}}
	}()
	return ch, nil
}

func (m *dlLateModel) Info() model.Info { return model.Info{Name: "dl-late"} }

func (m *dlLateModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// dlFixture drives a REAL fold: history above the trigger line, an over-cap card
// section (⇒ condensation attempt) and dropped skeleton events (⇒ narrative
// attempt), on top of a prior rolling summary that carries a narrative line.
func dlFixture(m model.Model, timeout time.Duration) (*ContextCompressor, []memory.EventReference) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	if m != nil {
		sc = NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000), WithSummaryModel(m))
	}
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(),
		1000, 0.8, 1,
		WithCardMaxChars(150), WithCompactKeysListed(8), WithSummaryTimeout(timeout))
	refs := []memory.EventReference{{
		EventKey:  -5000,
		EventType: tagentevent.TypeContextCompress,
		EventSummary: "[Compacted 3 historical events]\n" +
			narrativePrefix + dlPriorNarrative,
		Timestamp: 1_600_000_000_000,
		Role:      "user",
	}}
	return cc, append(refs, rbRefs(6)...)
}

// dlSummaryText returns the rolling-summary payload of a retained list.
func dlSummaryText(t *testing.T, refs []memory.EventReference) string {
	t.Helper()
	for _, r := range refs {
		if r.EventKey < 0 && r.EventType == tagentevent.TypeContextCompress {
			return r.EventSummary
		}
	}
	t.Fatal("a real fold must leave exactly one rolling summary ref")
	return ""
}

// runCompress guarded so an unbounded summary round shows up as a FAILURE
// instead of wedging the package binary.
func runCompress(t *testing.T, cc *ContextCompressor, ctx context.Context, refs []memory.EventReference, wait time.Duration) (CompressResult, time.Duration) {
	t.Helper()
	type outcome struct {
		res  CompressResult
		took time.Duration
	}
	done := make(chan outcome, 1)
	go func() {
		start := time.Now()
		res := cc.Compress(ctx, refs)
		done <- outcome{res: res, took: time.Since(start)}
	}()
	select {
	case o := <-done:
		return o.res, o.took
	case <-time.After(wait):
		t.Fatalf("Compress hung for %s: the synchronous summary round is unbounded", wait)
		return CompressResult{}, wait
	}
}

func TestSummaryDeadline_SharedAcrossCardAndNarrative(t *testing.T) {
	const timeout = 200 * time.Millisecond
	m := &dlStallingModel{}
	cc, refs := dlFixture(m, timeout)

	res, took := runCompress(t, cc, context.Background(), refs, 5*time.Second)

	calls := m.snapshot()
	if len(calls) < 2 {
		t.Fatalf("fixture must attempt both card condensation and narrative, got %d calls", len(calls))
	}
	for i, c := range calls {
		if !c.hasDeadline {
			t.Fatalf("call %d ran without a deadline: the summary round is unbounded", i)
		}
		if !c.deadline.Equal(calls[0].deadline) {
			t.Fatalf("call %d got its own deadline (%v vs %v): one real fold must share ONE sub-context",
				i, c.deadline.Format(time.RFC3339Nano), calls[0].deadline.Format(time.RFC3339Nano))
		}
	}
	if took >= 2*timeout {
		t.Fatalf("sequential timeouts instead of a shared one: took %s, deadline %s", took, timeout)
	}
	if !res.Compressed {
		t.Fatal("the fold must still complete engineering-side")
	}
	text := dlSummaryText(t, res.RetainedRefs)
	if !strings.Contains(text, dlPriorNarrative) {
		t.Fatalf("旧叙事必须保留, got %q", text)
	}
	if !strings.Contains(text, "earlier") {
		t.Fatalf("超时后卡片必须下沉为票据计数, got %q", text)
	}
}

func TestSummaryDeadline_ParentCancelStopsRound(t *testing.T) {
	m := &dlStallingModel{}
	cc, refs := dlFixture(m, 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	res, took := runCompress(t, cc, ctx, refs, 3*time.Second)

	if took > 2*time.Second {
		t.Fatalf("parent cancellation must abort the summary round promptly, took %s", took)
	}
	if !res.Compressed {
		t.Fatal("fold must degrade, not stall or skip")
	}
	if text := dlSummaryText(t, res.RetainedRefs); !strings.Contains(text, dlPriorNarrative) {
		t.Fatalf("prior narrative must survive a cancelled summary call, got %q", text)
	}
}

func TestSummaryDeadline_LateAnswerNeverRewrites(t *testing.T) {
	m := &dlLateModel{delay: 150 * time.Millisecond, reply: "LATE_NARRATIVE_TEXT"}
	cc, refs := dlFixture(m, 40*time.Millisecond)

	res, took := runCompress(t, cc, context.Background(), refs, 3*time.Second)

	if took > time.Second {
		t.Fatalf("the round waited for a late answer instead of degrading: took %s", took)
	}
	text := dlSummaryText(t, res.RetainedRefs)
	if strings.Contains(text, "LATE_NARRATIVE_TEXT") {
		t.Fatalf("a late answer reached the projection: %q", text)
	}
	if !strings.Contains(text, dlPriorNarrative) {
		t.Fatalf("degradation must keep the old narrative, got %q", text)
	}
	if m.count() < 1 {
		t.Fatal("fixture did not reach the summary model")
	}
}

func TestSummaryDeadline_InjectedValueReachesCall(t *testing.T) {
	answering := &dlReplyModel{reply: "- 即时综述"}
	cc, refs := dlFixture(answering, 3*time.Second)

	runCompress(t, cc, context.Background(), refs, 3*time.Second)

	got := answering.snapshot()
	if len(got) == 0 {
		t.Fatal("summary model was never consulted")
	}
	for _, c := range got {
		if !c.hasDeadline {
			t.Fatal("injected timeout must produce a deadline context")
		}
		if skew := c.deadline.Sub(c.start); skew < 2*time.Second || skew > 4*time.Second {
			t.Fatalf("deadline must be start+injected 3s, got %s", skew)
		}
	}
}

func TestSummaryDeadline_DefaultWhenNonPositive(t *testing.T) {
	if DefaultSummaryTimeout != 5*time.Second {
		t.Fatalf("default summary deadline must be 5s, got %s", DefaultSummaryTimeout)
	}
	sc := NewSmartCompressor(WithKeepRecentTasks(1))
	for _, d := range []time.Duration{0, -time.Second} {
		cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(),
			1000, 0.8, 1, WithSummaryTimeout(d))
		if got := cc.SummaryTimeout(); got != DefaultSummaryTimeout {
			t.Fatalf("WithSummaryTimeout(%v) must fall back to the default, got %v", d, got)
		}
	}
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(),
		1000, 0.8, 1, WithSummaryTimeout(7*time.Second))
	if got := cc.SummaryTimeout(); got != 7*time.Second {
		t.Fatalf("injected value must be readable, got %v", got)
	}
	cc.SetSummaryTimeout(0)
	if got := cc.SummaryTimeout(); got != DefaultSummaryTimeout {
		t.Fatalf("SetSummaryTimeout(0) must not mean 'no limit', got %v", got)
	}
}

// dlReplyModel answers immediately and records the deadline of every call.
type dlReplyModel struct {
	reply string

	mu    sync.Mutex
	calls []dlRecord
}

func (m *dlReplyModel) GenerateContent(ctx context.Context, _ *model.Request) (<-chan *model.Response, error) {
	rec := dlRecord{start: time.Now()}
	rec.deadline, rec.hasDeadline = ctx.Deadline()
	m.mu.Lock()
	m.calls = append(m.calls, rec)
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: m.reply,
	}}}}
	close(ch)
	return ch, nil
}

func (m *dlReplyModel) Info() model.Info { return model.Info{Name: "dl-reply"} }

func (m *dlReplyModel) snapshot() []dlRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]dlRecord(nil), m.calls...)
}
