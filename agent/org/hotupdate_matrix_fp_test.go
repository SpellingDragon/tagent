// hotupdate_matrix_fp_test 承载热更矩阵的 fp 面红线：每个登记维度各钉一条"配置变更⇒指纹变化"直指断言。
//
// - 断言对象是 agent/org 的白名单子集本身，与代际簿记同源，故本文件是矩阵行的证据锚；
// - 通道互斥同样被钉：五条数值热轴必须证明对指纹无影响，否则一次纯数值热更会白 rebuild 一整代。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
package org

import (
	"testing"

	"github.com/SpellingDragon/tagent/config"
	"github.com/stretchr/testify/require"
)

// matrixBaseConfig returns a freshly built config for one mutation probe: every
// case starts from an independent object, so a slice or map below is never shared
// between cases and one case cannot poison another baseline.
func matrixBaseConfig() *config.Config {
	return &config.Config{
		Entry:     "main",
		Model:     "glm-base",
		Provider:  "zai",
		PromptDir: "resources/prompts",
		Providers: map[string]config.ProviderConfig{
			"zai": {Provider: "openai", APIEndpoint: "https://base.example.com/v4"},
		},
		Agents: map[string]config.AgentConfig{
			"main": {
				Model:        "glm-main",
				Provider:     "zai",
				SystemPrompt: config.PromptConfig{Inline: "base prompt"},
				Tools:        []config.ToolRef{{Kind: config.ToolKindTool, ID: "exec"}},
				Memory:       config.MemoryConfig{Type: "memory"},
			},
			"sub": {Model: "glm-sub", Memory: config.MemoryConfig{Type: "memory"}},
		},
	}
}

// editMainAgent applies fn to a private copy of the "main" agent entry and puts it
// back: a map element is not addressable, so in-place field writes on c.Agents["main"]
// would not compile.
func editMainAgent(c *config.Config, fn func(*config.AgentConfig)) {
	ac := c.Agents["main"]
	fn(&ac)
	c.Agents["main"] = ac
}

func fpOf(t *testing.T, c *config.Config) string {
	t.Helper()
	fp, err := ComputeOrgFingerprint(c)
	require.NoErrorf(t, err, "fingerprint of %q must compute", c.Entry)
	return fp
}

// requireMovesFingerprint pins one dimension: a change inside it must move the
// fingerprint, otherwise that change never reaches a rebuild (fake hot reload).
func requireMovesFingerprint(t *testing.T, dim string, mutate func(*config.Config)) {
	t.Helper()
	base := matrixBaseConfig()
	mod := matrixBaseConfig()
	mutate(mod)
	require.NotEqualf(t, fpOf(t, base), fpOf(t, mod),
		"dimension %q changed but the org fingerprint did not — the change would never force a new generation", dim)
}

// TestFingerprintPromptDimension 钉住 prompt 声明维度属 fp 面：四种书写各自变更必须推进指纹。
// - 承担的行是矩阵里的 prompt 声明（system_prompt inline/files、agent prompt_dir、顶层 prompt_dir）；
// - 正文文件内容不在本维：它由提示词源的 mtime 懒读独立应答。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintPromptDimension(t *testing.T) {
	cases := []struct {
		dim    string
		mutate func(*config.Config)
	}{
		{"system_prompt.inline", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.SystemPrompt.Inline = "another prompt" })
		}},
		{"system_prompt.files", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) {
				a.SystemPrompt = config.PromptConfig{Files: []string{"plan_agent.md"}}
			})
		}},
		{"agents.*.prompt_dir", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.PromptDir = "resources/prompts2" })
		}},
		{"prompt_dir", func(c *config.Config) { c.PromptDir = "resources/prompts2" }},
	}
	for _, tc := range cases {
		t.Run(tc.dim, func(t *testing.T) { requireMovesFingerprint(t, tc.dim, tc.mutate) })
	}
}

// TestFingerprintToolsDimension 钉住 tools 维度属 fp 面：工具集的增删与改指必须推进指纹。
// - 承担的行是矩阵里的 tools（ToolRef 声明层；逐字段覆盖由 TestOrgFingerprint_CoversFullToolRef 承担）。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintToolsDimension(t *testing.T) {
	cases := []struct {
		dim    string
		mutate func(*config.Config)
	}{
		{"tools.id", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Tools[0].ID = "read_file" })
		}},
		{"tools.kind", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Tools[0].Kind = config.ToolKindAgent })
		}},
		{"tools.added", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) {
				a.Tools = append(a.Tools, config.ToolRef{Kind: config.ToolKindTool, ID: "recall"})
			})
		}},
		{"tools.cleared", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Tools = nil })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.dim, func(t *testing.T) { requireMovesFingerprint(t, tc.dim, tc.mutate) })
	}
}

// TestFingerprintModelIDDimension 钉住模型 ID 维度属 fp 面：agent 自身与全局默认两种落点都要推进指纹。
// - 全局默认值必须入指纹，因为未显式声明模型的子 agent 经注册表解析到它。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintModelIDDimension(t *testing.T) {
	cases := []struct {
		dim    string
		mutate func(*config.Config)
	}{
		{"agents.*.model", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Model = "glm-other" })
		}},
		{"model", func(c *config.Config) { c.Model = "glm-other" }},
	}
	for _, tc := range cases {
		t.Run(tc.dim, func(t *testing.T) { requireMovesFingerprint(t, tc.dim, tc.mutate) })
	}
}

// TestFingerprintProviderDimension 钉住 provider 维度属 fp 面：agent 覆盖与全局默认各自变更都要推进指纹。
// - per-agent provider 这一半此前只有归类审计，没有直指断言，是矩阵登记的回归保护空窗。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintProviderDimension(t *testing.T) {
	cases := []struct {
		dim    string
		mutate func(*config.Config)
	}{
		{"agents.*.provider", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Provider = "deepseek" })
		}},
		{"provider", func(c *config.Config) { c.Provider = "deepseek" }},
	}
	for _, tc := range cases {
		t.Run(tc.dim, func(t *testing.T) { requireMovesFingerprint(t, tc.dim, tc.mutate) })
	}
}

// TestFingerprintAPIEndpointDimension 钉住端点维度的两侧边界：providers.*.api_endpoint 必须入指纹，顶层 api_endpoint 必须不入。
// - 前者参与模型实例构建，隐身就等于路由没改；
// - 后者是进程级资源，入指纹会造出"热更成功而旧连接还在用"的假象。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintAPIEndpointDimension(t *testing.T) {
	requireMovesFingerprint(t, "providers.*.api_endpoint", func(c *config.Config) {
		p := c.Providers["zai"]
		p.APIEndpoint = "https://v2.example.com/v4"
		c.Providers["zai"] = p
	})

	base := matrixBaseConfig()
	mod := matrixBaseConfig()
	mod.APIEndpoint = "https://process-level.example.com"
	require.Equalf(t, fpOf(t, base), fpOf(t, mod),
		"process-level api_endpoint must stay outside the subset: it cannot migrate a live connection")
}

// TestFingerprintSummaryKnobsMoveFingerprint 钉住摘要策略 knob 属 fp 面（矩阵第三节的归属结论）。
// - 六个 knob 各自变更必须推进指纹，代际重建即把新值带进新的 SmartCompressor；
// - 若某项对指纹无影响，它既不换代也不进源面，就成了一条静默的配置——本域判定它属 fp，故必须逐名钉住。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintSummaryKnobsMoveFingerprint(t *testing.T) {
	cases := []struct {
		dim    string
		mutate func(*config.Config)
	}{
		{"compress.summary_max_tokens", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Compress.SummaryMaxTokens = 4096 })
		}},
		{"compress.summary.reasoning_effort", func(c *config.Config) {
			effort := "high"
			editMainAgent(c, func(a *config.AgentConfig) {
				a.Compress.Summary.ReasoningEffort = &effort
			})
		}},
		{"compress.summary.model", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Compress.Summary.Model = "summ-v2" })
		}},
		{"compress.card_max_chars", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Compress.CardMaxChars = 1200 })
		}},
		{"compress.compact_keys_listed", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Compress.CompactKeysListed = 8 })
		}},
		{"compress.recent_full_count", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Compress.RecentFullCount = 16 })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.dim, func(t *testing.T) { requireMovesFingerprint(t, tc.dim, tc.mutate) })
	}
}

// TestFingerprintGenerationKnobsDimension 钉住生成参数已落在 fp 面：五项 knob 各自变更都要推进指纹。
// - 这一行是"生成参数不挪源拉取面"裁决的证据锚：粒度已热，缺的只是更细粒度，不是通道。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintGenerationKnobsDimension(t *testing.T) {
	cases := []struct {
		dim    string
		mutate func(*config.Config)
	}{
		{"temperature", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Temperature = 0.7 })
		}},
		{"thinking_enabled", func(c *config.Config) {
			on := true
			editMainAgent(c, func(a *config.AgentConfig) { a.ThinkingEnabled = &on })
		}},
		{"thinking_tokens", func(c *config.Config) {
			n := 2048
			editMainAgent(c, func(a *config.AgentConfig) { a.ThinkingTokens = &n })
		}},
		{"reasoning_effort", func(c *config.Config) {
			effort := "medium"
			editMainAgent(c, func(a *config.AgentConfig) { a.ReasoningEffort = &effort })
		}},
		{"reasoning_content_mode", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.ReasoningContentMode = "keep_all" })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.dim, func(t *testing.T) { requireMovesFingerprint(t, tc.dim, tc.mutate) })
	}
}

// TestFingerprintRouteRefDimension 钉住模型路由声明属 fp 面：调用点的 provider/model 引用项各自变更都要推进指纹。
// - 路由引用的任何一项隐身，切换后端就会既换代失败也不报错。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintRouteRefDimension(t *testing.T) {
	cases := []struct {
		dim    string
		mutate func(*config.Config)
	}{
		{"compress.summary.provider", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.Compress.Summary.Provider = "another-provider" })
		}},
		{"sub agent model ref", func(c *config.Config) {
			ac := c.Agents["sub"]
			ac.Model, ac.Provider = "glm-sub-v2", "zai"
			c.Agents["sub"] = ac
		}},
	}
	for _, tc := range cases {
		t.Run(tc.dim, func(t *testing.T) { requireMovesFingerprint(t, tc.dim, tc.mutate) })
	}
}

// TestFingerprintHotParamAxesStayOutsideSubset 钉住通道互斥：五条数值热轴的任何一项变更都不得推进指纹。
// - 这五项由消费边界按次读取，换代对它们是多余重建；
// - 若某项意外进入子集，一次纯数值热更会整代 rebuild，在途钉定语义随之失真。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintHotParamAxesStayOutsideSubset(t *testing.T) {
	cases := []struct {
		dim    string
		mutate func(*config.Config)
	}{
		{"max_tokens", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.MaxTokens = 32000 })
		}},
		{"compress_threshold", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.CompressThreshold = 0.95 })
		}},
		{"keep_recent_tasks", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.KeepRecentTasks = 9 })
		}},
		{"task_terminal_ttl", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.TaskTerminalTTL = "45m" })
		}},
		{"task_default_ttl", func(c *config.Config) {
			editMainAgent(c, func(a *config.AgentConfig) { a.TaskDefaultTTL = "90m" })
		}},
	}
	base := fpOf(t, matrixBaseConfig())
	for _, tc := range cases {
		t.Run(tc.dim, func(t *testing.T) {
			mod := matrixBaseConfig()
			tc.mutate(mod)
			require.Equalf(t, base, fpOf(t, mod),
				"hot axis %q must not move the fingerprint — it is consumed at the read boundary, not by a rebuild", tc.dim)
		})
	}
}
