package rlexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/rl"
)

// offsetSource is the canonical read seam: offset-paged reads of one partition and the
// episode records the leakage-free split groups are drawn on.
type offsetSource struct {
	facts    []rl.ExportedFact
	episodes []rl.EpisodeRecord
}

// Page returns at most limit facts of one partition at the given offset, in a stable order.
func (s *offsetSource) Page(_ context.Context, partition string, offset, limit int) ([]rl.ExportedFact, error) {
	paged := make([]rl.ExportedFact, 0, len(s.facts))
	for _, fact := range s.facts {
		if fact.Partition != partition {
			continue
		}
		paged = append(paged, fact)
	}
	if offset >= len(paged) {
		return nil, nil
	}
	paged = paged[offset:]
	if limit > 0 && limit < len(paged) {
		paged = paged[:limit]
	}
	return paged, nil
}

// Episodes returns the episode records the split groups are drawn on.
func (s *offsetSource) Episodes(context.Context) ([]rl.EpisodeRecord, error) {
	return append([]rl.EpisodeRecord(nil), s.episodes...), nil
}

var _ rl.ExportSource = (*offsetSource)(nil)

// ExampleExportSource reads one page of a partition, reports the offset past the end as an
// empty page and returns the episodes the split groups are drawn on.
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func ExampleExportSource() {
	ctx := context.Background()
	source := &offsetSource{
		facts: []rl.ExportedFact{
			{EventKey: "event-1", Partition: "partition-1"},
			{EventKey: "event-2", Partition: "partition-1"},
		},
		episodes: []rl.EpisodeRecord{{Schema: rl.SchemaCurrent, ID: event.EpisodeID("episode-1"), Lineage: rl.LineageKnown}},
	}

	page, err := source.Page(ctx, "partition-1", 0, 1)
	if err != nil || len(page) != 1 || page[0].EventKey != "event-1" {
		return
	}
	empty, err := source.Page(ctx, "partition-1", 2, 1)
	if err != nil || len(empty) != 0 {
		return
	}
	episodes, err := source.Episodes(ctx)
	if err != nil || len(episodes) != 1 {
		return
	}
}
