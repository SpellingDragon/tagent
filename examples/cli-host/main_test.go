package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestCLISingleShotMockModel drives one zero-credential -c input through the assembly entry to a mock reply on stdout.
// 契约: docs/OPERATION.md#cli-host-test
func TestCLISingleShotMockModel(t *testing.T) {
	t.Setenv("TAGENT_MODEL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("TAGENT_API_KEY_ENV", "")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-c", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %s", code, stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), "mock reply: hello"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// TestEventIntegrityCLIAdapter pins two CLI-host guarantees: a credential-free stdin turn reaches stdout, and a model without its key fails closed.
// 契约: docs/OPERATION.md#cli-host-test
func TestEventIntegrityCLIAdapter(t *testing.T) {
	t.Run("formal_cli_turn", func(t *testing.T) {
		t.Setenv("TAGENT_MODEL", "")
		t.Setenv("TAGENT_BASE_URL", "")
		t.Setenv("TAGENT_API_KEY_ENV", "")
		t.Setenv("TAGENT_SESSION_ID", "")
		t.Setenv("OPENAI_API_KEY", "")

		var stdout, stderr bytes.Buffer
		code := run(nil, strings.NewReader("hello from stdin\n"), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("run exit = %d, stderr = %s", code, stderr.String())
		}
		if got := stderr.String(); !strings.Contains(got, "> ") {
			t.Fatalf("stderr = %q, want the repl prompt of the stdin path", got)
		}
		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		if len(lines) != 1 {
			t.Fatalf("stdout = %q, want exactly one assistant turn line", stdout.String())
		}
		if got, want := strings.TrimSpace(lines[0]), "mock reply: hello from stdin"; got != want {
			t.Fatalf("stdout line = %q, want %q", got, want)
		}
	})

	t.Run("missing_model_key_fails_closed", func(t *testing.T) {
		const (
			modelName = "gpt-4.1-mini"
			keyEnv    = "TAGENT_EVENT_INTEGRITY_MISSING_KEY"
		)
		t.Setenv("TAGENT_MODEL", "")
		t.Setenv("TAGENT_BASE_URL", "")
		t.Setenv("OPENAI_API_KEY", "")
		t.Setenv(keyEnv, "")

		var stdout, stderr bytes.Buffer
		code := run(
			[]string{"-model", modelName, "-api-key-env", keyEnv},
			strings.NewReader("hello from stdin\n"),
			&stdout, &stderr,
		)
		if code != 1 {
			t.Fatalf("run exit = %d, want 1; stderr = %s", code, stderr.String())
		}
		want := fmt.Sprintf("model %q needs a key in $%s; use -model mock for the credential-free path", modelName, keyEnv)
		if got := stderr.String(); !strings.Contains(got, want) {
			t.Fatalf("stderr = %q, want the named missing-key error %q", got, want)
		}
		if got := stdout.String(); got != "" {
			t.Fatalf("stdout = %q, want no assistant body when the key is missing", got)
		}
		if strings.Contains(stdout.String(), "mock reply") {
			t.Fatalf("stdout = %q, want no mock fallback", stdout.String())
		}
	})
}
