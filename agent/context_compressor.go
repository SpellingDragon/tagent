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

	// Resolve ALL refs from the projection into NATIVE timeline messages
	// (D3 v2): assistant tool_calls and role=tool results keep protocol form —
	// the model sees exactly its training distribution, leaving no textual
	// call syntax to imitate. Pairing legality is enforced HERE at render time
	// (single place): a result whose call is not in the rendered sequence
	// (compacted away, or id lost) is demoted to a user-side input note —
	// content preserved, so ANY compression window cut stays legal without
	// the compressor being pairing-aware.
	declared := make(map[string]bool)
	consumed := make(map[string]bool)
	resolved := make([]model.Message, 0, len(refs))
	for _, ref := range refs {
		msg := cc.resolveRef(ctx, ref)
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

// resolveRef resolves a single EventReference to a native timeline message.
// Full content comes from MemoryStore when available, falling back to the
// reference's EventSummary. The result is tagged with [evt_KEY|type] so
// SmartCompressor and buildRetainedRefs can track retained refs.
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

	content := ""
	var toolCalls []model.ToolCall
	toolID := ""
	resolved := false
	if cc.memStore != nil && ref.EventKey > 0 {
		full, err := cc.memStore.GetEvent(ref.EventKey)
		if err == nil && full != nil {
			toolCalls = full.ToolCalls
			toolID = full.ToolID
			switch {
			// FullEvent.Content is the authoritative (sanitized-at-storage) text;
			// prefer it over the raw Response message.
			case full.Content != "" || len(full.ToolCalls) > 0:
				content = full.Content
				resolved = true
			case full.Response != nil && len(full.Response.Choices) > 0:
				m := full.Response.Choices[0].Message
				content = m.Content
				if len(m.ToolCalls) > 0 {
					toolCalls = m.ToolCalls
				}
				if toolID == "" {
					toolID = m.ToolID
				}
				resolved = true
			case full.EventSummary != "":
				content = full.EventSummary
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
			Role:    eventTypeToRole(ref.EventType),
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
	if ref.EventKey == 0 || strings.HasPrefix(content, "[evt_") {
		return content
	}
	eventType := ref.EventType
	if eventType == "" {
		eventType = "unknown"
	}
	return fmt.Sprintf("[evt_%s|%s] %s", tagentevent.FormatEventKey(ref.EventKey), eventType, content)
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
			compressedKeys = append(compressedKeys, tagentevent.FormatEventKey(ref.EventKey))
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
