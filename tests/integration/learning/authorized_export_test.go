package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/rl"
)

// TestAuthorizedTrainingExport drives the authorized training export end to end and checks the whole grant-to-manifest chain:
// - authorization runs before any read, and a page limit is refused instead of clamped
// - every written row is rechecked against the grant, and every refusal lands in the gap ledger by name
// - a repeated page and a read failure both end partial, never as the end of a partition
// - the reward of the feedback-bound sample cites the feedback by identity while the others stay unrated with no invented value
// - the reward is derived from the feedback account as a separate account, and a differing second feedback is a named conflict
// - a late feedback derives a new version instead of rewriting the recorded one or moving the first snapshot's citation
// - concurrent exports write beside each other, the seal refuses to overwrite, and every cited instance stays pinnable
// - a restart keeps instances and versions stable, and a restore of different content is a named conflict
// - a foreign record with no instance identity is refused instead of pinning a rule version that could stand for several records
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestAuthorizedTrainingExport(t *testing.T) {
	t.Run("authorization_precedes_reads", func(t *testing.T) {
		src := newFakeExportSource()
		exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())

		if _, err := exporter.Export(context.Background(), rl.ExportRequest{}); !errors.Is(err, rl.ErrExportUnauthorized) {
			t.Fatalf("empty grant: got %v, want ErrExportUnauthorized", err)
		}
		if _, err := exporter.Export(context.Background(), rl.ExportRequest{PartitionIDs: []string{"", ""}}); !errors.Is(err, rl.ErrExportUnauthorized) {
			t.Fatalf("blank grant: got %v, want ErrExportUnauthorized", err)
		}
		if src.calls() != 0 {
			t.Fatalf("authorization ran after reads: source saw %d calls", src.calls())
		}
	})

	t.Run("page_limit_refused_not_clamped", func(t *testing.T) {
		for _, limit := range []int{-1, rl.MaxExportPageLimit + 1} {
			src := newFakeExportSource()
			exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())
			if _, err := exporter.Export(context.Background(), rl.ExportRequest{
				PartitionIDs: []string{"p1"}, PageLimit: limit,
			}); !errors.Is(err, rl.ErrExportPageLimit) {
				t.Fatalf("limit %d: got %v, want ErrExportPageLimit", limit, err)
			}
			if src.calls() != 0 {
				t.Fatalf("limit %d was read with: %d source calls", limit, src.calls())
			}
		}
	})

	t.Run("per_sample_provenance_and_manifest_mirror", func(t *testing.T) {
		src := newFakeExportSource()
		src.add("p1",
			exportFactOf("e1", "p1", "call-1", "cap-1", "ep-b", "dec-1", "att-1", "e0", 1000),
			exportFactOf("e2", "p1", "call-1", "cap-1", "ep-c", "dec-2", "att-2", "", 1001),
		)
		src.add("p2", exportFactOf("e3", "p2", "call-2", "cap-2", "ep-x", "dec-3", "att-3", "e0", 1002))
		src.episodes = []rl.EpisodeRecord{
			{ID: "ep-a", RootID: "ep-a", Lineage: rl.LineageKnown},
			{ID: "ep-b", RootID: "ep-a", ParentID: "ep-a", Lineage: rl.LineageKnown},
			{ID: "ep-c", RootID: "ep-a", DependsOn: []rl.EpisodeDependency{
				{EpisodeID: "ep-c", DependsOn: "ep-b", Kind: rl.DependencyHandoff, Lineage: rl.LineageKnown},
			}, Lineage: rl.LineageKnown},
		}

		feedback := rl.NewFeedbackLedger()
		if err := feedback.Add(rl.FeedbackRecord{
			FeedbackID: "fb-1", EventKey: "e1", DecisionID: "dec-1",
			Conclusion: "good", Source: "human", Version: "v1", RecordedAt: time.UnixMilli(2000),
		}); err != nil {
			t.Fatalf("add feedback: %v", err)
		}
		rewards := rl.NewRewardLedger()
		exporter := rl.NewTrainingExporter(src, feedback, rewards, exportTestRule())

		result, err := exporter.Export(context.Background(), rl.ExportRequest{
			PartitionIDs: []string{"p1", "p2"}, CallIDs: []string{"call-1", "call-2"},
		})
		if err != nil || result.Partial {
			t.Fatalf("export: err=%v partial=%v", err, result.Partial)
		}
		if !result.Manifest.Complete || result.Written != 3 {
			t.Fatalf("complete=%v written=%d, want complete with 3 rows", result.Manifest.Complete, result.Written)
		}
		lines, held := exporter.Snapshot(result.Manifest.ExportID)
		if !held || len(lines) != 3 {
			t.Fatalf("snapshot holds %d lines (held=%v), want 3", len(lines), held)
		}
		if len(result.Manifest.Samples) != len(lines) {
			t.Fatalf("manifest lists %d samples for %d lines", len(result.Manifest.Samples), len(lines))
		}
		for i, line := range lines {
			prov := line.Provenance
			if prov.SampleID == "" || prov.ExportID == "" || prov.CaptureID == "" || prov.EpisodeID == "" ||
				prov.DecisionID == "" || prov.AttemptID == "" || prov.Schema != rl.SchemaCurrent ||
				prov.RewardInstanceID == "" || prov.RewardRevision < 1 || prov.RewardVersion == "" || prov.GroupID == "" {
				t.Fatalf("line %d provenance is not complete: %+v", i, prov)
			}
			if prov.ExportID != result.Manifest.ExportID {
				t.Fatalf("line %d names export %q, manifest names %q", i, prov.ExportID, result.Manifest.ExportID)
			}
			if result.Manifest.Samples[i] != prov {
				t.Fatalf("manifest sample %d does not mirror the line: %+v vs %+v", i, result.Manifest.Samples[i], prov)
			}
		}
		if got, want := result.Manifest.SourceSHA256, linesDigestOf(t, lines); got != want {
			t.Fatalf("source digest %q, want %q (the digest must cover the bytes written)", got, want)
		}
		// Causal grouping: ep-b pulled into ep-a's group by its parent and
		// ep-c pulled in by its dependency, while ep-x stands alone at unknown
		// lineage. The groups never overlap.
		var groupBC, groupX string
		for _, line := range lines {
			switch line.Provenance.EpisodeID {
			case "ep-b":
				groupBC = line.Provenance.GroupID
			case "ep-c":
				if groupBC != "" && line.Provenance.GroupID != groupBC {
					t.Fatalf("ep-c group %q differs from ep-b group %q", line.Provenance.GroupID, groupBC)
				}
				groupBC = line.Provenance.GroupID
			case "ep-x":
				groupX = line.Provenance.GroupID
			}
		}
		if groupBC == "" || groupX == "" || groupBC == groupX {
			t.Fatalf("grouping is not leakage free: bc=%q x=%q", groupBC, groupX)
		}
		for _, group := range result.Manifest.Groups {
			if group.GroupID == groupBC {
				if group.RootID != "ep-a" {
					t.Fatalf("group root %q, want ep-a", group.RootID)
				}
				if group.Lineage != rl.LineageKnown {
					t.Fatalf("group lineage %v, want known", group.Lineage)
				}
			}
			if group.GroupID == groupX && group.Lineage != rl.LineageUnknown {
				t.Fatalf("recordless episode group lineage %v, want unknown", group.Lineage)
			}
		}
		if result.Manifest.ParentKeyMissing != 1 {
			t.Fatalf("parent-missing count %d, want 1 (an absent key is reported, never invented)", result.Manifest.ParentKeyMissing)
		}
		for _, line := range lines {
			if line.Provenance.DecisionID == "dec-1" {
				if line.Reward.State != rl.RewardRated || line.Reward.Value != 1 {
					t.Fatalf("feedback-bound reward: %+v", line.Reward)
				}
				if len(line.Reward.DerivedFrom) != 1 || line.Reward.DerivedFrom[0].FeedbackID != "fb-1" {
					t.Fatalf("reward does not cite its evidence: %+v", line.Reward.DerivedFrom)
				}
			} else if line.Reward.State != rl.RewardUnrated || line.Reward.Value != 0 || len(line.Reward.DerivedFrom) != 0 {
				t.Fatalf("unrated sample carries an invented reward: %+v", line.Reward)
			}
		}
	})

	t.Run("filters_counted_not_dropped_in_silence", func(t *testing.T) {
		src := newFakeExportSource()
		src.add("p1",
			exportFactOf("e1", "p1", "keep", "cap", "ep-a", "dec-1", "att-1", "", 1000),
			exportFactOf("e2", "p1", "drop", "cap", "ep-a", "dec-2", "att-2", "", 2000),
			exportFactOf("e3", "p1", "keep", "cap", "ep-a", "dec-3", "att-3", "", 9000),
		)
		exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())
		result, err := exporter.Export(context.Background(), rl.ExportRequest{
			PartitionIDs: []string{"p1"}, CallIDs: []string{"keep"}, Since: 500, Until: 5000,
		})
		if err != nil || result.Written != 1 || result.Filtered != 2 {
			t.Fatalf("written=%d filtered=%d err=%v, want 1 written and 2 filtered", result.Written, result.Filtered, err)
		}
		if result.Manifest.Filtered != 2 || !result.Manifest.Complete {
			t.Fatalf("manifest filtered=%d complete=%v", result.Manifest.Filtered, result.Manifest.Complete)
		}
	})

	t.Run("recheck_forbidden_row_written_not_at_all", func(t *testing.T) {
		src := newFakeExportSource()
		src.add("p1",
			exportFactOf("e1", "p1", "call", "cap", "ep-a", "dec-1", "att-1", "", 1000),
			exportFactOf("e2", "evil-partition", "call", "cap", "ep-a", "dec-2", "att-2", "", 1001),
		)
		exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())
		result, err := exporter.Export(context.Background(), rl.ExportRequest{PartitionIDs: []string{"p1"}})
		if err != nil || result.Partial {
			t.Fatalf("a refused row is accounted, not a failure: err=%v partial=%v", err, result.Partial)
		}
		if result.Written != 1 || result.Forbidden != 1 || result.Manifest.Forbidden != 1 {
			t.Fatalf("written=%d forbidden=%d, want 1 and 1", result.Written, result.Forbidden)
		}
		lines, _ := exporter.Snapshot(result.Manifest.ExportID)
		for _, line := range lines {
			if line.Provenance.SampleID == "sample:e2" {
				t.Fatal("a forbidden row was written")
			}
		}
	})

	t.Run("gap_ledger_names_every_refusal", func(t *testing.T) {
		legacy := exportFactOf("e1", "p1", "call", "cap", "ep-a", "dec-1", "att-1", "", 1000)
		legacy.Schema = rl.SchemaLegacyV2
		malformed := exportFactOf("e2", "p1", "call", "cap", "ep-a", "dec-2", "att-2", "", 1001)
		malformed.Body = json.RawMessage("{not json")
		missing := exportFactOf("e3", "p1", "call", "cap", "ep-a", "", "", "", 1002)
		src := newFakeExportSource()
		src.add("p1", legacy, malformed, missing)

		exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())
		result, err := exporter.Export(context.Background(), rl.ExportRequest{PartitionIDs: []string{"p1"}})
		if err != nil || result.Written != 0 {
			t.Fatalf("written=%d err=%v, want 0 and nil", result.Written, err)
		}
		kinds := map[string]rl.ProvenanceGapKind{}
		for _, gap := range result.Manifest.Rejected {
			kinds[gap.SampleID] = gap.Kind
			if gap.Reason == "" {
				t.Fatalf("gap %s carries no reason", gap.SampleID)
			}
		}
		if kinds["sample:e1"] != rl.ProvenanceGapUnknown || kinds["sample:e2"] != rl.ProvenanceGapMalformed || kinds["sample:e3"] != rl.ProvenanceGapMissing {
			t.Fatalf("gap kinds %v, want unknown/malformed/missing", kinds)
		}
	})

	t.Run("repeated_page_is_partial_never_end_of_partition", func(t *testing.T) {
		src := newFakeExportSource()
		src.add("p1",
			exportFactOf("e1", "p1", "call", "cap", "ep-a", "dec-1", "att-1", "", 1000),
			exportFactOf("e2", "p1", "call", "cap", "ep-a", "dec-2", "att-2", "", 1001),
		)
		src.stuck["p1"] = true
		exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())
		result, err := exporter.Export(context.Background(), rl.ExportRequest{PartitionIDs: []string{"p1"}, PageLimit: 1})
		if !errors.Is(err, rl.ErrExportRepeatedPage) {
			t.Fatalf("got %v, want ErrExportRepeatedPage", err)
		}
		if !result.Partial || result.Manifest.Complete {
			t.Fatalf("repeated page must be partial: partial=%v complete=%v", result.Partial, result.Manifest.Complete)
		}
		if result.Written != 1 {
			t.Fatalf("written before the stall: %d, want 1", result.Written)
		}
	})

	t.Run("read_failure_is_partial_with_what_was_read", func(t *testing.T) {
		src := newFakeExportSource()
		src.add("p1", exportFactOf("e1", "p1", "call", "cap", "ep-a", "dec-1", "att-1", "", 1000))
		src.add("p2", exportFactOf("e2", "p2", "call", "cap", "ep-a", "dec-2", "att-2", "", 1001))
		src.failAfter = 2
		exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())
		result, err := exporter.Export(context.Background(), rl.ExportRequest{PartitionIDs: []string{"p1", "p2"}})
		if !errors.Is(err, rl.ErrExportReadFailed) {
			t.Fatalf("got %v, want ErrExportReadFailed", err)
		}
		if !result.Partial || result.Manifest.Complete {
			t.Fatalf("read failure must be partial: partial=%v complete=%v", result.Partial, result.Manifest.Complete)
		}
	})

	t.Run("late_feedback_derives_new_version_without_rewrite", func(t *testing.T) {
		src := newFakeExportSource()
		src.add("p1", exportFactOf("e1", "p1", "call", "cap", "ep-a", "dec-1", "att-1", "", 1000))
		feedback := rl.NewFeedbackLedger()
		rewards := rl.NewRewardLedger()
		exporter := rl.NewTrainingExporter(src, feedback, rewards, exportTestRule())

		first, err := exporter.Export(context.Background(), rl.ExportRequest{PartitionIDs: []string{"p1"}})
		if err != nil {
			t.Fatalf("first export: %v", err)
		}
		firstLines, _ := exporter.Snapshot(first.Manifest.ExportID)
		prior := firstLines[0].Reward
		if prior.State != rl.RewardUnrated || prior.Revision != 1 || prior.InstanceID == "" {
			t.Fatalf("no-feedback sample must carry a pinnable unrated instance: %+v", prior)
		}

		if err := feedback.Add(rl.FeedbackRecord{
			FeedbackID: "fb-late", EventKey: "e1", DecisionID: "dec-1",
			Conclusion: "good", Source: "human", Version: "v1", RecordedAt: time.UnixMilli(3000),
		}); err != nil {
			t.Fatalf("late feedback: %v", err)
		}
		second, err := exporter.Export(context.Background(), rl.ExportRequest{PartitionIDs: []string{"p1"}})
		if err != nil {
			t.Fatalf("second export: %v", err)
		}
		secondLines, _ := exporter.Snapshot(second.Manifest.ExportID)
		later := secondLines[0].Reward
		if later.State != rl.RewardRated || later.Revision != 2 {
			t.Fatalf("late feedback must derive a new revision: %+v", later)
		}
		if later.InstanceID == prior.InstanceID {
			t.Fatalf("late feedback reused the instance %q", later.InstanceID)
		}
		if len(later.DerivedFrom) != 1 || later.DerivedFrom[0].FeedbackID != "fb-late" {
			t.Fatalf("later reward does not cite the late evidence: %+v", later.DerivedFrom)
		}
		history := rewards.Records("dec-1", "att-1")
		if len(history) != 2 || history[0].Revision != 1 || history[0].State != rl.RewardUnrated || history[0].Value != 0 {
			t.Fatalf("the recorded derivation was rewritten: %+v", history)
		}
		firstAgain, _ := exporter.Snapshot(first.Manifest.ExportID)
		if firstAgain[0].Reward.InstanceID != prior.InstanceID || firstAgain[0].Reward.Revision != 1 {
			t.Fatalf("the first snapshot's citation moved: %+v", firstAgain[0].Reward)
		}
	})

	t.Run("no_evidence_no_invention", func(t *testing.T) {
		rewards := rl.NewRewardLedger()
		record, err := rewards.Derive("dec-1", "att-1", exportTestRule(), nil)
		if !errors.Is(err, rl.ErrRewardNoFeedback) {
			t.Fatalf("got %v, want ErrRewardNoFeedback (a task terminal is never evidence)", err)
		}
		if record.State != rl.RewardUnrated || record.Value != 0 || len(record.DerivedFrom) != 0 {
			t.Fatalf("refused derivation must stand unrated: %+v", record)
		}
		if record.InstanceID == "" || record.Revision < 1 {
			t.Fatalf("the unrated instance cannot be pinned: %+v", record)
		}
		if _, err := rewards.Derive("dec-1", "att-1", rl.RewardRule{Version: ""}, nil); !errors.Is(err, rl.ErrRewardRuleMissing) {
			t.Fatalf("got %v, want ErrRewardRuleMissing", err)
		}
	})

	t.Run("feedback_and_reward_stay_two_accounts", func(t *testing.T) {
		feedback := rl.NewFeedbackLedger()
		if err := feedback.Add(rl.FeedbackRecord{
			FeedbackID: "fb-1", EventKey: "e1", DecisionID: "dec-1",
			Conclusion: "good", Source: "human", Version: "v1", RecordedAt: time.UnixMilli(1000),
		}); err != nil {
			t.Fatalf("add feedback: %v", err)
		}
		before := feedback.All()
		rewards := rl.NewRewardLedger()
		record, err := rewards.Derive("dec-1", "att-1", exportTestRule(), before)
		if err != nil {
			t.Fatalf("derive: %v", err)
		}
		after := feedback.All()
		if len(before) != len(after) || before[0] != after[0] {
			t.Fatalf("the derivation touched the feedback account: before=%+v after=%+v", before, after)
		}
		if record.DerivedFrom[0].FeedbackID != "fb-1" {
			t.Fatalf("the reward does not cite the feedback it was derived from: %+v", record.DerivedFrom)
		}
		if rewards.StateOf("dec-1", "att-1") != rl.RewardRated {
			t.Fatal("the reward account did not record the derivation")
		}
		if err := feedback.Add(rl.FeedbackRecord{
			FeedbackID: "fb-1", EventKey: "e1", DecisionID: "dec-1",
			Conclusion: "bad", Source: "human", Version: "v1", RecordedAt: time.UnixMilli(1000),
		}); !errors.Is(err, rl.ErrFeedbackConflict) {
			t.Fatalf("got %v, want ErrFeedbackConflict", err)
		}
	})

	t.Run("concurrent_exports_write_beside_never_over", func(t *testing.T) {
		src := newFakeExportSource()
		src.add("p1",
			exportFactOf("e1", "p1", "call", "cap", "ep-a", "dec-1", "att-1", "", 1000),
			exportFactOf("e2", "p1", "call", "cap", "ep-a", "dec-2", "att-2", "", 1001),
		)
		feedback := rl.NewFeedbackLedger()
		rewards := rl.NewRewardLedger()
		exporter := rl.NewTrainingExporter(src, feedback, rewards, exportTestRule())

		var wg sync.WaitGroup
		results := make([]rl.ExportResult, 2)
		errs := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i], errs[i] = exporter.Export(context.Background(), rl.ExportRequest{PartitionIDs: []string{"p1"}})
			}(i)
		}
		wg.Wait()
		for i := range errs {
			if errs[i] != nil || results[i].Partial || results[i].Written != 2 {
				t.Fatalf("run %d: err=%v partial=%v written=%d", i, errs[i], results[i].Partial, results[i].Written)
			}
		}
		if results[0].Manifest.ExportID == results[1].Manifest.ExportID {
			t.Fatalf("two concurrent runs share export id %q", results[0].Manifest.ExportID)
		}
		if ids := exporter.SnapshotIDs(); len(ids) != 2 {
			t.Fatalf("snapshot ids %v, want two sealed snapshots", ids)
		}
		for _, result := range results {
			lines, held := exporter.Snapshot(result.Manifest.ExportID)
			if !held || int64(len(lines)) != result.Written {
				t.Fatalf("snapshot %q lost rows", result.Manifest.ExportID)
			}
			for _, line := range lines {
				if line.Reward.InstanceID == "" || line.Reward.Revision < 1 {
					t.Fatalf("cited instance cannot be pinned: %+v", line.Reward)
				}
			}
		}
		sealed, _ := exporter.Snapshot(results[0].Manifest.ExportID)
		if err := exporter.SealSnapshot(results[0].Manifest.ExportID, sealed); !errors.Is(err, rl.ErrExportConflict) {
			t.Fatalf("overwrite attempt: got %v, want ErrExportConflict", err)
		}
		again, _ := exporter.Snapshot(results[0].Manifest.ExportID)
		if len(again) != len(sealed) || again[0].Provenance != sealed[0].Provenance {
			t.Fatal("the overwrite attempt moved the sealed snapshot")
		}
	})

	t.Run("concurrent_derivation_takes_independent_revisions", func(t *testing.T) {
		rewards := rl.NewRewardLedger()
		evidence := []rl.FeedbackRecord{{
			FeedbackID: "fb-1", EventKey: "e1", DecisionID: "dec-1",
			Conclusion: "good", Source: "human", Version: "v1", RecordedAt: time.UnixMilli(1000),
		}}
		var wg sync.WaitGroup
		records := make([]rl.RewardRecord, 2)
		errs := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				records[i], errs[i] = rewards.Derive("dec-1", "att-1", exportTestRule(), evidence)
			}(i)
		}
		wg.Wait()
		for i := range errs {
			if errs[i] != nil {
				t.Fatalf("derive %d: %v", i, errs[i])
			}
		}
		if records[0].InstanceID == records[1].InstanceID || records[0].Revision == records[1].Revision {
			t.Fatalf("two derivations collided: %+v vs %+v", records[0], records[1])
		}
		history := rewards.Records("dec-1", "att-1")
		if len(history) != 2 {
			t.Fatalf("history holds %d records, want 2 (nothing was overwritten)", len(history))
		}
	})

	t.Run("restart_keeps_instances_and_versions_stable", func(t *testing.T) {
		src := newFakeExportSource()
		src.add("p1", exportFactOf("e1", "p1", "call", "cap", "ep-a", "dec-1", "att-1", "", 1000))
		feedback := rl.NewFeedbackLedger()
		if err := feedback.Add(rl.FeedbackRecord{
			FeedbackID: "fb-1", EventKey: "e1", DecisionID: "dec-1",
			Conclusion: "good", Source: "human", Version: "v1", RecordedAt: time.UnixMilli(1000),
		}); err != nil {
			t.Fatalf("add feedback: %v", err)
		}
		rewards := rl.NewRewardLedger()
		exporter := rl.NewTrainingExporter(src, feedback, rewards, exportTestRule())
		before, err := exporter.Export(context.Background(), rl.ExportRequest{PartitionIDs: []string{"p1"}})
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		beforeLines, _ := exporter.Snapshot(before.Manifest.ExportID)
		cited := beforeLines[0].Reward

		reborn := rl.NewRewardLedger()
		for _, record := range rewards.Records("dec-1", "att-1") {
			if err := reborn.Restore(record); err != nil {
				t.Fatalf("restore %+v: %v", record, err)
			}
		}
		exporter2 := rl.NewTrainingExporter(src, feedback, reborn, exportTestRule())
		after, err := exporter2.Export(context.Background(), rl.ExportRequest{PartitionIDs: []string{"p1"}})
		if err != nil {
			t.Fatalf("post-restart export: %v", err)
		}
		afterLines, _ := exporter2.Snapshot(after.Manifest.ExportID)
		restored := afterLines[0].Reward
		if restored.InstanceID != cited.InstanceID || restored.Revision != cited.Revision {
			t.Fatalf("citation moved across restart: %+v vs %+v", restored, cited)
		}
		if restored.RuleVersion != cited.RuleVersion || restored.RuleVersion != "reward.rule.v1" {
			t.Fatalf("rule version drifted: %q", restored.RuleVersion)
		}
		tampered := cited
		tampered.Value += 1
		if err := reborn.Restore(tampered); !errors.Is(err, rl.ErrRewardConflict) {
			t.Fatalf("tampered restore: got %v, want ErrRewardConflict", err)
		}
	})

	t.Run("unpinnable_reward_refused_not_guessed", func(t *testing.T) {
		src := newFakeExportSource()
		src.add("p1", exportFactOf("e1", "p1", "call", "cap", "ep-a", "dec-1", "att-1", "", 1000))
		rewards := rl.NewRewardLedger()
		if err := rewards.Restore(rl.RewardRecord{
			DecisionID: "dec-1", AttemptID: "att-1", State: rl.RewardUnrated, RuleVersion: "reward.rule.v1",
		}); err != nil {
			t.Fatalf("restore foreign record: %v", err)
		}
		exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rewards, exportTestRule())
		result, err := exporter.Export(context.Background(), rl.ExportRequest{PartitionIDs: []string{"p1"}})
		if err != nil || result.Written != 0 {
			t.Fatalf("written=%d err=%v, want 0 and nil", result.Written, err)
		}
		if len(result.Manifest.Rejected) != 1 {
			t.Fatalf("rejected %v, want the unpinnable sample named", result.Manifest.Rejected)
		}
		gap := result.Manifest.Rejected[0]
		if gap.Kind != rl.ProvenanceGapUnknown || !strings.Contains(gap.Reason, "pinned") {
			t.Fatalf("gap %+v, want unknown with a pinning reason", gap)
		}
	})
}

// fakeExportSource is the read seam the export pages through: a fixed partition
// map in stable order, a page failure switch and a stuck switch that makes one
// partition hand back its first page forever, which is what a repeated page
// looks like from the outside.
type fakeExportSource struct {
	mu          sync.Mutex
	partitions  map[string][]rl.ExportedFact
	episodes    []rl.EpisodeRecord
	episodesErr error
	failAfter   int
	stuck       map[string]bool
	readCalls   int
}

func newFakeExportSource() *fakeExportSource {
	return &fakeExportSource{
		partitions: map[string][]rl.ExportedFact{},
		stuck:      map[string]bool{},
		failAfter:  -1,
	}
}

func (f *fakeExportSource) add(partition string, facts ...rl.ExportedFact) {
	f.partitions[partition] = append(f.partitions[partition], facts...)
}

func (f *fakeExportSource) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readCalls
}

func (f *fakeExportSource) Page(ctx context.Context, partition string, offset, limit int) ([]rl.ExportedFact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readCalls++
	if f.failAfter >= 0 && f.readCalls > f.failAfter {
		return nil, errors.New("fake: source read failed")
	}
	facts := f.partitions[partition]
	if f.stuck[partition] && offset > 0 {
		offset = 0
	}
	if offset >= len(facts) {
		return nil, nil
	}
	end := offset + limit
	if end > len(facts) {
		end = len(facts)
	}
	return append([]rl.ExportedFact(nil), facts[offset:end]...), nil
}

func (f *fakeExportSource) Episodes(ctx context.Context) ([]rl.EpisodeRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readCalls++
	if f.episodesErr != nil {
		return nil, f.episodesErr
	}
	return append([]rl.EpisodeRecord(nil), f.episodes...), nil
}

// exportFactOf builds one stored event for the export tests.
func exportFactOf(key, partition, call, capture string, episode event.EpisodeID, decision event.DecisionID, attempt event.AttemptID, parent event.EventID, ts int64) rl.ExportedFact {
	return rl.ExportedFact{
		EventKey:   event.EventID(key),
		Partition:  partition,
		CallID:     call,
		CaptureID:  capture,
		EpisodeID:  episode,
		DecisionID: decision,
		AttemptID:  attempt,
		Schema:     rl.SchemaCurrent,
		ParentKey:  parent,
		Timestamp:  ts,
		Body:       json.RawMessage(`{"kind":"tool_call"}`),
	}
}

// exportTestRule is the derivation rule the tests derive under: one point per
// positive conclusion, one per negative, under one pinned rule version.
func exportTestRule() rl.RewardRule {
	return rl.RewardRule{
		Version: "reward.rule.v1",
		Value: func(evidence []rl.FeedbackRecord) (float64, error) {
			total := 0.0
			for _, record := range evidence {
				if record.Conclusion == "good" {
					total += 1
				} else {
					total -= 1
				}
			}
			return total, nil
		},
	}
}

// linesDigestOf recomputes the digest over the bytes the snapshot lines
// marshal to, in write order.
func linesDigestOf(t *testing.T, lines []rl.SampleLine) string {
	t.Helper()
	digest := sha256.New()
	for _, line := range lines {
		encoded, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("marshal line: %v", err)
		}
		digest.Write(encoded)
	}
	return hex.EncodeToString(digest.Sum(nil))
}
