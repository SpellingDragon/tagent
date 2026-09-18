package tagent

// 3.8 真实模型契约矩阵（限定预算样本）。
//
// 目的：用真实远端 endpoint 验证 tagent 的 ReAct/常驻管线所依赖的模型协议契约是否成立，
// 而不是仅测 mock。矩阵覆盖：文本生成 / usage 记账 / 流式 / 原生 tool_calls / 工具结果回环 /
// reasoning_content 透传。
//
// 授权门：DEEPSEEK_API_KEY 未设 → 整组 t.Skip（**明确 SKIP，绝不记 PASS**，符合 tasks 3.8 要求）。
// 预算门：全程仅 3 次真实调用（文本、强制 tool_call、工具结果回环），每次 MaxTokens≤128、prompt 极短；
// usage/流式/reasoning 三项复用这 3 次调用的观测，不额外计费。
//
// 判定纪律：核心契约（非空文本、usage>0、tool_calls 且参数为合法 JSON）用 require 硬断言——
// 若真实模型不满足即 FAIL，作为真实发现上报，不粉饰。模型特定能力（是否分块流式、是否返回
// reasoning）按观测记录：不具备只记 SKIP/note，不冒充 PASS，也不误判 FAIL。

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declTool 是仅用于契约矩阵声明的最小 tool.Tool（只满足 Declaration()）。
type declTool struct{ d trpctool.Declaration }

func (t declTool) Declaration() *trpctool.Declaration { return &t.d }

// contractObservation 汇聚一次 GenerateContent 流式调用的全部可观测事实。
type contractObservation struct {
	content      string
	reasoning    string
	chunks       int
	usageSeen    bool
	promptTokens int
	complTokens  int
	toolCalls    []model.ToolCall
	finishReason string
	apiErr       string // resp.Error（API 级，非函数级）——诊断 tool_choice 被拒等
}

// chatOnce 用真实模型跑一次请求并收集观测。调用方负责预算控制（MaxTokens/prompt 长度）。
func chatOnce(t *testing.T, m model.Model, req *model.Request) (*contractObservation, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ch, err := m.GenerateContent(ctx, req)
	if err != nil {
		return nil, err
	}
	obs := &contractObservation{}
	for resp := range ch {
		if resp == nil {
			continue
		}
		obs.chunks++
		if resp.Error != nil {
			obs.apiErr = resp.Error.Message
			if obs.apiErr == "" {
				obs.apiErr = "<non-nil resp.Error>"
			}
		}
		if resp.Usage != nil && (resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0) {
			obs.usageSeen = true
			if resp.Usage.PromptTokens > obs.promptTokens {
				obs.promptTokens = resp.Usage.PromptTokens
			}
			if resp.Usage.CompletionTokens > obs.complTokens {
				obs.complTokens = resp.Usage.CompletionTokens
			}
		}
		if len(resp.Choices) == 0 {
			continue
		}
		c := resp.Choices[0]
		if c.FinishReason != nil && *c.FinishReason != "" {
			obs.finishReason = *c.FinishReason
		}
		// 流式下增量在 Delta，非流式/终块在 Message；两处都取以免漏。
		obs.content += c.Delta.Content
		if c.Message.Content != "" && !resp.IsPartial {
			// 某些适配器把完整文本放在终块 Message 而非 Delta。
			if c.Message.Content != obs.content && len(c.Message.Content) > len(obs.content) {
				obs.content = c.Message.Content
			}
		}
		if r := c.Delta.ReasoningContent; r != "" {
			obs.reasoning += r
		}
		if c.Message.ReasoningContent != "" {
			obs.reasoning += c.Message.ReasoningContent
		}
		for _, tc := range append(append([]model.ToolCall{}, c.Delta.ToolCalls...), c.Message.ToolCalls...) {
			if tc.Function.Name != "" || len(tc.Function.Arguments) > 0 {
				obs.toolCalls = append(obs.toolCalls, tc)
			}
		}
	}
	return obs, nil
}

// newDeepSeekModel 按已授权配置构造真实模型；未授权返回 nil + skip。
func newDeepSeekModel(t *testing.T) model.Model {
	t.Helper()
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("DEEPSEEK_API_KEY 未设置：3.8 真实模型契约矩阵未授权 → SKIP（不记 PASS）")
	}
	modelID := os.Getenv("DEEPSEEK_MODEL")
	if modelID == "" {
		modelID = "deepseek-flash"
	}
	cfg := Config{
		Provider: "deepseek",
		Providers: map[string]ProviderConfig{
			"deepseek": {
				Provider:    "openai", // DeepSeek 走 OpenAI 兼容协议
				APIEndpoint: "https://api.deepseek.com/v1",
				APIKeyEnv:   "DEEPSEEK_API_KEY",
			},
		},
		Agents: map[string]AgentConfig{
			"matrix": {Provider: "deepseek", Model: modelID},
		},
	}
	cfg.ApplyDefaults()
	rc := &runtimeConfig{}
	m := rc.resolveAgentModel("matrix", cfg.Agents["matrix"], cfg)
	require.NotNil(t, m, "model 应被解析（配置正确但未授权时会先 skip）")
	t.Logf("契约矩阵目标模型: %s @ %s", modelID, "https://api.deepseek.com/v1")
	return m
}

func TestModelContractMatrix_DeepSeek(t *testing.T) {
	m := newDeepSeekModel(t)
	mt := 128

	// ── 调用 1：文本生成 + usage 记账 + 流式 + reasoning（观测复用）────────────
	obs1, err := chatOnce(t, m, &model.Request{
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "你是简洁的助手，用一句话回答。"},
			{Role: model.RoleUser, Content: "1+1 等于几？只回答数字。"},
		},
		GenerationConfig: model.GenerationConfig{MaxTokens: &mt, Temperature: ptrF(0), Stream: true},
	})
	require.NoError(t, err, "GenerateContent 函数级错误（连接/鉴权）")

	t.Run("text_generation", func(t *testing.T) {
		require.NotEmpty(t, obs1.content, "非流式/终块也未取到文本内容")
		t.Logf("文本响应=%q finish=%q chunks=%d", obs1.content, obs1.finishReason, obs1.chunks)
	})

	t.Run("usage_accounting", func(t *testing.T) {
		require.True(t, obs1.usageSeen, "响应未携带非零 usage（无法驱动压缩阈值）")
		assert.Greater(t, obs1.promptTokens, 0, "prompt_tokens 应为正")
		assert.Greater(t, obs1.complTokens, 0, "completion_tokens 应为正")
		t.Logf("usage: prompt=%d completion=%d", obs1.promptTokens, obs1.complTokens)
	})

	t.Run("streaming", func(t *testing.T) {
		require.GreaterOrEqual(t, obs1.chunks, 1, "GenerateContent 应至少产出一个响应块且正常关闭通道")
		if obs1.chunks <= 1 {
			t.Skipf("endpoint 本次仅返回单块（未分块流式），流式增量契约不据此判 FAIL：chunks=%d", obs1.chunks)
		}
		t.Logf("流式增量确认：chunks=%d", obs1.chunks)
	})

	t.Run("reasoning_content_passthrough", func(t *testing.T) {
		if obs1.reasoning == "" {
			t.Skip("该模型/端点本次未返回 reasoning_content：非推理能力缺失即 SKIP，不记 PASS/FAIL")
		}
		t.Logf("reasoning 透传确认：len=%d（且未破坏 content 解析）", len(obs1.reasoning))
	})

	// ── 调用 2：原生 tool_calls（强制 tool_choice=required 使判定确定化）────────
	weatherTool := declTool{d: trpctool.Declaration{
		Name:        "get_weather",
		Description: "查询指定城市当前天气",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"city": {Type: "string", Description: "城市名"},
			},
			Required: []string{"city"},
		},
	}}
	obs2, err := chatOnce(t, m, &model.Request{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "北京现在天气怎么样？"},
		},
		GenerationConfig: model.GenerationConfig{MaxTokens: &mt, Temperature: ptrF(0)},
		Tools:            map[string]trpctool.Tool{"get_weather": weatherTool},
		// deepseek-flash 默认 thinking 模式**不支持强制 tool_choice**（实测 400：
		// "Thinking mode does not support this tool_choice"）。故本例显式关思考后指名工具，
		// 以确定化验证「序列化 tools + 解析 tool_calls」这条 ReAct 硬契约；仍不支持则 apiErr→SKIP。
		ExtraFields: map[string]any{
			"thinking":    map[string]any{"type": "disabled"},
			"tool_choice": "required",
		},
	})
	if err != nil {
		// 鉴权/连接之外的 400（如不支持 tool_choice）也走此路——诚实记为契约发现。
		t.Fatalf("tool_calls 请求函数级错误（真实契约发现）: %v", err)
	}

	var called model.ToolCall
	for _, tc := range obs2.toolCalls {
		if tc.Function.Name != "" {
			called = tc
			break
		}
	}
	t.Run("native_tool_calls", func(t *testing.T) {
		t.Logf("诊断: chunks=%d finish=%q apiErr=%q content=%q raw_toolcalls=%d",
			obs2.chunks, obs2.finishReason, obs2.apiErr, obs2.content, len(obs2.toolCalls))
		if obs2.apiErr != "" {
			t.Skipf("端点对指名 tool_choice 返回 API 级错误（能力/参数差异，非本测试可判定为缺陷）: %s", obs2.apiErr)
		}
		require.NotEmpty(t, obs2.toolCalls, "强制指名工具仍无 tool_calls：ReAct 无法驱动，属真实契约缺陷")
		require.NotEmpty(t, called.Function.Name, "tool_call 缺少 function.name")
		assert.Equal(t, "get_weather", called.Function.Name)
		require.True(t, json.Valid(called.Function.Arguments),
			"tool_call 参数必须是合法 JSON（否则框架无法反序列化执行），实得=%q", string(called.Function.Arguments))
		t.Logf("tool_call: name=%s args=%s", called.Function.Name, string(called.Function.Arguments))
	})

	// ── 调用 3：工具结果回环（多轮）——把 assistant 的 tool_call + tool 结果送回，应得续答 ──
	if called.Function.Name == "" {
		t.Skip("无可用 tool_call，跳过工具结果回环（依赖调用 2 的产物，不凭空构造）")
	}
	roundTrip := []model.Message{
		{Role: model.RoleUser, Content: "北京现在天气怎么样？"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{called}},
		{Role: model.RoleTool, ToolID: called.ID, ToolName: called.Function.Name, Content: `{"city":"北京","temp":"21C","sky":"晴"}`},
	}
	obs3, err := chatOnce(t, m, &model.Request{
		Messages:         roundTrip,
		GenerationConfig: model.GenerationConfig{MaxTokens: &mt, Temperature: ptrF(0)},
		Tools:            map[string]trpctool.Tool{"get_weather": weatherTool},
	})
	require.NoError(t, err, "工具结果回环请求函数级错误")
	t.Run("tool_result_roundtrip", func(t *testing.T) {
		require.NotEmpty(t, obs3.content, "送回工具结果后未得续答内容：多轮 tool 契约不成立")
		t.Logf("回环续答=%q", obs3.content)
	})
}

func ptrF(v float64) *float64 { return &v }
