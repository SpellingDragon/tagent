// The boot boundary of the resident consumption loop: a restart opens on the
// chain it already answered and drives only what that chain left unanswered.
package host

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// TestBootOverAnsweredChainAnswersNothing pins the first pull of a restarted host.
//
// - a closed segment presents its input as the history prefix of the next request, so the first pull of a boot seeds those facts as read
// - the restarted host over an answered chain serves no model request and delivers no body for the whole quiet window
// - a fresh input still drives: the answer for it arrives after the restart, so the seed hides the answered head and nothing else
// - the body the first host delivered is not sent again: its obligation carries a settled receipt, so the restart adds only the fresh answer
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestBootOverAnsweredChainAnswersNothing(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	const firstAnswer = "the boot chain answered once"
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf(firstAnswer) }
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-boot-replay"}

	if turn := invokeScriptedTurn(t, ctx, host, string(addr.SessionID)); turn.Terminal != model.KindCompleted {
		t.Fatalf("the first turn terminal = %v, want the completed answer", turn.Terminal)
	}
	waitForSettle(t, "the first answer to be delivered", func() bool {
		return len(sender.bodies()) == 1 && sender.bodies()[0] == firstAnswer
	})
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the first host: %v", err)
	}
	served, delivered := len(provider.servedModels()), len(sender.bodies())

	restarted, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot over the answered chain: %v", err)
	}
	defer func() {
		if err := restarted.Close(ctx); err != nil {
			t.Fatalf("close the restarted host: %v", err)
		}
	}()
	continuationQuiet(t, provider, time.Second)
	if got := len(provider.servedModels()); got != served {
		t.Fatalf("model requests after the restart = %d, want the %d the first host served: the answered chain drives no second turn", got, served)
	}
	if got := len(sender.bodies()); got != delivered {
		t.Fatalf("delivered bodies after the restart = %d, want the %d the first host delivered: the answered chain drives no second answer", got, delivered)
	}

	const secondAnswer = "the fresh input answered after the restart"
	provider.answer = func(int) string { return scriptedAnswerOf(secondAnswer) }
	turn := invokeScriptedTurn(t, ctx, restarted, string(addr.SessionID))
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("the turn after the restart terminal = %v, want the completed answer", turn.Terminal)
	}
	deadline := time.Now().Add(20 * time.Second)
	for len(sender.bodies()) != 2 || sender.bodies()[1] != secondAnswer {
		if time.Now().After(deadline) {
			t.Fatalf("delivered bodies after the fresh turn = %q (requests %v), want the first answer beside %q",
				sender.bodies(), provider.servedModels(), secondAnswer)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
