package execexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/tool/exec"
)

// errExitUnobserved is what the canonical executor reports for a stop it could not confirm.
var errExitUnobserved = errors.New("exec: the exit was not observed")

// leasedRun is the canonical executor: it holds one lease per resource it started, reports
// what a probe observed rather than what was requested, and keeps the lease of a resource
// whose stop it could not confirm.
type leasedRun struct {
	declared map[exec.ResourceKey]exec.ExecutionDecl
	held     map[exec.ResourceKey]bool
	output   map[exec.ResourceKey][]byte
}

// Start records decl and returns the canonical key of the resource it now holds.
func (r *leasedRun) Start(_ context.Context, decl exec.ExecutionDecl) (exec.ResourceKey, error) {
	if decl.Resource.Key == "" {
		return exec.ResourceKey{}, exec.ErrKeyMissing
	}
	if r.held == nil {
		r.held = map[exec.ResourceKey]bool{}
		r.declared = map[exec.ResourceKey]exec.ExecutionDecl{}
	}
	if r.held[decl.Resource] {
		return exec.ResourceKey{}, exec.ErrResourceHeld
	}
	r.held[decl.Resource] = true
	r.declared[decl.Resource] = decl
	return decl.Resource, nil
}

// Probe reports the state one probe observed for key; a state the probe could not determine
// comes back as exec.ProbeUnknown, never as death.
func (r *leasedRun) Probe(_ context.Context, key exec.ResourceKey) (exec.ProbeResult, error) {
	if !r.held[key] {
		return exec.ProbeResult{}, exec.ErrResourceUnknown
	}
	return exec.ProbeResult{State: exec.ProbeAlive, Detail: r.declared[key].Command}, nil
}

// Capture returns the output recorded for key while the execution ran.
func (r *leasedRun) Capture(_ context.Context, key exec.ResourceKey) ([]byte, error) {
	if !r.held[key] {
		return nil, exec.ErrResourceUnknown
	}
	if r.output == nil {
		r.output = map[exec.ResourceKey][]byte{}
	}
	return r.output[key], nil
}

// Stop returns the evidence of what actually stopped, and keeps the lease when the exit was
// not observed.
func (r *leasedRun) Stop(_ context.Context, key exec.ResourceKey) (exec.StopEvidence, error) {
	if !r.held[key] {
		return exec.StopEvidence{}, exec.ErrResourceUnknown
	}
	return exec.StopEvidence{Resource: key, Confirmed: false, Reason: errExitUnobserved.Error()}, errExitUnobserved
}

var _ exec.Executor = (*leasedRun)(nil)

// ExampleExecutor starts one external execution and then reports what it observed: the
// started resource is held under its canonical key, an unconfirmed exit keeps the lease
// instead of releasing it, and a second start under the same key is refused.
//
// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
func ExampleExecutor() {
	ctx := context.Background()
	runner := &leasedRun{}
	key, err := runner.Start(ctx, exec.ExecutionDecl{
		Command:  "sleep",
		Args:     []string{"30"},
		Resource: exec.ResourceKey{Kind: "tmux", Key: "run-1"},
	})
	if err != nil {
		return
	}
	probe, err := runner.Probe(ctx, key)
	if err != nil || probe.State != exec.ProbeAlive {
		return
	}
	if _, err := runner.Start(ctx, exec.ExecutionDecl{Command: "sleep", Resource: key}); err == nil {
		return
	}
	evidence, err := runner.Stop(ctx, key)
	if err == nil || evidence.Confirmed || evidence.Resource != key {
		return
	}
}
