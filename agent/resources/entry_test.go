// 本文件是资源注册表的深白盒用例：直接构造内部键与条目，验证封闭/毒化路径的机械行为。
// 契约: docs/wiki/platform/resource-ownership.md#last-lease-close
package resources

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// TestBuildFailure_CleanReclaimStaysRetryable 钉住 构建失败而回收已确认时，写权必须交还，同路径下一次获取能干净重开。
// 契约: docs/wiki/platform/resource-ownership.md#poisoned-seal
func TestBuildFailure_CleanReclaimStaysRetryable(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	fp := FingerprintMemory(config.MemoryConfig{Type: "localfile", Path: dir})
	buildErr := errors.New("kv died at startup")

	_, _, _, err := rr.Acquire("localfile", dir, fp, func() (OpenedResource, error) {
		return OpenedResource{}, buildErr
	})
	require.ErrorIs(t, err, buildErr)

	_, _, rel, err2 := rr.Acquire("localfile", dir, fp, func() (OpenedResource, error) {
		return OpenedResource{Store: &seqStore{MemoryStore: nil, seq: new([]string)}}, nil
	})
	require.NoError(t, err2, "a cleanly reclaimed failed build must not seal the path")
	require.NoError(t, rel())
}

// TestBuildFailure_UnconfirmedReclaimSealsWriter 钉住 回收无法确认的构建失败必须保持写权并封住路径，绝不与半活后端并写。
// - 原始失败原因仍要回到调用方，封路另用具名错误表达；
// - 封住是显式条目在册，不止账面记录：探测同一路径撞上「已被占用」。
// 契约: docs/wiki/platform/resource-ownership.md#poisoned-seal
func TestBuildFailure_UnconfirmedReclaimSealsWriter(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	fp := FingerprintMemory(config.MemoryConfig{Type: "localfile", Path: dir})
	buildErr := errors.New("segment store init failed")
	closeErr := errors.New("kv close hung")

	_, _, _, err := rr.Acquire("localfile", dir, fp, func() (OpenedResource, error) {
		return OpenedResource{}, fmt.Errorf("%w; %w", buildErr,
			fmt.Errorf("%w: kv close: %v", ErrReclaimUnconfirmed, closeErr))
	})
	require.ErrorIs(t, err, ErrReclaimUnconfirmed, "the original build error still reaches the caller")

	key := resourceKey{kind: "localfile", path: Canonicalize(dir)}
	rr.mu.Lock()
	e := rr.entries[key]
	rr.mu.Unlock()
	require.NotNil(t, e, "unconfirmed reclaim must seal via an explicit poisoned entry")
	require.True(t, e.poisoned)

	_, _, _, err2 := rr.Acquire("localfile", dir, fp, func() (OpenedResource, error) {
		t.Fatal("open must NOT run again on a sealed path")
		return OpenedResource{}, nil
	})
	require.ErrorIs(t, err2, ErrResourcePoisoned)

	probe, perr := os.OpenFile(filepath.Join(Canonicalize(dir), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, perr)
	defer probe.Close()
	require.Error(t, FlockExclusive(probe), "an unconfirmed reclaim must keep holding the writer lock")
}

// TestWriterLock_ExclusiveAcrossHandles 钉住 一个物理目录同时只允许一个写者，第二个持有者非阻塞抢锁必须失败。
// - 交还后可重新取得；
// - 进程崩溃由 OS 交还锁，不存在遗留标记把目录永久锁死。
// 契约: docs/wiki/platform/resource-ownership.md#single-writer
func TestWriterLock_ExclusiveAcrossHandles(t *testing.T) {
	dir := t.TempDir()

	f1, err := AcquireDirLock(dir)
	require.NoError(t, err)

	_, err = AcquireDirLock(dir)
	require.True(t, errors.Is(err, ErrStoreLocked), "second writer must be rejected, got: %v", err)

	require.NoError(t, UnlockDirLock(f1))

	f2, err := AcquireDirLock(dir)
	require.NoError(t, err, "after release the lock is re-acquirable")
	require.NoError(t, UnlockDirLock(f2))
}

// TestPoisoned_ExplicitEntrySealsPathAcrossGC 钉住 未确认停止的路径由显式条目封住，强引用与失败原因都在册，主动 GC 削弱不了它。
// - 同路径获取一律具名失败，并带上记录的那次失败；
// - 封路优先于冲突记账：换另一份指纹报的仍是「被封住」；
// - 无关路径不受影响，封的是一条路径而非整张登记簿。
// 契约: docs/wiki/platform/resource-ownership.md#poisoned-seal
func TestPoisoned_ExplicitEntrySealsPathAcrossGC(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	var seq []string
	engErr := errors.New("engine worker stuck")
	fp := FingerprintMemory(config.MemoryConfig{Type: "localfile", Path: dir})

	var sealed *seqStore
	openFn := func() (OpenedResource, error) {
		s := &seqStore{MemoryStore: memory.NewInMemoryStore(), seq: &seq}
		sealed = s
		return OpenedResource{Store: s, Engine: &seqEngine{seq: &seq, err: engErr}}, nil
	}
	_, _, rel, err := rr.Acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)

	require.ErrorIs(t, rel(), engErr, "the close failure must reach the releasing caller")

	key := resourceKey{kind: "localfile", path: Canonicalize(dir)}
	rr.mu.Lock()
	e := rr.entries[key]
	rr.mu.Unlock()
	require.NotNil(t, e, "§6.4: the poisoned entry must be RETAINED, not detached + silently leaked")
	require.True(t, e.poisoned)
	require.Same(t, memory.MemoryStore(sealed), e.store, "entry keeps the strong store reference")
	require.NotNil(t, e.lockFile, "entry keeps the lockfile reference")
	require.ErrorIs(t, e.closeErr, engErr)

	runtime.GC()
	runtime.GC()

	_, _, _, err2 := rr.Acquire("localfile", dir, fp, openFn)
	require.ErrorIs(t, err2, ErrResourcePoisoned, "same-path acquire must fail EXPLICITLY (poisoned), not via a flock race or a resurrected generation")
	require.ErrorContains(t, err2, engErr.Error(), "the sealing error carries the recorded failure to the caller")

	_, _, _, err3 := rr.Acquire("localfile", dir, "v1|other|fp", openFn)
	require.ErrorIs(t, err3, ErrResourcePoisoned)

	other := t.TempDir()
	fpOther := FingerprintMemory(config.MemoryConfig{Type: "localfile", Path: other})
	_, _, relOther, err4 := rr.Acquire("localfile", other, fpOther, func() (OpenedResource, error) {
		s := &seqStore{MemoryStore: memory.NewInMemoryStore(), seq: &seq}
		return OpenedResource{Store: s}, nil
	})
	require.NoError(t, err4, "poisoning one path must not seal the registry")
	require.NoError(t, relOther())

	probe, perr := os.OpenFile(filepath.Join(Canonicalize(dir), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, perr)
	defer probe.Close()
	require.Error(t, FlockExclusive(probe), "the poisoned entry must still hold the writer lock after GC")
}

// seqStore wraps a memory.MemoryStore, records the teardown sequence, lets the
// test force Close to fail, and exposes StopProducers so resources.CloseResource stops
// the forgetting producers BEFORE the engine worker.
type seqStore struct {
	memory.MemoryStore
	seq *[]string
	err error
}

func (s *seqStore) StopProducers() { *s.seq = append(*s.seq, "producers") }
func (s *seqStore) Close() error {
	*s.seq = append(*s.seq, "store")
	return s.err
}

// seqEngine wraps a memory.MemoryEngine; Close records the step and can fail.
// Only Close is exercised (resources.CloseResource never calls the promoted methods).
type seqEngine struct {
	memory.MemoryEngine
	seq *[]string
	err error
}

func (e *seqEngine) Close() error {
	*e.seq = append(*e.seq, "engine")
	return e.err
}

// blockCloseStore wraps a memory.MemoryStore whose Close blocks until unblocked,
// simulating a slow backend flush so the test can observe whether a same-path
// reopen wrongly races ahead of (or deadlocks against) the in-progress close.
type blockCloseStore struct {
	memory.MemoryStore
	// entered receives exactly once when a Close begins.
	entered chan struct{}
	// unblock releases Close: the call returns only after it is signalled.
	unblock chan struct{}
}

func (s *blockCloseStore) Close() error {
	s.entered <- struct{}{}
	<-s.unblock
	return nil
}
