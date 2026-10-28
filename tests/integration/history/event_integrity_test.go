// TestEventIntegrityConsumptionRecovery drives the consumption bookkeeping of
// the mailbox beside the durable material a task chain carries, over the real
// localfile tier and one real process boundary:
// - claim_then_commit_retry_recovers: a claim survives an uncertain commit, and the retry leaves the obligation owed and restorable
// - consume_only_after_durable_commit: no acceptance and no consumption precede the committed outbox fact
// - handover_before_confirmation: a stop inside the handover leaves the settled notification owed and its result recoverable
// - independent_process_restart: an abruptly interrupted process leaves the unconsumed notification complete across the restart
// - claim_does_not_confirm_early: a claim is not a completion, before or after the interruption
// - no_skip_over_gap: the consumption position never crosses an unconfirmed sequence
// - consumed_entry_not_reprocessed: a consumed notification is not offered to consumption again after the restart
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
package history

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/tool/recall"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// consumptionChildModeEnv, consumptionChildRootEnv, consumptionChildOutEnv and consumptionChildCfgEnv name the parent-to-child handshake; consumptionModeSeed, consumptionModeRecover and consumptionModeHandover name the phases.
const (
	consumptionChildModeEnv = "TAGENT_EVENT_CONSUMPTION_CHILD_MODE"
	consumptionChildRootEnv = "TAGENT_EVENT_CONSUMPTION_CHILD_ROOT"
	consumptionChildOutEnv  = "TAGENT_EVENT_CONSUMPTION_CHILD_OUT"
	consumptionChildCfgEnv  = "TAGENT_EVENT_CONSUMPTION_CHILD_CONFIG"

	consumptionModeSeed     = "seed"
	consumptionModeRecover  = "recover"
	consumptionModeHandover = "handover"
)

// consumptionSeedAddress, consumptionClaimAddress and consumptionClaimInst are the one address and instance the restart anchors seed and recover.
var (
	consumptionSeedAddress  = event.Address{AgentID: "agent-a", SessionID: "session-seed"}
	consumptionClaimAddress = event.Address{AgentID: "agent-a", SessionID: "session-claimed"}
	consumptionClaimInst    = event.InstanceID("inst-claimed")
)

// TestEventIntegrityConsumptionRecovery drives the consumption anchors over the real durable tier:
// - the mailbox keeps its three time points apart
// - the task chain keeps the notification obligation
// - a real process boundary decides what a restart still holds
func TestEventIntegrityConsumptionRecovery(t *testing.T) {
	if mode := os.Getenv(consumptionChildModeEnv); mode != "" {
		runConsumptionChild(t, mode)
		return
	}
	t.Run("claim_then_commit_retry_recovers", runClaimThenCommitRetry)
	t.Run("consume_only_after_durable_commit", runConsumeOnlyAfterDurableCommit)
	t.Run("handover_before_confirmation", runHandoverBeforeConfirmation)
	t.Run("independent_process_restart", runIndependentProcessRestart)
	t.Run("claim_does_not_confirm_early", runClaimDoesNotConfirmEarly)
	t.Run("no_skip_over_gap", runNoSkipOverGap)
	t.Run("consumed_entry_not_reprocessed", runConsumedEntryNotReprocessed)
}

// runClaimThenCommitRetry proves a claim is kept apart from the commit that
// makes the notification owed: the uncertain attempt stores no fact and owes
// no delivery, the retry stores the fact and leaves the obligation owed, and a
// reopen rebuilds that obligation from the chain alone before the claim
// settles.
func runClaimThenCommitRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
	spec := consumptionSpec("task-retry")
	notice := task.Notice{TaskID: "", Round: 1, Kind: task.NoticeSettled, Seq: 0, Event: "evt-settle-retry"}
	uncertain := true
	probe := func(record task.CommitRecord) bool {
		if !uncertain || !strings.HasPrefix(string(record.MutationID), "settle:") {
			return false
		}
		uncertain = false
		return true
	}
	tier, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("open the durable tier: %v", err)
	}
	durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), probe)
	if err != nil {
		t.Fatalf("build the durable commit: %v", err)
	}
	registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
	receipt, err := registry.Register(ctx, spec)
	if err != nil {
		t.Fatalf("register the task: %v", err)
	}
	notice.TaskID = receipt.TaskID
	settle := task.CommitRecord{
		MutationID:       task.MutationID("settle:" + string(receipt.TaskID)),
		TaskID:           receipt.TaskID,
		ExpectedRevision: 1,
		Status:           task.StatusSettled,
		Outbox:           []task.Envelope{consumptionEnvelope(spec, notice)},
	}

	site := newConsumptionSite(t, spec.Target, spec.InstanceID)
	site.accept(t, notice)
	seq := site.seqOf(t, notice)
	if err := site.store.Claim(ctx, spec.Target, spec.InstanceID, seq); err != nil {
		t.Fatalf("claim the accepted notification: %v", err)
	}
	if entry, ok := site.entry(t, seq); !ok || entry.Claim != state.ClaimClaimed || entry.Receipt.Completed {
		t.Fatalf("the claimed entry = %+v (found=%v), want a claim beside no completion", entry, ok)
	}

	result, err := durable.Commit(ctx, settle)
	if err != nil || result != task.CommitUncertain {
		t.Fatalf("the first commit = %v, %v, want the uncertain outcome the storage reported", result, err)
	}
	if facts := durable.Facts(receipt.TaskID); len(facts) != 1 {
		t.Fatalf("facts after the uncertain commit = %d, want the admission link alone", len(facts))
	}
	if owed := durable.Outbox(receipt.TaskID); len(owed) != 0 {
		t.Fatalf("outbox after the uncertain commit = %+v, want no delivery owed by an uncommitted fact", owed)
	}
	if revision := durable.Revision(receipt.TaskID); revision != 1 {
		t.Fatalf("revision after the uncertain commit = %d, want the admission revision", revision)
	}
	if session := site.session(t); session.Consumed != 0 {
		t.Fatalf("consumption position after the uncertain commit = %d, want no movement from a claim", session.Consumed)
	}
	if entry, ok := site.entry(t, seq); !ok || entry.Claim != state.ClaimClaimed || entry.Receipt.Completed {
		t.Fatalf("the entry after the uncertain commit = %+v (found=%v), want the claim kept beside no completion", entry, ok)
	}

	result, err = durable.Commit(ctx, settle)
	if err != nil || result != task.CommitCommitted {
		t.Fatalf("the retried commit = %v, %v, want the committed outcome", result, err)
	}
	if facts := durable.Facts(receipt.TaskID); len(facts) != 2 {
		t.Fatalf("facts after the retried commit = %d, want the admission and settle links", len(facts))
	}
	owed := durable.Outbox(receipt.TaskID)
	if len(owed) != 1 || owed[0].Delivery != task.DeliveryPending || owed[0].Notice.Notice != notice {
		t.Fatalf("outbox after the retried commit = %+v, want %s owed and pending", owed, notice.FactKey())
	}

	reopenedTier, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen the durable tier: %v", err)
	}
	reopened, err := state.NewDurableCommit(ledger.TierFactLog(reopenedTier), nil)
	if err != nil {
		t.Fatalf("rebuild the durable commit: %v", err)
	}
	chain, err := reopened.Records(ctx, receipt.TaskID)
	if err != nil {
		t.Fatalf("read the rebuilt chain: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("rebuilt chain = %d links, want the admission and settle links", len(chain))
	}
	restored := task.NewMemoryRegistry(reopened, task.NewMemoryAdmission())
	report, err := restored.Restore(ctx, chain, consumptionOwners(t, spec))
	if err != nil {
		t.Fatalf("restore the task: %v", err)
	}
	if len(report.Tasks) != 1 || !report.Tasks[0].Recovered || report.Tasks[0].Refused != nil {
		t.Fatalf("restore report = %+v, want the task rebuilt without a refusal", report.Tasks)
	}
	manager := task.NewMemoryManager(restored, nil)
	obligations, err := manager.Obligations(ctx, receipt.TaskID)
	if err != nil {
		t.Fatalf("read the restored obligations: %v", err)
	}
	if !consumptionCarriesDelivery(obligations, notice) {
		t.Fatalf("restored obligations = %+v, want the %s delivery obligation kept", obligations.Open, notice.FactKey())
	}

	if err := site.store.Consume(ctx, spec.Target, spec.InstanceID, seq); err != nil {
		t.Fatalf("consume the claimed notification beside the committed fact: %v", err)
	}
	session := site.session(t)
	if session.Consumed != seq {
		t.Fatalf("consumption position = %d, want %d beside the one consumed entry", session.Consumed, seq)
	}
	if entry, ok := site.entry(t, seq); !ok || !entry.Receipt.Completed {
		t.Fatalf("the consumed entry = %+v (found=%v), want its completion receipt recorded", entry, ok)
	}
	t.Logf("claim_then_commit_retry: task=%s notice=%s position=%d", receipt.TaskID, notice.FactKey(), session.Consumed)
}

// runConsumeOnlyAfterDurableCommit proves the ordering the delivery path keeps:
// a notification no committed record owes is refused by name, the commit writes
// its record behind the sync barrier, a freshly opened tier reads that record,
// and only then does the mailbox accept, claim and consume the notification.
func runConsumeOnlyAfterDurableCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
	tier, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("open the durable tier: %v", err)
	}
	recording := &consumptionBarrierLog{FactLog: ledger.TierFactLog(tier)}
	durable, err := state.NewDurableCommit(recording, nil)
	if err != nil {
		t.Fatalf("build the durable commit: %v", err)
	}
	spec := consumptionSpec("task-durable")
	registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
	receipt, err := registry.Register(ctx, spec)
	if err != nil {
		t.Fatalf("register the task: %v", err)
	}
	notice := task.Notice{TaskID: receipt.TaskID, Round: 1, Kind: task.NoticeSettled, Seq: 0, Event: "evt-settle-durable"}
	settle := task.CommitRecord{
		MutationID:       task.MutationID("settle:" + string(receipt.TaskID)),
		TaskID:           receipt.TaskID,
		ExpectedRevision: 1,
		Status:           task.StatusSettled,
		Outbox:           []task.Envelope{consumptionEnvelope(spec, notice)},
	}

	if _, err := durable.Accept(ctx, notice); !errors.Is(err, state.ErrNoticeNotOwed) {
		t.Fatalf("acceptance before the commit = %v, want the named ErrNoticeNotOwed refusal", err)
	}
	site := newConsumptionSite(t, spec.Target, spec.InstanceID)
	if err := site.store.Claim(ctx, spec.Target, spec.InstanceID, 1); !errors.Is(err, state.ErrEntryUnknown) {
		t.Fatalf("claim of a sequence no acceptance named = %v, want the named ErrEntryUnknown refusal", err)
	}

	syncsBefore := recording.barriers()
	result, err := durable.Commit(ctx, settle)
	if err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the settle fact = %v, %v, want committed", result, err)
	}
	put, barrier, syncs := recording.order()
	if syncs <= syncsBefore || barrier < put {
		t.Fatalf("barrier order = put@%d sync@%d (barriers %d before %d), want the record staged before the barrier it reports durable behind", put, barrier, syncsBefore, syncs)
	}

	reopenedTier, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen the durable tier: %v", err)
	}
	reopened, err := state.NewDurableCommit(ledger.TierFactLog(reopenedTier), nil)
	if err != nil {
		t.Fatalf("rebuild the durable commit: %v", err)
	}
	chain, err := reopened.Records(ctx, receipt.TaskID)
	if err != nil || len(chain) != 2 {
		t.Fatalf("rebuilt chain = %d links, %v, want the two committed records read back", len(chain), err)
	}

	if accepted, err := durable.Accept(ctx, notice); err != nil || accepted != task.DeliveryAccepted {
		t.Fatalf("acceptance beside the committed record = %v, %v, want the accepted delivery", accepted, err)
	}
	site.accept(t, notice)
	seq := site.seqOf(t, notice)
	if err := site.store.Consume(ctx, spec.Target, spec.InstanceID, seq); !errors.Is(err, state.ErrNotClaimed) {
		t.Fatalf("consumption of an unclaimed entry = %v, want the named ErrNotClaimed refusal", err)
	}
	if session := site.session(t); session.Consumed != 0 {
		t.Fatalf("consumption position after the refused consumption = %d, want no confirmation", session.Consumed)
	}
	if err := site.store.Claim(ctx, spec.Target, spec.InstanceID, seq); err != nil {
		t.Fatalf("claim the accepted notification: %v", err)
	}
	if err := site.store.Consume(ctx, spec.Target, spec.InstanceID, seq); err != nil {
		t.Fatalf("consume the claimed notification: %v", err)
	}
	if session := site.session(t); session.Consumed != seq {
		t.Fatalf("consumption position = %d, want %d after the ordered accept, claim and consume", session.Consumed, seq)
	}
	t.Logf("consume_only_after_durable_commit: task=%s notice=%s put@%d sync@%d", receipt.TaskID, notice.FactKey(), put, barrier)
}

// runHandoverBeforeConfirmation interrupts a real tool turn inside the handover
// of its settled result: the delivery fact is committed, the notification is
// booked and claimed, and the process exits before the intake reads it. The
// notification MUST still be owed after the restart, so the settled result is
// handed over again; a confirmation recorded before the handover starves the
// outbox of that notification and loses the result, which this anchor reports
// as the failure.
func runHandoverBeforeConfirmation(t *testing.T) {
	root := t.TempDir()
	seed := interruptConsumptionChild(t, consumptionModeHandover, root)
	if len(seed.Notices) != 1 {
		t.Fatalf("the seed stands inside the handover and reads notices = %+v, want the one settled notification of the tool it ran", seed.Notices)
	}
	stopped := seed.Notices[0]
	t.Logf("seed: pid=%d notice=%s phase=%s event=%s", seed.PID, stopped.FactKey(), stopped.Phase, stopped.Event)

	recovered := runConsumptionRecover(t, root)
	outcome, ok := consumptionRecoveryOf(recovered, stopped.TaskID)
	if !ok || !outcome.Recovered {
		t.Fatalf("recovery report = %+v, want the task %s rebuilt from the chain the interrupted handover left", recovered.Recovery, stopped.TaskID)
	}
	view, offered := consumptionNoticeOf(recovered, stopped)
	if !offered {
		t.Fatalf("the settled result of %s is not recoverable after the restart: the interrupted handover left no owed notification, so nothing hands the result over again (the seed stood in phase %q inside the handover, and a confirmation recorded before the handover starves the outbox of that notification)", stopped.FactKey(), stopped.Phase)
	}
	if view.Phase != string(query.NoticePhaseOwed) {
		t.Fatalf("the notification %s stands in phase %q after the restart, want it owed so the interrupted handover is replayed", view.FactKey(), view.Phase)
	}
	if view.Event != stopped.Event || view.TargetAgent != stopped.TargetAgent ||
		view.TargetSession != stopped.TargetSession || view.TargetInstance != stopped.TargetInstance {
		t.Fatalf("restarted notification = %+v, want the event and frozen routing of %+v kept complete", view, stopped)
	}

	if stopped.Phase == string(query.NoticePhaseConsumed) {
		t.Fatalf("the notification %s reads consumed while the seed stands inside the handover: its consumption was confirmed before the settled result was handed over", stopped.FactKey())
	}
	if stopped.Phase != string(query.NoticePhaseClaimed) {
		t.Fatalf("the handover window of %s reads phase %q, want the claimed entry whose consumption is not confirmed yet", stopped.FactKey(), stopped.Phase)
	}
	t.Logf("restart: pid=%d rebuilt=%d notice=%s phase=%s", recovered.PID, len(recovered.Recovery), view.FactKey(), view.Phase)
}

// runIndependentProcessRestart interrupts a seeded process after it reported a
// consumed notification and a claimed one, then boots a second process over the
// same durable root: both tasks are rebuilt, the claimed notification keeps its
// complete identity, event and frozen routing, and it is still owed.
func runIndependentProcessRestart(t *testing.T) {
	root := t.TempDir()
	seed := runConsumptionSeed(t, root)
	consumed := consumptionNoticeInPhase(t, seed, query.NoticePhaseConsumed)
	claimed := consumptionClaimedEntry(t, seed)
	if claimed.Completed || claimed.Claim != "claimed" {
		t.Fatalf("the seeded mailbox entry = %+v, want the claimed notification left unconsumed", claimed)
	}
	if len(seed.Mailbox) != 1 || seed.Mailbox[0].Consumed != 0 {
		t.Fatalf("seeded mailbox = %+v, want one site whose claim moved no consumption position", seed.Mailbox)
	}
	t.Logf("seed: pid=%d consumed=%s claimed=%s position=%d", seed.PID, consumed.FactKey(), claimed.FactKey(), seed.Mailbox[0].Consumed)

	recovered := runConsumptionRecover(t, root)
	for _, id := range []string{consumed.TaskID, claimed.TaskID} {
		outcome, ok := consumptionRecoveryOf(recovered, id)
		if !ok {
			t.Fatalf("recovery report = %+v, want the task %s rebuilt across the restart", recovered.Recovery, id)
		}
		if !outcome.Recovered || outcome.Refused != "" {
			t.Fatalf("recovery outcome of %s = %+v, want it rebuilt without a refusal", id, outcome)
		}
	}
	view, ok := consumptionNoticeOf(recovered, claimed.consumptionNoticeView)
	if !ok {
		t.Fatalf("restarted notices = %+v, want the claimed notification %s still visible", recovered.Notices, claimed.FactKey())
	}
	if view.Phase != string(query.NoticePhaseOwed) {
		t.Fatalf("the claimed notification stands in phase %q after the restart, want it still owed to consumption", view.Phase)
	}
	if view.Blocked != 0 {
		t.Fatalf("the claimed notification carries %d blocked obligations after the restart, want the claim to degrade nothing", view.Blocked)
	}
	if view.Event != claimed.Event || view.TargetAgent != claimed.TargetAgent ||
		view.TargetSession != claimed.TargetSession || view.TargetInstance != claimed.TargetInstance {
		t.Fatalf("restarted notification = %+v, want the event and frozen routing of %+v kept complete", view, claimed.consumptionNoticeView)
	}
	t.Logf("restart: pid=%d rebuilt=%d notices=%d owed=%s notes=%v", recovered.PID, len(recovered.Recovery), len(recovered.Notices), view.FactKey(), recovered.Notes)
}

// runClaimDoesNotConfirmEarly proves a claim never turns into a completion: the
// seeded mailbox holds the entry claimed with no receipt and the consumption
// position before it, and the restarted process still reports the notification
// as owed instead of consumed.
func runClaimDoesNotConfirmEarly(t *testing.T) {
	root := t.TempDir()
	seed := runConsumptionSeed(t, root)
	claimed := consumptionClaimedEntry(t, seed)
	if claimed.Completed {
		t.Fatalf("the seeded entry %s carries a completion receipt, want a claim that confirmed nothing", claimed.FactKey())
	}
	if claimed.Claim != "claimed" {
		t.Fatalf("the seeded entry %s stands in claim %q, want it still claimed and unconsumed", claimed.FactKey(), claimed.Claim)
	}
	if len(seed.Mailbox) != 1 || seed.Mailbox[0].Consumed != 0 {
		t.Fatalf("seeded mailbox = %+v, want a claim that moved no consumption position", seed.Mailbox)
	}
	t.Logf("seed: claimed=%s completed=%v position=%d", claimed.FactKey(), claimed.Completed, seed.Mailbox[0].Consumed)

	recovered := runConsumptionRecover(t, root)
	view, ok := consumptionNoticeOf(recovered, claimed.consumptionNoticeView)
	if !ok {
		t.Fatalf("restarted notices = %+v, want the claimed notification %s kept for consumption", recovered.Notices, claimed.FactKey())
	}
	if view.Phase == string(query.NoticePhaseConsumed) {
		t.Fatalf("the claimed notification %s reads consumed after the restart; a claim MUST NOT become a completion", view.FactKey())
	}
	if view.Phase != string(query.NoticePhaseOwed) {
		t.Fatalf("the claimed notification stands in phase %q after the restart, want it owed and waiting for consumption", view.Phase)
	}
	t.Logf("restart: notice=%s phase=%s", view.FactKey(), view.Phase)
}

// runNoSkipOverGap proves the consumption position never crosses a sequence
// left unconfirmed: a completion recorded beside a hole keeps the position
// before the hole, filling the hole advances the position over both, and the
// position never moves back or skips the entry still unconfirmed.
func runNoSkipOverGap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-gap"}
	instance := event.InstanceID("inst-gap")
	site := newConsumptionSite(t, addr, instance)
	taskID := event.TaskID("task-gap")
	notices := make([]task.Notice, 0, 3)
	for seq := int64(0); seq < 3; seq++ {
		notice := task.Notice{TaskID: taskID, Round: 1, Kind: task.NoticeSettled, Seq: seq, Event: event.EventID(fmt.Sprintf("evt-gap-%d", seq))}
		notices = append(notices, notice)
		site.accept(t, notice)
		accepted := site.seqOf(t, notice)
		if err := site.store.Claim(ctx, addr, instance, accepted); err != nil {
			t.Fatalf("claim %s: %v", notice.FactKey(), err)
		}
	}
	sequences := make([]state.AcceptSeq, 0, len(notices))
	for _, notice := range notices {
		sequences = append(sequences, site.seqOf(t, notice))
	}

	if err := site.store.Consume(ctx, addr, instance, sequences[1]); err != nil {
		t.Fatalf("consume %s beside the unconfirmed hole: %v", notices[1].FactKey(), err)
	}
	if session := site.session(t); session.Consumed != 0 {
		t.Fatalf("consumption position = %d after %s completed beside an unconfirmed %s, want the position kept before the hole",
			session.Consumed, notices[1].FactKey(), notices[0].FactKey())
	}
	if entry, ok := site.entry(t, sequences[0]); !ok || entry.Claim != state.ClaimClaimed {
		t.Fatalf("the entry before the hole = %+v (found=%v), want it still claimed and unconfirmed", entry, ok)
	}

	if err := site.store.Consume(ctx, addr, instance, sequences[0]); err != nil {
		t.Fatalf("consume the hole at %s: %v", notices[0].FactKey(), err)
	}
	if session := site.session(t); session.Consumed != sequences[1] {
		t.Fatalf("consumption position = %d after the hole filled, want %d covering the contiguous completions", session.Consumed, sequences[1])
	}
	position := site.session(t).Consumed

	if err := site.store.Consume(ctx, addr, instance, sequences[2]); err != nil {
		t.Fatalf("consume %s: %v", notices[2].FactKey(), err)
	}
	session := site.session(t)
	if session.Consumed != sequences[2] || session.Consumed < position {
		t.Fatalf("consumption position = %d after %s, want %d and never behind %d", session.Consumed, notices[2].FactKey(), sequences[2], position)
	}
	t.Logf("no_skip_over_gap: sequences=%v position=%d", sequences, session.Consumed)
}

// runConsumedEntryNotReprocessed proves a notification whose consumption
// completed before the interruption is not offered to consumption again after
// the restart.
func runConsumedEntryNotReprocessed(t *testing.T) {
	root := t.TempDir()
	seed := runConsumptionSeed(t, root)
	consumed := consumptionNoticeInPhase(t, seed, query.NoticePhaseConsumed)
	t.Logf("seed: consumed=%s phase=%s", consumed.FactKey(), consumed.Phase)

	recovered := runConsumptionRecover(t, root)
	view, offered := consumptionNoticeOf(recovered, consumed)
	if !offered {
		t.Logf("restart: the consumed notification %s is not offered again", consumed.FactKey())
		return
	}
	t.Fatalf("the notification %s completed its consumption before the interruption (phase %q, completed receipt) but the restarted process offers it again as %q: the durable outbox re-owes every committed notification because no durable material records the consumption, and 00.3 froze public_api_or_format_change=none, so recording the receipt needs a ruling",
		consumed.FactKey(), consumed.Phase, view.Phase)
}

// consumptionSpec builds one serialized task declaration whose target is the
// tool the durable chain resolves the owner against.
func consumptionSpec(callID string) task.Spec {
	session := event.SessionID("s-" + callID)
	return task.Spec{
		CallID:       callID,
		DedupKey:     task.DedupKey("inst-" + callID + "\x00inv-" + callID + "\x00dec-" + callID + "\x00" + callID),
		InvocationID: event.InvocationID("inv-" + callID),
		InstanceID:   event.InstanceID("inst-" + callID),
		Description:  "tool-c",
		Source:       event.Address{AgentID: "agent-b", SessionID: session},
		Target:       event.Address{AgentID: "tool-c", SessionID: session},
		Execution:    "serialized",
	}
}

// consumptionEnvelope freezes the routing one notification carries beside the
// stable identity of the notification itself.
func consumptionEnvelope(spec task.Spec, notice task.Notice) task.Envelope {
	return task.Envelope{
		Notice:         notice,
		Target:         spec.Target,
		Source:         spec.Source,
		TargetInstance: spec.InstanceID,
	}
}

// consumptionOwners resolves the owner of the task's target over the binding
// the current tool set holds.
func consumptionOwners(t *testing.T, spec task.Spec) *state.BindingOwners {
	t.Helper()
	bindings := state.NewBindings()
	if err := bindings.Hold(state.StatePlan{Agents: map[event.AgentID]state.AgentBinding{
		spec.Source.AgentID: {Tools: []trpctool.Tool{stubTool{name: string(spec.Target.AgentID)}}},
	}}); err != nil {
		t.Fatalf("hold the owner binding: %v", err)
	}
	return state.NewBindingOwners(bindings)
}

// consumptionCarriesDelivery reports whether the open obligations still carry
// the delivery of the named notification.
func consumptionCarriesDelivery(obligations task.Obligations, notice task.Notice) bool {
	for _, open := range obligations.Open {
		if open.Kind == task.ObligationAwaitingDelivery && open.Notice == notice {
			return true
		}
	}
	return false
}

// consumptionSite is one established mailbox site the checks read and write
// through the state owner's own faces.
type consumptionSite struct {
	addr      event.Address
	instance  event.InstanceID
	authority *state.Authority
	subject   state.Subject
	store     *state.MemoryStore
}

// newConsumptionSite establishes one site beside the read grant its own
// bookkeeping is read through.
func newConsumptionSite(t *testing.T, addr event.Address, instance event.InstanceID) *consumptionSite {
	t.Helper()
	ctx := context.Background()
	authority := state.NewAuthority()
	subject := state.Subject("event-integrity-consumption")
	if err := authority.GrantRead(ctx, subject, addr); err != nil {
		t.Fatalf("grant the mailbox read: %v", err)
	}
	store := state.NewMemoryStore(authority)
	if _, err := store.Establish(ctx, addr, instance); err != nil {
		t.Fatalf("establish the mailbox site: %v", err)
	}
	return &consumptionSite{addr: addr, instance: instance, authority: authority, subject: subject, store: store}
}

// accept routes and accepts one notification into the site's mailbox.
func (s *consumptionSite) accept(t *testing.T, notice task.Notice) {
	t.Helper()
	ctx := context.Background()
	if err := s.store.TrackDelivery(ctx, notice.TaskID, task.HandoffTarget{Target: s.addr, TargetInstance: s.instance}); err != nil {
		t.Fatalf("track the delivery of %s: %v", notice.FactKey(), err)
	}
	if err := s.store.AcceptNotice(ctx, notice); err != nil {
		t.Fatalf("accept %s: %v", notice.FactKey(), err)
	}
}

// session reads the site's mailbox bookkeeping.
func (s *consumptionSite) session(t *testing.T) *state.Session {
	t.Helper()
	session, err := s.store.Site(context.Background(), s.subject, s.addr, s.instance)
	if err != nil {
		t.Fatalf("read the mailbox site: %v", err)
	}
	return session
}

// seqOf reports the acceptance sequence the site assigned the notification.
func (s *consumptionSite) seqOf(t *testing.T, notice task.Notice) state.AcceptSeq {
	t.Helper()
	for _, entry := range s.session(t).Mailbox {
		if entry.Notice == notice {
			return entry.AcceptSeq
		}
	}
	t.Fatalf("the mailbox holds no entry for %s", notice.FactKey())
	return 0
}

// entry reads one mailbox entry by its acceptance sequence.
func (s *consumptionSite) entry(t *testing.T, seq state.AcceptSeq) (state.MailboxEntry, bool) {
	t.Helper()
	for _, entry := range s.session(t).Mailbox {
		if entry.AcceptSeq == seq {
			return entry, true
		}
	}
	return state.MailboxEntry{}, false
}

// consumptionBarrierLog records the write and barrier order of the real tier
// log it delegates to: the record it stages, the barrier behind which the
// carrier reports it durable, and how many barriers returned.
type consumptionBarrierLog struct {
	state.FactLog
	mu       sync.Mutex
	step     int
	lastPut  int
	lastSync int
	synced   int
}

// Put stages the record beside the step it took.
func (l *consumptionBarrierLog) Put(ctx context.Context, key string, value []byte) error {
	l.mu.Lock()
	l.step++
	l.lastPut = l.step
	l.mu.Unlock()
	return l.FactLog.Put(ctx, key, value)
}

// Sync completes the barrier beside the step it took.
func (l *consumptionBarrierLog) Sync(ctx context.Context) error {
	l.mu.Lock()
	l.step++
	l.lastSync = l.step
	l.synced++
	l.mu.Unlock()
	return l.FactLog.Sync(ctx)
}

// order reports the last staged record, the last barrier and the barriers that
// returned so far.
func (l *consumptionBarrierLog) order() (int, int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastPut, l.lastSync, l.synced
}

// barriers reports how many write barriers returned so far.
func (l *consumptionBarrierLog) barriers() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.synced
}

// consumptionChildReport is the child-process report of the restart anchors: the recovery outcomes, the task and notification readings and the mailbox time points.
type consumptionChildReport struct {
	Ready    bool                      `json:"ready"`
	Mode     string                    `json:"mode"`
	PID      int                       `json:"pid"`
	Recovery []consumptionRecoveryView `json:"recovery"`
	Tasks    []consumptionTaskView     `json:"tasks"`
	Notices  []consumptionNoticeView   `json:"notices"`
	Mailbox  []consumptionMailboxView  `json:"mailbox"`
	Notes    []string                  `json:"notes,omitempty"`
}

// consumptionRecoveryView is one restore outcome as the child folded it.
type consumptionRecoveryView struct {
	TaskID    string `json:"task_id"`
	Owner     string `json:"owner"`
	Status    int    `json:"status"`
	Recovered bool   `json:"recovered"`
	Refused   string `json:"refused,omitempty"`
}

// consumptionTaskView is one task reading of the diagnostic face.
type consumptionTaskView struct {
	TaskID string `json:"task_id"`
	Status int    `json:"status"`
}

// consumptionNoticeView is one notification reading of the diagnostic face,
// beside the frozen routing the commit record carries.
type consumptionNoticeView struct {
	TaskID         string `json:"task_id"`
	Round          int64  `json:"round"`
	Kind           string `json:"kind"`
	Seq            int64  `json:"seq"`
	Event          string `json:"event"`
	TargetAgent    string `json:"target_agent"`
	TargetSession  string `json:"target_session"`
	TargetInstance string `json:"target_instance"`
	Phase          string `json:"phase"`
	Blocked        int    `json:"blocked,omitempty"`
}

// FactKey reports the stable identity of the read notification.
func (v consumptionNoticeView) FactKey() string {
	return fmt.Sprintf("%s:%d:%s:%d", v.TaskID, v.Round, v.Kind, v.Seq)
}

// consumptionEntryView is one mailbox entry reading beside the notice it
// accepted.
type consumptionEntryView struct {
	consumptionNoticeView
	AcceptSeq uint64 `json:"accept_seq"`
	Claim     string `json:"claim"`
	Completed bool   `json:"completed"`
}

// consumptionMailboxView is one site's mailbox reading.
type consumptionMailboxView struct {
	Address  string                 `json:"address"`
	Instance string                 `json:"instance"`
	Consumed uint64                 `json:"consumed"`
	Entries  []consumptionEntryView `json:"entries"`
}

// runConsumptionChild dispatches one child phase of the restart anchors.
func runConsumptionChild(t *testing.T, mode string) {
	t.Helper()
	root := os.Getenv(consumptionChildRootEnv)
	out := os.Getenv(consumptionChildOutEnv)
	if root == "" || out == "" {
		t.Fatalf("the consumption child was handed root=%q out=%q, want both named", root, out)
	}
	switch mode {
	case consumptionModeSeed:
		seedConsumptionProcess(t, root, out)
	case consumptionModeRecover:
		recoverConsumptionProcess(t, root, out)
	case consumptionModeHandover:
		seedHandoverProcess(t, root, out)
	default:
		t.Fatalf("the consumption child was handed the unknown mode %q", mode)
	}
}

// seedConsumptionProcess commits the claimed notification's task chain beside
// the real turn of the consumed notification, reports both readings, and waits
// for the parent's interruption: it never closes, drains or hands anything on.
func seedConsumptionProcess(t *testing.T, root, out string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report := consumptionChildReport{Mode: consumptionModeSeed, PID: os.Getpid()}

	spec, notice := consumptionSeedClaimedTask(t, ctx, root)
	site := newConsumptionSite(t, spec.Source, spec.InstanceID)
	site.accept(t, notice)
	seq := site.seqOf(t, notice)
	if err := site.store.Claim(ctx, spec.Source, spec.InstanceID, seq); err != nil {
		t.Fatalf("claim the seeded notification: %v", err)
	}
	report.Mailbox = append(report.Mailbox, consumptionMailboxViewOf(site.session(t)))

	run := assembleConsumptionRun(t, ctx, root)
	report.Recovery = consumptionRecoveryViews(run.Recovery())
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-seed",
		Address:      consumptionSeedAddress,
		Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
	}); err != nil {
		t.Fatalf("invoke the seeded runtime: %v", err)
	}
	addresses := []event.Address{consumptionSeedAddress, consumptionClaimAddress}
	report.Tasks = consumptionTaskViews(t, ctx, run, addresses)
	report.Notices = consumptionNoticeViews(t, ctx, run, addresses)
	report.Notes = append(report.Notes, "the seed waits for the parent's kill and never closes")
	writeConsumptionReport(t, out, report)
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Minute):
	}
}

// recoverConsumptionProcess boots a second host over the same durable root and
// reports what the restart alone rebuilt: the formal Boot entry reads the tier
// the interrupted process left, never the memory of this process.
func recoverConsumptionProcess(t *testing.T, root, out string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run := recoverConsumptionRun(t, ctx, root)
	report := consumptionChildReport{Mode: consumptionModeRecover, PID: os.Getpid()}
	if os.Getenv(consumptionChildCfgEnv) != "" {
		report.Notes = append(report.Notes, "the restart opened through assembly.Boot")
	}
	report.Recovery = consumptionRecoveryViews(run.Recovery())
	addresses := []event.Address{consumptionSeedAddress, consumptionClaimAddress}
	report.Tasks = consumptionTaskViews(t, ctx, run, addresses)
	report.Notices = consumptionNoticeViews(t, ctx, run, addresses)
	writeConsumptionReport(t, out, report)
}

// seedHandoverProcess drives one real tool turn to its settled fact and stops
// inside the handover: the probe holds the After pass of that delivery before
// the notification reaches the intake, the child reports the state the durable
// tier and the mailbox stand in at that point, and it never closes or hands
// anything on.
func seedHandoverProcess(t *testing.T, root, out string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report := consumptionChildReport{Mode: consumptionModeHandover, PID: os.Getpid()}

	probe := &handoverProbe{reached: make(chan struct{})}
	run := assembleHandoverRun(t, ctx, root, probe)
	report.Recovery = consumptionRecoveryViews(run.Recovery())
	done := make(chan error, 1)
	go func() {
		_, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-handover",
			Address:      consumptionSeedAddress,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hand over"}},
		})
		done <- err
	}()
	waitHandoverReached(t, probe.reached, done)
	addresses := []event.Address{consumptionSeedAddress}
	report.Tasks = consumptionTaskViews(t, ctx, run, addresses)
	report.Notices = consumptionNoticeViews(t, ctx, run, addresses)
	report.Notes = append(report.Notes, "the seed stands inside the handover, never closes and never hands the notice on")
	writeConsumptionReport(t, out, report)
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Minute):
	}
}

// waitHandoverReached waits until the probe holds the settled delivery, so the
// report is read from the very state the interruption leaves on disk. A turn
// that returns early is not the handover the anchor proves: the hold is still
// awaited briefly, because the delivery runs beside the caller that admitted it.
func waitHandoverReached(t *testing.T, reached <-chan struct{}, done <-chan error) {
	t.Helper()
	var returned error
	select {
	case <-reached:
		return
	case returned = <-done:
	case <-time.After(90 * time.Second):
		t.Fatalf("the seeded turn never reached the handover")
	}
	select {
	case <-reached:
	case <-time.After(30 * time.Second):
		t.Fatalf("the seeded turn returned (err=%v) without reaching the handover", returned)
	}
}

// handoverProbe is the test face of the handover window: it holds the After
// pass of the delivery fact a backflow commits, after that fact landed and
// before the notification reaches the intake. The hold is the interruption
// point the anchor needs — the point a process exit must still recover from —
// and the probe declares the post-commit observation port like any other face.
type handoverProbe struct {
	run     *assembly.Run
	reached chan struct{}
	once    sync.Once
}

// Name returns the stable name of the probe.
func (p *handoverProbe) Name() string { return "handover-probe" }

// Port returns the single port the probe serves.
func (p *handoverProbe) Port() plugin.Port {
	return plugin.PortFactObservation
}

// ObserveCommit holds the settled delivery of a tool call at the handover
// boundary: a delivery fact carrying no settle-lineage stamp is the one the
// backflow commits, so the probe tells it apart from the fact a delivery flush
// observes and holds exactly that pass until the parent interrupts the process.
func (p *handoverProbe) ObserveCommit(ctx context.Context, id event.EventID) error {
	fact, err := p.run.CommittedEvent(ctx, id)
	if err != nil {
		return err
	}
	if fact.Type != event.TypeTaskSettled || fact.TaskID == "" || fact.Meta[event.MetaKeySettleTriggerSource] != "" {
		return nil
	}
	p.once.Do(func() { close(p.reached) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil
	}
}

// assembleHandoverRun assembles the runtime the handover anchor drives over the
// durable root named: the same assembly entry the host boots, the native tool
// the turn calls, and the probe that holds the delivery inside the handover.
func assembleHandoverRun(t *testing.T, ctx context.Context, root string, probe *handoverProbe) *assembly.Run {
	t.Helper()
	run, err := assembly.Assemble(ctx, chainDeclaration(), map[event.AgentID]assembly.NativeAgent{
		"agent-a": {
			Model: &chainModel{name: "model-a", answer: func(req *model.Request) *model.Response {
				if len(req.Messages) == 1 {
					return chainToolTurn(chainCall("call-handover", "probe", `{"x":1}`))
				}
				return chainFinalTurn("settled")
			}},
			Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-handover"}},
		},
	}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}),
		assembly.WithPlugins(func(assembled *assembly.Run) ([]plugin.Instance, error) {
			probe.run = assembled
			return []plugin.Instance{probe}, nil
		}))
	if err != nil {
		t.Fatalf("assemble the handover runtime: %v", err)
	}
	return run
}

// consumptionSeedClaimedTask commits one settled task chain whose outbox owes
// the notification the seed claims but never consumes.
func consumptionSeedClaimedTask(t *testing.T, ctx context.Context, root string) (task.Spec, task.Notice) {
	t.Helper()
	cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}
	tier, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("open the seed tier: %v", err)
	}
	durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("build the seed commit: %v", err)
	}
	registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
	spec := task.Spec{
		CallID:       "call-claimed",
		DedupKey:     "inst-claimed\x00inv-claimed\x00dec-claimed\x00call-claimed",
		InvocationID: "inv-claimed",
		InstanceID:   consumptionClaimInst,
		Description:  "claimed",
		Source:       consumptionClaimAddress,
		Execution:    "serialized",
	}
	receipt, err := registry.Register(ctx, spec)
	if err != nil {
		t.Fatalf("register the claimed task: %v", err)
	}
	notice := task.Notice{TaskID: receipt.TaskID, Round: 1, Kind: task.NoticeSettled, Seq: 0, Event: "evt-settled-claimed"}
	envelope := task.Envelope{Notice: notice, Target: consumptionClaimAddress, Source: consumptionClaimAddress, TargetInstance: consumptionClaimInst}
	result, err := registry.Advance(ctx, receipt.TaskID, task.StatusSettled, task.MutationID("settle:"+string(receipt.TaskID)), []task.Envelope{envelope})
	if err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the claimed task = %v, %v, want committed", result, err)
	}
	return spec, notice
}

// recoverConsumptionRun opens the restart through the formal Boot entry when
// the child was handed a host document, and through the very assembly entry
// Boot calls otherwise: both fold the durable chains of the same root.
func recoverConsumptionRun(t *testing.T, ctx context.Context, root string) *assembly.Run {
	t.Helper()
	configPath := os.Getenv(consumptionChildCfgEnv)
	if configPath == "" {
		return assembleConsumptionRun(t, ctx, root)
	}
	host, err := assembly.Boot(configPath)
	if err != nil {
		t.Fatalf("boot the restarted host: %v", err)
	}
	return host.Run()
}

// consumptionHostDocument renders the bare host document the restart boots:
// the one entry agent and the durable root, no tool and no sender.
func consumptionHostDocument(root string) string {
	return fmt.Sprintf("entry: agent-a\nprovider: local\nmodel: model-a\nproviders:\n  local: { provider: openai, api_endpoint: \"http://127.0.0.1:1/v1\", api_key_env: TAGENT_TEST_API_KEY }\nagents:\n  agent-a: {}\nstorage: { type: localfile, root: %q }\n", root)
}

// assembleConsumptionRun assembles the runtime the restart anchors drive over
// the durable root named: the same assembly entry the host boots, beside the
// native tool the seed's turn calls.
func assembleConsumptionRun(t *testing.T, ctx context.Context, root string) *assembly.Run {
	t.Helper()
	run, err := assembly.Assemble(ctx, chainDeclaration(), map[event.AgentID]assembly.NativeAgent{
		"agent-a": {
			Model: &chainModel{name: "model-a", answer: func(req *model.Request) *model.Response {
				if len(req.Messages) == 1 {
					return chainToolTurn(chainCall("call-seed", "probe", `{"x":1}`))
				}
				return chainFinalTurn("settled")
			}},
			Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}},
		},
	}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	if err != nil {
		t.Fatalf("assemble the consumption runtime: %v", err)
	}
	return run
}

// consumptionRecoveryViews reads the restore outcomes of one assembly.
func consumptionRecoveryViews(report task.RestoreReport) []consumptionRecoveryView {
	views := make([]consumptionRecoveryView, 0, len(report.Tasks))
	for _, outcome := range report.Tasks {
		view := consumptionRecoveryView{
			TaskID:    string(outcome.TaskID),
			Owner:     string(outcome.Owner),
			Status:    int(outcome.Status),
			Recovered: outcome.Recovered,
		}
		if outcome.Refused != nil {
			view.Refused = outcome.Refused.Error()
		}
		views = append(views, view)
	}
	return views
}

// restoreAssertSeed proves the seed process really committed the material the
// restart checks read, so no restart check can pass over an empty seed.
func restoreAssertSeed(t *testing.T, o restoreOutcome) {
	t.Helper()
	if !o.Seed.Ready || o.Seed.Mode != o.Mode {
		t.Fatalf("the %s seed report = %+v, want a ready report of its own mode", o.Mode, o.Seed)
	}
	if !o.Recover.Ready || o.Recover.Mode != restoreModeRecover {
		t.Fatalf("the %s restart report = %+v, want a ready report of the reopen", o.Mode, o.Recover)
	}
	for _, want := range []struct {
		what  string
		value string
	}{
		{"input", o.Seed.Input},
		{"tool result", o.Seed.ResultText},
		{"assistant output", o.Seed.Answer},
		{"call identity", o.Seed.CallID},
		{"task identity", string(o.Seed.TaskID)},
		{"invocation identity", string(o.Seed.Invocation)},
		{"runtime instance", string(o.Seed.Instance)},
		{"input event identity", string(o.Seed.InputFact)},
		{"result event identity", string(o.Seed.ResultFact)},
		{"answer event identity", string(o.Seed.AnswerFact)},
	} {
		if want.value == "" {
			t.Fatalf("the %s seed reported no %s, so the restart check would read nothing", o.Mode, want.what)
		}
	}
	if o.Seed.Input != restoreSeedInput || o.Seed.Answer != restoreSeedAnswer {
		t.Fatalf("the %s seed committed input=%q answer=%q, want the seeded input and answer", o.Mode, o.Seed.Input, o.Seed.Answer)
	}
}

// restoreAssertOriginalText proves the three texts a turn committed answer byte
// for byte from the tier alone after both stop modes.
func restoreAssertOriginalText(t *testing.T, outcome func(*testing.T, string) restoreOutcome) {
	t.Helper()
	for _, mode := range restoreModes {
		o := outcome(t, mode)
		restoreAssertSeed(t, o)
		for _, want := range []struct {
			what string
			text string
			key  event.EventID
		}{
			{"conversation input", o.Seed.Input, o.Seed.InputFact},
			{"tool result", o.Seed.ResultText, o.Seed.ResultFact},
			{"assistant output", o.Seed.Answer, o.Seed.AnswerFact},
		} {
			got, held := o.Recover.Restored[string(want.key)]
			if !held {
				t.Fatalf("the %s restart reported no read of the %s event %s", mode, want.what, want.key)
			}
			if !bytes.Equal([]byte(got), []byte(want.text)) {
				t.Fatalf("the %s restart read the %s as %q (%d bytes), want the committed bytes %q (%d bytes)",
					mode, want.what, got, len(got), want.text, len(want.text))
			}
		}
		if want := string(o.Seed.ResultFact); !restoreCarriesString(o.Recover.Observed, want) {
			t.Fatalf("the %s restart's observation table does not carry the tool result %s; observed=%v", mode, want, o.Recover.Observed)
		}
	}
}

// restoreAssertAttribution proves the invocation and task causal identities the
// seed committed are read back at the same site and originating instance.
func restoreAssertAttribution(t *testing.T, outcome func(*testing.T, string) restoreOutcome) {
	t.Helper()
	for _, mode := range restoreModes {
		o := outcome(t, mode)
		restoreAssertSeed(t, o)
		recovered := false
		for _, view := range o.Recover.Recovery {
			if view.TaskID != string(o.Seed.TaskID) {
				continue
			}
			recovered = true
			if !view.Recovered {
				t.Fatalf("the %s restart did not rebuild task %s; recovery=%+v", mode, o.Seed.TaskID, o.Recover.Recovery)
			}
			if view.Status != int(task.StatusSettled) {
				t.Fatalf("the %s restart rebuilt task %s at status %d, want the settled status %d",
					mode, o.Seed.TaskID, view.Status, int(task.StatusSettled))
			}
			if view.Owner != string(persistenceAgentID) {
				t.Fatalf("the %s restart rebuilt task %s under owner %q, want the admitting agent %q",
					mode, o.Seed.TaskID, view.Owner, persistenceAgentID)
			}
		}
		if !recovered {
			t.Fatalf("the %s restart rebuilt no task %s; recovery=%+v", mode, o.Seed.TaskID, o.Recover.Recovery)
		}

		invocation, held := restoreInvocationOf(o.Recover, o.Seed.Invocation)
		if !held {
			t.Fatalf("the %s restart read no invocation %s; invocations=%+v", mode, o.Seed.Invocation, o.Recover.Invocations)
		}
		if invocation.Address != o.Seed.Address {
			t.Fatalf("the %s restart attributed invocation %s to %+v, want the committed site %+v",
				mode, o.Seed.Invocation, invocation.Address, o.Seed.Address)
		}
		if invocation.Instance != o.Seed.Instance {
			t.Fatalf("the %s restart attributed invocation %s to instance %q, want the admitting instance %q",
				mode, o.Seed.Invocation, invocation.Instance, o.Seed.Instance)
		}

		taskView, held := restoreTaskOf(o.Recover, o.Seed.TaskID)
		if !held {
			t.Fatalf("the %s restart read no task %s; tasks=%+v", mode, o.Seed.TaskID, o.Recover.Tasks)
		}
		if taskView.Address != o.Seed.Address {
			t.Fatalf("the %s restart attributed task %s to %+v, want the committed site %+v",
				mode, o.Seed.TaskID, taskView.Address, o.Seed.Address)
		}
		if taskView.Instance != o.Seed.Instance {
			t.Fatalf("the %s restart attributed task %s to instance %q, want the admitting instance %q",
				mode, o.Seed.TaskID, taskView.Instance, o.Seed.Instance)
		}
		if taskView.Status != int(task.StatusSettled) {
			t.Fatalf("the %s restart read task %s at status %d, want the settled status %d",
				mode, o.Seed.TaskID, taskView.Status, int(task.StatusSettled))
		}
	}
}

// restoreAssertPairing proves the call identity the provider issued, the task
// the admission froze and the result the settled transition linked to it still
// answer as one chain after both stop modes.
func restoreAssertPairing(t *testing.T, outcome func(*testing.T, string) restoreOutcome) {
	t.Helper()
	for _, mode := range restoreModes {
		o := outcome(t, mode)
		restoreAssertSeed(t, o)
		chain, held := restoreChainOf(o.Recover, o.Seed.TaskID)
		if !held {
			t.Fatalf("the %s restart read no durable chain for task %s; chains=%+v", mode, o.Seed.TaskID, o.Recover.Chains)
		}
		if chain.CallID != o.Seed.CallID {
			t.Fatalf("the %s restart paired task %s with call %q, want the call the provider issued %q",
				mode, chain.TaskID, chain.CallID, o.Seed.CallID)
		}
		if chain.Invocation != o.Seed.Invocation {
			t.Fatalf("the %s restart paired task %s with invocation %q, want %q",
				mode, chain.TaskID, chain.Invocation, o.Seed.Invocation)
		}
		if chain.Source != o.Seed.Address {
			t.Fatalf("the %s restart paired task %s with source %+v, want %+v",
				mode, chain.TaskID, chain.Source, o.Seed.Address)
		}
		if chain.Instance != o.Seed.Instance {
			t.Fatalf("the %s restart paired task %s with instance %q, want %q",
				mode, chain.TaskID, chain.Instance, o.Seed.Instance)
		}
		if chain.Links < 2 {
			t.Fatalf("the %s restart read %d chain link(s) for task %s, want the admission and the settlement",
				mode, chain.Links, o.Seed.TaskID)
		}
		if chain.ResultFact != o.Seed.ResultFact {
			t.Fatalf("the %s restart's settled link points at result %s, want the committed tool result %s",
				mode, chain.ResultFact, o.Seed.ResultFact)
		}
		if chain.ResultTask != o.Seed.TaskID {
			t.Fatalf("the %s restart's settled link names task %s, want %s", mode, chain.ResultTask, o.Seed.TaskID)
		}
		if got := o.Recover.Restored[string(chain.ResultFact)]; !bytes.Equal([]byte(got), []byte(o.Seed.ResultText)) {
			t.Fatalf("the %s restart paired call %q with result %q, want the committed tool result %q",
				mode, chain.CallID, got, o.Seed.ResultText)
		}
	}
}

// restoreAssertNextRequest proves the next actual model request of the restarted
// process carries the restored material ahead of its own input.
func restoreAssertNextRequest(t *testing.T, outcome func(*testing.T, string) restoreOutcome) {
	t.Helper()
	for _, mode := range restoreModes {
		o := outcome(t, mode)
		restoreAssertSeed(t, o)
		if o.Recover.Requests < 1 {
			t.Fatalf("the %s restart sent no model request after the reopen", mode)
		}
		messages := o.Recover.Messages
		want := []struct {
			role    string
			content string
			what    string
		}{
			{string(model.RoleUser), o.Seed.Input, "the conversation input"},
			{string(model.RoleAssistant), persistenceCallText, "the assistant tool call"},
			{string(model.RoleTool), o.Seed.ResultText, "the tool result"},
			{string(model.RoleAssistant), o.Seed.Answer, "the assistant output"},
		}
		if len(messages) < len(want)+1 {
			t.Fatalf("the %s restart's next request carried %d message(s) %s, want the restored material beside its own input",
				mode, len(messages), restoreDescribe(messages))
		}
		for at, expectation := range want {
			if messages[at].Role != expectation.role || !bytes.Equal([]byte(messages[at].Content), []byte(expectation.content)) {
				t.Fatalf("the %s restart's next request message %d = %s %q, want %s %q (the restored material must be carried, not only read from the ledger); messages=%s",
					mode, at, messages[at].Role, messages[at].Content, expectation.role, expectation.content, restoreDescribe(messages))
			}
		}
		carriedCalls := messages[1].ToolCalls
		if len(carriedCalls) != 1 || carriedCalls[0].ID != restoreCallID {
			t.Fatalf("the %s restart's next request carried the call message %s, want the tool call %s the chain paired beside its result",
				mode, restoreDescribe(messages), restoreCallID)
		}
		last := messages[len(messages)-1]
		if last.Role != string(model.RoleUser) || last.Content != restoreNextInput {
			t.Fatalf("the %s restart's next request ended with %s %q, want its own input %q; messages=%s",
				mode, last.Role, last.Content, restoreNextInput, restoreDescribe(messages))
		}
	}
}

// restoreAssertLedgerIsNotCompletion is the negative reading: the restored
// ledger is complete after both stop modes, and that completeness is still not
// completion while the next actual request carries none of it.
func restoreAssertLedgerIsNotCompletion(t *testing.T, outcome func(*testing.T, string) restoreOutcome) {
	t.Helper()
	for _, mode := range restoreModes {
		o := outcome(t, mode)
		restoreAssertSeed(t, o)
		for _, want := range []struct {
			what string
			text string
			key  event.EventID
		}{
			{"conversation input", o.Seed.Input, o.Seed.InputFact},
			{"tool result", o.Seed.ResultText, o.Seed.ResultFact},
			{"assistant output", o.Seed.Answer, o.Seed.AnswerFact},
		} {
			if got := o.Recover.Restored[string(want.key)]; !bytes.Equal([]byte(got), []byte(want.text)) {
				t.Fatalf("the %s restart did not even recover the %s from the ledger: %q != %q", mode, want.what, got, want.text)
			}
		}
		carried := map[string]bool{}
		for _, message := range o.Recover.Messages {
			carried[message.Content] = true
		}
		for _, want := range []struct {
			what string
			text string
		}{
			{"conversation input", o.Seed.Input},
			{"tool result", o.Seed.ResultText},
			{"assistant output", o.Seed.Answer},
		} {
			if !carried[want.text] {
				t.Fatalf("restore_only_ledger_is_not_completion: the %s restart recovered the %s in the ledger but its next actual request carried none of it; messages=%s",
					mode, want.what, restoreDescribe(o.Recover.Messages))
			}
		}
	}
}

// restoreInvocationOf reports the invocation view the identity names.
func restoreInvocationOf(report restoreChildReport, id event.InvocationID) (restoreInvocationView, bool) {
	for _, view := range report.Invocations {
		if view.InvocationID == id {
			return view, true
		}
	}
	return restoreInvocationView{}, false
}

// restoreTaskOf reports the task view the identity names.
func restoreTaskOf(report restoreChildReport, id event.TaskID) (restoreTaskView, bool) {
	for _, view := range report.Tasks {
		if view.TaskID == id {
			return view, true
		}
	}
	return restoreTaskView{}, false
}

// restoreChainOf reports the durable chain view the task identity names.
func restoreChainOf(report restoreChildReport, id event.TaskID) (restoreChainView, bool) {
	for _, view := range report.Chains {
		if view.TaskID == id {
			return view, true
		}
	}
	return restoreChainView{}, false
}

// restoreCarriesString reports whether the list carries the text.
func restoreCarriesString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// restoreDescribe renders one recorded request as the role and content of every
// message, so a refused assertion names the material the request really carried.
func restoreDescribe(messages []persistenceMessage) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		parts = append(parts, fmt.Sprintf("%s:%q", message.Role, message.Content))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// consumptionTaskViews reads the task dimension of one assembly.
func consumptionTaskViews(t *testing.T, ctx context.Context, run *assembly.Run, addresses []event.Address) []consumptionTaskView {
	t.Helper()
	snapshot, err := run.Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}, Sessions: addresses},
		Kinds: []query.ObjectKind{query.ObjectTask},
	})
	if err != nil {
		t.Fatalf("read the task dimension: %v", err)
	}
	views := make([]consumptionTaskView, 0, len(snapshot.Details))
	for _, detail := range snapshot.Details {
		views = append(views, consumptionTaskView{TaskID: string(detail.Ref.TaskID), Status: int(detail.State.TaskStatus)})
	}
	return views
}

// consumptionNoticeViews reads the notification dimension of one assembly.
func consumptionNoticeViews(t *testing.T, ctx context.Context, run *assembly.Run, addresses []event.Address) []consumptionNoticeView {
	t.Helper()
	snapshot, err := run.Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}, Sessions: addresses},
		Kinds: []query.ObjectKind{query.ObjectNotice},
	})
	if err != nil {
		t.Fatalf("read the notification dimension: %v", err)
	}
	views := make([]consumptionNoticeView, 0, len(snapshot.Details))
	for _, detail := range snapshot.Details {
		notice := detail.Ref.Notice
		views = append(views, consumptionNoticeView{
			TaskID:         string(notice.TaskID),
			Round:          notice.Round,
			Kind:           string(notice.Kind),
			Seq:            notice.Seq,
			Event:          string(notice.Event),
			TargetAgent:    string(detail.Ref.Address.AgentID),
			TargetSession:  string(detail.Ref.Address.SessionID),
			TargetInstance: string(detail.Ref.InstanceID),
			Phase:          string(detail.State.NoticePhase),
			Blocked:        len(detail.Blocked),
		})
	}
	return views
}

// consumptionMailboxViewOf reads one site's mailbox time points.
func consumptionMailboxViewOf(session *state.Session) consumptionMailboxView {
	view := consumptionMailboxView{
		Address:  string(session.Address.AgentID) + "/" + string(session.Address.SessionID),
		Instance: string(session.Instance),
		Consumed: uint64(session.Consumed),
	}
	for _, entry := range session.Mailbox {
		view.Entries = append(view.Entries, consumptionEntryView{
			consumptionNoticeView{
				TaskID:         string(entry.Notice.TaskID),
				Round:          entry.Notice.Round,
				Kind:           string(entry.Notice.Kind),
				Seq:            entry.Notice.Seq,
				Event:          string(entry.Notice.Event),
				TargetAgent:    string(session.Address.AgentID),
				TargetSession:  string(session.Address.SessionID),
				TargetInstance: string(session.Instance),
				Phase:          consumptionPhaseOf(entry),
			},
			uint64(entry.AcceptSeq),
			consumptionClaimName(entry.Claim),
			entry.Receipt.Completed,
		})
	}
	return view
}

// consumptionPhaseOf derives the phase one mailbox entry stands in.
func consumptionPhaseOf(entry state.MailboxEntry) string {
	if entry.Receipt.Completed || entry.Claim == state.ClaimConsumed {
		return string(query.NoticePhaseConsumed)
	}
	if entry.Claim == state.ClaimClaimed {
		return string(query.NoticePhaseClaimed)
	}
	return string(query.NoticePhaseAccepted)
}

// consumptionClaimName names one claim state.
func consumptionClaimName(claim state.ClaimState) string {
	switch claim {
	case state.ClaimClaimed:
		return "claimed"
	case state.ClaimConsumed:
		return "consumed"
	default:
		return "pending"
	}
}

// writeConsumptionReport lands one child report atomically, so the parent
// never reads a half-written one.
func writeConsumptionReport(t *testing.T, path string, report consumptionChildReport) {
	t.Helper()
	report.Ready = true
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("encode the child report: %v", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		t.Fatalf("write the child report: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("land the child report: %v", err)
	}
}

// runConsumptionSeed starts the seed child, waits for its ready report and
// interrupts it, so the restart reads only what the durable tier kept.
func runConsumptionSeed(t *testing.T, root string) consumptionChildReport {
	t.Helper()
	return interruptConsumptionChild(t, consumptionModeSeed, root)
}

// interruptConsumptionChild starts the named child phase over the durable root,
// waits for the report it writes once it reached the point its phase proves, and
// interrupts it, so the restart reads only what the durable tier kept.
func interruptConsumptionChild(t *testing.T, mode, root string) consumptionChildReport {
	t.Helper()
	out := filepath.Join(t.TempDir(), mode+".json")
	cmd := consumptionChildCommand(mode, root, out)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the %s process: %v", mode, err)
	}
	report := waitConsumptionReport(t, out, &output, mode)
	t.Logf("parent: interrupt the %s process pid=%d", mode, cmd.Process.Pid)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("interrupt the %s process: %v", mode, err)
	}
	err := cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the interrupted %s reported %v, want the signalled exit the parent caused; output=%s", mode, err, output.String())
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		t.Fatalf("the %s ended %v, want the parent's kill; output=%s", mode, err, output.String())
	}
	t.Logf("parent: child Wait → signalled=%v signal=%v code=%d", status.Signaled(), status.Signal(), exitErr.ExitCode())
	return report
}

// runConsumptionRecover starts the recover child over the same durable root and
// reads the report the restart itself produced.
func runConsumptionRecover(t *testing.T, root string) consumptionChildReport {
	t.Helper()
	out := filepath.Join(t.TempDir(), "recover.json")
	configPath := filepath.Join(t.TempDir(), "host.yaml")
	if err := os.WriteFile(configPath, []byte(consumptionHostDocument(root)), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	cmd := consumptionChildCommand(consumptionModeRecover, root, out)
	cmd.Env = append(cmd.Env,
		consumptionChildCfgEnv+"="+configPath,
		"TAGENT_TEST_API_KEY=fixture",
		"TAGENT_HTTP_PORT=0",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("the recover process reported %v; output=%s", err, output.String())
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the recover report: %v; output=%s", err, output.String())
	}
	var report consumptionChildReport
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("decode the recover report: %v", err)
	}
	if !report.Ready || report.Mode != consumptionModeRecover {
		t.Fatalf("recover report = %+v, want a ready report of its own phase", report)
	}
	return report
}

// consumptionChildCommand builds the self-exec command of one child phase over
// the report it lands.
func consumptionChildCommand(mode, root, out string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestEventIntegrityConsumptionRecovery$", "-test.timeout=10m")
	cmd.Env = append(os.Environ(),
		consumptionChildModeEnv+"="+mode,
		consumptionChildRootEnv+"="+root,
		consumptionChildOutEnv+"="+out,
	)
	return cmd
}

// waitConsumptionReport waits for the child's atomic report.
func waitConsumptionReport(t *testing.T, path string, output *bytes.Buffer, mode string) consumptionChildReport {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		body, err := os.ReadFile(path)
		if err == nil {
			var report consumptionChildReport
			if json.Unmarshal(body, &report) == nil && report.Ready && report.Mode == mode {
				return report
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the %s process reported no ready report at %s; output=%s", mode, path, output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// consumptionNoticeInPhase reports the one notification the seed read in the
// named phase.
func consumptionNoticeInPhase(t *testing.T, report consumptionChildReport, phase query.NoticePhase) consumptionNoticeView {
	t.Helper()
	var found []consumptionNoticeView
	for _, notice := range report.Notices {
		if notice.Phase == string(phase) {
			found = append(found, notice)
		}
	}
	if len(found) != 1 {
		t.Fatalf("seed notices = %+v, want exactly one standing in phase %q", report.Notices, phase)
	}
	return found[0]
}

// consumptionClaimedEntry reports the one mailbox entry the seed left claimed.
func consumptionClaimedEntry(t *testing.T, report consumptionChildReport) consumptionEntryView {
	t.Helper()
	var found []consumptionEntryView
	for _, site := range report.Mailbox {
		for _, entry := range site.Entries {
			if entry.Claim == "claimed" {
				found = append(found, entry)
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("seeded mailbox = %+v, want exactly one claimed entry", report.Mailbox)
	}
	return found[0]
}

// consumptionRecoveryOf reports the restore outcome of one task.
func consumptionRecoveryOf(report consumptionChildReport, taskID string) (consumptionRecoveryView, bool) {
	for _, outcome := range report.Recovery {
		if outcome.TaskID == taskID {
			return outcome, true
		}
	}
	return consumptionRecoveryView{}, false
}

// consumptionNoticeOf reports the reading of one notification identity.
func consumptionNoticeOf(report consumptionChildReport, want consumptionNoticeView) (consumptionNoticeView, bool) {
	for _, notice := range report.Notices {
		if notice.FactKey() == want.FactKey() {
			return notice, true
		}
	}
	return consumptionNoticeView{}, false
}

// projectionWindowTokens is the small window the projection anchors declare: the commit plugin files its judgment as a compress request, and the fold's output is read from the projection, the chain and the formal request.
//
// The event keys are spelled turn-position-suffix, so a chain scanned by key replays the turns in the order they committed.
const projectionWindowTokens = 60

// projectionBase is the pass time the projection turns are laid out from.
var projectionBase = time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)

// projectionLeg is one fact of a projection turn: the position it takes in the
// turn beside the frozen event type it commits and the one-line read it stores.
type projectionLeg struct{ position, typ, summary string }

// projectionTurnLegs reports the four legs of one closed turn: the input that
// opens it, the reasoning and the tool command between the input and the output,
// and the output that closes it.
func projectionTurnLegs(turn int) []projectionLeg {
	return []projectionLeg{
		{"0-in", event.TypeExternalInput, fmt.Sprintf("ask %02d", turn)},
		{"1-plan", event.TypeThinkingPlan, fmt.Sprintf("plan %02d", turn)},
		{"2-call", event.TypeActionCommand, fmt.Sprintf("call %02d", turn)},
		{"3-out", event.TypeAgentOutput, fmt.Sprintf("answer %02d", turn)},
	}
}

// projectionBody reports the original text one leg of a turn carries: long
// enough that a handful of turns fills the small window, and distinct per leg so
// a body the fold consumed is recognisable in the formal request.
func projectionBody(turn int, position string) string {
	return strings.Repeat(fmt.Sprintf("正文 %s %02d。", position, turn), 12)
}

// projectionKey reports the event key of one leg: the turn first and the
// position in it second, so key order is commit order.
func projectionKey(turn int, position string) event.EventID {
	return event.EventID(fmt.Sprintf("%02d-%s", turn, position))
}

// newProjectionRun assembles one runtime whose agent declares the given model
// window over the given provider, and reports the address its turns run at.
func newProjectionRun(t *testing.T, maxTokens int, provider model.Model) (*assembly.Run, event.Address) {
	t.Helper()
	run, err := assembly.Assemble(context.Background(), &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: maxTokens}},
	}, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run, event.Address{AgentID: "agent-a", SessionID: "session-projection"}
}

// commitProjectionTurn commits the four legs of one closed turn under the run.
func commitProjectionTurn(t *testing.T, ctx context.Context, run *assembly.Run, addr event.Address, turn int) {
	t.Helper()
	for position, leg := range projectionTurnLegs(turn) {
		if _, err := run.CommitToSession(ctx, addr, event.Event{
			Key:       projectionKey(turn, leg.position),
			Type:      leg.typ,
			Timestamp: projectionBase.Add(time.Duration(turn*10+position) * time.Minute),
			Meta:      map[string]string{event.MetaKeyEventSummary: leg.summary},
			Message:   model.Message{Role: model.RoleUser, Content: projectionBody(turn, leg.position)},
		}); err != nil {
			t.Fatalf("commit %s: %v", projectionKey(turn, leg.position), err)
		}
	}
}

// commitProjectionLeg commits one leg of a turn under the run.
func commitProjectionLeg(t *testing.T, ctx context.Context, run *assembly.Run, addr event.Address, turn int, position, typ, summary string, content string) {
	t.Helper()
	if _, err := run.CommitToSession(ctx, addr, event.Event{
		Key:       projectionKey(turn, position),
		Type:      typ,
		Timestamp: projectionBase.Add(time.Duration(turn*10) * time.Minute),
		Meta:      map[string]string{event.MetaKeyEventSummary: summary},
		Message:   model.Message{Role: model.RoleUser, Content: content},
	}); err != nil {
		t.Fatalf("commit %s: %v", projectionKey(turn, position), err)
	}
}

// projectionEntriesOf reports the projection entries of one session in their
// canonical order, refusing a session that registered no projection.
func projectionEntriesOf(t *testing.T, run *assembly.Run, session event.SessionID) []replay.ProjectionEntry {
	t.Helper()
	entries, held := run.ReplayEntries(session)
	if !held {
		t.Fatalf("session %q registered no projection", session)
	}
	return entries
}

// projectionKeysOf reports the entry keys of a projection in order.
func projectionKeysOf(entries []replay.ProjectionEntry) []string {
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, string(entry.Key))
	}
	return keys
}

// projectionProductsOf reports the synthetic product keys of one session's
// projection in projection order.
func projectionProductsOf(t *testing.T, run *assembly.Run, session event.SessionID) []event.EventID {
	t.Helper()
	keys := make([]event.EventID, 0, 4)
	for _, entry := range projectionEntriesOf(t, run, session) {
		if replay.SyntheticKey(entry.Key) {
			keys = append(keys, entry.Key)
		}
	}
	return keys
}

// projectionFactCountOf reports how many facts of one frozen type the session
// chain holds.
func projectionFactCountOf(run *assembly.Run, session event.SessionID, factType string) int {
	count := 0
	for _, evt := range run.SessionFacts(event.Address{SessionID: session}) {
		if evt.Type == factType {
			count++
		}
	}
	return count
}

// rebuildProjectionFromRun replays the mapped facts of one session chain into an
// empty projection, as the cold start reads them: a compress request carries no
// message and maps onto no projection entry, so the rebuild reads back exactly
// the material the commits mapped. It reports the rebuilt projection, the
// rebuild report and how many mapped facts the chain carried.
func rebuildProjectionFromRun(t *testing.T, ctx context.Context, run *assembly.Run, session event.SessionID) (*replay.ReplayProjection, replay.RebuildReport, int) {
	t.Helper()
	chainKey := replay.ChainKey{PartitionID: 1, SessionID: session}
	chain := replay.NewMemoryEventChain()
	mapped := 0
	for _, evt := range run.SessionFacts(event.Address{SessionID: session}) {
		if evt.Type == event.CompressRequestType || evt.Message.Content == "" {
			continue
		}
		mapped++
		if err := chain.Commit(ctx, replay.ChainFact{
			Key:     evt.Key,
			Chain:   chainKey,
			Type:    evt.Type,
			Summary: evt.Meta[event.MetaKeyEventSummary],
			Content: evt.Message.Content,
			Time:    evt.Timestamp,
		}); err != nil {
			t.Fatalf("commit the chain fact %s: %v", evt.Key, err)
		}
	}
	rebuilt := replay.NewReplayProjection()
	report, err := replay.RebuildProjectionFromWAL(ctx, rebuilt, chain, replay.NewMemoryRelations(), chainKey, replay.RebuildOptions{})
	if err != nil {
		t.Fatalf("rebuild the projection from the chain: %v", err)
	}
	return rebuilt, report, mapped
}

// projectionWindowPlan reports the fold plan the declaration's window reads, so
// the fold the commits triggered and the fold a test re-runs are judged under
// the same window and threshold.
func projectionWindowPlan(maxTokens int) replay.AgeFoldPlan {
	return replay.AgeFoldPlan{
		KeepRecent:      config.DefaultKeepRecentTasks,
		VolumeThreshold: (assembly.FoldWindow{MaxTokens: maxTokens}).VolumeThresholdBytes(),
	}
}

// sameKeys reports whether two key sequences are equal, position by position.
func sameKeys(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestEventIntegrityProjection drives the projection anchors over the production commit path:
// - small_window_fold_and_restore: a small window folds the process detail, repeated folds append no second product, and a cold start rebuilds the sequence
// - unconsumed_not_lost: an input and its reasoning no output closed stay whole in the projection
// - rebuild_does_not_refold: a cold start rebuild restores the chain and folds nothing
// - fold_product_no_self_trigger: a fold product is never folded a second time
// - actual_request_assembly: the formal request presents the history summary and the raw folded text stays retrievable by its key
// - rebuild_refolds_from_wal: the WAL alone carries the fold form, and the restored raw sequence re-folds into the same summary and band structure
// - the production fold files a compress request, never a compaction payload, so a rebuilt projection owes its fold form to the WAL (the G4 ruling)
func TestEventIntegrityProjection(t *testing.T) {
	t.Run("small_window_fold_and_restore", runSmallWindowFoldAndRestore)
	t.Run("rebuild_refolds_from_wal", runRebuildRefoldsFromWAL)
	t.Run("unconsumed_not_lost", runUnconsumedNotLost)
	t.Run("rebuild_does_not_refold", runRebuildDoesNotRefold)
	t.Run("restart_restores_fold_form", runRestartRestoresFoldForm)
	t.Run("fold_product_no_self_trigger", runFoldProductNoSelfTrigger)
	t.Run("actual_request_assembly", runActualRequestAssembly)
	t.Run("notice_visible_under_fold", runNoticeVisibleUnderFold)
}

// runSmallWindowFoldAndRestore is the positive anchor of the projection fold.
//   - the fold runs off the commits of a session whose agent declares a small window
//   - the folded projection drops the process detail of a closed turn and keeps its input and output
//   - every product the fold minted is filed on the chain as a compress request
//   - folding the same material again appends no second product
//   - a cold start rebuilds the canonical sequence byte for byte, and the fold a rebuild repeats recovers the same view
func runSmallWindowFoldAndRestore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	wide, wideAddr := newProjectionRun(t, 1<<16, &replayModel{name: "model-a"})
	for turn := 0; turn < 4; turn++ {
		commitProjectionTurn(t, ctx, wide, wideAddr, turn)
	}
	if products := projectionProductsOf(t, wide, wideAddr.SessionID); len(products) != 0 {
		t.Fatalf("the wide window folded %v, want the reference sequence whole", products)
	}
	canonical := replay.RenderProjection(projectionEntriesOf(t, wide, wideAddr.SessionID))

	folded, addr := newProjectionRun(t, projectionWindowTokens, &replayModel{name: "model-a"})
	for turn := 0; turn < 4; turn++ {
		commitProjectionTurn(t, ctx, folded, addr, turn)
	}
	entries := projectionEntriesOf(t, folded, addr.SessionID)
	products := projectionProductsOf(t, folded, addr.SessionID)
	if len(products) == 0 {
		t.Fatal("the committed turns minted no fold product in the projection")
	}
	if filed := projectionFactCountOf(folded, addr.SessionID, event.CompressRequestType); filed == 0 {
		t.Fatalf("the fold filed no compress request on the chain beside %d product(s)", len(products))
	}
	held := make(map[event.EventID]bool, len(entries))
	for _, entry := range entries {
		held[entry.Key] = true
	}
	for _, key := range []event.EventID{projectionKey(2, "1-plan"), projectionKey(2, "2-call")} {
		if held[key] {
			t.Fatalf("the folded projection still carries the process detail %s the fold consumed", key)
		}
	}
	for _, key := range []event.EventID{projectionKey(2, "0-in"), projectionKey(2, "3-out")} {
		if !held[key] {
			t.Fatalf("the folded projection dropped %s, want the input and output of a turn kept whole", key)
		}
	}
	original, err := folded.CommittedEvent(ctx, projectionKey(2, "1-plan"))
	if err != nil {
		t.Fatalf("read the folded original: %v", err)
	}
	if original.Message.Content != projectionBody(2, "1-plan") {
		t.Fatal("the folded original is no longer the text the chain committed")
	}

	rebuilt, report, mapped := rebuildProjectionFromRun(t, ctx, folded, addr.SessionID)
	if !report.Fallback || report.FullBoundary != "" {
		t.Fatalf("rebuild report = %+v, want the bounded fallback of a chain carrying no compaction", report)
	}
	if rebuilt.Compaction() != nil {
		t.Fatalf("the rebuild restored a compaction payload %+v the chain never carried", rebuilt.Compaction())
	}
	if got := replay.RenderProjection(rebuilt.Entries()); !bytes.Equal(got, canonical) {
		t.Fatalf("the cold start rebuilt %d bytes, want the canonical %d bytes the wide window recorded", len(got), len(canonical))
	}
	if rebuilt.Len() != mapped {
		t.Fatalf("the rebuild restored %d entries from %d mapped facts, want every mapped fact alone", rebuilt.Len(), mapped)
	}

	plan := projectionWindowPlan(projectionWindowTokens)
	first, err := rebuilt.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("fold the rebuilt projection: %v", err)
	}
	if !first.Triggered || first.NewFolded == 0 || len(first.Products) == 0 {
		t.Fatalf("fold report = %+v, want the fold the commits triggered", first)
	}
	recoveredEntries := rebuilt.Entries()
	recovered := projectionKeysOf(recoveredEntries)
	second, err := rebuilt.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("fold the rebuilt projection again: %v", err)
	}
	if second.NewFolded != 0 {
		t.Fatalf("fold report = %+v, want a second fold to consume no process detail", second)
	}
	if !sameKeys(projectionKeysOf(rebuilt.Entries()), recovered) {
		t.Fatalf("a second fold changed the recovered keys %v into %v", recovered, projectionKeysOf(rebuilt.Entries()))
	}
	if !sameKeys(projectionRawKeysOf(recoveredEntries), projectionRawKeysOf(entries)) {
		t.Fatalf("the recovered raw keys = %v, want the folded projection's own %v", projectionRawKeysOf(recoveredEntries), projectionRawKeysOf(entries))
	}
	if !sameKeys(projectionProductKeysOfType(recoveredEntries, replay.ProjectionTypeSegmentFold), projectionProductKeysOfType(entries, replay.ProjectionTypeSegmentFold)) {
		t.Fatalf("the recovered segment products = %v, want the folded projection's own %v",
			projectionProductKeysOfType(recoveredEntries, replay.ProjectionTypeSegmentFold), projectionProductKeysOfType(entries, replay.ProjectionTypeSegmentFold))
	}
	if summaries := projectionProductKeysOfType(recoveredEntries, replay.ProjectionTypeHistorySummary); len(summaries) != 1 {
		t.Fatalf("the recovered projection carries %d history summaries, want the one summary the fold mints", len(summaries))
	}
}

// runRebuildRefoldsFromWAL is the positive anchor of the G4 ruling: the event WAL alone carries enough material to fold the projection form again.
// - the production fold files every judgment as a compress request and never a compaction payload, so a rebuilt projection restores the raw sequence
// - the fold form is owed to the fold rule over the WAL, not to a snapshot
// - one pass over the restored sequence keeps the same band structure
// - the same raw keys stay whole and are consumed, and the same segment products are minted under the same content-addressed keys
// - the same rule replayed at the commit boundaries the commits recorded restores the online fold form byte for byte, summary and band structure alike
// - a fold raised over already-folded material reads the product it minted instead of folding that detail again
// - the re-fold is projection-derived: every judgment it re-derives is already on the chain, so a wired re-fold would file no second request
// - the rebuild and both re-folds leave the compress requests of the chain untouched
func runRebuildRefoldsFromWAL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	run, addr := newProjectionRun(t, projectionWindowTokens, &replayModel{name: "model-a"})
	for turn := 0; turn < 4; turn++ {
		commitProjectionTurn(t, ctx, run, addr, turn)
	}
	online := projectionEntriesOf(t, run, addr.SessionID)
	onlineProducts := projectionProductsEntriesOf(t, online)
	if len(onlineProducts) == 0 {
		t.Fatal("the commits minted no fold product, so there is no fold form to recover")
	}
	if summaries := projectionProductKeysOfType(online, replay.ProjectionTypeHistorySummary); len(summaries) != 1 {
		t.Fatalf("the online fold carries %d history summaries, want the one summary it reads", len(summaries))
	}
	onlineKept := projectionRawKeysOf(online)
	if _, held := projectionEntryOfKey(online, projectionKey(0, "1-plan")); held {
		t.Fatalf("the online projection still carries the process detail %s the fold should have consumed", projectionKey(0, "1-plan"))
	}
	if _, held := projectionEntryOfKey(online, projectionKey(3, "0-in")); !held {
		t.Fatalf("the online projection dropped the newest input %s the fold keeps whole", projectionKey(3, "0-in"))
	}
	if segments := projectionProductKeysOfType(online, replay.ProjectionTypeSegmentFold); len(segments) == 0 {
		t.Fatalf("the online fold carries no segment product beside %v, want the band products it minted", projectionProductsOf(t, run, addr.SessionID))
	}
	filed := projectionFactCountOf(run, addr.SessionID, event.CompressRequestType)
	if filed == 0 {
		t.Fatal("the online fold filed no compress request, so there is no judgment to recover")
	}
	judgments := projectionCompressFactsOf(run, addr.SessionID)
	t.Logf("rebuild_refolds_from_wal: online entries=%v products=%v requests=%d", projectionKeysOf(online), projectionProductsOf(t, run, addr.SessionID), filed)

	plan := projectionWindowPlan(projectionWindowTokens)
	facts := projectionCommitFactsOf(t, run, addr.SessionID)
	chainKey := replay.ChainKey{PartitionID: 1, SessionID: addr.SessionID}
	chain := replay.NewMemoryEventChain()
	committed := make([]string, 0, len(facts))
	for _, fact := range facts {
		committed = append(committed, string(fact.Key))
		if err := chain.Commit(ctx, fact); err != nil {
			t.Fatalf("commit the WAL fact %s: %v", fact.Key, err)
		}
	}
	rebuilt := replay.NewReplayProjection()
	report, err := replay.RebuildProjectionFromWAL(ctx, rebuilt, chain, replay.NewMemoryRelations(), chainKey, replay.RebuildOptions{})
	if err != nil {
		t.Fatalf("rebuild the projection from the WAL: %v", err)
	}
	if !report.Fallback || !report.Scanned || report.Entries != len(facts) || report.FullBoundary != "" {
		t.Fatalf("rebuild report = %+v, want the bounded fallback replaying the %d raw facts of a chain carrying no compaction", report, len(facts))
	}
	if rebuilt.Compaction() != nil {
		t.Fatalf("the rebuild restored the compaction payload %+v the chain never carried", rebuilt.Compaction())
	}
	if got := projectionKeysOf(rebuilt.Entries()); !sameKeys(got, committed) {
		t.Fatalf("the WAL restored %v, want the raw sequence %v the commits recorded", got, committed)
	}
	if products := projectionProductsEntriesOf(t, rebuilt.Entries()); len(products) != 0 {
		t.Fatalf("the WAL restored the products %v, want the raw sequence alone: the fold form is a form of the projection, not a fact of the chain", projectionKeysOf(products))
	}

	onePass, err := rebuilt.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("fold the rebuilt projection: %v", err)
	}
	if !onePass.Triggered || onePass.NewFolded == 0 || len(onePass.Products) == 0 {
		t.Fatalf("the fold of the rebuilt projection = %+v, want the fold the online commits triggered", onePass)
	}
	if got := projectionRawKeysOf(rebuilt.Entries()); !sameKeys(got, onlineKept) {
		t.Fatalf("the rebuilt fold keeps %v whole, want the online fold's own %v", got, onlineKept)
	}
	if got := projectionConsumedRawKeysOf(committed, rebuilt.Entries()); !sameKeys(got, projectionConsumedRawKeysOf(committed, online)) {
		t.Fatalf("the rebuilt fold consumes %v, want the online fold's own %v", got, projectionConsumedRawKeysOf(committed, online))
	}
	if got, want := projectionProductKeysOfType(rebuilt.Entries(), replay.ProjectionTypeSegmentFold), projectionProductKeysOfType(online, replay.ProjectionTypeSegmentFold); !sameKeys(got, want) {
		t.Fatalf("the rebuilt fold mints the segment products %v, want the online fold's own %v", got, want)
	}
	for _, key := range projectionProductKeysOfType(online, replay.ProjectionTypeSegmentFold) {
		want, _ := projectionEntryOfKey(online, event.EventID(key))
		got, held := projectionEntryOfKey(rebuilt.Entries(), event.EventID(key))
		if !held || got.Content != want.Content || got.Type != want.Type || !got.Time.Equal(want.Time) {
			t.Fatalf("the rebuilt segment product %s = %+v (found=%v), want the online fold's own %+v", key, got, held, want)
		}
	}
	onePassSummary := projectionProductKeysOfType(rebuilt.Entries(), replay.ProjectionTypeHistorySummary)
	if len(onePassSummary) != 1 {
		t.Fatalf("the rebuilt fold carries %d history summaries, want the one summary the band reads", len(onePassSummary))
	}
	t.Logf("rebuild_refolds_from_wal: one_pass summary online=%v rebuilt=%v", projectionProductKeysOfType(online, replay.ProjectionTypeHistorySummary), onePassSummary)

	refolded := replay.NewReplayProjection()
	var rejudged []event.CompressRequest
	for _, fact := range facts {
		refolded.Append(ctx, replay.ProjectionEntry{Key: fact.Key, Type: fact.Type, Summary: fact.Summary, Content: fact.Content, Time: fact.Time})
		fold, err := refolded.FoldByAge(ctx, plan)
		if err != nil {
			t.Fatalf("fold the WAL replay at %s: %v", fact.Key, err)
		}
		rejudged = append(rejudged, fold.Requests...)
	}
	if got, want := replay.RenderProjection(refolded.Entries()), replay.RenderProjection(online); !bytes.Equal(got, want) {
		t.Fatalf("the WAL re-fold restored %d bytes = %v, want the online fold's own %d bytes = %v", len(got), projectionKeysOf(refolded.Entries()), len(want), projectionKeysOf(online))
	}

	if len(rejudged) == 0 {
		t.Fatal("the WAL re-fold re-derived no judgment, so it proves no recoverable fold")
	}
	for _, request := range rejudged {
		if !judgments[request.ID] {
			t.Fatalf("the WAL re-fold minted the judgment %s the chain never recorded, want the judgment the online fold filed", request.ID)
		}
	}
	if after := projectionFactCountOf(run, addr.SessionID, event.CompressRequestType); after != filed {
		t.Fatalf("the rebuild and its re-folds changed the compress requests on the chain from %d to %d, want the chain untouched", filed, after)
	}

	for _, key := range []event.EventID{projectionKey(2, "0-in"), projectionKey(3, "3-out")} {
		entry, held := projectionEntryOfKey(refolded.Entries(), key)
		if !held || entry.Content != projectionBodyOfKey(key) {
			t.Fatalf("the re-folded projection carries %s = %+v (found=%v), want the kept material whole", key, entry, held)
		}
	}
	for _, key := range []event.EventID{projectionKey(0, "1-plan"), projectionKey(1, "2-call")} {
		original, err := run.CommittedEvent(ctx, key)
		if err != nil {
			t.Fatalf("read the folded original %s: %v", key, err)
		}
		if original.Message.Content != projectionBodyOfKey(key) {
			t.Fatalf("the folded original %s = %q, want the chain's %q", key, original.Message.Content, projectionBodyOfKey(key))
		}
	}
	t.Logf("rebuild_refolds_from_wal: restored=%d raw facts; re-folded %d entries=%d bytes identical to the online %d entries=%d bytes; products=%v judgments=%d",
		len(facts), refolded.Len(), len(replay.RenderProjection(refolded.Entries())), len(online), len(replay.RenderProjection(online)),
		projectionProductsEntriesOf(t, refolded.Entries()), len(rejudged))
}

// projectionConsumedRawKeysOf reports the raw keys of one projection's committed
// sequence that its fold consumed, in commit order.
func projectionConsumedRawKeysOf(committed []string, entries []replay.ProjectionEntry) []string {
	kept := make(map[string]bool, len(entries))
	for _, key := range projectionRawKeysOf(entries) {
		kept[key] = true
	}
	consumed := make([]string, 0, len(committed))
	for _, key := range committed {
		if !kept[key] {
			consumed = append(consumed, key)
		}
	}
	return consumed
}

// projectionCommitFactsOf reports the mapped facts of one session chain in
// commit order, in the shape a cold start replays the WAL under: a compress
// request carries no message and maps onto no projection entry.
func projectionCommitFactsOf(t *testing.T, run *assembly.Run, session event.SessionID) []replay.ChainFact {
	t.Helper()
	facts := make([]replay.ChainFact, 0, 16)
	for _, evt := range run.SessionFacts(event.Address{SessionID: session}) {
		if evt.Type == event.CompressRequestType || evt.Message.Content == "" {
			continue
		}
		facts = append(facts, replay.ChainFact{
			Key:     evt.Key,
			Chain:   replay.ChainKey{PartitionID: 1, SessionID: session},
			Type:    evt.Type,
			Summary: evt.Meta[event.MetaKeyEventSummary],
			Content: evt.Message.Content,
			Time:    evt.Timestamp,
		})
	}
	return facts
}

// projectionCompressFactsOf reports the identities of the compress requests one
// session chain holds: the judgments the fold filed, quoted by their
// content-addressed identity.
func projectionCompressFactsOf(run *assembly.Run, session event.SessionID) map[event.EventID]bool {
	keys := map[event.EventID]bool{}
	for _, evt := range run.SessionFacts(event.Address{SessionID: session}) {
		if evt.Type == event.CompressRequestType {
			keys[evt.Key] = true
		}
	}
	return keys
}

// runUnconsumedNotLost is the negative anchor of the unconsumed tail: an input
// and its reasoning no output closed stay whole in the projection, no product
// speaks them, and the original text stays on the chain; only the output that
// closes the turn lets the fold consume the process detail.
func runUnconsumedNotLost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run, addr := newProjectionRun(t, projectionWindowTokens, &replayModel{name: "model-a"})
	for turn := 0; turn < 2; turn++ {
		commitProjectionTurn(t, ctx, run, addr, turn)
	}

	commitProjectionLeg(t, ctx, run, addr, 2, "0-in", event.TypeExternalInput, "ask 02", projectionBody(2, "0-in"))
	commitProjectionLeg(t, ctx, run, addr, 2, "1-plan", event.TypeThinkingPlan, "plan 02", projectionBody(2, "1-plan"))

	entries := projectionEntriesOf(t, run, addr.SessionID)
	for _, key := range []event.EventID{projectionKey(2, "0-in"), projectionKey(2, "1-plan")} {
		entry, held := projectionEntryOfKey(entries, key)
		if !held {
			t.Fatalf("the projection dropped the unconsumed %s: %v", key, projectionKeysOf(entries))
		}
		if entry.Content != projectionBody(2, projectionLegPosition(key)) {
			t.Fatalf("the unconsumed %s carries %q, want the text the chain committed", key, entry.Content)
		}
	}
	for _, product := range projectionProductsEntries(t, run, addr.SessionID) {
		if strings.Contains(product.Content, "plan 02") || strings.Contains(product.Content, "call 02") {
			t.Fatalf("the fold product %s speaks the unconsumed tail: %q", product.Key, product.Content)
		}
	}
	original, err := run.CommittedEvent(ctx, projectionKey(2, "1-plan"))
	if err != nil {
		t.Fatalf("read the unconsumed original: %v", err)
	}
	if original.Message.Content != projectionBody(2, "1-plan") {
		t.Fatal("the unconsumed reasoning is no longer the text the chain committed")
	}

	commitProjectionLeg(t, ctx, run, addr, 2, "2-call", event.TypeActionCommand, "call 02", projectionBody(2, "2-call"))
	commitProjectionLeg(t, ctx, run, addr, 2, "3-out", event.TypeAgentOutput, "answer 02", projectionBody(2, "3-out"))
	entries = projectionEntriesOf(t, run, addr.SessionID)
	held := make(map[event.EventID]bool, len(entries))
	for _, entry := range entries {
		held[entry.Key] = true
	}
	for _, key := range []event.EventID{projectionKey(2, "1-plan"), projectionKey(2, "2-call")} {
		if held[key] {
			t.Fatalf("the closed tail still carries the process detail %s the fold should consume", key)
		}
	}
	for _, key := range []event.EventID{projectionKey(2, "0-in"), projectionKey(2, "3-out")} {
		if !held[key] {
			t.Fatalf("the closed tail dropped %s, want the input and output of the turn kept whole", key)
		}
	}
	folded := false
	for _, product := range projectionProductsEntries(t, run, addr.SessionID) {
		if strings.Contains(product.Content, "plan 02") {
			folded = true
		}
	}
	if !folded {
		t.Fatalf("no fold product speaks the closed tail: %v", projectionProductsOf(t, run, addr.SessionID))
	}
	t.Logf("unconsumed_not_lost: entries=%d products=%d", len(entries), len(projectionProductsOf(t, run, addr.SessionID)))
}

// runRebuildDoesNotRefold is the negative anchor of the cold-start rebuild: a
// chain that carries a compress request but no compaction payload replays
// every mapped fact, folds nothing, mints no product and files no second
// request, so the rebuilt sequence is the raw sequence alone.
func runRebuildDoesNotRefold(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run, addr := newProjectionRun(t, projectionWindowTokens, &replayModel{name: "model-a"})
	for turn := 0; turn < 4; turn++ {
		commitProjectionTurn(t, ctx, run, addr, turn)
	}
	if products := projectionProductsOf(t, run, addr.SessionID); len(products) == 0 {
		t.Fatal("the commits minted no fold product, so the rebuild has nothing to refuse")
	}
	filed := projectionFactCountOf(run, addr.SessionID, event.CompressRequestType)

	rebuilt, report, mapped := rebuildProjectionFromRun(t, ctx, run, addr.SessionID)
	if !report.Fallback || report.Entries != mapped || report.Dropped != 0 || !report.Scanned {
		t.Fatalf("rebuild report = %+v, want the bounded fallback replaying %d mapped facts whole", report, mapped)
	}
	if rebuilt.Compaction() != nil || rebuilt.FullBoundary() != "" {
		t.Fatalf("the rebuild restored compaction=%+v boundary=%q from a chain that carries none", rebuilt.Compaction(), rebuilt.FullBoundary())
	}
	expected := make([]string, 0, 16)
	for turn := 0; turn < 4; turn++ {
		for _, leg := range projectionTurnLegs(turn) {
			expected = append(expected, string(projectionKey(turn, leg.position)))
		}
	}
	if got := projectionKeysOf(rebuilt.Entries()); !sameKeys(got, expected) {
		t.Fatalf("the rebuild restored %v, want every mapped fact alone in commit order %v", got, expected)
	}
	for _, entry := range rebuilt.Entries() {
		if replay.SyntheticKey(entry.Key) {
			t.Fatalf("the rebuild minted the fold product %s, want a restore that folds nothing", entry.Key)
		}
	}
	if after := projectionFactCountOf(run, addr.SessionID, event.CompressRequestType); after != filed {
		t.Fatalf("the rebuild changed the compress requests on the chain from %d to %d, want the chain untouched", filed, after)
	}
	t.Logf("rebuild_does_not_refold: entries=%d mapped=%d requests=%d", rebuilt.Len(), mapped, filed)
}

// runRestartRestoresFoldForm is the restore anchor of the G4 ruling: a restart
// over the durable chain rebuilds the fold form the process left, and the replay
// files no judgment of its own.
// - the commits fold the projection under the declared window and file the judgments they read
// - a second assembly over the same storage replays the chain into the same fold products, key for key
// - the replay re-derives the judgments the chain holds and writes no second compress request
func runRestartRestoresFoldForm(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	storage := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
	declaration := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: projectionWindowTokens}},
	}
	agents := map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: &replayModel{name: "model-a"}},
	}
	run, err := assembly.Assemble(ctx, declaration, agents, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(storage))
	if err != nil {
		t.Fatalf("assemble the durable runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-projection"}
	for turn := 0; turn < 4; turn++ {
		commitProjectionTurn(t, ctx, run, addr, turn)
	}
	online := projectionEntriesOf(t, run, addr.SessionID)
	products := projectionProductsEntriesOf(t, online)
	if len(products) == 0 {
		t.Fatalf("the commits minted no fold product: %v", projectionKeysOf(online))
	}
	if summaries := projectionProductKeysOfType(online, replay.ProjectionTypeHistorySummary); len(summaries) != 1 {
		t.Fatalf("the online fold carries %d history summaries, want the one summary it reads: %v", len(summaries), projectionProductsOf(t, run, addr.SessionID))
	}
	filed := projectionFactCountOf(run, addr.SessionID, event.CompressRequestType)
	if filed == 0 {
		t.Fatal("the online fold filed no compress request, so the restart proves no judgment")
	}

	restarted, err := assembly.Assemble(ctx, declaration, agents, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(storage))
	if err != nil {
		t.Fatalf("restart over the same storage: %v", err)
	}
	restored := projectionEntriesOf(t, restarted, addr.SessionID)
	want := projectionKeysOf(products)
	got := projectionKeysOf(projectionProductsEntriesOf(t, restored))
	if !sameKeys(got, want) {
		t.Fatalf("the restart restored the fold products %v, want the %v the chain held: the fold form is owed to the rule over the same commit boundaries", got, want)
	}
	if after := projectionFactCountOf(restarted, addr.SessionID, event.CompressRequestType); after != 0 {
		t.Fatalf("the restart filed %d compress request(s), want none: the replay reads the judgments the chain holds and writes no second one", after)
	}
	t.Logf("restart_restores_fold_form: restored %v; the replay filed no judgment", got)
}

// runFoldProductNoSelfTrigger is the negative anchor of the product guard: a
// fold repeated over material it already folded mints no second product and
// consumes none of the products it minted, and a synthetic-keyed process entry
// is never folded even when it sits in a closed segment over the threshold.
func runFoldProductNoSelfTrigger(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run, addr := newProjectionRun(t, projectionWindowTokens, &replayModel{name: "model-a"})
	for turn := 0; turn < 4; turn++ {
		commitProjectionTurn(t, ctx, run, addr, turn)
	}
	if products := projectionProductsOf(t, run, addr.SessionID); len(products) == 0 {
		t.Fatal("the commits minted no fold product, so there is no product to rejudge")
	}

	rebuilt, _, _ := rebuildProjectionFromRun(t, ctx, run, addr.SessionID)
	plan := replay.AgeFoldPlan{KeepRecent: config.DefaultKeepRecentTasks, VolumeThreshold: 1}
	first, err := rebuilt.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("fold the rebuilt projection: %v", err)
	}
	if !first.Triggered || first.NewFolded == 0 || len(first.Products) == 0 {
		t.Fatalf("first fold report = %+v, want the fold the restored chain judged due", first)
	}
	minted := projectionProductsEntriesOf(t, rebuilt.Entries())
	keys := projectionKeysOf(rebuilt.Entries())
	second, err := rebuilt.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("fold the folded projection again: %v", err)
	}
	if !second.Triggered {
		t.Fatalf("second fold report = %+v, want the forced fold to run", second)
	}
	if second.NewFolded != 0 || second.MidFolded != 0 || len(second.Products) != 0 || len(second.Requests) != 0 {
		t.Fatalf("second fold report = %+v, want a fold that consumes and mints nothing", second)
	}
	if !sameKeys(projectionKeysOf(rebuilt.Entries()), keys) {
		t.Fatalf("the second fold changed the folded keys %v into %v", keys, projectionKeysOf(rebuilt.Entries()))
	}
	for _, product := range minted {
		entry, held := projectionEntryOfKey(rebuilt.Entries(), product.Key)
		if !held || entry.Content != product.Content || entry.Type != product.Type {
			t.Fatalf("the product %s = %+v (found=%v), want it kept as the fold minted it", product.Key, entry, held)
		}
	}

	guarded := replay.NewReplayProjection()
	guarded.Append(ctx, replay.ProjectionEntry{Key: "10-in", Type: event.TypeExternalInput, Summary: "ask 10", Content: strings.Repeat("正文 0-in 10。", 64), Time: projectionBase})
	guarded.Append(ctx, replay.ProjectionEntry{Key: "-seg-minted", Type: event.TypeThinkingPlan, Summary: "plan 10", Content: "the product of an earlier fold", Time: projectionBase})
	guarded.Append(ctx, replay.ProjectionEntry{Key: "10-out", Type: event.TypeAgentOutput, Summary: "answer 10", Content: "answer 10", Time: projectionBase})
	guardedReport, err := guarded.FoldByAge(ctx, replay.AgeFoldPlan{KeepRecent: 2, VolumeThreshold: 1})
	if err != nil {
		t.Fatalf("fold the synthetic-keyed segment: %v", err)
	}
	if !guardedReport.Triggered {
		t.Fatalf("synthetic fold report = %+v, want the forced fold to run", guardedReport)
	}
	if guardedReport.NewFolded != 0 || len(guardedReport.Products) != 0 {
		t.Fatalf("synthetic fold report = %+v, want the synthetic-keyed plan never folded", guardedReport)
	}
	if _, held := projectionEntryOfKey(guarded.Entries(), "-seg-minted"); !held {
		t.Fatal("the fold consumed the synthetic-keyed product it should have skipped")
	}
	t.Logf("fold_product_no_self_trigger: products=%d second=+%d", len(minted), second.NewFolded)
}

// runActualRequestAssembly is the consumer anchor of the projection fold: the
// formal request assembly presents the history summary the fold minted and the
// product of the newest band, the raw folded bodies stay off the request, and
// the chain still answers the original text of a folded key.
func runActualRequestAssembly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	provider := &replayModel{name: "model-a"}
	run, addr := newProjectionRun(t, 512, provider)
	for turn := 0; turn < 4; turn++ {
		commitProjectionTurn(t, ctx, run, addr, turn)
	}
	before := projectionEntriesOf(t, run, addr.SessionID)
	if len(before) == 0 {
		t.Fatal("the session registered no projection entries to present")
	}
	t.Logf("actual_request_assembly: before=%v", projectionKeysOf(before))

	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-projection",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "projection request"}},
	}); err != nil {
		t.Fatalf("invoke the folded session: %v", err)
	}
	requests := provider.recorded()
	if len(requests) != 1 {
		t.Fatalf("the model was driven with %d requests, want the one formal request", len(requests))
	}
	messages := requests[0].Messages
	for i, message := range messages {
		t.Logf("actual_request_assembly: message[%d] role=%s len=%d text=%.80q", i, message.Role, len(message.Content), message.Content)
	}
	if len(messages) < 2 {
		t.Fatalf("the request carries %d messages, want the presented projection beside the input", len(messages))
	}
	input := messages[len(messages)-1]
	if input.Role != model.RoleUser || input.Content != "projection request" {
		t.Fatalf("the last message = %+v, want the caller's input", input)
	}
	after := projectionEntriesOf(t, run, addr.SessionID)
	start := -1
	for i, entry := range after {
		if entry.Type == replay.ProjectionTypeHistorySummary && messages[0].Content == projectionPresentationOf(entry) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("the request opens with %q, want the history summary of the projection %v", messages[0].Content, projectionKeysOf(after))
	}
	presented := messages[:len(messages)-1]
	if start+len(presented) > len(after) {
		t.Fatalf("the request presents %d entries from position %d of a %d-entry projection", len(presented), start, len(after))
	}
	for i, message := range presented {
		entry := after[start+i]
		if message.Content != projectionPresentationOf(entry) {
			t.Fatalf("message %d = %q, want the presented entry %s = %q", i, message.Content, entry.Key, projectionPresentationOf(entry))
		}
		role, err := agent.RoleOf(entry.Type)
		if err != nil {
			t.Fatalf("the presented entry %s carries the unmapped type %q: %v", entry.Key, entry.Type, err)
		}
		if message.Role != role {
			t.Fatalf("message %d of %s carries role %q, want %q", i, entry.Key, message.Role, role)
		}
	}

	if !strings.HasPrefix(messages[0].Content, "history summary: ") {
		t.Fatalf("the first message = %q, want the history summary the fold minted", messages[0].Content)
	}
	for _, want := range []string{"external_input: ask 00", "segment_fold: segment fold", "agent_output: answer 02"} {
		if !strings.Contains(messages[0].Content, want) {
			t.Fatalf("the history summary %q carries no %q", messages[0].Content, want)
		}
	}
	product := false
	for _, message := range messages {
		if strings.Contains(message.Content, "folded segment process:") {
			product = true
		}
	}
	if !product {
		t.Fatalf("no presented message carries a segment-fold product: %v", messages)
	}
	if body := projectionBody(3, "0-in"); !containsMessage(messages, body, model.RoleUser) {
		t.Fatalf("the request presents no raw input of the newest turn %q as a user message", body)
	}
	if body := projectionBody(3, "3-out"); !containsMessage(messages, body, model.RoleAssistant) {
		t.Fatalf("the request presents no raw output of the newest turn %q as an assistant message", body)
	}

	for _, foldedKey := range []event.EventID{projectionKey(0, "1-plan"), projectionKey(0, "2-call"), projectionKey(2, "1-plan")} {
		body := projectionBodyOfKey(foldedKey)
		for _, message := range messages {
			if strings.Contains(message.Content, body) {
				t.Fatalf("the request re-carries the raw folded body of %s", foldedKey)
			}
		}
		original, err := run.CommittedEvent(ctx, foldedKey)
		if err != nil {
			t.Fatalf("read the folded original %s: %v", foldedKey, err)
		}
		if original.Message.Content != body {
			t.Fatalf("the folded original %s = %q, want the chain's %q", foldedKey, original.Message.Content, body)
		}
	}
	t.Logf("actual_request_assembly: messages=%d entries=%d requests=%d", len(messages), len(before), projectionFactCountOf(run, addr.SessionID, event.CompressRequestType))
}

// projectionEntryOfKey reports the entry one key names.
func projectionEntryOfKey(entries []replay.ProjectionEntry, key event.EventID) (replay.ProjectionEntry, bool) {
	for _, entry := range entries {
		if entry.Key == key {
			return entry, true
		}
	}
	return replay.ProjectionEntry{}, false
}

// projectionProductsEntries reports the product entries of one session's
// projection in projection order.
func projectionProductsEntries(t *testing.T, run *assembly.Run, session event.SessionID) []replay.ProjectionEntry {
	t.Helper()
	return projectionProductsEntriesOf(t, projectionEntriesOf(t, run, session))
}

// projectionProductsEntriesOf reports the product entries of one projection in
// projection order.
func projectionProductsEntriesOf(t *testing.T, entries []replay.ProjectionEntry) []replay.ProjectionEntry {
	t.Helper()
	products := make([]replay.ProjectionEntry, 0, 4)
	for _, entry := range entries {
		if replay.SyntheticKey(entry.Key) {
			products = append(products, entry)
		}
	}
	return products
}

// projectionPresentationOf mirrors the label the request assembly composes ahead
// of a history summary; every other entry is presented as the fold left it.
func projectionPresentationOf(entry replay.ProjectionEntry) string {
	if entry.Type == replay.ProjectionTypeHistorySummary && entry.Summary != "" {
		return entry.Summary + ": " + entry.Content
	}
	return entry.Content
}

// projectionRawKeysOf reports the keys of the entries the chain committed, in
// order: the products the fold minted are left out.
func projectionRawKeysOf(entries []replay.ProjectionEntry) []string {
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		if replay.SyntheticKey(entry.Key) {
			continue
		}
		keys = append(keys, string(entry.Key))
	}
	return keys
}

// projectionProductKeysOfType reports the keys of the product entries of one
// type, in projection order.
func projectionProductKeysOfType(entries []replay.ProjectionEntry, projectionType string) []string {
	keys := make([]string, 0, 4)
	for _, entry := range entries {
		if replay.SyntheticKey(entry.Key) && entry.Type == projectionType {
			keys = append(keys, string(entry.Key))
		}
	}
	return keys
}

// containsMessage reports whether one message of the given role carries the
// content.
func containsMessage(messages []model.Message, content string, role model.Role) bool {
	for _, message := range messages {
		if message.Role == role && message.Content == content {
			return true
		}
	}
	return false
}

// projectionLegPosition reports the position part of one projection key: the
// text after the turn prefix.
func projectionLegPosition(key event.EventID) string {
	if _, position, found := strings.Cut(string(key), "-"); found {
		return position
	}
	return ""
}

// projectionBodyOfKey reports the body one projection leg committed.
func projectionBodyOfKey(key event.EventID) string {
	text := string(key)
	turn := 0
	if _, err := fmt.Sscanf(text, "%d-", &turn); err != nil {
		return ""
	}
	return projectionBody(turn, projectionLegPosition(key))
}

// restartChildModeEnv, restartChildRootEnv, restartChildOutEnv and restartChildPhaseEnv name the parent-to-child channel; restartModeSeed and restartModeRecover name the phases; restartTaskCall, restartNoticeBody, restartAnswerBody, restartRecoverInput and restartInvocation name the seeded material.
//
// The restart anchors interrupt a seeded process at each time point one delivery obligation crosses, and boot a second process over the same durable root.
const (
	restartChildModeEnv  = "TAGENT_EVENT_RESTART_CHILD_MODE"
	restartChildRootEnv  = "TAGENT_EVENT_RESTART_CHILD_ROOT"
	restartChildOutEnv   = "TAGENT_EVENT_RESTART_CHILD_OUT"
	restartChildPhaseEnv = "TAGENT_EVENT_RESTART_CHILD_PHASE"

	restartModeSeed    = "seed"
	restartModeRecover = "recover"

	restartTaskCall     = "call-restart"
	restartNoticeBody   = "restarted result"
	restartAnswerBody   = "restarted answer"
	restartRecoverInput = "what do you still owe?"
	restartInvocation   = event.InvocationID("inv-restart")

	// restartDrainWindow bounds the drain of the restarted run: a duty frozen
	// to the interrupted process stays open, so the close is asked to report
	// what it could not settle instead of waiting without a deadline.
	restartDrainWindow = 2 * time.Second
)

// restartSeedAddress, restartInstance and restartNoticeEvent are the one address the restart anchors seed and recover.
var (
	restartSeedAddress = event.Address{AgentID: "agent-a", SessionID: "session-restart"}
	restartInstance    = event.InstanceID("inst-restart")
	restartNoticeEvent = event.EventID("evt-restart-settled")
)

// restartPhase names one interruption point of a delivery obligation.
type restartPhase string

const (
	restartBeforeCommit       restartPhase = "before_commit"
	restartAfterCommit        restartPhase = "after_commit"
	restartAfterClaim         restartPhase = "after_claim"
	restartBeforeConfirmation restartPhase = "before_confirmation"
	restartAfterConfirmation  restartPhase = "after_confirmation"
)

// TestEventIntegrityRestart drives the restart anchors over the five interruption points of one delivery obligation:
// - the positive reading of every interruption point
// - the negative readings that must fail when the restart mistakes a boundary
// - the consumer reading of the first actual request and send of the restarted process
// - a phase is seeded and recovered once, so a negative reading never re-interrupts a process
func TestEventIntegrityRestart(t *testing.T) {
	if mode := os.Getenv(restartChildModeEnv); mode != "" {
		runRestartChild(t, mode)
		return
	}
	cases := map[restartPhase]restartOutcome{}
	outcome := func(t *testing.T, phase restartPhase) restartOutcome {
		t.Helper()
		if held, ok := cases[phase]; ok {
			return held
		}
		held := restartInterrupt(t, phase)
		cases[phase] = held
		return held
	}
	t.Run("positive", func(t *testing.T) {
		t.Run("before_commit", func(t *testing.T) { restartAssertNothingCommitted(t, outcome(t, restartBeforeCommit)) })
		t.Run("after_commit", func(t *testing.T) { restartAssertOwed(t, outcome(t, restartAfterCommit)) })
		t.Run("after_claim", func(t *testing.T) { restartAssertOwed(t, outcome(t, restartAfterClaim)) })
		t.Run("before_confirmation", func(t *testing.T) { restartAssertOwed(t, outcome(t, restartBeforeConfirmation)) })
		t.Run("after_confirmation", func(t *testing.T) { restartAssertConfirmed(t, outcome(t, restartAfterConfirmation)) })
	})
	t.Run("negative", func(t *testing.T) {
		t.Run("confirmed_not_resent", func(t *testing.T) {
			restartAssertConfirmedNotResent(t, outcome(t, restartAfterConfirmation))
		})
		t.Run("unconfirmed_not_lost", func(t *testing.T) {
			restartAssertUnconfirmedNotLost(t, outcome(t, restartBeforeConfirmation))
		})
		t.Run("cross_boundary_not_misjudged", func(t *testing.T) {
			restartAssertCrossBoundary(t, outcome(t, restartAfterClaim), outcome(t, restartAfterConfirmation))
		})
	})
	t.Run("consumer", func(t *testing.T) { restartAssertFirstRequest(t, outcome) })
}

// restartOutcome is one phase as the two processes reported it: the seed report
// the interrupted process landed beside the material it reached, and the report
// the restarted process produced from the same durable root alone.
type restartOutcome struct {
	Phase   restartPhase
	Seed    restartChildReport
	Recover restartChildReport
}

// restartChildReport is the report one child phase lands: what the seed reached
// before the interruption, or what the restart rebuilt, requested, sent,
// drained and read at the durable boundary.
type restartChildReport struct {
	Ready      bool                      `json:"ready"`
	Mode       string                    `json:"mode"`
	Phase      string                    `json:"phase"`
	PID        int                       `json:"pid"`
	Gate       restartGateView           `json:"gate"`
	Recovery   []consumptionRecoveryView `json:"recovery"`
	Tasks      []consumptionTaskView     `json:"tasks"`
	Notices    []consumptionNoticeView   `json:"notices"`
	Mailbox    []consumptionMailboxView  `json:"mailbox"`
	Requests   int                       `json:"requests"`
	Prompt     string                    `json:"prompt"`
	Sends      []string                  `json:"sends"`
	Drain      restartDrainView          `json:"drain"`
	Settles    int                       `json:"settles"`
	SettleBody string                    `json:"settle_body"`
	Boundary   restartBoundaryView       `json:"boundary"`
	Notes      []string                  `json:"notes,omitempty"`
}

// restartGateView names the write barrier the seed stood behind when the parent
// interrupted it, and whether that barrier was really entered.
type restartGateView struct {
	Barrier string `json:"barrier"`
	Entered bool   `json:"entered"`
}

// restartDrainView is the drain outcome of the restarted run.
type restartDrainView struct {
	Drained   bool     `json:"drained"`
	Unsettled []string `json:"unsettled,omitempty"`
}

// restartBoundaryView is what a third opener of the durable root reads after the
// restart closed: the tasks and owed notifications the log alone still holds,
// and the acceptance the carrier answers for the seeded notification.
type restartBoundaryView struct {
	Tasks     int    `json:"tasks"`
	Owed      int    `json:"owed"`
	Accept    string `json:"accept,omitempty"`
	AcceptErr string `json:"accept_err,omitempty"`
}

// restartInterrupt seeds one phase over a fresh durable root, interrupts the
// seed, and recovers the same root in a second process.
func restartInterrupt(t *testing.T, phase restartPhase) restartOutcome {
	t.Helper()
	root := t.TempDir()
	seed := runRestartSeed(t, root, phase)
	recovered := runRestartRecover(t, root, phase)
	t.Logf("%s: seed pid=%d gate=%+v mailbox=%+v", phase, seed.PID, seed.Gate, seed.Mailbox)
	t.Logf("%s: restart pid=%d recovered=%d notices=%d requests=%d sends=%d settles=%d drain=%+v boundary=%+v",
		phase, recovered.PID, len(recovered.Recovery), len(recovered.Notices), recovered.Requests,
		len(recovered.Sends), recovered.Settles, recovered.Drain, recovered.Boundary)
	return restartOutcome{Phase: phase, Seed: seed, Recover: recovered}
}

// runRestartChild dispatches one child phase of the restart anchors.
func runRestartChild(t *testing.T, mode string) {
	t.Helper()
	root := os.Getenv(restartChildRootEnv)
	out := os.Getenv(restartChildOutEnv)
	phase := restartPhase(os.Getenv(restartChildPhaseEnv))
	if root == "" || out == "" || phase == "" {
		t.Fatalf("the restart child was handed root=%q out=%q phase=%q, want all three named", root, out, phase)
	}
	switch mode {
	case restartModeSeed:
		seedRestartProcess(t, root, out, phase)
	case restartModeRecover:
		recoverRestartProcess(t, root, out, phase)
	default:
		t.Fatalf("the restart child was handed the unknown mode %q", mode)
	}
}

// restartGate withholds one write barrier of the real durable log, so the seed
// process can be interrupted exactly when the record stands staged and its
// barrier has not returned: the parent kills it there, and the log keeps
// nothing of the record a later process could read.
type restartGate struct {
	state.FactLog
	barrier string
	watch   string
	marker  string
	mu      sync.Mutex
	staged  bool
	entered chan struct{}
	hold    chan struct{}
	once    sync.Once
}

// newRestartGate returns the gate of one phase, withholding the barrier that
// phase must not cross.
func newRestartGate(log state.FactLog, phase restartPhase) *restartGate {
	gate := &restartGate{FactLog: log, entered: make(chan struct{}), hold: make(chan struct{})}
	switch phase {
	case restartBeforeCommit:
		gate.barrier = "settle-commit"
		gate.watch = "tagent/task/v1/fact:"
		gate.marker = string(restartNoticeEvent)
	case restartBeforeConfirmation:
		gate.barrier = "consumption-receipt"
		gate.watch = "tagent/task/v1/consume:"
	}
	return gate
}

// matches reports whether one staged write is the record this gate withholds.
func (g *restartGate) matches(key string, value []byte) bool {
	if g.watch == "" || !strings.HasPrefix(key, g.watch) {
		return false
	}
	return g.marker == "" || strings.Contains(string(value), g.marker)
}

// Put withholds the write of the watched record: the localfile tier publishes a
// key at the write itself, so the record must never reach the underlying log.
// The gate signals that the writer stands at the withheld write and never
// returns, so the process is killed before anything of that record lands.
func (g *restartGate) Put(ctx context.Context, key string, value []byte) error {
	if g.matches(key, value) {
		g.mu.Lock()
		g.staged = true
		g.mu.Unlock()
		g.once.Do(func() { close(g.entered) })
		<-g.hold
		return nil
	}
	return g.FactLog.Put(ctx, key, value)
}

// Sync delegates: the watched record never reaches the barrier because its
// write is withheld, and every other record publishes exactly as the tier does.
func (g *restartGate) Sync(ctx context.Context) error {
	return g.FactLog.Sync(ctx)
}

// await waits until the watched record stands staged behind its withheld
// barrier.
func (g *restartGate) await(t *testing.T) {
	t.Helper()
	if g.barrier == "" {
		t.Fatalf("the seed was handed a phase with no withheld barrier")
	}
	select {
	case <-g.entered:
	case <-time.After(60 * time.Second):
		t.Fatalf("the %s barrier of the seed was never staged", g.barrier)
	}
}

// view reports the barrier the seed stood behind.
func (g *restartGate) view() restartGateView {
	return restartGateView{Barrier: g.barrier, Entered: g.barrier != ""}
}

// seedRestartProcess commits one task's chain up to the phase's interruption
// point, reports the material it reached, and waits for the parent's kill: it
// never closes, drains or hands anything on.
func seedRestartProcess(t *testing.T, root, out string, phase restartPhase) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report := restartChildReport{Mode: restartModeSeed, Phase: string(phase), PID: os.Getpid()}

	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("open the seed tier: %v", err)
	}
	gate := newRestartGate(ledger.TierFactLog(tier), phase)
	durable, err := state.NewDurableCommit(gate, nil)
	if err != nil {
		t.Fatalf("build the seed commit: %v", err)
	}
	registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
	spec := restartSpec()
	receipt, err := registry.Register(ctx, spec)
	if err != nil {
		t.Fatalf("register the restart task: %v", err)
	}
	notice := restartNotice(receipt.TaskID)
	envelope := restartEnvelope(spec, notice)

	switch phase {
	case restartBeforeCommit:
		pending := make(chan error, 1)
		go func() {
			_, err := registry.Advance(ctx, receipt.TaskID, task.StatusSettled, restartMutation(receipt.TaskID), []task.Envelope{envelope})
			pending <- err
		}()
		gate.await(t)
		report.Gate = gate.view()
		report.Notes = append(report.Notes, "the settle record stands staged behind its withheld barrier; the process is interrupted before the commit lands")
	case restartAfterCommit:
		if err := restartSettle(ctx, registry, envelope); err != nil {
			t.Fatalf("settle the restart task: %v", err)
		}
		report.Notes = append(report.Notes, "the settle commit returned before the interruption and nothing delivered its notification")
	case restartAfterClaim, restartBeforeConfirmation, restartAfterConfirmation:
		if err := restartSettle(ctx, registry, envelope); err != nil {
			t.Fatalf("settle the restart task: %v", err)
		}
		site := newRestartSite(t, spec.Source, spec.InstanceID, durable)
		site.accept(t, notice)
		seq := site.seqOf(t, notice)
		if err := site.store.Claim(ctx, spec.Source, spec.InstanceID, seq); err != nil {
			t.Fatalf("claim the restart notification: %v", err)
		}
		report.Mailbox = []consumptionMailboxView{consumptionMailboxViewOf(site.session(t))}
		switch phase {
		case restartBeforeConfirmation:
			pending := make(chan error, 1)
			go func() { pending <- site.store.Consume(ctx, spec.Source, spec.InstanceID, seq) }()
			gate.await(t)
			report.Gate = gate.view()
			report.Notes = append(report.Notes, "the consumption receipt stands staged behind its withheld barrier; the process is interrupted before the confirmation")
		case restartAfterConfirmation:
			if err := site.store.Consume(ctx, spec.Source, spec.InstanceID, seq); err != nil {
				t.Fatalf("consume the restart notification: %v", err)
			}
			report.Mailbox = []consumptionMailboxView{consumptionMailboxViewOf(site.session(t))}
			report.Notes = append(report.Notes, "the consumption receipt returned before the interruption")
		}
	default:
		t.Fatalf("the restart seed was handed the unknown phase %q", phase)
	}
	writeRestartReport(t, out, report)
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Minute):
	}
}

// recoverRestartProcess boots the formal host over the same durable root, drives
// the first actual request of the restart beside the face it sends through,
// drains, and reads the durable boundary a third opener sees: every reading is
// this process's own, never a mirror of the seed's memory.
func recoverRestartProcess(t *testing.T, root, out string, phase restartPhase) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report := restartChildReport{Mode: restartModeRecover, Phase: string(phase), PID: os.Getpid()}
	provider := newRestartProvider(t)
	sender := &restartSender{}
	configPath := filepath.Join(t.TempDir(), "host.yaml")
	if err := os.WriteFile(configPath, []byte(restartHostDocument(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the restart host document: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the restarted host: %v", err)
	}
	run := host.Run()
	addresses := []event.Address{restartSeedAddress}
	report.Recovery = consumptionRecoveryViews(run.Recovery())
	report.Tasks = consumptionTaskViews(t, ctx, run, addresses)
	report.Notices = consumptionNoticeViews(t, ctx, run, addresses)
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-restart-recover",
		Address:      restartSeedAddress,
		Input:        []model.Message{{Role: model.RoleUser, Content: restartRecoverInput}},
	}); err != nil {
		t.Fatalf("the restarted process refused its first invocation: %v", err)
	}
	report.Requests = provider.count()
	report.Prompt = provider.firstPrompt()
	report.Sends = sender.bodies()
	drain, err := run.Close(ctx, agent.CloseRequest{Deadline: time.Now().Add(restartDrainWindow)})
	if err != nil && !errors.Is(err, agent.ErrDrainUnsettled) {
		t.Fatalf("the restarted run refused its drain: %v", err)
	}
	report.Drain = restartDrainView{Drained: drain.Drained}
	for _, unsettled := range drain.Unsettled {
		report.Drain.Unsettled = append(report.Drain.Unsettled, string(unsettled.TaskID)+": "+unsettled.Reason)
	}
	for _, fact := range run.SessionFacts(restartSeedAddress) {
		if fact.Type != event.TypeTaskSettled {
			continue
		}
		report.Settles++
		report.SettleBody += fact.Message.Content
	}
	report.Boundary = restartBoundary(t, root, report)
	writeRestartReport(t, out, report)
}

// restartProvider is the scripted model endpoint of the restarted process: it
// records every request the drive really sent and answers with one final body,
// so a check reads the request assembly instead of a bookkeeping mirror.
type restartProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	bodies []map[string]json.RawMessage
}

// newRestartProvider starts the endpoint and closes it with the test.
func newRestartProvider(t *testing.T) *restartProvider {
	t.Helper()
	provider := &restartProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// serve answers one chat completion and records the request it carried.
func (p *restartProvider) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body map[string]json.RawMessage
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	p.mu.Lock()
	p.bodies = append(p.bodies, body)
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":"scripted","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, restartAnswerBody)
}

// endpoint reports the API root the host document names.
func (p *restartProvider) endpoint() string { return p.server.URL + "/v1" }

// count reports how many requests the drive really sent.
func (p *restartProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.bodies)
}

// firstPrompt reports the last user message the first request carried, so a
// check reads the input the restart itself drove instead of a replayed one.
func (p *restartProvider) firstPrompt() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, body := range p.bodies {
		raw, held := body["messages"]
		if !held {
			continue
		}
		var messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(raw, &messages) != nil {
			continue
		}
		prompt := ""
		for _, message := range messages {
			if message.Role == string(model.RoleUser) {
				prompt = message.Content
			}
		}
		return prompt
	}
	return ""
}

// restartSender is the outbound face of the restarted host: every accepted body
// is recorded, so the check reads what the restart really sent.
type restartSender struct {
	mu      sync.Mutex
	targets []event.Address
	sent    []string
}

// Send records one delivered body beside its target address.
func (s *restartSender) Send(_ context.Context, target event.Address, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets = append(s.targets, target)
	s.sent = append(s.sent, content)
	return nil
}

// bodies reports the delivered bodies as a copy the caller owns.
func (s *restartSender) bodies() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sent...)
}

// restartHostDocument renders the host document of the restarted process: the
// entry agent, the scripted provider, and the durable root the seed left. The
// document is the one the formal Boot entry reads, so the restart opens through
// the same assembly the product opens through.
func restartHostDocument(root, endpoint string) string {
	return fmt.Sprintf("entry: agent-a\nprovider: local\nmodel: model-a\nproviders:\n  local: { provider: openai, api_endpoint: %q, api_key_env: TAGENT_TEST_API_KEY }\nagents:\n  agent-a: {}\nstorage: { type: localfile, root: %q }\n", endpoint, root)
}

// restartBoundary reads the durable root after the restarted host closed, so the
// boundary is decided by the log alone and not by anything either process held
// in memory.
func restartBoundary(t *testing.T, root string, report restartChildReport) restartBoundaryView {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("open the durable root after the restart closed: %v", err)
	}
	probe, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("read the durable boundary after the restart closed: %v", err)
	}
	view := restartBoundaryView{Tasks: len(probe.TaskIDs())}
	for _, id := range probe.TaskIDs() {
		view.Owed += len(probe.Outbox(id))
	}
	if len(report.Recovery) == 1 {
		notice := task.Notice{TaskID: event.TaskID(report.Recovery[0].TaskID), Round: 1, Kind: task.NoticeSettled, Seq: 0}
		acceptance, err := probe.Accept(ctx, notice)
		view.Accept = restartDeliveryName(acceptance)
		if err != nil {
			view.AcceptErr = err.Error()
		}
	}
	return view
}

// restartDeliveryName names one delivery acceptance.
func restartDeliveryName(acceptance task.DeliveryAcceptance) string {
	if acceptance == task.DeliveryAccepted {
		return "accepted"
	}
	return "pending"
}

// restartSpec builds the task declaration the seed commits: one serialized task
// whose owner is the address its notification is routed to.
func restartSpec() task.Spec {
	return task.Spec{
		CallID:       restartTaskCall,
		DedupKey:     task.DedupKey("inst-restart\x00inv-restart\x00dec-restart\x00" + restartTaskCall),
		InvocationID: restartInvocation,
		InstanceID:   restartInstance,
		Description:  "restart",
		Source:       restartSeedAddress,
		Execution:    "serialized",
	}
}

// restartNotice freezes one settled notification of the task.
func restartNotice(taskID event.TaskID) task.Notice {
	return task.Notice{TaskID: taskID, Round: 1, Kind: task.NoticeSettled, Seq: 0, Event: restartNoticeEvent, Text: restartNoticeBody}
}

// restartEnvelope freezes the routing the notification carries.
func restartEnvelope(spec task.Spec, notice task.Notice) task.Envelope {
	return task.Envelope{Notice: notice, Target: restartSeedAddress, Source: restartSeedAddress, TargetInstance: spec.InstanceID}
}

// restartMutation reports the mutation identity of the settle the seed commits.
func restartMutation(taskID event.TaskID) task.MutationID {
	return task.MutationID("settle:" + string(taskID))
}

// restartSettle commits the terminal transition of the seeded task beside its
// notification's outbox.
func restartSettle(ctx context.Context, registry *task.MemoryRegistry, envelope task.Envelope) error {
	notice := envelope.Notice
	result, err := registry.Advance(ctx, notice.TaskID, task.StatusSettled, restartMutation(notice.TaskID), []task.Envelope{envelope})
	if err != nil {
		return err
	}
	if result != task.CommitCommitted {
		return fmt.Errorf("the settle of %s committed as %v", notice.TaskID, result)
	}
	return nil
}

// newRestartSite establishes the mailbox site of one seeded address beside the
// durable carrier the run's own store is built with, so the claim and the
// consumption receipt of the seed land exactly as the production face lands
// them.
func newRestartSite(t *testing.T, addr event.Address, instance event.InstanceID, durable *state.DurableCommit) *consumptionSite {
	t.Helper()
	ctx := context.Background()
	authority := state.NewAuthority()
	subject := state.Subject("event-integrity-restart")
	if err := authority.GrantRead(ctx, subject, addr); err != nil {
		t.Fatalf("grant the restart mailbox read: %v", err)
	}
	store := state.NewMemoryStore(authority, durable)
	if _, err := store.Establish(ctx, addr, instance); err != nil {
		t.Fatalf("establish the restart mailbox site: %v", err)
	}
	return &consumptionSite{addr: addr, instance: instance, authority: authority, subject: subject, store: store}
}

// runRestartSeed starts the seed child, waits for its ready report and
// interrupts it, so the restart reads only what the durable tier kept.
func runRestartSeed(t *testing.T, root string, phase restartPhase) restartChildReport {
	t.Helper()
	out := filepath.Join(t.TempDir(), "seed.json")
	cmd := restartChildCommand(restartModeSeed, root, out, phase)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the %s seed process: %v", phase, err)
	}
	report := waitRestartReport(t, out, &output, restartModeSeed, phase)
	t.Logf("parent: interrupt the %s seed pid=%d", phase, cmd.Process.Pid)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("interrupt the %s seed process: %v", phase, err)
	}
	err := cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the interrupted %s seed reported %v, want the signalled exit the parent caused; output=%s", phase, err, output.String())
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		t.Fatalf("the %s seed ended %v, want the parent's kill; output=%s", phase, err, output.String())
	}
	return report
}

// runRestartRecover starts the recover child over the same durable root and
// reads the report the restart itself produced.
func runRestartRecover(t *testing.T, root string, phase restartPhase) restartChildReport {
	t.Helper()
	out := filepath.Join(t.TempDir(), "recover.json")
	cmd := restartChildCommand(restartModeRecover, root, out, phase)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("the %s restart process reported %v; output=%s", phase, err, output.String())
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the %s restart report: %v; output=%s", phase, err, output.String())
	}
	var report restartChildReport
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("decode the %s restart report: %v; output=%s", phase, err, output.String())
	}
	if !report.Ready || report.Mode != restartModeRecover || report.Phase != string(phase) {
		t.Fatalf("%s restart report = %+v, want a ready report of its own phase", phase, report)
	}
	return report
}

// restartChildCommand builds the self-exec command of one child phase over the
// report it lands.
func restartChildCommand(mode, root, out string, phase restartPhase) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestEventIntegrityRestart$", "-test.timeout=10m")
	cmd.Env = append(os.Environ(),
		restartChildModeEnv+"="+mode,
		restartChildRootEnv+"="+root,
		restartChildOutEnv+"="+out,
		restartChildPhaseEnv+"="+string(phase),
		"TAGENT_TEST_API_KEY=fixture",
		"TAGENT_HTTP_PORT=0",
	)
	return cmd
}

// waitRestartReport waits for the child's atomic report.
func waitRestartReport(t *testing.T, path string, output *bytes.Buffer, mode string, phase restartPhase) restartChildReport {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		body, err := os.ReadFile(path)
		if err == nil {
			var report restartChildReport
			if json.Unmarshal(body, &report) == nil && report.Ready && report.Mode == mode && report.Phase == string(phase) {
				return report
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the %s %s process reported no ready report at %s; output=%s", phase, mode, path, output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// writeRestartReport lands one child report atomically, so the parent never
// reads a half-written one.
func writeRestartReport(t *testing.T, path string, report restartChildReport) {
	t.Helper()
	report.Ready = true
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("encode the restart child report: %v", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		t.Fatalf("write the restart child report: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("land the restart child report: %v", err)
	}
}

// restartAssertNothingCommitted proves an interruption before the settle commit
// leaves the admitted task behind and only the admitted task: the restart
// rebuilds it as accepted beside the awaited execution, reads no notification
// from the commit that never landed, and owes no delivery, while it still drives
// its first actual request and send.
func restartAssertNothingCommitted(t *testing.T, o restartOutcome) {
	t.Helper()
	if !o.Seed.Gate.Entered {
		t.Fatalf("the %s seed was interrupted outside the barrier it was meant to stand behind: gate=%+v", o.Phase, o.Seed.Gate)
	}
	taskID, ok := restartSoleTask(o.Recover)
	if !ok {
		t.Fatalf("the restart rebuilt %+v, want the admitted task recovered from the admission material alone", o.Recover.Recovery)
	}
	if status := o.Recover.Recovery[0].Status; status != int(task.StatusAccepted) {
		t.Fatalf("the %s restart rebuilt the task in status %d, want the accepted task whose settle commit never landed", o.Phase, status)
	}
	if len(o.Recover.Notices) != 0 {
		t.Fatalf("the restart reads %+v, want no notification from a commit that never landed", o.Recover.Notices)
	}
	if o.Recover.Boundary.Tasks != 1 || o.Recover.Boundary.Owed != 0 {
		t.Fatalf("the durable root holds tasks=%d owed=%d after an interruption before the commit, want the admitted task beside no owed notification", o.Recover.Boundary.Tasks, o.Recover.Boundary.Owed)
	}
	if o.Recover.Settles != 0 {
		t.Fatalf("the restart committed %d settle facts for a task whose settle commit never landed, want none", o.Recover.Settles)
	}
	if o.Recover.Drain.Drained || len(o.Recover.Drain.Unsettled) != 1 {
		t.Fatalf("the %s restart drain = %+v, want the admitted task's execution reported open", o.Phase, o.Recover.Drain)
	}
	if open := o.Recover.Drain.Unsettled[0]; !strings.Contains(open, taskID) || !strings.Contains(open, string(task.ObligationAwaitingTool)) {
		t.Fatalf("the %s restart reports %q open, want the awaited execution of the task %s kept visible", o.Phase, open, taskID)
	}
}

// restartAssertOwed proves an interruption after the commit, after the claim or
// before the confirmation leaves the notification owed or really delivered, and
// never silently dropped. The restart rebuilds the task and keeps the duty
// honest in one of the two ways the contract admits: the unconsumed notice
// stays owed beside the durable boundary that still holds it (the mailbox
// branch, because an unconsumed notice is kept across the restart until a wake
// claims it), or it really delivered the duty and left the receipt the boundary
// reads. Both branches read the restart's own first actual request and send.
func restartAssertOwed(t *testing.T, o restartOutcome) {
	t.Helper()
	taskID, ok := restartSoleTask(o.Recover)
	if !ok {
		t.Fatalf("the %s restart recovered %+v, want exactly one rebuilt task", o.Phase, o.Recover.Recovery)
	}
	switch o.Recover.Boundary.Owed {
	case 1:
		if len(o.Recover.Notices) != 1 || o.Recover.Notices[0].TaskID != taskID {
			t.Fatalf("the %s restart reads %+v, want the one unconsumed notice of the task %s kept owed across the restart", o.Phase, o.Recover.Notices, taskID)
		}
		if phase := o.Recover.Notices[0].Phase; phase == string(query.NoticePhaseConsumed) {
			t.Fatalf("the %s restart reads the notification of %s as %q, want an unconsumed notice kept owed", o.Phase, taskID, phase)
		}
		if restartNoticeDelivered(o.Recover) {
			t.Fatalf("the %s restart delivered %q beside an owed unconsumed notice, want no delivery without a wake claiming it", o.Phase, o.Recover.Sends)
		}
		if o.Recover.Boundary.Accept != "accepted" || o.Recover.Boundary.AcceptErr != "" {
			t.Fatalf("the durable boundary answered the seeded notification %q err=%q, want its delivery still open to acceptance", o.Recover.Boundary.Accept, o.Recover.Boundary.AcceptErr)
		}
	case 0:
		if !o.Recover.Drain.Drained || len(o.Recover.Drain.Unsettled) != 0 {
			t.Fatalf("the %s restart drain = %+v, want a drained run beside the delivered duty", o.Phase, o.Recover.Drain)
		}
		if !restartNoticeDelivered(o.Recover) {
			t.Fatalf("the %s restart sent %q, want the one delivery of the recovered notification carrying %q", o.Phase, o.Recover.Sends, restartNoticeBody)
		}
		if !strings.Contains(o.Recover.Boundary.AcceptErr, "no delivery obligation") {
			t.Fatalf("the durable boundary answered the delivered notification %q err=%q, want its receipt to close the delivery", o.Recover.Boundary.Accept, o.Recover.Boundary.AcceptErr)
		}
	default:
		t.Fatalf("the durable boundary owes %d notifications, want the unconfirmed notification kept owed or really delivered", o.Recover.Boundary.Owed)
	}
}

// restartNoticeDelivered reports whether the restart really delivered the
// seeded notification body through its own sender face, so a delivery is read
// from the face that sends and never from a bookkeeping mirror.
func restartNoticeDelivered(recovered restartChildReport) bool {
	for _, body := range recovered.Sends {
		if strings.Contains(body, restartNoticeBody) {
			return true
		}
	}
	return false
}

// restartAssertConfirmed proves an interruption after the confirmation leaves a
// task that rebuilds but owes nothing: the restart reads no notification,
// commits no delivery fact, and the durable boundary already holds the receipt.
func restartAssertConfirmed(t *testing.T, o restartOutcome) {
	t.Helper()
	taskID, ok := restartSoleTask(o.Recover)
	if !ok {
		t.Fatalf("the restart recovered %+v, want the confirmed task rebuilt from its chain", o.Recover.Recovery)
	}
	if len(o.Recover.Notices) != 0 {
		t.Fatalf("the restart reads the notices %+v, want a confirmed consumption offered no second time", o.Recover.Notices)
	}
	if o.Recover.Settles != 0 {
		t.Fatalf("the restart committed %d settle facts, want no second delivery of a confirmed consumption", o.Recover.Settles)
	}
	if !o.Recover.Drain.Drained || len(o.Recover.Drain.Unsettled) != 0 {
		t.Fatalf("the restart drain = %+v, want a drained run beside no obligation", o.Recover.Drain)
	}
	if o.Recover.Boundary.Owed != 0 || !strings.Contains(o.Recover.Boundary.AcceptErr, "no delivery obligation") {
		t.Fatalf("the durable boundary holds owed=%d and answered %q err=%q, want the receipt to close the delivery obligation",
			o.Recover.Boundary.Owed, o.Recover.Boundary.Accept, o.Recover.Boundary.AcceptErr)
	}
	if len(o.Seed.Mailbox) != 1 || len(o.Seed.Mailbox[0].Entries) != 1 || !o.Seed.Mailbox[0].Entries[0].Completed {
		t.Fatalf("the %s seed mailbox = %+v, want the consumption confirmed before the interruption", o.Phase, o.Seed.Mailbox)
	}
	if key := o.Seed.Mailbox[0].Entries[0].FactKey(); key != taskID+":1:settled:0" {
		t.Fatalf("the %s seed confirmed %s, want the notification of the task %s", o.Phase, key, taskID)
	}
}

// restartAssertConfirmedNotResent fails when a confirmed consumption is
// delivered again after the restart: the restart names the duty, commits a
// second delivery fact, or the durable boundary still owes it.
func restartAssertConfirmedNotResent(t *testing.T, o restartOutcome) {
	t.Helper()
	if len(o.Recover.Notices) != 0 || o.Recover.Settles != 0 || o.Recover.Boundary.Owed != 0 {
		t.Fatalf("the restart re-offers a consumption the durable receipt already confirms: notices=%+v settles=%d owed=%d: a confirmed consumption MUST NOT be delivered a second time",
			o.Recover.Notices, o.Recover.Settles, o.Recover.Boundary.Owed)
	}
}

// restartAssertUnconfirmedNotLost fails when a notification whose consumption
// receipt never landed is dropped by the restart: the rebuilt task is gone, the
// duty is neither kept owed beside the durable boundary nor really delivered
// beside a receipt, or an open duty is kept owed and delivered a second time.
func restartAssertUnconfirmedNotLost(t *testing.T, o restartOutcome) {
	t.Helper()
	if _, ok := restartSoleTask(o.Recover); !ok {
		t.Fatalf("the restart dropped the task whose consumption receipt never landed: recovery=%+v", o.Recover.Recovery)
	}
	switch o.Recover.Boundary.Owed {
	case 1:
		if len(o.Recover.Notices) != 1 {
			t.Fatalf("the restart kept a duty whose consumption receipt never landed but dropped it from the account: notices=%+v boundary=%+v", o.Recover.Notices, o.Recover.Boundary)
		}
		if restartNoticeDelivered(o.Recover) {
			t.Fatalf("the restart kept a duty whose consumption receipt never landed owed and delivered it too: sends=%q: an unconfirmed notification MUST NOT be delivered a second time", o.Recover.Sends)
		}
	case 0:
		if !restartNoticeDelivered(o.Recover) {
			t.Fatalf("the restart closed the duty without a real delivery: sends=%q: an unconfirmed notification MUST stay owed or be delivered once", o.Recover.Sends)
		}
	default:
		t.Fatalf("the durable boundary owes %d notifications for a duty whose consumption receipt never landed: an unconfirmed notification MUST NOT be lost", o.Recover.Boundary.Owed)
	}
}

// restartAssertCrossBoundary fails when the restart reads a claim as a
// confirmation or a confirmation as a claim: the claimed side must stay owed and
// acceptable, the confirmed side must owe nothing and refuse acceptance.
func restartAssertCrossBoundary(t *testing.T, claimed, confirmed restartOutcome) {
	t.Helper()
	phase := ""
	if len(claimed.Recover.Notices) == 1 {
		phase = claimed.Recover.Notices[0].Phase
	}
	if phase == string(query.NoticePhaseConsumed) {
		t.Fatalf("the restart read a claim as a confirmation: the %s notification stands consumed although no receipt landed", claimed.Phase)
	}
	if claimed.Recover.Boundary.Owed != 1 || claimed.Recover.Boundary.Accept != "accepted" {
		t.Fatalf("the claimed side of the boundary owes %d and answered %q err=%q, want it owed and still acceptable",
			claimed.Recover.Boundary.Owed, claimed.Recover.Boundary.Accept, claimed.Recover.Boundary.AcceptErr)
	}
	if len(confirmed.Recover.Notices) != 0 || confirmed.Recover.Boundary.Owed != 0 {
		t.Fatalf("the restart read a confirmation as a claim: the confirmed side holds notices=%+v owed=%d, want neither",
			confirmed.Recover.Notices, confirmed.Recover.Boundary.Owed)
	}
	if confirmed.Recover.Boundary.AcceptErr == "" {
		t.Fatalf("the durable boundary accepted the confirmed notification %q, want it refused by name", confirmed.Recover.Boundary.Accept)
	}
	if claimed.Recover.Boundary.Owed == confirmed.Recover.Boundary.Owed {
		t.Fatalf("the boundary read the same on both sides (owed=%d), want a claim kept apart from a confirmation", claimed.Recover.Boundary.Owed)
	}
}

// restartAssertFirstRequest is the consumer anchor of the restart: every
// restarted phase made exactly one actual request carrying the input the
// restart was driven with, and sent exactly the answer the provider returned,
// so the record is a real Boot request and send instead of a bookkeeping
// mirror.
func restartAssertFirstRequest(t *testing.T, outcome func(*testing.T, restartPhase) restartOutcome) {
	t.Helper()
	for _, phase := range []restartPhase{
		restartBeforeCommit, restartAfterCommit, restartAfterClaim,
		restartBeforeConfirmation, restartAfterConfirmation,
	} {
		o := outcome(t, phase)
		if o.Recover.Requests != 1 || o.Recover.Prompt != restartRecoverInput {
			t.Fatalf("the %s restart made %d requests carrying %q, want its first actual request carrying the restart input", phase, o.Recover.Requests, o.Recover.Prompt)
		}
		if len(o.Recover.Sends) != 1 || !strings.Contains(o.Recover.Sends[0], restartAnswerBody) {
			t.Fatalf("the %s restart sent %+v, want the one answer the provider returned", phase, o.Recover.Sends)
		}
	}
}

// restartSoleTask reports the one task the restart rebuilt.
func restartSoleTask(report restartChildReport) (string, bool) {
	if len(report.Recovery) != 1 || !report.Recovery[0].Recovered {
		return "", false
	}
	return report.Recovery[0].TaskID, true
}

// restartSoleNotice reports the one notification the restart read.
func restartSoleNotice(report restartChildReport) (consumptionNoticeView, bool) {
	if len(report.Notices) != 1 {
		return consumptionNoticeView{}, false
	}
	return report.Notices[0], true
}

// newFoldWindowRun assembles one runtime whose agent declares the fold window
// the 03.11 long run declares: a small model window, the 0.3 share of that
// window and one recent task kept whole, so every new segment start folds the
// segment before it inside the check.
func newFoldWindowRun(t *testing.T, ctx context.Context, provider model.Model) (*assembly.Run, event.Address) {
	t.Helper()
	run, err := assembly.Assemble(ctx, &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:                "agent-a",
			Model:             "model-a",
			MaxTokens:         projectionWindowTokens,
			CompressThreshold: 0.3,
			KeepRecentTasks:   1,
		}},
	}, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run, event.Address{AgentID: "agent-a", SessionID: "session-projection-fold-notice"}
}

// runNoticeVisibleUnderFold is the negative anchor of EVID-04. A notice fact
// starts a range of its own, so its commit is the segment start the age fold
// reads: with one recent task kept whole, that commit folds the segment before
// it and rewrites the sequence the notice boundary was measured on. The notice
// identity and its material must still reach the model of the range it drives —
// through the increment read beside the budgeted prefix — instead of surviving
// only as the one-line summary the budget degraded it to.
//
// The check commits the notice over the production commit path and drives the
// notice range with no ready input, exactly the shape the host fires an
// unattended follow-up with, then reads the request the provider actually
// served.
func runNoticeVisibleUnderFold(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	provider := &replayModel{name: "model-a"}
	run, addr := newFoldWindowRun(t, ctx, provider)
	for turn := 0; turn < 3; turn++ {
		commitProjectionTurn(t, ctx, run, addr, turn)
	}

	const (
		invocation = event.InvocationID("inv-notice-under-fold")
		taskID     = event.TaskID("task-9")
		material   = "tagent: task task-9 settled\nresult:\nthe folded-window worker reports 88"
	)
	notice := task.Notice{TaskID: taskID, Kind: task.NoticeSettled}
	before := len(projectionEntriesOf(t, run, addr.SessionID))
	if _, err := run.CommitToSession(ctx, addr, event.Event{
		Key:       agent.NoticeFactKey(invocation, notice),
		Type:      event.TypeFeedback,
		Timestamp: projectionBase.Add(time.Hour),
		Meta: map[string]string{
			event.MetaKeyTriggerSource: "task",
			event.MetaKeyEventSummary:  string(task.NoticeSettled),
			event.MetaKeyNoticeKind:    string(task.NoticeSettled),
			event.MetaKeyTaskID:        string(taskID),
		},
		Message:      model.Message{Role: model.RoleUser, Content: material},
		InvocationID: invocation,
		TaskID:       taskID,
	}); err != nil {
		t.Fatalf("commit the notice fact: %v", err)
	}
	after := projectionEntriesOf(t, run, addr.SessionID)
	if len(after) >= before {
		t.Fatalf("the notice commit folded nothing (entries before=%d after=%d), so the sequence the boundary was measured on still stands and the check would not read the rewrite", before, len(after))
	}

	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID:  invocation,
		Address:       addr,
		TriggerSource: "task",
	}); err != nil {
		t.Fatalf("drive the notice range: %v", err)
	}
	requests := provider.recorded()
	if len(requests) == 0 {
		t.Fatal("the notice range drove no model request")
	}
	request := requests[len(requests)-1]
	if !containsMessage(request.Messages, material, model.RoleUser) {
		t.Fatalf("the request of the notice range = %+v, want the notice identity and its material %q; the notice fact is in the chain, so the loss is the visibility the rewrite left, not the ledger",
			request.Messages, material)
	}
}

// TestEventIntegrityPersistence drives the formal host over a real localfile tier and scripted chat and embedding endpoints:
// - the facts a turn commits are read back through the declared recall tools in the same process
// - a storage failure is reported by name instead of answered as success
// - one failed vector generation leaves a recoverable debt
// - a new fact never makes an older reachable record unreachable
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestEventIntegrityPersistence(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	t.Run("new_fact_recalled_same_turn", runPersistenceNewFact)
	t.Run("original_text_matches_committed_bytes", runPersistenceOriginalText)
	t.Run("formal_recall_tool_returns_new_fact", runPersistenceFormalTool)
	t.Run("storage_failure_not_reported_as_success", runPersistenceStorageFailure)
	t.Run("index_failure_leaves_recoverable_debt", runPersistenceIndexDebt)
	t.Run("new_fact_keeps_old_reachable", runPersistenceOldReachable)
}

// persistenceModel, persistenceEmbedding, persistenceDimensions, persistenceAgentID, persistenceSessionID and persistenceMaxRounds are the persistence fixture: one entry agent declaring the retrieval and exact-read tools beside a localfile memory tier.
//
// The chat endpoint is scripted per turn, so every model request is a step a check wrote and every tool answer the next request carries is the material the check reads back.
const (
	persistenceModel      = "persistence-model"
	persistenceEmbedding  = "persistence-embedding"
	persistenceDimensions = 8
	persistenceAgentID    = event.AgentID("agent-persistence")
	persistenceSessionID  = event.SessionID("session-persistence")
	// persistenceMaxRounds bounds the retrieval the scripted model is willing
	// to repeat while a vector generation has not landed yet.
	persistenceMaxRounds = 8
)

// persistenceAddress is the one session every persistence anchor drives.
var persistenceAddress = event.Address{AgentID: persistenceAgentID, SessionID: persistenceSessionID}

// persistenceChat is the scripted chat endpoint: every request is recorded in
// the shape a decision selects on, and the payload of the answer is decided
// from the request itself, so a check scripts the model step by step.
type persistenceChat struct {
	server   *httptest.Server
	mu       sync.Mutex
	bodies   []map[string]json.RawMessage
	requests []persistenceRequest
	decide   func(index int, request persistenceRequest) string
}

// persistenceRequest is one chat completion request as the endpoint decoded it.
type persistenceRequest struct {
	Index    int
	Messages []persistenceMessage
	Tools    []string
}

// persistenceMessage is one message a request carried.
type persistenceMessage struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []persistenceToolCall
}

// persistenceToolCall is one tool call an assistant message carried.
type persistenceToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// newPersistenceChat starts the scripted endpoint and closes it with the test.
func newPersistenceChat(t *testing.T) *persistenceChat {
	t.Helper()
	provider := &persistenceChat{}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// serve answers one chat completion with the payload the current script
// decides, and records the request beside the answer it drove.
func (p *persistenceChat) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body map[string]json.RawMessage
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	request := decodePersistenceRequest(0, body)
	p.mu.Lock()
	request.Index = len(p.bodies)
	p.bodies = append(p.bodies, body)
	p.requests = append(p.requests, request)
	decide := p.decide
	p.mu.Unlock()
	payload := persistenceTerminalAnswer("ok")
	if decide != nil {
		payload = decide(request.Index, request)
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, payload)
}

// setDecide installs the script the next request is answered by.
func (p *persistenceChat) setDecide(decide func(index int, request persistenceRequest) string) {
	p.mu.Lock()
	p.decide = decide
	p.mu.Unlock()
}

// endpoint reports the API root the host document names.
func (p *persistenceChat) endpoint() string { return p.server.URL + "/v1" }

// count reports how many chat requests the endpoint answered.
func (p *persistenceChat) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.bodies)
}

// snapshot reports the recorded requests as a copy the caller owns.
func (p *persistenceChat) snapshot() []persistenceRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]persistenceRequest(nil), p.requests...)
}

// decodePersistenceRequest reads one recorded body into the request shape a
// script decides on: the messages it carried, the tool calls inside them and
// the tool names the request declared.
func decodePersistenceRequest(index int, body map[string]json.RawMessage) persistenceRequest {
	request := persistenceRequest{Index: index}
	if raw, ok := body["messages"]; ok {
		var messages []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal(raw, &messages); err == nil {
			for _, message := range messages {
				decoded := persistenceMessage{Role: message.Role, Content: persistenceContent(message.Content), ToolCallID: message.ToolCallID}
				for _, call := range message.ToolCalls {
					decoded.ToolCalls = append(decoded.ToolCalls, persistenceToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
				}
				request.Messages = append(request.Messages, decoded)
			}
		}
	}
	if raw, ok := body["tools"]; ok {
		var tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &tools); err == nil {
			for _, tool := range tools {
				request.Tools = append(request.Tools, tool.Function.Name)
			}
		}
	}
	return request
}

// persistenceContent reads one message content the protocol may render as a
// plain string or as parts.
func persistenceContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "")
}

// recallAnswers reports every answer of the named recall tool the request
// carried, oldest first: an observation reaches the next request as user
// material whose content is the JSON the tool answered.
func (r persistenceRequest) recallAnswers(tool string) []map[string]any {
	var answers []map[string]any
	for _, message := range r.Messages {
		if message.Role != string(model.RoleUser) {
			continue
		}
		var answer map[string]any
		if json.Unmarshal([]byte(message.Content), &answer) != nil {
			continue
		}
		if name, _ := answer["tool"].(string); name != tool {
			continue
		}
		answers = append(answers, answer)
	}
	return answers
}

// persistenceTerminalAnswer renders one terminal assistant answer.
func persistenceTerminalAnswer(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil {
		panic("history: encode the persistence terminal answer: " + err.Error())
	}
	return `{"id":"persistence","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":` + string(encoded) + `},"finish_reason":"stop"}]}`
}

// persistenceCallText is the assistant text one scripted tool-call answer
// carries beside the calls it issues.
const persistenceCallText = "looking"

// persistenceToolCallAnswer renders one assistant answer that issues a call of
// the named tool with the given argument text.
func persistenceToolCallAnswer(name, callID, arguments string) string {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		panic("history: encode the persistence tool arguments: " + err.Error())
	}
	return `{"id":"persistence-tool","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"` + persistenceCallText + `","tool_calls":[{"id":"` +
		callID + `","type":"function","function":{"name":"` + name + `","arguments":` + string(encoded) + `}}]},"finish_reason":"tool_calls"}]}`
}

// persistenceEmbeddings is the scripted embedding endpoint: every text is
// recorded, the vector is derived from the words the text carries, so the
// cosine of two texts is decided by meaning rather than by chance, and a
// switch fails every document carrying the lantern watchword so one check can
// break the index without breaking the store.
type persistenceEmbeddings struct {
	server      *httptest.Server
	mu          sync.Mutex
	failLantern bool
	texts       []string
	success     map[string]int
	failures    map[string]int
}

// newPersistenceEmbeddings starts the scripted endpoint and closes it with the
// test.
func newPersistenceEmbeddings(t *testing.T) *persistenceEmbeddings {
	t.Helper()
	edge := &persistenceEmbeddings{success: map[string]int{}, failures: map[string]int{}}
	edge.server = httptest.NewServer(http.HandlerFunc(edge.serve))
	t.Cleanup(edge.server.Close)
	return edge
}

// serve answers one embedding request, or refuses the documents the check
// broke the index for by name.
func (e *persistenceEmbeddings) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/embeddings") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body struct {
		Input json.RawMessage `json:"input"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	text := persistenceEmbeddingInput(body.Input)
	e.mu.Lock()
	e.texts = append(e.texts, text)
	fail := e.failLantern && strings.Contains(text, "lantern")
	if fail {
		e.failures[text]++
	} else {
		e.success[text]++
	}
	e.mu.Unlock()
	if fail {
		http.Error(w, `{"error":{"message":"the embedding index refused the document","type":"server_error"}}`, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[%s]}],"model":%q,"usage":{"prompt_tokens":1,"total_tokens":1}}`,
		persistenceVectorOf(text), persistenceEmbedding)
}

// persistenceEmbeddingInput reads the input one embedding request carried,
// which the protocol renders as one string or as a list of strings.
func persistenceEmbeddingInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 {
		return ""
	}
	return list[0]
}

// persistenceVectorOf places one text in the scripted vector space: the lantern
// and illumination vocabulary sits on the first axis, the harbor vocabulary on
// the second, and every other text on the third, so a retrieval by meaning
// answers exactly the documents the check means it to answer.
func persistenceVectorOf(text string) string {
	switch {
	case strings.Contains(text, "lantern"), strings.Contains(text, "illumination"):
		return "1,0,0,0,0,0,0,0"
	case strings.Contains(text, "harbor"):
		return "0,1,0,0,0,0,0,0"
	default:
		return "0,0,1,0,0,0,0,0"
	}
}

// endpoint reports the base URL the engine declaration names.
func (e *persistenceEmbeddings) endpoint() string { return e.server.URL + "/v1/" }

// setFailLantern breaks or restores the generation of every lantern document.
func (e *persistenceEmbeddings) setFailLantern(fail bool) {
	e.mu.Lock()
	e.failLantern = fail
	e.mu.Unlock()
}

// successCount reports how many generations of the exact text succeeded.
func (e *persistenceEmbeddings) successCount(text string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.success[text]
}

// failureCount reports how many generations of the exact text were refused.
func (e *persistenceEmbeddings) failureCount(text string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.failures[text]
}

// recordedTexts reports every text the endpoint was asked to embed.
func (e *persistenceEmbeddings) recordedTexts() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.texts...)
}

// persistenceSender is the outbound face of the host: every delivered body is
// recorded, so the entry turns run through the same assembly a deployment runs.
type persistenceSender struct {
	mu   sync.Mutex
	sent []string
}

// Send records one delivered body.
func (s *persistenceSender) Send(_ context.Context, _ event.Address, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, content)
	return nil
}

// persistenceHostDocument renders the host document of the persistence rig:
// the entry agent beside the retrieval and exact-read tools and the localfile
// memory tier whose engine talks to the scripted embedding endpoint.
func persistenceHostDocument(root, chatEndpoint, embeddingEndpoint string) string {
	return fmt.Sprintf("entry: %s\n"+
		"provider: local\n"+
		"model: %s\n"+
		"providers:\n"+
		"  local: { provider: openai, api_endpoint: %q, api_key_env: TAGENT_TEST_API_KEY }\n"+
		"agents:\n"+
		"  %s:\n"+
		"    model: %s\n"+
		"    max_tokens: 4096\n"+
		"    max_tool_iterations: 12\n"+
		"    tools:\n"+
		"      - { kind: tool, id: %s }\n"+
		"      - { kind: tool, id: %s }\n"+
		"    memory:\n"+
		"      type: localfile\n"+
		"      path: %q\n"+
		"      engine:\n"+
		"        backend: memory\n"+
		"        embedding:\n"+
		"          provider: openai\n"+
		"          model: %s\n"+
		"          api_key_env: TAGENT_TEST_API_KEY\n"+
		"          dimensions: %d\n"+
		"          api_endpoint: %q\n"+
		"storage: { type: localfile, root: %q }\n",
		persistenceAgentID, persistenceModel, chatEndpoint,
		persistenceAgentID, persistenceModel,
		recall.QueryToolName, recall.GetToolName,
		root, persistenceEmbedding, persistenceDimensions, embeddingEndpoint, root)
}

// persistenceRig is one host over one durable root: the scripted endpoints, the
// outbound face and the assembled host the checks drive.
type persistenceRig struct {
	t          *testing.T
	root       string
	provider   *persistenceChat
	embeddings *persistenceEmbeddings
	sender     *persistenceSender
	host       *assembly.Host
}

// newPersistenceRig boots one host over a fresh durable root.
func newPersistenceRig(t *testing.T) *persistenceRig {
	t.Helper()
	rig := &persistenceRig{
		t:          t,
		root:       t.TempDir(),
		provider:   newPersistenceChat(t),
		embeddings: newPersistenceEmbeddings(t),
		sender:     &persistenceSender{},
	}
	t.Cleanup(func() {
		if rig.host == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := rig.host.Close(ctx); err != nil {
			t.Logf("the persistence host ended its close beside: %v", err)
		}
	})
	rig.boot()
	return rig
}

// boot assembles one host over the rig's root through the formal entry.
func (r *persistenceRig) boot() {
	r.t.Helper()
	configPath := filepath.Join(r.root, "tagent.yaml")
	document := persistenceHostDocument(r.root, r.provider.endpoint(), r.embeddings.endpoint())
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		r.t.Fatalf("write the persistence host document: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(r.sender))
	if err != nil {
		r.t.Fatalf("boot the persistence host: %v", err)
	}
	r.host = host
}

// invoke drives one user turn of the one address through the formal entry.
func (r *persistenceRig) invoke(ctx context.Context, invocation string, input string) (agent.Turn, error) {
	return r.host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: event.InvocationID(invocation),
		Address:      persistenceAddress,
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	})
}

// mustInvoke drives one turn and reports a refused turn at once.
func (r *persistenceRig) mustInvoke(ctx context.Context, invocation string, input string) {
	r.t.Helper()
	if _, err := r.invoke(ctx, invocation, input); err != nil {
		r.t.Fatalf("drive the persistence turn %q: %v", input, err)
	}
}

// shutdown drains the run and closes the host, so the durable root is read
// after the same close a deployment runs.
func (r *persistenceRig) shutdown() {
	r.t.Helper()
	if r.host == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := r.host.Run().Close(ctx, agent.CloseRequest{Deadline: time.Now().Add(20 * time.Second)})
	if err != nil {
		r.t.Fatalf("drain the persistence run: %v", err)
	}
	if !report.Drained || len(report.Unsettled) != 0 {
		r.t.Fatalf("drain report = %+v, want every formal obligation settled before the close", report)
	}
	if err := r.host.Close(ctx); err != nil {
		r.t.Fatalf("close the persistence host: %v", err)
	}
	r.host = nil
}

// scriptToolTurn answers the next request with one call of the named tool and
// ends the turn once that call's answer arrived.
func (r *persistenceRig) scriptToolTurn(tool, callID, arguments, terminal string) {
	seen := -1
	r.provider.setDecide(func(_ int, request persistenceRequest) string {
		answers := request.recallAnswers(tool)
		if seen < 0 {
			seen = len(answers)
		}
		if len(answers) > seen {
			return persistenceTerminalAnswer(terminal)
		}
		return persistenceToolCallAnswer(tool, callID, arguments)
	})
}

// scriptTerminal answers every request of the turn with the same terminal.
func (r *persistenceRig) scriptTerminal(text string) {
	r.provider.setDecide(func(int, persistenceRequest) string {
		return persistenceTerminalAnswer(text)
	})
}

// scriptPollingQuery issues one retrieval per round until the retrieval answers
// the wanted key, so a vector generation that lands asynchronously is waited
// for by the retrieval itself and never by a sleep.
func (r *persistenceRig) scriptPollingQuery(query string, want event.EventID, terminal, gaveUp string) {
	seen := -1
	rounds := 0
	r.provider.setDecide(func(_ int, request persistenceRequest) string {
		answers := request.recallAnswers(recall.QueryToolName)
		if seen < 0 {
			seen = len(answers)
		}
		if len(answers) > seen {
			latest := answers[len(answers)-1]
			if persistenceAnswerHas(latest, want) {
				return persistenceTerminalAnswer(terminal)
			}
			rounds++
			if rounds >= persistenceMaxRounds {
				return persistenceTerminalAnswer(gaveUp)
			}
		}
		return persistenceToolCallAnswer(recall.QueryToolName,
			fmt.Sprintf("call-poll-%d", rounds), fmt.Sprintf(`{"text":%q}`, query))
	})
}

// persistenceAnswerHas reports whether the retrieval answer carries the key.
func persistenceAnswerHas(answer map[string]any, key event.EventID) bool {
	for _, item := range persistenceItemsOf(answer) {
		if item["key"] == string(key) {
			return true
		}
	}
	return false
}

// persistenceItemsOf reports the items of one retrieval answer.
func persistenceItemsOf(answer map[string]any) []map[string]any {
	raw, _ := answer["items"].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if item, ok := entry.(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items
}

// persistenceLastAnswer reports the newest answer of the named tool the
// recorded requests carried.
func persistenceLastAnswer(t *testing.T, provider *persistenceChat, tool string) map[string]any {
	t.Helper()
	requests := provider.snapshot()
	for index := len(requests) - 1; index >= 0; index-- {
		if answers := requests[index].recallAnswers(tool); len(answers) > 0 {
			return answers[len(answers)-1]
		}
	}
	t.Fatalf("no recorded request carried an answer of %s: %d requests", tool, len(requests))
	return nil
}

// persistenceFactByContent reports the committed fact whose content is exactly
// the text.
func persistenceFactByContent(t *testing.T, run *assembly.Run, content string) event.Event {
	t.Helper()
	for _, fact := range run.SessionFacts(persistenceAddress) {
		if fact.Message.Content == content {
			return fact
		}
	}
	t.Fatalf("the session facts carry no fact whose content is %q", content)
	return event.Event{}
}

// persistenceFactOf reports the one committed fact the match selects.
func persistenceFactOf(t *testing.T, run *assembly.Run, what string, match func(event.Event) bool) event.Event {
	t.Helper()
	var matched []event.Event
	for _, fact := range run.SessionFacts(persistenceAddress) {
		if match(fact) {
			matched = append(matched, fact)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("the session facts carry %d facts naming %s, want exactly 1", len(matched), what)
	}
	return matched[0]
}

// persistenceWaitForEmbedding waits until the endpoint answered the exact text
// once, so the check reads a generation that really ran.
func persistenceWaitForEmbedding(t *testing.T, edge *persistenceEmbeddings, text string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if edge.successCount(text) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the embedding endpoint never answered %q; texts=%v", text, edge.recordedTexts())
}

// persistenceShardPath reports the shard file the agent's events are committed
// into: the partition its identity resolves to.
func persistenceShardPath(root string) string {
	partition := memory.PartitionIDFromName(string(persistenceAgentID))
	return filepath.Join(root, "kv", url.PathEscape(strconv.Itoa(partition)), "snapshot.json")
}

// persistenceHoldShard makes every commit into the agent's shard fail at the
// atomic-rename barrier: the snapshot path becomes a directory, so the rename
// cannot land whatever user the process runs as, and the failure is the storage
// barrier the commit path must report instead of answering success.
func persistenceHoldShard(t *testing.T, root string) func() {
	t.Helper()
	shard := persistenceShardPath(root)
	held := shard + ".held"
	if _, err := os.Stat(shard); err != nil {
		t.Fatalf("the durable shard %s is missing: the earlier turn committed nothing into the storage tier: %v", shard, err)
	}
	if err := os.Rename(shard, held); err != nil {
		t.Fatalf("hold the shard snapshot %s: %v", shard, err)
	}
	if err := os.MkdirAll(shard, 0o755); err != nil {
		t.Fatalf("hold the shard snapshot %s: %v", shard, err)
	}
	return func() {
		t.Helper()
		if err := os.RemoveAll(shard); err != nil {
			t.Fatalf("release the shard snapshot %s: %v", shard, err)
		}
		if err := os.Rename(held, shard); err != nil {
			t.Fatalf("release the shard snapshot %s: %v", shard, err)
		}
	}
}

// persistenceWaitForEmbeddingFailure waits until the endpoint refused the exact
// text once, so the check reads a generation that really ran and really failed
// rather than a submission the seam never picked up.
func persistenceWaitForEmbeddingFailure(t *testing.T, edge *persistenceEmbeddings, text string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if edge.failureCount(text) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the embedding endpoint never refused %q; texts=%v", text, edge.recordedTexts())
}

// runPersistenceNewFact drives one formal turn that retrieves before it answers,
// and reads the conversation facts of that turn back through the formal recall
// tools of the same process: the input is answered by meaning and the tool
// result keeps an identity of its own.
func runPersistenceNewFact(t *testing.T) {
	rig := newPersistenceRig(t)
	defer rig.shutdown()

	const input = "the lantern keeps the harbor in view"
	const answer = "the lantern was noted"
	rig.scriptToolTurn(recall.QueryToolName, "call-new-fact", `{"text":"lantern"}`, answer)
	rig.mustInvoke(context.Background(), "inv-new-fact", input)

	run := rig.host.Run()
	inputFact := persistenceFactByContent(t, run, input)
	outputFact := persistenceFactByContent(t, run, answer)
	toolFact := persistenceFactOf(t, run, "the record of the retrieval call", func(evt event.Event) bool {
		return evt.Type == event.TypeActionCommand && evt.Message.ToolName == recall.QueryToolName
	})
	if inputFact.Key == "" || outputFact.Key == "" || toolFact.Key == "" {
		t.Fatalf("the turn committed an empty identity: input=%q output=%q tool=%q",
			inputFact.Key, outputFact.Key, toolFact.Key)
	}
	for _, fact := range []event.Event{inputFact, outputFact, toolFact} {
		committed, err := run.CommittedEvent(context.Background(), fact.Key)
		if err != nil {
			t.Fatalf("read the committed fact %s: %v", fact.Key, err)
		}
		if committed.Key != fact.Key {
			t.Fatalf("committed fact key = %q, want %q", committed.Key, fact.Key)
		}
	}

	rig.scriptPollingQuery("lantern", inputFact.Key, "the input was recalled", "the input never came back")
	rig.mustInvoke(context.Background(), "inv-new-fact-recall", "recall the lantern")
	recalled := persistenceLastAnswer(t, rig.provider, recall.QueryToolName)
	if !persistenceAnswerHas(recalled, inputFact.Key) {
		t.Fatalf("the retrieval answer does not carry the committed input %s: %v", inputFact.Key, recalled)
	}
	t.Logf("the turn committed input=%s output=%s tool=%s; the query answered %v item(s)",
		inputFact.Key, outputFact.Key, toolFact.Key, recalled["count"])
	persistenceWaitForEmbedding(t, rig.embeddings, input)
}

// runPersistenceOriginalText reads one committed conversation fact back through
// the formal exact-read tool and compares the body byte for byte with the text
// the turn committed: a stored fact answers its own bytes, never a rendering of
// them.
func runPersistenceOriginalText(t *testing.T) {
	rig := newPersistenceRig(t)
	defer rig.shutdown()

	const text = "the harbor gate read:  tide 04:15, berth 七, note \"keep the exact bytes\""
	rig.scriptTerminal("the original was kept")
	rig.mustInvoke(context.Background(), "inv-original", text)

	run := rig.host.Run()
	fact := persistenceFactByContent(t, run, text)
	if fact.Key == "" {
		t.Fatalf("the turn committed no identity for its input")
	}
	rig.scriptToolTurn(recall.GetToolName, "call-original", fmt.Sprintf(`{"key":%q}`, fact.Key), "the original was read")
	rig.mustInvoke(context.Background(), "inv-original-read", "read the original")

	answer := persistenceLastAnswer(t, rig.provider, recall.GetToolName)
	if found, _ := answer["found"].(bool); !found {
		t.Fatalf("the exact read answered found=false: %v", answer)
	}
	if body, _ := answer["body"].(string); body != text {
		t.Fatalf("the exact read answered %q, want the committed bytes %q", body, text)
	}
	t.Logf("the exact read answered the committed bytes: key=%s bytes=%d", fact.Key, len(text))
}

// runPersistenceFormalTool reads the tool result of one formal turn as its
// original through the exact-read tool and answers the output of that turn
// through the retrieval tool: the two facts a turn writes beyond its input are
// recalled by the same declared tools a deployment grants.
func runPersistenceFormalTool(t *testing.T) {
	rig := newPersistenceRig(t)
	defer rig.shutdown()

	const input = "the lantern question needs an index"
	const answer = "the query answered"
	rig.scriptToolTurn(recall.QueryToolName, "call-formal", `{"text":"lantern"}`, answer)
	rig.mustInvoke(context.Background(), "inv-formal", input)

	run := rig.host.Run()
	toolFact := persistenceFactOf(t, run, "the record of the retrieval call", func(evt event.Event) bool {
		return evt.Type == event.TypeActionCommand && evt.Message.ToolName == recall.QueryToolName
	})
	outputFact := persistenceFactByContent(t, run, answer)
	persistenceWaitForEmbedding(t, rig.embeddings, input)
	persistenceWaitForEmbedding(t, rig.embeddings, toolFact.Message.Content)

	rig.scriptToolTurn(recall.GetToolName, "call-formal-read", fmt.Sprintf(`{"key":%q}`, toolFact.Key), "the tool result was read")
	rig.mustInvoke(context.Background(), "inv-formal-read", "read the tool result")
	read := persistenceLastAnswer(t, rig.provider, recall.GetToolName)
	if found, _ := read["found"].(bool); !found {
		t.Fatalf("the tool result %s has no original to read: %v", toolFact.Key, read)
	}
	if body, _ := read["body"].(string); !strings.Contains(body, string(recall.QueryToolName)) {
		t.Fatalf("the tool result original does not carry the call it answered: %q", body)
	}
	t.Logf("the tool result %s kept its original bytes=%d", toolFact.Key, len(toolFact.Message.Content))

	rig.scriptPollingQuery("query answered", outputFact.Key, "the output was recalled", "the output never came back")
	rig.mustInvoke(context.Background(), "inv-formal-recall", "recall the output")
	recalled := persistenceLastAnswer(t, rig.provider, recall.QueryToolName)
	if !persistenceAnswerHas(recalled, outputFact.Key) {
		t.Fatalf("the retrieval answer does not carry the committed output %s: %v", outputFact.Key, recalled)
	}
}

// runPersistenceStorageFailure holds the agent's shard at the atomic-rename
// barrier and drives one turn against it: the commit path reports the storage
// failure by name, no model step runs on a fact that never landed, no live fact
// carries the refused input, and the retry after the release is recalled.
func runPersistenceStorageFailure(t *testing.T) {
	rig := newPersistenceRig(t)
	defer rig.shutdown()

	const warmup = "the harbor keeps a warm shard"
	const refused = "the lantern must not be answered as success"
	rig.scriptTerminal("the first turn landed")
	rig.mustInvoke(context.Background(), "inv-storage-warmup", warmup)

	requests := rig.provider.count()
	release := persistenceHoldShard(t, rig.root)
	_, err := rig.invoke(context.Background(), "inv-storage-failure", refused)
	if err == nil {
		release()
		t.Fatalf("the turn beside a failing storage tier returned no failure")
	}
	if !errors.Is(err, ledger.ErrSegmentBarrier) {
		release()
		t.Fatalf("the turn failed beside %v, want the segment barrier", err)
	}
	if got := rig.provider.count(); got != requests {
		release()
		t.Fatalf("the refused turn reached the model %d times, want none", got-requests)
	}
	run := rig.host.Run()
	for _, fact := range run.SessionFacts(persistenceAddress) {
		if fact.Message.Content == refused {
			release()
			t.Fatalf("the refused commit left a live fact %s: %v", fact.Key, fact)
		}
	}
	release()

	rig.scriptTerminal("the retry landed")
	rig.mustInvoke(context.Background(), "inv-storage-retry", refused)
	retried := persistenceFactByContent(t, run, refused)
	if retried.Key == "" {
		t.Fatalf("the retried turn committed no identity")
	}
	rig.scriptPollingQuery("lantern", retried.Key, "the retry was recalled", "the retry never came back")
	rig.mustInvoke(context.Background(), "inv-storage-recall", "recall the retry")
	if recalled := persistenceLastAnswer(t, rig.provider, recall.QueryToolName); !persistenceAnswerHas(recalled, retried.Key) {
		t.Fatalf("the retrieval answer does not carry the retried fact %s: %v", retried.Key, recalled)
	}
	t.Logf("the refused commit failed beside %v; the retry landed as %s and was recalled", err, retried.Key)
}

// runPersistenceIndexDebt breaks the vector generation of one document: the
// store keeps it, the semantic query cannot answer it yet, and a restart over
// the same root re-submits every committed fact so the debt is paid and the
// document is reachable by meaning.
func runPersistenceIndexDebt(t *testing.T) {
	rig := newPersistenceRig(t)
	defer rig.shutdown()

	rig.embeddings.setFailLantern(true)
	const debt = "the lantern waits on the shelf"
	rig.scriptTerminal("the store kept the debt")
	rig.mustInvoke(context.Background(), "inv-index-debt", debt)
	persistenceWaitForEmbeddingFailure(t, rig.embeddings, debt)

	run := rig.host.Run()
	fact := persistenceFactByContent(t, run, debt)
	if fact.Key == "" {
		t.Fatalf("the store kept no identity for the refused document")
	}
	rig.scriptToolTurn(recall.GetToolName, "call-debt-read", fmt.Sprintf(`{"key":%q}`, fact.Key), "the debt is stored")
	rig.mustInvoke(context.Background(), "inv-index-debt-read", "read the debt")
	read := persistenceLastAnswer(t, rig.provider, recall.GetToolName)
	if found, _ := read["found"].(bool); !found {
		t.Fatalf("the refused document %s is not stored: %v", fact.Key, read)
	}

	rig.scriptToolTurn(recall.QueryToolName, "call-debt-miss", `{"text":"illumination"}`, "the debt is a miss")
	rig.mustInvoke(context.Background(), "inv-index-debt-miss", "recall the illumination")
	missed := persistenceLastAnswer(t, rig.provider, recall.QueryToolName)
	if persistenceAnswerHas(missed, fact.Key) {
		t.Fatalf("the unindexed document was answered by meaning: %v", missed)
	}

	rig.shutdown()
	rig.embeddings.setFailLantern(false)
	rig.boot()
	persistenceWaitForEmbedding(t, rig.embeddings, debt)
	rig.scriptPollingQuery("illumination", fact.Key, "the debt was paid", "the debt was never paid")
	rig.mustInvoke(context.Background(), "inv-index-debt-restart", "recall the illumination")
	if recalled := persistenceLastAnswer(t, rig.provider, recall.QueryToolName); !persistenceAnswerHas(recalled, fact.Key) {
		t.Fatalf("the restarted retrieval does not answer the paid debt %s: %v", fact.Key, recalled)
	}
	t.Logf("the index refused the document %d time(s); the restart submitted it and the query answered %s",
		rig.embeddings.failureCount(debt), fact.Key)
}

// runPersistenceOldReachable reaches one record by meaning, commits an unrelated
// record into the same index, and reaches the first record again: a new fact
// never makes an older reachable record unreachable, and the new record answers
// a query of the old meaning with nothing.
func runPersistenceOldReachable(t *testing.T) {
	rig := newPersistenceRig(t)
	defer rig.shutdown()

	const old = "the harbor register keeps the old berth"
	const fresh = "a lantern appears on the new shelf"
	rig.scriptTerminal("the old record was kept")
	rig.mustInvoke(context.Background(), "inv-old", old)
	persistenceWaitForEmbedding(t, rig.embeddings, old)

	run := rig.host.Run()
	oldFact := persistenceFactByContent(t, run, old)
	rig.scriptPollingQuery("harbor", oldFact.Key, "the old record is reachable", "the old record was unreachable")
	rig.mustInvoke(context.Background(), "inv-old-before", "recall the harbor")
	if before := persistenceLastAnswer(t, rig.provider, recall.QueryToolName); !persistenceAnswerHas(before, oldFact.Key) {
		t.Fatalf("the old record %s is not reachable before the new one: %v", oldFact.Key, before)
	}

	rig.scriptTerminal("the new record was kept")
	rig.mustInvoke(context.Background(), "inv-new", fresh)
	persistenceWaitForEmbedding(t, rig.embeddings, fresh)
	newFact := persistenceFactByContent(t, run, fresh)

	rig.scriptPollingQuery("harbor", oldFact.Key, "the old record is still reachable", "the new record made the old one unreachable")
	rig.mustInvoke(context.Background(), "inv-old-after", "recall the harbor again")
	after := persistenceLastAnswer(t, rig.provider, recall.QueryToolName)
	if !persistenceAnswerHas(after, oldFact.Key) {
		t.Fatalf("the old record %s is unreachable after the new one: %v", oldFact.Key, after)
	}
	if persistenceAnswerHas(after, newFact.Key) {
		t.Fatalf("the new record %s answered the old meaning: %v", newFact.Key, after)
	}
	t.Logf("the old record %s stayed reachable beside the new %s; the query answered %v item(s)",
		oldFact.Key, newFact.Key, after["count"])
}

// restoreChildModeEnv, restoreChildRootEnv, restoreChildOutEnv and restoreChildSeedEnv name the parent-to-child channel; restoreModeSeedGraceful, restoreModeSeedKill and restoreModeRecover name the stop modes; restoreSeedInvocation, restoreNextInvocation, restoreSeedInput, restoreSeedAnswer, restoreNextInput, restoreAfterAnswer, restoreCallID and restoreWatchArguments name the seeded material.
//
// The restore fixture reopens the durable root one earlier process left, and the restarted process reads it back from the tier alone.
const (
	restoreChildModeEnv = "TAGENT_EVENT_RESTORE_CHILD_MODE"
	restoreChildRootEnv = "TAGENT_EVENT_RESTORE_CHILD_ROOT"
	restoreChildOutEnv  = "TAGENT_EVENT_RESTORE_CHILD_OUT"
	restoreChildSeedEnv = "TAGENT_EVENT_RESTORE_CHILD_SEED"

	restoreModeSeedGraceful = "seed-graceful"
	restoreModeSeedKill     = "seed-kill"
	restoreModeRecover      = "recover"

	restoreSeedInvocation = "inv-restore-seed"
	restoreNextInvocation = "inv-restore-use"
	restoreSeedInput      = "the lantern needs a watcher"
	restoreSeedAnswer     = "the watcher sits on the east shelf"
	restoreNextInput      = "which shelf watches the lantern?"
	restoreAfterAnswer    = "the east shelf"
	restoreCallID         = "call-restore-watch"
	restoreWatchArguments = `{"text":"lantern"}`
)

// restoreModes is the two ways a process stops: the drained close a deployment
// runs and the abrupt interruption of a process that never closed.
var restoreModes = []string{restoreModeSeedGraceful, restoreModeSeedKill}

// restoreChildReport is what one process of the fixture reports: the seed
// reports the material it committed beside the call identity the provider
// issued, and the restarted process reports what the tier alone answered back
// beside the next request its own drive sent.
type restoreChildReport struct {
	Ready bool   `json:"ready"`
	Mode  string `json:"mode"`
	PID   int    `json:"pid"`

	Invocation event.InvocationID `json:"invocation,omitempty"`
	Instance   event.InstanceID   `json:"instance,omitempty"`
	Address    event.Address      `json:"address,omitempty"`

	Input      string        `json:"input,omitempty"`
	InputFact  event.EventID `json:"input_fact,omitempty"`
	CallID     string        `json:"call_id,omitempty"`
	CallText   string        `json:"call_text,omitempty"`
	ToolName   string        `json:"tool_name,omitempty"`
	ResultText string        `json:"result_text,omitempty"`
	ResultFact event.EventID `json:"result_fact,omitempty"`
	Answer     string        `json:"answer,omitempty"`
	AnswerFact event.EventID `json:"answer_fact,omitempty"`
	TaskID     event.TaskID  `json:"task_id,omitempty"`

	Recovery    []consumptionRecoveryView `json:"recovery,omitempty"`
	Invocations []restoreInvocationView   `json:"invocations,omitempty"`
	Tasks       []restoreTaskView         `json:"tasks,omitempty"`
	Chains      []restoreChainView        `json:"chains,omitempty"`
	Restored    map[string]string         `json:"restored,omitempty"`
	Observed    []string                  `json:"observed,omitempty"`

	Requests int                  `json:"requests,omitempty"`
	Messages []persistenceMessage `json:"messages,omitempty"`
}

// restoreInvocationView is one invocation the restarted run reads back.
type restoreInvocationView struct {
	InvocationID event.InvocationID `json:"invocation_id"`
	Address      event.Address      `json:"address"`
	Instance     event.InstanceID   `json:"instance"`
}

// restoreTaskView is one task the restarted run reads back.
type restoreTaskView struct {
	TaskID   event.TaskID     `json:"task_id"`
	Address  event.Address    `json:"address"`
	Instance event.InstanceID `json:"instance"`
	Status   int              `json:"status"`
}

// restoreChainView is one durable fact chain as the restarted process reads it
// back, beside the call identity the admission froze and the result identity
// the settled transition linked to it.
type restoreChainView struct {
	TaskID     event.TaskID       `json:"task_id"`
	CallID     string             `json:"call_id"`
	Invocation event.InvocationID `json:"invocation"`
	Source     event.Address      `json:"source"`
	Instance   event.InstanceID   `json:"instance"`
	Links      int                `json:"links"`
	ResultFact event.EventID      `json:"result_fact"`
	ResultTask event.TaskID       `json:"result_task"`
}

// TestEventIntegrityRestoreUse drives the cold-start anchors over one durable root:
// - one formal turn commits a conversation input, an assistant tool call, a tool result and an assistant output
// - the process stops by a drained close and by an abrupt interruption
// - a restarted process reopens the same root through the formal Boot entry and drives its next actual request
// - the positive readings are the original text, the invocation and task attribution and the call/result pairing the durable chain keeps
// - the negative reading is that a complete ledger is not completion while the next request carries none of it
func TestEventIntegrityRestoreUse(t *testing.T) {
	if mode := os.Getenv(restoreChildModeEnv); mode != "" {
		runRestoreChild(t, mode)
		return
	}
	cases := map[string]restoreOutcome{}
	outcome := func(t *testing.T, mode string) restoreOutcome {
		t.Helper()
		if held, ok := cases[mode]; ok {
			return held
		}
		held := restoreAcrossRestart(t, mode)
		cases[mode] = held
		return held
	}
	t.Run("restart_restores_original_text", func(t *testing.T) {
		restoreAssertOriginalText(t, outcome)
	})
	t.Run("restart_restores_invocation_attribution", func(t *testing.T) {
		restoreAssertAttribution(t, outcome)
	})
	t.Run("restart_restores_tool_call_result_pairing", func(t *testing.T) {
		restoreAssertPairing(t, outcome)
	})
	t.Run("next_request_uses_restored_material", func(t *testing.T) {
		restoreAssertNextRequest(t, outcome)
	})
	t.Run("restore_only_ledger_is_not_completion", func(t *testing.T) {
		restoreAssertLedgerIsNotCompletion(t, outcome)
	})
}

// restoreOutcome is one stop mode as the two processes reported it: the seed
// report the stopped process landed beside the raw output of both processes.
type restoreOutcome struct {
	Mode       string
	Root       string
	Seed       restoreChildReport
	Recover    restoreChildReport
	SeedLog    string
	RecoverLog string
}

// restoreAcrossRestart seeds one durable root, stops the seed process the way
// the mode names, and reopens the same root in a second process: the restart's
// readings are its own and never a mirror of the seed's memory.
func restoreAcrossRestart(t *testing.T, mode string) restoreOutcome {
	t.Helper()
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "seed.json")
	cmd := restoreChildCommand(mode, root, out, "")
	var seedLog restoreLog
	cmd.Stdout = &seedLog
	cmd.Stderr = &seedLog
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the %s seed process: %v", mode, err)
	}
	seed := waitRestoreReport(t, out, &seedLog, mode)
	restoreRelay(t, &seedLog, "seed")
	switch mode {
	case restoreModeSeedKill:
		t.Logf("parent: interrupt the %s seed pid=%d with SIGKILL", mode, cmd.Process.Pid)
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("interrupt the %s seed process: %v", mode, err)
		}
		err := cmd.Wait()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("the interrupted %s seed reported %v, want the signalled exit the parent caused; output=%s", mode, err, seedLog.String())
		}
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatalf("the %s seed ended %v, want the parent's SIGKILL; output=%s", mode, err, seedLog.String())
		}
		t.Logf("parent: the killed %s seed pid=%d was waited out with signal=%v", mode, cmd.Process.Pid, status.Signal())
	case restoreModeSeedGraceful:
		if err := cmd.Wait(); err != nil {
			t.Fatalf("the gracefully closed %s seed reported %v, want a drained exit; output=%s", mode, err, seedLog.String())
		}
		t.Logf("parent: the gracefully closed %s seed pid=%d exited 0 after its drained close", mode, cmd.Process.Pid)
	}

	recoverOut := filepath.Join(t.TempDir(), "recover.json")
	recoverCmd := restoreChildCommand(restoreModeRecover, root, recoverOut, out)
	var recoverLog restoreLog
	recoverCmd.Stdout = &recoverLog
	recoverCmd.Stderr = &recoverLog
	if err := recoverCmd.Run(); err != nil {
		t.Fatalf("the %s restart process reported %v; output=%s", mode, err, recoverLog.String())
	}
	recover := readRestoreReport(t, recoverOut)
	if !recover.Ready || recover.Mode != restoreModeRecover {
		t.Fatalf("%s restart report = %+v, want a ready report of the reopen", mode, recover)
	}
	restoreRelay(t, &recoverLog, "recover")
	t.Logf("parent: the %s restart read the same root %s; the restarted pid=%d drove %d request(s)",
		mode, root, recover.PID, recover.Requests)
	return restoreOutcome{Mode: mode, Root: root, Seed: seed, Recover: recover, SeedLog: seedLog.String(), RecoverLog: recoverLog.String()}
}

// runRestoreChild dispatches one child phase of the fixture.
func runRestoreChild(t *testing.T, mode string) {
	t.Helper()
	root := os.Getenv(restoreChildRootEnv)
	out := os.Getenv(restoreChildOutEnv)
	if root == "" || out == "" {
		t.Fatalf("the restore child %q was handed no durable root or report path", mode)
	}
	switch mode {
	case restoreModeSeedGraceful, restoreModeSeedKill:
		seedRestoreProcess(t, mode, root, out)
	case restoreModeRecover:
		recoverRestoreProcess(t, root, out, os.Getenv(restoreChildSeedEnv))
	default:
		t.Fatalf("the restore child was handed an unknown mode %q", mode)
	}
}

// restoreChildCommand builds the self-exec command of one child phase over the
// report it lands and the seed report the reopen reads.
func restoreChildCommand(mode, root, out, seedOut string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestEventIntegrityRestoreUse$", "-test.timeout=10m", "-test.v")
	cmd.Env = append(os.Environ(),
		restoreChildModeEnv+"="+mode,
		restoreChildRootEnv+"="+root,
		restoreChildOutEnv+"="+out,
		restoreChildSeedEnv+"="+seedOut,
		"TAGENT_TEST_API_KEY=fixture",
		"TAGENT_HTTP_PORT=0",
	)
	return cmd
}

// restoreLog is the synchronized log one child process writes from its own
// goroutine and the parent reads: the read never races the write.
type restoreLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends one child write under the lock.
func (l *restoreLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// String reports the log written so far under the lock.
func (l *restoreLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// waitRestoreReport waits for the child's atomic report.
func waitRestoreReport(t *testing.T, path string, output *restoreLog, mode string) restoreChildReport {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		body, err := os.ReadFile(path)
		if err == nil {
			var report restoreChildReport
			if json.Unmarshal(body, &report) == nil && report.Ready && report.Mode == mode {
				return report
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the %s seed process reported no ready report at %s; output=%s", mode, path, output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// readRestoreReport reads one report a child landed atomically.
func readRestoreReport(t *testing.T, path string) restoreChildReport {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the restore report %s: %v", path, err)
	}
	var report restoreChildReport
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("decode the restore report %s: %v", path, err)
	}
	return report
}

// writeRestoreReport lands one child report atomically, so the parent never
// reads a half-written one.
func writeRestoreReport(t *testing.T, path string, report restoreChildReport) {
	t.Helper()
	report.Ready = true
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("encode the restore child report: %v", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		t.Fatalf("write the restore child report: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("land the restore child report: %v", err)
	}
}

// restoreRelay relays the raw lines a child logged under its stage marker, so
// the parent's evidence carries the child's own readings verbatim.
func restoreRelay(t *testing.T, output *restoreLog, stage string) {
	t.Helper()
	for _, line := range strings.Split(output.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "restore "+stage+":") {
			continue
		}
		t.Logf("child %s: %s", stage, line)
	}
}

// seedRestoreProcess drives the one formal turn of the fixture over the durable
// root and reports the material it committed: the graceful mode drains and
// closes the host, and the kill mode lands its report and waits for the
// parent's interruption.
func seedRestoreProcess(t *testing.T, mode, root, out string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rig := newRestoreRig(t, root)
	rig.scriptToolTurn(recall.QueryToolName, restoreCallID, restoreWatchArguments, restoreSeedAnswer)
	rig.mustInvoke(ctx, restoreSeedInvocation, restoreSeedInput)
	run := rig.host.Run()

	input := persistenceFactByContent(t, run, restoreSeedInput)
	result := persistenceFactOf(t, run, "the record of the retrieval call", func(evt event.Event) bool {
		return evt.Type == event.TypeActionCommand && evt.Message.ToolName == recall.QueryToolName
	})
	answer := persistenceFactByContent(t, run, restoreSeedAnswer)
	requests := rig.provider.snapshot()
	if len(requests) == 0 {
		t.Fatalf("the seed turn sent no model request")
	}
	call := restoreCallOf(t, requests)
	report := restoreChildReport{
		Mode:       mode,
		PID:        os.Getpid(),
		Invocation: restoreSeedInvocation,
		Address:    persistenceAddress,
		Instance:   input.InstanceID,
		Input:      input.Message.Content,
		InputFact:  input.Key,
		CallID:     call.ToolCalls[0].ID,
		CallText:   call.Content,
		ToolName:   call.ToolCalls[0].Name,
		ResultText: result.Message.Content,
		ResultFact: result.Key,
		Answer:     answer.Message.Content,
		AnswerFact: answer.Key,
		TaskID:     result.TaskID,
	}
	t.Logf("restore seed: committed input=%s result=%s answer=%s task=%s call=%s instance=%s",
		report.InputFact, report.ResultFact, report.AnswerFact, report.TaskID, report.CallID, report.Instance)
	if mode == restoreModeSeedKill {
		writeRestoreReport(t, out, report)
		t.Logf("restore seed: the report landed; the process waits for the parent's interruption")
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Minute):
		}
		return
	}
	rig.shutdown()
	t.Logf("restore seed: the host drained and closed over the durable root")
	writeRestoreReport(t, out, report)
}

// restoreCallOf reports the assistant message that issued the tool call of the
// turn: the call rides on a later request of the turn, never on the request
// that asked for it.
func restoreCallOf(t *testing.T, requests []persistenceRequest) persistenceMessage {
	t.Helper()
	for _, request := range requests {
		for _, message := range request.Messages {
			if message.Role == string(model.RoleAssistant) && len(message.ToolCalls) > 0 {
				return message
			}
		}
	}
	t.Fatalf("the seed turn carried no assistant tool call across %d request(s)", len(requests))
	return persistenceMessage{}
}

// recoverRestoreProcess reopens the same durable root through the formal Boot
// entry, reads the committed material back from the tier alone, and drives the
// next actual request, so the report is the restart's own reading and never a
// mirror of the seed's memory.
func recoverRestoreProcess(t *testing.T, root, out, seedPath string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if seedPath == "" {
		t.Fatalf("the restore reopen was handed no seed report")
	}
	seed := readRestoreReport(t, seedPath)
	rig := newRestoreRig(t, root)
	run := rig.host.Run()
	report := restoreChildReport{Mode: restoreModeRecover, PID: os.Getpid()}

	report.Restored = map[string]string{}
	for _, want := range []struct {
		what string
		key  event.EventID
	}{
		{"conversation input", seed.InputFact},
		{"tool result", seed.ResultFact},
		{"assistant output", seed.AnswerFact},
	} {
		evt, err := run.CommittedEvent(ctx, want.key)
		if err != nil {
			t.Fatalf("the restart read back no %s event %s: %v", want.what, want.key, err)
		}
		report.Restored[string(want.key)] = evt.Message.Content
		t.Logf("restore recover: the tier answered the %s event %s bytes=%d", want.what, want.key, len(evt.Message.Content))
	}
	observed, err := run.ObservedEvents(ctx, memory.QueryOptions{
		PartitionIDs: []int{memory.PartitionIDFromName(string(seed.Address.AgentID))},
		EventTypes:   []string{event.TypeActionCommand},
	})
	if err != nil {
		t.Fatalf("the restart read the observation table: %v", err)
	}
	for _, ref := range observed {
		report.Observed = append(report.Observed, string(ref.Key))
	}
	report.Recovery = consumptionRecoveryViews(run.Recovery())
	report.Invocations = restoreInvocationViews(t, ctx, run, seed.Address)
	report.Tasks = restoreTaskViews(t, ctx, run, seed.Address)
	report.Chains = restoreChainViews(t, ctx, root)

	rig.scriptTerminal(restoreAfterAnswer)
	rig.mustInvoke(ctx, restoreNextInvocation, restoreNextInput)
	requests := rig.provider.snapshot()
	report.Requests = len(requests)
	if len(requests) > 0 {
		report.Messages = requests[0].Messages
	}
	for _, message := range report.Messages {
		t.Logf("restore recover: the next request carried role=%s content=%q", message.Role, message.Content)
	}
	rig.shutdown()
	writeRestoreReport(t, out, report)
}

// newRestoreRig boots one host over a durable root named by the caller through
// the same formal entry the persistence fixture drives, so the restart opens
// the root exactly the way a deployment does.
func newRestoreRig(t *testing.T, root string) *persistenceRig {
	t.Helper()
	rig := &persistenceRig{
		t:          t,
		root:       root,
		provider:   newPersistenceChat(t),
		embeddings: newPersistenceEmbeddings(t),
		sender:     &persistenceSender{},
	}
	t.Cleanup(func() {
		if rig.host == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := rig.host.Close(ctx); err != nil {
			t.Logf("the restore host ended its close beside: %v", err)
		}
	})
	rig.boot()
	return rig
}

// restoreInvocationViews reads the invocation dimension of one restarted run.
func restoreInvocationViews(t *testing.T, ctx context.Context, run *assembly.Run, addr event.Address) []restoreInvocationView {
	t.Helper()
	snapshot, err := run.Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{addr.AgentID}, Sessions: []event.Address{addr}},
		Kinds: []query.ObjectKind{query.ObjectInvocation},
	})
	if err != nil {
		t.Fatalf("read the invocation dimension of the restart: %v", err)
	}
	views := make([]restoreInvocationView, 0, len(snapshot.Details))
	for _, detail := range snapshot.Details {
		views = append(views, restoreInvocationView{
			InvocationID: detail.Ref.InvocationID,
			Address:      detail.Ref.Address,
			Instance:     detail.Ref.InstanceID,
		})
	}
	return views
}

// restoreTaskViews reads the task dimension of one restarted run.
func restoreTaskViews(t *testing.T, ctx context.Context, run *assembly.Run, addr event.Address) []restoreTaskView {
	t.Helper()
	snapshot, err := run.Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{addr.AgentID}, Sessions: []event.Address{addr}},
		Kinds: []query.ObjectKind{query.ObjectTask},
	})
	if err != nil {
		t.Fatalf("read the task dimension of the restart: %v", err)
	}
	views := make([]restoreTaskView, 0, len(snapshot.Details))
	for _, detail := range snapshot.Details {
		views = append(views, restoreTaskView{
			TaskID:   detail.Ref.TaskID,
			Address:  detail.Ref.Address,
			Instance: detail.Ref.InstanceID,
			Status:   int(detail.State.TaskStatus),
		})
	}
	return views
}

// restoreChainViews reads every durable fact chain the restarted process can
// open from the root, beside the result identity its settled transition linked
// to the call.
func restoreChainViews(t *testing.T, ctx context.Context, root string) []restoreChainView {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("open the durable tier of the restart: %v", err)
	}
	durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("open the durable commit of the restart: %v", err)
	}
	var views []restoreChainView
	for _, taskID := range durable.TaskIDs() {
		records, err := durable.Records(ctx, taskID)
		if err != nil {
			t.Fatalf("read the durable chain %s: %v", taskID, err)
		}
		view := restoreChainView{TaskID: taskID, Links: len(records)}
		if len(records) > 0 {
			view.CallID = records[0].Spec.CallID
			view.Invocation = records[0].Spec.InvocationID
			view.Source = records[0].Spec.Source
			view.Instance = records[0].Spec.InstanceID
		}
		for _, fact := range durable.Facts(taskID) {
			for _, envelope := range fact.Outbox {
				if envelope.Notice.Kind != task.NoticeSettled {
					continue
				}
				view.ResultFact = envelope.Notice.Event
				view.ResultTask = envelope.Notice.TaskID
			}
		}
		views = append(views, view)
	}
	return views
}
