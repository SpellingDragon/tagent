package assembly

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	mcpgw "github.com/SpellingDragon/tagent/tool/mcp"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// publicationRecords is the publication recorder of one assembled run: it
// commits every record as one fact of the configuration chain, so a restart
// reads the publications the previous process made instead of starting beside an
// empty ledger. The chain is the durable fact surface of the configured storage
// tier, or an in-process log when the run is assembled beside no tier; either
// way the sole publication entry commits through this one recorder, which keeps
// no copy of a record: the chain fact is the one copy a reader consults.
type publicationRecords struct {
	mu    sync.Mutex
	chain configChain
}

// attach points the recorder at the fact surface of one opened storage tier. A
// nil tier keeps the in-process chain, so a run assembled beside no storage
// still answers its own ledger for its lifetime.
func (m *publicationRecords) attach(tier *ledger.Tier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tier == nil {
		m.chain.attach(newMemoryFactLog())
		return
	}
	m.chain.attach(ledger.TierFactLog(tier))
}

// CommitPublication commits one publication record as one fact of the
// configuration chain: the record travels whole in the publication wire,
// committed before the switch it precedes so a record that cannot land refuses
// the publication.
func (m *publicationRecords) CommitPublication(ctx context.Context, record state.PublicationRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chain.log == nil {
		return nil
	}
	wire := configPublicationWireOf(record)
	_, err := m.chain.append(ctx, configFact{
		Kind:        configPublicationRecordKind,
		Type:        configPublicationFactType,
		Publication: &wire,
	})
	return err
}

// configPublicationFactType is the frozen type of the publication facts one
// run's configuration chain carries. The declaration family keeps its own type
// in api/config; this one belongs to the composition root that commits it.
const configPublicationFactType = "config.publication"

// configDeclarationKind, configPublicationRecordKind and configPublicationResultKind are the kinds one configuration fact carries: a declaration payload, the publication record committed before a switch, and the result of one attempt.
const (
	configDeclarationKind       = "declaration"
	configPublicationRecordKind = "record"
	configPublicationResultKind = "result"
)

// publicationResultPublished, publicationResultUnchanged, publicationResultLoadFailed, publicationResultGovernanceRejected and publicationResultInstallFailed are the results one publication attempt reports: the switch took effect, it carried the content already in effect, the load or the declaration refused it, the governance refused it, or the new generation was published while the model rebuild or installation was refused. The names are the wire spelling a reader of the chain answers by.
const (
	publicationResultPublished          = "published"
	publicationResultUnchanged          = "unchanged"
	publicationResultLoadFailed         = "load-failed"
	publicationResultGovernanceRejected = "governance-rejected"
	publicationResultInstallFailed      = "install-failed"
)

// configChainPrefix namespaces every fact of the configuration chain. The
// sequence number follows the prefix, so the commit order of the chain is the
// order of the keys a scan answers.
const configChainPrefix = "tagent/config/v1/fact:"

// publicationWindow is the decision window one publication attempt carries into
// its record: the generation and fingerprint it moves from, the model version
// each agent runs after it, the outcome of each configuration dimension it
// judged, the host dialect the candidate moves the fingerprint by, and the time
// the attempt was recorded.
type publicationWindow struct {
	previous            state.Generation
	previousFingerprint string
	models              map[string]string
	dimensions          map[string]string
	// dialectKey is the digest of the host dialect the candidate publishes —
	// the sections the shared declaration does not carry. The publication
	// fingerprint of the candidate covers it, so a document that moves only
	// those sections publishes a generation instead of reading as unchanged.
	dialectKey string
	at         time.Time
}

// configFact is one fact of the configuration chain: the declaration payload a
// published declaration carries, the publication record committed before its
// switch, or the result of one publication attempt. A record fact carries the
// record whole in Publication and leaves the fact-level record fields empty, so
// one record is encoded once and the fields above spell the declaration and the
// attempt results alone. The facts are append-only: a later attempt is a new
// sequence number and never a rewrite of an earlier fact. A fact written before
// the record wire carried the whole record still decodes: its duplicated
// fact-level fields are read past in favor of the record itself.
type configFact struct {
	Seq                 uint64                 `json:"seq"`
	Kind                string                 `json:"kind"`
	Type                string                 `json:"type"`
	At                  time.Time              `json:"at"`
	Fingerprint         string                 `json:"fingerprint,omitempty"`
	Generation          uint64                 `json:"generation,omitempty"`
	PreviousGeneration  uint64                 `json:"previous_generation,omitempty"`
	PreviousFingerprint string                 `json:"previous_fingerprint,omitempty"`
	Result              string                 `json:"result,omitempty"`
	Reason              string                 `json:"reason,omitempty"`
	Content             json.RawMessage        `json:"content,omitempty"`
	Models              map[string]string      `json:"models,omitempty"`
	Dimensions          map[string]string      `json:"dimensions,omitempty"`
	Publication         *configPublicationWire `json:"publication,omitempty"`
}

// configPublicationWire is the chain spelling of one state publication record:
// the record's own fields beside the decision window it carried. The state
// record keeps its five-field surface and reaches the window through its named
// accessors, so the wire form is the one place the whole record is serialized.
type configPublicationWire struct {
	Generation     uint64              `json:"generation"`
	Fingerprint    string              `json:"fingerprint"`
	Result         int                 `json:"result"`
	RollbackSource uint64              `json:"rollback_source,omitempty"`
	Checks         []state.CheckAnswer `json:"checks,omitempty"`
	Previous       uint64              `json:"previous_generation,omitempty"`
	PreviousFP     string              `json:"previous_fingerprint,omitempty"`
	Models         map[string]string   `json:"models,omitempty"`
	Dimensions     map[string]string   `json:"dimensions,omitempty"`
	At             time.Time           `json:"at,omitempty"`
}

// configPublicationWireOf renders one publication record as its chain spelling.
func configPublicationWireOf(record state.PublicationRecord) configPublicationWire {
	return configPublicationWire{
		Generation:     uint64(record.Generation),
		Fingerprint:    record.Fingerprint,
		Result:         int(record.Result),
		RollbackSource: uint64(record.RollbackSource),
		Checks:         append([]state.CheckAnswer(nil), record.Checks...),
		Previous:       uint64(record.PreviousGeneration()),
		PreviousFP:     record.PreviousFingerprint(),
		Models:         record.ModelVersions(),
		Dimensions:     record.DimensionOutcomes(),
		At:             record.RecordedAt(),
	}
}

// record answers the state publication record the wire spelling carries.
func (w configPublicationWire) record() state.PublicationRecord {
	return state.PublicationRecord{
		Generation:     state.Generation(w.Generation),
		Fingerprint:    w.Fingerprint,
		Result:         state.PublishResult(w.Result),
		RollbackSource: state.Generation(w.RollbackSource),
		Checks:         append([]state.CheckAnswer(nil), w.Checks...),
	}.WithWindow(
		state.Generation(w.Previous), w.PreviousFP,
		w.Models, w.Dimensions, w.At,
	)
}

// configChain is the ordered fact surface of the configuration chain: the facts
// already committed, loaded once from the fact log and appended beside every
// new fact, so a reader of this run reads the ledger the previous process left.
type configChain struct {
	mu     sync.Mutex
	log    state.FactLog
	facts  []configFact
	loaded bool
}

// attach points the chain at one fact log.
func (c *configChain) attach(log state.FactLog) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.log = log
	c.facts = nil
	c.loaded = false
}

// load reads every fact under the chain prefix once, in key order. A log read
// that fails or a fact that does not decode is a named failure: the chain is
// never read as an empty ledger.
func (c *configChain) load(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loadLocked(ctx)
}

// loadLocked reads the chain under the caller's hold.
func (c *configChain) loadLocked(ctx context.Context) error {
	if c.loaded {
		return nil
	}
	if c.log == nil {
		return fmt.Errorf("tagent: the configuration chain is not attached")
	}
	pairs, err := c.log.Scan(ctx, configChainPrefix)
	if err != nil {
		return fmt.Errorf("tagent: read the configuration chain: %w", err)
	}
	keys := make([]string, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	facts := make([]configFact, 0, len(keys))
	for _, key := range keys {
		var fact configFact
		if err := json.Unmarshal(pairs[key], &fact); err != nil {
			return fmt.Errorf("tagent: decode the configuration fact %s: %w", key, err)
		}
		facts = append(facts, fact)
	}
	c.facts = facts
	c.loaded = true
	return nil
}

// append commits one fact under the next sequence number of the chain and
// returns the fact as it landed.
func (c *configChain) append(ctx context.Context, fact configFact) (configFact, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(ctx); err != nil {
		return configFact{}, err
	}
	if fact.At.IsZero() {
		fact.At = time.Now().UTC()
	}
	fact.Seq = uint64(len(c.facts)) + 1
	if len(c.facts) > 0 {
		fact.Seq = c.facts[len(c.facts)-1].Seq + 1
	}
	encoded, err := json.Marshal(fact)
	if err != nil {
		return configFact{}, fmt.Errorf("tagent: encode the configuration fact: %w", err)
	}
	key := fmt.Sprintf("%s%020d", configChainPrefix, fact.Seq)
	if err := c.log.Put(ctx, key, encoded); err != nil {
		return configFact{}, fmt.Errorf("tagent: commit the configuration fact: %w", err)
	}
	if err := c.log.Sync(ctx); err != nil {
		return configFact{}, fmt.Errorf("tagent: make the configuration fact durable: %w", err)
	}
	c.facts = append(c.facts, fact)
	return fact, nil
}

// snapshot returns every fact the chain holds, in commit order.
func (c *configChain) snapshot(ctx context.Context) ([]configFact, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(ctx); err != nil {
		return nil, err
	}
	return append([]configFact(nil), c.facts...), nil
}

// latest reports the last fact of one kind, and false when the chain holds none.
func (c *configChain) latest(ctx context.Context, kinds ...string) (configFact, bool, error) {
	facts, err := c.snapshot(ctx)
	if err != nil {
		return configFact{}, false, err
	}
	for i := len(facts) - 1; i >= 0; i-- {
		for _, kind := range kinds {
			if facts[i].Kind == kind {
				return facts[i], true, nil
			}
		}
	}
	return configFact{}, false, nil
}

// memoryFactLog is the in-process fact surface of a run assembled beside no
// storage tier: the same put/get/scan/sync contract the durable tier satisfies,
// so the configuration chain keeps one shape over either surface.
type memoryFactLog struct {
	mu    sync.Mutex
	pairs map[string][]byte
}

// newMemoryFactLog returns an empty in-process fact log.
func newMemoryFactLog() *memoryFactLog {
	return &memoryFactLog{pairs: map[string][]byte{}}
}

// Put stores one key and its value.
func (l *memoryFactLog) Put(_ context.Context, key string, value []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pairs == nil {
		l.pairs = map[string][]byte{}
	}
	l.pairs[key] = append([]byte(nil), value...)
	return nil
}

// Get answers the value stored under one key.
func (l *memoryFactLog) Get(_ context.Context, key string) ([]byte, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	value, held := l.pairs[key]
	return append([]byte(nil), value...), held, nil
}

// Scan answers every pair under a prefix.
func (l *memoryFactLog) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string][]byte{}
	for key, value := range l.pairs {
		if strings.HasPrefix(key, prefix) {
			out[key] = append([]byte(nil), value...)
		}
	}
	return out, nil
}

// Sync completes no barrier of an in-process log: every Put is already visible.
func (l *memoryFactLog) Sync(context.Context) error { return nil }

// commitCarrier is the fact carrier behind one run's commit port: the commit
// port itself beside the notification ledger the close and the diagnostic read
// settle and list through. Both the in-memory commit and the durable commit
// satisfy it, so the run keeps one shape over either carrier.
type commitCarrier interface {
	task.Commit
	Outbox(taskID event.TaskID) []task.OutboxNotice
}

// runCommit is the commit carrier of one assembled run: every task fact is
// stored in the commit ledger behind it — the in-memory ledger of the task
// layer or the durable one the configured storage tier opens — and the
// identity of each task that committed a fact is remembered beside it, so the
// diagnostic read face lists the tasks the run holds without keeping a second
// copy of their content.
type runCommit struct {
	inner commitCarrier
	mu    sync.Mutex
	held  map[event.TaskID]bool
}

// The carrier is the very commit port the registry submits through.
var _ task.Commit = (*runCommit)(nil)

// Commit stores one task fact and remembers the identity of its task.
func (c *runCommit) Commit(ctx context.Context, record task.CommitRecord) (task.CommitResult, error) {
	result, err := c.inner.Commit(ctx, record)
	if result == task.CommitCommitted && record.TaskID != "" {
		c.mu.Lock()
		c.held[record.TaskID] = true
		c.mu.Unlock()
	}
	return result, err
}

// Declare stores the admission material of one task beside its first commit
// when the carrier behind this run holds a durable chain, so every fact chain
// the storage keeps is self-contained and a cold start rebuilds the task from
// material the chain already carries. A carrier beside no durable chain stores
// nothing and keeps the admission material in the registry of the run alone.
func (c *runCommit) Declare(ctx context.Context, taskID event.TaskID, spec task.Spec) error {
	type declarer interface {
		Declare(ctx context.Context, taskID event.TaskID, spec task.Spec) error
	}
	if d, ok := c.inner.(declarer); ok {
		return d.Declare(ctx, taskID, spec)
	}
	return nil
}

// TaskIDs reports the identity of every task this run holds, in identity order.
func (c *runCommit) TaskIDs() []event.TaskID {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]event.TaskID, 0, len(c.held))
	for id := range c.held {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// runOwners resolves the owner of a restored target under the current
// orchestration binding of this run. The formal root's task vocabulary — the
// wrapped call and the delegation alike — records a task's source and target as
// the address of its own owner, so a target that names its owner resolves to
// that owner while the binding still holds it; a target naming a member of the
// owner's tool set resolves through the binding owners, and both are refused by
// name when the current binding holds neither.
type runOwners struct{ bindings *state.Bindings }

// The formal root resolves restored targets through this very face.
var _ task.OwnerBinding = runOwners{}

// OwnerOf resolves one restored target against the current binding.
func (o runOwners) OwnerOf(ctx context.Context, owner event.AgentID, target event.AgentID) (event.AgentID, error) {
	if target == owner {
		handle, err := o.bindings.Borrow(ctx, owner)
		if err != nil {
			return "", fmt.Errorf("%w: owner %s target %s: %v", task.ErrTargetRemoved, owner, target, err)
		}
		defer handle.Release(ctx)
		return owner, nil
	}
	return state.NewBindingOwners(o.bindings).OwnerOf(ctx, owner, target)
}

// remember records one task identity the run holds, so a task the assembly
// restored from a durable chain is listed beside the tasks it admitted itself.
func (c *runCommit) remember(taskID event.TaskID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.held[taskID] = true
}

// Outbox reports the notifications one stored commit record still owes, each
// beside the delivery-layer state it stands in.
func (c *runCommit) Outbox(taskID event.TaskID) []task.OutboxNotice {
	return c.inner.Outbox(taskID)
}

// closeFace is the task face the close of this run settles through: the manager
// admits and settles the obligations and the registry lands the transitions,
// exactly the faces the wrapped tools of this run are driven over.
type closeFace struct {
	manager  *task.MemoryManager
	registry *task.MemoryRegistry
}

// The close face settles its delegations through the very task face this run
// wraps its tools over.
var _ agent.DelegationTasks = closeFace{}

// Admit admits one delegated call as a task through the manager.
func (f closeFace) Admit(ctx context.Context, spec task.Spec) (task.AdmissionReceipt, error) {
	return f.manager.Admit(ctx, spec)
}

// Advance moves one task to the next status through the registry.
func (f closeFace) Advance(ctx context.Context, taskID event.TaskID, to task.Status, mutation task.MutationID, outbox []task.Envelope) (task.CommitResult, error) {
	return f.registry.Advance(ctx, taskID, to, mutation, outbox)
}

// Obligations reports the obligations the manager still holds for one task.
func (f closeFace) Obligations(ctx context.Context, taskID event.TaskID) (task.Obligations, error) {
	return f.manager.Obligations(ctx, taskID)
}

// Flush delivers the notifications of one task through the manager.
func (f closeFace) Flush(ctx context.Context, taskID event.TaskID) error {
	return f.manager.Flush(ctx, taskID)
}

// canonicalDeclarationPayload renders the canonical declaration serialization a
// declaration fact carries: the declaration's whole surface in its field order,
// with the decoded source document left out. The fingerprint is computed over
// the same surface, so a reader that decodes the payload answers the very
// fingerprint the fact names. The payload is the declaration (paths and content
// keys included) and never a copy of the prompt bodies the file facts carry.
func canonicalDeclarationPayload(decl *config.Declaration) ([]byte, error) {
	if decl == nil {
		return nil, fmt.Errorf("tagent: the declaration fact carries no declaration")
	}
	normalized := decl.Clone()
	normalized.Raw = nil
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("tagent: encode the declaration fact: %w", err)
	}
	return payload, nil
}

// declarationFromPayload restores the declaration one declaration fact carries.
func declarationFromPayload(payload []byte) (*config.Declaration, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("tagent: the declaration fact carries no payload")
	}
	var decl config.Declaration
	if err := json.Unmarshal(payload, &decl); err != nil {
		return nil, fmt.Errorf("tagent: decode the declaration fact: %w", err)
	}
	return &decl, nil
}

// recordDeclarationFact commits the declaration one load produced as the
// declaration now in effect. The declaration family is the ordered ledger of the
// declarations that took effect: a load the newest declaration fact already
// answers writes no second one, so reloading equivalent content never doubles
// the ledger, while a load that moves files its own entry naming the declaration
// it moves from — including a load that reverts the source to a declaration an
// earlier generation already carried, which the ledger could not state by
// identity alone. The newest declaration fact is therefore always the
// declaration the load produced, which is the reading the reload and the boot
// reconciliation both consume. The fact names the generation the declaration is
// meant for beside the declaration it follows, so the chain keeps the relation
// between two declarations it must be read with.
func (r *Run) recordDeclarationFact(ctx context.Context, decl *config.Declaration, generation uint64) (configFact, error) {
	if decl == nil {
		return configFact{}, fmt.Errorf("tagent: the declaration fact carries no declaration")
	}
	fingerprint := decl.FactKey()
	if fingerprint == "" {
		return configFact{}, fmt.Errorf("tagent: the declaration answers no canonical fingerprint")
	}
	newest, held, err := r.records.chain.latest(ctx, configDeclarationKind)
	if err != nil {
		return configFact{}, err
	}
	if held && newest.Fingerprint == fingerprint {
		return newest, nil
	}
	content, err := canonicalDeclarationPayload(decl)
	if err != nil {
		return configFact{}, err
	}
	previous := ""
	if held {
		previous = newest.Fingerprint
	}
	return r.records.chain.append(ctx, configFact{
		Kind:                configDeclarationKind,
		Type:                decl.FactType(),
		Fingerprint:         fingerprint,
		Generation:          generation,
		PreviousFingerprint: previous,
		Content:             content,
	})
}

// latestDeclarationFact reports the newest declaration fact the configuration
// chain carries, and whether one is held at all.
func (r *Run) latestDeclarationFact(ctx context.Context) (configFact, bool, error) {
	return r.records.chain.latest(ctx, configDeclarationKind)
}

// latestPublishedRecord reports the newest publication record the configuration
// chain carries, and whether one is held at all. The record answers from the
// publication wire alone, which is the one encoding a record fact carries.
func (r *Run) latestPublishedRecord(ctx context.Context) (state.PublicationRecord, bool, error) {
	fact, held, err := r.records.chain.latest(ctx, configPublicationRecordKind)
	if err != nil || !held {
		return state.PublicationRecord{}, false, err
	}
	if fact.Publication == nil {
		return state.PublicationRecord{}, false, fmt.Errorf("tagent: the publication record fact %d carries no record", fact.Seq)
	}
	return fact.Publication.record(), true, nil
}

// publicationAttempt is the result of one publication attempt as the chain
// records it: what the attempt did, which generation and fingerprint it moved
// from and to, why it was refused when it was, and the window of the decisions
// it judged. The successful and the refused attempts are the same family; a
// reader consults the newest result fact and never self-reports from memory.
type publicationAttempt struct {
	Result              string
	Fingerprint         string
	Generation          uint64
	PreviousGeneration  uint64
	PreviousFingerprint string
	Reason              string
	Models              map[string]string
	Dimensions          map[string]string
	At                  time.Time
}

// recordPublicationAttempt commits the result of one publication attempt as one
// fact of the configuration chain. The fact names the generation it moved from
// and to, so the switch decision and the model version switch that follows it
// are readable as one window.
func (r *Run) recordPublicationAttempt(ctx context.Context, attempt publicationAttempt) (configFact, error) {
	if attempt.Result == "" {
		attempt.Result = publicationResultUnchanged
	}
	return r.records.chain.append(ctx, configFact{
		Kind:                configPublicationResultKind,
		Type:                configPublicationFactType,
		Fingerprint:         attempt.Fingerprint,
		Generation:          attempt.Generation,
		PreviousGeneration:  attempt.PreviousGeneration,
		PreviousFingerprint: attempt.PreviousFingerprint,
		Result:              attempt.Result,
		Reason:              attempt.Reason,
		Models:              attempt.Models,
		Dimensions:          attempt.Dimensions,
		At:                  attempt.At,
	})
}

// latestPublicationAttempt reports the newest publication attempt the
// configuration chain carries, and whether one is held at all.
func (r *Run) latestPublicationAttempt(ctx context.Context) (configFact, bool, error) {
	return r.records.chain.latest(ctx, configPublicationResultKind)
}

// latestDeclaration reports the declaration the newest declaration fact carries,
// beside that fact. The payload is restored and its fingerprint re-derived: a
// payload whose declared fingerprint does not answer the fact's own key is a
// named corruption, never a declaration silently served under another identity.
func (r *Run) latestDeclaration(ctx context.Context) (configFact, *config.Declaration, bool, error) {
	fact, held, err := r.latestDeclarationFact(ctx)
	if err != nil || !held {
		return configFact{}, nil, false, err
	}
	decl, err := declarationFromPayload(fact.Content)
	if err != nil {
		return configFact{}, nil, false, err
	}
	if got := decl.FactKey(); got != fact.Fingerprint {
		return configFact{}, nil, false, fmt.Errorf("tagent: the declaration fact %d names fingerprint %q beside %q", fact.Seq, fact.Fingerprint, got)
	}
	return fact, decl, true, nil
}

// adoptConfigPublication restores one publication record as the generation
// in effect of this restarted run: the candidate is constructed from the
// declaration the record names through the same suppliers the entry uses, and
// the publisher adopts it without committing a second record. Nothing is
// published here, so a restart that agrees with its chain adds no ghost
// publication.
func (r *Run) adoptConfigPublication(ctx context.Context, decl *config.Declaration, agents map[event.AgentID]NativeAgent, record state.PublicationRecord, dialectKey string) error {
	if decl == nil {
		return fmt.Errorf("tagent: the adopted publication carries no declaration")
	}
	fingerprint, err := config.Fingerprint(*decl)
	if err != nil {
		return err
	}
	fingerprint = publicationFingerprint(fingerprint, dialectKey)
	if fingerprint != record.Fingerprint {
		return fmt.Errorf("tagent: the declaration %q does not answer the adopted publication %q", fingerprint, record.Fingerprint)
	}
	servers := declaredServersOf(decl)
	if err := mcpgw.ValidateDeclared(servers); err != nil {
		return err
	}
	plan := state.OrgPlan{
		Declaration: decl,
		Baseline:    r.publisher.Baseline(),
		Fingerprint: fingerprint,
	}
	cand, err := prepare(ctx, plan, r.suppliers(decl, agents))
	if err != nil {
		return err
	}
	if err := r.publisher.Adopt(ctx, cand, record); err != nil {
		return err
	}
	r.loop.SetFinalRound(finalRoundPolicy(decl))
	r.loop.SetAgentFinalRounds(agentFinalRoundPolicies(decl))
	r.mu.Lock()
	r.current = cand.Plan
	r.natives = agents
	r.mu.Unlock()
	return r.bindMCPServers(ctx, servers)
}

// modelVersionsOf reports the model version each declared agent runs after one
// document is in effect, keyed by agent id, as the window fact records it.
func modelVersionsOf(doc *hostDocument, decl *config.Declaration) map[string]string {
	if doc == nil || decl == nil {
		return nil
	}
	versions := make(map[string]string, len(decl.Agents))
	for _, spec := range decl.Agents {
		if version := doc.modelVersion(spec.ID); version != "" {
			versions[spec.ID] = version
		}
	}
	return versions
}
