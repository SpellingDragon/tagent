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
