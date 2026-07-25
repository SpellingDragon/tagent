package compress

import (
	"context"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// countingSummaryModel counts GenerateContent calls and returns a fixed
// summary (satisfies model.Model for L3 summarization).
type countingSummaryModel struct {
	calls int
}

func (m *countingSummaryModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.calls++
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: "固定段摘要: 任务完成",
	}}}}
	close(ch)
	return ch, nil
}

func (m *countingSummaryModel) Info() model.Info { return model.Info{Name: "counting-summary"} }

// buildL3CorpusMsgs builds messages where segment 0 is an OLD tool-only
// segment (no user input, age ≥ 3) so plan assignment reaches L3, followed
// by four user-bounded fresh segments.
func buildL3CorpusMsgs() []model.Message {
	msgs := []model.Message{
		// Segment 0: no user input (assistant-only, task-bounded).
		{Role: model.RoleAssistant, Content: prefixEventKey("旧工具轮次输出", memory.EventReference{EventKey: 1000, EventType: tagentevent.TypeThinkingPlan})},
		{Role: model.RoleAssistant, Content: prefixEventKey("旧任务完成", memory.EventReference{EventKey: 1001, EventType: tagentevent.TypeAgentOutput})},
	}
	for i := int64(0); i < 4; i++ {
		base := 2000 + i*10
		msgs = append(msgs,
			model.Message{Role: model.RoleUser, Content: prefixEventKey("请求", memory.EventReference{EventKey: base, EventType: tagentevent.TypeExternalInput})},
			model.Message{Role: model.RoleAssistant, Content: prefixEventKey("完成", memory.EventReference{EventKey: base + 1, EventType: tagentevent.TypeAgentOutput})},
		)
	}
	return msgs
}

// TestArchiveSegment_CausalMountAndProvenance: the archived summary is
// mounted on the causal chain (parent = segment tail event) and records its
// source key set (I7).
func TestArchiveSegment_CausalMountAndProvenance(t *testing.T) {
	store := memory.NewInMemoryStore()
	sc := NewSmartCompressor(WithMemStore(store))

	seg := &TaskSegment{Messages: []model.Message{
		{Role: model.RoleUser, Content: prefixEventKey("hello", memory.EventReference{EventKey: 100, EventType: tagentevent.TypeExternalInput})},
		{Role: model.RoleAssistant, Content: prefixEventKey("done", memory.EventReference{EventKey: 200, EventType: tagentevent.TypeAgentOutput})},
	}}

	summaryKey, err := sc.archiveSegment(seg, "段摘要")
	if err != nil {
		t.Fatalf("archiveSegment: %v", err)
	}

	// Causal mount: parent = tail event key (200).
	parent, err := store.RelationStore().GetParent(summaryKey)
	if err != nil || parent != 200 {
		t.Errorf("summary must mount on causal chain with parent=200, got %d err=%v", parent, err)
	}

	// Provenance: source_keys metadata records the hex key set.
	full, err := store.GetEvent(summaryKey)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	want := tagentevent.FormatEventKey(100) + "," + tagentevent.FormatEventKey(200)
	if full.Metadata["source_keys"] != want {
		t.Errorf("source_keys = %q, want %q", full.Metadata["source_keys"], want)
	}
}

// TestArchiveCache_SameSegmentNotResummarized: compressing the same old
// segment across two rounds must call the summary LLM exactly once (material
// law: cost stays O(new segments)).
func TestArchiveCache_SameSegmentNotResummarized(t *testing.T) {
	store := memory.NewInMemoryStore()
	sm := &countingSummaryModel{}
	sc := NewSmartCompressor(
		WithMemStore(store),
		WithSummaryModel(sm),
		WithKeepRecentTasks(1),
		WithMaxTokens(1),
	)

	msgs := buildL3CorpusMsgs()

	_ = sc.Compress(context.Background(), msgs, nil)
	callsAfterFirst := sm.calls
	if callsAfterFirst == 0 {
		t.Fatalf("first round should summarize the old segment at least once")
	}

	// Second round over the same messages: L1/L2 view-level summaries are
	// recomputed by design (view transforms), but the L3 ARCHIVE hit the cache
	// — exactly one fewer LLM call than the first round.
	_ = sc.Compress(context.Background(), msgs, nil)
	secondRoundCalls := sm.calls - callsAfterFirst
	if secondRoundCalls != callsAfterFirst-1 {
		t.Errorf("L3 archive must hit cache on round 2: first=%d second=%d (want second = first-1)", callsAfterFirst, secondRoundCalls)
	}
}
