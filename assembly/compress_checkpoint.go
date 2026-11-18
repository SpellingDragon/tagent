// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// The compression consumer's state slice: the judgments a chain filed that no
// product answers yet. The cut states that batch beside the projections it
// serves, so a boot executes every judgment the covered facts filed without
// reading one of those facts back, and a judgment a product answers states
// nothing: its request, its answer and the folding it caused stay on the chain,
// which is what bounds the slice by the batch still owed instead of by the
// judgments the chain ever filed.
package assembly

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// compressStateSlice is the compression slice of one run: the account the
// consumer derives from the commits it observes, read from the consumer the run
// holds at the moment of the cut.
type compressStateSlice struct{ run *Run }

// compressSliceWire is the payload of the compression slice: the judgments of
// every session chain that no product answers yet, in session order, so the same
// account renders the same bytes. A chain that owes nothing is stated nowhere, so
// the payload is the batch still owed and never the judgments the chain executed.
type compressSliceWire struct {
	Sessions []compressSliceSession `json:"sessions"`
}

// compressSliceSession is the judgments one session chain still owes, in the order
// they were filed: the order the consumer executes an owed batch in.
type compressSliceSession struct {
	Session   string                  `json:"session"`
	Judgments []compressSliceJudgment `json:"judgments"`
}

// compressSliceJudgment is one owed judgment as the checkpoint states it: the
// identity the judgment is quoted by, the reason the request fact carries and the
// moment it was filed on the millisecond axis. The judgment is stated whole
// because it is still owed: a boot executes it without reading the request fact
// back. A judgment a product answers is stated nowhere, because its request and
// its answer stay on the chain and no boot owes it again.
type compressSliceJudgment struct {
	ID     event.EventID `json:"id"`
	Reason string        `json:"reason,omitempty"`
	At     int64         `json:"at,omitempty"`
}

// owedJudgments answers the judgments one session chain filed that no product
// answers yet, in filing order, as the checkpoint states them. The caller holds
// the consumer lock.
func owedJudgments(filed []event.CompressRequest, answered map[event.EventID]struct{}) []compressSliceJudgment {
	out := make([]compressSliceJudgment, 0, len(filed))
	for _, request := range filed {
		if _, done := answered[request.ID]; done {
			continue
		}
		out = append(out, compressSliceJudgment{ID: request.ID, Reason: request.Reason, At: request.At.UnixMilli()})
	}
	return out
}

// StateName reports the name the compression slice is keyed by.
func (s compressStateSlice) StateName() string { return checkpointSliceCompress }

// SnapshotState states the judgments and the answers the consumer of this run
// holds.
func (s compressStateSlice) SnapshotState(context.Context) ([]byte, error) {
	return s.run.compressSlice()
}

// SnapshotSessionState states the judgments one session chain filed beside the
// identities a product answers among them. A chain that filed no judgment answers
// nil: the account of a chain is the batch it owes a fold for, and a chain that
// owes none states nothing.
func (s compressStateSlice) SnapshotSessionState(_ context.Context, session event.SessionID) ([]byte, error) {
	return s.run.compressSliceOf(session)
}

// RestoreState puts the stated judgments and answers back into the consumer of
// this run.
func (s compressStateSlice) RestoreState(_ context.Context, payload []byte) error {
	return s.run.restoreCompressSlice(payload)
}

// compressSlice states the batch the compression consumer of this run still
// owes: the judgments of every session chain no product answers yet, in filing
// order. The read is the consumer's own derived state — it flushes nothing and
// commits nothing, because the facts that account derives from are already on the
// chain. A judgment a product answers is stated nowhere, because its request and
// its answer stay on the chain: the checkpoint costs the batch the consumer still
// owes beside nothing per judgment it already executed.
func (r *Run) compressSlice() ([]byte, error) {
	r.mu.Lock()
	consumer := r.compress
	r.mu.Unlock()
	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	sessions := make([]compressSliceSession, 0, len(consumer.judgments))
	for session, filed := range consumer.judgments {
		owed := owedJudgments(filed, consumer.answered)
		if len(owed) == 0 {
			continue
		}
		sessions = append(sessions, compressSliceSession{Session: string(session), Judgments: owed})
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Session < sessions[j].Session })
	return json.Marshal(compressSliceWire{Sessions: sessions})
}

// restoreCompressSlice puts the owed judgments the checkpoint states back into
// the consumer of this run. The facts the boot replays afterwards land on this
// account and never beside it: a request recorded twice is one judgment, and a
// product the replay reads notes the judgment it answers, so a judgment the
// checkpoint leaves owed is the execution the chain still owes.
//
// The restore merges: a checkpoint states the batch of the chains it covers, and a
// boot reads the framework's checkpoint beside the checkpoint of every chain, so each
// restore states the chain it carries and leaves the chains beside it standing.
//   - A judgment a product answered before the cut is restored by no checkpoint: the chain states its request and its answer, and no boot owes it again, so the restore seats the batch still owed and never the answers the chain already holds.
func (r *Run) restoreCompressSlice(payload []byte) error {
	var wire compressSliceWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return err
	}
	judgments := make(map[event.SessionID][]event.CompressRequest, len(wire.Sessions))
	for _, session := range wire.Sessions {
		if session.Session == "" {
			return fmt.Errorf("assembly: the compression slice states the judgments of no session chain")
		}
		filed := make([]event.CompressRequest, 0, len(session.Judgments))
		for _, judgment := range session.Judgments {
			if judgment.ID == "" {
				return fmt.Errorf("assembly: the compression slice states a judgment of session %s with no identity", session.Session)
			}
			request := event.CompressRequest{ID: judgment.ID, Reason: judgment.Reason}
			if judgment.At != 0 {
				request.At = time.UnixMilli(judgment.At)
			}
			filed = append(filed, request)
		}
		judgments[event.SessionID(session.Session)] = filed
	}
	r.mu.Lock()
	consumer := r.compress
	r.mu.Unlock()
	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	if consumer.judgments == nil {
		consumer.judgments = map[event.SessionID][]event.CompressRequest{}
	}
	for session, filed := range judgments {
		consumer.judgments[session] = filed
	}
	return nil
}

// compressSliceOf states the batch one session chain still owes: the judgments it
// filed that no product answers yet, in filing order. A judgment a product
// answers states nothing, because the request it was filed as and the answer it
// drew stay on the chain. A chain that owes no judgment states nothing, so the
// session checkpoint of a chain the fold already executed carries no compression
// account at all.
func (r *Run) compressSliceOf(session event.SessionID) ([]byte, error) {
	if session == "" {
		return nil, nil
	}
	r.mu.Lock()
	consumer := r.compress
	r.mu.Unlock()
	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	filed, held := consumer.judgments[session]
	if !held || len(filed) == 0 {
		return nil, nil
	}
	owed := owedJudgments(filed, consumer.answered)
	if len(owed) == 0 {
		return nil, nil
	}
	return json.Marshal(compressSliceWire{
		Sessions: []compressSliceSession{{Session: string(session), Judgments: owed}},
	})
}
