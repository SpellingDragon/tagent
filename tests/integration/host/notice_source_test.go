// The per-message notice-source anchors: one turn that mixes a notice and a
// user message commits each message's own source in its fact, and the
// round-level trigger source is left as the caller declared it. The source is
// therefore readable from the durable fact of the message it came from, without
// reading the body and without a second registry beside the facts.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package host

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// noticeSourceNotice is the body the admitted notice carries into the turn.
const noticeSourceNotice = "the reincarnation notice of the last start"

// noticeSourceAddress is the session the source anchors commit into.
var noticeSourceAddress = event.Address{AgentID: "agent-a", SessionID: "session-notice-source"}

// TestNoticeSymptomSourceQueryable pins the source of each message in the durable facts of its turn.
// - the notice fact is queryable by its own source without reading its body
// - the user message of the same turn carries no notice source
// - the round-level trigger source is left as the caller declared it, even when a per-message entry names one (负锚 round_trigger_source_untouched)
func TestNoticeSymptomSourceQueryable(t *testing.T) {
	t.Run("round_trigger_source_untouched", noticeSourceUntouched)
}

// noticeSourceTurn drives the one turn the anchors read: a notice beside a user
// message, each naming its own source, under a round trigger source the caller
// declares and a per-message entry that tries to overwrite it.
func noticeSourceTurn(t *testing.T, run *assembly.Run) []event.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID:  "inv-notice-source",
		Address:       noticeSourceAddress,
		TriggerSource: "user",
		Input: []model.Message{
			{Role: model.RoleUser, Content: noticeSourceNotice},
			{Role: model.RoleUser, Content: "the question of the user"},
		},
		InputMeta: []map[string]string{
			{event.MetaKeyNoticeSource: "reincarnation", event.MetaKeyTriggerSource: "spoofed"},
			nil,
		},
	}); err != nil {
		t.Fatalf("invoke the mixed turn: %v", err)
	}
	var inputs []event.Event
	for _, fact := range run.SessionFacts(noticeSourceAddress) {
		if fact.Type == event.TypeExternalInput {
			inputs = append(inputs, fact)
		}
	}
	if len(inputs) != 2 {
		t.Fatalf("input facts = %d, want the notice and the user message", len(inputs))
	}
	return inputs
}

// noticeSourceUntouched reads the two input facts of the mixed turn: the notice
// answers its own source, the user message carries none, and every message of
// the turn keeps the round trigger source the caller declared.
func noticeSourceUntouched(t *testing.T) {
	run := factFidelityAssemble(t, t.TempDir(), factFidelityProvider())
	defer factFidelityClose(t, run)
	inputs := noticeSourceTurn(t, run)
	notice, user := inputs[0], inputs[1]
	if got := notice.Meta[event.MetaKeyNoticeSource]; got != "reincarnation" {
		t.Fatalf("notice source = %q, want the source the message was admitted from", got)
	}
	if got, ok := user.Meta[event.MetaKeyNoticeSource]; ok {
		t.Fatalf("the user message carries the notice source %q", got)
	}
	for _, fact := range inputs {
		if got := fact.Meta[event.MetaKeyTriggerSource]; got != "user" {
			t.Fatalf("trigger source of %q = %q, want the declared round stamp", fact.Message.Content, got)
		}
	}
}
