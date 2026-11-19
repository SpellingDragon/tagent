// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// factStore is one attention store in memory: the pairs written beside the reads
// it answered and the Put or the read it refuses on demand.
type factStore struct {
	mu        sync.Mutex
	stored    map[string][]byte
	scans     int
	pairs     int
	refuse    int
	refuseKey string
}

// newFactStore returns an empty attention store.
func newFactStore() *factStore {
	return &factStore{stored: map[string][]byte{}}
}

// Put stores one fact, or refuses the write while its counter still holds one.
func (s *factStore) Put(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refuse > 0 {
		s.refuse--
		return fmt.Errorf("the store refused the attention write")
	}
	s.stored[key] = append([]byte(nil), value...)
	return nil
}

// Scan answers every stored pair under the prefix, and refuses the read of one key
// on demand.
func (s *factStore) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refuseKey != "" && prefix == s.refuseKey {
		return nil, fmt.Errorf("the store refused the read at %s", prefix)
	}
	s.scans++
	found := map[string][]byte{}
	for key, value := range s.stored {
		if strings.HasPrefix(key, prefix) {
			found[key] = append([]byte(nil), value...)
		}
	}
	s.pairs += len(found)
	return found, nil
}

// counters answers the reads this store served so far: the scans, and the pairs
// those scans handed back for a reader to decode.
func (s *factStore) counters() (scans, pairs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scans, s.pairs
}

// Sync completes the write barrier of one staged write.
func (s *factStore) Sync(context.Context) error { return nil }

// TestAttentionChainOutlivesTheProcess pins the chain as the durable record.
//   - positive: a question and the answer it was ruled with are stored, and a book opened beside the same store answers the same presence and the same ruling
//   - negative: an unwritable store reports the refusal to the caller that filed the fact instead of reporting it as durable
//   - consumer: BindAttention is the seam the composition hands the durable face through
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestAttentionChainOutlivesTheProcess(t *testing.T) {
	ctx := context.Background()
	store := newFactStore()
	args := []byte(`{"cmd":"deploy"}`)
	first := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := first.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	held, err := first.Request(ctx, "durable-hold", "shell_exec", args,
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval")
	if err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	if _, err := first.Decide("durable-hold", ApprovalDenied, testApprovalApprover); err != nil {
		t.Fatalf("rule the hold denied: %v", err)
	}
	store.mu.Lock()
	stored := 0
	for key := range store.stored {
		if strings.HasPrefix(key, attentionFactPrefix) {
			stored++
		}
	}
	store.mu.Unlock()
	if stored != 2 {
		t.Fatalf("stored attention facts = %d, want the question and the answer the registration and the ruling filed", stored)
	}

	restarted := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := restarted.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	if got := restarted.Pending(); len(got) != 0 {
		t.Fatalf("the restarted waiting face = %+v, want the ruled question read back as answered", got)
	}
	question := questionKeyOf("shell_exec", ArgsDigest(args), held.ID)
	if !holdsFact(t, restarted, question) {
		t.Fatalf("the restarted chain holds no question %s for the hold the first process filed", question)
	}

	refusing := newFactStore()
	refusing.refuse = 1
	blocked := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := blocked.BindAttention(ctx, refusing); err != nil {
		t.Fatalf("bind the refusing durable face: %v", err)
	}
	_, err = blocked.Request(ctx, "refused-hold", "shell_exec", []byte(`{"cmd":"scale"}`),
		plugin.RiskCritical, "no-scale", "critical risk awaits approval")
	if err == nil {
		t.Fatal("a registration whose attention fact the store refused answered nil, want the refusal named to the caller")
	}
	if !strings.Contains(err.Error(), "refused the attention write") {
		t.Fatalf("registration error = %v, want the store's own refusal named", err)
	}
}

// TestAttentionChainRefusesAStoredBodyOfAnotherFact pins the fold against a mislabelled key.
//   - positive: the stored question is folded when its body states the fact its key names
//   - negative: a body naming another fact is refused by name instead of landing as that key
//   - consumer: foldAttentionFacts is the read BindAttention runs
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestAttentionChainRefusesAStoredBodyOfAnotherFact(t *testing.T) {
	ctx := context.Background()
	store := newFactStore()
	manager := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := manager.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	if _, err := manager.Request(ctx, "tampering-hold", "shell_exec", []byte(`{"cmd":"deploy"}`),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	store.mu.Lock()
	var key string
	for held := range store.stored {
		if strings.HasPrefix(held, attentionFactPrefix) {
			key = held
		}
	}
	raw, err := json.Marshal(event.Event{Key: "tagent:attention:other", Type: event.TypeAttentionQuestion})
	if key == "" {
		store.mu.Unlock()
		t.Fatal("the store holds no attention fact, so the tamper has nothing to overwrite")
	}
	store.stored[key] = raw
	store.mu.Unlock()
	if err != nil {
		t.Fatalf("render the tampered body: %v", err)
	}

	restarted := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	err = restarted.BindAttention(ctx, store)
	if err == nil {
		t.Fatal("a stored body naming another fact was folded, want the refusal by name")
	}
	if !strings.Contains(err.Error(), "the attention fact key stands for other content") {
		t.Fatalf("fold error = %v, want the conflict named", err)
	}
}

// TestRequestRecordKeepsTheIdentityItsKeyNames pins the request-record fold.
//   - positive: the identity a registration answered with is seated from the store alone, beside no stated cut
//   - negative: a record storing another identity than its key names is refused by name instead of seated under that key
//   - consumer: BindAttention folds the records through the same durable face the facts use
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestRequestRecordKeepsTheIdentityItsKeyNames(t *testing.T) {
	ctx := context.Background()
	store := newFactStore()
	first := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := first.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	held, err := first.Request(ctx, "seated-hold", "shell_exec", []byte(`{"cmd":"deploy"}`),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval")
	if err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	store.mu.Lock()
	record, stored := store.stored[approvalRequestKey("seated-hold")]
	store.mu.Unlock()
	if !stored {
		t.Fatalf("the store holds no request record under %s, so the identity has no durable record", approvalRequestKey("seated-hold"))
	}

	restarted := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := restarted.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	if _, err := restarted.Decide(held.ID, ApprovalApproved, testApprovalApprover); err != nil {
		t.Fatalf("decide the identity the first book named: %v — the record %s seated no identity", err, approvalRequestKey("seated-hold"))
	}

	var tampered approvalRequestRecord
	if err := json.Unmarshal(record, &tampered); err != nil {
		t.Fatalf("read the stored request record: %v", err)
	}
	tampered.Asked = "another-hold"
	raw, err := json.Marshal(tampered)
	if err != nil {
		t.Fatalf("render the tampered record: %v", err)
	}
	store.mu.Lock()
	store.stored[approvalRequestKey("seated-hold")] = raw
	store.mu.Unlock()

	blocked := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	err = blocked.BindAttention(ctx, store)
	if err == nil {
		t.Fatal("a record storing another identity than its key names was seated, want the refusal by name")
	}
	if !strings.Contains(err.Error(), approvalRequestKey("seated-hold")) {
		t.Fatalf("fold error = %v, want the key %s named", err, approvalRequestKey("seated-hold"))
	}
}

// TestApprovalHistoryReadFailureDoesNotMintANewRequest pins the tail read of a stated book.
//   - positive: a book whose cut states an open hold boots beside a face that answers no tail, and still reports that hold
//   - negative: a face refusing the hold's own answer key is refused by name, and the hold still stands after the refusal
//   - negative: the same gate reading no request would mint one the approver never saw, so the store must keep its two records
//   - consumer: BindAttention is the read the production boot runs after the restore, and Lookup the read the gate takes before it mints
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestApprovalHistoryReadFailureDoesNotMintANewRequest(t *testing.T) {
	ctx := context.Background()
	store := newFactStore()
	args := []byte(`{"cmd":"deploy"}`)
	digest := ArgsDigest(args)
	writer := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := writer.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	open, err := writer.Request(ctx, "open-hold", "shell_exec", args,
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval")
	if err != nil {
		t.Fatalf("register the open hold: %v", err)
	}
	if _, err := writer.Request(ctx, "settled-hold", "shell_exec", []byte(`{"cmd":"scale"}`),
		plugin.RiskCritical, "no-scale", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the settled hold: %v", err)
	}
	if _, err := writer.Decide("settled-hold", ApprovalDenied, testApprovalApprover); err != nil {
		t.Fatalf("rule the settled hold denied: %v", err)
	}
	stated, keys := writer.SnapshotEvidence()
	if len(stated) != 1 || stated[0].ID != open.ID {
		t.Fatalf("the cut states %+v, want the one hold still open", stated)
	}

	restarted := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := restarted.RestoreEvidence(ctx, stated, keys); err != nil {
		t.Fatalf("restore the stated evidence: %v", err)
	}
	refused := factKey(answerKeyOf("shell_exec", digest, open.ID))
	store.mu.Lock()
	store.refuseKey = refused
	store.mu.Unlock()
	err = restarted.BindAttention(ctx, store)
	if err == nil {
		t.Fatal("a face refusing the hold's own answer key bound as landed, want the refusal named to the boot")
	}
	if !strings.Contains(err.Error(), refused) || !strings.Contains(err.Error(), "refused the read") {
		t.Fatalf("bind error = %v, want the refused key %s and the store's own words named", err, refused)
	}

	if got := restarted.Pending(); len(got) != 1 || got[0].ID != open.ID {
		t.Fatalf("the book after the refused read answers %+v, want the hold the cut states", got)
	}
	drawn := restarted.Lookup("shell_exec", digest)
	if drawn == nil || drawn.ID != open.ID || drawn.Status != ApprovalPending {
		t.Fatalf("lookup after the refused read = %+v, want the hold the cut stated — a gate reading no request mints one the approver never saw", drawn)
	}
	store.mu.Lock()
	records := 0
	for key := range store.stored {
		if strings.HasPrefix(key, approvalRequestPrefix) {
			records++
		}
	}
	store.mu.Unlock()
	if records != 2 {
		t.Fatalf("the store holds %d request records, want the two the one process registered: a refused read minted an identity", records)
	}
}

// TestAttentionBindReadsTheObligationsNotTheHistory pins what a stated boot pays.
//   - positive: a book whose cut states three obligations binds beside a settled history at the cost of those three keys, seating the same three
//   - negative: the same bind over a history twice as long costs the same scans, decodes and residents
//   - negative: a book beside no stated cut decodes the whole stream, so the flat read is the cut's and not a store this test cannot see
//   - consumer: BindAttention is the boot read the governance seat runs, and Lookup the gate read the settled history answers by identity
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestAttentionBindReadsTheObligationsNotTheHistory(t *testing.T) {
	ctx := context.Background()
	store := newFactStore()
	writer := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := writer.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	const openRows, settledRows = 3, 12
	for i := 0; i < openRows; i++ {
		id := fmt.Sprintf("open-%02d", i)
		if _, err := writer.Request(ctx, id, "shell_exec", []byte(fmt.Sprintf(`{"cmd":%q}`, id)),
			plugin.RiskCritical, "no-open", "critical risk awaits approval"); err != nil {
			t.Fatalf("register the open hold %s: %v", id, err)
		}
	}
	settled := 0
	settle := func(rounds int) {
		for i := 0; i < rounds; i++ {
			id := fmt.Sprintf("settled-%02d", settled)
			settled++
			if _, err := writer.Request(ctx, id, "shell_exec", []byte(fmt.Sprintf(`{"cmd":%q}`, id)),
				plugin.RiskCritical, "no-settled", "critical risk awaits approval"); err != nil {
				t.Fatalf("register the settled hold %s: %v", id, err)
			}
			if _, err := writer.Decide(id, ApprovalDenied, testApprovalApprover); err != nil {
				t.Fatalf("rule the settled hold %s denied: %v", id, err)
			}
		}
	}
	settle(settledRows)

	boot := func() (scans, pairs, resident int, allocated uint64) {
		return statedBootCost(t, ctx, store, writer, openRows)
	}

	scans, pairs, resident, bytes := boot()
	t.Logf("stated bind beside %d settled rounds: scans=%d decoded=%d resident=%d allocated=%d bytes",
		settledRows, scans, pairs, resident, bytes)
	if scans != openRows {
		t.Fatalf("the stated bind served %d scans, want the %d answer keys of the obligations it states", scans, openRows)
	}
	if pairs > openRows {
		t.Fatalf("the stated bind decoded %d bodies, want no more than the %d obligations it states", pairs, openRows)
	}
	if resident != openRows {
		t.Fatalf("the stated bind seats %d requests, want the %d obligations the cut states", resident, openRows)
	}

	settle(settledRows)
	grownScans, grownPairs, grownResident, grownBytes := boot()
	t.Logf("stated bind beside %d settled rounds: scans=%d decoded=%d resident=%d allocated=%d bytes",
		2*settledRows, grownScans, grownPairs, grownResident, grownBytes)
	if grownScans != scans || grownPairs != pairs || grownResident != resident {
		t.Fatalf("the bind of one stated cut cost scans=%d decoded=%d resident=%d beside %d settled rounds and scans=%d decoded=%d resident=%d beside %d, want the cut's own work either way",
			scans, pairs, resident, settledRows, grownScans, grownPairs, grownResident, 2*settledRows)
	}

	bare := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	beforeScans, beforePairs := store.counters()
	if err := bare.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face beside no stated cut: %v", err)
	}
	afterScans, afterPairs := store.counters()
	t.Logf("bind beside no stated cut: scans=%d decoded=%d", afterScans-beforeScans, afterPairs-beforePairs)
	if afterPairs-beforePairs <= pairs {
		t.Fatalf("the bind beside no stated cut decoded %d bodies, want the whole stream — this store answered only %d, so the flat read above is not a store the test cannot see",
			afterPairs-beforePairs, pairs)
	}

	settledDigest := ArgsDigest([]byte(fmt.Sprintf(`{"cmd":%q}`, "settled-00")))
	drawn := writer.Lookup("shell_exec", settledDigest)
	if drawn == nil || drawn.ID == "" || drawn.Status != ApprovalDenied {
		t.Fatalf("the gate read of a settled pair = %+v, want the identity that pair was ruled under", drawn)
	}
	if !strings.HasPrefix(drawn.ID, "settled-") {
		t.Fatalf("the gate read of a settled pair names %q, want the identity the record was filed under", drawn.ID)
	}
}

// statedBootCost measures one bind of the stated cut: the store reads and decoded
// bodies it served, the requests it seated, and the bytes it allocated, all beside
// whatever settled history the store holds at that moment.
func statedBootCost(t *testing.T, ctx context.Context, store *factStore, writer *ApprovalManager, openRows int) (scans, pairs, resident int, allocated uint64) {
	t.Helper()
	stated, keys := writer.SnapshotEvidence()
	if len(stated) != openRows {
		t.Fatalf("the cut states %d obligations, want the %d holds still open", len(stated), openRows)
	}
	book := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := book.RestoreEvidence(ctx, stated, keys); err != nil {
		t.Fatalf("restore the stated evidence: %v", err)
	}
	beforeScans, beforePairs := store.counters()
	var beforeAllocated runtime.MemStats
	runtime.ReadMemStats(&beforeAllocated)
	if err := book.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	var afterAllocated runtime.MemStats
	runtime.ReadMemStats(&afterAllocated)
	afterScans, afterPairs := store.counters()
	return afterScans - beforeScans, afterPairs - beforePairs, len(book.evidence),
		afterAllocated.TotalAlloc - beforeAllocated.TotalAlloc
}
