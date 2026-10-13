package rl

import (
	"context"

	"github.com/SpellingDragon/tagent/model"
)

// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
//
// Capture is the request-boundary observation port through which one model
// request is remembered: it fixes the view a request actually runs on, admits the
// attempts observed for it under a bound, and seals what it holds with a manifest.
//
// The port holds four invariants:
// - the view is fixed before the request starts;
// - a header, cookie or URL credential never reaches a persisted record;
// - admission is bounded and never waits on the caller;
// - a capture without a writer seal is partial, never complete.
type Capture interface {
	// Snapshot fixes the model, the generation parameters and the visible prompt,
	// skill and tool set of req before the request starts, and returns the
	// reference a decision records. A view is never reconstructed from the options
	// of a finished call.
	Snapshot(ctx context.Context, req model.Request) (ModelViewRef, error)

	// Record admits one observed attempt and reports the admission bound that
	// refused it when it is not admitted.
	Record(ctx context.Context, attempt AttemptRecord) (CaptureAdmission, error)

	// Seal closes the capture and returns the manifest a real writer confirmed. A
	// capture that still holds records in flight, or that no writer sealed, comes
	// back with Partial set.
	Seal(ctx context.Context) (CaptureManifest, error)
}

// CaptureBound names the admission bound that refused a record.
type CaptureBound string

const (
	// BoundNone marks an admitted record: no bound refused it.
	BoundNone CaptureBound = ""
	// BoundRecordSize marks a record that exceeded the per-record byte bound.
	BoundRecordSize CaptureBound = "record_size"
	// BoundInFlight marks a record refused because the in-flight count bound was
	// reached.
	BoundInFlight CaptureBound = "in_flight"
	// BoundBytes marks a record refused because the run byte budget was reached.
	BoundBytes CaptureBound = "bytes"
	// BoundClosed marks a record refused because the capture was already closed.
	BoundClosed CaptureBound = "closed"
)

// CaptureAdmission states whether a capture admitted one record, and which bound
// refused it when it did not.
//
// An unadmitted record is a counted loss and not a silent one: the bound that
// refused it stays readable on the admission.
type CaptureAdmission struct {
	// Admitted reports whether the record was admitted.
	Admitted bool
	// Bound names the bound that refused the record; it is BoundNone for an
	// admitted record.
	Bound CaptureBound
	// Reason states why the bound refused the record.
	Reason string
}

// CaptureManifest is the seal of one capture: what it holds, whether a real writer
// confirmed it, and what it lost.
//
// A capture without a writer seal is partial. A partial capture is never reported
// as complete, and the reason says what was still in flight.
type CaptureManifest struct {
	// CaptureID identifies the capture the manifest seals.
	CaptureID string
	// Sealed reports whether a real writer confirmed the seal after the records
	// were flushed.
	Sealed bool
	// Partial reports a capture that closed with records in flight or without a
	// writer seal.
	Partial bool
	// Records counts the records the capture wrote.
	Records int64
	// Dropped counts the records the capture refused under an admission bound.
	Dropped int64
	// InFlight counts the records still unflushed when the capture closed.
	InFlight int64
	// Reasons lists why the capture is partial.
	Reasons []string
}
