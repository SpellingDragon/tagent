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
	Entry    string             `json:"entry"`
	Agents   []canonicalAgent   `json:"agents"`
	Refs     []canonicalToolRef `json:"refs"`
	Baseline string             `json:"baseline"`
}

// canonicalAgent is one agent spec in canonical field order.
type canonicalAgent struct {
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
	// Memory and Meditation are the bindings of the agent: an agent that
	// declares neither carries no entry of its own, so its canonical form
	// names no binding and its fingerprint stays the one the declaration
	// surface had before the bindings existed.
	Memory     *canonicalMemory     `json:"memory,omitempty"`
	Meditation *canonicalMeditation `json:"meditation,omitempty"`
}

// canonicalMemory is one memory binding in canonical field order.
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

// canonicalMeditation is one meditation binding in canonical field order. The
// cadence is named in nanoseconds, so two spellings of one duration normalize
// onto the same fingerprint.
type canonicalMeditation struct {
	Enabled            bool     `json:"enabled,omitempty"`
	IntervalNS         int64    `json:"interval_ns,omitempty"`
	MinGapNS           int64    `json:"min_gap_ns,omitempty"`
	PromptFile         string   `json:"prompt_file,omitempty"`
	ObservedNamespaces []string `json:"observed_namespaces"`
	DeliverTo          []string `json:"deliver_to"`
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

// isZero reports whether the memory declaration names no binding at all.
func (s MemorySpec) isZero() bool {
	return s.Type == "" && s.Path == "" && len(s.ReadNamespaces) == 0 && s.Engine.isZero()
}

// isZero reports whether the engine declaration names no engine at all.
func (s EngineSpec) isZero() bool {
	return s == EngineSpec{}
}

// isZero reports whether the embedding declaration names no connection at all.
func (s EmbeddingSpec) isZero() bool {
	return s == EmbeddingSpec{}
}

// isZero reports whether the meditation declaration enables no reflection.
func (s MeditationSpec) isZero() bool {
	return !s.Enabled && s.Interval == 0 && s.MinGap == 0 && s.PromptFile == "" &&
		len(s.ObservedNamespaces) == 0 && len(s.DeliverTo) == 0
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
	}
}

// canonicalAgents maps agent specs onto canonical agent forms in declaration
// order.
func canonicalAgents(in []AgentSpec) []canonicalAgent {
	out := make([]canonicalAgent, 0, len(in))
	for _, a := range in {
		out = append(out, canonicalAgent{
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
			Memory:           canonicalMemoryOf(a.Memory),
			Meditation:       canonicalMeditationOf(a.Meditation),
		})
	}
	return out
}

// canonicalMemoryOf maps one memory binding onto its canonical form, or names
// no binding at all while the declaration names none.
func canonicalMemoryOf(s MemorySpec) *canonicalMemory {
	if s.isZero() {
		return nil
	}
	return &canonicalMemory{
		Type:           s.Type,
		Path:           s.Path,
		ReadNamespaces: canonicalStrings(s.ReadNamespaces),
		Engine:         canonicalEngineOf(s.Engine),
	}
}

// canonicalEngineOf maps one semantic engine onto its canonical form, or names
// no engine at all while the declaration names none.
func canonicalEngineOf(s EngineSpec) *canonicalEngine {
	if s.isZero() {
		return nil
	}
	return &canonicalEngine{
		Backend:     s.Backend,
		Embedding:   canonicalEmbeddingOf(s.Embedding),
		VectorTopK:  s.VectorTopK,
		KeywordTopK: s.KeywordTopK,
		RRFK:        s.RRFK,
	}
}

// canonicalEmbeddingOf maps one embedding connection onto its canonical form,
// or names no connection at all while the declaration names none.
func canonicalEmbeddingOf(s EmbeddingSpec) *canonicalEmbedding {
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

// canonicalMeditationOf maps one meditation binding onto its canonical form,
// or names no binding at all while the declaration enables none.
func canonicalMeditationOf(s MeditationSpec) *canonicalMeditation {
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
