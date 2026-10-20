// 契约: docs/wiki/plugin/plugin-architecture.md#overview
//
// Package plugin declares the material flow through which tagent injects its
// differentiated capabilities into the framework event flow: the producing phase
// and the consuming phase, each dispatched by the material a face reads, and the
// six ports those phases serve.
//
// A plugin is a registration unit: it reports the stable name of one flow
// instance and the route word of the port the instance serves — the material it
// reads and the product it answers — and the instance implements that port's
// narrow interface. The flow derives the phase from the port and dispatches the
// instance by the material that port reads. A built-in consumer — the drive that
// runs one decision, the tick that runs one reflection, the assembler that
// builds one request — is an instance of the same flow: the difference between a
// built-in instance and a mounted plugin is who mounts it, never how it is
// dispatched.
//
//   - the extension-face vocabulary: the phases, the ports, the narrow interfaces each port owns, the pipeline that dispatches them by material, the post-commit observation wiring and the presentation pass;
//   - the extension policies at the governance gate: the pre-execution ruling, the risk rules, the approval channel, the denial ledger, the goal registry and the budget window, each stated in its own file and reaching the pipeline only through the one face it serves.
package plugin

import (
	"context"
	"errors"
	"fmt"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
)

// Phase names the role one face serves in the material flow, and the roles are two:
//   - PhaseProduce: the face answers a product that grows out of the material it read — a corrected call, a ruling over a call, the environment blocks of a request;
//   - PhaseConsume: the face answers a consumption of the material it read — an observation of a fact, a view of a fact, the compression a filed request asks for.
//
// The phase is a reading of the port, never a position in time: the flow
// dispatches a face by the material it reads, and the order inside one material
// is the order the instances were declared in.
type Phase int

const (
	// PhaseProduce is the producing phase: the face answers a product of the
	// material it read.
	PhaseProduce Phase = iota

	// PhaseConsume is the consuming phase: the face answers a consumption of the
	// material it read.
	PhaseConsume
)

// phaseUnstated is the phase of a port the closed table does not declare; it is
// answered beside the refusal and is never a phase of the flow.
const phaseUnstated Phase = -1

// routeWordBase separates the six route words a Plugin reports from the two
// phases: a route word names one port of the flow and reads as that port's
// phase.
const routeWordBase Phase = 1 << 8

// PhaseCallTransform, PhaseRiskPolicy, PhasePostCommitObserve, PhaseFinalPresentation,
// PhaseCompressTrigger and PhaseEnvironmentInject are the six route words of Plugin:
// each names one port of the flow, and an element registering under it is dispatched
// exactly as an Instance declaring that port is dispatched.
//
//   - PhaseCallTransform → call->call: the call transform face
//   - PhaseRiskPolicy → call->ruling: the risk face and the ruling face
//   - PhasePostCommitObserve → fact->observation: the post-commit observation face
//   - PhaseFinalPresentation → fact->view: the final presentation face
//   - PhaseCompressTrigger → fact->compression: the compress-trigger face
//   - PhaseEnvironmentInject → request->environment: the request-assembly face
const (
	// PhaseCallTransform is the route word of the call->call port: the call is
	// corrected before its side effect and before its admission ruling, and the
	// corrected call is the one re-checked, executed and recorded.
	PhaseCallTransform Phase = routeWordBase + iota

	// PhaseRiskPolicy is the route word of the call->ruling port: the grade of
	// one call before its side effect beside the one pre-execution ruling over
	// it. The port carries two faces of that one ruling: a RiskAssessor renders
	// the grade and a Governor renders the ruling over it; the grade is read
	// once and never recomputed by the ruling.
	PhaseRiskPolicy

	// PhasePostCommitObserve is the route word of the fact->observation port: one
	// committed fact observed after its canonical commit, as a read-only duty.
	PhasePostCommitObserve

	// PhaseFinalPresentation is the route word of the fact->view port: the final
	// presentation of one committed fact for its channel.
	PhaseFinalPresentation

	// PhaseCompressTrigger is the route word of the fact->compression port: the
	// filed judgment that material is over its bound is handed to the faces that
	// answer it with the compression it owes, before the request is retired. The
	// port is the starter of the traceable mapping chain the request drives,
	// never the curation itself.
	PhaseCompressTrigger

	// PhaseEnvironmentInject is the route word of the request->environment port:
	// the faces render the dynamic environment material one request carries at
	// the bottom of its assembly, so what changes with the run never moves the
	// stable prefix. Each block a face answers is one product of that request,
	// traceable to it.
	PhaseEnvironmentInject
)

// Plugin is the registration unit a mounted element reports, distinct from the
// framework's plugin.Plugin: the stable name of one pipeline element and one of
// the six route words, each naming the port that element serves. The flow reads
// the port the word names and dispatches the element exactly as an Instance
// declaring that port is dispatched; the element implements the narrow interface
// of its port.
//
// The redline list (3 rules):
// MUST report the one route word of the port it serves and implement that port's narrow interface.
// MUST NOT report a route word whose narrow interface it does not implement.
// MUST NOT serve two ports: one registration element serves one port and may carry both faces of the ruling port.
type Plugin interface {
	// Name returns the stable name of this plugin.
	Name() string

	// Phase reports the route word of the one port this plugin serves.
	Phase() Phase
}

// TransformCall is the call->call port's narrow face, and it is where the
// allowed tool-call parameter transform is carried: it runs once per tool call
// before that call's side effect, not once at request assembly. The transform
// happens before the tool runs, earlier than every execution side effect other
// than admission, so it may correct or constrain the tool name and parameters of
// the call and must not change the call's identity: its CallID and trusted scope
// stay as resolved. A transformed call never bypasses the core: the transformed
// call is admitted through the wrapper's admission path, approval and dedup
// included, before it runs, the plugin never executes from the transform result
// directly, and the transformed call is the one both executed and recorded. A
// transform that answers an error refuses the pass: the call does not run,
// nothing of it is recorded, and the pass answers no CallPass.
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

// RiskAssessor is the call->ruling port's grade face: AssessRisk grades one tool
// call before its side effect runs, from the parameters and identity already
// resolved for that call.
//
// The redline list (2 rules):
// MUST grade the call it was handed and answer a declared RiskLevel.
// MUST NOT run a side effect or render the ruling: the ruling reads the grade it rendered.
type RiskAssessor interface {
	AssessRisk(ctx context.Context, call *trpcmodel.ToolCall) RiskLevel
}

// PostCommitObserver is the fact->observation port's narrow face: ObserveCommit
// observes one fact after its canonical commit, as a read-only duty.
//
// The redline list (3 rules):
// MUST observe a fact whose canonical commit already happened.
// MUST NOT roll the committed fact back or re-run the work when the observation fails.
// MUST NOT write the fact it observes.
type PostCommitObserver interface {
	ObserveCommit(ctx context.Context, id event.EventID) error
}

// FinalPresenter is the fact->view port's narrow face: Present renders the
// presentation of one fact for its channel and reports the presentation text
// with whether it was dropped. One fact may be projected through several
// presenters: each answers its own view, the views stand in the declared order,
// and every view carries the fact it was rendered from, so the products a fact
// answers stay traceable — one presenter, several, or none at all.
//
// The redline list (3 rules):
// MUST answer the presentation text beside whether the channel dropped it.
// MUST NOT drop a presentation without reporting the drop.
// MUST NOT retract the committed fact it was rendered from.
type FinalPresenter interface {
	Present(ctx context.Context, id event.EventID) (string, bool)
}

// EnvironmentRequest names the request assembly the environment blocks are
// rendered for: the agent the request runs as. The material itself is read by
// the face from the runtime it holds, so an assembly never has to hand a bundle
// of its own state across the phase. The request is every block's provenance:
// each block traces back to the assembly it was rendered for, a block created
// for an assembly that consumes no fact at all included.
type EnvironmentRequest struct {
	// Agent is the agent the request assembly runs as: provenance, the source that
	// every block rendered for this request traces back to.
	Agent event.AgentID
}

// EnvironmentInject is the request->environment port's narrow face:
// InjectEnvironment renders the dynamic environment material one request
// carries, and the assembly places those blocks where its own layout puts them.
// Each block is one product of this face and traces back to the request it was
// rendered for: the face answers one block, several, or none, and it never
// merges two products into one text.
//
// The redline list (3 rules):
// MUST render the material the request assembly holds at this boundary, and never a snapshot taken earlier.
// MUST NOT write the runtime it reads or change the request it serves.
// MUST NOT refuse a request to hide an empty face: an empty face is stated as empty, not as no block.
type EnvironmentInject interface {
	// InjectEnvironment renders the environment blocks of one request assembly. A
	// nil error carries the rendered blocks as separate products; a non-nil error
	// refuses the assembly by name, so no request is built over a block that
	// failed to render.
	InjectEnvironment(ctx context.Context, request EnvironmentRequest) ([]string, error)
}

// CompressTrigger is the fact->compression port's narrow face:
// TriggerCompress starts the traceable mapping chain one filed compress request
// drives — the curation that consumes the request and answers the compression it
// owes — and hands that chain the request as filed. The request is the judgment
// that material is over its bound; this face is the chain's starter, never the
// mapping itself, so it answers no product of its own to trace: the products the
// curation mints are the traceable ones.
//
// The redline list (3 rules):
// MUST answer the request it was handed: its identity and subject stay as filed.
// MUST NOT retire the request: the consumer retires it once the products of its compression are committed.
// MUST NOT compress material no filed request names.
type CompressTrigger interface {
	// TriggerCompress starts the compression one filed compress request drives. A
	// nil error carries the request forward into the compression that answers it;
	// a non-nil error refuses it by name, so the material is not compressed and
	// the request stays pending.
	TriggerCompress(ctx context.Context, request event.CompressRequest) error
}

// served is one registered pipeline face beside the stable name of the plugin
// that serves it, so a pass outcome can report which plugin rendered it and a
// failure is refused by name.
type served[T any] struct {
	name string
	impl T
}

// Pipeline is the material flow of one borrowed generation: every registered
// instance is dispatched into the face set of the port it declares, and every
// pass dispatches by the one material it reads.
//
//   - the call material runs as the one Before pass of a tool call;
//   - the fact material runs as the one After pass of a committed fact, and as the one TriggerCompress pass of a filed compress request;
//   - the request material runs as the one InjectEnvironment pass of a request assembly.
//
// The view port keeps every face it was declared, because the fact-to-view
// mapping is a traceable one with free cardinality: a fact answers one view,
// several views, or none. The environment port answers the same traceable way: a
// face answers one block, several blocks, or none, and every block it answers is
// one product. The flow is assembled once per dispatch from the borrowed
// generation's instances and is never reassembled mid-request.
//
//   - declaration order: every face of one material runs in the order its instance was declared;
//   - provenance refusal: a name identifies the one element that wrote a product, so a second element declared under one name is refused by name instead of leaving two products untraceable;
//   - composition closure: one After pass answers one Presentation for one fact, and every view it carries names the presenter that wrote it beside the fact it was rendered from, so every product of the chain stays traceable.
type Pipeline struct {
	transforms []served[TransformCall]
	risks      []served[RiskAssessor]
	governors  []served[Governor]
	injects    []served[EnvironmentInject]
	observers  []served[PostCommitObserver]
	presenters []served[FinalPresenter]
	triggers   []served[CompressTrigger]
}

// declaration is one registered element beside the port its registration
// declared, so both registration faces are assembled by one path.
type declaration struct {
	name string
	port Port
	impl any
}

// NewPipeline assembles the material flow out of the registration units that
// report a route word, in the declared order. It reads the port each route word
// names and dispatches the element exactly as NewFlow dispatches an instance
// declaring that port. A plugin whose phase is not one of the route words, a nil
// plugin, a plugin that implements no narrow interface of its port, and a name
// declared twice each refuse the flow by name instead of being trimmed in
// silence.
func NewPipeline(plugins []Plugin) (Pipeline, error) {
	declarations := make([]declaration, 0, len(plugins))
	for _, element := range plugins {
		if element == nil {
			return Pipeline{}, errors.New("plugin: the pipeline holds a nil plugin")
		}
		port, declared := portOfPhase(element.Phase())
		if !declared {
			return Pipeline{}, fmt.Errorf("plugin: %q serves phase %d, which is not one of the declared phases", element.Name(), element.Phase())
		}
		declarations = append(declarations, declaration{name: element.Name(), port: port, impl: element})
	}
	return assemble(declarations)
}

// assemble groups the declarations into their port's face set, in the declared
// order, reading the phase from the closed port table.
func assemble(declarations []declaration) (Pipeline, error) {
	var out Pipeline
	names := make(map[string]struct{}, len(declarations))
	for _, element := range declarations {
		if _, declared := names[element.name]; declared {
			return Pipeline{}, fmt.Errorf("plugin: %q is declared twice: a product's name must trace back to the one element that wrote it", element.name)
		}
		names[element.name] = struct{}{}
		if _, err := RouteOf(element.port); err != nil {
			return Pipeline{}, fmt.Errorf("plugin: %q declares %v", element.name, err)
		}
		servedAny := false
		switch element.port {
		case Port{Reads: MaterialCall, Produces: ProductCall}:
			if impl, ok := element.impl.(TransformCall); ok {
				out.transforms = append(out.transforms, served[TransformCall]{element.name, impl})
				servedAny = true
			}
		case Port{Reads: MaterialCall, Produces: ProductRuling}:
			if impl, ok := element.impl.(RiskAssessor); ok {
				out.risks = append(out.risks, served[RiskAssessor]{element.name, impl})
				servedAny = true
			}
			if impl, ok := element.impl.(Governor); ok {
				out.governors = append(out.governors, served[Governor]{element.name, impl})
				servedAny = true
			}
		case Port{Reads: MaterialRequest, Produces: ProductEnvironment}:
			if impl, ok := element.impl.(EnvironmentInject); ok {
				out.injects = append(out.injects, served[EnvironmentInject]{element.name, impl})
				servedAny = true
			}
		case Port{Reads: MaterialFact, Produces: ProductObservation}:
			if impl, ok := element.impl.(PostCommitObserver); ok {
				out.observers = append(out.observers, served[PostCommitObserver]{element.name, impl})
				servedAny = true
			}
		case Port{Reads: MaterialFact, Produces: ProductView}:
			if impl, ok := element.impl.(FinalPresenter); ok {
				out.presenters = append(out.presenters, served[FinalPresenter]{element.name, impl})
				servedAny = true
			}
		case Port{Reads: MaterialFact, Produces: ProductCompression}:
			if impl, ok := element.impl.(CompressTrigger); ok {
				out.triggers = append(out.triggers, served[CompressTrigger]{element.name, impl})
				servedAny = true
			}
		}
		if !servedAny {
			return Pipeline{}, fmt.Errorf("plugin: %q declares the port %s but implements no narrow interface of that port", element.name, element.port)
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

// Before runs the one producing pass over one tool call: every call->call face
// corrects a private copy of the call in pipeline order, then every
// call->ruling face grades the transformed call, and the pass reports the final
// call beside the strongest grade. The pass runs exactly once per call — before
// the call is admitted and before its side effect — and is never run again when
// a recorded fact is replayed. The call the model recorded is left
// byte-identical: the transform works on its own copy.
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
		observePhase(PhaseProduce, faceTransform, Port{Reads: MaterialCall, Produces: ProductCall}, face.name, phaseResult(err), "call="+call.ID, started)
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
		observePhase(PhaseProduce, faceRisk, Port{Reads: MaterialCall, Produces: ProductRuling}, face.name, phaseGrade(graded), "call="+call.ID, started)
		if graded.Rank() > risk.Rank() {
			risk = graded
		}
	}
	return CallPass{Final: final, Risk: risk}, nil
}

// TriggerCompress runs the consuming pass of one filed compress request: every
// fact->compression face is started with the request in pipeline order, so the
// compression a producer judged due is the one the wired faces deliver. The pass
// starts the traceable mapping chain and answers no product of its own: the
// products the curation mints downstream are what traces back to the request.
// The request is dispatched as the log filed it, and the consumer retires it
// afterwards — never here — so a request whose compression failed stays pending.
//
// A face that answers an error refuses the pass at that face by name: no later
// face runs, and the caller answers with the refusal instead of counting the
// request as compressed. A pipeline carrying no compression face answers nil, so
// a consumer wired without one refuses nothing of the request it read.
func (p Pipeline) TriggerCompress(ctx context.Context, request event.CompressRequest) error {
	for _, face := range p.triggers {
		started := time.Now()
		err := face.impl.TriggerCompress(ctx, request)
		observePhase(PhaseConsume, faceTrigger, Port{Reads: MaterialFact, Produces: ProductCompression}, face.name, phaseResult(err), "request="+string(request.ID), started)
		if err != nil {
			return fmt.Errorf("plugin: compress trigger %s: %w", face.name, err)
		}
	}
	return nil
}

// InjectEnvironment runs the producing pass of one request assembly: every
// request->environment face renders its blocks in pipeline order, and the pass
// answers every block that carried text, in the order their faces are declared.
// One element of the answer is one product of one face: the blocks of a face
// stay separate products, and a face answering no text contributes no block, so
// a pipeline carrying no environment face answers none at all. Every block is
// rendered for the one request the pass was dispatched with, so each block
// traces back to the one stated source.
//
// A face that answers an error refuses the pass at that face by name: no later
// face runs and the assembly answers with the refusal instead of carrying a
// block it could not render.
func (p Pipeline) InjectEnvironment(ctx context.Context, request EnvironmentRequest) ([]string, error) {
	var blocks []string
	for _, face := range p.injects {
		started := time.Now()
		texts, err := face.impl.InjectEnvironment(ctx, request)
		observePhase(PhaseProduce, faceInject, Port{Reads: MaterialRequest, Produces: ProductEnvironment}, face.name, phaseResult(err), "agent="+string(request.Agent), started)
		if err != nil {
			return nil, fmt.Errorf("plugin: environment inject %s: %w", face.name, err)
		}
		for _, text := range texts {
			if text != "" {
				blocks = append(blocks, text)
			}
		}
	}
	return blocks, nil
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

// portOfPhase reports the port one route word names. A value that is neither a
// route word nor a phase of the flow is not declared.
func portOfPhase(phase Phase) (Port, bool) {
	switch phase {
	case PhaseCallTransform:
		return Port{Reads: MaterialCall, Produces: ProductCall}, true
	case PhaseRiskPolicy:
		return Port{Reads: MaterialCall, Produces: ProductRuling}, true
	case PhasePostCommitObserve:
		return Port{Reads: MaterialFact, Produces: ProductObservation}, true
	case PhaseFinalPresentation:
		return Port{Reads: MaterialFact, Produces: ProductView}, true
	case PhaseCompressTrigger:
		return Port{Reads: MaterialFact, Produces: ProductCompression}, true
	case PhaseEnvironmentInject:
		return Port{Reads: MaterialRequest, Produces: ProductEnvironment}, true
	default:
		return Port{}, false
	}
}

// phaseName reports the declared name of one phase or route word for a refusal
// message and one observation line.
func phaseName(phase Phase) string {
	switch phase {
	case PhaseProduce:
		return "produce"
	case PhaseConsume:
		return "consume"
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

// faceTransform names the call->call face in one observation line; the rest
// name the one face of their own port the same way. The face is the narrow
// interface the dispatched element was held to, not the route word it
// registered under.
const (
	faceTransform = "transform"
	faceRisk      = "risk"
	faceObserve   = "observe"
	facePresent   = "present"
	faceTrigger   = "trigger"
	faceInject    = "inject"
)

// observePhase renders one line of one executed phase face at DEBUG: the phase,
// the face, the plugin that served it, the port it declared, the face's own
// result, the elapsed time and the identity the face was dispatched for. The
// line carries no call arguments and no fact content.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a refused pass.
// MUST render after the face returned, and never stand a second pass outcome of its own.
// MUST NOT render call arguments, tool output or fact content.
func observePhase(phase Phase, face string, port Port, name, result, subject string, started time.Time) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Debugf("tagent: plugin phase phase=%s face=%s plugin=%s port=%s result=%s elapsed=%s %s",
		phaseName(phase), face, name, port, result, time.Since(started).Round(time.Microsecond), subject)
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
