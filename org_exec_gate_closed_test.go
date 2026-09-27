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

// runBounded runs fn and fails the test if it does not return within d, so a gate
// that hangs instead of refusing is reported as a hang — never as an indefinite suite.
func runBounded(t *testing.T, what string, d time.Duration, fn func() error) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- fn() }()
	select {
	case err := <-errCh:
		return err
	case <-time.After(d):
		t.Fatalf("%s 在已收敛关闭后未返回——闸门必须拒绝，不能挂住", what)
		return nil
	}
}

// TestExecGate_WorkAfterConvergedCloseIsRefused is §3.2's acceptance item
// 「关闭后的 Run/Inject/Acquire 真正再进一次且被拒」.
//
// The owner here is not merely "close started" (that state is D7's refusal at org
// admission, already anchored): it has FULLY CONVERGED — drained, its runner closed,
// its store-owner registration revoked, gone from the resident table. A stale holder
// that still points at the instance is exactly what a leaked wrapper in an old
// generation's tool table is, and whatever it starts now must be REFUSED at the shared
// execution gate. Handing it a closed runner is the defect
// `tryAcquireActive`'s own comment names as unacceptable (「running a turn on a closed
// executor」), and registering a new reference after the drain already reported clean
// re-opens an obligation nobody is waiting on anymore.
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

	// ① Acquire: the gate must refuse, i.e. neither hand out a runnable executor nor
	// register a fresh reference on a converged generation.
	lease := cm.BeginTurnLease()
	require.NotNil(t, lease)
	if lease != nil {
		t.Cleanup(lease.Release)
		assert.Nil(t, lease.Runner(),
			"§3.2：已收敛关闭后 Acquire 必须被拒——把已关闭的执行器交给新 turn 正是 tryAcquireActive 注释里点名不可接受的失效")
	}
	assert.True(t, s3.Obligations().Idle(),
		"and a refused entry must not leave a new reference registered after the drain reported clean")

	// ② Inject: same refusal, surfaced to the caller.
	_, _, injErr := s3.InjectEnvelope(context.Background(), "stale-holder",
		[]model.Message{{Role: model.RoleUser, Content: "into a converged owner"}})
	// Measured, not assumed: input acceptance is refused by the LOOP gate (the pipeline is
	// gone), while starting work is refused by the GENERATION gate. Both are named, and
	// asserting the exact sentinel is what keeps a later refactor from quietly changing
	// what "refused" means here.
	assert.ErrorIs(t, injErr, agent.ErrLoopTerminated,
		"§3.2：关闭后的 Inject 必须被具名拒绝（输入接受面＝环路已终止）")

	// ③ Run: bounded, and an error rather than a turn executed on a closed executor.
	runErr := runBounded(t, "RunFlow", 10*time.Second, func() error {
		return cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "stale holder"})
	})
	assert.ErrorIs(t, runErr, agent.ErrExecClosed,
		"§3.2：关闭后的 Run 必须真正再进一次世代闸门并被具名拒绝，不得静默在已关闭执行器上跑完")
}
