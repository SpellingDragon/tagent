package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// TestCheckpointEmittedAtCadenceAndFold pins the bounded cadence and the cut it files.
//   - a run that commits past its interval files a checkpoint fact of the frozen type
//   - the offset the fact states is the partition sequence the run had committed
//   - the fact carries every state slice mounted on the seam, so the projection survivors are restored, not re-folded
//   - the cut is taken before the checkpoint fact lands, so its own partition stands past the watermark it states
//   - a cut asked for beside a chain at rest files a carrier of its own, the watermark read before that carrier lands
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCheckpointEmittedAtCadenceAndFold(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	run.checkpointEvery = 2
	addr := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-cadence"}
	commitWindowTurns(t, ctx, run, addr, 1)

	id, filedOffset := awaitCadenceCheckpoint(t, run, "")
	if id == "" {
		t.Fatal("the commit cadence filed no checkpoint fact")
	}
	stored, err := run.CommittedEvent(ctx, id)
	if err != nil {
		t.Fatalf("read the checkpoint fact back: %v", err)
	}
	if stored.Type != event.TypeCheckpoint {
		t.Fatalf("the filed fact is %q, want the frozen checkpoint type", stored.Type)
	}
	checkpoint, stated := event.CheckpointOf(stored)
	if !stated {
		t.Fatalf("the fact %s states no readable checkpoint", id)
	}
	partition := memory.PartitionIDFromName(string(addr.AgentID))
	if !reflect.DeepEqual(map[int]int64(checkpoint.Offset), map[int]int64(filedOffset)) {
		t.Fatalf("the fact states the cut %v, want the cut the coordinator recorded %v", checkpoint.Offset, filedOffset)
	}
	ownPartition, ownSeq := seqOfFact(t, run, id)
	if got := checkpoint.Offset[ownPartition]; got >= ownSeq {
		t.Fatalf("the checkpoint fact landed at seq %d of partition %d beside a cut of %d, want a cut behind the carrier it states",
			ownSeq, ownPartition, got)
	}
	if _, covered := checkpoint.Offset[partition]; !covered {
		t.Fatalf("the checkpoint covers the partitions %v, want the chain the run committed", checkpoint.Offset)
	}
	names := checkpoint.CheckpointSliceNames()
	if len(names) == 0 || !slices.Contains(names, checkpointSliceProjection) {
		t.Fatalf("the checkpoint carries the slices %v, want the mounted projection slice", names)
	}
	if stored.Meta[event.MetaKeyCheckpointOffset] == "" || stored.Meta[event.MetaKeyCheckpointSlices] == "" {
		t.Fatalf("the checkpoint fact carries the metadata %v, want the cut and the slice names", stored.Meta)
	}
	if stated := stored.Meta[event.MetaKeyCheckpointOffset]; stated != checkpoint.Offset.String() {
		t.Fatalf("the stated metadata cut reads %q, want the rendered cut %q the fact states", stated, checkpoint.Offset.String())
	}
	projection, carried := checkpointSliceOf(t, checkpoint, checkpointSliceProjection)
	if !carried {
		t.Fatal("the checkpoint carries no projection slice")
	}
	if len(projection.Sessions) != 1 || projection.Sessions[0].Session != string(addr.SessionID) {
		t.Fatalf("the projection slice states %+v, want the one chain the run presents", projection.Sessions)
	}
	if len(projection.Sessions[0].Entries) == 0 {
		t.Fatal("the projection slice carries no survivor beside a chain that presented material")
	}
	if projection.Sessions[0].Entries[0].Key != event.EventID(timeStampKey(checkpointBase(), addr, 0, 0)) {
		t.Fatalf("the projection slice starts at %s, want the first mapped leg of the chain", projection.Sessions[0].Entries[0].Key)
	}
	if _, filed, err := run.Checkpoint(ctx); err != nil || !filed {
		t.Fatalf("file the cut of the rested chain = (%t, %v), want a filed fact", filed, err)
	}
	restedID, restedOffset := run.CheckpointFiled()
	restedPartition, restedSeq := seqOfFact(t, run, restedID)
	if got := restedOffset[restedPartition]; got != restedSeq-1 {
		t.Fatalf("the rested cut landed at seq %d of partition %d beside a cut of %d, want the cut one slot behind it",
			restedSeq, restedPartition, got)
	}
}

// awaitCadenceCheckpoint waits for the cadence to file a checkpoint past the key
// it is given and answers that key beside the cut the coordinator recorded. The
// filing stands off the committing goroutine, so a test reads the carrier the
// cadence owes instead of one a commit returned beside.
func awaitCadenceCheckpoint(t *testing.T, run *Run, past event.EventID) (event.EventID, event.CheckpointOffset) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		id, offset := run.CheckpointFiled()
		if id != "" && id != past {
			return id, offset
		}
		if time.Now().After(deadline) {
			t.Fatalf("the cadence filed no checkpoint past %q after 20s", past)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestCheckpointOffsetNotFactSeq pins the offset semantics against the identity counter.
//   - the cut is the partition sequence, so a second partition's commits never move the first partition's offset although they move the identity counter
//   - a restart over the same store continues the partition sequence, so its checkpoint covers more than the first although identities restart at zero
//   - a second cadence files a checkpoint of its own whose cut stands behind its own fact, the watermark following the landing slots
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCheckpointOffsetNotFactSeq(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	}
	first := windowChainRun(t, ctx, storage())
	first.checkpointEvery = 1
	mine := event.Address{AgentID: "agent-a", SessionID: "session-offset-mine"}
	other := event.Address{AgentID: "agent-b", SessionID: "session-offset-other"}
	commitWindowTurns(t, ctx, first, mine, 1)
	firstID, _ := awaitCadenceCheckpoint(t, first, "")
	if firstID == "" {
		t.Fatal("the first cadence filed no checkpoint fact")
	}
	firstCut, err := first.CommittedEvent(ctx, firstID)
	if err != nil {
		t.Fatalf("read the first checkpoint back: %v", err)
	}
	firstCheckpoint, stated := event.CheckpointOf(firstCut)
	if !stated {
		t.Fatalf("the fact %s states no readable checkpoint", firstID)
	}
	partition := memory.PartitionIDFromName(string(mine.AgentID))
	ownPartitionSeq := firstCheckpoint.Offset[partition]
	cutPartition, cutSeq := seqOfFact(t, first, firstID)
	if cutPartition != partition || ownPartitionSeq >= cutSeq {
		t.Fatalf("the checkpoint landed at seq %d of partition %d beside a cut of %d on partition %d, want a cut behind its own fact",
			cutSeq, cutPartition, ownPartitionSeq, partition)
	}
	commitWindowTurns(t, ctx, first, other, 2)
	secondID, secondOffset := awaitCadenceCheckpoint(t, first, firstID)
	if secondID == "" || secondID == firstID {
		t.Fatalf("the second cadence filed %q, want a checkpoint of its own", secondID)
	}
	_, secondSeq := seqOfFact(t, first, secondID)
	if got := secondOffset[cutPartition]; got >= secondSeq {
		t.Fatalf("the second checkpoint landed at seq %d beside a cut of %d, want a cut behind it", secondSeq, got)
	}
	if secondOffset[partition] < ownPartitionSeq {
		t.Fatalf("the second cut went back to seq %d from %d, want the landing sequence to advance", secondOffset[partition], ownPartitionSeq)
	}

	restarted := windowChainRun(t, ctx, storage())
	restarted.checkpointEvery = 1
	restarted.mu.Lock()
	startSeq := restarted.factSeq
	restarted.mu.Unlock()
	commitWindowTurns(t, ctx, restarted, other, 1)
	thirdID, thirdOffset := awaitCadenceCheckpoint(t, restarted, "")
	if thirdID == "" {
		t.Fatal("the restarted run filed no checkpoint fact")
	}
	if thirdOffset[partition] <= ownPartitionSeq {
		t.Fatalf("the restarted cut covers partition %d up to seq %d, want it continued past %d",
			partition, thirdOffset[partition], ownPartitionSeq)
	}
	if startSeq != 0 {
		t.Fatalf("the restarted run opened at identity %d, want the identity counter to start over", startSeq)
	}
	if uint64(thirdOffset[partition]) <= restarted.factSeq {
		t.Fatalf("the restarted cut of seq %d sits inside the %d identities the fresh process minted, so it is the counter and not the partition sequence",
			thirdOffset[partition], restarted.factSeq)
	}
}

// seqOfFact reports the partition and sequence slot one committed fact landed at,
// read from the store index the landing path numbers.
func seqOfFact(t *testing.T, run *Run, key event.EventID) (int, int64) {
	t.Helper()
	partition, seq := -1, int64(0)
	if err := run.tier.Segments.EachIndexNewestFirst(context.Background(), func(held event.EventID, part int, at int64, _ string, _ event.SessionID) error {
		if held == key {
			partition, seq = part, at
		}
		return nil
	}); err != nil {
		t.Fatalf("read the store index for %s: %v", key, err)
	}
	if partition < 0 {
		t.Fatalf("the fact %s is not in the store index", key)
	}
	return partition, seq
}

// checkpointBase is the moment the seeded turns of a window chain start at.
func checkpointBase() time.Time {
	return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
}

// TestBootRefusesAnUnreadableCheckpoint pins the negative anchor of the anchored boot.
//   - an unreadable payload, one of another layout version and one stating a foreign identity each refuse the boot
//   - the refusal is the named one and names the checkpoint fact it refused
//   - the unreadable carrier is the newest fact of the store: the close files a carrier of its own before it, so the fixture appends the corrupted one last
//   - the boot answers no run beside the refusal (negative: a reader that skipped the unreadable carrier would start onto an empty state)
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestBootRefusesAnUnreadableCheckpoint(t *testing.T) {
	addr := event.Address{AgentID: "agent-a"}
	offset := event.CheckpointOffset{memory.PartitionIDFromName(string(addr.AgentID)): 1}
	stated, err := event.CheckpointFor(offset, []event.CheckpointSlice{
		{Name: checkpointSliceProjection, Payload: json.RawMessage(`{"sessions":[]}`)},
	})
	if err != nil {
		t.Fatalf("state the checkpoint of the fixture: %v", err)
	}
	readable, err := event.CheckpointPayloadOf(stated)
	if err != nil {
		t.Fatalf("render the checkpoint payload of the fixture: %v", err)
	}
	rewrite := func(mutate func(*event.CheckpointPayload)) []byte {
		var wire event.CheckpointPayload
		if err := json.Unmarshal(readable, &wire); err != nil {
			t.Fatalf("read the checkpoint payload back: %v", err)
		}
		mutate(&wire)
		payload, err := json.Marshal(wire)
		if err != nil {
			t.Fatalf("render the corrupted checkpoint payload: %v", err)
		}
		return payload
	}
	for name, content := range map[string][]byte{
		"a_payload_the_reader_cannot_read": []byte("{not-a-checkpoint"),
		"a_payload_of_another_layout_version": rewrite(func(wire *event.CheckpointPayload) {
			wire.Version = event.CheckpointVersion + 1
		}),
		"a_payload_that_states_a_foreign_identity": rewrite(func(wire *event.CheckpointPayload) {
			wire.ID = event.EventID("ckp-0000000000000000")
		}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			storage := func() Option {
				return WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
			}
			chain := event.Address{AgentID: "agent-a", SessionID: event.SessionID("session-" + name)}
			source := windowChainRun(t, ctx, storage())
			commitWindowTurns(t, ctx, source, chain, 1)
			if _, filed, err := source.Checkpoint(ctx); err != nil || !filed {
				t.Fatalf("file the checkpoint of the chain = (%t, %v), want a filed fact", filed, err)
			}
			if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
				t.Fatalf("close the source process: %v", err)
			}
			corrupt := windowChainRun(t, ctx, storage())
			corrupt.checkpointEvery = 1 << 30
			corrupt.checkpointPeriod = 1000 * time.Hour
			bad := event.EventID("checkpoint:9999")
			if _, err := corrupt.commitFactChecked(ctx, event.Event{
				Key:       bad,
				Type:      event.TypeCheckpoint,
				Timestamp: time.Now().UTC(),
				Owner:     event.Address{AgentID: chain.AgentID},
				Meta: map[string]string{
					event.MetaKeyCheckpointOffset: offset.String(),
					event.MetaKeyCheckpointSlices: checkpointSliceProjection,
					event.MetaKeyEventSummary:     "framework checkpoint",
				},
				Message: model.Message{Content: string(content)},
			}); err != nil {
				t.Fatalf("file the corrupted checkpoint %s: %v", bad, err)
			}
			booted, err := bootWindowChain(ctx, storage())
			if err == nil {
				if booted != nil {
					booted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)})
				}
				t.Fatalf("the boot started beside the unreadable checkpoint %s, want it refused by name", bad)
			}
			if !errors.Is(err, ErrCheckpointUnreadable) {
				t.Fatalf("the boot over the unreadable checkpoint %s answered %v, want %v", bad, err, ErrCheckpointUnreadable)
			}
			if !strings.Contains(err.Error(), string(bad)) {
				t.Fatalf("the refusal %q names no checkpoint fact, want the %s it refused", err, bad)
			}
		})
	}
}

// bootWindowChain assembles one run over a store the way windowChainRun does and
// answers the error a refusing boot reports, so a check reads the refusal instead
// of failing on it.
func bootWindowChain(ctx context.Context, opts ...Option) (*Run, error) {
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	options := append([]Option{
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}),
	}, opts...)
	return Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: 60}, {ID: "agent-b", Model: "model-a", MaxTokens: 60}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}, "agent-b": {Model: provider}}, options...)
}

// checkpointSliceOf reads one named slice of a checkpoint back as the projection
// payload the framework states.
func checkpointSliceOf(t *testing.T, checkpoint event.Checkpoint, name string) (projectionSliceWire, bool) {
	t.Helper()
	for _, slice := range checkpoint.Slices {
		if slice.Name != name {
			continue
		}
		var wire projectionSliceWire
		if err := json.Unmarshal([]byte(slice.Payload), &wire); err != nil {
			t.Fatalf("the %q slice states no readable payload: %v", name, err)
		}
		return wire, true
	}
	return projectionSliceWire{}, false
}

// TestCheckpointWaitsForTheQuietPointOfThePipeline pins the point a cut is taken at.
//   - a cut requested beside work in flight answers nothing until that work leaves, so the state it reads is never one caught mid landing
//   - the cut answers the moment the pipeline is quiet, filing the carrier the run owes
func TestCheckpointWaitsForTheQuietPointOfThePipeline(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-quiet"}
	commitWindowTurns(t, ctx, run, addr, 1)
	run.flight.enter()
	answered := make(chan struct{})
	var filed bool
	var err error
	go func() {
		defer close(answered)
		_, filed, err = run.Checkpoint(ctx)
	}()
	select {
	case <-answered:
		t.Fatal("the cut answered beside a unit of work in flight: the state it stated was read before the work it drains left the pipeline")
	case <-time.After(250 * time.Millisecond):
	}
	run.flight.leave()
	select {
	case <-answered:
	case <-time.After(20 * time.Second):
		t.Fatal("the cut never answered beside a quiet pipeline")
	}
	if err != nil {
		t.Fatalf("file the cut of the quiet pipeline: %v", err)
	}
	if !filed {
		t.Fatal("the cut of the quiet pipeline filed no checkpoint")
	}
}

// holderLockSlice is a consumer state slice whose snapshot takes the lock its own
// commits carry, so a cut asked from inside such a commit stands on the very lock
// that commit holds. The task registry is the shipped holder of that shape: the
// delivery flush commits its settlement under the registry lock.
type holderLockSlice struct {
	mu sync.Mutex
}

// StateName reports the name the slice is keyed by.
func (s *holderLockSlice) StateName() string { return "assembly.holder-lock" }

// SnapshotState states the slice under the lock a commit of it carries.
func (s *holderLockSlice) SnapshotState(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []byte("{}"), nil
}

// RestoreState puts the stated slice back.
func (s *holderLockSlice) RestoreState(context.Context, []byte) error { return nil }

// TestCheckpointCadenceNeverTakesAHoldersLock pins where the due is filed.
//   - a commit that reaches the cadence under a holder's own lock answers instead of filing the cut inside that lock
//   - the carrier the due owes lands after the holder released the lock, so the due is filed and not dropped
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCheckpointCadenceNeverTakesAHoldersLock(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	holder := &holderLockSlice{}
	if _, err := run.RegisterStateSlice(holder); err != nil {
		t.Fatalf("register the holder's state slice: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-holder-lock"}
	run.checkpointEvery = 1
	commitWindowTurns(t, ctx, run, addr, 1)
	filed, _ := awaitCadenceCheckpoint(t, run, "")

	holder.mu.Lock()
	committed := make(chan error, 1)
	go func() {
		_, err := run.CommitToSession(ctx, addr, event.Event{
			Key:       "the-fact-committed-under-the-holder-lock",
			Type:      event.TypeExternalInput,
			Timestamp: time.Now().UTC(),
			Meta:      map[string]string{event.MetaKeyEventSummary: "held"},
			Message:   model.Message{Role: model.RoleUser, Content: "the material committed under the holder lock"},
			Owner:     addr,
		})
		committed <- err
	}()
	select {
	case err := <-committed:
		if err != nil {
			holder.mu.Unlock()
			t.Fatalf("commit the fact under the holder lock: %v", err)
		}
	case <-time.After(20 * time.Second):
		holder.mu.Unlock()
		t.Fatal("the commit under the holder lock never answered: the cadence filed its cut inside the commit and waited on the lock that commit holds")
	}
	holder.mu.Unlock()
	if id, _ := awaitCadenceCheckpoint(t, run, filed); id == "" {
		t.Fatal("the cadence filed no carrier for the due the commit reached under the holder lock")
	}
}

// TestRestoredOwedRoundIsTheFactTheChainKeeps pins the owed-input slice.
//   - the carrier names the identity of each round nobody drove
//   - the boot reads that fact back, so the rest criterion judges the driving fact it is instead of a placeholder no criterion recognises
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRestoredOwedRoundIsTheFactTheChainKeeps(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-restored-input"}
	evt := event.Event{
		Key:       "the-undriven-round",
		Type:      event.TypeExternalInput,
		Timestamp: checkpointBase(),
		Meta:      map[string]string{event.MetaKeyEventSummary: "ask"},
		Message:   model.Message{Role: model.RoleUser, Content: "the round nobody drove"},
		Owner:     addr,
	}
	if _, err := run.commitOnly(ctx, addr, evt); err != nil {
		t.Fatalf("commit the undriven round: %v", err)
	}
	payload, err := json.Marshal(inputsSliceWire{Sessions: []inputsSliceSession{{
		Agent:   string(addr.AgentID),
		Session: string(addr.SessionID),
		Inputs:  []event.EventID{evt.Key},
	}}})
	if err != nil {
		t.Fatalf("encode the owed-input slice: %v", err)
	}
	if err := run.restoreInputsSlice(payload); err != nil {
		t.Fatalf("restore the owed-input slice: %v", err)
	}
	restored := run.restoredInputFacts(ctx, addr)
	if len(restored) != 1 {
		t.Fatalf("the restore answered %d owed round(s), want the one the carrier named", len(restored))
	}
	if restored[0].Key != evt.Key || restored[0].Type != event.TypeExternalInput {
		t.Fatalf("the restored round reads as %s/%q, want the driving fact %s of the input type", restored[0].Key, restored[0].Type, evt.Key)
	}
	pending := agent.PendingInputs(restored)
	if len(pending) != 1 || pending[0].Key != evt.Key {
		t.Fatalf("the rest criterion reads %d owed round(s) from the restore, want the one round nobody drove", len(pending))
	}
}

// restoreCounter is the state slice a boot counts its own restores with: the
// carrier states it, so a boot over that carrier reports how many times the cut
// reached its holder.
type restoreCounter struct {
	restores *int
}

// StateName reports the name the counted slice is keyed by.
func (s restoreCounter) StateName() string { return "assembly.restore-counter" }

// SnapshotState states the slice the carrier holds.
func (s restoreCounter) SnapshotState(context.Context) ([]byte, error) { return []byte("{}"), nil }

// RestoreState counts one application of the carrier.
func (s restoreCounter) RestoreState(context.Context, []byte) error {
	*s.restores++
	return nil
}

// countingBoot mounts the counted slice the moment the run opens, so the restore
// the same boot performs reaches it.
func countingBoot(restores *int) Option {
	return withBootConfigReconcile(func(ctx context.Context, run *Run, decl *config.Declaration, agents map[event.AgentID]NativeAgent) error {
		if _, err := run.RegisterStateSlice(restoreCounter{restores: restores}); err != nil {
			return err
		}
		_, err := run.Publish(ctx, decl, agents)
		return err
	})
}

// TestBootRestoresTheCarrierOnce pins the single restore of one boot.
//   - a boot over a store holding a checkpoint hands the cut to each holder once
//   - a boot over a store holding no carrier states the empty cut and hands that to each holder once as well
//   - the run that files the carrier restores the empty cut it booted beside, and the run that reads it restores the very slice the carrier states
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestBootRestoresTheCarrierOnce(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	sourceRestores := 0
	source := windowChainRun(t, ctx, storage, countingBoot(&sourceRestores))
	addr := event.Address{AgentID: "agent-a", SessionID: "session-restore-once"}
	commitWindowTurns(t, ctx, source, addr, 1)
	if _, filed, err := source.Checkpoint(ctx); err != nil || !filed {
		t.Fatalf("file the carrier of the chain = (%t, %v), want a filed fact", filed, err)
	}
	if sourceRestores != 1 {
		t.Fatalf("the boot of a store holding no carrier applied the empty cut %d time(s), want the one restore every boot hands its holders: a boot that skips them leaves the work they state unseated", sourceRestores)
	}

	restores := 0
	restarted := windowChainRun(t, ctx, storage, countingBoot(&restores))
	if filed, _ := restarted.CheckpointFiled(); filed == "" {
		t.Fatal("the restart states no checkpoint, so the count proves nothing")
	}
	if restores != 1 {
		t.Fatalf("the restart applied the cut %d time(s), want once", restores)
	}
}

// checkpointLog records the lines the assembly face writes at ERROR, so a check
// reads the failure a carrier path reports instead of assuming it reported one.
type checkpointLog struct {
	mu     sync.Mutex
	errors []string
}

// Error records one error line.
func (l *checkpointLog) Error(args ...any) { l.Errorf("%s", fmt.Sprint(args...)) }

// Errorf records one formatted error line.
func (l *checkpointLog) Errorf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors = append(l.errors, fmt.Sprintf(format, args...))
}

// Debug drops one debug line: the carrier failures are the only ones a check reads.
func (l *checkpointLog) Debug(...any) {}

// Debugf drops one formatted debug line.
func (l *checkpointLog) Debugf(string, ...any) {}

// Info drops one info line.
func (l *checkpointLog) Info(...any) {}

// Infof drops one formatted info line.
func (l *checkpointLog) Infof(string, ...any) {}

// Warn drops one warn line.
func (l *checkpointLog) Warn(...any) {}

// Warnf drops one formatted warn line.
func (l *checkpointLog) Warnf(string, ...any) {}

// Fatal drops one fatal line: a check never ends the binary it runs in.
func (l *checkpointLog) Fatal(...any) {}

// Fatalf drops one formatted fatal line.
func (l *checkpointLog) Fatalf(string, ...any) {}

// mentions reports whether one recorded error line carries every fragment.
func (l *checkpointLog) mentions(fragments ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.errors {
		holds := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				holds = false
				break
			}
		}
		if holds {
			return true
		}
	}
	return false
}

// awaitCheckpointLog waits for one error line of the assembly face carrying every
// fragment, so a check reads the filing failure the filer reported.
func awaitCheckpointLog(t *testing.T, log *checkpointLog, fragments ...string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if log.mentions(fragments...) {
			return
		}
		if time.Now().After(deadline) {
			log.mu.Lock()
			lines := append([]string(nil), log.errors...)
			log.mu.Unlock()
			t.Fatalf("the assembly face wrote no error line carrying %v after 20s, it wrote %d line(s): %v", fragments, len(lines), lines)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// commitWindowTurn commits one closed turn of four legs under keys no other turn
// of the same chain names, so a check drives the fold turn by turn.
func commitWindowTurn(t *testing.T, ctx context.Context, run *Run, addr event.Address, turn int) {
	t.Helper()
	base := checkpointBase()
	for position, leg := range []struct {
		typ     string
		summary string
	}{
		{event.TypeExternalInput, "ask"},
		{event.TypeThinkingPlan, "plan"},
		{event.TypeActionCommand, "call"},
		{event.TypeAgentOutput, "answer"},
	} {
		evt := event.Event{
			Key:       event.EventID(fmt.Sprintf("%s-turn-%d-leg-%d", addr.SessionID, turn, position)),
			Type:      leg.typ,
			Timestamp: base.Add(time.Duration(turn*10+position) * time.Minute),
			Meta:      map[string]string{event.MetaKeyEventSummary: leg.summary},
			Message:   model.Message{Role: model.RoleUser, Content: "the material of the landing walk"},
			Owner:     addr,
		}
		commitWindowFact(t, ctx, run, addr, evt)
	}
}

// newestFoldProductKey reports the identity of the newest compression product one
// chain filed, so a check reads the cut a carrier states against the fact that
// asked for it.
func newestFoldProductKey(t *testing.T, ctx context.Context, run *Run) event.EventID {
	t.Helper()
	key := event.EventID("")
	if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(held event.EventID, _ int, _ int64, typ string, _ event.SessionID) error {
		if key == "" && typ == event.CompressProductType {
			key = held
		}
		return nil
	}); err != nil {
		t.Fatalf("walk the store index: %v", err)
	}
	return key
}

// TestCheckpointFollowsTheFoldThatDropsMaterial pins the fold trigger of the cadence.
//   - a chain whose interval and period never fall due files no carrier while it only appends
//   - the commit that lands a fold product asks for a carrier, and the cut it states covers that product
//   - a fold can mint several products in one pass, and the carrier answering the last of them states a cut the newest product landed behind
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCheckpointFollowsTheFoldThatDropsMaterial(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	run.checkpointEvery = 1 << 30
	run.checkpointPeriod = 1000 * time.Hour
	addr := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-fold"}
	turn := 0
	for {
		if countFoldProducts(t, ctx, run) > 0 {
			break
		}
		if turn >= 12 {
			t.Fatal("the fixture folded nothing, so the fold trigger proves nothing")
		}
		if filed, _ := run.CheckpointFiled(); filed != "" {
			t.Fatalf("the chain filed the carrier %s beside no fold product, want the cadence clock to owe none before the fold", filed)
		}
		turn++
		commitWindowTurn(t, ctx, run, addr, turn)
	}
	product := newestFoldProductKey(t, ctx, run)
	if product == "" {
		t.Fatal("the fixture folded no product, so the trigger proves nothing")
	}
	partition, productSeq := seqOfFact(t, run, product)
	deadline := time.Now().Add(20 * time.Second)
	for {
		if id, offset := run.CheckpointFiled(); id != "" && offset[partition] >= productSeq {
			break
		}
		if time.Now().After(deadline) {
			_, offset := run.CheckpointFiled()
			t.Fatalf("the fold filed no carrier past the product %s at seq %d of partition %d after 20s, the newest cut states %d",
				product, productSeq, partition, offset[partition])
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestCloseFilesTheCarrierTheNextBootReads pins the close trigger of the cadence.
//   - a run whose interval and period never fell due files no carrier while it commits
//   - the close files one behind the facts it settled
//   - the carrier the close filed is the one the next boot restores
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCloseFilesTheCarrierTheNextBootReads(t *testing.T) {
	ctx := context.Background()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: t.TempDir()})
	run := windowChainRun(t, ctx, storage)
	run.checkpointEvery = 1 << 30
	run.checkpointPeriod = 1000 * time.Hour
	addr := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-close"}
	commitWindowTurns(t, ctx, run, addr, 1)
	if filed, _ := run.CheckpointFiled(); filed != "" {
		t.Fatalf("the chain filed the carrier %s beside no due, want the cadence to owe none before the close", filed)
	}
	if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(30 * time.Second)}); err != nil {
		t.Fatalf("close the run: %v", err)
	}
	id, offset := run.CheckpointFiled()
	if id == "" {
		t.Fatal("the close filed no carrier, want the account the run reached at its fixed point")
	}
	partition := memory.PartitionIDFromName(string(addr.AgentID))
	if offset[partition] <= 0 {
		t.Fatalf("the carrier the close filed states the cut %d of partition %d, want the facts the run settled", offset[partition], partition)
	}
	booted := windowChainRun(t, ctx, storage)
	if restored, _ := booted.CheckpointFiled(); restored != id {
		t.Fatalf("the boot restored the carrier %q, want the %q the close filed", restored, id)
	}
}

// refusingSlice is a state slice its holder refuses to state, so a cut that asks
// it for an account cannot state the one the run holds.
type refusingSlice struct{ err error }

// StateName reports the name the refusing slice is keyed by.
func (s refusingSlice) StateName() string { return "assembly.refusing" }

// SnapshotState refuses to state the account.
func (s refusingSlice) SnapshotState(context.Context) ([]byte, error) { return nil, s.err }

// RestoreState puts nothing back: the slice never states an account.
func (s refusingSlice) RestoreState(context.Context, []byte) error { return nil }

// TestAFailedCheckpointSurfaces pins the failure path of the carrier.
//   - a holder that refuses to state itself fails every cut it is part of, and the filer reports that at ERROR
//   - the close answers the failure to its caller instead of reporting a close beside a carrier no process can read
//   - a run whose holders state themselves closes beside no such failure (negative: the failure is the refused account, not the close)
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestAFailedCheckpointSurfaces(t *testing.T) {
	ctx := context.Background()
	observer := &checkpointLog{}
	original := trpclog.Default
	trpclog.Default = observer
	t.Cleanup(func() { trpclog.Default = original })

	run := windowChainRun(t, ctx)
	run.checkpointEvery = 1
	refused := errors.New("the holder refuses to state its account")
	if _, err := run.RegisterStateSlice(refusingSlice{err: refused}); err != nil {
		t.Fatalf("register the refusing slice: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-refused"}
	commitWindowTurns(t, ctx, run, addr, 1)
	awaitCheckpointLog(t, observer, "the framework checkpoint was not filed", refused.Error())
	if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(30 * time.Second)}); err == nil {
		t.Fatal("the close answered no failure beside a carrier nothing can read, want the refused account it could not state")
	} else if !strings.Contains(err.Error(), refused.Error()) {
		t.Fatalf("the close answered %q, want the refused account it could not state", err)
	}

	whole := windowChainRun(t, ctx)
	whole.checkpointEvery = 1
	other := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-stated"}
	commitWindowTurns(t, ctx, whole, other, 1)
	if _, err := whole.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(30 * time.Second)}); err != nil {
		t.Fatalf("the close of a run whose holders state themselves answered %v, want a closed run", err)
	}
}

// carrierSizes reports the bytes the newest carrier of one run states: the
// projection slice of the framework checkpoint and the survivor payload of the
// newest fold product, the two carriers a restart of the run reads.
func carrierSizes(t *testing.T, ctx context.Context, run *Run) (framework, product int) {
	t.Helper()
	id, filed, err := run.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the carrier of the chain = (%t, %v), want a filed fact", filed, err)
	}
	stored, err := run.CommittedEvent(ctx, id)
	if err != nil {
		t.Fatalf("read the carrier %s back: %v", id, err)
	}
	checkpoint, stated := event.CheckpointOf(stored)
	if !stated {
		t.Fatalf("the carrier %s states no readable checkpoint", id)
	}
	for _, slice := range checkpoint.Slices {
		if slice.Name == checkpointSliceProjection {
			framework = len(slice.Payload)
		}
	}
	if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, typ string, _ event.SessionID) error {
		if product > 0 || typ != event.CompressProductType {
			return nil
		}
		stored, held, err := run.tier.Segments.GetEvent(ctx, key)
		if err != nil || !held {
			return err
		}
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return err
		}
		evt := body.Event()
		evt.Key = stored.Key
		if stated, ok := compressProductOf(evt); ok {
			product = len(stated.Checkpoint())
		}
		return nil
	}); err != nil {
		t.Fatalf("walk the store index: %v", err)
	}
	if framework == 0 {
		t.Fatal("the carrier states no projection slice, so the size proves nothing")
	}
	return framework, product
}

// TestTheCheckpointCarrierStaysBoundedByItsEntries pins what a carrier states.
//   - the carrier names every survivor and leaves the text of a mapped entry on the chain, so the bodies a turn was written with never enter it
//   - a chain of the same turns written with larger bodies states the same carrier
//   - a longer history states a carrier of the same size, so the carrier follows the entries a fold leaves and never the history behind them
//   - the boot still serves the projection the full walk rebuilds, so the text left on the chain is read back
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestTheCheckpointCarrierStaysBoundedByItsEntries(t *testing.T) {
	staged := map[string]struct {
		framework int
		product   int
	}{}
	for _, scene := range []struct {
		name  string
		turns int
		body  int
	}{
		{name: "the small bodies of a short chain", turns: 12, body: 256},
		{name: "the large bodies of a short chain", turns: 12, body: 64 << 10},
		{name: "the large bodies of a long chain", turns: 36, body: 64 << 10},
	} {
		t.Run(scene.name, func(t *testing.T) {
			ctx := context.Background()
			storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
			addr := event.Address{AgentID: "agent-a", SessionID: "session-carrier-bound"}
			source := noTrimChainRun(t, ctx, storage)
			body := strings.Repeat("b", scene.body)
			for turn := 1; turn <= scene.turns; turn++ {
				for position, kind := range []string{event.TypeExternalInput, event.TypeThinkingPlan, event.TypeActionCommand, event.TypeAgentOutput} {
					commitWindowFact(t, ctx, source, addr, windowEvent(addr,
						fmt.Sprintf("%s-turn-%d-leg-%d", addr.SessionID, turn, position),
						fmt.Sprintf("inv-%d-%d", turn, position), body, kind))
				}
			}
			if count := countFoldProducts(t, ctx, source); count == 0 {
				t.Fatal("the fixture folded nothing, so the carrier proves nothing")
			}
			framework, product := carrierSizes(t, ctx, source)
			if product == 0 {
				t.Fatal("the chain filed no checkpoint-bearing product, so the carrier proves nothing")
			}
			t.Logf("the carrier states %d byte(s) of projection and %d byte(s) of product beside %d turn(s) of %d-byte bodies",
				framework, product, scene.turns, scene.body)
			staged[scene.name] = struct {
				framework int
				product   int
			}{framework: framework, product: product}
			if bound := 16 << 10; framework > bound || product > bound {
				t.Fatalf("the carrier states %d byte(s) of projection and %d byte(s) of product, want both under the %d-byte bound its entries fit in",
					framework, product, bound)
			}
			if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
				t.Fatalf("close the source process: %v", err)
			}
			booted := noTrimChainRun(t, ctx, storage)
			served, carried := booted.replays[addr]
			if !carried {
				t.Fatalf("the boot registered no projection for %v", addr)
			}
			full := noTrimChainRun(t, ctx, storage)
			fullSeeds, _, _, err := full.seedFullReplay(ctx, full.tier)
			if err != nil {
				t.Fatalf("replay the whole chain: %v", err)
			}
			want, carried := fullSeeds[addr]
			if !carried {
				t.Fatalf("the full walk registered no projection for %v", addr)
			}
			if got, want := replay.RenderProjection(served.projection.Entries()), replay.RenderProjection(want.projection.Entries()); !bytes.Equal(got, want) {
				t.Fatalf("the boot renders %d bytes, want the %d the full walk rebuilds from the chain", len(got), len(want))
			}
		})
	}
	small := staged["the small bodies of a short chain"]
	large := staged["the large bodies of a short chain"]
	if small.framework != large.framework || small.product != large.product {
		t.Fatalf("the carrier states %d/%d byte(s) beside 256-byte bodies and %d/%d beside 64KiB bodies, want the bodies to stay on the chain",
			small.framework, small.product, large.framework, large.product)
	}
	long := staged["the large bodies of a long chain"]
	if long.framework > small.framework || long.product > small.product {
		t.Fatalf("the carrier states %d/%d byte(s) beside 36 turns and %d/%d beside 12, want a carrier that follows the entries a fold leaves rather than the history",
			long.framework, long.product, small.framework, small.product)
	}
}

// requireCoveredByTheCut requires one committed fact of a restarted run to stand
// at or below the cut its carrier states for one state slice, so the check that
// reads the fact reads the account the carrier restores rather than the chain walk
// past the cut. The consequence names what a fact past the cut would leave the
// check proving nothing about.
func requireCoveredByTheCut(t *testing.T, run *Run, slice string, key event.EventID, consequence string) {
	t.Helper()
	cut := run.checkpointCut(slice)
	if len(cut) == 0 {
		t.Fatalf("the boot restored no %s cut, so %s", slice, consequence)
	}
	partition, seq := seqOfFact(t, run, key)
	if seq > cut[partition] {
		t.Fatalf("the fact %s stands at seq %d of partition %d past the %s cut %d, so %s",
			key, seq, partition, slice, cut[partition], consequence)
	}
}
