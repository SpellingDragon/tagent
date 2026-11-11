// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package runners

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/bench"
)

//go:embed capabilities.json
var capabilityMap []byte

// protocolVersion names the line protocol the bench-bridge speaks.
const protocolVersion = "bench-runner-v1"

// hotSample is the timing of one model request after the attempt's first one.
type hotSample struct {
	FirstTokenMS float64 `json:"first_token_ms"`
	CompleteMS   float64 `json:"complete_ms"`
}

// result is the envelope every runner returns: the final response, the
// delivery receipt, the api and token counts, the cold/hot timings and the
// intervention split.
type result struct {
	Protocol         string      `json:"protocol"`
	Runner           string      `json:"runner"`
	TaskID           string      `json:"task_id"`
	Status           string      `json:"status"`
	Output           string      `json:"output"`
	Delivered        bool        `json:"delivered"`
	APICalls         int         `json:"api_calls"`
	PromptTokens     int         `json:"prompt_tokens"`
	CompletionTokens int         `json:"completion_tokens"`
	FirstTokenMS     float64     `json:"first_token_ms"`
	CompleteMS       float64     `json:"complete_ms"`
	Hot              []hotSample `json:"hot"`
	ApprovalCalls    int         `json:"approval_calls"`
	BlockedReports   int         `json:"blocked_reports"`
	Error            string      `json:"error"`
}

// runner executes one task in one prepared workspace and answers the result
// envelope; a non-nil error is an infrastructure failure, not a task failure.
type runner interface {
	Run(ctx context.Context, task bench.Task, workspace string) ([]byte, error)
}

// CapabilityMap returns the registered capability-to-tool mapping, the table a
// runner's advertised tool face is judged against.
func CapabilityMap() []byte {
	return append([]byte(nil), capabilityMap...)
}

// BaselineToolFace returns the tool names the baseline runner exposes.
func BaselineToolFace() []string {
	return []string{"read_file", "write_file", "run_command"}
}

// DSHToolFace returns the DeepSeek Harness tool names the adapter maps onto.
func DSHToolFace() []string {
	return []string{"read", "write", "edit", "bash"}
}

// validateTask rejects a task a runner cannot execute before any model call.
func validateTask(task bench.Task) error {
	if strings.TrimSpace(task.ID) == "" {
		return errors.New("bench runner: task id is empty")
	}
	if len(task.Prompts) == 0 {
		return fmt.Errorf("bench runner: task %s declares no prompts", task.ID)
	}
	for index, prompt := range task.Prompts {
		if strings.TrimSpace(prompt) == "" {
			return fmt.Errorf("bench runner: task %s prompt %d is empty", task.ID, index+1)
		}
	}
	if task.Verifier == nil {
		return fmt.Errorf("bench runner: task %s declares no verifier", task.ID)
	}
	for _, capability := range task.Tools {
		switch capability {
		case bench.ToolFileRead, bench.ToolFileWrite, bench.ToolExec:
		default:
			return fmt.Errorf("bench runner: task %s declares unknown capability %q", task.ID, capability)
		}
	}
	return nil
}

// encodeResult canonicalizes one result envelope.
func encodeResult(value result) ([]byte, error) {
	value.Protocol = protocolVersion
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("bench runner: encode result: %w", err)
	}
	return encoded, nil
}

// decodeResult reads one result envelope.
func decodeResult(raw []byte) (result, error) {
	var value result
	if err := json.Unmarshal(raw, &value); err != nil {
		return result{}, fmt.Errorf("bench runner: decode result: %w", err)
	}
	return value, nil
}
