package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"trpc.group/trpc-go/trpc-agent-go/log"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// OutputLimitTool wraps a CallableTool and handles output that exceeds
// maxChars. When the serialized result exceeds the limit, the full output
// is saved to a file and a summary with the file path is returned instead.
//
// This prevents invalid JSON from mechanical truncation and avoids
// token explosion from large tool results in the LLM context.
type OutputLimitTool struct {
	inner       trpctool.Tool
	maxChars    int
	workspace   string
	fileCounter atomic.Int64
}

// NewOutputLimitTool wraps a tool with output size interception.
// maxChars is the maximum number of characters allowed in the serialized output.
func NewOutputLimitTool(inner trpctool.Tool, maxChars int) *OutputLimitTool {
	return &OutputLimitTool{
		inner:    inner,
		maxChars: maxChars,
	}
}

// SetWorkspace sets the directory for saving oversized outputs.
func (t *OutputLimitTool) SetWorkspace(dir string) {
	t.workspace = dir
}

// Declaration returns the inner tool's declaration unchanged.
func (t *OutputLimitTool) Declaration() *trpctool.Declaration {
	return t.inner.Declaration()
}

// Call executes the inner tool and intercepts the output if it exceeds maxChars.
func (t *OutputLimitTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	callable, ok := t.inner.(trpctool.CallableTool)
	if !ok {
		return nil, fmt.Errorf("OutputLimitTool: inner tool does not implement CallableTool")
	}

	result, err := callable.Call(ctx, jsonArgs)
	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, nil
	}

	// Serialize result to check size.
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return result, nil
	}

	if len(data) <= t.maxChars {
		return result, nil
	}

	// Output exceeds limit: save full output to file, return summary.
	outputFile := t.saveToFile(data)
	previewLen := 500
	if len(data) < previewLen {
		previewLen = len(data)
	}

	log.Infof("[OutputLimit] output %d chars > %d limit, saved to %s",
		len(data), t.maxChars, outputFile)

	return fmt.Sprintf("[output_too_large] 工具输出 %d 字符超过上限 %d。\n"+
		"完整内容已保存到: %s\n"+
		"前 %d 字符预览:\n%s\n\n"+
		"使用 read_file 工具读取该文件获取完整内容。",
		len(data), t.maxChars, outputFile, previewLen, string(data[:previewLen])), nil
}

// saveToFile writes data to a file in the workspace or temp directory.
func (t *OutputLimitTool) saveToFile(data []byte) string {
	counter := t.fileCounter.Add(1)
	filename := fmt.Sprintf("tool_output_%d.json", counter)

	dir := t.workspace
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		dir = os.TempDir()
	}

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Warnf("[OutputLimit] failed to save output to %s: %v", path, err)
		return path
	}
	return path
}
