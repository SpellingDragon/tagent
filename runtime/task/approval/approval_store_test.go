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
