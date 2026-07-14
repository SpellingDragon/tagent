package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
)

func TestProjectionOrganizer_OrganizeOnce(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	projection := NewSessionProjection()

	// Store events with long summaries
	for i := int64(1); i <= 6; i++ {
		memStore.StoreEvent(i, memory.FullEvent{
			EventKey:     i,
			EventType:    tagentevent.TypeExternalInput,
			EventSummary: "this is a very long event summary that should be refined by the organizer because it exceeds the max length",
			Content:      "full content for event " + string(rune('A'+i-1)),
		})
		projection.Append(memory.EventReference{
			EventKey:     i,
			EventType:    tagentevent.TypeExternalInput,
			EventSummary: "this is a very long event summary that should be refined by the organizer because it exceeds the max length",
		})
	}

	mockModel := &mockBatchSummaryModel{
		responses: []string{"refined summary A", "refined summary B", "refined summary C"},
	}

	lastEvent := time.Now().Add(-5 * time.Minute).UnixMilli()
	organizer := NewProjectionOrganizer(
		ProjectionOrganizerConfig{
			SummaryModel:  mockModel,
			OrganizeAge:   2, // Skip last 2 refs
			BatchSize:     3, // Process max 3 per round
			MaxSummaryLen: 50,
		},
		projection,
		memStore,
		func() int64 { return lastEvent },
	)

	count := organizer.OrganizeOnce(context.Background())

	assert.Equal(t, 3, count, "should organize 3 refs (batch size limit)")

	// Verify first 3 refs have refined summaries
	refs := projection.GetAll()
	assert.Contains(t, refs[0].EventSummary, "refined summary A")
	assert.Contains(t, refs[1].EventSummary, "refined summary B")
	assert.Contains(t, refs[2].EventSummary, "refined summary C")

	// Refs 3-5 should be unchanged (either skipped or not reached)
	assert.Contains(t, refs[3].EventSummary, "very long event summary")
}

func TestProjectionOrganizer_SkipsShortSummaries(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	projection := NewSessionProjection()

	// Event with short summary (already refined)
	memStore.StoreEvent(1, memory.FullEvent{
		EventKey:  1,
		EventType: tagentevent.TypeExternalInput,
		Content:   "some content",
	})
	projection.Append(memory.EventReference{
		EventKey:     1,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "short", // Already ≤ maxSummaryLen
	})

	// Event with long summary
	memStore.StoreEvent(2, memory.FullEvent{
		EventKey:  2,
		EventType: tagentevent.TypeExternalInput,
		Content:   "some other content that is longer",
	})
	projection.Append(memory.EventReference{
		EventKey:     2,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "this is a much longer summary that needs refinement because it exceeds the max length threshold",
	})

	// Use a simple inline mock that always returns "refined"
	simpleModel := &simpleMockModel{response: "refined"}

	organizer := NewProjectionOrganizer(
		ProjectionOrganizerConfig{
			SummaryModel:  simpleModel,
			OrganizeAge:   0,
			BatchSize:     5,
			MaxSummaryLen: 50,
		},
		projection,
		memStore,
		func() int64 { return time.Now().Add(-5 * time.Minute).UnixMilli() },
	)

	count := organizer.OrganizeOnce(context.Background())
	refs := projection.GetAll()
	assert.Equal(t, 1, count, "should only organize the long summary")
	assert.Equal(t, "short", refs[0].EventSummary, "short summary unchanged")
	assert.Equal(t, "refined", refs[1].EventSummary, "long summary refined")
}

func TestProjectionOrganizer_SkipsCompressType(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	projection := NewSessionProjection()

	memStore.StoreEvent(1, memory.FullEvent{
		EventKey:  1,
		EventType: "context_compress",
		Content:   "compressed history",
	})
	projection.Append(memory.EventReference{
		EventKey:     1,
		EventType:    "context_compress",
		EventSummary: "this is a very long context_compress summary that should NOT be organized",
	})

	mockModel := &mockBatchSummaryModel{
		responses: []string{"should not be called"},
	}

	organizer := NewProjectionOrganizer(
		ProjectionOrganizerConfig{
			SummaryModel:  mockModel,
			OrganizeAge:   0,
			BatchSize:     5,
			MaxSummaryLen: 10,
		},
		projection,
		memStore,
		func() int64 { return time.Now().Add(-5 * time.Minute).UnixMilli() },
	)

	count := organizer.OrganizeOnce(context.Background())
	assert.Equal(t, 0, count, "context_compress refs should be skipped")
}

func TestProjectionOrganizer_CtxCancellation(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	projection := NewSessionProjection()

	for i := int64(1); i <= 10; i++ {
		memStore.StoreEvent(i, memory.FullEvent{
			EventKey:     i,
			EventType:    tagentevent.TypeExternalInput,
			EventSummary: "this is a very long event summary that should be refined by the organizer",
			Content:      "content",
		})
		projection.Append(memory.EventReference{
			EventKey:     i,
			EventType:    tagentevent.TypeExternalInput,
			EventSummary: "this is a very long event summary that should be refined by the organizer",
		})
	}

	// Use a model that blocks on ctx cancellation
	blockingModel := &blockingMockModel{}

	ctx, cancel := context.WithCancel(context.Background())
	organizer := NewProjectionOrganizer(
		ProjectionOrganizerConfig{
			SummaryModel:  blockingModel,
			OrganizeAge:   0,
			BatchSize:     10,
			MaxSummaryLen: 10,
		},
		projection,
		memStore,
		func() int64 { return time.Now().Add(-5 * time.Minute).UnixMilli() },
	)

	// Cancel immediately
	cancel()
	count := organizer.OrganizeOnce(ctx)
	assert.Equal(t, 0, count, "should return immediately on cancelled ctx")
}

func TestProjectionOrganizer_NotEnoughRefs(t *testing.T) {
	projection := NewSessionProjection()
	projection.Append(memory.EventReference{EventKey: 1, EventType: tagentevent.TypeExternalInput})

	organizer := NewProjectionOrganizer(
		ProjectionOrganizerConfig{
			SummaryModel: &mockBatchSummaryModel{},
			OrganizeAge:  4, // Need at least 5 refs
		},
		projection,
		memory.NewInMemoryStore(),
		func() int64 { return 0 },
	)

	count := organizer.OrganizeOnce(context.Background())
	assert.Equal(t, 0, count, "should return 0 when not enough refs")
}

func TestProjectionOrganizer_StartStop(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	projection := NewSessionProjection()

	var lastEvent atomic.Int64
	lastEvent.Store(time.Now().Add(-5 * time.Minute).UnixMilli())

	organizer := NewProjectionOrganizer(
		ProjectionOrganizerConfig{
			SummaryModel:  &mockBatchSummaryModel{},
			CheckInterval: 100 * time.Millisecond,
			MinIdleGap:    1 * time.Millisecond,
		},
		projection,
		memStore,
		func() int64 { return lastEvent.Load() },
	)

	organizer.Start()
	time.Sleep(200 * time.Millisecond)
	organizer.Stop()
	// Should not panic or hang
}

func TestProjectionOrganizer_NilModel(t *testing.T) {
	organizer := NewProjectionOrganizer(
		ProjectionOrganizerConfig{SummaryModel: nil},
		NewSessionProjection(),
		memory.NewInMemoryStore(),
		func() int64 { return 0 },
	)

	// Start should be a no-op
	organizer.Start()
	// Stop should not panic
	organizer.Stop()
}

func TestProjectionOrganizer_LLMErrors(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	projection := NewSessionProjection()

	memStore.StoreEvent(1, memory.FullEvent{
		EventKey:  1,
		EventType: tagentevent.TypeExternalInput,
		Content:   "some content",
	})
	projection.Append(memory.EventReference{
		EventKey:     1,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "this is a very long event summary that should be refined",
	})

	// Model that returns errors
	errorModel := &errorMockModel{}

	organizer := NewProjectionOrganizer(
		ProjectionOrganizerConfig{
			SummaryModel:  errorModel,
			OrganizeAge:   0,
			BatchSize:     5,
			MaxSummaryLen: 10,
		},
		projection,
		memStore,
		func() int64 { return time.Now().Add(-5 * time.Minute).UnixMilli() },
	)

	count := organizer.OrganizeOnce(context.Background())
	assert.Equal(t, 0, count, "failed LLM calls should not count as organized")

	// Original summary should be preserved
	refs := projection.GetAll()
	assert.Equal(t, "this is a very long event summary that should be refined", refs[0].EventSummary)
}

// blockingMockModel blocks on GenerateContent until ctx is cancelled.
type blockingMockModel struct{}

func (m *blockingMockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (m *blockingMockModel) Info() model.Info {
	return model.Info{Name: "blocking-mock"}
}

// errorMockModel always returns an error.
type errorMockModel struct{}

func (m *errorMockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	return nil, fmt.Errorf("mock LLM error")
}

func (m *errorMockModel) Info() model.Info {
	return model.Info{Name: "error-mock"}
}

// simpleMockModel always returns the same response string.
type simpleMockModel struct {
	response string
}

func (m *simpleMockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{
			{Message: model.Message{Role: model.RoleAssistant, Content: m.response}},
		},
	}
	close(ch)
	return ch, nil
}

func (m *simpleMockModel) Info() model.Info {
	return model.Info{Name: "simple-mock"}
}

func TestUpdateSummary_OutOfBounds(t *testing.T) {
	p := NewSessionProjection()
	p.Append(memory.EventReference{EventSummary: "original"})

	// Out of bounds should not panic
	p.UpdateSummary(-1, "bad")
	p.UpdateSummary(5, "bad")
	p.UpdateSummary(0, "updated")

	refs := p.GetAll()
	assert.Equal(t, "updated", refs[0].EventSummary)
}
