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
	"fmt"
	"os"
	"path/filepath"
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
	scoped, ok := tier.Originals.(ledger.ScopedOriginalStore)
	if !ok {
		t.Fatalf("the memory tier holds %T, want the scoped original read face", tier.Originals)
	}
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
		Originals: scoped,
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

const (
	archiveRecallAgent   = "agent-archive"
	archiveRecallSession = event.SessionID("session-archive")
	archiveRecallParent  = event.EventID("archive-parent")
	archiveRecallChild   = event.EventID("archive-child")
	archiveRecallText    = "archived original\nsecond line"
)

// archiveRecallSurface is one real journal tier whose oldest sealed segment has
// moved to its archive, reopened without the writer's resident or segment caches.
type archiveRecallSurface struct {
	tools       map[string]trpctool.CallableTool
	segmentPath string
}

// openArchiveRecallSurface writes one parent and one child, then uses two
// checkpoint barriers to move the parent's interval into the archive.
func openArchiveRecallSurface(t *testing.T) archiveRecallSurface {
	t.Helper()
	ctx := context.Background()
	root, archiveRoot := t.TempDir(), t.TempDir()
	cfg := ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true}
	cfg.ArchiveRoot = archiveRoot
	cfg.HotLogMaxBytes = 1
	tier, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("open the archive-backed journal tier: %v", err)
	}
	partition := memory.PartitionIDFromName(archiveRecallAgent)
	owner := event.Address{AgentID: archiveRecallAgent, SessionID: archiveRecallSession}
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	parent := event.Event{
		Key:       archiveRecallParent,
		Type:      event.TypeExternalInput,
		Timestamp: at,
		Meta:      map[string]string{event.MetaKeyEventSummary: "archived parent"},
		Message:   model.Message{Role: model.RoleUser, Content: archiveRecallText},
		Owner:     owner,
	}
	body, err := memory.BodyFromEvent(parent, "text/plain", "keep")
	if err != nil {
		t.Fatalf("encode the archived parent: %v", err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("marshal the archived parent: %v", err)
	}
	outcome, err := ledger.CommitFact(ctx, tier, ledger.FactCommit{
		Record: ledger.SegmentRecord{
			PartitionID: partition,
			WindowTS:    at.UnixMilli(),
			Seq:         1,
			EventKey:    parent.Key,
			EventTime:   at.UnixMilli(),
			Body:        encoded,
		}.WithChain(parent.Type, archiveRecallSession),
		Ref: ledger.OriginalRef{
			Key:             parent.Key,
			Owner:           owner,
			Format:          "text/plain",
			RetentionPolicy: "keep",
		},
		Body: []byte(archiveRecallText),
	})
	if err != nil || outcome != ledger.FactCommitted {
		t.Fatalf("commit the archived parent = (%v, %v), want FactCommitted", outcome, err)
	}
	firstCut := fmt.Sprintf("checkpoint:%d:1", partition)
	if err := tier.Store.Commit(ctx, ledger.Batch{Puts: []ledger.Pair{{Key: firstCut, Value: []byte("cut")}}}); err != nil {
		t.Fatalf("commit checkpoint %s: %v", firstCut, err)
	}
	childAt := at.Add(time.Millisecond)
	child := event.Event{
		Key:       archiveRecallChild,
		Type:      event.TypeExternalInput,
		Timestamp: childAt,
		Meta:      map[string]string{event.MetaKeyEventSummary: "hot child"},
		Message:   model.Message{Role: model.RoleUser, Content: "child"},
		Owner:     owner,
	}
	body, err = memory.BodyFromEvent(child, "text/plain", "keep")
	if err != nil {
		t.Fatalf("encode the hot child: %v", err)
	}
	encoded, err = body.Encode()
	if err != nil {
		t.Fatalf("marshal the hot child: %v", err)
	}
	if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{
		PartitionID: partition,
		WindowTS:    childAt.UnixMilli(),
		Seq:         2,
		EventKey:    child.Key,
		EventTime:   childAt.UnixMilli(),
		Body:        encoded,
	}.WithChain(child.Type, archiveRecallSession)); err != nil {
		t.Fatalf("store the hot child: %v", err)
	}
	secondCut := fmt.Sprintf("checkpoint:%d:2", partition)
	if err := tier.Store.Commit(ctx, ledger.Batch{Puts: []ledger.Pair{{Key: secondCut, Value: []byte("cut")}}}); err != nil {
		t.Fatalf("commit checkpoint %s: %v", secondCut, err)
	}
	segments, err := filepath.Glob(filepath.Join(archiveRoot, "segments", "seg-*.log"))
	if err != nil || len(segments) != 1 {
		t.Fatalf("the archive segments = %v err=%v, want the parent's one moved interval", segments, err)
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("close the archive writer: %v", err)
	}
	cfg.WriteRights = false
	tier, err = ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen the archive reader: %v", err)
	}
	t.Cleanup(func() {
		if err := tier.Close(); err != nil {
			t.Errorf("close the archive reader: %v", err)
		}
	})
	scoped, ok := tier.Originals.(ledger.ScopedOriginalStore)
	if !ok {
		t.Fatalf("the journal tier holds %T, want the scoped original read face", tier.Originals)
	}
	source := assembly.NewTierQuerySource(tier.Segments)
	tools := recall.New(recall.Reads{Table: source, Trace: source, Originals: scoped}, []string{archiveRecallAgent})
	surface := make(map[string]trpctool.CallableTool, len(tools))
	for _, tool := range tools {
		surface[tool.Declaration().Name] = tool
	}
	return archiveRecallSurface{tools: surface, segmentPath: segments[0]}
}

// TestRecallGetCrossesArchive pins exact original bytes through the shipped tool after archival.
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestRecallGetCrossesArchive(t *testing.T) {
	surface := openArchiveRecallSurface(t)
	answered, err := surface.tools[recall.GetToolName].Call(context.Background(), []byte(fmt.Sprintf(`{"key":%q}`, archiveRecallParent)))
	if err != nil {
		t.Fatalf("read the archived parent: %v", err)
	}
	answer := answerOf(t, answered, err)
	if found, _ := answer["found"].(bool); !found {
		t.Fatalf("the archived read answered %v, want the original bytes", answer)
	}
	if got, _ := answer["body"].(string); got != archiveRecallText {
		t.Fatalf("the archived body = %q, want %q", got, archiveRecallText)
	}
	if eof, _ := answer["eof"].(bool); !eof {
		t.Fatalf("the archived read answered %v, want the whole original", answer)
	}
}

// TestRecallTraceReportsArchiveMissing pins a moved parent's missing segment staying named.
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestRecallTraceReportsArchiveMissing(t *testing.T) {
	surface := openArchiveRecallSurface(t)
	if err := os.Remove(surface.segmentPath); err != nil {
		t.Fatalf("remove the archived segment: %v", err)
	}
	answered, err := surface.tools[recall.TraceToolName].Call(context.Background(), []byte(fmt.Sprintf(`{"key":%q}`, archiveRecallChild)))
	if !errors.Is(err, ledger.ErrArchiveMissing) {
		t.Fatalf("the trace answered (%v, %v), want ErrArchiveMissing", answered, err)
	}
}

// TestRecallRefusesArchiveBytesThatChanged pins a warm read refusing same-size changed archive bytes.
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestRecallRefusesArchiveBytesThatChanged(t *testing.T) {
	surface := openArchiveRecallSurface(t)
	get := surface.tools[recall.GetToolName]
	args := []byte(fmt.Sprintf(`{"key":%q}`, archiveRecallParent))
	answered, err := get.Call(context.Background(), args)
	if err != nil {
		t.Fatalf("warm the archived read: %v", err)
	}
	if answer := answerOf(t, answered, err); answer["body"] != archiveRecallText {
		t.Fatalf("the warm archived read answered %v, want the original bytes", answer)
	}
	before, err := os.Stat(surface.segmentPath)
	if err != nil {
		t.Fatalf("stat the archived segment: %v", err)
	}
	file, err := os.OpenFile(surface.segmentPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the archived segment to change it: %v", err)
	}
	one := make([]byte, 1)
	if _, err := file.ReadAt(one, before.Size()/2); err != nil {
		file.Close()
		t.Fatalf("read the archived byte to change: %v", err)
	}
	one[0] ^= 0xff
	if _, err := file.WriteAt(one, before.Size()/2); err != nil {
		file.Close()
		t.Fatalf("change the archived byte: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close the changed archived segment: %v", err)
	}
	changedAt := before.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(surface.segmentPath, changedAt, changedAt); err != nil {
		t.Fatalf("change the archived segment timestamp: %v", err)
	}
	after, err := os.Stat(surface.segmentPath)
	if err != nil {
		t.Fatalf("stat the changed archived segment: %v", err)
	}
	if after.Size() != before.Size() || after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("the changed segment stat = (%d, %v), want size %d and a new timestamp", after.Size(), after.ModTime(), before.Size())
	}
	answered, err = get.Call(context.Background(), args)
	if !errors.Is(err, ledger.ErrArchiveCorrupt) {
		t.Fatalf("the read after the change answered (%v, %v), want ErrArchiveCorrupt", answered, err)
	}
}
