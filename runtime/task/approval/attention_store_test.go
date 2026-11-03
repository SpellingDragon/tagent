// The approval channel read from its attention side: one test per truth the
// manager derives from the chain instead of from an index of its own, so a
// restart, a ruling and a wait all read back what landed.
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// testApprovalApprover is the name the rulings in these anchors are rendered
// under, so a request reads back the approver that answered it.
const testApprovalApprover = "the-test-approver"

// TestApprovalManagerReadsAttention pins presence and ruling as the attention facts.
// - a registration files one approval question, and registering the same arguments again reads that identity back
// - the waiting face reports exactly the questions the attention face still holds, and none once one is answered
// - a repeated registration widens the alias of one request, never the waiting set: presence is a set of questions, so one approval identity is one entry
// - the status a lookup reports is the answer its question carries, never a state kept beside the chain
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestApprovalManagerReadsAttention(t *testing.T) {
	ctx := context.Background()
	manager := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	args := []byte(`{"cmd":"deploy"}`)
	first, err := manager.Request(ctx, "approval-reads-attention", "shell_exec", args,
		plugin.RiskCritical, "rule-critical", "the deploy call is critical")
	if err != nil {
		t.Fatalf("register the critical call: %v", err)
	}
	if first.Status != ApprovalPending {
		t.Fatalf("registered request = %s, want the wait the registration opens", first.Status)
	}
	question := questionKeyOf("shell_exec", ArgsDigest(args), first.ID)
	if !holdsFact(t, manager, question) {
		t.Fatalf("the chain holds no question for %s: a registration is one attention question", first.ID)
	}
	if got := manager.Pending(); len(got) != 1 || got[0].ID != first.ID {
		t.Fatalf("waiting face = %+v, want the one question the chain holds as a question", got)
	}

	repeat, err := manager.Request(ctx, "approval-reads-attention-again", "shell_exec", args,
		plugin.RiskCritical, "rule-critical", "the deploy call is critical")
	if err != nil {
		t.Fatalf("register the same arguments again: %v", err)
	}
	if repeat.ID != first.ID {
		t.Fatalf("repeated registration = %s, want the standing request %s read back", repeat.ID, first.ID)
	}
	if filed := questionKeysOf(t, manager); len(filed) != 1 {
		t.Fatalf("approval questions on the chain = %d, want one for the one standing wait", len(filed))
	}
	if got := manager.Pending(); len(got) != 1 || got[0].ID != first.ID {
		t.Fatalf("waiting face after the repeated registration = %+v, want the one standing question %s reported once", got, first.ID)
	}

	if _, err := manager.Decide(first.ID, ApprovalApproved, testApprovalApprover); err != nil {
		t.Fatalf("approve the held call: %v", err)
	}
	if got := manager.Pending(); len(got) != 0 {
		t.Fatalf("waiting face after the ruling = %+v, want the answered question gone", got)
	}
	present, err := manager.face.Present(ctx, task.AttentionFilter{Kind: task.AttentionKindApproval})
	if err != nil {
		t.Fatalf("read the attention presence: %v", err)
	}
	if len(present) != 0 {
		t.Fatalf("attention presence after the ruling = %+v, want none", present)
	}
	if !holdsFact(t, manager, answerKeyOf("shell_exec", ArgsDigest(args), first.ID)) {
		t.Fatalf("the chain holds no answer for %s: a ruling is one attention answer", first.ID)
	}
	decided := manager.Lookup("shell_exec", ArgsDigest(args))
	if decided == nil || decided.Status != ApprovalApproved || decided.DecidedBy != testApprovalApprover {
		t.Fatalf("lookup = %+v, want the ruling the answer carries", decided)
	}
}

// TestApprovalDigestChangeStartsNewQuestion pins the digest binding as one byte-exact lineage match.
// - an approval over one digest is not read by a call carrying other arguments
// - the changed arguments raise their own question, and the approved answer of the held arguments never closes it
// - a ruling that arrives after the validity ran out is refused by name, so an expired request never turns runnable
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestApprovalDigestChangeStartsNewQuestion(t *testing.T) {
	ctx := context.Background()
	nowMs := time.Now().UnixMilli()
	manager := NewApprovalManager("", time.Minute,
		WithApprovalClock(func() time.Time { return time.UnixMilli(nowMs) }),
		WithApprovalRescanInterval(0))
	held := []byte(`{"cmd":"deploy"}`)
	changed := []byte(`{"cmd":"rm -rf /"}`)
	first, err := manager.Request(ctx, "approval-held-arguments", "shell_exec", held,
		plugin.RiskCritical, "rule-critical", "the deploy call is critical")
	if err != nil {
		t.Fatalf("register the held arguments: %v", err)
	}
	if _, err := manager.Decide(first.ID, ApprovalApproved, testApprovalApprover); err != nil {
		t.Fatalf("approve the held arguments: %v", err)
	}
	if seen := manager.Lookup("shell_exec", ArgsDigest(changed)); seen != nil {
		t.Fatalf("changed-argument lookup = %+v, want no approval over arguments that were never held", seen)
	}

	second, err := manager.Request(ctx, "approval-changed-arguments", "shell_exec", changed,
		plugin.RiskCritical, "rule-critical", "the deploy call is critical")
	if err != nil {
		t.Fatalf("register the changed arguments: %v", err)
	}
	if second.ID == first.ID || second.ArgsDigest != ArgsDigest(changed) || second.Status != ApprovalPending {
		t.Fatalf("changed-argument request = %+v, want a fresh wait bound to its own digest", second)
	}
	if got := manager.Pending(); len(got) != 1 || got[0].ID != second.ID {
		t.Fatalf("waiting face = %+v, want the changed call held beside the answered one", got)
	}
	heldLineage := lineageOf(t, manager, event.TypeAttentionQuestion, "shell_exec", ArgsDigest(held), first.ID)
	heldAnswerLineage := lineageOf(t, manager, event.TypeAttentionAnswer, "shell_exec", ArgsDigest(held), first.ID)
	if heldLineage != heldAnswerLineage {
		t.Fatalf("answer lineage %q = question lineage %q, want the two byte for byte", heldAnswerLineage, heldLineage)
	}
	changedLineage := lineageOf(t, manager, event.TypeAttentionQuestion, "shell_exec", ArgsDigest(changed), second.ID)
	if changedLineage == heldLineage {
		t.Fatalf("changed-argument lineage %q = held lineage, want the digest to tell them apart", changedLineage)
	}

	nowMs += int64(2 * time.Minute / time.Millisecond)
	if seen := manager.Lookup("shell_exec", ArgsDigest(changed)); seen == nil || seen.Status != ApprovalExpired {
		t.Fatalf("lookup past the validity = %+v, want the request read as expired", seen)
	}
	if _, err := manager.Decide(second.ID, ApprovalApproved, "the-late-approver"); !errors.Is(err, ErrApprovalExpired) {
		t.Fatalf("late ruling error = %v, want the expired request refused by name", err)
	}
	if got := manager.Pending(); len(got) != 0 {
		t.Fatalf("waiting face past the validity = %+v, want no request awaiting a ruling", got)
	}
	if seen := manager.Lookup("shell_exec", ArgsDigest(changed)); seen == nil || seen.Status == ApprovalApproved {
		t.Fatalf("lookup after the refused ruling = %+v, want the request never made runnable", seen)
	}
}

// TestExternalFileRulingLandsAsAnswer pins the request file as the external write entry.
// - a ruling written into the request file lands as the attention answer its question waits for
// - the file is withdrawn once the ruling landed, so a settled approval leaves no residue behind
// - the file is never read as presence: a pending body does not resurrect an answered question
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestExternalFileRulingLandsAsAnswer(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	manager := NewApprovalManager(dir, time.Hour, WithApprovalRescanInterval(0))
	args := []byte(`{"cmd":"deploy"}`)
	req, err := manager.Request(ctx, "approval-external-ruling", "shell_exec", args,
		plugin.RiskCritical, "rule-critical", "the deploy call is critical")
	if err != nil {
		t.Fatalf("register the critical call: %v", err)
	}
	path := filepath.Join(dir, "approvals", req.ID+".json")
	ruling := readApprovalFile(t, path)
	ruling.Status = ApprovalApproved
	ruling.DecidedBy = "the-file-approver"
	writeApprovalFile(t, path, ruling)

	seen := manager.Lookup("shell_exec", ArgsDigest(args))
	if seen == nil || seen.Status != ApprovalApproved || seen.DecidedBy != "the-file-approver" {
		t.Fatalf("lookup after the file ruling = %+v, want the approved ruling the file stated", seen)
	}
	if !holdsFact(t, manager, answerKeyOf("shell_exec", ArgsDigest(args), req.ID)) {
		t.Fatalf("the chain holds no answer for %s: the file ruling lands as an attention answer", req.ID)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the request file of the settled approval still stands (%v), want it withdrawn once the answer landed", err)
	}
	if got := manager.Pending(); len(got) != 0 {
		t.Fatalf("waiting face after the file ruling = %+v, want the answered question gone", got)
	}

	pending := seen
	pending.Status = ApprovalPending
	pending.DecidedBy = ""
	writeApprovalFile(t, path, *pending)
	if got := manager.Pending(); len(got) != 0 {
		t.Fatalf("waiting face beside a pending request file = %+v, want the file never read as presence", got)
	}
	if drawn := manager.Lookup("shell_exec", ArgsDigest(args)); drawn == nil || drawn.Status != ApprovalApproved {
		t.Fatalf("lookup beside a pending request file = %+v, want the answer the chain holds", drawn)
	}
}

// questionKeyOf answers the fact key one approval question is filed under.
func questionKeyOf(tool, argsDigest, id string) event.EventID {
	return event.EventID(approvalFact(event.TypeAttentionQuestion, tool, argsDigest, id).FactKey())
}

// answerKeyOf answers the fact key one approval answer is filed under.
func answerKeyOf(tool, argsDigest, id string) event.EventID {
	return event.EventID(approvalFact(event.TypeAttentionAnswer, tool, argsDigest, id).FactKey())
}

// holdsFact reports whether the manager's chain holds the fact one identity filed.
func holdsFact(t *testing.T, manager *ApprovalManager, key event.EventID) bool {
	t.Helper()
	for _, fact := range chainFactsOf(t, manager) {
		if fact.Key == key {
			return true
		}
	}
	return false
}

// questionKeysOf answers the fact keys of the approval questions the chain holds.
func questionKeysOf(t *testing.T, manager *ApprovalManager) []event.EventID {
	t.Helper()
	keys := make([]event.EventID, 0, 2)
	for _, fact := range chainFactsOf(t, manager) {
		if fact.Type == event.TypeAttentionQuestion {
			keys = append(keys, fact.Key)
		}
	}
	return keys
}

// lineageOf answers the lineage one role of one identity was filed under, read
// off the chain instead of recomputed, so a test states what landed.
func lineageOf(t *testing.T, manager *ApprovalManager, role, tool, argsDigest, id string) string {
	t.Helper()
	key := questionKeyOf(tool, argsDigest, id)
	if role == event.TypeAttentionAnswer {
		key = answerKeyOf(tool, argsDigest, id)
	}
	for _, fact := range chainFactsOf(t, manager) {
		if fact.Key == key {
			return fact.Meta[event.MetaKeyAttentionLineage]
		}
	}
	t.Fatalf("the chain holds no %s fact for %s", role, id)
	return ""
}

// chainFactsOf answers every attention fact the manager's chain holds, so a test
// reads the truth the manager derives its answers from.
func chainFactsOf(t *testing.T, manager *ApprovalManager) []event.Event {
	t.Helper()
	facts, err := manager.chain.AttentionFacts(context.Background())
	if err != nil {
		t.Fatalf("read the attention chain: %v", err)
	}
	return facts
}

// writeApprovalFile writes one request into its file as an external approver
// does, so a ruling reaches the manager through the face an approver owns.
func writeApprovalFile(t *testing.T, path string, req ApprovalRequest) {
	t.Helper()
	raw, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		t.Fatalf("encode the request file %s: %v", path, err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write the request file %s: %v", path, err)
	}
}
