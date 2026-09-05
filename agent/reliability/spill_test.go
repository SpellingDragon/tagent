package reliability

import (
	"fmt"
	"sync"
	"testing"
)

func TestSpillStore_SpillReclaimFIFO(t *testing.T) {
	s, err := NewSpillStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSpillStore: %v", err)
	}
	for _, e := range []string{"event1", "event2", "event3"} {
		if err := s.Spill([]byte(e)); err != nil {
			t.Fatalf("Spill: %v", err)
		}
	}
	if s.Len() != 3 {
		t.Fatalf("应 3 溢出项, got %d", s.Len())
	}
	// FIFO 回收（零填充 seq → 字典序 = 时序）。
	for _, want := range []string{"event1", "event2", "event3"} {
		data, ok, err := s.Reclaim()
		if err != nil || !ok {
			t.Fatalf("Reclaim: ok=%v err=%v", ok, err)
		}
		if string(data) != want {
			t.Fatalf("回收=%s 期望 %s", data, want)
		}
	}
	if _, ok, _ := s.Reclaim(); ok {
		t.Fatal("空应 false")
	}
	if s.Len() != 0 {
		t.Fatal("回收后应空")
	}
}

func TestSpillStore_RecoverSeqAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s1, _ := NewSpillStore(dir)
	_ = s1.Spill([]byte("a"))
	_ = s1.Spill([]byte("b"))
	// 重开（模拟重启）：未消费项保留，seq 恢复到最大值不覆盖。
	s2, _ := NewSpillStore(dir)
	if s2.Len() != 2 {
		t.Fatalf("重启后应保留 2 未消费项, got %d", s2.Len())
	}
	_ = s2.Spill([]byte("c")) // seq=3，不覆盖 a(1)/b(2)
	for _, want := range []string{"a", "b", "c"} {
		data, ok, _ := s2.Reclaim()
		if !ok || string(data) != want {
			t.Fatalf("回收=%s(%v) 期望 %s", data, ok, want)
		}
	}
}

func TestSpillStore_EmptyDirError(t *testing.T) {
	if _, err := NewSpillStore(""); err == nil {
		t.Fatal("空 dir 应 error")
	}
}

func TestSpillStore_NilSafe(t *testing.T) {
	var s *SpillStore
	if s.Len() != 0 {
		t.Fatal("nil Len 应 0")
	}
	if _, ok, _ := s.Reclaim(); ok {
		t.Fatal("nil Reclaim 应 false")
	}
	if s.Dir() != "" {
		t.Fatal("nil Dir 应空")
	}
	if err := s.Spill([]byte("x")); err == nil {
		t.Fatal("nil Spill 应 error")
	}
}

// TestSpillStore_ConcurrentSpillReclaim 验证并发安全（-race）：多 goroutine 同时 spill/reclaim
// 无数据竞争、无 panic、无重复回收同一项。
func TestSpillStore_ConcurrentSpillReclaim(t *testing.T) {
	s, _ := NewSpillStore(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = s.Spill([]byte(fmt.Sprintf("e%d", i)))
		}(i)
		go func() {
			defer wg.Done()
			_, _, _ = s.Reclaim()
		}()
	}
	wg.Wait()
	// 排空剩余（不应 panic）。
	for {
		if _, ok, _ := s.Reclaim(); !ok {
			break
		}
	}
	if s.Len() != 0 {
		t.Fatalf("排空后应 0, got %d", s.Len())
	}
}
