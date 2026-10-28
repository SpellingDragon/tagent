// The history-restore anchors of the host: a session chain's history must
// survive the restart and reach the next request no matter what presentation
// budget its declaration names. A declaration of max_tokens: 0 names no
// presentation budget — the window is the budget and the fold measure, never
// the registration gate — so the chain registers its mapped sequence exactly
// as a budgeted one does and the request after the restart carries the old
// history in full. The two anchors beside it pin the identity the window
// keeps: the budget still shapes presentation when the window names one, and
// the age fold still folds under it — restoration is decoupled from the budget
// without dissolving the budget itself.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package host

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// historyRestoreHugeTurn renders the oversized turn of a budget scenario: a
// named head the whole text presents under, filler material that rides far over
// one 32000-token window, and a tail marker the budgeted presentation must cut.
func historyRestoreHugeTurn(head, tail string) string {
	return head + "\n" + strings.Repeat("unbounded material ", 8400) + "\n" + tail
}

// requestText renders every message content of one served request in message
// order, so a presence check reads exactly what the wire carried.
func requestText(request scriptedRequest) string {
	var out strings.Builder
	for _, message := range request.Messages {
		out.WriteString(message.Content)
		out.WriteByte('\n')
	}
	return out.String()
}

// requireRequestHistory requires one request to carry the named history
// material under the role it was committed as, so a green presence check proves
// the restored history reached the wire in its own voice instead of riding
// inside some unrelated message.
func requireRequestHistory(t *testing.T, request scriptedRequest, role model.Role, content, what string) {
	t.Helper()
	for _, message := range request.Messages {
		if message.Role == string(role) && strings.Contains(message.Content, content) {
			return
		}
	}
	t.Fatalf("the request carries no restored history: the %s (%s) material %q is absent from the wire, which carried %d messages",
		what, role, content, len(request.Messages))
}

// TestHistoryRestoresWithoutBudget requires a max_tokens: 0 chain to restore its history into the next request.
//   - positive: the request after the restart carries the turn-one input and answer of the same session
//   - negative A: max_tokens: 32000 still folds the chain into synthetic products and still shapes presentation
//   - negative B: the budgeted probe request degrades to a ticket and cuts the huge tail; max_tokens: 0 never does
//   - consumer: the scenario boots the formal assembly over a max_tokens: 0 document and drives real Host.Invoke turns
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestHistoryRestoresWithoutBudget(t *testing.T) {
	reproductionEnv(t)
	t.Run("history_restores_without_budget", runHistoryRestoresWithoutBudget)
	t.Run("presentation_is_unlimited_at_zero", runPresentationUnlimitedAtZero)
	t.Run("budget_window_still_folds_and_shapes", runBudgetWindowStillFoldsAndShapes)
}

// runHistoryRestoresWithoutBudget drives one conversation turn under a
// max_tokens: 0 declaration, closes the host and re-boots the same document
// over the same storage root, and requires the next turn's request to carry the
// turn the run before it committed: the input that opened it and the answer
// that closed it, each in its own role. The zero window registers its mapped
// sequence like any other window, so the restart reads the chain the first
// boot committed instead of a transcript no run carries.
func runHistoryRestoresWithoutBudget(t *testing.T) {
	const (
		inputOne  = "history-restore turn one input marker"
		answerOne = "history-restore turn one answer marker"
		inputTwo  = "history-restore turn two input marker"
		answerTwo = "history-restore turn two answer marker"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the first boot answers turn one", requestTail(inputOne)).
		answers(scriptedAnswerOf(answerOne)).
		delivers(answerOne)
	script.expect("model-entry", "the restarted run answers turn two", requestTail(inputTwo)).
		answers(scriptedAnswerOf(answerTwo)).
		delivers(answerTwo)
	provider.decide = fidelityGate(script.decide)

	sender := newRecordingSender()
	host, configPath := bootFidelityHost(t, provider, sender, t.TempDir(), "    max_tokens: 0\n")
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-history-restore-no-budget"}

	turn, err := driveIntegrityTurn(ctx, host, addr, inputOne)
	if err != nil {
		t.Fatalf("the first-boot turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("the first-boot turn = %v, want the completed turn", turn.Terminal)
	}
	closeIntegrityHost(t, ctx, host)

	rebooted, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("re-boot the history-restore host over the same storage root: %v", err)
	}
	keepIntegrityHost(t, script, rebooted)
	turnTwo, turnTwoErr := driveIntegrityTurn(ctx, rebooted, addr, inputTwo)

	var target scriptedRequest
	for _, request := range provider.servedRequests() {
		if requestTail(inputTwo)(request) {
			target = request
		}
	}
	if len(target.Messages) == 0 {
		t.Fatalf("the provider served no request for the turn after the restart")
	}
	requireRequestHistory(t, target, model.RoleUser, inputOne, "turn-one input")
	requireRequestHistory(t, target, model.RoleAssistant, answerOne, "turn-one answer")
	if _, registered := rebooted.Run().ReplayEntries(addr.SessionID); !registered {
		t.Fatalf("the restarted run registered no mapped sequence for the max_tokens: 0 chain, so the history above could not have been assembled from it")
	}
	if turnTwoErr != nil {
		t.Fatalf("the turn after the restart ended by error: %v", turnTwoErr)
	}
	if turnTwo.Terminal != model.KindCompleted {
		t.Fatalf("the turn after the restart = %v, want the completed turn", turnTwo.Terminal)
	}
	t.Logf("[history-restore 1] positive: the request after the restart carries the turn-one history (%q, %q) under max_tokens: 0", inputOne, answerOne)
	closeIntegrityHost(t, ctx, rebooted)
}

// runPresentationUnlimitedAtZero drives one oversized turn under a
// max_tokens: 0 declaration, restarts the run and requires the next request to
// carry the whole projection: the tail marker of the huge turn is on the wire
// and no ticket stands in its place. A presentation capped at some default
// budget would degrade the entry to a ticket here, and a fold that consumed
// the entry would cut the tail marker — both fail this anchor.
func runPresentationUnlimitedAtZero(t *testing.T) {
	const (
		hugeHead  = "history-restore unbounded head marker"
		hugeTail  = "history-restore unbounded tail marker"
		answerOne = "history-restore unbounded turn answer"
		inputTwo  = "history-restore unbounded probe input"
		answerTwo = "history-restore unbounded probe answer"
	)
	huge := historyRestoreHugeTurn(hugeHead, hugeTail)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the first boot answers the unbounded turn", requestTail(huge)).
		answers(scriptedAnswerOf(answerOne)).
		delivers(answerOne)
	script.expect("model-entry", "the restarted run answers the probe", requestTail(inputTwo)).
		answers(scriptedAnswerOf(answerTwo)).
		delivers(answerTwo)
	provider.decide = fidelityGate(script.decide)

	sender := newRecordingSender()
	host, configPath := bootFidelityHost(t, provider, sender, t.TempDir(), "    max_tokens: 0\n")
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-history-restore-unbounded"}

	turn, err := driveIntegrityTurn(ctx, host, addr, huge)
	if err != nil {
		t.Fatalf("the unbounded turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("the unbounded turn = %v, want the completed turn", turn.Terminal)
	}
	closeIntegrityHost(t, ctx, host)

	rebooted, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("re-boot the unbounded host over the same storage root: %v", err)
	}
	keepIntegrityHost(t, script, rebooted)
	turnTwo, turnTwoErr := driveIntegrityTurn(ctx, rebooted, addr, inputTwo)

	var target scriptedRequest
	for _, request := range provider.servedRequests() {
		if requestTail(inputTwo)(request) {
			target = request
		}
	}
	if len(target.Messages) == 0 {
		t.Fatalf("the provider served no request for the unbounded probe")
	}
	text := requestText(target)
	if !strings.Contains(text, hugeTail) {
		t.Fatalf("the max_tokens: 0 request presented no full projection: the tail marker %q is absent, so the unbounded material degraded (wire carries %d chars)", hugeTail, len(text))
	}
	if !strings.Contains(text, hugeHead) {
		t.Fatalf("the max_tokens: 0 request presented no full projection: the head marker %q is absent", hugeHead)
	}
	if strings.Contains(text, "[ticket") {
		t.Fatalf("the max_tokens: 0 request degraded material to a ticket although the declaration names no budget: %q", text)
	}
	if turnTwoErr != nil {
		t.Fatalf("the unbounded probe ended by error: %v", turnTwoErr)
	}
	if turnTwo.Terminal != model.KindCompleted {
		t.Fatalf("the unbounded probe = %v, want the completed turn", turnTwo.Terminal)
	}
	t.Logf("[history-restore 2] negative: the max_tokens: 0 request presents the whole %d-char projection with no ticket", len(text))
	closeIntegrityHost(t, ctx, rebooted)
}

// runBudgetWindowStillFoldsAndShapes fills one session chain under a budgeted
// declaration (max_tokens: 32000) — small fill turns, one oversized turn, and a
// probe — and requires both identities of the budget to stand: the age fold
// still folds the aged turns into synthetic products, and the presentation
// budget still shapes the probe request, which degrades the oversized turn to a
// ticket and cuts its tail marker. The budget keeps both identities, so no
// repair to the restore path may dissolve the budget it decoupled.
func runBudgetWindowStillFoldsAndShapes(t *testing.T) {
	const (
		hugeHead    = "budget-shapes huge head marker"
		hugeTail    = "budget-shapes huge tail marker"
		probeInput  = "budget-shapes probe input"
		probeAnswer = "budget-shapes probe answer"
	)
	huge := historyRestoreHugeTurn(hugeHead, hugeTail)
	fill := func(turn int) string {
		return fmt.Sprintf("budget-shapes fill turn %02d\n%s", turn, strings.Repeat("material ", 950))
	}
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	for turn := 0; turn < 2; turn++ {
		answer := fmt.Sprintf("budget-shapes fill answer %02d", turn)
		script.expect("model-entry", fmt.Sprintf("fill turn %d", turn+1), requestTail(fill(turn))).
			answers(scriptedAnswerOf(answer)).
			delivers(answer)
	}
	script.expect("model-entry", "the oversized turn answers", requestTail(huge)).
		answers(scriptedAnswerOf("budget-shapes huge answer")).
		delivers("budget-shapes huge answer")
	script.expect("model-entry", "the probe turn speaks from the chain", requestTail(probeInput)).
		answers(scriptedAnswerOf(probeAnswer)).
		delivers(probeAnswer)
	provider.decide = fidelityGate(script.decide)

	sender := newRecordingSender()
	host, _ := bootFidelityHost(t, provider, sender, t.TempDir(),
		"    max_tokens: 32000\n"+
			"    compress_threshold: 0.1\n"+
			"    keep_recent_tasks: 2\n")
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-history-restore-budget-shapes"}

	for turn := 0; turn < 2; turn++ {
		if _, err := driveIntegrityTurn(ctx, host, addr, fill(turn)); err != nil {
			t.Fatalf("fill turn %d ended by error: %v", turn+1, err)
		}
	}
	if _, err := driveIntegrityTurn(ctx, host, addr, huge); err != nil {
		t.Fatalf("the oversized turn ended by error: %v", err)
	}
	probe, probeErr := driveIntegrityTurn(ctx, host, addr, probeInput)

	entries, registered := host.Run().ReplayEntries(addr.SessionID)
	if !registered {
		t.Fatalf("the budgeted chain registered no projection, so neither fold nor budget could run")
	}
	synthetic := 0
	for _, entry := range entries {
		if replay.SyntheticKey(entry.Key) {
			synthetic++
		}
	}
	if synthetic == 0 {
		t.Fatalf("the aged turns folded nothing under the 32000-token window: the projection carries %d entries, none synthetic", len(entries))
	}

	var target scriptedRequest
	for _, request := range provider.servedRequests() {
		if requestTail(probeInput)(request) {
			target = request
		}
	}
	if len(target.Messages) == 0 {
		t.Fatalf("the provider served no request for the probe turn")
	}
	text := requestText(target)
	if !strings.Contains(text, "[ticket") {
		t.Fatalf("the budgeted request degraded nothing although the chain carries %d entries over the window: %q", len(entries), text)
	}
	if strings.Contains(text, hugeTail) {
		t.Fatalf("the budgeted request presented the oversized turn in full: the tail marker %q reached the wire although the window bounds it", hugeTail)
	}
	if probeErr != nil {
		t.Fatalf("the probe turn ended by error: %v", probeErr)
	}
	if probe.Terminal != model.KindCompleted {
		t.Fatalf("the probe turn = %v, want the completed turn", probe.Terminal)
	}
	t.Logf("[history-restore 3] negative: the 32000-token window still folds (%d synthetic products) and still shapes the request (ticket present, tail marker cut)", synthetic)
	closeIntegrityHost(t, ctx, host)
}
