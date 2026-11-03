// The file fact anchors of the host: a file content change is observed at the
// one request-boundary read, filed as the configuration change fact of the file
// dimension, and projected from that fact into the next request, while the
// request already under way keeps the content it fixed.
package host

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/agent"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
)

// fileFactAddress is the one agent session the file fact anchors drive.
var fileFactAddress = event.Address{AgentID: "agent-a", SessionID: "session-file-fact"}

// TestFileChangeLandsAFileFactOnTheChain checks the file change boundary on the formal assembly entry.
//   - the request under way keeps the content it fixed at its own boundary
//   - the rewritten file files one config_change fact of the file dimension on the chain, and its body carries the rewritten content
//   - the next request carries the rewritten content, so the file change takes effect at the boundary with no reload in between
//   - the file change files no structural publication: the generation stays
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestFileChangeLandsAFileFactOnTheChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	const (
		firstBody = "FILE-FACT-BODY-V1"
		nextBody  = "FILE-FACT-BODY-V2"
	)
	root := t.TempDir()
	promptPath := filepath.Join(root, "prompts", "system.md")
	writePromptAt(t, promptPath, firstBody+"\n")

	firstServing := make(chan struct{})
	releaseFirst := make(chan struct{})
	var served int32
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		if atomic.AddInt32(&served, 1) == 1 {
			close(firstServing)
			<-releaseFirst
		}
		return entryFinalTurn("settled")
	}}
	run := fileFactRun(t, ctx, provider, promptPath)
	defer fileFactClose(t, run)
	generation := fileFactGeneration(t, ctx, run)

	firstDone := make(chan error, 1)
	go func() {
		_, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-file-fact-1",
			Address:      fileFactAddress,
			Input:        []model.Message{{Role: model.RoleUser, Content: "the first file fact turn"}},
		})
		firstDone <- err
	}()
	select {
	case <-firstServing:
	case <-ctx.Done():
		close(releaseFirst)
		t.Fatalf("the first request never reached the model: %v", ctx.Err())
	case <-time.After(30 * time.Second):
		close(releaseFirst)
		t.Fatal("the first request never reached the model")
	}

	writePromptAt(t, promptPath, nextBody+"\n")
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("the first file fact turn failed: %v", err)
	}
	requests := provider.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want the one request of the first turn", len(requests))
	}
	if !carriesSystemBody(requests[0], firstBody) {
		t.Fatalf("first request = %+v, want the body fixed before the rewrite", requests[0].Messages)
	}
	if carriesSystemBody(requests[0], nextBody) {
		t.Fatalf("first request = %+v, want no content spliced into the request under way", requests[0].Messages)
	}

	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-file-fact-2",
		Address:      fileFactAddress,
		Input:        []model.Message{{Role: model.RoleUser, Content: "the second file fact turn"}},
	}); err != nil {
		t.Fatalf("the second file fact turn failed: %v", err)
	}
	requests = provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded requests = %d, want the second turn's request recorded too", len(requests))
	}
	if !carriesSystemBody(requests[1], nextBody) {
		t.Fatalf("second request = %+v, want the rewritten body carried into the next request", requests[1].Messages)
	}
	if carriesSystemBody(requests[1], firstBody) {
		t.Fatalf("second request = %+v, want no stale body from the earlier read", requests[1].Messages)
	}

	facts := fileFactsOf(t, ctx, run)
	if len(facts) != 2 {
		t.Fatalf("file change facts = %d, want one per content version the boundary fixed: %+v", len(facts), facts)
	}
	bodies := map[string]bool{}
	for _, fact := range facts {
		if fact.Type != event.TypeConfigChange {
			t.Fatalf("file fact type = %q, want the frozen configuration change type", fact.Type)
		}
		if got := fact.Meta[event.MetaKeyConfigChangeDimension]; got != "file" {
			t.Fatalf("file fact dimension = %q, want %q", got, "file")
		}
		if fact.Owner.AgentID != "agent-a" {
			t.Fatalf("file fact owner = %q, want the agent whose file face moved", fact.Owner.AgentID)
		}
		for _, body := range []string{firstBody, nextBody} {
			if strings.Contains(fact.Message.Content, body) {
				bodies[body] = true
			}
		}
	}
	for _, body := range []string{firstBody, nextBody} {
		if !bodies[body] {
			t.Fatalf("file change facts carry %v, want the body %q on the chain", bodies, body)
		}
	}
	for _, request := range requests {
		for _, message := range request.Messages {
			for _, fact := range facts {
				if strings.Contains(message.Content, string(fact.Key)) {
					t.Fatalf("request message = %q, want the file fact key %s to stay out of the model request", message.Content, fact.Key)
				}
			}
		}
	}

	if after := fileFactGeneration(t, ctx, run); after != generation {
		t.Fatalf("publication generation = %d after the file change, want the generation %d the boot published", after, generation)
	}
}

// TestFileFactFilingFailureDoesNotRefuseTheRequest checks the best-effort edge of the file fact boundary.
//   - a first run files the content-addressed file change fact of one content version on its chain
//   - a second run over a fresh storage plants another fact under that same identity
//   - the fact the boundary would file is already occupied by content it does not name
//   - the request is still served with the content read at its boundary: a filing that cannot land never stands in front of the business
//   - the refused filing is warned about, and the planted fact stays as it stood
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestFileFactFilingFailureDoesNotRefuseTheRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	const body = "FILE-FACT-CONFLICT-BODY"
	root := t.TempDir()
	promptPath := filepath.Join(root, "prompts", "system.md")
	writePromptAt(t, promptPath, body+"\n")

	var fileKey event.EventID
	t.Run("the boundary files the content-addressed fact", func(t *testing.T) {
		first := fileFactRun(t, ctx, &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
			return entryFinalTurn("settled")
		}}, promptPath)
		defer fileFactClose(t, first)
		if _, err := first.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-file-fact-identity",
			Address:      fileFactAddress,
			Input:        []model.Message{{Role: model.RoleUser, Content: "the identity turn"}},
		}); err != nil {
			t.Fatalf("the identity turn failed: %v", err)
		}
		facts := fileFactsOf(t, ctx, first)
		if len(facts) != 1 {
			t.Fatalf("file change facts = %d, want the one fact the boundary files for the content", len(facts))
		}
		fileKey = facts[0].Key
	})

	t.Run("a filing that cannot land does not refuse the request", func(t *testing.T) {
		if fileKey == "" {
			t.Fatal("the identity sub-case filed no file change fact to plant the conflict under")
		}
		provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
			return entryFinalTurn("settled")
		}}
		second := fileFactRun(t, ctx, provider, promptPath)
		defer fileFactClose(t, second)

		plantedBody := "FILE-FACT-PLANTED-OTHER-CONTENT"
		planted := event.Event{
			Key:       fileKey,
			Type:      event.TypeConfigChange,
			Timestamp: time.Now().UTC(),
			Meta: map[string]string{
				event.MetaKeyTriggerSource:         event.ConfigChangeSourceConfig,
				event.MetaKeyConfigChangeDimension: "file",
				event.MetaKeyEventSummary:          "the planted file fact",
			},
			Message: model.Message{Role: model.RoleSystem, Content: plantedBody},
			Owner:   event.Address{AgentID: "agent-a"},
		}
		if _, err := second.CommitToSession(ctx, event.Address{AgentID: "agent-a", SessionID: "session-file-fact-plant"}, planted); err != nil {
			t.Fatalf("plant the conflicting fact under %s: %v", fileKey, err)
		}

		logger := &observationLogger{}
		original := trpclog.Default
		trpclog.Default = logger
		defer func() { trpclog.Default = original }()

		turn, err := second.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-file-fact-conflict",
			Address:      fileFactAddress,
			Input:        []model.Message{{Role: model.RoleUser, Content: "the conflicting fact turn"}},
		})
		if err != nil {
			t.Fatalf("a file fact that could not be filed refused the request: %v", err)
		}
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %v, want the completed turn beside the refused filing", turn.Terminal)
		}
		requests := provider.recorded()
		if len(requests) == 0 {
			t.Fatal("the turn served no request, want the content read at the boundary")
		}
		if !carriesSystemBody(requests[0], body) {
			t.Fatalf("request = %+v, want the content read at the boundary beside the refused filing", requests[0].Messages)
		}
		if !logger.mentions("warn", "file change fact", "agent-a") {
			t.Fatalf("warn lines = %v, want the refused file change fact warned about", logger.at("warn"))
		}
		stored, err := second.CommittedEvent(ctx, fileKey)
		if err != nil {
			t.Fatalf("read the planted fact %s back: %v", fileKey, err)
		}
		if stored.Message.Content != plantedBody {
			t.Fatalf("planted fact body = %q, want the content it was filed with, immutable", stored.Message.Content)
		}
		if after := fileFactsOf(t, ctx, second); len(after) != 1 {
			t.Fatalf("file change facts = %d after the refused filing, want the one planted fact and no second identity for the content", len(after))
		}
	})
}

// fileFactRun assembles one runtime over the declared prompt file and a durable
// storage root of its own, so the file fact the boundary files is readable from
// the observation table of the same run.
func fileFactRun(t *testing.T, ctx context.Context, provider *entryModel, promptPath string) *assembly.Run {
	t.Helper()
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:    "agent-a",
			Model: "model-a",
			Files: []config.FileRef{{Path: promptPath, Kind: config.FileKindPrompt}},
		}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}),
		assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	if err != nil {
		t.Fatalf("assemble the file fact runtime: %v", err)
	}
	return run
}

// fileFactClose closes one file fact runtime beside its storage.
func fileFactClose(t *testing.T, run *assembly.Run) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := run.Close(ctx, agent.CloseRequest{Deadline: time.Now().Add(10 * time.Second)}); err != nil {
		t.Fatalf("close the file fact runtime: %v", err)
	}
}

// fileFactsOf reports the configuration change facts of the file dimension the
// run's chain holds, read through the observation table of its storage tier.
func fileFactsOf(t *testing.T, ctx context.Context, run *assembly.Run) []event.Event {
	t.Helper()
	refs, err := run.ObservedEvents(ctx, memory.QueryOptions{
		PartitionIDs: []int{memory.PartitionIDFromName("agent-a")},
		EventTypes:   []string{event.TypeConfigChange},
	})
	if err != nil {
		t.Fatalf("observe the configuration change facts: %v", err)
	}
	facts := make([]event.Event, 0, len(refs))
	for _, ref := range refs {
		fact, err := run.CommittedEvent(ctx, ref.Key)
		if err != nil {
			t.Fatalf("read the observed fact %s: %v", ref.Key, err)
		}
		if fact.Meta[event.MetaKeyConfigChangeDimension] != "file" {
			continue
		}
		facts = append(facts, fact)
	}
	return facts
}

// fileFactGeneration reports the structural publication generation in effect,
// read through the diagnostic face of the run.
func fileFactGeneration(t *testing.T, ctx context.Context, run *assembly.Run) uint64 {
	t.Helper()
	report, err := run.Diagnostics(ctx, query.ListFilter{}, assembly.DiagnosticSwitches{})
	if err != nil {
		t.Fatalf("read the publication generation: %v", err)
	}
	return report.Reflection.Publication.Generation
}
