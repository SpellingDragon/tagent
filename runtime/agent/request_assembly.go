package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/state"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// RequestChain is the causal chain identity one assembled request belongs to:
// the partition and the session the projected facts were replayed under. It is
// part of the assembly boundary, so one user's session chain is never joined
// into another's by a shared partition.
type RequestChain struct {
	// PartitionID is the partition the chain's facts are filed under.
	PartitionID int
	// SessionID is the session the chain belongs to; empty is refused by name.
	SessionID event.SessionID
}

// ErrRequestChainSessionRequired reports a request assembly under a chain that
// names no session: assembling one would join every user of the partition into
// one transcript.
var ErrRequestChainSessionRequired = errors.New("agent: projected request assembly needs a named session chain")

// RequestBoundary is the material one request boundary assembles its prefix
// from: the agent whose request it is, the causal chain the replay is read
// under, the borrowed binding the fixed file contents are read for and whether
// the request is the closing round of its drive.
type RequestBoundary struct {
	// Agent is the agent whose request is being assembled.
	Agent event.AgentID
	// Chain is the causal chain the projected replay belongs to.
	Chain RequestChain
	// Binding is the borrowed binding generation the request runs on.
	Binding state.AgentBinding
	// FinalRound marks the request of a drive's closing round: the round the
	// model delivers in after its budget is spent, whose tool calls are admitted
	// nowhere. It stands false for every ordinary request.
	FinalRound bool
	// ProjectionBudget overrides the token budget the projection presents under
	// when the loop's final request budget must fold the history further than
	// the replay window would. Zero presents the projection under the window's
	// own budget (see projectionBudget), so an ordinary request keeps the fold
	// semantics its window declares; a positive value is the room the final
	// request budget computed for the projected material after every other face
	// already yielded.
	ProjectionBudget int
}

// BoundaryView is what the request boundary settled on: the prefix messages
// the request carries in front of the transcript, in the fixed layout the
// request face assembles, the dynamic environment block the request closes
// with, the content version the request runs on, and the skill names that
// material makes visible. An empty PromptVersion keeps the version the binding
// already carries: a binding that names no files fixes no content version of
// its own.
type BoundaryView struct {
	// Messages are the prefix messages of the request, in fixed layout order.
	Messages []model.Message
	// Sources names the input face each Messages entry renders, one entry per
	// message in the same order: fixed_prompt, skill_bodies or history. A view
	// that names fewer sources than messages reports the unnamed tail as
	// history. The loop's request budget reads the sources to let one face yield
	// without touching another, and a request boundary that reports no sources
	// keeps every prefix message on the history face.
	Sources []string
	// DynamicSystem is the dynamic system section the request closes with: the
	// bodies the skill loads published beside the environment block an agent
	// declares, rendered as one system message the loop appends as the last
	// message of the request. The stable prefix ahead of it keeps the provider
	// cache; an empty section appends nothing.
	DynamicSystem string
	// PromptVersion is the content version the request boundary fixed.
	PromptVersion string
	// VisibleSkills lists the skill names the boundary material makes visible to
	// the request, in name order and once each: the skill bodies it fixed into
	// the prefix beside the bodies the binding already loaded. A boundary that
	// names no skill reports none, and the names carry no version of their own.
	VisibleSkills []string

	// budget is the request budget policy the boundary declared. The zero policy
	// is the closed gate: a boundary that declares no window leaves every request
	// whole, the debug fact of a declaration that names no budget.
	budget requestBudgetPolicy
	// dynamic is the section split into the faces it carries, in render order.
	// The loop rebuilds DynamicSystem from the parts it keeps, so dropping one
	// face never rewrites the other's framing.
	dynamic []boundaryDynamicPart
}

// WithBudget declares the request budget policy of this boundary: the provider
// context window and the output reserve the input budget must leave. A value
// zero or below names no window, so the gate stays closed.
func (v BoundaryView) WithBudget(providerWindow, outputReserve int) BoundaryView {
	if providerWindow < 0 {
		providerWindow = 0
	}
	if outputReserve < 0 {
		outputReserve = 0
	}
	v.budget = requestBudgetPolicy{providerWindow: providerWindow, outputReserve: outputReserve}
	return v
}

// WithDynamicSkills appends the skill bodies a load published to the dynamic
// section, recorded as the skill_bodies face the request budget may let yield.
func (v BoundaryView) WithDynamicSkills(body string) BoundaryView {
	if body == "" {
		return v
	}
	v.dynamic = append(v.dynamic, boundaryDynamicPart{source: BudgetFaceSkillBodies, body: body})
	v.DynamicSystem = joinBoundaryDynamic(v.dynamic)
	return v
}

// WithEnvironmentBlock appends the environment block to the dynamic section,
// recorded as the environment face the request budget yields before any other
// dynamic face.
func (v BoundaryView) WithEnvironmentBlock(body string) BoundaryView {
	if body == "" {
		return v
	}
	v.dynamic = append(v.dynamic, boundaryDynamicPart{source: BudgetFaceEnvironment, body: body})
	v.DynamicSystem = joinBoundaryDynamic(v.dynamic)
	return v
}

// requestBudgetPolicy is the budget one boundary declared for its final
// request: the provider context window and the output allowance the input
// budget leaves. A zero window is the closed gate.
type requestBudgetPolicy struct {
	providerWindow int
	outputReserve  int
}

// gateOpen reports whether the declaration opened the request budget gate. The
// gate is open exactly when the declaration names a provider window, explicitly
// or by deriving it from MaxTokens.
func (p requestBudgetPolicy) gateOpen() bool { return p.providerWindow > 0 }

// inputBudget reports the room the request's input material may take: the
// provider window minus the output reserve, never below zero. A declaration
// whose reserve consumes the whole window admits no input at all, so the final
// assembly refuses instead of silently sending over the window.
func (p requestBudgetPolicy) inputBudget() int {
	if p.providerWindow <= 0 {
		return 0
	}
	if p.outputReserve >= p.providerWindow {
		return 0
	}
	return p.providerWindow - p.outputReserve
}

// boundaryDynamicPart is one face of the dynamic section: the source word the
// request budget reads and the body exactly as it renders.
type boundaryDynamicPart struct {
	source string
	body   string
}

// joinBoundaryDynamic renders the dynamic section from the parts still
// admitted, preserving the byte shape the section had when every part was
// present: each part carries its own trailing blank line, and the section the
// loop appends loses exactly the one final newline.
func joinBoundaryDynamic(parts []boundaryDynamicPart) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part.body)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// dynamicParts reports the dynamic section as the faces it carries. A view
// assembled before the boundary split the section (a test face, or a boundary
// that filled only DynamicSystem) reports the whole section as the environment
// face so the budget still accounts for it; it cannot be split further.
func (v BoundaryView) dynamicParts() []boundaryDynamicPart {
	if len(v.dynamic) > 0 {
		return append([]boundaryDynamicPart(nil), v.dynamic...)
	}
	if v.DynamicSystem != "" {
		return []boundaryDynamicPart{{source: BudgetFaceEnvironment, body: v.DynamicSystem}}
	}
	return nil
}

// RequestFace assembles the request-boundary material of one decision: the
// fixed file contents read fresh at this boundary, the skill bodies already
// loaded on the binding beside them and the replayed projection of the named
// session chain. The prefix follows one fixed layout: the fixed file contents
// first — the prompt and skill bodies the binding names, in the structural
// order the binding carries — then the skill bodies already loaded on the
// binding's tool resources beside them (skill-name order, once each), then
// the replayed projection of the session chain (each presented entry under
// the frozen event-type→role table), then the transcript the drive
// accumulates behind the prefix. An agent declaring the environment block
// receives the block itself, and the loop appends it as the last message
// behind the transcript, so the dynamic material trails the request instead
// of entering the prefix. The boundary material enters the actual
// GenerateContent request at the loop, and no projection entry shape is
// mirrored into this package: the face crosses built-in, event, model and
// state types only, so the composition root attaches it with no package edge
// into the projection layer, the same stitching the recording face uses. A
// chain that names no session is refused by name through
// ErrRequestChainSessionRequired; a nil face keeps the request at its
// transcript alone.
// The redline list (3 rules):
// MUST assemble the prefix in the fixed layout from contents read fresh at this boundary.
// MUST refuse a chain that names no session by name.
// MUST NOT cross any package edge into the projection layer.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
type RequestFace func(ctx context.Context, boundary RequestBoundary) (BoundaryView, error)

// ErrProjectionTypeUnmapped reports a projection entry whose frozen type the
// role table does not declare: the table routes the declared types onto roles
// deterministically — every declared type names exactly one role, and several
// types may share one role — so a type outside that table is refused by name
// instead of rendering under a role no declaration gave it.
var ErrProjectionTypeUnmapped = errors.New("agent: the projection type maps to no message role")

// projectionTypeRecoveryHint, projectionTypeSegmentFold and
// projectionTypeHistorySummary are the projection product words the replay layer
// mints for the material it renders itself: a recovery hint answering a budget
// cut, the folded process detail of the newest band, and the history summary of
// the older bands. They name presented entries of the projection, never a frozen
// event type, and they are spelled here as the frozen words they are so this
// package stays clear of the projection layer.
const (
	projectionTypeRecoveryHint   = "recovery_hint"
	projectionTypeSegmentFold    = "segment_fold"
	projectionTypeHistorySummary = "history_summary"
	// projectionTypeDeliverySettlement is the frozen event type of the delivery
	// settlement fact family (F13). It is spelled here as the frozen word it is
	// so this package stays clear of the composition root: the family is defined
	// by assembly/host_delivery.go (deliverySettlementType), and the two spellings
	// must stay byte-identical or a settlement fact is refused by the projection
	// instead of presented. Only the settlement type is mapped for the family:
	// the owed declaration rides on the agent_output fact itself and takes the
	// assistant role that fact already has, so it has no visible face of its own.
	projectionTypeDeliverySettlement = "delivery.settlement"
)

// budgetPrefix reports the prefix messages the final request keeps and the
// source face each kept message renders, in the view's fixed layout order. A
// view names no source for a message (or names a face the meter does not know)
// keeps that message on the history face, so an unannotated boundary is
// accounted as transcript material rather than dropped.
func budgetPrefix(view BoundaryView, dropSkills bool) ([]model.Message, []string) {
	messages := make([]model.Message, 0, len(view.Messages))
	sources := make([]string, 0, len(view.Messages))
	for i, message := range view.Messages {
		source := BudgetFaceHistory
		if i < len(view.Sources) && view.Sources[i] != "" && knownBudgetFace(view.Sources[i]) {
			source = view.Sources[i]
		}
		if dropSkills && source == BudgetFaceSkillBodies {
			continue
		}
		messages = append(messages, message)
		sources = append(sources, source)
	}
	return messages, sources
}

// budgetDynamic reports the dynamic parts the final request keeps: the
// environment block yields before any other face, and the skill bodies yield
// next, so the declared prompt and the conversation survive longest.
func budgetDynamic(view BoundaryView, dropEnvironment, dropSkills bool) []boundaryDynamicPart {
	parts := view.dynamicParts()
	kept := make([]boundaryDynamicPart, 0, len(parts))
	for _, part := range parts {
		if dropEnvironment && part.source == BudgetFaceEnvironment {
			continue
		}
		if dropSkills && part.source == BudgetFaceSkillBodies {
			continue
		}
		kept = append(kept, part)
	}
	return kept
}

// accountRequestCandidate measures the final request one assembly candidate
// would send: the prefix it keeps, the transcript, the closing prompt of a final
// round and the dynamic parts. The accounting builds one message per dynamic
// part, while the wire joins them into one system message, so the estimate is
// conservative for exactly the material a drop can still save.
func accountRequestCandidate(prefix []model.Message, sources []string, transcript []model.Message, round decisionRound, dynamic []boundaryDynamicPart, tools map[string]trpctool.Tool) (map[string]int, []string, int) {
	messages := make([]model.Message, 0, len(prefix)+len(transcript)+1+len(dynamic))
	faces := make([]string, 0, cap(messages))
	messages = append(messages, prefix...)
	faces = append(faces, sources...)
	messages = append(messages, transcript...)
	for range transcript {
		faces = append(faces, BudgetFaceHistory)
	}
	if round.final {
		messages = append(messages, model.Message{Role: model.RoleUser, Content: round.prompt})
		faces = append(faces, BudgetFaceHistory)
	}
	for _, part := range dynamic {
		messages = append(messages, model.Message{Role: model.RoleSystem, Content: part.body})
		faces = append(faces, part.source)
	}
	return AccountRequestBudget(messages, faces, tools, "")
}

// finalRequestMessages renders the wire request one candidate sends: the kept
// prefix, the transcript, the closing prompt of a final round and the dynamic
// parts joined into the one trailing system message the boundary documents.
func finalRequestMessages(prefix []model.Message, transcript []model.Message, round decisionRound, dynamic []boundaryDynamicPart) []model.Message {
	messages := make([]model.Message, 0, len(prefix)+len(transcript)+1+len(dynamic))
	messages = append(messages, prefix...)
	messages = append(messages, transcript...)
	if round.final {
		messages = append(messages, model.Message{Role: model.RoleUser, Content: round.prompt})
	}
	if body := joinBoundaryDynamic(dynamic); body != "" {
		messages = append(messages, model.Message{Role: model.RoleSystem, Content: body})
	}
	return messages
}

// hasDynamicFace reports whether one dynamic section carries a named face.
func hasDynamicFace(parts []boundaryDynamicPart, face string) bool {
	for _, part := range parts {
		if part.source == face {
			return true
		}
	}
	return false
}

// BudgetFaceFixedPrompt and the other BudgetFace constants are the nine input
// faces the final request accounts for. Every face is a bucket of the one meter;
// the total is their sum, so a material the meter cannot measure still reports
// its face instead of folding into another.
const (
	// BudgetFaceFixedPrompt is the declared prompt the binding's prompt files
	// fix into the prefix.
	BudgetFaceFixedPrompt = "fixed_prompt"
	// BudgetFaceSkillBodies is the skill bodies the boundary fixes into the
	// prefix beside the bodies a load publishes into the dynamic section.
	BudgetFaceSkillBodies = "skill_bodies"
	// BudgetFaceEnvironment is the environment block a declaration closes every
	// request with.
	BudgetFaceEnvironment = "environment"
	// BudgetFaceHistory is the replayed projection and the transcript.
	BudgetFaceHistory = "history"
	// BudgetFaceToolDeclarations is the tools the request admits, their names,
	// descriptions and schemas, beside the tool calls it replays.
	BudgetFaceToolDeclarations = "tool_declarations"
	// BudgetFaceReasoning is the reasoning text and signature one message
	// carries.
	BudgetFaceReasoning = "reasoning"
	// BudgetFaceContentParts is the text content parts of a message.
	BudgetFaceContentParts = "content_parts"
	// BudgetFaceTaskBoard is the task board material the request carries.
	BudgetFaceTaskBoard = "task_board"
	// BudgetFaceRecoveryHint is the recovery hint the replay projection presents
	// when a fold cut its material.
	BudgetFaceRecoveryHint = "recovery_hint"
)

// DefaultOutputReserveDivisor is the fixed ratio of the provider context window
// a declaration that names no output reserve leaves for the model's answer. The
// input budget is the window minus that share; the constant is frozen here
// because a silent change of the default would move every undeclared budget.
const DefaultOutputReserveDivisor = 4

// budgetMessageOverheadTokens is the per-message framing the provider charges
// beyond the material itself: the role markers and separators around a message.
// It is the one home of that framing: the approximate prompt measure reads it at
// the replay budget's packing ratio (see messageOverheadBytes), so the two
// meters cannot hold separate copies of the figure.
const budgetMessageOverheadTokens = 4

// budgetToolCallOverheadTokens is the per-call framing a replayed assistant
// tool call carries beyond its identifier and arguments. The approximate prompt
// measure charges no per-call framing: it counts the call's own bytes alone, so
// on a request that carries tool calls this meter reads above that one and the
// budget side is the conservative of the two.
const budgetToolCallOverheadTokens = 5

// ErrRequestBudgetExceeded is the refusal the final assembly returns when the
// declared input budget cannot carry the request in any admissible form: the
// environment block, the skill bodies and the projection have all yielded and
// the system-required material still does not fit. The refusal travels the
// loop's existing failure path (a failed terminal and the host's existing
// failure notice), never a new output channel.
var ErrRequestBudgetExceeded = errors.New("agent: the request exceeds the declared input budget")

// OutputReserveOf reports the default output allowance of one provider context
// window: the fixed share of the window the declaration leaves for the model's
// answer. A window of zero declares no budget and both sides report zero; a
// window that divides below one token still reserves the one token it can, so
// the input budget never silently swallows the whole window.
func OutputReserveOf(providerWindow int) int {
	if providerWindow <= 0 {
		return 0
	}
	return (providerWindow + DefaultOutputReserveDivisor - 1) / DefaultOutputReserveDivisor
}

// AccountRequestBudget measures one final request's input material and answers
// the tokens each of the nine input faces takes, the media it could not measure
// and the total. The total is the sum of the nine faces; a media the meter
// cannot measure (an image, audio, video or file part) is named in unknown and
// counted nowhere, so the total is a floor the caller must never read as a
// proven fit. The source slice runs parallel to messages: a message with no
// source, or a source that names no face, renders under the history face.
// tools are accounted once each in deterministic name order, and taskBoard
// accounts the task-board material the request carries.
func AccountRequestBudget(messages []model.Message, sources []string, tools map[string]trpctool.Tool, taskBoard string) (map[string]int, []string, int) {
	faces := make(map[string]int, 9)
	for _, face := range []string{
		BudgetFaceFixedPrompt, BudgetFaceSkillBodies, BudgetFaceEnvironment, BudgetFaceHistory,
		BudgetFaceToolDeclarations, BudgetFaceReasoning, BudgetFaceContentParts,
		BudgetFaceTaskBoard, BudgetFaceRecoveryHint,
	} {
		faces[face] = 0
	}
	var unknown []string
	for i, message := range messages {
		face := BudgetFaceHistory
		if i < len(sources) && sources[i] != "" {
			if knownBudgetFace(sources[i]) {
				face = sources[i]
			}
		}
		base, reasoning, parts, calls := budgetMessageTokens(message, &unknown)
		faces[face] += base
		faces[BudgetFaceReasoning] += reasoning
		faces[BudgetFaceContentParts] += parts
		faces[BudgetFaceToolDeclarations] += calls
	}
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tool := tools[name]
		if tool == nil {
			continue
		}
		declaration := tool.Declaration()
		if declaration == nil {
			continue
		}
		faces[BudgetFaceToolDeclarations] += textTokens(declaration.Name) + textTokens(declaration.Description)
		for _, schema := range []*trpctool.Schema{declaration.InputSchema, declaration.OutputSchema} {
			if schema == nil {
				continue
			}
			raw, err := json.Marshal(schema)
			if err != nil {
				unknown = append(unknown, "tool "+name+" schema: "+err.Error())
				continue
			}
			faces[BudgetFaceToolDeclarations] += textTokens(string(raw))
		}
	}
	faces[BudgetFaceTaskBoard] += textTokens(taskBoard)
	total := 0
	for _, tokens := range faces {
		total += tokens
	}
	return faces, unknown, total
}

// budgetMessageTokens measures one message: the face its content belongs to,
// beside the reasoning, content-part and tool-call tokens that have faces of
// their own. Every message carries the framing overhead even when its content
// is empty, because the wire still carries the message.
func budgetMessageTokens(message model.Message, unknown *[]string) (base, reasoning, parts, calls int) {
	base = budgetMessageOverheadTokens + textTokens(message.Content) + textTokens(message.ToolID) + textTokens(message.ToolName)
	reasoning = textTokens(message.ReasoningContent) + textTokens(message.ReasoningSignature)
	for _, part := range message.ContentParts {
		if string(part.Type) == "text" {
			if part.Text == nil {
				*unknown = append(*unknown, "content part text without text")
				continue
			}
			parts += textTokens(*part.Text)
			continue
		}
		*unknown = append(*unknown, "content part "+string(part.Type))
	}
	for _, call := range message.ToolCalls {
		calls += budgetToolCallOverheadTokens + textTokens(call.ID) + textTokens(call.Function.Name) + textTokens(string(call.Function.Arguments))
	}
	return base, reasoning, parts, calls
}

// textTokens measures one text at the packing ratio the replay budget packs
// under. The measure rounds up, so a text that takes one byte still takes one
// token and no non-empty material is accounted as zero.
func textTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + replay.BudgetCharsPerToken - 1) / replay.BudgetCharsPerToken
}

// knownBudgetFace reports whether the word names one of the nine faces.
func knownBudgetFace(face string) bool {
	switch face {
	case BudgetFaceFixedPrompt, BudgetFaceSkillBodies, BudgetFaceEnvironment, BudgetFaceHistory,
		BudgetFaceToolDeclarations, BudgetFaceReasoning, BudgetFaceContentParts,
		BudgetFaceTaskBoard, BudgetFaceRecoveryHint:
		return true
	default:
		return false
	}
}

// RoleOf reports the native message role one presented projection entry renders
// as on the replay path, keeping the transcript shape the event loop speaks. The
// table is the whole of the routing and it is deterministic: every declared entry
// type names exactly one role, several types may name the same role, and a type
// the table does not declare is refused by ErrProjectionTypeUnmapped rather than
// rendered under a fallback role. The table routes what one consumption reads and
// binds the reading side only: the core logic of a consumption may mint several
// events from the one projection it read — the calls it admits beside the answer it
// gives — and those products owe no traceability, because a new fact is its own
// source.
//
// The settled result and the replay products each render under the one role their
// entry type declares:
// - a replayed settled fact is history replayed as it stood, so it renders under the tool role its result fact answered a call with
// - the live settled observation a dispatch feeds the next decision is input of the range, and it renders under its call identity (see observationOf)
// - the recovery hint, the folded segment and the history summary render as user input material, the history a range reads
func RoleOf(eventType string) (model.Role, error) {
	switch eventType {
	case event.TypeExternalInput, event.TypeContextCompress, event.TypeFeedback, event.TypeGovernance,
		projectionTypeRecoveryHint, projectionTypeSegmentFold, projectionTypeHistorySummary,
		projectionTypeDeliverySettlement:
		return model.RoleUser, nil
	case event.TypeThinkingPlan, event.TypeAgentOutput:
		return model.RoleAssistant, nil
	case event.TypeActionCommand, event.TypeTaskSettled:
		return model.RoleTool, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrProjectionTypeUnmapped, eventType)
	}
}
