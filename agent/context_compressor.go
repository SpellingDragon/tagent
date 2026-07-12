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
	// Messages is the compressed message list to send to the LLM.
	Messages []model.Message
	// RetainedRefs is the updated list of EventReferences to replace
	// the projection with after compression.
	RetainedRefs []memory.EventReference
	// Notices contains error/degradation notices injected during compression.
	Notices []model.Message
}

// ContextCompressor is the unified compression engine.
//
// It replaces both SmartCompressor (message-level compression) and Compactor
// (projection-level ref cleanup). It reads EventReferences from the
// SessionProjection, resolves them to messages via MemoryStore, applies
// value-driven L0-L3 compression, and returns:
//   - Compressed messages for the LLM
//   - Retained refs to update the projection (dropping compressed refs,
//     keeping recent ones)
//   - Error notices for engineering awareness
//
// The key difference from the old two-stage approach (SmartCompress + Compactor)
// is that compression and projection cleanup are now a single atomic operation:
// there is no window where the projection has stale refs or the messages are
// out of sync with the projection.
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

// Compress is the single entry point for context compression.
//
// Input:
//   - ctx: context for LLM calls
//   - refs: EventReferences from SessionProjection (historical context)
//   - currentMessages: messages already prepared by ContentRequestProcessor
//     (system prompt + current invocation tool results + user message)
//
// Output:
//   - Messages: compressed message list (replaces args.Request.Messages)
//   - RetainedRefs: updated refs (replaces projection)
//   - Notices: error/degradation notices
//
// Deduplication strategy:
//
// Previously, InjectEventKeys added [evt_KEY|type] prefixes to currentMessages
// positionally, and Compress used those prefixes to skip refs already present.
// This broke after compression because the projection no longer matches
// session messages 1:1 — the summary ref's key was mis-assigned to user
// messages, and tool messages (skipped by InjectEventKeys) couldn't be
// deduplicated, creating duplicates.
//
// Now, Compress resolves ALL refs from the projection (each correctly
// prefixed via resolveRef), then uses content-based deduplication to find
// new messages from ContentRequestProcessor that aren't yet in the
// projection. This eliminates positional matching entirely.
func (cc *ContextCompressor) Compress(
	ctx context.Context,
	refs []memory.EventReference,
	currentMessages []model.Message,
) CompressResult {
	startTime := time.Now()

	// Check if compression is needed based on current messages alone.
	// If under threshold, we still add [evt_KEY|type] prefixes by matching
	// currentMessages to projection refs. This ensures the LLM can see event
	// keys for sub-agent tool calls (recall, knowledge, etc.) even when no
	// compression is needed.
	usedTokens := cc.tokenCounter.Estimate(currentMessages)
	threshold := int(float64(cc.maxTokens) * cc.thresholdPct)

	if usedTokens <= threshold {
		prefixed := cc.prefixMessagesByContentMatch(ctx, currentMessages, refs)
		log.Infof("[ContextCompressor] under budget (%d <= %d), prefixed %d/%d messages",
			usedTokens, threshold, countPrefixed(prefixed), len(prefixed))
		return CompressResult{
			Messages:     prefixed,
			RetainedRefs: refs,
		}
	}

	// Over threshold — resolve ALL refs and rebuild messages.
	// 1. Resolve every ref from the projection → build a lookup map and ordered list.
	//    Each resolved message is correctly prefixed with [evt_KEY|type].
	resolvedByContent := make(map[string]model.Message)
	resolvedOrdered := make([]model.Message, 0, len(refs))
	resolvedKeysOrdered := make([]string, 0, len(refs))
	for i, ref := range refs {
		resolved := cc.resolveRef(ctx, ref, i, len(refs))
		key := dedupKey(resolved)
		if key != "" {
			resolvedByContent[key] = resolved
			resolvedOrdered = append(resolvedOrdered, resolved)
			resolvedKeysOrdered = append(resolvedKeysOrdered, key)
		}
	}

	// 2. Process currentMessages: replace with resolved versions when matched,
	//    and track which resolved refs were matched.
	systemMsg, currentBody := splitSystemMessage(currentMessages)
	matchedKeys := make(map[string]bool)
	processedBody := make([]model.Message, 0, len(currentBody))
	for _, msg := range currentBody {
		key := dedupKey(msg)
		if key != "" {
			if resolved, ok := resolvedByContent[key]; ok {
				// Use the resolved version (has [evt_KEY|type] prefix)
				processedBody = append(processedBody, resolved)
				matchedKeys[key] = true
				continue
			}
		}
		// Message not in projection (e.g., current user message, compressed
		// historical event) — keep as-is
		processedBody = append(processedBody, msg)
	}

	// 3. Find unresolved refs (in projection but not in currentMessages).
	//    These are historical events that ContentRequestProcessor didn't
	//    include (e.g., from previous invocations).
	//    IMPORTANT: preserve projection order (iterate over resolvedOrdered, not map).
	var unresolved []model.Message
	for i, key := range resolvedKeysOrdered {
		if !matchedKeys[key] {
			unresolved = append(unresolved, resolvedOrdered[i])
		}
	}

	// 4. Merge: [system] + [unresolved historical] + [processed current].
	//    Unresolved refs are historical events not in currentMessages,
	//    so they come first (chronologically before current invocation).
	merged := make([]model.Message, 0, 1+len(unresolved)+len(processedBody))
	if systemMsg != nil {
		merged = append(merged, *systemMsg)
	}
	merged = append(merged, unresolved...)
	merged = append(merged, processedBody...)

	// 5. Compress via SmartCompressor.
	originalKeepRecent := cc.compressor.KeepRecentTasks
	defer func() { cc.compressor.KeepRecentTasks = originalKeepRecent }()
	cc.compressor.KeepRecentTasks = cc.keepRecent

	compressedMsgs := cc.compressor.Compress(ctx, merged, nil)
	newTokens := cc.tokenCounter.Estimate(compressedMsgs)
	log.Infof("[ContextCompressor] SmartCompress: %d -> %d tokens (threshold=%d)",
		usedTokens, newTokens, threshold)

	// 6. Build retained refs.
	retainedRefs := cc.buildRetainedRefs(refs, compressedMsgs)

	// 7. Collect error notices.
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
	idx, total int,
) model.Message {
	// context_compress refs are summary references — use EventSummary directly.
	// Prefix with [evt_KEY|type] so buildRetainedRefs can track whether the
	// summary ref survived compression (prevents perpetual re-compression).
	if ref.EventType == tagentevent.TypeContextCompress {
		return model.Message{
			Role:    model.RoleSystem,
			Content: prefixEventKey(ref.EventSummary, ref),
		}
	}

	// Always try MemoryStore for full content, regardless of position.
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
// Unlike the old resolveReferenceToMessage, this NEVER returns
// "(用户消息，内容已压缩)" placeholder — instead it returns the EventSummary
// (even if empty) so the LLM can decide to recall if needed.
func (cc *ContextCompressor) resolveSummaryRef(ref memory.EventReference) model.Message {
	role := eventTypeToRole(ref.EventType)
	content := ref.EventSummary
	if content == "" {
		// Use a descriptive but non-misleading placeholder.
		// This tells the LLM the content exists but was summarized.
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

// dedupKey returns a normalized key for content-based deduplication.
// It strips any [evt_KEY|type] prefix and combines the message role with
// the first 200 characters of content. This allows matching messages from
// the projection (prefixed via resolveRef) with messages from
// ContentRequestProcessor (unprefixed) without relying on InjectEventKeys.
func dedupKey(msg model.Message) string {
	content := stripEventKeyPrefix(msg.Content)
	if content == "" {
		return ""
	}
	// Use first 200 chars to handle minor truncation differences between
	// MemoryStore full content and session event content.
	if len(content) > 200 {
		content = content[:200]
	}
	return string(msg.Role) + ":" + content
}

// prefixMessagesByContentMatch adds [evt_KEY|type] prefixes to currentMessages
// by matching each message against resolved projection refs. This runs when
// the context is under budget so the LLM still sees event keys for sub-agent
// tool calls (recall, knowledge, etc.). Message order and content are
// preserved; only the prefix is added.
func (cc *ContextCompressor) prefixMessagesByContentMatch(
	ctx context.Context,
	messages []model.Message,
	refs []memory.EventReference,
) []model.Message {
	if len(refs) == 0 || len(messages) == 0 {
		return messages
	}

	// Resolve refs to messages (with prefixes) and build a content → ref map.
	refByContent := make(map[string]memory.EventReference)
	for i, ref := range refs {
		resolved := cc.resolveRef(ctx, ref, i, len(refs))
		key := dedupKey(resolved)
		if key != "" {
			refByContent[key] = ref
		}
	}

	result := make([]model.Message, len(messages))
	copy(result, messages)
	for i := range result {
		msg := &result[i]
		if msg.Role == model.RoleSystem {
			continue
		}
		if strings.HasPrefix(msg.Content, "[evt_") {
			continue
		}
		dk := dedupKey(*msg)
		if ref, ok := refByContent[dk]; ok && ref.EventKey != 0 {
			msg.Content = prefixEventKey(msg.Content, ref)
		}
	}
	return result
}

// countPrefixed returns how many messages already have an [evt_ prefix.
func countPrefixed(messages []model.Message) int {
	count := 0
	for _, msg := range messages {
		if strings.HasPrefix(msg.Content, "[evt_") {
			count++
		}
	}
	return count
}

// buildRetainedRefs determines which EventReferences should be kept in the
// projection after compression.
//
// Strategy:
//   - Refs whose event keys appear in the compressed messages → retained (L0).
//   - Refs whose event keys are NOT in the compressed messages → were compressed.
//     These are replaced with a single summary ref (like Compactor did).
//   - Summary refs (negative keys) that are not retained are silently dropped
//     rather than added to compressedKeys. They represent previously compressed
//     events and will be replaced by the new summary ref.
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
		// Parse [evt_KEY|type] prefixes
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
			// This ref was retained (appeared in compressed messages)
			retained = append(retained, ref)
		} else if ref.EventKey > 0 {
			// Real event key that was compressed — add to the summary list.
			compressedKeys = append(compressedKeys, fmt.Sprintf("%d", ref.EventKey))
			if minTs == 0 || ref.Timestamp < minTs {
				minTs = ref.Timestamp
			}
		}
		// Negative keys (summary refs from previous compression) that are
		// not retained are silently dropped. They will be replaced by the
		// new summary ref below.
	}

	// If we have compressed refs, add a single summary reference.
	if len(compressedKeys) > 0 {
		if minTs == 0 {
			minTs = time.Now().UnixMilli()
		}
		summaryRef := memory.EventReference{
			EventKey:     -minTs, // Negative timestamp-based key (no collision with snowflake)
			EventType:    tagentevent.TypeContextCompress,
			EventSummary: fmt.Sprintf("[Compacted %d historical events: keys=%s]", len(compressedKeys), strings.Join(compressedKeys, ",")),
			Timestamp:    minTs,
			Role:         "system",
		}
		// Insert summary ref at the beginning, then retained refs
		retained = append([]memory.EventReference{summaryRef}, retained...)
	}

	return retained
}
