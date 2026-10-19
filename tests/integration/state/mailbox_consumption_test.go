package state

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"

	orgstate "github.com/SpellingDragon/tagent/runtime/state"
)

// TestMailboxConsumption checks how one mailbox keeps its three time points apart while a slow model decision holds the session slot.
// - acceptance, claim and consumption stay separately readable and never overwrite one another
// - a new input is accepted and a queued input is withdrawn while a slow decision is in flight, and neither path waits for that decision
// - a claim left unfinished is claimed again and finished beside the same entry, and a settled entry refuses the repeat by name
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestMailboxConsumption(t *testing.T) {
	t.Run("acceptance, claim and consumption stay apart", func(t *testing.T) {
		ctx := context.Background()
		authority := orgstate.NewAuthority()
		store := orgstate.NewMemoryStore(authority)
		addr := event.Address{AgentID: "agent-m", SessionID: "session-m"}
		if err := authority.GrantRead(ctx, "subject-a", addr); err != nil {
			t.Fatalf("record the read grant: %v", err)
		}
		if _, err := store.Establish(ctx, addr, "instance-1"); err != nil {
			t.Fatalf("establish the site: %v", err)
		}
		if err := store.TrackDelivery(ctx, "task-three", task.HandoffTarget{Target: addr, TargetInstance: "instance-1"}); err != nil {
			t.Fatalf("register the delivery target: %v", err)
		}
		notice := task.Notice{TaskID: "task-three", Round: 1, Kind: task.NoticeAccepted, Seq: 1}
		if err := store.AcceptNotice(ctx, notice); err != nil {
			t.Fatalf("accept the input: %v", err)
		}

		read := site(t, store, "subject-a", addr, "instance-1")
		accepted := read.Mailbox[0]
		if accepted.AcceptSeq != 1 || accepted.Claim != orgstate.ClaimPending || accepted.Receipt.Completed {
			t.Fatalf("accepted entry = %+v, want the acceptance sequence beside a pending claim and no receipt", accepted)
		}

		if err := store.Claim(ctx, addr, "instance-1", 1); err != nil {
			t.Fatalf("claim the accepted input: %v", err)
		}
		read = site(t, store, "subject-a", addr, "instance-1")
		claimed := read.Mailbox[0]
		if claimed.AcceptSeq != 1 || claimed.Claim != orgstate.ClaimClaimed || claimed.Receipt.Completed {
			t.Fatalf("claimed entry = %+v, want the claim moved alone beside the acceptance it kept and no receipt yet", claimed)
		}

		if err := store.Consume(ctx, addr, "instance-1", 1); err != nil {
			t.Fatalf("consume the claimed input: %v", err)
		}
		read = site(t, store, "subject-a", addr, "instance-1")
		done := read.Mailbox[0]
		if done.AcceptSeq != 1 || done.Claim != orgstate.ClaimConsumed || !done.Receipt.Completed {
			t.Fatalf("consumed entry = %+v, want the receipt recorded beside the acceptance and claim it kept", done)
		}
		if read.Consumed != 1 {
			t.Fatalf("consumption position = %d, want 1 covering the sequence whose consumption completed", read.Consumed)
		}
	})

	t.Run("a queued input is accepted and cancelled while the slow decision runs", func(t *testing.T) {
		ctx := context.Background()
		authority := orgstate.NewAuthority()
		store := orgstate.NewMemoryStore(authority)
		occupancy := orgstate.NewOccupancy()
		addr := event.Address{AgentID: "agent-s", SessionID: "session-s"}
		if err := authority.GrantRead(ctx, "subject-a", addr); err != nil {
			t.Fatalf("record the read grant: %v", err)
		}
		if _, err := store.Establish(ctx, addr, "instance-1"); err != nil {
			t.Fatalf("establish the site: %v", err)
		}
		if err := store.TrackDelivery(ctx, "task-slow", task.HandoffTarget{Target: addr, TargetInstance: "instance-1"}); err != nil {
			t.Fatalf("register the delivery target: %v", err)
		}
		first := task.Notice{TaskID: "task-slow", Round: 1, Kind: task.NoticeAccepted, Seq: 1}
		if err := store.AcceptNotice(ctx, first); err != nil {
			t.Fatalf("accept the first input: %v", err)
		}

		started := make(chan struct{})
		slowModel := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			if err := occupancy.Occupy(addr, "instance-1", "invocation-1", "decision-1"); err != nil {
				finished <- err
				return
			}
			close(started)
			<-slowModel
			finished <- occupancy.Vacate(addr, "instance-1", "invocation-1")
		}()
		<-started

		second := task.Notice{TaskID: "task-slow", Round: 1, Kind: task.NoticeAccepted, Seq: 2}
		if err := store.AcceptNotice(ctx, second); err != nil {
			t.Fatalf("accept beside a decision in flight = %v, want the acceptance to answer without waiting for the slow model", err)
		}
		if err := occupancy.Withdraw(addr, "instance-1", 2); err != nil {
			t.Fatalf("withdraw the queued input beside a decision in flight = %v, want the withdrawal to answer without waiting for the slow model", err)
		}
		if !occupancy.Withdrawn(addr, "instance-1", 2) {
			t.Fatalf("withdrawal record missing while the slow decision still runs")
		}
		if _, _, held := occupancy.Slot(addr, "instance-1"); !held {
			t.Fatalf("session slot vacated while the slow model is still blocked, want the accept and cancel paths decoupled from the execution occupancy")
		}
		if err := occupancy.Occupy(addr, "instance-1", "invocation-2", "decision-2"); !errors.Is(err, orgstate.ErrSlotHeld) {
			t.Fatalf("second decision of the same session = %v, want %v beside one effective model decision per session", err, orgstate.ErrSlotHeld)
		}

		read := site(t, store, "subject-a", addr, "instance-1")
		queued := read.Mailbox[1]
		if queued.AcceptSeq != 2 || queued.Claim != orgstate.ClaimPending {
			t.Fatalf("queued entry = %+v, want the second input waiting its turn beside no claim and no binding taken", queued)
		}

		close(slowModel)
		if err := <-finished; err != nil {
			t.Fatalf("vacate the slot the slow decision held: %v", err)
		}
	})

	t.Run("a claim left unfinished is claimed again and finished", func(t *testing.T) {
		ctx := context.Background()
		authority := orgstate.NewAuthority()
		store := orgstate.NewMemoryStore(authority)
		addr := event.Address{AgentID: "agent-r", SessionID: "session-r"}
		if err := authority.GrantRead(ctx, "subject-a", addr); err != nil {
			t.Fatalf("record the read grant: %v", err)
		}
		if _, err := store.Establish(ctx, addr, "instance-1"); err != nil {
			t.Fatalf("establish the site: %v", err)
		}
		if err := store.TrackDelivery(ctx, "task-crash", task.HandoffTarget{Target: addr, TargetInstance: "instance-1"}); err != nil {
			t.Fatalf("register the delivery target: %v", err)
		}
		first := task.Notice{TaskID: "task-crash", Round: 1, Kind: task.NoticeAccepted, Seq: 1}
		second := task.Notice{TaskID: "task-crash", Round: 1, Kind: task.NoticeAccepted, Seq: 2}
		if err := store.AcceptNotice(ctx, first); err != nil {
			t.Fatalf("accept the first input: %v", err)
		}
		if err := store.AcceptNotice(ctx, second); err != nil {
			t.Fatalf("accept the second input: %v", err)
		}
		if err := store.Claim(ctx, addr, "instance-1", 1); err != nil {
			t.Fatalf("claim the first input: %v", err)
		}

		if err := store.Reclaim(ctx, addr, "instance-1", 1); err != nil {
			t.Fatalf("reclaim beside an unfinished claim = %v, want the retry to pick the same input up again", err)
		}
		read := site(t, store, "subject-a", addr, "instance-1")
		retried := read.Mailbox[0]
		if retried.Claim != orgstate.ClaimClaimed || retried.Receipt.Completed {
			t.Fatalf("retried entry = %+v, want the claim reasserted beside no receipt recorded", retried)
		}

		if err := store.Reclaim(ctx, addr, "instance-1", 2); !errors.Is(err, orgstate.ErrNotClaimed) {
			t.Fatalf("reclaim of a never-claimed input = %v, want %v beside a first claim through Claim", err, orgstate.ErrNotClaimed)
		}

		if err := store.Consume(ctx, addr, "instance-1", 1); err != nil {
			t.Fatalf("consume the reclaimed input: %v", err)
		}
		if err := store.Reclaim(ctx, addr, "instance-1", 1); !errors.Is(err, orgstate.ErrClaimSettled) {
			t.Fatalf("reclaim of a settled input = %v, want %v beside a consumption already completed", err, orgstate.ErrClaimSettled)
		}
		read = site(t, store, "subject-a", addr, "instance-1")
		if read.Consumed != 1 || !read.Mailbox[0].Receipt.Completed {
			t.Fatalf("settled entry = %+v at position %d, want the receipt kept beside the one completed consumption", read.Mailbox[0], read.Consumed)
		}
	})

	t.Run("an entry traces back to its event, invocation and notice", func(t *testing.T) {
		ctx := context.Background()
		authority := orgstate.NewAuthority()
		store := orgstate.NewMemoryStore(authority)
		addr := event.Address{AgentID: "agent-t", SessionID: "session-t"}
		if err := authority.GrantRead(ctx, "subject-a", addr); err != nil {
			t.Fatalf("record the read grant: %v", err)
		}
		if _, err := store.Establish(ctx, addr, "instance-1"); err != nil {
			t.Fatalf("establish the site: %v", err)
		}
		if err := store.TrackDelivery(ctx, "task-trace", task.HandoffTarget{Target: addr, TargetInstance: "instance-1"}); err != nil {
			t.Fatalf("register the delivery target: %v", err)
		}
		delivery := task.WithCallFrame(ctx, task.CallFrame{
			Identity: task.CallIdentity{InstanceID: "instance-1", InvocationID: "invocation-t", DecisionID: "decision-t", CallID: "call-t"},
			Address:  addr,
		})
		notice := task.Notice{TaskID: "task-trace", Round: 1, Kind: task.NoticeSettled, Seq: 1, Event: "fact-t"}
		if err := store.AcceptNotice(delivery, notice); err != nil {
			t.Fatalf("accept the input beside its producing call: %v", err)
		}
		read := site(t, store, "subject-a", addr, "instance-1")
		entry := read.Mailbox[0]
		if entry.EventID == "" || entry.InvocationID == "" || entry.Notice.TaskID == "" {
			t.Fatalf("accepted entry = %+v, want the event, invocation and notice associations recorded", entry)
		}
		if entry.EventID != notice.Event || entry.EventID != entry.Notice.Event {
			t.Fatalf("entry event = %q, want the committed fact %q its notice points at, never chosen between the two", entry.EventID, notice.Event)
		}
		if entry.InvocationID != "invocation-t" {
			t.Fatalf("entry invocation = %q, want the responsibility range %q the producing call belongs to", entry.InvocationID, "invocation-t")
		}
		if entry.Notice != notice {
			t.Fatalf("entry notice = %+v, want the stable identity %+v accepted", entry.Notice, notice)
		}
	})

	t.Run("the position covers the longest contiguous completed prefix", func(t *testing.T) {
		ctx := context.Background()
		authority := orgstate.NewAuthority()
		store := orgstate.NewMemoryStore(authority)
		addr := event.Address{AgentID: "agent-p", SessionID: "session-p"}
		if err := authority.GrantRead(ctx, "subject-a", addr); err != nil {
			t.Fatalf("record the read grant: %v", err)
		}
		if _, err := store.Establish(ctx, addr, "instance-1"); err != nil {
			t.Fatalf("establish the site: %v", err)
		}
		if err := store.TrackDelivery(ctx, "task-prefix", task.HandoffTarget{Target: addr, TargetInstance: "instance-1"}); err != nil {
			t.Fatalf("register the delivery target: %v", err)
		}
		first := task.Notice{TaskID: "task-prefix", Round: 1, Kind: task.NoticeAccepted, Seq: 1}
		second := task.Notice{TaskID: "task-prefix", Round: 1, Kind: task.NoticeAccepted, Seq: 2}
		if err := store.AcceptNotice(ctx, first); err != nil {
			t.Fatalf("accept the first input: %v", err)
		}
		if err := store.AcceptNotice(ctx, second); err != nil {
			t.Fatalf("accept the second input: %v", err)
		}
		if err := store.Claim(ctx, addr, "instance-1", 2); err != nil {
			t.Fatalf("claim the second input: %v", err)
		}
		if err := store.Consume(ctx, addr, "instance-1", 2); err != nil {
			t.Fatalf("consume the second input beside the unfinished first: %v", err)
		}
		read := site(t, store, "subject-a", addr, "instance-1")
		if read.Consumed != 0 {
			t.Fatalf("consumption position = %d, want 0 staying before the hole the unfinished first input leaves", read.Consumed)
		}
		if read.Mailbox[0].Receipt.Completed || !read.Mailbox[1].Receipt.Completed {
			t.Fatalf("entries = %+v, want the second completion recorded on its own entry beside the first unfinished", read.Mailbox)
		}

		if err := store.Claim(ctx, addr, "instance-1", 1); err != nil {
			t.Fatalf("claim the first input: %v", err)
		}
		if err := store.Consume(ctx, addr, "instance-1", 1); err != nil {
			t.Fatalf("consume the first input: %v", err)
		}
		read = site(t, store, "subject-a", addr, "instance-1")
		if read.Consumed != 2 {
			t.Fatalf("consumption position = %d, want 2 covering the longest contiguous completed prefix", read.Consumed)
		}
	})
}
