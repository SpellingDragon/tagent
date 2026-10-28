package capabilities

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	tagentfile "github.com/SpellingDragon/tagent/tool/file"
)

// TestFileFaceServesTheDeclaredTools drives the file face one declaration fixes through the public assembly:
// - the seven declared file ids are the exact tool set the request carries
// - save, replace and read execute for real against the declared base directory
// - list, search_file and search_content answer the real directory they were rooted at
// - a path that climbs out of the base directory is refused instead of served
// - a write that climbs out of the base directory leaves the outside file untouched
//
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestFileFaceServesTheDeclaredTools(t *testing.T) {
	t.Run("the declared file ids answer the real base directory", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
			t.Fatalf("create the notes directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "notes", "draft.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
			t.Fatalf("write the draft file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "notes", "extra.md"), []byte("extra line\n"), 0o600); err != nil {
			t.Fatalf("write the extra file: %v", err)
		}

		round := 0
		provider := &ntModel{name: "model-file", answer: func(req *model.Request) *model.Response {
			round++
			if round == 1 {
				return ntToolTurn(
					ntCall("call-save", tagentfile.SaveFileToolName, `{"file_name":"notes/written.txt","contents":"written\n","overwrite":true}`),
					ntCall("call-replace", tagentfile.ReplaceContentToolName, `{"file_name":"notes/draft.txt","old_string":"two","new_string":"three"}`),
					ntCall("call-read", tagentfile.ReadFileToolName, `{"file_name":"notes/extra.md"}`),
					ntCall("call-read-many", tagentfile.ReadMultipleFilesToolName, `{"patterns":["notes/*.md"]}`),
					ntCall("call-list", tagentfile.ListFileToolName, `{"path":"notes"}`),
					ntCall("call-search-file", tagentfile.SearchFileToolName, `{"path":"notes","pattern":"*.md"}`),
					ntCall("call-search-content", tagentfile.SearchContentToolName, `{"path":"notes","file_pattern":"*.md","content_pattern":"extra"}`),
				)
			}
			if len(req.Tools) != len(tagentfile.Names()) {
				t.Errorf("second request tools = %d (%v), want exactly the seven declared file tools", len(req.Tools), req.Tools)
			}
			for _, name := range tagentfile.Names() {
				if req.Tools[name] == nil {
					t.Errorf("second request tools carry no %q, want every declared file id granted", name)
				}
			}
			for _, want := range []struct{ callID, tool, body string }{
				{callID: "call-save", tool: tagentfile.SaveFileToolName, body: "Successfully saved: notes/written.txt"},
				{callID: "call-replace", tool: tagentfile.ReplaceContentToolName, body: "replaced"},
				{callID: "call-read", tool: tagentfile.ReadFileToolName, body: "extra line"},
				{callID: "call-read-many", tool: tagentfile.ReadMultipleFilesToolName, body: "Read 1 file(s)"},
				{callID: "call-list", tool: tagentfile.ListFileToolName, body: "extra.md"},
				{callID: "call-search-file", tool: tagentfile.SearchFileToolName, body: "extra.md"},
				{callID: "call-search-content", tool: tagentfile.SearchContentToolName, body: "Found 1 files matching"},
			} {
				checkObservedBody(t, req, want.callID, want.tool, want.body)
			}
			return ntFinalTurn("the declared file tools answered")
		}}

		run, err := assembly.Assemble(ctx, fileFaceDeclaration(root), map[event.AgentID]assembly.NativeAgent{
			"agent-file": {Model: provider},
		}, assembly.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		turn, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-file-face",
			Address:      event.Address{AgentID: "agent-file", SessionID: "session-file-face"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "edit the notes"}},
		})
		if err != nil {
			t.Fatalf("invoke the file face: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		written, err := os.ReadFile(filepath.Join(root, "notes", "written.txt"))
		if err != nil || string(written) != "written\n" {
			t.Fatalf("written file = (%q, %v), want save_file to write the real file under the base directory", written, err)
		}
		replaced, err := os.ReadFile(filepath.Join(root, "notes", "draft.txt"))
		if err != nil || string(replaced) != "one\nthree\n" {
			t.Fatalf("replaced file = (%q, %v), want replace_content to edit the real file in place", replaced, err)
		}
	})

	t.Run("a path outside the base directory is refused instead of served", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		outside := t.TempDir()
		outsideFile := filepath.Join(outside, "escape.txt")
		if err := os.WriteFile(outsideFile, []byte("outside\n"), 0o600); err != nil {
			t.Fatalf("write the outside file: %v", err)
		}
		root := t.TempDir()

		round := 0
		provider := &ntModel{name: "model-file", answer: func(req *model.Request) *model.Response {
			round++
			if round == 1 {
				return ntToolTurn(
					ntCall("call-climb", tagentfile.ReadFileToolName, `{"file_name":"../escape.txt"}`),
					ntCall("call-absolute", tagentfile.ReadFileToolName, `{"file_name":`+quoteJSON(outsideFile)+`}`),
					ntCall("call-write-climb", tagentfile.SaveFileToolName, `{"file_name":"../escape.txt","contents":"overwritten\n","overwrite":true}`),
				)
			}
			checkObservedBody(t, req, "call-climb", tagentfile.ReadFileToolName, "'..'")
			checkObservedBody(t, req, "call-absolute", tagentfile.ReadFileToolName, "outside base_directory")
			checkObservedBody(t, req, "call-write-climb", tagentfile.SaveFileToolName, "'..'")
			return ntFinalTurn("the outside paths were refused")
		}}

		run, err := assembly.Assemble(ctx, fileFaceDeclaration(root), map[event.AgentID]assembly.NativeAgent{
			"agent-file": {Model: provider},
		}, assembly.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		if _, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-file-face-refusal",
			Address:      event.Address{AgentID: "agent-file", SessionID: "session-file-face-refusal"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "read the file above the root"}},
		}); err != nil {
			t.Fatalf("invoke the file face: %v", err)
		}
		untouched, err := os.ReadFile(outsideFile)
		if err != nil || string(untouched) != "outside\n" {
			t.Fatalf("outside file = (%q, %v), want the refused write to leave it untouched", untouched, err)
		}
	})
}

// fileFaceDeclaration declares one agent whose file face is rooted at base and
// that grants every file tool the core answers under a declared id.
func fileFaceDeclaration(base string) *config.Declaration {
	tools := make([]config.ToolRef, 0, len(tagentfile.Names()))
	for _, name := range tagentfile.Names() {
		tools = append(tools, config.ToolRef{Name: name})
	}
	return &config.Declaration{
		Entry:  "agent-file",
		Agents: []config.AgentSpec{fileFaceSpec(base, tools)},
	}
}

// fileFaceSpec is one agent spec whose file face is rooted at base.
func fileFaceSpec(base string, tools []config.ToolRef) config.AgentSpec {
	spec := config.AgentSpec{
		ID:    "agent-file",
		Model: "model-file",
		Tools: tools,
	}
	spec.File = config.FileSpec{BaseDir: base}
	return spec
}

// checkObservedBody checks the settled observation of one call carries the
// named body the real execution answered with.
func checkObservedBody(t *testing.T, req *model.Request, callID, tool, body string) {
	t.Helper()
	for _, msg := range req.Messages {
		if msg.ToolID != callID || msg.Role == model.RoleTool {
			continue
		}
		if msg.ToolName != tool {
			t.Errorf("observation of %q carries tool %q, want %q", callID, msg.ToolName, tool)
		}
		if !strings.Contains(msg.Content, body) {
			t.Errorf("observation of %q = %q, want it to carry %q", callID, msg.Content, body)
		}
		return
	}
	t.Errorf("no settled observation of %q in %+v, want the real result of %q", callID, req.Messages, tool)
}

// quoteJSON quotes one string as a JSON literal for the scripted call
// arguments of these sub-cases.
func quoteJSON(text string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(text, `\`, `\\`), `"`, `\"`) + `"`
}
