package host

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/assembly"
)

// TestReloadRefusesRestartOnlySectionsOfAnAddedAgent pins the boundary of the added-agent hot path.
//   - an added agent is constructed through the generation-scoped consumers, so its tool, skill, read, file and exec sections take effect with the generation
//   - its meditation section takes effect the same way
//   - the memory tier is opened once for the process: a generation naming a different tier is refused by name in the restart family
//   - carrying such a section on an added agent would answer success for a value nothing reads, so the reload refuses it by name
//   - the endpoint answers 409 and no configuration fact lands
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestReloadRefusesRestartOnlySectionsOfAnAddedAgent(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		block func(root string) string
	}{
		{"memory tier", "agents.other.memory", func(root string) string {
			return "    memory:\n      type: localfile\n      path: \"" +
				filepath.Join(root, "other-memory") + "\"\n"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			viewAnchorEnv(t)
			provider := newScriptedProvider(t)
			root := t.TempDir()
			configPath := filepath.Join(root, "tagent.yaml")
			writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", "", ""))
			host := bootViewAnchorHost(t, configPath)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			before := host.Run().CommittedFacts()

			writeHostDocumentFile(t, configPath, addedAgentSectionDocument(root, provider.endpoint(), tc.block(root)))

			_, err := host.Reload(ctx)
			if !errors.Is(err, assembly.ErrHostReloadRestartRequired) {
				t.Fatalf("reload error = %v, want the restart-required refusal", err)
			}
			if !errors.Is(err, assembly.ErrHostReloadStructural) {
				t.Fatalf("reload error = %v, want the refusal answered inside the structural family", err)
			}
			if !strings.Contains(err.Error(), "requires a restart") {
				t.Fatalf("reload error = %v, want the restart named", err)
			}
			if !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("reload error = %v, want the added agent's field path %q named", err, tc.path)
			}

			status, body := postReloadAttempt(t, host.FaceAddr())
			if status != http.StatusConflict {
				t.Fatalf("reload status = %d, want 409: %s", status, body)
			}
			if !strings.Contains(body, tc.path) || !strings.Contains(body, "requires a restart") {
				t.Fatalf("reload refusal body = %q, want %q named beside the restart", body, tc.path)
			}
			if after := host.Run().CommittedFacts(); after != before {
				t.Fatalf("committed facts = %d after a refused reload, want the %d before it", after, before)
			}
			if publications := host.Run().LeasePublications(); len(publications) != 0 {
				t.Fatalf("lease publications = %+v, want none after a refused reload", publications)
			}
			if err := host.Close(ctx); err != nil {
				t.Fatalf("close the host: %v", err)
			}
		})
	}
}

// addedAgentSectionDocument renders the boot document with the second agent
// declared and the given section block already indented under it.
func addedAgentSectionDocument(root, endpoint, block string) string {
	document := structuralScriptedHostDocument(root, endpoint)
	return strings.Replace(document, "  other: {}\n", "  other:\n"+block, 1)
}

// TestReloadAddedAgentMeditationFailsClosedByValidation pins that meditation is generation-scoped.
//   - an added agent's invalid meditation section refuses the candidate fail-closed by its own validation, not by the restart family
//   - no publication lands
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
func TestReloadAddedAgentMeditationFailsClosedByValidation(t *testing.T) {
	viewAnchorEnv(t)
	provider := newScriptedProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", "", ""))
	host := bootViewAnchorHost(t, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	before := host.Run().CommittedFacts()

	block := "    meditation:\n      enabled: true\n      interval: 30m\n"
	writeHostDocumentFile(t, configPath, addedAgentSectionDocument(root, provider.endpoint(), block))

	_, err := host.Reload(ctx)
	if err == nil {
		t.Fatalf("reload of an added agent whose meditation names no prompt_file succeeded, want a fail-closed validation refusal")
	}
	if errors.Is(err, assembly.ErrHostReloadRestartRequired) {
		t.Fatalf("reload error = %v, want the meditation validation refusal not the restart family (meditation is generation-scoped)", err)
	}
	if !strings.Contains(err.Error(), "prompt_file") {
		t.Fatalf("reload error = %v, want the missing prompt_file named", err)
	}
	if after := host.Run().CommittedFacts(); after != before {
		t.Fatalf("committed facts = %d after a fail-closed reload, want the %d before it", after, before)
	}
	if publications := host.Run().LeasePublications(); len(publications) != 0 {
		t.Fatalf("lease publications = %+v, want none after a fail-closed reload", publications)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}
