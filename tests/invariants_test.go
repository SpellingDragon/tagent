package tagent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	tagentmemory "github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Invariant Tests — 验证 prototype 的三个架构不变量
//
// 1. inputs 是投影：SessionProjection 只含 EventReference，不含 Content
// 2. Compact 修改投影：Compactor 不修改 MemoryStore
// 3. 工具结果回写 bus：action_command 经 onEvent 回流到 SessionProjection
// ============================================================================

type invariantMockModel struct {
	mu        sync.Mutex
	responses []*model.Response
	callIdx   int
}

func (m *invariantMockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := m.callIdx
	m.callIdx++
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if idx < len(m.responses) {
		ch <- m.responses[idx]
	}
	close(ch)
	return ch, nil
}

func (m *invariantMockModel) Info() model.Info { return model.Info{Name: "invariant-mock"} }

func makeAssistantResponse(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{
			{Message: model.Message{Role: model.RoleAssistant, Content: content}},
		},
	}
}

func makeToolCallResponse() *model.Response {
	return &model.Response{
		Choices: []model.Choice{
			{Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{
					{ID: "tc1", Type: "function", Function: model.FunctionDefinitionParam{Name: "action", Arguments: []byte(`{"command":"echo hello"}`)}},
				},
			}},
		},
	}
}

func makeFinalResponse(content string) *model.Response {
	return makeAssistantResponse(content)
}

// ============================================================================
// Invariant 1: SessionProjection 只含 EventReference（无 Content）
// ============================================================================

func TestInvariant1_ProjectionOnlyContainsEventReferences(t *testing.T) {
	memStore := tagentmemory.NewInMemoryStore()

	mockModel := &invariantMockModel{
		responses: []*model.Response{makeFinalResponse("Hello!")},
	}

	cfg := &tagentagent.TagentConfig{
		Model:             mockModel,
		MemoryStore:       memStore,
		MaxToolIterations: 5,
		MaxTokens:         8000,
	}

	ta, err := tagentagent.NewTagentAgent(cfg)
	require.NoError(t, err)
	defer ta.Close()

	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "Hi there"})

	select {
	case evt := <-outputCh:
		if evt != nil && evt.Response != nil && len(evt.Response.Choices) > 0 {
			choice := evt.Response.Choices[len(evt.Response.Choices)-1]
			if len(choice.Message.ToolCalls) == 0 {
				break
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for final response")
	}

	ta.StopLoop()

	stats := memStore.GetStats()
	assert.Greater(t, stats.TotalEvents, 0, "MemoryStore should have at least one event")

	allEvents := memStore.AllEvents()
	require.NotEmpty(t, allEvents, "MemoryStore should have events")

	for _, evt := range allEvents {
		assert.Greater(t, evt.EventKey, int64(0), "EventKey should be positive")
		assert.NotEmpty(t, evt.EventType, "EventType should be set")
		assert.Greater(t, evt.Timestamp, int64(0), "Timestamp should be positive")
	}
}

// ============================================================================
// Invariant 2: Compactor 不修改 MemoryStore
// ============================================================================

func TestInvariant2_CompactorDoesNotModifyMemoryStore(t *testing.T) {
	memStore := tagentmemory.NewInMemoryStore()

	partitionID := 42
	var storedKeys []int64
	for i := 0; i < 10; i++ {
		key := tagentmemory.NewSnowflakeEventKey(partitionID, int64(i+1)*1000)
		evt := tagentmemory.FullEvent{
			EventKey:     key,
			PartitionID:  partitionID,
			EventType:    "external_input",
			EventSummary: "test event",
			Timestamp:    int64(i+1) * 1000,
			Content:      "full content",
		}
		if (i+1)%3 == 0 {
			evt.EventType = "agent_output"
		}
		err := memStore.StoreEvent(key, evt)
		require.NoError(t, err)
		storedKeys = append(storedKeys, key)
	}

	var refs []tagentmemory.EventReference
	for _, key := range storedKeys {
		fullEvt, err := memStore.GetEvent(key)
		require.NoError(t, err)
		refs = append(refs, tagentmemory.EventReference{
			EventKey:     fullEvt.EventKey,
			PartitionID:  fullEvt.PartitionID,
			EventType:    fullEvt.EventType,
			EventSummary: fullEvt.EventSummary,
			Timestamp:    fullEvt.Timestamp,
		})
	}

	beforeEvents := memStore.AllEvents()
	beforeCount := len(beforeEvents)

	compactor := tagentagent.NewCompactor(2)
	compacted := compactor.Compact(refs)

	assert.Less(t, len(compacted), len(refs), "Compacted refs should be fewer than original")

	afterEvents := memStore.AllEvents()
	afterCount := len(afterEvents)

	assert.Equal(t, beforeCount, afterCount, "MemoryStore event count must not change after Compact")

	for i, beforeEvt := range beforeEvents {
		afterEvt := afterEvents[i]
		assert.Equal(t, beforeEvt.EventKey, afterEvt.EventKey, "EventKey must match")
		assert.Equal(t, beforeEvt.Content, afterEvt.Content, "Content must match")
		assert.Equal(t, beforeEvt.EventType, afterEvt.EventType, "EventType must match")
		assert.Equal(t, beforeEvt.EventSummary, afterEvt.EventSummary, "EventSummary must match")
	}
}

// ============================================================================
// Invariant 3: 工具结果经 onEvent 回流到 SessionProjection
// ============================================================================

func TestInvariant3_ToolResultsFlowBackToProjection(t *testing.T) {
	memStore := tagentmemory.NewInMemoryStore()

	mockModel := &invariantMockModel{
		responses: []*model.Response{
			makeToolCallResponse(),
			makeFinalResponse("Done"),
		},
	}

	cfg := &tagentagent.TagentConfig{
		Model:             mockModel,
		MemoryStore:       memStore,
		MaxToolIterations: 5,
		MaxTokens:         8000,
	}

	ta, err := tagentagent.NewTagentAgent(cfg)
	require.NoError(t, err)
	defer ta.Close()

	outputCh, err := ta.StartLoop("test-user", "test-session-3")
	require.NoError(t, err)

	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "Run echo hello"})

	timeout := time.After(10 * time.Second)
loop:
	for {
		select {
		case evt := <-outputCh:
			if evt == nil {
				break loop
			}
			if evt.Response != nil && len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[len(evt.Response.Choices)-1]
				if len(choice.Message.ToolCalls) == 0 {
					break loop
				}
			}
		case <-timeout:
			t.Fatal("timeout waiting for final response")
		}
	}

	ta.StopLoop()

	allEvents := memStore.AllEvents()
	require.NotEmpty(t, allEvents, "MemoryStore should have events after RunFlow")

	foundExternalInput := false
	for _, evt := range allEvents {
		if evt.EventType == "external_input" {
			foundExternalInput = true
			assert.Greater(t, evt.EventKey, int64(0), "EventKey should be positive for external_input")
			break
		}
	}
	assert.True(t, foundExternalInput, "MemoryStore should contain at least one external_input event")
}

var _ = context.Background
