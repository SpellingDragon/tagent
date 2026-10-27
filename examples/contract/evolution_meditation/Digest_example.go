package evolutionmeditationexample

import (
	"context"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/features/evolution/meditation"
)

// boundedDigest is the canonical self-state summary: it renders the observed facts since
// a watermark up to one bound and reports how many the bound left out.
type boundedDigest struct {
	facts []observedFact
	limit int
}

// observedFact is one fact of the observed face as the summary reads it.
type observedFact struct {
	at   time.Time
	line string
}

// Render returns the bounded self-state summary of the observed face since the given
// watermark, with the count the bound left out reported instead of dropped.
func (d *boundedDigest) Render(_ context.Context, since meditation.Watermark) (meditation.DigestSummary, error) {
	body := make([]string, 0, d.limit)
	omitted := 0
	for _, fact := range d.facts {
		if fact.at.Before(since.Moment) {
			continue
		}
		if len(body) == d.limit {
			omitted++
			continue
		}
		body = append(body, fact.line)
	}
	return meditation.DigestSummary{Body: strings.Join(body, "\n"), Omitted: omitted}, nil
}

var _ meditation.Digest = (*boundedDigest)(nil)

// ExampleDigest renders the summary of a face that holds more facts than the bound
// renders, so the count left out travels with the body.
//
// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
func ExampleDigest() {
	ctx := context.Background()
	since := meditation.Watermark{Moment: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	digest := &boundedDigest{
		limit: 1,
		facts: []observedFact{
			{at: since.Moment.Add(time.Minute), line: "the team shipped a fix"},
			{at: since.Moment.Add(2 * time.Minute), line: "the team filed a report"},
		},
	}
	summary, err := digest.Render(ctx, since)
	if err != nil {
		return
	}
	if summary.Omitted != 1 {
		return
	}
}
