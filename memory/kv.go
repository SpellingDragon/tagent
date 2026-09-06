package memory

// ==================== KV 存储后端契约（接入指南见文末） ====================
//
// KVStore 是 FileSegmentStore 的持久化底座抽象。契约与数据类型居核心包
//（消费方 segment_store 在此；实现居子包 memory/kv —— 与 memory/engine 的
// 契约/实现切分同构）。

// KVStore 抽象底层 KV 操作（RustViking RocksDB / LocalFileKV JSON 文件 /
// 未来 RocksDB-direct、Redis、Badger 等后端）。语义约束：键为字符串
// （memory/key_schema.go 定义 evt/idx/meta/tomb 键格式），Scan/Range 按
// 字典序返回，limit<=0 表示不限制。
type KVStore interface {
	KVPut(key, value string) error
	KVGet(key string) (string, error)
	KVDelete(key string) error
	KVScan(prefix string, limit int) ([]KVPair, error)
	KVRange(start, end string, limit int) ([]KVPair, error)
	KVBatch(ops []KVOp) error
}

// KVPair 表示一个键值对。
type KVPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// KVOp 表示一个批量操作。
type KVOp struct {
	Type  string `json:"op"` // "put" or "delete"
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

// ==================== 拓展指南：接入新的记忆引擎 ====================
//
// tagent 记忆有两条相互独立的拓展路径，按需求选一条（或两条）：
//
// 路径 A — 新 KV 存储后端（换持久化底座，保留事件/检索全套语义）：
//  1. 在 memory/kv/ 子包新增 <backend>.go，实现 memory.KVStore（本文件接口）；
//  2. 在根包 tagent.go 的 resolveMemoryStore 路由加一个 case（构造
//     FileSegmentStore{KV: <backend>}），并在 MemoryConfig.Type 的合法值
//     校验（config.go Validate）登记新 type 名；
//  3. 测试：复用 memory/kv 现有 durability 测试模式（crash-safe 契约见
//     wiki/memory §16）。无需触碰压缩/召回/引擎任何代码。
//
// 路径 B — 新语义检索引擎（换向量/混合召回，MemoryEngine 已在 C6 冻结）：
//  1. 在 memory/engine/ 子包新增 <engine>_engine.go，实现 memory.MemoryEngine
//     （IndexBuilder + Retriever + Closer；可选实现 RawVectorSearcher /
//     StatsProvider，数据类型 IndexableEvent/Retrieval* 均在核心包）；
//  2. 嵌入器实现 memory/engine 的 Embedder 接口（或复用 zhipu/mock）；
//  3. 在根包 buildMemoryEngine 的 provider/backend 路由加 case；
//  4. 契约红线：返回排序票据（两段式，全文由调用方 GetEvents 水合）、
//     Ready()==false 时退化关键词而非报错、遗忘联动 RemoveVector。
//     详见 docs/wiki/platform/platform-subsystems.md 语义检索节。
//
// 两条路径均不要求修改核心包的存储/压缩/事件代码——resolveMemoryStore 与
// buildMemoryEngine 是唯一的接线点（根包 tagent.go）。
