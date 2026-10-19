// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// builder maps one resolved assembly onto the tRPC agent builder and runs one turn of
// the agent that builder returns. The provider, the stream and the tool protocol stay
// the tRPC agent's own: this file resolves the reference names an assembly carries and
// drains the events of one turn.
package subagent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	trpcevent "trpc.group/trpc-go/trpc-agent-go/event"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ModelResolver resolves one model reference name into the model it names.
type ModelResolver func(ctx context.Context, name string) (trpcmodel.Model, error)

// SkillResolver resolves one skill reference list into the repository holding them.
type SkillResolver func(ctx context.Context, names []string) (skill.Repository, error)

// ToolResolver resolves one tool reference list into the tools they name.
type ToolResolver func(ctx context.Context, names []string) ([]trpctool.Tool, error)

// ToolSetResolver resolves one MCP server reference list into the tool sets they name.
type ToolSetResolver func(ctx context.Context, names []string) ([]trpctool.ToolSet, error)

// Builder resolves one assembly and hands it to the tRPC agent builder. Every resolver
// is injected: this package holds no model registry, no skill repository and no tool
// store of its own.
type Builder struct {
	// Model resolves the model reference an assembly names; an assembly that names a
	// model with no resolver attached is refused by name.
	Model ModelResolver
	// Skills resolves the skill references into the repository holding them.
	Skills SkillResolver
	// Tools resolves the tool references into the tools they name.
	Tools ToolResolver
	// MCP resolves the MCP server references into the tool sets they name.
	MCP ToolSetResolver
	// Sessions is the session service a delegated turn runs on; nil gives every turn
	// an in-memory session service of its own.
	Sessions session.Service
}

// ErrAssemblyMissing reports a build or a turn asked for without an assembly.
var ErrAssemblyMissing = errors.New("subagent: no assembly was given")

// ErrModelMissing reports an assembly that names no model a turn could run on.
var ErrModelMissing = errors.New("subagent: the assembly names no model")

// ErrModelUnresolved reports a model reference a resolver resolved to no model.
var ErrModelUnresolved = errors.New("subagent: the model reference resolved to no model")

// ErrResolverMissing reports a declared reference list the builder holds no resolver for.
var ErrResolverMissing = errors.New("subagent: no resolver is attached for the declared references")

// ErrTurnFailed reports a turn whose stream reached a terminal error.
var ErrTurnFailed = errors.New("subagent: the delegated turn failed")

// TurnResult is the outcome of one turn: the assistant text the turn ended on.
type TurnResult struct {
	// Content is the assistant text of the turn; empty when the turn produced none.
	Content string
}

// Turn names one delegated turn: the session the application selected for it and the
// request it starts from.
type Turn struct {
	// UserID is the user the selected session belongs to.
	UserID string
	// SessionID is the session the delegated turn runs in.
	SessionID string
	// Input is the request the turn starts from; empty runs on the instruction alone.
	Input string
}

// Build returns the live agent one assembly describes: the model, the skill repository,
// the tools and the MCP tool sets the assembly names are resolved through the injected
// resolvers and handed to the tRPC builder, and a reference that cannot be resolved
// refuses the build with no agent beside it.
func (b Builder) Build(ctx context.Context, a *Assembly) (trpcagent.Agent, error) {
	if a == nil {
		return nil, ErrAssemblyMissing
	}
	model, err := b.resolveModel(ctx, a)
	if err != nil {
		return nil, err
	}
	opts := []llmagent.Option{
		llmagent.WithModel(model),
		llmagent.WithDescription(a.Description),
		llmagent.WithInstruction(a.Instruction),
	}
	if len(a.Skills) > 0 {
		repository, err := b.resolveSkills(ctx, a.Skills)
		if err != nil {
			return nil, err
		}
		opts = append(opts, llmagent.WithSkills(repository))
	}
	if len(a.Tools) > 0 {
		tools, err := b.resolveTools(ctx, a.Tools)
		if err != nil {
			return nil, err
		}
		opts = append(opts, llmagent.WithTools(tools))
	}
	if len(a.MCP) > 0 {
		sets, err := b.resolveToolSets(ctx, a.MCP)
		if err != nil {
			return nil, err
		}
		opts = append(opts, llmagent.WithToolSets(sets))
	}
	return llmagent.New(a.Name, opts...), nil
}

// Serve builds the agent of one assembly and runs one turn of it through the tRPC
// runner, reporting the assistant text the turn ended on. The runner owns the session
// append of the turn, so a turn that ends never waits on a completion no one signals.
func (b Builder) Serve(ctx context.Context, a *Assembly, turn Turn) (TurnResult, error) {
	built, err := b.Build(ctx, a)
	if err != nil {
		return TurnResult{}, err
	}
	opts := make([]runner.Option, 0, 1)
	if b.Sessions != nil {
		opts = append(opts, runner.WithSessionService(b.Sessions))
	}
	driven := runner.NewRunner(a.Name, built, opts...)
	defer func() { _ = driven.Close() }()
	var input trpcmodel.Message
	if turn.Input != "" {
		input = trpcmodel.NewUserMessage(turn.Input)
	}
	events, err := driven.Run(ctx, turn.UserID, turn.SessionID, input)
	if err != nil {
		return TurnResult{}, err
	}
	return reduce(ctx, events)
}

// reduce drains one turn's stream into its single outcome: the assistant text the turn
// ended on, or the named terminal error the stream reported.
func reduce(ctx context.Context, events <-chan *trpcevent.Event) (TurnResult, error) {
	var out TurnResult
	for event := range events {
		if event == nil || event.Response == nil {
			continue
		}
		if event.IsTerminalError() {
			return out, fmt.Errorf("%w: %s", ErrTurnFailed, event.Response.Error.Message)
		}
		for _, choice := range event.Choices {
			if choice.Message.Role == trpcmodel.RoleAssistant && choice.Message.Content != "" {
				out.Content = choice.Message.Content
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// resolveModel settles the model one assembly runs on.
func (b Builder) resolveModel(ctx context.Context, a *Assembly) (trpcmodel.Model, error) {
	if strings.TrimSpace(a.Model) == "" {
		return nil, ErrModelMissing
	}
	if b.Model == nil {
		return nil, fmt.Errorf("%w: model %q", ErrResolverMissing, a.Model)
	}
	model, err := b.Model(ctx, a.Model)
	if err != nil {
		return nil, fmt.Errorf("subagent: model %q: %w", a.Model, err)
	}
	if model == nil {
		return nil, fmt.Errorf("%w: %q", ErrModelUnresolved, a.Model)
	}
	return model, nil
}

// resolveSkills settles the repository holding the declared skills.
func (b Builder) resolveSkills(ctx context.Context, names []string) (skill.Repository, error) {
	if b.Skills == nil {
		return nil, fmt.Errorf("%w: skills %v", ErrResolverMissing, names)
	}
	repository, err := b.Skills(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("subagent: skills %v: %w", names, err)
	}
	return repository, nil
}

// resolveTools settles the tools the declared references name.
func (b Builder) resolveTools(ctx context.Context, names []string) ([]trpctool.Tool, error) {
	if b.Tools == nil {
		return nil, fmt.Errorf("%w: tools %v", ErrResolverMissing, names)
	}
	tools, err := b.Tools(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("subagent: tools %v: %w", names, err)
	}
	return tools, nil
}

// resolveToolSets settles the tool sets the declared MCP servers name.
func (b Builder) resolveToolSets(ctx context.Context, names []string) ([]trpctool.ToolSet, error) {
	if b.MCP == nil {
		return nil, fmt.Errorf("%w: mcp %v", ErrResolverMissing, names)
	}
	sets, err := b.MCP(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("subagent: mcp %v: %w", names, err)
	}
	return sets, nil
}
