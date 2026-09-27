package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestRun_MediaOnlyInputIsNotDropped locks the code-review W-2 fix: an image/file-only
// delegation (empty Content, valid ContentParts) must not be flattened to empty before
// the event is built, and must therefore still reach the model rather than be skipped.
func TestRun_MediaOnlyInputIsNotDropped(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{finalTextResponse("c1", "handled")},
	}
	ta, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are a vision agent.",
		Name: "vision", Description: "v", MaxToolIterations: 3,
	})
	require.NoError(t, err)
	defer ta.Close()

	mediaOnly := model.Message{
		Role: model.RoleUser, Content: "",
		ContentParts: []model.ContentPart{
			{Type: model.ContentTypeImage, Image: &model.Image{URL: "http://host/img.png"}},
		},
	}
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(mediaOnly))
	ch, err := ta.Run(context.Background(), inv)
	require.NoError(t, err, "media-only input must NOT be rejected as empty (parts must survive)")
	require.NotNil(t, ch)
	for range ch { // drain to completion
	}
	require.GreaterOrEqual(t, callCount, 1,
		"the model must be invoked for a media-only turn — not silently skipped")
}

// TestRun_EmptyInputIsRejectedExplicitly locks the code-review W-1 fix: a delegation with
// neither content nor parts must fail loudly at the boundary, not silently close the
// caller's channel with zero events (which routing through processTurn's durable empty-
// invocation skip would otherwise do).
func TestRun_EmptyInputIsRejectedExplicitly(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{callCount: &callCount, responses: []*model.Response{finalTextResponse("c1", "x")}}
	ta, err := NewTagentAgent(&TagentConfig{Model: seq, SystemPrompt: "s", Name: "empty", Description: "e"})
	require.NoError(t, err)
	defer ta.Close()

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("")))
	ch, err := ta.Run(context.Background(), inv)
	require.Error(t, err, "truly-empty delegation input must be rejected explicitly")
	require.Nil(t, ch)
	require.Equal(t, 0, callCount, "no model call for a rejected input")
}
