// 契约: docs/wiki/plugin/plugin-architecture.md#overview
//
// This file names the two duties one fact->observation face may carry and the one
// error a retained obligation is reported beside. An observer-only face reads the
// committed fact; a driver face is asked by the fact to start work somebody owes —
// a follow-up turn, a delivery — and the After pass answers the two failures
// differently, because only one of them leaves work owed. The two duties are the
// two failure semantics the observation port admits: a face declaring its Material
// states which one it lands, and a face declaring only its port is read from the
// marker below.
package plugin

import (
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/view"
)

// ErrObservationObligationRetained reports one post-commit observation that failed
// while the work it owed stays owed: the driver keeps that obligation in its own
// durable account, so the After pass reports the failure beside this error and
// never as a delivered observation. The cause is wrapped beside it, so a reader
// learns from one error what failed and what stayed owed.
var ErrObservationObligationRetained = errors.New("plugin: the observation failed with its obligation retained")

// driverObservation is the duty a fact->observation face declares when its reading
// carries an obligation: the observation starts work a later turn performs, and a
// failed observation leaves that work owed instead of merely missing. A face
// declaring no duty is observer-only — telemetry, logging and audit read the fact
// and owe nothing of it — and its failure is a gap beside the committed fact. The
// duty is the compatibility read of the failure semantics such a face declares:
// a face declaring Material() states the failure it lands, and a face declaring
// only its port answers the duty from itself at assembly, so a registration
// declares it once and the pass never infers one from a name.
//
// The redline list (3 rules):
// MUST be declared only by a face whose failed observation leaves work owed in a durable account of its own.
// MUST keep that obligation recoverable without the After pass: the pass retries nothing and clears nothing.
// MUST NOT be declared by a reading face: a telemetry, logging or audit observation owes no work.
type driverObservation interface {
	// DrivesObligation marks this face as one whose failed observation leaves work
	// owed. The method carries no answer of its own: the duty is the declaration,
	// and the account the obligation rests in is the face's.
	DrivesObligation()
}

// observedFace is one registered fact->observation face beside the material it
// declared, so one face set dispatches both duties and each failure is answered by
// the strategy that face stated.
type observedFace struct {
	served[PostCommitObserver]
	material Material
}

// dutyOfObserved reads the duty one registered element declared: an element
// carrying the driver marker drives an obligation, and every other element reads
// the fact as an observer only.
func dutyOfObserved(impl any) bool {
	_, drives := impl.(driverObservation)
	return drives
}

// gap reports the one failed observation of this face beside the committed fact it
// failed on, answering the failure semantics the face declared: a reading gap is
// the named gap of a reading that did not land, and a retained obligation is the
// same gap carrying the work the face still owes, so a reader tells owed work
// apart from a missing line. The two semantics are read from the declaration and
// never from the port the face serves.
func (f observedFace) gap(fact event.EventID, err error) view.ObservationGap {
	if f.material.Failure == PreserveObligation {
		err = fmt.Errorf("%w: %w", ErrObservationObligationRetained, err)
	}
	return view.ObservationGap{Plugin: f.name, Fact: fact, Err: err}
}
