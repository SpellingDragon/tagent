// The approval store checks: the request file one identity names and the ruling
// written beside it, under the two writes a parked call and its approver make.
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/durable"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// approvalRaceRounds is the count of one request-and-ruling pairs the race check
// drives: every round opens its own directory, so the pair of one round never
// meets the pair of another.
const approvalRaceRounds = 64

// TestApprovalRulingBesideItsRequestKeepsTheFile pins the request file one identity keeps under a ruling filed beside it.
// - a ruling filed beside a registration never fails the registration it races
// - the request file a registration names still stands after the ruling landed
// - the file carries the ruling the manager answers for that identity
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestApprovalRulingBesideItsRequestKeepsTheFile(t *testing.T) {
	ctx := context.Background()
	for round := 0; round < approvalRaceRounds; round++ {
		dir := t.TempDir()
		manager := NewApprovalManager(dir, time.Hour)
		id := fmt.Sprintf("call-approval-race-%02d", round)
		registered := make(chan error, 1)
		go func() {
			_, err := manager.Request(ctx, id, "exec", []byte(`{"command":"ls"}`), plugin.RiskHigh, "rule-a", "the call needs a ruling")
			registered <- err
		}()
		decided, err := decideWhenRegistered(ctx, manager, id)
		if err != nil {
			t.Fatalf("round %d: rule the request %s: %v", round, id, err)
		}
		if err := <-registered; err != nil {
			t.Fatalf("round %d: the registration of %s beside its ruling = %v", round, id, err)
		}
		if decided.Status != ApprovalApproved {
			t.Fatalf("round %d: the ruling = %s, want the approved request", round, decided.Status)
		}
		path := filepath.Join(dir, "approvals", id+".json")
		stored := readApprovalFile(t, path)
		if stored.ID != id {
			t.Fatalf("round %d: the request file names %q, want %q", round, stored.ID, id)
		}
		if stored.Status != ApprovalApproved || stored.DecidedBy != "the-test-approver" {
			t.Fatalf("round %d: the request file records %s by %q, want the approved ruling", round, stored.Status, stored.DecidedBy)
		}
	}
}

// decideWhenRegistered rules one request from the moment its registration is
// visible, which is the adjacency a parked call and its approver run under: the
// ruling is retried while the registration is unknown and fails on every other
// refusal.
func decideWhenRegistered(ctx context.Context, manager *ApprovalManager, id string) (*ApprovalRequest, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		req, err := manager.Decide(id, ApprovalApproved, "the-test-approver")
		if err == nil {
			return req, nil
		}
		if !errors.Is(err, ErrApprovalNotFound) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: %s never registered", ErrApprovalNotFound, id)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		runtime.Gosched()
	}
}

// readApprovalFile reads one request file back as the store wrote it.
func readApprovalFile(t *testing.T, path string) ApprovalRequest {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the request file %s: %v", path, err)
	}
	var req ApprovalRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode the request file %s: %v", path, err)
	}
	return req
}

// bindBook constructs one book over one directory and hands it the durable face,
// which is the two calls a composition makes at boot: the directory fold of the
// construction and the bind that folds the facts and arms the request records.
func bindBook(t *testing.T, ctx context.Context, dir string, face durable.FactLog, opts ...ApprovalOption) *ApprovalManager {
	t.Helper()
	options := append([]ApprovalOption{WithApprovalRescanInterval(0)}, opts...)
	book := NewApprovalManager(dir, time.Hour, options...)
	if err := book.BindAttention(ctx, face); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	return book
}

// requestRecordOf reads the stored request record one caller identity is seated under.
func requestRecordOf(t *testing.T, store *factStore, asked string) (approvalRequestRecord, bool) {
	t.Helper()
	store.mu.Lock()
	raw, stored := store.stored[approvalRequestKey(asked)]
	store.mu.Unlock()
	if !stored {
		return approvalRequestRecord{}, false
	}
	var record approvalRequestRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("read the stored request record under %s: %v", approvalRequestKey(asked), err)
	}
	return record, true
}

// TestUnseatedBookBindsAfterItsDirectoryFold pins the two folds meeting on one identity.
//   - positive: a book that opened a request file at construction binds the durable face the same registration wrote, and answers the one hold both name
//   - negative: the bind arms the request-record write leg, so the identity a registration answers with lands in the store beside its facts
//   - consumer: NewApprovalManager over a directory is the book a composition wires, and BindAttention is the boot seam that hands it the tier's fact log
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestUnseatedBookBindsAfterItsDirectoryFold(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	args := []byte(`{"cmd":"deploy"}`)
	store := newFactStore()
	writer := bindBook(t, ctx, dir, store)
	if _, err := writer.Request(ctx, "dir-hold", "shell_exec", args,
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	mirror := filepath.Join(dir, "approvals", "dir-hold.json")
	if _, err := os.Stat(mirror); err != nil {
		t.Fatalf("the request file %s is missing, so the construction fold reads nothing: %v", mirror, err)
	}
	question := questionKeyOf("shell_exec", ArgsDigest(args), "dir-hold")
	store.mu.Lock()
	_, factStored := store.stored[factKey(question)]
	store.mu.Unlock()
	if !factStored {
		t.Fatalf("the store holds no question fact for the registration, so the bind folds nothing to meet the file: %s", question)
	}

	restarted := NewApprovalManager(dir, time.Hour, WithApprovalRescanInterval(0))
	if got := restarted.Pending(); len(got) != 1 || got[0].ID != "dir-hold" {
		t.Fatalf("the book opened over the directory answers %+v before the bind, want the hold its request file states", got)
	}
	if err := restarted.BindAttention(ctx, store); err != nil {
		t.Fatalf("the bind refused one registration's own file and fact: %v — the file and the fact state one question", err)
	}
	if got := restarted.Pending(); len(got) != 1 || got[0].ID != "dir-hold" {
		t.Fatalf("the bound book answers %+v, want the one hold both folds name", got)
	}
	if gaps := restarted.DeliveryGaps(); len(gaps) != 0 {
		t.Fatalf("the bind recorded %+v, want no visible failure", gaps)
	}
	if !holdsFact(t, restarted, question) {
		t.Fatalf("the bound chain holds no question %s, want the fact the directory fold and the bind name", question)
	}

	if _, err := restarted.Request(ctx, "bound-hold", "shell_exec", []byte(`{"cmd":"scale"}`),
		plugin.RiskCritical, "no-scale", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the second hold: %v", err)
	}
	record, stored := requestRecordOf(t, store, "bound-hold")
	if !stored {
		t.Fatalf("the store holds no request record under %s after the bind: the identity the bound book answered outlives no process",
			approvalRequestKey("bound-hold"))
	}
	if record.Asked != "bound-hold" || record.Request.ID != "bound-hold" {
		t.Fatalf("the stored request record states asked=%q request=%q, want the identity the registration answered",
			record.Asked, record.Request.ID)
	}
}

// TestUnseatedBookBindsARuledRequestFromItsFile pins the two folds meeting on a ruled hold.
//   - positive: a ruled request file restored at construction binds the durable face the same ruling wrote, and reads the same ruling back
//   - negative: the ruling stands before the bind, so the agreement the bind needs is the one the file and the fact share, not an empty chain
//   - consumer: NewApprovalManager over a directory beside BindAttention is the book a mounted composition boots, and Lookup reads its ruling
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestUnseatedBookBindsARuledRequestFromItsFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	args := []byte(`{"cmd":"deploy"}`)
	digest := ArgsDigest(args)
	store := newFactStore()
	writer := bindBook(t, ctx, dir, store)
	if _, err := writer.Request(ctx, "ruled-hold", "shell_exec", args,
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	if _, err := writer.Decide("ruled-hold", ApprovalApproved, testApprovalApprover); err != nil {
		t.Fatalf("rule the hold approved: %v", err)
	}

	restarted := NewApprovalManager(dir, time.Hour, WithApprovalRescanInterval(0))
	before := restarted.Lookup("shell_exec", digest)
	if before == nil || before.Status != ApprovalApproved || before.DecidedBy != testApprovalApprover {
		t.Fatalf("the book opened over the directory reads %+v before the bind, want the ruling its request file states", before)
	}
	if err := restarted.BindAttention(ctx, store); err != nil {
		t.Fatalf("the bind refused one ruling's own file and fact: %v — the file and the fact state one answer", err)
	}
	after := restarted.Lookup("shell_exec", digest)
	if after == nil || after.Status != ApprovalApproved || after.DecidedBy != testApprovalApprover {
		t.Fatalf("the bound book reads %+v, want the ruling both folds name", after)
	}
	if repeat, err := restarted.Decide("ruled-hold", ApprovalApproved, testApprovalApprover); err != nil || repeat.Status != ApprovalApproved {
		t.Fatalf("the bound book answers repeat=%+v err=%v, want the ruling already on the chain", repeat, err)
	}
	if _, stored := requestRecordOf(t, store, "ruled-hold"); !stored {
		t.Fatalf("the store holds no request record under %s: the identity the ruled registration answered outlives no process",
			approvalRequestKey("ruled-hold"))
	}
}

// TestSeatedBookBindsBeforeItReadsTheDirectory pins the construction order a seated book boots through.
//   - positive: a seated book answers no hold before its bind, though the request file stands on disk
//   - negative: the same book answers that hold once the bind folds the fact its registration wrote, so the empty read is the seat and not a missing file
//   - consumer: WithApprovalSeat is the option the host passes for the book the governance state holds, and BindAttention is the only fold it runs
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestSeatedBookBindsBeforeItReadsTheDirectory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	args := []byte(`{"cmd":"deploy"}`)
	store := newFactStore()
	writer := bindBook(t, ctx, dir, store, WithApprovalSeat())
	if _, err := writer.Request(ctx, "seat-hold", "shell_exec", args,
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	mirror := filepath.Join(dir, "approvals", "seat-hold.json")
	if _, err := os.Stat(mirror); err != nil {
		t.Fatalf("the request file %s is missing, so the empty read proves nothing: %v", mirror, err)
	}

	seated := NewApprovalManager(dir, time.Hour, WithApprovalRescanInterval(0), WithApprovalSeat())
	if got := seated.Pending(); len(got) != 0 {
		t.Fatalf("the seated book answers %+v before the bind, want an empty read: the seat takes its holds from the stated state", got)
	}
	if err := seated.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	if got := seated.Pending(); len(got) != 1 || got[0].ID != "seat-hold" {
		t.Fatalf("the bound book answers %+v, want the hold its registration filed", got)
	}
}

// TestSettledApprovalsStayReadableWithoutResidentEvidence pins what a bound book keeps beside growing history.
//   - positive: a bound book states and keeps the obligations still open however many requests it settled
//   - negative: the fixture grows the history it measures, so the constant resident set is a bounded book and not an empty store
//   - consumer: ApprovalManager.Lookup is the read the production gate rules a critical call through, and Decide the approver's answers
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestSettledApprovalsStayReadableWithoutResidentEvidence(t *testing.T) {
	ctx := context.Background()
	nowMs := time.Now().UnixMilli()
	clock := func() time.Time { return time.UnixMilli(nowMs) }
	store := newFactStore()
	book := NewApprovalManager(t.TempDir(), time.Hour, WithApprovalClock(clock), WithApprovalRescanInterval(0))
	if err := book.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	args := func(round int) []byte { return []byte(fmt.Sprintf(`{"cmd":"settle-%d"}`, round)) }
	identity := func(round int) string { return fmt.Sprintf("settled-%02d", round) }
	if _, err := book.Request(ctx, "open-hold", "shell_exec", []byte(`{"cmd":"open"}`),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the open hold: %v", err)
	}
	stated := func(t *testing.T, book *ApprovalManager) []ApprovalRequest {
		t.Helper()
		held, _ := book.SnapshotEvidence()
		return held
	}
	resident := func(book *ApprovalManager) int {
		book.mu.RLock()
		defer book.mu.RUnlock()
		return len(book.evidence)
	}
	if got := stated(t, book); len(got) != 1 || got[0].ID != "open-hold" {
		t.Fatalf("stated approvals before any ruling = %+v, want the one obligation still open", got)
	}

	const settledRounds = 5
	for round := 0; round < settledRounds; round++ {
		if _, err := book.Request(ctx, identity(round), "shell_exec", args(round),
			plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
			t.Fatalf("register the request of round %d: %v", round, err)
		}
		if _, err := book.Decide(identity(round), ApprovalDenied, testApprovalApprover); err != nil {
			t.Fatalf("rule the request of round %d denied: %v", round, err)
		}
		if got := stated(t, book); len(got) != 1 || got[0].ID != "open-hold" {
			t.Fatalf("stated approvals after %d rulings = %+v, want the one obligation still open: a cut costs the work still owed",
				round+1, got)
		}
		if got := resident(book); got != 1 {
			t.Fatalf("resident requests after %d rulings = %d, want the one obligation still open: a ruled request is read from the chain that ruled it",
				round+1, got)
		}
	}

	store.mu.Lock()
	facts, records := 0, 0
	for key := range store.stored {
		if strings.HasPrefix(key, attentionFactPrefix) {
			facts++
		}
		if strings.HasPrefix(key, approvalRequestPrefix) {
			records++
		}
	}
	store.mu.Unlock()
	if facts != 2*settledRounds+1 || records != settledRounds+1 {
		t.Fatalf("the store holds %d attention facts and %d request records, want the question of the open hold, one question and one answer per ruling and one record per identity: the fixture did not grow the history it measures",
			facts, records)
	}

	restarted := NewApprovalManager(t.TempDir(), time.Hour, WithApprovalClock(clock), WithApprovalRescanInterval(0))
	if err := restarted.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face of the restarted book: %v", err)
	}
	if got := restarted.Pending(); len(got) != 1 || got[0].ID != "open-hold" {
		t.Fatalf("the restarted waiting face = %+v, want the one obligation still open", got)
	}
	if got := stated(t, restarted); len(got) != 1 || got[0].ID != "open-hold" {
		t.Fatalf("stated approvals beside the restarted book = %+v, want the one obligation still open", got)
	}
	if got := resident(restarted); got != 1 {
		t.Fatalf("resident requests beside the restarted book = %d, want the one obligation still open", got)
	}

	last := settledRounds - 1
	repeat, err := restarted.Decide(identity(last), ApprovalApproved, "a-later-approver")
	if err != nil {
		t.Fatalf("the ruled identity %s no longer resolves after the restart: %v — the cut states the obligations still open, so the ruling is read back from the chain by the identity it was filed under",
			identity(last), err)
	}
	if repeat.Status != ApprovalDenied || repeat.DecidedBy != testApprovalApprover {
		t.Fatalf("the restarted book answers the identity %s with %s by %q, want the refusal the person stated",
			identity(last), repeat.Status, repeat.DecidedBy)
	}
	seen := restarted.Lookup("shell_exec", ArgsDigest(args(last)))
	if seen == nil || seen.ID != identity(last) || seen.Status != ApprovalDenied {
		t.Fatalf("the gate reads %+v for the refused arguments, want the refusal named under %s: a restart that answered nothing would mint a request nobody was asked for",
			seen, identity(last))
	}
	if got := resident(restarted); got != 1 {
		t.Fatalf("resident requests beside a book that read two rulings back = %d, want the one obligation still open: reading a ruling back never seats the history",
			got)
	}
	if got := stated(t, restarted); len(got) != 1 || got[0].ID != "open-hold" {
		t.Fatalf("stated approvals after two rulings were read back = %+v, want the one obligation still open", got)
	}

	besideNoFace := NewApprovalManager(t.TempDir(), time.Hour, WithApprovalClock(clock), WithApprovalRescanInterval(0))
	if _, err := besideNoFace.Request(ctx, "face-less-hold", "shell_exec", []byte(`{"cmd":"face-less"}`),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the hold of the face-less book: %v", err)
	}
	if _, err := besideNoFace.Decide("face-less-hold", ApprovalDenied, testApprovalApprover); err != nil {
		t.Fatalf("rule the hold of the face-less book denied: %v", err)
	}
	statedBesideNoFace, keysBesideNoFace := besideNoFace.SnapshotEvidence()
	if len(statedBesideNoFace) != 1 || statedBesideNoFace[0].Status != ApprovalDenied {
		t.Fatalf("the face-less book states %+v, want the ruling: a book beside no durable face has the cut alone as its record", statedBesideNoFace)
	}
	migrated := NewApprovalManager(t.TempDir(), time.Hour, WithApprovalClock(clock), WithApprovalRescanInterval(0))
	if err := migrated.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face of the migrated book: %v", err)
	}
	if err := migrated.RestoreEvidence(ctx, statedBesideNoFace, keysBesideNoFace); err != nil {
		t.Fatalf("restore the cut of the face-less book: %v", err)
	}
	for _, held := range stated(t, migrated) {
		if held.Status == ApprovalDenied || held.Status == ApprovalApproved {
			t.Fatalf("the migrated book states the ruled request %s, want the obligations still open alone: a book that reached the chain reads its rulings back instead of carrying them",
				held.ID)
		}
	}
	if seen := migrated.Lookup("shell_exec", ArgsDigest([]byte(`{"cmd":"face-less"}`))); seen == nil || seen.ID != "face-less-hold" || seen.Status != ApprovalDenied {
		t.Fatalf("the migrated book reads %+v for the ruled arguments, want the refusal the face-less book stated", seen)
	}
}

// approvalWriteSeam is one attention store a check arms: it stages writes and
// publishes them behind a barrier, and it refuses the n-th write or the n-th
// barrier on demand, so each leg of a filing answers the failure a real store
// answers there.
type approvalWriteSeam struct {
	mu       sync.Mutex
	stored   map[string][]byte
	staged   map[string][]byte
	puts     int
	syncs    int
	failPut  int
	failSync int
}

// newApprovalWriteSeam returns one empty seam.
func newApprovalWriteSeam() *approvalWriteSeam {
	return &approvalWriteSeam{stored: map[string][]byte{}, staged: map[string][]byte{}}
}

// Put stages one fact, or refuses the write of the armed leg.
func (s *approvalWriteSeam) Put(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts++
	if s.failPut != 0 && s.puts == s.failPut {
		return fmt.Errorf("the store refused the write at %s", key)
	}
	s.staged[key] = append([]byte(nil), value...)
	return nil
}

// Scan answers every published pair under the prefix: a staged write whose
// barrier did not complete is not published to a reader.
func (s *approvalWriteSeam) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := map[string][]byte{}
	for key, value := range s.stored {
		if strings.HasPrefix(key, prefix) {
			found[key] = append([]byte(nil), value...)
		}
	}
	return found, nil
}

// Sync completes the barrier over everything staged so far, or refuses it, in
// which case the staged writes stay unpublished.
func (s *approvalWriteSeam) Sync(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncs++
	if s.failSync != 0 && s.syncs == s.failSync {
		return fmt.Errorf("the store refused the barrier")
	}
	for key, value := range s.staged {
		s.stored[key] = value
	}
	s.staged = map[string][]byte{}
	return nil
}

// arm names the leg the next write or barrier refuses.
func (s *approvalWriteSeam) arm(failPut, failSync int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failPut, s.failSync = failPut, failSync
}

// TestApprovalWriteFailureDoesNotGrantPermission pins the ruling one failed write leaves behind against the ruling the chain states.
//   - a question write or barrier the store refused leaves the pair unruled, so the gate that reads it approves nothing
//   - an answer write or barrier the store refused leaves the chain holding no answer, so the pair the gate reads stands unruled until the ruling lands
//   - a ruling that landed afterwards is read back by a book opened over the same store, so the retry and the chain answer one status for one pair
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestApprovalWriteFailureDoesNotGrantPermission(t *testing.T) {
	ctx := context.Background()
	args := []byte(`{"cmd":"deploy"}`)
	digest := ArgsDigest(args)
	for _, held := range []struct {
		name     string
		arming   string
		failPut  int
		failSync int
	}{
		{name: "the question write is refused", arming: "registration", failPut: 1},
		{name: "the question barrier fails", arming: "registration", failSync: 1},
		{name: "the answer write is refused", arming: "ruling", failPut: 3},
		{name: "the answer barrier fails", arming: "ruling", failSync: 3},
	} {
		t.Run(held.name, func(t *testing.T) {
			seam := newApprovalWriteSeam()
			book := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
			if err := book.BindAttention(ctx, seam); err != nil {
				t.Fatalf("bind the durable face: %v", err)
			}
			id := "approval-write-" + held.arming
			if held.arming == "registration" {
				seam.arm(held.failPut, held.failSync)
				if _, err := book.Request(ctx, id, "shell_exec", args, plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err == nil {
					t.Fatalf("the registration answered no failure beside the armed %s leg, want the refusal the store answered", held.name)
				}
				seam.arm(0, 0)
			} else {
				if _, err := book.Request(ctx, id, "shell_exec", args, plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
					t.Fatalf("register the request over a store that carried every write: %v", err)
				}
				seam.arm(held.failPut, held.failSync)
				if _, err := book.Decide(id, ApprovalApproved, "approver"); err == nil {
					t.Fatalf("the ruling answered no failure beside the armed %s leg, want the refusal the store answered", held.name)
				}
				seam.arm(0, 0)
			}
			if req := book.Lookup("shell_exec", digest); req != nil && req.Status == ApprovalApproved {
				t.Fatalf("the gate approves the pair beside the failed %s leg %+v, want it unruled: the ruling never reached the chain",
					held.arming, req)
			}
			if _, err := book.Decide(id, ApprovalApproved, "approver"); err != nil {
				t.Fatalf("rule the pair again after the failed leg: %v", err)
			}
			req := book.Lookup("shell_exec", digest)
			if req == nil || req.Status != ApprovalApproved {
				t.Fatalf("the gate answers %+v after the ruling landed, want the approval", req)
			}
			reopened := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
			if err := reopened.BindAttention(ctx, seam); err != nil {
				t.Fatalf("bind the durable face of the second book: %v", err)
			}
			again := reopened.Lookup("shell_exec", digest)
			if again == nil || again.Status != ApprovalApproved {
				t.Fatalf("the book opened over the same store answers %+v, want the ruling the retry landed", again)
			}
		})
	}
}
