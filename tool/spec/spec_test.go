package spec

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// recordingBackend is the port stub the tool's own tests drive: it records the
// request it received and answers with a fixed result.
type recordingBackend struct {
	requests []Request
	result   Result
}

// Run records one request and answers with the stub result.
func (b *recordingBackend) Run(_ context.Context, req Request) (Result, error) {
	b.requests = append(b.requests, req)
	return b.result, nil
}

// Name reports the stub identity.
func (b *recordingBackend) Name() string { return "recording" }

// WorkspaceRoot reports the stub storage root.
func (b *recordingBackend) WorkspaceRoot() string { return "recorded-plans" }

// TestToolDeclaresTheClosedSet pins the model-visible surface: one enumerated operation field over the dispatch set, beside the fields the operations consume.
// - the declared name is the name the prompt and the declaration share
// - the op field is enumerated over every operation of the dispatch set
// - op alone is required; name, artifact and json are optional
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestToolDeclaresTheClosedSet(t *testing.T) {
	decl := NewTool(&recordingBackend{}).Declaration()
	if decl.Name != ToolName {
		t.Fatalf("declared name = %q, want %q", decl.Name, ToolName)
	}
	op, ok := decl.InputSchema.Properties["op"]
	if !ok {
		t.Fatal("the declaration carries no op field")
	}
	want := []any{
		string(OpInit), string(OpNew), string(OpStatus), string(OpValidate),
		string(OpArchive), string(OpInstructions), string(OpList),
	}
	if len(op.Enum) != len(want) {
		t.Fatalf("op enum = %v, want %v", op.Enum, want)
	}
	for i, w := range want {
		if op.Enum[i] != w {
			t.Fatalf("op enum = %v, want %v", op.Enum, want)
		}
	}
	if len(decl.InputSchema.Required) != 1 || decl.InputSchema.Required[0] != "op" {
		t.Fatalf("required fields = %v, want op alone", decl.InputSchema.Required)
	}
	for _, field := range []string{"name", "artifact", "json"} {
		if _, ok := decl.InputSchema.Properties[field]; !ok {
			t.Fatalf("the declaration carries no %q field", field)
		}
	}
}

// modelFaceDescription is the frozen declaration description: the model-visible
// surface names plans and their workspace, never the backend a deployment wired,
// so a wording change here is a prompt change and a declaration change in one
// breath.
const modelFaceDescription = "manage work plans without a shell: one typed operation per call, covering the whole plan lifecycle. " +
	"The operations prepare the plan workspace (idempotent), create a plan (name required), " +
	"report the artifact state of a plan (name optional) or of the workspace, " +
	"check a plan under the strict rule set (name required; plans carrying no spec deltas fail by design), " +
	"close a plan whose tasks are all checked (name required), " +
	"return the writing instructions of one artifact (artifact required: proposal/specs/design/tasks), " +
	"and list the plans of the workspace. Plan files are written and read through the file tools, not through this one."

// TestDeclarationSpeaksThePlanLanguage pins the frozen model-visible description.
// - the description is the plan-language text, so an implementation name cannot slip in unnoticed
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestDeclarationSpeaksThePlanLanguage(t *testing.T) {
	got := NewTool(&recordingBackend{}).Declaration().Description
	if got != modelFaceDescription {
		t.Fatalf("declaration description = %q, want the frozen plan-language text", got)
	}
}

// TestToolDispatchesATypedRequest pins that one call reaches the backend as the typed request its arguments describe.
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestToolDispatchesATypedRequest(t *testing.T) {
	backend := &recordingBackend{result: Result{Op: OpStatus, ExitCode: 0, OK: true, Output: "ok"}}
	tool := NewTool(backend)
	got, err := tool.Call(context.Background(), []byte(`{"op":"status","name":"p","artifact":"tasks","json":true}`))
	if err != nil {
		t.Fatalf("call = %v, want a dispatched operation", err)
	}
	if res, ok := got.(Result); !ok || !res.OK || res.Op != OpStatus {
		t.Fatalf("call = (%#v, %v), want the backend result", got, err)
	}
	want := Request{Op: OpStatus, Name: "p", Artifact: "tasks", JSON: true}
	if len(backend.requests) != 1 || backend.requests[0] != want {
		t.Fatalf("backend received %+v, want %+v", backend.requests, want)
	}
}

// TestToolRefusesTheClosedSetExteriorByName pins the refusal of every call the surface does not admit.
// - an operation outside the set is refused by name, and the backend sees nothing
// - arguments of no declared shape are refused as bad arguments
// - a tool beside no backend refuses the call as such, and no operation runs
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestToolRefusesTheClosedSetExteriorByName(t *testing.T) {
	backend := &recordingBackend{}
	if _, err := NewTool(backend).Call(context.Background(), []byte(`{"op":"rm -rf /"}`)); !errors.Is(err, ErrUnknownOp) {
		t.Fatalf("operation outside the set = %v, want %v", err, ErrUnknownOp)
	}
	if len(backend.requests) != 0 {
		t.Fatalf("backend received %+v, want no request for a refused operation", backend.requests)
	}
	if _, err := NewTool(backend).Call(context.Background(), []byte(`{"op":`)); !errors.Is(err, ErrBadArguments) {
		t.Fatalf("arguments of no declared shape = %v, want %v", err, ErrBadArguments)
	}
	if _, err := NewTool(nil).Call(context.Background(), []byte(`{"op":"status"}`)); !errors.Is(err, ErrBackendMissing) {
		t.Fatalf("tool beside no backend = %v, want %v", err, ErrBackendMissing)
	}
}

// TestValidPinsTheDispatchSet pins the dispatch predicate: every declared operation is valid and the empty operation is not.
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestValidPinsTheDispatchSet(t *testing.T) {
	for _, op := range []Op{OpInit, OpNew, OpStatus, OpValidate, OpArchive, OpInstructions, OpList} {
		if !Valid(op) {
			t.Errorf("Valid(%q) = false, want the declared set", op)
		}
	}
	for _, op := range []Op{"", "rm -rf /", "Status", "init "} {
		if Valid(op) {
			t.Errorf("Valid(%q) = true, want false", op)
		}
	}
	for _, op := range []Op{OpInit, OpNew, OpStatus, OpValidate, OpArchive, OpInstructions, OpList} {
		if !strings.Contains(allowedOps, string(op)) {
			t.Errorf("the refusal text %q does not name the declared operation %q", allowedOps, op)
		}
	}
}
