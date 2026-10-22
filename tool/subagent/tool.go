// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// call_subagent is the model-visible face of one sub-agent call: the declaration names
// the two assembly sources, and a call resolves one of them into an assembly, admits the
// assembled call as a task through the delegation face and answers with that admission's
// receipt.
package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolName is the tool name the declaration and the configuration reference share.
const ToolName = "call_subagent"

// deniedMarker heads every refusal the tool answers with.
const deniedMarker = "[call_subagent_denied]"

// ErrDelegationMissing reports a tool constructed with no delegation face.
var ErrDelegationMissing = errors.New("call_subagent: no delegation face is attached")

// ErrBindingsMissing reports a tool constructed with no borrowing face.
var ErrBindingsMissing = errors.New("call_subagent: no borrowing face is attached")

// ErrAddressIncomplete reports a call whose frame names no complete agent session.
var ErrAddressIncomplete = errors.New("call_subagent: the call frame names no complete session")

// ErrArguments reports arguments the declaration does not name.
var ErrArguments = errors.New("call_subagent: the arguments do not match the declaration")

// Delegation is the task face one sub-agent call is admitted through: the admission that
// answers the caller with a receipt, and the terminal report of that same task once the
// delegated turn ends.
//
// 红线清单（2 条）：
// MUST answer the caller with the admission receipt and report the terminal as a notice of the same task
// MUST NOT create a private writable history for the delegated turn, nor replace a notice identity with the fact it points at
type Delegation interface {
	// Delegate admits one sub-agent call as a task and reports its admission receipt.
	Delegate(ctx context.Context, req agent.Delegation) (task.AdmissionReceipt, error)
	// Complete reports the terminal of one admitted call, so its notification carries
	// the outcome to the session that issued the call.
	Complete(ctx context.Context, taskID event.TaskID, status task.Status, fact event.EventID) (task.Notice, error)
}

// ResultDelegation is the optional extension of the delegation face that carries the
// settled result body into the terminal notification: a delegation that implements it
// is asked for the body the delegated turn produced, and one that does not is settled
// exactly as before. The body is what the consumer of a task nobody drives reads.
type ResultDelegation interface {
	// CompleteWithResult reports the terminal of one admitted call beside the result
	// body the delegated turn produced.
	CompleteWithResult(ctx context.Context, taskID event.TaskID, status task.Status, fact event.EventID, text string) (task.Notice, error)
}

// ToolOptions is what the composition root fixes when it registers the tool: the parent
// assembly inheritance reads, the builder the delegated turn runs on, the delegation face
// the call is admitted through and the state owner's borrowing face.
type ToolOptions struct {
	// Parent is the assembly an assembled sub-agent inherits undeclared fields from;
	// nil means no inheritance.
	Parent *Assembly
	// Builder turns one assembly into the live agent the delegated turn runs on.
	Builder Builder
	// Delegation is the task face the call is admitted through.
	Delegation Delegation
	// Bindings is the state owner's borrowing face: the initiating generation a
	// delegated turn inherits is taken here when the call is issued.
	Bindings state.Borrow
}

// Tool is the call_subagent tool: one declaration naming both assembly sources, and a
// call that assembles one sub-agent, admits it as a task and answers with its receipt.
type Tool struct {
	parent  *Assembly
	builder Builder
	faces   delegationFaces
}

// delegationFaces is the pair every delegating tool of this package admits a turn
// through: the task face the call is admitted as and the borrowing face the
// initiating generation is taken from. A target tool and the generic call share
// exactly these two, so one admission path serves both.
type delegationFaces struct {
	delegation Delegation
	bindings   state.Borrow
}

// The tool satisfies the native callable tool port.
var _ trpctool.CallableTool = (*Tool)(nil)

// NewTool returns the call_subagent tool over the faces the caller fixes.
func NewTool(opts ToolOptions) (*Tool, error) {
	if opts.Delegation == nil {
		return nil, ErrDelegationMissing
	}
	if opts.Bindings == nil {
		return nil, ErrBindingsMissing
	}
	return &Tool{
		parent:  opts.Parent,
		builder: opts.Builder,
		faces:   delegationFaces{delegation: opts.Delegation, bindings: opts.Bindings},
	}, nil
}

// Declaration implements the native callable tool port. The description states the two
// mutually exclusive assembly sources, which is the contract the model reads.
func (t *Tool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name: ToolName,
		Description: "delegate one sub-agent call as an asynchronous task and answer with its admission receipt: " +
			"the receipt carries the task identity, and the progress and terminal of that task arrive as its notifications. " +
			"exactly one assembly source is given, and a call naming both is refused: " +
			"agents_file names a markdown agents file whose --- frontmatter declares name/description/model/prompt/skills/tools/mcp and whose body is the prompt, " +
			"while the explicit source declares prompt/model/skills/tools/mcp (and name/description) directly, " +
			"beside the optional sampling of temperature/thinking_enabled/reasoning_effort that its model requests carry; " +
			"a field either source leaves undeclared inherits the caller's own value, and sampling left undeclared is not injected at all. " +
			"a call may declare a structured handoff beside the request: the intent it serves, the acceptance constraints its result is held to, " +
			"the decisions it may not take on its own and the stored facts it starts from, while its grants are the tool, skill and MCP references the assembled sub-agent declares. " +
			"every refusal - both sources at once, an unreadable file, a missing name, a model or a reference list that cannot be resolved, a refused delegation, " +
			"a handoff naming no intent or no acceptance constraint - " +
			"answers " + deniedMarker + " beside no task.",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"agents_file": {
					Type:        "string",
					Description: "the agents file to assemble: a markdown file whose frontmatter declares the sub-agent; mutually exclusive with the explicit parameters",
				},
				"name": {
					Type:        "string",
					Description: "the sub-agent name; required by the explicit source and read from the frontmatter by the agents file source",
				},
				"description": {
					Type:        "string",
					Description: "what the sub-agent does, for the explicit source",
				},
				"prompt": {
					Type:        "string",
					Description: "the instruction of the sub-agent: prompt text, or a file reference opening with ./, ../ or /",
				},
				"model": {
					Type:        "string",
					Description: "the model reference the sub-agent runs on",
				},
				"skills": {
					Type:        "array",
					Items:       &trpctool.Schema{Type: "string"},
					Description: "the skill references the sub-agent may load",
				},
				"tools": {
					Type:        "array",
					Items:       &trpctool.Schema{Type: "string"},
					Description: "the tool references the sub-agent may call",
				},
				"mcp": {
					Type:        "array",
					Items:       &trpctool.Schema{Type: "string"},
					Description: "the MCP server references the sub-agent may use",
				},
				"input": {
					Type:        "string",
					Description: "the request the delegated turn starts from; empty runs the assembled instruction alone",
				},
				"temperature": {
					Type:        "number",
					Description: "the sampling temperature the sub-agent's model requests carry; undeclared keeps the model default",
				},
				"thinking_enabled": {
					Type:        "boolean",
					Description: "whether extended reasoning runs for the sub-agent's model requests; undeclared keeps the model default",
				},
				"reasoning_effort": {
					Type:        "string",
					Description: "the reasoning effort the sub-agent's model requests carry; undeclared keeps the model default",
				},
				"handoff": handoffSchema(),
			},
		},
	}
}

// Call implements the native callable tool port: it resolves exactly one assembly source,
// admits the assembled sub-agent as a task and answers with the receipt of that admission.
// The delegated turn runs beside the admission, and its outcome returns as the terminal
// notification of the task that the session which issued the call receives.
//
//   - a refusal of the arguments, the assembly or the delegation answers as one named
//     result text beside no task, so the turn continues;
//   - err is reserved for the protocol fault of a call carrying no admission identity.
func (t *Tool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return nil, task.ErrCallFrameMissing
	}
	args, unknown, err := parseCallArgs(jsonArgs)
	if err != nil {
		if errors.Is(err, ErrHandoffArguments) {
			return t.denied(err), nil
		}
		return nil, err
	}
	if len(unknown) > 0 {
		return t.denied(fmt.Errorf("%w: %s", ErrArguments, joinNames(unknown))), nil
	}
	assembly, err := Assemble(ctx, t.parent, args.source())
	if err != nil {
		return t.denied(err), nil
	}
	envelope, err := args.envelope(assembly)
	if err != nil {
		return t.denied(err), nil
	}
	receipt, err := t.faces.admit(ctx, t.builder, frame, assembly, args.Input, envelope)
	if err != nil {
		return t.denied(err), nil
	}
	return receipt, nil
}

// admit hands one assembled call to the delegation face: the call is admitted under the
// identity of the decision that issued it, the initiating generation is borrowed for the
// reference the delegated turn inherits, and a fresh admission starts the delegated turn
// beside the receipt it already returned, carrying the envelope the call declared.
func (d delegationFaces) admit(ctx context.Context, builder Builder, frame task.CallFrame, assembly *Assembly, input string, envelope *agent.HandoffEnvelope) (task.AdmissionReceipt, error) {
	if !frame.Address.IsComplete() {
		return task.AdmissionReceipt{}, ErrAddressIncomplete
	}
	handle, err := d.bindings.Borrow(ctx, frame.Address.AgentID)
	if err != nil {
		return task.AdmissionReceipt{}, err
	}
	receipt, err := d.delegation.Delegate(ctx, agent.Delegation{
		InvocationID:  frame.Identity.InvocationID,
		DecisionID:    frame.Identity.DecisionID,
		CallID:        frame.Identity.CallID,
		Target:        frame.Address,
		TriggerSource: event.LineageDelegation,
		Ref:           handle.Ref,
	})
	releaseErr := handle.Release(ctx)
	if err != nil {
		return receipt, err
	}
	if releaseErr != nil {
		return receipt, releaseErr
	}
	if receipt.Kind == task.AdmissionFresh {
		go d.serve(context.WithoutCancel(ctx), builder, receipt.TaskID, assembly, frame.Address, input, envelope)
	}
	return receipt, nil
}

// serve runs the delegated turn of one fresh admission and reports its terminal: a turn
// that ends settles its task and one that fails reports the failure. A settle the ledger
// refuses leaves the task where it is rather than inventing an outcome.
func (d delegationFaces) serve(ctx context.Context, builder Builder, taskID event.TaskID, assembly *Assembly, addr event.Address, input string, envelope *agent.HandoffEnvelope) {
	status := task.StatusSettled
	result, err := builder.Serve(ctx, assembly, Turn{
		UserID:    string(addr.AgentID),
		SessionID: string(addr.SessionID),
		Input:     input,
		Handoff:   envelope,
	})
	if err != nil {
		status = task.StatusFailed
	}
	d.completeDelegation(ctx, taskID, status, result.Content)
}

// completeDelegation reports the terminal of one delegated turn beside the body it
// produced: a delegation that carries result material is asked for it, and one that
// does not is settled exactly as it was before the body travelled.
func (d delegationFaces) completeDelegation(ctx context.Context, taskID event.TaskID, status task.Status, body string) {
	if carrier, ok := d.delegation.(ResultDelegation); ok {
		_, _ = carrier.CompleteWithResult(ctx, taskID, status, "", body)
		return
	}
	_, _ = d.delegation.Complete(ctx, taskID, status, "")
}

// denied renders one refusal as result text: the named case beside the source, field and
// reason it carries, and the statement that no task was admitted.
func (t *Tool) denied(reason error) string {
	return fmt.Sprintf("%s %s; no task was admitted", deniedMarker, named(reason))
}

// named renders one refusal with the kind it wraps, so the text names the case and not
// only the place it was refused at.
func named(reason error) string {
	text := reason.Error()
	kind := errors.Unwrap(reason)
	if kind == nil || strings.Contains(text, kind.Error()) {
		return text
	}
	return text + " (" + kind.Error() + ")"
}

// callArgs is the declared argument shape one call carries.
type callArgs struct {
	// AgentsFile names the agents file to assemble.
	AgentsFile string `json:"agents_file"`
	// Name names the sub-agent of the explicit source.
	Name string `json:"name"`
	// Description describes the sub-agent of the explicit source.
	Description string `json:"description"`
	// Model names the model the sub-agent runs on.
	Model string `json:"model"`
	// Prompt carries the instruction text or its file reference.
	Prompt string `json:"prompt"`
	// Skills lists the skill references of the explicit source.
	Skills []string `json:"skills"`
	// Tools lists the tool references of the explicit source.
	Tools []string `json:"tools"`
	// MCP lists the MCP server references of the explicit source.
	MCP []string `json:"mcp"`
	// Temperature is the sampling temperature the sub-agent's requests carry.
	Temperature *float64 `json:"temperature"`
	// ThinkingEnabled is the thinking switch the sub-agent's requests carry.
	ThinkingEnabled *bool `json:"thinking_enabled"`
	// ReasoningEffort is the reasoning effort the sub-agent's requests carry.
	ReasoningEffort string `json:"reasoning_effort"`
	// Input is the request the delegated turn starts from.
	Input string `json:"input"`
	// Handoff is the structured handoff the call declares beside that request; nil
	// when the call declares none.
	Handoff *handoffArgs `json:"handoff"`
}

// callArgFields is the closed argument key set the declaration names.
var callArgFields = map[string]bool{
	"agents_file":      true,
	"name":             true,
	"description":      true,
	"model":            true,
	"prompt":           true,
	"skills":           true,
	"tools":            true,
	"mcp":              true,
	"temperature":      true,
	"thinking_enabled": true,
	"reasoning_effort": true,
	"input":            true,
	"handoff":          true,
}

// source names the assembly source one call carries: the agents file and the explicit
// parameters are both stated when the call declares both, so the assembly refuses them.
func (a callArgs) source() Source {
	src := Source{AgentsFile: a.AgentsFile}
	src.Explicit = a.explicit()
	return src
}

// explicit reports the parameter set the call declares, or nil when it declares none.
func (a callArgs) explicit() *Definition {
	if a.Name == "" && a.Description == "" && a.Model == "" && a.Prompt == "" &&
		a.Skills == nil && a.Tools == nil && a.MCP == nil &&
		a.Temperature == nil && a.ThinkingEnabled == nil && a.ReasoningEffort == "" {
		return nil
	}
	return &Definition{
		Name:            a.Name,
		Description:     a.Description,
		Model:           a.Model,
		Prompt:          a.Prompt,
		Skills:          a.Skills,
		Tools:           a.Tools,
		MCP:             a.MCP,
		Temperature:     a.Temperature,
		ThinkingEnabled: a.ThinkingEnabled,
		ReasoningEffort: a.ReasoningEffort,
	}
}

// parseCallArgs reads one call's declared arguments beside the keys it does not name.
func parseCallArgs(jsonArgs []byte) (callArgs, []string, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(jsonArgs, &probe); err != nil {
		return callArgs{}, nil, fmt.Errorf("call_subagent: malformed arguments: %w", err)
	}
	unknown := make([]string, 0, len(probe))
	for key := range probe {
		if !callArgFields[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	if raw, ok := probe["handoff"]; ok {
		if err := checkHandoffKeys(raw); err != nil {
			return callArgs{}, nil, err
		}
	}
	var args callArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return callArgs{}, nil, fmt.Errorf("call_subagent: malformed arguments: %w", err)
	}
	return args, unknown, nil
}

// joinNames renders one name list for a refusal, in its declared order.
func joinNames(names []string) string {
	out := ""
	for i, name := range names {
		if i > 0 {
			out += ", "
		}
		out += "'" + name + "'"
	}
	return out
}
