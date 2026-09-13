package tagent

import (
	"testing"
)

// cfgFor builds a minimal valid Config for fingerprint tests.
func cfgFor() *Config {
	return &Config{
		Entry:     "main",
		PromptDir: "resources/prompts",
		Providers: map[string]ProviderConfig{
			"p1": {Provider: "openai", APIEndpoint: "https://api.example.com"},
		},
		Agents: map[string]AgentConfig{
			"main": {
				Model:             "gpt-x",
				CompressThreshold: 0.8,
				Tools:             []ToolRef{{Kind: "tool", ID: "recall"}},
			},
			"sub": {
				Model: "gpt-y",
			},
		},
	}
}

func TestOrgFingerprint_StableAcrossEmptyChanges(t *testing.T) {
	a, err := computeOrgFingerprint(cfgFor())
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	// Semantic no-ops at the org layer: governance dir, reliability dirs,
	// per-agent memory path, top-level APIEndpoint — none may change the fp.
	b := cfgFor()
	b.Governance.Dir = "data/gov2"
	b.Reliability.BusSpillDir = "data/bus2"
	am := cfgFor()
	am.Agents["main"] = AgentConfig{Model: "gpt-x", CompressThreshold: 0.8, Tools: []ToolRef{{Kind: "tool", ID: "recall"}}, Memory: MemoryConfig{Path: "data/mem2"}}
	c := cfgFor()
	c.APIEndpoint = "https://other.example.com"
	// compress_threshold is hot-applicable (ApplyOrgParams) → excluded from
	// the fingerprint: changing it alone must NOT force a rebuild (D3).
	act := cfgFor()
	act.Agents["main"] = AgentConfig{Model: "gpt-x", CompressThreshold: 0.5, Tools: []ToolRef{{Kind: "tool", ID: "recall"}}}

	for name, mod := range map[string]*Config{"gov": b, "mem": am, "api": c, "ct": act} {
		fp, err := computeOrgFingerprint(mod)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fp != a {
			t.Errorf("%s: excluded field changed fingerprint %s.. -> %s..", name, a[:8], fp[:8])
		}
	}
}

func TestOrgFingerprint_ChangesOnOrgFields(t *testing.T) {
	base, err := computeOrgFingerprint(cfgFor())
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	mut := []struct {
		name string
		mod  func(*Config)
	}{
		{"tools", func(c *Config) {
			ac := c.Agents["main"]
			ac.Tools = []ToolRef{{Kind: "tool", ID: "recall"}, {Kind: "tool", ID: "knowledge"}}
			c.Agents["main"] = ac
		}},
		{"agent_added", func(c *Config) { c.Agents["extra"] = AgentConfig{Model: "gpt-z"} }},
		{"agent_removed", func(c *Config) { delete(c.Agents, "sub") }},
		{"provider_endpoint", func(c *Config) {
			p := c.Providers["p1"]
			p.APIEndpoint = "https://v2.example.com"
			c.Providers["p1"] = p
		}},
		{"entry", func(c *Config) { c.Entry = "sub" }},
		{"model", func(c *Config) { ac := c.Agents["main"]; ac.Model = "gpt-w"; c.Agents["main"] = ac }},
	}
	for _, m := range mut {
		c := cfgFor()
		m.mod(c)
		fp, err := computeOrgFingerprint(c)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		if fp == base {
			t.Errorf("%s: org field changed but fingerprint did not", m.name)
		}
	}
}

func TestOrgFingerprint_CanonicalStable(t *testing.T) {
	// Two configs that differ only in YAML-level noise (agent map order is a
	// Go map here; the marshal path must sort) produce identical fingerprints.
	c1, c2 := cfgFor(), cfgFor()
	f1, err := computeOrgFingerprint(c1)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	f2, err := computeOrgFingerprint(c2)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if f1 != f2 {
		t.Errorf("canonical form unstable: %s.. vs %s..", f1[:8], f2[:8])
	}
}

// R4（resident-continuity-r2-r4 3.2）回归：memory 先序检测可达（🔴5）——
// memory 被 org 指纹白名单排除，仅改 memory 时 org 指纹不变；computeMemoryFingerprint
// 必须独立感知该变更（否则懒检查静默走 ApplyOrgParams 分支，变更不生效也不告警）。
func TestOrgFingerprint_ChangesOnGlobalModelDefaults(t *testing.T) {
	// 5.1: global provider/model now drive sub-agent instance resolution
	// (resolveGlobalDefaultModel), so they must participate in the org
	// fingerprint — otherwise yaml-only flips stay invisible to hot-reload.
	base := &Config{Entry: "main", Provider: "zhipu", Model: "glm-5.3-flash"}
	fp0, err := computeOrgFingerprint(base)
	if err != nil {
		t.Fatalf("baseline fingerprint: %v", err)
	}
	for _, mut := range []struct {
		name string
		mut  func(c *Config)
	}{
		{"model", func(c *Config) { c.Model = "glm-5.3" }},
		{"provider", func(c *Config) { c.Provider = "deepseek" }},
	} {
		c := *base
		mut.mut(&c)
		fp, err := computeOrgFingerprint(&c)
		if err != nil {
			t.Fatalf("%s: fingerprint: %v", mut.name, err)
		}
		if fp == fp0 {
			t.Errorf("%s change did NOT alter org fingerprint (fp %s)", mut.name, fp[:8])
		}
	}
}

func TestMemoryFingerprint_DetectsMemoryOnlyChanges(t *testing.T) {
	base := cfgFor()
	orgFP, err := computeOrgFingerprint(base)
	if err != nil {
		t.Fatalf("org fp: %v", err)
	}
	memFP, err := computeMemoryFingerprint(base)
	if err != nil {
		t.Fatalf("mem fp: %v", err)
	}

	// 仅改 memory 段：org 指纹必须不变（既有白名单语义），memory 指纹必须变。
	mod := cfgFor()
	mod.Agents["main"] = AgentConfig{
		Model: "gpt-x", Tools: []ToolRef{{Kind: "tool", ID: "recall"}},
		Memory: MemoryConfig{Type: "file", Path: "data/mem2"},
	}
	orgFP2, err := computeOrgFingerprint(mod)
	if err != nil {
		t.Fatalf("org fp2: %v", err)
	}
	if orgFP2 != orgFP {
		t.Errorf("memory-only change must NOT alter the org fingerprint (D3 whitelist)")
	}
	memFP2, err := computeMemoryFingerprint(mod)
	if err != nil {
		t.Fatalf("mem fp2: %v", err)
	}
	if memFP2 == memFP {
		t.Errorf("memory-only change MUST alter the memory fingerprint (R4 3.1 detection reachable)")
	}

	// 非 memory 变更不误报：org 指纹变、memory 指纹不变（先序检测零误伤）。
	orgMod := cfgFor()
	a := orgMod.Agents["main"]
	a.Model = "gpt-z"
	orgMod.Agents["main"] = a
	if m2, err := computeMemoryFingerprint(orgMod); err == nil && m2 != memFP {
		t.Errorf("non-memory change must not alter the memory fingerprint")
	}

	// canonical 稳定：map 迭代序无关。
	if m3, err := computeMemoryFingerprint(cfgFor()); err != nil || m3 != memFP {
		t.Errorf("memory fingerprint must be canonical-stable (err=%v)", err)
	}
}
