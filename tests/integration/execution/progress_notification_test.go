package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// progressNoteFragment is the input material a progress notification is handed to the range as.
const progressNoteFragment = "reports progress in round"

// TestProgressNotificationReachesTheAgent drives one progress notification through the delivery face of the host loop.
// - a progress notification handed to the mailbox enters no decision of the invocation that issued the call
// - the notification answers no call: the decision holds no tool message under the task
// - a notification that is not progress carries nothing into the decision
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestProgressNotificationReachesTheAgent(t *testing.T) {
	owner := event.Address{AgentID: "agent-host", SessionID: "session-progress"}

	t.Run("a progress notification enters the decision that issued the call", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &customEventModel{name: "model-host", answer: func(req *model.Request) *model.Response {
			if progressCarried(req.Messages, progressNoteFragment) {
				t.Errorf("request messages = %+v, want no progress report in the material a decision reads", req.Messages)
			}
			if paired := observationOf(req.Messages, "task-progress-host"); paired != nil {
				t.Errorf("request messages = %+v, want the progress to answer no call", req.Messages)
			}
			return convFinalTurn("progress consumed")
		}}
		rig := newCustomEventRig(t, provider, nil, owner)
		progress := task.Notice{TaskID: "task-progress-host", Round: 1, Kind: task.NoticeProgress, Seq: 0, Event: "fact-progress-host"}
		if err := rig.mail(ctx, progress); err != nil {
			t.Fatalf("hand the progress notification to the mailbox: %v", err)
		}
		if result, err := rig.consume(ctx, provider, customEventOf(owner)); err != nil || result != "progress consumed" {
			t.Fatalf("consume beside a progress notification = (%q, %v), want the one consumed result", result, err)
		}
		requests := provider.recorded()
		if len(requests) != 1 {
			t.Fatalf("model requests = %d, want the one decision of the range", len(requests))
		}
		if progressCarried(requests[0].Messages, progressNoteFragment) {
			t.Fatalf("request messages = %+v, want the progress of the call carried into no decision", requests[0].Messages)
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
