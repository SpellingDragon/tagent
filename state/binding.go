package state

import (
	"context"

	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/plugin"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
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

// Borrow is the per-request borrowing face of the state owner: a request that
// begins executing takes the generation effective at that moment.
//
// An input that is still queued is not bound in advance, so an input queued
// before a publication borrows the new generation when it starts executing,
// while a request already under way keeps the generation it took.
type Borrow interface {
	// Borrow returns a handle to the binding effective for one agent at the
	// moment the request begins executing, never one taken before the request
	// started. The borrowed generation is held by the returned handle for the
	// whole of the request's life.
	Borrow(ctx context.Context, agentID event.AgentID) (*BindingHandle, error)
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
}
