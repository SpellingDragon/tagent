package plugin // import "github.com/SpellingDragon/tagent/plugin"

Package plugin 把记忆写入挂到框架的事件管线上：MemoryPlugin 负责筛选、持久化并在 同一同步点投影，SummaryPlugin
负责事件类型与元数据标注，Attribution 与 EchoCredential 经 ctx 提供回合级归因与精确回显识别。

契约: docs/wiki/plugin/plugin-architecture.md#overview

FUNCTIONS

func WithAttribution(ctx context.Context, a Attribution) context.Context
    WithAttribution 返回携带归因章的 ctx；空归因不写入，返回原 ctx。

func WithEchoCredential(ctx context.Context, c *EchoCredential) context.Context
    WithEchoCredential 让本次尝试的凭据经 ctx 传递；nil 不注入。

func WithProjectionSink(ctx context.Context, sink ProjectionSink) context.Context
    WithProjectionSink 绑定本次调用的投影接收端；调用链 ctx 天然隔离主循环与子 agent。


TYPES

type Attribution map[string]string
    Attribution 是回合级归因章（写入 FullEvent.Metadata），由 RunFlow 每回合经 ctx 绑定。

    契约: docs/wiki/plugin/plugin-architecture.md#attribution-carrier

func AttributionFrom(ctx context.Context) (Attribution, bool)
    AttributionFrom 从 ctx 取回归因章；无有效归因时返回 (nil, false)。

type EchoCredential struct {
	AttemptToken  string
	Agent         string
	Session       string
	MergedMessage string
	CommittedKeys []int64

	// Has unexported fields.
}
    EchoCredential 标识本次尝试期望的输入回显，使 MemoryPlugin 精确跳过那一条的重复入库。 它按每次 runner
    尝试新建、随该调用的 ctx 结束而释放，不存在回合级或「首个信封」状态。 绑定与拒绝状态由内部字段承载，判定粒度是根调用 id（框架事件不携带逐事件
    id）。

func EchoCredentialFrom(ctx context.Context) (*EchoCredential, bool)
    EchoCredentialFrom 取回本次尝试的凭据；不存在或为 nil 时返回 (nil, false)。

func (c *EchoCredential) Bind(invocationID string)
    Bind 记录首次匹配所在的根调用 id；同一尝试内的后续匹配不覆盖（幂等）。

func (c *EchoCredential) BoundID() string
    BoundID 返回首次匹配绑定的调用 id；未绑定时为空串。

func (c *EchoCredential) MarkRejected(reason string)
    MarkRejected 把已装入的凭据降级为不可验证并记录首个原因（粘滞）。

func (c *EchoCredential) Verified() bool
    Verified 报告期望回显是否已被正面确认（已绑定且未被降级）。装入了凭据而未 Verified 的回合， 模型入口与 ack 决策都必须失败关闭。

type MemoryPlugin struct {
	// Has unexported fields.
}
    MemoryPlugin 把框架事件管线的输出同步写入 MemoryStore，并在同一同步点投影到本调用的
    ProjectionSink。它按顺序跳过无载荷屏障事件、流式分片、退化空终态与本次尝试的精确输入回显； 因果父子关系按 (partition,
    session) 独立维护并有上界，经 RelationStore 承载。 存储标识与归因随事件写回 StateDelta 与
    FullEvent.Metadata。

    契约: docs/wiki/plugin/plugin-architecture.md#memory-plugin

func NewMemoryPlugin(store memory.MemoryStore) *MemoryPlugin
    NewMemoryPlugin 创建一个把事件写入 store 并同步投影的插件；因果链状态初始为空。

func (p *MemoryPlugin) Name() string
    Name 返回插件名 memory。

func (p *MemoryPlugin) OnEvent(
	ctx context.Context,
	inv *agent.Invocation,
	evt *event.Event,
) (*event.Event, error)
    OnEvent 是内部事件钩子的导出形式，供测试与工具直接调用；生产路径经 Register 注入。

func (p *MemoryPlugin) Register(r *plugin.Registry)
    Register 把本插件挂到框架的 OnEvent 钩子。

type ProjectionSink interface {
	Append(ref memory.EventReference)
}
    ProjectionSink 接收事件在持久化当刻产生的引用；agent 的 SessionProjection 实现它。
    它是「写入即投影」的唯一同步点，使投影完成先于 BeforeModel 由构造保证。

    契约: docs/wiki/plugin/plugin-architecture.md#skip-set

func ProjectionSinkFrom(ctx context.Context) (ProjectionSink, bool)
    ProjectionSinkFrom 取回本调用的投影接收端；无则返回 (nil, false)。

type SummaryPlugin struct{}
    SummaryPlugin 只做事件类型与元数据标注：它附加的 event_summary 是原文视图 （多数类型为原文，action_command
    为一行工具调用行），不承担内容摘要——内容级摘要 发生在压缩与策展阶段。

    契约: docs/wiki/plugin/plugin-architecture.md#summary-plugin

func NewSummaryPlugin() *SummaryPlugin
    NewSummaryPlugin 创建一个无状态的 SummaryPlugin。

func (p *SummaryPlugin) Name() string
    Name 返回插件名 summary。

func (p *SummaryPlugin) Register(r *plugin.Registry)
    Register 把本插件挂到框架的 OnEvent 钩子。

