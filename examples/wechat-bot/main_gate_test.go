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
