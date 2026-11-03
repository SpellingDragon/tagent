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
)

const (
	entryOwedModeEnv   = "TAGENT_ENTRY_OWED_MODE"
	entryOwedConfigEnv = "TAGENT_ENTRY_OWED_CONFIG"
	entryOwedRootEnv   = "TAGENT_ENTRY_OWED_ROOT"
	entryOwedOutEnv    = "TAGENT_ENTRY_OWED_OUT"

	entryOwedHeld       = "held"
	entryOwedAccount    = "entry-owed-sends.jsonl"
	entryOwedSending    = "sending"
	entryOwedBody       = "the answer the moved entry owes across the restart"
	entryOwedSession    = "session-entry-owed"
	entryOwedUserAgent  = "user"
	entryOwedNewEntry   = "reviewer"
	entryOwedUserModel  = "model-entry"
	entryOwedEntryModel = "model-reviewer"
)

// TestReloadMovedEntryDeliveryLandsOwedAcrossRestart pins the delivery account to the entry the generation in effect declares.
//   - a reload moves the declared entry from user to reviewer
//   - a real turn at the moved entry registers its body as a durable owed obligation before the sender runs
//   - the process is killed inside that send, and a fresh host booted over the same durable root delivers the owed body once to the entry in effect
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestReloadMovedEntryDeliveryLandsOwedAcrossRestart(t *testing.T) {
	if mode := os.Getenv(entryOwedModeEnv); mode != "" {
		entryOwedResident(t, mode)
		return
	}
	root, out := t.TempDir(), t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	child := startEntryOwedResident(t, configPath, root, out)
	t.Cleanup(func() { stopEntryOwedChild(child) })
	child.waitLedgerEvent(t, entryOwedSending)
	child.kill(t)

	records := owedDeliveryRecords(t, root)
	held := recordsInState(records, owedState)
	if len(held) != 1 {
		t.Fatalf("owed delivery obligations beside the killed process = %+v, want the one the moved entry registered", records)
	}
	if held[0].Content != entryOwedBody || held[0].Target.AgentID != entryOwedNewEntry {
		t.Fatalf("owed delivery = %+v, want %q addressed to the entry the generation declares", held[0], entryOwedBody)
	}

	viewAnchorEnv(t)
	sender := &deliveryRecorder{}
	restored, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the restarted host over the same durable root: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	waitForSettle(t, "the owed body of the moved entry to reach the restarted sender", func() bool {
		return len(sender.bodies()) >= 1
	})
	if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != entryOwedBody {
		t.Fatalf("delivered bodies after the restart = %q, want the owed body exactly once", bodies)
	}
	if deliveries := sender.deliveries(); len(deliveries) != 1 || deliveries[0].target.AgentID != entryOwedNewEntry {
		t.Fatalf("delivery targets after the restart = %+v, want the entry the generation declares", deliveries)
	}
	if err := restored.Close(ctx); err != nil {
		t.Fatalf("close the restarted host: %v", err)
	}
	settled := owedDeliveryRecords(t, root)
	if owed := recordsInState(settled, owedState); len(owed) != 0 {
		t.Fatalf("delivery obligations standing owed after the restart = %+v, want none", owed)
	}
	if sent := recordsCarrying(settled, entryOwedBody); len(sent) != 1 || sent[0].Status != sentState {
		t.Fatalf("the record of %q after the restart = %+v, want the one record settled as sent", entryOwedBody, sent)
	}
}

// entryOwedResident is the resident process the anchor kills: it boots the two
// agent roster, moves the entry to reviewer through the formal reload entry and
// drives the first turn at the new entry beside a sender that never returns.
func entryOwedResident(t *testing.T, mode string) {
	t.Helper()
	if mode != entryOwedHeld {
		t.Fatalf("unknown entry-owed work order %q", mode)
	}
	viewAnchorEnv(t)
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf(entryOwedBody) }
	configPath := os.Getenv(entryOwedConfigEnv)
	root := os.Getenv(entryOwedRootEnv)
	out := os.Getenv(entryOwedOutEnv)
	writeHostDocumentFile(t, configPath, orgRosterDocument(root, provider.endpoint(), entryOwedUserAgent,
		orgRosterAgent{ID: entryOwedUserAgent, Model: entryOwedUserModel},
		orgRosterAgent{ID: entryOwedNewEntry, Model: entryOwedEntryModel},
	))
	host, err := assembly.Boot(configPath, assembly.WithSender(&entryOwedSender{account: filepath.Join(out, entryOwedAccount)}))
	if err != nil {
		t.Fatalf("boot the resident host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	writeHostDocumentFile(t, configPath, orgRosterDocument(root, provider.endpoint(), entryOwedNewEntry,
		orgRosterAgent{ID: entryOwedUserAgent, Model: entryOwedUserModel},
		orgRosterAgent{ID: entryOwedNewEntry, Model: entryOwedEntryModel},
	))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("reload status = %d, want 200: %s", status, body)
	}
	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address: event.Address{AgentID: entryOwedNewEntry, SessionID: entryOwedSession},
		Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("invoke the moved entry: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn whose body the sender holds", turn.Terminal)
	}
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("the parent never killed the resident process")
		case <-time.After(time.Second):
		}
	}
}

// entryOwedSender is the outbound face of the resident process: it accounts for
// the body it is holding and never confirms the send, so the kill lands between
// the owed record and its receipt.
type entryOwedSender struct {
	account string
}

// Send records the started send and waits for the process to be killed.
func (s *entryOwedSender) Send(ctx context.Context, target event.Address, content string) error {
	settleDriveAppendLine(s.account, map[string]any{
		"event":  entryOwedSending,
		"target": target.SessionID,
		"agent":  target.AgentID,
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

// startEntryOwedResident starts the resident process of the moved-entry delivery
// anchor over the configuration source and durable root the parent names.
func startEntryOwedResident(t *testing.T, configPath, root, out string) *settleDriveChildProcess {
	t.Helper()
	child := &settleDriveChildProcess{ledger: filepath.Join(out, entryOwedAccount)}
	child.cmd = exec.Command(os.Args[0], "-test.run=^TestReloadMovedEntryDeliveryLandsOwedAcrossRestart$", "-test.timeout=10m")
	child.cmd.Env = append(os.Environ(),
		entryOwedModeEnv+"="+entryOwedHeld,
		entryOwedConfigEnv+"="+configPath,
		entryOwedRootEnv+"="+root,
		entryOwedOutEnv+"="+out,
	)
	child.cmd.Stderr = &child.stderr
	if err := child.cmd.Start(); err != nil {
		t.Fatalf("start the resident child: %v", err)
	}
	child.pid = child.cmd.Process.Pid
	return child
}

// stopEntryOwedChild ends a child the anchor left running, so a failed check
// never leaves a resident process behind.
func stopEntryOwedChild(child *settleDriveChildProcess) {
	if child == nil || child.cmd == nil || child.cmd.Process == nil || child.cmd.ProcessState != nil {
		return
	}
	_ = child.cmd.Process.Kill()
	_ = child.cmd.Wait()
}
