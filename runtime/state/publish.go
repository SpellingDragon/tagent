package state

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// 契约: docs/wiki/platform/org-hot-reload.md#generations
//
// Generation is the publication identity of one effective binding generation:
// the monotonically increasing number the sole publication entry assigns at
// each actual publication. The number, never the content fingerprint, names the
// publication: a byte-identical reload assigns no number and builds no new
// resource, and a rollback re-issues the previous full configuration as one
// more number beside freshly constructed live objects.
type Generation uint64

var (
	// ErrRequiredCheckMissing reports a publication refused for missing required
	// evidence: a required check the governance left undispositioned. A check
	// without a disposition is a missing check and is never read as passing.
	ErrRequiredCheckMissing = errors.New("state: the publication carries no disposition for a required check")
	// ErrPublicationContractEmpty reports a publish contract that requires no
	// check at all. It is a named case of ErrRequiredCheckMissing: a contract
	// that demands no evidence refuses every publication.
	ErrPublicationContractEmpty = fmt.Errorf("%w: the publish contract requires no check", ErrRequiredCheckMissing)
	// ErrCheckRefused reports a publication one governance check refused.
	ErrCheckRefused = errors.New("state: a governance check refused the publication")
	// ErrExemptionUnrecorded reports a check exemption that carries no reason or
	// no scope. An exemption is recorded explicitly or it holds nothing.
	ErrExemptionUnrecorded = errors.New("state: the check exemption records no reason or no scope")
	// ErrPublicationCommitterMissing reports a publisher that carries no
	// publication recorder, so no publication record could be committed beside
	// any switch it would make.
	ErrPublicationCommitterMissing = errors.New("state: the publisher carries no publication recorder")
	// ErrBindingHolderMissing reports a publisher that carries no binding holder.
	ErrBindingHolderMissing = errors.New("state: the publisher carries no binding holder")
	// ErrGenerationDiverged reports a hold that assigned a generation other than
	// the one the committed publication record names: a publication outside this
	// entry touched the holder and the numbering it owns.
	ErrGenerationDiverged = errors.New("state: the assigned generation diverges from the committed publication record")
	// ErrRollbackUnavailable reports a rollback with no previous full
	// configuration to rebuild from: no publication has superseded another yet.
	ErrRollbackUnavailable = errors.New("state: no previous full configuration is available to roll back to")
	// ErrRollbackSourceMismatch reports a rollback candidate that carries
	// content other than the rollback source it rebuilds.
	ErrRollbackSourceMismatch = errors.New("state: the rollback candidate does not carry the rollback source fingerprint")
)

// CheckName names one governance check the publish precondition contract
// declares as required evidence for a publication.
type CheckName string

// CheckVerdict is the disposition one governance check reports for one
// publication candidate.
type CheckVerdict int

const (
	// CheckUnanswered is the zero value and the conservative disposition: a
	// check that reports no disposition is never read as passing.
	CheckUnanswered CheckVerdict = iota
	// CheckPass reports a check whose evidence supports the publication.
	CheckPass
	// CheckReject reports a check that refuses the publication.
	CheckReject
	// CheckExempt reports a check that is not applicable to the candidate or is
	// explicitly disabled; its exemption is recorded beside its reason and scope
	// and counts as no passing evidence.
	CheckExempt
)

// CheckAnswer is the recorded disposition of one governance check: the verdict
// beside the reason and scope it rests on and the evidence it cites.
type CheckAnswer struct {
	// Name names the check this answer dispositions.
	Name CheckName
	// Verdict is the disposition reported.
	Verdict CheckVerdict
	// Reason states the ground the disposition rests on.
	Reason string
	// Scope names the surface the disposition covers.
	Scope string
	// Evidence lists the evidence the disposition cites.
	Evidence []string
}

// PublishContract is the publish precondition contract: the checks one
// publication must carry a disposition for. A contract that requires no check
// refuses every publication, so silence is missing evidence and never a passing
// disposition.
type PublishContract struct {
	// Required names the checks one publication must carry a disposition for.
	Required []CheckName
}

// Governance is the policy seam the publish precondition contract is enforced
// against: it evaluates one publication candidate and dispositions the checks
// by name. A deterministic stand-in of the real policy may be injected to check
// the publication mechanism; a stand-in's answers are mechanism evidence and
// carry no quality conclusion, and the formal quality assessment lives in the
// structural evaluation gate.
//
// The redline list (4 rules):
// MUST answer each check under the name it is filed by.
// MUST refuse a publication whose required check is missing.
// MUST NOT count an inapplicable or disabled check as evidence that passed.
// MUST NOT draw a quality conclusion from the answers of an injected stand-in.
type Governance interface {
	// Check evaluates one candidate plan and answers each check under the name
	// it is filed by.
	Check(ctx context.Context, plan StatePlan) (map[CheckName]CheckAnswer, error)
}

// PublishResult reports how one publication attempt took effect.
type PublishResult int

const (
	// PublishUnchanged is the zero value: the attempt published nothing because
	// the candidate carries the content already in effect.
	PublishUnchanged PublishResult = iota
	// PublishApplied is a publication that took effect as one new generation
	// built from a fresh change.
	PublishApplied
	// PublishRolledBack is a publication that took effect as one new generation
	// built from the previous full configuration.
	PublishRolledBack
)

// PublicationRecord is the self-contained ledger unit of one publication: the
// generation it assigned, the fingerprint it published, the result it took and
// the generation its content came back from, beside every check disposition it
// recorded so an exemption stays visible with its reason and scope. The unit is
// committed through the publication recorder alone and shares no commit with a
// task finality transition or a pending notification.
type PublicationRecord struct {
	// Generation is the generation the publication assigned.
	Generation Generation
	// Fingerprint is the organization fingerprint the publication published.
	Fingerprint string
	// Result reports how the publication took effect.
	Result PublishResult
	// RollbackSource names the generation the content came back from; it stays
	// zero for a publication built from a fresh change.
	RollbackSource Generation
	// Checks carries every check disposition the publication recorded, each
	// self-contained.
	Checks []CheckAnswer
}

// PublicationRecorder is the port one publication record is committed through:
// one self-contained unit per publication, beside the task commit port and
// never inside a task commit record.
//
// The redline list (3 rules):
// MUST commit one self-contained record per publication.
// MUST keep the publication record outside any task commit record.
// MUST NOT report a publication as recorded before its record is stored.
type PublicationRecorder interface {
	// CommitPublication stores one publication record.
	CommitPublication(ctx context.Context, record PublicationRecord) error
}

// Candidate is one prepared publication candidate: the state plan it carries
// and the resource ledger of the candidate-exclusive resources its construction
// acquired. The ledger is the candidate's own release face, so a candidate that
// leaves the publication frees exactly what it acquired and touches neither the
// shared faces nor the resources already in service.
type Candidate struct {
	// Plan is the prepared state plan.
	Plan StatePlan
	// Resources is the ledger of the candidate-exclusive resources.
	Resources *Leases
}

// PublishOutcome is what one publication attempt reports: the publication in
// effect beside the result of the attempt. A refusal reports the zero outcome
// beside the error that names it.
type PublishOutcome struct {
	// Publication is the publication in effect once the attempt ended.
	Publication Publication
	// Result reports how the attempt took effect.
	Result PublishResult
}

// Publisher is the sole publication entry over one binding holder: the startup
// generation, a lazy or manual reload and a rollback each publish through this
// entry, and no second path switches the effective binding. The entry keeps the
// candidate construction apart from the publication: the baseline re-check, the
// governance dispositions and the record commit each end before the effective
// binding, the generation number, the rollback source and the recorded
// diagnostics switch as one commit.
//
// A failure before the switch leaves the effective binding untouched and frees
// the candidate-exclusive resources in the reverse of the construction order;
// the shared faces and the resources already in service stay out of that
// release. A superseded generation keeps its resources while a request still
// holds it and retires when the last hold is gone.
type Publisher struct {
	mu         sync.Mutex
	bindings   *Bindings
	contract   PublishContract
	governance Governance
	records    PublicationRecorder
	retire     *retirement

	effective Publication
	current   OrgPlan
	source    OrgPlan
	sourceGen Generation
	sourceOK  bool
}

// NewPublisher returns the publication entry over one binding holder: the
// publish precondition contract to enforce, the governance to ask and the
// recorder to commit each publication record through. The entry starts with no
// generation published, so no rollback source exists yet.
func NewPublisher(bindings *Bindings, contract PublishContract, governance Governance, records PublicationRecorder) *Publisher {
	return &Publisher{
		bindings:   bindings,
		contract:   contract,
		governance: governance,
		records:    records,
		retire:     newRetirement(bindings),
	}
}

// Current returns the publication in effect: the generation, the fingerprint it
// published and the baseline it was admitted against. The zero publication
// stands while nothing has been published.
func (p *Publisher) Current() Publication {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.effective
}

// RollbackConfig returns the previous full configuration the rollback rebuilds
// and whether one exists. The caller rebuilds it beside freshly constructed
// live objects and submits that candidate through Rollback; the source stays
// the same across consecutive rollbacks.
func (p *Publisher) RollbackConfig() (OrgPlan, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.source, p.sourceOK
}

// Publish runs one prepared candidate through the sole publication entry and
// reports how the attempt took effect. The candidate records the baseline of
// the entry at construction, and the commit point re-checks it: a candidate
// whose baseline a newer publication advanced is discarded instead of
// overwriting that publication. A candidate carrying the fingerprint already in
// effect publishes nothing: no generation is assigned, no record is committed
// and no resource enters service.
//
// A publication commits its record before the switch, so a record that cannot
// be committed refuses the publication and the effective binding stays as it
// was. An error returned beside an applied outcome reports a retirement release
// that failed after the publication took effect.
func (p *Publisher) Publish(ctx context.Context, cand Candidate) (PublishOutcome, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.publishLocked(ctx, cand, 0)
}

// Rollback re-publishes the previous full configuration through the same entry
// and reports how the attempt took effect. The candidate carries that
// configuration rebuilt beside freshly constructed live objects: no retired
// execution object is restored, and the rollback assigns one more generation
// even when the content matches the one in effect.
func (p *Publisher) Rollback(ctx context.Context, cand Candidate) (PublishOutcome, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.sourceOK {
		return PublishOutcome{}, refuse(ctx, cand, ErrRollbackUnavailable)
	}
	if cand.Plan.Plan.Fingerprint != p.source.Fingerprint {
		return PublishOutcome{}, refuse(ctx, cand, ErrRollbackSourceMismatch)
	}
	return p.publishLocked(ctx, cand, p.sourceGen)
}

// publishLocked runs one candidate through the publication once the entry owns
// the turn: source zero publishes a fresh change, source non-zero rolls the
// named generation back.
func (p *Publisher) publishLocked(ctx context.Context, cand Candidate, source Generation) (PublishOutcome, error) {
	if p.bindings == nil {
		return PublishOutcome{}, refuse(ctx, cand, ErrBindingHolderMissing)
	}
	if p.records == nil {
		return PublishOutcome{}, refuse(ctx, cand, ErrPublicationCommitterMissing)
	}
	if len(cand.Plan.Agents) == 0 {
		return PublishOutcome{}, refuse(ctx, cand, ErrPlanEmpty)
	}
	if err := p.recheckBaseline(cand); err != nil {
		return PublishOutcome{}, refuse(ctx, cand, err)
	}
	if source == 0 && p.effective.Generation != 0 && cand.Plan.Plan.Fingerprint == p.effective.Fingerprint {
		if err := releaseCandidate(ctx, cand); err != nil {
			return PublishOutcome{Publication: p.effective, Result: PublishUnchanged}, err
		}
		return PublishOutcome{Publication: p.effective, Result: PublishUnchanged}, nil
	}
	answers, err := p.evaluate(ctx, cand)
	if err != nil {
		return PublishOutcome{}, refuse(ctx, cand, err)
	}
	record, err := p.commitRecord(ctx, cand, source, answers)
	if err != nil {
		return PublishOutcome{}, refuse(ctx, cand, err)
	}
	if err := p.bindings.Hold(cand.Plan); err != nil {
		return PublishOutcome{}, refuse(ctx, cand, fmt.Errorf("state: hold the published plan: %w", err))
	}
	result := PublishApplied
	if source != 0 {
		result = PublishRolledBack
	}
	got := Generation(peekGeneration(p.bindings))
	if source == 0 && p.effective.Generation != 0 {
		p.source, p.sourceGen, p.sourceOK = p.current, Generation(p.effective.Generation), true
	}
	p.current = cand.Plan.Plan
	p.effective = Publication{
		Generation:  uint64(got),
		Fingerprint: cand.Plan.Plan.Fingerprint,
		Baseline:    cand.Plan.Plan.Baseline,
	}
	p.retire.track(got, cand.Resources)
	out := PublishOutcome{Publication: p.effective, Result: result}
	if got != record.Generation {
		return out, errors.Join(ErrGenerationDiverged, p.retire.sweep(ctx))
	}
	if err := p.retire.sweep(ctx); err != nil {
		return out, err
	}
	return out, nil
}

// evaluate runs the governance seam over the candidate and checks every
// disposition against the publish precondition contract: a contract that
// requires no check, a required check left undispositioned, a check that
// refuses, and an exemption that records no reason or no scope each refuse the
// publication. An exemption is recorded with its reason and scope and counts as
// no passing evidence beside the dispositions it stands next to.
func (p *Publisher) evaluate(ctx context.Context, cand Candidate) ([]CheckAnswer, error) {
	if len(p.contract.Required) == 0 {
		return nil, ErrPublicationContractEmpty
	}
	var answers map[CheckName]CheckAnswer
	if p.governance != nil {
		var err error
		answers, err = p.governance.Check(ctx, cand.Plan)
		if err != nil {
			return nil, fmt.Errorf("state: govern the publication candidate: %w", err)
		}
	}
	out := make([]CheckAnswer, 0, len(answers))
	for name, answer := range answers {
		if answer.Verdict == CheckExempt && (answer.Reason == "" || answer.Scope == "") {
			return nil, fmt.Errorf("%w: check %s", ErrExemptionUnrecorded, name)
		}
		if answer.Verdict == CheckReject {
			return nil, fmt.Errorf("%w: check %s", ErrCheckRefused, name)
		}
		answer.Name = name
		out = append(out, answer)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	for _, need := range p.contract.Required {
		answer, ok := answers[need]
		if !ok || answer.Verdict == CheckUnanswered {
			return nil, fmt.Errorf("%w: check %s", ErrRequiredCheckMissing, need)
		}
	}
	return out, nil
}

// commitRecord commits one publication record through the recorder beside the
// switch it precedes: the record carries the generation the hold assigns, and a
// record that cannot be committed refuses the publication with the effective
// binding untouched.
func (p *Publisher) commitRecord(ctx context.Context, cand Candidate, source Generation, answers []CheckAnswer) (PublicationRecord, error) {
	result := PublishApplied
	if source != 0 {
		result = PublishRolledBack
	}
	record := PublicationRecord{
		Generation:     Generation(peekGeneration(p.bindings)) + 1,
		Fingerprint:    cand.Plan.Plan.Fingerprint,
		Result:         result,
		RollbackSource: source,
		Checks:         append([]CheckAnswer(nil), answers...),
	}
	if err := p.records.CommitPublication(ctx, record); err != nil {
		return PublicationRecord{}, fmt.Errorf("state: commit the publication record: %w", err)
	}
	return record, nil
}

// peekGeneration reads the generation number the binding holder has assigned so
// far: the single source the publication numbering comes from.
func peekGeneration(b *Bindings) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.generation
}

// refuse frees the candidate-exclusive resources beside the refusal that
// stopped the publication and keeps that refusal as the returned cause. The
// shared faces and the resources already in service stay out of the release.
func refuse(ctx context.Context, cand Candidate, cause error) error {
	if err := releaseCandidate(ctx, cand); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

// releaseCandidate frees the candidate-exclusive resources the candidate
// construction acquired, in the reverse of the construction order and once
// each.
func releaseCandidate(ctx context.Context, cand Candidate) error {
	if cand.Resources == nil {
		return nil
	}
	return cand.Resources.ReleaseAll(ctx)
}
