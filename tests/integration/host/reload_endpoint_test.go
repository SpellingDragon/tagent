package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
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
//   - the replacement fact is committed under the configuration source and the versions
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
	host, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
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
		t.Fatal("the judged change names no committed replacement fact")
	}

	publications := host.Run().LeasePublications()
	if len(publications) != 1 || publications[0].Name() != "model-b@local/model-b" {
		t.Fatalf("lease publications = %+v, want the one version the replacement installed", publications)
	}

	fact, err := host.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the committed replacement fact: %v", err)
	}
	if fact.Type != event.ModelReplacementType {
		t.Fatalf("committed fact type = %q, want %q", fact.Type, event.ModelReplacementType)
	}
	if fact.Owner.AgentID != "user" {
		t.Fatalf("committed fact owner = %q, want the agent whose model moved", fact.Owner.AgentID)
	}
	if got := fact.Meta[event.MetaKeyTriggerSource]; got != event.ModelReplacementSourceConfig {
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

// TestReloadEndpointRefusesStructuralChange posts a document that adds an agent:
//   - the endpoint answers the structural refusal instead of publishing
//   - no replacement version is installed and no lease publishes a version
//   - the next turn is still served by the model the boot document named
//
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestReloadEndpointRefusesStructuralChange(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	host, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if turn := invokeScriptedTurn(t, ctx, host, "session-refused"); turn.Terminal != model.KindCompleted {
		t.Fatalf("first turn terminal = %v, want the completed turn of the boot model", turn.Terminal)
	}

	writeHostDocumentFile(t, configPath, structuralScriptedHostDocument(root, provider.endpoint()))

	resp, err := http.Post(fmt.Sprintf("http://%s/reload", host.FaceAddr()), "application/json", nil)
	if err != nil {
		t.Fatalf("post the structural reload: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, hostBodyLimit))
	if err != nil {
		t.Fatalf("read the structural refusal: %v", err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("structural reload status = %d, want 409: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "declared agent set") {
		t.Fatalf("structural refusal body = %q, want the declared agent set named", body)
	}
	if publications := host.Run().LeasePublications(); len(publications) != 0 {
		t.Fatalf("lease publications = %+v, want none after a refused reload", publications)
	}

	if turn := invokeScriptedTurn(t, ctx, host, "session-refused"); turn.Terminal != model.KindCompleted {
		t.Fatalf("second turn terminal = %v, want the completed turn of the unchanged model", turn.Terminal)
	}
	models := provider.servedModels()
	if len(models) != 2 || models[0] != "model-a" || models[1] != "model-a" {
		t.Fatalf("served models = %v, want both turns served by the model the boot document named", models)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// invokeScriptedTurn drives one user turn of the booted host and answers its
// turn.
func invokeScriptedTurn(t *testing.T, ctx context.Context, host *tagent.Host, sessionID string) agent.Turn {
	t.Helper()
	turn, err := host.Invoke(ctx, tagent.InvokeRequest{
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
