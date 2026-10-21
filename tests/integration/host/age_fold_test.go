package host

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// ageFoldInput is the user text one committed turn carries: long enough that a
// handful of turns fills a small window, and distinct per turn so the products
// the fold mints name the material they folded.
func ageFoldInput(turn int) string {
	return fmt.Sprintf("age fold turn %02d: %s", turn, strings.Repeat("material ", 4))
}

// ageFoldWindow reports the window a test declaration of maxTokens names under
// the declaration defaults, so the fold the plugin runs and the fold a test
// re-runs are judged under the same threshold.
func ageFoldWindow(maxTokens int) tagent.FoldWindow {
	return tagent.FoldWindow{
		MaxTokens:         maxTokens,
		CompressThreshold: config.DefaultCompressThreshold,
		KeepRecentTasks:   config.DefaultKeepRecentTasks,
	}
}

// ageFoldProducts reports the synthetic product entries one session's projection
// carries, sorted by key, beside how many entries it carries at all.
func ageFoldProducts(t *testing.T, run *tagent.Run, session event.SessionID) ([]event.EventID, int) {
	t.Helper()
	entries, held := run.ReplayEntries(session)
	if !held {
		t.Fatalf("session %q registered no projection", session)
	}
	keys := make([]event.EventID, 0, len(entries))
	for _, entry := range entries {
		if replay.SyntheticKey(entry.Key) {
			keys = append(keys, entry.Key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys, len(entries)
}

// ageFoldFacts reports how many facts of one frozen type the session chain holds.
func ageFoldFacts(run *tagent.Run, session event.SessionID, factType string) int {
	count := 0
	for _, evt := range run.SessionFacts(event.Address{SessionID: session}) {
		if evt.Type == factType {
			count++
		}
	}
	return count
}

// TestAgeFoldFiresFromCommits is the positive anchor of the commit-driven fold.
//   - the fold runs off the commits of a session whose agent declares a model window
//   - the projection the request boundary presents carries the fold products
//   - every product the fold minted is filed on the chain as a compress request
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAgeFoldFiresFromCommits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the folded answer")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:        "agent-a",
			Model:     "model-a",
			MaxTokens: 100,
		}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-age-fold"}
	var products []event.EventID
	for turn := 0; turn < 4 && len(products) == 0; turn++ {
		if _, _, err := run.InjectEvent(ctx, event.Event{
			Type:    event.TypeExternalInput,
			Owner:   addr,
			Message: model.Message{Role: model.RoleUser, Content: ageFoldInput(turn)},
		}, event.InvocationID(fmt.Sprintf("inv-age-%d", turn))); err != nil {
			t.Fatalf("inject turn %d: %v", turn, err)
		}
		products, _ = ageFoldProducts(t, run, addr.SessionID)
	}
	if len(products) == 0 {
		t.Fatalf("the committed turns minted no fold product in the projection")
	}
	if filed := ageFoldFacts(run, addr.SessionID, event.CompressRequestType); filed == 0 {
		t.Fatalf("the fold filed no compress request on the chain beside %d product(s)", len(products))
	}
}

// TestUnderThresholdUntouched is the negative anchor of the commit-driven fold.
//   - a projection inside the window's threshold keeps every entry it committed
//   - no synthetic product enters the projection
//   - no compress request is filed on the chain
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestUnderThresholdUntouched(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the kept answer")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:        "agent-a",
			Model:     "model-a",
			MaxTokens: 1 << 16,
		}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-under-threshold"}
	for turn := 0; turn < 3; turn++ {
		key := event.EventID(fmt.Sprintf("under-%02d", turn))
		if _, err := run.CommitToSession(ctx, addr, event.Event{
			Key:     key,
			Type:    event.TypeExternalInput,
			Message: model.Message{Role: model.RoleUser, Content: ageFoldInput(turn)},
		}); err != nil {
			t.Fatalf("commit %s: %v", key, err)
		}
	}
	products, entries := ageFoldProducts(t, run, addr.SessionID)
	if entries != 3 {
		t.Fatalf("projection carries %d entries, want the three committed facts alone", entries)
	}
	if len(products) != 0 {
		t.Fatalf("projection carries %d synthetic product(s), want none inside the threshold: %v", len(products), products)
	}
	if filed := ageFoldFacts(run, addr.SessionID, event.CompressRequestType); filed != 0 {
		t.Fatalf("the chain carries %d compress request(s), want none inside the threshold", filed)
	}
}

// TestRebuildRefoldsIdempotent anchors the fold's content-addressed keys.
//   - the fold a commit triggers mints its products under content-addressed keys
//   - a fresh projection rebuilt from the same chain folds to the same keys
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestRebuildRefoldsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const maxTokens = 100
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the idempotent answer")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:        "agent-a",
			Model:     "model-a",
			MaxTokens: maxTokens,
		}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-refold"}
	base := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	var first []event.EventID
	for turn := 0; turn < 6 && len(first) == 0; turn++ {
		legs := []struct {
			suffix  string
			typ     string
			summary string
		}{
			{"in", event.TypeExternalInput, fmt.Sprintf("ask %02d", turn)},
			{"out", event.TypeAgentOutput, fmt.Sprintf("answer %02d", turn)},
		}
		for position, leg := range legs {
			key := event.EventID(fmt.Sprintf("refold-%02d-%s", turn, leg.suffix))
			if _, err := run.CommitToSession(ctx, addr, event.Event{
				Key:       key,
				Type:      leg.typ,
				Timestamp: base.Add(time.Duration(turn*10+position) * time.Minute),
				Meta:      map[string]string{event.MetaKeyEventSummary: leg.summary},
				Message:   model.Message{Role: model.RoleUser, Content: ageFoldInput(turn)},
			}); err != nil {
				t.Fatalf("commit %s: %v", key, err)
			}
			first, _ = ageFoldProducts(t, run, addr.SessionID)
			if len(first) > 0 {
				break
			}
		}
	}
	if len(first) == 0 {
		t.Fatalf("the committed turns minted no fold product in the projection")
	}
	chainKey := replay.ChainKey{PartitionID: 1, SessionID: addr.SessionID}
	chain := replay.NewMemoryEventChain()
	for _, evt := range run.SessionFacts(event.Address{SessionID: addr.SessionID}) {
		if evt.Type == event.CompressRequestType || evt.Message.Content == "" {
			continue
		}
		if err := chain.Commit(ctx, replay.ChainFact{
			Key:     evt.Key,
			Chain:   chainKey,
			Type:    evt.Type,
			Summary: evt.Meta[event.MetaKeyEventSummary],
			Content: evt.Message.Content,
			Time:    evt.Timestamp,
		}); err != nil {
			t.Fatalf("commit the chain fact %s: %v", evt.Key, err)
		}
	}
	rebuilt := replay.NewReplayProjection()
	if _, err := replay.RebuildProjectionFromWAL(ctx, rebuilt, chain, replay.NewMemoryRelations(), chainKey, replay.RebuildOptions{}); err != nil {
		t.Fatalf("rebuild the projection from the chain: %v", err)
	}
	report, err := rebuilt.FoldByAge(ctx, replay.AgeFoldPlan{
		KeepRecent:      config.DefaultKeepRecentTasks,
		VolumeThreshold: ageFoldWindow(maxTokens).VolumeThresholdBytes(),
	})
	if err != nil {
		t.Fatalf("fold the rebuilt projection: %v", err)
	}
	if !report.Triggered {
		t.Fatalf("the rebuilt projection stayed inside the threshold, want the same fold the commits triggered")
	}
	rebuiltKeys := make([]event.EventID, 0, len(report.Products))
	for _, product := range report.Products {
		rebuiltKeys = append(rebuiltKeys, product.Key)
	}
	sort.Slice(rebuiltKeys, func(i, j int) bool { return rebuiltKeys[i] < rebuiltKeys[j] })
	if len(first) != len(rebuiltKeys) {
		t.Fatalf("rebuilt fold minted %v, want the keys the commits minted %v", rebuiltKeys, first)
	}
	for i := range first {
		if first[i] != rebuiltKeys[i] {
			t.Fatalf("rebuilt fold minted %v, want the keys the commits minted %v", rebuiltKeys, first)
		}
	}
}
