package reliability

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// logWarnf relays through the framework logger (leaf package has no logger
// of its own; kept as a tiny indirection so call sites stay one-line).
func logWarnf(format string, args ...any) { log.Warnf(format, args...) }

// syncDir is the reliability-leaf dir-sync (best-effort, mirroring
// memory/kv semantics: platform-unsupported is swallowed, real errors are
// returned to callers that treat the rename as a durability step).
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	serr := d.Sync()
	_ = d.Close()
	if serr != nil {
		return serr
	}
	return nil
}

// syncDirFunc is the injectable dir-sync seam used by the atomic envelope write
// (defaults to syncDir).  tests force a post-rename dir-sync failure to
// observe the publish-uncertain outcome without needing real power loss.
var syncDirFunc = syncDir

// testGateHook, when non-nil (TEST ONLY), runs inside Enqueue after the liveness
// fast-check and before taking the mutation lock. It gives  a deterministic
// way to interleave a completed Close with an in-flight Enqueue and prove the two
// are coordinated under one lock. Always nil in production.
var testGateHook func()

// testWriteStageHook, when non-nil (TEST ONLY), is called by writeEnvelopeFile
// right AFTER the tmp file is written+fsynced+closed (stage "tmp") and right
// AFTER the rename landed, before the directory sync (stage "renamed").
// crash-window children exit inside this hook, so the kill lands exactly at
// the audited production write step — enqueue, claim and prepare rewrites all
// flow through writeEnvelopeFile and are covered by the same two stages.
// Always nil in production.
var testWriteStageHook func(stage string)

var (
	// ErrInboxFull: pending ≥ maxPending — explicit rejection, never a silent
	// fallback to volatile.
	ErrInboxFull = errors.New("reliability: durable inbox full")
	// ErrQuarantineUndispositioned 表示：上一次运行隔离了不可读或版本未知的项，而运维尚未处置。
	// 重开必须拒绝，而不是每次启动都静默忽略它们。
	//
	// 前代格式（.spill / inbox-v1）数据不阻止启动：运行时只加载当前格式，并把前代项当作惰性数据
	// 处理（绝不猜测解析、绝不消费），等待一次显式的受管 ResetTransitional。仍会阻止重开的，
	// 只有当前格式自身的损坏（隔离）。
	ErrQuarantineUndispositioned = errors.New("reliability: inbox quarantine holds undispositioned items; review and disposition them before reopening (never silently ignored)")
	// ErrCompletionConflict: RecordCompletion carries a payload that differs
	// from the already-durable completion. The frozen completion is
	// authoritative; the caller must not overwrite it (D3 step 8).
	ErrCompletionConflict = errors.New("reliability: envelope completion already durable with a different payload")
	// ErrReceiptKeyConflict: PrepareFacts was asked to reserve a different
	// receipt_key than the one already frozen on the envelope.
	ErrReceiptKeyConflict = errors.New("reliability: envelope receipt_key already reserved with a different value")
	// ErrPrepareConflict: PrepareFacts hit a DETERMINISTIC format/identity conflict
	// — a slot already frozen with a different fact, a facts/slots mismatch, or an
	// envelope not in the claimed state. Unlike a transient I/O failure, retrying
	// cannot resolve it, so the submit gate isolates the envelope and stops
	// auto-consumption. ErrReceiptKeyConflict is a
	// specific instance of this class.
	ErrPrepareConflict = errors.New("reliability: prepare hit a deterministic conflict")
	// ErrReceiveUncertain: an envelope's rename landed on disk but its directory
	// sync failed. The original and its allocated sequence are
	// retained — no later input may reuse that sequence to overwrite it — but the
	// receive MUST NOT be reported as durable-accepted; reconciliation on reopen
	// owns the item. It is distinct from a definite not-published failure.
	ErrReceiveUncertain = errors.New("reliability: receive published but durability unconfirmed")
	// ErrInboxClosed is returned by Enqueue once Close has been called. The
	// liveness decision is made under the mutation lock so an Enqueue whose
	// pre-lock fast-check raced a completed Close cannot register afterwards.
	ErrInboxClosed = errors.New("reliability: inbox closed")
)

const (
	inboxDirName = "inbox-v2"
	// legacyInboxV1DirName 是由更早版本进程写入的收件目录名：v2 不猜测 v1 的有损格式；
	// 该目录的存在不阻止启动——v2 只加载当前格式，并把 v1 内容留作惰性，直到一次受管重置。
	legacyInboxV1DirName = "inbox-v1"
	// spillFileExt 是已停用 SpillStore 溢出文件的扩展名，仅保留用于让分类与重置能识别
	//（而非读取）前代格式的 .spill 项。
	spillFileExt = ".spill"
	// envelopeVersion is the explicit, REQUIRED format version of every v2
	// envelope. A missing or other version is unreadable and quarantined —
	// never consumed (D2「版本不识别...不得作为有效输入继续消费」).
	envelopeVersion = 2

	// PreparedVersionCurrent 是冻结 prepared_fact 时盖上的预备格式版本。带着其它版本（或缺失版本）
	// 材料落盘的槽位属不兼容的过渡材料：绝不解析、绝不静默消费——readEnvelope 拒绝它，由调用方隔离该信封。
	PreparedVersionCurrent = 1

	inboxQuarantine = "quarantine"

	// InboxStatePending / Claimed / Receipted are the three durable states.
	InboxStatePending = "pending"
	// InboxStateClaimed 信封已被某次 Pull 领取：持有者独占处理权，其它领取者看不到它。
	InboxStateClaimed = "claimed"
	// InboxStateReceipted 表示已写下回执但尚未 ack：此类信封必须在重复领取扫描中
	// 原样存活（既不删除也不重投），删除与容量/租约的释放只属于 Ack。
	InboxStateReceipted = "receipted"
)

// writeOutcome is the tri-state result of an atomic envelope write (, design
// 决策2). A caller MUST distinguish "definitely not published" (no final file →
// safe to retry on a fresh sequence) from "renamed into place but its directory
// entry is unconfirmed" (the original exists and must be retained, capacity
// reserved, sequence never reused) from "durable" (accepted). Only outcomeDurable
// may be reported as accepted; outcomePublishUncertain keeps the landed original.
type writeOutcome int

const (
	outcomeNotPublished writeOutcome = iota
	outcomePublishUncertain
	outcomeDurable
)

// MessageSlot is one inbound input at a FIXED slot index inside an envelope.
// The slot number is assigned at acceptance and NEVER renumbered — a failed
// slot does not compact the others (F4「序号不可压紧」).
type MessageSlot struct {
	// Slot is the immutable 0-based position within the envelope.
	Slot int `json:"slot"`
	// SourceEvent is the lossless JSON snapshot of the original AgentEvent
	// (ID/Type/Source/Timestamp/full Message/business Metadata), written at
	// acceptance. The reliability leaf holds it as opaque bytes: only the
	// agent layer knows its schema (no agent/memory import here, D2).
	SourceEvent json.RawMessage `json:"source_event"`
	// PreparedFact is the canonical FullEvent JSON frozen on FIRST handling,
	// before any fact is written. Absent (nil) = not yet prepared. A replay
	// MUST reuse it verbatim — never restamp time/attribution/summary .
	PreparedFact json.RawMessage `json:"prepared_fact,omitempty"`
	// PreparedVersion 记录 prepared_fact 冻结时所用的预备格式版本（见 PreparedVersionCurrent）；
	// 0 表示尚无材料。readEnvelope 会拒绝"有材料但版本非当前"的槽位，因此不兼容的过渡材料
	// 绝不会经前代格式解析器被回放。
	PreparedVersion int `json:"prepared_version,omitempty"`
}

// Envelope is one durable acceptance unit (inbox-v2): a batch of messages from
// one producer call, accepted (202) only after this whole file is durable.
type Envelope struct {
	Version   int    `json:"version"`
	RequestID string `json:"request_id"`
	Source    string `json:"source"`
	State     string `json:"state"`
	// Attempts counts re-claims of this envelope (requeue + claim each count
	// one). It is a PERSISTENT AUDIT field only: the behavioural consumer (a
	// max-attempts quarantine gate) is NOT wired, and the diagnostics
	// aggregation surface over it is deferred
	//. Kept for retry forensics, not read to drive behaviour.
	Attempts int `json:"attempts,omitempty"`
	// ReceiptKey is the processing-receipt EventKey (hex) reserved on FIRST
	// prepare. It represents a RESERVED identity only, not completion .
	ReceiptKey string `json:"receipt_key,omitempty"`
	// Messages are the fixed-slot inbound inputs (one per producer message).
	Messages []MessageSlot `json:"messages"`
	// Completion is the frozen post-turn receipt payload + per-slot
	// processed/skipped disposition. Absent = no terminal state yet .
	Completion json.RawMessage `json:"completion,omitempty"`
}

// ReceiptCredential is the verified receipt credential RecordReceipt requires
// (, design 决策 L126「RecordReceipt 必须要求合法 completion 与已核验回执
// 凭据」). It carries ONLY the reserved receipt-key identity under which the
// caller has verified the fact-chain receipt — it is never a free-form
// description string or a request id, the two weak confirmation shapes
// deletes. The leaf checks it against the envelope's frozen reservation; the
// schema-level legality of the completion (decode + validate) is verified by
// the agent layer before a credential is ever issued (D2 keeps this leaf
// schema-agnostic).
type ReceiptCredential struct {
	// ReceiptKey is the hex reserved receipt key whose fact-chain receipt the
	// caller verified present. The empty key is never a credential.
	ReceiptKey string
}

// Inbox is the durable input mailbox (concurrency-safe).
type Inbox struct {
	dir       string
	max       int
	mu        sync.Mutex
	seq       atomic.Int64
	pending   atomic.Int64
	dead      chan struct{}
	closeOnce sync.Once
	closed    bool

	// cleanupOwed is the independent cleanup account: paths whose
	// envelope file was unlinked by Ack but whose directory-sync barrier failed,
	// so the removal is not yet durable. Capacity (pending) and the retention lease
	// are NOT released for these until a retry/drain completes the barrier — exactly
	// once . The path is seq-named and never reused, so the account keys are
	// stable; on restart it is re-derived from surviving files (a gone file is simply
	// not counted, its lease not re-armed).
	cleanupOwed map[string]owedCleanup

	// transitional records previous-format data detected at open. It is
	// never read or consumed; boot proceeds on the current format and only an
	// explicit operator-confirmed ResetTransitional clears it. Read-only after open.
	transitional transitionalData
}

// owedCleanup records what a completed-but-unsynced Ack removal still owes:
// the protected material (so the caller releases the retention lease once,
// when the barrier finally syncs).
type owedCleanup struct {
	material UnackedMaterial
}

// NewInbox opens (or creates) an inbox-v2 at dir. On reopen: claimed items
// WITHOUT a receipt go back to pending (the crash may have happened anywhere
// between claim and receipt — replay is the safe default); receipted items
// stay receipted so the consumer Ack-skips them without re-executing.
func NewInbox(dir string, maxPending int) (*Inbox, error) {
	if dir == "" {
		return nil, fmt.Errorf("reliability: inbox requires non-empty dir")
	}
	if maxPending <= 0 {
		maxPending = 2560
	}
	envDir := filepath.Join(dir, inboxDirName)
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return nil, fmt.Errorf("reliability: create inbox dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(envDir, inboxQuarantine), 0o755); err != nil {
		return nil, fmt.Errorf("reliability: create inbox quarantine: %w", err)
	}
	if err := syncDirFunc(envDir); err != nil {
		return nil, fmt.Errorf("reliability: inbox dir sync at open: %w", err)
	}
	if err := checkQuarantineDispositioned(filepath.Join(envDir, inboxQuarantine)); err != nil {
		return nil, err
	}
	transitional := classifyTransitional(filepath.Dir(envDir))
	in := &Inbox{dir: envDir, max: maxPending, dead: make(chan struct{}), cleanupOwed: map[string]owedCleanup{}, transitional: transitional}

	entries, err := os.ReadDir(envDir)
	if err != nil {
		return nil, fmt.Errorf("reliability: scan inbox: %w", err)
	}
	var maxSeq int64
	var unconfirmed int64
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		n, perr := parseInboxSeq(name)
		if perr != nil {
			if _, qerr := in.quarantineFile(filepath.Join(envDir, name), "bad name: "+name); qerr != nil {
				return nil, qerr
			}
			continue
		}
		if n > maxSeq {
			maxSeq = n
		}
		env, rerr := readEnvelope(filepath.Join(envDir, name))
		if rerr != nil {
			if _, qerr := in.quarantineFile(filepath.Join(envDir, name), "unreadable envelope: "+rerr.Error()); qerr != nil {
				return nil, qerr
			}
			continue
		}
		switch env.State {
		case InboxStateClaimed:
			env.State = InboxStatePending
			env.Attempts++
			if _, werr := writeEnvelopeFile(filepath.Join(envDir, name), env); werr != nil {
				return nil, fmt.Errorf("reliability: requeue claimed %s: %w", name, werr)
			}
			unconfirmed++
		case InboxStateReceipted:
			unconfirmed++
		default:
			unconfirmed++
		}
	}
	in.seq.Store(maxSeq)
	in.pending.Store(unconfirmed)
	return in, nil
}

// transitionalData enumerates previous-format files found at open (, design
// 决策10). They are never read: the runtime loads ONLY the current format. The
// ONLY thing that removes them is an explicit, operator-confirmed ResetTransitional.
type transitionalData struct {
	spill []string
	v1    []string
}

func (t transitionalData) present() bool { return len(t.spill) > 0 || len(t.v1) > 0 }

// classifyTransitional 扫描前代格式数据（只读、绝不改动）：父目录下散落的 *.spill 与
// inbox-v1 下的 *.json。它故意不查看 inbox-v2（当前格式），也从不删除——只作报告，
// 从而让启动能以当前格式继续，前代格式数据保持惰性。
func classifyTransitional(parent string) transitionalData {
	var t transitionalData
	if ents, err := os.ReadDir(parent); err == nil {
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(e.Name(), spillFileExt) {
				t.spill = append(t.spill, filepath.Join(parent, e.Name()))
			}
		}
	}
	if ents, err := os.ReadDir(filepath.Join(parent, legacyInboxV1DirName)); err == nil {
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				t.v1 = append(t.v1, filepath.Join(parent, legacyInboxV1DirName, e.Name()))
			}
		}
	}
	return t
}

// checkQuarantineDispositioned still blocks reopen when the inbox quarantine holds
// items from a prior run. Quarantine holds CURRENT-format unreadable/unknown-version
// envelopes — corruption that MUST surface, never be silently wiped (: current
// corruption never triggers a reset). Read-only; the operator must disposition first.
func checkQuarantineDispositioned(quarantineDir string) error {
	q, err := os.ReadDir(quarantineDir)
	if err != nil {
		return nil
	}
	for _, e := range q {
		if !e.IsDir() {
			return fmt.Errorf("%w: %s", ErrQuarantineUndispositioned, e.Name())
		}
	}
	return nil
}

// TransitionalData reports the previous-format items detected at open. They
// are inert — never read or consumed — and remain on disk until an explicit,
// operator-confirmed ResetTransitional. The slices are copies (safe to retain).
func (in *Inbox) TransitionalData() (spill, v1 []string) {
	return append([]string(nil), in.transitional.spill...), append([]string(nil), in.transitional.v1...)
}

// ResetTransitional 是对前代格式数据的**一次性受管重置**，也是本可靠性叶子内唯一具破坏性的路径。
// 安全约束（不得被推导成"任意删除"的能力）：
//   - 必须显式 confirm：重置是运维动作，绝不自动发生；
//   - 只删除打开时枚举到的前代格式文件（散落的 *.spill 与 inbox-v1/*.json）——它们与
//     在用的 inbox-v2 树互不相交，且不被任何当前事实引用，因此清掉它们不留悬空引用
//     （恢复单元的一致性规则约束的是当前数据）；
//   - 绝不触碰 inbox-v2 及其隔离区（当前格式损坏必须暴露，而不是被抹掉），也不触碰本
//     叶子目录树之外的任何路径；
//   - 已关闭、或仍有信封未 ack（pending>0）时拒绝执行：受管重置需要独占写入权，而不是
//     在一个进行中的回合里插队。
//
// 当前格式损坏与一般 I/O 失败都不属于"过渡数据"，这里绝不清理它们。返回被删除的
// 前代格式文件数。
func (in *Inbox) ResetTransitional(confirm bool) (int, error) {
	if !confirm {
		return 0, fmt.Errorf("reliability: ResetTransitional requires explicit confirmation (destructive operator action)")
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.closed {
		return 0, fmt.Errorf("reliability: ResetTransitional refused — inbox closed")
	}
	if n := in.pending.Load(); n > 0 {
		return 0, fmt.Errorf("reliability: ResetTransitional refused — %d live unacked envelope(s); reset needs exclusive writer access", n)
	}
	targets := append([]string{}, in.transitional.spill...)
	targets = append(targets, in.transitional.v1...)
	leafParent := filepath.Dir(in.dir)
	removed := 0
	for _, p := range targets {
		if !underDir(p, leafParent) || strings.Contains(p, inboxDirName) {
			return removed, fmt.Errorf("reliability: ResetTransitional refused %s (outside the leaf's transitional allow-list)", p)
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("reliability: ResetTransitional remove %s: %w", p, err)
		}
		removed++
	}
	in.transitional = transitionalData{}
	if err := syncDir(leafParent); err != nil {
		return removed, fmt.Errorf("reliability: ResetTransitional dir sync: %w", err)
	}
	return removed, nil
}

// underDir reports whether path lies strictly inside dir (not equal, not escaping).
func underDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// Enqueue durably appends one envelope (whole-batch acceptance). It stamps
// version=2, assigns fixed slot indices, and fsyncs the file + syncs its
// directory entry BEFORE returning — only then may the caller report a durable
// receipt. Each slot MUST carry a non-empty source_event (lossless input); an
// empty payload is refused, never accepted with silently-dropped fields.
func (in *Inbox) Enqueue(env *Envelope) (int64, error) {
	if in == nil || env == nil {
		return 0, fmt.Errorf("reliability: nil inbox/envelope")
	}
	if len(env.Messages) == 0 {
		return 0, fmt.Errorf("reliability: empty envelope")
	}
	for i := range env.Messages {
		if err := validateSourceEvent(env.Messages[i].SourceEvent); err != nil {
			return 0, fmt.Errorf("reliability: envelope slot %d: %w", i, err)
		}
	}
	select {
	case <-in.dead:
		return 0, ErrInboxClosed
	default:
	}
	if testGateHook != nil {
		testGateHook()
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.closed {
		return 0, ErrInboxClosed
	}
	if in.pending.Load() >= int64(in.max) {
		return 0, ErrInboxFull
	}
	n := in.seq.Add(1)
	env.Version = envelopeVersion
	env.State = InboxStatePending
	for i := range env.Messages {
		env.Messages[i].Slot = i
	}
	path := in.seqPath(n)
	oc, werr := writeEnvelopeFile(path, env)
	if oc == outcomePublishUncertain {
		in.pending.Add(1)
		return 0, fmt.Errorf("%w: sequence %d retained: %v", ErrReceiveUncertain, n, werr)
	}
	if oc != outcomeDurable {
		return 0, werr
	}
	in.pending.Add(1)
	return n, nil
}

// ClaimNext returns the OLDEST pending envelope (lexicographic seq = enqueue
// order), atomically marking it claimed. The file is NOT deleted: a crash
// after claim replays it. Returns (nil, "", nil) when the inbox is drained.
func (in *Inbox) ClaimNext() (*Envelope, string, error) {
	select {
	case <-in.dead:
		return nil, "", fmt.Errorf("reliability: inbox closed")
	default:
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	// Re-check under the lock, symmetric with Enqueue: Close publishes
	// closed=true inside the same lock the fast check raced against, so a
	// claim that passed the lock-free check must still refuse to hand out a
	// new claim on a closed inbox.
	if in.closed {
		return nil, "", fmt.Errorf("reliability: inbox closed")
	}
	path, env, err := in.nextClaimable()
	if err != nil || env == nil {
		return nil, "", err
	}
	if env.State == InboxStateReceipted {
		return env, path, nil
	}
	env.State = InboxStateClaimed
	env.Attempts++
	if _, werr := writeEnvelopeFile(path, env); werr != nil {
		return nil, "", fmt.Errorf("reliability: claim rewrite: %w", werr)
	}
	return env, path, nil
}

// PrepareFacts durably freezes each slot's prepared_fact and a reserved
// receipt_key onto a CLAIMED envelope in ONE atomic rewrite. The caller MUST
// complete this before writing the first fact (D2「写前准备耐久重写」): a
// failure here means NO fact is written and the claim replays. It is
// idempotent on a re-prepare with the same receipt_key and identical facts; a
// different receipt_key or a different already-frozen fact is a conflict (the
// envelope stays untouched so the caller can quarantine it). A nil element in
// facts leaves that slot's existing prepared_fact unchanged (partial prepare
// across a retry is allowed).
func (in *Inbox) PrepareFacts(path, receiptKey string, facts []json.RawMessage) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	env, err := readEnvelope(path)
	if err != nil {
		return fmt.Errorf("reliability: prepare read: %w", err)
	}
	if env.State != InboxStateClaimed {
		return fmt.Errorf("%w: envelope %s not claimed (state=%s)", ErrPrepareConflict, path, env.State)
	}
	if env.ReceiptKey != "" && env.ReceiptKey != receiptKey {
		return fmt.Errorf("%w: %s has %q, asked %q", ErrReceiptKeyConflict, path, env.ReceiptKey, receiptKey)
	}
	if len(facts) != len(env.Messages) {
		return fmt.Errorf("%w: prepare facts/slots mismatch (%d vs %d)", ErrPrepareConflict, len(facts), len(env.Messages))
	}
	for i, fact := range facts {
		if fact == nil {
			continue
		}
		if existing := env.Messages[i].PreparedFact; len(existing) > 0 && !jsonEqual(existing, fact) {
			return fmt.Errorf("%w: slot %d already frozen with different fact", ErrPrepareConflict, i)
		}
		env.Messages[i].PreparedFact = fact
		env.Messages[i].PreparedVersion = PreparedVersionCurrent
	}
	env.ReceiptKey = receiptKey
	if _, werr := writeEnvelopeFile(path, env); werr != nil {
		return fmt.Errorf("reliability: prepare write: %w", werr)
	}
	return nil
}

// QuarantineEnvelope isolates one specific envelope (by path) into the quarantine
// dir under the mutation lock and frees its unacked capacity. The bytes are kept
// on disk for operator inspection (never destroyed). The  submit gate uses it
// to isolate a deterministic-conflict input rather than silently retry or drop it.
//
// It returns the isolated envelope's retention material together with the moved
// flag: quarantine is a terminal disposition like Ack, so the caller MUST release
// the returned material's holders whenever moved is true. Reading the material
// from the SAME pre-move read (rather than a separate pre-read) is what makes
// "isolated ⇔ releasable" atomic — a second read that could disagree with this
// one (transient I/O error between the two) would strand the lease forever.
// An already-gone/unreadable envelope quarantines nothing, protects nothing,
// and returns (zero, false).
func (in *Inbox) QuarantineEnvelope(path, reason string) (UnackedMaterial, bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	env, err := readEnvelope(path)
	if err != nil {
		return UnackedMaterial{}, false
	}
	moved, qerr := in.quarantineFile(path, reason)
	if qerr != nil {
		logWarnf("reliability: quarantine of %s incomplete: %v", path, qerr)
	}
	if !moved {
		return UnackedMaterial{}, false
	}
	in.pending.Add(-1)
	return MaterialOf(env), true
}

// ReleaseClaim 把已领取的信封退回 pending，使后续 Pull 能按严格序号重新领取它。提交门用它
// 在瞬时 I/O 失败时退避——不 ack、不丢弃、不消费输入，从而保住顺序与有界背压（最旧的卡住
// 信封先于任何较晚到达者被重领）。文件已消失、或已不处于 claimed 态时是 no-op。
func (in *Inbox) ReleaseClaim(path string) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	env, err := readEnvelope(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if env.State != InboxStateClaimed {
		return nil
	}
	env.State = InboxStatePending
	if _, werr := writeEnvelopeFile(path, env); werr != nil {
		return fmt.Errorf("reliability: release claim write: %w", werr)
	}
	return nil
}

// RecordCompletion durably freezes the post-turn completion payload (per-slot
// processed/skipped disposition + receipt content) onto the envelope BEFORE
// the fact-chain receipt is submitted (D3 step 7). Idempotent on an identical
// payload; a different payload on an already-completed envelope is a conflict
// — the frozen completion is authoritative.
func (in *Inbox) RecordCompletion(path string, completion json.RawMessage) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	env, err := readEnvelope(path)
	if err != nil {
		return fmt.Errorf("reliability: completion read: %w", err)
	}
	if len(env.Completion) > 0 && !jsonEqual(env.Completion, completion) {
		return fmt.Errorf("%w: %s", ErrCompletionConflict, path)
	}
	env.Completion = completion
	if _, werr := writeEnvelopeFile(path, env); werr != nil {
		return fmt.Errorf("reliability: completion write: %w", werr)
	}
	return nil
}

// RecordReceipt durably marks a claimed envelope as processed (claimed →
// receipted) ONLY on valid evidence. Three gates, none of
// which is a description string or a request id:
// ① a LEGAL completion is durably frozen — present, valid JSON, and schema-
//
//	consistent enough for this schema-agnostic leaf to trust (the agent layer
//	fully decodes/validates before issuing any credential, D2);
//
// ② the two-phase reservation was actually established — the envelope carries a
//
//	non-empty reserved receipt key from PrepareFacts;
//
// ③ the caller presents a ReceiptCredential whose key matches that reservation
//
//	— the verified receipt identity, minted only after the fact-chain receipt
//	commit was confirmed. A refused receipt never advances the state: the claim
//	stays and replays rather than letting a bare transition stand in for
//	processing evidence. Crash before a successful call → the claim replays.
func (in *Inbox) RecordReceipt(path string, cred ReceiptCredential) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	env, err := readEnvelope(path)
	if err != nil {
		return fmt.Errorf("reliability: receipt read: %w", err)
	}
	if env.State == InboxStateReceipted {
		return nil
	}
	if len(env.Completion) == 0 {
		return fmt.Errorf("reliability: receipt refused — envelope %s has no durable completion (state=%s)", path, env.State)
	}
	if !json.Valid(env.Completion) {
		return fmt.Errorf("reliability: receipt refused — envelope %s carries an illegal (undecodable) completion", path)
	}
	if env.ReceiptKey == "" {
		return fmt.Errorf("reliability: receipt refused — envelope %s has no reserved receipt key (two-phase protocol not established)", path)
	}
	if cred.ReceiptKey == "" || cred.ReceiptKey != env.ReceiptKey {
		return fmt.Errorf("reliability: receipt refused — envelope %s credential key %q does not match reserved receipt key (unverified receipt)", path, cred.ReceiptKey)
	}
	env.State = InboxStateReceipted
	if _, werr := writeEnvelopeFile(path, env); werr != nil {
		return fmt.Errorf("reliability: receipt write: %w", werr)
	}
	return nil
}

// Ack removes a confirmed envelope. Idempotent: a repeated Ack (or a lost
// remove that a later replay retries) is success, never a re-execution.
//
// /: removal is only "done" once BOTH the unlink and
// its directory-sync are durable, and the independent cleanup account is
// REGISTERED BEFORE the unlink happens — an unconfirmed removal can never be
// lost between "started" and "owed". If the unlink lands but the dir-sync
// fails, the account stays (unlink 后同步失败账目保留): capacity, the request-id
// index and the retention lease are NOT released, and Ack returns an uncertain
// error. A later Ack of the now-missing file, or a per-turn DrainCleanups,
// completes the outstanding barrier and then releases capacity/index/retention
// EXACTLY ONCE. A remove that definitively fails (file still present, cleanup
// never started) cancels the pre-registered account — no phantom owed entry for
// an untouched file. An Ack of a missing file with NO owed account was fully
// acked earlier and must not decrement again (no double release).
func (in *Inbox) Ack(path string) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	env, err := readEnvelope(path)
	if err != nil {
		if os.IsNotExist(err) {
			if _, ok := in.cleanupOwed[path]; ok {
				if sderr := syncDirFunc(filepath.Dir(path)); sderr != nil {
					return fmt.Errorf("reliability: ack dir sync (owed %s): %w", path, sderr)
				}
				in.finalizeCleanup(path)
			}
			return nil
		}
		return fmt.Errorf("reliability: ack read: %w", err)
	}
	if env.State != InboxStateReceipted {
		return fmt.Errorf("reliability: ack refused — envelope %s not receipted (state=%s)", path, env.State)
	}
	if len(env.Completion) == 0 {
		return fmt.Errorf("reliability: ack refused — envelope %s marked receipted without a durable completion (contradiction, kept for inspection)", path)
	}
	oc := owedCleanup{material: MaterialOf(env)}
	in.cleanupOwed[path] = oc
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		delete(in.cleanupOwed, path)
		return fmt.Errorf("reliability: ack remove: %w", err)
	}
	if sderr := syncDirFunc(filepath.Dir(path)); sderr != nil {
		return fmt.Errorf("reliability: ack dir sync: %w", sderr)
	}
	in.finalizeCleanup(path)
	return nil
}

// finalizeCleanup releases the unacked capacity for one envelope whose removal
// barrier is now durable, and drops its owed account so the release happens
// EXACTLY ONCE . Callers hold in.mu.
func (in *Inbox) finalizeCleanup(path string) {
	delete(in.cleanupOwed, path)
	in.pending.Add(-1)
}

// DrainCleanups 为"unlink 已落地、但目录同步仍未成功"的信封补齐所欠的 ack-清理屏障：
// 每个本次同步成功的账目恰好释放一次容量，并交回受保护材料供调用方释放保留租约；仍无法
// 同步的账目继续欠着，留待下一次排空。每轮调用都安全。
func (in *Inbox) DrainCleanups() []UnackedMaterial {
	in.mu.Lock()
	defer in.mu.Unlock()
	var released []UnackedMaterial
	for path, oc := range in.cleanupOwed {
		if err := syncDirFunc(filepath.Dir(path)); err != nil {
			continue
		}
		released = append(released, oc.material)
		in.finalizeCleanup(path)
	}
	return released
}

// UnackedMaterial describes the durable originals that an outstanding (not-yet-
// acked) envelope still depends on. The recovery owner rebuilds the store's
// retention lease from these so the originals survive TTL/capacity/compaction until
// the envelope is acked (dir-synced) and released.
type UnackedMaterial struct {
	// ReceiptKey is the processing-receipt EventKey in hex, reserved on prepare
	// ("" if the envelope never got a receipt key). Returned verbatim: the
	// reliability leaf holds it as an opaque identity and does not parse it.
	ReceiptKey string
	// FactKeys are the canonical fact EventKeys frozen in the slots' prepared_fact
	// (int64, parsed straight from the opaque JSON via its event_key field). A slot
	// with no prepared_fact contributes nothing (never committed → nothing to protect).
	FactKeys []int64
}

// MaterialOf extracts the protected originals of one envelope: the fact
// EventKeys frozen in its slots' prepared_fact plus the reserved receipt key (hex,
// returned verbatim — the reliability leaf does not parse it). Used by both the
// startup lease rebuild and the per-ack release so protect/release derive the SAME
// key set. A nil env yields the zero material.
func MaterialOf(env *Envelope) UnackedMaterial {
	if env == nil {
		return UnackedMaterial{}
	}
	m := UnackedMaterial{ReceiptKey: env.ReceiptKey}
	for i := range env.Messages {
		pf := env.Messages[i].PreparedFact
		if len(pf) == 0 {
			continue
		}
		var keyOnly struct {
			EventKey int64 `json:"event_key"`
		}
		if json.Unmarshal(pf, &keyOnly) == nil && keyOnly.EventKey != 0 {
			m.FactKeys = append(m.FactKeys, keyOnly.EventKey)
		}
	}
	return m
}

// MaterialOfPath reads one envelope from disk and returns its material, used
// by the ack path to release exactly what the lease protected for that envelope.
// os.IsNotExist (already acked/absent) → (zero, false, nil).
func (in *Inbox) MaterialOfPath(path string) (UnackedMaterial, bool, error) {
	if in == nil {
		return UnackedMaterial{}, false, nil
	}
	env, err := readEnvelope(path)
	if err != nil {
		if os.IsNotExist(err) {
			return UnackedMaterial{}, false, nil
		}
		return UnackedMaterial{}, false, err
	}
	return MaterialOf(env), true, nil
}

// UnackedMaterial enumerates the protected originals of every un-acked envelope
// (a *.json file still present in the inbox == pending/claimed/receipted; an acked
// envelope's file is already removed). This is the  "rebuild the lease from
// existing unacked material" source: it reads the on-disk envelopes directly and
// introduces NO second persistence table. Read-only. A dir-read failure is returned
// so the caller can fail conservative (do not open forgetting on an incomplete view).
func (in *Inbox) UnackedMaterial() ([]UnackedMaterial, error) {
	if in == nil || in.dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(in.dir)
	if err != nil {
		return nil, fmt.Errorf("reliability: read inbox dir for material: %w", err)
	}
	var out []UnackedMaterial
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		env, rerr := readEnvelope(filepath.Join(in.dir, e.Name()))
		if rerr != nil {
			continue
		}
		out = append(out, MaterialOf(env))
	}
	return out, nil
}

// OutstandingEnvelope is one still-present inbox envelope from the cold-start
// inventory: the full original plus its path, for the agent layer to
// reconcile DIRECTLY against each envelope's own fixed receipt key — the
// confirmation list is never harvested from the projection scan (spec L162:
// an outstanding receipt whose key predates the snapshot/tail window would be
// missed and re-executed forever).
type OutstandingEnvelope struct {
	Path string
	Env  *Envelope
	// ReadErr is the raw per-file failure (nil when decodable). A missing file
	// (os.IsNotExist) is simply skipped; any other error means the original is
	// present but unreadable NOW (open already quarantined corrupt items, so
	// this is new I/O trouble) — the reconcile must block on it conservatively,
	// never guess a confirmation or a deletion.
	ReadErr error
}

// Outstanding inventories every un-acked envelope (any state: pending/claimed/
// receipted) with its full original. Read-only under the mutation lock; it
// deletes, claims, rewrites or requeues NOTHING — reconcile dispositions are
// the caller's protocol-level decisions (Ack/Quarantine/retain).
func (in *Inbox) Outstanding() ([]OutstandingEnvelope, error) {
	if in == nil || in.dir == "" {
		return nil, nil
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	entries, err := os.ReadDir(in.dir)
	if err != nil {
		return nil, fmt.Errorf("reliability: read inbox dir for outstanding: %w", err)
	}
	var out []OutstandingEnvelope
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(in.dir, e.Name())
		env, rerr := readEnvelope(path)
		if rerr != nil {
			if os.IsNotExist(rerr) {
				continue
			}
			out = append(out, OutstandingEnvelope{Path: path, ReadErr: rerr})
			continue
		}
		out = append(out, OutstandingEnvelope{Path: path, Env: env})
	}
	return out, nil
}

// Dir returns the envelope directory (diagnostics/logging).
func (in *Inbox) Dir() string {
	if in == nil {
		return ""
	}
	return in.dir
}

// Pending returns the unconfirmed (pending+claimed+receipted) count.
func (in *Inbox) Pending() int64 {
	if in == nil {
		return 0
	}
	return in.pending.Load()
}

// Close refuses further use; on-disk items are intentionally left intact
// (shutdown keeps unconfirmed inputs durable for the next process).
func (in *Inbox) Close() error {
	in.mu.Lock()
	in.closed = true
	in.mu.Unlock()
	in.closeOnce.Do(func() { close(in.dead) })
	return nil
}

func (in *Inbox) seqPath(n int64) string {
	return filepath.Join(in.dir, fmt.Sprintf("%020d.json", n))
}

// nextClaimable returns the OLDEST claimable envelope: a pending item (claimed
// on the way out) or a receipted-but-unacked one — which is RETURNED to the
// caller, never swept here:
// receipted cleanup routes exclusively through Ack, which owns the barrier,
// the cleanup account and the exactly-once capacity release, and whose caller
// pairs it with releaseRetention (the leaf has no store handle).
// Claimed items are skipped: they belong to the in-flight consumer; a crashed
// process requeues them at open time.
func (in *Inbox) nextClaimable() (string, *Envelope, error) {
	entries, err := os.ReadDir(in.dir)
	if err != nil {
		return "", nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(in.dir, name)
		env, rerr := readEnvelope(path)
		if rerr != nil {
			moved, qerr := in.quarantineFile(path, "unreadable during claim: "+rerr.Error())
			if qerr != nil {
				return "", nil, qerr
			}
			if moved {
				in.pending.Add(-1)
			}
			continue
		}
		switch env.State {
		case InboxStateReceipted:
			return path, env, nil
		case InboxStatePending:
			return path, env, nil
		default:
			continue
		}
	}
	return "", nil, nil
}

// quarantineFile moves one inbox item into the quarantine dir. It reports
// whether the file actually moved and surfaces every failure:
//   - already absent (a prior attempt moved it): (false, nil) — the caller may
//     treat the disposition as complete (idempotent re-entry);
//   - rename failed: (false, err) — nothing moved, capacity MUST stay booked;
//   - moved but the dir barrier failed: (true, err) — the item is out of the
//     claim path; a power-loss rollback may resurrect it, and the next scan
//     re-quarantines (ENOENT → already absent) or the tombstone refuses the
//     revival, so the leak self-heals; the error still surfaces because the
//     rename is not yet durable.
//
// Callers must bind capacity decrements to moved==true, never to the bare
// call (a silent rename failure used to drop pending and lose the item's
// accounting while its bytes stayed in the claim dir).
func (in *Inbox) quarantineFile(path, reason string) (bool, error) {
	qdir := filepath.Join(in.dir, inboxQuarantine)
	dst := filepath.Join(qdir, filepath.Base(path))
	if err := os.Rename(path, dst); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reliability: quarantine rename %s: %w", path, err)
	}
	serr := syncDirFunc(in.dir)
	if derr := syncDirFunc(qdir); serr == nil {
		serr = derr
	}
	logWarnf("reliability: quarantined inbox item %s (%s) — kept for inspection, not silently destroyed", dst, reason)
	if serr != nil {
		return true, fmt.Errorf("reliability: quarantine dir-sync after moving %s: %w", path, serr)
	}
	return true, nil
}

func parseInboxSeq(name string) (int64, error) {
	var n int64
	if _, err := fmt.Sscanf(strings.TrimSuffix(name, ".json"), "%020d", &n); err != nil {
		return 0, err
	}
	return n, nil
}

// validateSourceEvent refuses a Message that is absent (empty), JSON null, or
// unparseable. A well-formed
// JSON value — a valid empty-text OR a non-text (image) payload — is legal input
// and passes; "有效空输入与非法输入 SHALL 区分" places the boundary at nil/broken,
// not at "empty text".
func validateSourceEvent(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty source_event (lossless input required)")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("nil source_event (Message is null)")
	}
	if !json.Valid(raw) {
		return fmt.Errorf("source_event is not valid JSON")
	}
	// : an external_input source event MUST carry a non-nil Message. A well-formed
	// object with "message":null or no message field still passes json.Valid, so the
	// receive boundary checks it explicitly — otherwise the write-before prepare barrier
	// would dereference a nil Message. Non-external_input events may legitimately have a
	// nil Message, so they are not gated here.
	var probe struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("source_event is not a decodable AgentEvent: %w", err)
	}
	if probe.Type == "external_input" {
		if len(probe.Message) == 0 || bytes.Equal(bytes.TrimSpace(probe.Message), []byte("null")) {
			return fmt.Errorf("external_input source_event has a nil Message (lossless input required)")
		}
	}
	return nil
}

// readEnvelope parses and VALIDATES an on-disk envelope. A missing/unknown
// version or any missing required field is an error (the caller quarantines
// it) — never a partially-valid input that continues to be consumed .
func readEnvelope(path string) (*Envelope, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if env.Version != envelopeVersion {
		return nil, fmt.Errorf("unsupported envelope version %d (want %d)", env.Version, envelopeVersion)
	}
	if env.RequestID == "" || len(env.Messages) == 0 {
		return nil, fmt.Errorf("envelope missing required fields")
	}
	switch env.State {
	case InboxStatePending, InboxStateClaimed, InboxStateReceipted:
	default:
		return nil, fmt.Errorf("envelope has illegal state %q", env.State)
	}
	for i := range env.Messages {
		if env.Messages[i].Slot != i {
			return nil, fmt.Errorf("envelope slot %d mis-numbered (got %d)", i, env.Messages[i].Slot)
		}
		if err := validateSourceEvent(env.Messages[i].SourceEvent); err != nil {
			return nil, fmt.Errorf("envelope slot %d: %w", i, err)
		}
		if len(env.Messages[i].PreparedFact) > 0 && env.Messages[i].PreparedVersion != PreparedVersionCurrent {
			return nil, fmt.Errorf("envelope slot %d prepared_fact under incompatible version %d (want %d)",
				i, env.Messages[i].PreparedVersion, PreparedVersionCurrent)
		}
	}
	return &env, nil
}

func writeEnvelopeFile(path string, env *Envelope) (writeOutcome, error) {
	raw, err := json.Marshal(env)
	if err != nil {
		return outcomeNotPublished, fmt.Errorf("marshal envelope: %w", err)
	}
	tmp := path + fmt.Sprintf(".%d.tmp", os.Getpid())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return outcomeNotPublished, fmt.Errorf("create inbox tmp: %w", err)
	}
	if _, err = f.Write(raw); err != nil {
		f.Close()
		os.Remove(tmp)
		return outcomeNotPublished, fmt.Errorf("write inbox tmp: %w", err)
	}
	if err = f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return outcomeNotPublished, fmt.Errorf("fsync inbox file: %w", err)
	}
	if err = f.Close(); err != nil {
		os.Remove(tmp)
		return outcomeNotPublished, fmt.Errorf("close inbox tmp: %w", err)
	}
	if h := testWriteStageHook; h != nil {
		h("tmp")
	}
	if err = os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return outcomeNotPublished, fmt.Errorf("rename inbox: %w", err)
	}
	if h := testWriteStageHook; h != nil {
		h("renamed")
	}
	if sderr := syncDirFunc(filepath.Dir(path)); sderr != nil {
		return outcomePublishUncertain, fmt.Errorf("reliability: envelope dir sync: %w", sderr)
	}
	return outcomeDurable, nil
}

// jsonEqual reports whether two raw JSON values are semantically equal
// (key-order-insensitive) WHILE preserving exact integer identity — used to make
// prepare/completion idempotent without depending on byte-for-byte marshalling,
// yet never merging two distinct big-integer event keys.
func jsonEqual(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	av, aerr := decodeExactJSON(a)
	bv, berr := decodeExactJSON(b)
	if aerr != nil || berr != nil {
		return false
	}
	am, merrA := json.Marshal(av)
	bm, merrB := json.Marshal(bv)
	if merrA != nil || merrB != nil {
		return false
	}
	return bytes.Equal(am, bm)
}

// decodeExactJSON parses a JSON value preserving integer identity: UseNumber
// turns every numeric literal into a json.Number (string-backed), so adjacent
// int64 event keys above 2^53 — which a float64 decode would collapse to the
// same value — stay distinct through the equality round-trip. Re-marshalling a
// json.Number emits the original literal verbatim.
func decodeExactJSON(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}
