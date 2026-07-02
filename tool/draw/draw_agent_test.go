package draw

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// mockModel is a minimal model.Model implementation for testing.
type mockModel struct{}

func (m *mockModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true}
	close(ch)
	return ch, nil
}

func (m *mockModel) Info() model.Info { return model.Info{Name: "mock-model"} }

// mockTool is a minimal trpctool.Tool implementation for testing.
type mockTool struct{ name string }

func (m *mockTool) Declaration() *trpctool.Declaration            { return &trpctool.Declaration{Name: m.name} }
func (m *mockTool) Call(_ context.Context, _ []byte) (any, error) { return nil, nil }

func TestNewAgent_Success(t *testing.T) {
	cfg := Config{
		Model:    &mockModel{},
		MemStore: memory.NewInMemoryStore(),
		Tools:    []trpctool.Tool{&mockTool{name: "image_gen"}},
		Prompt:   PromptConfig{Inline: "You are a draw agent."},
	}

	ta, err := NewAgent(cfg)
	require.NoError(t, err)
	require.NotNil(t, ta)
	assert.Equal(t, "draw", ta.Info().Name)

	tools := ta.Tools()
	require.Len(t, tools, 1)
	assert.Equal(t, "image_gen", tools[0].Declaration().Name)
}

func TestNewAgent_RequiresModel(t *testing.T) {
	cfg := Config{
		Prompt: PromptConfig{Inline: "You are a draw agent."},
	}

	ta, err := NewAgent(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is required")
	assert.Nil(t, ta)
}

func TestNewAgent_Defaults(t *testing.T) {
	cfg := Config{
		Model:  &mockModel{},
		Prompt: PromptConfig{Inline: "You are a draw agent."},
	}

	ta, err := NewAgent(cfg)
	require.NoError(t, err)
	require.NotNil(t, ta)
	assert.Equal(t, "draw", ta.Info().Name)
}
