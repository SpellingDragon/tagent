package specexample

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent/tool/spec"
)

// filedPlan is the canonical backend: it answers the plan operations from one fixed
// workspace image, reports a refused operation as a Result carrying the hint that names the
// next step, and reports an operation it cannot dispatch as a named error.
type filedPlan struct {
	tasksChecked bool
}

// Run answers one operation from the fixed workspace image.
func (b *filedPlan) Run(_ context.Context, req spec.Request) (spec.Result, error) {
	if !spec.Valid(req.Op) {
		return spec.Result{}, fmt.Errorf("%w: %q", spec.ErrUnknownOp, req.Op)
	}
	if req.Op != spec.OpArchive || req.Name == "" {
		return spec.Result{Op: req.Op, OK: true, Output: req.Name + ": ready"}, nil
	}
	if !b.tasksChecked {
		return spec.Result{
			Op:       req.Op,
			ExitCode: 1,
			Output:   "1 incomplete task(s) found",
			Hint:     "check every task before archiving",
		}, nil
	}
	return spec.Result{Op: req.Op, OK: true, Output: "archived as 2026-01-01-" + req.Name}, nil
}

// Name identifies the canonical backend.
func (b *filedPlan) Name() string { return "filed-plan" }

var _ spec.Backend = (*filedPlan)(nil)

// ExampleBackend drives the canonical backend through the distinction the port is built
// on: a refused archive is a Result carrying its hint, and an operation outside the closed
// set is a named error that reaches no backend work.
//
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func ExampleBackend() {
	ctx := context.Background()
	backend := &filedPlan{}
	refused, err := backend.Run(ctx, spec.Request{Op: spec.OpArchive, Name: "wire-plan-tool"})
	if err != nil || refused.OK || refused.Hint == "" {
		return
	}
	backend.tasksChecked = true
	archived, err := backend.Run(ctx, spec.Request{Op: spec.OpArchive, Name: "wire-plan-tool"})
	if err != nil || !archived.OK {
		return
	}
	if _, err := backend.Run(ctx, spec.Request{Op: spec.Op("rm -rf /")}); err == nil {
		return
	}
}
