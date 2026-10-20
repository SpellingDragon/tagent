package acceptance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"

	"trpc.group/trpc-go/trpc-agent-go/skill"
)

// scenarioChainKey is the session the context-layout scenarios replay their turns
// on.
// 契约: docs/wiki/agent/agent-architecture.md#test-support
var scenarioChainKey = replay.ChainKey{PartitionID: 1, SessionID: event.SessionID("scenario-context")}

// scenarioBase is the pass time the replayed turns are laid out from.
var scenarioBase = time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)

// scenarioPartStaticHead is the label the fixed prompt and skill bodies carry in
// the closed part set of one request.
const scenarioPartStaticHead = "static_head"

// scenarioPartFoldedSummary is the label the folded history summary carries.
const scenarioPartFoldedSummary = "folded_summary"

// scenarioPartProjection is the label the replayed projection entries carry.
const scenarioPartProjection = "projection"

// scenarioPartEnvironment is the label the dynamic environment block carries.
const scenarioPartEnvironment = "environment_block"

// scenarioPartTranscript is the label the invocation's own input carries.
const scenarioPartTranscript = "transcript"

// scenarioHistorySummaryPrefix is the content prefix a folded history summary is
// rendered with.
const scenarioHistorySummaryPrefix = "history summary: "

// scenarioSegmentFoldPrefix is the content prefix a folded segment is rendered
// with.
const scenarioSegmentFoldPrefix = "folded segment process: "

// scenarioEnvironmentPrefix is the content prefix the dynamic environment block
// is rendered with.
const scenarioEnvironmentPrefix = "## Environment\n"

// scenarioRecoveryPrefix is the content prefix the replay path's recovery marker
// is rendered with: the marker names how much of the chain the budget replayed,
// so it is no folded entry of the projection itself.
const scenarioRecoveryPrefix = "recovery: "

// scenarioChainFact is one fact of a replayed turn: the summary is the one-line
// reading an age fold keeps, and the content is the raw text it drops.
func scenarioChainFact(key, typ, summary, content string, step int) replay.ChainFact {
	return replay.ChainFact{
		Key:     event.EventID(key),
		Chain:   scenarioChainKey,
		Type:    typ,
		Summary: summary,
		Content: content,
		Time:    scenarioBase.Add(time.Duration(step) * time.Minute),
	}
}

// scenarioTurn lays one closed turn on the chain: the input that opens it, the
// process detail between the input and the output, and the output that closes.
// The keys carry the turn and the position in it, so the chain reads back in the
// order the turns ran.
func scenarioTurn(step int, seed string) []replay.ChainFact {
	type leg struct {
		suffix  string
		typ     string
		summary string
		body    string
	}
	legs := []leg{
		{"in", event.TypeExternalInput, "ask " + seed, "请求正文 " + seed + "。"},
		{"plan", event.TypeThinkingPlan, "plan " + seed, "思考正文 " + seed + "。"},
		{"call", event.TypeActionCommand, "call " + seed, "工具输出 " + seed + "。"},
		{"out", event.TypeAgentOutput, "answer " + seed, "回答正文 " + seed + "。"},
	}
	facts := make([]replay.ChainFact, 0, len(legs))
	for position, part := range legs {
		key := fmt.Sprintf("%02d-%d-%s", step, position, part.suffix)
		facts = append(facts, scenarioChainFact(key, part.typ, part.summary, strings.Repeat(part.body, 12), step*10+position))
	}
	return facts
}

// scenarioFourTurns lays the four closed turns the context-layout scenarios
// replay: the newest band is the turn kept as it stands and the older three are
// the material the age fold consumes.
func scenarioFourTurns() []replay.ChainFact {
	var facts []replay.ChainFact
	for step, seed := range []string{"first", "second", "third", "fourth"} {
		facts = append(facts, scenarioTurn(step, seed)...)
	}
	return facts
}

// scenarioProjection replays the given facts into one projection over the
// scenario session, as a cold start reads them back.
func scenarioProjection(t *testing.T, facts []replay.ChainFact) *replay.ReplayProjection {
	t.Helper()
	ctx := context.Background()
	chain := replay.NewMemoryEventChain()
	for _, fact := range facts {
		if err := chain.Commit(ctx, fact); err != nil {
			t.Fatalf("commit the chain fact %s: %v", fact.Key, err)
		}
	}
	projection := replay.NewReplayProjection()
	if _, err := replay.RebuildProjectionFromWAL(ctx, projection, chain, replay.NewMemoryRelations(), scenarioChainKey, replay.RebuildOptions{}); err != nil {
		t.Fatalf("rebuild the projection from the chain: %v", err)
	}
	return projection
}

// scenarioWriteSkill writes one skill folder with its SKILL.md under the given
// root, in the shape the framework skill repository scans and parses.
func scenarioWriteSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the skill directory: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: \"the named skill\"\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, skill.SkillFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write the skill file: %v", err)
	}
}

// scenarioFixedFiles writes the prompt body and the skill body the static head of
// a context request names, and reports the file references the declaration
// carries them under.
func scenarioFixedFiles(t *testing.T) (promptBody, skillBody string, files []config.FileRef) {
	t.Helper()
	root := t.TempDir()
	promptBody = "the prompt body of the scenario assembly"
	skillBody = "the skill body of the scenario assembly"
	scenarioWritePrompt(t, filepath.Join(root, "prompts", "system.md"), promptBody+"\n")
	scenarioWriteSkill(t, root, "greet", skillBody+"\n")
	return promptBody, skillBody, []config.FileRef{
		{Path: filepath.Join(root, "prompts", "system.md"), Kind: config.FileKindPrompt},
		{Path: filepath.Join(root, "skills", "greet", skill.SkillFile), Kind: config.FileKindSkill},
	}
}

// scenarioContextRun assembles one runtime whose session carries the given
// projection, whose static head carries the fixed files and whose requests close
// with the dynamic environment block, so the request the model is driven with is
// the assembled boundary the scenario observes.
func scenarioContextRun(t *testing.T, projection *replay.ReplayProjection) (*tagent.Run, *scenarioModel, string, string) {
	t.Helper()
	promptBody, skillBody, files := scenarioFixedFiles(t)
	provider := &scenarioModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return scenarioFinalTurn("settled")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:               "agent-a",
			Model:            "model-a",
			ReplayBudget:     1 << 16,
			EnvironmentBlock: true,
			Files:            files,
		}},
	}
	run, err := tagent.Assemble(context.Background(), decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider},
	}, tagent.WithGovernance(&scenarioGovernance{answers: scenarioPassingChecks("eval", "benchmark", "apply-gate")}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	if err := run.AttachReplay(agent.RequestChain{PartitionID: scenarioChainKey.PartitionID, SessionID: scenarioChainKey.SessionID}, 1<<16, projection); err != nil {
		t.Fatalf("attach the replayed projection: %v", err)
	}
	return run, provider, promptBody, skillBody
}

// scenarioInvokeOnce drives one invocation of the scenario session and reports
// the messages of the single request the model was driven with.
func scenarioInvokeOnce(t *testing.T, run *tagent.Run, provider *scenarioModel, invocation, input string) []model.Message {
	t.Helper()
	if _, err := run.Invoke(context.Background(), tagent.InvokeRequest{
		InvocationID: event.InvocationID(invocation),
		Address:      event.Address{AgentID: "agent-a", SessionID: scenarioChainKey.SessionID},
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	}); err != nil {
		t.Fatalf("invoke the formal request: %v", err)
	}
	requests := provider.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want exactly one formal request", len(requests))
	}
	return requests[0].Messages
}

// scenarioPartOf classifies one request message into the closed part set of the
// request boundary, and reports an empty label for a message the set does not
// cover.
func scenarioPartOf(msg model.Message, input, promptBody, skillBody string) string {
	switch {
	case strings.HasPrefix(msg.Content, scenarioEnvironmentPrefix):
		return scenarioPartEnvironment
	case msg.Content == input:
		return scenarioPartTranscript
	case strings.Contains(msg.Content, promptBody) || strings.Contains(msg.Content, skillBody):
		return scenarioPartStaticHead
	case strings.HasPrefix(msg.Content, scenarioHistorySummaryPrefix):
		return scenarioPartFoldedSummary
	case msg.Role == model.RoleSystem:
		return ""
	default:
		return scenarioPartProjection
	}
}

// scenarioParts classifies every message of one request and reports the part
// labels in appearance order, failing the drive on a message the closed set does
// not cover.
func scenarioParts(t *testing.T, messages []model.Message, input, promptBody, skillBody string) []string {
	t.Helper()
	parts := make([]string, 0, len(messages))
	for index, msg := range messages {
		part := scenarioPartOf(msg, input, promptBody, skillBody)
		if part == "" {
			t.Fatalf("request message %d = (%q, %q) belongs to no declared part of the assembly", index, msg.Role, msg.Content)
		}
		parts = append(parts, part)
	}
	return parts
}

// scenarioDistinct reports the labels of the closed part set in appearance order,
// each label named once: the count of them is the closed part count of the
// request.
func scenarioDistinct(parts []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}

// scenarioIndex reports the position of the first message carrying one part, or
// -1 while the request carries none.
func scenarioIndex(parts []string, part string) int {
	for index, label := range parts {
		if label == part {
			return index
		}
	}
	return -1
}

// scenarioCountType reports how many projection entries carry one entry type.
func scenarioCountType(entries []replay.ProjectionEntry, entryType string) int {
	count := 0
	for _, entry := range entries {
		if entry.Type == entryType {
			count++
		}
	}
	return count
}
