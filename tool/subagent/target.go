// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// The target tool of one declared agent reference: the declaration names the
// target agent, so the model sees a tool of that agent's own name instead of a
// generic call it could point anywhere. The tool surface behind it is the
// target's own declaration — the tools that agent declares, not the caller's
// assembled set — so the reference grants exactly the declared target and
// nothing wider.
package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ParamAgent, ParamDescriptionFile and ParamEventParams are the reference
// parameters one agent reference carries: the declared target it authorizes,
// the file its description is read from at every request boundary, and the
// call arguments whose values travel as the handoff's refs.
const (
	// ParamAgent names the declared agent one reference authorizes.
	ParamAgent = "agent"
	// ParamDescriptionFile names the file the target's description is read from.
	ParamDescriptionFile = "description_file"
	// ParamEventParams names the call arguments that carry event references.
	ParamEventParams = "event_params"
)

// ErrTargetNameMissing reports a target tool constructed with no name.
var ErrTargetNameMissing = errors.New("subagent: the target tool names no declared agent")

// ErrTargetArguments reports a call carrying a key the target's declaration
// does not name.
var ErrTargetArguments = errors.New("subagent: the call names no declared argument")

// TargetOptions fixes what the composition root hands one target tool: the
// declared target's identity and purpose, the assembly its delegated turn runs
// on, the builder that runs it, the delegation and borrowing faces of the run
// and the call arguments that travel as the handoff's refs.
type TargetOptions struct {
	// Name is the tool name the model sees: the declared target agent identity.
	Name string
	// Description is the declared purpose read when no description file is
	// named, and the fallback of a file that cannot be read.
	Description string
	// DescriptionFile is the file the description is read from at every
	// declaration read, so a rewritten file takes effect with no reload.
	DescriptionFile string
	// Assembly is the resolved declaration of the target agent: its identity,
	// model, tool names and instruction.
	Assembly *Assembly
	// Builder runs the assembly into the live agent and one turn of it.
	Builder Builder
	// Delegation is the task face the call is admitted through.
	Delegation Delegation
	// Bindings is the state owner's borrowing face.
	Bindings state.Borrow
	// EventParams lists the call arguments whose values are handed over as the
	// envelope's refs, in declaration order.
	EventParams []string
}

// Target is the model-visible face of one declared agent reference: a tool of
// the target's own name whose call admits one turn of that target as a task.
type Target struct {
	name            string
	description     string
	descriptionFile string
	assembly        *Assembly
	builder         Builder
	faces           delegationFaces
	params          []string
}

// The target tool satisfies the native callable tool port.
var _ trpctool.CallableTool = (*Target)(nil)

// NewTarget returns the target tool over the declared faces.
func NewTarget(opts TargetOptions) (*Target, error) {
	if strings.TrimSpace(opts.Name) == "" {
		return nil, ErrTargetNameMissing
	}
	if opts.Delegation == nil {
		return nil, ErrDelegationMissing
	}
	if opts.Bindings == nil {
		return nil, ErrBindingsMissing
	}
	if opts.Assembly == nil {
		return nil, ErrAssemblyMissing
	}
	return &Target{
		name:            opts.Name,
		description:     opts.Description,
		descriptionFile: opts.DescriptionFile,
		assembly:        opts.Assembly,
		builder:         opts.Builder,
		faces:           delegationFaces{delegation: opts.Delegation, bindings: opts.Bindings},
		params:          append([]string(nil), opts.EventParams...),
	}, nil
}

// Declaration implements the native callable tool port. The description is read
// from the declared file at every read, so a rewritten file reaches the next
// request's tool declaration without a reload; a file that cannot be read
// answers with the declared description instead.
func (t *Target) Declaration() *trpctool.Declaration {
	properties := map[string]*trpctool.Schema{
		"input": {
			Type:        "string",
			Description: "the request the " + t.name + " turn starts from",
		},
		"handoff": handoffSchema(),
	}
	for _, param := range t.params {
		properties[param] = &trpctool.Schema{
			Type:        "array",
			Items:       &trpctool.Schema{Type: "string"},
			Description: "the stored event references this call hands the " + t.name + " turn",
		}
	}
	return &trpctool.Declaration{
		Name: t.name,
		Description: t.purpose() + " " +
			"the call admits one turn of the declared agent as an asynchronous task and answers with its admission receipt; " +
			"progress and terminal arrive as the notifications of that task. " +
			"the call may state a structured handoff beside the request and may hand over stored event references. " +
			"every refusal answers " + deniedMarker + " beside no task.",
		InputSchema: &trpctool.Schema{
			Type:       "object",
			Properties: properties,
		},
	}
}

// purpose reports the description this tool declares: the file's text when it
// can be read, the declared text otherwise.
func (t *Target) purpose() string {
	if t.descriptionFile == "" {
		return t.description
	}
	body, err := os.ReadFile(t.descriptionFile)
	if err != nil {
		return t.description
	}
	if text := strings.TrimSpace(string(body)); text != "" {
		return text
	}
	return t.description
}

// Call implements the native callable tool port: it reads one call's declared
// arguments, builds the handoff the declared event params feed, admits one turn
// of the target as a task and answers with the receipt of that admission.
func (t *Target) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return nil, task.ErrCallFrameMissing
	}
	args, err := t.parseArgs(jsonArgs)
	if err != nil {
		return t.denied(err), nil
	}
	envelope, err := t.envelope(args)
	if err != nil {
		return t.denied(err), nil
	}
	receipt, err := t.faces.admit(ctx, t.builder, frame, t.assembly, args.Input, envelope)
	if err != nil {
		return t.denied(err), nil
	}
	return receipt, nil
}

// targetArgs is the declared argument shape one target call carries: the
// request, the optional handoff and the values of the declared event params.
type targetArgs struct {
	// Input is the request the target turn starts from.
	Input string
	// Handoff is the structured handoff the call declares beside that request.
	Handoff *handoffArgs
	// Refs lists the event references the declared params carried, in
	// declaration order.
	Refs []event.EventID
	// Declared reports whether the call carried a handoff or any event
	// reference at all.
	Declared bool
}

// parseArgs reads one call's arguments against the key set this tool declares:
// a key outside it is refused by name, and the declared event params are read
// as the reference lists they name.
func (t *Target) parseArgs(jsonArgs []byte) (targetArgs, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(jsonArgs, &probe); err != nil {
		return targetArgs{}, fmt.Errorf("%s: malformed arguments: %w", t.name, err)
	}
	declared := map[string]bool{"input": true, "handoff": true}
	for _, param := range t.params {
		declared[param] = true
	}
	unknown := make([]string, 0, len(probe))
	for key := range probe {
		if !declared[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return targetArgs{}, fmt.Errorf("%s: %w: %s", t.name, ErrTargetArguments, joinNames(unknown))
	}
	var args targetArgs
	if raw, ok := probe["input"]; ok {
		if err := json.Unmarshal(raw, &args.Input); err != nil {
			return targetArgs{}, fmt.Errorf("%s: malformed input: %w", t.name, err)
		}
	}
	if raw, ok := probe["handoff"]; ok && string(raw) != "null" {
		if err := checkHandoffKeys(raw); err != nil {
			return targetArgs{}, err
		}
		var handoff handoffArgs
		if err := json.Unmarshal(raw, &handoff); err != nil {
			return targetArgs{}, fmt.Errorf("%s: malformed handoff: %w", t.name, err)
		}
		args.Handoff = &handoff
		args.Declared = true
	}
	for _, param := range t.params {
		raw, ok := probe[param]
		if !ok {
			continue
		}
		var refs []string
		if err := json.Unmarshal(raw, &refs); err != nil {
			return targetArgs{}, fmt.Errorf("%s: malformed %s: %w", t.name, param, err)
		}
		for _, ref := range refs {
			if trimmed := strings.TrimSpace(ref); trimmed != "" {
				args.Refs = append(args.Refs, event.EventID(trimmed))
				args.Declared = true
			}
		}
	}
	return args, nil
}

// envelope builds the handoff one target call hands its turn: the intent the
// declaration fixes, the constraints, pending decisions and refs the call
// states beside the declared event references. A call that declares neither a
// handoff nor an event reference hands no envelope, so the turn runs on the
// request alone.
func (t *Target) envelope(args targetArgs) (*agent.HandoffEnvelope, error) {
	if !args.Declared {
		return nil, nil
	}
	envelope := agent.HandoffEnvelope{Intent: t.purpose()}
	if handoff := args.Handoff; handoff != nil {
		if intent := strings.TrimSpace(handoff.Intent); intent != "" {
			envelope.Intent = intent
		}
		envelope.Constraints = append([]string(nil), handoff.Constraints...)
		envelope.PendingDecisions = append([]string(nil), handoff.PendingDecisions...)
		for _, ref := range handoff.Refs {
			envelope.Refs = append(envelope.Refs, event.EventID(ref))
		}
	}
	envelope.Refs = append(envelope.Refs, args.Refs...)
	envelope.Grants = grantsOf(t.assembly)
	if args.Handoff != nil {
		if err := envelope.Validate(); err != nil {
			return nil, err
		}
	}
	return &envelope, nil
}

// denied renders one refusal as result text: the named case beside the source,
// field and reason it carries, and the statement that no task was admitted.
func (t *Target) denied(reason error) string {
	return fmt.Sprintf("%s %s; no task was admitted", deniedMarker, named(reason))
}
