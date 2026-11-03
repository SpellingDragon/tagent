package replay

import (
	"os"
	"strings"
	"testing"
)

// readFoldMeasurementFile reads one repository file the fold-cost decision cites.
func readFoldMeasurementFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

// TestFoldDepthMeasurementExists pins the independent measurements the fold-cost decision reads.
// - the fold-depth benchmark stays a smoke build measuring exactly the two approved windows
// - the recovery anchors the same decision cites stay named test functions
//
// 契约: docs/FACT-VOCABULARY.md#reduction-criteria
func TestFoldDepthMeasurementExists(t *testing.T) {
	benchmark := readFoldMeasurementFile(t, "../../examples/wechat-bot/smoke_bench_test.go")
	if !strings.HasPrefix(benchmark, "//go:build smoke\n") {
		t.Fatalf("the fold-depth benchmark is not behind the smoke build tag, so the measured windows cannot be run as the decision commands run them")
	}
	if !strings.Contains(benchmark, "func BenchmarkFoldDepthCost(") {
		t.Fatalf("the fold-depth benchmark is absent, so the fold cost the capacity decision reads has no independent measurement")
	}
	if !strings.Contains(benchmark, "[]int{256, 32768}") {
		t.Fatalf("the fold-depth benchmark no longer measures exactly the approved windows [256 32768], so a window the long matrix does not run could be reported as measured")
	}

	restore := readFoldMeasurementFile(t, "../../tests/integration/host/compress_pipeline_test.go")
	if !strings.Contains(restore, "func TestCompressProductRestoresAfterRestart(") {
		t.Fatalf("the restart recovery anchor is absent, so the product restore the fold-cost decision cites is not measured")
	}
	refold := readFoldMeasurementFile(t, "../../tests/integration/host/age_fold_test.go")
	if !strings.Contains(refold, "func TestRebuildRefoldsIdempotent(") {
		t.Fatalf("the rebuild anchor is absent, so the refold idempotence the fold-cost decision cites is not measured")
	}
}
