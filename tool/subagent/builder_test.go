// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package subagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestAssembledAgentServes runs one turn of the agent an assembly describes:
// - every declared reference is resolved through its injected resolver and handed to the tRPC builder
// - the instruction the assembly carries reaches the model request of the turn
// - the turn reports the assistant text the model answered with
// - an assembly naming no model, an unresolved model and a declared reference list with no resolver are refused by name
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestAssembledAgentServes(t *testing.T) {
	ctx := context.Background()

	t.Run("one turn runs on the resolved model with the resolved references", func(t *testing.T) {
		provider := &serveModel{name: "model-reviewer", answer: "review recorded"}
		var skills, tools, mcp []string
		builder := Builder{
			Model: func(_ context.Context, name string) (trpcmodel.Model, error) {
				if name != "model-reviewer" {
					t.Fatalf("model reference = %q, want the declared model", name)
				}
				return provider, nil
			},
			Skills: func(_ context.Context, names []string) (skill.Repository, error) {
				skills = names
				return serveSkills{}, nil
			},
			Tools: func(_ context.Context, names []string) ([]trpctool.Tool, error) {
				tools = names
				return []trpctool.Tool{serveTool{name: "exec_run"}}, nil
			},
			MCP: func(_ context.Context, names []string) ([]trpctool.ToolSet, error) {
				mcp = names
				return []trpctool.ToolSet{serveToolSet{name: "mcp-lsp"}}, nil
			},
		}
		assembly := &Assembly{
			Definition: Definition{
				Name:        "reviewer",
				Description: "reviews one change",
				Model:       "model-reviewer",
				Skills:      []string{"go-service-ops"},
				Tools:       []string{"exec_run"},
				MCP:         []string{"mcp-lsp"},
			},
			Instruction: "Review the change and report findings.",
		}
		result, err := builder.Serve(ctx, assembly, Turn{
			UserID:    "tenant-review",
			SessionID: "session-review",
			Input:     "the change under review",
		})
		if err != nil {
			t.Fatalf("serve one turn = %v, want the assembled agent to answer", err)
		}
		if result.Content != "review recorded" {
			t.Fatalf("turn content = %q, want the assistant text the model answered with", result.Content)
		}
		requests := provider.recorded()
		if len(requests) != 1 {
			t.Fatalf("model requests = %d, want exactly one turn", len(requests))
		}
		if !carriesInstruction(requests[0], "Review the change and report findings.") {
			t.Fatalf("model request carries no instruction: %+v", requests[0].Messages)
		}
		if !carriesContent(requests[0], "the change under review") {
			t.Fatalf("model request carries no input: %+v", requests[0].Messages)
		}
		requireNames(t, "skills", skills, []string{"go-service-ops"})
		requireNames(t, "tools", tools, []string{"exec_run"})
		requireNames(t, "mcp", mcp, []string{"mcp-lsp"})
	})

	t.Run("an assembly naming no model is refused", func(t *testing.T) {
		builder := Builder{Model: func(context.Context, string) (trpcmodel.Model, error) {
			return &serveModel{}, nil
		}}
		_, err := builder.Build(ctx, &Assembly{Definition: Definition{Name: "reviewer"}})
		requireRefusal(t, err, ErrModelMissing)
	})

	t.Run("a model reference resolving to no model is refused", func(t *testing.T) {
		builder := Builder{Model: func(context.Context, string) (trpcmodel.Model, error) { return nil, nil }}
		_, err := builder.Build(ctx, &Assembly{Definition: Definition{Name: "reviewer", Model: "model-reviewer"}})
		requireRefusal(t, err, ErrModelUnresolved)
	})

	t.Run("a declared reference list with no resolver is refused", func(t *testing.T) {
		builder := Builder{Model: func(context.Context, string) (trpcmodel.Model, error) {
			return &serveModel{}, nil
		}}
		assembly := &Assembly{Definition: Definition{
			Name:   "reviewer",
			Model:  "model-reviewer",
			Skills: []string{"go-service-ops"},
		}}
		_, err := builder.Build(ctx, assembly)
		requireRefusal(t, err, ErrResolverMissing)
	})

	t.Run("a turn with no assembly is refused", func(t *testing.T) {
		_, err := Builder{}.Serve(ctx, nil, Turn{Input: "review"})
		if !errors.Is(err, ErrAssemblyMissing) {
			t.Fatalf("serve without an assembly = %v, want the named refusal", err)
		}
	})
}

// serveModel is the model stand-in of the turn test: it records every request and
// streams one scripted final answer.
type serveModel struct {
	mu       sync.Mutex
	name     string
	answer   string
	requests []trpcmodel.Request
}

// GenerateContent records the request and streams the scripted final answer.
func (m *serveModel) GenerateContent(_ context.Context, req *trpcmodel.Request) (<-chan *trpcmodel.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	answer := m.answer
	m.mu.Unlock()
	ch := make(chan *trpcmodel.Response, 1)
	ch <- &trpcmodel.Response{
		Done:    true,
		Choices: []trpcmodel.Choice{{Message: trpcmodel.NewAssistantMessage(answer)}},
	}
	close(ch)
	return ch, nil
}

// Info reports the model descriptor.
func (m *serveModel) Info() trpcmodel.Info { return trpcmodel.Info{Name: m.name} }

// recorded reports the requests the model was driven with.
func (m *serveModel) recorded() []trpcmodel.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]trpcmodel.Request(nil), m.requests...)
}

// serveTool is the tool stand-in of the turn test.
type serveTool struct {
	name string
}

// Declaration returns the declaration naming the stand-in tool.
func (s serveTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: s.name}
}

// serveToolSet is the tool set stand-in of the turn test.
type serveToolSet struct {
	name string
}

// Tools reports the tools the stand-in set carries.
func (s serveToolSet) Tools(context.Context) []trpctool.Tool { return nil }

// Close releases the stand-in set.
func (s serveToolSet) Close() error { return nil }

// Name reports the stand-in set name.
func (s serveToolSet) Name() string { return s.name }

// serveSkills is the skill repository stand-in of the turn test.
type serveSkills struct{}

// Summaries reports the skills the stand-in repository holds.
func (serveSkills) Summaries() []skill.Summary { return nil }

// Get reports the named skill; the stand-in repository holds none.
func (serveSkills) Get(name string) (*skill.Skill, error) {
	return nil, errors.New("the stand-in repository holds no skill " + name)
}

// Path reports the directory of the named skill; the stand-in repository holds none.
func (serveSkills) Path(name string) (string, error) {
	return "", errors.New("the stand-in repository holds no skill " + name)
}

// carriesInstruction reports whether one request carries the instruction as a system message.
func carriesInstruction(req trpcmodel.Request, instruction string) bool {
	for _, message := range req.Messages {
		if message.Role == trpcmodel.RoleSystem && strings.Contains(message.Content, instruction) {
			return true
		}
	}
	return false
}

// carriesContent reports whether one request carries the given content in any message.
func carriesContent(req trpcmodel.Request, content string) bool {
	for _, message := range req.Messages {
		if strings.Contains(message.Content, content) {
			return true
		}
	}
	return false
}
