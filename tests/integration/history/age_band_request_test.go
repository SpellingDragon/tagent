package history

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// ageChainKey is the session the age-band invariants replay their turns on.
var ageChainKey = replay.ChainKey{PartitionID: 1, SessionID: event.SessionID("age-s1")}

// ageBandBase is the pass time the age-band turns are laid out from.
var ageBandBase = time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)

// ageBandFact is one fact of a replayed turn: the summary is the one-line
// reading an age fold keeps, and the content is the raw text it drops.
func ageBandFact(key, typ, summary, content string, step int) replay.ChainFact {
	return replay.ChainFact{
		Key:     event.EventID(key),
		Chain:   ageChainKey,
		Type:    typ,
		Summary: summary,
		Content: content,
		Time:    ageBandBase.Add(time.Duration(step) * time.Minute),
	}
}

// ageBandTurn lays one closed turn on the chain: the input that opens it, the
// process detail between the input and the output, and the output that closes.
// The keys carry the turn and the position in it, so the chain reads back in
// the order the turns ran.
func ageBandTurn(step int, seed string) []replay.ChainFact {
	type leg struct {
		suffix  string
		typ     string
		summary string
		body    string
	}
	legs := []leg{
		{"in", event.TypeExternalInput, "ask " + seed, "请求正文 " + seed + "。"},
		{"plan", event.TypeThinkingPlan, "plan " + seed, "思考正文 " + seed + "。"},
		{"call", event.TypeActionCommand, "call " + seed, "工具输出 " + seed + "。"},
		{"out", event.TypeAgentOutput, "answer " + seed, "回答正文 " + seed + "。"},
	}
	facts := make([]replay.ChainFact, 0, len(legs))
	for position, part := range legs {
		key := fmt.Sprintf("%02d-%d-%s", step, position, part.suffix)
		facts = append(facts, ageBandFact(key, part.typ, part.summary, strings.Repeat(part.body, 12), step*10+position))
	}
	return facts
}

// ageBandProjection replays the given facts into one projection over the age
// session, as a cold start reads them back.
func ageBandProjection(t *testing.T, facts []replay.ChainFact) *replay.ReplayProjection {
	t.Helper()
	ctx := context.Background()
	chain := replay.NewMemoryEventChain()
	for _, fact := range facts {
		if err := chain.Commit(ctx, fact); err != nil {
			t.Fatalf("Commit %s: %v", fact.Key, err)
		}
	}
	projection := replay.NewReplayProjection()
	if _, err := replay.RebuildProjectionFromWAL(ctx, projection, chain, &replay.MemoryRelations{}, ageChainKey, replay.RebuildOptions{}); err != nil {
		t.Fatalf("RebuildProjectionFromWAL: %v", err)
	}
	return projection
}

// ageBandRun assembles one runtime whose session carries the given projection,
// so the request the model is driven with is the consumer of the sequence.
func ageBandRun(t *testing.T, projection *replay.ReplayProjection) (*assembly.Run, *replayModel) {
	t.Helper()
	provider := &replayModel{name: "model-a"}
	run, err := assembly.Assemble(context.Background(), &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(replayPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	if err := run.AttachReplay(agent.RequestChain{PartitionID: ageChainKey.PartitionID, SessionID: ageChainKey.SessionID}, assembly.FoldWindow{MaxTokens: 1 << 16}, projection); err != nil {
		t.Fatalf("AttachReplay: %v", err)
	}
	return run, provider
}

// ageBandRequest drives one turn through the assembled runtime and returns the
// messages of the single formal request.
func ageBandRequest(t *testing.T, run *assembly.Run, provider *replayModel, input string) []model.Message {
	t.Helper()
	if _, err := run.Invoke(context.Background(), assembly.InvokeRequest{
		InvocationID: "inv-age",
		Address:      event.Address{AgentID: "agent-a", SessionID: ageChainKey.SessionID},
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	}); err != nil {
		t.Fatalf("invoke the formal request: %v", err)
	}
	requests := provider.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want exactly one formal request", len(requests))
	}
	return requests[0].Messages
}

// TestAgeBandsReachTheRequest pins the consumer of an age-band fold in the next request.
//   - one fold promotes the segments that left the recent window into the middle band, whose process detail folds into one segment product each
//   - a later fold promotes that material into the oldest band, since the bands follow the folds
//   - the fold settles once no segment outside the recent window carries process detail
//   - the settled projection reads as one history summary leading the request, stated by the one-line reads and the keys of the folded products
//   - the raw text of the folded bands never reaches the request, and the newest turn stays whole in front of the input
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestAgeBandsReachTheRequest(t *testing.T) {
	ctx := context.Background()
	var facts []replay.ChainFact
	for step, seed := range []string{"first", "second", "third", "fourth"} {
		facts = append(facts, ageBandTurn(step, seed)...)
	}
	projection := ageBandProjection(t, facts)

	plan := replay.AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600}
	first, err := projection.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if !first.Triggered || first.NewFolded != 6 || first.MidFolded != 0 {
		t.Fatalf("first fold report = %+v, want the six process entries of the three segments that left the window folded and no history summary minted yet", first)
	}
	if len(first.Products) != 3 || len(first.Requests) != 3 {
		t.Fatalf("first fold report = %+v, want one segment product per folded segment, each beside its judgment", first)
	}
	for _, product := range first.Products {
		if product.Type != replay.ProjectionTypeSegmentFold {
			t.Fatalf("first fold product %s = %q, want a segment fold", product.Key, product.Type)
		}
	}

	promoting := replay.AgeFoldReport{}
	settled := false
	for round := 2; round <= 4; round++ {
		report, err := projection.FoldByAge(ctx, plan)
		if err != nil {
			t.Fatalf("FoldByAge round %d: %v", round, err)
		}
		if !report.Triggered {
			settled = true
			break
		}
		promoting = report
	}
	if !settled {
		t.Fatal("the fold minted a product in every round up to the fourth, want the generational fold to settle")
	}
	if promoting.NewFolded != 0 || promoting.MidFolded != 9 || len(promoting.Products) != 1 || promoting.Products[0].Type != replay.ProjectionTypeHistorySummary {
		t.Fatalf("promoting fold report = %+v, want the nine entries the earlier fold left read as one history summary and no process detail folded twice", promoting)
	}

	entries := projection.Entries()
	if len(entries) != 5 || entries[0].Type != replay.ProjectionTypeHistorySummary {
		t.Fatalf("settled projection = %v, want the history summary followed by the four entries of the newest turn", projectionKeysOf(entries))
	}
	if string(entries[1].Key) != "03-0-in" || string(entries[4].Key) != "03-3-out" {
		t.Fatalf("settled projection = %v, want the newest turn kept whole behind the summary", projectionKeysOf(entries))
	}

	run, provider := ageBandRun(t, projection)
	got := ageBandRequest(t, run, provider, "折叠后的新请求。")
	if len(got) == 0 {
		t.Fatal("the formal request carries no messages")
	}
	lead := 0
	if strings.HasPrefix(got[0].Content, "recovery: ") {
		lead = 1
	}
	if lead >= len(got) || !strings.HasPrefix(got[lead].Content, "history summary: ") {
		t.Fatalf("message %d = %q, want the history summary of the older bands leading the material", lead, got[min(lead, len(got)-1)].Content)
	}
	summary := strings.TrimPrefix(got[lead].Content, "history summary: ")
	for _, want := range []string{"external_input: ask first", "agent_output: answer first", "external_input: ask third", "agent_output: answer third"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("history summary = %q, want the one-line read %q of the folded turn", summary, want)
		}
	}
	if !strings.Contains(summary, "segment fold (-seg-") {
		t.Fatalf("history summary = %q, want the folded segments stated by the keys of their products", summary)
	}
	for _, line := range strings.Split(summary, "; ") {
		if !strings.HasPrefix(line, "[") || !strings.Contains(line, "|gen:?]") {
			t.Fatalf("history summary line %q opens with no provenance anchor naming the generation this chain leaves unstated", line)
		}
	}
	if last := got[len(got)-1]; last.Content != "折叠后的新请求。" || last.Role != model.RoleUser {
		t.Fatalf("last message = (%q, %q), want the input that opened this turn", last.Role, last.Content)
	}
	for _, message := range got {
		for _, raw := range []string{
			strings.Repeat("思考正文 first。", 12),
			strings.Repeat("工具输出 first。", 12),
			strings.Repeat("思考正文 second。", 12),
			strings.Repeat("工具输出 second。", 12),
			strings.Repeat("思考正文 third。", 12),
			strings.Repeat("工具输出 third。", 12),
		} {
			if strings.Contains(message.Content, raw) {
				t.Fatalf("request still carries the raw folded text %q", raw)
			}
		}
	}
	joined := strings.Join(contentsOf(got), "\n")
	if !strings.Contains(joined, strings.Repeat("回答正文 fourth。", 12)) {
		t.Fatal("request dropped the output that closed the newest turn, want the input and output of that turn kept")
	}
	if !strings.Contains(joined, strings.Repeat("思考正文 fourth。", 12)) || !strings.Contains(joined, strings.Repeat("工具输出 fourth。", 12)) {
		t.Fatal("request dropped the process detail of the newest turn, want the recent window kept whole")
	}
}

// TestAgeFoldUnderThresholdLeavesTheRequestAsItStands pins the negative side of the consumer.
//   - a projection inside the volume threshold reaches the request as it stood
//   - the raw process detail of the newest turn stays in place
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestAgeFoldUnderThresholdLeavesTheRequestAsItStands(t *testing.T) {
	ctx := context.Background()
	var facts []replay.ChainFact
	for step, seed := range []string{"first", "second", "third", "fourth"} {
		facts = append(facts, ageBandTurn(step, seed)...)
	}
	projection := ageBandProjection(t, facts)
	before := replay.RenderProjection(projection.Entries())

	report, err := projection.FoldByAge(ctx, replay.AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 1 << 20})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if report.Triggered || len(report.Products) != 0 || len(report.Requests) != 0 {
		t.Fatalf("report = %+v, want the fold to wait for the threshold", report)
	}
	if got := replay.RenderProjection(projection.Entries()); string(got) != string(before) {
		t.Fatal("projection rendered differently after a fold that did not trigger")
	}

	run, provider := ageBandRun(t, projection)
	got := ageBandRequest(t, run, provider, "阈值之内的新请求。")
	joined := strings.Join(contentsOf(got), "\n")
	if strings.Contains(joined, "history summary: ") || strings.Contains(joined, "|gen:") {
		t.Fatalf("request = %q, want no fold product presented under the threshold", joined)
	}
	if !strings.Contains(joined, strings.Repeat("思考正文 fourth。", 12)) {
		t.Fatal("request dropped the raw process detail of the newest turn, want it presented as it stood")
	}
}

// contentsOf reports the contents of the given messages in order.
func contentsOf(messages []model.Message) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		out = append(out, message.Content)
	}
	return out
}
