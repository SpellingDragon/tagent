package tagent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// introduce-durable-workflow-engine §2.1/§2.2 契约测：候选快照私有性与
// 组织指纹覆盖面。断言对象是「换代所用配置真的是新配置」与「执行配置变更
// 必然换代」，不是日志编号。

// populatedAgentConfig sets every AgentConfig field to a distinct non-zero
// value so the fingerprint subset can be audited field by field.
func populatedAgentConfig() AgentConfig {
	enable := true
	effort := "high"
	tokens := 4096
	return AgentConfig{
		Model: "m1", Provider: "p1", PromptDir: "pd",
		SystemPrompt:         PromptConfig{Inline: "sp"},
		Memory:               MemoryConfig{Type: "memory"},
		Tools:                []ToolRef{{Kind: ToolKindAgent, AgentID: "sub", Description: "d", EventParams: []string{"event_key"}, ExtraParams: []ExtraParam{{Name: "action", Type: "string"}}, Async: &enable}},
		MaxToolIterations:    7,
		MaxTokens:            8000,
		Temperature:          0.5,
		CompressThreshold:    0.7,
		KeepRecentTasks:      3,
		TaskTerminalTTL:      "2m",
		TaskDefaultTTL:       "10m",
		ResumeContextRounds:  2,
		Compress:             CompressConfig{CompactKeysListed: 1, RecentFullCount: 2, CardMaxChars: 300},
		ThinkingEnabled:      &enable,
		ThinkingTokens:       &tokens,
		ReasoningEffort:      &effort,
		ReasoningContentMode: "raw",
		Meditation:           MeditationConfig{Enabled: true},
		WorkspaceRoot:        "/ws",
		Description:          "desc",
	}
}

// fingerprintExcludedFields are the AgentConfig fields deliberately outside the
// org fingerprint, keyed by their **YAML/JSON name** (what the subset carries).
// Each entry states WHY; a new field is only acceptable here if it is
// hot-applicable through its own contract or needs a restart.
var fingerprintExcludedFields = map[string]string{
	"memory":             "storage paths/backends cannot migrate at runtime — restart (pinned by the reloader's changedMemoryAgents/residentMemFP pre-check)",
	"max_tokens":         "hot-applicable via ApplyOrgHotParams (compressor budget)",
	"compress_threshold": "hot-applicable via ApplyOrgHotParams (compressor threshold)",
	"keep_recent_tasks":  "hot-applicable via ApplyOrgHotParams",
	"task_terminal_ttl":  "hot-applicable via the TaskManager reaper setter",
	"task_default_ttl":   "hot-applicable via the TaskManager reaper setter",
}

func jsonFieldName(f reflect.StructField) string {
	tag := strings.Split(f.Tag.Get("json"), ",")[0]
	if tag == "" {
		return f.Name
	}
	return tag
}

// TestOrgFingerprint_AuditsEveryAgentConfigField is the §2.2 guard that replaces
// the withdrawn prototype's completeness note: every AgentConfig field must be
// EITHER folded into the fingerprint subset OR named in
// fingerprintExcludedFields with a reason. Without it a newly added execution
// field (a tool switch, a model selection) would silently stop forcing a
// generation — the exact "配置改了但执行没变" defect class this change exists to
// close.
func TestOrgFingerprint_AuditsEveryAgentConfigField(t *testing.T) {
	ac := populatedAgentConfig()
	raw, err := canonicalAgentSubset(&ac)
	require.NoError(t, err)
	inSubset := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(raw, &inSubset))

	acType := reflect.TypeOf(AgentConfig{})
	for i := 0; i < acType.NumField(); i++ {
		f := acType.Field(i)
		if !f.IsExported() {
			continue
		}
		name := jsonFieldName(f)
		if reason, excluded := fingerprintExcludedFields[name]; excluded {
			require.Falsef(t, inSubset[name] != nil,
				"field %q is declared excluded (%s) yet appears in the fingerprint subset — the tables disagree", name, reason)
			continue
		}
		require.Truef(t, inSubset[name] != nil,
			"field %q is neither in the fingerprint subset nor in fingerprintExcludedFields — a change to it would not force a new generation", name)
	}

	// Reverse drift: the subset must not carry keys AgentConfig no longer has
	// (a stale key would keep fingerprinting a dead field).
	for key := range inSubset {
		found := false
		for i := 0; i < acType.NumField(); i++ {
			if f := acType.Field(i); f.IsExported() && jsonFieldName(f) == key {
				found = true
				break
			}
		}
		require.Truef(t, found, "fingerprint subset has key %q with no matching AgentConfig field", key)
	}
}

// TestOrgFingerprint_CoversFullToolRef: every ToolRef dimension is an execution
// binding (built-in id, agent target, description source, event/extra params,
// async gate, factory properties, remote A2A endpoint) — each must move the
// fingerprint. A `json:"-"` on any of them would make a real routing change
// fingerprint-invisible.
func TestOrgFingerprint_CoversFullToolRef(t *testing.T) {
	reflType := reflect.TypeOf(ToolRef{})
	for i := 0; i < reflType.NumField(); i++ {
		f := reflType.Field(i)
		if !f.IsExported() {
			continue
		}
		require.NotEqualf(t, "-", strings.Split(f.Tag.Get("json"), ",")[0],
			"ToolRef field %q is json:\"-\": it would never reach the org fingerprint", f.Name)
	}

	base := Config{Entry: "main", Agents: map[string]AgentConfig{
		"main": {Model: "m", Tools: []ToolRef{{Kind: ToolKindTool, ID: "exec", Properties: map[string]interface{}{"workspace": "/a"}}}},
	}}
	baseFP, err := computeOrgFingerprint(&base)
	require.NoError(t, err)

	// editMain replaces the (non-addressable) map value after mutating a copy.
	editMain := func(c *Config, mutate func(*AgentConfig)) {
		ac := c.Agents["main"]
		mutate(&ac)
		c.Agents["main"] = ac
	}
	mutations := []struct {
		name   string
		mutate func(*AgentConfig)
	}{
		{"tool id", func(a *AgentConfig) { a.Tools[0].ID = "read" }},
		{"tool kind", func(a *AgentConfig) { a.Tools[0].Kind = ToolKindAgent }},
		{"agent target", func(a *AgentConfig) { a.Tools[0].AgentID = "other" }},
		{"description", func(a *AgentConfig) { a.Tools[0].Description = "changed" }},
		{"description file", func(a *AgentConfig) { a.Tools[0].DescriptionFile = "d.md" }},
		{"event params", func(a *AgentConfig) { a.Tools[0].EventParams = []string{"event_key"} }},
		{"extra params", func(a *AgentConfig) { a.Tools[0].ExtraParams = []ExtraParam{{Name: "action"}} }},
		{"async gate", func(a *AgentConfig) { off := false; a.Tools[0].Async = &off }},
		{"factory props", func(a *AgentConfig) { a.Tools[0].Properties["workspace"] = "/b" }},
		{"last tool removed", func(a *AgentConfig) { a.Tools = nil }},
	}
	for _, m := range mutations {
		// Per-case deep copy through the same Clone the runtime uses: a shallow
		// struct copy would share Tools' backing array / Properties' map and let
		// one case mutate the base fixture.
		c2, cerr := base.Clone()
		require.NoError(t, cerr)
		editMain(c2, m.mutate)
		fp2, err := computeOrgFingerprint(c2)
		require.NoErrorf(t, err, "mutation %q", m.name)
		require.NotEqualf(t, baseFP, fp2,
			"mutation %q did not change the org fingerprint — the change would not force a new generation", m.name)
	}
}

// TestConfigClone_IsPrivateAndFingerprintNeutral is §2.1's snapshot integrity
// contract: a published generation owns its config, so mutating the clone can
// never reach the original (and vice versa), while the clone stays
// fingerprint-neutral (a clone must not look like a new version).
func TestConfigClone_IsPrivateAndFingerprintNeutral(t *testing.T) {
	orig := populatedAgentConfig()
	cfg := Config{
		Entry: "main", ConfigPath: "/etc/tagent.yaml", Model: "m1", Provider: "p1",
		PromptDir: "pd",
		Agents:    map[string]AgentConfig{"main": orig, "sub": {Model: "m2"}},
		Providers: map[string]ProviderConfig{"p1": {Provider: "openai", APIEndpoint: "https://a"}},
	}
	fp, err := computeOrgFingerprint(&cfg)
	require.NoError(t, err)
	mc := cfg.Agents["main"]
	mfp := agentMemoryFingerprint(&mc)

	clone, err := cfg.Clone()
	require.NoError(t, err)
	require.NotNil(t, clone)

	clonedFP, err := computeOrgFingerprint(clone)
	require.NoError(t, err)
	require.Equal(t, fp, clonedFP, "clone must be fingerprint-neutral")
	cmc := clone.Agents["main"]
	clonedMemFP := agentMemoryFingerprint(&cmc)
	require.Equal(t, mfp, clonedMemFP, "clone must be memory-fingerprint-neutral")
	require.Equal(t, cfg.ConfigPath, clone.ConfigPath, "json:\"-\" field must survive the clone")

	// Mutate every aliased container the clone could have shared.
	clone.Agents["main"] = AgentConfig{Model: "mutated"}
	clone.Agents["newagent"] = AgentConfig{Model: "x"}
	delete(clone.Providers, "p1")
	clone.Providers["added"] = ProviderConfig{Provider: "anthropic"}
	subAC := clone.Agents["sub"]
	subAC.Tools = []ToolRef{{Kind: ToolKindTool, ID: "t"}}
	clone.Agents["sub"] = subAC

	require.Equal(t, orig.Model, cfg.Agents["main"].Model, "Agents map must not be shared")
	_, stillThere := cfg.Agents["newagent"]
	require.False(t, stillThere, "Agents map keys must not be shared")
	_, ok := cfg.Providers["p1"]
	require.True(t, ok, "Providers map must not be shared")
	_, ok = cfg.Providers["added"]
	require.False(t, ok, "Providers map keys must not be shared")
	require.Nil(t, cfg.Agents["sub"].Tools, "per-agent slices must not be shared")
	require.Equal(t, fp, mustFP(t, &cfg), "the original must be untouched by clone mutations")
}

func mustFP(t *testing.T, c *Config) string {
	t.Helper()
	fp, err := computeOrgFingerprint(c)
	require.NoError(t, err)
	return fp
}

// TestOrgCoordinator_SameContentAndPublishIdentity pins §2.2's division of
// labour: the content fingerprint decides WHETHER a reload changes anything,
// the monotonic sequence is the publish identity. Rollback republishes as a new
// sequence; a superseded generation's content never counts as current again.
func TestOrgCoordinator_SameContentAndPublishIdentity(t *testing.T) {
	startup := &Config{Entry: "main", Agents: map[string]AgentConfig{"main": populatedAgentConfig()}}
	fpA := mustFP(t, startup)

	c := newOrgCoordinator()
	c.init(fpA, startup)
	require.Equal(t, fpA, c.current.fingerprint)
	require.True(t, c.sameAsCurrent(fpA), "startup content is current until a publish")
	require.False(t, c.sameAsCurrent("ffffffff"))
	require.Nil(t, c.rollbackSource(), "nothing has been published yet — no rollback source")

	// Structural change: swap reports the superseded fp and advances the sequence.
	fpB := "bbbb1111"
	oldFP, gen := c.swap(fpB, startup, nil)
	require.Equal(t, fpA, oldFP, "the log/alert line names the superseded fingerprint")
	require.Equal(t, 1, gen.seq, "first publish is generation 1")
	require.Equal(t, fpB, c.current.fingerprint)
	require.False(t, c.sameAsCurrent(fpA), "superseded content must not count as current")
	require.True(t, c.sameAsCurrent(fpB), "a same-content reload short-circuits here — no swap, no rebuild")

	// Rollback republishes the old CONTENT under a NEW sequence (D4: content hash
	// is not a publish identity) and leaves the ring pointing at the same source.
	rg := c.recordRollback(fpA, startup, nil)
	require.Equal(t, 2, rg.seq)
	require.Equal(t, fpA, c.current.fingerprint)
	require.NotNil(t, c.rollbackSource())
	require.Equal(t, fpA, c.rollbackSource().fingerprint)
	require.NotNil(t, rg.cfg, "a rollback stores the restored full config, not a nil alias")

	// Diagnostics: a rejection is visible until a publish succeeds.
	c.recordFailure(errSentinel{})
	require.EqualError(t, c.lastFailure(), "sentinel")
	_, gen3 := c.swap("cccc2222", startup, nil)
	require.Equal(t, 3, gen3.seq)
	require.Nil(t, c.lastFailure(), "a successful publish clears the rejection record")
}

type errSentinel struct{}

func (errSentinel) Error() string { return "sentinel" }
