// 本文件负责 inbox 的写者身份与目录单属主：对端残骸不得堵死合法写入，一个目录只允许一个属主。
// 契约: docs/wiki/reliability/durable-delivery.md#inbox-ownership
package reliability

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPeerTmpResidueMustNotWedgeAWriter 钉住 tmp 名必须携带写者身份。
// - pid 粒度不够：同进程对端会算出同一个临时名并用 O_EXCL 堵死合法写入者。
// - 失败清理只删自己的临时文件，对端残骸必须原样存在。
func TestPeerTmpResidueMustNotWedgeAWriter(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	defer func() { _ = in.Close() }()

	path := filepath.Join(in.Dir(), fmt.Sprintf("%020d.json", 1))
	peer := path + fmt.Sprintf(".%d.tmp", os.Getpid())
	f, err := os.Create(peer)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, err = in.Enqueue(env("a", "user"))
	require.NoError(t, err, "对端的临时文件残骸不得堵死本写入者")

	_, statErr := os.Stat(peer)
	assert.True(t, statErr == nil, "失败清理不得删除不属于自己的临时文件")
}

// TestNewInboxRefusesSecondOwner 钉住一个 spillDir 只有一个属主。
// - 打开即写且 seq 自行推导，两个实例会分到同一最终路径并静默覆盖，故第二属主必须被具名拒绝。
// - 拒绝不得影响原属主的记账与写入。
func TestNewInboxRefusesSecondOwner(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	defer func() { _ = in.Close() }()

	_, err = NewInbox(dir, 10)
	require.ErrorIs(t, err, ErrInboxOwned, "同一目录的第二个属主必须被具名拒绝，而不是静默共享目录")

	mustEnqueue(t, in, env("still-works", "user"))
	assert.EqualValues(t, 1, in.Pending(), "拒绝第二个打开者不得影响原属主")
}
