package host

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestConfigChangeLandsOnTheChain drives the configuration change fact over the
// formal reload entry:
//   - a prompt declaration change lands one config_change fact on the chain
//   - the fact names the prompt dimension, the generation the publication
//     assigned and the change it describes
//   - a refused reload lands no fact
//   - a model-only change files the replacement fact and no duplicate config
//     change
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestConfigChangeLandsOnTheChain(t *testing.T) {
	t.Run("prompt change lands on the chain", runConfigChangeLands)
	t.Run("binding change lands on the chain", runBindingChangeLands)
	t.Run("refused reload lands no fact", runConfigChangeRefused)
	t.Run("model change files no duplicate config change", runModelOnlyFilesNoConfigChange)
	t.Run("boot and unchanged reload land no fact", runNoFactOnBootOrUnchanged)
}

// configChangeReloadAnswer is the subset of the reload answer these checks read:
// the publication receipt, the model changes and the configuration change facts
// the reload filed.
type configChangeReloadAnswer struct {
	Reloaded    bool `json:"reloaded"`
	Publication struct {
		Generation uint64 `json:"generation"`
		Published  bool   `json:"published"`
	} `json:"publication"`
	ModelChanges  []json.RawMessage `json:"model_changes"`
	ConfigChanges []struct {
		Agent     string `json:"agent"`
		Dimension string `json:"dimension"`
		Fact      string `json:"fact"`
	} `json:"config_changes"`
}

// runConfigChangeLands changes the declared prompt file through the formal
// reload entry:
//   - the publication answers one new generation and no model move
//   - one config_change fact lands on the chain, owned by the agent, with the
//     prompt dimension, the publication generation and the change description
func runConfigChangeLands(t *testing.T) {
	viewAnchorEnv(t)
	const (
		bootBody = "CONFIG-CHANGE-BOOT-PROMPT"
		nextBody = "CONFIG-CHANGE-NEXT-PROMPT"
	)
	provider := newScriptedProvider(t)
	root := t.TempDir()
	writeReloadPrompt(t, root, "boot.md", bootBody)
	writeReloadPrompt(t, root, "next.md", nextBody)
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("boot.md")))
	host := bootViewAnchorHost(t, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	before := host.Run().CommittedFacts()

	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("next.md")))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("reload status = %d, want 200: %s", status, body)
	}
	answer := decodeConfigChangeAnswer(t, body)
	if !answer.Publication.Published || answer.Publication.Generation == 0 {
		t.Fatalf("publication = %+v, want the prompt change published as a new generation", answer.Publication)
	}
	if len(answer.ModelChanges) != 0 {
		t.Fatalf("judged model changes = %s, want none beside a prompt-only change", answer.ModelChanges)
	}
	if len(answer.ConfigChanges) != 1 {
		t.Fatalf("filed config changes = %+v, want the one prompt dimension", answer.ConfigChanges)
	}
	change := answer.ConfigChanges[0]
	if change.Agent != "user" || change.Dimension != "prompt" {
		t.Fatalf("filed config change = %+v, want the prompt dimension of the declared agent", change)
	}
	if change.Fact == "" {
		t.Fatal("the filed config change names no committed fact")
	}

	fact, err := host.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the committed config change fact: %v", err)
	}
	if fact.Type != "config_change" {
		t.Fatalf("committed fact type = %q, want %q", fact.Type, "config_change")
	}
	if fact.Owner.AgentID != "user" {
		t.Fatalf("committed fact owner = %q, want the agent whose prompt moved", fact.Owner.AgentID)
	}
	if got := fact.Meta["trigger_source"]; got != "config" {
		t.Fatalf("committed fact trigger source = %q, want the configuration source", got)
	}
	if got := fact.Meta["config_change_dimension"]; got != "prompt" {
		t.Fatalf("committed fact dimension = %q, want the prompt dimension", got)
	}
	wantGeneration := strconv.FormatUint(answer.Publication.Generation, 10)
	if got := fact.Meta["config_change_generation"]; got != wantGeneration {
		t.Fatalf("committed fact generation = %q, want the publication generation %s", got, wantGeneration)
	}
	if !strings.Contains(fact.Message.Content, "prompt") ||
		!strings.Contains(fact.Message.Content, "boot.md") ||
		!strings.Contains(fact.Message.Content, "next.md") {
		t.Fatalf("committed fact body = %q, want the prompt change described", fact.Message.Content)
	}
	if after := host.Run().CommittedFacts(); after != before+1 {
		t.Fatalf("committed facts = %d after the prompt change, want the one config change beside the %d before", after, before)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// runConfigChangeRefused rewrites a field only Boot reads and reloads:
//   - the reload is refused by name as requiring a restart
//   - no config_change fact lands on the chain
func runConfigChangeRefused(t *testing.T) {
	viewAnchorEnv(t)
	provider := newScriptedProvider(t)
	root := t.TempDir()
	writeReloadPrompt(t, root, "boot.md", "CONFIG-CHANGE-REFUSED-PROMPT")
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("boot.md")))
	host := bootViewAnchorHost(t, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	before := host.Run().CommittedFacts()

	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("boot.md"), "request_timeout_seconds: 42\n"))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 409 {
		t.Fatalf("refused reload status = %d, want 409: %s", status, body)
	}
	if !strings.Contains(body, "requires a restart") {
		t.Fatalf("refused reload body = %q, want the restart named", body)
	}
	if after := host.Run().CommittedFacts(); after != before {
		t.Fatalf("committed facts = %d after a refused reload, want the %d before it", after, before)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// runModelOnlyFilesNoConfigChange moves the declared model while the prompt
// stays as it was:
//   - the reload publishes the new generation and judges the one model move
//   - no config_change fact is filed beside the model change
func runModelOnlyFilesNoConfigChange(t *testing.T) {
	viewAnchorEnv(t)
	provider := newScriptedProvider(t)
	root := t.TempDir()
	writeReloadPrompt(t, root, "boot.md", "CONFIG-CHANGE-MODEL-PROMPT")
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("boot.md")))
	host := bootViewAnchorHost(t, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-b", root, reloadPromptBlock("boot.md")))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("model reload status = %d, want 200: %s", status, body)
	}
	answer := decodeConfigChangeAnswer(t, body)
	if !answer.Publication.Published {
		t.Fatalf("publication = %+v, want the model move published as a new generation", answer.Publication)
	}
	if len(answer.ModelChanges) != 1 {
		t.Fatalf("judged model changes = %s, want the one move onto the new version", answer.ModelChanges)
	}
	if len(answer.ConfigChanges) != 0 {
		t.Fatalf("filed config changes = %+v, want none beside the model change", answer.ConfigChanges)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// runBindingChangeLands moves one declared binding field over the formal reload
// entry:
//   - the publication answers one new generation and no model move
//   - one config_change fact lands with the binding dimension, naming the field
//     that moved and not the prompt sources that stayed as they were, so the
//     binding dimension is a diff of the declared fields
//   - the fact carries the publication generation and the configuration source
func runBindingChangeLands(t *testing.T) {
	viewAnchorEnv(t)
	provider := newScriptedProvider(t)
	root := t.TempDir()
	writeReloadPrompt(t, root, "boot.md", "CONFIG-CHANGE-BINDING-PROMPT")
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root,
		reloadPromptBlock("boot.md")+"    temperature: 0.25\n"))
	host := bootViewAnchorHost(t, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	before := host.Run().CommittedFacts()

	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root,
		reloadPromptBlock("boot.md")+"    temperature: 0.75\n"))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("reload status = %d, want 200: %s", status, body)
	}
	answer := decodeConfigChangeAnswer(t, body)
	if !answer.Publication.Published || answer.Publication.Generation == 0 {
		t.Fatalf("publication = %+v, want the binding change published as a new generation", answer.Publication)
	}
	if len(answer.ModelChanges) != 0 {
		t.Fatalf("judged model changes = %s, want none beside a binding-only change", answer.ModelChanges)
	}
	if len(answer.ConfigChanges) != 1 {
		t.Fatalf("filed config changes = %+v, want the one binding dimension", answer.ConfigChanges)
	}
	change := answer.ConfigChanges[0]
	if change.Agent != "user" || change.Dimension != "binding" {
		t.Fatalf("filed config change = %+v, want the binding dimension of the declared agent", change)
	}

	fact, err := host.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the committed config change fact: %v", err)
	}
	if got := fact.Meta["config_change_dimension"]; got != "binding" {
		t.Fatalf("committed fact dimension = %q, want the binding dimension", got)
	}
	if got := fact.Meta["trigger_source"]; got != "config" {
		t.Fatalf("committed fact trigger source = %q, want the configuration source", got)
	}
	if got := fact.Meta["config_change_generation"]; got != strconv.FormatUint(answer.Publication.Generation, 10) {
		t.Fatalf("committed fact generation = %q, want the publication generation %d", got, answer.Publication.Generation)
	}
	if !strings.Contains(fact.Message.Content, "binding changed: sampling") {
		t.Fatalf("committed fact body = %q, want the moved field named as a binding diff", fact.Message.Content)
	}
	if strings.Contains(fact.Message.Content, "prompt source changed") {
		t.Fatalf("committed fact body = %q, want the unchanged prompt sources left out of the binding diff", fact.Message.Content)
	}
	if after := host.Run().CommittedFacts(); after != before+1 {
		t.Fatalf("committed facts = %d after the binding change, want the one config change beside the %d before", after, before)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// runNoFactOnBootOrUnchanged boots a host and reloads a document it already
// carries:
//   - boot files no configuration change fact
//   - an unchanged reload answers no new generation, files no config change and
//     leaves the fact count where boot left it
func runNoFactOnBootOrUnchanged(t *testing.T) {
	viewAnchorEnv(t)
	provider := newScriptedProvider(t)
	root := t.TempDir()
	writeReloadPrompt(t, root, "boot.md", "CONFIG-CHANGE-UNCHANGED-PROMPT")
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("boot.md")))
	host := bootViewAnchorHost(t, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if booted := host.Run().CommittedFacts(); booted != 0 {
		t.Fatalf("committed facts = %d after boot, want none: boot judges no configuration change", booted)
	}

	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("unchanged reload status = %d, want 200: %s", status, body)
	}
	answer := decodeConfigChangeAnswer(t, body)
	if answer.Publication.Published {
		t.Fatalf("publication = %+v, want the unchanged document carried without a new generation", answer.Publication)
	}
	if len(answer.ConfigChanges) != 0 {
		t.Fatalf("filed config changes = %+v, want none beside an unchanged document", answer.ConfigChanges)
	}
	if after := host.Run().CommittedFacts(); after != 0 {
		t.Fatalf("committed facts = %d after an unchanged reload, want none: an unjudged change lands no fact", after)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// decodeConfigChangeAnswer decodes one answered reload body into the subset of
// the receipt these checks read.
func decodeConfigChangeAnswer(t *testing.T, body string) configChangeReloadAnswer {
	t.Helper()
	var answer configChangeReloadAnswer
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		t.Fatalf("decode the reload answer %q: %v", body, err)
	}
	return answer
}
