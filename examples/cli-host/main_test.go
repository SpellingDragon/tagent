package main

import (
	"bytes"
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
