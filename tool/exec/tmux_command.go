// 契约: docs/wiki/tool/tool-architecture.md#action-tool
//
// The command face of the tmux executor: how one declaration is rendered as
// the shell line a pane runs, and how the exit record kept beside a session is
// prepared, read and cleared.
package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// commandLineWithDrain renders the declared command as a subshell whose status
// is re-raised after the drain window: a command that ends at once still leaves
// its output on the pane before the pane is reported dead, and a pane whose tmux
// lost the ending still leaves the status in the record beside the session.
func commandLineWithDrain(decl ExecutionDecl, record string) string {
	line := fmt.Sprintf("( %s ); code=$?; ", commandLine(decl))
	if record != "" {
		line += fmt.Sprintf("printf '%%d' \"$code\" > %s 2>/dev/null; ", shellQuote(record))
	}
	return line + fmt.Sprintf("sleep %s; exit $code",
		strconv.FormatFloat(tmuxDrainWindow.Seconds(), 'f', -1, 64))
}

// newExitRecord prepares the exit record of the named session: the record an
// earlier session under the same key left behind is cleared, so no stop reads a
// status this session did not write, and a record that cannot be prepared answers
// no path, so the session runs beside the status tmux keeps alone.
func newExitRecord(name string) string {
	path, err := exitRecordPath(name)
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ""
	}
	_ = os.Remove(path)
	return path
}

// exitRecordPath names the file one session's drain wrapper records its command's
// exit status in: the path is derived from the session name alone, so a stop in
// another process still finds the record of the session it stops, and the base is
// the user's own cache directory, so no other user writes a status this session
// reads.
func exitRecordPath(name string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "tagent", tmuxExitRecordDir, name), nil
}

// takeRecordedExitStatus reads and removes the exit status the drain wrapper of
// the named session recorded: a tmux that lost the pane's ending keeps a dead pane
// with no pane_dead_status, and the wrapper's record is the status that ending
// still carries. The record is removed whatever it holds, so a stop leaves no
// status beside a session that ended, and a record that is absent or unreadable
// answers the unobserved -1 rather than a status the session did not end with.
func takeRecordedExitStatus(name string) int {
	path, err := exitRecordPath(name)
	if err != nil {
		return -1
	}
	raw, readErr := os.ReadFile(path)
	_ = os.Remove(path)
	if readErr != nil {
		return -1
	}
	code, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if convErr != nil {
		return -1
	}
	return code
}

// clearExitRecord removes the exit record of the named session without reading
// it: a session that left needs no status kept beside it.
func clearExitRecord(name string) {
	path, err := exitRecordPath(name)
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

// commandLine renders one declaration as the shell command line tmux runs: the
// command and every argument are quoted for the shell, so an argument stays one
// word whatever it carries.
func commandLine(decl ExecutionDecl) string {
	parts := make([]string, 0, len(decl.Args)+1)
	parts = append(parts, shellQuote(decl.Command))
	for _, arg := range decl.Args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

// shellQuote renders one value as a single shell word: a value holding only
// safe characters stands as itself, and every other value is single-quoted with
// its embedded quotes escaped.
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if shellSafe(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// shellSafe reports whether value may stand as one unquoted shell word.
func shellSafe(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("-_./:=,@%+", r):
		default:
			return false
		}
	}
	return true
}
