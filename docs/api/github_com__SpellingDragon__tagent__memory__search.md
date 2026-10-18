package search // import "github.com/SpellingDragon/tagent/memory/search"

Package search is the retrieval port of the memory side: it names the authorized
scope a recall query may read in, returns the reference set a query hit,
and fuses the vector leg and the keyword leg into that one set.

The port speaks references, not facts. The index behind it is a derived,
replaceable structure, so a hit names the stored original and the text stays
behind the original store: the index is never the source of truth, and a hit
is never served as a second copy of the text. This is the phase 1 skeleton:
it declares the port and holds no behaviour.

Skeleton compile-time assertions for the phase 1 retrieval port. The phase 2
types that satisfy the ports do not exist yet, so there is no implementation
assertion to write; the assertions below pin what the skeleton proves on its
own: every declared field name and type, every declared method signature,
and every value that must stay a compile-time constant.

The retrieval over a real store: both legs of one query read through the
QueryEvents face of the observation table, the keyword leg matches the query
text and the vector leg recalls the text an embedding places near it, and the
fusion port merges them into the one ordered reference set the page is cut from.
A hit stays a reference and the text behind it stays in the original store,
read byte for byte through the scoped path.

CONSTANTS

const DefaultLimit = 20
    DefaultLimit is the page bound a query with a non-positive Limit runs under.
    A zero Limit is not "no bound": it falls back to this bound, so every page
    is finite and an oversized match set is consumed page by page through the
    continuation cursor rather than returned whole. Neither the recall protocol
    nor the evaluation report names a number (the protocol defers to the engine
    clamp), so 20 stands as the small conservative default declared here;
    it is aligned with that clamp when one exists and never silently raised.

const RRFRankConstant = 60
    RRFRankConstant is the rank constant of the reciprocal rank rule, the
    canonical value the hybrid strategy uses.

VARIABLES

var ErrCursorUnbound = errors.New("search: continuation cursor does not bind to this query")
    ErrCursorUnbound reports a continuation cursor that does not bind to
    the query carrying it: a malformed token, or a token minted for another
    retrieval text or another filter set. A cursor is bound to its query and is
    refused by name when it arrives under different terms.

var ErrQueryTextMissing = errors.New("search: the query carries no retrieval text")
    ErrQueryTextMissing reports a retrieval request that carries no retrieval
    content: a retrieval runs for the text it names, so a query without text is
    refused by name rather than answered as a listing of everything its filters
    admit. Listing is not a degraded retrieval form; a caller that wants a
    listing names it on a path made for listing.

var ErrStrategyUnknown = errors.New("search: the query names an unknown strategy")
    ErrStrategyUnknown reports a query naming a strategy outside the declared
    vocabulary, so no rule could decide how its legs combine.

var ErrStrategyUnwired = errors.New("search: the vector-only strategy has no consumer and is not implemented")
    ErrStrategyUnwired reports the vector-only strategy: it declares no consumer
    anywhere in the runtime and is therefore not implemented. The constant stays
    declared so the strategy vocabulary does not change underneath callers,
    and a query naming it is refused by name instead of being answered by an
    implementation nobody asked for.

TYPES

type Cursor string
    Cursor is the continuation token of a paginated retrieval: the opaque mark a
    page hands out for the page after it.

    A cursor is not a key and carries no authority. It is bound to the query
    that produced it — the same retrieval text and the same filters — and
    callers treat it as opaque: they do not construct one, and the only source
    of one is the NextCursor of the previous page's Result. The zero cursor asks
    for the first page, and it is never read as an unknown page.

type Fusion interface {
	// Fuse merges the vector leg and the keyword leg under strategy and returns
	// the ordered reference set they fuse into. Each leg is a reference list
	// ranked from the best down and an empty leg is not an error; naming the
	// legs as references keeps the port independent of any embedding framework.
	// A fused set is complete as given and hands out no continuation: its
	// NextCursor stays zero, and paging is answered by the retrieval port from
	// the legs before they fuse, not by the fusion.
	Fuse(ctx context.Context, strategy Strategy, vector, keyword []memory.OriginalRef) (Result, error)
}

    Fusion is the hybrid recall port: it merges the vector leg and the keyword
    leg one query raised into the single ordered reference set the caller
    receives.

    The redline list (3 rules): MUST merge the two legs into the single ordered
    reference set the caller receives. MUST NOT widen the caller's authorized
    scope or read a leg of its own. MUST NOT answer a fused hit with a copy of
    the text, which stays behind the original path.

func NewRRFFusion() Fusion
    NewRRFFusion returns the hybrid fusion port.

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
    Query is one retrieval request: the authorized read scope it may run in and
    the time, type and relation filters that narrow that scope.

    A filter only narrows what the scope already admits and never widens it,
    and the scope itself is default-nothing rather than a fallback to the whole
    store.

func (q Query) PageLimit() int
    PageLimit returns the page bound this query runs under: the Limit it carries
    when that is positive, and DefaultLimit otherwise, so a zero Limit never
    resolves to an unbounded page.

func (q Query) QueryOptions() memory.QueryOptions
    QueryOptions returns the QueryEvents filter this retrieval maps onto: the
    one query semantics of the memory read face, never a second one beside it.

    - The mapping is thin on purpose: the time axis is carried across as Unix
    milliseconds (Since and Until become the inclusive window, a zero bound
    staying open) and Types as the event-type restriction unchanged. - Text,
    the retrieval content of the query, is carried as the keyword leg, which is
    what a free-text query is served by; the retrieval layer may grow a vector
    leg behind the same mapping later. - The page is carried as the Limit of the
    query window, and the order is fixed to newest-first because recall serves
    the time arrow the compaction keeps: the newer event is recalled before the
    older one. - The partition scope is deliberately not carried: an authorized
    read receives its partition set injected into the mapped options by the read
    seam, so this method never widens a query to partitions its caller is not
    authorized for.

func (q Query) Validate() error
    Validate returns the named refusal that rejects this query before it runs,
    or nil when the query may run. The only refusal today is a query whose Text
    is empty: retrieval runs for content, and an empty Text is refused by name
    rather than degraded into a filter-only listing.

type RRFFusion struct{}
    RRFFusion is the Fusion implementation of this package. It carries the two
    strategies that have consumers — hybrid and keyword-only — and refuses the
    rest by name.

    - The hybrid fallback is byte-identical by contract: an empty vector leg
    (unconfigured embedder, dropped generation or no hit) runs the very same
    code path keyword-only runs, so a caller cannot tell the two apart and a
    vector pipeline coming up later never changes what a keyword-only answer
    looked like. - The fusion grants nothing: both legs arrive already read
    under the caller's authorized scope and the fused result stays inside it.
    - A fused hit stays a reference, so a fused set is never a second copy of
    the text behind it.

func (RRFFusion) Fuse(ctx context.Context, strategy Strategy, vector, keyword []memory.OriginalRef) (Result, error)
    Fuse merges the two legs under strategy and returns the ordered reference
    set they fuse into, on the terms of the Fusion port.

type ReferenceSource interface {
	// QueryEvents returns the references matching opts: the matching set
	// filtered, totally ordered by (Timestamp, Key) under the named direction,
	// deduplicated by event key and then paged by Offset and Limit. Two
	// identical queries over identical state return identical sequences,
	// whatever the order the events were recorded in.
	QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)

	// Reference returns the recall reference the event key was landed with,
	// and reports whether the table holds one.
	Reference(key event.EventID) (memory.OriginalRef, bool)
}
    ReferenceSource is the query face a retrieval reads through: the event
    references a QueryOptions matches, and the recall reference of any event
    key the observation table holds. Both legs of a query read through it,
    so the consumption order of the read face holds for the vector leg as well.

    The redline list (3 rules): MUST return the matching set filtered, totally
    ordered by (Timestamp, Key), deduplicated by event key and paged by Offset
    and Limit. MUST return identical sequences for two identical queries over
    identical state. MUST NOT read anything of the query implementation beyond
    these two reads.

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
    Result is the reference set one retrieval returned: the references the query
    hit, ordered from the best hit down.

    It carries references, not text. The index that produced it is not the
    source of truth, so the caller reads the text behind a hit through the
    original path and a hit is never served as a second copy of it. An empty set
    is a legal answer: both a miss and a scope that authorizes nothing produce
    one.

type Searcher interface {
	// Search returns the reference set of q, ranked by the retrieval rule the
	// query runs under and bounded by the page the query asks for.
	Search(ctx context.Context, q Query) (Result, error)
}
    Searcher is the authorized-scope query port: it answers one recall query
    inside the scope the caller is authorized to read and returns the references
    the query hit.

    The redline list (4 rules): MUST answer inside the authorized scope
    the caller holds. MUST treat an empty authorized set as authorizing no
    partition. MUST NOT widen to the whole store when the authorized set is
    empty. MUST NOT answer an unauthorized read as a hit.

type StoreSearcher struct {
	// Has unexported fields.
}
    StoreSearcher is the Searcher over a real store: the query source both legs
    read through, the vector seam the embedding leg borrows, and the embedder
    bound for the retrieval text. A searcher constructed without a vector seam
    or without an embedder answers through the keyword leg alone, which is the
    byte-identical fallback the hybrid contract states.

func NewStoreSearcher(source ReferenceSource, vectors VectorSource, embedder memory.Embedder) *StoreSearcher
    NewStoreSearcher returns a searcher over source. vectors and embedder may
    name nothing, and then the vector leg is empty for every query; the fusion
    behind the searcher is the reciprocal rank fusion of this package.

func (s *StoreSearcher) Search(ctx context.Context, q Query) (Result, error)
    Search returns the reference set of q, ranked by the retrieval rule the
    query runs under and cut to the page the query asks for.

type Strategy string
    Strategy parameterizes how the retrieval legs of a query are combined.
    It is an explicit argument rather than a fixed choice of this package,
    so the rule a query runs under is the rule its caller named.

const (
	// StrategyHybrid fuses the vector leg and the keyword leg by reciprocal
	// rank: a reference scores the sum of one over the rank constant plus its
	// rank, and a tie is broken toward the larger event key.
	StrategyHybrid Strategy = "hybrid"
	// StrategyKeywordOnly keeps the keyword ranking alone, the path a query
	// whose vector leg returned nothing falls to.
	StrategyKeywordOnly Strategy = "keyword_only"
	// StrategyVectorOnly keeps the vector ranking alone.
	StrategyVectorOnly Strategy = "vector_only"
)
type VectorSource interface {
	// Vector returns the embedding vector of the event key and whether it is
	// ready.
	Vector(key event.EventID) ([]float32, bool)
}
    VectorSource is the embedding seam a retrieval borrows its vectors from:
    the vector of one event key and whether its generation completed.

    The redline list (2 rules): MUST return the vector of the event key and
    whether its generation completed. MUST NOT infer a ready vector from a
    generation that has not completed.
