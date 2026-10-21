package learning

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/rl"
)

// chainExportTurn is one turn the chain export check injects: the invocation,
// session and episode it is admitted under, and the input it presents.
type chainExportTurn struct {
	invocation event.InvocationID
	session    event.SessionID
	episode    event.EpisodeID
	input      string
}

// TestChainExporterProducesSamples pins the SFT rows exported from the chain the formal runtime entry committed.
//   - the export is non-empty and pairs each input with the assistant output of its own invocation
//   - episodes group the rows even when one episode spans two sessions, and turn numbers restart per episode
//   - a session the export does not name contributes no row
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestChainExporterProducesSamples(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := formalRun(t, ctx, &collector{}, func(req *model.Request) *model.Response {
		last := req.Messages[len(req.Messages)-1]
		return learnFinalTurn("answer to " + last.Content)
	})
	var _ rl.FactReader = run

	turns := []chainExportTurn{
		{invocation: "inv-chain-1", session: "session-chain-a", episode: "episode-chain-a", input: "first question"},
		{invocation: "inv-chain-2", session: "session-chain-a", episode: "episode-chain-a", input: "second question"},
		{invocation: "inv-chain-3", session: "session-chain-b", episode: "episode-chain-a", input: "third question"},
		{invocation: "inv-chain-4", session: "session-chain-b", episode: "episode-chain-b", input: "fourth question"},
	}
	for _, turn := range turns {
		evt := event.Event{
			Type:      event.TypeExternalInput,
			Owner:     event.Address{AgentID: "agent-a", SessionID: turn.session},
			EpisodeID: turn.episode,
			Message:   model.Message{Role: model.RoleUser, Content: turn.input},
		}
		if _, _, err := run.InjectEvent(ctx, evt, turn.invocation); err != nil {
			t.Fatalf("inject %s: %v", turn.invocation, err)
		}
	}

	exporter := rl.NewChainExporter(run)
	samples, err := exporter.Export(ctx, rl.ExportOptions{
		SessionIDs: []event.SessionID{"session-chain-a", "session-chain-b"},
	})
	if err != nil {
		t.Fatalf("export the injected chain: %v", err)
	}
	want := []rl.SFTSample{
		{SessionID: "session-chain-a", Turn: 1, Input: "first question", Label: "answer to first question", EpisodeID: "episode-chain-a"},
		{SessionID: "session-chain-a", Turn: 2, Input: "second question", Label: "answer to second question", EpisodeID: "episode-chain-a"},
		{SessionID: "session-chain-b", Turn: 3, Input: "third question", Label: "answer to third question", EpisodeID: "episode-chain-a"},
		{SessionID: "session-chain-b", Turn: 1, Input: "fourth question", Label: "answer to fourth question", EpisodeID: "episode-chain-b"},
	}
	if !reflect.DeepEqual(samples, want) {
		t.Fatalf("samples = %+v, want %+v", samples, want)
	}

	unread, err := exporter.Export(ctx, rl.ExportOptions{SessionIDs: []event.SessionID{"session-chain-unknown"}})
	if err != nil {
		t.Fatalf("export an unknown session: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("unknown session samples = %+v, want none", unread)
	}
}
