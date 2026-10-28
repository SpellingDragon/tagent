package evolution

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
//
// Skeleton compile-time assertions for the evaluation declarations of the
// self-improvement domain: the structural gate's three segments, the records the
// gate and the post-hoc evaluation answer with, and the activation evidence
// window they are joined over. A renamed field, a widened signature or a constant
// of another type stops this file from compiling.
var (
	_ func(StructuralGate, context.Context, state.OrgPlan, state.StatePlan) (GateReport, error) = StructuralGate.Eval

	_ func(StructuralGate, context.Context, state.OrgPlan, state.StatePlan) (GateReport, error) = StructuralGate.Benchmark

	_ func(StructuralGate, context.Context, state.OrgPlan, state.StatePlan) (GateReport, error) = StructuralGate.ApplyGate
)

// Every declared port name is an interface type.
var (
	_ StructuralGate = (StructuralGate)(nil)
)

// The declared record and window types keep every documented field.
var (
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
