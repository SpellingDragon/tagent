// 契约: docs/FACT-VOCABULARY.md#families
//
// The fold plan: the judgment one producer files and one consumer executes. A
// plan reads a projection and answers the products a fold would mint without
// touching it; applying the products answers the entries one direct fold
// answers, so the two halves of compression never share a step.
package replay

import (
	"context"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// FoldProduct is one product a plan would mint: the synthetic entry to place,
// the entries it covers and the compress request it answers. A product is what
// a producer judges and what a consumer executes, so the judgment and the
// execution never share a step.
type FoldProduct struct {
	// Entry is the synthetic entry the projection carries under Entry.Key.
	Entry ProjectionEntry
	// Covered lists the entry keys the product consumes, in their original
	// order: the process detail of one closed segment, or the middle bands.
	Covered []event.EventID
	// Request is the judgment this product answers.
	Request event.CompressRequest
}

// AgeFoldPlanReport is the judgment of one fold plan: what the plan would
// consume and the products it would mint, beside the volume it read. Reading a
// plan never touches the projection.
type AgeFoldPlanReport struct {
	// Volume is the rendered volume the judgment read.
	Volume int
	// Threshold is the volume the plan measures against.
	Threshold int
	// Triggered reports whether the material is over its bound.
	Triggered bool
	// Counts reports what the products would consume.
	Counts FoldedCounts
	// Products lists the products the plan would mint, in application order.
	Products []FoldProduct
}

// FoldedCounts reports what one fold judgment would consume.
type FoldedCounts struct {
	// Segment counts the process-detail entries a segment product consumes.
	Segment int
	// Middle counts the entries a history summary consumes.
	Middle int
}

// PlanByAge judges one fold plan without touching the projection: it answers the
// products the fold would mint, so a producer can file the judgment and a
// consumer can execute it later, each on its own.
func (p *ReplayProjection) PlanByAge(ctx context.Context, plan AgeFoldPlan) (AgeFoldPlanReport, error) {
	if err := ctx.Err(); err != nil {
		return AgeFoldPlanReport{}, err
	}
	p.mu.Lock()
	entries := append([]ProjectionEntry(nil), p.entries...)
	p.mu.Unlock()
	return planFold(entries, plan), nil
}

// planFold judges one plan over a fixed entry sequence.
func planFold(entries []ProjectionEntry, plan AgeFoldPlan) AgeFoldPlanReport {
	keepRecent := plan.KeepRecent
	if keepRecent < 1 {
		keepRecent = DefaultAgeKeepRecent
	}
	threshold := plan.VolumeThreshold
	if threshold <= 0 {
		threshold = DefaultAgeVolumeThreshold
	}
	report := AgeFoldPlanReport{Volume: len(RenderProjection(entries)), Threshold: threshold}
	if len(entries) == 0 || report.Volume <= threshold {
		return report
	}
	report.Triggered = true
	var middle []ProjectionEntry
	for _, segment := range SegmentsOf(entries, keepRecent) {
		if segment.Band != BandNew {
			middle = append(middle, segment.Entries...)
			continue
		}
		folded, product := foldProcessDetail(segment.Entries, segment.Complete)
		report.Counts.Segment += folded
		if product == nil {
			continue
		}
		report.Products = append(report.Products, FoldProduct{
			Entry:   *product,
			Covered: processDetailKeys(segment.Entries),
			Request: ageRequest(AgeReasonSegmentFold, *product),
		})
	}
	report.Counts.Middle = len(middle)
	if len(middle) > 0 {
		summary := historySummaryOf(middle)
		report.Products = append(report.Products, FoldProduct{
			Entry:   summary,
			Covered: entryKeysOf(middle),
			Request: ageRequest(AgeReasonHistorySummary, summary),
		})
	}
	return report
}

// ApplyProducts places the products a judgment minted onto the projection: each
// covered entry is replaced by its product, a history summary lands ahead of the
// material it summarizes, and a product whose entry is already held changes
// nothing — applying one judgment twice is applying it once. A covered entry
// leaves the projection with its per-key material (the kept original, the dedup
// key and the consumed mark), so the maps say what the rewritten entries say.
func (p *ReplayProjection) ApplyProducts(products []FoldProduct) {
	if len(products) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = applyFoldProducts(p.entries, products)
	p.forgetDroppedLocked()
}

// applyFoldProducts places one batch of products onto a fixed entry sequence.
func applyFoldProducts(entries []ProjectionEntry, products []FoldProduct) []ProjectionEntry {
	held := make(map[event.EventID]struct{}, len(entries))
	for _, entry := range entries {
		held[entry.Key] = struct{}{}
	}
	replacement := make(map[event.EventID]ProjectionEntry, len(entries))
	var heads []ProjectionEntry
	var live []FoldProduct
	for _, product := range products {
		if product.Entry.Key == "" {
			continue
		}
		if _, exists := held[product.Entry.Key]; exists {
			continue
		}
		switch product.Entry.Type {
		case ProjectionTypeHistorySummary:
			heads = append(heads, product.Entry)
		default:
			live = append(live, product)
		}
		for _, key := range product.Covered {
			replacement[key] = product.Entry
		}
	}
	if len(heads) == 0 && len(live) == 0 {
		return entries
	}
	out := make([]ProjectionEntry, 0, len(entries)+len(heads))
	placed := make(map[event.EventID]struct{}, len(live))
	for _, entry := range entries {
		product, covered := replacement[entry.Key]
		if !covered {
			out = append(out, entry)
			continue
		}
		if product.Type == ProjectionTypeHistorySummary {
			continue
		}
		if _, done := placed[product.Key]; done {
			continue
		}
		placed[product.Key] = struct{}{}
		out = append(out, product)
	}
	return append(append([]ProjectionEntry(nil), heads...), out...)
}

// RestoreProducts places the products a chain recorded onto the projection: the
// product fact names the key, the text and the entries it covers, so a restore
// rebuilds the entry the fold minted instead of folding the material again. The
// kind of product is read off the key's prefix and its moment off the first
// entry it covers, which is the moment the fold itself gave it. A key no fold
// mints, and a product whose entry the projection already holds, change nothing.
func (p *ReplayProjection) RestoreProducts(products []event.CompressProduct) {
	if len(products) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, product := range products {
		restored, ok := foldProductOf(product, p.entries)
		if !ok {
			continue
		}
		p.entries = applyFoldProducts(p.entries, []FoldProduct{restored})
	}
	p.forgetDroppedLocked()
}

// foldProductOf reads one product fact back as the fold product it recorded: the
// entry under the key the fact names, its kind read off the key prefix and its
// moment the moment the first covered entry carries. A key no fold mints answers
// no product, so a fact naming material this package never folded is skipped
// rather than placed under invented keys.
func foldProductOf(product event.CompressProduct, entries []ProjectionEntry) (FoldProduct, bool) {
	kind, ok := foldKindOf(product.Key)
	if !ok {
		return FoldProduct{}, false
	}
	return FoldProduct{
		Entry: ProjectionEntry{
			Key:     product.Key,
			Type:    kind,
			Summary: foldSummaryOf(kind),
			Content: product.Text,
			Time:    coveredMomentOf(product.Covered, entries),
		},
		Covered: append([]event.EventID(nil), product.Covered...),
		Request: event.CompressRequest{ID: product.Answers.Request},
	}, true
}

// foldKindOf reports the product kind one synthetic key names: the fold files a
// segment fold and a history summary under one prefix each, and the prefix is
// the only thing the fact carries about what it minted.
func foldKindOf(key event.EventID) (string, bool) {
	name := strings.TrimPrefix(string(key), "-")
	switch {
	case strings.HasPrefix(name, ageSegmentKeyPrefix+"-"):
		return ProjectionTypeSegmentFold, true
	case strings.HasPrefix(name, ageHistoryKeyPrefix+"-"):
		return ProjectionTypeHistorySummary, true
	default:
		return "", false
	}
}

// foldSummaryOf reports the one-line read a product of one kind carries, the
// same line the fold writes beside the text it mints.
func foldSummaryOf(kind string) string {
	if kind == ProjectionTypeHistorySummary {
		return "history summary"
	}
	return "segment fold"
}

// coveredMomentOf reports the moment the first covered entry carries: the fold
// stamps a product with the moment of the material it opened on, so a restored
// product reads the same moment the folded one did. Covered material the
// projection does not carry answers the zero moment, so a restore never
// invents one.
func coveredMomentOf(covered []event.EventID, entries []ProjectionEntry) time.Time {
	for _, key := range covered {
		for _, entry := range entries {
			if entry.Key == key {
				return entry.Time
			}
		}
	}
	return time.Time{}
}

// processDetailKeys reports the keys of the process-detail entries of one
// segment, in their original order.
func processDetailKeys(entries []ProjectionEntry) []event.EventID {
	out := make([]event.EventID, 0, len(entries))
	for _, entry := range entries {
		if isProcessDetail(entry) {
			out = append(out, entry.Key)
		}
	}
	return out
}

// entryKeysOf reports the keys of one entry sequence, in their original order.
func entryKeysOf(entries []ProjectionEntry) []event.EventID {
	out := make([]event.EventID, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Key)
	}
	return out
}
