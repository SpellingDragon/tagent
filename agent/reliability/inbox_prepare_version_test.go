package reliability

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// §3.5 prepared-version gate. A slot's prepared_fact is frozen under an explicit
// prepare version; recovery must only trust current-version material. Incompatible
// transitional material (a foreign/absent version) is rejected at read so it can
// never be replayed through a legacy parser — while a pending slot with no material
// still runs its normal first prepare. Each fail-before is recorded in evidence.md.

// PrepareFacts must stamp the current prepare version onto the frozen slot so a
// later recovery can authenticate the material it replays.
func TestInbox_PrepareFactsStampsCurrentVersion(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("r1", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)
	// A pending→claimed envelope with no material prepares normally on first try.
	require.NoError(t, in.PrepareFacts(path, "rk-1",
		[]json.RawMessage{json.RawMessage(`{"event_key":111,"event_summary":"s"}`)}),
		"a no-material pending envelope must run its normal first prepare")

	reopened, err := readEnvelope(path)
	require.NoError(t, err)
	require.Equal(t, PreparedVersionCurrent, reopened.Messages[0].PreparedVersion,
		"prepare must stamp the current prepare version")
	require.NotEmpty(t, reopened.Messages[0].PreparedFact)
}

// A slot carrying material under a non-current (here absent/0) prepare version is
// incompatible transitional data: readEnvelope must reject it (→ quarantine),
// NOT silently consume it as if prepared. Before the version gate this read passed.
func TestInbox_ReadEnvelopeRejectsIncompatiblePreparedVersion(t *testing.T) {
	dir := t.TempDir()
	body := `{"version":2,"request_id":"r","source":"user","state":"claimed","receipt_key":"k",` +
		`"messages":[{"slot":0,"source_event":{"id":"e","content":"x"},` +
		`"prepared_fact":{"event_key":7,"event_summary":"s"}}]}` // prepared_fact present, prepared_version omitted (0)
	p := writeRawEnvelope(t, dir, "00000000000000000002.json", body)
	_, err := readEnvelope(p)
	require.Error(t, err, "material under an incompatible prepare version must be rejected, not consumed")
	require.Contains(t, err.Error(), "version")
}
