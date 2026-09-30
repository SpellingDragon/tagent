package compress

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

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
	// Compressed reports whether this round performed a real compaction
	// (budget-exceeded path). False for under-budget early returns and
	// degraded paths — only true results carry a persisted snapshot (D2,
	// tagent-compress-event-sourcing).
	Compressed bool
}

// ContextCompressor is the projection-only compression engine.
//
// It reads EventReferences from the SessionProjection, resolves them to
// messages via MemoryStore, checks token budget, and applies value-driven
// L0-L3 compression when over budget. Returns:
// - Resolved/compressed messages (the historical timeline)
// - Retained refs to update the projection
// - Error notices for engineering awareness
//
// Design principle: Projection is the SINGLE source of truth for the
// historical timeline. ContextCompressor does NOT reconcile against
// framework ContentRequestProcessor output — there is no content-based
// deduplication. The BeforeModel callback handles merging the compressed
// history with current-turn messages.
type ContextCompressor struct {
	compressor   *SmartCompressor
	memStore     memory.MemoryStore
	tokenCounter TokenCounter
	// maxTokens / keepRecent are the CONSTRUCTION values: after
	// the push face was deleted they are written exactly once, here, and only
	// consulted as the fallback when no hot source is installed. The live numbers
	// come from hotSource per boundary, so an in-flight turn can never observe a
	// half-applied hot rotation.
	maxTokens atomic.Int64
	// thresholdBits holds the construction threshold as atomic bits; 0 bits =
	// unset sentinel handled by currentThreshold(). Written only by
	// seedThreshold (constructor). The live threshold resolves through
	// liveNums: hot source when installed, these bits otherwise.
	thresholdBits atomic.Uint64
	keepRecent    atomic.Int64

	// hotSource is the  pull side:
	// when installed, every consumption boundary (BudgetLine/Threshold/
	// KeepRecentValue/Compress) resolves the FULL numeric group from it, and the
	// atomics above retire to the construction fallback. Push paths may still
	// write the atomics during the transition; a wired source wins per field, so
	// there is never a second authority on the read side.
	hotSource atomic.Pointer[func() HotNumbers]

	// recentFullCount is the full-window size ANCHORED at each compaction
	// round: the most recent recentFullCount
	// retained refs resolve full, and that window stays FROZEN between
	// compactions (append-only stable prefix). When not explicitly configured
	// it derives from keepRecent × DefaultRefsPerTurn so the most recent
	// keepRecent complete turns resolve full as a whole .
	recentFullCount int

	// fullBoundary anchors the full-render window at the last compaction
	// round (D3 render freeze): refs with EventKey >= fullBoundary resolve
	// full (window + newer appends, whose Snowflake keys are monotonic); older
	// refs stay frozen on their EventSummary render. Zero (never compacted, or
	// fewer retained refs than the window) keeps everything full. Written and
	// read only from Compress (single BeforeModel goroutine) — no lock.
	fullBoundary int64

	// budgetUnrepresentable counts single over-cap cards whose tickets-only
	// form still exceeded card_max_chars (6.3 observability; monotone).
	budgetUnrepresentable atomic.Int64

	// condensedTicketsLost counts recall tickets that card condensation folded
	// into prose (cold-eyes W-3 observability; the guard only forces
	// head/tail/★ survival, every further drop is a navigation-address loss —
	// legitimate compression, but never silent).
	condensedTicketsLost atomic.Int64

	// listedKeysCap bounds the keys listed in the rolling compaction summary
	// (default DefaultCompactKeysListed; see WithCompactKeysListed).
	listedKeysCap int

	// cardMaxChars bounds the index-card section of the rolling summary
	// (default DefaultCardMaxChars; see WithCardMaxChars). When exceeded and a
	// summary model is available, old card lines are LLM-condensed; otherwise
	// the oldest lines sink into an "earlier n items" counter (never breaks).
	cardMaxChars int

	// meditationMu meditationKeys marks agent_output events produced by meditation turns so
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

// WithRecentFullCount sets the full-window size anchored at compaction rounds,
// overriding the derived default (keepRecent × DefaultRefsPerTurn, see D6).
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

// HotNumbers is the full numeric hot bundle consumed at compression boundaries.
// It must be taken with a single read (why a per-field read is unsafe is specified
// in the document below); zero or invalid fields fall back to the construction
// values, so an owner without any record yet still computes a sane budget.
// 契约: docs/wiki/agent/compression-and-telemetry.md#hot-bundle-atomicity
type HotNumbers struct {
	ThresholdPct float64
	MaxTokens    int
	KeepRecent   int
}

// WithHotSource installs the pull source read at EVERY consumption boundary
// (BudgetLine/Threshold/KeepRecentValue/Compress). While installed the source
// wins per field; unset/invalid fields fall back to the construction atomics.
func WithHotSource(src func() HotNumbers) ContextCompressorOption {
	return func(cc *ContextCompressor) {
		if src != nil {
			cc.hotSource.Store(&src)
		}
	}
}

// SetHotSource installs (or replaces) the pull source after construction — the
// owner's hot view is only reachable once the agent wiring exists (resident CM
// is built before its TagentAgent fields finish wiring, ).
func (cc *ContextCompressor) SetHotSource(src func() HotNumbers) {
	if src == nil {
		return
	}
	cc.hotSource.Store(&src)
}

// liveNums resolves the effective numeric group at THIS boundary: installed
// source per field (>0 / valid wins), else the construction atomics. Each
// boundary takes exactly one source read, so a concurrent rotation can never
// be observed half-applied (threshold from one generation, maxTokens from
// another).
func (cc *ContextCompressor) liveNums() (threshold float64, maxTokens, keepRecent int) {
	threshold, maxTokens, keepRecent = cc.currentThreshold(), int(cc.maxTokens.Load()), int(cc.keepRecent.Load())
	src := cc.hotSource.Load()
	if src == nil {
		return
	}
	h := (*src)()
	if h.ThresholdPct > 0 && !math.IsNaN(h.ThresholdPct) && !math.IsInf(h.ThresholdPct, 0) {
		threshold = h.ThresholdPct
	}
	if h.MaxTokens > 0 {
		maxTokens = h.MaxTokens
	}
	if h.KeepRecent > 0 {
		keepRecent = h.KeepRecent
	}
	return
}

// BudgetLine exposes the effective compression trigger line
// (maxTokens × currentThreshold) for introspection and tests — the number
// the "under budget (x <= y)" log prints. Resolved per boundary from the
// installed hot source; construction atomics when no source.
func (cc *ContextCompressor) BudgetLine() int {
	thr, maxTokens, _ := cc.liveNums()
	return int(float64(maxTokens) * thr)
}

// KeepRecentValue returns the live keepRecent (introspection, 4.6) — hot
// source when installed, construction atomics otherwise.
func (cc *ContextCompressor) KeepRecentValue() int {
	_, _, keepRecent := cc.liveNums()
	return keepRecent
}

// seedThreshold records the construction threshold as atomic bits (the read side
// is currentThreshold/liveNums). Unexported on purpose: after  there is no
// hot write to it — the only authority that can change the effective threshold
// is the hot source.
func (cc *ContextCompressor) seedThreshold(pct float64) {
	if pct <= 0 || math.IsNaN(pct) || math.IsInf(pct, 0) {
		return
	}
	cc.thresholdBits.Store(math.Float64bits(pct))
}

// currentThreshold returns the live threshold as float64 via atomic load.
// Zero bits (no UpdateThreshold ever called) falls back to the default.
func (cc *ContextCompressor) currentThreshold() float64 {
	bits := cc.thresholdBits.Load()
	if bits == 0 {
		return DefaultCompressThreshold
	}
	return math.Float64frombits(bits)
}

// Threshold reports the live compression threshold (the authoritative consumer
// value) for introspection/ops callers. OrgThreshold reads through here so
// ContextManager needs no separate non-atomic mirror (D4/M-3 single source;
// also removes a background-write vs read data race on the mirror). Resolved
// from the hot source when installed.
func (cc *ContextCompressor) Threshold() float64 {
	thr, _, _ := cc.liveNums()
	return thr
}

// MarkMeditationKey 把该 EventKey 登记为冥想产出的锚点，供冥想身份的判定与重建时复用；
// 键集按需分配，压缩器为 nil 或 key 为 0（无身份）时不记录。
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

// MeditationKeysSnapshot returns a sorted copy of the meditation protection
// keys (lock-held snapshot; safe for the caller to hold/traverse).
// Observability for the projection-rebuild reseed path and its tests
// ; the old snapshot.go consumer is gone but
// the read surface stays — ★ rendering correctness depends on these keys
// surviving restarts.
func (cc *ContextCompressor) MeditationKeysSnapshot() []int64 {
	cc.meditationMu.Lock()
	defer cc.meditationMu.Unlock()
	out := make([]int64, 0, len(cc.meditationKeys))
	for k := range cc.meditationKeys {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
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
		listedKeysCap: 0,
		cardMaxChars:  0,
	}
	cc.maxTokens.Store(int64(maxTokens))
	cc.keepRecent.Store(int64(keepRecent))
	for _, opt := range opts {
		opt(cc)
	}
	cc.seedThreshold(thresholdPct)
	if cc.cardMaxChars <= 0 {
		cc.cardMaxChars = maxTokens / 20
		if cc.cardMaxChars <= 0 {
			cc.cardMaxChars = DefaultCardMaxChars
		}
	}
	if cc.listedKeysCap <= 0 {
		cc.listedKeysCap = cc.cardMaxChars / 200
		if cc.listedKeysCap <= 0 {
			cc.listedKeysCap = DefaultCompactKeysListed
		}
	}
	if cc.recentFullCount <= 0 {
		cc.recentFullCount = keepRecent * DefaultRefsPerTurn
	}
	return cc
}

// Compress resolves all projection refs into messages, checks token budget,
// and compresses if over threshold.
//
// Input:
// - ctx: context for LLM calls (used by SmartCompressor)
// - refs: EventReferences from SessionProjection (the historical timeline)
//
// Output:
// - Messages: resolved (and possibly compressed) message list
// - RetainedRefs: updated refs (replaces projection)
// - Notices: error/degradation notices
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

	resolved := cc.resolveRefs(ctx, refs)

	usedTokens := cc.tokenCounter.Estimate(resolved)
	thr, maxTokens, keepRecent := cc.liveNums()
	threshold := int(float64(maxTokens) * thr)

	if usedTokens <= threshold {
		log.Infof("[ContextCompressor] under budget (%d <= %d), %d refs, %d messages",
			usedTokens, threshold, len(refs), len(resolved))
		return CompressResult{
			Messages:     resolved,
			RetainedRefs: refs,
		}
	}

	refs = cc.foldToolRuns(refs)
	dispositions := TelemetryDispositions(ctx, cc.memStore, refs, keepRecent)
	refs = cc.foldSettleRuns(refs, dispositions)
	resolved = cc.resolveRefs(ctx, refs)

	log.Infof("[ContextCompressor] compressing (tokens %d vs %d; folded render %d tokens), %d messages from %d refs",
		usedTokens, threshold, cc.tokenCounter.Estimate(resolved), len(resolved), len(refs))

	compressedMsgs := cc.compressor.CompressWithOptions(ctx, resolved, CompressOptions{
		KeepRecentTasks: keepRecent,
		MaxTokens:       maxTokens,
		TriggerBudget:   threshold,
	})
	newTokens := cc.tokenCounter.Estimate(compressedMsgs)
	log.Infof("[ContextCompressor] SmartCompress: %d -> %d tokens (threshold=%d)",
		usedTokens, newTokens, threshold)

	retainedRefs := cc.buildRetainedRefs(refs, compressedMsgs, ctx, dispositions)

	cc.fullBoundary = anchorFullBoundary(retainedRefs, cc.recentFullCount)

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
		Compressed:   true,
	}
}

// FullBoundary returns the current full-render window anchor . Zero
// means everything renders full (never compacted, or fewer retained refs
// than the window).
func (cc *ContextCompressor) FullBoundary() int64 { return cc.fullBoundary }

// SetFullBoundary overrides the full-render window anchor. Intended for
// cross-instance boundary inheritance (e.g. simulations / future restart
// restoration): the anchor is process-local state, so a fresh compressor
// starts at zero unless explicitly seeded.
func (cc *ContextCompressor) SetFullBoundary(key int64) { cc.fullBoundary = key }

// resolveRefs resolves projection refs into native timeline messages with
// render-time pairing repair. Full resolution (MemoryStore content) applies to
// refs at/after the full-window anchor frozen at the last compaction round
// ; older refs render from their EventSummary — bounding each BeforeModel's
// store-query volume to the window plus newer appends instead of O(refs).
func (cc *ContextCompressor) resolveRefs(ctx context.Context, refs []memory.EventReference) []model.Message {
	declared := make(map[string]bool)
	consumed := make(map[string]bool)
	resolved := make([]model.Message, 0, len(refs))
	for _, ref := range refs {
		msg := cc.resolveRef(ctx, ref, ref.EventKey > 0 && ref.EventKey >= cc.fullBoundary)
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
	return resolved
}

// anchorFullBoundary picks the full-window anchor after a compaction round
// : the oldest of the most recent recentFull positive-key retained refs.
// Zero (fewer positive-key refs than the window) keeps everything full —
// the small-session behavior: that round's retained set is tiny, so full
// rendering is cheap and correct; if a later round re-anchors, prior frozen
// refs that survived stay within the window semantics (a transient re-anchor
// churn is self-healing — one extra compaction re-converges; no error).
func anchorFullBoundary(refs []memory.EventReference, recentFull int) int64 {
	count := 0
	for i := len(refs) - 1; i >= 0; i-- {
		if refs[i].EventKey > 0 {
			count++
			if count == recentFull {
				return refs[i].EventKey
			}
		}
	}
	return 0
}

// foldToolRuns collapses runs of AGED complete tool events (thinking_plan +
// action_command) into compact tool_chain synthetic refs (tool-chain-
// consolidation D2). A "run" is a maximal consecutive sequence of tool events
// not interrupted by a boundary event (external_input/agent_output). Only runs
// in the aged range (before fullFrom = len-keepRecent×refsPerTurn) are folded;
// the recent active frontier (full=true) stays native so tool-call pairing
// stays legal. Folding is idempotent: a tool_chain ref is not a tool event, so
// it is never re-folded; newly-aged tool events fold on subsequent rounds.
func (cc *ContextCompressor) foldToolRuns(refs []memory.EventReference) []memory.EventReference {
	fullFrom := len(refs) - cc.recentFullCount
	if fullFrom <= 1 {
		return refs
	}
	result := make([]memory.EventReference, 0, len(refs))
	i := 0
	for i < len(refs) {
		if i >= fullFrom {
			result = append(result, refs[i:]...)
			break
		}
		if isToolEventRef(refs[i].EventType) {
			j := i
			for j < fullFrom && isToolEventRef(refs[j].EventType) {
				j++
			}
			run := refs[i:j]
			if len(run) >= 2 {
				if n := len(result); n > 0 && result[n-1].EventType == tagentevent.TypeToolChain {
					result[n-1] = mergeToolChainRef(result[n-1], run)
				} else {
					result = append(result, buildToolChainRef(run))
				}
			} else {
				result = append(result, run...)
			}
			i = j
		} else {
			result = append(result, refs[i])
			i++
		}
	}
	return result
}

// isToolEventRef reports whether an event type is a tool-call or tool-result
// event (the two halves of a tool pair).
func isToolEventRef(eventType string) bool {
	return eventType == tagentevent.TypeThinkingPlan || eventType == tagentevent.TypeActionCommand
}

// buildToolChainRef folds a run of tool events into one tool_chain synthetic
// ref. Tool names are read from the thinking_plan EventSummaries ("调用 X",
// populated by GenerateEventSummary D1) — no full-content refetch. The ref
// carries a [evt_first→evt_last] recall ticket so memory_turn can retrieve the
// full chain (the underlying events stay in MemoryStore).
func buildToolChainRef(run []memory.EventReference) memory.EventReference {
	var names []string
	var minTs int64
	steps := 0
	firstKey := run[0].EventKey
	lastKey := run[len(run)-1].EventKey
	for _, ref := range run {
		if ref.EventType == tagentevent.TypeThinkingPlan {
			steps++
			if name := extractToolNameFromSummary(ref.EventSummary); name != "" {
				names = append(names, name)
			}
		}
		if ref.Timestamp > 0 && (minTs == 0 || ref.Timestamp < minTs) {
			minTs = ref.Timestamp
		}
	}
	if minTs == 0 {
		minTs = 1
	}
	var b strings.Builder
	b.WriteString("- 工具链: ")
	if len(names) > 0 {
		b.WriteString(strings.Join(names, "→"))
	} else {
		b.WriteString("工具调用")
	}
	fmt.Fprintf(&b, "（%d步）[evt_%s→evt_%s]",
		steps, tagentevent.FormatEventKey(firstKey), tagentevent.FormatEventKey(lastKey))
	return memory.EventReference{
		EventKey:     -minTs,
		EventType:    tagentevent.TypeToolChain,
		EventSummary: b.String(),
		Timestamp:    minTs,
		Role:         "user",
	}
}

// mergeToolChainRef extends an existing tool_chain ref with a contiguous
// later run: the tool-name sequence, step count, and the
// ticket's last key are extended; the chain's key (its oldest timestamp) is
// kept so the merged chain still sorts at the run's start.
func mergeToolChainRef(existing memory.EventReference, run []memory.EventReference) memory.EventReference {
	names, steps, first := parseToolChainSummary(existing.EventSummary)
	var runNames []string
	runSteps := 0
	lastKey := run[len(run)-1].EventKey
	for _, ref := range run {
		if ref.EventType == tagentevent.TypeThinkingPlan {
			runSteps++
			if n := extractToolNameFromSummary(ref.EventSummary); n != "" {
				runNames = append(runNames, n)
			}
		}
	}
	allNames := names
	if len(runNames) > 0 {
		if allNames != "" && allNames != "工具调用" {
			allNames += "→"
		} else {
			allNames = ""
		}
		allNames += strings.Join(runNames, "→")
	}
	if allNames == "" {
		allNames = "工具调用"
	}
	var b strings.Builder
	b.WriteString("- 工具链: ")
	b.WriteString(allNames)
	fmt.Fprintf(&b, "（%d步）[evt_%s→evt_%s]", steps+runSteps, first, tagentevent.FormatEventKey(lastKey))
	out := existing
	out.EventSummary = b.String()
	return out
}

// parseToolChainSummary splits a tool_chain EventSummary
// ("- 工具链: <names>（<N>步）[evt_<first>→<last>]") back into its parts so a
// contiguous chain can be extended. Tool names are simple identifiers (no
// "（"/"→"), so the fixed self-generated format parses unambiguously.
func parseToolChainSummary(summary string) (names string, steps int, first string) {
	s := strings.TrimPrefix(summary, "- 工具链: ")
	if i := strings.LastIndex(s, "（"); i >= 0 {
		names = strings.TrimSpace(s[:i])
		s = s[i:]
	}
	if _, err := fmt.Sscanf(s, "（%d步）", &steps); err != nil {
		steps = 0
	}
	if i := strings.Index(s, "["); i >= 0 {
		rest := s[i+1:]
		rest = strings.TrimPrefix(rest, "evt_")
		if j := strings.Index(rest, "→"); j >= 0 {
			first = rest[:j]
		}
	}
	return names, steps, first
}

// extractToolNameFromSummary strips the "调用 " prefix from a thinking_plan
// EventSummary ("调用 read_file、grep" -> "read_file、grep"). A thinking_plan
// whose EventSummary is PROSE (think-then-call reasoning models: content
// non-empty, summary = verbatim prose) does NOT carry the "调用 " prefix, so
// it yields "" — the prose must never leak into the tool-chain line as a fake
// "tool name".
func extractToolNameFromSummary(summary string) string {
	s := strings.TrimSpace(summary)
	if !strings.HasPrefix(s, "调用 ") {
		return ""
	}
	return strings.TrimPrefix(s, "调用 ")
}

// settleNoticePrefix matches task-settle notification bodies: "[task settled]"
// (event_bus newTaskSettledEvent / newBatchRetiredSummaryEvent) and the inline
// variant "[task settled inline]" (context_manager reclaim path). Detection
// runs on the ref's EventSummary — external_input is a Special type, so the
// summary is the verbatim single-line body.
const settleNoticePrefix = "[task settled"

// settleFoldRowMaxChars bounds one ticket-card row's summary text — the same
// honesty bound extractCardLine applies to rolling-summary cards; the full
// body (result inline/spill ticket) stays recallable via the row's evt_key.
const settleFoldRowMaxChars = 80

// isSettleNoticeRef reports whether a projection ref is a task-settle
// notification external_input (fold-eligible regardless of segment age —
// unlike tool runs, settle notices carry no pairing legality to protect).
func isSettleNoticeRef(ref memory.EventReference) bool {
	if ref.EventType != tagentevent.TypeExternalInput {
		return false
	}
	s := strings.TrimSpace(tagentevent.StripEventKeyPrefix(ref.EventSummary))
	return strings.HasPrefix(s, settleNoticePrefix)
}

// foldSettleRuns collapses maximal runs of ≥2 consecutive settle-notification
// refs into one settle_fold ticket-card ref. Idempotent: a
// settle_fold ref is not an external_input, so cards are never re-folded; a
// card adjacent to newly-arrived settles stays a separate card (each fold
// act is one bounded card — no unbounded card growth across rounds).
func (cc *ContextCompressor) foldSettleRuns(refs []memory.EventReference, dispositions map[int64]int8) []memory.EventReference {
	result := make([]memory.EventReference, 0, len(refs))
	for i := 0; i < len(refs); {
		if !isSettleNoticeRef(refs[i]) {
			result = append(result, refs[i])
			i++
			continue
		}
		j := i
		for j < len(refs) && isSettleNoticeRef(refs[j]) {
			j++
		}
		run := refs[i:j]
		switch {
		case len(run) >= 2:
			result = append(result, buildSettleFoldRef(run))
		case dispositions[run[0].EventKey] == TelemDemote:
			result = append(result, memory.EventReference{
				EventKey:     -run[0].Timestamp,
				EventType:    tagentevent.TypeSettleFold,
				EventSummary: "- " + settleFoldLine(run[0]),
				Timestamp:    run[0].Timestamp,
				Role:         "user",
			})
		default:
			result = append(result, run...)
		}
		i = j
	}
	return result
}

// buildSettleFoldRef folds a settle run into one ticket-card synthetic ref.
// The card is honest about what it drops: a header stating the fold count and
// the recall path, then one row per settle event carrying its evt_key ticket.
func buildSettleFoldRef(run []memory.EventReference) memory.EventReference {
	var b strings.Builder
	fmt.Fprintf(&b, "〔结算汇总〕%d 个结算通知事件已折叠为票据卡片（批量汇总事件计 1 条，批内多行共享其票据），原文可用 memory_recall 按卡片中的 [evt_key] 票据逐条取回：", len(run))
	var minTs int64
	for _, ref := range run {
		b.WriteString("\n- ")
		b.WriteString(settleFoldLine(ref))
		if ref.Timestamp > 0 && (minTs == 0 || ref.Timestamp < minTs) {
			minTs = ref.Timestamp
		}
	}
	if minTs == 0 {
		minTs = 1
	}
	return memory.EventReference{
		EventKey:     -minTs,
		EventType:    tagentevent.TypeSettleFold,
		EventSummary: b.String(),
		Timestamp:    minTs,
		Role:         "user",
	}
}

// settleFoldLine renders one ticket-card row: "<marker> [evt_key] 摘要行".
// The marker (✓/✗/∞/⚠/◈/?) is the settle line's leading token; the summary
// text keeps everything after the "[task settled …]" wrapper, bounded to
// settleFoldRowMaxChars (recall returns the full body).
func settleFoldLine(ref memory.EventReference) string {
	line := strings.TrimSpace(tagentevent.StripEventKeyPrefix(ref.EventSummary))
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	if idx := strings.IndexByte(line, ']'); idx >= 0 {
		line = strings.TrimSpace(line[idx+1:])
	}
	marker, rest := line, ""
	if sp := strings.IndexByte(line, ' '); sp > 0 {
		marker, rest = line[:sp], strings.TrimSpace(line[sp+1:])
	}
	if utf8.RuneCountInString(rest) > settleFoldRowMaxChars {
		rest = truncate(rest, settleFoldRowMaxChars)
	}
	if marker == "✗" {
		return fmt.Sprintf("★ %s [%s] %s", marker, tagentevent.FormatEventKey(ref.EventKey), rest)
	}
	return fmt.Sprintf("%s [%s] %s", marker, tagentevent.FormatEventKey(ref.EventKey), rest)
}

// parseSettleFoldCardLines extracts the ticket rows (lines starting "- ")
// from a settle_fold card summary, skipping the header line. Used when the
// card itself is L3-retired: the rows re-enter the rolling-summary card
// sequence so every settle keeps its recall ticket (lossless exit).
func parseSettleFoldCardLines(summary string) []string {
	var lines []string
	for _, line := range strings.Split(summary, "\n") {
		if strings.HasPrefix(line, "- ") {
			lines = append(lines, line)
		}
	}
	return lines
}

// resolveRef resolves a single EventReference to a native timeline message.
// When full is true the content comes from MemoryStore; otherwise (refs before
// the full-window anchor) the reference's EventSummary is used directly — no
// store query. The result is tagged with [evt_KEY|type] so SmartCompressor and
// buildRetainedRefs can track retained refs.
func (cc *ContextCompressor) resolveRef(
	ctx context.Context,
	ref memory.EventReference,
	full bool,
) model.Message {
	if ref.EventType == tagentevent.TypeContextCompress {
		return model.Message{
			Role:    model.RoleUser,
			Content: prefixEventKey("〔历史归档〕系统生成的压缩摘要（非用户发言，勿模仿此格式）："+ref.EventSummary, ref),
		}
	}
	if ref.EventType == tagentevent.TypeToolChain {
		return model.Message{
			Role:    model.RoleUser,
			Content: prefixEventKey(ref.EventSummary, ref),
		}
	}
	if ref.EventType == tagentevent.TypeSettleFold {
		return model.Message{
			Role:    model.RoleUser,
			Content: prefixEventKey(ref.EventSummary, ref),
		}
	}

	content := ""
	var toolCalls []model.ToolCall
	toolID := ""
	var contentParts []model.ContentPart
	resolved := false
	if full && cc.memStore != nil && ref.EventKey > 0 {
		evt, err := cc.memStore.GetEvent(ref.EventKey)
		if err == nil && evt != nil {
			toolCalls = evt.ToolCalls
			toolID = evt.ToolID
			contentParts = evt.ContentParts
			switch {
			case evt.Content != "" || len(evt.ToolCalls) > 0 || len(evt.ContentParts) > 0:
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
	return renderTimelineMessage(ref, content, toolCalls, toolID, contentParts)
}

// renderTimelineMessage renders one event in NATIVE protocol form (D3 v2):
// - thinking_plan → role=assistant with native ToolCalls restored from the
// stored event; content is prose only — the system NEVER generates textual
// call syntax into assistant history (any such syntax is imitable and
// leads models to fabricate tool calls in plain text)
// - action_command → role=tool with its ToolID (pairing legality against
// the rendered sequence is enforced by the caller, which demotes orphans
// via demoteToInputNote)
// - notifications and everything else → eventTypeToRole text
func renderTimelineMessage(
	ref memory.EventReference,
	content string,
	toolCalls []model.ToolCall,
	toolID string,
	contentParts []model.ContentPart,
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
			Role:         EventTypeToRole(ref.EventType),
			Content:      prefixEventKey(content, ref),
			ContentParts: contentParts,
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

// rollingNarrativeCapChars Rolling-narrative caps (compile-time constants, not config knobs — the
// compression knob diet applies here too):
// - rollingNarrativeCapChars bounds the narrative section itself;
// - narrativeSkeletonCapChars bounds each skeleton excerpt fed to synthesis;
// - narrativeEventCap bounds the number of excerpts per L3 round.
const (
	rollingNarrativeCapChars  = 1500
	narrativeSkeletonCapChars = 800
	narrativeEventCap         = 24
)

// narrativePrefix marks the LLM rolling-narrative line inside a rolling
// summary (single line, scrubbed at synthesis, parsed back next round).
const narrativePrefix = "〔历史综述〕"

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

// parseNarrativeSection extracts the prior 〔历史综述〕 line from a rolling
// summary. The narrative is a SINGLE line (scrubbed at synthesis); cards and
// trailers below it are unaffected.
func parseNarrativeSection(summary string) string {
	for _, line := range strings.Split(summary, "\n") {
		if strings.HasPrefix(line, narrativePrefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, narrativePrefix))
		}
	}
	return ""
}

// curateCards enforces the card-section bound: when the joined lines exceed
// cardMaxChars, OLD lines are LLM-condensed (material law: input = card
// lines, layer-2 artifacts) — but ONLY after the machine ticket guard passes
// . A rejected or
// failed condensation falls through to deterministic sinking: without a
// model, on error, or when the condensed text drops/forges recall tickets,
// the oldest ORIGINAL lines sink into the earlier-items counter (engineering
// fallback, never breaks, model text never enters the payload).
func (cc *ContextCompressor) curateCards(ctx context.Context, cards []string, earlier int) ([]string, int) {
	joined := strings.Join(cards, "\n")
	if cc.cardMaxChars <= 0 || len(joined) <= cc.cardMaxChars {
		return cards, earlier
	}
	half := len(cards) / 2
	if half > 0 && cc.compressor != nil && cc.compressor.summaryModel != nil {
		condensed, err := cc.condenseCardLines(ctx, cards[:half])
		condensed = strings.Join(strings.Fields(condensed), " ")
		reject := ""
		if err == nil && condensed != "" {
			reject = guardCondensedCard(condensed, cards[:half])
		}
		switch {
		case err != nil:
			log.Warnf("[ContextCompressor] card condensation failed (sinking instead): %v", err)
		case reject != "":
			log.Warnf("[ContextCompressor] card condensation REJECTED by ticket guard (sinking instead): %s", reject)
		case condensed == "":
		default:
			newCards := append([]string{"- " + condensed}, cards[half:]...)
			if len(strings.Join(newCards, "\n")) <= cc.cardMaxChars {
				cc.noteCondensedTicketsLost(condensed, cards[:half])
				return newCards, earlier
			}
			cards = newCards
		}
	}
	joint := len(strings.Join(cards, "\n"))
	for len(cards) > 1 && joint > cc.cardMaxChars {
		dropped := len(cards[0]) + 1
		cards = cards[1:]
		joint -= dropped
		earlier++
	}
	if len(cards) == 1 && len(cards[0]) > cc.cardMaxChars {
		fitted, representable := fitTicketCard(cards[0], cc.cardMaxChars)
		cards[0] = fitted
		if !representable {
			cc.noteBudgetUnrepresentable(len(fitted), cc.cardMaxChars, fitted)
		}
	}
	return cards, earlier
}

// noteBudgetUnrepresentable budgetUnrepresentable counts (observable, monotone per compressor) how
// often the card budget could not express even the tickets-only form of a
// single over-cap card — an operator-tuning signal (card_max_chars scales
// with max_tokens, so hitting it means the budget formula is under-sized).
func (cc *ContextCompressor) noteBudgetUnrepresentable(got, cap int, line string) {
	cc.budgetUnrepresentable.Add(1)
	log.Errorf("[ContextCompressor] budget-unrepresentable: single card needs %d chars > cap %d, kept tickets-only: %q", got, cap, line)
}

// BudgetUnrepresentable returns the cumulative count of budget-unrepresentable
// card states (diagnostics surface; 6.3 "无法表达状态传给诊断").
func (cc *ContextCompressor) BudgetUnrepresentable() int64 {
	return cc.budgetUnrepresentable.Load()
}

// noteCondensedTicketsLost makes condensation's navigation trade observable
// : the guard forces head/tail/★ ticket survival, every ticket
// BEYOND those that the condensed prose swallows is a recall-address loss —
// legitimate compression (the events stay recallable by time range), but a
// counted and logged one. Silent ticket death is the failure mode this closes.
func (cc *ContextCompressor) noteCondensedTicketsLost(condensed string, input []string) {
	in := make(map[string]bool)
	for _, l := range input {
		for _, k := range parseCardTickets(l) {
			in[k] = true
		}
	}
	out := make(map[string]bool)
	for _, k := range parseCardTickets(condensed) {
		out[k] = true
	}
	lost := 0
	for k := range in {
		if !out[k] {
			lost++
		}
	}
	if lost == 0 {
		return
	}
	cc.condensedTicketsLost.Add(int64(lost))
	log.Warnf("[ContextCompressor] card condensation folded %d recall ticket(s) into prose (cumulative %d; events stay recallable by time range)",
		lost, cc.condensedTicketsLost.Load())
}

// CondensedTicketsLost returns the cumulative count of recall tickets folded
// into prose by card condensation.
func (cc *ContextCompressor) CondensedTicketsLost() int64 {
	return cc.condensedTicketsLost.Load()
}

// fitTicketCard bounds one over-cap card line to at most cap chars while
// keeping every recall ticket it carries. Prose is stripped first (tickets +
// truncation marker survive); the result is idempotent — re-fitting an already
// fitted line is a no-op when it fits, or the same tickets-only form again.
// ok=false reports budget-unrepresentable: even the tickets-only line exceeds
// the cap (the tickets-only line is still returned — losing tickets silently
// is not an option).
func fitTicketCard(line string, cap int) (fitted string, ok bool) {
	var b strings.Builder
	b.WriteString("- ")
	if strings.Contains(line, "★") {
		b.WriteString("★ ")
	}
	for _, k := range parseCardTickets(line) {
		b.WriteString("[" + k + "] ")
	}
	b.WriteString("〔预算截断〕")
	fitted = strings.TrimRight(b.String(), " ")
	if len(fitted) <= cap {
		return fitted, true
	}
	return fitted, false
}

// cardTicketRe matches canonical recall tickets inside card lines:
// "[<hex>]" in FormatEventKey form (lowercase hex, negative keys allowed).
// Uppercase or non-hex bracketed text is NOT a ticket — a model-echoed
// fabrication like "[AAAA0004]" must fail the subset check rather than pass
// as a case-insensitive match.
var cardTicketRe = regexp.MustCompile(`\[(-?[0-9a-f]+)\]`)

// parseCardTickets extracts the recall tickets of one card line.
func parseCardTickets(line string) []string {
	ms := cardTicketRe.FindAllStringSubmatch(line, -1)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m[1])
	}
	return out
}

// guardCondensedCard 对一行清洗后的综述卡片做机器校验，比对的输入是折叠掉的那半输入行。
// 可采纳时返回 ""，否则返回拒绝理由。要求：
// - 输出票据 ⊆ 输入票据（不得伪造——伪造票据会进入 compaction 载荷并污染其后每一次召回）；
// - 头、尾以及每个 ★ 高亮行的票据都必须存活（导航锚点与长期反思结论）；
// - 输入含票据而输出一个都没有 = 票据全丢 ⇒ 拒绝。
//
// 输入本身不含可解析票据（散文类 fixture）时不拒绝——本守卫只约束真实票据。
// 零 LLM 调用、零存储读取。
func guardCondensedCard(condensed string, input []string) string {
	in := make(map[string]bool)
	required := make(map[string]bool)
	markAll := func(keys []string, into map[string]bool) {
		for _, k := range keys {
			into[k] = true
		}
	}
	for _, l := range input {
		keys := parseCardTickets(l)
		markAll(keys, in)
	}
	if len(in) == 0 {
		return ""
	}
	if len(input) > 0 {
		markAll(parseCardTickets(input[0]), required)
		markAll(parseCardTickets(input[len(input)-1]), required)
		for _, l := range input {
			if strings.Contains(l, "★") {
				markAll(parseCardTickets(l), required)
			}
		}
	}
	out := parseCardTickets(condensed)
	if len(out) == 0 {
		return "no recall ticket survived"
	}
	for _, k := range out {
		if !in[k] {
			return "unknown ticket [" + k + "]"
		}
	}
	outSet := make(map[string]bool, len(out))
	markAll(out, outSet)
	for _, l := range input {
		for _, k := range parseCardTickets(l) {
			if !required[k] {
				continue
			}
			if !outSet[k] {
				return "required ticket [" + k + "] dropped"
			}
		}
	}
	return ""
}

// condenseCardLines asks the summary model to condense old card lines into a
// single compact card, preserving the task skeleton and key references so
// memory_recall tickets stay valid.
func (cc *ContextCompressor) condenseCardLines(ctx context.Context, lines []string) (string, error) {
	prompt := "将以下历史任务卡片浓缩为一行紧凑概述。硬性要求：保留任务骨架与时间跨度，保留方括号内的关键 key（至少保留首尾与重要节点的 [key]），★ 开头的高亮行为长期反思结论——其要点与 [key] 必须保留，不添加任何未出现的事实，不确定处省略。只输出浓缩后的一行，不要前缀。\n\n" + strings.Join(lines, "\n")
	return cc.compressor.generatePlainSummary(ctx, prompt)
}

// synthesizeRollingNarrative folds the prior narrative plus THIS round's
// newly L3-compacted skeleton excerpts into a fresh single-line narrative via
// the summary model (rolling-semantic-summary). The ticket layer (card lines)
// stays pure engineering — this is the optional LLM layer on top of it, giving
// the model a comprehension-level overview of the oldest history while the
// [evt_key] tickets keep every compacted fact recallable.
//
// Degradation contract: no model / no new skeleton material / call failure →
// the prior narrative is returned unchanged and compaction proceeds
// engineering-only. Never blocks compaction, never loses tickets, never
// invents facts beyond the given material (material law: excerpts are the
// real stored text, not the second-hand card lines).
func (cc *ContextCompressor) synthesizeRollingNarrative(ctx context.Context, prior string, dropped []memory.EventReference) string {
	if cc.compressor == nil || cc.compressor.summaryModel == nil {
		return prior
	}
	var b strings.Builder
	n := 0
	for _, ref := range dropped {
		if n >= narrativeEventCap {
			break
		}
		if ref.EventType != tagentevent.TypeExternalInput && ref.EventType != tagentevent.TypeAgentOutput {
			continue
		}
		text := ref.EventSummary
		if cc.memStore != nil && ref.EventKey > 0 {
			if evt, err := cc.memStore.GetEvent(ref.EventKey); err == nil && evt != nil && evt.Content != "" {
				text = evt.Content
			}
		}
		text = strings.Join(strings.Fields(text), " ")
		role := "user"
		if ref.EventType == tagentevent.TypeAgentOutput {
			role = "assistant"
		}
		ts := ""
		if ref.Timestamp > 0 {
			ts = time.UnixMilli(ref.Timestamp).Format(cardTimeLayout) + " "
		}
		fmt.Fprintf(&b, "- %s%s: %s\n", ts, role, truncate(text, narrativeSkeletonCapChars))
		n++
	}
	if b.Len() == 0 {
		return prior
	}

	var pb strings.Builder
	pb.WriteString("将「旧历史综述」与「新折叠的历史事件」合成为一段新的历史综述。硬性要求：\n")
	pb.WriteString("- 保留旧综述中仍然有效的事实，融合新事件的关键信息（用户请求、完成的工作、重要结果与结论）\n")
	pb.WriteString("- 按时间顺序组织，语言紧凑；事实仅限给定材料，不添加任何未出现的内容，不确定处省略\n")
	fmt.Fprintf(&pb, "- 只输出一段连续文字：不要换行、不要列表、不要前缀，长度不超过 %d 个字符\n\n", rollingNarrativeCapChars)
	if prior != "" {
		pb.WriteString("旧历史综述：\n" + prior + "\n\n")
	} else {
		pb.WriteString("旧历史综述：（无）\n\n")
	}
	pb.WriteString("新折叠的历史事件（时间旧→新，user=用户输入 / assistant=助手产出）：\n")
	pb.WriteString(b.String())

	out, err := cc.compressor.generatePlainSummary(ctx, pb.String())
	if err != nil || strings.TrimSpace(out) == "" {
		log.Warnf("[ContextCompressor] rolling narrative synthesis failed (engineering-only fallback): %v", err)
		return prior
	}
	narrative := strings.Join(strings.Fields(out), " ")
	return truncate(narrative, rollingNarrativeCapChars)
}

func (cc *ContextCompressor) buildRetainedRefs(
	originalRefs []memory.EventReference,
	compressedMsgs []model.Message,
	ctx context.Context,
	dispositions map[int64]int8,
) []memory.EventReference {
	if len(originalRefs) == 0 {
		return nil
	}

	retainedKeys := make(map[int64]bool)
	retainedChainKeys := make(map[int64]bool)
	retainedFoldKeys := make(map[int64]bool)
	for _, msg := range compressedMsgs {
		if MessageEventType(&msg) == tagentevent.TypeToolChain {
			if k, _, _ := tagentevent.ParseEventKeyAndType(msg.Content); k < 0 {
				retainedChainKeys[k] = true
			}
		}
		if MessageEventType(&msg) == tagentevent.TypeSettleFold {
			if k, _, _ := tagentevent.ParseEventKeyAndType(msg.Content); k < 0 {
				retainedFoldKeys[k] = true
			}
		}
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
	var droppedRefs []memory.EventReference
	var newCards, oldCards []string
	var minTs int64
	var foldedCardEvents int
	priorCount := 0
	earlier := 0
	priorNarrative := ""

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
			if pn := parseNarrativeSection(ref.EventSummary); pn != "" {
				if priorNarrative == "" {
					priorNarrative = pn
				} else {
					priorNarrative += " " + pn
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
		if ref.EventKey < 0 && ref.EventType == tagentevent.TypeToolChain {
			if retainedChainKeys[ref.EventKey] {
				retained = append(retained, ref)
			}
			continue
		}
		if ref.EventKey < 0 && ref.EventType == tagentevent.TypeSettleFold {
			if retainedFoldKeys[ref.EventKey] {
				retained = append(retained, ref)
			} else {
				rows := parseSettleFoldCardLines(ref.EventSummary)
				newCards = append(newCards, rows...)
				foldedCardEvents += len(rows)
				if minTs == 0 || ref.Timestamp < minTs {
					minTs = ref.Timestamp
				}
			}
			continue
		}
		if retainedKeys[ref.EventKey] {
			retained = append(retained, ref)
		} else if dispositions != nil && isSettleNoticeRef(ref) && dispositions[ref.EventKey] == TelemActive {
			retained = append(retained, ref)
		} else if ref.EventKey > 0 {
			compressedKeys = append(compressedKeys, tagentevent.FormatEventKey(ref.EventKey))
			droppedRefs = append(droppedRefs, ref)
			if card := cc.extractCardLine(ref); card != "" {
				newCards = append(newCards, card)
			}
			if minTs == 0 || ref.Timestamp < minTs {
				minTs = ref.Timestamp
			}
		}
	}

	if total := priorCount + len(compressedKeys) + foldedCardEvents; total > 0 {
		if minTs == 0 {
			minTs = time.Now().UnixMilli()
		}
		cards, earlierOut := cc.curateCards(ctx, append(oldCards, newCards...), earlier)
		narrative := cc.synthesizeRollingNarrative(ctx, priorNarrative, droppedRefs)

		var b strings.Builder
		fmt.Fprintf(&b, "[Compacted %d historical events]", total)
		if narrative != "" {
			b.WriteString("\n" + narrativePrefix + narrative)
		}
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
