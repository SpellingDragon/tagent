// 契约: docs/wiki/memory/memory-architecture.md#read-paths
//
// Package search is the retrieval port of the memory side: it names the
// authorized scope a recall query may read in, returns the reference set a
// query hit, and fuses the vector leg and the keyword leg into that one set.
//
// The port speaks references, not facts. The index behind it is a derived,
// replaceable structure, so a hit names the stored original and the text stays
// behind the original store: the index is never the source of truth, and a hit
// is never served as a second copy of the text. This is the phase 1 skeleton:
// it declares the port and holds no behaviour.
package search

import (
	"context"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// ErrQueryTextMissing reports a retrieval request that carries no retrieval
// content: a retrieval runs for the text it names, so a query without text is
// refused by name rather than answered as a listing of everything its filters
// admit. Listing is not a degraded retrieval form; a caller that wants a
// listing names it on a path made for listing.
var ErrQueryTextMissing = errors.New("search: the query carries no retrieval text")

// DefaultLimit is the page bound a query with a non-positive Limit runs under.
// A zero Limit is not "no bound": it falls back to this bound, so every page is
// finite and an oversized match set is consumed page by page through the
// continuation cursor rather than returned whole. Neither the recall protocol
// nor the evaluation report names a number (the protocol defers to the engine
// clamp), so 20 stands as the small conservative default declared here; it is
// aligned with that clamp when one exists and never silently raised.
const DefaultLimit = 20

// Cursor is the continuation token of a paginated retrieval: the opaque mark a
// page hands out for the page after it.
//
// A cursor is not a key and carries no authority. It is bound to the query that
// produced it — the same retrieval text and the same filters — and callers
// treat it as opaque: they do not construct one, and the only source of one is
// the NextCursor of the previous page's Result. The zero cursor asks for the
// first page, and it is never read as an unknown page.
type Cursor string

// Searcher is the authorized-scope query port: it answers one recall query
// inside the scope the caller is authorized to read and returns the references
// the query hit.
//
// The redline list (4 rules):
// MUST answer inside the authorized scope the caller holds.
// MUST treat an empty authorized set as authorizing no partition.
// MUST NOT widen to the whole store when the authorized set is empty.
// MUST NOT answer an unauthorized read as a hit.
type Searcher interface {
	// Search returns the reference set of q, ranked by the retrieval rule the
	// query runs under and bounded by the page the query asks for.
	Search(ctx context.Context, q Query) (Result, error)
}

// Query is one retrieval request: the authorized read scope it may run in and
// the time, type and relation filters that narrow that scope.
//
// A filter only narrows what the scope already admits and never widens it, and
// the scope itself is default-nothing rather than a fallback to the whole
// store.
type Query struct {
	// Text is the retrieval content of the query: the question whose answers
	// the retrieval runs for. Two queries naming different Text are different
	// retrievals even under identical filters, so the content is carried
	// explicitly rather than inferred from the scope the query runs in. An
	// empty Text is not a listing request: it is a retrieval without content
	// and is refused by name with ErrQueryTextMissing (see Validate), the same
	// way an empty Partitions set authorizes nothing instead of everything.
	Text string
	// Strategy names the retrieval rule the query runs under, one of the
	// strategy constants of this package: the keyword leg alone, the vector
	// leg alone, or the fusion of both. The zero Strategy names no rule and
	// leaves the choice to the port the query runs on.
	Strategy Strategy
	// Cursor is the continuation token of the page this query asks for: the
	// opaque mark the previous page handed out. The zero cursor asks for the
	// first page, and a cursor is only ever carried forward from a Result, never
	// constructed or guessed.
	Cursor Cursor
	// Partitions is the authorized partition set this query may read. An empty
	// set authorizes nothing and is never read as every partition.
	Partitions []string
	// Types restricts the query to the given frozen event types; it is empty
	// when no type restriction applies.
	Types []string
	// Since and Until bound the query to the events recorded inside that
	// window. A zero Since leaves the lower bound open, and a zero Until the
	// upper one.
	Since time.Time
	Until time.Time
	// Relations restricts the query to the events standing in a causal relation
	// to the given keys; it is empty when no relation restriction applies.
	//
	// Unimplemented by adjudication: no consumer of relation-filtered retrieval
	// exists — causal-chain consumption walks the
	// RelationStore explicitly instead, and content stays separate from
	// relations by design. The field is kept so the declared query shape does
	// not change underneath its callers, and no relation filter is ever read:
	// zero implementation and zero tests stand behind it until a real consumer
	// names the need.
	Relations []event.EventID
	// Limit caps how many references one page carries, so an oversized match
	// set is consumed page by page rather than whole. A non-positive Limit is
	// not "no bound": it runs under DefaultLimit (see PageLimit), so every page
	// stays finite.
	Limit int
}

// Validate returns the named refusal that rejects this query before it runs,
// or nil when the query may run. The only refusal today is a query whose Text
// is empty: retrieval runs for content, and an empty Text is refused by name
// rather than degraded into a filter-only listing.
func (q Query) Validate() error {
	if q.Text == "" {
		return ErrQueryTextMissing
	}
	return nil
}

// PageLimit returns the page bound this query runs under: the Limit it carries
// when that is positive, and DefaultLimit otherwise, so a zero Limit never
// resolves to an unbounded page.
func (q Query) PageLimit() int {
	if q.Limit > 0 {
		return q.Limit
	}
	return DefaultLimit
}

// QueryOptions returns the QueryEvents filter this retrieval maps onto: the one query semantics of
// the memory read face, never a second one beside it.
//
// - The mapping is thin on purpose: the time axis is carried across as Unix milliseconds (Since and Until become the inclusive window, a zero bound staying open) and Types as the event-type restriction unchanged.
// - Text, the retrieval content of the query, is carried as the keyword leg, which is what a free-text query is served by; the retrieval layer may grow a vector leg behind the same mapping later.
// - The page is carried as the Limit of the query window, and the order is fixed to newest-first because recall serves the time arrow the compaction keeps: the newer event is recalled before the older one.
// - The partition scope is deliberately not carried: an authorized read receives its partition set injected into the mapped options by the read seam, so this method never widens a query to partitions its caller is not authorized for.
func (q Query) QueryOptions() memory.QueryOptions {
	opts := memory.QueryOptions{
		EventTypes: append([]string(nil), q.Types...),
		Keyword:    q.Text,
		Limit:      q.PageLimit(),
		OrderBy:    memory.QueryOrderByDesc,
	}
	if !q.Since.IsZero() {
		opts.StartTime = q.Since.UnixMilli()
	}
	if !q.Until.IsZero() {
		opts.EndTime = q.Until.UnixMilli()
	}
	return opts
}

// Result is the reference set one retrieval returned: the references the query
// hit, ordered from the best hit down.
//
// It carries references, not text. The index that produced it is not the source
// of truth, so the caller reads the text behind a hit through the original path
// and a hit is never served as a second copy of it. An empty set is a legal
// answer: both a miss and a scope that authorizes nothing produce one.
type Result struct {
	// Refs is the reference set of the result, ordered from the best hit to the
	// worst.
	Refs []memory.OriginalRef
	// NextCursor is the continuation token for the page after this result: the
	// zero cursor means this is the last page and no page follows, never an
	// unknown one. A cursor is opaque and is only carried back unchanged in the
	// Cursor of the next query, under the same retrieval text and filters; it
	// carries no authority of its own, so the scope of the query carrying it
	// still governs what the next page may read.
	NextCursor Cursor
}
