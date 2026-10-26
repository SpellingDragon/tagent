// J4 is the hot-reload journey: the supervisor rewrites the declared model and
// posts the reload endpoint the host face serves on loopback, and the next turn
// is served by the model the rewrite named.
//
// 契约: docs/wiki/platform/org-hot-reload.md#overview
package journey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// journeyReloadAnswer is the reload endpoint's answer as a supervisor reads it.
type journeyReloadAnswer struct {
	Reloaded     bool `json:"reloaded"`
	ModelChanges []struct {
		Agent string `json:"agent"`
		From  string `json:"from"`
		To    string `json:"to"`
		Fact  string `json:"fact"`
	} `json:"model_changes"`
}

// TestJourneyReloadSwapsModel drives one turn, rewrites the model and posts the reload endpoint:
// - the first turn is served by the model the boot document named
// - the endpoint answers the judged move from the boot version to the rewritten one
// - the next turn's request carries the new model name the provider saw
//
// 契约: docs/wiki/platform/org-hot-reload.md#overview
func TestJourneyReloadSwapsModel(t *testing.T) {
	const (
		session      = "session-journey-reload"
		firstAnswer  = "the boot model answered"
		secondAnswer = "the swapped model answered"
	)
	provider := newJourneyProvider(t)
	provider.scriptAnswers("model-chat", firstAnswer)
	provider.scriptAnswers("model-swapped", secondAnswer)
	host := bootJourney(t, provider, journeySpec{})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if host.host.FaceAddr() == "" {
		t.Fatal("the host face listens on no address")
	}

	host.invokeOK(t, ctx, session, "first turn")
	if models := provider.servedModels(); len(models) != 1 || models[0] != "model-chat" {
		t.Fatalf("served models after the first turn = %v, want the boot model", models)
	}

	host.rewrite(t, journeySpec{Model: "model-swapped"})
	answer := postJourneyReload(t, host.host.FaceAddr())
	if !answer.Reloaded {
		t.Fatal("the reload answered without publishing the rewritten document")
	}
	if len(answer.ModelChanges) != 1 {
		t.Fatalf("judged model changes = %+v, want the one move of the entry agent", answer.ModelChanges)
	}
	change := answer.ModelChanges[0]
	if change.Agent != string(journeyEntryAgent) {
		t.Fatalf("judged change agent = %q, want the entry agent", change.Agent)
	}
	if change.From != "local/model-chat" || change.To != "local/model-swapped" {
		t.Fatalf("judged change = %q -> %q, want the move between the two registered versions", change.From, change.To)
	}
	if change.Fact == "" {
		t.Fatal("the judged change names no committed replacement fact")
	}

	host.invokeOK(t, ctx, session, "second turn")
	models := provider.servedModels()
	if len(models) != 2 || models[1] != "model-swapped" {
		t.Fatalf("served models = %v, want the second turn served by the rewritten model", models)
	}
}

// postJourneyReload posts the reload endpoint on the host's loopback face and
// answers the decoded publication result.
func postJourneyReload(t *testing.T, faceAddr string) journeyReloadAnswer {
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read the reload answer: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reload status = %d, want 200: %s", resp.StatusCode, body)
	}
	var answer journeyReloadAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode the reload answer %q: %v", body, err)
	}
	return answer
}
