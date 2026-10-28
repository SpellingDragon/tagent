// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
//
// The trajectory capture of a booted host: one account per assembled run. Boot
// opens the file writer under the storage tier's trajectory directory and one
// bounded sink over it, and the run fixes, admits and seals every turn through
// the scope below.
//
// The capture is one account however many turns it spans, while the execution
// loop seals the capture it holds at the end of every drive. The scope therefore
// keeps the loop's per-turn seal from closing the run-wide sink: a turn ends
// without ending the account, and Close seals the real capture once, after the
// run drained.
package assembly

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
)

// hostCaptureID is the identity the boot capture seals its manifest under.
const hostCaptureID = "host-trajectory"

// hostTrajectoriesDir is the directory under the storage root the attempt
// records are written to.
const hostTrajectoriesDir = "trajectories"

// openHostCapture opens the trajectory capture of one boot: the records land
// under <root>/trajectories, and admissions run through the sink's default
// bounds, so a capture never waits on the writer and never blocks a request.
func openHostCapture(root string) (*rl.TrajectoryCapture, error) {
	dir := filepath.Join(root, hostTrajectoriesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("tagent: create the trajectory directory %s: %w", dir, err)
	}
	writer, err := rl.NewFileCaptureWriter(dir, 0)
	if err != nil {
		return nil, fmt.Errorf("tagent: open the trajectory writer: %w", err)
	}
	sink, err := rl.NewCaptureSink(hostCaptureID, rl.SinkConfig{}, writer)
	if err != nil {
		return nil, fmt.Errorf("tagent: open the trajectory sink: %w", err)
	}
	capture, err := rl.NewTrajectoryCapture(hostCaptureID, sink, rl.SnapshotPolicy{})
	if err != nil {
		return nil, fmt.Errorf("tagent: open the trajectory capture: %w", err)
	}
	return capture, nil
}

// sealHostCapture seals one boot capture under the host close grace.
func sealHostCapture(capture *rl.TrajectoryCapture) {
	if capture == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostShutdownGrace)
	defer cancel()
	if _, err := capture.Seal(ctx); err != nil {
		assemblyWarn("tagent: the trajectory seal reported: %v", err)
	}
}

// hostRunCapture scopes one boot capture onto the capture face the execution
// loop is attached through. The trajectory itself owns snapshots, records and
// the run-wide seal; the wrapper keeps the loop's per-turn seal a no-op and the
// scoped captures it hands out do the same, so the sink stays open across every
// turn of the run.
type hostRunCapture struct {
	trajectory *rl.TrajectoryCapture
}

var _ rl.CaptureScope = (*hostRunCapture)(nil)

// Snapshot refuses a viewless snapshot: a request is fixed against the view of
// the decision that carries it, exactly as the trajectory declares.
func (c *hostRunCapture) Snapshot(ctx context.Context, request model.Request) (rl.ModelViewRef, error) {
	if c == nil || c.trajectory == nil {
		return rl.ModelViewRef{}, rl.ErrCaptureSinkMissing
	}
	return c.trajectory.Snapshot(ctx, request)
}

// Record admits one attempt through the run-wide sink.
func (c *hostRunCapture) Record(ctx context.Context, attempt rl.AttemptRecord) (rl.CaptureAdmission, error) {
	if c == nil || c.trajectory == nil {
		return rl.CaptureAdmission{}, rl.ErrCaptureSinkMissing
	}
	return c.trajectory.Record(ctx, attempt)
}

// Seal answers the per-turn seal without closing the run-wide account: the host
// seals the trajectory once its run drained.
func (c *hostRunCapture) Seal(context.Context) (rl.CaptureManifest, error) {
	return rl.CaptureManifest{}, nil
}

// Scope returns the capture of one decision's request view, under the same
// per-turn seal the run-wide account keeps open.
func (c *hostRunCapture) Scope(view rl.ModelViewRef, decisionID event.DecisionID) rl.Capture {
	if c == nil || c.trajectory == nil {
		return hostScopedCapture{}
	}
	return hostScopedCapture{inner: c.trajectory.Scope(view, decisionID)}
}

// Snapshots reports the safe request snapshots the run-wide trajectory fixed, in
// fix order, each beside the decision whose request it was fixed for. The read
// goes to the trajectory that holds them, so the run's snapshot read face and
// the capture's own account are one account rather than two.
func (c *hostRunCapture) Snapshots() []rl.DecisionSnapshot {
	if c == nil || c.trajectory == nil {
		return nil
	}
	return c.trajectory.Snapshots()
}

// hostScopedCapture keeps one scoped capture of the run-wide trajectory beside
// the per-turn seal the loop runs at the end of every drive.
type hostScopedCapture struct {
	inner rl.Capture
}

var _ rl.Capture = hostScopedCapture{}

// Snapshot fixes the request view of the decision this capture is scoped to.
func (s hostScopedCapture) Snapshot(ctx context.Context, request model.Request) (rl.ModelViewRef, error) {
	if s.inner == nil {
		return rl.ModelViewRef{}, rl.ErrCaptureSinkMissing
	}
	return s.inner.Snapshot(ctx, request)
}

// Record admits one attempt against the fixed view of this decision.
func (s hostScopedCapture) Record(ctx context.Context, attempt rl.AttemptRecord) (rl.CaptureAdmission, error) {
	if s.inner == nil {
		return rl.CaptureAdmission{}, rl.ErrCaptureSinkMissing
	}
	return s.inner.Record(ctx, attempt)
}

// Seal answers the per-turn seal without closing the run-wide account.
func (s hostScopedCapture) Seal(context.Context) (rl.CaptureManifest, error) {
	return rl.CaptureManifest{}, nil
}
