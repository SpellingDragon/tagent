package rl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/rl/rl-architecture.md#training-export
//
// Exporter is the read-only consumption port for authorized training export: it
// reads the events of the partitions a caller is authorized for, together with the
// feedback bound to them, and writes one snapshot whose provenance manifest says
// what the snapshot contains and how completely it was read.
//
// The redline list (4 rules):
// MUST run authorization before any read, and refuse an empty partition list.
// MUST recheck every row it writes and count a row that fails the recheck.
// MUST NOT write a row that fails its recheck.
// MUST NOT report a partial read as a complete snapshot.
type Exporter interface {
	// Export runs one authorized export and returns the manifest of the snapshot it
	// wrote with the counts of the run. The context bounds the run, and a cancelled
	// run reports what it read as partial.
	Export(ctx context.Context, req ExportRequest) (ExportResult, error)
}

// ExportRequest is one authorized export run: the partitions the caller may read,
// the optional call-id filter and time bounds, and the page size to read them with.
//
// The request is the authorization input: the partition list is the caller's grant,
// and no field of the request may widen that grant.
type ExportRequest struct {
	// PartitionIDs names the partitions the caller is authorized to read. An empty
	// list is refused, and the port never widens it into a whole-store scan.
	PartitionIDs []string
	// CallIDs keeps only the facts whose call id is listed when it is non-empty.
	CallIDs []string
	// Since and Until bound the export in Unix milliseconds, the unit the stored
	// events use; 0 means open.
	Since int64
	Until int64
	// PageLimit is the per-partition page size; 0 takes the default and a value
	// outside the supported range is refused rather than clamped.
	PageLimit int
}

// ExportResult is the outcome of one export run: the provenance manifest of the
// snapshot together with the counts a caller reads without opening the snapshot.
//
// A partial run returns both the manifest and a non-nil error, so a truncated
// snapshot is never mistaken for a complete one.
type ExportResult struct {
	// Manifest is the provenance manifest of the run.
	Manifest ExportManifest
	// Written counts the sample lines the run wrote.
	Written int64
	// Forbidden counts the rows the per-row authorization recheck refused.
	Forbidden int64
	// Filtered counts the facts the caller's filters removed.
	Filtered int64
	// Partial reports a run that stopped before every authorized page was read.
	Partial bool
}

// Export contract assertions pin the port and its request and result types: a
// changed signature or a renamed field stops the package from compiling.
var (
	_ Exporter = (Exporter)(nil)

	_ func(Exporter, context.Context, ExportRequest) (ExportResult, error) = Exporter.Export

	_ = ExportRequest{
		PartitionIDs: []string{},
		CallIDs:      []string{},
		Since:        0,
		Until:        0,
		PageLimit:    0,
	}

	_ = ExportResult{
		Manifest:  ExportManifest{},
		Written:   0,
		Forbidden: 0,
		Filtered:  0,
		Partial:   false,
	}
)

// DefaultExportPageLimit and MaxExportPageLimit bound the page size a request is
// read with: a request that names no page limit takes the default, and a limit
// outside the supported range is refused by name rather than clamped into it.
const (
	// DefaultExportPageLimit is the per-partition page size a request that
	// names no limit is read with.
	DefaultExportPageLimit = 200
	// MaxExportPageLimit is the largest per-partition page size supported.
	MaxExportPageLimit = 1000
)

// ExportedFact is one stored event as the export source hands it over: the
// canonical event key and the partition it is filed under, the capture,
// episode, decision and attempt it descends from, the schema it was written
// with, the parent key it was linked under, its event time in Unix
// milliseconds and the event payload committed byte for byte.
type ExportedFact struct {
	// EventKey is the canonical identity of the event.
	EventKey event.EventID `json:"event_key"`
	// Partition is the partition the event is filed under; the export
	// rechecks it against the grant before every written row.
	Partition string `json:"partition"`
	// CallID names the call the event belongs to; the caller's call filter is
	// applied to it.
	CallID string `json:"call_id,omitempty"`
	// CaptureID names the capture the event was recorded under.
	CaptureID string `json:"capture_id,omitempty"`
	// EpisodeID names the episode the event belongs to.
	EpisodeID event.EpisodeID `json:"episode_id,omitempty"`
	// DecisionID names the decision the event was reduced from.
	DecisionID event.DecisionID `json:"decision_id,omitempty"`
	// AttemptID names the attempt the event observed.
	AttemptID event.AttemptID `json:"attempt_id,omitempty"`
	// Schema is the record schema version the event was written with.
	Schema SchemaVersion `json:"schema"`
	// ParentKey names the parent the event was linked under; an absent key is
	// reported as absent and never invented.
	ParentKey event.EventID `json:"parent_key,omitempty"`
	// Timestamp is the event time in Unix milliseconds, the unit the time
	// bounds of the request are read in.
	Timestamp int64 `json:"timestamp"`
	// Body is the event payload, byte for byte.
	Body json.RawMessage `json:"body"`
}

// ExportSource is the read seam an authorized export pages the store through:
// offset-paged reads of one partition and the episode records the leakage-free
// split groups are drawn on. The exporter reaches no further than this seam.
//
// The redline list (3 rules):
// MUST page one partition by offset in a stable order, ending it on a short or empty page.
// MUST return the episode records the split groups are drawn on.
// MUST NOT page by an event-key cursor or reach beyond this seam.
type ExportSource interface {
	// Page returns at most limit facts of one partition starting at the given
	// offset, in a stable order. A short or empty page ends the partition, and
	// paging is by offset and never by an event-key cursor.
	Page(ctx context.Context, partition string, offset, limit int) ([]ExportedFact, error)
	// Episodes returns the episode records the split groups are drawn on.
	Episodes(ctx context.Context) ([]EpisodeRecord, error)
}

// SampleLine is one exported training sample: the provenance entry it carries
// beside the fact it was reduced from, the feedback evidence bound to it and
// the reward instance derived from that evidence. Feedback and reward stay two
// separate accounts on the line — the feedback is raw evidence and the reward
// is the derived record citing it by identity — and are never merged into one.
type SampleLine struct {
	// Schema is the line schema version the sample was written with.
	Schema SchemaVersion `json:"schema"`
	// Provenance is the manifest entry this sample carries.
	Provenance SampleProvenance `json:"provenance"`
	// Fact is the stored event the sample was reduced from.
	Fact ExportedFact `json:"fact"`
	// Feedback lists the raw feedback records the reward was derived from.
	Feedback []FeedbackRecord `json:"feedback,omitempty"`
	// Reward is the reward instance the sample carries, cited by instance ID
	// and revision.
	Reward RewardRecord `json:"reward"`
}

// ErrExportUnauthorized and the export errors beside it name the way a run was
// refused or cut short, so a caller routes it by name and reads the partial result
// beside it.
var (
	// ErrExportUnauthorized reports a request that authorizes no partition:
	// authorization runs before any read and an empty grant is a refusal,
	// never a whole-store scan.
	ErrExportUnauthorized = errors.New("rl: the export request authorizes no partition")
	// ErrExportPageLimit reports a page limit outside the supported range: a
	// limit is refused by name rather than clamped into the range.
	ErrExportPageLimit = errors.New("rl: the export page limit is outside the supported range")
	// ErrExportRepeatedPage reports a page that named no event the run had not
	// already seen: the read cannot advance and is stopped at that partition,
	// and a repeated page is never read as the end of the partition.
	ErrExportRepeatedPage = errors.New("rl: the export page repeated already-seen events and did not advance")
	// ErrExportReadFailed reports a source read that failed. What the run had
	// already read is returned as a partial result beside this error.
	ErrExportReadFailed = errors.New("rl: the export source read failed")
	// ErrExportConflict reports a snapshot seal that would overwrite an
	// existing snapshot: snapshots are created exclusively and a concurrent
	// run writes beside an earlier one, never over it.
	ErrExportConflict = errors.New("rl: the export snapshot already exists")
	// ErrExportWriteFailed reports a snapshot that could not be written.
	ErrExportWriteFailed = errors.New("rl: the export snapshot write failed")
)

// exportRead is one fact the run read together with the partition it was read
// from, so the per-row recheck can compare the two.
type exportRead struct {
	fact      ExportedFact
	partition string
}

// TrainingExporter runs authorized training exports over an export source and
// the two ledgers. Every run takes its own export ID and seals its own
// snapshot, so concurrent runs over one partition write beside each other and
// never overwrite.
type TrainingExporter struct {
	source   ExportSource
	feedback *FeedbackLedger
	rewards  *RewardLedger
	rule     RewardRule

	mu        sync.Mutex
	seq       int64
	snapshots map[string][]SampleLine
}

// NewTrainingExporter returns an exporter reading the given source and
// deriving every reward under the given rule from the feedback account.
func NewTrainingExporter(source ExportSource, feedback *FeedbackLedger, rewards *RewardLedger, rule RewardRule) *TrainingExporter {
	return &TrainingExporter{
		source:    source,
		feedback:  feedback,
		rewards:   rewards,
		rule:      rule,
		snapshots: map[string][]SampleLine{},
	}
}

var _ Exporter = (*TrainingExporter)(nil)

// nextExportID names one export run. The name carries the run order and the
// wall time it started at, so two concurrent runs never share an ID.
func (e *TrainingExporter) nextExportID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq++
	return fmt.Sprintf("export:%d:%d", time.Now().UnixNano(), e.seq)
}

// SealSnapshot stores one export snapshot under its export ID. The seal is
// exclusive: an export ID already sealed is refused with ErrExportConflict, so
// concurrent runs write beside each other and a sealed snapshot is never
// overwritten.
func (e *TrainingExporter) SealSnapshot(exportID string, lines []SampleLine) error {
	if exportID == "" {
		return ErrExportWriteFailed
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, held := e.snapshots[exportID]; held {
		return ErrExportConflict
	}
	e.snapshots[exportID] = append([]SampleLine(nil), lines...)
	return nil
}

// Snapshot returns the sample lines sealed under one export ID, and reports
// whether the snapshot is held.
func (e *TrainingExporter) Snapshot(exportID string) ([]SampleLine, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	lines, held := e.snapshots[exportID]
	return append([]SampleLine(nil), lines...), held
}

// SnapshotIDs returns the export IDs sealed so far.
func (e *TrainingExporter) SnapshotIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	ids := make([]string, 0, len(e.snapshots))
	for id := range e.snapshots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// rewardFor returns the reward instance one sample carries. When the newest
// derived record was computed from exactly the evidence the feedback account
// holds now, that record is cited as it stands; when the evidence changed — a
// late feedback arrived — a new instance is derived at the next revision and
// the record earlier samples cite is left exactly as it was.
func (e *TrainingExporter) rewardFor(decision event.DecisionID, attempt event.AttemptID, evidence []FeedbackRecord) (RewardRecord, error) {
	return e.rewards.DeriveForEvidence(decision, attempt, e.rule, evidence)
}

// Export runs one authorized export. Authorization runs before any read: a
// request that grants no partition is refused by name and the source is never
// touched. Every row is rechecked against the grant before it is written, and
// a row failing the recheck is counted as forbidden and written not at all. A
// run cut short by a read or write failure returns its manifest as partial
// beside a named error, never as a complete snapshot.
func (e *TrainingExporter) Export(ctx context.Context, req ExportRequest) (ExportResult, error) {
	authorized := map[string]bool{}
	var partitions []string
	for _, partition := range req.PartitionIDs {
		if partition == "" || authorized[partition] {
			continue
		}
		authorized[partition] = true
		partitions = append(partitions, partition)
	}
	if len(partitions) == 0 {
		return ExportResult{}, ErrExportUnauthorized
	}
	pageLimit := req.PageLimit
	if pageLimit == 0 {
		pageLimit = DefaultExportPageLimit
	} else if pageLimit < 1 || pageLimit > MaxExportPageLimit {
		return ExportResult{}, ErrExportPageLimit
	}
	exportID := e.nextExportID()

	episodes, err := e.source.Episodes(ctx)
	if err != nil {
		return ExportResult{
			Manifest: ExportManifest{Schema: SchemaCurrent, ExportID: exportID, Cutoff: req.Until},
			Partial:  true,
		}, fmt.Errorf("%w: %w", ErrExportReadFailed, err)
	}
	index := newGroupIndex(episodes)

	var (
		reads   []exportRead
		seen    = map[string]bool{}
		readErr error
	)
	for _, partition := range partitions {
		offset := 0
		for {
			if err := ctx.Err(); err != nil {
				readErr = err
				break
			}
			page, err := e.source.Page(ctx, partition, offset, pageLimit)
			if err != nil {
				readErr = fmt.Errorf("%w: %w", ErrExportReadFailed, err)
				break
			}
			if len(page) == 0 {
				break
			}
			fresh := 0
			for _, fact := range page {
				if !seen[string(fact.EventKey)] {
					seen[string(fact.EventKey)] = true
					fresh++
				}
			}
			if fresh == 0 {
				readErr = ErrExportRepeatedPage
				break
			}
			for _, fact := range page {
				reads = append(reads, exportRead{fact: fact, partition: partition})
			}
			if len(page) < pageLimit {
				break
			}
			offset += len(page)
		}
		if readErr != nil {
			break
		}
	}

	evidenceFor := map[event.DecisionID][]FeedbackRecord{}
	decisionOf := map[string]event.DecisionID{}
	for _, read := range reads {
		decisionOf[string(read.fact.EventKey)] = read.fact.DecisionID
	}
	for _, feedback := range e.feedback.All() {
		decision := feedback.DecisionID
		if decision == "" {
			decision = decisionOf[feedback.EventKey]
		}
		if decision != "" {
			evidenceFor[decision] = append(evidenceFor[decision], feedback)
		}
	}

	var (
		lines      []SampleLine
		rejected   []ProvenanceGap
		groupSeen  = map[string]EpisodeGroup{}
		derived    = map[string]RewardRecord{}
		callFilter = map[string]bool{}
		digest     = sha256.New()
		written    int64
		forbidden  int64
		filtered   int64
		parentMiss int64
		writeErr   error
	)
	for _, id := range req.CallIDs {
		callFilter[id] = true
	}
	for _, read := range reads {
		fact := read.fact
		sampleID := "sample:" + string(fact.EventKey)
		if fact.EventKey == "" || fact.Partition != read.partition || !authorized[fact.Partition] {
			forbidden++
			continue
		}
		if len(callFilter) > 0 && !callFilter[fact.CallID] {
			filtered++
			continue
		}
		if (req.Since > 0 && fact.Timestamp < req.Since) || (req.Until > 0 && fact.Timestamp > req.Until) {
			filtered++
			continue
		}
		if fact.Schema != SchemaCurrent {
			rejected = append(rejected, ProvenanceGap{SampleID: sampleID, Kind: ProvenanceGapUnknown,
				Reason: "the fact is not written under the current record schema"})
			continue
		}
		if len(fact.Body) == 0 || !json.Valid(fact.Body) {
			rejected = append(rejected, ProvenanceGap{SampleID: sampleID, Kind: ProvenanceGapMalformed,
				Reason: "the fact body does not parse"})
			continue
		}
		if fact.EpisodeID == "" || fact.DecisionID == "" {
			rejected = append(rejected, ProvenanceGap{SampleID: sampleID, Kind: ProvenanceGapMissing,
				Reason: "the fact carries no episode or decision identity to trace the sample by"})
			continue
		}
		key := string(fact.DecisionID) + "\x00" + string(fact.AttemptID)
		reward, held := derived[key]
		if !held {
			next, err := e.rewardFor(fact.DecisionID, fact.AttemptID, evidenceFor[fact.DecisionID])
			if err != nil {
				rejected = append(rejected, ProvenanceGap{SampleID: sampleID, Kind: ProvenanceGapUnknown,
					Reason: "the reward could not be derived: " + err.Error()})
				continue
			}
			reward = next
			derived[key] = reward
		}
		if reward.InstanceID == "" || reward.Revision < 1 {
			rejected = append(rejected, ProvenanceGap{SampleID: sampleID, Kind: ProvenanceGapUnknown,
				Reason: "the reward instance cannot be pinned"})
			continue
		}
		if fact.ParentKey == "" {
			parentMiss++
		}
		group := index.groupFor(fact.EpisodeID)
		groupSeen[group.GroupID] = group
		line := SampleLine{
			Schema: SchemaCurrent,
			Provenance: SampleProvenance{
				SampleID:         sampleID,
				CaptureID:        fact.CaptureID,
				ExportID:         exportID,
				EpisodeID:        fact.EpisodeID,
				DecisionID:       fact.DecisionID,
				AttemptID:        fact.AttemptID,
				Schema:           fact.Schema,
				RewardInstanceID: reward.InstanceID,
				RewardRevision:   reward.Revision,
				RewardVersion:    reward.RuleVersion,
				GroupID:          group.GroupID,
			},
			Fact:     fact,
			Feedback: evidenceFor[fact.DecisionID],
			Reward:   reward,
		}
		encoded, err := json.Marshal(line)
		if err != nil {
			writeErr = fmt.Errorf("%w: %v", ErrExportWriteFailed, err)
			break
		}
		digest.Write(encoded)
		lines = append(lines, line)
		written++
	}

	if writeErr == nil {
		if err := e.SealSnapshot(exportID, lines); err != nil {
			writeErr = err
		}
	}

	manifest := ExportManifest{
		Schema:           SchemaCurrent,
		ExportID:         exportID,
		Samples:          make([]SampleProvenance, 0, len(lines)),
		Rejected:         rejected,
		Forbidden:        forbidden,
		Filtered:         filtered,
		ParentKeyMissing: parentMiss,
		SourceSHA256:     hex.EncodeToString(digest.Sum(nil)),
		Cutoff:           req.Until,
		Complete:         readErr == nil && writeErr == nil,
	}
	for _, line := range lines {
		manifest.Samples = append(manifest.Samples, line.Provenance)
	}
	for _, group := range groupSeen {
		manifest.Groups = append(manifest.Groups, group)
	}
	sort.Slice(manifest.Groups, func(i, j int) bool { return manifest.Groups[i].GroupID < manifest.Groups[j].GroupID })

	result := ExportResult{
		Manifest:  manifest,
		Written:   written,
		Forbidden: forbidden,
		Filtered:  filtered,
		Partial:   !manifest.Complete,
	}
	if readErr != nil {
		return result, readErr
	}
	if writeErr != nil {
		return result, writeErr
	}
	return result, nil
}

// groupIndex draws the leakage-free split groups over the recorded episode
// dependencies: a parent link or a declared dependency pulls two episodes into
// one group, so no group crosses the train and test sides a split draws. A
// sample whose episode carries no record stands alone in a group of its own at
// unknown lineage rather than being merged under a guessed parent.
type groupIndex struct {
	parent  map[event.EpisodeID]event.EpisodeID
	records map[event.EpisodeID]EpisodeRecord
}

// newGroupIndex unions every episode with its parent and every episode with the
// episodes it declares a dependency on.
func newGroupIndex(episodes []EpisodeRecord) *groupIndex {
	g := &groupIndex{
		parent:  map[event.EpisodeID]event.EpisodeID{},
		records: map[event.EpisodeID]EpisodeRecord{},
	}
	for _, record := range episodes {
		g.records[record.ID] = record
		g.find(record.ID)
		if record.ParentID != "" {
			g.union(record.ID, record.ParentID)
		}
		for _, dep := range record.DependsOn {
			if dep.DependsOn != "" {
				g.union(record.ID, dep.DependsOn)
			}
		}
	}
	return g
}

// find returns the representative of the group an episode belongs to.
func (g *groupIndex) find(id event.EpisodeID) event.EpisodeID {
	root := id
	for {
		next, held := g.parent[root]
		if !held || next == root {
			break
		}
		root = next
	}
	for id != root {
		next := g.parent[id]
		g.parent[id] = root
		id = next
	}
	return root
}

// union pulls two episodes into one group, keeping the smaller identifier as
// the representative so the group name is deterministic.
func (g *groupIndex) union(a, b event.EpisodeID) {
	ra, rb := g.find(a), g.find(b)
	if ra == rb {
		return
	}
	if rb < ra {
		ra, rb = rb, ra
	}
	g.parent[rb] = ra
}

// groupFor returns the split group one episode belongs to: every episode it
// shares a parent link or a declared dependency with, named after the
// representative of that set.
func (g *groupIndex) groupFor(episode event.EpisodeID) EpisodeGroup {
	root := g.find(episode)
	group := EpisodeGroup{GroupID: "group:" + string(root), Lineage: LineageKnown}
	roots := map[event.EpisodeID]bool{}
	for id, record := range g.records {
		if g.find(id) != root {
			continue
		}
		group.Members = append(group.Members, id)
		if record.RootID != "" {
			roots[record.RootID] = true
		}
		if record.Lineage != LineageKnown {
			group.Lineage = LineageUnknown
		}
		for _, dep := range record.DependsOn {
			if dep.DependsOn != "" && g.find(dep.DependsOn) == root {
				group.Basis = append(group.Basis, dep)
			}
		}
	}
	if len(group.Members) == 0 {
		group.Members = []event.EpisodeID{episode}
		group.Lineage = LineageUnknown
		return group
	}
	if _, held := g.records[episode]; !held {
		group.Members = append(group.Members, episode)
		group.Lineage = LineageUnknown
	}
	if len(roots) == 1 {
		for root := range roots {
			group.RootID = root
		}
	}
	sort.Slice(group.Members, func(i, j int) bool { return group.Members[i] < group.Members[j] })
	sort.Slice(group.Basis, func(i, j int) bool {
		if group.Basis[i].EpisodeID != group.Basis[j].EpisodeID {
			return group.Basis[i].EpisodeID < group.Basis[j].EpisodeID
		}
		return group.Basis[i].DependsOn < group.Basis[j].DependsOn
	})
	return group
}
