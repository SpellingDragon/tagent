// 契约: docs/wiki/memory/memory-architecture.md#read-paths
package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
//   - the bounds cap each leg's contribution to the fusion: the keyword bound at the read, the vector bound after the scoring over the whole candidate set
//   - a zero tuning leaves both legs unbounded, which is the standing behaviour
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestStoreSearcherHonoursRetrievalTuning(t *testing.T) {
	ctx := context.Background()
	var keywordLimit, vectorLimit int
	source := &tuningSource{refs: tuningRefs(6), keyword: &keywordLimit, partless: &vectorLimit}
	vectors := tuningVectors{}
	for i, ref := range source.refs {
		vectors[ref.Key] = []float32{1, float32(i) * 0.1}
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
	if vectorLimit != 0 {
		t.Fatalf("vector leg pre-truncated the candidates at %d, want the whole candidate set read and cut after the scoring", vectorLimit)
	}
	if len(result.Refs) != 3 {
		t.Fatalf("tuned search answered %d references, want the 3 best-scoring vector hits beside the keyword bound", len(result.Refs))
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

type completenessSource struct {
	refs  []memory.EventReference
	texts map[event.EventID]string
	limit int
}

// QueryEvents answers the candidates in the newest-first order the recall read
// face runs under, narrowing to the keyword leg's text matches and recording
// the page bound the read ran with.
func (s *completenessSource) QueryEvents(_ context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	s.limit = opts.Limit
	out := make([]memory.EventReference, 0, len(s.refs))
	for _, ref := range s.refs {
		if opts.Keyword != "" && !strings.Contains(s.texts[ref.Key], opts.Keyword) {
			continue
		}
		out = append(out, ref)
	}
	if opts.Limit > 0 && opts.Limit < len(out) {
		out = out[:opts.Limit]
	}
	return out, nil
}

func (s *completenessSource) Reference(key event.EventID) (ledger.OriginalRef, bool) {
	for _, ref := range s.refs {
		if ref.Key == key {
			return ledger.OriginalRef{Key: key}, true
		}
	}
	return ledger.OriginalRef{}, false
}

// TestVectorCandidateCompleteness pins whole-candidate scoring: the declared bound cuts the best hits after the scoring.
//   - an unbounded vector leg recalls the old relevant record a synonym query shares no word with
//   - a bound of one still recalls that record, which the newest candidates carry no vector for
//   - a query with no retrieval text, a negative tuning and a zero bound keep their named refusals and standing behaviour
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestVectorCandidateCompleteness(t *testing.T) {
	ctx := context.Background()
	source := &completenessSource{
		refs: []memory.EventReference{
			{Key: "new-no-vector", PartitionID: 1, Timestamp: 3},
			{Key: "new-unrelated", PartitionID: 1, Timestamp: 2},
			{Key: "old-relevant", PartitionID: 1, Timestamp: 1},
		},
		texts: map[event.EventID]string{
			"new-no-vector": "另一条没有向量的新记录",
			"new-unrelated": "不相关的新记录",
			"old-relevant":  "部署失败回滚",
		},
	}
	vectors := tuningVectors{
		"old-relevant":  {1, 0},
		"new-unrelated": {0, 1},
	}
	query := Query{Text: "上线出问题撤回了", Partitions: []string{"p"}, Limit: 10}

	t.Run("unbounded candidates recall the old relevant record", func(t *testing.T) {
		searcher := NewStoreSearcher(source, vectors, tuningEmbedder{vec: []float32{1, 0}})
		result, err := searcher.Search(ctx, query)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if !carriesKey(result.Refs, "old-relevant") {
			t.Fatalf("unbounded candidates = %v, want the old relevant record recalled", keysOf(result.Refs))
		}
	})

	t.Run("bound one still recalls the old relevant record", func(t *testing.T) {
		searcher := NewStoreSearcher(source, vectors, tuningEmbedder{vec: []float32{1, 0}})
		if err := searcher.SetRetrievalTuning(1, 0, 0); err != nil {
			t.Fatalf("apply the retrieval tuning: %v", err)
		}
		result, err := searcher.Search(ctx, query)
		if err != nil {
			t.Fatalf("search under the bound of one: %v", err)
		}
		if !carriesKey(result.Refs, "old-relevant") {
			t.Fatalf("vector_top_k=1 answered %v, want the old relevant record among the scored candidates", keysOf(result.Refs))
		}
		if len(result.Refs) != 1 {
			t.Fatalf("vector_top_k=1 answered %d references, want the one best-scoring candidate", len(result.Refs))
		}
	})

	t.Run("the named refusals and the zero bound stand", func(t *testing.T) {
		searcher := NewStoreSearcher(source, vectors, tuningEmbedder{vec: []float32{1, 0}})
		if _, err := searcher.Search(ctx, Query{Partitions: []string{"p"}}); !errors.Is(err, ErrQueryTextMissing) {
			t.Fatalf("query without text answered %v, want the named refusal %v", err, ErrQueryTextMissing)
		}
		if err := searcher.SetRetrievalTuning(-1, 0, 0); !errors.Is(err, ErrRetrievalTuning) {
			t.Fatalf("negative tuning answered %v, want the named refusal %v", err, ErrRetrievalTuning)
		}
		if err := searcher.SetRetrievalTuning(0, 0, 0); err != nil {
			t.Fatalf("zero tuning: %v", err)
		}
		result, err := searcher.Search(ctx, query)
		if err != nil {
			t.Fatalf("search under the zero tuning: %v", err)
		}
		if !carriesKey(result.Refs, "old-relevant") || source.limit != 0 {
			t.Fatalf("zero bound answered %v under read bound %d, want the leg unbounded and the old record recalled", keysOf(result.Refs), source.limit)
		}
	})
}

// carriesKey reports whether the reference set holds one event key.
func carriesKey(refs []ledger.OriginalRef, key event.EventID) bool {
	for _, ref := range refs {
		if ref.Key == key {
			return true
		}
	}
	return false
}

// keysOf names the event keys of one reference set, for a failure message.
func keysOf(refs []ledger.OriginalRef) []event.EventID {
	out := make([]event.EventID, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Key)
	}
	return out
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
