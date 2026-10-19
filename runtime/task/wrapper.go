package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// 契约: docs/wiki/agent/task-lifecycle.md#spawn-dedup-origin
//
// CallFrame is the admission identity one wrapped call carries: the call identity
// a duplicate resolves against, the address the task belongs to, and the
// serialized execution the task runs under. The frame travels on the context, so
// a call is admitted under the identity of the decision that issued it.
//
// The frame is the one relationship the locked native tool port leaves implicit:
// `trpctool.CallableTool.Call(ctx, jsonArgs)` has no other channel for a per-call
// identity, so the frame rides the context and CallFrameFrom is its single typed
// accessor. Every reader below reads it through that accessor and never through
// a bare context value; a new reader states the key, the type, the accessor and
// the default in the change's ctx contract table before it lands.
type CallFrame struct {
	// Identity is the call identity a duplicate admission resolves against.
	Identity CallIdentity
	// Address is the address the task belongs to and its notifications target.
	Address event.Address
	// Execution names the serialized execution the task runs under.
	Execution Serialization
}

// callFrameKey is the context key one call frame is carried under.
type callFrameKey struct{}

// WithCallFrame carries one call frame on the context, so the wrapped call that
// context reaches is admitted under that identity. The dispatch states the frame
// of the decision that issues the call, and the manager states the frame it
// rebuilds from one admission's stored material on a delivery flush; those two
// are the only writers.
func WithCallFrame(ctx context.Context, frame CallFrame) context.Context {
	return context.WithValue(ctx, callFrameKey{}, frame)
}

// CallFrameFrom reports the call frame the context carries and whether one is
// set. It is this seam's single typed accessor: the admission wrapper, the run's
// fact/notice bookkeeping beside the skill tool of the composition root and the
// mailbox's responsibility range all read the frame here rather than through a
// bare context value. A call driven beside no frame reads not-ok, and every
// reader decides its own default — the wrapper refuses the call closed with
// ErrCallFrameMissing, the mailbox reports an empty responsibility range.
func CallFrameFrom(ctx context.Context) (CallFrame, bool) {
	frame, ok := ctx.Value(callFrameKey{}).(CallFrame)
	return frame, ok
}

// Result is the recorded outcome of one wrapped call: the tool output or the tool
// error beside the named paired result of a refusal. It is the body the background
// result backflow carries, and the call's own answer never carries it a second
// time.
type Result struct {
	// CallID is the native provider call identifier the result pairs with.
	CallID string
	// Tool is the name of the tool the result belongs to.
	Tool string
	// TaskID is the task the result settles; a refusal registers no task and
	// carries none here.
	TaskID event.TaskID
	// Output is the tool output the wrapped tool returned.
	Output any
	// Err is the tool error the wrapped tool returned.
	Err error
	// Denied is the named paired result of a refusal; it is nil for a result the
	// execution produced.
	Denied *Denied
}

// ResultRecorder records one result as a fact and returns the identity of that
// fact. The recorder is the injected face onto the event storage of the runtime,
// and the wrapper holds no store of its own.
type ResultRecorder func(ctx context.Context, result Result) (event.EventID, error)

// NoticeSink admits one notification into the mailbox of its target. A nil error
// states the acceptance of the target, the only answer that clears the delivery
// obligation of that notification.
type NoticeSink func(ctx context.Context, notice Notice) error

// ErrCallFrameMissing reports a wrapped call carrying no call frame, so it could
// be admitted under no identity.
var ErrCallFrameMissing = errors.New("task: the wrapped call carries no call frame")

// ErrNoResultRecorder reports a wrapped call that could not record its result,
// because no recorder is attached to the wrapper.
var ErrNoResultRecorder = errors.New("task: no result recorder is attached to this wrapper")

// ErrNoMailbox reports a wrapped call whose background result could not travel
// back, because no mailbox is attached to the wrapper.
var ErrNoMailbox = errors.New("task: no mailbox is attached to this wrapper")

// WrapOption configures a registration wrapper at construction.
type WrapOption func(*Wrapper)

// WithResultRecorder attaches the recorder a wrapped call records its result
// through. A wrapper with no recorder refuses a call closed before admission.
func WithResultRecorder(record ResultRecorder) WrapOption {
	return func(w *Wrapper) { w.record = record }
}

// WithMailbox attaches the mailbox write a background result travels through. A
// wrapper with no mailbox refuses a call closed before admission.
func WithMailbox(sink NoticeSink) WrapOption {
	return func(w *Wrapper) { w.mailbox = sink }
}

// WithApprover attaches the approval decision a wrapped call is admitted under. A
// wrapper with no approver admits every call for execution.
func WithApprover(approve Approver) WrapOption {
	return func(w *Wrapper) { w.approve = approve }
}

// Wrapper is the registration wrapper of one native tool: it admits each call
// through the task manager and answers that call with its single receipt, while
// the tool's own outcome returns later as an independent notification. It
// satisfies the native callable tool port of the locked framework.
type Wrapper struct {
	manager *MemoryManager
	inner   trpctool.CallableTool
	holds   *HoldBook
	record  ResultRecorder
	mailbox NoticeSink
	approve Approver
}

// The wrapper satisfies the native callable tool port.
var _ trpctool.CallableTool = (*Wrapper)(nil)

// Wrap returns the registration wrapper of one native tool over a task manager:
// each call is admitted through the manager and answered with its single receipt,
// and the outcome of the tool returns beside the admission as an independent
// notification.
func Wrap(mgr *MemoryManager, inner trpctool.CallableTool, opts ...WrapOption) *Wrapper {
	w := &Wrapper{manager: mgr, inner: inner, holds: NewHoldBook()}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Holds returns the waiting table the wrapper parks approval holds in.
func (w *Wrapper) Holds() *HoldBook {
	return w.holds
}

// Declaration returns the declaration of the wrapped tool unchanged.
func (w *Wrapper) Declaration() *trpctool.Declaration {
	return w.inner.Declaration()
}

// Call admits one native call and answers it with the single receipt of that
// admission. The receipt is the whole synchronous answer: the outcome of the
// execution returns later as an independent notification, a call the approval
// channel parks answers with its waiting receipt, and a call that cannot be
// admitted or recorded refuses once as a native tool error beside no result.
func (w *Wrapper) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	frame, ok := CallFrameFrom(ctx)
	if !ok {
		return nil, ErrCallFrameMissing
	}
	name := ""
	if declaration := w.inner.Declaration(); declaration != nil {
		name = declaration.Name
	}
	ruling, err := w.ruling(ctx, name, jsonArgs)
	if err != nil {
		return nil, err
	}
	spec := Spec{
		CallID:       frame.Identity.CallID,
		DedupKey:     frame.Identity.DedupKey(),
		InvocationID: frame.Identity.InvocationID,
		InstanceID:   frame.Identity.InstanceID,
		Description:  name,
		Source:       frame.Address,
		Target:       frame.Address,
		Execution:    frame.Execution,
	}
	switch ruling.Kind {
	case RulingRun:
		return w.run(ctx, frame, spec, name, jsonArgs)
	case RulingHold:
		return w.hold(ctx, spec, ruling, jsonArgs)
	case RulingDeny:
		return w.deny(ctx, frame, name, ruling)
	default:
		return nil, ErrRulingUnknown
	}
}

// ruling reports the approval decision one call is admitted under. A wrapper with
// no approver admits every call for execution.
func (w *Wrapper) ruling(ctx context.Context, name string, jsonArgs []byte) (Ruling, error) {
	if w.approve == nil {
		return Ruling{Kind: RulingRun}, nil
	}
	return w.approve(ctx, name, jsonArgs)
}

// run admits one call for execution and answers it with its single receipt: the
// tool runs beside the admission and its outcome returns as an independent
// notification. A repeated admission of the same identity executes nothing a
// second time.
func (w *Wrapper) run(ctx context.Context, frame CallFrame, spec Spec, name string, jsonArgs []byte) (any, error) {
	if w.record == nil {
		return nil, ErrNoResultRecorder
	}
	if w.mailbox == nil {
		return nil, ErrNoMailbox
	}
	receipt, err := w.manager.Admit(ctx, spec)
	if err != nil {
		return nil, err
	}
	if receipt.Kind == AdmissionDedup {
		return receipt, nil
	}
	go w.execute(context.WithoutCancel(ctx), frame, receipt.TaskID, name, jsonArgs)
	return receipt, nil
}

// hold parks one call in the approval wait: the call is registered as a task that
// executed nothing and the caller receives the waiting receipt. The wait is a
// state of its own, so the call answers with a receipt and no refusal. The hold
// is issued beside its approval identity and argument digest, so a later host
// response is bound to the arguments that were held, and the task reports the
// approval wait without occupying a decision slot.
func (w *Wrapper) hold(ctx context.Context, spec Spec, ruling Ruling, jsonArgs []byte) (any, error) {
	receipt, err := w.manager.Admit(ctx, spec)
	if err != nil {
		return nil, err
	}
	if receipt.Kind == AdmissionDedup {
		if existing, ok := w.holds.Waiting(receipt.TaskID); ok {
			return HoldReceipt{CallID: receipt.CallID, Hold: existing}, nil
		}
	}
	parked := Hold{
		TaskID:         receipt.TaskID,
		Reason:         ruling.Reason,
		ApprovalID:     NewApprovalID(),
		ExpectedDigest: DigestArgs(jsonArgs),
	}
	w.holds.Record(parked)
	result, err := w.manager.registry.Advance(ctx, receipt.TaskID, StatusAwaitingApproval,
		MutationID("await-approval:"+string(receipt.TaskID)), nil)
	if result != CommitCommitted {
		if err == nil {
			err = ErrCommitUncertain
		}
		return nil, err
	}
	return HoldReceipt{CallID: receipt.CallID, Hold: parked}, nil
}

// deny refuses one call before any execution and answers it with one native tool
// error. The refusal carries its named paired result through the recorder, so the
// model reads the refusal as corrective material beside the error and the result
// body never returns through the call itself.
func (w *Wrapper) deny(ctx context.Context, frame CallFrame, name string, ruling Ruling) (any, error) {
	if w.record == nil {
		return nil, ErrNoResultRecorder
	}
	refusal := Denied{Tool: name, Reason: ruling.Reason}
	if _, err := w.record(ctx, refusal.PairedResult(frame.Identity.CallID)); err != nil {
		return nil, errors.Join(ErrDenied, err)
	}
	return nil, fmt.Errorf("%w: %s: %s", ErrDenied, name, ruling.Reason)
}

// execute runs the wrapped tool beside the admission and moves its outcome back
// as an independent notification: the recorded fact settles the task beside its
// pending notification in one commit, and that notification enters the target
// mailbox. The outcome never returns through the call, so one call identity never
// carries a second synchronous response.
func (w *Wrapper) execute(ctx context.Context, frame CallFrame, taskID event.TaskID, name string, jsonArgs []byte) {
	output, callErr := w.inner.Call(ctx, jsonArgs)
	fact, recordErr := w.record(ctx, Result{
		CallID: frame.Identity.CallID,
		Tool:   name,
		TaskID: taskID,
		Output: output,
		Err:    callErr,
	})
	status, kind := StatusSettled, NoticeSettled
	if callErr != nil || recordErr != nil {
		status, kind = StatusFailed, NoticeFailed
	}
	if recordErr != nil {
		fact = ""
	}
	round := w.manager.registry.roundOf(taskID)
	notice := Notice{TaskID: taskID, Round: round, Kind: kind, Seq: 0, Event: fact}
	envelope, ok := w.manager.registry.envelopeOf(taskID, notice)
	if !ok {
		return
	}
	result, err := w.manager.registry.Advance(ctx, taskID, status,
		MutationID(fmt.Sprintf("settle:%s:%d", taskID, round)), []NoticeEnvelope{envelope})
	if err != nil || result != CommitCommitted {
		return
	}
	if err := w.mailbox(ctx, notice); err != nil {
		return
	}
	w.manager.acceptDelivery(notice)
}

// roundOf reports the execution round a task is currently in, or zero when the
// registry holds no record of that task.
func (r *MemoryRegistry) roundOf(taskID event.TaskID) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.tasks[taskID]; ok {
		return entry.round
	}
	return 0
}

// acceptDelivery clears the delivery obligation of one notification the target
// answered with acceptance, mirroring the accepted path of a flush.
func (m *MemoryManager) acceptDelivery(notice Notice) {
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()
	entry, ok := m.registry.tasks[notice.TaskID]
	if !ok {
		return
	}
	entry.dropBlocked(notice)
	open := entry.obligations[:0]
	for _, held := range entry.obligations {
		if held.obligation.Kind == ObligationAwaitingDelivery && held.obligation.Notice == notice {
			continue
		}
		open = append(open, held)
	}
	entry.obligations = open
}
