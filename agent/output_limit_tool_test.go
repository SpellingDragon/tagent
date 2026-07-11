package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

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
	if !strings.Contains(resultStr, "使用 read_file 工具读取该文件") {
		t.Fatalf("expected read_file hint, got: %s", resultStr[:min(200, len(resultStr))])
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
