package reliability

import (
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

// syncDirDurablyFs is the reliability-leaf dir-sync (best-effort, mirroring
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

// ==================== Inbox（T-G · durable 输入信箱，resident-readiness-plan 2/3.2）====================
//
// 可靠模式下 ALL inbound inputs are persisted HERE before acknowledgement —
// not just the overflow (the old SpillStore only caught channel overflow, so
// a low-load durable input still lived only in the channel and died with the
// process). Lifecycle of one envelope:
//
//	Enqueue → file written (tmp+rename+fsync+dirsync), state=pending
//	Claim   → atomically rewritten state=claimed (NOT deleted — crash safe)
//	Receipt → rewritten state=receipted (+processing evidence) once the turn
//	          that consumed it finished; crash before this → claimed replays
//	Ack     → file removed (+dirsync); repeated Ack is idempotent
//
// The inbox is the durable truth ONLY for undelivered/unconfirmed inputs;
// once facts are in the event chain the chain owns history. Corrupt items go
// to quarantine (kept, alerted) — never silently destroyed.

var (
	// ErrInboxFull: pending ≥ maxPending — explicit rejection, never a silent
	// fallback to volatile (delta spec「可靠输入全序持久化」).
	ErrInboxFull = errors.New("reliability: durable inbox full")
	// ErrLegacySpillNotDrained: pre-migration *.spill files exist — the new
	// binary refuses to start so old-format items are drained by the version
	// that wrote them (delta spec「旧格式或损坏项」).
	ErrLegacySpillNotDrained = errors.New("reliability: legacy .spill items present; drain them with the previous binary before upgrading to inbox-v1")
)

const (
	inboxDirName    = "inbox-v1"
	inboxQuarantine = "quarantine"
	// InboxStatePending / Claimed / Receipted are the three durable states.
	InboxStatePending   = "pending"
	InboxStateClaimed   = "claimed"
	InboxStateReceipted = "receipted"
)

// EnvelopeMessage is one inbound input inside an envelope batch.
type EnvelopeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Envelope is one durable acceptance unit: a batch of messages from one
// producer call, accepted (202) only after this whole file is durable.
type Envelope struct {
	RequestID string `json:"request_id"`
	Source    string `json:"source"`
	State     string `json:"state"`
	Attempts  int    `json:"attempts,omitempty"`
	Receipt   string `json:"receipt_note,omitempty"`
	// EventKeys 回写已提交事实的 EventKey（hex）：重放时消费者据此做事实链
	// 去重（同输入不重复入库/投影）。回写失败仅降级为 at-least-once 重执行，
	// 信封自身仍是 outstanding 真源（「去重索引只随 outstanding 存在」）。
	EventKeys []string          `json:"event_keys,omitempty"`
	Messages  []EnvelopeMessage `json:"messages"`
}

// RecordEventKeys durably writes the committed fact keys back onto the
// claimed envelope. Failure returns an error (caller logs; replay then
// degrades to at-least-once re-execution).
func (in *Inbox) RecordEventKeys(path string, keys []string) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	env, err := readEnvelope(path)
	if err != nil {
		return fmt.Errorf("reliability: event-keys read: %w", err)
	}
	// cold-eyes R2 S-2: dedup before append — replay writebacks re-submit the
	// same keys; without this the envelope's EventKeys grows without bound
	// across retry/crash cycles.
	existing := make(map[string]bool, len(env.EventKeys)+len(keys))
	for _, k := range env.EventKeys {
		existing[k] = true
	}
	for _, k := range keys {
		if k != "" && !existing[k] {
			existing[k] = true
			env.EventKeys = append(env.EventKeys, k)
		}
	}
	if werr := writeEnvelopeFile(path, env); werr != nil {
		return fmt.Errorf("reliability: event-keys write: %w", werr)
	}
	return nil
}

// Inbox is the durable input mailbox (concurrency-safe).
type Inbox struct {
	dir       string
	max       int
	mu        sync.Mutex    // serializes seq allocation + claim rewrite (全序)
	seq       atomic.Int64  // monotonic, lexicographic = enqueue order
	pending   atomic.Int64  // pending+claimed (unconfirmed) count
	dead      chan struct{} // closed on Close: Enqueue/Claim refuse afterwards
	closeOnce sync.Once

	// pathsByRequestID (cold-eyes Major 2): requestID → envelope path for the
	// envelopes still on disk — the receipt-reconcile bridge looks up the
	// envelope a fact-chain receipt belongs to.
	pathsByRequestID map[string]string
}

// NewInbox opens (or creates) an inbox-v1 at dir. On reopen: claimed items
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
	in := &Inbox{dir: envDir, max: maxPending, dead: make(chan struct{}), pathsByRequestID: map[string]string{}}

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
			// Unreadable body → quarantine the whole file (kept, alerted);
			// it was never a confirmable envelope, so it never counted.
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
			if werr := writeEnvelopeFile(filepath.Join(envDir, name), env); werr != nil {
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
	// Legacy spill leftovers block the upgrade path (fail-loud, not silent).
	parent := filepath.Dir(envDir)
	if legacy, lerr := os.ReadDir(parent); lerr == nil {
		for _, e := range legacy {
			if !e.IsDir() && strings.HasSuffix(e.Name(), spillExt) {
				return nil, fmt.Errorf("%w: %s", ErrLegacySpillNotDrained, e.Name())
			}
		}
	}
	in.seq.Store(maxSeq)
	in.pending.Store(unconfirmed)
	return in, nil
}

// Enqueue durably appends one envelope (whole-batch acceptance). The file is
// fsynced and its directory entry synced BEFORE returning — only then may the
// caller report a durable receipt.
func (in *Inbox) Enqueue(env *Envelope) (int64, error) {
	if in == nil || env == nil {
		return 0, fmt.Errorf("reliability: nil inbox/envelope")
	}
	if len(env.Messages) == 0 {
		return 0, fmt.Errorf("reliability: empty envelope")
	}
	select {
	case <-in.dead:
		return 0, fmt.Errorf("reliability: inbox closed")
	default:
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.pending.Load() >= int64(in.max) {
		return 0, ErrInboxFull
	}
	n := in.seq.Add(1)
	env.State = InboxStatePending
	path := in.seqPath(n)
	if err := writeEnvelopeFile(path, env); err != nil {
		in.seq.Add(-1) // release the failed slot
		return 0, err
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
	path, env, err := in.nextClaimable()
	if err != nil || env == nil {
		return nil, "", err
	}
	env.State = InboxStateClaimed
	env.Attempts++
	if werr := writeEnvelopeFile(path, env); werr != nil {
		return nil, "", fmt.Errorf("reliability: claim rewrite: %w", werr)
	}
	return env, path, nil
}

// RecordReceipt durably marks a claimed envelope as processed (the turn that
// consumed it finished). Crash before this → the claim replays (re-execute);
// crash after → replay skips execution and only Acks.
func (in *Inbox) RecordReceipt(path, note string) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	env, err := readEnvelope(path)
	if err != nil {
		return fmt.Errorf("reliability: receipt read: %w", err)
	}
	if env.State == InboxStateReceipted {
		return nil // idempotent
	}
	env.State = InboxStateReceipted
	env.Receipt = note
	if werr := writeEnvelopeFile(path, env); werr != nil {
		return fmt.Errorf("reliability: receipt write: %w", werr)
	}
	return nil
}

// Ack removes a confirmed envelope. Idempotent: a repeated Ack (or a lost
// remove that a later replay retries) is success, never a re-execution.
func (in *Inbox) Ack(path string) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	env, err := readEnvelope(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // already acked
		}
		return fmt.Errorf("reliability: ack read: %w", err)
	}
	if env.State != InboxStateReceipted {
		return fmt.Errorf("reliability: ack refused — envelope %s not receipted (state=%s)", path, env.State)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reliability: ack remove: %w", err)
	}
	syncDir(filepath.Dir(path)) // best-effort durability of the removal
	if env.RequestID != "" {
		delete(in.pathsByRequestID, env.RequestID)
	}
	in.pending.Add(-1)
	return nil
}

// ConfirmDurableByRequestID (cold-eyes Major 2): the startup receipt-reconcile
// bridge — a fact-chain receipt event proves the batch was fully committed and
// receipted before a crash; converge the envelope (claimed → receipted → ack)
// WITHOUT re-executing. Returns false when no live envelope carries the rid.
func (in *Inbox) ConfirmDurableByRequestID(rid string) bool {
	if in == nil || rid == "" {
		return false
	}
	in.mu.Lock()
	path, ok := in.pathsByRequestID[rid]
	in.mu.Unlock()
	if !ok {
		return false
	}
	if err := in.RecordReceipt(path, "reconciled from fact-chain receipt"); err != nil {
		return false
	}
	if err := in.Ack(path); err != nil {
		return false
	}
	return true
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
	in.closeOnce.Do(func() { close(in.dead) })
	return nil
}

// ---- internals ----

func (in *Inbox) seqPath(n int64) string {
	return filepath.Join(in.dir, fmt.Sprintf("%020d.json", n))
}

// nextClaimable returns the OLDEST pending envelope, clearing receipted
// items OUT OF THE WAY on the same scan (their processing already finished —
// Ack-skip, never re-execute). Claimed items are skipped: they belong to the
// in-flight consumer; a crashed process requeues them at open time.
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
			// Processing already finished durably: Ack-skip in place.
			if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
				return "", nil, fmt.Errorf("reliability: clear receipted %s: %w", name, rmErr)
			}
			syncDir(in.dir)
			in.pending.Add(-1)
			continue
		case InboxStatePending:
			return path, env, nil
		default: // claimed: in-flight here; a crashed owner requeues at open
			continue
		}
	}
	return "", nil, nil
}

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

func readEnvelope(path string) (*Envelope, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if env.RequestID == "" || len(env.Messages) == 0 || env.State == "" {
		return nil, fmt.Errorf("envelope missing required fields")
	}
	return &env, nil
}

func writeEnvelopeFile(path string, env *Envelope) error {
	raw, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	tmp := path + fmt.Sprintf(".%d.tmp", os.Getpid())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create inbox tmp: %w", err)
	}
	if _, err = f.Write(raw); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write inbox tmp: %w", err)
	}
	if err = f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("fsync inbox file: %w", err)
	}
	if err = f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close inbox tmp: %w", err)
	}
	if err = os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename inbox: %w", err)
	}
	if sderr := syncDir(filepath.Dir(path)); sderr != nil {
		// cold-eyes R2 Minor 6: the rename's directory entry must be durable
		// for the envelope to survive a crash — surface a failure instead of
		// silently ignoring it (the enqueue result is then untrustworthy).
		return fmt.Errorf("reliability: envelope dir sync: %w", sderr)
	}
	return nil
}
