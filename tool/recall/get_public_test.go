// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
//
// The read-failure honesty checks of the exact read: they call the shipped tool
// entry over the production durable read face, so what they pin is the answer a
// deployment gives, not the answer of a stand-in table. Corruption reaches the
// caller by name while a key the chain really does not hold stays missing.
package recall_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/tool/recall"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// corruptKey is the event the fixture stores with a body no reader can decode,
// beside a body that decodes, so one surface carries corruption and a readable
// event at once.
const corruptKey event.EventID = "corrupt-body"

// answerOf decodes the JSON text one call answered with, and reports the error
// the call returned instead when it refused the call.
func answerOf(t *testing.T, answered any, err error) map[string]any {
	t.Helper()
	if err != nil {
		return nil
	}
	text, ok := answered.(string)
	if !ok {
		t.Fatalf("a call answered %T, want the JSON text every answer is rendered as", answered)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("decode the answer %q: %v", text, err)
	}
	return decoded
}

// corruptSurface builds the read face the recall tools consume over one durable
// tier holding a readable event beside a body that cannot be decoded.
func corruptSurface(t *testing.T) map[string]trpctool.CallableTool {
	t.Helper()
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageMemory})
	if err != nil {
		t.Fatalf("open the durable tier: %v", err)
	}
	t.Cleanup(func() { tier.Segments.Close() })
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	partition := memory.PartitionIDFromName("agent-a")
	store := func(key event.EventID, body []byte) {
		t.Helper()
		record := ledger.SegmentRecord{
			PartitionID: partition,
			WindowTS:    at.UnixMilli(),
			Seq:         int64(len(key)),
			EventKey:    key,
			EventTime:   at.UnixMilli(),
			Body:        body,
		}
		if err := tier.Segments.StoreEvent(ctx, record.WithChain(event.TypeExternalInput, "session-corrupt")); err != nil {
			t.Fatalf("store %s: %v", key, err)
		}
	}
	readable := event.Event{
		Key:       "readable-body",
		Type:      event.TypeExternalInput,
		Timestamp: at,
		Meta:      map[string]string{event.MetaKeyEventSummary: "ask"},
		Message:   model.Message{Content: "the readable material"},
		Owner:     event.Address{AgentID: "agent-a", SessionID: "session-corrupt"},
	}
	body, err := memory.BodyFromEvent(readable, "test", "test")
	if err != nil {
		t.Fatalf("encode the readable event: %v", err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("marshal the readable event: %v", err)
	}
	store(readable.Key, encoded)
	store(corruptKey, []byte("not-an-event"))
	tools := recall.New(recall.Reads{
		Table:     assembly.NewTierQuerySource(tier.Segments),
		Originals: ledger.NewMemoryOriginalStore(),
	}, []string{"agent-a"})
	surface := make(map[string]trpctool.CallableTool, len(tools))
	for _, tool := range tools {
		surface[tool.Declaration().Name] = tool
	}
	return surface
}

// TestRecallCorruptionNotMissing pins the symptom the read face has to answer for: an event the chain holds with a body it cannot decode is corruption.
//   - a read of it never comes back as a key the memory does not hold
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestRecallCorruptionNotMissing(t *testing.T) {
	get := corruptSurface(t)[recall.GetToolName]
	answered, err := get.Call(context.Background(), []byte(`{"key":"`+string(corruptKey)+`"}`))
	if err != nil {
		if !errors.Is(err, memory.ErrEventBody) {
			t.Fatalf("the corrupt read refused with %v, want the cause %v", err, memory.ErrEventBody)
		}
		return
	}
	answer := answerOf(t, answered, err)
	if reason, _ := answer["reason"].(string); reason == "missing" {
		t.Fatalf("the corrupt read answered %v, want the corruption named instead of a missing key", answer)
	}
	t.Fatalf("the corrupt read answered %v, want the corruption named", answer)
}

// TestRecallGetPublicCorruption pins corruption apart from absence through the shipped tool entry.
//   - a body that cannot be decoded refuses the call with its cause
//   - a key the chain holds no event for still answers missing
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestRecallGetPublicCorruption(t *testing.T) {
	tools := corruptSurface(t)
	get := tools[recall.GetToolName]
	answered, err := get.Call(context.Background(), []byte(`{"key":"`+string(corruptKey)+`"}`))
	if err == nil {
		t.Fatalf("the corrupt read answered %v with no error, want the corruption named", answered)
	}
	if !errors.Is(err, memory.ErrEventBody) {
		t.Fatalf("the corrupt read refused with %v, want the cause %v", err, memory.ErrEventBody)
	}
	if strings.Contains(err.Error(), "missing") {
		t.Fatalf("the corrupt read refused with %v, want no missing key in a corruption refusal", err)
	}
	absent, err := get.Call(context.Background(), []byte(`{"key":"absent-body"}`))
	answer := answerOf(t, absent, err)
	if reason, _ := answer["reason"].(string); reason != "missing" {
		t.Fatalf("the absent key answered %v, want the missing reason", answer)
	}
	if found, _ := answer["found"].(bool); found {
		t.Fatalf("the absent key answered %v, want no served text", answer)
	}
}
