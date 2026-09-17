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
		bus:         bus, // persistBusEvent 的事实键回写依赖 bus（3.4）
	}
	return &TagentAgent{
		name:           "durable-test",
		persistentBus:  bus,
		contextManager: cm,
	}
}

func durableMsg(content string) *AgentEvent {
	return NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: content})
}

// TestDurableReceipt_ReplayNoDoubleWrite（3.4/3.6 场景 2）：入库后、receipt 前
// 崩溃 → 信封带事实键重放 → 同输入不重复入库、投影不重复追加。
func TestDurableReceipt_ReplayNoDoubleWrite(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus

	// 第一次执行：durable 接收 → claim → 事实入库（模拟 persistBusEvent 完整路径：
	// 用真实 cm.persistBusEvent 走同一代码）。
	_, err := bus.PublishContext(context.Background(), durableMsg("fact-A"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 1)
	for _, evt := range batch {
		ta.contextManager.persistBusEvent(evt)
	}
	require.Equal(t, 1, ta.contextManager.memStore.GetStats().TotalEvents)
	projKeys := keysOf(ta.contextManager.projection.GetAll())
	require.Len(t, projKeys, 1)

	// 崩溃在 receipt 前：回写事实键（persistBusEvent 内部已做），进程结束。
	require.Equal(t, int64(1), bus.DurablePending())

	// 重启：新 bus 同 dir → claim 重放，事件带 inbox_dedup_keys。
	ta2 := durableAgent(t, dir)
	replayed, err := ta2.persistentBus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, replayed, 1)
	require.NotEmpty(t, replayed[0].Metadata["inbox_dedup_key"], "replay must carry dedup evidence (singular, per-message)")

	// 重放执行：persistBusEvent 走 dedup 分支 → 不重复入库，投影幂等。
	for _, evt := range replayed {
		ta2.contextManager.persistBusEvent(evt)
	}
	require.Equal(t, 1, ta2.contextManager.memStore.GetStats().TotalEvents,
		"replayed input must NOT double-write the fact")
	require.Equal(t, projKeys, keysOf(ta2.contextManager.projection.GetAll()),
		"projection stays idempotent by key")

	// 处理完成：fact-chain receipt 落库后信封才被确认。
	ta2.finishDurableBatch(replayed)
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
			"receipt must never enter the projection (skipProjectionEvent)")
	}
}

// TestDurableReceipt_StoreFailureKeepsClaim（3.4 stored-gate 延伸）：receipt
// 事件写失败 ⇒ 信封不被确认（绝不无凭据 ack）。
func TestDurableReceipt_StoreFailureKeepsClaim(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	_, err := ta.persistentBus.PublishContext(context.Background(), durableMsg("x"))
	require.NoError(t, err)
	batch, err := ta.persistentBus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 1)

	// swap in a failing store for the receipt write.
	ta.contextManager.memStore = &failStore{memory.NewInMemoryStore()}
	ta.finishDurableBatch(batch)
	require.Equal(t, int64(1), ta.persistentBus.DurablePending(),
		"unbacked ack is forbidden — the claim must stay for replay")
}

// TestPersistBusEvent_DedupMissingSlotFallsThrough：dedup 键指向的事实缺失
// （回写后半孤儿）→ 正常 StoreEvent（同 key 同内容 = 确定性补齐）。
func TestPersistBusEvent_DedupMissingSlotFallsThrough(t *testing.T) {
	cm := &ContextManager{
		partitionID: 1,
		memStore:    memory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
	}
	evt := &AgentEvent{
		Message:   &model.Message{Role: model.RoleUser, Content: "orphan"},
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"inbox_dedup_keys": tagentevent.FormatEventKey(12345), // 不存在
		},
	}
	cm.persistBusEvent(evt)
	require.Equal(t, 1, cm.memStore.GetStats().TotalEvents, "missing dedup slot falls through to store")
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

	// T1：envelope A（单消息）接收 → claim → 事实入库+回写 → receipt 前 crash。
	recA, err := bus.PublishEnvelopeContext(context.Background(), "user",
		[]model.Message{{Role: model.RoleUser, Content: "msg-A"}})
	require.NoError(t, err)
	require.True(t, recA.Durable)
	claimedA, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, claimedA, 1)
	for _, evt := range claimedA {
		ta.contextManager.persistBusEvent(evt)
	}
	keysA := keysOf(ta.contextManager.projection.GetAll())
	require.Len(t, keysA, 1)
	require.Equal(t, int64(1), bus.DurablePending())

	// T2：进程「重启」（同 dir 新 bus）+ envelope B 入队；重放批次 = A(带
	// dedup 证据) + B(无证据)。
	ta2 := durableAgent(t, dir)
	bus2 := ta2.persistentBus
	recB, err := bus2.PublishEnvelopeContext(context.Background(), "user",
		[]model.Message{{Role: model.RoleUser, Content: "msg-B"}})
	require.NoError(t, err)
	require.True(t, recB.Durable)

	batch, err := bus2.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 2, "A replay + B first delivery in one batch")

	// 结构修路径：批次内每条 claim 事件逐消息 persistBusEvent（A 幂等、B 新键）。
	for _, evt := range batch {
		ta2.contextManager.persistBusEvent(evt)
	}
	// A 与 B 的事实都在链上（B 不得被 A 的证据误吞）。
	require.Len(t, keysOf(ta2.contextManager.projection.GetAll()), 2,
		"both A and B input facts must exist after replay")
	require.Equal(t, int64(2), bus2.DurablePending(), "both envelopes still unconfirmed")

	// 两 envelope 均获自有回写证据（A: 原键；B: 新键）——receipt+ack 不失据。
	provenance := bus2.DurableProvenance(batch)
	require.Len(t, provenance, 2, "both envelopes must be covered by provenance")

	// finishDurableBatch：两 envelope receipt+ack 收敛，pending 归零。
	ta2.finishDurableBatch(batch)
	require.Equal(t, int64(0), bus2.DurablePending(),
		"both envelopes must be receipted+acked with their facts safely on chain")
}
