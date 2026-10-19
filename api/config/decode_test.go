package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStrictDecodeRefusesEveryUndeclaredShape pins the startup gate: every shape the declaration surface does not name is refused before defaults run.
// - an unknown root key is refused by field path and source line
// - an unknown key inside an agent, tool reference or file reference is refused by its full field path
// - a duplicate key is refused instead of letting the last one win
// - a scalar where a mapping or a sequence is required is refused by type
// - a missing or empty required field is refused by its full field path
// - a file kind outside the declared set is refused by name
// - a source whose extension is not yaml, yml or json is refused by extension
// - a key that is not a string is refused before any declaration field is read
// - a second yaml or json document after the first is refused instead of silently ignored
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
func TestStrictDecodeRefusesEveryUndeclaredShape(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		body     string
		field    string
		reason   string
		wantLine int
	}{
		{
			name:     "unknown root key",
			file:     "org.yaml",
			body:     "entry: main\nunexpected: 1\n",
			field:    "unexpected",
			reason:   "unknown field",
			wantLine: 2,
		},
		{
			name:     "unknown agent key",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    unexpected: 1\n",
			field:    "agents[0].unexpected",
			reason:   "unknown field",
			wantLine: 3,
		},
		{
			name:     "unknown tool reference key",
			file:     "org.yaml",
			body:     "refs:\n  - name: remote\n    extra: 1\n",
			field:    "refs[0].extra",
			reason:   "unknown field",
			wantLine: 3,
		},
		{
			name:     "unknown file reference key",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    files:\n      - path: prompt.md\n        mode: 1\n",
			field:    "agents[0].files[0].mode",
			reason:   "unknown field",
			wantLine: 5,
		},
		{
			name:     "duplicate root key",
			file:     "org.yaml",
			body:     "entry: main\nentry: other\n",
			field:    "entry",
			reason:   "duplicate field",
			wantLine: 2,
		},
		{
			name:     "duplicate parameter key",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    params:\n      temperature: 1\n      temperature: 2\n",
			field:    "agents[0].params.temperature",
			reason:   "duplicate field",
			wantLine: 5,
		},
		{
			name:     "root is a sequence",
			file:     "org.yaml",
			body:     "- main\n",
			reason:   "expected a mapping",
			wantLine: 1,
		},
		{
			name:     "agents is a mapping",
			file:     "org.yaml",
			body:     "agents: {}\n",
			field:    "agents",
			reason:   "expected a sequence",
			wantLine: 1,
		},
		{
			name:     "entry is a number",
			file:     "org.yaml",
			body:     "entry: 3\n",
			field:    "entry",
			reason:   "expected a string",
			wantLine: 1,
		},
		{
			name:     "agent id is a number",
			file:     "org.yaml",
			body:     "agents:\n  - id: 3\n",
			field:    "agents[0].id",
			reason:   "expected a string",
			wantLine: 2,
		},
		{
			name:     "agent id is missing",
			file:     "org.yaml",
			body:     "agents:\n  - model: m\n",
			field:    "agents[0].id",
			reason:   "required field missing",
			wantLine: 2,
		},
		{
			name:     "agent id is empty",
			file:     "org.yaml",
			body:     "agents:\n  - id: \"\"\n",
			field:    "agents[0].id",
			reason:   "required field missing",
			wantLine: 2,
		},
		{
			name:     "tool reference name is missing",
			file:     "org.yaml",
			body:     "refs:\n  - endpoint: http://example\n",
			field:    "refs[0].name",
			reason:   "required field missing",
			wantLine: 2,
		},
		{
			name:     "file reference path is missing",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    files:\n      - kind: prompt\n",
			field:    "agents[0].files[0].path",
			reason:   "required field missing",
			wantLine: 4,
		},
		{
			name:     "file kind is outside the declared set",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    files:\n      - path: prompt.md\n        kind: binary\n",
			field:    "agents[0].files[0].kind",
			reason:   "unknown kind \"binary\"",
			wantLine: 5,
		},
		{
			name:     "parameter value is not a number",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    params:\n      temperature: hot\n",
			field:    "agents[0].params.temperature",
			reason:   "expected a number",
			wantLine: 4,
		},
		{
			name:     "reference parameter value is not a string",
			file:     "org.yaml",
			body:     "refs:\n  - name: remote\n    params:\n      mode: 1\n",
			field:    "refs[0].params.mode",
			reason:   "expected a string",
			wantLine: 4,
		},
		{
			name:   "unsupported source extension",
			file:   "org.toml",
			body:   "entry = \"main\"\n",
			reason: "unsupported source format \".toml\"",
		},
		{
			name:     "non string yaml key",
			file:     "org.yaml",
			body:     "1: main\n",
			reason:   "expected a string key",
			wantLine: 1,
		},
		{
			name:     "trailing yaml document",
			file:     "org.yaml",
			body:     "entry: main\n---\nentry: other\n",
			reason:   "trailing document after the first",
			wantLine: 2,
		},
		{
			name:     "trailing json value",
			file:     "org.json",
			body:     "{\"entry\":\"main\"}\n{\"entry\":\"other\"}\n",
			reason:   "trailing content after the first value",
			wantLine: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfigSource(t, tc.file, tc.body)
			decl, err := Load(context.Background(), path)
			if err == nil {
				t.Fatalf("Load(%q) = %+v, want a refusal", tc.body, decl)
			}
			if decl != nil {
				t.Fatalf("Load(%q) returned a declaration beside the refusal: %+v", tc.body, decl)
			}
			var refused *decodeError
			if !errors.As(err, &refused) {
				t.Fatalf("Load(%q) error = %T (%v), want *decodeError", tc.body, err, err)
			}
			if refused.field != tc.field {
				t.Fatalf("Load(%q) field = %q, want %q (error: %v)", tc.body, refused.field, tc.field, err)
			}
			if !strings.Contains(refused.reason, tc.reason) {
				t.Fatalf("Load(%q) reason = %q, want it to name %q", tc.body, refused.reason, tc.reason)
			}
			if tc.wantLine != 0 && refused.line != tc.wantLine {
				t.Fatalf("Load(%q) line = %d, want %d (error: %v)", tc.body, refused.line, tc.wantLine, err)
			}
			if !strings.Contains(err.Error(), filepath.Base(path)) {
				t.Fatalf("refusal %q does not name the source %q", err, filepath.Base(path))
			}
		})
	}
}

// TestStrictDecodeMapsEmptyDocumentsOntoTheZeroDeclaration pins that absence of content is not a shape error.
// - an empty yaml source decodes to the zero declaration with no raw document
// - an explicit null document decodes to the same zero declaration
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
func TestStrictDecodeMapsEmptyDocumentsOntoTheZeroDeclaration(t *testing.T) {
	cases := []struct {
		name string
		file string
		body string
	}{
		{name: "empty yaml", file: "org.yaml", body: ""},
		{name: "null yaml document", file: "org.yaml", body: "null\n"},
		{name: "empty json", file: "org.json", body: ""},
		{name: "null json document", file: "org.json", body: "null\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decl, err := Load(context.Background(), writeConfigSource(t, tc.file, tc.body))
			if err != nil {
				t.Fatalf("Load(%q) = %v, want the zero declaration", tc.body, err)
			}
			if decl.Entry != "" || decl.Baseline != "" || decl.Agents != nil || decl.Refs != nil || decl.Raw != nil {
				t.Fatalf("Load(%q) = %+v, want the zero declaration with no raw document", tc.body, decl)
			}
		})
	}
}

// TestLoadRefusesACancelledContextBeforeReading pins that a cancelled caller stops the load at its door.
// - a cancelled context returns its own error and no declaration
// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
func TestLoadRefusesACancelledContextBeforeReading(t *testing.T) {
	path := writeConfigSource(t, "org.yaml", "entry: main\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	decl, err := Load(ctx, path)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load over a cancelled context = %v, want context.Canceled", err)
	}
	if decl != nil {
		t.Fatalf("Load over a cancelled context returned %+v, want no declaration", decl)
	}
}

func writeConfigSource(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write source %s: %v", name, err)
	}
	return path
}
