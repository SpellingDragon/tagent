package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
)

// §5.9 — the retention lease belongs to the SHARED RESOURCE OWNER (the store),
// never to any single agent: closing one agent (its durable bus) must leave
// every protection — its own un-acked material included — intact on the live
// store, and the surviving agent still releases exactly its own holders on
// ack. There is no bulk-release path anywhere; this test locks that shape.

func TestRetention_ClosingOneAgentKeepsSharedStoreLease(t *testing.T) {
	kvDir := t.TempDir()
	kvStore, err := kv.NewLocalFileKV(kvDir)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, kvDir, 100)
	require.NoError(t, err)
	store.SetRetentionLease(memory.NewRetentionLease())

	now := time.Now().UnixMilli()
	// Two agents, two inboxes, ONE shared store+lease. Each holds an un-acked
	// envelope protecting its own overdue fact original.
	type owner struct {
		bus      *EventBus
		inboxDir string
		factKey  int64
		receipt  int64
		path     string
	}
	newOwner := func(content string, overdue bool) *owner {
		o := &owner{inboxDir: t.TempDir()}
		ts := now
		if overdue {
			ts = now - 10*24*3600*1000
		}
		o.factKey = memory.NewSnowflakeEventKey(1, ts)
		o.receipt = memory.NewSnowflakeEventKey(1, now)
		require.NoError(t, store.StoreEvent(o.factKey, memory.FullEvent{
			EventKey: o.factKey, PartitionID: 1, EventType: "external_input",
			EventSummary: content, Timestamp: ts,
		}))
		bus, berr := NewReliableEventBus(o.inboxDir)
		require.NoError(t, berr)
		bus.SetRetentionGuard(store) // the SHARED resource owner's lease surface
		o.bus = bus
		in := bus.inbox
		_, eerr := in.Enqueue(&reliability.Envelope{
			RequestID: content, State: reliability.InboxStatePending,
			Messages: []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e-` + content + `","type":"external_input","message":{"role":"user","content":"x"}}`)}},
		})
		require.NoError(t, eerr)
		_, o.path, eerr = in.ClaimNext()
		require.NoError(t, eerr)
		require.NoError(t, in.PrepareFacts(o.path, tagentevent.FormatEventKey(o.receipt),
			[]json.RawMessage{json.RawMessage(`{"event_key":` + itoa(o.factKey) + `}`)}))
		require.NoError(t, bus.ArmRetentionFromInbox())
		return o
	}
	a := newOwner("agentA", true)
	b := newOwner("agentB", false)

	require.True(t, store.IsKeyProtected(a.factKey), "agentA's overdue original is leased")
	require.True(t, store.IsKeyProtected(b.factKey), "agentB's original is leased")

	// agentA CLOSES while the store survives: nothing it held is released —
	// its envelope is still un-acked (a restart must still find it protected),
	// and agentB's lease is none of the closing agent's business.
	require.NoError(t, a.bus.CloseDurable())
	require.True(t, store.IsKeyProtected(a.factKey), "closing an agent NEVER releases its un-acked material's lease")
	require.True(t, store.IsKeyProtected(b.factKey), "closing an agent NEVER touches another owner's lease")

	// The surviving owner acks normally and releases exactly ITS OWN holders.
	in := b.bus.inbox
	cred := reliability.ReceiptCredential{ReceiptKey: tagentevent.FormatEventKey(b.receipt)}
	require.NoError(t, in.RecordCompletion(b.path, json.RawMessage(`{"completion_version":1}`)))
	require.NoError(t, b.bus.ConfirmDurable(b.path, cred))
	require.False(t, store.IsKeyProtected(b.factKey), "agentB's ack released agentB's holder")
	require.False(t, store.IsKeyProtected(b.receipt), "including the receipt original")
	require.True(t, store.IsKeyProtected(a.factKey), "agentA's protection still stands for its next opener")
}
