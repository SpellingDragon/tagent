package rlexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
)

// boundedCapture is the canonical request-boundary observation port: it fixes the view a
// request runs on, admits the attempts observed for it under a bound and seals what it
// holds with a manifest.
type boundedCapture struct {
	view    rl.ModelViewRef
	bound   int
	records int
}

// Snapshot fixes the model, the generation parameters and the visible set before the
// request starts.
func (c *boundedCapture) Snapshot(_ context.Context, _ model.Request) (rl.ModelViewRef, error) {
	c.view = rl.ModelViewRef{
		Model:            "model-1",
		ModelVersion:     "2026-01",
		PromptVersion:    "prompt-v1",
		ParametersDigest: "params-1",
	}
	return c.view, nil
}

// Record admits one observed attempt and reports the bound that refused it.
func (c *boundedCapture) Record(_ context.Context, _ rl.AttemptRecord) (rl.CaptureAdmission, error) {
	if c.records >= c.bound {
		return rl.CaptureAdmission{Admitted: false, Bound: rl.BoundInFlight, Reason: "in-flight bound reached"}, nil
	}
	c.records++
	return rl.CaptureAdmission{Admitted: true, Bound: rl.BoundNone}, nil
}

// Seal closes the capture and returns the manifest a real writer confirmed.
func (c *boundedCapture) Seal(context.Context) (rl.CaptureManifest, error) {
	return rl.CaptureManifest{CaptureID: "capture-1", Sealed: true, Records: int64(c.records)}, nil
}

var _ rl.Capture = (*boundedCapture)(nil)

// ExampleCapture fixes one request view, admits an attempt up to the bound and reports that
// the attempt past the bound is refused by name rather than dropped silently.
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func ExampleCapture() {
	ctx := context.Background()
	capture := &boundedCapture{bound: 1}

	view, err := capture.Snapshot(ctx, model.Request{})
	if err != nil || view.Model == "" {
		return
	}
	attempt := rl.AttemptRecord{Schema: rl.SchemaCurrent, ID: event.AttemptID("attempt-1"), DecisionID: event.DecisionID("decision-1")}
	if admission, err := capture.Record(ctx, attempt); err != nil || !admission.Admitted {
		return
	}
	if admission, err := capture.Record(ctx, attempt); err != nil || admission.Bound != rl.BoundInFlight {
		return
	}
	manifest, err := capture.Seal(ctx)
	if err != nil || !manifest.Sealed || manifest.Records != 1 {
		return
	}
}
