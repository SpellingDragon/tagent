package tagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Agent organization-layer hot reload: fingerprint and normalized snapshot.
//
// Design: examples/wechat-bot/openspec/changes/tagent-agent-hot-reload/design.md
// (D1 org-level atomic snapshot, D2 drain-free in-flight events, D3
// normalized-canonical-subset fingerprint, D4 fail-closed to previous snapshot).
//
// Fingerprint subset (D3): only fields whose change requires rebuilding agent
// instances to take effect. Persisted-path fields (governance/reliability/
// agents.*.memory.*) are excluded on purpose — changing them at runtime cannot
// migrate resources and must go through a restart.

// reloadSnapshot（R4，resident-continuity-r2-r4 3.8）：ring 2 上一代配置
// 摘要（Rollback 数据源；换代时覆盖更早代）。orgSnapshot/builtAgent 原
// 「整代原子快照」intent 已被第六轮 fresh-eyes 证伪（常驻 loop 形态下无
// drain-free 可立）——按 roadmap D5 原案落地为 cm.runner 级 SwapExecutor
// （tagent.go 懒检查编排；本文件保留 fingerprint/检测面）。
type reloadSnapshot struct {
	fp  string
	cfg *Config
}

// short truncates a fingerprint for compact logs.
func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// orgSubset is the canonical in-memory representation of the fingerprinted
// configuration subset (D3 whitelist). Field order below is significant: it
// defines the JSON key order of the canonical form, so it is stable across
// map iteration orders.
type orgSubset struct {
	Entry string `json:"entry"`
	// Model/Provider: global defaults now participate in agent instance
	// rebuilds (5.1: sub-agents without explicit model resolve through the
	// provider registry from cfg.Provider/cfg.Model), so per the D3 criterion
	// they must force a rebuild fingerprint.
	Model     string                     `json:"model,omitempty"`
	Provider  string                     `json:"provider,omitempty"`
	PromptDir string                     `json:"prompt_dir,omitempty"`
	Providers map[string]providerSubset  `json:"providers,omitempty"`
	Agents    map[string]json.RawMessage `json:"agents"` // canonical per-agent subset
}

// providerSubset carries the provider fields that participate in rebuilding
// model instances (design D3: providers.*.api_endpoint included; top-level
// Config.APIEndpoint excluded as process-level).
type providerSubset struct {
	Provider    string `json:"provider,omitempty"`
	APIEndpoint string `json:"api_endpoint,omitempty"`
}

// computeOrgFingerprint returns the SHA-256 fingerprint of the org subset of cfg.
// The input is first reduced to the whitelist subset, then canonicalized
// (stable key order via ordered structs + sorted maps, no indentation).
// Any change inside the subset changes the fingerprint; changes outside it
// (governance.*, reliability.*, agents.*.memory.*, mcp_servers, process-level
// misc) never do.
func computeOrgFingerprint(cfg *Config) (string, error) {
	sub, err := extractOrgSubset(cfg)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(sub)
	if err != nil {
		return "", fmt.Errorf("org fingerprint: canonical marshal: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// computeMemoryFingerprint（R4，resident-continuity-r2-r4 3.1 修 🔴5）：
// agents.*.Memory 段的独立 canonical 指纹。memory 被 org 指纹白名单排除（运行
// 时不可迁移存储资源），若不先检则仅改 memory 时 org 指纹不变→静默走
// ApplyOrgParams 分支→变更不生效也不告警。懒检查先序：mtime 变→先比
// memory 指纹（命中=ERROR+须重启+return，检测可达）→再比 org 指纹。
// MemoryConfig 含 Lifecycle/Engine 指针字段——用 JSON canonical 而非 ==。
func computeMemoryFingerprint(cfg *Config) (string, error) {
	type memEntry struct {
		Name   string       `json:"name"`
		Memory MemoryConfig `json:"memory"`
	}
	entries := make([]memEntry, 0, len(cfg.Agents))
	for name, ac := range cfg.Agents {
		entries = append(entries, memEntry{Name: name, Memory: ac.Memory})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	b, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("memory fingerprint: canonical marshal: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// extractOrgSubset reduces cfg to the fingerprint whitelist. Agents are
// canonicalized individually (sorted names, per-agent subset marshal) so the
// result is byte-stable regardless of YAML map iteration order.
func extractOrgSubset(cfg *Config) (*orgSubset, error) {
	sub := &orgSubset{
		Entry:     cfg.Entry,
		Model:     cfg.Model,
		Provider:  cfg.Provider,
		PromptDir: cfg.PromptDir,
		Providers: map[string]providerSubset{},
		Agents:    map[string]json.RawMessage{},
	}
	names := make([]string, 0, len(cfg.Providers))
	for k := range cfg.Providers {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		p := cfg.Providers[k]
		sub.Providers[k] = providerSubset{Provider: p.Provider, APIEndpoint: p.APIEndpoint}
	}

	agentNames := make([]string, 0, len(cfg.Agents))
	for k := range cfg.Agents {
		agentNames = append(agentNames, k)
	}
	sort.Strings(agentNames)
	for _, k := range agentNames {
		ac := cfg.Agents[k] // copy: map index of struct is not addressable
		raw, err := canonicalAgentSubset(&ac)
		if err != nil {
			return nil, fmt.Errorf("org fingerprint agent %q: %w", k, err)
		}
		sub.Agents[k] = raw
	}
	return sub, nil
}

// agentSubset mirrors AgentConfig minus memory (D3 blacklist: swapping memory
// store instances at runtime would split history; such changes need a restart).
// NOTE: keep field list in sync with config.go AgentConfig — new org-relevant
// fields MUST be added here (checked by TestOrgFingerprint_CoversAgentConfig).
type agentSubset struct {
	Model             string       `json:"model,omitempty"`
	Provider          string       `json:"provider,omitempty"`
	PromptDir         string       `json:"prompt_dir,omitempty"`
	SystemPrompt      PromptConfig `json:"system_prompt,omitempty"`
	Tools             []ToolRef    `json:"tools,omitempty"`
	MaxToolIterations int          `json:"max_tool_iterations,omitempty"`
	MaxTokens         int          `json:"max_tokens,omitempty"`
	Temperature       float64      `json:"temperature,omitempty"`
	// CompressThreshold is intentionally EXCLUDED from the fingerprint:
	// it is hot-applicable via ApplyOrgParams (compressor atomic threshold
	// swap), so per the D3 criterion ("only fields whose change requires
	// rebuilding agent instances fingerprint") it must NOT force a rebuild.
	// The unchanged-fingerprint branch in the tagent.New reloader hot-applies it.
	KeepRecentTasks      int              `json:"keep_recent_tasks,omitempty"`
	TaskTerminalTTL      string           `json:"task_terminal_ttl,omitempty"`
	ResumeContextRounds  int              `json:"resume_context_rounds,omitempty"`
	Compress             CompressConfig   `json:"compress,omitempty"`
	ThinkingEnabled      *bool            `json:"thinking_enabled,omitempty"`
	ThinkingTokens       *int             `json:"thinking_tokens,omitempty"`
	ReasoningEffort      *string          `json:"reasoning_effort,omitempty"`
	ReasoningContentMode string           `json:"reasoning_content_mode,omitempty"`
	Meditation           MeditationConfig `json:"meditation,omitempty"`
	WorkspaceRoot        string           `json:"workspace_root,omitempty"`
	Description          string           `json:"description,omitempty"`
}

func canonicalAgentSubset(ac *AgentConfig) (json.RawMessage, error) {
	as := agentSubset{
		Model: ac.Model, Provider: ac.Provider, PromptDir: ac.PromptDir,
		SystemPrompt: ac.SystemPrompt, Tools: ac.Tools,
		MaxToolIterations: ac.MaxToolIterations, MaxTokens: ac.MaxTokens,
		Temperature:     ac.Temperature,
		KeepRecentTasks: ac.KeepRecentTasks, TaskTerminalTTL: ac.TaskTerminalTTL,
		ResumeContextRounds: ac.ResumeContextRounds, Compress: ac.Compress,
		ThinkingEnabled: ac.ThinkingEnabled, ThinkingTokens: ac.ThinkingTokens,
		ReasoningEffort: ac.ReasoningEffort, ReasoningContentMode: ac.ReasoningContentMode,
		Meditation: ac.Meditation, WorkspaceRoot: ac.WorkspaceRoot,
		Description: ac.Description,
	}
	b, err := json.Marshal(as)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}
