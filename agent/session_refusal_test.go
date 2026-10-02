// 契约: docs/wiki/agent/execution-generations.md#lease-holds-reference
package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestSubagentRun_RefusalLeaksNoLiveCM 钉住委托被拒时私有 CM 既不注册也不悬挂。
// - 拒绝分支直接 Close，不经 registerLiveCM：liveCMs 保持零，owner 义务能归零，退役排水不被卡死。
func TestSubagentRun_RefusalLeaksNoLiveCM(t *testing.T) {
	ta, err := NewTagentAgent(&TagentConfig{
		Model:        newRecordableMockModel(gateOKResp()),
		SystemPrompt: "refuser",
		Name:         "refuser",
	})
	require.NoError(t, err)

	require.NoError(t, ta.contextManager.Close())

	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationID("refused-inv-1"),
		trpcagent.WithInvocationMessage(model.NewUserMessage("hello")),
	)
	_, err = ta.Run(context.Background(), inv)
	require.ErrorContains(t, err, "invocation refused")
	require.Zero(t, ta.LiveCMCount(), "a refused delegation must not leave a live CM registered")

	rep := ta.Obligations()
	require.Zero(t, rep.Invocations, "refusal must not inflate owner obligations")
}
