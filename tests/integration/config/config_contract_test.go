// Package config holds the contract tests of the configuration declaration surface.
package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	orgconfig "github.com/SpellingDragon/tagent/config"
)

// TestConfigurationContract checks the configuration declaration contract end to end:
// - equivalent content keeps one fingerprint, and an empty document stays the zero declaration
// - invalid declarations are rejected by field name
// - defaults are applied after decoding
// - accessor results are isolated from the declaration
// - the fingerprint carries the declaration surface only
// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
func TestConfigurationContract(t *testing.T) {
	t.Run("equivalent content keeps the fingerprint", func(t *testing.T) {
		fingerprints := make([]string, 0, 4)
		for name, content := range map[string]string{
			"canonical.yaml": yamlCanonical,
			"rewritten.yaml": yamlRewritten,
			"canonical.json": jsonCanonical,
			"nokind.yaml":    yamlNoKind,
		} {
			d := loadSource(t, name, content)
			fingerprints = append(fingerprints, fingerprintOf(t, d))
		}
		for i := 1; i < len(fingerprints); i++ {
			if fingerprints[i] != fingerprints[0] {
				t.Fatalf("equivalent content produced different fingerprints: %q vs %q", fingerprints[0], fingerprints[i])
			}
		}
		for name, content := range map[string]string{
			"empty.yaml": "",
			"null.yaml":  "null\n",
			"empty.json": "{}",
		} {
			d := loadSource(t, name, content)
			if d.Entry != "" || len(d.Agents) != 0 || len(d.Refs) != 0 || d.Baseline != "" {
				t.Fatalf("%s: empty document decoded to %+v, want the zero declaration", name, d)
			}
			if fp := fingerprintOf(t, d); fp != fingerprints[0] && name == "none" {
				t.Fatalf("%s: unexpected fingerprint %q", name, fp)
			}
		}
	})

	t.Run("invalid declarations are rejected by name", func(t *testing.T) {
		cases := []struct {
			name    string
			file    string
			content string
			wants   []string
		}{
			{"unknown top-level field", "unknown_top.yaml", "entry: main\nentr: x\n", []string{"config: ", "entr: unknown field"}},
			{"unknown nested field", "unknown_nested.yaml", "agents:\n  - id: main\n    tols: []\n", []string{"agents[0].tols: unknown field"}},
			{"wrong scalar type", "wrong_scalar.yaml", "entry: 5\n", []string{"entry: expected a string"}},
			{"agents wrong type", "agents_type.yaml", "agents: main\n", []string{"agents: expected a sequence"}},
			{"missing id", "missing_id.yaml", "agents:\n  - model: m\n", []string{"agents[0].id: required field missing"}},
			{"empty id", "empty_id.yaml", "agents:\n  - id: \"\"\n", []string{"agents[0].id: required field missing"}},
			{"unknown kind", "unknown_kind.yaml", "agents:\n  - id: a\n    files:\n      - path: a.md\n        kind: styles\n", []string{"agents[0].files[0].kind: unknown kind"}},
			{"duplicate field", "dup_field.yaml", "entry: a\nentry: b\n", []string{"entry: duplicate field"}},
			{"duplicate parameter key", "dup_param.yaml", "agents:\n  - id: a\n    params:\n      t: 1\n      t: 2\n", []string{"agents[0].params.t: duplicate field"}},
			{"trailing yaml document", "trail.yaml", "entry: main\n---\nentry: other\n", []string{"trailing document"}},
			{"trailing json content", "trail.json", "{\"entry\":\"main\"} {\"x\":1}", []string{"trailing content"}},
			{"yaml syntax error", "syntax.yaml", "entry: [unclosed\n", []string{"yaml:"}},
			{"json syntax error", "syntax.json", "{\"entry\":", []string{"json:"}},
			{"unsupported format", "decl.toml", "entry = \"main\"\n", []string{"unsupported source format"}},
			{"non-string key", "int_key.yaml", "1: x\n", []string{"expected a string key"}},
			{"json unknown field", "unknown.json", "{\"entr\":\"main\"}", []string{"entr: unknown field"}},
			{"tool missing name", "tool_name.yaml", "agents:\n  - id: a\n    tools:\n      - params: {}\n", []string{"agents[0].tools[0].name: required field missing"}},
			{"file missing path", "file_path.yaml", "agents:\n  - id: a\n    files:\n      - kind: prompt\n", []string{"agents[0].files[0].path: required field missing"}},
			{"non-number parameter", "param_type.yaml", "agents:\n  - id: a\n    params:\n      temperature: hot\n", []string{"agents[0].params.temperature: expected a number"}},
			{"non-string tool parameter", "tool_param.yaml", "agents:\n  - id: a\n    tools:\n      - name: exec\n        params:\n          top_k: 4\n", []string{"agents[0].tools[0].params.top_k: expected a string"}},
			{"agent not a mapping", "agent_map.yaml", "agents:\n  - main\n", []string{"agents[0]: expected a mapping"}},
			{"document not a mapping", "root.yaml", "main\n", []string{"expected a mapping"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				path := writeSource(t, tc.file, tc.content)
				d, err := orgconfig.Load(context.Background(), path)
				if err == nil {
					t.Fatalf("load %s: accepted declaration %+v, want rejection", tc.file, d)
				}
				for _, want := range tc.wants {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("load %s: error %q does not name %q", tc.file, err, want)
					}
				}
			})
		}
	})

	t.Run("defaults are applied after decoding", func(t *testing.T) {
		d := loadSource(t, "defaults.yaml", yamlNoKind)
		files := d.AgentSpecs()[0].Files
		if files[0].Kind != orgconfig.FileKindPrompt {
			t.Fatalf("omitted kind decoded to %q, want %q", files[0].Kind, orgconfig.FileKindPrompt)
		}
		if files[1].Kind != orgconfig.FileKindSkill {
			t.Fatalf("explicit kind decoded to %q, want %q", files[1].Kind, orgconfig.FileKindSkill)
		}
		if fingerprintOf(t, d) != fingerprintOf(t, loadSource(t, "explicit.yaml", yamlCanonical)) {
			t.Fatal("omitted kind and explicit prompt kind produced different fingerprints")
		}
	})

	t.Run("accessor results are isolated", func(t *testing.T) {
		d := loadSource(t, "decl.yaml", yamlCanonical)
		want := loadSource(t, "decl.yaml", yamlCanonical)

		specs := d.AgentSpecs()
		specs[0].ID = "hacked"
		specs[0].Params["temperature"] = 9
		specs[0].Tools[0].Params["workspace"] = "elsewhere"
		specs[0].Tools[1].Endpoint = "elsewhere"
		specs[0].Files[0].Kind = orgconfig.FileKindSkill
		specs[0].Plugins[0] = "hacked"

		refs := d.ToolRefs()
		refs[0].Name = "hacked"
		refs[0].Params["top_k"] = "9"
		refs[0].Endpoint = "elsewhere"

		raw := d.RawDocument()
		raw["entry"] = "hacked"
		raw["agents"].([]any)[0].(map[string]any)["id"] = "hacked"

		clone := d.Clone()
		clone.Entry = "hacked"
		clone.Agents[0].Params["temperature"] = 9
		clone.Raw["entry"] = "hacked"

		if !reflect.DeepEqual(d, want) {
			t.Fatalf("accessor mutation reached the declaration: got %+v, want %+v", d, want)
		}
		if fp1, fp2 := fingerprintOf(t, d), fingerprintOf(t, d); fp1 != fp2 {
			t.Fatalf("fingerprint changed the declaration: %q vs %q", fp1, fp2)
		}
		if !reflect.DeepEqual(d, want) {
			t.Fatal("fingerprint modified its input")
		}

		d.Raw["entry"] = "hacked"
		d.Raw["agents"].([]any)[0].(map[string]any)["id"] = "hacked"
		if d.Agents[0].ID != "main" || d.Entry != "main" {
			t.Fatalf("raw document mutation reached the declaration: entry %q, agent %q", d.Entry, d.Agents[0].ID)
		}
	})

	t.Run("fingerprint carries the declaration surface only", func(t *testing.T) {
		d := loadSource(t, "decl.yaml", yamlCanonical)
		assertSurface(t, reflect.ValueOf(*d), "Declaration")

		base := fingerprintOf(t, d)
		other := d.Clone()
		other.Raw = map[string]any{"entry": "other", "extra": []any{1.5, true, nil}}
		if fp := fingerprintOf(t, other); fp != base {
			t.Fatalf("raw document contributed to the fingerprint: %q vs %q", base, fp)
		}
		bare := d.Clone()
		bare.Raw = nil
		if fp := fingerprintOf(t, bare); fp != base {
			t.Fatalf("raw document contributed to the fingerprint: %q vs %q", base, fp)
		}
	})
}

// assertSurface walks one declaration value and fails on any kind outside the
// plain declaration surface.
func assertSurface(t *testing.T, v reflect.Value, path string) {
	t.Helper()
	switch v.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			assertSurface(t, v.Field(i), path+"."+v.Type().Field(i).Name)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			assertSurface(t, v.Index(i), fmt.Sprintf("%s[%d]", path, i))
		}
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			t.Fatalf("%s: map keys of kind %s are outside the declaration surface", path, v.Type().Key().Kind())
		}
		for _, key := range v.MapKeys() {
			assertSurface(t, v.MapIndex(key), path+"."+key.String())
		}
	case reflect.Interface:
		if !v.IsNil() {
			assertSurface(t, v.Elem(), path)
		}
	default:
		t.Fatalf("%s: kind %s is outside the declaration surface", path, v.Kind())
	}
}

// writeSource writes one declaration source into a fresh temporary directory and
// returns its path.
func writeSource(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// loadSource writes and loads one declaration source.
func loadSource(t *testing.T, name, content string) *orgconfig.Declaration {
	t.Helper()
	d, err := orgconfig.Load(context.Background(), writeSource(t, name, content))
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return d
}

// fingerprintOf returns the fingerprint of one declaration.
func fingerprintOf(t *testing.T, d *orgconfig.Declaration) string {
	t.Helper()
	fp, err := orgconfig.Fingerprint(*d)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	return fp
}

// yamlCanonical is the reference declaration written in YAML.
const yamlCanonical = `entry: main
baseline: v1
agents:
  - id: main
    model: glm-4-flash
    task_policy: default
    plugins:
      - risk
      - view
    params:
      temperature: 0.2
      max_tokens: 4096
    tools:
      - name: exec
        params:
          workspace: /tmp/ws
      - name: knowledge
        endpoint: https://remote.example/knowledge
    files:
      - path: prompts/main.md
        kind: prompt
      - path: skills/search.md
        kind: skill
  - id: knowledge
    parent_id: main
    model: glm-4-flash
    files:
      - path: prompts/knowledge.md
        kind: prompt
refs:
  - name: search
    params:
      top_k: "4"
`

// yamlRewritten spells the reference declaration out with different key order,
// whitespace and flow notation.
const yamlRewritten = `# the reference declaration, spelled differently
baseline: v1
refs: [{params: {top_k: "4"}, name: search}]
agents:
- {id: main, model: glm-4-flash, task_policy: default, plugins: [risk, view],
   params: {max_tokens: 4096, temperature: 0.2},
   tools: [{name: exec, params: {workspace: "/tmp/ws"}},
     {endpoint: "https://remote.example/knowledge", name: knowledge}],
   files: [{kind: prompt, path: prompts/main.md}, {kind: skill, path: skills/search.md}]}
- {parent_id: main, id: knowledge, model: glm-4-flash,
   files: [{path: prompts/knowledge.md, kind: prompt}]}
entry: main
`

// jsonCanonical spells the reference declaration out in JSON.
const jsonCanonical = `{
  "entry": "main",
  "baseline": "v1",
  "agents": [
    {
      "id": "main",
      "model": "glm-4-flash",
      "task_policy": "default",
      "plugins": ["risk", "view"],
      "params": {"temperature": 0.2, "max_tokens": 4096},
      "tools": [
        {"name": "exec", "params": {"workspace": "/tmp/ws"}},
        {"name": "knowledge", "endpoint": "https://remote.example/knowledge"}
      ],
      "files": [
        {"path": "prompts/main.md", "kind": "prompt"},
        {"path": "skills/search.md", "kind": "skill"}
      ]
    },
    {
      "id": "knowledge",
      "parent_id": "main",
      "model": "glm-4-flash",
      "files": [{"path": "prompts/knowledge.md", "kind": "prompt"}]
    }
  ],
  "refs": [{"name": "search", "params": {"top_k": "4"}}]
}
`

// yamlNoKind spells the reference declaration out with the prompt file kinds
// omitted.
const yamlNoKind = `entry: main
baseline: v1
agents:
  - id: main
    model: glm-4-flash
    task_policy: default
    plugins:
      - risk
      - view
    params:
      temperature: 0.2
      max_tokens: 4096
    tools:
      - name: exec
        params:
          workspace: /tmp/ws
      - name: knowledge
        endpoint: https://remote.example/knowledge
    files:
      - path: prompts/main.md
      - path: skills/search.md
        kind: skill
  - id: knowledge
    parent_id: main
    model: glm-4-flash
    files:
      - path: prompts/knowledge.md
refs:
  - name: search
    params:
      top_k: "4"
`
