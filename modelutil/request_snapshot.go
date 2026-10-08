// S1 request snapshot plus the per-component request budget.
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
//
// This file is deliberately a LEAF: it depends only on the framework model/tool
// packages and the standard library, and touches no tagent internal package, so
// it cannot create a layering cycle while agent/compress and the recorder both
// consume it. It owns no lifecycle, storage or scheduler reference — the snapshot
// is a plain frozen value produced by a pure function.
package modelutil

import (
	"encoding/json"
	"fmt"
	"sort"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// CharsPerToken, PerMessageOverheadTokens and PerToolCallOverheadTokens are the
// single source of the estimation numbers: agent/compress's default token counter
// references these same constants, so the request budget and the compression
// trigger line cannot drift apart. They live in this leaf package because
// modelutil may not import tagent internals.
const (
	// CharsPerToken is the character-per-token heuristic (中英混排保守近似).
	CharsPerToken = 2.0
	// PerMessageOverheadTokens is the fixed structural allowance per message,
	// mirroring DefaultTokenCounter.Estimate's per-message +10 term.
	PerMessageOverheadTokens = 10
	// PerToolCallOverheadTokens is the fixed structural allowance per tool call,
	// mirroring DefaultTokenCounter.Estimate's +20×len(ToolCalls) term.
	PerToolCallOverheadTokens = 20

	// protocolEnvelopeBytes is a documented heuristic floor for the request-level
	// JSON envelope (role/turn framing). It is NOT a provider-exact count; the
	// provider-private system prompt and HTTP wire body are intentionally not
	// claimed by this snapshot.
	protocolEnvelopeBytes = 64
)

// RequestSnapshot is the D14-S1 read-only, deep-copied view of ONE model
// request's SDK input: frozen messages, the generation config in force, the
// structured-output spec and a stably-sorted slice of tool DECLARATION
// snapshots. It never replaces the live CallableTool registry — its scope is the
// SDK *input* only, and it does not capture provider-private system prompts or
// the wire body. Its purpose is to let the request budget, the outbound
// model.Request and O5 capture all read the exact same frozen declarations
// instead of each re-reading a hot-mutable source within one call.
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

// ToolDeclarationSnapshot is an immutable copy of a single tool.Declaration
// paired with its registry key (the map key under which the live tool is
// registered in model.Request.Tools). RegistryKey and Name are captured
// separately because they can legitimately differ; the map key is unique per
// tool, so it doubles as the stable sort key.
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

// snapshotParams carries the optional per-request configuration fed to
// NewRequestSnapshot. It is unexported; consumers use the With* options.
type snapshotParams struct {
	gen *model.GenerationConfig
	out *model.StructuredOutput
}

// SnapshotOption customises an otherwise bare NewRequestSnapshot call.
type SnapshotOption func(*snapshotParams)

// WithGenerationConfig records the generation parameters in force for this call.
func WithGenerationConfig(gc model.GenerationConfig) SnapshotOption {
	return func(p *snapshotParams) {
		c := gc
		p.gen = &c
	}
}

// WithStructuredOutput freezes the request's structured-output spec alongside
// the declarations (D14-S1 "结构化输出字段透传").
func WithStructuredOutput(so *model.StructuredOutput) SnapshotOption {
	return func(p *snapshotParams) { p.out = so }
}

// NewRequestSnapshot freezes msgs and tools into an S1 snapshot. It is a pure
// function: the returned value shares no mutable memory with the inputs, so a
// later mutation of the caller's messages or tool registry cannot leak into the
// snapshot (or vice versa). Declaration order is normalised by the stable
// registry key so the same tool set always serialises identically regardless of
// map iteration order. Only the framework model/tool types and the standard
// library are touched — never a tagent internal package.
func NewRequestSnapshot(msgs []model.Message, tools map[string]tool.Tool, cfg ...SnapshotOption) RequestSnapshot {
	var p snapshotParams
	for _, opt := range cfg {
		if opt != nil {
			opt(&p)
		}
	}
	snap := RequestSnapshot{
		Messages: copyMessages(msgs),
		Tools:    snapshotToolDeclarations(tools),
	}
	if p.gen != nil {
		snap.GenerationConfig = copyGenerationConfig(*p.gen)
	}
	if p.out != nil {
		snap.StructuredOutput = copyStructuredOutput(p.out)
	}
	return snap
}

// snapshotToolDeclarations reads each tool's Declaration once and returns an
// immutably sorted view. It calls only Tool.Declaration() — it never asserts
// CallableTool or copies the tool itself, so the executable registry and its
// Close/Iter capabilities stay owned by the caller (D14-S1 read-only split).
func snapshotToolDeclarations(tools map[string]tool.Tool) []ToolDeclarationSnapshot {
	if len(tools) == 0 {
		return nil
	}
	out := make([]ToolDeclarationSnapshot, 0, len(tools))
	for key, tl := range tools {
		s := ToolDeclarationSnapshot{RegistryKey: key}
		if tl != nil {
			if d := tl.Declaration(); d != nil {
				s.Name = d.Name
				s.Description = d.Description
				s.InputSchema = copySchema(d.InputSchema)
				s.OutputSchema = copySchema(d.OutputSchema)
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RegistryKey != out[j].RegistryKey {
			return out[i].RegistryKey < out[j].RegistryKey
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// RequestBudget is the honest, per-component token estimate for a single model
// request (request-budget-accounting). Every quantified bucket uses the shared
// CharsPerToken heuristic plus the shared per-message / per-tool-call overheads
// so it stays consistent with the compressor's counter. Unknown lists items that
// could NOT be measured reliably (e.g. media without usable metadata) so a
// consumer never mistakes an absent line item for "free". Total is only the sum
// of the quantified buckets; when Unknown is non-empty, Total is a floor, not a
// guarantee. Input budget and output limit (max_tokens) remain separate
// concerns — this type never redefines max_tokens as a provider window.
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

// addUnknown records a component that could not be measured. It never counts a
// guessed zero for such an item.
func (b *RequestBudget) addUnknown(reason string) {
	b.Unknown = append(b.Unknown, reason)
}

// EstimateBudget derives the per-component budget for the snapshot. notices
// carries dynamic task-board / recovery-prompt text assembled outside the frozen
// SDK messages; pass "" when there is none. Media content parts without reliable
// sizing are recorded in Unknown rather than counted as zero, and the estimate
// never claims provider-window safety.
func (s RequestSnapshot) EstimateBudget(notices string) RequestBudget {
	b := RequestBudget{}

	for i := range s.Messages {
		m := &s.Messages[i]
		text := tokensOfRunes(m.Content)
		if m.Role == model.RoleSystem {
			b.System += text + PerMessageOverheadTokens
		} else {
			b.Messages += text + PerMessageOverheadTokens
		}
		if m.ReasoningContent != "" {
			b.Reasoning += tokensOfRunes(m.ReasoningContent)
		}
		if m.ReasoningSignature != "" {
			b.Reasoning += tokensOfBytes([]byte(m.ReasoningSignature))
		}
		for j := range m.ToolCalls {
			b.ToolCallArguments += PerToolCallOverheadTokens
			if args := m.ToolCalls[j].Function.Arguments; len(args) > 0 {
				b.ToolCallArguments += tokensOfBytes(args)
			}
		}
		for k := range m.ContentParts {
			p := &m.ContentParts[k]
			if p.Type == model.ContentTypeText {
				if p.Text != nil {
					b.ContentParts += tokensOfRunes(*p.Text)
				}
				continue
			}
			b.addUnknown(fmt.Sprintf("content part (type=%q) has no reliable token metadata", string(p.Type)))
		}
	}

	for i := range s.Tools {
		t := &s.Tools[i]
		b.ToolDeclarations += tokensOfRunes(t.Name) + tokensOfRunes(t.Description)
		b.ToolDeclarations += schemaTokens(t.InputSchema, &b)
		b.ToolDeclarations += schemaTokens(t.OutputSchema, &b)
	}

	b.Notices = tokensOfRunes(notices)
	b.ProtocolOverhead = s.protocolOverhead(&b)

	b.Total = b.System + b.Messages + b.ToolDeclarations + b.ToolCallArguments +
		b.Reasoning + b.ContentParts + b.Notices + b.ProtocolOverhead
	return b
}

// protocolOverhead adds the request envelope plus the structured-output wrapper
// JSON, so a large structured-output schema is not silently free.
func (s RequestSnapshot) protocolOverhead(b *RequestBudget) int {
	var over int
	if len(s.Messages) > 0 || len(s.Tools) > 0 ||
		(s.StructuredOutput != nil && s.StructuredOutput.JSONSchema != nil) {
		over += protocolEnvelopeBytes
	}
	if s.StructuredOutput != nil && s.StructuredOutput.JSONSchema != nil {
		if data, err := json.Marshal(s.StructuredOutput.JSONSchema.Schema); err == nil {
			over += len(data)
		} else {
			b.addUnknown(fmt.Sprintf("structured-output schema could not be serialized for budgeting: %v", err))
		}
	}
	return int(float64(over) / CharsPerToken)
}

// schemaTokens estimates a tool schema from its serialized JSON length. On a
// marshal failure it records Unknown instead of silently counting zero.
func schemaTokens(s *tool.Schema, b *RequestBudget) int {
	if s == nil {
		return 0
	}
	data, err := json.Marshal(s)
	if err != nil {
		b.addUnknown(fmt.Sprintf("tool schema could not be serialized for budgeting: %v", err))
		return 0
	}
	return tokensOfBytes(data)
}

func tokensOfRunes(s string) int {
	if s == "" {
		return 0
	}
	return int(float64(len([]rune(s))) / CharsPerToken)
}

func tokensOfBytes(p []byte) int {
	if len(p) == 0 {
		return 0
	}
	return int(float64(len(p)) / CharsPerToken)
}

func copyMessages(msgs []model.Message) []model.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]model.Message, len(msgs))
	for i := range msgs {
		out[i] = copyMessage(&msgs[i])
	}
	return out
}

func copyMessage(m *model.Message) model.Message {
	c := *m
	c.ContentParts = copyContentParts(m.ContentParts)
	c.ToolCalls = copyToolCalls(m.ToolCalls)
	return c
}

func copyContentParts(parts []model.ContentPart) []model.ContentPart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]model.ContentPart, len(parts))
	for i, p := range parts {
		cp := p
		cp.Text = clonePtr(p.Text)
		cp.Image = copyImage(p.Image)
		cp.Audio = copyAudio(p.Audio)
		cp.Video = copyVideo(p.Video)
		cp.File = copyFile(p.File)
		cp.ContentRef = clonePtr(p.ContentRef)
		out[i] = cp
	}
	return out
}

func copyImage(v *model.Image) *model.Image {
	if v == nil {
		return nil
	}
	c := *v
	c.Data = cloneBytes(v.Data)
	return &c
}

func copyAudio(v *model.Audio) *model.Audio {
	if v == nil {
		return nil
	}
	c := *v
	c.Data = cloneBytes(v.Data)
	return &c
}

func copyVideo(v *model.Video) *model.Video {
	if v == nil {
		return nil
	}
	c := *v
	c.Data = cloneBytes(v.Data)
	return &c
}

func copyFile(v *model.File) *model.File {
	if v == nil {
		return nil
	}
	c := *v
	c.Data = cloneBytes(v.Data)
	return &c
}

func copyToolCalls(tcs []model.ToolCall) []model.ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	out := make([]model.ToolCall, len(tcs))
	for i, tc := range tcs {
		c := tc
		c.Index = clonePtr(tc.Index)
		c.Function.Arguments = cloneBytes(tc.Function.Arguments)
		c.ExtraFields = deepCopyAnyMap(tc.ExtraFields)
		out[i] = c
	}
	return out
}

func copySchema(s *tool.Schema) *tool.Schema {
	if s == nil {
		return nil
	}
	return &tool.Schema{
		Type:                 s.Type,
		Description:          s.Description,
		Pattern:              s.Pattern,
		Required:             copyStringSlice(s.Required),
		Properties:           copySchemaMap(s.Properties),
		Items:                copySchema(s.Items),
		AdditionalProperties: deepCopyAny(s.AdditionalProperties),
		Default:              deepCopyAny(s.Default),
		Enum:                 deepCopyAnySlice(s.Enum),
		Ref:                  s.Ref,
		Defs:                 copySchemaMap(s.Defs),
	}
}

func copySchemaMap(m map[string]*tool.Schema) map[string]*tool.Schema {
	if m == nil {
		return nil
	}
	out := make(map[string]*tool.Schema, len(m))
	for k, v := range m {
		out[k] = copySchema(v)
	}
	return out
}

func copyGenerationConfig(gc model.GenerationConfig) model.GenerationConfig {
	out := gc
	out.Stop = copyStringSlice(gc.Stop)
	out.MaxTokens = clonePtr(gc.MaxTokens)
	out.Temperature = clonePtr(gc.Temperature)
	out.TopP = clonePtr(gc.TopP)
	out.PresencePenalty = clonePtr(gc.PresencePenalty)
	out.FrequencyPenalty = clonePtr(gc.FrequencyPenalty)
	out.Logprobs = clonePtr(gc.Logprobs)
	out.TopLogprobs = clonePtr(gc.TopLogprobs)
	out.ReasoningEffort = clonePtr(gc.ReasoningEffort)
	out.ThinkingEnabled = clonePtr(gc.ThinkingEnabled)
	out.ThinkingTokens = clonePtr(gc.ThinkingTokens)
	out.ThinkingLevel = clonePtr(gc.ThinkingLevel)
	return out
}

func copyStructuredOutput(so *model.StructuredOutput) *model.StructuredOutput {
	if so == nil {
		return nil
	}
	out := *so
	if so.JSONSchema != nil {
		js := *so.JSONSchema
		js.Schema = deepCopyAnyMap(so.JSONSchema.Schema)
		out.JSONSchema = &js
	}
	return &out
}

func copyStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte(nil), b...)
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// deepCopyAny clones the generic JSON-shaped payloads that appear in schemas and
// extra fields. Strings/numbers/bools are value-immutable and returned as-is;
// maps and slices are rebuilt. Opaque pointers to provider-specific structs are
// not reflected into (they are not mutated in practice and are out of the
// snapshot's mutation-isolation guarantee); see the delivery report limitation.
func deepCopyAny(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case map[string]any:
		return deepCopyAnyMap(x)
	case []any:
		return deepCopyAnySlice(x)
	case []string:
		return copyStringSlice(x)
	case []byte:
		return cloneBytes(x)
	default:
		return v
	}
}

func deepCopyAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = deepCopyAny(v)
	}
	return out
}

func deepCopyAnySlice(in []any) []any {
	if in == nil {
		return nil
	}
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = deepCopyAny(v)
	}
	return out
}
