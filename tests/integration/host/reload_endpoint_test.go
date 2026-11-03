package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// reloadAnswer is the endpoint's answer as the supervisor reads it back: the
// publication result beside the model changes the reload judged. The receipt
// names the generation in effect after the attempt and whether that attempt
// published it, so a carried document is answered as one instead of leaving a
// reader to infer it from an empty change list.
type reloadAnswer struct {
	Reloaded    bool `json:"reloaded"`
	Publication struct {
		Generation uint64 `json:"generation"`
		Published  bool   `json:"published"`
	} `json:"publication"`
	ModelChanges []struct {
		Agent string `json:"agent"`
		From  string `json:"from"`
		To    string `json:"to"`
		Fact  string `json:"fact"`
	} `json:"model_changes"`
}

// TestReloadEndpointSwapsModel moves the declared model and posts the reload endpoint:
//   - the first turn is served by the model the boot document names
//   - the endpoint answers the judged move from the version in effect to the new one
//   - the model configuration change is committed under the configuration source and the versions
//   - the lease installs and publishes exactly the version the move asked for
//   - the next turn is served by the new model name the provider actually saw
//
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestReloadEndpointSwapsModel(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	host, err := assembly.Boot(configPath, assembly.WithSender(recordingSender{}))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if host.FaceAddr() == "" {
		t.Fatal("the host face listens on no address")
	}

	first := invokeScriptedTurn(t, ctx, host, "session-reload")
	if first.Terminal != model.KindCompleted {
		t.Fatalf("first turn terminal = %v, want the completed turn of the boot model", first.Terminal)
	}
	if models := provider.servedModels(); len(models) != 1 || models[0] != "model-a" {
		t.Fatalf("served models = %v, want the one request of the boot model", models)
	}

	rewriteScriptedHostDocument(t, configPath, root, provider.endpoint(), "model-b")

	answer := postReload(t, host.FaceAddr())
	if !answer.Reloaded {
		t.Fatal("the reload answered without publishing the rewritten document")
	}
	if len(answer.ModelChanges) != 1 {
		t.Fatalf("judged model changes = %+v, want the one move of the entry agent", answer.ModelChanges)
	}
	change := answer.ModelChanges[0]
	if change.Agent != "user" {
		t.Fatalf("judged change agent = %q, want the entry agent", change.Agent)
	}
	if change.From != "local/model-a" || change.To != "local/model-b" {
		t.Fatalf("judged change = %q -> %q, want the move between the two registered versions", change.From, change.To)
	}
	if change.Fact == "" {
		t.Fatal("the judged change names no committed model configuration change")
	}

	publications := host.Run().LeasePublications()
	if len(publications) != 1 || publications[0].Name() != "model-b@local/model-b" {
		t.Fatalf("lease publications = %+v, want the one version the replacement installed", publications)
	}

	fact, err := host.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the committed model configuration change: %v", err)
	}
	if fact.Type != event.TypeConfigChange {
		t.Fatalf("committed fact type = %q, want the configuration change family", fact.Type)
	}
	if fact.Owner.AgentID != "user" {
		t.Fatalf("committed fact owner = %q, want the agent whose model moved", fact.Owner.AgentID)
	}
	if got := fact.Meta[event.MetaKeyConfigChangeDimension]; got != event.ConfigChangeDimensionModel {
		t.Fatalf("committed fact dimension = %q, want the model dimension", got)
	}
	if got := fact.Meta[event.MetaKeyConfigChangeGeneration]; got != strconv.FormatUint(answer.Publication.Generation, 10) {
		t.Fatalf("committed fact generation = %q, want the publication generation %d", got, answer.Publication.Generation)
	}
	if got := fact.Meta[event.MetaKeyTriggerSource]; got != event.ConfigChangeSourceConfig {
		t.Fatalf("committed fact trigger source = %q, want the configuration source", got)
	}
	if got := fact.Meta[event.MetaKeyModelFromVersion]; got != "model-a@local/model-a" {
		t.Fatalf("committed fact from version = %q, want the version the move started from", got)
	}
	if got := fact.Meta[event.MetaKeyModelToVersion]; got != "local/model-b" {
		t.Fatalf("committed fact to version = %q, want the version the move asked for", got)
	}

	second := invokeScriptedTurn(t, ctx, host, "session-reload")
	if second.Terminal != model.KindCompleted {
		t.Fatalf("second turn terminal = %v, want the completed turn of the installed version", second.Terminal)
	}
	models := provider.servedModels()
	if len(models) != 2 || models[1] != "model-b" {
		t.Fatalf("served models = %v, want the second request served by the installed model", models)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestReloadEndpointPublishesTheAddedAgent posts a document that adds an agent.
//   - the endpoint answers the publication instead of a structural refusal
//   - the receipt names the one organization agents change, owned by the agent the document added, and no replacement version is installed
//   - the next turn of the entry is still served by the model the boot document named
//   - the added address serves a real turn on the model it declares
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestReloadEndpointPublishesTheAddedAgent(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf("ok") }
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	host, err := assembly.Boot(configPath, assembly.WithSender(recordingSender{}))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if turn := invokeScriptedTurn(t, ctx, host, "session-published"); turn.Terminal != model.KindCompleted {
		t.Fatalf("first turn terminal = %v, want the completed turn of the boot model", turn.Terminal)
	}

	writeHostDocumentFile(t, configPath, structuralScriptedHostDocument(root, provider.endpoint()))

	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != http.StatusOK {
		t.Fatalf("added-agent reload status = %d, want 200: %s", status, body)
	}
	answer := decodeConfigChangeAnswer(t, body)
	if !answer.Publication.Published || answer.Publication.Generation == 0 {
		t.Fatalf("publication = %+v, want the added agent published as a new generation", answer.Publication)
	}
	if len(answer.ModelChanges) != 0 {
		t.Fatalf("judged model changes = %s, want none beside the added agent", answer.ModelChanges)
	}
	if len(answer.ConfigChanges) != 1 {
		t.Fatalf("filed config changes = %+v, want the one organization agents change", answer.ConfigChanges)
	}
	change := answer.ConfigChanges[0]
	if change.Agent != "other" || change.Dimension != "org.agents" {
		t.Fatalf("filed config change = %+v, want the organization agents dimension of the added agent", change)
	}
	if change.Fact == "" {
		t.Fatal("the filed agents change names no committed fact")
	}
	if publications := host.Run().LeasePublications(); len(publications) != 0 {
		t.Fatalf("lease publications = %+v, want none beside an added agent", publications)
	}

	if turn := invokeScriptedTurn(t, ctx, host, "session-published"); turn.Terminal != model.KindCompleted {
		t.Fatalf("second turn terminal = %v, want the completed turn of the unchanged model", turn.Terminal)
	}
	models := provider.servedModels()
	if len(models) != 2 || models[0] != "model-a" || models[1] != "model-a" {
		t.Fatalf("served models = %v, want both entry turns served by the model the boot document named", models)
	}
	added := invokeAgentTurn(t, ctx, host, "other", "session-published-agent")
	if added.Terminal != model.KindCompleted {
		t.Fatalf("added agent turn terminal = %v, want the completed turn of the added address", added.Terminal)
	}
	models = provider.servedModels()
	if len(models) != 3 || models[2] != "model-a" {
		t.Fatalf("served models = %v, want the added agent served by the model its declaration names", models)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// invokeScriptedTurn drives one user turn of the booted host and answers its
// turn.
func invokeScriptedTurn(t *testing.T, ctx context.Context, host *assembly.Host, sessionID string) agent.Turn {
	t.Helper()
	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address: event.Address{AgentID: "user", SessionID: event.SessionID(sessionID)},
		Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("invoke the booted host: %v", err)
	}
	return turn
}

// postReload posts the reload endpoint on the host's loopback face and answers
// the decoded publication result.
func postReload(t *testing.T, faceAddr string) reloadAnswer {
	t.Helper()
	request, err := http.NewRequest("POST", fmt.Sprintf("http://%s/reload", faceAddr), bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("build the reload request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("post the reload: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, hostBodyLimit))
	if err != nil {
		t.Fatalf("read the reload answer: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reload status = %d, want 200: %s", resp.StatusCode, body)
	}
	var answer reloadAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode the reload answer %q: %v", body, err)
	}
	return answer
}

// hostBodyLimit is the ceiling of one reload answer the check reads back.
const hostBodyLimit = 1 << 20
