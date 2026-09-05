package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ==================== LLMJudgeEvaluator（T-EVO · 指令4「模型决策是否回滚」）====================
//
// 快道后验评估的「模型决策」环：把 canary 观察窗的表现证据（Evidence）喂给 judge model，
// 让模型判断该 bundle 激活后是否劣化。与 MetricGuardrail（确定性阈值闸）互补——guardrail 快、
// 廉价、抓明确违约；LLM-judge 慢、质性、抓阈值未覆盖的整体劣化。二者构成 release.go 的双回滚触发。
//
// 保守原则（防误回滚错杀）：judge 未配置 / 证据收集失败 / 样本不足 / judge 调用失败 / 响应解析
// 失败 → 一律 Pass:true（保守通过，canary 保持）。**仅当模型明确判定劣化（score < 阈值）才
// Pass:false 触发回滚**。评估器内部消化 model 错误（不向 release 抛 err），避免 judge 暂时
// 不可用就回滚已激活的低风险变更（guardrail 仍是确定性防线）。

const judgeSystemPrompt = `你是 agent 配置版本的质量评审官。给定某版本（bundle）激活后 canary 观察窗的表现证据，判断该版本是否导致表现劣化。
只依据证据、保守判断：仅当证据明确显示劣化（如治理拒绝率显著升高、危险/critical 操作频发）才判劣化。
必须只返回严格 JSON，无其他文字：{"score": <0.0到1.0的浮点数>, "reason": "<一句话依据>"}
score 语义：1.0=表现优秀，0.5=可接受边界，0.0=严重劣化。score < 0.5 表示应回滚。`

// LLMJudgeEvaluator 用 LLM 对 canary 表现证据做质量裁决（实现 release.go 的 Evaluator）。
type LLMJudgeEvaluator struct {
	judge         model.Model
	src           EvidenceSource
	minSamples    int
	passThreshold float64
	timeout       time.Duration
}

// NewLLMJudgeEvaluator 构建 LLM 评审器。judge 为 nil 时 Evaluate 恒保守通过。
// minSamples<=0 取 5；passThreshold<=0 取 0.5；timeout<=0 取 60s。
func NewLLMJudgeEvaluator(judge model.Model, src EvidenceSource, minSamples int, passThreshold float64, timeout time.Duration) *LLMJudgeEvaluator {
	if minSamples <= 0 {
		minSamples = 5
	}
	if passThreshold <= 0 {
		passThreshold = 0.5
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &LLMJudgeEvaluator{judge: judge, src: src, minSamples: minSamples, passThreshold: passThreshold, timeout: timeout}
}

// Evaluate 实现 Evaluator：收集证据 → LLM 裁决 → EvalResult。保守：任何不确定均 Pass:true。
func (e *LLMJudgeEvaluator) Evaluate(ctx context.Context, bundleID string) (EvalResult, error) {
	if e == nil || e.judge == nil {
		return EvalResult{Pass: true, Score: 1.0, Reason: "无 judge model，保守通过"}, nil
	}
	ev, err := e.src.Collect(ctx, bundleID)
	if err != nil {
		// 证据收集失败：保守通过（不误回滚），记录原因。
		return EvalResult{Pass: true, Score: 1.0, Reason: "证据收集失败，保守通过: " + err.Error()}, nil
	}
	if !ev.Sufficient(e.minSamples) {
		return EvalResult{Pass: true, Score: 1.0,
			Reason: fmt.Sprintf("样本不足(%d<%d)，保守通过", ev.TurnCount, e.minSamples)}, nil
	}

	jctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	text, err := collectModelText(jctx, e.judge, buildJudgeRequest(ev, bundleID))
	if err != nil {
		// judge 调用失败：保守通过（judge 暂时不可用不应回滚已激活的低风险变更）。
		return EvalResult{Pass: true, Score: 1.0, Reason: "judge 调用失败，保守通过: " + err.Error()}, nil
	}
	verdict := parseJudgeVerdict(text)
	return EvalResult{
		Score:  verdict.Score,
		Pass:   verdict.Score >= e.passThreshold,
		Reason: fmt.Sprintf("LLM 评审 score=%.2f（阈值%.2f）：%s", verdict.Score, e.passThreshold, verdict.Reason),
	}, nil
}

// judgeVerdict 是 judge model 的结构化裁决。
type judgeVerdict struct {
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

func buildJudgeRequest(ev Evidence, bundleID string) *model.Request {
	userPrompt := fmt.Sprintf(
		"待评审版本 bundle: %s\ncanary 观察窗表现证据：\n- 事件量(turn_count): %d\n- 治理拒绝数: %d（拒绝率 %.2f）\n- critical 操作数: %d（率 %.2f）\n- 观察窗: %dms\n\n请判断该版本表现是否劣化，只返回 JSON。",
		bundleID, ev.TurnCount, ev.DenialCount, ev.DenialRate(), ev.CriticalCount, ev.CriticalRate(), ev.WindowMs,
	)
	return &model.Request{
		Messages: []model.Message{
			model.NewSystemMessage(judgeSystemPrompt),
			model.NewUserMessage(userPrompt),
		},
	}
}

// collectModelText 调 model 并收集生成文本（兼容非流式 Message 与流式 Delta）。
func collectModelText(ctx context.Context, m model.Model, req *model.Request) (string, error) {
	ch, err := m.GenerateContent(ctx, req)
	if err != nil {
		return "", err
	}
	var streamed strings.Builder
	var lastFull string
	for resp := range ch {
		if resp == nil {
			continue
		}
		if resp.Error != nil {
			return "", fmt.Errorf("%s", resp.Error.Message)
		}
		for _, c := range resp.Choices {
			if c.Delta.Content != "" {
				streamed.WriteString(c.Delta.Content)
			}
			if c.Message.Content != "" {
				lastFull = c.Message.Content
			}
		}
	}
	if streamed.Len() > 0 {
		return streamed.String(), nil
	}
	return lastFull, nil
}

// parseJudgeVerdict 从 LLM 文本解析 JSON 裁决。解析失败 → 保守 verdict（score=1.0 通过）。
func parseJudgeVerdict(text string) judgeVerdict {
	conservative := judgeVerdict{Score: 1.0, Reason: "裁决解析失败，保守通过"}
	jsonStr := extractJSON(text)
	if jsonStr == "" {
		return conservative
	}
	// score 用指针检测「字段缺失」——合法 JSON 但无 score（或大小写不符/null）时，零值 0 会被
	// 误判为劣化触发回滚（Major：违背「仅明确判定劣化才回滚」的保守原则）。缺失 → 保守通过。
	var raw struct {
		Score  *float64 `json:"score"`
		Reason string   `json:"reason"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil || raw.Score == nil {
		conservative.Reason = "裁决缺 score 或 JSON 非法，保守通过: " + text
		return conservative
	}
	v := judgeVerdict{Score: *raw.Score, Reason: raw.Reason}
	// score 越界防御：夹到 [0,1]。
	if v.Score < 0 {
		v.Score = 0
	}
	if v.Score > 1 {
		v.Score = 1
	}
	if v.Reason == "" {
		v.Reason = "（judge 未给理由）"
	}
	return v
}

// extractJSON 从可能含 markdown fence 或前后缀文字的响应中提取首个 JSON 对象。
func extractJSON(text string) string {
	text = strings.TrimSpace(text)
	// 去 markdown ```json ... ``` fence。
	if i := strings.Index(text, "```"); i >= 0 {
		rest := text[i+3:]
		rest = strings.TrimPrefix(rest, "json")
		if j := strings.Index(rest, "```"); j >= 0 {
			rest = rest[:j]
		}
		text = strings.TrimSpace(rest)
	}
	// 提取首个 { 到末个 } 的子串。
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return ""
}

// 编译期确认 LLMJudgeEvaluator 满足 Evaluator 接口。
var _ Evaluator = (*LLMJudgeEvaluator)(nil)
