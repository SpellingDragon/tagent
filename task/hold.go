package task

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
//
// Hold is the waiting state one approval decision leaves behind: the call is
// registered as a task that executed nothing and the caller receives a waiting
// receipt. The state is a wait, so a query observes it as waiting and reads it as
// no final answer.
type Hold struct {
	// TaskID is the task registered beside the wait; the task holds no execution.
	TaskID event.TaskID
	// Reason is the readable reason the approval is waiting.
	Reason string
	// ApprovalID is the approval ticket identity this hold was issued under:
	// tagent issues it when the host parks the call, and it is globally unique.
	// A host response resolves the hold against that identity, never against a
	// native CallID that is not globally unique.
	ApprovalID string
	// ExpectedDigest is the digest of the call arguments this hold was issued
	// over, computed by DigestArgs. A response whose digest names other
	// arguments is refused beside ErrApprovalDigestMismatch instead of
	// approving a changed call.
	ExpectedDigest string
}

// Denied is the final refusal of one call: the call executed nothing and the
// refusal names the tool it turns away. The refusal is a terminal fact of its
// own, and the named paired result beside it carries the corrective material the
// model reads.
type Denied struct {
	// Tool is the name of the tool the refusal names.
	Tool string
	// Reason is the readable reason the approval refused the call.
	Reason string
}

// PairedResult returns the named paired result of one refusal: the result record
// naming the refused tool and pairing with the native call, so the refusal
// reaches the model as corrective material beside the native error response.
func (d Denied) PairedResult(callID string) Result {
	paired := d
	return Result{CallID: callID, Tool: d.Tool, Denied: &paired}
}

// HoldReceipt is the waiting receipt of one call parked in the approval channel.
// It answers the call with its wait and nothing more: the task it names executed
// nothing, and the receipt is read as no final refusal.
type HoldReceipt struct {
	// CallID is the native provider call identifier the receipt pairs with.
	CallID string
	// Hold is the waiting state the call was parked in.
	Hold Hold
}

// RulingKind is the class one approval decision falls in.
type RulingKind string

const (
	// RulingRun is a ruling that admits the call for execution.
	RulingRun RulingKind = "run"
	// RulingHold is a ruling that parks the call in the approval wait.
	RulingHold RulingKind = "hold"
	// RulingDeny is a ruling that refuses the call before any execution.
	RulingDeny RulingKind = "deny"
)

// Ruling is the approval decision over one call: the class it falls in beside the
// reason the approver reports.
type Ruling struct {
	// Kind is the class the decision falls in.
	Kind RulingKind
	// Reason is the readable reason the approver reports beside the decision.
	Reason string
}

// Approver reports the approval decision one call is admitted under, judging the
// tool name and the native arguments the call carries. An approver reporting an
// error refuses the call closed rather than guessing a decision.
type Approver func(ctx context.Context, tool string, jsonArgs []byte) (Ruling, error)

// ErrDenied reports a call the approval channel refused before execution. The
// refusal pairs with the named result of the call beside this native error.
var ErrDenied = errors.New("task: the call was refused before execution")

// ErrRulingUnknown reports an approval decision whose class is none this channel
// knows, so the call is refused closed rather than admitted under a guess.
var ErrRulingUnknown = errors.New("task: the approval decision names no known class")

// ErrApprovalIdentityMismatch reports an approval response that does not name
// the approval identity the hold was issued under. The response is refused by
// name and never applied to another call or to another hold.
var ErrApprovalIdentityMismatch = errors.New("task: the approval response names another approval identity")

// ErrApprovalDigestMismatch reports an approval response whose argument digest
// names other arguments than the ones that were held. The response is refused
// by name and never silently applied to the changed arguments.
var ErrApprovalDigestMismatch = errors.New("task: the approval response names other arguments than the held ones")

// NewApprovalID issues the approval identity of one hold: "approval-" beside the
// lowercase hex of 16 random bytes. tagent issues it when the host parks a call
// in a hold, so one held call is resolved against a globally unique identity
// rather than against a native CallID, which is not globally unique.
func NewApprovalID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("task: crypto/rand failed to issue an approval identity: " + err.Error())
	}
	return "approval-" + hex.EncodeToString(raw[:])
}

// DigestArgs computes the argument digest of one call: the lowercase hex of the
// SHA-256 of the exact argument bytes the call carries. The digest binds an
// approval to the arguments that were held, so a response over other arguments
// is refused instead of approving a changed call.
func DigestArgs(jsonArgs []byte) string {
	sum := sha256.Sum256(jsonArgs)
	return hex.EncodeToString(sum[:])
}

// ApprovalAnswer is the ruling one approval response carries: the approval
// identity and argument digest the response is bound to, beside whether the
// held call proceeds and the reason the host gave.
type ApprovalAnswer struct {
	// ApprovalID is the approval identity of the hold the response resolves.
	ApprovalID string
	// ExpectedDigest is the argument digest the response is bound to: the digest
	// of the arguments that were held.
	ExpectedDigest string
	// Granted reports whether the held call proceeds; false denies it.
	Granted bool
	// Reason is the host's stated ground for the ruling.
	Reason string
}

// HoldBook is the waiting table of the approval channel: it records the hold of
// one parked task beside the first approval response that hold received, and
// answers a query with that hold while the wait is open. The book consumes one
// response per hold and observes waits otherwise, so a query resolves and
// finalizes nothing.
type HoldBook struct {
	mu      sync.Mutex
	holds   map[event.TaskID]Hold
	answers map[string]ApprovalAnswer
}

// NewHoldBook returns an empty waiting table of the approval channel.
func NewHoldBook() *HoldBook {
	return &HoldBook{holds: map[event.TaskID]Hold{}, answers: map[string]ApprovalAnswer{}}
}

// Record stores the waiting state of one parked task. A repeated record of the
// same task keeps the waiting state already stored.
func (b *HoldBook) Record(hold Hold) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.holds[hold.TaskID]; ok {
		return
	}
	b.holds[hold.TaskID] = hold
}

// Waiting reports the waiting state of one parked task and whether the task is
// held at all. The query reports the open wait and moves nothing.
func (b *HoldBook) Waiting(taskID event.TaskID) (Hold, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	hold, ok := b.holds[taskID]
	return hold, ok
}

// Resolve consumes one approval response for the hold of the named task and
// reports the ruling that took effect. The response is bound to the hold it
// resolves: answer.ApprovalID must name the approval identity issued when that
// call was parked, and answer.ExpectedDigest must match the digest of the
// arguments that were held. A response that matches neither is refused beside
// ErrApprovalIdentityMismatch or ErrApprovalDigestMismatch and is never applied
// to another call or to the same call over changed arguments. A repeated
// response under the same approval identity is idempotent: the first response
// takes effect and the repeat reports that first ruling without consuming a
// second one.
func (b *HoldBook) Resolve(taskID event.TaskID, answer ApprovalAnswer) (ApprovalAnswer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	hold, ok := b.holds[taskID]
	if !ok {
		return ApprovalAnswer{}, fmt.Errorf("%w: no hold was issued for %s", ErrApprovalIdentityMismatch, taskID)
	}
	if answer.ApprovalID != hold.ApprovalID {
		return ApprovalAnswer{}, fmt.Errorf("%w: %s", ErrApprovalIdentityMismatch, answer.ApprovalID)
	}
	if answer.ExpectedDigest != hold.ExpectedDigest {
		return ApprovalAnswer{}, fmt.Errorf("%w: %s", ErrApprovalDigestMismatch, answer.ApprovalID)
	}
	if prior, ok := b.answers[hold.ApprovalID]; ok {
		return prior, nil
	}
	b.answers[hold.ApprovalID] = answer
	return answer, nil
}
