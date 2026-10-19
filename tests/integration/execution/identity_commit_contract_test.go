// Package execution holds the execution-layer integration contract tests: event
// identity and native payload fidelity beside the in-memory task admission and
// commit carriers.
package execution

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestIdentityAndCommitContract checks event identity, native payload fidelity and the layered admission, commit and delivery results.
// - a native assistant message with multimodal content parts survives an event round trip with its role, parts and call identifiers intact
// - the same CallID in another instance is admitted as another call, and one instance, invocation and decision stays one dedup scope
// - a committed, a refused and an uncertain commit stay separately observable
// - an admission receipt stands beside the commit result and the delivery state
// - an idempotent repeat commit leaves the delivery obligation open
// - a committed record carries the frozen delivery routing of each notification and never re-resolves a target
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestIdentityAndCommitContract(t *testing.T) {
	t.Run("native payload round trip", func(t *testing.T) {
		text := "look at this"
		payload := trpcmodel.Message{
			Role:    trpcmodel.RoleAssistant,
			Content: "done",
			ContentParts: []trpcmodel.ContentPart{
				{Type: trpcmodel.ContentTypeText, Text: &text},
				{Type: trpcmodel.ContentTypeImage, Image: &trpcmodel.Image{
					URL:    "https://example.invalid/a.png",
					Detail: "high",
				}},
			},
			ToolCalls: []trpcmodel.ToolCall{
				{
					Type: "function",
					ID:   "call_7f3a",
					Function: trpcmodel.FunctionDefinitionParam{
						Name:      "shell",
						Arguments: []byte(`{"cmd":"ls"}`),
					},
				},
			},
			ReasoningContent:   "planning",
			ReasoningSignature: "sig-1",
		}
		meta := map[string]string{
			event.MetaKeyEventKey:            "evt-1",
			event.MetaKeyPartitionID:         "part-1",
			event.MetaKeyEventType:           event.TypeActionCommand,
			event.MetaKeyEventSummary:        "run shell",
			event.MetaKeyTriggerSource:       "model",
			event.MetaKeySettleTriggerSource: "settle-1",
			event.MetaKeyBundleID:            "bundle-1",
			event.MetaPrefix + "trace":       "trace-9",
			"custom_key":                     "kept",
		}
		fact := event.Event{
			Key:          "evt-1",
			Type:         event.TypeActionCommand,
			Timestamp:    time.Unix(1760000000, 0).UTC(),
			Meta:         meta,
			Message:      payload,
			Owner:        event.Address{AgentID: "agent-a", SessionID: "session-a"},
			Source:       event.Address{AgentID: "agent-a", SessionID: "session-a"},
			Target:       event.Address{AgentID: "agent-b", SessionID: "session-b"},
			InvocationID: "inv-1",
			TaskID:       "task-1",
			InstanceID:   "instance-a",
			EpisodeID:    "episode-1",
			DecisionID:   "decision-1",
			AttemptID:    "attempt-1",
			CallID:       "call_7f3a",
		}

		raw, err := json.Marshal(fact)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		var restored event.Event
		if err := json.Unmarshal(raw, &restored); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if restored.Message.Role != trpcmodel.RoleAssistant {
			t.Fatalf("role = %q, want %q", restored.Message.Role, trpcmodel.RoleAssistant)
		}
		if restored.Message.Content != payload.Content {
			t.Fatalf("content = %q, want %q", restored.Message.Content, payload.Content)
		}
		if len(restored.Message.ContentParts) != 2 {
			t.Fatalf("content parts = %d, want 2", len(restored.Message.ContentParts))
		}
		if restored.Message.ContentParts[0].Type != trpcmodel.ContentTypeText ||
			restored.Message.ContentParts[0].Text == nil ||
			*restored.Message.ContentParts[0].Text != text {
			t.Fatalf("text part = %+v, want type %q with %q", restored.Message.ContentParts[0], trpcmodel.ContentTypeText, text)
		}
		if restored.Message.ContentParts[1].Type != trpcmodel.ContentTypeImage ||
			restored.Message.ContentParts[1].Image == nil ||
			restored.Message.ContentParts[1].Image.URL != payload.ContentParts[1].Image.URL {
			t.Fatalf("image part = %+v, want the native image part preserved", restored.Message.ContentParts[1])
		}
		if len(restored.Message.ToolCalls) != 1 ||
			restored.Message.ToolCalls[0].ID != "call_7f3a" ||
			restored.Message.ToolCalls[0].Function.Name != "shell" ||
			string(restored.Message.ToolCalls[0].Function.Arguments) != string(payload.ToolCalls[0].Function.Arguments) {
			t.Fatalf("tool calls = %+v, want one call with its identifier and arguments preserved", restored.Message.ToolCalls)
		}
		if restored.Message.ReasoningContent != payload.ReasoningContent ||
			restored.Message.ReasoningSignature != payload.ReasoningSignature {
			t.Fatalf("reasoning = %q/%q, want %q/%q", restored.Message.ReasoningContent, restored.Message.ReasoningSignature,
				payload.ReasoningContent, payload.ReasoningSignature)
		}
		if restored.CallID != "call_7f3a" {
			t.Fatalf("call id = %q, want %q", restored.CallID, "call_7f3a")
		}
		if restored.InstanceID != fact.InstanceID || restored.InvocationID != fact.InvocationID ||
			restored.DecisionID != fact.DecisionID || restored.CallID != fact.CallID {
			t.Fatalf("identity = (%s,%s,%s,%s), want (%s,%s,%s,%s)",
				restored.InstanceID, restored.InvocationID, restored.DecisionID, restored.CallID,
				fact.InstanceID, fact.InvocationID, fact.DecisionID, fact.CallID)
		}

		parsed := event.ParseEventMeta(restored.Meta)
		if parsed.EventKey != "evt-1" || parsed.PartitionID != "part-1" ||
			parsed.EventType != event.TypeActionCommand || parsed.EventSummary != "run shell" ||
			parsed.TriggerSource != "model" || parsed.SettleTriggerSource != "settle-1" ||
			parsed.BundleID != "bundle-1" {
			t.Fatalf("parsed frozen keys = %+v, want the frozen keys read into their fields", parsed)
		}
		if got := parsed.PassThrough[event.MetaPrefix+"trace"]; got != "trace-9" {
			t.Fatalf("pass-through entry = %q, want %q", got, "trace-9")
		}
		if len(parsed.PassThrough) != 1 {
			t.Fatalf("pass-through entries = %d, want 1", len(parsed.PassThrough))
		}
		if got := parsed.Unknown["custom_key"]; got != "kept" {
			t.Fatalf("unknown entry = %q, want %q", got, "kept")
		}
		if len(parsed.Unknown) != 1 {
			t.Fatalf("unknown entries = %d, want 1", len(parsed.Unknown))
		}
		if len(parsed.PassThrough)+len(parsed.Unknown)+7 != len(restored.Meta) {
			t.Fatalf("classified %d entries of %d, want every entry classified once",
				len(parsed.PassThrough)+len(parsed.Unknown)+7, len(restored.Meta))
		}
	})

	t.Run("call id dedup scope", func(t *testing.T) {
		admission := task.NewMemoryAdmission()
		first := task.CallIdentity{
			InstanceID:   "instance-a",
			InvocationID: "inv-1",
			DecisionID:   "decision-1",
			CallID:       "call-same",
		}
		fresh := admission.Admit(first, "task-a")
		if !fresh.Accepted || fresh.Kind != task.AdmissionFresh || fresh.TaskID != "task-a" || fresh.CallID != "call-same" {
			t.Fatalf("first admission = %+v, want a fresh admission of task-a", fresh)
		}
		repeat := admission.Admit(first, "task-b")
		if repeat.Kind != task.AdmissionDedup || repeat.TaskID != "task-a" {
			t.Fatalf("repeat admission = %+v, want a dedup reporting the first task", repeat)
		}

		otherInstance := task.CallIdentity{
			InstanceID:   "instance-b",
			InvocationID: "inv-1",
			DecisionID:   "decision-1",
			CallID:       "call-same",
		}
		if otherInstance.DedupKey() == first.DedupKey() {
			t.Fatalf("dedup keys collide across instances: %q", otherInstance.DedupKey())
		}
		other := admission.Admit(otherInstance, "task-c")
		if other.Kind != task.AdmissionFresh || other.TaskID != "task-c" {
			t.Fatalf("other-instance admission = %+v, want a fresh admission of task-c", other)
		}

		otherDecision := task.CallIdentity{
			InstanceID:   "instance-a",
			InvocationID: "inv-1",
			DecisionID:   "decision-2",
			CallID:       "call-same",
		}
		decisionScoped := admission.Admit(otherDecision, "task-d")
		if decisionScoped.Kind != task.AdmissionFresh || decisionScoped.TaskID != "task-d" {
			t.Fatalf("other-decision admission = %+v, want a fresh admission of task-d", decisionScoped)
		}
	})

	t.Run("commit outcomes stay separate", func(t *testing.T) {
		ctx := context.Background()
		uncertain := false
		carrier := task.NewMemoryCommit(func(task.CommitRecord) bool { return uncertain })

		record := task.CommitRecord{
			MutationID:       "m-1",
			TaskID:           "task-c",
			ExpectedRevision: 0,
			Status:           task.StatusAccepted,
		}
		committed, err := carrier.Commit(ctx, record)
		if committed != task.CommitCommitted || err != nil {
			t.Fatalf("commit = (%v, %v), want (committed, nil)", committed, err)
		}
		if got := carrier.Facts("task-c"); len(got) != 1 {
			t.Fatalf("facts = %d, want 1", len(got))
		}
		if got := carrier.Revision("task-c"); got != 1 {
			t.Fatalf("revision = %d, want 1", got)
		}

		conflict := task.CommitRecord{
			MutationID:       "m-2",
			TaskID:           "task-c",
			ExpectedRevision: 0,
			Status:           task.StatusRunning,
		}
		refused, err := carrier.Commit(ctx, conflict)
		if refused != task.CommitNotCommitted || !errors.Is(err, task.ErrRevisionConflict) {
			t.Fatalf("conflicting commit = (%v, %v), want (not-committed, %v)", refused, err, task.ErrRevisionConflict)
		}
		if got := carrier.Facts("task-c"); len(got) != 1 {
			t.Fatalf("facts = %d after a refused commit, want 1", len(got))
		}

		uncertain = true
		probe := task.CommitRecord{
			MutationID:       "m-3",
			TaskID:           "task-c",
			ExpectedRevision: 1,
			Status:           task.StatusSettled,
		}
		unknown, err := carrier.Commit(ctx, probe)
		if unknown != task.CommitUncertain || err != nil {
			t.Fatalf("uncertain commit = (%v, %v), want (uncertain, nil)", unknown, err)
		}
		if got := carrier.Facts("task-c"); len(got) != 1 {
			t.Fatalf("facts = %d after an uncertain commit, want 1", len(got))
		}
		if got := carrier.Revision("task-c"); got != 1 {
			t.Fatalf("revision = %d after an uncertain commit, want 1", got)
		}

		uncertain = false
		reconciled, err := carrier.Commit(ctx, probe)
		if reconciled != task.CommitCommitted || err != nil {
			t.Fatalf("reconciled commit = (%v, %v), want (committed, nil)", reconciled, err)
		}
		if got := carrier.Facts("task-c"); len(got) != 2 {
			t.Fatalf("facts = %d after the repeat lands, want 2", len(got))
		}

		altered := probe
		altered.Status = task.StatusFailed
		sameIdentity, err := carrier.Commit(ctx, altered)
		if sameIdentity != task.CommitNotCommitted || !errors.Is(err, task.ErrMutationConflict) {
			t.Fatalf("same identity with other content = (%v, %v), want (not-committed, %v)", sameIdentity, err, task.ErrMutationConflict)
		}
		if got := carrier.Facts("task-c"); len(got) != 2 {
			t.Fatalf("facts = %d after a refused repeat, want 2", len(got))
		}
		seen := map[task.CommitResult]bool{committed: true, refused: true, unknown: true}
		if len(seen) != 3 {
			t.Fatalf("observed %d distinct commit outcomes, want 3", len(seen))
		}
	})

	t.Run("admission stands beside commit and delivery", func(t *testing.T) {
		ctx := context.Background()
		admission := task.NewMemoryAdmission()
		receipt := admission.Admit(task.CallIdentity{
			InstanceID:   "instance-a",
			InvocationID: "inv-1",
			DecisionID:   "decision-1",
			CallID:       "call-1",
		}, "task-e")
		before := receipt

		carrier := task.NewMemoryCommit(nil)
		result, err := carrier.Commit(ctx, task.CommitRecord{
			MutationID:       "m-5",
			TaskID:           "task-e",
			ExpectedRevision: 7,
			Status:           task.StatusSettled,
		})
		if result != task.CommitNotCommitted || !errors.Is(err, task.ErrRevisionConflict) {
			t.Fatalf("commit = (%v, %v), want (not-committed, %v)", result, err, task.ErrRevisionConflict)
		}
		if receipt != before {
			t.Fatalf("receipt = %+v, want the admitted receipt untouched by a commit outcome", receipt)
		}
		if !receipt.Accepted || receipt.Kind != task.AdmissionFresh {
			t.Fatalf("receipt = %+v, want an accepted fresh admission", receipt)
		}
		if got := carrier.Outbox("task-e"); len(got) != 0 {
			t.Fatalf("outbox = %d entries after a refused commit, want 0", len(got))
		}
	})

	t.Run("idempotent commit is not delivery", func(t *testing.T) {
		ctx := context.Background()
		carrier := task.NewMemoryCommit(nil)
		notice := task.Notice{
			TaskID: "task-f",
			Round:  1,
			Kind:   task.NoticeSettled,
			Seq:    0,
			Event:  "evt-9",
		}
		envelope := task.NoticeEnvelope{
			Notice:         notice,
			Target:         event.Address{AgentID: "agent-a", SessionID: "session-a"},
			Source:         event.Address{AgentID: "agent-b", SessionID: "session-b"},
			TargetInstance: "instance-a",
		}
		record := task.CommitRecord{
			MutationID:       "m-6",
			TaskID:           "task-f",
			ExpectedRevision: 0,
			Status:           task.StatusSettled,
			Outbox:           []task.NoticeEnvelope{envelope},
		}

		first, err := carrier.Commit(ctx, record)
		if first != task.CommitCommitted || err != nil {
			t.Fatalf("commit = (%v, %v), want (committed, nil)", first, err)
		}
		owed := carrier.Outbox("task-f")
		if len(owed) != 1 || owed[0].Notice != envelope {
			t.Fatalf("outbox = %+v, want the single committed notification owed beside its frozen envelope", owed)
		}
		if owed[0].Delivery != task.DeliveryPending {
			t.Fatalf("delivery = %v, want pending after a commit", owed[0].Delivery)
		}

		again, err := carrier.Commit(ctx, record)
		if again != task.CommitCommitted || err != nil {
			t.Fatalf("repeat commit = (%v, %v), want (committed, nil)", again, err)
		}
		facts := carrier.Facts("task-f")
		if len(facts) != 1 || !reflect.DeepEqual(facts[0], record) {
			t.Fatalf("facts = %+v, want the one stored fact returned by the repeat", facts)
		}
		if got := carrier.Revision("task-f"); got != 1 {
			t.Fatalf("revision = %d after the repeat, want 1", got)
		}
		owed = carrier.Outbox("task-f")
		if len(owed) != 1 || owed[0].Delivery != task.DeliveryPending {
			t.Fatalf("outbox = %+v, want the delivery obligation still open after a repeat commit", owed)
		}
	})

	t.Run("the commit record freezes the notification target", func(t *testing.T) {
		ctx := context.Background()
		carrier := task.NewMemoryCommit(nil)
		notice := task.Notice{TaskID: "task-g", Round: 1, Kind: task.NoticeSettled, Seq: 0, Event: "evt-g"}
		frozen := task.NoticeEnvelope{
			Notice:         notice,
			Target:         event.Address{AgentID: "agent-a", SessionID: "session-a"},
			Source:         event.Address{AgentID: "agent-b", SessionID: "session-b"},
			TargetInstance: "instance-old",
		}
		if result, err := carrier.Commit(ctx, task.CommitRecord{
			MutationID:       "m-7",
			TaskID:           "task-g",
			ExpectedRevision: 0,
			Status:           task.StatusSettled,
			Outbox:           []task.NoticeEnvelope{frozen},
		}); result != task.CommitCommitted || err != nil {
			t.Fatalf("commit = (%v, %v), want (committed, nil)", result, err)
		}

		facts := carrier.Facts("task-g")
		if len(facts) != 1 || len(facts[0].Outbox) != 1 {
			t.Fatalf("facts = %+v, want one fact carrying one notification", facts)
		}
		got := facts[0].Outbox[0]
		if got != frozen {
			t.Fatalf("envelope = %+v, want the envelope stored verbatim beside the transition", got)
		}
		if got.Target.IsZero() || got.Source.IsZero() || got.TargetInstance == "" {
			t.Fatalf("envelope routing = (target %+v, source %+v, instance %q), want all three frozen and non-empty",
				got.Target, got.Source, got.TargetInstance)
		}
		owed := carrier.Outbox("task-g")
		if len(owed) != 1 || owed[0].Notice != frozen {
			t.Fatalf("outbox = %+v, want the owed notification kept beside its original frozen target", owed)
		}
	})
}
