package host

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
)

// TestBootMountsCapture boots a host over a storage root and reads the capture it wrote:
//   - the turns complete against the scripted provider
//   - no writer seal exists while the run serves, so the per-turn seal stays a no-op
//   - <root>/trajectories/attempts.jsonl holds the attempt of every turn's decision
//   - every stored line parses as the versioned attempt record the sink writes
//   - the sealed trajectory reads back complete under the boot capture identity
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestBootMountsCapture(t *testing.T) {
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
	for _, content := range []string{"hello", "hello again"} {
		turn, err := host.Invoke(ctx, tagent.InvokeRequest{
			Address: event.Address{AgentID: "user", SessionID: "session-capture"},
			Input:   []model.Message{{Role: model.RoleUser, Content: content}},
		})
		if err != nil {
			t.Fatalf("invoke the booted host with %q: %v", content, err)
		}
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("turn %q terminal = %v, want the completed turn of the scripted answer", content, turn.Terminal)
		}
	}

	dir := filepath.Join(root, "trajectories")
	beforeClose, err := rl.ReadCaptureSeal(dir)
	if err != nil {
		t.Fatalf("read the capture seal before the close: %v", err)
	}
	if beforeClose.Sealed {
		t.Fatalf("capture seal before the close = %+v, want the run-wide account still open", beforeClose)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
	if err != nil {
		t.Fatalf("read the boot capture record file: %v", err)
	}
	lines := 0
	for _, raw := range bytes.Split(content, []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		line, err := rl.ParseLine(raw)
		if err != nil {
			t.Fatalf("parse the stored record %q: %v", raw, err)
		}
		if line.Kind != rl.RecordKindAttempt || line.Attempt == nil {
			t.Fatalf("stored record kind = %q, want the attempt line the sink writes", line.Kind)
		}
		lines++
	}
	if lines != 2 {
		t.Fatalf("stored record lines = %d, want the attempt of each turn's decision under one sink", lines)
	}

	manifest, err := rl.ReadCaptureSeal(dir)
	if err != nil {
		t.Fatalf("read the boot capture seal: %v", err)
	}
	if !manifest.Sealed || manifest.Partial {
		t.Fatalf("manifest = %+v, want the writer-confirmed seal of a complete capture", manifest)
	}
	if manifest.CaptureID != "host-trajectory" {
		t.Fatalf("manifest capture id = %q, want the boot capture identity", manifest.CaptureID)
	}
	if manifest.Records != int64(lines) || manifest.Dropped != 0 {
		t.Fatalf("manifest = %+v, want the %d stored records and no loss", manifest, lines)
	}
	if len(manifest.Files) == 0 || manifest.Files[0].Name != rl.SinkRecordsFile || manifest.Files[0].Records < 1 {
		t.Fatalf("manifest files = %+v, want the sealed record file entry", manifest.Files)
	}
}
