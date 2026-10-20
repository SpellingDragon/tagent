package tagent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/skill"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// RequestSnapshotRecord is one safe request snapshot kept beside the decision
// whose request it was fixed for: the decision identity of that request and
// the snapshot itself.
type RequestSnapshotRecord struct {
	// Decision is the decision whose request the snapshot was fixed for.
	Decision event.DecisionID
	// Snapshot is the safe fixed copy of what the model actually saw.
	Snapshot rl.RequestSnapshot
}

// snapshotRequest fixes the safe request snapshot of one decision beside the
// request the model entry is about to be called with: the fixed copy is taken
// at the formal request boundary before the call starts and holds what the
// model actually sees — the assembled prompt, skill bodies, projection and
// transcript of the request as it stands then. The snapshot observes the
// request and never edits it: the projection runs over copies, so the request
// keeps every original value including its credentials.
func (r *Run) snapshotRequest(_ context.Context, decisionID event.DecisionID, modelName, promptVersion string, visibleTools []string, binding state.AgentBinding, req model.Request) error {
	view := rl.ModelViewRef{
		Model:         modelName,
		PromptVersion: promptVersion,
		VisibleTools:  unversionedNames(visibleTools),
		VisibleSkills: unversionedNames(visibleSkillsOf(binding)),
	}
	snapshot, err := rl.SnapshotRequest(view, req, rl.SnapshotPolicy{})
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots = append(r.snapshots, RequestSnapshotRecord{Decision: decisionID, Snapshot: snapshot})
	return nil
}

// RequestSnapshots reports the safe request snapshots this assembly fixed
// before its model calls, in call order, as a structural-layer copy the caller
// owns: each Message, VisibleTools and VisibleSkills slice is copied, while
// nested ContentParts share the underlying parts of the fixed snapshot.
// Writing a shared part would be a caller bug; the snapshots themselves were
// fixed before the call. The snapshots are read-out material beside the
// requests they were fixed for; reading them moves nothing.
func (r *Run) RequestSnapshots() []RequestSnapshotRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RequestSnapshotRecord, 0, len(r.snapshots))
	for _, record := range r.snapshots {
		got := record
		got.Snapshot.Messages = append([]model.Message(nil), record.Snapshot.Messages...)
		got.Snapshot.VisibleTools = append([]rl.VersionedName(nil), record.Snapshot.VisibleTools...)
		got.Snapshot.VisibleSkills = append([]rl.VersionedName(nil), record.Snapshot.VisibleSkills...)
		out = append(out, got)
	}
	return out
}

// unversionedNames records a list of visible names as unversioned view members:
// the snapshot pins each name exactly as the request saw it and never invents
// a version beside a member that carries none.
func unversionedNames(names []string) []rl.VersionedName {
	out := make([]rl.VersionedName, 0, len(names))
	for _, name := range names {
		out = append(out, rl.VersionedName{Name: name})
	}
	return out
}

// visibleSkillsOf reports the skill names visible to one binding, in skill-name
// order, each named once: the declared skill files of the prompt source beside
// the skill bodies the binding's tool resources already loaded, under the same
// visibility rule the request boundary assembles skill bodies under.
func visibleSkillsOf(binding state.AgentBinding) []string {
	seen := map[string]bool{}
	var names []string
	for _, ref := range binding.PromptSource.Files {
		if ref.Kind != config.FileKindSkill {
			continue
		}
		name := skillNameOf(ref.Path)
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if resources, ok := binding.Set.(*state.ToolResources); ok {
		for _, load := range resources.SkillLoads() {
			if !load.Loaded || seen[load.Skill] {
				continue
			}
			seen[load.Skill] = true
			names = append(names, load.Skill)
		}
	}
	sort.Strings(names)
	return names
}

// fileFixer is the request-boundary file read of one agent: the file source
// that reads the declared prompt and skill bodies under the root those
// declarations resolve against. The structural binding keeps naming the
// files themselves — which files, in which order — while this source only
// reads their content afresh at every request boundary and fixes the content
// version one request runs on.
type fileFixer struct {
	source *state.FileSource
	root   string
}

// newFileFixer opens the request-boundary file source of one declared agent:
// prompt files are read under the root the declared files resolve against and
// skill files through the framework skill repository the declared skill
// folders give roots for. A declaration that names no file carries a source
// that fixes nothing.
func newFileFixer(files []config.FileRef) (*fileFixer, error) {
	root := fileRootOf(files)
	var repo skill.Repository
	if roots := skillRoots(files); len(roots) > 0 {
		opened, err := skill.NewFSRepository(roots...)
		if err != nil {
			return nil, fmt.Errorf("tagent: open the framework skill repository under %q: %w", strings.Join(roots, ","), err)
		}
		repo = opened
	}
	return &fileFixer{source: state.NewFileSource(root, repo), root: root}, nil
}

// rememberFixer records the request-boundary file source of one agent, so
// every request of that agent fixes its file contents through it. A
// republishing of the declaration replaces the source with the one the new
// construction opened.
func (r *Run) rememberFixer(id event.AgentID, fixer *fileFixer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fixers[id] = fixer
}

// fileRootOf reports the directory the declared files resolve against: the
// deepest directory every declared file lives under, made absolute so an
// absolute and a relative declaration name the same file under one root. A
// declaration that names no file carries no root.
func fileRootOf(files []config.FileRef) string {
	base := commonDir(files)
	if base == "" {
		return ""
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		return base
	}
	return abs
}

// relativeToRoot reports the given reference path as relative to the source
// root, so an absolute and a relative declaration resolve to the same file
// under one root. A reference that does not resolve keeps its own shape.
func relativeToRoot(root, path string) string {
	if root == "" {
		return filepath.FromSlash(path)
	}
	abs := filepath.FromSlash(path)
	if !filepath.IsAbs(abs) {
		if resolved, err := filepath.Abs(abs); err == nil {
			abs = resolved
		}
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.FromSlash(path)
	}
	return rel
}

// skillNameOf reports the skill name one skill reference names — the same
// naming the file source reads a skill body under: the reference points at the
// skill folder or at its SKILL.md file.
func skillNameOf(path string) string {
	name := filepath.Base(filepath.FromSlash(path))
	if name == skill.SkillFile {
		name = filepath.Base(filepath.Dir(filepath.FromSlash(path)))
	}
	return name
}

// fixate reads the contents one request uses afresh and fixes their version:
// every reference is resolved against the source root before it is read, so a
// file rewritten between two requests is read anew by the later one while the
// structural generation the binding belongs to stays where it is.
func (f *fileFixer) fixate(ctx context.Context, source state.PromptSource) (state.FileSnapshot, error) {
	normalized := state.PromptSource{Version: source.Version}
	for _, ref := range source.Files {
		normalized.Files = append(normalized.Files, config.FileRef{
			Path: relativeToRoot(f.root, ref.Path),
			Kind: ref.Kind,
		})
	}
	return f.source.Fixate(ctx, normalized)
}

// replayEntry is one session chain's mapped sequence registered for replay: the
// projection itself beside the budget the request boundary assembles it under,
// how many of its leading entries have ended — the material a request presents,
// while the entries still in flight stay with the transcript that carries them —
// and the invocation whose range those in-flight entries belong to.
type replayEntry struct {
	projection      *replay.ReplayProjection
	budget          int
	ended           int
	rangeInvocation event.InvocationID
}

// AttachReplay registers the replayed projection of one session chain on this
// assembly: every request of that session carries the chain's budget assembly
// in front of its transcript, mapped under the frozen event-type→role table.
// The entries a registration carries were replayed from the chain, so they are
// ended material and are presented at once; the facts the run commits from here
// on present themselves once their range ends.
// A chain that names no session is refused by name: assembling one would join
// every user of the partition into one transcript.
func (r *Run) AttachReplay(chain agent.RequestChain, budget int, projection *replay.ReplayProjection) error {
	if chain.SessionID == "" {
		return agent.ErrRequestChainSessionRequired
	}
	if projection == nil {
		return errors.New("tagent: the replay registration carries no projection")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replays[chain.SessionID] = replayEntry{projection: projection, budget: budget, ended: projection.Len()}
	return nil
}

// requestBoundary assembles the request-boundary material of one decision in
// the fixed layout the agent's assembly documents: the fixed file contents
// first — the prompt and skill bodies the binding names, in structural order
// — then the skill bodies already loaded on the binding's tool resources
// beside them (skill-name order, once each, and never a body the fixed files
// already carry), then the replayed projection of the session chain, and
// finally the dynamic environment material of the request when the agent
// declares it: that block is the answer of the request-assembly plugin phase,
// rendered by the element this assembly registers for it. The content version
// the boundary fixed is reported back as the version the request runs on.
//
// The order is the cache ordering the environment block exists for: what stays
// the same between two requests of one session — the fixed prompt and skill
// bodies, the transcript ahead of it — sits at the top as the stable prefix,
// while what changes with the run sits at the bottom of the assembly, so a
// change of the available tool face or of the open task state never moves the
// prefix before it.
func (r *Run) requestBoundary(ctx context.Context, boundary agent.RequestBoundary) (agent.BoundaryView, error) {
	if boundary.Chain.SessionID == "" {
		return agent.BoundaryView{}, agent.ErrRequestChainSessionRequired
	}
	view := agent.BoundaryView{}
	r.mu.Lock()
	fixer := r.fixers[boundary.Agent]
	sequence, registered := r.replays[boundary.Chain.SessionID]
	r.mu.Unlock()
	fixedSkills := map[string]bool{}
	if fixer != nil {
		snapshot, err := fixer.fixate(ctx, boundary.Binding.PromptSource)
		if err != nil {
			return agent.BoundaryView{}, err
		}
		for _, fixed := range snapshot.Files {
			view.Messages = append(view.Messages, model.Message{Role: model.RoleSystem, Content: fixed.Content})
			if fixed.Ref.Kind == config.FileKindSkill {
				fixedSkills[skillNameOf(fixed.Ref.Path)] = true
			}
		}
		if snapshot.Version != "" {
			view.PromptVersion = snapshot.Version
		}
	}
	if resources, ok := boundary.Binding.Set.(*state.ToolResources); ok {
		for _, load := range resources.SkillLoads() {
			if !load.Loaded || fixedSkills[load.Skill] {
				continue
			}
			view.Messages = append(view.Messages, model.Message{Role: model.RoleSystem, Content: load.Body})
		}
	}
	if registered {
		projected, err := sequence.projection.AssembleBudgetPrefix(ctx, sequence.budget, sequence.ended)
		if err != nil {
			return agent.BoundaryView{}, err
		}
		for _, entry := range projected {
			role, err := agent.RoleOf(entry.Type)
			if err != nil {
				return agent.BoundaryView{}, err
			}
			view.Messages = append(view.Messages, model.Message{Role: role, Content: entry.Content})
		}
	}
	if r.agentSpecOf(boundary.Agent).EnvironmentBlock {
		pipeline, err := plugin.NewPipeline([]plugin.Plugin{environmentBlockPlugin{run: r, boundary: boundary}})
		if err != nil {
			return agent.BoundaryView{}, err
		}
		block, err := pipeline.InjectEnvironment(ctx, plugin.EnvironmentRequest{Agent: boundary.Agent})
		if err != nil {
			return agent.BoundaryView{}, err
		}
		view.Messages = append(view.Messages, model.Message{Role: model.RoleSystem, Content: block})
	}
	return view, nil
}

// environmentBlockPlugin is the request-assembly element of this run: the
// environment-injection phase face over one request boundary of one agent. The
// block it renders is the dynamic material the boundary closes its assembly with,
// read from the runtime at this boundary.
type environmentBlockPlugin struct {
	run      *Run
	boundary agent.RequestBoundary
}

// The element satisfies both the registration face and the phase face it serves.
var (
	_ plugin.Plugin            = environmentBlockPlugin{}
	_ plugin.EnvironmentInject = environmentBlockPlugin{}
)

// Name returns the stable plugin name the phase line records.
func (p environmentBlockPlugin) Name() string { return "environment-block" }

// Phase reports the request-assembly phase this plugin serves.
func (p environmentBlockPlugin) Phase() plugin.Phase { return plugin.PhaseEnvironmentInject }

// InjectEnvironment renders the environment block of this assembly: the one
// block a request carries at the bottom of its boundary material.
func (p environmentBlockPlugin) InjectEnvironment(ctx context.Context, _ plugin.EnvironmentRequest) (string, error) {
	return p.run.environmentBlock(ctx, p.boundary), nil
}

// environmentTaskLines bounds how many open obligations one environment block
// names: a run holding more reports the count it left out rather than growing
// the request without bound.
const environmentTaskLines = 16

// environmentBlockGenerations counts the environment blocks the request boundary
// has rendered in this process, so the block's rewrite frequency is a measured
// face for a later throttle decision rather than an inference from the lines.
var environmentBlockGenerations atomic.Int64

// EnvironmentBlockGenerations reports how many environment blocks the request
// boundary has rendered in this process so far.
func EnvironmentBlockGenerations() int64 { return environmentBlockGenerations.Load() }

// environmentBlockDebug renders one line of the environment-block face at DEBUG:
// the trigger reason, the agent and the task count one generation named. One line
// per generation keeps the rewrite frequency observable, and the line carries no
// task text.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a failed assembly.
// MUST keep the INFO stream bounded by leaving every generation line at DEBUG.
// MUST NOT render task text or any boundary content.
func environmentBlockDebug(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Debugf(format, args...)
}

// environmentBlock renders the dynamic environment material one request carries
// at the bottom of the boundary assembly, for an agent that declares it: the
// agent the request runs as, the tool, skill and MCP faces available to it, and
// the asynchronous work it holds open at this boundary. The block is one system
// message and it closes the assembly, because everything in it changes with the
// run while the material above it — the fixed prompt, the skill bodies and the
// transcript ahead of the assembly — does not, so the changing part never moves
// the stable prefix it follows.
func (r *Run) environmentBlock(ctx context.Context, boundary agent.RequestBoundary) string {
	spec := r.agentSpecOf(boundary.Agent)
	open, omitted := r.openTaskLines(ctx)
	environmentBlockGenerations.Add(1)
	environmentBlockDebug("tagent: environment block generated reason=request-boundary agent=%s tasks=%d omitted=%d", boundary.Agent, len(open)+omitted, omitted)
	var b strings.Builder
	b.WriteString("## Environment\n")
	fmt.Fprintf(&b, "- agent: %s\n", boundary.Agent)
	if modelName := modelNameOf(boundary.Binding); modelName != "" {
		fmt.Fprintf(&b, "- model: %s\n", modelName)
	}
	fmt.Fprintf(&b, "- tools: %s\n", listOrNone(toolNamesOf(boundary.Binding)))
	fmt.Fprintf(&b, "- skills: %s\n", listOrNone(visibleSkillsOf(boundary.Binding)))
	fmt.Fprintf(&b, "- mcp: %s\n", listOrNone(endpointsOf(spec.Tools)))
	fmt.Fprintf(&b, "- tasks: %d open\n", len(open)+omitted)
	if len(open) == 0 {
		b.WriteString("  - (none)\n")
	}
	for _, line := range open {
		fmt.Fprintf(&b, "  - %s\n", line)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "  - (+%d more)\n", omitted)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// openTaskLines reports the obligations this run owes as the environment block
// names them — the class each waits in beside the task it belongs to — in stable
// order, bounded. The obligations left out are counted, and a task this run has
// released contributes no line.
func (r *Run) openTaskLines(ctx context.Context) ([]string, int) {
	if r.commit == nil || r.manager == nil {
		return nil, 0
	}
	var lines []string
	for _, id := range r.commit.TaskIDs() {
		open, err := r.manager.Obligations(ctx, id)
		if err != nil {
			continue
		}
		for _, obligation := range open.Open {
			lines = append(lines, fmt.Sprintf("%s task=%s", obligation.Kind, obligation.TaskID))
		}
	}
	sort.Strings(lines)
	if len(lines) <= environmentTaskLines {
		return lines, 0
	}
	return lines[:environmentTaskLines], len(lines) - environmentTaskLines
}

// toolNamesOf reports the tool names one binding exposes to the model, in name
// order and each named once: the tools the binding carries beside the tools its
// tool resources registered, so the file and skill families the request face
// carries are named with the contributed ones.
func toolNamesOf(binding state.AgentBinding) []string {
	seen := map[string]bool{}
	var names []string
	collect := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	for _, tool := range binding.Tools {
		if tool == nil || tool.Declaration() == nil {
			continue
		}
		collect(tool.Declaration().Name)
	}
	if resources, ok := binding.Set.(*state.ToolResources); ok {
		for _, record := range resources.Resources() {
			collect(record.Name)
		}
	}
	sort.Strings(names)
	return names
}

// endpointsOf reports the MCP faces one declaration makes available, as the
// name of the reference beside the endpoint it names, in name order.
func endpointsOf(refs []config.ToolRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.Endpoint == "" {
			continue
		}
		out = append(out, ref.Name+"@"+ref.Endpoint)
	}
	sort.Strings(out)
	return out
}

// modelNameOf reports the model name one binding runs against, empty when the
// binding carries no model.
func modelNameOf(binding state.AgentBinding) string {
	if binding.Model == nil {
		return ""
	}
	return binding.Model.Info().Name
}

// listOrNone renders one list of names as the environment block names it: the
// names in the order they were given, and a named empty face rather than a
// blank line, so a request that reaches an agent with no tool still says so.
func listOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}
