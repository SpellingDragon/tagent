// 本文件钉住 O3.3（request-budget-accounting「完整预算输入与诚实精度」/「单一压缩及安全消费」）：
// 唯一压缩入口 Compress 必须把请求的固定可估开销（system 装配文本、冻结工具声明、动态通知/任务板、
// 协议余量）计入触发线；传入的「可压缩内容预算」必须显式区分零与缺省（零不得回退成旧的 maxTokens 大值）；
// 纯固定开销已超输入上限时必须给出具名 budget_exceeded 结果，而不是静默删 system 硬过门；
// 无法计量的媒体只登记 unknown，绝不臆造成本、也绝不宣称精确。
// 契约: docs/wiki/agent/compression-and-telemetry.md
package compress

import (
	"context"
	"strings"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	memory "github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/modelutil"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// rbRefs builds n complete turns (external_input + agent_output) of a fixed
// per-message size, so the history side of the trigger arithmetic is known.
func rbRefs(n int) []memory.EventReference {
	refs := make([]memory.EventReference, 0, n*2)
	body := strings.Repeat("h", 80)
	for i := 0; i < n; i++ {
		k := int64(i*2 + 1)
		refs = append(refs,
			memory.EventReference{EventKey: k, EventType: tagentevent.TypeExternalInput,
				EventSummary: "请求 " + body, Timestamp: 1_700_000_000_000 + k},
			memory.EventReference{EventKey: k + 1, EventType: tagentevent.TypeAgentOutput,
				EventSummary: "答复 " + body, Timestamp: 1_700_000_000_000 + k + 1},
		)
	}
	return refs
}

// rbCompressor is the fixed fixture: maxTokens=1000, threshold=0.8 → trigger
// line 800, keepRecent=1.
func rbCompressor() *ContextCompressor {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	return NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(),
		1000, 0.8, 1, WithCardMaxChars(6000), WithCompactKeysListed(8))
}

// rbBigTool is a frozen declaration whose description alone costs ~650 tokens.
func rbBigTool() []modelutil.ToolDeclarationSnapshot {
	return []modelutil.ToolDeclarationSnapshot{{
		RegistryKey: "big_tool",
		Name:        "big_tool",
		Description: strings.Repeat("d", 1300),
		InputSchema: &tool.Schema{Type: "object"},
	}}
}

func TestContextCompressor_FixedOverhead(t *testing.T) {
	ctx := context.Background()
	refs := rbRefs(4)

	t.Run("absent_context_is_the_legacy_path", func(t *testing.T) {
		cc := rbCompressor()
		res := cc.Compress(ctx, refs)
		if res.Compressed {
			t.Fatal("baseline: under-budget history must not fold")
		}
		if res.FixedOverhead != 0 || res.Budget != nil {
			t.Fatalf("no budget context must report zero fixed overhead, got %d / %+v", res.FixedOverhead, res.Budget)
		}
		if res.ContentBudget != cc.BudgetLine() {
			t.Fatalf("缺省 content budget must stay the untouched trigger line: got %d want %d",
				res.ContentBudget, cc.BudgetLine())
		}
		if res.BudgetExceeded || res.BudgetReason != "" {
			t.Fatalf("no context must never refuse: %+v %q", res.BudgetExceeded, res.BudgetReason)
		}
	})

	t.Run("tool_declarations_fold_into_the_trigger", func(t *testing.T) {
		cc := rbCompressor()
		plain := cc.Compress(ctx, refs)
		hist := NewDefaultTokenCounter().Estimate(plain.Messages)
		if hist > cc.BudgetLine() {
			t.Fatalf("fixture must sit under the line on its own, got %d", hist)
		}

		res := cc.Compress(ctx, refs, RequestBudgetContext{Tools: rbBigTool()})
		b := rbBudget(t, res)
		if want := 654; b.ToolDeclarations < want {
			t.Fatalf("long description must be counted in full: got %d, want >= %d",
				b.ToolDeclarations, want)
		}
		if res.FixedOverhead != b.Total {
			t.Fatalf("FixedOverhead must be the quantified total: %d vs %d", res.FixedOverhead, b.Total)
		}
		if hist+res.FixedOverhead <= cc.BudgetLine() {
			t.Fatalf("fixture arithmetic broken: %d + %d must exceed %d", hist, res.FixedOverhead, cc.BudgetLine())
		}
		if !res.Compressed {
			t.Fatal("a short body with a big schema must trip the trigger once the known overhead is counted")
		}
		if want := cc.BudgetLine() - res.FixedOverhead; res.ContentBudget != want {
			t.Fatalf("inner target must be the trigger line minus the fixed part: got %d want %d",
				res.ContentBudget, want)
		}
	})

	t.Run("system_and_notices_are_counted", func(t *testing.T) {
		cc := rbCompressor()
		res := cc.Compress(ctx, refs, RequestBudgetContext{
			SystemText:  strings.Repeat("s", 400),
			NoticesText: strings.Repeat("n", 200),
		})
		b := rbBudget(t, res)
		if b.System <= 0 || b.Notices <= 0 {
			t.Fatalf("assembled system prompt and live board text must be priced: %+v", *b)
		}
		if res.FixedOverhead <= 300 {
			t.Fatalf("600 chars of fixed text must cost ≥300 tokens, got %d", res.FixedOverhead)
		}
	})

	t.Run("explicit_zero_budget_never_falls_back_to_the_old_large_value", func(t *testing.T) {
		cc := rbCompressor()
		plain := cc.Compress(ctx, refs)
		baseline := len(plain.Messages)

		res := cc.Compress(ctx, refs, RequestBudgetContext{NoticesText: strings.Repeat("q", 1600)})
		if res.BudgetExceeded {
			t.Fatalf("fixed == trigger line is still inside the input limit: %+v", res.BudgetReason)
		}
		if res.ContentBudget != 0 {
			t.Fatalf("zero content budget must survive, got %d", res.ContentBudget)
		}
		if !res.Compressed {
			t.Fatal("zero content budget must still fold (the compressor owns the history)")
		}
		if len(res.Messages) >= baseline {
			t.Fatalf("a zero budget treated as 缺省 would fall back to maxTokens and trim nothing: %d messages kept",
				len(res.Messages))
		}
		if !hasSummaryRef(res.RetainedRefs) {
			t.Fatal("the fold must still emit the rolling summary ref (票据层不丢)")
		}
	})

	t.Run("negative_overshoot_clamps_to_zero_not_below", func(t *testing.T) {
		cc := rbCompressor()
		res := cc.Compress(ctx, refs, RequestBudgetContext{NoticesText: strings.Repeat("q", 1900)})
		if res.FixedOverhead != 950 {
			t.Fatalf("fixed = 1900 runes / 2.0 = 950 tokens, got %d", res.FixedOverhead)
		}
		if res.BudgetExceeded {
			t.Fatalf("950 < 1000 input limit must not refuse, got %q", res.BudgetReason)
		}
		if res.ContentBudget != 0 {
			t.Fatalf("800-950 must clamp to 0, got %d", res.ContentBudget)
		}
	})

	t.Run("fixed_over_input_limit_is_a_named_refusal", func(t *testing.T) {
		cc := rbCompressor()
		res := cc.Compress(ctx, refs, RequestBudgetContext{NoticesText: strings.Repeat("q", 2000)})
		if !res.BudgetExceeded {
			t.Fatal("fixed part alone exceeds the input limit; a silent pass-through is the failure this closes")
		}
		if res.BudgetReason != BudgetExceededReason {
			t.Fatalf("refusal must be named %q, got %q", BudgetExceededReason, res.BudgetReason)
		}
		if res.ContentBudget < 0 {
			t.Fatalf("content budget must never go negative: %d", res.ContentBudget)
		}
		if res.Compressed {
			t.Fatal("budget_exceeded must not fold history — trimming history cannot pay for a fixed-cost overrun")
		}
		if len(res.Messages) != len(refs) {
			t.Fatalf("history must stay intact for the caller to refuse honestly, got %d of %d",
				len(res.Messages), len(refs))
		}
		if len(res.RetainedRefs) != len(refs) {
			t.Fatalf("projection must not be rewritten on refusal, got %d want %d",
				len(res.RetainedRefs), len(refs))
		}
	})

	t.Run("empty_history_with_huge_schema_still_refuses", func(t *testing.T) {
		cc := rbCompressor()
		res := cc.Compress(ctx, nil, RequestBudgetContext{
			SystemText: strings.Repeat("s", 2000),
			Tools:      rbBigTool(),
		})
		if !res.BudgetExceeded || res.BudgetReason != BudgetExceededReason {
			t.Fatalf("a short body with a huge schema is exactly the 拒发 case, got %+v %q",
				res.BudgetExceeded, res.BudgetReason)
		}
	})

	t.Run("unknown_media_is_reported_not_priced", func(t *testing.T) {
		cc := rbCompressor()
		plain := cc.Compress(ctx, refs, RequestBudgetContext{SystemText: strings.Repeat("s", 100)})
		withUnknown := cc.Compress(ctx, refs, RequestBudgetContext{
			SystemText:   strings.Repeat("s", 100),
			ExtraUnknown: []string{"image part without sizing metadata"},
		})
		if withUnknown.FixedOverhead != plain.FixedOverhead {
			t.Fatalf("an unknown must never invent cost: %d vs %d", withUnknown.FixedOverhead, plain.FixedOverhead)
		}
		ub := rbBudget(t, withUnknown)
		if len(ub.Unknown) != 1 {
			t.Fatalf("coverage gap must be visible, got %+v", ub.Unknown)
		}
		if len(rbBudget(t, plain).Unknown) != 0 {
			t.Fatalf("no gap declared means no unknown, got %+v", rbBudget(t, plain).Unknown)
		}
		if withUnknown.BudgetExceeded {
			t.Fatal("unknown media alone must not refuse the round (既有可用性保持)")
		}
	})

	t.Run("estimate_stays_an_estimate", func(t *testing.T) {
		cc := rbCompressor()
		res := cc.Compress(ctx, refs, RequestBudgetContext{Tools: rbBigTool()})
		if got := rbBudget(t, res).Notices; got != 0 {
			t.Fatalf("no notices text must cost nothing, got %d", got)
		}
		if rbBudget(t, res).ProtocolOverhead <= 0 {
			t.Fatal("the request envelope is a documented floor, not zero")
		}
	})
}

// TestCompressOptions_ContentBudgetIsExplicit closes the zero-vs-absent distinction at the inner target line.
//   - an explicit zero content budget is authoritative; a negative one clamps to zero
//   - an absent one keeps the configured trigger/window pair
//   - an explicit positive value travels verbatim
func TestCompressOptions_ContentBudgetIsExplicit(t *testing.T) {
	sc := NewSmartCompressor(WithMaxTokens(1000), WithTriggerBudget(800))
	zero := 0
	neg := -5
	if got := (CompressOptions{MaxTokens: 1000, TriggerBudget: 800, ContentBudget: &zero}).budget(sc); got != 0 {
		t.Fatalf("explicit zero must win over the old large value, got %d", got)
	}
	if got := (CompressOptions{MaxTokens: 1000, TriggerBudget: 800, ContentBudget: &neg}).budget(sc); got != 0 {
		t.Fatalf("a negative overshoot must clamp to 0, got %d", got)
	}
	if got := (CompressOptions{MaxTokens: 1000, TriggerBudget: 800}).budget(sc); got != 800 {
		t.Fatalf("缺省 must keep the hot-group trigger budget, got %d", got)
	}
	custom := 333
	if got := (CompressOptions{MaxTokens: 1000, TriggerBudget: 800, ContentBudget: &custom}).budget(sc); got != 333 {
		t.Fatalf("an explicit content budget must travel verbatim, got %d", got)
	}
}

// TestCompressSkeleton_HonoursZeroContentBudget drives the pipeline with a zero compressible-history target.
//   - with a zero target the oldest segments must fold
//   - the same input under the configured target folds nothing
func TestCompressSkeleton_HonoursZeroContentBudget(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	msgs := buildTurns(4)
	zero := 0
	got := sc.CompressWithOptions(context.Background(), msgs, CompressOptions{
		KeepRecentTasks: 1, MaxTokens: 400, TriggerBudget: 300, ContentBudget: &zero,
	})
	if len(got) >= len(msgs) {
		t.Fatalf("zero budget must trim, kept %d of %d", len(got), len(msgs))
	}
	def := sc.CompressWithOptions(context.Background(), msgs, CompressOptions{
		KeepRecentTasks: 1, MaxTokens: 8000, TriggerBudget: 8000,
	})
	if len(def) != len(msgs) {
		t.Fatalf("under-budget input must be untouched, got %d of %d", len(def), len(msgs))
	}
}

// TestTokenCounter_SharedConstants pins that the compressor's counter and the request budget read the same numbers.
func TestTokenCounter_SharedConstants(t *testing.T) {
	c := NewDefaultTokenCounter()
	if c.CharsPerToken != float64(modelutil.CharsPerToken) {
		t.Fatalf("chars-per-token must come from modelutil: %v vs %v", c.CharsPerToken, modelutil.CharsPerToken)
	}
	msgs := []model.Message{{Role: model.RoleUser, Content: strings.Repeat("a", 40)}}
	want := int(float64(40)/modelutil.CharsPerToken) + modelutil.PerMessageOverheadTokens
	if got := c.Estimate(msgs); got != want {
		t.Fatalf("per-message overhead must come from modelutil: got %d want %d", got, want)
	}
	withCall := []model.Message{{Role: model.RoleAssistant, Content: "ab",
		ToolCalls: []model.ToolCall{{ID: "t1", Function: model.FunctionDefinitionParam{Arguments: nil}}}}}
	wantCall := int(float64(2)/modelutil.CharsPerToken) +
		modelutil.PerMessageOverheadTokens + modelutil.PerToolCallOverheadTokens
	if got := c.Estimate(withCall); got != wantCall {
		t.Fatalf("per-tool-call overhead must come from modelutil: got %d want %d", got, wantCall)
	}
}

// rbBudget demands the reported breakdown before any assertion reads it, so a
// missing budget surface fails with a message instead of a nil dereference.
func rbBudget(t *testing.T, res CompressResult) *modelutil.RequestBudget {
	t.Helper()
	if res.Budget == nil {
		t.Fatal("a supplied RequestBudgetContext must travel back as a breakdown (CompressResult.Budget)")
	}
	return res.Budget
}

// hasSummaryRef reports a rolling summary ref inside a retained list.
func hasSummaryRef(refs []memory.EventReference) bool {
	for _, r := range refs {
		if r.EventKey < 0 && r.EventType == tagentevent.TypeContextCompress {
			return true
		}
	}
	return false
}
