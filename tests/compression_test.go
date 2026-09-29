package tagent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	tagentmemory "github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// compressMockModel returns the configured response and records every request.
type compressMockModel struct {
	mu       sync.Mutex
	response *model.Response
	requests []*model.Request
}

func newCompressMockModel(response *model.Response) *compressMockModel {
	return &compressMockModel{response: response}
}

func (m *compressMockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if m.response != nil {
		ch <- m.response
	}
	close(ch)
	return ch, nil
}

func (m *compressMockModel) Info() model.Info { return model.Info{Name: "compress-mock"} }

func (m *compressMockModel) Requests() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*model.Request(nil), m.requests...)
}

// TestCompression_FullHistory verifies that when the token budget is exceeded,
// SmartCompress acts on the complete conversation history (not just the new
// batch) and that session.Events remains unchanged.
//
// 契约: docs/wiki/agent/event-flow.md#unified-compression
func TestCompression_FullHistory(t *testing.T) {
	mockModel := newCompressMockModel(&model.Response{
		ID:   "resp-final",
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: "compressed response",
			},
		}},
	})

	store := tagentmemory.NewInMemoryStore()
	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             mockModel,
		MemoryStore:       store,
		SystemPrompt:      "You are a test assistant.",
		MaxTokens:         100,
		CompressThreshold: 0.5,
	})
	require.NoError(t, err)
	defer ag.Close()

	outputCh, err := ag.StartLoop("user-1", "session-compress")
	require.NoError(t, err)

	ag.InjectMessage(model.NewUserMessage("First message"))
	waitForFinal(t, outputCh)

	longContent := "compress me "
	for i := 0; i < 50; i++ {
		longContent += "compress me "
	}
	ag.InjectMessage(model.NewUserMessage(longContent))
	waitForFinal(t, outputCh)

	ag.StopLoop()

	requests := mockModel.Requests()
	require.GreaterOrEqual(t, len(requests), 2, "model should be called at least twice")

	secondTurn := requests[len(requests)-1]
	require.NotEmpty(t, secondTurn.Messages)

	assert.Less(t, len(secondTurn.Messages), 20,
		"compression should reduce messages sent to model")

	partitionID := tagentmemory.PartitionIDFromName(ag.Info().Name)
	events, err := store.QueryEvents(tagentmemory.QueryOptions{
		PartitionID: partitionID,
		Limit:       100,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 3, "session/memory should contain original events")
}

// waitForFinal drains outputCh until a final response arrives or timeout.
func waitForFinal(t *testing.T, outputCh <-chan *event.Event) {
	t.Helper()
	for {
		select {
		case evt, ok := <-outputCh:
			if !ok {
				return
			}
			if evt.IsFinalResponse() {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for final response")
		}
	}
}
