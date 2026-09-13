package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// R2（resident-continuity-r2-r4 1.7）：OnSpawn/OnInlineSettle 钩子与记录形态。

// task 层：inline settle 必须触发 OnInlineSettle（历史上无任何记录——第六轮 🔴3）。
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

// agent 层：EmitTaskSpawnedRecord 记录形态（类型/载荷/元数据含 task_id）。
func TestEmitTaskSpawnedRecord_Shape(t *testing.T) {
	store := memory.NewInMemoryStore()
	cm := &ContextManager{partitionID: rb2Partition, memStore: store, name: "t-agent"}
	started := time.Now()
	tk := &task.Task{ID: "t-uuid-1", Spec: task.TaskSpec{
		Kind: "command", Desc: "svc",
		Declarative: &task.Declarative{Kind: "command", Desc: "svc", Key: "svc", Command: "echo hi"},
	}, StartedAt: started}
	cm.EmitTaskSpawnedRecord(tk)

	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{rb2Partition}})
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

// agent 层：inline settle 记录形态（task_inline_record 标记 + 结构化键；registry-only）。
func TestEmitTaskInlineSettleRecord_Shape(t *testing.T) {
	store := memory.NewInMemoryStore()
	cm := &ContextManager{partitionID: rb2Partition, memStore: store, name: "t-agent"}
	tk := &task.Task{ID: "t-uuid-2", Spec: task.TaskSpec{Kind: "command", Desc: "quick"}, StartedAt: time.Now()}
	cm.EmitTaskInlineSettleRecord(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "out"})

	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{rb2Partition},
		EventTypes:   []string{tagentevent.TypeExternalInput},
	})
	require.NoError(t, err)
	require.Len(t, refs, 1)
	ev, err := store.GetEvent(refs[0].EventKey)
	require.NoError(t, err)
	require.Equal(t, "t-uuid-2", ev.Metadata["task_id"])
	require.Equal(t, "true", ev.Metadata["task_inline_record"], "inline flag marks registry-only records")
	require.NotEmpty(t, ev.Metadata["settle_status"])
}

// persistBusEvent：Source==task 的结构化键拷贝（R2 1.5——机器可辨、不解析正文）。
func TestPersistBusEvent_TaskMetadataCopied(t *testing.T) {
	store := memory.NewInMemoryStore()
	cm := &ContextManager{partitionID: rb2Partition, memStore: store, name: "t-agent"}
	evt := newTaskSettledEvent(&task.Task{ID: "t-uuid-3", Spec: task.TaskSpec{Kind: "command", Desc: "svc"}},
		task.SettleSignal{Kind: task.SettleCompleted, Output: "ok"}, 1<<20, "")
	cm.persistBusEvent(evt)

	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{rb2Partition},
		EventTypes:   []string{tagentevent.TypeExternalInput},
	})
	require.NoError(t, err)
	require.Len(t, refs, 1)
	ev, err := store.GetEvent(refs[0].EventKey)
	require.NoError(t, err)
	require.Equal(t, "t-uuid-3", ev.Metadata["task_id"], "full task_id must be copied into FullEvent.Metadata")
	require.Equal(t, "completed", ev.Metadata["settle_status"])
}

// R2（review 终审🔴）fail-before：Cancel 此前仅改内存态，事实链无 cancelled
// 终态记录 → 回放折叠以 suspect 复活（看板幽灵 + subagent 同 Key dedup 锁死）。
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

	// fail-before：无 cancelled 记录时重建为幽灵 suspect。
	if n := RebuildTaskRegistry(store, rb2Partition, tm, nil); n != 1 {
		t.Fatalf("pre-record: restored = %d, want 1 (ghost suspect)", n)
	}
	// 写 cancelled 终态记录（OnCancel 接线产物）→ 回放不再重建。
	cm := &ContextManager{partitionID: rb2Partition, memStore: store, name: "t-agent"}
	tk, _ := tm.Get("t-cancel")
	cm.EmitTaskCancelledRecord(tk)
	tm2 := task.NewTaskManager(task.TaskManagerConfig{})
	if n := RebuildTaskRegistry(store, rb2Partition, tm2, nil); n != 0 {
		t.Fatalf("post-record: restored = %d, want 0 (cancelled is terminal — ghost eliminated)", n)
	}
	if _, ok := tm2.Get("t-cancel"); ok {
		t.Fatal("cancelled task must NOT be rebuilt after the fact-chain record")
	}
}
