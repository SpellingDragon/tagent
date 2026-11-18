package assembly

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// TestAttentionWorkEndReadsTheCloseVerdict pins the work-end gate as a read of the close's own verdict.
//
// - a run whose close stored a drained verdict reads as ended and drained
// - a run whose close stored no verdict yet reads as ended and not drained
//
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestAttentionWorkEndReadsTheCloseVerdict(t *testing.T) {
	settled := hostReading{runReading: runReading{run: &Run{closing: true, closeDrained: true}}}
	if ended, drained := settled.attentionWorkEnd(); !ended || !drained {
		t.Fatalf("the work end beside a close that stored a drained verdict = (%v, %v), want the stored verdict read back", ended, drained)
	}
	open := hostReading{runReading: runReading{run: &Run{closing: true}}}
	if ended, drained := open.attentionWorkEnd(); !ended || drained {
		t.Fatalf("the work end beside a close that stored no verdict yet = (%v, %v), want the drain read as unfinished", ended, drained)
	}
	if ended, drained := (hostReading{}).attentionWorkEnd(); ended || drained {
		t.Fatalf("the work end beside no run = (%v, %v), want no work end read at all", ended, drained)
	}
}

// TestAttentionQuestionsClearsApprovalOnDrainedClose pins the work-end clearance of a presence the drain does not owe.
//
// - one critical call parked through the approval channel still stands in the presence after the drained close of its run
// - the attention read face beside that close answers no question at all, so the drained work end clears the presence
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestAttentionQuestionsClearsApprovalOnDrainedClose(t *testing.T) {
	host, book := bootParkedApprovalHost(t, "approval-clearance-1")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	report := closeDrainedWork(t, host)
	present := book.AttentionPresence(ctx)
	t.Logf("premise: close report = {outcome: %q, drained: %v, unsettled: %d} beside %d approval question(s) still present",
		report.Outcome, report.Drained, len(report.Unsettled), len(present))
	if len(present) != 1 {
		t.Fatalf("the approval presence after the drained close = %+v, want the parked call the drain does not owe", present)
	}

	questions, err := attentionFaceOf(host, book).AttentionQuestions(ctx)
	if err != nil {
		t.Fatalf("the attention read face after the drained close = %v, want the questions of a cleared work end", err)
	}
	if len(questions) != 0 {
		t.Fatalf("attention questions after the drained close = %+v, want none: a work end that drained every obligation it owed clears the presence, and the approval question beside it is one the drain does not owe", questions)
	}
}

// TestAttentionQuestionsAnswersBesideClosedViewRefusal pins the cleared read as the one answer left beside a closed view.
//
// - the task read of the closed run refuses by name, and so does the projection read over it
// - the attention read face beside the same run answers its own dimension with no error at all
//
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestAttentionQuestionsAnswersBesideClosedViewRefusal(t *testing.T) {
	host, book := bootParkedApprovalHost(t, "approval-closed-view-1")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	closeDrainedWork(t, host)
	run := host.Run()

	if _, err := (runReading{run: run}).Tasks(ctx); !errors.Is(err, ledger.ErrReadAfterClose) {
		t.Fatalf("the task read of the drained and closed run = %v, want %v: the close gave the storage view up", err, ledger.ErrReadAfterClose)
	}
	if _, err := run.Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{"user"}},
		Kinds: []query.ObjectKind{query.ObjectTask},
	}); !errors.Is(err, ledger.ErrReadAfterClose) {
		t.Fatalf("the projection read of the drained and closed run = %v, want %v: the read face answers no dimension over a view the close ended", err, ledger.ErrReadAfterClose)
	}

	questions, err := attentionFaceOf(host, book).AttentionQuestions(ctx)
	if err != nil {
		t.Fatalf("the attention read face beside the closed view = %v, want the cleared work end answered beside it", err)
	}
	if len(questions) != 0 {
		t.Fatalf("attention questions beside the closed view = %+v, want none", questions)
	}
}

// bootParkedApprovalHost boots one host over a document whose send call is graded
// critical and parks one call through the approval channel, so the presence stands
// beside a run that owes the drain nothing.
func bootParkedApprovalHost(t *testing.T, id string) (*Host, *approval.ApprovalManager) {
	t.Helper()
	configPath, root, document := governanceDoc(t)
	book := approval.NewApprovalManager(filepath.Join(root, "approvals"), time.Hour, approval.WithApprovalSeat())
	host := bootGovernanceHost(t, configPath, document, WithApprovals(book))
	t.Cleanup(func() { closeLiveness(t, host) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := book.Request(ctx, id, "exec", []byte(`{"cmd":"deploy"}`), plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("park one critical call through the approval channel: %v", err)
	}
	return host, book
}

// closeDrainedWork closes one host and answers the report of that close, failing
// the test unless the close reached a drained quiesce.
func closeDrainedWork(t *testing.T, host *Host) agent.DrainReport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
	report := host.CloseReport()
	if report.Outcome != agent.CloseQuiesced || !report.Drained || len(report.Unsettled) != 0 {
		t.Fatalf("close report = %+v, want the quiesced drain that settled every obligation", report)
	}
	return report
}

// attentionFaceOf wires the attention read face over one booted host the way the
// composition root wires its inventory, so a case reads the face the projection
// reads.
func attentionFaceOf(host *Host, book *approval.ApprovalManager) hostReading {
	return hostReading{runReading: runReading{run: host.Run()}, approvals: book}
}
