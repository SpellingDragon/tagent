package host

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/runtime/agent"
)

// TestQuiesceCancelledCallerAnswersCut pins that a close whose caller cancelled is answered Cut where the run stands, never Broken.
// - the criterion is a read of the account, so a cancelled caller spends the budget of the close
// - work that stands on the chain is answered as work a restart resumes
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestQuiesceCancelledCallerAnswersCut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	scene := unconsumedSceneUnder(t, ctx, true)
	defer scene.releaseWorker()
	defer scene.releaseFollowUp()

	// The notification's follow-up turn is on the chain but has not answered;
	// waiting for it to reach the provider reads the close beside work the run
	// still owes, not beside the failure a fast endpoint would have settled.
	select {
	case <-scene.followUpSeen:
	case <-ctx.Done():
		t.Fatal("the delegated notification never reached its follow-up turn")
	}
	cancelled, cancelCall := context.WithCancel(ctx)
	cancelCall()
	report, err := scene.host.Quiesce(cancelled, agent.CloseRequest{Timeout: time.Now().Add(400 * time.Millisecond)})
	if err != nil {
		t.Fatalf("quiesce beside a cancelled caller = %v, want Cut as a normal return", err)
	}
	if report.Outcome != agent.CloseCut || report.Drained {
		t.Fatalf("report = %+v, want Cut beside the work the run still owes", report)
	}
	if len(report.Unsettled) == 0 {
		t.Fatalf("report = %+v, want the criterion answered as the run stands", report)
	}
	scene.releaseFollowUp()
	if err := scene.host.Close(ctx); err != nil {
		t.Fatalf("close of the host = %v, want the facilities to stop", err)
	}
}
