package rl

import (
	"context"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// Recorder is the face one production point writes its versioned records
// through. The three methods cover the three record kinds, one call per record
// written at the point that record actually originates.
//
// The redline list (4 rules):
// MUST write each record at the point that record actually originates.
// MUST report a record it cannot accept as an error.
// MUST NOT edit the fields of a record it persists or projects.
// MUST NOT drop a record it could not accept without reporting it.
//
// - The production face of the versioned records: one episode boundary, one model decision, one observable model attempt each write their record through this face, exactly as the production point observed them.
// - A production point records what it observed and derives nothing: the terminal comes from the single model reduction, the response is the native response the attempt saw, and no record here re-classifies a state of its own.
// - A recorder error never reaches the model decision: recording is observation, not execution.
// - The telemetry observation sink shares only the word: it consumes committed events, while this face writes remembered records.
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
type Recorder interface {
	// RecordEpisode writes the episode record of one business process at the
	// point that process starts.
	RecordEpisode(ctx context.Context, record EpisodeRecord) error
	// RecordDecision writes the decision record of one model decision at the
	// point that decision produced its terminal.
	RecordDecision(ctx context.Context, record DecisionRecord) error
	// RecordAttempt writes the attempt record of one observable model attempt at
	// the point that attempt produced its response.
	RecordAttempt(ctx context.Context, record AttemptRecord) error
}

// LogprobsStateOf reports how the logprobs of one observed response are known:
// logprobs the response actually returned stand present, and a response that
// returned none stands unknown with no source, because a plain absence of
// logprobs is never reported as a provider declaration of unsupported. A
// LogprobsUnsupported state rests on a declaration the caller names and is
// never derived here.
func LogprobsStateOf(resp *model.Response) (LogprobsState, string) {
	if resp == nil {
		return LogprobsUnknown, ""
	}
	for _, choice := range resp.Choices {
		if choice.Logprobs != nil {
			return LogprobsPresent, ""
		}
	}
	return LogprobsUnknown, ""
}

// ErrLogprobsSourceMissing reports an attempt carrying a LogprobsUnsupported
// state with no declaration named: an unsupported state rests on a provider
// declaration and is never written or read without naming it.
var ErrLogprobsSourceMissing = errors.New("rl: the logprobs unsupported state carries no declaration source")

// ValidateAttempt reports whether one attempt record may be written and read
// back. It holds exactly one rule: a LogprobsUnsupported state is valid only
// beside the declaration it rests on. A present state rests on the values the
// response returned and needs no source, and an unknown state is never forced
// into either of the other two.
func ValidateAttempt(record AttemptRecord) error {
	if record.Logprobs == LogprobsUnsupported && record.LogprobsSource == "" {
		return ErrLogprobsSourceMissing
	}
	return nil
}

// AttemptOf is the single observation face of one observable model attempt:
// the record of what the reduction actually produced. The terminal is the Kind
// the model reduction derived — it is never re-derived here — the response is
// the native response body the attempt observed, and the logprobs state is read
// from that body through LogprobsStateOf so a value the response never returned
// is never reported as present.
//
// The model version is read from the identity the serving version declared on
// the response it handed over, so the version a record names is the one that
// actually served the attempt rather than the one in effect when the record was
// written. A response naming no identity is recorded unversioned.
func AttemptOf(id event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) AttemptRecord {
	logprobs, source := LogprobsStateOf(outcome.Body)
	response := model.Response{}
	modelVersion := ""
	if outcome.Body != nil {
		response = *outcome.Body
		modelVersion = ParseModelIdentity(outcome.Body.Model).Version
	}
	return AttemptRecord{
		Schema:         SchemaCurrent,
		ID:             id,
		DecisionID:     decisionID,
		EpisodeID:      episodeID,
		ModelVersion:   modelVersion,
		Response:       response,
		Terminal:       outcome.Kind,
		Usage:          outcome.Usage,
		Error:          AttemptError(outcome),
		Logprobs:       logprobs,
		LogprobsSource: source,
		Timestamp:      time.Now(),
	}
}

// AttemptError reports the error text one observed attempt carries: the same
// cause the reduction reports, read through the one accessor every face shares
// so the attempt record and the observation line can never name two causes for
// one request. It is empty for an attempt that observed no error, and it reports
// what was observed without judging a terminal.
func AttemptError(outcome model.Outcome) string {
	if cause := outcome.Cause(); cause != nil {
		return cause.Error()
	}
	return ""
}

// RecorderAdapter adapts the execution loop's surviving narrow face onto this
// package's record pipeline: the loop hands over the attempt it admitted — the
// request view material of a decision and the reduced outcome of an attempt —
// and the adapter assembles the versioned attempt record and hands it to the
// wrapped recorder exactly as the attempt observed it. The adapter edits no
// field of the record it assembles and derives nothing: the attempt record is
// built through the single observation face AttemptOf, and a visible member
// that ran unversioned is recorded unversioned rather than assumed current.
//
// - The attempt admission is the one production point this adapter writes: the loop merged its attempt record into the capture admission, so one observable model attempt is one write here and never two.
// - The episode boundary and the stored input of a decision are not observed at this point, so no episode or decision record is assembled here: a consumer that needs them derives them from the capture's own evidence beside the chain the run keeps, and this adapter invents neither.
// - The snapshot and the seal belong to the capture that fixes and closes them; accepting them here keeps one loop observation crossing one face, and writes nothing of its own.
//
// The methods below carry built-in, event and model types only — the same
// shapes the execution loop's capture face crosses — so this adapter satisfies
// that face with no package edge into the execution core.
type RecorderAdapter struct {
	inner Recorder
}

// NewRecorderAdapter wraps one record pipeline in the capture face the
// execution loop attaches: the attempt assembled from the one admission the
// loop hands over is written through inner.
func NewRecorderAdapter(inner Recorder) RecorderAdapter {
	return RecorderAdapter{inner: inner}
}

// Snapshot accepts the view material of one decision's request without writing
// a record of its own: the snapshot is the capture's to fix, and the view is
// read back from that evidence rather than restated as a decision record here.
func (a RecorderAdapter) Snapshot(ctx context.Context, decisionID event.DecisionID, modelName, promptVersion string, visibleTools, visibleSkills []string, req model.Request) error {
	return nil
}

// Record builds the attempt record of one observed attempt through the single
// observation face and writes it through the wrapped pipeline: the terminal it
// records is the one the model reduction derived, and none is re-derived here.
// The attempt's decision is named by the identity the loop admitted it under, so
// an attempt of an intermediate decision stays replayable exactly like the last.
func (a RecorderAdapter) Record(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) error {
	return a.inner.RecordAttempt(ctx, AttemptOf(attemptID, decisionID, episodeID, outcome))
}

// Seal accepts the close of the turn's capture without sealing anything of its
// own: the seal is the capture's account of what its writer confirmed, and this
// adapter keeps no queue to close.
func (a RecorderAdapter) Seal(ctx context.Context) error {
	return nil
}
