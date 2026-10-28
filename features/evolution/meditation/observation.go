package meditation

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
)

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// LineageReflection is the trigger-source stamp a reflection input carries: the
// reflection turn's own lineage, which is self-managed traffic, so an agent's own
// reflection never makes it reflect on itself again.
const LineageReflection = "meditation"

const (
	// DefaultObservationPageLimit bounds one pass over the observed face; a face
	// with a longer backlog reports the bound instead of growing the pass.
	DefaultObservationPageLimit = 1000
	// DefaultDigestFactLimit bounds how many hydrated facts one digest lists.
	DefaultDigestFactLimit = 8
)

// ErrObservedFaceUnwired reports a pass attempted while the read face of the
// observed fact chain is not wired. The reflection gate stays closed: the fact
// chain is the only data face of the freshness judgment.
var ErrObservedFaceUnwired = errors.New("evolution: the observed fact-chain read face is not wired")

// ErrWatermarkSourceUnwired reports a tick attempted while the execution
// watermark has no source. The gate stays closed instead of reading a fresh
// default.
var ErrWatermarkSourceUnwired = errors.New("evolution: the execution watermark source is not wired")

// ObservedFacts is the read-only fact-chain face the reflection judgment runs on:
// the conditional retrieval the authorization-scoped query answers through, beside
// the committed-fact read that hydrates one reference into the fact it names.
//
// A query answers references only, which carry no trigger source, so the judgment
// hydrates the newest references one by one and stops at its first hit. Both
// calls are reads: a pass never writes the chain it observes.
// The redline list (3 rules):
// MUST answer references through the authorization-scoped query and hydrate each one through its committed fact.
// MUST NOT write the chain it observes.
// MUST NOT answer an identity that never committed as an empty fact.
type ObservedFacts interface {
	// QueryEvents returns the references matching opts.
	QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)
	// CommittedFact returns the committed fact the identity names; an identity
	// with no committed fact behind it is an error, never an empty fact.
	CommittedFact(ctx context.Context, id event.EventID) (event.Event, error)
}

// PartitionActivity is one observed partition's tally in a pass: how much of the
// reference page belongs to it and how the hydrated sample splits by lineage.
type PartitionActivity struct {
	// Name is the namespace name the partition was resolved from.
	Name string
	// Partition is the storage partition the name resolves to.
	Partition int
	// References counts this partition's entries on the reference page, which
	// the read face answers without any hydration.
	References int
	// External counts hydrated facts the freshness policy reads as new
	// material: the freshness evidence.
	External int
	// SelfManaged counts hydrated facts the freshness policy reads as
	// self-managed, unknown attribution included.
	SelfManaged int
}

// FactIdentity is which fact a pass read and where it was filed: the canonical
// identity the fact committed under and the namespace name it was filed as.
type FactIdentity struct {
	// Key is the canonical identity of the fact.
	Key event.EventID
	// Partition is the namespace name the fact was filed under.
	Partition string
}

// FactContent is what a fact recorded: its frozen type, its stored summary and the
// moment it was recorded.
type FactContent struct {
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the stored summary of the fact.
	Summary string
	// Timestamp is the moment the fact was recorded.
	Timestamp time.Time
}

// FactAttribution is whose traffic a fact is: the trigger-source stamp it carries
// and the derivation of that stamp.
type FactAttribution struct {
	// Lineage is the trigger-source stamp the fact carries; empty means the fact
	// carries none.
	Lineage string
	// SelfManaged reports the lineage derivation of this fact.
	SelfManaged bool
}

// ScannedFact is one hydrated fact of a pass, kept in the deterministic order the
// pass read it.
type ScannedFact struct {
	// Identity is the fact and the partition it was filed under.
	Identity FactIdentity
	// Content is what the fact recorded.
	Content FactContent
	// Attribution is whose traffic the fact is.
	Attribution FactAttribution
}

// ScanCounts is the accounting of one pass: the reference page it read, how many
// facts it hydrated, and the references that yielded no freshness evidence.
type ScanCounts struct {
	// ReferenceTotal is the size of the reference page the pass read.
	ReferenceTotal int
	// HydratedTotal is the number of facts the pass hydrated.
	HydratedTotal int
	// Unreadable counts references the pass could not hydrate; an unreadable fact
	// is never counted as freshness.
	Unreadable int
	// UnknownLineage counts hydrated facts that carry no trigger-source stamp.
	UnknownLineage int
	// Foreign counts hydrated facts filed under a partition outside the observed
	// face; such a fact is never counted as freshness either.
	Foreign int
}

// ScanVerdict is what the freshness judgment read off one pass: whether the face
// holds a fact newer than the watermark that the freshness policy reads as new
// material, and that fact.
type ScanVerdict struct {
	// Novelty reports whether the face holds a fact newer than the watermark that
	// the freshness policy reads as new material.
	Novelty bool
	// Recent is the newest fact the freshness policy reads as new material, when
	// the pass found one.
	Recent *ScannedFact
}

// ObservationScan is the bounded evidence of one pass over the observed face: the
// watermark the window opened at, the per-partition tallies, the hydrated sample,
// the accounting of that read and the verdict the freshness judgment read from it.
//
// The evidence is kept so the digest renders from this pass and never reads the
// fact chain a second time.
type ObservationScan struct {
	// Watermark is the execution watermark the window opened after.
	Watermark time.Time
	// Partitions holds one tally line per observed partition, in declared order.
	Partitions []PartitionActivity
	// Facts is the hydrated sample in read order, bounded by the pass's fact limit.
	Facts []ScannedFact
	// Counts is the accounting of the read.
	Counts ScanCounts
	// Verdict is what the freshness judgment read from this pass.
	Verdict ScanVerdict
}

// ObservationScanRequest is the window and bounds of one pass over the observed
// face.
type ObservationScanRequest struct {
	// Since is the execution watermark the window opens strictly after; the zero
	// value opens the window at the epoch.
	Since time.Time
	// PageLimit bounds the reference page; zero runs the default bound.
	PageLimit int
	// FactLimit bounds the hydrated sample; zero runs the default bound.
	FactLimit int
}

// windowStart encodes a strictly-after watermark onto the inclusive lower bound
// of the read face.
func windowStart(since time.Time) int64 {
	if since.IsZero() {
		return 0
	}
	return since.UnixMilli() + 1
}

// indexOf locates a partition's tally line by name, reporting whether the
// partition is on the observed face at all.
func (s *ObservationScan) indexOf(name string) (int, bool) {
	for i := range s.Partitions {
		if s.Partitions[i].Name == name {
			return i, true
		}
	}
	return 0, false
}

// partitionOf reads the partition a hydrated fact was filed under, falling back
// to the partition its reference named when the fact carries no readable one.
func partitionOf(fact event.Event, fallback int) int {
	if raw, held := fact.Meta[event.MetaKeyPartitionID]; held {
		if parsed, err := strconv.Atoi(raw); err == nil {
			return parsed
		}
	}
	return fallback
}

// observe runs one pass over the observed face: the authorization-scoped query
// answers the reference page, the tally splits that page by partition, and the
// hydration walk reads the newest references until the first fact the freshness
// policy reads as new material, so the judgment costs one page read and at most
// one page of hydrations.
//
// A read failure is returned by name and never reinterpreted as an empty face or
// as a fresh one: the gate that consumes this pass stays closed and there is no
// second criterion to fall back to. A reference that cannot be hydrated is counted
// and skipped, and never counted as freshness.
func observe(ctx context.Context, scope ReflectionScope, facts ObservedFacts, req ObservationScanRequest) (*ObservationScan, error) {
	if facts == nil {
		return nil, ErrObservedFaceUnwired
	}
	pageLimit := req.PageLimit
	if pageLimit <= 0 {
		pageLimit = DefaultObservationPageLimit
	}
	factLimit := req.FactLimit
	if factLimit <= 0 {
		factLimit = DefaultDigestFactLimit
	}
	scan := &ObservationScan{Watermark: req.Since, Partitions: make([]PartitionActivity, 0, len(scope.Names))}
	for i, name := range scope.Names {
		scan.Partitions = append(scan.Partitions, PartitionActivity{Name: name, Partition: scope.Partitions[i]})
	}
	refs, err := facts.QueryEvents(ctx, memory.QueryOptions{
		PartitionIDs: append([]int(nil), scope.Partitions...),
		StartTime:    windowStart(req.Since),
		OrderBy:      memory.QueryOrderByDesc,
		Limit:        pageLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("evolution: query the observed face: %w", err)
	}
	scan.Counts.ReferenceTotal = len(refs)
	for _, ref := range refs {
		for i := range scan.Partitions {
			if scan.Partitions[i].Partition == ref.PartitionID {
				scan.Partitions[i].References++
				break
			}
		}
	}
	for _, ref := range refs {
		if len(scan.Facts) >= factLimit {
			break
		}
		fact, err := facts.CommittedFact(ctx, ref.Key)
		if err != nil {
			scan.Counts.Unreadable++
			continue
		}
		if !scan.Watermark.IsZero() && !fact.Timestamp.After(scan.Watermark) {
			continue
		}
		partition := partitionOf(fact, ref.PartitionID)
		at, onFace := partitionIndexOf(scan, partition)
		if !onFace {
			scan.Counts.Foreign++
			continue
		}
		stamp := fact.Meta[event.MetaKeyTriggerSource]
		novel := NoveltyPolicy{}.Novel(fact)
		entry := ScannedFact{
			Identity: FactIdentity{Key: ref.Key, Partition: scan.Partitions[at].Name},
			Content: FactContent{
				Type:      fact.Type,
				Summary:   fact.Meta[event.MetaKeyEventSummary],
				Timestamp: fact.Timestamp,
			},
			Attribution: FactAttribution{Lineage: stamp, SelfManaged: SelfManagedLineage(stamp)},
		}
		scan.Counts.HydratedTotal++
		scan.Facts = append(scan.Facts, entry)
		if !novel {
			scan.Partitions[at].SelfManaged++
			if stamp == "" {
				scan.Counts.UnknownLineage++
			}
			continue
		}
		scan.Partitions[at].External++
		recent := entry
		scan.Verdict.Recent = &recent
		scan.Verdict.Novelty = true
		break
	}
	return scan, nil
}

// partitionIndexOf locates a tally line by storage partition.
func partitionIndexOf(scan *ObservationScan, partition int) (int, bool) {
	for i := range scan.Partitions {
		if scan.Partitions[i].Partition == partition {
			return i, true
		}
	}
	return 0, false
}
