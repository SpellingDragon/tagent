// Package rlexample shows how the capture scope binds one decision view.
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
package rlexample

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/rl"
)

// trajectScope is the canonical decision-bound capture: the scope hands the
// capture of one decision request view, and that capture observes only the
// attempts of the decision it was scoped to.
type trajectScope struct{}

func (trajectScope) Snapshot(context.Context, model.Request) (rl.ModelViewRef, error) {
	return rl.ModelViewRef{}, nil
}
func (trajectScope) Record(context.Context, rl.AttemptRecord) (rl.CaptureAdmission, error) {
	return rl.CaptureAdmission{}, nil
}
func (trajectScope) Seal(context.Context) (rl.CaptureManifest, error) {
	return rl.CaptureManifest{}, nil
}

// Scope returns the capture of one decision request view.
func (trajectScope) Scope(view rl.ModelViewRef, decisionID event.DecisionID) rl.Capture {
	return trajectScope{}
}

var _ rl.CaptureScope = trajectScope{}

// ExampleCaptureScope demonstrates scoping one decision view.
func ExampleCaptureScope() {
	var scope rl.CaptureScope = trajectScope{}
	_ = scope.Scope(rl.ModelViewRef{}, event.DecisionID("d-1"))
	fmt.Println("scoped")
}
