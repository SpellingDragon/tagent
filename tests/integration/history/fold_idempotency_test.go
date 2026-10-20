package history

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// TestFoldSurvivesCrashAndRebuildsByIdentity pins the fold's crash contract: one material, one content-addressed product.
//   - 折叠产物未落盘前崩溃: a fold that never landed refolds from the original facts under the same identity
//   - 折叠产物落盘后崩溃: a rebuilt snapshot answers the payload it restored and mints no second fold
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestFoldSurvivesCrashAndRebuildsByIdentity(t *testing.T) {
	ctx := context.Background()
	session := replay.ChainKey{PartitionID: 3, SessionID: event.SessionID("fold-crash")}
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	factOf := func(key, typ, summary, content string, step int) replay.ChainFact {
		return replay.ChainFact{
			Key:     event.EventID(key),
			Chain:   session,
			Type:    typ,
			Summary: summary,
			Content: content,
			Time:    base.Add(time.Duration(step) * time.Minute),
		}
	}
	facts := []replay.ChainFact{
		factOf("k-01", event.TypeExternalInput, "需求", "需求原文", 0),
		factOf("k-02", event.TypeAgentOutput, "实现", "实现正文", 1),
		factOf("k-03", event.TypeActionCommand, "验证", "go test ./...", 2),
		factOf("k-04", event.TypeAgentOutput, "收尾", "收尾记录", 3),
		factOf("k-05", event.TypeExternalInput, "追问", "追问原文", 4),
		factOf("k-06", event.TypeAgentOutput, "答复", "答复正文", 5),
	}
	project := func(run []replay.ChainFact) *replay.ReplayProjection {
		projection := replay.NewReplayProjection()
		for _, fact := range run {
			projection.Append(ctx, replay.ProjectionEntry{Key: fact.Key, Type: fact.Type, Summary: fact.Summary, Content: fact.Content, Time: fact.Time})
		}
		return projection
	}
	openChain := func(t *testing.T) (durableChain, string) {
		t.Helper()
		root := t.TempDir()
		tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
		if err != nil {
			t.Fatalf("OpenTier: %v", err)
		}
		return durableChain{log: ledger.TierFactLog(tier)}, root
	}
	commit := func(t *testing.T, chain durableChain, fact replay.ChainFact) {
		t.Helper()
		if err := chain.commit(ctx, fact); err != nil {
			t.Fatalf("commit %s: %v", fact.Key, err)
		}
	}
	foldFacts := func(t *testing.T, chain durableChain) []replay.ChainFact {
		t.Helper()
		all, err := chain.ScanFrom(ctx, "", 1<<16)
		if err != nil {
			t.Fatalf("scan the chain: %v", err)
		}
		var folds []replay.ChainFact
		for _, fact := range all {
			if fact.Type == replay.CompactionEventType {
				folds = append(folds, fact)
			}
		}
		return folds
	}

	t.Run("折叠产物未落盘前崩溃: a fold that never landed refolds under the same identity", func(t *testing.T) {
		chain, root := openChain(t)
		for _, fact := range facts[:4] {
			commit(t, chain, fact)
		}
		before := project(facts[:4])
		crashed, err := before.Fold(ctx)
		if err != nil {
			t.Fatalf("Fold: %v", err)
		}
		payload := *before.Compaction()
		folded := replay.RenderProjection(before.Entries())
		if len(foldFacts(t, chain)) != 0 {
			t.Fatal("the chain already carries a fold the crash was meant to lose")
		}

		relations, err := replay.OpenMemoryRelations(root)
		if err != nil {
			t.Fatalf("OpenMemoryRelations: %v", err)
		}
		rebuilt := replay.NewReplayProjection()
		report, err := replay.RebuildProjectionFromWAL(ctx, rebuilt, chain, relations, session, replay.RebuildOptions{PageSize: 2})
		if err != nil {
			t.Fatalf("RebuildProjectionFromWAL: %v", err)
		}
		if !report.Scanned || report.Partial || !report.Fallback || report.Entries != 4 {
			t.Fatalf("rebuild report = %+v, want a scanned fallback over the four source facts", report)
		}

		refolded, err := rebuilt.Fold(ctx)
		if err != nil {
			t.Fatalf("Fold of the rebuilt projection: %v", err)
		}
		if refolded.Key != crashed.Key || refolded.Key != payload.Summary.Key {
			t.Fatalf("refolded summary key = %q, want the identity %q the crashed fold minted", refolded.Key, crashed.Key)
		}
		if got := rebuilt.Compaction(); got == nil || !reflect.DeepEqual(*got, payload) {
			t.Fatalf("refolded payload = %+v, want the crashed fold payload %+v", got, payload)
		}
		if !bytes.Equal(replay.RenderProjection(rebuilt.Entries()), folded) {
			t.Fatal("the refold of the same facts rendered a different line")
		}

		identity := meditation.CompactionFactKey(payload)
		fact, err := meditation.CompactionChainFact(session, *rebuilt.Compaction(), base.Add(time.Hour))
		if err != nil {
			t.Fatalf("CompactionChainFact: %v", err)
		}
		if fact.Key != identity {
			t.Fatalf("refold fact key = %q, want the content-addressed identity %q", fact.Key, identity)
		}
		commit(t, chain, fact)
		commit(t, chain, fact)
		if folds := foldFacts(t, chain); len(folds) != 1 || folds[0].Key != identity {
			t.Fatalf("chain folds = %v, want one fold under %q", folds, identity)
		}
	})

	t.Run("折叠产物落盘后崩溃: a rebuilt snapshot answers the payload it restored", func(t *testing.T) {
		chain, root := openChain(t)
		for _, fact := range facts[:4] {
			commit(t, chain, fact)
		}
		live := project(facts[:4])
		if _, err := live.Fold(ctx); err != nil {
			t.Fatalf("Fold: %v", err)
		}
		payload := *live.Compaction()
		foldFact, err := meditation.CompactionChainFact(session, payload, base.Add(time.Hour))
		if err != nil {
			t.Fatalf("CompactionChainFact: %v", err)
		}
		commit(t, chain, foldFact)
		for _, fact := range facts[4:] {
			commit(t, chain, fact)
			live.Append(ctx, replay.ProjectionEntry{Key: fact.Key, Type: fact.Type, Summary: fact.Summary, Content: fact.Content, Time: fact.Time})
		}
		liveRender := replay.RenderProjection(live.Entries())

		relations, err := replay.OpenMemoryRelations(root)
		if err != nil {
			t.Fatalf("OpenMemoryRelations: %v", err)
		}
		rebuilt := replay.NewReplayProjection()
		report, err := replay.RebuildProjectionFromWAL(ctx, rebuilt, chain, relations, session, replay.RebuildOptions{PageSize: 2})
		if err != nil {
			t.Fatalf("RebuildProjectionFromWAL: %v", err)
		}
		if !report.Scanned || report.Partial || report.Fallback || report.FullBoundary != "k-04" {
			t.Fatalf("rebuild report = %+v, want the snapshot restored up to k-04", report)
		}
		if !bytes.Equal(replay.RenderProjection(rebuilt.Entries()), liveRender) {
			t.Fatal("the rebuilt projection rendered a different chain than the crashed one")
		}
		if got := rebuilt.Compaction(); got == nil || !reflect.DeepEqual(*got, payload) {
			t.Fatalf("restored payload = %+v, want the committed payload %+v", got, payload)
		}

		folded := replay.RenderProjection(rebuilt.Entries())
		again, err := rebuilt.Fold(ctx)
		if err != nil {
			t.Fatalf("Fold of the rebuilt projection: %v", err)
		}
		if again.Key != payload.Summary.Key {
			t.Fatalf("refolded summary key = %q, want the restored identity %q", again.Key, payload.Summary.Key)
		}
		if !bytes.Equal(replay.RenderProjection(rebuilt.Entries()), folded) {
			t.Fatal("a fold offered to the rebuilt projection moved it")
		}
		prefolded, err := meditation.Prefold(ctx, rebuilt)
		if err != nil {
			t.Fatalf("Prefold: %v", err)
		}
		if !reflect.DeepEqual(prefolded, payload) {
			t.Fatalf("Prefold = %+v, want the one payload the chain carries %+v", prefolded, payload)
		}
		if folds := foldFacts(t, chain); len(folds) != 1 || folds[0].Key != meditation.CompactionFactKey(payload) {
			t.Fatalf("chain folds = %v, want the single committed fold", folds)
		}
		if err := relations.VerifyReplay(ctx); err != nil {
			t.Fatalf("VerifyReplay over the rebuilt chain: %v", err)
		}
		reopened, err := replay.OpenMemoryRelations(root)
		if err != nil {
			t.Fatalf("reopen the relation ledger: %v", err)
		}
		if err := reopened.VerifyReplay(ctx); err != nil {
			t.Fatalf("VerifyReplay after reopening the journal: %v", err)
		}
	})
}
