package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// storeErrStore fails every StoreEvent (reads/replay are inherited from the embedded
// store) — the "framework logs the plugin error and continues past it" shape that §4.5
// must NOT allow to cross the durable commit gate.
type storeErrStore struct{ *memory.InMemoryStore }

func (s *storeErrStore) StoreEvent(int64, memory.FullEvent) error {
	return errors.New("disk full")
}

// §4.4 replaces the coarse whole-turn "first envelope / any user" skip (old
// DurableInbound.FactsPrePersisted) with a precise per-attempt EchoCredential: MemoryPlugin
// skips ONLY the event that is THIS attempt's root input echo (root invocation,
// author=user, user role, and content equal to the committed merged message). These
// tests lock both directions: the real echo is skipped (no double-store) AND everything
// else (other user content, assistant, non-root/absent invocation) takes the normal
// store path — the over-skip the whole-turn flag risked.

const mergedInput = "msg-A\n\n---\n\nmsg-B"

// rootInv is a zero invocation whose GetParentInvocation() is nil → treated as the root.
func rootInv() *agent.Invocation { return &agent.Invocation{} }

// echoCtx returns a context carrying an attempt credential expecting mergedInput.
func echoCtx(merged string) context.Context {
	return WithEchoCredential(context.Background(), &EchoCredential{
		AttemptToken:  "resident#attempt-1",
		Agent:         "resident",
		Session:       "sess-1",
		MergedMessage: merged,
		CommittedKeys: []int64{111, 222},
	})
}

// userEvent builds a framework-shaped user input echo (root, author "user").
func userEvent(author string, content string) *event.Event {
	return &event.Event{
		Author: author,
		Response: &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleUser, Content: content}}},
		},
	}
}

// The exact merged echo of a committed durable batch must be skipped by the pipeline
// (the event loop already stored the per-message facts), producing zero new facts.
func TestMemoryPlugin_ExpectedRootEcho_Skipped(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	if _, err := mp.OnEvent(echoCtx(mergedInput), rootInv(), userEvent("user", mergedInput)); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if got := store.GetStats().TotalEvents; got != 0 {
		t.Fatalf("expected root echo must NOT be re-stored (would double-write), got %d facts", got)
	}
}

// A user event whose content is NOT the committed merged message (e.g. a subsequent
// same-turn user message) must take the NORMAL path — the whole-turn flag would have
// wrongly skipped it (§4.4 over-skip fix).
func TestMemoryPlugin_DifferentUserContent_NotSkipped(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	if _, err := mp.OnEvent(echoCtx(mergedInput), rootInv(), userEvent("user", "a completely different user message")); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if got := store.GetStats().TotalEvents; got != 1 {
		t.Fatalf("non-matching user message must still be stored, got %d", got)
	}
}

// An assistant output in the same durable turn must be stored (not skipped).
func TestMemoryPlugin_AssistantOutput_StillStored(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	assistantEvt := &event.Event{
		Author:   "resident",
		Response: &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	if _, err := mp.OnEvent(echoCtx(mergedInput), rootInv(), assistantEvt); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if got := store.GetStats().TotalEvents; got != 1 {
		t.Fatalf("assistant output must still be stored, got %d", got)
	}
}

// Even a user message EQUAL to the merged input but on a NON-root (absent) invocation
// must not be skipped — only the positively-identified root echo qualifies (sub-call /
// absent-invocation guard; real sub-call covered by the §4.4 grounding e2e + §4.8).
func TestMemoryPlugin_NonRootInvocation_NotSkipped(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	if _, err := mp.OnEvent(echoCtx(mergedInput), nil, userEvent("user", mergedInput)); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if got := store.GetStats().TotalEvents; got != 1 {
		t.Fatalf("non-root invocation must not trigger the echo skip, got %d", got)
	}
}

// Without a credential (non-durable turn) the behavior is unchanged: the message stores.
func TestMemoryPlugin_NoCredential_Unchanged(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)
	if _, err := mp.OnEvent(context.Background(), rootInv(), userEvent("user", "plain")); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if got := store.GetStats().TotalEvents; got != 1 {
		t.Fatalf("plain path must still store, got %d", got)
	}
}

// §4.3 all-chain: the PLUGIN store path must carry non-text parts too (parity with the
// durable buildBusFact). A volatile image-only user input stored via onEvent must keep its
// ContentParts, so a later compression/rebuild (which reads FullEvent.ContentParts) does not
// silently drop the image. Fails red if onEvent omits `fullEvent.ContentParts`.
func TestMemoryPlugin_PluginPathPreservesContentParts(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	inv := &agent.Invocation{AgentName: "resident"}
	evt := &event.Event{
		Author: "user",
		Response: &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleUser,
			ContentParts: []model.ContentPart{{
				Type:  model.ContentTypeImage,
				Image: &model.Image{URL: "https://example.com/pic.png"},
			}},
		}}}},
	}
	if _, err := mp.OnEvent(context.Background(), inv, evt); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	require.Equal(t, 1, store.GetStats().TotalEvents, "the volatile image input must be stored")
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{memory.PartitionIDFromName("resident")}, Limit: 10})
	require.NoError(t, err)
	require.Len(t, refs, 1, "the volatile image input must be queryable on its partition")
	fe, gerr := store.GetEvent(refs[0].EventKey)
	require.NoError(t, gerr)
	require.Len(t, fe.ContentParts, 1, "plugin store path must persist ContentParts (not drop the image)")
	require.NotNil(t, fe.ContentParts[0].Image)
	require.Equal(t, "https://example.com/pic.png", fe.ContentParts[0].Image.URL)
}

// §4.5: the framework LOGS a plugin error and continues (OnEvent returns nil even when
// the store failed), so a swallowed store failure during a credentialed turn MUST downgrade
// the attempt's EchoCredential (MarkRejected) — the credential STATE becomes the authority
// the model-entry gate / loop ack consult, since the returned error is not propagated.
func TestMemoryPlugin_SwallowedStoreErrorDowngradesCredential(t *testing.T) {
	store := &storeErrStore{InMemoryStore: memory.NewInMemoryStore()}
	mp := NewMemoryPlugin(store)
	ctx := echoCtx(mergedInput)
	cred, ok := EchoCredentialFrom(ctx)
	require.True(t, ok)

	// The exact root echo is skipped and BINDS the credential → verified (normal path).
	if _, err := mp.OnEvent(ctx, rootInv(), userEvent("user", mergedInput)); err != nil {
		t.Fatalf("OnEvent echo: %v", err)
	}
	require.True(t, cred.Verified(), "a bound root echo makes the credential verified")

	// An assistant output whose StoreEvent fails (framework-swallowed) must reject it.
	assistantEvt := &event.Event{
		Author:   "resident",
		Response: &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	if _, err := mp.OnEvent(ctx, rootInv(), assistantEvt); err != nil {
		t.Fatalf("OnEvent assistant must not surface the swallowed store error: %v", err)
	}
	require.False(t, cred.Verified(), "a swallowed store error during a credentialed turn must MarkRejected the credential (§4.5)")
}

// §4.7 (persistent-event-loop scenario「精确回显隔离」): ONE credentialed durable turn threads
// the exact committed input echo, an assistant output, a tool result, a subsequent DIFFERENT
// user message, and a same-content-but-non-root user event through the REAL plugin. ONLY the
// exact root input echo is skipped (already committed by the loop); every other event —
// including one carrying identical merged text on a non-root invocation — takes the normal
// store path. Locks that §4.4/§4.5 echo identification never wrongly skips assistant/tool/
// subsequent-user/other-invocation events (real parent-bearing sub-call turns are covered by
// the §4.4 grounding e2e; source-priority / Metadata-merge / sync-tool ordering by the agent
// metadata_propagation + session_subagent_toolstop suites).
func TestMemoryPlugin_PreciseEchoIsolation_Threading(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)
	cred := &EchoCredential{AttemptToken: "resident#attempt-1", Agent: "resident", Session: "sess-1", MergedMessage: mergedInput, CommittedKeys: []int64{111, 222}}
	ctx := WithEchoCredential(context.Background(), cred)

	// 1. Exact root input echo → SKIPPED (the only event that must not store).
	if _, err := mp.OnEvent(ctx, rootInv(), userEvent("user", mergedInput)); err != nil {
		t.Fatalf("echo: %v", err)
	}
	// 2. Assistant output → stored.
	if _, err := mp.OnEvent(ctx, rootInv(), &event.Event{Author: "resident",
		Response: &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}}}); err != nil {
		t.Fatalf("assistant: %v", err)
	}
	// 3. Tool result → stored (participates in the current ReAct; not an echo).
	if _, err := mp.OnEvent(ctx, rootInv(), &event.Event{Author: "resident",
		Response: &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleTool, Content: "tool output", ToolID: "tc1"}}}}}); err != nil {
		t.Fatalf("tool: %v", err)
	}
	// 4. Subsequent DIFFERENT user message → stored (whole-turn style skipping would drop it).
	if _, err := mp.OnEvent(ctx, rootInv(), userEvent("user", "an entirely different later question")); err != nil {
		t.Fatalf("later-user: %v", err)
	}
	// 5. SAME merged content on a NON-root invocation → stored (skip is per-exact-root-echo,
	//    not content-equality-forever).
	if _, err := mp.OnEvent(ctx, nil, userEvent("user", mergedInput)); err != nil {
		t.Fatalf("same-nonroot: %v", err)
	}

	require.Equal(t, 4, store.GetStats().TotalEvents,
		"§4.7: exactly the one root echo is skipped; assistant/tool/subsequent-user/same-non-root all store")
	require.True(t, cred.Verified(), "the root echo Bind makes the credential verified")
}
