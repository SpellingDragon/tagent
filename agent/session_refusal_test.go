package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestSubagentRun_RefusalLeaksNoLiveCM 钉住 委托被拒（owner 代已收敛关闭）时，invocation 私有 CM
// 既不注册也不悬挂：早先版本在租约检查之前就 registerLiveCM，拒绝分支没有配对的清理
// goroutine，liveCMs 永久残留、owner 义务永不归零、退役排水被卡死且每次拒绝叠加一条。
func TestSubagentRun_RefusalLeaksNoLiveCM(t *testing.T) {
	ta, err := NewTagentAgent(&TagentConfig{
		Model:        newRecordableMockModel(gateOKResp()),
		SystemPrompt: "refuser",
		Name:         "refuser",
	})
	require.NoError(t, err)

	// Terminal owner generation: every new delegation must be refused.
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
