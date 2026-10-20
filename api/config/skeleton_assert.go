package config

import "context"

// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
//
// Skeleton compile-time assertions for the phase 1 declaration layer. The
// declaration layer is plain data, so there is no interface to assert with the
// usual `var _ Iface = (*T)(nil)` form; the assertions below instead name every
// documented field of every declaration type in a zero-value composite literal,
// so the package fails to compile if a field is renamed or its type changes.
//
// Exported entry points must keep exactly these signatures for the composition
// root to compile against them, and the assertions prove the declaration types
// stay usable by that consumer.
var (
	_ func(context.Context, string) (*Declaration, error) = Load
	_ func(Declaration) (string, error)                   = Fingerprint
)

// Declared declaration types and their fields.
var (
	_ = Declaration{
		Entry:    "",
		Agents:   []AgentSpec{},
		Refs:     []ToolRef{},
		Baseline: "",
		Raw:      map[string]any{},
	}

	_ = AgentSpec{
		ID:         "",
		ParentID:   "",
		Model:      "",
		Tools:      []ToolRef{},
		Plugins:    []string{},
		TaskPolicy: "",
		Files:      []FileRef{},
		Params:     map[string]float64{},
		FinalRound: FinalRoundSpec{},
	}

	_ = FinalRoundSpec{
		MaxRounds:      0,
		BudgetSeconds:  0,
		TimeoutSeconds: 0,
		Prompt:         "",
	}

	_ = ToolRef{
		Name:     "",
		Params:   map[string]string{},
		Endpoint: "",
	}

	_ = FileRef{
		Path: "",
		Kind: FileKindPrompt,
	}

	_ = FileKindSkill
)
