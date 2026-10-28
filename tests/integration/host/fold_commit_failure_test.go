// The commit-refusal anchors of the host: one fact has one commit entry, and a
// commit that entry refuses is answered as a refusal — never as a commitment.
// The failure of a commit is the caller's to observe, so no path may report
// success over a fact the storage tier never took: the fact a refused commit
// names is absent from the live ledger and from the durable record, and the
// named cause of the refusal reaches the caller that asked for it.
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestFoldCommitFailureSurfaces requires a refused commit to surface instead of being answered as a commitment.
//   - positive: a commit the storage path refuses answers its named cause and lands in no ledger at all
//   - negative: a second content under one identity is refused by name, and the first fact stands unchanged
//   - consumer: both refusals run through the public commit entries of a formally assembled run over a real localfile tier
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestFoldCommitFailureSurfaces(t *testing.T) {
	reproductionEnv(t)
	t.Run("refused_commit_lands_in_no_ledger", runRefusedCommitLandsInNoLedger)
	t.Run("conflicting_content_is_refused_by_name", runConflictingContentIsRefusedByName)
}

// commitRefusalRun assembles one run over a freshly opened localfile tier, so
// every commit below passes the storage path a served process commits through
// and no refusal is a memory-only shortcut.
func commitRefusalRun(t *testing.T) *assembly.Run {
	t.Helper()
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: 32000}},
	}
	native := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the commit-refusal run drives no decision of its own")
	}}
	run, err := assembly.Assemble(context.Background(), decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: native},
	}, assembly.WithGovernance(passPolicy{}),
		assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	if err != nil {
		t.Fatalf("assemble the commit-refusal run: %v", err)
	}
	return run
}

// runRefusedCommitLandsInNoLedger commits one fact the storage path refuses and
// requires the refusal to reach the caller beside no fact at all: the cause is
// named, the live ledger holds nothing under the identity and the durable record
// answers nothing either, so no reader is told the fact committed.
func runRefusedCommitLandsInNoLedger(t *testing.T) {
	run := commitRefusalRun(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-refused-commit"}
	key := event.EventID("refused-commit-1")
	_, err := run.CommitToSession(ctx, addr, event.Event{
		Key:       key,
		Type:      event.TypeExternalInput,
		Timestamp: time.Date(1960, time.January, 1, 0, 0, 0, 0, time.UTC),
		Message:   model.Message{Role: model.RoleUser, Content: "the material whose commit is refused"},
	})
	if err == nil {
		t.Fatalf("the refused commit answered no error, want the named cause of the refusal")
	}
	if !strings.Contains(err.Error(), "carries no event time") {
		t.Fatalf("the refused commit answered %v, want the cause the storage path refused it under", err)
	}
	if _, readErr := run.CommittedEvent(ctx, key); !errors.Is(readErr, assembly.ErrCommittedFactMissing) {
		t.Fatalf("the refused fact reads back as %v, want %v: a refused commit may never be answered as a commitment", readErr, assembly.ErrCommittedFactMissing)
	}
	if held := run.SessionFacts(addr); len(held) != 0 {
		t.Fatalf("the session holds %d fact(s) beside a refused commit: %+v", len(held), held)
	}
	if count := run.CommittedFacts(); count != 0 {
		t.Fatalf("the live ledger holds %d fact(s) beside a refused commit, want none", count)
	}
	t.Logf("[commit-refusal 1] positive: the refused commit answered %q and landed beside no fact (live=0)", err.Error())
}

// runConflictingContentIsRefusedByName requires the second content under one
// identity to be refused by name before anything moves: one identity names one
// immutable fact, so the first fact reads back unchanged and the ledger still
// holds exactly that one fact.
func runConflictingContentIsRefusedByName(t *testing.T) {
	run := commitRefusalRun(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-conflicting-commit"}
	key := event.EventID("conflicting-commit-1")
	commit := func(content string) error {
		_, err := run.CommitToSession(ctx, addr, event.Event{
			Key:       key,
			Type:      event.TypeExternalInput,
			Timestamp: time.Now(),
			Message:   model.Message{Role: model.RoleUser, Content: content},
		})
		return err
	}
	if err := commit("the first content one identity names"); err != nil {
		t.Fatalf("the first commit under the identity was refused: %v", err)
	}
	if err := commit("another content under the same identity"); !errors.Is(err, memory.ErrFactKeyConflict) {
		t.Fatalf("the second content under one identity answered %v, want %v", err, memory.ErrFactKeyConflict)
	}
	held, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("the identity the conflict was refused under reads back: %v", err)
	}
	if held.Message.Content != "the first content one identity names" {
		t.Fatalf("the fact under one identity reads back as %q, want the first content: the refused second content moved it", held.Message.Content)
	}
	if count := run.CommittedFacts(); count != 1 {
		t.Fatalf("the live ledger holds %d fact(s) under one identity, want exactly one", count)
	}
	t.Logf("[commit-refusal 2] negative: the second content under one identity was refused by name and the first fact stands unchanged")
}
