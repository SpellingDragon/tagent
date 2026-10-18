package acceptance

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// scenarioNamedTool is the named tool one restored target resolves against.
type scenarioNamedTool struct{ name string }

// Declaration reports the declaration of the tool.
func (s scenarioNamedTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: s.name}
}

// scenarioOffers records the notifications one deliverer was offered.
type scenarioOffers struct {
	mu      sync.Mutex
	offered []task.NoticeEnvelope
}

// deliver records one offered envelope.
func (o *scenarioOffers) deliver(ctx context.Context, envelope task.NoticeEnvelope) (task.DeliveryAcceptance, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.offered = append(o.offered, envelope)
	return task.DeliveryPending, nil
}

// snapshot reports the envelopes offered so far.
func (o *scenarioOffers) snapshot() []task.NoticeEnvelope {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]task.NoticeEnvelope, len(o.offered))
	copy(out, o.offered)
	return out
}

// scenarioSettleSpec is the task one durable drive admits and settles.
func scenarioSettleSpec() task.Spec {
	return task.Spec{
		CallID:       "call-1",
		DedupKey:     task.DedupKey("inst-1\x00inv-1\x00dec-1\x00call-1"),
		InvocationID: "inv-1",
		InstanceID:   "inst-1",
		Description:  "tool-c",
		Source:       event.Address{AgentID: "agent-b", SessionID: "s1"},
		Target:       event.Address{AgentID: "tool-c", SessionID: "s1"},
		Execution:    "serialized",
	}
}

// scenarioSettleRecord is the settle mutation of the task scenarioSettleSpec
// admits, beside the notification it owes.
func scenarioSettleRecord() task.CommitRecord {
	spec := scenarioSettleSpec()
	return task.CommitRecord{
		MutationID:       "settle:task-1",
		TaskID:           "task-1",
		ExpectedRevision: 1,
		Status:           task.StatusSettled,
		Outbox: []task.NoticeEnvelope{{
			Notice: task.Notice{
				TaskID: "task-1",
				Round:  1,
				Kind:   task.NoticeSettled,
				Seq:    0,
				Event:  "evt-settle-1",
			},
			Target:         spec.Target,
			Source:         spec.Source,
			TargetInstance: spec.InstanceID,
		}},
	}
}

// scenarioTargetOwners holds the target tool of the durable drive so the
// restore resolves it.
func scenarioTargetOwners(t *testing.T) *state.BindingOwners {
	t.Helper()
	bindings := state.NewBindings()
	if err := bindings.Hold(state.StatePlan{
		Agents: map[event.AgentID]state.AgentBinding{
			"agent-b": {Tools: []trpctool.Tool{scenarioNamedTool{name: "tool-c"}}},
		},
	}); err != nil {
		t.Fatalf("hold the owner binding: %v", err)
	}
	return state.NewBindingOwners(bindings)
}

// TestScenario06DurableCommitAndReplay drives one task through the durable fact log across a restart.
// - the admission and its settle never conflict, and a repeated submission returns the stored fact
// - a repeated submission never reports the owed notification as delivered
// - the delivery the restart found unconfirmed replays, and the rebuilt registry reads the same task back
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestScenario06DurableCommitAndReplay(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S06")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}

	spec := scenarioSettleSpec()
	settle := scenarioSettleRecord()
	taskID := settle.TaskID
	cfg := memory.StorageConfig{Type: memory.StorageLocalFile, Root: t.TempDir()}
	tier, err := memory.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("open the storage tier: %v", err)
	}
	durable, err := state.NewDurableCommit(memory.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("open the durable commit: %v", err)
	}
	registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
	receipt, err := registry.Register(ctx, spec)
	if err != nil {
		t.Fatalf("register the task: %v", err)
	}
	if receipt.TaskID != taskID || receipt.Kind != task.AdmissionFresh {
		t.Fatalf("register receipt = %+v, want the fresh admission of %s", receipt, taskID)
	}
	result, settleErr := registry.Advance(ctx, taskID, settle.Status, settle.MutationID, settle.Outbox)
	observations["accepted_settled_conflict"] = result == task.CommitNotCommitted && errors.Is(settleErr, task.ErrMutationConflict)

	before := len(durable.Facts(taskID))
	repeated, repeatErr := durable.Commit(ctx, settle)
	observations["duplicate_submit_returns_existing"] = repeated == task.CommitCommitted && repeatErr == nil &&
		len(durable.Facts(taskID)) == before
	delivered := false
	for _, owed := range durable.Outbox(taskID) {
		if owed.Delivery == task.DeliveryAccepted {
			delivered = true
		}
	}
	observations["duplicate_reported_as_delivered"] = delivered
	refs = append(refs, string(taskID))

	reopened, err := memory.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen the storage tier: %v", err)
	}
	restored, err := state.NewDurableCommit(memory.TierFactLog(reopened), nil)
	if err != nil {
		t.Fatalf("reopen the durable commit: %v", err)
	}
	chain, err := restored.Records(ctx, taskID)
	if err != nil {
		t.Fatalf("read the fact chain: %v", err)
	}
	registry2 := task.NewMemoryRegistry(restored, task.NewMemoryAdmission())
	report, err := registry2.Restore(ctx, chain, scenarioTargetOwners(t))
	if err != nil {
		t.Fatalf("restore the task: %v", err)
	}
	recovered := len(report.Tasks) == 1 && report.Tasks[0].Recovered &&
		report.Tasks[0].Refused == nil && report.Tasks[0].Status == task.StatusSettled
	looked, status, lookupErr := registry2.Lookup(ctx, taskID)
	observations["roundtrip_survives_restart"] = recovered && lookupErr == nil &&
		status == task.StatusSettled && looked == spec

	offers := &scenarioOffers{}
	manager := task.NewMemoryManager(registry2, nil, task.WithEnvelopeDeliverer(offers.deliver))
	if err := manager.Flush(ctx, taskID); err != nil {
		t.Fatalf("flush the unconfirmed delivery: %v", err)
	}
	owed := offers.snapshot()
	replayed := len(owed) == 1 && owed[0].Notice.Event == settle.Outbox[0].Notice.Event &&
		owed[0].Target == settle.Outbox[0].Target
	observations["replay_of_unconfirmed_delivery"] = replayed

	EmitScenarioRecord(t, scenario, refs, observations)
}
