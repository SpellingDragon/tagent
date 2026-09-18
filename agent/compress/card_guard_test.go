package compress

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// fixedCondenseModel returns a canned summary-model answer for
// condenseCardLines and counts calls (the guard must not re-ask the model).
type fixedCondenseModel struct {
	mu    sync.Mutex
	calls int
	out   string
}

func (m *fixedCondenseModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: m.out,
	}}}}
	close(ch)
	return ch, nil
}

func (m *fixedCondenseModel) Info() model.Info { return model.Info{Name: "fixed-condense"} }

func (m *fixedCondenseModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// cardLine builds a canonical extractCardLine-shaped card line with a hex
// recall ticket; star adds the meditation highlight prefix.
func cardLine(ts, key, text string, star bool) string {
	s := "- "
	if star {
		s += "★ "
	}
	return fmt.Sprintf("%s%s [%s] %s", s, ts, key, text)
}

// guardFixture returns an over-cap card sequence whose OLD HALF (first 4 of
// 8 lines) carries tickets t0..t3 with t0 as head, t3 as tail and t2
// highlighted, plus fresh newest lines that must stay verbatim.
func guardFixture(condensed string) (*ContextCompressor, *fixedCondenseModel, []string) {
	sm := &fixedCondenseModel{out: condensed}
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000), WithSummaryModel(sm))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(250))
	cards := []string{
		cardLine("07-21 10:00", "aaaa0001", "任务一完成了一些工作", false),
		cardLine("07-21 11:00", "aaaa0002", "任务二完成了一些工作", false),
		cardLine("07-21 12:00", "aaaa0003", "冥想反思结论", true),
		cardLine("07-21 13:00", "aaaa0004", "任务四完成了一些工作", false),
		cardLine("07-22 10:00", "bbbb0005", "新任务五", false),
		cardLine("07-22 11:00", "bbbb0006", "新任务六", false),
		cardLine("07-22 12:00", "bbbb0007", "新任务七", false),
		cardLine("07-22 13:00", "bbbb0008", "新任务八", false),
	}
	return cc, sm, cards
}

// --- 2.1 counterexamples: the pre-guard path accepted ALL of these ----------

func TestCurateCards_TicketGuard_ValidCondensationAdopted(t *testing.T) {
	// Head/tail/★ tickets preserved, subset of input → adopt.
	condensed := cardAll("", []string{"aaaa0001", "aaaa0003", "aaaa0004"}, "浓缩旧任务一至四")
	cc, sm, cards := guardFixture(condensed)
	out, earlier := cc.curateCards(context.Background(), cards, 0)
	if sm.count() != 1 {
		t.Fatalf("exactly one condensation call expected, got %d", sm.count())
	}
	if earlier != 0 {
		t.Errorf("valid condensation must not sink anything, earlier=%d", earlier)
	}
	joined := strings.Join(out, "\n")
	if len(joined) > 250 {
		t.Errorf("accepted result must fit the cap, got %d chars", len(joined))
	}
	if !strings.Contains(out[0], "浓缩旧任务一至四") {
		t.Errorf("first line must be the condensed card: %q", out[0])
	}
	for _, k := range []string{"aaaa0001", "aaaa0003", "aaaa0004"} {
		if !strings.Contains(out[0], "["+k+"]") {
			t.Errorf("required ticket [%s] must survive in the condensed line: %q", k, out[0])
		}
	}
}

func TestCurateCards_TicketGuard_RejectsTicketLossAndFabrication(t *testing.T) {
	cases := []struct {
		name      string
		condensed string
	}{
		{"non_empty_without_ticket", "旧任务一到四的浓缩概述（没有任何票据）"},
		{"unknown_ticket", cardAll("", []string{"aaaa0001", "aaaa0004", "ffffffff"}, "伪造")},
		{"dropped_head", cardAll("", []string{"aaaa0002", "aaaa0003"}, "dropped first")},
		{"dropped_tail", cardAll("", []string{"aaaa0001", "aaaa0002", "aaaa0003"}, "dropped last")},
		{"dropped_star", cardAll("", []string{"aaaa0001", "aaaa0004"}, "dropped the highlighted ticket")},
		{"garbled_tickets", cardAll("", []string{"aaaa0001", "AAAA0004"}, "大小写未知票")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cc, sm, cards := guardFixture(tc.condensed)
			out, earlier := cc.curateCards(context.Background(), cards, 0)
			if sm.count() != 1 {
				t.Fatalf("guard must not ask the model again (calls=%d)", sm.count())
			}
			// Rejection = deterministic sinking of the ORIGINAL lines: every
			// surviving line must be verbatim from the input (model text never
			// enters the payload), and the dropped ones are counted.
			if earlier < 1 {
				t.Errorf("rejection must fall through to deterministic sinking (earlier=%d, out=%q)", earlier, out)
			}
			for _, line := range out {
				if !containsLine(cards, line) {
					t.Errorf("sunk fallback must keep original card lines verbatim, got %q", line)
				}
			}
		})
	}
}

// cardAll builds "- <star?>[ts] [k1] [k2] ... text" freely.
func cardAll(star string, tickets []string, text string) string {
	var b strings.Builder
	if star != "" {
		b.WriteString(star + " ")
	}
	for _, k := range tickets {
		b.WriteString("[" + k + "] ")
	}
	b.WriteString(text)
	return b.String()
}

func containsLine(lines []string, line string) bool {
	for _, l := range lines {
		if l == line {
			return true
		}
	}
	return false
}

// --- 6.3 single over-cap card & budget-unrepresentable -----------------------

func TestCurateCards_SingleOverCapCardKeepsEveryTicket(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(50))
	cards := []string{"- ★ 07-21 10:00 [aaaa0001] 一段远超预算的冥想反思摘要文字 [bbbb0002] 尾部还有更多无法容纳的叙述"}

	out, earlier := cc.curateCards(context.Background(), cards, 0)
	if len(out) != 1 || earlier != 0 {
		t.Fatalf("a single card must be bounded in place, not dropped: out=%q earlier=%d", out, earlier)
	}
	if len(out[0]) > 50 {
		t.Errorf("fitted card must respect the cap, got %d chars: %q", len(out[0]), out[0])
	}
	for _, k := range []string{"aaaa0001", "bbbb0002"} {
		if !strings.Contains(out[0], "["+k+"]") {
			t.Errorf("ticket [%s] must survive truncation: %q", k, out[0])
		}
	}
	if !strings.Contains(out[0], "〔预算截断〕") || !strings.Contains(out[0], "★") {
		t.Errorf("truncation marker and ★ highlight must survive: %q", out[0])
	}
	if cc.BudgetUnrepresentable() != 0 {
		t.Errorf("representable truncation must not raise the counter")
	}
}

func TestCurateCards_BudgetUnrepresentableIsObservableAndStable(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(15)) // too small even for the tickets-only form
	cards := []string{"- [aaaa0001] [aaaa0002] [aaaa0003] 三条票据无法在 15 字符内表达"}

	out, _ := cc.curateCards(context.Background(), cards, 0)
	if cc.BudgetUnrepresentable() != 1 {
		t.Fatalf("budget-unrepresentable must be counted, got %d", cc.BudgetUnrepresentable())
	}
	for _, k := range []string{"aaaa0001", "aaaa0002", "aaaa0003"} {
		if !strings.Contains(out[0], "["+k+"]") {
			t.Errorf("ticket [%s] must survive even unrepresentable state (never silently dropped): %q", k, out[0])
		}
	}

	// Serialize → parse back (projection round-trip) → re-curated: the guard
	// stays stable, the tickets-only form is a fixed point, no unbounded growth.
	back, _ := parseCardSection(out[0])
	if len(back) != 1 || back[0] != out[0] {
		t.Fatalf("fitted card must parse back as one card line: %q", back)
	}
	out2, _ := cc.curateCards(context.Background(), out, 0)
	if out2[0] != out[0] {
		t.Errorf("re-curation must be a fixed point, got %q vs %q", out2[0], out[0])
	}
	if cc.BudgetUnrepresentable() != 2 {
		t.Errorf("each unrepresentable round is observed (monotone), got %d", cc.BudgetUnrepresentable())
	}
}

// --- 2.2 acceptance-side guarantees ------------------------------------------

// TestCurateCards_ModelFailureKeepsOriginals: the model-error path is the
// deterministic sinking branch — original lines verbatim, count honest, no
// retry by the guard.
func TestCurateCards_ModelFailureKeepsOriginals(t *testing.T) {
	sm := &failingCondenseModel{}
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000), WithSummaryModel(sm))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(250))
	cards := []string{
		cardLine("07-21 10:00", "aaaa0001", "任务一完成了一些工作较长描述", false),
		cardLine("07-21 11:00", "aaaa0002", "任务二完成了一些工作较长描述", false),
		cardLine("07-21 12:00", "aaaa0003", "任务三完成了一些工作较长描述", false),
		cardLine("07-21 13:00", "aaaa0004", "任务四完成了一些工作较长描述", false),
	}
	out, earlier := cc.curateCards(context.Background(), cards, 0)
	if sm.calls != 1 {
		t.Fatalf("failed call must not be retried by the guard, calls=%d", sm.calls)
	}
	if earlier == 0 {
		t.Errorf("model failure must fall through to counted sinking")
	}
	for _, line := range out {
		if !containsLine(cards, line) {
			t.Errorf("original card lines must survive verbatim, got %q", line)
		}
	}
}

type failingCondenseModel struct{ calls int }

func (m *failingCondenseModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.calls++
	return nil, fmt.Errorf("summary engine unavailable")
}

func (m *failingCondenseModel) Info() model.Info { return model.Info{Name: "failing-condense"} }

// TestBuildRetainedRefs_RejectedCondensationNeverEntersSummary is the spec
// scenario "模型生成未知票据 → 不把伪造票据写进 compaction 载荷": over-cap
// real cards go through curateCards, the model answers with a fabricated
// ticket, the guard rejects it, and the CARD SEQUENCE carries only verbatim
// original card lines. (The 〔历史综述〕 narrative line is the comprehension
// layer — deliberately NOT ticket-guarded; scripted answers keep the two LLM
// channels distinguishable.)
func TestBuildRetainedRefs_RejectedCondensationNeverEntersSummary(t *testing.T) {
	sm := &scriptedCondenseModel{outs: []string{
		"[aaaa0001] [deadbeef] 模型编造的浓缩行", // curateCards → guard rejects
		"历史综述正常成文不受影响",                   // narrative channel
	}}
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000), WithSummaryModel(sm))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(60))

	// 10 dropped boundary refs → real hex cards (keys 0xaaaa0001..0xaaaa000a).
	refs := make([]memory.EventReference, 0, 10)
	for i := 0; i < 10; i++ {
		key := int64(0xaaaa0001 + i)
		refs = append(refs, memory.EventReference{
			EventKey: key, EventType: tagentevent.TypeExternalInput,
			EventSummary: fmt.Sprintf("历史任务 %d 的完整摘要行内容", i), Timestamp: 1_000_000 + int64(i),
		})
	}
	// compressedMsgs=nil → every ref is dropped → cards form → curate fires.
	retained := cc.buildRetainedRefs(refs, nil, context.Background())
	if len(retained) == 0 || retained[0].EventType != tagentevent.TypeContextCompress {
		t.Fatalf("expected leading rolling summary ref: %+v", retained)
	}
	summary := retained[0].EventSummary

	// Ticket law applies to the card sequence: every "- " line is verbatim
	// engineering output; the rejected model text is absent from them.
	var cardLines int
	for _, line := range strings.Split(summary, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		cardLines++
		if strings.Contains(line, "模型编造的浓缩行") || strings.Contains(line, "deadbeef") {
			t.Errorf("guard-rejected model text must never enter the card lines: %q", line)
		}
	}
	if cardLines == 0 {
		t.Fatalf("summary must retain card lines: %q", summary)
	}
	// One condensation ask + one narrative ask — the guard never re-asks the
	// model for the card channel.
	if sm.count() != 2 {
		t.Errorf("curate 1 + narrative 1 LLM calls expected, got %d", sm.count())
	}
	// Every dropped ticket survives (card line) or is counted (sunk).
	if !strings.Contains(summary, "[aaaa000a]") ||
		!strings.Contains(summary, "(earlier 9 items retrievable via memory_recall)") {
		t.Errorf("newest card must survive and the 9 sunk lines be counted: %q", summary)
	}
}

// scriptedCondenseModel returns scripted answers in call order (the last one
// repeats), counting calls.
type scriptedCondenseModel struct {
	mu    sync.Mutex
	calls int
	outs  []string
}

func (m *scriptedCondenseModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.outs[m.calls%len(m.outs)]
	m.calls++
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: out,
	}}}}
	close(ch)
	return ch, nil
}

func (m *scriptedCondenseModel) Info() model.Info { return model.Info{Name: "scripted-condense"} }

func (m *scriptedCondenseModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}
