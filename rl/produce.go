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

// AttemptError reports the error text one observed attempt carries: the error
// of the retained body, or the first error the observation trace carries when
// the body is absent or carries none. It is empty for an attempt that observed
// no error, and it reports what was observed without judging a terminal.
func AttemptError(outcome model.Outcome) string {
	if outcome.Body != nil && outcome.Body.Error != nil {
		return outcome.Body.Error.Message
	}
	for _, item := range outcome.Trace {
		if item.Err != nil {
			return item.Err.Error()
		}
	}
	return ""
}

// RecorderAdapter adapts the execution loop's narrow recording face onto this
// package's record pipeline: the scattered observations one production point
// writes are assembled here into the versioned episode, decision and attempt
// records and handed to the wrapped recorder exactly as the production point
// observed them. The adapter edits no field of the record it assembles and
// derives nothing: the attempt record is built through the single observation
// face AttemptOf, and a visible member that ran unversioned is recorded
// unversioned rather than assumed current.
//
// The methods below carry built-in, event and model types only — the same
// shapes the execution loop's recording face crosses — so this adapter
// satisfies that face with no package edge into the execution core.
type RecorderAdapter struct {
	inner Recorder
}

// NewRecorderAdapter wraps one record pipeline in the recording face the
// execution loop attaches: every record assembled from one production point's
// observations is written through inner.
func NewRecorderAdapter(inner Recorder) RecorderAdapter {
	return RecorderAdapter{inner: inner}
}

// RecordEpisode assembles the episode record of one business process from the
// boundary the production point observed and writes it through the wrapped
// pipeline. The lineage is recorded as unknown with no parent invented: no
// origin is read at this production point, and an unknown lineage is never
// reported as a known parent.
func (a RecorderAdapter) RecordEpisode(ctx context.Context, episodeID event.EpisodeID, sessionID event.SessionID, at time.Time) error {
	return a.inner.RecordEpisode(ctx, EpisodeRecord{
		Schema:    SchemaCurrent,
		ID:        episodeID,
		Sessions:  []event.SessionID{sessionID},
		Lineage:   LineageUnknown,
		Timestamp: at,
	})
}

// RecordDecision assembles the decision record of one model decision from the
// identities, stored input events and model view material the production point
// observed and writes it through the wrapped pipeline. A visible tool that ran
// unversioned is recorded unversioned rather than assumed current, and the
// snapshot reference stays empty at this production point: the safe request
// snapshot is the capture port's to produce, and a digest alone is never a
// snapshot reference.
func (a RecorderAdapter) RecordDecision(ctx context.Context, decisionID event.DecisionID, invocationID event.InvocationID, episodeID event.EpisodeID, inputs []event.EventID, modelName, promptVersion string, visibleTools []string, at time.Time) error {
	view := ModelViewRef{Model: modelName, PromptVersion: promptVersion}
	for _, name := range visibleTools {
		view.VisibleTools = append(view.VisibleTools, VersionedName{Name: name})
	}
	return a.inner.RecordDecision(ctx, DecisionRecord{
		Schema:       SchemaCurrent,
		ID:           decisionID,
		InvocationID: invocationID,
		EpisodeID:    episodeID,
		InputEvents:  inputs,
		ModelView:    view,
		Timestamp:    at,
	})
}

// RecordAttempt builds the attempt record through the single observation face
// and writes it through the wrapped pipeline: the terminal it records is the
// one the model reduction derived, and none is re-derived here.
func (a RecorderAdapter) RecordAttempt(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) error {
	return a.inner.RecordAttempt(ctx, AttemptOf(attemptID, decisionID, episodeID, outcome))
}
