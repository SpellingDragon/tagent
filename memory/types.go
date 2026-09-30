package memory

import (
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// EventReference 是指向已存事件的轻量引用（键、类型、摘要、时间、角色）。会话侧只持有引用
// 列表，全文按需用 GetEvent/GetEvents 水合。字段单位与取值属契约，见文档。
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
type EventReference struct {
	EventKey     int64  `json:"event_key"`
	PartitionID  int    `json:"partition_id,omitempty"`
	EventType    string `json:"event_type"`
	EventSummary string `json:"event_summary"`
	Timestamp    int64  `json:"timestamp"`
	Role         string `json:"role,omitempty"`
}

// FullEvent 是一条事件的完整记录，也是事件数据的唯一真源。
//
// 字段语义与单位取值、两条时间轴的分工、因果父引用的存放位置，均以文档为唯一真源。
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
type FullEvent struct {
	EventKey     int64  `json:"event_key"`
	PartitionID  int    `json:"partition_id"`
	EventType    string `json:"event_type"`
	EventSummary string `json:"event_summary"`
	Timestamp    int64  `json:"timestamp"`
	Content      string `json:"content"`
	// ContentParts 承载文本正文为空、内容在部件里（图/文件/音）的输入：一次有效输入必须同时
	// 存活于事实链与真正发出的请求，只留一条路径等于丢失。附加式且 omitempty，故本字段之前
	// 存下的记录解码不变。
	ContentParts []model.ContentPart `json:"content_parts,omitempty"`
	ToolCalls    []model.ToolCall    `json:"tool_calls"`
	// ToolID 记录本工具结果对应哪次调用，保证跨存储→解析不丢配对。
	ToolID      string                 `json:"tool_id,omitempty"`
	ToolResults map[string]interface{} `json:"tool_results"`
	Metadata    map[string]string      `json:"metadata"`
	// Response 是可选的 LLM 响应快照；按契约视为只读，故不随本结构深拷贝。
	Response *model.Response `json:"response,omitempty"`
}

// MemoryStore 是事件的存储与读取接口，也是事件数据的唯一真源。
// 存储隔离：以 PartitionID 作为分区键——记忆层不认识 agent，分区是纯存储概念；
// "身份 → 分区"的映射发生在 MemoryStore 之外（插件层），以保持记忆层存储语义干净。
type MemoryStore interface {
	// StoreEvent 写入单条事件及其完整细节。
	StoreEvent(key int64, event FullEvent) error

	// GetEvent 按事件键读取单条事件。
	GetEvent(key int64) (*FullEvent, error)

	// GetEvents 按键批量读取，返回顺序与 keys 一致，缺失键跳过而不报错。
	GetEvents(keys []int64) ([]FullEvent, error)

	// QueryEvents 按条件查询，返回轻量引用列表（不返回全文）。
	QueryEvents(query QueryOptions) ([]EventReference, error)

	// SearchByEmbedding 以查询向量做语义检索，返回排序票据。
	SearchByEmbedding(query []float32, topK int) ([]EventReference, error)

	// StoreEventWithEmbedding 写入事件并登记其向量。
	StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error

	// SupportsVectorSearch 报告本存储是否具备向量能力（不支持时调用方退回关键词路）。
	SupportsVectorSearch() bool

	// DeleteEvent 永久删除事件；受保留租约保护时必须被拒（见 ErrEventProtected）。
	DeleteEvent(key int64) error

	// GetStats 返回存储统计。
	GetStats() StoreStats
}

var (
	// ErrVectorSearchNotSupported 表示后端不具备向量能力：调用方必须退回关键词路，
	// 而不是把"不支持"当成"没有结果"。
	ErrVectorSearchNotSupported = fmt.Errorf("vector search not supported")
)

// RelationStoreProvider 是可选接口：并非所有存储都暴露关系操作，调用方访问父子关系前必须先
// 断言本接口存在，断言失败按"无关系能力"处理而不是报错。
type RelationStoreProvider interface {
	RelationStore() RelationStore
}

var (
	_ RelationStoreProvider = (*InMemoryStore)(nil)
	_ RelationStoreProvider = (*FileSegmentStore)(nil)
)

// QueryOptions specifies filters for querying events.
type QueryOptions struct {
	// PartitionID filters events by storage partition.
	// 0 = no partition filter (query across all partitions).
	PartitionID int `json:"partition_id"`
	// PartitionIDs filters events across multiple partitions.
	// Takes precedence over PartitionID if non-empty.
	PartitionIDs []int    `json:"partition_ids"`
	EventTypes   []string `json:"event_types"`
	// StartTime/EndTime filter events by timestamp, in Unix MILLISECONDS
	// (same unit as FullEvent.Timestamp and the recall tools' since/until).
	// Zero = no bound.
	StartTime int64 `json:"start_time"`
	EndTime   int64 `json:"end_time"`
	// MinEventKey filters events whose EventKey is STRICTLY GREATER than this
	// value — the write-order axis, never semantic time (see the TIME CONTRACT
	// on FullEvent: async write-back events carry an EventKey assigned at
	// write time while their Timestamp is the bus-arrival moment, so a
	// StartTime approximation mis-cuts such stragglers). 0 = no bound.
	MinEventKey int64  `json:"min_event_key,omitempty"`
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset"`
	OrderBy     string `json:"order_by"`
	// Keyword filters events whose EventSummary or Content contains the keyword (case-insensitive).
	// Empty string = no keyword filter.
	Keyword string `json:"keyword,omitempty"`
}

// StoreStats 是存储统计。
//
// 契约: docs/wiki/memory/memory-architecture.md#counts-known
type StoreStats struct {
	// TotalEvents 是活跃事件数；仅在 CountsKnown 为真时可信赖。
	TotalEvents int `json:"total_events"`
	// StorageSize 是占用字节数，DataDir 是数据目录。
	StorageSize int64  `json:"storage_size"`
	DataDir     string `json:"data_dir"`
	// CountsKnown 为假表示活跃计数无法从事实链重建（后端不支持分区枚举，或扫描失败）。
	// 计数未知时容量驱逐必须暂停——未知绝不得伪装成精确的 0，理由见文档。
	CountsKnown bool `json:"counts_known"`
}

// ReplayResult 区分一次内部回放究竟发生了什么，调用方据此决定计数与副作用只加一次。
type ReplayResult int

const (
	// ReplayNew：此前不存在，已作为新提交写入，活跃计数加一。
	ReplayNew ReplayResult = iota
	// ReplayRepaired：半写孤儿被补全（索引在而原文或元数据缺），提交屏障已重跑，事件现已完整
	// 持久；活跃计数恰好加一，不多加。
	ReplayRepaired
	// ReplayAlreadyCommitted：发现已完整提交（原文逐字节相同且键在发布缓存内）。调用幂等，
	// 不加活跃计数。
	ReplayAlreadyCommitted
)

// EventReplayer 是可选能力接口，支持"以内容校验为前提"的内部回放。与公开 StoreEvent 不同
// （后者把任何已存在的键一律判为重复，哪怕内容逐字节相同），回放必须先确认已存原文与待提交内容
// 逐字节相同，才补写半写孤儿并重跑提交屏障；同键不同内容判为冲突且**绝不覆盖既有事实**。
// 返回的 ReplayResult 让调用方把计数与副作用精确加一次。
//
// 可靠收件箱与内存溢出落盘的恢复路径**必须**走本接口而非公开 StoreEvent，以显式区分"新提交"
// 与"回放"；不持有类型化断言的调用方继续用 StoreEvent。实现：文件段存储与内存存储，装饰链
// （引擎桥、错误统计存储）透明透传。
type EventReplayer interface {
	// ReplayEvent 走内部回放路径提交 canonicalFact，返回发生了什么、以及**实际存储的**事实
	// （若已提交过，非内容字段可能与入参不同）。提交屏障失败或检出内容冲突时返回错误。
	ReplayEvent(key int64, canonicalFact FullEvent) (ReplayResult, FullEvent, error)
}

// RetentionGuard 是可选的"材料保留"恢复能力：恢复归属方为未确认材料的持久原文登记保留，
// 使 TTL 过期、容量驱逐与墓碑终清在安全释放（ack 目录同步完成或落盘项被移除）之前不销毁它们。
// 租约由文件段存储持有，装饰链（引擎桥、错误统计）把能力递归透传给它。
// 不具备持久恢复能力的后端直接不实现本接口——缺失即"无物可保留"，不是错误。
type RetentionGuard interface {
	// ProtectKey 为该键登记一个持有者（引用计数，幂等）。
	ProtectKey(key int64)
	// ReleaseKey 释放一个持有者；最后一个持有者离开后键恢复按年龄处理——原始时间戳绝不被重打。
	ReleaseKey(key int64)
	// ArmRetention 表示恢复归属方已从现存未确认材料重建完保留集，从而放行存储的首次破坏性扫描
	// （重启竞态门）。幂等；无保留集的存储忽略本调用。
	ArmRetention()
	// BeginHold/EndHold 抬起与放下登记屏障：任一持有者存在期间，生命周期扫描**无条件暂停**遗忘
	// 遍次（无宽限逃逸），使组合根的聚合清点或迟挂入的共享恢复目录能在任何破坏性遍次落地前
	// 保护其材料。每个 Begin 必须配一个 End；嵌套与并发持有者按引用计数。
	BeginHold()
	EndHold()
}

// RetentionHoldable 只暴露登记屏障那一面（由组合根构建门或迟挂入的恢复归属方抬起，在其清点
// 落地期间暂停遗忘）。文件段存储经由保留集满足它；无保留集（也就无破坏性扫描）的存储不满足，
// 调用方把"能力缺失"当作"无物可暂停"，而不是错误。
type RetentionHoldable interface {
	BeginHold()
	EndHold()
}

const (
	// partitionIDShift 等常量定义事件键的 64 位布局（Snowflake 式）：符号位恒 0——正键为真实
	// 事件、负键保留给压实产出的合成摘要引用；分区 10 位（0-1023，由调用方推导，记忆层不解释）；
	// 秒级时间戳 31 位；同秒序号 10 位；低位保留。位宽为何只让出 10 位、以及跨重启单调性见文档。
	//
	// 契约: docs/wiki/memory/memory-architecture.md#event-key
	partitionIDShift = 53
	timestampShift   = 22
	sequenceShift    = 12

	// partitionIDMask 只取 10 位，故意让出符号位（理由见文档）。
	partitionIDMask = 0x3FF
	// timestampMask 与 sequenceMask 分别是 31 位秒数与 10 位同秒序号。
	timestampMask = 0x7FFFFFFF
	sequenceMask  = 0x3FF

	// snowflakeEpoch 是键内时间戳的零点基准（UTC），值即注释旁的字面量所指。
	snowflakeEpoch = 1704067200
)

// snowflakeSeqMu 保护各分区的发号计数器。
var snowflakeSeqMu sync.Mutex

// snowflakeSeqLast 记录各分区上一次已发出的时间戳（秒）。
var snowflakeSeqLast = make(map[int]int64)

// snowflakeSeqCnt 记录各分区在当前秒内已发出的序号。
var snowflakeSeqCnt = make(map[int]int)

// NewSnowflakeEventKey 生成 Snowflake 式事件键。nowMs 传 0 表示用真实时钟，传值表示由调用方
// 驱动时钟（测试语义）——只有真实时钟路径享受时钟回退保护。
func NewSnowflakeEventKey(partitionID int, nowMs int64) int64 {
	explicit := nowMs > 0
	if !explicit {
		nowMs = time.Now().UnixMilli()
	}
	ts := nowMs/1000 - snowflakeEpoch

	snowflakeSeqMu.Lock()
	if !explicit && ts < snowflakeSeqLast[partitionID] {
		ts = snowflakeSeqLast[partitionID]
	}
	if ts == snowflakeSeqLast[partitionID] {
		snowflakeSeqCnt[partitionID]++
	} else {
		snowflakeSeqCnt[partitionID] = 0
		snowflakeSeqLast[partitionID] = ts
	}
	seq := snowflakeSeqCnt[partitionID]
	snowflakeSeqMu.Unlock()

	return (int64(partitionID&partitionIDMask) << partitionIDShift) |
		((ts & timestampMask) << timestampShift) |
		(int64(seq&sequenceMask) << sequenceShift)
}

// RaiseSnowflakeFloor 用"磁盘上已发出的最大键"这一持久观测播种分区的单调性守卫：新进程继承
// 的是事实链而不是内存计数器，缺此播种则同秒内重启会重发与已提交事实相撞的键，而每次相撞都判
// 内容冲突、配合冻结键不得覆盖的规则会永久僵持。守卫单向：只有严格更高的观测值抬升它。
func RaiseSnowflakeFloor(partitionID int, highestIssuedKey int64) {
	if highestIssuedKey <= 0 {
		return
	}
	ts := (highestIssuedKey >> timestampShift) & timestampMask
	seq := (highestIssuedKey >> sequenceShift) & sequenceMask
	snowflakeSeqMu.Lock()
	defer snowflakeSeqMu.Unlock()
	if ts > snowflakeSeqLast[partitionID] ||
		(ts == snowflakeSeqLast[partitionID] && int64(seq) >= int64(snowflakeSeqCnt[partitionID])) {
		seq++
		if seq > sequenceMask {
			ts++
			seq = 0
		}
		snowflakeSeqLast[partitionID] = ts
		snowflakeSeqCnt[partitionID] = int(seq)
	}
}

// PartitionIDFromEventKey 从事件键取出分区号。
func PartitionIDFromEventKey(key int64) int {
	return int((key >> partitionIDShift) & partitionIDMask)
}

// TimestampFromEventKey 从事件键取出写入时刻的 Unix 秒（不是语义时间，见事件记录的时间契约）。
func TimestampFromEventKey(key int64) int64 {
	return ((key >> timestampShift) & timestampMask) + snowflakeEpoch
}

// SequenceFromEventKey 从事件键取出同秒序号。
func SequenceFromEventKey(key int64) int {
	return int((key >> sequenceShift) & sequenceMask)
}

var partitionIDCache sync.Map

// PartitionIDFromName 由名字确定性地推导分区号（同名恒同值，0-1023）。允许碰撞——分区用于
// 因果链隔离，不用于唯一性标识。
//
// 契约: docs/wiki/memory/memory-architecture.md#event-key
func PartitionIDFromName(name string) int {
	if v, ok := partitionIDCache.Load(name); ok {
		return v.(int)
	}
	h := fnv.New32a()
	h.Write([]byte(name))
	id := int(h.Sum32() & uint32(partitionIDMask))
	partitionIDCache.Store(name, id)
	return id
}

var globalPartitionCounter atomic.Int64

// NewPartitionID 在无稳定名字时由进程内原子计数器推导分区号。乘数为奇数，故在 10 位空间上是
// 双射：连续计数得到互不相同的分区号，周期恰为 1024——同一进程内超过 1024 个分区才会首次撞号。
func NewPartitionID() int {
	seq := globalPartitionCounter.Add(1)
	return int((seq * 1337) & partitionIDMask)
}
