package agent

import (
	"context"
	"fmt"
	"strings"
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
	// full content from MemoryStore. Older refs use EventSummary.
	recentFullCount int
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
	return &ContextCompressor{
		compressor:      sc,
		memStore:        memStore,
		tokenCounter:    tokenCounter,
		maxTokens:       maxTokens,
		thresholdPct:    thresholdPct,
		keepRecent:      keepRecent,
		recentFullCount: 4,
	}
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

	// Resolve ALL refs from the projection into messages.
	// Each resolved message is tagged with [evt_KEY|type] prefix for
	// buildRetainedRefs tracking after compression.
	resolved := make([]model.Message, 0, len(refs))
	for _, ref := range refs {
		msg := cc.resolveRef(ctx, ref)
		resolved = append(resolved, msg)
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
	retainedRefs := cc.buildRetainedRefs(refs, compressedMsgs)

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

// resolveRef resolves a single EventReference to a model.Message.
// Always tries MemoryStore first for full content; falls back to EventSummary.
// The returned message is tagged with [evt_KEY|type] so that downstream
// SmartCompressor and buildRetainedRefs can track which projection refs are
// retained after compression.
func (cc *ContextCompressor) resolveRef(
	ctx context.Context,
	ref memory.EventReference,
) model.Message {
	// context_compress refs are summary references — use EventSummary directly.
	if ref.EventType == tagentevent.TypeContextCompress {
		return model.Message{
			Role:    model.RoleSystem,
			Content: prefixEventKey(ref.EventSummary, ref),
		}
	}

	// Always try MemoryStore for full content.
	if cc.memStore != nil && ref.EventKey > 0 {
		full, err := cc.memStore.GetEvent(ref.EventKey)
		if err == nil && full != nil {
			if full.Response != nil && len(full.Response.Choices) > 0 {
				msg := full.Response.Choices[0].Message
				msg.Content = prefixEventKey(msg.Content, ref)
				return msg
			}
			if full.Content != "" || len(full.ToolCalls) > 0 {
				return model.Message{
					Role:      eventTypeToRole(ref.EventType),
					Content:   prefixEventKey(full.Content, ref),
					ToolCalls: full.ToolCalls,
				}
			}
			if full.EventSummary != "" {
				return model.Message{
					Role:    eventTypeToRole(ref.EventType),
					Content: prefixEventKey(full.EventSummary, ref),
				}
			}
		}
	}

	// Fallback: use EventSummary from the reference.
	return cc.resolveSummaryRef(ref)
}

// resolveSummaryRef builds a message from EventReference's summary fields.
func (cc *ContextCompressor) resolveSummaryRef(ref memory.EventReference) model.Message {
	role := eventTypeToRole(ref.EventType)
	content := ref.EventSummary
	if content == "" {
		content = fmt.Sprintf("(历史事件摘要为空，可用 recall 检索)")
	}
	return model.Message{
		Role:    role,
		Content: prefixEventKey(content, ref),
	}
}

// prefixEventKey prepends "[evt_KEY|type]" to content when the reference has a
// valid key and the content is not already prefixed. This prefix is the
// lightweight metadata channel that lets SmartCompressor and buildRetainedRefs
// track which projection refs survive compression.
func prefixEventKey(content string, ref memory.EventReference) string {
	if ref.EventKey == 0 || strings.HasPrefix(content, "[evt_") {
		return content
	}
	eventType := ref.EventType
	if eventType == "" {
		eventType = "unknown"
	}
	return fmt.Sprintf("[evt_%d|%s] %s", ref.EventKey, eventType, content)
}

// stripEventKeyPrefix removes a leading [evt_KEY|type] prefix from content.
// Returns the original content if no prefix is found.
func stripEventKeyPrefix(content string) string {
	_, _, remainder := parseEventKeyAndType(content)
	return remainder
}

// buildRetainedRefs determines which EventReferences should be kept in the
// projection after compression.
//
// Strategy:
//   - Refs whose event keys appear in the compressed messages → retained.
//   - Refs whose event keys are NOT in the compressed messages → were compressed.
//     These are replaced with a single summary ref.
//   - Summary refs (negative keys) that are not retained are silently dropped.
func (cc *ContextCompressor) buildRetainedRefs(
	originalRefs []memory.EventReference,
	compressedMsgs []model.Message,
) []memory.EventReference {
	if len(originalRefs) == 0 {
		return nil
	}

	// Collect all event keys present in compressed messages.
	retainedKeys := make(map[int64]bool)
	for _, msg := range compressedMsgs {
		content := msg.Content
		for {
			key, _, remainder := parseEventKeyAndType(content)
			if key <= 0 {
				break
			}
			retainedKeys[key] = true
			content = remainder
		}
	}

	// Build retained refs: keep refs whose keys are in compressed messages,
	// and replace compressed refs with a single summary ref.
	var retained []memory.EventReference
	var compressedKeys []string
	var minTs int64

	for _, ref := range originalRefs {
		if ref.EventKey == 0 {
			continue
		}
		if retainedKeys[ref.EventKey] {
			retained = append(retained, ref)
		} else if ref.EventKey > 0 {
			compressedKeys = append(compressedKeys, fmt.Sprintf("%d", ref.EventKey))
			if minTs == 0 || ref.Timestamp < minTs {
				minTs = ref.Timestamp
			}
		}
	}

	// If we have compressed refs, add a single summary reference.
	if len(compressedKeys) > 0 {
		if minTs == 0 {
			minTs = time.Now().UnixMilli()
		}
		summaryRef := memory.EventReference{
			EventKey:     -minTs,
			EventType:    tagentevent.TypeContextCompress,
			EventSummary: fmt.Sprintf("[Compacted %d historical events: keys=%s]", len(compressedKeys), strings.Join(compressedKeys, ",")),
			Timestamp:    minTs,
			Role:         "system",
		}
		retained = append([]memory.EventReference{summaryRef}, retained...)
	}

	return retained
}

// extractCurrentTurnMessages identifies messages from the current ReAct
// iteration that have NOT been persisted to the projection yet.
//
// args.Request.Messages has this structure (from framework ContentRequestProcessor + ReAct loop):
//
//	[system] [ContentRequestProcessor msgs (no prefix)] [ReAct iteration msgs (no prefix)]
//
// ContentRequestProcessor provides user messages from session history — these
// are ALREADY in the projection (added via AppendEventHook). ReAct iteration
// produces assistant (with tool_calls) and tool (result) messages — these are
// NOT yet in the projection.
//
// Strategy:
//  1. Scan from tail backwards to find the boundary between projection-resolved
//     messages (with [evt_ prefix) and framework-provided messages (no prefix).
//  2. From the unprefixed tail, filter out user messages (from ContentRequestProcessor,
//     already in projection). Keep only assistant and tool messages (current ReAct turn).
func extractCurrentTurnMessages(messages []model.Message, filterUnprefixedUser bool) []model.Message {
	if len(messages) == 0 {
		return nil
	}

	// Find the last message that has an [evt_ prefix or is a system message.
	// Everything after that is "current turn" (from framework, no prefix).
	lastPrefixedIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == model.RoleSystem {
			lastPrefixedIdx = i
			break
		}
		if strings.HasPrefix(msg.Content, "[evt_") {
			lastPrefixedIdx = i
			break
		}
	}

	var tail []model.Message
	if lastPrefixedIdx < 0 {
		// No prefixed messages found — all non-system messages are from framework.
		for _, msg := range messages {
			if msg.Role != model.RoleSystem {
				tail = append(tail, msg)
			}
		}
	} else {
		tail = messages[lastPrefixedIdx+1:]
	}

	if len(tail) == 0 {
		return nil
	}

	// Filter out session echoes while preserving ReAct-internal messages.
	//
	// filterUnprefixedUser is determined by the caller (BeforeModel callback):
	//   - false: sub-agent mode OR projection has no user yet — keep user messages
	//   - true:  persistent loop where projection already has the user — drop echoes
	//
	// ReAct-internal messages (assistant with tool_calls + tool results) are
	// always kept unconditionally.
	var result []model.Message
	for _, msg := range tail {
		if strings.HasPrefix(msg.Content, "[evt_") {
			// Projection-sourced message (shouldn't happen after
			// lastPrefixedIdx cut, defensive).
			result = append(result, msg)
			continue
		}
		switch msg.Role {
		case model.RoleUser:
			if !filterUnprefixedUser {
				result = append(result, msg)
			}
		case model.RoleAssistant:
			if len(msg.ToolCalls) == 0 {
				// Prior agent_output surfaced by ContentRequestProcessor.
				continue
			}
			result = append(result, msg)
		case model.RoleTool:
			// Current-turn tool result (paired with the assistant tool_calls above).
			result = append(result, msg)
		default:
			// Drop unprefixed system messages — they duplicate the
			// projection/system prompt already present in args.
			continue
		}
	}
	return result
}
