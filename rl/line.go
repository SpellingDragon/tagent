package rl

import (
	"encoding/json"
	"errors"
	"fmt"
)

// RecordKind names which of the three record kinds one line carries.
//
// - The versioned record line: one JSONL line carries its schema version and exactly one record kind, and a reader selects its parse path from the version the line declares.
// - The wire shape is this schema's own: it evolves on its own version and is never bound to the shape of a legacy capture dump, so a line carrying legacy capture fields is refused here and routed to the legacy import instead.
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
type RecordKind string

const (
	// RecordKindEpisode names a line carrying one EpisodeRecord.
	RecordKindEpisode RecordKind = "episode"
	// RecordKindDecision names a line carrying one DecisionRecord.
	RecordKindDecision RecordKind = "decision"
	// RecordKindAttempt names a line carrying one AttemptRecord.
	RecordKindAttempt RecordKind = "attempt"
)

// legacyMarkerFields are the top level fields that belong to a legacy capture
// record. A line that declares the current schema while carrying one of these
// is a legacy structure masquerading as the current schema and is refused with
// a migration route rather than read under the current parse path.
var legacyMarkerFields = []string{
	"schema_version",
	"capture_scope",
	"run_id",
	"call_id",
	"llm_call",
	"tool_calls",
	"tool_results",
	"missing_reasons",
	"binding_status",
}

// ErrSchemaMissing and the other named errors of the line parse path: each names the way a line was
// refused, so a caller can route the line rather than guess at it.
var (
	// ErrSchemaMissing reports a line that carries no schema version at all.
	ErrSchemaMissing = errors.New("rl: the record line carries no schema version")
	// ErrSchemaUnknown reports a line whose schema version this package does not
	// recognise; the line is never read under a known version.
	ErrSchemaUnknown = errors.New("rl: the record line carries an unknown schema version")
	// ErrLegacySchemaLine reports a legacy line offered to the current parse
	// path; the legacy import is its only read path.
	ErrLegacySchemaLine = errors.New("rl: the record line carries a legacy schema")
	// ErrSchemaMisuse reports a line that declares the current schema while
	// carrying legacy capture fields.
	ErrSchemaMisuse = errors.New("rl: the record line declares the current schema over a legacy capture shape")
	// ErrLineShape reports a line whose kind and payload do not agree.
	ErrLineShape = errors.New("rl: the record line kind and payload do not agree")
)

// Line is one versioned record line: the schema it was written with, the kind
// it carries and exactly one record payload.
type Line struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion `json:"schema"`
	// Kind names the record kind the line carries.
	Kind RecordKind `json:"kind"`
	// Episode is the episode record of an episode line.
	Episode *EpisodeRecord `json:"episode,omitempty"`
	// Decision is the decision record of a decision line.
	Decision *DecisionRecord `json:"decision,omitempty"`
	// Attempt is the attempt record of an attempt line.
	Attempt *AttemptRecord `json:"attempt,omitempty"`
}

// NewEpisodeLine builds the versioned line of one episode record, stamped with
// the schema version this package writes.
func NewEpisodeLine(record EpisodeRecord) Line {
	record.Schema = SchemaCurrent
	return Line{Schema: SchemaCurrent, Kind: RecordKindEpisode, Episode: &record}
}

// NewDecisionLine builds the versioned line of one decision record, stamped
// with the schema version this package writes.
func NewDecisionLine(record DecisionRecord) Line {
	record.Schema = SchemaCurrent
	return Line{Schema: SchemaCurrent, Kind: RecordKindDecision, Decision: &record}
}

// NewAttemptLine builds the versioned line of one attempt record, stamped with
// the schema version this package writes.
func NewAttemptLine(record AttemptRecord) Line {
	record.Schema = SchemaCurrent
	return Line{Schema: SchemaCurrent, Kind: RecordKindAttempt, Attempt: &record}
}

// MarshalLine encodes one versioned record line. A line carrying no schema, an
// unknown schema or a kind and payload that do not agree is refused rather than
// written without its version.
//
// - An attempt line is written only in a shape that can be read back: LogprobsUnsupported without its declaration source is refused here, never persisted to be refused at the read side.
func MarshalLine(line Line) ([]byte, error) {
	if line.Schema == "" {
		return nil, fmt.Errorf("%w: an empty schema is never written", ErrSchemaMissing)
	}
	if line.Schema != SchemaCurrent {
		return nil, fmt.Errorf("%w: %q is not the schema this package writes", ErrSchemaUnknown, line.Schema)
	}
	payloads := 0
	for _, present := range []bool{line.Episode != nil, line.Decision != nil, line.Attempt != nil} {
		if present {
			payloads++
		}
	}
	if payloads != 1 {
		return nil, fmt.Errorf("%w: a line carries exactly one record payload, this one carries %d", ErrLineShape, payloads)
	}
	switch {
	case line.Kind == RecordKindEpisode && line.Episode != nil,
		line.Kind == RecordKindDecision && line.Decision != nil,
		line.Kind == RecordKindAttempt && line.Attempt != nil:
	default:
		return nil, fmt.Errorf("%w: kind %q", ErrLineShape, line.Kind)
	}
	if line.Attempt != nil {
		if err := ValidateAttempt(*line.Attempt); err != nil {
			return nil, err
		}
	}
	data, err := json.Marshal(line)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ParseLine decodes one versioned record line: it reads the declared schema
// first and selects the parse path from that value alone. A legacy line is
// refused with its migration route (the legacy import reads it), a line that
// declares the current schema over legacy capture fields is refused as a
// masquerade, and an unknown version is refused as unknown rather than read
// under a known one.
//
// - On the read side an attempt line carrying a declaration-free unsupported logprobs state is refused as well: the wire is never trusted to hold a shape the write side would have refused.
func ParseLine(data []byte) (Line, error) {
	var head map[string]json.RawMessage
	if err := json.Unmarshal(data, &head); err != nil {
		return Line{}, fmt.Errorf("%w: %v", ErrLineShape, err)
	}
	raw, hasSchema := head["schema"]
	if marker := legacyMarker(head); marker != "" {
		if hasSchema {
			return Line{}, fmt.Errorf("%w: it declares a schema while carrying legacy capture field %q; route it through ImportLines instead", ErrSchemaMisuse, marker)
		}
		return Line{}, fmt.Errorf("%w: it carries legacy capture field %q; read it through ImportLines instead", ErrLegacySchemaLine, marker)
	}
	if !hasSchema {
		return Line{}, ErrSchemaMissing
	}
	var schema SchemaVersion
	if err := json.Unmarshal(raw, &schema); err != nil || schema == "" {
		return Line{}, fmt.Errorf("%w: %s", ErrSchemaUnknown, raw)
	}
	if schema != SchemaCurrent {
		return Line{}, fmt.Errorf("%w: %q", ErrSchemaUnknown, schema)
	}
	var line Line
	if err := json.Unmarshal(data, &line); err != nil {
		return Line{}, fmt.Errorf("%w: %v", ErrLineShape, err)
	}
	payloads := 0
	for _, present := range []bool{line.Episode != nil, line.Decision != nil, line.Attempt != nil} {
		if present {
			payloads++
		}
	}
	if payloads != 1 {
		return Line{}, fmt.Errorf("%w: a line carries exactly one record payload, this one carries %d", ErrLineShape, payloads)
	}
	switch {
	case line.Kind == RecordKindEpisode && line.Episode != nil,
		line.Kind == RecordKindDecision && line.Decision != nil:
		return line, nil
	case line.Kind == RecordKindAttempt && line.Attempt != nil:
		if err := ValidateAttempt(*line.Attempt); err != nil {
			return Line{}, err
		}
		return line, nil
	default:
		return Line{}, fmt.Errorf("%w: kind %q", ErrLineShape, line.Kind)
	}
}

// legacyMarker reports the first legacy capture field the line head carries,
// or an empty string when it carries none.
func legacyMarker(head map[string]json.RawMessage) string {
	for _, field := range legacyMarkerFields {
		if _, ok := head[field]; ok {
			return field
		}
	}
	return ""
}
