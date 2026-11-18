// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
package task_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// taskSliceFactLog is the fact log the reduction anchors store through: an in-process map.
type taskSliceFactLog struct {
	held map[string][]byte
}

// Put stores one value under its key.
func (l *taskSliceFactLog) Put(_ context.Context, key string, value []byte) error {
	l.held[key] = append([]byte(nil), value...)
	return nil
}

// Get answers one stored value.
func (l *taskSliceFactLog) Get(_ context.Context, key string) ([]byte, bool, error) {
	value, held := l.held[key]
	return value, held, nil
}

// Scan answers every stored pair under the prefix.
func (l *taskSliceFactLog) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for key, value := range l.held {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			out[key] = value
		}
	}
	return out, nil
}

// Sync completes the barrier.
func (l *taskSliceFactLog) Sync(context.Context) error { return nil }

// settleSeed is the chain one anchor seats against: the admission material beside the record whose three notices the account owes, before any receipt lands.
type settleSeed struct {
	log     *taskSliceFactLog
	commit  *state.DurableCommit
	taskID  event.TaskID
	spec    task.Spec
	notices []task.Notice
}

// seedSettleChain commits the admission material and one three-notice record into a fresh log, so an anchor seats a chain it can also read back.
func seedSettleChain(t *testing.T) settleSeed {
	t.Helper()
	ctx := context.Background()
	log := &taskSliceFactLog{held: map[string][]byte{}}
	taskID := event.TaskID("task-owed-receipt")
	spec := task.Spec{
		CallID: "call-owed", DedupKey: "dedup-owed",
		InvocationID: "inv-owed", InstanceID: "inst-owed",
		Description:   "the task whose stated receipt a reduction may only omit",
		Source:        event.Address{AgentID: "agent-a", SessionID: "session-owed"},
		Target:        event.Address{AgentID: "agent-b", SessionID: "session-owed"},
		TriggerSource: "user",
		Execution:     task.Serialization("session-owed-serialized"),
	}
	commit, err := state.NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("open the account: %v", err)
	}
	if err := commit.Declare(ctx, taskID, spec); err != nil {
		t.Fatalf("declare the admission material: %v", err)
	}
	notices := make([]task.Notice, 0, 3)
	for seq := int64(1); seq <= 3; seq++ {
		notices = append(notices, task.Notice{
			TaskID: taskID, Round: 1, Kind: task.NoticeProgress, Seq: seq,
			Event: event.EventID("owed-fact"),
		})
	}
	outbox := make([]task.Envelope, 0, len(notices))
	for _, notice := range notices {
		outbox = append(outbox, task.Envelope{
			Notice: notice, Target: spec.Target, Source: spec.Source, TargetInstance: spec.InstanceID,
		})
	}
	record := task.CommitRecord{
		MutationID: "owed-mutation-0", TaskID: taskID, ExpectedRevision: 0,
		Status: task.StatusRunning, Outbox: outbox,
	}
	if result, err := commit.Commit(ctx, record); err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the three-notice record = (%v, %v)", result, err)
	}
	return settleSeed{log: log, commit: commit, taskID: taskID, spec: spec, notices: notices}
}

// consumeFirst records the attempt of the first notice beside its claim and its consumption, so the chain holds one receipt and the record still owes the other two.
func (s settleSeed) consumeFirst(t *testing.T) task.Notice {
	t.Helper()
	ctx := context.Background()
	store := state.NewMemoryStore(state.NewAuthority(), s.commit)
	if err := store.TrackDelivery(ctx, s.taskID, task.HandoffTarget{Target: s.spec.Target, TargetInstance: s.spec.InstanceID}); err != nil {
		t.Fatalf("register the delivery: %v", err)
	}
	mailboxPayload, err := json.Marshal(struct {
		Owed []task.OutboxNotice `json:"owed"`
	}{Owed: s.commit.OwedNotices()})
	if err != nil {
		t.Fatalf("state the mailbox slice: %v", err)
	}
	if err := state.NewMailboxState(store).RestoreState(ctx, mailboxPayload); err != nil {
		t.Fatalf("restore the mailbox: %v", err)
	}
	if err := store.RecordAttempt(ctx, s.notices[0], state.SettleAttempt{Terminal: "completed"}); err != nil {
		t.Fatalf("record the attempt of the settled notice: %v", err)
	}
	settled := s.notices[0]
	var seat state.WorkSeat
	for _, open := range store.Unconsumed() {
		if open.Notice == settled {
			seat = open
		}
	}
	if seat.Notice != settled {
		t.Fatalf("the restored mailbox holds no seat for %s, want the notice it accepted", settled.FactKey())
	}
	if err := store.Claim(ctx, seat.Address, seat.Instance, seat.AcceptSeq); err != nil {
		t.Fatalf("claim the settled notice: %v", err)
	}
	if err := store.Consume(ctx, seat.Address, seat.Instance, seat.AcceptSeq); err != nil {
		t.Fatalf("consume the first notice: %v", err)
	}
	if owed := len(s.commit.OwedNotices()); owed != 2 {
		t.Fatalf("the account owes %d notice(s) after one consumption, want the 2 still open", owed)
	}
	return settled
}

// payloadStatingConsumed answers the tasks slice the producer states with the consumption family replaced: a nil slice omits the family, an empty slice states it as none.
func (s settleSeed) payloadStatingConsumed(t *testing.T, consumed []task.StatedConsumption) []byte {
	t.Helper()
	ctx := context.Background()
	entry := task.StatedTask{
		TaskID: s.taskID, Spec: s.spec, Status: task.StatusRunning, Revision: s.commit.Revision(s.taskID),
		Mutations: s.commit.Facts(s.taskID),
	}
	payload, err := json.Marshal(task.RegistryCheckpoint{Tasks: []task.StatedTask{entry}})
	if err != nil {
		t.Fatalf("state the task account: %v", err)
	}
	payload, err = task.StateSettleAccount(ctx, payload, s.commit)
	if err != nil {
		t.Fatalf("state the settle account: %v", err)
	}
	var stated task.RegistryCheckpoint
	if err := json.Unmarshal(payload, &stated); err != nil {
		t.Fatalf("read the stated account: %v", err)
	}
	if held := len(stated.Tasks[0].Consumed); held != 1 {
		t.Fatalf("the producer stated %d consumption receipt(s), want the 1 the account holds: an account holding a receipt is stated as that receipt and never as an empty family", held)
	}
	stated.Tasks[0].Consumed = consumed
	body, err := json.Marshal(stated)
	if err != nil {
		t.Fatalf("state the reduced account: %v", err)
	}
	return body
}

// seatOver seats one stated payload over one chain and answers the account it built, so a caller reads that account without paying the seat twice.
func seatOver(t *testing.T, log *taskSliceFactLog, payload []byte) *state.DurableCommit {
	t.Helper()
	seated := state.NewEmptyDurableCommit(log, nil)
	if err := seated.Seat(context.Background(), payload, map[string]struct{}{}); err != nil {
		t.Fatalf("seat the stated account: %v", err)
	}
	return seated
}

// owesNotice reports whether the account still owes one notification identity.
func owesNotice(commit *state.DurableCommit, notice task.Notice) bool {
	for _, open := range commit.OwedNotices() {
		if open.Notice.Notice == notice {
			return true
		}
	}
	return false
}

// TestTaskSliceReadsBackTheReceiptItDoesNotState pins the one reduction a tasks slice may make: omit a receipt family.
//   - mutation: the producer states the consumption receipt, then the payload leaves that family out
//   - positive: the seat re-owes the settled notice none and answers the receipt it reads back from that task's own chain
//   - negative: the same payload over a chain holding no receipt re-owes the notice, so the receipt is the chain's and not the payload's
//   - consumer: StateSettleAccount as the producer and DurableCommit.Seat as the restore of the stated payload
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestTaskSliceReadsBackTheReceiptItDoesNotState(t *testing.T) {
	seed := seedSettleChain(t)
	settled := seed.consumeFirst(t)
	payload := seed.payloadStatingConsumed(t, nil)

	seated := seatOver(t, seed.log, payload)
	if owesNotice(seated, settled) {
		t.Fatalf("the slice that omits the consumption family re-owed the notice %s: an omission states no receipt and never the empty account, so the seat reads the receipt back from the chain",
			settled.FactKey())
	}
	consumed := seated.ReceiptsOf(seed.taskID).Consumed
	if len(consumed) != 1 {
		t.Fatalf("the seat of a slice that omits the consumption family answers %d receipt(s), want the 1 the chain holds: an omission leaves the receipt to the chain, which still holds it",
			len(consumed))
	}
	readback := consumed[0]
	if readback.Notice.TaskID != settled.TaskID || readback.Notice.Round != settled.Round ||
		readback.Notice.Kind != settled.Kind || readback.Notice.Seq != settled.Seq || readback.Event != settled.Event {
		t.Fatalf("the seat of a slice that omits the consumption family answers the receipt %s of %s, want the receipt of %s the chain holds: the receipt is the identity a cross-process retry is judged by",
			readback.Notice.FactKey(), readback.Event, settled.FactKey())
	}
	if scans := seated.LoadStats().Scans; len(scans) == 0 {
		t.Fatalf("the seat of a slice that omits the consumption family read no durable prefix, want the read of that task's own chain")
	}

	unreceipted := seedSettleChain(t)
	if !owesNotice(seatOver(t, unreceipted.log, payload), settled) {
		t.Fatalf("the same slice over a chain holding no receipt owed the notice %s nothing, want it owed: the receipt the seat answers is the one the chain holds",
			settled.FactKey())
	}
}

// TestTaskSliceTrustsAStatedEmptyReceiptSet pins the other half of the same distinction: an empty set is a statement, and the seat trusts it over the chain.
//   - mutation: the same account states the consumption family as an empty list while the chain holds the receipt
//   - positive: the seat reads no durable prefix and answers no receipt, so the notice the statement denies is owed again
//   - negative: the same payload with the family omitted answers that receipt, so a stated empty set and an omission never interchange
//   - consumer: DurableCommit.Seat as the restore of the stated payload a reduction that states an empty family would write
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestTaskSliceTrustsAStatedEmptyReceiptSet(t *testing.T) {
	seed := seedSettleChain(t)
	settled := seed.consumeFirst(t)
	payload := seed.payloadStatingConsumed(t, []task.StatedConsumption{})

	seated := seatOver(t, seed.log, payload)
	if got := len(seated.ReceiptsOf(seed.taskID).Consumed); got != 0 {
		t.Fatalf("the seat of a slice that states no consumption answers %d receipt(s), want the 0 the statement makes: reading the chain past a stated empty set would answer a receipt the slice denies",
			got)
	}
	if !owesNotice(seated, settled) {
		t.Fatalf("the seat of a slice that states no consumption owed the notice %s nothing, want it owed again: stating an empty family while the chain holds the receipt loses it, which is why a reduction only ever omits",
			settled.FactKey())
	}
	if scans := seated.LoadStats().Scans; len(scans) != 0 {
		t.Fatalf("the seat of a slice that states its settle account read %d durable prefix(es), want none: a stated empty set is an account and not an omission",
			len(scans))
	}
}
