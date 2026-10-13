package acceptance

import (
	"context"
	"errors"
)

// errNotImplemented is the sentinel error returned by every skeleton stub. The
// phase-2 implementation replaces each stub with real behavior.
var errNotImplemented = errors.New("phase 2: not implemented")

// skeletonRunner is a compile-time placeholder for the phase-2 scenario runner.
// It carries the stub methods that satisfy Runner so the interface
// implementation relation is proven at compile time, and it holds no runner
// logic.
type skeletonRunner struct{}

// Load implements Runner.Load as a phase-2 stub.
func (skeletonRunner) Load(source string) ([]Scenario, error) {
	return nil, errNotImplemented
}

// Drive implements Runner.Drive as a phase-2 stub.
func (skeletonRunner) Drive(ctx context.Context, scenario Scenario) error {
	return errNotImplemented
}

// Derive implements Runner.Derive as a phase-2 stub.
func (skeletonRunner) Derive(scenario Scenario) ([]Observation, error) {
	return nil, errNotImplemented
}

// Assert implements Runner.Assert as a phase-2 stub.
func (skeletonRunner) Assert(scenario Scenario, observations []Observation) (Result, error) {
	return Result{}, errNotImplemented
}

// Compile-time assertion that skeletonRunner implements Runner.
var _ Runner = (*skeletonRunner)(nil)
