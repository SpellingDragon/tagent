// The settle pump drives its turns from the one account the mailbox keeps, so
// the question whether work waits is answered in one place: one notification
// starts one turn however often the pump is woken, an unconsumed notice survives
// a stop and the next process over the same log resumes the turn it owes, and a
// consumed one never comes back beside a repeat its account refuses by name.
package execution

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// wakeAccountAddr is the user address every wake check drives its turns at.
var wakeAccountAddr = event.Address{AgentID: "user", SessionID: "session-wake-account"}

// wakeAccountInstance is the runtime instance the frozen routing names.
const wakeAccountInstance event.InstanceID = "instance-wake"

// wakeAccountNotice is the settled notification the checks settle: one terminal
// notice of one task, frozen beside its result material.
var wakeAccountNotice = task.Notice{
	TaskID: "task-wake",
	Round:  1,
	Kind:   task.NoticeSettled,
	Seq:    0,
	Event:  "fact-wake",
	Text:   "the settled worker reports the total is 42",
}

// wakeAccountFact is the delivery fact the notification points at: the terminal
// settle fact the trigger table fires on.
var wakeAccountFact = event.Event{
	Key:     "settle-fact-wake",
	Type:    event.TypeTaskSettled,
	TaskID:  wakeAccountNotice.TaskID,
	Owner:   wakeAccountAddr,
	Source:  wakeAccountAddr,
	Target:  wakeAccountAddr,
	Message: model.Message{Role: model.RoleUser, Content: wakeAccountNotice.Text},
	Meta: map[string]string{
		event.MetaKeyNoticeKind:          string(task.NoticeSettled),
		event.MetaKeySettleTriggerSource: string(task.NoticeSettled),
		event.MetaKeyEventSummary:        string(task.NoticeSettled),
		event.MetaKeyTaskID:              string(wakeAccountNotice.TaskID),
	},
}

// wakeAccountLog opens one durable account over the named root and commits the
// settled notification into it exactly as the task layer does, so the notice
// stands owed beside its frozen routing until a turn consumes it.
func wakeAccountLog(t *testing.T, root string) *state.DurableCommit {
	t.Helper()
	tier, err := ledger.OpenTier(context.Background(), ledger.StorageConfig{
		Type: ledger.StorageLocalFile,
		Root: root,
	})
	if err != nil {
		t.Fatalf("open the storage tier of %s: %v", root, err)
	}
	durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("open the durable account of %s: %v", root, err)
	}
	if _, err := durable.Commit(context.Background(), task.CommitRecord{
		MutationID: "settle:task-wake:1",
		TaskID:     wakeAccountNotice.TaskID,
		Status:     task.StatusSettled,
		Outbox: []task.Envelope{{
			Notice:         wakeAccountNotice,
			Target:         wakeAccountAddr,
			Source:         wakeAccountAddr,
			TargetInstance: wakeAccountInstance,
		}},
	}); err != nil {
		t.Fatalf("commit the settled notification into %s: %v", root, err)
	}
	return durable
}

// wakeAccountLookup answers every fact read the driver makes with the one
// terminal settle fact, beside an assertion that the read names a settle
// delivery identity rather than any other fact in the chain.
func wakeAccountLookup(t *testing.T) func(context.Context, event.EventID) (event.Event, error) {
	t.Helper()
	return func(_ context.Context, id event.EventID) (event.Event, error) {
		if !strings.HasPrefix(string(id), "settle-") {
			t.Errorf("the driver read the fact %q, want a settle delivery identity", id)
		}
		return wakeAccountFact, nil
	}
}

// wakeFire is one driver's follow-up face: it counts the turns it drove and
// parks the ones the check holds, so a second turn beside a first in flight
// would be visible in the count.
type wakeFire struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	hold    chan struct{}
	release sync.Once
}

// newWakeFire returns a follow-up face whose turns park until releaseFire lets
// them return, when hold is not nil; beside a nil hold every turn returns at
// once.
func newWakeFire(hold chan struct{}) *wakeFire {
	return &wakeFire{
		started: make(chan struct{}, 8),
		hold:    hold,
	}
}

// fire drives one follow-up turn, holding it beside the check's gate when one
// stands. A cancelled drive reports the cancellation as its outcome, so the
// account keeps the entry owed exactly as a stopped executor leaves it.
func (f *wakeFire) fire(ctx context.Context, _ event.Address, _ event.Event) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	f.started <- struct{}{}
	if f.hold != nil {
		select {
		case <-f.hold:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// count reports how many turns this face drove.
func (f *wakeFire) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// releaseFire lets every parked turn return.
func (f *wakeFire) releaseFire() {
	f.release.Do(func() {
		if f.hold != nil {
			close(f.hold)
		}
	})
}

// wakeWaitFor waits for one account effect, bounded by the check's own deadline.
func wakeWaitFor(t *testing.T, what string, reached func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !reached() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSettleWakeAccount pins the one-work-account semantics of the settle pump over the mailbox face.
//   - one notification starts one turn however often the pump is woken
//   - a stop keeps the unconsumed notice and the next process over the same durable account resumes the turn it owes
//   - a consumed notice never replays and the repeated consumption is refused by name
//
// 契约: docs/wiki/agent/agent-architecture.md#settle-driver
func TestSettleWakeAccount(t *testing.T) {
	t.Run("one notification starts one turn however often the pump is woken", func(t *testing.T) {
		durable := wakeAccountLog(t, t.TempDir())
		store := state.NewMemoryStore(state.NewAuthority(), durable)
		hold := make(chan struct{})
		fire := newWakeFire(hold)
		defer fire.releaseFire()
		driver := assembly.NewSettleDriver(wakeAccountLookup(t), func() assembly.SettleWake { return store }, fire.fire)
		defer driver.Stop()

		for i := 0; i < 3; i++ {
			driver.Wake()
		}
		wakeWaitFor(t, "the one turn the notification owes", func() bool { return fire.count() == 1 })
		for i := 0; i < 3; i++ {
			driver.Wake()
		}
		time.Sleep(500 * time.Millisecond)
		if got := fire.count(); got != 1 {
			t.Fatalf("follow-up turns = %d, want the one turn one notification starts however often the pump is woken", got)
		}

		fire.releaseFire()
		wakeWaitFor(t, "the finished turn to consume its entry", func() bool { return len(store.Unconsumed()) == 0 })
		driver.Wake()
		time.Sleep(500 * time.Millisecond)
		if got := fire.count(); got != 1 {
			t.Fatalf("follow-up turns after the consumption = %d, want the single turn the notification already delivered", got)
		}
	})

	t.Run("a stop keeps the unconsumed notice and the next process resumes it", func(t *testing.T) {
		root := t.TempDir()
		durable := wakeAccountLog(t, root)
		store := state.NewMemoryStore(state.NewAuthority(), durable)
		hold := make(chan struct{})
		fire := newWakeFire(hold)
		defer fire.releaseFire()
		driver := assembly.NewSettleDriver(wakeAccountLookup(t), func() assembly.SettleWake { return store }, fire.fire)

		driver.Wake()
		wakeWaitFor(t, "the first follow-up attempt", func() bool { return fire.count() == 1 })
		driver.Stop()
		fire.releaseFire()
		if got := len(store.Unconsumed()); got != 1 {
			t.Fatalf("unconsumed entries beside the stop = %d, want the stopped attempt's notice kept", got)
		}

		restored := wakeAccountLog(t, root)
		resumed := state.NewMemoryStore(state.NewAuthority(), restored)
		resumedFire := newWakeFire(nil)
		next := assembly.NewSettleDriver(wakeAccountLookup(t), func() assembly.SettleWake { return resumed }, resumedFire.fire)
		defer next.Stop()
		next.Wake()
		wakeWaitFor(t, "the resumed follow-up", func() bool { return resumedFire.count() == 1 })
		wakeWaitFor(t, "the resumed turn to consume its entry", func() bool { return len(resumed.Unconsumed()) == 0 })
		if got := restored.OwedNotices(); len(got) != 0 {
			t.Fatalf("the durable account still owes %d notices, want the resumed turn's consumption to close the entry", len(got))
		}
		time.Sleep(500 * time.Millisecond)
		if got := resumedFire.count(); got != 1 {
			t.Fatalf("follow-up turns after the resume = %d, want the single resumed turn", got)
		}
	})

	t.Run("a consumed notice never replays and the repeated consumption is refused by name", func(t *testing.T) {
		ctx := context.Background()
		durable := wakeAccountLog(t, t.TempDir())
		store := state.NewMemoryStore(state.NewAuthority(), durable)
		hold := make(chan struct{})
		fire := newWakeFire(hold)
		defer fire.releaseFire()
		driver := assembly.NewSettleDriver(wakeAccountLookup(t), func() assembly.SettleWake { return store }, fire.fire)
		defer driver.Stop()

		driver.Wake()
		wakeWaitFor(t, "the one turn the notification owes", func() bool { return fire.count() == 1 })
		var seat state.WorkSeat
		wakeWaitFor(t, "the claimed seat of the parked turn", func() bool {
			seats := store.Unconsumed()
			if len(seats) != 1 || seats[0].Claim != state.ClaimClaimed {
				return false
			}
			seat = seats[0]
			return true
		})

		fire.releaseFire()
		wakeWaitFor(t, "the finished turn to consume its entry", func() bool { return len(store.Unconsumed()) == 0 })
		if err := store.Consume(ctx, seat.Address, seat.Instance, seat.AcceptSeq); !errors.Is(err, state.ErrClaimSettled) {
			t.Fatalf("repeat the consumption = %v, want %v beside the entry the first consumption settled", err, state.ErrClaimSettled)
		}

		replay := newWakeFire(nil)
		guard := assembly.NewSettleDriver(wakeAccountLookup(t), func() assembly.SettleWake { return store }, replay.fire)
		defer guard.Stop()
		guard.Wake()
		time.Sleep(500 * time.Millisecond)
		if got := replay.count(); got != 0 {
			t.Fatalf("follow-up turns of the replay guard = %d, want the consumed notice to stay consumed", got)
		}
	})
}
