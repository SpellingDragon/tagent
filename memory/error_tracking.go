package memory

import (
	"errors"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ==================== ErrorTrackingStore（T-G · 退化检测挂点，报告 D3 C2 契约最外层）====================
//
// 报告 D3 §4.2 冻结契约：resolveMemoryStore 装饰器串联 ErrorTrackingStore(engineBridge(FileSegmentStore))
// ——错误追踪在最外层。本装饰器是 DegradationManager 的**统一错误输入源**：此前 DegradationManager
// 状态机建成却零接线，根因正是这个报告设计的挂点缺失（仅 engine_bridge.go:16 注释引用它）。
//
// 设计原则（报告 line 1861「退化检测用装饰器而非改 plugin，避免两处分裂」）：非侵入透传 inner
// 全部方法（含可选接口，防 recall hybrid/Close 能力丢失），仅在错误返回时按特征归因依赖并旁路
// 上报，成功时上报恢复。MemoryPlugin 的错误处理（仅 log）不动。sink 为 nil 时纯透传——配置
// 门控关闭 = 现状逐字节零行为变化。

// DegradationSink 是错误上报目标（依赖倒置：memory 定义窄接口、用 string 依赖名，
// reliability.DegradationManager 经适配器满足——memory 不 import reliability，无环）。
type DegradationSink interface {
	ReportFailure(dep string, err error)
	ReportSuccess(dep string)
}

// 依赖名常量（值与 reliability.Dependency 一致，经 sink 桥接；避免 memory import reliability）。
const (
	depMemory     = "memory"
	depRustViking = "rustviking"
	depDisk       = "disk"
)

// ErrorTrackingStore 装饰 MemoryStore 做退化检测（C2 契约最外层）。
type ErrorTrackingStore struct {
	inner MemoryStore
	sink  DegradationSink // nil = 纯透传不上报（配置门控关闭）
	spill *MemSpill       // nil = 不落盘兜底；非 nil 时 StoreEvent 失败事件落 JSONL（步4，事件不丢）
}

// 编译期锁定 ErrorTrackingStore 是 MemoryStore。
var _ MemoryStore = (*ErrorTrackingStore)(nil)

// NewErrorTrackingStore 包裹 inner 做错误追踪。sink 为 nil 则纯透传（不上报）。返回具体类型
// 以支持 SetMemSpill/ReplaySpilled（步4 兜底），仍满足 MemoryStore 接口。
func NewErrorTrackingStore(inner MemoryStore, sink DegradationSink) *ErrorTrackingStore {
	return &ErrorTrackingStore{inner: inner, sink: sink}
}

// SetMemSpill 启用 memory 退化事件兜底（报告 D3 步4）：StoreEvent 失败时事件落 path 的 JSONL，
// 恢复后经 ReplaySpilled 重放（事件不丢，at-least-once 延伸到存储层）。path 空则禁用。
func (s *ErrorTrackingStore) SetMemSpill(path string) {
	s.spill = NewMemSpill(path)
}

// ReplaySpilled 重放兜底事件到 inner store（绕过自身防递归）。返回重放成功数。由 memory 依赖
// 恢复（DegradationManager onChange）或探针/运维触发。
func (s *ErrorTrackingStore) ReplaySpilled() (int, error) {
	if s.spill == nil {
		return 0, nil
	}
	return s.spill.Replay(s.inner)
}

// MemSpillLen 返回当前兜底待重放事件数（诊断/背压信号）。
func (s *ErrorTrackingStore) MemSpillLen() int {
	if s.spill == nil {
		return 0
	}
	return s.spill.Len()
}

// spillEvent 落盘 StoreEvent 失败的事件（步4 兜底，best-effort）。落盘失败仅告警——兜底也失败
// （如磁盘满）则事件真丢，但已尽最后努力（DegradationManager 已记 disk/memory 退化，可观测）。
func (s *ErrorTrackingStore) spillEvent(key int64, event FullEvent) {
	if s.spill == nil {
		return
	}
	if err := s.spill.Append(key, event); err != nil {
		log.Warnf("[ErrorTrackingStore] mem_spill append failed (event may be lost): %v", err)
	}
}

// classifyStoreErr 按 error 特征归因依赖（报告 D3 §5.2 降级矩阵）。disk 先判（S3：rustviking
// CLI 错误消息常内嵌 "rustviking"，若真因是 ENOSPC 会误归 rustviking 掩盖磁盘满，两者降级
// 动作不同）；rustviking 收窄到 fork/exec 与二进制缺失（CLI fork 失败确证），不用泛 "rustviking"
// 匹配（避免任何提及该词的业务错误都命中）；其余归 memory。
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

// report 旁路上报（sink nil 则 no-op）。err!=nil 上报失败归因，nil 上报该依赖成功恢复。
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

// === 写路径：错误归因上报，成功上报 memory 恢复（写成功 = memory+disk 均健康）===

func (s *ErrorTrackingStore) StoreEvent(key int64, event FullEvent) error {
	err := s.inner.StoreEvent(key, event)
	if err != nil {
		s.report(classifyStoreErr(err), err)
		s.spillEvent(key, event) // 步4：memory 退化事件兜底落盘（不丢，恢复后重放）
	} else {
		s.reportStoreHealthy()
	}
	return err
}

func (s *ErrorTrackingStore) StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error {
	err := s.inner.StoreEventWithEmbedding(key, event, embedding)
	if err != nil {
		s.report(classifyStoreErr(err), err)
		s.spillEvent(key, event) // 步4：兜底落盘（embedding 不重放，重放走 StoreEvent 文本路径重嵌入）
	} else {
		s.reportStoreHealthy()
	}
	return err
}

// reportStoreHealthy 写成功时上报存储栈三依赖恢复（M2：写成功证明 memory + disk + rustviking
// 写路径均健康）。否则 disk/rustviking 一旦 degraded 无恢复信号，卡到重启，违背「检测→降级→
// 恢复」三段式。ReportSuccess 对 normal 态依赖仅重置失败计数（无副作用），故对未退化依赖上报无害。
func (s *ErrorTrackingStore) reportStoreHealthy() {
	s.report(depMemory, nil)
	s.report(depDisk, nil)
	s.report(depRustViking, nil)
}

func (s *ErrorTrackingStore) DeleteEvent(key int64) error {
	err := s.inner.DeleteEvent(key)
	if err != nil {
		s.report(classifyStoreErr(err), err)
	}
	return err
}

// === 向量检索：归因 rustviking（向量索引依赖）===

func (s *ErrorTrackingStore) SearchByEmbedding(query []float32, topK int) ([]EventReference, error) {
	refs, err := s.inner.SearchByEmbedding(query, topK)
	// S1: ErrVectorSearchNotSupported 是能力声明（未配引擎），非依赖失败——不上报（否则未配
	// 语义检索的部署一调用即 rustviking degraded）。S2: 真失败按 classifyStoreErr 归因（引擎
	// 路径失败已退回 inner，能到此的 error 多来自 GetEvents → memory/disk；MVP 向量索引进程内
	// InMemoryEngine 与 rustviking 无关，不无条件归 rustviking）。
	if err != nil && !errors.Is(err, ErrVectorSearchNotSupported) {
		s.report(classifyStoreErr(err), err)
	}
	return refs, err
}

// === 读路径：错误归因上报（成功不上报——读成功不代表写依赖已恢复）===

func (s *ErrorTrackingStore) GetEvent(key int64) (*FullEvent, error) {
	e, err := s.inner.GetEvent(key)
	if err != nil {
		s.report(classifyStoreErr(err), err)
	}
	return e, err
}

func (s *ErrorTrackingStore) GetEvents(keys []int64) ([]FullEvent, error) {
	events, err := s.inner.GetEvents(keys)
	if err != nil {
		s.report(classifyStoreErr(err), err)
	}
	return events, err
}

func (s *ErrorTrackingStore) QueryEvents(query QueryOptions) ([]EventReference, error) {
	refs, err := s.inner.QueryEvents(query)
	if err != nil {
		s.report(classifyStoreErr(err), err)
	}
	return refs, err
}

// === 无错误语义方法：纯透传 ===

func (s *ErrorTrackingStore) SupportsVectorSearch() bool { return s.inner.SupportsVectorSearch() }
func (s *ErrorTrackingStore) GetStats() StoreStats       { return s.inner.GetStats() }

// === 可选接口透传：保持 inner 能力，否则包裹后 recall hybrid(MemoryEngineProvider)/
// 向量持久化(KVProvider)/遗忘移除(VectorRemover)/因果链(RelationStoreProvider)/引擎回收
// (Closer) 全部丢失。inner 未实现则返回 nil/no-op（下游均已 nil-safe）===

func (s *ErrorTrackingStore) MemoryEngine() MemoryEngine {
	if p, ok := s.inner.(MemoryEngineProvider); ok {
		return p.MemoryEngine()
	}
	return nil
}

func (s *ErrorTrackingStore) KVBackend() KVStore {
	if p, ok := s.inner.(KVProvider); ok {
		return p.KVBackend()
	}
	return nil
}

func (s *ErrorTrackingStore) RemoveVector(eventKey int64) {
	if r, ok := s.inner.(VectorRemover); ok {
		r.RemoveVector(eventKey)
	}
}

func (s *ErrorTrackingStore) RelationStore() RelationStore {
	if p, ok := s.inner.(RelationStoreProvider); ok {
		return p.RelationStore()
	}
	return nil
}

func (s *ErrorTrackingStore) Close() error {
	if c, ok := s.inner.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}
