// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
//
// The four domain sections the agent declaration spells out decode from one
// document, fingerprint as one stable surface, and leave no trace when absent.
package config

import (
	"context"
	"strings"
	"testing"
	"time"
)

// loadSections decodes one declaration source through the package pipeline.
func loadSections(t *testing.T, name, content string) *Declaration {
	t.Helper()
	declaration, err := decodeConfigFile(context.Background(), writeConfigSource(t, name, content))
	if err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return declaration
}

// TestRegisteredSectionsLoadOneDocument pins the end-to-end section surface.
// - every domain section reads its own declaration field out of one document
// - the full declaration fingerprints as one stable canonical entry
// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
func TestRegisteredSectionsLoadOneDocument(t *testing.T) {
	declaration := loadSections(t, "sections.yaml", sectionsCanonical)
	agent := declaration.Agents[0]

	memory := agent.Memory
	if memory.Type != "localfile" || memory.Path != "/var/lib/tagent/memory" {
		t.Fatalf("memory tier = (%q, %q), want the declared tier", memory.Type, memory.Path)
	}
	if memory.Engine.Backend != "semantic" || memory.Engine.VectorTopK != 5 {
		t.Fatalf("memory engine = %+v, want the declared semantic engine", memory.Engine)
	}
	if memory.Engine.Embedding.Provider != "openai" || memory.Engine.Embedding.Model != "text-embedding-3-small" {
		t.Fatalf("memory embedding = %+v, want the declared connection", memory.Engine.Embedding)
	}

	reflection := agent.Meditation
	if !reflection.Enabled || reflection.Interval != 30*time.Minute || reflection.MinGap != 5*time.Minute {
		t.Fatalf("meditation = %+v, want the declared cadence", reflection)
	}
	if reflection.PromptFile != "reflect.md" || len(reflection.DeliverTo) != 1 || reflection.DeliverTo[0] != "main" {
		t.Fatalf("meditation target = %+v, want the declared prompt and delivery", reflection)
	}

	files := agent.File
	if files.BaseDir != "/srv/workspace" || len(files.ReadRoots) != 1 || files.ReadRoots[0] != "/srv/workspace" {
		t.Fatalf("file section = %+v, want the declared base and read roots", files)
	}

	monitor := agent.Exec.Monitor
	if monitor.Interval != 2*time.Second || monitor.StableDuration != 3*time.Second {
		t.Fatalf("exec monitor = %+v, want the declared rhythm", monitor)
	}

	if got, want := fingerprintOf(t, *declaration), sectionsFingerprint; got != want {
		t.Fatalf("fingerprint of the four-section declaration = %q, want %q", got, want)
	}
}

// TestRegisteredSectionsLeaveNoTraceWhenAbsent pins the zero declaration.
// - an agent spelling no section keeps the bare agent's fingerprint
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
func TestRegisteredSectionsLeaveNoTraceWhenAbsent(t *testing.T) {
	bare := loadSections(t, "bare.yaml", "entry: main\nagents:\n  - id: main\n    model: m1\n")
	empty := loadSections(t, "empty.yaml", `entry: main
agents:
  - id: main
    model: m1
    memory: {}
    meditation: {}
    file: {}
    exec: {}
`)
	if got, want := fingerprintOf(t, *empty), fingerprintOf(t, *bare); got != want {
		t.Fatalf("empty section mappings moved the fingerprint: %q, want the bare %q", got, want)
	}
}

// TestRegisteredSectionsRefuseAnUnknownSectionKey pins the strict boundary.
// - a key no agent section names is refused by full field path
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
func TestRegisteredSectionsRefuseAnUnknownSectionKey(t *testing.T) {
	_, err := decodeConfigFile(context.Background(), writeConfigSource(t, "unknown.yaml", "entry: main\nagents:\n  - id: main\n    memori: {}\n"))
	if err == nil {
		t.Fatalf("decode over an unregistered section = nil error, want a refusal")
	}
	for _, want := range []string{"agents[0].memori", "unknown field"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q does not name %q", err, want)
		}
	}
}

// sectionsCanonical is one declaration naming all four agent sections.
const sectionsCanonical = `entry: main
agents:
  - id: main
    model: m1
    memory:
      type: localfile
      path: /var/lib/tagent/memory
      read_namespaces:
        - team
      engine:
        backend: semantic
        vector_top_k: 5
        embedding:
          provider: openai
          model: text-embedding-3-small
          api_key_env: OPENAI_API_KEY
    meditation:
      enabled: true
      interval: 30m
      min_gap: 5m
      prompt_file: reflect.md
      observed_namespaces:
        - team
      deliver_to:
        - main
    file:
      base_dir: /srv/workspace
      read_roots:
        - /srv/workspace
    exec:
      monitor:
        interval: 2s
        stable_duration: 3s
`

// sectionsFingerprint is the canonical fingerprint of sectionsCanonical.
const sectionsFingerprint = "7e735f1f19d6e17e83b237da784233ad4475b20aa7a8cc60f86cc1d93e09eafd"
