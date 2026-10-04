package embedder // import "github.com/SpellingDragon/tagent/memory/embedder"

Package embedder 承载 Embedder 契约的实现（mock／zhipu HTTP 供应商／traced 装饰器）。 分包原则与契约居核心包的
memory.Embedder 一致：本包只依赖核心 memory 包的接口与数据类型， 消费方无需认识具体供应商；新增供应商的接线点在组合根。已裁决：嵌入走
tagent 侧 HTTP 供应商， 不用 rustviking CLI（其向量索引为进程内易失）。

TYPES

type MockEmbedder struct {
	// Has unexported fields.
}
    MockEmbedder 用文本哈希把内容映射到固定维度的确定性伪向量，实现 memory.Embedder。
    相同文本必得相同向量，共享词元越多余弦越高；仅用于验证机制（融合、过滤、降级），
    不承诺真实语义质量——以它通过的测试不能推断线上召回效果。零值实例仍可用。

func NewMockEmbedder(dim int) *MockEmbedder
    NewMockEmbedder 创建确定性 mock 嵌入器（dim<=0 时取 64）。

func (m *MockEmbedder) Dimension() int
    Dimension 返回构造时确定的维度。

func (m *MockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error)
    Embed 对每条文本做词元哈希袋（bag-of-token-hashes）→ L2 归一化向量。
    共享词元产生重叠维度，故余弦相似度随词元重叠单调——足以驱动 RRF 机制测试。

func (m *MockEmbedder) ModelID() string
    ModelID 返回含维度的标识，使换维度后的旧向量在重建时被跳过。

type TracedEmbedder struct {
	// Has unexported fields.
}
    TracedEmbedder 装饰任意 Embedder，为每次嵌入产生 span（对齐上游 GenAI
    语义约定）并记录 调用数／文本条数／维度分布。两条不变量：可观测只在装饰器内部产生，工具与引擎的 Declaration
    零触碰（否则每次加可观测都会扰动模型可见声明、破坏 prefix-cache 稳定性）；未配置导出时全局 provider 为
    noop，本装饰器仅透传、行为逐字不变。属性只带元数据，嵌入内容不入 span。

func NewTracedEmbedder(inner memory.Embedder) *TracedEmbedder
    NewTracedEmbedder 包裹 inner 加向量链路可观测。inner 为 nil 返回 nil。metric 创建失败 用 noop
    计数（otel 保证返回可用零值，不阻断）。

func (t *TracedEmbedder) Dimension() int
    Dimension 透传 inner（span/metric 不改变维度语义）。

func (t *TracedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error)
    Embed 开 span（GenAI 属性）→ 委托 inner → 记 metric。ctx 取消/超时透传 inner（尊重 ctx）。

func (t *TracedEmbedder) ModelID() string
    ModelID 透传 inner（索引指纹比对不受装饰影响）。

type ZhipuEmbedder struct {
	// Has unexported fields.
}
    ZhipuEmbedder 经 openai 兼容的 /embeddings 端点生成向量，实现 memory.Embedder（密钥默认 复用
    ZAI_API_KEY）。分批、重试分类与 index 还原契约见文档。

func NewZhipuEmbedder(cfg ZhipuEmbedderConfig) (*ZhipuEmbedder, error)
    NewZhipuEmbedder 构建嵌入器。apiKey 为空时从 cfg.APIKeyEnv（默认 ZAI_API_KEY）
    环境变量读取；仍为空则返回 error（调用方据此判定「未配置=功能关闭」优雅降级）。

func (z *ZhipuEmbedder) Dimension() int
    Dimension 返回请求维度；0 表示用模型默认维度（尚未探测）。

func (z *ZhipuEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error)
    Embed 批量嵌入，按 MaxBatch 切片分批；每批失败重试 ≤1 次后放弃该批（返回 error，
    由上层丢弃该事件向量——关键词路径兜底，向量缺失不报错、不中断主链路）。

func (z *ZhipuEmbedder) ModelID() string
    ModelID 返回模型标识（含维度），供索引指纹比对以跳过旧向量。

type ZhipuEmbedderConfig struct {
	// Endpoint 是 embeddings 端点基址（含 /embeddings 或到 /v4 由实现补全）。
	// 默认 https://open.bigmodel.cn/api/paas/v4/embeddings。
	Endpoint string
	// Model 默认 "embedding-3"。
	Model string
	// APIKeyEnv 是读取密钥的环境变量名，默认 ZAI_API_KEY（GLM Coding Plan）。
	APIKeyEnv string
	// Dimensions 请求维度（embedding-3 支持 512/1024/2048 等）；0 = 用模型默认。
	Dimensions int
	// Timeout 单次 HTTP 超时，默认 30s。
	Timeout time.Duration
	// MaxBatch 单批最大条数，默认 16（超出切片分批）。
	MaxBatch int
}
    ZhipuEmbedderConfig 配置 zhipu 兼容嵌入端点。
