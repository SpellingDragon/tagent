package rl

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/rl/rl-architecture.md#http-api
//
// FeedbackBindingState names how one submission was attributed to the recorded
// evidence.
type FeedbackBindingState string

const (
	// FeedbackBound marks a submission attributed to a recorded decision or attempt.
	FeedbackBound FeedbackBindingState = "bound"
	// FeedbackUnbound marks a submission carrying no locatable event reference: the
	// conclusion is not bound and no reward is derived from it.
	FeedbackUnbound FeedbackBindingState = "unbound"
	// FeedbackAmbiguous marks a submission whose evidence conflicts: nothing is
	// written under a guessed binding.
	FeedbackAmbiguous FeedbackBindingState = "ambiguous"
)

// FeedbackVersionCurrent and FeedbackSourceAPI pin the version every record of this
// face is written at and the source of a conclusion that arrived over the wire.
const (
	// FeedbackVersionCurrent is the feedback record version this face writes.
	FeedbackVersionCurrent = "feedback.v1"
	// FeedbackSourceAPI names the source of a conclusion that arrived over the RL
	// HTTP face.
	FeedbackSourceAPI = "api"
)

// ErrFeedbackFaceClosed and the feedback endpoint errors beside it name the way a
// submission was refused, so a caller routes it by name rather than reading a
// status code alone.
var (
	// ErrFeedbackFaceClosed reports a request to a face beside no configured token:
	// the endpoint stays closed until a credential is configured for it.
	ErrFeedbackFaceClosed = errors.New("rl: the feedback endpoint is closed: no request token is configured")
	// ErrFeedbackNotAuthenticated reports a request whose subject did not
	// authenticate: the credential is missing, malformed or unmatched.
	ErrFeedbackNotAuthenticated = errors.New("rl: the feedback request is not authenticated")
	// ErrFeedbackMethod reports a request on a method other than POST.
	ErrFeedbackMethod = errors.New("rl: the feedback endpoint accepts POST only")
	// ErrFeedbackMalformed reports a body the endpoint cannot read as one payload.
	ErrFeedbackMalformed = errors.New("rl: the feedback body does not parse as one payload")
	// ErrFeedbackConclusionMissing reports a submission carrying no conclusion: a
	// conclusion is the evidence a reward is derived from and is never invented.
	ErrFeedbackConclusionMissing = errors.New("rl: the feedback submission carries no conclusion")
	// ErrFeedbackEventUnknown reports an event key the recorded evidence does not
	// hold: the submission is refused rather than bound to a guessed target.
	ErrFeedbackEventUnknown = errors.New("rl: no recorded event carries the feedback event key")
	// ErrFeedbackTargetAmbiguous reports evidence that resolves to conflicting
	// decisions or attempts, or a named target the evidence does not confirm.
	ErrFeedbackTargetAmbiguous = errors.New("rl: the feedback evidence resolves to conflicting decisions or attempts")
	// ErrFeedbackRecordless reports a face built beside no feedback or reward
	// account: a submission has nowhere to be written and nowhere to be reduced.
	ErrFeedbackRecordless = errors.New("rl: the feedback face carries no feedback or reward account")
)

// FeedbackTarget is the recorded identity one event key resolves to: the decision
// the record was reduced for, the attempt it observed and the model version that
// served it. The attempt is empty for a record filed at the decision boundary.
type FeedbackTarget struct {
	// EventKey is the hex event key the evidence was read under.
	EventKey string
	// DecisionID names the decision the recorded event belongs to.
	DecisionID event.DecisionID
	// AttemptID names the attempt the recorded event observed.
	AttemptID event.AttemptID
	// ModelVersion is the version of the model the recorded event was served
	// under. It is empty when the record names no version, which is bound
	// unversioned rather than assumed to be the version in effect.
	ModelVersion string
}

// FeedbackTargetResolver resolves the recorded identity behind one event key. The
// endpoint reaches no further than this seam, so attribution is decided by the
// evidence a resolver holds and never by the presence of a trace.
//
// The redline list (3 rules):
// MUST resolve the recorded identity from the evidence the resolver holds.
// MUST NOT decide attribution from the presence of a trace.
// MUST NOT answer a key the evidence does not hold as a resolved target.
type FeedbackTargetResolver interface {
	// ResolveFeedbackTarget returns the recorded identity one event key names. A key
	// the evidence does not hold is ErrFeedbackEventUnknown, and evidence naming
	// conflicting identities is ErrFeedbackTargetAmbiguous.
	ResolveFeedbackTarget(ctx context.Context, eventKey string) (FeedbackTarget, error)
}

// FeedbackSubmission is the outcome of one attributed submission: the binding state
// beside the feedback identity it was written under and the reward instance derived
// from the evidence.
type FeedbackSubmission struct {
	// State names how the submission was attributed.
	State FeedbackBindingState `json:"state"`
	// FeedbackID identifies the feedback record the submission was written as.
	FeedbackID string `json:"feedback_id,omitempty"`
	// EventKey is the event key the submission named.
	EventKey string `json:"event_key,omitempty"`
	// DecisionID names the decision the feedback is bound to.
	DecisionID event.DecisionID `json:"decision_id,omitempty"`
	// AttemptID names the attempt the feedback is bound to.
	AttemptID event.AttemptID `json:"attempt_id,omitempty"`
	// ModelVersion is the version of the model the bound evidence was served
	// under, so a submission binds to the version that ran rather than the one
	// in effect when it arrived.
	ModelVersion string `json:"model_version,omitempty"`
	// RewardInstanceID identifies the reward instance derived from the evidence.
	RewardInstanceID string `json:"reward_instance_id,omitempty"`
	// RewardRevision orders the derivation beside the earlier revisions.
	RewardRevision int64 `json:"reward_revision,omitempty"`
	// RewardVersion is the rule version the reward value was computed under.
	RewardVersion string `json:"reward_version,omitempty"`
}

// FeedbackAttribution is the running account of how the submissions this face took
// were attributed.
type FeedbackAttribution struct {
	// Bound counts the submissions attributed to recorded evidence.
	Bound int64 `json:"bound"`
	// Unbound counts the submissions carrying no locatable reference.
	Unbound int64 `json:"unbound"`
	// Ambiguous counts the submissions whose evidence conflicted.
	Ambiguous int64 `json:"ambiguous"`
}

// FeedbackEndpointOption names one optional part of a feedback face at
// construction: the request credential or the request bounds. The face carries no
// configuration value to choose from: the evidence resolver, the two accounts and
// the derivation rule are positional, and every optional part is added by the
// option that names it.
type FeedbackEndpointOption func(*FeedbackEndpoint)

// FeedbackEndpoint is the RL HTTP face of the delayed feedback entry: it
// authenticates at one point ahead of the payload read, attributes the submission to
// recorded evidence, writes the raw feedback and answers with the reward instance
// derived from it.
type FeedbackEndpoint struct {
	resolver FeedbackTargetResolver
	feedback *FeedbackLedger
	rewards  *RewardLedger
	rule     RewardRule
	token    string
	limits   RequestLimits
	now      func() time.Time

	mu         sync.Mutex
	attributed FeedbackAttribution
}

// WithFeedbackToken sets the request credential the face authenticates with: the
// bearer token of every accepted submission. A face beside no credential stays
// closed, so no submission is served under an absent credential.
func WithFeedbackToken(token string) FeedbackEndpointOption {
	return func(e *FeedbackEndpoint) { e.token = token }
}

// WithFeedbackLimits sets the request bounds the face enforces. A negative bound
// and the defaults a zero bound takes are decided by RequestLimits.Normalize, so
// the face reads one bound set for every request it serves.
func WithFeedbackLimits(limits RequestLimits) FeedbackEndpointOption {
	return func(e *FeedbackEndpoint) { e.limits = limits }
}

// NewFeedbackEndpoint returns the feedback face over the given evidence and the two
// accounts, computing every reward under the given rule. The resolver, the two
// accounts and the rule are positional because the face cannot be built without
// them; the request credential and the request bounds are named by the option that
// adds them. An argument beside no resolver, no account or no derivation rule is
// refused by name before the face is built, and a negative request bound is refused
// as well.
func NewFeedbackEndpoint(resolver FeedbackTargetResolver, feedback *FeedbackLedger, rewards *RewardLedger, rule RewardRule, opts ...FeedbackEndpointOption) (*FeedbackEndpoint, error) {
	if resolver == nil {
		return nil, fmt.Errorf("%w: the endpoint carries no evidence resolver", ErrFeedbackTargetAmbiguous)
	}
	if feedback == nil || rewards == nil {
		return nil, fmt.Errorf("%w: the endpoint carries no feedback or reward account", ErrFeedbackRecordless)
	}
	if rule.Version == "" || rule.Value == nil {
		return nil, ErrRewardRuleMissing
	}
	endpoint := &FeedbackEndpoint{
		resolver: resolver,
		feedback: feedback,
		rewards:  rewards,
		rule:     rule,
		now:      func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(endpoint)
		}
	}
	limits, err := endpoint.limits.Normalize()
	if err != nil {
		return nil, err
	}
	endpoint.limits = limits
	return endpoint, nil
}

// Attribution returns the account of how the submissions taken so far were
// attributed.
func (e *FeedbackEndpoint) Attribution() FeedbackAttribution {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.attributed
}

// authenticate judges the request subject at the single authentication point of the
// face: the gate runs ahead of the payload read, so a submission without a matched
// credential has no side effect. The bearer scheme is matched case independently and
// the token is compared in constant time.
func (e *FeedbackEndpoint) authenticate(r *http.Request) error {
	if e.token == "" {
		return ErrFeedbackFaceClosed
	}
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ErrFeedbackNotAuthenticated
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(e.token)) != 1 {
		return ErrFeedbackNotAuthenticated
	}
	return nil
}

// ServeHTTP serves one feedback submission: the request subject authenticates ahead
// of the payload read, the body is read under the declared byte bound, and the
// submission is attributed before anything is written. The reply carries the binding
// state and the reward instance, and a refusal carries its named error.
func (e *FeedbackEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := e.authenticate(r); err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="tagent-rl"`)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, ErrFeedbackMethod.Error(), http.StatusMethodNotAllowed)
		return
	}
	body, err := readBounded(r.Body, e.limits.MaxBodyBytes)
	if err != nil {
		http.Error(w, "the feedback request body: "+err.Error(), feedbackStatus(err))
		return
	}
	var payload FeedbackPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, fmt.Sprintf("%v: %v", ErrFeedbackMalformed, err), http.StatusBadRequest)
		return
	}
	submission, err := e.Submit(r.Context(), payload)
	if err != nil {
		http.Error(w, err.Error(), feedbackStatus(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(submission); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Submit attributes one submission to the recorded evidence and writes it to the
// feedback account. A submission naming no event key is answered as unbound and
// writes nothing; a key the evidence does not hold is refused by name; conflicting
// evidence is answered as ambiguous beside its named refusal. A bound submission is
// written at its own identity and the reward account is brought to the evidence set
// of its decision, taking a new revision only when the evidence changed.
func (e *FeedbackEndpoint) Submit(ctx context.Context, payload FeedbackPayload) (FeedbackSubmission, error) {
	payload.EventKey = strings.TrimSpace(payload.EventKey)
	payload.Conclusion = strings.TrimSpace(payload.Conclusion)
	payload.Source = strings.TrimSpace(payload.Source)
	if payload.Conclusion == "" {
		return FeedbackSubmission{State: FeedbackUnbound}, ErrFeedbackConclusionMissing
	}
	if payload.Source == "" {
		payload.Source = FeedbackSourceAPI
	}
	if payload.EventKey == "" {
		e.count(func(a *FeedbackAttribution) { a.Unbound++ })
		return FeedbackSubmission{State: FeedbackUnbound}, nil
	}
	if err := ctx.Err(); err != nil {
		return FeedbackSubmission{}, err
	}
	target, err := e.resolver.ResolveFeedbackTarget(ctx, payload.EventKey)
	if err != nil {
		if errors.Is(err, ErrFeedbackTargetAmbiguous) || errors.Is(err, ErrFeedbackConflict) {
			e.count(func(a *FeedbackAttribution) { a.Ambiguous++ })
			return FeedbackSubmission{State: FeedbackAmbiguous, EventKey: payload.EventKey}, err
		}
		return FeedbackSubmission{}, err
	}
	if err := confirmNamedTarget(payload, target); err != nil {
		e.count(func(a *FeedbackAttribution) { a.Ambiguous++ })
		return FeedbackSubmission{State: FeedbackAmbiguous, EventKey: payload.EventKey}, err
	}
	record := FeedbackRecord{
		FeedbackID:   feedbackIDOf(payload),
		EventKey:     target.EventKey,
		DecisionID:   target.DecisionID,
		AttemptID:    target.AttemptID,
		ModelVersion: target.ModelVersion,
		Conclusion:   payload.Conclusion,
		Source:       payload.Source,
		Version:      FeedbackVersionCurrent,
		RecordedAt:   e.now(),
	}
	if err := e.feedback.Add(record); err != nil {
		if errors.Is(err, ErrFeedbackConflict) {
			e.count(func(a *FeedbackAttribution) { a.Ambiguous++ })
			return FeedbackSubmission{State: FeedbackAmbiguous, EventKey: payload.EventKey}, err
		}
		return FeedbackSubmission{}, err
	}
	evidence := e.evidenceFor(target.DecisionID)
	reward, err := e.rewards.DeriveForEvidence(target.DecisionID, target.AttemptID, e.rule, evidence)
	if err != nil && !errors.Is(err, ErrRewardNoFeedback) {
		return FeedbackSubmission{State: FeedbackBound, FeedbackID: record.FeedbackID, EventKey: record.EventKey}, err
	}
	e.count(func(a *FeedbackAttribution) { a.Bound++ })
	return FeedbackSubmission{
		State:            FeedbackBound,
		FeedbackID:       record.FeedbackID,
		EventKey:         record.EventKey,
		DecisionID:       record.DecisionID,
		AttemptID:        record.AttemptID,
		ModelVersion:     reward.ModelVersion,
		RewardInstanceID: reward.InstanceID,
		RewardRevision:   reward.Revision,
		RewardVersion:    reward.RuleVersion,
	}, nil
}

// count adds one attribution outcome to the running account.
func (e *FeedbackEndpoint) count(add func(*FeedbackAttribution)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	add(&e.attributed)
}

// evidenceFor returns the raw feedback the reward of one decision is derived from:
// every record bound to that decision, in the order it was recorded.
func (e *FeedbackEndpoint) evidenceFor(decision event.DecisionID) []FeedbackRecord {
	var evidence []FeedbackRecord
	for _, record := range e.feedback.All() {
		if record.DecisionID == decision {
			evidence = append(evidence, record)
		}
	}
	return evidence
}

// feedbackIDOf names one submission: the identity is computed from the submission
// alone, so a repeated delivery is the same record and two different conclusions are
// two records.
func feedbackIDOf(payload FeedbackPayload) string {
	sum := sha256.Sum256([]byte(payload.EventKey + "\x00" + payload.Conclusion + "\x00" + payload.Source + "\x00" + FeedbackVersionCurrent))
	return "feedback:" + hex.EncodeToString(sum[:16])
}

// confirmNamedTarget judges the target the caller named against the recorded one: a
// named decision or attempt the evidence does not confirm is a conflict rather than a
// binding taken on trust.
func confirmNamedTarget(payload FeedbackPayload, target FeedbackTarget) error {
	if payload.DecisionID != "" && payload.DecisionID != target.DecisionID {
		return fmt.Errorf("%w: the named decision %q and the recorded decision %q disagree", ErrFeedbackTargetAmbiguous, payload.DecisionID, target.DecisionID)
	}
	if payload.AttemptID != "" && payload.AttemptID != target.AttemptID {
		return fmt.Errorf("%w: the named attempt %q and the recorded attempt %q disagree", ErrFeedbackTargetAmbiguous, payload.AttemptID, target.AttemptID)
	}
	return nil
}

// feedbackStatus maps one named refusal to the status the face answers with.
func feedbackStatus(err error) int {
	switch {
	case errors.Is(err, ErrFeedbackFaceClosed), errors.Is(err, ErrFeedbackNotAuthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, ErrFeedbackMethod):
		return http.StatusMethodNotAllowed
	case errors.Is(err, ErrBodyTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, ErrFeedbackEventUnknown):
		return http.StatusNotFound
	case errors.Is(err, ErrFeedbackTargetAmbiguous), errors.Is(err, ErrFeedbackConflict):
		return http.StatusConflict
	case errors.Is(err, ErrFeedbackMalformed), errors.Is(err, ErrFeedbackConclusionMissing):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
