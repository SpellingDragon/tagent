package event

import (
	"encoding/json"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

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

// TestCheckpointKeyNamesThePartition pins the spelling of the fact key.
// - the key names the partition beside the sequence slot, so two partitions standing at one slot name two facts
// - two slots of one partition name two keys
// - the key never equals the slot-only spelling a cut filed before the partition was named carries
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestCheckpointKeyNamesThePartition(t *testing.T) {
	if got := CheckpointKey(7, 9); got != "checkpoint:7:9" {
		t.Fatalf("CheckpointKey(7, 9) = %q, want the partition beside the slot it names", got)
	}
	if CheckpointKey(7, 9) == CheckpointKey(8, 9) {
		t.Fatal("two partitions standing at seq 9 name one key, so the cut of the second partition is refused as a key conflict")
	}
	if CheckpointKey(7, 9) == CheckpointKey(7, 10) {
		t.Fatal("two slots of one partition name one key, so the later cut is refused as a key conflict")
	}
	if CheckpointKey(7, 9) == checkpointKeyPrefix+"9" {
		t.Fatal("the partition-scoped key equals the slot-only key an earlier cut was filed under, so the two collide in the global fact index")
	}
}

// TestCheckpointOfReadsBackTheKeyItWasFiledUnder pins the family read against the spelling of the key it was filed under.
//   - the frozen type answers the checkpoint, and the key it was filed under is the key it reads back as
//   - a cut filed under the slot-only spelling is still readable beside the partition-scoped ones
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestCheckpointOfReadsBackTheKeyItWasFiledUnder(t *testing.T) {
	offset := CheckpointOffset{7: 9}
	slices := []CheckpointSlice{{Name: "projection", Payload: json.RawMessage(`{"sessions":[]}`)}}
	for _, key := range []string{checkpointKeyPrefix + "9", CheckpointKey(7, 9)} {
		checkpoint, err := CheckpointFor(offset, slices)
		if err != nil {
			t.Fatalf("state the checkpoint: %v", err)
		}
		checkpoint.Key = key
		payload, err := CheckpointPayloadOf(checkpoint)
		if err != nil {
			t.Fatalf("render the checkpoint payload: %v", err)
		}
		back, stated := CheckpointOf(Event{
			Key:     EventID(key),
			Type:    TypeCheckpoint,
			Message: model.Message{Content: string(payload)},
		})
		if !stated {
			t.Fatalf("the fact %s states no readable checkpoint, want the family read by its frozen type", key)
		}
		if back.Key != key {
			t.Fatalf("the checkpoint reads back under %q, want the key %q it was filed under", back.Key, key)
		}
		if back.FactType() != TypeCheckpoint {
			t.Fatalf("the checkpoint reads back as %q, want the frozen checkpoint type", back.FactType())
		}
	}
}
