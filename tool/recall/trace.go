// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
//
// The causal chain tool of the recall surface: one key is walked back along the
// parent links of the relation ledger, and the part that can be read is
// answered beside the named reason the walk stopped there.
package recall

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// reasonMissingAncestor, reasonRelationError, reasonCycle and reasonLimit name
// why a causal walk stopped before it reached a root: the parent link could not
// be read, the ancestor it names is not readable, the link loops back to an
// event the walk already read, or the step bound ran out first.
const (
	reasonMissingAncestor = "missing_ancestor"
	reasonRelationError   = "relation_error"
	reasonCycle           = "cycle"
	reasonLimit           = "limit"
)

// traceArgs is the declared argument shape of one causal walk: the key it
// starts at and the steps it may take.
type traceArgs struct {
	// Key is the ticket or key of the event the walk starts at.
	Key string `json:"key"`
	// MaxSteps bounds how many events the walk reads.
	MaxSteps int `json:"max_steps"`
}

// traceAnswer is the answer of one causal walk: the events it read from the
// starting key back along the parent links, and whether it reached a root.
type traceAnswer struct {
	// Tool is the name of the tool that answered.
	Tool string `json:"tool"`
	// Partitions is the granted partition set the walk ran in.
	Partitions []string `json:"partitions"`
	// Count is how many events the walk read.
	Count int `json:"count"`
	// Complete reports that the walk reached an event with no parent link.
	Complete bool `json:"complete"`
	// Capped reports that the step bound ran out before the walk reached a root.
	Capped bool `json:"capped"`
	// Reason is why the walk stopped, empty when it reached a root.
	Reason string `json:"reason,omitempty"`
	// Items is the chain the walk read, the starting key first.
	Items []eventItem `json:"items"`
}

// traceTool is the model-facing causal chain tool.
type traceTool struct{ base }

// Declaration reports the causal chain tool's declaration.
func (t *traceTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name: TraceToolName,
		Description: "从给定事件键沿父链回溯：要因果与关联（同一回合的上下游、义务的归属方与推进方）时用它。" +
			"结果是从该键往回的链，最近的在前；步数有上界，用尽时 capped 为真并给出 reason。" +
			"断链即止：已读到的那一段照常返回并标出停点，不跨断点猜父链。",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"key": {
					Type:        "string",
					Description: "起点事件键：[evt_KEY|type]、evt_KEY 或裸键三种写法都接受",
				},
				"max_steps": {
					Type:        "integer",
					Description: "最多读多少条，缺省 20，最大 50",
				},
			},
			Required: []string{"key"},
		},
	}
}

// Call walks the parent chain back from one key. A starting key the read face
// does not hold is refused by name, because no chain starts there; a link that
// breaks further back stops the walk and is answered with the part already read
// beside the reason it stopped.
func (t *traceTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	if t.reads.Table == nil || t.reads.Trace == nil {
		return nil, unwired(TraceToolName)
	}
	var args traceArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("%s: malformed arguments: %w", TraceToolName, err)
	}
	start, err := parseKey(args.Key)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", TraceToolName, err)
	}
	steps := clampSteps(args.MaxSteps)
	answer := traceAnswer{Tool: TraceToolName, Partitions: t.names, Items: make([]eventItem, 0, steps)}
	read := make(map[event.EventID]bool, steps)
	current := start
	for {
		if read[current] {
			answer.Reason = reasonCycle
			break
		}
		read[current] = true
		landed, held := t.reads.Table.Event(current)
		if !held || !t.admits(landed.PartitionID) {
			if current == start {
				return nil, fmt.Errorf("%s: %w: %s", TraceToolName, ErrRecallMissing, current)
			}
			answer.Reason = missingReason(held)
			break
		}
		answer.Items = append(answer.Items, itemOf(landed))
		parent, err := t.reads.Trace.GetParent(current)
		if err != nil {
			answer.Reason = reasonRelationError
			break
		}
		if parent == "" {
			answer.Complete = true
			break
		}
		if len(answer.Items) >= steps {
			answer.Capped = true
			answer.Reason = reasonLimit
			break
		}
		current = parent
	}
	answer.Count = len(answer.Items)
	return encode(TraceToolName, answer)
}

// missingReason names why one link of the walk could not be read: the table
// holds no such event, or it holds one outside the granted partitions.
func missingReason(held bool) string {
	if held {
		return reasonOutOfScope
	}
	return reasonMissingAncestor
}

// clampSteps resolves the step bound one walk runs under: the default when it
// names none, and the ceiling when it names more steps than a walk may take.
func clampSteps(steps int) int {
	switch {
	case steps <= 0:
		return defaultTraceSteps
	case steps > maxTraceSteps:
		return maxTraceSteps
	default:
		return steps
	}
}
