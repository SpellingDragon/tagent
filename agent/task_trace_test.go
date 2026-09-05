package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
)

// TestNewTaskSettledEvent_CarriesTraceAnchor 验证 T-B task span link 轻量实现的管道末端：
// task Origin 含 trace_id/span_id（RunFlow 从 turn span 经 spanTraceIDs 捕获并盖章到
// OriginSpawner.Origin）→ settle 时经 Origin→Metadata 管道写入 task_settled 事件 Metadata，
// 使异步任务关联回触发它的 turn trace（指令2「一套数据模式」延伸到异步链路，零 task 包侵入）。
func TestNewTaskSettledEvent_CarriesTraceAnchor(t *testing.T) {
	tk := &task.Task{ID: "t1", Spec: task.TaskSpec{Desc: "x", Origin: map[string]string{
		tagentevent.MetaKeyTraceID: "trace-abc",
		tagentevent.MetaKeySpanID:  "span-def",
		"chat_id":                  "u1",
	}}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "done"}, 0, "")

	if evt.Metadata[tagentevent.MetaKeyTraceID] != "trace-abc" {
		t.Fatalf("task_settled 应携带原 turn trace_id, got %v", evt.Metadata)
	}
	if evt.Metadata[tagentevent.MetaKeySpanID] != "span-def" {
		t.Fatalf("task_settled 应携带原 turn span_id, got %v", evt.Metadata)
	}
	// trace 锚点与既有 origin baggage（chat_id 路由）共存，不互斥。
	if evt.Metadata["chat_id"] != "u1" {
		t.Fatalf("trace 锚点应与 origin baggage 共存, got %v", evt.Metadata)
	}
}

// TestNewTaskSettledEvent_NoTraceAnchorSafe 验证无 trace 锚点（未启用 OTLP/noop span）时
// task_settled 事件正常构建（trace_id 缺省不报错，向后兼容）。
func TestNewTaskSettledEvent_NoTraceAnchorSafe(t *testing.T) {
	tk := &task.Task{ID: "t2", Spec: task.TaskSpec{Desc: "x", Origin: map[string]string{"chat_id": "u1"}}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "done"}, 0, "")
	if _, has := evt.Metadata[tagentevent.MetaKeyTraceID]; has {
		t.Fatal("无 trace 锚点时不应凭空出现 trace_id")
	}
	if evt.Metadata["chat_id"] != "u1" {
		t.Fatal("origin baggage 应正常携带")
	}
}
