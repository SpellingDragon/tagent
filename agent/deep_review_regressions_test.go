package agent

// Deep-review regression trio (change: deep-review-fixes, second edition after
// the reject/replay cycle) — tasks 3.2 / 4.2 / 4.3. Pinned semantics:
//   - the submitTransient EXIT ITSELF requeues over the full frozen received
//     set (driven through the real processTurn gate with an injected store
//     failure — not just the releaseBatchClaims function), so a meditation
//     yielding to a mixed batch never zombies its durable envelope;
//   - an undecodable source_event slot quarantines the WHOLE envelope (bytes
//     kept for inspection) instead of staying claimed forever (all-bad) or
//     letting the decodable siblings' completion ACK-destroy the corrupt
//     input (half-bad).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// undecodableEvent is JSON-valid (the envelope still reads at the leaf) but
// cannot decode into AgentEvent (array ≠ struct) — the post-validate anomaly
// decodeSourceEvent must refuse.
var undecodableEvent = json.RawMessage(`[9]`)

// flakyKV injects persistent KVPut failures into the store path so the real
// durable submit gate exhausts its backoff and takes the submitTransient exit.
type flakyKV struct {
	memory.KVStore
	failPut atomic.Bool
}

func (f *flakyKV) KVPut(key, value string) error {
	if f.failPut.Load() {
		return errors.New("injected transient store failure (deep-review repro)")
	}
	return f.KVStore.KVPut(key, value)
}

// Capability passthrough so the wrapper keeps the concrete backend's optional
// faces (RebuildLiveCounts probes ListPartitionIDs; the commit barrier probes
// Sync) — embedding alone hides them from the type assertions.
func (f *flakyKV) Sync() error { return f.KVStore.(interface{ Sync() error }).Sync() }
func (f *flakyKV) ListPartitionIDs() []int {
	return f.KVStore.(interface{ ListPartitionIDs() []int }).ListPartitionIDs()
}
func (f *flakyKV) KVBatch(ops []memory.KVOp) error { return f.KVStore.KVBatch(ops) }

// flakyStack mirrors r30Stack but wraps the KV so the commit phase can fail
// on demand (the prepare phase lives in the inbox files, not the store).
func flakyStack(t *testing.T, root string) (*flakyKV, *memory.FileSegmentStore, *EventBus, *TagentAgent) {
	t.Helper()
	base, err := kv.NewLocalFileKV(filepath.Join(root, "store"))
	require.NoError(t, err)
	fk := &flakyKV{KVStore: base}
	store, err := memory.NewFileSegmentStore(fk, nil, filepath.Join(root, "store"), 500)
	require.NoError(t, err)
	require.NoError(t, store.RebuildLiveCounts())
	lease := memory.NewRetentionLease()
	store.SetRetentionLease(lease)
	bus, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	require.NoError(t, err)
	bus.SetRetentionGuard(store)
	require.NoError(t, bus.ArmRetentionFromInbox())
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "flaky", persistentBus: bus, contextManager: cm}
	return fk, store, bus, ta
}

// TestTransientRequeueCoversYieldedMeditation drives the REAL submitTransient
// branch: a mixed batch (user + yielding meditation) whose durable commit
// keeps failing exhausts the gate's backoff, and the exit must requeue the
// claims over the full RECEIVED set — both envelopes return to pending and
// re-enter consumption order (P2-1: the unfixed exit requeued only the
// selected subset, zombieing the meditation's claim until a restart).
func TestTransientRequeueCoversYieldedMeditation(t *testing.T) {
	root := t.TempDir()
	fk, store, bus, ta := flakyStack(t, root)
	defer store.Close()

	_, err := bus.PublishContext(context.Background(), durableMsg("user-input"))
	require.NoError(t, err)
	med := NewExternalInputEvent("meditation", model.Message{Role: model.RoleUser, Content: "[meditation] reflect"})
	_, err = bus.PublishContext(context.Background(), med)
	require.NoError(t, err)

	received, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, received, 2)
	selected := dropMeditationFromMixedBatch(received, "flaky")
	require.Len(t, selected, 1, "precondition: meditation yielded from model input")

	fk.failPut.Store(true) // every commit attempt fails → backoff exhausts → transient
	disp := ta.processTurn(context.Background(), ta.contextManager, received)
	require.Equal(t, turnContinue, disp, "the transient exit keeps consuming; it must not stop or model")
	fk.failPut.Store(false)

	requeued := bus.TryPull()
	require.Len(t, requeued, 2, "received-scope requeue must restore BOTH envelopes to pending")
	var sawUser, sawMed bool
	for _, e := range requeued {
		if e.Source == "meditation" {
			sawMed = true
		} else {
			sawUser = true
		}
	}
	require.True(t, sawUser && sawMed, "the yielding meditation's claim was requeued with the batch")
}

// TestUndecodableAllSlotsQuarantined pins the P2-2 all-bad branch: the envelope
// is isolated (bytes kept) rather than stuck claimed across every restart.
func TestUndecodableAllSlotsQuarantined(t *testing.T) {
	root := t.TempDir()
	store, bus, _ := r30Stack(root)
	defer store.Close()

	_, err := bus.PublishContext(context.Background(), durableMsg("doomed-input"))
	require.NoError(t, err)
	tamperEnvelope(t, pendingEnvelopePath(t, root), func(env *reliability.Envelope) {
		env.Messages[0].SourceEvent = undecodableEvent
	})

	got := bus.TryPull()
	require.Len(t, got, 0, "an all-undecodable envelope contributes no events")

	q, err := filepath.Glob(filepath.Join(root, "inbox", "inbox-v2", "quarantine", "*"))
	require.NoError(t, err)
	require.Len(t, q, 1, "the envelope is quarantined (kept inspectable), not left claimed")

	// Restart shape (the protocol's honest surface): reopen REFUSES while the
	// quarantine holds undispositioned items — the corruption surfaces to the
	// operator instead of the P2-2 defect's silent claimed-zombie loop.
	require.NoError(t, bus.CloseDurable())
	_, err = NewReliableEventBus(filepath.Join(root, "inbox"))
	require.Error(t, err, "reopen must surface the undispositioned quarantine, never silently ignore it")
	require.ErrorContains(t, err, "quarantine holds undispositioned items")

	// After the operator dispositions the kept bytes, reopen succeeds and the
	// disposed input is NOT resurrected for consumption.
	require.NoError(t, os.RemoveAll(filepath.Join(root, "inbox", "inbox-v2", "quarantine")))
	bus2, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	require.NoError(t, err)
	require.Len(t, bus2.TryPull(), 0, "a dispositioned quarantine does not re-enter consumption")
	require.NoError(t, bus2.CloseDurable())

	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
	require.NoError(t, err)
	require.Len(t, refs, 0, "quarantined input must leave no facts behind")
}

// TestUndecodableOneSlotQuarantinesWholeEnvelope pins the P2-2 half-bad branch:
// a sibling slot's decode failure claims the whole envelope — the decodable
// slot does NOT sail into a completion that would ACK-destroy its corrupt twin.
func TestUndecodableOneSlotQuarantinesWholeEnvelope(t *testing.T) {
	root := t.TempDir()
	store, bus, _ := r30Stack(root)
	defer store.Close()

	_, err := bus.PublishContext(context.Background(), durableMsg("healthy-input"))
	require.NoError(t, err)

	// Duplicate slot 0 as a second (healthy) slot, then corrupt slot 0 itself:
	// one decodable, one undecodable inside ONE envelope.
	tamperEnvelope(t, pendingEnvelopePath(t, root), func(env *reliability.Envelope) {
		slot1 := env.Messages[0]
		slot1.Slot = 1
		env.Messages = append(env.Messages, slot1)
		env.Messages[0].SourceEvent = undecodableEvent
	})

	got := bus.TryPull()
	require.Len(t, got, 0, "the decodable sibling must be dropped with its envelope, not consumed")

	q, err := filepath.Glob(filepath.Join(root, "inbox", "inbox-v2", "quarantine", "*"))
	require.NoError(t, err)
	require.Len(t, q, 1, "half-bad envelope quarantines whole")

	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
	require.NoError(t, err)
	require.Len(t, refs, 0, "no slot of a quarantined envelope may be committed")
	require.Equal(t, int64(0), bus.DurablePending(), "quarantine frees the envelope's unacked capacity")
}

// pendingEnvelopePath finds the single envelope file in the inbox main dir of
// an r30Stack root (NewReliableEventBus(root/inbox) → root/inbox/inbox-v2).
func pendingEnvelopePath(t *testing.T, root string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, "inbox", "inbox-v2", "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 1, "exactly one envelope in the main inbox dir")
	return files[0]
}
