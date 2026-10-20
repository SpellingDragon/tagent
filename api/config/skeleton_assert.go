package config

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
	_ func(Declaration) (string, error) = Fingerprint
)

// Declared declaration types and their fields.
var (
	_ = Declaration{
		Entry:    "",
		Agents:   []AgentSpec{},
		Refs:     []ToolRef{},
		Baseline: "",
		Servers:  map[string]MCPServerSpec{},
		Raw:      map[string]any{},
	}

	_ = AgentSpec{
		ID:               "",
		ParentID:         "",
		Model:            "",
		Tools:            []ToolRef{},
		Plugins:          []string{},
		TaskPolicy:       "",
		Files:            []FileRef{},
		Params:           map[string]float64{},
		FinalRound:       FinalRoundSpec{},
		ReplayBudget:     0,
		EnvironmentBlock: false,
		Memory:           MemorySpec{},
		Meditation:       MeditationSpec{},
		File:             FileSpec{},
		Exec:             ExecSpec{},
	}

	_ = MemorySpec{
		Type:           "",
		Path:           "",
		ReadNamespaces: []string{},
		Engine:         EngineSpec{},
	}

	_ = EngineSpec{
		Backend:     "",
		Embedding:   EmbeddingSpec{},
		VectorTopK:  0,
		KeywordTopK: 0,
		RRFK:        0,
	}

	_ = EmbeddingSpec{
		Provider:    "",
		Model:       "",
		APIKeyEnv:   "",
		Dimensions:  0,
		APIEndpoint: "",
	}

	_ = MeditationSpec{
		Enabled:            false,
		Interval:           0,
		MinGap:             0,
		PromptFile:         "",
		ObservedNamespaces: []string{},
		DeliverTo:          []string{},
	}

	_ = FileSpec{
		BaseDir:   "",
		ReadRoots: []string{},
	}

	_ = ExecSpec{
		Monitor: MonitorSpec{},
	}

	_ = MonitorSpec{
		Interval:                  0,
		StableDuration:            0,
		InteractiveStableDuration: 0,
		FakeDeadDuration:          0,
	}

	_ = MCPServerSpec{
		Transport: "",
		URL:       "",
		APIKeyEnv: "",
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
