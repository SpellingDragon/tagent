// 本文件负责工具结果超限的处理形状：未超限透传，超限内联摘要＋全文落盘并留票据，nil 与
// 结构体结果都有确定行为。
// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// mockCallableTool is a test tool that returns a predefined result.
type mockCallableTool struct {
	declaration *trpctool.Declaration
	result      any
	err         error
}

func (m *mockCallableTool) Declaration() *trpctool.Declaration {
	return m.declaration
}

func (m *mockCallableTool) Call(_ context.Context, _ []byte) (any, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestOutputLimitTool_Declaration_Passthrough(t *testing.T) {
	decl := &trpctool.Declaration{Name: "test-tool", Description: "test"}
	inner := &mockCallableTool{declaration: decl}
	wrapper := NewOutputLimitTool(inner, 1000)

	got := wrapper.Declaration()
	if got != decl {
		t.Fatalf("Declaration() = %v, want %v", got, decl)
	}
}

func TestOutputLimitTool_Call_UnderLimit(t *testing.T) {
	inner := &mockCallableTool{result: "hello world"}
	wrapper := NewOutputLimitTool(inner, 1000)

	got, err := wrapper.Call(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("Call() = %v, want %q", got, "hello world")
	}
}

func TestOutputLimitTool_Call_OverLimit(t *testing.T) {
	longOutput := strings.Repeat("a", 500)
	inner := &mockCallableTool{result: longOutput}
	wrapper := NewOutputLimitTool(inner, 100)

	got, err := wrapper.Call(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultStr, ok := got.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", got)
	}

	if !strings.Contains(resultStr, "[output_too_large]") {
		t.Fatalf("expected [output_too_large] marker, got: %s", resultStr[:min(200, len(resultStr))])
	}
	if !strings.Contains(resultStr, "超过上限 100") {
		t.Fatalf("expected limit info, got: %s", resultStr[:min(200, len(resultStr))])
	}
	if !strings.Contains(resultStr, "完整内容已保存到") {
		t.Fatalf("expected file save info, got: %s", resultStr[:min(200, len(resultStr))])
	}
}

func TestOutputLimitTool_Call_NilResult(t *testing.T) {
	inner := &mockCallableTool{result: nil}
	wrapper := NewOutputLimitTool(inner, 100)

	got, err := wrapper.Call(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil result, got %v", got)
	}
}

func TestOutputLimitTool_Call_StructResult(t *testing.T) {
	type Result struct {
		Text string `json:"text"`
	}
	inner := &mockCallableTool{result: Result{Text: "short"}}
	wrapper := NewOutputLimitTool(inner, 1000)

	got, err := wrapper.Call(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := json.Marshal(got)
	if len(data) > 1000 {
		t.Fatalf("result should be under limit, got %d bytes", len(data))
	}
}

func TestOutputLimitTool_Call_StructResult_OverLimit(t *testing.T) {
	type Result struct {
		Text string `json:"text"`
	}
	inner := &mockCallableTool{result: Result{Text: strings.Repeat("x", 200)}}
	wrapper := NewOutputLimitTool(inner, 50)

	got, err := wrapper.Call(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultStr, ok := got.(string)
	if !ok {
		t.Fatalf("expected string result after save-to-file, got %T", got)
	}
	if !strings.Contains(resultStr, "[output_too_large]") {
		t.Fatalf("expected [output_too_large] marker")
	}
}

func TestOutputLimitTool_SaveToFile(t *testing.T) {
	wrapper := NewOutputLimitTool(&mockCallableTool{result: "test"}, 1000)
	tmpDir := t.TempDir()
	wrapper.SetWorkspace(tmpDir)

	data := []byte("test content")
	path := wrapper.saveToFile(data)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist at %s: %v", path, err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if string(saved) != "test content" {
		t.Fatalf("file content = %q, want %q", string(saved), "test content")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestRunFlow_SlowConsumer_OverflowsToDisk 钉住 从不读取的消费者不得把主循环堵在宽限期之后：完整事件落盘，票据摘要非阻塞尝试。
// - 断言点在每次发往输出通道所用的投递层，而不是只测工具本身。
// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
func TestRunFlow_SlowConsumer_OverflowsToDisk(t *testing.T) {
	dir := t.TempDir()
	cm := &ContextManager{
		outputCh:    make(chan *event.Event, 1),
		overflowDir: filepath.Join(dir, "output-overflow"),
	}
	cm.outputCh <- &event.Event{}

	evt := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{{Message: model.Message{
				Role:    "assistant",
				Content: longContent(),
			}}},
		},
	}
	done := make(chan bool, 1)
	go func() { done <- cm.deliverEvent(context.Background(), evt) }()

	select {
	case <-done:
	case <-time.After(outputSendGrace + 2*time.Second):
		t.Fatal("F2 regression: deliverEvent blocked on a stalled consumer")
	}

	matches, err := filepath.Glob(filepath.Join(dir, "output-overflow", "overflow-*.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("overflow file not persisted: matches=%v err=%v", matches, err)
	}
	if len(cm.outputCh) != 1 {
		t.Fatalf("ticket must not block: channel len=%d", len(cm.outputCh))
	}
}

// TestOverflowTicketExcerpt 钉住 bounds the ticket: head+tail kept, middle replaced by the persisted marker, StateDelta carries the retrieval path.
func TestOverflowTicketExcerpt(t *testing.T) {
	evt := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{{Message: model.Message{Content: longContent()}}},
		},
	}
	ticket := overflowTicket(evt, "/tmp/x.json")
	got := ticket.Response.Choices[0].Message.Content
	if len(got) >= len(longContent()) {
		t.Fatalf("ticket not excerpted: len=%d", len(got))
	}
	if string(ticket.StateDelta["output_overflow_path"]) != "/tmp/x.json" {
		t.Fatal("ticket missing output_overflow_path")
	}
	if evt.Response.Choices[0].Message.Content != longContent() {
		t.Fatal("original event mutated by ticket build")
	}
}

func longContent() string {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return string(b)
}

// TestDumpOverflowEvent writes a JSON file on demand (dir created).
func TestDumpOverflowEvent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "output-overflow")
	path, err := dumpOverflowEvent(dir, &event.Event{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("overflow file missing: %v", err)
	}
}
