package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/SpellingDragon/tagent/memory"
)

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

// ZhipuEmbedder 经 openai 兼容 /embeddings 端点生成向量（实现 memory.Embedder；
// 复用 GLM Coding Plan 的 ZAI_API_KEY）。
type ZhipuEmbedder struct {
	cfg    ZhipuEmbedderConfig
	client *http.Client
	apiKey string
}

var _ memory.Embedder = (*ZhipuEmbedder)(nil)

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
