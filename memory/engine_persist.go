package memory

import (
	"encoding/json"
	"strconv"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ==================== 向量 KV 持久层（T-A · rustviking-backed 持久化）====================
//
// 裁决依据 f1-rustviking-capability-report.md「追加发现」：rustviking `index` CLI 是
// 进程内易失索引，不可作持久后端；改用 rustviking / LocalFile **KV**（持久）序列化向量
// + 启动异步重建内存索引（= 原 hybrid 变更 D1-A 方案）。KVStore 接口两后端
// （RustVikingClient / LocalFileKV / MockRustVikingClient）皆可，引擎不感知具体后端。
//
// 优雅降级：KV 持久失败仅记日志 + 计数，绝不传染索引/检索主链路（向量是增强索引）。

// rebuildScanLimit 是启动重建时 KV 前缀扫描的条数上限。取大值以覆盖 MVP 规模
// （万级事件）；rustviking KV scan 需显式 -l（默认仅 100），故传大 limit。
// 超大规模的分页重建依赖 rustviking R5 迭代器（backlog），此处以 limit 兜底。
const rebuildScanLimit = 200000

// persistedVector 是向量 + 元数据的 KV 序列化形态（JSON，rustviking KV 接受 JSON value）。
// 元数据（pid/type/ts）随向量持久，使重建无需回读事件即可恢复过滤维度。
type persistedVector struct {
	Vec         []float32 `json:"v"`
	ModelID     string    `json:"m,omitempty"` // 嵌入模型指纹：换模型/维度后旧向量重建时跳过（审查 M3）
	PartitionID int       `json:"pid"`
	EventType   string    `json:"t"`
	Timestamp   int64     `json:"ts"`
}

// vecKVKey 构造向量的 KV 键：{prefix}{eventKey}。
func (e *InMemoryEngine) vecKVKey(eventKey int64) string {
	return e.vecPrefix + strconv.FormatInt(eventKey, 10)
}

// modelID 返回当前嵌入器模型标识（无嵌入器返回空）。用于持久化指纹比对。
func (e *InMemoryEngine) modelID() string {
	if e.emb != nil {
		return e.emb.ModelID()
	}
	return ""
}

// persistVectors 批量把向量写入 KV（worker flush 后调用）。失败仅记日志，不传染。
func (e *InMemoryEngine) persistVectors(batch []IndexableEvent, vecs [][]float32) {
	if e.kv == nil {
		return
	}
	ops := make([]KVOp, 0, len(batch))
	for i, evt := range batch {
		if i >= len(vecs) || len(vecs[i]) == 0 {
			continue
		}
		raw, err := json.Marshal(persistedVector{
			Vec:         vecs[i],
			ModelID:     e.modelID(),
			PartitionID: evt.PartitionID,
			EventType:   evt.EventType,
			Timestamp:   evt.Timestamp,
		})
		if err != nil {
			continue
		}
		ops = append(ops, KVOp{Type: "put", Key: e.vecKVKey(evt.EventKey), Value: string(raw)})
	}
	if len(ops) == 0 {
		return
	}
	if err := e.kv.KVBatch(ops); err != nil {
		log.Warnf("[InMemoryEngine] persist %d vectors failed: %v", len(ops), err)
	}
}

// removePersisted 删除 KV 中的向量（Remove 时调用，防重建复活已删向量）。
func (e *InMemoryEngine) removePersisted(eventKey int64) {
	if e.kv == nil {
		return
	}
	if err := e.kv.KVDelete(e.vecKVKey(eventKey)); err != nil {
		log.Debugf("[InMemoryEngine] remove persisted vector %d: %v", eventKey, err)
	}
}

// rebuildFromKV 启动时从 KV 扫描向量键空间，异步重建内存索引（不阻塞构造）。
// 重建期间向量渐进可用；rebuildDone 供可观测/测试同步。损坏/不可解析的键跳过 + 计数。
func (e *InMemoryEngine) rebuildFromKV() {
	if e.kv == nil {
		e.rebuildDone.Store(true)
		return
	}
	pairs, err := e.kv.KVScan(e.vecPrefix, rebuildScanLimit)
	if err != nil {
		log.Warnf("[InMemoryEngine] rebuild scan failed (prefix %q): %v", e.vecPrefix, err)
		e.rebuildDone.Store(true)
		return
	}
	var loaded, corrupt, staleModel int
	curModel := e.modelID()
	e.mu.Lock()
	for _, p := range pairs {
		key, err := strconv.ParseInt(strings.TrimPrefix(p.Key, e.vecPrefix), 10, 64)
		if err != nil || key <= 0 {
			corrupt++
			continue
		}
		var pv persistedVector
		if err := json.Unmarshal([]byte(p.Value), &pv); err != nil || len(pv.Vec) == 0 {
			corrupt++
			continue
		}
		// 换模型/维度后的旧向量：跳过，防维度不匹配 0 分候选与语义混用（审查 M3）。
		if curModel != "" && pv.ModelID != "" && pv.ModelID != curModel {
			staleModel++
			continue
		}
		e.vectors[key] = pv.Vec
		e.vmeta[key] = vectorMeta{partitionID: pv.PartitionID, eventType: pv.EventType, timestamp: pv.Timestamp}
		loaded++
	}
	e.mu.Unlock()
	e.indexedCount.Add(int64(loaded)) // 重建计入 indexed，与 vectorCount 语义一致（审查 Nit1）
	e.rebuildDone.Store(true)
	log.Infof("[InMemoryEngine] rebuilt %d vectors from KV (prefix %q, corrupt=%d, staleModel=%d)", loaded, e.vecPrefix, corrupt, staleModel)
}

// RebuildDone 报告 KV 重建是否完成（可观测/测试同步用；生产检索不等待，向量渐进可用）。
func (e *InMemoryEngine) RebuildDone() bool { return e.rebuildDone.Load() }
