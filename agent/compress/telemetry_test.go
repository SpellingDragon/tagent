package compress

// Telemetry-channel regressions (change: attention-budget-architecture,
// specs/telemetry-channel). Pinned semantics:
//   - disposition is a deterministic fold over ref order + settle metadata:
//     unconsumed = Active (never demoted, never L3-absorbed), consumed with
//     deliverable lineage = Demote (exit at next act), internal = keep for
//     keepRecent turns then Demote; undecidable → internal (conservative);
//   - single settled notices fold via the EXISTING settle_fold card
//     machinery — consumption, not adjacency, is the exit unit;
//   - nil dispositions keeps pre-change behavior byte-for-byte (single runs
//     stay, ≥2 folds as before);
//   - buildRetainedRefs must only exempt explicit Active settle refs —
//     TelemActive being the zero value may never exempt conversational refs.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func settleRef(key int64, ts int64) memory.EventReference {
	return memory.EventReference{
		EventKey:     key,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "[task settled] ✓ deploy (id=abc12345) completed → done",
		Timestamp:    ts,
		Role:         "user",
	}
}

func outputRef(key int64, ts int64) memory.EventReference {
	return memory.EventReference{
		EventKey:     key,
		EventType:    tagentevent.TypeAgentOutput,
		EventSummary: "ok",
		Timestamp:    ts,
	}
}

func userRef(key int64, ts int64) memory.EventReference {
	return memory.EventReference{
		EventKey:     key,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "部署 v3 并盯着",
		Timestamp:    ts,
	}
}

func TestTelemetryDispositions_Structural(t *testing.T) {
	refs := []memory.EventReference{
		settleRef(11, 1), outputRef(12, 2), // consumed, store nil → internal (young)
		outputRef(13, 3), outputRef(14, 4), // two more turns → #11 aged internal→demote
		settleRef(21, 5), // never consumed → active
	}
	// Re-evaluate #11 with two following outputs (aged out of keepRecent=2):
	d := TelemetryDispositions(context.Background(), nil, refs[:4], 2)
	if d[11] != TelemDemote {
		t.Fatalf("internal notice aged past keepRecent=2 must demote, got %d", d[11])
	}
	d2 := TelemetryDispositions(context.Background(), nil, refs[:2], 2)
	if d2[11] != TelemInternal {
		t.Fatalf("young consumed notice must stay internal reminder, got %d", d2[11])
	}
	d3 := TelemetryDispositions(context.Background(), nil, refs, 2)
	if d3[21] != TelemActive {
		t.Fatalf("unconsumed notice must stay Active, got %d", d3[21])
	}
	// Conversational refs get no entry at all.
	if _, ok := d3[12]; ok {
		t.Fatal("agent_output must not carry a telemetry disposition")
	}
}

func TestTelemetryDispositions_LineageViaStore(t *testing.T) {
	store := memory.NewInMemoryStore()
	mk := func(key int64, meta map[string]string) {
		evt := memory.FullEvent{EventKey: key, EventType: tagentevent.TypeExternalInput, Metadata: meta}
		if err := store.StoreEvent(key, evt); err != nil {
			t.Fatal(err)
		}
	}
	mk(31, map[string]string{"meta_trigger_source": "user"})       // deliverable lineage
	mk(32, map[string]string{"meta_trigger_source": "meditation"}) // internal gate holds it
	mk(33, map[string]string{"lineage_absent": "true"})            // unknown → withhold → internal
	mk(34, map[string]string{})                                    // no lineage → internal (conservative)
	refs := []memory.EventReference{
		settleRef(31, 1), outputRef(41, 2),
	}
	d := TelemetryDispositions(context.Background(), store, refs, 2)
	if d[31] != TelemDemote {
		t.Fatalf("user-lineage consumed notice must demote (externalized), got %d", d[31])
	}
	// Internal/unknown/absent lineage: each evaluated with its OWN single
	// consuming output (no aged-out ambiguity) → internal reminder window.
	for _, k := range []int64{32, 33, 34} {
		solo := []memory.EventReference{settleRef(k, 1), outputRef(k+10, 2)}
		dk := TelemetryDispositions(context.Background(), store, solo, 2)
		if dk[k] != TelemInternal {
			t.Fatalf("internal/unknown lineage notice %d must stay internal, got %d", k, dk[k])
		}
	}
}

func TestFoldSettleRuns_ConsumptionDemotesSingle(t *testing.T) {
	cc := newFoldCC(2)
	single := []memory.EventReference{userRef(50, 100), settleRef(51, 101)}
	// Active: untouched (不可丢级).
	if got := cc.foldSettleRuns(single, map[int64]int8{51: TelemActive}); len(got) != 2 {
		t.Fatalf("Active single notice must stay verbatim, got %d refs", len(got))
	}
	// Internal young: untouched.
	if got := cc.foldSettleRuns(single, map[int64]int8{51: TelemInternal}); len(got) != 2 {
		t.Fatalf("internal notice must keep its reminder window, got %d refs", len(got))
	}
	// Demote: single folds to the ticket card (reuses settle_fold machinery).
	got := cc.foldSettleRuns(single, map[int64]int8{51: TelemDemote})
	if len(got) != 2 || got[1].EventType != tagentevent.TypeSettleFold {
		t.Fatalf("demoted single notice must fold to a settle_fold card, got %+v", got)
	}
	// Cards never re-fold (idempotence across acts).
	again := cc.foldSettleRuns(got, map[int64]int8{got[1].EventKey: TelemDemote})
	if len(again) != 2 || again[1].EventKey != got[1].EventKey {
		t.Fatalf("folded card must not re-fold, got %+v", again)
	}
}

func TestFoldSettleRuns_NilDispositionsByteIdentical(t *testing.T) {
	cc := newFoldCC(2)
	single := []memory.EventReference{settleRef(51, 101)}
	run2 := []memory.EventReference{settleRef(52, 102), settleRef(53, 103)}
	if got := cc.foldSettleRuns(single, nil); len(got) != 1 {
		t.Fatalf("nil dispositions must preserve pre-change single-not-folded behavior, got %d", len(got))
	}
	if got := cc.foldSettleRuns(run2, nil); len(got) != 1 || got[0].EventType != tagentevent.TypeSettleFold {
		t.Fatalf("≥2 runs must still fold as before, got %+v", got)
	}
}

func TestBuildRetainedRefs_OnlyActiveExempted(t *testing.T) {
	cc := newFoldCC(2)
	active := settleRef(61, 200)
	internal := settleRef(62, 201)
	dropped := []memory.EventReference{userRef(60, 199), active, internal}
	// Empty compressed messages → nothing survived → all would be absorbed.
	retained := cc.buildRetainedRefs(dropped, nil, context.Background(), map[int64]int8{
		61: TelemActive,
		62: TelemInternal,
	})
	sawActive, sawInternal := false, false
	for _, r := range retained {
		if r.EventKey == 61 {
			sawActive = true
		}
		if r.EventKey == 62 {
			sawInternal = true
		}
	}
	if !sawActive {
		t.Fatal("unconsumed telemetry ref must survive L3 retirement in the projection")
	}
	if sawInternal {
		t.Fatal("consumed internal ref is budget-eligible: it must fold into the rolling summary")
	}
	// A conversational ref must never be exempted via the zero value.
	conv := cc.buildRetainedRefs([]memory.EventReference{userRef(70, 300)}, nil, context.Background(), map[int64]int8{})
	for _, r := range conv {
		if r.EventKey == 70 {
			t.Fatal("zero-value dispositions must not exempt conversational refs (prefix model unchanged)")
		}
	}
}

// TestCompress_TelemetryChannelIntegration runs the real Compress path with
// an over-budget workload: an externalized consumed notice must arrive in
// RetainedRefs as a settle_fold card (dispositions wired through Compress),
// while an unconsumed notice stays verbatim (compaction 豁免).
func TestCompress_TelemetryChannelIntegration(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	mustStore := func(key int64, evtType, content string, meta map[string]string) {
		require.NoError(t, memStore.StoreEvent(key, memory.FullEvent{
			EventKey: key, EventType: evtType, EventSummary: content[:min(40, len(content))], Content: content, Metadata: meta,
		}))
	}
	mustStore(60, tagentevent.TypeExternalInput, "部署 v3 并盯着三天，注意观察内存水位与日志异常，有任何问题立即汇报给我", nil)
	mustStore(61, tagentevent.TypeExternalInput, "[task settled] ✓ deploy (id=a1) completed → 部署成功，无异常", map[string]string{"meta_trigger_source": "user"})
	mustStore(62, tagentevent.TypeAgentOutput, "部署已完成，无异常", nil)
	mustStore(63, tagentevent.TypeExternalInput, "[task settled] ✓ watchdog (id=b2) completed → 还在跑未消费", nil)
	refs := []memory.EventReference{
		{EventKey: 60, EventType: tagentevent.TypeExternalInput, EventSummary: "部署 v3", Timestamp: 2000, Role: "user"},
		{EventKey: 61, EventType: tagentevent.TypeExternalInput, EventSummary: "[task settled] ✓ deploy (id=a1) completed", Timestamp: 2001, Role: "user"},
		{EventKey: 62, EventType: tagentevent.TypeAgentOutput, EventSummary: "ok", Timestamp: 2002, Role: "assistant"},
		{EventKey: 63, EventType: tagentevent.TypeExternalInput, EventSummary: "[task settled] ✓ watchdog (id=b2) completed", Timestamp: 2003, Role: "user"},
	}
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(100))
	// Production-sized card budget: at the tiny default the pre-existing
	// curateCards SINKING (earlier-N) drops even demotion tickets — that loss
	// is an existing observation item, orthogonal to this change.
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 100, 0.8, 1,
		WithCardMaxChars(6000))
	res := cc.Compress(context.Background(), refs)
	require.True(t, res.Compressed, "over budget: compaction act must run")
	ticket61 := tagentevent.FormatEventKey(61)
	var sawTicket, stillVerbatim61, keptActive bool
	for _, r := range res.RetainedRefs {
		if r.EventKey == 61 {
			stillVerbatim61 = true
		}
		if r.EventKey == 63 {
			keptActive = true
		}
		if strings.Contains(r.EventSummary, ticket61) {
			sawTicket = true
		}
	}
	// Demotion occurred at the real Compress path: the externalized notice
	// left the verbatim form, while its recall ticket survived (lossless exit
	// — card or rolling-summary row, bounded by nothing less than both).
	require.False(t, stillVerbatim61, "externalized consumed notice must not remain a verbatim ref")
	require.True(t, sawTicket, "demoted notice must keep its recall ticket in the card/summary")
	require.True(t, keptActive, "unconsumed notice stays verbatim (compaction 豁免)")
}

func TestSettleFoldLine_FailedCarriesStar(t *testing.T) {
	ref := memory.EventReference{
		EventKey:     81,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "[task settled] ✗ build (id=x) failed → oom",
		Timestamp:    1,
	}
	line := settleFoldLine(ref)
	if !strings.HasPrefix(line, "★ ") {
		t.Fatalf("failed-polarity card row must carry the ★ reflection anchor, got %q", line)
	}
	ok := memory.EventReference{EventKey: 82, EventSummary: "[task settled] ✓ ok (id=y) completed → done", Timestamp: 1}
	if strings.HasPrefix(settleFoldLine(ok), "★ ") {
		t.Fatal("completed rows must not carry ★")
	}
}

// TestTelemetryReplay_RealTrajectory (attention-budget-architecture tasks 5.1):
// the REAL remote trajectory (batch 123, 831 messages → 447 refs incl. 159
// settle notices, 477K chars, heartbeat-shaped Sx1 interleave with 17×22K
// legacy-cap monsters) replayed through the production disposition + fold
// rules. Regression gate against the review-finding class "verified on
// synthetic forms, broken on production forms" — the fixture is EXTRACTED
// from traj-30m-raw.jsonl, never hand-written.
func TestTelemetryReplay_RealTrajectory(t *testing.T) {
	raw, err := os.ReadFile("testdata/telemetry_replay_fixture.json")
	require.NoError(t, err)
	var fixture []struct {
		K int64  `json:"k"`
		T string `json:"t"`
		S string `json:"s"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	require.Greater(t, len(fixture), 300, "fixture degraded: production replay must carry the full ref order")

	var refs []memory.EventReference
	var settleChars int
	for i, f := range fixture {
		refs = append(refs, memory.EventReference{
			EventKey: f.K, EventType: f.T, EventSummary: f.S,
			Timestamp: int64(2000 + i), Role: "user",
		})
		if f.T == tagentevent.TypeExternalInput && strings.HasPrefix(strings.TrimSpace(f.S), "[task settled]") {
			settleChars += len(f.S)
		}
	}
	require.Equal(t, 159, countSettleRefs(refs), "fixture must carry the 159 real settle notices")
	require.Greater(t, settleChars, 400_000, "fixture must carry the real volume incl. the 22K legacy monsters")

	// Production rules over the real shape (msgs carry no metadata → the
	// conservative internal lineage口径, exactly the review replay's).
	d := TelemetryDispositions(context.Background(), nil, refs, 2)
	var active, internal, demote int
	for _, v := range d {
		switch v {
		case TelemActive:
			active++
		case TelemInternal:
			internal++
		case TelemDemote:
			demote++
		}
	}
	require.GreaterOrEqual(t, demote, 156, "≥156/159 consumed-and-aged notices must demote on the real shape")
	require.LessOrEqual(t, active+internal, 3, "only the head-of-stream notices may stay verbatim")

	// Fold: measure the REAL post-fold telemetry volume — verbatim settle
	// bodies plus settle_fold card summaries (each single fold carries a
	// ~50-char header + one ~90-char ticket row) — and require ≥95% reclaim.
	cc := newFoldCC(2)
	folded := cc.foldSettleRuns(refs, d)
	after := 0
	for _, r := range folded {
		if isSettleNoticeRef(r) {
			after += len(r.EventSummary)
		}
		if r.EventType == tagentevent.TypeSettleFold {
			after += len(r.EventSummary)
		}
	}
	require.Less(t, after, settleChars/20, "fold must reclaim ≥95%% of the settle body on the real shape; got %d -> %d chars", settleChars, after)
}

func countSettleRefs(refs []memory.EventReference) int {
	n := 0
	for _, r := range refs {
		if isSettleNoticeRef(r) {
			n++
		}
	}
	return n
}

// TestRecallStagingNeverResident (tasks 5.2): a recall tool result renders as
// an action_command (tool) message — the skeleton pipeline drops it at L1 by
// design, so recalled detail NEVER becomes resident through any skeleton
// preservation. Pins the telemetry-channel "召回闭环" requirement against a
// future change that would accidentally skeletonize tool output.
func TestRecallStagingNeverResident(t *testing.T) {
	recallMsg := model.Message{Role: model.RoleTool, Content: "[evt_123|action_command] memory_recall: 9K 详情原文……"}
	require.False(t, IsSkeletonMessage(&recallMsg),
		"recall results are tool-class: skeleton preservation must never apply")
	seg := SegmentMessages([]model.Message{
		{Role: model.RoleUser, Content: "[evt_110|external_input] 查昨天的报错细节"},
		recallMsg,
		{Role: model.RoleAssistant, Content: "[evt_111|agent_output] 详情是……"},
	})
	require.Len(t, seg, 1)
	dropped := applySegmentLevel(seg[0], 1) // L1: tool first out
	for _, m := range dropped {
		require.NotContains(t, m.Content, "memory_recall",
			"L1 must have dropped the recalled detail from the resident view")
	}
}

// TestDispositionRebuildDeterminism (tasks 5.3): restart-replay equivalence —
// dispositions are a pure fold over (fact order + metadata), so deriving them
// again over the WAL-rebuilt refs yields the same map as before shutdown,
// regardless of any intermediate fold that the pre-restart projection held.
func TestDispositionRebuildDeterminism(t *testing.T) {
	store := memory.NewInMemoryStore()
	mk := func(key int64, evtType string, meta map[string]string) {
		require.NoError(t, store.StoreEvent(key, memory.FullEvent{EventKey: key, EventType: evtType, Metadata: meta}))
	}
	mk(71, tagentevent.TypeExternalInput, map[string]string{"meta_trigger_source": "user"})
	mk(72, tagentevent.TypeExternalInput, map[string]string{"meta_trigger_source": "meditation"})
	mk(73, tagentevent.TypeExternalInput, nil) // lineage_absent-class: no metadata
	original := []memory.EventReference{
		{EventKey: 71, EventType: tagentevent.TypeExternalInput, EventSummary: "[task settled] ✓ a completed", Timestamp: 1, Role: "user"},
		{EventKey: 80, EventType: tagentevent.TypeAgentOutput, EventSummary: "out", Timestamp: 2, Role: "assistant"},
		{EventKey: 72, EventType: tagentevent.TypeExternalInput, EventSummary: "[task settled] ✓ b completed", Timestamp: 3, Role: "user"},
		{EventKey: 81, EventType: tagentevent.TypeAgentOutput, EventSummary: "out", Timestamp: 4, Role: "assistant"},
		{EventKey: 82, EventType: tagentevent.TypeAgentOutput, EventSummary: "out", Timestamp: 5, Role: "assistant"},
		{EventKey: 73, EventType: tagentevent.TypeExternalInput, EventSummary: "[task settled] ✓ c completed", Timestamp: 6, Role: "user"},
	}
	before := TelemetryDispositions(context.Background(), store, original, 2)
	// Simulated restart: WAL rebuild re-yields the ORIGINAL refs (the fold was
	// a projection-only operation); the derivation must reproduce the map.
	rebuilt := append([]memory.EventReference(nil), original...)
	after := TelemetryDispositions(context.Background(), store, rebuilt, 2)
	require.Equal(t, before, after,
		"dispositions are a deterministic fold: restart rebuild must reproduce the pre-shutdown map")
	require.Equal(t, TelemDemote, before[71], "user-lineage consumed → demote")
	require.Equal(t, TelemInternal, before[72], "meditation-lineage consumed, one turn since → internal reminder")
	require.Equal(t, TelemActive, before[73], "unconsumed → active (不可丢)")
}
