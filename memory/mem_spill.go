package memory

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
)

// ==================== MemSpill（T-G 步4 · memory 退化事件兜底，报告 D3 line 2229/2392）====================
//
// memory 依赖退化（StoreEvent 失败）时，事件（key 已分配 + FullEvent）落 JSONL 兜底，恢复后
// 重放（按原 key StoreEvent）。使 memory 退化期间事件不丢——at-least-once 延伸到存储层：
// DegradationManager 检测退化状态（可观测），MemSpill 提供退化的**实质兜底行为**（不丢事件）。
//
// 重放幂等：按原 key StoreEvent（FileSegmentStore/InMemoryStore key 幂等覆盖）；projection 侧
// 由 memory_plugin 的 stored 分支 + projection seen map 去重（报告 line 2229）。重放用 inner
// store（绕过 ErrorTrackingStore，防重放失败再次触发上报/落盘的递归）。

// spilledEvent 是兜底 JSONL 的一行（key + 完整事件）。
type spilledEvent struct {
	Key   int64     `json:"key"`
	Event FullEvent `json:"event"`
}

// MemSpill 是 memory 退化事件兜底 JSONL 存储（并发安全）。path 空则禁用（nil 语义）。
type MemSpill struct {
	path string
	mu   sync.Mutex
}

// NewMemSpill 构建兜底存储。path 为空返回 nil（禁用，ErrorTrackingStore 据此跳过落盘）。
func NewMemSpill(path string) *MemSpill {
	if path == "" {
		return nil
	}
	return &MemSpill{path: path}
}

// Append 落盘一个 StoreEvent 失败的事件（JSONL 追加）。best-effort：落盘失败返回 error
// （调用方据此告警——兜底也失败则事件真丢，但已尽最后一力）。
func (s *MemSpill) Append(key int64, event FullEvent) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := json.Marshal(spilledEvent{Key: key, Event: event})
	if err != nil {
		return err
	}
	_, err = f.Write(append(raw, '\n'))
	return err
}

// Replay 重放兜底事件到 store（按原 key StoreEvent）。成功的移除、仍失败的保留在文件。
// 返回重放成功数。store 应为 inner（绕过 ErrorTrackingStore 防递归）。坏行跳过。
func (s *MemSpill) Replay(store MemoryStore) (int, error) {
	if s == nil || store == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, err := s.readAll()
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	var failed []spilledEvent
	replayed := 0
	for _, sp := range pending {
		if serr := store.StoreEvent(sp.Key, sp.Event); serr != nil {
			failed = append(failed, sp) // 仍失败，保留待下次重放
			continue
		}
		replayed++
	}
	// 重写文件（仅保留仍失败的）；全部成功则文件清空。
	if rerr := s.rewrite(failed); rerr != nil {
		return replayed, rerr
	}
	return replayed, nil
}

// Len 返回当前兜底事件数（诊断/背压信号）。
func (s *MemSpill) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, _ := s.readAll()
	return len(pending)
}

func (s *MemSpill) readAll() ([]spilledEvent, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []spilledEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024) // 大事件（Content 不截断，不变量6）
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var sp spilledEvent
		if err := json.Unmarshal(line, &sp); err != nil {
			continue // 坏行跳过（torn tail 容忍，同 replayWAL 精神）
		}
		out = append(out, sp)
	}
	return out, sc.Err()
}

func (s *MemSpill) rewrite(remaining []spilledEvent) error {
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	for _, sp := range remaining {
		raw, merr := json.Marshal(sp)
		if merr != nil {
			f.Close()
			_ = os.Remove(tmp)
			return merr
		}
		if _, werr := f.Write(append(raw, '\n')); werr != nil {
			f.Close()
			_ = os.Remove(tmp)
			return werr
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, s.path) // 原子替换
}
