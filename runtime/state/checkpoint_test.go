// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
package state

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// recordingHolder is a state holder that records every payload a restore hands it.
type recordingHolder struct {
	name     string
	restored []string
}

func (h *recordingHolder) StateName() string { return h.name }

func (h *recordingHolder) SnapshotState(context.Context) ([]byte, error) {
	return []byte(emptyStateJSON), nil
}

func (h *recordingHolder) RestoreState(_ context.Context, payload []byte) error {
	h.restored = append(h.restored, string(payload))
	return nil
}

// TestPendingRestoreAccumulatesEveryCheckpointOfAName pins the per-session checkpoint restore.
// - two checkpoints state a slice of the same name before the holder mounts; the holder restores both in order, not only the last
// - revert pending to one payload per name and the first session's slice is lost (the kept payload never restored)
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestPendingRestoreAccumulatesEveryCheckpointOfAName(t *testing.T) {
	ctx := context.Background()
	registry := NewStateRegistry()
	for _, payload := range []string{`{"session":"kept"}`, `{"session":"other"}`} {
		if err := registry.RestoreState(ctx, []event.CheckpointSlice{{Name: "delivery", Payload: []byte(payload)}}); err != nil {
			t.Fatalf("restore a checkpoint slice: %v", err)
		}
	}
	if pending := registry.PendingRestore(); len(pending) != 1 || pending[0] != "delivery" {
		t.Fatalf("pending = %v, want the one delivery name awaiting its holder", pending)
	}
	holder := &recordingHolder{name: "delivery"}
	dispose, err := registry.Register(holder)
	if err != nil {
		t.Fatalf("register the holder: %v", err)
	}
	defer dispose()
	if len(holder.restored) != 2 || holder.restored[0] != `{"session":"kept"}` || holder.restored[1] != `{"session":"other"}` {
		t.Fatalf("the holder restored %v, want both checkpoint slices in order (kept then other)", holder.restored)
	}
	if pending := registry.PendingRestore(); len(pending) != 0 {
		t.Fatalf("pending = %v after the holder mounted, want every slice restored", pending)
	}
}

// foldHolder is a state holder that folds the facts it is handed and refuses the identities it names.
type foldHolder struct {
	name   string
	mu     sync.Mutex
	refuse map[event.EventID]error
	folded []event.EventID
}

func (h *foldHolder) StateName() string { return h.name }

func (h *foldHolder) SnapshotState(context.Context) ([]byte, error) {
	return []byte(emptyStateJSON), nil
}

func (h *foldHolder) RestoreState(context.Context, []byte) error { return nil }

// Reduce refuses one named identity and folds every other into the account.
func (h *foldHolder) Reduce(_ context.Context, evt event.Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if refused, held := h.refuse[evt.Key]; held {
		return refused
	}
	h.folded = append(h.folded, evt.Key)
	return nil
}

// allow stops refusing the named identity, so a later fold of it applies.
func (h *foldHolder) allow(key event.EventID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.refuse, key)
}

// folds reports the identities this holder applied.
func (h *foldHolder) folds() []event.EventID {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]event.EventID(nil), h.folded...)
}

// sessionHolder is a foldHolder that also states a slice per session chain.
type sessionHolder struct {
	*foldHolder
}

// SnapshotSessionState states one slice once the holder applied any fact, so a cut of a chain reads this face.
func (h *sessionHolder) SnapshotSessionState(context.Context, event.SessionID) ([]byte, error) {
	if len(h.folds()) == 0 {
		return nil, nil
	}
	return []byte(`{"stated":true}`), nil
}

// stateFact builds the committed fact one identity names, routed at one address.
func stateFact(key event.EventID, addr event.Address) event.Event {
	return event.Event{Key: key, Type: event.TypeExternalInput, Owner: addr, Target: addr, Source: addr}
}

// TestReduceRefusesTheCutUntilTheSameFactLands pins the one identity a cut may not cover.
// - a fact one holder refused refuses the whole cut, by name of the fact and of the slice
// - another fact landing beside it says nothing about the refused one, and the same identity landing clears it
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestReduceRefusesTheCutUntilTheSameFactLands(t *testing.T) {
	ctx := context.Background()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	registry := NewStateRegistry()
	holder := &foldHolder{name: "consumer", refuse: map[event.EventID]error{"fact-a": errors.New("the consumer refuses it")}}
	dispose, err := registry.Register(holder)
	if err != nil {
		t.Fatalf("register the holder: %v", err)
	}
	defer dispose()
	refused := stateFact("fact-a", addr)
	if err := registry.Reduce(ctx, refused); err == nil {
		t.Fatal("the fold answered no error, want the refusal of fact-a")
	}
	_, cutErr := registry.SnapshotState(ctx)
	if cutErr == nil {
		t.Fatal("the cut was filed, want it refused over the fact no holder applied")
	}
	if !strings.Contains(cutErr.Error(), "fact-a") || !strings.Contains(cutErr.Error(), "consumer") {
		t.Fatalf("the refused cut states %q, want it to name the fact fact-a and the slice consumer", cutErr)
	}
	if err := registry.Reduce(ctx, stateFact("fact-b", addr)); err != nil {
		t.Fatalf("fold the fact beside the refused one: %v", err)
	}
	if _, cutErr := registry.SnapshotState(ctx); cutErr == nil || !strings.Contains(cutErr.Error(), "fact-a") {
		t.Fatalf("the cut beside a landed fact states %v, want it still refused over fact-a", cutErr)
	}
	holder.allow("fact-a")
	if err := registry.Reduce(ctx, refused); err != nil {
		t.Fatalf("fold the refused fact again: %v", err)
	}
	if slices, err := registry.SnapshotState(ctx); err != nil {
		t.Fatalf("the cut over the settled fact states %v, want it filed", err)
	} else if len(slices) != 1 || slices[0].Name != "consumer" {
		t.Fatalf("the filed cut states %v, want the consumer slice", slices)
	}
}

// TestSnapshotRefusalNamesEveryUnappliedFactInIdentityOrder pins the determinism of a refusal with several facts.
// - two refused identities are named in identity order, however often the cut is asked
// - settling one identity leaves the other refusing, so one landing does not clear the account
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestSnapshotRefusalNamesEveryUnappliedFactInIdentityOrder(t *testing.T) {
	ctx := context.Background()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	registry := NewStateRegistry()
	holder := &foldHolder{name: "consumer", refuse: map[event.EventID]error{
		"fact-z": errors.New("the consumer refuses fact-z"),
		"fact-a": errors.New("the consumer refuses fact-a"),
	}}
	dispose, err := registry.Register(holder)
	if err != nil {
		t.Fatalf("register the holder: %v", err)
	}
	defer dispose()
	for _, key := range []event.EventID{"fact-z", "fact-a"} {
		if err := registry.Reduce(ctx, stateFact(key, addr)); err == nil {
			t.Fatalf("the fold of %s answered no error, want its refusal", key)
		}
	}
	var first string
	for i := 0; i < 8; i++ {
		_, cutErr := registry.SnapshotState(ctx)
		if cutErr == nil {
			t.Fatal("the cut was filed, want it refused over both facts")
		}
		if i == 0 {
			first = cutErr.Error()
			if strings.Index(first, "fact-a") > strings.Index(first, "fact-z") {
				t.Fatalf("the refusal states %q, want the facts in identity order", first)
			}
			continue
		}
		if cutErr.Error() != first {
			t.Fatalf("the refusal states %q on the read %d, want the deterministic %q", cutErr, i, first)
		}
	}
	holder.allow("fact-a")
	if err := registry.Reduce(ctx, stateFact("fact-a", addr)); err != nil {
		t.Fatalf("fold the settled fact: %v", err)
	}
	_, cutErr := registry.SnapshotState(ctx)
	if cutErr == nil || !strings.Contains(cutErr.Error(), "fact-z") || strings.Contains(cutErr.Error(), "fact-a") {
		t.Fatalf("the cut after one fact settled states %v, want it refused over fact-z alone", cutErr)
	}
}

// TestSessionCutRefusesOnAnUnappliedFactWhateverChainItNames pins that no session scopes a refusal.
// - a refused fact holds the cut of every session and of the whole account, whatever chain its addresses name
// - the same identity landing again files the session cut
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestSessionCutRefusesOnAnUnappliedFactWhateverChainItNames(t *testing.T) {
	ctx := context.Background()
	registry := NewStateRegistry()
	holder := &sessionHolder{foldHolder: &foldHolder{name: "session-consumer", refuse: map[event.EventID]error{
		"fact-a": errors.New("the consumer refuses it"),
	}}}
	dispose, err := registry.Register(holder)
	if err != nil {
		t.Fatalf("register the holder: %v", err)
	}
	defer dispose()
	refused := event.Event{
		Key:    "fact-a",
		Type:   event.TypeExternalInput,
		Owner:  event.Address{AgentID: "agent-a"},
		Target: event.Address{AgentID: "agent-a", SessionID: "session-target"},
		Source: event.Address{AgentID: "agent-a", SessionID: "session-source"},
	}
	if err := registry.Reduce(ctx, refused); err == nil {
		t.Fatal("the fold answered no error, want the refusal of fact-a")
	}
	if err := registry.Reduce(ctx, stateFact("fact-kept", event.Address{AgentID: "agent-a", SessionID: "session-unrelated"})); err != nil {
		t.Fatalf("fold the fact of the chain beside it: %v", err)
	}
	for _, session := range []event.SessionID{"session-target", "session-source", "session-unrelated"} {
		if _, err := registry.SnapshotSessionState(ctx, session); err == nil || !strings.Contains(err.Error(), "fact-a") {
			t.Fatalf("the cut of %s states %v, want it refused over fact-a: the framework guesses no chain a refusal belongs to", session, err)
		}
	}
	if _, err := registry.SnapshotState(ctx); err == nil {
		t.Fatal("the cut of the whole account was filed, want it refused over fact-a")
	}
	holder.allow("fact-a")
	if err := registry.Reduce(ctx, refused); err != nil {
		t.Fatalf("fold the settled fact: %v", err)
	}
	if slices, err := registry.SnapshotSessionState(ctx, "session-target"); err != nil {
		t.Fatalf("the cut of session-target after the fact landed states %v, want it filed", err)
	} else if len(slices) != 1 || slices[0].Name != "session-consumer" {
		t.Fatalf("the filed session cut states %v, want the session-consumer slice", slices)
	}
}

type interleavedHolder struct {
	*recordingHolder
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
	refused error
}

func (h *interleavedHolder) Reduce(context.Context, event.Event) error {
	h.mu.Lock()
	h.calls++
	call := h.calls
	h.mu.Unlock()
	if call == 1 {
		close(h.started)
		<-h.release
		return nil
	}
	if call == 2 {
		return h.refused
	}
	return nil
}

// TestConcurrentRetryDoesNotClearANewerFailure preserves a refusal made after the successful pass began.
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestConcurrentRetryDoesNotClearANewerFailure(t *testing.T) {
	ctx := context.Background()
	registry := NewStateRegistry()
	holder := &interleavedHolder{
		recordingHolder: &recordingHolder{name: "overlapping-consumer"},
		started:         make(chan struct{}),
		release:         make(chan struct{}),
		refused:         errors.New("later application refused"),
	}
	if _, err := registry.Register(holder); err != nil {
		t.Fatal(err)
	}
	fact := stateFact("same-fact", event.Address{AgentID: "agent-a", SessionID: "session-a"})
	completed := make(chan error, 1)
	go func() { completed <- registry.Reduce(ctx, fact) }()
	<-holder.started
	err := registry.Reduce(ctx, fact)
	close(holder.release)
	if !errors.Is(err, holder.refused) {
		t.Fatalf("overlapping application = %v, want %v", err, holder.refused)
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SnapshotState(ctx); !errors.Is(err, holder.refused) {
		t.Fatalf("snapshot after an older success = %v, want the later refusal preserved", err)
	}
	if err := registry.Reduce(ctx, fact); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SnapshotState(ctx); err != nil {
		t.Fatalf("snapshot after a subsequent successful retry = %v", err)
	}
}

// TestConcurrentFoldsKeepTheUnappliedFact reads and writes the record under the race detector.
// - folds and cuts of one identity run together without one waiting on the other's lock
// - the record ends refused while the holder refuses and settled once it applies
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestConcurrentFoldsKeepTheUnappliedFact(t *testing.T) {
	ctx := context.Background()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	registry := NewStateRegistry()
	holder := &foldHolder{name: "consumer", refuse: map[event.EventID]error{"fact-a": errors.New("the consumer refuses it")}}
	dispose, err := registry.Register(holder)
	if err != nil {
		t.Fatalf("register the holder: %v", err)
	}
	defer dispose()
	refused := stateFact("fact-a", addr)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = registry.Reduce(ctx, refused)
		}()
		go func() {
			defer wg.Done()
			_, _ = registry.SnapshotState(ctx)
		}()
	}
	wg.Wait()
	if _, err := registry.SnapshotState(ctx); err == nil {
		t.Fatal("the cut was filed, want it refused over fact-a")
	}
	holder.allow("fact-a")
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := registry.Reduce(ctx, refused); err != nil {
				t.Errorf("fold the settled fact: %v", err)
			}
		}()
	}
	wg.Wait()
	if _, err := registry.SnapshotState(ctx); err != nil {
		t.Fatalf("the cut over the settled fact states %v, want it filed", err)
	}
}

// unfinishedSlice is a state slice that states the obligations it still owes
// beside the ones a restore hands it, so the same holder answers what an unmount
// would lose.
type unfinishedSlice struct {
	name string
	mu   sync.Mutex
	owed []string
}

// StateName reports the name the slice is keyed by.
func (s *unfinishedSlice) StateName() string { return s.name }

// SnapshotState states the open obligations.
func (s *unfinishedSlice) SnapshotState(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Marshal(s.owed)
}

// RestoreState puts the stated obligations back.
func (s *unfinishedSlice) RestoreState(_ context.Context, payload []byte) error {
	var stated []string
	if err := json.Unmarshal(payload, &stated); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owed = append(s.owed, stated...)
	return nil
}

// UnfinishedWork reports the obligations an unmount would lose.
func (s *unfinishedSlice) UnfinishedWork() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.owed)
}

// open records the obligations the slice still owes.
func (s *unfinishedSlice) open(items ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owed = append(s.owed, items...)
}

// settle clears the obligations the slice answered.
func (s *unfinishedSlice) settle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owed = nil
}

// TestUnregisterRefusesASliceWithUnfinishedWork is the negative anchor of the unmount edge: a slice that still owes work may not leave.
// - the disposer refuses by name with the count of unfinished obligations, and the registry keeps that very holder
// - the cut filed after the refusal still states the obligations the holder holds, including one it recorded after the refusal
// - a disposer that deletes the holder, or that reports only after deleting it, fails the count, the identity or the payload
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestUnregisterRefusesASliceWithUnfinishedWork(t *testing.T) {
	ctx := context.Background()
	registry := NewStateRegistry()
	holder := &unfinishedSlice{name: "owed-consumer", owed: []string{"notice-a", "notice-b"}}
	dispose, err := registry.Register(holder)
	if err != nil {
		t.Fatalf("register the holder: %v", err)
	}
	refusal := dispose()
	if refusal == nil {
		t.Fatal("the unmount of a slice owing 2 obligation(s) answered nil, want it refused by name")
	}
	if !strings.Contains(refusal.Error(), `"owed-consumer"`) || !strings.Contains(refusal.Error(), "2 unfinished obligation(s)") {
		t.Fatalf("the refusal states %q, want it to name the slice and the 2 unfinished obligation(s)", refusal)
	}
	mounted := registry.mounted()
	if len(mounted) != 1 || mounted[0] != holder {
		t.Fatalf("the registry holds %v after the refused unmount, want the very holder that owes the work", mounted)
	}
	if names := registry.Names(); len(names) != 1 || names[0] != "owed-consumer" {
		t.Fatalf("the registry names %v after the refused unmount, want the slice kept", names)
	}
	holder.open("notice-c")
	slices, cutErr := registry.SnapshotState(ctx)
	if cutErr != nil {
		t.Fatalf("file the cut beside the refused unmount: %v", cutErr)
	}
	if len(slices) != 1 || !strings.Contains(string(slices[0].Payload), "notice-c") {
		t.Fatalf("the cut states %v, want the obligations of the holder the registry kept", slices)
	}
	holder.settle()
	if err := dispose(); err != nil {
		t.Fatalf("the unmount once the holder settled its account answered %v, want it to leave", err)
	}
	if names := registry.Names(); len(names) != 0 {
		t.Fatalf("the registry names %v after the settled unmount, want the slice gone", names)
	}
}

// TestUnregisterAllowsASettledSlice pins the other half of the refusal: a slice that owes nothing leaves as it registered.
// - a holder that declares no unfinished work unmounts without an error and leaves no slice behind
// - the cut of the registry after the unmount states no slice of that name
// - a registry that refuses every unmount answers an error here
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestUnregisterAllowsASettledSlice(t *testing.T) {
	ctx := context.Background()
	registry := NewStateRegistry()
	holder := &unfinishedSlice{name: "settled-consumer"}
	dispose, err := registry.Register(holder)
	if err != nil {
		t.Fatalf("register the holder: %v", err)
	}
	if names := registry.Names(); len(names) != 1 {
		t.Fatalf("the registry names %v after the registration, want the one slice", names)
	}
	if err := dispose(); err != nil {
		t.Fatalf("the unmount of a slice owing nothing answered %v, want it to leave", err)
	}
	if mounted := registry.mounted(); len(mounted) != 0 {
		t.Fatalf("the registry holds %v after the unmount, want no holder", mounted)
	}
	slices, cutErr := registry.SnapshotState(ctx)
	if cutErr != nil {
		t.Fatalf("file the cut after the unmount: %v", cutErr)
	}
	if len(slices) != 0 {
		t.Fatalf("the cut states %v after the settled slice left, want no slice", slices)
	}
}

// TestSliceHandoverKeepsTheUnfinishedObligations pins who holds the obligations after a refused unmount.
// - the refused unmount keeps the name with the holder that owes, and a second holder of that name is refused beside it
// - the name passes to a successor only once the holder settled, so no successor silently receives an account it was never handed
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestSliceHandoverKeepsTheUnfinishedObligations(t *testing.T) {
	ctx := context.Background()
	registry := NewStateRegistry()
	holder := &unfinishedSlice{name: "owed-consumer", owed: []string{"notice-a"}}
	dispose, err := registry.Register(holder)
	if err != nil {
		t.Fatalf("register the holder: %v", err)
	}
	if refusal := dispose(); refusal == nil || !strings.Contains(refusal.Error(), "1 unfinished obligation(s)") {
		t.Fatalf("the unmount of a slice owing 1 obligation = %v, want it refused by name with the count", refusal)
	}
	successor := &unfinishedSlice{name: "owed-consumer"}
	if _, err := registry.Register(successor); err == nil || !strings.Contains(err.Error(), "already mounted") {
		t.Fatalf("mounting a second holder of the kept name answered %v, want the name kept by the holder that owes", err)
	}
	slices, cutErr := registry.SnapshotState(ctx)
	if cutErr != nil {
		t.Fatalf("file the cut beside the refused unmount: %v", cutErr)
	}
	if len(slices) != 1 || !strings.Contains(string(slices[0].Payload), "notice-a") {
		t.Fatalf("the cut states %v, want the obligation still stated by the holder that owes it", slices)
	}
	holder.settle()
	if err := dispose(); err != nil {
		t.Fatalf("the unmount once the holder settled answered %v, want it to leave", err)
	}
	disposeSuccessor, err := registry.Register(successor)
	if err != nil {
		t.Fatalf("mount the successor of the settled name: %v", err)
	}
	if refusal := disposeSuccessor(); refusal != nil {
		t.Fatalf("the successor owes nothing, so its unmount answered %v", refusal)
	}
}
