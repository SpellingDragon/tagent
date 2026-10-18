// 契约: docs/wiki/platform/org-hot-reload.md#generations
//
// The FILE lazy-read face of the effective binding: the state owner holds the structural binding of a
// prompt — which files, in which order — while the file content is read here at the request boundary
// and fixed as the version that one request uses.
//
// - The two are never conflated: the structural binding belongs to the published generation, the content version does not.
// - A file change takes effect at the next request boundary at once — no register, no evaluation and no precondition gate stands in its way — and it never assigns a structural generation.
// - A request under way keeps the content it fixed and is never half-way spliced with a newer file.
// - Skill content is read through the framework skill repository (the RefreshableRepository rescan that replaces the whole index and the parser behind it); this face is only the request-boundary glue around it, with no skill scanner and no parsing copied here.
package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SpellingDragon/tagent/config"

	"trpc.group/trpc-go/trpc-agent-go/skill"
)

var (
	// ErrFileRootMissing reports a prompt file read through a file source that
	// names no root to read it under.
	ErrFileRootMissing = errors.New("state: the file source names no root")
	// ErrSkillRepositoryMissing reports a skill file read through a file source
	// that carries no skill repository to read it through.
	ErrSkillRepositoryMissing = errors.New("state: the file source carries no skill repository")
	// ErrFileKindUnknown reports a file reference whose kind is neither prompt
	// nor skill, so no read path belongs to it.
	ErrFileKindUnknown = errors.New("state: the file reference names no known kind")
)

// FixedFile is one file content fixed at a request boundary: the file
// reference it was read for beside the content read for this request.
type FixedFile struct {
	// Ref is the file reference the content was read for.
	Ref config.FileRef
	// Content is the content read at this request boundary.
	Content string
}

// FileSnapshot is the content version one request fixes at its boundary: the
// contents read in the structural order of the binding beside the version
// computed from exactly those contents.
//
// The snapshot is immutable once fixed: a later file change never rewrites it,
// so a request under way keeps the version it fixed and no historical snapshot
// pins a later request to its content.
type FileSnapshot struct {
	// Files are the contents fixed in the structural order of the binding.
	Files []FixedFile
	// Version is the content version fixed at this request boundary. It names
	// the contents this snapshot carries and never the structural generation.
	Version string
}

// FileSource is the file source behind one agent's structural binding: it
// reads the prompt and skill contents the binding names at the request
// boundary and fixes the version one request uses.
//
// The read is a fresh read at every request boundary, so a file rewritten
// between two requests is read anew by the later one while the structural
// generation the binding belongs to stays where it is.
type FileSource struct {
	root   string
	skills skill.Repository
}

// NewFileSource returns the file source that reads prompt files under the
// given root and skill files through the given framework skill repository. The
// repository may be nil when the binding names no skill file; a skill read
// without one is refused by name.
func NewFileSource(root string, skills skill.Repository) *FileSource {
	return &FileSource{root: root, skills: skills}
}

// Fixate reads the contents one request uses and fixes their version: the
// files are read in the structural order the binding carries and the version
// is computed from exactly the contents read. The read takes effect at once —
// no register, no evaluation and no precondition gate — and assigns no
// structural generation.
func (s *FileSource) Fixate(ctx context.Context, prompt PromptSource) (FileSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return FileSnapshot{}, err
	}
	files := make([]FixedFile, 0, len(prompt.Files))
	for _, ref := range prompt.Files {
		content, err := s.read(ref)
		if err != nil {
			return FileSnapshot{}, err
		}
		files = append(files, FixedFile{Ref: ref, Content: content})
	}
	return FileSnapshot{Files: files, Version: contentVersion(files)}, nil
}

// read reports the content of one file reference on the channel its kind
// names: a prompt file is read under the root, a skill file is read through
// the framework skill repository, and any other kind is refused by name.
func (s *FileSource) read(ref config.FileRef) (string, error) {
	switch ref.Kind {
	case config.FileKindPrompt, "":
		return s.readPrompt(ref)
	case config.FileKindSkill:
		return s.readSkill(ref)
	default:
		return "", NewCause(ref.Path, 0, "read the file content at the request boundary",
			fmt.Errorf("%w: %s", ErrFileKindUnknown, ref.Kind))
	}
}

// readPrompt reads one prompt file under the source root at the request
// boundary. The read is fresh on every call, so a rewritten prompt is read by
// the next request without any reload in between.
func (s *FileSource) readPrompt(ref config.FileRef) (string, error) {
	if s.root == "" {
		return "", NewCause(ref.Path, 0, "read the file content at the request boundary", ErrFileRootMissing)
	}
	data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(ref.Path)))
	if err != nil {
		return "", NewCause(ref.Path, 0, "read the file content at the request boundary", err)
	}
	return string(data), nil
}

// readSkill reads one skill body through the framework skill repository. The
// glue names the skill the reference points at and calls the repository: the
// rescan that replaces the whole index and the front-matter parsing both stay
// inside the framework, so no scanner and no skill parsing lives here.
func (s *FileSource) readSkill(ref config.FileRef) (string, error) {
	if s.skills == nil {
		return "", NewCause(ref.Path, 0, "read the skill content at the request boundary", ErrSkillRepositoryMissing)
	}
	if refreshable, ok := s.skills.(skill.RefreshableRepository); ok {
		if err := refreshable.Refresh(); err != nil {
			return "", NewCause(ref.Path, 0, "refresh the skill repository at the request boundary", err)
		}
	}
	got, err := s.skills.Get(skillNameOf(ref.Path))
	if err != nil {
		return "", NewCause(ref.Path, 0, "read the skill content at the request boundary", err)
	}
	return got.Body, nil
}

// skillNameOf reports the skill name one skill reference names: the reference
// points at the skill folder or at its SKILL.md file, and the repository
// indexes the skill under the name the framework derives for it.
func skillNameOf(path string) string {
	name := filepath.Base(filepath.FromSlash(path))
	if name == skill.SkillFile {
		name = filepath.Base(filepath.Dir(filepath.FromSlash(path)))
	}
	return name
}

// contentVersion reports the content version of exactly the contents one
// snapshot fixes: the hash covers every reference and its content in the
// structural order, so a version names one fixed content set and nothing else —
// least of all a structural generation.
func contentVersion(files []FixedFile) string {
	sum := sha256.New()
	for _, f := range files {
		fmt.Fprintf(sum, "%s\n%s\n%d\n%s\n", f.Ref.Path, f.Ref.Kind, len(f.Content), f.Content)
	}
	return hex.EncodeToString(sum.Sum(nil))
}
