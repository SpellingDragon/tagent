package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// taskToolFaceName is the tool member the task layer must never declare again: the
// model-visible resume tool the send re-entry replaced. The spelling is assembled so
// the residue gate over the tree reports the member once it is declared again.
const taskToolFaceName = "resume" + "_task"

// TestTaskToolFaceWithoutResume pins the tool face of the task layer: the member the model never sees again, and the input channel kept beneath it.
// - the wrapped face is the inner tool's own declaration, so the task layer adds no member of its own and no resume tool can come from here
// - the retained resume channel is identity-bearing: an input carrying no event identity is refused by name, so no free-text call opens a round
// - no file of this package declares the resume tool, and the scan reports one that does, so the check is sensitive to the member it exists to refuse
// 契约: docs/wiki/agent/task-lifecycle.md#resume-states
func TestTaskToolFaceWithoutResume(t *testing.T) {
	ctx := context.Background()

	t.Run("the tool face is the wrapped tool's own", func(t *testing.T) {
		inner := &taskToolStub{name: "plan_update"}
		declaration := Wrap(nil, inner).Declaration()
		if declaration == nil || declaration.Name != inner.name {
			t.Fatalf("wrapped declaration = %+v, want the inner tool %q unchanged", declaration, inner.name)
		}
		if declaration.Name == taskToolFaceName {
			t.Fatalf("the wrapped declaration = %q, want a member the task layer did not synthesize", declaration.Name)
		}
	})

	t.Run("the retained channel needs an input identity", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if _, err := registry.Resume(ctx, taskID, event.Event{}); !errors.Is(err, ErrInputMissing) {
			t.Fatalf("a resume carrying no event identity = %v, want %v", err, ErrInputMissing)
		}
	})

	t.Run("the tool family enumeration carries no resume member", func(t *testing.T) {
		sources, err := taskPackageSources()
		if err != nil {
			t.Fatalf("read the package sources: %v", err)
		}
		if declared := taskToolFaceDeclarations(sources, taskToolFaceName); len(declared) > 0 {
			t.Fatalf("the resume tool is declared again in %v, want no member the model reaches", declared)
		}
		planted := map[string]string{
			"planted.go": "// the member the scan exists to refuse\nconst name = \"" + taskToolFaceName + "\"\n",
		}
		if declared := taskToolFaceDeclarations(planted, taskToolFaceName); len(declared) == 0 {
			t.Fatal("a package declaring the resume tool again was read as none")
		}
	})
}

// taskToolStub is the smallest tool the wrapper is driven beside: one declaration and
// one answer, so the wrapped face is read without a provider.
type taskToolStub struct {
	name string
}

var _ trpctool.CallableTool = (*taskToolStub)(nil)

// Declaration reports the one member the stub stands for.
func (s *taskToolStub) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: s.name, InputSchema: &trpctool.Schema{Type: "object"}}
}

// Call answers the one result text the stub carries.
func (s *taskToolStub) Call(context.Context, []byte) (any, error) { return "ok", nil }

// taskPackageSources reads the Go sources of this package, keyed by file name, so the
// tool-face enumeration of the layer is read from the code the build compiles.
func taskPackageSources() (map[string]string, error) {
	entries, err := os.ReadDir(".")
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		body, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			return nil, err
		}
		out[name] = string(body)
	}
	return out, nil
}

// taskToolFaceDeclarations reports the files whose source names the given tool member,
// sorted so the report is reproducible.
func taskToolFaceDeclarations(sources map[string]string, name string) []string {
	var declared []string
	for file, body := range sources {
		if strings.Contains(body, name) {
			declared = append(declared, file)
		}
	}
	sort.Strings(declared)
	return declared
}
