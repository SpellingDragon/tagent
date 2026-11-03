package host

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// quietModel stands in for the declared model of the relation-wiring anchor:
// the anchor commits its facts directly and drives no turn, so the model is
// never invoked.
type quietModel struct{}

func (quietModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	close(ch)
	return ch, nil
}

func (quietModel) Info() model.Info { return model.Info{Name: "model-quiet"} }

// TestProjectionFoldsByTheWiredConsumptionRelation pins the production wiring of the consumption relation.
//   - the entry assembly hands each fact's input_events to the projection
//   - an output that names only part of the inputs it arrived beside closes its segment with the named ones and the unnamed one rolls to the next segment
//   - without the wiring line every entry reads position only, the late input folds into the answered segment, and this anchor goes red by name
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestProjectionFoldsByTheWiredConsumptionRelation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-quiet"}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: quietModel{}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the run: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-relation-wiring"}
	base := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	commit := func(key string, typ string, role model.Role, content string, meta map[string]string) {
		t.Helper()
		if _, err := run.CommitToSession(ctx, addr, event.Event{
			Key:       event.EventID(key),
			Type:      typ,
			Timestamp: base.Add(time.Duration(len(key)) * time.Minute),
			Meta:      meta,
			Message:   model.Message{Role: role, Content: content},
		}); err != nil {
			t.Fatalf("commit %s: %v", key, err)
		}
	}
	commit("1-in", event.TypeExternalInput, model.RoleUser, "the answered input", nil)
	commit("2-in", event.TypeExternalInput, model.RoleUser, "the late input the next turn consumes", nil)
	commit("3-out", event.TypeAgentOutput, model.RoleAssistant, "answer one", map[string]string{
		event.MetaKeyInputEvents: fmt.Sprintf("[%q]", "1-in"),
	})
	entries, held := run.ReplayEntries(addr.SessionID)
	if !held {
		t.Fatalf("session %q registered no projection", addr.SessionID)
	}
	segments := replay.SegmentsOf(entries, replay.DefaultAgeKeepRecent)
	keyIn := func(seg replay.Segment, key string) bool {
		for _, entry := range seg.Entries {
			if entry.Key == event.EventID(key) {
				return true
			}
		}
		return false
	}
	var answered, trailing *replay.Segment
	for at := range segments {
		if keyIn(segments[at], "3-out") {
			answered = &segments[at]
		} else if keyIn(segments[at], "2-in") {
			trailing = &segments[at]
		}
	}
	if answered == nil {
		t.Fatalf("no segment carries the output: %+v", segments)
	}
	if !keyIn(*answered, "1-in") {
		t.Fatalf("the answered segment misses the named input: %+v", answered.Entries)
	}
	if keyIn(*answered, "2-in") {
		t.Fatalf("the late input folded into the answered segment beside no relation naming it: %+v", answered.Entries)
	}
	if trailing == nil {
		t.Fatalf("the late input rolled into no next segment: %+v", segments)
	}
}
