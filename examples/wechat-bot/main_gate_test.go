package main

import "testing"

// Tests for resolveTriggerSource — the fail-closed delivery gate
// (unified-event-delivery, 2026-09-15).
func TestResolveTriggerSource_FailClosed(t *testing.T) {
	// Unstamped → internal, NOT deliverable (the 03:20/04:52 leak class).
	src, ok := resolveTriggerSource("")
	if ok {
		t.Fatalf("unstamped must not be deliverable, got ok=true")
	}
	if src != "internal-unstamped" {
		t.Fatalf("unstamped source = %q, want internal-unstamped", src)
	}
}

func TestResolveTriggerSource_UserStampedDelivers(t *testing.T) {
	// Real user turns are stamped at the RunFlow forwarding block — must
	// still deliver (anti-overreach guard).
	for _, s := range []string{"user", "task"} {
		src, ok := resolveTriggerSource(s)
		if !ok || src != s {
			t.Fatalf("stamped %q: got (%q,%v), want (%q,true)", s, src, ok, s)
		}
	}
}

func TestResolveTriggerSource_InternalStampedHeld(t *testing.T) {
	// Stamped internal sources pass through unchanged; delivery decision
	// stays with the dispatch switch (meditation/error are log-only there).
	src, ok := resolveTriggerSource("meditation")
	if !ok || src != "meditation" {
		t.Fatalf("meditation: got (%q,%v)", src, ok)
	}
}

// TestResolveTriggerSource_HostNoticesDeliver (B-fix, 2026-09-16): host
// notices stamped with dedicated sources must stay deliverable — the old
// code borrowed "meditation" for these and the delivery gate's internal
// branch silently withheld their output.
func TestResolveTriggerSource_HostNoticesDeliver(t *testing.T) {
	for _, s := range []string{"reincarnation", "system_alert"} {
		src, ok := resolveTriggerSource(s)
		if !ok || src != s {
			t.Fatalf("host notice %q: got (%q,%v), want passthrough", s, src, ok)
		}
	}
}

// hardening-review-batch2 1.3/1.4：未知来源与框架降级的 task-unstamped 必须
// 扣留（fail-closed 白名单）；meditation 双层语义不变。
func TestResolveTriggerSource_UnknownAndUnstampedHeld(t *testing.T) {
	if src, ok := resolveTriggerSource("task-unstamped"); ok {
		t.Fatalf("task-unstamped (lineage_absent degrade) must be held, got (%q,%v)", src, ok)
	}
	for _, s := range []string{"unknown-future-value", "weird"} {
		if src, ok := resolveTriggerSource(s); ok {
			t.Fatalf("unknown source %q must be held, got (%q,%v)", s, src, ok)
		}
	}
	if src, ok := resolveTriggerSource("meditation"); !ok || src != "meditation" {
		t.Fatalf("meditation passthrough semantics must be preserved, got (%q,%v)", src, ok)
	}
}
