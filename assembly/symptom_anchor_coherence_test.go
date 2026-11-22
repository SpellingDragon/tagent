package assembly

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// symptomAnchorObserver is the run-level face the host-entry anchor contributes:
// it records every committed fact the fact->observation pass dispatches it.
type symptomAnchorObserver struct {
	name string
	mu   sync.Mutex
	seen []event.EventID
}

// Name returns the stable name of this instance.
func (o *symptomAnchorObserver) Name() string { return o.name }

// Port reports the port this instance serves.
func (o *symptomAnchorObserver) Port() plugin.Port { return plugin.PortFactObservation }

// ObserveCommit records one committed fact the observation pass dispatched.
func (o *symptomAnchorObserver) ObserveCommit(_ context.Context, id event.EventID) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = append(o.seen, id)
	return nil
}

// observed reports the facts the pass dispatched, in dispatch order.
func (o *symptomAnchorObserver) observed() []event.EventID {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]event.EventID(nil), o.seen...)
}

// TestSymptomAnchorHostBootMountsContributedPlugin is the host-entry symptom anchor: the public boot entry mounts the plugin instance a caller contributes.
//   - positive: Boot meets a WithPlugins contribution and the fact->observation pass of the driven turn dispatches the instance that contribution built
//   - negative: every dispatched identity reads back as a committed fact of the booted run and one of them belongs to the driven session
//   - so the pass observed this run's commits rather than a set the check supplied
//   - the contribution is built exactly once, so a boot that mounts it neither skips the factory nor runs it twice
//   - the booted run commits the facts of that turn regardless, so an empty observation is the dropped contribution and not a turn that never ran
//   - consumer: Boot is the public host entry and Run.Invoke the run entry it publishes, never Assemble direct nor a pipeline the check wires itself
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestSymptomAnchorHostBootMountsContributedPlugin(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	document := generationDocument(root, provider.URL+"/v1", "")
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	contributed := &symptomAnchorObserver{name: "symptom-anchor-contributed"}
	builds := 0
	host, err := Boot(configPath, WithPlugins(func(*Run) ([]plugin.Instance, error) {
		builds++
		return []plugin.Instance{contributed}, nil
	}))
	if err != nil {
		t.Fatalf("boot the host beside the contributed instance: %v", err)
	}
	t.Cleanup(func() { closeLiveness(t, host) })

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-symptom-anchor-boot"}
	turn, err := host.Run().Invoke(ctx, InvokeRequest{
		InvocationID:  event.InvocationID("inv-symptom-anchor-boot"),
		Address:       addr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: "the one request of the anchor"}},
	})
	if err != nil {
		t.Fatalf("drive the one turn of the anchor: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("terminal of the driven turn = %q, want the completed answer", turn.Terminal)
	}
	session := host.Run().SessionFacts(addr)
	if len(session) == 0 {
		t.Fatal("the booted run committed no fact of the driven turn, so the check cannot tell a dropped contribution from a turn that never ran")
	}
	observed := contributed.observed()
	if len(observed) == 0 {
		t.Fatalf("the contributed instance observed no fact of the driven turn (its builder ran %d times): the boot dropped the WithPlugins contribution and the run-level set it published carries the host's own instances alone, or the published binding never dispatched the instance it built", builds)
	}
	if builds != 1 {
		t.Fatalf("the contributed builder ran %d times, want exactly once per boot", builds)
	}
	inSession := make(map[event.EventID]bool, len(session))
	for _, fact := range session {
		inSession[fact.Key] = true
	}
	turned := 0
	for _, id := range observed {
		if _, err := host.Run().CommittedEvent(ctx, id); err != nil {
			t.Fatalf("read the observed fact %s back from the booted run: %v", id, err)
		}
		if inSession[id] {
			turned++
		}
	}
	if turned == 0 {
		t.Fatalf("the dispatched facts %v name none of the driven session %v, want the commits of the turn the anchor drove", observed, addr)
	}
}
