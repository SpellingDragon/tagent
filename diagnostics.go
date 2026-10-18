// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// The diagnostic visibility face of the host: the unified runtime reading beside
// the reflection, training and observation faces, each opened by the switch the
// host was started with. Every section reads the owner that already holds the
// fact — the query projection, the publication entry, the reflection anchor
// store, the recording loss account and the presentation sink — and keeps no
// second copy of the live scene.
//
// A face whose switch is off, or whose subsystem the assembly carries no wiring
// for, names that state in its reason instead of answering a zero reading that
// would pass for an idle subsystem.
package tagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
)

// DiagnosticSubsystem names one diagnostic face of the host beside the unified
// runtime map.
type DiagnosticSubsystem string

// SubsystemReflection names the reflection and self-evolution face.
const SubsystemReflection DiagnosticSubsystem = "reflection"

// SubsystemTraining names the training record face.
const SubsystemTraining DiagnosticSubsystem = "training"

// SubsystemTelemetry names the post-commit observation face.
const SubsystemTelemetry DiagnosticSubsystem = "telemetry"

// DiagnosticSwitchEnv names the environment variable the host reads the
// diagnostic switches from: a comma-separated list of subsystem names.
const DiagnosticSwitchEnv = "TAGENT_DIAGNOSTICS"

// ErrDiagnosticSwitchUnknown reports a diagnostic switch naming no subsystem of
// this host: the switch is refused by name instead of being dropped in silence,
// so a misspelt face never reads as a closed one.
var ErrDiagnosticSwitchUnknown = errors.New("tagent: the diagnostic switch names no subsystem of this host")

// reasonSwitchClosed, reasonFaceOpen and the named absence reasons are the
// readings one diagnostic face reports its state with.
const (
	reasonSwitchClosed   = "the diagnostic switch of this subsystem is off"
	reasonFaceOpen       = "the switch opened the face over its wired subsystem"
	reasonNoAnchors      = "the run carries no reflection anchor store"
	reasonNoRecorder     = "the run carries no training recorder"
	reasonNoObserver     = "the run carries no observation face"
	reasonWatermarkUnset = "the anchor store holds no watermark of a consumed reflection turn"
)

// DiagnosticSwitches names the diagnostic faces the host was started with.
//
// The unified runtime map carries no switch: it is the runtime's own read face
// and stays readable while every switch below is off. Each switch below opens
// one optional face over the subsystem the assembly actually wired.
type DiagnosticSwitches struct {
	// Reflection opens the reflection and self-evolution face.
	Reflection bool
	// Training opens the training record face.
	Training bool
	// Telemetry opens the post-commit observation face.
	Telemetry bool
}

// DiagnosticSwitchesFromEnv reads the diagnostic switches the host process was
// started with. The variable carries a comma-separated list of subsystem names;
// an empty variable opens no optional face, and a name outside the three
// subsystems is refused with ErrDiagnosticSwitchUnknown.
func DiagnosticSwitchesFromEnv() (DiagnosticSwitches, error) {
	var switches DiagnosticSwitches
	for _, token := range strings.Split(os.Getenv(DiagnosticSwitchEnv), ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		switch DiagnosticSubsystem(strings.ToLower(token)) {
		case SubsystemReflection:
			switches.Reflection = true
		case SubsystemTraining:
			switches.Training = true
		case SubsystemTelemetry:
			switches.Telemetry = true
		default:
			return DiagnosticSwitches{}, fmt.Errorf("%w: %q", ErrDiagnosticSwitchUnknown, token)
		}
	}
	return switches, nil
}

// DiagnosticFace reports the state of one optional diagnostic face: the
// subsystem it belongs to, whether its switch opened it, whether the assembly
// wired the subsystem behind it, and the named reason of the reading.
type DiagnosticFace struct {
	// Subsystem names the face this reading belongs to.
	Subsystem DiagnosticSubsystem
	// Open reports whether the face is open: its switch was on and the
	// assembly wired the subsystem behind it.
	Open bool
	// Available reports whether the assembly wired the subsystem behind the
	// face; a face beside no wiring stays closed whatever the switch says.
	Available bool
	// Reason names the state of the face instead of leaving a zero reading to
	// be mistaken for an idle subsystem.
	Reason string
}

// DiagnosticWatermark is the reflection watermark reading: the moment the last
// consumed reflection turn advanced it to, read from the anchor store that
// persists it.
type DiagnosticWatermark struct {
	// Wired reports whether the assembly carries an anchor store.
	Wired bool
	// Established reports whether the watermark was read from the store.
	Established bool
	// Moment is the watermark the store holds; the zero moment marks a store
	// with no consumed turn yet.
	Moment time.Time
	// Reason names an unestablished watermark instead of answering a fresh one.
	Reason string
}

// DiagnosticReflection is the reflection and self-evolution reading: the
// publication account of the run beside the reflection watermark of its anchor
// store.
type DiagnosticReflection struct {
	// Face is the switch and wiring reading of this subsystem.
	Face DiagnosticFace
	// Publication is the generation the sole publication entry currently holds.
	Publication state.Publication
	// Watermark is the reflection watermark reading.
	Watermark DiagnosticWatermark
}

// DiagnosticRecordingLoss is one training record the production points could
// not write: the record kind and identity beside the named failure.
type DiagnosticRecordingLoss struct {
	// Kind names the record kind the failed write belonged to.
	Kind string
	// ID is the identity of the record the failed write belonged to.
	ID string
	// Reason is the named failure that stopped the write.
	Reason string
}

// DiagnosticTraining is the training record reading: the recorder wiring, the
// request snapshots the run holds, the explicit recording loss account and,
// beside a mounted feedback face, the attribution of the submissions it took.
type DiagnosticTraining struct {
	// Face is the switch and wiring reading of this subsystem.
	Face DiagnosticFace
	// Recorder reports whether the assembly carries a training recorder.
	Recorder bool
	// Snapshots counts the request snapshots the run holds.
	Snapshots int
	// Losses lists the records the production points could not write.
	Losses []DiagnosticRecordingLoss
	// Feedback is the attribution of the mounted feedback face, and nil beside
	// no mounted face.
	Feedback *rl.FeedbackAttribution
}

// DiagnosticTelemetry is the post-commit observation reading: the observer
// wiring beside the presentations the run handed to its read face.
type DiagnosticTelemetry struct {
	// Face is the switch and wiring reading of this subsystem.
	Face DiagnosticFace
	// Observer reports whether the assembly carries an observation face.
	Observer bool
	// Presentations counts the After pass presentations the run took.
	Presentations int
	// Views counts the rendered views those presentations carry.
	Views int
	// Gaps counts the visible observation gaps those presentations carry.
	Gaps int
}

// DiagnosticRecovery is one restored task of the startup fold: the identity it
// was admitted under, the owner it resolved to, the status its chain folds to,
// whether it was rebuilt, and the named refusal beside a chain that rebuilt
// nothing.
type DiagnosticRecovery struct {
	// TaskID is the task the durable chain covers.
	TaskID event.TaskID
	// Owner is the owner the admission material resolved against.
	Owner event.AgentID
	// Status is the status the chain folds to.
	Status task.Status
	// Recovered reports whether the task was rebuilt.
	Recovered bool
	// Refusal names why a chain rebuilt nothing.
	Refusal string
}

// DiagnosticReconciliation is the delegation ledger reading the close lands
// admitted call terminals through.
type DiagnosticReconciliation struct {
	// Wired reports whether the run carries a delegation ledger attached to
	// its close gate.
	Wired bool
	// Booked counts the fresh admissions the ledger still lands.
	Booked int
}

// DiagnosticLifecycle is the host lifecycle reading: whether the close of this
// run has begun, the tasks the startup fold restored, and the reconciliation
// face the close settles delegated terminals through.
type DiagnosticLifecycle struct {
	// Closing reports whether the close of this run has begun.
	Closing bool
	// Recovered lists one entry per task the startup fold covered.
	Recovered []DiagnosticRecovery
	// Reconciliation is the delegation ledger reading.
	Reconciliation DiagnosticReconciliation
}

// DiagnosticReport is one diagnostic reading of the host: the unified runtime
// map under the caller's scope beside the optional faces the switches opened
// and the lifecycle reading of the run.
type DiagnosticReport struct {
	// Instance is the identity of the assembled runtime this reading covers.
	Instance event.InstanceID
	// Switches are the diagnostic switches the reading was taken with.
	Switches DiagnosticSwitches
	// Query is the unified runtime map: the projection reading every section
	// above is derived from, opened whatever the switches say.
	Query query.Snapshot
	// Reflection is the reflection and self-evolution reading.
	Reflection DiagnosticReflection
	// Training is the training record reading.
	Training DiagnosticTraining
	// Telemetry is the post-commit observation reading.
	Telemetry DiagnosticTelemetry
	// Lifecycle is the host lifecycle reading.
	Lifecycle DiagnosticLifecycle
}

// Diagnostics reads every diagnostic face of this run under the caller's
// authorization scope: the unified runtime map beside the reflection, training,
// observation and lifecycle readings, each taken from the owner that already
// holds it. The read moves nothing and keeps no copy, so two reads of one
// unchanged scene answer the same report and a later admission shows up in the
// next read.
//
// The unified map carries no switch and stays readable while every optional
// face is closed. A face the assembly carries no wiring for names that state in
// its reason instead of failing the whole read.
func (r *Run) Diagnostics(ctx context.Context, filter query.ListFilter, switches DiagnosticSwitches) (DiagnosticReport, error) {
	snapshot, err := r.Query(ctx, filter)
	if err != nil {
		return DiagnosticReport{}, err
	}
	return DiagnosticReport{
		Instance:   r.instance,
		Switches:   switches,
		Query:      snapshot,
		Reflection: r.reflectionFace(ctx, switches.Reflection),
		Training:   r.trainingFace(switches.Training),
		Telemetry:  r.telemetryFace(switches.Telemetry),
		Lifecycle:  r.lifecycleFace(),
	}, nil
}

// reflectionFace reads the publication account of the run beside the reflection
// watermark of its anchor store. A run beside no anchor store reports the
// wiring it lacks instead of a watermark of its own.
func (r *Run) reflectionFace(ctx context.Context, open bool) DiagnosticReflection {
	reading := DiagnosticReflection{
		Face:        diagnosticFace(SubsystemReflection, open, r.watermark != nil, reasonNoAnchors),
		Publication: r.publisher.Current(),
	}
	if r.watermark == nil {
		return reading
	}
	watermark, err := r.watermark.Current(ctx)
	if err != nil {
		reading.Watermark = DiagnosticWatermark{Wired: true, Reason: err.Error()}
		return reading
	}
	reading.Watermark = DiagnosticWatermark{Wired: true, Established: true, Moment: watermark.Moment}
	if watermark.Moment.IsZero() {
		reading.Watermark.Reason = reasonWatermarkUnset
	}
	return reading
}

// trainingFace reads the training record account of the run: the recorder
// wiring, the request snapshots, the recording losses the production points
// counted and the attribution of a mounted feedback face.
func (r *Run) trainingFace(open bool) DiagnosticTraining {
	losses := r.RecordingLosses()
	reading := DiagnosticTraining{
		Face:      diagnosticFace(SubsystemTraining, open, r.recorder != nil, reasonNoRecorder),
		Recorder:  r.recorder != nil,
		Snapshots: len(r.RequestSnapshots()),
		Losses:    make([]DiagnosticRecordingLoss, 0, len(losses)),
	}
	for _, loss := range losses {
		reason := ""
		if loss.Err != nil {
			reason = loss.Err.Error()
		}
		reading.Losses = append(reading.Losses, DiagnosticRecordingLoss{Kind: loss.Kind, ID: loss.ID, Reason: reason})
	}
	return reading
}

// telemetryFace reads the post-commit observation account of the run: the
// observer wiring beside the presentations the run took and the views and gaps
// they carry.
func (r *Run) telemetryFace(open bool) DiagnosticTelemetry {
	presented := r.Presentations()
	reading := DiagnosticTelemetry{
		Face:          diagnosticFace(SubsystemTelemetry, open, r.observer != nil, reasonNoObserver),
		Observer:      r.observer != nil,
		Presentations: len(presented),
	}
	for _, presentation := range presented {
		reading.Views += len(presentation.Views)
		reading.Gaps += len(presentation.Gaps)
	}
	return reading
}

// lifecycleFace reads the lifecycle account of the run: whether its close has
// begun, the tasks the startup fold restored and the delegation ledger attached
// to its close gate.
func (r *Run) lifecycleFace() DiagnosticLifecycle {
	recovery := r.Recovery()
	reading := DiagnosticLifecycle{
		Closing:   r.refusing(),
		Recovered: make([]DiagnosticRecovery, 0, len(recovery.Tasks)),
	}
	for _, outcome := range recovery.Tasks {
		entry := DiagnosticRecovery{
			TaskID:    outcome.TaskID,
			Owner:     outcome.Owner,
			Status:    outcome.Status,
			Recovered: outcome.Recovered,
		}
		if outcome.Refused != nil {
			entry.Refusal = outcome.Refused.Error()
		}
		reading.Recovered = append(reading.Recovered, entry)
	}
	reading.Reconciliation = DiagnosticReconciliation{Wired: r.delegations != nil}
	if r.delegations != nil {
		reading.Reconciliation.Booked = r.delegations.Pending()
	}
	return reading
}

// diagnosticFace reports one optional face: an unwired subsystem stays closed
// whatever the switch says, and a wired one reports whether its switch opened
// it.
func diagnosticFace(subsystem DiagnosticSubsystem, open, available bool, missing string) DiagnosticFace {
	face := DiagnosticFace{Subsystem: subsystem, Open: open && available, Available: available}
	switch {
	case !available:
		face.Reason = missing
	case !open:
		face.Reason = reasonSwitchClosed
	default:
		face.Reason = reasonFaceOpen
	}
	return face
}
