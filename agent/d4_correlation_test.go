package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestNewDelegationEvent_CarriesCorrelationHandle locks S1 (design D4): a
// request/response sub-call input rides its originating invocation ID as the
// correlation handle a resident owner will later use to route a background
// follow-up back to the right waiting request.
func TestNewDelegationEvent_CarriesCorrelationHandle(t *testing.T) {
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("do it")))
	require.NotEmpty(t, inv.InvocationID, "NewInvocation always assigns a UUID")

	evt := newDelegationEvent(inv, model.NewUserMessage("do it"))
	require.Equal(t, event.TypeExternalInput, evt.Type)
	require.Equal(t, "user", evt.Source)
	require.Equal(t, inv.InvocationID, evt.Metadata[metaKeyInvocationID],
		"the correlation handle must ride the delegation input")
	require.NotNil(t, evt.Message)
	require.Equal(t, "do it", evt.Message.Content, "the message payload is preserved unchanged")
}

// TestNewDelegationEvent_NilInvocationIsSafe guards the helper against a nil/ID-less
// invocation (no panic, no bogus handle).
func TestNewDelegationEvent_NilInvocationIsSafe(t *testing.T) {
	evt := newDelegationEvent(nil, model.NewUserMessage("x"))
	require.NotContains(t, evt.Metadata, metaKeyInvocationID)
}

// TestInvocationIDIsControlKey locks the D4/7.3 hygiene invariant the whole S1→S3
// migration must preserve: the correlation handle is CONTROL metadata — it must
// NEVER be extracted into invocation metadata (and therefore never forwarded as
// user-visible meta_* / model-visible text the model could spoof to fake routing).
func TestInvocationIDIsControlKey(t *testing.T) {
	require.True(t, controlMetaKeys[metaKeyInvocationID],
		"invocation_id must be a control key (D4: 控制字段不透传给模型)")

	evt := NewExternalInputEvent("user", model.NewUserMessage("hi"))
	evt.Metadata[metaKeyInvocationID] = "corr-123"
	evt.Metadata["chat_id"] = "chat-9" // a legitimate business meta that SHOULD propagate

	md := extractRootMetadata([]*AgentEvent{evt})
	require.Equal(t, "chat-9", md["chat_id"], "ordinary metadata still propagates normally")
	require.NotContains(t, md, metaKeyInvocationID,
		"the correlation handle must not leak into invocation/model-visible metadata")
}
