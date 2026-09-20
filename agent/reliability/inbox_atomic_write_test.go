package reliability

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// §3.2 (fail-before): an atomic write whose rename lands but whose directory
// sync fails is "publish uncertain" — the original MUST be retained, its
// reserved capacity kept, and no subsequent input may reuse the sequence to
// overwrite it. The pre-§3.2 Enqueue rolled the sequence back on any write
// error, so the next input reused the same path and destroyed the landed
// original. This test asserts the spec-mandated retention.
func TestInbox_PublishUncertainRetainsOriginalAndNoOverwrite(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)

	// Force the POST-RENAME dir sync to fail for the first enqueue only.
	origSync := syncDirFunc
	syncDirFunc = func(string) error { return errors.New("injected dirsync failure") }
	seq1, err1 := in.Enqueue(env("u1", "user"))
	syncDirFunc = origSync

	require.Error(t, err1, "a publish-uncertain write must NOT report durable acceptance")
	require.Equal(t, int64(0), seq1, "an unaccepted receive reports no sequence")

	// The landed original must be retained and its capacity reserved.
	require.Equal(t, int64(1), in.Pending(),
		"the durable original left by an uncertain write keeps its reserved capacity")
	env1Path := filepath.Join(in.dir, "00000000000000000001.json")
	e1, rerr := readEnvelope(env1Path)
	require.NoError(t, rerr, "the original file must still exist at its sequence path")
	require.Equal(t, "u1", e1.RequestID, "the retained original must be u1")

	// A subsequent successful enqueue must NOT reuse the failed sequence — it
	// would overwrite the retained original (the §3.2 bug).
	seq2, err2 := in.Enqueue(env("u2", "user"))
	require.NoError(t, err2)
	require.Greater(t, seq2, seq1, "the next input gets a fresh, higher sequence")
	require.NotEqual(t, seq2, int64(1), "sequence 1 is reserved by the uncertain original and must not be reused")

	e1again, rerr2 := readEnvelope(env1Path)
	require.NoError(t, rerr2, "the retained original must survive the next enqueue")
	require.Equal(t, "u1", e1again.RequestID, "u2 must NOT overwrite u1's original (no sequence reuse)")

	require.Equal(t, int64(2), in.Pending(), "both the retained uncertain original and u2 occupy capacity")
}
