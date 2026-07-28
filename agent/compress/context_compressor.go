package compress

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// CompressResult is the output of ContextCompressor.Compress.
type CompressResult struct {
	// Messages is the resolved (and possibly compressed) message list from the projection.
	// Does NOT include system prompt or current-turn messages — those are
	// prepended/appended by the BeforeModel callback.
	Messages []model.Message
	// RetainedRefs is the updated list of EventReferences to replace
	// the projection with after compression.
	RetainedRefs []memory.EventReference
	// Notices contains error/degradation notices injected during compression.
	Notices []model.Message
}

// ContextCompressor is the projection-only compression engine.
//
// It reads EventReferences from the SessionProjection, resolves them to
// messages via MemoryStore, checks token budget, and applies value-driven
// L0-L3 compression when over budget. Returns:
//   - Resolved/compressed messages (the historical timeline)
//   - Retained refs to update the projection
//   - Error notices for engineering awareness
//
// Design principle: Projection is the SINGLE source of truth for the
// historical timeline. ContextCompressor does NOT reconcile against
// framework ContentRequestProcessor output — there is no content-based
// deduplication. The BeforeModel callback handles merging the compressed
// history with current-turn messages.
type ContextCompressor struct {
	compressor   *SmartCompressor // Reuses L0-L3 / value-driven strategy
	memStore     memory.MemoryStore
	tokenCounter TokenCounter
	maxTokens    int
	thresholdPct float64
	keepRecent   int

	// recentFullCount is the number of most recent refs to resolve with
	// full content from MemoryStore. Older refs use EventSummary. When not
	// explicitly configured it derives from keepRecent × DefaultRefsPerTurn
	// so the most recent keepRecent complete turns resolve full as a whole
	// (D6).
	recentFullCount int

	// listedKeysCap bounds the keys listed in the rolling compaction summary
	// (default DefaultCompactKeysListed; see WithCompactKeysListed).
	listedKeysCap int

	// cardMaxChars bounds the index-card section of the rolling summary
	// (default DefaultCardMaxChars; see WithCardMaxChars). When exceeded and a
	// summary model is available, old card lines are LLM-condensed; otherwise
	// the oldest lines sink into an "earlier n items" counter (never breaks).
	cardMaxChars int

	// meditationKeys marks agent_output events produced by meditation turns so
	// their card lines get the ★ highlight (long-term reflection anchors).
	// Written from the consumer goroutine, read at BeforeModel — mutex guarded.
	meditationMu   sync.Mutex
	meditationKeys map[int64]bool
}

// ContextCompressorOption configures optional ContextCompressor constraints.
type ContextCompressorOption func(*ContextCompressor)

// WithCompactKeysListed caps the number of keys listed in the rolling
// compaction summary (default DefaultCompactKeysListed).
func WithCompactKeysListed(n int) ContextCompressorOption {
	return func(cc *ContextCompressor) {
		if n > 0 {
			cc.listedKeysCap = n
		}
	}
}

// WithRecentFullCount sets how many most-recent refs resolve with full
// content from MemoryStore, overriding the derived default
// (keepRecent × DefaultRefsPerTurn, see D6).
func WithRecentFullCount(n int) ContextCompressorOption {
	return func(cc *ContextCompressor) {
		if n > 0 {
			cc.recentFullCount = n
		}
	}
}

// WithCardMaxChars caps the index-card section length in the rolling summary
// (default DefaultCardMaxChars).
func WithCardMaxChars(n int) ContextCompressorOption {
	return func(cc *ContextCompressor) {
		if n > 0 {
			cc.cardMaxChars = n
		}
	}
}

// MarkMeditationKey records that the given event key is a meditation-turn
// output; its index card line will carry the ★ highlight.
func (cc *ContextCompressor) MarkMeditationKey(key int64) {
	if cc == nil || key == 0 {
		return
	}
	cc.meditationMu.Lock()
	defer cc.meditationMu.Unlock()
	if cc.meditationKeys == nil {
		cc.meditationKeys = make(map[int64]bool)
	}
	cc.meditationKeys[key] = true
}

func (cc *ContextCompressor) isMeditationKey(key int64) bool {
	cc.meditationMu.Lock()
	defer cc.meditationMu.Unlock()
	return cc.meditationKeys[key]
}

// NewContextCompressor creates a ContextCompressor from a SmartCompressor.
// The SmartCompressor provides the L0-L3 compression strategy; ContextCompressor
// adds the ref-resolution and projection-management layer on top.
func NewContextCompressor(
	sc *SmartCompressor,
	memStore memory.MemoryStore,
	tokenCounter TokenCounter,
	maxTokens int,
	thresholdPct float64,
	keepRecent int,
	opts ...ContextCompressorOption,
) *ContextCompressor {
	if tokenCounter == nil {
		tokenCounter = NewDefaultTokenCounter()
	}
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	if thresholdPct <= 0 {
		thresholdPct = DefaultCompressThreshold
	}
	if keepRecent <= 0 {
		keepRecent = 2
	}
	cc := &ContextCompressor{
		compressor:    sc,
		memStore:      memStore,
		tokenCounter:  tokenCounter,
		maxTokens:     maxTokens,
		thresholdPct:  thresholdPct,
		keepRecent:    keepRecent,
		listedKeysCap: DefaultCompactKeysListed,
		cardMaxChars:  DefaultCardMaxChars,
	}
	for _, opt := range opts {
		opt(cc)
	}
	// D6: unless explicitly configured, the full-resolution window covers the
	// most recent keepRecent complete turns as a whole — a fixed small count
	// would push the second-newest L0 turn into the summary-only zone and
	// demote its action_command results, weakening the L0 full-fidelity
	// semantics.
	if cc.recentFullCount <= 0 {
		cc.recentFullCount = keepRecent * DefaultRefsPerTurn
	}
	return cc
}

// Compress resolves all projection refs into messages, checks token budget,
// and compresses if over threshold.
//
// Input:
//   - ctx: context for LLM calls (used by SmartCompressor)
//   - refs: EventReferences from SessionProjection (the historical timeline)
//
// Output:
//   - Messages: resolved (and possibly compressed) message list
//   - RetainedRefs: updated refs (replaces projection)
//   - Notices: error/degradation notices
//
// The returned Messages do NOT include a system prompt — the caller
// (BeforeModel callback) prepends system prompt and appends current-turn
// messages after calling Compress.
func (cc *ContextCompressor) Compress(
	ctx context.Context,
	refs []memory.EventReference,
) CompressResult {
	startTime := time.Now()

	if len(refs) == 0 {
		return CompressResult{
			Messages:     nil,
			RetainedRefs: nil,
		}
	}

	// Resolve ALL refs from the projection into NATIVE timeline messages
	// (D3 v2): assistant tool_calls and role=tool results keep protocol form —
	// the model sees exactly its training distribution, leaving no textual
	// call syntax to imitate. Pairing legality has TWO layers of ownership:
	//   1. RENDER time (here): repairs pre-existing dangles in the projection
	//      — results whose call is already gone (compacted in an earlier
	//      round, or id lost) are demoted to user-side input notes, and
	//      calls whose result is already gone are stripped below.
	//   2. COMPRESS time (applySegmentLevel L1): when THIS round drops a
	//      segment's action_command results, it strips the matching
	//      tool_calls in the same pass — the output of one compression is
	//      legal on its own.
	// Content is preserved in both layers, so ANY compression window cut
	// stays legal without the compressor being pairing-aware globally.
	declared := make(map[string]bool)
	consumed := make(map[string]bool)
	resolved := make([]model.Message, 0, len(refs))
	// Only the most recent recentFullCount refs resolve with full content
	// from MemoryStore; older refs render from their EventSummary. This
	// bounds each BeforeModel's store-query volume to O(recentFullCount)
	// instead of O(refs) (task-skeleton-compression, performance item).
	fullFrom := len(refs) - cc.recentFullCount
	for i, ref := range refs {
		msg := cc.resolveRef(ctx, ref, i >= fullFrom)
		switch msg.Role {
		case model.RoleAssistant:
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					declared[tc.ID] = true
				}
			}
		case model.RoleTool:
			if msg.ToolID == "" || !declared[msg.ToolID] || consumed[msg.ToolID] {
				msg = demoteToInputNote(msg)
			} else {
				consumed[msg.ToolID] = true
			}
		}
		resolved = append(resolved, msg)
	}
	// Render-time legality for CALLS (layer 1, symmetric with
	// demoteToInputNote): strip tool_calls whose result is not in the
	// rendered sequence — the result ref was dropped by an EARLIER round's
	// L1, or resolved summary-only past recentFullCount. Without this,
	// stale unanswered calls would be re-sent every round. The prose
	// content (with its event prefix) is preserved. This-round drops are
	// layer 2's job (applySegmentLevel L1 strips calls alongside results).
	for i := range resolved {
		msg := &resolved[i]
		if msg.Role != model.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		var kept []model.ToolCall
		for _, tc := range msg.ToolCalls {
			if tc.ID != "" && consumed[tc.ID] {
				kept = append(kept, tc)
			}
		}
		msg.ToolCalls = kept
	}

	usedTokens := cc.tokenCounter.Estimate(resolved)
	threshold := int(float64(cc.maxTokens) * cc.thresholdPct)

	if usedTokens <= threshold {
		log.Infof("[ContextCompressor] under budget (%d <= %d), %d refs, %d messages",
			usedTokens, threshold, len(refs), len(resolved))
		return CompressResult{
			Messages:     resolved,
			RetainedRefs: refs,
		}
	}

	// Over threshold — compress via SmartCompressor.
	log.Infof("[ContextCompressor] over budget (%d > %d), compressing %d messages from %d refs",
		usedTokens, threshold, len(resolved), len(refs))

	originalKeepRecent := cc.compressor.KeepRecentTasks
	defer func() { cc.compressor.KeepRecentTasks = originalKeepRecent }()
	cc.compressor.KeepRecentTasks = cc.keepRecent

	compressedMsgs := cc.compressor.Compress(ctx, resolved, nil)
	newTokens := cc.tokenCounter.Estimate(compressedMsgs)
	log.Infof("[ContextCompressor] SmartCompress: %d -> %d tokens (threshold=%d)",
		usedTokens, newTokens, threshold)

	// Build retained refs.
	retainedRefs := cc.buildRetainedRefs(refs, compressedMsgs, ctx)

	// Collect error notices.
	var notices []model.Message
	for _, msg := range compressedMsgs {
		if strings.Contains(msg.Content, "[context_compress_error]") {
			notices = append(notices, msg)
		}
	}

	log.Infof("[ContextCompressor] refs=%d -> retained=%d, messages=%d, duration=%dms",
		len(refs), len(retainedRefs), len(compressedMsgs), time.Since(startTime).Milliseconds())

	return CompressResult{
		Messages:     compressedMsgs,
		RetainedRefs: retainedRefs,
		Notices:      notices,
	}
}

// resolveRef resolves a single EventReference to a native timeline message.
// When full is true the content comes from MemoryStore; otherwise (older
// refs beyond recentFullCount) the reference's EventSummary is used directly
// — no store query. The result is tagged with [evt_KEY|type] so
// SmartCompressor and buildRetainedRefs can track retained refs.
func (cc *ContextCompressor) resolveRef(
	ctx context.Context,
	ref memory.EventReference,
	full bool,
) model.Message {
	// context_compress refs are summary references — rendered as a USER-side
	// archival note (observation input), never role=system/assistant:
	//   - not system: the summary paraphrases user/tool content; system role
	//     would elevate paraphrased external text to instruction authority
	//     (prompt-injection amplifier) and sits outside training distribution
	//     (mid-conversation system messages behave inconsistently across models)
	//   - not assistant: the LLM never said this; any system-generated format
	//     placed in assistant history becomes an imitation template
	// Forgery is harmless by construction: real archive refs live in the
	// projection (negative EventKey, metadata channel) — imitated text parses
	// into nothing.
	if ref.EventType == tagentevent.TypeContextCompress {
		return model.Message{
			Role:    model.RoleUser,
			Content: prefixEventKey("〔历史归档〕系统生成的压缩摘要（非用户发言，勿模仿此格式）："+ref.EventSummary, ref),
		}
	}

	content := ""
	var toolCalls []model.ToolCall
	toolID := ""
	resolved := false
	if full && cc.memStore != nil && ref.EventKey > 0 {
		evt, err := cc.memStore.GetEvent(ref.EventKey)
		if err == nil && evt != nil {
			toolCalls = evt.ToolCalls
			toolID = evt.ToolID
			switch {
			// FullEvent.Content is the authoritative (sanitized-at-storage) text;
			// prefer it over the raw Response message.
			case evt.Content != "" || len(evt.ToolCalls) > 0:
				content = evt.Content
				resolved = true
			case evt.Response != nil && len(evt.Response.Choices) > 0:
				m := evt.Response.Choices[0].Message
				content = m.Content
				if len(m.ToolCalls) > 0 {
					toolCalls = m.ToolCalls
				}
				if toolID == "" {
					toolID = m.ToolID
				}
				resolved = true
			case evt.EventSummary != "":
				content = evt.EventSummary
				resolved = true
			}
		}
	}
	if !resolved {
		content = ref.EventSummary
		if content == "" {
			content = "(历史事件摘要为空，可用 recall 检索)"
		}
	}
	return renderTimelineMessage(ref, content, toolCalls, toolID)
}

// renderTimelineMessage renders one event in NATIVE protocol form (D3 v2):
//   - thinking_plan → role=assistant with native ToolCalls restored from the
//     stored event; content is prose only — the system NEVER generates textual
//     call syntax into assistant history (any such syntax is imitable and
//     leads models to fabricate tool calls in plain text)
//   - action_command → role=tool with its ToolID (pairing legality against
//     the rendered sequence is enforced by the caller, which demotes orphans
//     via demoteToInputNote)
//   - notifications and everything else → eventTypeToRole text
func renderTimelineMessage(
	ref memory.EventReference,
	content string,
	toolCalls []model.ToolCall,
	toolID string,
) model.Message {
	switch ref.EventType {
	case tagentevent.TypeThinkingPlan:
		return model.Message{
			Role:      model.RoleAssistant,
			Content:   prefixEventKey(content, ref),
			ToolCalls: toolCalls,
		}
	case tagentevent.TypeActionCommand:
		return model.Message{
			Role:    model.RoleTool,
			ToolID:  toolID,
			Content: prefixEventKey(content, ref),
		}
	default:
		return model.Message{
			Role:    EventTypeToRole(ref.EventType),
			Content: prefixEventKey(content, ref),
		}
	}
}

// demoteToInputNote converts a tool-result message that cannot legally pair
// (id lost, call compacted away, or duplicate answer) into a user-side input
// note. Content and the correlation id are preserved; the sequence stays a
// legal native conversation. This is a narrow render-time edge rule, not a
// load-bearing repair layer.
func demoteToInputNote(msg model.Message) model.Message {
	note := msg.Content
	if msg.ToolID != "" {
		note = fmt.Sprintf("〔工具结果 tool_id=%s（其调用已被压缩）〕%s", msg.ToolID, msg.Content)
	}
	return model.Message{
		Role:    model.RoleUser,
		Content: note,
	}
}

// prefixEventKey prepends "[evt_KEY|type]" to content when the reference has a
// valid key and the content is not already prefixed. This prefix is the
// lightweight metadata channel that lets SmartCompressor and buildRetainedRefs
// track which projection refs survive compression.
func prefixEventKey(content string, ref memory.EventReference) string {
	if ref.EventKey == 0 || tagentevent.HasEventPrefix(content) {
		return content
	}
	eventType := ref.EventType
	if eventType == "" {
		eventType = "unknown"
	}
	return tagentevent.FormatEventPrefix(ref.EventKey, eventType) + " " + content
}

// buildRetainedRefs determines which EventReferences should be kept in the
// projection after compression.
//
// Strategy:
//   - Refs whose event keys appear in the compressed messages → retained.
//   - Refs whose event keys are NOT in the compressed messages → were compressed.
//     These are replaced with a single ROLLING summary ref.
//   - A prior summary ref (negative key) is absorbed into the new summary
//     (count + time lower bound carry over) — never silently dropped.

// maxListedCompactKeys — see WithCompactKeysListed / DefaultCompactKeysListed.
// Without a cap the summary line grows without bound across a long-running
// session (each key is ~17 hex chars; hundreds of compacted events would make
// this single message kilobytes large). Older keys drop off the list but stay
// retrievable via recall (time/semantic queries); the rolling total keeps the
// count honest.

// compactedCountRe extracts the rolling total from a prior summary reference
// (single-point format: written and parsed only here). LINE-ANCHORED: card
// lines carry user-controlled text (external_input summaries) — an unanchored
// match would let crafted input inflate the rolling count (injection surface).
// Card lines always start with "- ", so the anchor is sufficient.
var compactedCountRe = regexp.MustCompile(`(?m)^\[Compacted (\d+) historical events`)

// earlierItemsRe extracts the sunk-items counter from a prior summary.
// Line-anchored for the same injection-hardening reason as compactedCountRe.
var earlierItemsRe = regexp.MustCompile(`(?m)^\(earlier (\d+) items retrievable via memory_recall\)$`)

// cardTimeLayout renders card-line timestamps compactly.
const cardTimeLayout = "01-02 15:04"

// extractCardLine builds ONE index-card line for a compressed ref, or "" if
// the event is not a task-skeleton node. Engineering extraction, zero LLM:
// only boundary events (external_input / agent_output) become cards — tool
// steps are represented by their task's card. Meditation outputs get the ★
// highlight (long-term reflection anchors).
func (cc *ContextCompressor) extractCardLine(ref memory.EventReference) string {
	if ref.EventType != tagentevent.TypeExternalInput && ref.EventType != tagentevent.TypeAgentOutput {
		return ""
	}
	summary := ref.EventSummary
	if cc.memStore != nil && summary == "" {
		if full, err := cc.memStore.GetEvent(ref.EventKey); err == nil && full != nil {
			summary = full.EventSummary
			if summary == "" {
				summary = full.Content
			}
		}
	}
	if idx := strings.IndexByte(summary, '\n'); idx >= 0 {
		summary = summary[:idx]
	}
	summary = strings.TrimSpace(tagentevent.StripEventKeyPrefix(summary))
	if len(summary) > 80 {
		summary = truncateString(summary, 80)
	}
	if summary == "" {
		return ""
	}
	star := ""
	if cc.isMeditationKey(ref.EventKey) {
		star = "★ "
	}
	ts := ""
	if ref.Timestamp > 0 {
		ts = time.UnixMilli(ref.Timestamp).Format(cardTimeLayout) + " "
	}
	return fmt.Sprintf("- %s%s[%s] %s", star, ts, tagentevent.FormatEventKey(ref.EventKey), summary)
}

// parseCardSection splits a prior rolling summary into its card lines and
// sunk-items counter (single-point format, self-produced self-consumed).
func parseCardSection(summary string) (cards []string, earlier int) {
	for _, line := range strings.Split(summary, "\n") {
		if strings.HasPrefix(line, "- ") {
			cards = append(cards, line)
		}
	}
	if m := earlierItemsRe.FindStringSubmatch(summary); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			earlier = n
		}
	}
	return cards, earlier
}

// curateCards enforces the card-section bound: when the joined lines exceed
// cardMaxChars, OLD lines are LLM-condensed (material law: input = card
// lines, layer-2 artifacts); without a model or on failure the oldest lines
// SINK into the earlier-items counter (engineering fallback, never breaks).
func (cc *ContextCompressor) curateCards(ctx context.Context, cards []string, earlier int) ([]string, int) {
	joined := strings.Join(cards, "\n")
	if cc.cardMaxChars <= 0 || len(joined) <= cc.cardMaxChars {
		return cards, earlier
	}
	// Try LLM condensation of the OLDER half (keep the newest lines verbatim).
	half := len(cards) / 2
	if half > 0 && cc.compressor != nil && cc.compressor.summaryModel != nil {
		condensed, err := cc.condenseCardLines(ctx, cards[:half])
		// Single-line scrub: the card section is parsed by "- "-prefixed
		// lines; a multi-line LLM output would have its continuation lines
		// silently dropped next round (or split into phantom cards).
		condensed = strings.Join(strings.Fields(condensed), " ")
		if err == nil && condensed != "" {
			newCards := append([]string{"- " + condensed}, cards[half:]...)
			if len(strings.Join(newCards, "\n")) <= cc.cardMaxChars {
				return newCards, earlier
			}
			cards = newCards // condensed but still over — fall through to sinking
		} else if err != nil {
			log.Warnf("[ContextCompressor] card condensation failed (sinking instead): %v", err)
		}
	}
	// Engineering fallback: sink oldest lines until under the cap.
	for len(cards) > 1 && len(strings.Join(cards, "\n")) > cc.cardMaxChars {
		cards = cards[1:]
		earlier++
	}
	return cards, earlier
}

// condenseCardLines asks the summary model to condense old card lines into a
// single compact card, preserving the task skeleton and key references so
// memory_recall tickets stay valid.
func (cc *ContextCompressor) condenseCardLines(ctx context.Context, lines []string) (string, error) {
	prompt := "将以下历史任务卡片浓缩为一行紧凑概述。硬性要求：保留任务骨架与时间跨度，保留方括号内的关键 key（至少保留首尾与重要节点的 [key]），★ 开头的高亮行为长期反思结论——其要点与 [key] 必须保留，不添加任何未出现的事实，不确定处省略。只输出浓缩后的一行，不要前缀。\n\n" + strings.Join(lines, "\n")
	return cc.compressor.generatePlainSummary(ctx, prompt)
}

func (cc *ContextCompressor) buildRetainedRefs(
	originalRefs []memory.EventReference,
	compressedMsgs []model.Message,
	ctx context.Context,
) []memory.EventReference {
	if len(originalRefs) == 0 {
		return nil
	}

	// Collect all event keys present in compressed messages.
	retainedKeys := make(map[int64]bool)
	for _, msg := range compressedMsgs {
		content := msg.Content
		for {
			key, _, remainder := tagentevent.ParseEventKeyAndType(content)
			if key <= 0 {
				break
			}
			retainedKeys[key] = true
			content = remainder
		}
	}

	// Build retained refs: keep refs whose keys are in compressed messages,
	// replace compressed refs with a single ROLLING summary ref. A prior
	// summary ref (negative key) is absorbed into the new one — its count and
	// time lower bound carry over — instead of being silently dropped (which
	// would sever the timeline's entry point to earlier compacted history).
	var retained []memory.EventReference
	var compressedKeys []string
	var newCards, oldCards []string
	var minTs int64
	priorCount := 0
	earlier := 0

	for _, ref := range originalRefs {
		if ref.EventKey == 0 {
			continue
		}
		if ref.EventKey < 0 && ref.EventType == tagentevent.TypeContextCompress {
			if m := compactedCountRe.FindStringSubmatch(ref.EventSummary); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil {
					priorCount += n
				}
			}
			cards, e := parseCardSection(ref.EventSummary)
			oldCards = append(oldCards, cards...)
			earlier += e
			if minTs == 0 || ref.Timestamp < minTs {
				minTs = ref.Timestamp
			}
			continue
		}
		if retainedKeys[ref.EventKey] {
			retained = append(retained, ref)
		} else if ref.EventKey > 0 {
			compressedKeys = append(compressedKeys, tagentevent.FormatEventKey(ref.EventKey))
			if card := cc.extractCardLine(ref); card != "" {
				newCards = append(newCards, card)
			}
			if minTs == 0 || ref.Timestamp < minTs {
				minTs = ref.Timestamp
			}
		}
	}

	// Emit the rolling summary whenever there is anything compacted — this
	// round or carried over from prior rounds. The summary carries the
	// INDEX-CARD SEQUENCE: engineering-extracted task skeleton lines whose
	// [hex] keys are recall tickets (memory_recall items).
	if total := priorCount + len(compressedKeys); total > 0 {
		if minTs == 0 {
			minTs = time.Now().UnixMilli()
		}
		cards, earlierOut := cc.curateCards(ctx, append(oldCards, newCards...), earlier)

		var b strings.Builder
		fmt.Fprintf(&b, "[Compacted %d historical events]", total)
		if len(cards) > 0 {
			b.WriteString("\n")
			b.WriteString(strings.Join(cards, "\n"))
		}
		if earlierOut > 0 {
			fmt.Fprintf(&b, "\n(earlier %d items retrievable via memory_recall)", earlierOut)
		}
		listed := compressedKeys
		if cap := cc.listedKeysCap; cap > 0 && len(listed) > cap {
			listed = listed[len(listed)-cap:]
		}
		if len(listed) > 0 {
			fmt.Fprintf(&b, "\nrecent keys=%s", strings.Join(listed, ","))
		}
		summaryRef := memory.EventReference{
			EventKey:     -minTs,
			EventType:    tagentevent.TypeContextCompress,
			EventSummary: b.String(),
			Timestamp:    minTs,
			Role:         "user",
		}
		retained = append([]memory.EventReference{summaryRef}, retained...)
	}

	return retained
}
