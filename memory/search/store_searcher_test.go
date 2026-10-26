// 契约: docs/wiki/memory/memory-architecture.md#read-paths
package search

import (
	"context"
	"fmt"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// tuningSource is a query source over a fixed reference list: it honours the
// page bound of the options and records the bound each leg asked for.
type tuningSource struct {
	refs     []memory.EventReference
	keyword  *int
	partless *int
}

func (s *tuningSource) QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	limit := opts.Limit
	if opts.Keyword == "" {
		if s.partless != nil {
			*s.partless = limit
		}
	} else if s.keyword != nil {
		*s.keyword = limit
	}
	out := s.refs
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return append([]memory.EventReference(nil), out...), nil
}

func (s *tuningSource) Reference(key event.EventID) (ledger.OriginalRef, bool) {
	for _, ref := range s.refs {
		if ref.Key == key {
			return ledger.OriginalRef{Key: key}, true
		}
	}
	return ledger.OriginalRef{}, false
}

// tuningVectors answers one ready vector for every key it names.
type tuningVectors map[event.EventID][]float32

func (v tuningVectors) Vector(key event.EventID) ([]float32, bool) {
	vec, held := v[key]
	return vec, held
}

// tuningEmbedder answers one fixed query vector.
type tuningEmbedder struct{ vec []float32 }

func (e tuningEmbedder) Embed(context.Context, string) ([]float32, error) { return e.vec, nil }

func tuningRefs(n int) []memory.EventReference {
	refs := make([]memory.EventReference, 0, n)
	for i := 0; i < n; i++ {
		key := event.EventID(string(rune('a' + i)))
		refs = append(refs, memory.EventReference{Key: key, PartitionID: 1, Timestamp: int64(i + 1)})
	}
	return refs
}

// TestStoreSearcherHonoursRetrievalTuning pins the two leg bounds and the rank constant a deployment declares.
//   - the bounds cap each leg before fusion
//   - a zero tuning leaves both legs unbounded, which is the standing behaviour
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestStoreSearcherHonoursRetrievalTuning(t *testing.T) {
	ctx := context.Background()
	var keywordLimit, vectorLimit int
	source := &tuningSource{refs: tuningRefs(6), keyword: &keywordLimit, partless: &vectorLimit}
	vectors := tuningVectors{}
	for _, ref := range source.refs {
		vectors[ref.Key] = []float32{1, 0}
	}
	searcher := NewStoreSearcher(source, vectors, tuningEmbedder{vec: []float32{1, 0}})
	query := Query{Text: "x", Partitions: []string{"p"}, Limit: 10}

	if _, err := searcher.Search(ctx, query); err != nil {
		t.Fatalf("search under the zero tuning: %v", err)
	}
	if keywordLimit != 0 || vectorLimit != 0 {
		t.Fatalf("zero tuning bounded the legs (keyword=%d vector=%d), want unbounded legs", keywordLimit, vectorLimit)
	}

	if err := searcher.SetRetrievalTuning(3, 2, 7); err != nil {
		t.Fatalf("apply the retrieval tuning: %v", err)
	}
	result, err := searcher.Search(ctx, query)
	if err != nil {
		t.Fatalf("search under the declared tuning: %v", err)
	}
	if keywordLimit != 2 {
		t.Fatalf("keyword leg ran under limit %d, want the declared 2", keywordLimit)
	}
	if vectorLimit != 3 {
		t.Fatalf("vector leg ran under limit %d, want the declared 3", vectorLimit)
	}
	if len(result.Refs) != 3 {
		t.Fatalf("tuned search answered %d references, want the page bound 3", len(result.Refs))
	}

	if err := searcher.SetRetrievalTuning(-1, 0, 0); err == nil {
		t.Fatal("negative tuning was accepted")
	}
}

// TestReciprocalRankUsesTheDeclaredConstant pins the rank constant as an argument of the fusion rule.
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestReciprocalRankUsesTheDeclaredConstant(t *testing.T) {
	vector := make([]ledger.OriginalRef, 0, 10)
	for i := 0; i < 10; i++ {
		vector = append(vector, ledger.OriginalRef{Key: event.EventID(fmt.Sprintf("v%d", i))})
	}
	both := ledger.OriginalRef{Key: "both"}
	vector[9] = both
	keyword := []ledger.OriginalRef{{Key: "top"}}
	for i := 0; i < 8; i++ {
		keyword = append(keyword, ledger.OriginalRef{Key: event.EventID(fmt.Sprintf("k%d", i))})
	}
	keyword = append(keyword, both)

	canonical := reciprocalRank(vector, keyword, RRFRankConstant)
	small := reciprocalRank(vector, keyword, 1)
	top := ledger.OriginalRef{Key: "top"}
	if rankOf(canonical.Refs, both) >= rankOf(canonical.Refs, top) {
		t.Fatalf("canonical fusion ranked %q at %d and %q at %d, want the reference both legs carry ahead",
			both.Key, rankOf(canonical.Refs, both), top.Key, rankOf(canonical.Refs, top))
	}
	if rankOf(small.Refs, both) <= rankOf(small.Refs, top) {
		t.Fatalf("rank constant 1 ranked %q at %d and %q at %d, want the keyword leg's top ahead",
			both.Key, rankOf(small.Refs, both), top.Key, rankOf(small.Refs, top))
	}
}

// rankOf reports where one reference sits in a fused set, or the length of the
// set when it is absent.
func rankOf(refs []ledger.OriginalRef, ref ledger.OriginalRef) int {
	for i, candidate := range refs {
		if candidate == ref {
			return i
		}
	}
	return len(refs)
}
