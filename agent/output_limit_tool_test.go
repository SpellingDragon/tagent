package agent

import (
	"context"
	"encoding/json"
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

	if !strings.Contains(resultStr, "[ERROR: Tool output exceeded 100 characters, truncated.") {
		t.Fatalf("expected truncation error message, got: %s", resultStr[:200])
	}
	if !strings.Contains(resultStr, "Total: 502 characters") {
		t.Fatalf("expected total character count in error message, got: %s", resultStr[:200])
	}
	// Should contain truncated content (first 100 chars of JSON: " + 99 a's)
	if !strings.HasPrefix(resultStr, `"`+strings.Repeat("a", 99)) {
		t.Fatalf("expected truncated content at start")
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

	// Should return original result (under limit)
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
		t.Fatalf("expected string result after truncation, got %T", got)
	}
	if !strings.Contains(resultStr, "[ERROR: Tool output exceeded 50 characters, truncated.") {
		t.Fatalf("expected truncation error message")
	}
}
