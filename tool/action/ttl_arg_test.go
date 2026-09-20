package action

import (
	"testing"
	"time"
)

// TestResolveTTL covers async-task-lifetime 10.2: the effective absolute lifetime
// for a spawn is explicit ttl (seconds) when > 0, else the configured default,
// else the 10-minute floor — and there is no value that disables the reaper.
func TestResolveTTL(t *testing.T) {
	ct := NewActionTool(WithActionWorkspace(t.TempDir()), WithOrphanCleanupDisabled())

	// Constructor default = 10 minutes when no operator default is set.
	if got := ct.resolveTTL(ActionArgs{}); got != 10*time.Minute {
		t.Fatalf("omitted ttl (no configured default) = %v, want 10m floor", got)
	}
	// Explicit ttl wins, in seconds.
	if got := ct.resolveTTL(ActionArgs{TTL: 45}); got != 45*time.Second {
		t.Fatalf("explicit ttl = %v, want 45s", got)
	}
	// A configured default applies only when ttl is omitted (0).
	ct.SetDefaultTaskTTL(2 * time.Hour)
	if got := ct.resolveTTL(ActionArgs{}); got != 2*time.Hour {
		t.Fatalf("configured default (ttl omitted) = %v, want 2h", got)
	}
	// Explicit ttl still overrides the configured default.
	if got := ct.resolveTTL(ActionArgs{TTL: 30}); got != 30*time.Second {
		t.Fatalf("explicit ttl over configured default = %v, want 30s", got)
	}
	// A non-positive default is ignored: the previously set default (and the floor)
	// survive — there is no "0 = unlimited" interpretation.
	ct.SetDefaultTaskTTL(0)
	if got := ct.resolveTTL(ActionArgs{}); got != 2*time.Hour {
		t.Fatalf("SetDefaultTaskTTL(0) must not clear the default, got %v", got)
	}
}

// TestDeclarationExposesTTL verifies the model-facing schema advertises ttl.
func TestDeclarationExposesTTL(t *testing.T) {
	ct := NewActionTool(WithActionWorkspace(t.TempDir()), WithOrphanCleanupDisabled())
	props := ct.Declaration().InputSchema.Properties
	if props == nil {
		t.Fatal("declaration must carry an input schema")
	}
	if _, ok := props["ttl"]; !ok {
		t.Fatal("Declaration must expose the ttl parameter")
	}
}
