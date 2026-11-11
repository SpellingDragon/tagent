package rl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// FactReader is the read-only face of the fact chain the exporter consumes: the
// facts one session committed, in the order they committed. The face carries no
// write method at all, so an export can never move the chain it reads.
//
// The assembled runtime answers this face through its own session chain read
// (Run.SessionFacts), which reports the facts as they committed rather than a
// reconstruction of them.
// The redline list (2 rules):
// MUST report the facts as they committed, in commit order.
// MUST NOT reconstruct or re-classify a fact on the way out.
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
	// Input is the text the turn's own input events presented, in the order the
	// output consumed them.
	Input string `json:"input"`
	// Label is the text the assistant output that consumed the inputs produced.
	Label string `json:"label"`
	// EpisodeID names the episode the turn belongs to; it is empty for a turn
	// whose facts name no episode boundary.
	EpisodeID string `json:"episode_id,omitempty"`
	// Inputs names the inputs of a turn that presented more than one, each beside
	// the source it came from: Input is then a rendering of several, and the row
	// keeps the per-input attribution beside it. A turn that presented one input
	// names none, because that input's text is Input itself.
	Inputs []SFTInput `json:"inputs,omitempty"`
}

// SFTInput is one input a batched turn presented, beside the source it came
// from: two inputs of different sources enter one request, so a row that carries
// both keeps each one's own origin instead of reading them as one anonymous
// block of text.
type SFTInput struct {
	// Agent is the agent of the address the input fact came from.
	Agent string `json:"agent"`
	// Session is the session of that address.
	Session string `json:"session"`
	// Text is the text the input presented, as the fact carries it.
	Text string `json:"text"`
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
// MUST pair one turn's inputs with the assistant output that consumed them, by the
// input identities that output recorded rather than by invocation or chain order.
// MUST NOT invent a sample for a turn that consumed no input or produced no assistant output.
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

// chainTurn is the pairing unit of one export: the input events one agent output
// consumed and the label it produced, on one session of one episode.
type chainTurn struct {
	episode event.EpisodeID
	session event.SessionID
	inputs  []chainInputFact
	label   []string
}

// chainInputFact is one input fact a turn may consume: the identity an output
// names it by, the source it came from and the text it presented.
type chainInputFact struct {
	key     event.EventID
	source  event.Address
	episode event.EpisodeID
	text    string
}

// Export assembles the SFT sample rows of the named sessions: it reads each
// session's chain in commit order and pairs every agent output with the inputs
// that output consumed — the input identities its fact carries in
// event.MetaKeyInputEvents — so a batch that mixed sources lands in one row while
// an input no output carried produces none and stays in the segment in progress.
// The pairing never keys on invocation identity: two inputs of different
// invocations enter one row, and one output of a third invocation answers both.
//
// An output beside no readable relation pairs positionally — every input
// committed ahead of it that no earlier output consumed — so a chain recorded
// without the relation exports as it always did rather than losing its inputs.
// A turn that consumed no input or produced no assistant output is skipped
// rather than filled with an invented side. The limit bounds the rows emitted,
// never the pairing.
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
	visited := make(map[event.SessionID]bool, len(opts.SessionIDs))
	for _, sessionID := range opts.SessionIDs {
		if sessionID == "" || visited[sessionID] {
			continue
		}
		visited[sessionID] = true
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var pending []chainInputFact
		for _, fact := range e.facts.SessionFacts(event.Address{SessionID: sessionID}) {
			switch fact.Type {
			case event.TypeExternalInput:
				if fact.Message.Content == "" {
					continue
				}
				pending = append(pending, chainInputFact{
					key:     fact.Key,
					source:  fact.Source,
					episode: fact.EpisodeID,
					text:    fact.Message.Content,
				})
			case event.TypeAgentOutput:
				consumed, held := consumedInputsOf(fact)
				taken := make([]chainInputFact, 0, len(pending))
				carried := make([]chainInputFact, 0, len(pending))
				if held {
					keyed := make(map[event.EventID]chainInputFact, len(pending))
					for _, input := range pending {
						if input.key != "" {
							keyed[input.key] = input
						}
					}
					named := make(map[event.EventID]bool, len(consumed))
					for _, key := range consumed {
						input, found := keyed[key]
						if !found || named[key] {
							continue
						}
						named[key] = true
						taken = append(taken, input)
					}
					for _, input := range pending {
						if input.key == "" || !named[input.key] {
							carried = append(carried, input)
						}
					}
				} else {
					taken, carried = pending, nil
				}
				pending = carried
				episode := fact.EpisodeID
				if len(taken) > 0 {
					episode = taken[0].episode
				}
				turns = append(turns, &chainTurn{
					episode: episode,
					session: sessionID,
					inputs:  taken,
					label:   appendText(nil, fact.Message.Content),
				})
			}
		}
	}
	samples := make([]SFTSample, 0, len(turns))
	turnNumbers := make(map[event.EpisodeID]int)
	for _, turn := range turns {
		if len(turn.inputs) == 0 || len(turn.label) == 0 {
			continue
		}
		if opts.Limit > 0 && len(samples) >= opts.Limit {
			break
		}
		turnNumbers[turn.episode]++
		samples = append(samples, SFTSample{
			SessionID: string(turn.session),
			Turn:      turnNumbers[turn.episode],
			Input:     strings.Join(turn.inputTexts(), "\n"),
			Label:     strings.Join(turn.label, "\n"),
			EpisodeID: string(turn.episode),
			Inputs:    turn.batchedInputs(),
		})
	}
	return samples, nil
}

// inputTexts reports the texts the turn's inputs presented, in the order the
// output consumed them.
func (t *chainTurn) inputTexts() []string {
	texts := make([]string, 0, len(t.inputs))
	for _, input := range t.inputs {
		texts = append(texts, input.text)
	}
	return texts
}

// batchedInputs reports the per-input attribution of a turn that presented more
// than one input, each beside the source it came from; a turn that presented one
// input keeps its text in the row's Input alone.
func (t *chainTurn) batchedInputs() []SFTInput {
	if len(t.inputs) < 2 {
		return nil
	}
	inputs := make([]SFTInput, 0, len(t.inputs))
	for _, one := range t.inputs {
		inputs = append(inputs, SFTInput{
			Agent:   string(one.source.AgentID),
			Session: string(one.source.SessionID),
			Text:    one.text,
		})
	}
	return inputs
}

// consumedInputsOf reads the consumption relation one agent output carries: the
// input fact identities it actually consumed, in the order the request presented
// them, as event.MetaKeyInputEvents records them. A relation naming no identity —
// absent, unreadable, empty after the empty and repeated identities are dropped —
// is no relation at all and answers unheld, so the caller pairs positionally
// rather than reading every input as consumed.
func consumedInputsOf(fact event.Event) ([]event.EventID, bool) {
	raw := fact.Meta[event.MetaKeyInputEvents]
	if raw == "" {
		return nil, false
	}
	var decoded []event.EventID
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, false
	}
	consumed := make([]event.EventID, 0, len(decoded))
	seen := make(map[event.EventID]bool, len(decoded))
	for _, key := range decoded {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		consumed = append(consumed, key)
	}
	if len(consumed) == 0 {
		return nil, false
	}
	return consumed, true
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

// EvidenceLines is the read-only face of the record lines one capture landed:
// the versioned lines as the capture's single writer stored them, in landing
// order. The face carries no write method, so an export never moves the chain it
// reads.
type EvidenceLines interface {
	// LandedLines reports the record lines one capture landed, in landing order.
	LandedLines() ([]Line, error)
}

// SnapshotReadBack reads the runtime request snapshot behind one reference back:
// the content returned is the snapshot that reference was fixed on. An export
// rechecks it against the reference digest, so a store that answers other content
// is refused rather than exported.
type SnapshotReadBack interface {
	// SnapshotOf reads one request snapshot back through its reference.
	SnapshotOf(ref SnapshotRef) (RequestSnapshot, error)
}

// ErrEvidenceLinesMissing and the other named errors of the evidence export: each names the way an
// export was refused before it read anything, so a caller routes it by name.
var (
	// ErrEvidenceLinesMissing reports an evidence export built beside no landed
	// evidence: a dataset is never invented for a chain nothing landed.
	ErrEvidenceLinesMissing = errors.New("rl: the evidence export was built beside no landed evidence")
	// ErrSnapshotReadBackMissing reports an evidence export built beside no
	// snapshot read-back: a request is exported from content read back through its
	// reference, never from the record alone.
	ErrSnapshotReadBackMissing = errors.New("rl: the evidence export was built beside no snapshot read-back")
	// ErrEvidenceMissing reports a capture directory holding no record file: an
	// export names the missing evidence rather than reporting an empty dataset as
	// a complete one.
	ErrEvidenceMissing = errors.New("rl: the landed request evidence is missing")
)

// RefusalName names the way one row of an export was refused, so a caller routes
// the refusal instead of counting it as a row.
type RefusalName string

const (
	// RefusalMissingEvidence marks a row whose request evidence or attempt was not
	// landed, or whose snapshot cannot be read back.
	RefusalMissingEvidence RefusalName = "missing_evidence"
	// RefusalDigestMismatch marks a row whose read-back content does not recheck
	// against the digest of its reference.
	RefusalDigestMismatch RefusalName = "digest_mismatch"
	// RefusalRedacted marks a row whose read-back request carries a projected
	// credential value where request content stood: the projected value is not the
	// value the request held.
	RefusalRedacted RefusalName = "redacted_not_exact"
	// RefusalDuplicateEvidence marks a second evidence line of one decision: one
	// request is evidenced once.
	RefusalDuplicateEvidence RefusalName = "duplicate_evidence"
	// RefusalTerminalUnknown marks a row whose attempt reached no determined
	// terminal: an unknown terminal is never exported as a target.
	RefusalTerminalUnknown RefusalName = "unknown_terminal"
	// RefusalTerminalNotCompleted marks a row whose attempt ended in a terminal
	// other than completed, which is no completion to train on.
	RefusalTerminalNotCompleted RefusalName = "terminal_not_completed"
	// RefusalUnsupportedMedia marks a row whose prompt carries a media part the
	// row cannot carry: the dataset holds the reference the request carried, not
	// the media bytes behind it.
	RefusalUnsupportedMedia RefusalName = "unsupported_media"
	// RefusalEmptyTarget marks a row whose completion carries neither text nor a
	// tool call: an empty target is refused rather than written as a blank label.
	RefusalEmptyTarget RefusalName = "empty_target"
	// RefusalDifferentPrefix marks attempts of one task whose prompts differ: two
	// different prompts are never paired as one preference.
	RefusalDifferentPrefix RefusalName = "different_prefix"
	// RefusalMissingPreference marks attempts one task holds without the verdict
	// pair a preference needs, or with a verdict carrying no rule version.
	RefusalMissingPreference RefusalName = "missing_preference"
)

// verdictPass and verdictFail are the verdict spellings an annex carries, as the
// sample gate writes them. A verdict outside them is no preference.
const (
	verdictPass = "pass"
	verdictFail = "fail"
)

// DatasetRefusal is one row an export refused, named so an operator routes it
// rather than reading the row count as complete.
type DatasetRefusal struct {
	// DecisionID names the decision the refused row belonged to.
	DecisionID string `json:"decision_id,omitempty"`
	// AttemptID names the attempt the refused row belonged to.
	AttemptID string `json:"attempt_id,omitempty"`
	// TaskID names the bench task of a refused preference pair.
	TaskID string `json:"task_id,omitempty"`
	// Name is the closed refusal name the row was refused under.
	Name RefusalName `json:"name"`
	// Reason states why the row was refused.
	Reason string `json:"reason"`
}

// SFTRow is one supervised fine-tuning row assembled from landed evidence: the
// prompt the request presented, the completion the attempt observed and the tool
// declarations the request ran with.
//
// The completion is carried as the assistant message the attempt observed, so a
// target that is only a tool call is a target and never an empty label.
type SFTRow struct {
	// DecisionID names the decision whose request the prompt is.
	DecisionID string `json:"decision_id"`
	// AttemptID names the attempt whose response the completion is.
	AttemptID string `json:"attempt_id"`
	// Prompt is the conversation the request presented, as the snapshot holds it.
	Prompt []model.Message `json:"prompt"`
	// Completion is the assistant message the attempt observed.
	Completion model.Message `json:"completion"`
	// Tools holds the declaration of every tool visible to the request.
	Tools []json.RawMessage `json:"tools,omitempty"`
}

// SFTDataset is the SFT export of one evidence chain: the rows it assembled and
// the rows it refused by name.
type SFTDataset struct {
	// Rows are the assembled rows, in landing order.
	Rows []SFTRow `json:"rows"`
	// Refused lists every row the export refused beside the reason it refused it.
	Refused []DatasetRefusal `json:"refused"`
}

// PreferenceRow is one preference pair assembled from landed evidence: the
// prompt both attempts ran on, the completion the passing verdict chose, the
// completion the failing verdict rejected, and the source and version of that
// preference.
type PreferenceRow struct {
	// TaskID names the bench task both attempts ran.
	TaskID string `json:"task_id"`
	// Prompt is the conversation both attempts presented.
	Prompt []model.Message `json:"prompt"`
	// Tools holds the declaration of every tool visible to both attempts.
	Tools []json.RawMessage `json:"tools,omitempty"`
	// Chosen is the completion the passing verdict chose.
	Chosen model.Message `json:"chosen"`
	// Rejected is the completion the failing verdict rejected.
	Rejected model.Message `json:"rejected"`
	// Source names where the preference came from.
	Source string `json:"preference_source"`
	// Version names the version of the derivation rules the preference was
	// reached under.
	Version string `json:"preference_version"`
}

// PreferenceDataset is the preference export of one evidence chain: the pairs it
// assembled and the rows it refused by name.
type PreferenceDataset struct {
	// Rows are the assembled pairs, in landing order.
	Rows []PreferenceRow `json:"rows"`
	// Refused lists every row the export refused beside the reason it refused it.
	Refused []DatasetRefusal `json:"refused"`
}

// EvidenceOptions bounds the rows one evidence export emits.
type EvidenceOptions struct {
	// Limit bounds the rows written; zero leaves the export unbounded and a
	// negative bound is refused.
	Limit int
}

// EvidenceExporter assembles training rows from the request evidence a capture
// landed: each decision's runtime request snapshot read back through its
// reference and rechecked against the digest, beside the attempt the capture
// observed for that decision.
//
// The exporter reads and never writes: it derives no request, no completion and
// no verdict, and a row it cannot assemble from what landed is refused by name
// with its reason rather than filled or skipped in silence.
type EvidenceExporter struct {
	lines EvidenceLines
	snaps SnapshotReadBack
}

// NewEvidenceExporter returns the exporter reading the landed lines through
// lines and the request content through snaps.
func NewEvidenceExporter(lines EvidenceLines, snaps SnapshotReadBack) (*EvidenceExporter, error) {
	if lines == nil {
		return nil, ErrEvidenceLinesMissing
	}
	if snaps == nil {
		return nil, ErrSnapshotReadBackMissing
	}
	return &EvidenceExporter{lines: lines, snaps: snaps}, nil
}

// landedEvidence is one decision's landed evidence: the record that names the
// request, the snapshot read back through the reference it carries, and the
// attempt the capture observed for that decision.
type landedEvidence struct {
	evidence RequestEvidenceRecord
	snapshot RequestSnapshot
	attempt  AttemptRecord
	held     bool
}

// evidenceRead is what one read of the landed chain answered: the evidence of
// each decision, the annexes that judged attempts, and the rows refused while
// reading.
type evidenceRead struct {
	entries  []landedEvidence
	annexes  []AnnexRecord
	refusals []DatasetRefusal
}

// readChain reads the landed chain once: every request evidence line is read
// back through the snapshot it names, every attempt is joined to the decision it
// was observed for, and every annex is kept for the preference export.
func (e *EvidenceExporter) readChain(ctx context.Context) (evidenceRead, error) {
	var read evidenceRead
	if e == nil || e.lines == nil {
		return read, ErrEvidenceLinesMissing
	}
	if e.snaps == nil {
		return read, ErrSnapshotReadBackMissing
	}
	if err := ctx.Err(); err != nil {
		return read, err
	}
	lines, err := e.lines.LandedLines()
	if err != nil {
		return read, err
	}
	attempts := map[event.DecisionID]AttemptRecord{}
	seen := map[event.DecisionID]bool{}
	for _, line := range lines {
		if err := ctx.Err(); err != nil {
			return read, err
		}
		switch line.Kind {
		case RecordKindAttempt:
			attempt := line.Attempt
			if attempt == nil {
				continue
			}
			if _, held := attempts[attempt.DecisionID]; !held {
				attempts[attempt.DecisionID] = *attempt
			}
		case RecordKindAnnex:
			if annex := line.Annex(); annex != nil {
				read.annexes = append(read.annexes, *annex)
			}
		case RecordKindRequest:
			entry, refusal := e.readEvidence(*line.RequestEvidence(), seen)
			if refusal != nil {
				read.refusals = append(read.refusals, *refusal)
				continue
			}
			read.entries = append(read.entries, entry)
		}
	}
	for i := range read.entries {
		attempt, held := attempts[read.entries[i].evidence.DecisionID]
		read.entries[i].attempt = attempt
		read.entries[i].held = held
	}
	return read, nil
}

// readEvidence reads one request evidence record back: the snapshot is read
// through the reference the record carries, rechecked against the digest, and
// checked for the exact request content. A record whose content was already
// evidenced, cannot be read back, does not match its digest or carries a
// projected value in place of request content is refused by name.
func (e *EvidenceExporter) readEvidence(record RequestEvidenceRecord, seen map[event.DecisionID]bool) (landedEvidence, *DatasetRefusal) {
	entry := landedEvidence{evidence: record}
	if seen[record.DecisionID] {
		return entry, refusalFor(record.DecisionID, "", RefusalDuplicateEvidence,
			fmt.Sprintf("decision %q already carries request evidence", record.DecisionID))
	}
	seen[record.DecisionID] = true
	snapshot, err := e.snaps.SnapshotOf(record.Snapshot)
	if err != nil {
		name := RefusalMissingEvidence
		if errors.Is(err, ErrSnapshotMismatch) {
			name = RefusalDigestMismatch
		}
		return entry, refusalFor(record.DecisionID, "", name, fmt.Sprintf("read the request snapshot back: %v", err))
	}
	content, err := MarshalSnapshot(snapshot)
	if err != nil {
		return entry, refusalFor(record.DecisionID, "", RefusalMissingEvidence, fmt.Sprintf("encode the read-back snapshot: %v", err))
	}
	if err := VerifySnapshot(record.Snapshot, content); err != nil {
		return entry, refusalFor(record.DecisionID, "", RefusalDigestMismatch, fmt.Sprintf("recheck the read-back snapshot: %v", err))
	}
	if redactedField(snapshot) != "" {
		return entry, refusalFor(record.DecisionID, "", RefusalRedacted,
			fmt.Sprintf("the read-back request carries %s where the request content stood", redactedField(snapshot)))
	}
	entry.snapshot = snapshot
	return entry, nil
}

// SFT assembles the supervised fine-tuning dataset of the landed evidence.
//
// A row is one landed request beside the attempt observed for its decision: the
// prompt and the tool declarations come from the request read back through its
// reference, and the completion is the assistant message that attempt observed.
// A target carrying only tool calls is a target, an unknown terminal is never a
// target, and a row the evidence cannot support is refused by name with its
// reason rather than written with an invented side.
func (e *EvidenceExporter) SFT(ctx context.Context, opts EvidenceOptions) (SFTDataset, error) {
	dataset := SFTDataset{Rows: make([]SFTRow, 0), Refused: make([]DatasetRefusal, 0)}
	if opts.Limit < 0 {
		return dataset, ErrExportLimit
	}
	read, err := e.readChain(ctx)
	if err != nil {
		return dataset, err
	}
	dataset.Refused = append(dataset.Refused, read.refusals...)
	for _, entry := range read.entries {
		if opts.Limit > 0 && len(dataset.Rows) >= opts.Limit {
			break
		}
		completion, refusal := targetOf(entry)
		if refusal == nil {
			if media := mediaOf(entry.snapshot.Messages); media != "" {
				refusal = refusalFor(entry.evidence.DecisionID, attemptIDOf(entry), RefusalUnsupportedMedia,
					fmt.Sprintf("the prompt carries a %s part the row cannot carry", media))
			}
		}
		if refusal != nil {
			dataset.Refused = append(dataset.Refused, *refusal)
			continue
		}
		dataset.Rows = append(dataset.Rows, SFTRow{
			DecisionID: string(entry.evidence.DecisionID),
			AttemptID:  string(entry.attempt.ID),
			Prompt:     entry.snapshot.Messages,
			Completion: completion,
			Tools:      entry.snapshot.ToolSchemas,
		})
	}
	return dataset, nil
}

// DPO assembles the preference dataset of the landed evidence.
//
// A pair joins two attempts of one bench task whose prompts and tool
// declarations are the same, one judged a pass and one a fail by the annex that
// carries the verdict and the version of the rules it was reached under: the
// passing completion is the chosen side and the failing completion the rejected
// one. Attempts of one task whose prompts differ are never paired, an attempt
// the annex never judged carries no preference, and a verdict without the rule
// version it was reached under is no exported preference.
func (e *EvidenceExporter) DPO(ctx context.Context, opts EvidenceOptions) (PreferenceDataset, error) {
	dataset := PreferenceDataset{Rows: make([]PreferenceRow, 0), Refused: make([]DatasetRefusal, 0)}
	if opts.Limit < 0 {
		return dataset, ErrExportLimit
	}
	read, err := e.readChain(ctx)
	if err != nil {
		return dataset, err
	}
	dataset.Refused = append(dataset.Refused, read.refusals...)
	verdicts := map[event.AttemptID]AnnexRecord{}
	for _, annex := range read.annexes {
		if _, held := verdicts[annex.AttemptID]; !held {
			verdicts[annex.AttemptID] = annex
		}
	}
	for _, task := range pairByTask(read.entries, verdicts, &dataset) {
		if opts.Limit > 0 && len(dataset.Rows) >= opts.Limit {
			break
		}
		dataset.Rows = append(dataset.Rows, task)
	}
	return dataset, nil
}

// preferenceCandidate is one judged attempt of a bench task: the landed evidence,
// the completion it carries and the verdict the annex reached on it.
type preferenceCandidate struct {
	entry      landedEvidence
	completion model.Message
	verdict    string
}

// pairByTask groups the landed evidence of one task and appends one row per
// prompt prefix that holds both a passing and a failing verdict, refusing the
// task by name when its prompts differ or a side is missing.
func pairByTask(entries []landedEvidence, verdicts map[event.AttemptID]AnnexRecord, dataset *PreferenceDataset) []PreferenceRow {
	taskOrder := make([]string, 0)
	groups := map[string]map[string][]preferenceCandidate{}
	for _, entry := range entries {
		annex, judged := verdicts[entry.attempt.ID]
		if !entry.held || !judged || annex.TaskID == "" {
			dataset.Refused = append(dataset.Refused, *refusalFor(entry.evidence.DecisionID, attemptIDOf(entry), RefusalMissingPreference,
				"the attempt carries no annex verdict binding it to a bench task"))
			continue
		}
		if annex.Verdict != verdictPass && annex.Verdict != verdictFail {
			dataset.Refused = append(dataset.Refused, *refusalFor(entry.evidence.DecisionID, attemptIDOf(entry), RefusalMissingPreference,
				fmt.Sprintf("the annex verdict %q is no preference", annex.Verdict)))
			continue
		}
		if annex.RuleVersion == "" {
			dataset.Refused = append(dataset.Refused, *refusalFor(entry.evidence.DecisionID, attemptIDOf(entry), RefusalMissingPreference,
				"the annex verdict carries no rule version the preference was reached under"))
			continue
		}
		completion, refusal := targetOf(entry)
		if refusal == nil {
			if media := mediaOf(entry.snapshot.Messages); media != "" {
				refusal = refusalFor(entry.evidence.DecisionID, attemptIDOf(entry), RefusalUnsupportedMedia,
					fmt.Sprintf("the prompt carries a %s part the pair cannot carry", media))
			}
		}
		if refusal != nil {
			dataset.Refused = append(dataset.Refused, *refusal)
			continue
		}
		task := annex.TaskID
		if _, held := groups[task]; !held {
			groups[task] = map[string][]preferenceCandidate{}
			taskOrder = append(taskOrder, task)
		}
		prefix := prefixKey(entry.snapshot)
		groups[task][prefix] = append(groups[task][prefix], preferenceCandidate{entry: entry, completion: completion, verdict: annex.Verdict})
	}
	rows := make([]PreferenceRow, 0)
	for _, task := range taskOrder {
		byPrefix := groups[task]
		if len(byPrefix) > 1 {
			dataset.Refused = append(dataset.Refused, DatasetRefusal{
				TaskID: task,
				Name:   RefusalDifferentPrefix,
				Reason: fmt.Sprintf("task %q holds %d different prompt prefixes, so its attempts are never paired across them; the first is decision %q", task, len(byPrefix), firstDecision(byPrefix)),
			})
		}
		for _, prefix := range prefixOrder(byPrefix) {
			candidates := byPrefix[prefix]
			var chosen, rejected *preferenceCandidate
			for i := range candidates {
				switch candidates[i].verdict {
				case verdictPass:
					if chosen == nil {
						chosen = &candidates[i]
					}
				case verdictFail:
					if rejected == nil {
						rejected = &candidates[i]
					}
				}
			}
			if chosen == nil || rejected == nil {
				dataset.Refused = append(dataset.Refused, DatasetRefusal{
					TaskID:     task,
					DecisionID: string(candidates[0].entry.evidence.DecisionID),
					AttemptID:  attemptIDOf(candidates[0].entry),
					Name:       RefusalMissingPreference,
					Reason:     missingSideReason(chosen, rejected),
				})
				continue
			}
			rows = append(rows, PreferenceRow{
				TaskID:   task,
				Prompt:   chosen.entry.snapshot.Messages,
				Tools:    chosen.entry.snapshot.ToolSchemas,
				Chosen:   chosen.completion,
				Rejected: rejected.completion,
				Source:   "annex_verdict",
				Version:  verdicts[chosen.entry.attempt.ID].RuleVersion,
			})
		}
	}
	return rows
}

// missingSideReason states the side one prompt prefix holds no completion for.
func missingSideReason(chosen, rejected *preferenceCandidate) string {
	switch {
	case chosen == nil && rejected == nil:
		return "the prompt prefix holds no judged attempt"
	case chosen == nil:
		return "the prompt prefix holds no completion a passing verdict chose"
	default:
		return "the prompt prefix holds no completion a failing verdict rejected"
	}
}

// prefixKey names the prompt a pair shares: the messages and tool declarations
// both attempts presented, digested so two attempts of one prompt compare by one
// value.
func prefixKey(snapshot RequestSnapshot) string {
	content, err := json.Marshal(struct {
		Messages []model.Message   `json:"messages"`
		Tools    []json.RawMessage `json:"tools,omitempty"`
	}{Messages: snapshot.Messages, Tools: snapshot.ToolSchemas})
	if err != nil {
		return ""
	}
	return digestOf(content)
}

// prefixOrder reports the prompt prefixes of one task in a stable order, so an
// export of the same chain answers the same rows in the same order.
func prefixOrder(byPrefix map[string][]preferenceCandidate) []string {
	order := make([]string, 0, len(byPrefix))
	for prefix := range byPrefix {
		order = append(order, prefix)
	}
	sort.Strings(order)
	return order
}

// firstDecision reports the decision of one task's first attempt, so a refusal
// names where to look.
func firstDecision(byPrefix map[string][]preferenceCandidate) string {
	for _, candidates := range byPrefix {
		if len(candidates) > 0 {
			return string(candidates[0].entry.evidence.DecisionID)
		}
	}
	return ""
}

// targetOf reports the completion one landed evidence entry carries: the
// assistant message the attempt observed, refused by name when the evidence names
// no attempt, the terminal is unknown or not completed, or the message carries
// neither text nor a tool call.
func targetOf(entry landedEvidence) (model.Message, *DatasetRefusal) {
	if !entry.held {
		return model.Message{}, refusalFor(entry.evidence.DecisionID, "", RefusalMissingEvidence,
			"no attempt was landed for the decision")
	}
	switch entry.attempt.Terminal {
	case model.KindUnknown:
		return model.Message{}, refusalFor(entry.evidence.DecisionID, attemptIDOf(entry), RefusalTerminalUnknown,
			"the attempt reached no determined terminal, so it carries no target")
	case model.KindCompleted:
	default:
		return model.Message{}, refusalFor(entry.evidence.DecisionID, attemptIDOf(entry), RefusalTerminalNotCompleted,
			fmt.Sprintf("the attempt ended %q, which is no completion to train on", entry.attempt.Terminal))
	}
	message, held := assistantMessageOf(entry.attempt)
	if !held {
		return model.Message{}, refusalFor(entry.evidence.DecisionID, attemptIDOf(entry), RefusalEmptyTarget,
			"the attempt observed no assistant message: its response carries no choice")
	}
	if message.Content == "" && len(message.ToolCalls) == 0 && message.ReasoningContent == "" {
		return model.Message{}, refusalFor(entry.evidence.DecisionID, attemptIDOf(entry), RefusalEmptyTarget,
			"the completion carries neither text nor a tool call, so the row would be a blank label")
	}
	return message, nil
}

// assistantMessageOf reports the assistant message one attempt observed, and
// whether it observed one at all.
func assistantMessageOf(attempt AttemptRecord) (model.Message, bool) {
	if len(attempt.Response.Choices) == 0 {
		return model.Message{}, false
	}
	message := attempt.Response.Choices[0].Message
	if message.Role == "" {
		message.Role = model.RoleAssistant
	}
	return message, true
}

// mediaOf reports the media kind one prompt carries, or an empty string when it
// carries none: a row holds the reference a media part points at, not the media
// behind it.
func mediaOf(messages []model.Message) string {
	for _, message := range messages {
		for _, part := range message.ContentParts {
			switch {
			case part.Image != nil:
				return "image"
			case part.Audio != nil:
				return "audio"
			case part.Video != nil:
				return "video"
			case part.File != nil:
				return "file"
			}
		}
	}
	return ""
}

// redactedField reports the field of one read-back request whose content lost its
// value to the redaction projection, or an empty string when the read-back
// content is the exact request. Header values are excluded: they are transport
// carriers the projection always replaces, and they are no request content.
func redactedField(snapshot RequestSnapshot) string {
	content := snapshot
	content.Headers = nil
	encoded, err := json.Marshal(content)
	if err != nil {
		return "content"
	}
	if bytes.Contains(encoded, []byte(RedactedValue)) {
		return "request content"
	}
	return ""
}

// attemptIDOf reports the identity of the attempt one entry was joined with, or
// an empty string when no attempt was landed.
func attemptIDOf(entry landedEvidence) string {
	if !entry.held {
		return ""
	}
	return string(entry.attempt.ID)
}

// refusalFor builds the refusal of one row under the given name.
func refusalFor(decision event.DecisionID, attemptID string, name RefusalName, reason string) *DatasetRefusal {
	return &DatasetRefusal{DecisionID: string(decision), AttemptID: attemptID, Name: name, Reason: reason}
}

// digestOf reports the hex SHA-256 of one body.
func digestOf(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// FileEvidenceLines is the record lines one capture directory holds: the lines
// the capture's single writer appended, parsed through the versioned line parser.
// A directory without the record file is reported as missing evidence rather than
// as an empty chain.
type FileEvidenceLines struct {
	dir string
}

// OpenFileEvidenceLines returns the landed lines of one capture directory.
func OpenFileEvidenceLines(dir string) (*FileEvidenceLines, error) {
	if dir == "" {
		return nil, fmt.Errorf("%w: no capture directory was named", ErrEvidenceMissing)
	}
	return &FileEvidenceLines{dir: dir}, nil
}

// LandedLines reports the record lines the capture landed, in landing order. A
// directory holding no record file is refused as missing evidence, and a line
// that does not parse as a versioned record is refused rather than skipped: an
// export reads the chain it was given or reports what it could not read.
func (f *FileEvidenceLines) LandedLines() ([]Line, error) {
	if f == nil {
		return nil, ErrEvidenceLinesMissing
	}
	path := filepath.Join(f.dir, SinkRecordsFile)
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrEvidenceMissing, path)
	}
	if err != nil {
		return nil, fmt.Errorf("rl: read the landed evidence %s: %w", path, err)
	}
	lines := make([]Line, 0)
	for at, raw := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		line, err := ParseLine([]byte(raw))
		if err != nil {
			return nil, fmt.Errorf("rl: parse the landed line %d of %s: %w", at+1, path, err)
		}
		lines = append(lines, line)
	}
	return lines, nil
}
