package evolution

import (
	"context"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/state"
)

// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
//
// Skeleton compile-time assertions for the phase 1 evolution declarations.
//
// The phase 2 types that satisfy StructuralGate and Refine do not exist yet, so
// the placeholders below are the only implementation assertion the skeleton can
// write: the assertions pin each declared operation's exact shape, each declared
// field's name and type, and every declared constant's type.
var (
	_ func(StructuralGate, context.Context, state.OrgPlan, state.StatePlan) (GateVerdict, error) = StructuralGate.Eval

	_ func(StructuralGate, context.Context, state.OrgPlan, state.StatePlan) (GateVerdict, error) = StructuralGate.Benchmark

	_ func(StructuralGate, context.Context, state.OrgPlan, state.StatePlan) (GateVerdict, error) = StructuralGate.ApplyGate

	_ func(Refine, context.Context, []string, string) (RefineRecord, error) = Refine.Register

	_ func(Refine, context.Context) ([]RefineRecord, error) = Refine.Status

	_ func(Refine, context.Context, string) error = Refine.Rollback
)

// Every declared port name is an interface type.
var (
	_ StructuralGate = (StructuralGate)(nil)
	_ Refine         = (Refine)(nil)
)

// The declared record and window types keep every documented field.
var (
	_ = RefineRecord{
		Paths: []string{},
		Note:  "",
	}

	_ = EvidenceWindow{
		Version:     "",
		ActivatedAt: time.Time{},
	}
)

// The verdict vocabulary stays compile-time constants of its declared type.
const (
	_ GateVerdict = GateVerdictPass
	_ GateVerdict = GateVerdictReject
	_ GateVerdict = GateVerdictNotApplicable
	_ GateVerdict = GateVerdictInsufficientEvidence
)

// skeletonStructuralGate is a compile-time placeholder for the structural gate.
// It carries the stub segments that satisfy StructuralGate and holds no logic.
type skeletonStructuralGate struct{}

// Eval implements StructuralGate.Eval as a phase 2 stub.
func (skeletonStructuralGate) Eval(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateVerdict, error) {
	return "", errors.New("phase 2: not implemented")
}

// Benchmark implements StructuralGate.Benchmark as a phase 2 stub.
func (skeletonStructuralGate) Benchmark(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateVerdict, error) {
	return "", errors.New("phase 2: not implemented")
}

// ApplyGate implements StructuralGate.ApplyGate as a phase 2 stub.
func (skeletonStructuralGate) ApplyGate(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateVerdict, error) {
	return "", errors.New("phase 2: not implemented")
}

// skeletonRefine is a compile-time placeholder for the refinement surface. It
// carries the stub operations that satisfy Refine and holds no logic.
type skeletonRefine struct{}

// Register implements Refine.Register as a phase 2 stub.
func (skeletonRefine) Register(ctx context.Context, paths []string, note string) (RefineRecord, error) {
	return RefineRecord{}, errors.New("phase 2: not implemented")
}

// Status implements Refine.Status as a phase 2 stub.
func (skeletonRefine) Status(ctx context.Context) ([]RefineRecord, error) {
	return nil, errors.New("phase 2: not implemented")
}

// Rollback implements Refine.Rollback as a phase 2 stub.
func (skeletonRefine) Rollback(ctx context.Context, sha string) error {
	return errors.New("phase 2: not implemented")
}

// The port interfaces are satisfied by their skeleton placeholders.
var (
	_ StructuralGate = (*skeletonStructuralGate)(nil)
	_ Refine         = (*skeletonRefine)(nil)
)
