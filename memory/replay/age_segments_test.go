package replay

import (
	"context"
	"fmt"
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

// TestAgeFoldFoldsTheNewBandIntoOneProduct pins the new→middle production of an age fold.
//   - the process detail of every closed new-band turn collapses into one product entry
//   - the product stands where the folded detail stood, between the input and the output
//   - the fold files one compress request per product, content-addressed over the material
func TestAgeFoldFoldsTheNewBandIntoOneProduct(t *testing.T) {
	ctx := context.Background()
	entries := append(ageTurn("t1", 0, 2), ageTurn("t2", 1, 2)...)
	projection := ageProjection(t, entries)

	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: DefaultAgeKeepRecent, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if !report.Triggered {
		t.Fatalf("report = %+v, want the fold to trigger past the threshold", report)
	}
	if report.NewFolded != 8 {
		t.Fatalf("new-band folded entries = %d, want the eight process entries of the two turns", report.NewFolded)
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
	if got := processDetail(projection.Entries()); got != 0 {
		t.Fatalf("projection still carries %d raw process entries, want the new band folded", got)
	}
	got := projection.Entries()
	if len(got) != 6 {
		t.Fatalf("projection holds %d entries, want the two turns as input, product and output: %v", len(got), got)
	}
	for i, want := range []struct {
		typ  string
		key  event.EventID
		time time.Time
	}{
		{event.TypeExternalInput, "t1-in", ageBase},
		{ProjectionTypeSegmentFold, report.Products[0].Key, ageBase.Add(time.Minute)},
		{event.TypeAgentOutput, "t1-out", ageBase.Add(9 * time.Minute)},
	} {
		if got[i].Type != want.typ || got[i].Key != want.key {
			t.Fatalf("entry %d = (%q, %q), want the folded turn laid out as %q then %q", i, got[i].Key, got[i].Type, want.key, want.typ)
		}
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
//   - the middle and oldest bands leave the timeline and read as one history summary
//   - the summary leads the projection, so the skeleton of the new band follows it
//   - the new band is folded the same way while the summary speaks for the older turns
func TestAgeFoldFoldsTheOlderBandsIntoOneHistorySummary(t *testing.T) {
	ctx := context.Background()
	var entries []ProjectionEntry
	for step, seed := range []string{"t1", "t2", "t3", "t4"} {
		entries = append(entries, ageTurn(seed, step, 1)...)
	}
	projection := ageProjection(t, entries)

	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if !report.Triggered || report.MidFolded != 12 {
		t.Fatalf("report = %+v, want the three older turns (four entries each) folded into the summary", report)
	}
	if len(report.Products) != 2 {
		t.Fatalf("products = %d, want the new band product and the history summary", len(report.Products))
	}
	summary := report.Products[len(report.Products)-1]
	if summary.Type != ProjectionTypeHistorySummary || !SyntheticKey(summary.Key) {
		t.Fatalf("history summary = %+v, want a synthetic summary entry", summary)
	}
	if !strings.Contains(summary.Content, "external_input: input t1") || !strings.Contains(summary.Content, "agent_output: output t3") {
		t.Fatalf("history summary content = %q, want the one-line reads of the older turns", summary.Content)
	}
	want := event.CompressRequestFor(AgeProducerSegmentAge, strings.TrimPrefix(string(summary.Key), "-"), AgeReasonHistorySummary)
	if last := report.Requests[len(report.Requests)-1]; last.ID != want.ID {
		t.Fatalf("last request = %+v, want the content-addressed history judgment %q", last, want.ID)
	}
	got := projection.Entries()
	if len(got) != 4 {
		t.Fatalf("projection holds %d entries, want the summary beside the folded new band: %v", len(got), got)
	}
	if got[0].Type != ProjectionTypeHistorySummary || got[0].Key != summary.Key {
		t.Fatalf("first entry = %+v, want the history summary leading the projection", got[0])
	}
	if got[1].Key != "t4-in" || got[2].Type != ProjectionTypeSegmentFold || got[3].Key != "t4-out" {
		t.Fatalf("new band = %v, want the newest turn kept as input, product and output", got[1:])
	}
	for _, entry := range got {
		if strings.HasPrefix(string(entry.Key), "t1-") || strings.HasPrefix(string(entry.Key), "t2-") || strings.HasPrefix(string(entry.Key), "t3-") {
			t.Fatalf("entry %+v still stands in the timeline, want the older bands replaced by the summary", entry)
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
//   - the closed turn beside it is folded, so the tail keeps its whole detail
func TestAgeFoldKeepsTheSegmentInProgressWhole(t *testing.T) {
	ctx := context.Background()
	entries := ageTurn("t1", 0, 2)
	entries = append(entries,
		ProjectionEntry{Key: "t2-in", Type: event.TypeExternalInput, Summary: "input t2", Content: strings.Repeat("进行中的输入。", 20), Time: ageBase.Add(time.Hour)},
		ProjectionEntry{Key: "t2-plan", Type: event.TypeThinkingPlan, Summary: "plan t2", Content: strings.Repeat("进行中的过程。", 20), Time: ageBase.Add(time.Hour + time.Minute)},
		ProjectionEntry{Key: "t2-cmd", Type: event.TypeActionCommand, Summary: "call t2", Content: strings.Repeat("进行中的工具。", 20), Time: ageBase.Add(time.Hour + 2*time.Minute)},
	)
	projection := ageProjection(t, entries)

	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: DefaultAgeKeepRecent, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if report.NewFolded != 4 {
		t.Fatalf("new-band folded entries = %d, want only the four process entries of the closed turn", report.NewFolded)
	}
	for _, key := range []event.EventID{"t2-in", "t2-plan", "t2-cmd"} {
		found := false
		for _, entry := range projection.Entries() {
			if entry.Key == key {
				found = true
			}
		}
		if !found {
			t.Fatalf("projection lost %q, want the segment in progress kept whole", key)
		}
	}
}

// TestRefoldReadsTypeNotPrefix pins the judgment a later history fold reads a standing product by: its Type, never its text.
//   - a standing history summary whose content carries no type label is still judged a product, and the turns it recorded survive
//   - the summary a fold mints spells no type label of its own, so what kind of product it is stays its Type
func TestRefoldReadsTypeNotPrefix(t *testing.T) {
	ctx := context.Background()
	const early = "EARLY-INPUT: the first turn of the session"
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

	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if !report.Triggered || len(report.Products) == 0 {
		t.Fatalf("report = %+v, want the four turns to pass the volume threshold", report)
	}
	summary := report.Products[len(report.Products)-1]
	if summary.Type != ProjectionTypeHistorySummary {
		t.Fatalf("product = %+v, want the history summary of the older bands", summary)
	}
	if !strings.HasPrefix(summary.Content, "external_input: "+early) {
		t.Fatalf("history summary content = %q, want the standing product's content read verbatim ahead of the folded lines", summary.Content)
	}
	if strings.Contains(summary.Content, "history summary") || strings.Contains(summary.Content, ProjectionTypeHistorySummary+": ") {
		t.Fatalf("history summary content = %q, want no type label written into the material", summary.Content)
	}
	if !strings.Contains(summary.Content, "input t2") {
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

	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if !report.Triggered || report.MidFolded != 3*perTurn {
		t.Fatalf("report = %+v, want the three older turns (%d entries) folded into the history summary", report, 3*perTurn)
	}

	kept := projection.Entries()
	if len(kept) == 0 || kept[0].Type != ProjectionTypeHistorySummary {
		t.Fatalf("projection = %+v, want the history summary leading the model-visible sequence", kept)
	}
	summary := kept[0]
	lines := strings.Split(summary.Content, "; ")
	folded := chain[:3*perTurn]
	dump := func() string {
		var out strings.Builder
		for i, line := range lines {
			generation := ""
			if i < len(folded) {
				generation = string(folded[i].Envelope.InstanceID)
			}
			fmt.Fprintf(&out, "\n  line %d: %q (recorder generation %q)", i, line, generation)
		}
		return out.String()
	}
	prefix := foldAnchorLine
	if len(lines) != len(folded) {
		t.Fatalf("history summary has %d lines, want %d (one per folded entry); folded lines:%s",
			len(lines), len(folded), dump())
	}
	for i, line := range lines {
		wantGeneration := string(folded[i].Envelope.InstanceID)
		if wantGeneration == "" {
			wantGeneration = "?"
		}
		match := prefix.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("history summary line %d = %q, want a [MM-DD HH:MM|gen:%s] prefix (gen MUST equal the exact recorder generation %q of the folded fact; only gen:? may stand in for a missing generation); folded lines:%s",
				i, line, wantGeneration, wantGeneration, dump())
		}
		if want := folded[i].Envelope.Timestamp.Format("01-02 15:04"); match[1] != want {
			t.Fatalf("history summary line %d timestamp = %q, want %q from the folded entry; folded lines:%s",
				i, match[1], want, dump())
		}
		if match[2] != wantGeneration {
			t.Fatalf("history summary line %d generation = %q, want the exact recorder generation %q of the folded fact (only gen:? may stand in for a missing generation); folded lines:%s",
				i, match[2], wantGeneration, dump())
		}
		wantRead := folded[i].Envelope.Type + ": " + folded[i].Summary
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
	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if !report.Triggered || len(report.Products) == 0 {
		t.Fatalf("report = %+v, want the four turns past the threshold", report)
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
				t.Fatalf("%s line %d = %q, want the folded read %q after the placeholder anchor", name, i, line, want)
			}
		}
	}
	summary := report.Products[len(report.Products)-1]
	if summary.Type != ProjectionTypeHistorySummary {
		t.Fatalf("last product = %+v, want the history summary of the older turns", summary)
	}
	requireDegraded("history summary", summary.Content, entries[:12])
	var segment *ProjectionEntry
	for i := range report.Products {
		if report.Products[i].Type == ProjectionTypeSegmentFold {
			segment = &report.Products[i]
		}
	}
	if segment == nil {
		t.Fatalf("products = %+v, want the newest turn folded into a segment product", report.Products)
	}
	requireDegraded("segment fold", segment.Content, entries[13:15])
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
	for i := range entries {
		generation := before
		if strings.HasPrefix(string(entries[i].Key), "t2") {
			generation = after
		}
		entries[i] = WithInstanceID(entries[i], generation)
	}
	projection := ageProjection(t, entries)
	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: DefaultAgeKeepRecent, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if !report.Triggered || len(report.Products) != 2 {
		t.Fatalf("report = %+v, want one segment product per closed turn", report)
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
	if len(lines) != 4 {
		t.Fatalf("history summary has %d lines, want the two segment-product lines beside the two closing turn lines: %q", len(lines), summary.Content)
	}
	for i, line := range lines {
		match := foldAnchorLine.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("history summary line %d = %q, want a [MM-DD HH:MM|gen:…] prefix at the head", i, line)
		}
		if match[2] == "?" {
			t.Fatalf("history summary line %d = %q, want the recorded generation of the source line", i, line)
		}
	}
	if !strings.Contains(summary.Content, "|gen:"+string(after)+"] thinking_plan: plan t2a") {
		t.Fatalf("history summary content = %q, want the segment product's source line beside its own generation", summary.Content)
	}
	if !strings.Contains(summary.Content, "|gen:"+string(before)+"] external_input: input t3") {
		t.Fatalf("history summary content = %q, want the closing turn's line beside its own generation", summary.Content)
	}
}

// TestFoldProvenanceConsumerPath pins that provenance reaches the consumer the request is assembled for.
//
// - The history summary entry the budget assembly presents still opens with the anchored lines, and the segment products it presents do the same.
// - The presented lines name the full generations of the facts they came from, so two recorder generations stay distinguishable on the consumer's side.
func TestFoldProvenanceConsumerPath(t *testing.T) {
	ctx := context.Background()
	const (
		before = event.InstanceID("recorder-1a2b3c4d")
		after  = event.InstanceID("recorder-5e6f7a8b")
	)
	var entries []ProjectionEntry
	for step, seed := range []string{"t1", "t2", "t3", "t4"} {
		generation := before
		if seed == "t4" {
			generation = after
		}
		for _, entry := range ageTurn(seed, step, 1) {
			entries = append(entries, WithInstanceID(entry, generation))
		}
	}
	projection := ageProjection(t, entries)
	if _, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600}); err != nil {
		t.Fatalf("FoldByAge: %v", err)
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
