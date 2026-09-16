package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/tool/action"
)

// R2（resident-continuity-r2-r4 1.7/1.11）回归：spawn/inline-settle 事实链记录
// 与 RebuildTaskRegistry 纯全量回放。fail-before：无事件重建→板空（证明事件承重）。

const rb2Partition = 99

func rb2Key(ms int64) int64 { return memory.NewSnowflakeEventKey(rb2Partition, ms) }

// storeSpawned 写一条 task_spawned 记录（模拟 OnSpawn 产物）。
func storeSpawned(t *testing.T, store *memory.InMemoryStore, taskID string, decl task.Declarative, ms int64) {
	t.Helper()
	raw, err := json.Marshal(decl)
	if err != nil {
		t.Fatalf("marshal declarative: %v", err)
	}
	if err := store.StoreEvent(rb2Key(ms), memory.FullEvent{
		EventKey:     rb2Key(ms),
		PartitionID:  rb2Partition,
		EventType:    tagentevent.TypeTaskSpawned,
		EventSummary: "任务创建: " + decl.Desc,
		Content:      string(raw),
		Timestamp:    ms,
		Metadata:     map[string]string{"task_id": taskID},
	}); err != nil {
		t.Fatalf("store spawned: %v", err)
	}
}

// storeSettle 写一条 settle 记录（external_input + 结构化 Metadata，模拟
// persistBusEvent 拷贝后的形态；inline 标记可选）。
func storeSettle(t *testing.T, store *memory.InMemoryStore, taskID, status string, ms int64, inline bool) {
	t.Helper()
	md := map[string]string{"task_id": taskID, "settle_status": status}
	if inline {
		md["task_inline_record"] = "true"
	}
	if err := store.StoreEvent(rb2Key(ms), memory.FullEvent{
		EventKey:     rb2Key(ms),
		PartitionID:  rb2Partition,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "[task settled] x",
		Content:      "[task settled] x (id=" + taskID + ") " + status,
		Timestamp:    ms,
		Metadata:     md,
	}); err != nil {
		t.Fatalf("store settle: %v", err)
	}
}

func TestRebuildTaskRegistry_ActiveStates(t *testing.T) {
	now := time.Now().UnixMilli()
	store := memory.NewInMemoryStore()
	// 2 running（无 settle 记录）、1 alive-detached、2 completed（其一 inline）、1 failed。
	storeSpawned(t, store, "t-run-1", task.Declarative{Kind: "command", Desc: "svc1", Key: "svc1", Command: "svc1"}, now-60_000)
	storeSpawned(t, store, "t-run-2", task.Declarative{Kind: "command", Desc: "svc2", Command: "svc2"}, now-50_000)
	storeSpawned(t, store, "t-ad-1", task.Declarative{Kind: "command", Desc: "svc3", Command: "svc3"}, now-40_000)
	storeSpawned(t, store, "t-done-1", task.Declarative{Kind: "command", Desc: "job1", Command: "job1"}, now-30_000)
	storeSpawned(t, store, "t-done-2", task.Declarative{Kind: "command", Desc: "job2", Command: "job2"}, now-20_000)
	storeSpawned(t, store, "t-fail-1", task.Declarative{Kind: "command", Desc: "job3", Command: "job3"}, now-10_000)
	storeSettle(t, store, "t-ad-1", "alive-detached", now-35_000, false)
	storeSettle(t, store, "t-done-1", "completed", now-25_000, false)
	storeSettle(t, store, "t-done-2", "completed", now-15_000, true) // inline 记录同为终态
	storeSettle(t, store, "t-fail-1", "failed", now-5_000, false)

	tm := task.NewTaskManager(task.TaskManagerConfig{})
	restored := RebuildTaskRegistry(store, rb2Partition, tm, nil)
	if restored != 3 {
		t.Fatalf("restored = %d, want 3 (2 running→suspect + 1 alive-detached; terminal excluded)", restored)
	}
	if got, ok := tm.Get("t-run-1"); !ok || got == nil {
		t.Fatal("t-run-1 not restored")
	} else if s := got.Status(); s != task.TaskSuspect {
		t.Errorf("t-run-1 status = %v, want suspect (running degrades cross-restart)", s)
	}
	if got, ok := tm.Get("t-ad-1"); !ok || got.Status() != task.TaskAliveDetached {
		t.Errorf("t-ad-1 not restored as alive-detached (ok=%v)", ok)
	}
	for _, id := range []string{"t-done-1", "t-done-2", "t-fail-1"} {
		if _, ok := tm.Get(id); ok {
			t.Errorf("%s restored but is terminal (inline settle records must prevent ghost suspects)", id)
		}
	}
}

func TestRebuildTaskRegistry_FailBefore_NoRecordsEmpty(t *testing.T) {
	store := memory.NewInMemoryStore() // 无任务事件
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	if n := RebuildTaskRegistry(store, rb2Partition, tm, nil); n != 0 {
		t.Fatalf("restored = %d, want 0 (fail-before: no events → empty board)", n)
	}
	if len(tm.List()) != 0 {
		t.Fatal("registry must be empty without task_spawned records")
	}
}

func TestRebuildTaskRegistry_LastSettleWins(t *testing.T) {
	now := time.Now().UnixMilli()
	store := memory.NewInMemoryStore()
	// alive-detached 通知后再 failed —— 末次 settle 决定终态。
	storeSpawned(t, store, "t-x", task.Declarative{Kind: "command", Desc: "svc", Command: "svc"}, now-90_000)
	storeSettle(t, store, "t-x", "alive-detached", now-60_000, false)
	storeSettle(t, store, "t-x", "failed", now-30_000, false)
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	if n := RebuildTaskRegistry(store, rb2Partition, tm, nil); n != 0 {
		t.Fatalf("restored = %d, want 0 (last settle=failed is terminal)", n)
	}
}

func TestRebuildTaskRegistry_SubagentResumeGuidance(t *testing.T) {
	now := time.Now().UnixMilli()
	store := memory.NewInMemoryStore()
	storeSpawned(t, store, "t-sub-1", task.Declarative{
		Kind: "subagent", Desc: "researcher: 查一下", Key: "researcher:查一下",
		AgentName: "researcher", MessageBody: "查一下",
	}, now-60_000)

	tm := task.NewTaskManager(task.TaskManagerConfig{})
	redispatch := func(agentName, body string) (task.SpawnResult, error) {
		t.Logf("redispatch %s", agentName)
		return task.SpawnResult{}, nil
	}
	RebuildTaskRegistry(store, rb2Partition, tm, func(decl task.Declarative) task.TaskSpec {
		return action.SubagentSpecFromDeclarative(redispatch, decl)
	})
	got, ok := tm.Get("t-sub-1")
	if !ok {
		t.Fatal("subagent task not restored")
	}
	if got.Spec.ResumeFn == nil {
		t.Fatal("ResumeFn must exist (guidance error, not nil)")
	}
	if _, err := got.Spec.ResumeFn("继续"); err == nil || !strings.Contains(err.Error(), "relaunch") {
		t.Errorf("cross-restart subagent resume must return relaunch guidance, got err=%v", err)
	}
	if got.Spec.Relaunch == nil {
		t.Fatal("subagent Relaunch must be rebuilt (promise table)")
	}
}

// hardening-review-batch2 1.2/1.4（世系跨重启保真 roundtrip 前半）：带 Origin
// 的 task_spawned 经 RebuildTaskRegistry 恢复后，恢复闭包工厂覆盖 spec 不得
// 丢失 Origin/Key——身份以持久层为准，工厂仅补执行能力。
func TestRebuildTaskRegistry_OriginPreservedThroughRestore(t *testing.T) {
	store := memory.NewInMemoryStore()
	storeSpawned(t, store, "t-orig-1", task.Declarative{
		Kind: "command", Desc: "svc", Key: "svc-key",
		TaskID: "n-orig",
		Origin: map[string]string{"trigger_source": "meditation", "chat_id": "c-9"},
	}, time.Now().UnixMilli()-60_000)

	tm := task.NewTaskManager(task.TaskManagerConfig{})
	rebuildClosures := func(decl task.Declarative) task.TaskSpec {
		// 模拟真实工厂：只带执行能力描述，不带 Origin/Key。
		return task.TaskSpec{Kind: decl.Kind, Desc: decl.Desc,
			Declarative: &task.Declarative{Kind: decl.Kind, TaskID: decl.TaskID}}
	}
	if n := RebuildTaskRegistry(store, rb2Partition, tm, rebuildClosures); n != 1 {
		t.Fatalf("restored = %d, want 1", n)
	}
	tk, ok := tm.Get("t-orig-1")
	if !ok {
		t.Fatal("task must be restored")
	}
	if tk.Spec.Origin == nil || tk.Spec.Origin["trigger_source"] != "meditation" {
		t.Fatalf("Origin lost through restore+factory-override: %+v", tk.Spec.Origin)
	}
	if tk.Spec.Key != "svc-key" {
		t.Fatalf("Key lost through restore+factory-override: %q", tk.Spec.Key)
	}
}

// cold-eyes P1-2：stale 一次性告警（settle_status=watch）不得覆盖
// alive-detached 的末次结算语义——恢复态保持 alive_detached，detachedAt
// 从 watch 记录的 detached_at_ms 还原（真实脱离时长）。
func TestRebuildTaskRegistry_WatchNoticeDoesNotDowngradeDetached(t *testing.T) {
	store := memory.NewInMemoryStore()
	now := time.Now().UnixMilli()
	storeSpawned(t, store, "t-w", task.Declarative{
		Kind: "command", Desc: "svc", Key: "svc-w", TaskID: "n-w",
	}, now-4*60_000)

	// alive-detached 结算（带 detached_at_ms）。
	storeEventStatus(t, store, "t-w", "alive-detached", now-3*60_000, now-3*60_000)
	// 其后的 stale 一次性告警（watch + detached_at_ms 同值）。
	storeEventStatus(t, store, "t-w", "watch", now-1*60_000, now-3*60_000)

	tm := task.NewTaskManager(task.TaskManagerConfig{})
	if n := RebuildTaskRegistry(store, rb2Partition, tm, nil); n != 1 {
		t.Fatalf("restored = %d, want 1", n)
	}
	tk, ok := tm.Get("t-w")
	if !ok {
		t.Fatal("task must be restored")
	}
	if got := tk.Status(); got != task.TaskAliveDetached {
		t.Fatalf("status = %s, want alive_detached (watch must not downgrade)", got)
	}
	detachedAt := baseMilli(now - 3*60_000)
	if tk.DetachedAtMilli() == 0 {
		t.Fatal("detachedAt must be restored from detached_at_ms")
	}
	if tk.DetachedAtMilli() != detachedAt {
		t.Fatalf("detachedAt = %d, want %d (real age, not restore time)", tk.DetachedAtMilli(), detachedAt)
	}
}

func baseMilli(ms int64) int64 { return ms }

func storeEventStatus(t *testing.T, store *memory.InMemoryStore, taskID, status string, tsMs, detachedMs int64) {
	t.Helper()
	k := rb2Key(tsMs)
	ev := memory.FullEvent{
		EventKey: k, PartitionID: rb2Partition,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "[task settled] notice",
		Content:      "[task settled] svc " + status,
		Timestamp:    tsMs,
		Metadata: map[string]string{
			"task_id":       taskID,
			"settle_status": status,
		},
	}
	if detachedMs > 0 {
		ev.Metadata["detached_at_ms"] = fmt.Sprintf("%d", detachedMs)
	}
	if err := store.StoreEvent(k, ev); err != nil {
		t.Fatalf("StoreEvent: %v", err)
	}
}
