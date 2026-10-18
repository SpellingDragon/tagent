package evolution

import (
	"context"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/model"
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
	_ func(StructuralGate, context.Context, state.OrgPlan, state.StatePlan) (GateReport, error) = StructuralGate.Eval

	_ func(StructuralGate, context.Context, state.OrgPlan, state.StatePlan) (GateReport, error) = StructuralGate.Benchmark

	_ func(StructuralGate, context.Context, state.OrgPlan, state.StatePlan) (GateReport, error) = StructuralGate.ApplyGate

	_ func(Refine, context.Context, []string, string) (RefineRecord, error) = Refine.Register

	_ func(Refine, context.Context) ([]RefineRecord, error) = Refine.Status

	_ func(Refine, context.Context, string) error = Refine.Rollback
)

// Every declared port name is an interface type.
var (
	_ StructuralGate = (StructuralGate)(nil)
	_ Refine         = (Refine)(nil)
)

// The declared record, report and window types keep every documented field.
var (
	_ = RefineRecord{
		Paths:       []string{},
		Note:        "",
		SHA:         "",
		CommittedAt: time.Time{},
		GateBasis: GateReport{
			Verdict:      GateVerdictNotApplicable,
			Reason:       "",
			Scope:        "",
			EvidenceRefs: []EvidenceRef{},
		},
		Window: EvidenceWindow{
			Version:            "",
			ActivatedAt:        time.Time{},
			ActivationEvidence: EvidenceRef(""),
		},
		Posterior: PosteriorReport{
			Verdict:      PosteriorInsufficient,
			Reason:       "",
			EvidenceRefs: []EvidenceRef{},
		},
	}

	_ = GateReport{
		Verdict:      GateVerdictPass,
		Reason:       "",
		Scope:        "",
		EvidenceRefs: []EvidenceRef{},
	}

	_ = EvidenceWindow{
		Version:            "",
		ActivatedAt:        time.Time{},
		ActivationEvidence: EvidenceRef(""),
	}

	_ = PosteriorReport{
		Verdict:      PosteriorHealthy,
		Reason:       "",
		EvidenceRefs: []EvidenceRef{},
	}

	_ EvidenceRef = EvidenceRef("")

	_ = Improvement{
		SHA:   "",
		Paths: []string{},
		Note:  "",
	}

	_ ImprovementRecorder = func(ctx context.Context, improvement Improvement) error { return nil }
)

// The verdict vocabularies stay compile-time constants of their declared types:
// the structural gate's dispositions and the post-hoc states are two disjoint
// vocabularies.
const (
	_ GateVerdict = GateVerdictPass
	_ GateVerdict = GateVerdictReject
	_ GateVerdict = GateVerdictNotApplicable
	_ GateVerdict = GateVerdictInsufficientEvidence

	_ PosteriorVerdict = PosteriorHealthy
	_ PosteriorVerdict = PosteriorDegraded
	_ PosteriorVerdict = PosteriorInsufficient
	_ PosteriorVerdict = PosteriorNotDue

	_ string = ImprovementMarker
)

// skeletonStructuralGate is a compile-time placeholder for the structural gate.
// It carries the stub segments that satisfy StructuralGate and holds no logic.
type skeletonStructuralGate struct{}

// Eval implements StructuralGate.Eval as a phase 2 stub.
func (skeletonStructuralGate) Eval(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error) {
	return GateReport{}, errors.New("phase 2: not implemented")
}

// Benchmark implements StructuralGate.Benchmark as a phase 2 stub.
func (skeletonStructuralGate) Benchmark(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error) {
	return GateReport{}, errors.New("phase 2: not implemented")
}

// ApplyGate implements StructuralGate.ApplyGate as a phase 2 stub.
func (skeletonStructuralGate) ApplyGate(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error) {
	return GateReport{}, errors.New("phase 2: not implemented")
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

// The activation evidence window and the post-hoc evaluation keep exactly
// these shapes: a renamed field or a changed signature stops this block from
// compiling.
var (
	_ func() *ActivationLedger = NewActivationLedger

	_ func(*ActivationLedger, Activation) (EvidenceWindow, bool, error) = (*ActivationLedger).Consume
	_ func(*ActivationLedger, string) (EvidenceWindow, bool)            = (*ActivationLedger).Window
	_ func(*ActivationLedger, string) []Activation                      = (*ActivationLedger).Consumed
	_ func(*ActivationLedger, string, string) error                     = (*ActivationLedger).Correlate
	_ func(*ActivationLedger, string) (string, bool)                    = (*ActivationLedger).RegistrationOf

	_ = Activation{
		Version:  "",
		At:       time.Time{},
		Evidence: "",
	}

	_ = Feedback{
		Stamp:    "",
		Consumed: "",
		Degrades: false,
		Evidence: "",
	}

	_ = Guardrail{
		Enabled:       false,
		DenialCount:   0,
		CriticalCount: 0,
	}

	_ = JudgePolicy{
		JudgeDelay:      0,
		MinSamples:      0,
		NegativeRateMax: 0,
		DenialRateMax:   0,
		CriticalRateMax: 0,
	}

	_ func() JudgePolicy = DefaultJudgePolicy

	_ = Review{
		Version:   "",
		Turns:     0,
		Joined:    nil,
		Guardrail: Guardrail{},
	}

	_ = Judgment{
		Decidable: false,
		Degraded:  false,
		Reason:    "",
		Evidence:  nil,
	}

	_ func(context.Context, Review) (Judgment, error)   = Judge(nil)
	_ func(context.Context, string) ([]Feedback, error) = FeedbackSource(nil)
	_ func(context.Context, string) (Guardrail, error)  = GuardrailSource(nil)
	_ func(context.Context, Evaluation) error           = EvaluationRecorder(nil)

	_ = Evaluation{
		Version:      "",
		SHA:          "",
		Verdict:      "",
		Summary:      "",
		Suggestion:   "",
		EvidenceRefs: nil,
	}

	_ func(*ActivationLedger, JudgePolicy, FeedbackSource, GuardrailSource, Judge, EvaluationRecorder) *Posterior = NewPosterior
	_ func(*Posterior, context.Context, string, time.Time) (EvidenceWindow, PosteriorReport, error)               = (*Posterior).Evaluate

	_ func(model.Model, float64) Judge = NewModelJudge
)
