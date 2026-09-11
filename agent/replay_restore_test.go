package agent

// replay_restore_test.go — 压缩事件溯源的重放状态机与写入路径单测
// （tagent-compress-event-sourcing 任务 2.4/3.2/3.3/3.4）。
//
// 覆盖面：
//   - 快照事件 → Replace+三态回灌（多次快照后者胜；快照后普通事件继续 append）
//   - 冥想 agent_output（trigger_source=meditation）→ Mark 派生 + 照常 Append
//   - L2 降级：schema 不识别 → ERROR 留痕跳过，投影不动
//   - 写入路径：persistSnapshotEvent 落库 + 快照事件不进投影；Compressed=false 不写；
//     StoreEvent 失败 → [compress_event_write_failed] 通知

import (
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func newTestHarness(t *testing.T) (*TagentAgent, *ContextManager, *memory.InMemoryStore) {
	t.Helper()
	store := memory.NewInMemoryStore()
	cm := &ContextManager{
		name:        "tagent",
		memStore:    store,
		projection:  compress.NewSessionProjection(),
		partitionID: 7,
		contextCompressor: compress.NewContextCompressor(
			compress.NewSmartCompressor(), store, nil, 0, 0, 0,
		),
	}
	ta := &TagentAgent{contextManager: cm}
	return ta, cm, store
}

func snapshotEvent(key int64, boundary int64, threshold float64, refs []memory.EventReference) memory.FullEvent {
	raw, err := compress.MarshalSnapshot(&compress.CompressionSnapshot{
		SchemaVersion: compress.SnapshotSchemaV1,
		FullBoundary:  boundary,
		Threshold:     threshold,
		RetainedRefs:  refs,
	})
	if err != nil {
		panic(err)
	}
	return memory.FullEvent{
		EventKey:  key,
		EventType: tagentevent.TypeContextCompress,
		Timestamp: 1000,
		Metadata:  map[string]string{compress.SnapshotMetaKey: raw},
	}
}

func plainEvent(key int64, evType, trigger string) memory.FullEvent {
	md := map[string]string{}
	if trigger != "" {
		md[tagentevent.MetaKeyTriggerSource] = trigger
	}
	return memory.FullEvent{EventKey: key, EventType: evType, Timestamp: key, Metadata: md}
}

func ref(key int64) memory.EventReference {
	return memory.EventReference{EventKey: key, PartitionID: 7, EventType: "external_input",
		EventSummary: "s", Timestamp: key, Role: "user"}
}

// 3.3/3.4：Replace 收敛 + 回灌三态 + 快照后普通事件继续 append。
func TestReplay_SnapshotReplaceAndRestore(t *testing.T) {
	ta, cm, _ := newTestHarness(t)
	h := ReplayProjectionHandler(ta)

	h(plainEvent(101, tagentevent.TypeExternalInput, ""))
	h(plainEvent(102, tagentevent.TypeAgentOutput, ""))
	if got := len(cm.projection.GetAll()); got != 2 {
		t.Fatalf("pre-snapshot refs = %d, want 2", got)
	}

	retained := []memory.EventReference{ref(101)}
	h(snapshotEvent(103, 101, 42.5, retained))

	if got := len(cm.projection.GetAll()); got != 1 {
		t.Fatalf("post-snapshot refs = %d, want 1 (Replace semantics)", got)
	}
	if got := cm.projection.GetAll()[0].EventKey; got != 101 {
		t.Fatalf("post-snapshot ref key = %d, want 101", got)
	}
	if got := cm.contextCompressor.FullBoundary(); got != 101 {
		t.Fatalf("FullBoundary = %d, want 101 (回灌)", got)
	}
	if got := cm.contextCompressor.Threshold(); got != 42.5 {
		t.Fatalf("Threshold = %v, want 42.5 (回灌)", got)
	}

	// 快照后普通事件继续 append（3.3 场景三）
	h(plainEvent(104, tagentevent.TypeExternalInput, ""))
	all := cm.projection.GetAll()
	if len(all) != 2 || all[0].EventKey != 101 || all[1].EventKey != 104 {
		t.Fatalf("post-append refs = %+v, want [101 104]", all)
	}
}

// 3.3：多次快照后者胜（收敛到最后快照）。
func TestReplay_LatestSnapshotWins(t *testing.T) {
	ta, cm, _ := newTestHarness(t)
	h := ReplayProjectionHandler(ta)

	h(snapshotEvent(201, 101, 40, []memory.EventReference{ref(101)}))
	h(snapshotEvent(202, 205, 60, []memory.EventReference{ref(103), ref(104)}))

	all := cm.projection.GetAll()
	if len(all) != 2 || all[0].EventKey != 103 || all[1].EventKey != 104 {
		t.Fatalf("final refs = %+v, want [103 104] (后者胜)", all)
	}
	if got := cm.contextCompressor.FullBoundary(); got != 205 {
		t.Fatalf("FullBoundary = %d, want 205", got)
	}
	if got := cm.contextCompressor.Threshold(); got != 60 {
		t.Fatalf("Threshold = %v, want 60", got)
	}
}

// 3.2：冥想 agent_output → Mark 派生 + 照常 Append；非冥想不 Mark。
func TestReplay_MeditationMarkDerived(t *testing.T) {
	ta, cm, _ := newTestHarness(t)
	h := ReplayProjectionHandler(ta)

	h(plainEvent(301, tagentevent.TypeAgentOutput, "meditation"))
	h(plainEvent(302, tagentevent.TypeAgentOutput, ""))

	marks := cm.contextCompressor.MeditationKeysSnapshot()
	if len(marks) != 1 || marks[0] != 301 {
		t.Fatalf("meditation marks = %v, want [301]", marks)
	}
	if got := len(cm.projection.GetAll()); got != 2 {
		t.Fatalf("refs = %d, want 2（冥想事件照常投影）", got)
	}
}

// 3.3 L2 降级：schema 不识别 → 跳过且投影不动。
func TestReplay_InvalidSnapshotSkipped(t *testing.T) {
	ta, cm, _ := newTestHarness(t)
	h := ReplayProjectionHandler(ta)

	h(plainEvent(401, tagentevent.TypeExternalInput, ""))
	before := cm.projection.GetAll()

	bad := plainEvent(402, tagentevent.TypeContextCompress, "")
	bad.Metadata[compress.SnapshotMetaKey] = `{"schema_version":99,"full_boundary":1,"threshold":50,"retained_refs":[]}`
	h(bad)

	after := cm.projection.GetAll()
	if len(after) != len(before) {
		t.Fatalf("invalid snapshot changed projection: %d -> %d", len(before), len(after))
	}
	if got := cm.contextCompressor.FullBoundary(); got != 0 {
		t.Fatalf("FullBoundary = %d, want 0（未回灌）", got)
	}
}

// 2.4 写入路径：真压缩落库 + 快照事件不进投影 + compressed_keys 差集；旗标 false 不写。
func TestPersistSnapshotEvent_WritesAndSkips(t *testing.T) {
	ta, cm, store := newTestHarness(t)
	_ = ta

	refs := []memory.EventReference{ref(501), ref(502), ref(503)}
	for _, r := range refs {
		cm.projection.Append(r)
	}
	retained := []memory.EventReference{ref(502), ref(503)}
	result := compress.CompressResult{RetainedRefs: retained, Compressed: true}

	// 真实时序：真压缩已发生 ⇒ boundary 已被置为正键。这里模拟之。
	cm.contextCompressor.SetFullBoundary(502)
	if notice := cm.persistSnapshotEvent(result, refs); notice != nil {
		t.Fatalf("unexpected notice: %v", notice.Content)
	}
	stored, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{7}, Limit: 10000})
	if err != nil {
		t.Fatal(err)
	}
	var snapEv *memory.FullEvent
	for i := range stored {
		e, err := store.GetEvent(stored[i].EventKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := e.Metadata[compress.SnapshotMetaKey]; ok {
			snapEv = e
		}
	}
	if snapEv == nil {
		t.Fatal("snapshot event not persisted")
	}
	snap, err := compress.ParseSnapshot(snapEv.Metadata[compress.SnapshotMetaKey])
	if err != nil {
		t.Fatalf("stored snapshot unparseable: %v", err)
	}
	if len(snap.CompressedKeys) != 1 || snap.CompressedKeys[0] != 501 {
		t.Fatalf("compressed_keys = %v, want [501]（差集）", snap.CompressedKeys)
	}
	if len(snap.RetainedRefs) != 2 {
		t.Fatalf("retained_refs = %d, want 2", len(snap.RetainedRefs))
	}
	// 快照事件不进投影：投影仍是压缩前的 3 条（Replace 由调用点负责，此处只验 Append 侧不重复记）
	if got := len(cm.projection.GetAll()); got != 3 {
		t.Fatalf("projection = %d, want 3（快照事件不得 Append）", got)
	}

	// 旗标 false：不写
	result2 := compress.CompressResult{RetainedRefs: retained, Compressed: false}
	if notice := cm.persistSnapshotEvent(result2, refs); notice != nil {
		t.Fatalf("unexpected notice for under-budget: %v", notice.Content)
	}
}

// 写入/读取契约对称：退化 boundary=0 跳过落盘（WARN），不产生不可解析快照。
func TestPersistSnapshotEvent_DegenerateBoundarySkips(t *testing.T) {
	_, cm, store := newTestHarness(t)
	refs := []memory.EventReference{ref(701)}
	result := compress.CompressResult{RetainedRefs: refs, Compressed: true}

	if notice := cm.persistSnapshotEvent(result, refs); notice != nil {
		t.Fatalf("degenerate case should skip silently, got notice: %v", notice.Content)
	}
	stored, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{7}, Limit: 10000})
	if err != nil {
		t.Fatal(err)
	}
	for i := range stored {
		e, _ := store.GetEvent(stored[i].EventKey)
		if _, ok := e.Metadata[compress.SnapshotMetaKey]; ok {
			t.Fatal("degenerate boundary must not persist snapshot")
		}
	}
}

// 2.2 降级路径：StoreEvent 失败 → [compress_event_write_failed] 通知，不 panic 不阻断。
func TestPersistSnapshotEvent_WriteFailureNotice(t *testing.T) {
	_, cm, store := newTestHarness(t)
	cm.memStore = &failingStore{InMemoryStore: store}

	// 真实时序：真压缩已发生 ⇒ boundary 为正键（否则退化守卫会先短路，走不到 StoreEvent）。
	cm.contextCompressor.SetFullBoundary(601)
	refs := []memory.EventReference{ref(601)}
	result := compress.CompressResult{RetainedRefs: refs, Compressed: true}
	notice := cm.persistSnapshotEvent(result, refs)
	if notice == nil {
		t.Fatal("want failure notice, got nil")
	}
	if !strings.Contains(notice.Content, "[compress_event_write_failed]") {
		t.Fatalf("notice content missing marker: %q", notice.Content)
	}
	if notice.Role != model.RoleUser {
		t.Fatalf("notice role = %q, want user", notice.Role)
	}
}

// failingStore 只让 StoreEvent 失败（2.2 降级路径专用），其余透传。
type failingStore struct {
	*memory.InMemoryStore
}

func (f *failingStore) StoreEvent(key int64, event memory.FullEvent) error {
	return &storeErr{}
}

type storeErr struct{}

func (*storeErr) Error() string { return "injected store failure" }
