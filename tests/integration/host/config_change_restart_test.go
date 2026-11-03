package host

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// TestConfigChangeSurvivesRestart keeps the configuration change fact as the ledger a restart reads.
//   - a prompt change files one config_change fact naming the publication generation
//   - the host is closed and a fresh host boots over the same storage root, so no live memory of the old process remains
//   - the fact reads back from the durable chain with identity, dimension, generation and body intact
//   - that is the account the publication record cannot offer after a restart
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestConfigChangeSurvivesRestart(t *testing.T) {
	viewAnchorEnv(t)
	const (
		bootBody = "CONFIG-CHANGE-RESTART-BOOT-PROMPT"
		nextBody = "CONFIG-CHANGE-RESTART-NEXT-PROMPT"
	)
	provider := newScriptedProvider(t)
	root := t.TempDir()
	writeReloadPrompt(t, root, "boot.md", bootBody)
	writeReloadPrompt(t, root, "next.md", nextBody)
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("boot.md")))
	host := bootViewAnchorHost(t, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("next.md")))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("reload status = %d, want 200: %s", status, body)
	}
	answer := decodeConfigChangeAnswer(t, body)
	if len(answer.ConfigChanges) != 1 {
		t.Fatalf("filed config changes = %+v, want the one prompt dimension", answer.ConfigChanges)
	}
	change := answer.ConfigChanges[0]
	wantGeneration := answer.Publication.Generation
	committed, err := host.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the committed config change before the restart: %v", err)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host before the restart: %v", err)
	}

	restarted := bootViewAnchorHost(t, configPath)
	defer func() {
		if err := restarted.Close(ctx); err != nil {
			t.Fatalf("close the restarted host: %v", err)
		}
	}()
	if count := restarted.Run().CommittedFacts(); count != 1 {
		t.Fatalf("restarted run holds %d fact(s), want the one config change restored from the durable chain", count)
	}
	restored, err := restarted.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the config change after the restart: %v", err)
	}
	if restored.Type != "config_change" {
		t.Fatalf("restored fact type = %q, want the frozen config_change type", restored.Type)
	}
	if restored.Key != committed.Key {
		t.Fatalf("restored fact key = %q, want the identity the old process committed %q", restored.Key, committed.Key)
	}
	if got := restored.Meta[event.MetaKeyConfigChangeDimension]; got != event.ConfigChangeDimensionPrompt {
		t.Fatalf("restored dimension = %q, want the prompt dimension", got)
	}
	if got, want := restored.Meta[event.MetaKeyConfigChangeGeneration], strconv.FormatUint(wantGeneration, 10); got != want {
		t.Fatalf("restored generation = %q, want the publication generation %s", got, want)
	}
	if restored.Message.Content != committed.Message.Content {
		t.Fatalf("restored body = %q, want the committed body %q", restored.Message.Content, committed.Message.Content)
	}
	if !strings.Contains(restored.Message.Content, "next.md") {
		t.Fatalf("restored body = %q, want the prompt change the fact reports", restored.Message.Content)
	}
}

// TestConfigChangeNeverReachesARequest keeps the configuration change fact out of every model request.
//   - the prompt change files a config_change fact that stays readable on the chain by its identity and body
//   - the next request carries the published prompt body, so the request was assembled from the new generation
//   - no request message carries the fact key, summary or body: the control-plane judgment never becomes transcript material
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestConfigChangeNeverReachesARequest(t *testing.T) {
	viewAnchorEnv(t)
	const nextBody = "CONFIG-CHANGE-NEVER-REQUEST-PROMPT"
	provider := newScriptedProvider(t)
	root := t.TempDir()
	writeReloadPrompt(t, root, "boot.md", "CONFIG-CHANGE-NEVER-REQUEST-BOOT")
	writeReloadPrompt(t, root, "next.md", nextBody)
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("boot.md")))
	host := bootViewAnchorHost(t, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("next.md")))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("reload status = %d, want 200: %s", status, body)
	}
	answer := decodeConfigChangeAnswer(t, body)
	if len(answer.ConfigChanges) != 1 {
		t.Fatalf("filed config changes = %+v, want the one prompt dimension", answer.ConfigChanges)
	}
	fact, err := host.Run().CommittedEvent(ctx, event.EventID(answer.ConfigChanges[0].Fact))
	if err != nil {
		t.Fatalf("read the committed config change fact: %v", err)
	}

	turn := invokeScriptedTurn(t, ctx, host, "session-config-change-request")
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn after the reload", turn.Terminal)
	}
	requests := provider.servedRequests()
	if len(requests) == 0 {
		t.Fatal("the turn served no model request, want one request whose material is inspected")
	}
	if !requestCarriesPrompt(provider, len(requests)-1, nextBody) {
		t.Fatalf("the request served after the reload does not carry the published prompt body %q", nextBody)
	}

	forbidden := []string{
		string(fact.Key),
		fact.Meta[event.MetaKeyEventSummary],
		fact.Message.Content,
		event.TypeConfigChange,
	}
	for at, request := range requests {
		for _, message := range request.Messages {
			for _, marker := range forbidden {
				if marker == "" {
					continue
				}
				if strings.Contains(message.Content, marker) {
					t.Fatalf("request %d carries the configuration change material %q in its %s message: the fact must never enter a model request", at, marker, message.Role)
				}
			}
		}
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}
