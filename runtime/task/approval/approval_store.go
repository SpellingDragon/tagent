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

	"github.com/SpellingDragon/tagent/api/durable"
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

// WithApprovalSeat declares that the framework state slice seats this book: the
// book reads no request file at construction, and the restore of the stated state
// puts it back (see RestoreEvidence and RestoreBoot). A book wired through its
// directory alone keeps restoring itself from the files at construction.
func WithApprovalSeat() ApprovalOption {
	return func(a *ApprovalManager) { a.seated = true }
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
	seated         bool
	stated         bool
	factLog        durable.FactLog

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
// keeps the manager on its in-memory chain alone. A manager seated by the
// framework state slice (WithApprovalSeat) reads no file here: the restore of the
// stated state puts the book back instead. The ttl is the validity a request is
// issued with.
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
		if !a.seated {
			_ = a.rebuild(context.Background())
		}
	}
	return a
}

// BindAttention hands the book the durable face its attention chain is written to
// and reads it for the work the boot owes, and never for the history behind it.
//
// - A cut stated this book: the boot states the obligations still owed, so the read is each of their answer keys — the ruling a person filed while the process was down — and the resident set stays the cut's own. A request or a ruling of history is read by the reader that asks for it by identity (Lookup, Decide), never here.
// - No cut stated this book: the face is the one record of that book, so the whole stream is folded — the questions still standing, the rulings already stated and the request records that seat the identities those facts were filed under.
//
// The face is kept as the one every registration writes its request record to, so
// the identity an approver is handed outlives the process beside the facts it was
// filed with. The error belongs to the caller that can stop the boot — a chain this
// book could not read leaves the rulings behind the gate that serves them, and a
// book bound to no face writes no record.
func (a *ApprovalManager) BindAttention(ctx context.Context, log durable.FactLog) error {
	if a == nil || log == nil {
		return nil
	}
	if a.statedByCut() {
		if err := a.foldAnswersInForce(ctx, log); err != nil {
			return err
		}
	} else {
		if err := a.chain.bind(ctx, log); err != nil {
			return err
		}
		if err := a.foldRequestRecords(ctx, log); err != nil {
			return err
		}
	}
	a.mu.Lock()
	a.factLog = log
	a.mu.Unlock()
	return nil
}

// statedByCut reports whether a framework cut stated this book's obligations: a
// seated book that read one owes the face the tail those obligations gained and
// nothing else.
func (a *ApprovalManager) statedByCut() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.stated
}

// SnapshotEvidence reports the obligations the book still owes, for the framework
// state slice to state: the identity each caller asked under, the evidence an
// approver reads and the state each request stands in.
//
// - An expired hold is not stated: a restore drops it, so the cut would carry a request no process keeps.
// - A ruled request is stated only beside no durable face: a bound book reads its ruling back by identity.
// - A face-less book states its rulings: there the cut is the one record those arguments were ruled over.
//
// The identity each entry is stated under is separate from the request's own
// identity because a second registration aliases a caller identity onto one
// standing request: the two answers a cut must carry are the entry and the identity
// it is read under. The read takes the book's own read lock and nothing else, so it
// never waits on a write of its own nor flushes one.
func (a *ApprovalManager) SnapshotEvidence() ([]ApprovalRequest, []string) {
	if a == nil {
		return nil, nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := a.now().UnixMilli()
	rulingsStated := a.factLog == nil
	type stated struct {
		asked string
		held  ApprovalRequest
	}
	pairs := make([]stated, 0, len(a.evidence))
	for key, held := range a.evidence {
		if !statedByCut(*held, now, rulingsStated) {
			continue
		}
		pairs = append(pairs, stated{asked: key, held: *held})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].asked < pairs[j].asked })
	asked := make([]string, 0, len(pairs))
	out := make([]ApprovalRequest, 0, len(pairs))
	for _, pair := range pairs {
		asked = append(asked, pair.asked)
		out = append(out, pair.held)
	}
	return out, asked
}

// RestoreEvidence puts one stated approval state back: every request still within
// its validity is seated beside the question it stands on and, when a ruling
// landed, beside the answer that ruling was filed as, so a restarted book reports
// the same presence and the same rulings the process before it reported.
//
// The read is the stated state alone — no request file is opened. A hold past its
// validity is dropped rather than resurrected, because an expired hold never
// proceeds and a restart that put one back would hand a dead wait a second life.
// A ruled request is kept past its validity instead: the answer it stands on is
// the record that these exact arguments were refused or approved by a person, and
// a book that forgot it would mint a fresh request the same person never saw —
// the very request a restart must not be able to turn into an approval. It also
// states that a cut is this book's record, so the bind that follows reads that face
// for the work the cut left and never for the history behind it.
func (a *ApprovalManager) RestoreEvidence(ctx context.Context, stated []ApprovalRequest, keys []string) error {
	if a == nil {
		return nil
	}
	now := a.now().UnixMilli()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stated = true
	for i, held := range stated {
		if held.ID == "" || expiredWithoutRuling(held, now) {
			continue
		}
		restored := held
		a.evidence[aliasOf(keys, i, held.ID)] = &restored
		if err := a.fileQuestion(ctx, &restored); err != nil {
			return err
		}
		if restored.Status == ApprovalApproved || restored.Status == ApprovalDenied {
			if err := a.fileAnswer(ctx, &restored); err != nil {
				return err
			}
		}
	}
	return nil
}

// statedByCut reports whether one held request is a cut's to state: an obligation
// still open, and a ruled request only beside no durable face — a bound book reads
// a ruling back through the chain, so a cut costs the work still owed.
func statedByCut(held ApprovalRequest, nowMs int64, rulingsStated bool) bool {
	if held.Status == ApprovalApproved || held.Status == ApprovalDenied {
		return rulingsStated
	}
	return held.ExpiresMs <= 0 || nowMs < held.ExpiresMs
}

// expiredWithoutRuling reports whether one stated request is past its validity
// before any ruling landed on it.
func expiredWithoutRuling(held ApprovalRequest, nowMs int64) bool {
	if held.ExpiresMs <= 0 || nowMs < held.ExpiresMs {
		return false
	}
	return held.Status != ApprovalApproved && held.Status != ApprovalDenied
}

// aliasOf answers the identity one stated request is seated under: the caller
// identity the book answered before the cut, which is the request's own identity
// unless a second registration aliased it onto a standing request.
func aliasOf(keys []string, i int, id string) string {
	if i < len(keys) && keys[i] != "" {
		return keys[i]
	}
	return id
}

// RestoreBoot restores the book from the row of request files, for the boot whose
// state states no approval book: the files are the per-request durable write and
// the only record a process that took no cut left behind.
//
// A row this boot cannot read leaves the book empty and the failure visible beside
// it, never a boot that dies on it: an unreadable row states no ruling either way,
// so the restore it refuses approves nothing, and a call that still needs a ruling
// parks again and reports the same failure when it files its own request. A boot
// that stopped here would be a wall around a gate.
func (a *ApprovalManager) RestoreBoot(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if err := a.rebuild(ctx); err != nil {
		a.recordGap(DeliveryGap{Err: err})
	}
	return nil
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
		recordErr := a.recordRequestLocked(ctx, id, snapshot)
		a.mu.Unlock()
		return &snapshot, recordErr
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
	recordErr := a.recordRequestLocked(ctx, id, snapshot)
	channels := append([]ApprovalChannel(nil), a.channels...)
	a.mu.Unlock()
	for _, ch := range channels {
		if deliverErr := ch.Deliver(ctx, snapshot); deliverErr != nil {
			a.recordGap(DeliveryGap{Request: id, Err: deliverErr})
		}
	}
	return &snapshot, errors.Join(writeErr, commitErr, recordErr)
}

// Decide records the ruling of one approval request as the attention answer its
// question waits for, and reports the request with the ruling that took effect.
// The first ruling takes effect and a repeated response is idempotent: it reports
// the answer already on the chain instead of consuming a second one. A ruling over
// an identity the book holds no request for is answered from the chain that holds
// it, so a ruling filed before a restart still answers the identity the approver
// was handed; an identity the chain states no ruling for is refused beside
// ErrApprovalNotFound, a ruling that names no known decision is refused beside
// ErrApprovalRulingUnknown, and a ruling that arrived after the request expired is
// refused beside ErrApprovalExpired — an expired request is treated as not approved
// and is never made runnable by a late response. The ruling reaches the request
// file as its mirror under the store lock the evidence block is read and rewritten
// under, so a rescan never reads a pending body back over a landed ruling, and it
// reaches the request record of the answering identity, so the approver who ruled
// it is read back beside the ruling after a restart. A book that reached the chain
// drops the ruled request from its resident set once that record landed — the
// ruling is read back through the chain — so the book keeps the obligations still
// open; a book beside no face keeps it, the file being its only record. An identity
// the book holds no resident request for is ruled from the record the chain keeps
// under it, so an approver answering a request the cut left behind still rules that
// request instead of one a book would have to mint.
//
// - a ruling is filed before the mirror moves: the resident status changes only once the answer landed, so a refused write leaves the request owed in the cut
// - a write this book could not confirm is answered by reading the answer key back, so the ruling is the fact the store kept and never this call's guess
// - a face this book cannot read is named to the caller, never folded into "no ruling": an unread answer is not an answer that never landed
func (a *ApprovalManager) Decide(id string, status ApprovalStatus, by string) (*ApprovalRequest, error) {
	if status != ApprovalApproved && status != ApprovalDenied {
		return nil, ErrApprovalRulingUnknown
	}
	ctx := context.Background()
	ruled, stated, err := a.rulingFromChain(ctx, id)
	if err != nil || stated {
		return ruled, err
	}
	log := a.boundFace()
	a.mu.Lock()
	req, ok := a.evidence[id]
	if !ok {
		rulable, readErr := a.requestToRule(ctx, log, id)
		if readErr != nil {
			a.mu.Unlock()
			return nil, readErr
		}
		req = rulable
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
	ruling := *req
	ruling.Status = status
	ruling.DecidedBy = by
	answerErr := a.fileAnswer(ctx, &ruling)
	if answerErr != nil {
		landed, filed, readErr := a.foldFiledRuling(ctx, log, req)
		if readErr != nil {
			a.mu.Unlock()
			return nil, errors.Join(answerErr, readErr)
		}
		if !landed {
			a.mu.Unlock()
			return nil, answerErr
		}
		ruling.Status = filed
	}
	recordErr := a.recordRequestLocked(ctx, id, ruling)
	writeErr := a.write(&ruling)
	if resident, seated := a.evidence[id]; seated {
		*resident = ruling
	}
	if a.factLog != nil && recordErr == nil {
		delete(a.evidence, id)
	}
	a.mu.Unlock()
	return &ruling, errors.Join(answerErr, writeErr, recordErr)
}

// rulingFromChain answers the ruling one identity already carries on the durable
// face, for an approver answering a request this book holds no resident request
// for: a cut states the obligations still open, so a ruled identity is read back
// through the answer filed under the identity the record names. An identity the
// face holds no record for, and one whose record carries no ruling, are answered as
// unstated — the two reads a book answers from its own state. A face this book
// cannot read is named to the caller instead, so a refusal is never reported as a
// ruling that never landed.
func (a *ApprovalManager) rulingFromChain(ctx context.Context, id string) (*ApprovalRequest, bool, error) {
	log := a.boundFace()
	if log == nil {
		return nil, false, nil
	}
	req, stored, err := requestRecordOnFace(ctx, log, id)
	if err != nil || !stored {
		return nil, false, err
	}
	answer, answered, err := answerOnFace(ctx, log, req.ToolName, req.ArgsDigest, req.ID)
	if err != nil || !answered {
		return nil, false, err
	}
	ruling, known := rulingIn(answer)
	if !known {
		return nil, false, nil
	}
	req.Status = ruling
	return &req, true, nil
}

// requestToRule answers the request one caller identity was issued under, for an
// approver answering an identity this book holds no resident request for: the
// record the chain keeps under that identity, beside the question it was registered
// with, folded into the chain so the answer lands on the question the identity was
// asked and never on one a second registration minted. The reads are the identity's
// own keys — its record, its question — so the cost of answering an identity of
// history is that identity's alone. An identity the face states no record for is
// refused beside ErrApprovalNotFound rather than answered by a request minted here.
func (a *ApprovalManager) requestToRule(ctx context.Context, log durable.FactLog, id string) (*ApprovalRequest, error) {
	if log == nil {
		return nil, fmt.Errorf("%w: %s", ErrApprovalNotFound, id)
	}
	req, stored, err := requestRecordOnFace(ctx, log, id)
	if err != nil {
		return nil, err
	}
	if !stored {
		return nil, fmt.Errorf("%w: %s", ErrApprovalNotFound, id)
	}
	question, kept, err := questionOnFace(ctx, log, req.ToolName, req.ArgsDigest, req.ID)
	if err != nil {
		return nil, err
	}
	if !kept {
		return nil, fmt.Errorf("%w: %s", task.ErrAttentionQuestionMissing, req.ID)
	}
	if err := a.chain.bindFacts([]event.Event{question}, log); err != nil {
		return nil, err
	}
	return &req, nil
}

// boundFace reports the durable face this book files its facts on, read under the
// book's read lock so a bind landing beside a read is never half seen.
func (a *ApprovalManager) boundFace() durable.FactLog {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.factLog
}

// Lookup reports the request the gate reads one (tool, arguments) pair under: an
// unexpired approved request — the approval the held call may proceed on — or,
// when none is approved, the newest request of that pair with its effective
// status: pending, denied, or expired once its validity ran out. The status is
// folded from the attention facts and the effective status from the validity the
// question was filed under, so a request whose validity ran out reads as expired
// and never as a standing approval.
//
// - A pair with no resident request is answered from the chain, by the identity it was filed under: the newest record of that pair, ruled or still open.
// - The read a ruling costs belongs to the gate that asks, never to the boot that restores the book.
//
// An unapproved read triggers a throttled rescan of the request directory, so a
// ruling an external approver wrote to a request file lands on the chain at
// runtime instead of waiting for a restart.
func (a *ApprovalManager) Lookup(tool, argsDigest string) *ApprovalRequest {
	if req := a.lookupFacts(tool, argsDigest); req != nil && req.Status == ApprovalApproved {
		return req
	}
	if a.rescanDue() {
		_ = a.rebuild(context.Background())
	}
	if req := a.lookupFacts(tool, argsDigest); req != nil {
		return req
	}
	return a.lookupOnChain(tool, argsDigest)
}

// lookupOnChain answers the request one (tool, arguments) pair carries on the
// durable face under the identity its request record names. The pair is read the
// way the resident block is read — an unexpired approval wins, the newest
// otherwise — so a restarted book and the process before it answer one status for
// one pair, and an open record of that pair reads as the pending hold it stands in
// rather than as no request at all. Nothing is seated: the record stays where it
// landed, and the next ask reads it again.
func (a *ApprovalManager) lookupOnChain(tool, argsDigest string) *ApprovalRequest {
	log := a.boundFace()
	if log == nil {
		return nil
	}
	ctx := context.Background()
	records, err := storedRequestRecords(ctx, log)
	if err != nil {
		a.recordGap(DeliveryGap{Err: err})
		return nil
	}
	pairs := make([]ApprovalRequest, 0, len(records))
	for _, record := range records {
		if record.Request.ToolName != tool || record.Request.ArgsDigest != argsDigest {
			continue
		}
		pairs = append(pairs, record.Request)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].CreatedMs != pairs[j].CreatedMs {
			return pairs[i].CreatedMs > pairs[j].CreatedMs
		}
		return pairs[i].ID < pairs[j].ID
	})
	now := a.now().UnixMilli()
	var newest *ApprovalRequest
	for _, held := range pairs {
		answer, answered, err := answerOnFace(ctx, log, held.ToolName, held.ArgsDigest, held.ID)
		if err != nil {
			a.recordGap(DeliveryGap{Request: held.ID, Err: err})
			return nil
		}
		state := ApprovalState{CreatedMs: held.CreatedMs, ExpiresMs: held.ExpiresMs, Status: ApprovalPending}
		if answered {
			if ruling, known := rulingIn(answer); known {
				state.Status = ruling
				state.DecidedBy = held.DecidedBy
			}
		}
		state.Status = effectiveStatus(state.Status, held.ExpiresMs, now)
		snapshot := held
		snapshot.ApprovalState = state
		if state.Status == ApprovalApproved {
			return &snapshot
		}
		if newest == nil {
			newest = &snapshot
		}
	}
	return newest
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
// - The evidence map is keyed by the identity each caller asked under, and a
// second registration of one standing pair aliases the one request it read back,
// so the map can name one approval identity twice. Presence is a set of
// questions, so the read reports each approval identity once: a repeated
// registration widens the alias, never the waiting set, and two requests stay two
// entries because their identities differ.
func (a *ApprovalManager) Pending() []*ApprovalRequest {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ctx := context.Background()
	facts := a.factsLocked(ctx)
	now := a.now().UnixMilli()
	seen := make(map[string]struct{}, len(a.evidence))
	out := make([]*ApprovalRequest, 0, len(a.evidence))
	for _, held := range a.evidence {
		if _, reported := seen[held.ID]; reported {
			continue
		}
		if held.ExpiresMs > 0 && now >= held.ExpiresMs {
			continue
		}
		if !facts.present(held.ToolName, held.ArgsDigest, held.ID) {
			continue
		}
		seen[held.ID] = struct{}{}
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

// foldFiledRuling answers the ruling one identity carries on the durable face after
// a ruling write this book could not confirm, and folds that answer back into the
// chain, so the presence and the ruling every later read takes is the fact the
// store kept.
//
// - the read is the identity's own answer key: a write that landed behind a failed barrier is read back by the question it was filed on
// - a face holding no answer under that key states the write never landed, which leaves the request owed exactly as it stood
// - a face that cannot be read, and a landed answer that names no ruling, are refused by name instead: neither is "no ruling"
func (a *ApprovalManager) foldFiledRuling(ctx context.Context, log durable.FactLog, req *ApprovalRequest) (bool, ApprovalStatus, error) {
	if log == nil {
		return false, "", nil
	}
	answer, answered, err := answerOnFace(ctx, log, req.ToolName, req.ArgsDigest, req.ID)
	if err != nil {
		return false, "", err
	}
	if !answered {
		return false, "", nil
	}
	ruling, known := rulingIn(answer)
	if !known {
		return false, "", fmt.Errorf("%w: %s states no ruling", errApprovalFactConflict, answer.Key)
	}
	if err := a.chain.bindFacts([]event.Event{answer}, log); err != nil {
		return false, "", err
	}
	return true, ruling, nil
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

// recordRequestLocked writes the request record of one caller identity to the
// durable face, so the identity an approver was handed outlives a process whose
// cut states no approval. The caller holds the book's write lock. A book beside
// no durable face answers nil, and a store that refused the write is reported to
// the registration beside the hold that stands either way.
func (a *ApprovalManager) recordRequestLocked(ctx context.Context, asked string, req ApprovalRequest) error {
	log := a.factLog
	if log == nil {
		return nil
	}
	return a.storeRequestRecord(ctx, log, asked, req)
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
//
// - this is the row of request files, not the boot's reader: a book the framework state slice seats reads the stated state at boot and keeps this read for the runtime rescan
// - a seated book takes its holds from the stated state alone: a file whose question the chain does not hold states no hold of that book, so it is neither seated nor filed, and only the ruling it carries lands
// - a book that reached the chain seats the obligations still open and leaves a ruled request on the chain it is read back through, so a rescan costs the waits still owed
// - a rescan that cannot read its directory leaves the standing book in place, and the wait it did not see is reported on the next read
func (a *ApprovalManager) rebuild(ctx context.Context) error {
	if a == nil || a.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return fmt.Errorf("approval: read the approval request files under %s: %w", a.dir, err)
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
			continue
		}
		restored := req
		facts := a.factsLocked(ctx)
		_, asked := facts.question(req.ToolName, req.ArgsDigest, req.ID)
		_, answered := facts.answer(req.ToolName, req.ArgsDigest, req.ID)
		settled := req.Status == ApprovalApproved || req.Status == ApprovalDenied
		if a.seated {
			if asked && !answered && settled {
				_ = a.fileAnswer(ctx, &restored)
				_ = os.Remove(path)
			}
			continue
		}
		if a.factLog != nil && asked && answered {
			continue
		}
		a.evidence[req.ID] = &restored
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
	return nil
}

// truncateArgs bounds the argument preview an approver reads.
func truncateArgs(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
