// The retroactive-denial anchors of a re-entered task: a send{target:task} that
// already answered the bare ok has committed its input fact, so a failure the
// round meets afterwards may never withdraw that round and file a denied or a
// broken receipt beside the ok it already answered (design D3 of
// 02-message-semantics). The failure belongs to the round itself: the task
// keeps the terminal this round really reached, the body the receipt accepted
// stays owed to the next round, and only a failure from before the input
// committed may still answer a denial and compensate.
//
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
package host

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/query"
	taskpkg "github.com/SpellingDragon/tagent/runtime/task"
)

// retroEntryAgent names the material the no-retroactive-denial rig drives: one
// entry agent holding the communication tool beside the delegation tool, one
// reviewing agent the delegation names, and the bodies the two rounds and the
// re-entry carry.
const (
	retroEntryAgent    = "user"
	retroEntryModel    = "model-entry"
	retroReviewerAgent = "reviewer"
	retroReviewerModel = "model-reviewer"
	retroSession       = "session-reentry-no-retroactive-denial"
	retroToolSend      = "send"
	retroToolSubagent  = "call_subagent"

	// retroCallDelegate is the call identity of the delegation the entry turn
	// opens, retroCallSend the one of the send that re-enters it,
	// retroCallBusy the fresh identity a body sent beside the in-flight round
	// is refused under, and retroCallResend the fresh identity the later send
	// serves the next round under.
	retroCallDelegate = "call-retro-delegate"
	retroCallSend     = "call-retro-send"
	retroCallBusy     = "call-retro-busy"
	retroCallResend   = "call-retro-resend"

	retroRequest     = "retro: review the second draft under way"
	retroFirstRound  = "retro: the first round settled"
	retroSecondRound = "retro: the second round answered the re-entry"
	retroThirdRound  = "retro: the third round answered the later send"
	retroBody        = "retro: the change moved on, review it again"
	retroBusyBody    = "retro: the body refused beside the round in flight"
	retroLaterBody   = "retro: the body the later identity serves"
	retroTurnDone    = "retro: the turn is done"

	// retroReceiptOK is the whole receipt of a re-entered task, and
	// retroDeniedPrefix opens the receipt of a call the host refuses by name.
	retroReceiptOK    = "ok"
	retroDeniedPrefix = "[send_denied] "

	// retroDenialRound and retroDenialRollback are the account stages of the two
	// receipts a retroactive denial files under: a round that landed the body and
	// then failed to record its terminal, and a round the compensation could not
	// put back. Neither may ever answer a body whose input already committed.
	retroDenialRound    = "round"
	retroDenialRollback = "rollback"

	// retroDenialTask is the account stage of an ordinary task-table refusal —
	// the pre-commit denial the negative anchor keeps, and the refusal a repeated
	// identity of an in-flight or a failed task answers beside no round served.
	retroDenialTask = "task"
)

// errRetroRoundRefusal is the refusal the reviewer's second round answers with
// when the check makes the round itself fail after its input landed.
var errRetroRoundRefusal = errors.New("retro: the reviewing round refuses to answer")

// retroWindow bounds how long a check waits for a decision, a receipt or a
// settle fact of the rig.
const retroWindow = 20 * time.Second

// retroGrace is how long a check waits beside a closed round before it reads
// the account back: whatever the settlement path files, it files inside that
// window, so the count a check asserts is the count the closed round leaves.
const retroGrace = 600 * time.Millisecond

// retroAddress is the address the entry turn is driven at, and
// retroReviewerAddress the execution site the delegated rounds run at: the
// sub-site the delegation site mints from the entry session and the delegation
// call identity, so a check reads the reviewing round without waiting on a
// session list to name it.
var (
	retroAddress         = event.Address{AgentID: retroEntryAgent, SessionID: retroSession}
	retroReviewerAddress = event.Address{
		AgentID:   retroReviewerAgent,
		SessionID: event.SessionID(string(retroSession) + "#sub:" + retroCallDelegate),
	}
)

// TestCommittedReentryIsNotRetroactivelyDenied drives one delegated task
// through the formal send{target:task} and pins what a failure after the
// committed input may and may not do (design D3):
//   - positive (committed_input_survives_unrecorded_terminal): a round that
//     landed its input and then could not record its terminal leaves the ok it
//     answered standing, files no denied and no broken receipt, and puts
//     nothing back, so the task keeps the round it really holds and the replay
//     of the same identity copies neither the body nor a second round.
//   - positive (committed_round_failure_keeps_real_terminal): a round that
//     genuinely fails settles the failed terminal of its own round, and the ok
//     it already answered stands beside it.
//   - negative (pre_commit_failure_stays_denied): a body sent before its own
//     input can commit is still answered as a named denial, and the task stays
//     re-enterable for a fresh identity that serves the later body exactly once
//     after the round already in flight settles.
//
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestCommittedReentryIsNotRetroactivelyDenied(t *testing.T) {
	t.Run("committed_input_survives_unrecorded_terminal", retroLegUnrecordedTerminal)
	t.Run("committed_round_failure_keeps_real_terminal", retroLegRoundFailure)
	t.Run("pre_commit_failure_stays_denied", retroLegPreCommitRefusal)
}

// retroRig is the decision script of the no-retroactive-denial rig: the entry
// turn delegates the review once, re-enters the task it read the terminal of
// once, and every reviewing round answers the provider on its own. The second
// reviewing round, the send decision and the body sent beside the in-flight
// round are hookable, so a check holds the exact step it is about to fail.
type retroRig struct {
	provider *scriptedProvider
	mu       sync.Mutex
	issued   map[string]bool

	// secondRound answers the re-entered reviewing round: it may hold the answer
	// until the check releases it, or refuse it so the round itself fails.
	secondRound func() (string, error)
	// sendAnswer renders the decision that issues the send: the plain call, or
	// the same identity twice when a check replays it beside the committed one.
	sendAnswer func(task string) string
	// busySend renders the decision that sends a second body beside the round
	// the first send opened, while that round is still in flight.
	busySend func(task string) string
}

// newRetroRig starts the endpoint the rig boots against.
func newRetroRig(t *testing.T) *retroRig {
	t.Helper()
	rig := &retroRig{provider: newScriptedProvider(t), issued: map[string]bool{}}
	rig.provider.decide = rig.decide
	return rig
}

// claim reports whether the named decision is this request's to make, exactly
// once.
func (r *retroRig) claim(step string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.issued[step] {
		return false
	}
	r.issued[step] = true
	return true
}

// decide answers one request off the material it carried: a reviewing request
// answers its own round, and the entry turn delegates once, hands the body to
// the task whose terminal it read, sends a second body beside the round in
// flight when a check asks it to, re-sends after the terminal it reads, and
// closes every later turn.
func (r *retroRig) decide(request scriptedRequest) (string, error) {
	if request.Model == retroReviewerModel {
		if sendRigCarries(request, retroLaterBody) {
			return sendRigAnswered(retroThirdRound), nil
		}
		if sendRigCarries(request, retroBody) {
			if r.secondRound != nil {
				return r.secondRound()
			}
			return sendRigAnswered(retroSecondRound), nil
		}
		return sendRigAnswered(retroFirstRound), nil
	}
	if r.claim("delegate") {
		return settleToolCallAnswer(retroToolSubagent, retroCallDelegate, `{"name":"`+retroReviewerAgent+
			`","model":"`+retroReviewerModel+`","prompt":"`+retroRequest+`"}`), nil
	}
	if task := sendRigSettledTask(request); task != "" && r.claim("send") {
		if r.sendAnswer != nil {
			return r.sendAnswer(task), nil
		}
		return retroSendAnswer(task, retroCallSend), nil
	}
	if task := retroNoticeTask(request, "failed"); task != "" && r.claim("replay") {
		return retroSendAnswer(task, retroCallSend), nil
	}
	// A check that sends beside the in-flight round waits for the ok the first
	// send answered, so the task is already running when the second body leaves.
	if r.busySend != nil {
		if task := sendRigSettledTask(request); task != "" {
			if receipt, ok := sendRigReceipt(request, retroCallSend); ok && receipt == retroReceiptOK && r.claim("busy") {
				return r.busySend(task), nil
			}
		}
	}
	// The later send waits for the settled terminal of the round the first send
	// opened: its notice carries the round's verdict, so the request proves the
	// terminal landed before the body is handed over again.
	if sendRigCarries(request, retroDeniedPrefix) && sendRigCarries(request, retroSecondRound) && r.claim("resend") {
		if task := sendRigSettledTask(request); task != "" {
			return retroSendAnswerWithBody(task, retroCallResend, retroLaterBody), nil
		}
	}
	return sendRigAnswered(retroTurnDone), nil
}

// retroSendAnswer renders the decision that re-enters one task with the body.
func retroSendAnswer(task, callID string) string {
	return retroSendAnswerWithBody(task, callID, retroBody)
}

// retroSendAnswerWithBody renders the decision that re-enters one task with the
// named body, so a check tells the body a refused identity carried from the one
// a later identity serves.
func retroSendAnswerWithBody(task, callID, body string) string {
	return settleToolCallAnswer(retroToolSend, callID, `{"target":{"task":"`+task+
		`"},"content":"`+body+`"}`)
}

// retroLatch is one arrival signal beside a release the check controls: the
// step a rig holds reports its arrival before it waits, so a check fails a
// precondition exactly between the arrival and the step it releases.
type retroLatch struct {
	arrived chan struct{}
	release chan struct{}
	once    sync.Once
	opened  sync.Once
}

// newRetroLatch returns a latch nothing has arrived at.
func newRetroLatch() *retroLatch {
	return &retroLatch{arrived: make(chan struct{}), release: make(chan struct{})}
}

// hold reports the step's arrival and waits for the check to release it.
func (l *retroLatch) hold() {
	l.once.Do(func() { close(l.arrived) })
	<-l.release
}

// awaitArrived waits for the held step to arrive.
func (l *retroLatch) awaitArrived(t *testing.T) {
	t.Helper()
	select {
	case <-l.arrived:
		return
	case <-time.After(retroWindow):
		t.Fatalf("the held decision of the rig never arrived within %s", retroWindow)
	}
}

// open releases the held step, at most once, so a check that fails beside a
// held step still releases it for cleanup.
func (l *retroLatch) open() { l.opened.Do(func() { close(l.release) }) }

// retroBoot boots the entry document the rig drives: the entry agent beside
// both the communication tool and the delegation tool, beside the reviewing
// agent a delegation names, over the localfile tier whose task-fact shard the
// check holds.
func retroBoot(t *testing.T, rig *retroRig, root string) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	configPath := writeScriptedHostDocument(t, root, rig.provider.endpoint(), retroEntryModel)
	sections := "  " + retroEntryAgent + ":\n" +
		"    model: " + retroEntryModel + "\n" +
		"    tools:\n" +
		"      - { kind: tool, id: " + retroToolSend + " }\n" +
		"      - { kind: tool, id: " + retroToolSubagent + " }\n" +
		"  " + retroReviewerAgent + ":\n" +
		"    model: " + retroReviewerModel + "\n"
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, rig.provider.endpoint(), retroEntryModel, sections))
	host, err := assembly.Boot(configPath, assembly.WithSender(newDeliverySender(nil)))
	if err != nil {
		t.Fatalf("boot the no-retroactive-denial rig: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer closeCancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the no-retroactive-denial host ended its close beside: %v", err)
		}
	})
	return host
}

// retroTaskFactShard is the storage shard directory the durable task facts of
// one localfile tier commit under: every task mutation — the reactivation, the
// settle and the compensating rollback — writes its snapshot here, while the
// session events and the refusal records of the account land in the partition
// shards beside it. Holding this one directory therefore fails exactly the task
// commits of a run whose account stays writable.
func retroTaskFactShard(t *testing.T, root string) string {
	t.Helper()
	const namespace = "tagent/task/v1/fact:"
	shards, err := os.ReadDir(filepath.Join(root, "kv"))
	if err != nil {
		t.Fatalf("the localfile tier %s holds no shard directory: %v", root, err)
	}
	for _, entry := range shards {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, "kv", entry.Name())
		body, err := os.ReadFile(filepath.Join(dir, "snapshot.json"))
		if err != nil {
			continue
		}
		var snapshot struct {
			Entries map[string]json.RawMessage `json:"entries"`
		}
		if err := json.Unmarshal(body, &snapshot); err != nil {
			t.Fatalf("read the shard snapshot %s: %v", dir, err)
		}
		for key := range snapshot.Entries {
			if strings.HasPrefix(key, namespace) {
				return dir
			}
		}
	}
	t.Fatalf("the localfile tier %s holds no task-fact shard: no snapshot carries the %q namespace", root, namespace)
	return ""
}

// retroHoldTaskFacts makes the task-fact shard refuse further writes, and
// restores it with the check.
func retroHoldTaskFacts(t *testing.T, shard string) {
	t.Helper()
	if err := os.Chmod(shard, 0o500); err != nil {
		t.Fatalf("hold the task-fact shard %s: %v", shard, err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(shard, 0o755); err != nil {
			t.Logf("restore the task-fact shard %s: %v", shard, err)
		}
	})
}

// retroReceiptsOf reports every result the model read beside one call, in the
// order the transcript committed them: a receipt the runtime answers twice —
// the committed delivery beside the refusal of a repeated identity — reads
// back as two, so a check tells the ok that already stood from what a replay
// was answered with.
func retroReceiptsOf(rig *retroRig, callID string) []string {
	var out []string
	for _, request := range rig.provider.servedRequests() {
		for index, message := range request.Messages {
			if message.ToolCallID != callID || message.Role != "tool" {
				continue
			}
			for _, later := range request.Messages[index+1:] {
				if later.Role == "user" {
					out = append(out, later.Content)
					break
				}
			}
		}
	}
	return out
}

// retroAwaitReceipt waits for the first result the model read beside one call
// and answers it, failing the check when the window closes first.
func retroAwaitReceipt(t *testing.T, rig *retroRig, callID string) string {
	t.Helper()
	deadline := time.Now().Add(retroWindow)
	for {
		if receipts := retroReceiptsOf(rig, callID); len(receipts) > 0 {
			return receipts[0]
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("no decision of the rig read the result of %q within %s", callID, retroWindow)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// retroAwaitLastReceipt waits for a further result the model read beside one
// call and answers the newest of them, so a check reads what a repeated
// identity was answered with rather than the receipt that already stood.
func retroAwaitLastReceipt(t *testing.T, rig *retroRig, callID string) string {
	t.Helper()
	first := retroAwaitReceipt(t, rig, callID)
	deadline := time.Now().Add(retroWindow)
	for {
		receipts := retroReceiptsOf(rig, callID)
		if len(receipts) > 1 {
			return receipts[len(receipts)-1]
		}
		if !time.Now().Before(deadline) {
			return first
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// retroReviewerBodyRequests reports the reviewing requests that carried the
// re-entry body, so a check tells the one round that read it from a copy a
// replay would have made.
func retroReviewerBodyRequests(rig *retroRig) []scriptedRequest {
	return retroReviewerBodyRequestsFor(rig, retroBody)
}

// retroReviewerBodyRequestsFor reports the reviewing requests that carried one
// body, so a check counts the rounds one body served apart from another body a
// refused or later identity carried.
func retroReviewerBodyRequestsFor(rig *retroRig, body string) []scriptedRequest {
	var matched []scriptedRequest
	for _, request := range rig.provider.servedRequests() {
		if request.Model == retroReviewerModel && sendRigCarries(request, body) {
			matched = append(matched, request)
		}
	}
	return matched
}

// retroAwaitReviewerBody waits for the first reviewing round that carries the
// re-entry body, so a check reads the round the receipt answered for.
func retroAwaitReviewerBody(t *testing.T, rig *retroRig) {
	t.Helper()
	retroAwaitReviewerBodyFor(t, rig, retroBody)
}

// retroAwaitReviewerBodyFor waits for the first reviewing round that carries the
// named body, failing the check when the window closes first.
func retroAwaitReviewerBodyFor(t *testing.T, rig *retroRig, body string) {
	t.Helper()
	deadline := time.Now().Add(retroWindow)
	for {
		if len(retroReviewerBodyRequestsFor(rig, body)) > 0 {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("no reviewing round carried %q within %s", body, retroWindow)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// retroRefusalsOf reads the attempts the account filed against one task, so a
// check tells the receipts the run answered retroactively from the refusals a
// later attempt asked for by name.
func retroRefusalsOf(host *assembly.Host, taskID event.TaskID) []assembly.DeliveryRefusal {
	var filed []assembly.DeliveryRefusal
	for _, refusal := range host.Run().DeliveryLedger().Refusals() {
		if !strings.HasSuffix(refusal.Identity, "\x00"+string(taskID)) {
			continue
		}
		filed = append(filed, refusal)
	}
	return filed
}

// retroRetroactiveRefusals reports the filed attempts that answer a body whose
// input already committed: a denied receipt under the round stage, and every
// broken receipt, name a withdrawal or a compensation of a round the ok
// already stood for, and neither may exist after the input fact is committed.
func retroRetroactiveRefusals(host *assembly.Host, taskID event.TaskID) []assembly.DeliveryRefusal {
	var retroactive []assembly.DeliveryRefusal
	for _, refusal := range retroRefusalsOf(host, taskID) {
		if refusal.Category == retroDenialRound || refusal.Category == retroDenialRollback ||
			refusal.Receipt == assembly.DeliveryReceiptBroken {
			retroactive = append(retroactive, refusal)
		}
	}
	return retroactive
}

// retroNoticeTask reports the task one terminal notice of the named kind the
// request carried belongs to, and the empty identity beside no such notice.
func retroNoticeTask(request scriptedRequest, kind string) string {
	for _, message := range request.Messages {
		if message.Role != "user" {
			continue
		}
		if task := retroTaskOfNoticeMaterial(message.Content, kind); task != "" {
			return task
		}
	}
	return ""
}

// retroTaskOfNoticeMaterial reads the task one terminal notice material of the
// named kind names: the material a settle turn reads opens with
// "tagent: task <id> <kind>", so the identity is read off the text the runtime
// committed rather than minted by the check.
func retroTaskOfNoticeMaterial(material, kind string) string {
	const opening = "tagent: task "
	at := strings.Index(material, opening)
	for at >= 0 {
		rest := material[at+len(opening):]
		if line := strings.IndexAny(rest, " \n"); line > 0 {
			if strings.HasPrefix(strings.TrimLeft(rest[line:], " \n"), kind) {
				return rest[:line]
			}
		}
		next := strings.Index(rest[1:], opening)
		if next < 0 {
			return ""
		}
		at += len(opening) + 1 + next
	}
	return ""
}

// retroNoticeInvocation names the invocation one terminal notification of one
// round of one kind drives under: task, round, kind and sequence, the
// four-part identity the runtime mints.
func retroNoticeInvocation(taskID event.TaskID, round int64, kind string) event.InvocationID {
	return event.InvocationID("notice:" + string(taskID) + ":" + strconv.FormatInt(round, 10) + ":" + kind + ":0")
}

// retroAwaitNotice waits for the settle notice one round of one task is
// reported under and answers the material it carries, failing the check when
// the window closes first.
func retroAwaitNotice(t *testing.T, host *assembly.Host, taskID event.TaskID, round int64, kind string) string {
	t.Helper()
	invocation := retroNoticeInvocation(taskID, round, kind)
	deadline := time.Now().Add(retroWindow)
	for {
		if material, ok := sendRigNoticeMaterials(host, retroAddress, taskID)[invocation]; ok {
			return material
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the round %d of task %s was reported under no %s notice of its own within %s; notices = %+v",
				round, taskID, kind, retroWindow, sendRigNoticeMaterials(host, retroAddress, taskID))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// retroAwaitRoundClosed waits until the re-entered reviewing round closed: the
// verdict it produced is read back from the reviewing session the round ran
// at, and whatever the settlement path files beside that closure files inside
// the grace that follows. A retroactive receipt that files earlier ends the
// wait the moment it appears.
func retroAwaitRoundClosed(t *testing.T, host *assembly.Host, taskID event.TaskID) {
	t.Helper()
	deadline := time.Now().Add(retroWindow)
	for {
		if len(retroRetroactiveRefusals(host, taskID)) > 0 {
			time.Sleep(100 * time.Millisecond)
			return
		}
		if retroReviewerCarries(host, retroSecondRound) {
			time.Sleep(retroGrace)
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the re-entered reviewing round never closed within %s", retroWindow)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// retroReviewerCarries reports whether the reviewing session the delegated
// rounds ran at already holds the verdict the re-entered round produced.
func retroReviewerCarries(host *assembly.Host, verdict string) bool {
	for _, fact := range host.Run().SessionFacts(retroReviewerAddress) {
		if strings.Contains(fact.Message.Content, verdict) {
			return true
		}
	}
	for _, message := range host.Run().SessionHistory(retroReviewerAddress) {
		if strings.Contains(message.Content, verdict) {
			return true
		}
	}
	return false
}

// retroDelegationBookings reads how many delegation terminals the run holds
// still booked: one for the round the re-entry reopened, and none once that
// reopen was withdrawn. The read is the diagnostics face's own reconciliation
// reading, so the check reads the public account instead of a copy of it.
func retroDelegationBookings(t *testing.T, host *assembly.Host) int {
	t.Helper()
	scope := query.AuthorizationScope{
		Agents:   []event.AgentID{retroEntryAgent, retroReviewerAgent},
		Sessions: []event.Address{retroAddress, retroReviewerAddress},
	}
	report, err := host.Run().Diagnostics(context.Background(), query.ListFilter{Scope: scope}, assembly.DiagnosticSwitches{})
	if err != nil {
		t.Fatalf("read the diagnostics of the run: %v", err)
	}
	return report.Lifecycle.Reconciliation.Booked
}

// retroTaskStatus reports the status the task table holds for one task, read
// through the public unsettled work criterion so the check reads the state the
// machine really holds instead of a copy.
func retroTaskStatus(host *assembly.Host, taskID event.TaskID) taskpkg.Status {
	for _, work := range host.Run().Unsettled(context.Background()).Work {
		if work.TaskID == taskID {
			return work.Status
		}
	}
	return taskpkg.StatusUnknown
}

// retroLegUnrecordedTerminal holds the re-entered round after its input landed,
// fails the commit of its terminal, and requires the ok the send already
// answered to stand: no denial, no broken receipt, no withdrawal, and no copy
// of the body a replay of the same identity could have made.
func retroLegUnrecordedTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	root := t.TempDir()
	rig := newRetroRig(t)
	round2 := newRetroLatch()
	rig.secondRound = func() (string, error) {
		round2.hold()
		return sendRigAnswered(retroSecondRound), nil
	}
	// The send decision replays the same identity beside the committed one, so
	// the leg reads what a repeated delivery of an in-flight round answers.
	rig.sendAnswer = func(task string) string {
		call := wiringCall{id: retroCallSend, name: retroToolSend,
			arguments: `{"target":{"task":"` + task + `"},"content":"` + retroBody + `"}`}
		return wiringToolCallsAnswer(call, call)
	}
	host := retroBoot(t, rig, root)
	t.Cleanup(round2.open)

	if _, err := driveIntegrityTurn(ctx, host, retroAddress, retroRequest); err != nil {
		t.Fatalf("the turn that re-enters the task it delegated ended by error: %v", err)
	}
	if receipt := retroAwaitReceipt(t, rig, retroCallSend); receipt != retroReceiptOK {
		t.Fatalf("the send receipt = %q, want the bare %q of a re-entered task", receipt, retroReceiptOK)
	}
	task := sendRigSettledTaskID(host, retroAddress)
	if task == "" {
		t.Fatalf("the session chain holds no settled notification of the delegated round")
	}
	retroAwaitReviewerBody(t, rig)
	round2.awaitArrived(t)

	// The input fact is committed and the receipt answered ok; the round is held
	// at its verdict. Failing the task-fact commit now fails exactly the settle
	// the closed round owes. The checks read the public task and delegation state
	// while the shard is held, so what they assert is the settle path's own
	// behavior rather than a receipt the failing account could not have filed.
	shard := retroTaskFactShard(t, root)
	retroHoldTaskFacts(t, shard)
	round2.open()
	retroAwaitRoundClosed(t, host, task)

	if status := retroTaskStatus(host, task); status != taskpkg.StatusRunning {
		t.Fatalf("the task after the unrecorded terminal = %q, want the running round the ok answered for: a failure after the input committed may not put the task back", status)
	}
	if booked := retroDelegationBookings(t, host); booked != 1 {
		t.Fatalf("delegation terminals booked after the unrecorded terminal = %d, want the one round the ok answered for: the re-entry withdrew the reopen of a round whose input had already committed", booked)
	}
	if filed := retroRetroactiveRefusals(host, task); len(filed) > 0 {
		t.Fatalf("the account answers the committed re-entry of %s retroactively: %+v; the ok it already answered may not be denied after the input committed", task, filed)
	}
	if receipts := retroReceiptsOf(rig, retroCallSend); len(receipts) == 0 || receipts[0] != retroReceiptOK {
		t.Fatalf("the receipts of the send = %q, want the ok it already answered to stand first", receipts)
	}
	if rounds := retroReviewerBodyRequests(rig); len(rounds) != 1 {
		t.Fatalf("reviewing rounds carrying the body = %d, want the one round the receipt answered for", len(rounds))
	}
}

// retroLegRoundFailure makes the re-entered round genuinely fail after its
// input landed, and requires the task to settle the failed terminal of its own
// round: the ok stands beside it, no denial is filed, and the replay of the
// same identity is refused by name without copying the body or opening a round.
func retroLegRoundFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	root := t.TempDir()
	rig := newRetroRig(t)
	round2 := newRetroLatch()
	rig.secondRound = func() (string, error) {
		round2.hold()
		return "", errRetroRoundRefusal
	}
	host := retroBoot(t, rig, root)
	t.Cleanup(round2.open)

	if _, err := driveIntegrityTurn(ctx, host, retroAddress, retroRequest); err != nil {
		t.Fatalf("the turn that re-enters the task it delegated ended by error: %v", err)
	}
	if receipt := retroAwaitReceipt(t, rig, retroCallSend); receipt != retroReceiptOK {
		t.Fatalf("the send receipt = %q, want the bare %q of a re-entered task", receipt, retroReceiptOK)
	}
	task := sendRigSettledTaskID(host, retroAddress)
	if task == "" {
		t.Fatalf("the session chain holds no settled notification of the delegated round")
	}
	retroAwaitReviewerBody(t, rig)
	round2.awaitArrived(t)
	round2.open()

	material := retroAwaitNotice(t, host, task, 2, "failed")
	if !strings.Contains(material, errRetroRoundRefusal.Error()) {
		t.Fatalf("the notice of the failed round = %q, want the refusal the round answered with", material)
	}
	time.Sleep(retroGrace)

	if filed := retroRetroactiveRefusals(host, task); len(filed) > 0 {
		t.Fatalf("the account answers the failed round of %s retroactively: %+v; a genuine failure is the terminal of its own round and never a denial of the input", task, filed)
	}
	refused := retroAwaitLastReceipt(t, rig, retroCallSend)
	if !strings.HasPrefix(refused, retroDeniedPrefix) {
		t.Fatalf("the replayed identity of the failed task = %q, want the named refusal of the terminal it holds", refused)
	}
	if !strings.Contains(refused, "the task is failed") {
		t.Fatalf("the refusal of the failed task = %q, want it to name the failed terminal by name", refused)
	}
	if rounds := retroReviewerBodyRequests(rig); len(rounds) != 1 {
		t.Fatalf("reviewing rounds carrying the body = %d, want the one round the receipt answered for", len(rounds))
	}
}

// retroLegPreCommitRefusal sends a second body at a task whose first re-entry
// is still in flight, so the refusal happens before the second body's input can
// commit. It requires the named denial such a pre-commit failure answers with:
// no round was opened for the second body, the body reached no reviewing round,
// the round already in flight settles as its own round, and the task stays
// re-enterable for a fresh identity that serves the later body exactly once.
func retroLegPreCommitRefusal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	root := t.TempDir()
	rig := newRetroRig(t)
	round2 := newRetroLatch()
	rig.secondRound = func() (string, error) {
		round2.hold()
		return sendRigAnswered(retroSecondRound), nil
	}
	rig.busySend = func(task string) string {
		return retroSendAnswerWithBody(task, retroCallBusy, retroBusyBody)
	}
	host := retroBoot(t, rig, root)
	t.Cleanup(round2.open)

	if _, err := driveIntegrityTurn(ctx, host, retroAddress, retroRequest); err != nil {
		t.Fatalf("the turn that re-enters the task it delegated ended by error: %v", err)
	}
	if receipt := retroAwaitReceipt(t, rig, retroCallSend); receipt != retroReceiptOK {
		t.Fatalf("the send receipt = %q, want the bare %q of a re-entered task", receipt, retroReceiptOK)
	}
	task := sendRigSettledTaskID(host, retroAddress)
	if task == "" {
		t.Fatalf("the session chain holds no settled notification of the delegated round")
	}
	retroAwaitReviewerBody(t, rig)
	round2.awaitArrived(t)

	// The first re-entry is in flight and held at its verdict. The second body
	// arrives under a fresh identity, so the task table refuses it before any
	// round of it is opened and its input never commits.
	refused := retroAwaitReceipt(t, rig, retroCallBusy)
	if !strings.HasPrefix(refused, retroDeniedPrefix) {
		t.Fatalf("the receipt of the body sent beside the round in flight = %q, want the named denial of the task table", refused)
	}
	if !strings.Contains(refused, "in flight") {
		t.Fatalf("the refusal of the body sent beside the round in flight = %q, want it to name the round that owns the task", refused)
	}
	time.Sleep(retroGrace)
	filed := retroRefusalsOf(host, task)
	if len(filed) == 0 {
		t.Fatalf("the account files no refusal of %s, so the pre-commit denial lives only in the text the model read", task)
	}
	for _, refusal := range filed {
		if refusal.Receipt != assembly.DeliveryReceiptDenied || refusal.Category != retroDenialTask {
			t.Fatalf("the pre-commit refusal of %s = %+v, want the task stage that refused the identity", task, refusal)
		}
	}
	if rounds := retroReviewerBodyRequestsFor(rig, retroBusyBody); len(rounds) != 0 {
		t.Fatalf("reviewing rounds carrying the refused body = %d, want none beside an input that never committed", len(rounds))
	}

	// The round the first send opened was never touched by the refusal: it
	// settles its own terminal, and the task is settled again for a later send.
	round2.open()
	material := retroAwaitNotice(t, host, task, 2, "settled")
	if !strings.Contains(material, retroSecondRound) {
		t.Fatalf("the notice of the re-entered round = %q, want the verdict the round produced", material)
	}

	// The refusal compensated nothing it had moved, so the task stays
	// re-enterable: a fresh identity serves the later body exactly once.
	if receipt := retroAwaitReceipt(t, rig, retroCallResend); receipt != retroReceiptOK {
		t.Fatalf("the later send after the terminal = %q, want the bare %q of a task the refusal left re-enterable", receipt, retroReceiptOK)
	}
	retroAwaitReviewerBodyFor(t, rig, retroLaterBody)
	material = retroAwaitNotice(t, host, task, 3, "settled")
	if !strings.Contains(material, retroThirdRound) {
		t.Fatalf("the notice of the third round = %q, want the verdict the later send served", material)
	}
	if rounds := retroReviewerBodyRequestsFor(rig, retroLaterBody); len(rounds) != 1 {
		t.Fatalf("reviewing rounds carrying the later body = %d, want the one the later send served", len(rounds))
	}
}
