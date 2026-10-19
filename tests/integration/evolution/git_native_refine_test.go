package evolution

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/evolution/refine"
)

// refineRepo is one throwaway git work tree standing in for the agent's managed
// controlled repository. Every git command it runs targets this temporary
// directory only, so the refine surface under test never reaches the repository
// this test source lives in.
type refineRepo struct {
	t    *testing.T
	root string
}

// newRefineRepo builds a temporary git work tree with the controlled trees
// present and one baseline commit, so a registration has a clean tree to commit
// on top of.
func newRefineRepo(t *testing.T) *refineRepo {
	t.Helper()
	repo := &refineRepo{t: t, root: t.TempDir()}
	repo.git("init", "-q")
	repo.git("config", "user.name", "tester")
	repo.git("config", "user.email", "tester@example.com")
	repo.git("config", "commit.gpgsign", "false")
	repo.write("resources/prompts/SOUL.md", "base soul\n")
	repo.write("skills/alpha/SKILL.md", "base skill\n")
	repo.git("add", ".")
	repo.git("commit", "-q", "-m", "baseline")
	return repo
}

// git runs one git command in the temporary work tree and fails the case when it
// errors.
func (r *refineRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.root
	output, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

// tryGit runs one git command in the temporary work tree and reports its output
// and error without failing the case.
func (r *refineRepo) tryGit(args ...string) (string, error) {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.root
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// tryGitOut runs one git command and returns its combined output regardless of
// the exit code, for read-only snapshots.
func (r *refineRepo) tryGitOut(args ...string) string {
	r.t.Helper()
	output, _ := r.tryGit(args...)
	return output
}

// write lands content at a path relative to the work tree root.
func (r *refineRepo) write(rel, content string) {
	r.t.Helper()
	full := filepath.Join(r.root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatalf("write %s: %v", rel, err)
	}
}

// state snapshots the git state the refine surface must leave untouched on a
// refused registration: the HEAD commit and the staged/working status.
func (r *refineRepo) state() string {
	r.t.Helper()
	head := strings.TrimSpace(r.tryGitOut("rev-parse", "HEAD"))
	status := strings.TrimSpace(r.tryGitOut("status", "--porcelain"))
	return head + "\n---\n" + status
}

// improvementLog records the improvement events the refine surface writes
// through its injected recorder.
type improvementLog struct {
	entries []refine.Improvement
}

// record captures one improvement event.
func (l *improvementLog) record(ctx context.Context, improvement refine.Improvement) error {
	l.entries = append(l.entries, improvement)
	return nil
}

// recorder returns the port the refine surface records through.
func (l *improvementLog) recorder() refine.ImprovementRecorder {
	return l.record
}

// TestGitNativeRefine checks the git-native refine surface over one managed controlled repository:
// - register commits exactly the paths it is handed and never rolls in the user's staged work or other working-tree edits
// - a path outside the controlled set refuses the whole registration before git is touched
// - a run directory outside a git work tree names the failure while the file edit stays live
// - status lists only marked commits with their time and never the user's own commits, and the soft reminder names artifacts changed since the last registration
// - rollback refuses a commit without the marker and a commit that resolves to nothing, and a rollback writes no improvement event
// - registering with nothing to commit is a normal outcome and records no event
// - a revert that stops on a conflict returns its details and leaves the revert open, and a registration during an open revert is refused
// 契约: docs/wiki/evolution/evolution-architecture.md#refine
func TestGitNativeRefine(t *testing.T) {
	ctx := context.Background()

	t.Run("register commits only the explicit controlled paths under the improvement marker", func(t *testing.T) {
		repo := newRefineRepo(t)
		log := &improvementLog{}
		gitRefine := refine.NewGitRefine(repo.root, log.recorder(), nil)

		repo.write("resources/prompts/SOUL.md", "improved soul\n")
		repo.write("skills/alpha/SKILL.md", "improved skill\n")
		repo.write("notes/user.txt", "user scratch\n")
		repo.git("add", "notes/user.txt")

		record, err := gitRefine.Register(ctx, []string{"resources/prompts/SOUL.md"}, "痛点:提示词漂移 → 产物:重写 SOUL.md → 预期收益:回答更稳")
		if err != nil {
			t.Fatalf("register of one controlled path must commit: %v", err)
		}
		if record.SHA == "" || record.CommittedAt.IsZero() {
			t.Fatalf("a registration carries its commit SHA and commit time, got %#v", record)
		}
		if got := strings.Join(record.Paths, ","); got != "resources/prompts/SOUL.md" {
			t.Fatalf("the record names the one path it committed, got %q", got)
		}
		if record.Note != "痛点:提示词漂移 → 产物:重写 SOUL.md → 预期收益:回答更稳" {
			t.Fatalf("the record carries the author's note, got %q", record.Note)
		}
		if !record.Window.ActivatedAt.IsZero() || record.Posterior.Verdict != "" {
			t.Fatalf("a fresh registration opens no window and records no judgment, got %#v", record)
		}

		subject := strings.TrimSpace(repo.git("log", "-1", "--format=%s", record.SHA))
		if subject != refine.ImprovementMarker+" register" {
			t.Fatalf("the commit subject opens with the improvement marker, got %q", subject)
		}
		committed := strings.TrimSpace(repo.git("diff-tree", "--no-commit-id", "--name-only", "-r", record.SHA))
		if committed != "resources/prompts/SOUL.md" {
			t.Fatalf("the commit holds only the explicit path, got %q", committed)
		}
		if staged := repo.git("diff", "--cached", "--name-only"); !strings.Contains(staged, "notes/user.txt") {
			t.Fatalf("the user's staged work stays staged and uncommitted, got %q", staged)
		}
		if status := repo.git("status", "--porcelain"); !strings.Contains(status, "skills/alpha/SKILL.md") {
			t.Fatalf("an unregistered working-tree edit is left alone, got %q", status)
		}

		if len(log.entries) != 1 {
			t.Fatalf("one registration writes one improvement event, got %d", len(log.entries))
		}
		event := log.entries[0]
		if event.SHA != record.SHA || event.Note != record.Note || strings.Join(event.Paths, ",") != "resources/prompts/SOUL.md" {
			t.Fatalf("the improvement event carries the registration, got %#v", event)
		}
	})

	t.Run("a path outside the controlled set refuses the whole registration before git is touched", func(t *testing.T) {
		repo := newRefineRepo(t)
		log := &improvementLog{}
		gitRefine := refine.NewGitRefine(repo.root, log.recorder(), nil)
		repo.write("notes/user.txt", "user scratch\n")
		repo.git("add", "notes/user.txt")
		before := repo.state()

		for _, cased := range []struct {
			name  string
			paths []string
		}{
			{"a plain uncontrolled file", []string{"config/evil.txt"}},
			{"a resources path outside the prompts tree", []string{"resources/other.txt"}},
			{"a resources look-alike prefix", []string{"resources-evil/x.txt"}},
			{"an in-scope path beside an escaping one", []string{"resources/prompts/SOUL.md", "skills/../../evil.txt"}},
			{"the working directory itself", []string{"."}},
			{"a parent directory", []string{".."}},
			{"an empty path", []string{""}},
		} {
			_, err := gitRefine.Register(ctx, cased.paths, "note")
			if err == nil {
				t.Fatalf("%s: an out-of-scope registration must be refused", cased.name)
			}
			if !errors.Is(err, refine.ErrPathOutOfScope) {
				t.Fatalf("%s: the refusal must reach ErrPathOutOfScope, got %v", cased.name, err)
			}
			var scopeErr *refine.PathScopeError
			if !errors.As(err, &scopeErr) || len(scopeErr.Offending) == 0 || len(scopeErr.Controlled) == 0 {
				t.Fatalf("%s: the refusal names the offenders and the controlled set, got %#v", cased.name, err)
			}
			if after := repo.state(); after != before {
				t.Fatalf("%s: a refused registration never touches git, before %q after %q", cased.name, before, after)
			}
		}

		if _, err := gitRefine.Register(ctx, nil, "note"); !errors.Is(err, refine.ErrNoRegisterPaths) {
			t.Fatalf("a registration with no path is named, got %v", err)
		}
		if after := repo.state(); after != before {
			t.Fatalf("a registration with no path never touches git, before %q after %q", before, after)
		}
		if len(log.entries) != 0 {
			t.Fatalf("a refused registration writes no improvement event, got %d", len(log.entries))
		}
	})

	t.Run("register outside a git work tree names the failure while the file stays live", func(t *testing.T) {
		plain := t.TempDir()
		if err := os.MkdirAll(filepath.Join(plain, "resources/prompts"), 0o755); err != nil {
			t.Fatalf("mkdir controlled tree: %v", err)
		}
		soul := filepath.Join(plain, "resources/prompts/SOUL.md")
		if err := os.WriteFile(soul, []byte("edited soul\n"), 0o644); err != nil {
			t.Fatalf("write controlled file: %v", err)
		}
		log := &improvementLog{}
		gitRefine := refine.NewGitRefine(plain, log.recorder(), nil)

		_, err := gitRefine.Register(ctx, []string{"resources/prompts/SOUL.md"}, "note")
		if !errors.Is(err, refine.ErrNotGitRepository) {
			t.Fatalf("register outside a git work tree must name ErrNotGitRepository, got %v", err)
		}
		if body, err := os.ReadFile(soul); err != nil || string(body) != "edited soul\n" {
			t.Fatalf("the file edit stays live outside a git work tree, got %q beside %v", body, err)
		}
		if len(log.entries) != 0 {
			t.Fatalf("a degraded registration writes no improvement event, got %d", len(log.entries))
		}
	})

	t.Run("status lists the marked ledger with its time and the soft reminder names unregistered artifacts", func(t *testing.T) {
		repo := newRefineRepo(t)
		log := &improvementLog{}
		gitRefine := refine.NewGitRefine(repo.root, log.recorder(), nil)

		repo.write("resources/prompts/SOUL.md", "improved soul\n")
		first, err := gitRefine.Register(ctx, []string{"resources/prompts/SOUL.md"}, "note-1")
		if err != nil {
			t.Fatalf("register the first improvement: %v", err)
		}
		repo.write("scratch/user.txt", "user work\n")
		repo.git("add", "scratch/user.txt")
		repo.git("commit", "-q", "-m", "user change")
		userSHA := strings.TrimSpace(repo.git("rev-parse", "HEAD"))

		repo.write("skills/alpha/SKILL.md", "improved skill\n")
		second, err := gitRefine.Register(ctx, []string{"skills/alpha/SKILL.md"}, "note-2")
		if err != nil {
			t.Fatalf("register the second improvement: %v", err)
		}

		ledger, err := gitRefine.Status(ctx)
		if err != nil {
			t.Fatalf("status must list the ledger: %v", err)
		}
		if len(ledger) != 2 {
			t.Fatalf("status lists the two improvements and not the user commit, got %d", len(ledger))
		}
		if ledger[0].SHA != second.SHA || ledger[1].SHA != first.SHA {
			t.Fatalf("the ledger is newest first, got %q then %q", ledger[0].SHA, ledger[1].SHA)
		}
		for _, entry := range ledger {
			if entry.SHA == userSHA {
				t.Fatalf("the user's own commit never enters the ledger, got %#v", entry)
			}
			if entry.CommittedAt.IsZero() || entry.Note == "" || len(entry.Paths) == 0 {
				t.Fatalf("a ledger entry carries its time, note and paths, got %#v", entry)
			}
			if !entry.Window.ActivatedAt.IsZero() || entry.Posterior.Verdict != "" {
				t.Fatalf("the ledger opens no window and records no judgment here, got %#v", entry)
			}
		}

		later := time.Now().Add(2 * time.Second)
		soul := filepath.Join(repo.root, "resources/prompts/SOUL.md")
		if err := os.Chtimes(soul, later, later); err != nil {
			t.Fatalf("push the artifact past the last registration: %v", err)
		}
		reminders, err := gitRefine.Unregistered(ctx)
		if err != nil {
			t.Fatalf("the soft reminder must be readable: %v", err)
		}
		if got := strings.Join(reminders, ","); !strings.Contains(got, "resources/prompts/SOUL.md") {
			t.Fatalf("the reminder names the artifact changed since the last registration, got %q", got)
		}
		if strings.Contains(strings.Join(reminders, ","), "skills/alpha/SKILL.md") {
			t.Fatalf("a registered artifact is not an unregistered reminder, got %q", reminders)
		}
	})

	t.Run("rollback reverts only a marked commit and refuses a user commit", func(t *testing.T) {
		repo := newRefineRepo(t)
		log := &improvementLog{}
		gitRefine := refine.NewGitRefine(repo.root, log.recorder(), nil)

		repo.write("resources/prompts/SOUL.md", "improved soul\n")
		record, err := gitRefine.Register(ctx, []string{"resources/prompts/SOUL.md"}, "note")
		if err != nil {
			t.Fatalf("register the improvement to roll back: %v", err)
		}
		repo.write("scratch/user.txt", "user work\n")
		repo.git("add", "scratch/user.txt")
		repo.git("commit", "-q", "-m", "user change")
		userSHA := strings.TrimSpace(repo.git("rev-parse", "HEAD"))
		eventsAfterRegister := len(log.entries)

		if err := gitRefine.Rollback(ctx, userSHA); !errors.Is(err, refine.ErrCommitNotImprovement) {
			t.Fatalf("rollback must refuse a commit without the marker, got %v", err)
		}
		if err := gitRefine.Rollback(ctx, "0000000000000000000000000000000000000000"); !errors.Is(err, refine.ErrImprovementUnknown) {
			t.Fatalf("rollback must refuse a commit that resolves to nothing, got %v", err)
		}

		if err := gitRefine.Rollback(ctx, record.SHA); err != nil {
			t.Fatalf("rollback of a marked improvement must revert: %v", err)
		}
		revertSubject := strings.TrimSpace(repo.git("log", "-1", "--format=%s"))
		if !strings.HasPrefix(revertSubject, "Revert ") {
			t.Fatalf("a rollback lands a revert commit, got %q", revertSubject)
		}
		if body := strings.TrimSpace(repo.git("show", "HEAD:resources/prompts/SOUL.md")); body != "base soul" {
			t.Fatalf("the revert restores the pre-improvement content, got %q", body)
		}

		ledger, err := gitRefine.Status(ctx)
		if err != nil {
			t.Fatalf("status after a rollback must be readable: %v", err)
		}
		if len(ledger) != 1 || ledger[0].SHA != record.SHA {
			t.Fatalf("the revert is never re-read as a fresh improvement, got %#v", ledger)
		}
		if len(log.entries) != eventsAfterRegister {
			t.Fatalf("a rollback is terminal and writes no improvement event, got %d", len(log.entries))
		}
	})

	t.Run("registering with nothing to commit is a normal outcome and records no event", func(t *testing.T) {
		repo := newRefineRepo(t)
		log := &improvementLog{}
		gitRefine := refine.NewGitRefine(repo.root, log.recorder(), nil)

		record, err := gitRefine.Register(ctx, []string{"resources/prompts/SOUL.md"}, "note")
		if err != nil {
			t.Fatalf("registering unchanged content is not a failure: %v", err)
		}
		if record.SHA != "" {
			t.Fatalf("nothing was committed, so no SHA is recorded, got %q", record.SHA)
		}
		if len(log.entries) != 0 {
			t.Fatalf("no commit means no improvement event, got %d", len(log.entries))
		}
	})

	t.Run("a revert conflict reports details and a registration during an open revert is refused", func(t *testing.T) {
		repo := newRefineRepo(t)
		log := &improvementLog{}
		gitRefine := refine.NewGitRefine(repo.root, log.recorder(), nil)

		repo.write("resources/prompts/SOUL.md", "improved soul\n")
		record, err := gitRefine.Register(ctx, []string{"resources/prompts/SOUL.md"}, "note")
		if err != nil {
			t.Fatalf("register the improvement to conflict: %v", err)
		}
		repo.write("resources/prompts/SOUL.md", "user rework\n")
		repo.git("add", "resources/prompts/SOUL.md")
		repo.git("commit", "-q", "-m", "user rework")

		err = gitRefine.Rollback(ctx, record.SHA)
		if !errors.Is(err, refine.ErrRevertConflict) {
			t.Fatalf("a conflicting revert must report ErrRevertConflict, got %v", err)
		}
		var conflict *refine.RevertConflictError
		if !errors.As(err, &conflict) || conflict.SHA != record.SHA || !strings.Contains(conflict.Output, "CONFLICT") {
			t.Fatalf("the conflict carries its commit and the git output, got %#v", conflict)
		}
		if _, tryErr := repo.tryGit("rev-parse", "--verify", "--quiet", "REVERT_HEAD"); tryErr != nil {
			t.Fatalf("the revert stays open for the agent to resolve, got %v", tryErr)
		}

		if _, err := gitRefine.Register(ctx, []string{"resources/prompts/SOUL.md"}, "note"); !errors.Is(err, refine.ErrGitOperationInProgress) {
			t.Fatalf("a registration during an open revert is refused by name, got %v", err)
		}
		repo.git("revert", "--abort")
	})
}
