// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
//
// The configuration reload of a booted host: the document is read again, the
// declaration it carries is published through the one publication entry, and
// every agent whose lease holds a version other than the one the document names
// has the move judged as one configuration change of the model dimension on its
// own pipeline. The lease mounted on the agent runs the rebuild, publication and
// installation order, so a reload never installs a model outside the
// event-driven configuration path the run already owns.
//
// The document is the host's own dialect, so a reload reads it with the same
// strict decoder Boot reads it with; a document the decoder refuses changes
// nothing at all.
package assembly

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/runtime/state"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// ErrHostReloadStructural is the family of reload refusals the running host
// cannot take into service. The entry agent and the declared agent set are no
// longer refused by it: a reload publishes them as one generation, builds the
// native pieces of an added agent and judges the next turn and delivery against
// the generation in effect. What stays in the family is the change whose only
// consumer Boot built — the restart-required fields — so every face that maps the
// family answers the refusal the same way.
var ErrHostReloadStructural = errors.New("tagent: the reload cannot apply its change without a restart")

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
	// ConfigChanges lists the configuration dimensions the reload judged and
	// filed as config_change facts, one per agent dimension that moved. The
	// model dimension is reported as the model move beside this list and is
	// never restated here.
	ConfigChanges []hostReloadConfigChange `json:"config_changes"`
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
	// Fact is the identity of the committed model configuration change, so a
	// caller reads the judgment on the agent's own pipeline by name instead of
	// guessing the content-addressed key; it stays empty when the judgment could
	// not be filed.
	Fact string `json:"fact,omitempty"`
}

// hostReloadConfigChange is one configuration dimension a reload judged: the
// agent whose declaration moved, the dimension that moved and the identity of
// the committed config change fact. The description is carried to the filing
// step and never serialized in the receipt; the committed fact is where a reader
// reads what changed.
type hostReloadConfigChange struct {
	Agent     string `json:"agent"`
	Dimension string `json:"dimension"`
	// Fact is the identity of the committed config change fact, so a caller
	// reads the judgment on the chain by name instead of guessing the
	// content-addressed key; it stays empty when the judgment could not be
	// filed.
	Fact string `json:"fact,omitempty"`

	description string
}

// hostModelBuilder builds the native model of one agent from the configuration
// source the host holds: Boot builds the first generation, and a reload replaces
// the source before the model configuration change facts are filed, so a lease
// rebuild reads the provider registry of the document in effect instead of a
// stale copy.
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
// carries, and files the configuration facts the move judged: one config change
// per declaration dimension that moved and one model configuration change per
// agent whose lease holds a version the document does not name. The publication is
// the one entry the run already owns; the model change is the configuration
// change family's model dimension, committed after the publication settled —
// including a carried document, so a move whose install failed is judged again.
// The lease mounted on each agent runs its own rebuild, publication and
// installation order and decides whether the new version takes effect.
//
// - The generation declares the agent set: an agent both documents name keeps the contribution already in service, an added agent is constructed through the same native path Boot ran, and its model lease is built beside the publication so the address serves its first turn on the model the declaration names.
// - The leases are mounted only once the generation that declared the agents published: a refused reload attaches nothing, and the next publication reads the extended table through the same lease path Boot filled.
func (h *Host) Reload(ctx context.Context) (hostReloadReport, error) {
	report := hostReloadReport{
		Reloaded:      false,
		ModelChanges:  []hostReloadModelChange{},
		ConfigChanges: []hostReloadConfigChange{},
	}
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
	added := make([]event.AgentID, 0, len(decl.Agents))
	for _, spec := range decl.Agents {
		id := event.AgentID(spec.ID)
		if held, ok := natives[id]; ok {
			next[id] = held
			continue
		}
		if h.nativeFor == nil {
			return report, fmt.Errorf("tagent: the host carries no native construction to assemble the added agent %q", spec.ID)
		}
		native, err := h.nativeFor(ctx, doc, id)
		if err != nil {
			return report, err
		}
		next[id] = native
		added = append(added, id)
	}
	extensions, err := h.modelLeaseExtensions(next, doc, added)
	if err != nil {
		return report, err
	}
	previousDecl, err := previous.Declaration()
	if err != nil {
		return report, err
	}
	changes := h.modelChanges(decl, doc)
	configChanges := configChangesOf(previousDecl, decl)
	outcome, err := h.run.Publish(ctx, decl, next)
	if err != nil {
		return report, err
	}
	report.Publication = hostReloadPublication{
		Generation: outcome.Publication.Generation,
		Published:  outcome.Result != state.PublishUnchanged,
	}
	h.run.installModelLeases(extensions)
	h.models.set(doc)
	h.mu.Lock()
	h.doc = doc
	h.natives = next
	h.mu.Unlock()
	report.Reloaded = true
	var configFailed, modelFailed []error
	if outcome.Result != state.PublishUnchanged {
		for _, change := range configChanges {
			fact := event.ConfigChangeFor(event.AgentID(change.Agent), change.Dimension, outcome.Publication.Generation, change.description)
			key, commitErr := h.run.commitFactChecked(ctx, fact.Event(time.Now().UTC()))
			if commitErr != nil {
				configFailed = append(configFailed, fmt.Errorf("tagent: file the %s configuration change of agent %q: %w", change.Dimension, change.Agent, commitErr))
				continue
			}
			change.Fact = string(key)
			report.ConfigChanges = append(report.ConfigChanges, change)
		}
	}
	for _, change := range changes {
		key, err := h.run.noteModelConfigurationChange(ctx, event.AgentID(change.Agent), outcome.Publication.Generation, "", change.To)
		if err != nil {
			h.run.recordLeaseLoss(err)
			modelFailed = append(modelFailed, fmt.Errorf("tagent: file the model configuration change of agent %q: %w", change.Agent, err))
			continue
		}
		change.Fact = string(key)
		report.ModelChanges = append(report.ModelChanges, change)
	}
	switch {
	case len(configFailed) > 0 && len(modelFailed) > 0:
		failed := append(append([]error{}, configFailed...), modelFailed...)
		return report, fmt.Errorf("tagent: %d configuration changes and %d model changes failed: %w", len(configFailed), len(modelFailed), errors.Join(failed...))
	case len(configFailed) > 0:
		return report, fmt.Errorf("tagent: %d of %d configuration changes failed: %w", len(configFailed), len(configChanges), errors.Join(configFailed...))
	case len(modelFailed) > 0:
		return report, fmt.Errorf("tagent: %d of %d model changes failed: %w", len(modelFailed), len(changes), errors.Join(modelFailed...))
	}
	return report, nil
}

// modelLeaseExtensions constructs the model leases of the agents one publication
// adds, through the same lease the assembly attaches at Boot: the native model the
// document names beside the version in effect and the rebuild the document's model
// builder runs. The leases are built before the publication, so a model that
// cannot be leased refuses the reload before a generation enters service; they are
// mounted on the run only after the generation that declared them published.
func (h *Host) modelLeaseExtensions(natives map[event.AgentID]NativeAgent, doc *hostDocument, added []event.AgentID) ([]modelLeaseBinding, error) {
	extensions := make([]modelLeaseBinding, 0, len(added))
	for _, id := range added {
		lease, err := rl.NewModelLease(natives[id].Model, doc.modelVersion(string(id)))
		if err != nil {
			return nil, fmt.Errorf("tagent: attach the model lease of agent %q: %w", id, err)
		}
		extension, err := h.run.buildModelLeaseBinding(id, leaseState{lease: lease, rebuild: h.models.rebuild(id)})
		if err != nil {
			return nil, err
		}
		extensions = append(extensions, extension)
	}
	return extensions, nil
}

// configChangesOf lists the configuration change facts one published reload
// files, one per declared dimension that moved. The organization dimensions come
// first — the entry agent the organization speaks to and every agent the declared
// set gained or lost — and then, per declared agent, the prompt sources, the
// config-declared delegation assembly and the remaining binding. The model
// reference is deliberately outside these comparisons: a move it makes is the
// model dimension's own change filed beside these, never a prompt, assembly or
// binding entry.
//
// The order is the declaration order of the agents, so the same edit states the
// same facts. The judgment is filed only beside a publication that took effect, so
// a document the reload refuses files none of these facts: the judgment states
// every declaration dimension in one place and never invents a fact for a
// refusal.
func configChangesOf(previous, next *config.Declaration) []hostReloadConfigChange {
	if previous == nil || next == nil {
		return nil
	}
	before := make(map[event.AgentID]config.AgentSpec, len(previous.Agents))
	for _, spec := range previous.Agents {
		before[event.AgentID(spec.ID)] = spec
	}
	after := make(map[event.AgentID]config.AgentSpec, len(next.Agents))
	for _, spec := range next.Agents {
		after[event.AgentID(spec.ID)] = spec
	}
	changes := make([]hostReloadConfigChange, 0, len(next.Agents)+1)
	entry := strings.TrimSpace(next.Entry)
	if entry != strings.TrimSpace(previous.Entry) {
		changes = append(changes, hostReloadConfigChange{
			Agent:       entry,
			Dimension:   event.ConfigChangeDimensionOrgEntry,
			description: "entry moved from " + strconv.Quote(strings.TrimSpace(previous.Entry)) + " to " + strconv.Quote(entry),
		})
	}
	for _, spec := range next.Agents {
		if _, held := before[event.AgentID(spec.ID)]; held {
			continue
		}
		changes = append(changes, hostReloadConfigChange{
			Agent:       spec.ID,
			Dimension:   event.ConfigChangeDimensionOrgAgents,
			description: "agent " + strconv.Quote(spec.ID) + " was added to the declared agent set",
		})
	}
	for _, spec := range previous.Agents {
		if _, held := after[event.AgentID(spec.ID)]; held {
			continue
		}
		changes = append(changes, hostReloadConfigChange{
			Agent:       spec.ID,
			Dimension:   event.ConfigChangeDimensionOrgAgents,
			description: "agent " + strconv.Quote(spec.ID) + " was removed from the declared agent set",
		})
	}
	organization := organizationChangeDescription(previous, next)
	for _, spec := range next.Agents {
		held, ok := before[event.AgentID(spec.ID)]
		if !ok {
			continue
		}
		if description := promptChangeDescription(held, spec); description != "" {
			changes = append(changes, hostReloadConfigChange{
				Agent:       spec.ID,
				Dimension:   event.ConfigChangeDimensionPrompt,
				description: "agent " + spec.ID + " " + description,
			})
		}
		if description := assemblyChangeDescription(held, spec); description != "" {
			changes = append(changes, hostReloadConfigChange{
				Agent:       spec.ID,
				Dimension:   event.ConfigChangeDimensionOrgAssembly,
				description: "agent " + spec.ID + " " + description,
			})
		}
		binding := bindingChangeDescription(held, spec)
		if spec.ID == entry && organization != "" {
			if binding == "" {
				binding = organization
			} else {
				binding += "; " + organization
			}
		}
		if binding != "" {
			changes = append(changes, hostReloadConfigChange{
				Agent:       spec.ID,
				Dimension:   event.ConfigChangeDimensionBinding,
				description: "agent " + spec.ID + " " + binding,
			})
		}
	}
	return changes
}

// assemblyChangeDescription states the config-declared delegation assembly move
// between two declarations of one agent, and the empty string when the assembly
// did not move: the agent references the declaration wires — the declared target,
// the tool name it is reached by, the description file it reads and the event
// params it hands over, in declaration order. A reference the declaration does
// not name as an agent is a preset the host assembles itself and belongs to the
// binding, and the resolved target's own declaration is judged on that agent's
// own dimensions, so neither is restated here.
func assemblyChangeDescription(before, after config.AgentSpec) string {
	old := agentReferences(before.Tools)
	next := agentReferences(after.Tools)
	if reflect.DeepEqual(old, next) {
		return ""
	}
	return "delegation assembly changed: [" + strings.Join(referenceLabels(old), ", ") + "] -> [" + strings.Join(referenceLabels(next), ", ") + "]"
}

// agentReferences lists the agent references one declaration wires, in
// declaration order: a tool reference carrying the declared agent parameter is
// the reference that grants a target tool, while every other reference is a
// preset the host assembles itself.
func agentReferences(refs []config.ToolRef) []config.ToolRef {
	out := make([]config.ToolRef, 0, len(refs))
	for _, ref := range refs {
		if agentRef(ref) != "" {
			out = append(out, ref)
		}
	}
	return out
}

// declaredToolRefs lists the references of one declaration that are not agent
// references: the presets the host assembles itself. The agent references are the
// config-declared delegation assembly and are judged by that dimension.
func declaredToolRefs(refs []config.ToolRef) []config.ToolRef {
	out := make([]config.ToolRef, 0, len(refs))
	for _, ref := range refs {
		if agentRef(ref) != "" {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// referenceLabels states one declaration's agent references as the labels a
// change description reads: the tool name beside every parameter the reference
// declares, in parameter-key order, so the same edit always states the same move.
func referenceLabels(refs []config.ToolRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		keys := make([]string, 0, len(ref.Params))
		for key := range ref.Params {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+"="+ref.Params[key])
		}
		out = append(out, strconv.Quote(ref.Name)+"("+strings.Join(parts, ", ")+")")
	}
	return out
}

// promptChangeDescription states the prompt source move between two declarations
// of one agent, and the empty string when the prompt sources did not move. The
// order is the declaration order, so the same edit states the same prompt move.
func promptChangeDescription(before, after config.AgentSpec) string {
	old := promptPaths(before.Files)
	next := promptPaths(after.Files)
	if reflect.DeepEqual(old, next) {
		return ""
	}
	return "prompt source changed: [" + strings.Join(old, ", ") + "] -> [" + strings.Join(next, ", ") + "]"
}

// promptPaths lists the prompt sources one agent declaration names.
func promptPaths(files []config.FileRef) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		if file.Kind != "" && file.Kind != config.FileKindPrompt {
			continue
		}
		paths = append(paths, file.Path)
	}
	return paths
}

// bindingChangeDescription states the declaration binding move between two
// declarations of one agent, and the empty string when no binding field moved.
// The model reference, the prompt sources and the config-declared delegation
// assembly are read by their own dimensions and are never restated here: the tool
// comparison covers the preset references the host assembles itself, not the
// agent references that dimension owns.
func bindingChangeDescription(before, after config.AgentSpec) string {
	changed := make([]string, 0, 6)
	note := func(label string, moved bool) {
		if moved {
			changed = append(changed, label)
		}
	}
	note("parent", before.ParentID != after.ParentID)
	note("tools", !reflect.DeepEqual(declaredToolRefs(before.Tools), declaredToolRefs(after.Tools)))
	note("plugins", !reflect.DeepEqual(before.Plugins, after.Plugins))
	note("task_policy", before.TaskPolicy != after.TaskPolicy)
	note("params", !reflect.DeepEqual(before.Params, after.Params))
	note("final_round", before.FinalRound != after.FinalRound)
	note("replay_window", before.MaxTokens != after.MaxTokens ||
		before.CompressThreshold != after.CompressThreshold ||
		before.KeepRecentTasks != after.KeepRecentTasks)
	note("environment_block", before.EnvironmentBlock != after.EnvironmentBlock)
	note("sampling", !reflect.DeepEqual(before.Temperature, after.Temperature) ||
		!reflect.DeepEqual(before.ThinkingEnabled, after.ThinkingEnabled) ||
		before.ReasoningEffort != after.ReasoningEffort)
	note("max_tool_iterations", before.MaxToolIterations != after.MaxToolIterations)
	note("max_calls_per_response", before.MaxCallsPerResponse != after.MaxCallsPerResponse)
	note("long_context_tokens", before.LongContextTokens != after.LongContextTokens)
	note("capture_budget_mb", before.CaptureBudgetMB != after.CaptureBudgetMB)
	note("memory", !reflect.DeepEqual(before.Memory, after.Memory))
	note("meditation", !reflect.DeepEqual(before.Meditation, after.Meditation))
	note("file", !reflect.DeepEqual(before.File, after.File))
	note("exec", !reflect.DeepEqual(before.Exec, after.Exec))
	if len(changed) == 0 {
		return ""
	}
	return "binding changed: " + strings.Join(changed, ", ")
}

// organizationChangeDescription states the organization sections that moved
// between two declarations: the MCP servers, the organization references and the
// version baseline. The entry agent and the declared set are stated by their own
// dimensions, so they are never restated here.
func organizationChangeDescription(before, after *config.Declaration) string {
	changed := make([]string, 0, 3)
	if !reflect.DeepEqual(before.Servers, after.Servers) {
		changed = append(changed, "mcp_servers")
	}
	if !reflect.DeepEqual(before.Refs, after.Refs) {
		changed = append(changed, "refs")
	}
	if before.Baseline != after.Baseline {
		changed = append(changed, "baseline")
	}
	if len(changed) == 0 {
		return ""
	}
	return "organization binding changed: " + strings.Join(changed, ", ")
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

// compatibleWith refuses a document that moves a field only Boot reads. It runs
// before the document in effect is swapped, so a refused reload leaves the
// document and the natives exactly as they were. The entry agent and the declared
// agent set are not refused by themselves: the reload publishes them as one
// generation, builds the native pieces of an added agent and reads the generation
// in effect for the next turn and delivery. An added agent that carries a section
// whose consumer only Boot built is still refused by name, because construction of
// the agent does not rebuild that consumer.
func (d *hostDocument) compatibleWith(next *hostDocument) error {
	if next == nil {
		return fmt.Errorf("%w: the reload carries no document", ErrHostReloadStructural)
	}
	if changed := d.restartRequiredChanges(next); len(changed) > 0 {
		return fmt.Errorf("%w: %s changed", ErrHostReloadRestartRequired, strings.Join(changed, ", "))
	}
	return nil
}

// restartRequiredChanges lists the fields the next document moves while the only
// consumer of each was built at Boot: the storage tier, the request timeout, the
// observation face, the governance rules and, per agent, the host-contributed tool
// declarations, the skill roots, the read root, the file surface, the execution
// face, the memory section and the meditation section. Boot constructs those
// consumers once beside the natives it contributes, and a reload hands the natives
// back as they were, so a reload that took a moved value would answer success for
// a change no consumer takes.
//
// The tool comparison covers the host-contributed tool references alone, the same
// way the declaration maps them: an agent reference is the configuration's own
// delegation assembly, built afresh for every published generation, so it moves
// with the generation instead of being refused here - the org.assembly dimension
// states that move. Per agent the comparison covers the fields of an agent both
// documents declare. An added agent's tool, skill, read and file sections are
// built through the same native path Boot ran, so they take effect with its first
// generation; its execution, memory and meditation sections are not, because the
// execution manager, the memory plan and the reflection driver are built once at
// Boot. Carrying one of those on an added agent is therefore a restart-required
// change refused by name, not a section the new binding would take. A removed
// agent's sections leave with the generation that dropped it, so it is not
// compared.
// Every path is the YAML key an operator searches the configuration source for,
// and the order is fixed here and then by agent id, so the same edit always
// answers the same names.
//
// - An added agent's exec, memory and meditation consumers were assembled once at Boot over the boot declaration, so a section it carries has no live reader and is compared against the zero value.
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
	for _, id := range d.agentIDsIn(next) {
		before, held := d.Agents[id]
		after := next.Agents[id]
		if !held {
			compare("agents."+id+".exec", config.ExecSpec{}, after.Exec)
			compare("agents."+id+".memory", config.MemorySpec{}, after.Memory)
			compare("agents."+id+".meditation", config.MeditationSpec{}, after.Meditation)
			continue
		}
		compare("agents."+id+".tools", hostToolDeclarations(before.Tools), hostToolDeclarations(after.Tools))
		compare("agents."+id+".skill_dirs", before.SkillDirs, after.SkillDirs)
		compare("agents."+id+".read_root", before.ReadRoot, after.ReadRoot)
		compare("agents."+id+".file", before.File, after.File)
		compare("agents."+id+".exec", before.Exec, after.Exec)
		compare("agents."+id+".memory", before.Memory, after.Memory)
		compare("agents."+id+".meditation", before.Meditation, after.Meditation)
	}
	return changed
}

// hostToolDeclarations lists the tool references of one agent section whose only
// consumer Boot built: the host-contributed tool instances, in declaration order.
// An agent reference is the configuration's own delegation assembly and is built
// afresh for every published generation, so it is judged by the org.assembly
// dimension instead of being refused here.
func hostToolDeclarations(refs []hostToolRef) []hostToolRef {
	out := make([]hostToolRef, 0, len(refs))
	for _, ref := range refs {
		if namesDeclaredAgent(ref) {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// namesDeclaredAgent reports whether one declared tool reference names a declared
// agent, the way the declaration mapping reads it: the explicit agent kind, or a
// bare reference carrying a target agent.
func namesDeclaredAgent(ref hostToolRef) bool {
	kind := strings.TrimSpace(ref.Kind)
	if kind == "agent" {
		return true
	}
	return kind == "" && strings.TrimSpace(ref.Agent) != ""
}

// agentIDsIn lists every agent the next document declares, in name order: the
// fields an agent both documents name are compared, and the restart-only sections
// an added agent carries are judged against their zero value. A removed agent is
// absent from the next document and is not judged here, because its sections leave
// with the generation that dropped it.
func (d *hostDocument) agentIDsIn(next *hostDocument) []string {
	ids := make([]string, 0, len(next.Agents))
	for id := range next.Agents {
		ids = append(ids, id)
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
