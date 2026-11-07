package assembly

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/features/telemetry"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/search"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
	mcpgw "github.com/SpellingDragon/tagent/tool/mcp"
	"github.com/SpellingDragon/tagent/tool/recall"
	"github.com/SpellingDragon/tagent/tool/subagent"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// deliver accepts one notification of an admitted task into the mailbox of the
// site its frozen routing names and reports the delivery-layer outcome. The
// route is registered with the state owner first, so the acceptance lands in
// the owner's own bookkeeping instead of a copy of it.
//
// A terminal notification is also the one fact the settle face of the host
// reads: the delivery commits that fact beside the acceptance, so the turn that
// nobody drives — the delegation settle whose admitting turn already ended —
// always has a fact to fire on. The fact carries the terminal kind under the
// settle-lineage meta key and is the only fact family that does, so a wrapped
// tool's own settle, which the live invocation reads through its backflow,
// never fires a second turn.
//
// - Only a target that refused the notification is a rejection: the refusal is the consumption result of its notification, and the ledger keeps it as the feedback fact it is.
// - A route the mailbox cannot open — an address this run never established, an instance another one replaced — is not a refusal by anyone: the notification stands undelivered, answers pending, and the reason is recorded beside it for the next attempt.
// - A terminal notification whose settle fact the storage path refused is none of those either: the acceptance reports it pending beside the refusal, so the obligation stands for the next offer and no reader is told a fact landed that no storage holds. No offer is retained beside the delivery: a storage path that recovers is read through the flush the close offers on its own drain path, never by a window or a poll of the delivery.
func (r *Run) deliver(ctx context.Context, envelope task.Envelope) (task.DeliveryAcceptance, error) {
	if err := r.store.TrackDelivery(ctx, envelope.Notice.TaskID, task.HandoffTarget{
		Target:         envelope.Target,
		TargetInstance: envelope.TargetInstance,
	}); err != nil {
		return task.DeliveryPending, err
	}
	if err := r.store.AcceptNotice(ctx, envelope.Notice); err != nil {
		if errors.Is(err, state.ErrDeliveryRefused) {
			return task.DeliveryRejected, err
		}
		return task.DeliveryPending, err
	}
	if err := r.observeSettleDelivery(ctx, envelope); err != nil {
		return task.DeliveryPending, err
	}
	return task.DeliveryAccepted, nil
}

// settleFactLanded reports whether the live ledger of this run holds one fact
// identity, which is the observable a second commit of the same family reads
// before writing again.
func (r *Run) settleFactLanded(key event.EventID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, held := r.facts[key]
	return held
}

// observeSettleDelivery commits the delivery fact of one terminal notification
// and runs its After pass, so the settle that no live invocation reads is
// observed exactly as every other committed fact of this run is.
//
// The commit participates in the acceptance the delivery reports: a refusal is
// answered to the caller, which leaves the delivery pending beside it, so the
// notification keeps its obligation and a later offer commits the fact then. A
// run beside no published pipeline still commits the fact and reports the
// missing face as a gap, and a delivery that already landed the fact commits no
// second one.
func (r *Run) observeSettleDelivery(ctx context.Context, envelope task.Envelope) error {
	notice := envelope.Notice
	switch notice.Kind {
	case task.NoticeSettled, task.NoticeFailed, task.NoticeCancelled:
	default:
		return nil
	}
	addr := envelope.Target
	if !addr.IsComplete() {
		return nil
	}
	fact := event.Event{
		Key:       settleDeliveryKey(notice),
		Type:      event.TypeTaskSettled,
		Timestamp: time.Now(),
		Meta: map[string]string{
			event.MetaKeySettleTriggerSource: string(notice.Kind),
			event.MetaKeyEventSummary:        string(notice.Kind),
			event.MetaKeyNoticeKind:          string(notice.Kind),
			event.MetaKeyTaskID:              string(notice.TaskID),
		},
		Message:    model.Message{Role: model.RoleUser, Content: settleNoticeText(notice)},
		Owner:      addr,
		Source:     addr,
		Target:     addr,
		TaskID:     notice.TaskID,
		InstanceID: r.instance,
	}
	if frame, ok := task.CallFrameFrom(ctx); ok {
		fact.InvocationID = frame.Identity.InvocationID
	} else if r.delegations != nil {
		if owner, ok := r.delegations.OwnerOf(notice.TaskID); ok {
			fact.InvocationID = owner
		}
	}
	r.mu.Lock()
	_, held := r.facts[fact.Key]
	r.mu.Unlock()
	if held {
		return nil
	}
	key, err := r.commitFactChecked(ctx, fact)
	if err != nil || key == "" {
		if err == nil {
			err = errors.New("tagent: the commit reported no identity")
		}
		return fmt.Errorf("tagent: the settle of %s at %s did not commit: %w", notice.TaskID, addr.AgentID, err)
	}
	pipeline, err := r.factPipeline(addr.AgentID)
	if err != nil {
		assemblyWarn("tagent: the settle of %s at %s committed beside no pipeline: %v", notice.TaskID, addr.AgentID, err)
		return nil
	}
	r.takePresentation(pipeline.After(ctx, key))
	return nil
}

// settleDeliveryKey reports the canonical identity the delivery fact of one
// terminal notification commits under: a retried delivery keeps its identity,
// so the chain carries one settle for one notification.
func settleDeliveryKey(notice task.Notice) event.EventID {
	return event.EventID(fmt.Sprintf("settle-%x", notice.FactKey()))
}

// RecallReads returns the recall read faces this run wires beside the retrieval
// face Run.Recall serves: the durable query and parent reader, the scoped
// original store of that tier and the embedding binding of this assembly. A run assembled beside no storage tier wires no table and no
// original store, and a run beside no embedder wires no embedding, so a caller
// binds exactly the faces that answer and leaves the rest as the named refusal
// the recall surface already states.
func (r *Run) RecallReads() recall.Reads {
	r.mu.Lock()
	defer r.mu.Unlock()
	reads := recall.Reads{Embedder: r.embedder}
	if r.retrieval != nil {
		reads.Table = r.retrieval.source
		reads.Trace = r.retrieval.source
	}
	if r.tier != nil {
		if scoped, ok := r.tier.Originals.(ledger.ScopedOriginalStore); ok {
			reads.Originals = scoped
		}
	}
	return reads
}

// errAgentTargetUndeclared reports an agent reference whose declared target the
// same declaration does not declare: the declaration is the authorization, so a
// reference to an undeclared agent is refused by name instead of resolving to a
// tool the reference never granted.
var errAgentTargetUndeclared = errors.New("tagent: the agent reference names no declared agent")

// errAgentReferenceCycle reports an agent reference that leads back to an agent
// already being assembled: the target's own tool surface is the declaration's,
// so a cycle would never finish resolving.
var errAgentReferenceCycle = errors.New("tagent: the agent reference closes a cycle")

// leaseObserverOf reports the replacement consumer mounted for one agent, or
// nil beside none. A typed nil observer is never returned as a non-nil
// interface: the pipeline would otherwise carry a face that observes nothing
// under a name the run never mounted.
func (r *Run) leaseObserverOf(agentID event.AgentID) plugin.Instance {
	r.mu.Lock()
	defer r.mu.Unlock()
	observer, mounted := r.leaseObservers[agentID]
	if !mounted || observer == nil {
		return nil
	}
	return observer
}

// suppliers adapts the contributed native pieces of every agent onto the
// construction chain prepare drives: every native callable tool of every
// family is registered through the task wrapper of this run before the binding
// is published, so a call is admitted and settled through the task face and
// its result backflows to the invocation that issued it. The contributed tools
// are handed over native: the admission wrap happens once per family in the
// registration step beside the file and skill families. A leased agent's model
// is the lease's stable handle: every request borrows the version in effect
// when it begins, and an installed version serves the requests that begin after
// it without a second binding generation.
func (r *Run) suppliers(decl *config.Declaration, agents map[event.AgentID]NativeAgent) map[event.AgentID]supplier {
	out := make(map[event.AgentID]supplier, len(agents))
	for id, native := range agents {
		contributed := native
		out[id] = supplier{
			model: func(_ context.Context, spec config.AgentSpec) (model.Model, func(context.Context) error, error) {
				release := func(context.Context) error { return nil }
				if held := r.leaseOf(id); held != nil {
					return withHostModelParams(rl.NewLeaseModel(held.lease), spec), release, nil
				}
				if contributed.Model == nil {
					return nil, nil, fmt.Errorf("tagent: agent %q contributes no model", id)
				}
				return withHostModelParams(contributed.Model, spec), release, nil
			},
			tools: func(ctx context.Context, spec config.AgentSpec) ([]trpctool.Tool, trpctool.ToolSet, func(context.Context) error, error) {
				native := make([]trpctool.Tool, 0, len(contributed.Tools)+len(spec.Tools))
				for _, tool := range contributed.Tools {
					native = append(native, tool)
				}
				presets, err := r.presetTools(ctx, decl, agents, id, spec, native, nil)
				if err != nil {
					return nil, nil, nil, err
				}
				native = append(native, presets...)
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
			plugins: func(_ context.Context, spec config.AgentSpec) ([]plugin.Instance, error) {
				elements := append([]plugin.Instance(nil), contributed.Plugins...)
				elements = append(elements, r.plugins...)
				if observer := r.leaseObserverOf(event.AgentID(spec.ID)); observer != nil {
					elements = append(elements, observer)
				}
				if spec.MaxTokens > 0 && r.ageFold != nil {
					elements = append(elements, r.ageFold)
				}
				if r.observer != nil {
					elements = append(elements,
						observePlugin{observer: telemetry.NewCommitObserver(runFacts{run: r}, r.observer)})
				}
				return elements, nil
			},
			wrap:              r.wrap,
			reflectionSession: contributed.reflectionSession,
			reflectionOnly:    append([]string(nil), contributed.reflectionOnly...),
		}
	}
	return out
}

// presetTools resolves the references one declaration names into the tools they
// reference, beside the tools the host contributed: the knowledge and recall
// references resolve through the preset face of the sub-agent package, the
// call_subagent reference resolves through the delegation face of this run, and
// an agent reference resolves into the target tool of the declared agent it
// names. A preset tool the host already contributed under the same name is
// dropped, so the host contribution stays the one the model sees.
//
// The chain names the agents already being assembled above this one: a target
// whose own declaration references an agent of the chain is refused by name, so
// the recursive resolution of a declaration's own tool surface always ends.
func (r *Run) presetTools(ctx context.Context, decl *config.Declaration, agents map[event.AgentID]NativeAgent, id event.AgentID, spec config.AgentSpec, native []trpctool.Tool, chain []event.AgentID) ([]trpctool.Tool, error) {
	refs := make([]config.ToolRef, 0, len(spec.Tools))
	targets := make([]config.ToolRef, 0, len(spec.Tools))
	faces := make([]trpctool.Tool, 0, 3)
	delegate := false
	for _, ref := range spec.Tools {
		if agentRef(ref) != "" {
			targets = append(targets, ref)
			continue
		}
		switch ref.Name {
		case subagent.CallSubagentRef:
			delegate = true
			continue
		case tagentexec.PresetName:
			faces = append(faces, tagentexec.NewPreset(r.execManager, monitorConfigOf(spec), tagentexec.WithRunOwner(r.execOwner))...)
			continue
		case mcpgw.ToolNameMCPCall, mcpgw.ToolNameMCPDiscover:
			if r.mcpGateways == nil {
				return nil, mcpgw.ErrRegistryMissing
			}
			if ref.Name == mcpgw.ToolNameMCPCall {
				faces = append(faces, r.mcpGateways.CallTool())
			} else {
				faces = append(faces, r.mcpGateways.DiscoverTool())
			}
			continue
		}
		refs = append(refs, ref)
	}
	presets, err := subagent.ResolvePresets(ctx, refs, subagent.PresetOptions{Recall: r.recall})
	if err != nil {
		return nil, err
	}
	for _, ref := range targets {
		target, err := r.agentTarget(ctx, decl, agents, id, ref, chain)
		if err != nil {
			return nil, err
		}
		presets = append(presets, target)
	}
	presets = append(presets, faces...)
	assembled := append(append([]trpctool.Tool(nil), native...), presets...)
	if delegate {
		delegation, err := r.presetSubagent(ctx, agents, id, spec, assembled)
		if err != nil {
			return nil, err
		}
		presets = append(presets, delegation)
	}
	declared := make(map[string]bool, len(native))
	for _, tool := range native {
		if declaration := tool.Declaration(); declaration != nil {
			declared[declaration.Name] = true
		}
	}
	out := make([]trpctool.Tool, 0, len(presets))
	for _, tool := range presets {
		name := ""
		if declaration := tool.Declaration(); declaration != nil {
			name = declaration.Name
		}
		if name != "" && declared[name] {
			continue
		}
		if name != "" {
			declared[name] = true
		}
		out = append(out, tool)
	}
	return out, nil
}

// monitorConfigOf maps the monitor rhythm one declaration fixes onto the
// execution face: a duration the declaration leaves at zero keeps the framework
// default of that field, so a declaration naming no rhythm runs exactly as an
// undeclared one did.
func monitorConfigOf(spec config.AgentSpec) tagentexec.MonitorConfig {
	monitor := spec.Exec.Monitor
	return tagentexec.MonitorConfigFromBase(
		monitor.Interval,
		monitor.StableDuration,
		monitor.InteractiveStableDuration,
		monitor.FakeDeadDuration,
	)
}

// agentRef reports the declared agent one reference authorizes, or the empty
// string when the reference names no agent. The parameter is what separates an
// agent reference from a preset reference of the same name, so a declaration
// that references the agent named `knowledge` is never read as the knowledge
// preset.
func agentRef(ref config.ToolRef) string {
	return strings.TrimSpace(ref.Params[subagent.ParamAgent])
}

// agentTarget resolves one agent reference into the target tool of the declared
// agent it names: the target's own declaration is the authorization and the
// target's own references are its tool surface, so a call reaches exactly the
// agent the declaration named and exactly the tools that agent declares. A
// reference to an agent the declaration does not declare, and a reference that
// leads back into the chain already being assembled, are refused by name.
func (r *Run) agentTarget(ctx context.Context, decl *config.Declaration, agents map[event.AgentID]NativeAgent, caller event.AgentID, ref config.ToolRef, chain []event.AgentID) (*subagent.Target, error) {
	target := agentRef(ref)
	if target == "" {
		return nil, fmt.Errorf("%w: %q", errAgentTargetUndeclared, ref.Name)
	}
	for _, seen := range append(append([]event.AgentID(nil), chain...), caller) {
		if string(seen) == target {
			return nil, fmt.Errorf("%w: %q referenced by %q", errAgentReferenceCycle, target, caller)
		}
	}
	spec := specOfLocked(decl, event.AgentID(target))
	if spec.ID == "" {
		return nil, fmt.Errorf("%w: %q referenced by %q", errAgentTargetUndeclared, target, caller)
	}
	surface, err := r.agentSurface(ctx, decl, agents, event.AgentID(target), append(append([]event.AgentID(nil), chain...), caller))
	if err != nil {
		return nil, err
	}
	assembly, err := r.presetAssembly(ctx, event.AgentID(target), spec, surface)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		name = target
	}
	return subagent.NewTarget(subagent.TargetOptions{
		Name:            name,
		Description:     targetDescription(name),
		DescriptionFile: strings.TrimSpace(ref.Params[subagent.ParamDescriptionFile]),
		Assembly:        assembly,
		Builder:         r.surfaceBuilder(agents, spec, surface),
		Delegation:      r.delegations,
		Bindings:        r.bindings,
		EventParams:     splitRefParam(ref.Params[subagent.ParamEventParams]),
	})
}

// agentSurface resolves the tool surface one declared agent's own declaration
// grants it: the native tools this process contributes for it beside the
// references its own declaration names, each agent reference there resolved
// recursively under the chain. The caller's assembled set never enters it, so
// the surface is the target's own.
func (r *Run) agentSurface(ctx context.Context, decl *config.Declaration, agents map[event.AgentID]NativeAgent, id event.AgentID, chain []event.AgentID) ([]trpctool.Tool, error) {
	spec := specOfLocked(decl, id)
	if spec.ID == "" {
		return nil, fmt.Errorf("%w: %q", errAgentTargetUndeclared, id)
	}
	native := make([]trpctool.Tool, 0, len(agents[id].Tools)+len(spec.Tools))
	for _, tool := range agents[id].Tools {
		native = append(native, tool)
	}
	presets, err := r.presetTools(ctx, decl, agents, id, spec, native, chain)
	if err != nil {
		return nil, err
	}
	return append(native, presets...), nil
}

// surfaceBuilder is the builder a target tool runs its delegated turn on: the
// model the target's declaration names, the tools of that target's own surface,
// and the skill repository its declared skill files carry. Every reference
// resolves against the declaration of the target, never against the caller's
// assembled set.
func (r *Run) surfaceBuilder(agents map[event.AgentID]NativeAgent, spec config.AgentSpec, surface []trpctool.Tool) subagent.Builder {
	byName := make(map[string]trpctool.Tool, len(surface))
	for _, tool := range surface {
		if declaration := tool.Declaration(); declaration != nil && declaration.Name != "" {
			byName[declaration.Name] = tool
		}
	}
	builder := subagent.Builder{
		Facts:   r.CommittedEvent,
		Capture: r.captureScope,
		Model: func(_ context.Context, name string) (model.Model, error) {
			if name == "" {
				return nil, subagent.ErrModelMissing
			}
			for _, candidate := range agents {
				if candidate.Model != nil && candidate.Model.Info().Name == name {
					return delegatedModel(candidate.Model, name, spec), nil
				}
			}
			return nil, fmt.Errorf("%w: %q", subagent.ErrModelUnresolved, name)
		},
		Tools: func(_ context.Context, names []string) ([]trpctool.Tool, error) {
			out := make([]trpctool.Tool, 0, len(names))
			for _, name := range names {
				tool, found := byName[name]
				if !found {
					return nil, fmt.Errorf("%w: tool reference %q", subagent.ErrResolverMissing, name)
				}
				out = append(out, tool)
			}
			return out, nil
		},
	}
	if roots := skillRoots(spec.Files); len(roots) > 0 {
		builder.Skills = func(context.Context, []string) (skill.Repository, error) {
			repository, err := skill.NewFSRepository(roots...)
			if err != nil {
				return nil, fmt.Errorf("tagent: open the skill repository of agent %q: %w", spec.ID, err)
			}
			return repository, nil
		}
	}
	return builder
}

// errDelegatedBuilderUnbacked reports a delegated round asked to rebuild its
// builder beside no published declaration or no construction contribution, so
// the model and tool references the declaration names cannot be resolved.
var errDelegatedBuilderUnbacked = errors.New("assembly: the delegated round carries no live declaration to rebuild its builder from")

// delegatedBuilder rebuilds the builder one delegated round is served through
// after a restart: the caller that issued the delegation is the agent whose
// assembled surface resolved the round's tools, so its native contribution and
// declared surface are assembled again from the generation in effect and handed
// to the same builder shape the delegating tool runs on. The round keeps the
// declaration the chain recorded; only the process-local model and tool
// resolution is rebuilt.
//
// The read is the construction the publisher already runs for that agent, so no
// second tool factory sits beside the real one: a tool the declaration does not
// grant is refused by the same resolver that would refuse it at the next
// publication.
func (r *Run) delegatedBuilder(ctx context.Context, issuer event.AgentID) (subagent.Builder, error) {
	r.mu.Lock()
	decl := r.current.Plan.Declaration
	agents := r.natives
	r.mu.Unlock()
	if decl == nil || len(agents) == 0 {
		return subagent.Builder{}, fmt.Errorf("%w: caller %q", errDelegatedBuilderUnbacked, issuer)
	}
	spec := specOfLocked(decl, issuer)
	if spec.ID == "" {
		return subagent.Builder{}, fmt.Errorf("%w: caller %q", errDelegatedBuilderUnbacked, issuer)
	}
	contributed := agents[issuer].Tools
	native := make([]trpctool.Tool, 0, len(contributed))
	for _, tool := range contributed {
		native = append(native, tool)
	}
	presets, err := r.presetTools(ctx, decl, agents, issuer, spec, native, nil)
	if err != nil {
		return subagent.Builder{}, err
	}
	surface := append(append([]trpctool.Tool(nil), native...), presets...)
	return r.surfaceBuilder(agents, spec, surface), nil
}

// delegatedModel wraps one resolved model with the sampling the authorizing
// declaration names, when the model is the one that declaration names: a
// delegated turn then carries the declared sampling exactly as a turn of the
// agent itself does. A model the authorizing declaration does not name keeps its
// own path, so the declared sampling never leaks onto another agent's model.
func delegatedModel(candidate model.Model, name string, spec config.AgentSpec) model.Model {
	if name != spec.Model {
		return candidate
	}
	return withHostModelParams(candidate, spec)
}

// targetDescription is the declared purpose of one target tool: the agent's own
// prompt text is not a tool description, so the description states the agent it
// delegates to and the surface that agent declares.
func targetDescription(name string) string {
	return fmt.Sprintf("delegate to the declared %q agent and hand it one request; the turn runs on that agent's own declaration, not on the tools of this one", name)
}

// splitRefParam splits one comma-separated reference parameter into its trimmed
// entries, in declaration order.
func splitRefParam(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := make([]string, 0, 2)
	for _, entry := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// presetSubagent builds the call_subagent tool one declaration references: the
// parent assembly is this agent's own declaration — its identity, its model, the
// tools assembled beside it and the prompt its files carry — and the builder
// resolves the model, skill and tool references of a delegated assembly against
// the same pieces. The delegation and borrowing faces are this run's own, so a
// delegated call is admitted as a task of this run, and the continuation account
// its rounds are parked in is the one account this run shares among its tools:
// the first tool mints it and every later tool is built beside it.
func (r *Run) presetSubagent(ctx context.Context, agents map[event.AgentID]NativeAgent, id event.AgentID, spec config.AgentSpec, assembled []trpctool.Tool) (*subagent.Tool, error) {
	parent, err := r.presetAssembly(ctx, id, spec, assembled)
	if err != nil {
		return nil, err
	}
	builder := subagent.Builder{
		Facts:   r.CommittedEvent,
		Capture: r.captureScope,
		Model: func(_ context.Context, name string) (model.Model, error) {
			if name == "" {
				return nil, subagent.ErrModelMissing
			}
			for _, candidate := range agents {
				if candidate.Model != nil && candidate.Model.Info().Name == name {
					return delegatedModel(candidate.Model, name, spec), nil
				}
			}
			return nil, fmt.Errorf("%w: %q", subagent.ErrModelUnresolved, name)
		},
		Tools: func(_ context.Context, names []string) ([]trpctool.Tool, error) {
			byName := make(map[string]trpctool.Tool, len(assembled))
			for _, tool := range assembled {
				if declaration := tool.Declaration(); declaration != nil && declaration.Name != "" {
					byName[declaration.Name] = tool
				}
			}
			out := make([]trpctool.Tool, 0, len(names))
			for _, name := range names {
				tool, found := byName[name]
				if !found {
					return nil, fmt.Errorf("%w: tool reference %q", subagent.ErrResolverMissing, name)
				}
				out = append(out, tool)
			}
			return out, nil
		},
	}
	if roots := skillRoots(spec.Files); len(roots) > 0 {
		repository, err := skill.NewFSRepository(roots...)
		if err != nil {
			return nil, fmt.Errorf("tagent: open the skill repository of agent %q: %w", id, err)
		}
		builder.Skills = func(context.Context, []string) (skill.Repository, error) { return repository, nil }
	}
	r.mu.Lock()
	tool, shared, err := subagent.NewTool(subagent.ToolOptions{
		Parent:     parent,
		Builder:    builder,
		Delegation: r.delegations,
		Bindings:   r.bindings,
	}, r.continuations)
	if err != nil {
		r.mu.Unlock()
		return nil, err
	}
	r.continuations = shared
	r.mu.Unlock()
	return tool, nil
}

// presetAssembly resolves the assembly a delegated declaration inherits from:
// the declaration's own identity, model and assembled tool names beside the
// prompt its prompt files carry. The prompt text is read through the file source
// of the agent the files belong to, so the instruction a delegated declaration
// hands over is the content the chain carries, never a second read beside it.
func (r *Run) presetAssembly(ctx context.Context, id event.AgentID, spec config.AgentSpec, assembled []trpctool.Tool) (*subagent.Assembly, error) {
	prompt, err := r.presetPrompt(ctx, id, spec)
	if err != nil {
		return nil, err
	}
	tools := make([]string, 0, len(assembled))
	for _, tool := range assembled {
		if declaration := tool.Declaration(); declaration != nil && declaration.Name != "" {
			tools = append(tools, declaration.Name)
		}
	}
	return &subagent.Assembly{
		Definition: subagent.Definition{
			Name:  spec.ID,
			Model: spec.Model,
			Tools: tools,
		},
		Instruction: prompt,
	}, nil
}

// presetPrompt reads the prompt text the declared prompt files carry, in the
// declaration order, through the file source of the agent the files belong to:
// the read fixes the content version and files the file change fact when the
// chain does not carry it yet, so the instruction a delegated assembly hands
// over is the content the fact projects. Reading the body around the chain would
// be the third path the file dimension exists to close. The fact states the
// content the assembly read, never that a generation took it: the file dimension
// claims no publication, so a candidate that does not take effect leaves the
// observation it made and nothing else.
func (r *Run) presetPrompt(ctx context.Context, id event.AgentID, spec config.AgentSpec) (string, error) {
	fixer, err := r.delegatedFileFixer(id, spec.Files)
	if err != nil {
		return "", err
	}
	snapshot, err := fixer.fixate(ctx, state.PromptSource{Files: spec.Files})
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(snapshot.Files))
	for _, file := range snapshot.Files {
		if file.Ref.Kind != config.FileKindPrompt {
			continue
		}
		if content := strings.TrimSpace(file.Content); content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n"), nil
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
	} else if approve == nil && r.enforcement == governance.EnforcementStrict {
		approve = denyWithoutApprover
	}
	return &admissionGate{run: r, inner: r.wrappedTool(tool, approve)}
}

// wrappedTool builds the registration wrapper of one native callable tool and
// files its waiting table beside the waits of this run, so the close reads the
// holds of this process as the waits a ruling can still rejoin.
func (r *Run) wrappedTool(tool trpctool.CallableTool, approve task.Approver) *task.Wrapper {
	wrapped := task.Wrap(r.manager, &toolProducer{run: r, inner: tool},
		task.WithResultRecorder(r.record), task.WithMailbox(r.sink), task.WithApprover(approve))
	r.hold(wrapped.Holds())
	return wrapped
}

// hold files one waiting table of the approval channel this run parks calls
// through, so the cut of a close reads every hold of the process.
func (r *Run) hold(book *task.HoldBook) {
	if book == nil {
		return
	}
	r.mu.Lock()
	r.holds = append(r.holds, book)
	r.mu.Unlock()
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

// toolProducer registers the production of one wrapped call beside the writer
// ledger of the run: the registration happens where the production actually
// runs, on the detached execution the task wrapper drives, where the call frame
// of the admission still travels on the context. The registration names the
// call, never a task identity: a call in flight is a writer of the run, and its
// stop is the one the close waits for.
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

// Call registers the tool production beside the call it runs, runs the tool, and
// confirms the production's stop when the tool returned. The stop is confirmed
// by the tool's own return alone: a cancelled request that leaves the detached
// execution running is never read as one.
func (p *toolProducer) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	stop := p.run.writers.Begin(toolWriterName(ctx))
	defer stop()
	return p.inner.Call(ctx, jsonArgs)
}

// toolWriterName names one tool production by the call it runs: the call
// identity the frame carries, the dedup key of the frame beside no call identity,
// or the bare production name beside no frame at all.
func toolWriterName(ctx context.Context) string {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return "call"
	}
	if frame.Identity.CallID != "" {
		return "call:" + frame.Identity.CallID
	}
	if key := frame.Identity.DedupKey(); key != "" {
		return "call:" + string(key)
	}
	return "call"
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

// RecallPort is the late-bound recall read face a host assembles its recall
// tools over: the tools are constructed before the run exists, so every face is
// bound to the assembled run once the assembly returns. A call before that
// binding answers the named refusal of the recall surface instead of an empty
// page that would read as "nothing was found".
//
// The port carries the read faces the tools consume and forwards each to the
// face the run wires: the retrieval port, the durable query source and the
// scoped original store of the storage tier. The parent reader is bound with
// the other faces after assembly, never held as a second relation graph.
type RecallPort struct {
	mu       sync.RWMutex
	searcher func(ctx context.Context, query search.Query) (search.Result, error)
	reads    recall.Reads
}

var _ search.Searcher = (*RecallPort)(nil)

var _ recall.Table = (*RecallPort)(nil)

var _ ledger.ScopedOriginalStore = (*RecallPort)(nil)

// NewRecallPort opens the port over the embedding binding the recall tools are
// built with. Every other face, including the parent reader, is attached by Bind.
func NewRecallPort(embedder memory.Embedder) *RecallPort {
	return &RecallPort{reads: recall.Reads{Embedder: embedder}}
}

// Bind attaches the read faces of the assembled run.
func (p *RecallPort) Bind(r *Run) {
	p.mu.Lock()
	p.searcher = r.recall
	p.reads = r.RecallReads()
	p.mu.Unlock()
}

// Search answers one retrieval through the bound face.
func (p *RecallPort) Search(ctx context.Context, query search.Query) (search.Result, error) {
	p.mu.RLock()
	face := p.searcher
	p.mu.RUnlock()
	if face == nil {
		return search.Result{}, recall.ErrRecallUnwired
	}
	return face(ctx, query)
}

// QueryEvents answers the event references one query matches through the bound
// durable query source.
func (p *RecallPort) QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	p.mu.RLock()
	table := p.reads.Table
	p.mu.RUnlock()
	if table == nil {
		return nil, fmt.Errorf("recall_query: %w", recall.ErrRecallUnwired)
	}
	return table.QueryEvents(ctx, opts)
}

// Event answers the reference of one event through the bound table.
func (p *RecallPort) Event(key event.EventID) (memory.EventReference, bool) {
	p.mu.RLock()
	table := p.reads.Table
	p.mu.RUnlock()
	if table == nil {
		return memory.EventReference{}, false
	}
	return table.Event(key)
}

// Reference answers the original reference one event carries through the bound
// table.
func (p *RecallPort) Reference(key event.EventID) (ledger.OriginalRef, bool) {
	p.mu.RLock()
	table := p.reads.Table
	p.mu.RUnlock()
	if table == nil {
		return ledger.OriginalRef{}, false
	}
	return table.Reference(key)
}

// GetScoped answers one original read through the bound scoped original store.
func (p *RecallPort) GetScoped(ctx context.Context, key event.EventID, scope ledger.ReadScope) ledger.RecallResult {
	p.mu.RLock()
	originals := p.reads.Originals
	p.mu.RUnlock()
	if originals == nil {
		return ledger.RecallResult{Key: key, Outcome: ledger.RecallFailed, Err: recall.ErrRecallUnwired}
	}
	return originals.GetScoped(ctx, key, scope)
}

// ReadSegmentScoped answers one segment read through the bound scoped original
// store.
func (p *RecallPort) ReadSegmentScoped(ctx context.Context, key event.EventID, scope ledger.ReadScope, req ledger.SegmentRequest) (ledger.SegmentResult, error) {
	p.mu.RLock()
	originals := p.reads.Originals
	p.mu.RUnlock()
	if originals == nil {
		return ledger.SegmentResult{}, fmt.Errorf("recall_get: %w", recall.ErrRecallUnwired)
	}
	return originals.ReadSegmentScoped(ctx, key, scope, req)
}

// Reads reports the read face this port wires for the recall tools: the
// retrieval port, the embedding binding and the table and original store of the
// bound run, beside the relation ledger the host holds. A face the deployment
// wires nothing for stays absent, so the tool that needs it refuses by name
// instead of answering an empty result.
func (p *RecallPort) Reads() recall.Reads {
	p.mu.RLock()
	defer p.mu.RUnlock()
	reads := p.reads
	reads.Searcher = p
	reads.Trace = p
	if reads.Table != nil {
		reads.Table = p
	}
	if reads.Originals != nil {
		reads.Originals = p
	}
	return reads
}

// GetParent forwards one parent read to the durable reader bound after assembly.
func (p *RecallPort) GetParent(key event.EventID) (event.EventID, error) {
	p.mu.RLock()
	trace := p.reads.Trace
	p.mu.RUnlock()
	if trace == nil {
		return "", fmt.Errorf("recall_trace: %w", recall.ErrRecallUnwired)
	}
	return trace.GetParent(key)
}

// ObservationFacts returns the read-only fact-chain face the scheduled
// reflection of this run judges on: the committed fact chain the run reads,
// carrying the lineage each fact was committed under.
func (r *Run) ObservationFacts() meditation.ObservedFacts {
	return runObservation{run: r}
}

// runObservation is the observation face of one reflection over the run.
type runObservation struct {
	run *Run
}

// QueryEvents answers one page over the fact chain the run observes.
func (o runObservation) QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	return o.run.ObservedEvents(ctx, opts)
}

// CommittedFact reports the fact one reference names.
func (o runObservation) CommittedFact(ctx context.Context, id event.EventID) (event.Event, error) {
	return o.run.CommittedEvent(ctx, id)
}

// ReflectionInput returns the injection face the scheduled reflection of this
// run drives its own loop session through: it commits the reflection input as a
// fact of the observing agent's own partition, drives that agent's loop session
// with it and reports what became of the turn.
func (r *Run) ReflectionInput() meditation.ReflectionInput {
	return &runReflectionInput{run: r}
}

// runReflectionInput is the injection face of one reflection over the run. The
// receipt is the only evidence the reflection turn ran, so an injection that
// errors reports no receipt and the watermark never moves.
type runReflectionInput struct {
	run *Run

	mu  sync.Mutex
	seq uint64
}

var _ meditation.ReflectionInput = (*runReflectionInput)(nil)

// InjectReflection commits one reflection input, drives its loop session and
// reports the outcome the turn reached: a completed terminal is a consumed
// reflection, and every other terminal is a deferral that leaves the watermark
// where it stands.
func (i *runReflectionInput) InjectReflection(ctx context.Context, evt event.Event) (meditation.ReflectionReceipt, error) {
	i.mu.Lock()
	i.seq++
	seq := i.seq
	i.mu.Unlock()
	invocation := event.InvocationID(fmt.Sprintf("%s#meditation-%d", evt.Owner.SessionID, seq))
	turn, key, err := i.run.InjectEvent(ctx, evt, invocation)
	if err != nil {
		return meditation.ReflectionReceipt{}, err
	}
	receipt := meditation.ReflectionReceipt{Key: key, At: evt.Timestamp}
	if turn.Terminal != model.KindCompleted {
		receipt.Outcome = meditation.ReflectionDeferred
		receipt.Reason = fmt.Sprintf("the reflection turn reached terminal %q instead of a completed one", turn.Terminal)
		return receipt, nil
	}
	receipt.Outcome = meditation.ReflectionConsumed
	return receipt, nil
}
