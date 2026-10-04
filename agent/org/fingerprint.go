// 本文件承载世代簿记：机构子集的规范化、指纹计算与热参数签名——纯函数，不持装配态。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
package org

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/config"
)

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
	Model     string                    `json:"model,omitempty"`
	Provider  string                    `json:"provider,omitempty"`
	PromptDir string                    `json:"prompt_dir,omitempty"`
	Providers map[string]providerSubset `json:"providers,omitempty"`
	// Agents canonical per-agent subset
	Agents map[string]json.RawMessage `json:"agents"`
}

// providerSubset carries the provider fields that participate in rebuilding
// model instances (design D3: providers.*.api_endpoint included; top-level
// config.Config.APIEndpoint excluded as process-level).
type providerSubset struct {
	Provider    string `json:"provider,omitempty"`
	APIEndpoint string `json:"api_endpoint,omitempty"`
}

// ComputeOrgFingerprint returns the SHA-256 fingerprint of the org subset of cfg.
// The input is first reduced to the whitelist subset, then canonicalized
// (stable key order via ordered structs + sorted maps, no indentation).
// Any change inside the subset changes the fingerprint; changes outside it
// (governance.*, reliability.*, agents.*.memory.*, mcp_servers, process-level
// misc) never do.
func ComputeOrgFingerprint(cfg *config.Config) (string, error) {
	sub, err := ExtractOrgSubset(cfg)
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

// ExtractOrgSubset reduces cfg to the fingerprint whitelist. Agents are
// canonicalized individually (sorted names, per-agent subset marshal) so the
// result is byte-stable regardless of YAML map iteration order. Each agent is
// first copied into a local: a map index is not addressable, so taking its
// address directly would not compile.
func ExtractOrgSubset(cfg *config.Config) (*orgSubset, error) {
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
		ac := cfg.Agents[k]
		raw, err := CanonicalAgentSubset(&ac)
		if err != nil {
			return nil, fmt.Errorf("org fingerprint agent %q: %w", k, err)
		}
		sub.Agents[k] = raw
	}
	return sub, nil
}

// agentSubset mirrors config.AgentConfig minus memory (D3 blacklist: swapping memory
// store instances at runtime would split history; such changes need a restart).
// NOTE: keep field list in sync with config.go config.AgentConfig — new org-relevant
// fields MUST be added here (checked by TestOrgFingerprint_CoversAgentConfig).
type agentSubset struct {
	Model             string              `json:"model,omitempty"`
	Provider          string              `json:"provider,omitempty"`
	PromptDir         string              `json:"prompt_dir,omitempty"`
	SystemPrompt      config.PromptConfig `json:"system_prompt,omitempty"`
	Tools             []config.ToolRef    `json:"tools,omitempty"`
	MaxToolIterations int                 `json:"max_tool_iterations,omitempty"`
	Temperature       float64             `json:"temperature,omitempty"`
	// KeepRecentTasks CompressThreshold / MaxTokens / KeepRecentTasks / TaskTerminalTTL /
	// TaskDefaultTTL are intentionally EXCLUDED from the fingerprint
	//: all are hot-applicable WITHOUT a
	// rebuild — turned
	// them into reads at the consumer's safe boundary (compressor per-compression
	// liveNums, task manager TTL at read, ActionTool at spawn), fed by the single
	// committed application record. So per the D3 criterion
	// ("only fields whose change requires rebuilding agent instances
	// fingerprint") they must NOT force a rebuild. The unchanged-fingerprint
	// branch in the tagent.New reloader hot-applies them.
	KeepRecentTasks      int                     `json:"-"`
	TaskTerminalTTL      string                  `json:"-"`
	ResumeContextRounds  int                     `json:"resume_context_rounds,omitempty"`
	Compress             config.CompressConfig   `json:"compress,omitempty"`
	ThinkingEnabled      *bool                   `json:"thinking_enabled,omitempty"`
	ThinkingTokens       *int                    `json:"thinking_tokens,omitempty"`
	ReasoningEffort      *string                 `json:"reasoning_effort,omitempty"`
	ReasoningContentMode string                  `json:"reasoning_content_mode,omitempty"`
	Meditation           config.MeditationConfig `json:"meditation,omitempty"`
	WorkspaceRoot        string                  `json:"workspace_root,omitempty"`
	Description          string                  `json:"description,omitempty"`
}

// CanonicalAgentSubset marshals one agent's config into the canonical form the
// org fingerprint consumes: ordered fields, sorted maps, no indentation.
func CanonicalAgentSubset(ac *config.AgentConfig) (json.RawMessage, error) {
	as := agentSubset{
		Model: ac.Model, Provider: ac.Provider, PromptDir: ac.PromptDir,
		SystemPrompt: ac.SystemPrompt, Tools: ac.Tools,
		MaxToolIterations:   ac.MaxToolIterations,
		Temperature:         ac.Temperature,
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
