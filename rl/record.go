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
//
//   - record vocabulary: the versioned schemas and the line envelope (record, line)
//   - capture: the pre-request snapshot port, its redaction policy and the sealed sink (capture, sink, writer)
//   - production face: the recording port the execution loop attaches (produce)
//   - feedback attribution: the counters the host reads beside the training face (feedback)
//   - replacement lease: the swappable model handle and its replacement history (replacement)
//   - record vocabulary versus event identity: a record's identity is the event package's EpisodeID, DecisionID or AttemptID, no second identity is declared here, and a record type is never read as an event type
//   - shape pins: the compile-time assertions pin every declared shape and carry no logic of their own
package rl

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// SchemaVersion names the record schema a line was written with. A reader selects
// its parse path from this value, and a version it does not recognise is reported
// as an unknown version rather than read under a known one.
//
// The current record schema is versioned on its own: its wire shape evolves
// independently and is never bound to the shape of a legacy capture dump.
type SchemaVersion string

const (
	// SchemaCurrent is the record schema version this package writes: the
	// versioned episode, decision and attempt records. A line written under it
	// carries exactly one record kind and no legacy capture field, and a reader
	// that does not know this value refuses the line instead of reading it as
	// another version.
	SchemaCurrent SchemaVersion = "tagent.record.v1"
	// SchemaLegacyV2 names the legacy capture v2 record schema (its own
	// schema_version 2): it is read through the legacy import only and is never
	// written back.
	SchemaLegacyV2 SchemaVersion = "capture.v2"
	// SchemaLegacyV1 names the legacy v1 trajectory dump schema: it is read
	// through the legacy import only and is never written back.
	SchemaLegacyV1 SchemaVersion = "trajectory.v1"
	// SchemaUnknown names a line whose schema version is not recognised. It is
	// recorded explicitly on such a line and is never reported as a known one.
	SchemaUnknown SchemaVersion = "unknown"
)

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

// lineageNames is the explicit wire spelling of each lineage state: a record
// line names the state in words, so an unknown lineage is never read as a bare
// zero.
var lineageNames = map[Lineage]string{
	LineageUnknown: "unknown",
	LineageKnown:   "known",
}

// MarshalJSON writes one lineage as its explicit wire name.
func (l Lineage) MarshalJSON() ([]byte, error) {
	name, ok := lineageNames[l]
	if !ok {
		return nil, errUnknownState("lineage", int(l))
	}
	return json.Marshal(name)
}

// UnmarshalJSON reads one lineage from its explicit wire name; a name this
// package does not know is refused rather than read as the zero value.
func (l *Lineage) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	for state, known := range lineageNames {
		if known == name {
			*l = state
			return nil
		}
	}
	return errUnknownState("lineage", name)
}

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

// logprobsNames is the explicit wire spelling of each logprobs state.
var logprobsNames = map[LogprobsState]string{
	LogprobsUnknown:     "unknown",
	LogprobsPresent:     "present",
	LogprobsUnsupported: "unsupported",
}

// MarshalJSON writes one logprobs state as its explicit wire name.
func (s LogprobsState) MarshalJSON() ([]byte, error) {
	name, ok := logprobsNames[s]
	if !ok {
		return nil, errUnknownState("logprobs state", int(s))
	}
	return json.Marshal(name)
}

// UnmarshalJSON reads one logprobs state from its explicit wire name; a name
// this package does not know is refused rather than read as the zero value.
func (s *LogprobsState) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	for state, known := range logprobsNames {
		if known == name {
			*s = state
			return nil
		}
	}
	return errUnknownState("logprobs state", name)
}

// errUnknownState reports a state value this package has no explicit name for.
func errUnknownState(what string, got any) error {
	return fmt.Errorf("rl: unknown %s %v", what, got)
}

// SnapshotRef is the readable reference to one safe request snapshot: the request
// as it stood before the request started, projected so no header, cookie or URL
// credential is persisted, readable back in full through the named read entry and
// rechecked against its digest.
//
// The reference reads content back and not merely verifies it: the messages, tool
// schemas, multimodal parts, reasoning and actual parameters of the request are
// read back through the reference, because a digest alone confirms bytes it
// cannot return and is therefore not a snapshot reference. A reference to a
// snapshot that still carries a credential is not a safe reference, and a view
// whose snapshot cannot be read back is recorded as unreadable rather than
// re-derived from the options of a finished call.
type SnapshotRef struct {
	// SnapshotID identifies the safe snapshot persisted at the request boundary,
	// before the request started.
	SnapshotID string `json:"snapshot_id,omitempty"`
	// Digest is the content digest of the persisted snapshot bytes, which a
	// reader rechecks against what it reads back.
	Digest string `json:"digest,omitempty"`
	// ReadEntry names the trusted authorization entry the snapshot is read back
	// through; the reference carries no raw credential and no unscoped path.
	ReadEntry string `json:"read_entry,omitempty"`
}

// VersionedName names one member of a request's visible set together with the
// version of the definition the request actually ran with.
//
// A name without a version never pins what the request saw: two definitions
// under one tool name are different members of a view, and a member recorded
// without a version is reported as unversioned rather than assumed to be the
// current one.
type VersionedName struct {
	// Name identifies the member: a tool name or a skill name.
	Name string `json:"name"`
	// Version is the version of that member's definition the request ran with.
	// It is empty when the member ran unversioned, which is recorded as
	// unversioned rather than assumed to be the current one.
	Version string `json:"version,omitempty"`
}

// ModelViewRef references the model view a decision was actually made on: the
// safe request snapshot the request was fixed against, the model and generation
// parameters of the request, and the prompt, skill and tool set visible to it,
// each member with the version it was read at.
//
// The type names the view; it holds no second copy of the request. The view is
// fixed before the request starts and is never reconstructed from the options of
// a finished call: the digest fields verify what a reader reads back through
// Snapshot and never stand in for it, and a visible member is pinned by name and
// version together rather than by name alone.
type ModelViewRef struct {
	// Model names the model the request was declared against.
	Model string `json:"model,omitempty"`
	// ModelVersion is the version of that model; it is empty when the model ran
	// unversioned.
	ModelVersion string `json:"model_version,omitempty"`
	// ParametersDigest is the content digest of the generation parameters the
	// request was declared with.
	ParametersDigest string `json:"parameters_digest,omitempty"`
	// PromptVersion is the version of the system prompt content the request was
	// assembled from.
	PromptVersion string `json:"prompt_version,omitempty"`
	// VisibleTools lists the tools visible to the request, each with the version
	// of its definition the request ran with.
	VisibleTools []VersionedName `json:"visible_tools,omitempty"`
	// VisibleSkills lists the skills visible to the request, each with the
	// version of its definition the request ran with.
	VisibleSkills []VersionedName `json:"visible_skills,omitempty"`
	// ContextDigest is the content digest of the assembled context.
	ContextDigest string `json:"context_digest,omitempty"`
	// Snapshot is the readable reference to the safe request snapshot the view
	// was fixed against. An empty reference is not a snapshot: a digest alone
	// confirms bytes it cannot return and is therefore not a snapshot reference.
	Snapshot SnapshotRef `json:"snapshot"`
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
	Schema SchemaVersion `json:"schema"`
	// ID identifies the episode.
	ID event.EpisodeID `json:"episode_id"`
	// ParentID names the episode this one was spawned from; it is empty for a
	// root episode and for an episode of unknown lineage.
	ParentID event.EpisodeID `json:"parent_id,omitempty"`
	// RootID names the first episode of the lineage.
	RootID event.EpisodeID `json:"root_id,omitempty"`
	// Ancestors lists the lineage from the root down to ParentID, in that order.
	Ancestors []event.EpisodeID `json:"ancestors,omitempty"`
	// Sessions lists the sessions the episode spans.
	Sessions []event.SessionID `json:"sessions,omitempty"`
	// DependsOn declares the episodes this episode shares data with, which is the
	// basis a leakage free split is drawn on.
	DependsOn []EpisodeDependency `json:"depends_on,omitempty"`
	// Lineage states how certain the recorded lineage is.
	Lineage Lineage `json:"lineage"`
	// Timestamp is the time the record was written.
	Timestamp time.Time `json:"timestamp"`
}

// DecisionRecord is one model decision as data: the input events it was reduced
// from, the model view it actually ran on and the invocation it belongs to.
type DecisionRecord struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion `json:"schema"`
	// ID identifies the decision boundary.
	ID event.DecisionID `json:"decision_id"`
	// InvocationID scopes the decision to one responsibility range.
	InvocationID event.InvocationID `json:"invocation_id"`
	// EpisodeID names the episode the decision belongs to.
	EpisodeID event.EpisodeID `json:"episode_id,omitempty"`
	// InputEvents lists the stored events that were reduced into this decision, in
	// the order they were read.
	InputEvents []event.EventID `json:"input_events,omitempty"`
	// ModelView references the view the request actually ran on.
	ModelView ModelViewRef `json:"model_view"`
	// Timestamp is the time the record was written.
	Timestamp time.Time `json:"timestamp"`
}

// AttemptRecord is one observable model attempt as data: the response it observed,
// the terminal it reached and the logprobs state of that response.
//
// A retry below the decision boundary is not a second attempt, and the response is
// the native response the attempt observed rather than a derived copy of it.
type AttemptRecord struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion `json:"schema"`
	// ID identifies the attempt.
	ID event.AttemptID `json:"attempt_id"`
	// DecisionID names the decision the attempt belongs to.
	DecisionID event.DecisionID `json:"decision_id"`
	// EpisodeID names the episode the attempt belongs to.
	EpisodeID event.EpisodeID `json:"episode_id,omitempty"`
	// ModelVersion is the version of the model that served the response, read
	// from the identity the serving version declares on it. It is empty when
	// the response names no version, which is recorded unversioned rather than
	// assumed to be the version in effect.
	ModelVersion string `json:"model_version,omitempty"`
	// Response is the native model response the attempt observed, kept as the
	// snapshot the attempt produced rather than a derived copy of it.
	Response model.Response `json:"response"`
	// Terminal is the single terminal the model reduction derived for the attempt;
	// the capture shares that terminal and derives none of its own.
	Terminal model.Kind `json:"terminal"`
	// Usage is the usage the reduction accumulated for the attempt. It is
	// recorded beside the response because the retained body may carry no usage
	// of its own while a usage tail does.
	Usage model.Usage `json:"usage"`
	// Error is the error text the attempt observed: the response error of the
	// observed body, or the error the trace carries when the body is absent. It
	// is empty for an attempt that observed no error.
	Error string `json:"error,omitempty"`
	// Logprobs states how the logprobs of the response are known.
	Logprobs LogprobsState `json:"logprobs"`
	// LogprobsSource names the provider declaration a LogprobsUnsupported state
	// rests on; it is required for that state and empty otherwise.
	LogprobsSource string `json:"logprobs_source,omitempty"`
	// Timestamp is the time the record was written.
	Timestamp time.Time `json:"timestamp"`
}
