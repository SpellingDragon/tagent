// 契约: docs/wiki/memory/memory-architecture.md#read-paths
//
// The retrieval over a real store: both legs of one query read through the
// QueryEvents face of the observation table, the keyword leg matches the query
// text and the vector leg recalls the text an embedding places near it, and the
// fusion port merges them into the one ordered reference set the page is cut
// from. A hit stays a reference and the text behind it stays in the original
// store, read byte for byte through the scoped path.
package search

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// ErrCursorUnbound reports a continuation cursor that does not bind to the
// query carrying it: a malformed token, or a token minted for another
// retrieval text or another filter set. A cursor is bound to its query and is
// refused by name when it arrives under different terms.
var ErrCursorUnbound = errors.New("search: continuation cursor does not bind to this query")

// ReferenceSource is the query face a retrieval reads through: the event
// references a QueryOptions matches, and the recall reference of any event key
// the observation table holds. Both legs of a query read through it, so the
// consumption order of the read face holds for the vector leg as well.
//
// The redline list (3 rules):
// MUST return the matching set filtered, totally ordered by (Timestamp, Key), deduplicated by event key and paged by Offset and Limit.
// MUST return identical sequences for two identical queries over identical state.
// MUST NOT read anything of the query implementation beyond these two reads.
type ReferenceSource interface {
	// QueryEvents returns the references matching opts: the matching set
	// filtered, totally ordered by (Timestamp, Key) under the named direction,
	// deduplicated by event key and then paged by Offset and Limit. Two
	// identical queries over identical state return identical sequences,
	// whatever the order the events were recorded in.
	QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)

	// Reference returns the recall reference the event key was landed with,
	// and reports whether the table holds one.
	Reference(key event.EventID) (ledger.OriginalRef, bool)
}

// VectorSource is the embedding seam a retrieval borrows its vectors from: the
// vector of one event key and whether its generation completed.
//
// The redline list (2 rules):
// MUST return the vector of the event key and whether its generation completed.
// MUST NOT infer a ready vector from a generation that has not completed.
type VectorSource interface {
	// Vector returns the embedding vector of the event key and whether it is
	// ready.
	Vector(key event.EventID) ([]float32, bool)
}

// cursorToken is the decoded form of one continuation cursor: the result
// position the next page resumes at and the query binding the token was minted
// under.
type cursorToken struct {
	Offset int    `json:"offset"`
	Sig    string `json:"sig"`
}

// StoreSearcher is the Searcher over a real store: the query source both legs
// read through, the vector seam the embedding leg borrows, and the embedder
// bound for the retrieval text. A searcher constructed without a vector seam or
// without an embedder answers through the keyword leg alone, which is the
// byte-identical fallback the hybrid contract states.
type StoreSearcher struct {
	source   ReferenceSource
	vectors  VectorSource
	embedder memory.Embedder
	fusion   Fusion
}

// NewStoreSearcher returns a searcher over source. vectors and embedder may
// name nothing, and then the vector leg is empty for every query; the fusion
// behind the searcher is the reciprocal rank fusion of this package.
func NewStoreSearcher(source ReferenceSource, vectors VectorSource, embedder memory.Embedder) *StoreSearcher {
	return &StoreSearcher{source: source, vectors: vectors, embedder: embedder, fusion: NewRRFFusion()}
}

var _ Searcher = (*StoreSearcher)(nil)

// Search returns the reference set of q, ranked by the retrieval rule the
// query runs under and cut to the page the query asks for.
func (s *StoreSearcher) Search(ctx context.Context, q Query) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := q.Validate(); err != nil {
		return Result{}, err
	}
	strategy := q.Strategy
	if strategy == "" {
		strategy = StrategyHybrid
	}
	offset := 0
	if q.Cursor != "" {
		resumed, err := s.decodeCursor(q)
		if err != nil {
			return Result{}, err
		}
		offset = resumed
	}
	partitions := partitionIDsOf(q.Partitions)
	keyword, err := s.keywordLeg(ctx, q, partitions)
	if err != nil {
		return Result{}, err
	}
	vector, err := s.vectorLeg(ctx, q, partitions, strategy)
	if err != nil {
		return Result{}, err
	}
	fused, err := s.fusion.Fuse(ctx, strategy, vector, keyword)
	if err != nil {
		return Result{}, err
	}
	return s.page(fused.Refs, offset, q), nil
}

// keywordLeg returns the keyword leg of q: the references whose event matches
// the query text on the query semantics of the read face, inside the partition
// set this search is authorized for. The page bound is left to the port, so the
// leg carries the whole matching set and the page is cut after the fusion.
func (s *StoreSearcher) keywordLeg(ctx context.Context, q Query, partitions []int) ([]ledger.OriginalRef, error) {
	opts := q.QueryOptions()
	opts.PartitionIDs = partitions
	opts.Limit = 0
	refs, err := s.source.QueryEvents(ctx, opts)
	if err != nil {
		return nil, err
	}
	return s.referencesOf(refs), nil
}

// vectorLeg returns the vector leg of q: the references of the authorized
// partition set ranked by the cosine of their embedding against the embedding
// of the query text. A searcher with no seam or no embedder, and an embedder
// that fails or returns no vector, all answer an empty leg, which is the
// keyword-only fallback rather than a failed retrieval.
func (s *StoreSearcher) vectorLeg(ctx context.Context, q Query, partitions []int, strategy Strategy) ([]ledger.OriginalRef, error) {
	if strategy == StrategyKeywordOnly || s.vectors == nil || s.embedder == nil {
		return nil, nil
	}
	queryVector, err := s.embedder.Embed(ctx, q.Text)
	if err != nil || len(queryVector) == 0 {
		return nil, nil
	}
	opts := q.QueryOptions()
	opts.PartitionIDs = partitions
	opts.Limit = 0
	opts.Keyword = ""
	refs, err := s.source.QueryEvents(ctx, opts)
	if err != nil {
		return nil, err
	}
	type scoredRef struct {
		ref   ledger.OriginalRef
		score float64
		key   event.EventID
	}
	var hits []scoredRef
	for _, ref := range refs {
		vector, ready := s.vectors.Vector(ref.Key)
		if !ready {
			continue
		}
		score := cosine(queryVector, vector)
		if score <= 0 {
			continue
		}
		original, held := s.source.Reference(ref.Key)
		if !held {
			continue
		}
		hits = append(hits, scoredRef{ref: original, score: score, key: ref.Key})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].key > hits[j].key
	})
	refs = nil
	out := make([]ledger.OriginalRef, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.ref)
	}
	return out, nil
}

// referencesOf resolves the recall reference of each event reference, dropping
// the hits the observation table holds no reference for.
func (s *StoreSearcher) referencesOf(refs []memory.EventReference) []ledger.OriginalRef {
	out := make([]ledger.OriginalRef, 0, len(refs))
	for _, ref := range refs {
		if original, held := s.source.Reference(ref.Key); held {
			out = append(out, original)
		}
	}
	return out
}

// page cuts one page out of the fused set: the page bound of the query, the
// result position a continuation cursor names, and the continuation token for
// the page after this one. The zero NextCursor means this is the last page, so
// a cursor is handed out only while references remain behind the page.
func (s *StoreSearcher) page(refs []ledger.OriginalRef, offset int, q Query) Result {
	limit := q.PageLimit()
	if offset >= len(refs) {
		return Result{Refs: []ledger.OriginalRef{}}
	}
	end := offset + limit
	next := Cursor("")
	if end < len(refs) {
		next = encodeCursor(cursorToken{Offset: end, Sig: s.signature(q)})
	} else {
		end = len(refs)
	}
	page := make([]ledger.OriginalRef, end-offset)
	copy(page, refs[offset:end])
	return Result{Refs: page, NextCursor: next}
}

// signature binds a continuation cursor to the query that minted it: the
// retrieval text, the strategy, the partition and type filters and the time
// window, so a cursor can only resume the retrieval it belongs to.
func (s *StoreSearcher) signature(q Query) string {
	partitions := append([]string(nil), q.Partitions...)
	sort.Strings(partitions)
	types := append([]string(nil), q.Types...)
	sort.Strings(types)
	parts := []string{
		q.Text,
		string(q.Strategy),
		strings.Join(partitions, ","),
		strings.Join(types, ","),
		strconv.FormatInt(q.Since.UnixMilli(), 10),
		strconv.FormatInt(q.Until.UnixMilli(), 10),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// decodeCursor decodes the continuation cursor of q and returns the result
// position it resumes at. A token that does not decode, carries a negative
// position or does not bind to this query is refused by name with
// ErrCursorUnbound.
func (s *StoreSearcher) decodeCursor(q Query) (int, error) {
	payload, err := base64.RawURLEncoding.DecodeString(string(q.Cursor))
	if err != nil {
		return 0, ErrCursorUnbound
	}
	var token cursorToken
	if err := json.Unmarshal(payload, &token); err != nil {
		return 0, ErrCursorUnbound
	}
	if token.Offset < 0 || token.Sig != s.signature(q) {
		return 0, ErrCursorUnbound
	}
	return token.Offset, nil
}

// encodeCursor encodes one continuation token into the opaque cursor a page
// hands out.
func encodeCursor(token cursorToken) Cursor {
	payload, err := json.Marshal(token)
	if err != nil {
		return ""
	}
	return Cursor(base64.RawURLEncoding.EncodeToString(payload))
}

// partitionIDsOf derives the partition IDs of the named partitions: the
// authorized set a query may read. An empty name set derives an empty ID set,
// which the read face scans as nothing.
func partitionIDsOf(names []string) []int {
	ids := make([]int, 0, len(names))
	for _, name := range names {
		ids = append(ids, memory.PartitionIDFromName(name))
	}
	return ids
}

// cosine returns the cosine similarity of two vectors, or zero for vectors of
// different widths or with no magnitude, so a malformed vector contributes
// nothing rather than a spurious score.
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	dot, normA, normB := float64(0), float64(0), float64(0)
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
