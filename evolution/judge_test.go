package evolution

import (
	"context"
	"fmt"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// judgeMockModel 返回固定文本响应（或错误），隔离 LLM-judge 逻辑。
type judgeMockModel struct {
	text string
	err  error
}

func (m judgeMockModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{Content: m.text}}}}
	close(ch)
	return ch, nil
}

func (judgeMockModel) Info() model.Info { return model.Info{} }

func TestLLMJudge_PassOnGoodScore(t *testing.T) {
	src := mockEvidenceSource{ev: Evidence{TurnCount: 10, DenialCount: 0}}
	e := NewLLMJudgeEvaluator(judgeMockModel{text: `{"score": 0.9, "reason": "表现良好"}`}, src, 5, 0.5, 0)
	res, err := e.Evaluate(context.Background(), "b1")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !res.Pass || res.Score != 0.9 {
		t.Fatalf("score 0.9 >= 0.5 应 pass, got %+v", res)
	}
}

func TestLLMJudge_RollbackOnLowScore(t *testing.T) {
	src := mockEvidenceSource{ev: Evidence{TurnCount: 10, DenialCount: 6}}
	e := NewLLMJudgeEvaluator(judgeMockModel{text: `{"score": 0.2, "reason": "拒绝率高，劣化"}`}, src, 5, 0.5, 0)
	res, _ := e.Evaluate(context.Background(), "b1")
	if res.Pass {
		t.Fatalf("score 0.2 < 0.5 应 fail(触发回滚), got %+v", res)
	}
}

func TestLLMJudge_ConservativePaths(t *testing.T) {
	// 样本不足 → 保守通过（不调 judge，即便 judge 会判劣化）。
	eInsufficient := NewLLMJudgeEvaluator(judgeMockModel{text: `{"score": 0.1}`}, mockEvidenceSource{ev: Evidence{TurnCount: 2}}, 5, 0.5, 0)
	if res, _ := eInsufficient.Evaluate(context.Background(), "b1"); !res.Pass {
		t.Fatal("样本不足应保守通过")
	}
	// judge 调用错误 → 内部消化，保守通过（不抛 err）。
	eErr := NewLLMJudgeEvaluator(judgeMockModel{err: fmt.Errorf("llm down")}, mockEvidenceSource{ev: Evidence{TurnCount: 10}}, 5, 0.5, 0)
	res, err := eErr.Evaluate(context.Background(), "b1")
	if err != nil {
		t.Fatalf("judge 错误应内部消化不抛 err, got %v", err)
	}
	if !res.Pass {
		t.Fatal("judge 调用失败应保守通过")
	}
	// nil judge → 保守通过。
	eNil := NewLLMJudgeEvaluator(nil, mockEvidenceSource{ev: Evidence{TurnCount: 10}}, 5, 0.5, 0)
	if res, _ := eNil.Evaluate(context.Background(), "b1"); !res.Pass {
		t.Fatal("nil judge 应保守通过")
	}
	// 证据收集失败 → 保守通过。
	eCollect := NewLLMJudgeEvaluator(judgeMockModel{text: `{"score":0.1}`}, mockEvidenceSource{err: fmt.Errorf("store down")}, 5, 0.5, 0)
	if res, _ := eCollect.Evaluate(context.Background(), "b1"); !res.Pass {
		t.Fatal("证据收集失败应保守通过")
	}
}

func TestParseJudgeVerdict(t *testing.T) {
	// markdown fence 包裹。
	if v := parseJudgeVerdict("```json\n{\"score\": 0.3, \"reason\": \"劣化\"}\n```"); v.Score != 0.3 {
		t.Fatalf("应解析 fence JSON, got %+v", v)
	}
	// 前后缀文字中嵌入 JSON。
	if v := parseJudgeVerdict("评审结果：{\"score\": 0.8, \"reason\": \"ok\"} 以上"); v.Score != 0.8 {
		t.Fatalf("应提取嵌入 JSON, got %+v", v)
	}
	// 非法响应 → 保守 score=1.0。
	if v := parseJudgeVerdict("我无法判断"); v.Score != 1.0 {
		t.Fatalf("解析失败应保守 score=1.0, got %+v", v)
	}
	// score 越界夹取到 [0,1]。
	if v := parseJudgeVerdict(`{"score": 5.0}`); v.Score != 1.0 {
		t.Fatalf("score 应夹到 1.0, got %f", v.Score)
	}
	if v := parseJudgeVerdict(`{"score": -2.0}`); v.Score != 0.0 {
		t.Fatalf("score 应夹到 0.0, got %f", v.Score)
	}
}

func TestExtractJSON(t *testing.T) {
	if got := extractJSON("前缀 {\"a\":1} 后缀"); got != `{"a":1}` {
		t.Fatalf("应提取花括号子串, got %q", got)
	}
	if got := extractJSON("无 JSON"); got != "" {
		t.Fatalf("无 JSON 应返回空, got %q", got)
	}
}
