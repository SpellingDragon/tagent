package tagent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriterLock_ExclusiveAcrossHandles（4.3）：同目录的单 writer flock 互斥——
// 第二个持有者（另一进程或另一 fd）非阻塞抢锁必须失败；释放后可重取。
// 进程崩溃由 OS 自动释放 flock，不存在遗留锁永久锁死（delta spec「单 writer」）。
func TestWriterLock_ExclusiveAcrossHandles(t *testing.T) {
	dir := t.TempDir()

	f1, err := acquireDirLock(dir)
	require.NoError(t, err)

	_, err = acquireDirLock(dir) // second handle, same dir
	require.True(t, errors.Is(err, ErrStoreLocked), "second writer must be rejected, got: %v", err)

	require.NoError(t, unlockDirLock(f1))

	f2, err := acquireDirLock(dir)
	require.NoError(t, err, "after release the lock is re-acquirable")
	require.NoError(t, unlockDirLock(f2))
}
