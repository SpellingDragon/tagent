package action

import (
	"testing"
)

// ==================== Task 3.6: runAsUser 非空时 tmux 命令带 sudo 前缀 ====================

func TestBuildTmuxCommand_WithRunAsUser(t *testing.T) {
	te := NewTmuxExecutor(
		WithTmuxRunAsUser("agent-runner"),
		WithTmuxRunAsGroup("agent-group"),
	)

	cmd, args := te.buildTmuxCommand([]string{"new-session", "-d", "-s", "test"})

	if cmd != "sudo" {
		t.Fatalf("expected command 'sudo', got %q", cmd)
	}

	expected := []string{"-n", "-u", "agent-runner", "-g", "agent-group", "tmux", "new-session", "-d", "-s", "test"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg[%d]: expected %q, got %q", i, v, args[i])
		}
	}
}

func TestBuildTmuxCommand_WithRunAsUserNoGroup(t *testing.T) {
	te := NewTmuxExecutor(
		WithTmuxRunAsUser("agent-runner"),
	)

	cmd, args := te.buildTmuxCommand([]string{"kill-session", "-t", "sess1"})

	if cmd != "sudo" {
		t.Fatalf("expected command 'sudo', got %q", cmd)
	}

	expected := []string{"-n", "-u", "agent-runner", "tmux", "kill-session", "-t", "sess1"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg[%d]: expected %q, got %q", i, v, args[i])
		}
	}
}

// ==================== Task 3.7: runAsUser 为空时 tmux 命令不带 sudo ====================

func TestBuildTmuxCommand_WithoutRunAsUser(t *testing.T) {
	te := NewTmuxExecutor()

	cmd, args := te.buildTmuxCommand([]string{"new-session", "-d", "-s", "test"})

	if cmd != "tmux" {
		t.Fatalf("expected command 'tmux', got %q", cmd)
	}

	expected := []string{"new-session", "-d", "-s", "test"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg[%d]: expected %q, got %q", i, v, args[i])
		}
	}
}

func TestBuildTmuxCommand_BackwardCompat_AllMethods(t *testing.T) {
	// Verify that a default TmuxExecutor (no runAsUser) produces plain tmux commands
	// for all method types — ensuring backward compatibility.
	te := NewTmuxExecutor()

	cases := []struct {
		name string
		args []string
	}{
		{"new-session", []string{"new-session", "-d", "-s", "s1", "-c", "/tmp", "echo hi"}},
		{"kill-session", []string{"kill-session", "-t", "s1"}},
		{"has-session", []string{"has-session", "-t", "s1"}},
		{"capture-pane", []string{"capture-pane", "-p", "-t", "s1"}},
		{"display-message", []string{"display-message", "-p", "-t", "s1", "#{pane_pid}"}},
		{"send-keys", []string{"send-keys", "-t", "s1", "echo test"}},
		{"list-sessions", []string{"list-sessions", "-F", "#{session_name}"}},
		{"set-environment", []string{"set-environment", "-t", "s1", "FOO", "bar"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, args := te.buildTmuxCommand(tc.args)
			if cmd != "tmux" {
				t.Errorf("expected 'tmux', got %q", cmd)
			}
			if len(args) != len(tc.args) {
				t.Errorf("expected %d args, got %d", len(tc.args), len(args))
			}
		})
	}
}

// ==================== Task 3.8: opts.Env 被正确传递到 tmux set-environment 命令 ====================

func TestBuildTmuxCommand_SetEnvironmentWithSudo(t *testing.T) {
	// When runAsUser is set, set-environment commands must also go through sudo
	// to target the correct user's tmux server.
	te := NewTmuxExecutor(
		WithTmuxRunAsUser("agent-runner"),
	)

	envVars := map[string]string{
		"PATH":     "/usr/local/bin:/usr/bin",
		"HOME":     "/home/agent-runner",
		"NODE_ENV": "production",
	}

	for k, v := range envVars {
		args := []string{"set-environment", "-t", "test-session", k, v}
		cmd, cmdArgs := te.buildTmuxCommand(args)

		if cmd != "sudo" {
			t.Errorf("expected 'sudo' for env %s, got %q", k, cmd)
		}

		// Verify the env key and value are in the args
		found := false
		for i, a := range cmdArgs {
			if a == k && i+1 < len(cmdArgs) && cmdArgs[i+1] == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("env var %s=%s not found in command args: %v", k, v, cmdArgs)
		}
	}
}

func TestNewActionTool_PassesRunAsUserToTmuxExecutor(t *testing.T) {
	// Verify that NewActionTool passes runAsUser/runAsGroup/workspace
	// to the TmuxExecutor (task 3.1 integration check).
	ct := NewActionTool(
		WithActionRunAsUser("testuser"),
		WithActionRunAsGroup("testgroup"),
		WithActionWorkspace("/tmp/test-ws"),
	)

	if ct.tmuxExecutor == nil {
		t.Skip("tmux not available, skipping")
	}

	if ct.tmuxExecutor.runAsUser != "testuser" {
		t.Errorf("expected tmuxExecutor.runAsUser='testuser', got %q", ct.tmuxExecutor.runAsUser)
	}
	if ct.tmuxExecutor.runAsGroup != "testgroup" {
		t.Errorf("expected tmuxExecutor.runAsGroup='testgroup', got %q", ct.tmuxExecutor.runAsGroup)
	}
	if ct.tmuxExecutor.workspace != "/tmp/test-ws" {
		t.Errorf("expected tmuxExecutor.workspace='/tmp/test-ws', got %q", ct.tmuxExecutor.workspace)
	}

	// Verify the executor produces sudo-wrapped commands
	cmd, _ := ct.tmuxExecutor.buildTmuxCommand([]string{"new-session"})
	if cmd != "sudo" {
		t.Errorf("expected sudo-wrapped command, got %q", cmd)
	}
}
