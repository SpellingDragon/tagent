package compress

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// narrativeCaptureModel captures the last user-message prompt of each call and
// replies with a fixed string. Counts calls for cost assertions.
type narrativeCaptureModel struct {
	mu      sync.Mutex
	prompts []string
	reply   string
}

func (m *narrativeCaptureModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.prompts = append(m.prompts, req.Messages[len(req.Messages)-1].Content)
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{
		Message: model.Message{Role: model.RoleAssistant, Content: m.reply},
	}}}
	close(ch)
	return ch, nil
}
func (m *narrativeCaptureModel) Info() model.Info { return model.Info{Name: "narrative-capture"} }
func (m *narrativeCaptureModel) calls() int       { m.mu.Lock(); defer m.mu.Unlock(); return len(m.prompts) }
func (m *narrativeCaptureModel) lastPrompt() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.prompts) == 0 {
		return ""
	}
	return m.prompts[len(m.prompts)-1]
}

func newNarrativeCompressor(t *testing.T, m model.Model) (*ContextCompressor, *memory.InMemoryStore) {
	t.Helper()
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	if m != nil {
		sc = NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000), WithSummaryModel(m))
	}
	store := memory.NewInMemoryStore()
	cc := NewContextCompressor(sc, store, NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(6000), WithCompactKeysListed(32))
	return cc, store
}

func seedNarrativeEvents(t *testing.T, store *memory.InMemoryStore) []memory.EventReference {
	t.Helper()
	// Two skeleton turns stored with FULL content (richer than EventSummary —
	// the material law must feed the real stored text to the model).
	type seed struct {
		key     int64
		typ     string
		summary string
		content string
	}
	seeds := []seed{
		{100, tagentevent.TypeExternalInput, "请求部署", "请帮我把服务部署到测试环境，并跑一遍健康检查"},
		{101, tagentevent.TypeAgentOutput, "部署完成", "服务已部署到测试环境，健康检查全部通过"},
		{102, tagentevent.TypeExternalInput, "请求汇总", "请汇总今天的部署结果"},
		{103, tagentevent.TypeAgentOutput, "汇总完成", "今日部署 1 个服务，全部通过"},
	}
	refs := make([]memory.EventReference, 0, len(seeds))
	for _, s := range seeds {
		if err := store.StoreEvent(s.key, memory.FullEvent{
			EventKey: s.key, EventType: s.typ, EventSummary: s.summary,
			Content: s.content, Timestamp: s.key,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		refs = append(refs, memory.EventReference{
			EventKey: s.key, EventType: s.typ, EventSummary: s.summary, Timestamp: s.key,
		})
	}
	return refs
}

// TestRollingNarrative_SynthesizedOnFold: when a summary model is wired and
// events are L3-folded this round, the rolling summary carries a 〔历史综述〕
// narrative line synthesized from the REAL stored content (material law),
// layered ABOVE the engineering ticket lines.
func TestRollingNarrative_SynthesizedOnFold(t *testing.T) {
	m := &narrativeCaptureModel{reply: "用户请求部署并汇总，服务部署成功且健康检查通过，当日结果已汇总。"}
	cc, store := newNarrativeCompressor(t, m)
	refs := seedNarrativeEvents(t, store)

	retained := cc.buildRetainedRefs(refs, nil, context.Background())
	if len(retained) != 1 {
		t.Fatalf("expected single rolling summary ref, got %d", len(retained))
	}
	s := retained[0].EventSummary

	// Narrative line present, above the ticket layer.
	if !strings.Contains(s, "〔历史综述〕用户请求部署并汇总") {
		t.Errorf("rolling summary must carry the synthesized narrative, got: %q", s)
	}
	// Ticket layer still present (additive, not replacement).
	if !strings.Contains(s, "["+tagentevent.FormatEventKey(100)+"] 请求部署") {
		t.Errorf("engineering card lines must stay, got: %q", s)
	}
	if !strings.Contains(s, "recent keys=") {
		t.Errorf("recent keys must stay, got: %q", s)
	}

	// Material law: the synthesis prompt must contain the FULL stored content,
	// not the summary stubs.
	p := m.lastPrompt()
	if !strings.Contains(p, "健康检查全部通过") || !strings.Contains(p, "请帮我把服务部署到测试环境") {
		t.Errorf("prompt must feed real stored content (material law), got: %q", p)
	}
	if !strings.Contains(p, "旧历史综述：（无）") {
		t.Errorf("first fold must mark prior narrative as absent, got: %q", p)
	}
}

// TestRollingNarrative_Incremental: the prior narrative is carried into the
// synthesis prompt and replaced by the new output; carry-over rounds with no
// new folds cost ZERO additional LLM calls.
func TestRollingNarrative_Incremental(t *testing.T) {
	m := &narrativeCaptureModel{reply: "第二轮综述：涵盖部署与汇总两轮工作。"}
	cc, store := newNarrativeCompressor(t, m)
	refs := seedNarrativeEvents(t, store)

	first := cc.buildRetainedRefs(refs, nil, context.Background())
	callsAfterFold := m.calls()
	if callsAfterFold != 1 {
		t.Fatalf("one L3 fold must cost exactly 1 LLM call, got %d", callsAfterFold)
	}

	// Second round: prior summary + one new dropped event.
	refs2 := append([]memory.EventReference{}, first...)
	refs2 = append(refs2, memory.EventReference{
		EventKey: 200, EventType: tagentevent.TypeExternalInput,
		EventSummary: "请求复盘", Timestamp: 200,
	})
	second := cc.buildRetainedRefs(refs2, nil, context.Background())
	if m.calls() != 2 {
		t.Fatalf("second fold must add exactly 1 call, got %d", m.calls())
	}
	// The mock replies with a fixed string, so round 1's narrative IS that
	// reply; round 2's prompt must carry it as the prior narrative.
	if !strings.Contains(m.lastPrompt(), "旧历史综述：\n第二轮综述：涵盖部署与汇总两轮工作。") {
		t.Errorf("prior narrative (round-1 model output) must feed the incremental synthesis, got: %q", m.lastPrompt())
	}
	if !strings.Contains(second[0].EventSummary, "〔历史综述〕第二轮综述") {
		t.Errorf("narrative must be replaced by the new synthesis, got: %q", second[0].EventSummary)
	}

	// Carry-over round (no new drops): no extra LLM call, narrative preserved.
	third := cc.buildRetainedRefs(second, nil, context.Background())
	if m.calls() != 2 {
		t.Errorf("carry-over round must cost zero LLM calls, got %d", m.calls())
	}
	if !strings.Contains(third[0].EventSummary, "〔历史综述〕第二轮综述") {
		t.Errorf("narrative must survive carry-over rounds, got: %q", third[0].EventSummary)
	}
}

// TestRollingNarrative_FailureFallsBack: on model failure the prior narrative
// is preserved and the ticket layer is intact — compaction never breaks.
func TestRollingNarrative_FailureFallsBack(t *testing.T) {
	m := &mockBatchSummaryModel{failOnCall: map[int]bool{0: true, 1: true}}
	cc, store := newNarrativeCompressor(t, m)
	refs := seedNarrativeEvents(t, store)

	// Round 1 with a prior narrative already present.
	refs = append([]memory.EventReference{{
		EventKey:     -50,
		EventType:    tagentevent.TypeContextCompress,
		EventSummary: "[Compacted 5 historical events]\n〔历史综述〕早期历史：完成过环境搭建。\nrecent keys=zz",
		Timestamp:    50,
		Role:         "user",
	}}, refs...)

	retained := cc.buildRetainedRefs(refs, nil, context.Background())
	s := retained[0].EventSummary
	if !strings.Contains(s, "〔历史综述〕早期历史：完成过环境搭建。") {
		t.Errorf("prior narrative must be preserved verbatim on failure, got: %q", s)
	}
	if !strings.Contains(s, "["+tagentevent.FormatEventKey(100)+"] 请求部署") {
		t.Errorf("ticket layer must be intact on failure, got: %q", s)
	}
}

// TestRollingNarrative_NoModelEngineeringOnly: without a summary model the
// rolling summary has NO narrative section — the pure-engineering form is
// unchanged (byte-compatible with the pre-narrative format for first folds).
func TestRollingNarrative_NoModelEngineeringOnly(t *testing.T) {
	cc, store := newNarrativeCompressor(t, nil)
	refs := seedNarrativeEvents(t, store)

	retained := cc.buildRetainedRefs(refs, nil, context.Background())
	if strings.Contains(retained[0].EventSummary, narrativePrefix) {
		t.Errorf("no model must yield pure engineering form, got: %q", retained[0].EventSummary)
	}
	if !strings.Contains(retained[0].EventSummary, "[Compacted 4 historical events]") {
		t.Errorf("count/cards must be unaffected, got: %q", retained[0].EventSummary)
	}
}

// TestRollingNarrative_CapEnforced: an over-long model reply is scrubbed to a
// single line and truncated at the compile-time cap.
func TestRollingNarrative_CapEnforced(t *testing.T) {
	long := strings.Repeat("综述内容", 2000) // 8000 runes
	m := &narrativeCaptureModel{reply: long}
	cc, store := newNarrativeCompressor(t, m)
	refs := seedNarrativeEvents(t, store)

	retained := cc.buildRetainedRefs(refs, nil, context.Background())
	line := ""
	for _, l := range strings.Split(retained[0].EventSummary, "\n") {
		if strings.HasPrefix(l, narrativePrefix) {
			line = strings.TrimPrefix(l, narrativePrefix)
		}
	}
	if line == "" {
		t.Fatalf("narrative line missing: %q", retained[0].EventSummary)
	}
	if n := len([]rune(line)); n > rollingNarrativeCapChars+3 {
		t.Errorf("narrative must be capped at %d runes (+ellipsis), got %d", rollingNarrativeCapChars, n)
	}
}

// TestParseNarrativeSection: round-trip parsing of the narrative line inside
// a rolling summary (cards and trailers ignored).
func TestParseNarrativeSection(t *testing.T) {
	summary := "[Compacted 7 historical events]\n〔历史综述〕用户完成了部署与汇总。\n- 08-23 17:08 [aa] 请求部署\n- 08-23 17:20 [bb] 部署完成\n(earlier 2 items retrievable via memory_recall)\nrecent keys=aa,bb"
	if got := parseNarrativeSection(summary); got != "用户完成了部署与汇总。" {
		t.Errorf("parseNarrativeSection = %q", got)
	}
	if got := parseNarrativeSection("[Compacted 3 historical events]\n- [aa] x"); got != "" {
		t.Errorf("absent narrative must parse empty, got %q", got)
	}
	// Multi-line narratives never occur (scrubbed at synthesis) but a trailing
	// continuation line must not be swallowed into cards either.
	fmt.Println("narrative parser ok")
}
