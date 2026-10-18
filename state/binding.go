package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/plugin"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

var (
	// ErrPlanMissing reports a StatePlan construction whose declaration plan
	// carries no declaration, so no agent list names the candidates.
	ErrPlanMissing = errors.New("state: the declaration plan carries no declaration")
	// ErrAgentBindingMissing reports a declared agent no candidate binding was
	// constructed for.
	ErrAgentBindingMissing = errors.New("state: no candidate binding was constructed for the declared agent")
	// ErrAgentBindingUnexpected reports a candidate binding for an agent the
	// declaration does not name.
	ErrAgentBindingUnexpected = errors.New("state: candidate binding names an agent the declaration does not")
	// ErrBindingIncomplete reports a candidate binding that carries no model, so
	// no request could ever execute on it.
	ErrBindingIncomplete = errors.New("state: the candidate binding carries no model")
	// ErrPlanEmpty reports a plan that carries no agent binding to hold.
	ErrPlanEmpty = errors.New("state: the candidate plan carries no agent binding")
	// ErrAgentUnknown reports a borrow of an agent no held binding serves.
	ErrAgentUnknown = errors.New("state: no binding is held for the named agent")
	// ErrHandleReleased reports the release of a binding handle already released.
	ErrHandleReleased = errors.New("state: the binding handle is already released")
	// ErrBindingRefUnknown reports a derived borrow through a binding reference
	// that names no borrow this holder issued: the zero reference, a reference
	// issued by another holder, or one carried over from a previous run.
	ErrBindingRefUnknown = errors.New("state: the binding reference names no borrow this holder issued")
	// ErrBindingGenerationReleased reports a derived borrow through a binding
	// reference whose initiating generation has already been released. The
	// named binding is gone, and the current generation is never lent in its
	// place. It is a named case of ErrBindingRefUnknown, so a caller matching
	// the refusal of any reference that resolves here matches it too.
	ErrBindingGenerationReleased = fmt.Errorf("%w: the generation it names is already released", ErrBindingRefUnknown)
)

// 契约: docs/wiki/platform/org-hot-reload.md#generations
//
// OrgPlan is the pure declaration plan one configuration change compiles to: the
// declaration it comes from, the baseline and fingerprint that identify it, the
// declaration-level change entries, and the next effective prompt.
//
// The plan carries the declaration itself rather than a second copy of the agent
// list, so the declaration stays the single source of truth for the agent graph.
// Its fingerprint is the canonical organization fingerprint of the declaration
// and covers only the declaration surface.
type OrgPlan struct {
	// Declaration is the pure declaration this plan was compiled from.
	Declaration *config.Declaration
	// Baseline is the version baseline the plan was compiled against. At the
	// commit point it is re-checked: a plan whose baseline was already advanced
	// is discarded instead of overwriting the newer publication.
	Baseline string
	// Fingerprint is the canonical organization fingerprint of the declaration.
	Fingerprint string
	// Changes lists the declaration-level change entries.
	Changes []PlanChange
	// NextPrompt is the next effective prompt the change produces.
	NextPrompt string
}

// PlanChangeKind classifies one declaration-level change entry as an added,
// updated or removed agent declaration.
type PlanChangeKind int

const (
	// PlanChangeAdded is an agent declaration the change adds.
	PlanChangeAdded PlanChangeKind = iota
	// PlanChangeUpdated is an agent declaration the change updates.
	PlanChangeUpdated
	// PlanChangeRemoved is an agent declaration the change removes.
	PlanChangeRemoved
)

// PlanChange is one declaration-level change entry: its kind, the agent it
// applies to, and a short summary of the declaration it carries.
type PlanChange struct {
	// Kind classifies the change.
	Kind PlanChangeKind
	// AgentID names the agent the change applies to.
	AgentID event.AgentID
	// Summary is a short readable summary of the declaration the change carries.
	Summary string
}

// StatePlan is what a configuration change prepares before it is admitted: the
// declaration plan together with the candidate live binding of every agent and
// the candidate resource registrations.
//
// The plan is admitted as a whole. A candidate that is refused or fails part way
// through construction releases only its own resources, so the shared face and
// the live bindings already in effect are never touched.
type StatePlan struct {
	// Plan is the declaration plan the candidates were constructed from.
	Plan OrgPlan
	// Agents holds the candidate live binding of each agent, keyed by agent
	// identity.
	Agents map[event.AgentID]AgentBinding
	// Leases lists the candidate resources that were registered while the
	// candidates were constructed, in construction order.
	Leases []LeaseRef
}

// LeaseRef is the registration of one candidate resource: the kind and key that
// name it and the reference the release path uses to release it once.
type LeaseRef struct {
	// Kind names the resource kind.
	Kind string
	// Key identifies the resource within its kind.
	Key string
	// Ref is the release reference of the resource.
	Ref string
}

// AgentBinding is the live binding of one agent: the native model, tools and
// ordered plugin pipeline a request executes with, together with the prompt
// source and the tool-set connection.
//
// The binding is constructed privately for a candidate. A request takes one
// generation of it and keeps that generation for its whole life; the binding is
// released when its user is done, not when a newer generation is published.
type AgentBinding struct {
	// Model is the native model the agent runs against.
	Model model.Model
	// Tools lists the native tools visible to the agent.
	Tools []trpctool.Tool
	// Plugins is the ordered plugin pipeline of the agent; the order is the
	// published order and is never reassembled mid-request.
	Plugins []plugin.Plugin
	// PromptSource is the prompt source of the agent.
	PromptSource PromptSource
	// Set is the tool-set connection of the agent; its Close is governed by a
	// resource lease rather than by the release of this binding.
	Set trpctool.ToolSet
}

// PromptSource is the prompt source of one agent: the structural binding that
// names which files the prompt is assembled from, in order, and the content
// version fixed for one request.
//
// The structural binding belongs to the published generation, while the content
// is read and fixed at the request boundary, so a file may change without a new
// generation.
type PromptSource struct {
	// Files is the ordered structural binding of prompt and skill files.
	Files []config.FileRef
	// Version is the content version fixed at the request boundary.
	Version string
}

// Publication records one admitted publication of an effective binding: the
// monotonically increasing generation, the fingerprint it published and the
// baseline it was admitted against.
type Publication struct {
	// Generation is the monotonically increasing number assigned to each actual
	// publication. Reloading byte-identical content yields no new generation.
	Generation uint64
	// Fingerprint is the organization fingerprint of the published declaration.
	Fingerprint string
	// Baseline is the version baseline the publication was admitted against.
	Baseline string
}

// BindingRef is the stable reference to one borrow: the initiating reference
// a delegated request carries so that it borrows the generation its initiator
// took rather than the generation effective at its own start.
//
// The reference names one agent of one published generation of this holder.
// Generation numbers come from this holder's publications only and are never
// chosen by a caller, and the instance identity is minted unguessable when the
// holder is created: the zero reference, one issued by another holder, one
// carried over from a previous run and a guessed one each name no borrow here.
// A reference is not a lease; it holds nothing and resolves only while the
// generation it names is still held.
type BindingRef struct {
	// AgentID names the agent whose borrowed binding the reference names.
	AgentID event.AgentID
	// InstanceID identifies the holder that issued the reference, so a
	// reference minted by another holder or by a previous run of this one
	// never resolves against this one.
	InstanceID event.InstanceID
	// Generation names the generation as this holder numbered its
	// publications when the borrow was opened. A publication advances the
	// number and no two publications share one.
	Generation uint64
}

// Borrow is the per-request borrowing face of the state owner: a request that
// begins executing takes the generation effective at that moment.
//
// The redline list (4 rules):
// MUST return the generation effective when the request begins executing, and hold it for the whole request.
// MUST serve a delegated request the generation its initiator took.
// MUST NOT bind a queued input in advance, nor swap the generation under a request already under way.
// MUST NOT serve a reference this holder never issued.
type Borrow interface {
	// Borrow returns a handle to the binding effective for one agent at the
	// moment the request begins executing, never one taken before the request
	// started. The borrowed generation is held by the returned handle for the
	// whole of the request's life.
	Borrow(ctx context.Context, agentID event.AgentID) (*BindingHandle, error)
	// BorrowDerived returns a handle to the binding of the generation the
	// reference's borrow took, for the whole of the delegated request's life:
	// the delegated request inherits its initiator's generation and is never
	// silently served the generation effective at its own start. A reference
	// this holder never issued is refused as ErrBindingRefUnknown, and one
	// whose initiating generation has already been released is refused as
	// ErrBindingGenerationReleased.
	BorrowDerived(ctx context.Context, ref BindingRef) (*BindingHandle, error)
}

// BindingHandle holds the borrowed binding one request uses for the whole of its
// life and ends that use when the request is done.
//
// Release ends the use of the borrowed binding and is called when the request's
// actual use of the binding has finished and the work producing it has ended,
// never merely because a newer generation was published: a request already under
// way keeps the generation it took until then.
type BindingHandle struct {
	// Binding is the binding generation this handle holds for the request.
	Binding AgentBinding
	// Ref is the stable reference to this borrow: the reference a delegated
	// request carries to BorrowDerived to inherit the generation this handle
	// holds.
	Ref BindingRef

	release  func(context.Context) error
	released int32
}

// NewStatePlan instantiates the candidate StatePlan of one configuration
// change: the Agents map is keyed by the identity of every declared agent, and
// the candidate resource registrations stay in the order the candidates were
// constructed. A declared agent no candidate binding was constructed for, a
// binding for an agent the declaration does not name, and a binding that
// carries no model each refuse the plan by name.
func NewStatePlan(plan OrgPlan, agents map[event.AgentID]AgentBinding, leases []LeaseRef) (StatePlan, error) {
	if plan.Declaration == nil {
		return StatePlan{}, ErrPlanMissing
	}
	out := StatePlan{
		Plan:   plan,
		Agents: map[event.AgentID]AgentBinding{},
		Leases: append([]LeaseRef(nil), leases...),
	}
	declared := map[event.AgentID]bool{}
	for _, spec := range plan.Declaration.Agents {
		id := event.AgentID(spec.ID)
		declared[id] = true
		binding, ok := agents[id]
		if !ok {
			return StatePlan{}, fmt.Errorf("%w: %s", ErrAgentBindingMissing, id)
		}
		if binding.Model == nil {
			return StatePlan{}, fmt.Errorf("%w: %s", ErrBindingIncomplete, id)
		}
		out.Agents[id] = binding
	}
	for id := range agents {
		if !declared[id] {
			return StatePlan{}, fmt.Errorf("%w: %s", ErrAgentBindingUnexpected, id)
		}
	}
	return out, nil
}

// Bindings holds the live binding generation the state owner serves to requests and hands one
// generation to each request as its execution begins.
//
// - A held generation is replaced as a whole: a request under way keeps the binding its handle was opened with until it releases that handle, an input still queued takes the generation effective when its own execution begins, and the handles in use stay countable here.
// - The holder keeps the generations it issued references to, so a delegated request borrows the generation its initiator took, and a generation whose last hold is gone is dropped by name instead of being silently re-lent.
// - The hold protocol: an input holds the generation effective when it begins executing until it releases the handle it borrowed; a delegated input takes its hold on the initiating generation before it is queued and the queue hands that hold on to the executing request instead of borrowing again.
// - A publication moves no handle already borrowed, and a cancelled or refused input releases the hold it took.
// - A recovery counts the holds that were never released before it borrows again and re-establishes the ones it accounts for; a borrow it cannot account for is not recoverable, its reference is refused by name, and the current generation is never lent in its place.
type Bindings struct {
	mu         sync.RWMutex
	agents     map[event.AgentID]AgentBinding
	inflight   int
	instance   event.InstanceID
	generation uint64
	current    *heldGeneration
	issued     map[uint64]*heldGeneration
}

// heldGeneration is the registry entry of one generation this holder served:
// the bindings it lent, the borrows still holding them, and the release that
// ends their use once the last hold is gone.
type heldGeneration struct {
	agents   map[event.AgentID]AgentBinding
	borrows  int
	released bool
}

// release drops the bindings of a generation whose last hold is gone, so the
// material becomes recyclable and a later reference to the generation is
// refused by name rather than served from what it left behind.
func (g *heldGeneration) release() {
	g.released = true
	g.agents = nil
}

// Bindings satisfies the Borrow face.
var _ Borrow = (*Bindings)(nil)

// NewBindings returns an empty binding holder that serves no generation yet.
func NewBindings() *Bindings {
	return &Bindings{
		instance: newInstanceID(),
		issued:   map[uint64]*heldGeneration{},
	}
}

// newInstanceID mints the unguessable identity of one binding holder, so a
// reference issued by another holder or carried over from a previous run never
// resolves against this one.
func newInstanceID() event.InstanceID {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("state: mint the binding holder identity: " + err.Error())
	}
	return event.InstanceID(hex.EncodeToString(buf[:]))
}

// Hold makes one prepared plan's candidate bindings the effective generation
// the next borrow takes. A plan that carries no agent binding is refused. Each
// hold publishes one generation numbered here, so the number a reference
// carries comes from a real publication and is never chosen by a caller; the
// generation it supersedes is released once no borrow holds it.
func (b *Bindings) Hold(plan StatePlan) error {
	if len(plan.Agents) == 0 {
		return ErrPlanEmpty
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	next := make(map[event.AgentID]AgentBinding, len(plan.Agents))
	for id, binding := range plan.Agents {
		next[id] = binding
	}
	if b.current != nil && b.current.borrows == 0 {
		b.current.release()
	}
	b.generation++
	b.agents = next
	b.current = &heldGeneration{agents: next}
	b.issued[b.generation] = b.current
	return nil
}

// Borrow implements Borrow and opens one request's use of the binding that is
// effective at the moment the call arrives. The returned handle holds that
// binding for the whole life of the request and ends that use through Release.
// Its Ref names the borrow, so a delegated request inherits the generation
// this call took.
func (b *Bindings) Borrow(ctx context.Context, agentID event.AgentID) (*BindingHandle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	binding, ok := b.agents[agentID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAgentUnknown, agentID)
	}
	gen := b.current
	gen.borrows++
	b.inflight++
	return &BindingHandle{
		Binding: binding,
		Ref:     BindingRef{AgentID: agentID, InstanceID: b.instance, Generation: b.generation},
		release: b.releaseFunc(gen),
	}, nil
}

// BorrowDerived implements Borrow and opens a delegated request's use of the
// binding of the generation the reference's borrow took. The returned handle
// holds that binding for the whole life of the delegated request, keeps its
// hold observable in Inflight, and ends that use through Release exactly once,
// as the initiating handle does. A reference this holder never issued — the
// zero reference, one issued by another holder, one carried over from a
// previous run or a guessed one — is refused as ErrBindingRefUnknown, and one
// whose initiating generation has already been released is refused as
// ErrBindingGenerationReleased; neither is silently served the generation
// effective at the delegated request's own start.
func (b *Bindings) BorrowDerived(ctx context.Context, ref BindingRef) (*BindingHandle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ref.InstanceID != b.instance {
		return nil, fmt.Errorf("%w: instance %s", ErrBindingRefUnknown, ref.InstanceID)
	}
	gen, ok := b.issued[ref.Generation]
	if !ok {
		return nil, fmt.Errorf("%w: generation %d", ErrBindingRefUnknown, ref.Generation)
	}
	if gen.released {
		return nil, fmt.Errorf("%w: generation %d", ErrBindingGenerationReleased, ref.Generation)
	}
	binding, ok := gen.agents[ref.AgentID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAgentUnknown, ref.AgentID)
	}
	gen.borrows++
	b.inflight++
	return &BindingHandle{
		Binding: binding,
		Ref:     ref,
		release: b.releaseFunc(gen),
	}, nil
}

// releaseFunc returns the release of one handle opened on the given
// generation: the holder's in-flight count and the generation's holds end
// together, and a superseded generation whose last hold is gone is released.
func (b *Bindings) releaseFunc(gen *heldGeneration) func(context.Context) error {
	return func(context.Context) error {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.inflight > 0 {
			b.inflight--
		}
		if gen.borrows > 0 {
			gen.borrows--
		}
		if gen != b.current && gen.borrows == 0 && !gen.released {
			gen.release()
		}
		return nil
	}
}

// Inflight reports how many borrowed handles are still in use, so the holders
// of a superseded generation — including the delegated holds opened through a
// reference — are observable while they finish.
func (b *Bindings) Inflight() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.inflight
}

// Release ends the request's use of the borrowed binding. It answers once per
// handle: a handle already released is refused by name, so the use of one
// borrowed generation ends exactly once.
func (h *BindingHandle) Release(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&h.released, 0, 1) {
		return ErrHandleReleased
	}
	if h.release == nil {
		return nil
	}
	return h.release(ctx)
}
