package reliability

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// ==================== SpillStore（T-G · 事件溢出磁盘存储，at-least-once）====================
//
// 当内存事件队列满时，事件序列化后溢出到此处（不丢弃）；队列空时从此处回收。这是
// ReliableBus「常驻不丢事件」的持久层。
//
// 设计（报告 D3「at-least-once 而非 exactly-once」）：
//   - 每溢出项一文件（零填充 seq 命名，字典序 = 时序），回收即删（消费确认）；
//   - tmp + rename 原子写（防半写文件被回收）；
//   - 重启扫描现有 .spill 恢复 seq 计数（不覆盖未消费项）；
//   - 写后崩溃 → 项仍在磁盘，重启后回收 → 可能重复（at-least-once，消费方需幂等）；
//   - 字节级接口（不依赖具体事件类型）→ reliability 保持叶子包，无 agent 依赖环。

const spillExt = ".spill"

// SpillStore 是溢出项的磁盘存储（并发安全）。
type SpillStore struct {
	dir string
	mu  sync.Mutex   // 序列化 Reclaim 的读-删（防并发回收同一项）
	seq atomic.Int64 // 单调递增溢出序号
}

// NewSpillStore 构建溢出存储。dir 为空返回 error（溢出必须持久化才有意义）。
// 扫描现有 .spill 文件恢复 seq 到最大值（重启后不覆盖未消费项）。
func NewSpillStore(dir string) (*SpillStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("reliability: SpillStore requires non-empty dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("reliability: create spill dir: %w", err)
	}
	s := &SpillStore{dir: dir}
	s.recoverSeq()
	return s, nil
}

// recoverSeq 扫描现有溢出文件，把 seq 恢复到最大值（重启不覆盖未消费项）。
func (s *SpillStore) recoverSeq() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	var max int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), spillExt) {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSuffix(e.Name(), spillExt), 10, 64)
		if err == nil && n > max {
			max = n
		}
	}
	s.seq.Store(max)
}

// Spill 溢出一项（序列化字节）到磁盘。原子写（tmp + rename）。磁盘满等错误返回给调用方
// （由调用方决定降级——如回退到丢弃 + 告警）。
func (s *SpillStore) Spill(data []byte) error {
	if s == nil {
		return fmt.Errorf("reliability: nil SpillStore")
	}
	n := s.seq.Add(1)
	name := filepath.Join(s.dir, fmt.Sprintf("%020d%s", n, spillExt))
	tmp := name + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("reliability: spill write: %w", err)
	}
	if err := os.Rename(tmp, name); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("reliability: spill rename: %w", err)
	}
	return nil
}

// Reclaim 取回最早溢出的一项（字典序最小 = 时序最早），消费即删。
// 无溢出项返回 (nil, false, nil)。mu 序列化读-删防并发回收同一项。
func (s *SpillStore) Reclaim() ([]byte, bool, error) {
	if s == nil {
		return nil, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, false, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), spillExt) {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, false, nil
	}
	sort.Strings(names) // 零填充 seq → 字典序 = 时序
	path := filepath.Join(s.dir, names[0])
	data, err := os.ReadFile(path)
	if err != nil {
		// 读失败：删除坏项（防卡死队头），报告错误让调用方跳过。
		_ = os.Remove(path)
		return nil, false, fmt.Errorf("reliability: reclaim read %s: %w", names[0], err)
	}
	if err := os.Remove(path); err != nil {
		return data, true, fmt.Errorf("reliability: reclaim remove %s: %w", names[0], err)
	}
	return data, true, nil
}

// Len 返回当前溢出项数（诊断/背压信号）。
func (s *SpillStore) Len() int {
	if s == nil {
		return 0
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), spillExt) {
			n++
		}
	}
	return n
}

// Dir 返回溢出目录（诊断）。
func (s *SpillStore) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}
