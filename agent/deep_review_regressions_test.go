// 本文件负责两条已发生的回归：瞬时重排必须覆盖让位的冥想产出；信封内任一槽位不可解码即
// **整封隔离**（全部不可解码同样隔离），不得只丢那一条。
// 契约: docs/wiki/reliability/durable-delivery.md#reopen-refusal
package agent

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

// Sync Capability passthrough so the wrapper keeps the concrete backend's optional
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

// TestTransientRequeueCoversYieldedMeditation 钉住 混合批次（用户输入加让出的冥想产出）的持久提交反复失败并耗尽退避后，必须按完整已受理集重新入队全部领取。
// - 两个信封都回到待处理并重新进入消费次序；
// - 只重投被选中的子集，会让未选中的那个停在已领取状态而成僵尸。
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

	fk.failPut.Store(true)
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

// TestUndecodableAllSlotsQuarantined 钉住 pins the P2-2 all-bad branch: the envelope is isolated (bytes kept) rather than stuck claimed across every restart.
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

	require.NoError(t, bus.CloseDurable())
	_, err = NewReliableEventBus(filepath.Join(root, "inbox"))
	require.Error(t, err, "reopen must surface the undispositioned quarantine, never silently ignore it")
	require.ErrorContains(t, err, "quarantine holds undispositioned items")

	require.NoError(t, os.RemoveAll(filepath.Join(root, "inbox", "inbox-v2", "quarantine")))
	bus2, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	require.NoError(t, err)
	require.Len(t, bus2.TryPull(), 0, "a dispositioned quarantine does not re-enter consumption")
	require.NoError(t, bus2.CloseDurable())

	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
	require.NoError(t, err)
	require.Len(t, refs, 0, "quarantined input must leave no facts behind")
}

// TestUndecodableOneSlotQuarantinesWholeEnvelope 钉住 兄弟槽位解码失败时，被认领的是整个信封。
// - 可解码的那条不得继续进入一次会确认销毁其损坏孪生项的完成。
func TestUndecodableOneSlotQuarantinesWholeEnvelope(t *testing.T) {
	root := t.TempDir()
	store, bus, _ := r30Stack(root)
	defer store.Close()

	_, err := bus.PublishContext(context.Background(), durableMsg("healthy-input"))
	require.NoError(t, err)

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
