package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestArchiveSegment_WritesMemoryStore(t *testing.T) {
	store := memory.NewInMemoryStore()
	sc := NewSmartCompressor(
		WithMemStore(store),
		WithValuationConfig(ValuationConfig{
			ValueFloors: DefaultValuationFloors(),
		}),
	)

	seg := &TaskSegment{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "[evt_100|external_input] user request"},
			{Role: model.RoleAssistant, Content: "[evt_101|agent_output] assistant reply"},
		},
		IsComplete: true,
	}
	value := EventValue{
		EventKey:   100,
		ValueScore: 0.9,
		Processing: Summary,
		KeyFacts:   "user request",
	}

	summaryKey, err := sc.archiveSegment(seg, "summary text", value)
	require.NoError(t, err)
	assert.NotZero(t, summaryKey)

	// Verify the event is stored in MemoryStore
	stored, err := store.GetEvent(summaryKey)
	require.NoError(t, err)
	assert.Equal(t, "context_compress_summary", stored.EventType)
	assert.Equal(t, "summary text", stored.EventSummary)
	assert.Equal(t, "user request", stored.Content)
	assert.Equal(t, "100", stored.Metadata["original_key"])
}

func TestArchiveSegment_CacheReuse(t *testing.T) {
	store := memory.NewInMemoryStore()
	sc := NewSmartCompressor(
		WithMemStore(store),
		WithValuationConfig(ValuationConfig{
			ValueFloors: DefaultValuationFloors(),
		}),
	)

	seg := &TaskSegment{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "same content"},
		},
		IsComplete: true,
	}
	value := EventValue{
		EventKey:   100,
		ValueScore: 0.5,
		Processing: Summary,
	}

	key1, err := sc.archiveSegment(seg, "summary A", value)
	require.NoError(t, err)

	key2, err := sc.archiveSegment(seg, "summary B", value)
	require.NoError(t, err)

	assert.Equal(t, key1, key2, "second archive with same content should reuse cached key")
}

func TestArchiveSegment_NilMemStore(t *testing.T) {
	sc := NewSmartCompressor() // no memStore

	seg := &TaskSegment{
		Messages:   []model.Message{{Role: model.RoleUser, Content: "test"}},
		IsComplete: true,
	}
	value := EventValue{EventKey: 100, ValueScore: 0.5, Processing: Summary}

	_, err := sc.archiveSegment(seg, "summary", value)
	assert.Error(t, err, "archiveSegment should fail when memStore is nil")
}

func TestBuildReferenceMessage(t *testing.T) {
	value := EventValue{
		EventKey:   100,
		ValueScore: 0.8,
		Processing: Reference,
		KeyFacts:   "key facts here",
	}
	msg := buildReferenceMessage(100, 789, value)
	assert.Equal(t, model.RoleSystem, msg.Role)
	assert.Contains(t, msg.Content, "[context_archive]")
	assert.Contains(t, msg.Content, "evt_100")
	assert.Contains(t, msg.Content, "摘要 key: 789")
	assert.Contains(t, msg.Content, "关键事实: key facts here")
	assert.Contains(t, msg.Content, "recall")
}

func TestBuildReferenceMessage_NoKeyFacts(t *testing.T) {
	value := EventValue{
		EventKey:   100,
		ValueScore: 0.3,
		Processing: Drop,
	}
	msg := buildReferenceMessage(100, 789, value)
	assert.Equal(t, model.RoleSystem, msg.Role)
	assert.NotContains(t, msg.Content, "关键事实")
}
