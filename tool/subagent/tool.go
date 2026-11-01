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

// ErrPipelineMissing reports a delegated round asked for from a delegation face that
// carries no resident-instance face: nothing would run the round the call admitted.
var ErrPipelineMissing = errors.New("call_subagent: the delegation face carries no resident instance")

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

// delegatedPipeline is the resident-instance face a delegation ledger carries when the
// turns it admits run on the instances of their own execution sites: one round is handed
// to the site's resident instance and answered by the segment that consumed its input.
//
// 红线清单（2 条）：
// MUST run the round on the resident instance of the execution site the call names
// MUST NOT admit a second task of its own, nor settle the task this tool admitted
type delegatedPipeline interface {
	// Submit hands one round to the resident instance of its execution site and
	// reports the terminal it ended at beside the body it produced.
	Submit(ctx context.Context, in agent.DelegatedInput) (agent.DelegatedAnswer, error)
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
	// Continuations is the round-typed continuation point the delegated turns of
	// this tool park in, so a task re-entered after it settled is served its next
	// round on the same assembly. Nil keeps the one-round contract: nothing is
	// registered and no second round is served.
	Continuations *Continuations
}

// Tool is the call_subagent tool: one declaration naming both assembly sources, and a
// call that assembles one sub-agent, admits it as a task and answers with its receipt.
type Tool struct {
	parent  *Assembly
	builder Builder
	faces   delegationFaces
}

// delegationFaces is what every delegating tool of this package admits a turn through:
// the task face the call is admitted as, the borrowing face the initiating generation is
// taken from, and the continuation the served round parks in. A target tool and the
// generic call share exactly these, so one admission path serves both.
type delegationFaces struct {
	delegation    Delegation
	bindings      state.Borrow
	continuations *Continuations
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
		faces: delegationFaces{
			delegation:    opts.Delegation,
			bindings:      opts.Bindings,
			continuations: opts.Continuations,
		},
	}, nil
}

// Declaration implements the native callable tool port. The description states the two
// mutually exclusive assembly sources, which is the contract the model reads.
func (t *Tool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name: ToolName,
		Description: "delegate one sub-agent call as an asynchronous task and answer with its admission receipt: " +
			"the receipt is a bare ok; the terminal of that task arrives as a notification in a follow-up turn of this session. " +
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

// subAddressOf reports the execution site one assembled call runs in: the address of the
// assembled sub-agent inside the session of the caller, keyed by the native call identity
// that issued it. The site is minted for the delegated turn itself, so the turn runs in a
// place of its own rather than in the caller's site, while the notification of its task
// returns to the caller that issued the call.
func subAddressOf(caller event.Address, assembly *Assembly, callID string) event.Address {
	agentID := event.AgentID(assembly.Name)
	if agentID == "" {
		agentID = caller.AgentID
	}
	return event.Address{
		AgentID:   agentID,
		SessionID: event.SessionID(fmt.Sprintf("%s#sub:%s", caller.SessionID, callID)),
	}
}

// admit hands one assembled call to the delegation face: the call is admitted under the
// identity of the decision that issued it, at the execution site minted for the delegated
// turn beside the caller it reports back to, the initiating generation is borrowed for the
// reference the delegated turn inherits, and a fresh admission starts the delegated turn
// beside the receipt it already returned, carrying the envelope the call declared.
func (d delegationFaces) admit(ctx context.Context, builder Builder, frame task.CallFrame, assembly *Assembly, input string, envelope *agent.HandoffEnvelope) (task.AdmissionReceipt, error) {
	if !frame.Address.IsComplete() {
		return task.AdmissionReceipt{}, ErrAddressIncomplete
	}
	site := subAddressOf(frame.Address, assembly, frame.Identity.CallID)
	if !site.IsComplete() {
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
		Sites:         agent.DelegationSites{Target: site, Issuer: frame.Address},
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
		go d.serve(context.WithoutCancel(ctx), builder, receipt.TaskID, assembly, site, input, envelope)
	}
	return receipt, nil
}

// serve runs the delegated turn of one fresh admission at its own execution site and
// reports its terminal: a turn that ends settles its task and one that fails reports the
// failure. A settle the ledger refuses leaves the task where it is rather than inventing
// an outcome.
//
// The round it serves is also the continuation a re-entry hands input to: the served task
// is registered before the round runs and marked idle once it settled, so a re-entry
// arriving inside the round is refused by name and one arriving after it serves the next
// round of the same task. A tool carrying no continuation registers nothing.
func (d delegationFaces) serve(ctx context.Context, builder Builder, taskID event.TaskID, assembly *Assembly, site event.Address, input string, envelope *agent.HandoffEnvelope) {
	if d.continuations != nil {
		d.continuations.register(taskID, d, builder, assembly, site)
	}
	status, body := d.runRound(ctx, builder, assembly, site, input, envelope)
	_ = d.completeDelegation(ctx, taskID, status, body)
	if d.continuations != nil {
		d.continuations.idle(taskID)
	}
}

// runRound serves one round of one delegated task at the execution site the call minted
// for it and reports the terminal it ended at beside the body it produced: the round is
// handed to the resident instance of that site on the binding the assembly resolves, so
// the turn runs on the site's own loop and never as a drive of this goroutine.
//
// A round that cannot run reports the cause it could not run for as its body, so the
// settle notice of the task names what refused it instead of carrying an empty failure a
// reader would have to attribute on its own.
func (d delegationFaces) runRound(ctx context.Context, builder Builder, assembly *Assembly, site event.Address, input string, envelope *agent.HandoffEnvelope) (task.Status, string) {
	pipeline, ok := d.delegation.(delegatedPipeline)
	if !ok {
		return task.StatusFailed, ErrPipelineMissing.Error()
	}
	bound, err := builder.Bind(ctx, assembly)
	if err != nil {
		return task.StatusFailed, named(err)
	}
	defer closeFace(bound.Set)
	turn := Turn{UserID: string(site.AgentID), SessionID: string(site.SessionID), Input: input, Handoff: envelope}
	answer, err := pipeline.Submit(ctx, agent.DelegatedInput{
		Site:        site,
		Binding:     bound,
		Instruction: assembly.Instruction,
		Request:     turn.request(ctx, builder.Facts, builder.RefBudget),
	})
	builder.sealCapture(ctx)
	if err != nil {
		return task.StatusFailed, named(err)
	}
	if answer.Status == task.StatusFailed && answer.Failure != "" {
		return answer.Status, answer.Failure
	}
	return answer.Status, answer.Body
}

// completeDelegation reports the terminal of one delegated round beside the body it
// produced: a delegation that carries result material is asked for it, and one that
// does not is settled exactly as it was before the body travelled. The refusal is
// returned so the caller of a re-entered round learns that the outcome it produced was
// not recorded; a first round reports it beside the contract it always had.
func (d delegationFaces) completeDelegation(ctx context.Context, taskID event.TaskID, status task.Status, body string) error {
	if carrier, ok := d.delegation.(ResultDelegation); ok {
		_, err := carrier.CompleteWithResult(ctx, taskID, status, "", body)
		return err
	}
	_, err := d.delegation.Complete(ctx, taskID, status, "")
	return err
}

// denied renders one refusal as result text: the named case beside the source, field and
// reason it carries, and the statement that no task was admitted.
func (t *Tool) denied(reason error) string {
	return fmt.Sprintf("%s %s; no task was admitted", deniedMarker, named(reason))
}

// closeFace releases the tool-set connection one binding opened for the round it served:
// the MCP face a delegated binding resolved stays open exactly as long as the round that
// carries it, so the round that opened it closes it and no connection outlives its call.
func closeFace(set trpctool.ToolSet) {
	if set == nil {
		return
	}
	_ = set.Close()
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
