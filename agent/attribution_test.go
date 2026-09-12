package agent

import (
	"context"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
)

// TestBuildTurnAttribution_TriggerSource guards the meditation-identity
// persistence prerequisite (event-sourced-projection D3): the trigger source
// must enter the turn attribution so MemoryPlugin persists it into
// agent_output Metadata — otherwise the projection-rebuild meditation reseed
// condition Metadata[trigger_source]==meditation can never fire (previously
// it only lived on the in-memory StateDelta).
func TestBuildTurnAttribution_TriggerSource(t *testing.T) {
	cm := &ContextManager{
		sessionID:     "sess-1",
		triggerSource: "meditation",
		bundleIDFn:    func() string { return "bundle-1" },
	}
	attr := cm.buildTurnAttribution(context.Background())
	require.Equal(t, "sess-1", attr[tagentevent.MetaKeyRolloutID])
	require.Equal(t, "bundle-1", attr[tagentevent.MetaKeyBundleID])
	require.Equal(t, "meditation", attr[tagentevent.MetaKeyTriggerSource],
		"trigger source must enter attribution so it persists into agent_output Metadata")

	// Empty trigger source: no stamp (user turns carry no meditation identity).
	plain := &ContextManager{sessionID: "sess-2"}
	attr = plain.buildTurnAttribution(context.Background())
	require.Equal(t, "sess-2", attr[tagentevent.MetaKeyRolloutID])
	_, stamped := attr[tagentevent.MetaKeyTriggerSource]
	require.False(t, stamped, "empty trigger source must not stamp")
}
