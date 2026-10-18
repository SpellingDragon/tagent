package evolution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// Digest is the reflection loop's self-state summary: a deterministic, bounded
// rendering of the observed face since a watermark that a reflection turn
// reads without spending a model call.
//
// The rendering is bounded, so a face with a long backlog reports how many of
// its facts the bound left out instead of growing without limit, and the same
// face at the same watermark renders the same summary. A render that cannot
// reach the face reports that it is unavailable by name rather than presenting
// an empty summary as a quiet face.
// The redline list (3 rules):
// MUST render the same summary for the same face at the same watermark.
// MUST report the count a bound left out beside the rendering it bounded.
// MUST NOT present an unreachable face as a quiet one.
type Digest interface {
	// Render returns the bounded self-state summary of the observed face since
	// the given watermark. When the face holds more than the bound renders, the
	// count left out is reported in the result instead of being dropped in
	// silence.
	Render(ctx context.Context, since Watermark) (DigestSummary, error)
}

// DigestSummary is the rendered self-state summary together with the count of
// facts the render bound left out.
type DigestSummary struct {
	// Body is the rendered summary of the observed face.
	Body string
	// Omitted is the number of facts the render bound left out of Body.
	Omitted int
}

// ErrDigestFaceUnavailable reports a digest asked to render without the pass it
// summarizes. An absent pass is named as unavailable, never presented as a quiet
// face.
var ErrDigestFaceUnavailable = errors.New("evolution: the digest has no observed pass to render")

// ErrDigestScanMismatch reports a digest asked to render a pass that opened at a
// watermark other than the one it was asked about. The pass a digest summarizes
// is the one its caller already read, so the rendering never triggers a second
// pass over the fact chain.
var ErrDigestScanMismatch = errors.New("evolution: the observed pass opened at another watermark")

const (
	// digestPartitionLimit bounds how many partition lines one digest lists.
	digestPartitionLimit = 8
	// digestSummaryLimit bounds one fact summary in runes.
	digestSummaryLimit = 120
)

// ScanDigest renders the self-state summary out of one completed observation
// pass: the watermark the window opened at, one tally line per observed
// partition and the hydrated facts the pass kept, all in the deterministic order
// the pass read them.
//
// The rendering spends no model call, blocks on no external surface, reads the
// fact chain no further than the pass already did and stays bounded whatever the
// backlog: partitions and facts beyond the bounds, and references the pass did not
// hydrate, are counted in Omitted instead of being dropped in silence.
type ScanDigest struct {
	scan      *ObservationScan
	factLimit int
}

// NewScanDigest builds a digest over one completed pass, limiting the listed
// facts to factLimit entries; a non-positive limit runs the default bound.
func NewScanDigest(scan *ObservationScan, factLimit int) *ScanDigest {
	if factLimit <= 0 {
		factLimit = DefaultDigestFactLimit
	}
	return &ScanDigest{scan: scan, factLimit: factLimit}
}

// Render returns the bounded self-state summary of the pass this digest was built
// over, refusing by name when there is no pass or when the pass opened at another
// watermark than the one since names.
func (d *ScanDigest) Render(ctx context.Context, since Watermark) (DigestSummary, error) {
	if err := ctx.Err(); err != nil {
		return DigestSummary{}, err
	}
	if d == nil || d.scan == nil {
		return DigestSummary{}, ErrDigestFaceUnavailable
	}
	scan := d.scan
	if !since.Moment.Equal(scan.Watermark) {
		return DigestSummary{}, fmt.Errorf("%w: pass opened at %s, rendered for %s",
			ErrDigestScanMismatch, describeMoment(scan.Watermark), describeMoment(since.Moment))
	}
	lines := []string{fmt.Sprintf("observed face since %s: %d reference(s), %d hydrated, %d unreadable, %d outside the face",
		describeMoment(scan.Watermark), scan.Counts.ReferenceTotal, scan.Counts.HydratedTotal, scan.Counts.Unreadable, scan.Counts.Foreign)}
	for i, part := range scan.Partitions {
		if i >= digestPartitionLimit {
			break
		}
		lines = append(lines, fmt.Sprintf("- %s: %d reference(s), %d external, %d self-managed",
			part.Name, part.References, part.External, part.SelfManaged))
	}
	listed := 0
	for _, fact := range scan.Facts {
		if listed >= d.factLimit {
			break
		}
		lines = append(lines, "- "+describeFact(fact))
		listed++
	}
	omitted := scan.Counts.ReferenceTotal - listed
	if omitted < 0 {
		omitted = 0
	}
	if scan.Counts.UnknownLineage > 0 {
		lines = append(lines, fmt.Sprintf("attribution: %d hydrated fact(s) carry no trigger source and count as self-managed", scan.Counts.UnknownLineage))
	}
	if omitted > 0 {
		lines = append(lines, fmt.Sprintf("omitted: %d fact(s) beyond the listing bound", omitted))
	}
	return DigestSummary{Body: strings.Join(lines, "\n"), Omitted: omitted}, nil
}

// describeFact renders one hydrated fact as a digest line: its partition, type,
// truncated summary, lineage reading and recording moment.
func describeFact(fact ScannedFact) string {
	lineage := "external"
	if fact.Attribution.SelfManaged {
		lineage = "self-managed"
	}
	return fmt.Sprintf("[%s] %s: %s (%s, %s)", fact.Identity.Partition, fact.Content.Type, truncateSummary(fact.Content.Summary), lineage, fact.Content.Timestamp.UTC().Format(time.RFC3339Nano))
}

// truncateSummary shortens one stored summary to the digest bound on rune
// boundaries, so a long summary shortens the line without corrupting it.
func truncateSummary(summary string) string {
	if summary == "" {
		return "(no summary)"
	}
	runes := []rune(summary)
	if len(runes) <= digestSummaryLimit {
		return summary
	}
	return string(runes[:digestSummaryLimit]) + "…"
}

// The declared method, interface kind and fields must keep exactly these shapes:
// a renamed field or a changed method signature stops this block from compiling.
var (
	_ func(Digest, context.Context, Watermark) (DigestSummary, error) = Digest.Render

	_ Digest = (Digest)(nil)

	_ Digest = (*ScanDigest)(nil)

	_ = DigestSummary{
		Body:    "",
		Omitted: 0,
	}
)
