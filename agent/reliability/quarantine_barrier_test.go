// 本文件钉住 隔离搬移的屏障与容量扣减绑定：rename 失败不穿透 pending、错误上抛；
// ClaimNext 在锁内复查关闭状态。
package reliability

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestQuarantine_RenameFailureKeepsCapacity 钉住 认领扫描遇到坏信封、而隔离 rename
// 又被阻塞时：错误必须上抛（不静默跳过），pending 不得扣减（容量记账不穿透）。
func TestQuarantine_RenameFailureKeepsCapacity(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	defer in.Close()

	mustEnqueue(t, in, env("q1", "user"))
	paths, _ := filepath.Glob(filepath.Join(in.dir, "*.json"))
	require.Len(t, paths, 1)
	require.NoError(t, os.WriteFile(paths[0], []byte("{ broken"), 0o644))

	// Block the move: occupy the quarantine destination with a directory.
	dst := filepath.Join(in.dir, inboxQuarantine, filepath.Base(paths[0]))
	require.NoError(t, os.MkdirAll(dst, 0o755))

	pendingBefore := in.Pending()
	_, _, err = in.ClaimNext()
	require.ErrorContains(t, err, "quarantine rename")
	require.Equal(t, pendingBefore, in.Pending(), "a failed quarantine must not free capacity")

	// Unblocking lets the next claim self-heal: the item moves, capacity frees,
	// the scan proceeds past it.
	require.NoError(t, os.RemoveAll(dst))
	_, _, err = in.ClaimNext()
	require.NoError(t, err, "after the block clears the claim path must recover")
	require.Equal(t, pendingBefore-1, in.Pending(), "the successful quarantine decrements once")
}

// TestClaimNext_RefusedAfterClose 钉住 锁内 closed 复查：与 Enqueue 对称，Close 之后
// 的认领一律拒绝，不复用无锁快查的竞态窗口发放新认领。
func TestClaimNext_RefusedAfterClose(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("c1", "user"))
	require.NoError(t, in.Close())

	_, _, err = in.ClaimNext()
	require.ErrorContains(t, err, "closed")
}
