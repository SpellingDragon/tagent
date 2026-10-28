// The history of one session chain is read from one face and one face only — the mapped sequence the commits record — so
// what a chain restores is the same sequence whether or not its declaration
// names a presentation budget. A budget shapes what a request presents; it never
// decides whether the chain's history exists, so two chains of one session that
// committed the same turns restore the same messages in the same order, before
// and after a restart alike.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package host

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// singleHistoryInputOne and the markers beside it spell the turns both chains
// of the anchor commit: one session, one transcript, driven twice before the
// restart and probed once after it.
const (
	singleHistoryInputOne  = "single-history turn one input marker"
	singleHistoryAnswerOne = "single-history turn one answer marker"
	singleHistoryInputTwo  = "single-history turn two input marker"
	singleHistoryAnswerTwo = "single-history turn two answer marker"
	singleHistoryProbe     = "single-history probe input marker"
)

// singleHistoryChain is the material one budget arm of the anchor hands to the
// comparison: the messages the second turn's request carried before the restart
// beside the messages the probe request carried after it.
type singleHistoryChain struct {
	beforeRestart []scriptedMessage
	afterRestart  []scriptedMessage
}

// TestSingleHistoryPath requires one session chain to restore one history whether or not it names a budget.
//   - positive: both chains restore the turn-one and turn-two material of the session, message for message
//   - negative: the budget arm restores exactly what the no-budget arm restores, before and after the restart
//   - consumer: both arms boot the formal assembly over their own storage root and drive real Host.Invoke turns, restarting over the same document
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestSingleHistoryPath(t *testing.T) {
	reproductionEnv(t)
	t.Run("restored_history_is_one_path_for_both_windows", runSingleHistoryPath)
}

// runSingleHistoryPath drives the named turns through one no-budget chain and
// one budgeted chain of the same session, restarts both over the document they
// booted, and requires what each arm restores to be the same messages in the
// same roles: the budget changes what a request presents, never which history
// the chain reads it from.
func runSingleHistoryPath(t *testing.T) {
	noBudget := driveSingleHistoryChain(t, "    max_tokens: 0\n")
	budgeted := driveSingleHistoryChain(t, "    max_tokens: 32000\n")

	if len(noBudget.afterRestart) == 0 || len(budgeted.afterRestart) == 0 {
		t.Fatalf("the probe request of an arm carries no material: no-budget=%d budgeted=%d", len(noBudget.afterRestart), len(budgeted.afterRestart))
	}
	restoredNoBudget := noBudget.afterRestart[:len(noBudget.afterRestart)-1]
	restoredBudgeted := budgeted.afterRestart[:len(budgeted.afterRestart)-1]
	if len(restoredNoBudget) == 0 {
		t.Fatalf("the restarted no-budget chain restored no history: the probe request carried %d message(s)", len(noBudget.afterRestart))
	}
	requireSameHistory(t, "the second turns of the two windows, which differ only in the budget the declaration names", noBudget.beforeRestart, budgeted.beforeRestart)
	requireSameHistory(t, "the restarted probes of the two windows, which read the same session forward", noBudget.afterRestart, budgeted.afterRestart)
	requireHistoryExtends(t, "the restarted no-budget chain, whose second turn is the head of the probe's material", noBudget.beforeRestart[:len(noBudget.beforeRestart)-1], restoredNoBudget)
	requireHistoryExtends(t, "the restarted budgeted chain", budgeted.beforeRestart[:len(budgeted.beforeRestart)-1], restoredBudgeted)
	requireHistoryCarries(t, restoredNoBudget, singleHistoryInputOne, singleHistoryAnswerOne)
	requireHistoryCarries(t, restoredNoBudget, singleHistoryInputTwo, singleHistoryAnswerTwo)
	t.Logf("[single-history] positive: both arms restored %d message(s) of the same session, before and after the restart", len(restoredNoBudget))
}

// driveSingleHistoryChain boots one arm of the anchor over its own storage root
// with the given window block, drives the two committed turns, restarts the same
// document and drives the probe, and answers the messages the second turn and
// the probe carried.
func driveSingleHistoryChain(t *testing.T, window string) singleHistoryChain {
	t.Helper()
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the arm answers turn one", requestTail(singleHistoryInputOne)).
		answers(scriptedAnswerOf(singleHistoryAnswerOne)).
		delivers(singleHistoryAnswerOne)
	script.expect("model-entry", "the arm answers turn two", requestTail(singleHistoryInputTwo)).
		answers(scriptedAnswerOf(singleHistoryAnswerTwo)).
		delivers(singleHistoryAnswerTwo)
	script.expect("model-entry", "the restarted arm answers the probe", requestTail(singleHistoryProbe)).
		answers(scriptedAnswerOf("single-history probe answer")).
		delivers("single-history probe answer")
	provider.decide = fidelityGate(script.decide)

	sender := newRecordingSender()
	host, configPath := bootFidelityHost(t, provider, sender, t.TempDir(), window)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-single-history"}

	for _, input := range []string{singleHistoryInputOne, singleHistoryInputTwo} {
		turn, err := driveIntegrityTurn(ctx, host, addr, input)
		if err != nil {
			t.Fatalf("the arm's turn for %q ended by error: %v", input, err)
		}
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("the arm's turn for %q = %v, want the completed turn", input, turn.Terminal)
		}
	}
	closeIntegrityHost(t, ctx, host)

	rebooted, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("re-boot the arm over the same storage root: %v", err)
	}
	keepIntegrityHost(t, script, rebooted)
	probe, probeErr := driveIntegrityTurn(ctx, rebooted, addr, singleHistoryProbe)
	if probeErr != nil {
		t.Fatalf("the restarted arm's probe ended by error: %v", probeErr)
	}
	if probe.Terminal != model.KindCompleted {
		t.Fatalf("the restarted arm's probe = %v, want the completed turn", probe.Terminal)
	}
	if history := rebooted.Run().SessionHistory(addr); len(history) != 0 {
		t.Fatalf("the restarted arm keeps a transcript beside the mapped sequence (%d message(s)): one session reads from one face", len(history))
	}
	if _, registered := rebooted.Run().ReplayEntries(addr.SessionID); !registered {
		t.Fatalf("the restarted arm registered no mapped sequence, so the history compared here would come from a second face")
	}
	chain := singleHistoryChain{
		beforeRestart: singleHistoryRequest(t, provider, singleHistoryInputTwo),
		afterRestart:  singleHistoryRequest(t, provider, singleHistoryProbe),
	}
	closeIntegrityHost(t, ctx, rebooted)
	return chain
}

// singleHistoryRequest reports the messages of the request whose last message is
// the named input, so a comparison reads exactly what the wire carried.
func singleHistoryRequest(t *testing.T, provider *scriptedProvider, input string) []scriptedMessage {
	t.Helper()
	for _, request := range provider.servedRequests() {
		if requestTail(input)(request) {
			return request.Messages
		}
	}
	t.Fatalf("the provider served no request whose tail is %q", input)
	return nil
}

// requireSameHistory requires two histories to carry the same messages in the
// same roles and order, and names the first message that differs.
func requireSameHistory(t *testing.T, what string, want, got []scriptedMessage) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s restored %d message(s), want the %d of the other arm: %+v vs %+v", what, len(got), len(want), got, want)
	}
	for at := range want {
		if want[at].Role != got[at].Role || want[at].Content != got[at].Content {
			t.Fatalf("%s restored message %d as (%q, %q), want (%q, %q): the two arms read different histories",
				what, at, got[at].Role, got[at].Content, want[at].Role, want[at].Content)
		}
	}
}

// requireHistoryExtends requires the longer history to carry the shorter one as
// its head, message for message: the chain reads forward, so what it presented
// before the restart is what it presents after it, with the turns committed in
// between behind.
func requireHistoryExtends(t *testing.T, what string, head, longer []scriptedMessage) {
	t.Helper()
	if len(head) > len(longer) {
		t.Fatalf("%s restored %d message(s), fewer than the %d the chain presented before the restart", what, len(longer), len(head))
	}
	requireSameHistory(t, what+" read forward", head, longer[:len(head)])
}

// requireHistoryCarries requires the restored history to carry the named input
// and answer of the session under their own roles, so a green comparison is
// never a pass over two empty or two truncated sequences.
func requireHistoryCarries(t *testing.T, history []scriptedMessage, input, answer string) {
	t.Helper()
	var carriedInput, carriedAnswer bool
	for _, message := range history {
		if message.Role == string(model.RoleUser) && message.Content == input {
			carriedInput = true
		}
		if message.Role == string(model.RoleAssistant) && message.Content == answer {
			carriedAnswer = true
		}
	}
	if !carriedInput || !carriedAnswer {
		t.Fatalf("the restored history carries the turn-one input=%t answer=%t, want both: %+v", carriedInput, carriedAnswer, history)
	}
}
