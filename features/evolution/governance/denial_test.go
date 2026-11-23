// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/plugin"
)

// refusingLog is one governance store that refuses the writes and reads a probe names.
type refusingLog struct {
	sliceLog
	refusePut  map[string]bool
	refuseRead bool
	putErr     error
	readErr    error
	refused    int
}

// Put refuses the named keys and stores the rest.
func (l *refusingLog) Put(ctx context.Context, key string, value []byte) error {
	l.mu.Lock()
	refuse := l.refusePut[key]
	if refuse {
		l.refused++
	}
	l.mu.Unlock()
	if refuse {
		return l.putErr
	}
	return l.sliceLog.Put(ctx, key, value)
}

// Get answers the read refusal or the stored pair.
func (l *refusingLog) Get(ctx context.Context, key string) ([]byte, bool, error) {
	l.mu.Lock()
	refuse := l.refuseRead
	l.mu.Unlock()
	if refuse {
		return nil, false, l.readErr
	}
	return l.sliceLog.Get(ctx, key)
}

// Scan answers the read refusal or the stored pairs.
func (l *refusingLog) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	l.mu.Lock()
	refuse := l.refuseRead
	l.mu.Unlock()
	if refuse {
		return nil, l.readErr
	}
	return l.sliceLog.Scan(ctx, prefix)
}

// TestRefusedDurableWriteIsNamedNeitherToCallerNorLog pins the failure semantics of one governance double.
//   - positive: a durable write the store refused is named to the caller or to the log, or the index does not count it
//   - negative: the same store's read refusal reaches the boot, so the two legs must not differ in silence
//   - consumer: DenialLedger.Record is the live leg and StateSeat.Bind the recovery leg of one governance double
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
func TestRefusedDurableWriteIsNamedNeitherToCallerNorLog(t *testing.T) {
	ctx := context.Background()
	observer := &approvalObserver{}
	watchApprovalLog(t, observer)
	log := &refusingLog{
		sliceLog:  sliceLog{stored: map[string][]byte{}},
		refusePut: map[string]bool{denialEventPrefix + "0000000000000000001": true},
		putErr:    errors.New("the store refused the governance write"),
		readErr:   errors.New("the store answered no read"),
	}
	ledger := NewDenialLedger()
	if err := ledger.Bind(ctx, log); err != nil {
		t.Fatalf("bind the ledger: %v", err)
	}
	ledger.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: "shell_exec", Level: plugin.RiskHigh, Reason: "refused"})

	if log.refused != 1 {
		t.Fatalf("refused writes = %d, want exactly the one this probe injected", log.refused)
	}
	if got := len(log.keysOf(denialEventPrefix)); got != 0 {
		t.Fatalf("stored denial events = %d, want none: the write was not actually refused", got)
	}
	indexed := ledger.Count()
	lines := observer.all()
	named := false
	for _, line := range lines {
		if strings.Contains(line, "refused the governance write") || strings.Contains(line, "shell_exec") {
			named = true
		}
	}

	recovery := NewStateSeat(NewDenialLedger(), nil, nil)
	if err := recovery.RestoreState(ctx, []byte(fmt.Sprintf(`{"version":%d,"denial_seq":1}`, stateSliceVersion))); err != nil {
		t.Fatalf("restore the stated cut: %v", err)
	}
	log.mu.Lock()
	log.refuseRead = true
	log.mu.Unlock()
	readErr := recovery.Bind(ctx, log)

	if indexed != 0 && !named {
		t.Fatalf("the live write leg is silent while the recovery read leg is fatal: putGovernanceEvent (denial.go:317-326) "+
			"returns void on a Put error and discards the Sync error, so Record counted %d record(s) the store does not hold "+
			"and wrote %d log line(s) naming it, while the same store's read refusal answers the boot with %v — "+
			"one governance double, two failure semantics", indexed, len(lines), readErr)
	}
	if readErr == nil {
		t.Fatal("the recovery read refusal answered nil, want the error the boot stops on")
	}
}

// TestDenialTailSeqIsNotTheRecordCount pins the two answers a cut reports about one denial stream.
//   - positive: the folded count and the tail name one set of events, so a stream the fold read whole answers the same number both ways
//   - negative: a body the fold cannot parse is reported by key instead of skipped, so no key numbers an event the index never folded
//   - consumer: DenialLedger.Count answers the index and the seated DenialSeq the tail, and a cut that states no history must not answer its sequence as the count
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
func TestDenialTailSeqIsNotTheRecordCount(t *testing.T) {
	ctx := context.Background()

	t.Run("a stream the fold read whole answers one number both ways", func(t *testing.T) {
		log := newSliceLog()
		ledger := NewDenialLedger()
		if err := ledger.Bind(ctx, log); err != nil {
			t.Fatalf("bind the ledger: %v", err)
		}
		for _, tool := range []string{"first", "second", "third"} {
			ledger.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: tool, Level: plugin.RiskHigh})
		}
		if got := len(log.keysOf(denialEventPrefix)); got != 3 {
			t.Fatalf("stored denial events = %d, want the three the fixture recorded", got)
		}

		restarted := NewDenialLedger()
		seat := NewStateSeat(restarted, nil, nil)
		if err := seat.RestoreState(ctx, nil); err != nil {
			t.Fatalf("restore the zero cut: %v", err)
		}
		if err := seat.Bind(ctx, log); err != nil {
			t.Fatalf("bind the durable face: %v", err)
		}
		count, tail := restarted.Count(), restarted.evtSeq
		if count == 0 {
			t.Fatal("the fold recovered no record, so the comparison has an empty denominator")
		}
		if count != int(tail) {
			t.Fatalf("a stream the fold read whole answers Count()=%d over DenialSeq=%d, want one number: "+
				"every key the tail numbers must be an event the index folded", count, tail)
		}
	})

	t.Run("a body the fold cannot parse is named and no key numbers it", func(t *testing.T) {
		log := newSliceLog()
		ledger := NewDenialLedger()
		if err := ledger.Bind(ctx, log); err != nil {
			t.Fatalf("bind the ledger: %v", err)
		}
		ledger.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: "first", Level: plugin.RiskHigh})
		first := denialEventPrefix + "0000000000000000001"
		malformed := denialEventPrefix + "0000000000000000002"
		log.mu.Lock()
		log.stored[malformed] = []byte("{not json")
		log.stored[denialEventPrefix+"0000000000000000003"] = append([]byte(nil), log.stored[first]...)
		log.mu.Unlock()

		restarted := NewDenialLedger()
		seat := NewStateSeat(restarted, nil, nil)
		if err := seat.RestoreState(ctx, nil); err != nil {
			t.Fatalf("restore the zero cut: %v", err)
		}
		bindErr := seat.Bind(ctx, log)
		if got := len(log.keysOf(denialEventPrefix)); got != 3 {
			t.Fatalf("stored denial events = %d, want the three keys the fixture wrote", got)
		}
		if bindErr == nil || !strings.Contains(bindErr.Error(), malformed) {
			t.Fatalf("bind of a stream with an unreadable body = %v, want the key %s named: "+
				"a skipped body leaves Count()=%d beside DenialSeq=%d, so the count answers an event the index lacks",
				bindErr, malformed, restarted.Count(), restarted.evtSeq)
		}
		count, tail := restarted.Count(), restarted.evtSeq
		if count != int(tail) {
			t.Fatalf("a refused fold answered Count()=%d beside DenialSeq=%d, want both at the stated cut", count, tail)
		}
	})

	t.Run("the count answers the index, never the tail a cut names", func(t *testing.T) {
		restarted := NewDenialLedger()
		seat := NewStateSeat(restarted, nil, nil)
		cut := fmt.Sprintf(`{"version":%d,"denial_seq":20}`, stateSliceVersion)
		if err := seat.RestoreState(ctx, []byte(cut)); err != nil {
			t.Fatalf("restore the stated cut: %v", err)
		}
		tail := restarted.evtSeq
		if tail != 20 {
			t.Fatalf("the seated tail = %d, want the twenty the cut names", tail)
		}
		if count := restarted.Count(); count != 0 {
			t.Fatalf("Count() = %d beside a cut that states a tail of %d and no record, "+
				"want the index it holds: redefining the count as the tail sequence over-reports the events a window left behind",
				count, tail)
		}
	})
}

// recordedDenialStore records four denial events into one fresh store.
func recordedDenialStore(t *testing.T) *sliceLog {
	t.Helper()
	log := newSliceLog()
	donor := NewDenialLedger()
	if err := donor.Bind(context.Background(), log); err != nil {
		t.Fatalf("bind the donor ledger: %v", err)
	}
	for _, tool := range []string{"first", "second", "third", "fourth"} {
		donor.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: tool, Level: plugin.RiskHigh})
	}
	if got := len(log.keysOf(denialEventPrefix)); got != 4 {
		t.Fatalf("stored denial events = %d, want the four the fixture recorded", got)
	}
	return log
}

// holedDenialStore withdraws the second key of a four-event stream: the write leg
// hands a refused number back, so a hole is constructed as the stored shape a
// stream kept before that discipline, which is the shape the read must name.
func holedDenialStore(t *testing.T) *sliceLog {
	t.Helper()
	log := recordedDenialStore(t)
	log.mu.Lock()
	delete(log.stored, denialEventPrefix+"0000000000000000002")
	log.mu.Unlock()
	if got := log.keysOf(denialEventPrefix); len(got) != 3 {
		t.Fatalf("stored denial keys = %v, want the three the hole leaves", got)
	}
	return log
}

// TestGovernanceHistoryQueryNamesItsBoundary pins the named window of a seated history read.
//   - positive: a stream with a hole answers its index beside an ERROR naming every key the window leaves out
//   - negative: the same four events read whole answer Count()=4 with no gap named, so the naming is the hole's doing
//   - consumer: DenialLedger.Query and Count fold the history a seated cut numbers through foldHistory
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
func TestGovernanceHistoryQueryNamesItsBoundary(t *testing.T) {
	ctx := context.Background()

	t.Run("a window that ends below stored events names each one it leaves out", func(t *testing.T) {
		observer := &approvalObserver{}
		watchApprovalLog(t, observer)
		log := holedDenialStore(t)
		restarted := NewDenialLedger()
		seat := NewStateSeat(restarted, nil, nil)
		if err := seat.RestoreState(ctx, []byte(fmt.Sprintf(`{"version":%d,"denial_seq":1}`, stateSliceVersion))); err != nil {
			t.Fatalf("seat the cut below the hole: %v", err)
		}
		if err := seat.Bind(ctx, log); err != nil {
			t.Fatalf("bind the durable face: %v", err)
		}
		if tail := restarted.evtSeq; tail != 1 {
			t.Fatalf("the seated tail = %d, want the one the tail read stopped at", tail)
		}
		records := restarted.Query(0)
		if len(records) != 1 || records[0].ToolName != "first" {
			t.Fatalf("Query(0) = %+v, want the one record the window covers", records)
		}
		if count := restarted.Count(); count != 1 {
			t.Fatalf("Count() = %d, want the one record the window covers", count)
		}
		for _, seq := range []string{"0000000000000000003", "0000000000000000004"} {
			key := denialEventPrefix + seq
			if !observer.mentions("error", key) {
				t.Fatalf("the history answer left %s unnamed: the store holds it and the window does not answer it, "+
					"so a reader cannot tell a shorter history from a stated boundary (lines=%v)", key, observer.all())
			}
		}
	})

	t.Run("a hole behind the seated window is named by its sequence", func(t *testing.T) {
		observer := &approvalObserver{}
		watchApprovalLog(t, observer)
		log := holedDenialStore(t)
		restarted := NewDenialLedger()
		seat := NewStateSeat(restarted, nil, nil)
		if err := seat.RestoreState(ctx, []byte(fmt.Sprintf(`{"version":%d,"denial_seq":4}`, stateSliceVersion))); err != nil {
			t.Fatalf("seat the cut past the hole: %v", err)
		}
		if err := seat.Bind(ctx, log); err != nil {
			t.Fatalf("bind the durable face: %v", err)
		}
		if count := restarted.Count(); count != 3 {
			t.Fatalf("Count() = %d, want the three stored events the window covers", count)
		}
		if tail := restarted.evtSeq; tail != 4 {
			t.Fatalf("the seated tail = %d, want the four the cut names", tail)
		}
		missing := denialEventPrefix + "0000000000000000002"
		if !observer.mentions("error", missing) {
			t.Fatalf("the window numbers %s and the store does not hold it, yet the history answer named no gap "+
				"(Count()=3 beside DenialSeq=4, lines=%v)", missing, observer.all())
		}
	})

	t.Run("a stream read whole names no gap", func(t *testing.T) {
		observer := &approvalObserver{}
		watchApprovalLog(t, observer)
		log := recordedDenialStore(t)
		restarted := NewDenialLedger()
		seat := NewStateSeat(restarted, nil, nil)
		if err := seat.RestoreState(ctx, []byte(fmt.Sprintf(`{"version":%d,"denial_seq":4}`, stateSliceVersion))); err != nil {
			t.Fatalf("seat the cut at the stream end: %v", err)
		}
		if err := seat.Bind(ctx, log); err != nil {
			t.Fatalf("bind the durable face: %v", err)
		}
		if count := restarted.Count(); count != 4 {
			t.Fatalf("Count() = %d, want the four events the whole stream holds", count)
		}
		for _, line := range observer.all() {
			if strings.Contains(line, "governance history unreadable") {
				t.Fatalf("a whole stream named a gap: %s", line)
			}
		}
	})
}

// uncertainPutLog is one governance store that lands a named key's bytes and then
// refuses to confirm the write: the landing is unknown to the writer, the shape a
// store leaves when a commit reports failure after the bytes reached it.
type uncertainPutLog struct {
	sliceLog
	uncertain map[string]bool
	puts      int
}

// Put lands the named key once and reports the unconfirmed write, counting every
// put so a check reads how many writes reached the store.
func (l *uncertainPutLog) Put(ctx context.Context, key string, value []byte) error {
	l.mu.Lock()
	unc := l.uncertain[key]
	delete(l.uncertain, key)
	l.puts++
	l.mu.Unlock()
	if err := l.sliceLog.Put(ctx, key, value); err != nil {
		return err
	}
	if unc {
		return errors.New("the store landed the write but did not confirm it")
	}
	return nil
}

// eventBytes renders one record as the stored body of the event at one key.
func eventBytes(t *testing.T, key string, rec DenialRecord) []byte {
	t.Helper()
	raw, err := json.Marshal(governanceEventOf(key, rec))
	if err != nil {
		t.Fatalf("render the event at %s: %v", key, err)
	}
	return raw
}

// holedTailStore writes events 1/2, refuses the write at 3 exactly once, and lands
// events 4/5 past the hole: the stored shape a stream keeps when a refused write
// left an empty number and later facts committed behind it.
func holedTailStore(t *testing.T) *refusingLog {
	t.Helper()
	ctx := context.Background()
	key3 := denialEventPrefix + "0000000000000000003"
	log := &refusingLog{
		sliceLog:  sliceLog{stored: map[string][]byte{}},
		refusePut: map[string]bool{key3: true},
		putErr:    errors.New("the store refused the third governance write"),
	}
	donor := NewDenialLedger()
	if err := donor.Bind(ctx, log); err != nil {
		t.Fatalf("bind the donor ledger: %v", err)
	}
	recs := []DenialRecord{
		{Subtype: SubtypeDenial, ToolName: "first", Level: plugin.RiskHigh},
		{Subtype: SubtypeDenial, ToolName: "second", Level: plugin.RiskHigh},
		{Subtype: SubtypeDenial, ToolName: "third", Level: plugin.RiskHigh},
		{Subtype: SubtypeDenial, ToolName: "fourth", Level: plugin.RiskHigh},
		{Subtype: SubtypeDenial, ToolName: "fifth", Level: plugin.RiskHigh},
	}
	donor.Record(recs[0])
	donor.Record(recs[1])
	donor.Record(recs[2])
	if log.refused != 1 {
		t.Fatalf("refused writes = %d, want exactly the one this fixture injected at key 3", log.refused)
	}
	log.mu.Lock()
	log.refusePut = map[string]bool{}
	log.stored[denialEventPrefix+"0000000000000000004"] = eventBytes(t, denialEventPrefix+"0000000000000000004", recs[3])
	log.stored[denialEventPrefix+"0000000000000000005"] = eventBytes(t, denialEventPrefix+"0000000000000000005", recs[4])
	log.mu.Unlock()
	want := []string{
		denialEventPrefix + "0000000000000000001",
		denialEventPrefix + "0000000000000000002",
		denialEventPrefix + "0000000000000000004",
		denialEventPrefix + "0000000000000000005",
	}
	if got := log.keysOf(denialEventPrefix); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("stored denial keys = %v, want the four the hole leaves (1/2/4/5): the fixture must hold the facts the continuation could re-key over", got)
	}
	if _, held := log.stored[key3]; held {
		t.Fatalf("key 3 is held, want the empty number the refused write left: the fixture must carry a real hole")
	}
	return log
}

// TestGovernanceTailHoleContinuesPastTheHighestCommitted pins the continuation a restart writes past a hole.
//   - positive: a cut below a hole continues from the highest committed sequence, names the hole, and leaves every stored body byte-identical
//   - negative: the continuation never re-keys over the events stored behind the hole and never fills the empty number with a later fact
//   - consumer: DenialLedger.Record resolves the continuation tail on the first write past a seated cut (resolveContinuationTail)
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
func TestGovernanceTailHoleContinuesPastTheHighestCommitted(t *testing.T) {
	ctx := context.Background()
	observer := &approvalObserver{}
	watchApprovalLog(t, observer)
	log := holedTailStore(t)
	key4 := denialEventPrefix + "0000000000000000004"
	key5 := denialEventPrefix + "0000000000000000005"
	body4 := append([]byte(nil), log.stored[key4]...)
	body5 := append([]byte(nil), log.stored[key5]...)

	restarted := NewDenialLedger()
	seat := NewStateSeat(restarted, nil, nil)
	if err := seat.RestoreState(ctx, []byte(fmt.Sprintf(`{"version":%d,"denial_seq":2}`, stateSliceVersion))); err != nil {
		t.Fatalf("seat the cut below the hole: %v", err)
	}
	if err := seat.Bind(ctx, log); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	if tail := restarted.evtSeq; tail != 2 {
		t.Fatalf("the tail read stopped at %d, want the cut below the hole: the cheap boot read ends at the first miss", tail)
	}

	restarted.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: "after-restart-a", Level: plugin.RiskHigh})
	restarted.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: "after-restart-b", Level: plugin.RiskHigh})

	if !bytes.Equal(log.stored[key4], body4) {
		t.Fatalf("key 4 body was rewritten: a restart re-keyed over a stored governance event, the P1 red line (before=%s after=%s)", body4, log.stored[key4])
	}
	if !bytes.Equal(log.stored[key5], body5) {
		t.Fatalf("key 5 body was rewritten: a restart re-keyed over a stored governance event (before=%s after=%s)", body5, log.stored[key5])
	}
	if _, held := log.stored[denialEventPrefix+"0000000000000000003"]; held {
		t.Fatal("the continuation filled the empty number 3 with a later fact: the hole is named, not closed by inserting a fact out of order")
	}
	want := []string{
		denialEventPrefix + "0000000000000000001",
		denialEventPrefix + "0000000000000000002",
		denialEventPrefix + "0000000000000000004",
		denialEventPrefix + "0000000000000000005",
		denialEventPrefix + "0000000000000000006",
		denialEventPrefix + "0000000000000000007",
	}
	if got := log.keysOf(denialEventPrefix); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("stored denial keys after continuation = %v, want 1/2/4/5/6/7: the continuation starts past the highest committed sequence (5), not at the hole (3)", got)
	}
	if !observer.mentions("error", denialEventPrefix+"0000000000000000003") {
		t.Fatalf("the continuation named no hole at key 3 (lines=%v): a hole is a stated boundary, never a silent chain end", observer.all())
	}
}

// tailReadFailureLog refuses the point read of one governance key and answers the
// other reads from the store beside it.
type tailReadFailureLog struct {
	*sliceLog
	key string
	err error
}

// Get answers the stored pair, or the named failure of the one key a probe
// refuses, and records the read either way.
func (l *tailReadFailureLog) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if key == l.key {
		l.mu.Lock()
		l.scans = append(l.scans, key)
		l.mu.Unlock()
		return nil, false, l.err
	}
	return l.sliceLog.Get(ctx, key)
}

// TestGovernanceTailReadFailureIsNamed pins the read a recovery pays for the governance tail past a cut.
//   - positive: the tail is the one key each next sequence names, read as a point read of that key
//   - negative: a point read the store refused fails the recovery by name instead of ending the tail like a hole
//   - consumer: StateSeat.Bind folds the denial stream past the cut a restored slice states
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
func TestGovernanceTailReadFailureIsNamed(t *testing.T) {
	ctx := context.Background()
	key := func(seq int) string { return fmt.Sprintf("%s%019d", denialEventPrefix, seq) }
	records := []DenialRecord{
		{Subtype: SubtypeDenial, ToolName: "tail-a", Level: plugin.RiskHigh},
		{Subtype: SubtypeDenial, ToolName: "tail-b", Level: plugin.RiskHigh},
		{Subtype: SubtypeDenial, ToolName: "tail-c", Level: plugin.RiskHigh},
	}
	store := newSliceLog()
	for i, rec := range records {
		store.stored[key(i+1)] = eventBytes(t, key(i+1), rec)
	}
	readErr := errors.New("the store answered no read of the governance tail")
	log := &tailReadFailureLog{sliceLog: store, key: key(2), err: readErr}

	ledger := NewDenialLedger()
	seat := NewStateSeat(ledger, nil, nil)
	if err := seat.RestoreState(ctx, []byte(fmt.Sprintf(`{"version":%d,"denial_seq":1}`, stateSliceVersion))); err != nil {
		t.Fatalf("seat the cut below the tail: %v", err)
	}
	mark := log.mark()
	err := seat.Bind(ctx, log)
	if !errors.Is(err, readErr) {
		t.Fatalf("the recovery beside a refused tail read = %v, want %v: a read folded into the end of the tail turns a stored event into a hole", err, readErr)
	}
	if !strings.Contains(err.Error(), key(2)) {
		t.Fatalf("the refused tail read = %v, want the key %s it could not read named beside the failure", err, key(2))
	}
	if reads := log.scansSince(mark); strings.Join(reads, " ") != key(2) {
		t.Fatalf("the tail read paid for %v, want the one key %s the first sequence past the cut names: a known sequence is a point read, never a scan of the stream behind it", reads, key(2))
	}
	if tail := ledger.evtSeq; tail != 1 {
		t.Fatalf("the tail read stopped at %d, want the cut it was seated at: the refused read served no event and moved no number", tail)
	}
	if got := ledger.Count(); got != 1 {
		t.Fatalf("the index holds %d record(s), want the one the cut states: the refused tail read folded nothing", got)
	}
}

// TestGovernanceEventKeyRefusesOtherContent pins the same-key conflict refusal of a governance write.
//   - positive: a write whose key already names other bytes is refused by name and leaves the stored body byte-identical
//   - negative: the refused record is not indexed beside the fact the identity already names, so the index never diverges from the chain
//   - consumer: DenialLedger.Record meets the store under its key before it lands (errGovernanceEventConflict)
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
func TestGovernanceEventKeyRefusesOtherContent(t *testing.T) {
	ctx := context.Background()
	observer := &approvalObserver{}
	watchApprovalLog(t, observer)
	key1 := denialEventPrefix + "0000000000000000001"
	log := &uncertainPutLog{sliceLog: sliceLog{stored: map[string][]byte{}}, uncertain: map[string]bool{key1: true}}
	ledger := NewDenialLedger()
	if err := ledger.Bind(ctx, log); err != nil {
		t.Fatalf("bind the ledger: %v", err)
	}
	recA := DenialRecord{Subtype: SubtypeDenial, ToolName: "alpha", Level: plugin.RiskHigh, Reason: "first ruling", Timestamp: 1700000000000}
	ledger.Record(recA)
	bodyA := append([]byte(nil), log.stored[key1]...)
	if len(bodyA) == 0 {
		t.Fatal("the uncertain write landed no bytes, so the conflict has no stored body to protect")
	}

	recB := DenialRecord{Subtype: SubtypeDenial, ToolName: "beta", Level: plugin.RiskHigh, Reason: "other ruling"}
	ledger.Record(recB)

	if !bytes.Equal(log.stored[key1], bodyA) {
		t.Fatalf("key 1 body was overwritten by another content: the same-key conflict must refuse by name, not rewrite (before=%s after=%s)", bodyA, log.stored[key1])
	}
	if !observer.mentions("error", "already names other content", key1) {
		t.Fatalf("the conflicting write was not refused by name at key 1 (lines=%v): an overwrite must be a named refusal", observer.all())
	}
	if count := ledger.Count(); count != 1 {
		t.Fatalf("Count() = %d, want the one fact the identity names: a refused conflict indexes nothing beside the stored fact", count)
	}
}

// TestGovernanceEventReplayIndexesNothingTwice pins the identity dedup of a governance write.
//   - positive: a replay of the event already held under its key lands no second write and indexes no second record
//   - negative: the numbering tail still advances past the replayed commitment, so the next fresh fact takes the next number
//   - consumer: DenialLedger.Record reads the held bytes under its key and answers a replay as the commitment it is
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
func TestGovernanceEventReplayIndexesNothingTwice(t *testing.T) {
	ctx := context.Background()
	key1 := denialEventPrefix + "0000000000000000001"
	log := &uncertainPutLog{sliceLog: sliceLog{stored: map[string][]byte{}}, uncertain: map[string]bool{key1: true}}
	ledger := NewDenialLedger()
	if err := ledger.Bind(ctx, log); err != nil {
		t.Fatalf("bind the ledger: %v", err)
	}
	recA := DenialRecord{Subtype: SubtypeDenial, ToolName: "alpha", Level: plugin.RiskHigh, Reason: "first ruling", Timestamp: 1700000000000}
	ledger.Record(recA)
	putsAfterFirst := log.puts
	bodyA := append([]byte(nil), log.stored[key1]...)

	ledger.Record(recA)

	if log.puts != putsAfterFirst {
		t.Fatalf("a replay of the held event wrote again (puts %d -> %d): the same bytes under one key commit nothing twice", putsAfterFirst, log.puts)
	}
	if !bytes.Equal(log.stored[key1], bodyA) {
		t.Fatalf("a replay rewrote the stored body (before=%s after=%s)", bodyA, log.stored[key1])
	}
	if count := ledger.Count(); count != 1 {
		t.Fatalf("Count() = %d after a replay, want one record: a replay of one identity indexes nothing twice", count)
	}
	ledger.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: "beta", Level: plugin.RiskHigh})
	if _, held := log.stored[denialEventPrefix+"0000000000000000002"]; !held {
		t.Fatal("the fresh fact after a replay did not take the next number: the tail must advance past the replayed commitment")
	}
}
