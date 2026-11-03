// The storm fuse parked on the attention channel: the batch one over-limit model
// response raises is one attention question on the approval manager, and the
// ruling the wait reads is the answer that question carries.
//
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
package execution

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// TestFuseBatchParksOnAttention pins the parked storm batch as one attention question.
// - an over-limit response parks as one waiting request, and nothing of it runs while it waits
// - the ruling closes that question on the waiting face, so the parked wait is gone the moment it is answered and the whole batch runs
// - a manager rebuilt over the same directory reads the ruling back, so the answer the question carries is the truth the fuse waited on
//
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestFuseBatchParksOnAttention(t *testing.T) {
	fuseLimits(t, agent.FuseLimits{CallCap: 64})
	book, dir := approvalBook(t, time.Hour)
	run, _, tool, addr := responseStormRig(t, "session-attention-batch")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	errs := invokeAsync(ctx, run, assembly.InvokeRequest{
		InvocationID: "inv-attention-batch-1",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "run every rotation at once"}},
	})

	parked := awaitBatchRequest(t, book, dir)
	if got := tool.calls(); got != 0 {
		t.Fatalf("dispatched calls of the parked batch = %d, want the wait to run nothing", got)
	}
	if pending := book.Pending(); len(pending) != 1 || pending[0].ID != parked.ID {
		t.Fatalf("waiting face while the batch waits = %+v, want the one question it parked", pending)
	}
	waiting := book.Lookup(parked.ToolName, parked.ArgsDigest)
	if waiting == nil || waiting.Status != approval.ApprovalPending {
		t.Fatalf("lookup of the parked batch = %+v, want the wait it registered", waiting)
	}

	decideBatch(t, book, parked.ID, approval.ApprovalApproved)
	awaitAsync(t, errs)
	if got := awaitToolCalls(t, tool, responseStormCalls); got != responseStormCalls {
		t.Fatalf("dispatched calls of the approved batch = %d, want every call of it", got)
	}
	if pending := book.Pending(); len(pending) != 0 {
		t.Fatalf("waiting face after the ruling = %+v, want the answered question gone", pending)
	}
	restarted := approval.NewApprovalManager(filepath.Dir(dir), time.Hour)
	seen := restarted.Lookup(parked.ToolName, parked.ArgsDigest)
	if seen == nil || seen.ID != parked.ID || seen.Status != approval.ApprovalApproved {
		t.Fatalf("restarted lookup = %+v, want the ruling the answer carries read back", seen)
	}
}
