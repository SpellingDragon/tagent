package compress

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// settleRefs builds N settle-notification refs with realistic single-line
// bodies (event_bus newTaskSettledEvent form): "[task settled] <marker> <desc>
// (id=…) <status> → 结果: <result>". resultLen pads the inline result so the
// fold has a measurable reclaim (settleInlineCapChars bounds it at 600).
func settleRefs(startKey int64, n int, resultLen int) []memory.EventReference {
	refs := make([]memory.EventReference, 0, n)
	for i := 0; i < n; i++ {
		key := startKey + int64(i)
		marker := "✓"
		status := "completed"
		if i%3 == 0 {
			marker = "✗"
			status = "failed"
		}
		body := fmt.Sprintf("[task settled] %s 任务%d (id=%08x) %s → 结果: %s",
			marker, i, key, status, strings.Repeat("数", resultLen))
		refs = append(refs, memory.EventReference{
			EventKey: key, EventType: tagentevent.TypeExternalInput,
			EventSummary: body, Timestamp: 1_000_000 + int64(i),
		})
	}
	return refs
}

// --- foldSettleRuns unit tests ---------------------------------------------

func TestFoldSettleRuns_ConsecutiveRunFoldsToCard(t *testing.T) {
	cc := newFoldCC(2)
	refs := append([]memory.EventReference{
		toolRef(1, tagentevent.TypeExternalInput, "用户请求", 10),
	}, settleRefs(101, 3, 200)...)
	refs = append(refs, toolRef(200, tagentevent.TypeAgentOutput, "完成", 99))

	folded := cc.foldSettleRuns(refs)

	var cards []memory.EventReference
	var settles int
	for _, r := range folded {
		switch {
		case r.EventType == tagentevent.TypeSettleFold:
			cards = append(cards, r)
		case isSettleNoticeRef(r):
			settles++
		}
	}
	if len(cards) != 1 || settles != 0 {
		t.Fatalf("expected 1 card / 0 loose settles, got cards=%d settles=%d: %+v", len(cards), settles, folded)
	}
	card := cards[0]
	if card.EventKey >= 0 {
		t.Errorf("settle_fold ref must carry a negative synthetic key, got %d", card.EventKey)
	}
	if !strings.Contains(card.EventSummary, "memory_recall") {
		t.Errorf("card must carry the recall hint: %q", card.EventSummary)
	}
	lines := parseSettleFoldCardLines(card.EventSummary)
	if len(lines) != 3 {
		t.Fatalf("card must carry one row per settle, got %d: %q", len(lines), card.EventSummary)
	}
	for i, line := range lines {
		want := "[" + tagentevent.FormatEventKey(int64(101+i)) + "]"
		if !strings.Contains(line, want) {
			t.Errorf("row %d missing evt ticket %s: %q", i, want, line)
		}
		if !(strings.HasPrefix(line, "- ✓ ") || strings.HasPrefix(line, "- ✗ ")) {
			t.Errorf("row %d must start with '- <marker> ', got %q", i, line)
		}
	}
}

func TestFoldSettleRuns_SingleAndInterruptedNotFolded(t *testing.T) {
	cc := newFoldCC(2)
	single := settleRefs(101, 1, 50)
	interrupted := append(settleRefs(110, 1, 50),
		toolRef(120, tagentevent.TypeAgentOutput, "边界", 120))
	interrupted = append(interrupted, settleRefs(121, 1, 50)...)

	for name, refs := range map[string][]memory.EventReference{
		"single": single,
		"broken": interrupted,
	} {
		if folded := cc.foldSettleRuns(refs); hasSettleFold(folded) {
			t.Errorf("%s: runs shorter than 2 (or interrupted) must not fold: %+v", name, folded)
		}
	}
}

func TestFoldSettleRuns_Idempotent(t *testing.T) {
	cc := newFoldCC(2)
	refs := settleRefs(101, 4, 100)
	once := cc.foldSettleRuns(refs)
	twice := cc.foldSettleRuns(once)
	if len(once) != 1 || len(twice) != 1 {
		t.Fatalf("folding must converge to one card, got %d then %d", len(once), len(twice))
	}
	if once[0].EventSummary != twice[0].EventSummary {
		t.Errorf("card must not re-fold or grow: %q vs %q", once[0].EventSummary, twice[0].EventSummary)
	}
}

func hasSettleFold(refs []memory.EventReference) bool {
	for _, r := range refs {
		if r.EventType == tagentevent.TypeSettleFold {
			return true
		}
	}
	return false
}

// --- settleFoldLine rendering ------------------------------------------------

func TestSettleFoldLine_MarkerTruncationAndPrefixStrip(t *testing.T) {
	body := "[task settled inline] ∞ 长任务描述 (id=deadbeef) alive-detached → 结果: " + strings.Repeat("结", 500)
	prefixed := tagentevent.FormatEventPrefix(77, tagentevent.TypeExternalInput) + " " + body
	line := settleFoldLine(memory.EventReference{
		EventKey: 77, EventType: tagentevent.TypeExternalInput, EventSummary: prefixed,
	})
	if !strings.HasPrefix(line, "∞ ["+tagentevent.FormatEventKey(77)+"] 长任务描述") {
		t.Errorf("row must carry marker + ticket + summary head, got %q", line)
	}
	if strings.Contains(line, "[task settled") {
		t.Errorf("wrapper must be stripped: %q", line)
	}
	if len([]rune(line)) > settleFoldRowMaxChars+40 {
		t.Errorf("row must stay bounded, got %d chars: %q", len([]rune(line)), line)
	}
	// cold-eyes m-1: CJK truncation must stay on rune boundaries — a card
	// line with invalid UTF-8 corrupts every downstream JSON render.
	if !utf8.ValidString(line) {
		t.Errorf("truncated row must be valid UTF-8: %q", line)
	}
}

// --- end-to-end through Compress ---------------------------------------------

// TestCompress_SettleStormFoldReclaims80Percent is the 1.3 acceptance scenario:
// 50 settle external_inputs in the projection, compaction fires → ONE ticket
// card, ≥80% character reclaim, fact chain untouched (every event still
// GetEvent-recallable by its ticket key).
func TestCompress_SettleStormFoldReclaims80Percent(t *testing.T) {
	ctx := context.Background()
	store := memory.NewInMemoryStore()
	refs := settleRefs(101, 50, 600)
	for _, r := range refs {
		if err := store.StoreEvent(r.EventKey, memory.FullEvent{
			EventKey: r.EventKey, EventType: r.EventType,
			EventSummary: r.EventSummary, Content: r.EventSummary, Timestamp: r.Timestamp,
		}); err != nil {
			t.Fatalf("StoreEvent %d: %v", r.EventKey, err)
		}
	}
	refs = append(refs,
		toolRef(500, tagentevent.TypeExternalInput, "用户请求", 900),
		toolRef(501, tagentevent.TypeAgentOutput, "答复", 901))

	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(800))
	cc := NewContextCompressor(sc, store, NewDefaultTokenCounter(), 1000, 0.8, 2)

	beforeChars := 0
	for _, m := range cc.resolveRefs(ctx, refs) {
		beforeChars += len([]rune(m.Content))
	}

	res := cc.Compress(ctx, refs)
	if !res.Compressed {
		t.Fatalf("expected compaction to fire")
	}
	afterChars := 0
	cards := 0
	for _, m := range res.Messages {
		afterChars += len([]rune(m.Content))
		if strings.Contains(m.Content, "|"+tagentevent.TypeSettleFold+"]") {
			cards++
		}
	}
	if cards != 1 {
		t.Fatalf("expected exactly 1 ticket card message, got %d (messages=%d)", cards, len(res.Messages))
	}
	reclaim := 1 - float64(afterChars)/float64(beforeChars)
	if reclaim < 0.80 {
		t.Errorf("expected ≥80%% char reclaim, got %.1f%% (%d -> %d)", reclaim*100, beforeChars, afterChars)
	}

	// Fact chain unaffected: every settle event stays recallable by its key.
	for _, r := range refs[:50] {
		evt, err := store.GetEvent(r.EventKey)
		if err != nil || evt == nil || evt.Content != r.EventSummary {
			t.Fatalf("fact chain event %d damaged by fold: %v", r.EventKey, err)
		}
	}

	// The card's tickets must resolve back to full originals (recall path).
	var cardRef *memory.EventReference
	for i := range res.RetainedRefs {
		if res.RetainedRefs[i].EventType == tagentevent.TypeSettleFold {
			cardRef = &res.RetainedRefs[i]
		}
	}
	if cardRef == nil {
		t.Fatalf("settle_fold card must stay in retained refs: %+v", res.RetainedRefs)
	}
	lines := parseSettleFoldCardLines(cardRef.EventSummary)
	if len(lines) != 50 {
		t.Fatalf("expected 50 ticket rows, got %d", len(lines))
	}
	for i, line := range lines {
		key := tagentevent.FormatEventKey(int64(101 + i))
		if !strings.Contains(line, "["+key+"]") {
			t.Fatalf("row %d missing ticket %s: %q", i, key, line)
		}
	}
}

// --- buildRetainedRefs lifecycle ---------------------------------------------

func TestBuildRetainedRefs_SettleFoldSurvivalAndLosslessExit(t *testing.T) {
	ctx := context.Background()
	cc := newFoldCC(2)
	run := settleRefs(101, 2, 100)
	card := buildSettleFoldRef(run)
	user := toolRef(500, tagentevent.TypeExternalInput, "用户请求", 900)
	refs := []memory.EventReference{user, card}

	// Card message survived the round → ref kept verbatim, no summary forced.
	cardMsg := model.Message{Role: model.RoleUser, Content: prefixEventKey(card.EventSummary, card)}
	userMsg := model.Message{Role: model.RoleUser, Content: tagentevent.FormatEventPrefix(500, tagentevent.TypeExternalInput) + " 用户请求"}
	retained := cc.buildRetainedRefs(refs, []model.Message{userMsg, cardMsg}, ctx)
	survived := false
	for _, r := range retained {
		if r.EventType == tagentevent.TypeSettleFold && r.EventKey == card.EventKey {
			survived = true
		}
	}
	if !survived {
		t.Fatalf("surviving card ref must be retained: %+v", retained)
	}

	// Card retired by L3 → ticket rows move into the rolling summary card
	// sequence and the compacted count grows by the row count (lossless exit).
	retained = cc.buildRetainedRefs(refs, []model.Message{userMsg}, ctx)
	var summary *memory.EventReference
	for i := range retained {
		if retained[i].EventType == tagentevent.TypeContextCompress {
			summary = &retained[i]
		}
	}
	if summary == nil {
		t.Fatalf("retiring a card must emit the rolling summary even with no other drops: %+v", retained)
	}
	for i, r := range run {
		want := "- " + settleFoldLine(r)
		if !strings.Contains(summary.EventSummary, want) {
			t.Errorf("row %d must survive in the card sequence, want %q in %q", i, want, summary.EventSummary)
		}
	}
	if !strings.Contains(summary.EventSummary, fmt.Sprintf("[Compacted %d historical events", len(run))) {
		t.Errorf("rolling count must include the folded settles: %q", summary.EventSummary)
	}
}
