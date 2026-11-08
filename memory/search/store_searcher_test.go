// 契约: docs/wiki/memory/memory-architecture.md#read-paths
package search

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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
	texts    map[event.EventID]string
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

// Text returns the original text of one candidate.
func (s *tuningSource) Text(key event.EventID) (string, bool) {
	text, held := s.texts[key]
	return text, held
}

// tuningEmbedder answers the query vector and each candidate's distinct vector.
type tuningEmbedder struct {
	vec    []float32
	byText map[string][]float32
}

// Embed returns the candidate-specific vector when one is registered.
func (e tuningEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if e.byText != nil {
		if vector, held := e.byText[text]; held {
			return vector, nil
		}
		return nil, nil
	}
	return e.vec, nil
}

// tuningEmbedderFor builds the candidate vectors from the original text.
func tuningEmbedderFor(source *tuningSource) tuningEmbedder {
	embedder := tuningEmbedder{vec: []float32{1, 0}, byText: map[string][]float32{}}
	source.texts = map[event.EventID]string{}
	for i, ref := range source.refs {
		text := fmt.Sprintf("text-%d", i)
		source.texts[ref.Key] = text
		embedder.byText[text] = []float32{1, float32(i) * 0.1}
	}
	embedder.byText["x"] = embedder.vec
	return embedder
}

func tuningRefs(n int) []memory.EventReference {
	refs := make([]memory.EventReference, 0, n)
	for i := 0; i < n; i++ {
		key := event.EventID(string(rune('a' + i)))
		refs = append(refs, memory.EventReference{Key: key, PartitionID: 1, Timestamp: int64(i + 1)})
	}
	return refs
}

// TestRetrievalTuningDerived pins the retrieval rules as the mechanism's own.
//   - the constructor derives both leg bounds from DefaultLimit
//   - the fusion runs under the canonical RRFRankConstant
//   - the searcher carries no independent write of either bound
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestRetrievalTuningDerived(t *testing.T) {
	ctx := context.Background()
	var keywordLimit, vectorLimit int
	source := &tuningSource{refs: tuningRefs(25), keyword: &keywordLimit, partless: &vectorLimit}
	searcher := NewStoreSearcher(source, tuningEmbedderFor(source))

	if searcher.keywordTopK != DefaultLimit || searcher.vectorTopK != DefaultLimit {
		t.Fatalf("leg bounds = keyword %d vector %d, want the mechanism bound %d on both",
			searcher.keywordTopK, searcher.vectorTopK, DefaultLimit)
	}
	if _, held := reflect.TypeOf(searcher).MethodByName("SetRetrievalTuning"); held {
		t.Fatal("the searcher carries an independent write of the retrieval bounds, want them derived from the mechanism constants")
	}
	rrf, fusionHeld := searcher.fusion.(RRFFusion)
	if !fusionHeld || rrf.K != RRFRankConstant {
		t.Fatalf("fusion = %#v, want the reciprocal rank fusion under the canonical constant %d", searcher.fusion, RRFRankConstant)
	}

	if _, err := searcher.Search(ctx, Query{Text: "x", Partitions: []string{"p"}, Limit: 5}); err != nil {
		t.Fatalf("search under the derived bounds: %v", err)
	}
	if keywordLimit != DefaultLimit {
		t.Fatalf("keyword leg read under limit %d, want the mechanism bound %d", keywordLimit, DefaultLimit)
	}
	if vectorLimit != 0 {
		t.Fatalf("vector leg pre-truncated the candidates at %d, want the whole candidate set read and cut after the scoring", vectorLimit)
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

// Text returns the original text of one candidate.
func (s *completenessSource) Text(key event.EventID) (string, bool) {
	text, held := s.texts[key]
	return text, held
}

// TestVectorCandidateCompleteness pins whole-candidate scoring: the mechanism bound cuts the best hits after the scoring.
//   - the vector leg scores every candidate, so the old relevant record a synonym query shares no word with is recalled
//   - a query with no retrieval text keeps its named refusal and the keyword leg reads under the mechanism bound
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
	vectors := tuningEmbedder{vec: []float32{1, 0}, byText: map[string][]float32{
		"上线出问题撤回了": {1, 0},
		"部署失败回滚":   {1, 0},
		"不相关的新记录":  {0, 1},
	}}
	query := Query{Text: "上线出问题撤回了", Partitions: []string{"p"}, Limit: 10}

	t.Run("the whole candidate set is scored, so the old relevant record is recalled", func(t *testing.T) {
		searcher := NewStoreSearcher(source, vectors)
		result, err := searcher.Search(ctx, query)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if !carriesKey(result.Refs, "old-relevant") {
			t.Fatalf("scored candidates = %v, want the old relevant record recalled", keysOf(result.Refs))
		}
	})

	t.Run("the named refusal and the mechanism bound stand", func(t *testing.T) {
		searcher := NewStoreSearcher(source, vectors)
		if _, err := searcher.Search(ctx, Query{Partitions: []string{"p"}}); !errors.Is(err, ErrQueryTextMissing) {
			t.Fatalf("query without text answered %v, want the named refusal %v", err, ErrQueryTextMissing)
		}
		result, err := searcher.Search(ctx, query)
		if err != nil {
			t.Fatalf("search under the mechanism bound: %v", err)
		}
		if !carriesKey(result.Refs, "old-relevant") {
			t.Fatalf("search answered %v, want the old record recalled under the mechanism bound", keysOf(result.Refs))
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
