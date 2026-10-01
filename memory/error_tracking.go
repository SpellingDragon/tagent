package memory

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// DegradationSink 是上报目标的窄接口：由记忆包定义、以字符串表依赖名，降级状态机经适
// 配器满足它，故记忆包不依赖可靠性包、不成环。
type DegradationSink interface {
	// ReportFailure 上报某依赖的一次失败及原始错误。
	ReportFailure(dep string, err error)
	// ReportSuccess 上报某依赖恢复健康。
	ReportSuccess(dep string)
}

const (
	// depMemory 是记忆存储依赖名（值与可靠性包的枚举一致，经上报接口桥接以免去包依赖）。
	depMemory = "memory"
	// depRustViking 与 depDisk 分别是向量后端与磁盘的依赖名。
	depRustViking = "rustviking"
	depDisk       = "disk"
)

// ErrorTrackingStore 是存储装饰链的最外层：把失败按特征归因到依赖并旁路上报，构成降级状态机
// 的唯一错误输入源；非侵入透传 inner 全部方法（含可选接口）。归因矩阵、三类"不算故障"的情况与
// 恢复证明规则见文档。
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
type ErrorTrackingStore struct {
	inner MemoryStore
	// sink 为 nil 时纯透传、不上报——配置门控关闭即行为逐字节不变。
	sink DegradationSink
	// spill 为 nil 时不做兜底落盘；非 nil 时写入失败的事件落入 JSONL 待重放（事件不丢）。
	spill *MemSpill

	mu sync.RWMutex
	// replayProjection 是重放成功后的补投影回调，使退化恢复路径仍满足"存储与投影同点"。
	replayProjection func(FullEvent)
}

var (
	_ MemoryStore    = (*ErrorTrackingStore)(nil)
	_ EventReplayer  = (*ErrorTrackingStore)(nil)
	_ RetentionGuard = (*ErrorTrackingStore)(nil)
)

// NewErrorTrackingStore 包裹 inner 做错误追踪；sink 为 nil 即纯透传、不上报。
func NewErrorTrackingStore(inner MemoryStore, sink DegradationSink) *ErrorTrackingStore {
	return &ErrorTrackingStore{inner: inner, sink: sink}
}

// SetMemSpill 启用写入失败事件的兜底落盘（path 为空即禁用），恢复后经 ReplaySpilled 重放，
// 把 at-least-once 语义延伸到存储层。启用时必须把保留租约接进兜底文件，并在放行扫描器前按现存
// 条目重建保留集；该步失败一律上抛而非吞错——吞错会造出"热更成功＋悬空遗忘屏障"，使待重放原文
// 可能被销毁，调用方须据此 fail-closed、旧实例继续服务。
func (s *ErrorTrackingStore) SetMemSpill(path string) error {
	s.spill = NewMemSpill(path)
	if s.spill != nil {
		if g, ok := s.inner.(RetentionGuard); ok {
			s.spill.SetGuard(g)
			if err := s.spill.ProtectAllPending(); err != nil {
				return fmt.Errorf("spill retention rebuild failed (pending keys may not be protected): %w", err)
			}
		}
	}
	return nil
}

// SetReplayProjection 注册重放后的补投影回调（传 nil 清除）。回调失败或 panic 不影响重放——
// 事件不丢优先，投影可后补。
func (s *ErrorTrackingStore) SetReplayProjection(fn func(FullEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replayProjection = fn
}

// ReplaySpilled 把兜底事件重放进内层存储（直连内层以防递归再次落盘），返回成功条数。
func (s *ErrorTrackingStore) ReplaySpilled() (int, error) {
	if s.spill == nil {
		return 0, nil
	}
	s.mu.Lock()
	fn := s.replayProjection
	s.mu.Unlock()
	return s.spill.ReplayWithNotify(s.inner, fn)
}

// ProtectKey 把保留登记递归透传给内层租约；内层无租约则空操作。
func (s *ErrorTrackingStore) ProtectKey(key int64) {
	if g, ok := s.inner.(RetentionGuard); ok {
		g.ProtectKey(key)
	}
}

// ReleaseKey 透传保留释放，语义同 ProtectKey。
func (s *ErrorTrackingStore) ReleaseKey(key int64) {
	if g, ok := s.inner.(RetentionGuard); ok {
		g.ReleaseKey(key)
	}
}

// ArmRetention 放行内层首次破坏性扫描的门控。
func (s *ErrorTrackingStore) ArmRetention() {
	if g, ok := s.inner.(RetentionGuard); ok {
		g.ArmRetention()
	}
}

// BeginHold 抬起登记屏障：屏障语义由底层租约持有，底层无此能力则静默跳过。
func (s *ErrorTrackingStore) BeginHold() {
	if h, ok := s.inner.(RetentionHoldable); ok {
		h.BeginHold()
	}
}

// EndHold 放下登记屏障，须与 BeginHold 成对。
func (s *ErrorTrackingStore) EndHold() {
	if h, ok := s.inner.(RetentionHoldable); ok {
		h.EndHold()
	}
}

// MemSpillLen 返回当前待重放的兜底事件数，可作诊断与背压信号。
func (s *ErrorTrackingStore) MemSpillLen() int {
	if s.spill == nil {
		return 0
	}
	return s.spill.Len()
}

// spillEvent 尽力落盘一条写入失败的事件。落盘本身失败只告警：此时事件确实会丢，但退化已被
// 记录，属可观测范围内的最后一搏；重试由重放路径统一承担。
func (s *ErrorTrackingStore) spillEvent(key int64, event FullEvent) {
	if s.spill == nil {
		return
	}
	if err := s.spill.Append(key, event); err != nil {
		log.Warnf("[ErrorTrackingStore] mem_spill append failed (event may be lost): %v", err)
	}
}

// classifyStoreErr 按错误特征把一次失败归因到依赖。判定顺序与匹配面都是契约：磁盘先判，
// 否则向量后端的错误文本内嵌其后端名会把磁盘满误归向量依赖（两者降级动作不同）；向量只认进程
// 派生失败与二进制缺失，不用宽泛名字匹配；其余归记忆存储。
func classifyStoreErr(err error) string {
	if err == nil {
		return depMemory
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no space left") || strings.Contains(msg, "enospc") ||
		strings.Contains(msg, "disk quota") {
		return depDisk
	}
	if strings.Contains(msg, "fork/exec") || strings.Contains(msg, "executable file not found") {
		return depRustViking
	}
	return depMemory
}

// report 旁路上报一次结果：err 非空记为该依赖失败，为空记为其恢复健康；sink 为 nil 时空操作。
func (s *ErrorTrackingStore) report(dep string, err error) {
	if s.sink == nil {
		return
	}
	if err != nil {
		s.sink.ReportFailure(dep, err)
	} else {
		s.sink.ReportSuccess(dep)
	}
}

// StoreEvent 透传写入并归因错误；写成功即证明三条写路径依赖皆通（见 reportStoreHealthy）。
// 公共路径的重复键属调用方契约冲突，原样上抛而不上报失败、不落盘。
func (s *ErrorTrackingStore) StoreEvent(key int64, event FullEvent) error {
	err := s.inner.StoreEvent(key, event)
	if err != nil {
		if IsDuplicateEventKey(err) {
			return err
		}
		s.report(classifyStoreErr(err), err)
		s.spillEvent(key, event)
	} else {
		s.reportStoreHealthy()
	}
	return err
}

// StoreEventWithEmbedding 的归因与兜底语义同 StoreEvent；兜底条目不携带向量，重放走文本路径重新嵌入。
func (s *ErrorTrackingStore) StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error {
	err := s.inner.StoreEventWithEmbedding(key, event, embedding)
	if err != nil {
		if IsDuplicateEventKey(err) {
			return err
		}
		s.report(classifyStoreErr(err), err)
		s.spillEvent(key, event)
	} else {
		s.reportStoreHealthy()
	}
	return err
}

// ReplayEvent 透传内部回放并上报归因。与 StoreEvent 不同：回放失败不做兜底落盘——可靠收件箱
// 已自持这些事件的 at-least-once 重试，再落一份会在兜底文件里造出重复条目。
func (s *ErrorTrackingStore) ReplayEvent(key int64, canonicalFact FullEvent) (ReplayResult, FullEvent, error) {
	replayer, ok := s.inner.(EventReplayer)
	if !ok {
		return ReplayNew, canonicalFact,
			errors.New("ErrorTrackingStore: inner store does not implement EventReplayer")
	}
	result, stored, err := replayer.ReplayEvent(key, canonicalFact)
	if err != nil {
		s.report(classifyStoreErr(err), err)
		return result, stored, err
	}
	s.reportStoreHealthy()
	return result, stored, nil
}

// reportStoreHealthy 在写成功时把记忆、磁盘、向量一并报为健康：一次成功写入即证明三条写路径
// 都通。缺这条，磁盘或向量一旦降级就再无恢复信号、只能等重启，违背"检测→降级→恢复"三段式。对
// 已正常的依赖上报成功只重置失败计数，故三报无副作用。
func (s *ErrorTrackingStore) reportStoreHealthy() {
	s.report(depMemory, nil)
	s.report(depDisk, nil)
	s.report(depRustViking, nil)
}

// DeleteEvent 透传删除；失败按依赖归因上报，成功不报恢复（删除成功不证明写路径）。
func (s *ErrorTrackingStore) DeleteEvent(key int64) error {
	err := s.inner.DeleteEvent(key)
	if err != nil {
		s.report(classifyStoreErr(err), err)
	}
	return err
}

// SearchByEmbedding 透传语义检索。"不支持向量"是能力声明而非依赖故障，故不上报——否则未配置
// 语义检索的部署一调用就把向量依赖打成降级。真失败仍走归因，不无条件算给向量依赖。
func (s *ErrorTrackingStore) SearchByEmbedding(query []float32, topK int) ([]EventReference, error) {
	refs, err := s.inner.SearchByEmbedding(query, topK)
	if err != nil && !errors.Is(err, ErrVectorSearchNotSupported) {
		s.report(classifyStoreErr(err), err)
	}
	return refs, err
}

// GetEvent 透传单条读取；失败上报归因，成功不上报恢复（读通不代表写依赖已恢复）。
func (s *ErrorTrackingStore) GetEvent(key int64) (*FullEvent, error) {
	e, err := s.inner.GetEvent(key)
	if err != nil {
		s.report(classifyStoreErr(err), err)
	}
	return e, err
}

// GetEvents 批量读取，错误处理语义同 GetEvent。
func (s *ErrorTrackingStore) GetEvents(keys []int64) ([]FullEvent, error) {
	events, err := s.inner.GetEvents(keys)
	if err != nil {
		s.report(classifyStoreErr(err), err)
	}
	return events, err
}

// QueryEvents 条件查询，错误处理语义同 GetEvent。
func (s *ErrorTrackingStore) QueryEvents(query QueryOptions) ([]EventReference, error) {
	refs, err := s.inner.QueryEvents(query)
	if err != nil {
		s.report(classifyStoreErr(err), err)
	}
	return refs, err
}

// SupportsVectorSearch 纯透传，无错误语义。
func (s *ErrorTrackingStore) SupportsVectorSearch() bool { return s.inner.SupportsVectorSearch() }

// GetStats 纯透传，无错误语义。
func (s *ErrorTrackingStore) GetStats() StoreStats { return s.inner.GetStats() }

// MemoryEngine 透传内层语义引擎（无则 nil）。
func (s *ErrorTrackingStore) MemoryEngine() MemoryEngine {
	if p, ok := s.inner.(MemoryEngineProvider); ok {
		return p.MemoryEngine()
	}
	return nil
}

// KVBackend 透传内层 KV 底座（无则 nil）。
func (s *ErrorTrackingStore) KVBackend() KVStore {
	if p, ok := s.inner.(KVProvider); ok {
		return p.KVBackend()
	}
	return nil
}

// RemoveVector 把遗忘联动移除向量透传给内层（无该能力则空操作）。
func (s *ErrorTrackingStore) RemoveVector(eventKey int64) {
	if r, ok := s.inner.(VectorRemover); ok {
		r.RemoveVector(eventKey)
	}
}

// RelationStore 透传因果关系存储（无则 nil，调用方按"无关系能力"处理）。
func (s *ErrorTrackingStore) RelationStore() RelationStore {
	if p, ok := s.inner.(RelationStoreProvider); ok {
		return p.RelationStore()
	}
	return nil
}

// Close 透传内层关闭以回收资源（内层无 Close 则返回 nil）。
func (s *ErrorTrackingStore) Close() error {
	if c, ok := s.inner.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}
