package rl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// ImportGap is one thing the legacy import could not read: an unparsable row, an
// unknown schema version or a row matching no known legacy schema. Every gap is
// recorded explicitly with its line number and raw bytes, so nothing the import
// could not read is dropped or guessed at.
//
// - The import is read-only over legacy JSONL and yields three separate chains: rows already written in the current record schema, legacy rows held read-only, and the explicit gaps of everything it could not classify.
// - A legacy row is mapped into its read-only projection with its raw bytes kept; no output of this package re-encodes a legacy row.
// - Classification mirrors scripts/convert_trajectories.py (classify_capture_row): schema_version 2 is capture v2, a numeric version below 2 is the legacy v1 dump era, an unknown or non-numeric version is never guessed at, a row with no schema_version is a v1 dump only beside llm_call without call_id or run_id, and a row that does not even parse is a parse gap.
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
type ImportGap struct {
	// Line is the 1-based line number the gap was found on.
	Line int `json:"line"`
	// Reason names the gap: one of the ImportReason constants.
	Reason string `json:"reason"`
	// Detail carries the human-readable context of the gap.
	Detail string `json:"detail,omitempty"`
	// Raw keeps the raw bytes of the line as they stood.
	Raw string `json:"raw,omitempty"`
	// Err is the named parse-path error behind the gap, when one applies.
	Err error `json:"-"`
}

// ImportReasonParseError and the other gap reasons: each names one way a line became a gap instead of a record.
const (
	// ImportReasonParseError is a line that is not valid JSON.
	ImportReasonParseError = "parse_error"
	// ImportReasonUnknownSchema is a line whose declared schema is not the
	// current record schema and is never read under it.
	ImportReasonUnknownSchema = "unknown_schema"
	// ImportReasonUnknownSchemaVersion is a legacy line whose schema_version is
	// not a number or lies beyond every known version; it is never guessed at.
	ImportReasonUnknownSchemaVersion = "unknown_schema_version"
	// ImportReasonUnclassified is a line matching no known legacy schema.
	ImportReasonUnclassified = "unclassified_row"
	// ImportReasonSchemaMisuse is a line declaring the current schema over a
	// legacy capture shape.
	ImportReasonSchemaMisuse = "schema_misuse"
	// ImportReasonCurrentLineRejected is a current-schema line the parse path
	// refused.
	ImportReasonCurrentLineRejected = "current_line_rejected"
)

// LegacyRecord is one legacy row held read-only: the fields it carries kept as
// they stand, the fields outside that schema's known set named explicitly
// instead of dropped, and the lineage it can offer stated as unknown — a legacy
// row carries no episode lineage contract, so its lineage is never guessed.
//
// The record is a projection of a row the import only reads. Nothing writes it
// back in its legacy shape.
type LegacyRecord struct {
	// Schema names the legacy schema the row was classified as.
	Schema SchemaVersion `json:"schema"`
	// Line is the 1-based line number the row was read from.
	Line int `json:"line"`
	// Fields keeps every field of the row as it stood, including the ones named
	// in UnknownFields.
	Fields map[string]any `json:"fields"`
	// UnknownFields names the fields outside this legacy schema's known set,
	// sorted; they are kept in Fields and never dropped.
	UnknownFields []string `json:"unknown_fields,omitempty"`
	// Lineage states how certain the lineage of the imported row is. A legacy
	// row is always LineageUnknown: the legacy format carries no episode
	// lineage, and a session or a trace is never substituted for it.
	Lineage Lineage `json:"lineage"`
	// Raw keeps the raw bytes of the row as they stood.
	Raw string `json:"raw"`
}

// ImportResult is the three-chain result of one read-only import: the rows
// already written in the current record schema, the legacy rows held read-only
// and the explicit gaps. The chains are separate so a legacy row is never read
// as a current record and a current record is never projected into a legacy
// one.
type ImportResult struct {
	// Records holds the rows written in the current record schema, in line
	// order.
	Records []Line
	// Legacy holds the legacy rows read-only, in line order.
	Legacy []LegacyRecord
	// Unknown holds the explicit gaps, in line order.
	Unknown []ImportGap
}

// ImportFile reads one JSONL source read-only and classifies each of its lines.
// The file is opened for reading only and is never modified, rewritten or
// replaced.
func ImportFile(path string) (ImportResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return ImportResult{}, err
	}
	defer file.Close()
	return ImportLines(file)
}

// ImportLines reads one JSONL source read-only and classifies each of its lines
// into the three chains of ImportResult. Blank lines are skipped, every other
// line lands in exactly one chain, and a line the importer could not read is an
// explicit gap rather than a silent drop. The source is never written to.
func ImportLines(source io.Reader) (ImportResult, error) {
	var result ImportResult
	reader := bufio.NewReader(source)
	for lineNo := 1; ; lineNo++ {
		raw, err := reader.ReadString('\n')
		if raw == "" && err != nil {
			if err == io.EOF {
				return result, nil
			}
			return result, err
		}
		text := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
		if strings.TrimSpace(text) == "" {
			if err == io.EOF {
				return result, nil
			}
			continue
		}
		result = importLine(result, lineNo, text)
		if err == io.EOF {
			return result, nil
		}
	}
}

// importLine classifies one source line into one chain of the result and
// returns the extended result.
func importLine(result ImportResult, lineNo int, text string) ImportResult {
	var fields map[string]any
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		result.Unknown = append(result.Unknown, ImportGap{
			Line:   lineNo,
			Reason: ImportReasonParseError,
			Detail: err.Error(),
			Raw:    text,
			Err:    err,
		})
		return result
	}
	if declared, ok := fields["schema"]; ok {
		schema, isString := declared.(string)
		if !isString || SchemaVersion(schema) != SchemaCurrent {
			result.Unknown = append(result.Unknown, ImportGap{
				Line:   lineNo,
				Reason: ImportReasonUnknownSchema,
				Detail: fmt.Sprintf("declared schema %v is not the current record schema", declared),
				Raw:    text,
			})
			return result
		}
		line, err := ParseLine([]byte(text))
		if err != nil {
			reason := ImportReasonCurrentLineRejected
			if errors.Is(err, ErrSchemaMisuse) {
				reason = ImportReasonSchemaMisuse
			}
			result.Unknown = append(result.Unknown, ImportGap{
				Line:   lineNo,
				Reason: reason,
				Detail: err.Error(),
				Raw:    text,
				Err:    err,
			})
			return result
		}
		result.Records = append(result.Records, line)
		return result
	}
	schema, reason, detail, ok := classifyLegacyRow(fields)
	if !ok {
		result.Unknown = append(result.Unknown, ImportGap{
			Line:   lineNo,
			Reason: reason,
			Detail: detail,
			Raw:    text,
		})
		return result
	}
	result.Legacy = append(result.Legacy, LegacyRecord{
		Schema:        schema,
		Line:          lineNo,
		Fields:        fields,
		UnknownFields: unknownFields(fields, schema),
		Lineage:       LineageUnknown,
		Raw:           text,
	})
	return result
}

// classifyLegacyRow classifies one legacy row the way
// scripts/convert_trajectories.py does: a numeric schema_version 2 is capture
// v2, a numeric version below 2 is the v1 dump era, a numeric version beyond
// every known one and a non-numeric version are refused as unknown instead of
// guessed at, a row without schema_version is a v1 dump only when it carries
// llm_call beside neither call_id nor run_id, and anything else is
// unclassified.
func classifyLegacyRow(fields map[string]any) (schema SchemaVersion, reason, detail string, ok bool) {
	if raw, has := fields["schema_version"]; has {
		version, isNumber := raw.(float64)
		if !isNumber {
			return "", ImportReasonUnknownSchemaVersion,
				fmt.Sprintf("schema_version %v is not a number and is never guessed at", raw), false
		}
		switch {
		case version == 2:
			return SchemaLegacyV2, "", "", true
		case version < 2:
			return SchemaLegacyV1, "", "", true
		default:
			return "", ImportReasonUnknownSchemaVersion,
				fmt.Sprintf("schema_version %v lies beyond every known version and is never guessed at", version), false
		}
	}
	_, hasCall := fields["call_id"]
	_, hasRun := fields["run_id"]
	_, hasLLM := fields["llm_call"]
	if hasLLM && !hasCall && !hasRun {
		return SchemaLegacyV1, "", "", true
	}
	return "", ImportReasonUnclassified, "the row matches no known legacy schema", false
}

// legacyKnownFields is the known field set of each legacy schema, mirroring the
// fields scripts/convert_trajectories.py reads. A field outside the set is kept
// and named explicitly rather than dropped.
var legacyKnownFields = map[SchemaVersion]map[string]bool{
	SchemaLegacyV1: {
		"timestamp": true, "session_id": true, "user_id": true,
		"batch_index": true, "llm_call": true, "metadata": true,
	},
	SchemaLegacyV2: {
		"schema_version": true, "capture_scope": true, "run_id": true,
		"call_id": true, "response_id": true, "request_digest": true,
		"binding_status": true, "missing_reasons": true, "owner": true,
		"llm_call": true, "tool_calls": true, "tool_results": true,
		"metadata": true, "trace_id": true, "span_id": true,
	},
}

// unknownFields names the fields one legacy row carries outside its schema's
// known set, sorted. The fields themselves stay in the row; the names make the
// unknown ones explicit instead of silently ignored.
func unknownFields(fields map[string]any, schema SchemaVersion) []string {
	known := legacyKnownFields[schema]
	var unknown []string
	for name := range fields {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}
