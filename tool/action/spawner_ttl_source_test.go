package action

import (
	"testing"
	"time"
)

// TestSpawnerTTLSourceIsLiveAuthority pins the §6.4 spawner axis (introduce-
// durable-workflow-engine 6.4): the default absolute lifetime for spawns that
// omit `ttl` is RESOLVED AT SPAWN TIME from the owner's committed application
// record — it is not a value someone has to remember to push into this tool.
//
// Before this, ActionTool held its own mutable defaultTTL written by
// SetDefaultTaskTTL at construction and at each structural publish/rollback, so
// a numeric-only change to task_default_ttl reached the TaskManager (which pulls)
// while every new spawn kept the stale pushed number — two authorities for one
// knob, and a non-atomic field written on the reload goroutine while business
// turns read it. Rotation here is a plain assignment to the captured variable,
// which is exactly how the composition root rotates it (record swap).
func TestSpawnerTTLSourceIsLiveAuthority(t *testing.T) {
	cur := 2 * time.Hour
	ct := NewActionTool()
	ct.SetDefaultTTLSource(func() time.Duration { return cur })

	if got := ct.resolveTTL(ActionArgs{}); got != 2*time.Hour {
		t.Fatalf("spawn with omitted ttl = %v, want the source's 2h", got)
	}

	// Rotate the record only — no write into the tool.
	cur = 45 * time.Minute
	if got := ct.resolveTTL(ActionArgs{}); got != 45*time.Minute {
		t.Fatalf("after source rotation = %v, want 45m (the source is the authority, no push)", got)
	}

	// An explicit ttl argument still outranks the source.
	if got := ct.resolveTTL(ActionArgs{TTL: 10}); got != 10*time.Second {
		t.Fatalf("explicit ttl = %v, want 10s", got)
	}

	// A zero reading means the record has no opinion → the construction default
	// answers, never a zero/unbounded lifetime.
	cur = 0
	if got := ct.resolveTTL(ActionArgs{}); got != defaultTaskTTL {
		t.Fatalf("zero source reading = %v, want the construction default %v", got, defaultTaskTTL)
	}
}

// TestSpawnerTTLSourceAbsentKeepsConstructionDefault is the no-source boundary
// (a tool built outside any org composition root): the construction default must
// keep answering, so the pull contract cannot require a source to exist.
func TestSpawnerTTLSourceAbsentKeepsConstructionDefault(t *testing.T) {
	ct := NewActionTool()
	if got := ct.resolveTTL(ActionArgs{}); got != defaultTaskTTL {
		t.Fatalf("no source = %v, want construction default %v", got, defaultTaskTTL)
	}
}
