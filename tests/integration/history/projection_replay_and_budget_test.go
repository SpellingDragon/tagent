package history

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// durableChain adapts one tier's fact log to the replay.EventChain port: the
// facts a process committed survive its exit, so a rebuild after reopening the
// same tier replays exactly what a fresh process sees.
type durableChain struct {
	log *ledger.TierFacts
}

var _ replay.EventChain = durableChain{}

const chainPrefix = "evt:"

func (c durableChain) commit(ctx context.Context, fact replay.ChainFact) error {
	payload, err := json.Marshal(fact)
	if err != nil {
		return err
	}
	if err := c.log.Put(ctx, chainPrefix+string(fact.Key), payload); err != nil {
		return err
	}
	return c.log.Sync(ctx)
}

func (c durableChain) ScanFrom(ctx context.Context, after event.EventID, limit int) ([]replay.ChainFact, error) {
	if limit < 1 {
		return nil, replay.ErrChainPageLimit
	}
	pairs, err := c.log.Scan(ctx, chainPrefix)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(pairs))
	for key := range pairs {
		if key > chainPrefix+string(after) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]replay.ChainFact, 0, len(keys))
	for _, key := range keys {
		var fact replay.ChainFact
		if err := json.Unmarshal(pairs[key], &fact); err != nil {
			return nil, err
		}
		out = append(out, fact)
	}
	return out, nil
}

func (c durableChain) Fact(ctx context.Context, key event.EventID) (replay.ChainFact, bool, error) {
	value, held, err := c.log.Get(ctx, chainPrefix+string(key))
	if err != nil || !held {
		return replay.ChainFact{}, false, err
	}
	var fact replay.ChainFact
	if err := json.Unmarshal(value, &fact); err != nil {
		return replay.ChainFact{}, false, err
	}
	return fact, true, nil
}

// TestProjectionReplayAndBudget proves the projection, its presentation decisions and the budget assembly on the frozen 03.4 face.
//
//   - 逐字节重建 snapshot+尾部: the snapshot and a multi-page tail rebuild byte for byte
//   - 无 compaction 有界 fallback: an unfactored chain replays under the bound, never empty
//   - 大结果仅票据不复发全文: a large result is presented as a ticket only
//   - 预算组装含恢复提示: the assembly after a rebuild carries the recovery hint first
//   - 同 session 因果链不跨会话误连: the causal chain never crosses sessions
//   - 投影进入实际请求: the replayed projection is assembled into the actual request
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestProjectionReplayAndBudget(t *testing.T) {
	ctx := context.Background()
	session := replay.ChainKey{PartitionID: 1, SessionID: event.SessionID("s1")}
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	at := func(step int) time.Time { return base.Add(time.Duration(step) * time.Minute) }

	factOf := func(key, typ, summary, content string, when time.Time) replay.ChainFact {
		return replay.ChainFact{
			Key:     event.EventID(key),
			Chain:   session,
			Type:    typ,
			Summary: summary,
			Content: content,
			Time:    when,
		}
	}
	entryOf := func(fact replay.ChainFact) replay.ProjectionEntry {
		return replay.ProjectionEntry{Key: fact.Key, Type: fact.Type, Summary: fact.Summary, Content: fact.Content, Time: fact.Time}
	}

	t.Run("G2.1 逐字节重建 snapshot+尾部: snapshot and multi-page tail rebuild byte for byte", func(t *testing.T) {
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
		tier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("OpenTier: %v", err)
		}
		chain := durableChain{log: ledger.TierFactLog(tier)}
		run := []replay.ChainFact{
			factOf("k-0001", event.TypeExternalInput, "user asks", "请分析这份报告并给出结论，注意第三段的数据异常。", at(1)),
			factOf("k-0002", event.TypeAgentOutput, "analysis result", strings.Repeat("分析结果正文，与摘要完全不同。", 8), at(2)),
			factOf("k-0003", event.TypeActionCommand, "tool call grep", "grep -n TODO src/*.go", at(3)),
			factOf("k-0004", event.TypeThinkingPlan, "plan step", "先看目录结构，再读入口文件。", at(4)),
		}
		toolChain := replay.ProjectionEntry{
			Type:    event.TypeActionCommand,
			Summary: "tool chain",
			Content: "- 工具链: grep -n TODO → sed -n 1,20p",
			Time:    at(3),
		}
		tail := []replay.ChainFact{
			factOf("k-0005", event.TypeAgentOutput, "tail output", "尾部输出正文，绝不等于摘要。", at(5)),
			factOf("k-0006", event.TypeExternalInput, "tail input", "尾部输入原文。", at(6)),
			factOf("k-0007", event.TypeThinkingPlan, "tail plan", "尾部冥想正文。", at(7)),
		}

		projA := replay.NewReplayProjection()
		for _, fact := range run {
			projA.Append(ctx, entryOf(fact))
		}
		projA.Append(ctx, toolChain)
		summary, err := projA.Fold(ctx)
		if err != nil {
			t.Fatalf("Fold: %v", err)
		}
		if summary.Type != replay.CompactionEventType {
			t.Fatalf("fold summary type = %q, want %q", summary.Type, replay.CompactionEventType)
		}
		for _, fact := range tail {
			projA.Append(ctx, entryOf(fact))
		}
		payload := projA.Compaction()
		if payload == nil {
			t.Fatal("Fold left no compaction payload on the projection")
		}
		baseline := replay.RenderProjection(projA.Entries())

		for _, fact := range run {
			if err := chain.commit(ctx, fact); err != nil {
				t.Fatalf("commit %s: %v", fact.Key, err)
			}
		}
		compactionBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal compaction: %v", err)
		}
		if err := chain.commit(ctx, replay.ChainFact{
			Key:     event.EventID("k-0004c"),
			Chain:   session,
			Type:    replay.CompactionEventType,
			Summary: "compaction",
			Content: string(compactionBytes),
			Time:    at(4),
		}); err != nil {
			t.Fatalf("commit compaction: %v", err)
		}
		for _, fact := range tail {
			if err := chain.commit(ctx, fact); err != nil {
				t.Fatalf("commit %s: %v", fact.Key, err)
			}
		}

		reopened, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen OpenTier: %v", err)
		}
		reopenedChain := durableChain{log: ledger.TierFactLog(reopened)}
		projB := replay.NewReplayProjection()
		relations := replay.NewMemoryRelations()
		report, err := replay.RebuildProjectionFromWAL(ctx, projB, reopenedChain, relations, session, replay.RebuildOptions{PageSize: 1})
		if err != nil {
			t.Fatalf("RebuildProjectionFromWAL: %v", err)
		}
		if !report.Scanned || report.Fallback || report.Partial || report.Skipped != "" {
			t.Fatalf("rebuild report = %+v, want a clean scanned snapshot rebuild", report)
		}
		if !bytes.Equal(replay.RenderProjection(projB.Entries()), baseline) {
			t.Fatalf("rebuilt render differs from the pre-restart render:\n got: %s\nwant: %s",
				replay.RenderProjection(projB.Entries()), baseline)
		}
		if projB.Len() != projA.Len() {
			t.Fatalf("rebuilt entries = %d, want %d (snapshot refs and the whole tail)", projB.Len(), projA.Len())
		}
		if projB.FullBoundary() != event.EventID("k-0004") || report.FullBoundary != event.EventID("k-0004") {
			t.Fatalf("full boundary = %q/%q, want the payload value k-0004", projB.FullBoundary(), report.FullBoundary)
		}
		meditation := map[event.EventID]bool{}
		for _, key := range projB.ConsumedKeys() {
			meditation[key] = true
		}
		if !meditation["k-0004"] || !meditation["k-0007"] {
			t.Fatalf("meditation keys = %v, want both the snapshot and the tail keys reseeded", projB.ConsumedKeys())
		}
		if parent, err := relations.GetParent("k-0005"); err != nil || parent != event.EventID("k-0003") {
			t.Fatalf("tail link parent = %q (%v), want the last retained snapshot ref", parent, err)
		}
	})

	t.Run("G2.2 无 compaction 有界 fallback: an unfactored chain replays under the bound, never as empty", func(t *testing.T) {
		chain := replay.NewMemoryEventChain()
		for i := 1; i <= 5; i++ {
			key := event.EventID("f-000" + string(rune('0'+i)))
			if err := chain.Commit(ctx, factOf(string(key), event.TypeAgentOutput, "out", "正文"+string(rune('0'+i)), at(i))); err != nil {
				t.Fatalf("Commit %s: %v", key, err)
			}
		}
		proj := replay.NewReplayProjection()
		report, err := replay.RebuildProjectionFromWAL(ctx, proj, chain, replay.NewMemoryRelations(), session, replay.RebuildOptions{FallbackBound: 3})
		if err != nil {
			t.Fatalf("RebuildProjectionFromWAL: %v", err)
		}
		if !report.Fallback || !report.Scanned || report.Dropped != 2 {
			t.Fatalf("fallback report = %+v, want fallback with 2 dropped", report)
		}
		if proj.Len() != 3 {
			t.Fatalf("fallback kept %d entries, want the 3 most recent", proj.Len())
		}
		kept := proj.Entries()
		if kept[0].Key != event.EventID("f-0003") || kept[2].Key != event.EventID("f-0005") {
			t.Fatalf("fallback kept %v, want the most recent run in write order", []event.EventID{kept[0].Key, kept[2].Key})
		}
	})

	t.Run("G2.3 大结果仅票据不复发全文: a large result is presented as a ticket, never re-carrying the full text", func(t *testing.T) {
		full := strings.Repeat("大结果正文。", 300)
		large := replay.ChainFact{Key: "big-1", Chain: session, Type: event.TypeAgentOutput, Summary: "big result", Content: full, Time: at(1)}
		proj := replay.NewReplayProjection()
		proj.Append(ctx, entryOf(large))

		first, err := proj.AssembleBudget(ctx, 200)
		if err != nil {
			t.Fatalf("AssembleBudget: %v", err)
		}
		if len(first) != 1 {
			t.Fatalf("assembly returned %d entries, want 1", len(first))
		}
		view := first[0].Content
		if !strings.Contains(view, "[ticket big-1:") {
			t.Fatalf("ticket view = %q, want a ticket reaching the original by segment", view)
		}
		if strings.Contains(view, full) || len(view) > 512 {
			t.Fatalf("ticket view re-carry the full text (%d bytes), want a bounded preview only", len(view))
		}
		if original, kept := proj.Original("big-1"); !kept || original != full {
			t.Fatalf("original layer = %d bytes kept=%v, want the full text kept apart from the view", len(original), kept)
		}

		second, err := proj.AssembleBudget(ctx, 200)
		if err != nil {
			t.Fatalf("second AssembleBudget: %v", err)
		}
		if second[0].Content != view {
			t.Fatalf("second assembly view = %q, want the same budget to recompute the same view", second[0].Content)
		}
		if strings.Contains(second[0].Content, full) {
			t.Fatal("second assembly re-carried the full text although the budget only fits the ticket")
		}
	})

	t.Run("G2.4 预算组装含恢复提示: the assembly after a rebuild carries the recovery hint first", func(t *testing.T) {
		chain := replay.NewMemoryEventChain()
		if err := chain.Commit(ctx, factOf("r-0001", event.TypeAgentOutput, "out", "恢复后的正文。", at(1))); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if err := chain.Commit(ctx, factOf("r-0002", event.TypeExternalInput, "in", "恢复后的输入。", at(2))); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		proj := replay.NewReplayProjection()
		report, err := replay.RebuildProjectionFromWAL(ctx, proj, chain, replay.NewMemoryRelations(), session, replay.RebuildOptions{})
		if err != nil {
			t.Fatalf("RebuildProjectionFromWAL: %v", err)
		}
		if !report.Fallback {
			t.Fatalf("rebuild report = %+v, want the bounded fallback recorded", report)
		}
		out, err := proj.AssembleBudget(ctx, 100)
		if err != nil {
			t.Fatalf("AssembleBudget: %v", err)
		}
		if len(out) < 3 {
			t.Fatalf("assembly returned %d entries, want the hint and the rebuilt entries", len(out))
		}
		if out[0].Type != replay.ProjectionTypeRecoveryHint || !strings.Contains(out[0].Content, "recovery:") {
			t.Fatalf("first assembled entry = %+v, want the recovery hint first", out[0])
		}
		hint := proj.RecoveryHint()
		if hint == nil || hint.Type != replay.ProjectionTypeRecoveryHint {
			t.Fatalf("RecoveryHint = %+v, want the recorded hint", hint)
		}
	})

	t.Run("G2.5 同 session 因果链不跨会话误连: the causal chain is isolated per session", func(t *testing.T) {
		other := replay.ChainKey{PartitionID: 1, SessionID: event.SessionID("s2")}
		chain := replay.NewMemoryEventChain()
		facts := []replay.ChainFact{
			factOf("s1-1", event.TypeExternalInput, "a", "会话一输入。", at(1)),
			{Key: "s2-1", Chain: other, Type: event.TypeExternalInput, Summary: "b", Content: "会话二输入。", Time: at(2)},
			factOf("s1-2", event.TypeAgentOutput, "c", "会话一输出。", at(3)),
			{Key: "s2-2", Chain: other, Type: event.TypeAgentOutput, Summary: "d", Content: "会话二输出。", Time: at(4)},
		}
		for _, fact := range facts {
			if err := chain.Commit(ctx, fact); err != nil {
				t.Fatalf("Commit %s: %v", fact.Key, err)
			}
		}
		relations := replay.NewMemoryRelations()
		proj := replay.NewReplayProjection()
		report, err := replay.RebuildProjectionFromWAL(ctx, proj, chain, relations, session, replay.RebuildOptions{})
		if err != nil {
			t.Fatalf("RebuildProjectionFromWAL: %v", err)
		}
		if report.Entries != 2 || proj.Len() != 2 {
			t.Fatalf("session s1 replayed %d entries, want only its own two", proj.Len())
		}
		for _, entry := range proj.Entries() {
			if strings.HasPrefix(string(entry.Key), "s2-") {
				t.Fatalf("session s1 replay carries foreign entry %s", entry.Key)
			}
		}
		parent, err := relations.GetParent("s1-2")
		if err != nil || parent != event.EventID("s1-1") {
			t.Fatalf("s1 chain parent = %q (%v), want s1-1 linked inside the session", parent, err)
		}
		if parent, err := relations.GetParent("s2-2"); err != nil || parent != "" {
			t.Fatalf("s2 chain parent = %q (%v), want no cross-session link", parent, err)
		}
		if _, err := replay.RebuildProjectionFromWAL(ctx, replay.NewReplayProjection(), chain, relations,
			replay.ChainKey{PartitionID: 1}, replay.RebuildOptions{}); !errors.Is(err, replay.ErrChainKeyRequired) {
			t.Fatalf("empty-session rebuild error = %v, want ErrChainKeyRequired", err)
		}
	})

	t.Run("G2.6 投影进入实际请求: the replayed projection is assembled into the actual request", func(t *testing.T) {
		chain := replay.NewMemoryEventChain()
		for _, fact := range []replay.ChainFact{
			factOf("q-0001", event.TypeExternalInput, "in", "进入请求的输入原文。", at(1)),
			factOf("q-0002", event.TypeThinkingPlan, "plan", "进入请求的思考正文。", at(2)),
			factOf("q-0003", event.TypeActionCommand, "call", "进入请求的工具调用。", at(3)),
		} {
			if err := chain.Commit(ctx, fact); err != nil {
				t.Fatalf("Commit %s: %v", fact.Key, err)
			}
		}
		proj := replay.NewReplayProjection()
		if _, err := replay.RebuildProjectionFromWAL(ctx, proj, chain, replay.NewMemoryRelations(), session, replay.RebuildOptions{}); err != nil {
			t.Fatalf("RebuildProjectionFromWAL: %v", err)
		}
		presented, err := proj.AssembleBudget(ctx, 100)
		if err != nil {
			t.Fatalf("AssembleBudget: %v", err)
		}
		provider := &replayModel{name: "model-a"}
		run, err := tagent.Assemble(ctx, &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
		}, map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider},
		}, tagent.WithGovernance(replayPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		if err := run.AttachReplay(agent.RequestChain{PartitionID: session.PartitionID, SessionID: session.SessionID}, tagent.FoldWindow{MaxTokens: 100}, proj); err != nil {
			t.Fatalf("AttachReplay: %v", err)
		}
		if _, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-replay-1",
			Address:      event.Address{AgentID: "agent-a", SessionID: session.SessionID},
			Input:        []model.Message{{Role: model.RoleUser, Content: "请求带投影。"}},
		}); err != nil {
			t.Fatalf("invoke the formal request: %v", err)
		}
		requests := provider.recorded()
		if len(requests) != 1 {
			t.Fatalf("recorded requests = %d, want exactly one formal request", len(requests))
		}
		got := requests[0].Messages
		if len(got) != len(presented)+1 {
			t.Fatalf("request carries %d messages, want %d presented entries in front of the transcript", len(got), len(presented))
		}
		sawInput, sawPlan, sawTool := false, false, false
		for i, message := range got[:len(presented)] {
			if message.Content != presented[i].Content {
				t.Fatalf("message %d content = %q, want the presented view %q", i, message.Content, presented[i].Content)
			}
			switch presented[i].Type {
			case event.TypeExternalInput:
				sawInput = message.Role == "user"
			case event.TypeThinkingPlan:
				sawPlan = message.Role == "assistant"
			case event.TypeActionCommand:
				sawTool = message.Role == "tool"
			}
		}
		if !sawInput || !sawPlan || !sawTool {
			t.Fatalf("role mapping = input:%v plan:%v tool:%v, want the frozen types mapped onto user/assistant/tool", sawInput, sawPlan, sawTool)
		}
		if err := run.AttachReplay(agent.RequestChain{PartitionID: session.PartitionID}, tagent.FoldWindow{MaxTokens: 100}, proj); !errors.Is(err, agent.ErrRequestChainSessionRequired) {
			t.Fatalf("sessionless registration error = %v, want ErrRequestChainSessionRequired", err)
		}
	})
}

// replayPolicy is the deterministic stand-in governance of this assembly: the
// three required checks each carry a passing disposition, which is a mechanism
// input with no quality conclusion.
type replayPolicy struct{}

// Check implements state.Governance beside the fixed passing dispositions.
func (replayPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       {Name: "eval", Verdict: state.CheckPass},
		"benchmark":  {Name: "benchmark", Verdict: state.CheckPass},
		"apply-gate": {Name: "apply-gate", Verdict: state.CheckPass},
	}, nil
}

// replayModel is the scripted provider stand-in: it records every native
// request it is driven with and answers with a fixed final. It never reaches a
// real endpoint.
type replayModel struct {
	name     string
	requests []model.Request
}

// GenerateContent records the native request and streams the fixed final.
func (m *replayModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.requests = append(m.requests, *req)
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
		Done:    true,
	}
	close(ch)
	return ch, nil
}

// Info reports the scripted provider's descriptor.
func (m *replayModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// recorded reports the native requests the fake provider was driven with.
func (m *replayModel) recorded() []model.Request {
	return append([]model.Request(nil), m.requests...)
}
