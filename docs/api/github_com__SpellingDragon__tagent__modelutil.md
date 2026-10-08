package modelutil // import "github.com/SpellingDragon/tagent/modelutil"

Package modelutil hosts the shared assembly for direct (non-agent)
model call sites — summary compression and the evolution judge

This file is deliberately a LEAF: it depends only on the framework model/tool
packages and the standard library, and touches no tagent internal package,
so it cannot create a layering cycle while agent/compress and the recorder both
consume it. It owns no lifecycle, storage or scheduler reference — the snapshot
is a plain frozen value produced by a pure function.

CONSTANTS

const (
	// CharsPerToken is the character-per-token heuristic (中英混排保守近似).
	CharsPerToken = 2.0
	// PerMessageOverheadTokens is the fixed structural allowance per message,
	// mirroring DefaultTokenCounter.Estimate's per-message +10 term.
	PerMessageOverheadTokens = 10
	// PerToolCallOverheadTokens is the fixed structural allowance per tool call,
	// mirroring DefaultTokenCounter.Estimate's +20×len(ToolCalls) term.
	PerToolCallOverheadTokens = 20
)
    CharsPerToken, PerMessageOverheadTokens and PerToolCallOverheadTokens are
    the single source of the estimation numbers: agent/compress's default token
    counter references these same constants, so the request budget and the
    compression trigger line cannot drift apart. They live in this leaf package
    because modelutil may not import tagent internals.

VARIABLES

var ErrNilStream = errors.New("modelutil: provider returned a nil response stream")
    ErrNilStream names the determinable failure of a provider that returned a
    nil response channel without an error. Ranging over a nil channel blocks
    forever, so the bounded synchronous summary call must treat it as a failure
    instead of hanging the BeforeModel round.

FUNCTIONS

func BuildRequest(msgs []model.Message, k Knobs) *model.Request
    BuildRequest assembles a model.Request with generation knobs applied.

func Call(ctx context.Context, m model.Model, req *model.Request) (string, error)
    Call runs one direct completion and collects the final text. It carries the
    reasoning-fallback drain shared by summary/judge sites: when a reasoning
    model returns empty Content but non-empty ReasoningContent, the reasoning
    text is used instead of failing (first generalized from the summary site).

    The drain is bounded by the caller's context (O3.4): every iteration selects
    on ctx.Done, so a provider that stops delivering can neither outlive the
    summary deadline nor ignore a parent cancellation — Call returns the context
    error and DROPS the stream, which is exactly the "no late rewrite" property
    the synchronous summary round needs (there is no background reader that
    could hand a belated answer back to the caller). Providers keep owning their
    send side and are bound by the same ctx they were handed here; nothing is
    retried.

TYPES

type Knobs struct {
	Temperature          *float64
	MaxTokens            *int
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string
}
    Knobs carries the generation settings a direct call site may override.
    Nil fields stay untouched so per-site defaults (e.g. summaryMaxTokens)
    remain authoritative when the ModelRef omits them.

type RequestBudget struct {
	// System counts role=system messages (assembled prompt).
	System int
	// Messages counts non-system historical/current-turn text.
	Messages int
	// ToolDeclarations counts frozen name+description+schema JSON.
	ToolDeclarations int
	// ToolCallArguments counts the raw JSON argument bytes of every tool call,
	// plus the fixed per-tool-call overhead. Long arguments are not dropped.
	ToolCallArguments int
	// Reasoning counts ReasoningContent and ReasoningSignature.
	Reasoning int
	// ContentParts counts text content parts; media goes to Unknown.
	ContentParts int
	// Notices counts dynamic task-board / recovery-prompt text.
	Notices int
	// ProtocolOverhead is the request-level JSON envelope plus any
	// structured-output wrapper cost. It is a documented estimate.
	ProtocolOverhead int
	// Unknown lists things that could not be reliably quantified.
	Unknown []string
	// Total is the sum of the quantified buckets (a floor when Unknown is set).
	Total int
}
    RequestBudget is the honest, per-component token estimate for a single
    model request (request-budget-accounting). Every quantified bucket uses the
    shared CharsPerToken heuristic plus the shared per-message / per-tool-call
    overheads so it stays consistent with the compressor's counter. Unknown
    lists items that could NOT be measured reliably (e.g. media without usable
    metadata) so a consumer never mistakes an absent line item for "free".
    Total is only the sum of the quantified buckets; when Unknown is non-empty,
    Total is a floor, not a guarantee. Input budget and output limit
    (max_tokens) remain separate concerns — this type never redefines max_tokens
    as a provider window.

type RequestSnapshot struct {
	// Messages is a deep copy of the request messages, in original order.
	Messages []model.Message
	// Tools is the frozen tool-declaration view, sorted by stable key.
	Tools []ToolDeclarationSnapshot
	// GenerationConfig is a deep copy of the generation parameters in force.
	GenerationConfig model.GenerationConfig
	// StructuredOutput is a deep copy of the request's structured-output spec, or nil.
	StructuredOutput *model.StructuredOutput
}
    RequestSnapshot is the D14-S1 read-only, deep-copied view of ONE model
    request's SDK input: frozen messages, the generation config in force,
    the structured-output spec and a stably-sorted slice of tool DECLARATION
    snapshots. It never replaces the live CallableTool registry — its scope
    is the SDK *input* only, and it does not capture provider-private system
    prompts or the wire body. Its purpose is to let the request budget,
    the outbound model.Request and O5 capture all read the exact same frozen
    declarations instead of each re-reading a hot-mutable source within one
    call.

func NewRequestSnapshot(msgs []model.Message, tools map[string]tool.Tool, cfg ...SnapshotOption) RequestSnapshot
    NewRequestSnapshot freezes msgs and tools into an S1 snapshot. It is a pure
    function: the returned value shares no mutable memory with the inputs, so a
    later mutation of the caller's messages or tool registry cannot leak into
    the snapshot (or vice versa). Declaration order is normalised by the stable
    registry key so the same tool set always serialises identically regardless
    of map iteration order. Only the framework model/tool types and the standard
    library are touched — never a tagent internal package.

func (s RequestSnapshot) EstimateBudget(notices string) RequestBudget
    EstimateBudget derives the per-component budget for the snapshot.
    notices carries dynamic task-board / recovery-prompt text assembled outside
    the frozen SDK messages; pass "" when there is none. Media content parts
    without reliable sizing are recorded in Unknown rather than counted as zero,
    and the estimate never claims provider-window safety.

type SnapshotOption func(*snapshotParams)
    SnapshotOption customises an otherwise bare NewRequestSnapshot call.

func WithGenerationConfig(gc model.GenerationConfig) SnapshotOption
    WithGenerationConfig records the generation parameters in force for this
    call.

func WithStructuredOutput(so *model.StructuredOutput) SnapshotOption
    WithStructuredOutput freezes the request's structured-output spec alongside
    the declarations (D14-S1 "结构化输出字段透传").

type ToolDeclarationSnapshot struct {
	// RegistryKey is the model.Request.Tools map key.
	RegistryKey string
	// Name is Declaration().Name (the model-facing tool name).
	Name string
	// Description is Declaration().Description.
	Description string
	// InputSchema is a deep copy of Declaration().InputSchema, or nil.
	InputSchema *tool.Schema
	// OutputSchema is a deep copy of Declaration().OutputSchema, or nil.
	OutputSchema *tool.Schema
}
    ToolDeclarationSnapshot is an immutable copy of a single tool.Declaration
    paired with its registry key (the map key under which the live tool is
    registered in model.Request.Tools). RegistryKey and Name are captured
    separately because they can legitimately differ; the map key is unique per
    tool, so it doubles as the stable sort key.
