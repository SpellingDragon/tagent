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
	}
}

// canonicalAgents maps agent specs onto canonical agent forms in declaration
// order.
func canonicalAgents(in []AgentSpec) []canonicalAgent {
	out := make([]canonicalAgent, 0, len(in))
	for _, a := range in {
		out = append(out, canonicalAgent{
			ID:         a.ID,
			ParentID:   a.ParentID,
			Model:      a.Model,
			Tools:      canonicalToolRefs(a.Tools),
			Plugins:    canonicalStrings(a.Plugins),
			TaskPolicy: a.TaskPolicy,
			Files:      canonicalFileRefs(a.Files),
			Params:     canonicalFloatMap(a.Params),
			FinalRound: canonicalFinalRoundOf(a.FinalRound),
		})
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
