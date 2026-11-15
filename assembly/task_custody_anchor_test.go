// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package assembly

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestBootSeatsTheMailboxAfterTheDurableAccount pins the order one boot states the durable account in.
//   - the account is seated before the slices are restored, so a notification that landed past the checkpoint's cut is seated by the mailbox of the same boot
//   - a boot that restored the slices first would wait for a checkpoint that never states that notification
//   - the fixture files a checkpoint, then commits one task record whose outbox the stopped process never delivered
//   - the record is past the cut, so the tasks slice does not cover it and the boot must read the chain that landed after it
//   - that record is the increment a boot reads by identity: the run that wrote it stopped before any later checkpoint could cover it
//   - positive: the booted mailbox seats that notification, routed to the address the record froze, and the durable load reached it by the identity of its task
//   - negative of the order: an account seated after the mailbox restore seats nothing there, which is the work item a restart silently loses
//
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func TestBootSeatsTheMailboxAfterTheDurableAccount(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	addr := deliveryTestAddress("session-seat-after-cut")
	source := noTrimChainRun(t, ctx, storage)
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, "seat", 2)
	if _, _, err := source.Checkpoint(ctx); err != nil {
		t.Fatalf("file the checkpoint of the account: %v", err)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	owedAddr := event.Address{AgentID: "agent-a", SessionID: "session-seat-after-cut-tail"}
	owedTask := event.TaskID("task-seat-after-cut")
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the storage tier beside the cut: %v", err)
	}
	account := state.NewEmptyDurableCommit(ledger.TierFactLog(tier), nil)
	if err := account.Seat(ctx, nil, nil); err != nil {
		t.Fatalf("seat the account of the storage: %v", err)
	}
	if _, err := account.Commit(ctx, task.CommitRecord{
		MutationID: "seat-after-cut:0",
		TaskID:     owedTask,
		Status:     task.StatusRunning,
		Outbox: []task.Envelope{{
			Notice: task.Notice{
				TaskID: owedTask, Round: 1, Kind: task.NoticeProgress, Seq: 1,
				Event: "fact-seat-after-cut", Text: "the progress the stopped process never delivered",
			},
			Target:         owedAddr,
			Source:         owedAddr,
			TargetInstance: "instance-seat-after-cut",
		}},
	}); err != nil {
		t.Fatalf("commit the owed notification past the cut: %v", err)
	}

	booted := noTrimChainRun(t, ctx, storage)
	booted.stopCheckpointFiler()
	if key, _ := booted.CheckpointFiled(); key == "" {
		t.Fatal("the boot read no checkpoint, want the cut this account is restored beside")
	}
	seats := booted.store.Unconsumed()
	held := false
	for _, seat := range seats {
		if seat.Notice.TaskID == owedTask {
			held = true
			if seat.Address != owedAddr {
				t.Fatalf("the restored seat routes to %v, want the frozen routing %v", seat.Address, owedAddr)
			}
		}
	}
	if !held {
		t.Fatalf("the booted mailbox seats %+v, want the notification that landed past the cut", seats)
	}
	scanned := false
	for _, scan := range booted.commit.inner.(*state.DurableCommit).LoadStats().Scans {
		if scan.Prefix == "tagent/task/v1/fact:tagent/" {
			t.Fatal("the boot scanned the fact family of the whole history, want the tasks slice beside the chain that landed after it: the account was seated from that slice, so no prefix of the whole history costs a key")
		}
		if strings.Contains(scan.Prefix, string(owedTask)) {
			scanned = true
		}
	}
	if !scanned {
		t.Fatal("the boot read no key of the task that landed past the cut, want its chain read by identity")
	}
}
