package learning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// chainExportTurn is one turn the chain export check injects: the invocation,
// session and episode it is admitted under, and the input it presents.
type chainExportTurn struct {
	invocation event.InvocationID
	session    event.SessionID
	episode    event.EpisodeID
	input      string
}

// TestChainExporterProducesSamples pins the SFT rows exported from the chain the formal runtime entry committed.
//   - the export is non-empty and pairs each input with the assistant output of its own invocation
//   - episodes group the rows even when one episode spans two sessions, and turn numbers restart per episode
//   - a session the export does not name contributes no row
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestChainExporterProducesSamples(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := formalRun(t, ctx, &collector{}, func(req *model.Request) *model.Response {
		last := req.Messages[len(req.Messages)-1]
		return learnFinalTurn("answer to " + last.Content)
	})
	var _ rl.FactReader = run.DurableFactReader()

	turns := []chainExportTurn{
		{invocation: "inv-chain-1", session: "session-chain-a", episode: "episode-chain-a", input: "first question"},
		{invocation: "inv-chain-2", session: "session-chain-a", episode: "episode-chain-a", input: "second question"},
		{invocation: "inv-chain-3", session: "session-chain-b", episode: "episode-chain-a", input: "third question"},
		{invocation: "inv-chain-4", session: "session-chain-b", episode: "episode-chain-b", input: "fourth question"},
	}
	for _, turn := range turns {
		evt := event.Event{
			Type:      event.TypeExternalInput,
			Owner:     event.Address{AgentID: "agent-a", SessionID: turn.session},
			EpisodeID: turn.episode,
			Message:   model.Message{Role: model.RoleUser, Content: turn.input},
		}
		if _, _, err := run.InjectEvent(ctx, evt, turn.invocation); err != nil {
			t.Fatalf("inject %s: %v", turn.invocation, err)
		}
	}

	exporter := rl.NewChainExporter(run.DurableFactReader())
	samples, err := exporter.Export(ctx, rl.ExportOptions{
		SessionIDs: []event.SessionID{"session-chain-a", "session-chain-b"},
	})
	if err != nil {
		t.Fatalf("export the injected chain: %v", err)
	}
	want := []rl.SFTSample{
		{SessionID: "session-chain-a", Turn: 1, Input: "first question", Label: "answer to first question", EpisodeID: "episode-chain-a"},
		{SessionID: "session-chain-a", Turn: 2, Input: "second question", Label: "answer to second question", EpisodeID: "episode-chain-a"},
		{SessionID: "session-chain-b", Turn: 3, Input: "third question", Label: "answer to third question", EpisodeID: "episode-chain-a"},
		{SessionID: "session-chain-b", Turn: 1, Input: "fourth question", Label: "answer to fourth question", EpisodeID: "episode-chain-b"},
	}
	if !reflect.DeepEqual(samples, want) {
		t.Fatalf("samples = %+v, want %+v", samples, want)
	}

	unread, err := exporter.Export(ctx, rl.ExportOptions{SessionIDs: []event.SessionID{"session-chain-unknown"}})
	if err != nil {
		t.Fatalf("export an unknown session: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("unknown session samples = %+v, want none", unread)
	}
}

// TestExportReadsArchivedFactsByIdentity pins exact archived export and named missing/corrupt failures.
func TestExportReadsArchivedFactsByIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run, addr, before, archiveRoot := openArchivedExportChain(t, ctx)

	segmentPath := ""
	waitForReplacement(t, "the original fact's segment to move", func() bool {
		segmentPath = archivedExportSegmentPath(t, archiveRoot, string(before[0].Key))
		return segmentPath != ""
	})

	reader := run.DurableFactReader()
	facts, err := reader.SessionFacts(ctx, addr)
	if err != nil {
		t.Fatalf("read the archived chain: %v", err)
	}
	if len(facts) < len(before) {
		t.Fatalf("durable facts = %d, want at least the %d original facts", len(facts), len(before))
	}
	for i, want := range before {
		gotJSON, err := json.Marshal(facts[i])
		if err != nil {
			t.Fatalf("encode durable fact %d: %v", i, err)
		}
		wantJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("encode original fact %d: %v", i, err)
		}
		if facts[i].Key != want.Key || !bytes.Equal(gotJSON, wantJSON) {
			t.Fatalf("durable fact %d changed after archival: got %s, want %s", i, gotJSON, wantJSON)
		}
	}

	exporter := rl.NewChainExporter(reader)
	opts := rl.ExportOptions{SessionIDs: []event.SessionID{addr.SessionID}}
	body := strings.Repeat("x", 64<<10)
	wantSamples := []rl.SFTSample{
		{SessionID: string(addr.SessionID), Turn: 1, Input: "archived question", Label: "archived answer", EpisodeID: "episode-export-archive"},
		{SessionID: string(addr.SessionID), Turn: 2, Input: body + " one", Label: "answer to " + body + " one", EpisodeID: "episode-export-archive"},
		{SessionID: string(addr.SessionID), Turn: 3, Input: body + " two", Label: "answer to " + body + " two", EpisodeID: "episode-export-archive"},
		{SessionID: string(addr.SessionID), Turn: 4, Input: body + " three", Label: "answer to " + body + " three", EpisodeID: "episode-export-archive"},
	}
	samples, err := exporter.Export(ctx, opts)
	if err != nil {
		t.Fatalf("export the archived chain: %v", err)
	}
	if !reflect.DeepEqual(samples, wantSamples) {
		t.Fatalf("archived export = %+v, want %+v", samples, wantSamples)
	}

	hidden := segmentPath + ".hidden"
	if err := os.Rename(segmentPath, hidden); err != nil {
		t.Fatalf("remove the archived segment: %v", err)
	}
	missingSamples, missingErr := exporter.Export(ctx, opts)
	if err := os.Rename(hidden, segmentPath); err != nil {
		t.Fatalf("restore the archived segment: %v", err)
	}
	if !errors.Is(missingErr, ledger.ErrArchiveMissing) || missingSamples != nil {
		t.Fatalf("export after removal = (%+v, %v), want no samples and ErrArchiveMissing", missingSamples, missingErr)
	}

	file, err := os.OpenFile(segmentPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the archived segment to change it: %v", err)
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		t.Fatalf("stat the archived segment: %v", err)
	}
	offset := stat.Size() / 2
	one := make([]byte, 1)
	if _, err := file.ReadAt(one, offset); err != nil {
		file.Close()
		t.Fatalf("read the archived byte to change: %v", err)
	}
	original := one[0]
	one[0] ^= 0xff
	if _, err := file.WriteAt(one, offset); err != nil {
		file.Close()
		t.Fatalf("change the archived byte: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close the changed archived segment: %v", err)
	}
	changedAt := stat.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(segmentPath, changedAt, changedAt); err != nil {
		t.Fatalf("change the archived segment timestamp: %v", err)
	}
	corruptSamples, corruptErr := exporter.Export(ctx, opts)
	restored, err := os.OpenFile(segmentPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("reopen the archived segment to restore it: %v", err)
	}
	if _, err := restored.WriteAt([]byte{original}, offset); err != nil {
		restored.Close()
		t.Fatalf("restore the archived byte: %v", err)
	}
	if err := restored.Close(); err != nil {
		t.Fatalf("close the restored archived segment: %v", err)
	}
	if err := os.Chtimes(segmentPath, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatalf("restore the archived segment timestamp: %v", err)
	}
	if !errors.Is(corruptErr, ledger.ErrArchiveCorrupt) || corruptSamples != nil {
		t.Fatalf("export after corruption = (%+v, %v), want no samples and ErrArchiveCorrupt", corruptSamples, corruptErr)
	}
	if _, err := exporter.Export(ctx, opts); err != nil {
		t.Fatalf("export after restoring the archive: %v", err)
	}
}

// openArchivedExportChain commits one original input/output pair through the
// real tier, records its pre-archive bytes, and assembles the runtime over the
// same root with automatic cuts and archival enabled.
func openArchivedExportChain(t *testing.T, ctx context.Context) (*assembly.Run, event.Address, []event.Event, string) {
	t.Helper()
	root, archiveRoot := t.TempDir(), t.TempDir()
	storage := ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true}
	storage.ArchiveRoot = archiveRoot
	storage.HotLogMaxBytes = 128 << 10
	tier, err := ledger.OpenTier(ctx, storage)
	if err != nil {
		t.Fatalf("open the archive-backed journal tier: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-export-archive"}
	at := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	input := event.Event{
		Key:          "archive-export-input",
		Type:         event.TypeExternalInput,
		Owner:        addr,
		Source:       addr,
		InvocationID: "inv-archive-export",
		EpisodeID:    "episode-export-archive",
		Timestamp:    at,
		Message:      model.Message{Role: model.RoleUser, Content: "archived question"},
	}
	output := event.Event{
		Key:          "archive-export-output",
		Type:         event.TypeAgentOutput,
		Owner:        addr,
		InvocationID: "inv-archive-export",
		EpisodeID:    "episode-export-archive",
		Timestamp:    at.Add(time.Millisecond),
		Meta:         map[string]string{event.MetaKeyInputEvents: `["archive-export-input"]`},
		Message:      model.Message{Role: model.RoleAssistant, Content: "archived answer"},
	}
	commitArchiveExportFact(t, ctx, tier, input, 1)
	commitArchiveExportFact(t, ctx, tier, output, 2)
	before := make([]event.Event, 0, 2)
	for _, key := range []event.EventID{input.Key, output.Key} {
		stored, held, err := tier.Segments.GetEvent(ctx, key)
		if err != nil || !held {
			t.Fatalf("read original fact %s = (held=%t, %v)", key, held, err)
		}
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			t.Fatalf("decode original fact %s: %v", key, err)
		}
		fact := body.Event()
		fact.Key = key
		before = append(before, fact)
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("close the original tier writer: %v", err)
	}

	provider := &learnModel{name: "model-a", answer: func(req *model.Request) *model.Response {
		last := req.Messages[len(req.Messages)-1]
		return learnFinalTurn("answer to " + last.Content)
	}}
	decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}}}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(formalPolicy{}), assembly.WithStorage(storage))
	if err != nil {
		t.Fatalf("assemble over the original tier: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer closeCancel()
		if _, err := run.Close(closeCtx, agent.CloseRequest{Deadline: time.Now().Add(20 * time.Second)}); err != nil {
			t.Errorf("close the archive export run: %v", err)
		}
	})
	bootFacts, err := run.DurableFactReader().SessionFacts(ctx, addr)
	if err != nil {
		t.Fatalf("read the original chain before archival: %v", err)
	}
	if len(bootFacts) < len(before) {
		t.Fatalf("boot facts = %d, want the %d original facts", len(bootFacts), len(before))
	}
	for i := range before {
		if bootFacts[i].Key != before[i].Key {
			t.Fatalf("boot fact %d = %s, want original identity %s", i, bootFacts[i].Key, before[i].Key)
		}
	}
	body := strings.Repeat("x", 64<<10)
	questions := []string{body + " one", body + " two", body + " three"}
	for i, question := range questions {
		if _, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: event.InvocationID("inv-archive-current-" + string(rune('1'+i))),
			Address:      addr,
			Episode:      input.EpisodeID,
			Input:        []model.Message{{Role: model.RoleUser, Content: question}},
		}); err != nil {
			t.Fatalf("invoke the current turn %d: %v", i+1, err)
		}
		if _, filed, err := run.Checkpoint(ctx); err != nil || !filed {
			t.Fatalf("checkpoint the current turn %d: filed=%v err=%v", i+1, filed, err)
		}
	}
	return run, addr, before, archiveRoot
}

// commitArchiveExportFact commits one original fact and its recall body as the
// same durable unit.
func commitArchiveExportFact(t *testing.T, ctx context.Context, tier *ledger.Tier, fact event.Event, seq int64) {
	t.Helper()
	body, err := memory.BodyFromEvent(fact, "text/plain", "keep")
	if err != nil {
		t.Fatalf("encode original fact %s: %v", fact.Key, err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("marshal original fact %s: %v", fact.Key, err)
	}
	outcome, err := ledger.CommitFact(ctx, tier, ledger.FactCommit{
		Record: ledger.SegmentRecord{
			PartitionID: memory.PartitionIDFromName(string(fact.Owner.AgentID)),
			WindowTS:    fact.Timestamp.UnixMilli(),
			Seq:         seq,
			EventKey:    fact.Key,
			EventTime:   fact.Timestamp.UnixMilli(),
			Body:        encoded,
		}.WithChain(fact.Type, fact.Owner.SessionID),
		Ref: ledger.OriginalRef{
			Key:             fact.Key,
			Owner:           fact.Owner,
			Format:          "text/plain",
			RetentionPolicy: "keep",
		},
		Body: []byte(fact.Message.Content),
	})
	if err != nil || outcome != ledger.FactCommitted {
		t.Fatalf("commit original fact %s = (%v, %v), want FactCommitted", fact.Key, outcome, err)
	}
}

// archivedExportSegmentPath reports the segment whose stated key domain contains
// key, or an empty path while the automatic move has not landed yet.
func archivedExportSegmentPath(t *testing.T, archiveRoot, key string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(archiveRoot, "manifest.a"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatalf("read the archive manifest: %v", err)
	}
	var manifest struct {
		Segments []struct {
			SegmentID string `json:"segment_id"`
			KeyDomain struct {
				FirstKey string `json:"first_key"`
				LastKey  string `json:"last_key"`
			} `json:"key_domain"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode the archive manifest: %v", err)
	}
	for _, segment := range manifest.Segments {
		if segment.KeyDomain.FirstKey != "" && segment.KeyDomain.FirstKey <= key && key <= segment.KeyDomain.LastKey {
			return filepath.Join(archiveRoot, "segments", segment.SegmentID+".log")
		}
	}
	return ""
}
