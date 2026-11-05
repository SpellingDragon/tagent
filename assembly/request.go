package assembly

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/tool/knowledge"

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

// snapshotReader reads back the safe request snapshots a capture fixed: every
// snapshot it keeps, in fix order, each beside the decision whose request it was
// fixed for. A run holds no snapshot copy of its own, so the read face is the
// capture's own account and never a second one beside it.
type snapshotReader interface {
	Snapshots() []rl.DecisionSnapshot
}

// requestSnapshots reports the safe request snapshots the capture of this run
// fixed, in fix order, or none while the assembly carries no capture: a run
// beside no capture fixes no request view and answers an empty read face rather
// than a copy kept beside the capture.
func (r *Run) requestSnapshots() []rl.DecisionSnapshot {
	reader, ok := r.capture.(snapshotReader)
	if !ok {
		return nil
	}
	return reader.Snapshots()
}

// RequestSnapshots reports the safe request snapshots this assembly fixed
// before its model calls, in call order, as a structural-layer copy the caller
// owns: each Message, VisibleTools and VisibleSkills slice is copied, while
// nested ContentParts share the underlying parts of the fixed snapshot.
// Writing a shared part would be a caller bug; the snapshots themselves were
// fixed before the call. The snapshots are read back from the capture that fixed
// them — their one home — and a run assembled beside no capture answers none.
// Reading them moves nothing.
func (r *Run) RequestSnapshots() []RequestSnapshotRecord {
	fixed := r.requestSnapshots()
	out := make([]RequestSnapshotRecord, 0, len(fixed))
	for _, one := range fixed {
		record := RequestSnapshotRecord{Decision: one.Decision, Snapshot: one.Snapshot}
		record.Snapshot.Messages = append([]model.Message(nil), one.Snapshot.Messages...)
		record.Snapshot.VisibleTools = append([]rl.VersionedName(nil), one.Snapshot.VisibleTools...)
		record.Snapshot.VisibleSkills = append([]rl.VersionedName(nil), one.Snapshot.VisibleSkills...)
		out = append(out, record)
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

// fileFactDimension names the configuration dimension one file content move
// states: the file face is a declaration source, so a content change lands as a
// config_change fact of this dimension and the request boundary projects the
// content from that fact instead of reading the file around it. The vocabulary
// is one list in the event package, so a reader of the dimension reads the same
// constant every producer writes.
const fileFactDimension = event.ConfigChangeDimensionFile

// fileFactGeneration is the publication generation a file change fact names: a
// file content change takes effect at the request boundary without entering a
// publication — the frozen configuration contract forbids it from moving the
// structural generation — so the judgment claims no publication and answers the
// zero one. The zero generation also keeps the identity of one content version
// stable across publications and restarts.
const fileFactGeneration uint64 = 0

// fileFixer is the request-boundary file read of one agent: the file source
// that reads the declared prompt and skill bodies under the root those
// declarations resolve against. The structural binding keeps naming the
// files themselves — which files, in which order — while this source reads
// their content afresh at every request boundary, files the content version it
// read as a config_change fact of the file dimension, and fixes the version one
// request runs on.
//
// The read is the one in-process observation of the files: the content a
// request runs on is projected from the fact the read files, so no second path
// hands a later request a file body the chain does not carry. A fixer the run
// never installed has no chain to file through and keeps the direct read, and a
// fixer built for a delegated assembly is bound to the run and the agent the
// same way, so the instruction that assembly hands over is projected from the
// chain too.
type fileFixer struct {
	source *state.FileSource
	root   string
	// run is the runtime the file change facts are filed through and read back
	// from; it stays nil for a fixer the run never installed.
	run *Run
	// agent names the agent whose file face this fixer fixes; it stays empty
	// beside no run.
	agent event.AgentID
	// mu serializes the fact resolution of this fixer, so two boundaries of one
	// agent never race one content identity into two commits.
	mu sync.Mutex
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
// every request of that agent fixes its file contents through it. The run and
// the agent are bound here: the fixer files and reads back the file change
// facts of that agent's face. A republishing of the declaration replaces the
// source with the one the new construction opened.
func (r *Run) rememberFixer(id event.AgentID, fixer *fileFixer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fixer.run = r
	fixer.agent = id
	r.fixers[id] = fixer
}

// delegatedFileFixer opens the file source one delegated assembly reads its
// prompt through and binds the run and the agent whose file change facts the
// read files and reads back. The binding is the one rememberFixer gives the
// request-boundary source, so the instruction a delegated assembly hands over is
// projected from the same fact family the request boundary projects; the request
// boundary keeps the source its own construction registered, because the
// delegated read is built over the file list of the declaration it assembles and
// may front-run that registration while the generation is still being prepared.
// The fact identity is content-addressed, so a read beside the registered source
// files the very same fact.
func (r *Run) delegatedFileFixer(id event.AgentID, files []config.FileRef) (*fileFixer, error) {
	fixer, err := newFileFixer(files)
	if err != nil {
		return nil, err
	}
	fixer.run = r
	fixer.agent = id
	return fixer, nil
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
// file rewritten between two requests is observed anew by the later one while
// the structural generation the binding belongs to stays where it is.
//
// The read is the observation, never the projection: each read content is filed
// as a config_change fact of the file dimension and the content the request runs
// on is read back from that fact. A filing that cannot land does not refuse the
// request — the content the boundary read stands in for it — and is warned about
// instead, so a broken fact chain never stops the business while the failure
// stays visible.
func (f *fileFixer) fixate(ctx context.Context, source state.PromptSource) (state.FileSnapshot, error) {
	normalized := state.PromptSource{Version: source.Version}
	for _, ref := range source.Files {
		normalized.Files = append(normalized.Files, config.FileRef{
			Path: relativeToRoot(f.root, ref.Path),
			Kind: ref.Kind,
		})
	}
	observed, err := f.source.Fixate(ctx, normalized)
	if err != nil {
		return state.FileSnapshot{}, err
	}
	if f.run == nil || f.agent == "" {
		return observed, nil
	}
	return state.FileSnapshot{
		Files:   f.projectThroughFacts(ctx, observed.Files),
		Version: observed.Version,
	}, nil
}

// projectThroughFacts projects the read contents through the file change facts
// of this agent: each fixed file is named by a content-addressed fact, the fact
// is filed when the chain does not carry it yet, and the content the request
// runs on is read back from that fact. The content version stays the version the
// read computed: a fact that does not carry exactly the description the read
// names is a filing failure and leaves the read content in place, so the
// projected contents always answer the same version.
func (f *fileFixer) projectThroughFacts(ctx context.Context, files []state.FixedFile) []state.FixedFile {
	f.mu.Lock()
	defer f.mu.Unlock()
	projected := make([]state.FixedFile, 0, len(files))
	for _, file := range files {
		content := file.Content
		factContent, err := f.fileFact(ctx, file.Ref, file.Content)
		if err == nil {
			content = factContent
		} else {
			f.warnFileFact(file.Ref, err)
		}
		projected = append(projected, state.FixedFile{Ref: file.Ref, Content: content})
	}
	return projected
}

// fileFact returns the content the file change fact of one read names: the
// content-addressed fact is read first, so a request whose content the chain
// already carries runs on the fact exactly as a later request does; a content
// the chain does not carry yet is filed as one config_change fact of the file
// dimension and read back. The answered content is the content the fact carries.
// The summary the fact carries names the file and the content version in one line,
// so a reader of the fact names the change without rendering its body.
func (f *fileFixer) fileFact(ctx context.Context, ref config.FileRef, content string) (string, error) {
	header := fileFactHeader(ref, content)
	description := header + "\n" + content
	change := event.ConfigChangeFor(f.agent, fileFactDimension, fileFactGeneration, description)
	fact, err := f.run.CommittedEvent(ctx, change.ID)
	switch {
	case err == nil:
		return fileFactContent(fact, description)
	case errors.Is(err, ErrCommittedFactMissing):
	default:
		return "", fmt.Errorf("read the committed file change fact %s: %w", change.ID, err)
	}
	committed := change.Event(time.Now().UTC())
	committed.Meta[event.MetaKeyEventSummary] = header
	if _, err := f.run.commitFactChecked(ctx, committed); err != nil {
		return "", fmt.Errorf("commit the file change fact %s: %w", change.ID, err)
	}
	fact, err = f.run.CommittedEvent(ctx, change.ID)
	if err != nil {
		return "", fmt.Errorf("read the committed file change fact %s: %w", change.ID, err)
	}
	return fileFactContent(fact, description)
}

// fileFactContent reads the content one committed file change fact carries: the
// fact must be the configuration change of the file dimension and must carry
// exactly the description the boundary computed for the content it read. A fact
// under the identity carrying other content is refused as a filing failure
// instead of being projected, so a conflicting fact never rewrites a request.
func fileFactContent(fact event.Event, description string) (string, error) {
	if fact.Type != event.TypeConfigChange {
		return "", fmt.Errorf("the fact under the identity is of type %q, not %q", fact.Type, event.TypeConfigChange)
	}
	if dimension := fact.Meta[event.MetaKeyConfigChangeDimension]; dimension != fileFactDimension {
		return "", fmt.Errorf("the fact under the identity names dimension %q, not %q", dimension, fileFactDimension)
	}
	if fact.Message.Content != description {
		return "", errors.New("the fact under the identity carries other content")
	}
	_, content, found := strings.Cut(description, "\n")
	if !found {
		return "", errors.New("the file change description carries no content")
	}
	return content, nil
}

// fileFactHeader states the one-line identity of one fixed file: the reference
// the boundary read and the version of the content it fixed. It is the summary
// the fact commits with, so a reader names the file and the version without
// rendering the body.
func fileFactHeader(ref config.FileRef, content string) string {
	sum := sha256.Sum256([]byte(content))
	kind := string(ref.Kind)
	if kind == "" {
		kind = string(config.FileKindPrompt)
	}
	return "file " + strconv.Quote(ref.Path) + " kind=" + kind + " version=" + hex.EncodeToString(sum[:])
}

// warnFileFact reports a file change fact the boundary could not read or file.
// The line stays a bypass: the request runs on the content the boundary read,
// and the warning is the account of the fact that never landed. The line names
// the agent, the file and the failure but never the content, so the prompt
// material stays out of the log.
func (f *fileFixer) warnFileFact(ref config.FileRef, err error) {
	assemblyWarn("tagent: the file change fact of agent %q for %q was not filed: %v", f.agent, ref.Path, err)
}

// replayEntry is one session chain's mapped sequence registered for replay: the
// projection itself beside the window the declaration replays it into, how many
// of its leading entries have ended — the material a request presents, while the
// entries still in flight stay with the transcript that carries them — and
// whether the chain carries a segment that has not closed yet.
type replayEntry struct {
	projection *replay.ReplayProjection
	window     FoldWindow
	// endedAt names the first entry of the range in flight: everything before
	// it is the ended material a request presents. The boundary is recorded by
	// identity, never by position, because the age fold rewrites the sequence
	// the commits record — a position read after a fold names other material,
	// or falls past the shortened sequence and silently moves the in-flight
	// notice into the budgeted prefix. An empty identity names a chain with no
	// range in flight, so every entry is ended material.
	endedAt event.EventID
	// rangeOpen reports whether the chain carries a segment that has not
	// closed yet: the input that started it opened it, and only the
	// agent_output that closed it ends it. Input that lands beside the open
	// segment belongs to that segment and never moves the ended boundary.
	rangeOpen bool
}

// endedPrefix reports the position the ended material of the registered sequence
// ends at, resolved against the sequence as it stands now: the position of the
// entry the boundary names by identity — inclusive for the agent_output that
// closed a segment (its answer is ended material the next segment presents),
// exclusive for the input that opened the next range (it belongs to that range
// and the drive or loop of it carries it). A boundary naming no entry — a
// chain that admitted no range yet, or one whose opening entry a fold has
// already consumed — reports the end of the sequence, so every entry is ended
// material and none of the range in flight is presented twice.
func (h replayEntry) endedPrefix() int {
	if h.projection == nil {
		return 0
	}
	if h.endedAt == "" {
		return h.projection.Len()
	}
	for at, entry := range h.projection.Entries() {
		if entry.Key == h.endedAt {
			if entry.Type == event.TypeAgentOutput {
				return at + 1
			}
			return at
		}
	}
	return h.projection.Len()
}

// SessionPresented reports the facts of one session chain whose own text the
// request prefix of that session presents: the entries the registered
// projection still holds ahead of the boundary its ended material reaches, kept
// to the ones the assembly presents at their own text rather than at the ticket,
// the summary or the product a tight budget or a fold leaves in their place.
//
// The resident consumption loop judges the material of a pull by this read, so
// the request prefix and the segment transcript present one fact between them
// exactly once: a fact the prefix carries is left out of the transcript, and a
// fact the prefix carries nowhere — behind the boundary, or consumed by a fold
// before any decision read it — is carried by the segment that drives it. The
// read is the assembly the request itself runs, never a second judgment of the
// same material. A session this run registered no sequence for answers nothing,
// because no prefix is assembled for it and no fact is presented there.
func (r *Run) SessionPresented(addr event.Address) map[event.EventID]bool {
	if addr.SessionID == "" {
		return nil
	}
	r.mu.Lock()
	_, held, carried := r.replaySlotLocked(addr)
	r.mu.Unlock()
	if !carried || held.projection == nil {
		return nil
	}
	entries := held.projection.Entries()
	prefix := held.endedPrefix()
	if prefix > len(entries) {
		prefix = len(entries)
	}
	text := make(map[event.EventID]string, prefix)
	for at := 0; at < prefix; at++ {
		if key := entries[at].Key; key != "" {
			text[key] = entries[at].Content
		}
	}
	if len(text) == 0 {
		return nil
	}
	views, err := held.projection.AssembleBudgetPrefix(context.Background(),
		projectionBudget(held.projection, held.window, prefix, 0), prefix)
	if err != nil {
		return nil
	}
	presented := make(map[event.EventID]bool, len(views))
	for _, view := range views {
		if full, held := text[view.Key]; held && full == view.Content {
			presented[view.Key] = true
		}
	}
	return presented
}

// AttachReplay registers the replayed projection of one session chain on this
// assembly: every request of that session carries the chain's window assembly
// in front of its transcript, mapped under the frozen event-type→role table.
// The entries a registration carries were replayed from the chain, so they are
// ended material and are presented at once; the facts the run commits from here
// on present themselves once their range ends.
// A chain that names no session is refused by name: assembling one would join
// every user of the partition into one transcript. A window of no tokens is
// accepted as the no-budget window it means: the requests present the whole
// projection with no budget bound, and the age fold measures against the
// default window instead of none.
// The seam carries no agent identity, so the registration is kept for every
// user of the session it names; the chains this run commits register under the
// full address of the agent that owns them through attachScopedReplay, so they
// never cross agents that share a session id.
func (r *Run) AttachReplay(chain agent.RequestChain, window FoldWindow, projection *replay.ReplayProjection) error {
	if chain.SessionID == "" {
		return agent.ErrRequestChainSessionRequired
	}
	return r.attachReplayAt(event.Address{SessionID: chain.SessionID}, window, projection)
}

// attachScopedReplay registers the mapped sequence of one agent's session chain
// under its full address — the agent beside its session — so a request of that
// agent reads its own chain's material and never the chain of another agent
// sharing the session id. A chain that names no session is refused exactly as
// the AttachReplay seam refuses it.
func (r *Run) attachScopedReplay(addr event.Address, window FoldWindow, projection *replay.ReplayProjection) error {
	if addr.SessionID == "" {
		return agent.ErrRequestChainSessionRequired
	}
	return r.attachReplayAt(addr, window, projection)
}

// attachReplayAt registers one mapped sequence under the address it names. A
// registration that carries no projection is refused exactly as the attach
// seams document; a window of no tokens is the no-budget window, never a
// refused one.
func (r *Run) attachReplayAt(addr event.Address, window FoldWindow, projection *replay.ReplayProjection) error {
	if projection == nil {
		return errors.New("tagent: the replay registration carries no projection")
	}
	window = normalizeFoldWindow(window)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replays[addr] = replayEntry{projection: projection, window: window}
	return nil
}

// replayAddressOf reports the full address one committed fact's mapped sequence
// registers under: the agent that owns the fact beside the session chain it
// belongs to, the same address its transcript history is keyed by.
func replayAddressOf(evt event.Event) event.Address {
	return event.Address{AgentID: agentOf(evt), SessionID: sessionOf(evt)}
}

// replaySlotLocked resolves the slot one address's mapped sequence is kept in,
// beside the registration it holds: the chain registered under the full address
// first, and the chain a registration naming no agent — the AttachReplay seam —
// left for every user of the session beside it. A reader that names no agent
// resolves through replayOfSessionLocked instead. The read is taken under the
// run lock the caller already holds.
func (r *Run) replaySlotLocked(addr event.Address) (event.Address, replayEntry, bool) {
	if held, carried := r.replays[addr]; carried {
		return addr, held, true
	}
	legacy := event.Address{SessionID: addr.SessionID}
	if held, carried := r.replays[legacy]; carried {
		return legacy, held, true
	}
	return event.Address{}, replayEntry{}, false
}

// replayOfSessionLocked resolves the mapped sequence a reader that names only
// the session chain reads: the chain a registration naming no agent left for
// every user of the session first, and otherwise the chain the session
// registered — beside several agent-scoped chains sharing one session id, the
// first of them in agent order, deterministically. The read is taken under the
// run lock the caller already holds.
func (r *Run) replayOfSessionLocked(session event.SessionID) (replayEntry, bool) {
	if session == "" {
		return replayEntry{}, false
	}
	if held, carried := r.replays[event.Address{SessionID: session}]; carried {
		return held, true
	}
	var agents []event.AgentID
	for addr := range r.replays {
		if addr.SessionID == session && addr.AgentID != "" {
			agents = append(agents, addr.AgentID)
		}
	}
	if len(agents) == 0 {
		return replayEntry{}, false
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i] < agents[j] })
	return r.replays[event.Address{AgentID: agents[0], SessionID: session}], true
}

// normalizeFoldWindow fills the fold settings a declaration left unset: a
// declaration naming only its model window folds under the default share of
// that window and keeps the default recent turns, so the zero declaration
// behaves as the default one. A ratio outside (0, 1] names no share the
// declaration can mean, so it reads as the default share rather than degrading
// to a bound.
func normalizeFoldWindow(window FoldWindow) FoldWindow {
	if window.CompressThreshold <= 0 || window.CompressThreshold > 1 {
		window.CompressThreshold = config.DefaultCompressThreshold
	}
	if window.KeepRecentTasks <= 0 {
		window.KeepRecentTasks = config.DefaultKeepRecentTasks
	}
	return window
}

// foldWindowOf reports the replay window one agent declaration names: the model
// window it replays into beside the fold settings the declaration gave it, with
// the unset settings read as their declaration defaults.
func foldWindowOf(spec config.AgentSpec) FoldWindow {
	return normalizeFoldWindow(FoldWindow{
		MaxTokens:         spec.MaxTokens,
		CompressThreshold: spec.CompressThreshold,
		KeepRecentTasks:   spec.KeepRecentTasks,
	})
}

// requestBoundary assembles the request-boundary material of one decision in
// the fixed layout the agent's assembly documents: the fixed file contents
// first — the prompt and skill bodies the binding names, in structural order
// — then the skill bodies already loaded on the binding's tool resources
// beside them (skill-name order, once each, and never a body the fixed files
// already carry), then the replayed projection of the session chain. Every
// segment of the prefix is a product of that one assembler, and the projected
// main context is assembled under the same budget assembly the delegated refs
// render under, so the request side never grows a second assembly channel
// beside it. The content version the boundary fixed is reported back as the
// version the request runs on, and an agent declaring the environment block
// receives the block itself: the request face renders it here and the loop
// appends it behind the transcript, so the block never enters the prefix.
//
// The order is the cache ordering the environment block exists for: what stays
// the same between two requests of one session — the fixed prompt and skill
// bodies, the transcript ahead of it — sits at the top as the stable prefix,
// while what changes with the run trails the request, so a change of the
// available tool face or of the open task state never moves the prefix it
// follows.
func (r *Run) requestBoundary(ctx context.Context, boundary agent.RequestBoundary) (agent.BoundaryView, error) {
	if boundary.Chain.SessionID == "" {
		return agent.BoundaryView{}, agent.ErrRequestChainSessionRequired
	}
	r.mu.Lock()
	fixer := r.fixers[boundary.Agent]
	_, sequence, registered := r.replaySlotLocked(event.Address{AgentID: boundary.Agent, SessionID: boundary.Chain.SessionID})
	r.mu.Unlock()
	assembly := requestAssembly{
		run:               r,
		boundary:          boundary,
		fixer:             fixer,
		sequence:          sequence,
		registered:        registered,
		fixedSkills:       map[string]bool{},
		loadedSkillBodies: map[string]string{},
	}
	return assembly.assemble(ctx)
}

// requestSegment produces one segment of a request assembly: the messages the
// segment contributes to the prefix, in the order the assembly lays them out.
type requestSegment func(ctx context.Context) ([]model.Message, []string, error)

// projectionAssembler is the one assembler the request boundary builds the
// prefix of a request through: every segment of the prefix is a product of this
// assembler — the fixed file contents, the loaded skill bodies and the projected
// main context — laid out in the order the segments are declared. No segment
// grows a dispatch channel of its own beside the assembly.
type projectionAssembler struct {
	segments []requestSegment
}

// segment declares one segment of the assembly in layout order.
func (a *projectionAssembler) segment(produce requestSegment) {
	a.segments = append(a.segments, produce)
}

// assemble runs every declared segment in order and answers the prefix they
// produce together. A refused segment refuses the assembly at that segment, so
// no request is built over material that failed to render.
func (a *projectionAssembler) assemble(ctx context.Context) ([]model.Message, []string, error) {
	var out []model.Message
	var sources []string
	for _, produce := range a.segments {
		messages, faces, err := produce(ctx)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, messages...)
		sources = append(sources, faces...)
	}
	return out, sources, nil
}

// requestAssembly is one request boundary's assembly: the assembler that lays
// the prefix out beside the material its segments settle while they run — the
// skill bodies the fixed files already carry and the content version the
// request runs on.
type requestAssembly struct {
	run               *Run
	boundary          agent.RequestBoundary
	fixer             *fileFixer
	sequence          replayEntry
	registered        bool
	fixedSkills       map[string]bool
	loadedSkillBodies map[string]string
	promptVersion     string
	assembler         projectionAssembler
}

// assemble declares the segments of the layout in the fixed order, runs them
// through the one assembler and answers the boundary view they produce. The
// prefix carries only the stable material: the declared prompt and the declared
// skill bodies, each merged into one system message, beside the history
// projection. Everything that changes while the run goes — the bodies a skill
// load published and the environment block — is rendered into the dynamic
// section and appended by the loop as the request's last message, so a change
// there never moves the stable prefix the provider caches.
func (a *requestAssembly) assemble(ctx context.Context) (agent.BoundaryView, error) {
	a.publishSkillBodies(ctx)
	if a.fixer != nil {
		a.assembler.segment(a.fixedSystem)
	}
	if a.registered {
		a.assembler.segment(a.projectedMainContext)
	}
	messages, sources, err := a.assembler.assemble(ctx)
	if err != nil {
		return agent.BoundaryView{}, err
	}
	view := agent.BoundaryView{
		Messages:      messages,
		Sources:       sources,
		PromptVersion: a.promptVersion,
		VisibleSkills: a.narrowSkillNames(mergeSkillNames(visibleSkillsOf(a.boundary.Binding), a.loadedSkillNames())),
	}
	window := a.budgetWindow()
	view = view.WithBudget(window, a.budgetReserve(window))
	if skills := a.dynamicSkillBodies(); skills != "" {
		view = view.WithDynamicSkills(skills)
	}
	if environment := a.dynamicEnvironment(ctx); environment != "" {
		view = view.WithEnvironmentBlock(environment)
	}
	return view, nil
}

// budgetWindow reports the provider context window the final request budget is
// measured against: the window the declaration names explicitly, or — when it
// names none — the replay window MaxTokens as the declaration's one window
// word. A declaration naming neither opens no gate.
func (a *requestAssembly) budgetWindow() int {
	spec := a.run.agentSpecOf(a.boundary.Agent)
	if spec.ProviderContextWindow > 0 {
		return spec.ProviderContextWindow
	}
	return spec.MaxTokens
}

// budgetReserve reports the output allowance the input budget leaves: the
// allowance the declaration names, or the fixed share of the provider window
// when it names none. The reserve is derived from the window the gate uses, so
// a window derived from MaxTokens carries the same default share.
func (a *requestAssembly) budgetReserve(window int) int {
	if reserve := a.run.agentSpecOf(a.boundary.Agent).OutputReserve; reserve > 0 {
		return reserve
	}
	return agent.OutputReserveOf(window)
}

// dynamicSkillBodies renders the bodies the skill loads published, in
// skill-name order, as the skill_bodies face of the dynamic section. The section
// travels the system channel as the request's final message, so each family
// adapter frames it as system material while the stable prefix ahead of it keeps
// its cache.
func (a *requestAssembly) dynamicSkillBodies() string {
	var b strings.Builder
	for _, name := range a.loadedSkillNames() {
		body := a.loadedSkillBodies[name]
		if body == "" {
			continue
		}
		fmt.Fprintf(&b, "## Skill: %s\n%s\n\n", name, body)
	}
	return b.String()
}

// dynamicEnvironment renders the environment block an agent declares as the
// environment face of the dynamic section. The material travels the system
// channel, so each family adapter turns it into its own system framing instead
// of the request posing as conversation.
func (a *requestAssembly) dynamicEnvironment(ctx context.Context) string {
	if !a.run.agentSpecOf(a.boundary.Agent).EnvironmentBlock {
		return ""
	}
	return a.run.environmentBlock(ctx, a.boundary, a.promptVersion)
}

// fixedSystem reads the declared prompt and skill bodies afresh at this boundary
// and contributes them as the leading system messages, in the structural order
// the binding carries, each beside the input face it renders. The skill names it
// read are kept beside the messages, so the loaded-skill segment never repeats a
// body the fixed files already carry.
func (a *requestAssembly) fixedSystem(ctx context.Context) ([]model.Message, []string, error) {
	snapshot, err := a.fixer.fixate(ctx, a.boundary.Binding.PromptSource)
	if err != nil {
		return nil, nil, err
	}
	var prompts, skillBodies []string
	visibleSkills := a.visibleSkillSet()
	for _, fixed := range snapshot.Files {
		if fixed.Ref.Kind == config.FileKindSkill {
			name := skillNameOf(fixed.Ref.Path)
			a.fixedSkills[name] = true
			if visibleSkills != nil && !visibleSkills[name] {
				continue
			}
			skillBodies = append(skillBodies, fixed.Content)
			continue
		}
		prompts = append(prompts, fixed.Content)
	}
	if snapshot.Version != "" {
		a.promptVersion = snapshot.Version
	}
	messages := make([]model.Message, 0, 2)
	sources := make([]string, 0, 2)
	if len(prompts) > 0 {
		messages = append(messages, model.Message{Role: model.RoleSystem, Content: strings.Join(prompts, "\n\n")})
		sources = append(sources, agent.BudgetFaceFixedPrompt)
	}
	if len(skillBodies) > 0 {
		messages = append(messages, model.Message{Role: model.RoleSystem, Content: strings.Join(skillBodies, "\n\n")})
		sources = append(sources, agent.BudgetFaceSkillBodies)
	}
	return messages, sources, nil
}

// publishSkillBodies records the skill bodies a load already fixed for the
// request into the dynamic section: the bodies the binding's tool resources fixed on the declared skill
// files, beside the bodies the agent's skill_dirs load tool published through
// its sink. Skill-name order, once each, and never a body the fixed files
// already carry. It runs after the fixed-file segment, so the names that
// segment read are exactly the ones it skips. The two sources meet here and
// nowhere else: a declared skill file keeps its request-boundary fixed view on
// the binding, while a skill_dirs load has no such file declaration to fix a
// view on and publishes through the sink instead.
func (a *requestAssembly) publishSkillBodies(context.Context) {
	bodies := a.loadedSkillBodies
	declared := map[string]bool{}
	for _, ref := range a.boundary.Binding.PromptSource.Files {
		if ref.Kind == config.FileKindSkill {
			declared[skillNameOf(ref.Path)] = true
		}
	}
	visibleSkills := a.visibleSkillSet()
	hold := func(name, body string) {
		if name == "" || body == "" || declared[name] || a.fixedSkills[name] {
			return
		}
		if visibleSkills != nil && !visibleSkills[name] {
			return
		}
		if _, held := bodies[name]; held {
			return
		}
		bodies[name] = body
	}
	resources, ok := a.boundary.Binding.Set.(*state.ToolResources)
	if !ok {
		return
	}
	for _, load := range resources.SkillLoads() {
		if !load.Loaded {
			continue
		}
		hold(load.Skill, load.Body)
	}
	if carriesContributedLoad(resources) {
		for _, load := range skillLoadsOf(a.boundary.Agent) {
			hold(load.Name, load.Body)
		}
	}
}

// visibleSkillSet reports the skills the visible face keeps as a set, or nil
// when the selection names no subset: the fixed skill bodies and the dynamic
// section read the same face, so the prefix never carries a body the visible
// list omits.
func (a *requestAssembly) visibleSkillSet() map[string]bool {
	resources, ok := a.boundary.Binding.Set.(*state.ToolResources)
	if !ok {
		return nil
	}
	visible := resources.Visible().Skills
	if visible == nil {
		return nil
	}
	set := make(map[string]bool, len(visible))
	for _, name := range visible {
		set[name] = true
	}
	return set
}

// narrowedSkillsOf reports the declared skills the visible face keeps: a nil
// selection keeps every declaration, and a named subset keeps the
// intersection. The environment block and the request's visible skills read
// one face.
func narrowedSkillsOf(binding state.AgentBinding) []string {
	names := visibleSkillsOf(binding)
	resources, ok := binding.Set.(*state.ToolResources)
	if !ok {
		return names
	}
	visible := resources.Visible().Skills
	if visible == nil {
		return names
	}
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if slices.Contains(visible, name) {
			kept = append(kept, name)
		}
	}
	return kept
}

// narrowSkillNames reports the skill names the visible face keeps: a nil
// selection keeps every name, and a named subset keeps the intersection.
func (a *requestAssembly) narrowSkillNames(names []string) []string {
	resources, ok := a.boundary.Binding.Set.(*state.ToolResources)
	if !ok {
		return names
	}
	visible := resources.Visible().Skills
	if visible == nil {
		return names
	}
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if slices.Contains(visible, name) {
			kept = append(kept, name)
		}
	}
	return kept
}

// visibleMCPOf reports the MCP references the visible face keeps, by reference
// name: a nil selection keeps every configured reference.
func visibleMCPOf(spec config.AgentSpec, binding state.AgentBinding) []string {
	refs := spec.Tools
	if resources, ok := binding.Set.(*state.ToolResources); ok {
		if visible := resources.Visible().MCP; visible != nil {
			kept := make([]config.ToolRef, 0, len(refs))
			for _, ref := range refs {
				if slices.Contains(visible, ref.Name) {
					kept = append(kept, ref)
				}
			}
			refs = kept
		}
	}
	return endpointsOf(refs)
}

// mergeSkillNames reports the union of two skill-name lists, each name once,
// in name order: the visible face the request carries and the dynamic section
// name the same set.
func mergeSkillNames(declared, loaded []string) []string {
	seen := make(map[string]bool, len(declared)+len(loaded))
	names := make([]string, 0, len(declared)+len(loaded))
	for _, list := range [][]string{declared, loaded} {
		for _, name := range list {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// loadedSkillNames reports the published skill bodies in skill-name order.
func (a *requestAssembly) loadedSkillNames() []string {
	names := make([]string, 0, len(a.loadedSkillBodies))
	for name := range a.loadedSkillBodies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// carriesContributedLoad reports whether one binding carries the contributed
// skill load tool: the knowledge surface the host contributes answers under the
// function family, while the load tool of the declared skill files answers under
// the skill family. Only the contributed one publishes skill_dirs bodies: a
// declaration reloaded without the knowledge face keeps no way to load a body,
// so the bodies a retired surface published never leak into the requests of the
// generation after it, and the declared skill files keep their own recorded
// source.
func carriesContributedLoad(resources *state.ToolResources) bool {
	for _, record := range resources.Resources() {
		if record.Kind == state.ToolKindFunction && record.Name == knowledge.SkillLoadToolName {
			return true
		}
	}
	return false
}

// skillLoadRegistry holds the skill_dirs bodies the load tools published, one
// ledger per agent: the sink a host mounts on an agent's knowledge surface
// writes into that agent's ledger, and the request boundary of the same agent
// reads the bodies back through the agent identity it assembles for. The
// ledger is per agent rather than per run because the sink is mounted while the
// agent's tools are constructed, before the run exists; the agent identity is
// the one fact both sides already carry.
type skillLoadRegistry struct {
	mu      sync.Mutex
	ledgers map[event.AgentID]*skillLoadLedger
}

// skillLoadLedger is one agent's published skill bodies: the latest body of
// every skill its load tool answered, in the order the loads published them.
type skillLoadLedger struct {
	mu     sync.Mutex
	order  []string
	bodies map[string]string
}

// skillLoads is the process-wide registry the sink factory mounts ledgers on
// and the request boundary reads them from.
var skillLoads = &skillLoadRegistry{ledgers: map[event.AgentID]*skillLoadLedger{}}

// NewSkillLoadSink mounts a fresh loaded-skill ledger on one agent and answers
// the sink that agent's knowledge surface publishes loaded bodies to. The host
// mounts one sink per knowledge surface it constructs, so a surface rebuilt on
// reload replaces its agent's ledger instead of inheriting the bodies the
// previous generation loaded. The request boundary of the agent reads the
// ledger back through the agent identity, so the bodies a load published enter
// the next request of that agent and no other. A host that mounts no sink
// keeps the load's name-level answer exactly as before.
func NewSkillLoadSink(agent event.AgentID) knowledge.LoadSink {
	ledger := &skillLoadLedger{bodies: map[string]string{}}
	skillLoads.mu.Lock()
	skillLoads.ledgers[agent] = ledger
	skillLoads.mu.Unlock()
	return ledger.loaded
}

// loaded records one skill body the load tool published: the latest body of a
// skill replaces the previous one, and the first publish keeps its place in the
// load order. An empty name or body records nothing.
func (l *skillLoadLedger) loaded(name, body string) {
	if name == "" || body == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, held := l.bodies[name]; !held {
		l.order = append(l.order, name)
	}
	l.bodies[name] = body
}

// loads reports the bodies this ledger holds, in publish order.
func (l *skillLoadLedger) loads() []knowledge.LoadedSkill {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]knowledge.LoadedSkill, 0, len(l.order))
	for _, name := range l.order {
		out = append(out, knowledge.LoadedSkill{Name: name, Body: l.bodies[name]})
	}
	return out
}

// skillLoadsOf reports the skill_dirs bodies the agent's load tool published,
// in publish order. An agent beside no mounted sink reports none, so a
// deployment that wires no knowledge surface keeps its request shape.
func skillLoadsOf(agent event.AgentID) []knowledge.LoadedSkill {
	skillLoads.mu.Lock()
	ledger := skillLoads.ledgers[agent]
	skillLoads.mu.Unlock()
	if ledger == nil {
		return nil
	}
	return ledger.loads()
}

// projectedMainContext contributes the replayed projection of the session chain
// as the main context of the request: the window assembly of the registration
// the chain fixed, bounded to the entries that already ended.
func (a *requestAssembly) projectedMainContext(ctx context.Context) ([]model.Message, []string, error) {
	return projectedSegment(ctx, a.run, a.sequence.projection, a.sequence.window, a.sequence.endedPrefix(), a.boundary.ProjectionBudget)
}

// projectedSegment renders projected material under the window assembly: the
// leading prefix of the projection, mapped under the frozen event-type→role
// table. It is the one entry the request side presents projected material
// through — the main context of a request here, and the refs a delegated turn
// starts from through the same AssembleBudgetPrefix rule — so both degrade
// full→ticket→summary under one window assembly and neither grows a budget
// assembly of its own. The token budget is read off the one window the
// registration carries, so the request side never receives a second budget the
// registration does not know — except the window of no tokens, which names no
// budget at all and presents the whole projection (see projectionBudget). A
// negative prefix assembles the whole projection.
//
// The projection carries one entry per fact and keeps no call pairing, so the
// segment restores the pairing the wire requires before the request leaves:
// every tool entry gains the tool_call_id its fact answers under beside the
// assistant tool_calls message that issued the call (see restoreCallPairing).
func projectedSegment(ctx context.Context, run *Run, projection *replay.ReplayProjection, window FoldWindow, prefix, override int) ([]model.Message, []string, error) {
	if projection == nil {
		return nil, nil, nil
	}
	entries, err := projection.AssembleBudgetPrefix(ctx, projectionBudget(projection, window, prefix, override), prefix)
	if err != nil {
		return nil, nil, err
	}
	messages := make([]model.Message, 0, len(entries))
	sources := make([]string, 0, len(entries))
	paired := make(map[string]bool, len(entries))
	for _, entry := range entries {
		role := model.Role(entry.StatedRole())
		if role == "" {
			filled, err := agent.RoleOf(entry.Type)
			if err != nil {
				return nil, nil, err
			}
			role = filled
		}
		message := model.Message{Role: role, Content: presentedProjectionContent(entry)}
		if run != nil {
			run.restoreCallPairing(ctx, &messages, &message, entry, paired)
		}
		messages = append(messages, message)
		sources = append(sources, projectionSourceOf(entry.Type))
	}
	return messages, sources, nil
}

// projectionSourceOf names the input face one presented projection entry
// renders: the recovery hint has a face of its own, and every other projected
// entry is history the request carries.
func projectionSourceOf(entryType string) string {
	if entryType == replay.ProjectionTypeRecoveryHint {
		return agent.BudgetFaceRecoveryHint
	}
	return agent.BudgetFaceHistory
}

// projectionBudget reports the token budget one request presents its projected
// material under. A window naming a positive model window presents under that
// window's budget, so the budget keeps shaping presentation exactly as the
// window's fold threshold shapes the fold: the two are separate identities of
// one window. A window naming no tokens presents no bound at all: the budget is
// measured from the material itself — every entry's kept original text, at the
// packing ratio the assembly packs under — so the whole projection presents in
// full and no entry degrades to a ticket the declaration never asked for. The
// measure reads the original the projection keeps, never the view an assembly
// shaped, because the view belongs to that assembly alone.
func projectionBudget(projection *replay.ReplayProjection, window FoldWindow, prefix, override int) int {
	if override > 0 {
		return override
	}
	if window.MaxTokens > 0 {
		return window.MaxTokens
	}
	entries := projection.Entries()
	if prefix >= 0 && prefix < len(entries) {
		entries = entries[:prefix]
	}
	total := 0
	if hint := projection.RecoveryHint(); hint != nil {
		total += len(hint.Content)
	}
	for _, entry := range entries {
		text, kept := projection.Original(entry.Key)
		if !kept {
			text = entry.Content
		}
		total += len(text)
	}
	return total/replay.BudgetCharsPerToken + 1
}

// restoreCallPairing restores the call-result pairing one projected entry lost
// at the projection boundary. A tool entry arrives with its result text alone:
// the restore reads the fact the entry was minted from and repairs the message
// the wire requires — the tool_call_id and tool name the result answers under,
// beside the assistant tool_calls message that issued the call, emitted once
// per call right before the first tool entry answering it and carrying the
// arguments the model actually issued (see callArgumentsOf). An assistant entry
// whose fact committed native tool calls gains them back and marks the calls as
// paired, so no second assistant message is minted beside them. Material no
// live fact recovers — a restarted run whose facts left with its process —
// presents as it stands: the entry keeps no pairing and the protocol gate names
// the break rather than the request silently carrying a broken pair.
//
// - A fact the hot window trimmed from the live ledger still pairs: the storage tier holds the committed body, so the pairing follows the entry wherever the window cut it and a long chain presents whole.
func (r *Run) restoreCallPairing(ctx context.Context, out *[]model.Message, message *model.Message, entry replay.ProjectionEntry, paired map[string]bool) {
	fact, held := r.factOf(entry.Key)
	if !held {
		restored, err := r.CommittedEvent(ctx, entry.Key)
		if err == nil {
			fact, held = restored, true
		}
	}
	switch message.Role {
	case model.RoleAssistant:
		if held && len(fact.Message.ToolCalls) > 0 {
			message.ToolCalls = fact.Message.ToolCalls
			for _, call := range fact.Message.ToolCalls {
				paired[call.ID] = true
			}
		}
		return
	case model.RoleTool:
	default:
		return
	}
	callID, toolName := r.callIdentityOf(fact)
	if callID == "" {
		return
	}
	message.ToolID = callID
	message.ToolName = toolName
	if paired[callID] {
		return
	}
	paired[callID] = true
	*out = append(*out, model.Message{
		Role: model.RoleAssistant,
		ToolCalls: []model.ToolCall{{
			Type: "function",
			ID:   callID,
			Function: model.FunctionDefinitionParam{
				Name:      toolName,
				Arguments: []byte(r.callArgumentsOf(callID)),
			},
		}},
	})
}

// callIdentityOf reports the call identity one tool fact answers under: the
// tool message a result fact committed carries the identity directly, and a
// settled delivery answers under the identity its result fact answered a call
// with — exactly the tool role the frozen event-type→role table renders a
// replayed settle under. A fact carrying no recoverable identity answers none.
func (r *Run) callIdentityOf(fact event.Event) (callID, toolName string) {
	if fact.Message.ToolID != "" {
		return fact.Message.ToolID, fact.Message.ToolName
	}
	if fact.Type != event.TypeTaskSettled {
		return "", ""
	}
	taskID := fact.TaskID
	if taskID == "" {
		taskID = event.TaskID(fact.Meta[event.MetaKeyTaskID])
	}
	if taskID == "" {
		return "", ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.factOrder {
		result, held := r.facts[id]
		if !held || result.Type != event.TypeActionCommand || result.TaskID != taskID {
			continue
		}
		if result.Message.ToolID != "" {
			return result.Message.ToolID, result.Message.ToolName
		}
	}
	return "", ""
}

// callArgumentsOf reports the arguments one call was issued with, read from the
// request snapshot whose transcript carried the assistant tool_calls message of
// that call: the projection keeps no arguments and the snapshot is the record
// of what the model actually saw. The snapshots are the capture's own account —
// the same source the read face answers from — so a call is never paired from a
// second copy kept beside the capture. A call no snapshot carried reads as the
// empty argument object, so the pair stays complete on the wire instead of
// breaking the conversation the call opened.
func (r *Run) callArgumentsOf(callID string) string {
	for _, fixed := range r.requestSnapshots() {
		for _, message := range fixed.Snapshot.Messages {
			for _, call := range message.ToolCalls {
				if call.ID == callID && len(call.Function.Arguments) > 0 {
					return string(call.Function.Arguments)
				}
			}
		}
	}
	return "{}"
}

// factOf reports the fact one projected entry was minted from, as this run
// committed it, and whether the run still holds it: a restarted run reads a
// projection its facts left behind, and an identity it cannot resolve recovers
// no pairing.
func (r *Run) factOf(id event.EventID) (event.Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fact, held := r.facts[id]
	return fact, held
}

// presentedProjectionContent renders one assembled entry as the message the
// model reads. A product of the age fold speaks its one-line read ahead of the
// content it folded, so the model sees what kind of material follows; the label
// is composed here, from the entry's Summary, and never written into the product
// the fold minted.
func presentedProjectionContent(entry replay.ProjectionEntry) string {
	if entry.Type == replay.ProjectionTypeHistorySummary && entry.Summary != "" {
		return entry.Summary + ": " + entry.Content
	}
	return entry.Content
}

// environmentBlockGenerations counts the environment blocks the request boundary
// has rendered in this process, so the block's rewrite frequency is a measured
// face for a later throttle decision rather than an inference from the lines.
var environmentBlockGenerations atomic.Int64

// EnvironmentBlockGenerations reports how many environment blocks the request
// boundary has rendered in this process so far.
func EnvironmentBlockGenerations() int64 { return environmentBlockGenerations.Load() }

// environmentBlockDebug renders one line of the environment-block face at DEBUG:
// the trigger reason, the agent and the content version one generation named.
// One line per generation keeps the rewrite frequency observable, and the line
// carries no task text.
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
// behind its transcript, for an agent that declares it: the agent the request
// runs as, the tool, skill and MCP faces available to it, the content version
// its prompt files fixed and the asynchronous work it holds open at this
// boundary. The block is the closing section of the request's system material,
// rendered as a named Markdown section the way the fixed prompt renders its own:
// the system channel is what the family adapters turn into their own system
// framing, so the block never poses as conversation. Everything in it changes
// with the run while the material ahead of it — the fixed prompt and the skill
// bodies — does not, so the changing part never moves the stable prefix it
// follows, and no marker of our own is invented beside the adapter's framing.
func (r *Run) environmentBlock(ctx context.Context, boundary agent.RequestBoundary, promptVersion string) string {
	spec := r.agentSpecOf(boundary.Agent)
	environmentBlockGenerations.Add(1)
	environmentBlockDebug("tagent: environment block generated reason=request-boundary agent=%s", boundary.Agent)
	var b strings.Builder
	b.WriteString("## Environment\n")
	fmt.Fprintf(&b, "- agent: %s\n", boundary.Agent)
	if modelName := modelNameOf(boundary.Binding); modelName != "" {
		fmt.Fprintf(&b, "- model: %s\n", modelName)
	}
	fmt.Fprintf(&b, "- tools: %s\n", listOrNone(toolNamesOf(boundary.Binding, boundary.Chain.SessionID)))
	fmt.Fprintf(&b, "- skills: %s\n", listOrNone(narrowedSkillsOf(boundary.Binding)))
	fmt.Fprintf(&b, "- mcp: %s\n", listOrNone(visibleMCPOf(spec, boundary.Binding)))
	fmt.Fprintf(&b, "- prompt_version: %s\n", versionOrNone(promptVersion))
	return strings.TrimSuffix(b.String(), "\n")
}

// toolNamesOf reports the tool names one binding's request carries at one
// session, in the order the request emits them: the published tools in name
// order first, the tools registered after publication behind them. It reads the
// session-scoped face the request's Tools come from, so reflection-only tools
// are listed only for the dedicated reflection session and the model never
// reads a name it cannot call at its own session.
func toolNamesOf(binding state.AgentBinding, session event.SessionID) []string {
	_, order := state.ToolFace(binding, session)
	return order
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

// versionOrNone renders the content version the request boundary fixed as the
// environment block names it, and a named empty version rather than a blank
// field, so a request that fixed no content version still says so.
func versionOrNone(version string) string {
	if version == "" {
		return "(none)"
	}
	return version
}
