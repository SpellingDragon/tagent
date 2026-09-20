package memory

import (
	"trpc.group/trpc-go/trpc-agent-go/log"

	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// ==================== MemSpill（T-G 步4 · memory 退化事件兜底，报告 D3 line 2229/2392）====================
//
// memory 依赖退化（StoreEvent 失败）时，事件（key 已分配 + FullEvent）落 JSONL 兜底，恢复后
// 重放（按原 key StoreEvent）。使 memory 退化期间事件不丢——at-least-once 延伸到存储层：
// DegradationManager 检测退化状态（可观测），MemSpill 提供退化的**实质兜底行为**（不丢事件）。
//
// 重放幂等（W1 修订，§8.3）：重放前逐条 GetEvent 预检——事件已存在（假阴性失败：KV 已写但
// CLI 响应解析失败误报 error）即计成功移除，避免撞 FileSegmentStore "already exists 拒绝重写"
// 守卫致 spill 永久滞留（原设计误设"按原 key 幂等覆盖"，实际 FileSegmentStore 拒绝重写，见
// segment_store.go:263）。projection 侧由 memory_plugin stored 分支 + seen map 去重。重放用
// inner store（绕过 ErrorTrackingStore，防重放失败再次触发上报/落盘递归）。

// spilledEvent 是兜底 JSONL 的一行（key + 完整事件）。
type spilledEvent struct {
	Key   int64     `json:"key"`
	Event FullEvent `json:"event"`
}

// MemSpill 是 memory 退化事件兜底 JSONL 存储（并发安全）。path 空则禁用（nil 语义）。
type MemSpill struct {
	path string
	mu   sync.Mutex

	// §2.8：可选保留租约守卫（由 ErrorTrackingStore 从 inner store 注入）。非 nil 时，
	// 每条 spill 待重放 key 在其 durable 原文上注册一个持有者（append 即 protect、重放成功
	// 即 release、启动 ProtectAllPending 从现有文件重建），使其在重放前不受 TTL/容量/压实销毁。
	guard RetentionGuard
}

// SetGuard 注入 §2.8 保留租约守卫（nil = 不保护）。由持有本 spill 的装饰器从其后端取得。
func (s *MemSpill) SetGuard(g RetentionGuard) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guard = g
}

// ProtectAllPending 从现有 spill 文件重建保留租约（§2.8 重启恢复）：对每条待重放 key
// 注册一个持有者。由恢复 owner 在装载 spill 后、放行扫描器前调用。
func (s *MemSpill) ProtectAllPending() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.guard == nil {
		return nil
	}
	// §5.8: the spill inventory lands under the registration barrier — on an
	// already-running store (late attach) no forgetting pass may squeeze between
	// "rebuild started" and "pending keys protected". A read failure KEEPS the
	// hold (explicit block: unreadable spill material must not be destroyed on an
	// incomplete view) and surfaces to the caller.
	s.guard.BeginHold()
	pending, err := s.readAll()
	if err != nil {
		return err
	}
	for _, sp := range pending {
		if sp.Key != 0 {
			s.guard.ProtectKey(sp.Key)
		}
	}
	s.guard.EndHold()
	return nil
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
	if err == nil && s.guard != nil {
		s.guard.ProtectKey(key) // §2.8: a spilled pending key's original is retained until replayed
	}
	return err
}

// Replay 重放兜底事件到 store（按原 key StoreEvent）。成功的移除、仍失败的保留在文件。
// 返回重放成功数。store 应为 inner（绕过 ErrorTrackingStore 防递归）。坏行跳过。
func (s *MemSpill) Replay(store MemoryStore) (int, error) {
	return s.ReplayWithNotify(store, nil)
}

// ReplayWithNotify 是 Replay 的双写形态（design-report-closeout 5.5）：每条重放成功
// （含幂等命中）的事件回调 notify——调用方据此补投影（projection.Append），恢复
// 「存储⇔投影同点原子」的等价语义（写入统一 D1 在退化路径上的延伸）。notify 为 nil
// 或内部失败不影响重放结果（投影可后补，事件不丢优先）。
//
// §2.6 canonical replay only: spill replay MUST use the store's EventReplayer contract
// (ReplayEvent) — it distinguishes new-commit / orphan-repair / already-committed
// atomically against the durable fact chain. A store that does NOT implement
// EventReplayer is refused and its spill originals are retained (spec L99: 内层没有显式
// 恢复能力 → 能力检查失败、原件保留). The former GetEvent+StoreEvent weak fallback was
// removed: a GetEvent hit only proves a read returns the record, not that the durable
// commit (barrier + index/meta publication) completed, and public StoreEvent now REFUSES
// an existing key (§2.1) so it can never complete an orphan anyway.
func (s *MemSpill) ReplayWithNotify(store MemoryStore, notify func(FullEvent)) (int, error) {
	if s == nil || store == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, err := s.readAll()
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	replayer, ok := store.(EventReplayer)
	if !ok {
		// §2.6: fail the capability check loudly and keep every original rather than
		// degrade to a GetEvent/StoreEvent weak fallback. Nothing is consumed.
		return 0, fmt.Errorf("mem_spill: store %T does not implement EventReplayer; replay refused and spill originals retained (§2.6: no GetEvent weak fallback)", store)
	}
	var failed []spilledEvent
	replayed := 0
	notifySafe := func(ev FullEvent) {
		if notify == nil {
			return
		}
		// 投影失败仅记录（与主链路同语义：投影是派生视图，可后补）。
		defer func() {
			if r := recover(); r != nil {
				log.Warnf("[mem_spill] replay notify panic (projection skipped): %v", r)
			}
		}()
		notify(ev)
	}
	for _, sp := range pending {
		// §2.6 canonical path: ReplayEvent is idempotent and handles all orphan states.
		result, _, rerr := replayer.ReplayEvent(sp.Key, sp.Event)
		if rerr != nil {
			failed = append(failed, sp) // store faulty / forgotten / conflict: keep for next replay
			continue
		}
		// ReplayNew, ReplayRepaired, and ReplayAlreadyCommitted all count as success:
		// the fact is now durable in the store (at-least-once guarantee fulfilled).
		_ = result
		replayed++
		if s.guard != nil {
			s.guard.ReleaseKey(sp.Key) // §2.8: replayed and (via rewrite) removed → drop the spill holder
		}
		notifySafe(sp.Event)
	}
	// 重写文件（仅保留仍失败的）；全部成功则文件清空。
	if rerr := s.rewrite(failed); rerr != nil {
		return replayed, rerr
	}
	return replayed, nil
}

// PendingKeys 返回仍在等待 spill 重放的事件 key（§2.8）。恢复 owner 启动时据此从现有
// 未确认 spill 材料重建保留租约，令其 durable 原文在被重放移除前不受 TTL/容量/压实销毁。
// nil-safe；读文件失败返回错误（调用方保守处理，不得据此开放淘汰）。
func (s *MemSpill) PendingKeys() ([]int64, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, err := s.readAll()
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(pending))
	for _, sp := range pending {
		if sp.Key != 0 {
			out = append(out, sp.Key)
		}
	}
	return out, nil
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
