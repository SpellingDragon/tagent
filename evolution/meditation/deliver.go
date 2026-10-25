package meditation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// DeliverSummaryToolName is the model-visible name of the cross-session write
// face a reflection turn may call: one summary, delivered by the model's own
// decision, never by the reflection mechanism in silence.
const DeliverSummaryToolName = "deliver_summary"

// DeliverSummaryContentMax is the byte ceiling of one summary body.
const DeliverSummaryContentMax = 8 * 1024

// DeliverSummarySummaryMax is the rune ceiling of the stored event summary.
const DeliverSummarySummaryMax = 120

// EventSink is the cross-session write face of the runtime: one event committed
// into the fact chain of the address it names, under a fresh identity of its own.
//
// It is the only way a reflection summary reaches another session: the tool
// decides to write, the sink files the fact, and the target session's own next
// assembly reads it like any other external input.
// 红线清单（2 条）：
// MUST commit the event into the chain of the address it names.
// MUST NOT write into the caller's chain or answer an identity the commit did not record.
type EventSink interface {
	// CommitToSession commits evt into the fact chain of addr.
	CommitToSession(ctx context.Context, addr event.Address, evt event.Event) (event.EventID, error)
}

// SessionQuery is the live-binding face of the runtime: the sessions currently
// bound to one agent. A summary is delivered to the sessions that exist, and an
// agent with no bound session is a named refusal, never a silent skip.
//
// 红线清单（2 条）：
// MUST answer the complete addresses of the sessions actually bound to the agent.
// MUST NOT invent a session or report an address the run has not established.
type SessionQuery interface {
	// ActiveSessions returns the complete addresses of the sessions bound to
	// agentID, in a deterministic order.
	ActiveSessions(agentID event.AgentID) []event.Address
}

// DeliverSummaryTool is the meditation session's cross-session write tool: it
// delivers one summary to every active session of the declared deliver_to
// agents.
//
// Every refusal is answered as result text and the turn continues; an error is
// reserved for an argument shape that is not JSON at all.
type DeliverSummaryTool struct {
	targets  []string
	sink     EventSink
	sessions SessionQuery

	// mu guards receipts, the receipts one action identity already answered.
	// A replay of the same identity returns that receipt and writes nothing
	// again; the dedup reads the identity the call frame carries, never the
	// content or a time window.
	mu       sync.Mutex
	receipts map[task.DedupKey]string
}

var _ trpctool.CallableTool = (*DeliverSummaryTool)(nil)

// deliverSummaryArgs is the argument shape the model sees: the body, and
// nothing else.
type deliverSummaryArgs struct {
	Content string `json:"content"`
}

// NewDeliverSummaryTool assembles the cross-session write tool over the declared
// target agents, the write face and the live-binding face.
func NewDeliverSummaryTool(targets []string, sink EventSink, sessions SessionQuery) *DeliverSummaryTool {
	return &DeliverSummaryTool{
		targets:  append([]string(nil), targets...),
		sink:     sink,
		sessions: sessions,
		receipts: make(map[task.DedupKey]string),
	}
}

// Declaration reports the tool declaration: one content argument, delivered to
// the sessions the declaration's deliver_to targets bind.
func (t *DeliverSummaryTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name: DeliverSummaryToolName,
		Description: "Deliver one reflection summary into every active session bound to the declared target agents. " +
			"Use it when the reflection produced something the target sessions should know; the delivery is your decision, not the mechanism's. " +
			"A refusal returns a [deliver_summary_denied] result text and the turn continues.",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"content": {Type: "string", Description: "The summary to deliver, UTF-8, at most 8KiB"},
			},
			Required: []string{"content"},
		},
	}
}

// Call delivers one summary to every active session of the declared targets.
// Refusals — malformed fields, an empty or oversized body, an unwired write
// face and a target with no bound session — are answered as result text, so the
// reflection turn continues and the model sees why nothing was written.
func (t *DeliverSummaryTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(jsonArgs, &probe); err != nil {
		return nil, fmt.Errorf("deliver_summary: malformed arguments: %w", err)
	}
	if unknown := deliverUnknownFields(probe); len(unknown) > 0 {
		return denied("the arguments name fields outside the declaration " + strings.Join(unknown, ", ") +
			"; deliver_summary accepts content only, and the targets come from deliver_to"), nil
	}
	var args deliverSummaryArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("deliver_summary: malformed arguments: %w", err)
	}
	if args.Content == "" {
		return denied("empty content"), nil
	}
	if len(args.Content) > DeliverSummaryContentMax {
		return denied(fmt.Sprintf("content exceeds the 8KiB ceiling (%d bytes)", len(args.Content))), nil
	}
	if t.sink == nil || t.sessions == nil {
		return denied("the cross-session write face is not wired"), nil
	}

	key, replay := deliverActionKey(ctx)
	if replay {
		if receipt, seen := t.receiptOf(key); seen {
			return receipt, nil
		}
	}
	receipt := t.deliver(ctx, args.Content)
	if replay {
		t.recordReceipt(key, receipt)
	}
	return receipt, nil
}

// deliver writes one summary to every live session of the declared targets and
// reports the coverage per target: a success names every target beside its
// committed session count, and a target whose loop is not running is refused by
// name instead of being folded into a bare success.
func (t *DeliverSummaryTool) deliver(ctx context.Context, content string) string {
	groups := t.targetSessions()
	total := 0
	for _, group := range groups {
		total += len(group.addresses)
	}
	if total == 0 {
		return denied("no bound target sessions")
	}
	now := time.Now()
	summary := deliverSummaryLine(content)
	failed := 0
	var firstErr error
	covered := make([]string, 0, len(groups))
	var uncovered []string
	for _, group := range groups {
		if len(group.addresses) == 0 {
			uncovered = append(uncovered, group.target)
			continue
		}
		committed := 0
		for _, addr := range group.addresses {
			evt := event.Event{
				Type:      event.TypeExternalInput,
				Timestamp: now,
				Meta: map[string]string{
					event.MetaKeyTriggerSource: LineageReflection,
					event.MetaKeyEventSummary:  summary,
				},
				Message: model.Message{Role: model.RoleUser, Content: content},
				Owner:   addr,
				Source:  addr,
				Target:  addr,
			}
			if _, err := t.sink.CommitToSession(ctx, addr, evt); err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			committed++
		}
		covered = append(covered, fmt.Sprintf("%s: %d/%d sessions committed", group.target, committed, len(group.addresses)))
	}
	if failed > 0 {
		reason := fmt.Sprintf("%d of %d deliveries failed: %v", failed, total, firstErr)
		if len(uncovered) > 0 {
			reason += "; " + uncoveredRefusal(uncovered)
		}
		if len(covered) > 0 {
			reason += "; covered " + strings.Join(covered, ", ")
		}
		return denied(reason)
	}
	if len(uncovered) > 0 {
		reason := uncoveredRefusal(uncovered)
		if len(covered) > 0 {
			reason += "; covered " + strings.Join(covered, ", ")
		}
		return denied(reason)
	}
	return fmt.Sprintf("[deliver_summary_ok] delivered to %d session(s) across %d target(s): %s",
		total, len(covered), strings.Join(covered, ", "))
}

// targetSessions resolves the declared targets into the live sessions they
// bind, deduplicated inside one target and kept in target-declaration order.
func (t *DeliverSummaryTool) targetSessions() []targetSessions {
	out := make([]targetSessions, 0, len(t.targets))
	for _, target := range t.targets {
		seen := make(map[event.Address]struct{})
		addresses := make([]event.Address, 0)
		for _, addr := range t.sessions.ActiveSessions(event.AgentID(target)) {
			if !addr.IsComplete() {
				continue
			}
			if _, ok := seen[addr]; ok {
				continue
			}
			seen[addr] = struct{}{}
			addresses = append(addresses, addr)
		}
		out = append(out, targetSessions{target: target, addresses: addresses})
	}
	return out
}

// targetSessions is one declared target beside the live sessions it binds.
type targetSessions struct {
	target    string
	addresses []event.Address
}

// uncoveredRefusal names the targets whose loop is not running, so the caller
// reads which declared target the delivery could not reach.
func uncoveredRefusal(targets []string) string {
	quoted := make([]string, 0, len(targets))
	for _, target := range targets {
		quoted = append(quoted, `"`+target+`"`)
	}
	return "no live session for target " + strings.Join(quoted, ", ")
}

// deliverActionKey reports the action identity the call frame carries, and
// whether one is set. A call driven beside no frame has no action identity to
// deduplicate against.
func deliverActionKey(ctx context.Context) (task.DedupKey, bool) {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return "", false
	}
	return frame.Identity.DedupKey(), true
}

// receiptOf reports the receipt one action identity already answered.
func (t *DeliverSummaryTool) receiptOf(key task.DedupKey) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	receipt, ok := t.receipts[key]
	return receipt, ok
}

// recordReceipt pins the receipt of one action identity, so a replay answers
// the same text without writing again.
func (t *DeliverSummaryTool) recordReceipt(key task.DedupKey, receipt string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.receipts == nil {
		t.receipts = make(map[task.DedupKey]string)
	}
	t.receipts[key] = receipt
}

// denied renders one refusal as result text.
func denied(reason string) string {
	return "[deliver_summary_denied] " + reason
}

// deliverUnknownFields names the argument fields outside the declaration,
// sorted so the text is reproducible.
func deliverUnknownFields(probe map[string]json.RawMessage) []string {
	var out []string
	for key := range probe {
		if key != "content" {
			out = append(out, "'"+key+"'")
		}
	}
	sort.Strings(out)
	return out
}

// deliverSummaryLine renders the stored event summary: the first non-empty line
// of the body, bounded by DeliverSummarySummaryMax runes.
func deliverSummaryLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > DeliverSummarySummaryMax {
			line = string(runes[:DeliverSummarySummaryMax])
		}
		return line
	}
	return ""
}
