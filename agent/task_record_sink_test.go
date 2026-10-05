// 本文件负责从任务记录重建登记：活动态恢复、失败先于记录时为空、最后一次结算胜出、恢复时
// 保留来源袋、观察通知不得降级已脱离态，以及记录钩子的外发时机。
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const sinkPartition = 99

func sinkEventKey(ms int64) int64 { return memory.NewSnowflakeEventKey(sinkPartition, ms) }

// storeSpawned 写一条 task_spawned 记录（模拟 OnSpawn 产物）。
func storeSpawned(t *testing.T, store *memory.InMemoryStore, taskID string, decl task.Declarative, ms int64) {
	t.Helper()
	raw, err := json.Marshal(decl)
	if err != nil {
		t.Fatalf("marshal declarative: %v", err)
	}
	if err := store.StoreEvent(sinkEventKey(ms), memory.FullEvent{
		EventKey:     sinkEventKey(ms),
		PartitionID:  sinkPartition,
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
		md[tagentevent.MetaKeyTaskInlineRecord] = "true"
	}
	if err := store.StoreEvent(sinkEventKey(ms), memory.FullEvent{
		EventKey:     sinkEventKey(ms),
		PartitionID:  sinkPartition,
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
	storeSpawned(t, store, "t-run-1", task.Declarative{Kind: "command", Desc: "svc1", Key: "svc1", Command: "svc1"}, now-60_000)
	storeSpawned(t, store, "t-run-2", task.Declarative{Kind: "command", Desc: "svc2", Command: "svc2"}, now-50_000)
	storeSpawned(t, store, "t-ad-1", task.Declarative{Kind: "command", Desc: "svc3", Command: "svc3"}, now-40_000)
	storeSpawned(t, store, "t-done-1", task.Declarative{Kind: "command", Desc: "job1", Command: "job1"}, now-30_000)
	storeSpawned(t, store, "t-done-2", task.Declarative{Kind: "command", Desc: "job2", Command: "job2"}, now-20_000)
	storeSpawned(t, store, "t-fail-1", task.Declarative{Kind: "command", Desc: "job3", Command: "job3"}, now-10_000)
	storeSettle(t, store, "t-ad-1", "alive-detached", now-35_000, false)
	storeSettle(t, store, "t-done-1", "completed", now-25_000, false)
	storeSettle(t, store, "t-done-2", "completed", now-15_000, true)
	storeSettle(t, store, "t-fail-1", "failed", now-5_000, false)

	tm := task.NewTaskManager(task.TaskManagerConfig{})
	restored := RebuildTaskRegistry(store, sinkPartition, tm, nil)
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
	store := memory.NewInMemoryStore()
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	if n := RebuildTaskRegistry(store, sinkPartition, tm, nil); n != 0 {
		t.Fatalf("restored = %d, want 0 (fail-before: no events → empty board)", n)
	}
	if len(tm.List()) != 0 {
		t.Fatal("registry must be empty without task_spawned records")
	}
}

func TestRebuildTaskRegistry_LastSettleWins(t *testing.T) {
	now := time.Now().UnixMilli()
	store := memory.NewInMemoryStore()
	storeSpawned(t, store, "t-x", task.Declarative{Kind: "command", Desc: "svc", Command: "svc"}, now-90_000)
	storeSettle(t, store, "t-x", "alive-detached", now-60_000, false)
	storeSettle(t, store, "t-x", "failed", now-30_000, false)
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	if n := RebuildTaskRegistry(store, sinkPartition, tm, nil); n != 0 {
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
	redispatch := func(_ context.Context, agentName, body string, _ *task.Overrides) (task.SpawnResult, error) {
		t.Logf("redispatch %s", agentName)
		return task.SpawnResult{}, nil
	}
	RebuildTaskRegistry(store, sinkPartition, tm, func(decl task.Declarative) task.TaskSpec {
		return action.SubagentSpecFromDeclarative(redispatch, decl)
	})
	got, ok := tm.Get("t-sub-1")
	if !ok {
		t.Fatal("subagent task not restored")
	}
	if got.Spec.ResumeFn == nil {
		t.Fatal("ResumeFn must exist (guidance error, not nil)")
	}
	if _, err := got.Spec.ResumeFn(context.Background(), "继续"); err == nil || !strings.Contains(err.Error(), "relaunch") {
		t.Errorf("cross-restart subagent resume must return relaunch guidance, got err=%v", err)
	}
	if got.Spec.Relaunch == nil {
		t.Fatal("subagent Relaunch must be rebuilt (promise table)")
	}
}

// TestRebuildTaskRegistry_OriginPreservedThroughRestore 钉住 带来源袋的派生记录经重建恢复后，来源与索引键都不得丢失。
// - 恢复闭包可补齐执行能力，但身份以持久层为准；
// - 这是世系跨重启保真的前半段。
func TestRebuildTaskRegistry_OriginPreservedThroughRestore(t *testing.T) {
	store := memory.NewInMemoryStore()
	storeSpawned(t, store, "t-orig-1", task.Declarative{
		Kind: "command", Desc: "svc", Key: "svc-key",
		TaskID: "n-orig",
		Origin: map[string]string{"trigger_source": "meditation", "chat_id": "c-9"},
	}, time.Now().UnixMilli()-60_000)

	tm := task.NewTaskManager(task.TaskManagerConfig{})
	rebuildClosures := func(decl task.Declarative) task.TaskSpec {
		return task.TaskSpec{Kind: decl.Kind, Desc: decl.Desc,
			Declarative: &task.Declarative{Kind: decl.Kind, TaskID: decl.TaskID}}
	}
	if n := RebuildTaskRegistry(store, sinkPartition, tm, rebuildClosures); n != 1 {
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

// TestRebuildTaskRegistry_WatchNoticeDoesNotDowngradeDetached 钉住 一次性陈旧告警不得覆盖已脱离态的末次结算语义。
// - 恢复态保持已脱离，脱离时刻从该观察记录自身的字段还原，保住真实脱离时长。
func TestRebuildTaskRegistry_WatchNoticeDoesNotDowngradeDetached(t *testing.T) {
	store := memory.NewInMemoryStore()
	now := time.Now().UnixMilli()
	storeSpawned(t, store, "t-w", task.Declarative{
		Kind: "command", Desc: "svc", Key: "svc-w", TaskID: "n-w",
	}, now-4*60_000)

	storeEventStatus(t, store, "t-w", "alive-detached", now-3*60_000, now-3*60_000)
	storeEventStatus(t, store, "t-w", "watch", now-1*60_000, now-3*60_000)

	tm := task.NewTaskManager(task.TaskManagerConfig{})
	if n := RebuildTaskRegistry(store, sinkPartition, tm, nil); n != 1 {
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
	k := sinkEventKey(tsMs)
	ev := memory.FullEvent{
		EventKey: k, PartitionID: sinkPartition,
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

// TestTaskManager_InlineSettleEmitsRecordHook 钉住 task 层的 inline settle 必须触发 OnInlineSettle，使该路径确实产生记录钩子事件。
func TestTaskManager_InlineSettleEmitsRecordHook(t *testing.T) {
	var spawned []*task.Task
	var inline []string
	tm := task.NewTaskManager(task.TaskManagerConfig{
		OnSpawn: func(tk *task.Task) { spawned = append(spawned, tk) },
		OnInlineSettle: func(tk *task.Task, sig task.SettleSignal) {
			inline = append(inline, tk.ID)
		},
	})
	det := task.NewFuncSettleDetector(context.Background(), func(context.Context) (string, error) {
		return "done", nil
	}, 0)
	res := tm.Spawn(task.TaskSpec{
		Kind: "command", Desc: "quick", Key: "quick",
		Declarative: &task.Declarative{Kind: "command", Desc: "quick", Key: "quick"},
	}, det)
	require.True(t, res.Settled, "fast settle must be inline")
	require.Len(t, spawned, 1, "OnSpawn fires on registration")
	require.Equal(t, res.Task.ID, spawned[0].ID)
	require.Equal(t, []string{res.Task.ID}, inline,
		"inline settle MUST emit OnInlineSettle (fail-before: historically no record → replay ghost)")
}

// TestEmitTaskSpawnedRecord_Shape agent 层：EmitTaskSpawnedRecord 记录形态（类型/载荷/元数据含 task_id）。
func TestEmitTaskSpawnedRecord_Shape(t *testing.T) {
	store := memory.NewInMemoryStore()
	cm := &ContextManager{partitionID: sinkPartition, memStore: store, name: "t-agent"}
	started := time.Now()
	tk := &task.Task{ID: "t-uuid-1", Spec: task.TaskSpec{
		Kind: "command", Desc: "svc",
		Declarative: &task.Declarative{Kind: "command", Desc: "svc", Key: "svc", Command: "echo hi"},
	}, StartedAt: started}
	cm.EmitTaskSpawnedRecord(tk)

	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{sinkPartition}})
	require.NoError(t, err)
	require.Len(t, refs, 1)
	ev, err := store.GetEvent(refs[0].EventKey)
	require.NoError(t, err)
	require.Equal(t, tagentevent.TypeTaskSpawned, ev.EventType)
	require.Equal(t, "t-uuid-1", ev.Metadata["task_id"])
	var decl task.Declarative
	require.NoError(t, json.Unmarshal([]byte(ev.Content), &decl))
	require.Equal(t, "echo hi", decl.Command)
	require.Equal(t, started.UnixMilli(), decl.StartedAtMilli, "StartedAt is backfilled from task")
}

// TestEmitTaskSpawnedRecord_OriginBackfilled 钉住 派生记录必须把运行态的来源袋补填进持久化的声明式材料。
// - 补填发生在派生入口，声明式构造点本身不携带来源；
// - 否则恢复后的任务退化为无谱系，可能被宿主误投递；
// - 声明式已带来源时以声明式为准，不得覆盖。
func TestEmitTaskSpawnedRecord_OriginBackfilled(t *testing.T) {
	store := memory.NewInMemoryStore()
	cm := &ContextManager{partitionID: sinkPartition, memStore: store, name: "t-agent"}
	started := time.Now()
	tk := &task.Task{ID: "t-uuid-origin", Spec: task.TaskSpec{
		Kind: "command", Desc: "svc",
		Origin:      map[string]string{"trigger_source": "meditation", "chat_id": "c-1"},
		Declarative: &task.Declarative{Kind: "command", Desc: "svc", Key: "svc"},
	}, StartedAt: started}
	cm.EmitTaskSpawnedRecord(tk)

	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{sinkPartition}})
	require.NoError(t, err)
	require.Len(t, refs, 1)
	ev, err := store.GetEvent(refs[0].EventKey)
	require.NoError(t, err)
	var decl task.Declarative
	require.NoError(t, json.Unmarshal([]byte(ev.Content), &decl))
	require.Equal(t, map[string]string{"trigger_source": "meditation", "chat_id": "c-1"},
		decl.Origin, "runtime Spec.Origin must be backfilled into the persisted declarative")

	tk2 := &task.Task{ID: "t-uuid-origin2", Spec: task.TaskSpec{
		Kind: "command", Desc: "svc2",
		Origin:      map[string]string{"trigger_source": "runtime"},
		Declarative: &task.Declarative{Kind: "command", Desc: "svc2", Origin: map[string]string{"trigger_source": "declared"}},
	}, StartedAt: started}
	cm.EmitTaskSpawnedRecord(tk2)
	refs2, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{sinkPartition}})
	require.NoError(t, err)
	require.Len(t, refs2, 2)
	ev2, err := store.GetEvent(refs2[1].EventKey)
	require.NoError(t, err)
	var decl2 task.Declarative
	require.NoError(t, json.Unmarshal([]byte(ev2.Content), &decl2))
	require.Equal(t, map[string]string{"trigger_source": "declared"}, decl2.Origin,
		"declared Origin wins — backfill must not overwrite")
}

// TestEmitTaskInlineSettleRecord_Shape agent 层：inline settle 记录形态（task_inline_record 标记 + 结构化键；registry-only）。
func TestEmitTaskInlineSettleRecord_Shape(t *testing.T) {
	store := memory.NewInMemoryStore()
	cm := &ContextManager{partitionID: sinkPartition, memStore: store, name: "t-agent"}
	tk := &task.Task{ID: "t-uuid-2", Spec: task.TaskSpec{Kind: "command", Desc: "quick"}, StartedAt: time.Now()}
	cm.EmitTaskInlineSettleRecord(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "out"})

	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{sinkPartition},
		EventTypes:   []string{tagentevent.TypeExternalInput},
	})
	require.NoError(t, err)
	require.Len(t, refs, 1)
	ev, err := store.GetEvent(refs[0].EventKey)
	require.NoError(t, err)
	require.Equal(t, "t-uuid-2", ev.Metadata["task_id"])
	require.Equal(t, "true", ev.Metadata[tagentevent.MetaKeyTaskInlineRecord], "inline flag marks registry-only records")
	require.NotEmpty(t, ev.Metadata["settle_status"])
}

// TestPersistBusEvent_TaskMetadataCopied persistBusEvent：Source==task 的结构化键拷贝（R2 1.5——机器可辨、不解析正文）。
func TestPersistBusEvent_TaskMetadataCopied(t *testing.T) {
	store := memory.NewInMemoryStore()
	cm := &ContextManager{partitionID: sinkPartition, memStore: store, name: "t-agent"}
	evt := newTaskSettledEvent(&task.Task{ID: "t-uuid-3", Spec: task.TaskSpec{Kind: "command", Desc: "svc"}},
		task.SettleSignal{Kind: task.SettleCompleted, Output: "ok"}, 1<<20, "")
	cm.persistBusEvent(evt)

	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{sinkPartition},
		EventTypes:   []string{tagentevent.TypeExternalInput},
	})
	require.NoError(t, err)
	require.Len(t, refs, 1)
	ev, err := store.GetEvent(refs[0].EventKey)
	require.NoError(t, err)
	require.Equal(t, "t-uuid-3", ev.Metadata["task_id"], "full task_id must be copied into FullEvent.Metadata")
	require.Equal(t, "completed", ev.Metadata["settle_status"])
}

// TestCancel_EmitsCancelledRecord_FailBefore 钉住 Cancel 此前仅改内存态，事实链无 cancelled 终态记录 → 回放折叠以 suspect 复活（看板幽灵 + subagent 同 Key dedup 锁死）。
func TestCancel_EmitsCancelledRecord_FailBefore(t *testing.T) {
	store := memory.NewInMemoryStore()
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	storeSpawned(t, store, "t-cancel", task.Declarative{
		Kind: "command", Desc: "svc", TaskID: "n-x",
	}, time.Now().UnixMilli()-60_000)
	tm.RestoreTask("t-cancel", task.TaskSpec{
		Kind: "command", Desc: "svc",
		Declarative: &task.Declarative{Kind: "command", Desc: "svc", TaskID: "n-x"},
	}, time.Now(), task.TaskSuspect)

	if n := RebuildTaskRegistry(store, sinkPartition, tm, nil); n != 1 {
		t.Fatalf("pre-record: restored = %d, want 1 (ghost suspect)", n)
	}
	cm := &ContextManager{partitionID: sinkPartition, memStore: store, name: "t-agent"}
	tk, _ := tm.Get("t-cancel")
	cm.EmitTaskCancelledRecord(tk)
	tm2 := task.NewTaskManager(task.TaskManagerConfig{})
	if n := RebuildTaskRegistry(store, sinkPartition, tm2, nil); n != 0 {
		t.Fatalf("post-record: restored = %d, want 0 (cancelled is terminal — ghost eliminated)", n)
	}
	if _, ok := tm2.Get("t-cancel"); ok {
		t.Fatal("cancelled task must NOT be rebuilt after the fact-chain record")
	}
}

// TestInjectLiveTaskBoard_WiredAfterConstruction 钉住 委派入口在上下文管理器构造之后才接上，因此面板回调的空值判定必须在调用时而非注册时。
// - 有在途任务时面板必须被注入；注册期设守卫会让它永久缺席。
func TestInjectLiveTaskBoard_WiredAfterConstruction(t *testing.T) {
	cm := &ContextManager{}
	cm.taskController = &fakeTaskController{tasks: []*task.Task{
		task.NewTaskFixture("t1", "npm build", task.TaskRunning, time.Now()),
	}}

	args := &model.BeforeModelArgs{Request: &model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	}}
	cm.injectLiveTaskBoard(args)

	var found bool
	for _, m := range args.Request.Messages {
		if strings.Contains(m.Content, "后台任务看板") {
			found = true
		}
	}
	if !found {
		t.Error("board must inject even when taskController is wired after construction")
	}
}

// TestInjectLiveTaskBoard_NilControllerSafe 钉住 a nil taskController at call time is a safe no-op (no panic, nothing injected).
func TestInjectLiveTaskBoard_NilControllerSafe(t *testing.T) {
	cm := &ContextManager{}
	args := &model.BeforeModelArgs{Request: &model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	}}
	cm.injectLiveTaskBoard(args)
	if len(args.Request.Messages) != 1 {
		t.Errorf("nil taskController should inject nothing, got %d msgs", len(args.Request.Messages))
	}
}
