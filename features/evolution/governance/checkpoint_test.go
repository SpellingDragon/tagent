// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
package governance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// sliceLog is one governance store in memory: the pairs written beside a log of
// the scans it answered, so a check reads "how much of the stream did the
// recovery pay for" as a number. A parked log holds every write until the check
// releases it, so the read a cut states can be timed against a commit of its own.
type sliceLog struct {
	mu     sync.Mutex
	stored map[string][]byte
	scans  []string
	parked chan struct{}
	writes chan struct{}
}

// newSliceLog returns an empty governance store.
func newSliceLog() *sliceLog {
	return &sliceLog{stored: map[string][]byte{}}
}

// Put stores one governance event under its key.
func (l *sliceLog) Put(_ context.Context, key string, value []byte) error {
	l.mu.Lock()
	parked := l.parked
	l.mu.Unlock()
	if parked != nil {
		l.mu.Lock()
		if l.writes != nil {
			select {
			case l.writes <- struct{}{}:
			default:
			}
		}
		l.mu.Unlock()
		<-parked
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stored[key] = append([]byte(nil), value...)
	return nil
}

// Scan answers every stored pair under the prefix and records the read.
func (l *sliceLog) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.scans = append(l.scans, prefix)
	found := map[string][]byte{}
	for key, value := range l.stored {
		if strings.HasPrefix(key, prefix) {
			found[key] = value
		}
	}
	return found, nil
}

// Sync completes the write barrier of one staged write.
func (l *sliceLog) Sync(context.Context) error { return nil }

// park holds every write until the returned release is called, and reports the
// write a caller parks in flight.
func (l *sliceLog) park() (release func(), inFlight func()) {
	gate := make(chan struct{})
	arrived := make(chan struct{}, 1)
	l.mu.Lock()
	l.parked, l.writes = gate, arrived
	l.mu.Unlock()
	return func() {
			l.mu.Lock()
			l.parked, l.writes = nil, nil
			l.mu.Unlock()
			close(gate)
		}, func() {
			select {
			case <-arrived:
			case <-time.After(5 * time.Second):
			}
		}
}

// scansSince returns the reads the store answered from one mark on.
func (l *sliceLog) scansSince(mark int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if mark > len(l.scans) {
		return nil
	}
	return append([]string(nil), l.scans[mark:]...)
}

// mark reports where the read log stands, so a check states the reads one step
// answered.
func (l *sliceLog) mark() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.scans)
}

// keys returns every stored key in order.
func (l *sliceLog) keys() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.stored))
	for key := range l.stored {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// keysOf returns the stored keys of one stream in order.
func (l *sliceLog) keysOf(prefix string) []string {
	var out []string
	for _, key := range l.keys() {
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	return out
}

// sliceFixture is one governance state beside the store it writes to.
type sliceFixture struct {
	log    *sliceLog
	ledger *DenialLedger
	goals  *GoalRegistry
	book   *approval.ApprovalManager
	seat   *StateSeat
}

// newSliceFixture returns a governance state bound to a fresh store.
func newSliceFixture(t *testing.T) *sliceFixture {
	t.Helper()
	return newSliceFixtureAt(t, newSliceLog(), newDenialLedgerOn(t), newGoalRegistryOn(t))
}

// newDenialLedgerOn returns a ledger bound to one store.
func newDenialLedgerOn(t *testing.T) *DenialLedger {
	t.Helper()
	ledger := NewDenialLedger()
	return ledger
}

// newGoalRegistryOn returns a goal registry bound to one store.
func newGoalRegistryOn(t *testing.T) *GoalRegistry {
	t.Helper()
	return NewGoalRegistry()
}

// newSliceFixtureAt builds one fixture over a stated store, ledger and registry.
func newSliceFixtureAt(t *testing.T, log *sliceLog, ledger *DenialLedger, goals *GoalRegistry) *sliceFixture {
	t.Helper()
	return &sliceFixture{log: log, ledger: ledger, goals: goals, seat: NewStateSeat(ledger, goals, nil)}
}

// recordDenial writes one ledger record and fails the check when the write is
// lost.
func (f *sliceFixture) recordDenial(t *testing.T, tool string) {
	t.Helper()
	f.ledger.Record(DenialRecord{
		Subtype: SubtypeDenial, ToolName: tool, Level: plugin.RiskHigh,
		RuleID: "no-" + tool, Reason: "refused " + tool,
	})
}

// TestGovernanceSliceSnapshot pins the governance state slice.
//   - positive: the cut states the tail of both streams, the index they folded and the approval book
//   - negative: the read states the state the holder already derived and never waits on a commit of its own
//   - consumer: the framework state seam mounts the slice under its own name and states it in a cut
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestGovernanceSliceSnapshot(t *testing.T) {
	t.Run("the cut states both tails, the derived index and the book", func(t *testing.T) {
		ctx := context.Background()
		log := newSliceLog()
		ledger, goals := NewDenialLedger(), NewGoalRegistry()
		if err := ledger.Bind(ctx, log); err != nil {
			t.Fatalf("bind the ledger to the slice log: %v", err)
		}
		if err := goals.Bind(ctx, log); err != nil {
			t.Fatalf("bind the registry to the slice log: %v", err)
		}
		book := approval.NewApprovalManager("", 0)
		seat := NewStateSeat(ledger, goals, book)

		ledger.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: "shell_exec", Level: plugin.RiskHigh, RuleID: "no-deploy", Reason: "refused"})
		ledger.Record(DenialRecord{Subtype: SubtypeAudit, ToolName: "read_file", Level: plugin.RiskLow})
		declared := goals.Declare("ship the release", "entry", 0)
		if !goals.Resolve(declared, GoalAchieved) {
			t.Fatalf("resolve goal %s: the registry must accept the resolution", declared)
		}
		if _, err := book.Request(ctx, "approval-cut-1", "shell_exec", []byte(`{"cmd":"deploy"}`), plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
			t.Fatalf("register one approval request: %v", err)
		}

		payload, err := seat.SnapshotState(ctx)
		if err != nil {
			t.Fatalf("state the governance cut: %v", err)
		}
		var wire stateSliceWire
		if err := json.Unmarshal(payload, &wire); err != nil {
			t.Fatalf("the stated cut is no governance state: %v (%s)", err, payload)
		}
		if wire.Version != stateSliceVersion {
			t.Fatalf("stated layout = %d, want the layout this code reads", wire.Version)
		}
		if wire.DenialSeq != 2 {
			t.Fatalf("stated denial stream = tail %d, want the two records the ledger folded", wire.DenialSeq)
		}
		if wire.GoalSeq != 2 || wire.GoalOrder != 1 {
			t.Fatalf("stated goal stream = tail %d order %d, want the declaration and its resolution the registry wrote", wire.GoalSeq, wire.GoalOrder)
		}
		if len(wire.Goals) != 0 {
			t.Fatalf("stated goals = %+v, want none: %s is resolved, so the cut states the work in force and its tail numbers that record", wire.Goals, declared)
		}
		if len(wire.Approvals) != 1 || wire.Approvals[0].ID != "approval-cut-1" {
			t.Fatalf("stated approvals = %+v, want the request the book holds", wire.Approvals)
		}

		restarted := newSliceFixture(t)
		if err := restarted.seat.RestoreState(ctx, payload); err != nil {
			t.Fatalf("restore the stated governance cut: %v", err)
		}
		if got := restarted.ledger.Count(); got != 0 {
			t.Fatalf("records beside the restored cut before its stream is bound = %d, want none: the cut states the stream's tail, never its history", got)
		}
		if err := restarted.ledger.Bind(ctx, log); err != nil {
			t.Fatalf("bind the stream the restored cut numbers: %v", err)
		}
		records := restarted.ledger.Query(0)
		if len(records) != 2 || records[0].ToolName != "read_file" || records[1].ToolName != "shell_exec" {
			t.Fatalf("restored records = %+v, want the two the cut numbers in the order they happened", records)
		}
		if goals := restarted.goals.List(); len(goals) != 0 {
			t.Fatalf("goals beside the restored cut before its stream is bound = %+v, want none: the cut states the work in force, never the history its tail numbers", goals)
		}
		if err := restarted.goals.Bind(ctx, log); err != nil {
			t.Fatalf("bind the goal stream the restored cut numbers: %v", err)
		}
		if goals := restarted.goals.List(); len(goals) != 1 || goals[0].ID != declared || goals[0].Status != GoalAchieved {
			t.Fatalf("restored goals = %+v, want %s resolved, answered from the stream behind the cut", goals, declared)
		}
		if active := restarted.goals.HasActive(); active {
			t.Fatal("restored registry reports an active goal, want the resolution the cut stated")
		}
		if got := restarted.ledger.Count(); got != 2 {
			t.Fatalf("restored record count = %d, want the index the stream behind the cut holds and no tail past it", got)
		}
	})

	t.Run("the read does not wait on a commit of its own", func(t *testing.T) {
		ctx := context.Background()
		log := newSliceLog()
		ledger, goals := NewDenialLedger(), NewGoalRegistry()
		if err := ledger.Bind(ctx, log); err != nil {
			t.Fatalf("bind the ledger to the slice log: %v", err)
		}
		if err := goals.Bind(ctx, log); err != nil {
			t.Fatalf("bind the registry to the slice log: %v", err)
		}
		seat := NewStateSeat(ledger, goals, nil)

		release, inFlight := log.park()
		recorded := make(chan struct{})
		go func() {
			defer close(recorded)
			ledger.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: "shell_exec", Level: plugin.RiskHigh})
		}()
		inFlight()

		stated := make(chan []byte, 1)
		go func() {
			payload, err := seat.SnapshotState(ctx)
			if err != nil {
				stated <- nil
				return
			}
			stated <- payload
		}()
		select {
		case payload := <-stated:
			if len(payload) == 0 {
				t.Fatal("the cut states nothing beside a record in flight")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the cut waited on the write it states beside: the snapshot is a pure read")
		}
		release()
		<-recorded
	})

	t.Run("the seam mounts the slice by its own name", func(t *testing.T) {
		fixture := newSliceFixture(t)
		registry := state.NewStateRegistry()
		disposer, err := registry.Register(fixture.seat)
		if err != nil {
			t.Fatalf("mount the governance slice on the state seam: %v", err)
		}
		names := registry.Names()
		if len(names) != 1 || names[0] != stateSliceName {
			t.Fatalf("mounted slices = %v, want the governance slice under its own name", names)
		}
		slices, err := registry.SnapshotState(context.Background())
		if err != nil {
			t.Fatalf("state the mounted slices: %v", err)
		}
		if len(slices) != 1 || slices[0].Name != stateSliceName {
			t.Fatalf("stated slices = %+v, want the governance slice", slices)
		}
		disposer()
		if names := registry.Names(); len(names) != 0 {
			t.Fatalf("mounted slices after the disposer = %v, want none", names)
		}
	})
}

// TestGovernanceRecoveryFromStateSlice pins the governance recovery a boot runs.
//   - positive: a cut plus the events past it rebuilds the same state the whole stream folds
//   - negative: the read pays for the tail past the cut alone, and a cut this code cannot read is refused by name instead of resetting the streams to zero
//   - consumer: the ledger and the registry read the cut through the state seam, never the whole stream behind it
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestGovernanceRecoveryFromStateSlice(t *testing.T) {
	ctx := context.Background()
	log := newSliceLog()
	ledger, goals := NewDenialLedger(), NewGoalRegistry()
	if err := ledger.Bind(ctx, log); err != nil {
		t.Fatalf("bind the ledger to the slice log: %v", err)
	}
	if err := goals.Bind(ctx, log); err != nil {
		t.Fatalf("bind the registry to the slice log: %v", err)
	}
	seat := NewStateSeat(ledger, goals, nil)

	tools := []string{"shell_exec", "read_file", "write_file", "delete_file", "edit_file"}
	for _, tool := range tools {
		ledger.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: tool, Level: plugin.RiskHigh, RuleID: "no-" + tool})
	}
	goals.Declare("ship the release", "entry", 0)
	cut, err := seat.SnapshotState(ctx)
	if err != nil {
		t.Fatalf("state the cut over the first five records: %v", err)
	}
	for _, tool := range []string{"spawn", "kill", "mount"} {
		ledger.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: tool, Level: plugin.RiskHigh, RuleID: "no-" + tool})
		_ = tool
	}
	if got := len(log.keysOf(denialEventPrefix)); got != 8 {
		t.Fatalf("stored denial events = %d, want the eight the first process wrote", got)
	}

	restarted := newSliceFixture(t)
	if err := restarted.seat.RestoreState(ctx, cut); err != nil {
		t.Fatalf("restore the stated cut: %v", err)
	}
	mark := log.mark()
	if err := restarted.seat.Bind(ctx, log); err != nil {
		t.Fatalf("bind the durable face past the stated cut: %v", err)
	}
	reads := log.scansSince(mark)
	wantReads := []string{
		denialEventPrefix + "0000000000000000006",
		denialEventPrefix + "0000000000000000007",
		denialEventPrefix + "0000000000000000008",
		denialEventPrefix + "0000000000000000009",
		goalEventPrefix + "0000000000000000002",
	}
	if strings.Join(reads, " ") != strings.Join(wantReads, " ") {
		t.Fatalf("recovery reads past the cut = %v, want the keys each tail names and the miss that ends it: %v", reads, wantReads)
	}
	for _, read := range reads {
		if read == denialEventPrefix || read == goalEventPrefix {
			t.Fatalf("recovery read the whole stream (%q), want the tail past the cut alone", read)
		}
	}
	if got := restarted.ledger.Count(); got != 8 {
		t.Fatalf("restored records = %d, want the five the cut stated and the three past it", got)
	}
	newest := restarted.ledger.Query(1)
	if len(newest) != 1 || newest[0].ToolName != "mount" {
		t.Fatalf("newest restored record = %+v, want the last event the tail folded", newest)
	}

	restarted.recordDenial(t, "reboot")
	wrote := log.keysOf(denialEventPrefix)
	if got := wrote[len(wrote)-1]; got != fmt.Sprintf("%s%019d", denialEventPrefix, 9) {
		t.Fatalf("next denial event key = %q, want the sequence past the eight already written", got)
	}

	full := newSliceFixture(t)
	if err := full.seat.RestoreState(ctx, mustStateCut(t, restarted.seat)); err != nil {
		t.Fatalf("restore the cut stated after the tail was folded: %v", err)
	}
	mark = log.mark()
	if err := full.seat.Bind(ctx, log); err != nil {
		t.Fatalf("bind a durable face beside a cut that states it: %v", err)
	}
	emptyTail := []string{
		denialEventPrefix + "0000000000000000010",
		goalEventPrefix + "0000000000000000002",
	}
	if reads := log.scansSince(mark); strings.Join(reads, " ") != strings.Join(emptyTail, " ") {
		t.Fatalf("recovery reads beside a cut that already states the stream = %v, want the miss that ends each walk and no key behind the cut: the recovery pays for the state it holds, never for the stream behind it", reads)
	}
	if got := full.ledger.Count(); got != 9 {
		t.Fatalf("records beside a cut that states the stream = %d, want the nine the cut states", got)
	}

	broken := append([]byte(nil), cut...)
	broken = append(broken, []byte("!")...)
	if err := restarted.seat.RestoreState(ctx, broken); err == nil {
		t.Fatal("restore of a cut that states no governance state = nil, want the refusal by name")
	}
	stale := fmt.Sprintf(`{"version":%d}`, stateSliceVersion+1)
	if err := restarted.seat.RestoreState(ctx, []byte(stale)); err == nil || !strings.Contains(err.Error(), "layout") {
		t.Fatalf("restore of an unknown layout = %v, want the refusal by name", err)
	}
	restarted.recordDenial(t, "remount")
	wrote = log.keysOf(denialEventPrefix)
	if got := wrote[len(wrote)-1]; got != fmt.Sprintf("%s%019d", denialEventPrefix, 10) {
		t.Fatalf("next denial event key after the refused restores = %q, want the sequence past the nine written: the refusal left the stated watermark standing, where a reset to zero would hand the streams' keys to a writer that re-keys over the events already stored", got)
	}
}

// failingLog is one governance store whose reads fail: the store a boot cannot
// tell from an empty one, and the report a silent fold would swallow.
type failingLog struct {
	sliceLog
	err error
}

// Scan answers the read failure of the store.
func (l *failingLog) Scan(context.Context, string) (map[string][]byte, error) {
	return nil, l.err
}

// mustStateCut states one governance cut and fails the check when it cannot.
func mustStateCut(t *testing.T, seat *StateSeat) []byte {
	t.Helper()
	payload, err := seat.SnapshotState(context.Background())
	if err != nil {
		t.Fatalf("state the governance cut: %v", err)
	}
	return payload
}

// goalOnRecord reports the goal the registry answers under one identity, failing
// the check when no list names it.
func goalOnRecord(t *testing.T, goals *GoalRegistry, id string) *Goal {
	t.Helper()
	for _, goal := range goals.List() {
		if goal.ID == id {
			return goal
		}
	}
	t.Fatalf("goal %s is answered by no list of the registry", id)
	return nil
}

// TestGovernanceBindReportsAReadFailure pins the report of a stream a boot could not read.
//   - positive: the cut a boot restored keeps its watermark, so a refused read is not a zero state
//   - negative: a store whose read fails answers an error instead of folding nothing, so no writer re-keys over the events already stored
//   - consumer: the state seam's bind answers that error to the boot that seats it
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestGovernanceBindReportsAReadFailure(t *testing.T) {
	ctx := context.Background()
	seat := NewStateSeat(NewDenialLedger(), nil, nil)
	cut := fmt.Sprintf(`{"version":%d,"denial_seq":7}`, stateSliceVersion)
	if err := seat.RestoreState(ctx, []byte(cut)); err != nil {
		t.Fatalf("restore the stated cut: %v", err)
	}
	broken := &failingLog{sliceLog: sliceLog{stored: map[string][]byte{}}, err: errors.New("the store answered no read")}
	err := seat.Bind(ctx, broken)
	if err == nil {
		t.Fatal("bind a store whose read fails = nil, want the read failure reported to the boot")
	}
	if !strings.Contains(err.Error(), "denial") {
		t.Fatalf("the report of a store whose read fails = %v, want the denial stream named", err)
	}
	seat.ledger.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: "shell_exec", Level: plugin.RiskHigh})
	if got := seat.ledger.evtSeq; got != 8 {
		t.Fatalf("stream sequence after the refused read = %d, want the stated cut continued instead of reset: a watermark reset to zero would re-key over the seven events the cut states", got)
	}
}

// TestGovernanceApprovalRestoredFromStateSlice pins the approval boot restore.
//   - positive: a stated book is seated with the questions and rulings the process filed
//   - negative: the restore opens no request file, so a book whose directory is gone still reports the hold it stated
//   - consumer: the approval channel reads the same presence and the same ruling after the restore
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestGovernanceApprovalRestoredFromStateSlice(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	book := approval.NewApprovalManager(dir, 0, approval.WithApprovalSeat())
	args := []byte(`{"cmd":"deploy"}`)
	held, err := book.Request(ctx, "approval-restore-1", "shell_exec", args, plugin.RiskCritical, "no-deploy", "critical risk awaits approval")
	if err != nil {
		t.Fatalf("register one approval request: %v", err)
	}
	if _, err := book.Request(ctx, "approval-restore-2", "shell_exec", []byte(`{"cmd":"rollback"}`), plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the second approval request: %v", err)
	}
	if _, err := book.Decide("approval-restore-2", approval.ApprovalApproved, "operator"); err != nil {
		t.Fatalf("rule the second request: %v", err)
	}
	seat := NewStateSeat(NewDenialLedger(), NewGoalRegistry(), book)
	cut := mustStateCut(t, seat)

	if err := os.RemoveAll(filepath.Join(dir, "approvals")); err != nil {
		t.Fatalf("withdraw the request files under %s: %v", dir, err)
	}

	restartedBook := approval.NewApprovalManager(dir, 0, approval.WithApprovalSeat())
	restarted := NewStateSeat(NewDenialLedger(), NewGoalRegistry(), restartedBook)
	if err := restarted.RestoreState(ctx, cut); err != nil {
		t.Fatalf("restore the stated approval book: %v", err)
	}
	presence := restartedBook.Pending()
	if len(presence) != 1 || presence[0].ID != held.ID {
		t.Fatalf("pending requests beside the restored book = %+v, want the one hold that still waits, read from the stated book now that the per-request files are gone", presence)
	}
	approved := restartedBook.Lookup("shell_exec", approval.ArgsDigest([]byte(`{"cmd":"rollback"}`)))
	if approved == nil || approved.Status != approval.ApprovalApproved || approved.DecidedBy != "operator" {
		t.Fatalf("ruling read beside the restored book = %+v, want the approval the process stated", approved)
	}
	if decided := restartedBook.Lookup("shell_exec", approval.ArgsDigest(args)); decided == nil || decided.Status != approval.ApprovalPending {
		t.Fatalf("hold read beside the restored book = %+v, want the pending request it stated", decided)
	}

	expired := mustStateCut(t, seat)
	var wire stateSliceWire
	if err := json.Unmarshal(expired, &wire); err != nil {
		t.Fatalf("read the stated book: %v", err)
	}
	for index := range wire.Approvals {
		wire.Approvals[index].ExpiresMs = time.Now().Add(-time.Hour).UnixMilli()
	}
	stale, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("state the expired book: %v", err)
	}
	afterExpiry := approval.NewApprovalManager(dir, 0, approval.WithApprovalSeat())
	if err := NewStateSeat(NewDenialLedger(), NewGoalRegistry(), afterExpiry).RestoreState(ctx, stale); err != nil {
		t.Fatalf("restore the expired book: %v", err)
	}
	if presence := afterExpiry.Pending(); len(presence) != 0 {
		t.Fatalf("pending requests beside an expired cut = %+v, want none resurrected: the restore drops the hold the clock already ended instead of handing it a second life", presence)
	}
}

// statedWire reads one stated cut as the payload the seam carries.
func statedWire(t *testing.T, payload []byte) stateSliceWire {
	t.Helper()
	var wire stateSliceWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("the stated cut is no governance state: %v (%s)", err, payload)
	}
	return wire
}

// restatedWire renders one wire back into a cut payload.
func restatedWire(t *testing.T, wire stateSliceWire) []byte {
	t.Helper()
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("state the tampered cut: %v", err)
	}
	return raw
}

// TestGovernanceSliceStatesTheValidStateNotTheHistory pins what one cut states against the denial history.
//   - positive: the records and bytes of a cut follow the live work, so five generations of history state one budget
//   - negative: the same live work over five generations must not state five times the index
//   - consumer: StateSeat.SnapshotState as the framework state seam states the governance payload
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestGovernanceSliceStatesTheValidStateNotTheHistory(t *testing.T) {
	ctx := context.Background()
	log := newSliceLog()
	ledger, goals := NewDenialLedger(), NewGoalRegistry()
	if err := ledger.Bind(ctx, log); err != nil {
		t.Fatalf("bind the ledger: %v", err)
	}
	if err := goals.Bind(ctx, log); err != nil {
		t.Fatalf("bind the registry: %v", err)
	}
	clock := newObservationClock()
	book := approval.NewApprovalManager("", time.Hour, approval.WithApprovalClock(clock.Now))
	seat := NewStateSeat(ledger, goals, book)

	goals.Declare("ship the release", "entry", 0)
	if _, err := book.Request(ctx, "live-hold", "shell_exec", []byte(`{"cmd":"deploy"}`),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the live hold: %v", err)
	}

	const generations = 5
	const perGeneration = 4
	var first, last []byte
	for gen := 1; gen <= generations; gen++ {
		for i := 0; i < perGeneration; i++ {
			ledger.Record(DenialRecord{
				Subtype: SubtypeDenial, ToolName: fmt.Sprintf("tool-%d-%d", gen, i),
				Level: plugin.RiskHigh, RuleID: "no-history", Reason: "refused",
			})
		}
		payload, err := seat.SnapshotState(ctx)
		if err != nil {
			t.Fatalf("state the cut of generation %d: %v", gen, err)
		}
		wire := statedWire(t, payload)
		if wire.DenialSeq != int64(gen*perGeneration) {
			t.Fatalf("generation %d states denial tail %d, want the %d events the stream holds", gen, wire.DenialSeq, gen*perGeneration)
		}
		if gen == 1 {
			first = payload
		}
		last = payload
		t.Logf("generation %d: stated approvals=%d bytes=%d denial tail=%d; stored denial events=%d",
			gen, len(wire.Approvals), len(payload), wire.DenialSeq, len(log.keysOf(denialEventPrefix)))
	}

	if got := len(log.keysOf(denialEventPrefix)); got != generations*perGeneration {
		t.Fatalf("stored denial events = %d, want %d: the fixture did not grow the history it measures",
			got, generations*perGeneration)
	}
	if !goals.HasActive() {
		t.Fatal("the live goal is not in flight, so the constant live work guard proves nothing")
	}
	if pending := book.Pending(); len(pending) != 1 || pending[0].ID != "live-hold" {
		t.Fatalf("live holds = %+v, want exactly the one hold the fixture registered", pending)
	}
	if len(first) == 0 {
		t.Fatal("the first generation stated nothing, so the growth bound has an empty denominator")
	}

	if len(last) > len(first)*2 {
		t.Fatalf("the cut grew with the denial history: %d -> %d bytes over %d generations while the live work stayed "+
			"1 goal in flight and 1 hold — a cut states the live state, never the history its tail numbers; "+
			"ratio %d/%d equals the generation ratio %d/1",
			len(first), len(last), generations, len(last), len(first), generations)
	}
	if wire, was := statedWire(t, first), statedWire(t, last); len(wire.Approvals) != len(was.Approvals) {
		t.Fatalf("stated approval entries grew with the denial history: %d -> %d", len(wire.Approvals), len(was.Approvals))
	}

	restated := newSliceFixtureAt(t, log, NewDenialLedger(), NewGoalRegistry())
	if err := restated.seat.RestoreState(ctx, last); err != nil {
		t.Fatalf("restore the last cut: %v", err)
	}
	if got := restated.ledger.Count(); got != 0 {
		t.Fatalf("records beside the restored cut before its stream is bound = %d, want none: the cut numbers the stream instead of carrying it", got)
	}
	if err := restated.seat.Bind(ctx, log); err != nil {
		t.Fatalf("bind the stream the restored cut numbers: %v", err)
	}
	if got := restated.ledger.Count(); got != generations*perGeneration {
		t.Fatalf("records the restored cut and its stream answer = %d, want the %d the process that cut it held: "+
			"a cut that states the live state must not lose the history it numbers", got, generations*perGeneration)
	}
}

// criticalCall renders the one critical call these probes rule.
func criticalCall(tool, args string) plugin.CallPass {
	return plugin.CallPass{
		Final: trpcmodel.ToolCall{
			ID:       "call-" + tool,
			Function: trpcmodel.FunctionDefinitionParam{Name: tool, Arguments: []byte(args)},
		},
		Risk: plugin.RiskCritical,
	}
}

// governanceGate opens the production gate over one book, ledger and registry.
func governanceGate(book *approval.ApprovalManager, ledger *DenialLedger, goals *GoalRegistry) *Policy {
	return Open(WithApprovals(book), WithLedger(ledger), WithGoals(goals))
}

// withdrawApprovalFiles removes the per-request file mirror, leaving the stated cut as the only record.
func withdrawApprovalFiles(t *testing.T, dir string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(dir, "approvals")); err != nil {
		t.Fatalf("withdraw the request files under %s: %v", dir, err)
	}
}

// TestGovernanceSliceRefusesToDropAnOpenApproval pins the identity a dropped open hold must keep answering.
//   - mutation: one unexpired pending approval is removed from the payload and the request files are withdrawn
//   - positive: the original identity still resolves a ruling and the gate parks naming it, never a freshly minted hold
//   - negative: the untampered cut resolves the same identity, and the tampered restore seated nothing before the chain bound
//   - consumer: Policy.Judge rules the call and ApprovalManager.Decide answers the identity the approver was handed
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestGovernanceSliceRefusesToDropAnOpenApproval(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	clock := newObservationClock()
	store := newSliceLog()
	book := approval.NewApprovalManager(dir, time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
	if err := book.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	args := `{"cmd":"deploy"}`
	held, err := book.Request(ctx, "open-hold", "shell_exec", []byte(args),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval")
	if err != nil {
		t.Fatalf("register the open hold: %v", err)
	}
	seat := NewStateSeat(NewDenialLedger(), NewGoalRegistry(), book)
	payload, err := seat.SnapshotState(ctx)
	if err != nil {
		t.Fatalf("state the cut: %v", err)
	}
	wire := statedWire(t, payload)
	if len(wire.Approvals) != 1 || wire.Approvals[0].ID != held.ID {
		t.Fatalf("the first cut states %+v, want the one open hold: the mutation has nothing to remove", wire.Approvals)
	}
	if wire.Approvals[0].ExpiresMs <= clock.Now().UnixMilli() {
		t.Fatal("the hold is already past its validity, so dropping it proves nothing about an open hold")
	}

	keepBook := approval.NewApprovalManager(t.TempDir(), time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
	keepSeat := NewStateSeat(NewDenialLedger(), NewGoalRegistry(), keepBook)
	if err := keepSeat.RestoreState(ctx, payload); err != nil {
		t.Fatalf("restore the untampered cut: %v", err)
	}
	if _, err := keepBook.Decide("open-hold", approval.ApprovalApproved, "operator"); err != nil {
		t.Fatalf("the untampered cut must resolve the identity the approver was handed: %v", err)
	}

	wire.Approvals = nil
	wire.ApprovalKeys = nil
	tampered := restatedWire(t, wire)
	if got := statedWire(t, tampered); len(got.Approvals) != 0 {
		t.Fatalf("the tampered cut still states %+v, want the hold removed", got.Approvals)
	}
	mirror := filepath.Join(dir, "approvals", "open-hold.json")
	if _, err := os.Stat(mirror); err != nil {
		t.Fatalf("the request file %s is gone before the withdrawal, so the directory never held the hold this probe withdraws: %v", mirror, err)
	}
	withdrawApprovalFiles(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "approvals")); !os.IsNotExist(err) {
		t.Fatalf("the request directory still answers stat (%v), so the chain is not the only durable record left", err)
	}

	restartedBook := approval.NewApprovalManager(dir, time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
	restartedLedger, restartedGoals := NewDenialLedger(), NewGoalRegistry()
	restarted := NewStateSeat(restartedLedger, restartedGoals, restartedBook)
	if err := restarted.RestoreState(ctx, tampered); err != nil {
		t.Fatalf("restore the tampered cut: %v", err)
	}
	if pending := restartedBook.Pending(); len(pending) != 0 {
		t.Fatalf("the tampered cut seated %+v, want the empty book: the identity this probe resolves must come from the chain, not the cut", pending)
	}
	if err := restarted.Bind(ctx, store); err != nil {
		t.Fatalf("bind the durable face the first process wrote: %v", err)
	}
	gate := governanceGate(restartedBook, restartedLedger, restartedGoals)
	decision, err := gate.Judge(criticalCall("shell_exec", args))
	if err != nil {
		t.Fatalf("rule the critical call: %v", err)
	}
	if restartedLedger.Count() == 0 {
		t.Fatal("the gate recorded nothing, so it never ruled the call this probe measures")
	}
	if decision.Disposition == plugin.DispositionAllow || decision.Disposition == plugin.DispositionRecord {
		t.Fatalf("the gate let the call run on a dropped hold: disposition=%q reason=%q — a missing approval must park or refuse, never permit",
			decision.Disposition, decision.Reason)
	}
	if decision.Disposition != plugin.DispositionHold {
		t.Fatalf("disposition = %q, want the hold this probe measures (reason=%q)", decision.Disposition, decision.Reason)
	}
	if !strings.Contains(decision.Reason, "open-hold") {
		t.Fatalf("the gate parked under reason %q, want the original identity open-hold named: a freshly minted hold is not the dropped one surviving",
			decision.Reason)
	}
	if _, err := restartedBook.Decide("open-hold", approval.ApprovalApproved, "operator"); err != nil {
		t.Fatalf("the approval identity the first process handed the approver does not resolve after the dropped cut: %v — "+
			"the gate parked under %q, so answering the identity the approver was given is refused by name while a second hold waits for an answer nobody was asked for",
			err, decision.Reason)
	}
}

// TestOpenApprovalIdentityMustSurviveTheCut pins the identity an approver was handed.
//   - positive: the identity the first process named still resolves a ruling after the restore
//   - negative: the untampered cut resolves the same identity, so the refusal is the mutation's doing
//   - consumer: ApprovalManager.Decide as an external approver answers by that identity
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestOpenApprovalIdentityMustSurviveTheCut(t *testing.T) {
	ctx := context.Background()
	clock := newObservationClock()
	args := `{"cmd":"deploy"}`
	store := newSliceLog()
	build := func(dir string, face *sliceLog) (*approval.ApprovalManager, *StateSeat) {
		book := approval.NewApprovalManager(dir, time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
		if err := book.BindAttention(ctx, face); err != nil {
			t.Fatalf("bind the durable face: %v", err)
		}
		return book, NewStateSeat(NewDenialLedger(), NewGoalRegistry(), book)
	}
	book, seat := build(t.TempDir(), store)
	if _, err := book.Request(ctx, "identity-hold", "shell_exec", []byte(args),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	payload, err := seat.SnapshotState(ctx)
	if err != nil {
		t.Fatalf("state the cut: %v", err)
	}

	keepBook, keepSeat := build(t.TempDir(), newSliceLog())
	if err := keepSeat.RestoreState(ctx, payload); err != nil {
		t.Fatalf("restore the untampered cut: %v", err)
	}
	if _, err := keepBook.Decide("identity-hold", approval.ApprovalApproved, "operator"); err != nil {
		t.Fatalf("the untampered cut must resolve the identity the approver was handed: %v", err)
	}

	wire := statedWire(t, payload)
	wire.Approvals = nil
	dropBook, dropSeat := build(t.TempDir(), store)
	if err := dropSeat.RestoreState(ctx, restatedWire(t, wire)); err != nil {
		t.Fatalf("restore the dropped cut: %v", err)
	}
	gate := governanceGate(dropBook, NewDenialLedger(), NewGoalRegistry())
	decision, err := gate.Judge(criticalCall("shell_exec", args))
	if err != nil {
		t.Fatalf("rule the critical call: %v", err)
	}
	if decision.Disposition != plugin.DispositionHold {
		t.Fatalf("disposition = %q, want the hold this probe measures", decision.Disposition)
	}
	if _, err := dropBook.Decide("identity-hold", approval.ApprovalApproved, "operator"); err != nil {
		t.Fatalf("the approval identity the first process handed the approver no longer resolves after the restore: %v — "+
			"the gate parked the call again under a newly minted identity (%q), so answering the identity the approver was given is refused by name "+
			"while a second hold waits for an answer nobody was asked for", err, decision.Reason)
	}
}

// TestDeniedRulingMustSurviveItsValidityAcrossRestart pins the terminal refusal across a restart.
//   - positive: arguments a human refused are not freshly approvable after a restart past the request validity
//   - negative: the same instant in the same process mints no new request, so the two paths must agree
//   - consumer: the production gate rules the call, and the approver answers the request the gate names
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestDeniedRulingMustSurviveItsValidityAcrossRestart(t *testing.T) {
	ctx := context.Background()
	clock := newObservationClock()
	args := `{"cmd":"deploy"}`
	digest := approval.ArgsDigest([]byte(args))
	dir := t.TempDir()
	book := approval.NewApprovalManager(dir, time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
	ledger, goals := NewDenialLedger(), NewGoalRegistry()
	seat := NewStateSeat(ledger, goals, book)
	if _, err := book.Request(ctx, "denied-hold", "shell_exec", []byte(args),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	if _, err := book.Decide("denied-hold", approval.ApprovalDenied, "operator"); err != nil {
		t.Fatalf("rule the hold denied: %v", err)
	}
	gate := governanceGate(book, ledger, goals)
	live, err := gate.Judge(criticalCall("shell_exec", args))
	if err != nil {
		t.Fatalf("rule the refused call: %v", err)
	}
	if live.Denial == nil || live.Disposition == plugin.DispositionAllow || live.Disposition == plugin.DispositionRecord {
		t.Fatalf("the live refusal is disposition=%q denial=%v, want a named refusal: the fixture never refused the call",
			live.Disposition, live.Denial)
	}

	clock.advance(2 * time.Hour)
	sameProcess, err := gate.Judge(criticalCall("shell_exec", args))
	if err != nil {
		t.Fatalf("rule the refused call past its validity: %v", err)
	}
	livePending := len(book.Pending())
	liveLookup := book.Lookup("shell_exec", digest)
	if liveLookup == nil || liveLookup.ID != "denied-hold" {
		t.Fatalf("the live book names %+v, want the refused request it still holds", liveLookup)
	}

	payload, err := seat.SnapshotState(ctx)
	if err != nil {
		t.Fatalf("state the cut past the validity: %v", err)
	}
	if wire := statedWire(t, payload); len(wire.Approvals) != 1 || wire.Approvals[0].Status != approval.ApprovalDenied {
		t.Fatalf("the cut states %+v, want the refused request: the restart has nothing to drop", wire.Approvals)
	}
	withdrawApprovalFiles(t, dir)

	restartedBook := approval.NewApprovalManager(dir, time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
	restartedLedger, restartedGoals := NewDenialLedger(), NewGoalRegistry()
	restarted := NewStateSeat(restartedLedger, restartedGoals, restartedBook)
	if err := restarted.RestoreState(ctx, payload); err != nil {
		t.Fatalf("restore the cut: %v", err)
	}
	restartedGate := governanceGate(restartedBook, restartedLedger, restartedGoals)
	after, err := restartedGate.Judge(criticalCall("shell_exec", args))
	if err != nil {
		t.Fatalf("rule the refused call after the restart: %v", err)
	}
	restartedLookup := restartedBook.Lookup("shell_exec", digest)
	fresh := restartedBook.Pending()
	if len(fresh) > 0 {
		if _, err := restartedBook.Decide(fresh[0].ID, approval.ApprovalApproved, "operator"); err != nil {
			t.Fatalf("answer the request the restarted gate named: %v", err)
		}
		ruled, err := restartedGate.Judge(criticalCall("shell_exec", args))
		if err != nil {
			t.Fatalf("rule the call once the fresh request is approved: %v", err)
		}
		if ruled.Disposition == plugin.DispositionAllow || ruled.Disposition == plugin.DispositionRecord {
			t.Fatalf("a call a human explicitly refused became runnable after a restart: RestoreEvidence (approval_store.go:170) "+
				"drops every entry past its ExpiresMs including the refused one, the attention chain is in-memory only "+
				"(approval_store.go:124 newAttentionLedger), so the refusal has no durable record; the restarted gate minted "+
				"request %q for the same digest and approved it (disposition=%q), while the same instant in the same process "+
				"names %q and mints nothing (pending=%d, disposition=%q)",
				fresh[0].ID, ruled.Disposition, liveLookup.ID, livePending, sameProcess.Disposition)
		}
	}
	if restartedLookup == nil || restartedLookup.ID != liveLookup.ID {
		t.Fatalf("the restarted book names %+v for the refused arguments while the live book names %q: "+
			"the same instant renders two different request identities (live disposition=%q, restarted disposition=%q)",
			restartedLookup, liveLookup.ID, sameProcess.Disposition, after.Disposition)
	}
}

// TestExpiredApprovalNeverRunsAfterRestart pins the validity boundary that holds.
//   - positive: an approval past its validity never lets the call run after a restart
//   - negative: the same call inside its validity does run, so the assertion can fail
//   - consumer: the production gate rules the call over the restored book
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestExpiredApprovalNeverRunsAfterRestart(t *testing.T) {
	ctx := context.Background()
	clock := newObservationClock()
	args := `{"cmd":"deploy"}`
	dir := t.TempDir()
	book := approval.NewApprovalManager(dir, time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
	ledger, goals := NewDenialLedger(), NewGoalRegistry()
	seat := NewStateSeat(ledger, goals, book)
	if _, err := book.Request(ctx, "approved-hold", "shell_exec", []byte(args),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	if _, err := book.Decide("approved-hold", approval.ApprovalApproved, "operator"); err != nil {
		t.Fatalf("approve the hold: %v", err)
	}
	gate := governanceGate(book, ledger, goals)
	inside, err := gate.Judge(criticalCall("shell_exec", args))
	if err != nil {
		t.Fatalf("rule the approved call: %v", err)
	}
	if inside.Disposition != plugin.DispositionRecord {
		t.Fatalf("the approved call inside its validity is disposition=%q, want it to run: the fixture proves nothing about expiry",
			inside.Disposition)
	}

	clock.advance(2 * time.Hour)
	payload, err := seat.SnapshotState(ctx)
	if err != nil {
		t.Fatalf("state the cut past the validity: %v", err)
	}
	withdrawApprovalFiles(t, dir)
	restartedBook := approval.NewApprovalManager(dir, time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
	restarted := NewStateSeat(NewDenialLedger(), NewGoalRegistry(), restartedBook)
	if err := restarted.RestoreState(ctx, payload); err != nil {
		t.Fatalf("restore the expired cut: %v", err)
	}
	restartedGate := governanceGate(restartedBook, NewDenialLedger(), NewGoalRegistry())
	after, err := restartedGate.Judge(criticalCall("shell_exec", args))
	if err != nil {
		t.Fatalf("rule the expired call after the restart: %v", err)
	}
	if after.Disposition == plugin.DispositionAllow || after.Disposition == plugin.DispositionRecord {
		t.Fatalf("an expired approval was revived by the restart: disposition=%q reason=%q", after.Disposition, after.Reason)
	}
	for _, held := range restartedBook.Pending() {
		if held.ExpiresMs > 0 && held.ExpiresMs <= clock.Now().UnixMilli() {
			t.Fatalf("the restart resurrected an expired hold %+v", held)
		}
	}
}

// TestSeatedApprovalBookStillReadsTheRequestDirectory pins the authority of the stated cut over the file mirror.
//   - positive: a seated book answers from the cut it restored, so a request the cut omits stays unstated
//   - negative: the restore really dropped the hold and the file really survives, so the resurrection has a named source
//   - consumer: ApprovalManager.Lookup is the read the production gate rules a critical call through
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestSeatedApprovalBookStillReadsTheRequestDirectory(t *testing.T) {
	ctx := context.Background()
	clock := newObservationClock()
	dir := t.TempDir()
	args := `{"cmd":"deploy"}`
	book := approval.NewApprovalManager(dir, time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
	if _, err := book.Request(ctx, "dir-hold", "shell_exec", []byte(args),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	seat := NewStateSeat(NewDenialLedger(), NewGoalRegistry(), book)
	payload, err := seat.SnapshotState(ctx)
	if err != nil {
		t.Fatalf("state the cut: %v", err)
	}
	wire := statedWire(t, payload)
	wire.Approvals = nil
	tampered := restatedWire(t, wire)

	mirror := filepath.Join(dir, "approvals", "dir-hold.json")
	if _, err := os.Stat(mirror); err != nil {
		t.Fatalf("the request file %s is gone, so the directory cannot resurrect anything: %v", mirror, err)
	}
	restartedBook := approval.NewApprovalManager(dir, time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
	restarted := NewStateSeat(NewDenialLedger(), NewGoalRegistry(), restartedBook)
	if err := restarted.RestoreState(ctx, tampered); err != nil {
		t.Fatalf("restore the dropped cut: %v", err)
	}
	if pending := restartedBook.Pending(); len(pending) != 0 {
		t.Fatalf("the restore itself seated %+v, want the empty book the dropped cut states", pending)
	}
	gate := governanceGate(restartedBook, NewDenialLedger(), NewGoalRegistry())
	decision, err := gate.Judge(criticalCall("shell_exec", args))
	if err != nil {
		t.Fatalf("rule the critical call: %v", err)
	}
	if got := restartedBook.Lookup("shell_exec", approval.ArgsDigest([]byte(args))); got != nil && got.ID == "dir-hold" {
		t.Fatalf("the seated book resurrected a request the cut did not state: Lookup answers %q after the restore seated nothing — "+
			"Lookup (approval_store.go:358-360) calls rebuild on the first unapproved read even when WithApprovalSeat is set, "+
			"and rebuild (approval_store.go:570-574) reads every non-expired file under the directory, so the request file is a "+
			"live second source beside the cut and narrowing SnapshotEvidence alone does not bound the book (gate reason=%q)",
			got.ID, decision.Reason)
	}
}

// TestApprovalAliasCollapsesAcrossTheCut pins the second registration of one standing pair.
//   - positive: every identity the book answered before the cut still resolves a ruling after it
//   - negative: the cut states one entry per map key, so the alias is visible in the payload
//   - consumer: Request aliases a second caller identity, and a ruling answered under it must survive a restart
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestApprovalAliasCollapsesAcrossTheCut(t *testing.T) {
	ctx := context.Background()
	clock := newObservationClock()
	args := []byte(`{"calls":["a","b"]}`)
	book := approval.NewApprovalManager(t.TempDir(), time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
	if _, err := book.Request(ctx, "alias-a", "tagent:tool-call-batch", args,
		plugin.RiskCritical, "fuse", "the batch awaits approval"); err != nil {
		t.Fatalf("register the first identity: %v", err)
	}
	second, err := book.Request(ctx, "alias-b", "tagent:tool-call-batch", args,
		plugin.RiskCritical, "fuse", "the batch awaits approval")
	if err != nil {
		t.Fatalf("register the second identity over the same pair: %v", err)
	}
	if second.ID != "alias-a" {
		t.Fatalf("the second registration answered %q, want the standing request it aliases", second.ID)
	}
	if pending := book.Pending(); len(pending) != 1 {
		t.Fatalf("the waiting set widened to %d entries, want the one question the alias must not duplicate", len(pending))
	}
	seat := NewStateSeat(NewDenialLedger(), NewGoalRegistry(), book)
	payload, err := seat.SnapshotState(ctx)
	if err != nil {
		t.Fatalf("state the cut: %v", err)
	}
	wire := statedWire(t, payload)
	if len(wire.Approvals) != 2 {
		t.Fatalf("the cut states %d approval entries, want the two the evidence map keys hold", len(wire.Approvals))
	}
	restartedBook := approval.NewApprovalManager(t.TempDir(), time.Hour, approval.WithApprovalClock(clock.Now), approval.WithApprovalSeat())
	restarted := NewStateSeat(NewDenialLedger(), NewGoalRegistry(), restartedBook)
	if err := restarted.RestoreState(ctx, payload); err != nil {
		t.Fatalf("restore the cut: %v", err)
	}
	if _, err := restartedBook.Decide("alias-b", approval.ApprovalApproved, "operator"); err != nil {
		t.Fatalf("the aliased identity no longer resolves after the restore: %v — Request (approval_store.go:256-262) aliases a second "+
			"caller identity to the standing request, SnapshotEvidence (:146-149) states one entry per map key so the cut carries %d "+
			"copies of identity %q, and RestoreEvidence (:174) keys the book by restored.ID so only one survives; the tool fuse "+
			"(runtime/agent/fuse.go:363) registers each batch under its own decision identity, so a ruling answered under the second "+
			"identity is refused by name after a restart", err, len(wire.Approvals), second.ID)
	}
}

// TestGovernanceGoalSliceTracksOnlyActiveWork pins what one goal slice states against the settled history.
//   - positive: a cut carries the goals in force, so its bytes and the memory state follow the live work while the settled history grows
//   - negative: the same live work over five generations must not state a longer goal list, and a goal past its validity must not stay in the memory state either
//   - consumer: StateSeat.SnapshotState states the goal slice a boot restores, and the restored registry continues both numberings
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestGovernanceGoalSliceTracksOnlyActiveWork(t *testing.T) {
	ctx := context.Background()
	log := newSliceLog()
	goals := NewGoalRegistry()
	if err := goals.Bind(ctx, log); err != nil {
		t.Fatalf("bind the registry: %v", err)
	}
	seat := NewStateSeat(NewDenialLedger(), goals, nil)

	live := goals.Declare("keep the rollout watched", "entry", 0)
	const generations = 5
	var first, last []byte
	for gen := 1; gen <= generations; gen++ {
		settled := goals.Declare(fmt.Sprintf("settled work %d", gen), "entry", 0)
		if !goals.Resolve(settled, GoalAchieved) {
			t.Fatalf("generation %d: resolve the settled goal %s, which the registry must hold", gen, settled)
		}
		payload, err := seat.SnapshotState(ctx)
		if err != nil {
			t.Fatalf("state the cut of generation %d: %v", gen, err)
		}
		wire := statedWire(t, payload)
		if wantSeq := int64(2*gen + 1); wire.GoalSeq != wantSeq {
			t.Fatalf("generation %d states goal tail %d, want the %d declarations and resolutions the stream holds", gen, wire.GoalSeq, wantSeq)
		}
		if wire.GoalOrder != gen+1 {
			t.Fatalf("generation %d states goal order %d, want the %d identities the registry minted", gen, wire.GoalOrder, gen+1)
		}
		if len(wire.Goals) != 1 || wire.Goals[0].ID != live {
			t.Fatalf("generation %d states goals %+v, want the one goal in flight (%s) alone: a settled goal is the record of the stream the cut numbers", gen, wire.Goals, live)
		}
		if gen == 1 {
			first = payload
		}
		last = payload
		t.Logf("generation %d: stated goals=%d bytes=%d goal tail=%d; stored goal events=%d",
			gen, len(wire.Goals), len(payload), wire.GoalSeq, len(log.keysOf(goalEventPrefix)))
	}
	if got := len(log.keysOf(goalEventPrefix)); got != 2*generations+1 {
		t.Fatalf("stored goal events = %d, want %d: the fixture did not grow the history it measures", got, 2*generations+1)
	}
	if !goals.HasActive() || goals.Current() == nil || goals.Current().ID != live {
		t.Fatalf("live goal = %+v, want the one goal in flight as the goal context, so the constant live work guard proves something", goals.Current())
	}
	goals.mu.RLock()
	resident := len(goals.goals)
	goals.mu.RUnlock()
	if resident != 1 {
		t.Fatalf("the memory state holds %d goals beside one goal in flight, want the work in force alone: a resolved goal belongs to the stream from the moment its event landed", resident)
	}
	if len(first) == 0 {
		t.Fatal("the first generation stated nothing, so the growth bound has an empty denominator")
	}
	if len(last) > len(first)*2 {
		t.Fatalf("the cut grew with the settled goal history: %d -> %d bytes over %d generations while the live work stayed "+
			"1 goal in flight — a cut states the work in force, never the history its tail numbers; ratio %d/%d equals the generation ratio %d/1",
			len(first), len(last), generations, len(last), len(first), generations)
	}

	expired := goals.Declare("work that outlived its validity", "entry", time.Now().UnixMilli()-1000)
	if !goals.HasActive() {
		t.Fatal("the goal in flight stopped counting, so the expiry probe would measure nothing")
	}
	if wire := statedWire(t, mustStateCut(t, seat)); len(wire.Goals) != 1 || wire.Goals[0].ID != live {
		t.Fatalf("stated goals beside one expired goal = %+v, want %s alone: expired is derived from validity, so it states nothing", wire.Goals, live)
	}
	next := goals.Declare("work that starts after it", "entry", 0)
	if next != "g-8" {
		t.Fatalf("declaration after one expired goal = %q, want the identity past the seven already minted", next)
	}
	goals.mu.RLock()
	resident = len(goals.goals)
	goals.mu.RUnlock()
	if resident != 2 {
		t.Fatalf("the memory state holds %d goals after the expired one met the write boundary, want the two in flight", resident)
	}
	if read := goalOnRecord(t, goals, expired); read.Status != GoalActive || read.Statement != "work that outlived its validity" {
		t.Fatalf("the expired goal answers %+v, want its declaration read back by identity: taking a goal out of the memory state must not take it out of the history", read)
	}

	cut := mustStateCut(t, seat)
	restarted := newSliceFixture(t)
	if err := restarted.seat.RestoreState(ctx, cut); err != nil {
		t.Fatalf("restore the cut over the work in force: %v", err)
	}
	if err := restarted.seat.Bind(ctx, log); err != nil {
		t.Fatalf("bind the goal stream the restored cut numbers: %v", err)
	}
	if !restarted.goals.HasActive() || restarted.goals.Current() == nil || restarted.goals.Current().ID != next {
		t.Fatalf("restored goal context = %+v, want %s as the newest goal in flight beside the goal the first process kept", restarted.goals.Current(), next)
	}
	if got := restarted.goals.Declare("work after the restart", "entry", 0); got != "g-9" {
		t.Fatalf("post-restart declaration = %q, want the identity past the ones the cut states: a restored registry continues the declaration order it was seated with", got)
	}
	if got := len(restarted.goals.List()); got != generations+4 {
		t.Fatalf("the restarted registry answers %d goals, want the nine it declared across both processes", got)
	}
}

// TestGovernanceHistoryStaysReadableByIdentity pins the read that answers the goals a cut leaves out of its list.
//   - positive: every settled goal answers its own identity, statement and resolution after a restart, so a shorter list in the cut is not a shorter history
//   - negative: a history read that cannot reach the stream answers the work in force and invents no goal
//   - consumer: GoalRegistry.List answers the goals a process asks about, over the stream its cut numbers
//
// 契约: docs/wiki/agent/governance-enforcement.md#goal-registry
func TestGovernanceHistoryStaysReadableByIdentity(t *testing.T) {
	ctx := context.Background()

	t.Run("every settled goal answers its own identity and statement", func(t *testing.T) {
		log := newSliceLog()
		goals := NewGoalRegistry()
		if err := goals.Bind(ctx, log); err != nil {
			t.Fatalf("bind the registry: %v", err)
		}
		seat := NewStateSeat(NewDenialLedger(), goals, nil)

		type declared struct {
			id        string
			statement string
			createdBy string
			status    GoalStatus
		}
		want := make([]declared, 0, 4)
		for i, word := range []GoalStatus{GoalAchieved, GoalAbandoned, GoalAchieved, GoalActive} {
			statement, createdBy := fmt.Sprintf("goal number %d", i+1), fmt.Sprintf("entry-%d", i+1)
			id := goals.Declare(statement, createdBy, 0)
			if word != GoalActive && !goals.Resolve(id, word) {
				t.Fatalf("resolve goal %s to %s, which the registry must hold", id, word)
			}
			want = append(want, declared{id: id, statement: statement, createdBy: createdBy, status: word})
		}
		cut := mustStateCut(t, seat)
		if wire := statedWire(t, cut); len(wire.Goals) != 1 {
			t.Fatalf("the cut states %+v, want the one goal still in flight", wire.Goals)
		}

		restarted := newSliceFixture(t)
		if err := restarted.seat.RestoreState(ctx, cut); err != nil {
			t.Fatalf("restore the cut: %v", err)
		}
		if err := restarted.seat.Bind(ctx, log); err != nil {
			t.Fatalf("bind the goal stream the restored cut numbers: %v", err)
		}
		read := map[string]*Goal{}
		for _, goal := range restarted.goals.List() {
			read[goal.ID] = goal
		}
		if len(read) != len(want) {
			t.Fatalf("the restarted registry answers %d goals, want the %d the first process declared: a cut that states the work in force must not shorten the history a reader asks about", len(read), len(want))
		}
		for _, w := range want {
			got, ok := read[w.id]
			if !ok {
				t.Fatalf("goal %s is answered by no list after the restart, want it read back from the stream its cut numbers", w.id)
			}
			if got.Statement != w.statement || got.CreatedBy != w.createdBy || got.Status != w.status {
				t.Fatalf("goal %s answers (%q by %q, %s), want (%q by %q, %s): the history is read per identity, with the statement and the resolution its declaration froze",
					w.id, got.Statement, got.CreatedBy, got.Status, w.statement, w.createdBy, w.status)
			}
			if got.CreatedMs == 0 {
				t.Fatalf("goal %s answers no declaration time, want the moment its event was stamped", w.id)
			}
		}
	})

	t.Run("a stream it cannot read answers the work in force and invents no goal", func(t *testing.T) {
		log := newSliceLog()
		donor := NewGoalRegistry()
		if err := donor.Bind(ctx, log); err != nil {
			t.Fatalf("bind the registry: %v", err)
		}
		settled := donor.Declare("work that closes", "entry", 0)
		if !donor.Resolve(settled, GoalAchieved) {
			t.Fatalf("resolve the settled goal %s", settled)
		}
		live := donor.Declare("work still in force", "entry", 0)
		cut := mustStateCut(t, NewStateSeat(NewDenialLedger(), donor, nil))

		restarted := NewGoalRegistry()
		seat := NewStateSeat(NewDenialLedger(), restarted, nil)
		if err := seat.RestoreState(ctx, cut); err != nil {
			t.Fatalf("restore the cut: %v", err)
		}
		if err := seat.Bind(ctx, &failingLog{sliceLog: sliceLog{stored: map[string][]byte{}}, err: errors.New("the store answered no read")}); err == nil {
			t.Fatal("a store that cannot be read must report the unread tail to the binder")
		}
		if got := restarted.List(); len(got) != 1 || got[0].ID != live {
			t.Fatalf("the history read that could not reach the stream answers %+v, want the one goal in force and no goal it did not read", got)
		}
	})
}

// TestGovernanceCountAndQueryStateTheirBoundary pins what the history queries answer beside a cut that states tails alone.
//   - positive: the queries answer the history behind the stated tails, so a restored cut never answers an empty history
//   - negative: no query answers its stream's tail as a count, and a stream that numbers resolutions beside declarations answers the goals, not the events
//   - consumer: DenialLedger.Query/Count and GoalRegistry.List are the faces a reader of the recovered process asks
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestGovernanceCountAndQueryStateTheirBoundary(t *testing.T) {
	ctx := context.Background()
	log := newSliceLog()
	ledger, goals := NewDenialLedger(), NewGoalRegistry()
	if err := ledger.Bind(ctx, log); err != nil {
		t.Fatalf("bind the ledger: %v", err)
	}
	if err := goals.Bind(ctx, log); err != nil {
		t.Fatalf("bind the registry: %v", err)
	}
	seat := NewStateSeat(ledger, goals, nil)

	for _, tool := range []string{"first", "second", "third"} {
		ledger.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: tool, Level: plugin.RiskHigh, RuleID: "no-" + tool})
	}
	achieved := goals.Declare("work that closes", "entry", 0)
	abandoned := goals.Declare("work that stops", "entry", 0)
	if !goals.Resolve(achieved, GoalAchieved) || !goals.Resolve(abandoned, GoalAbandoned) {
		t.Fatalf("resolve the two goals, which the registry must hold")
	}

	cut := mustStateCut(t, seat)
	wire := statedWire(t, cut)
	if wire.DenialSeq != 3 || wire.GoalSeq != 4 {
		t.Fatalf("the cut states denial tail %d and goal tail %d, want the three records and the four goal operations the streams hold", wire.DenialSeq, wire.GoalSeq)
	}
	if len(wire.Goals) != 0 {
		t.Fatalf("the cut states %+v, want no goal: both are settled, so the tails are their record", wire.Goals)
	}

	restarted := newSliceFixture(t)
	if err := restarted.seat.RestoreState(ctx, cut); err != nil {
		t.Fatalf("restore the cut: %v", err)
	}
	if err := restarted.seat.Bind(ctx, log); err != nil {
		t.Fatalf("bind the streams the restored cut numbers: %v", err)
	}
	if got := restarted.ledger.Count(); got != 3 {
		t.Fatalf("Count() beside a cut that numbers three records = %d, want the three the stream holds: a query that answers an empty history is a record that went missing in silence", got)
	}
	records := restarted.ledger.Query(0)
	if len(records) != 3 || records[0].ToolName != "third" || records[2].ToolName != "first" {
		t.Fatalf("Query(0) beside a cut that numbers three records = %+v, want the three in the order they happened, newest first", records)
	}
	read := restarted.goals.List()
	if len(read) != 2 || read[0].ID != achieved || read[1].ID != abandoned {
		t.Fatalf("List() beside a cut that numbers two goals = %+v, want the two in declaration order: the goal stream numbers a resolution beside every declaration, so its tail %d is not the goal count and no query answers it as one", read, wire.GoalSeq)
	}
	if len(read) == int(wire.GoalSeq) {
		t.Fatalf("the goal history answers %d goals beside a stated tail of %d, want the goals counted and the tail left to the stream: a count read off the tail over-reports every resolution it numbers", len(read), wire.GoalSeq)
	}
}
