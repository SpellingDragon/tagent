package agent

import (
	"github.com/SpellingDragon/tagent/agent/compress"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
)

// mockTokenCounter is a simple token counter that always returns a fixed estimate.
type mockTokenCounter struct {
	tokens int
}

func (m *mockTokenCounter) Estimate(messages []model.Message) int { return m.tokens }

// newTestContextManager creates a ContextManager for test use.
func newTestContextManager(name string, m model.Model, tools []trpctool.Tool, outputCh chan *event.Event, bus *EventBus) *ContextManager {
	compressor := compress.NewSmartCompressor(compress.WithMaxTokens(8000), compress.WithTokenCounter(&mockTokenCounter{tokens: 100}))
	memStore := memory.NewInMemoryStore()
	memPlugin := plugin.NewMemoryPlugin(memStore)
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
