// 契约: docs/wiki/plugin/plugin-architecture.md#overview
//
// Package plugin declares the ordered per-phase pipeline through which tagent injects
// its differentiated capabilities into the framework event flow: the pre-execution call
// transform and risk ruling, the post-commit observation and final presentation, the
// request-assembly environment injection and the compress-request consumption.
//
// A plugin is a registration unit: it reports the phase it serves and implements that
// phase's one narrow interface. The pre-execution faces form one Before pass a request
// runs exactly once; the post-commit faces form the After pass, which reads only facts
// whose canonical commit already happened and answers with a Presentation. The two
// further phases answer their own pass: one request assembly asks for its environment
// block, and one compress request is handed to the faces that answer it.
//
//   - the extension-face vocabulary: the phases, the narrow interfaces each phase owns, the pipeline that dispatches them, the post-commit observation wiring and the presentation pass;
//   - the extension policies at the governance gate: the pre-execution ruling, the risk rules, the approval channel, the denial ledger, the goal registry and the budget window, each stated in its own file and reaching the pipeline only through the one face it serves.
package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
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
//   - PhaseCompressTrigger → CompressTrigger
//   - PhaseEnvironmentInject → EnvironmentInject
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

	// PhaseCompressTrigger is the consumption phase of a compress request: the
	// filed judgment that material is over its bound is handed to the faces that
	// answer it with the compression it owes, before the request is retired.
	PhaseCompressTrigger

	// PhaseEnvironmentInject is the request-assembly phase: the faces render the
	// dynamic environment material one request carries at the bottom of its
	// assembly, so what changes with the run never moves the stable prefix.
	PhaseEnvironmentInject
)

// Plugin is a tagent registration unit, distinct from the framework's plugin.Plugin: it
// reports the stable name of one pipeline element and the single phase that element
// serves, and the element implements that phase's narrow interface. The composition root
// reads the phase of every registered Plugin and dispatches the element into that
// phase's narrow interface set — TransformCall, RiskAssessor, Governor,
// PostCommitObserver, FinalPresenter, CompressTrigger, EnvironmentInject — in the
// declared order.
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
// The redline list (4 rules):
// MUST answer the presentation text beside whether the channel dropped it.
// MUST NOT drop a presentation without reporting the drop.
// MUST NOT retract the committed fact it was rendered from.
// MUST NOT be registered beside a second presenter: one fact carries one presentation.
type FinalPresenter interface {
	Present(ctx context.Context, id event.EventID) (string, bool)
}

// EnvironmentRequest names the request assembly one environment block is rendered
// for: the agent the request runs as. The material itself is read by the face from the
// runtime it holds, so an assembly never has to hand a bundle of its own state across
// the phase.
type EnvironmentRequest struct {
	// Agent is the agent the request assembly runs as.
	Agent event.AgentID
}

// EnvironmentInject is the request-assembly face: InjectEnvironment renders the dynamic
// environment material one request carries, and the assembly places that text where its
// own layout puts it.
//
// The redline list (3 rules):
// MUST render the material the request assembly holds at this boundary, and never a snapshot taken earlier.
// MUST NOT write the runtime it reads or change the request it serves.
// MUST NOT refuse a request to hide an empty face: an empty face is stated as empty, not as no block.
type EnvironmentInject interface {
	// InjectEnvironment renders the environment block of one request assembly. A nil
	// error carries the rendered text; a non-nil error refuses the assembly by name,
	// so no request is built over a block that failed to render.
	InjectEnvironment(ctx context.Context, request EnvironmentRequest) (string, error)
}

// CompressTrigger is the compress-consumption face: TriggerCompress answers one filed
// compress request with the compression it owes. The request is the judgment that
// material is over its bound; the face is the mechanism that answers it.
//
// The redline list (3 rules):
// MUST answer the request it was handed: its identity and subject stay as filed.
// MUST NOT retire the request: the consumer retires it once the products of its compression are committed.
// MUST NOT compress material no filed request names.
type CompressTrigger interface {
	// TriggerCompress dispatches one filed compress request. A nil error carries the
	// request forward into the compression that answers it; a non-nil error refuses
	// it by name, so the material is not compressed and the request stays pending.
	TriggerCompress(ctx context.Context, request event.CompressRequest) error
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
// never reassembled mid-request. The presentation phase holds at most one
// face, because the fact-to-presentation mapping is one-to-one.
//
//   - declaration order: every face runs in the order its plugin was declared;
//   - conflict refusal: the one Views projection field has exactly one writer, so a second final presenter is refused by name;
//   - composition closure: one After pass answers one Presentation for one fact carrying the one view its one presenter rendered.
type Pipeline struct {
	transforms []served[TransformCall]
	risks      []served[RiskAssessor]
	governors  []served[Governor]
	observers  []served[PostCommitObserver]
	presenters []served[FinalPresenter]
	triggers   []served[CompressTrigger]
	injects    []served[EnvironmentInject]
}

// NewPipeline groups the registered plugins into their phase's narrow interface
// set, in the declared order. A plugin whose phase is not one of the declared
// phases, a nil plugin, a plugin that implements no narrow interface of its own
// phase, and a second final presenter each refuse the pipeline by name instead
// of being trimmed in silence.
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
				if len(out.presenters) > 0 {
					return Pipeline{}, fmt.Errorf("plugin: %q serves final presentation beside %q: one fact is presented through exactly one presenter", name, out.presenters[0].name)
				}
				out.presenters = append(out.presenters, served[FinalPresenter]{name, impl})
				servedAny = true
			}
		case PhaseCompressTrigger:
			if impl, ok := element.(CompressTrigger); ok {
				out.triggers = append(out.triggers, served[CompressTrigger]{name, impl})
				servedAny = true
			}
		case PhaseEnvironmentInject:
			if impl, ok := element.(EnvironmentInject); ok {
				out.injects = append(out.injects, served[EnvironmentInject]{name, impl})
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
		started := time.Now()
		err := face.impl.TransformCall(ctx, &final)
		observePhase(PhaseCallTransform, faceTransform, face.name, phaseResult(err), "call="+call.ID, started)
		if err != nil {
			return CallPass{}, fmt.Errorf("plugin: transform %s: %w", face.name, err)
		}
		if final.ID != call.ID {
			return CallPass{}, fmt.Errorf("%w: %s moved call %q to %q", ErrCallIdentityChanged, face.name, call.ID, final.ID)
		}
	}
	risk := RiskLevel("")
	for _, face := range p.risks {
		started := time.Now()
		graded := face.impl.AssessRisk(ctx, &final)
		observePhase(PhaseRiskPolicy, faceRisk, face.name, phaseGrade(graded), "call="+call.ID, started)
		if graded.Rank() > risk.Rank() {
			risk = graded
		}
	}
	return CallPass{Final: final, Risk: risk}, nil
}

// TriggerCompress runs the trigger pass of one filed compress request: every
// compress face answers the request in pipeline order, so the compression a
// producer judged due is the one the wired faces deliver. The request is
// dispatched as the log filed it, and the consumer retires it afterwards — never
// here — so a request whose compression failed stays pending.
//
// A face that answers an error refuses the pass at that face by name: no later
// face runs, and the caller answers with the refusal instead of counting the
// request as compressed. A pipeline carrying no compress face answers nil, so a
// consumer wired without one refuses nothing of the request it read.
func (p Pipeline) TriggerCompress(ctx context.Context, request event.CompressRequest) error {
	for _, face := range p.triggers {
		started := time.Now()
		err := face.impl.TriggerCompress(ctx, request)
		observePhase(PhaseCompressTrigger, faceTrigger, face.name, phaseResult(err), "request="+string(request.ID), started)
		if err != nil {
			return fmt.Errorf("plugin: compress trigger %s: %w", face.name, err)
		}
	}
	return nil
}

// InjectEnvironment runs the injection pass of one request assembly: every
// environment face renders its block in pipeline order, and the pass answers the
// blocks that carried text, in that order. A face answering no text contributes
// no block, and a pipeline carrying no environment face answers no block at all.
//
// A face that answers an error refuses the pass at that face by name: no later
// face runs and the assembly answers with the refusal instead of carrying a
// block it could not render.
func (p Pipeline) InjectEnvironment(ctx context.Context, request EnvironmentRequest) (string, error) {
	blocks := make([]string, 0, len(p.injects))
	for _, face := range p.injects {
		started := time.Now()
		text, err := face.impl.InjectEnvironment(ctx, request)
		observePhase(PhaseEnvironmentInject, faceInject, face.name, phaseResult(err), "agent="+string(request.Agent), started)
		if err != nil {
			return "", fmt.Errorf("plugin: environment inject %s: %w", face.name, err)
		}
		if text != "" {
			blocks = append(blocks, text)
		}
	}
	return strings.Join(blocks, "\n\n"), nil
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
	case PhaseCompressTrigger:
		return "compress-trigger"
	case PhaseEnvironmentInject:
		return "environment-inject"
	default:
		return fmt.Sprintf("phase-%d", phase)
	}
}

// faceTransform names the transform face of the call-transform phase in one
// observation line; the rest name the single face of their own phase the same
// way. The face is the narrow interface the dispatched element was held to, not
// the phase it registered for.
const (
	faceTransform = "transform"
	faceRisk      = "risk"
	faceObserve   = "observe"
	facePresent   = "present"
	faceTrigger   = "trigger"
	faceInject    = "inject"
)

// observePhase renders one line of one executed phase face at DEBUG: the phase,
// the face, the plugin that served it, the face's own result, the elapsed time
// and the identity the face was dispatched for. The line carries no call
// arguments and no fact content.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a refused pass.
// MUST render after the face returned, and never stand a second pass outcome of its own.
// MUST NOT render call arguments, tool output or fact content.
func observePhase(phase Phase, face, name, result, subject string, started time.Time) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Debugf("tagent: plugin phase phase=%s face=%s plugin=%s result=%s elapsed=%s %s",
		phaseName(phase), face, name, result, time.Since(started).Round(time.Microsecond), subject)
}

// phaseResult spells whether one face answered without an error.
func phaseResult(err error) string {
	if err != nil {
		return "refused"
	}
	return "ok"
}

// phaseGrade spells the risk grade one assessing face rendered, and names the
// absence of a grade instead of rendering an empty result.
func phaseGrade(graded RiskLevel) string {
	if graded == "" {
		return "unstated"
	}
	return string(graded)
}
