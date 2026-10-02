package memory

// KVStore 抽象底层 KV 操作，是 FileSegmentStore 的持久化底座。契约与数据类型居核心包、
// 实现居子包 memory/kv（与 MemoryEngine、Embedder 同一切分原则）。语义约束：键为字符串
// （键格式由 memory/key_schema.go 单点定义）；Scan/Range 按字典序返回；limit<=0 不限制。
// 接入新后端的路径与接线点见文档。
//
// 契约: docs/wiki/memory/memory-architecture.md#extension-paths
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
	// Type 是操作类型，取 "put" 或 "delete"。
	Type  string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}
