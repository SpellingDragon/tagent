// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// TestDigestRendersTheSamePassIdentically pins the digest: one pass at one watermark renders identically, and every reference a bound leaves out is counted.
// - two renders of the same pass at the same watermark are byte-identical
// - the partition lines stop at the partition bound
// - the fact lines stop at the fact bound
// - omitted counts every reference the fact listing left out, and the summary states it
// - a long fact summary is shortened on a rune boundary
// - a pass opened at another watermark is refused by name
// - an absent pass is unavailable by name, never a quiet empty summary
package meditation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

func TestDigestRendersTheSamePassIdentically(t *testing.T) {
	ctx := context.Background()
	scan := digestScan(10)
	digest := NewScanDigest(scan, 3)
	since := Watermark{Moment: meditationNow}

	first, err := digest.Render(ctx, since)
	if err != nil {
		t.Fatalf("render the first digest: %v", err)
	}
	second, err := digest.Render(ctx, since)
	if err != nil {
		t.Fatalf("render the second digest: %v", err)
	}
	if first.Body != second.Body || first.Omitted != second.Omitted {
		t.Fatalf("digests differ over the same pass:\nfirst  %+v\nsecond %+v", first, second)
	}
	if got := strings.Count(first.Body, "\n- p"); got != digestPartitionLimit {
		t.Fatalf("partition lines = %d, want the bound of %d", got, digestPartitionLimit)
	}
	if got := strings.Count(first.Body, "\n- ["); got != 3 {
		t.Fatalf("fact lines = %d, want the bound of 3", got)
	}
	if first.Omitted != 17 {
		t.Fatalf("omitted = %d, want the 17 references beyond the three listed", first.Omitted)
	}
	if !strings.Contains(first.Body, "omitted: 17 fact(s) beyond the listing bound") {
		t.Fatalf("body = %q, want the omitted count stated beside the rendering", first.Body)
	}
	if !strings.Contains(first.Body, "attribution: 3 hydrated fact(s) carry no trigger source") {
		t.Fatalf("body = %q, want the unknown-lineage count stated", first.Body)
	}
	if !strings.Contains(first.Body, "…") || strings.Contains(first.Body, strings.Repeat("x", 121)) {
		t.Fatalf("body = %q, want a long summary shortened on a rune boundary", first.Body)
	}

	if _, err := digest.Render(ctx, Watermark{Moment: meditationNow.Add(time.Second)}); !errors.Is(err, ErrDigestScanMismatch) {
		t.Fatalf("render at another watermark = %v, want ErrDigestScanMismatch", err)
	}
	if summary, err := NewScanDigest(nil, 0).Render(ctx, since); !errors.Is(err, ErrDigestFaceUnavailable) || summary.Body != "" {
		t.Fatalf("render an absent pass = (%+v, %v), want ErrDigestFaceUnavailable and no body", summary, err)
	}
}

// digestScan returns one completed pass over ten partitions and facts hydrated facts,
// each summary long enough to be shortened by the digest bound.
func digestScan(facts int) *ObservationScan {
	scan := &ObservationScan{
		Watermark: meditationNow,
		Counts:    ScanCounts{ReferenceTotal: 20, HydratedTotal: facts, Unreadable: 1, Foreign: 2, UnknownLineage: 3},
	}
	for i := 0; i < 10; i++ {
		scan.Partitions = append(scan.Partitions, PartitionActivity{
			Name: fmt.Sprintf("p%d", i), Partition: i, References: i, External: i, SelfManaged: 1,
		})
	}
	for i := 0; i < facts; i++ {
		scan.Facts = append(scan.Facts, ScannedFact{
			Identity:    FactIdentity{Key: event.EventID(fmt.Sprintf("fact-%02d", i)), Partition: "p0"},
			Content:     FactContent{Type: "agent.output", Summary: strings.Repeat("x", 130), Timestamp: meditationNow},
			Attribution: FactAttribution{Lineage: "user", SelfManaged: i%2 == 1},
		})
	}
	return scan
}
