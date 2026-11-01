// The wiring face of the approval channel: the manager that holds requests, the
// option seam that injects its clock and rescan window, and the file face an
// external approver answers. The words it records (status, subject, evidence,
// state, request, channel, delivery gap) are stated once in approval.go; this
// file keeps the evidence an approver reads and folds presence and ruling from
// the attention facts, so the vocabulary has one spelling and the chain has the
// truth.
//
// - a registration files one attention question and a ruling files one attention answer, so the status a request reports is the answer its question carries
// - presence is the question the chain still holds as a question, and the waiting face reports exactly that set
// - the request file is the write entry of an external approver and the mirror of the evidence block, never a second truth
// - a ruling found in a request file lands as an answer, and the file is withdrawn once that answer landed
//
// The redline list (4 rules):
// MUST answer presence and ruling from the attention facts alone, never from the request file or the evidence block.
// MUST bind every ruling to the argument digest through the question identity, so changed arguments never ride an old approval.
// MUST keep the request file at the path external approvers already write, its three blocks flat.
// MUST NOT run an unapproved call: a store that cannot record the request leaves the gate waiting, never executing.
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// ApprovalOption configures an approval manager at construction.
type ApprovalOption func(*ApprovalManager)

// WithApprovalClock injects the clock the manager reads its time from, so a
// test drives validity windows deterministically instead of sleeping.
func WithApprovalClock(now func() time.Time) ApprovalOption {
	return func(a *ApprovalManager) { a.now = now }
}

// WithApprovalRescanInterval injects the throttled window between two rescans of
// the request directory. The window exists so an external approver's write to a
// request file becomes visible at runtime without rescanning on every read; zero
// rescans on every unapproved read, which a test uses to stay deterministic.
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
// file under <dir>/approvals/, the attention facts that carry its presence and
// its ruling, and a throttled rescan through which a ruling an external approver
// wrote to a request file lands on the chain. The manager is concurrent safe.
//
// The manager owns the digest binding: it computes the digest of the exact
// argument bytes at registration and every ruling it honours is matched against
// that digest through the question lineage, so an approval of other arguments
// never runs.
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
type ApprovalManager struct {
	dir            string
	ttl            time.Duration
	rescanInterval time.Duration
	now            func() time.Time

	chain *attentionLedger
	face  *task.Attention

	mu         sync.RWMutex
	evidence   map[string]*ApprovalRequest
	channels   []ApprovalChannel
	gaps       []DeliveryGap
	lastRescan int64
}

// NewApprovalManager returns the approval manager over one directory. A non-empty
// dir keeps the requests under <dir>/approvals/ as one file per request and
// restores the attention questions and answers the files state; an empty dir
// keeps the manager on its in-memory chain alone. The ttl is the validity a
// request is issued with.
func NewApprovalManager(dir string, ttl time.Duration, opts ...ApprovalOption) *ApprovalManager {
	if ttl <= 0 {
		ttl = defaultApprovalTTL
	}
	a := &ApprovalManager{
		ttl:            ttl,
		rescanInterval: approvalRescanInterval,
		now:            time.Now,
		evidence:       map[string]*ApprovalRequest{},
	}
	for _, opt := range opts {
		opt(a)
	}
	a.chain = newAttentionLedger()
	a.face = task.NewAttention(a.chain, task.WithAttentionClock(func() time.Time { return a.now() }))
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
// tagent issued when the call was parked: it files the attention question the
// request stands on, mirrors the request into its own file, and offers it to
// every attached channel. The manager computes the argument digest itself, so the
// registration keeps the digest binding the hold was issued over.
//
// A repeated registration of one approval identity, and a registration of
// arguments a standing question already covers, read that request back instead of
// filing a second question, so one held call is one question on the chain. The
// question is filed before the answer it waits for, so no ruling names a question
// the chain does not hold. A channel that fails records a visible gap and never
// fails the registration (the gate is a gate, never a wall). A request that
// cannot be written to its file, or whose question the chain refuses, is reported
// to the caller beside the request and the wait stands either way: a hold is
// never turned into a denial by a store-side failure.
// A caller that asks under an id of its own while a standing question already
// holds the pair names that same question: its id resolves the same ruling, so
// deciding by the id one minted never misses the question the gate filed first.
func (a *ApprovalManager) Request(ctx context.Context, id, tool string, jsonArgs []byte, risk plugin.RiskLevel, rule, reason string) (*ApprovalRequest, error) {
	if id == "" {
		return nil, ErrApprovalIdentityMissing
	}
	digest := ArgsDigest(jsonArgs)
	now := a.now()
	a.mu.Lock()
	if stored, ok := a.evidence[id]; ok {
		snapshot := *stored
		a.mu.Unlock()
		return &snapshot, nil
	}
	facts := a.factsLocked(ctx)
	if standing := a.standingLocked(facts, tool, digest, now.UnixMilli()); standing != nil {
		a.evidence[id] = standing
		snapshot := *standing
		snapshot.Status = ApprovalPending
		snapshot.DecidedBy = ""
		a.mu.Unlock()
		return &snapshot, nil
	}
	req := &ApprovalRequest{
		ApprovalSubject: ApprovalSubject{
			ID:         id,
			ToolName:   tool,
			ArgsDigest: digest,
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
	snapshot := *req
	writeErr := a.write(&snapshot)
	a.evidence[id] = req
	commitErr := a.fileQuestion(ctx, req)
	channels := append([]ApprovalChannel(nil), a.channels...)
	a.mu.Unlock()
	for _, ch := range channels {
		if deliverErr := ch.Deliver(ctx, snapshot); deliverErr != nil {
			a.recordGap(DeliveryGap{Request: id, Err: deliverErr})
		}
	}
	return &snapshot, errors.Join(writeErr, commitErr)
}

// Decide records the ruling of one approval request as the attention answer its
// question waits for, and reports the request with the ruling that took effect.
// The first ruling takes effect and a repeated response is idempotent: it reports
// the answer already on the chain instead of consuming a second one. A ruling over
// an unknown identity is refused beside ErrApprovalNotFound, a ruling that names
// no known decision is refused beside ErrApprovalRulingUnknown, and a ruling that
// arrived after the request expired is refused beside ErrApprovalExpired — an
// expired request is treated as not approved and is never made runnable by a late
// response. The ruling reaches the request file as its mirror under the store lock
// the evidence block is read and rewritten under, so a rescan never reads a
// pending body back over a landed ruling.
func (a *ApprovalManager) Decide(id string, status ApprovalStatus, by string) (*ApprovalRequest, error) {
	if status != ApprovalApproved && status != ApprovalDenied {
		return nil, ErrApprovalRulingUnknown
	}
	ctx := context.Background()
	a.mu.Lock()
	req, ok := a.evidence[id]
	if !ok {
		a.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrApprovalNotFound, id)
	}
	facts := a.factsLocked(ctx)
	if answer, answered := facts.answer(req.ToolName, req.ArgsDigest, id); answered {
		snapshot := *req
		if ruling, known := rulingIn(answer); known {
			snapshot.Status = ruling
		}
		a.mu.Unlock()
		return &snapshot, nil
	}
	if a.now().UnixMilli() >= req.ExpiresMs {
		a.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrApprovalExpired, id)
	}
	req.Status = status
	req.DecidedBy = by
	if err := a.fileAnswer(ctx, req); err != nil {
		a.mu.Unlock()
		return nil, err
	}
	snapshot := *req
	err := a.write(&snapshot)
	a.mu.Unlock()
	return &snapshot, err
}

// Lookup reports the request the gate reads one (tool, arguments) pair under: an
// unexpired approved request — the approval the held call may proceed on — or,
// when none is approved, the newest request of that pair with its effective
// status: pending, denied, or expired once its validity ran out. The status is
// folded from the attention facts and the effective status from the validity the
// question was filed under, so a request whose validity ran out reads as expired
// and never as a standing approval.
//
// An unapproved read triggers a throttled rescan of the request directory, so a
// ruling an external approver wrote to a request file lands on the chain at
// runtime instead of waiting for a restart.
func (a *ApprovalManager) Lookup(tool, argsDigest string) *ApprovalRequest {
	if req := a.lookupFacts(tool, argsDigest); req != nil && req.Status == ApprovalApproved {
		return req
	}
	if a.rescanDue() {
		a.rebuild()
	}
	return a.lookupFacts(tool, argsDigest)
}

// AttentionPresence reports the approval questions the attention chain still
// holds as questions: the one presence every reader of the channel shares, so
// an answered approval leaves this read exactly as it leaves the waiting face,
// whatever audit record a delivery channel keeps beside it.
func (a *ApprovalManager) AttentionPresence(ctx context.Context) []task.AttentionQuestion {
	a.mu.RLock()
	face := a.face
	a.mu.RUnlock()
	if face == nil {
		return nil
	}
	present, err := face.Present(ctx, task.AttentionFilter{Kind: task.AttentionKindApproval})
	if err != nil {
		return nil
	}
	return present
}

// Pending reports the requests still awaiting a ruling and within their validity,
// oldest first, for an approval entry to display and answer. The set is the
// presence the attention chain holds, so the waiting face and a presence read
// name the same questions.
func (a *ApprovalManager) Pending() []*ApprovalRequest {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ctx := context.Background()
	facts := a.factsLocked(ctx)
	now := a.now().UnixMilli()
	out := make([]*ApprovalRequest, 0, len(a.evidence))
	for _, held := range a.evidence {
		if held.ExpiresMs > 0 && now >= held.ExpiresMs {
			continue
		}
		if !facts.present(held.ToolName, held.ArgsDigest, held.ID) {
			continue
		}
		snapshot := *held
		snapshot.Status = ApprovalPending
		snapshot.DecidedBy = ""
		out = append(out, &snapshot)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedMs < out[j].CreatedMs })
	return out
}

// factsLocked reads the chain as the approval channel reads it. The caller holds
// at least the read lock, so one read answers every request of one call.
func (a *ApprovalManager) factsLocked(ctx context.Context) approvalFacts {
	held, err := a.chain.AttentionFacts(ctx)
	if err != nil {
		return approvalFacts{}
	}
	return factsOf(held)
}

// standingLocked reports the request one (tool, arguments) pair is already
// standing under: a question the chain still holds as a question, within its
// validity. A repeated registration reads that request back instead of filing a
// second question over the same arguments, and it reads the one record back: the
// identity it asks under names the same ruling, so a ruling taken through either
// identity leaves both reading the approver the answer carries instead of the
// copy the second registration happened to take.
func (a *ApprovalManager) standingLocked(facts approvalFacts, tool, argsDigest string, nowMs int64) *ApprovalRequest {
	for _, held := range a.evidence {
		if held.ToolName != tool || held.ArgsDigest != argsDigest {
			continue
		}
		if held.ExpiresMs > 0 && nowMs >= held.ExpiresMs {
			continue
		}
		if !facts.present(tool, argsDigest, held.ID) {
			continue
		}
		return held
	}
	return nil
}

// lookupFacts folds the evidence block and the attention facts into the request
// the gate reads one (tool, arguments) pair under: an unexpired approved request
// wins, otherwise the newest request of that pair reports its effective status.
// The returned request is a copy, so a reader never observes a concurrent write.
func (a *ApprovalManager) lookupFacts(tool, argsDigest string) *ApprovalRequest {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ctx := context.Background()
	facts := a.factsLocked(ctx)
	now := a.now().UnixMilli()
	var newest *ApprovalRequest
	var newestState ApprovalState
	for _, held := range a.evidence {
		if held.ToolName != tool || held.ArgsDigest != argsDigest {
			continue
		}
		state := stateOf(facts, held)
		state.Status = effectiveStatus(state.Status, held.ExpiresMs, now)
		if state.Status == ApprovalApproved {
			snapshot := *held
			snapshot.ApprovalState = state
			return &snapshot
		}
		if newest == nil || held.CreatedMs > newest.CreatedMs {
			newest, newestState = held, state
		}
	}
	if newest == nil {
		return nil
	}
	snapshot := *newest
	snapshot.ApprovalState = newestState
	return &snapshot
}

// effectiveStatus reports the state one request is read under: a request whose
// validity ran out reads as expired whatever the chain recorded, so nothing
// proceeds on an old request afterwards.
func effectiveStatus(status ApprovalStatus, expiresMs, nowMs int64) ApprovalStatus {
	if expiresMs > 0 && nowMs >= expiresMs {
		return ApprovalExpired
	}
	return status
}

// fileQuestion files the attention question one registration raises: the reason
// the call was held as its text and the validity the request was issued with.
func (a *ApprovalManager) fileQuestion(ctx context.Context, req *ApprovalRequest) error {
	filing := approvalFact(event.TypeAttentionQuestion, req.ToolName, req.ArgsDigest, req.ID)
	filing.Detail = task.AttentionDetail{Text: req.Reason, ExpiresMs: req.ExpiresMs}
	_, err := a.face.Commit(ctx, filing)
	return err
}

// fileAnswer files the attention answer one ruling states: the status word as its
// body and the approver who stated it as its ruler.
func (a *ApprovalManager) fileAnswer(ctx context.Context, req *ApprovalRequest) error {
	filing := approvalFact(event.TypeAttentionAnswer, req.ToolName, req.ArgsDigest, req.ID)
	filing.Detail = task.AttentionDetail{Text: string(req.Status), By: req.DecidedBy}
	_, err := a.face.Commit(ctx, filing)
	return err
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

// rebuild restores the attention facts the request directory states, so a
// restarted manager reads back the waits it held before it stopped: a pending
// file restores its question, a decided file whose question is new restores both,
// and a ruling an external approver wrote over a question already on the chain
// lands as the answer and withdraws the file it was read from. A file whose
// validity ran out is removed instead of restored.
func (a *ApprovalManager) rebuild() {
	if a.dir == "" {
		return
	}
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return
	}
	now := a.now().UnixMilli()
	ctx := context.Background()
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
			continue
		}
		restored := req
		a.evidence[req.ID] = &restored
		facts := a.factsLocked(ctx)
		_, asked := facts.question(req.ToolName, req.ArgsDigest, req.ID)
		_, answered := facts.answer(req.ToolName, req.ArgsDigest, req.ID)
		settled := req.Status == ApprovalApproved || req.Status == ApprovalDenied
		if !asked {
			_ = a.fileQuestion(ctx, &restored)
			if settled {
				_ = a.fileAnswer(ctx, &restored)
			}
			continue
		}
		if !answered && settled {
			_ = a.fileAnswer(ctx, &restored)
			_ = os.Remove(path)
		}
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
