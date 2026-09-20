package agent

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §4.3 (design 决策4 L106): 有效非文本载荷必须留在事实与实际请求中，不能因驱动文本
// 为空跳过。An image-only input has empty Content but valid ContentParts. Pre-§4.3
// buildBusFact froze only Content/ToolCalls (parts lost — the §3.4-deferred item) and
// BuildInvocation read Content only (image-only merged to empty → dropped by the loop's
// empty gate). Both must now preserve the parts.

func imageOnlyEvent() *AgentEvent {
	return &AgentEvent{
		ID: "m", Type: tagentevent.TypeExternalInput, Source: "user", Timestamp: time.Now(),
		Message: &model.Message{Role: model.RoleUser, Content: "", ContentParts: []model.ContentPart{
			{Type: model.ContentTypeImage, Image: &model.Image{URL: "http://host/img.png"}}}},
		Metadata: map[string]any{},
	}
}

func TestBuildBusFact_FreezesMultimodalContentParts(t *testing.T) {
	cm := &ContextManager{partitionID: 1, memStore: memory.NewInMemoryStore(), projection: compress.NewSessionProjection()}
	fact := cm.buildBusFact(imageOnlyEvent())
	require.Len(t, fact.ContentParts, 1, "§4.3/§3.4: multimodal parts must be frozen onto the canonical fact")
	require.NotNil(t, fact.ContentParts[0].Image)
	require.Equal(t, "http://host/img.png", fact.ContentParts[0].Image.URL)
}

func TestBuildInvocation_KeepsImageOnlyInput(t *testing.T) {
	cm := &ContextManager{partitionID: 1}
	msg := cm.BuildInvocation([]*AgentEvent{imageOnlyEvent()})
	require.Empty(t, msg.Content, "image-only input has no text")
	require.NotEmpty(t, msg.ContentParts,
		"§4.3: an image-only input must NOT collapse to an empty invocation (else the loop's empty gate drops it)")
}
