// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// The host-owned tools and ports: the outbound send tool the model calls, the
// scheduled reflection plan and the port that commits its body to a session, and
// the request shape an inbound channel posts to the task endpoint. Each one is a
// face the host contributes to the assembly, so the host builds it here rather
// than leaving the wiring to every deployment.
package tagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// hostSendToolID is the declaration-side name of the outbound message tool.
const hostSendToolID = "send"

// hostSendContentMax is the byte ceiling of one body the send tool delivers.
const hostSendContentMax = 8 * 1024

// hostSendLineage is the delivery lineage of a call that carries no governance
// pass: the body originated in the send tool itself, so the line names the tool
// rather than borrowing the user's line.
const hostSendLineage = "send"

// hostSendTool is the model-visible outbound face: one content argument handed
// to the host's one delivery outlet, which rules it before any body leaves.
type hostSendTool struct {
	sender  MessageSender
	deliver func(context.Context, plugin.Delivery) (deliveryOutcome, string)
}

var _ trpctool.CallableTool = (*hostSendTool)(nil)

// hostSendArgs is the argument shape the model sees: the body, and nothing
// else.
type hostSendArgs struct {
	Content string `json:"content"`
}

// Declaration implements tool.CallableTool.
func (t *hostSendTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        hostSendToolID,
		Description: "Deliver one body to the user address of this call. The target comes from the call's session address; it is not an argument. A refusal returns a [send_denied] result text, and a body held for review returns a [send_held] result text; the turn continues either way.",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"content": {Type: "string", Description: "The body to deliver, UTF-8, at most 8KiB"},
			},
			Required: []string{"content"},
		},
	}
}

// Call implements tool.CallableTool: every refusal and delivery failure is
// answered as result text, and an error is reserved for a malformed argument
// shape. The body is handed to the host's one delivery outlet, so the same
// ruler rules a cross-agent send and a turn body, and a hold is answered as a
// hold: the receipt must never report a held body as delivered. A delivered
// body is answered with the bare ok: the receipt is the credential that the
// delivery happened, not a report to repeat, so it names no target session and
// carries no wording the model could speak to the user.
func (t *hostSendTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(jsonArgs, &probe); err != nil {
		return nil, fmt.Errorf("send: malformed arguments: %w", err)
	}
	if unknown := hostUnknownFields(probe); len(unknown) > 0 {
		return t.denied("the arguments name fields outside the declaration " + strings.Join(unknown, ", ") +
			"; send accepts content only, and the target comes from the host"), nil
	}
	var args hostSendArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("send: malformed arguments: %w", err)
	}
	if args.Content == "" {
		return t.denied("content is empty"), nil
	}
	if len(args.Content) > hostSendContentMax {
		return t.denied(fmt.Sprintf("content exceeds the 8KiB ceiling (%d bytes)", len(args.Content))), nil
	}
	if t.sender == nil {
		return t.denied("the host carries no outbound message sender"), nil
	}
	addr, ok := hostCallAddress(ctx)
	if !ok {
		return t.denied("this call carries no explicit session address"), nil
	}
	if t.deliver == nil {
		return t.denied("the host carries no delivery outlet"), nil
	}
	outcome, cause := t.deliver(ctx, plugin.Delivery{
		Content: args.Content,
		Lineage: hostSendLineageOf(ctx),
		Target:  addr,
	})
	switch outcome {
	case deliverySent:
		return "ok", nil
	case deliveryHeld:
		return t.held(cause), nil
	default:
		return t.denied(cause), nil
	}
}

// denied renders one refusal as result text.
func (t *hostSendTool) denied(reason string) string {
	return "[send_denied] " + reason
}

// held renders one hold as result text: the body is waiting for review, not
// delivered, and the text must not read as a delivery receipt.
func (t *hostSendTool) held(reason string) string {
	return "[send_held] " + reason
}

// hostSendLineageOf reports the lineage a send call hands to the delivery
// ruler: the governance pass of the running call when the dispatch stated one,
// the trigger-source stamp of the running turn otherwise, and the send tool's
// own line when the call runs beside neither.
func hostSendLineageOf(ctx context.Context) string {
	if pass, ok := plugin.CallPassFrom(ctx); ok {
		if source := strings.TrimSpace(pass.TriggerSource); source != "" {
			return source
		}
	}
	if lineage, ok := deliveryLineageFrom(ctx); ok {
		return lineage
	}
	return hostSendLineage
}

// hostCallAddress reports the explicit invocation address the call frame
// carries.
func hostCallAddress(ctx context.Context) (event.Address, bool) {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok || !frame.Address.IsComplete() {
		return event.Address{}, false
	}
	return frame.Address, true
}

// hostUnknownFields names the argument fields outside the declaration, sorted
// so the text is reproducible.
func hostUnknownFields(probe map[string]json.RawMessage) []string {
	var out []string
	for key := range probe {
		if key != "content" {
			out = append(out, "'"+key+"'")
		}
	}
	sort.Strings(out)
	return out
}

// hostMeditationPlan is the one reflection section a document enables: the
// observing agent, the loop session, the cadence and floor, the static prompt,
// the observed and read namespaces, the declared delivery targets and the
// anchor record the watermark persists through.
type hostMeditationPlan struct {
	agent    string
	session  string
	interval time.Duration
	minGap   time.Duration
	prompt   string
	observed []string
	reads    []string
	targets  []string
	anchors  string
}

// hostMeditationDefaultInterval is the tick cadence of a declaration that names
// none.
const hostMeditationDefaultInterval = 30 * time.Minute

// resolveHostMeditation resolves the one meditation section a document enables:
// the entry agent first and the remaining agents in declaration order, so one
// document resolves to one plan across restarts. A document enabling none
// reports a nil plan, and every inconsistent section is refused by name.
func resolveHostMeditation(doc document.Document, promptDir, storageRoot string) (*hostMeditationPlan, error) {
	var plan *hostMeditationPlan
	declaredBy := ""
	for _, id := range doc.Order() {
		spec, ok := doc.Agents[id]
		if !ok || !spec.Meditation.Enabled {
			continue
		}
		if plan != nil {
			return nil, fmt.Errorf("tagent: agents %q and %q both enable meditation, and one host drives one reflection loop",
				declaredBy, id)
		}
		entry := spec.Meditation
		interval := entry.Interval
		if interval < 0 {
			return nil, fmt.Errorf("tagent: agent %q declares a negative meditation interval %s", id, interval)
		}
		if interval == 0 {
			interval = hostMeditationDefaultInterval
		}
		if entry.MinGap < 0 {
			return nil, fmt.Errorf("tagent: agent %q declares a negative meditation min_gap %s", id, entry.MinGap)
		}
		prompt, err := hostMeditationPrompt(promptDir, id, entry.PromptFile)
		if err != nil {
			return nil, err
		}
		reads := memory.PartitionsOf(id, spec.Memory.ReadNamespaces)
		scope, err := meditation.ResolveObservationScope(id, entry.ObservedNamespaces, reads)
		if err != nil {
			return nil, fmt.Errorf("tagent: %w", err)
		}
		targets, err := hostMeditationTargets(doc, id, entry.DeliverTo)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(storageRoot) == "" {
			return nil, fmt.Errorf("tagent: agent %q enables meditation beside no storage tier, so its anchor record has nowhere to persist", id)
		}
		plan = &hostMeditationPlan{
			agent:    id,
			session:  hostSessionPrefix(doc.Entry) + "meditation",
			interval: interval,
			minGap:   entry.MinGap,
			prompt:   prompt,
			observed: scope.Names,
			reads:    reads,
			targets:  targets,
			anchors:  filepath.Join(storageRoot, "meditation", id, "anchors.json"),
		}
		declaredBy = id
	}
	return plan, nil
}

// hostMeditationPrompt reads the static prompt one enabled meditation carries.
// A relative path resolves beside the declared prompt directory, and a file the
// checkout does not carry refuses the assembly.
func hostMeditationPrompt(promptDir, id, name string) (string, error) {
	declared := strings.TrimSpace(name)
	if declared == "" {
		return "", fmt.Errorf("tagent: agent %q enables meditation but names no prompt_file", id)
	}
	path := declared
	if !filepath.IsAbs(path) {
		path = filepath.Join(promptDir, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("tagent: agent %q cannot read the meditation prompt %s: %w", id, path, err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return "", fmt.Errorf("tagent: agent %q declares an empty meditation prompt %s", id, path)
	}
	return string(body), nil
}

// hostMeditationTargets resolves the delivery white list of one enabled
// meditation: every name must be a declared agent, deduplicated in declaration
// order, and a section that names no target is refused by name.
func hostMeditationTargets(doc document.Document, id string, declared []string) ([]string, error) {
	targets := make([]string, 0, len(declared))
	seen := make(map[string]bool, len(declared))
	for _, name := range declared {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		if _, ok := doc.Agents[trimmed]; !ok {
			return nil, fmt.Errorf("tagent: agent %q delivers its meditation to the undeclared agent %q", id, trimmed)
		}
		seen[trimmed] = true
		targets = append(targets, trimmed)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("tagent: agent %q enables meditation but names no deliver_to target", id)
	}
	return targets, nil
}

// hostSessionPrefix reports the session identity prefix of one entry agent: the
// label the deployment names, and the host default beside none.
func hostSessionPrefix(entry string) string {
	label := strings.TrimSpace(os.Getenv("TAGENT_SESSION_ID"))
	if label == "" {
		label = "tagent-session"
	}
	return label + ":" + strings.TrimSpace(entry) + ":"
}

// hostMeditationPort is the host's binding of the two faces a reflection turn
// writes through: the cross-session sink a delivered summary commits into and
// the live-session query it resolves its declared targets against.
//
// The port is built before the run and bound after Assemble, so the tool the
// meditation session carries reads the live scene the run holds instead of a
// copy made at construction. The meditation loop session is never a delivery
// target: it is the observing session itself, not a binding of the agent.
type hostMeditationPort struct {
	plan *hostMeditationPlan
	run  *Run
}

var (
	_ meditation.EventSink    = (*hostMeditationPort)(nil)
	_ meditation.SessionQuery = (*hostMeditationPort)(nil)
)

// Bind attaches the assembled run the port writes through.
func (p *hostMeditationPort) Bind(run *Run) { p.run = run }

// CommitToSession commits one delivered event into the chain of its address.
func (p *hostMeditationPort) CommitToSession(ctx context.Context, addr event.Address, evt event.Event) (event.EventID, error) {
	if p.run == nil {
		return "", errors.New("tagent: the meditation port is bound to no run")
	}
	return p.run.CommitToSession(ctx, addr, evt)
}

// ActiveSessions reports the run's live bindings of one agent, without the
// observing session of the reflection itself.
func (p *hostMeditationPort) ActiveSessions(agentID event.AgentID) []event.Address {
	if p.run == nil {
		return nil
	}
	sessions := p.run.ActiveSessions(agentID)
	out := make([]event.Address, 0, len(sessions))
	for _, addr := range sessions {
		if p.plan != nil && addr.AgentID == event.AgentID(p.plan.agent) && addr.SessionID == event.SessionID(p.plan.session) {
			continue
		}
		out = append(out, addr)
	}
	return out
}

// hostInjection is the body an inbound channel posts to the task endpoint: the
// messages the turn reads and the lineage the channel declares for it. A
// non-empty Session spells an inject-only session of its own instead of
// reusing the chat session the host admitted last.
type hostInjection struct {
	Messages      []hostInjectionMessage `json:"messages"`
	TriggerSource string                 `json:"trigger_source"`
	Session       string                 `json:"session"`
}

// hostInjectionMessage is one message of an injection: the role it speaks for
// and its content.
type hostInjectionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// userText reports the user content the injection carries: the last non-empty
// user message is the one a turn reads.
func (in hostInjection) userText() string {
	for i := len(in.Messages) - 1; i >= 0; i-- {
		msg := in.Messages[i]
		if msg.Role != string(model.RoleUser) {
			continue
		}
		if text := strings.TrimSpace(msg.Content); text != "" {
			return msg.Content
		}
	}
	return ""
}
