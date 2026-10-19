package execution

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// restartOwnerTool is the named tool stub the current orchestration binding
// holds in its owner tool set, so a restore resolves the task's target against
// the binding rather than a top-level singleton.
type restartOwnerTool struct{ name string }

func (s restartOwnerTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: s.name}
}

// restartMailbox reads the mailbox entries one established site holds, beside
// the read grant the authority records for the reader.
func restartMailbox(t *testing.T, ctx context.Context, store *state.MemoryStore, authority *state.Authority, addr event.Address, instance event.InstanceID) []state.MailboxEntry {
	t.Helper()
	const subject state.Subject = "subject-restart"
	if err := authority.GrantRead(ctx, subject, addr); err != nil {
		t.Fatalf("record the read grant over %s: %v", addr, err)
	}
	session, err := store.Site(ctx, subject, addr, instance)
	if err != nil {
		t.Fatalf("read the site %s / %s: %v", addr, instance, err)
	}
	return session.Mailbox
}

// TestHandoffRoutingSurvivesRestart checks that one handoff and the notification attribution survive a task-layer restart through the formal chain.
// - a started notification keeps the old instance its envelope froze and is never re-routed to the new one
// - a future notification freezes the new target and target instance the stored handoff names
// - the attribution comes from the persisted admission material beside a foreign call frame or no frame at all
// - a repeated transport records no second fact and no second mailbox entry
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestHandoffRoutingSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
	oldTarget := event.Address{AgentID: "agent-recv", SessionID: "session-old"}
	newTarget := event.Address{AgentID: "agent-recv", SessionID: "session-new"}
	const sourceInstance event.InstanceID = "instance-source"
	const targetInstance event.InstanceID = "instance-target"
	const invocation event.InvocationID = "inv-persisted"

	openStore := func() (*state.Authority, *state.MemoryStore) {
		authority := state.NewAuthority()
		store := state.NewMemoryStore(authority)
		for _, site := range []struct {
			addr     event.Address
			instance event.InstanceID
		}{{oldTarget, sourceInstance}, {newTarget, targetInstance}} {
			if _, err := store.Establish(ctx, site.addr, site.instance); err != nil {
				t.Fatalf("establish %s / %s: %v", site.addr, site.instance, err)
			}
		}
		return authority, store
	}
	deliver := func(store *state.MemoryStore, attempts *[]task.Envelope) task.EnvelopeDeliverer {
		return func(ctx context.Context, envelope task.Envelope) (task.DeliveryAcceptance, error) {
			*attempts = append(*attempts, envelope)
			if err := store.TrackDelivery(ctx, envelope.Notice.TaskID, task.HandoffTarget{Target: envelope.Target, TargetInstance: envelope.TargetInstance}); err != nil {
				return task.DeliveryRejected, err
			}
			if err := store.AcceptNotice(ctx, envelope.Notice); err != nil {
				return task.DeliveryPending, err
			}
			return task.DeliveryAccepted, nil
		}
	}
	authority1, store1 := openStore()
	authority2, store2 := openStore()

	tier, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("OpenTier: %v", err)
	}
	durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("NewDurableCommit: %v", err)
	}
	var attempts1 []task.Envelope
	registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
	manager := task.NewMemoryManager(registry, nil, task.WithEnvelopeDeliverer(deliver(store1, &attempts1)))
	spec := task.Spec{
		CallID:       "call-restart",
		DedupKey:     task.DedupKey("inst-source\x00inv-persisted\x00dec-1\x00call-restart"),
		InvocationID: invocation,
		InstanceID:   sourceInstance,
		Description:  "hand the report over",
		Source:       event.Address{AgentID: "agent-src", SessionID: "session-src"},
		Target:       oldTarget,
		Execution:    "serialized",
	}
	receipt, err := manager.Admit(ctx, spec)
	if err != nil || !receipt.Accepted || receipt.Kind != task.AdmissionFresh {
		t.Fatalf("admit = (%+v, %v), want one accepted fresh admission", receipt, err)
	}
	taskID := receipt.TaskID
	if result, err := registry.Advance(ctx, taskID, task.StatusRunning, task.MutationID("run:"+string(taskID)), nil); result != task.CommitCommitted || err != nil {
		t.Fatalf("advance running = (%v, %v), want the task running", result, err)
	}
	before, err := registry.IssueNotice(ctx, taskID, task.NoticeProgress, "evt-before")
	if err != nil {
		t.Fatalf("issue the started notification: %v", err)
	}
	if before.Target != oldTarget || before.TargetInstance != sourceInstance {
		t.Fatalf("envelope = %+v, want the started notification frozen beside %v / %s", before, oldTarget, sourceInstance)
	}
	if result, err := registry.Advance(ctx, taskID, task.StatusProgress, task.MutationID("progress:"+string(taskID)+":1"), []task.Envelope{before}); result != task.CommitCommitted || err != nil {
		t.Fatalf("advance progress = (%v, %v), want the notification committed beside its frozen routing", result, err)
	}
	if err := manager.Handoff(ctx, taskID, task.HandoffTarget{Target: newTarget, TargetInstance: targetInstance}); err != nil {
		t.Fatalf("handoff: %v", err)
	}
	wantRecord := task.HandoffRecord{
		TaskID:         taskID,
		PreviousTarget: oldTarget,
		SourceInstance: sourceInstance,
		Target:         newTarget,
		TargetInstance: targetInstance,
	}
	if record, err := manager.HandoffOf(ctx, taskID); err != nil || record != wantRecord {
		t.Fatalf("handoff record = (%+v, %v), want the recorded link %+v", record, err, wantRecord)
	}

	transport := task.WithCallFrame(ctx, task.CallFrame{
		Identity: task.CallIdentity{InstanceID: "instance-foreign", InvocationID: "inv-transport", DecisionID: "dec-transport", CallID: "call-foreign"},
		Address:  oldTarget,
	})
	if err := manager.Flush(transport, taskID); err != nil {
		t.Fatalf("flush beside the foreign call frame: %v", err)
	}
	if len(attempts1) != 1 || attempts1[0] != before {
		t.Fatalf("attempts = %+v, want the started notification offered beside its frozen routing", attempts1)
	}
	if got := restartMailbox(t, ctx, store1, authority1, oldTarget, sourceInstance); len(got) != 1 || got[0].InvocationID != invocation {
		t.Fatalf("old-instance mailbox = %+v, want the accepted notification attributed to %s beside the foreign transport frame", got, invocation)
	}
	if got := restartMailbox(t, ctx, store1, authority1, newTarget, targetInstance); len(got) != 0 {
		t.Fatalf("new-instance mailbox = %+v, want the started notification never delivered to the new instance", got)
	}

	reopened, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen OpenTier: %v", err)
	}
	restored, err := state.NewDurableCommit(ledger.TierFactLog(reopened), nil)
	if err != nil {
		t.Fatalf("reopen NewDurableCommit: %v", err)
	}
	chain, err := restored.Records(ctx, taskID)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(chain) != 4 {
		t.Fatalf("chain = %d links, want the admission, the run, the progress and the handoff facts", len(chain))
	}
	bindings := state.NewBindings()
	if err := bindings.Hold(state.StatePlan{
		Agents: map[event.AgentID]state.AgentBinding{
			spec.Source.AgentID: {Tools: []trpctool.Tool{restartOwnerTool{name: string(spec.Target.AgentID)}}},
		},
	}); err != nil {
		t.Fatalf("hold the binding: %v", err)
	}
	var attempts2 []task.Envelope
	registry2 := task.NewMemoryRegistry(restored, task.NewMemoryAdmission())
	manager2 := task.NewMemoryManager(registry2, nil, task.WithEnvelopeDeliverer(deliver(store2, &attempts2)))
	report, err := registry2.Restore(ctx, chain, state.NewBindingOwners(bindings))
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(report.Tasks) != 1 || report.Tasks[0].TaskID != taskID || !report.Tasks[0].Recovered || report.Tasks[0].Refused != nil {
		t.Fatalf("restore report = %+v, want the task rebuilt from the chain beside a resolved owner", report)
	}

	if record, err := manager2.HandoffOf(ctx, taskID); err != nil || record != wantRecord {
		t.Fatalf("handoff record after the restart = (%+v, %v), want the persisted move %+v", record, err, wantRecord)
	}
	after, err := registry2.IssueNotice(ctx, taskID, task.NoticeProgress, "evt-after")
	if err != nil {
		t.Fatalf("issue the future notification: %v", err)
	}
	if after.Target != newTarget || after.TargetInstance != targetInstance {
		t.Fatalf("envelope = %+v, want the future notification frozen beside %v / %s", after, newTarget, targetInstance)
	}
	if after.Source != before.Source {
		t.Fatalf("source = %+v, want the frozen source %+v kept beside the new target", after.Source, before.Source)
	}
	if result, err := registry2.Advance(ctx, taskID, task.StatusProgress, task.MutationID("progress:"+string(taskID)+":2"), []task.Envelope{after}); result != task.CommitCommitted || err != nil {
		t.Fatalf("advance progress = (%v, %v), want the future notification committed beside its frozen routing", result, err)
	}

	if err := manager2.Flush(ctx, taskID); err != nil {
		t.Fatalf("flush after the restart: %v", err)
	}
	if len(attempts2) != 2 || attempts2[0] != before || attempts2[1] != after {
		t.Fatalf("attempts = %+v, want the started notification beside its frozen routing and the future one beside the new target", attempts2)
	}
	if got := restartMailbox(t, ctx, store2, authority2, oldTarget, sourceInstance); len(got) != 1 || got[0].Notice != before.Notice || got[0].InvocationID != invocation {
		t.Fatalf("old-instance mailbox = %+v, want the started notification kept beside %s attributed to %s", got, sourceInstance, invocation)
	}
	if got := restartMailbox(t, ctx, store2, authority2, newTarget, targetInstance); len(got) != 1 || got[0].Notice != after.Notice || got[0].InvocationID != invocation {
		t.Fatalf("new-instance mailbox = %+v, want the future notification delivered to %s attributed to %s", got, targetInstance, invocation)
	}

	if err := manager2.Flush(ctx, taskID); err != nil {
		t.Fatalf("repeated flush: %v", err)
	}
	if len(attempts2) != 2 {
		t.Fatalf("repeated flush offered %d further notifications, want none beyond the two already accepted", len(attempts2)-2)
	}
	duplicate := deliver(store2, &attempts2)
	for _, envelope := range []task.Envelope{before, after} {
		if outcome, err := duplicate(transport, envelope); outcome != task.DeliveryAccepted || err != nil {
			t.Fatalf("duplicate transport = (%v, %v), want the repeated delivery recognized as the same notification", outcome, err)
		}
	}
	if got := restartMailbox(t, ctx, store2, authority2, oldTarget, sourceInstance); len(got) != 1 || got[0].InvocationID != invocation {
		t.Fatalf("old-instance mailbox = %+v, want one entry for the one notification identity", got)
	}
	if got := restartMailbox(t, ctx, store2, authority2, newTarget, targetInstance); len(got) != 1 || got[0].InvocationID != invocation {
		t.Fatalf("new-instance mailbox = %+v, want one entry for the one notification identity", got)
	}
	if facts := restored.Facts(taskID); len(facts) != 5 {
		t.Fatalf("stored %d facts after the repeated transports, want the five the chain carries", len(facts))
	}

	if err := manager2.Handoff(ctx, taskID, task.HandoffTarget{Target: newTarget, TargetInstance: targetInstance}); err != nil {
		t.Fatalf("repeated handoff: %v", err)
	}
	if facts := restored.Facts(taskID); len(facts) != 5 {
		t.Fatalf("stored %d facts after the repeated handoff, want no second handoff fact", len(facts))
	}
}
