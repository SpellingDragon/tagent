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
		// Platform-unsupported dir fsync degrades silently here (the file
		// itself was already fsynced); real I/O errors are reported.
		return serr
	}
	return nil
}

// syncDirFunc is the injectable dir-sync seam used by the atomic envelope write
// (defaults to syncDir). §3.2 tests force a post-rename dir-sync failure to
// observe the publish-uncertain outcome without needing real power loss.
var syncDirFunc = syncDir

// testGateHook, when non-nil (TEST ONLY), runs inside Enqueue after the liveness
// fast-check and before taking the mutation lock. It gives §3.3 a deterministic
// way to interleave a completed Close with an in-flight Enqueue and prove the two
// are coordinated under one lock. Always nil in production.
var testGateHook func()

// testWriteStageHook, when non-nil (TEST ONLY), is called by writeEnvelopeFile
// right AFTER the tmp file is written+fsynced+closed (stage "tmp") and right
// AFTER the rename landed, before the directory sync (stage "renamed"). §8.3
// crash-window children exit inside this hook, so the kill lands exactly at
// the audited production write step — enqueue, claim and prepare rewrites all
// flow through writeEnvelopeFile and are covered by the same two stages.
// Always nil in production.
var testWriteStageHook func(stage string)

// ==================== Inbox-v2（durable 输入信箱，fix-resident-reliability-boundaries D2）====================
//
// 可靠模式下 ALL inbound inputs are persisted HERE before acknowledgement —
// not just the overflow (the old SpillStore only caught channel overflow, so
// a low-load durable input still lived only in the channel and died with the
// process). inbox-v2 is LOSSLESS: every message slot keeps a full JSON
// snapshot of the original AgentEvent (source_event) so a restart restores the
// ID/Type/Source/Timestamp/business Metadata that inbox-v1 dropped (F1), and
// slot indices are never compacted (F4).
//
// Lifecycle of one envelope (two-phase completion, D3):
//
//	Enqueue    → file written (tmp+rename+fsync+dirsync), state=pending
//	Claim      → atomically rewritten state=claimed (NOT deleted — crash safe)
//	Prepare    → first claim freezes each slot's prepared_fact + a reserved
//	             receipt_key in ONE durable rewrite BEFORE any fact is written
//	Completion → post-turn per-slot disposition frozen durably (completion)
//	Receipt    → state=receipted once the completion's fact-chain receipt is
//	             submitted; crash before this → the claim replays
//	Ack        → file removed (+dirsync); repeated Ack is idempotent
//
// The inbox is the durable truth ONLY for undelivered/unconfirmed inputs;
// once facts are in the event chain the chain owns history. Corrupt items and
// unknown-format versions go to quarantine (kept, alerted) — never silently
// consumed or destroyed.

var (
	// ErrInboxFull: pending ≥ maxPending — explicit rejection, never a silent
	// fallback to volatile (delta spec「可靠输入全序持久化」).
	ErrInboxFull = errors.New("reliability: durable inbox full")
	// §3.7 (design 决策10): previous-format (.spill / inbox-v1) data no longer
	// blocks boot — the runtime loads ONLY the current format and treats legacy
	// items as inert (never guessed, never consumed), pending an explicit managed
	// ResetTransitional. The old drain-as-precondition errors are removed; only
	// current-format corruption (quarantine) still blocks reopen.
	// ErrQuarantineUndispositioned: a prior run quarantined unreadable/unknown-
	// version items the operator has not dispositioned. Reopening must refuse
	// rather than silently re-ignore them every boot (D2 migration gate, 3.5).
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
	// auto-consumption (§4.2「确定冲突隔离并停止自动消费」). ErrReceiptKeyConflict is a
	// specific instance of this class.
	ErrPrepareConflict = errors.New("reliability: prepare hit a deterministic conflict")
	// ErrReceiveUncertain: an envelope's rename landed on disk but its directory
	// sync failed (§3.2, design 决策2). The original and its allocated sequence are
	// retained — no later input may reuse that sequence to overwrite it — but the
	// receive MUST NOT be reported as durable-accepted; reconciliation on reopen
	// owns the item. It is distinct from a definite not-published failure.
	ErrReceiveUncertain = errors.New("reliability: receive published but durability unconfirmed")
	// ErrInboxClosed is returned by Enqueue once Close has been called. The
	// liveness decision is made under the mutation lock so an Enqueue whose
	// pre-lock fast-check raced a completed Close cannot register afterwards (§3.3).
	ErrInboxClosed = errors.New("reliability: inbox closed")
)

const (
	inboxDirName = "inbox-v2"
	// legacyInboxV1DirName is the directory written by the previous binary. §3.7:
	// v2 never guesses v1's lossy format, but its presence no longer blocks boot —
	// v2 loads only the current format and leaves v1 inert until a managed reset.
	legacyInboxV1DirName = "inbox-v1"
	// spillFileExt is the dead SpillStore overflow file's extension, kept only so
	// §3.7 classification/reset can recognize (never read) legacy .spill items.
	spillFileExt = ".spill"
	// envelopeVersion is the explicit, REQUIRED format version of every v2
	// envelope. A missing or other version is unreadable and quarantined —
	// never consumed (D2「版本不识别...不得作为有效输入继续消费」).
	envelopeVersion = 2

	// PreparedVersionCurrent is the prepare-format version stamped onto a slot
	// when its prepared_fact is frozen (task 3.5). A slot carrying material under
	// any other (or absent) version is incompatible transitional material: it is
	// NEVER parsed by a legacy reader or silently consumed — readEnvelope rejects
	// it so the caller quarantines the envelope (§3.7 owns the explicit reset).
	PreparedVersionCurrent = 1

	inboxQuarantine = "quarantine"

	// InboxStatePending / Claimed / Receipted are the three durable states.
	InboxStatePending   = "pending"
	InboxStateClaimed   = "claimed"
	InboxStateReceipted = "receipted"
)

// writeOutcome is the tri-state result of an atomic envelope write (§3.2, design
// 决策2). A caller MUST distinguish "definitely not published" (no final file →
// safe to retry on a fresh sequence) from "renamed into place but its directory
// entry is unconfirmed" (the original exists and must be retained, capacity
// reserved, sequence never reused) from "durable" (accepted). Only outcomeDurable
// may be reported as accepted; outcomePublishUncertain keeps the landed original.
type writeOutcome int

const (
	outcomeNotPublished     writeOutcome = iota // failed before/at rename: no final file
	outcomePublishUncertain                     // rename landed, dir sync failed
	outcomeDurable                              // final file present and dir-synced
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
	// MUST reuse it verbatim — never restamp time/attribution/summary (D2).
	PreparedFact json.RawMessage `json:"prepared_fact,omitempty"`
	// PreparedVersion is the prepare-format version the prepared_fact was frozen
	// under (PreparedVersionCurrent); 0 means no material yet. readEnvelope
	// rejects a slot that has material under a non-current version, so
	// incompatible transitional material can never be replayed through a legacy
	// parser (task 3.5「不保留旧解析器」).
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
	// aggregation surface over it is deferred (resident-review-fixes 5.3,
	// design 决策 7). Kept for retry forensics, not read to drive behaviour.
	Attempts int `json:"attempts,omitempty"`
	// ReceiptKey is the processing-receipt EventKey (hex) reserved on FIRST
	// prepare. It represents a RESERVED identity only, not completion (D2).
	ReceiptKey string `json:"receipt_key,omitempty"`
	// Messages are the fixed-slot inbound inputs (one per producer message).
	Messages []MessageSlot `json:"messages"`
	// Completion is the frozen post-turn receipt payload + per-slot
	// processed/skipped disposition. Absent = no terminal state yet (D3).
	Completion json.RawMessage `json:"completion,omitempty"`
}

// ReceiptCredential is the verified receipt credential RecordReceipt requires
// (§5.4, design 决策 L126「RecordReceipt 必须要求合法 completion 与已核验回执
// 凭据」). It carries ONLY the reserved receipt-key identity under which the
// caller has verified the fact-chain receipt — it is never a free-form
// description string or a request id, the two weak confirmation shapes §5.4
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
	mu        sync.Mutex    // serializes seq allocation + claim rewrite (全序)
	seq       atomic.Int64  // monotonic, lexicographic = enqueue order
	pending   atomic.Int64  // pending+claimed+receipted (unconfirmed) count
	dead      chan struct{} // closed on Close: Enqueue/Claim refuse afterwards
	closeOnce sync.Once
	closed    bool // authoritative close flag guarded by mu; Enqueue refuses once set (§3.3)

	// pathsByRequestID: requestID → envelope path for the envelopes still on
	// disk — the receipt-reconcile bridge looks up the envelope a fact-chain
	// receipt belongs to.
	pathsByRequestID map[string]string

	// cleanupOwed is the independent cleanup account (§3.6, spec L96): paths whose
	// envelope file was unlinked by Ack but whose directory-sync barrier failed,
	// so the removal is not yet durable. Capacity (pending) and the retention lease
	// are NOT released for these until a retry/drain completes the barrier — exactly
	// once (L110). The path is seq-named and never reused, so the account keys are
	// stable; on restart it is re-derived from surviving files (a gone file is simply
	// not counted, its lease not re-armed).
	cleanupOwed map[string]owedCleanup

	// transitional records previous-format data detected at open (§3.7). It is
	// never read or consumed; boot proceeds on the current format and only an
	// explicit operator-confirmed ResetTransitional clears it. Read-only after open.
	transitional transitionalData
}

// owedCleanup records what a completed-but-unsynced Ack removal still owes: the
// request id (to drop the path mapping) and the protected material (so the caller
// releases the retention lease once, when the barrier finally syncs).
type owedCleanup struct {
	requestID string
	material  UnackedMaterial
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
	// §5.7 (spec L96/L162「冷启动先同步目录并清点」): the directory barrier runs
	// BEFORE the inventory scan — an entry whose unlink/dir-sync previously died
	// mid-flight must be settled (present or gone) before anything is counted,
	// or the scan view itself is uncertain. A sync failure blocks the open
	// (fail-loud): counting and reconcile never run on a possibly-stale listing.
	if err := syncDirFunc(envDir); err != nil {
		return nil, fmt.Errorf("reliability: inbox dir sync at open: %w", err)
	}
	// Upgrade gates run BEFORE the scan so a legacy-format tree is refused as a
	// whole, before any v2 item is requeued or any quarantined item is touched.
	// Refusing must not mutate the tree — the previous binary owns draining.
	// §3.7: quarantine (CURRENT-format unreadable/unknown-version items) still
	// blocks reopen — corruption must surface, never be silently wiped. Legacy
	// previous-format items no longer block boot: they are classified as inert
	// transitional data (never read) and only an explicit managed reset clears them.
	if err := checkQuarantineDispositioned(filepath.Join(envDir, inboxQuarantine)); err != nil {
		return nil, err
	}
	transitional := classifyTransitional(filepath.Dir(envDir))
	in := &Inbox{dir: envDir, max: maxPending, dead: make(chan struct{}), pathsByRequestID: map[string]string{}, cleanupOwed: map[string]owedCleanup{}, transitional: transitional}

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
			// Unreadable name → quarantine the whole file (kept, alerted).
			in.quarantineFile(filepath.Join(envDir, name), "bad name: "+name)
			continue
		}
		if n > maxSeq {
			maxSeq = n
		}
		env, rerr := readEnvelope(filepath.Join(envDir, name))
		if rerr != nil {
			// Unreadable body / unknown version → quarantine the whole file
			// (kept, alerted); it was never a confirmable envelope, so it
			// never counted. v1 or corrupt items are never consumed.
			in.quarantineFile(filepath.Join(envDir, name), "unreadable envelope: "+rerr.Error())
			continue
		}
		if env.RequestID != "" {
			in.pathsByRequestID[env.RequestID] = filepath.Join(envDir, name)
		}
		switch env.State {
		case InboxStateClaimed:
			// Crash between claim and receipt: back to pending for replay.
			env.State = InboxStatePending
			env.Attempts++
			if _, werr := writeEnvelopeFile(filepath.Join(envDir, name), env); werr != nil {
				return nil, fmt.Errorf("reliability: requeue claimed %s: %w", name, werr)
			}
			unconfirmed++
		case InboxStateReceipted:
			// Receipt is durable — stays; consumer Ack-skips without re-exec.
			unconfirmed++
		default: // pending
			unconfirmed++
		}
	}
	in.seq.Store(maxSeq)
	in.pending.Store(unconfirmed)
	return in, nil
}

// transitionalData enumerates previous-format files found at open (§3.7, design
// 决策10). They are never read: the runtime loads ONLY the current format. The
// ONLY thing that removes them is an explicit, operator-confirmed ResetTransitional.
type transitionalData struct {
	spill []string // legacy *.spill files (dead SpillStore overflow format)
	v1    []string // leftover inbox-v1 items v2 cannot losslessly reinterpret
}

func (t transitionalData) present() bool { return len(t.spill) > 0 || len(t.v1) > 0 }

// classifyTransitional scans (read-only, NEVER mutating) for previous-format data
// under parent: stray *.spill in the parent and *.json items under inbox-v1. It
// deliberately does NOT inspect inbox-v2 (the live format) and never deletes — it
// only reports, so boot can proceed on the current format with legacy left inert.
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
// envelopes — corruption that MUST surface, never be silently wiped (§3.7: current
// corruption never triggers a reset). Read-only; the operator must disposition first.
func checkQuarantineDispositioned(quarantineDir string) error {
	q, err := os.ReadDir(quarantineDir)
	if err != nil {
		return nil // no quarantine dir yet is fine
	}
	for _, e := range q {
		if !e.IsDir() {
			return fmt.Errorf("%w: %s", ErrQuarantineUndispositioned, e.Name())
		}
	}
	return nil
}

// TransitionalData reports the previous-format items detected at open (§3.7). They
// are inert — never read or consumed — and remain on disk until an explicit,
// operator-confirmed ResetTransitional. The slices are copies (safe to retain).
func (in *Inbox) TransitionalData() (spill, v1 []string) {
	return append([]string(nil), in.transitional.spill...), append([]string(nil), in.transitional.v1...)
}

// ResetTransitional is the one-time managed reset of previous-format data (§3.7,
// design 决策10) for the reliability leaf, and is intentionally the ONLY destructive
// path here. Safety guards (never derived into arbitrary-delete power):
//   - Requires an EXPLICIT confirm; a reset is an operator act, never automatic.
//   - Removes ONLY the legacy files enumerated at open (stray *.spill and
//     inbox-v1/*.json), which are disjoint from the live inbox-v2 tree and
//     referenced by no current fact — so clearing them leaves no dangling reference
//     (the recovery-unit consistency rule is about CURRENT data).
//   - Never touches inbox-v2 or its quarantine (current corruption must surface, not
//     be wiped), nor any path outside this leaf's own directory tree.
//   - Refuses while closed or while any envelope is unacked (pending>0): a managed
//     reset needs exclusive writer access, not an in-flight turn.
//
// Current-format corruption and ordinary I/O failures are NOT transitional and are
// never cleared here. Returns the number of legacy files removed.
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
	leafParent := filepath.Dir(in.dir) // the BusSpillDir root; live tree is <root>/inbox-v2
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
	in.transitional = transitionalData{} // enumerated set cleared
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
	// §3.3: authoritative liveness decision under the lock — if a Close completed
	// while this Enqueue sat between its fast-check and here, closed is now observed
	// and the receive is refused, so no item is registered after Close returned.
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
		env.Messages[i].Slot = i // fixed slot index, never compacted later
	}
	// §3.2: a sequence, once allocated, is NEVER rolled back — a failed write leaves
	// a hole (a cleaned-up tmp, or a landed-but-unconfirmed original), which is what
	// forbids a later input from reusing this sequence to overwrite an uncertain
	// original (design 决策2 "序号分配后永不回退，允许空洞").
	path := in.seqPath(n)
	oc, werr := writeEnvelopeFile(path, env)
	if oc == outcomePublishUncertain {
		// Rename landed: keep the durable original AND reserve its capacity, but
		// report the receive as uncertain (never durable-accepted); reopen owns it.
		in.pending.Add(1)
		if env.RequestID != "" {
			in.pathsByRequestID[env.RequestID] = path
		}
		return 0, fmt.Errorf("%w: sequence %d retained: %v", ErrReceiveUncertain, n, werr)
	}
	if oc != outcomeDurable {
		// outcomeNotPublished: no final file exists; the sequence stays consumed as a
		// hole so no future write reuses it. The caller may retry with a fresh one.
		return 0, werr
	}
	in.pending.Add(1)
	if env.RequestID != "" {
		in.pathsByRequestID[env.RequestID] = path
	}
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
	path, env, err := in.nextClaimable()
	if err != nil || env == nil {
		return nil, "", err
	}
	if env.State == InboxStateReceipted {
		// A settled item: hand it to the caller unchanged (no re-claim, no Attempts++)
		// so it Acks + releases retention. Re-claiming would resurrect finished work.
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
			continue // partial prepare: leave this slot as-is
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
// on disk for operator inspection (never destroyed). The §4.2 submit gate uses it
// to isolate a deterministic-conflict input rather than silently retry or drop it.
// It reports whether an envelope was actually present and moved — the caller uses
// the true result as the release point for the envelope's §2.8 retention holders
// (an already-gone/unreadable envelope protects nothing and returns false).
func (in *Inbox) QuarantineEnvelope(path, reason string) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	env, err := readEnvelope(path)
	if err != nil {
		return false // already gone or unreadable: nothing left to isolate
	}
	in.quarantineFile(path, reason)
	if env.RequestID != "" {
		delete(in.pathsByRequestID, env.RequestID)
	}
	in.pending.Add(-1)
	return true
}

// ReleaseClaim returns a claimed envelope to pending so a later Pull re-claims it
// in strict sequence order. The §4.2 submit gate uses it to back off a transient
// I/O failure WITHOUT acking, dropping, or consuming the input — order and bounded
// backpressure are preserved (the oldest stuck envelope is re-claimed before any
// newer arrival). A no-op if the file is gone or is no longer in the claimed state.
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
		return nil // only a live claim is released
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
	// §3.3: even when the completion is already durable with identical content,
	// re-run the write so an earlier attempt that landed the rename but failed its
	// dir-sync still has its barrier completed — an idempotent retry must NOT
	// early-return merely because the content matches ("不因内容相同提前成功").
	env.Completion = completion
	if _, werr := writeEnvelopeFile(path, env); werr != nil {
		return fmt.Errorf("reliability: completion write: %w", werr)
	}
	return nil
}

// RecordReceipt durably marks a claimed envelope as processed (claimed →
// receipted) ONLY on valid evidence (§5.4, design L126). Three gates, none of
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
		return nil // idempotent
	}
	// ① No receipt without a durable, structurally-legal completion. The
	// completion's full schema is owned by the agent layer; this leaf gates its
	// PRESENCE and JSON legality only (D2 opaqueness).
	if len(env.Completion) == 0 {
		return fmt.Errorf("reliability: receipt refused — envelope %s has no durable completion (state=%s)", path, env.State)
	}
	if !json.Valid(env.Completion) {
		return fmt.Errorf("reliability: receipt refused — envelope %s carries an illegal (undecodable) completion", path)
	}
	// ② A completion frozen without a reserved key means the prepare phase never
	// established the two-phase identity; receipting would orphan the receipt.
	if env.ReceiptKey == "" {
		return fmt.Errorf("reliability: receipt refused — envelope %s has no reserved receipt key (two-phase protocol not established)", path)
	}
	// ③ The credential must carry and match the reserved receipt identity.
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
// §3.6/§5.6 (spec L96/L110): removal is only "done" once BOTH the unlink and
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
			// The file is already gone. Only complete the barrier + release when a
			// prior uncertain ack left an owed account (L96: "文件不存在的重试仍完成
			// 目录屏障"). Otherwise it was fully acked before — nothing owed, no
			// second capacity release.
			if oc, ok := in.cleanupOwed[path]; ok {
				if sderr := syncDirFunc(filepath.Dir(path)); sderr != nil {
					return fmt.Errorf("reliability: ack dir sync (owed %s): %w", path, sderr)
				}
				in.finalizeCleanup(path, oc)
			}
			return nil // already acked
		}
		return fmt.Errorf("reliability: ack read: %w", err)
	}
	if env.State != InboxStateReceipted {
		return fmt.Errorf("reliability: ack refused — envelope %s not receipted (state=%s)", path, env.State)
	}
	// §5.4 (spec L172-174): a receipted STATE without a matching durable completion is
	// a contradiction — deletion never keys off the status string alone. The original
	// is kept and surfaced; the startup reconcile (§5.7) reports/quarantines it.
	if len(env.Completion) == 0 {
		return fmt.Errorf("reliability: ack refused — envelope %s marked receipted without a durable completion (contradiction, kept for inspection)", path)
	}
	oc := owedCleanup{requestID: env.RequestID, material: MaterialOf(env)}
	// §5.6: the independent cleanup account is registered BEFORE the unlink —
	// from this point any failure at or after removal keeps the owed entry (the
	// dir-sync branch below), so an uncertain removal is never unaccounted.
	in.cleanupOwed[path] = oc
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		// Cleanup never started (the original is intact): cancel the account, a
		// phantom owed entry must never claim removal of a file still on disk.
		delete(in.cleanupOwed, path)
		return fmt.Errorf("reliability: ack remove: %w", err)
	}
	if sderr := syncDirFunc(filepath.Dir(path)); sderr != nil {
		// The removal's directory entry is not yet durable: keep the file's capacity
		// and its retention lease; the pre-registered account lets a retry/drain
		// finish the barrier and release exactly once (§3.6). Never report completion.
		return fmt.Errorf("reliability: ack dir sync: %w", sderr)
	}
	in.finalizeCleanup(path, oc)
	return nil
}

// finalizeCleanup releases the unacked capacity + request-id mapping for one
// envelope whose removal barrier is now durable, and drops its owed account so the
// release happens EXACTLY ONCE (L110). Callers hold in.mu.
func (in *Inbox) finalizeCleanup(path string, oc owedCleanup) {
	delete(in.cleanupOwed, path)
	if oc.requestID != "" {
		delete(in.pathsByRequestID, oc.requestID)
	}
	in.pending.Add(-1)
}

// DrainCleanups completes outstanding ack-cleanup barriers for envelopes whose
// unlink landed but whose dir-sync previously failed (§3.6/L96). For each account
// whose barrier now syncs successfully it releases capacity exactly once and returns
// the protected material so the caller drops the retention lease; accounts that
// still cannot sync stay owed for the next drain. Safe to call every turn.
func (in *Inbox) DrainCleanups() []UnackedMaterial {
	in.mu.Lock()
	defer in.mu.Unlock()
	var released []UnackedMaterial
	for path, oc := range in.cleanupOwed {
		if err := syncDirFunc(filepath.Dir(path)); err != nil {
			continue // still uncertain; keep the account and retry on the next drain
		}
		released = append(released, oc.material)
		in.finalizeCleanup(path, oc)
	}
	return released
}

// UnackedMaterial describes the durable originals that an outstanding (not-yet-
// acked) envelope still depends on (§2.8). The recovery owner rebuilds the store's
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

// MaterialOf extracts the protected originals of one envelope (§2.8): the fact
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

// MaterialOfPath reads one envelope from disk and returns its material (§2.8), used
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
// envelope's file is already removed). This is the §2.8 "rebuild the lease from
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
			continue // unreadable/quarantined files are handled at open; not protectable material
		}
		out = append(out, MaterialOf(env))
	}
	return out, nil
}

// OutstandingEnvelope is one still-present inbox envelope from the cold-start
// inventory (§5.7): the full original plus its path, for the agent layer to
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
				continue // acked concurrently — nothing outstanding
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
	// §3.3: flip the closed flag under the SAME lock Enqueue uses to register, so
	// the check-and-publish of a receive and the close are serialized — either an
	// item is fully registered before Close or a later Enqueue sees closed and is
	// refused. close(dead) stays for the non-locking fast paths (ClaimNext, etc.).
	in.mu.Lock()
	in.closed = true
	in.mu.Unlock()
	in.closeOnce.Do(func() { close(in.dead) })
	return nil
}

// ---- internals ----

func (in *Inbox) seqPath(n int64) string {
	return filepath.Join(in.dir, fmt.Sprintf("%020d.json", n))
}

// nextClaimable returns the OLDEST claimable envelope: a pending item (claimed
// on the way out) or a receipted-but-unacked one — which is RETURNED to the
// caller, never swept here (§5.6/L96「nextClaimable 不私自删除 receipted 项」):
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
	sort.Strings(names) // zero-padded seq → lexicographic = enqueue order
	for _, name := range names {
		path := filepath.Join(in.dir, name)
		env, rerr := readEnvelope(path)
		if rerr != nil {
			in.quarantineFile(path, "unreadable during claim: "+rerr.Error())
			in.pending.Add(-1)
			continue
		}
		switch env.State {
		case InboxStateReceipted:
			// A settled (receipted-but-unacked) envelope is RETURNED to the caller
			// (claimDurable) so it can Ack the item AND release the §2.8 retention in
			// one place. The leaf must NOT silently sweep it here: releaseRetention lives
			// in EventBus (the leaf has no store handle), so an internal remove would ack
			// the envelope but leak its protected fact/receipt originals forever.
			return path, env, nil
		case InboxStatePending:
			return path, env, nil
		default: // claimed: in-flight here; a crashed owner requeues at open
			continue
		}
	}
	return "", nil, nil
}

// removeAndSync removed an envelope and synced its directory. It was the
// receipted-sweep helper; receipted cleanup now routes through Ack (which owns
// the cleanup account) so the caller can pair it with releaseRetention. Deleted
// as dead code (§2.8 lease-release fix).

func (in *Inbox) quarantineFile(path, reason string) {
	dst := filepath.Join(in.dir, inboxQuarantine, filepath.Base(path))
	_ = os.Rename(path, dst)
	logWarnf("reliability: quarantined inbox item %s (%s) — kept for inspection, not silently destroyed", dst, reason)
}

func parseInboxSeq(name string) (int64, error) {
	var n int64
	if _, err := fmt.Sscanf(strings.TrimSuffix(name, ".json"), "%020d", &n); err != nil {
		return 0, err
	}
	return n, nil
}

// validateSourceEvent refuses a Message that is absent (empty), JSON null, or
// unparseable (§3.1 "JSON 不可编码...nil Message...在接收前拒绝"). A well-formed
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
	// §3.1: an external_input source event MUST carry a non-nil Message. A well-formed
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
// it) — never a partially-valid input that continues to be consumed (D2).
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
	// §3.1: the durable state must be one of the three legal ones — an illegal or
	// corrupt state is quarantined by the caller, never defaulted to pending and
	// silently consumed.
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
		// §3.5: material frozen under a non-current prepare version is incompatible
		// transitional data — reject so it is quarantined, never parsed by a legacy
		// reader or silently consumed. A pending slot with no material (version 0,
		// empty fact) is legal and will run its normal first prepare.
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
	// The rename landed — the envelope now exists at its final path. From here a
	// failure is NOT "not published": a dir-sync error is publish-uncertain, so the
	// caller retains the original and reserves its sequence rather than treating the
	// write as absent (the enqueue/claim result is then untrustworthy, never a hole).
	if sderr := syncDirFunc(filepath.Dir(path)); sderr != nil {
		return outcomePublishUncertain, fmt.Errorf("reliability: envelope dir sync: %w", sderr)
	}
	return outcomeDurable, nil
}

// jsonEqual reports whether two raw JSON values are semantically equal
// (key-order-insensitive) WHILE preserving exact integer identity — used to make
// prepare/completion idempotent without depending on byte-for-byte marshalling,
// yet never merging two distinct big-integer event keys (§3.1).
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
