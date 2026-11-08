package learning

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/rl"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// openEvidenceCapture opens one real capture over a temporary directory: the file
// writer and the bounded sink the assembled host opens for its trajectory, so
// the lines an export reads are the bytes a real writer stored.
func openEvidenceCapture(t *testing.T) (*rl.TrajectoryCapture, string) {
	t.Helper()
	dir := t.TempDir()
	writer, err := rl.NewFileCaptureWriter(dir, 0)
	if err != nil {
		t.Fatalf("open the trajectory writer: %v", err)
	}
	sink, err := rl.NewCaptureSink("cap:evidence-export", rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("open the trajectory sink: %v", err)
	}
	capture, err := rl.NewTrajectoryCapture("cap:evidence-export", sink, rl.SnapshotPolicy{})
	if err != nil {
		t.Fatalf("open the trajectory capture: %v", err)
	}
	return capture, dir
}

// driveEvidenceRun assembles one runtime through the sole assembly entry beside the
// capture it is given and drives one invocation through the formal invoke entry.
func driveEvidenceRun(t *testing.T, ctx context.Context, capture rl.Capture, answer string) *assembly.Run {
	t.Helper()
	provider := &learnModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return learnFinalTurn(answer)
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{&learnTool{name: "echo-a", output: "out-a"}}},
	}, assembly.WithGovernance(formalPolicy{}), assembly.WithCapture(capture))
	if err != nil {
		t.Fatalf("assemble the runtime beside the capture: %v", err)
	}
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-evidence",
		Episode:      "ep-evidence",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-evidence"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "export me"}},
	}); err != nil {
		t.Fatalf("invoke the formal runtime: %v", err)
	}
	return run
}

// scriptedEvidence is one landed chain a refusal check scripts: the record lines
// in the order they landed and the snapshots that answer their references.
type scriptedEvidence struct {
	lines []rl.Line
	snaps map[string]rl.RequestSnapshot
	errs  map[string]error
}

// LandedLines reports the scripted record lines.
func (s *scriptedEvidence) LandedLines() ([]rl.Line, error) {
	return s.lines, nil
}

// SnapshotOf answers one scripted read-back: a refusal the script names, the
// snapshot it holds, or the missing-snapshot refusal a store without the content
// answers.
func (s *scriptedEvidence) SnapshotOf(ref rl.SnapshotRef) (rl.RequestSnapshot, error) {
	if err, refused := s.errs[ref.SnapshotID]; refused {
		return rl.RequestSnapshot{}, err
	}
	snapshot, held := s.snaps[ref.SnapshotID]
	if !held {
		return rl.RequestSnapshot{}, rl.ErrCaptureSnapshotMissing
	}
	return snapshot, nil
}

// evidenceRequest is one scripted request evidence record beside the line that
// lands it: the reference digests the bytes its snapshot encodes, so the export
// rechecks exactly what the store answers.
func evidenceRequest(t *testing.T, decision event.DecisionID, snapshot rl.RequestSnapshot) (rl.Line, rl.RequestEvidenceRecord) {
	t.Helper()
	ref, err := snapshot.Ref("snapshot-"+string(decision), "capture/evidence/"+string(decision))
	if err != nil {
		t.Fatalf("reference the snapshot of %s: %v", decision, err)
	}
	record := rl.RequestEvidenceRecord{DecisionID: decision, EpisodeID: "ep-evidence", Snapshot: ref}
	return rl.NewRequestEvidenceLine(record), record
}

// evidenceAttempt is one scripted attempt record of the given decision, ended in
// the given terminal beside the assistant message it observed.
func evidenceAttempt(decision event.DecisionID, attempt event.AttemptID, terminal model.Kind, message model.Message) rl.Line {
	return rl.NewAttemptLine(rl.AttemptRecord{
		Schema:     rl.SchemaCurrent,
		ID:         attempt,
		DecisionID: decision,
		EpisodeID:  "ep-evidence",
		Response:   model.Response{Choices: []model.Choice{{Message: message}}},
		Terminal:   terminal,
	})
}

// evidenceAnnex is one scripted acceptance annex binding an attempt to a bench
// task under the verdict and the rule version the preference rests on.
func evidenceAnnex(attempt event.AttemptID, task, verdict, ruleVersion string) rl.Line {
	return rl.NewAnnexLine(rl.AnnexRecord{AttemptID: attempt, TaskID: task, Verdict: verdict, RuleVersion: ruleVersion})
}

// landedSnapshot is one scripted runtime request snapshot: the prompt the
// request presented beside the tool declarations it ran with.
func landedSnapshot(messages []model.Message, schemas ...string) rl.RequestSnapshot {
	tools := make([]json.RawMessage, 0, len(schemas))
	for _, schema := range schemas {
		tools = append(tools, json.RawMessage(schema))
	}
	return rl.RequestSnapshot{
		Model:       "model-a",
		Parameters:  json.RawMessage(`{"temperature":0.3}`),
		Messages:    messages,
		ToolSchemas: tools,
	}
}

// refusalCounts reports the refusal names one export answered.
func refusalCounts(refused []rl.DatasetRefusal) map[rl.RefusalName]int {
	names := map[rl.RefusalName]int{}
	for _, refusal := range refused {
		names[refusal.Name]++
	}
	return names
}

// TestExportReadsDurableEvidence pins the export over the request evidence the formal runtime actually landed.
//   - the assembled runtime fixes a request snapshot and lands its evidence as a request line beside the attempt line in the capture's record file
//   - the export reads the landed file and assembles a row whose prompt and completion are read back through the evidence, not reconstructed
//   - missing_evidence_rejected: evidence whose snapshot cannot be read back is refused by name
//   - redacted_not_exact: a read-back request carrying a projected credential value is refused by name
//   - digest_mismatch_rejected: read-back content that does not recheck against its digest is refused by name
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestExportReadsDurableEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	capture, dir := openEvidenceCapture(t)
	driveEvidenceRun(t, ctx, capture, "landed answer")

	landed, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
	if err != nil {
		t.Fatalf("read the landed record file: %v", err)
	}
	kinds := map[rl.RecordKind]int{}
	for _, raw := range recordLinesOf(string(landed)) {
		line, err := rl.ParseLine([]byte(raw))
		if err != nil {
			t.Fatalf("parse the landed line: %v", err)
		}
		kinds[line.Kind]++
	}
	if kinds[rl.RecordKindRequest] != 1 || kinds[rl.RecordKindAttempt] != 1 {
		t.Fatalf("landed kinds = %v, want one request evidence line beside one attempt line", kinds)
	}

	lines, err := rl.OpenFileEvidenceLines(dir)
	if err != nil {
		t.Fatalf("open the landed evidence: %v", err)
	}
	exporter, err := rl.NewEvidenceExporter(lines, capture)
	if err != nil {
		t.Fatalf("open the evidence exporter: %v", err)
	}
	dataset, err := exporter.SFT(ctx, rl.EvidenceOptions{})
	if err != nil {
		t.Fatalf("export the landed evidence: %v", err)
	}
	if len(dataset.Refused) != 0 {
		t.Fatalf("refused = %+v, want no refusal over the evidence the runtime landed", dataset.Refused)
	}
	if len(dataset.Rows) != 1 {
		t.Fatalf("rows = %+v, want the one decision the invoke made", dataset.Rows)
	}
	row := dataset.Rows[0]
	if row.Completion.Content != "landed answer" {
		t.Fatalf("completion = %q, want the answer the attempt observed", row.Completion.Content)
	}
	if len(row.Prompt) == 0 || row.Prompt[len(row.Prompt)-1].Content == "" {
		t.Fatalf("prompt = %+v, want the conversation the request presented", row.Prompt)
	}
	if row.DecisionID == "" || row.AttemptID == "" {
		t.Fatalf("row identity = (%q, %q), want the decision and attempt it was read from", row.DecisionID, row.AttemptID)
	}

	t.Run("missing_evidence_rejected", func(t *testing.T) {
		line, record := evidenceRequest(t, "dec-unreadable", landedSnapshot([]model.Message{{Role: model.RoleUser, Content: "q"}}))
		chain := &scriptedEvidence{
			lines: []rl.Line{line, evidenceAttempt("dec-unreadable", "at-unreadable", model.KindCompleted, model.Message{Role: model.RoleAssistant, Content: "a"})},
			errs:  map[string]error{record.Snapshot.SnapshotID: rl.ErrCaptureSnapshotMissing},
		}
		exporter, err := rl.NewEvidenceExporter(chain, chain)
		if err != nil {
			t.Fatalf("open the evidence exporter: %v", err)
		}
		dataset, err := exporter.SFT(ctx, rl.EvidenceOptions{})
		if err != nil {
			t.Fatalf("export beside unreadable evidence: %v", err)
		}
		if len(dataset.Rows) != 0 {
			t.Fatalf("rows = %+v, want none for evidence nothing reads back", dataset.Rows)
		}
		if got := refusalCounts(dataset.Refused); got[rl.RefusalMissingEvidence] != 1 {
			t.Fatalf("refusals = %+v, want one missing_evidence refusal", dataset.Refused)
		}
	})

	t.Run("redacted_not_exact", func(t *testing.T) {
		snapshot := landedSnapshot([]model.Message{{
			Role:         model.RoleUser,
			ContentParts: []model.ContentPart{{Type: "image", Image: &trpcmodel.Image{URL: rl.RedactedValue}}},
		}})
		line, _ := evidenceRequest(t, "dec-redacted", snapshot)
		chain := &scriptedEvidence{
			lines: []rl.Line{line, evidenceAttempt("dec-redacted", "at-redacted", model.KindCompleted, model.Message{Role: model.RoleAssistant, Content: "a"})},
			snaps: map[string]rl.RequestSnapshot{"snapshot-dec-redacted": snapshot},
		}
		exporter, err := rl.NewEvidenceExporter(chain, chain)
		if err != nil {
			t.Fatalf("open the evidence exporter: %v", err)
		}
		dataset, err := exporter.SFT(ctx, rl.EvidenceOptions{})
		if err != nil {
			t.Fatalf("export beside redacted evidence: %v", err)
		}
		if len(dataset.Rows) != 0 {
			t.Fatalf("rows = %+v, want none for a request whose content was projected", dataset.Rows)
		}
		if got := refusalCounts(dataset.Refused); got[rl.RefusalRedacted] != 1 {
			t.Fatalf("refusals = %+v, want one redacted_not_exact refusal", dataset.Refused)
		}
	})

	t.Run("digest_mismatch_rejected", func(t *testing.T) {
		line, _ := evidenceRequest(t, "dec-mismatch", landedSnapshot([]model.Message{{Role: model.RoleUser, Content: "read back"}}))
		other := landedSnapshot([]model.Message{{Role: model.RoleUser, Content: "another request"}})
		chain := &scriptedEvidence{
			lines: []rl.Line{line, evidenceAttempt("dec-mismatch", "at-mismatch", model.KindCompleted, model.Message{Role: model.RoleAssistant, Content: "a"})},
			snaps: map[string]rl.RequestSnapshot{"snapshot-dec-mismatch": other},
		}
		exporter, err := rl.NewEvidenceExporter(chain, chain)
		if err != nil {
			t.Fatalf("open the evidence exporter: %v", err)
		}
		dataset, err := exporter.SFT(ctx, rl.EvidenceOptions{})
		if err != nil {
			t.Fatalf("export beside mismatched evidence: %v", err)
		}
		if len(dataset.Rows) != 0 {
			t.Fatalf("rows = %+v, want none for content that does not match its digest", dataset.Rows)
		}
		if got := refusalCounts(dataset.Refused); got[rl.RefusalDigestMismatch] != 1 {
			t.Fatalf("refusals = %+v, want one digest_mismatch refusal", dataset.Refused)
		}
	})

	t.Run("evidence a directory does not hold is named", func(t *testing.T) {
		missing, err := rl.OpenFileEvidenceLines(t.TempDir())
		if err != nil {
			t.Fatalf("open an empty evidence directory: %v", err)
		}
		if _, err := missing.LandedLines(); !errors.Is(err, rl.ErrEvidenceMissing) {
			t.Fatalf("read evidence a directory does not hold = %v, want ErrEvidenceMissing", err)
		}
	})
}

// TestSFTDatasetShape pins the supervised fine-tuning row the evidence lands.
//   - the row carries the prompt, the completion and the tool declarations the request ran with
//   - tool_only_target_preserved: a completion carrying only a tool call is a target and is never dropped
//   - unsupported_media_rejected: a prompt carrying a media part is refused by name
//   - an unknown terminal is refused rather than exported as a target
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestSFTDatasetShape(t *testing.T) {
	ctx := context.Background()

	t.Run("prompt completion and tools", func(t *testing.T) {
		snapshot := landedSnapshot(
			[]model.Message{{Role: model.RoleUser, Content: "what is one plus one"}},
			`{"name":"echo-a"}`,
		)
		line, _ := evidenceRequest(t, "dec-shape", snapshot)
		chain := &scriptedEvidence{
			lines: []rl.Line{line, evidenceAttempt("dec-shape", "at-shape", model.KindCompleted, model.Message{Role: model.RoleAssistant, Content: "two"})},
			snaps: map[string]rl.RequestSnapshot{"snapshot-dec-shape": snapshot},
		}
		exporter, err := rl.NewEvidenceExporter(chain, chain)
		if err != nil {
			t.Fatalf("open the evidence exporter: %v", err)
		}
		dataset, err := exporter.SFT(ctx, rl.EvidenceOptions{})
		if err != nil {
			t.Fatalf("export the scripted evidence: %v", err)
		}
		if len(dataset.Rows) != 1 || len(dataset.Refused) != 0 {
			t.Fatalf("dataset = (%+v, %+v), want one row and no refusal", dataset.Rows, dataset.Refused)
		}
		row := dataset.Rows[0]
		if len(row.Prompt) != 1 || row.Prompt[0].Content != "what is one plus one" {
			t.Fatalf("prompt = %+v, want the conversation the request presented", row.Prompt)
		}
		if row.Completion.Content != "two" || row.Completion.Role != model.RoleAssistant {
			t.Fatalf("completion = %+v, want the assistant message the attempt observed", row.Completion)
		}
		if len(row.Tools) != 1 || string(row.Tools[0]) != `{"name":"echo-a"}` {
			t.Fatalf("tools = %+v, want the tool declarations the request ran with", row.Tools)
		}
	})

	t.Run("tool_only_target_preserved", func(t *testing.T) {
		snapshot := landedSnapshot([]model.Message{{Role: model.RoleUser, Content: "use the tool"}})
		line, _ := evidenceRequest(t, "dec-tool", snapshot)
		call := model.ToolCall{ID: "call-1", Type: "function", Function: model.FunctionDefinitionParam{Name: "echo-a", Arguments: []byte(`{"x":1}`)}}
		chain := &scriptedEvidence{
			lines: []rl.Line{line, evidenceAttempt("dec-tool", "at-tool", model.KindCompleted, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{call}})},
			snaps: map[string]rl.RequestSnapshot{"snapshot-dec-tool": snapshot},
		}
		exporter, err := rl.NewEvidenceExporter(chain, chain)
		if err != nil {
			t.Fatalf("open the evidence exporter: %v", err)
		}
		dataset, err := exporter.SFT(ctx, rl.EvidenceOptions{})
		if err != nil {
			t.Fatalf("export the tool-only target: %v", err)
		}
		if len(dataset.Rows) != 1 || len(dataset.Refused) != 0 {
			t.Fatalf("dataset = (%+v, %+v), want the tool-only target preserved as one row", dataset.Rows, dataset.Refused)
		}
		if len(dataset.Rows[0].Completion.ToolCalls) != 1 || dataset.Rows[0].Completion.ToolCalls[0].Function.Name != "echo-a" {
			t.Fatalf("completion = %+v, want the tool call the attempt observed", dataset.Rows[0].Completion)
		}
	})

	t.Run("unsupported_media_rejected", func(t *testing.T) {
		snapshot := landedSnapshot([]model.Message{{
			Role:         model.RoleUser,
			ContentParts: []model.ContentPart{{Type: "image", Image: &trpcmodel.Image{URL: "https://example.test/picture.png"}}},
		}})
		line, _ := evidenceRequest(t, "dec-media", snapshot)
		chain := &scriptedEvidence{
			lines: []rl.Line{line, evidenceAttempt("dec-media", "at-media", model.KindCompleted, model.Message{Role: model.RoleAssistant, Content: "a picture"})},
			snaps: map[string]rl.RequestSnapshot{"snapshot-dec-media": snapshot},
		}
		exporter, err := rl.NewEvidenceExporter(chain, chain)
		if err != nil {
			t.Fatalf("open the evidence exporter: %v", err)
		}
		dataset, err := exporter.SFT(ctx, rl.EvidenceOptions{})
		if err != nil {
			t.Fatalf("export the media prompt: %v", err)
		}
		if len(dataset.Rows) != 0 {
			t.Fatalf("rows = %+v, want none for a prompt the row cannot carry", dataset.Rows)
		}
		if got := refusalCounts(dataset.Refused); got[rl.RefusalUnsupportedMedia] != 1 {
			t.Fatalf("refusals = %+v, want one unsupported_media refusal", dataset.Refused)
		}
	})

	t.Run("an unknown terminal is no target", func(t *testing.T) {
		snapshot := landedSnapshot([]model.Message{{Role: model.RoleUser, Content: "question"}})
		line, _ := evidenceRequest(t, "dec-unknown", snapshot)
		chain := &scriptedEvidence{
			lines: []rl.Line{line, evidenceAttempt("dec-unknown", "at-unknown", model.KindUnknown, model.Message{Role: model.RoleAssistant, Content: "unsettled"})},
			snaps: map[string]rl.RequestSnapshot{"snapshot-dec-unknown": snapshot},
		}
		exporter, err := rl.NewEvidenceExporter(chain, chain)
		if err != nil {
			t.Fatalf("open the evidence exporter: %v", err)
		}
		dataset, err := exporter.SFT(ctx, rl.EvidenceOptions{})
		if err != nil {
			t.Fatalf("export the unknown terminal: %v", err)
		}
		if len(dataset.Rows) != 0 {
			t.Fatalf("rows = %+v, want none for an unknown terminal", dataset.Rows)
		}
		if got := refusalCounts(dataset.Refused); got[rl.RefusalTerminalUnknown] != 1 {
			t.Fatalf("refusals = %+v, want one unknown_terminal refusal", dataset.Refused)
		}
	})
}

// TestDPODatasetShape pins the preference pair the evidence lands.
//   - two attempts of one bench task on one prompt pair the passing completion with the failing one, carrying the preference source and its version
//   - different_prefix_not_paired: attempts of one task whose prompts differ are never paired
//   - missing_preference_rejected: an attempt no annex judged carries no preference
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestDPODatasetShape(t *testing.T) {
	ctx := context.Background()

	t.Run("a passing and a failing verdict pair", func(t *testing.T) {
		snapshot := landedSnapshot([]model.Message{{Role: model.RoleUser, Content: "one task"}}, `{"name":"echo-a"}`)
		chosenLine, _ := evidenceRequest(t, "dec-chosen", snapshot)
		rejectedLine, _ := evidenceRequest(t, "dec-rejected", snapshot)
		chain := &scriptedEvidence{
			lines: []rl.Line{
				chosenLine,
				evidenceAttempt("dec-chosen", "at-chosen", model.KindCompleted, model.Message{Role: model.RoleAssistant, Content: "good answer"}),
				evidenceAnnex("at-chosen", "task-a", "pass", "rules-v1"),
				rejectedLine,
				evidenceAttempt("dec-rejected", "at-rejected", model.KindCompleted, model.Message{Role: model.RoleAssistant, Content: "bad answer"}),
				evidenceAnnex("at-rejected", "task-a", "fail", "rules-v1"),
			},
			snaps: map[string]rl.RequestSnapshot{
				"snapshot-dec-chosen":   snapshot,
				"snapshot-dec-rejected": snapshot,
			},
		}
		exporter, err := rl.NewEvidenceExporter(chain, chain)
		if err != nil {
			t.Fatalf("open the evidence exporter: %v", err)
		}
		dataset, err := exporter.DPO(ctx, rl.EvidenceOptions{})
		if err != nil {
			t.Fatalf("export the judged pair: %v", err)
		}
		if len(dataset.Rows) != 1 || len(dataset.Refused) != 0 {
			t.Fatalf("dataset = (%+v, %+v), want one pair and no refusal", dataset.Rows, dataset.Refused)
		}
		row := dataset.Rows[0]
		if row.TaskID != "task-a" || row.Chosen.Content != "good answer" || row.Rejected.Content != "bad answer" {
			t.Fatalf("pair = %+v, want the chosen and rejected completions of task-a", row)
		}
		if row.Source != "annex_verdict" || row.Version != "rules-v1" {
			t.Fatalf("pair source = (%q, %q), want the verdict source beside the rule version", row.Source, row.Version)
		}
		if len(row.Prompt) != 1 || row.Prompt[0].Content != "one task" || len(row.Tools) != 1 {
			t.Fatalf("pair prompt = (%+v, %+v), want the prompt and tools both attempts ran", row.Prompt, row.Tools)
		}
	})

	t.Run("different_prefix_not_paired", func(t *testing.T) {
		first := landedSnapshot([]model.Message{{Role: model.RoleUser, Content: "first prompt"}})
		second := landedSnapshot([]model.Message{{Role: model.RoleUser, Content: "second prompt"}})
		firstLine, _ := evidenceRequest(t, "dec-first", first)
		secondLine, _ := evidenceRequest(t, "dec-second", second)
		chain := &scriptedEvidence{
			lines: []rl.Line{
				firstLine,
				evidenceAttempt("dec-first", "at-first", model.KindCompleted, model.Message{Role: model.RoleAssistant, Content: "first answer"}),
				evidenceAnnex("at-first", "task-b", "pass", "rules-v1"),
				secondLine,
				evidenceAttempt("dec-second", "at-second", model.KindCompleted, model.Message{Role: model.RoleAssistant, Content: "second answer"}),
				evidenceAnnex("at-second", "task-b", "fail", "rules-v1"),
			},
			snaps: map[string]rl.RequestSnapshot{
				"snapshot-dec-first":  first,
				"snapshot-dec-second": second,
			},
		}
		exporter, err := rl.NewEvidenceExporter(chain, chain)
		if err != nil {
			t.Fatalf("open the evidence exporter: %v", err)
		}
		dataset, err := exporter.DPO(ctx, rl.EvidenceOptions{})
		if err != nil {
			t.Fatalf("export the differing prompts: %v", err)
		}
		if len(dataset.Rows) != 0 {
			t.Fatalf("rows = %+v, want no pair over differing prompts", dataset.Rows)
		}
		if got := refusalCounts(dataset.Refused); got[rl.RefusalDifferentPrefix] != 1 {
			t.Fatalf("refusals = %+v, want one different_prefix refusal", dataset.Refused)
		}
	})

	t.Run("missing_preference_rejected", func(t *testing.T) {
		snapshot := landedSnapshot([]model.Message{{Role: model.RoleUser, Content: "unjudged task"}})
		line, _ := evidenceRequest(t, "dec-unjudged", snapshot)
		chain := &scriptedEvidence{
			lines: []rl.Line{line, evidenceAttempt("dec-unjudged", "at-unjudged", model.KindCompleted, model.Message{Role: model.RoleAssistant, Content: "answer"})},
			snaps: map[string]rl.RequestSnapshot{"snapshot-dec-unjudged": snapshot},
		}
		exporter, err := rl.NewEvidenceExporter(chain, chain)
		if err != nil {
			t.Fatalf("open the evidence exporter: %v", err)
		}
		dataset, err := exporter.DPO(ctx, rl.EvidenceOptions{})
		if err != nil {
			t.Fatalf("export the unjudged attempt: %v", err)
		}
		if len(dataset.Rows) != 0 {
			t.Fatalf("rows = %+v, want no pair without a preference", dataset.Rows)
		}
		if got := refusalCounts(dataset.Refused); got[rl.RefusalMissingPreference] != 1 {
			t.Fatalf("refusals = %+v, want one missing_preference refusal", dataset.Refused)
		}
	})
}

// recordLinesOf splits one sealed record file into its non-empty lines.
func recordLinesOf(content string) []string {
	lines := make([]string, 0)
	at := 0
	for at < len(content) {
		next := at
		for next < len(content) && content[next] != '\n' {
			next++
		}
		if next > at {
			lines = append(lines, content[at:next])
		}
		at = next + 1
	}
	return lines
}
