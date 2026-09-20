package compress

import (
	"context"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §4.3: a valid non-text input must survive into the ACTUAL request. An image-only
// external_input (empty Content, non-empty ContentParts) must render to a message
// carrying those parts. Pre-§4.3 resolveRef's content-resolution branch keyed only on
// Content/ToolCalls, so an image-only fact fell through to summary-only rendering and
// the parts never reached the model.
func TestResolveRef_RendersImageOnlyInputParts(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	parts := []model.ContentPart{{Type: model.ContentTypeImage, Image: &model.Image{URL: "http://host/img.png"}}}
	key := memory.NewSnowflakeEventKey(1, time.Now().UnixMilli()) // a valid partition-1 key so GetEvent's pid derivation hits
	memStore.StoreEvent(key, memory.FullEvent{
		EventKey:     key,
		PartitionID:  1,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "an image",
		Content:      "", // image-only: no text
		ContentParts: parts,
	})
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 8000, 0.8, 2)

	msgs := cc.resolveRefs(context.Background(), []memory.EventReference{
		{EventKey: key, PartitionID: 1, EventType: tagentevent.TypeExternalInput, EventSummary: "an image"},
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 rendered message, got %d", len(msgs))
	}
	if len(msgs[0].ContentParts) != 1 || msgs[0].ContentParts[0].Image == nil ||
		msgs[0].ContentParts[0].Image.URL != "http://host/img.png" {
		t.Fatalf("§4.3: image parts must reach the rendered request, got %+v", msgs[0].ContentParts)
	}
}
