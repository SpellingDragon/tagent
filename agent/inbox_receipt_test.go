package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// durableAgent builds a minimal agent wired to a durable bus over dir, with a
// real InMemoryStore fact chain — enough to exercise the full
// publish → claim → dedup-replay → receipt → ack lifecycle.
func durableAgent(t *testing.T, dir string) *TagentAgent {
	t.Helper()
	bus, err := NewReliableEventBus(dir)
	require.NoError(t, err)
	cm := &ContextManager{
		partitionID: 1,
		memStore:    memory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
		bus:         bus, // prepareBatchFacts freezes prepared_fact via bus (3.4)
	}
	return &TagentAgent{
		name:           "durable-test",
		persistentBus:  bus,
		contextManager: cm,
	}
}

// durableAgentReopen reuses cm's store+projection — the durable fact chain that
// SURVIVES the restart — while opening a fresh bus over dir (the replay path).
// Sharing the store is what makes the frozen-key idempotency observable: a
// replay that re-derived a fresh key would double-write and drift the
// projection; reusing the envelope's frozen prepared_fact must not.
func durableAgentReopen(t *testing.T, dir string, cm *ContextManager) *TagentAgent {
	t.Helper()
	bus, err := NewReliableEventBus(dir)
	require.NoError(t, err)
	cm.bus = bus
	return &TagentAgent{
		name:           "durable-test",
		persistentBus:  bus,
		contextManager: cm,
	}
}

func durableMsg(content string) *AgentEvent {
	return NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: content})
}

// TestDurableReceipt_ReplayNoDoubleWrite（3.4/3.6 场景 2）：写前准备冻结事实、
// 入库后、receipt 前崩溃 → 信封带冻结的 prepared_fact + receipt_key 重放 →
// 同输入不重复入库、投影不重复追加（复用冻结键，绝不重盖 time/归因/摘要）。
func TestDurableReceipt_ReplayNoDoubleWrite(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus

	// 第一次执行：durable 接收 → claim → 写前准备（冻结 prepared_fact）→ 事实入库。
	_, err := bus.PublishContext(context.Background(), durableMsg("fact-A"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 1)
	if st, _ := ta.prepareBatchFacts(batch); st != submitOK {
		t.Fatalf("first-claim prepare must freeze the fact durably, got status %d", st)
	}
	for _, evt := range batch {
		require.True(t, ta.contextManager.persistBusEvent(evt))
	}
	require.Equal(t, 1, ta.contextManager.memStore.GetStats().TotalEvents)
	projKeys := keysOf(ta.contextManager.projection.GetAll())
	require.Len(t, projKeys, 1)

	// 崩溃在 receipt 前：信封仍 claimed（已冻结事实），进程结束。
	require.Equal(t, int64(1), bus.DurablePending())

	// 重启：新 bus 同 dir，复用同一条事实链（durable store 跨重启存活）→ claim 重放，
	// 事件携带写前准备冻结的 prepared_fact 与预留 receipt_key（typed claim，非 Metadata）。
	ta2 := durableAgentReopen(t, dir, ta.contextManager)
	replayed, err := ta2.persistentBus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, replayed, 1)
	require.NotNil(t, replayed[0].claim, "replayed input must carry a typed durable claim")
	require.NotEmpty(t, replayed[0].claim.PreparedFact, "replay must carry the frozen prepared fact (verbatim reuse)")
	require.NotEmpty(t, replayed[0].claim.ReceiptKey, "replay must carry the reserved receipt key")

	// 重放执行：prepare 复用冻结事实（不重盖）→ persistBusEvent 走 replay 去重分支
	// → 不重复入库、投影幂等、不重复 feedback。
	if st, _ := ta2.prepareBatchFacts(replayed); st != submitOK {
		t.Fatalf("replayed prepare must reuse the frozen fact, got status %d", st)
	}
	for _, evt := range replayed {
		require.True(t, ta2.contextManager.persistBusEvent(evt))
	}
	require.Equal(t, 1, ta2.contextManager.memStore.GetStats().TotalEvents,
		"replayed input must NOT double-write the fact")
	require.Equal(t, projKeys, keysOf(ta2.contextManager.projection.GetAll()),
		"projection stays idempotent by key")

	// 处理完成：§5.3 两阶段——冻结 completion → 以预留 key 提交 inbox_receipt →
	// RecordReceipt+Ack。fact-chain receipt 落库后信封才被确认。
	ta2.finishDurableBatch(context.Background(), replayed, replayed, completedOutcome())
	require.Equal(t, int64(0), ta2.persistentBus.DurablePending())
	require.Equal(t, 2, ta2.contextManager.memStore.GetStats().TotalEvents,
		"one original fact + one inbox_receipt event")

	// receipt 是事实链事件且不入投影。
	var receiptFound bool
	refs, _ := ta2.contextManager.memStore.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	for _, r := range refs {
		if r.EventType == tagentevent.TypeInboxReceipt {
			receiptFound = true
		}
	}
	require.True(t, receiptFound, "receipt must live in the fact chain (dedup window truth source)")
	for _, ref := range ta2.contextManager.projection.GetAll() {
		require.NotEqual(t, tagentevent.TypeInboxReceipt, ref.EventType,
			"receipt must never enter the projection (§5.5 event.IsNonProjectionRecord)")
	}
}

// receiptFaultStore serves the fact chain normally but refuses EXPLICIT replay
// commits for the internal inbox-receipt event. §5.3 submits the receipt through the
// replay interface (not the old fresh-key StoreEvent), so receipt-commit durability
// failure must retain the claim — the ack is never granted without its receipt.
type receiptFaultStore struct {
	*memory.InMemoryStore
}

func (s *receiptFaultStore) ReplayEvent(key int64, e memory.FullEvent) (memory.ReplayResult, memory.FullEvent, error) {
	if e.EventType == tagentevent.TypeInboxReceipt {
		return 0, memory.FullEvent{}, errors.New("receipt replay disk full")
	}
	return s.InMemoryStore.ReplayEvent(key, e)
}

// TestDurableReceipt_StoreFailureKeepsClaim（3.4 stored-gate 延伸 / §5.3 Phase B）：
// receipt 事件提交失败 ⇒ 信封不被确认（绝不无凭据 ack）。prepare 冻结事实+预留 key
// 使 completion 可成形，再让 receipt 的显式重放失败，验证 claim 保留。
func TestDurableReceipt_StoreFailureKeepsClaim(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	_, err := ta.persistentBus.PublishContext(context.Background(), durableMsg("x"))
	require.NoError(t, err)
	batch, err := ta.persistentBus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 1)
	if st, _ := ta.prepareBatchFacts(batch); st != submitOK {
		t.Fatalf("prepare must freeze fact + reserve receipt key, got %d", st)
	}

	// swap in a store that refuses the receipt's replay commit.
	ta.contextManager.memStore = &receiptFaultStore{InMemoryStore: memory.NewInMemoryStore()}
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(1), ta.persistentBus.DurablePending(),
		"unbacked ack is forbidden — the claim must stay for replay")
}

// TestPersistBusEvent_UnpreparedClaimGates（D2「准备失败不调用 StoreEvent」）：
// 带 durable claim 但 prepared_fact 尚未冻结（写前准备屏障未成功）→ 不写任何事实、
// 不追加投影（返回 false，claim 保留待重放）；而无 claim 的 volatile 事件照常入库。
func TestPersistBusEvent_UnpreparedClaimGates(t *testing.T) {
	cm := &ContextManager{
		partitionID: 1,
		memStore:    memory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
	}
	// A claim whose barrier did not freeze a fact must never enter the fact chain.
	gated := &AgentEvent{
		Message:   &model.Message{Role: model.RoleUser, Content: "half"},
		Timestamp: time.Now(),
		claim:     &durableClaim{Path: "/x/1.json", RequestID: "req-half", Slot: 0},
	}
	require.False(t, cm.persistBusEvent(gated), "unprepared claim must be gated (no half-written fact)")
	require.Equal(t, 0, cm.memStore.GetStats().TotalEvents, "gated claim writes nothing")
	require.Equal(t, 0, cm.projection.Len(), "gated claim appends nothing")

	// A volatile (claim-less) event still stores normally.
	evt := &AgentEvent{
		Message:   &model.Message{Role: model.RoleUser, Content: "orphan"},
		Timestamp: time.Now(),
	}
	require.True(t, cm.persistBusEvent(evt), "claim-less volatile event stores through the default branch")
	require.Equal(t, 1, cm.memStore.GetStats().TotalEvents)
	require.Equal(t, 1, cm.projection.Len())
}

func keysOf(refs []memory.EventReference) []int64 {
	out := make([]int64, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.EventKey)
	}
	return out
}

var _ = errors.New

// TestDurableReceipt_MultiEnvelopeBatchReplay（cold-eyes R2 M-1 回归）：跨
// crash 的多 envelope 批次——A 回放（带 dedup 证据）+ B 首次（无证据）同批
// 到达时，两者的输入事实都必须落库、两个 envelope 都拿到自有回写证据，
// receipt+ack 后不得有任何一方丢失。
func TestDurableReceipt_MultiEnvelopeBatchReplay(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus

	// T1：envelope A（单消息）接收 → claim → 写前准备冻结事实 → 入库 → receipt 前 crash。
	recA, err := bus.PublishEnvelopeContext(context.Background(), "user",
		[]model.Message{{Role: model.RoleUser, Content: "msg-A"}})
	require.NoError(t, err)
	require.True(t, recA.Durable)
	claimedA, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, claimedA, 1)
	if st, _ := ta.prepareBatchFacts(claimedA); st != submitOK {
		t.Fatalf("claimedA prepare must freeze the fact durably, got status %d", st)
	}
	for _, evt := range claimedA {
		require.True(t, ta.contextManager.persistBusEvent(evt))
	}
	keysA := keysOf(ta.contextManager.projection.GetAll())
	require.Len(t, keysA, 1)
	require.Equal(t, int64(1), bus.DurablePending())

	// T2：进程「重启」（同 dir 新 bus）+ envelope B 入队；重放批次 = A(带冻结
	// prepared_fact/receipt_key) + B(首次，尚未准备)。
	ta2 := durableAgent(t, dir)
	bus2 := ta2.persistentBus
	recB, err := bus2.PublishEnvelopeContext(context.Background(), "user",
		[]model.Message{{Role: model.RoleUser, Content: "msg-B"}})
	require.NoError(t, err)
	require.True(t, recB.Durable)

	batch, err := bus2.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 2, "A replay + B first delivery in one batch")

	// 写前准备：A 复用冻结事实（不重盖）、B 首次冻结；随后逐条 persistBusEvent。
	if st, _ := ta2.prepareBatchFacts(batch); st != submitOK {
		t.Fatalf("replayed prepare must reuse the frozen fact, got status %d", st)
	}
	for _, evt := range batch {
		require.True(t, ta2.contextManager.persistBusEvent(evt))
	}
	// A 与 B 的事实都在链上（B 不得被 A 的证据误吞）。
	require.Len(t, keysOf(ta2.contextManager.projection.GetAll()), 2,
		"both A and B input facts must exist after replay")
	require.Equal(t, int64(2), bus2.DurablePending(), "both envelopes still unconfirmed")

	// 两 envelope 均获自有回写证据（A: 原键；B: 新键）——receipt+ack 不失据。
	provenance := bus2.DurableProvenance(batch)
	require.Len(t, provenance, 2, "both envelopes must be covered by provenance")

	// finishDurableBatch：两 envelope receipt+ack 收敛，pending 归零。
	ta2.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(0), bus2.DurablePending(),
		"both envelopes must be receipted+acked with their facts safely on chain")
}
