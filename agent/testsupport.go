package agent

import (
	"context"
	"sync"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/rl"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// mockTokenCounter always returns a fixed token estimate.
// 契约: docs/wiki/agent/agent-architecture.md#test-support
type mockTokenCounter struct {
	tokens int
}

func (m *mockTokenCounter) Estimate(messages []model.Message) int { return m.tokens }

// newTestContextManager creates a ContextManager for test use.
func newTestContextManager(name string, m model.Model, tools []trpctool.Tool, outputCh chan *event.Event, bus *EventBus) *ContextManager {
	compressor := compress.NewSmartCompressor(compress.WithMaxTokens(8000), compress.WithTokenCounter(&mockTokenCounter{tokens: 100}))
	memStore := memory.NewInMemoryStore()
	memPlugin := plugin.NewMemoryPlugin(memStore, plugin.WithCallIDResolver(rl.CallIDForResponse))
	sessionSvc := sessioninmemory.NewSessionService()
	cm := NewContextManager(ContextManagerConfig{
		Name:         name,
		UserID:       "test-user",
		SessionID:    "test-session",
		Model:        m,
		Tools:        tools,
		MaxToolIters: 10,
		Compressor:   compressor,
		TokenCounter: &mockTokenCounter{tokens: 100},
		MaxTokens:    8000,
		ThresholdPct: 0.8,
		MemStore:     memStore,
		MemPlugin:    memPlugin,
		SessionSvc:   sessionSvc,
		OutputCh:     outputCh,
		Bus:          bus,
		Projection:   compress.NewSessionProjection(),
		OnEvent:      func(evt *event.Event) {},
	})
	return cm
}

// recordableMockModel mockModel records the request it receives and returns a preset response.
// This shared mock is used by both package tests and PoC tests.
type recordableMockModel struct {
	mu          sync.Mutex
	lastRequest *model.Request
	response    *model.Response
}

func newRecordableMockModel(response *model.Response) *recordableMockModel {
	return &recordableMockModel{response: response}
}

func (m *recordableMockModel) GenerateContent(
	ctx context.Context,
	request *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.lastRequest = request
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if m.response != nil {
		ch <- m.response
	}
	close(ch)
	return ch, nil
}

func (m *recordableMockModel) Info() model.Info {
	return model.Info{Name: "mock-model"}
}

func (m *recordableMockModel) GetLastRequest() *model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRequest
}

// sequenceMockModel multiCallModel returns responses in sequence for tests that need
// multiple model invocations (e.g., tool call followed by final response).
type sequenceMockModel struct {
	responses []*model.Response
	callCount *int
	mu        sync.Mutex
}

func (m *sequenceMockModel) GenerateContent(
	ctx context.Context,
	request *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := *m.callCount
	*m.callCount++
	var resp *model.Response
	if idx < len(m.responses) {
		resp = m.responses[idx]
	}
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if resp != nil {
		ch <- resp
	}
	close(ch)
	return ch, nil
}

func (m *sequenceMockModel) Info() model.Info {
	return model.Info{Name: "multi-call-model"}
}

// mockModel is a simple mock for tests that don't need request recording.
type mockModel struct {
	info model.Info
}

func (m *mockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		ID:    "test",
		Model: m.info.Name,
		Done:  true,
		Choices: []model.Choice{{
			Message: model.Message{Role: model.RoleAssistant, Content: "mock response"},
		}},
	}
	close(ch)
	return ch, nil
}

func (m *mockModel) Info() model.Info { return m.info }
