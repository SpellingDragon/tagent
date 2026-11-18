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
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestCheckpointEmittedAtCadenceAndFold pins the bounded cadence and the cut it files.
//   - a run that commits past its interval files a checkpoint fact of the frozen type
//   - the offset the fact states is the partition sequence the run had committed
//   - the fact carries every state slice mounted on the seam, so the projection survivors are restored, not re-folded
//   - the cut is taken before the checkpoint fact lands, so its own partition stands past the watermark it states
//   - a cut asked for beside a chain at rest files a checkpoint of its own, the watermark read before that checkpoint lands
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
		t.Fatalf("the checkpoint fact landed at seq %d of partition %d beside a cut of %d, want a cut behind the checkpoint it states",
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
// filing stands off the committing goroutine, so a test reads the checkpoint the
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
	endRun(t, first)

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
	endRun(t, restarted)
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

// TestCheckpointKeySurvivesTheOwnerMovingToASmallerAgent pins the key of a cut against the owner the run files it under.
//   - the chain committed first owns the cut, and the smaller agent that attaches beside it takes the ownership over
//   - the two partitions stand at the same watermark, so a key naming the watermark alone names one fact twice and the second cut is refused as a key conflict
//   - both cuts stay readable by their own identity, each in the partition of the owner that filed it
//   - a restart over the same store restores the newest cut of the frozen type and files a cut past it
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCheckpointKeySurvivesTheOwnerMovingToASmallerAgent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	}
	large := event.Address{AgentID: "agent-b", SessionID: "session-owner-large"}
	small := event.Address{AgentID: "agent-a", SessionID: "session-owner-small"}
	largePartition := memory.PartitionIDFromName(string(large.AgentID))
	smallPartition := memory.PartitionIDFromName(string(small.AgentID))
	if largePartition == smallPartition {
		t.Fatalf("both agents fold into partition %d, so the owner move proves no key of another partition", largePartition)
	}
	run := windowChainRun(t, ctx, storage())
	run.checkpointEvery = 1 << 30
	run.checkpointPeriod = 1000 * time.Hour

	commitWindowTurns(t, ctx, run, large, 2)
	run.mu.Lock()
	owner := run.checkpointOwnerLocked()
	run.mu.Unlock()
	if owner.AgentID != large.AgentID {
		t.Fatalf("the run files its cut under %q, want the only chain it holds, %q", owner.AgentID, large.AgentID)
	}
	firstID, filed, err := run.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut of the first owner = (%t, %v), want a filed fact", filed, err)
	}
	firstPartition, _ := seqOfFact(t, run, firstID)
	if firstPartition != largePartition {
		t.Fatalf("the first cut landed in partition %d, want the partition %d of its owner", firstPartition, largePartition)
	}
	_, firstOffset := run.CheckpointFiled()
	watermark := firstOffset[largePartition]

	for turn := 0; ; turn++ {
		run.landing.Lock()
		at := run.landedSeq[smallPartition]
		run.landing.Unlock()
		if at == watermark {
			break
		}
		if at > watermark || turn >= 8 {
			t.Fatalf("partition %d stands at seq %d after %d turn(s), want the watermark %d the first cut stated",
				smallPartition, at, turn, watermark)
		}
		commitWindowTurn(t, ctx, run, small, turn)
	}
	run.mu.Lock()
	owner = run.checkpointOwnerLocked()
	run.mu.Unlock()
	if owner.AgentID != small.AgentID {
		t.Fatalf("the owner stayed %q beside the smaller chain %q, so the cut proves no owner move", owner.AgentID, small.AgentID)
	}
	secondID, filed, err := run.Checkpoint(ctx)
	if err != nil {
		t.Fatalf("file the cut of the owner that moved to %s = %v (fact key conflict: %t), want a checkpoint beside the %s already filed",
			small.AgentID, err, errors.Is(err, memory.ErrFactKeyConflict), firstID)
	}
	if !filed || secondID == "" || secondID == firstID {
		t.Fatalf("the cut of the moved owner filed (%q, %t), want a fact of its own beside %q", secondID, filed, firstID)
	}
	secondPartition, _ := seqOfFact(t, run, secondID)
	if secondPartition != smallPartition {
		t.Fatalf("the second cut landed in partition %d, want the partition %d of its owner", secondPartition, smallPartition)
	}
	_, secondOffset := run.CheckpointFiled()
	for _, id := range []event.EventID{firstID, secondID} {
		stored, err := run.CommittedEvent(ctx, id)
		if err != nil {
			t.Fatalf("read the checkpoint %s back: %v", id, err)
		}
		if stored.Type != event.TypeCheckpoint {
			t.Fatalf("the fact %s is %q, want the frozen checkpoint type", id, stored.Type)
		}
		if _, stated := event.CheckpointOf(stored); !stated {
			t.Fatalf("the fact %s states no readable checkpoint", id)
		}
	}
	endRun(t, run)

	restarted := windowChainRun(t, ctx, storage())
	restarted.checkpointEvery = 1 << 30
	restarted.checkpointPeriod = 1000 * time.Hour
	restoredID, restoredOffset := restarted.CheckpointFiled()
	if restoredID != secondID {
		t.Fatalf("the restart restored %q, want the newest cut %q read by the frozen type", restoredID, secondID)
	}
	if !reflect.DeepEqual(map[int]int64(restoredOffset), map[int]int64(secondOffset)) {
		t.Fatalf("the restart restored the cut %v, want the cut %v the newest checkpoint states", restoredOffset, secondOffset)
	}
	thirdID, filed, err := restarted.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut of the restarted run = (%t, %v), want a checkpoint past the restored one", filed, err)
	}
	thirdPartition, thirdSeq := seqOfFact(t, restarted, thirdID)
	restoredPartition, restoredSeq := seqOfFact(t, restarted, secondID)
	if thirdID == secondID || thirdPartition != restoredPartition || thirdSeq <= restoredSeq {
		t.Fatalf("the restarted cut %s landed at seq %d of partition %d beside %s at seq %d of partition %d, want a later slot of the same partition",
			thirdID, thirdSeq, thirdPartition, secondID, restoredSeq, restoredPartition)
	}
}

// TestRestoreReadsTheCutPastASeatThatOmitsItsPartition pins the read of the newest cut.
//   - the seat the root states is the one an earlier frame wrote, so it omits the partition the newest cut landed in while that cut's body stands in the log
//   - the seat-bounded read answers absence beside that cut, so the fixture states the defect the restore must not inherit
//   - the restore reads the cut back by naming the partition its index entry landed in, and answers the cut it states
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRestoreReadsTheCutPastASeatThatOmitsItsPartition(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	}
	large := event.Address{AgentID: "agent-b", SessionID: "session-omitted-seat-large"}
	small := event.Address{AgentID: "agent-a", SessionID: "session-omitted-seat-small"}
	largePartition := memory.PartitionIDFromName(string(large.AgentID))
	smallPartition := memory.PartitionIDFromName(string(small.AgentID))
	if largePartition == smallPartition {
		t.Fatalf("both agents fold into partition %d, so no seat can omit one chain's partition", largePartition)
	}

	earlier := windowChainRun(t, ctx, storage())
	earlier.checkpointEvery = 1 << 30
	earlier.checkpointPeriod = 1000 * time.Hour
	commitWindowTurns(t, ctx, earlier, large, 1)
	seat := seatValueOf(t, ctx, earlier.tier)
	endRun(t, earlier)

	later := windowChainRun(t, ctx, storage())
	later.checkpointEvery = 1 << 30
	later.checkpointPeriod = 1000 * time.Hour
	commitWindowTurns(t, ctx, later, small, 1)
	cutID, filed, err := later.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut of the two chains = (%t, %v), want a filed fact", filed, err)
	}
	_, cutOffset := later.CheckpointFiled()
	cutPartition, _ := seqOfFact(t, later, cutID)
	if cutPartition != smallPartition {
		t.Fatalf("the cut landed in partition %d, want the partition %d of its owner", cutPartition, smallPartition)
	}
	endRun(t, later)
	restateSeat(t, ctx, root, seat)

	view, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the root the seat was stated over: %v", err)
	}
	if _, held, err := view.Segments.GetEvent(ctx, cutID); err != nil || held {
		t.Fatalf("the seat-bounded read of %s answers held %t, %v, want the absence of the seat it reads: the seat still states partition %d",
			cutID, held, err, smallPartition)
	}
	if _, held, err := view.Segments.GetEventInPartition(ctx, cutID, smallPartition); err != nil || !held {
		t.Fatalf("the read of %s in partition %d answers held %t, %v, want the body the log holds", cutID, smallPartition, held, err)
	}
	if err := view.Close(); err != nil {
		t.Fatalf("close the view the fixture read: %v", err)
	}

	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the root the cut was filed in: %v", err)
	}
	defer tier.Close()
	boot := windowChainRun(t, ctx)
	restoredOffset, held, err := boot.restoreNewestCheckpoint(ctx, tier)
	if err != nil || !held {
		t.Fatalf("the restore of the root = (held %t, %v), want the newest cut %s read back by the partition its entry landed in", held, err, cutID)
	}
	if !reflect.DeepEqual(map[int]int64(restoredOffset), map[int]int64(cutOffset)) {
		t.Fatalf("the restore answers the cut %v, want the cut %v the newest checkpoint states", restoredOffset, cutOffset)
	}
	if filed, _ := boot.CheckpointFiled(); filed != cutID {
		t.Fatalf("the restore filed %q, want the newest cut %q read by the frozen type beside a seat that omits its partition", filed, cutID)
	}
}

// checkpointSeatKey names the chain key the store states the partitions its index is read in under.
const checkpointSeatKey = "store:seat"

// seatValueOf answers the seat value one run's store states, so a check hands it back later.
func seatValueOf(t *testing.T, ctx context.Context, tier *ledger.Tier) []byte {
	t.Helper()
	pairs, err := tier.Store.Read(ctx, ledger.Fetch{Key: checkpointSeatKey})
	if err != nil || len(pairs) == 0 {
		t.Fatalf("read the seat of the store = (%d pair(s), %v), want the one a commit stated", len(pairs), err)
	}
	return append([]byte(nil), pairs[0].Value...)
}

// restateSeat writes one seat value back over the root's own, so a check states the partitions a later reader resolves identities by.
func restateSeat(t *testing.T, ctx context.Context, root string, seat []byte) {
	t.Helper()
	writer, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the root that states the seat: %v", err)
	}
	if err := writer.Store.Commit(ctx, ledger.Batch{Puts: []ledger.Pair{{Key: checkpointSeatKey, Value: seat}}}); err != nil {
		t.Fatalf("state the seat of the root: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the root that stated the seat: %v", err)
	}
}

// TestRestoreHydratesSurvivorsPastASeatThatOmitsTheirPartition pins the survivor read of a rebuilt chain.
//   - the seat the root states is the one an earlier frame wrote, so it omits the partition the chain and its cut landed in
//   - the seat-bounded read answers absence beside that chain's facts, so the fixture states the defect the restore must not inherit
//   - the boot hydrates the survivors from the partition its chain slot names, so the chain is rebuilt rather than refused
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRestoreHydratesSurvivorsPastASeatThatOmitsTheirPartition(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	}
	large := event.Address{AgentID: "agent-b", SessionID: "session-survivor-seat-large"}
	small := event.Address{AgentID: "agent-a", SessionID: "session-survivor-seat-small"}
	largePartition := memory.PartitionIDFromName(string(large.AgentID))
	smallPartition := memory.PartitionIDFromName(string(small.AgentID))
	if largePartition == smallPartition {
		t.Fatalf("both agents fold into partition %d, so no seat can omit the survivor chain's partition", largePartition)
	}

	earlier := windowChainRun(t, ctx, storage())
	earlier.stopCheckpointFiler()
	commitWindowTurns(t, ctx, earlier, large, 1)
	seat := seatValueOf(t, ctx, earlier.tier)
	endRun(t, earlier)

	later := windowChainRun(t, ctx, storage())
	later.stopCheckpointFiler()
	commitWindowTurns(t, ctx, later, small, 2)
	cutID, filed, err := later.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut of the two chains = (%t, %v), want a filed fact", filed, err)
	}
	if cutPartition, _ := seqOfFact(t, later, cutID); cutPartition != smallPartition {
		t.Fatalf("the cut landed in partition %d, want the partition %d of its owner", cutPartition, smallPartition)
	}
	survivor := event.EventID("")
	if err := later.tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, partition int, _ int64, typ string, _ event.SessionID) error {
		if survivor == "" && partition == smallPartition && typ != event.TypeCheckpoint {
			survivor = key
		}
		return nil
	}); err != nil {
		t.Fatalf("read the index of the surviving chain: %v", err)
	}
	if survivor == "" {
		t.Fatal("the fixture indexed no fact of the chain whose partition the seat omits")
	}
	endRun(t, later)
	restateSeat(t, ctx, root, seat)

	view, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the root the seat was stated over: %v", err)
	}
	if _, held, err := view.Segments.GetEvent(ctx, survivor); err != nil || held {
		t.Fatalf("the seat-bounded read of %s answers held %t, %v, want the absence the stale seat states", survivor, held, err)
	}
	if _, held, err := view.Segments.GetEventInPartition(ctx, survivor, smallPartition); err != nil || !held {
		t.Fatalf("the read of %s in partition %d answers held %t, %v, want the body the log holds", survivor, smallPartition, held, err)
	}
	if err := view.Close(); err != nil {
		t.Fatalf("close the view the fixture read: %v", err)
	}

	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the root the chains were filed in: %v", err)
	}
	defer tier.Close()
	boot := windowChainRun(t, ctx)
	if err := boot.restoreCheckpointState(ctx, tier); err != nil {
		t.Fatalf("the restore of the root: %v", err)
	}
	seeds, report, err := boot.seedAnchoredReplay(ctx, tier)
	if err != nil || report.Checkpoints == 0 {
		t.Fatalf("the rebuild of the root = (%d checkpoint(s), %v), want the chain rebuilt from the cut %s", report.Checkpoints, err, cutID)
	}
	seed, carried := seeds[small]
	if !carried {
		t.Fatal("the rebuild registered no projection for the chain the cut states")
	}
	for _, entry := range seed.projection.Entries() {
		if entry.Key != survivor {
			continue
		}
		if entry.Content == "" {
			t.Fatalf("the rebuilt projection states the survivor %s with no text, want the text the chain files", survivor)
		}
		return
	}
	t.Fatalf("the rebuilt projection holds %d entr(y/ies) of the chain and not the survivor %s, want the chain hydrated from its partition",
		len(seed.projection.Entries()), survivor)
}

// TestCheckpointKeysNameTheOwnerPartition pins the key of both filings of a cut against the partition it lands in.
//   - a session cut is filed under the owner partition beside the slot that partition stands at, exactly as the framework cut is
//   - the owner moving to a smaller agent standing at the same watermark files a session cut beside the one already filed
//   - successive framework cuts of one owner name later slots of that partition, so no two cuts of one partition share a key
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCheckpointKeysNameTheOwnerPartition(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root}))
	run.checkpointEvery = 1 << 30
	run.checkpointPeriod = 1000 * time.Hour
	large := event.Address{AgentID: "agent-b", SessionID: "session-key-large"}
	small := event.Address{AgentID: "agent-a", SessionID: "session-key-small"}
	largePartition := memory.PartitionIDFromName(string(large.AgentID))
	smallPartition := memory.PartitionIDFromName(string(small.AgentID))
	if largePartition == smallPartition {
		t.Fatalf("both agents fold into partition %d, so the owner move proves no key of another partition", largePartition)
	}

	commitWindowTurns(t, ctx, run, large, 2)
	largeCut, filed, err := run.CheckpointSession(ctx, large)
	if err != nil || !filed {
		t.Fatalf("file the session cut of the first owner = (%t, %v), want a filed fact", filed, err)
	}
	cutPartition, cutSeq := seqOfFact(t, run, largeCut)
	if cutPartition != largePartition {
		t.Fatalf("the session cut landed in partition %d, want the partition %d of its owner", cutPartition, largePartition)
	}
	if string(largeCut) != event.CheckpointKey(largePartition, cutSeq-1) {
		t.Fatalf("the session cut is keyed %s, want the partition %d beside the slot %d it states", largeCut, largePartition, cutSeq-1)
	}

	watermark := cutSeq - 1
	for turn := 0; ; turn++ {
		run.landing.Lock()
		at := run.landedSeq[smallPartition]
		run.landing.Unlock()
		if at == watermark {
			break
		}
		if at > watermark || turn >= 8 {
			t.Fatalf("partition %d stands at seq %d after %d turn(s), want the watermark %d the session cut stated",
				smallPartition, at, turn, watermark)
		}
		commitWindowTurn(t, ctx, run, small, turn)
	}
	smallCut, filed, err := run.CheckpointSession(ctx, small)
	if err != nil {
		t.Fatalf("file the session cut of the owner that moved to %s = %v (fact key conflict: %t), want a checkpoint beside the %s already filed",
			small.AgentID, err, errors.Is(err, memory.ErrFactKeyConflict), largeCut)
	}
	if !filed || smallCut == largeCut {
		t.Fatalf("the session cut of the moved owner filed (%q, %t), want a fact of its own beside %q", smallCut, filed, largeCut)
	}
	movedPartition, movedSeq := seqOfFact(t, run, smallCut)
	if movedPartition != smallPartition {
		t.Fatalf("the moved session cut landed in partition %d, want the partition %d of its owner", movedPartition, smallPartition)
	}
	if string(smallCut) != event.CheckpointKey(smallPartition, movedSeq-1) {
		t.Fatalf("the moved session cut is keyed %s, want the partition %d beside the slot %d it states", smallCut, smallPartition, movedSeq-1)
	}
	if movedSeq != cutSeq {
		t.Fatalf("the two session cuts landed at seq %d and %d, want the one watermark both partitions stood at", movedSeq, cutSeq)
	}
	for _, id := range []event.EventID{largeCut, smallCut} {
		stored, err := run.CommittedEvent(ctx, id)
		if err != nil {
			t.Fatalf("read the session checkpoint %s back: %v", id, err)
		}
		if _, stated := event.CheckpointOf(stored); !stated {
			t.Fatalf("the fact %s states no readable checkpoint", id)
		}
	}

	previous, previousSeq := event.EventID(""), int64(0)
	for cut := 0; cut < 2; cut++ {
		id, filed, err := run.Checkpoint(ctx)
		if err != nil || !filed {
			t.Fatalf("file the framework cut %d of the moved owner = (%t, %v), want a filed fact", cut, filed, err)
		}
		partition, seq := seqOfFact(t, run, id)
		if partition != smallPartition {
			t.Fatalf("the framework cut landed in partition %d, want the partition %d of its owner", partition, smallPartition)
		}
		if string(id) != event.CheckpointKey(partition, seq-1) {
			t.Fatalf("the framework cut is keyed %s, want the partition %d beside the slot %d it states", id, partition, seq-1)
		}
		if previous != "" && (id == previous || seq <= previousSeq) {
			t.Fatalf("the framework cut %s landed at seq %d beside the earlier %s at seq %d, want a later slot of the same partition",
				id, seq, previous, previousSeq)
		}
		previous, previousSeq = id, seq
	}
}

// checkpointBase is the moment the seeded turns of a window chain start at.
func checkpointBase() time.Time {
	return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
}

// TestBootRefusesAnUnreadableCheckpoint pins the negative anchor of the anchored boot.
//   - an unreadable payload, one of another layout version and one stating a foreign identity each refuse the boot
//   - the refusal is the named one and names the checkpoint fact it refused
//   - the unreadable checkpoint is the newest fact of the store: the close files a checkpoint of its own before it, so the fixture appends the corrupted one last
//   - the boot answers no run beside the refusal (negative: a reader that skipped the unreadable checkpoint would start onto an empty state)
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
			endRun(t, corrupt)
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
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}, {ID: "agent-b", Model: "model-a", ReplayWindow: 60}},
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
//   - the cut answers the moment the pipeline is quiet, filing the checkpoint the run owes
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
//   - the checkpoint the due owes lands after the holder released the lock, so the due is filed and not dropped
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
		t.Fatal("the cadence filed no checkpoint for the due the commit reached under the holder lock")
	}
}

// TestRestoredOwedRoundIsTheFactTheChainKeeps pins the owed-input slice.
//   - the checkpoint names the identity of each round nobody drove
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
		t.Fatalf("the restore answered %d owed round(s), want the one the checkpoint named", len(restored))
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
// checkpoint states it, so a boot over that checkpoint reports how many times the cut
// reached its holder.
type restoreCounter struct {
	restores *int
}

// StateName reports the name the counted slice is keyed by.
func (s restoreCounter) StateName() string { return "assembly.restore-counter" }

// SnapshotState states the slice the checkpoint holds.
func (s restoreCounter) SnapshotState(context.Context) ([]byte, error) { return []byte("{}"), nil }

// RestoreState counts one application of the checkpoint.
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

// TestBootRestoresTheCheckpointOnce pins the single restore of one boot.
//   - a boot over a store holding a checkpoint hands the cut to each holder once
//   - a boot over a store holding no checkpoint states the empty cut and hands that to each holder once as well
//   - the run that files the checkpoint restores the empty cut it booted beside, and the run that reads it restores the very slice the checkpoint states
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestBootRestoresTheCheckpointOnce(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	sourceRestores := 0
	source := windowChainRun(t, ctx, storage, countingBoot(&sourceRestores))
	addr := event.Address{AgentID: "agent-a", SessionID: "session-restore-once"}
	commitWindowTurns(t, ctx, source, addr, 1)
	if _, filed, err := source.Checkpoint(ctx); err != nil || !filed {
		t.Fatalf("file the checkpoint of the chain = (%t, %v), want a filed fact", filed, err)
	}
	if sourceRestores != 1 {
		t.Fatalf("the boot of a store holding no checkpoint applied the empty cut %d time(s), want the one restore every boot hands its holders: a boot that skips them leaves the work they state unseated", sourceRestores)
	}

	restores := 0
	endRun(t, source)
	restarted := windowChainRun(t, ctx, storage, countingBoot(&restores))
	if filed, _ := restarted.CheckpointFiled(); filed == "" {
		t.Fatal("the restart states no checkpoint, so the count proves nothing")
	}
	if restores != 1 {
		t.Fatalf("the restart applied the cut %d time(s), want once", restores)
	}
}

// checkpointLog records the lines the assembly face writes at ERROR, so a check
// reads the failure a checkpoint path reports instead of assuming it reported one.
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

// Debug drops one debug line: the checkpoint failures are the only ones a check reads.
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
// chain filed, so a check reads the cut a checkpoint states against the fact that
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
//   - a chain whose interval and period never fall due files no checkpoint while it only appends
//   - the commit that lands a fold product asks for a checkpoint, and the cut it states covers that product
//   - a fold can mint several products in one pass, and the checkpoint answering the last of them states a cut the newest product landed behind
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
			t.Fatalf("the chain filed the checkpoint %s beside no fold product, want the cadence clock to owe none before the fold", filed)
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
			t.Fatalf("the fold filed no checkpoint past the product %s at seq %d of partition %d after 20s, the newest cut states %d",
				product, productSeq, partition, offset[partition])
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestCloseFilesTheCheckpointTheNextBootReads pins the close trigger of the cadence.
//   - a run whose interval and period never fell due files no checkpoint while it commits
//   - the close files one behind the facts it settled
//   - the checkpoint the close filed is the one the next boot restores
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCloseFilesTheCheckpointTheNextBootReads(t *testing.T) {
	ctx := context.Background()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: t.TempDir()})
	run := windowChainRun(t, ctx, storage)
	run.checkpointEvery = 1 << 30
	run.checkpointPeriod = 1000 * time.Hour
	addr := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-close"}
	commitWindowTurns(t, ctx, run, addr, 1)
	if filed, _ := run.CheckpointFiled(); filed != "" {
		t.Fatalf("the chain filed the checkpoint %s beside no due, want the cadence to owe none before the close", filed)
	}
	if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(30 * time.Second)}); err != nil {
		t.Fatalf("close the run: %v", err)
	}
	id, offset := run.CheckpointFiled()
	if id == "" {
		t.Fatal("the close filed no checkpoint, want the account the run reached at its fixed point")
	}
	partition := memory.PartitionIDFromName(string(addr.AgentID))
	if offset[partition] <= 0 {
		t.Fatalf("the checkpoint the close filed states the cut %d of partition %d, want the facts the run settled", offset[partition], partition)
	}
	booted := windowChainRun(t, ctx, storage)
	if restored, _ := booted.CheckpointFiled(); restored != id {
		t.Fatalf("the boot restored the checkpoint %q, want the %q the close filed", restored, id)
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

// TestAFailedCheckpointSurfaces pins the failure path of the checkpoint.
//   - a holder that refuses to state itself fails every cut it is part of, and the filer reports that at ERROR
//   - the close answers the failure to its caller instead of reporting a close beside a checkpoint no process can read
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
		t.Fatal("the close answered no failure beside a checkpoint nothing can read, want the refused account it could not state")
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

// checkpointSizes reports the bytes the newest checkpoint of one run states: the
// projection slice of the framework checkpoint and the survivor payload of the
// newest fold product, the two checkpoints a restart of the run reads.
func checkpointSizes(t *testing.T, ctx context.Context, run *Run) (framework, product int) {
	t.Helper()
	id, filed, err := run.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the checkpoint of the chain = (%t, %v), want a filed fact", filed, err)
	}
	stored, err := run.CommittedEvent(ctx, id)
	if err != nil {
		t.Fatalf("read the checkpoint %s back: %v", id, err)
	}
	checkpoint, stated := event.CheckpointOf(stored)
	if !stated {
		t.Fatalf("the checkpoint %s states no readable checkpoint", id)
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
		t.Fatal("the checkpoint states no projection slice, so the size proves nothing")
	}
	return framework, product
}

// TestTheCheckpointStaysBoundedByItsEntries pins what a checkpoint states.
//   - the checkpoint names every survivor and leaves the text of a mapped entry on the chain, so the bodies a turn was written with never enter it
//   - a chain of the same turns written with larger bodies states the same checkpoint
//   - a longer history states a checkpoint of the same size, so the checkpoint follows the entries a fold leaves and never the history behind them
//   - the boot still serves the projection the full walk rebuilds, so the text left on the chain is read back
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestTheCheckpointStaysBoundedByItsEntries(t *testing.T) {
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
			addr := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-bound"}
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
				t.Fatal("the fixture folded nothing, so the checkpoint proves nothing")
			}
			framework, product := checkpointSizes(t, ctx, source)
			if product == 0 {
				t.Fatal("the chain filed no checkpoint-bearing product, so the checkpoint proves nothing")
			}
			t.Logf("the checkpoint states %d byte(s) of projection and %d byte(s) of product beside %d turn(s) of %d-byte bodies",
				framework, product, scene.turns, scene.body)
			staged[scene.name] = struct {
				framework int
				product   int
			}{framework: framework, product: product}
			if bound := 16 << 10; framework > bound || product > bound {
				t.Fatalf("the checkpoint states %d byte(s) of projection and %d byte(s) of product, want both under the %d-byte bound its entries fit in",
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
			endRun(t, booted)
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
		t.Fatalf("the checkpoint states %d/%d byte(s) beside 256-byte bodies and %d/%d beside 64KiB bodies, want the bodies to stay on the chain",
			small.framework, small.product, large.framework, large.product)
	}
	long := staged["the large bodies of a long chain"]
	if long.framework > small.framework || long.product > small.product {
		t.Fatalf("the checkpoint states %d/%d byte(s) beside 36 turns and %d/%d beside 12, want a checkpoint that follows the entries a fold leaves rather than the history",
			long.framework, long.product, small.framework, small.product)
	}
}

// requireCoveredByTheCut requires one committed fact of a restarted run to stand
// at or below the cut its checkpoint states for one state slice, so the check that
// reads the fact reads the account the checkpoint restores rather than the chain walk
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

// commitSessionTurns commits closed turns of four legs into one session chain,
// under keys the tag keeps apart from every other generation's: a restart over one
// durable root commits the same shape again, so the identity may not repeat.
func commitSessionTurns(t *testing.T, ctx context.Context, run *Run, addr event.Address, tag string, turns int) {
	t.Helper()
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for turn := 0; turn < turns; turn++ {
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
				Key:       event.EventID(fmt.Sprintf("%s-%d-%d", tag, turn, position)),
				Type:      leg.typ,
				Timestamp: base.Add(time.Duration(turn*10+position) * time.Minute),
				Meta:      map[string]string{event.MetaKeyEventSummary: leg.summary},
				Message:   model.Message{Role: model.RoleUser, Content: "the material of the generation walk"},
				Owner:     addr, Source: addr, Target: addr,
			}
			if _, err := run.CommitToSession(ctx, addr, evt); err != nil {
				t.Fatalf("commit %s: %v", evt.Key, err)
			}
		}
	}
}

// checkpointPayloadOf renders every slice one checkpoint states, so a check reads what
// the checkpoint carries rather than what a slice claims to be named.
func checkpointPayloadOf(t *testing.T, checkpoint event.Checkpoint) string {
	t.Helper()
	var rendered strings.Builder
	for _, slice := range checkpoint.Slices {
		rendered.WriteString(slice.Name)
		rendered.WriteString("=")
		rendered.WriteString(string(slice.Payload))
		rendered.WriteString("\n")
	}
	return rendered.String()
}

// checkpointOfIdentity reads one filed checkpoint back as the checkpoint it states.
func checkpointOfIdentity(t *testing.T, ctx context.Context, run *Run, key event.EventID) event.Checkpoint {
	t.Helper()
	stored, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("read the checkpoint %s back: %v", key, err)
	}
	checkpoint, stated := event.CheckpointOf(stored)
	if !stated {
		t.Fatalf("the checkpoint %s states no readable checkpoint", key)
	}
	return checkpoint
}

// TestSymptomAnchorSessionBootCostFlatAcrossGenerations pins that one session's recovery cost stays flat while its chain grows.
//   - fixture: one session runs over one durable root, each generation restarts the process and commits its own turns
//   - every boot states what it had to read: the checkpoint that session filed plus the tail past its boundary
//   - positive: the bodies a boot reads stop growing after the first checkpoint exists, so the generations add material without adding recovery cost
//   - negative: the boot reads a small fraction of the indexed chain, which a recovery that fell back to a per-site full-history scan could not
//   - contrast: the full walk of the same chain is the measured denominator the boot is held against
//   - consumer: the real boot entry (restoreCheckpointState → seedAnchoredReplay), never a hand-driven rebuild
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestSymptomAnchorSessionBootCostFlatAcrossGenerations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-boot-cost"}
	const generations = 5
	costs := make([]int, 0, generations)
	indexed := make([]int, 0, generations)
	for generation := 0; generation < generations; generation++ {
		boot := noTrimChainRun(t, ctx, storage)
		boot.stopCheckpointFiler()
		costs = append(costs, boot.CommittedFacts())
		indexed = append(indexed, chainIndexSize(t, ctx, boot))
		commitSessionTurns(t, ctx, boot, addr, fmt.Sprintf("gen-%d", generation), 6)
		if _, _, err := boot.CheckpointSession(ctx, addr); err != nil {
			t.Fatalf("file the session checkpoint of generation %d: %v", generation, err)
		}
		if _, _, err := boot.Checkpoint(ctx); err != nil {
			t.Fatalf("file the framework checkpoint of generation %d: %v", generation, err)
		}
		if _, err := boot.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
			t.Fatalf("close the process of generation %d: %v", generation, err)
		}
	}
	t.Logf("the boots read %v bodies over the indexed %v", costs, indexed)
	if indexed[generations-1] <= indexed[1] {
		t.Fatalf("the chain indexed %d facts at the last generation and %d at the second, want a chain that grew so the flat cost is a claim about recovery rather than about a chain that stood still: the index is the denominator the recovery is measured against",
			indexed[generations-1], indexed[1])
	}
	flat := costs[generations-1]
	for generation := 2; generation < generations; generation++ {
		if costs[generation] != flat {
			t.Fatalf("the boot of generation %d read %d bodies beside the %d of the last generation, want the recovery cost to stay flat across the generations (%v)",
				generation, costs[generation], flat, costs)
		}
	}
	if flat*2 >= indexed[generations-1] {
		t.Fatalf("the boot read %d of the %d indexed facts, want a recovery that reads its checkpoint and tail rather than the history the chain already settled",
			flat, indexed[generations-1])
	}
	fullWalk := noTrimNoWindowRun(t, ctx, storage)
	fullWalk.stopCheckpointFiler()
	if _, _, fullReport, err := fullWalk.seedFullReplay(ctx, fullWalk.tier); err != nil {
		t.Fatalf("replay the whole chain for the contrast: %v", err)
	} else if fullReport.Decoded <= flat*2 {
		t.Fatalf("the full walk decoded %d bodies beside the %d a boot reads, want a walk far larger than the recovery, because the same chain walked in full is the cost a recovery pays per site when it falls back to the history",
			fullReport.Decoded, flat)
	}
}

// TestSymptomAnchorCompressionJudgmentParityAfterRestart pins that a restart answers the live process's compression judgments, entry by entry.
//   - parity: the live account, the booted account and the full walk of the chain are compared through requireSameAccount
//   - positive: the booted account equals the live account, and the full walk of the same chain equals both
//   - negative: the boot answers judgments no judgment fact of its ledger holds, which is the checkpoint's account rather than a replayed one
//   - negative: a boot whose checkpoint stated no compression account answers the batch past the cut alone and never the whole one
//   - consumer: the real boot entry over the durable root, beside TestCompressionJudgmentsSurviveTheBootCut's checkpoint-reading check of the same parity
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestSymptomAnchorCompressionJudgmentParityAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-parity-anchor"}
	source := windowChainRun(t, ctx, storage)
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, "parity", 12)
	live := compressAccountOf(t, source.compress)
	if live.judgedCount() == 0 {
		t.Fatal("the fixture judged nothing, so the parity proves nothing")
	}
	if _, _, err := source.CheckpointSession(ctx, addr); err != nil {
		t.Fatalf("file the session checkpoint of the chain: %v", err)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	booted := windowChainRun(t, ctx, storage)
	requireSameAccount(t, "booted", compressAccountOf(t, booted.compress), live)
	covered := 0
	for request := range live.Answered {
		booted.mu.Lock()
		_, read := booted.facts[request]
		booted.mu.Unlock()
		if !read {
			covered++
		}
	}
	if covered == 0 {
		t.Fatal("the boot read every judgment it answers, so the checkpoint carried none of them")
	}
	endRun(t, booted)

	full := windowChainRun(t, ctx, storage)
	resetCompression(full)
	if _, _, _, err := full.seedFullReplay(ctx, full.tier); err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	requireSameAccount(t, "full walk", compressAccountOf(t, full.compress), live)
	t.Logf("the boot answers %d judgment(s), %d of them beside no judgment fact in its ledger", len(live.Answered), covered)
}

// TestSessionCutStatesOnlyTheTriggeringSession pins that a session checkpoint states its own chain and pays nothing for the chains beside it.
//   - positive: the checkpoint of session A states A's projection, owed inputs, compression account and delivery obligations, and names A
//   - negative: no slice of A's checkpoint names session B or any fact of B's chain, so a cut of one session reads no state of another
//   - consumer: Run.CheckpointSession, the entry a session context requests a cut through
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestSessionCutStatesOnlyTheTriggeringSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	run.stopCheckpointFiler()
	triggering := event.Address{AgentID: "agent-a", SessionID: "session-cut-triggering"}
	beside := event.Address{AgentID: "agent-b", SessionID: "session-cut-beside"}
	commitSessionTurns(t, ctx, run, triggering, "cut-triggering", 6)
	commitSessionTurns(t, ctx, run, beside, "cut-beside", 6)

	key, filed, err := run.CheckpointSession(ctx, triggering)
	if err != nil || !filed {
		t.Fatalf("file the session checkpoint = (%t, %v), want a filed fact", filed, err)
	}
	checkpoint := checkpointOfIdentity(t, ctx, run, key)
	names := checkpoint.CheckpointSliceNames()
	if len(names) == 0 {
		t.Fatal("the session checkpoint states no slice, so it carries no state of the chain that asked for it")
	}
	for _, unsessioned := range []string{checkpointSliceTasks, state.StateSliceMailbox} {
		if slices.Contains(names, unsessioned) {
			t.Fatalf("the session checkpoint states the %q slice beside %v, want the slices a session chain owns", unsessioned, names)
		}
	}
	payload := checkpointPayloadOf(t, checkpoint)
	if !strings.Contains(payload, string(triggering.SessionID)) {
		t.Fatalf("the checkpoint states %q, want the chain that asked for the cut", payload)
	}
	if strings.Contains(payload, string(beside.SessionID)) || strings.Contains(payload, "cut-beside-") {
		t.Fatalf("the checkpoint states the chain beside the one that asked for it: %q", payload)
	}
}

// TestSessionCheckpointNamesTheChainWithoutEnteringIt pins the read face of a chain beside the checkpoint it filed.
//   - negative: EachSessionFact streams the committed material and leaves the checkpoint out, so a rebuild reads no state slice as a line
//   - positive: the index still answers the newest checkpoint of that chain, so the exclusion hides no state from the boot that recovers it
//   - consumer: Run.EachSessionFact, the cold-start read face a rebuild replays, over a run whose session cut filed a checkpoint
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestSessionCheckpointNamesTheChainWithoutEnteringIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	run.stopCheckpointFiler()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-stream"}
	commitSessionTurns(t, ctx, run, addr, "checkpoint-stream", 4)

	key, filed, err := run.CheckpointSession(ctx, addr)
	if err != nil || !filed {
		t.Fatalf("file the session checkpoint = (%t, %v), want a filed fact", filed, err)
	}
	streamed := map[event.EventID]bool{}
	if err := run.EachSessionFact(ctx, addr, func(evt event.Event) error {
		if evt.Type == event.TypeCheckpoint {
			t.Fatalf("the chain stream carries the checkpoint %s, want the material the chain committed alone", evt.Key)
		}
		streamed[evt.Key] = true
		return nil
	}); err != nil {
		t.Fatalf("stream the session chain: %v", err)
	}
	for turn := 0; turn < 4; turn++ {
		key := event.EventID(fmt.Sprintf("checkpoint-stream-%d-0", turn))
		if !streamed[key] {
			t.Fatalf("the chain stream left out the committed fact %s beside the checkpoint the cut filed", key)
		}
	}
	checkpoint, held, err := newestSessionCheckpointRef(ctx, run.tier, addr.SessionID)
	if err != nil || !held {
		t.Fatalf("the index answered the chain's checkpoint = (%t, %v), want the cut the chain filed", held, err)
	}
	if checkpoint.key != key {
		t.Fatalf("the index answers the checkpoint %s, want the newest cut %s the chain filed", checkpoint.key, key)
	}
}

// TestSessionCutReadsNoOtherSessionResidue pins that a chain's cut states one checkpoint whatever the chains beside it grew to.
//   - positive: the checkpoint of a quiet chain states the same bytes whether a chain beside it is young or long
//   - negative: the long chain's material reaches no slice of the quiet chain's checkpoint, which a cut that read the whole account could not say
//   - consumer: Run.CheckpointSession over a run holding two chains, one of them folded many times
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestSessionCutReadsNoOtherSessionResidue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	run.stopCheckpointFiler()
	quiet := event.Address{AgentID: "agent-a", SessionID: "session-cut-quiet"}
	long := event.Address{AgentID: "agent-b", SessionID: "session-cut-long"}
	commitSessionTurns(t, ctx, run, quiet, "residue-quiet", 2)
	commitSessionTurns(t, ctx, run, long, "residue-long", 4)
	early, filed, err := run.CheckpointSession(ctx, quiet)
	if err != nil || !filed {
		t.Fatalf("file the quiet chain's first checkpoint = (%t, %v), want a filed fact", err == nil && filed, err)
	}
	earlyBytes := len(checkpointPayloadOf(t, checkpointOfIdentity(t, ctx, run, early)))

	commitSessionTurns(t, ctx, run, long, "residue-long-more", 12)
	late, filed, err := run.CheckpointSession(ctx, quiet)
	if err != nil || !filed {
		t.Fatalf("file the quiet chain's second checkpoint = (%t, %v), want a filed fact", err == nil && filed, err)
	}
	checkpoint := checkpointOfIdentity(t, ctx, run, late)
	payload := checkpointPayloadOf(t, checkpoint)
	if strings.Contains(payload, string(long.SessionID)) || strings.Contains(payload, "residue-long") {
		t.Fatalf("the quiet chain's checkpoint states the long chain's material: %q", payload)
	}
	lateBytes := len(payload)
	if lateBytes != earlyBytes {
		t.Fatalf("the quiet chain's checkpoint states %d bytes beside %d before the long chain grew, want a cut whose cost does not follow another chain's history",
			lateBytes, earlyBytes)
	}
}

// declareObligationTargeting commits the output fact of one chain that declares a
// delivery against another chain, through the commit entry the loop lands its
// output on, and answers the identity the declaration seats.
func declareObligationTargeting(t *testing.T, ctx context.Context, run *Run, owner, target event.Address, key event.EventID, body string) string {
	t.Helper()
	evt := event.Event{
		Key:       key,
		Type:      event.TypeAgentOutput,
		Timestamp: time.Now(),
		Message:   model.Message{Role: model.RoleAssistant, Content: body},
		Owner:     owner, Source: owner, Target: target,
		Meta: map[string]string{
			deliveryOwedMeta:          "1",
			deliveryTargetAgentMeta:   string(target.AgentID),
			deliveryTargetSessionMeta: string(target.SessionID),
			deliveryLineageMeta:       "user",
		},
	}
	declaration, declared := declaredObligationOf(evt)
	if !declared {
		t.Fatalf("the fact %s declares no obligation", key)
	}
	if _, err := run.CommitToSession(ctx, owner, evt); err != nil {
		t.Fatalf("commit the declaration %s: %v", key, err)
	}
	return declaration.identity
}

// TestCrossSessionDeliveryObligationSurvivesTheTargetSessionCut pins that an obligation declared against a chain is the target's to recover.
//   - positive: the checkpoint a target session files states the obligation declared against it, with the delivery it owes
//   - negative: the source chain's checkpoint states no obligation of the target chain, because the account of a chain is what that chain is owed
//   - consumer: declareObligation seats it at its landing and a boot over the checkpoint hands it back through the delivery slice restore
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestCrossSessionDeliveryObligationSurvivesTheTargetSessionCut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	source := event.Address{AgentID: deliveryTestAgent, SessionID: "session-delivery-source"}
	target := event.Address{AgentID: deliveryTestAgent, SessionID: "session-delivery-target"}
	run := windowChainRun(t, ctx, storage)
	run.stopCheckpointFiler()
	host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))
	if err := host.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account: %v", err)
	}
	identity := declareObligationTargeting(t, ctx, run, source, target, "delivery-target-declaration", "the body owed to the target chain")
	commitSessionTurns(t, ctx, run, target, "delivery-target", 2)

	key, filed, err := run.CheckpointSession(ctx, target)
	if err != nil || !filed {
		t.Fatalf("file the target chain's checkpoint = (%t, %v), want a filed fact", filed, err)
	}
	wire, stated := checkpointDeliveryOf(t, ctx, run, key)
	if !stated {
		t.Fatal("the target chain's checkpoint states no delivery slice, want the obligation declared against it")
	}
	owed, carried := owedOfIdentity(wire, identity)
	if !carried {
		t.Fatalf("the target chain's checkpoint owes %d obligation(s) and none is %s", len(wire.Owed), identity)
	}
	if owed.Session != string(target.SessionID) {
		t.Fatalf("the checkpoint states the obligation against %q, want the target chain %q", owed.Session, target.SessionID)
	}
	endRun(t, run)

	booted := windowChainRun(t, ctx, storage)
	booted.stopCheckpointFiler()
	sender := newDeliveryTestSender(nil)
	bootedHost := deliveryTestHost(ctx, booted, sender)
	if err := bootedHost.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account of the boot: %v", err)
	}
	if err := bootedHost.restoreEventifiedDeliveries(ctx); err != nil {
		t.Fatalf("restore the account the checkpoint states: %v", err)
	}
	bootedHost.mu.Lock()
	account := bootedHost.deliveryAccountLocked(target.SessionID, false)
	declared, parked, state := false, false, ""
	if account != nil {
		_, declared = account.declared[identity]
		_, parked = account.held[identity]
		state = account.states[identity]
	}
	bootedHost.mu.Unlock()
	sender.mu.Lock()
	sent := append([]deliveryTestAttempt(nil), sender.attempts...)
	sender.mu.Unlock()
	if !declared && !parked && len(sent) == 0 && state == "" {
		t.Fatalf("the boot owes no delivery for %s and sent none, want the obligation the target chain's checkpoint states", identity)
	}
	if len(sent) > 0 && (sent[0].Target != target || sent[0].Content != "the body owed to the target chain") {
		t.Fatalf("the boot delivered %q to %v, want the body the checkpoint states to %v", sent[0].Content, sent[0].Target, target)
	}
}

// TestSourceSessionCutMissesNoTargetingObligation pins that a cut of the declaring chain states nothing of the target and loses nothing.
//   - positive: the source chain's checkpoint states no obligation of the target chain, so the source cut pays nothing for it
//   - negative: the obligation still stands owed after the source cut, so the quiet point of the chain that declared it neither states nor drops it
//   - consumer: the account applyObligation seats at the landing, read after Run.CheckpointSession
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestSourceSessionCutMissesNoTargetingObligation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	run.stopCheckpointFiler()
	source := event.Address{AgentID: deliveryTestAgent, SessionID: "session-declaring-source"}
	target := event.Address{AgentID: deliveryTestAgent, SessionID: "session-declared-target"}
	host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))
	if err := host.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account: %v", err)
	}
	identity := declareObligationTargeting(t, ctx, run, source, target, "delivery-source-declaration", "the body the source chain declared")
	commitSessionTurns(t, ctx, run, source, "declaring-source", 2)

	key, filed, err := run.CheckpointSession(ctx, source)
	if err != nil || !filed {
		t.Fatalf("file the source chain's checkpoint = (%t, %v), want a filed fact", filed, err)
	}
	if wire, stated := checkpointDeliveryOf(t, ctx, run, key); stated {
		if _, carried := owedOfIdentity(wire, identity); carried {
			t.Fatalf("the source chain's checkpoint states the obligation %s its target owes, want the account of a chain to be what that chain is owed", identity)
		}
	}
	host.mu.Lock()
	stillOwed := false
	if account := host.deliveryAccountLocked(target.SessionID, false); account != nil {
		_, stillOwed = account.declared[identity]
	}
	host.mu.Unlock()
	if !stillOwed {
		t.Fatalf("the account lost the obligation %s at the cut of the declaring chain, want it standing for the chain it targets", identity)
	}
}

// sessionRecoveryFixture runs one durable root through two session chains that
// both filed their own checkpoint, and answers the process that left it.
func sessionRecoveryFixture(t *testing.T, ctx context.Context) (storage Option, kept, other event.Address) {
	t.Helper()
	storage = WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	kept = event.Address{AgentID: deliveryTestAgent, SessionID: "session-recovery-kept"}
	other = event.Address{AgentID: deliveryTestAgent, SessionID: "session-recovery-other"}
	source := windowChainRun(t, ctx, storage)
	source.stopCheckpointFiler()
	host := deliveryTestHost(ctx, source, newDeliveryTestSender(nil))
	if err := host.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account of the source: %v", err)
	}
	if err := host.writeDeliveryState(ctx, "delivery-recovery-held",
		plugin.Delivery{Content: "the body held across the restart", Lineage: "user", Target: kept},
		heldStateOf(deliveryReasonApproval), time.Now()); err != nil {
		t.Fatalf("write the held settlement of the kept chain: %v", err)
	}
	for index := 0; index < 3; index++ {
		key := fmt.Sprintf("kept-in-%d", index)
		commitWindowFact(t, ctx, source, kept, windowEvent(kept, key, "inv-kept-"+key, "the round of the kept chain", event.TypeExternalInput))
		commitWindowFact(t, ctx, source, kept, windowEvent(kept, "kept-out-"+key, "inv-kept-"+key, "the answer of the kept chain", event.TypeAgentOutput))
	}
	for index := 0; index < 9; index++ {
		key := fmt.Sprintf("other-in-%d", index)
		commitWindowFact(t, ctx, source, other, windowEvent(other, key, "inv-other-"+key, "the round of the chain beside", event.TypeExternalInput))
	}
	if _, _, err := source.CheckpointSession(ctx, kept); err != nil {
		t.Fatalf("file the kept chain's checkpoint: %v", err)
	}
	if _, _, err := source.CheckpointSession(ctx, other); err != nil {
		t.Fatalf("file the chain beside: %v", err)
	}
	endRun(t, source)
	return storage, kept, other
}

// TestSessionRecoveryReplaysOnlyItsTail pins that a restart rebuilds the chain its checkpoint states, past the boundary it names.
//   - positive: a boot over the checkpoints answers the projection the live process held for the kept chain, entry for entry
//   - negative: the boot reads far fewer bodies than the chain holds, so the rebuild completes the state the checkpoint states instead of folding the history again
//   - consumer: the real boot entry (restoreCheckpointState → seedAnchoredReplay) over the durable root
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestSessionRecoveryReplaysOnlyItsTail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage, kept, _ := sessionRecoveryFixture(t, ctx)

	booted := noTrimChainRun(t, ctx, storage)
	booted.stopCheckpointFiler()
	rebuilt, carried := booted.replays[kept]
	if !carried {
		t.Fatalf("the boot registered no projection for %v", kept)
	}
	if len(rebuilt.projection.Entries()) == 0 {
		t.Fatal("the boot rebuilt no entry of the chain its checkpoint states")
	}
	endRun(t, booted)
	full := noTrimNoWindowRun(t, ctx, storage)
	full.stopCheckpointFiler()
	seeds, _, fullReport, err := full.seedFullReplay(ctx, full.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	if decoded := booted.CommittedFacts(); decoded >= fullReport.Decoded {
		t.Fatalf("the boot read %d bodies beside the %d of a full walk, want the tail past the boundary the checkpoints name",
			decoded, fullReport.Decoded)
	}
	if seed, held := seeds[kept]; held {
		got, want := rebuilt.projection.Entries(), seed.projection.Entries()
		if len(got) != len(want) {
			t.Fatalf("the boot holds %d entries of the kept chain, want the %d the rebuild answers", len(got), len(want))
		}
		for at := range got {
			if got[at].Key != want[at].Key {
				t.Fatalf("entry %d of the boot is %s, want %s", at, got[at].Key, want[at].Key)
			}
		}
	}
}

// TestReplayFiresNoSettleDuringRebuild pins that the replay folds state and drives no observation.
//   - positive: the boot commits no settlement fact and sends no delivery
//   - negative: the count of settlement facts in the store is the one the process before it left, so a rebuild that fired one settle is red
//   - consumer: the boot entry over the durable root, beside the delivery face the account is restored through
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestReplayFiresNoSettleDuringRebuild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage, _, _ := sessionRecoveryFixture(t, ctx)
	before := 0
	count := func(run *Run) int {
		t.Helper()
		settled := 0
		if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(_ event.EventID, _ int, _ int64, typ string, _ event.SessionID) error {
			if typ == deliverySettlementType {
				settled++
			}
			return nil
		}); err != nil {
			t.Fatalf("count the settlement facts: %v", err)
		}
		return settled
	}
	probe := noTrimChainRun(t, ctx, storage)
	probe.stopCheckpointFiler()
	before = count(probe)
	if before == 0 {
		t.Fatal("the fixture committed no settlement fact, so the count proves nothing about a rebuild that would add one")
	}
	endRun(t, probe)

	booted := noTrimChainRun(t, ctx, storage)
	booted.stopCheckpointFiler()
	sender := newDeliveryTestSender(nil)
	host := deliveryTestHost(ctx, booted, sender)
	if err := host.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account of the boot: %v", err)
	}
	if err := host.restoreEventifiedDeliveries(ctx); err != nil {
		t.Fatalf("restore the account the boot reads: %v", err)
	}
	if after := count(booted); after != before {
		t.Fatalf("the boot left %d settlement fact(s) beside the %d the process before it left, want a replay that settles nothing", after, before)
	}
	sender.mu.Lock()
	attempts := len(sender.attempts)
	sender.mu.Unlock()
	if attempts != 0 {
		t.Fatalf("the boot sent %d delivery attempt(s), want a rebuild that drives no observation", attempts)
	}
}

// foldRecorder is a consumer state slice a check counts the reducers of: it
// derives its state from the facts the seam folds and reports every attempt, so a
// check reads how many times one fact reached one consumer.
type foldRecorder struct {
	name string
	mu   sync.Mutex
	// folded counts the fold attempts per identity, and derived states what each
	// attempt applied, so a check compares the state one fact derived live with the
	// state the same fact derives beside a rebuild.
	folded  map[event.EventID]int
	derived map[event.EventID]string
}

// StateName reports the name the recorded slice is keyed by.
func (s *foldRecorder) StateName() string { return s.name }

// SnapshotState states what the folds derived, in the identity order the checkpoint
// renders the same bytes for the same facts.
func (s *foldRecorder) SnapshotState(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.derived))
	for key := range s.derived {
		keys = append(keys, string(key))
	}
	slices.Sort(keys)
	stated := make([]string, 0, len(keys))
	for _, key := range keys {
		stated = append(stated, key+"="+s.derived[event.EventID(key)])
	}
	return json.Marshal(stated)
}

// RestoreState puts one stated account back.
func (s *foldRecorder) RestoreState(_ context.Context, payload []byte) error {
	var stated []string
	if err := json.Unmarshal(payload, &stated); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.derived == nil {
		s.derived = map[event.EventID]string{}
	}
	for _, line := range stated {
		at := strings.IndexByte(line, '=')
		if at <= 0 {
			return fmt.Errorf("the recorded slice states no entry in %q", line)
		}
		if _, held := s.derived[event.EventID(line[:at])]; held {
			continue
		}
		s.derived[event.EventID(line[:at])] = line[at+1:]
	}
	return nil
}

// Reduce folds one fact into the recorded account and counts the attempt.
func (s *foldRecorder) Reduce(_ context.Context, evt event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.folded == nil {
		s.folded = map[event.EventID]int{}
	}
	if s.derived == nil {
		s.derived = map[event.EventID]string{}
	}
	s.folded[evt.Key]++
	s.derived[evt.Key] = evt.Type + ":" + evt.Message.Content
	return nil
}

// folds reports the fold attempts the recorder counted, per identity.
func (s *foldRecorder) folds() map[event.EventID]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[event.EventID]int, len(s.folded))
	for key, count := range s.folded {
		out[key] = count
	}
	return out
}

// derivedOf reports the state the recorder derived for one identity.
func (s *foldRecorder) derivedOf(key event.EventID) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, held := s.derived[key]
	return state, held
}

// recordingBoot mounts the named recorder the moment the run opens, so the
// restore and the rebuild the same boot runs reach it.
func recordingBoot(recorder *foldRecorder) Option {
	return withBootConfigReconcile(func(ctx context.Context, run *Run, decl *config.Declaration, agents map[event.AgentID]NativeAgent) error {
		if _, err := run.RegisterStateSlice(recorder); err != nil {
			return err
		}
		_, err := run.Publish(ctx, decl, agents)
		return err
	})
}

// projectionKeysOf reports the mapped sequence one chain presents, in display order.
func projectionKeysOf(t *testing.T, run *Run, addr event.Address) []event.EventID {
	t.Helper()
	entries, carried := run.ReplayEntries(addr.SessionID)
	if !carried {
		t.Fatalf("the chain %v presents no mapped sequence", addr)
	}
	keys := make([]event.EventID, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	return keys
}

// obligationLinesOf states the obligations an account owes as identity=state lines,
// in the order the account answers them.
func obligationLinesOf(obligations []deliverySettlement) []string {
	lines := make([]string, 0, len(obligations))
	for _, obligation := range obligations {
		lines = append(lines, obligation.identity+"="+obligation.state)
	}
	return lines
}

// TestReducerDerivesEqualAtRuntimeAndRecovery pins the one derivation path of the state seam.
//   - positive: one chain's projection, compression account and delivery obligations are entry-equal live and after a boot
//   - negative: a consumer that rebuilds its state through a reader of its own answers a different account, which the entry comparison of each of the three rejects
//   - consumer: the real boot entry (restoreCheckpointState → seedAnchoredReplay → seedFact → StateRegistry.Reduce)
//   - consumer: the live commit entry (commitFactNoCadence → StateRegistry.Reduce)
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestReducerDerivesEqualAtRuntimeAndRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: deliveryTestAgent, SessionID: "session-reducer-parity"}
	sourceAddr := event.Address{AgentID: deliveryTestAgent, SessionID: "session-reducer-parity-source"}
	source := windowChainRun(t, ctx, storage)
	source.stopCheckpointFiler()
	sourceHost := deliveryTestHost(ctx, source, nil)
	if err := sourceHost.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account of the live process: %v", err)
	}
	commitSessionTurns(t, ctx, source, addr, "reducer-parity", 10)
	if identity := declareObligationTargeting(t, ctx, source, addr, addr, "reducer-parity-owed", "the body the chain owes"); identity == "" {
		t.Fatal("the declaration seated no identity")
	}
	liveProjection := projectionKeysOf(t, source, addr)
	liveCompress := compressAccountOf(t, source.compress)
	liveObligations := obligationLinesOf(sourceHost.deliveryObligations())
	if len(liveProjection) == 0 || liveCompress.judgedCount() == 0 || len(liveObligations) == 0 {
		t.Fatalf("the fixture derived %d projection entries, %d judgments and %d obligations, want every consumer to have derived something",
			len(liveProjection), liveCompress.judgedCount(), len(liveObligations))
	}
	if _, _, err := source.CheckpointSession(ctx, addr); err != nil {
		t.Fatalf("file the checkpoint of the chain: %v", err)
	}
	declareObligationTargeting(t, ctx, source, sourceAddr, addr, "reducer-parity-after-cut", "the body the chain owes past the cut")
	liveObligations = obligationLinesOf(sourceHost.deliveryObligations())
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now()}); err != nil {
		t.Fatalf("close the live process: %v", err)
	}

	booted := windowChainRun(t, ctx, storage)
	booted.stopCheckpointFiler()
	bootedHost := deliveryTestHost(ctx, booted, nil)
	if err := bootedHost.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account of the boot: %v", err)
	}
	if _, err := bootedHost.replayDeliveryFacts(ctx); err != nil {
		t.Fatalf("fold the delivery facts past the cut: %v", err)
	}
	bootedProjection := projectionKeysOf(t, booted, addr)
	if !slices.Equal(bootedProjection, liveProjection) {
		t.Fatalf("the boot presents %d projection entries beside the %d the live process presents: %v against %v",
			len(bootedProjection), len(liveProjection), bootedProjection, liveProjection)
	}
	requireSameAccount(t, "booted", compressAccountOf(t, booted.compress), liveCompress)
	bootedObligations := obligationLinesOf(bootedHost.deliveryObligations())
	if !slices.Equal(bootedObligations, liveObligations) {
		t.Fatalf("the boot owes %v beside the %v the live process owes", bootedObligations, liveObligations)
	}
}

// TestSnapshotFlushesNothing pins the pure read of a cut.
//   - positive: the cut answers beside a holder that proves no lock of the run is held while its state is read
//   - positive: the cut leaves the ledger as it found it, so the checkpoint it files is the only write
//   - negative: a cut that read its holders under a lock of the run fails the holder's proof, and one that flushed state grows the ledger
//   - consumer: Run.Checkpoint, the cut the cadence and the close file through
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestSnapshotFlushesNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	run.stopCheckpointFiler()
	holder := &pureReadSlice{run: run}
	if _, err := run.RegisterStateSlice(holder); err != nil {
		t.Fatalf("register the pure-read holder: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-pure-read"}
	commitSessionTurns(t, ctx, run, addr, "pure-read", 3)

	before := run.CommittedFacts()
	key, filed, err := run.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut = (%t, %v), want a filed checkpoint", filed, err)
	}
	if holder.lockedHeld {
		t.Fatal("the cut read the holders beside a lock of the run held, want a read taken with no lock of the run")
	}
	if after := run.CommittedFacts(); after != before {
		t.Fatalf("the cut left %d committed fact(s) beside the %d before it, want the checkpoint on the store alone and nothing a holder flushed into the ledger",
			after, before)
	}
	if stated := checkpointOfIdentity(t, ctx, run, key); !slices.Contains(stated.CheckpointSliceNames(), holder.StateName()) {
		t.Fatalf("the checkpoint names the slices %v, want the pure-read holder's", stated.CheckpointSliceNames())
	}
}

// pureReadSlice is a state holder whose snapshot proves the read is pure: it takes
// the run lock, which the cut leaves free, and records whether the lock was held.
type pureReadSlice struct {
	run        *Run
	lockedHeld bool
}

// StateName reports the name the slice is keyed by.
func (s *pureReadSlice) StateName() string { return "assembly.pure-read" }

// SnapshotState states the slice and reports a run lock held beside the read.
func (s *pureReadSlice) SnapshotState(context.Context) ([]byte, error) {
	if !s.run.mu.TryLock() {
		s.lockedHeld = true
		return nil, errors.New("the cut read the holder beside the run lock")
	}
	s.run.mu.Unlock()
	return []byte("{}"), nil
}

// RestoreState puts the stated slice back.
func (s *pureReadSlice) RestoreState(context.Context, []byte) error { return nil }

// TestSeedFactReusesTheReducer pins that a rebuild folds its facts through the reducer the live commit drives.
//   - positive: every fact the boot folds is a fact the live process folded, and it derives the same state from it
//   - negative: a fact the rebuild reaches that the live process never folded, and one whose derived state differs from the live state, both fail
//   - consumer: seedFact (assembly/run.go) over StateRegistry.Reduce, driven by the real boot entry
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestSeedFactReusesTheReducer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-seed-reducer"}
	live := &foldRecorder{name: "assembly.seed-reducer"}
	source := windowChainRun(t, ctx, storage, recordingBoot(live))
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, "seed-reducer", 6)
	if liveFolds := live.folds(); len(liveFolds) == 0 {
		t.Fatal("the live commit folded nothing, so the reuse proves nothing")
	}
	if _, _, err := source.CheckpointSession(ctx, addr); err != nil {
		t.Fatalf("file the checkpoint of the chain: %v", err)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the live process: %v", err)
	}

	booted := &foldRecorder{name: "assembly.seed-reducer"}
	rebuilt := windowChainRun(t, ctx, storage, recordingBoot(booted))
	rebuilt.stopCheckpointFiler()
	bootFolds := booted.folds()
	if len(bootFolds) == 0 {
		t.Fatal("the boot folded no fact through the recorder, want the reducer the live commit drives")
	}
	for key := range bootFolds {
		if _, folded := live.folds()[key]; !folded {
			t.Fatalf("the boot folded the fact %s the live process never folded", key)
		}
		bootedState, _ := booted.derivedOf(key)
		liveState, _ := live.derivedOf(key)
		if bootedState != liveState {
			t.Fatalf("the fact %s derived %q beside the boot and %q beside the live process, want one derivation", key, bootedState, liveState)
		}
	}
}

// TestRecoveryDerivesNoSecondTime pins that the rebuild folds each fact once.
//   - positive: each fact the boot replays reaches the reducer exactly once
//   - negative: a second derivation of the same fact beside the rebuild fails the count, which is what a bespoke recovery reader produces
//   - negative: the fold of the delivery account sends nothing, so the observation duty is not driven inside the recovery
//   - consumer: seedFact over StateRegistry.Reduce, and the delivery fold replayDeliveryFacts
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestRecoveryDerivesNoSecondTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: deliveryTestAgent, SessionID: "session-single-derivation"}
	live := &foldRecorder{name: "assembly.single-derivation"}
	source := windowChainRun(t, ctx, storage, recordingBoot(live))
	source.stopCheckpointFiler()
	sourceHost := deliveryTestHost(ctx, source, newDeliveryTestSender(nil))
	if err := sourceHost.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account of the live process: %v", err)
	}
	commitSessionTurns(t, ctx, source, addr, "single-derivation", 8)
	declareObligationTargeting(t, ctx, source, addr, addr, "single-derivation-owed", "the body the chain owes")
	if _, _, err := source.CheckpointSession(ctx, addr); err != nil {
		t.Fatalf("file the checkpoint of the chain: %v", err)
	}
	deliveryTestStopRetries(sourceHost)
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the live process: %v", err)
	}

	booted := &foldRecorder{name: "assembly.single-derivation"}
	rebuilt := windowChainRun(t, ctx, storage, recordingBoot(booted))
	rebuilt.stopCheckpointFiler()
	bootFolds := booted.folds()
	if len(bootFolds) == 0 {
		t.Fatal("the boot folded no fact, so the count proves nothing")
	}
	for key, count := range bootFolds {
		if count != 1 {
			t.Fatalf("the boot folded the fact %s %d time(s), want one derivation per fact", key, count)
		}
	}

	sender := newDeliveryTestSender(nil)
	bootedHost := deliveryTestHost(ctx, rebuilt, sender)
	if err := bootedHost.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account of the boot: %v", err)
	}
	if _, err := bootedHost.replayDeliveryFacts(ctx); err != nil {
		t.Fatalf("fold the delivery facts past the cut: %v", err)
	}
	defer deliveryTestStopRetries(bootedHost)
	sender.mu.Lock()
	attempts := len(sender.attempts)
	sender.mu.Unlock()
	if attempts != 0 {
		t.Fatalf("the fold sent %d delivery attempt(s), want the account seated without driving the duty", attempts)
	}
}

// TestNewConsumerRegistersSliceOnly pins that a slice is all a consumer mounts.
//   - positive: a consumer registered through Run.RegisterStateSlice alone is stated by the cut and handed back by the boot
//   - positive: the state it reads after the boot is the state the checkpoint and the tail derive
//   - negative: a consumer the framework named a hook for would need wiring beside the registration, which this consumer has none of
//   - consumer: Run.RegisterStateSlice at boot, then the real boot entry that restores the checkpoint and replays the tail into it
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestNewConsumerRegistersSliceOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-new-consumer"}
	live := &foldRecorder{name: "assembly.new-consumer"}
	source := windowChainRun(t, ctx, storage, recordingBoot(live))
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, "new-consumer", 4)
	if !slices.Contains(source.StateRegistry().Names(), live.StateName()) {
		t.Fatalf("the registry names %v, want the registered slice among them", source.StateRegistry().Names())
	}
	if len(live.folds()) == 0 {
		t.Fatal("the registered consumer folded nothing, so the cut states nothing about it")
	}
	key, filed, err := source.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the checkpoint of the run = (%t, %v), want a filed fact", filed, err)
	}
	if !slices.Contains(checkpointOfIdentity(t, ctx, source, key).CheckpointSliceNames(), live.StateName()) {
		t.Fatalf("the checkpoint names the slices %v, want the registered consumer's", checkpointOfIdentity(t, ctx, source, key).CheckpointSliceNames())
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the live process: %v", err)
	}

	booted := &foldRecorder{name: "assembly.new-consumer"}
	rebuilt := windowChainRun(t, ctx, storage, recordingBoot(booted))
	rebuilt.stopCheckpointFiler()
	if len(booted.folds()) == 0 {
		t.Fatal("the boot restored and folded nothing into the registered slice, want the checkpoint and the tail it states")
	}
	bootedState, err := booted.SnapshotState(ctx)
	if err != nil {
		t.Fatalf("state the restored slice: %v", err)
	}
	if len(string(bootedState)) <= len("null") {
		t.Fatalf("the restored slice states %q, want the entries the checkpoint and the tail derive", bootedState)
	}
}

// TestNoPerConsumerDispatchPath pins that the seam dispatches by declaration.
//   - positive: two consumers registered under different names, one of them a name the framework itself uses, fold exactly the same facts
//   - negative: a dispatch that routed a fact by the consumer's name would answer different sets for the two names
//   - consumer: StateRegistry.Reduce over the live commit entry and the rebuild entry, which name no consumer
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestNoPerConsumerDispatchPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	run.stopCheckpointFiler()
	named := &foldRecorder{name: checkpointSliceDelivery}
	free := &foldRecorder{name: "assembly.no-dispatch"}
	for _, recorder := range []*foldRecorder{named, free} {
		if _, err := run.RegisterStateSlice(recorder); err != nil {
			t.Fatalf("register the %q slice: %v", recorder.StateName(), err)
		}
	}
	addr := event.Address{AgentID: deliveryTestAgent, SessionID: "session-no-dispatch"}
	commitSessionTurns(t, ctx, run, addr, "no-dispatch", 3)
	declareObligationTargeting(t, ctx, run, addr, addr, "no-dispatch-owed", "the body of the no-dispatch chain")
	if err := run.fileCompressRequest(ctx, addr.SessionID, event.CompressRequest{
		ID:     "no-dispatch-judgment",
		Reason: "the material of the no-dispatch chain",
		At:     checkpointBase(),
	}); err != nil {
		t.Fatalf("file the judgment of the no-dispatch chain: %v", err)
	}
	if _, err := run.CommitToSession(ctx, addr, event.Event{
		Key:       deliverySettlementKey("no-dispatch-owed", deliveryStateSent),
		Type:      deliverySettlementType,
		Timestamp: checkpointBase(),
		Message:   model.Message{Role: model.RoleUser, Content: "no-dispatch settlement"},
		Meta: map[string]string{
			event.MetaKeyEventSummary: deliverySettlementSummary,
			deliveryMetaIdentity:      "no-dispatch-owed",
			deliveryMetaState:         deliveryStateSent,
			deliveryMetaTargetAgent:   string(addr.AgentID),
			deliveryMetaTargetSession: string(addr.SessionID),
		},
		Owner: addr, Source: addr, Target: addr,
	}); err != nil {
		t.Fatalf("commit the settlement of the no-dispatch chain: %v", err)
	}
	namedFolds := named.folds()
	freeFolds := free.folds()
	if len(namedFolds) == 0 {
		t.Fatal("the named consumer folded nothing, so the dispatch comparison proves nothing")
	}
	if len(namedFolds) != len(freeFolds) {
		t.Fatalf("the %q consumer folded %d fact(s) and the %q consumer folded %d, want the same set from the same facts",
			named.StateName(), len(namedFolds), free.StateName(), len(freeFolds))
	}
	for key := range namedFolds {
		if _, folded := freeFolds[key]; !folded {
			t.Fatalf("the %q consumer folded the fact %s and the %q consumer did not, so the dispatch read a consumer name",
				named.StateName(), key, free.StateName())
		}
	}
}

// seatTailFixture is one durable root whose framework checkpoint sits under a later
// session checkpoint, beside the tasks the fixture settled on each side of it.
type seatTailFixture struct {
	// tier is the live view the anchors walk, which is the view a boot opens over the root.
	tier *ledger.Tier
	// covered are the tasks the framework checkpoint states.
	covered []event.TaskID
	// behind are the tasks that landed past the framework checkpoint and before the session checkpoint filed over them.
	behind []event.TaskID
	// beyond are the tasks that landed past that session checkpoint.
	beyond []event.TaskID
}

// seatTailRun opens one durable run whose checkpoint filer is stopped, so every
// checkpoint the fixture reads was filed by the fixture itself.
func seatTailRun(t *testing.T, ctx context.Context, root string) *Run {
	t.Helper()
	run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root}))
	run.stopCheckpointFiler()
	t.Cleanup(func() {
		if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
			t.Logf("close the seat tail run: %v", err)
		}
	})
	return run
}

// seatTailRoots builds the durable root the anchors share: two session chains, a
// framework checkpoint, the tasks that landed before it, a session checkpoint filed
// after those tasks, and the tasks that landed past it.
//
// The order is the whole point of the fixture. The tasks between the framework
// checkpoint and the session checkpoint are the ones a walk ending at anything
// spelled like a checkpoint would drop, so a fixture that filed no session checkpoint
// could not tell a framework stop from a spelling stop. The walk is read on the live
// view, because closing a run files a framework checkpoint of its own and that cut
// covers every task the fixture settled.
func seatTailRoots(t *testing.T, ctx context.Context) *seatTailFixture {
	t.Helper()
	run := seatTailRun(t, ctx, t.TempDir())
	first := deliveryTestAddress("session-seat-tail-first")
	second := deliveryTestAddress("session-seat-tail-second")
	commitSessionTurns(t, ctx, run, first, "seat-tail-first", 2)
	commitSessionTurns(t, ctx, run, second, "seat-tail-second", 2)
	covered := settleTasks(t, ctx, run, taskCheckpointCalls("covered", 2))
	if _, filed, err := run.Checkpoint(ctx); err != nil || !filed {
		t.Fatalf("file the framework checkpoint of the seat tail fixture = (%t, %v), want a filed fact", filed, err)
	}
	behind := settleTasks(t, ctx, run, taskCheckpointCalls("behind", 2))
	if _, filed, err := run.CheckpointSession(ctx, first); err != nil || !filed {
		t.Fatalf("file the session checkpoint behind the seat tail = (%t, %v), want a filed fact", filed, err)
	}
	beyond := settleTasks(t, ctx, run, taskCheckpointCalls("beyond", 2))
	if _, filed, err := run.CheckpointSession(ctx, second); err != nil || !filed {
		t.Fatalf("file the session checkpoint past the seat tail = (%t, %v), want a filed fact", filed, err)
	}
	return &seatTailFixture{tier: run.tier, covered: covered, behind: behind, beyond: beyond}
}

// seatTailNames answers which of the named tasks one tail states, so an anchor
// compares the set a walk named against the tasks the fixture settled.
func seatTailNames(tail map[string]struct{}, ids []event.TaskID) map[event.TaskID]bool {
	named := make(map[event.TaskID]bool, len(ids))
	for _, id := range ids {
		for key := range tail {
			if strings.Contains(key, string(id)) {
				named[id] = true
				break
			}
		}
	}
	return named
}

// TestSeatTailNamesEveryTaskYoungerThanTheNewestFrameworkCheckpoint pins the walk the task account is seated from.
//   - positive: every task settled past the newest framework checkpoint is named, both behind the session checkpoint over it and past that checkpoint
//   - negative: no task the checkpoint states is named, so the tail is the chains past the cut and never the history behind it
//   - the fixture files a session checkpoint after those tasks, so a walk ending at the checkpoint type names none of them
//   - a tail of the empty set is still non-nil, because nil is the answer of a boot that reads the whole log
//   - consumer: tailFactKeys, the source of the tail Run.seatDurableAccount hands to DurableCommit.Seat
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestSeatTailNamesEveryTaskYoungerThanTheNewestFrameworkCheckpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	fixture := seatTailRoots(t, ctx)
	tail, err := tailFactKeys(ctx, fixture.tier)
	if err != nil {
		t.Fatalf("walk the commit order of the seat tail fixture: %v", err)
	}
	if tail == nil {
		t.Fatal("the walk of a root holding a framework checkpoint answered nil, want the tail its account is seated from: nil states no cut and sends the boot to the whole log")
	}
	younger := append(append([]event.TaskID{}, fixture.behind...), fixture.beyond...)
	for _, id := range younger {
		if !seatTailNames(tail, []event.TaskID{id})[id] {
			t.Fatalf("the tail names no key of the task %s that landed past the framework checkpoint, want every younger chain named beside the %d key(s) it does name",
				id, len(tail))
		}
	}
	for _, id := range fixture.covered {
		if seatTailNames(tail, []event.TaskID{id})[id] {
			t.Fatalf("the tail names the task %s the framework checkpoint states, want the chains past that checkpoint alone", id)
		}
	}
}

// TestASpellingBasedStopDropsTasksBehindASessionCheckpoint is the negative anchor of that walk.
//   - the fixture files a session checkpoint after the tasks that landed past the framework checkpoint
//   - a walk ending at that session checkpoint names none of them, and the boot seats the account as if they never committed
//   - positive: the tail names every task behind that session checkpoint, so the stop is the framework checkpoint and not the type
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestASpellingBasedStopDropsTasksBehindASessionCheckpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	fixture := seatTailRoots(t, ctx)
	if len(fixture.behind) == 0 {
		t.Fatal("the fixture settled no task between the framework checkpoint and the session checkpoint, so a spelling stop would drop nothing and this anchor would prove nothing")
	}
	tail, err := tailFactKeys(ctx, fixture.tier)
	if err != nil {
		t.Fatalf("walk the commit order of the seat tail fixture: %v", err)
	}
	for _, id := range fixture.behind {
		if !seatTailNames(tail, []event.TaskID{id})[id] {
			t.Fatalf("the tail names no key of the task %s that landed behind the session checkpoint filed after it, want it named: a walk ending at a checkpoint spelled like the framework one drops it and the boot seats its account without it", id)
		}
	}
}

// TestAnEmptyNonNilTailMustNotSeatZeroYoungTasks pins the three answers the walk owes a boot.
//   - a root whose checkpoint covers every task answers a tail naming none of them, and answers it non-nil
//   - a root holding no framework checkpoint answers nil, so the seat reads the whole log instead of stating an account
//   - a root holding tasks past its checkpoint answers a tail naming them, so a tail naming none never stands beside a younger chain
//   - a nil answer costs the boot every durable prefix key, which is the red this walk removes
//   - the set a cut leaves also carries the keys its own commit landed, so the anchors judge the task face and never the length
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestAnEmptyNonNilTailMustNotSeatZeroYoungTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	seatTailRoot := func(t *testing.T, checkpoint bool, past []string) (*ledger.Tier, []event.TaskID, []event.TaskID) {
		t.Helper()
		run := seatTailRun(t, ctx, t.TempDir())
		commitSessionTurns(t, ctx, run, deliveryTestAddress("session-seat-tail-empty"), "seat-tail-empty", 2)
		covered := settleTasks(t, ctx, run, taskCheckpointCalls("covered", 2))
		if checkpoint {
			if _, filed, err := run.Checkpoint(ctx); err != nil || !filed {
				t.Fatalf("file the framework checkpoint of the tail root = (%t, %v), want a filed fact", filed, err)
			}
		}
		young := []event.TaskID{}
		if len(past) > 0 {
			young = settleTasks(t, ctx, run, past)
		}
		return run.tier, covered, young
	}

	coveredTail := func(t *testing.T, tier *ledger.Tier) map[string]struct{} {
		t.Helper()
		tail, err := tailFactKeys(ctx, tier)
		if err != nil {
			t.Fatalf("walk the commit order of the tail root: %v", err)
		}
		if tail == nil {
			t.Fatal("the walk of a root holding a framework checkpoint answered nil, want the tail its account is seated from: nil is a boot that reads every durable prefix key")
		}
		return tail
	}

	latest, latestTasks, _ := seatTailRoot(t, true, nil)
	for id := range seatTailNames(coveredTail(t, latest), latestTasks) {
		t.Fatalf("the tail of a root whose framework checkpoint covers every task names the task %s, want the chains past the cut alone", id)
	}

	uncut, _, _ := seatTailRoot(t, false, nil)
	walked, err := tailFactKeys(ctx, uncut)
	if err != nil {
		t.Fatalf("walk the commit order of the root holding no framework checkpoint: %v", err)
	}
	if walked != nil {
		t.Fatalf("the walk of a root holding no framework checkpoint answered %d key(s), want nil: nil sends the seat to the whole log, and a tail of none would state an account that finished reading", len(walked))
	}

	young, coveredTasks, youngTasks := seatTailRoot(t, true, taskCheckpointCalls("young", 2))
	tail := coveredTail(t, young)
	if len(tail) == 0 {
		t.Fatal("the tail of a root holding tasks past its checkpoint names no key, want those chains named: a tail of none beside a younger chain seats the account as if the chain never committed")
	}
	named := seatTailNames(tail, youngTasks)
	for _, id := range youngTasks {
		if !named[id] {
			t.Fatalf("the tail names no key of the task %s settled past the checkpoint, want every younger chain named: a tail of none never states the account it covers", id)
		}
	}
	for _, id := range coveredTasks {
		if seatTailNames(tail, []event.TaskID{id})[id] {
			t.Fatalf("the tail names the task %s the checkpoint states, want the chains past that checkpoint alone", id)
		}
	}
}

// TestBootSeatsTheMailboxAfterTheDurableAccount pins the order one boot states the durable account in.
//   - the account is seated before the slices are restored, so a notification that landed past the checkpoint's cut is seated by the mailbox of the same boot
//   - a boot that restored the slices first would wait for a checkpoint that never states that notification
//   - the fixture files a checkpoint, then commits one task record whose outbox the stopped process never delivered
//   - the record is past the cut, so the tasks slice does not cover it and the boot must read the chain that landed after it
//   - that record is the increment a boot reads by identity: the run that wrote it stopped before any later checkpoint could cover it
//   - positive: the booted mailbox seats that notification, routed to the address the record froze, and the durable load reached it by the identity of its task
//   - negative of the order: an account seated after the mailbox restore seats nothing there, which is the work item a restart silently loses
//
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func TestBootSeatsTheMailboxAfterTheDurableAccount(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	addr := deliveryTestAddress("session-seat-after-cut")
	source := noTrimChainRun(t, ctx, storage)
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, "seat", 2)
	if _, _, err := source.Checkpoint(ctx); err != nil {
		t.Fatalf("file the checkpoint of the account: %v", err)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	owedAddr := event.Address{AgentID: "agent-a", SessionID: "session-seat-after-cut-tail"}
	owedTask := event.TaskID("task-seat-after-cut")
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the storage tier beside the cut: %v", err)
	}
	account := state.NewEmptyDurableCommit(ledger.TierFactLog(tier), nil)
	if err := account.Seat(ctx, nil, nil); err != nil {
		t.Fatalf("seat the account of the storage: %v", err)
	}
	if _, err := account.Commit(ctx, task.CommitRecord{
		MutationID: "seat-after-cut:0",
		TaskID:     owedTask,
		Status:     task.StatusRunning,
		Outbox: []task.Envelope{{
			Notice: task.Notice{
				TaskID: owedTask, Round: 1, Kind: task.NoticeProgress, Seq: 1,
				Event: "fact-seat-after-cut", Text: "the progress the stopped process never delivered",
			},
			Target:         owedAddr,
			Source:         owedAddr,
			TargetInstance: "instance-seat-after-cut",
		}},
	}); err != nil {
		t.Fatalf("commit the owed notification past the cut: %v", err)
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("release the storage tier beside the cut: %v", err)
	}

	booted := noTrimChainRun(t, ctx, storage)
	booted.stopCheckpointFiler()
	if key, _ := booted.CheckpointFiled(); key == "" {
		t.Fatal("the boot read no checkpoint, want the cut this account is restored beside")
	}
	seats := booted.store.Unconsumed()
	held := false
	for _, seat := range seats {
		if seat.Notice.TaskID == owedTask {
			held = true
			if seat.Address != owedAddr {
				t.Fatalf("the restored seat routes to %v, want the frozen routing %v", seat.Address, owedAddr)
			}
		}
	}
	if !held {
		t.Fatalf("the booted mailbox seats %+v, want the notification that landed past the cut", seats)
	}
	scanned := false
	for _, scan := range booted.commit.inner.(*state.DurableCommit).LoadStats().Scans {
		if scan.Prefix == "tagent/task/v1/fact:tagent/" {
			t.Fatal("the boot scanned the fact family of the whole history, want the tasks slice beside the chain that landed after it: the account was seated from that slice, so no prefix of the whole history costs a key")
		}
		if strings.Contains(scan.Prefix, string(owedTask)) {
			scanned = true
		}
	}
	if !scanned {
		t.Fatal("the boot read no key of the task that landed past the cut, want its chain read by identity")
	}
}

// tailWalkTier opens a journal store holding the covered facts of one partition
// beside the facts a cut leaves past it, and answers the store beside the keys
// those tail facts were committed under.
func tailWalkTier(t *testing.T, ctx context.Context, covered, tail, beside int) (*ledger.Tier, []event.EventID) {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir(), WriteRights: true})
	if err != nil {
		t.Fatalf("open the journal store: %v", err)
	}
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	seq := map[int]int64{}
	commit := func(partition int, session string, typ string, key event.EventID) {
		t.Helper()
		seq[partition]++
		stamp := at.Add(time.Duration(seq[partition]) * time.Millisecond)
		evt := event.Event{
			Key:       key,
			Type:      typ,
			Timestamp: stamp,
			Meta:      map[string]string{event.MetaKeyEventSummary: "ask"},
			Message:   model.Message{Role: model.RoleUser, Content: "tail"},
			Owner:     event.Address{AgentID: event.AgentID(fmt.Sprintf("agent-%d", partition)), SessionID: event.SessionID(session)},
		}
		body, err := memory.BodyFromEvent(evt, "test", "test")
		if err != nil {
			t.Fatalf("encode %s: %v", key, err)
		}
		encoded, err := body.Encode()
		if err != nil {
			t.Fatalf("marshal %s: %v", key, err)
		}
		record := ledger.SegmentRecord{PartitionID: partition, WindowTS: stamp.UnixMilli(), Seq: seq[partition], EventKey: key, EventTime: stamp.UnixMilli(), Body: encoded}
		if err := tier.Segments.StoreEvent(ctx, record.WithChain(typ, event.SessionID(session))); err != nil {
			t.Fatalf("store %s: %v", key, err)
		}
	}
	for i := 1; i <= covered; i++ {
		commit(10, "session-covered", event.TypeExternalInput, event.EventID(fmt.Sprintf("covered-%d", i)))
	}
	later := make([]event.EventID, 0, tail)
	for i := 1; i <= tail; i++ {
		key := event.EventID(fmt.Sprintf("tail-%d", i))
		commit(10, "session-tail", event.TypeExternalInput, key)
		later = append(later, key)
		if i == 1 {
			commit(10, "", event.TypeCheckpoint, "tail-checkpoint")
		}
		if i <= beside {
			key := event.EventID(fmt.Sprintf("beside-%d", i))
			commit(11, "session-beside", event.TypeExternalInput, key)
			later = append(later, key)
		}
	}
	return tier, later
}

// TestTailWalkStopsAtTheWatermarkAndTheTailEnd pins what the walk of the facts past a cut reads.
//   - positive: a store holding 600 covered facts beside six tail facts answers those six, naming them beside the one entry the cut stands at
//   - the checkpoint the tail region holds stays out of the answer, because a restore reads it as the cut it is instead of a fact to fold
//   - negative: a fact of a partition the cut does not name leaves the stop unable to account for the walk, so the whole walk answers the same facts
//   - consumer: Host.replayDeliveryFacts folds the delivery account through this walk, so a boot of a long-lived store costs the obligations in flight
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestTailWalkStopsAtTheWatermarkAndTheTailEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	const covered, tail = 600, 6
	tier, later := tailWalkTier(t, ctx, covered, tail, 0)
	defer func() {
		if err := tier.Segments.Close(); err != nil {
			t.Fatalf("close the store: %v", err)
		}
	}()
	var seen []event.EventID
	before, _, _ := tier.Segments.IndexWalkReads()
	if err := (&Run{}).eachFactPastCut(ctx, tier, event.CheckpointOffset{10: covered}, func(stored ledger.StoredEvent) error {
		seen = append(seen, stored.Key)
		return nil
	}); err != nil {
		t.Fatalf("walk the facts past the cut: %v", err)
	}
	after, _, _ := tier.Segments.IndexWalkReads()
	named := after - before
	t.Logf("covered=%d tail=%d walk-named=%d visited=%d", covered, tail, named, len(seen))
	if !slices.Equal(seen, later) {
		t.Fatalf("the walk answered %v, want the tail the cut leaves %v", seen, later)
	}
	if named > len(later)+2 {
		t.Fatalf("the walk named %d index entries beside %d tail facts and %d covered ones: the walk reads the history behind the cut", named, len(later), covered)
	}
	beside, besideKeys := tailWalkTier(t, ctx, covered, tail, 2)
	defer func() {
		if err := beside.Segments.Close(); err != nil {
			t.Fatalf("close the store beside the unnamed partition: %v", err)
		}
	}()
	var wide []event.EventID
	if err := (&Run{}).eachFactPastCut(ctx, beside, event.CheckpointOffset{10: covered}, func(stored ledger.StoredEvent) error {
		wide = append(wide, stored.Key)
		return nil
	}); err != nil {
		t.Fatalf("walk the facts past the cut beside an unnamed partition: %v", err)
	}
	if !slices.Equal(wide, besideKeys) {
		t.Fatalf("the walk beside an unnamed partition answered %v, want the facts past the cut %v: the stop cannot answer for a partition the cut does not name", wide, besideKeys)
	}
}

// refusingConsumer is a state slice whose holder refuses to apply one named fact.
type refusingConsumer struct {
	*foldRecorder
	refuse  event.EventID
	mu      sync.Mutex
	applies bool
	refused []event.EventID
}

// Reduce refuses the one named fact until the refusal is lifted, and folds every other through the recorder.
func (c *refusingConsumer) Reduce(ctx context.Context, evt event.Event) error {
	c.mu.Lock()
	refused := evt.Key == c.refuse && !c.applies
	if refused {
		c.refused = append(c.refused, evt.Key)
	}
	c.mu.Unlock()
	if refused {
		return fmt.Errorf("the consumer refuses to apply the fact %s", evt.Key)
	}
	return c.foldRecorder.Reduce(ctx, evt)
}

// allow lifts the refusal, so a later fold of the same identity applies it.
func (c *refusingConsumer) allow() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.applies = true
}

// refusals reports the identities this consumer refused, in refusal order.
func (c *refusingConsumer) refusals() []event.EventID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]event.EventID(nil), c.refused...)
}

// refusingBoot mounts the consumers the moment the run opens, so the restore and
// the rebuild the same boot runs reach them.
func refusingBoot(consumers ...state.Stateful) Option {
	return withBootConfigReconcile(func(ctx context.Context, run *Run, decl *config.Declaration, agents map[event.AgentID]NativeAgent) error {
		for _, consumer := range consumers {
			if _, err := run.RegisterStateSlice(consumer); err != nil {
				return err
			}
		}
		_, err := run.Publish(ctx, decl, agents)
		return err
	})
}

// TestRetryLeavesPresentedRangeAtTheLatestRound keeps successful consumers unchanged when an older fact is retried.
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRetryLeavesPresentedRangeAtTheLatestRound(t *testing.T) {
	for _, refuses := range []bool{false, true} {
		t.Run(fmt.Sprintf("refuses=%t", refuses), func(t *testing.T) {
			ctx := context.Background()
			addr := event.Address{AgentID: "agent-a", SessionID: "retry-presented"}
			consumer := &refusingConsumer{foldRecorder: &foldRecorder{name: "retry-presented"}}
			if refuses {
				consumer.refuse = "retry-presented-0-3"
			}
			run := wideWindowChainRun(t, ctx, refusingBoot(consumer))
			run.stopCheckpointFiler()
			commitSessionTurns(t, ctx, run, addr, "retry-presented", 2)
			before := run.SessionPresented(addr)
			if !before["retry-presented-1-3"] {
				t.Fatalf("the second round is not presented: %v", before)
			}
			fact, err := run.CommittedEvent(ctx, "retry-presented-0-3")
			if err != nil {
				t.Fatal(err)
			}
			consumer.allow()
			if _, err := run.CommitToSession(ctx, addr, fact); err != nil {
				t.Fatal(err)
			}
			if after := run.SessionPresented(addr); !reflect.DeepEqual(after, before) {
				t.Fatalf("retry moved the presented range from %v to %v, want the latest completed round unchanged", before, after)
			}
			if _, held := consumer.derivedOf(fact.Key); !held {
				t.Fatalf("the retried fact %s remains unapplied", fact.Key)
			}
		})
	}
}

// sessionBehalf states what its consumer folded in one session chain, so a cut of
// that chain reads the state of the chain that asked for it and never of the chain
// beside it.
type sessionBehalf struct {
	*refusingConsumer
	stateMu   sync.Mutex
	bySession map[event.SessionID][]event.EventID
}

func (s *sessionBehalf) Reduce(ctx context.Context, evt event.Event) error {
	if err := s.refusingConsumer.Reduce(ctx, evt); err != nil {
		return err
	}
	session := sessionOf(evt)
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.bySession == nil {
		s.bySession = map[event.SessionID][]event.EventID{}
	}
	if !slices.Contains(s.bySession[session], evt.Key) {
		s.bySession[session] = append(s.bySession[session], evt.Key)
	}
	return nil
}

// SnapshotSessionState states the identities this consumer folded in one chain, in
// the identity order two reads of the same state render alike.
func (s *sessionBehalf) SnapshotSessionState(_ context.Context, session event.SessionID) ([]byte, error) {
	s.stateMu.Lock()
	keys := append([]event.EventID(nil), s.bySession[session]...)
	s.stateMu.Unlock()
	if len(keys) == 0 {
		return nil, nil
	}
	stated := make([]string, 0, len(keys))
	for _, key := range keys {
		stated = append(stated, string(key))
	}
	slices.Sort(stated)
	return json.Marshal(stated)
}

// wideWindowChainRun assembles a run whose replay window never folds, so the
// facts a restart replays are decided by the cut it restored rather than by a
// compression product standing in their place.
func wideWindowChainRun(t *testing.T, ctx context.Context, opts ...Option) *Run {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	options := append([]Option{
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}),
	}, opts...)
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 100000}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}}, options...)
	if err != nil {
		t.Fatalf("assemble the wide-window run: %v", err)
	}
	return run
}

// retryFactOf builds the delivery retry attempt the projection never carries: the
// fact a cut may stand past without any replay path, because the fold that rebuilds
// a chain states delivery accounting nowhere.
func retryFactOf(identity string, addr event.Address) event.Event {
	return event.Event{
		Key:       deliverySettlementKey(identity, deliveryRetryState(1)),
		Type:      deliverySettlementType,
		Timestamp: checkpointBase().Add(45 * time.Minute),
		Message:   model.Message{Role: model.RoleUser, Content: "delivery retry 1: " + identity + " (12 bytes)"},
		Meta: map[string]string{
			event.MetaKeyEventSummary:  deliverySettlementSummary,
			event.MetaKeyTriggerSource: "host",
			deliveryMetaIdentity:       identity,
			deliveryMetaState:          deliveryRetryState(1),
			deliveryMetaTargetAgent:    string(addr.AgentID),
			deliveryMetaTargetSession:  string(addr.SessionID),
		},
		Owner: addr, Source: addr, Target: addr,
	}
}

// TestConsumerBehindTheChainSurvivesARestart is symptom anchor S4: a cut over a fact no consumer applied is refused, and a restart resumes from the cut that landed.
//   - fixture: one consumer refuses one committed fact the projection never carries, after an earlier cut landed, and the process restarts
//   - positive: the cut over it is refused by name, the earlier cut stays the newest, and the restarted consumer applies the fact it refused
//   - negative: a cut that covers a fact no consumer applied leaves that fact with no replay path, which the boot's derived set rejects
//   - consumer: the real boot entry (restoreCheckpointState → seedAnchoredReplay → StateRegistry.Reduce) and the live commit entry
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestConsumerBehindTheChainSurvivesARestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-consumer-behind"}
	// A delivery retry attempt is the fact the projection never carries: the fold
	// that rebuilt the chain states it nowhere, so a cut standing past it leaves it
	// with no replay path at all.
	refusedFact := retryFactOf("notice-behind", addr)
	refusedKey := refusedFact.Key
	postCutKey := event.EventID("behind-1-0")
	const sliceName = "assembly.consumer-behind"

	live := &refusingConsumer{foldRecorder: &foldRecorder{name: sliceName}, refuse: refusedKey}
	source := wideWindowChainRun(t, ctx, storage, refusingBoot(live))
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, "behind-early", 1)
	landed, filed, err := source.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the checkpoint before the refusal = (%t, %v), want the cut the restart resumes from", filed, err)
	}
	if _, err := source.CommitToSession(ctx, addr, refusedFact); err != nil {
		t.Fatalf("commit the fact the consumer refuses: %v", err)
	}
	commitSessionTurns(t, ctx, source, addr, "behind", 2)

	if got := live.refusals(); len(got) != 1 || got[0] != refusedKey {
		t.Fatalf("the consumer refused %v, want exactly the one named fact %s (the refusal path never ran, so the fixture proves nothing)", got, refusedKey)
	}
	if _, err := source.CommittedEvent(ctx, refusedKey); err != nil {
		t.Fatalf("read the refused fact %s back: %v, want it committed on the chain", refusedKey, err)
	}
	if _, held := live.derivedOf(refusedKey); held {
		t.Fatalf("the live consumer applied %s, want the refused fact unapplied", refusedKey)
	}
	if len(live.folds()) < 2 {
		t.Fatalf("the live consumer folded %d fact(s), want the facts beside the refused one", len(live.folds()))
	}

	records := source.tier.Segments.EventCount()
	refusedCut, covered, cutErr := source.Checkpoint(ctx)
	if cutErr == nil || covered {
		t.Fatalf("file the checkpoint over the unapplied fact = (%q, %t), want the cut refused by name", refusedCut, covered)
	}
	if !strings.Contains(cutErr.Error(), string(refusedKey)) || !strings.Contains(cutErr.Error(), sliceName) {
		t.Fatalf("the refused cut states %q, want it to name the unapplied fact %s and the slice %q", cutErr, refusedKey, sliceName)
	}
	if after := source.tier.Segments.EventCount(); after != records {
		t.Fatalf("the refused cut left %d record(s) beside the %d the account stood at, want a cut that files nothing", after, records)
	}
	if kept, _ := source.CheckpointFiled(); kept != landed {
		t.Fatalf("the newest cut is %q, want the cut that landed before the refusal %q", kept, landed)
	}

	_, closeErr := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(10 * time.Second)})
	if closeErr != nil && (!strings.Contains(closeErr.Error(), string(refusedKey)) || !strings.Contains(closeErr.Error(), sliceName)) {
		t.Fatalf("close the live process = %v, want the owed cut refused by the name of %s and the slice %q", closeErr, refusedKey, sliceName)
	}

	booted := &foldRecorder{name: sliceName}
	rebuilt := wideWindowChainRun(t, ctx, storage, recordingBoot(booted))
	rebuilt.stopCheckpointFiler()
	if kept, _ := rebuilt.CheckpointFiled(); kept != landed {
		t.Fatalf("the boot restored the cut %q, want the cut that landed before the refusal %q", kept, landed)
	}
	bootFolds := booted.folds()
	if len(bootFolds) == 0 {
		t.Fatal("the boot folded no fact, so the derived set proves nothing")
	}
	if _, replayed := bootFolds[postCutKey]; !replayed {
		t.Fatalf("the boot replayed %d fact(s) but not %s, want the tail past the cut replayed (the replay path never ran)", len(bootFolds), postCutKey)
	}
	if _, err := rebuilt.CommittedEvent(ctx, refusedKey); err != nil {
		t.Fatalf("the boot cannot read the refused fact %s by identity: %v, want the committed fact still owed on the chain", refusedKey, err)
	}
	if _, replayed := bootFolds[refusedKey]; replayed {
		t.Logf("the boot replayed the refused fact %s, so the cut did not cover it", refusedKey)
	}
	if _, held := booted.derivedOf(refusedKey); !held {
		foldedKeys := make([]string, 0, len(bootFolds))
		for key := range bootFolds {
			foldedKeys = append(foldedKeys, string(key))
		}
		slices.Sort(foldedKeys)
		t.Fatalf("the restarted consumer never applied the committed fact %s: the boot restored the cut %s and folded %v, yet %s is neither in the restored payload nor in the replay — committed, never applied, never replayed again",
			refusedKey, landed, foldedKeys, refusedKey)
	}
}

// TestCutRefusesToCoverAnUnappliedFact pins the cut a consumer's refusal refuses.
// - two consumers refusing two facts refuse the whole cut, by the identity of both and the name of both slices
// - a fact landing beside the refused ones changes nothing, and settling one identity leaves the other refusing
// - the same identity committed again folds the fact it names, files no second record, and lets the cut be filed
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCutRefusesToCoverAnUnappliedFact(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-cut-refuses"}
	refusedA := event.EventID("refuse-0-1")
	refusedB := event.EventID("refuse-1-1")
	first := &refusingConsumer{foldRecorder: &foldRecorder{name: "assembly.cut-first"}, refuse: refusedA}
	second := &refusingConsumer{foldRecorder: &foldRecorder{name: "assembly.cut-second"}, refuse: refusedB}
	source := wideWindowChainRun(t, ctx, storage, refusingBoot(first, second))
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, "refuse", 2)

	if got := first.refusals(); len(got) != 1 || got[0] != refusedA {
		t.Fatalf("the first consumer refused %v, want the one named fact %s", got, refusedA)
	}
	if got := second.refusals(); len(got) != 1 || got[0] != refusedB {
		t.Fatalf("the second consumer refused %v, want the one named fact %s", got, refusedB)
	}
	records := source.tier.Segments.EventCount()
	if _, filed, cutErr := source.Checkpoint(ctx); cutErr == nil || filed {
		t.Fatalf("file the cut over the two unapplied facts = (%t, %v), want it refused by name", filed, cutErr)
	} else {
		for _, want := range []string{string(refusedA), string(refusedB), first.StateName(), second.StateName()} {
			if !strings.Contains(cutErr.Error(), want) {
				t.Fatalf("the refused cut states %q, want it to name %q", cutErr, want)
			}
		}
	}
	if after := source.tier.Segments.EventCount(); after != records {
		t.Fatalf("the refused cut left %d record(s) beside the %d the account stood at, want a cut that files nothing", after, records)
	}

	commitSessionTurns(t, ctx, source, addr, "refuse-past", 1)
	if _, filed, cutErr := source.Checkpoint(ctx); cutErr == nil || filed {
		t.Fatalf("file the cut beside the facts that landed = (%t, %v), want it still refused", filed, cutErr)
	} else if !strings.Contains(cutErr.Error(), string(refusedA)) || !strings.Contains(cutErr.Error(), string(refusedB)) {
		t.Fatalf("the cut beside the facts that landed states %q, want it refused over both %s and %s", cutErr, refusedA, refusedB)
	}

	first.allow()
	settled, err := source.CommittedEvent(ctx, refusedA)
	if err != nil {
		t.Fatalf("read the refused fact %s back: %v", refusedA, err)
	}
	before := source.tier.Segments.EventCount()
	if _, err := source.CommitToSession(ctx, addr, settled); err != nil {
		t.Fatalf("commit the fact %s again: %v", refusedA, err)
	}
	if after := source.tier.Segments.EventCount(); after != before {
		t.Fatalf("the repeated commit left %d record(s) beside the %d the account stood at, want no second record", after, before)
	}
	if _, held := first.derivedOf(refusedA); !held {
		t.Fatalf("the repeated commit never folded %s into the %q slice, want the consumer that refused it asked again", refusedA, first.StateName())
	}
	if _, filed, cutErr := source.Checkpoint(ctx); cutErr == nil || filed {
		t.Fatalf("file the cut beside the settled fact = (%t, %v), want it refused over %s alone", filed, cutErr, refusedB)
	} else if strings.Contains(cutErr.Error(), string(refusedA)) || !strings.Contains(cutErr.Error(), string(refusedB)) {
		t.Fatalf("the cut beside the settled fact states %q, want it refused over %s alone, never over the settled %s", cutErr, refusedB, refusedA)
	}

	second.allow()
	settled, err = source.CommittedEvent(ctx, refusedB)
	if err != nil {
		t.Fatalf("read the refused fact %s back: %v", refusedB, err)
	}
	if _, err := source.CommitToSession(ctx, addr, settled); err != nil {
		t.Fatalf("commit the fact %s again: %v", refusedB, err)
	}
	key, filed, err := source.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut once every refused fact landed = (%q, %t, %v), want it filed", key, filed, err)
	}
	cut := checkpointOfIdentity(t, ctx, source, key)
	for _, name := range []string{first.StateName(), second.StateName()} {
		payload := slicePayloadOf(t, cut, name)
		for _, want := range []event.EventID{refusedA, refusedB} {
			if !strings.Contains(payload, string(want)) {
				t.Fatalf("the filed cut states the slice %q as %q, want it to carry the applied %s", name, payload, want)
			}
		}
	}
}

// TestCutNeverCoversAnUnappliedFact pins that no chain scopes a refusal.
// - the cut of the chain whose fact stands unapplied is refused, by the fact and the slice
// - the cut of a chain the fact is not of is refused too: the framework guesses no chain a refusal belongs to
// - the same identity committed again clears the refusal, every session cut files, and the slice of the chain beside it states that chain's facts and never the refused one
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCutNeverCoversAnUnappliedFact(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	owed := event.Address{AgentID: "agent-a", SessionID: "session-cut-owed"}
	beside := event.Address{AgentID: "agent-a", SessionID: "session-cut-beside"}
	refusedKey := event.EventID("owed-1-1")
	consumer := &sessionBehalf{refusingConsumer: &refusingConsumer{foldRecorder: &foldRecorder{name: "assembly.session-behind"}, refuse: refusedKey}}
	source := wideWindowChainRun(t, ctx, storage, refusingBoot(consumer))
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, owed, "owed", 2)
	commitSessionTurns(t, ctx, source, beside, "beside", 1)

	if _, filed, cutErr := source.CheckpointSession(ctx, owed); cutErr == nil || filed {
		t.Fatalf("file the cut of the chain holding the unapplied fact = (%t, %v), want it refused", filed, cutErr)
	} else if !strings.Contains(cutErr.Error(), string(refusedKey)) || !strings.Contains(cutErr.Error(), consumer.StateName()) {
		t.Fatalf("the refused session cut states %q, want it to name the fact %s and the slice %q", cutErr, refusedKey, consumer.StateName())
	}
	if _, filed, cutErr := source.CheckpointSession(ctx, beside); cutErr == nil || filed {
		t.Fatalf("file the cut of the chain beside the refusal = (%t, %v), want it refused too: the framework guesses no chain a refusal belongs to", filed, cutErr)
	} else if !strings.Contains(cutErr.Error(), string(refusedKey)) {
		t.Fatalf("the refused cut of the chain beside states %q, want it to name the fact %s", cutErr, refusedKey)
	}
	if _, filed, cutErr := source.Checkpoint(ctx); cutErr == nil || filed {
		t.Fatalf("file the whole-account cut = (%t, %v), want it refused over the fact of one chain no consumer applied", filed, cutErr)
	}

	consumer.allow()
	settled, err := source.CommittedEvent(ctx, refusedKey)
	if err != nil {
		t.Fatalf("read the refused fact %s back: %v", refusedKey, err)
	}
	if _, err := source.CommitToSession(ctx, owed, settled); err != nil {
		t.Fatalf("commit the fact %s again: %v", refusedKey, err)
	}
	for _, chain := range []event.Address{owed, beside} {
		key, filed, err := source.CheckpointSession(ctx, chain)
		if err != nil || !filed {
			t.Fatalf("file the cut of %s once the fact landed = (%q, %t, %v), want it filed", chain.SessionID, key, filed, err)
		}
		if chain != beside {
			continue
		}
		payload := string(slicePayloadOf(t, checkpointOfIdentity(t, ctx, source, key), consumer.StateName()))
		if !strings.Contains(payload, "beside-0-0") {
			t.Fatalf("the cut of the chain beside the refusal states %q, want that chain's own facts", payload)
		}
		if strings.Contains(payload, string(refusedKey)) {
			t.Fatalf("the cut of the chain beside the refusal states %q, want no fact of the chain that owes %s", payload, refusedKey)
		}
	}
}

// TestAnchorRebuildFoldsTheRangeNoCheckpointVouches pins the seam read an anchor may bound.
// - a covered fact no checkpoint vouches still folds into the state seam, while the projection keeps the anchor's fold
// - a consumer refusing that covered fact refuses the rebuild by name, so the obligation survives the restart
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestAnchorRebuildFoldsTheRangeNoCheckpointVouches(t *testing.T) {
	ctx := context.Background()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-anchor-seam"}
	refusedKey := event.EventID("anchor-seam-0-3")
	for _, tc := range []struct {
		name   string
		refuse bool
	}{{"applies", false}, {"refuses", true}} {
		t.Run(tc.name, func(t *testing.T) {
			storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
			source := wideWindowChainRun(t, ctx, storage)
			source.stopCheckpointFiler()
			commitSessionTurns(t, ctx, source, addr, "anchor-seam", 2)
			survivors := []anchorEntry{
				{Key: "anchor-seam-0-0", Type: event.TypeExternalInput, Summary: "ask"},
				{Key: "anchor-seam-1-3", Type: event.TypeAgentOutput, Summary: "answer"},
			}
			payload, err := json.Marshal(survivors)
			if err != nil {
				t.Fatalf("encode the survivor payload: %v", err)
			}
			request := event.CompressRequestFor("assembly.test", "the seam the anchor bounds", "the fixture folds a range no checkpoint vouches")
			product := event.CompressProductFor(request.ID, compressionProducer, event.EventID("-summary-anchor-seam"), nil, "the folded summary").WithCheckpoint(payload)
			if _, err := source.commitFactChecked(ctx, compressProductEvent(addr.SessionID, addr.AgentID, product, time.Now().UTC())); err != nil {
				t.Fatalf("commit the anchor: %v", err)
			}
			endRun(t, source)

			consumer := &refusingConsumer{foldRecorder: &foldRecorder{name: "assembly.anchor-seam"}}
			if tc.refuse {
				consumer.refuse = refusedKey
			}
			if tc.refuse {
				provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
					return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
				}}
				_, err := Assemble(ctx, &config.Declaration{
					Entry:  "agent-a",
					Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 100000}},
				}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}},
					WithGovernance(snapshotTestGovernance{}), storage, refusingBoot(consumer))
				if err == nil || !strings.Contains(err.Error(), string(refusedKey)) {
					t.Fatalf("the anchored boot answered %v, want it refused over the covered fact %s", err, refusedKey)
				}
				return
			}
			rebuild := wideWindowChainRun(t, ctx, storage, refusingBoot(consumer))
			rebuild.stopCheckpointFiler()
			seeds, report, err := rebuild.seedAnchoredReplay(ctx, rebuild.tier)
			if tc.refuse {
				if err == nil || !strings.Contains(err.Error(), string(refusedKey)) {
					t.Fatalf("the anchored rebuild answered %v, want it refused over the covered fact %s", err, refusedKey)
				}
				return
			}
			if err != nil {
				t.Fatalf("the anchored rebuild: %v", err)
			}
			if report.Anchors != 1 {
				t.Fatalf("the rebuild used %d anchor(s), want the one the chain carries", report.Anchors)
			}
			if _, held := consumer.derivedOf(refusedKey); !held {
				t.Fatalf("the rebuild never folded the covered fact %s into the state seam, want the range no checkpoint vouches folded", refusedKey)
			}
			seed, carried := seeds[addr]
			if !carried {
				t.Fatal("the anchored rebuild registered no projection for the chain")
			}
			for _, entry := range seed.projection.Entries() {
				if entry.Key == refusedKey {
					t.Fatalf("the rebuilt projection holds the covered fact %s, want the anchor's fold to keep it out of the projection", refusedKey)
				}
			}
		})
	}
}

// TestRestartResumesTheObligationTheConsumerFailed pins the retry a refusal waits for.
// - the cut attempted beside the refusal is refused by name, so no cut states a position past the fact
// - the same identity committed again folds it into the consumer that refused it and files no second record
// - the restart that reads the cut holds what that consumer derived, through the same reducer the live commit runs
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRestartResumesTheObligationTheConsumerFailed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-resume"}
	sliceName := "assembly.resume"
	refusedFact := retryFactOf("notice-resume", addr)
	refusedKey := refusedFact.Key
	live := &refusingConsumer{foldRecorder: &foldRecorder{name: sliceName}, refuse: refusedKey}
	source := wideWindowChainRun(t, ctx, storage, refusingBoot(live))
	source.stopCheckpointFiler()
	if _, err := source.CommitToSession(ctx, addr, refusedFact); err != nil {
		t.Fatalf("commit the fact the consumer refuses: %v", err)
	}
	commitSessionTurns(t, ctx, source, addr, "resume", 1)
	if got := live.refusals(); len(got) != 1 || got[0] != refusedKey {
		t.Fatalf("the consumer refused %v, want exactly the one named fact %s", got, refusedKey)
	}

	if _, filed, cutErr := source.Checkpoint(ctx); cutErr == nil || filed {
		t.Fatalf("file the cut beside the refusal = (%t, %v), want it refused by name", filed, cutErr)
	}
	live.allow()
	settled, err := source.CommittedEvent(ctx, refusedKey)
	if err != nil {
		t.Fatalf("read the refused fact %s back: %v", refusedKey, err)
	}
	before := source.tier.Segments.EventCount()
	if _, err := source.CommitToSession(ctx, addr, settled); err != nil {
		t.Fatalf("commit the fact %s again: %v", refusedKey, err)
	}
	if after := source.tier.Segments.EventCount(); after != before {
		t.Fatalf("the repeated commit left %d record(s) beside the %d the account stood at, want no second record", after, before)
	}
	if _, held := live.derivedOf(refusedKey); !held {
		t.Fatalf("the repeated commit never folded %s into the %q slice, want the refusal retried rather than answered alone", refusedKey, sliceName)
	}
	derived, _ := live.derivedOf(refusedKey)
	if key, filed, err := source.Checkpoint(ctx); err != nil || !filed {
		t.Fatalf("file the cut once the retried fact landed = (%q, %t, %v), want it filed", key, filed, err)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(10 * time.Second)}); err != nil {
		t.Fatalf("close the live process: %v", err)
	}

	occurrences := 0
	booted := &foldRecorder{name: sliceName}
	rebuilt := wideWindowChainRun(t, ctx, storage, recordingBoot(booted))
	rebuilt.stopCheckpointFiler()
	if err := rebuilt.tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, _ string, _ event.SessionID) error {
		if key == refusedKey {
			occurrences++
		}
		return nil
	}); err != nil {
		t.Fatalf("walk the index of the restarted run: %v", err)
	}
	if occurrences != 1 {
		t.Fatalf("the durable chain holds %d record(s) under %s, want exactly one", occurrences, refusedKey)
	}
	if held, _ := booted.derivedOf(refusedKey); held != derived {
		t.Fatalf("the restarted consumer derived %q for %s, want the %q the live consumer derived through the same reducer", held, refusedKey, derived)
	}
}

// owedSlice is a state slice that states one unfinished obligation.
type owedSlice struct {
	name string
	mu   sync.Mutex
	owed []string
}

// StateName reports the name the owed slice is keyed by.
func (s *owedSlice) StateName() string { return s.name }

// SnapshotState states the open obligations.
func (s *owedSlice) SnapshotState(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Marshal(s.owed)
}

// RestoreState puts the stated obligations back.
func (s *owedSlice) RestoreState(_ context.Context, payload []byte) error {
	var stated []string
	if err := json.Unmarshal(payload, &stated); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owed = append(s.owed, stated...)
	return nil
}

// open records one unfinished obligation.
func (s *owedSlice) open(item string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owed = append(s.owed, item)
}

// TestUnmountKeepsTheOwedItemsOfItsSlice is symptom anchor S5: an unmount must hand the owed work over or refuse by name.
//   - fixture: one slice states an open obligation, a cut states it, the slice is unmounted, a second cut is filed, the process restarts
//   - positive: the obligation survives the unmount — a named successor states it, or the unmount is refused by name with the unfinished count
//   - negative: a disposer that deletes the holder leaves the obligation stated nowhere, which the second cut and the boot both reject
//   - consumer: StateRegistry.Register's disposer, Run.Checkpoint and the real boot entry
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestUnmountKeepsTheOwedItemsOfItsSlice(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-owed-unmount"}
	const sliceName = "assembly.owed-consumer"
	const owedItem = "the-notice-still-owed"

	holder := &owedSlice{name: sliceName}
	var unmount func()
	source := windowChainRun(t, ctx, storage, withBootConfigReconcile(func(ctx context.Context, run *Run, decl *config.Declaration, agents map[event.AgentID]NativeAgent) error {
		dispose, err := run.RegisterStateSlice(holder)
		if err != nil {
			return err
		}
		unmount = dispose
		_, err = run.Publish(ctx, decl, agents)
		return err
	}))
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, "owed", 1)
	holder.open(owedItem)

	firstKey, filed, err := source.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the first cut = (%t, %v), want a filed cut", filed, err)
	}
	first := checkpointOfIdentity(t, ctx, source, firstKey)
	if !strings.Contains(slicePayloadOf(t, first, sliceName), owedItem) {
		t.Fatalf("the first cut states %q, want the open obligation %s (the fixture states nothing, so the unmount proves nothing)", slicePayloadOf(t, first, sliceName), owedItem)
	}

	unmount()
	if names := source.StateRegistry().Names(); sliceContainsName(names, sliceName) {
		t.Fatalf("the registry still names %v after the unmount, want the slice gone", names)
	}
	secondKey, filed, err := source.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the second cut = (%t, %v), want a filed cut", filed, err)
	}
	second := checkpointOfIdentity(t, ctx, source, secondKey)
	if secondKey == firstKey {
		t.Fatalf("the second cut names the key %s of the first, want a later cut", secondKey)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(30 * time.Second)}); err != nil {
		t.Fatalf("close the live process: %v", err)
	}

	rebuilt := windowChainRun(t, ctx, storage)
	rebuilt.stopCheckpointFiler()
	restoredKey, _ := rebuilt.CheckpointFiled()
	if restoredKey == "" {
		t.Fatal("the boot restored no checkpoint, so the obligation comparison proves nothing")
	}
	if pending := rebuilt.StateRegistry().PendingRestore(); len(pending) != 0 {
		t.Logf("the boot names the pending slices %v", pending)
	}
	bootCut := checkpointOfIdentity(t, ctx, rebuilt, restoredKey)
	if sliceContains(t, bootCut, sliceName) && strings.Contains(slicePayloadOf(t, bootCut, sliceName), owedItem) {
		return
	}
	t.Fatalf("the open obligation %s vanished with the unmount: the first cut %s stated it, the disposer deleted the holder without a named refusal or a named successor, the second cut %s states the slices %v, and the boot restored %s stating %v and naming no pending slice — the owed work is stated nowhere and nothing named the loss",
		owedItem, firstKey, secondKey, second.CheckpointSliceNames(), restoredKey, bootCut.CheckpointSliceNames())
}

// sliceContains reports whether one checkpoint states a slice of the given name.
func sliceContains(t *testing.T, checkpoint event.Checkpoint, name string) bool {
	t.Helper()
	for _, slice := range checkpoint.Slices {
		if slice.Name == name {
			return true
		}
	}
	return false
}

// slicePayloadOf renders the payload one checkpoint states for a slice.
func slicePayloadOf(t *testing.T, checkpoint event.Checkpoint, name string) string {
	t.Helper()
	for _, slice := range checkpoint.Slices {
		if slice.Name == name {
			return string(slice.Payload)
		}
	}
	return ""
}

// sliceContainsName reports whether a name list carries one name.
func sliceContainsName(names []string, name string) bool {
	for _, held := range names {
		if held == name {
			return true
		}
	}
	return false
}
