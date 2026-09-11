package tagent

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// e2eYAML renders the minimal org config used by the hot-shift e2e test.
// Structural fields stay byte-identical across renders so only
// compress_threshold (hot-applicable, fingerprint-excluded) or an explicit
// org field can move the fingerprint.
func e2eYAML(threshold float64, model string) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key: sk-test\n" +
		"agents:\n  main:\n    model: " + model + "\n" +
		"    system_prompt:\n      inline: \"e2e hot shift\"\n" +
		"    compress_threshold: " + strconv.FormatFloat(threshold, 'f', -1, 64) + "\n" +
		"    memory:\n      type: memory\n"
}

// TestOrgHotShift_EndToEnd drives the full incremental-A loop against a real
// agent built by tagent.New: mtime churn -> re-parse -> fingerprint compare ->
// ApplyOrgParams hot-shift (unchanged fp) / keep-serving (changed fp, broken
// config) — without restarting the process.
func TestOrgHotShift_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(yamlPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		// Force strictly increasing mtime: FS timestamp granularity can
		// swallow rapid successive writes and silently skip the reload.
		tick = tick.Add(2 * time.Second)
		if err := os.Chtimes(yamlPath, tick, tick); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}

	write(e2eYAML(0.8, "gpt-x"))
	cfg, err := LoadConfig(yamlPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = entry.Close() }()

	if got := entry.OrgThreshold(); got != 0.8 {
		t.Fatalf("initial threshold = %v, want 0.8 (constructor must seed cm.thresholdPct)", got)
	}

	// 1) hot shift: same org structure, new threshold -> applied live.
	write(e2eYAML(0.5, "gpt-x"))
	entry.CheckOrgReload()
	if got := entry.OrgThreshold(); got != 0.5 {
		t.Fatalf("after hot shift threshold = %v, want 0.5", got)
	}

	// 2) structural change (model is fingerprinted): keep serving,
	// threshold unchanged, no panic.
	write(e2eYAML(0.5, "gpt-w"))
	entry.CheckOrgReload()
	if got := entry.OrgThreshold(); got != 0.5 {
		t.Fatalf("after structural change threshold = %v, want 0.5 (kept)", got)
	}

	// 3) broken config: fail-closed to previous snapshot.
	write("entry: [broken")
	entry.CheckOrgReload()
	if got := entry.OrgThreshold(); got != 0.5 {
		t.Fatalf("after broken config threshold = %v, want 0.5 (fail-closed)", got)
	}
}
