// 契约: docs/wiki/plugin/plugin-architecture.md#overview
//
// Package plugin declares the ordered per-phase pipeline through which tagent injects
// its differentiated capabilities into the framework event flow: the pre-execution call
// transform and risk ruling, and the post-commit observation and final presentation.
//
// A plugin is a registration unit: it reports the phase it serves and implements that
// phase's one narrow interface. The pre-execution faces form one Before pass a request
// runs exactly once; the post-commit faces form the After pass, which reads only facts
// whose canonical commit already happened and answers with a Presentation.
//
//   - the extension-face vocabulary: the phases, the four narrow interfaces, the pipeline that dispatches them, the post-commit observation wiring and the presentation pass;
//   - the extension policies at the governance gate: the pre-execution ruling, the risk rules, the approval channel, the denial ledger, the goal registry and the budget window, each stated in its own file and reaching the pipeline only through the one face it serves.
package plugin

import (
	"context"
	"errors"
	"fmt"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
)

// Phase names one stage of the single ordered plugin pipeline; the constant order is
// the pipeline order. Each phase owns one narrow interface set, and the phase a plugin
// reports selects the set that plugin is dispatched into:
//
//   - PhaseCallTransform → TransformCall
//   - PhaseRiskPolicy → RiskAssessor and Governor
//   - PhasePostCommitObserve → PostCommitObserver
//   - PhaseFinalPresentation → FinalPresenter
//
// The risk phase carries two faces of one ruling: a RiskAssessor renders the grade and a
// Governor renders the one pre-execution ruling over that grade. The two stand beside
// each other as two faces of one phase, never as two risk faces — no grade is rendered
// twice — and one element may implement either face or both.
type Phase int

const (
	// PhaseCallTransform is the pre-execution call phase: the call is corrected
	// before its side effect and before its admission ruling, and the corrected
	// call is the one re-checked, executed and recorded.
	PhaseCallTransform Phase = iota

	// PhaseRiskPolicy grades the risk of a tool call before its side effect and
	// renders the pre-execution ruling. The phase carries two faces of that one
	// ruling: a RiskAssessor renders the grade and a Governor renders the ruling
	// over it; the grade is read once and never recomputed by the ruling.
	PhaseRiskPolicy

	// PhasePostCommitObserve observes a fact after its canonical commit, as a
	// read-only duty.
	PhasePostCommitObserve

	// PhaseFinalPresentation renders the final presentation of a fact for its
	// channel.
	PhaseFinalPresentation
)

// Plugin is a tagent registration unit, distinct from the framework's plugin.Plugin: it
// reports the stable name of one pipeline element and the single phase that element
// serves, and the element implements that phase's narrow interface. The composition root
// reads the phase of every registered Plugin and dispatches the element into that
// phase's narrow interface set — TransformCall, RiskAssessor, Governor,
// PostCommitObserver, FinalPresenter — in the declared order.
//
// The redline list (3 rules):
// MUST report the one phase the element serves and implement that phase's narrow interface.
// MUST NOT report a phase whose narrow interface it does not implement.
// MUST NOT serve two phases: one registration element serves one phase and may carry the faces of that phase (the risk phase carries two faces of one ruling).
type Plugin interface {
	// Name returns the stable name of this plugin.
	Name() string

	// Phase reports the single phase this plugin serves, which both orders the
	// plugin inside the pipeline and selects the narrow interface it implements.
	Phase() Phase
}

// TransformCall is the tool-call-level pre-side-effect face, and it is where the
// allowed tool-call parameter transform is carried: it runs once per tool call before
// that call's side effect, not once at request assembly. The transform happens before
// the tool runs, earlier than every execution side effect other than admission, so it
// may correct or constrain the tool name and parameters of the call and must not change
// the call's identity: its CallID and trusted scope stay as resolved. A transformed
// call never bypasses the core: the transformed call is admitted through the wrapper's
// admission path, approval and dedup included, before it runs, the plugin never
// executes from the transform result directly, and the transformed call is the one
// both executed and recorded. A transform that answers an error refuses the pass: the
// call does not run, nothing of it is recorded, and the pass answers no CallPass.
//
// The redline list (3 rules):
// MUST keep the ID the call was resolved under; the pass refuses a transform that moves it.
// MUST NOT execute the call itself: admission, approval and dedup stay with the core.
// MUST NOT hold on to the call pointer past the call it was handed.
type TransformCall interface {
	// TransformCall transforms one call before its side effect runs. It runs once
	// per call and before admission, and the transformed call is re-checked by the
	// core before it runs, as the one both executed and recorded. A nil error
	// carries the transform forward; a non-nil error refuses the pass by name, so
	// the call is neither executed nor recorded.
	TransformCall(ctx context.Context, call *trpcmodel.ToolCall) error
}

// RiskAssessor is the pre-side-effect risk face: AssessRisk grades one tool call before
// its side effect runs, from the parameters and identity already resolved for that
// call.
//
// The redline list (2 rules):
// MUST grade the call it was handed and answer a declared RiskLevel.
// MUST NOT run a side effect or render the ruling: the ruling reads the grade it rendered.
type RiskAssessor interface {
	AssessRisk(ctx context.Context, call *trpcmodel.ToolCall) RiskLevel
}

// PostCommitObserver is the post-commit observation face: ObserveCommit observes one
// fact after its canonical commit, as a read-only duty.
//
// The redline list (3 rules):
// MUST observe a fact whose canonical commit already happened.
// MUST NOT roll the committed fact back or re-run the work when the observation fails.
// MUST NOT write the fact it observes.
type PostCommitObserver interface {
	ObserveCommit(ctx context.Context, id event.EventID) error
}

// FinalPresenter is the final-presentation face: Present renders the presentation of
// one fact for its channel and reports the presentation text with whether it was
// dropped.
//
// The redline list (3 rules):
// MUST answer the presentation text beside whether the channel dropped it.
// MUST NOT drop a presentation without reporting the drop.
// MUST NOT retract the committed fact it was rendered from.
type FinalPresenter interface {
	Present(ctx context.Context, id event.EventID) (string, bool)
}

// served is one registered pipeline face beside the stable name of the plugin
// that serves it, so a pass outcome can report which plugin rendered it and a
// failure is refused by name.
type served[T any] struct {
	name string
	impl T
}

// Pipeline is the phase-grouped face set of one ordered plugin pipeline: every
// registered Plugin is dispatched into its own phase's narrow interface set in
// the declared order, and one plugin serves exactly one phase.
//
// The pre-execution faces run as the one Before pass of a tool call; the
// post-commit faces run as the After pass of a committed fact. The pipeline is
// assembled once per dispatch from the borrowed generation's plugins and is
// never reassembled mid-request.
type Pipeline struct {
	transforms []served[TransformCall]
	risks      []served[RiskAssessor]
	governors  []served[Governor]
	observers  []served[PostCommitObserver]
	presenters []served[FinalPresenter]
}

// NewPipeline groups the registered plugins into their phase's narrow interface
// set, in the declared order. A plugin whose phase is not one of the four
// declared phases, a nil plugin, and a plugin that implements no narrow
// interface of its own phase each refuse the pipeline by name instead of being
// trimmed in silence.
func NewPipeline(plugins []Plugin) (Pipeline, error) {
	var out Pipeline
	for _, element := range plugins {
		if element == nil {
			return Pipeline{}, errors.New("plugin: the pipeline holds a nil plugin")
		}
		name := element.Name()
		servedAny := false
		switch element.Phase() {
		case PhaseCallTransform:
			if impl, ok := element.(TransformCall); ok {
				out.transforms = append(out.transforms, served[TransformCall]{name, impl})
				servedAny = true
			}
		case PhaseRiskPolicy:
			if impl, ok := element.(RiskAssessor); ok {
				out.risks = append(out.risks, served[RiskAssessor]{name, impl})
				servedAny = true
			}
			if impl, ok := element.(Governor); ok {
				out.governors = append(out.governors, served[Governor]{name, impl})
				servedAny = true
			}
		case PhasePostCommitObserve:
			if impl, ok := element.(PostCommitObserver); ok {
				out.observers = append(out.observers, served[PostCommitObserver]{name, impl})
				servedAny = true
			}
		case PhaseFinalPresentation:
			if impl, ok := element.(FinalPresenter); ok {
				out.presenters = append(out.presenters, served[FinalPresenter]{name, impl})
				servedAny = true
			}
		default:
			return Pipeline{}, fmt.Errorf("plugin: %q serves phase %d, which is not one of the declared phases", name, element.Phase())
		}
		if !servedAny {
			return Pipeline{}, fmt.Errorf("plugin: %q serves phase %s but implements no narrow interface of that phase", name, phaseName(element.Phase()))
		}
	}
	return out, nil
}

// CallPass is the whole input of one pre-execution ruling: the final call the
// Before pass produced, the strongest risk grade its faces rendered and what
// triggered the call. The final call is the one both executed and recorded; the
// risk grade advises the pre-execution ruling without ever running a second risk
// face; the trigger source is stated by the dispatching layer that knows what
// started the call, so a ruling has no hidden input of its own.
type CallPass struct {
	// Final is the transformed final call the pass produced: the call with the
	// allowed parameter transform applied and its CallID and trusted scope left
	// as resolved. It is the call the core re-checks, executes and records.
	Final trpcmodel.ToolCall

	// Risk is the strongest grade the risk faces rendered for the final call,
	// from RiskLow to RiskCritical; it stays empty when the pipeline carries no
	// risk face. An empty grade is not a grade of its own and never bypasses the
	// gates: the ruling reads it as RiskMedium, so a call beside no risk face is
	// still ruled as an ordinary mutating call.
	Risk RiskLevel

	// TriggerSource names what triggered the call — the source a goal gate and a
	// trigger-scoped rule match on exactly. The dispatcher states it beside the
	// pass it produced; the pass never guesses one, and an empty value means the
	// call declares no source.
	TriggerSource string
}

// ErrCallIdentityChanged reports a transform that changed the identity of the
// call it was handed: a transform may correct the tool name and parameters of a
// call and must never change its CallID, which stays as resolved. The Before pass
// reports it beside the plugin that moved the identity and errors.Is matches it.
var ErrCallIdentityChanged = errors.New("plugin: the transform changed the call identity")

// Before runs the one Before pass of one tool call: every transform face
// corrects a private copy of the call in pipeline order, then every risk face
// grades the transformed call, and the pass reports the final call beside the
// strongest grade. The pass runs exactly once per call — before the call is
// admitted and before its side effect — and is never run again when a recorded
// fact is replayed. The call the model recorded is left byte-identical: the
// transform works on its own copy.
//
// A transform that answers an error refuses the whole pass at that face: no later
// face runs, no ruling is rendered, and the call is neither executed nor recorded.
// A transform that moves the call's CallID refuses the pass by name. The
// transformed final call is re-checked by the core before it runs — identity
// and permission at the wrapper's admission path — and the same final call is
// the one both executed and recorded.
func (p Pipeline) Before(ctx context.Context, call trpcmodel.ToolCall) (CallPass, error) {
	final := copyCall(call)
	for _, face := range p.transforms {
		if err := face.impl.TransformCall(ctx, &final); err != nil {
			return CallPass{}, fmt.Errorf("plugin: transform %s: %w", face.name, err)
		}
		if final.ID != call.ID {
			return CallPass{}, fmt.Errorf("%w: %s moved call %q to %q", ErrCallIdentityChanged, face.name, call.ID, final.ID)
		}
	}
	risk := RiskLevel("")
	for _, face := range p.risks {
		graded := face.impl.AssessRisk(ctx, &final)
		if graded.Rank() > risk.Rank() {
			risk = graded
		}
	}
	return CallPass{Final: final, Risk: risk}, nil
}

// copyCall returns a private copy of one call: the argument bytes, the
// streaming index and the passthrough fields are copied, so a transform
// corrects its own copy and the call the model recorded stays as it stands.
func copyCall(in trpcmodel.ToolCall) trpcmodel.ToolCall {
	out := in
	if in.Function.Arguments != nil {
		out.Function.Arguments = append([]byte(nil), in.Function.Arguments...)
	}
	if in.Index != nil {
		index := *in.Index
		out.Index = &index
	}
	if in.ExtraFields != nil {
		fields := make(map[string]any, len(in.ExtraFields))
		for key, value := range in.ExtraFields {
			fields[key] = value
		}
		out.ExtraFields = fields
	}
	return out
}

// phaseName reports the declared name of one phase for a refusal message.
func phaseName(phase Phase) string {
	switch phase {
	case PhaseCallTransform:
		return "call-transform"
	case PhaseRiskPolicy:
		return "risk-policy"
	case PhasePostCommitObserve:
		return "post-commit-observe"
	case PhaseFinalPresentation:
		return "final-presentation"
	default:
		return fmt.Sprintf("phase-%d", phase)
	}
}
