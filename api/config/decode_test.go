package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// decodeConfigFile reads, decodes and normalizes one declaration source through
// the package's decoding pipeline: the same sequence a caller performs, without
// a file-door entry of its own.
func decodeConfigFile(ctx context.Context, path string) (*Declaration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	root, err := decodeSource(path, data)
	if err != nil {
		return nil, err
	}
	d, err := decodeDeclaration(path, root)
	if err != nil {
		return nil, err
	}
	applyDefaults(d)
	return d, nil
}

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
			name:     "unknown agent section",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    memori:\n      type: localfile\n",
			field:    "agents[0].memori",
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
			name:     "unknown watchdog budget key",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    final_round:\n      max_rounds: 3\n      rounds: 4\n",
			field:    "agents[0].final_round.rounds",
			reason:   "unknown field",
			wantLine: 5,
		},
		{
			name:     "watchdog budget is a scalar",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    final_round: 3\n",
			field:    "agents[0].final_round",
			reason:   "expected a mapping",
			wantLine: 3,
		},
		{
			name:     "watchdog round budget is not a number",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    final_round:\n      max_rounds: many\n",
			field:    "agents[0].final_round.max_rounds",
			reason:   "expected a number",
			wantLine: 4,
		},
		{
			name:     "watchdog round budget is negative",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    final_round:\n      max_rounds: -1\n",
			field:    "agents[0].final_round.max_rounds",
			reason:   "expected a non-negative count of rounds",
			wantLine: 4,
		},
		{
			name:     "watchdog round budget is fractional",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    final_round:\n      max_rounds: 2.5\n",
			field:    "agents[0].final_round.max_rounds",
			reason:   "expected a whole count of rounds",
			wantLine: 4,
		},
		{
			name:     "watchdog time budget is negative",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    final_round:\n      budget_seconds: -5\n",
			field:    "agents[0].final_round.budget_seconds",
			reason:   "expected a non-negative count of seconds",
			wantLine: 4,
		},
		{
			name:     "watchdog prompt is not a string",
			file:     "org.yaml",
			body:     "agents:\n  - id: main\n    final_round:\n      prompt: 3\n",
			field:    "agents[0].final_round.prompt",
			reason:   "expected a string",
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
			decl, err := decodeConfigFile(context.Background(), path)
			if err == nil {
				t.Fatalf("decodeConfigFile(%q) = %+v, want a refusal", tc.body, decl)
			}
			if decl != nil {
				t.Fatalf("decodeConfigFile(%q) returned a declaration beside the refusal: %+v", tc.body, decl)
			}
			var refused *decodeError
			if !errors.As(err, &refused) {
				t.Fatalf("decodeConfigFile(%q) error = %T (%v), want *decodeError", tc.body, err, err)
			}
			if refused.field != tc.field {
				t.Fatalf("decodeConfigFile(%q) field = %q, want %q (error: %v)", tc.body, refused.field, tc.field, err)
			}
			if !strings.Contains(refused.reason, tc.reason) {
				t.Fatalf("decodeConfigFile(%q) reason = %q, want it to name %q", tc.body, refused.reason, tc.reason)
			}
			if tc.wantLine != 0 && refused.line != tc.wantLine {
				t.Fatalf("decodeConfigFile(%q) line = %d, want %d (error: %v)", tc.body, refused.line, tc.wantLine, err)
			}
			if !strings.Contains(err.Error(), filepath.Base(path)) {
				t.Fatalf("refusal %q does not name the source %q", err, filepath.Base(path))
			}
		})
	}
}

// TestStrictDecodeReadsTheWatchdogBudget pins the accepted side of the watchdog budget.
// - every entry a declaration names reaches the spec of its own agent with its own value
// - an agent naming no budget carries the zero budget, which places no bound on its invocations
// - a budget naming one entry leaves every entry it does not name unbounded
// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
func TestStrictDecodeReadsTheWatchdogBudget(t *testing.T) {
	body := "entry: main\nagents:\n  - id: main\n    final_round:\n      max_rounds: 24\n" +
		"      budget_seconds: 3300\n      timeout_seconds: 120\n      prompt: deliver\n" +
		"  - id: helper\n" +
		"  - id: bounded\n    final_round:\n      max_rounds: 3\n"
	decl, err := decodeConfigFile(context.Background(), writeConfigSource(t, "org.yaml", body))
	if err != nil {
		t.Fatalf("decodeConfigFile over a declared watchdog budget = %v, want the budget read", err)
	}
	want := FinalRoundSpec{MaxRounds: 24, BudgetSeconds: 3300, TimeoutSeconds: 120, Prompt: "deliver"}
	if got := decl.Agents[0].FinalRound; got != want {
		t.Fatalf("entry budget = %+v, want %+v", got, want)
	}
	if got := decl.Agents[1].FinalRound; !got.isZero() {
		t.Fatalf("undeclared budget = %+v, want the zero budget", got)
	}
	partial := decl.Agents[2].FinalRound
	if partial.MaxRounds != 3 || partial.BudgetSeconds != 0 || partial.TimeoutSeconds != 0 || partial.Prompt != "" {
		t.Fatalf("partial budget = %+v, want only the entry the declaration names", partial)
	}
}

// TestStrictDecodeReadsTheSectionDeclarations pins the accepted side of the four domain sections.
// - every declared section lands in the plain field of its agent with its own value
// - an agent naming no section carries the zero section of each kind
// - a section naming one entry leaves the entries it does not name at their zero
// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
func TestStrictDecodeReadsTheSectionDeclarations(t *testing.T) {
	const body = `entry: main
agents:
  - id: main
    memory:
      type: localfile
      path: /var/lib/tagent/memory
      read_namespaces: [audit, ops]
      engine:
        backend: memory
        vector_top_k: 20
        keyword_top_k: 10
        rrf_k: 60
        embedding:
          provider: zhipu
          model: embedding-3
          api_key_env: ZAI_API_KEY
          dimensions: 512
    meditation:
      enabled: true
      interval: 30m
      min_gap: 5m
      prompt_file: prompts/meditation.md
      observed_namespaces: [team]
      deliver_to: [main]
    file:
      base_dir: /srv/workspace
      read_roots: [/srv/workspace]
    exec:
      monitor:
        interval: 2s
        stable_duration: 3s
        interactive_stable_duration: 4s
        fake_dead_duration: 5s
  - id: plain
`
	decl, err := decodeConfigFile(context.Background(), writeConfigSource(t, "org.yaml", body))
	if err != nil {
		t.Fatalf("decodeConfigFile over the four declared sections = %v, want the sections read", err)
	}
	agent := decl.Agents[0]
	if agent.Memory.Type != "localfile" || agent.Memory.Path != "/var/lib/tagent/memory" || len(agent.Memory.ReadNamespaces) != 2 {
		t.Fatalf("memory = %+v, want the declared tier and read face", agent.Memory)
	}
	engine := agent.Memory.Engine
	if engine.Backend != "memory" || engine.VectorTopK != 20 || engine.KeywordTopK != 10 || engine.RRFK != 60 {
		t.Fatalf("engine = %+v, want the declared backend and retrieval tuning", engine)
	}
	embedding := engine.Embedding
	if embedding.Provider != "zhipu" || embedding.Model != "embedding-3" ||
		embedding.APIKeyEnv != "ZAI_API_KEY" || embedding.Dimensions != 512 {
		t.Fatalf("embedding = %+v, want the declared provider connection", embedding)
	}
	meditation := agent.Meditation
	if !meditation.Enabled || meditation.Interval != 30*time.Minute || meditation.MinGap != 5*time.Minute {
		t.Fatalf("meditation cadence = %+v, want the declared switch and durations", meditation)
	}
	if meditation.PromptFile != "prompts/meditation.md" || len(meditation.ObservedNamespaces) != 1 || len(meditation.DeliverTo) != 1 {
		t.Fatalf("meditation face = %+v, want the declared prompt, observation face and target", meditation)
	}
	if agent.File.BaseDir != "/srv/workspace" || len(agent.File.ReadRoots) != 1 {
		t.Fatalf("file face = %+v, want the declared base directory and read roots", agent.File)
	}
	wantMonitor := MonitorSpec{
		Interval:                  2 * time.Second,
		StableDuration:            3 * time.Second,
		InteractiveStableDuration: 4 * time.Second,
		FakeDeadDuration:          5 * time.Second,
	}
	if agent.Exec.Monitor != wantMonitor {
		t.Fatalf("exec monitor = %+v, want %+v", agent.Exec.Monitor, wantMonitor)
	}
	plain := decl.Agents[1]
	if !plain.Memory.isZero() || !plain.Meditation.isZero() || !plain.File.isZero() || !plain.Exec.isZero() {
		t.Fatalf("undeclared sections = %+v/%+v/%+v/%+v, want the zero section of each kind",
			plain.Memory, plain.Meditation, plain.File, plain.Exec)
	}
	partial, err := decodeConfigFile(context.Background(), writeConfigSource(t, "partial.yaml", "entry: main\nagents:\n  - id: main\n    memory:\n      type: localfile\n"))
	if err != nil {
		t.Fatalf("decodeConfigFile over a partial memory section = %v, want the section read", err)
	}
	if got := partial.Agents[0].Memory; got.Type != "localfile" || got.Path != "" || len(got.ReadNamespaces) != 0 {
		t.Fatalf("partial memory = %+v, want only the entry the declaration names", got)
	}
}

// TestStrictDecodeRefusesUndeclaredSectionKeys pins the strict boundary inside every section.
// - a key a section does not name is refused by full field path and source line
// - a section value that is not a mapping is refused by type
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
func TestStrictDecodeRefusesUndeclaredSectionKeys(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		field    string
		reason   string
		wantLine int
	}{
		{
			name:     "memory key",
			body:     "entry: main\nagents:\n  - id: main\n    memory:\n      region: x\n",
			field:    "agents[0].memory.region",
			reason:   "unknown field",
			wantLine: 5,
		},
		{
			name:     "memory engine key",
			body:     "entry: main\nagents:\n  - id: main\n    memory:\n      engine:\n        embedding:\n          api_base: x\n",
			field:    "agents[0].memory.engine.embedding.api_base",
			reason:   "unknown field",
			wantLine: 7,
		},
		{
			name:     "meditation key",
			body:     "entry: main\nagents:\n  - id: main\n    meditation:\n      cadence: 30m\n",
			field:    "agents[0].meditation.cadence",
			reason:   "unknown field",
			wantLine: 5,
		},
		{
			name:     "file key",
			body:     "entry: main\nagents:\n  - id: main\n    file:\n      root: /tmp/base\n",
			field:    "agents[0].file.root",
			reason:   "unknown field",
			wantLine: 5,
		},
		{
			name:     "exec monitor key",
			body:     "entry: main\nagents:\n  - id: main\n    exec:\n      monitor:\n        poll: 1s\n",
			field:    "agents[0].exec.monitor.poll",
			reason:   "unknown field",
			wantLine: 6,
		},
		{
			name:     "section is not a mapping",
			body:     "entry: main\nagents:\n  - id: main\n    memory: 3\n",
			field:    "agents[0].memory",
			reason:   "expected a mapping",
			wantLine: 4,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfigSource(t, "org.yaml", tc.body)
			_, err := decodeConfigFile(context.Background(), path)
			if err == nil {
				t.Fatalf("decodeConfigFile(%q) = nil error, want a refusal", tc.body)
			}
			var refused *decodeError
			if !errors.As(err, &refused) {
				t.Fatalf("decodeConfigFile(%q) error = %T (%v), want *decodeError", tc.body, err, err)
			}
			if refused.field != tc.field {
				t.Fatalf("decodeConfigFile(%q) field = %q, want %q (error: %v)", tc.body, refused.field, tc.field, err)
			}
			if !strings.Contains(refused.reason, tc.reason) {
				t.Fatalf("decodeConfigFile(%q) reason = %q, want it to name %q", tc.body, refused.reason, tc.reason)
			}
			if refused.line != tc.wantLine {
				t.Fatalf("decodeConfigFile(%q) line = %d, want %d (error: %v)", tc.body, refused.line, tc.wantLine, err)
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
			decl, err := decodeConfigFile(context.Background(), writeConfigSource(t, tc.file, tc.body))
			if err != nil {
				t.Fatalf("decodeConfigFile(%q) = %v, want the zero declaration", tc.body, err)
			}
			if decl.Entry != "" || decl.Baseline != "" || decl.Agents != nil || decl.Refs != nil || decl.Raw != nil {
				t.Fatalf("decodeConfigFile(%q) = %+v, want the zero declaration with no raw document", tc.body, decl)
			}
		})
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
