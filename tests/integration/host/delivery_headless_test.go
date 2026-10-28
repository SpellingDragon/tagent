// The headless-delivery check of the host: a body generated at an address that
// is not the entry belongs to the caller that drove it and never to a message
// channel, so no delivery obligation may carry it and no restart may push it.
package host

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// headlessSettleBody, headlessWorkerBody and headlessEntryBody are the three
// bodies the scripted provider answers with: the follow-up answer the headless
// address produces, the report of the worker it delegated to, and the answer of
// the entry address the restart must keep for itself.
const (
	headlessSettleBody = "the headless follow-up answered the settle"
	headlessWorkerBody = "the delegated worker reports the total is 11"
	headlessEntryBody  = "the entry address answered on its own"
)

// headlessSettleSession names the one session the headless-delivery checks
// drive, and headlessLegacyBody names the body of the obligation they plant for
// an address the host never addresses.
const (
	headlessSettleSession = "session-headless-settle"
	headlessLegacyBody    = "the body a process of another generation left owed"
)

// headlessState names the record state of a body retired without a send: the one
// account a headless address's owed record reaches beside a restart.
const headlessState = "headless"

// headlessAgentSections renders the three-agent surface the headless checks
// drive: the entry agent, the knowledge agent that delegates to one worker
// without being the entry, and the worker itself.
const headlessAgentSections = "  user: {}\n" +
	"  knowledge:\n" +
	"    model: model-knowledge\n" +
	"    tools:\n" +
	"      - { kind: agent, agent: worker }\n" +
	"  worker:\n" +
	"    model: model-worker\n"

// TestOwedDeliveryStaysHeadlessForNonEntryAddresses pins the entry gate on the delivery obligation itself.
//   - positive: the follow-up turn a headless address drives takes no delivery obligation, so no restart pushes its body to a channel
//   - negative: the restart over the same storage pushes none of its bodies to the sender, and a planted owed record is retired instead of sent
//   - consumer: the boot restore reads the obligation through the host's own table and sender, not a test seam
//   - the settle commits its obligation and consumption receipt right after the terminal fact, so the check waits one beat past the answer
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestOwedDeliveryStaysHeadlessForNonEntryAddresses(t *testing.T) {
	t.Run("a settle at a headless address takes no delivery obligation", runHeadlessSettleTakesNoObligation)
	t.Run("an owed record at a headless address is retired without a send", runHeadlessOwedRetiredOnBoot)
}

// runHeadlessSettleTakesNoObligation drives one delegated settle from an address
// that is not the entry: the worker settles, the notification routes back to the
// headless agent, and its follow-up turn answers. That body is delivered nowhere,
// so the restart over the same storage has nothing to push to the channel.
func runHeadlessSettleTakesNoObligation(t *testing.T) {
	wakeEnv(t)
	provider := newScriptedProvider(t)
	settled := make(chan struct{})
	var settleOnce sync.Once
	var mu sync.Mutex
	knowledgeCalls := 0
	provider.answer = func(index int) string {
		switch provider.servedModels()[index] {
		case "model-worker":
			return scriptedAnswerOf(headlessWorkerBody)
		case "model-knowledge":
			mu.Lock()
			defer mu.Unlock()
			knowledgeCalls++
			if knowledgeCalls == 1 {
				return wakeDelegate("call-worker", "compute the total")
			}
			settleOnce.Do(func() { close(settled) })
			return scriptedAnswerOf(headlessSettleBody)
		default:
			return scriptedAnswerOf(headlessEntryBody)
		}
	}
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", headlessAgentSections))
	sender := &deliveryRecorder{}
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "knowledge", SessionID: headlessSettleSession}
	if _, err := host.Invoke(ctx, tagent.InvokeRequest{
		Address: addr,
		Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the total to the worker"}},
	}); err != nil {
		t.Fatalf("drive the delegating turn at the headless address: %v", err)
	}
	select {
	case <-settled:
	case <-time.After(30 * time.Second):
		t.Fatal("the headless address never drove a follow-up turn")
	}
	waitForSettle(t, "the headless follow-up turn to land its answer", func() bool {
		for _, fact := range host.Run().SessionFacts(addr) {
			if fact.Message.Role == model.RoleAssistant && strings.TrimSpace(fact.Message.Content) == headlessSettleBody {
				return true
			}
		}
		return false
	})
	time.Sleep(time.Second)
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
	rebooted := &deliveryRecorder{}
	second, err := tagent.Boot(configPath, tagent.WithSender(rebooted))
	if err != nil {
		t.Fatalf("boot the host over the same storage: %v", err)
	}
	if bodies := rebooted.bodies(); len(bodies) != 0 {
		t.Fatalf("bodies the restart pushed = %q, want the headless settle to stay off the channel", bodies)
	}
	if err := second.Close(ctx); err != nil {
		t.Fatalf("close the restarted host: %v", err)
	}
	if carried := recordsCarrying(owedDeliveryRecords(t, root), headlessSettleBody); len(carried) != 0 {
		t.Fatalf("delivery records carrying the headless body = %+v, want no obligation at a headless address", carried)
	}
}

// runHeadlessOwedRetiredOnBoot plants the obligation a process of another
// generation left at a headless address and boots over it: the record is retired
// under its own state and nothing reaches the channel.
func runHeadlessOwedRetiredOnBoot(t *testing.T) {
	wakeEnv(t)
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf(headlessEntryBody) }
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry",
		"  user: {}\n"+
			"  knowledge: {}\n"))
	const legacyID = "delivery-fresh-legacy-headless"
	writeDeliveryRecord(t, root, owedDeliveryRecord{
		ID:      legacyID,
		Content: headlessLegacyBody,
		Lineage: "task",
		Target:  event.Address{AgentID: "knowledge", SessionID: headlessSettleSession},
		Status:  owedState,
	})
	sender := &deliveryRecorder{}
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host over the planted record: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer func() {
		if err := host.Close(ctx); err != nil {
			t.Fatalf("close the host: %v", err)
		}
	}()
	if bodies := sender.bodies(); len(bodies) != 0 {
		t.Fatalf("bodies the boot delivered = %q, want the headless obligation left off the channel", bodies)
	}
	retired := recordsCarrying(owedDeliveryRecords(t, root), headlessLegacyBody)
	if len(retired) != 1 || retired[0].Status != headlessState {
		t.Fatalf("records carrying the headless body = %+v, want the one obligation retired as %q", retired, headlessState)
	}
}

// writeDeliveryRecord commits the durable record of one delivery obligation to a
// storage root, the state a process of another generation leaves behind.
func writeDeliveryRecord(t *testing.T, root string, record owedDeliveryRecord) {
	t.Helper()
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encode the delivery record %s: %v", record.ID, err)
	}
	tier, err := ledger.OpenTier(context.Background(), ledger.StorageConfig{
		Type: ledger.StorageLocalFile,
		Root: root,
	})
	if err != nil {
		t.Fatalf("open the storage tier of %s: %v", root, err)
	}
	facts := ledger.TierFactLog(tier)
	if err := facts.Put(context.Background(), owedDeliveryPrefix+record.ID, payload); err != nil {
		t.Fatalf("stage the delivery record %s: %v", record.ID, err)
	}
	if err := facts.Sync(context.Background()); err != nil {
		t.Fatalf("commit the delivery record %s: %v", record.ID, err)
	}
}
