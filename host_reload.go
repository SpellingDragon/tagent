// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
//
// The configuration reload of a booted host: the document is read again, the
// declaration it carries is published through the one publication entry, and
// every agent whose model reference moved has the move judged as a model
// replacement fact on its own pipeline. The lease mounted on the agent runs the
// rebuild, publication and installation order, so a reload never installs a
// model outside the event-driven replacement path the run already owns.
//
// The document is the host's own dialect, so a reload reads it with the same
// strict decoder Boot reads it with; a document the decoder refuses changes
// nothing at all.
package tagent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/runtime/state"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// ErrHostReloadStructural reports a reload that changes the entry agent or the
// declared agent set. Boot wires the model leases, the reflection plan and the
// contributed tool faces of the declared agents once, so a structural change is
// refused by name instead of being half-applied; a host that needs another
// organization is restarted with it.
var ErrHostReloadStructural = errors.New("tagent: the reload changes the declared agent set")

// ErrHostReloadRestartRequired reports a reload that moves a field whose only
// consumer Boot built: the storage tier, the request timeout, the observation
// face, the governance rules, and per agent the tool, skill, read, file,
// execution, memory and meditation sections. No path in a running host rebuilds
// those consumers, so the reload refuses the change by name rather than
// answering success for a value nothing reads; a host that needs it is restarted
// with the document.
var ErrHostReloadRestartRequired error = errHostReloadRestartRequired{}

// errHostReloadRestartRequired is the named refusal of a restart-only change.
type errHostReloadRestartRequired struct{}

// Error names the restart the refused change needs.
func (errHostReloadRestartRequired) Error() string {
	return "tagent: the reload requires a restart"
}

// Unwrap attaches the refusal to the structural family: a restart-only change is
// one face Boot wired whose replacement has no path, so every face that maps the
// structural refusal answers this one the same way.
func (errHostReloadRestartRequired) Unwrap() error { return ErrHostReloadStructural }

// hostReloadReport is the answer of one reload: the publication result beside
// the model changes the reload judged.
type hostReloadReport struct {
	// Reloaded states that the document was read and published.
	Reloaded bool `json:"reloaded"`
	// Publication states what the one publication entry did with the candidate:
	// the generation in effect after the attempt and whether the attempt
	// published a new one. A reload carrying the content already in effect
	// enters no generation, so the receipt answers that instead of leaving a
	// reader to infer it from an empty model change list.
	Publication hostReloadPublication `json:"publication"`
	// ModelChanges lists the model reference moves the reload judged, one per
	// agent whose lease moved.
	ModelChanges []hostReloadModelChange `json:"model_changes"`
}

// hostReloadPublication is the publication receipt of one reload: the generation
// in effect once the attempt ended, and whether that attempt published it.
type hostReloadPublication struct {
	Generation uint64 `json:"generation"`
	// Published states whether the attempt entered service as a new generation;
	// false means the candidate carried the content already in effect, so
	// nothing was rebuilt and the generation stayed where it was.
	Published bool `json:"published"`
}

// hostReloadModelChange is one model reference move judged by a reload: the
// vendor-neutral version in effect, the version the document now names, and the
// agent whose lease moves between them.
type hostReloadModelChange struct {
	Agent string `json:"agent"`
	From  string `json:"from"`
	To    string `json:"to"`
	// Fact is the identity of the committed replacement fact, so a caller reads
	// the judgment on the agent's own pipeline by name instead of guessing the
	// content-addressed key; it stays empty when the judgment could not be filed.
	Fact string `json:"fact,omitempty"`
}

// hostModelBuilder builds the native model of one agent from the configuration
// source the host holds: Boot builds the first generation, and a reload replaces
// the source before the replacement facts are filed, so a lease rebuild reads
// the provider registry of the document in effect instead of a stale copy.
type hostModelBuilder struct {
	mu     sync.RWMutex
	doc    *hostDocument
	client *http.Client
}

// newHostModelBuilder returns the builder of one boot over the document it
// loaded and the HTTP client every model of the host runs under.
func newHostModelBuilder(doc *hostDocument, client *http.Client) *hostModelBuilder {
	return &hostModelBuilder{doc: doc, client: client}
}

// set replaces the document the builder reads; the next rebuild runs over it.
func (b *hostModelBuilder) set(doc *hostDocument) {
	b.mu.Lock()
	b.doc = doc
	b.mu.Unlock()
}

// document reports the document the builder currently reads.
func (b *hostModelBuilder) document() *hostDocument {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.doc
}

// rebuild returns the rebuild step of one agent's lease: the step receives the
// version a replacement fact asks for, splits it onto the provider and the model
// name the new document declares, and builds the model through the same
// reentrant constructor Boot uses.
func (b *hostModelBuilder) rebuild(agentID event.AgentID) func(string) (trpcmodel.Model, error) {
	return func(version string) (trpcmodel.Model, error) {
		provider, name, err := splitHostModelVersion(version)
		if err != nil {
			return nil, err
		}
		doc := b.document()
		if doc == nil {
			return nil, fmt.Errorf("tagent: the model builder holds no document")
		}
		return doc.buildModel(string(agentID), provider, name, b.client)
	}
}

// Reload reads the configuration source again, publishes the declaration it
// carries, and files the model replacement of every agent whose model reference
// moved. The publication is the one entry the run already owns; the replacement
// facts are the same facts a declaration-driven change files, so the lease
// mounted on each agent decides whether the new version takes effect.
func (h *Host) Reload(ctx context.Context) (hostReloadReport, error) {
	report := hostReloadReport{Reloaded: false, ModelChanges: []hostReloadModelChange{}}
	if h == nil || h.run == nil {
		return report, ErrHostClosed
	}
	h.reloadMu.Lock()
	defer h.reloadMu.Unlock()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return report, ErrHostClosed
	}
	configPath, previous, natives := h.configPath, h.doc, h.natives
	h.mu.Unlock()
	if previous == nil {
		return report, fmt.Errorf("tagent: the host holds no configuration source to reload")
	}
	doc, err := loadHostDocument(configPath)
	if err != nil {
		return report, err
	}
	decl, err := doc.Declaration()
	if err != nil {
		return report, err
	}
	if err := previous.compatibleWith(doc); err != nil {
		return report, err
	}
	next := make(map[event.AgentID]NativeAgent, len(decl.Agents))
	for _, spec := range decl.Agents {
		id := event.AgentID(spec.ID)
		held, ok := natives[id]
		if !ok {
			return report, fmt.Errorf("%w: agent %q is not wired by this host", ErrHostReloadStructural, spec.ID)
		}
		next[id] = held
	}
	changes := h.modelChanges(decl, doc)
	outcome, err := h.run.Publish(ctx, decl, next)
	if err != nil {
		return report, err
	}
	report.Publication = hostReloadPublication{
		Generation: outcome.Publication.Generation,
		Published:  outcome.Result != state.PublishUnchanged,
	}
	h.models.set(doc)
	h.mu.Lock()
	h.doc = doc
	h.natives = next
	h.mu.Unlock()
	report.Reloaded = true
	var failed []error
	for _, change := range changes {
		key, err := h.run.NoteModelChange(event.AgentID(change.Agent), "", change.To, event.ModelReplacementSourceConfig)
		if err != nil {
			failed = append(failed, fmt.Errorf("tagent: move the model of agent %q: %w", change.Agent, err))
			continue
		}
		change.Fact = string(key)
		report.ModelChanges = append(report.ModelChanges, change)
	}
	if len(failed) > 0 {
		return report, fmt.Errorf("tagent: %d of %d model changes failed: %w", len(failed), len(changes), errors.Join(failed...))
	}
	return report, nil
}

// modelChanges lists the model reference moves one reload judges: every declared
// agent whose lease holds a version other than the version the new document
// names. The version in effect is read from the lease itself, so a change that
// failed to install is judged again by the next reload instead of being read as
// already applied.
func (h *Host) modelChanges(decl *config.Declaration, doc *hostDocument) []hostReloadModelChange {
	changes := make([]hostReloadModelChange, 0, len(decl.Agents))
	for _, spec := range decl.Agents {
		held := h.run.leaseOf(event.AgentID(spec.ID))
		if held == nil || held.lease == nil {
			continue
		}
		version, _ := heldModelVersion(held.lease)
		to := doc.modelVersion(spec.ID)
		if to == "" || to == version {
			continue
		}
		changes = append(changes, hostReloadModelChange{Agent: spec.ID, From: version, To: to})
	}
	return changes
}

// compatibleWith refuses a document that changes the entry agent, the declared
// agent set or a field only Boot reads. It runs before the natives the host
// contributed are reused and before the document in effect is swapped, so a
// refused reload leaves both exactly as they were.
func (d *hostDocument) compatibleWith(next *hostDocument) error {
	if next == nil {
		return fmt.Errorf("%w: the reload carries no document", ErrHostReloadStructural)
	}
	if entry := strings.TrimSpace(d.Entry); entry != strings.TrimSpace(next.Entry) {
		return fmt.Errorf("%w: the entry moves from %q to %q", ErrHostReloadStructural, entry, strings.TrimSpace(next.Entry))
	}
	if len(d.Agents) != len(next.Agents) {
		return fmt.Errorf("%w: the document declares %d agents and the reload declares %d", ErrHostReloadStructural, len(d.Agents), len(next.Agents))
	}
	for id := range next.Agents {
		if _, ok := d.Agents[id]; !ok {
			return fmt.Errorf("%w: agent %q is added", ErrHostReloadStructural, id)
		}
	}
	for id := range d.Agents {
		if _, ok := next.Agents[id]; !ok {
			return fmt.Errorf("%w: agent %q is removed", ErrHostReloadStructural, id)
		}
	}
	if changed := d.restartRequiredChanges(next); len(changed) > 0 {
		return fmt.Errorf("%w: %s changed", ErrHostReloadRestartRequired, strings.Join(changed, ", "))
	}
	return nil
}

// restartRequiredChanges lists the fields the next document moves while the only
// consumer of each was built at Boot: the storage tier, the request timeout, the
// observation face, the governance rules and, per agent, the tool declarations,
// the skill roots, the read root, the file surface, the execution face, the
// memory section and the meditation section. Boot constructs those consumers
// once beside the natives it contributes, and a reload hands the natives back as
// they were, so a reload that took a moved value would answer success for a
// change no consumer takes.
//
// Every path is the YAML key an operator searches the configuration source for,
// and the order is fixed here and then by agent id, so the same edit always
// answers the same names.
func (d *hostDocument) restartRequiredChanges(next *hostDocument) []string {
	changed := make([]string, 0, 4)
	compare := func(path string, before, after any) {
		if !reflect.DeepEqual(before, after) {
			changed = append(changed, path)
		}
	}
	compare("storage.type", d.Storage.Type, next.Storage.Type)
	compare("storage.root", d.Storage.Root, next.Storage.Root)
	compare("request_timeout_seconds", d.TimeoutSeconds, next.TimeoutSeconds)
	compare("governance", d.Governance, next.Governance)
	compare("telemetry", d.Telemetry, next.Telemetry)
	for _, id := range d.declaredAgentIDs(next) {
		before, after := d.Agents[id], next.Agents[id]
		compare("agents."+id+".tools", before.Tools, after.Tools)
		compare("agents."+id+".skill_dirs", before.SkillDirs, after.SkillDirs)
		compare("agents."+id+".read_root", before.ReadRoot, after.ReadRoot)
		compare("agents."+id+".file", before.File, after.File)
		compare("agents."+id+".exec", before.Exec, after.Exec)
		compare("agents."+id+".memory", before.Memory, after.Memory)
		compare("agents."+id+".meditation", before.Meditation, after.Meditation)
	}
	return changed
}

// declaredAgentIDs lists every agent id the two documents declare, in name
// order: the compared fields belong to agents both documents name.
func (d *hostDocument) declaredAgentIDs(next *hostDocument) []string {
	ids := make([]string, 0, len(d.Agents))
	for id := range d.Agents {
		ids = append(ids, id)
	}
	for id := range next.Agents {
		if _, held := d.Agents[id]; !held {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// heldModelVersion reports the model version one agent's lease holds, and the
// empty version beside a lease that names none. The current identity is split at
// the model name it begins with, so a model name or version that carries the
// identity separator is never split at a guessed position.
func heldModelVersion(lease *rl.ModelLease) (string, bool) {
	if lease == nil {
		return "", false
	}
	current := lease.CurrentVersion()
	name := lease.Current().Info().Name
	if current == name {
		return "", true
	}
	version, found := strings.CutPrefix(current, name+hostModelIdentitySeparator)
	return version, found
}
