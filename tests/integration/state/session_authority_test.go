// Package state binds one explicit address to one runtime instance and keeps
// delivery, read and mailbox time points apart beside that identity.
package state

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/task"

	orgstate "github.com/SpellingDragon/tagent/state"
)

// TestSessionAuthority checks how one session site keeps its explicit target, instance identity, read grants and mailbox time points apart.
// - a notification no delivery registration routes is refused by name, and no site is established behind the refusal
// - a rebuilt instance takes none of the late arrivals meant for the instance it replaced
// - a read without a recorded grant is refused, and a check records no grant of its own
// - acceptance, claim and consumption stay separately readable and never overwrite one another
// - one notification identity records exactly one fact however often it is delivered
// 契约: docs/wiki/agent/event-flow.md#multi-user-isolation
func TestSessionAuthority(t *testing.T) {
	t.Run("explicit target refuses what it does not name", func(t *testing.T) {
		ctx := context.Background()
		authority := orgstate.NewAuthority()
		store := orgstate.NewMemoryStore(authority)
		addr := event.Address{AgentID: "agent-a", SessionID: "session-a"}
		if _, err := store.Establish(ctx, addr, "instance-a"); err != nil {
			t.Fatalf("establish the site a caller named: %v", err)
		}

		unrouted := task.Notice{TaskID: "task-unrouted", Round: 1, Kind: task.NoticeAccepted, Seq: 1}
		if err := store.AcceptNotice(ctx, unrouted); !errors.Is(err, orgstate.ErrDeliveryNotAllowed) {
			t.Fatalf("unrouted notification = %v, want %v beside a refusal that names the missing registration", err, orgstate.ErrDeliveryNotAllowed)
		}

		half := event.Address{AgentID: "agent-a"}
		if _, err := store.Establish(ctx, half, "instance-a"); !errors.Is(err, orgstate.ErrIncompleteAddress) {
			t.Fatalf("half address established = %v, want %v beside an address that names no session", err, orgstate.ErrIncompleteAddress)
		}

		other := event.Address{AgentID: "agent-b", SessionID: "session-b"}
		if err := store.TrackDelivery(ctx, "task-unbuilt", task.HandoffTarget{Target: other, TargetInstance: "instance-b"}); err != nil {
			t.Fatalf("register the explicit delivery target: %v", err)
		}
		routed := task.Notice{TaskID: "task-unbuilt", Round: 1, Kind: task.NoticeAccepted, Seq: 1}
		if err := store.AcceptNotice(ctx, routed); !errors.Is(err, orgstate.ErrUnknownTarget) {
			t.Fatalf("delivery to a target no site serves = %v, want %v beside a refusal that establishes nothing", err, orgstate.ErrUnknownTarget)
		}

		if err := authority.GrantRead(ctx, "subject-a", other); err != nil {
			t.Fatalf("record the read grant: %v", err)
		}
		got, err := store.Site(ctx, "subject-a", other, "instance-b")
		if !errors.Is(err, orgstate.ErrUnknownTarget) || got != nil {
			t.Fatalf("read of an unestablished target = (%v, %v), want (nil, %v) so no site appears behind the refusal", got, err, orgstate.ErrUnknownTarget)
		}
	})

	t.Run("a rebuilt instance takes none of the retired one", func(t *testing.T) {
		ctx := context.Background()
		authority := orgstate.NewAuthority()
		store := orgstate.NewMemoryStore(authority)
		addr := event.Address{AgentID: "agent-c", SessionID: "session-c"}
		if err := authority.GrantRead(ctx, "subject-a", addr); err != nil {
			t.Fatalf("record the read grant: %v", err)
		}
		first, err := store.Establish(ctx, addr, "instance-1")
		if err != nil {
			t.Fatalf("establish the first instance: %v", err)
		}
		if first.Instance != "instance-1" {
			t.Fatalf("first instance = %q, want %q", first.Instance, "instance-1")
		}
		accepted := task.Notice{TaskID: "task-old", Round: 1, Kind: task.NoticeAccepted, Seq: 1, Event: "fact-accepted"}
		if err := store.TrackDelivery(ctx, "task-old", task.HandoffTarget{Target: addr, TargetInstance: "instance-1"}); err != nil {
			t.Fatalf("register the first instance as the delivery target: %v", err)
		}
		if err := store.AcceptNotice(ctx, accepted); err != nil {
			t.Fatalf("accept into the first instance: %v", err)
		}

		second, err := store.Establish(ctx, addr, "instance-2")
		if err != nil {
			t.Fatalf("rebuild under the same address: %v", err)
		}
		if second.Instance != "instance-2" {
			t.Fatalf("rebuilt instance = %q, want %q beside a new identity for the same name", second.Instance, "instance-2")
		}

		late := task.Notice{TaskID: "task-old", Round: 1, Kind: task.NoticeAccepted, Seq: 2, Event: "fact-late"}
		if err := store.AcceptNotice(ctx, late); !errors.Is(err, orgstate.ErrInstanceRetired) {
			t.Fatalf("late arrival for the retired instance = %v, want %v beside a refusal that keeps it out of the rebuilt instance", err, orgstate.ErrInstanceRetired)
		}
		if err := store.AcceptNotice(ctx, accepted); err != nil {
			t.Fatalf("replay of the accepted notification = %v, want the idempotent nil that records no second fact", err)
		}

		rebuilt := site(t, store, "subject-a", addr, "instance-2")
		if len(rebuilt.Mailbox) != 0 {
			t.Fatalf("rebuilt mailbox = %+v, want empty so nothing crosses from the instance it replaced", rebuilt.Mailbox)
		}
		retired := site(t, store, "subject-a", addr, "instance-1")
		if len(retired.Mailbox) != 1 || retired.Mailbox[0].Notice != accepted {
			t.Fatalf("retired mailbox = %+v, want the undelivered obligation kept where its acceptance recorded it", retired.Mailbox)
		}
	})

	t.Run("a read without a grant is refused by name", func(t *testing.T) {
		ctx := context.Background()
		authority := orgstate.NewAuthority()
		store := orgstate.NewMemoryStore(authority)
		addr := event.Address{AgentID: "agent-d", SessionID: "session-d"}
		if _, err := store.Establish(ctx, addr, "instance-1"); err != nil {
			t.Fatalf("establish the site: %v", err)
		}
		subject := orgstate.Subject("subject-b")
		got, err := store.Site(ctx, subject, addr, "instance-1")
		if !errors.Is(err, orgstate.ErrReadNotAllowed) || got != nil {
			t.Fatalf("ungranted read = (%v, %v), want (nil, %v) beside a refusal that returns no content", got, err, orgstate.ErrReadNotAllowed)
		}
		if err := authority.CheckRead(ctx, subject, addr); !errors.Is(err, orgstate.ErrReadNotAllowed) {
			t.Fatalf("check over an unrecorded grant = %v, want %v beside a check that grants nothing", err, orgstate.ErrReadNotAllowed)
		}

		if err := authority.GrantRead(ctx, subject, addr); err != nil {
			t.Fatalf("record the read grant: %v", err)
		}
		readable := site(t, store, subject, addr, "instance-1")
		if readable.Instance != "instance-1" {
			t.Fatalf("readable instance = %q, want %q", readable.Instance, "instance-1")
		}

		if err := store.TrackDelivery(ctx, "task-delivered", task.HandoffTarget{Target: addr, TargetInstance: "instance-1"}); err != nil {
			t.Fatalf("register the delivery target: %v", err)
		}
		delivered := task.Notice{TaskID: "task-delivered", Round: 1, Kind: task.NoticeSettled, Seq: 1}
		if err := store.AcceptNotice(ctx, delivered); err != nil {
			t.Fatalf("accept the notification beside its registration: %v", err)
		}
		elsewhere := event.Address{AgentID: "agent-e", SessionID: "session-e"}
		if err := authority.CheckRead(ctx, subject, elsewhere); !errors.Is(err, orgstate.ErrReadNotAllowed) {
			t.Fatalf("read granted by a delivery = %v, want %v beside a delivery that grants no read", err, orgstate.ErrReadNotAllowed)
		}
	})

	t.Run("acceptance, claim and consumption stay apart", func(t *testing.T) {
		ctx := context.Background()
		authority := orgstate.NewAuthority()
		store := orgstate.NewMemoryStore(authority)
		addr := event.Address{AgentID: "agent-f", SessionID: "session-f"}
		if err := authority.GrantRead(ctx, "subject-a", addr); err != nil {
			t.Fatalf("record the read grant: %v", err)
		}
		if _, err := store.Establish(ctx, addr, "instance-1"); err != nil {
			t.Fatalf("establish the site: %v", err)
		}
		if err := store.TrackDelivery(ctx, "task-ledger", task.HandoffTarget{Target: addr, TargetInstance: "instance-1"}); err != nil {
			t.Fatalf("register the delivery target: %v", err)
		}
		first := task.Notice{TaskID: "task-ledger", Round: 1, Kind: task.NoticeAccepted, Seq: 1}
		second := task.Notice{TaskID: "task-ledger", Round: 1, Kind: task.NoticeAccepted, Seq: 2}
		if err := store.AcceptNotice(ctx, first); err != nil {
			t.Fatalf("accept the first input: %v", err)
		}
		if err := store.AcceptNotice(ctx, second); err != nil {
			t.Fatalf("accept the second input: %v", err)
		}

		read := site(t, store, "subject-a", addr, "instance-1")
		if len(read.Mailbox) != 2 || read.Mailbox[0].AcceptSeq != 1 || read.Mailbox[1].AcceptSeq != 2 {
			t.Fatalf("acceptance log = %+v, want two entries in the monotonic order acceptance assigned", read.Mailbox)
		}
		for _, entry := range read.Mailbox {
			if entry.Claim != orgstate.ClaimPending || entry.Receipt.Completed {
				t.Fatalf("entry = %+v, want pending beside no receipt recorded", entry)
			}
		}

		if err := store.Claim(ctx, addr, "instance-1", 1); err != nil {
			t.Fatalf("claim the first input: %v", err)
		}
		read = site(t, store, "subject-a", addr, "instance-1")
		claimed := read.Mailbox[0]
		if claimed.Claim != orgstate.ClaimClaimed || claimed.AcceptSeq != 1 || claimed.Receipt.Completed {
			t.Fatalf("claimed entry = %+v, want the claim moved alone with acceptance and receipt untouched", claimed)
		}
		if err := store.Claim(ctx, addr, "instance-1", 1); !errors.Is(err, orgstate.ErrClaimConflict) {
			t.Fatalf("second claim = %v, want %v beside a claim held by one executor", err, orgstate.ErrClaimConflict)
		}

		if err := store.Consume(ctx, addr, "instance-1", 1); err != nil {
			t.Fatalf("consume the claimed input: %v", err)
		}
		read = site(t, store, "subject-a", addr, "instance-1")
		done := read.Mailbox[0]
		if done.Claim != orgstate.ClaimConsumed || !done.Receipt.Completed || done.AcceptSeq != 1 {
			t.Fatalf("consumed entry = %+v, want the receipt recorded beside the acceptance and claim it kept", done)
		}
		if read.Consumed != 1 {
			t.Fatalf("consumption position = %d, want 1 covering the sequence whose consumption completed", read.Consumed)
		}
		untouched := read.Mailbox[1]
		if untouched.Claim != orgstate.ClaimPending || untouched.Receipt.Completed {
			t.Fatalf("second entry = %+v, want its own time points untouched by the consumption beside it", untouched)
		}
		if err := store.Consume(ctx, addr, "instance-1", 2); !errors.Is(err, orgstate.ErrNotClaimed) {
			t.Fatalf("consumption of an unclaimed entry = %v, want %v beside a claim requirement", err, orgstate.ErrNotClaimed)
		}
	})

	t.Run("one notification identity records one fact", func(t *testing.T) {
		ctx := context.Background()
		authority := orgstate.NewAuthority()
		store := orgstate.NewMemoryStore(authority)
		addr := event.Address{AgentID: "agent-g", SessionID: "session-g"}
		if err := authority.GrantRead(ctx, "subject-a", addr); err != nil {
			t.Fatalf("record the read grant: %v", err)
		}
		if _, err := store.Establish(ctx, addr, "instance-1"); err != nil {
			t.Fatalf("establish the site: %v", err)
		}
		if err := store.TrackDelivery(ctx, "task-idempotent", task.HandoffTarget{Target: addr, TargetInstance: "instance-1"}); err != nil {
			t.Fatalf("register the delivery target: %v", err)
		}
		notice := task.Notice{TaskID: "task-idempotent", Round: 1, Kind: task.NoticeSettled, Seq: 1, Event: "fact-1"}
		if err := store.AcceptNotice(ctx, notice); err != nil {
			t.Fatalf("first delivery: %v", err)
		}
		if err := store.AcceptNotice(ctx, notice); err != nil {
			t.Fatalf("repeated delivery = %v, want the idempotent nil beside an identity already recorded", err)
		}
		read := site(t, store, "subject-a", addr, "instance-1")
		if len(read.Mailbox) != 1 || read.Mailbox[0].AcceptSeq != 1 || read.Mailbox[0].Notice != notice {
			t.Fatalf("mailbox = %+v, want the one fact recorded once at the acceptance it first got", read.Mailbox)
		}

		altered := notice
		altered.Event = "fact-2"
		if err := store.AcceptNotice(ctx, altered); !errors.Is(err, orgstate.ErrNoticeConflict) {
			t.Fatalf("same identity beside other content = %v, want %v beside a refusal that keeps the recorded fact", err, orgstate.ErrNoticeConflict)
		}
		read = site(t, store, "subject-a", addr, "instance-1")
		if len(read.Mailbox) != 1 {
			t.Fatalf("mailbox = %+v, want the refused repeat to add nothing", read.Mailbox)
		}
	})
}

func site(t *testing.T, store *orgstate.MemoryStore, subject orgstate.Subject, addr event.Address, instance event.InstanceID) *orgstate.Session {
	t.Helper()
	got, err := store.Site(context.Background(), subject, addr, instance)
	if err != nil {
		t.Fatalf("read the site bookkeeping beside a recorded grant: %v", err)
	}
	return got
}
