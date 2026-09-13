package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ---------------------------------------------------------------------------
// event-sourced-projection regression suite (tasks 4.1/4.2/4.4-4.10).
// Process A folds for real (incl. a tool_chain run), persists the compaction
// event, keeps talking (tail events + a dual-time straggler + meditation
// marks), then "restarts" as process B with an empty projection: the rebuild
// must restore the projection, the rendered messages, fullBoundary and the
// meditation keys byte-identically (prefix-cache proof).
// ---------------------------------------------------------------------------

const rbPid = 9

// rbNowMs anchors all fixture keys ABOVE any real-now snowflake (compaction
// events are written at real now): fake keys must sort after them or the
// rebuild tail cut (MinEventKey > compactionKey) mis-slices.
func rbNowMs() int64 { return time.Now().UnixMilli() + 1_000_000_000 }

// rbStore puts one real event (Content deliberately ≠ EventSummary: any
// fullBoundary drift between A and B flips the full-vs-summary render and
// breaks byte-identity — the dev fixedpoint test masked exactly this).
func rbStore(t *testing.T, store memory.MemoryStore, key int64, eventType, summary, content string, ts int64, md map[string]string) memory.EventReference {
	t.Helper()
	if md == nil {
		md = map[string]string{}
	}
	require.NoError(t, store.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: rbPid, EventType: eventType,
		EventSummary: summary, Content: content, Timestamp: ts, Metadata: md,
	}))
	return memory.EventReference{EventKey: key, PartitionID: rbPid, EventType: eventType,
		EventSummary: summary, Timestamp: ts, Role: string(tagentevent.EventTypeRole(eventType))}
}

// rbFoldCM builds a ContextManager whose compressor REALLY folds the given
// refs (tiny budget forces over-threshold; engineering-only fold — LLM
// narrative/condense degrade, which is fine: the fold state is what must
// survive, and the spec mandates no-LLM folds persist too).
func rbFoldCM(store memory.MemoryStore, proj *compress.SessionProjection, recentFull int) (*ContextManager, *compress.ContextCompressor) {
	cc := compress.NewContextCompressor(
		compress.NewSmartCompressor(compress.WithKeepRecentTasks(2)),
		store, compress.NewDefaultTokenCounter(), 60, 0.8, 2,
		compress.WithRecentFullCount(recentFull))
	cm := &ContextManager{partitionID: rbPid, memStore: store, projection: proj, contextCompressor: cc}
	return cm, cc
}

// rbAgedRefs: FIVE boundary-to-boundary task segments (each: user request +
// 3 tool pairs + output). Multi-segment structure gives SmartCompressor the
// age gradient (keepRecent=2 → older segments' middles drop → real fold,
// tool runs fold into tool_chain refs); all events store-backed.
func rbAgedRefs(t *testing.T, store memory.MemoryStore, base int64) []memory.EventReference {
	t.Helper()
	shape := []struct{ typ, sum string }{
		{tagentevent.TypeExternalInput, "用户请求部署服务"},
		{tagentevent.TypeThinkingPlan, "调用 read_file"},
		{tagentevent.TypeActionCommand, "配置文件内容"},
		{tagentevent.TypeThinkingPlan, "调用 grep"},
		{tagentevent.TypeActionCommand, "匹配结果三行"},
		{tagentevent.TypeThinkingPlan, "调用 edit_file"},
		{tagentevent.TypeActionCommand, "编辑成功"},
		{tagentevent.TypeAgentOutput, "部署完成"},
	}
	var refs []memory.EventReference
	for task := 0; task < 5; task++ {
		lastCallID := ""
		for i, s := range shape {
			key := memory.NewSnowflakeEventKey(rbPid, base+int64(task*100_000+i*1000))
			ts := base + int64(task*100_000+i*1000)
			evt := memory.FullEvent{
				EventKey: key, PartitionID: rbPid, EventType: s.typ,
				EventSummary: s.sum, Content: "FULL-" + s.sum, Timestamp: ts,
			}
			// Native pairing: thinking_plan carries the tool_calls, the paired
			// action_command answers via the SAME ToolID — without this
			// SmartCompressor sees incomplete segments and nothing folds.
			switch s.typ {
			case tagentevent.TypeThinkingPlan:
				lastCallID = fmt.Sprintf("call-%d-%d", task, i)
				name := strings.TrimPrefix(s.sum, "调用 ")
				evt.ToolCalls = []model.ToolCall{{Type: "function", ID: lastCallID,
					Function: model.FunctionDefinitionParam{Name: name}}}
			case tagentevent.TypeActionCommand:
				evt.ToolID = lastCallID
			}
			require.NoError(t, store.StoreEvent(key, evt))
			refs = append(refs, memory.EventReference{EventKey: key, PartitionID: rbPid,
				EventType: s.typ, EventSummary: s.sum, Timestamp: ts,
				Role: string(tagentevent.EventTypeRole(s.typ))})
		}
	}
	return refs
}

// driveRealFold runs A's pre-restart lifecycle: aged refs appended → real
// fold (asserted) → compaction event persisted.
func driveRealFold(t *testing.T, store memory.MemoryStore, proj *compress.SessionProjection, recentFull int) *ContextManager {
	t.Helper()
	cm, cc := rbFoldCM(store, proj, recentFull)
	for _, ref := range rbAgedRefs(t, store, rbNowMs()+60_000_000) {
		proj.Append(ref)
	}
	result := cc.Compress(context.Background(), proj.GetAll())
	require.NotEmpty(t, result.RetainedRefs)
	require.Less(t, result.RetainedRefs[0].EventKey, int64(0),
		"fold must have happened (negative summary ref at head)")
	require.Equal(t, tagentevent.TypeContextCompress, result.RetainedRefs[0].EventType)
	proj.Replace(result.RetainedRefs)
	// assembleRequest parity: Replace THEN emit; failure notice must be nil.
	require.Nil(t, cm.emitCompactionEvent(result.RetainedRefs))
	return cm
}

// TestRebuildProjectionFromWAL_ByteIdentical — the crown assertion (4.2 +
// 4.4 tool_chain + 4.5 + tail write-order + 4.6 meditation reseed + 4.10
// invariant: rebuild ≡ runtime fold).
func TestRebuildProjectionFromWAL_ByteIdentical(t *testing.T) {
	store := memory.NewInMemoryStore()
	projA := compress.NewSessionProjection()

	// A's pre-restart lifecycle: aged refs → real fold → compaction event.
	// A snapshot-side meditation agent_output is covered separately below
	// (snapshot-side detection lives in rebuild's GetEvent loop — see
	// TestRebuildProjectionFromWAL_SnapshotMeditationReseed).
	cmA := driveRealFold(t, store, projA, 2)
	ccA := cmA.contextCompressor

	// --- Tail: A keeps talking after the fold. Dual-time straggler (fresh-eyes
	// B): F produced first (write key smaller, Timestamp NEWER), E drained
	// later (write key larger, Timestamp OLDER — bus-arrival moment). Runtime
	// append order is WRITE order [F, E]; a Timestamp-ordered rebuild would
	// invert them.
	base := rbNowMs() + 200_000_000
	fRef := rbStore(t, store, memory.NewSnowflakeEventKey(rbPid, base), tagentevent.TypeAgentOutput,
		"F-产出", "FULL-F-产出", base+1000,
		map[string]string{tagentevent.MetaKeyTriggerSource: "meditation"})
	eRef := rbStore(t, store, memory.NewSnowflakeEventKey(rbPid, base+50_000), tagentevent.TypeExternalInput,
		"E-结算", "FULL-E-结算", base-500, nil) // OLD semantic Timestamp, NEW write key
	require.Less(t, fRef.EventKey, eRef.EventKey)
	projA.Append(fRef)
	projA.Append(eRef)
	ccA.MarkMeditationKey(fRef.EventKey) // runtime mark (session.go mirror)

	// A's rendered messages immediately before "restart" (under-budget
	// passthrough renders with A's fullBoundary — Content≠EventSummary makes
	// any boundary drift visible).
	renderA := ccA.Compress(context.Background(), projA.GetAll()).Messages
	snapshotA := projA.GetAll()
	medA := ccA.MeditationKeysSnapshot()

	// --- Restart: process B, empty projection, same fact chain.
	projB := compress.NewSessionProjection()
	cmB, ccB := rbFoldCM(store, projB, 2)
	_ = cmB
	cmB.rebuildProjectionFromWAL()

	// (4.10) Projection identity: same refs, same order (summary head,
	// tool_chain synthetic interleaved, tail in WRITE order [F, E]).
	gotB := projB.GetAll()
	require.Equal(t, snapshotA, gotB, "rebuilt projection must equal the pre-restart projection (refs + order)")
	// Write-order discrimination (fail-before: Timestamp order gives [E, F]).
	for i := range gotB {
		if gotB[i].EventKey == eRef.EventKey {
			require.Greater(t, i, 0, "straggler E must NOT be first (write order, not Timestamp order)")
		}
	}

	// (4.2) Render byte-identity — the prefix-cache proof.
	renderB := ccB.Compress(context.Background(), projB.GetAll()).Messages
	require.Equal(t, renderA, renderB, "render(projection) must be byte-identical across restart")

	// (D3) fullBoundary seeded from payload, exactly A's runtime state.
	require.Equal(t, ccA.FullBoundary(), ccB.FullBoundary())

	// (4.6) Meditation keys reseeded from the fact chain (tail side; the
	// snapshot-side path is covered by the no-LLM fold marking below).
	require.Equal(t, medA, ccB.MeditationKeysSnapshot(),
		"meditation keys must survive restart (Metadata[trigger_source] reseed)")

	// (4.7) Folded originals stay recall-redeemable: the aged tool events
	// were folded into the summary/tool_chain, never deleted.
	for _, key := range foldedKeys(t, store) {
		ev, err := store.GetEvent(key)
		require.NoError(t, err)
		require.NotNil(t, ev)
	}
}

// foldedKeys returns every stored external/tool event that is NOT a
// compaction event — the originals the fold absorbed (recall tickets).
func foldedKeys(t *testing.T, store memory.MemoryStore) []int64 {
	t.Helper()
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{rbPid}, Limit: 500})
	require.NoError(t, err)
	var out []int64
	for _, r := range refs {
		if r.EventType != tagentevent.TypeContextCompressSummary {
			out = append(out, r.EventKey)
		}
	}
	return out
}

// TestRebuildProjectionFromWAL_SnapshotMeditationReseed: a RETAINED positive
// agent_output carrying trigger_source=meditation in the compaction payload
// is re-marked by the rebuild's GetEvent loop (snapshot side; the tail side
// is covered in ByteIdentical).
func TestRebuildProjectionFromWAL_SnapshotMeditationReseed(t *testing.T) {
	store := memory.NewInMemoryStore()
	projA := compress.NewSessionProjection()
	cmA := driveRealFold(t, store, projA, 2)

	// A retained meditation agent_output inside the snapshot: store-side
	// metadata carries trigger_source; runtime A marked it too.
	var medKey int64
	for _, ref := range projA.GetAll() {
		if ref.EventKey > 0 && ref.EventType == tagentevent.TypeAgentOutput {
			medKey = ref.EventKey
			break
		}
	}
	require.NotZero(t, medKey, "fold must retain at least one agent_output")
	ev, err := store.GetEvent(medKey)
	require.NoError(t, err)
	md := map[string]string{tagentevent.MetaKeyTriggerSource: "meditation"}
	for k, v := range ev.Metadata {
		md[k] = v
	}
	require.NoError(t, store.DeleteEvent(medKey))
	ev.Metadata = md
	require.NoError(t, store.StoreEvent(medKey, *ev))
	cmA.contextCompressor.MarkMeditationKey(medKey)
	// Re-emit the compaction event so the payload covers the marked key.
	res := cmA.contextCompressor.Compress(context.Background(), projA.GetAll())
	require.True(t, res.Compressed)
	projA.Replace(res.RetainedRefs)
	require.Nil(t, cmA.emitCompactionEvent(res.RetainedRefs))

	projB := compress.NewSessionProjection()
	cmB, _ := rbFoldCM(store, projB, 2)
	cmB.rebuildProjectionFromWAL()
	require.Contains(t, cmB.contextCompressor.MeditationKeysSnapshot(), medKey,
		"snapshot-side meditation key must be reseeded from Metadata")
}

// TestEmitCompactionEvent_UnderBudgetAfterFoldNoEmit (review 🟠1): once the
// projection carries a fold summary at its head, an under-budget round must
// NOT re-emit (RetainedRefs[0] stays negative — only result.Compressed
// distinguishes a real fold from an under-budget pass-through).
func TestEmitCompactionEvent_UnderBudgetAfterFoldNoEmit(t *testing.T) {
	store := memory.NewInMemoryStore()
	projA := compress.NewSessionProjection()
	cmA := driveRealFold(t, store, projA, 2)

	// The fold-budget compressor (60 tokens) would re-fold every round; swap
	// to a large-budget compressor to reach a genuine under-budget round while
	// the projection head is still the old summary ref.
	bigCC := compress.NewContextCompressor(
		compress.NewSmartCompressor(compress.WithKeepRecentTasks(2)),
		store, compress.NewDefaultTokenCounter(), 8000, 0.8, 2)
	bigCC.SetFullBoundary(cmA.contextCompressor.FullBoundary())
	cmA.contextCompressor = bigCC

	tail := rbStore(t, store, memory.NewSnowflakeEventKey(rbPid, rbNowMs()+250_000_000),
		tagentevent.TypeExternalInput, "尾部一条", "FULL-尾部一条", rbNowMs()+250_000_000, nil)
	projA.Append(tail)
	res := cmA.contextCompressor.Compress(context.Background(), projA.GetAll())
	require.False(t, res.Compressed, "fixture expects an under-budget round")
	require.Less(t, res.RetainedRefs[0].EventKey, int64(0),
		"head must still be the old summary ref (the trap 🟠1 fix guards)")
	require.Nil(t, cmA.emitCompactionEvent(res.RetainedRefs),
		"under-budget round after a fold must not emit")
}

// TestEmitCompactionEvent_SupersedeGuards (4.1): real fold emits a
// marker-tagged narrative event; second fold supersedes the first (exactly
// one alive); legacy 固化物 (no marker) is never selected nor deleted;
// under-budget rounds emit nothing.
func TestEmitCompactionEvent_SupersedeGuards(t *testing.T) {
	store := memory.NewInMemoryStore()

	// Legacy immortal 固化物: same type, NO generation marker, older ts.
	legacyKey := memory.NewSnowflakeEventKey(rbPid, rbNowMs())
	require.NoError(t, store.StoreEvent(legacyKey, memory.FullEvent{
		EventKey: legacyKey, PartitionID: rbPid,
		EventType:    tagentevent.TypeContextCompressSummary,
		EventSummary: "legacy 综述", Timestamp: rbNowMs(),
	}))

	proj := compress.NewSessionProjection()
	cm := driveRealFold(t, store, proj, 2)
	first := cm.latestCompactionKey()
	require.NotZero(t, first, "first fold must emit a marker-tagged compaction event")
	require.NotEqual(t, legacyKey, first, "legacy 固化物 must never be selected")

	// (4.9) The compaction event is recall-shaped: content is the NARRATIVE
	// itself, not internal metadata.
	ev, err := store.GetEvent(first)
	require.NoError(t, err)
	require.Equal(t, compress.CompactionGenV1, ev.Metadata[compress.CompactionMetaKey])
	require.Contains(t, ev.Content, "[Compacted", "recall body must be the narrative")
	require.NotContains(t, ev.Content, "retained", "internal payload lives in Metadata, never the recall body")
	payload, err := compress.UnmarshalPayload(ev.Metadata[compress.CompactionPayloadMetaKey])
	require.NoError(t, err)
	require.NotEmpty(t, payload.Retained)

	// Second fold supersedes the first.
	result := cm.contextCompressor.Compress(context.Background(), proj.GetAll())
	if result.RetainedRefs[0].EventKey < 0 { // still folding at this tiny budget
		proj.Replace(result.RetainedRefs)
		require.Nil(t, cm.emitCompactionEvent(result.RetainedRefs))
	}
	second := cm.latestCompactionKey()
	require.NotZero(t, second)
	require.NotEqual(t, first, second, "supersede must move to the newer event")
	_, err = store.GetEvent(first)
	require.Error(t, err, "superseded compaction event must be deleted (only latest alive)")

	// Legacy untouched (fail-before: unscoped supersede deletes it).
	legacy, err := store.GetEvent(legacyKey)
	require.NoError(t, err)
	require.Equal(t, "legacy 综述", legacy.EventSummary)

	// Under-budget / no-fold rounds emit nothing.
	plain := compress.NewSessionProjection()
	plain.Append(rbStore(t, store, memory.NewSnowflakeEventKey(rbPid, rbNowMs()+300_000_000),
		tagentevent.TypeExternalInput, "仅一条", "FULL-仅一条", rbNowMs()+300_000_000, nil))
	cmPlain, ccPlain := rbFoldCM(store, plain, 2)
	res := ccPlain.Compress(context.Background(), plain.GetAll())
	if res.RetainedRefs[0].EventKey >= 0 { // no fold (single ref under budget)
		require.Nil(t, cmPlain.emitCompactionEvent(res.RetainedRefs),
			"under-budget round must not emit (no always-on writes)")
	}
}

// TestRebuildProjectionFromWAL_NoCompactionNoop (2.6): empty chain → no-op;
// non-empty projection → WARN skip, never Replace-over-live.
func TestRebuildProjectionFromWAL_NoCompactionNoop(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, _ := rbFoldCM(store, proj, 2)

	// No compaction event in the chain → no-op (projection stays empty).
	cm.rebuildProjectionFromWAL()
	require.Equal(t, 0, proj.Len())

	// Non-empty projection → skipped, content preserved (no Replace-over-live).
	proj.Append(rbStore(t, store, memory.NewSnowflakeEventKey(rbPid, rbNowMs()+310_000_000),
		tagentevent.TypeExternalInput, "活投影", "FULL-活投影", rbNowMs()+310_000_000, nil))
	driveRealFold(t, store, compress.NewSessionProjection(), 2) // chain now HAS a compaction event
	cm.rebuildProjectionFromWAL()
	require.Equal(t, 1, proj.Len(), "non-empty projection must not be replaced")
}

// TestReplayProjectionHandler_Branches (4.8 adjacent): compaction events are
// skipped (never double-represented as refs); meditation agent_output marks +
// appends; normal events append.
func TestReplayProjectionHandler_Branches(t *testing.T) {
	log.Infof("") // keep log imported for handler path parity
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, _ := rbFoldCM(store, proj, 2)
	ta := &TagentAgent{contextManager: cm}
	h := ReplayProjectionHandler(ta)

	medKey := memory.NewSnowflakeEventKey(rbPid, rbNowMs()+320_000_000)
	h(memory.FullEvent{EventKey: medKey, PartitionID: rbPid, EventType: tagentevent.TypeAgentOutput,
		EventSummary: "冥想产出", Metadata: map[string]string{tagentevent.MetaKeyTriggerSource: "meditation"}})
	require.Equal(t, 1, proj.Len())
	require.Equal(t, []int64{medKey}, cm.contextCompressor.MeditationKeysSnapshot())

	h(memory.FullEvent{EventKey: memory.NewSnowflakeEventKey(rbPid, rbNowMs()+320_001_000),
		PartitionID: rbPid, EventType: tagentevent.TypeExternalInput, EventSummary: "普通"})
	require.Equal(t, 2, proj.Len())

	// Compaction event → skipped (fact-chain record, not a projection ref).
	before := proj.Len()
	h(memory.FullEvent{EventKey: memory.NewSnowflakeEventKey(rbPid, rbNowMs()+320_002_000),
		PartitionID: rbPid, EventType: tagentevent.TypeContextCompressSummary,
		Metadata: map[string]string{compress.CompactionMetaKey: compress.CompactionGenV1}})
	require.Equal(t, before, proj.Len(), "compaction events must not append as projection refs")
}
