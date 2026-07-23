package tagent_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// tencentEndpoint 与 examples/wechat-bot/tagent.yaml 的 providers.tencent 一致。
const tencentEndpoint = "https://tokenhub.tencentmaas.com/v1"

// hy3Result 聚合一次 hy3 调用的输出。
type hy3Result struct {
	content   string
	reasoning string
	err       error
}

// callHy3 用给定的 GenerationConfig 调用 hy3，聚合 content 与 reasoning_content。
func callHy3(ctx context.Context, m model.Model, gen model.GenerationConfig, prompt string) hy3Result {
	req := &model.Request{
		Messages:         []model.Message{model.NewUserMessage(prompt)},
		GenerationConfig: gen,
	}
	ch, err := m.GenerateContent(ctx, req)
	if err != nil {
		return hy3Result{err: err}
	}
	var content, reasoning strings.Builder
	for resp := range ch {
		if resp == nil {
			continue
		}
		if resp.Error != nil {
			return hy3Result{content: content.String(), reasoning: reasoning.String(), err: &apiErr{resp.Error.Message}}
		}
		if len(resp.Choices) == 0 {
			continue
		}
		c := resp.Choices[len(resp.Choices)-1]
		// 非流式在 Message，流式在 Delta，两者都聚合以兼容。
		content.WriteString(c.Message.Content)
		content.WriteString(c.Delta.Content)
		reasoning.WriteString(c.Message.ReasoningContent)
		reasoning.WriteString(c.Delta.ReasoningContent)
	}
	return hy3Result{content: content.String(), reasoning: reasoning.String()}
}

type apiErr struct{ msg string }

func (e *apiErr) Error() string { return e.msg }

// TestHy3ThinkingMode_RealAPI 用真实 tencent 端点验证 hy3 的 thinking 触发条件。
//
// 背景：wechat-bot 主 agent(tagent) 与 action agent 都用 hy3 且只配了
// `thinking_enabled: true`。本测试对比四种配置，确认哪种真正触发 hy3 的
// reasoning_content，从而判断"主 agent 的思考是否实际生效"以及"是否支持调级"。
//
// 运行：TENCENT_API_KEY=xxx go test -v -run TestHy3ThinkingMode_RealAPI ./tests/ -timeout 200s
func TestHy3ThinkingMode_RealAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-API test in short mode")
	}
	apiKey := os.Getenv("TENCENT_API_KEY")
	if apiKey == "" {
		t.Skip("TENCENT_API_KEY 未设置，跳过 hy3 真实调用测试")
	}
	m := openai.New("hy3", openai.WithAPIKey(apiKey), openai.WithBaseURL(tencentEndpoint))
	prompt := "9.11 和 9.9 这两个数，哪个更大？请一步步推理后给出结论。"
	boolPtr := func(b bool) *bool { return &b }
	intPtr := func(i int) *int { return &i }
	strPtr := func(s string) *string { return &s }
	maxTok := 4000

	cases := []struct {
		name string
		gen  model.GenerationConfig
	}{
		{"thinking_enabled_only", model.GenerationConfig{MaxTokens: &maxTok, ThinkingEnabled: boolPtr(true)}},
		{"reasoning_effort_high", model.GenerationConfig{MaxTokens: &maxTok, ThinkingEnabled: boolPtr(true), ReasoningEffort: strPtr("high")}},
		{"thinking_tokens_2048", model.GenerationConfig{MaxTokens: &maxTok, ThinkingEnabled: boolPtr(true), ThinkingTokens: intPtr(2048)}},
		{"effort_high+tokens", model.GenerationConfig{MaxTokens: &maxTok, ThinkingEnabled: boolPtr(true), ReasoningEffort: strPtr("high"), ThinkingTokens: intPtr(2048)}},
	}

	reasonedBy := map[string]int{}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			r := callHy3(ctx, m, tc.gen, prompt)
			if r.err != nil {
				t.Logf("⚠️  [%s] 调用报错: %v", tc.name, r.err)
				return
			}
			reasonedBy[tc.name] = len(r.reasoning)
			t.Logf("[%s] reasoning_len=%d content_len=%d", tc.name, len(r.reasoning), len(r.content))
			if strings.TrimSpace(r.reasoning) != "" {
				t.Logf("  ✅ 触发 hy3 思考 (reasoning 摘要: %s)", truncateHy3(r.reasoning, 120))
			} else {
				t.Logf("  ⚠️ 未触发 reasoning_content")
			}
			if strings.TrimSpace(r.content) == "" {
				t.Errorf("❌ [%s] 最终回复 content 为空", tc.name)
			}
		})
	}

	// 结论断言：hy3 必须至少有一种配置能触发思考，否则说明思考模式完全不可用。
	anyReasoned := false
	for _, n := range reasonedBy {
		if n > 0 {
			anyReasoned = true
		}
	}
	t.Logf("========== hy3 思考触发矩阵 ==========")
	for _, tc := range cases {
		t.Logf("  %-22s reasoning_len=%d", tc.name, reasonedBy[tc.name])
	}
	if !anyReasoned {
		t.Errorf("❌ 所有配置均未触发 hy3 reasoning_content —— 思考模式无法开启")
	} else {
		t.Logf("✅ hy3 支持思考模式；请对照矩阵确认 thinking_enabled 单独是否足够")
	}
}

func truncateHy3(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
