// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// benchPrompt is the head prompt the tagent runner owns; the assembly and the
// tool face come from the wechat-bot module, the instruction stays benchmark
// owned so the three parties answer the same question the same way.
const benchPrompt = `You are a coding agent working alone in an isolated workspace.
The workspace root is the file tools' base directory; relative paths resolve there.
Use the tools when the task needs them. Run shell commands only through exec_run and release them with exec_release.
When you are done, reply with exactly what the task asks for: no preamble, no summary.`

// bridgeConfig is the per-task input the bridge assembles one host from.
type bridgeConfig struct {
	workspace    string
	wechatConfig string
	promptDir    string
	relayURL     string
	apiKeyEnv    string
	model        string
}

// buildHostDocument reuses the wechat-bot action agent declaration: its tool
// face, file and exec sections stay production-owned, while the workspace,
// prompt and sampling knobs are rebound for the benchmark.
func buildHostDocument(config bridgeConfig) ([]byte, error) {
	raw, err := os.ReadFile(config.wechatConfig)
	if err != nil {
		return nil, fmt.Errorf("bench bridge: read wechat config: %w", err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("bench bridge: decode wechat config: %w", err)
	}
	agents, ok := document["agents"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("bench bridge: wechat config declares no agents")
	}
	action, ok := agents["action"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("bench bridge: wechat config declares no action agent")
	}
	action["system_prompt"] = map[string]any{"files": []any{"bench_action.md"}}
	file, _ := action["file"].(map[string]any)
	if file == nil {
		file = map[string]any{}
		action["file"] = file
	}
	file["base_dir"] = config.workspace
	action["temperature"] = 0
	action["max_tokens"] = 128000
	action["final_round"] = map[string]any{
		"max_rounds":      12,
		"budget_seconds":  280,
		"timeout_seconds": 120,
	}
	action["tools"] = benchmarkToolRefs(action["tools"])
	host := map[string]any{
		"entry":                   "action",
		"provider":                "bench",
		"model":                   config.model,
		"request_timeout_seconds": 600,
		"prompt_dir":              config.promptDir,
		"providers": map[string]any{
			"bench": map[string]any{
				"provider":     "openai",
				"api_endpoint": config.relayURL,
				"api_key_env":  config.apiKeyEnv,
			},
		},
		"agents": map[string]any{"action": action},
	}
	encoded, err := yaml.Marshal(host)
	if err != nil {
		return nil, fmt.Errorf("bench bridge: encode host document: %w", err)
	}
	return encoded, nil
}

// declaredTools lists the granted tool names of the action agent, mapping the
// exec declaration onto the two tools it grants.
func declaredTools(wechatConfig string) ([]string, error) {
	raw, err := os.ReadFile(wechatConfig)
	if err != nil {
		return nil, fmt.Errorf("bench bridge: read wechat config: %w", err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("bench bridge: decode wechat config: %w", err)
	}
	agents, _ := document["agents"].(map[string]any)
	action, _ := agents["action"].(map[string]any)
	if action == nil {
		return nil, fmt.Errorf("bench bridge: wechat config declares no action agent")
	}
	return toolNames(benchmarkToolRefs(action["tools"])), nil
}

// benchmarkToolRefs drops the MCP gateway entries (the bench host mounts no MCP
// server) and keeps the production tool references, exec included: the core
// expands one exec declaration into exec_run and exec_release.
func benchmarkToolRefs(raw any) []any {
	if raw == nil {
		return nil
	}
	refs := make([]any, 0, 8)
	entries, _ := raw.([]any)
	for _, entry := range entries {
		fields, _ := entry.(map[string]any)
		id, _ := fields["id"].(string)
		switch id {
		case "", "mcp_call", "mcp_discover":
			continue
		}
		refs = append(refs, entry)
	}
	return refs
}

// toolNames renders the tool names a list of references grants.
func toolNames(refs []any) []string {
	names := make([]string, 0, len(refs)+1)
	for _, entry := range refs {
		fields, _ := entry.(map[string]any)
		id, _ := fields["id"].(string)
		if id == "exec" {
			names = append(names, "exec_run", "exec_release")
			continue
		}
		if id != "" {
			names = append(names, id)
		}
	}
	return names
}
