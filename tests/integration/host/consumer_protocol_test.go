package host

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/telemetry"
)

// errProtocolFactMissing is the read-side gap of these checks: an identity with
// no committed fact behind it.
var errProtocolFactMissing = errors.New("no committed fact behind the identity")

// protocolFactStore is the read-only fact side of these checks: it answers one
// committed fact by identity and counts every read, every commit and every
// audit record it lands, so the observation's read-only duty is checkable.
type protocolFactStore struct {
	facts   map[event.EventID]event.Event
	reads   int
	commits int
	audits  []telemetry.AuditRecord
}

// CommittedFact answers the fact behind one identity, counting the read.
func (s *protocolFactStore) CommittedFact(_ context.Context, id event.EventID) (event.Event, error) {
	s.reads++
	fact, ok := s.facts[id]
	if !ok {
		return event.Event{}, errProtocolFactMissing
	}
	return fact, nil
}

// Audit lands one layered audit record, counting nothing as a fact.
func (s *protocolFactStore) Audit(_ context.Context, record telemetry.AuditRecord) error {
	s.audits = append(s.audits, record)
	return nil
}

// TestSecondWaveConsumersShareTheProtocol pins the observation consumer to the shared consumer protocol.
// - the observer reads one committed fact by identity and hands it unchanged to the layered audit face
// - the audit record carries the fact identity and the skipped layers of a disabled OTel
// - the observer is read-only: it commits no fact, keeps the source fact unchanged, and refuses an identity that never committed
// 契约: docs/EXTENSIONS.md#consumer-protocol
func TestSecondWaveConsumersShareTheProtocol(t *testing.T) {
	ctx := context.Background()
	committed := event.Event{
		Key:       event.EventID("fact-observer"),
		Type:      event.TypeTaskSettled,
		Timestamp: time.Unix(0, 0).UTC(),
	}
	store := &protocolFactStore{
		facts:   map[event.EventID]event.Event{committed.Key: committed},
		commits: 1,
	}
	observer, err := telemetry.NewSDKObserver(store, telemetry.WithOTelDisabled())
	if err != nil {
		t.Fatalf("assemble the observer = %v, want one observation face", err)
	}
	consumer := plugin.NewCommitObserver(store, observer)

	if err := consumer.ObserveCommit(ctx, committed.Key); err != nil {
		t.Fatalf("observe the committed fact = %v, want the observation landed", err)
	}
	if store.reads != 1 {
		t.Fatalf("fact reads = %d, want the one read of the identity it observed", store.reads)
	}
	wantAttempts := []telemetry.Attempt{
		{Layer: telemetry.LayerTrace, Skipped: true},
		{Layer: telemetry.LayerMetric, Skipped: true},
	}
	if len(store.audits) != 1 || store.audits[0].Fact != committed.Key {
		t.Fatalf("audit records = %+v, want one record naming %q", store.audits, committed.Key)
	}
	if !reflect.DeepEqual(store.audits[0].Attempts, wantAttempts) {
		t.Fatalf("audit attempts = %+v, want the skipped layer order %+v", store.audits[0].Attempts, wantAttempts)
	}
	if got := store.facts[committed.Key]; !reflect.DeepEqual(got, committed) {
		t.Fatalf("source fact after the observation = %+v, want %+v unchanged", got, committed)
	}
	if store.commits != 1 || len(store.facts) != 1 {
		t.Fatalf("facts after the observation = %d commits / %d facts, want the observation to produce no fact",
			store.commits, len(store.facts))
	}

	if err := consumer.ObserveCommit(ctx, event.EventID("fact-never-committed")); !errors.Is(err, errProtocolFactMissing) {
		t.Fatalf("observe an uncommitted identity = %v, want the named refusal", err)
	}
	if store.commits != 1 || len(store.facts) != 1 || len(store.audits) != 1 {
		t.Fatalf("store after the refused observation = %d commits / %d facts / %d audits, want nothing produced",
			store.commits, len(store.facts), len(store.audits))
	}
}
