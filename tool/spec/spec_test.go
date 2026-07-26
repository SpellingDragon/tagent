package spec

import (
	"context"
	"strings"
	"testing"
)

// TestBuildArgv_NoShellInterpolation: each op maps to a fixed argv whose
// structure the model cannot influence — name/artifact are discrete entries,
// never interpolated into a shell string. This is the core safety property.
func TestBuildArgv_NoShellInterpolation(t *testing.T) {
	b := &openspecBackend{bin: "openspec"}
	cases := []struct {
		req  Request
		want []string
	}{
		{Request{Op: OpInit}, []string{"init", "--tools", "none"}},
		{Request{Op: OpNew, Name: "my-plan"}, []string{"new", "change", "my-plan"}},
		{Request{Op: OpStatus, Name: "p", JSON: true}, []string{"status", "--change", "p", "--json"}},
		{Request{Op: OpValidate, Name: "p"}, []string{"validate", "p", "--strict"}},
		{Request{Op: OpArchive, Name: "p"}, []string{"archive", "p"}},
		{Request{Op: OpInstructions, Artifact: "specs", Name: "p"}, []string{"instructions", "specs", "--change", "p"}},
		{Request{Op: OpList}, []string{"list"}},
	}
	for _, c := range cases {
		got, err := b.buildArgv(c.req)
		if err != nil {
			t.Fatalf("buildArgv(%+v): %v", c.req, err)
		}
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("buildArgv(%+v) = %v, want %v", c.req, got, c.want)
		}
	}
}

// TestBuildArgv_MissingRequired: ops that need name/artifact reject empty ones.
func TestBuildArgv_MissingRequired(t *testing.T) {
	b := &openspecBackend{bin: "openspec"}
	for _, op := range []Op{OpNew, OpValidate, OpArchive} {
		if _, err := b.buildArgv(Request{Op: op}); err == nil {
			t.Errorf("op %q without name must error", op)
		}
	}
	if _, err := b.buildArgv(Request{Op: OpInstructions}); err == nil {
		t.Errorf("instructions without artifact must error")
	}
}

// TestRun_UnknownOpRejected: the dispatch whitelist blocks unknown ops before
// any process is spawned.
func TestRun_UnknownOpRejected(t *testing.T) {
	b := NewOpenSpecBackend()
	_, err := b.Run(context.Background(), Request{Op: Op("rm -rf /")})
	if err == nil || !strings.Contains(err.Error(), "unknown op") {
		t.Fatalf("unknown op must be rejected, got err=%v", err)
	}
}

// TestRun_MissingBinary: a non-existent binary yields an actionable error
// (the model has no shell to self-install), not a panic or hang.
func TestRun_MissingBinary(t *testing.T) {
	b := NewOpenSpecBackend(WithOpenSpecBin("definitely-not-a-real-binary-xyz"))
	_, err := b.Run(context.Background(), Request{Op: OpList})
	if err == nil {
		t.Fatal("missing binary must error")
	}
	if !strings.Contains(err.Error(), "openspec CLI installed") {
		t.Errorf("error must guide toward provisioning, got: %v", err)
	}
}
