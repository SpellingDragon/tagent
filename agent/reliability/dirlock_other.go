//go:build !unix

// 本文件是目录单属主锁在非 unix 平台的降级面：恒成功、不串行，锁只是防事故栏杆而非可移植承诺。
// 契约: docs/wiki/reliability/durable-delivery.md#inbox-ownership
package reliability

import (
	"os"
	"path/filepath"
)

// dirLock is a no-op on platforms without flock semantics.
type dirLock struct{}

func dirLockPath(dir string) string { return filepath.Join(dir, ".owner") }

// acquireDirLock always succeeds here: single ownership is not enforced on this
// platform, so the writer-identity rules in inbox.go carry the whole safety load.
func acquireDirLock(dir string) (*dirLock, string, error) {
	f, err := os.OpenFile(dirLockPath(dir), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, "", err
	}
	_ = f.Close()
	return &dirLock{}, "", nil
}

func (l *dirLock) release() error { return nil }

func readDirLockHolder(*os.File) string { return "" }
