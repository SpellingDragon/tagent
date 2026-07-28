package compress

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// SmartCompressor performs deterministic context compression with optional
// LLM summary generation.
//
// Pipeline (skeleton model, default):
//  1. Segment messages into task turns bounded by agent_output.
//  2. Deterministic level per segment age (pure function).
//  3. Per-segment drop: L0 (keep) / L1 (drop tool) / L2 (skeleton only) /
//     L3 (multi-segment compaction — whole segment leaves the timeline).
//  4. Assemble chronologically; kept messages keep their event key prefixes.
//
// This is a "view transformation" — it modifies the messages sent to the LLM,
// but does NOT modify the Session or Projection.
type SmartCompressor struct {
	summaryModel    model.Model  // Optional: used for LLM summary generation
	KeepRecentTasks int          // Number of recent complete tasks to keep (default: 2)
	maxTokens       int          // Token budget for calculating batch size (default: DefaultMaxTokens)
	tokenCounter    TokenCounter // Token estimator (injected, not NewDefaultTokenCounter)

	// skeletonSegmentation selects the skeleton pipeline (task-skeleton-
	// compression): agent_output-bounded segments, age-driven levels,
	// tool>assistant drop order, L3 multi-segment compaction. Default on;
	// false falls back to the legacy user-boundary pipeline below.
	skeletonSegmentation bool

	// archiveCache maps segment content hash → archived artifact, so the same
	// segment is NEVER re-summarized or re-stored across rounds (material law:
	// cost stays O(new segments), independent of history size).
	archiveCacheMu       sync.Mutex
	archiveCache         map[string]archivedSegment
	archiveCacheCap      int // bound on cached entries (0 → DefaultArchiveCacheCap)
	maxSummaryInputChars int // splitting threshold for one summary call's input (0 → DefaultMaxSummaryInputChars)
	summaryMaxTokens     int // output-token budget floor for summary calls (0 → DefaultSummaryMaxTokens)

	// Configurable truncation parameters
	maxExecStateChars  int // Total execution state truncation (default: 2000)
	maxToolResultChars int // Per-tool-result truncation (default: 500)
	maxToolArgsChars   int // Per-tool-args truncation (default: 80)
	maxNoticeChars     int // Compress-notice text cap (default: DefaultMaxNoticeChars)

	// Summary parameters
	chunkSummaryLen int                // Summary length per segment (default: 150)
	memStore        memory.MemoryStore // Optional: for summary archive
	projection      *SessionProjection // Optional: for archive EventReference append
}

// NewSmartCompressor creates a new SmartCompressor.
func NewSmartCompressor(opts ...SmartCompressorOption) *SmartCompressor {
	sc := &SmartCompressor{
		KeepRecentTasks:      2,
		tokenCounter:         NewDefaultTokenCounter(),
		maxTokens:            DefaultMaxTokens,
		maxExecStateChars:    2000,
		maxToolResultChars:   500,
		maxToolArgsChars:     80,
		chunkSummaryLen:      150,
		skeletonSegmentation: true,
	}
	for _, opt := range opts {
		opt(sc)
	}
	return sc
}

// SmartCompressorOption configures SmartCompressor.
type SmartCompressorOption func(*SmartCompressor)

// WithSummaryModel sets the LLM model for Stage 2 summary generation.
func WithSummaryModel(m model.Model) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		sc.summaryModel = m
	}
}

// WithKeepRecentTasks sets how many recent tasks to keep.
func WithKeepRecentTasks(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		sc.KeepRecentTasks = n
	}
}

// WithSkeletonSegmentation toggles the skeleton pipeline (agent_output
// segment boundaries + age-driven levels + multi-segment compaction).
// Default true; false reverts to the legacy user-boundary pipeline.
func WithSkeletonSegmentation(enabled bool) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.skeletonSegmentation = enabled }
}

// WithMaxTokens sets the token budget used for batch size calculation.
func WithMaxTokens(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.maxTokens = n }
}

// WithMaxExecStateChars sets the total execution state truncation limit.
func WithMaxExecStateChars(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.maxExecStateChars = n }
}

// WithMaxToolResultChars sets the per-tool-result truncation limit.
func WithMaxToolResultChars(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.maxToolResultChars = n }
}

// WithMaxToolArgsChars sets the per-tool-args truncation limit.
func WithMaxToolArgsChars(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.maxToolArgsChars = n }
}

// WithMaxNoticeChars caps the compress-notice text length (default
// DefaultMaxNoticeChars).
func WithMaxNoticeChars(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		sc.maxNoticeChars = n
	}
}

// WithChunkSummaryLen sets the summary length per segment.
func WithChunkSummaryLen(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.chunkSummaryLen = n }
}

// WithMaxSummaryInputChars sets the splitting threshold for a single summary
// call's input (0 → DefaultMaxSummaryInputChars). A giant segment exceeding it
// is split into smaller message-groups (each summarized separately then
// joined); a single oversized message is sent as-is (never content-truncated).
func WithMaxSummaryInputChars(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		if n > 0 {
			sc.maxSummaryInputChars = n
		}
	}
}

// WithSummaryMaxTokens sets the output-token budget floor for summary calls
// (0 → DefaultSummaryMaxTokens). Reasoning models spend part of max_tokens on
// their thinking chain; reserving enough output tokens keeps Content from
// coming back empty (which would degrade every segment).
func WithSummaryMaxTokens(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		if n > 0 {
			sc.summaryMaxTokens = n
		}
	}
}

// WithArchiveCacheCap bounds the per-process archive cache entries
// (0 → DefaultArchiveCacheCap; the archives themselves persist in MemoryStore).
func WithArchiveCacheCap(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		if n > 0 {
			sc.archiveCacheCap = n
		}
	}
}

// WithMemStore injects a MemoryStore for summary archive persistence.
func WithMemStore(ms memory.MemoryStore) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.memStore = ms }
}

// WithProjection injects a SessionProjection for archive EventReference append.
func WithProjection(p *SessionProjection) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.projection = p }
}

func WithTokenCounter(tc TokenCounter) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.tokenCounter = tc }
}

// Compress implements budget-aware compression.
//
// Default (skeleton pipeline, task-skeleton-compression):
// 1. Segment messages into task turns bounded by agent_output
// 2. Deterministic level per segment age (pure function, zero LLM):
//   - L0: keep all (in-progress segment, or age < keepRecent)
//   - L1: drop action_command, keep skeleton + thinking_plan
//   - L2: skeleton only (external_input + agent_output)
//   - L3: multi-segment compaction — whole segment leaves the timeline;
//     ContextCompressor.buildRetainedRefs folds it into the rolling summary
//
// 3. Budget escalation: old segments → L2, then oldest → L3 until under budget
//
// Legacy (WithSkeletonSegmentation(false)): user-boundary segments with
// HasUserInput-driven levels and per-segment LLM summaries (see
// compressLegacy).
func (sc *SmartCompressor) Compress(
	ctx context.Context,
	messages []model.Message,
	inv *agent.Invocation,
) []model.Message {
	if sc.skeletonSegmentation {
		return sc.compressSkeleton(ctx, messages)
	}
	return sc.compressLegacy(ctx, messages, inv)
}

// deterministicLevel assigns a compression level to a task segment by age
// (deterministic-compress-level spec). Pure function: no side effects, no
// LLM/store reads; age = totalSegs - 1 - segIdx (0 = newest). The old
// HasUserInput criterion is retired — segments are agent_output-bounded, so
// archival (L3) is genuinely reachable.
func deterministicLevel(seg *TaskSegment, segIdx, totalSegs, keepRecent int) int {
	if seg == nil || !seg.IsComplete {
		return 0 // in-progress segment: pending input, never compressed
	}
	if keepRecent < 1 {
		keepRecent = 1
	}
	age := totalSegs - 1 - segIdx
	switch {
	case age < keepRecent:
		return 0
	case age < keepRecent*2:
		return 1
	case age < keepRecent*3:
		return 2
	default:
		return 3
	}
}

// applySegmentLevel renders the messages a segment keeps at a level. Kept
// messages retain their original content — and thus the [evt_KEY|type]
// prefix — so buildRetainedRefs can track surviving refs. Drop order is
// tool > assistant: L1 drops action_command only, L2 drops thinking_plan
// too, L3 drops the whole segment (multi-segment compaction).
func applySegmentLevel(seg *TaskSegment, level int) []model.Message {
	switch level {
	case 0:
		return seg.Messages
	case 1:
		out := make([]model.Message, 0, len(seg.Messages))
		for i := range seg.Messages {
			msg := seg.Messages[i]
			if MessageEventType(&msg) == tagentevent.TypeActionCommand {
				continue
			}
			// Results are dropped, so declared calls must go too — a
			// dangling tool_call is illegal in native protocol form. Keep
			// the prose (with its event prefix); drop pure-call messages.
			if len(msg.ToolCalls) > 0 {
				if msg.Content == "" {
					continue
				}
				msg.ToolCalls = nil
			}
			out = append(out, msg)
		}
		return out
	case 2:
		out := make([]model.Message, 0, len(seg.Messages))
		for i := range seg.Messages {
			msg := seg.Messages[i]
			if !IsSkeletonMessage(&msg) {
				continue
			}
			msg.ToolCalls = nil
			out = append(out, msg)
		}
		return out
	default:
		// L3 multi-segment compaction: the whole segment leaves the timeline.
		// Its event keys never appear in the output, so buildRetainedRefs
		// folds the skeleton (external_input/agent_output cards) into the
		// rolling summary — the archival exit, zero LLM required.
		return nil
	}
}

// compressSkeleton is the skeleton-model pipeline. It is pure engineering —
// no summary-model calls — so it never fails or degrades; with
// summaryModel=nil the archival path still completes via the rolling-summary
// index cards downstream.
func (sc *SmartCompressor) compressSkeleton(_ context.Context, messages []model.Message) []model.Message {
	startTime := time.Now()
	systemMsg, rest := SplitSystemMessage(messages)
	segments := SegmentMessages(rest)

	keepRecent := sc.KeepRecentTasks
	if keepRecent < 1 {
		keepRecent = 1
	}
	completeCount := 0
	for _, seg := range segments {
		if seg.IsComplete {
			completeCount++
		}
	}
	beforeTokens := sc.tokenCounter.Estimate(messages)
	if beforeTokens <= sc.maxTokens && completeCount <= keepRecent {
		return messages
	}

	// Base levels by segment age (pure function).
	levels := make([]int, len(segments))
	for i, seg := range segments {
		levels[i] = deterministicLevel(seg, i, len(segments), keepRecent)
	}

	// Precompute per-segment cost at every level (4n Estimate calls), so
	// budget escalation below is O(1) incremental per step instead of
	// re-estimating the whole timeline (O(n²) — code review M1).
	systemCost := 0
	if systemMsg != nil {
		systemCost = sc.tokenCounter.Estimate([]model.Message{*systemMsg})
	}
	cost := make([][4]int, len(segments))
	for i, seg := range segments {
		for lvl := 0; lvl <= 3; lvl++ {
			cost[i][lvl] = sc.tokenCounter.Estimate(applySegmentLevel(seg, lvl))
		}
	}
	total := systemCost
	for i := range segments {
		total += cost[i][levels[i]]
	}

	// Budget escalation (D3 "预算仍不足"): press all old complete segments to
	// skeleton first, then compact whole segments oldest-first. Segments
	// within keepRecent and in-progress segments are never escalated.
	escalate := func(i, lvl int) {
		total -= cost[i][levels[i]] - cost[i][lvl]
		levels[i] = lvl
	}
	if total > sc.maxTokens {
		for i, seg := range segments {
			if age := len(segments) - 1 - i; seg.IsComplete && age >= keepRecent && levels[i] < 2 {
				escalate(i, 2)
			}
		}
	}
	if total > sc.maxTokens {
		for i, seg := range segments {
			if age := len(segments) - 1 - i; seg.IsComplete && age >= keepRecent && levels[i] < 3 {
				escalate(i, 3)
				if total <= sc.maxTokens {
					break
				}
			}
		}
	}

	// Chronological assembly: kept messages carry their original event key
	// prefixes so surviving refs stay trackable downstream.
	result := make([]model.Message, 0, len(messages))
	if systemMsg != nil {
		result = append(result, *systemMsg)
	}
	changed := false
	levelCounts := map[int]int{0: 0, 1: 0, 2: 0, 3: 0}
	for i, seg := range segments {
		kept := applySegmentLevel(seg, levels[i])
		if len(kept) != len(seg.Messages) {
			changed = true
		}
		levelCounts[levels[i]]++
		result = append(result, kept...)
	}
	if !changed {
		return messages
	}

	afterTokens := sc.tokenCounter.Estimate(result)
	metrics := map[string]interface{}{
		"event":         "smart_compress",
		"mode":          "skeleton",
		"before_tokens": beforeTokens,
		"after_tokens":  afterTokens,
		"segments":      len(segments),
		"l0_keep":       levelCounts[0],
		"l1_drop_tool":  levelCounts[1],
		"l2_skeleton":   levelCounts[2],
		"l3_compact":    levelCounts[3],
		"duration_ms":   time.Since(startTime).Milliseconds(),
	}
	if metricsJSON, err := json.Marshal(metrics); err == nil {
		log.Infof("[SmartCompress] %s tokens=%d->%d (%+d) levels=L0:%d L1:%d L2:%d L3:%d",
			string(metricsJSON), beforeTokens, afterTokens, afterTokens-beforeTokens,
			levelCounts[0], levelCounts[1], levelCounts[2], levelCounts[3])
	}
	return result
}

// compressLegacy is the pre-skeleton pipeline (user-boundary segments,
// HasUserInput-driven levels, per-segment LLM summaries), kept as the
// WithSkeletonSegmentation(false) rollback path.
//
// Algorithm:
// 1. Segment messages by user input boundary
// 2. Compute excess = current_tokens - max_tokens (how much to cut)
// 3. Deterministic level assignment per segment (age + content type):
//   - Level 0: no compression (recent segments or last incomplete segment)
//   - Level 1: selective (preserve user input + key tools, compress non-key exec)
//   - Level 2: partial (preserve user input, compress all exec)
//   - Level 3: full (archive to MemoryStore + inline summary)
//
// 4. Per-segment LLM call for level 2 exec and level 1 non-key (degrade to first-stage on failure)
// 5. Assemble: chronological order with level-specific handling
func (sc *SmartCompressor) compressLegacy(
	ctx context.Context,
	messages []model.Message,
	inv *agent.Invocation,
) []model.Message {
	startTime := time.Now()

	// 1. Separate system message
	systemMsg, rest := SplitSystemMessage(messages)

	// 2. Segment by user input boundary
	segments := segmentMessagesByUser(rest)

	// 3. Quick exit: not enough segments or under budget with few segments
	beforeTokens := sc.tokenCounter.Estimate(messages)
	minPreserve := sc.KeepRecentTasks
	if minPreserve < 1 {
		minPreserve = 1
	}
	// Only skip compression if: under token budget AND not too many segments
	if beforeTokens <= sc.maxTokens && len(segments) <= minPreserve {
		return messages
	}

	// excess: how much we need to cut (at least 1 if there are old segments)
	excess := beforeTokens - sc.maxTokens
	hasOldSegments := len(segments) > minPreserve
	if hasOldSegments && excess <= 0 {
		excess = 1 // Force compression of old segments beyond KeepRecentTasks
	}

	completeCount := 0
	for _, seg := range segments {
		if seg.IsComplete {
			completeCount++
		}
	}
	log.Debugf("[SmartCompress] split: segments=%d (complete=%d incomplete=%d) excess=%d msgs=%d",
		len(segments), completeCount, len(segments)-completeCount, excess, len(messages))

	// 4. Budget-aware greedy compression planning
	type segPlan struct {
		seg        *TaskSegment
		level      int // 0=none, 1=selective, 2=partial, 3=full
		userMsgs   []model.Message
		keyMsgs    []model.Message
		nonKeyMsgs []model.Message
		execMsgs   []model.Message
		origIndex  int // original index in segments slice (for chronological assembly)
	}

	plans := make([]segPlan, len(segments))
	for i, seg := range segments {
		userMsgs, execMsgs := splitUserAndExec(seg.Messages)
		keyMsgs, nonKeyMsgs := sc.selectiveSplit(execMsgs)
		plans[i] = segPlan{
			seg:        seg,
			level:      0,
			userMsgs:   userMsgs,
			keyMsgs:    keyMsgs,
			nonKeyMsgs: nonKeyMsgs,
			execMsgs:   execMsgs,
			origIndex:  i,
		}
	}

	// 5. Deterministic level assignment based on segment age and content features.
	// This replaces the LLM-based valuation with a pure, instant function.
	//
	// Level assignment rules:
	//   - Last segment or recent (within KeepRecentTasks): L0 (no compression)
	//   - Has user input AND age < keepRecent*2: L1 (selective: keep user + key tools)
	//   - Has user input OR age < keepRecent*3: L2 (partial: keep user, compress exec)
	//   - Old tool-only segments: L3 (full: archive to MemoryStore + inline summary)

	for i := range plans {
		isLast := i == len(plans)-1
		isLastIncomplete := isLast && !plans[i].seg.IsComplete
		isRecent := i >= len(plans)-minPreserve
		hasUserInput := len(plans[i].userMsgs) > 0

		if isRecent || isLastIncomplete {
			plans[i].level = 0 // L0: keep
			continue
		}

		age := len(plans) - 1 - i // 0=newest, larger=older

		if hasUserInput && age < minPreserve*2 {
			plans[i].level = 1 // L1: selective
		} else if hasUserInput || age < minPreserve*3 {
			plans[i].level = 2 // L2: partial
		} else {
			plans[i].level = 3 // L3: full archive
		}
	}

	// Fallback: if we're over budget but all segments are considered "recent"
	// (e.g., only 2 segments with keep_recent=2), force compression of the
	// oldest non-last segments until we can make progress.
	if excess > 0 && len(plans) > 1 {
		allRecent := true
		for i := range plans {
			if plans[i].level > 0 {
				allRecent = false
				break
			}
		}
		if allRecent {
			for i := 0; i < len(plans)-1; i++ {
				if len(plans[i].execMsgs) > 0 {
					plans[i].level = 2 // L2: summarize exec, keep user input
					log.Debugf("[SmartCompress] over budget but no candidates; force-compressing oldest segment")
					break
				}
			}
		}
	}

	// 6. Collect compressed segments for event info
	var compressedSegs []*TaskSegment
	var level3Segs []*TaskSegment
	for _, p := range plans {
		if p.level > 0 {
			compressedSegs = append(compressedSegs, p.seg)
		}
		if p.level == 3 {
			level3Segs = append(level3Segs, p.seg)
		}
	}

	// If no segments were compressed, return original messages.
	// Don't add a useless [context_compress] message that wastes tokens.
	if len(compressedSegs) == 0 {
		log.Debugf("[SmartCompress] no segments compressed — returning original")
		return messages
	}

	// 6. Execute LLM summarization (no silent timeout/skip — errors degrade to
	// first-stage and are reported via compress_error notices).
	//
	// Level 3: per-segment full summary. Older segments (beyond keep_recent_tasks)
	// are summarized inline so the conversation gist stays in context, instead of
	// being archived as opaque references that the LLM cannot see unless it calls
	// recall.
	level3Failed := make(map[int]bool)
	level3Summaries := make(map[int]string)
	// cachedArchives: segments whose hash hit the archive cache — summary and
	// summaryKey are reused verbatim; no LLM call, no re-store.
	cachedArchives := make(map[int]archivedSegment)

	// Collect all per-segment summary jobs (L3 cache misses + L2 + L1), then
	// run them concurrently. Segments are independent; serial calls made the
	// whole compression block the turn for the sum of call latencies
	// (observed live: 10 segments × 3-30s = 74s inside BeforeModel).
	type summaryJob struct {
		idx   int
		level int
		msgs  []model.Message
	}
	var jobs []summaryJob
	for i, p := range plans {
		switch p.level {
		case 3:
			if hit, ok := sc.lookupArchive(sc.segmentContentHash(p.seg)); ok {
				cachedArchives[i] = hit
				level3Summaries[i] = hit.summary
				continue
			}
			if sc.summaryModel == nil {
				level3Failed[i] = true
				continue
			}
			jobs = append(jobs, summaryJob{idx: i, level: 3, msgs: p.seg.Messages})
		case 2:
			// Skip segments with nothing to compress at this level: an empty
			// target is NOT a failure (previously the empty summary was mistaken
			// for an LLM failure → false degradation + scary error notice +
			// firstStage that barely reduces tokens, observed live as dur≈0
			// degradations with after>=before).
			if len(p.execMsgs) > 0 {
				jobs = append(jobs, summaryJob{idx: i, level: 2, msgs: p.execMsgs})
			}
		case 1:
			if len(p.nonKeyMsgs) > 0 {
				jobs = append(jobs, summaryJob{idx: i, level: 1, msgs: p.nonKeyMsgs})
			}
		}
	}

	level2Failed := make(map[int]bool)
	level1Failed := make(map[int]bool)
	level2Summaries := make(map[int]string)
	level1Summaries := make(map[int]string)

	// summaryConcurrency bounds parallel summary-model calls — a scheduling
	// knob (provider QPS courtesy), not a behavioral limit.
	const summaryConcurrency = 3
	sem := make(chan struct{}, summaryConcurrency)
	var jobWG sync.WaitGroup
	var jobMu sync.Mutex
	for _, j := range jobs {
		jobWG.Add(1)
		go func(j summaryJob) {
			defer jobWG.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			summary, hadErr := sc.summarizeMsgs(ctx, j.msgs, j.idx, len(plans))
			jobMu.Lock()
			defer jobMu.Unlock()
			failed := hadErr || summary == ""
			switch j.level {
			case 3:
				if failed {
					level3Failed[j.idx] = true
				} else {
					level3Summaries[j.idx] = summary
				}
			case 2:
				if failed {
					level2Failed[j.idx] = true
				} else {
					level2Summaries[j.idx] = summary
				}
			case 1:
				if failed {
					level1Failed[j.idx] = true
				} else {
					level1Summaries[j.idx] = summary
				}
			}
		}(j)
	}
	jobWG.Wait()

	// L3 LLM failed → degrade to L1 first-stage (drop tool, keep text)
	for i := range level3Failed {
		plans[i].level = 1
	}

	// 7. Assemble result (maintain chronological order)
	var result []model.Message
	if systemMsg != nil {
		result = append(result, *systemMsg)
	}

	// Track degraded segments for a combined error notice, injected at top
	// (before segments) so the agent sees it immediately and the pending user
	// message remains the last message in the result.
	degradedCount := 0
	for i := range plans {
		if level2Failed[i] || level1Failed[i] {
			degradedCount++
		}
	}
	if degradedCount > 0 {
		result = append(result, buildCompressErrorNotice("LLM 摘要生成失败", degradedCount))
	}

	// Chronological assembly
	for i, p := range plans {
		switch p.level {
		case 0:
			result = append(result, p.seg.Messages...)
		case 1:
			result = append(result, p.userMsgs...)
			result = append(result, p.keyMsgs...)
			if summary, ok := level1Summaries[i]; ok && summary != "" {
				result = append(result, sc.buildSegmentCompressNotice(p.nonKeyMsgs, "selective"))
				result = append(result, model.Message{
					Role: model.RoleAssistant, Content: summary,
				})
			} else {
				// LLM failed → first-stage: drop tool, keep text
				result = append(result, sc.firstStageCompress(p.nonKeyMsgs)...)
			}
		case 2:
			result = append(result, p.userMsgs...)
			if summary, ok := level2Summaries[i]; ok && summary != "" {
				result = append(result, sc.buildSegmentCompressNotice(p.execMsgs, "partial"))
				result = append(result, model.Message{
					Role: model.RoleAssistant, Content: summary,
				})
			} else {
				// LLM failed → first-stage: drop tool, keep text
				result = append(result, sc.firstStageCompress(p.execMsgs)...)
			}
		case 3:
			// Level 3: replace the entire old segment with an inline summary.
			// The segment is also archived so recall can retrieve full details.
			summary := level3Summaries[i]
			// Extract event key from first message of the segment
			segEventKey := int64(0)
			if len(p.seg.Messages) > 0 {
				k, _, _ := tagentevent.ParseEventKeyAndType(p.seg.Messages[0].Content)
				segEventKey = k
			}
			if hit, ok := cachedArchives[i]; ok {
				// Cache hit: reuse the archived artifact — no LLM, no re-store.
				result = append(result, model.NewUserMessage(
					fmt.Sprintf("〔历史归档〕[context_archive] evt_%s 已摘要归档，摘要 key=%s", tagentevent.FormatEventKey(segEventKey), tagentevent.FormatEventKey(hit.summaryKey)),
				))
			} else if sc.memStore != nil {
				summaryKey, archiveErr := sc.archiveSegment(p.seg, summary)
				if archiveErr == nil {
					sc.storeArchive(sc.segmentContentHash(p.seg), archivedSegment{summaryKey: summaryKey, summary: summary})
					// Archival note: user-side observation, never system — system is
					// reserved for instructions; mechanism-generated notes render as
					// user messages (same rationale as buildSegmentCompressNotice).
					result = append(result, model.NewUserMessage(
						fmt.Sprintf("〔历史归档〕[context_archive] evt_%s 已摘要归档，摘要 key=%s", tagentevent.FormatEventKey(segEventKey), tagentevent.FormatEventKey(summaryKey)),
					))
				} else {
					log.Warnf("[SmartCompress] archive failed for segment %d: %v", p.origIndex, archiveErr)
				}
			}
			if summary != "" {
				result = append(result, model.Message{
					Role:    model.RoleAssistant,
					Content: fmt.Sprintf("[历史摘要] %s", summary),
				})
			}
		}
	}

	// 8. Metrics
	afterTokens := sc.tokenCounter.Estimate(result)
	levelCounts := map[int]int{0: 0, 1: 0, 2: 0, 3: 0}
	for _, p := range plans {
		levelCounts[p.level]++
	}
	metrics := map[string]interface{}{
		"event":             "smart_compress",
		"before_tokens":     beforeTokens,
		"after_tokens":      afterTokens,
		"excess":            excess,
		"l0_preserved":      levelCounts[0],
		"l1_selective":      levelCounts[1],
		"l2_partial":        levelCounts[2],
		"l3_full":           levelCounts[3],
		"summary_generated": sc.summaryModel != nil && len(level3Summaries) > 0,
		"batch_count":       0,
		"degraded_count":    degradedCount,
		"duration_ms":       time.Since(startTime).Milliseconds(),
	}
	if metricsJSON, err := json.Marshal(metrics); err == nil {
		log.Infof("[SmartCompress] %s tokens=%d->%d (%+d) levels=L0:%d L1:%d L2:%d L3:%d",
			string(metricsJSON), beforeTokens, afterTokens, afterTokens-beforeTokens,
			levelCounts[0], levelCounts[1], levelCounts[2], levelCounts[3])
	}

	return result
}

// splitUserAndExec splits messages into user (RoleUser) and execution (everything else).
func splitUserAndExec(msgs []model.Message) (user, exec []model.Message) {
	for _, msg := range msgs {
		if msg.Role == model.RoleUser {
			user = append(user, msg)
		} else {
			exec = append(exec, msg)
		}
	}
	return
}

// selectiveSplit identifies key messages (worth preserving) and non-key messages
// (compressible) within an execution process.
//
// Key messages (preserved at compression level 1):
//   - Tool calls (assistant with ToolCalls): preserve function name + args
//   - Tool results: must be kept in sync with their tool calls
//     (assistant tool_call + tool result is an atomic pair)
//   - Error results (tool results containing "Error")
//   - Async tool results (system messages with [action_tool_result])
//   - Short messages (< 100 chars, not worth compressing)
//
// Important: tool_call and tool_result pairs are atomic — both are key or both are non-key.
func (sc *SmartCompressor) selectiveSplit(execMsgs []model.Message) (key, nonKey []model.Message) {
	// First pass: identify which messages are tool_call/tool_result pairs
	// and decide per-pair whether they are key or non-key.
	isKey := make([]bool, len(execMsgs))

	for i := 0; i < len(execMsgs); i++ {
		msg := &execMsgs[i]

		// Tool call (assistant with ToolCalls)
		if msg.Role == model.RoleAssistant && len(msg.ToolCalls) > 0 {
			// Find the corresponding tool result(s)
			resultStart := i + 1
			resultEnd := resultStart
			for resultEnd < len(execMsgs) && execMsgs[resultEnd].Role == model.RoleTool {
				resultEnd++
			}

			// Determine if this tool_call+result pair is key
			pairIsKey := false

			// Short tool calls (content < 100) are key
			if len(msg.Content) < 100 {
				pairIsKey = true
			}

			// Check results for errors or short content
			for j := resultStart; j < resultEnd; j++ {
				result := &execMsgs[j]
				if strings.Contains(result.Content, "Error") {
					pairIsKey = true
				}
				if len(result.Content) < 100 {
					pairIsKey = true
				}
			}

			// If pair is non-key, check if it's the last tool result in the segment
			// (last result is always key — it's the most relevant)
			if !pairIsKey && resultEnd > resultStart {
				lastResultIdx := -1
				for k := len(execMsgs) - 1; k >= 0; k-- {
					if execMsgs[k].Role == model.RoleTool {
						lastResultIdx = k
						break
					}
				}
				if lastResultIdx >= resultStart && lastResultIdx < resultEnd {
					pairIsKey = true
				}
			}

			// Mark the entire pair
			isKey[i] = pairIsKey
			for j := resultStart; j < resultEnd; j++ {
				isKey[j] = pairIsKey
			}

			// Skip past the results
			i = resultEnd - 1
			continue
		}

		// Non tool_call messages
		// Async tool results are key
		if msg.Role == model.RoleSystem && strings.Contains(msg.Content, "[action_tool_result]") {
			isKey[i] = true
			continue
		}
		// Short messages are key
		if len(msg.Content) < 100 {
			isKey[i] = true
			continue
		}
		// Default: non-key (compressible)
		isKey[i] = false
	}

	// Second pass: split
	for i, msg := range execMsgs {
		if isKey[i] {
			key = append(key, msg)
		} else {
			nonKey = append(nonKey, msg)
		}
	}
	return
}

// summarizeMsgs summarizes a list of messages using the summary model.
// Returns (summary, hadError). hadError=true means the LLM call failed;
// caller should degrade to firstStageCompress and inject an error notice.
func (sc *SmartCompressor) summarizeMsgs(ctx context.Context, msgs []model.Message, segIdx, totalSegs int) (string, bool) {
	if len(msgs) == 0 {
		return "", false
	}

	if sc.summaryModel == nil {
		return "", true // no model available → treat as error so caller degrades
	}

	seg := &TaskSegment{Messages: msgs}
	summary, hadError := sc.generateSummary(ctx, []*TaskSegment{seg}, segIdx+1, totalSegs)
	if hadError {
		return "", true
	}
	if summary == "" {
		return "", true // empty summary is treated as failure
	}
	return summary, false
}

// generatePlainSummary runs a single plain-prompt completion on the summary
// model (used by index-card condensation). Returns an error when no model is
// configured or the call fails — callers degrade engineering-side.
func (sc *SmartCompressor) generatePlainSummary(ctx context.Context, prompt string) (string, error) {
	if sc.summaryModel == nil {
		return "", fmt.Errorf("no summary model configured")
	}
	req := &model.Request{
		Messages: []model.Message{
			model.NewSystemMessage("你是一个历史记录浓缩助手。严格遵循用户的硬性要求。"),
			model.NewUserMessage(prompt),
		},
	}
	// Same reasoning-model guard as generateSummaryRecursive: reserve ample
	// output tokens so reasoning doesn't squeeze Content to empty.
	plainMaxOut := sc.effectiveSummaryMaxTokens()
	req.MaxTokens = &plainMaxOut
	respCh, err := sc.summaryModel.GenerateContent(ctx, req)
	if err != nil {
		return "", err
	}
	var result string
	var reasoning string
	for resp := range respCh {
		if resp.Error != nil {
			return "", fmt.Errorf("summary model error: %s", resp.Error.Message)
		}
		if len(resp.Choices) > 0 {
			result += resp.Choices[0].Message.Content
			reasoning += resp.Choices[0].Message.ReasoningContent
		}
	}
	// Reasoning-model fallback (see generateSummaryRecursive): use reasoning
	// content when the model left Content empty.
	if strings.TrimSpace(result) == "" && strings.TrimSpace(reasoning) != "" {
		result = reasoning
	}
	return strings.TrimSpace(result), nil
}

// firstStageCompress drops all tool-related messages from execMsgs, keeping only
// the text content of assistant and user messages. This is the "Stage 1" fallback
// strategy defined in the original smart-compress design: when LLM summarization
// is unavailable or fails, we still reduce token usage by removing bulky tool
// calls and tool results, while preserving the conversational thread.
//
// Messages dropped:
//   - assistant messages whose ONLY content is tool_calls (no text)
//   - tool result messages (role=tool)
//   - system messages containing [action_tool_result]
//
// Messages preserved (with tool_calls stripped):
//   - assistant messages with non-empty text content
//   - user messages
func (sc *SmartCompressor) firstStageCompress(execMsgs []model.Message) []model.Message {
	var result []model.Message
	for _, msg := range execMsgs {
		switch msg.Role {
		case model.RoleAssistant:
			if len(msg.ToolCalls) > 0 && msg.Content == "" {
				// Pure tool-call message with no text — drop
				continue
			}
			// Keep text, strip tool_calls
			if msg.Content != "" {
				result = append(result, model.Message{
					Role:    model.RoleAssistant,
					Content: msg.Content,
				})
			}
		case model.RoleUser:
			result = append(result, msg)
		case model.RoleTool:
			// Drop tool results
		case model.RoleSystem:
			if strings.Contains(msg.Content, "[action_tool_result]") {
				// Drop async tool results
			} else {
				result = append(result, msg)
			}
		}
	}
	return result
}

// buildCompressErrorNotice creates a system message that notifies the agent
// that compression encountered errors and degraded to first-stage strategy.
// This ensures the agent is aware of the information loss and can react
// accordingly (e.g., ask the user for clarification, recall specific events).
func buildCompressErrorNotice(reason string, degradedCount int) model.Message {
	content := fmt.Sprintf(
		"[context_compress_error] 上下文压缩遇到错误，已降级为第一阶段策略。\n"+
			"原因: %s\n"+
			"受影响片段: %d\n"+
			"降级策略: 丢弃工具调用记录，仅保留对话文本。\n"+
			"如需历史工具调用结果，请使用 recall 工具按 event_key 检索。",
		reason, degradedCount,
	)
	// Degradation notice: user-side observation, never system.
	return model.NewUserMessage("〔历史归档〕" + content)
}

// EventInfo holds extracted metadata from a compressed message's [evt_KEY|type] prefix.
type EventInfo struct {
	Key     int64
	Type    string
	Summary string
}

// buildSegmentCompressNotice creates an inline compress notice for a single segment.
// Only lists exec message keys (user input is preserved, not listed here).
// Placed AFTER user input in the assembled messages.
//
// The notice is capped in size: it lists at most maxNoticeInfos unique event keys
// and truncates the whole text to maxNoticeChars. This prevents the notice itself
// from becoming a source of token bloat when a segment contains many exec messages.
//
// Warns the agent not to repeat expensive operations that were compressed —
// regardless of whether the cause was file reads, model outputs, search results, etc.
func (sc *SmartCompressor) buildSegmentCompressNotice(execMsgs []model.Message, level string) model.Message {
	const maxNoticeInfos = 5
	maxNoticeChars := sc.maxNoticeChars
	if maxNoticeChars <= 0 {
		maxNoticeChars = DefaultMaxNoticeChars
	}

	levelDesc := map[string]string{
		"selective": "选择性压缩",
		"partial":   "部分压缩",
		"full":      "全压缩",
	}
	desc := levelDesc[level]
	if desc == "" {
		desc = level
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("[context_compress] %s: 压缩了 %d 条执行消息", desc, len(execMsgs)))

	// Extract event keys from exec messages only
	seen := make(map[int64]bool)
	var infos []EventInfo

	for _, msg := range execMsgs {
		key, evtType, remainder := tagentevent.ParseEventKeyAndType(msg.Content)
		if key > 0 && !seen[key] {
			seen[key] = true
			summary := truncate(remainder, sc.chunkSummaryLen)
			if summary == "" {
				summary = truncate(msg.Content, sc.chunkSummaryLen)
			}
			infos = append(infos, EventInfo{
				Key: key, Type: evtType, Summary: summary,
			})
			if len(infos) >= maxNoticeInfos {
				break
			}
		}
	}

	if len(infos) > 0 {
		content.WriteString("\n")
		for _, info := range infos {
			content.WriteString(fmt.Sprintf("\n- evt_%s [%s]: %s", tagentevent.FormatEventKey(info.Key), info.Type, info.Summary))
		}
		if len(execMsgs) > len(infos) {
			content.WriteString(fmt.Sprintf("\n... 还有 %d 条执行消息被压缩", len(execMsgs)-len(infos)))
		}

		// General warning: don't repeat operations that produced the compressed content.
		// The overflow cause might be anything — large files, model outputs, search results, etc.
		// The agent should not blindly re-execute the same operations to recover this information.
		content.WriteString("\n\n**不要重复执行已被压缩的操作来获取相同内容**，否则将再次触发压缩。如需查找特定信息，请使用：")
		content.WriteString("\n- `recall({\"event_keys\": [KEY]})` — 检索完整事件（如果可用）")
		content.WriteString("\n- `search_content({\"path\": \"<path>\", \"query\": \"<关键词>\"})` — 搜索特定内容")
		content.WriteString("\n- `read_file` 配合 `start_line`/`end_line` 参数 — 只读取需要的部分")
	}

	notice := content.String()
	if len(notice) > maxNoticeChars {
		notice = notice[:maxNoticeChars] + "...(通知已截断)"
	}
	// Compress notice: user-side observation, never system.
	return model.NewUserMessage("〔历史归档〕" + notice)
}

// ============================================================================
// Archive: Summary-Memory RAG Integration
// ============================================================================

// archiveSegment writes a summary of the given segment to MemoryStore and
// returns the summary EventKey. Each call creates a new archive (no dedup cache).
func (sc *SmartCompressor) archiveSegment(seg *TaskSegment, summary string) (int64, error) {
	if sc.memStore == nil {
		return 0, fmt.Errorf("archiveSegment: memStore is nil")
	}

	// Collect source event keys from the segment (provenance, I7) and derive
	// the partition from the first keyed message. The LAST keyed event is the
	// causal parent of the summary: recall_trace can walk from the curated
	// artifact back into the raw events it condenses.
	partitionID := memory.NewPartitionID()
	var sourceKeys []string
	var tailKey int64
	for _, msg := range seg.Messages {
		k, _, _ := tagentevent.ParseEventKeyAndType(msg.Content)
		if k <= 0 {
			continue
		}
		if len(sourceKeys) == 0 {
			partitionID = memory.PartitionIDFromEventKey(k)
		}
		sourceKeys = append(sourceKeys, tagentevent.FormatEventKey(k))
		tailKey = k
	}

	// Generate new Snowflake key for the summary event
	summaryKey := memory.NewSnowflakeEventKey(partitionID, 0)

	// Build summary event
	summaryEvent := memory.FullEvent{
		EventKey:     summaryKey,
		PartitionID:  partitionID,
		EventType:    tagentevent.TypeContextCompressSummary,
		EventSummary: summary,
		Content:      summary,
		Timestamp:    time.Now().UnixMilli(),
		Metadata: map[string]string{
			"content_hash": sc.segmentContentHash(seg),
			"source_keys":  strings.Join(sourceKeys, ","),
		},
	}

	if err := sc.memStore.StoreEvent(summaryKey, summaryEvent); err != nil {
		return 0, fmt.Errorf("archiveSegment: StoreEvent failed: %w", err)
	}

	// Causal-chain mount (I7): parent = the segment's tail event. Best-effort:
	// a missing relation store degrades recall_trace reach, not archiving.
	if tailKey != 0 {
		if rsp, ok := sc.memStore.(memory.RelationStoreProvider); ok {
			if err := rsp.RelationStore().SetParent(summaryKey, tailKey); err != nil {
				log.Warnf("[SmartCompress] archive SetParent failed summaryKey=%d parent=%d: %v", summaryKey, tailKey, err)
			}
		}
	}

	log.Infof("[SmartCompress] archived segment to summaryKey=%d (sources=%d)", summaryKey, len(sourceKeys))
	return summaryKey, nil
}

// archivedSegment is a cached L3 archive artifact (per content hash).
type archivedSegment struct {
	summaryKey int64
	summary    string
}

// lookupArchive returns the cached artifact for a segment content hash.
func (sc *SmartCompressor) lookupArchive(hash string) (archivedSegment, bool) {
	sc.archiveCacheMu.Lock()
	defer sc.archiveCacheMu.Unlock()
	hit, ok := sc.archiveCache[hash]
	return hit, ok
}

// storeArchive records an archived artifact under its segment content hash.
// Beyond the cap a random victim is evicted (map iteration order is
// randomized — a cheap LRU approximation; the archive itself persists in
// MemoryStore).
func (sc *SmartCompressor) storeArchive(hash string, entry archivedSegment) {
	sc.archiveCacheMu.Lock()
	defer sc.archiveCacheMu.Unlock()
	if sc.archiveCache == nil {
		sc.archiveCache = make(map[string]archivedSegment)
	}
	limit := sc.archiveCacheCap
	if limit <= 0 {
		limit = DefaultArchiveCacheCap
	}
	if len(sc.archiveCache) >= limit {
		for victim := range sc.archiveCache { // random victim via map order
			delete(sc.archiveCache, victim)
			break
		}
	}
	sc.archiveCache[hash] = entry
}

// segmentContentHash computes a stable hash of the segment's message contents.
func (sc *SmartCompressor) segmentContentHash(seg *TaskSegment) string {
	h := fnv.New64a()
	for _, msg := range seg.Messages {
		h.Write([]byte(msg.Content))
		h.Write([]byte{0}) // separator
	}
	return fmt.Sprintf("%x", h.Sum64())
}

// roleLabel returns a human-readable label for a model.Role.
func roleLabel(role model.Role) string {
	switch role {
	case model.RoleSystem:
		return "system"
	case model.RoleUser:
		return "user"
	case model.RoleAssistant:
		return "assistant"
	case model.RoleTool:
		return "tool"
	default:
		return "unknown"
	}
}

// buildBatchSummaryPrompt builds the engineering-requirements header for one
// LLM summarization batch. Requirement 3 (keep correlation identifiers: task
// id / tool_id / tool name) preserves the content-level linkage between later
// notifications/results and their originating calls after compression
// (unified-event-projection D7).
func buildBatchSummaryPrompt(segmentCount, batchIndex, totalBatches, targetChars, inputChars int) string {
	return fmt.Sprintf(
		"请对以下 %d 个历史对话片段生成摘要。这是第 %d/%d 批。\n\n"+
			"工程要求：\n"+
			"1. 保留关键语义、用户意图、执行操作和最终结果\n"+
			"2. 保留工具调用的成功/失败状态和关键返回值（如文件路径、命令输出摘要）\n"+
			"3. 保留关联标识文本：task id、tool_id、工具名——后续通知/结果需要靠它们与历史调用建立内容关联\n"+
			"4. 摘要目标长度：约 %d 字符（原始内容 %d 字符，压缩比 %.1fx）\n"+
			"5. 超出目标长度的部分必须省略，不可溢出\n"+
			"6. 使用简洁的要点式表达\n\n",
		segmentCount, batchIndex, totalBatches,
		targetChars, inputChars, float64(inputChars)/float64(targetChars),
	)
}

// splitSystemMessage separates the system message from the rest.
func SplitSystemMessage(messages []model.Message) (*model.Message, []model.Message) {
	if len(messages) == 0 {
		return nil, nil
	}
	if messages[0].Role == model.RoleSystem {
		return &messages[0], messages[1:]
	}
	return nil, messages
}

// generateSummary generates an LLM summary of old task segments.
// batchIndex and totalBatches provide context for dynamic target sizing.
// Returns the summary string and whether an error occurred during LLM invocation.
// If summaryModel is nil, returns ("", false) meaning summary not attempted.
//
// If the LLM returns a summary exceeding targetChars * 1.5, the segments are
// split into two sub-batches and each is summarized independently with halved
// targetChars. Recursion depth is limited to 2. If still oversized at the
// limit, the result is hard-truncated to targetChars.
func (sc *SmartCompressor) generateSummary(
	ctx context.Context, segments []*TaskSegment, batchIndex, totalBatches int,
) (summary string, hadError bool) {
	return sc.generateSummaryRecursive(ctx, segments, batchIndex, totalBatches, 0)
}

// effectiveMaxSummaryInputChars returns the splitting threshold for one
// summary call's input (config override or package default).
func (sc *SmartCompressor) effectiveMaxSummaryInputChars() int {
	if sc.maxSummaryInputChars > 0 {
		return sc.maxSummaryInputChars
	}
	return DefaultMaxSummaryInputChars
}

// effectiveSummaryMaxTokens returns the output-token budget floor for summary
// calls (config override or package default).
func (sc *SmartCompressor) effectiveSummaryMaxTokens() int {
	if sc.summaryMaxTokens > 0 {
		return sc.summaryMaxTokens
	}
	return DefaultSummaryMaxTokens
}

// generateSummaryRecursive is the recursive implementation with depth tracking.
func (sc *SmartCompressor) generateSummaryRecursive(
	ctx context.Context, segments []*TaskSegment, batchIndex, totalBatches, depth int,
) (summary string, hadError bool) {
	if sc.summaryModel == nil {
		return "", false
	}

	// Giant-segment splitting: bound each summary call's input by splitting an
	// oversized input into smaller message-groups, summarized separately then
	// joined. Only message-GROUP splitting is done — a single oversized message
	// is sent as-is (splitting one message's content is meaningless and loses
	// information).
	if depth < 2 {
		totalChars := 0
		for _, seg := range segments {
			for _, msg := range seg.Messages {
				totalChars += len(msg.Content)
			}
		}
		if totalChars > sc.effectiveMaxSummaryInputChars() {
			var left, right []*TaskSegment
			switch {
			case len(segments) > 1:
				mid := len(segments) / 2
				left, right = segments[:mid], segments[mid:]
			case len(segments[0].Messages) > 1:
				msgs := segments[0].Messages
				mid := len(msgs) / 2
				left = []*TaskSegment{{Messages: msgs[:mid]}}
				right = []*TaskSegment{{Messages: msgs[mid:]}}
			}
			if left != nil {
				log.Debugf("[SmartCompress] batch %d/%d input %d chars exceeds split threshold %d, splitting (depth=%d)",
					batchIndex, totalBatches, totalChars, sc.effectiveMaxSummaryInputChars(), depth)
				leftSummary, leftErr := sc.generateSummaryRecursive(ctx, left, batchIndex, totalBatches, depth+1)
				rightSummary, rightErr := sc.generateSummaryRecursive(ctx, right, batchIndex, totalBatches, depth+1)
				switch {
				case leftErr && rightErr:
					return "", true
				case !leftErr && !rightErr:
					return leftSummary + "\n" + rightSummary, false
				case !leftErr:
					return leftSummary, false
				default:
					return rightSummary, false
				}
			}
			// Single message exceeding the threshold: fall through and send as-is.
		}
	}

	// Build conversation content from old segments
	var contentBuilder strings.Builder
	for i, seg := range segments {
		contentBuilder.WriteString(fmt.Sprintf("--- 片段 %d ---\n", i+1))
		for _, msg := range seg.Messages {
			roleLabel := "unknown"
			switch msg.Role {
			case model.RoleSystem:
				roleLabel = "system"
			case model.RoleUser:
				roleLabel = "user"
			case model.RoleAssistant:
				roleLabel = "assistant"
			case model.RoleTool:
				roleLabel = "tool"
			}
			content := msg.Content
			if len(msg.ToolCalls) > 0 {
				var calls []string
				for _, tc := range msg.ToolCalls {
					calls = append(calls, fmt.Sprintf("%s(%s)", tc.Function.Name, string(tc.Function.Arguments)))
				}
				content = fmt.Sprintf("[tool_calls: %s]", strings.Join(calls, ", "))
				if msg.Content != "" {
					content = msg.Content + "\n" + content
				}
			}
			contentBuilder.WriteString(fmt.Sprintf("%s: %s\n", roleLabel, content))
		}
	}

	// Dynamic target calculation based on actual input and token budget
	inputChars := contentBuilder.Len()
	// Default compression ratio: 5x (target = 20% of input)
	targetChars := inputChars / 5
	// Cap: ensure all batch summaries fit within token budget.
	// Reserve space for system prompt + recent segments by dividing budget
	// across (totalBatches + 2) slots.
	maxCharsPerBatch := sc.maxTokens * 4 / (totalBatches + 2)
	if targetChars > maxCharsPerBatch {
		targetChars = maxCharsPerBatch
	}
	if targetChars < 200 {
		targetChars = 200 // minimum viable summary
	}

	// Build prompt with explicit engineering target
	var promptBuilder strings.Builder
	promptBuilder.WriteString(buildBatchSummaryPrompt(len(segments), batchIndex, totalBatches, targetChars, inputChars))
	promptBuilder.WriteString(contentBuilder.String())
	promptBuilder.WriteString("\n--- 摘要 ---")

	req := &model.Request{
		Messages: []model.Message{
			model.NewSystemMessage("你是一个对话摘要助手。严格按照用户指定的目标长度生成摘要。"),
			model.NewUserMessage(promptBuilder.String()),
		},
	}
	// Reserve enough output tokens for BOTH reasoning and the summary. A
	// reasoning model spends part of max_tokens on its thinking chain; with no
	// explicit budget the default reserve is consumed by reasoning and Content
	// comes back EMPTY — which degraded every segment and collapsed the context
	// to user-only messages (observed live: 65/67 segments, "summary generated
	// 0 chars"). chars≈tokens is a conservative estimate; ×2 leaves reasoning
	// headroom, with a floor for small summaries. It is a cap, not a target —
	// the model stops once the summary is produced.
	maxOut := targetChars * 2
	if floor := sc.effectiveSummaryMaxTokens(); maxOut < floor {
		maxOut = floor
	}
	req.MaxTokens = &maxOut

	log.Debugf("[SmartCompress] batch %d/%d (depth=%d): inputChars=%d targetChars=%d ratio=%.1f",
		batchIndex, totalBatches, depth, inputChars, targetChars, float64(inputChars)/float64(targetChars))

	respCh, err := sc.summaryModel.GenerateContent(ctx, req)
	if err != nil {
		log.Errorf("[SmartCompress] stage2 LLM failed: %v", err)
		return "", true
	}

	var result string
	var reasoning string
	for resp := range respCh {
		if resp.Error != nil {
			log.Errorf("[SmartCompress] stage2 response error: %v", resp.Error)
			return "", true
		}
		if len(resp.Choices) > 0 {
			result += resp.Choices[0].Message.Content
			reasoning += resp.Choices[0].Message.ReasoningContent
		}
	}
	// Reasoning-model fallback: some models emit the summary into
	// reasoning_content and leave Content empty. Without this, every such
	// segment "summarized" to 0 chars → mass degradation → the context
	// collapsed to user-only messages (observed live: 65/67 segments degraded,
	// 40 consecutive user messages). Prefer Content; use reasoning only when
	// Content is empty.
	if strings.TrimSpace(result) == "" && strings.TrimSpace(reasoning) != "" {
		log.Warnf("[SmartCompress] batch %d/%d (depth=%d): Content empty, falling back to reasoning_content (%d chars)",
			batchIndex, totalBatches, depth, len(reasoning))
		result = reasoning
	}

	log.Debugf("[SmartCompress] batch %d/%d (depth=%d): summary generated %d chars (target %d)",
		batchIndex, totalBatches, depth, len(result), targetChars)

	// Check if summary exceeds target with 50% tolerance.
	// If so, split segments and re-summarize each half independently.
	if len(result) > targetChars*3/2 && depth < 2 && len(segments) > 1 {
		log.Warnf("[SmartCompress] batch %d/%d summary %d chars exceeds target %d*1.5=%d, splitting into sub-batches (depth=%d)",
			batchIndex, totalBatches, len(result), targetChars, targetChars*3/2, depth)

		mid := len(segments) / 2
		if mid < 1 {
			mid = 1
		}
		left := segments[:mid]
		right := segments[mid:]

		var leftSummary, rightSummary string
		var leftErr, rightErr bool

		leftSummary, leftErr = sc.generateSummaryRecursive(ctx, left, batchIndex, totalBatches, depth+1)
		if !leftErr && len(right) > 0 {
			rightSummary, rightErr = sc.generateSummaryRecursive(ctx, right, batchIndex, totalBatches, depth+1)
		}

		if leftErr && rightErr {
			// Both failed — fall through to hard truncation below
		} else if !leftErr && !rightErr {
			result = leftSummary + "\n\n" + rightSummary
		} else if !leftErr {
			result = leftSummary
		} else {
			result = rightSummary
		}

		log.Debugf("[SmartCompress] batch %d/%d (depth=%d): re-summarized result %d chars",
			batchIndex, totalBatches, depth, len(result))
	}

	// Final hard truncation if still over target
	if len(result) > targetChars*3/2 {
		log.Warnf("[SmartCompress] batch %d/%d summary still %d chars after re-summarization, hard truncating to %d",
			batchIndex, totalBatches, len(result), targetChars)
		result = result[:targetChars] + "...(摘要已截断)"
	}

	return result, false
}
