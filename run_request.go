package tagent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/state"

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

// replayEntry is one session chain's projection registered for replay: the
// projection itself beside the budget the request boundary assembles it under.
type replayEntry struct {
	projection *replay.ReplayProjection
	budget     int
}

// AttachReplay registers the replayed projection of one session chain on this
// assembly: every request of that session carries the chain's budget assembly
// in front of its transcript, mapped under the frozen event-type→role table.
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
	r.replays[chain.SessionID] = replayEntry{projection: projection, budget: budget}
	return nil
}

// requestBoundary assembles the request-boundary material of one decision in
// the fixed layout the agent's assembly documents: the fixed file contents
// first — the prompt and skill bodies the binding names, in structural order
// — then the skill bodies already loaded on the binding's tool resources
// beside them (skill-name order, once each, and never a body the fixed files
// already carry), then the replayed projection of the session chain. The
// content version the boundary fixed is reported back as the version the
// request runs on.
func (r *Run) requestBoundary(ctx context.Context, boundary agent.RequestBoundary) (agent.BoundaryView, error) {
	if boundary.Chain.SessionID == "" {
		return agent.BoundaryView{}, agent.ErrRequestChainSessionRequired
	}
	view := agent.BoundaryView{}
	r.mu.Lock()
	fixer := r.fixers[boundary.Agent]
	replay, registered := r.replays[boundary.Chain.SessionID]
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
		projected, err := replay.projection.AssembleBudget(ctx, replay.budget)
		if err != nil {
			return agent.BoundaryView{}, err
		}
		for _, entry := range projected {
			view.Messages = append(view.Messages, model.Message{Role: agent.RoleOf(entry.Type), Content: entry.Content})
		}
	}
	return view, nil
}
