// org_hotreload_closed_owner 已收敛属主域：收敛关闭后再进必须具名拒绝。
// 契约: docs/wiki/platform/org-hot-reload.md#closed-owner-refusal
package tagent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestExecGate_WorkAfterConvergedCloseIsRefused 钉住 已收敛退场的属主面对再进必须具名拒绝且有界返回。
// - 状态是排空完成、执行器已关、属主登记已撤销、已离开常驻表，不是"关闭已发起"；
// - 获取执行器被拒意味着交出的运行器为空，且不得在账面已清之后再登记新引用；
// - 输入接受与启动工作分别由环路闸门与世代闸门拒绝，两个具名哨兵都要断言，否则"被拒"的含义可被静默改掉；
// - 再进必须返回错误而非在已关闭执行器上跑完一回合，也必须自己返回而不是挂住。
// 契约: docs/wiki/platform/org-hot-reload.md#closed-owner-refusal
func TestExecGate_WorkAfterConvergedCloseIsRefused(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s3"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s3 := residentCacheForTest(entry)["s3"]
	require.NotNil(t, s3, "precondition: s3 resident at startup")

	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	require.NotContains(t, residentCacheForTest(entry), "s3", "precondition: s3 converged and left the table")
	require.True(t, s3.CloseStarted(), "precondition: its close completed, not just started")
	require.True(t, s3.Obligations().Idle(), "precondition: it holds no obligations")

	cm := s3.ContextManager()

	lease := cm.BeginTurnLease()
	require.NotNil(t, lease)
	if lease != nil {
		t.Cleanup(lease.Release)
		assert.Nil(t, lease.Runner(),
			"§3.2：已收敛关闭后 Acquire 必须被拒——把已关闭的执行器交给新 turn 正是 tryAcquireActive 注释里点名不可接受的失效")
	}
	assert.True(t, s3.Obligations().Idle(),
		"and a refused entry must not leave a new reference registered after the drain reported clean")

	_, _, injErr := s3.InjectEnvelope(context.Background(), "stale-holder",
		[]model.Message{{Role: model.RoleUser, Content: "into a converged owner"}})
	assert.ErrorIs(t, injErr, agent.ErrLoopTerminated,
		"§3.2：关闭后的 Inject 必须被具名拒绝（输入接受面＝环路已终止）")

	runErr := runBounded(t, "RunFlow", 10*time.Second, func() error {
		return cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "stale holder"})
	})
	assert.ErrorIs(t, runErr, agent.ErrExecClosed,
		"§3.2：关闭后的 Run 必须真正再进一次世代闸门并被具名拒绝，不得静默在已关闭执行器上跑完")
}
