package history

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// stubTool is the named tool stub one owner's tool set resolves restored
// targets against.
type stubTool struct{ name string }

func (s stubTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: s.name}
}

// settleRecordsOf returns the settled notification of one admitted task beside
// the settle mutation that carries it: the task identity is minted at admission
// and never enumerable, so every round trip builds its records from the receipt
// of its own admission.
func settleRecordsOf(taskID event.TaskID, spec task.Spec) (task.Notice, task.CommitRecord) {
	notice := task.Notice{
		TaskID: taskID,
		Round:  1,
		Kind:   task.NoticeSettled,
		Seq:    0,
		Event:  "evt-settle-1",
	}
	return notice, task.CommitRecord{
		MutationID:       task.MutationID("settle:" + string(taskID)),
		TaskID:           taskID,
		ExpectedRevision: 1,
		Status:           task.StatusSettled,
		Outbox: []task.Envelope{{
			Notice:         notice,
			Target:         spec.Target,
			Source:         spec.Source,
			TargetInstance: spec.InstanceID,
		}},
	}
}

// TestDurableTaskRoundTrip proves the durable task commit, the idempotent acceptance and the delivery replay:
// - 持久提交往返: one commit round trip across a restart rebuilds the registry
// - 重复提交幂等不新增: a repeated commit adds no second fact and delivers nothing
// - 未确认投递重放并定位 owner: an unconfirmed delivery replays and locates its owner
// - 目标不存在明确拒绝: a target that is gone is refused by name with its obligation kept
// - 无需更换数据库: events and task facts round trip on one database
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestDurableTaskRoundTrip(t *testing.T) {
	ctx := context.Background()
	owner := event.AgentID("agent-b")
	target := event.AgentID("tool-c")
	spec := task.Spec{
		CallID:       "call-1",
		DedupKey:     "inst-1\x00inv-1\x00dec-1\x00call-1",
		InvocationID: "inv-1",
		InstanceID:   "inst-1",
		Description:  "tool-c",
		Source:       event.Address{AgentID: owner, SessionID: "s1"},
		Target:       event.Address{AgentID: target, SessionID: "s1"},
		Execution:    "serialized",
	}
	holdTarget := func(t *testing.T, names ...string) *state.Bindings {
		t.Helper()
		tools := make([]trpctool.Tool, 0, len(names))
		for _, name := range names {
			tools = append(tools, stubTool{name: name})
		}
		bindings := state.NewBindings()
		if err := bindings.Hold(state.StatePlan{
			Agents: map[event.AgentID]state.AgentBinding{owner: {Tools: tools}},
		}); err != nil {
			t.Fatalf("Hold(%v): %v", names, err)
		}
		return bindings
	}

	t.Run("G2.1 持久提交往返: durable commit round trip across a restart", func(t *testing.T) {
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
		tier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("OpenTier: %v", err)
		}
		durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
		if err != nil {
			t.Fatalf("NewDurableCommit: %v", err)
		}
		registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
		receipt, err := registry.Register(ctx, spec)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		if !strings.HasPrefix(string(receipt.TaskID), "task-") || receipt.Kind != task.AdmissionFresh {
			t.Fatalf("Register receipt = %+v, want one fresh admission of a task identity", receipt)
		}
		_, settle := settleRecordsOf(receipt.TaskID, spec)
		if landed, err := durable.Records(ctx, receipt.TaskID); err != nil || len(landed) != 1 || landed[0].Spec != spec {
			t.Fatalf("chain after the first submission = %+v (%v), want one admission link carrying %+v", landed, err, spec)
		}
		result, err := registry.Advance(ctx, receipt.TaskID, task.StatusSettled, settle.MutationID, settle.Outbox)
		if err != nil || result != task.CommitCommitted {
			t.Fatalf("Advance settle = %v, %v, want committed", result, err)
		}

		reopened, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen OpenTier: %v", err)
		}
		restored, err := state.NewDurableCommit(ledger.TierFactLog(reopened), nil)
		if err != nil {
			t.Fatalf("reopen NewDurableCommit: %v", err)
		}
		chain, err := restored.Records(ctx, receipt.TaskID)
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		if len(chain) != 2 {
			t.Fatalf("Records = %d links, want 2 (admit + settle)", len(chain))
		}
		if chain[0].Spec != spec {
			t.Fatalf("admission link carries spec %+v, want %+v", chain[0].Spec, spec)
		}
		registry2 := task.NewMemoryRegistry(restored, task.NewMemoryAdmission())
		report, err := registry2.Restore(ctx, chain, state.NewBindingOwners(holdTarget(t, string(target))))
		if err != nil {
			t.Fatalf("Restore: %v", err)
		}
		if len(report.Tasks) != 1 {
			t.Fatalf("Restore report = %d outcomes, want 1", len(report.Tasks))
		}
		outcome := report.Tasks[0]
		if !outcome.Recovered || outcome.Owner != owner || outcome.Refused != nil {
			t.Fatalf("Restore outcome = %+v, want recovered at owner %s", outcome, owner)
		}
		looked, status, err := registry2.Lookup(ctx, receipt.TaskID)
		if err != nil {
			t.Fatalf("Lookup after restore: %v", err)
		}
		if status != task.StatusSettled {
			t.Fatalf("restored status = %v, want settled", status)
		}
		if looked != spec {
			t.Fatalf("restored spec = %+v, want %+v", looked, spec)
		}
		if revision := restored.Revision(receipt.TaskID); revision != 2 {
			t.Fatalf("restored revision = %d, want 2", revision)
		}
		if facts := restored.Facts(receipt.TaskID); len(facts) != 2 {
			t.Fatalf("restored facts = %d, want 2", len(facts))
		}
		owed := restored.Outbox(receipt.TaskID)
		if len(owed) != 1 || owed[0].Delivery != task.DeliveryPending {
			t.Fatalf("restored outbox = %+v, want one pending delivery", owed)
		}
	})

	t.Run("G2.2 重复提交幂等不新增: repeated commit returns the stored fact and delivers nothing", func(t *testing.T) {
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
		tier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("OpenTier: %v", err)
		}
		durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
		if err != nil {
			t.Fatalf("NewDurableCommit: %v", err)
		}
		registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
		receipt, err := registry.Register(ctx, spec)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		_, settle := settleRecordsOf(receipt.TaskID, spec)
		if landed, err := durable.Records(ctx, receipt.TaskID); err != nil || len(landed) != 1 || landed[0].Spec != spec {
			t.Fatalf("chain after the first submission = %+v (%v), want one admission link carrying %+v", landed, err, spec)
		}
		if result, err := registry.Advance(ctx, receipt.TaskID, task.StatusSettled, settle.MutationID, settle.Outbox); err != nil || result != task.CommitCommitted {
			t.Fatalf("Advance settle = %v, %v, want committed", result, err)
		}
		if facts := durable.Facts(receipt.TaskID); len(facts) != 2 {
			t.Fatalf("facts before repeat = %d, want 2", len(facts))
		}

		result, err := durable.Commit(ctx, settle)
		if err != nil || result != task.CommitCommitted {
			t.Fatalf("repeated Commit = %v, %v, want committed without a new fact", result, err)
		}
		if facts := durable.Facts(receipt.TaskID); len(facts) != 2 {
			t.Fatalf("facts after repeat = %d, want 2 (no second fact)", len(facts))
		}
		owed := durable.Outbox(receipt.TaskID)
		if len(owed) != 1 || owed[0].Delivery != task.DeliveryPending {
			t.Fatalf("outbox after repeat = %+v, want one still pending (idempotent hit is not delivery)", owed)
		}

		conflicting := settle
		conflicting.Status = task.StatusFailed
		result, err = durable.Commit(ctx, conflicting)
		if result != task.CommitNotCommitted || !errors.Is(err, task.ErrMutationConflict) {
			t.Fatalf("conflicting Commit = %v, %v, want not committed with ErrMutationConflict", result, err)
		}

		chain, err := durable.Records(ctx, receipt.TaskID)
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		owners := state.NewBindingOwners(holdTarget(t, string(target)))
		restoring := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
		first, err := restoring.Restore(ctx, chain, owners)
		if err != nil {
			t.Fatalf("Restore: %v", err)
		}
		again, err := restoring.Restore(ctx, chain, owners)
		if err != nil {
			t.Fatalf("Restore repeat: %v", err)
		}
		if !first.Tasks[0].Recovered || again.Tasks[0].Recovered {
			t.Fatalf("restore outcomes = first %+v again %+v, want recovered then idempotent", first.Tasks[0], again.Tasks[0])
		}
		repeat, err := restoring.Register(ctx, spec)
		if err != nil {
			t.Fatalf("Register after restore: %v", err)
		}
		if repeat.Kind != task.AdmissionDedup || repeat.TaskID != receipt.TaskID {
			t.Fatalf("repeat receipt = %+v, want the dedup of %s", repeat, receipt.TaskID)
		}
	})

	t.Run("G2.3 未确认投递重放并定位 owner: unconfirmed replay locates its owner and advances only behind acceptance", func(t *testing.T) {
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
		tier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("OpenTier: %v", err)
		}
		durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
		if err != nil {
			t.Fatalf("NewDurableCommit: %v", err)
		}
		registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
		receipt, err := registry.Register(ctx, spec)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		notice, settle := settleRecordsOf(receipt.TaskID, spec)
		if landed, err := durable.Records(ctx, receipt.TaskID); err != nil || len(landed) != 1 || landed[0].Spec != spec {
			t.Fatalf("chain after the first submission = %+v (%v), want one admission link carrying %+v", landed, err, spec)
		}
		if result, err := registry.Advance(ctx, receipt.TaskID, task.StatusSettled, settle.MutationID, settle.Outbox); err != nil || result != task.CommitCommitted {
			t.Fatalf("Advance settle = %v, %v, want committed", result, err)
		}

		reopened, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen OpenTier: %v", err)
		}
		restored, err := state.NewDurableCommit(ledger.TierFactLog(reopened), nil)
		if err != nil {
			t.Fatalf("reopen NewDurableCommit: %v", err)
		}
		owed := restored.Outbox(receipt.TaskID)
		if len(owed) != 1 || owed[0].Delivery != task.DeliveryPending {
			t.Fatalf("reopened outbox = %+v, want one unconfirmed delivery", owed)
		}
		pending := []memory.DeliveryRecord{{
			TaskID:         string(owed[0].Notice.Notice.TaskID),
			Round:          owed[0].Notice.Notice.Round,
			Kind:           string(owed[0].Notice.Notice.Kind),
			Seq:            owed[0].Notice.Notice.Seq,
			Event:          owed[0].Notice.Notice.Event,
			Target:         owed[0].Notice.Target,
			Source:         owed[0].Notice.Source,
			TargetInstance: owed[0].Notice.TargetInstance,
		}}

		store := state.NewMemoryStore(state.NewAuthority())
		if _, err := store.Establish(ctx, spec.Target, spec.InstanceID); err != nil {
			t.Fatalf("Establish: %v", err)
		}
		if err := store.TrackDelivery(ctx, receipt.TaskID, task.HandoffTarget{Target: spec.Target, TargetInstance: spec.InstanceID}); err != nil {
			t.Fatalf("TrackDelivery: %v", err)
		}
		sink := func(ctx context.Context, rec memory.DeliveryRecord) error {
			admitted := task.Notice{
				TaskID: event.TaskID(rec.TaskID),
				Round:  rec.Round,
				Kind:   task.NoticeKind(rec.Kind),
				Seq:    rec.Seq,
				Event:  rec.Event,
			}
			if err := store.AcceptNotice(ctx, admitted); err != nil {
				return err
			}
			_, err := restored.Accept(ctx, admitted)
			return err
		}
		replay := memory.NewReplay()
		report, err := replay.Replay(ctx, pending, state.NewBindingOwners(holdTarget(t, string(target))).OwnerOf, sink)
		if err != nil {
			t.Fatalf("Replay: %v", err)
		}
		if len(report.Outcomes) != 1 {
			t.Fatalf("Replay report = %d outcomes, want 1", len(report.Outcomes))
		}
		outcome := report.Outcomes[0]
		if !outcome.Delivered || outcome.Owner != owner || outcome.Refused != nil {
			t.Fatalf("replay outcome = %+v, want delivered at owner %s", outcome, owner)
		}
		after := restored.Outbox(receipt.TaskID)
		if len(after) != 1 || after[0].Delivery != task.DeliveryAccepted {
			t.Fatalf("outbox after acceptance = %+v, want accepted", after)
		}
		accepted, err := store.Establish(ctx, spec.Target, spec.InstanceID)
		if err != nil {
			t.Fatalf("Establish after replay: %v", err)
		}
		if len(accepted.Mailbox) != 1 {
			t.Fatalf("mailbox holds %d entries, want 1", len(accepted.Mailbox))
		}

		again, err := replay.Replay(ctx, pending, state.NewBindingOwners(holdTarget(t, string(target))).OwnerOf, sink)
		if err != nil {
			t.Fatalf("Replay again: %v", err)
		}
		if !again.Outcomes[0].Skipped || again.Outcomes[0].Delivered {
			t.Fatalf("replay again outcome = %+v, want skipped", again.Outcomes[0])
		}
		if err := store.AcceptNotice(ctx, notice); err != nil {
			t.Fatalf("AcceptNotice repeat: %v", err)
		}
		accepted, err = store.Establish(ctx, spec.Target, spec.InstanceID)
		if err != nil {
			t.Fatalf("Establish after repeat: %v", err)
		}
		if len(accepted.Mailbox) != 1 {
			t.Fatalf("mailbox holds %d entries after replay, want 1 (no second fact)", len(accepted.Mailbox))
		}
	})

	t.Run("G2.4 目标已移除明确拒绝: removed target refused by name with its obligation kept", func(t *testing.T) {
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
		tier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("OpenTier: %v", err)
		}
		durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
		if err != nil {
			t.Fatalf("NewDurableCommit: %v", err)
		}
		registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
		receipt, err := registry.Register(ctx, spec)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		_, settle := settleRecordsOf(receipt.TaskID, spec)
		if landed, err := durable.Records(ctx, receipt.TaskID); err != nil || len(landed) != 1 || landed[0].Spec != spec {
			t.Fatalf("chain after the first submission = %+v (%v), want one admission link carrying %+v", landed, err, spec)
		}
		if result, err := registry.Advance(ctx, receipt.TaskID, task.StatusSettled, settle.MutationID, settle.Outbox); err != nil || result != task.CommitCommitted {
			t.Fatalf("Advance settle = %v, %v, want committed", result, err)
		}
		chain, err := durable.Records(ctx, receipt.TaskID)
		if err != nil {
			t.Fatalf("Records: %v", err)
		}

		removed := holdTarget(t, "tool-d")
		restoring := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
		report, err := restoring.Restore(ctx, chain, state.NewBindingOwners(removed))
		if err != nil {
			t.Fatalf("Restore: %v", err)
		}
		if !errors.Is(report.Tasks[0].Refused, task.ErrTargetRemoved) {
			t.Fatalf("restore refusal = %v, want ErrTargetRemoved", report.Tasks[0].Refused)
		}
		if _, status, err := restoring.Lookup(ctx, receipt.TaskID); err != nil || status != task.StatusSettled {
			t.Fatalf("Lookup after refusal = %v, %v, want the restored chain kept", status, err)
		}
		if kept, _, err := restoring.Lookup(ctx, receipt.TaskID); err != nil || kept.CallID != spec.CallID {
			t.Fatalf("restored chain material = %+v, %v, want kept", kept, err)
		}

		owed := durable.Outbox(receipt.TaskID)
		if len(owed) != 1 || owed[0].Delivery != task.DeliveryPending {
			t.Fatalf("outbox before replay = %+v, want one pending", owed)
		}
		pending := []memory.DeliveryRecord{{
			TaskID:         string(owed[0].Notice.Notice.TaskID),
			Round:          owed[0].Notice.Notice.Round,
			Kind:           string(owed[0].Notice.Notice.Kind),
			Seq:            owed[0].Notice.Notice.Seq,
			Event:          owed[0].Notice.Notice.Event,
			Target:         owed[0].Notice.Target,
			Source:         owed[0].Notice.Source,
			TargetInstance: owed[0].Notice.TargetInstance,
		}}
		store := state.NewMemoryStore(state.NewAuthority())
		if _, err := store.Establish(ctx, spec.Target, spec.InstanceID); err != nil {
			t.Fatalf("Establish: %v", err)
		}
		if err := store.TrackDelivery(ctx, receipt.TaskID, task.HandoffTarget{Target: spec.Target, TargetInstance: spec.InstanceID}); err != nil {
			t.Fatalf("TrackDelivery: %v", err)
		}
		sink := func(ctx context.Context, rec memory.DeliveryRecord) error {
			t.Fatalf("sink must not run for a refused delivery")
			return nil
		}
		replay := memory.NewReplay()
		replayReport, err := replay.Replay(ctx, pending, state.NewBindingOwners(removed).OwnerOf, sink)
		if err != nil {
			t.Fatalf("Replay: %v", err)
		}
		if !errors.Is(replayReport.Outcomes[0].Refused, task.ErrTargetRemoved) {
			t.Fatalf("replay refusal = %v, want ErrTargetRemoved", replayReport.Outcomes[0].Refused)
		}
		if replayReport.Outcomes[0].Delivered {
			t.Fatalf("replay outcome = %+v, want nothing delivered", replayReport.Outcomes[0])
		}
		after := durable.Outbox(receipt.TaskID)
		if len(after) != 1 || after[0].Delivery != task.DeliveryPending {
			t.Fatalf("outbox after refusal = %+v, want the obligation kept pending", after)
		}
		empty, err := store.Establish(ctx, spec.Target, spec.InstanceID)
		if err != nil {
			t.Fatalf("Establish after refusal: %v", err)
		}
		if len(empty.Mailbox) != 0 {
			t.Fatalf("mailbox holds %d entries, want 0 for a refused delivery", len(empty.Mailbox))
		}
	})

	t.Run("G2.5 无需更换数据库: events and task facts round trip on one database", func(t *testing.T) {
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
		tier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("OpenTier: %v", err)
		}
		ref := ledger.OriginalRef{Key: "d5a1", Owner: spec.Source, Format: "text/plain", RetentionPolicy: "keep"}
		if err := tier.Originals.Put(ctx, ref, []byte("原文内容\n")); err != nil {
			t.Fatalf("Originals.Put: %v", err)
		}
		eventBody := []byte("{\"key\":\"e7c2\",\"text\":\"事件体\"}\n")
		if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{
			PartitionID: 1, WindowTS: 700, Seq: 1, EventKey: "e7c2", EventTime: 7, Body: eventBody,
		}); err != nil {
			t.Fatalf("StoreEvent: %v", err)
		}
		durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
		if err != nil {
			t.Fatalf("NewDurableCommit: %v", err)
		}
		registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
		receipt, err := registry.Register(ctx, spec)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		_, settle := settleRecordsOf(receipt.TaskID, spec)
		if landed, err := durable.Records(ctx, receipt.TaskID); err != nil || len(landed) != 1 || landed[0].Spec != spec {
			t.Fatalf("chain after the first submission = %+v (%v), want one admission link carrying %+v", landed, err, spec)
		}
		if result, err := registry.Advance(ctx, receipt.TaskID, task.StatusSettled, settle.MutationID, settle.Outbox); err != nil || result != task.CommitCommitted {
			t.Fatalf("Advance settle = %v, %v, want committed", result, err)
		}

		reopened, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen OpenTier: %v", err)
		}
		if reopened.Capabilities != tier.Capabilities {
			t.Fatalf("reopened capabilities = %+v, want %+v", reopened.Capabilities, tier.Capabilities)
		}
		recalled := reopened.Originals.Get(ctx, ref.Key)
		if recalled.Outcome != ledger.RecallFound || string(recalled.Body) != "原文内容\n" {
			t.Fatalf("reopened original = outcome %v body %q, want the stored bytes", recalled.Outcome, recalled.Body)
		}
		stored, held, err := reopened.Segments.GetEvent(ctx, "e7c2")
		if err != nil || !held || string(stored.Body) != string(eventBody) {
			t.Fatalf("reopened event = held %v body %q, %v, want the committed event", held, stored.Body, err)
		}
		restored, err := state.NewDurableCommit(ledger.TierFactLog(reopened), nil)
		if err != nil {
			t.Fatalf("reopen NewDurableCommit: %v", err)
		}
		chain, err := restored.Records(ctx, receipt.TaskID)
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		registry2 := task.NewMemoryRegistry(restored, task.NewMemoryAdmission())
		report, err := registry2.Restore(ctx, chain, state.NewBindingOwners(holdTarget(t, string(target))))
		if err != nil {
			t.Fatalf("Restore: %v", err)
		}
		if !report.Tasks[0].Recovered || report.Tasks[0].Refused != nil {
			t.Fatalf("restore outcome = %+v, want recovered without a refusal", report.Tasks[0])
		}
		if _, status, err := registry2.Lookup(ctx, receipt.TaskID); err != nil || status != task.StatusSettled {
			t.Fatalf("restored status = %v, %v, want settled", status, err)
		}
	})
}
