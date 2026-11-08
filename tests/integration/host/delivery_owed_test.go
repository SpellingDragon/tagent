// The owed-delivery check of the host: the body a turn generates is a durable
// delivery obligation before the sender runs, so a process killed inside a send
// nobody confirmed is answered by a restart that delivers the body once, without
// driving the turn again.
package host

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// owedKillModeEnv, owedKillConfigEnv, owedKillRootEnv and owedKillOutEnv name
// the work order the resident process of the owed-delivery check reads: the
// configuration to boot and the directory its account file is written into.
const (
	owedKillModeEnv   = "TAGENT_OWED_KILL_MODE"
	owedKillConfigEnv = "TAGENT_OWED_KILL_CONFIG"
	owedKillRootEnv   = "TAGENT_OWED_KILL_ROOT"
	owedKillOutEnv    = "TAGENT_OWED_KILL_OUT"
)

// owedKillHeld names the one work order the resident process runs in.
const owedKillHeld = "held"

// owedKillAccountName names the account file the resident process appends the
// send it never finished to.
const owedKillAccountName = "owed-sends.jsonl"

// owedKillSending is the account line the resident process writes once the
// follow-up body reaches its sender and the send is left unconfirmed.
const owedKillSending = "sending"

// owedDeliveryPrefix is the durable namespace of the delivery obligations the
// host keeps on the storage tier, read back here as the evidence a restart acts
// on.
const owedDeliveryPrefix = "tagent/task/v1/delivery:"

// settleConsumePrefix is the durable namespace of the consumption receipts the
// settle account keeps, read back here as the evidence one generation obligation
// completed exactly one consumption.
const settleConsumePrefix = "tagent/task/v1/consume:"

// owedDeliveryRecord is one durable delivery obligation as the check reads it
// back from the tier: the identity, body, routing and state of one obligation.
type owedDeliveryRecord struct {
	ID      string        `json:"id"`
	Content string        `json:"content"`
	Lineage string        `json:"lineage"`
	Target  event.Address `json:"target"`
	Status  string        `json:"status"`
}

// owedState and sentState name the two record states the check reads back: the
// obligation of a generated body no sender confirmed, and the receipt of one the
// account settled.
const (
	owedState = "owed"
	sentState = "sent"
)

// TestSettleDeliversAcrossKilledSender pins the delivery obligation of a generated body across a kill inside an unconfirmed send.
//   - positive: the body is a durable owed record before its sender ran, and the restart delivers it exactly once
//   - negative: the restart drives no second follow-up turn, the generation obligation keeps its one consumption, and no obligation stands owed afterwards
//   - consumer: the boot restore reads the obligation through the host's own delivery table and sender, not a test seam
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestSettleDeliversAcrossKilledSender(t *testing.T) {
	if mode := os.Getenv(owedKillModeEnv); mode != "" {
		owedKillResident(t, mode)
		return
	}
	t.Run("a kill inside an unconfirmed send delivers the body once", runOwedDeliveryAcrossKill)
}

// owedKillResident is the resident process the owed-delivery check kills: it
// boots the configuration the parent wrote, drives the one delegating turn and
// holds the process open beside the send its sender never answers.
func owedKillResident(t *testing.T, mode string) {
	t.Helper()
	if mode != owedKillHeld {
		t.Fatalf("unknown owed-delivery work order %q", mode)
	}
	account := filepath.Join(os.Getenv(owedKillOutEnv), owedKillAccountName)
	host, err := assembly.Boot(os.Getenv(owedKillConfigEnv), assembly.WithSender(&owedKillSender{account: account}))
	if err != nil {
		t.Fatalf("boot the resident host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address: event.Address{AgentID: "user", SessionID: settleDriveSession},
		Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the total to the worker"}},
	}); err != nil {
		t.Fatalf("the delegating turn ended by error: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("the parent never killed the resident process")
		case <-time.After(time.Second):
		}
	}
}

// owedKillSender is the outbound face of the resident process: every body but
// the follow-up answer passes through, and the follow-up answer is accounted for
// as attempted and then held open, so the kill lands inside a send no channel
// confirmed.
type owedKillSender struct {
	account string
}

// Send reports the follow-up body as attempted and never confirms it.
func (s *owedKillSender) Send(ctx context.Context, target event.Address, content string) error {
	if content != settleDriveFollowUpBody {
		return nil
	}
	settleDriveAppendLine(s.account, map[string]any{
		"event":  owedKillSending,
		"pid":    os.Getpid(),
		"target": target.SessionID,
		"body":   content,
	})
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Hour):
		}
	}
}

// startOwedKillResident starts the resident process of the owed-delivery check
// over the storage root and the account directory the parent names.
func startOwedKillResident(t *testing.T, configPath, root, out string) *settleDriveChildProcess {
	t.Helper()
	child := &settleDriveChildProcess{ledger: filepath.Join(out, owedKillAccountName)}
	child.cmd = exec.Command(os.Args[0], "-test.run=^TestSettleDeliversAcrossKilledSender$", "-test.timeout=10m")
	child.cmd.Env = append(os.Environ(),
		owedKillModeEnv+"="+owedKillHeld,
		owedKillConfigEnv+"="+configPath,
		owedKillRootEnv+"="+root,
		owedKillOutEnv+"="+out,
	)
	child.cmd.Stderr = &child.stderr
	if err := child.cmd.Start(); err != nil {
		t.Fatalf("start the resident child: %v", err)
	}
	child.pid = child.cmd.Process.Pid
	return child
}

// runOwedDeliveryAcrossKill drives one delegated settle in a real process, kills
// that process inside the follow-up send, and boots the same root again: the
// obligation the killed process registered is delivered by the restart, and the
// generation obligation it already consumed drives no second turn.
func runOwedDeliveryAcrossKill(t *testing.T) {
	wakeEnv(t)

	provider := newScriptedProvider(t)
	tally := &settleDriveTally{}
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			return scriptedAnswerOf(settleDriveWorkerBody), nil
		}
		tally.mu.Lock()
		defer tally.mu.Unlock()
		if settleDriveReadsMaterial(request, settleDriveWorkerBody) {
			tally.followUps++
			return scriptedAnswerOf(settleDriveFollowUpBody), nil
		}
		tally.entryCalls++
		if tally.entryCalls == 1 {
			return wakeDelegate("call-worker", "compute the total"), nil
		}
		return scriptedAnswerOf(settleDriveEntryBody), nil
	}
	root, out := t.TempDir(), t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))

	child := startOwedKillResident(t, configPath, root, out)
	child.waitLedgerEvent(t, owedKillSending)
	child.kill(t)

	if got := settleDriveEventsNamed(child.ledger, "delivered"); got != 0 {
		t.Fatalf("confirmed deliveries in the killed process = %d, want none: the send never returned", got)
	}
	before := deliveryFacts(t, root)
	declared := deliveryDeclarations(before, settleDriveFollowUpBody)
	if len(declared) != 1 {
		t.Fatalf("owed declarations beside the killed process = %+v, want the one declaration of %q", declared, settleDriveFollowUpBody)
	}
	identity := declared[0].Identity
	if settled := deliverySettlementFactsOf(before, identity); len(settled) != 0 {
		t.Fatalf("settlement facts beside the killed process = %+v, want none: the send was never confirmed", settled)
	}
	consumed := tierKeys(t, root, settleConsumePrefix)
	if len(consumed) == 0 {
		t.Fatal("no consumption receipt stands beside the killed process, want the one the landed generation wrote")
	}

	restored, sender := bootRestoredSettleDrive(t, configPath)
	defer restored.Close(context.Background())
	waitForSettle(t, "the owed body to reach the sender", func() bool {
		return len(sender.bodies()) >= 1
	})
	time.Sleep(settleDriveQuiet)

	if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != settleDriveFollowUpBody {
		t.Fatalf("delivered bodies after the restart = %q, want the owed body exactly once", bodies)
	}
	entryCalls, followUps := tally.counts()
	if followUps != 1 {
		t.Fatalf("settlement answers after the restart = %d, want the one the delegating turn owes", followUps)
	}
	if entryCalls != 1 && entryCalls != 2 {
		t.Fatalf("decisions answered beside the settlement = %d, want the delegating decision and, only when the settlement opened a range of its own, the accept decision beside it: the kill left no second follow-up", entryCalls)
	}
	if err := restored.Close(context.Background()); err != nil {
		t.Fatalf("close the restored host: %v", err)
	}
	settled := deliveryFacts(t, root)
	if state, held := deliveryNewestState(settled, identity); !held || state != deliveryStateSent {
		t.Fatalf("the settlement state of %s after the restart = %q (held=%t): %+v, want the one record settled as sent",
			identity, state, held, deliverySettlementFactsOf(settled, identity))
	}
	if after := tierKeys(t, root, settleConsumePrefix); len(after) != len(consumed) {
		t.Fatalf("consumption receipts after the restart = %d, want the %d the killed process left: one generation obligation is consumed once", len(after), len(consumed))
	}
	t.Logf("owed-delivery kill evidence: pid=%d declared_before_restart=1 delivered_after_restart=%d settled_after_restart=sent follow_ups=1", child.pid, len(sender.bodies()))
}

// tierKeys reads every key one storage root holds under a prefix.
func tierKeys(t *testing.T, root, prefix string) map[string][]byte {
	t.Helper()
	tier, err := ledger.OpenTier(context.Background(), ledger.StorageConfig{
		Type: ledger.StorageLocalFile,
		Root: root,
	})
	if err != nil {
		t.Fatalf("open the storage tier of %s: %v", root, err)
	}
	held, err := ledger.TierFactLog(tier).Scan(context.Background(), prefix)
	if err != nil {
		t.Fatalf("scan %s in the storage tier of %s: %v", prefix, root, err)
	}
	return held
}
