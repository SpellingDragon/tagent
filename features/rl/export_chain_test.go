package rl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// chainFacts is the read-only chain stand-in of these checks: it answers the
// facts one session holds and counts every session it was asked for, so a
// repeated read scope is observable.
type chainFacts struct {
	sessions map[event.SessionID][]event.Event
	reads    []event.SessionID
}

// SessionFacts reports the standardised facts of one session.
func (f *chainFacts) SessionFacts(addr event.Address) []event.Event {
	f.reads = append(f.reads, addr.SessionID)
	return append([]event.Event(nil), f.sessions[addr.SessionID]...)
}

// chainInput is one external input fact of the chain.
func chainInput(invocation event.InvocationID, episode event.EpisodeID, content string) event.Event {
	return event.Event{
		Type:         event.TypeExternalInput,
		InvocationID: invocation,
		EpisodeID:    episode,
		Message:      model.Message{Role: model.RoleUser, Content: content},
	}
}

// chainOutput is one assistant output fact of the chain.
func chainOutput(invocation event.InvocationID, episode event.EpisodeID, content string) event.Event {
	return event.Event{
		Type:         event.TypeAgentOutput,
		InvocationID: invocation,
		EpisodeID:    episode,
		Message:      model.Message{Role: model.RoleAssistant, Content: content},
	}
}

// TestChainExporterPairsTurns pins the chain export: rows are paired by chain order inside one episode.
// - one episode spanning two sessions stays one episode with its turns numbered in order
// - inputs that land before an answer arrive batch into the turn that answer closes, and a turn that produced no assistant output is skipped, not filled
// - q4 lands, the late input batches beside it, and the output that follows answers both in one turn
// - a repeated session is read once, and the limit bounds the rows emitted
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestChainExporterPairsTurns(t *testing.T) {
	const (
		episodeA = event.EpisodeID("episode-a")
		episodeB = event.EpisodeID("episode-b")
	)
	first := []event.Event{
		chainInput("inv-1", episodeA, "q1"),
		chainOutput("inv-1", episodeA, "a1"),
		{Type: event.TypeActionCommand, InvocationID: "inv-1", EpisodeID: episodeA},
		chainInput("inv-2", episodeA, "q2"),
		chainOutput("inv-2", episodeA, "a2"),
		chainInput("inv-3", episodeB, "q3"),
		chainOutput("inv-3", episodeB, "a3"),
		chainInput("inv-4", episodeB, "q4"),
		chainInput("", episodeA, "unpaired"),
		chainOutput("", episodeA, "unpaired"),
	}
	second := []event.Event{
		chainInput("inv-5", episodeA, "q5"),
		chainOutput("inv-5", episodeA, "a5"),
	}
	facts := &chainFacts{sessions: map[event.SessionID][]event.Event{
		"session-1": first,
		"session-2": second,
	}}
	exporter := NewChainExporter(facts)
	samples, err := exporter.Export(context.Background(), ExportOptions{
		SessionIDs: []event.SessionID{"session-1", "session-2", "session-1"},
	})
	if err != nil {
		t.Fatalf("export the chain: %v", err)
	}
	if len(samples) != 5 {
		t.Fatalf("samples = %+v, want the five paired turns", samples)
	}
	expected := []SFTSample{
		{SessionID: "session-1", Turn: 1, Input: "q1", Label: "a1", EpisodeID: string(episodeA)},
		{SessionID: "session-1", Turn: 2, Input: "q2", Label: "a2", EpisodeID: string(episodeA)},
		{SessionID: "session-1", Turn: 1, Input: "q3", Label: "a3", EpisodeID: string(episodeB)},
		{SessionID: "session-1", Turn: 2, Input: "q4\nunpaired", Label: "unpaired", EpisodeID: string(episodeB)},
		{SessionID: "session-2", Turn: 3, Input: "q5", Label: "a5", EpisodeID: string(episodeA)},
	}
	if !reflect.DeepEqual(samples, expected) {
		t.Fatalf("samples = %+v, want %+v", samples, expected)
	}
	if !reflect.DeepEqual(facts.reads, []event.SessionID{"session-1", "session-2"}) {
		t.Fatalf("session reads = %v, want each named session read once", facts.reads)
	}

	limited, err := exporter.Export(context.Background(), ExportOptions{SessionIDs: []event.SessionID{"session-1"}, Limit: 2})
	if err != nil {
		t.Fatalf("export with a limit: %v", err)
	}
	if len(limited) != 2 || limited[1].Input != "q2" {
		t.Fatalf("limited samples = %+v, want the first two rows in order", limited)
	}
}

// TestChainExporterRefusals pins the export refusals and the empty chain.
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestChainExporterRefusals(t *testing.T) {
	ctx := context.Background()
	if _, err := (&ChainExporter{}).Export(ctx, ExportOptions{}); !errors.Is(err, ErrExportReaderMissing) {
		t.Fatalf("export beside no fact reader = %v, want ErrExportReaderMissing", err)
	}
	if _, err := NewChainExporter(&chainFacts{}).Export(ctx, ExportOptions{Limit: -1}); !errors.Is(err, ErrExportLimit) {
		t.Fatalf("export beside a negative limit = %v, want ErrExportLimit", err)
	}
	empty, err := NewChainExporter(&chainFacts{}).Export(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("export an empty scope: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty chain samples = %+v, want none", empty)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := NewChainExporter(&chainFacts{}).Export(cancelled, ExportOptions{SessionIDs: []event.SessionID{"session-1"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("export on a cancelled context = %v, want context.Canceled", err)
	}
}

// TestWriteSamplesWritesJSONL pins the JSONL write helper: one sample per line, an empty set writing nothing.
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestWriteSamplesWritesJSONL(t *testing.T) {
	samples := []SFTSample{
		{SessionID: "session-1", Turn: 1, Input: "q1", Label: "a1", EpisodeID: "episode-a"},
		{SessionID: "session-2", Turn: 2, Input: "q2", Label: "a2", EpisodeID: "episode-a"},
	}
	var buffer bytes.Buffer
	if err := WriteSamples(&buffer, samples); err != nil {
		t.Fatalf("write the samples: %v", err)
	}
	lines := bytes.Split(bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), []byte("\n"))
	if len(lines) != len(samples) {
		t.Fatalf("written lines = %d, want one per sample (%d)", len(lines), len(samples))
	}
	for i, line := range lines {
		var decoded SFTSample
		if err := json.Unmarshal(line, &decoded); err != nil {
			t.Fatalf("decode written line %d: %v", i, err)
		}
		if !reflect.DeepEqual(decoded, samples[i]) {
			t.Fatalf("written line %d = %+v, want %+v", i, decoded, samples[i])
		}
	}
	var empty bytes.Buffer
	if err := WriteSamples(&empty, nil); err != nil || empty.Len() != 0 {
		t.Fatalf("empty write = (%d bytes, %v), want nothing written", empty.Len(), err)
	}
	if err := WriteSamples(nil, samples); !errors.Is(err, ErrSampleWriterMissing) {
		t.Fatalf("write beside no writer = %v, want ErrSampleWriterMissing", err)
	}
}
