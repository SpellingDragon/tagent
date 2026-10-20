package recall // import "github.com/SpellingDragon/tagent/tool/recall"

The exact read tool of the recall surface: one event key is read back as the
original bytes the scoped path serves, one bounded segment at a time.

Ticket parsing of the recall surface: the three ways a caller spells one event
key all resolve to the same canonical identity.

The retrieval tool of the recall surface: one query runs through the retrieval
port inside the granted partitions, and every hit is answered as the reference
of the event it landed as, never as a copy of its text.

Package recall is the model-facing recall surface of the memory axis: the four
tools a recall agent calls — a retrieval query, an exact read of one event,
the recent page and the causal chain — each answering what the memory side
already holds rather than a retrieval of its own.

The tools consume the read faces one runtime wires; they own no store and keep
no copy of an event. A face a deployment leaves unwired refuses the tool that
needs it by name, so a call never answers an empty set in place of a capability
the deployment does not carry.

The recent page tool of the recall surface: the newest events of the granted
partitions, on the same declarative query semantics the memory read face states,
newest first and cut to one page.

Compile-time assertions of the recall surface: the four tools stay native
callable tools, the declared names stay the vocabulary the prompt spells,
the table port stays satisfied by the observation table, and the scoped original
store stays satisfied by the in-process original store.

The observation-table port of the recall surface: the references a time, type
and keyword query matches, the reference of one landed event and the original
reference that event was landed with.

The causal chain tool of the recall surface: one key is walked back along the
parent links of the relation ledger, and the part that can be read is answered
beside the named reason the walk stopped there.

CONSTANTS

const (
	// QueryToolName is the name of the semantic retrieval tool.
	QueryToolName = "recall_query"
	// GetToolName is the name of the exact read tool.
	GetToolName = "recall_get"
	// RecentToolName is the name of the recent page tool.
	RecentToolName = "recall_recent"
	// TraceToolName is the name of the causal chain tool.
	TraceToolName = "recall_trace"
)
    QueryToolName, GetToolName, RecentToolName and TraceToolName are the
    vocabulary both the prompt and the declaration spell, so a recall agent
    calling a name it was told about reaches the tool that answers it. They are
    frozen: a rename is a prompt change and a declaration change in one breath.

VARIABLES

var ErrRecallKey = errors.New("recall: the call names no event key")
    ErrRecallKey reports a call that names no event key: neither a ticket nor a
    key could be read out of the argument, so no event is addressed.

var ErrRecallMissing = errors.New("recall: the read face holds no such event")
    ErrRecallMissing reports a causal walk whose first key the read face does
    not hold: the chain starts at an event nothing answers for, so there is no
    link to follow and no part to return.

var ErrRecallUnwired = errors.New("recall: the deployment wires no read face for this tool")
    ErrRecallUnwired reports a tool whose read face the deployment did not wire:
    the call is refused by name rather than answered as an empty result,
    so the caller can tell "nothing matched" apart from "nothing could be read".

FUNCTIONS

func New(reads Reads, partitions []string) []trpctool.CallableTool
    New returns the four recall tools over one read face, in a fixed order:
    recall_query, recall_get, recall_recent, recall_trace. The partitions
    are the authorization this caller grants; an empty set grants nothing,
    and the tools answer that no event is inside their scope rather than reading
    the store.

TYPES

type Reads struct {
	// Searcher answers one retrieval query inside the granted partitions.
	Searcher search.Searcher
	// Embedder is the embedding capability of this deployment: nil runs no
	// semantic retrieval at all, and the retrieval tool refuses by name.
	Embedder memory.Embedder
	// Table answers the event references a query matches and the reference of
	// one landed event.
	Table Table
	// Originals serves the text behind one landed event inside its read scope.
	Originals ledger.ScopedOriginalStore
	// Trace answers the parent link of one event.
	Trace *replay.MemoryRelations
}
    Reads is the read face the four tools consume: the retrieval port, the
    embedding capability of this deployment, the observation table, the scoped
    original store and the causal relation ledger.

    A field left nil is a deployment that wires nothing there, and the tool that
    needs it refuses by name with ErrRecallUnwired.

type Table interface {
	// QueryEvents returns the references matching opts, totally ordered and
	// paged on the declarative terms of the memory read face.
	QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)
	// Event returns the reference of the landed event under key and reports
	// whether the table holds it.
	Event(key event.EventID) (memory.EventReference, bool)
	// Reference returns the original reference key was landed with and reports
	// whether the table holds one.
	Reference(key event.EventID) (ledger.OriginalRef, bool)
}
    Table is the observation-table read face the recall tools consume: the event
    references one declarative query matches, the reference of one event the
    table has landed, and the original reference that event was landed with.

    The redline list (3 rules): MUST answer over the events the table has
    already landed and never over a store behind it. MUST answer a key the table
    holds no event for as not held. MUST NOT widen the granted partition set:
    an event outside it takes no part in an answer.
