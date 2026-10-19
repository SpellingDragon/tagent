package refine

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// ErrNoRegisterPaths reports a registration that names no controlled path to
// record. A registration with no explicit pathspec has nothing to stage and
// commit and is refused before git is touched.
var ErrNoRegisterPaths = errors.New("evolution: refine register needs at least one controlled path")

// ErrPathOutOfScope reports a registration naming a path outside the controlled
// path set. The whole registration is refused before any git command runs, so an
// out-of-scope path never reaches `git add` or `git commit` and the repository
// keeps the state it had.
var ErrPathOutOfScope = errors.New("evolution: refine register refuses a path outside the controlled set")

// PathScopeError is the named refusal of a registration whose paths leave the
// controlled set: the offending paths and the controlled patterns they were
// judged against. Unwrap exposes ErrPathOutOfScope, so a caller reaches the
// named refusal through errors.Is instead of matching rendered text.
type PathScopeError struct {
	// Offending lists the written paths that fall outside the controlled set.
	Offending []string
	// Controlled lists the controlled patterns the paths were judged against.
	Controlled []string
}

// Error renders the offending paths beside the controlled set they missed.
func (e *PathScopeError) Error() string {
	return fmt.Sprintf("evolution: refine register refuses out-of-scope paths %v, controlled set %v", e.Offending, e.Controlled)
}

// Unwrap exposes the named refusal underneath the rendered message.
func (e *PathScopeError) Unwrap() error {
	return ErrPathOutOfScope
}

// 契约: docs/wiki/evolution/evolution-architecture.md#git-safety
//
// DefaultProtectedPaths returns the controlled path patterns the refine surface
// guards by default: the prompt and skill trees. A registration may record only
// the files under these patterns, so an edit to anything else is refused before
// git is touched, and the file itself still took effect on disk the moment it
// was written.
func DefaultProtectedPaths() []string {
	return []string{"resources/prompts/**", "skills/**"}
}

// normalizeControlledPath folds one written path to a clean slash path relative
// to the run directory. A path may arrive as an absolute path, a path relative
// to the run directory, or a path relative to the workspace that shares the run
// directory; all three collapse to the same relative form, so one file cannot
// slip past or trip the controlled check by being written differently. An empty
// path collapses to the empty string, which is never in scope.
func normalizeControlledPath(written, root string) string {
	written = strings.TrimSpace(written)
	if written == "" {
		return ""
	}
	if filepath.IsAbs(written) {
		rootReal, rootErr := filepath.EvalSymlinks(root)
		pathReal, pathErr := filepath.EvalSymlinks(written)
		if rootErr == nil && pathErr == nil {
			if rel, err := filepath.Rel(rootReal, pathReal); err == nil {
				written = rel
			}
		} else if rel, err := filepath.Rel(root, written); err == nil {
			written = rel
		}
	}
	return filepath.ToSlash(filepath.Clean(written))
}

// pathInScope reports whether a normalized relative path falls under one of the
// controlled patterns. The match is segment-recursive so a `**` segment spans
// any depth while a plain segment stays a single-segment wildcard, keeping a
// look-alike such as "resources-evil/x" out of "resources/prompts/**".
func pathInScope(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchSegments(pattern, relPath) {
			return true
		}
	}
	return false
}

// matchSegments matches one `/`-segmented pattern against one `/`-segmented
// path. path.Match cannot express `**`, so the split segments are matched
// recursively here instead.
func matchSegments(pattern, target string) bool {
	return matchSeg(strings.Split(pattern, "/"), strings.Split(target, "/"))
}

// matchSeg matches pattern segments against target segments. A leading `**`
// consumes any number of target segments including none; every other segment is
// a single-segment path.Match and so never crosses a `/`.
func matchSeg(pattern, target []string) bool {
	if len(pattern) == 0 {
		return len(target) == 0
	}
	if pattern[0] == "**" {
		if matchSeg(pattern[1:], target) {
			return true
		}
		return len(target) > 0 && matchSeg(pattern, target[1:])
	}
	if len(target) == 0 {
		return false
	}
	if ok, err := path.Match(pattern[0], target[0]); err != nil || !ok {
		return false
	}
	return matchSeg(pattern[1:], target[1:])
}
