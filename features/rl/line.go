package rl

import (
	"encoding/json"
	"errors"
	"fmt"
)

// RecordKind names which of the record kinds one line carries.
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
	// RecordKindAnnex names a line carrying one AnnexRecord: a sample's
	// acceptance verdict, appended under the sample's identity and never
	// rewritten.
	RecordKindAnnex RecordKind = "annex"
	// RecordKindRequest names a line carrying one RequestEvidenceRecord: the
	// request evidence of one decision, landed before the request starts and
	// readable back through the reference it carries.
	RecordKindRequest RecordKind = "request"
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
//
// The annex payload is carried in an unexported field and read through Annex:
// the versioned line keeps its declared export surface, and a reader still
// reads the acceptance annex it was written with. NewAnnexLine constructs the
// line; the codec marshals the payload under the "annex" key exactly as the
// other kinds are marshaled under theirs.
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
	// annex is the acceptance annex of an annex line. It is unexported so the
	// line gains no exported field; Annex reads it.
	annex *AnnexRecord
	// request is the request evidence of a request line. It is unexported for the
	// same reason the annex is: the versioned line keeps its declared export
	// surface, and RequestEvidence reads the evidence the line carries.
	request *RequestEvidenceRecord
}

// lineWire is the encoded shape of one line: the exported envelope fields of
// Line plus the annex payload the line keeps unexported. Encoding and decoding
// through it keeps the annex on the wire without adding an exported Line field.
type lineWire struct {
	Schema   SchemaVersion          `json:"schema"`
	Kind     RecordKind             `json:"kind"`
	Episode  *EpisodeRecord         `json:"episode,omitempty"`
	Decision *DecisionRecord        `json:"decision,omitempty"`
	Attempt  *AttemptRecord         `json:"attempt,omitempty"`
	Annex    *AnnexRecord           `json:"annex,omitempty"`
	Request  *RequestEvidenceRecord `json:"request,omitempty"`
}

// MarshalJSON writes the line through its wire shape, including the annex
// payload kept in the unexported field. It is the encoding the versioned line
// always had for episode, decision and attempt lines, with the annex kind
// carried beside them.
func (l Line) MarshalJSON() ([]byte, error) {
	return json.Marshal(lineWire{
		Schema:   l.Schema,
		Kind:     l.Kind,
		Episode:  l.Episode,
		Decision: l.Decision,
		Attempt:  l.Attempt,
		Annex:    l.annex,
		Request:  l.request,
	})
}

// UnmarshalJSON reads a line through its wire shape. The annex payload enters
// the unexported field; the reader reaches it through Annex, exactly as the
// write side built it through NewAnnexLine.
func (l *Line) UnmarshalJSON(data []byte) error {
	var wire lineWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*l = Line{
		Schema:   wire.Schema,
		Kind:     wire.Kind,
		Episode:  wire.Episode,
		Decision: wire.Decision,
		Attempt:  wire.Attempt,
		annex:    wire.Annex,
		request:  wire.Request,
	}
	return nil
}

// Annex reports the acceptance annex of an annex line, or nil for a line of
// another kind. The returned record is the line's own payload: a reader takes
// it as the append-only annex that was written and never edits the line in
// place.
func (l Line) Annex() *AnnexRecord {
	return l.annex
}

// RequestEvidence reports the request evidence of a request line, or nil for a
// line of another kind. The returned record is the line's own payload: it names
// the reference the request content is read back through and holds no second
// copy of that content.
func (l Line) RequestEvidence() *RequestEvidenceRecord {
	return l.request
}

// NewRequestEvidenceLine builds the versioned line of one request evidence
// record, stamped with the schema version this package writes.
func NewRequestEvidenceLine(record RequestEvidenceRecord) Line {
	return Line{Schema: SchemaCurrent, Kind: RecordKindRequest, request: &record}
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

// NewAnnexLine builds the versioned line of one acceptance annex. The annex is
// append-only: the constructor stamps the versioned envelope and carries the
// record under the sample identity, and a later verdict is another line rather
// than an edit of this one.
func NewAnnexLine(record AnnexRecord) Line {
	return Line{Schema: SchemaCurrent, Kind: RecordKindAnnex, annex: &record}
}

// MarshalLine encodes one versioned record line. A line carrying no schema, an
// unknown schema or a kind and payload that do not agree is refused rather than
// written without its version.
//
// - An attempt line is written only in a shape that can be read back: LogprobsUnsupported without its declaration source is refused here, never persisted to be refused at the read side.
// - An annex line carries the sample identity it joins on: a line without both the attempt and the bench task identity is refused here rather than written as an annex no sample can be aggregated from.
func MarshalLine(line Line) ([]byte, error) {
	if line.Schema == "" {
		return nil, fmt.Errorf("%w: an empty schema is never written", ErrSchemaMissing)
	}
	if line.Schema != SchemaCurrent {
		return nil, fmt.Errorf("%w: %q is not the schema this package writes", ErrSchemaUnknown, line.Schema)
	}
	payloads := linePayloads(line)
	if payloads != 1 {
		return nil, fmt.Errorf("%w: a line carries exactly one record payload, this one carries %d", ErrLineShape, payloads)
	}
	switch {
	case line.Kind == RecordKindEpisode && line.Episode != nil,
		line.Kind == RecordKindDecision && line.Decision != nil,
		line.Kind == RecordKindAttempt && line.Attempt != nil,
		line.Kind == RecordKindAnnex && line.annex != nil,
		line.Kind == RecordKindRequest && line.request != nil:
	default:
		return nil, fmt.Errorf("%w: kind %q", ErrLineShape, line.Kind)
	}
	if line.Attempt != nil {
		if err := ValidateAttempt(*line.Attempt); err != nil {
			return nil, err
		}
	}
	if line.annex != nil {
		if err := ValidateAnnex(*line.annex); err != nil {
			return nil, err
		}
	}
	if line.request != nil {
		if err := ValidateRequestEvidence(*line.request); err != nil {
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
// - On the read side an annex line without the two sample identities is refused as well: the write side never persists an identity-free annex, and the reader does not accept one as the referenced annex.
func ParseLine(data []byte) (Line, error) {
	var head map[string]json.RawMessage
	if err := json.Unmarshal(data, &head); err != nil {
		return Line{}, fmt.Errorf("%w: %v", ErrLineShape, err)
	}
	raw, hasSchema := head["schema"]
	if marker := legacyMarker(head); marker != "" {
		if hasSchema {
			return Line{}, fmt.Errorf("%w: it declares a schema while carrying legacy capture field %q; legacy capture lines are not read under the current schema", ErrSchemaMisuse, marker)
		}
		return Line{}, fmt.Errorf("%w: it carries legacy capture field %q; legacy capture lines are not read under the current schema", ErrLegacySchemaLine, marker)
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
	payloads := linePayloads(line)
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
	case line.Kind == RecordKindAnnex && line.annex != nil:
		if err := ValidateAnnex(*line.annex); err != nil {
			return Line{}, err
		}
		return line, nil
	case line.Kind == RecordKindRequest && line.request != nil:
		if err := ValidateRequestEvidence(*line.request); err != nil {
			return Line{}, err
		}
		return line, nil
	default:
		return Line{}, fmt.Errorf("%w: kind %q", ErrLineShape, line.Kind)
	}
}

// linePayloads counts the record payloads one line carries: exactly one kind is
// written on a line, and the annex and request evidence payloads are counted
// beside the three record kinds even though they live in unexported fields.
func linePayloads(line Line) int {
	payloads := 0
	for _, present := range []bool{line.Episode != nil, line.Decision != nil, line.Attempt != nil, line.annex != nil, line.request != nil} {
		if present {
			payloads++
		}
	}
	return payloads
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
