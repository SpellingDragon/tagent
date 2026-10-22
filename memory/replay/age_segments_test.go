package replay

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// ageBase is the pass time the segment-age invariants lay their turns out from.
var ageBase = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

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

// TestSegmentsCloseAtAgentOutput pins the partition of a projection into task-turn segments.
//   - a segment spans an input beside the process detail up to the output that closes it
//   - a tail no output closed stays its own segment, in progress and never over the recent window
//   - the ages run from the newest segment backwards
func TestSegmentsCloseAtAgentOutput(t *testing.T) {
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
		if !strings.HasPrefix(product.Content, "folded segment process: thinking_plan: ") {
			t.Fatalf("product %d content = %q, want the one-line reads of the folded detail", i, product.Content)
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
		if entry.Type == ProjectionTypeSegmentFold && strings.Contains(entry.Content, "folded segment process") {
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
