package event

import (
	"bytes"
	"testing"
)

// TestRecoveryTicketDeterministic pins the deterministic identity and payload.
// - the same coverage files the same identity and the same canonical bytes
// - a different cursor or digest changes the identity
// 契约: docs/FACT-VOCABULARY.md#families
func TestRecoveryTicketDeterministic(t *testing.T) {
	first := RecoveryTicketFor(1, "tagent/task/v1/fact:task-a:settle-1", "sha256:abcd", 1024)
	second := RecoveryTicketFor(1, "tagent/task/v1/fact:task-a:settle-1", "sha256:abcd", 1024)
	if first.FactKey() != second.FactKey() {
		t.Fatalf("identity drifted: %q vs %q", first.FactKey(), second.FactKey())
	}
	if !bytes.Equal(first.Payload(), second.Payload()) {
		t.Fatalf("payload drifted: %q vs %q", first.Payload(), second.Payload())
	}
	if first.FactType() != RecoveryTicketType {
		t.Fatalf("type = %q, want the frozen word", first.FactType())
	}
	otherCursor := RecoveryTicketFor(1, "tagent/task/v1/fact:task-a:settle-2", "sha256:abcd", 1024)
	otherDigest := RecoveryTicketFor(1, "tagent/task/v1/fact:task-a:settle-1", "sha256:ffff", 1024)
	if otherCursor.FactKey() == first.FactKey() || otherDigest.FactKey() == first.FactKey() {
		t.Fatal("a different cursor or digest kept the identity")
	}
}
