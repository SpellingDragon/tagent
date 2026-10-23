package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
)

// TestHostBootWiresTheMeditationDriver boots a host whose meditation session carries deliver_summary, then closes it:
// - the boot accepts the document and the run serves an empty active-session set
// - Close stops the driver and returns no error
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestHostBootWiresTheMeditationDriver(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	root := t.TempDir()
	configPath := writeMeditationHostDocument(t, root, []string{"send"}, []string{"deliver_summary"})
	host, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if host.FaceAddr() == "" {
		t.Fatal("the host face listens on no address")
	}
	if host.Run() == nil {
		t.Fatal("the host drives no run")
	}
	if sessions := host.Run().ActiveSessions("user"); len(sessions) != 0 {
		t.Fatalf("active user sessions = %+v, want none before the first turn", sessions)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestHostBootRefusesMisplacedMeditationTools checks that a tool granted to the wrong agent refuses the boot by name:
// - deliver_summary outside the meditation session is refused
// - send on the meditation session is refused
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestHostBootRefusesMisplacedMeditationTools(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	cases := []struct {
		name            string
		userTools       []string
		meditationTools []string
		want            string
	}{
		{"summary outside the meditation session", []string{"send", "deliver_summary"}, []string{"deliver_summary"}, "deliver_summary"},
		{"send on the meditation session", []string{"send"}, []string{"deliver_summary", "send"}, "send"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			configPath := writeMeditationHostDocument(t, root, tc.userTools, tc.meditationTools)
			host, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
			if host != nil {
				t.Fatal("a refused boot returned a host")
			}
			if !errors.Is(err, tagent.ErrHostMeditationToolMisplaced) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("boot error = %v, want the misplaced %q refusal", err, tc.want)
			}
		})
	}
}

// writeMeditationHostDocument writes one host document over the temp root,
// granting the named tools to the entry agent and to the meditation session,
// and answers its path. The meditation prompt the plan reads is written beside
// it.
func writeMeditationHostDocument(t *testing.T, root string, userTools, meditationTools []string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: scripted\n")
	b.WriteString("prompt_dir: " + root + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"http://127.0.0.1:1/v1\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  user:\n")
	b.WriteString("    tools:\n")
	for _, name := range userTools {
		b.WriteString("      - { kind: tool, id: " + name + " }\n")
	}
	b.WriteString("  meditator:\n")
	b.WriteString("    tools:\n")
	for _, name := range meditationTools {
		b.WriteString("      - { kind: tool, id: " + name + " }\n")
	}
	b.WriteString("    memory: { type: localfile, path: " + root + ", read_namespaces: [user] }\n")
	b.WriteString("    meditation:\n")
	b.WriteString("      enabled: true\n")
	b.WriteString("      interval: 1h\n")
	b.WriteString("      min_gap: 2h\n")
	b.WriteString("      prompt_file: meditation.md\n")
	b.WriteString("      observed_namespaces: [user]\n")
	b.WriteString("      deliver_to: [user]\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "meditation.md"), []byte("reflect on the session"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	return configPath
}

// recordingSender is the outbound message stand-in of the smoke check: it
// accepts every body and records nothing.
type recordingSender struct{}

// Send accepts one body.
func (recordingSender) Send(context.Context, event.Address, string) error { return nil }
