package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ProjectionOrganizerConfig is the runtime configuration for ProjectionOrganizer.
type ProjectionOrganizerConfig struct {
	// SummaryModel is the LLM model for generating refined summaries.
	// If nil, organizer is not created.
	SummaryModel model.Model

	// OrganizeAge is the minimum age (number of segments from the end) for a
	// ref to be eligible for organization. Refs within the last OrganizeAge
	// positions are considered "recent" and skipped.
	// Default: 4 (i.e., keepRecent * 2).
	OrganizeAge int

	// BatchSize is the maximum number of refs to organize per cycle.
	// Default: 5.
	BatchSize int

	// CheckInterval is how often the organizer checks for idle state.
	// Default: 5 minutes.
	CheckInterval time.Duration

	// MinIdleGap is the minimum duration since the last event before
	// organization is allowed. Default: 1 minute.
	MinIdleGap time.Duration

	// MaxSummaryLen is the maximum character length for generated summaries.
	// Default: 150.
	MaxSummaryLen int
}

// ProjectionOrganizer proactively refines EventReference summaries in the
// SessionProjection during agent idle periods.
//
// Design: independent goroutine with periodic timer, similar to MeditationManager.
// Does NOT inject events into EventBus — directly updates Projection refs.
// Only updates EventSummary (the lightweight field), never touches MemoryStore
// (the permanent full event content is preserved).
//
// Trigger: when the agent has been idle for at least MinIdleGap.
// Action: for each eligible ref, read full content from MemoryStore,
// call SummaryModel to generate a refined summary (≤MaxSummaryLen chars),
// and update the ref's EventSummary via Projection.UpdateSummary.
type ProjectionOrganizer struct {
	projection   *SessionProjection
	memStore     memory.MemoryStore
	summaryModel model.Model

	organizeAge   int
	batchSize     int
	checkInterval time.Duration
	minIdleGap    time.Duration
	maxSummaryLen int

	// lastEventTime returns the last event timestamp in Unix milliseconds.
	// Shared with MeditationManager's lastEventTime (both track the same idle state).
	lastEventTime func() int64

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewProjectionOrganizer creates a ProjectionOrganizer.
// lastEventTime is a function returning the last event's UnixMilli timestamp
// (shared with MeditationManager).
func NewProjectionOrganizer(
	cfg ProjectionOrganizerConfig,
	projection *SessionProjection,
	memStore memory.MemoryStore,
	lastEventTime func() int64,
) *ProjectionOrganizer {
	if cfg.OrganizeAge < 0 {
		cfg.OrganizeAge = 4
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 5
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 5 * time.Minute
	}
	if cfg.MinIdleGap <= 0 {
		cfg.MinIdleGap = 1 * time.Minute
	}
	if cfg.MaxSummaryLen <= 0 {
		cfg.MaxSummaryLen = 150
	}
	return &ProjectionOrganizer{
		projection:    projection,
		memStore:      memStore,
		summaryModel:  cfg.SummaryModel,
		organizeAge:   cfg.OrganizeAge,
		batchSize:     cfg.BatchSize,
		checkInterval: cfg.CheckInterval,
		minIdleGap:    cfg.MinIdleGap,
		maxSummaryLen: cfg.MaxSummaryLen,
		lastEventTime: lastEventTime,
	}
}

// Start launches the organizer goroutine.
// Must be called after the owner's event loop is active.
func (o *ProjectionOrganizer) Start() {
	if o.summaryModel == nil {
		return // No model configured, don't start
	}
	o.ctx, o.cancel = context.WithCancel(context.Background())
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		ticker := time.NewTicker(o.checkInterval)
		defer ticker.Stop()
		log.Infof("[OrganizeProjection] started: interval=%s min_idle=%s organize_age=%d batch_size=%d",
			o.checkInterval, o.minIdleGap, o.organizeAge, o.batchSize)
		for {
			select {
			case <-ticker.C:
				o.checkAndOrganize()
			case <-o.ctx.Done():
				return
			}
		}
	}()
}

// Stop signals the organizer goroutine to stop and waits for it.
func (o *ProjectionOrganizer) Stop() {
	if o.cancel != nil {
		o.cancel()
	}
	o.wg.Wait()
	log.Info("[OrganizeProjection] stopped")
}

// checkAndOrganize checks idle state and runs one round of organization if eligible.
func (o *ProjectionOrganizer) checkAndOrganize() {
	lastMs := o.lastEventTime()
	if lastMs == 0 {
		return // No events yet
	}

	idleDuration := time.Since(time.UnixMilli(lastMs))
	if idleDuration < o.minIdleGap {
		return // Agent still active
	}

	count := o.OrganizeOnce(o.ctx)
	if count > 0 {
		log.Infof("[OrganizeProjection] idle=%s, organized %d refs", idleDuration.Round(time.Second), count)
	}
}

// OrganizeOnce performs one round of projection organization.
// Returns the number of refs actually updated.
// Respects ctx cancellation — returns immediately if ctx is done.
func (o *ProjectionOrganizer) OrganizeOnce(ctx context.Context) int {
	refs := o.projection.GetAll()
	if len(refs) <= o.organizeAge {
		return 0 // Not enough refs to organize anything
	}

	// Scan from oldest to newest, skipping the most recent OrganizeAge refs.
	eligibleEnd := len(refs) - o.organizeAge
	organized := 0
	skipped := 0
	failed := 0

	for i := 0; i < eligibleEnd && organized < o.batchSize; i++ {
		// Check cancellation
		if ctx.Err() != nil {
			return organized
		}

		ref := &refs[i]

		// Skip context_compress type refs (already summaries)
		if ref.EventType == "context_compress" {
			skipped++
			continue
		}

		// Skip refs with short summaries (already refined)
		if len(ref.EventSummary) <= o.maxSummaryLen {
			skipped++
			continue
		}

		// Skip refs with no EventKey (can't fetch from MemoryStore)
		if ref.EventKey <= 0 {
			skipped++
			continue
		}

		// Fetch full content from MemoryStore
		full, err := o.memStore.GetEvent(ref.EventKey)
		if err != nil || full == nil {
			skipped++
			continue
		}

		// Determine the content to summarize
		content := full.Content
		if content == "" {
			content = full.EventSummary
		}
		if content == "" {
			skipped++
			continue
		}

		// Generate refined summary via LLM
		summary, err := o.generateRefinedSummary(ctx, content, ref.EventType)
		if err != nil || summary == "" {
			failed++
			continue
		}

		// Update the ref's EventSummary in the projection
		o.projection.UpdateSummary(i, summary)
		organized++
	}

	log.Debugf("[OrganizeProjection] organized=%d skipped=%d failed=%d", organized, skipped, failed)
	return organized
}

// generateRefinedSummary calls the LLM to produce a concise summary of the content.
func (o *ProjectionOrganizer) generateRefinedSummary(ctx context.Context, content, eventType string) (string, error) {
	if o.summaryModel == nil {
		return "", fmt.Errorf("no summary model configured")
	}

	// Truncate content for the prompt to avoid overwhelming the LLM
	promptContent := content
	if len(promptContent) > 2000 {
		promptContent = promptContent[:2000] + "..."
	}

	prompt := fmt.Sprintf("请用不超过%d个字符概括以下%s事件的关键信息。只输出摘要文本，不要添加任何前缀或标记。\n\n%s",
		o.maxSummaryLen, eventType, promptContent)

	summaryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req := &model.Request{
		Messages: []model.Message{
			model.NewUserMessage(prompt),
		},
	}

	respCh, err := o.summaryModel.GenerateContent(summaryCtx, req)
	if err != nil {
		return "", fmt.Errorf("GenerateContent: %w", err)
	}

	var result string
	for resp := range respCh {
		if resp.Error != nil {
			return "", fmt.Errorf("response error: %w", resp.Error)
		}
		if len(resp.Choices) > 0 {
			result += resp.Choices[0].Message.Content
		}
	}

	// Trim and enforce length limit
	result = trimSpace(result)
	if len(result) > o.maxSummaryLen {
		result = result[:o.maxSummaryLen]
	}

	if result == "" {
		return "", fmt.Errorf("empty summary")
	}

	return result, nil
}

// trimSpace removes leading/trailing whitespace including newlines.
func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\r' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
