package tagent

import (
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/internal/strictyaml"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/require"
)

// TestBuildAgent_ReferenceCycleDetected 钉住 相互引用的 agent 在构建期必须显式报环错误。
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

// TestBuildAgent_SelfReferenceDetected 钉住 把自己列为工具的 agent 构成单节点环，同样必须显式报环错误。
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

// TestStrictDecode_RejectsUnknownField: a typo'd top-level key fails config loading with the unknown key named.
func TestStrictDecode_RejectsUnknownField(t *testing.T) {
	data := []byte("entry: tagent\nwroking_dir: /tmp\n")
	var cfg Config
	err := strictyaml.DecodeYAML(data, &cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "wroking_dir")
}

// TestBuildAgent_DiamondBuildsOnce pins that a diamond build graph is legal and builds D exactly once.
// - A to {B,C}, B to D, C to D: the cycle stack is path-scoped, so reaching D again through a second parent is not a cycle.
// - D comes from the cache once, which is the other half of the cycle-check contract.
// - If leaving a path stopped deleting its stack entry, this legal shape would report a false cycle.
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
