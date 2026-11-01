// The red-line guard of the agent-instance work reads the three semantics the
// instance refactor must not move through the production faces that own them,
// so a change that moves one of them turns this guard red before it reaches the
// driver work.
//
// 契约: docs/wiki/agent/agent-architecture.md#settle-driver
package host

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

const (
	// redlineGuardSettleTurns is the number of follow-up turns one delivered
	// terminal notification may drive, however often it is observed.
	redlineGuardSettleTurns = 1
	// redlineGuardSlotWinners is the number of effective model decisions one
	// site may admit at a time.
	redlineGuardSlotWinners = 1
	// redlineGuardWhitelistAllowed is the number of targets an unregistered
	// deliver_to table lets through.
	redlineGuardWhitelistAllowed = 0
)

const (
	// redlineGuardWindow bounds every wait of this guard, so a moved semantic
	// answers the named gap instead of holding the run.
	redlineGuardWindow = 30 * time.Second
	// redlineGuardQuiet is the window a repeat of one observation is watched
	// over: the pump answers no work it already consumed, so a second turn would
	// land inside it.
	redlineGuardQuiet = 200 * time.Millisecond
	// redlineGuardInstance is the runtime instance serving every guard site.
	redlineGuardInstance event.InstanceID = "instance-guard"
)

// redlineGuardSubject is the subject that reads back the guard sites it
// established.
const redlineGuardSubject state.Subject = "subject-guard"

// redlineGuardSettleAddress is the site the settle guard drives its turn at.
var redlineGuardSettleAddress = event.Address{AgentID: "agent-guard", SessionID: "session-guard-settle"}

// TestInstancesRedlineGuard pins the three red lines the instance refactor must keep, each through the production face that owns it.
// - settle_drives_one_turn: one settled notification drives exactly one follow-up turn at the settle pump over a real mailbox account
// - settled_entry_refuses_a_second_consumption: the entry that turn closed refuses a repeat by name, so the receipt is the fence
// - progress_notice_drives_no_turn: a notification that is not terminal is never claimed, so a running task turns no runtime
// - slot_admits_one_decision: concurrent decisions at one site admit exactly one occupant of the occupancy ledger
// - slot_refuses_the_second_decision: every other decision at that site is refused by name
// - slot_is_per_site: another session is admitted beside the first
// - vacate_without_the_slot: a vacating invocation that holds no slot is refused by name
// - empty_whitelist_refuses_every_target: the first gate set refuses every target beside an undeclared deliver_to table, and names that table
// - empty_whitelist_refuses_a_resident_target: the bound peer session is refused too, and no target chain ever reads the body
// - registered_whitelist_admits_its_target: the same gate delivers once the document declares the target
func TestInstancesRedlineGuard(t *testing.T) {
	t.Run("settle_drives_one_turn", redlineGuardSettleDrivesOneTurn)
	t.Run("slot_admits_one_decision", redlineGuardSlotAdmitsOneDecision)
	t.Run("empty_whitelist_refuses_every_target", redlineGuardEmptyWhitelistRefusesEveryTarget)
}

// redlineGuardSettleDrivesOneTurn drives one settled notification through the
// settle pump over a real mailbox account and counts the follow-up turns it
// starts: one delivered terminal notification starts one turn, and the mailbox
// entry that turn consumed keeps a repeat from starting a second one.
func redlineGuardSettleDrivesOneTurn(t *testing.T) {
	ctx := context.Background()
	addr := redlineGuardSettleAddress
	settled := task.Notice{TaskID: "task-guard-settled", Round: 1, Kind: task.NoticeSettled, Seq: 1}
	progress := task.Notice{TaskID: "task-guard-progress", Round: 1, Kind: task.NoticeProgress, Seq: 1}
	store := redlineGuardStore(t, ctx, addr, settled.TaskID, progress.TaskID)
	if err := store.AcceptNotice(ctx, settled); err != nil {
		t.Fatalf("accept the settled notification: %v", err)
	}

	var fired atomic.Int32
	var read atomic.Int32
	driver := assembly.NewSettleDriver(
		func(_ context.Context, id event.EventID) (event.Event, error) {
			read.Add(1)
			return event.Event{
				Key:    id,
				Type:   event.TypeTaskSettled,
				TaskID: settled.TaskID,
				Meta:   map[string]string{event.MetaKeyNoticeKind: string(task.NoticeSettled)},
			}, nil
		},
		func() assembly.SettleWake { return store },
		func(context.Context, event.Address, event.Event) error {
			fired.Add(1)
			return nil
		},
	)
	driver.DeclareScope(string(addr.AgentID), string(addr.AgentID))
	defer driver.Stop()

	driver.Wake()
	redlineGuardAwait(t, "the one follow-up turn of the settled notification", func() bool {
		return redlineGuardEntryConsumed(ctx, store, addr)
	})
	if got := int(fired.Load()); got != redlineGuardSettleTurns {
		t.Fatalf("follow-up turns driven by one settled notification = %d, want %d", got, redlineGuardSettleTurns)
	}
	if read.Load() == 0 {
		t.Fatalf("the settle driver read no fact, want the one it drove its turn beside")
	}
	if entry, err := redlineGuardEntry(ctx, store, addr, 1); err != nil {
		t.Fatalf("read the settled mailbox entry: %v", err)
	} else if !entry.Receipt.Completed {
		t.Fatalf("receipt of the settled entry = %+v, want the consumption completed beside the turn it drove", entry.Receipt)
	}

	for i := 0; i < 3; i++ {
		driver.Wake()
	}
	if err := driver.ObserveCommit(ctx, "event-guard-repeated"); err != nil {
		t.Fatalf("observe the repeated commit of the settled fact: %v", err)
	}
	time.Sleep(redlineGuardQuiet)
	if got := int(fired.Load()); got != redlineGuardSettleTurns {
		t.Fatalf("follow-up turns after a repeated observation of one settled fact = %d, want %d", got, redlineGuardSettleTurns)
	}

	if err := store.AcceptNotice(ctx, progress); err != nil {
		t.Fatalf("accept the progress notification: %v", err)
	}
	driver.Wake()
	time.Sleep(redlineGuardQuiet)
	if got := int(fired.Load()); got != redlineGuardSettleTurns {
		t.Fatalf("follow-up turns after a progress notification = %d, want %d", got, redlineGuardSettleTurns)
	}
	seat, ok := redlineGuardSeatOf(store, progress)
	if !ok {
		t.Fatalf("the progress notification left no mailbox seat, want it still owed and unclaimed")
	}
	if seat.Claim != state.ClaimPending {
		t.Fatalf("claim of the progress entry = %d, want %d: a notification that is not terminal is never claimed", seat.Claim, state.ClaimPending)
	}

	if err := store.Consume(ctx, addr, redlineGuardInstance, 1); !errors.Is(err, state.ErrClaimSettled) {
		t.Fatalf("second consumption of the settled entry = %v, want %v", err, state.ErrClaimSettled)
	}
}

// redlineGuardSlotAdmitsOneDecision races the decisions of one site against the
// occupancy ledger: exactly one occupant is admitted, every other decision is
// refused by name, and the slot is per site and released only by its holder.
func redlineGuardSlotAdmitsOneDecision(t *testing.T) {
	const racers = 8
	if racers < 2 {
		t.Fatalf("the slot guard races %d decisions, want at least 2", racers)
	}
	slots := state.NewOccupancy()
	addr := event.Address{AgentID: "agent-guard", SessionID: "session-guard-slot"}
	other := event.Address{AgentID: "agent-guard", SessionID: "session-guard-slot-other"}

	var winners atomic.Int32
	var refused atomic.Int32
	var mu sync.Mutex
	var winner event.InvocationID
	var unexpected []error
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			invocation := event.InvocationID(fmt.Sprintf("invocation-guard-%d", i))
			decision := event.DecisionID(fmt.Sprintf("decision-guard-%d", i))
			<-start
			err := slots.Occupy(addr, redlineGuardInstance, invocation, decision)
			switch {
			case err == nil:
				winners.Add(1)
				mu.Lock()
				winner = invocation
				mu.Unlock()
			case errors.Is(err, state.ErrSlotHeld):
				refused.Add(1)
			default:
				mu.Lock()
				unexpected = append(unexpected, err)
				mu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if len(unexpected) != 0 {
		t.Fatalf("occupancy refusals other than the named one = %v, want every refusal to name a held slot", unexpected)
	}
	if got := int(winners.Load()); got != redlineGuardSlotWinners {
		t.Fatalf("effective model decisions admitted at one site = %d, want %d", got, redlineGuardSlotWinners)
	}
	if got, want := int(refused.Load()), racers-redlineGuardSlotWinners; got != want {
		t.Fatalf("decisions refused beside the one occupant = %d, want %d", got, want)
	}
	holder, decision, ok := slots.Slot(addr, redlineGuardInstance)
	if !ok {
		t.Fatalf("the slot of the site reports no occupant, want the one decision it admitted")
	}
	if holder != winner {
		t.Fatalf("slot occupant = %q, want the admitted decision %q", holder, winner)
	}
	if decision == "" {
		t.Fatalf("slot reports no decision identity, want the decision of the admitted occupant")
	}

	if err := slots.Occupy(other, redlineGuardInstance, "invocation-guard-other", "decision-guard-other"); err != nil {
		t.Fatalf("the second session of the same agent = %v, want its own slot", err)
	}

	if err := slots.Vacate(addr, redlineGuardInstance, "invocation-guard-outsider"); !errors.Is(err, state.ErrSlotNotHeld) {
		t.Fatalf("vacate by an invocation holding no slot = %v, want %v", err, state.ErrSlotNotHeld)
	}
	if holder, _, ok := slots.Slot(addr, redlineGuardInstance); !ok || holder != winner {
		t.Fatalf("slot occupant after a refused vacate = %q, want the admitted decision %q", holder, winner)
	}
	if err := slots.Vacate(addr, redlineGuardInstance, winner); err != nil {
		t.Fatalf("vacate by the slot holder = %v, want it released", err)
	}
	if _, _, ok := slots.Slot(addr, redlineGuardInstance); ok {
		t.Fatalf("the slot of the site is still held after its holder vacated it")
	}
	if err := slots.Occupy(addr, redlineGuardInstance, "invocation-guard-next", "decision-guard-next"); err != nil {
		t.Fatalf("the decision behind a vacated slot = %v, want it admitted", err)
	}
}

// redlineGuardEmptyWhitelistRefusesEveryTarget sends to every kind of target
// through the first gate set of the delivery face, the deliver_to white list the
// host document declares: a host that declares none refuses every target by name
// and no target chain reads the body, while the same gate delivers to a target
// the document declared.
//
// The seam under the guard is the one that owns the white list tier today: the
// cross-session delivery face answers through the tool a reflecting agent
// carries, so the guard boots the deployment and reads the receipt the model
// read back. A target that is bound and running is refused beside the empty
// table too, so the refusal is the table and never the residency of the target.
func redlineGuardEmptyWhitelistRefusesEveryTarget(t *testing.T) {
	const (
		boundCall   = "call-guard-bound"
		unboundCall = "call-guard-unbound"
		entryCall   = "call-guard-entry"
	)
	targets := []event.Address{
		{AgentID: hostDrivePeerAgent, SessionID: hostDrivePeerSession},
		{AgentID: hostDrivePeerAgent, SessionID: "session-guard-unbound"},
		{AgentID: hostDriveEntryAgent, SessionID: "session-guard-entry"},
	}
	if len(targets) == 0 {
		t.Fatalf("the white list guard sends to %d targets, want at least one", len(targets))
	}

	empty := newHostDriveSceneWith(t, redlineGuardNoWhitelistDocument)
	empty.bindPeer(t)
	calls := make([]wiringCall, 0, len(targets))
	for i, target := range targets {
		calls = append(calls, wiringCall{
			id:        []string{boundCall, unboundCall, entryCall}[i],
			name:      hostDriveToolSend,
			arguments: redlineGuardSendArguments(string(target.AgentID), string(target.SessionID)),
		})
	}
	empty.invokeMeditation(t, boundCall, wiringToolCallsAnswer(calls...))

	delivered := 0
	for i, target := range targets {
		receipt := empty.awaitReceipt(t, calls[i].id)
		if receipt == hostDriveReceiptOK {
			delivered++
			t.Logf("the empty white list delivered to %+v", target)
			continue
		}
		if !strings.HasPrefix(receipt, hostSendDeniedPrefixText) || !strings.Contains(receipt, "declares no deliver_to white list") {
			t.Fatalf("receipt of the send to %+v = %q, want a refusal naming the empty deliver_to table", target, receipt)
		}
	}
	if got := delivered; got != redlineGuardWhitelistAllowed {
		t.Fatalf("targets an unregistered deliver_to table delivered to = %d, want %d", got, redlineGuardWhitelistAllowed)
	}
	if facts := empty.bodyFacts(t); len(facts) != 0 {
		t.Fatalf("facts of the peer chain beside the empty white list = %d, want none: a refused body reaches no target", len(facts))
	}
	if rounds := empty.peerRoundsCarrying(t); len(rounds) != 0 {
		t.Fatalf("peer rounds carrying a body refused beside the empty white list = %d, want none", len(rounds))
	}

	open := newHostDriveScene(t)
	open.bindPeer(t)
	open.invokeMeditation(t, boundCall, hostDrivePlainSend(boundCall))
	if receipt := open.awaitReceipt(t, boundCall); receipt != hostDriveReceiptOK {
		t.Fatalf("receipt of the send to a declared target = %q, want the bare %q:\n%s", receipt, hostDriveReceiptOK, open.describe())
	}
	open.awaitPeerBody(t)
}

// hostSendDeniedPrefixText is the opening of the refusal receipt the send tool
// answers with, read back from the message the model carried.
const hostSendDeniedPrefixText = "[send_denied] "

// redlineGuardSendArguments renders the arguments of one send call naming a
// session destination.
func redlineGuardSendArguments(agent, session string) string {
	return `{"target":{"agent":` + hostDriveQuoted(agent) + `,"session":` + hostDriveQuoted(session) +
		`},"content":` + hostDriveQuoted(hostDriveBody) + `}`
}

// redlineGuardNoWhitelistDocument renders the guard document whose reflecting
// agent carries the send tool beside no declared white list at all: it enables
// no meditation, so the deliver_to table the first gate reads is empty.
func redlineGuardNoWhitelistDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: " + hostDriveEntryAgent + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + hostDriveEntryModel + "\n")
	b.WriteString("prompt_dir: " + root + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  " + hostDriveEntryAgent + ":\n")
	b.WriteString("    model: " + hostDriveEntryModel + "\n")
	b.WriteString("  " + hostDrivePeerAgent + ":\n")
	b.WriteString("    model: " + hostDrivePeerModel + "\n")
	b.WriteString("  " + hostDriveMeditatorAgent + ":\n")
	b.WriteString("    model: " + hostDriveMeditatorModel + "\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: " + hostDriveToolSend + " }\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// redlineGuardStore establishes the guard site beside the notifications it
// routes there, so a mailbox account answers the way one run's account does.
func redlineGuardStore(t *testing.T, ctx context.Context, addr event.Address, taskIDs ...event.TaskID) *state.MemoryStore {
	t.Helper()
	authority := state.NewAuthority()
	if err := authority.GrantRead(ctx, redlineGuardSubject, addr); err != nil {
		t.Fatalf("record the read grant of the guard subject: %v", err)
	}
	store := state.NewMemoryStore(authority)
	if _, err := store.Establish(ctx, addr, redlineGuardInstance); err != nil {
		t.Fatalf("establish the guard site: %v", err)
	}
	for _, id := range taskIDs {
		if err := store.TrackDelivery(ctx, id, task.HandoffTarget{Target: addr, TargetInstance: redlineGuardInstance}); err != nil {
			t.Fatalf("register the delivery of %q: %v", id, err)
		}
	}
	return store
}

// redlineGuardEntry reads one mailbox entry of the guard site back.
func redlineGuardEntry(ctx context.Context, store *state.MemoryStore, addr event.Address, seq state.AcceptSeq) (state.MailboxEntry, error) {
	session, err := store.Site(ctx, redlineGuardSubject, addr, redlineGuardInstance)
	if err != nil {
		return state.MailboxEntry{}, err
	}
	for _, entry := range session.Mailbox {
		if entry.AcceptSeq == seq {
			return entry, nil
		}
	}
	return state.MailboxEntry{}, fmt.Errorf("no mailbox entry at acceptance sequence %d", seq)
}

// redlineGuardEntryConsumed reports whether one mailbox entry already landed its
// consumption receipt, which is the account position the settle pump closes.
func redlineGuardEntryConsumed(ctx context.Context, store *state.MemoryStore, addr event.Address) bool {
	entry, err := redlineGuardEntry(ctx, store, addr, 1)
	if err != nil {
		return false
	}
	return entry.Claim == state.ClaimConsumed && entry.Receipt.Completed
}

// redlineGuardSeatOf reports the seat one notification still owes, or that the
// account holds none.
func redlineGuardSeatOf(store *state.MemoryStore, notice task.Notice) (state.WorkSeat, bool) {
	for _, seat := range store.Unconsumed() {
		if seat.Notice.TaskID == notice.TaskID && seat.Notice.Kind == notice.Kind {
			return seat, true
		}
	}
	return state.WorkSeat{}, false
}

// redlineGuardAwait waits for one account position inside the guard window, so a
// semantic that never lands answers the named gap instead of a timeout alone.
func redlineGuardAwait(t *testing.T, what string, landed func() bool) {
	t.Helper()
	deadline := time.Now().Add(redlineGuardWindow)
	for time.Now().Before(deadline) {
		if landed() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waited %s for %s, want it landed", redlineGuardWindow, what)
}
