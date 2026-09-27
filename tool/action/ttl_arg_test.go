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
	// A configured default applies only when ttl is omitted (0). §6.4 (6.4
	// spawner axis): it now arrives as a LIVE source reading (the owner's
	// committed record), not a number pushed into the tool.
	cur := 2 * time.Hour
	ct.SetDefaultTTLSource(func() time.Duration { return cur })
	if got := ct.resolveTTL(ActionArgs{}); got != 2*time.Hour {
		t.Fatalf("configured default (ttl omitted) = %v, want 2h", got)
	}
	// Explicit ttl still overrides the configured default.
	if got := ct.resolveTTL(ActionArgs{TTL: 30}); got != 30*time.Second {
		t.Fatalf("explicit ttl over configured default = %v, want 30s", got)
	}
	// A zero source reading means the record has no opinion → the construction
	// default answers; "0 = unlimited" is not an interpretation either way. (The
	// retired setter instead kept the last pushed value; that stickiness died with
	// it because the record is now the single authority.)
	cur = 0
	if got := ct.resolveTTL(ActionArgs{}); got != 10*time.Minute {
		t.Fatalf("zero source reading must fall back to the construction default, got %v", got)
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
