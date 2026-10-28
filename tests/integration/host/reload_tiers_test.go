package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// TestReloadPublishesTheHotFieldsAsOneGeneration moves the model and the declared prompt file between two turns:
//   - the boot request is served by the declared model and carries the first prompt body
//   - the reload answers one new generation and the judged model change names the move
//   - the next request is served by the installed model and carries the second body
//   - the body of the generation before is gone, so the request reads the published prompt source
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestReloadPublishesTheHotFieldsAsOneGeneration(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	root := t.TempDir()
	writeReloadPrompt(t, root, "first.md", bootPromptMarker)
	writeReloadPrompt(t, root, "second.md", publishedPromptMarker)
	configPath := filepath.Join(root, "tagent.yaml")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("first.md")))
	host, err := assembly.Boot(configPath, assembly.WithSender(newRecordingSender()))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if turn := invokeScriptedTurn(t, ctx, host, "session-tier-hot"); turn.Terminal != model.KindCompleted {
		t.Fatalf("first turn terminal = %v, want the completed turn of the boot generation", turn.Terminal)
	}
	if !requestCarriesPrompt(provider, 0, bootPromptMarker) {
		t.Fatalf("the boot request carried no %q, want the declared prompt body", bootPromptMarker)
	}

	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-b", root, reloadPromptBlock("second.md")))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != http.StatusOK {
		t.Fatalf("reload status = %d, want 200: %s", status, body)
	}
	answer := decodeReloadAnswer(t, body)
	if !answer.Publication.Published {
		t.Fatalf("publication = %+v, want the moved model and prompt files published as a new generation", answer.Publication)
	}
	if answer.Publication.Generation == 0 {
		t.Fatalf("publication = %+v, want the generation the publication entry assigned", answer.Publication)
	}
	if len(answer.ModelChanges) != 1 || answer.ModelChanges[0].To != "local/model-b" {
		t.Fatalf("judged model changes = %+v, want the one move onto the new version", answer.ModelChanges)
	}

	if turn := invokeScriptedTurn(t, ctx, host, "session-tier-hot"); turn.Terminal != model.KindCompleted {
		t.Fatalf("second turn terminal = %v, want the completed turn of the published generation", turn.Terminal)
	}
	if served := provider.servedModels(); len(served) != 2 || served[1] != "model-b" {
		t.Fatalf("served models = %v, want the second request served by the installed model", served)
	}
	if !requestCarriesPrompt(provider, 1, publishedPromptMarker) {
		t.Fatalf("the request of the published generation carried no %q, want the declared prompt body", publishedPromptMarker)
	}
	if requestCarriesPrompt(provider, 1, bootPromptMarker) {
		t.Fatalf("the request of the published generation still carried %q, want the prompt source of the generation before replaced", bootPromptMarker)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestReloadRefusesTheFieldsOnlyBootReads moves one field per case whose only consumer Boot built:
//   - the exported entry answers the restart refusal by name, inside the structural family
//   - the refusal names the field path the operator searches the source for
//   - the reload endpoint answers the same refusal as a conflict
//   - nothing entered service: no lease published a version and the next turn is the boot model
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestReloadRefusesTheFieldsOnlyBootReads(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		change func(root, endpoint string) string
	}{
		{"tool declaration", "agents.user.tools", func(root, endpoint string) string {
			return reloadTierDocument(root, endpoint, "model-a", "", "    tools:\n      - {kind: tool, id: send}\n")
		}},
		{"skill roots", "agents.user.skill_dirs", func(root, endpoint string) string {
			return reloadTierDocument(root, endpoint, "model-a", "", "    skill_dirs: [\""+filepath.Join(root, "skills")+"\"]\n")
		}},
		{"read root", "agents.user.read_root", func(root, endpoint string) string {
			return reloadTierDocument(root, endpoint, "model-a", "", "    read_root: \""+root+"\"\n")
		}},
		{"file surface", "agents.user.file", func(root, endpoint string) string {
			return reloadTierDocument(root, endpoint, "model-a", "", "    file:\n      base_dir: \""+root+"\"\n")
		}},
		{"execution face", "agents.user.exec", func(root, endpoint string) string {
			return reloadTierDocument(root, endpoint, "model-a", "", "    exec:\n      monitor:\n        interval: 3s\n")
		}},
		{"memory section", "agents.user.memory", func(root, endpoint string) string {
			return reloadTierDocument(root, endpoint, "model-a", "", "    memory:\n      type: localfile\n      path: \""+filepath.Join(root, "store")+"\"\n")
		}},
		{"meditation section", "agents.user.meditation", func(root, endpoint string) string {
			return reloadTierDocument(root, endpoint, "model-a", "", "    meditation:\n      enabled: true\n      interval: 30m\n")
		}},
		{"governance rules", "governance", func(root, endpoint string) string {
			return reloadTierDocument(root, endpoint, "model-a", "", "", "governance:\n  risk_rules: []\n")
		}},
		{"observation face", "telemetry", func(root, endpoint string) string {
			return reloadTierDocument(root, endpoint, "model-a", "", "", "telemetry:\n  otel_enabled: true\n")
		}},
		{"request timeout", "request_timeout_seconds", func(root, endpoint string) string {
			return reloadTierDocument(root, endpoint, "model-a", "", "", "request_timeout_seconds: 42\n")
		}},
		{"storage root", "storage.root", func(root, endpoint string) string {
			return reloadTierDocument(filepath.Join(root, "moved"), endpoint, "model-a", "", "")
		}},
		{"storage tier", "storage.type", func(root, endpoint string) string {
			return strings.Replace(reloadTierDocument(root, endpoint, "model-a", "", ""), "type: localfile", "type: file", 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TAGENT_TEST_API_KEY", "test-key")
			t.Setenv("TAGENT_HTTP_PORT", "0")
			t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
			provider := newScriptedProvider(t)
			root := t.TempDir()
			configPath := filepath.Join(root, "tagent.yaml")
			writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", "", ""))
			host, err := assembly.Boot(configPath, assembly.WithSender(newRecordingSender()))
			if err != nil {
				t.Fatalf("boot the host: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if turn := invokeScriptedTurn(t, ctx, host, "session-tier-restart"); turn.Terminal != model.KindCompleted {
				t.Fatalf("first turn terminal = %v, want the completed turn of the boot generation", turn.Terminal)
			}

			writeHostDocumentFile(t, configPath, tc.change(root, provider.endpoint()))

			_, err = host.Reload(ctx)
			if !errors.Is(err, assembly.ErrHostReloadRestartRequired) {
				t.Fatalf("reload error = %v, want the restart refusal", err)
			}
			if !errors.Is(err, assembly.ErrHostReloadStructural) {
				t.Fatalf("reload error = %v, want the refusal answered inside the structural family", err)
			}
			if !strings.Contains(err.Error(), "requires a restart") {
				t.Fatalf("reload error = %v, want the restart named", err)
			}
			if !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("reload error = %v, want the field path %q named", err, tc.path)
			}

			status, body := postReloadAttempt(t, host.FaceAddr())
			if status != http.StatusConflict {
				t.Fatalf("reload status = %d, want 409: %s", status, body)
			}
			if !strings.Contains(body, tc.path) || !strings.Contains(body, "requires a restart") {
				t.Fatalf("reload refusal body = %q, want %q named beside the restart", body, tc.path)
			}
			if publications := host.Run().LeasePublications(); len(publications) != 0 {
				t.Fatalf("lease publications = %+v, want none after a refused reload", publications)
			}

			if turn := invokeScriptedTurn(t, ctx, host, "session-tier-restart"); turn.Terminal != model.KindCompleted {
				t.Fatalf("second turn terminal = %v, want the completed turn of the unchanged generation", turn.Terminal)
			}
			if served := provider.servedModels(); len(served) != 2 || served[0] != "model-a" || served[1] != "model-a" {
				t.Fatalf("served models = %v, want both turns served by the model the boot document named", served)
			}
			if err := host.Close(ctx); err != nil {
				t.Fatalf("close the host: %v", err)
			}
		})
	}
}

// TestReloadCarriesNoGenerationForAnUnchangedDocument posts the reload entry twice beside an unchanged document:
//   - both unchanged reloads answer no new generation and leave the boot generation where it was
//   - no lease publishes a version beside the carried document
//   - the moved model answers one new generation, exactly one past that number
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestReloadCarriesNoGenerationForAnUnchangedDocument(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", "", ""))
	host, err := assembly.Boot(configPath, assembly.WithSender(newRecordingSender()))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if turn := invokeScriptedTurn(t, ctx, host, "session-tier-same"); turn.Terminal != model.KindCompleted {
		t.Fatalf("first turn terminal = %v, want the completed turn of the boot generation", turn.Terminal)
	}

	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != http.StatusOK {
		t.Fatalf("first unchanged reload status = %d, want 200: %s", status, body)
	}
	carried := decodeReloadAnswer(t, body)
	if carried.Publication.Published {
		t.Fatalf("publication = %+v, want the unchanged document carried without a new generation", carried.Publication)
	}
	if len(carried.ModelChanges) != 0 {
		t.Fatalf("judged model changes = %+v, want none beside an unchanged document", carried.ModelChanges)
	}

	status, body = postReloadAttempt(t, host.FaceAddr())
	if status != http.StatusOK {
		t.Fatalf("second unchanged reload status = %d, want 200: %s", status, body)
	}
	again := decodeReloadAnswer(t, body)
	if again.Publication.Published {
		t.Fatalf("publication = %+v, want the second unchanged reload carried as well", again.Publication)
	}
	if again.Publication.Generation != carried.Publication.Generation {
		t.Fatalf("generation = %d after the second carried reload, want the %d the first one left", again.Publication.Generation, carried.Publication.Generation)
	}
	if publications := host.Run().LeasePublications(); len(publications) != 0 {
		t.Fatalf("lease publications = %+v, want none beside a carried document", publications)
	}

	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-b", "", ""))
	status, body = postReloadAttempt(t, host.FaceAddr())
	if status != http.StatusOK {
		t.Fatalf("reload of the moved model status = %d, want 200: %s", status, body)
	}
	moved := decodeReloadAnswer(t, body)
	if !moved.Publication.Published {
		t.Fatalf("publication = %+v, want the moved model published as a new generation", moved.Publication)
	}
	if moved.Publication.Generation != again.Publication.Generation+1 {
		t.Fatalf("generation = %d after the moved model, want exactly one past the %d it started from", moved.Publication.Generation, again.Publication.Generation)
	}

	if turn := invokeScriptedTurn(t, ctx, host, "session-tier-same"); turn.Terminal != model.KindCompleted {
		t.Fatalf("second turn terminal = %v, want the completed turn of the published generation", turn.Terminal)
	}
	if served := provider.servedModels(); len(served) != 2 || served[1] != "model-b" {
		t.Fatalf("served models = %v, want the second request served by the installed model", served)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// bootPromptMarker is the body the boot document declares as its prompt file.
const bootPromptMarker = "TIER-FIRST-PROMPT-BODY"

// publishedPromptMarker is the body the reloaded document declares as its prompt file.
const publishedPromptMarker = "TIER-SECOND-PROMPT-BODY"

// reloadTierDocument renders one document of the tiers over the scripted fixture:
// the entry agent carries the given block, the prompt directory is named when it is
// not empty, and the extra sections are appended as top-level keys.
func reloadTierDocument(root, endpoint, modelName, promptDir, agentBlock string, extra ...string) string {
	document := scriptedHostDocumentWithAgent(root, endpoint, modelName, agentBlock)
	if strings.TrimSpace(promptDir) != "" {
		document = "prompt_dir: \"" + promptDir + "\"\n" + document
	}
	for _, section := range extra {
		document += section
	}
	return document
}

// reloadPromptBlock renders the entry agent block that declares one prompt file.
func reloadPromptBlock(file string) string {
	return "    system_prompt:\n      files: [" + file + "]\n"
}

// writeReloadPrompt writes one prompt body under the prompt directory.
func writeReloadPrompt(t *testing.T, root, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write the prompt file %s: %v", name, err)
	}
}

// requestCarriesPrompt reports whether the request at the given ordinal carried the
// marker in any message, which is where a declared prompt body reaches the provider.
func requestCarriesPrompt(provider *scriptedProvider, index int, marker string) bool {
	requests := provider.servedRequests()
	if index < 0 || index >= len(requests) {
		return false
	}
	for _, message := range requests[index].Messages {
		if strings.Contains(message.Content, marker) {
			return true
		}
	}
	return false
}

// postReloadAttempt posts the reload endpoint on the host's loopback face and answers
// its status beside the body as written, so a publication and a refusal read the same way.
func postReloadAttempt(t *testing.T, faceAddr string) (int, string) {
	t.Helper()
	request, err := http.NewRequest("POST", fmt.Sprintf("http://%s/reload", faceAddr), nil)
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
	return resp.StatusCode, string(body)
}

// decodeReloadAnswer decodes one answered reload body into the shape a supervisor reads back.
func decodeReloadAnswer(t *testing.T, body string) reloadAnswer {
	t.Helper()
	var answer reloadAnswer
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		t.Fatalf("decode the reload answer %q: %v", body, err)
	}
	return answer
}
