// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
//
// The exact read tool of the recall surface: one event key is read back as the
// original bytes the scoped path serves, one bounded segment at a time.
package recall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// getArgs is the declared argument shape of one exact read: the key to read,
// the resume position and bound of the segment, and whether the parent summary
// comes back beside it.
type getArgs struct {
	// Key is the ticket or key of the event to read.
	Key string `json:"key"`
	// Offset is the byte position the segment starts at.
	Offset int64 `json:"offset"`
	// MaxBytes bounds the bytes this read answers with.
	MaxBytes int64 `json:"max_bytes"`
	// IncludeParent brings the summary of the parent event back beside the text.
	IncludeParent bool `json:"include_parent"`
}

// getAnswer is the answer of one exact read: the segment the scoped path served
// beside the rendering of the event it belongs to, or the named reason no text
// came back.
type getAnswer struct {
	// Tool is the name of the tool that answered.
	Tool string `json:"tool"`
	// Key is the event the call named, in its canonical form.
	Key event.EventID `json:"key"`
	// Found reports that the read served text for the key.
	Found bool `json:"found"`
	// Reason is why no text came back, empty when Found is true.
	Reason string `json:"reason,omitempty"`
	// Type is the frozen event type of the event.
	Type string `json:"type,omitempty"`
	// Summary is the stored summary of the event.
	Summary string `json:"summary,omitempty"`
	// Time is the event time in Unix milliseconds.
	Time int64 `json:"time,omitempty"`
	// Partition is the partition the event is filed under.
	Partition int `json:"partition,omitempty"`
	// Body is the segment of the original this read served.
	Body string `json:"body,omitempty"`
	// Offset is the byte position this segment started at.
	Offset int64 `json:"offset,omitempty"`
	// Next is the byte position the segment after this one resumes from.
	Next int64 `json:"next,omitempty"`
	// EOF reports that this segment reached the end of the original.
	EOF bool `json:"eof,omitempty"`
	// Parent is the summary of the parent event, when the call asked for it.
	Parent *eventItem `json:"parent,omitempty"`
}

// getTool is the model-facing exact read tool.
type getTool struct{ base }

// Declaration reports the exact read tool's declaration.
func (t *getTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name: GetToolName,
		Description: "按事件键取回原文：手里已有事件键（卡片行里的 [evt_…] 票据）时用它，比语义检索精确。" +
			"返回该事件的一段原文、下一段的续读位置与是否读到底；include_parent 为真时同时带回父事件的摘要。" +
			"键不存在或已过保留期时如实标出 missing/expired，不补、不猜。",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"key": {
					Type:        "string",
					Description: "事件键：[evt_KEY|type]、evt_KEY 或裸键三种写法都接受",
				},
				"offset": {
					Type:        "integer",
					Description: "续读起点（字节），缺省从头开始",
				},
				"max_bytes": {
					Type:        "integer",
					Description: "本次返回的字节上限，缺省 8KiB，最大 32KiB",
				},
				"include_parent": {
					Type:        "boolean",
					Description: "是否同时带回父事件的摘要",
				},
			},
			Required: []string{"key"},
		},
	}
}

// Call reads one event back through the scoped original path. A key the read
// face does not hold, an original past its retention window and an event
// outside the granted partitions are answered with their named reason, so the
// caller can tell a miss from a read that never ran.
func (t *getTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	if t.reads.Table == nil || t.reads.Originals == nil {
		return nil, unwired(GetToolName)
	}
	var args getArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("%s: malformed arguments: %w", GetToolName, err)
	}
	key, err := parseKey(args.Key)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", GetToolName, err)
	}
	if args.IncludeParent && t.reads.Trace == nil {
		return nil, unwired(GetToolName)
	}
	answer := getAnswer{Tool: GetToolName, Key: key}
	landed, held := t.reads.Table.Event(key)
	if !held {
		answer.Reason = reasonMissing
		return encode(GetToolName, answer)
	}
	if !t.admits(landed.PartitionID) {
		answer.Reason = reasonOutOfScope
		return encode(GetToolName, answer)
	}
	answer.Type, answer.Summary = landed.Type, landed.Summary
	answer.Time, answer.Partition = landed.Timestamp, landed.PartitionID
	reference, held := t.reads.Table.Reference(key)
	if !held {
		answer.Reason = reasonMissing
		return encode(GetToolName, answer)
	}
	segment, err := t.reads.Originals.ReadSegmentScoped(ctx, key, reference.ReadScope(), ledger.SegmentRequest{
		From:     ledger.SegmentPosition{Offset: args.Offset},
		MaxBytes: clampRead(args.MaxBytes),
	})
	if err != nil {
		answer.Reason = readReason(err)
		if answer.Reason == "" {
			return nil, fmt.Errorf("%s: %w", GetToolName, err)
		}
		return encode(GetToolName, answer)
	}
	answer.Found = true
	answer.Body = string(segment.Body)
	answer.Offset = args.Offset
	answer.Next = segment.Next.Offset
	answer.EOF = segment.EOF
	if args.IncludeParent {
		answer.Parent = t.parentOf(key)
	}
	return encode(GetToolName, answer)
}

// parentOf renders the summary of the parent event of one key, or nothing when
// the chain has no parent there or the parent lies outside the granted
// partitions.
func (t *getTool) parentOf(key event.EventID) *eventItem {
	parent, err := t.reads.Trace.GetParent(key)
	if err != nil || parent == "" {
		return nil
	}
	landed, held := t.reads.Table.Event(parent)
	if !held || !t.admits(landed.PartitionID) {
		return nil
	}
	item := itemOf(landed)
	return &item
}

// readReason names the reason a scoped read served no text, or the empty string
// when the failure is not one of the declared read outcomes.
func readReason(err error) string {
	switch {
	case errors.Is(err, ledger.ErrOriginalMissing):
		return reasonMissing
	case errors.Is(err, ledger.ErrOriginalExpired):
		return reasonExpired
	default:
		return ""
	}
}

// clampRead resolves the byte bound one read runs under: the default when it
// names none, and the ceiling when it names more than one read may serve.
func clampRead(bytes int64) int64 {
	switch {
	case bytes <= 0:
		return defaultReadBytes
	case bytes > maxReadBytes:
		return maxReadBytes
	default:
		return bytes
	}
}
