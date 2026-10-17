// 契约: docs/wiki/platform/platform-subsystems.md#testing-helpers
package testutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// RequireTmux checks real session creation; TAGENT_REQUIRE_TMUX makes unavailability fatal.
func RequireTmux(t testing.TB) {
	t.Helper()
	unavailable := func(detail string) {
		t.Helper()
		if os.Getenv("TAGENT_REQUIRE_TMUX") == "1" {
			t.Fatal(detail)
		}
		t.Skip(detail)
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		unavailable(fmt.Sprintf("tmux is unavailable: %v", err))
		return
	}
	session := fmt.Sprintf("tagent-test-probe-%d-%d", os.Getpid(), time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", session, "sleep 30").CombinedOutput()
	if err != nil {
		unavailable(fmt.Sprintf("tmux cannot create a session in this environment: %v: %s", err, output))
		return
	}
	ctx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCleanup()
	if output, err := exec.CommandContext(ctx, "tmux", "kill-session", "-t", "="+session).CombinedOutput(); err != nil {
		t.Fatalf("tmux probe cleanup failed: %v: %s", err, output)
	}
}
