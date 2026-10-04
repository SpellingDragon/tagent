//go:build unix

// 本文件用非阻塞文件锁声明 inbox 目录的单属主：属主死亡由内核释放，接管不需人工清残骸。
// 契约: docs/wiki/reliability/durable-delivery.md#inbox-ownership
package reliability

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// dirLock holds an exclusive, non-blocking lock on the inbox directory. The
// descriptor stays open for the lifetime of the owner; the kernel releases the
// lock when the process dies, so a crashed owner never wedges a restart.
type dirLock struct {
	f *os.File
}

// dirLockPath is the lock file inside the inbox directory. It is not an
// envelope, so the open scan ignores it.
func dirLockPath(dir string) string { return filepath.Join(dir, ".owner") }

// acquireDirLock takes the directory lock or returns the current holder's
// description. It never waits: a second live owner is an error, not a queue.
func acquireDirLock(dir string) (*dirLock, string, error) {
	f, err := os.OpenFile(dirLockPath(dir), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, "", fmt.Errorf("open inbox owner file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		holder := readDirLockHolder(f)
		f.Close()
		return nil, holder, errLocked
	}
	if err := f.Truncate(0); err == nil {
		_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	}
	return &dirLock{f: f}, "", nil
}

// release gives up the directory lock.
func (l *dirLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	defer func() { _ = l.f.Close(); l.f = nil }()
	return syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
}

func readDirLockHolder(f *os.File) string {
	if _, err := f.Seek(0, 0); err != nil {
		return ""
	}
	buf := make([]byte, 32)
	n, _ := f.Read(buf)
	return strings.TrimSpace(string(buf[:n]))
}
