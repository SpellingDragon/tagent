package action

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

func (te *TmuxExecutor) runResourceCommand(ctx context.Context, args []string) (string, error) {
	if te.run != nil {
		return te.run(ctx, args)
	}
	name, argv := te.buildTmuxCommand(args)
	out, err := exec.CommandContext(ctx, name, argv...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// SessionResourceID returns the tmux server and native session identity.
func (te *TmuxExecutor) SessionResourceID(sessionID string) (string, error) {
	out, err := te.runResourceCommand(context.Background(), []string{"display-message", "-p", "-t", "=" + sessionID, "#{pid}:#{session_id}"})
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.TrimSpace(out), ":")
	if len(parts) != 2 || parts[0] == "" || !strings.HasPrefix(parts[1], "$") {
		return "", fmt.Errorf("invalid tmux resource identity %q", out)
	}
	return out, nil
}

func (te *TmuxExecutor) resourceTarget(sessionID, resourceID string) (string, error) {
	if resourceID == "" {
		return "", fmt.Errorf("session %s has no resource identity", sessionID)
	}
	current, err := te.SessionResourceID(sessionID)
	if err != nil {
		return "", err
	}
	if current != resourceID {
		return "", fmt.Errorf("session %s resource changed: expected %s, found %s", sessionID, resourceID, current)
	}
	return strings.Split(resourceID, ":")[1], nil
}

func (te *TmuxExecutor) resourceStopped(sessionID, resourceID string) bool {
	current, err := te.SessionResourceID(sessionID)
	if err == nil {
		return current != resourceID || te.IsPaneDead(sessionID)
	}
	alive, known := te.SessionAlive3(sessionID)
	return known && !alive
}

func (te *TmuxExecutor) killResource(sessionID, resourceID string) error {
	target, err := te.resourceTarget(sessionID, resourceID)
	if err != nil {
		if te.resourceStopped(sessionID, resourceID) {
			return nil
		}
		return err
	}
	_, err = te.runResourceCommand(context.Background(), []string{"if-shell", "-F", "-t", target,
		"#{==:#{pid}:#{session_id}," + resourceID + "}", "kill-session -t '" + target + "'",
		"display-message -p 'resource changed'"})
	if err != nil {
		return err
	}
	if !te.resourceStopped(sessionID, resourceID) {
		return fmt.Errorf("session %s termination is unconfirmed", sessionID)
	}
	return nil
}

// PrepareSession reserves a session without executing the requested command.
func (te *TmuxExecutor) PrepareSession(ctx context.Context, opts TmuxCreateOptions) (*TmuxSession, error) {
	command := opts.Command
	placeholder := "exec sleep 2147483647 # tagent-prepare-" + uuid.NewString()
	opts.Command = placeholder
	session, err := te.CreateSession(ctx, opts)
	if err != nil {
		return nil, err
	}
	session.Command, session.Placeholder, session.Phase = command, placeholder, "prepared"
	session.PipeFile = te.resourcePipePath(session.ID, session.ResourceID)
	if _, err := te.runResourceCommand(ctx, []string{"pipe-pane", "-t", strings.Split(session.ResourceID, ":")[1], "cat >> " + session.PipeFile}); err != nil {
		_ = te.killResource(session.ID, session.ResourceID)
		return nil, fmt.Errorf("prepare output capture: %w", err)
	}
	return session, nil
}

func (te *TmuxExecutor) resourcePipePath(sessionID, resourceID string) string {
	identity := strings.NewReplacer(":", "-", "$", "").Replace(resourceID)
	return filepath.Join(os.TempDir(), "tagent-pipe-"+sessionID+"-"+identity+".log")
}

func (te *TmuxExecutor) resourceOutput(sessionID, resourceID string) (string, error) {
	if b, err := os.ReadFile(te.resourcePipePath(sessionID, resourceID)); err == nil && len(b) > 0 {
		return string(b), nil
	}
	if _, err := te.resourceTarget(sessionID, resourceID); err != nil {
		return "", err
	}
	return te.GetSessionOutput(sessionID)
}

func (te *TmuxExecutor) activateResource(sessionID, resourceID, placeholder, command string) error {
	target, err := te.resourceTarget(sessionID, resourceID)
	if err != nil {
		return err
	}
	actual, err := te.runResourceCommand(context.Background(), []string{"display-message", "-p", "-t", target, "#{pane_start_command}"})
	if err != nil {
		return err
	}
	if actual != placeholder {
		return nil
	}
	return te.resourceMutation(target, resourceID, []string{"respawn-pane", "-k", "-t", target, command})
}

func (te *TmuxExecutor) sendResource(sessionID, resourceID, keys string) error {
	target, err := te.resourceTarget(sessionID, resourceID)
	if err != nil {
		return err
	}
	return te.resourceMutation(target, resourceID, []string{"send-keys", "-l", "-t", target, keys})
}

func tmuxQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func (te *TmuxExecutor) resourceMutation(target, resourceID string, args []string) error {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = tmuxQuote(arg)
	}
	out, err := te.runResourceCommand(context.Background(), []string{"if-shell", "-F", "-t", target,
		"#{==:#{pid}:#{session_id}," + resourceID + "}", strings.Join(quoted, " "), "display-message -p tagent-resource-changed"})
	if err == nil && strings.Contains(out, "tagent-resource-changed") {
		return fmt.Errorf("tmux resource %s changed during mutation", resourceID)
	}
	return err
}
