package kv // import "github.com/SpellingDragon/tagent/memory/kv"

Package kv 提供 memory.KVStore 的可选后端：LocalFileKV（内存 map ＋ 单张原子快照， 只用于跨进程验证，无
fsync、不保证掉电安全）、RustVikingClient（封装 rustviking CLI 的 JSON 契约，range 由公共前缀扫描模拟）与
MockRustVikingClient（测试替身，扫描同样 按字典序）。

契约: docs/wiki/memory/memory-architecture.md#local-file-kv

TYPES

type CLIResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}
    CLIResponse 表示 RustViking CLI 的统一 JSON 响应。

type KVOp struct {
	// Type "put" or "delete"
	Type  string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}
    KVOp 表示一个批量操作。

type KVPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
    KVPair 表示一个键值对。

type LocalFileKV struct {
	// Has unexported fields.
}
    LocalFileKV is a MINIMAL file-backed memory.KVStore, used ONLY as the MVP
    cross-process verification backend for the resident reliability protocol.
    It is a deliberately temporary model: an in-memory map persisted as a single
    JSON snapshot. It provides NO production durability, security, or long-term
    availability/maintainability guarantees — those are deferred to a dedicated
    storage engine (e.g. rustviking) wired in a later phase.

    Layout: kv.json — a full-map snapshot rewritten atomically (write tmp +
    rename) on Sync/Close. A POSIX rename is atomic, so a process KILL can never
    leave a torn snapshot: a reopen always sees the last successfully Synced
    state.

    Durability model (verification-grade only): writes update the in-memory
    map immediately, so in-process reads are always consistent; Sync() is
    the barrier that persists the map. A committed fact becomes visible to a
    fresh process only after its commit barrier ran Sync() — which is exactly
    what FileSegmentStore does. There is intentionally NO fsync: this backend
    survives a process restart / reopen (the guarantee actually verified), NOT
    an OS power loss. A write that was never Synced is lost on restart (honest
    "flush-only" semantics). A later storage-engine backend replaces this whole
    file.

func NewLocalFileKV(dataDir string, opts ...LocalFileKVOption) (*LocalFileKV, error)
    NewLocalFileKV opens (creating if needed) the directory and loads any
    existing kv.json snapshot.

func (k *LocalFileKV) Close() error
    Close persists any pending changes so every acknowledged write is on disk
    before returning. Idempotent.

func (k *LocalFileKV) KVBatch(ops []memory.KVOp) error
    KVBatch applies a batch of put/delete operations to the in-memory map
    (durable after the next Sync barrier).

func (k *LocalFileKV) KVDelete(key string) error
    KVDelete removes a key (durable after the next Sync barrier).

func (k *LocalFileKV) KVGet(key string) (string, error)
    KVGet retrieves the value for the key. A missing key returns an error
    wrapping memory.ErrKeyNotFound.

func (k *LocalFileKV) KVPut(key, value string) error
    KVPut stores a key-value pair in the in-memory map. In-process reads see it
    immediately; it is durable to a fresh process only after a subsequent Sync()
    barrier. A nil return is NOT a durability guarantee.

func (k *LocalFileKV) KVRange(start, end string, limit int) ([]memory.KVPair, error)
    KVRange returns all key-value pairs whose keys fall in [start, end), sorted
    lexicographically by key. If limit > 0, at most limit pairs are returned.

func (k *LocalFileKV) KVScan(prefix string, limit int) ([]memory.KVPair, error)
    KVScan returns all key-value pairs whose keys start with the given prefix,
    sorted lexicographically by key. If limit > 0, at most limit pairs are
    returned.

func (k *LocalFileKV) ListPartitionIDs() []int
    ListPartitionIDs enumerates persisted partition IDs from key namespaces
    (`{pid}:evt|idx|meta|tomb:…`; any persisted key in a partition's
    namespace proves the partition exists). Optional capability consumed
    by FileSegmentStore via a type assertion for cold-partition discovery;
    non-partition namespaces (e.g. `global:*`) are ignored.

func (k *LocalFileKV) Sync() error
    Sync is the durability barrier: it persists the in-memory map to the
    snapshot via an atomic tmp+rename. After a successful Sync the data is
    visible to a fresh process. A no-op when nothing changed since the last
    flush. Safe to call concurrently.

type LocalFileKVOption func(*LocalFileKV)
    LocalFileKVOption is retained purely for call-site compatibility (wiring
    passes WithFSync from MemoryConfig). In the minimal model it is a no-op.

func WithFSync(enabled bool) LocalFileKVOption
    WithFSync is accepted but IGNORED by the minimal verification backend (there
    is no fsync either way). It remains only so existing config plumbing stays
    stable until a real storage engine gives durability modes meaning again.

type MockRustVikingClient struct {
	// Has unexported fields.
}
    MockRustVikingClient 是 RustVikingClient 的内存 mock，用于开发和测试。

func NewMockRustVikingClient() *MockRustVikingClient
    NewMockRustVikingClient 创建 MockRustVikingClient。

func (m *MockRustVikingClient) KVBatch(ops []memory.KVOp) error
    KVBatch 经 stdin 提交一批 put/delete 操作。

func (m *MockRustVikingClient) KVDelete(key string) error
    KVDelete 删除一个键。

func (m *MockRustVikingClient) KVGet(key string) (string, error)
    KVGet 读取键值；键不存在时返回包装 memory.ErrKeyNotFound 的类型化错误。

func (m *MockRustVikingClient) KVPut(key, value string) error
    KVPut 写入一对键值。

func (m *MockRustVikingClient) KVRange(start, end string, limit int) ([]memory.KVPair, error)
    KVRange 返回 [start, end) 内的键值；由公共前缀扫描加客户端过滤模拟。

func (m *MockRustVikingClient) KVScan(prefix string, limit int) ([]memory.KVPair, error)
    KVScan 按前缀扫描，结果按字典序排序，limit 在排序后截断。

type RustVikingClient struct {
	// Has unexported fields.
}
    RustVikingClient 封装对 rustviking CLI 的调用。

func NewRustVikingClient(binaryPath, configPath string) *RustVikingClient
    NewRustVikingClient 创建 RustVikingClient。 binaryPath: rustviking 二进制路径（空值使用
    "rustviking"） configPath: 配置文件路径（config.toml），用于指定存储目录等设置

func (c *RustVikingClient) KVBatch(ops []memory.KVOp) error
    KVBatch 批量写入（通过 stdin pipe）。 rustviking 期望 JSON 格式:
    [{"op":"put","key":"k1","value":"v1"},{"op":"delete","key":"k2"}]

func (c *RustVikingClient) KVDelete(key string) error
    KVDelete 删除单个 KV。

func (c *RustVikingClient) KVGet(key string) (string, error)
    KVGet 获取单个 KV 的值。 注意: rustviking CLI 在 key 不存在时返回 null value（而非错误）。

func (c *RustVikingClient) KVPut(key, value string) error
    KVPut 写入单个 KV。

func (c *RustVikingClient) KVRange(start, end string, limit int) ([]memory.KVPair, error)
    KVRange 范围扫描。 注意: rustviking CLI 不直接支持 range 操作，使用 KVScan 扫描公共前缀后过滤。

func (c *RustVikingClient) KVScan(prefix string, limit int) ([]memory.KVPair, error)
    KVScan 前缀扫描。

func (c *RustVikingClient) VectorDelete(id uint64) error
    VectorDelete 删除向量（index delete，F1 确认真实存在——修正报告「无 VectorDelete」
    的包装层局限判断）。best-effort：不存在时按成功处理由调用方决定。

func (c *RustVikingClient) VectorInsert(id uint64, vector []float32, level uint8) error
    VectorInsert 插入或覆盖一条向量（index insert）。**当前无生产调用方**：MVP 的向量持久化走 KV
    序列化＋启动重建，而原生 index 是进程内易失索引，两条路线互斥。level 的语义未经真实 二进制验证，且显式传 0 会偏离 rustviking
    默认 level=1；接线前须先实测 0/1 的索引结构差异 再定传参。完整约束见文档。

    契约: docs/wiki/memory/memory-architecture.md#rv-vector-cmds

func (c *RustVikingClient) VectorSearch(query []float32, k int) ([]VectorResult, error)
    VectorSearch 向量检索（index search），返回按相似度排序的命中（含 score）。

type VectorResult struct {
	ID    uint64  `json:"id"`
	Score float32 `json:"score"`
	Level uint8   `json:"level"`
}
    VectorResult 是向量检索的单条命中：rustviking 返回的 id、相似度分与索引层级。

