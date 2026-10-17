// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
package agent

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/stretchr/testify/require"
	upagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

type commandRelaunchProbe struct {
	generation string
	calls      int
	decl       task.Declarative
}

func (p *commandRelaunchProbe) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "action"}
}

func (p *commandRelaunchProbe) RelaunchTask(_ context.Context, decl task.Declarative) (task.SpawnResult, error) {
	p.calls++
	p.decl = decl
	return task.SpawnResult{Blocked: p.generation}, nil
}

type commandRelaunchRunner struct {
	countingRunner
	relaunch func(context.Context) (task.SpawnResult, error)
	result   task.SpawnResult
	err      error
}

func (r *commandRelaunchRunner) Run(ctx context.Context, _, _ string, _ model.Message, _ ...upagent.RunOption) (<-chan *event.Event, error) {
	r.result, r.err = r.relaunch(ctx)
	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}

// TestCommandRelaunchUsesPinnedAndNewExecutionFaces preserves in-flight authority across publication.
func TestCommandRelaunchUsesPinnedAndNewExecutionFaces(t *testing.T) {
	oldAction := &action.ActionTool{}
	decl := action.DeclarativeFromArgs(action.ActionArgs{Command: "relaunch-command", Mode: "resident", Probe: "true"}, "old-session")
	oldSpec := oldAction.SpecFromDeclarative(nil, *decl)
	g1 := &commandRelaunchProbe{generation: "G1"}
	r1 := &commandRelaunchRunner{relaunch: oldSpec.Relaunch}
	cm := newRecycleManager()
	cm.runner = r1
	cm.execCfg.Tools = []trpctool.Tool{NewOutputLimitTool(g1, 1000)}
	lease := cm.AcquireLease(LeaseTurn)
	t.Cleanup(lease.Release)

	g2 := &commandRelaunchProbe{generation: "G2"}
	r2 := &commandRelaunchRunner{relaunch: oldSpec.Relaunch}
	face := cm.ExecutorConfig()
	face.Tools = []trpctool.Tool{NewOutputLimitTool(g2, 1000)}
	cm.PublishExecutor(r2, face)
	require.Zero(t, r1.closed.Load())

	require.NoError(t, cm.RunFlowWithExecutor(lease.WithContext(t.Context()), model.NewUserMessage("in-flight relaunch"), lease.Runner()))
	require.NoError(t, r1.err)
	require.Equal(t, "G1", r1.result.Blocked)
	require.Equal(t, 1, g1.calls)
	require.Zero(t, g2.calls)
	lease.Release()
	require.EqualValues(t, 1, r1.closed.Load())
	require.NoError(t, oldAction.Close())

	require.NoError(t, cm.RunFlow(t.Context(), model.NewUserMessage("new relaunch")))
	require.NoError(t, r2.err)
	require.Equal(t, "G2", r2.result.Blocked)
	require.Equal(t, 1, g1.calls)
	require.Equal(t, 1, g2.calls)
	require.Equal(t, "relaunch-command", g2.decl.Command)
	require.Empty(t, g2.decl.TaskID)
	cm.Close()
}

// TestCommandRelaunchRefusesRemovedTool reports the selected generation's missing command authority.
func TestCommandRelaunchRefusesRemovedTool(t *testing.T) {
	oldAction := &action.ActionTool{}
	decl := action.DeclarativeFromArgs(action.ActionArgs{Command: "removed-command"}, "old-session")
	oldSpec := oldAction.SpecFromDeclarative(nil, *decl)
	cm := newRecycleManager()
	cm.execCfg.Tools = []trpctool.Tool{oldAction}
	lease := cm.AcquireLease(LeaseTurn)
	lease.Release()
	run := &commandRelaunchRunner{relaunch: oldSpec.Relaunch}
	face := cm.ExecutorConfig()
	face.Tools = nil
	cm.PublishExecutor(run, face)
	require.NoError(t, oldAction.Close())

	require.NoError(t, cm.RunFlow(t.Context(), model.NewUserMessage("relaunch removed action")))
	require.ErrorContains(t, run.err, "action tool is not declared")
	require.ErrorContains(t, run.err, "execution generation 2")
	cm.Close()
}
