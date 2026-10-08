package rl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"sort"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

const (
	// exportDefaultPageLimit / exportMaxPageLimit bound one paging round. The cap
	// exists because the exporter holds no second writer: an unbounded page would
	// turn an audited export into an in-memory incident.
	exportDefaultPageLimit = 200
	exportMaxPageLimit     = 1000

	// exportMetaKeyCallID 取 event 包的单源常数：键名漂移会让 join 计数静默归零，
	// 与 MetaKeySubtype 的防线同理。
	exportMetaKeyCallID = tagentevent.MetaKeyCallID
)

// ErrExportAuthorization and its siblings are the sentinel refusals. A caller
// must be able to tell "you did not authorize this" apart from "the read failed",
// so these are typed and never wrapped away.
var (
	// ErrExportAuthorization means no partition allowlist was supplied: an
	// un-parameterized export must not degrade into reading everybody's memory.
	ErrExportAuthorization = errors.New("rl: training export requires an explicit non-empty partition allowlist")
	// ErrExportPageLimitExceeded means the requested page size is outside 1..1000.
	ErrExportPageLimitExceeded = fmt.Errorf("rl: training export page limit must be within 1..%d", exportMaxPageLimit)
	// ErrExportNilStore rejects a missing source instead of exporting "nothing" as
	// if that were a complete snapshot.
	ErrExportNilStore = errors.New("rl: training export store is nil")
	// ErrExportNilWriter rejects a missing sink for the same reason.
	ErrExportNilWriter = errors.New("rl: training export writer is nil")
)

// ExportOptions is the caller's authorization plus scoping for one export.
type ExportOptions struct {
	// PartitionIDs is the allowlist. It must be non-empty; duplicates collapse and
	// each partition is paged on its own, so one authorized partition can never
	// widen the read into its neighbours.
	PartitionIDs []int
	// EventTypes narrows the query. Empty means "every type in the authorized
	// partitions" — leaving it empty is the safe default, because a type filter
	// that omits feedback silently removes the join the consumer asked for.
	EventTypes []string
	// Since/Until bound Timestamp in Unix MILLISECONDS — the same unit as
	// memory.FullEvent.Timestamp and QueryOptions.StartTime/EndTime. Zero is open.
	Since int64
	Until int64
	// PageLimit is events per paging round: 0 uses exportDefaultPageLimit, and a
	// value outside 1..exportMaxPageLimit is refused rather than silently clamped.
	PageLimit int
	// CallIDs, when non-empty, keeps only lines whose metadata call_id is in the
	// set. There is no metadata index, so this is a read-then-filter pass over the
	// authorized partitions and excluded events are counted in Filtered — never
	// reported as absent.
	CallIDs []string
}

// ExportedFact is one JSONL line: the complete FullEvent copy (embedded, so the
// field set is the storage contract's rather than a projection of it) plus the
// causal parent pointer, which FullEvent itself does not carry.
type ExportedFact struct {
	memory.FullEvent
	// ParentKey is the canonical hex event key of this event's predecessor, or ""
	// when the store holds no evidence of one. Absence is counted
	// (ParentKeyMissing), never invented.
	ParentKey string `json:"parent_key"`
}

// ExportManifest states what the snapshot contains and how completely it was
// read. Every counter is a flat top-level field on purpose: the offline reader
// treats a nested object as zero, and a zero read as "nothing went wrong" would
// turn a partial export into a claimed-complete one — the same rule the capture
// seal follows in MarshalCaptureManifest.
type ExportManifest struct {
	// GeneratedAt is this export's Unix-millisecond wall clock. Cutoff is the
	// upper time bound honoured (opts.Until, or GeneratedAt when unset).
	GeneratedAt int64 `json:"generated_at"`
	Cutoff      int64 `json:"cutoff"`
	// Partitions echoes the effective (deduplicated, sorted) allowlist.
	Partitions []int `json:"partitions"`
	PageLimit  int   `json:"page_limit"`

	EventsWritten int `json:"events_written"`
	// Forbidden counts bodies that surfaced inside an authorized paging round but
	// failed the pre-write authorization re-check, and were therefore not written.
	Forbidden int `json:"forbidden"`
	// Filtered counts events excluded by an explicit CallIDs selection.
	Filtered int `json:"filtered"`
	// ParentKeyMissing counts written lines whose parent_key resolved to "".
	ParentKeyMissing int `json:"parent_key_missing"`

	// FeedbackTotal counts written feedback events; the remaining columns
	// partition exactly those.
	FeedbackTotal        int `json:"feedback_total"`
	JoinBound            int `json:"join_bound"`
	JoinMissing          int `json:"join_missing"`
	JoinExpiredOrMissing int `json:"join_expired_or_missing"`
	JoinForbiddenParent  int `json:"join_forbidden_parent"`
	JoinAmbiguous        int `json:"join_ambiguous"`

	// ReadErrors counts failed paging reads and failed event reads; Pages counts
	// paging rounds actually issued.
	ReadErrors int `json:"read_errors"`
	Pages      int `json:"pages"`

	// SourceSHA256 is the digest of exactly the bytes written, tying the snapshot
	// file to this manifest.
	SourceSHA256 string `json:"source_sha256"`
	// Complete is true only when no paging round, event read or write failed.
	Complete bool `json:"complete"`
	// Errs is how many distinct failures were folded into the returned error.
	Errs int `json:"errs"`
}

// factReader is the exporter's entire read surface. Keeping the seam this narrow
// is the structural half of the "export never writes" promise: no StoreEvent,
// DeleteEvent or lifecycle method is reachable from here.
type factReader interface {
	QueryEvents(query memory.QueryOptions) ([]memory.EventReference, error)
	GetEvent(key int64) (*memory.FullEvent, error)
}

// joinOutcome is the column a feedback association landed in.
type joinOutcome int

const (
	// joinPending resolved to a parent call_id; whether it is ambiguous is decided
	// once the whole snapshot has been walked.
	joinPending joinOutcome = iota
	// joinMissing: no parent pointer, or the parent carries no call_id.
	joinMissing
	// joinExpiredOrMissing: the parent could not be read. The reason (TTL, eviction,
	// a pointer to something that never existed) is deliberately not guessed.
	joinExpiredOrMissing
	// joinForbiddenParent: the parent sits in a partition nobody authorized.
	joinForbiddenParent
)

type feedbackJoin struct {
	feedbackKey int64
	parentKey   int64
	callID      string
	outcome     joinOutcome
}

type trainingExporter struct {
	reader     factReader
	rel        memory.RelationStore
	writer     io.Writer
	digest     hash.Hash
	allowed    map[int]bool
	callFilter map[string]bool
	options    ExportOptions

	manifest ExportManifest
	joins    []feedbackJoin
	errs     []error
}

// ExportTrainingFacts streams the authorized fact snapshot into w and returns the
// manifest describing it. A partial read returns the manifest AND a non-nil
// error with Complete=false: an incomplete snapshot is never presented as a
// finished one.
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func ExportTrainingFacts(ctx context.Context, store memory.MemoryStore, opts ExportOptions, w io.Writer) (ExportManifest, error) {
	exp, err := newTrainingExporter(store, opts, w)
	if err != nil {
		exp.manifest.Complete = false
		exp.manifest.Errs = 1
		return exp.manifest, err
	}
	exp.run(ctx)
	return exp.finish()
}

func newTrainingExporter(store memory.MemoryStore, opts ExportOptions, w io.Writer) (trainingExporter, error) {
	exp := trainingExporter{
		manifest: ExportManifest{Complete: true, Partitions: []int{}},
		options:  opts,
	}
	switch {
	case store == nil:
		return exp, ErrExportNilStore
	case w == nil:
		return exp, ErrExportNilWriter
	}

	allowed, partitions := normalizePartitions(opts.PartitionIDs)
	if len(partitions) == 0 {
		return exp, ErrExportAuthorization
	}
	pageLimit := opts.PageLimit
	if pageLimit == 0 {
		pageLimit = exportDefaultPageLimit
	}
	if pageLimit < 1 || pageLimit > exportMaxPageLimit {
		return exp, ErrExportPageLimitExceeded
	}

	exp.reader = store
	exp.writer = w
	exp.digest = sha256.New()
	exp.allowed = allowed
	exp.callFilter = toStringSet(opts.CallIDs)
	exp.manifest.Partitions = partitions
	exp.manifest.PageLimit = pageLimit
	exp.manifest.GeneratedAt = time.Now().UnixMilli()
	exp.manifest.Cutoff = opts.Until
	if exp.manifest.Cutoff == 0 {
		exp.manifest.Cutoff = exp.manifest.GeneratedAt
	}
	if provider, ok := store.(memory.RelationStoreProvider); ok {
		exp.rel = provider.RelationStore()
	}
	return exp, nil
}

func (e *trainingExporter) run(ctx context.Context) {
	for _, pid := range e.manifest.Partitions {
		if err := ctx.Err(); err != nil {
			e.fail(err)
			return
		}
		e.walkPartition(ctx, pid)
	}
}

// walkPartition pages one authorized partition on its own. Paging uses Offset,
// not an event-key cursor: the store orders a page by semantic Timestamp while
// MinEventKey bounds the write-order axis, so cutting on the key axis would
// silently drop async write-back events whose key is lower than the cursor (see
// the TIME CONTRACT on memory.FullEvent). A snapshot that silently loses facts is
// worse than one that reports itself incomplete.
func (e *trainingExporter) walkPartition(ctx context.Context, pid int) {
	seen := make(map[int64]bool)
	for offset := 0; ; {
		if err := ctx.Err(); err != nil {
			e.fail(err)
			return
		}
		query := memory.QueryOptions{
			PartitionIDs: []int{pid},
			EventTypes:   e.options.EventTypes,
			StartTime:    e.options.Since,
			EndTime:      e.options.Until,
			Limit:        e.manifest.PageLimit,
			Offset:       offset,
		}
		refs, err := e.reader.QueryEvents(query)
		e.manifest.Pages++
		if err != nil {
			e.manifest.ReadErrors++
			e.fail(fmt.Errorf("rl: training export: partition %d page at offset %d: %w", pid, offset, err))
			return
		}
		fresh := 0
		for _, ref := range refs {
			if ref.EventKey == 0 || seen[ref.EventKey] {
				continue
			}
			seen[ref.EventKey] = true
			fresh++
			e.exportEvent(ref.EventKey)
		}
		if len(refs) < e.manifest.PageLimit {
			return
		}
		if fresh == 0 {
			e.fail(fmt.Errorf("rl: training export: partition %d repeated page at offset %d", pid, offset))
			return
		}
		offset += len(refs)
	}
}

// exportEvent reads one reference in full, re-checks authorization against the
// body, then writes it. A reference's partition field is never the verdict: the
// stored body and the event key's own partition claim are what get verified.
func (e *trainingExporter) exportEvent(key int64) {
	evt, err := e.reader.GetEvent(key)
	if err != nil || evt == nil {
		e.manifest.ReadErrors++
		e.fail(fmt.Errorf("rl: training export: read event %d: %w", key, err))
		return
	}
	if !e.allowed[evt.PartitionID] || memory.PartitionIDFromEventKey(evt.EventKey) != evt.PartitionID {
		e.manifest.Forbidden++
		return
	}
	if len(e.callFilter) > 0 && !e.callFilter[evt.Metadata[exportMetaKeyCallID]] {
		e.manifest.Filtered++
		return
	}

	parentKey := e.resolveParentKey(evt)
	line := ExportedFact{FullEvent: *evt, ParentKey: parentKey}
	data, err := json.Marshal(line)
	if err != nil {
		e.manifest.ReadErrors++
		e.fail(fmt.Errorf("rl: training export: encode event %d: %w", key, err))
		return
	}
	data = append(data, '\n')
	if _, err := e.writer.Write(data); err != nil {
		e.fail(fmt.Errorf("rl: training export: write event %d: %w", key, err))
		return
	}
	e.digest.Write(data)
	e.manifest.EventsWritten++
	if parentKey == "" {
		e.manifest.ParentKeyMissing++
	}
	if evt.EventType == tagentevent.TypeFeedback {
		e.manifest.FeedbackTotal++
		e.recordFeedbackJoin(evt)
	}
}

// resolveParentKey reports an event's predecessor from the evidence the store
// actually holds: the causal edge first, then a parent_key the event carries in
// its own content. Proximity, timestamps and "the previous line" are never used.
func (e *trainingExporter) resolveParentKey(evt *memory.FullEvent) string {
	if e.rel != nil {
		if pk, err := e.rel.GetParent(evt.EventKey); err == nil && pk != 0 {
			return tagentevent.FormatEventKey(pk)
		}
	}
	if pk := parentKeyFromContent(evt.Content); pk != 0 {
		return tagentevent.FormatEventKey(pk)
	}
	return ""
}

// recordFeedbackJoin resolves feedback → parent → parent call_id. The parent's
// partition is verified from the parent's own identity BEFORE any read, and the
// body again after it, so an unauthorized parent is refused without ever being
// fetched.
func (e *trainingExporter) recordFeedbackJoin(evt *memory.FullEvent) {
	parentKey := parentKeyFromContent(evt.Content)
	if parentKey == 0 && e.rel != nil {
		if pk, err := e.rel.GetParent(evt.EventKey); err == nil {
			parentKey = pk
		}
	}
	if parentKey == 0 {
		e.joins = append(e.joins, feedbackJoin{feedbackKey: evt.EventKey, outcome: joinMissing})
		return
	}
	if !e.allowed[memory.PartitionIDFromEventKey(parentKey)] {
		e.joins = append(e.joins, feedbackJoin{feedbackKey: evt.EventKey, parentKey: parentKey, outcome: joinForbiddenParent})
		return
	}
	parent, err := e.reader.GetEvent(parentKey)
	if err != nil || parent == nil {
		e.joins = append(e.joins, feedbackJoin{feedbackKey: evt.EventKey, parentKey: parentKey, outcome: joinExpiredOrMissing})
		return
	}
	if !e.allowed[parent.PartitionID] || memory.PartitionIDFromEventKey(parent.EventKey) != parent.PartitionID {
		e.joins = append(e.joins, feedbackJoin{feedbackKey: evt.EventKey, parentKey: parentKey, outcome: joinForbiddenParent})
		return
	}
	callID := parent.Metadata[exportMetaKeyCallID]
	if callID == "" {
		e.joins = append(e.joins, feedbackJoin{feedbackKey: evt.EventKey, parentKey: parentKey, outcome: joinMissing})
		return
	}
	e.joins = append(e.joins, feedbackJoin{
		feedbackKey: evt.EventKey, parentKey: parentKey, callID: callID, outcome: joinPending,
	})
}

// finalizeJoins decides the pending associations. Several feedbacks pointing at
// one call (same parent) are all bound — repeated judgement is data, not an
// error. "ambiguous" is reserved for the reverse case: one call_id reached from
// contradictory parents, which is broken attribution and must not be quietly
// bound to either side. No numeric reward is produced here.
func (e *trainingExporter) finalizeJoins() {
	parentByCall := make(map[string]int64)
	contradictory := make(map[string]bool)
	for _, j := range e.joins {
		if j.outcome != joinPending {
			continue
		}
		if seen, ok := parentByCall[j.callID]; ok && seen != j.parentKey {
			contradictory[j.callID] = true
		}
		if _, ok := parentByCall[j.callID]; !ok {
			parentByCall[j.callID] = j.parentKey
		}
	}

	for _, j := range e.joins {
		switch j.outcome {
		case joinMissing:
			e.manifest.JoinMissing++
		case joinExpiredOrMissing:
			e.manifest.JoinExpiredOrMissing++
		case joinForbiddenParent:
			e.manifest.JoinForbiddenParent++
		case joinPending:
			if contradictory[j.callID] {
				e.manifest.JoinAmbiguous++
			} else {
				e.manifest.JoinBound++
			}
		}
	}
}

func (e *trainingExporter) finish() (ExportManifest, error) {
	e.finalizeJoins()
	e.manifest.SourceSHA256 = hex.EncodeToString(e.digest.Sum(nil))
	e.manifest.Errs = len(e.errs)
	if len(e.errs) > 0 {
		e.manifest.Complete = false
		return e.manifest, errors.Join(e.errs...)
	}
	return e.manifest, nil
}

func (e *trainingExporter) fail(err error) {
	e.errs = append(e.errs, err)
	e.manifest.Complete = false
}

// parentKeyFromContent reads the parent pointer from an event's JSON content.
// Only a well-formed parent_key counts; anything else is "no evidence", which the
// caller reports as missing instead of guessing at.
func parentKeyFromContent(content string) int64 {
	if content == "" {
		return 0
	}
	var payload struct {
		ParentKey string `json:"parent_key"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil || payload.ParentKey == "" {
		return 0
	}
	key, err := tagentevent.ParseEventKey(payload.ParentKey)
	if err != nil {
		return 0
	}
	return key
}

// normalizePartitions deduplicates the allowlist and puts it in a fixed order, so
// two callers authorizing the same partitions get byte-identical snapshots.
func normalizePartitions(ids []int) (map[int]bool, []int) {
	allowed := make(map[int]bool, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id < 0 || allowed[id] {
			continue
		}
		allowed[id] = true
		out = append(out, id)
	}
	sort.Ints(out)
	return allowed, out
}

func toStringSet(values []string) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]bool, len(values))
	for _, v := range values {
		if v != "" {
			set[v] = true
		}
	}
	return set
}
