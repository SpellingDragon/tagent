package replay

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// ageBase is the pass time the segment-age invariants lay their turns out from.
var ageBase = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

// foldAnchorLine is the provenance anchor every folded line opens with: the
// moment the folded entry recorded, in the timezone its fact stated, and the
// recorder generation that wrote it, or the explicit gen:? placeholder beside an
// entry whose fact named no generation.
var foldAnchorLine = regexp.MustCompile(`^\[(\d{2}-\d{2} \d{2}:\d{2})\|gen:(\?|[0-9A-Za-z_.-]+)\] `)

// ageTurn lays one closed turn out on the projection: the input that opens it,
// the process detail of the turn and the output that closes it.
func ageTurn(seed string, step, process int) []ProjectionEntry {
	at := func(offset int) time.Time {
		return ageBase.Add(time.Duration(step*10+offset) * time.Minute)
	}
	entries := []ProjectionEntry{{
		Key:     event.EventID(seed + "-in"),
		Type:    event.TypeExternalInput,
		Summary: "input " + seed,
		Content: strings.Repeat("用户输入 "+seed+"。", 20),
		Time:    at(0),
	}}
	for i := 0; i < process; i++ {
		suffix := string(rune('a' + i))
		entries = append(entries, ProjectionEntry{
			Key:     event.EventID(seed + "-plan-" + suffix),
			Type:    event.TypeThinkingPlan,
			Summary: "plan " + seed + suffix,
			Content: strings.Repeat("过程细节 "+seed+"。", 20),
			Time:    at(i + 1),
		})
		entries = append(entries, ProjectionEntry{
			Key:     event.EventID(seed + "-cmd-" + suffix),
			Type:    event.TypeActionCommand,
			Summary: "call " + seed + suffix,
			Content: strings.Repeat("工具输出 "+seed+"。", 20),
			Time:    at(i + 1),
		})
	}
	return append(entries, ProjectionEntry{
		Key:     event.EventID(seed + "-out"),
		Type:    event.TypeAgentOutput,
		Summary: "output " + seed,
		Content: strings.Repeat("回答 "+seed+"。", 20),
		Time:    at(9),
	})
}

// ageProjection returns a projection over the given entries, in the order given.
func ageProjection(t *testing.T, entries []ProjectionEntry) *ReplayProjection {
	t.Helper()
	projection := NewReplayProjection()
	for _, entry := range entries {
		projection.Append(context.Background(), entry)
	}
	return projection
}

// processDetail reports how many entries of the projection still carry the raw
// process detail of a turn.
func processDetail(entries []ProjectionEntry) int {
	count := 0
	for _, entry := range entries {
		if isProcessDetail(entry) {
			count++
		}
	}
	return count
}

// TestAgeLevelReadsTheSharedRule pins the age rule the segment-age axis shares with the deterministic compress level.
//   - the recent window reads L0, the next window L1, the window after it L2 and everything older L3
//   - a plan naming no window reads the two-segment default
//   - the levels map onto the three bands: L0 new, L1 and L2 mid, L3 old
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestAgeLevelReadsTheSharedRule(t *testing.T) {
	cases := []struct {
		age      int
		keep     int
		level    int
		wantBand Band
	}{
		{age: 0, keep: 2, level: 0, wantBand: BandNew},
		{age: 1, keep: 2, level: 0, wantBand: BandNew},
		{age: 2, keep: 2, level: 1, wantBand: BandMid},
		{age: 3, keep: 2, level: 1, wantBand: BandMid},
		{age: 4, keep: 2, level: 2, wantBand: BandMid},
		{age: 5, keep: 2, level: 2, wantBand: BandMid},
		{age: 6, keep: 2, level: 3, wantBand: BandOld},
		{age: 9, keep: 2, level: 3, wantBand: BandOld},
		{age: 0, keep: 1, level: 0, wantBand: BandNew},
		{age: 1, keep: 1, level: 1, wantBand: BandMid},
		{age: 2, keep: 1, level: 2, wantBand: BandMid},
		{age: 3, keep: 1, level: 3, wantBand: BandOld},
		{age: 5, keep: 0, level: 2, wantBand: BandMid},
	}
	for _, legal := range cases {
		if got := LevelOfAge(legal.age, legal.keep); got != legal.level {
			t.Errorf("LevelOfAge(age=%d, keepRecent=%d) = %d, want %d", legal.age, legal.keep, got, legal.level)
		}
		if got := BandOfAge(legal.age, legal.keep); got != legal.wantBand {
			t.Errorf("BandOfAge(age=%d, keepRecent=%d) = %v, want %v", legal.age, legal.keep, got, legal.wantBand)
		}
	}
	for level, want := range map[int]Band{0: BandNew, 1: BandMid, 2: BandMid, 3: BandOld} {
		if got := BandOfLevel(level); got != want {
			t.Errorf("BandOfLevel(%d) = %v, want %v", level, got, want)
		}
	}
	for band, want := range map[Band]string{BandNew: "new", BandMid: "mid", BandOld: "old"} {
		if got := band.String(); got != want {
			t.Errorf("Band(%d).String() = %q, want %q", int(band), got, want)
		}
	}
}

// testSegmentsCloseAtAgentOutput pins the positional partition of a projection into
// task-turn segments: an output that records no readable input relation closes
// every entry before it, so the positional boundary stands.
//   - a segment spans an input beside the process detail up to the output that closes it
//   - a tail no output closed stays its own segment, in progress and never over the recent window
//   - the ages run from the newest segment backwards
func testSegmentsCloseAtAgentOutput(t *testing.T) {
	entries := append(ageTurn("t1", 0, 1), ageTurn("t2", 1, 1)...)
	entries = append(entries, ProjectionEntry{
		Key:     "t3-in",
		Type:    event.TypeExternalInput,
		Summary: "input t3",
		Content: "进行中的输入。",
		Time:    ageBase.Add(time.Hour),
	})
	segments := SegmentsOf(entries, DefaultAgeKeepRecent)
	if len(segments) != 3 {
		t.Fatalf("segments = %d, want the two closed turns and the tail in progress", len(segments))
	}
	if !segments[0].Complete || segments[0].Age != 2 || segments[0].Level != 1 || segments[0].Band != BandMid {
		t.Fatalf("first segment = %+v, want the oldest closed turn reading L1 in the middle band", segments[0])
	}
	if !segments[1].Complete || segments[1].Age != 1 || segments[1].Level != 0 || segments[1].Band != BandNew {
		t.Fatalf("second segment = %+v, want the closed turn inside the recent window", segments[1])
	}
	if segments[2].Complete || segments[2].Age != 0 || segments[2].Level != 0 || segments[2].Band != BandNew {
		t.Fatalf("tail segment = %+v, want the segment in progress kept whole in the new band", segments[2])
	}
	spanned := 0
	for _, segment := range segments {
		spanned += len(segment.Entries)
	}
	if spanned != len(entries) {
		t.Fatalf("segments span %d entries, want every one of the %d entries", spanned, len(entries))
	}
}

// TestSegmentsCloseByConsumptionRelation pins the relation a segment closes on.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestSegmentsCloseByConsumptionRelation(t *testing.T) {
	input := func(key string) ProjectionEntry {
		return ProjectionEntry{
			Key:     event.EventID(key),
			Type:    event.TypeExternalInput,
			Summary: "input " + key,
			Content: "用户输入 " + key + "。",
			Time:    ageBase,
		}
	}
	output := func(key, relation string) ProjectionEntry {
		entry := ProjectionEntry{
			Key:     event.EventID(key),
			Type:    event.TypeAgentOutput,
			Summary: "output " + key,
			Content: "回答 " + key + "。",
			Time:    ageBase,
		}
		if relation == "" {
			return entry
		}
		return WithInputEvents(entry, relation)
	}
	keysOf := func(segments []Segment) [][]event.EventID {
		out := make([][]event.EventID, 0, len(segments))
		for _, segment := range segments {
			keys := make([]event.EventID, 0, len(segment.Entries))
			for _, entry := range segment.Entries {
				keys = append(keys, entry.Key)
			}
			out = append(out, keys)
		}
		return out
	}
	assertSegments := func(t *testing.T, got []Segment, want [][]event.EventID) {
		t.Helper()
		gotKeys := keysOf(got)
		if len(gotKeys) != len(want) {
			t.Fatalf("segments = %v, want %v", gotKeys, want)
		}
		for i := range want {
			if !slices.Equal(gotKeys[i], want[i]) {
				t.Fatalf("segment %d = %v, want %v (all segments %v)", i, gotKeys[i], want[i], gotKeys)
			}
		}
	}

	t.Run("the consumption relation closes the segment", func(t *testing.T) {
		entries := []ProjectionEntry{
			input("i-1"),
			{Key: "i-notice", Type: event.TypeFeedback, Summary: "notice", Content: "进度通知。", Time: ageBase},
			input("i-2"),
			{Key: "i-plan", Type: event.TypeThinkingPlan, Summary: "plan", Content: "过程细节。", Time: ageBase},
			input("i-3"),
			output("a", `["i-1","i-2"]`),
			input("i-4"),
			output("b", `["i-3","i-4"]`),
		}
		segments := SegmentsOf(entries, DefaultAgeKeepRecent)
		assertSegments(t, segments, [][]event.EventID{
			{"i-1", "i-notice", "i-2", "i-plan", "a"},
			{"i-3", "i-4", "b"},
		})
		if !segments[0].Complete || !segments[1].Complete {
			t.Fatalf("segments = %+v, want both closed by their output", segments)
		}
		entries = []ProjectionEntry{
			input("i-1"), input("i-2"),
			output("a", `["i-1","i-2"]`),
			input("i-3"),
			output("b", `["i-3"]`),
		}
		segments = SegmentsOf(entries, DefaultAgeKeepRecent)
		assertSegments(t, segments, [][]event.EventID{
			{"i-1", "i-2", "a"},
			{"i-3", "b"},
		})
	})

	t.Run("legacy output without input_events", testSegmentsCloseAtAgentOutput)

	t.Run("unreadable input_events", func(t *testing.T) {
		for name, relation := range map[string]string{
			"empty array":      "[]",
			"empty identities": `["",""]`,
			"null":             "null",
			"not a list":       `"i-1"`,
			"unparseable":      `{"input_events":`,
		} {
			t.Run(name, func(t *testing.T) {
				entries := []ProjectionEntry{
					input("i-1"), input("i-2"),
					output("a", relation),
					input("i-3"),
					output("b", relation),
				}
				assertSegments(t, SegmentsOf(entries, DefaultAgeKeepRecent), [][]event.EventID{
					{"i-1", "i-2", "a"},
					{"i-3", "b"},
				})
			})
		}
	})
}

// TestAgeFoldWaitsForTheVolumeThreshold pins the trigger of an age fold.
//   - a projection inside the threshold answers as it stands, with nothing triggered
//   - every entry keeps its place and the projection renders byte for byte as before
func TestAgeFoldWaitsForTheVolumeThreshold(t *testing.T) {
	ctx := context.Background()
	entries := append(ageTurn("t1", 0, 2), ageTurn("t2", 1, 2)...)
	projection := ageProjection(t, entries)
	before := RenderProjection(projection.Entries())

	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: DefaultAgeKeepRecent, VolumeThreshold: 1 << 20})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if report.Triggered || report.NewFolded != 0 || report.MidFolded != 0 || len(report.Products) != 0 || len(report.Requests) != 0 {
		t.Fatalf("report = %+v, want the fold to wait for the threshold", report)
	}
	if report.Volume != len(before) || report.Volume >= report.Threshold {
		t.Fatalf("report volume = %d (threshold %d), want the rendered volume it judged inside the threshold", report.Volume, report.Threshold)
	}
	if !strings.EqualFold(string(RenderProjection(projection.Entries())), string(before)) {
		t.Fatal("projection rendered differently after a fold that did not trigger")
	}
	if projection.Len() != len(entries) {
		t.Fatalf("projection holds %d entries, want the %d it held before the judgment", projection.Len(), len(entries))
	}
}

// TestAgeFoldFoldsTheMiddleBandIntoOneProduct pins the new→middle production of an age fold.
//   - the process detail of every middle-band turn collapses into one product entry
//   - the product stands where the folded detail stood, between the input and the output
//   - the fold files one compress request per product, content-addressed over the material
//   - the new band stays whole: the newest turn keeps its raw process detail
func TestAgeFoldFoldsTheMiddleBandIntoOneProduct(t *testing.T) {
	ctx := context.Background()
	entries := append(ageTurn("t1", 0, 2), ageTurn("t2", 1, 2)...)
	entries = append(entries, ageTurn("t3", 2, 2)...)
	projection := ageProjection(t, entries)

	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if !report.Triggered {
		t.Fatalf("report = %+v, want the fold to trigger past the threshold", report)
	}
	if report.NewFolded != 8 {
		t.Fatalf("middle-band folded entries = %d, want the eight process entries of the two turns that left the window", report.NewFolded)
	}
	if report.MidFolded != 0 || len(report.Products) != 2 {
		t.Fatalf("report = %+v, want one product per folded turn and no history summary yet", report)
	}
	for i, product := range report.Products {
		if product.Type != ProjectionTypeSegmentFold || !SyntheticKey(product.Key) {
			t.Fatalf("product %d = %+v, want a synthetic segment-fold product", i, product)
		}
		if !foldAnchorLine.MatchString(product.Content) || !strings.Contains(product.Content, "|gen:?] thinking_plan: ") {
			t.Fatalf("product %d content = %q, want the provenance anchor beside the one-line reads of the folded detail", i, product.Content)
		}
		want := event.CompressRequestFor(AgeProducerSegmentAge, strings.TrimPrefix(string(product.Key), "-"), AgeReasonSegmentFold)
		if len(report.Requests) <= i || report.Requests[i].ID != want.ID {
			t.Fatalf("request %d = %+v, want the content-addressed judgment %q", i, report.Requests, want.ID)
		}
	}
	if got := processDetail(projection.Entries()); got != 4 {
		t.Fatalf("projection still carries %d raw process entries, want the four of the newest turn kept whole", got)
	}
	got := projection.Entries()
	if len(got) != 12 {
		t.Fatalf("projection holds %d entries, want the two folded turns as input, product and output beside the whole new band: %v", len(got), got)
	}
	for i, want := range []struct {
		typ string
		key event.EventID
	}{
		{event.TypeExternalInput, "t1-in"},
		{ProjectionTypeSegmentFold, report.Products[0].Key},
		{event.TypeAgentOutput, "t1-out"},
		{event.TypeExternalInput, "t2-in"},
		{ProjectionTypeSegmentFold, report.Products[1].Key},
		{event.TypeAgentOutput, "t2-out"},
		{event.TypeExternalInput, "t3-in"},
	} {
		if got[i].Type != want.typ || got[i].Key != want.key {
			t.Fatalf("entry %d = (%q, %q), want the folded turns laid out as the new band begins at %q", i, got[i].Key, got[i].Type, want.key)
		}
	}
	if got[6].Time != ageBase.Add(20*time.Minute) {
		t.Fatalf("new band opens at %v, want the newest turn untouched at its own moment", got[6].Time)
	}
	assembled, err := projection.AssembleBudget(ctx, 1000)
	if err != nil {
		t.Fatalf("AssembleBudget: %v", err)
	}
	sawProduct := false
	for _, entry := range assembled {
		if entry.Type == ProjectionTypeSegmentFold && foldAnchorLine.MatchString(entry.Content) {
			sawProduct = true
		}
	}
	if !sawProduct {
		t.Fatalf("assembly = %+v, want the segment-fold product presented in the request", assembled)
	}
}

// TestAgeFoldFoldsTheOlderBandsIntoOneHistorySummary pins the middle→old production.
//   - the turns an earlier fold consumed leave the timeline and read as one history summary
//   - the summary leads the projection, so the skeleton of the new band follows it
//   - the earlier fold minted one segment product per turn, and the summary carries them by key
func TestAgeFoldFoldsTheOlderBandsIntoOneHistorySummary(t *testing.T) {
	ctx := context.Background()
	var entries []ProjectionEntry
	for step, seed := range []string{"t1", "t2", "t3", "t4"} {
		entries = append(entries, ageTurn(seed, step, 1)...)
	}
	projection := ageProjection(t, entries)
	plan := AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600}

	first, err := projection.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("first FoldByAge: %v", err)
	}
	if !first.Triggered || first.MidFolded != 0 || len(first.Products) != 3 {
		t.Fatalf("first fold = %+v, want one segment product per turn that left the window and no summary yet", first)
	}

	report, err := projection.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("second FoldByAge: %v", err)
	}
	if !report.Triggered || report.MidFolded != 9 {
		t.Fatalf("report = %+v, want the three folded turns (three entries each) folded into the summary", report)
	}
	if len(report.Products) != 1 {
		t.Fatalf("products = %d, want the one history summary the oldest band folds into", len(report.Products))
	}
	summary := report.Products[0]
	if summary.Type != ProjectionTypeHistorySummary || !SyntheticKey(summary.Key) {
		t.Fatalf("history summary = %+v, want a synthetic summary entry", summary)
	}
	if !strings.Contains(summary.Content, "external_input: input t1") || !strings.Contains(summary.Content, "agent_output: output t3") {
		t.Fatalf("history summary content = %q, want the one-line reads of the older turns", summary.Content)
	}
	for _, product := range first.Products {
		if !strings.Contains(summary.Content, string(product.Key)) {
			t.Fatalf("history summary content = %q, want the segment product %q referenced by key", summary.Content, product.Key)
		}
	}
	want := event.CompressRequestFor(AgeProducerSegmentAge, strings.TrimPrefix(string(summary.Key), "-"), AgeReasonHistorySummary)
	if last := report.Requests[len(report.Requests)-1]; last.ID != want.ID {
		t.Fatalf("last request = %+v, want the content-addressed history judgment %q", last, want.ID)
	}
	got := projection.Entries()
	if len(got) != 5 {
		t.Fatalf("projection holds %d entries, want the summary beside the whole new band: %v", len(got), got)
	}
	if got[0].Type != ProjectionTypeHistorySummary || got[0].Key != summary.Key {
		t.Fatalf("first entry = %+v, want the history summary leading the projection", got[0])
	}
	if got[1].Key != "t4-in" || got[2].Key != "t4-plan-a" || got[3].Key != "t4-cmd-a" || got[4].Key != "t4-out" {
		t.Fatalf("new band = %v, want the newest turn kept whole beside the summary", got[1:])
	}
	for _, entry := range got {
		if strings.HasPrefix(string(entry.Key), "t1-") || strings.HasPrefix(string(entry.Key), "t2-") || strings.HasPrefix(string(entry.Key), "t3-") {
			t.Fatalf("entry %+v still stands in the timeline, want the older bands replaced by the summary", entry)
		}
	}
}

// TestAgeFoldPromotesTheGenerationsAcrossFolds pins the generational recursion of the three bands.
//   - the turn the window drops reads the middle band, and its process detail folds into one product
//   - the next fold reads the same turn in the oldest band, and the summary covers it
//   - a band is decided by the fold that consumed the material, never by the distance the turn sits from the newest one
func TestAgeFoldPromotesTheGenerationsAcrossFolds(t *testing.T) {
	ctx := context.Background()
	var entries []ProjectionEntry
	for step, seed := range []string{"t1", "t2", "t3"} {
		entries = append(entries, ageTurn(seed, step, 1)...)
	}
	projection := ageProjection(t, entries)
	plan := AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600}
	bandOfTurn := func(seed string) (Band, bool) {
		for _, segment := range SegmentsOf(projection.Entries(), 1) {
			for _, entry := range segment.Entries {
				if string(entry.Key) == seed+"-in" {
					return segment.Band, true
				}
			}
		}
		return BandNew, false
	}

	if band, held := bandOfTurn("t1"); !held || band != BandMid {
		t.Fatalf("t1 reads the %v band (held %v) before the first fold, want the middle band it enters when the window drops it", band, held)
	}
	first, err := projection.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("first FoldByAge: %v", err)
	}
	if len(first.Products) != 2 {
		t.Fatalf("first fold = %+v, want the two middle-band turns folded into one product each", first)
	}
	if band, held := bandOfTurn("t1"); !held || band != BandOld {
		t.Fatalf("t1 reads the %v band (held %v) after its fold, want the oldest band the product it now carries names", band, held)
	}
	if band, _ := bandOfTurn("t3"); band != BandNew {
		t.Fatalf("t3 reads the %v band, want the newest turn still in the new band", band)
	}

	second, err := projection.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("second FoldByAge: %v", err)
	}
	if len(second.Products) != 1 || second.Products[0].Type != ProjectionTypeHistorySummary {
		t.Fatalf("second fold = %+v, want the oldest band folded into the one history summary", second)
	}
	if _, held := bandOfTurn("t1"); held {
		t.Fatalf("t1 still stands in the timeline, want the history summary to cover the turn after its second generation")
	}
	for _, wantRead := range []string{"external_input: input t1", "agent_output: output t1"} {
		if !strings.Contains(second.Products[0].Content, wantRead) {
			t.Fatalf("summary = %q, want the one-line read %q of the turn it summarized", second.Products[0].Content, wantRead)
		}
	}
}

// TestAgeFoldMintsTheSameProductsForTheSameMaterial pins the determinism of the fold.
//   - the same material folds under the same product keys and the same request identities
//   - a second fold of what is already folded mints no second product and moves nothing
func TestAgeFoldMintsTheSameProductsForTheSameMaterial(t *testing.T) {
	ctx := context.Background()
	entries := append(ageTurn("t1", 0, 2), ageTurn("t2", 1, 2)...)
	plan := AgeFoldPlan{KeepRecent: DefaultAgeKeepRecent, VolumeThreshold: 600}
	first, err := ageProjection(t, entries).FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("first FoldByAge: %v", err)
	}
	second, err := ageProjection(t, entries).FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("second FoldByAge: %v", err)
	}
	if len(first.Products) != len(second.Products) {
		t.Fatalf("products = %d and %d, want the same fold over the same material", len(first.Products), len(second.Products))
	}
	for i := range first.Products {
		if first.Products[i].Key != second.Products[i].Key || first.Products[i].Content != second.Products[i].Content {
			t.Fatalf("product %d = %+v and %+v, want content-addressed identity", i, first.Products[i], second.Products[i])
		}
		if first.Requests[i].ID != second.Requests[i].ID {
			t.Fatalf("request %d = %q and %q, want one identity per judgment", i, first.Requests[i].ID, second.Requests[i].ID)
		}
	}

	projection := ageProjection(t, entries)
	if _, err := projection.FoldByAge(ctx, plan); err != nil {
		t.Fatalf("first FoldByAge: %v", err)
	}
	folded := RenderProjection(projection.Entries())
	again, err := projection.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("second FoldByAge: %v", err)
	}
	if again.NewFolded != 0 || len(again.Products) != 0 {
		t.Fatalf("second fold = %+v, want no second product over material already folded", again)
	}
	if string(RenderProjection(projection.Entries())) != string(folded) {
		t.Fatal("projection moved on a fold that found nothing left to fold")
	}
}

// TestAgeFoldKeepsTheSegmentInProgressWhole pins the pending turn against the fold.
//   - the process detail of the segment in progress is never folded
//   - the closed turns the window dropped are folded, so the tail keeps its whole detail
//   - the newest closed turn keeps its detail as well: the new band is never folded either
func TestAgeFoldKeepsTheSegmentInProgressWhole(t *testing.T) {
	ctx := context.Background()
	entries := append(ageTurn("t1", 0, 2), ageTurn("t2", 1, 2)...)
	entries = append(entries,
		ProjectionEntry{Key: "t3-in", Type: event.TypeExternalInput, Summary: "input t3", Content: strings.Repeat("进行中的输入。", 20), Time: ageBase.Add(time.Hour)},
		ProjectionEntry{Key: "t3-plan", Type: event.TypeThinkingPlan, Summary: "plan t3", Content: strings.Repeat("进行中的过程。", 20), Time: ageBase.Add(time.Hour + time.Minute)},
		ProjectionEntry{Key: "t3-cmd", Type: event.TypeActionCommand, Summary: "call t3", Content: strings.Repeat("进行中的工具。", 20), Time: ageBase.Add(time.Hour + 2*time.Minute)},
	)
	projection := ageProjection(t, entries)

	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: 2, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if report.NewFolded != 4 {
		t.Fatalf("middle-band folded entries = %d, want only the four process entries of the turn the window dropped", report.NewFolded)
	}
	whole := append(ageTurn("t2", 1, 2), ProjectionEntry{Key: "t3-in"}, ProjectionEntry{Key: "t3-plan"}, ProjectionEntry{Key: "t3-cmd"})
	for _, kept := range whole {
		found := false
		for _, entry := range projection.Entries() {
			if entry.Key == kept.Key {
				found = true
			}
		}
		if !found {
			t.Fatalf("projection lost %q, want the segment in progress beside the whole new band", kept.Key)
		}
	}
}

// TestRefoldReadsTypeNotPrefix pins the judgment a later history fold reads a standing product by: its Type, never its text.
//   - a standing history summary is judged a product by its Type and contributes a bounded reference to its own key
//   - the text it stands for is never copied into the next summary, so folding a summary does not re-carry the material it compressed
//   - the summary a fold mints spells no type label of its own, so what kind of product it is stays its Type
//   - the round the summary is minted is the one whose oldest band carries the standing summary beside the turns they folded
func TestRefoldReadsTypeNotPrefix(t *testing.T) {
	ctx := context.Background()
	const early = "EARLY-INPUT: the first turn of the session"
	plan := AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600}
	standing := ProjectionEntry{
		Key:     "-hist-standing",
		Type:    ProjectionTypeHistorySummary,
		Summary: "history summary",
		Content: "external_input: " + early + "; agent_output: the first answer",
		Time:    ageBase,
	}
	entries := []ProjectionEntry{standing}
	for step, seed := range []string{"t2", "t3", "t4"} {
		entries = append(entries, ageTurn(seed, step, 1)...)
	}
	projection := ageProjection(t, entries)

	first, err := projection.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("first FoldByAge: %v", err)
	}
	if !first.Triggered {
		t.Fatalf("report = %+v, want the turns to pass the volume threshold", first)
	}
	for _, product := range first.Products {
		if product.Type != ProjectionTypeSegmentFold {
			t.Fatalf("first fold = %+v, want the skeleton of the turns the window dropped beside the standing summary", first.Products)
		}
	}

	report, err := projection.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("second FoldByAge: %v", err)
	}
	if !report.Triggered || len(report.Products) == 0 {
		t.Fatalf("report = %+v, want the oldest band to fold into the one history summary", report)
	}
	summary := report.Products[len(report.Products)-1]
	if summary.Type != ProjectionTypeHistorySummary {
		t.Fatalf("product = %+v, want the history summary of the older bands", summary)
	}
	if strings.Contains(summary.Content, early) {
		t.Fatalf("history summary content = %q, want the standing product referenced by key instead of copied", summary.Content)
	}
	if !strings.Contains(summary.Content, string(standing.Key)) {
		t.Fatalf("history summary content = %q, want the bounded reference to the standing product's key %q", summary.Content, standing.Key)
	}
	if strings.Contains(summary.Content, ProjectionTypeHistorySummary+": ") {
		t.Fatalf("history summary content = %q, want no type label written into the material", summary.Content)
	}
	if !strings.Contains(summary.Content, "external_input: input t2") {
		t.Fatalf("history summary content = %q, want the turn folded beside the standing product", summary.Content)
	}
}

// foldAnchorFact is one recorded fact of the cross-restart chain: the event
// envelope that carries its key, frozen type, explicit RFC3339 timestamp and
// recorder generation, beside the one-line read the projection stores.
type foldAnchorFact struct {
	Envelope event.Event
	Summary  string
	Content  string
}

// projectionEntry maps one recorded fact onto the projection entry shape the
// replay path stores: the five public fields assembly/facts.go mappedEntryOf
// fills (key, type, summary, content, time) beside the recorder generation the
// envelope's InstanceID names, copied through the entry's one private setter
// exactly as the fact→projection mapping copies it. The envelope carries both the
// explicit timestamp and the generation, so the entry below drops neither — the
// fold reads them back from the entry it is handed.
func (f foldAnchorFact) projectionEntry() ProjectionEntry {
	return WithInstanceID(ProjectionEntry{
		Key:     f.Envelope.Key,
		Type:    f.Envelope.Type,
		Summary: f.Summary,
		Content: f.Content,
		Time:    f.Envelope.Timestamp,
	}, f.Envelope.InstanceID)
}

// TestSymptomAnchorFoldSummaryProvenance pins the provenance an age-fold history summary must carry onto the model-visible text.
//
// - 轨迹症状：折叠摘要行只拼 entry.Type+": "+entry.Summary，事件时刻与 recorder 世代一起被剥掉，跨重启回放的历史退化成无时序、无世代的团块。
// - 钉住底线：history summary 的每一行必须以 [MM-DD HH:MM|gen:…] 开头，时刻取自被折叠条目自己的显式时间戳，而不是折叠发生的时刻。
// - 世代承载：事件信封（event.Event.InstanceID）携带 recorder 世代，经 Generation() 读取、WithInstanceID 设置，映射到投影入口（assembly/facts.go mappedEntryOf）时逐字透传。
// - 有世代信息时必须给出完整 InstanceID 并与该行事实逐字相等；没有时以 gen:? 显式标注、不得伪造。
// - 取数面：折叠后投影里的 history summary 条目正文本身（模型可见面），不用内部行函数或日志。
// - 覆盖来源：第二折把第一折消化掉的材料当作最老带，故它产出的摘要覆盖的键就是那一折之前的投影条目，每行的世代从那批条目逐字读回。
// - 场景：两个 recorder 世代（重启前三个回合、重启后最新回合），时刻全部是显式 RFC3339、跨日且逐条不同。
func TestSymptomAnchorFoldSummaryProvenance(t *testing.T) {
	ctx := context.Background()

	const (
		generationBefore = event.InstanceID("recorder-1a2b3c4d")
		generationAfter  = event.InstanceID("recorder-5e6f7a8b")
		perTurn          = 4
	)
	parseMoment := func(text string) time.Time {
		t.Helper()
		moment, err := time.Parse(time.RFC3339, text)
		if err != nil {
			t.Fatalf("parse RFC3339 timestamp %q: %v", text, err)
		}
		return moment
	}
	envelope := func(key, typ string, generation event.InstanceID, moment time.Time) event.Event {
		return event.Event{Key: event.EventID(key), Type: typ, Timestamp: moment, InstanceID: generation}
	}
	turn := func(seed string, generation event.InstanceID, opened string) []foldAnchorFact {
		at := parseMoment(opened)
		return []foldAnchorFact{
			{
				Envelope: envelope(seed+"-in", event.TypeExternalInput, generation, at),
				Summary:  "input " + seed,
				Content:  strings.Repeat("用户输入 "+seed+"。", 20),
			},
			{
				Envelope: envelope(seed+"-plan", event.TypeThinkingPlan, generation, at.Add(6*time.Minute)),
				Summary:  "plan " + seed,
				Content:  strings.Repeat("过程细节 "+seed+"。", 20),
			},
			{
				Envelope: envelope(seed+"-cmd", event.TypeActionCommand, generation, at.Add(7*time.Minute)),
				Summary:  "call " + seed,
				Content:  strings.Repeat("工具输出 "+seed+"。", 20),
			},
			{
				Envelope: envelope(seed+"-out", event.TypeAgentOutput, generation, at.Add(9*time.Minute)),
				Summary:  "output " + seed,
				Content:  strings.Repeat("回答 "+seed+"。", 20),
			},
		}
	}
	var chain []foldAnchorFact
	chain = append(chain, turn("t1", generationBefore, "2026-11-03T21:05:00+08:00")...)
	chain = append(chain, turn("t2", generationBefore, "2026-11-03T22:07:00+08:00")...)
	chain = append(chain, turn("t3", generationBefore, "2026-11-03T23:09:00+08:00")...)
	chain = append(chain, turn("t4", generationAfter, "2026-11-04T08:01:00+08:00")...)

	entries := make([]ProjectionEntry, 0, len(chain))
	for _, fact := range chain {
		entries = append(entries, fact.projectionEntry())
	}
	projection := ageProjection(t, entries)

	plan := AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600}
	first, err := projection.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("first FoldByAge: %v", err)
	}
	if !first.Triggered || len(first.Products) != 3 {
		t.Fatalf("first fold = %+v, want the three turns the window dropped folded into one product each", first)
	}
	covering := projection.Entries()
	second := foldOnce(t, projection, plan)
	if len(second) != 1 || second[0].Entry.Type != ProjectionTypeHistorySummary {
		t.Fatalf("second fold products = %+v, want the one history summary the oldest band folds into", second)
	}

	kept := projection.Entries()
	if len(kept) == 0 || kept[0].Type != ProjectionTypeHistorySummary {
		t.Fatalf("projection = %+v, want the history summary leading the model-visible sequence", kept)
	}
	summary := kept[0]
	lines := strings.Split(summary.Content, "; ")
	covered := second[0].Covered
	sourceOf := func(key event.EventID) (ProjectionEntry, bool) {
		for _, entry := range covering {
			if entry.Key == key {
				return entry, true
			}
		}
		return ProjectionEntry{}, false
	}
	dump := func() string {
		var out strings.Builder
		for i, line := range lines {
			generation := ""
			if i < len(covered) {
				if entry, held := sourceOf(covered[i]); held {
					generation = string(entry.Generation())
				}
			}
			fmt.Fprintf(&out, "\n  line %d: %q (recorder generation %q)", i, line, generation)
		}
		return out.String()
	}
	prefix := foldAnchorLine
	if len(lines) != len(covered) {
		t.Fatalf("history summary has %d lines, want %d (one per covered entry); folded lines:%s",
			len(lines), len(covered), dump())
	}
	for i, line := range lines {
		source, held := sourceOf(covered[i])
		if !held {
			t.Fatalf("the summary covers %q, which the projection before the fold does not carry; folded lines:%s", covered[i], dump())
		}
		wantGeneration := string(source.Generation())
		if wantGeneration == "" {
			wantGeneration = "?"
		}
		match := prefix.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("history summary line %d = %q, want a [MM-DD HH:MM|gen:%s] prefix (gen MUST equal the exact recorder generation %q of the folded fact; only gen:? may stand in for a generation no fact names); folded lines:%s",
				i, line, wantGeneration, wantGeneration, dump())
		}
		if want := source.Time.Format("01-02 15:04"); match[1] != want {
			t.Fatalf("history summary line %d timestamp = %q, want %q from the folded entry; folded lines:%s",
				i, match[1], want, dump())
		}
		if match[2] != wantGeneration {
			t.Fatalf("history summary line %d generation = %q, want the exact recorder generation %q of the folded fact (only gen:? may stand in for a generation no fact names); folded lines:%s",
				i, match[2], wantGeneration, dump())
		}
		wantRead := source.Type + ": " + source.Summary
		if source.Type == ProjectionTypeSegmentFold || source.Type == ProjectionTypeHistorySummary {
			wantRead = source.Summary + " (" + string(source.Key) + ")"
		}
		if !strings.Contains(line, wantRead) {
			t.Fatalf("history summary line %d = %q, want the folded read %q after the provenance prefix; folded lines:%s",
				i, line, wantRead, dump())
		}
	}
}

// TestFoldLineWithoutProvenanceDegrades pins the explicit placeholder a fold line carries when the folded entry names no recorder generation.
//
// - Every line of the history summary and of every segment product opens with [MM-DD HH:MM|gen:?], and the fold does not fail.
// - The moment still comes from the folded entry itself, so only the generation is missing.
// - No generation is invented for an entry that named none.
func TestFoldLineWithoutProvenanceDegrades(t *testing.T) {
	ctx := context.Background()
	var entries []ProjectionEntry
	for step, seed := range []string{"t1", "t2", "t3", "t4"} {
		entries = append(entries, ageTurn(seed, step, 1)...)
	}
	projection := ageProjection(t, entries)
	plan := AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600}
	first, err := projection.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("first FoldByAge: %v", err)
	}
	if !first.Triggered || len(first.Products) != 3 {
		t.Fatalf("first fold = %+v, want the three turns the window dropped folded into one product each", first)
	}
	covering := projection.Entries()
	second := foldOnce(t, projection, plan)
	if len(second) == 0 {
		t.Fatalf("second fold = %+v, want the oldest band folded into the history summary", second)
	}
	requireDegraded := func(name, content string, folded []ProjectionEntry) {
		t.Helper()
		lines := strings.Split(content, "; ")
		if len(lines) != len(folded) {
			t.Fatalf("%s has %d lines, want %d (one per folded entry): %q", name, len(lines), len(folded), content)
		}
		for i, line := range lines {
			match := foldAnchorLine.FindStringSubmatch(line)
			if match == nil {
				t.Fatalf("%s line %d = %q, want a [MM-DD HH:MM|gen:?] prefix", name, i, line)
			}
			if match[2] != "?" {
				t.Fatalf("%s line %d generation = %q, want the explicit gen:? placeholder beside an entry that named no generation", name, i, match[2])
			}
			if want := folded[i].Time.Format("01-02 15:04"); match[1] != want {
				t.Fatalf("%s line %d timestamp = %q, want %q from the folded entry", name, i, match[1], want)
			}
			if want := folded[i].Type + ": " + folded[i].Summary; !strings.Contains(line, want) {
				reference := folded[i].Summary + " (" + string(folded[i].Key) + ")"
				if folded[i].Type != ProjectionTypeSegmentFold && folded[i].Type != ProjectionTypeHistorySummary || !strings.Contains(line, reference) {
					t.Fatalf("%s line %d = %q, want the folded read %q after the placeholder anchor (a product contributes the bounded reference %q)", name, i, line, want, reference)
				}
			}
		}
	}
	summary := second[len(second)-1]
	if summary.Entry.Type != ProjectionTypeHistorySummary {
		t.Fatalf("last product = %+v, want the history summary of the oldest band", summary)
	}
	covered := make([]ProjectionEntry, 0, len(summary.Covered))
	for _, key := range summary.Covered {
		for _, entry := range covering {
			if entry.Key == key {
				covered = append(covered, entry)
			}
		}
	}
	if len(covered) != len(summary.Covered) {
		t.Fatalf("the summary covers %v, want every key of the projection before the fold", summary.Covered)
	}
	requireDegraded("history summary", summary.Entry.Content, covered)
	segment := first.Products[0]
	if segment.Type != ProjectionTypeSegmentFold {
		t.Fatalf("first fold products = %+v, want the turns the window dropped folded into segment products", first.Products)
	}
	requireDegraded("segment fold", segment.Content, entries[1:3])
}

// TestSegmentFoldCarriesProvenance pins the third formula at the segment fold.
//
// - The product's content opens with [MM-DD HH:MM|gen:…] at its first character, behind no "folded segment process:" label.
// - Every source line keeps the moment and the full recorder generation of the process entry it folded, never a truncated identity.
// - Two generations folded in one projection stay distinguishable.
func TestSegmentFoldCarriesProvenance(t *testing.T) {
	ctx := context.Background()
	const (
		before = event.InstanceID("recorder-01a2b3c4d5")
		after  = event.InstanceID("recorder-9f8e7d6c5b")
	)
	entries := append(ageTurn("t1", 0, 1), ageTurn("t2", 1, 1)...)
	entries = append(entries, ageTurn("t3", 2, 1)...)
	for i := range entries {
		generation := before
		if strings.HasPrefix(string(entries[i].Key), "t2") {
			generation = after
		}
		entries[i] = WithInstanceID(entries[i], generation)
	}
	projection := ageProjection(t, entries)
	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if !report.Triggered || len(report.Products) != 2 {
		t.Fatalf("report = %+v, want one segment product per turn the window dropped", report)
	}
	folded := []ProjectionEntry{entries[1], entries[2], entries[5], entries[6]}
	at := 0
	for _, product := range report.Products {
		if product.Type != ProjectionTypeSegmentFold {
			t.Fatalf("product = %+v, want a segment-fold product", product)
		}
		if !strings.HasPrefix(product.Content, "[") {
			t.Fatalf("product content = %q, want the anchor at the first character: 行首即锚点", product.Content)
		}
		if strings.Contains(product.Content, "folded segment process") {
			t.Fatalf("product content = %q, want the old label gone from the head", product.Content)
		}
		lines := strings.Split(product.Content, "; ")
		if len(lines) != 2 {
			t.Fatalf("product content = %q, want the two folded source lines", product.Content)
		}
		for _, line := range lines {
			source := folded[at]
			at++
			match := foldAnchorLine.FindStringSubmatch(line)
			if match == nil {
				t.Fatalf("source line = %q, want a [MM-DD HH:MM|gen:…] prefix", line)
			}
			if want := string(source.Generation()); match[2] != want {
				t.Fatalf("source line generation = %q, want the full recorder generation %q of the folded entry", match[2], want)
			}
			if want := source.Time.Format("01-02 15:04"); match[1] != want {
				t.Fatalf("source line timestamp = %q, want %q from the folded entry", match[1], want)
			}
			if want := source.Type + ": " + source.Summary; !strings.Contains(line, want) {
				t.Fatalf("source line = %q, want the folded read %q after the anchor", line, want)
			}
		}
	}
	if at != len(folded) {
		t.Fatalf("folded source lines = %d, want the %d process entries", at, len(folded))
	}
	if !strings.Contains(report.Products[0].Content, "gen:"+string(before)+"]") || !strings.Contains(report.Products[1].Content, "gen:"+string(after)+"]") {
		t.Fatalf("products = %q and %q, want the two recorder generations distinguishable", report.Products[0].Content, report.Products[1].Content)
	}
}

// TestSecondFoldKeepsProvenance pins the third formula end to end.
//
// - A segment-fold product folded a second time still carries its source lines' provenance.
// - The second fold reads the segment product's content, so the history summary opens with [MM-DD HH:MM|gen:…] at its first character.
// - Every summary line keeps a moment and a full generation, and the source lines of the earlier product stay readable after the second fold.
func TestSecondFoldKeepsProvenance(t *testing.T) {
	ctx := context.Background()
	const (
		before = event.InstanceID("recorder-1a2b3c4d")
		after  = event.InstanceID("recorder-5e6f7a8b")
	)
	firstEntries := append(ageTurn("t1", 0, 1), ageTurn("t2", 1, 1)...)
	for i := range firstEntries {
		generation := before
		if strings.HasPrefix(string(firstEntries[i].Key), "t2") {
			generation = after
		}
		firstEntries[i] = WithInstanceID(firstEntries[i], generation)
	}
	firstReport, err := ageProjection(t, firstEntries).FoldByAge(ctx, AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("first FoldByAge: %v", err)
	}
	var segment ProjectionEntry
	found := false
	for _, product := range firstReport.Products {
		if product.Type == ProjectionTypeSegmentFold {
			segment, found = product, true
		}
	}
	if !found || !foldAnchorLine.MatchString(segment.Content) {
		t.Fatalf("first fold products = %+v, want an anchored segment product to fold a second time", firstReport.Products)
	}

	secondEntries := []ProjectionEntry{segment}
	for _, turn := range [][]ProjectionEntry{ageTurn("t3", 2, 0), ageTurn("t4", 3, 0)} {
		for _, entry := range turn {
			secondEntries = append(secondEntries, WithInstanceID(entry, before))
		}
	}
	projection := ageProjection(t, secondEntries)
	secondReport, err := projection.FoldByAge(ctx, AgeFoldPlan{
		KeepRecent:      1,
		VolumeThreshold: len(RenderProjection(secondEntries)) / 2,
	})
	if err != nil {
		t.Fatalf("second FoldByAge: %v", err)
	}
	if !secondReport.Triggered || len(secondReport.Products) == 0 {
		t.Fatalf("second report = %+v, want the product-bearing turn folded into the history summary", secondReport)
	}
	summary := secondReport.Products[len(secondReport.Products)-1]
	if summary.Type != ProjectionTypeHistorySummary {
		t.Fatalf("last product = %+v, want the history summary of the older band", summary)
	}
	if summary.Content == "" || summary.Content[0] != '[' {
		t.Fatalf("history summary content = %q, want the anchor at the first character after the second fold", summary.Content)
	}
	if strings.Contains(summary.Content, "folded segment process") {
		t.Fatalf("history summary content = %q, want the old label gone from the head", summary.Content)
	}
	lines := strings.Split(summary.Content, "; ")
	if len(lines) != 3 {
		t.Fatalf("history summary has %d lines, want the segment product's bounded reference beside the two closing turn lines: %q", len(lines), summary.Content)
	}
	for i, line := range lines {
		match := foldAnchorLine.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("history summary line %d = %q, want a [MM-DD HH:MM|gen:…] prefix at the head", i, line)
		}
	}
	if !strings.Contains(lines[0], string(segment.Key)) {
		t.Fatalf("history summary line 0 = %q, want the bounded reference to the segment product %q", lines[0], segment.Key)
	}
	if strings.Contains(summary.Content, "plan t2a") {
		t.Fatalf("history summary content = %q, want the segment product referenced by key instead of its source lines copied", summary.Content)
	}
	if !strings.Contains(summary.Content, "|gen:"+string(before)+"] external_input: input t3") {
		t.Fatalf("history summary content = %q, want the closing turn's line beside its own generation", summary.Content)
	}
}

// TestFoldProvenanceConsumerPath pins that provenance reaches the consumer the request is assembled for.
//
// - The history summary entry the budget assembly presents still opens with the anchored lines, and any segment product it presents does the same.
// - The presented lines name the full generations of the facts they came from, so two recorder generations stay distinguishable on the consumer's side.
// - The material the summary stands for reaches the model through a fold that promotes it twice: the middle band folds, then the oldest band summarizes.
func TestFoldProvenanceConsumerPath(t *testing.T) {
	ctx := context.Background()
	const (
		before = event.InstanceID("recorder-1a2b3c4d")
		after  = event.InstanceID("recorder-5e6f7a8b")
	)
	var entries []ProjectionEntry
	for step, seed := range []string{"t1", "t2", "t3", "t4"} {
		generation := before
		if seed == "t3" || seed == "t4" {
			generation = after
		}
		for _, entry := range ageTurn(seed, step, 1) {
			entries = append(entries, WithInstanceID(entry, generation))
		}
	}
	projection := ageProjection(t, entries)
	plan := AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600}
	if _, err := projection.FoldByAge(ctx, plan); err != nil {
		t.Fatalf("first FoldByAge: %v", err)
	}
	if _, err := projection.FoldByAge(ctx, plan); err != nil {
		t.Fatalf("second FoldByAge: %v", err)
	}
	assembled, err := projection.AssembleBudget(ctx, 1<<20)
	if err != nil {
		t.Fatalf("AssembleBudget: %v", err)
	}
	var summary *ProjectionEntry
	generations := map[string]bool{}
	for i := range assembled {
		switch assembled[i].Type {
		case ProjectionTypeHistorySummary:
			summary = &assembled[i]
		case ProjectionTypeSegmentFold:
			if !foldAnchorLine.MatchString(assembled[i].Content) {
				t.Fatalf("assembled segment product = %q, want the anchor at the head of the material the model reads", assembled[i].Content)
			}
			for _, line := range strings.Split(assembled[i].Content, "; ") {
				match := foldAnchorLine.FindStringSubmatch(line)
				if match == nil {
					t.Fatalf("assembled segment line = %q, want a [MM-DD HH:MM|gen:…] prefix", line)
				}
				generations[match[2]] = true
			}
		}
	}
	if summary == nil {
		t.Fatalf("assembled = %+v, want the history summary in the material the model reads", assembled)
	}
	if summary.Content == "" || summary.Content[0] != '[' {
		t.Fatalf("assembled history summary = %q, want the anchor at the first character", summary.Content)
	}
	for i, line := range strings.Split(summary.Content, "; ") {
		match := foldAnchorLine.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("assembled summary line %d = %q, want a [MM-DD HH:MM|gen:…] prefix", i, line)
		}
		generations[match[2]] = true
	}
	if !generations[string(before)] || !generations[string(after)] {
		t.Fatalf("assembled generations = %v, want the replayed %q beside the latest %q", generations, before, after)
	}
}

// TestSegmentLegacyMarker pins the diagnostic mark a segment carries when the output that closed it recorded no readable input relation.
//   - a segment closed by an output that names its inputs carries no mark
//   - an output without the relation marks its segment and keeps the chain-order cut, so the historical chain still reads positionally
//   - an unreadable relation is no relation and marks the same way
//   - a segment still in progress is never marked, because no output closed it
func TestSegmentLegacyMarker(t *testing.T) {
	legacyInput := func(key string) ProjectionEntry {
		return ProjectionEntry{Key: event.EventID(key), Type: event.TypeExternalInput, Summary: "input " + key, Content: "用户输入 " + key + "。", Time: ageBase}
	}
	legacyOutput := func(key, relation string) ProjectionEntry {
		entry := ProjectionEntry{Key: event.EventID(key), Type: event.TypeAgentOutput, Summary: "output " + key, Content: "回答 " + key + "。", Time: ageBase}
		if relation == "" {
			return entry
		}
		return WithInputEvents(entry, relation)
	}
	marksOf := func(segments []Segment) []bool {
		marks := make([]bool, 0, len(segments))
		for _, segment := range segments {
			marks = append(marks, segment.Legacy())
		}
		return marks
	}

	t.Run("the relation clears the mark", func(t *testing.T) {
		entries := []ProjectionEntry{
			legacyInput("i-1"), legacyInput("i-2"), legacyOutput("a", `["i-1","i-2"]`),
		}
		segments := SegmentsOf(entries, DefaultAgeKeepRecent)
		if len(segments) != 1 {
			t.Fatalf("segments = %d, want the one the output closed", len(segments))
		}
		if segments[0].Legacy() {
			t.Fatalf("segment = %+v answered legacy, want the mark cleared by the relation the output recorded", segments[0])
		}
	})

	t.Run("an output without the relation marks its segment", func(t *testing.T) {
		entries := []ProjectionEntry{
			legacyInput("i-1"), legacyOutput("a", ""),
			legacyInput("i-2"), legacyOutput("b", ""),
		}
		segments := SegmentsOf(entries, DefaultAgeKeepRecent)
		if got := marksOf(segments); !slices.Equal(got, []bool{true, true}) {
			t.Fatalf("marks = %v, want both segments marked legacy", got)
		}
		if len(segments) != 2 {
			t.Fatalf("segments = %d, want the chain-order cut of the two turns", len(segments))
		}
		for at, segment := range segments {
			if !segment.Complete {
				t.Fatalf("segment %d = %+v, want the output to close it", at, segment)
			}
		}
	})

	t.Run("an unreadable relation marks the same way", func(t *testing.T) {
		for name, relation := range map[string]string{
			"empty array":      "[]",
			"empty identities": `["",""]`,
			"null":             "null",
			"not a list":       `"i-1"`,
			"unparseable":      `{"input_events":`,
		} {
			t.Run(name, func(t *testing.T) {
				entries := []ProjectionEntry{legacyInput("i-1"), legacyOutput("a", relation)}
				segments := SegmentsOf(entries, DefaultAgeKeepRecent)
				if len(segments) != 1 || !segments[0].Legacy() {
					t.Fatalf("segments = %+v, want the one closed segment marked legacy by %q", segments, relation)
				}
			})
		}
	})

	t.Run("a segment in progress is never marked", func(t *testing.T) {
		entries := []ProjectionEntry{
			legacyInput("i-1"), legacyOutput("a", ""),
			legacyInput("i-2"),
		}
		segments := SegmentsOf(entries, DefaultAgeKeepRecent)
		if len(segments) != 2 {
			t.Fatalf("segments = %d, want the closed turn beside the tail in progress", len(segments))
		}
		if !segments[0].Legacy() {
			t.Fatalf("closed segment = %+v, want the mark of its output", segments[0])
		}
		if segments[1].Complete || segments[1].Legacy() {
			t.Fatalf("tail segment = %+v, want a segment in progress carrying no mark", segments[1])
		}
	})
}

// TestProjectionEntryFaceUnchanged pins the public face of one projection entry.
//   - the diagnostic mark is read from the segment, never from the entry, so the entry a consumer reads stays the five fields of a projection record
//   - the entry exports exactly the five fields of a projection record, in order
//   - the entry exposes no method for that mark, so a consumer cannot read it off the entry
//   - the field check rejects a widened record, so the pin is not satisfied by any type it is handed
func TestProjectionEntryFaceUnchanged(t *testing.T) {
	exportedNames := func(typ reflect.Type) []string {
		names := make([]string, 0, typ.NumField())
		for at := 0; at < typ.NumField(); at++ {
			if field := typ.Field(at); field.PkgPath == "" {
				names = append(names, field.Name)
			}
		}
		return names
	}
	want := []string{"Key", "Type", "Summary", "Content", "Time"}
	typ := reflect.TypeOf(ProjectionEntry{})
	if exported := exportedNames(typ); !slices.Equal(exported, want) {
		t.Fatalf("ProjectionEntry exports %v, want the five fields of a projection record %v", exported, want)
	}
	for at := 0; at < typ.NumMethod(); at++ {
		if name := typ.Method(at).Name; name == "Legacy" {
			t.Fatalf("ProjectionEntry exposes %q, want the legacy mark read from the segment alone", name)
		}
	}
	if widened := exportedNames(reflect.TypeOf(widenedProbe{})); slices.Equal(widened, want) {
		t.Fatalf("a record exporting six fields answered %v, want the face check to reject it", widened)
	}
}

// widenedProbe is a projection-record stand-in carrying the sixth exported field
// the face check must reject, so the check never passes by reading no field at
// all.
type widenedProbe struct {
	Key     string
	Type    string
	Summary string
	Content string
	Time    time.Time
	Legacy  bool
}

// foldOnce judges and executes one age fold the way the production pair does:
// the producer judges through PlanByAge and the consumer executes through
// ApplyProducts, so the invariants below read the path the model-visible
// projection is actually built on rather than a test-only shortcut.
func foldOnce(t *testing.T, projection *ReplayProjection, plan AgeFoldPlan) []FoldProduct {
	t.Helper()
	report, err := projection.PlanByAge(context.Background(), plan)
	if err != nil {
		t.Fatalf("PlanByAge: %v", err)
	}
	projection.ApplyProducts(report.Products)
	return report.Products
}

// TestSymptomAnchorCompressionStaysBounded pins the bound the compressed projection carries.
//
// - 轨迹症状：长程会话里每次折叠都把上一份 history summary 的正文整段抄进下一份，压缩产物随回合数线性膨胀（88MB，超日志帧上限）。
// - 钉住底线：任意回合数之后，投影渲染体积 ≤ 预算、history summary 正文 ≤ 界，二者都不随历史增长。
// - 覆盖完整：被摘要吃掉的原条目身份全部出现在产品的 Covered 里，原文按 key 仍可从链上读回。
// - 取数面：PlanByAge（生产判据）＋ ApplyProducts（生产执行）之后的投影本身。
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestSymptomAnchorCompressionStaysBounded(t *testing.T) {
	const budget = DefaultAgeSummaryTextLimit + 8*1024
	plan := AgeFoldPlan{KeepRecent: DefaultAgeKeepRecent, VolumeThreshold: 600}
	projection := NewReplayProjection()
	ctx := context.Background()
	appended := make([]event.EventID, 0, 4*300)
	covered := map[event.EventID]bool{}
	for step := 0; step < 300; step++ {
		for _, entry := range ageTurn(fmt.Sprintf("t%d", step), step, 2) {
			appended = append(appended, entry.Key)
			projection.Append(ctx, entry)
		}
		for _, product := range foldOnce(t, projection, plan) {
			for _, key := range product.Covered {
				covered[key] = true
			}
		}
		rendered := RenderProjection(projection.Entries())
		if len(rendered) > budget {
			t.Fatalf("after %d turns the projection renders %d bytes, want at most %d: the compressed view must not grow with the history it compresses", step+1, len(rendered), budget)
		}
		for _, entry := range projection.Entries() {
			if entry.Type == ProjectionTypeHistorySummary && len(entry.Content) > DefaultAgeSummaryTextLimit {
				t.Fatalf("after %d turns the history summary carries %d bytes, want at most %d", step+1, len(entry.Content), DefaultAgeSummaryTextLimit)
			}
		}
	}
	if bound := 1 + DefaultAgeKeepRecent*6 + 3; len(projection.Entries()) > bound {
		t.Fatalf("projection holds %d entries after 300 turns, want the summary beside the recent window and the segment that folded this round: at most %d", len(projection.Entries()), bound)
	}
	standing := map[event.EventID]bool{}
	for _, entry := range projection.Entries() {
		standing[entry.Key] = true
	}
	for _, key := range appended {
		if !standing[key] && !covered[key] {
			t.Fatalf("entry %q is neither in the projection nor covered by any product, want every original reachable by key", key)
		}
	}
}

// TestGlobalSummaryStandsApartFromTheRecentTurn pins the segment a standing history summary draws for itself.
//
// - a summary that covers several turns belongs to none of them, so it stands as one complete segment of its own
// - the turn the window dropped beside it reads the middle band and folds its process detail into one segment product
// - that turn's input and output stay whole around the product, so the newest material keeps its own text
// - the standing summary keeps the text it holds: it is neither re-carried nor widened by the turn beside it
// - 症状：摘要滚入首个 turn 段，该段因持有折叠产物被判最老带，turn 自有过程细节未经中段骨架折叠即进摘要
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestGlobalSummaryStandsApartFromTheRecentTurn(t *testing.T) {
	ctx := context.Background()
	standing := ProjectionEntry{
		Key:     "-hist-standing",
		Type:    ProjectionTypeHistorySummary,
		Summary: "history summary",
		Content: "external_input: ask t0; [09-30 08:00|gen:?] agent_output: answer t0",
		Time:    ageBase,
	}
	entries := []ProjectionEntry{standing}
	for step, seed := range []string{"t1", "t2", "t3"} {
		entries = append(entries, ageTurn(seed, step+1, 1)...)
	}
	projection := ageProjection(t, entries)
	plan := AgeFoldPlan{KeepRecent: DefaultAgeKeepRecent, VolumeThreshold: 600}

	entriesOf := func(keys ...event.EventID) []string {
		out := make([]string, 0, len(keys))
		for _, key := range keys {
			out = append(out, string(key))
		}
		return out
	}
	originalOf := func(want event.EventID) ProjectionEntry {
		for _, entry := range entries {
			if entry.Key == want {
				return entry
			}
		}
		return ProjectionEntry{}
	}
	bandOf := func(want event.EventID) (Band, bool) {
		for _, segment := range SegmentsOf(projection.Entries(), DefaultAgeKeepRecent) {
			for _, entry := range segment.Entries {
				if entry.Key == want {
					return segment.Band, true
				}
			}
		}
		return BandNew, false
	}
	if band, held := bandOf("t1-in"); !held || band != BandMid {
		t.Fatalf("the turn beside the standing summary reads the %v band (held %v), want the middle band its own fold generation names", band, held)
	}

	report, err := projection.FoldByAge(ctx, plan)
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if len(report.Products) != 1 || report.Products[0].Type != ProjectionTypeSegmentFold {
		t.Fatalf("products = %+v, want the one segment product the turn beside the summary folds into", report.Products)
	}
	product := report.Products[0]
	for _, want := range []string{"thinking_plan: plan t1", "action_command: call t1"} {
		if !strings.Contains(product.Content, want) {
			t.Fatalf("segment product = %q, want the one-line read %q of the turn it folds", product.Content, want)
		}
	}

	got := projection.Entries()
	for at, want := range []event.EventID{standing.Key, "t1-in", product.Key, "t1-out"} {
		if at >= len(got) || got[at].Key != want {
			t.Fatalf("projection = %v, want the standing summary, the turn's input, its product and its output in order", entriesOf(standing.Key, "t1-in", product.Key, "t1-out"))
		}
	}
	if got[0].Type != ProjectionTypeHistorySummary || got[0].Content != standing.Content {
		t.Fatalf("first entry = %+v, want the standing summary holding the text it stands for", got[0])
	}
	for _, absorbed := range []string{"plan t1", "call t1", "input t1", "output t1"} {
		if strings.Contains(got[0].Content, absorbed) {
			t.Fatalf("standing summary = %q, want no %q of the turn it stands beside: the turn folds into its own product", got[0].Content, absorbed)
		}
	}
	for at, key := range map[int]event.EventID{1: "t1-in", 3: "t1-out"} {
		if original := originalOf(key); got[at].Content != original.Content {
			t.Fatalf("entry %d = %+v, want the original text of the turn's own %q kept whole", at, got[at], key)
		}
	}
	summaries := 0
	for _, entry := range got {
		if entry.Type == ProjectionTypeHistorySummary {
			summaries++
		}
	}
	if summaries != 1 {
		t.Fatalf("projection carries %d history summaries, want the one standing summary it already held", summaries)
	}
}

// TestSymptomAnchorCompressionConverges pins the fold as a convergent compression.
//
// - 轨迹症状：折叠上一层 history summary 时把它的正文整段抄进下一份，摘要每折一次就重抄一遍，产物随折叠次数膨胀。
// - 钉住底线：站在投影里的摘要被下一份摘要引用时只贡献有界引用，产出的摘要正文 ≤ 界、不含它上一层的正文。
// - 晋升节奏：摘要自成一后才折它后面离开窗口的 turn（中段骨架），最老带把该摘要连同那些骨架折进下一份摘要的那一轮才诞生新摘要。
// - 收敛（r49 裁定）：世代递推下第一次折叠把材料晋一级、第二次再晋一级，故 fold² 不必等于 fold¹；不变量是迭代终有稳定不动点——某轮起产出零产品、投影逐字不变，且体积全程不超界。
// - 取数面：PlanByAge ＋ ApplyProducts 之后的投影本身。
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestSymptomAnchorCompressionConverges(t *testing.T) {
	const standingText = "旧回合的一行摘要。"
	const budget = DefaultAgeSummaryTextLimit + 8*1024
	plan := AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600}
	standing := ProjectionEntry{
		Key:     "-hist-standing",
		Type:    ProjectionTypeHistorySummary,
		Summary: "history summary",
		Content: strings.Repeat(standingText, 200),
		Time:    ageBase,
	}
	entries := []ProjectionEntry{standing}
	for step, seed := range []string{"t2", "t3", "t4"} {
		entries = append(entries, ageTurn(seed, step, 1)...)
	}
	projection := ageProjection(t, entries)

	var summary *ProjectionEntry
	for round := 0; round < 6 && summary == nil; round++ {
		for _, product := range foldOnce(t, projection, plan) {
			if product.Entry.Type == ProjectionTypeHistorySummary {
				entry := product.Entry
				summary = &entry
			}
		}
		if rendered := RenderProjection(projection.Entries()); len(rendered) > budget {
			t.Fatalf("round %d renders %d bytes, want at most %d: the compressed view must not grow with the folds it absorbs", round+1, len(rendered), budget)
		}
	}
	if summary == nil {
		t.Fatalf("projection = %+v, want the material a fold consumed read as one history summary", projection.Entries())
	}
	if len(summary.Content) > DefaultAgeSummaryTextLimit {
		t.Fatalf("history summary carries %d bytes, want at most %d: a summary must not copy the text it stands for", len(summary.Content), DefaultAgeSummaryTextLimit)
	}
	if strings.Contains(summary.Content, standingText) {
		t.Fatalf("history summary = %q, want the standing summary referenced by key instead of re-carried", summary.Content)
	}
	if !strings.Contains(summary.Content, string(standing.Key)) {
		t.Fatalf("history summary = %q, want the bounded reference to the standing summary %q", summary.Content, standing.Key)
	}

	fixed := false
	for round := 0; round < 6; round++ {
		before := RenderProjection(projection.Entries())
		products := foldOnce(t, projection, plan)
		if len(products) == 0 {
			if string(RenderProjection(projection.Entries())) != string(before) {
				t.Fatal("projection moved on a fold that found nothing left to fold")
			}
			fixed = true
			break
		}
		if rendered := RenderProjection(projection.Entries()); len(rendered) > budget {
			t.Fatalf("round %d renders %d bytes, want at most %d: the compressed view must not grow with the folds it absorbs", round+2, len(rendered), budget)
		}
		for _, entry := range projection.Entries() {
			if entry.Type == ProjectionTypeHistorySummary && len(entry.Content) > DefaultAgeSummaryTextLimit {
				t.Fatalf("round %d summary carries %d bytes, want at most %d", round+2, len(entry.Content), DefaultAgeSummaryTextLimit)
			}
		}
	}
	if !fixed {
		t.Fatalf("projection = %+v, want the folds to reach a stable fixed point", projection.Entries())
	}
	if products := foldOnce(t, projection, plan); len(products) != 0 {
		t.Fatalf("the fixed point minted %d further products: %+v", len(products), products)
	}
}
