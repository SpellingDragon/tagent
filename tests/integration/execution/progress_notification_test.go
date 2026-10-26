package execution

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// progressNoteFragment is the material a progress notification reports its round with.
const progressNoteFragment = "reports progress in round"

// TestProgressNotificationStaysOutOfTheDecision drives one progress notification through the delivery face of the host loop.
// - a progress notification of an addressed range enters no decision of the invocation that issued the call: the next request carries no progress material
// - the range is opened by one decision before the report arrives, so the fact has an addressed range to land in rather than being discarded unaddressed
// - the notification lands as the fact of its range under the identity production commits it with, so the report is recorded rather than dropped
// - the notification answers no call: the decision holds no tool message under the task
// - a notification that is not progress carries nothing into the decision
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestProgressNotificationStaysOutOfTheDecision(t *testing.T) {
	owner := event.Address{AgentID: "agent-host", SessionID: "session-progress"}

	t.Run("progress_lands_as_a_fact_without_entering_the_decision", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &customEventModel{name: "model-host", answer: func(req *model.Request) *model.Response {
			if progressCarried(req.Messages, progressNoteFragment) {
				t.Errorf("request messages = %+v, want no progress report in the material a decision reads", req.Messages)
			}
			if paired := observationOf(req.Messages, "task-progress-host"); paired != nil {
				t.Errorf("request messages = %+v, want the progress to answer no call", req.Messages)
			}
			return convFinalTurn("progress stays out")
		}}
		rig := newCustomEventRig(t, provider, nil, owner)
		facts := &committedFacts{}
		rig.loop.SetFactCommit(facts.commit)
		if _, err := rig.consume(ctx, provider, customEventOf(owner)); err != nil {
			t.Fatalf("open the range beside the progress notification: %v", err)
		}
		progress := task.Notice{TaskID: "task-progress-host", Round: 1, Kind: task.NoticeProgress, Seq: 0, Event: "fact-progress-host"}
		if err := rig.mail(ctx, progress); err != nil {
			t.Fatalf("hand the progress notification to the mailbox: %v", err)
		}
		next := customEventOf(owner)
		next.Key = "ev-custom-event-next"
		if result, err := rig.consume(ctx, provider, next); err != nil || result != "progress stays out" {
			t.Fatalf("consume beside a progress notification = (%q, %v), want the one consumed result", result, err)
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("model requests = %d, want the one decision per drive", len(requests))
		}
		if progressCarried(requests[1].Messages, progressNoteFragment) {
			t.Fatalf("request messages = %+v, want the progress of the call carried into no decision", requests[1].Messages)
		}
		key := agent.NoticeFactKey(rig.invocationID, progress)
		landed := facts.under(key)
		if len(landed) != 1 {
			t.Fatalf("facts committed under the notice identity = %d, want the one fact the progress lands as", len(landed))
		}
		if !strings.Contains(landed[0].Message.Content, progressNoteFragment) {
			t.Fatalf("notice fact content = %q, want the report recorded in the fact of its range", landed[0].Message.Content)
		}
	})

	t.Run("a notification that is not progress carries nothing into the decision", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &customEventModel{name: "model-host", answer: func(req *model.Request) *model.Response {
			if progressCarried(req.Messages, progressNoteFragment) {
				t.Errorf("request messages = %+v, want no progress material beside an accepted notice", req.Messages)
			}
			return convFinalTurn("accepted consumed")
		}}
		rig := newCustomEventRig(t, provider, nil, owner)
		accepted := task.Notice{TaskID: "task-accepted-host", Round: 1, Kind: task.NoticeAccepted, Seq: 0, Event: "fact-accepted-host"}
		if err := rig.mail(ctx, accepted); err != nil {
			t.Fatalf("hand the accepted notification to the mailbox: %v", err)
		}
		if result, err := rig.consume(ctx, provider, customEventOf(owner)); err != nil || result != "accepted consumed" {
			t.Fatalf("consume beside an accepted notification = (%q, %v), want the one consumed result", result, err)
		}
		if requests := provider.recorded(); len(requests) != 1 {
			t.Fatalf("model requests = %d, want the one decision of the range", len(requests))
		}
	})
}

// progressCarried reports whether one message set carries input material naming the fragment.
func progressCarried(messages []model.Message, fragment string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, fragment) {
			return true
		}
	}
	return false
}

// committedFacts is the commit face of the progress case: it keeps every fact the
// loop commits, so the notice fact can be read back under the identity production
// commits it with rather than under an identity the test invents.
type committedFacts struct {
	mu   sync.Mutex
	seen []event.Event
}

// commit keeps one fact as it stands and reports the identity it carries.
func (c *committedFacts) commit(evt event.Event) (event.EventID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, evt)
	return evt.Key, nil
}

// under reports the facts committed under one identity.
func (c *committedFacts) under(key event.EventID) []event.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var hits []event.Event
	for _, evt := range c.seen {
		if evt.Key == key {
			hits = append(hits, evt)
		}
	}
	return hits
}
