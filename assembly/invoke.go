package assembly

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Invoke drives one invocation through the persistent event loop: the input is
// aggregated into the range's own transcript, every decision borrows the
// binding generation effective at its own start, and the settled tool
// notifications backflow to this invocation to drive its next decision. The
// turn reports the decisions it ran and the terminal the latest one reduced to;
// two invocations of one session are never batched together.
//
// The composition root establishes the session site of the explicit address on
// this instance and grants the diagnostic read face of the run a read of it, so
// the site is owned and read back from the state owner rather than mirrored
// beside it. Every input message commits as an entry fact of the range before
// the drive begins, so the turn-start boundary is one committed fact the
// observation reads back.
// The redline list (3 rules):
// MUST aggregate the input into the range's own transcript and never batch two invocations of one session.
// MUST borrow the binding generation effective at each decision's own start.
// MUST NOT let an admission receipt stand for the result of the call.
func (r *Run) Invoke(ctx context.Context, req InvokeRequest) (agent.Turn, error) {
	if req.InvocationID == "" {
		return agent.Turn{}, errors.New("tagent: the invocation carries no identity")
	}
	if err := r.establish(ctx, req.Address); err != nil {
		return agent.Turn{}, err
	}
	ready := make([]event.Event, 0, len(req.Input))
	now := time.Now()
	for _, msg := range req.Input {
		evt := r.inputEventOf(req, msg, req.InvocationID, now)
		key, err := r.commitFactChecked(ctx, evt)
		if err != nil {
			return agent.Turn{}, fmt.Errorf("tagent: commit the input fact: %w", err)
		}
		evt.Key = key
		ready = append(ready, evt)
	}
	inv := agent.Invocation{
		InvocationID: req.InvocationID,
		Address:      req.Address,
		Instance:     r.instance,
		Ready:        ready,
	}
	return r.loop.Drive(ctx, inv)
}

// Close closes the runtime's own side for the named session address and drains
// what it already holds, exactly as the close face of the execution domain
// prescribes: new input is refused before the first settlement pass, the
// obligations already accepted settle only by real settlement beside the real
// stop confirmations of the producers behind them, and a drain that did not
// converge is reported beside the named reason of everything that stayed open.
// The session side closes only after that settlement, so the mailbox
// bookkeeping an in-flight tool result still lands through stays open exactly
// as long as the drain may need it. A repeated close returns the first result
// and runs no second settlement.
// The redline list (3 rules):
// MUST refuse new input before the first settlement pass.
// MUST report a drain only beside real settlement and the real stop confirmations behind it.
// MUST NOT read a cancelled request, an emptied queue or an elapsed timeout as settlement.
func (r *Run) Close(ctx context.Context, req agent.CloseRequest) (agent.DrainReport, error) {
	r.mu.Lock()
	r.closing = true
	r.mu.Unlock()
	if r.retrieval != nil {
		defer r.retrieval.close()
	}
	settled, own := r.settleOwed(ctx, req.Deadline)
	report, err := r.shutdown.Close(ctx, req)
	report.Drained = report.Drained && settled
	report.Unsettled = append(report.Unsettled, own...)
	sort.Slice(report.Unsettled, func(i, j int) bool {
		if report.Unsettled[i].TaskID != report.Unsettled[j].TaskID {
			return report.Unsettled[i].TaskID < report.Unsettled[j].TaskID
		}
		return report.Unsettled[i].Reason < report.Unsettled[j].Reason
	})
	if req.Address.IsComplete() {
		if closeErr := r.store.CloseSession(ctx, req.Address); closeErr != nil && !errors.Is(closeErr, state.ErrUnknownTarget) {
			return agent.DrainReport{}, closeErr
		}
	}
	return report, err
}

// refusing reports whether the close of this run has begun, so a new call is
// turned away at the admission gate before any obligation opens beside the
// drain.
func (r *Run) refusing() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closing
}

// settleOwed offers the notifications the tasks of this run still owe until
// every accepted obligation reached real settlement or the deadline arrived:
// a flush only offers them beside their frozen routing, and only an explicit
// acceptance clears one. It reads and settles through the task face alone and
// keeps no bookkeeping of its own.
func (r *Run) settleOwed(ctx context.Context, deadline time.Time) (bool, []agent.UnsettledObligation) {
	for {
		settled := true
		var unsettled []agent.UnsettledObligation
		for _, id := range r.commit.TaskIDs() {
			_ = r.manager.Flush(ctx, id)
			open, err := r.manager.Obligations(ctx, id)
			if err != nil {
				settled = false
				unsettled = append(unsettled, agent.UnsettledObligation{TaskID: id, Reason: err.Error()})
				continue
			}
			for _, obligation := range open.Open {
				settled = false
				unsettled = append(unsettled, agent.UnsettledObligation{TaskID: id, Reason: string(obligation.Kind)})
			}
		}
		if settled {
			return true, nil
		}
		if (!deadline.IsZero() && !time.Now().Before(deadline)) || ctx.Err() != nil {
			return false, unsettled
		}
		select {
		case <-ctx.Done():
			return false, unsettled
		case <-time.After(agentShutdownPollInterval):
		}
	}
}

// typeOf reports the frozen event type one native input message renders as.
func typeOf(msg model.Message) string {
	switch msg.Role {
	case model.RoleAssistant:
		if len(msg.ToolCalls) > 0 {
			return event.TypeThinkingPlan
		}
		return event.TypeAgentOutput
	case model.RoleTool:
		return event.TypeActionCommand
	default:
		return event.TypeExternalInput
	}
}

// inputEventOf renders one native input message as the entry fact of a range:
// the frozen type of the message beside the address, identity and instance the
// range stamps on it, and the trigger-source stamp of the material when the
// caller names one. The identity is the range the message is input of, so a
// message admitted beside a range already running commits under that range and
// is read at that drive's next decision boundary.
func (r *Run) inputEventOf(req InvokeRequest, msg model.Message, invocationID event.InvocationID, now time.Time) event.Event {
	evt := event.Event{
		Type:         typeOf(msg),
		Timestamp:    now,
		Message:      msg,
		Owner:        req.Address,
		Source:       req.Address,
		Target:       req.Address,
		InvocationID: invocationID,
		InstanceID:   r.instance,
		EpisodeID:    req.Episode,
	}
	if req.TriggerSource != "" {
		evt.Meta = map[string]string{event.MetaKeyTriggerSource: req.TriggerSource}
	}
	return evt
}
