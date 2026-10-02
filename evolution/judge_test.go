package evolution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
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

// TestLLMJudge_PassOnGoodScore TestLLMJudge*/TestParseJudgeVerdict/TestExtractJSON 系列覆盖 LLM 评审的打分通过线、四类保守
//
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
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
	eInsufficient := NewLLMJudgeEvaluator(judgeMockModel{text: `{"score": 0.1}`}, mockEvidenceSource{ev: Evidence{TurnCount: 2}}, 5, 0.5, 0)
	if res, _ := eInsufficient.Evaluate(context.Background(), "b1"); !res.Pass {
		t.Fatal("样本不足应保守通过")
	}
	eErr := NewLLMJudgeEvaluator(judgeMockModel{err: fmt.Errorf("llm down")}, mockEvidenceSource{ev: Evidence{TurnCount: 10}}, 5, 0.5, 0)
	res, err := eErr.Evaluate(context.Background(), "b1")
	if err != nil {
		t.Fatalf("judge 错误应内部消化不抛 err, got %v", err)
	}
	if !res.Pass {
		t.Fatal("judge 调用失败应保守通过")
	}
	eNil := NewLLMJudgeEvaluator(nil, mockEvidenceSource{ev: Evidence{TurnCount: 10}}, 5, 0.5, 0)
	if res, _ := eNil.Evaluate(context.Background(), "b1"); !res.Pass {
		t.Fatal("nil judge 应保守通过")
	}
	eCollect := NewLLMJudgeEvaluator(judgeMockModel{text: `{"score":0.1}`}, mockEvidenceSource{err: fmt.Errorf("store down")}, 5, 0.5, 0)
	if res, _ := eCollect.Evaluate(context.Background(), "b1"); !res.Pass {
		t.Fatal("证据收集失败应保守通过")
	}
}

func TestParseJudgeVerdict(t *testing.T) {
	if v := parseJudgeVerdict("```json\n{\"score\": 0.3, \"reason\": \"劣化\"}\n```"); v.Score != 0.3 {
		t.Fatalf("应解析 fence JSON, got %+v", v)
	}
	if v := parseJudgeVerdict("评审结果：{\"score\": 0.8, \"reason\": \"ok\"} 以上"); v.Score != 0.8 {
		t.Fatalf("应提取嵌入 JSON, got %+v", v)
	}
	if v := parseJudgeVerdict("我无法判断"); v.Score != 1.0 {
		t.Fatalf("解析失败应保守 score=1.0, got %+v", v)
	}
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

// TestParseJudgeVerdict_MissingScoreConservative pins the conservative verdict on an unusable score.
// - Valid JSON with a missing, null or case-mismatched score passes with score=1.0.
// - A zero value must not be read as a regression, since that would trigger a rollback no model verdict justified.
func TestParseJudgeVerdict_MissingScoreConservative(t *testing.T) {
	if v := parseJudgeVerdict(`{"reason":"证据不足"}`); v.Score != 1.0 {
		t.Fatalf("缺 score 应保守 1.0（不误回滚）, got %f", v.Score)
	}
	if v := parseJudgeVerdict(`{"score":null,"reason":"x"}`); v.Score != 1.0 {
		t.Fatalf("score=null 应保守 1.0, got %f", v.Score)
	}
	if v := parseJudgeVerdict(`{"Score":0.9}`); v.Score != 0.9 {
		t.Fatalf("Go json 大小写不敏感，Score 应解析为 0.9, got %f", v.Score)
	}
}

type constJudge struct {
	res EvalResult
	err error
}

func (j constJudge) Evaluate(context.Context, string) (EvalResult, error) { return j.res, j.err }

func newEvaluatedJudge(t *testing.T, judge Evaluator, signals func() bool) (string, *GitEvolution) {
	t.Helper()
	dir := initGitRepo(t)
	writeFile(t, dir, "resources/prompts/SOUL.md", "v2")
	g := NewGitEvolution(GitEvolutionConfig{WorkDir: dir})
	g.BindRuntime(memory.NewInMemoryStore(), memory.PartitionIDFromName("tagent"), judge, nil)
	if signals != nil {
		g.SetGovernanceSignalsAvailable(signals)
	}
	sha, _, err := g.Register([]string{"resources/prompts/SOUL.md"}, "combo")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	g.EvaluateNow(sha, 0)
	return sha, g
}

// TestSwitchCombo_EvidenceUnavailableIsInsufficient Evidence/judge unavailable must degrade to an explicit "insufficient" verdict.
//
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
func TestSwitchCombo_EvidenceUnavailableIsInsufficient(t *testing.T) {
	sha, g := newEvaluatedJudge(t, constJudge{err: errors.New("judge backend down")}, nil)
	ev := g.Evaluations()[sha]
	if ev.Verdict != "insufficient" {
		t.Fatalf("judge error must yield insufficient verdict, got %q (%+v)", ev.Verdict, ev)
	}
	if !strings.Contains(ev.Reason, "评估失败") {
		t.Fatalf("insufficient verdict must state the failure honestly: %q", ev.Reason)
	}
}

// TestSwitchCombo_GovernanceUnavailableIsExplicitNotSilent 钉住 治理信号不可用时，即便指标正常，也必须在结论理由里显式说明，不得静默略过。
func TestSwitchCombo_GovernanceUnavailableIsExplicitNotSilent(t *testing.T) {
	sha, g := newEvaluatedJudge(t,
		constJudge{res: EvalResult{Pass: true, Score: 1.0, Reason: "metrics look fine"}},
		func() bool { return false })
	ev := g.Evaluations()[sha]
	if ev.Verdict != "healthy" {
		t.Fatalf("passing judge with signals off still records healthy, got %q", ev.Verdict)
	}
	if !strings.Contains(ev.Reason, "unavailable") {
		t.Fatalf("healthy verdict with governance off must say signals were unavailable: %q", ev.Reason)
	}
}

// TestSwitchCombo_GovernanceAvailableOmitsCaveat 钉住 对照：信号可用且判定通过时给出健康结论，且不得附带"不可用"的免责说明。
func TestSwitchCombo_GovernanceAvailableOmitsCaveat(t *testing.T) {
	sha, g := newEvaluatedJudge(t,
		constJudge{res: EvalResult{Pass: true, Score: 1.0, Reason: "metrics look fine"}},
		func() bool { return true })
	ev := g.Evaluations()[sha]
	if ev.Verdict != "healthy" {
		t.Fatalf("want healthy, got %q", ev.Verdict)
	}
	if strings.Contains(ev.Reason, "unavailable") {
		t.Fatalf("available-governance healthy verdict must not claim unavailability: %q", ev.Reason)
	}
}
