// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
//
// 隔离搬移的屏障与容量扣减绑定：rename 失败不穿透 pending、错误上抛；ClaimNext 在锁内复查关闭状态。
package reliability

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestQuarantine_RenameFailureKeepsCapacity 钉住隔离搬移被阻时错误上抛且容量不扣。
// - 坏信封隔离 rename 失败不静默跳过：pending 不扣减，容量记账不穿透。
// - 障碍解除后下一次认领自愈：项目搬移、容量释放、扫描前进。
func TestQuarantine_RenameFailureKeepsCapacity(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	defer in.Close()

	mustEnqueue(t, in, env("q1", "user"))
	paths, _ := filepath.Glob(filepath.Join(in.dir, "*.json"))
	require.Len(t, paths, 1)
	require.NoError(t, os.WriteFile(paths[0], []byte("{ broken"), 0o644))

	dst := filepath.Join(in.dir, inboxQuarantine, filepath.Base(paths[0]))
	require.NoError(t, os.MkdirAll(dst, 0o755), "a directory at the quarantine destination blocks the rename")

	pendingBefore := in.Pending()
	_, _, err = in.ClaimNext()
	require.ErrorContains(t, err, "quarantine rename")
	require.Equal(t, pendingBefore, in.Pending(), "a failed quarantine must not free capacity")

	require.NoError(t, os.RemoveAll(dst))
	_, _, err = in.ClaimNext()
	require.NoError(t, err, "after the block clears the claim path must recover")
	require.Equal(t, pendingBefore-1, in.Pending(), "the successful quarantine decrements once")
}

// TestClaimNext_RefusedAfterClose 钉住锁内 closed 复查与 Enqueue 对称。
// - Close 之后的认领一律拒绝，不复用无锁快查的竞态窗口发放新认领。
func TestClaimNext_RefusedAfterClose(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("c1", "user"))
	require.NoError(t, in.Close())

	_, _, err = in.ClaimNext()
	require.ErrorContains(t, err, "closed")
}
