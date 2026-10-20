package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
//
// Fingerprint returns the canonical organization fingerprint of d: a stable
// string derived solely from the declaration surface, namely the entry agent,
// the declared agent graph with its model selection, prompt sources and tool
// declarations, the organization references and the version baseline. Live
// objects (model handles, tool instances, connections, locks and generation
// numbers) never contribute to the fingerprint, so reloading equivalent content
// yields the same fingerprint.
//
// The fingerprint is the SHA-256 of the canonical JSON encoding of the
// declaration with defaults applied: omitted collections encode as empty ones,
// map keys encode in order, negative zero normalizes to zero and the decoded
// source document is left out. The declaration d is not modified.
func Fingerprint(d Declaration) (string, error) {
	normalized := d.Clone()
	applyDefaults(normalized)
	data, err := json.Marshal(canonicalOf(normalized))
	if err != nil {
		return "", fmt.Errorf("config: fingerprint: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalDeclaration is the declaration surface in canonical field order.
type canonicalDeclaration struct {
	Entry    string                        `json:"entry"`
	Agents   []canonicalAgent              `json:"agents"`
	Refs     []canonicalToolRef            `json:"refs"`
	Baseline string                        `json:"baseline"`
	Servers  map[string]canonicalMCPServer `json:"mcp_servers,omitempty"`
}

// canonicalMCPServer is one declared MCP server in canonical field order.
type canonicalMCPServer struct {
	Transport string `json:"transport,omitempty"`
	URL       string `json:"url"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
}

// canonicalAgentCore is the core of one agent spec in canonical field order.
type canonicalAgentCore struct {
	ID         string               `json:"id"`
	ParentID   string               `json:"parent_id"`
	Model      string               `json:"model"`
	Tools      []canonicalToolRef   `json:"tools"`
	Plugins    []string             `json:"plugins"`
	TaskPolicy string               `json:"task_policy"`
	Files      []canonicalFileRef   `json:"files"`
	Params     map[string]float64   `json:"params"`
	FinalRound *canonicalFinalRound `json:"final_round,omitempty"`
	// ReplayBudget and EnvironmentBlock are the request-context switches of the
	// agent: an agent that declares neither carries no entry of its own, so its
	// canonical form names no switch and its fingerprint stays the one the
	// declaration surface had before the switches existed.
	ReplayBudget     int  `json:"replay_budget,omitempty"`
	EnvironmentBlock bool `json:"environment_block,omitempty"`
}

// canonicalAgent is one agent spec in canonical field order: the core fields
// followed by the four domain sections, each encoded only while the agent
// declares it.
type canonicalAgent struct {
	canonicalAgentCore
	Memory     *canonicalMemory     `json:"memory,omitempty"`
	Meditation *canonicalMeditation `json:"meditation,omitempty"`
	File       *canonicalFile       `json:"file,omitempty"`
	Exec       *canonicalExec       `json:"exec,omitempty"`
}

// canonicalMemory is one memory declaration in canonical field order.
type canonicalMemory struct {
	Type           string           `json:"type"`
	Path           string           `json:"path"`
	ReadNamespaces []string         `json:"read_namespaces"`
	Engine         *canonicalEngine `json:"engine,omitempty"`
}

// canonicalEngine is one semantic engine in canonical field order.
type canonicalEngine struct {
	Backend     string              `json:"backend"`
	Embedding   *canonicalEmbedding `json:"embedding,omitempty"`
	VectorTopK  int                 `json:"vector_top_k,omitempty"`
	KeywordTopK int                 `json:"keyword_top_k,omitempty"`
	RRFK        int                 `json:"rrf_k,omitempty"`
}

// canonicalEmbedding is one embedding connection in canonical field order.
type canonicalEmbedding struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	APIKeyEnv   string `json:"api_key_env"`
	Dimensions  int    `json:"dimensions,omitempty"`
	APIEndpoint string `json:"api_endpoint,omitempty"`
}

// canonicalMeditation is one meditation declaration in canonical field order.
// The cadence is named in nanoseconds, so two spellings of one duration
// normalize onto the same fingerprint.
type canonicalMeditation struct {
	Enabled            bool     `json:"enabled,omitempty"`
	IntervalNS         int64    `json:"interval_ns,omitempty"`
	MinGapNS           int64    `json:"min_gap_ns,omitempty"`
	PromptFile         string   `json:"prompt_file,omitempty"`
	ObservedNamespaces []string `json:"observed_namespaces"`
	DeliverTo          []string `json:"deliver_to"`
}

// canonicalFile is one file face in canonical field order.
type canonicalFile struct {
	BaseDir   string   `json:"base_dir"`
	ReadRoots []string `json:"read_roots"`
}

// canonicalExec is one execution face in canonical field order.
type canonicalExec struct {
	Monitor *canonicalMonitor `json:"monitor,omitempty"`
}

// canonicalMonitor is one monitor rhythm in canonical field order.
type canonicalMonitor struct {
	IntervalNS                  int64 `json:"interval_ns,omitempty"`
	StableDurationNS            int64 `json:"stable_duration_ns,omitempty"`
	InteractiveStableDurationNS int64 `json:"interactive_stable_duration_ns,omitempty"`
	FakeDeadDurationNS          int64 `json:"fake_dead_duration_ns,omitempty"`
}

// isZero reports whether the server declaration names nothing at all.
func (s MCPServerSpec) isZero() bool {
	return s == MCPServerSpec{}
}

// canonicalFinalRound is one agent's watchdog budget in canonical field order.
// An agent that declares no bound carries none of it, so its canonical form
// names no budget and its fingerprint stays the one the declaration surface had
// before the budget existed.
type canonicalFinalRound struct {
	MaxRounds      int     `json:"max_rounds,omitempty"`
	BudgetSeconds  float64 `json:"budget_seconds,omitempty"`
	TimeoutSeconds float64 `json:"timeout_seconds,omitempty"`
	Prompt         string  `json:"prompt,omitempty"`
}

// canonicalToolRef is one tool ref in canonical field order.
type canonicalToolRef struct {
	Name     string            `json:"name"`
	Params   map[string]string `json:"params"`
	Endpoint string            `json:"endpoint"`
}

// canonicalFileRef is one file ref in canonical field order.
type canonicalFileRef struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// canonicalOf maps a normalized declaration onto its canonical form.
func canonicalOf(d *Declaration) canonicalDeclaration {
	return canonicalDeclaration{
		Entry:    d.Entry,
		Agents:   canonicalAgents(d.Agents),
		Refs:     canonicalToolRefs(d.Refs),
		Baseline: d.Baseline,
		Servers:  canonicalMCPServers(d.Servers),
	}
}

// canonicalAgents maps agent specs onto canonical agent forms in declaration
// order: the core fields first and then the four domain sections in the slot
// order the declaration surface always encoded them.
func canonicalAgents(in []AgentSpec) []canonicalAgent {
	out := make([]canonicalAgent, 0, len(in))
	for _, a := range in {
		out = append(out, canonicalAgent{
			canonicalAgentCore: canonicalAgentCore{
				ID:               a.ID,
				ParentID:         a.ParentID,
				Model:            a.Model,
				Tools:            canonicalToolRefs(a.Tools),
				Plugins:          canonicalStrings(a.Plugins),
				TaskPolicy:       a.TaskPolicy,
				Files:            canonicalFileRefs(a.Files),
				Params:           canonicalFloatMap(a.Params),
				FinalRound:       canonicalFinalRoundOf(a.FinalRound),
				ReplayBudget:     a.ReplayBudget,
				EnvironmentBlock: a.EnvironmentBlock,
			},
			Memory:     a.Memory.canonical(),
			Meditation: a.Meditation.canonical(),
			File:       a.File.canonical(),
			Exec:       a.Exec.canonical(),
		})
	}
	return out
}

// canonical maps one memory declaration onto its canonical form, or names no
// memory section at all while the declaration names no binding.
func (s MemorySpec) canonical() *canonicalMemory {
	if s.isZero() {
		return nil
	}
	return &canonicalMemory{
		Type:           s.Type,
		Path:           s.Path,
		ReadNamespaces: canonicalStrings(s.ReadNamespaces),
		Engine:         s.Engine.canonical(),
	}
}

// isZero reports whether the memory declaration names no binding at all.
func (s MemorySpec) isZero() bool {
	return s.Type == "" && s.Path == "" && len(s.ReadNamespaces) == 0 && s.Engine.isZero()
}

// canonical maps one semantic engine onto its canonical form, or names no
// engine at all while the declaration names none.
func (s EngineSpec) canonical() *canonicalEngine {
	if s.isZero() {
		return nil
	}
	return &canonicalEngine{
		Backend:     s.Backend,
		Embedding:   s.Embedding.canonical(),
		VectorTopK:  s.VectorTopK,
		KeywordTopK: s.KeywordTopK,
		RRFK:        s.RRFK,
	}
}

// isZero reports whether the engine declaration names no engine at all.
func (s EngineSpec) isZero() bool {
	return s == EngineSpec{}
}

// canonical maps one embedding connection onto its canonical form, or names no
// connection at all while the declaration names none.
func (s EmbeddingSpec) canonical() *canonicalEmbedding {
	if s.isZero() {
		return nil
	}
	return &canonicalEmbedding{
		Provider:    s.Provider,
		Model:       s.Model,
		APIKeyEnv:   s.APIKeyEnv,
		Dimensions:  s.Dimensions,
		APIEndpoint: s.APIEndpoint,
	}
}

// isZero reports whether the embedding declaration names no connection at all.
func (s EmbeddingSpec) isZero() bool {
	return s == EmbeddingSpec{}
}

// canonical maps one meditation declaration onto its canonical form, or names
// no meditation section at all while the declaration enables no reflection and
// names no field of one.
func (s MeditationSpec) canonical() *canonicalMeditation {
	if s.isZero() {
		return nil
	}
	return &canonicalMeditation{
		Enabled:            s.Enabled,
		IntervalNS:         int64(s.Interval),
		MinGapNS:           int64(s.MinGap),
		PromptFile:         s.PromptFile,
		ObservedNamespaces: canonicalStrings(s.ObservedNamespaces),
		DeliverTo:          canonicalStrings(s.DeliverTo),
	}
}

// isZero reports whether the meditation declaration enables no reflection.
func (s MeditationSpec) isZero() bool {
	return !s.Enabled && s.Interval == 0 && s.MinGap == 0 && s.PromptFile == "" &&
		len(s.ObservedNamespaces) == 0 && len(s.DeliverTo) == 0
}

// canonical maps one file face onto its canonical form, or names no file
// section at all while the declaration names neither a base directory nor a
// read root.
func (s FileSpec) canonical() *canonicalFile {
	if s.isZero() {
		return nil
	}
	return &canonicalFile{BaseDir: s.BaseDir, ReadRoots: canonicalStrings(s.ReadRoots)}
}

// isZero reports whether the file declaration names no face at all.
func (s FileSpec) isZero() bool {
	return s.BaseDir == "" && len(s.ReadRoots) == 0
}

// canonical maps one execution face onto its canonical form, or names no exec
// section at all while the declaration names no monitor rhythm.
func (s ExecSpec) canonical() *canonicalExec {
	if s.isZero() {
		return nil
	}
	return &canonicalExec{Monitor: s.Monitor.canonical()}
}

// isZero reports whether the execution face names no rhythm at all.
func (s ExecSpec) isZero() bool { return s.Monitor == MonitorSpec{} }

// canonical maps one monitor rhythm onto its canonical form, or names no rhythm
// at all while the declaration names none.
func (s MonitorSpec) canonical() *canonicalMonitor {
	if s == (MonitorSpec{}) {
		return nil
	}
	return &canonicalMonitor{
		IntervalNS:                  int64(s.Interval),
		StableDurationNS:            int64(s.StableDuration),
		InteractiveStableDurationNS: int64(s.InteractiveStableDuration),
		FakeDeadDurationNS:          int64(s.FakeDeadDuration),
	}
}

// canonicalMCPServers maps the declared servers onto their canonical forms,
// keyed by the name each is bound under.
func canonicalMCPServers(in map[string]MCPServerSpec) map[string]canonicalMCPServer {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]canonicalMCPServer, len(in))
	for name, spec := range in {
		out[name] = canonicalMCPServer{Transport: spec.Transport, URL: spec.URL, APIKeyEnv: spec.APIKeyEnv}
	}
	return out
}

// canonicalFinalRoundOf maps one watchdog budget onto its canonical form, or
// names no budget at all while the budget places no bound.
func canonicalFinalRoundOf(s FinalRoundSpec) *canonicalFinalRound {
	if s.isZero() {
		return nil
	}
	return &canonicalFinalRound{
		MaxRounds:      s.MaxRounds,
		BudgetSeconds:  canonicalFloat(s.BudgetSeconds),
		TimeoutSeconds: canonicalFloat(s.TimeoutSeconds),
		Prompt:         s.Prompt,
	}
}

// canonicalToolRefs maps tool refs onto canonical tool ref forms.
func canonicalToolRefs(in []ToolRef) []canonicalToolRef {
	out := make([]canonicalToolRef, 0, len(in))
	for _, r := range in {
		out = append(out, canonicalToolRef{
			Name:     r.Name,
			Params:   canonicalStringMap(r.Params),
			Endpoint: r.Endpoint,
		})
	}
	return out
}

// canonicalFileRefs maps file refs onto canonical file ref forms.
func canonicalFileRefs(in []FileRef) []canonicalFileRef {
	out := make([]canonicalFileRef, 0, len(in))
	for _, f := range in {
		out = append(out, canonicalFileRef{Path: f.Path, Kind: string(f.Kind)})
	}
	return out
}

// canonicalStrings maps a string slice onto its canonical form.
func canonicalStrings(in []string) []string {
	out := make([]string, 0, len(in))
	out = append(out, in...)
	return out
}

// canonicalStringMap maps a string map onto its canonical form.
func canonicalStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// canonicalFloatMap maps a float map onto its canonical form.
func canonicalFloatMap(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = canonicalFloat(v)
	}
	return out
}

// canonicalFloat normalizes negative zero onto zero.
func canonicalFloat(v float64) float64 {
	if v == 0 {
		return 0
	}
	return v
}
