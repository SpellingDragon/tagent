package assembly

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// TestRelationsFollowCommitsAndRestart pins the owner-log relation channel.
// - two agents sharing one session id still form two isolated parent chains
// - journal reopen and a full boot over an empty graph restore the same links
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestRelationsFollowCommitsAndRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	relations, err := replay.OpenMemoryRelations(filepath.Join(root, "relations"))
	if err != nil {
		t.Fatalf("open the relation journal: %v", err)
	}
	decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: 100000}, {ID: "agent-b", Model: "model-a", MaxTokens: 100000}}}
	build := func() map[event.AgentID]NativeAgent {
		provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
			return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
		}}
		return map[event.AgentID]NativeAgent{"agent-a": {Model: provider}, "agent-b": {Model: provider}}
	}
	first, err := Assemble(ctx, decl, build(), WithGovernance(snapshotTestGovernance{}), storage, withRelations(relations))
	if err != nil {
		t.Fatalf("assemble the first run: %v", err)
	}
	addrA := event.Address{AgentID: "agent-a", SessionID: "session-shared-relations"}
	addrB := event.Address{AgentID: "agent-b", SessionID: "session-shared-relations"}
	commitWindowTurns(t, ctx, first, addrA, 1)
	commitWindowTurns(t, ctx, first, addrB, 1)
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	a0 := event.EventID(timeStampKey(base, addrA, 0, 0))
	a1 := event.EventID(timeStampKey(base, addrA, 0, 1))
	b0 := event.EventID(timeStampKey(base, addrB, 0, 0))
	b1 := event.EventID(timeStampKey(base, addrB, 0, 1))
	assertParent := func(label string, graph *replay.MemoryRelations, child, parent event.EventID) {
		t.Helper()
		got, err := graph.GetParent(child)
		if err != nil {
			t.Fatalf("%s: read the parent of %s: %v", label, child, err)
		}
		if got != parent {
			t.Fatalf("%s: parent(%s)=%q, want %q", label, child, got, parent)
		}
	}
	assertParent("live", relations, a1, a0)
	assertParent("live", relations, b0, "")
	assertParent("live", relations, b1, b0)
	if err := relations.VerifyReplay(ctx); err != nil {
		t.Fatalf("the live graph differs from its journal: %v", err)
	}
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("drain the first run: %v", err)
	}
	reopened, err := replay.OpenMemoryRelations(filepath.Join(root, "relations"))
	if err != nil {
		t.Fatalf("reopen the relation journal: %v", err)
	}
	assertParent("journal reopen", reopened, a1, a0)
	assertParent("journal reopen", reopened, b0, "")
	assertParent("journal reopen", reopened, b1, b0)
	fresh := replay.NewMemoryRelations()
	second, err := Assemble(ctx, decl, build(), WithGovernance(snapshotTestGovernance{}), storage, withRelations(fresh))
	if err != nil {
		t.Fatalf("assemble the restarted run: %v", err)
	}
	assertParent("full boot", fresh, a1, a0)
	assertParent("full boot", fresh, b0, "")
	assertParent("full boot", fresh, b1, b0)
	if _, err := second.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("drain the restarted run: %v", err)
	}
	journal, err := ledger.OpenRelationsJournal(filepath.Join(root, "relations"))
	if err != nil {
		t.Fatalf("open the journal record reader: %v", err)
	}
	before, err := journal.Records(ctx)
	if err != nil {
		t.Fatalf("read the journal before the idempotent boot: %v", err)
	}
	third, err := Assemble(ctx, decl, build(), WithGovernance(snapshotTestGovernance{}), storage, withRelations(reopened))
	if err != nil {
		t.Fatalf("assemble over the journaled graph: %v", err)
	}
	after, err := journal.Records(ctx)
	if err != nil {
		t.Fatalf("read the journal after the idempotent boot: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("the idempotent boot appended %d duplicate relation record(s)", len(after)-len(before))
	}
	if err := reopened.VerifyReplay(ctx); err != nil {
		t.Fatalf("the journaled graph differs after boot: %v", err)
	}
	if _, err := third.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("drain the journaled restart: %v", err)
	}
}
