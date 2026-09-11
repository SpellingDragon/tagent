package compress

import (
	"reflect"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

func TestSnapshotRoundTrip(t *testing.T) {
	in := &CompressionSnapshot{
		SchemaVersion:  SnapshotSchemaV1,
		FullBoundary:   12345,
		Threshold:      70,
		RetainedRefs:   []memory.EventReference{{EventKey: 12346, EventType: "external_input", EventSummary: "用户输入", Timestamp: 1726000000000, Role: "user"}},
		MeditationKeys: []int64{111, 222},
		CompressedKeys: []int64{1, 2, 3},
		CardText:       "- 工具链: x",
		CreatedAt:      42,
	}
	raw, err := MarshalSnapshot(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := ParseSnapshot(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("roundtrip mismatch:\n in=%+v\nout=%+v", in, out)
	}
}

func TestParseSnapshotInvalid(t *testing.T) {
	cases := map[string]string{
		"empty payload":  "",
		"garbage":        "not-json",
		"bad version":    `{"schema_version":2,"full_boundary":5,"threshold":60,"retained_refs":[]}`,
		"zero boundary":  `{"schema_version":1,"threshold":60,"retained_refs":[]}`,
		"bad threshold":  `{"schema_version":1,"full_boundary":5,"threshold":0,"retained_refs":[]}`,
		"missing refs":   `{"schema_version":1,"full_boundary":5,"threshold":60}`,
		"null refs":      `{"schema_version":1,"full_boundary":5,"threshold":60,"retained_refs":null}`,
	}
	for name, raw := range cases {
		if s, err := ParseSnapshot(raw); err == nil {
			t.Fatalf("%s: expected error, got %+v", name, s)
		}
	}
}

func TestParseSnapshotOptionalMissingTolerated(t *testing.T) {
	out, err := ParseSnapshot(`{"schema_version":1,"full_boundary":5,"threshold":60,"retained_refs":[]}`)
	if err != nil {
		t.Fatalf("tolerated case failed: %v", err)
	}
	if len(out.MeditationKeys) != 0 || len(out.CompressedKeys) != 0 || out.CardText != "" {
		t.Fatalf("optional fields should default empty: %+v", out)
	}
}

func TestMeditationKeysSnapshotSortedCopy(t *testing.T) {
	cc := &ContextCompressor{}
	cc.MarkMeditationKey(30)
	cc.MarkMeditationKey(10)
	cc.MarkMeditationKey(20)
	want := []int64{10, 20, 30}
	got := cc.MeditationKeysSnapshot()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got[0] = 999 // 副本 mutation 不得影响内部态
	if again := cc.MeditationKeysSnapshot(); !reflect.DeepEqual(again, want) {
		t.Fatalf("internal state mutated via returned copy: %v", again)
	}
}

func TestThresholdGetterDefaultPositive(t *testing.T) {
	cc := &ContextCompressor{}
	if cc.Threshold() <= 0 || cc.Threshold() >= 100 {
		t.Fatalf("default threshold out of (0,100): %v", cc.Threshold())
	}
}
