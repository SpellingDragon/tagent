// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
//
// Package rl is the training observation surface of the runtime: it declares the
// versioned records a model request is remembered by, the episode and dependency
// grouping those records are split on, and the capture port that fixes a request
// view before the request starts.
//
// The package is data and ports only. Record identities come from the event
// package, the observed response is the native model response, and no type here
// derives an identity, a lineage or a redaction on its own.
package rl

import (
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
)

// SchemaVersion names the record schema a line was written with. A reader selects
// its parse path from this value, and a version it does not recognise is reported
// as an unknown version rather than read under a known one.
//
// The concrete version values belong to the shared record schema contract; this
// package fixes only the type that carries one.
type SchemaVersion string

// Lineage states how certain a record's lineage is.
//
// LineageUnknown is the zero value: a record whose origin could not be read is
// marked unknown, and an unknown lineage is never reported as a known parent.
type Lineage int

const (
	// LineageUnknown is the zero value: the record's origin could not be read.
	LineageUnknown Lineage = iota
	// LineageKnown is a lineage read from the application's own episode boundary.
	LineageKnown
)

// LogprobsState states how the logprobs of one attempt are known.
//
// LogprobsUnknown is the zero value: with neither a returned value nor a provider
// declaration observed, the state is unknown, and unknown is never reported as a
// plain absence of logprobs.
type LogprobsState int

const (
	// LogprobsUnknown is the zero value: no returned logprobs and no provider
	// declaration were observed for the attempt.
	LogprobsUnknown LogprobsState = iota
	// LogprobsPresent marks logprobs the response actually returned.
	LogprobsPresent
	// LogprobsUnsupported marks a provider that declared the logprobs capability
	// unsupported; LogprobsSource names that declaration.
	LogprobsUnsupported
)

// ModelViewRef references the model view a decision was actually made on: the
// model and generation parameters of the request together with the prompt, skill
// and tool set visible to it, each with the version it was read at.
//
// The type names the view; it holds no second copy of the request.
type ModelViewRef struct {
	// Model names the model the request was declared against.
	Model string
	// ModelVersion is the version of that model.
	ModelVersion string
	// ParametersDigest is the content digest of the generation parameters the
	// request was declared with.
	ParametersDigest string
	// PromptVersion is the version of the system prompt content the request was
	// assembled from.
	PromptVersion string
	// VisibleTools lists the tool names visible to the request.
	VisibleTools []string
	// VisibleSkills lists the skill names visible to the request.
	VisibleSkills []string
	// ContextDigest is the content digest of the assembled context.
	ContextDigest string
}

// EpisodeRecord is one business process as data: its lineage, its parent and the
// dependencies that decide which records may be split apart from it.
//
// An episode is a business process and not a session: a session may hold several
// episodes and an episode may span several sessions, and neither equality is
// forced. A record whose origin could not be read carries LineageUnknown with an
// empty ParentID instead of a guessed parent.
type EpisodeRecord struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion
	// ID identifies the episode.
	ID event.EpisodeID
	// ParentID names the episode this one was spawned from; it is empty for a
	// root episode and for an episode of unknown lineage.
	ParentID event.EpisodeID
	// RootID names the first episode of the lineage.
	RootID event.EpisodeID
	// Ancestors lists the lineage from the root down to ParentID, in that order.
	Ancestors []event.EpisodeID
	// Sessions lists the sessions the episode spans.
	Sessions []event.SessionID
	// DependsOn declares the episodes this episode shares data with, which is the
	// basis a leakage free split is drawn on.
	DependsOn []EpisodeDependency
	// Lineage states how certain the recorded lineage is.
	Lineage Lineage
	// Timestamp is the time the record was written.
	Timestamp time.Time
}

// DecisionRecord is one model decision as data: the input events it was reduced
// from, the model view it actually ran on and the invocation it belongs to.
type DecisionRecord struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion
	// ID identifies the decision boundary.
	ID event.DecisionID
	// InvocationID scopes the decision to one responsibility range.
	InvocationID event.InvocationID
	// EpisodeID names the episode the decision belongs to.
	EpisodeID event.EpisodeID
	// InputEvents lists the stored events that were reduced into this decision, in
	// the order they were read.
	InputEvents []event.EventID
	// ModelView references the view the request actually ran on.
	ModelView ModelViewRef
	// Timestamp is the time the record was written.
	Timestamp time.Time
}

// AttemptRecord is one observable model attempt as data: the response it observed,
// the terminal it reached and the logprobs state of that response.
//
// A retry below the decision boundary is not a second attempt, and the response is
// the native response the attempt observed rather than a derived copy of it.
type AttemptRecord struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion
	// ID identifies the attempt.
	ID event.AttemptID
	// DecisionID names the decision the attempt belongs to.
	DecisionID event.DecisionID
	// EpisodeID names the episode the attempt belongs to.
	EpisodeID event.EpisodeID
	// Response is the native model response the attempt observed, referenced as
	// the snapshot the attempt produced.
	Response model.Response
	// Terminal is the single terminal the model reduction derived for the attempt;
	// the capture shares that terminal and derives none of its own.
	Terminal model.Kind
	// Logprobs states how the logprobs of the response are known.
	Logprobs LogprobsState
	// LogprobsSource names the provider declaration a LogprobsUnsupported state
	// rests on; it is required for that state and empty otherwise.
	LogprobsSource string
	// Timestamp is the time the record was written.
	Timestamp time.Time
}
