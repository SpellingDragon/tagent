package file

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestSurfaceGrantsDeclaredTools pins the selection the declaration drives.
// - a declaration naming no tool grants all seven in the frozen order
// - a declaration naming tools grants exactly its entries in its own order
// - a declaration naming an unknown id is refused by name
// - a declaration naming the same id twice is refused by name
// - a base directory that does not exist refuses the assembly by name
//
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestSurfaceGrantsDeclaredTools(t *testing.T) {
	dir := t.TempDir()

	t.Run("no declaration grants all seven", func(t *testing.T) {
		surface := newSurface(t, Options{BaseDir: dir})
		assertTools(t, surface, Names())
	})

	t.Run("a written declaration grants its own order", func(t *testing.T) {
		granted := []string{ReplaceContentToolName, ReadFileToolName, SearchContentToolName}
		surface := newSurface(t, Options{BaseDir: dir, Tools: granted})
		assertTools(t, surface, granted)
	})

	t.Run("unknown id", func(t *testing.T) {
		_, err := New(Options{BaseDir: dir, Tools: []string{"read_everything"}})
		if !errors.Is(err, ErrUnknownTool) {
			t.Fatalf("New = %v, want ErrUnknownTool", err)
		}
	})

	t.Run("duplicate id", func(t *testing.T) {
		_, err := New(Options{BaseDir: dir, Tools: []string{ReadFileToolName, ReadFileToolName}})
		if !errors.Is(err, ErrDuplicateTool) {
			t.Fatalf("New = %v, want ErrDuplicateTool", err)
		}
	})

	t.Run("missing base directory", func(t *testing.T) {
		_, err := New(Options{BaseDir: filepath.Join(dir, "absent")})
		if err == nil || !strings.Contains(err.Error(), "base directory") {
			t.Fatalf("New = %v, want a named refusal beside the missing directory", err)
		}
	})
}

// TestSurfaceAnswersTheDeclaredTools pins that the granted tools read, write, edit, list and search one real base directory.
// - save_file writes a relative path under the base directory
// - replace_content edits the written file in place
// - read_file answers the edited content
// - read_multiple_files answers every globbed file
// - list_file and search_file answer the directory and the glob
// - search_content answers the line that carries the pattern
//
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestSurfaceAnswersTheDeclaredTools(t *testing.T) {
	dir := t.TempDir()
	surface := newSurface(t, Options{BaseDir: dir})

	callSurface(t, surface, SaveFileToolName, `{"file_name":"notes/a.txt","contents":"alpha\nneedle here\n","overwrite":true}`)
	callSurface(t, surface, ReplaceContentToolName, `{"file_name":"notes/a.txt","old_string":"needle here","new_string":"replaced value"}`)

	var read struct {
		Contents string `json:"contents"`
	}
	result, err := callSurface(t, surface, ReadFileToolName, `{"file_name":"notes/a.txt"}`)
	decodeCall(t, result, err, &read)
	if !strings.Contains(read.Contents, "replaced value") {
		t.Fatalf("read_file contents = %q, want the replaced line", read.Contents)
	}

	var multiple struct {
		Files []struct {
			FileName string `json:"file_name"`
		} `json:"files"`
	}
	result, err = callSurface(t, surface, ReadMultipleFilesToolName, `{"patterns":["notes/*.txt"]}`)
	decodeCall(t, result, err, &multiple)
	if len(multiple.Files) != 1 || multiple.Files[0].FileName != "notes/a.txt" {
		t.Fatalf("read_multiple_files = %+v, want notes/a.txt", multiple.Files)
	}

	var listed struct {
		Files []string `json:"files"`
	}
	result, err = callSurface(t, surface, ListFileToolName, `{"path":"notes"}`)
	decodeCall(t, result, err, &listed)
	if len(listed.Files) != 1 || listed.Files[0] != "a.txt" {
		t.Fatalf("list_file = %v, want a.txt", listed.Files)
	}

	var searched struct {
		Files []string `json:"files"`
	}
	result, err = callSurface(t, surface, SearchFileToolName, `{"path":"","pattern":"**/*.txt"}`)
	decodeCall(t, result, err, &searched)
	if len(searched.Files) != 1 || searched.Files[0] != "notes/a.txt" {
		t.Fatalf("search_file = %v, want notes/a.txt", searched.Files)
	}

	var matched struct {
		FileMatches []struct {
			FilePath string `json:"file_path"`
		} `json:"file_matches"`
	}
	result, err = callSurface(t, surface, SearchContentToolName, `{"path":"","file_pattern":"**/*.txt","content_pattern":"replaced"}`)
	decodeCall(t, result, err, &matched)
	if len(matched.FileMatches) != 1 || matched.FileMatches[0].FilePath != "notes/a.txt" {
		t.Fatalf("search_content = %+v, want notes/a.txt", matched.FileMatches)
	}
}

// TestSurfaceRefusesPathsOutsideTheBaseDirectory pins that a path climbing out of the base directory is refused and nothing is written outside.
// - a relative read climbing out with .. is refused by name
// - a relative write climbing out with .. is refused by name and writes nothing
// - an absolute read outside every root is refused by name
//
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestSurfaceRefusesPathsOutsideTheBaseDirectory(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "inside")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	surface := newSurface(t, Options{BaseDir: base})

	_, err := callSurface(t, surface, ReadFileToolName, `{"file_name":"../outside.txt"}`)
	if err == nil || !strings.Contains(err.Error(), "'..'") {
		t.Fatalf("read outside = %v, want the traversal refusal", err)
	}

	_, err = callSurface(t, surface, SaveFileToolName, `{"file_name":"../written.txt","contents":"x","overwrite":true}`)
	if err == nil || !strings.Contains(err.Error(), "'..'") {
		t.Fatalf("write outside = %v, want the traversal refusal", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "written.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("write outside left %q behind", filepath.Join(root, "written.txt"))
	}

	_, err = callSurface(t, surface, ReadFileToolName, `{"file_name":`+quote(t, outside)+`}`)
	if err == nil || !strings.Contains(err.Error(), "outside base_directory") {
		t.Fatalf("absolute read = %v, want the root refusal", err)
	}
}

// newSurface builds the surface one test runs over and releases it at the end.
func newSurface(t *testing.T, opts Options) *Surface {
	t.Helper()
	surface, err := New(opts)
	if err != nil {
		t.Fatalf("New(%+v): %v", opts, err)
	}
	t.Cleanup(func() {
		if err := surface.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	return surface
}

// assertTools pins the declared names one surface carries, in order.
func assertTools(t *testing.T, surface *Surface, want []string) {
	t.Helper()
	got := make([]string, 0, len(surface.Tools(context.Background())))
	for _, tool := range surface.Tools(context.Background()) {
		got = append(got, tool.Declaration().Name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("surface tools = %v, want %v", got, want)
	}
}

// callSurface calls one named tool of the surface with a raw argument object.
func callSurface(t *testing.T, surface *Surface, name, args string) (any, error) {
	t.Helper()
	for _, tool := range surface.Tools(context.Background()) {
		if tool.Declaration().Name != name {
			continue
		}
		callable, ok := tool.(trpctool.CallableTool)
		if !ok {
			t.Fatalf("tool %q carries no call surface", name)
		}
		return callable.Call(context.Background(), []byte(args))
	}
	t.Fatalf("surface carries no %q tool", name)
	return nil, nil
}

// decodeCall folds one successful call result onto a typed answer.
func decodeCall(t *testing.T, result any, err error, into any) {
	t.Helper()
	if err != nil {
		t.Fatalf("call = %v", err)
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatalf("marshal result: %v", marshalErr)
	}
	if unmarshalErr := json.Unmarshal(raw, into); unmarshalErr != nil {
		t.Fatalf("unmarshal result %s: %v", raw, unmarshalErr)
	}
}

// quote renders one path as a JSON string literal.
func quote(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %q: %v", value, err)
	}
	return string(raw)
}
