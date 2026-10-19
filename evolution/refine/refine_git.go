package refine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrNotGitRepository and the other named failures of the git-native refine surface are reachable
// through errors.Is over the Unwrap chain rather than rendered text, so a caller reads the refusal it
// needs without matching messages.
var (
	// ErrNotGitRepository reports a refine operation whose run directory is not
	// a git work tree. The file edit itself already took effect on disk, so the
	// registration degrades to a plain named failure and never blocks the edit.
	ErrNotGitRepository = errors.New("evolution: refine needs a git repository (改进登记需 git 仓)")

	// ErrGitOperationInProgress reports a refine mutation refused because a git
	// revert or merge is still open in the repository. Registering during an
	// open revert would commit through it and swallow the sequencer state, so
	// the registration stops before any git command mutates the repository.
	ErrGitOperationInProgress = errors.New("evolution: refine refuses to mutate while a git revert or merge is open")

	// ErrImprovementUnrecorded reports a registration whose commit landed but
	// whose improvement event could not be recorded. The commit stays, because
	// its SHA is a correlation only; the unrecorded event is reported by name.
	ErrImprovementUnrecorded = errors.New("evolution: the registration committed but its improvement event was not recorded")

	// ErrImprovementUnknown reports a rollback naming a commit that does not
	// resolve to any commit in the repository.
	ErrImprovementUnknown = errors.New("evolution: no commit resolves to the named improvement")

	// ErrCommitNotImprovement reports a rollback naming a commit that carries no
	// improvement marker. A user's own commit is never reverted, so the rollback
	// is refused before git is touched.
	ErrCommitNotImprovement = errors.New("evolution: only a commit registered under the improvement marker may be rolled back")

	// ErrRevertConflict reports a rollback whose revert stopped on a conflict.
	// The git output and the disposition guidance travel back to the agent, and
	// the repository keeps the open revert for the agent to resolve or abort.
	ErrRevertConflict = errors.New("evolution: the revert left a conflict for the agent to resolve")
)

// RevertConflictError is the named failure of a rollback whose revert stopped on
// a conflict: the commit it tried to revert and the git output carrying the
// conflict. Unwrap exposes ErrRevertConflict. The repository is left in its open
// revert state, so the agent resolves the conflict and runs `git revert
// --continue`, or abandons it with `git revert --abort`; the framework never
// resolves or aborts a revert on its own.
type RevertConflictError struct {
	// SHA is the resolved commit the revert tried to undo.
	SHA string
	// Output is the git output naming the conflict and the disposition steps.
	Output string
}

// Error renders the reverted commit and the git output guiding the resolution.
func (e *RevertConflictError) Error() string {
	return fmt.Sprintf("evolution: reverting %s left a conflict; resolve it then run `git revert --continue`, or run `git revert --abort` to abandon:\n%s", e.SHA, e.Output)
}

// Unwrap exposes the named conflict underneath the rendered message.
func (e *RevertConflictError) Unwrap() error {
	return ErrRevertConflict
}

// gitFace runs git in the controlled repository under the refine surface. Every
// call runs with its own injected identity and an explicit argv, never a shell
// string, so a bare environment without a global git config still commits and no
// path is ever word-split.
type gitFace struct {
	root string
}

// run executes one git command in the repository root and returns its combined
// output. Identity is injected locally per call rather than read from the user's
// global git config.
func (g gitFace) run(ctx context.Context, args ...string) (string, error) {
	argv := append([]string{
		"-c", "user.name=tagent",
		"-c", "user.email=tagent@localhost",
	}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Dir = g.root
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// 契约: docs/wiki/evolution/evolution-architecture.md#refine
//
// GitRefine is the git-native refinement surface over one controlled
// repository: it records an explicit set of controlled files as one improvement
// commit, lists the improvement ledger, and safely reverts one marked commit.
// It holds no version store of its own — git is the version layer and the files
// on disk are the source of truth, so an edit takes effect the moment it lands
// and this surface only leaves a trace, an evaluation hook and a safe undo.
type GitRefine struct {
	git       gitFace
	root      string
	protected []string
	record    ImprovementRecorder
}

// NewGitRefine returns the refinement surface over the controlled repository at
// root. It records each successful registration through the injected recorder,
// which is optional, and admits only paths under the controlled patterns — the
// defaults when none are supplied. Every git command runs in root, so the
// surface never reaches any repository but the one it is pointed at.
func NewGitRefine(root string, record ImprovementRecorder, protected []string) *GitRefine {
	if len(protected) == 0 {
		protected = DefaultProtectedPaths()
	}
	return &GitRefine{git: gitFace{root: root}, root: root, protected: protected, record: record}
}

// GitRefine is satisfied by the refinement surface.
var _ Refine = (*GitRefine)(nil)

// Register records the explicitly listed controlled files as one improvement.
// It stages exactly those paths and commits only them under the improvement
// marker, leaving the rest of the working tree and anything the user already
// staged untouched. A path outside the controlled set refuses the whole
// registration before git is touched.
//
// - A commit that finds nothing to commit is a normal outcome, not a failure: the files already hold the content the registration would record.
func (g *GitRefine) Register(ctx context.Context, paths []string, note string) (RefineRecord, error) {
	normalized, err := g.admit(paths)
	if err != nil {
		return RefineRecord{}, err
	}
	if err := g.repositoryReady(ctx); err != nil {
		return RefineRecord{}, err
	}
	if err := g.noOperationInProgress(ctx); err != nil {
		return RefineRecord{}, err
	}
	if _, err := g.git.run(ctx, append([]string{"add", "--"}, normalized...)...); err != nil {
		return RefineRecord{}, fmt.Errorf("evolution: refine register staged %v: %w", normalized, err)
	}
	subject := ImprovementMarker + " register"
	body, err := commitBody(note, normalized)
	if err != nil {
		return RefineRecord{}, err
	}
	output, err := g.git.run(ctx, append([]string{"commit", "--only", "-m", subject, "-m", body, "--"}, normalized...)...)
	if err != nil {
		if isNothingToCommit(output) {
			return RefineRecord{Paths: normalized, Note: note}, nil
		}
		return RefineRecord{}, fmt.Errorf("evolution: refine register committed %v: %w", normalized, err)
	}
	sha, committedAt, err := g.headCommit(ctx)
	if err != nil {
		return RefineRecord{}, err
	}
	record := RefineRecord{Paths: normalized, Note: note, SHA: sha, CommittedAt: committedAt}
	if g.record != nil {
		if err := g.record(ctx, Improvement{SHA: sha, Paths: normalized, Note: note}); err != nil {
			return record, fmt.Errorf("%w: %w", ErrImprovementUnrecorded, err)
		}
	}
	return record, nil
}

// Status lists the improvement ledger: every commit whose subject opens with the
// improvement marker, newest first, with the note and controlled paths each
// registration carried and the moment its commit landed. A commit without the
// marker — including the reverts git writes back — never enters the ledger. The
// evaluation window and post-hoc report stay zero here and are filled by the
// post-activation evaluation; a zero record is never read as healthy.
func (g *GitRefine) Status(ctx context.Context) ([]RefineRecord, error) {
	if err := g.repositoryReady(ctx); err != nil {
		return nil, err
	}
	output, err := g.git.run(ctx, "log", "--grep="+regexp.QuoteMeta(ImprovementMarker), "--format=%H%x00%ct%x00%B%x1e")
	if err != nil {
		if isNoCommits(output) {
			return []RefineRecord{}, nil
		}
		return nil, fmt.Errorf("evolution: refine read the improvement ledger: %w", err)
	}
	ledger := []RefineRecord{}
	for _, chunk := range strings.Split(output, "\x1e") {
		record, ok := g.parseEntry(ctx, chunk)
		if ok {
			ledger = append(ledger, record)
		}
	}
	return ledger, nil
}

// Rollback reverts one recorded improvement named by its full commit or an
// unambiguous prefix. A commit that does not carry the improvement marker is
// refused before git is touched, so a user's own commit is never reverted. A
// revert that stops on a conflict returns the git output and disposition
// guidance and leaves the revert open for the agent; reverting is a terminal act
// of the agent and never of the framework.
func (g *GitRefine) Rollback(ctx context.Context, sha string) error {
	if err := g.repositoryReady(ctx); err != nil {
		return err
	}
	resolved, err := g.resolveCommit(ctx, sha)
	if err != nil {
		return err
	}
	subject, err := g.commitSubject(ctx, resolved)
	if err != nil {
		return err
	}
	if !isImprovementSubject(subject) {
		return fmt.Errorf("%w: %s carries subject %q", ErrCommitNotImprovement, sha, subject)
	}
	if err := g.noOperationInProgress(ctx); err != nil {
		return err
	}
	output, err := g.git.run(ctx, "revert", "--no-edit", resolved)
	if err != nil {
		if isRevertConflict(output) {
			return &RevertConflictError{SHA: resolved, Output: output}
		}
		return fmt.Errorf("evolution: refine rollback reverted %s: %w", resolved, err)
	}
	return nil
}

// Unregistered lists the controlled files whose modification time falls after
// the last registration commit: artifacts the agent has already changed on disk
// but not yet recorded. It is a soft reminder beside the ledger, never a gate,
// because a file takes effect the moment it lands regardless of registration.
func (g *GitRefine) Unregistered(ctx context.Context) ([]string, error) {
	ledger, err := g.Status(ctx)
	if err != nil {
		return nil, err
	}
	var latest int64
	for _, entry := range ledger {
		if at := entry.CommittedAt.Unix(); at > latest {
			latest = at
		}
	}
	if latest == 0 {
		return []string{}, nil
	}
	reminders := []string{}
	walkErr := filepath.WalkDir(g.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(g.root, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !pathInScope(rel, g.protected) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if info.ModTime().Unix() > latest {
			reminders = append(reminders, rel)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return reminders, nil
}

// admit normalizes the written paths to clean run-directory-relative form and
// checks every one against the controlled set. It returns the normalized paths
// only when all of them are in scope; one offending path refuses the whole
// registration and names both the offenders and the controlled set.
func (g *GitRefine) admit(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, ErrNoRegisterPaths
	}
	normalized := make([]string, 0, len(paths))
	offending := []string{}
	seen := map[string]bool{}
	for _, written := range paths {
		rel := normalizeControlledPath(written, g.root)
		if rel == "" || rel == "." || strings.HasPrefix(rel, "../") || rel == ".." {
			offending = append(offending, written)
			continue
		}
		if !pathInScope(rel, g.protected) {
			offending = append(offending, written)
			continue
		}
		if !seen[rel] {
			seen[rel] = true
			normalized = append(normalized, rel)
		}
	}
	if len(offending) > 0 {
		return nil, &PathScopeError{Offending: offending, Controlled: g.protected}
	}
	return normalized, nil
}

// repositoryReady reports whether the run directory is the top level of a git
// work tree. A directory outside any work tree names the failure and lets the
// already-applied file edit stand.
func (g *GitRefine) repositoryReady(ctx context.Context) error {
	output, err := g.git.run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return ErrNotGitRepository
	}
	toplevel := strings.TrimSpace(output)
	rootResolved := resolvePath(g.root)
	topResolved := resolvePath(toplevel)
	if filepath.Clean(topResolved) != filepath.Clean(rootResolved) {
		return fmt.Errorf("%w: run directory %s is not the top level %s", ErrNotGitRepository, g.root, toplevel)
	}
	return nil
}

// noOperationInProgress refuses a mutation while a git revert or merge is open.
// Registering through an open revert would commit past it and swallow the
// sequencer state the agent still has to resolve, corrupting the repository.
func (g *GitRefine) noOperationInProgress(ctx context.Context) error {
	for _, head := range []string{"REVERT_HEAD", "MERGE_HEAD", "CHERRY_PICK_HEAD"} {
		if _, err := g.git.run(ctx, "rev-parse", "--verify", "--quiet", head); err == nil {
			return fmt.Errorf("%w: %s is present", ErrGitOperationInProgress, head)
		}
	}
	return nil
}

// resolveCommit resolves a full SHA or an unambiguous prefix to the commit it
// names, or reports that no commit resolves to it.
func (g *GitRefine) resolveCommit(ctx context.Context, sha string) (string, error) {
	output, err := g.git.run(ctx, "rev-parse", "--verify", sha+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrImprovementUnknown, sha)
	}
	return strings.TrimSpace(output), nil
}

// commitSubject returns the subject line of one commit.
func (g *GitRefine) commitSubject(ctx context.Context, sha string) (string, error) {
	output, err := g.git.run(ctx, "log", "-1", "--format=%s", sha)
	if err != nil {
		return "", fmt.Errorf("evolution: refine read commit %s: %w", sha, err)
	}
	return strings.TrimSpace(output), nil
}

// headCommit returns the SHA and commit time of the current HEAD, read back from
// git after a registration commit lands.
func (g *GitRefine) headCommit(ctx context.Context) (string, time.Time, error) {
	output, err := g.git.run(ctx, "log", "-1", "--format=%H%x00%ct")
	if err != nil {
		return "", time.Time{}, fmt.Errorf("evolution: refine read its registration commit: %w", err)
	}
	parts := strings.SplitN(strings.TrimSpace(output), "\x00", 2)
	if len(parts) != 2 {
		return "", time.Time{}, fmt.Errorf("evolution: refine read a malformed commit header %q", output)
	}
	unix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("evolution: refine read a malformed commit time %q: %w", parts[1], err)
	}
	return parts[0], time.Unix(unix, 0), nil
}

// parseEntry turns one `sha\0ct\0message` ledger chunk into a RefineRecord. It
// keeps only the entries whose subject opens with the improvement marker — the
// grep above is a prefilter and this line-anchored check is what really decides
// membership — and reads the note and paths from the structured commit body,
// falling back to the committed paths when the body is unreadable.
func (g *GitRefine) parseEntry(ctx context.Context, chunk string) (RefineRecord, bool) {
	chunk = strings.TrimSpace(chunk)
	if chunk == "" {
		return RefineRecord{}, false
	}
	fields := strings.SplitN(chunk, "\x00", 3)
	if len(fields) != 3 {
		return RefineRecord{}, false
	}
	message := strings.TrimSpace(fields[2])
	if message == "" {
		return RefineRecord{}, false
	}
	subject := strings.SplitN(message, "\n", 2)[0]
	if !isImprovementSubject(subject) {
		return RefineRecord{}, false
	}
	unix, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
	if err != nil {
		return RefineRecord{}, false
	}
	record := RefineRecord{
		SHA:         fields[0],
		CommittedAt: time.Unix(unix, 0),
	}
	note, paths := parseCommitBody(message)
	record.Note = note
	record.Paths = paths
	if len(record.Paths) == 0 {
		record.Paths = g.commitPaths(ctx, fields[0])
	}
	return record, true
}

// commitPaths returns the files one commit changed, used as the fallback when a
// ledger entry's structured body cannot be read.
func (g *GitRefine) commitPaths(ctx context.Context, sha string) []string {
	output, err := g.git.run(ctx, "diff-tree", "--no-commit-id", "--name-only", "-r", sha)
	if err != nil {
		return nil
	}
	paths := []string{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

// commitBody renders the structured body of an improvement commit: one JSON line
// carrying the note and the controlled paths the registration committed.
func commitBody(note string, paths []string) (string, error) {
	payload := struct {
		Note  string   `json:"note"`
		Paths []string `json:"paths"`
	}{Note: note, Paths: paths}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("evolution: refine encode its commit body: %w", err)
	}
	return string(encoded), nil
}

// parseCommitBody reads the note and paths back out of an improvement commit
// message. The structured body is the JSON line after the subject; an
// unreadable body yields empty values and the caller falls back to the commit's
// own paths.
func parseCommitBody(message string) (string, []string) {
	body := message
	if idx := strings.Index(message, "\n\n"); idx >= 0 {
		body = strings.TrimSpace(message[idx+2:])
	} else {
		return "", nil
	}
	payload := struct {
		Note  string   `json:"note"`
		Paths []string `json:"paths"`
	}{}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return "", nil
	}
	return payload.Note, payload.Paths
}

// isImprovementSubject reports whether a commit subject is line-anchored to the
// improvement marker: the marker alone or the marker followed by a space. The
// line anchoring keeps `Revert "[self-improve] …"` and a look-alike such as
// `[self-improve-foo]` out of the ledger, and generation, listing and rollback
// verification all share this one test.
func isImprovementSubject(subject string) bool {
	return subject == ImprovementMarker || strings.HasPrefix(subject, ImprovementMarker+" ")
}

// isNothingToCommit reports whether a failed commit failed because the paths
// already held the content to record, which is a normal outcome and not a
// failure.
func isNothingToCommit(output string) bool {
	return strings.Contains(output, "no changes added to commit") ||
		strings.Contains(output, "nothing to commit") ||
		strings.Contains(output, "nothing added to commit")
}

// isNoCommits reports whether a failed log failed on a repository that holds no
// commits yet, which lists as an empty ledger.
func isNoCommits(output string) bool {
	return strings.Contains(output, "does not have any commits yet") ||
		strings.Contains(output, "bad default revision")
}

// isRevertConflict reports whether a failed revert stopped on a conflict the
// agent has to resolve.
func isRevertConflict(output string) bool {
	return strings.Contains(output, "CONFLICT") ||
		strings.Contains(output, "could not apply") ||
		strings.Contains(output, "after resolving the conflicts")
}

// resolvePath resolves symlinks on a path so a run directory and git's view of
// the same work tree compare equal, falling back to the path as written when it
// cannot be resolved.
func resolvePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}
