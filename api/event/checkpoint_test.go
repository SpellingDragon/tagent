package event

import "testing"

// TestCheckpointOffsetReadsBackItsRendering pins the two directions of the rendered watermark.
// - the pairs the offset renders read back as the offset they were rendered from, in sorted order
// - a rendering without its separator or without a number is refused rather than defaulted
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestCheckpointOffsetReadsBackItsRendering(t *testing.T) {
	offset := CheckpointOffset{2: 7, 1: 3}
	rendered := offset.String()
	if rendered != "1:3,2:7" {
		t.Fatalf("the rendered watermark = %q, want the sorted partition:seq pairs", rendered)
	}
	back, ok := parseCheckpointOffset(rendered)
	if !ok || len(back) != len(offset) {
		t.Fatalf("the rendering reads back as (%v, %v), want the %d partitions it rendered", back, ok, len(offset))
	}
	for partition, seq := range offset {
		if back[partition] != seq {
			t.Fatalf("partition %d reads back as %d, want the %d it rendered", partition, back[partition], seq)
		}
	}
	for _, refused := range []string{"", "1", "1:", "1:x", "x:1"} {
		if _, held := parseCheckpointOffset(refused); held {
			t.Fatalf("%q reads back as a watermark, want a refusal", refused)
		}
	}
}
