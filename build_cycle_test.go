package tagent

import (
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/internal/strictyaml"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/require"
)

// TestBuildAgent_ReferenceCycleDetected (implementation-hardening 6.2):
// agents referencing each other through config (A→B→A or self-reference)
// must fail with an explicit cycle error at build time — the build cache
// only dedupes COMPLETED agents, so without the path check a cycle recurses
// until the stack overflows.
func TestBuildAgent_ReferenceCycleDetected(t *testing.T) {
	cfg := Config{
		Entry: "a",
		Agents: map[string]AgentConfig{
			"a": {Tools: []ToolRef{{Kind: ToolKindAgent, AgentID: "b", Description: "b tool"}}},
			"b": {Tools: []ToolRef{{Kind: ToolKindAgent, AgentID: "a", Description: "a tool"}}},
		},
	}
	rc := &runtimeConfig{model: &factoryMockModel{}}
	loader := prompt.NewLoader("")

	_, err := buildAgent("a", cfg.Agents["a"], cfg, rc, loader, map[string]*agent.TagentAgent{}, buildModeResident)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reference cycle")
	require.True(t, strings.Contains(err.Error(), `"a"`), "error must name the cycle member")
}

// TestBuildAgent_SelfReferenceDetected: an agent that lists itself as a tool
// is a one-node cycle — same explicit error.
func TestBuildAgent_SelfReferenceDetected(t *testing.T) {
	cfg := Config{
		Entry: "solo",
		Agents: map[string]AgentConfig{
			"solo": {Tools: []ToolRef{{Kind: ToolKindAgent, AgentID: "solo", Description: "self"}}},
		},
	}
	rc := &runtimeConfig{model: &factoryMockModel{}}
	loader := prompt.NewLoader("")

	_, err := buildAgent("solo", cfg.Agents["solo"], cfg, rc, loader, map[string]*agent.TagentAgent{}, buildModeResident)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reference cycle")
}

// TestStrictDecode_RejectsUnknownField (implementation-hardening 6.1): a
// typo'd top-level key fails config loading with the unknown key named.
func TestStrictDecode_RejectsUnknownField(t *testing.T) {
	data := []byte("entry: tagent\nwroking_dir: /tmp\n")
	var cfg Config
	err := strictyaml.DecodeYAML(data, &cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "wroking_dir")
}

// TestBuildAgent_DiamondBuildsOnce (review test-blind-spot #3): A→{B,C},
// B→D, C→D — the stack is path-scoped, so the diamond is legal and D is
// built exactly once via the cache. Locks the other half of the cycle-check
// contract: a regression that stops deleting stack entries on exit would
// make this legal shape report a false cycle.
func TestBuildAgent_DiamondBuildsOnce(t *testing.T) {
	cfg := Config{
		Entry: "a",
		Agents: map[string]AgentConfig{
			"a": {Tools: []ToolRef{
				{Kind: ToolKindAgent, AgentID: "b", Description: "b"},
				{Kind: ToolKindAgent, AgentID: "c", Description: "c"},
			}},
			"b": {Tools: []ToolRef{{Kind: ToolKindAgent, AgentID: "d", Description: "d via b"}}},
			"c": {Tools: []ToolRef{{Kind: ToolKindAgent, AgentID: "d", Description: "d via c"}}},
			"d": {},
		},
	}
	rc := &runtimeConfig{model: &factoryMockModel{}}
	loader := prompt.NewLoader("")
	cache := map[string]*agent.TagentAgent{}

	_, err := buildAgent("a", cfg.Agents["a"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)
	require.Contains(t, cache, "d", "shared dependency d must be built (once) and cached")
}
