// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// The compression consumer's state slice: the judgments a producer filed and the
// request identities a product answers, both of which the consumer derives as it
// observes the chain. The cut states that account beside the projections it
// serves, so a boot answers every judgment the covered facts filed without
// reading one of those facts back, and the judgments the cut left owed stay owed.
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
// every session chain in session order, and the request identities a product
// answers in identity order, so the same account renders the same bytes.
type compressSliceWire struct {
	Sessions []compressSliceSession `json:"sessions"`
	Answered []event.EventID        `json:"answered"`
}

// compressSliceSession is the judgments one session chain filed, in the order
// they were filed: the order the consumer executes an owed batch in.
type compressSliceSession struct {
	Session   string                  `json:"session"`
	Judgments []compressSliceJudgment `json:"judgments"`
}

// compressSliceJudgment is one judgment as the carrier states it: the identity
// the judgment is quoted by, the reason the request fact carries and the moment
// it was filed on the millisecond axis.
type compressSliceJudgment struct {
	ID     event.EventID `json:"id"`
	Reason string        `json:"reason"`
	At     int64         `json:"at"`
}

// StateName reports the name the compression slice is keyed by.
func (s compressStateSlice) StateName() string { return checkpointSliceCompress }

// SnapshotState states the judgments and the answers the consumer of this run
// holds.
func (s compressStateSlice) SnapshotState(context.Context) ([]byte, error) {
	return s.run.compressSlice()
}

// RestoreState puts the stated judgments and answers back into the consumer of
// this run.
func (s compressStateSlice) RestoreState(_ context.Context, payload []byte) error {
	return s.run.restoreCompressSlice(payload)
}

// compressSlice states the account the compression consumer of this run holds:
// the judgments of every session chain in session order and the identities its
// products answer in identity order. The read is the consumer's own derived
// state — it flushes nothing and commits nothing, because the facts that account
// derives from are already on the chain.
func (r *Run) compressSlice() ([]byte, error) {
	r.mu.Lock()
	consumer := r.compress
	r.mu.Unlock()
	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	wire := compressSliceWire{
		Sessions: make([]compressSliceSession, 0, len(consumer.judgments)),
		Answered: make([]event.EventID, 0, len(consumer.answered)),
	}
	for session, filed := range consumer.judgments {
		judgments := make([]compressSliceJudgment, 0, len(filed))
		for _, request := range filed {
			judgments = append(judgments, compressSliceJudgment{ID: request.ID, Reason: request.Reason, At: request.At.UnixMilli()})
		}
		wire.Sessions = append(wire.Sessions, compressSliceSession{Session: string(session), Judgments: judgments})
	}
	sort.Slice(wire.Sessions, func(i, j int) bool { return wire.Sessions[i].Session < wire.Sessions[j].Session })
	for request := range consumer.answered {
		wire.Answered = append(wire.Answered, request)
	}
	sort.Slice(wire.Answered, func(i, j int) bool { return wire.Answered[i] < wire.Answered[j] })
	return json.Marshal(wire)
}

// restoreCompressSlice puts the judgments and answers the carrier states back
// into the consumer of this run. The facts the boot replays afterwards land on
// this account and never beside it: a request recorded twice is one judgment, a
// product noted twice is one answer, and a judgment the carrier leaves unanswered
// is the execution the chain still owes.
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
			filed = append(filed, event.CompressRequest{ID: judgment.ID, Reason: judgment.Reason, At: time.UnixMilli(judgment.At)})
		}
		judgments[event.SessionID(session.Session)] = filed
	}
	answered := make(map[event.EventID]struct{}, len(wire.Answered))
	for _, request := range wire.Answered {
		answered[request] = struct{}{}
	}
	r.mu.Lock()
	consumer := r.compress
	r.mu.Unlock()
	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	consumer.judgments = judgments
	consumer.answered = answered
	return nil
}
