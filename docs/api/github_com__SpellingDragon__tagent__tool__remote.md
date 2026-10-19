package remote // import "github.com/SpellingDragon/tagent/tool/remote"

Package remote adapts the locked trpc-a2a-go client to explicitly addressed
remote agent calls: opening a connection to a named destination, sending one
request under a fixed call identity, and carrying the session association of
that call.

The A2A wire protocol is not reimplemented here. Message encoding,
JSON-RPC transport, agent cards and task types stay with the native
trpc.group/trpc-go/trpc-a2a-go packages this package targets.

VARIABLES

var (
	// ErrTargetMissing reports a remote call that names no complete target: no
	// destination URL beside the agent name it belongs to.
	ErrTargetMissing = errors.New("remote: the call names no explicit target")
	// ErrTargetUndeclared reports a target outside the caller's declaration, so
	// the call is refused rather than dialled.
	ErrTargetUndeclared = errors.New("remote: the named target is not declared by the caller")
	// ErrCallIdentityMissing reports a remote call that carries no attribution
	// identity, so two attempts of it could not be recognized as one call.
	ErrCallIdentityMissing = errors.New("remote: the call carries no attribution identity")
	// ErrConnectorMissing reports a dispatcher constructed without a connector.
	ErrConnectorMissing = errors.New("remote: no connector is attached")
	// ErrCallTargetConflict reports one call identity naming two targets: a call
	// keeps the target it started on.
	ErrCallTargetConflict = errors.New("remote: the call identity is bound to another target")
	// ErrHandoffMissing reports a send that names no handoff to carry.
	ErrHandoffMissing = errors.New("remote: the call carries no handoff")
)
var (
	// ErrToolTargetMissing reports a tool call that names no agent to address.
	// The call is refused rather than aimed at a default peer.
	ErrToolTargetMissing = errors.New("remote: the tool call names no agent")
	// ErrDispatcherMissing reports a tool constructed without a dispatcher.
	ErrDispatcherMissing = errors.New("remote: no dispatcher is attached")
)

FUNCTIONS

func ResultText(result *a2aprotocol.MessageResult) string
    ResultText reports the text one remote call result carries: the message a
    remote agent answered with, or the message of the task status it reported.
    A result carrying no text reports an empty string rather than a rendering of
    its own.

TYPES

type A2ADispatcher struct {
	// Has unexported fields.
}

    A2ADispatcher sends one explicitly addressed request under the caller's
    attribution identity and owns the retry shape of that call.

    Every attempt repeats the same identity and the same target, and a repeat
    of a call that already ran is answered with the outcome it left rather than
    sent a second time: one call identity carries one outcome.

func NewA2ADispatcher(connector Connector, options ...DispatcherOption) (*A2ADispatcher, error)
    NewA2ADispatcher returns the dispatcher over the given connector.

func (d *A2ADispatcher) Call(identity CallIdentity) (CallRecord, bool)
    Call reports the attribution record of one call identity.

func (d *A2ADispatcher) Send(ctx context.Context, target Target, identity CallIdentity, params a2aprotocol.SendMessageParams, opts ...a2aclient.RequestOption) (*a2aprotocol.MessageResult, error)
    Send delivers params to the explicit target under identity. The call is
    refused when it names no complete target, when the target is outside the
    caller's declaration, or when the identity carries no attribution; a repeat
    of an identity already bound to another target is refused as well.

func (d *A2ADispatcher) SendHandoff(ctx context.Context, target Target, identity CallIdentity, handoff HandoffCarrier, params a2aprotocol.SendMessageParams, opts ...a2aclient.RequestOption) (*a2aprotocol.MessageResult, error)
    SendHandoff delivers params whose message carries the handoff as one data
    part beside the parts it already names, and keeps every other property of
    the call: the same target, the same identity, the same retry shape and one
    outcome for the identity.

    The message keeps its own parts, so the handoff travels beside the request
    and never in place of it. A handoff naming no intent or no acceptance
    constraint is refused by name before the call is claimed, so an unacceptable
    handoff never reaches a peer and leaves no attribution record.

type CallIdentity struct {
	// InvocationID identifies the responsibility scope that owns the call.
	InvocationID string

	// DecisionID identifies the model decision that requested the call.
	DecisionID string

	// CallID is the native model call identifier the request carried.
	CallID string
}
    CallIdentity is the identity one remote call keeps across every retry it
    makes: the owning invocation, the owning decision and the native model call
    that started it.

    Two attempts under one identity are one call, so a retry cannot be counted
    as a second one.

type CallRecord struct {
	// Identity is the attribution identity of the call.
	Identity CallIdentity
	// Target is the target the call is bound to.
	Target Target
	// Attempts is the number of attempts the call made.
	Attempts int
	// Settled reports that the call left its outcome.
	Settled bool
}
    CallRecord is the attribution of one remote call: the identity it
    runs under, the target it is bound to, and how many attempts it made.
    Two attempts under one identity share this record, so they are one call and
    never two.

type Connector interface {
	// Connect creates a client for the explicit target.
	Connect(ctx context.Context, target Target, opts ...a2aclient.Option) (*a2aclient.A2AClient, error)
}
    Connector opens an explicitly addressed connection to a remote A2A agent.

    The redline list (2 rules): MUST create a client for the explicit target
    it is handed. MUST NOT resolve a target by default or reuse the most recent
    one.

type Dispatcher interface {
	// Send delivers params to target under identity.
	//
	// Every retry the implementation performs keeps the same identity and the
	// same target, and leaves one outcome for the call.
	Send(ctx context.Context, target Target, identity CallIdentity, params a2aprotocol.SendMessageParams, opts ...a2aclient.RequestOption) (*a2aprotocol.MessageResult, error)
}
    Dispatcher sends one addressed request and owns the retry shape of that
    call.

    The redline list (3 rules): MUST keep the same identity and the same target
    across every retry it performs. MUST leave one outcome for the call.
    MUST NOT record a retry as a second call or a second fact.

type DispatcherOption func(*A2ADispatcher)
    DispatcherOption configures a dispatcher at construction.

func WithEndpoints(endpoints *Endpoints) DispatcherOption
    WithEndpoints attaches the caller's declaration of the agents it may
    address.

func WithRetryPolicy(policy RetryPolicy) DispatcherOption
    WithRetryPolicy attaches the retry shape of the calls this dispatcher makes.

type Endpoints struct {
	// Has unexported fields.
}
    Endpoints is the caller's declaration of the remote agents it may address:
    one explicit target per agent name. A name the declaration does not carry is
    refused, so no call resolves a destination of its own.

func NewEndpoints(targets ...Target) (*Endpoints, error)
    NewEndpoints declares the remote agents one caller may address. Every entry
    names both an agent and its destination, and one agent name carries one
    destination.

func (e *Endpoints) Lookup(agent string) (Target, error)
    Lookup returns the declared target of one agent name. A name the declaration
    does not carry is refused, so no call dials a destination of its own.

func (e *Endpoints) Resolve(target Target) (Target, error)
    Resolve returns the declared target the call names. A name the declaration
    does not carry, or one whose destination differs from the declaration,
    is refused by name.

type HandoffCarrier interface {
	// Validate reports whether the handoff names what a delegated turn could serve.
	Validate() error
	// DataPart returns the part one message carries the handoff as.
	DataPart() a2aprotocol.DataPart
}
    HandoffCarrier is one handoff in the form a remote call carries it:
    the acceptance it is held to and the data part its message travels with. The
    record that states a handoff hands itself over through this seam, so this
    adapter carries a handoff without depending on the package that states one.

    The redline list (2 rules): MUST answer with the part the whole handoff
    travels as, not a fragment of it. MUST NOT answer with a part that names
    another kind.

type IdentityResolver func(ctx context.Context) (CallIdentity, bool)
    IdentityResolver reports the attribution identity of the call under way,
    so the dispatch it drives is attributed to the invocation, decision and
    native call that issued it.

type NativeConnector struct{}

    NativeConnector opens the locked trpc-a2a-go client for one explicit target.
    The agent card, the transport and the message types stay with that client.

func (NativeConnector) Connect(ctx context.Context, target Target, opts ...a2aclient.Option) (*a2aclient.A2AClient, error)
    Connect creates the client of one explicit target.

type RetryPolicy struct {
	// Attempts is the number of attempts one call makes; a value below one is
	// read as one attempt.
	Attempts int
}
    RetryPolicy is the bounded retry shape of one remote call: how many attempts
    a call makes at most before its failure is reported.

func DefaultRetryPolicy() RetryPolicy
    DefaultRetryPolicy is the retry shape of a dispatcher no policy was attached
    to: one attempt, so a call is never repeated behind the caller's back.

func NewRetryPolicy(attempts int) RetryPolicy
    NewRetryPolicy returns the retry shape of the given attempt count.

type SessionBinder interface {
	// Bind returns the session scope to use for the explicit target.
	Bind(ctx context.Context, target Target) (SessionScope, error)
}
    SessionBinder resolves the session association a remote call runs under.

    The redline list (2 rules): MUST resolve the session association of the
    explicit target. MUST NOT hand out a session scope the caller is not
    authorized to bind.

type SessionScope struct {
	// AgentID is the local agent instance that owns the call.
	AgentID string

	// SessionID is the local session the call is attributed to.
	SessionID string

	// ContextID is the A2A context identifier used by this call on the wire.
	ContextID string
}
    SessionScope is the session association of one remote call: the local agent
    and session that own it, plus the A2A context identifier carried on the wire
    for that pair.

type Target struct {
	// AgentURL is the base endpoint of the remote A2A agent.
	AgentURL string

	// AgentID names the remote agent inside the caller's declaration, and is
	// what call attribution reports.
	AgentID string
}
    Target is the explicit destination of a delegation, taken from the caller's
    declaration. A call without a resolvable target is refused; no default or
    most-recent destination is substituted.

type Tool struct {
	// Has unexported fields.
}

    Tool is the callable tool face of one explicitly addressed remote agent
    call: it reads the addressed agent from its arguments, the destination from
    the caller's declaration and the attribution from the resolver, then hands
    one message to the dispatcher.

func NewTool(dispatcher Dispatcher, endpoints *Endpoints, identity IdentityResolver, options ...ToolOption) (*Tool, error)
    NewTool returns the remote call tool over the given dispatcher, declaration
    and identity resolver.

func (t *Tool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call sends the message to the explicitly named agent and answers with the
    text its reply carries. A name the caller's declaration does not carry,
    and a call beside no attribution, are refused by name. A resolver answer
    counts only beside its ok: one the resolver declines to attribute keeps
    the zero identity and is refused by the dispatcher instead of accepted as a
    partial attribution.

func (t *Tool) Declaration() *trpctool.Declaration
    Declaration returns the fixed declaration of the remote call tool.

type ToolOption func(*Tool)
    ToolOption configures the remote call tool at construction.

func WithToolName(name string) ToolOption
    WithToolName sets the declared name of the remote call tool.
