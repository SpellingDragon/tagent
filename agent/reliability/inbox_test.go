// 本文件负责持久投递的信封契约：三段状态的迁移、瞬时失败退避不消耗输入、确定性冲突
// 全有或全无，以及重开时对"当前格式隔离项"与"前代格式惰性数据"截然不同的处理。
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
package reliability

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// srcEvent builds a minimal opaque source_event payload for a slot. The
// reliability leaf treats it as raw JSON — only the agent layer knows the
// AgentEvent schema — so any non-empty JSON is a valid lossless snapshot here.
func srcEvent(id, content string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"id": id, "content": content})
	return b
}

func env(n, src string) *Envelope {
	return &Envelope{RequestID: n, Source: src, State: InboxStatePending,
		Messages: []MessageSlot{{Slot: 0, SourceEvent: srcEvent(n, "m-"+n)}}}
}

func mustEnqueue(t *testing.T, in *Inbox, e *Envelope) int64 {
	t.Helper()
	seq, err := in.Enqueue(e)
	require.NoError(t, err)
	return seq
}

// finish drives a claimed envelope through the two-phase order (reserve →
// completion → credentialed receipt → ack) that D3 step 7 mandates. Used by
// lifecycle tests that just want to retire an envelope.
func finish(t *testing.T, in *Inbox, path string) {
	t.Helper()
	cred := reserveCredential(t, in, path)
	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(path, cred))
	require.NoError(t, in.Ack(path))
}

// reserveCredential mirrors the production two-phase protocol at the leaf: it
// reserves a receipt key via PrepareFacts when the envelope has none yet and
// returns the matching  verified receipt credential.
func reserveCredential(t *testing.T, in *Inbox, path string) ReceiptCredential {
	t.Helper()
	e, err := readEnvelope(path)
	require.NoError(t, err)
	key := e.ReceiptKey
	if key == "" {
		key = "rk-" + filepath.Base(path)
		require.NoError(t, in.PrepareFacts(path, key, make([]json.RawMessage, len(e.Messages))))
	}
	return ReceiptCredential{ReceiptKey: key}
}

// claimUntil claims envelopes in order until the named one shows up,
// finishing the older ones to keep the scan deterministic.
func claimUntil(t *testing.T, in *Inbox, id string) (*Envelope, string) {
	t.Helper()
	for {
		e, p, err := in.ClaimNext()
		require.NoError(t, err)
		require.NotNil(t, e)
		if e.RequestID == id {
			return e, p
		}
		finish(t, in, p)
	}
}

func TestInbox_EnqueueClaimAck_BasicOrder(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("a", "user"))
	mustEnqueue(t, in, env("b", "user"))
	require.Equal(t, int64(2), in.Pending())

	e1, p1, err := in.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "a", e1.RequestID)
	require.Equal(t, InboxStateClaimed, e1.State)
	require.Equal(t, int64(2), in.Pending(), "claim does not remove")

	require.Error(t, in.Ack(p1), "ack refuses non-receipted claim")

	cred1 := reserveCredential(t, in, p1)
	require.NoError(t, in.RecordCompletion(p1, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(p1, cred1))
	require.NoError(t, in.Ack(p1))
	require.Equal(t, int64(1), in.Pending())
	require.NoError(t, in.Ack(p1))

	e2, p2, err := in.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "b", e2.RequestID)
	finish(t, in, p2)
	require.Equal(t, int64(0), in.Pending())
}

func TestInbox_FullIsExplicitRejection(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 2)
	require.NoError(t, err)
	mustEnqueue(t, in, env("1", "user"))
	mustEnqueue(t, in, env("2", "user"))
	_, err = in.Enqueue(env("3", "user"))
	require.True(t, errors.Is(err, ErrInboxFull), "overflow must be typed, got: %v", err)
}

func TestInbox_Reopen_RequeuesClaimed_KeepsReceipted(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("a", "user"))
	_, pA := claimUntil(t, in, "a")

	mustEnqueue(t, in, env("b", "user"))
	_, pB := claimUntil(t, in, "b")
	credB := reserveCredential(t, in, pB)
	require.NoError(t, in.RecordCompletion(pB, json.RawMessage(`{}`)))
	require.NoError(t, in.RecordReceipt(pB, credB))

	mustEnqueue(t, in, env("c", "user"))
	require.NoError(t, in.Close())

	in2, err := NewInbox(dir, 10)
	require.NoError(t, err)
	require.Equal(t, int64(3), in2.Pending())

	got1, _, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "a", got1.RequestID, "claimed-but-unreceipted replays")
	require.GreaterOrEqual(t, got1.Attempts, 2, "requeue + re-claim each count one attempt")

	got2, pB2, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "b", got2.RequestID, "a settled receipted envelope is surfaced, not hidden")
	require.Equal(t, InboxStateReceipted, got2.State, "returned as-is: not re-claimed")
	require.NoError(t, in2.Ack(pB2))

	got3, _, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "c", got3.RequestID)
	require.Equal(t, int64(2), in2.Pending(), "b acked (-1); a and c claimed but files kept until ack")
	_ = pA
	_ = pB
}

func TestInbox_CorruptItemQuarantined(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("good", "user"))
	corrupt := filepath.Join(in.Dir(), "00000000000000000002.json")
	require.NoError(t, os.WriteFile(corrupt, []byte("{not json"), 0o644))

	require.NoError(t, in.Close())
	in2, err := NewInbox(dir, 10)
	require.NoError(t, err)
	e, _, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "good", e.RequestID)
	_, statErr := os.Stat(filepath.Join(in.Dir(), inboxQuarantine, "00000000000000000002.json"))
	require.NoError(t, statErr, "corrupt item kept in quarantine, not destroyed")
}

// TestInbox_UnknownVersionQuarantined 钉住 a v1-shaped (no version) or a v3 file is never consumed as a valid input — it is quarantined and alerted .
func TestInbox_UnknownVersionQuarantined(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("good", "user"))
	v1 := filepath.Join(in.Dir(), "00000000000000000005.json")
	require.NoError(t, os.WriteFile(v1,
		[]byte(`{"request_id":"old","source":"user","state":"pending","messages":[{"role":"user","content":"x"}]}`), 0o644))

	require.NoError(t, in.Close())
	in2, err := NewInbox(dir, 10)
	require.NoError(t, err)
	e, _, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "good", e.RequestID, "v1 item is never consumed")
	_, statErr := os.Stat(filepath.Join(in.Dir(), inboxQuarantine, "00000000000000000005.json"))
	require.NoError(t, statErr, "unknown-version item quarantined, not destroyed")
}

// TestInbox_EnqueueStampsVersionAndSlots 钉住 受理时盖上当前版本，并分配固定的递增槽位号。
// - 后续领取与准备的重写不得重排槽位号：序号不可压紧。
func TestInbox_EnqueueStampsVersionAndSlots(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	e := &Envelope{RequestID: "batch", Source: "user",
		Messages: []MessageSlot{{SourceEvent: srcEvent("a", "A")}, {SourceEvent: srcEvent("b", "B")}}}
	_, err = in.Enqueue(e)
	require.NoError(t, err)

	claimed, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, envelopeVersion, claimed.Version)
	require.Len(t, claimed.Messages, 2)
	require.Equal(t, 0, claimed.Messages[0].Slot)
	require.Equal(t, 1, claimed.Messages[1].Slot)

	require.NoError(t, in.PrepareFacts(path, "deadbeef",
		[]json.RawMessage{json.RawMessage(`{"k":1}`), json.RawMessage(`{"k":2}`)}))
	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{}`)))
	got, err := readEnvelope(path)
	require.NoError(t, err)
	require.Equal(t, 0, got.Messages[0].Slot)
	require.Equal(t, 1, got.Messages[1].Slot)
	require.Equal(t, json.RawMessage(`{"k":1}`), got.Messages[0].PreparedFact)
}

// TestInbox_EnqueueRejectsEmptySourceEvent 钉住 a slot without a source_event is refused at acceptance — the inbox never accepts silently-lossy input .
func TestInbox_EnqueueRejectsEmptySourceEvent(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	_, err = in.Enqueue(&Envelope{RequestID: "x", Source: "user",
		Messages: []MessageSlot{{Slot: 0}}})
	require.Error(t, err)
	require.Equal(t, int64(0), in.Pending(), "rejected input leaves no durable item")
}

// TestInbox_PrepareFacts_freezesAndConflicts 钉住 准备阶段预留回执键与逐槽事实，相同内容的重做保持幂等。
// - 回执键冲突或已冻结事实不一致一律拒绝。
func TestInbox_PrepareFacts_freezesAndConflicts(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("p", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)

	facts := []json.RawMessage{json.RawMessage(`{"a":1}`)}
	require.NoError(t, in.PrepareFacts(path, "key-1", facts))
	require.NoError(t, in.PrepareFacts(path, "key-1", facts), "identical re-prepare is idempotent")

	require.ErrorIs(t, in.PrepareFacts(path, "key-2", facts), ErrReceiptKeyConflict,
		"a different receipt_key is a conflict")
	require.Error(t, in.PrepareFacts(path, "key-1", []json.RawMessage{json.RawMessage(`{"a":2}`)}),
		"a different prepared fact is a conflict")
}

// TestInbox_RecordCompletion_idempotentConflict 钉住 the frozen completion cannot be overwritten with a different payload (D3 step 8).
func TestInbox_RecordCompletion_idempotentConflict(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("c", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)

	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"result":"completed"}`)), "idempotent")
	require.ErrorIs(t, in.RecordCompletion(path, json.RawMessage(`{"result":"failed"}`)), ErrCompletionConflict)
}

// TestInbox_LegacySpillIsInertNotBlocking 钉住 前代格式数据不阻止启动：收件箱以当前格式打开，把前代项分类为惰性过渡数据（不读取、不消费），直到显式受管重置才清除。
func TestInbox_LegacySpillIsInertNotBlocking(t *testing.T) {
	dir := t.TempDir()
	spill := filepath.Join(dir, "00000000000000000009.spill")
	require.NoError(t, os.WriteFile(spill, []byte("{}"), 0o644))
	in, err := NewInbox(dir, 10)
	require.NoError(t, err, "legacy .spill must NOT block boot on the current format")
	sp, _ := in.TransitionalData()
	require.Contains(t, sp, spill, "stray .spill is classified as transitional, not consumed")
	require.NoError(t, in.Close())
	_, statErr := os.Stat(spill)
	require.NoError(t, statErr, "classification is read-only: the legacy file is left untouched")
}

func TestInbox_LegacyV1DirIsInertNotBlocking(t *testing.T) {
	dir := t.TempDir()
	v1 := filepath.Join(dir, "inbox-v1")
	require.NoError(t, os.MkdirAll(v1, 0o755))
	item := filepath.Join(v1, "00000000000000000001.json")
	require.NoError(t, os.WriteFile(item, []byte("{}"), 0o644))
	in, err := NewInbox(dir, 10)
	require.NoError(t, err, "leftover inbox-v1 items must NOT block boot")
	_, v1s := in.TransitionalData()
	require.Contains(t, v1s, item, "v1 item is classified as transitional, never guess-migrated")
	require.Equal(t, int64(0), in.Pending(), "v1 items are never consumed into v2 pending")
	require.NoError(t, in.Close())
}

// TestInbox_EmptyV1DirDoesNotBlock 钉住 已排空的前代目录不构成阻塞：门禁看的是遗留项而非目录是否存在，排空后的升级照常推进。
func TestInbox_EmptyV1DirDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "inbox-v1"), 0o755))
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	require.NotNil(t, in)
}

// TestInbox_QuarantineUndispositionedBlocksReopen 钉住 存在未被处置的隔离项时不得重开发信：处置属运维动作。
// - 每次启动都静默忽略它们，等于把数据丢失藏起来。
func TestInbox_QuarantineUndispositionedBlocksReopen(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	qdir := filepath.Join(in.Dir(), inboxQuarantine)
	require.NoError(t, os.WriteFile(filepath.Join(qdir, "00000000000000000007.json"), []byte("{bad"), 0o644))
	require.NoError(t, in.Close())

	_, err = NewInbox(dir, 10)
	require.True(t, errors.Is(err, ErrQuarantineUndispositioned), "undispositioned quarantine must fail loud, got: %v", err)
}

// TestInbox_TransitionalClassificationIsReadOnly 钉住 分类是只读的：以当前格式打开不得改动或删除残留的前代格式项（只有显式受管重置才可以），残留 .spill 文件必须逐字节不变。
func TestInbox_TransitionalClassificationIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	spill := filepath.Join(dir, "00000000000000000009.spill")
	require.NoError(t, os.WriteFile(spill, []byte("keepme"), 0o644))
	_, err := NewInbox(dir, 10)
	require.NoError(t, err)
	got, rerr := os.ReadFile(spill)
	require.NoError(t, rerr, "classification must leave the legacy item untouched (read-only)")
	require.Equal(t, "keepme", string(got))
}

func TestInbox_ConcurrentEnqueue_NoOvertakeNoLoss(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 0)
	require.NoError(t, err)
	const G, N = 10, 10
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < N; i++ {
				_, err := in.Enqueue(env(fmt.Sprintf("g%02d-i%02d", g, i), "user"))
				require.NoError(t, err)
			}
		}(g)
	}
	wg.Wait()
	require.Equal(t, int64(G*N), in.Pending())
	seen := 0
	for {
		e, p, err := in.ClaimNext()
		require.NoError(t, err)
		if e == nil {
			break
		}
		finish(t, in, p)
		seen++
	}
	require.Equal(t, G*N, seen, "every durable input must survive concurrency")
}

// TestInbox_ReceiptAndAckRequireDurableCompletion 钉住 状态门：回执与确认都不得绕过持久的完成记录。
// - 从未冻结过完成的已领取信封必须被拒绝记录回执，且状态不被改动——裸的状态迁移不能顶替处理证据；
// - 完成持久成功、且预留与经验证凭据齐备时，同一凭据可收敛；
// - 仅凭状态串就删除"有回执但缺完成"的矛盾项是禁止的：拒绝确认并原样保留。
func TestInbox_ReceiptAndAckRequireDurableCompletion(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)

	mustEnqueue(t, in, env("gate-1", "user"))
	_, p, err := in.ClaimNext()
	require.NoError(t, err)

	require.ErrorContains(t, in.RecordReceipt(p, ReceiptCredential{ReceiptKey: "whatever"}), "no durable completion")
	envAfter, rerr := readEnvelope(p)
	require.NoError(t, rerr)
	require.Equal(t, InboxStateClaimed, envAfter.State, "a refused receipt must not advance the state")

	require.NoError(t, in.RecordCompletion(p, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(p, reserveCredential(t, in, p)))
	require.NoError(t, in.Ack(p))

	mustEnqueue(t, in, env("gate-2", "user"))
	_, p2, err := in.ClaimNext()
	require.NoError(t, err)
	cred2 := reserveCredential(t, in, p2)
	require.NoError(t, in.RecordCompletion(p2, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(p2, cred2))
	bad, rerr := readEnvelope(p2)
	require.NoError(t, rerr)
	bad.Completion = nil
	_, werr := in.writeEnvelopeFile(p2, bad)
	require.NoError(t, werr)
	require.ErrorContains(t, in.Ack(p2), "without a durable completion")
	require.FileExists(t, p2, "a contradictory receipted item is kept for inspection, never deleted per status string")
}

// TestInbox_RecordReceiptRequiresVerifiedCredential 钉住 凭据门：即便已有持久化的合法完成，RecordReceipt 仍须拒绝三种情形，且任何拒绝都不得推进状态。
// - ①从未建立两阶段预留的信封、②空凭据、③凭据键与信封预留回执键不符（裸备注串或请求 id 证明不了任何事）；
// - 只有凭据完全匹配才收敛。
func TestInbox_RecordReceiptRequiresVerifiedCredential(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("cred-1", "user"))
	_, p, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.RecordCompletion(p, json.RawMessage(`{"result":"completed"}`)))

	require.ErrorContains(t, in.RecordReceipt(p, ReceiptCredential{ReceiptKey: "rk"}), "no reserved receipt key")

	cred := reserveCredential(t, in, p)
	require.NotEmpty(t, cred.ReceiptKey)

	require.ErrorContains(t, in.RecordReceipt(p, ReceiptCredential{}), "credential key")
	require.ErrorContains(t, in.RecordReceipt(p, ReceiptCredential{ReceiptKey: "some-OTHER-key"}), "does not match")
	e, rerr := readEnvelope(p)
	require.NoError(t, rerr)
	require.Equal(t, InboxStateClaimed, e.State, "a refused receipt must keep the claim, never confirm on nothing")

	require.NoError(t, in.RecordReceipt(p, cred))
	require.NoError(t, in.Ack(p))
	require.Equal(t, int64(0), in.Pending())
}

// TestInbox_PublishUncertainRetainsOriginalAndNoOverwrite 钉住 重命名已落地而目录同步失败属"发布不确定"：原件必须保留、预留容量继续占有、序号不得回滚复用。
// - 回滚序号会让下一条输入写进同一路径，把已落地的原件覆盖掉。
// 契约: docs/wiki/reliability/durable-delivery.md#release-claim-backoff
func TestInbox_PublishUncertainRetainsOriginalAndNoOverwrite(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)

	origSync := syncDirFunc
	syncDirFunc = func(string) error { return errors.New("injected dirsync failure") }
	seq1, err1 := in.Enqueue(env("u1", "user"))
	syncDirFunc = origSync

	require.Error(t, err1, "a publish-uncertain write must NOT report durable acceptance")
	require.Equal(t, int64(0), seq1, "an unaccepted receive reports no sequence")

	require.Equal(t, int64(1), in.Pending(),
		"the durable original left by an uncertain write keeps its reserved capacity")
	env1Path := filepath.Join(in.dir, "00000000000000000001.json")
	e1, rerr := readEnvelope(env1Path)
	require.NoError(t, rerr, "the original file must still exist at its sequence path")
	require.Equal(t, "u1", e1.RequestID, "the retained original must be u1")

	seq2, err2 := in.Enqueue(env("u2", "user"))
	require.NoError(t, err2)
	require.Greater(t, seq2, seq1, "the next input gets a fresh, higher sequence")
	require.NotEqual(t, seq2, int64(1), "sequence 1 is reserved by the uncertain original and must not be reused")

	e1again, rerr2 := readEnvelope(env1Path)
	require.NoError(t, rerr2, "the retained original must survive the next enqueue")
	require.Equal(t, "u1", e1again.RequestID, "u2 must NOT overwrite u1's original (no sequence reuse)")

	require.Equal(t, int64(2), in.Pending(), "both the retained uncertain original and u2 occupy capacity")
}

// ackToReceipted walks one envelope through the lifecycle to the receipted state so
// Ack can be exercised.
func ackToReceipted(t *testing.T, in *Inbox) string {
	t.Helper()
	mustEnqueue(t, in, env("r-ack", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.PrepareFacts(path, "rk",
		[]json.RawMessage{json.RawMessage(`{"event_key":555,"event_summary":"s","prepared_version":1}`)}))
	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(path, ReceiptCredential{ReceiptKey: "rk"}))
	return path
}

func TestAck_UncertainDirSyncRetainsCapacityThenRetryCompletesBarrier(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	path := ackToReceipted(t, in)
	require.Equal(t, int64(1), in.Pending())

	orig := syncDirFunc
	defer func() { syncDirFunc = orig }()

	syncDirFunc = func(string) error { return errors.New("dir sync boom") }
	aerr := in.Ack(path)
	require.Error(t, aerr, "an uncertain cleanup (sync failed) must not report success")
	_, statErr := os.Stat(path)
	require.True(t, os.IsNotExist(statErr), "the file was unlinked")
	require.Equal(t, int64(1), in.Pending(),
		"capacity must be RETAINED while the removal barrier is unconfirmed (§3.6, no premature release)")

	syncDirFunc = orig
	require.NoError(t, in.Ack(path), "retry of an owed cleanup must complete the barrier")
	require.Equal(t, int64(0), in.Pending(), "capacity released once the barrier is durable")

	require.NoError(t, in.Ack(path))
	require.Equal(t, int64(0), in.Pending())
}

func TestAck_OwedBarrierStillFailingKeepsCapacityAndLease(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	path := ackToReceipted(t, in)

	orig := syncDirFunc
	defer func() { syncDirFunc = orig }()
	syncDirFunc = func(string) error { return errors.New("dir sync boom") }

	require.Error(t, in.Ack(path))
	require.Equal(t, int64(1), in.Pending())
	require.Len(t, in.DrainCleanups(), 0,
		"a drain that cannot sync must keep the account owed")
	require.Equal(t, int64(1), in.Pending())

	syncDirFunc = orig
	released := in.DrainCleanups()
	require.Len(t, released, 1, "drain completes the owed barrier and returns the protected material")
	require.Contains(t, released[0].FactKeys, int64(555),
		"the returned material must carry the frozen fact key so the caller releases the right lease (§2.8)")
	require.Equal(t, int64(0), in.Pending(), "capacity released once by the drain")
	require.Len(t, in.DrainCleanups(), 0, "no double release on a subsequent drain")
}

// TestInbox_EnqueueCoordinatesWithCompletedClose 钉住 接收发布与关闭必须在同一把锁下协调：liveness 快检与一次已完成的 Close 竞争时，Enqueue 不得在 Close 返回后再新增未登记项（经测试钩子确定性交错两条路径）。
func TestInbox_EnqueueCoordinatesWithCompletedClose(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	defer func() { testGateHook = nil }()

	var closeDone bool
	testGateHook = func() {
		_ = in.Close()
		closeDone = true
	}

	_, err = in.Enqueue(env("raced", "user"))

	require.True(t, closeDone, "Close completed during the enqueue's pre-lock window")
	require.ErrorIs(t, err, ErrInboxClosed,
		"an enqueue that raced a completed Close must be refused, never registered after Close returned")
	require.Equal(t, int64(0), in.Pending(), "no item may be registered after Close returned")
}

// TestInbox_RecordCompletion_IdempotentRetryRerunsBarrier 钉住 相同的 prepare/完成重试仍须补齐所欠的持久化屏障，不得因内容已一致就提前成功——首次可能重命名落地而目录同步失败，屏障依然欠着。
func TestInbox_RecordCompletion_IdempotentRetryRerunsBarrier(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("rc", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)

	payload := json.RawMessage(`{"result":"completed"}`)

	orig := syncDirFunc
	var failOnce atomic.Bool
	var syncCalls atomic.Int64
	failOnce.Store(true)
	syncDirFunc = func(d string) error {
		syncCalls.Add(1)
		if failOnce.Load() {
			return errors.New("injected dirsync failure")
		}
		return orig(d)
	}

	err1 := in.RecordCompletion(path, payload)
	require.Error(t, err1, "first completion must surface the publish-uncertain failure")
	require.Greater(t, syncCalls.Load(), int64(0))

	failOnce.Store(false)
	syncCalls.Store(0)
	err2 := in.RecordCompletion(path, payload)
	syncDirFunc = orig

	require.NoError(t, err2, "identical re-completion succeeds once the barrier is re-run")
	require.Greater(t, syncCalls.Load(), int64(0),
		"an identical completion retry MUST re-run the durable barrier, not early-return on content equality")
}

// TestInbox_RecordCompletion_DifferentPayloadStillConflicts 钉住 A DIFFERENT completion payload still conflicts (must not be softened by the barrier-retry fix).
func TestInbox_RecordCompletion_DifferentPayloadStillConflicts(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("rc2", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)

	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"result":"completed"}`)))
	require.ErrorIs(t, in.RecordCompletion(path, json.RawMessage(`{"result":"failed"}`)), ErrCompletionConflict)
}

// TestInbox_PrepareFactsStampsCurrentVersion 钉住 冻结槽必须盖上当前的准备版本，使后续恢复能验明它所重放的材料。
func TestInbox_PrepareFactsStampsCurrentVersion(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("r1", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.PrepareFacts(path, "rk-1",
		[]json.RawMessage{json.RawMessage(`{"event_key":111,"event_summary":"s"}`)}),
		"a no-material pending envelope must run its normal first prepare")

	reopened, err := readEnvelope(path)
	require.NoError(t, err)
	require.Equal(t, PreparedVersionCurrent, reopened.Messages[0].PreparedVersion,
		"prepare must stamp the current prepare version")
	require.NotEmpty(t, reopened.Messages[0].PreparedFact)
}

// TestInbox_ReadEnvelopeRejectsIncompatiblePreparedVersion 钉住 槽内材料带着非当前（此处为缺失或零）准备版本时属不兼容数据：读取必须拒绝并转入隔离。
// - 绝不当作已准备好而静默消费。
func TestInbox_ReadEnvelopeRejectsIncompatiblePreparedVersion(t *testing.T) {
	dir := t.TempDir()
	body := `{"version":2,"request_id":"r","source":"user","state":"claimed","receipt_key":"k",` +
		`"messages":[{"slot":0,"source_event":{"id":"e","content":"x"},` +
		`"prepared_fact":{"event_key":7,"event_summary":"s"}}]}`
	p := writeRawEnvelope(t, dir, "00000000000000000002.json", body)
	_, err := readEnvelope(p)
	require.Error(t, err, "material under an incompatible prepare version must be rejected, not consumed")
	require.Contains(t, err.Error(), "version")
}

const (
	crashAtEnv  = "TAGENT_CRASH_AT"
	crashDirEnv = "TAGENT_CRASH_DIR"
)

// crashReqID crashSlots builds a fixed 2-slot batch under a request id known to BOTH the
// child and the parent (identity reconciliation needs no IPC).
func crashReqID() string { return "crash-req" }

func crashEnvelope() *Envelope {
	return &Envelope{RequestID: crashReqID(), Source: "user", State: InboxStatePending,
		Messages: []MessageSlot{
			{Slot: 0, SourceEvent: srcEvent("c0", "crash-slot-0")},
			{Slot: 1, SourceEvent: srcEvent("c1", "crash-slot-1")},
		}}
}

// TestCrashWindowChild 钉住 仅供子进程使用：父进程带崩溃点环境变量重执行本测试二进制。
// - 钩子按写入信封文件的次序计数（受理、领取重写、准备重写），在指定阶段直接退出——不关闭、不清理。
func TestCrashWindowChild(t *testing.T) {
	spec, dir := os.Getenv(crashAtEnv), os.Getenv(crashDirEnv)
	if spec == "" || dir == "" {
		t.Skip("crash-window child: spawned by TestCrashWindows_ReceiveSideMatrix only")
	}
	callStr, stage, ok := strings.Cut(spec, ":")
	if !ok {
		fatalChild("malformed TAGENT_CRASH_AT " + spec)
	}
	wantCall, err := strconv.Atoi(callStr)
	if err != nil {
		fatalChild("bad crash call number: " + err.Error())
	}
	in, err := NewInbox(dir, 0)
	if err != nil {
		fatalChild("child NewInbox: " + err.Error())
	}
	cur := 0
	testWriteStageHook = func(s string) {
		if s == "tmp" {
			cur++
		}
		if cur == wantCall && s == stage {
			fmt.Printf("crashed at call %d stage %s\n", cur, s)
			os.Stdout.Sync()
			os.Exit(0)
		}
	}
	if _, err := in.Enqueue(crashEnvelope()); err != nil {
		fatalChild("child enqueue: " + err.Error())
	}
	env, path, err := in.ClaimNext()
	if err != nil || env == nil {
		fatalChild("child claim: " + fmt.Sprint(err))
	}
	if err := in.PrepareFacts(path, "11112222333344445555666677778888", []json.RawMessage{
		json.RawMessage(`{"prepared":"slot0"}`), json.RawMessage(`{"prepared":"slot1"}`),
	}); err != nil {
		fatalChild("child prepare: " + err.Error())
	}
	fatalChild(fmt.Sprintf("crash point %s never reached (writes seen: %d)", spec, cur))
}

func fatalChild(msg string) {
	fmt.Fprintln(os.Stderr, "CHILD FATAL:", msg)
	os.Exit(1)
}

func TestCrashWindows_ReceiveSideMatrix(t *testing.T) {
	cases := []struct {
		name       string
		spec       string
		wantEnvs   int
		wantTmp    int
		wantState  string
		wantClaims int
		wantPrep   bool
	}{
		{"receive-tmp", "1:tmp", 0, 1, "", 0, false},
		{"receive-renamed", "1:renamed", 1, 0, InboxStatePending, 0, false},
		{"claim", "2:renamed", 1, 0, InboxStatePending, 2, false},
		{"prepare", "3:renamed", 1, 0, InboxStatePending, 2, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run", "^TestCrashWindowChild$", "-test.v")
			cmd.Env = append(os.Environ(), crashAtEnv+"="+c.spec, crashDirEnv+"="+dir)
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "child must hit its crash point:\n%s", out)

			in, err := NewInbox(dir, 0)
			require.NoError(t, err)
			got, err := in.Outstanding()
			require.NoError(t, err)
			require.Len(t, got, c.wantEnvs, "accepted-set count after %s crash", c.name)

			tmps, err := filepath.Glob(filepath.Join(dir, "inbox-v2", "*.tmp"))
			require.NoError(t, err)
			require.Len(t, tmps, c.wantTmp, "unconfirmed material visible after %s crash", c.name)
			if c.wantTmp > 0 {
				raw, rerr := os.ReadFile(tmps[0])
				require.NoError(t, rerr)
				require.Contains(t, string(raw), crashReqID())
			}
			if c.wantEnvs == 0 {
				return
			}
			env := got[0].Env
			require.Equal(t, crashReqID(), env.RequestID, "identity survives the crash")
			require.Equal(t, "user", env.Source, "source survives the crash")
			require.Len(t, env.Messages, 2, "fixed slots are never compacted")
			require.Contains(t, string(env.Messages[1].SourceEvent), "crash-slot-1", "slot-1 ORIGINAL is intact")
			require.Equal(t, c.wantState, env.State)
			require.Equal(t, c.wantClaims, env.Attempts, "the crashed claim counts as one attempt at open requeue")
			if c.wantPrep {
				require.Equal(t, "11112222333344445555666677778888", env.ReceiptKey, "reserved key landed atomically with the freeze")
				require.NotEmpty(t, env.Messages[0].PreparedFact)
				require.NotEmpty(t, env.Messages[1].PreparedFact)
			} else {
				require.Empty(t, env.ReceiptKey, "nothing reserved before prepare")
			}
		})
	}
}

// TestNextClaimable_ReturnsReceiptedNeverSweeps 钉住 领取扫描不得私自删除"已回执未确认"的信封：它在磁盘与容量计数上都必须原样存活，删除与容量/索引/保留额的恰好一次释放只属于 ack（调用方须与保留额释放成对）。
func TestNextClaimable_ReturnsReceiptedNeverSweeps(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("sweep-1", "user"))
	_, p, err := in.ClaimNext()
	require.NoError(t, err)
	cred := reserveCredential(t, in, p)
	require.NoError(t, in.RecordCompletion(p, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(p, cred))

	for i := 0; i < 3; i++ {
		envOut, pOut, cerr := in.ClaimNext()
		require.NoError(t, cerr)
		require.NotNil(t, envOut, "scan %d: the receipted item must be RETURNED, never swept", i)
		require.Equal(t, p, pOut)
		require.Equal(t, InboxStateReceipted, envOut.State, "returned unchanged, not re-claimed")
		require.FileExists(t, p, "scan %d: deletion may only happen through Ack", i)
	}
	require.Equal(t, int64(1), in.Pending(),
		"capacity stays held until an Ack completes the barrier (no silent release on scan)")

	require.NoError(t, in.Ack(p))
	require.NoFileExists(t, p)
	require.Equal(t, int64(0), in.Pending())
}

// TestAck_RemoveFailureLeavesNoPhantomAccount 钉住 清理账在删除之前登记；删除确实失败、原件仍在盘上时，该账必须撤销。
// - 留下一条欠账，会让之后的清理排空为一个从未被删除的文件判定屏障通过；
// - 于是容量与租约被释放而信封仍然存在，真正的确认将二次释放。
func TestAck_RemoveFailureLeavesNoPhantomAccount(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("phantom-1", "user"))
	_, p, err := in.ClaimNext()
	require.NoError(t, err)
	cred := reserveCredential(t, in, p)
	require.NoError(t, in.RecordCompletion(p, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(p, cred))

	envDir := filepath.Dir(p)
	require.NoError(t, os.Chmod(envDir, 0o500))
	defer func() { _ = os.Chmod(envDir, 0o700) }()
	_, rerr := os.CreateTemp(envDir, "probe")
	if rerr == nil {
		t.Skip("platform allows unlink from a read-only dir; cannot induce a remove failure")
	}

	require.ErrorContains(t, in.Ack(p), "ack remove")
	require.FileExists(t, p, "the untouched original stays")

	require.NoError(t, os.Chmod(envDir, 0o700))
	require.Empty(t, in.DrainCleanups(), "a failed remove cancelled the pre-registered account")
	require.Equal(t, int64(1), in.Pending(), "the envelope still counts until its real Ack")

	require.NoError(t, in.Ack(p))
	require.Equal(t, int64(0), in.Pending(), "capacity released exactly once, by the real barrier")
}

func TestResetTransitional_RequiresExplicitConfirmThenClears(t *testing.T) {
	dir := t.TempDir()
	spill := filepath.Join(dir, "00000000000000000009.spill")
	require.NoError(t, os.WriteFile(spill, []byte("{}"), 0o644))
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)

	n, rerr := in.ResetTransitional(false)
	require.Error(t, rerr)
	require.Equal(t, 0, n)
	require.FileExists(t, spill, "an unconfirmed reset must not delete anything")

	n, rerr = in.ResetTransitional(true)
	require.NoError(t, rerr)
	require.Equal(t, 1, n)
	require.NoFileExists(t, spill)
	sp, v1 := in.TransitionalData()
	require.Empty(t, sp)
	require.Empty(t, v1)
}

func TestResetTransitional_RefusedWithLiveUnacked(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "00000000000000000009.spill"), []byte("{}"), 0o644))
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("r", "user"))

	n, rerr := in.ResetTransitional(true)
	require.Error(t, rerr, "a managed reset needs exclusive writer access, never an in-flight turn")
	require.Equal(t, 0, n)
}

// TestTransitional_NeverClassifiesCurrentV2 钉住 当前格式项永不被归为前代格式，因此永不进入受管重置的允许清单。
// - 该重置在结构上不具备抹掉当前格式的能力。
func TestTransitional_NeverClassifiesCurrentV2(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("r", "user"))
	sp, v1 := in.TransitionalData()
	require.Empty(t, sp)
	require.Empty(t, v1, "current-format v2 items are never transitional / reset targets")
}

// TestQuarantineCorruptionStillBlocksDespiteTransitional 钉住 前代格式 .spill 被重新分类后，不会掩盖"当前格式损坏"这条臂： 隔离项仍必须 fail-loud 且绝不被清除。
func TestQuarantineCorruptionStillBlocksDespiteTransitional(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "00000000000000000009.spill"), []byte("{}"), 0o644))
	q := filepath.Join(dir, "inbox-v2", inboxQuarantine)
	require.NoError(t, os.MkdirAll(q, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(q, "00000000000000000003.json"), []byte("{bad"), 0o644))

	_, err := NewInbox(dir, 10)
	require.ErrorIs(t, err, ErrQuarantineUndispositioned,
		"current-format corruption must surface, not be ignored as transitional or wiped")
}

// writeRawEnvelope drops a hand-built bytes blob straight into the inbox dir so
// a test can exercise the on-disk validation path (readEnvelope) independent of
// Enqueue's own guards.
func writeRawEnvelope(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

// TestInbox_ReadEnvelopeRejectsIllegalState 钉住 读取信封必须拒绝三种合法状态之外的持久状态：被篡改的状态绝不当作待领取消费。
func TestInbox_ReadEnvelopeRejectsIllegalState(t *testing.T) {
	dir := t.TempDir()
	body := `{"version":2,"request_id":"r","source":"user","state":"bogus",` +
		`"messages":[{"slot":0,"source_event":{"id":"e","content":"x"}}]}`
	p := writeRawEnvelope(t, dir, "00000000000000000001.json", body)
	_, err := readEnvelope(p)
	require.Error(t, err, "an illegal durable state must be rejected, not consumed as pending")
	require.Contains(t, err.Error(), "state")
}

// TestInbox_ReadEnvelopeAcceptsLegalStates 钉住 readEnvelope still accepts each of the three legal states (so the whitelist guard above is not over-broad).
func TestInbox_ReadEnvelopeAcceptsLegalStates(t *testing.T) {
	for _, st := range []string{InboxStatePending, InboxStateClaimed, InboxStateReceipted} {
		dir := t.TempDir()
		body := `{"version":2,"request_id":"r","source":"user","state":"` + st + `",` +
			`"messages":[{"slot":0,"source_event":{"id":"e","content":"x"}}]}`
		p := writeRawEnvelope(t, dir, "00000000000000000002.json", body)
		_, err := readEnvelope(p)
		require.NoErrorf(t, err, "legal state %q must be accepted", st)
	}
}

// TestInbox_EnqueueRejectsNilOrMalformedSourceEvent 钉住 受理必须拒绝空或畸形的源事件：它们在接收时就属非法／缺失消息，不是"合法的空输入"。
func TestInbox_EnqueueRejectsNilOrMalformedSourceEvent(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)

	_, err = in.Enqueue(&Envelope{RequestID: "n", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: json.RawMessage("null")}}})
	require.Error(t, err, "a null source_event (nil Message) must be refused at receive")

	_, err = in.Enqueue(&Envelope{RequestID: "m", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: json.RawMessage("{not json")}}})
	require.Error(t, err, "an unparseable source_event must be refused at receive")

	require.Equal(t, int64(0), in.Pending(), "refused inputs leave no durable item")
}

// TestInbox_EnqueueRejectsExternalInputWithNilMessage 钉住 外部输入源事件必须携带非空消息：结构合法但消息为空或缺字段同样是非无损输入，须在接收处拒绝。
// - 只拒"整体为空或无法解析"的口径会放这类进来，让后续装配解引用空消息。
func TestInbox_EnqueueRejectsExternalInputWithNilMessage(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)

	_, err = in.Enqueue(&Envelope{RequestID: "t1", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e","type":"external_input","message":null}`)}}})
	require.Error(t, err, `external_input with "message":null must be refused at receive`)
	require.Contains(t, err.Error(), "nil Message")

	_, err = in.Enqueue(&Envelope{RequestID: "t2", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e","type":"external_input"}`)}}})
	require.Error(t, err, "external_input with an absent Message must be refused at receive")

	_, err = in.Enqueue(&Envelope{RequestID: "t3", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e","type":"external_input","message":{"role":"user","content":""}}`)}}})
	require.NoError(t, err, "external_input with a valid empty-text Message is legal")

	require.Equal(t, int64(1), in.Pending(), "only the valid input becomes durable")
}

// TestInbox_EnqueueAcceptsValidEmptyAndNonTextMessages 钉住 C (positive control). A valid empty-text Message and a non-text (image-only)
func TestInbox_EnqueueAcceptsValidEmptyAndNonTextMessages(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)

	emptyText, _ := json.Marshal(map[string]any{"id": "e1", "content": ""})
	_, err = in.Enqueue(&Envelope{RequestID: "et", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: emptyText}}})
	require.NoError(t, err, "a valid empty-text message is legal input")

	imageOnly, _ := json.Marshal(map[string]any{"id": "e2", "content": "", "parts": []any{
		map[string]any{"type": "image_url", "url": "http://x/y.png"}}})
	_, err = in.Enqueue(&Envelope{RequestID: "img", Source: "user",
		Messages: []MessageSlot{{Slot: 0, SourceEvent: imageOnly}}})
	require.NoError(t, err, "a non-text (image) payload is legal input, not an empty one")
}

// TestJsonEqual_BigIntAdjacentKeysDistinct 钉住 比较必须按精确整数判定大整数身份。
// - 超过 2 的 53 次方的相邻两个事件键是两个不同身份；
// - 一旦经浮点往返就被折成相等，准备与完成的幂等性即被腐蚀。
func TestJsonEqual_BigIntAdjacentKeysDistinct(t *testing.T) {
	a := json.RawMessage(`{"event_key":9007199254740992}`)
	b := json.RawMessage(`{"event_key":9007199254740993}`)
	require.False(t, jsonEqual(a, b),
		"adjacent big-integer keys differ by 1 and MUST NOT compare equal")

	na := json.RawMessage(`{"slots":[{"event_key":9007199254740992}]}`)
	nb := json.RawMessage(`{"slots":[{"event_key":9007199254740993}]}`)
	require.False(t, jsonEqual(na, nb), "nested adjacent big-int keys must differ")

	require.True(t, jsonEqual(
		json.RawMessage(`{"b":2,"a":1}`), json.RawMessage(`{"a":1,"b":2}`)),
		"same object, different key order, must be equal")
	require.True(t, jsonEqual(
		json.RawMessage(`{"event_key":9007199254740993}`),
		json.RawMessage(`{"event_key":9007199254740993}`)),
		"identical big-int keys must be equal")
}

// TestInbox_PrepareFacts_BigIntAdjacentFactIsConflict 钉住 与已冻结事实仅差一枚相邻大整数事件键的准备，必须判为冲突。
// - 不得当作幂等重做而静默接受：键相近不等于内容相同。
func TestInbox_PrepareFacts_BigIntAdjacentFactIsConflict(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("bigp", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)

	require.NoError(t, in.PrepareFacts(path, "rk",
		[]json.RawMessage{json.RawMessage(`{"event_key":9007199254740992}`)}))
	require.Error(t,
		in.PrepareFacts(path, "rk", []json.RawMessage{json.RawMessage(`{"event_key":9007199254740993}`)}),
		"an adjacent big-int fact must be a conflict, not an idempotent no-op")
}

// TestQuarantine_RenameFailureKeepsCapacity 钉住隔离搬移被阻时错误上抛且容量不扣。
// - 坏信封隔离 rename 失败不静默跳过：pending 不扣减，容量记账不穿透。
// - 障碍解除后下一次认领自愈：项目搬移、容量释放、扫描前进。
func TestQuarantine_RenameFailureKeepsCapacity(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	defer in.Close()

	mustEnqueue(t, in, env("q1", "user"))
	paths, _ := filepath.Glob(filepath.Join(in.dir, "*.json"))
	require.Len(t, paths, 1)
	require.NoError(t, os.WriteFile(paths[0], []byte("{ broken"), 0o644))

	dst := filepath.Join(in.dir, inboxQuarantine, filepath.Base(paths[0]))
	require.NoError(t, os.MkdirAll(dst, 0o755), "a directory at the quarantine destination blocks the rename")

	pendingBefore := in.Pending()
	_, _, err = in.ClaimNext()
	require.ErrorContains(t, err, "quarantine rename")
	require.Equal(t, pendingBefore, in.Pending(), "a failed quarantine must not free capacity")

	require.NoError(t, os.RemoveAll(dst))
	_, _, err = in.ClaimNext()
	require.NoError(t, err, "after the block clears the claim path must recover")
	require.Equal(t, pendingBefore-1, in.Pending(), "the successful quarantine decrements once")
}

// TestClaimNext_RefusedAfterClose 钉住锁内 closed 复查与 Enqueue 对称。
// - Close 之后的认领一律拒绝，不复用无锁快查的竞态窗口发放新认领。
func TestClaimNext_RefusedAfterClose(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("c1", "user"))
	require.NoError(t, in.Close())

	_, _, err = in.ClaimNext()
	require.ErrorContains(t, err, "closed")
}
