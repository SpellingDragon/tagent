package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
)

// TestHostDocumentRefusesUnknownKey pins the startup gate: a key the host schema does not name stops the process with a locatable refusal.
// - the rejection wraps config.ErrUnknownConfigKey, so a caller separates it from a value of the wrong shape
// - it names the source file, the line and the full field path
// - the gate reaches a section nested under a declared agent, not only the root
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestHostDocumentRefusesUnknownKey(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		field string
		line  int
	}{
		{
			name:  "root key",
			body:  "entry: tagent\nlog_level: debug\n",
			field: "log_level",
			line:  2,
		},
		{
			name:  "section under a declared agent",
			body:  "entry: tagent\nagents:\n  tagent:\n    memory:\n      type: local\n",
			field: "agents.tagent.memory",
			line:  4,
		},
		{
			name:  "key under governance",
			body:  "governance:\n  budget_window_minutes: 60\n",
			field: "governance.budget_window_minutes",
			line:  2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tagent.yaml")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
			doc, err := loadHostDocument(path)
			if err == nil {
				t.Fatalf("load %s: accepted a key outside the host schema: %+v", path, doc)
			}
			if !errors.Is(err, config.ErrUnknownConfigKey) {
				t.Fatalf("load %s: error %v does not wrap config.ErrUnknownConfigKey", path, err)
			}
			for _, want := range []string{path, tc.field + ": unknown field", ":" + strconv.Itoa(tc.line) + ":"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("load %s: error %q does not name %q", path, err, want)
				}
			}
		})
	}
}

// TestHostDocumentReadsShippedDocuments pins the accepted side: both documents this example ships decode and yield a declaration carrying every declared agent.
// - tagent.yaml and tagent.rl.yaml both decode
// - each decoded agent names a model
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestHostDocumentReadsShippedDocuments(t *testing.T) {
	for _, path := range []string{"tagent.yaml", "tagent.rl.yaml"} {
		doc, err := loadHostDocument(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		if doc.Entry == "" || len(doc.Agents) == 0 {
			t.Fatalf("load %s: decoded %+v, want an entry agent beside declared agents", path, doc)
		}
		decl, err := doc.Declaration()
		if err != nil {
			t.Fatalf("derive the declaration of %s: %v", path, err)
		}
		if len(decl.Agents) != len(doc.Agents) {
			t.Fatalf("declaration of %s carries %d agents, want %d", path, len(decl.Agents), len(doc.Agents))
		}
		for _, spec := range decl.Agents {
			if spec.Model == "" {
				t.Errorf("%s: agent %q declares no model", path, spec.ID)
			}
		}
	}
}
