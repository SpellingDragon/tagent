package assembly

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// recordingEmbedder records every text the vector seam is asked to embed.
type recordingEmbedder struct {
	mu    sync.Mutex
	texts []string
}

// Embed records text and answers one fixed vector.
func (e *recordingEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.texts = append(e.texts, text)
	return []float32{1}, nil
}

// recorded reports the texts seen so far.
func (e *recordingEmbedder) recorded() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.texts...)
}

// TestVectorSeamNeverSeesMechanismWording pins the seam rule.
// - the retrieval supply embeds business facts only
// - a compression mechanism fact never submits its internal summary
//
// 契约: docs/wiki/memory/memory-architecture.md#embedder-contract
func TestVectorSeamNeverSeesMechanismWording(t *testing.T) {
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageMemory})
	if err != nil {
		t.Fatalf("open the tier: %v", err)
	}
	defer tier.Segments.Close()

	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	store := func(key event.EventID, typ string, summary, content string, seq int64) {
		t.Helper()
		evt := event.Event{
			Key:       key,
			Type:      typ,
			Timestamp: at,
			Meta:      map[string]string{event.MetaKeyEventSummary: summary},
			Message:   model.Message{Content: content},
			Owner:     event.Address{AgentID: "agent-a", SessionID: "session-seam"},
		}
		body, err := memory.BodyFromEvent(evt, "test", "test")
		if err != nil {
			t.Fatalf("encode %s: %v", key, err)
		}
		encoded, err := body.Encode()
		if err != nil {
			t.Fatalf("marshal %s: %v", key, err)
		}
		if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{
			PartitionID: 1,
			WindowTS:    at.UnixMilli(),
			Seq:         seq,
			EventKey:    key,
			EventTime:   at.UnixMilli(),
			Body:        encoded,
		}.WithChain(typ, evt.Owner.SessionID)); err != nil {
			t.Fatalf("store %s: %v", key, err)
		}
	}
	store("business-134000", event.TypeExternalInput, "ask", "the material of the landing walk", 1)
	store("cmpout-product", event.CompressProductType, "compress product -hist-internal", "", 2)
	store("cmp-request", event.CompressRequestType, "compress request -hist-internal", "", 3)

	embedder := &recordingEmbedder{}
	supply, err := supplyRetrieval(ctx, tier, embedder)
	if err != nil {
		t.Fatalf("wire the retrieval supply: %v", err)
	}
	defer supply.seam.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ready := supply.seam.Vector("business-134000"); ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the business fact never produced its vector")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, text := range embedder.recorded() {
		if strings.Contains(text, "compress product") || strings.Contains(text, "compress request") {
			t.Fatalf("the vector seam embedded internal mechanism wording %q", text)
		}
	}
	found := false
	for _, text := range embedder.recorded() {
		if text == "the material of the landing walk" {
			found = true
		}
	}
	if !found {
		t.Fatal("the business fact was never embedded")
	}
}
