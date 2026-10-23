package state

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// 契约: docs/wiki/platform/org-hot-reload.md#overview
//
// ErrSkillReadMissing and ErrToolSetClosed are the named failures of the tool-resource face of the
// effective binding: the registration records of the native tools the composition root registered —
// which tool, of which native family, read from which source — beside the skill load state those
// tools leave in the state owner.
//
// - A skill load is a state fact of the binding generation the request borrowed, so it is recorded on the tool-set connection that binding carries and read back from it.
// - The body and the content version are fixed at one request boundary and kept as that request's fixed view: a file rewritten while a request is under way is never spliced into it, and the next request boundary reads the rewritten content anew.
var (
	// ErrSkillReadMissing reports a skill load fixed through no read: the load
	// carries no way to read the skill body it is asked to fix.
	ErrSkillReadMissing = errors.New("state: the skill load carries no read")
	// ErrToolSetClosed reports a tool registration on tool resources whose
	// tool sets were already closed.
	ErrToolSetClosed = errors.New("state: the tool resources are already closed")
)

// ToolKind names the native family one registered tool belongs to.
type ToolKind string

const (
	// ToolKindFile marks a tool of the native file tool set.
	ToolKindFile ToolKind = "file"
	// ToolKindSkill marks a tool of the native skill family.
	ToolKindSkill ToolKind = "skill"
	// ToolKindFunction marks a contributed native callable tool, including the
	// function-tool shape.
	ToolKindFunction ToolKind = "function"
)

// ToolResource is the registration record of one native tool: the name the
// model sees beside the family it belongs to and the source it was read from.
type ToolResource struct {
	// Name is the tool name the request carries.
	Name string
	// Kind is the native family the tool belongs to.
	Kind ToolKind
	// Source names where the tool was constructed from.
	Source string
}

// SkillLoad is the load state one skill load left in the state owner: the
// skill and agent it names, whether it loaded, the content version it fixed at
// that request boundary beside the body fixed with it, and the reason a load
// that did not succeed reports.
type SkillLoad struct {
	// Skill is the skill name the load was asked for.
	Skill string
	// Agent is the agent whose request carried the load.
	Agent string
	// Loaded reports whether the skill body was read and fixed.
	Loaded bool
	// Version is the content version of the fixed body. It names the body
	// fixed at one request boundary and never a structural generation.
	Version string
	// Body is the skill body fixed at the request boundary that loaded it.
	Body string
	// Reason reports why a load that did not succeed failed.
	Reason string
}

// ToolResources aggregates the registered native tools of one agent behind the
// tool-set slot of its effective binding: the registration records of every
// tool that entered the request beside the skill load state those tools write
// back, and the tool sets whose Close one resource lease governs.
//
// The skill load view is fixed per request boundary: the first load of one
// skill in a request reads and fixes the body, the loads that follow in the
// same request reuse that fixed view, and the request after it reads anew.
type ToolResources struct {
	mu      sync.Mutex
	name    string
	tools   []trpctool.Tool
	sets    []trpctool.ToolSet
	records []ToolResource
	loads   map[string]SkillLoad
	views   map[string]SkillLoad
	order   []string
	closed  bool
}

// skillViewLimit bounds how many request boundaries keep their fixed skill
// view on one binding, so a long-lived binding never accumulates the views of
// requests that ended long ago.
const skillViewLimit = 256

// NewToolResources returns the tool resources of one agent over the tool sets
// whose Close one resource lease governs. A nil set is skipped: it carries
// nothing to close.
func NewToolResources(name string, sets ...trpctool.ToolSet) *ToolResources {
	r := &ToolResources{
		name:  name,
		loads: map[string]SkillLoad{},
		views: map[string]SkillLoad{},
	}
	r.Attach(sets...)
	return r
}

// Attach registers tool sets beside the resources: their tools stay wherever
// the caller registered them, while their Close runs with these resources.
func (r *ToolResources) Attach(sets ...trpctool.ToolSet) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, set := range sets {
		if set != nil {
			r.sets = append(r.sets, set)
		}
	}
}

// Register records the given native tools as belonging to one family and
// makes them the tools the request carries. A nil tool is skipped.
func (r *ToolResources) Register(kind ToolKind, source string, tools ...trpctool.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		name := ""
		if declaration := tool.Declaration(); declaration != nil {
			name = declaration.Name
		}
		r.tools = append(r.tools, tool)
		r.records = append(r.records, ToolResource{Name: name, Kind: kind, Source: source})
	}
}

// Resources reports the registration records in registration order.
func (r *ToolResources) Resources() []ToolResource {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ToolResource(nil), r.records...)
}

// FixateSkillLoad fixes one skill load at the named request boundary: the
// first load of that skill in one request reads the body through the given
// read and keeps it as that request's fixed view, the loads that follow in the
// same request answer from that view without a second read, and the request
// after it reads anew — so a file rewritten mid-request is never spliced into
// the request under way. Whichever way the load ends, its state is recorded on
// these resources: a successful load carries the fixed body and its content
// version, and a refused one carries the reason it refused by.
//
// An empty request boundary fixes nothing: the read runs and the state is
// recorded, but no view is kept for a later call to answer from.
func (r *ToolResources) FixateSkillLoad(requestID, agent, name string, read func(string) (SkillLoad, error)) (SkillLoad, error) {
	if read == nil {
		return SkillLoad{}, ErrSkillReadMissing
	}
	key := requestID + "\x00" + name
	if requestID != "" {
		r.mu.Lock()
		if hit, ok := r.views[key]; ok {
			r.mu.Unlock()
			return hit, nil
		}
		r.mu.Unlock()
	}
	load, err := read(name)
	load.Skill = name
	load.Agent = agent
	if err != nil {
		load.Loaded = false
		if load.Reason == "" {
			load.Reason = err.Error()
		}
		r.record(load)
		return load, err
	}
	load.Loaded = true
	r.record(load)
	if requestID != "" {
		r.keepView(key, load)
	}
	return load, nil
}

// record keeps one skill load state on these resources, so the state the
// binding carries reads back the latest load of every skill.
func (r *ToolResources) record(load SkillLoad) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loads[load.Skill] = load
}

// keepView keeps one fixed skill view for the named request boundary,
// dropping the oldest views once the bounded table is full.
func (r *ToolResources) keepView(key string, load SkillLoad) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.views[key]; !ok {
		r.order = append(r.order, key)
		for len(r.order) > skillViewLimit {
			delete(r.views, r.order[0])
			r.order = r.order[1:]
		}
	}
	r.views[key] = load
}

// SkillLoad reports the load state one skill left in the state owner and
// whether any load of it was recorded.
func (r *ToolResources) SkillLoad(name string) (SkillLoad, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	load, ok := r.loads[name]
	return load, ok
}

// SkillLoads reports every recorded skill load state in skill-name order.
func (r *ToolResources) SkillLoads() []SkillLoad {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.loads))
	for name := range r.loads {
		names = append(names, name)
	}
	sort.Strings(names)
	loads := make([]SkillLoad, 0, len(names))
	for _, name := range names {
		loads = append(loads, r.loads[name])
	}
	return loads
}

// Tools reports the registered native tools of these resources in
// registration order.
func (r *ToolResources) Tools(context.Context) []trpctool.Tool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]trpctool.Tool(nil), r.tools...)
}

// Name reports the name identifying these tool resources.
func (r *ToolResources) Name() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.name
}

// Close closes every tool set these resources hold in reverse registration
// order and once each, joining whatever refuses into the returned cause. A
// second close reports nothing left to close instead of closing twice.
func (r *ToolResources) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrToolSetClosed, r.name)
	}
	r.closed = true
	sets := append([]trpctool.ToolSet(nil), r.sets...)
	r.mu.Unlock()
	var cause error
	for i := len(sets) - 1; i >= 0; i-- {
		cause = errors.Join(cause, sets[i].Close())
	}
	return cause
}

// ToolResources satisfies the native tool-set face of the framework.
var _ trpctool.ToolSet = (*ToolResources)(nil)
