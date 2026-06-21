package agent

import (
	"context"
	"encoding/json"
	"fmt"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// OutputLimitTool wraps a CallableTool and truncates output that exceeds
// maxChars. When the serialized result exceeds the limit, the output is
// truncated and an error message is appended so the agent can perceive
// the problem and decide how to proceed.
type OutputLimitTool struct {
	inner    trpctool.Tool
	maxChars int
}

// NewOutputLimitTool wraps a tool with output size interception.
// maxChars is the maximum number of characters allowed in the serialized output.
func NewOutputLimitTool(inner trpctool.Tool, maxChars int) *OutputLimitTool {
	return &OutputLimitTool{
		inner:    inner,
		maxChars: maxChars,
	}
}

// Declaration returns the inner tool's declaration unchanged.
func (t *OutputLimitTool) Declaration() *trpctool.Declaration {
	return t.inner.Declaration()
}

// Call executes the inner tool and intercepts the output if it exceeds maxChars.
func (t *OutputLimitTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	callable, ok := t.inner.(trpctool.CallableTool)
	if !ok {
		// Inner tool is not callable — shouldn't happen in practice.
		return nil, fmt.Errorf("OutputLimitTool: inner tool does not implement CallableTool")
	}

	result, err := callable.Call(ctx, jsonArgs)
	if err != nil {
		return nil, err
	}

	// nil result — no interception needed.
	if result == nil {
		return nil, nil
	}

	// Serialize result to check size.
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		// If we can't marshal, return the original result as-is.
		return result, nil
	}

	// Check if output exceeds the limit.
	if len(data) <= t.maxChars {
		return result, nil
	}

	// Truncate and append error message.
	truncated := string(data[:t.maxChars])
	errorMsg := fmt.Sprintf(
		"\n\n[ERROR: Tool output exceeded %d characters, truncated. Total: %d characters. "+
			"Consider optimizing your command or using more specific queries.]",
		t.maxChars, len(data),
	)

	return truncated + errorMsg, nil
}
