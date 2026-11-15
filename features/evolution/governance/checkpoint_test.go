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
		if wire.DenialSeq != 2 || len(wire.Records) != 2 {
			t.Fatalf("stated denial stream = tail %d over %d records, want the two records the ledger folded", wire.DenialSeq, len(wire.Records))
		}
		if wire.GoalSeq != 2 || wire.GoalOrder != 1 || len(wire.Goals) != 1 {
			t.Fatalf("stated goal stream = tail %d order %d over %d goals, want the declaration and its resolution", wire.GoalSeq, wire.GoalOrder, len(wire.Goals))
		}
		if wire.Goals[0].Status != GoalAchieved {
			t.Fatalf("stated goal status = %q, want the resolution the registry applied", wire.Goals[0].Status)
		}
		if len(wire.Approvals) != 1 || wire.Approvals[0].ID != "approval-cut-1" {
			t.Fatalf("stated approvals = %+v, want the request the book holds", wire.Approvals)
		}

		restarted := newSliceFixture(t)
		if err := restarted.seat.RestoreState(ctx, payload); err != nil {
			t.Fatalf("restore the stated governance cut: %v", err)
		}
		records := restarted.ledger.Query(0)
		if len(records) != 2 || records[0].ToolName != "read_file" || records[1].ToolName != "shell_exec" {
			t.Fatalf("restored records = %+v, want the two the cut folded in the order they happened", records)
		}
		if goals := restarted.goals.List(); len(goals) != 1 || goals[0].ID != declared || goals[0].Status != GoalAchieved {
			t.Fatalf("restored goals = %+v, want %s resolved", goals, declared)
		}
		if active := restarted.goals.HasActive(); active {
			t.Fatal("restored registry reports an active goal, want the resolution the cut stated")
		}
		if got := restarted.ledger.Count(); got != 2 {
			t.Fatalf("restored record count = %d, want the index the cut stated and no tail past it", got)
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
