package task

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestDeliveryFactOfIsOneFactForBothWriteFaces pins the one fact identity every face of a delivery names against the second record a local assembly minted.
//   - the identity is the notification's own, so a retried delivery and a second write face meet the fact the first one landed
//   - two faces rendering one notification render one fact: the stamps the commit protocol generates beside it move, the fact itself does not
//   - the rendering states the settle line the notification carries, so a reader that opens a segment on the fact reads what settled
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDeliveryFactOfIsOneFactForBothWriteFaces(t *testing.T) {
	notice := Notice{
		TaskID: "task-delivery-fact", Round: 2, Kind: NoticeSettled, Seq: 0,
		Event: event.EventID("task-delivery-fact-result"),
		Text:  "the delegated call answered",
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	const instance = event.InstanceID("instance-a")

	t.Run("the identity is the notification", func(t *testing.T) {
		if got, want := string(DeliveryFactKey(notice)), fmt.Sprintf("settle-%x", notice.FactKey()); got != want {
			t.Fatalf("the delivery identity = %q, want %q: one notification stands for one delivery fact", got, want)
		}
		if again := DeliveryFactKey(notice); again != DeliveryFactKey(notice) {
			t.Fatalf("the delivery identity of one notification answered %q and %q: a retried delivery must keep the identity it landed under",
				again, DeliveryFactKey(notice))
		}
		if other := DeliveryFactKey(Notice{TaskID: notice.TaskID, Round: notice.Round, Kind: NoticeFailed, Seq: notice.Seq}); other == DeliveryFactKey(notice) {
			t.Fatalf("the delivery identities of a settled and a failed round = %q, want two identities", other)
		}
	})

	t.Run("two faces render one fact", func(t *testing.T) {
		backflow := DeliveryFactOf(notice, addr, instance, "invocation-backflow")
		delivered := DeliveryFactOf(notice, addr, instance, "invocation-delivery")
		if backflow.Key != delivered.Key {
			t.Fatalf("the two faces committed %q and %q, want the one identity of the notification",
				backflow.Key, delivered.Key)
		}
		if !sameDeliveryFact(backflow, delivered) {
			t.Fatalf("the two faces rendered two facts: backflow=%+v delivered=%+v, want the same type, task, owner, body and metadata so the second commit is the repeat",
				backflow, delivered)
		}
	})

	t.Run("the rendering states the settle line of the notification", func(t *testing.T) {
		fact := DeliveryFactOf(notice, addr, instance, "invocation-any")
		if fact.Type != event.TypeTaskSettled {
			t.Fatalf("the delivery fact type = %q, want %q", fact.Type, event.TypeTaskSettled)
		}
		for _, key := range []string{event.MetaKeySettleTriggerSource, event.MetaKeyNoticeKind, event.MetaKeyTaskID} {
			if fact.Meta[key] == "" {
				t.Fatalf("the delivery fact carries no %q entry: %+v", key, fact.Meta)
			}
		}
		if fact.TaskID != notice.TaskID {
			t.Fatalf("the delivery fact names the task %q, want %q", fact.TaskID, notice.TaskID)
		}
		if body := fact.Message.Content; body == "" || !containsAll(body, string(notice.TaskID), string(notice.Kind), notice.Text) {
			t.Fatalf("the delivery fact body = %q, want the task, the kind and the result text the notification carries", body)
		}
	})
}

// sameDeliveryFact reports whether two renderings state one fact: the type, the
// key, the task, the owner and routing, the body and every metadata entry. The
// record time, the invocation and the instance describe the commit rather than
// the fact, so they decide nothing here.
func sameDeliveryFact(one, other event.Event) bool {
	if one.Key != other.Key || one.Type != other.Type || one.TaskID != other.TaskID {
		return false
	}
	if one.Owner != other.Owner || one.Source != other.Source || one.Target != other.Target {
		return false
	}
	if one.Message.Role != other.Message.Role || one.Message.Content != other.Message.Content {
		return false
	}
	if len(one.Meta) != len(other.Meta) {
		return false
	}
	for key, value := range one.Meta {
		if other.Meta[key] != value {
			return false
		}
	}
	return true
}

// containsAll reports whether one text carries every fragment named.
func containsAll(text string, fragments ...string) bool {
	for _, fragment := range fragments {
		if fragment == "" || !strings.Contains(text, fragment) {
			return false
		}
	}
	return true
}
