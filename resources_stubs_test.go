// 本文件是资源所有权的装配级用例在根侧共用的关闭顺序桩。
// 契约: docs/wiki/platform/resource-ownership.md#last-lease-close
package tagent

import (
	"github.com/SpellingDragon/tagent/memory"
)

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

type seqEngine struct {
	memory.MemoryEngine
	seq *[]string
	err error
}

func (e *seqEngine) Close() error {
	*e.seq = append(*e.seq, "engine")
	return e.err
}

type blockCloseStore struct {
	memory.MemoryStore
	entered chan struct{}
	unblock chan struct{}
}

func (s *blockCloseStore) Close() error {
	s.entered <- struct{}{}
	<-s.unblock
	return nil
}
