package event

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// §5.5 — the non-projection classification is registry-declared and shared by
// ALL append paths (normal commit, spill replay, cold-start rebuild). This test
// pins the declaration set itself: adding an internal record type means adding
// one NonProjection flag here, not editing three call sites' private enums.

func TestIsNonProjectionEventType_Declarations(t *testing.T) {
	// Current internal records: fact-chain bookkeeping, never projection refs.
	require.True(t, IsNonProjectionEventType(TypeInboxReceipt), "inbox receipt is the current internal record class")
	require.True(t, IsNonProjectionEventType(TypeTaskSpawned), "task_spawned is registry data")
	require.True(t, IsNonProjectionEventType(TypeResidentSession), "resident_session is an audit record")
	require.True(t, IsNonProjectionEventType(TypeContextCompressSummary), "the compaction event body is re-folded from its payload")

	// Business events stay projectable.
	require.False(t, IsNonProjectionEventType(TypeExternalInput))
	require.False(t, IsNonProjectionEventType(TypeAgentOutput))
	require.False(t, IsNonProjectionEventType(TypeActionCommand))

	// An unknown type falls back conservatively: projectable (the historical
	// exclude-whitelist semantics — never a silent drop of real history).
	require.False(t, IsNonProjectionEventType("some_future_business_type"))
}

func TestIsNonProjectionRecord_MetadataClause(t *testing.T) {
	// The inline settle marker is the metadata half of the single predicate.
	require.True(t, IsNonProjectionRecord(TypeAgentOutput, map[string]string{MetaKeyTaskInlineRecord: "true"}))
	require.False(t, IsNonProjectionRecord(TypeAgentOutput, map[string]string{MetaKeyTaskInlineRecord: ""}))
	require.False(t, IsNonProjectionRecord(TypeAgentOutput, nil))
	require.False(t, IsNonProjectionRecord(TypeExternalInput, map[string]string{"unrelated": "x"}))
}
