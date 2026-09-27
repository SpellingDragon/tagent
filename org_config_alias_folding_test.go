package tagent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// §5.2（配置别名）：`FoldModelRefAliases` 把 legacy 扁平键（compress.summary_model
// / summary_provider / summary_effort）折进统一的 ModelRef（compress.summary.*），
// 显式 ModelRef 逐字段优先、legacy 只填未设字段。此折叠发生在 LoadConfig→ApplyDefaults，
// 因此**发布决策所比的 `computeOrgFingerprint` 看到的恒是折叠后的规范形**。
//
// 若折叠缺位或晚于指纹：同一语义写成 legacy 形与 canonical 形会算出不同指纹，运维把一
// 个弃用键改写成推荐键（值不变）就会被误判为**结构性变更 → 幻影发布一代**（白白重建
// executor、扰乱在途判定）。本测钉住这条 5.2 此前零覆盖的契约：别名折叠稳定、指纹不因
// 别名书写漂移、且被折叠字段仍真实参与指纹（否则稳定性是假绿）。

func aliasYAML(compressBlock string) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n    system_prompt:\n      inline: \"P\"\n" +
		compressBlock +
		"    memory:\n      type: memory\n"
}

// writeCfg writes content and returns the config loaded through the REAL parse path
// (LoadConfig runs ApplyDefaults → FoldModelRefAliases), which is exactly what the
// reloader feeds to computeOrgFingerprint on a fresh check (tagent.go:693).
func writeCfg(t *testing.T, dir, name, content string) *Config {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	cfg, err := LoadConfig(p)
	require.NoError(t, err, "config %s must load", name)
	return cfg
}

// TestD52_ModelRefAliasesFoldToStableFingerprint is the core §5.2 alias contract.
func TestD52_ModelRefAliasesFoldToStableFingerprint(t *testing.T) {
	dir := t.TempDir()

	// Legacy flat form and the canonical ModelRef form carry the SAME semantics.
	legacy := writeCfg(t, dir, "legacy.yaml", aliasYAML("    compress:\n      summary_model: summ\n"))
	canonical := writeCfg(t, dir, "canonical.yaml", aliasYAML("    compress:\n      summary:\n        model: summ\n"))

	// LoadConfig folded the legacy key into the unified holder and CLEARED it — so
	// the flat field can never be independently fingerprinted.
	require.Equal(t, "summ", legacy.Agents["main"].Compress.Summary.Model,
		"summary_model folded into compress.summary.model at load")
	require.Empty(t, legacy.Agents["main"].Compress.SummaryModel,
		"the legacy field is cleared after folding (not left as a second source)")

	// The whole point: the published generation must NOT depend on which spelling the
	// operator used. Equal folded configs → equal fingerprint → no phantom publish.
	require.Equal(t, mustFP(t, legacy), mustFP(t, canonical),
		"alias and canonical spellings of the same setting fold to an identical fingerprint")
}

// TestD52_FoldedFieldIsLiveInTheFingerprint guards against the stable-fingerprint test
// passing vacuously: if the folded summary model simply were not fingerprinted at all,
// alias-vs-canonical would "agree" while a genuine change was also invisible. This
// asserts a real value change DOES move the fingerprint.
func TestD52_FoldedFieldIsLiveInTheFingerprint(t *testing.T) {
	dir := t.TempDir()
	base := writeCfg(t, dir, "base.yaml", aliasYAML("    compress:\n      summary_model: summ\n"))
	changed := writeCfg(t, dir, "changed.yaml", aliasYAML("    compress:\n      summary_model: different\n"))
	require.NotEqual(t, mustFP(t, base), mustFP(t, changed),
		"the folded summary model is genuinely part of the fingerprint — stability is not from exclusion")
}

// TestD52_FoldIsIdempotentUnderRepeat checks that re-folding an already-folded config
// (what a second CheckOrgReload would do to a still-cached effective config) neither
// re-moves a value nor resurrects the legacy field — the fold must be a fixed point.
func TestD52_FoldIsIdempotentUnderRepeat(t *testing.T) {
	dir := t.TempDir()
	legacy := writeCfg(t, dir, "legacy.yaml", aliasYAML("    compress:\n      summary_model: summ\n      summary_provider: p1\n"))
	before := mustFP(t, legacy)

	require.Equal(t, "summ", legacy.Agents["main"].Compress.Summary.Model)
	require.Equal(t, "p1", legacy.Agents["main"].Compress.Summary.Provider)

	legacy.FoldModelRefAliases() // fold again — must be a no-op on already-folded state
	require.Equal(t, "summ", legacy.Agents["main"].Compress.Summary.Model, "value is not lost on re-fold")
	require.Empty(t, legacy.Agents["main"].Compress.SummaryModel, "legacy field does not reappear")
	require.Equal(t, before, mustFP(t, legacy), "re-folding does not drift the fingerprint")
}
