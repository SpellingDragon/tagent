// 契约: docs/wiki/tool/tool-architecture.md#action-tool
//
// The session face of the tmux executor: the session name one resource key
// names, and the reads of the tmux session list that Start, Probe and Stop
// base their verdicts on.
package exec

import (
	"context"
	"fmt"
	"strings"
)

// sessionNames lists the session names tmux reports, with a server that is not
// running read as an empty list: a refusal is retried at the server cadence,
// so a busy server's refusal or a server that left mid-read is never read as an
// empty list before the retries pass.
func (e *TmuxExecutor) sessionNames(ctx context.Context) ([]string, error) {
	for attempt := 1; ; attempt++ {
		out, err := e.tmux(ctx, "list-sessions", "-F", "#{session_name}")
		if err == nil {
			lines := strings.Split(out, "\n")
			names := make([]string, 0, len(lines))
			for _, line := range lines {
				if name := strings.TrimSpace(line); name != "" {
					names = append(names, name)
				}
			}
			return names, nil
		}
		if !tmuxServerAbsent(err) {
			return nil, err
		}
		if attempt >= tmuxServerAttempts || !sleepContext(ctx, tmuxServerRetryGap) {
			return nil, nil
		}
	}
}

// sessionExists reports whether one named session is present in the session list.
func (e *TmuxExecutor) sessionExists(ctx context.Context, name string) (bool, error) {
	names, err := e.sessionNames(ctx)
	if err != nil {
		return false, err
	}
	return containsName(names, name), nil
}

// sessionAbsent reports whether one named session left the session list.
func (e *TmuxExecutor) sessionAbsent(ctx context.Context, name string) (bool, error) {
	names, err := e.sessionNames(ctx)
	if err != nil {
		return false, err
	}
	return !containsName(names, name), nil
}

// sessionNameOf reports the tmux session name one resource key names: an empty
// kind selects the tmux family, any other kind is refused, and the key may carry
// only characters a session name holds, so it never reaches tmux as more than one
// word.
func sessionNameOf(key ResourceKey) (string, error) {
	if key.Kind != "" && key.Kind != KindTmux {
		return "", fmt.Errorf("exec: the tmux executor cannot serve resource kind %q", key.Kind)
	}
	if !validSessionName(key.Key) {
		return "", fmt.Errorf("%w: %q", errSessionNameInvalid, key.Key)
	}
	return key.Key, nil
}

// validSessionName reports whether name is a non-empty session name of at most
// tmuxSessionMaxName characters drawn from letters, digits, hyphens and
// underscores.
func validSessionName(name string) bool {
	if name == "" || len(name) > tmuxSessionMaxName {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
