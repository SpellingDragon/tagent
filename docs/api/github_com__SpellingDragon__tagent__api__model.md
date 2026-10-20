package model // import "github.com/SpellingDragon/tagent/api/model"

Package model is the runtime's single entry point to the locked tRPC model
protocol.

Every exported name in this package is a thin, unwrapped alias
of the corresponding symbol in the locked framework package
trpc.group/trpc-go/trpc-agent-go/model, so one import gives the runtime
the native model, request, response and message types without building a
mirror schema or a second tool loop. The aliases preserve the native method
sets and field layouts exactly: no alias wraps a native type, copies or
reshapes a native field, or adds behaviour of its own. The one behaviour this
package owns is the request-scoped terminal reduction over tool-call streams
(ReduceChannel/ReduceSeq); terminal.go states that contract, including that the
reduction is the single terminal-decision entry point the agent loop consumes.

The model-response face of the observation bypass: one chat span on the
framework tracer beside one bounded line on the framework logger per model
request, both rendered from the Outcome the single reduction already derived.

The redline list (4 rules): MUST render after the terminal stands,
from the reduced Outcome, and never re-fold the stream. MUST keep every
rendering bounded: identities, counts and token totals, never prompt or
completion content. MUST stay a bypass: a failing tracer or logger is a missing
observation, never a failed request. MUST NOT mint a second terminal beside the
reduction it observes.

Package model folds one tool-call response stream into one terminal per model
request; this file declares the terminal reduction contract that every stream
consumer is held to.

- Terminal reduction has exactly one entry point per stream shape: ReduceChannel
for a response channel and ReduceSeq for a response sequence, both folding one
model request into one Outcome, so channel and iterator consumers share exactly
one terminal judgement. - A consumer — the agent execution loop — consumes the
Outcome and MUST NOT write a second terminal judgement beside the reduction:
no re-classifying responses into the six Signal states, no parallel request
fold, and no terminal derived from stream mechanics (channel close, sequence
exhaustion, cancellation). - The reduction is request-scoped: one call covers
exactly one model request, and usage, body, trace, late responses and the
terminal belong to that request alone and never accumulate across requests.
- Every end path leaves its own evidence in the Outcome and never stands a
second terminal: a nil stream handle fails the reduction as KindFailed with
AlertNilStream; a context cancellation closes it as KindCancelled with a
SignalCancelled trace item while the producer is not required to have stopped;
a producer end records a SignalProducerDone trace item and resolves the standing
terminal, or alerts AlertUnknownTerminal when no terminal evidence stands.
- The three paths are told apart by these marks, not by guesswork on the stream.

CONSTANTS

const (
	// RoleSystem is the system role.
	RoleSystem = trpcmodel.RoleSystem
	// RoleUser is the user role.
	RoleUser = trpcmodel.RoleUser
	// RoleAssistant is the assistant role.
	RoleAssistant = trpcmodel.RoleAssistant
	// RoleTool is the tool response role.
	RoleTool = trpcmodel.RoleTool
)
    RoleSystem, RoleUser, RoleAssistant and RoleTool are aliases for the locked
    tRPC message author roles.

const (
	// ErrorTypeStreamError is a stream-level failure carried by a Response.
	ErrorTypeStreamError = trpcmodel.ErrorTypeStreamError
	// ErrorTypeAPIError is an API-level failure carried by a Response.
	ErrorTypeAPIError = trpcmodel.ErrorTypeAPIError
	// ErrorTypeCancelled is a cancellation or timeout failure carried by a
	// Response.
	ErrorTypeCancelled = trpcmodel.ErrorTypeCancelled
)
    ErrorTypeStreamError, ErrorTypeAPIError and ErrorTypeCancelled are aliases
    for the locked tRPC response error type constants, so the terminal reduction
    reads the native error categories. Every constant re-exported here is
    one the runtime reads: a category no consumer reads is not re-exported,
    and no second spelling of it is kept beside the reduction that reads it.

const (
	// AlertNilStream is reported when the stream handle handed to a reduction
	// entry point is nil.
	AlertNilStream = "nil-stream"
	// AlertNilResponse is reported when a stream yields a nil response item.
	AlertNilResponse = "nil-response"
	// AlertLateResponse is reported when a response arrives after the terminal
	// already stands.
	AlertLateResponse = "late-response"
	// AlertUnknownTerminal is reported when the producer ends without terminal
	// evidence, so the terminal is unknown instead of completed.
	AlertUnknownTerminal = "unknown-terminal"
)
    AlertNilStream, AlertNilResponse, AlertLateResponse and AlertUnknownTerminal
    name the four reasons the terminal reduction alerts its caller.

VARIABLES

var ErrNilResponse = errors.New("model: nil response observed in stream")
    ErrNilResponse reports a nil response item observed inside a model stream.

var ErrNoTerminal = errors.New("model: no terminal evidence")
    ErrNoTerminal reports an observation that carries no terminal evidence of
    its own.

TYPES

type Alert struct {
	// Reason is one of the Alert constants declared beside this type.
	Reason string
	// Terminal is the terminal Kind standing when the alert fired.
	Terminal Kind
	// Detail carries the diagnostic text of the alerting observation.
	Detail string
}
    Alert reports one observation that the terminal reduction surfaces to its
    caller instead of resolving silently.

type Choice = trpcmodel.Choice
    Choice is an alias for the locked tRPC completion choice held by a Response.

type ContentPart = trpcmodel.ContentPart
    ContentPart is an alias for the locked tRPC multimodal content part held by
    a Message.

type FunctionDefinitionParam = trpcmodel.FunctionDefinitionParam
    FunctionDefinitionParam is an alias for the locked tRPC function definition
    and JSON-encoded arguments of a ToolCall.

type Info = trpcmodel.Info
    Info is an alias for the locked tRPC model descriptor returned by
    Model.Info.

type Item struct {
	// Signal is the six-state category the observed input reduces to.
	Signal Signal
	// Kind is the terminal Kind standing after this item; a usage tail carries
	// the standing terminal forward because it bears no terminal evidence of its
	// own.
	Kind Kind
	// Late reports whether the item arrived after the terminal already stood.
	Late bool
	// Dispatchable reports whether tool dispatch is permitted for this item: a
	// valid tool terminal is dispatchable, and every late item is not.
	Dispatchable bool
	// Err is the error the observed input itself carries, nil when it carries
	// none.
	Err error
}
    Item is one observed input of the terminal reduction, recording the signal
    the input carries and the terminal standing after it.

type IterModel = trpcmodel.IterModel
    IterModel is an alias for the locked tRPC iterator extension of Model,
    which streams responses in the caller goroutine instead of over a channel.

type Kind int

    Kind identifies the unique terminal outcome that the single terminal
    reduction entry point derives for a model request.

    The terminal is contract-frozen: one request MUST reduce to exactly one
    Kind, the five values are mutually exclusive, and a consumer reads the Kind
    of the returned Outcome as the terminal fact instead of re-deriving it.
    KindUnknown is the zero value, so an unset or undeterminable terminal is
    never silently reported as completed.

const (
	// KindUnknown is the zero value; it means the terminal could not be
	// determined. Callers MUST treat it as an alert and MUST NOT downgrade it to
	// KindCompleted.
	KindUnknown Kind = iota
	// KindCompleted is the terminal of a request that produced a complete,
	// non-partial answer.
	KindCompleted
	// KindFailed is the terminal of a request whose stream carried an error, or
	// whose entry failed before a stream could be established.
	KindFailed
	// KindCancelled is the terminal of a request cancelled by its caller.
	KindCancelled
	// KindPartialOnly is the terminal of a stream that ended after producing only
	// partial output.
	KindPartialOnly
)
func (k Kind) String() string
    String reports the frozen spelling of one terminal kind: the five
    declarations above are the terminals a reduction answers, and a value
    outside them reports its ordinal so a gap never reads as a declared
    terminal.

type Message = trpcmodel.Message
    Message is an alias for the locked tRPC conversation message, preserving the
    native role, content parts, tool calls and reasoning fields.

type Model = trpcmodel.Model
    Model is an alias for the locked tRPC model interface, which streams
    responses over a channel and reports system-level failures through its error
    return.

type Outcome struct {
	// Kind is the unique terminal of the request.
	Kind Kind
	// Usage is the usage accumulated across every observed response.
	Usage Usage
	// Body is the last response carrying valid content, or nil when none did.
	Body *Response
	// Trace holds every recorded item in observation order.
	Trace []Item
	// Late holds the non-tail responses observed after the terminal stood.
	Late []*Response
}
    Outcome is the single terminal fact of one model request: one Kind beside
    the accumulated usage, the retained body, the observation trace and the
    late responses. It is request-scoped — one Outcome covers exactly one model
    request, and nothing accumulates across requests — and a consumer (the agent
    execution loop) consumes it as the terminal fact: it reads Kind and the
    recorded evidence and MUST NOT re-classify the six states or re-derive the
    terminal.

func ReduceChannel(ctx context.Context, ch <-chan *Response, onAlert func(Alert)) (outcome Outcome)
    ReduceChannel is one of the two terminal reduction entry points:
    it consumes a response channel of one tool-call stream into that request's
    single terminal outcome, and reports every alert to onAlert while it is
    non-nil. It is request-scoped: one call covers exactly one model request,
    and the same responses reduced through ReduceSeq yield the same terminal.
    End paths leave distinct evidence and never stand a second terminal: a nil
    channel fails the reduction as KindFailed with alert reason AlertNilStream;
    a cancelled context closes it as KindCancelled (a standing terminal is never
    moved) with a SignalCancelled trace item, without waiting for the channel to
    close and without requiring the producer to stop; a closed channel records
    a SignalProducerDone trace item that resolves the standing terminal — or
    alerts AlertUnknownTerminal when no terminal evidence stands. The consumer
    reads the returned Outcome as the terminal fact and MUST NOT write a second
    terminal judgement beside it.

func ReduceSeq(ctx context.Context, seq Seq[*Response], onAlert func(Alert)) (outcome Outcome)
    ReduceSeq is the other terminal reduction entry point: it consumes a
    response sequence of one tool-call stream into that request's single
    terminal outcome, and reports every alert to onAlert while it is non-nil.
    It is request-scoped: one call covers exactly one model request, and the
    same responses reduced through ReduceChannel yield the same terminal.
    End paths leave distinct evidence and never stand a second terminal: a nil
    sequence fails the reduction as KindFailed with alert reason AlertNilStream;
    a cancelled context stops the consumption as KindCancelled (a standing
    terminal is never moved) with a SignalCancelled trace item, without waiting
    for the sequence to end and without requiring the producer to stop; an
    exhausted sequence records a SignalProducerDone trace item that resolves the
    standing terminal — or alerts AlertUnknownTerminal when no terminal evidence
    stands. The consumer reads the returned Outcome as the terminal fact and
    MUST NOT write a second terminal judgement beside it.

type Reducer struct {
	// Has unexported fields.
}
    Reducer folds one model request into its single terminal outcome.
    The channel consumer and the iterator consumer drive the same reducer,
    so both stream shapes share exactly one terminal judgement. One request is
    folded by exactly one Reducer, driven through ReduceChannel or ReduceSeq;
    a consumer MUST NOT keep a parallel accumulator or derive a second terminal
    beside the returned Outcome.

func NewReducer(onAlert func(Alert)) *Reducer
    NewReducer returns a reducer that reports every alert to onAlert while it is
    non-nil.

func (r *Reducer) Cancel() Item
    Cancel closes the reduction as KindCancelled unless a terminal already
    stands. The producer is not required to stop for the cancellation to take
    effect. A cancellation after the terminal stands changes nothing.

func (r *Reducer) Observe(resp *Response) Item
    Observe folds one observed response into the reduction and returns the
    item it added to the trace. Every response deposits its usage, while only a
    response carrying valid content may take the body slot before the terminal
    stands.

func (r *Reducer) Outcome() Outcome
    Outcome resolves the reduction and returns its single terminal fact.
    A reduction that never observed the producer end is resolved as if it had.

func (r *Reducer) ProducerDone() Item
    ProducerDone records the end of the producer and resolves a standing
    provisional terminal. A repeated call records nothing and changes nothing.

type Request = trpcmodel.Request
    Request is an alias for the locked tRPC model request, which carries the
    native conversation history and generation configuration.

type Response = trpcmodel.Response
    Response is an alias for the locked tRPC model response, which is the unit
    of the single terminal reduction performed by this package.

type ResponseError = trpcmodel.ResponseError
    ResponseError is an alias for the locked tRPC API-level error carried by a
    Response.

type Role = trpcmodel.Role
    Role is an alias for the locked tRPC message author role.

type Seq[T any] = trpcmodel.Seq[T]
    Seq is an alias for the locked tRPC callback-based response sequence yielded
    by IterModel.GenerateContentIter.

type Signal int
    Signal identifies one input category that the single terminal reduction path
    must observe and evidence separately while it derives the terminal Kind.

    The signals form the six-state evidence table of the reduction: every
    observed input reduces to exactly one Signal (the six are mutually exclusive
    and exhaustive per observation), and a Signal is evidence of an observation,
    never a terminal of its own — only a Kind is a terminal.

const (
	// SignalValidTerminal is the evidence of a complete, valid terminal
	// response: the only signal that stands KindCompleted.
	SignalValidTerminal Signal = iota
	// SignalUsageTail is the evidence of a usage-only tail packet: it deposits
	// usage and never replaces the body or overrides the standing terminal.
	SignalUsageTail
	// SignalPartialOnly is the evidence of a response carrying only partial
	// output: it stands a provisional KindPartialOnly that only the producer end
	// settles.
	SignalPartialOnly
	// SignalError is the evidence of an error response or a nil stream item: it
	// stands KindFailed and carries the error itself.
	SignalError
	// SignalCancelled is the evidence of a cancellation or timeout error, or of
	// a caller cancellation closing the reduction: it stands KindCancelled and
	// is distinct from both the error signal and the producer end.
	SignalCancelled
	// SignalProducerDone is the evidence of the producer end (a closed channel
	// or an exhausted sequence): it resolves the standing terminal and never
	// stands a second terminal.
	SignalProducerDone
)
type TimingInfo = trpcmodel.TimingInfo
    TimingInfo is an alias for the locked tRPC generation timing record carried
    by a Usage.

type ToolCall = trpcmodel.ToolCall
    ToolCall is an alias for the locked tRPC tool call carried by an assistant
    message, preserving the native call identifier and function arguments.

type Usage = trpcmodel.Usage
    Usage is an alias for the locked tRPC token usage record carried by a
    Response, including the usage tail packet that ends a stream.
