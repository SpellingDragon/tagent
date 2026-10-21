package rl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
)

// FactReader is the read-only face of the fact chain the exporter consumes: the
// facts one session committed, in the order they committed. The face carries no
// write method at all, so an export can never move the chain it reads.
//
// The assembled runtime answers this face through its own session chain read
// (Run.SessionFacts), which reports the facts as they committed rather than a
// reconstruction of them.
type FactReader interface {
	// SessionFacts reports the facts one session committed, in commit order.
	SessionFacts(addr event.Address) []event.Event
}

// ErrExportReaderMissing and the other named errors of the chain export: each
// names the way an export was refused, so a caller routes it by name.
var (
	// ErrExportReaderMissing reports an exporter built beside no fact reader: a
	// chain is never invented for an export.
	ErrExportReaderMissing = errors.New("rl: the chain exporter was built beside no fact reader")
	// ErrExportLimit reports a negative sample limit: a negative bound is
	// refused rather than read as unlimited.
	ErrExportLimit = errors.New("rl: the chain export limit is negative")
	// ErrSampleWriterMissing reports a sample write offered to no writer.
	ErrSampleWriterMissing = errors.New("rl: the SFT sample writer is missing")
)

// ExportOptions narrows one export to the sessions it reads and the number of
// rows it emits.
//
// The session list is the export's whole read scope: an empty list exports
// nothing rather than scanning every session the reader holds, and a repeated
// session is read once.
type ExportOptions struct {
	// SessionIDs names the sessions whose chains are read, in the order the
	// export visits them. An empty list exports no sample.
	SessionIDs []event.SessionID
	// Limit bounds the number of sample rows written; zero leaves the export
	// unbounded and a negative bound is refused.
	Limit int
}

// SFTSample is one supervised fine-tuning row assembled from the chain: the
// input one turn presented, the label the assistant produced for it, and the
// episode and turn the row belongs to.
//
// A row is one turn of one episode: the turn number starts at one inside each
// episode, and the row names the session whose chain carried the input.
type SFTSample struct {
	// SessionID names the session the turn's input committed on.
	SessionID string `json:"session_id"`
	// Turn is the one-based turn number inside the episode.
	Turn int `json:"turn"`
	// Input is the text the turn's own input events presented.
	Input string `json:"input"`
	// Label is the text the assistant output of the same invocation produced.
	Label string `json:"label"`
	// EpisodeID names the episode the turn belongs to; it is empty for a turn
	// whose facts name no episode boundary.
	EpisodeID string `json:"episode_id,omitempty"`
}

// 契约: docs/wiki/rl/rl-architecture.md#training-export
//
// ChainExporter assembles SFT sample rows from the fact chain of sessions:
// one row per turn — the input the request presented (projection entry texts)
// and the label the assistant produced. It reads the chain, derives nothing,
// and never touches the production path.
//
// The redline list (3 rules):
// MUST read the chain through the read-only fact face and write nothing back.
// MUST pair one turn's input with the assistant output of the same invocation.
// MUST NOT invent a sample for a turn that presented no input or produced no assistant output.
// ChainExporter assembles SFT sample rows from the fact chain of sessions. It
// holds the read-only fact face and keeps no state of its own, so one exporter
// may export the same chain again and again without moving it.
type ChainExporter struct {
	facts FactReader
}

// NewChainExporter returns the exporter reading the chain through facts.
func NewChainExporter(facts FactReader) *ChainExporter {
	return &ChainExporter{facts: facts}
}

// chainTurn is the pairing unit of one export: the input events and the
// assistant output events one invocation presented and produced on one session
// of one episode.
type chainTurn struct {
	episode event.EpisodeID
	session event.SessionID
	input   []string
	label   []string
}

// turnIdentity is the identity one turn is paired on: the episode, the
// invocation and the session whose chain carried it. A fact carrying no
// invocation names no turn boundary and is never paired into one.
type turnIdentity struct {
	episode    event.EpisodeID
	invocation event.InvocationID
	session    event.SessionID
}

// Export assembles the SFT sample rows of the named sessions: it reads each
// session's chain in commit order, groups the facts by episode, pairs the input
// events with the assistant output events of the same invocation, and emits one
// row per paired turn, in the order the turns first appeared.
//
// A turn that presented no input or produced no assistant output is skipped
// rather than filled with an invented side, and a fact carrying no invocation
// is not paired at all. The limit bounds the rows emitted, never the pairing.
func (e *ChainExporter) Export(ctx context.Context, opts ExportOptions) ([]SFTSample, error) {
	if e == nil || e.facts == nil {
		return nil, ErrExportReaderMissing
	}
	if opts.Limit < 0 {
		return nil, ErrExportLimit
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	turns := make([]*chainTurn, 0)
	byIdentity := make(map[turnIdentity]*chainTurn)
	visited := make(map[event.SessionID]bool, len(opts.SessionIDs))
	for _, sessionID := range opts.SessionIDs {
		if sessionID == "" || visited[sessionID] {
			continue
		}
		visited[sessionID] = true
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, fact := range e.facts.SessionFacts(event.Address{SessionID: sessionID}) {
			if fact.InvocationID == "" {
				continue
			}
			identity := turnIdentity{episode: fact.EpisodeID, invocation: fact.InvocationID, session: sessionID}
			turn, held := byIdentity[identity]
			if !held {
				turn = &chainTurn{episode: fact.EpisodeID, session: sessionID}
				byIdentity[identity] = turn
				turns = append(turns, turn)
			}
			switch fact.Type {
			case event.TypeExternalInput:
				turn.input = appendText(turn.input, fact.Message.Content)
			case event.TypeAgentOutput:
				turn.label = appendText(turn.label, fact.Message.Content)
			}
		}
	}
	samples := make([]SFTSample, 0, len(turns))
	turnNumbers := make(map[event.EpisodeID]int)
	for _, turn := range turns {
		if len(turn.input) == 0 || len(turn.label) == 0 {
			continue
		}
		if opts.Limit > 0 && len(samples) >= opts.Limit {
			break
		}
		turnNumbers[turn.episode]++
		samples = append(samples, SFTSample{
			SessionID: string(turn.session),
			Turn:      turnNumbers[turn.episode],
			Input:     strings.Join(turn.input, "\n"),
			Label:     strings.Join(turn.label, "\n"),
			EpisodeID: string(turn.episode),
		})
	}
	return samples, nil
}

// appendText appends one non-empty text to the side of a turn; an empty content
// contributes nothing and never stands as a side of its own.
func appendText(texts []string, content string) []string {
	if content == "" {
		return texts
	}
	return append(texts, content)
}

// WriteSamples writes the sample rows as JSONL: one encoded sample per line, in
// the order they were assembled. An empty sample set writes nothing, and a
// write that fails is reported by name rather than counted as written.
func WriteSamples(w io.Writer, samples []SFTSample) error {
	if w == nil {
		return ErrSampleWriterMissing
	}
	for i, sample := range samples {
		line, err := json.Marshal(sample)
		if err != nil {
			return fmt.Errorf("rl: encode SFT sample %d: %w", i, err)
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("rl: write SFT sample %d: %w", i, err)
		}
	}
	return nil
}
