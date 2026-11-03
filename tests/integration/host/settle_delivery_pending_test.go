// The settle-delivery pending check of the host: the commit of the settle fact
// one terminal notification delivers participates in the acceptance that
// delivery reports. A commit the storage path refuses leaves the notification
// pending — its delivery obligation standing beside the named refusal, no settle
// fact landed and no follow-up turn run — and once the refusal is lifted, the
// offer a close makes re-writes the fact exactly once and the notification
// drives the one follow-up turn it owes.
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
package host

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// settlePendingSession names the one session the settle-pending check drives,
// and settlePendingAgentSections renders the surface it drives under: the entry
// agent beside the delegation tool, and the reviewing agent a delegation names.
const settlePendingSession = "session-settle-pending"

const settlePendingAgentSections = "  user:\n" +
	"    model: model-entry\n" +
	"    max_tokens: 32000\n" +
	"    tools:\n" +
	"      - { kind: tool, id: call_subagent }\n" +
	"  reviewer:\n" +
	"    model: model-reviewer\n"

// settlePendingMarker is the phrase the delivery-layer reason of a notification
// whose settle fact the storage path refused carries, so the owed account names
// the refused commit instead of the plain wait of an unconsumed mailbox entry.
const settlePendingMarker = "did not commit"

// settlePendingQuiet is the beat the check waits past a settle delivery before
// it reads the failed window, so a drive that the refusal should have kept from
// opening has room to open inside it and be counted.
const settlePendingQuiet = time.Second

// TestSettleDeliveryCommitFailureStaysPending pins the settle fact commit as part of the acceptance a delivery reports.
//   - positive (failed window): a settle delivery whose fact commit the storage path refuses reports Pending — the notification stands owed beside the named refusal, no settle fact lands and no follow-up turn runs
//   - negative (recovery): once the refusal is lifted, the offer the close makes re-writes the settle fact exactly once and the follow-up turn the notification owes answers exactly once
//   - consumer: the settle rides the formal host assembly — the delegated round of an admission the entry turn already ended, the unattended settle delivery and the settle driver path — and the refusal is injected through the storage tier the run commits its facts under, never by calling the delivery internals
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestSettleDeliveryCommitFailureStaysPending(t *testing.T) {
	wakeEnv(t)

	const (
		reviewerBody   = "the delegated reviewer reports the total is 7"
		entryDoneBody  = "the entry turn ended on its own"
		followUpAnswer = "the settle reached the user after the recovery"
		delegateCall   = "call-settle-pending-delegate"
	)

	root := t.TempDir()
	provider := newScriptedProvider(t)
	reviewerSeen := make(chan struct{})
	releaseReviewer := make(chan struct{})
	var reviewerOnce sync.Once
	var mu sync.Mutex
	entryCalls, followUps := 0, 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-reviewer" {
			reviewerOnce.Do(func() { close(reviewerSeen) })
			<-releaseReviewer
			if err := settlePendingBlockShard(root); err != nil {
				return "", fmt.Errorf("block the entry partition snapshot: %w", err)
			}
			return scriptedAnswerOf(reviewerBody), nil
		}
		mu.Lock()
		defer mu.Unlock()
		if settleDriveReadsMaterial(request, reviewerBody) {
			followUps++
			return scriptedAnswerOf(followUpAnswer), nil
		}
		entryCalls++
		if entryCalls == 1 {
			return settleToolCallAnswer("call_subagent", delegateCall,
				`{"name":"reviewer","model":"model-reviewer","prompt":"review the note under way"}`), nil
		}
		return scriptedAnswerOf(entryDoneBody), nil
	}

	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", settlePendingAgentSections))
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the settle-pending host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: settlePendingSession}

	// The entry turn delegates once, reads the admission it was answered with and
	// ends on its own answer, so the settle of the delegated round arrives beside
	// no live invocation of it: the unattended settle whose delivery fact only
	// the delivery itself commits.
	invoked := make(chan error, 1)
	go func() {
		_, err := host.Invoke(ctx, assembly.InvokeRequest{
			Address: addr,
			Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the review to the reviewer"}},
		})
		invoked <- err
	}()
	select {
	case <-reviewerSeen:
	case <-time.After(30 * time.Second):
		t.Fatal("the delegated round never reached its provider")
	}
	waitForSettle(t, "the entry turn to end on its own answer", func() bool {
		return settleDriveOutputsOf(host.Run().SessionFacts(addr), entryDoneBody) > 0
	})
	if err := <-invoked; err != nil {
		t.Fatalf("the entry turn ended by error: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	close(releaseReviewer)

	// The settle delivery reached its target mailbox: the notification stands in
	// the owed account the host itself reads.
	waitForSettle(t, "the settle notice to stand in the owed account", func() bool {
		return settlePendingNotice(t, host, "") != nil
	})
	time.Sleep(settlePendingQuiet)

	// The failed window: the delivery may not report acceptance over a fact the
	// storage tier refused. The notification keeps its delivery obligation with
	// the refusal named beside it, the settle fact landed nowhere and no
	// follow-up turn opened.
	notice := settlePendingNotice(t, host, "")
	if notice == nil {
		t.Fatal("the settle notice left the owed account beside a refused fact commit")
	}
	if !strings.Contains(notice.Reason, settlePendingMarker) {
		t.Fatalf("the owed settle notice of %s carries the reason %q, want the refused commit named beside the pending delivery: a delivery whose fact %s may not report acceptance",
			notice.Notice.TaskID, notice.Reason, settlePendingMarker)
	}
	if got := settlePendingFacts(host, addr, notice.Notice.TaskID); got != 0 {
		t.Fatalf("settle facts of %s in the failed window = %d, want none: an uncommitted fact may not appear as a product of the delivery", notice.Notice.TaskID, got)
	}
	mu.Lock()
	fired := followUps
	mu.Unlock()
	if fired != 0 {
		t.Fatalf("follow-up turns in the failed window = %d, want none beside a settle fact no storage holds", fired)
	}

	// The recovery: the refusal lifted, the offer the close makes delivers the
	// retained obligation again, so the settle fact is re-written exactly once
	// and the follow-up turn the notification owes answers exactly once.
	if err := settlePendingUnblockShard(root); err != nil {
		t.Fatalf("lift the refusal off the entry partition snapshot: %v", err)
	}
	if err := host.Close(ctx); err != nil {
		t.Logf("the recovered close answered: %v", err)
	}
	waitForSettle(t, "the recovered settle to drive its follow-up turn", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return followUps >= 1
	})
	time.Sleep(settlePendingQuiet)
	mu.Lock()
	fired = followUps
	mu.Unlock()
	if fired != 1 {
		t.Fatalf("follow-up turns after the recovery = %d, want exactly the one the notification owes", fired)
	}
	if got := settlePendingFacts(host, addr, notice.Notice.TaskID); got != 1 {
		t.Fatalf("settle facts of %s after the recovery = %d, want the one re-written delivery of the retained obligation", notice.Notice.TaskID, got)
	}
	if got := deliveredCopies(sender.bodies(), followUpAnswer); got != 1 {
		t.Fatalf("delivered copies of the follow-up answer after the recovery = %d, want exactly one", got)
	}
	t.Logf("settle-pending evidence: task=%s reason=%q settle_facts_after_recovery=1 follow_ups=1", notice.Notice.TaskID, notice.Reason)
}

// settlePendingNotice reports the settled notification of the owed account the
// host reads, beside the reason it stands unconsumed, or nil beside none. The
// task identity narrows the read when it is not empty.
func settlePendingNotice(t *testing.T, host *assembly.Host, taskID event.TaskID) *task.UnsettledNotice {
	t.Helper()
	read := host.Run().Unsettled(context.Background())
	for at := range read.Notices {
		entry := read.Notices[at]
		if entry.Notice.Kind != task.NoticeSettled {
			continue
		}
		if taskID != "" && entry.Notice.TaskID != taskID {
			continue
		}
		return &entry
	}
	return nil
}

// settlePendingFacts counts the settle facts one session chain holds for one
// task: the delivery facts a terminal notification commits under its own
// identity, present exactly as often as their commit really landed.
func settlePendingFacts(host *assembly.Host, addr event.Address, taskID event.TaskID) int {
	count := 0
	for _, fact := range host.Run().SessionFacts(addr) {
		if fact.Type != event.TypeTaskSettled || fact.TaskID != taskID {
			continue
		}
		if fact.Meta[event.MetaKeyNoticeKind] != string(task.NoticeSettled) {
			continue
		}
		count++
	}
	return count
}

// settlePendingShardDir names the snapshot directory of the partition the entry
// agent commits its facts under. The settle fact of a notification delivered to
// the entry address lands in that one shard, so a snapshot write the directory
// refuses refuses exactly that family of commits and nothing else the flow
// commits — the delegated round's facts live in the reviewer's own partition
// and the task account in its own namespaces.
func settlePendingShardDir(root string) string {
	return filepath.Join(root, "kv", strconv.Itoa(memory.PartitionIDFromName("user")))
}

// settlePendingBlockShard replaces the entry partition's snapshot directory
// with one plain file, so the barrier of the next commit into that partition
// fails to take its directory back and the commit is refused with the cause the
// storage path reports. Nothing outside that partition notices the block.
func settlePendingBlockShard(root string) error {
	dir := settlePendingShardDir(root)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	if err := os.WriteFile(dir, []byte("blocked by the settle-pending check"), 0o644); err != nil {
		return fmt.Errorf("block %s: %w", dir, err)
	}
	return nil
}

// settlePendingUnblockShard puts the entry partition's snapshot directory back,
// so the next commit into the partition writes its snapshot again and the
// refusal the block injected is lifted for the recovery to observe.
func settlePendingUnblockShard(root string) error {
	dir := settlePendingShardDir(root)
	if err := os.Remove(dir); err != nil {
		return fmt.Errorf("unblock %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("restore %s: %w", dir, err)
	}
	return nil
}
