// 契约: docs/FACT-VOCABULARY.md#families
//
// The fold plan: the judgment one producer files and one consumer executes. A
// plan reads a projection and answers the products a fold would mint without
// touching it; applying the products answers the entries one direct fold
// answers, so the two halves of compression never share a step.
package replay

import (
	"context"

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
// nothing — applying one judgment twice is applying it once.
func (p *ReplayProjection) ApplyProducts(products []FoldProduct) {
	if len(products) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = applyFoldProducts(p.entries, products)
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
