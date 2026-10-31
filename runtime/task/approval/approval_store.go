// The wiring face of the approval channel: the manager that holds requests,
// the option seam that injects its clock and rescan window, and the file-backed
// request directory an external approver writes into. The words it records
// (status, subject, evidence, state, request, channel, delivery gap) are stated
// once in approval.go; this file only keeps and resolves them, so the vocabulary
// has one spelling and the store has one job.
package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/plugin"
)

// ApprovalOption configures an approval manager at construction.
type ApprovalOption func(*ApprovalManager)

// WithApprovalClock injects the clock the manager reads its time from, so a
// test drives validity windows deterministically instead of sleeping.
func WithApprovalClock(now func() time.Time) ApprovalOption {
	return func(a *ApprovalManager) { a.now = now }
}

// WithApprovalRescanInterval injects the throttled window between two directory
// rescans. The window exists so an external approver's write to a request file
// becomes visible at runtime without rescanning on every read; zero rescans on
// every unapproved read, which a test uses to stay deterministic.
func WithApprovalRescanInterval(window time.Duration) ApprovalOption {
	return func(a *ApprovalManager) { a.rescanInterval = window }
}

// approvalRescanInterval is the default minimum interval between two rescans of
// the approvals directory after an unapproved read.
const approvalRescanInterval = 2 * time.Second

// defaultApprovalTTL is the validity a request is issued with when the caller
// asks for none.
const defaultApprovalTTL = 30 * time.Minute

// approvalArgsPreview bounds the argument preview an approver reads.
const approvalArgsPreview = 500

// ApprovalManager is the waiting book of the approval channel: one request per
// file under <dir>/approvals/, an in-memory index beside it, and a throttled
// rescan through which a ruling an external approver wrote to a request file
// becomes visible at runtime. The manager is concurrent safe.
//
// The manager owns the digest binding: it computes the digest of the exact
// argument bytes at registration and every ruling it honours is matched
// against that digest, so an approval of other arguments never runs.
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
type ApprovalManager struct {
	dir            string
	ttl            time.Duration
	rescanInterval time.Duration
	now            func() time.Time

	mu         sync.RWMutex
	index      map[string]*ApprovalRequest
	channels   []ApprovalChannel
	gaps       []DeliveryGap
	lastRescan int64
}

// NewApprovalManager returns the approval manager over one directory. A non-empty
// dir keeps the requests under <dir>/approvals/ as one file per request and
// rebuilds the index from what it finds there; an empty dir keeps the manager in
// memory only. The ttl is the validity a request is issued with.
func NewApprovalManager(dir string, ttl time.Duration, opts ...ApprovalOption) *ApprovalManager {
	if ttl <= 0 {
		ttl = defaultApprovalTTL
	}
	a := &ApprovalManager{
		ttl:            ttl,
		rescanInterval: approvalRescanInterval,
		now:            time.Now,
		index:          map[string]*ApprovalRequest{},
	}
	for _, opt := range opts {
		opt(a)
	}
	if dir != "" {
		a.dir = filepath.Join(dir, "approvals")
		_ = os.MkdirAll(a.dir, 0o755)
		a.rebuild()
	}
	return a
}

// AddChannel attaches one outbound delivery face the manager offers its
// requests to. A channel that fails records a visible gap and never blocks the
// gate.
func (a *ApprovalManager) AddChannel(ch ApprovalChannel) {
	if ch == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.channels = append(a.channels, ch)
}

// DeliveryGaps reports the visible channel failures recorded so far.
func (a *ApprovalManager) DeliveryGaps() []DeliveryGap {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]DeliveryGap(nil), a.gaps...)
}

// Request registers one pending approval request under the approval identity
// tagent issued when the call was parked and writes it to its own file beside
// offering it to every attached channel. The manager computes the argument
// digest itself, so the registration keeps the digest binding the hold was
// issued over.
//
// The request file lands before the in-memory index names the request: a
// registration is the file, so a ruling of that identity is written only after
// the pending body it rules on reached the disk, and the two writes of one
// identity never share a temporary file. The write and the index entry it
// registers are one step of the store lock, so no rescan slips between them. A
// channel that fails records a visible gap
// and never fails the registration (the gate is a gate, never a wall), and a
// repeated registration under the same identity keeps the request already stored.
// A request that cannot be written to its file is reported to the caller beside
// the request, which stays registered in memory: a hold waits either way and is
// never turned into a denial by a channel-side failure.
func (a *ApprovalManager) Request(ctx context.Context, id, tool string, jsonArgs []byte, risk plugin.RiskLevel, rule, reason string) (*ApprovalRequest, error) {
	if id == "" {
		return nil, ErrApprovalIdentityMissing
	}
	now := a.now()
	req := &ApprovalRequest{
		ApprovalSubject: ApprovalSubject{
			ID:         id,
			ToolName:   tool,
			ArgsDigest: ArgsDigest(jsonArgs),
		},
		ApprovalEvidence: ApprovalEvidence{
			ArgsPreview: truncateArgs(string(jsonArgs), approvalArgsPreview),
			RiskLevel:   risk,
			RuleID:      rule,
			Reason:      reason,
		},
		ApprovalState: ApprovalState{
			CreatedMs: now.UnixMilli(),
			ExpiresMs: now.Add(a.ttl).UnixMilli(),
			Status:    ApprovalPending,
		},
	}
	a.mu.Lock()
	if stored, ok := a.index[id]; ok {
		a.mu.Unlock()
		return stored, nil
	}
	snapshot := *req
	err := a.write(&snapshot)
	a.index[id] = req
	channels := append([]ApprovalChannel(nil), a.channels...)
	a.mu.Unlock()
	for _, ch := range channels {
		if deliverErr := ch.Deliver(ctx, snapshot); deliverErr != nil {
			a.recordGap(DeliveryGap{Request: id, Err: deliverErr})
		}
	}
	return &snapshot, err
}

// Decide records the ruling of one approval request and reports the request
// with the ruling that took effect. The first ruling takes effect and a
// repeated response is idempotent: it reports the first ruling again instead of
// consuming a second one. A ruling over an unknown identity is refused beside
// ErrApprovalNotFound, a ruling that names no known decision is refused beside
// ErrApprovalRulingUnknown, and a ruling that arrived after the request expired
// is refused beside ErrApprovalExpired — an expired request is treated as not
// approved and is never made runnable by a late response. The ruling reaches the
// request file under the store lock the index entry is read and rewritten under,
// so a rescan never reads a pending body back over a landed ruling.
func (a *ApprovalManager) Decide(id string, status ApprovalStatus, by string) (*ApprovalRequest, error) {
	if status != ApprovalApproved && status != ApprovalDenied {
		return nil, ErrApprovalRulingUnknown
	}
	a.mu.Lock()
	req, ok := a.index[id]
	if !ok {
		a.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrApprovalNotFound, id)
	}
	if req.Status == ApprovalApproved || req.Status == ApprovalDenied {
		snapshot := *req
		a.mu.Unlock()
		return &snapshot, nil
	}
	if a.now().UnixMilli() >= req.ExpiresMs {
		a.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrApprovalExpired, id)
	}
	req.Status = status
	req.DecidedBy = by
	snapshot := *req
	err := a.write(&snapshot)
	a.mu.Unlock()
	return &snapshot, err
}

// Lookup reports the request the gate reads one (tool, arguments) pair under:
// an unexpired approved request — the approval the held call may proceed on —
// or, when none is approved, the newest request of that pair with its effective
// status: pending, denied, or expired once its validity ran out. The effective
// status is what the returned copy reports, so a request whose validity ran out
// reads as expired and never as a standing approval.
//
// An unapproved read triggers a throttled rescan of the request directory, so a
// ruling an external approver wrote to a request file becomes visible at
// runtime instead of waiting for a restart.
func (a *ApprovalManager) Lookup(tool, argsDigest string) *ApprovalRequest {
	if req := a.lookupIndex(tool, argsDigest); req != nil && req.Status == ApprovalApproved {
		return req
	}
	if a.rescanDue() {
		a.rebuild()
	}
	return a.lookupIndex(tool, argsDigest)
}

// Pending reports the requests still awaiting a ruling and within their
// validity, oldest first, for an approval entry to display and answer.
func (a *ApprovalManager) Pending() []*ApprovalRequest {
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := a.now().UnixMilli()
	out := make([]*ApprovalRequest, 0, len(a.index))
	for _, req := range a.index {
		if req.Status == ApprovalPending && req.ExpiresMs > now {
			snapshot := *req
			out = append(out, &snapshot)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedMs < out[j].CreatedMs })
	return out
}

// lookupIndex reads the current index for one (tool, arguments) pair under the
// selection the gate documents: an unexpired approved request wins, otherwise
// the newest request reports its effective status. The returned request is a
// copy, so a reader never observes a concurrent write.
func (a *ApprovalManager) lookupIndex(tool, argsDigest string) *ApprovalRequest {
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := a.now().UnixMilli()
	var newest *ApprovalRequest
	for _, req := range a.index {
		if req.ToolName != tool || req.ArgsDigest != argsDigest {
			continue
		}
		effective := effectiveStatus(req, now)
		if effective == ApprovalApproved {
			snapshot := *req
			return &snapshot
		}
		if newest == nil || req.CreatedMs > newest.CreatedMs {
			newest = req
		}
	}
	if newest == nil {
		return nil
	}
	snapshot := *newest
	snapshot.Status = effectiveStatus(newest, now)
	return &snapshot
}

// effectiveStatus reports the state one request is read under: a request whose
// validity ran out reads as expired whatever its recorded state, so nothing
// proceeds on an old request afterwards.
func effectiveStatus(req *ApprovalRequest, nowMs int64) ApprovalStatus {
	if req.ExpiresMs > 0 && nowMs >= req.ExpiresMs {
		return ApprovalExpired
	}
	return req.Status
}

// rescanDue reports whether the throttled window since the last rescan has
// elapsed, and moves the window forward when it has.
func (a *ApprovalManager) rescanDue() bool {
	now := a.now().UnixNano()
	a.mu.Lock()
	defer a.mu.Unlock()
	if now-a.lastRescan < int64(a.rescanInterval) {
		return false
	}
	a.lastRescan = now
	return true
}

// recordGap stores one visible channel failure beside the request it was for.
func (a *ApprovalManager) recordGap(gap DeliveryGap) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gaps = append(a.gaps, gap)
}

// path reports the file one request lives in.
func (a *ApprovalManager) path(id string) string {
	return filepath.Join(a.dir, id+".json")
}

// write persists one request as its own file through a rename, so a reader
// never observes half a request.
func (a *ApprovalManager) write(req *ApprovalRequest) error {
	if a.dir == "" {
		return nil
	}
	raw, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return err
	}
	final := a.path(req.ID)
	if err := os.WriteFile(final+".tmp", raw, 0o644); err != nil {
		return err
	}
	return os.Rename(final+".tmp", final)
}

// rebuild rebuilds the in-memory index from the request directory and removes
// the files whose validity ran out: an expired request is cleaned up, and a
// ruling an external approver wrote into a request file is read into the index
// so the gate sees it.
func (a *ApprovalManager) rebuild() {
	if a.dir == "" {
		return
	}
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return
	}
	now := a.now().UnixMilli()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(a.dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req ApprovalRequest
		if err := json.Unmarshal(raw, &req); err != nil || req.ID == "" {
			continue
		}
		if req.ExpiresMs > 0 && req.ExpiresMs < now {
			_ = os.Remove(path)
			if existing, ok := a.index[req.ID]; ok {
				existing.Status = ApprovalExpired
			} else {
				req.Status = ApprovalExpired
				a.index[req.ID] = &req
			}
			continue
		}
		a.index[req.ID] = &req
	}
}

// truncateArgs bounds the argument preview an approver reads.
func truncateArgs(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
