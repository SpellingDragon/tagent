package config

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestFingerprintIsStableUnderDefaultsAndRepeatedLoads pins that the fingerprint names the declaration surface, not the spelling of a document.
// - loading one source twice yields equal declarations and equal fingerprints
// - a file kind spelled out and one omitted normalize onto the same fingerprint
// - source key order and map entry order do not move the fingerprint
// - a changed declared field moves the fingerprint
// - a negative zero parameter normalizes onto zero, so two spellings of zero fingerprint the same
// - computing the fingerprint leaves the declaration, including its raw document, unmodified
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintIsStableUnderDefaultsAndRepeatedLoads(t *testing.T) {
	const base = `entry: main
baseline: v1
refs:
  - name: remote
    params:
      beta: two
      alpha: one
    endpoint: http://example
agents:
  - id: main
    model: m1
    task_policy: default
    tools:
      - name: local
    plugins:
      - audit
    params:
      temperature: 0.2
      top_p: 0.9
    files:
      - path: prompt.md
`
	d1 := loadConfigDeclaration(t, base)
	d2 := loadConfigDeclaration(t, base)
	if !reflect.DeepEqual(d1, d2) {
		t.Fatalf("two loads of one source differ:\n%+v\n%+v", d1, d2)
	}
	fp1 := fingerprintOf(t, *d1)
	fp2 := fingerprintOf(t, *d2)
	if fp1 != fp2 {
		t.Fatalf("fingerprint of two equal declarations = %q and %q", fp1, fp2)
	}
	if len(fp1) != 64 {
		t.Fatalf("fingerprint %q has %d characters, want the 64 of SHA-256", fp1, len(fp1))
	}
	for _, r := range fp1 {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			t.Fatalf("fingerprint %q carries %q outside lowercase hex", fp1, r)
		}
	}

	explicit := loadConfigDeclaration(t, `entry: main
baseline: v1
refs:
  - name: remote
    params:
      beta: two
      alpha: one
    endpoint: http://example
agents:
  - id: main
    model: m1
    task_policy: default
    tools:
      - name: local
    plugins:
      - audit
    params:
      temperature: 0.2
      top_p: 0.9
    files:
      - path: prompt.md
        kind: prompt
`)
	if got := fingerprintOf(t, *explicit); got != fp1 {
		t.Fatalf("fingerprint with the default kind spelled out = %q, want %q", got, fp1)
	}

	reordered := loadConfigDeclaration(t, `baseline: v1
entry: main
agents:
  - model: m1
    id: main
    params:
      top_p: 0.9
      temperature: 0.2
    files:
      - path: prompt.md
    plugins: [audit]
    tools: [{name: local}]
    task_policy: default
refs:
  - endpoint: http://example
    params:
      alpha: one
      beta: two
    name: remote
`)
	if got := fingerprintOf(t, *reordered); got != fp1 {
		t.Fatalf("fingerprint with reordered source keys = %q, want %q", got, fp1)
	}

	changed := loadConfigDeclaration(t, `entry: main
baseline: v1
refs:
  - name: remote
    params:
      beta: two
      alpha: one
    endpoint: http://example
agents:
  - id: main
    model: m2
    task_policy: default
    tools:
      - name: local
    plugins:
      - audit
    params:
      temperature: 0.2
      top_p: 0.9
    files:
      - path: prompt.md
`)
	if got := fingerprintOf(t, *changed); got == fp1 {
		t.Fatalf("fingerprint did not move when the model changed: %q", got)
	}

	zero := *d1.Clone()
	negativeZero := *d1.Clone()
	zero.Agents[0].Params["temperature"] = 0
	negativeZero.Agents[0].Params["temperature"] = math.Copysign(0, -1)
	if got, want := fingerprintOf(t, negativeZero), fingerprintOf(t, zero); got != want {
		t.Fatalf("fingerprint of negative zero = %q, want the %q of zero", got, want)
	}

	before := d1.Clone()
	fingerprintOf(t, *d1)
	if !reflect.DeepEqual(d1, before) {
		t.Fatalf("fingerprinting changed the declaration:\nbefore: %+v\nafter:  %+v", before, d1)
	}
}

// TestFingerprintNamesTheWatchdogBudgetOnlyWhenDeclared pins that the budget moves the fingerprint only where a declaration names one.
// - a declaration naming no budget fingerprints exactly as the same declaration carrying the zero budget
// - naming any one entry of the budget moves the fingerprint
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintNamesTheWatchdogBudgetOnlyWhenDeclared(t *testing.T) {
	decl := loadConfigDeclaration(t, "entry: main\nagents:\n  - id: main\n    model: m1\n")
	bare := fingerprintOf(t, *decl)
	zeroed := *decl.Clone()
	zeroed.Agents[0].FinalRound = FinalRoundSpec{}
	if got := fingerprintOf(t, zeroed); got != bare {
		t.Fatalf("fingerprint with the zero budget spelled out = %q, want %q", got, bare)
	}
	shape, err := json.Marshal(canonicalAgent{canonicalAgentCore: canonicalAgentCore{ID: "main", FinalRound: canonicalFinalRoundOf(FinalRoundSpec{})}})
	if err != nil {
		t.Fatalf("marshal the canonical agent = %v", err)
	}
	if strings.Contains(string(shape), "final_round") {
		t.Fatalf("canonical agent of a declaration naming no budget = %s, want no budget key", shape)
	}
	for _, tc := range []struct {
		name string
		spec FinalRoundSpec
	}{
		{name: "rounds", spec: FinalRoundSpec{MaxRounds: 3}},
		{name: "time budget", spec: FinalRoundSpec{BudgetSeconds: 60}},
		{name: "closing timeout", spec: FinalRoundSpec{TimeoutSeconds: 5}},
		{name: "closing prompt", spec: FinalRoundSpec{Prompt: "deliver"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bounded := *decl.Clone()
			bounded.Agents[0].FinalRound = tc.spec
			if got := fingerprintOf(t, bounded); got == bare {
				t.Fatalf("fingerprint did not move when the declaration named %s: %q", tc.name, got)
			}
		})
	}
}

// TestFingerprintNamesSectionsOnlyWhenDeclared pins that each domain section moves the fingerprint only where a declaration names one.
// - a declaration carrying the zero section of each kind fingerprints exactly as the same declaration naming none
// - naming any entry of a section moves the fingerprint
// - two spellings of one duration normalize onto the same fingerprint
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintNamesSectionsOnlyWhenDeclared(t *testing.T) {
	decl := loadConfigDeclaration(t, "entry: main\nagents:\n  - id: main\n    model: m1\n")
	bare := fingerprintOf(t, *decl)
	zeroed := *decl.Clone()
	zeroed.Agents[0].Memory = MemorySpec{}
	zeroed.Agents[0].Meditation = MeditationSpec{}
	zeroed.Agents[0].File = FileSpec{}
	zeroed.Agents[0].Exec = ExecSpec{}
	if got := fingerprintOf(t, zeroed); got != bare {
		t.Fatalf("fingerprint with the zero sections spelled out = %q, want %q", got, bare)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*AgentSpec)
	}{
		{name: "memory tier", mutate: func(s *AgentSpec) { s.Memory.Type = "localfile" }},
		{name: "embedding connection", mutate: func(s *AgentSpec) { s.Memory.Engine.Embedding.Model = "embedding-3" }},
		{name: "meditation switch", mutate: func(s *AgentSpec) { s.Meditation.Enabled = true }},
		{name: "meditation cadence", mutate: func(s *AgentSpec) { s.Meditation.Interval = time.Minute }},
		{name: "delivery target", mutate: func(s *AgentSpec) { s.Meditation.DeliverTo = []string{"main"} }},
		{name: "base directory", mutate: func(s *AgentSpec) { s.File.BaseDir = "/tmp/base" }},
		{name: "read root", mutate: func(s *AgentSpec) { s.File.ReadRoots = []string{"/tmp/a"} }},
		{name: "monitor interval", mutate: func(s *AgentSpec) { s.Exec.Monitor.Interval = time.Second }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bound := *decl.Clone()
			tc.mutate(&bound.Agents[0])
			if got := fingerprintOf(t, bound); got == bare {
				t.Fatalf("fingerprint did not move when the declaration named %s: %q", tc.name, got)
			}
		})
	}
	spelled := loadConfigDeclaration(t, "entry: main\nagents:\n  - id: main\n    model: m1\n    exec:\n      monitor:\n        interval: 60s\n")
	compact := loadConfigDeclaration(t, "entry: main\nagents:\n  - id: main\n    model: m1\n    exec:\n      monitor:\n        interval: 1m\n")
	if got, want := fingerprintOf(t, *spelled), fingerprintOf(t, *compact); got != want {
		t.Fatalf("fingerprint of 60s = %q, want the %q of 1m", got, want)
	}
}

// TestCloneOwnsTheSectionSlices pins that a copied declaration shares no section slice with its source.
// - mutating the read namespaces, the delivery target or the read roots of a copy reaches none of the original
// 契约: docs/wiki/platform/org-hot-reload.md#config-clone
func TestCloneOwnsTheSectionSlices(t *testing.T) {
	decl := loadConfigDeclaration(t, `entry: main
agents:
  - id: main
    memory:
      read_namespaces: [audit]
    meditation:
      deliver_to: [main]
    file:
      read_roots: [/tmp/a]
`)
	clone := decl.Clone()
	clone.Agents[0].Memory.ReadNamespaces[0] = "changed"
	clone.Agents[0].Meditation.DeliverTo[0] = "changed"
	clone.Agents[0].File.ReadRoots[0] = "changed"
	agent := decl.Agents[0]
	if got := agent.Memory.ReadNamespaces[0]; got != "audit" {
		t.Fatalf("mutating a clone reached the source read face: %q", got)
	}
	if got := agent.Meditation.DeliverTo[0]; got != "main" {
		t.Fatalf("mutating a clone reached the source delivery list: %q", got)
	}
	if got := agent.File.ReadRoots[0]; got != "/tmp/a" {
		t.Fatalf("mutating a clone reached the source read roots: %q", got)
	}
}

// TestCloneOwnsEveryNestedValue pins that a copied declaration shares no map or slice with its source.
// - mutating a copy's agent, tool, plugin, file, parameter and raw values reaches none of the original
// - the accessor forms hand out copies with the same isolation
// 契约: docs/wiki/platform/org-hot-reload.md#config-clone
func TestCloneOwnsEveryNestedValue(t *testing.T) {
	const source = `entry: main
refs:
  - name: remote
    params:
      token: secret
agents:
  - id: main
    tools:
      - name: local
        params:
          mode: fast
    plugins:
      - audit
    files:
      - path: prompt.md
    params:
      temperature: 0.2
`
	original := loadConfigDeclaration(t, source)
	before := original.Clone()

	clone := original.Clone()
	clone.Entry = "other"
	clone.Refs[0].Params["token"] = "changed"
	clone.Agents[0].ID = "other"
	clone.Agents[0].Tools[0].Params["mode"] = "slow"
	clone.Agents[0].Plugins[0] = "changed"
	clone.Agents[0].Files[0].Path = "other.md"
	clone.Agents[0].Params["temperature"] = 1
	clone.Raw["entry"] = "other"
	rawAgents := clone.Raw["agents"].([]any)
	rawAgents[0].(map[string]any)["id"] = "other"

	if !reflect.DeepEqual(original, before) {
		t.Fatalf("mutating a clone reached the original:\nbefore: %+v\nafter:  %+v", before, original)
	}
	if got := original.Agents[0].Tools[0].Params["mode"]; got != "fast" {
		t.Fatalf("original tool parameter = %q, want %q", got, "fast")
	}
	if got := original.Raw["entry"]; got != "main" {
		t.Fatalf("original raw entry = %v, want %q", got, "main")
	}

	agents := original.AgentSpecs()
	agents[0].Plugins[0] = "changed"
	refs := original.ToolRefs()
	refs[0].Params["token"] = "changed"
	raw := original.RawDocument()
	raw["entry"] = "other"
	if got := original.Agents[0].Plugins[0]; got != "audit" {
		t.Fatalf("AgentSpecs handed out a shared plugin slice: %q", got)
	}
	if got := original.Refs[0].Params["token"]; got != "secret" {
		t.Fatalf("ToolRefs handed out a shared parameter map: %q", got)
	}
	if got := original.Raw["entry"]; got != "main" {
		t.Fatalf("RawDocument handed out a shared document: %v", got)
	}
}

func loadConfigDeclaration(t *testing.T, body string) *Declaration {
	t.Helper()
	decl, err := decodeConfigFile(context.Background(), writeConfigSource(t, "org.yaml", body))
	if err != nil {
		t.Fatalf("decodeConfigFile(%q) = %v, want a declaration", body, err)
	}
	return decl
}

func fingerprintOf(t *testing.T, decl Declaration) string {
	t.Helper()
	fingerprint, err := Fingerprint(decl)
	if err != nil {
		t.Fatalf("Fingerprint = %v, want a fingerprint", err)
	}
	return fingerprint
}
