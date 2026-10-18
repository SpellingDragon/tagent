package tagent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// deliver accepts one notification of an admitted task into the mailbox of the
// site its frozen routing names and reports the delivery-layer outcome. The
// route is registered with the state owner first, so the acceptance lands in
// the owner's own bookkeeping instead of a copy of it.
func (r *Run) deliver(ctx context.Context, envelope task.NoticeEnvelope) (task.DeliveryAcceptance, error) {
	if err := r.store.TrackDelivery(ctx, envelope.Notice.TaskID, task.HandoffTarget{
		Target:         envelope.Target,
		TargetInstance: envelope.TargetInstance,
	}); err != nil {
		return task.DeliveryRejected, err
	}
	if err := r.store.AcceptNotice(ctx, envelope.Notice); err != nil {
		return task.DeliveryPending, err
	}
	return task.DeliveryAccepted, nil
}

// suppliers adapts the contributed native pieces of every agent onto the
// construction chain prepare drives: every native callable tool of every
// family is registered through the task wrapper of this run before the binding
// is published, so a call is admitted and settled through the task face and
// its result backflows to the invocation that issued it. The contributed tools
// are handed over native: the admission wrap happens once per family in the
// registration step beside the file and skill families.
func (r *Run) suppliers(agents map[event.AgentID]NativeAgent) map[event.AgentID]supplier {
	out := make(map[event.AgentID]supplier, len(agents))
	for id, native := range agents {
		contributed := native
		out[id] = supplier{
			model: func(context.Context, config.AgentSpec) (model.Model, func(context.Context) error, error) {
				if contributed.Model == nil {
					return nil, nil, fmt.Errorf("tagent: agent %q contributes no model", id)
				}
				return contributed.Model, func(context.Context) error { return nil }, nil
			},
			tools: func(context.Context, config.AgentSpec) ([]trpctool.Tool, trpctool.ToolSet, func(context.Context) error, error) {
				native := make([]trpctool.Tool, 0, len(contributed.Tools))
				for _, tool := range contributed.Tools {
					native = append(native, tool)
				}
				return native, nil, func(context.Context) error { return nil }, nil
			},
			prompt: func(_ context.Context, spec config.AgentSpec) (state.PromptSource, error) {
				fixer, err := newFileFixer(spec.Files)
				if err != nil {
					return state.PromptSource{}, err
				}
				r.rememberFixer(event.AgentID(spec.ID), fixer)
				return state.PromptSource{Files: spec.Files}, nil
			},
			plugins: func(context.Context, config.AgentSpec) ([]plugin.Plugin, error) {
				elements := contributed.Plugins
				if r.observer != nil {
					elements = append(append([]plugin.Plugin(nil), contributed.Plugins...),
						observePlugin{observer: plugin.NewCommitObserver(runFacts{run: r}, r.observer)})
				}
				return elements, nil
			},
			wrap: r.wrap,
		}
	}
	return out
}

// wrap is the admission wrap every native callable tool of this run is
// registered under: each call is refused by name once the close of this run has
// begun and otherwise admitted through the task manager beside the result
// recorder and the mailbox of this run, and ruled by the pre-execution decision
// the assembly was configured with. A policy attached to the assembly is that
// decision and rules every call over the Before pass it carries; beside no
// policy the approval decision the assembly was configured with rules. Under
// the strict enforcement grade a wrap beside neither refuses every call before
// any execution instead of admitting it for execution by default. The tool
// production of an admitted call is registered beside the task it runs behind,
// so the close of this run waits for its real stop instead of counting the
// detached execution's cancellation as one.
func (r *Run) wrap(tool trpctool.CallableTool) trpctool.Tool {
	approve := r.approver
	if r.policy != nil {
		approve = policyRuling(r.policy)
	} else if approve == nil && r.enforcement == plugin.EnforcementStrict {
		approve = denyWithoutApprover
	}
	return &admissionGate{run: r, inner: task.Wrap(r.manager, &toolProducer{run: r, inner: tool},
		task.WithResultRecorder(r.record), task.WithMailbox(r.sink), task.WithApprover(approve))}
}

// admissionGate is the refusal gate one registered tool's calls pass through
// before they reach the registration wrapper: once the close of this run has
// begun, a call is turned away by name beside no admission instead of opening a
// new obligation beside the drain.
type admissionGate struct {
	run   *Run
	inner trpctool.CallableTool
}

// The gate fronts the registration wrapper of one native tool.
var _ trpctool.CallableTool = (*admissionGate)(nil)

// Declaration returns the declaration of the wrapped tool unchanged.
func (g *admissionGate) Declaration() *trpctool.Declaration {
	return g.inner.Declaration()
}

// Call turns the call away by name once the close of this run has begun, and
// admits it through the registration wrapper otherwise.
func (g *admissionGate) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	if g.run.refusing() {
		return nil, fmt.Errorf("%w: the close of this run refuses new calls", agent.ErrShutdownRefused)
	}
	return g.inner.Call(ctx, jsonArgs)
}

// toolProduction is the stop confirmation of one wrapped call's tool
// production: the production runs on the detached execution the task wrapper
// drives beside the admission and confirms its stop only when the tool really
// returned. The detachment keeps the production running through the caller's
// cancellation, so a cancellation is never read as a stop here.
type toolProduction struct {
	mu      sync.Mutex
	stopped bool
}

// Stopped reports whether this tool production really stopped.
func (p *toolProduction) Stopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

// stop confirms the real stop of this tool production.
func (p *toolProduction) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
}

// toolProducer registers the production of one wrapped call beside the task
// identity the call was admitted as: the registration happens where the
// production actually runs, on the detached execution the task wrapper drives,
// where the call frame of the admission still travels on the context.
type toolProducer struct {
	run   *Run
	inner trpctool.CallableTool
}

// The producer wraps one native callable tool.
var _ trpctool.CallableTool = (*toolProducer)(nil)

// Declaration returns the declaration of the wrapped tool unchanged.
func (p *toolProducer) Declaration() *trpctool.Declaration {
	return p.inner.Declaration()
}

// Call registers the tool production beside the task the call was admitted as,
// runs the tool, and confirms the production's stop when the tool returned.
func (p *toolProducer) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	production := &toolProduction{}
	defer production.stop()
	if frame, ok := task.CallFrameFrom(ctx); ok {
		taskID, found := p.run.admission.Lookup(frame.Identity.DedupKey())
		if !found {
			taskID = event.TaskID(frame.Identity.InvocationID)
		}
		if taskID != "" {
			p.run.shutdown.Track(taskID)
			_ = p.run.shutdown.Watch(taskID, production.Stopped)
		}
	}
	return p.inner.Call(ctx, jsonArgs)
}

// policyRuling adapts one governance policy onto the approval face the task
// wrapper admits a call under: the ruling consumes the one Before pass the
// context carries — the final call beside the risk grade its risk faces
// rendered — and reports the class the wrapper admits the call under. The
// refusal is read first: a refused call is terminal and names its rule and
// reason in the paired result, a held call is a wait and never a denial, and a
// call driven without its Before pass is refused closed rather than ruled at a
// guess. The ruling happens at admission, exactly once per call, before any
// execution.
func policyRuling(governor plugin.Governor) task.Approver {
	return func(ctx context.Context, _ string, _ []byte) (task.Ruling, error) {
		pass, ok := plugin.CallPassFrom(ctx)
		if !ok {
			return task.Ruling{}, errors.New("tagent: the call carries no Before pass to rule over")
		}
		decision, err := governor.Judge(pass)
		if err != nil {
			return task.Ruling{}, err
		}
		reason := decision.Reason
		if decision.Rule != "" {
			reason = fmt.Sprintf("%s (rule: %s)", decision.Reason, decision.Rule)
		}
		switch {
		case decision.Refused():
			return task.Ruling{Kind: task.RulingDeny, Reason: reason}, nil
		case decision.Disposition == plugin.DispositionHold:
			return task.Ruling{Kind: task.RulingHold, Reason: reason}, nil
		default:
			return task.Ruling{Kind: task.RulingRun, Reason: reason}, nil
		}
	}
}

// denyWithoutApprover is the admission ruling of the strict enforcement grade
// beside no approver: the strict grade admits no call the governance never
// ruled on, so the call is refused before any execution instead of being
// admitted for execution by the default of the wrapper.
func denyWithoutApprover(context.Context, string, []byte) (task.Ruling, error) {
	return task.Ruling{Kind: task.RulingDeny, Reason: "strict enforcement admits no call without an approver"}, nil
}
