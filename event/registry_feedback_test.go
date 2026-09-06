package event

import "testing"

// TestFeedbackEventRegistered (2.1, design-report-closeout): feedback must
// be a registered type with the D1 semantics — recallable, not low-value,
// TTL 30 days, system role — so the whole pipeline (lifecycle/compaction/
// recall filtering) picks it up from the single registry point.
func TestFeedbackEventRegistered(t *testing.T) {
	spec, ok := LookupEventType(TypeFeedback)
	if !ok {
		t.Fatal("feedback not registered")
	}
	if !spec.Recallable || spec.LowValue {
		t.Fatalf("feedback spec wrong: %+v", spec)
	}
	if spec.TTLDays != 30 {
		t.Fatalf("feedback TTL = %d, want 30", spec.TTLDays)
	}
	if found := false; !found {
		for _, n := range RegisteredEventTypes() {
			if n == TypeFeedback {
				found = true
			}
		}
		if !found {
			t.Fatal("feedback missing from RegisteredEventTypes")
		}
	}
}

// TestGovernanceNotSkeletonized (5.3, design-report-closeout): governance
// events must NOT be skeleton-compressed — full text is the audit record and
// the rebuild source for the goal registry. Fail-before: spec was
// Skeleton:true.
func TestGovernanceNotSkeletonized(t *testing.T) {
	if IsSkeletonEventType(TypeGovernance) {
		t.Fatal("governance must not be skeletonized (audit/goal-rebuild needs full text)")
	}
}
