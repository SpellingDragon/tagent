// 本文件负责会话投影的折叠语义：带键事件幂等追加、重建按当前引用整表重算键集、读取返回
// 拷贝，因此它不是历史缓冲，也不会把同一条事实注入上下文两次。
// 契约: docs/wiki/agent/compression-and-telemetry.md#projection-fold
package compress

import (
	"sync"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestSessionProjection_AppendAndGetAll(t *testing.T) {
	p := NewSessionProjection()
	if p.Len() != 0 {
		t.Fatalf("expected empty projection, got len=%d", p.Len())
	}

	ref := memory.EventReference{
		EventKey:     123,
		PartitionID:  42,
		EventType:    "external_input",
		EventSummary: "hello",
		Timestamp:    1000,
	}
	p.Append(ref)

	if p.Len() != 1 {
		t.Fatalf("expected len=1, got %d", p.Len())
	}

	all := p.GetAll()
	if len(all) != 1 {
		t.Fatalf("expected GetAll len=1, got %d", len(all))
	}
	if all[0].EventKey != 123 {
		t.Fatalf("expected EventKey=123, got %d", all[0].EventKey)
	}

	all[0].EventKey = 999
	all = p.GetAll()
	if all[0].EventKey != 123 {
		t.Fatalf("projection should return defensive copy")
	}
}

func TestSessionProjection_Replace(t *testing.T) {
	p := NewSessionProjection()
	p.Append(memory.EventReference{EventKey: 1})
	p.Append(memory.EventReference{EventKey: 2})

	p.Replace([]memory.EventReference{
		{EventKey: 3},
	})

	if p.Len() != 1 {
		t.Fatalf("expected len=1 after Replace, got %d", p.Len())
	}
	if p.GetAll()[0].EventKey != 3 {
		t.Fatalf("expected EventKey=3, got %d", p.GetAll()[0].EventKey)
	}
}

func TestSessionProjection_Concurrent(t *testing.T) {
	p := NewSessionProjection()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			p.Append(memory.EventReference{EventKey: int64(i)})
		}
		done <- struct{}{}
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = p.GetAll()
		}
		done <- struct{}{}
	}()

	<-done
	<-done

	if p.Len() != 100 {
		t.Fatalf("expected len=100, got %d", p.Len())
	}
}

// TestProjection_IdempotentAppend L1: same EventKey (>0) appended twice → only one kept.
func TestProjection_IdempotentAppend(t *testing.T) {
	p := NewSessionProjection()
	p.Append(memory.EventReference{EventKey: 100, Role: "tool"})
	p.Append(memory.EventReference{EventKey: 100, Role: "tool"})
	p.Append(memory.EventReference{EventKey: 101, Role: "user"})
	if got := p.Len(); got != 2 {
		t.Errorf("duplicate key should be skipped: len=%d, want 2", got)
	}
}

// TestProjection_ZeroKeyNotDeduped L1: EventKey==0 (unkeyed) must NOT be deduped.
func TestProjection_ZeroKeyNotDeduped(t *testing.T) {
	p := NewSessionProjection()
	p.Append(memory.EventReference{EventKey: 0, Role: "user"})
	p.Append(memory.EventReference{EventKey: 0, Role: "user"})
	if got := p.Len(); got != 2 {
		t.Errorf("key==0 must not dedup: len=%d, want 2", got)
	}
}

// TestProjection_ReplaceRebuildsSeen 钉住 整表替换必须按新引用一致地重算去重集。
// - 被折叠掉的键重新可追加；仍在新引用里的键继续被去重。
func TestProjection_ReplaceRebuildsSeen(t *testing.T) {
	p := NewSessionProjection()
	p.Append(memory.EventReference{EventKey: 1})
	p.Replace([]memory.EventReference{{EventKey: 2}})
	p.Append(memory.EventReference{EventKey: 1})
	p.Append(memory.EventReference{EventKey: 2})

	got := p.GetAll()
	var keys []int64
	for _, r := range got {
		keys = append(keys, r.EventKey)
	}
	if len(keys) != 2 || keys[0] != 2 || keys[1] != 1 {
		t.Errorf("after Replace, seen must mirror refs; got keys=%v, want [2 1]", keys)
	}
}

// TestProjection_ConcurrentAppendSafe L1: concurrent appends of the same key stay idempotent + race-free.
func TestProjection_ConcurrentAppendSafe(t *testing.T) {
	p := NewSessionProjection()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); p.Append(memory.EventReference{EventKey: 7}) }()
	}
	wg.Wait()
	if got := p.Len(); got != 1 {
		t.Errorf("concurrent dup appends: len=%d, want 1", got)
	}
}

// prefixedMsg builds a message carrying the [evt_KEY|type] prefix, the
// primary segmentation signal (task-skeleton D1).
func prefixedMsg(role model.Role, key int64, evtType, content string) model.Message {
	return model.Message{
		Role:    role,
		Content: prefixEventKey(content, memory.EventReference{EventKey: key, EventType: evtType}),
	}
}

// TestSegmentMessages_CompleteTurns 钉住 agent_output closes each segment .
func TestSegmentMessages_CompleteTurns(t *testing.T) {
	msgs := []model.Message{
		prefixedMsg(model.RoleUser, 1, tagentevent.TypeExternalInput, "task A"),
		prefixedMsg(model.RoleAssistant, 2, tagentevent.TypeThinkingPlan, "plan A"),
		prefixedMsg(model.RoleTool, 3, tagentevent.TypeActionCommand, "tool A"),
		prefixedMsg(model.RoleAssistant, 4, tagentevent.TypeAgentOutput, "reply A"),
		prefixedMsg(model.RoleUser, 5, tagentevent.TypeExternalInput, "task B"),
		prefixedMsg(model.RoleAssistant, 6, tagentevent.TypeAgentOutput, "reply B"),
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.Len(t, segments[0].Messages, 4)
	assert.True(t, segments[1].IsComplete)
	assert.Len(t, segments[1].Messages, 2)
}

// TestSegmentMessages_ConsecutiveExternalInputs 钉住 user re-sends without an agent reply stay in one in-progress segment.
func TestSegmentMessages_ConsecutiveExternalInputs(t *testing.T) {
	msgs := []model.Message{
		prefixedMsg(model.RoleUser, 1, tagentevent.TypeExternalInput, "task A"),
		prefixedMsg(model.RoleAssistant, 2, tagentevent.TypeAgentOutput, "reply A"),
		prefixedMsg(model.RoleUser, 3, tagentevent.TypeExternalInput, "task B"),
		prefixedMsg(model.RoleUser, 4, tagentevent.TypeExternalInput, "task C"),
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.False(t, segments[1].IsComplete)
	assert.Len(t, segments[1].Messages, 2, "consecutive external_input must merge into one in-progress segment")
}

// TestSegmentMessages_TrailingWithoutOutput 钉住 a tail without agent_output is an in-progress segment.
func TestSegmentMessages_TrailingWithoutOutput(t *testing.T) {
	msgs := []model.Message{
		prefixedMsg(model.RoleUser, 1, tagentevent.TypeExternalInput, "task A"),
		prefixedMsg(model.RoleAssistant, 2, tagentevent.TypeAgentOutput, "reply A"),
		prefixedMsg(model.RoleUser, 3, tagentevent.TypeExternalInput, "task B"),
		prefixedMsg(model.RoleAssistant, 4, tagentevent.TypeThinkingPlan, "plan B"),
		prefixedMsg(model.RoleTool, 5, tagentevent.TypeActionCommand, "tool B"),
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.False(t, segments[1].IsComplete)
	assert.Len(t, segments[1].Messages, 3)
}

// TestSegmentMessages_HeuristicFallback 钉住 unprefixed messages fall back to the role heuristic — assistant without tool_calls closes the turn.
func TestSegmentMessages_HeuristicFallback(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "do something"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{
			{ID: "tc1", Function: model.FunctionDefinitionParam{Name: "echo"}},
		}},
		{Role: model.RoleTool, Content: "result"},
		{Role: model.RoleAssistant, Content: "final"},
		{Role: model.RoleUser, Content: "next task"},
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete, "assistant without tool_calls closes the turn")
	assert.Len(t, segments[0].Messages, 4)
	assert.False(t, segments[1].IsComplete, "pending user input is an in-progress segment")
}

func TestSegmentMessages_Empty(t *testing.T) {
	assert.Nil(t, SegmentMessages(nil))
	assert.Nil(t, SegmentMessages([]model.Message{}))
}

// TestIsSkeletonMessage 钉住 skeleton vs intermediate is a pure event-type function; content is never read.
func TestIsSkeletonMessage(t *testing.T) {
	skeleton := []model.Message{
		prefixedMsg(model.RoleUser, 1, tagentevent.TypeExternalInput, "in"),
		prefixedMsg(model.RoleAssistant, 2, tagentevent.TypeAgentOutput, "out"),
		prefixedMsg(model.RoleUser, -3, tagentevent.TypeContextCompress, "compacted"),
	}
	for i := range skeleton {
		assert.True(t, IsSkeletonMessage(&skeleton[i]), "message %d should be skeleton", i)
	}

	intermediate := []model.Message{
		prefixedMsg(model.RoleTool, 4, tagentevent.TypeActionCommand, "tool result"),
		prefixedMsg(model.RoleAssistant, 5, tagentevent.TypeThinkingPlan, "planning"),
	}
	for i := range intermediate {
		assert.False(t, IsSkeletonMessage(&intermediate[i]), "message %d should be intermediate", i)
	}
}
