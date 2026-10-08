// This file pins the framework facts the decision-capture evidence chain rests on,
// read through the real runner and plugin pipeline instead of a mocked plugin sequence.
//
// - Turn attribution installed per run must be visible on the ctx that reaches the model call.
// - The response object the model returned, ID included, must be the one stored as a fact.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#attribution-carrier
package tagent_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"

	tagent "github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
)

// pinnedResponseID is the fixed model.Response.ID the fake model returns.
const pinnedResponseID = "resp-pin-framework-parity"

// frameworkParityModel records what the model layer actually observed and
// returns one terminal, non-partial response carrying a stable ID.
type frameworkParityModel struct {
	seenAttribution map[string]string
	calls           int
}

func (m *frameworkParityModel) Info() model.Info {
	return model.Info{Name: "framework-parity"}
}

func (m *frameworkParityModel) GenerateContent(
	ctx context.Context,
	req *model.Request,
) (<-chan *model.Response, error) {
	m.calls++
	if attr, ok := plugin.AttributionFrom(ctx); ok {
		m.seenAttribution = map[string]string(attr)
	}
	resp := &model.Response{
		ID:   pinnedResponseID,
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{Role: model.RoleAssistant, Content: "parity-pong"},
		}},
	}
	ch := make(chan *model.Response, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

// TestDecisionCapture_FrameworkAttribution pins attribution and response identity across the real runner.
// - A green run is a precondition of the capture attribution plan, read only through exported seams.
// - The store is in-process: no external service and no real provider is contacted here.
func TestDecisionCapture_FrameworkAttribution(t *testing.T) {
	cfg := tagent.Config{
		Entry: "parity",
		Agents: map[string]tagent.AgentConfig{
			"parity": {
				SystemPrompt:      tagent.PromptConfig{Inline: "answer with pong"},
				MaxToolIterations: 2,
			},
		},
	}
	fm := &frameworkParityModel{}
	ta, err := tagent.New(cfg, tagent.WithModel(fm))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	outputCh, err := ta.StartLoop("parity-user", "parity-session")
	require.NoError(t, err)
	ta.InjectMessage(model.NewUserMessage("ping"))

	deadline := time.After(30 * time.Second)
	seenPong := false
	for !seenPong {
		select {
		case evt, ok := <-outputCh:
			if !ok {
				t.Fatal("output channel closed before the terminal assistant response was stored")
			}
			if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
				continue
			}
			if evt.Response.Choices[0].Message.Role == model.RoleAssistant &&
				evt.Response.Choices[0].Message.Content == "parity-pong" {
				seenPong = true
			}
		case <-deadline:
			t.Fatal("no terminal assistant response within 30s")
		}
	}

	require.GreaterOrEqual(t, fm.calls, 1, "model must have been invoked through the real runner")

	require.NotNil(t, fm.seenAttribution, "attribution ctx value must reach model.GenerateContent")
	require.Equal(t, "parity-session", fm.seenAttribution["rollout_id"],
		"rollout id stamped per turn must survive into the model-call ctx")

	store := ta.MemStore()
	require.NotNil(t, store)
	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{memory.PartitionIDFromName("parity")},
		OrderBy:      "timestamp_desc",
		Limit:        50,
	})
	require.NoError(t, err)
	var found string
	for _, ref := range refs {
		evt, gerr := store.GetEvent(ref.EventKey)
		if gerr != nil || evt == nil || evt.Response == nil {
			continue
		}
		if evt.Response.ID != "" {
			found = evt.Response.ID
			break
		}
	}
	require.Equal(t, pinnedResponseID, found,
		"stored FullEvent.Response must be the model's original response object (ID preserved)")
}
