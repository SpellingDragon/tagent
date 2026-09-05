package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"os"
	"time"
)

// ==================== Embedder（T-A · 引擎内部构件）====================
//
// Embedder 是文本→向量的抽象，属记忆引擎实现的内部构件（不进 C6 解耦缝——
// tagent 核心只依赖 MemoryEngine，不直接依赖 Embedder）。
//
// 实现：
//   - MockEmbedder：确定性哈希向量，无网络，供单测/开发/降级验证（同义改写不命中，
//     但相同/前缀文本相近，足以验证 hybrid 融合与分区过滤的机制正确性）。
//   - ZhipuEmbedder：zhipu embedding-3（openai 兼容 /embeddings），复用 GLM Coding
//     Plan 的 ZAI_API_KEY；维度可配；HTTP 超时 + 重试 ≤1；失败丢弃该批（关键词兜底）。
//
// 裁决依据：f1-rustviking-capability-report.md DECIDED F1-③（嵌入用 tagent 侧 zhipu
// HTTP，非 rustviking mock/CLI——rustviking 无 embed CLI 且默认 mock）。

// Embedder 文本向量化。批量语义：返回与 texts 等长、顺序对应的向量切片。
type Embedder interface {
	// Embed 批量嵌入。实现 MUST 尊重 ctx 取消/超时。
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dimension 返回向量维度；0 = 未知（尚未探测）。
	Dimension() int
	// ModelID 返回嵌入模型标识（用于索引指纹比对，防换模型后向量混用）。
	ModelID() string
}

// ---------------------------------------------------------------------------
// MockEmbedder — 确定性哈希向量（无网络）
// ---------------------------------------------------------------------------

// MockEmbedder 用 FNV 哈希把文本映射到固定维度的确定性伪向量。
// 语义：相同文本 → 相同向量；共享词元越多 → 余弦越高（弱语义）。
// 仅用于验证机制（融合/过滤/降级），不承诺真实语义质量。
type MockEmbedder struct {
	dim int
}

// NewMockEmbedder 创建确定性 mock 嵌入器（dim<=0 时取 64）。
func NewMockEmbedder(dim int) *MockEmbedder {
	if dim <= 0 {
		dim = 64
	}
	return &MockEmbedder{dim: dim}
}

func (m *MockEmbedder) Dimension() int { return m.dim }
func (m *MockEmbedder) ModelID() string {
	return fmt.Sprintf("mock-embed-%d", m.dim)
}

// Embed 对每条文本做词元哈希袋（bag-of-token-hashes）→ L2 归一化向量。
// 共享词元产生重叠维度，故余弦相似度随词元重叠单调——足以驱动 RRF 机制测试。
func (m *MockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = m.embedOne(t)
	}
	return out, nil
}

func (m *MockEmbedder) embedOne(text string) []float32 {
	dim := m.dim
	if dim <= 0 {
		dim = 64 // 零值 MockEmbedder{} 兜底，防除零 panic（审查 Nit8）
	}
	vec := make([]float32, dim)
	// 词元哈希袋：按空白/标点粗切，每词元投到两个维度（增碰撞分辨）。
	start := 0
	for i := 0; i <= len(text); i++ {
		if i == len(text) || isDelim(text[i]) {
			if i > start {
				tok := text[start:i]
				h := fnv.New32a()
				_, _ = h.Write([]byte(tok))
				sum := h.Sum32()
				vec[sum%uint32(dim)] += 1.0
				vec[(sum>>7)%uint32(dim)] += 0.5
			}
			start = i + 1
		}
	}
	l2normalize(vec)
	return vec
}

func isDelim(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	case c >= 0x80: // UTF-8 多字节（中文等）：按字节聚合到词元，不切分
		return false
	default:
		return true
	}
}

// l2normalize 原地 L2 归一化（零向量保持不变，避免除零）。
func l2normalize(vec []float32) {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return
	}
	norm := float32(math.Sqrt(sum))
	for i := range vec {
		vec[i] /= norm
	}
}

// ---------------------------------------------------------------------------
// ZhipuEmbedder — zhipu embedding-3（openai 兼容 /embeddings）
// ---------------------------------------------------------------------------

// ZhipuEmbedderConfig 配置 zhipu 兼容嵌入端点。
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

// ZhipuEmbedder 经 openai 兼容 /embeddings 端点生成向量。
type ZhipuEmbedder struct {
	cfg    ZhipuEmbedderConfig
	client *http.Client
	apiKey string
}

// NewZhipuEmbedder 构建嵌入器。apiKey 为空时从 cfg.APIKeyEnv（默认 ZAI_API_KEY）
// 环境变量读取；仍为空则返回 error（调用方据此判定「未配置=功能关闭」优雅降级）。
func NewZhipuEmbedder(cfg ZhipuEmbedderConfig) (*ZhipuEmbedder, error) {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://open.bigmodel.cn/api/paas/v4/embeddings"
	}
	if cfg.Model == "" {
		cfg.Model = "embedding-3"
	}
	if cfg.APIKeyEnv == "" {
		cfg.APIKeyEnv = "ZAI_API_KEY"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxBatch <= 0 {
		cfg.MaxBatch = 16
	}
	key := os.Getenv(cfg.APIKeyEnv)
	if key == "" {
		return nil, fmt.Errorf("zhipu embedder: env %s not set (embedding disabled)", cfg.APIKeyEnv)
	}
	return &ZhipuEmbedder{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		apiKey: key,
	}, nil
}

func (z *ZhipuEmbedder) Dimension() int { return z.cfg.Dimensions }
func (z *ZhipuEmbedder) ModelID() string {
	if z.cfg.Dimensions > 0 {
		return fmt.Sprintf("%s-%d", z.cfg.Model, z.cfg.Dimensions)
	}
	return z.cfg.Model
}

type embeddingsRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embeddingsResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Embed 批量嵌入，按 MaxBatch 切片分批；每批失败重试 ≤1 次后放弃该批（返回 error，
// 由上层丢弃该事件向量——关键词路径兜底，向量缺失不报错、不中断主链路）。
func (z *ZhipuEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += z.cfg.MaxBatch {
		end := start + z.cfg.MaxBatch
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := z.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

func (z *ZhipuEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ { // 重试 ≤1
		vecs, retryable, err := z.doEmbedRequest(ctx, texts)
		if err == nil {
			return vecs, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err() // ctx 取消不重试
		}
		if !retryable {
			return nil, err // 4xx（非 429）等不可恢复错误不重试（审查 Nit4：省配额、快反馈）
		}
		// 可恢复（429/5xx/网络）：短退避后重试。
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
		}
	}
	return nil, lastErr
}

// doEmbedRequest 发一次嵌入请求。返回 (向量, 是否可重试, 错误)：
// 网络错误/429/5xx 可重试；其余 4xx（密钥错、参数非法）与解析错误不可重试（审查 Nit4）。
func (z *ZhipuEmbedder) doEmbedRequest(ctx context.Context, texts []string) ([][]float32, bool, error) {
	body, err := json.Marshal(embeddingsRequest{
		Model:      z.cfg.Model,
		Input:      texts,
		Dimensions: z.cfg.Dimensions,
	})
	if err != nil {
		return nil, false, fmt.Errorf("marshal embeddings request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, z.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("build embeddings request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+z.apiKey)

	resp, err := z.client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("embeddings http: %w", err) // 网络错误可重试
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, retryable, fmt.Errorf("embeddings http %d: %s", resp.StatusCode, truncateForError(string(raw)))
	}
	var parsed embeddingsResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, false, fmt.Errorf("parse embeddings response: %w", err)
	}
	if parsed.Error != nil {
		return nil, false, fmt.Errorf("embeddings api error: %s", parsed.Error.Message)
	}
	// 按 index 排序还原（端点可能乱序返回）。
	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index >= 0 && d.Index < len(out) {
			out[d.Index] = d.Embedding
		}
	}
	for i, v := range out {
		if v == nil {
			return nil, false, fmt.Errorf("embeddings response missing index %d", i)
		}
	}
	return out, true, nil
}

func truncateForError(s string) string {
	const max = 300
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
