// 本文件负责投影重建的一致性：从 WAL 重建必须与活投影字节一致、冥想重播种、折叠事件的抑制与
// 超额守卫，以及重放处理器的分支选择——重放只逐事件补投影，绝不在活投影上整表替换。
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

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
	require.Nil(t, cm.emitCompactionEvent(result.RetainedRefs))
	return cm
}

// TestRebuildProjectionFromWAL_ByteIdentical 钉住 核心判据：从写前日志重建必须与运行时折叠逐字节等价。
// - 覆盖工具链折叠、尾部写入顺序与冥想重播种各分支；
// - "重建等价于运行时折叠"是恒等式，不是近似。
func TestRebuildProjectionFromWAL_ByteIdentical(t *testing.T) {
	store := memory.NewInMemoryStore()
	projA := compress.NewSessionProjection()

	cmA := driveRealFold(t, store, projA, 2)
	ccA := cmA.contextCompressor

	base := rbNowMs() + 200_000_000
	fRef := rbStore(t, store, memory.NewSnowflakeEventKey(rbPid, base), tagentevent.TypeAgentOutput,
		"F-产出", "FULL-F-产出", base+1000,
		map[string]string{tagentevent.MetaKeyTriggerSource: "meditation"})
	eRef := rbStore(t, store, memory.NewSnowflakeEventKey(rbPid, base+50_000), tagentevent.TypeExternalInput,
		"E-结算", "FULL-E-结算", base-500, nil)
	require.Less(t, fRef.EventKey, eRef.EventKey)
	projA.Append(fRef)
	projA.Append(eRef)
	ccA.MarkMeditationKey(fRef.EventKey)

	renderA := ccA.Compress(context.Background(), projA.GetAll()).Messages
	snapshotA := projA.GetAll()
	medA := ccA.MeditationKeysSnapshot()

	projB := compress.NewSessionProjection()
	cmB, ccB := rbFoldCM(store, projB, 2)
	_ = cmB
	cmB.rebuildProjectionFromWAL()

	gotB := projB.GetAll()
	require.Equal(t, snapshotA, gotB, "rebuilt projection must equal the pre-restart projection (refs + order)")
	for i := range gotB {
		if gotB[i].EventKey == eRef.EventKey {
			require.Greater(t, i, 0, "straggler E must NOT be first (write order, not Timestamp order)")
		}
	}

	renderB := ccB.Compress(context.Background(), projB.GetAll()).Messages
	require.Equal(t, renderA, renderB, "render(projection) must be byte-identical across restart")

	require.Equal(t, ccA.FullBoundary(), ccB.FullBoundary())

	require.Equal(t, medA, ccB.MeditationKeysSnapshot(),
		"meditation keys must survive restart (Metadata[trigger_source] reseed)")

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

// TestRebuildProjectionFromWAL_SnapshotMeditationReseed 钉住 折叠载荷里带冥想触发源且被保留的正向输出，重建时必须重新标记。
// - 本条覆盖快照侧；尾部侧由逐字节等价那条负责。
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

// TestEmitCompactionEvent_UnderBudgetAfterFoldNoEmit 钉住 投影头部已带折叠摘要时，预算内的回合不得再次发出折叠事件。
// - 判定落在真实保留引用的首项仍为负键上：只有压缩结果本身能区分真折叠与预算内原样通过。
func TestEmitCompactionEvent_UnderBudgetAfterFoldNoEmit(t *testing.T) {
	store := memory.NewInMemoryStore()
	projA := compress.NewSessionProjection()
	cmA := driveRealFold(t, store, projA, 2)

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

// TestEmitCompactionEvent_SupersedeGuards 钉住 真折叠发射带代际标记的叙事事件；第二次折叠恰好取代第一次（仅一条存活）；无标记的既有固化数据既不选也不删；未超预算的轮次不发射。
func TestEmitCompactionEvent_SupersedeGuards(t *testing.T) {
	store := memory.NewInMemoryStore()

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

	ev, err := store.GetEvent(first)
	require.NoError(t, err)
	require.Equal(t, compress.CompactionGenV1, ev.Metadata[compress.CompactionMetaKey])
	require.Contains(t, ev.Content, "[Compacted", "recall body must be the narrative")
	require.NotContains(t, ev.Content, "retained", "internal payload lives in Metadata, never the recall body")
	payload, err := compress.UnmarshalPayload(ev.Metadata[compress.CompactionPayloadMetaKey])
	require.NoError(t, err)
	require.NotEmpty(t, payload.Retained)

	result := cm.contextCompressor.Compress(context.Background(), proj.GetAll())
	if result.RetainedRefs[0].EventKey < 0 {
		proj.Replace(result.RetainedRefs)
		require.Nil(t, cm.emitCompactionEvent(result.RetainedRefs))
	}
	second := cm.latestCompactionKey()
	require.NotZero(t, second)
	require.NotEqual(t, first, second, "supersede must move to the newer event")
	_, err = store.GetEvent(first)
	require.Error(t, err, "superseded compaction event must be deleted (only latest alive)")

	legacy, err := store.GetEvent(legacyKey)
	require.NoError(t, err)
	require.Equal(t, "legacy 综述", legacy.EventSummary)

	plain := compress.NewSessionProjection()
	plain.Append(rbStore(t, store, memory.NewSnowflakeEventKey(rbPid, rbNowMs()+300_000_000),
		tagentevent.TypeExternalInput, "仅一条", "FULL-仅一条", rbNowMs()+300_000_000, nil))
	cmPlain, ccPlain := rbFoldCM(store, plain, 2)
	res := ccPlain.Compress(context.Background(), plain.GetAll())
	if res.RetainedRefs[0].EventKey >= 0 {
		require.Nil(t, cmPlain.emitCompactionEvent(res.RetainedRefs),
			"under-budget round must not emit (no always-on writes)")
	}
}

// TestRebuildProjectionFromWAL_NoCompactionNoop 钉住 (2.6): empty chain → no-op; non-empty projection → WARN skip, never Replace-over-live.。
func TestRebuildProjectionFromWAL_NoCompactionNoop(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, _ := rbFoldCM(store, proj, 2)

	cm.rebuildProjectionFromWAL()
	require.Equal(t, 0, proj.Len())

	proj.Append(rbStore(t, store, memory.NewSnowflakeEventKey(rbPid, rbNowMs()+310_000_000),
		tagentevent.TypeExternalInput, "活投影", "FULL-活投影", rbNowMs()+310_000_000, nil))
	driveRealFold(t, store, compress.NewSessionProjection(), 2)
	cm.rebuildProjectionFromWAL()
	require.Equal(t, 1, proj.Len(), "non-empty projection must not be replaced")
}

// TestReplayProjectionHandler_Branches 钉住 重放处理器的分支选择：压实事件被跳过，绝不作为引用重复呈现。
// - 冥想的输出是标记加追加；普通事件只追加。
func TestReplayProjectionHandler_Branches(t *testing.T) {
	log.Infof("")
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

	before := proj.Len()
	h(memory.FullEvent{EventKey: memory.NewSnowflakeEventKey(rbPid, rbNowMs()+320_002_000),
		PartitionID: rbPid, EventType: tagentevent.TypeContextCompressSummary,
		Metadata: map[string]string{compress.CompactionMetaKey: compress.CompactionGenV1}})
	require.Equal(t, before, proj.Len(), "compaction events must not append as projection refs")
}

// TestRebuildProjectionFromWAL_DiskRoundtripByteIdentical 钉住 真实磁盘链路（本地文件 KV 的 WAL 与快照，跨一次真实关闭→重开）也必须字节一致。
// - 同一条折叠与尾部生命周期、持久化屏障之后冷重开，既还原渲染也还原投影身份；
// - 内存链路的断言不能替代这一条。
func TestRebuildProjectionFromWAL_DiskRoundtripByteIdentical(t *testing.T) {
	dir := t.TempDir()
	kv1, err := kv.NewLocalFileKV(dir)
	require.NoError(t, err)
	store1, err := memory.NewFileSegmentStore(kv1, nil, dir, 200)
	require.NoError(t, err)

	projA := compress.NewSessionProjection()
	cmA := driveRealFold(t, store1, projA, 2)
	ccA := cmA.contextCompressor

	base := rbNowMs() + 300_000_000
	fRef := rbStore(t, store1, memory.NewSnowflakeEventKey(rbPid, base), tagentevent.TypeAgentOutput,
		"F-产出", "FULL-F-产出", base+1000,
		map[string]string{tagentevent.MetaKeyTriggerSource: "meditation"})
	eRef := rbStore(t, store1, memory.NewSnowflakeEventKey(rbPid, base+50_000), tagentevent.TypeExternalInput,
		"E-结算", "FULL-E-结算", base-500, nil)
	projA.Append(fRef)
	projA.Append(eRef)
	ccA.MarkMeditationKey(fRef.EventKey)

	renderA := ccA.Compress(context.Background(), projA.GetAll()).Messages
	snapshotA := projA.GetAll()
	boundaryA := ccA.FullBoundary()

	require.NoError(t, kv1.Sync())
	require.NoError(t, kv1.Close())

	kv2, err := kv.NewLocalFileKV(dir)
	require.NoError(t, err)
	defer kv2.Close()
	store2, err := memory.NewFileSegmentStore(kv2, nil, dir, 200)
	require.NoError(t, err)

	projB := compress.NewSessionProjection()
	cmB, _ := rbFoldCM(store2, projB, 2)
	cmB.rebuildProjectionFromWAL()

	gotB := projB.GetAll()
	require.Equal(t, snapshotA, gotB, "disk roundtrip: rebuilt projection must equal the pre-restart projection")

	ccB := cmB.contextCompressor
	renderB := ccB.Compress(context.Background(), projB.GetAll()).Messages
	require.Equal(t, renderA, renderB, "disk roundtrip: render(projection) must be byte-identical across a real persist/reopen cycle")
	require.Equal(t, boundaryA, ccB.FullBoundary(), "fullBoundary must seed from the persisted payload, not be recomputed")
}

// fbChainStore lays n agent-output events on the fact chain.
//
// Keys MUST be real Snowflake keys with partition bits = rbPid: the store's
// GetEvents resolves the partition FROM THE KEY BITS, so raw-ms keys make
// events land under partition rbPid while key-lookup decodes partition 0 —
// the fallback tail fetch would silently return empty (0 refs).
// Returns issued keys in write order (monotonic, NOT +1-consecutive).
func fbChainStore(t *testing.T, store memory.MemoryStore, n int, startMs int64) []int64 {
	t.Helper()
	keys := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		k := memory.NewSnowflakeEventKey(rbPid, startMs+int64(i))
		rbStore(t, store, k, tagentevent.TypeAgentOutput,
			fmt.Sprintf("fallback summary %d", i),
			fmt.Sprintf("fallback full content %d", i),
			startMs+int64(i), nil)
		keys = append(keys, k)
	}
	return keys
}

func TestRebuildFallback_NoAnchor_RecoversTail(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, _ := rbFoldCM(store, proj, 4)
	const n = 8
	keys := fbChainStore(t, store, n, rbNowMs())

	cm.rebuildProjectionFromWAL()

	require.Equal(t, n, proj.Len(), "fallback must recover ALL chain events into the projection")
	got := proj.GetAll()
	require.Equal(t, keys[0], got[0].EventKey)
	require.Equal(t, keys[n-1], got[n-1].EventKey)
	require.Equal(t, tagentevent.TypeAgentOutput, got[n-1].EventType)
}

func TestRebuildFallback_NoCap_RecoversFullChain(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, _ := rbFoldCM(store, proj, 4)
	const n = 505
	keys := fbChainStore(t, store, n, rbNowMs())

	cm.rebuildProjectionFromWAL()

	require.Equal(t, n, proj.Len(), "fallback must recover the FULL chain, no cap")
	got := proj.GetAll()
	require.Equal(t, keys[0], got[0].EventKey, "oldest event must survive")
	require.Equal(t, keys[n-1], got[n-1].EventKey)
}

func TestRebuildFallback_EmptyChain_StaysEmpty(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, _ := rbFoldCM(store, proj, 4)

	cm.rebuildProjectionFromWAL()
	require.Equal(t, 0, proj.Len())
}

// TestRebuildFallback_FilterBeforeCap 钉住 无锚回放覆盖全链存活（不设 fallback 上限：重启后完整历史必须可复原），且过滤次序不变——task_spawned 等非投影记录绝不占用投影槽位。
func TestRebuildFallback_FilterBeforeCap(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, _ := rbFoldCM(store, proj, 4)

	base := rbNowMs()
	for i := 0; i < 600; i++ {
		ms := base + int64(2*i)
		k := memory.NewSnowflakeEventKey(rbPid, ms)
		rbStore(t, store, k, tagentevent.TypeAgentOutput,
			fmt.Sprintf("valid %d", i), fmt.Sprintf("content %d", i), ms, nil)
		k2 := memory.NewSnowflakeEventKey(rbPid, ms+1)
		rbStore(t, store, k2, tagentevent.TypeTaskSpawned,
			fmt.Sprintf("spawn %d", i), fmt.Sprintf("spawn-body %d", i), ms+1, nil)
	}

	cm.rebuildProjectionFromWAL()

	require.Equal(t, 600, proj.Len(), "cap removed: full chain survives restart")
	got := proj.GetAll()
	for _, ref := range got {
		require.NotEqual(t, tagentevent.TypeTaskSpawned, ref.EventType,
			"internal records must not occupy projection slots")
	}
	rec := cm.RecoveryResult()
	require.NotNil(t, rec)
	require.Equal(t, "fallback", rec.Mode)
	require.Equal(t, 0, rec.Truncated, "cap removed: nothing truncated")
	require.Equal(t, "full", rec.Status)
	require.Empty(t, cm.TakeRecoveryNotice(), "full recovery has no model-facing notice")
}

// TestPersistBusEvent_ProjectionRefUsesCanonicalTime 钉住 投影引用取冻结的规范时间，而不是当前事件的到达时间。
// - 二者不同就必须可见地采用规范值：投影与事实链在时间上同源。
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestPersistBusEvent_ProjectionRefUsesCanonicalTime(t *testing.T) {
	cm := newPreparedGateCM()
	const canonicalTS = int64(1700000000000)
	fact, err := json.Marshal(memory.FullEvent{
		EventKey:     555000111,
		PartitionID:  1,
		EventType:    "external_input",
		EventSummary: "canon-summary",
		Content:      "hello",
		Timestamp:    canonicalTS,
		Metadata:     map[string]string{"agent_name": "a"},
	})
	require.NoError(t, err)

	evt := evtWithPreparedClaim(fact)
	require.True(t, evt.Timestamp.UnixMilli() != canonicalTS, "precondition: arrival time differs from canonical")
	require.True(t, cm.persistBusEvent(evt))

	refs := cm.projection.GetAll()
	require.Len(t, refs, 1)
	require.Equal(t, canonicalTS, refs[0].Timestamp,
		"§4.6: the projection ref MUST carry the canonical fact's time, not the current event's arrival time")
	require.Equal(t, "canon-summary", refs[0].EventSummary, "ref summary comes from the canonical fact")
}

// TestPersistBusEvent_AlreadyCommittedSelectedInputIsBackfilled 钉住 已选输入的事实虽已在链上、却未被冷启动快照或尾部恢复进投影时，仍须在执行前补回引用。
// - "已有事实"不等于"当前请求已包含"，该分类不得把输入丢掉。
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestPersistBusEvent_AlreadyCommittedSelectedInputIsBackfilled(t *testing.T) {
	cm := newPreparedGateCM()
	canonical := memory.FullEvent{
		EventKey:     777000999,
		PartitionID:  1,
		EventType:    "external_input",
		EventSummary: "pre-crash input",
		Content:      "hello",
		Timestamp:    1690000000000,
		Metadata:     map[string]string{"agent_name": "a"},
	}
	require.NoError(t, cm.memStore.StoreEvent(canonical.EventKey, canonical))
	require.Empty(t, cm.projection.GetAll(), "precondition: cold start did NOT restore this outstanding input's ref")

	fact, err := json.Marshal(canonical)
	require.NoError(t, err)

	require.True(t, cm.persistBusEvent(evtWithPreparedClaim(fact)), "a replayed selected input still reports committed")

	refs := cm.projection.GetAll()
	require.Len(t, refs, 1, "§4.6: an already-committed SELECTED outstanding input MUST be backfilled into the projection (not dropped by the 'already' classification)")
	require.Equal(t, canonical.EventKey, refs[0].EventKey)
	require.Equal(t, canonical.Timestamp, refs[0].Timestamp, "backfilled ref is built from the canonical fact")
}

// TestPersistBusEvent_ReCommitDoesNotDoubleProject 钉住 选中的事实只投影一次，重跑同一次提交不得二次投影。
// - 投影追加按事件键幂等，因此首次提交与同一键的回填收敛为恰好一条引用；
// - 这条防的是模型重试与压缩后重插造成的重复注入。
func TestPersistBusEvent_ReCommitDoesNotDoubleProject(t *testing.T) {
	cm := newPreparedGateCM()
	fact, err := json.Marshal(memory.FullEvent{
		EventKey: 31337, PartitionID: 1, EventType: "external_input",
		EventSummary: "once", Content: "hello", Timestamp: 1680000000000,
		Metadata: map[string]string{"agent_name": "a"},
	})
	require.NoError(t, err)
	require.True(t, cm.persistBusEvent(evtWithPreparedClaim(fact)))
	require.True(t, cm.persistBusEvent(evtWithPreparedClaim(fact)))
	require.Len(t, cm.projection.GetAll(), 1, "the same selected fact must project exactly once across re-commit")
}

// callProjection reopened gap (W-1 isolation half):
// the delegation wrapper lives in ta.config.Tools and is therefore SHARED by
// every invocation-private ContextManager that the same agent builds. Binding
// that shared object per call (`buildExecutor` → SetParentProjection) both
// writes a field another in-flight call reads, and lets call A's event_keys
// auto-inject resolve against call B's projection. Design D2 forbids a
// call-private projection from being shared across calls; D5 forbids rebinding
// an already-published wrapper. This test drives TWO barrier-synchronized real
// concurrent invocations of one agent and requires each delegation to inject
// only from its OWN call's projection.
//
// Test-only peeks keep production API free of introspection surface.
func (cm *ContextManager) callProjection() *compress.SessionProjection { return cm.projection }

func (w *AgentToolWrapper) publishedCallProjection() *compress.SessionProjection {
	return w.parentProjection
}

// enteredGate parks one flow's model call inside the invocation window, which is the
// proof that that flow's private CM has already been constructed.
type enteredGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// recordingFlowModel scripts the parent: per flow (keyed by the driving user message),
// the FIRST model call signals entry, waits for release, then emits a
// delegation tool_call; every later call closes the turn.
type recordingFlowModel struct {
	mu     sync.Mutex
	counts map[string]int
	gates  map[string]*enteredGate
	tool   string
}

// flow identifies which call a model request belongs to. The driving message
// reaches the model decorated by the memory layer ("[evt_<id>|external_input] A"),
// so the flow key is the message's LAST field, not the whole content.
func (m *recordingFlowModel) flow(req *model.Request) (*enteredGate, string) {
	for _, msg := range req.Messages {
		if msg.Role != model.RoleUser {
			continue
		}
		fields := strings.Fields(msg.Content)
		if len(fields) == 0 {
			continue
		}
		name := fields[len(fields)-1]
		if g, ok := m.gates[name]; ok {
			return g, name
		}
	}
	return nil, ""
}

func (m *recordingFlowModel) next(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[name]++
	return m.counts[name]
}

func (m *recordingFlowModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	g, name := m.flow(req)
	if g == nil {
		ch <- scriptedFinalResp()
		close(ch)
		return ch, nil
	}
	if m.next(name) == 1 {
		g.once.Do(func() { close(g.entered) })
		select {
		case <-g.release:
		case <-ctx.Done():
			close(ch)
			return ch, ctx.Err()
		}
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{ID: "w1-tc", Function: model.FunctionDefinitionParam{
				Name: m.tool, Arguments: []byte(`{"request":"do it"}`),
			}}},
		}}}}
		close(ch)
		return ch, nil
	}
	ch <- scriptedFinalResp()
	close(ch)
	return ch, nil
}

func (m *recordingFlowModel) Info() model.Info { return model.Info{Name: "w1-flow"} }

// subCallChildAgent records, per delegation, which event_keys actually arrived.
type subCallChildAgent struct {
	name string
	mu   sync.Mutex
	runs [][]int64
}

func (c *subCallChildAgent) Run(_ context.Context, inv *trpcagent.Invocation) (<-chan *trpcEvent.Event, error) {
	c.mu.Lock()
	c.runs = append(c.runs, injectedEventKeys(inv))
	c.mu.Unlock()
	ch := make(chan *trpcEvent.Event)
	close(ch)
	return ch, nil
}

func (c *subCallChildAgent) snapshot() [][]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]int64, len(c.runs))
	copy(out, c.runs)
	return out
}

func (c *subCallChildAgent) Tools() []trpctool.Tool                { return nil }
func (c *subCallChildAgent) Info() trpcagent.Info                  { return trpcagent.Info{Name: c.name} }
func (c *subCallChildAgent) SubAgents() []trpcagent.Agent          { return nil }
func (c *subCallChildAgent) FindSubAgent(_ string) trpcagent.Agent { return nil }

func injectedEventKeys(inv *trpcagent.Invocation) []int64 {
	rawCtx, ok := inv.RunOptions.RuntimeState[ExternalContextKey].(json.RawMessage)
	if !ok {
		return nil
	}
	var entries []ExternalContextEntry
	if err := json.Unmarshal(rawCtx, &entries); err != nil {
		return nil
	}
	var out []int64
	for _, e := range entries {
		out = append(out, e.EventKey)
	}
	return out
}

func waitGateEntered(t *testing.T, g *enteredGate, who string) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(10 * time.Second):
		t.Fatalf("flow %s never reached its model call — harness broken", who)
	}
}

func waitChildRuns(t *testing.T, c *subCallChildAgent, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return len(c.snapshot()) >= want },
		10*time.Second, 10*time.Millisecond, "delegation did not reach the child")
}

func containsKey(keys []int64, want int64) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// TestConcurrentCallsIsolateTheirProjections 钉住 同一 agent 的两个并发真实调用（共享同一委派包装器）只能各自注入本调用私有的投影。
// - 任何一方都不得改写已发布的绑定。
func TestConcurrentCallsIsolateTheirProjections(t *testing.T) {
	store := memory.NewInMemoryStore()
	const kA, kB = int64(0x1201abcd00e01), int64(0x1201abcd00e02)
	storeEvent(t, store, kA, "A 路事件")
	storeEvent(t, store, kB, "B 路事件")

	child := &subCallChildAgent{name: "worker"}
	delegate := NewAgentToolWrapper(child, "do the work", []string{"event_keys"}, store)

	gates := map[string]*enteredGate{
		"A": {entered: make(chan struct{}), release: make(chan struct{})},
		"B": {entered: make(chan struct{}), release: make(chan struct{})},
	}
	m := &recordingFlowModel{counts: map[string]int{}, gates: gates, tool: "worker"}

	ta, err := NewTagentAgent(&TagentConfig{
		Model:             m,
		Name:              "w1parent",
		SystemPrompt:      "sp",
		MaxToolIterations: 5,
		MaxTokens:         8000,
		Tools:             []trpctool.Tool{delegate},
	})
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	ta.SetToolParentProjection()
	published := delegate.publishedCallProjection()
	require.Same(t, ta.projection, published, "cold start binds the resident projection")

	ctxA, cancelA := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelA()
	outA, err := ta.Run(ctxA, trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("A"))))
	require.NoError(t, err)
	waitGateEntered(t, gates["A"], "A")
	require.Equal(t, 1, ta.LiveCMCount())
	projA := ta.snapshotLiveCMs()[0].callProjection()
	projA.Append(memory.EventReference{EventKey: kA, EventType: "external_input", EventSummary: "A 路事件"})

	ctxB, cancelB := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelB()
	outB, err := ta.Run(ctxB, trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("B"))))
	require.NoError(t, err)
	waitGateEntered(t, gates["B"], "B")
	require.Equal(t, 2, ta.LiveCMCount(), "both calls are concurrently in flight")

	var projB *compress.SessionProjection
	for _, cm := range ta.snapshotLiveCMs() {
		if p := cm.callProjection(); p != projA {
			projB = p
		}
	}
	require.NotNil(t, projB, "call B must own a distinct call-private projection")
	projB.Append(memory.EventReference{EventKey: kB, EventType: "external_input", EventSummary: "B 路事件"})

	close(gates["A"].release)
	waitChildRuns(t, child, 1)
	gotA := child.snapshot()[0]
	require.True(t, containsKey(gotA, kA), "call A must auto-inject from its own projection, got %v", gotA)
	require.False(t, containsKey(gotA, kB), "call A must not see call B's projection key %v (got %v)", kB, gotA)

	close(gates["B"].release)
	waitChildRuns(t, child, 2)
	gotB := child.snapshot()[1]
	require.True(t, containsKey(gotB, kB), "call B must auto-inject from its own projection, got %v", gotB)
	require.False(t, containsKey(gotB, kA), "call B must not see call A's projection key %v (got %v)", kA, gotB)

	for range outA {
	}
	for range outB {
	}
	require.Zero(t, ta.LiveCMCount(), "both registrations must be reclaimed")

	require.Same(t, published, delegate.publishedCallProjection(),
		"a sub-call must never rewrite the published wrapper binding")
}

// leafTool is a non-delegation tool (exec/file/mcp shape). : the projection
// wiring must NOT touch it — it has no parentProjection field and is not an
// *AgentToolWrapper, so collectAgentToolWrappers must skip it without panicking.
type leafTool struct{ name string }

func (leafTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "leaf"}
}
func (l leafTool) Call(_ context.Context, _ []byte) (any, error) { return l.name, nil }

// innerPeel is a test stand-in for the governance decorator: it exposes Inner()
// (not Unwrap()) so the collector's Inner branch is exercised without importing
// agent/governance (which would risk an import cycle). Wrap order under test
// mirrors production: OutputLimitTool(...Inner(...)...) over the real wrapper.
type innerPeel struct{ inner trpctool.Tool }

func (p innerPeel) Declaration() *trpctool.Declaration { return p.inner.Declaration() }
func (p innerPeel) Inner() trpctool.Tool               { return p.inner }
func (p innerPeel) Call(ctx context.Context, a []byte) (any, error) {
	return p.inner.(trpctool.CallableTool).Call(ctx, a)
}

// wireThroughTransparent 复刻冷启动在发布期做的接线（SetToolParentProjection）：穿透装饰器链，
// 把每个可达的 wrapper 绑到 proj。与生产路径共用同一个 helper，因此一旦穿透逻辑退化回
// 裸的 t.(*AgentToolWrapper) 断言，本用例就会失败。调用方私有的 ContextManager 走的是调用
// 上下文自带的投影，完全不经过此路径（见 w1_concurrent_projection_test.go）；本组用例钉的是
// 已发布的绑定，以及读到它的路径外兜底。
func wireThroughTransparent(tools []trpctool.Tool, proj *compress.SessionProjection) {
	for _, w := range collectAgentToolWrappers(tools) {
		w.SetParentProjection(proj)
	}
}

func projOf(t *testing.T, store memory.MemoryStore, keys ...int64) *compress.SessionProjection {
	t.Helper()
	proj := compress.NewSessionProjection()
	for _, k := range keys {
		evt, err := store.GetEvent(k)
		require.NoError(t, err)
		proj.Append(memory.EventReference{
			EventKey:     k,
			EventType:    evt.EventType,
			EventSummary: evt.EventSummary,
		})
	}
	return proj
}

func injectedKeys(t *testing.T, mock *mockAgent) []int64 {
	t.Helper()
	require.NotNil(t, mock.lastInv, "sub-agent must have been invoked through the wrapper chain")
	rs := mock.lastInv.RunOptions.RuntimeState
	rawCtx, ok := rs[ExternalContextKey].(json.RawMessage)
	if !ok {
		return nil
	}
	var entries []ExternalContextEntry
	require.NoError(t, json.Unmarshal(rawCtx, &entries))
	var out []int64
	for _, e := range entries {
		out = append(out, e.EventKey)
	}
	return out
}

func storeEvent(t *testing.T, store memory.MemoryStore, k int64, summary string) {
	t.Helper()
	require.NoError(t, store.StoreEvent(k, memory.FullEvent{
		EventKey: k, EventType: "external_input", EventSummary: summary, Content: summary,
	}))
}

// TestAutoInjectFiresThroughTransparentWrapper 钉住 投影经装饰器接上之后，真实工具调用省略事件键时仍须自动注入父投影事件。
// - 按类型断言接线会悄悄打断这条：装饰器被隐藏、父投影为空、注入静默成空操作且无报错；
// - 因此断言必须打在真实调用路径上，而不是打在接线形状上。
func TestAutoInjectFiresThroughTransparentWrapper(t *testing.T) {
	store := memory.NewInMemoryStore()
	const k = int64(0x1201abcd00001)
	storeEvent(t, store, k, "近期部署事件")

	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	olt := NewOutputLimitTool(w, 1<<20)

	wireThroughTransparent([]trpctool.Tool{olt}, projOf(t, store, k))

	raw, _ := json.Marshal(map[string]any{"request": "分析"})
	_, err := olt.Call(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, []int64{k}, injectedKeys(t, mock),
		"auto-inject must reach the wrapper hidden behind OutputLimitTool")
}

// TestExplicitKeysTakePriority 钉住 confirms the fallback never overrides keys the model actually passed.
func TestExplicitKeysTakePriority(t *testing.T) {
	store := memory.NewInMemoryStore()
	const projK, explicitK = int64(0x1201abcd000aa), int64(0x1201abcd000bb)
	storeEvent(t, store, projK, "投影事件")
	storeEvent(t, store, explicitK, "模型指定事件")

	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	olt := NewOutputLimitTool(w, 1<<20)
	wireThroughTransparent([]trpctool.Tool{olt}, projOf(t, store, projK))

	raw, _ := json.Marshal(map[string]any{"request": "分析", "event_keys": []any{float64(explicitK)}})
	_, err := olt.Call(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, []int64{explicitK}, injectedKeys(t, mock),
		"explicit keys must win; the projection fallback must not append")
}

// TestEmptyProjectionIsGraceful 钉住 a wired wrapper with an empty projection and no model keys injects nothing and does not error.
func TestEmptyProjectionIsGraceful(t *testing.T) {
	store := memory.NewInMemoryStore()
	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	olt := NewOutputLimitTool(w, 1<<20)
	wireThroughTransparent([]trpctool.Tool{olt}, projOf(t, store))

	raw, _ := json.Marshal(map[string]any{"request": "分析"})
	_, err := olt.Call(context.Background(), raw)
	require.NoError(t, err)
	require.Empty(t, injectedKeys(t, mock), "empty projection → no injected events")
}

// TestNormalToolsUnaffected 钉住 a leaf tool (and its OutputLimitTool wrapper) is never collected, so wiring a mixed list touches only the delegation wrappers.
func TestNormalToolsUnaffected(t *testing.T) {
	store := memory.NewInMemoryStore()
	mock := &mockAgent{name: "analyzer"}
	delegate := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	leaf := NewOutputLimitTool(leafTool{name: "exec"}, 1<<20)

	got := collectAgentToolWrappers([]trpctool.Tool{leaf, NewOutputLimitTool(delegate, 1<<20)})
	require.Len(t, got, 1, "only the delegation wrapper is reachable; the leaf stays untouched")
	require.Same(t, delegate, got[0])
}

// TestPiercesNestedDecorators 钉住 the collector walks multi-layer chains (OutputLimitTool → Inner-decorator → OutputLimitTool → wrapper).
func TestPiercesNestedDecorators(t *testing.T) {
	store := memory.NewInMemoryStore()
	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	nested := NewOutputLimitTool(innerPeel{inner: NewOutputLimitTool(w, 1<<20)}, 1<<20)

	got := collectAgentToolWrappers([]trpctool.Tool{nested})
	require.Len(t, got, 1)
	require.Same(t, w, got[0], "must peel Unwrap and Inner layers alike")
}

// TestCallIsolation 钉住 两个父级各挂一份投影的两个包装器互不污染：每次真实调用只从自己绑定的投影注入。
func TestCallIsolation(t *testing.T) {
	store1 := memory.NewInMemoryStore()
	store2 := memory.NewInMemoryStore()
	const k1, k2 = int64(0x1201abcd00f01), int64(0x1201abcd00f02)
	storeEvent(t, store1, k1, "parent-1 事件")
	storeEvent(t, store2, k2, "parent-2 事件")

	g1 := &mockAgent{name: "g1"}
	g2 := &mockAgent{name: "g2"}
	w1 := NewAgentToolWrapper(g1, "a1", []string{"event_keys"}, store1)
	w2 := NewAgentToolWrapper(g2, "a2", []string{"event_keys"}, store2)
	olt1 := NewOutputLimitTool(w1, 1<<20)
	olt2 := NewOutputLimitTool(w2, 1<<20)

	wireThroughTransparent([]trpctool.Tool{olt1}, projOf(t, store1, k1))
	wireThroughTransparent([]trpctool.Tool{olt2}, projOf(t, store2, k2))

	raw, _ := json.Marshal(map[string]any{"request": "分析"})
	_, err := olt1.Call(context.Background(), raw)
	require.NoError(t, err)
	_, err = olt2.Call(context.Background(), raw)
	require.NoError(t, err)

	require.Equal(t, []int64{k1}, injectedKeys(t, g1), "g1 sees only parent-1's projection")
	require.Equal(t, []int64{k2}, injectedKeys(t, g2), "g2 sees only parent-2's projection")
}
