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
