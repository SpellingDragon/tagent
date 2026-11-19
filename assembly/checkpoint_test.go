package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

// errInjectedCloseBarrier names the failure a close fixture injects at the storage
// engine seam: the barrier a commit completes did not complete.
var errInjectedCloseBarrier = errors.New("the barrier over the journal did not complete")

// closeBarrierEngine is the storage engine seam a close fixture wraps: it states
// which batches carried keys and which only completed a barrier, and it fails the
// barrier-completing ones once the fixture arms it. The seam is the one the engine
// contract names for failure injection, so the same injection runs over any engine.
type closeBarrierEngine struct {
	ledger.AtomicStore
	mu sync.Mutex
	// writes counts the batches that carried keys.
	writes int
	// barriers counts the batches that carried none and only completed a barrier.
	barriers int
	// failed counts the armed barriers the wrapper answered with the failure.
	failed int
	// written holds the keys the batches carried, in commit order.
	written []string
	// armed answers every barrier-completing batch from then on.
	armed bool
	// calls counts the batches the engine was handed.
	calls int
	// failedAt is the call number of the first armed barrier, and -1 before one.
	failedAt int
}

// Commit records the batch it is handed and answers the barrier the fixture armed.
func (e *closeBarrierEngine) Commit(ctx context.Context, batch ledger.Batch) error {
	carries := len(batch.Puts) > 0 || len(batch.Deletes) > 0
	e.mu.Lock()
	e.calls++
	if carries {
		e.writes++
		for _, put := range batch.Puts {
			e.written = append(e.written, put.Key)
		}
	} else {
		e.barriers++
	}
	armed := e.armed
	if armed && !carries {
		e.failed++
		if e.failedAt < 0 {
			e.failedAt = e.calls
		}
	}
	e.mu.Unlock()
	if armed && !carries {
		return fmt.Errorf("%w: %w", ledger.ErrJournalBarrier, errInjectedCloseBarrier)
	}
	return e.AtomicStore.Commit(ctx, batch)
}

// Close releases the engine the wrapper holds, so the storage root's write rights
// leave with the view that holds them.
func (e *closeBarrierEngine) Close() error {
	if closer, held := e.AtomicStore.(interface{ Close() error }); held {
		return closer.Close()
	}
	return nil
}

// arm fails every barrier-completing batch this engine is handed from now on.
func (e *closeBarrierEngine) arm() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.armed = true
}

// counted states what the engine was handed so far: the batches that carried keys,
// the ones that only completed a barrier and the armed barriers it failed.
func (e *closeBarrierEngine) counted() (writes, barriers, failed int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.writes, e.barriers, e.failed
}

// afterFailure states how many batches the engine was handed after the first
// armed barrier, and -1 while it was handed none that failed.
func (e *closeBarrierEngine) afterFailure() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failedAt < 0 {
		return -1
	}
	return e.calls - e.failedAt
}

// writtenKeys states the keys the batches carried, in commit order.
func (e *closeBarrierEngine) writtenKeys() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.written...)
}

// closeBarrierRun assembles one run whose storage commits through probe over root,
// so a test reads the batches of a commit beside the close that follows it.
func closeBarrierRun(t *testing.T, ctx context.Context, root string, probe *closeBarrierEngine) *Run {
	t.Helper()
	engine, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the engine of the root: %v", err)
	}
	probe.AtomicStore = engine
	return windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, Engine: probe}))
}

// TestCloseReportsTheActualBarrierFailure pins the barrier a close takes over the storage engine, and that a failed one reaches the caller.
//   - the fixture's facts commit through the probe, so the keys the close's barrier is taken over are the ones the assertions read back
//   - a barrier that does not complete inside the close answers the close, named as the barrier failure the engine states instead of a clean close
//   - that failure is not a refusal decided before a write, and it is the last batch the close handed the engine
//   - the checkpoint the close files before its last barrier stands filed, so the error comes from that barrier and not an earlier layer
//   - the failed close still releases the storage root and leaves the facts the commits landed under it, so the error is not a state it broke
//   - a close beside no armed barrier answers nil and takes that same barrier, so the failure above is the armed barrier (negative)
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestCloseReportsTheActualBarrierFailure(t *testing.T) {
	ctx := context.Background()

	t.Run("an_armed_barrier_failure_reaches_the_callers_close", func(t *testing.T) {
		root := t.TempDir()
		probe := &closeBarrierEngine{failedAt: -1}
		run := closeBarrierRun(t, ctx, root, probe)
		run.checkpointEvery = 1 << 30
		run.checkpointPeriod = 1000 * time.Hour
		addr := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-barrier"}
		commitWindowTurns(t, ctx, run, addr, 1)
		if writes, _, _ := probe.counted(); writes == 0 {
			t.Fatal("the fixture committed no fact through the probe, so a close under it proves nothing")
		}
		probe.arm()
		_, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(30 * time.Second)})
		writes, barriers, failed := probe.counted()
		if failed == 0 {
			t.Fatalf("the close was handed %d batch(es) carrying keys and completed %d barrier(s) through the probe without reaching the armed barrier, so the answer it states is not the barrier's",
				writes, barriers)
		}
		if after := probe.afterFailure(); after != 0 {
			t.Fatalf("the engine was handed %d batch(es) after the barrier that did not complete, want the failure the close ends on instead of one a later call of the close could have swallowed", after)
		}
		if id, _ := run.CheckpointFiled(); id == "" {
			t.Fatalf("the close answered %v beside no checkpoint filed, want the checkpoint it files before the barrier it ends on", err)
		}
		if err == nil {
			t.Fatalf("the close answered no failure beside %d barrier(s) that did not complete, want the barrier it could not take", failed)
		}
		if !errors.Is(err, ledger.ErrJournalBarrier) {
			t.Fatalf("the close beside a barrier that did not complete answered %v, want %v", err, ledger.ErrJournalBarrier)
		}
		if !strings.Contains(err.Error(), errInjectedCloseBarrier.Error()) {
			t.Fatalf("the close answered %q, want the failure the barrier answered", err)
		}
		for _, refusal := range []struct {
			what string
			err  error
		}{
			{"a view Close ended refusing a later commit", ledger.ErrCommitAfterClose},
			{"a view holding no write rights", ledger.ErrCommitWithoutWriteRights},
			{"a frame the size bound refuses", ledger.ErrJournalFrameTooLarge},
		} {
			if errors.Is(err, refusal.err) {
				t.Fatalf("the close answered %v beside a barrier that did not complete, want the barrier's own failure and not %s", err, refusal.what)
			}
		}
		engine, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
		if err != nil {
			t.Fatalf("open the root the failed close ended on = %v, want a root the next writer is admitted to", err)
		}
		keys := probe.writtenKeys()
		held := 0
		for _, key := range keys {
			pairs, err := engine.Read(ctx, ledger.Fetch{Key: key})
			if err != nil {
				t.Fatalf("read %s back from the released root: %v", key, err)
			}
			if len(pairs) > 0 {
				held++
			}
		}
		if held == 0 {
			t.Fatalf("the released root holds no value under any of the %d key(s) the commits landed, want the facts the close's barrier was taken over", len(keys))
		}
		if closer, ok := engine.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				t.Fatalf("close the root the fixture reopened: %v", err)
			}
		}
	})

	t.Run("a_close_beside_no_armed_barrier_answers_nil", func(t *testing.T) {
		root := t.TempDir()
		probe := &closeBarrierEngine{failedAt: -1}
		run := closeBarrierRun(t, ctx, root, probe)
		run.checkpointEvery = 1 << 30
		run.checkpointPeriod = 1000 * time.Hour
		addr := event.Address{AgentID: "agent-a", SessionID: "session-checkpoint-barrier-clean"}
		commitWindowTurns(t, ctx, run, addr, 1)
		if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(30 * time.Second)}); err != nil {
			t.Fatalf("close a run whose barrier completes = %v, want a closed run", err)
		}
		if _, barriers, failed := probe.counted(); barriers == 0 || failed != 0 {
			t.Fatalf("the unarmed close completed %d barrier(s) and failed %d of them through the probe, want the barrier this close takes completed", barriers, failed)
		}
	})
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

// retryFoldedOutput commits windowed turns until the fold moves the first turn's output out of the projection, and answers that fact with the turn it belongs to.
//   - the fold fires between commits as it does online, so the output the source query reads back by identity is material the projection has moved out: the retry of it is the one that must not state a second carriage
func retryFoldedOutput(t *testing.T, ctx context.Context, run *Run, addr event.Address, turns int) event.Event {
	t.Helper()
	run.stopCheckpointFiler()
	commitWindowTurns(t, ctx, run, addr, turns)
	folded := event.EventID(timeStampKey(time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), addr, 0, 3))
	if slices.Contains(projectionKeysOf(t, run, addr), folded) {
		t.Fatalf("the projection still carries %s: the fixture needs the fold to move it out", folded)
	}
	fact, err := run.CommittedEvent(ctx, folded)
	if err != nil {
		t.Fatalf("read the folded output back by identity: %v", err)
	}
	return fact
}

// retryFoldedOutputLeavesTheRangeAlone retries a folded output and requires the
// presented range, the sequence and the filed products to stand as they were.
func retryFoldedOutputLeavesTheRangeAlone(t *testing.T, ctx context.Context, run *Run, addr event.Address, fact event.Event) {
	t.Helper()
	run.mu.Lock()
	_, held, carried := run.replaySlotLocked(addr)
	run.mu.Unlock()
	if !carried {
		t.Fatalf("the chain %v presents no mapped sequence", addr)
	}
	endedAt, open := held.endedAt, held.rangeOpen
	before := run.SessionPresented(addr)
	keys := projectionKeysOf(t, run, addr)
	products := countFoldProducts(t, ctx, run)
	if _, err := run.CommitToSession(ctx, addr, fact); err != nil {
		t.Fatalf("retry the folded output %s: %v", fact.Key, err)
	}
	run.mu.Lock()
	_, after, _ := run.replaySlotLocked(addr)
	run.mu.Unlock()
	if after.endedAt != endedAt || after.rangeOpen != open {
		t.Fatalf("the retry of the folded output %s moved the completed range from (%s, open=%t) to (%s, open=%t)",
			fact.Key, endedAt, open, after.endedAt, after.rangeOpen)
	}
	if presented := run.SessionPresented(addr); !reflect.DeepEqual(presented, before) {
		t.Fatalf("the retry of the folded output %s presented %v, want the range it stood at %v", fact.Key, presented, before)
	}
	if now := projectionKeysOf(t, run, addr); !slices.Equal(now, keys) {
		t.Fatalf("the retry of the folded output %s restated the sequence as %v, want %v", fact.Key, now, keys)
	}
	if now := countFoldProducts(t, ctx, run); now != products {
		t.Fatalf("the retry of the folded output %s filed %d products beside the %d the chain held: the fold stated the same material twice", fact.Key, now, products)
	}
}

// TestRetryLeavesPresentedRangeAtTheLatestRound keeps successful consumers unchanged when an older fact is retried.
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRetryLeavesPresentedRangeAtTheLatestRound(t *testing.T) {
	t.Run("folded", func(t *testing.T) {
		ctx := context.Background()
		addr := event.Address{AgentID: "agent-a", SessionID: "retry-folded"}
		run := windowChainRun(t, ctx)
		fact := retryFoldedOutput(t, ctx, run, addr, 3)
		if countFoldProducts(t, ctx, run) == 0 {
			t.Fatalf("the fixture folded nothing: the retried output stands in the projection")
		}
		retryFoldedOutputLeavesTheRangeAlone(t, ctx, run, addr, fact)
	})
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

// TestRetryOfFoldedOutputDoesNotReopenTheRound pins the judgment the presented range advances under.
//   - the retry of an output the fold already moved out of the sequence states no close of its own
//   - so the range stays closed at the round that closed it and the sequence is not stated a second time
//   - fixture: windowed turns whose fold replaced the first turn's output with a product, and the retry of that output through the live commit entry
//   - positive: the range stays closed at the newest round and the presented range, the sequence and the filed products stand unchanged
//   - negative: a judgment resting on what the projection retains answers the retried identity as newly carried
//   - negative: that reopens the closed round and mints the same fold twice
//   - consumer: the live commit entry (CommitToSession → commitFactNoCadence → StateRegistry.Reduce → projectionStateSlice.Reduce)
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRetryOfFoldedOutputDoesNotReopenTheRound(t *testing.T) {
	ctx := context.Background()
	addr := event.Address{AgentID: "agent-a", SessionID: "retry-folded-range"}
	run := windowChainRun(t, ctx)
	fact := retryFoldedOutput(t, ctx, run, addr, 3)
	retryFoldedOutputLeavesTheRangeAlone(t, ctx, run, addr, fact)
}

// TestRestartRetryOfFoldedOutputKeepsThePresentedRange pins the same judgment across a restart.
//   - the chain on the storage tier already holds the folded output, so the retry of it after the rebuilt boot states no new position either
//   - fixture: windowed turns over a durable root, the fold moving the first turn's output out, the source process closed
//   - fixture: a rebuilt boot over the same root
//   - positive: the retry leaves the presented range, the sequence and the filed products as the boot rebuilt them
//   - negative: a retry the tier answers as already present and the fold answers as newly carried states the material the chain folded away a second time
//   - consumer: the real boot entry (Assemble → seedAnchoredReplay → StateRegistry.Reduce) beside the live commit entry, over one durable root
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRestartRetryOfFoldedOutputKeepsThePresentedRange(t *testing.T) {
	ctx := context.Background()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "retry-folded-restart"}
	source := windowChainRun(t, ctx, storage)
	fact := retryFoldedOutput(t, ctx, source, addr, 3)
	endRun(t, source)

	booted := windowChainRun(t, ctx, storage)
	booted.stopCheckpointFiler()
	if keys := projectionKeysOf(t, booted, addr); slices.Contains(keys, fact.Key) {
		t.Fatalf("the rebuilt projection %v carries the folded output %s", keys, fact.Key)
	}
	retryFoldedOutputLeavesTheRangeAlone(t, ctx, booted, addr, fact)
	endRun(t, booted)
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

// TestConsumerBehindTheChainSurvivesARestart is symptom anchor S4: the cut over a fact no consumer applied is refused.
//   - fixture: one consumer refuses one committed fact the projection never carries, after an earlier cut landed, and the process restarts
//   - positive: the cut over it is refused by name, the earlier cut stays the newest, and the restarted consumer applies the fact it refused
//   - positive: a restart resumes from the cut that landed
//   - negative: a cut that covers a fact no consumer applied leaves that fact with no replay path, which the boot's derived set rejects
//   - consumer: the real boot entry (restoreCheckpointState → seedAnchoredReplay → StateRegistry.Reduce) and the live commit entry
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestConsumerBehindTheChainSurvivesARestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-consumer-behind"}
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
//   - the same identity committed again clears the refusal, and every session cut files
//   - the slice of the chain beside it states that chain's facts and never the refused one
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

// orderObligation is the consumer of a chain whose state turns on the order its
// facts arrive: it folds a fact only after one the chain committed earlier and
// refuses any other by name. The rank map is the order a check read off the
// living run, so the account states an obligation a rebuild that reorders the
// chain breaks instead of folding a state no run derived.
type orderObligation struct {
	*foldRecorder
	mu     sync.Mutex
	rank   map[event.EventID]int
	folded []event.EventID
	last   int
	begun  bool
}

// errOutOfOrderObligation names the refusal of a fact the chain committed before the fact folded last.
var errOutOfOrderObligation = errors.New("the consumer refuses a fact the chain committed out of order")

// Reduce folds one fact at the position the chain committed it.
func (o *orderObligation) Reduce(ctx context.Context, evt event.Event) error {
	position, ranked := o.rank[evt.Key]
	o.mu.Lock()
	if ranked && o.begun && position < o.last {
		previous := o.folded[len(o.folded)-1]
		o.mu.Unlock()
		return fmt.Errorf("%w: %s stands before %s", errOutOfOrderObligation, evt.Key, previous)
	}
	if ranked {
		o.begun, o.last = true, position
	}
	o.folded = append(o.folded, evt.Key)
	o.mu.Unlock()
	return o.foldRecorder.Reduce(ctx, evt)
}

// order reports the identities the consumer folded, in fold order.
func (o *orderObligation) order() []event.EventID {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]event.EventID(nil), o.folded...)
}

// anchorOrderChain boots a living run over one durable root, commits a chain of
// three turns and commits the anchor payload the check names on top of it, and
// answers the storage the restart opens with the consumer that folded the
// living chain.
func anchorOrderChain(t *testing.T, ctx context.Context, addr event.Address, tag string, survivors []anchorEntry) (Option, *orderObligation) {
	t.Helper()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	live := &orderObligation{foldRecorder: &foldRecorder{name: "assembly.anchor-order-" + tag}}
	source := wideWindowChainRun(t, ctx, storage, refusingBoot(live))
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, tag, 3)
	payload, err := json.Marshal(survivors)
	if err != nil {
		t.Fatalf("encode the survivor payload: %v", err)
	}
	request := event.CompressRequestFor("assembly.test", "the commit order a rebuild folds", "the fixture folds a range beside the survivors it states")
	product := event.CompressProductFor(request.ID, compressionProducer, event.EventID("-summary-"+tag), nil, "the folded summary").WithCheckpoint(payload)
	if _, err := source.commitFactChecked(ctx, compressProductEvent(addr.SessionID, addr.AgentID, product, time.Now().UTC())); err != nil {
		t.Fatalf("commit the anchor: %v", err)
	}
	endRun(t, source)
	return storage, live
}

// TestAnchoredRecoveryPreservesCommitOrderAcrossSurvivors pins the one commit order a rebuild folds.
// - positive: the facts a survivor payload states and the facts of the range beside it reach the state seam in commit order, element by element
// - positive: the chain's survivors and its covered facts interleave in that order, so neither the payload nor the range alone states it
// - positive: every fact folds once, the state each derives is the state the living run derived, and the projection still presents the payload's own order
// - negative: a consumer whose obligation states any order but the commit order refuses the rebuild by name (TestAnchoredRecoveryRefusesAnOutOfOrderObligation)
// - consumer: the real boot entry (Assemble → restoreCheckpointState → seedAnchoredReplay → seedFact → StateRegistry.Reduce)
// - consumer: the live commit entry (commitFactNoCadence → StateRegistry.Reduce)
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestAnchoredRecoveryPreservesCommitOrderAcrossSurvivors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-anchor-order"}
	tag := "anchor-order"
	first, last := event.EventID(tag+"-0-0"), event.EventID(tag+"-2-3")
	survivors := []anchorEntry{
		{Key: first, Type: event.TypeExternalInput, Summary: "ask"},
		{Key: last, Type: event.TypeAgentOutput, Summary: "answer"},
	}
	storage, live := anchorOrderChain(t, ctx, addr, tag, survivors)
	liveOrder := live.order()
	rank := make(map[event.EventID]int, len(liveOrder))
	for at, key := range liveOrder {
		rank[key] = at
	}

	rebuilt := &orderObligation{foldRecorder: &foldRecorder{name: "assembly.anchor-order-rebuild"}, rank: rank}
	rebuild := wideWindowChainRun(t, ctx, storage, refusingBoot(rebuilt))
	rebuild.stopCheckpointFiler()

	folded := make([]event.EventID, 0, len(rank))
	for _, key := range rebuilt.order() {
		if _, held := rank[key]; held {
			folded = append(folded, key)
		}
	}
	if len(folded) < 4 {
		t.Fatalf("the rebuild folded %d chain fact(s), want the survivors beside the covered range", len(folded))
	}
	foldedSet := make(map[event.EventID]struct{}, len(folded))
	for _, key := range folded {
		foldedSet[key] = struct{}{}
	}
	want := make([]event.EventID, 0, len(folded))
	for _, key := range liveOrder {
		if _, held := foldedSet[key]; held {
			want = append(want, key)
		}
	}
	if !slices.Equal(want, folded) {
		t.Fatalf("the rebuild folded the chain as %v, want the order the living run committed it in: %v", folded, want)
	}

	restored := map[event.EventID]struct{}{first: {}, last: {}}
	var restoredSeen, coveredSeen, coveredAfter, survivorAfter bool
	for _, key := range folded {
		if _, held := restored[key]; held {
			if coveredSeen {
				survivorAfter = true
			}
			restoredSeen = true
			continue
		}
		if restoredSeen {
			coveredAfter = true
		}
		coveredSeen = true
	}
	if !coveredAfter || !survivorAfter {
		t.Fatalf("the rebuild folded the survivors beside the covered range as %v, want the fixture's two classes to interleave so no per-class order states it", folded)
	}

	for key, count := range rebuilt.folds() {
		if _, held := rank[key]; !held {
			continue
		}
		if count != 1 {
			t.Fatalf("the rebuild folded %s %d time(s), want the seam to take every fact once", key, count)
		}
		wantState, _ := live.derivedOf(key)
		gotState, _ := rebuilt.derivedOf(key)
		if gotState != wantState {
			t.Fatalf("the rebuild derived %q for %s, want the state the living run derived: %q", gotState, key, wantState)
		}
	}

	keys := projectionKeysOf(t, rebuild, addr)
	if wantKeys := []event.EventID{first, last}; !slices.Equal(keys, wantKeys) {
		t.Fatalf("the rebuilt projection presents %v, want the anchor's own display order %v with the covered range left to the seam", keys, wantKeys)
	}
	endRun(t, rebuild)
}

// TestAnchoredRecoveryRefusesAnOutOfOrderObligation pins that a rebuild refuses a reordered chain by name.
// - negative: a consumer whose obligation states the covered range before the survivors it states refuses the boot, naming the fact and the fact it stands before
// - consumer: the real boot entry (Assemble → restoreCheckpointState → seedAnchoredReplay → seedFact → StateRegistry.Reduce)
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestAnchoredRecoveryRefusesAnOutOfOrderObligation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-anchor-order-refused"}
	tag := "anchor-order-refused"
	first, last := event.EventID(tag+"-0-0"), event.EventID(tag+"-2-3")
	survivors := []anchorEntry{
		{Key: first, Type: event.TypeExternalInput, Summary: "ask"},
		{Key: last, Type: event.TypeAgentOutput, Summary: "answer"},
	}
	storage, live := anchorOrderChain(t, ctx, addr, tag, survivors)

	restored := map[event.EventID]struct{}{first: {}, last: {}}
	outOfOrder := make(map[event.EventID]int, len(live.order()))
	for _, key := range live.order() {
		if _, held := restored[key]; held {
			continue
		}
		if key == event.EventID("-summary-"+tag) {
			continue
		}
		outOfOrder[key] = len(outOfOrder)
	}
	outOfOrder[first] = len(outOfOrder)
	outOfOrder[last] = len(outOfOrder)
	if len(outOfOrder) < 4 {
		t.Fatalf("the fixture states %d position(s), want the covered range beside the survivors", len(outOfOrder))
	}

	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	_, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 100000}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}},
		WithGovernance(snapshotTestGovernance{}), storage,
		refusingBoot(&orderObligation{foldRecorder: &foldRecorder{name: "assembly.anchor-order-refused"}, rank: outOfOrder}))
	if err == nil {
		t.Fatal("the rebuild folded the chain beside an obligation that states no commit order, want it refused by name")
	}
	if !errors.Is(err, errOutOfOrderObligation) {
		t.Fatalf("the rebuild answered %v, want it refused by the order obligation", err)
	}
	if !strings.Contains(err.Error(), string(first)) {
		t.Fatalf("the refusal states %q, want it to name the survivor %s the out-of-order fact stands before", err, first)
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

// UnfinishedWork reports the obligations an unmount would lose.
func (s *owedSlice) UnfinishedWork() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.owed)
}

// owes reports the obligations the slice still holds.
func (s *owedSlice) owes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.owed...)
}

// settle clears the obligations the slice answered.
func (s *owedSlice) settle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owed = nil
}

// TestUnmountKeepsTheOwedItemsOfItsSlice is symptom anchor S5: a slice that still owes work may not be unmounted.
//   - fixture: the slice states an open obligation, a cut states it, the disposer is called, the slice records a second one, a cut follows, the process restarts
//   - positive: the disposer refuses by name with the count, the registry keeps that very holder, the restart hands both obligations back to that name
//   - negative: a disposer that deletes the holder answers no refusal and states the obligations nowhere, which the count, the kept name and the payload each reject
//   - consumer: Run.RegisterStateSlice's disposer, Run.Checkpoint and the real boot entry
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestUnmountKeepsTheOwedItemsOfItsSlice(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-owed-unmount"}
	const sliceName = "assembly.owed-consumer"
	const owedItem = "the-notice-still-owed"
	const laterItem = "the-notice-that-arrived-after-the-refusal"

	holder := &owedSlice{name: sliceName}
	var unmount func() error
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

	refusal := unmount()
	if refusal == nil {
		t.Fatal("the unmount of a slice stating an open obligation answered nil, want it refused by name")
	}
	if !strings.Contains(refusal.Error(), sliceName) || !strings.Contains(refusal.Error(), "1 unfinished obligation(s)") {
		t.Fatalf("the refusal states %q, want it to name the slice %s and the 1 unfinished obligation(s)", refusal, sliceName)
	}
	if names := source.StateRegistry().Names(); !sliceContainsName(names, sliceName) {
		t.Fatalf("the registry names %v after the refused unmount, want the slice kept", names)
	}
	holder.open(laterItem)
	secondKey, filed, err := source.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the second cut = (%t, %v), want a filed cut", filed, err)
	}
	if secondKey == firstKey {
		t.Fatalf("the second cut names the key %s of the first, want a later cut", secondKey)
	}
	second := checkpointOfIdentity(t, ctx, source, secondKey)
	if payload := slicePayloadOf(t, second, sliceName); !strings.Contains(payload, owedItem) || !strings.Contains(payload, laterItem) {
		t.Fatalf("the second cut states %q for %s, want the obligation of the first cut beside the one the holder recorded after the refusal — a same-name replacement states neither", payload, sliceName)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(30 * time.Second)}); err != nil {
		t.Fatalf("close the live process: %v", err)
	}

	restored := &owedSlice{name: sliceName}
	var rebuiltUnmount func() error
	rebuilt := windowChainRun(t, ctx, storage, withBootConfigReconcile(func(ctx context.Context, run *Run, decl *config.Declaration, agents map[event.AgentID]NativeAgent) error {
		dispose, err := run.RegisterStateSlice(restored)
		if err != nil {
			return err
		}
		rebuiltUnmount = dispose
		_, err = run.Publish(ctx, decl, agents)
		return err
	}))
	rebuilt.stopCheckpointFiler()
	if held := restored.owes(); len(held) != 2 || held[0] != owedItem || held[1] != laterItem {
		t.Fatalf("the restart handed the slice %v, want the two obligations %v the cut states", held, []string{owedItem, laterItem})
	}
	thirdKey, filed, err := rebuilt.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut of the restarted process = (%t, %v), want a filed cut", filed, err)
	}
	third := checkpointOfIdentity(t, ctx, rebuilt, thirdKey)
	if payload := slicePayloadOf(t, third, sliceName); !strings.Contains(payload, owedItem) || !strings.Contains(payload, laterItem) {
		t.Fatalf("the restarted cut states %q for %s, want both obligations kept across the restart", payload, sliceName)
	}
	restored.settle()
	if err := rebuiltUnmount(); err != nil {
		t.Fatalf("the unmount of the settled slice answered %v, want it to leave", err)
	}
	if names := rebuilt.StateRegistry().Names(); sliceContainsName(names, sliceName) {
		t.Fatalf("the registry names %v after the settled unmount, want the slice gone", names)
	}
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

// TestPendingSliceIsRefusedByName pins the boot half of the lifecycle: a checkpoint stating a slice no holder mounts stops the boot by name.
//   - fixture: one process registers a slice stating an open obligation beside the framework's own and files the cut; a second boots the same root without it
//   - positive: the boot is refused by name, naming the slice the checkpoint states and no holder mounts
//   - negative: a boot that starts beside the unmounted slice reads work it does not hold, and reports the missing holder as nothing
//   - consumer: the Host boot entry and Run.RegisterStateSlice
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestPendingSliceIsRefusedByName(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	const sliceName = "assembly.pending-consumer"
	const owedItem = "the-notice-no-holder-mounts"
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	document := generationDocument(root, provider.URL+"/v1", "")
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	first, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the process that mounts the slice: %v", err)
	}
	holder := &owedSlice{name: sliceName}
	holder.open(owedItem)
	if _, err := first.Run().RegisterStateSlice(holder); err != nil {
		t.Fatalf("register the slice of the first process: %v", err)
	}
	commitSessionTurns(t, ctx, first.Run(), event.Address{AgentID: "user", SessionID: "session-pending"}, "pending", 1)
	key, filed, err := first.Run().Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut of the first process = (%t, %v), want a filed cut", filed, err)
	}
	cut := checkpointOfIdentity(t, ctx, first.Run(), key)
	if payload := slicePayloadOf(t, cut, sliceName); !strings.Contains(payload, owedItem) {
		t.Fatalf("the first cut states %q for %s, want the open obligation %s (the fixture states nothing, so the boot refusal proves nothing)", payload, sliceName, owedItem)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatalf("close the process that mounted the slice: %v", err)
	}

	second, err := Boot(configPath)
	if err == nil {
		closeErr := second.Close(ctx)
		t.Fatalf("the boot over a checkpoint naming %s answered success (close %v), want it refused by name", sliceName, closeErr)
	}
	if !strings.Contains(err.Error(), sliceName) || !strings.Contains(err.Error(), "no holder mounts") {
		t.Fatalf("the boot refusal states %q, want it to name the slice %s and the missing holder", err, sliceName)
	}
}

// redeliveredNoticeInstance is the runtime instance every process of the
// redelivery check freezes the notification's routing under.
const redeliveredNoticeInstance event.InstanceID = "instance-redelivered-settle"

// redeliveredNoticeOf names the one notification identity the redelivery check
// hands over: the first terminal notification of one round of one task, so every
// process of the check answers the same four-part identity.
func redeliveredNoticeOf(session string) task.Notice {
	return task.Notice{
		TaskID: event.TaskID("task-redelivered-" + session), Round: 1, Kind: task.NoticeSettled, Seq: 1,
		Event: event.EventID("fact-redelivered-" + session),
	}
}

// redeliveredNoticeAddr is the session address one notification of the check is
// routed to and committed under.
func redeliveredNoticeAddr(notice task.Notice) event.Address {
	return deliveryTestAddress("session-" + string(notice.TaskID))
}

// redeliveredNoticeStorage is the one storage root the processes of the
// redelivery check share, so each of them reads what the one before left.
func redeliveredNoticeStorage(root string) Option {
	return WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
}

// redeliveredNoticeFact renders the settle fact the follow-up turn of one
// notification reads, under the identity the settle pump looks it up by.
func redeliveredNoticeFact(addr event.Address, notice task.Notice) event.Event {
	return event.Event{
		Key:       settleDeliveryKey(notice),
		Type:      event.TypeTaskSettled,
		Timestamp: time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC),
		TaskID:    notice.TaskID,
		Meta: map[string]string{
			event.MetaKeyNoticeKind:   string(notice.Kind),
			event.MetaKeyTaskID:       string(notice.TaskID),
			event.MetaKeyEventSummary: string(notice.Kind),
		},
		Message: model.Message{Role: model.RoleUser, Content: "the settled task the follow-up turn answers"},
		Owner:   addr, Source: addr, Target: addr,
	}
}

// stopBesideTheCut runs the two processes of one cut over the storage root and answers the address the notification is routed to.
//   - the first process commits the settle fact and files the checkpoint the record lands past, so the cut states neither the finished task nor its notification and a boot reaches both by identity
//   - the record the stopped process leaves is the durable account's commitment: a settled task whose outbox states the notification, written beside the attempt of a turn that landed when the cut names one
func stopBesideTheCut(t *testing.T, ctx context.Context, root string, notice task.Notice, landedAttempt bool) event.Address {
	t.Helper()
	addr := redeliveredNoticeAddr(notice)
	source := noTrimChainRun(t, ctx, redeliveredNoticeStorage(root))
	source.stopCheckpointFiler()
	if _, err := source.CommitToSession(ctx, addr, redeliveredNoticeFact(addr, notice)); err != nil {
		t.Fatalf("commit the settle fact of %s: %v", notice.FactKey(), err)
	}
	cutKey, filed, err := source.Checkpoint(ctx)
	if err != nil || !filed || cutKey == "" {
		t.Fatalf("the source process filed checkpoint %q filed=%v err=%v, want the cut the record is left past", cutKey, filed, err)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the storage tier beside the cut: %v", err)
	}
	account := state.NewEmptyDurableCommit(ledger.TierFactLog(tier), nil)
	if err := account.Seat(ctx, nil, nil); err != nil {
		t.Fatalf("seat the account of the storage: %v", err)
	}
	if _, err := account.Commit(ctx, task.CommitRecord{
		MutationID: task.MutationID("redelivered:" + string(notice.TaskID)),
		TaskID:     notice.TaskID,
		Status:     task.StatusSettled,
		Outbox: []task.Envelope{{
			Notice:         notice,
			Target:         addr,
			Source:         addr,
			TargetInstance: redeliveredNoticeInstance,
		}},
	}); err != nil {
		t.Fatalf("commit the settled task whose outbox states %s: %v", notice.FactKey(), err)
	}
	if owed := account.OwedNotices(); len(owed) != 1 {
		t.Fatalf("the account past the cut owes %+v, want the one notification the settled task left", owed)
	}
	if landedAttempt {
		store := state.NewMemoryStore(state.NewAuthority(), account)
		if err := store.RecordAttempt(ctx, notice, state.SettleAttempt{Terminal: model.KindCompleted.String()}); err != nil {
			t.Fatalf("land the attempt of %s: %v", notice.FactKey(), err)
		}
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("release the storage tier beside the cut: %v", err)
	}
	return addr
}

// consumeTheOwedNotification lands the consumption receipt of one notification
// past the cut the last process filed, the way a stop after the delivery leaves
// it: the payload that cut wrote still names the notification as owed, and the
// receipt the account commits here is the only record answering it.
func consumeTheOwedNotification(t *testing.T, ctx context.Context, root string, notice task.Notice) {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the storage tier beside the receipt: %v", err)
	}
	account := state.NewEmptyDurableCommit(ledger.TierFactLog(tier), nil)
	if err := account.Seat(ctx, nil, nil); err != nil {
		t.Fatalf("seat the account of the storage: %v", err)
	}
	store := state.NewMemoryStore(state.NewAuthority(), account)
	addr := redeliveredNoticeAddr(notice)
	if _, err := store.Establish(ctx, addr, redeliveredNoticeInstance); err != nil {
		t.Fatalf("establish the target site of %s: %v", notice.FactKey(), err)
	}
	if err := store.TrackDelivery(ctx, notice.TaskID, task.HandoffTarget{Target: addr, TargetInstance: redeliveredNoticeInstance}); err != nil {
		t.Fatalf("register the delivery route of %s: %v", notice.TaskID, err)
	}
	if err := store.AcceptNotice(ctx, notice); err != nil {
		t.Fatalf("accept %s at its target: %v", notice.FactKey(), err)
	}
	seat, held := seatOfTask(store.Unconsumed(), notice.TaskID)
	if !held {
		t.Fatalf("the mailbox of the receipt states no entry for %s, want the one it accepted", notice.TaskID)
	}
	if err := store.Claim(ctx, seat.Address, seat.Instance, seat.AcceptSeq); err != nil {
		t.Fatalf("claim %s past the cut: %v", notice.FactKey(), err)
	}
	if err := store.Consume(ctx, seat.Address, seat.Instance, seat.AcceptSeq); err != nil {
		t.Fatalf("consume %s past the cut: %v", notice.FactKey(), err)
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("release the storage tier beside the receipt: %v", err)
	}
}

// stateTheOwedNotification runs one whole process over the root that restores the
// notification the record left owed and files the checkpoint stating it, so the
// next boot reads a cut that names a notification the account has already
// answered: the stale payload every receipt fence has to read past.
func stateTheOwedNotification(t *testing.T, ctx context.Context, root string, notice task.Notice) {
	t.Helper()
	stating := noTrimChainRun(t, ctx, redeliveredNoticeStorage(root))
	stating.stopCheckpointFiler()
	if _, held := seatOfTask(stating.store.Unconsumed(), notice.TaskID); !held {
		t.Fatalf("the process that states the cut owes %+v, want the one notification the record left", stating.store.Unconsumed())
	}
	cutKey, filed, err := stating.Checkpoint(ctx)
	if err != nil || !filed || cutKey == "" {
		t.Fatalf("the stating process filed checkpoint %q filed=%v err=%v, want the cut that names the owed notification", cutKey, filed, err)
	}
	if _, err := stating.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("close the stating process: %v", err)
	}
}

// bootBesideTheCut opens one more process over the same storage root and answers
// the run it assembled there beside the checkpoint the last process filed:
// nothing of a stopped process is rebuilt in this one, so every record it reads
// comes from the root.
func bootBesideTheCut(t *testing.T, ctx context.Context, root string) *Run {
	t.Helper()
	booted := noTrimChainRun(t, ctx, redeliveredNoticeStorage(root))
	booted.stopCheckpointFiler()
	if key, _ := booted.CheckpointFiled(); key == "" {
		t.Fatal("the boot read no checkpoint, want the cut the redelivered notification is restored beside")
	}
	return booted
}

// driveSettleTurns drives the real settle pump over the mailbox account one
// process restored and reports how many follow-up turns it fired. The pump reads
// the settle fact through the run's own committed-fact face and leaves the
// terminal on the account through the barrier the drive stamps, so a turn this
// counts is a turn the account answered and not one the check answered for it.
func driveSettleTurns(t *testing.T, run *Run, addr event.Address) int {
	t.Helper()
	var mu sync.Mutex
	turns := 0
	fire := func(ctx context.Context, _ event.Address, _ event.Event) error {
		mu.Lock()
		turns++
		mu.Unlock()
		barrier, held := settleBarrierFrom(ctx)
		if !held {
			return nil
		}
		return barrier.settle(ctx, model.KindCompleted)
	}
	driver := NewSettleDriver(run.CommittedEvent, func() SettleWake { return run.store }, fire)
	driver.DeclareScope(string(addr.AgentID), string(addr.AgentID))
	defer driver.Stop()
	driver.Wake()
	waitScopeGate(t, "the settle pump to rest beside the restored notification", func() bool {
		return len(run.store.Unconsumed()) == 0
	})
	mu.Lock()
	defer mu.Unlock()
	return turns
}

// consumeReceiptKeys reports the consumption receipts one root holds, read from
// the durable family the account commits them under rather than from the mailbox
// of any single process.
func consumeReceiptKeys(t *testing.T, ctx context.Context, tier *ledger.Tier) map[string][]byte {
	t.Helper()
	if tier == nil {
		t.Fatal("the run opened no storage tier, want the durable root the receipts are read from")
	}
	keys, err := ledger.TierFactLog(tier).Scan(ctx, "tagent/task/v1/consume:")
	if err != nil {
		t.Fatalf("read the consumption receipts of the root: %v", err)
	}
	return keys
}

// seatOfTask reports the mailbox entry one task waits under, so a check reads the
// acceptance sequence and the frozen routing the restore assigned.
func seatOfTask(seats []state.WorkSeat, taskID event.TaskID) (state.WorkSeat, bool) {
	for _, seat := range seats {
		if seat.Notice.TaskID == taskID {
			return seat, true
		}
	}
	return state.WorkSeat{}, false
}

// TestRedeliveredNoticeAfterRestartSettlesOnce pins the fences a restart reads before it drives one notification again.
//   - fixture: one real localfile root, an account left past the cut that states neither the finished task nor its notification
//   - fixture: a boot whose every record comes from that root, so no cross-process step is faked by a rebuild in the process that drives
//   - the account the boot seats states no task while the settle fact of the notification stays readable
//   - so a boot that drove nothing proves the account answered instead of proving there was no work
//   - cut_before_the_turn: the boot drives exactly one follow-up turn, and a third process that redelivers the same notification drives none
//   - cut_before_the_turn: the root still holds one receipt
//   - cut_before_the_receipt: the attempt of the turn that landed is read back by identity, so the boot drives no second turn
//   - cut_before_the_receipt: the boot writes the one receipt the stopped process never wrote
//   - redelivery_after_the_receipt: the receipt the cut states nowhere answers the redelivery, so the boot owes the notification nothing
//   - consumer: the real settle pump over the mailbox account the boot restored, reading the settle fact through Run.CommittedEvent
//
// 契约: docs/wiki/agent/agent-architecture.md#settle-driver
func TestRedeliveredNoticeAfterRestartSettlesOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	t.Run("cut_before_the_turn", func(t *testing.T) {
		root := t.TempDir()
		notice := redeliveredNoticeOf("before-turn")
		addr := stopBesideTheCut(t, ctx, root, notice, false)
		stateTheOwedNotification(t, ctx, root, notice)

		booted := bootBesideTheCut(t, ctx, root)
		restored, held := seatOfTask(booted.store.Unconsumed(), notice.TaskID)
		if !held {
			t.Fatalf("the booted mailbox owes %+v, want the notification the cut stated", booted.store.Unconsumed())
		}
		if restored.Address != addr {
			t.Fatalf("the restored seat routes to %v, want the frozen routing %v", restored.Address, addr)
		}
		if turns := driveSettleTurns(t, booted, restored.Address); turns != 1 {
			t.Fatalf("the boot drove %d follow-up turn(s) for the notification the cut left owed, want the one turn", turns)
		}
		if keys := consumeReceiptKeys(t, ctx, booted.tier); len(keys) != 1 {
			t.Fatalf("the root holds %d consumption receipt(s) after the turn, want one", len(keys))
		}
		if _, err := booted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
			t.Fatalf("close the process beside the turn: %v", err)
		}

		redelivered := bootBesideTheCut(t, ctx, root)
		if owed := redelivered.store.Unconsumed(); len(owed) != 0 {
			t.Fatalf("the third process owes %+v, want nothing: the receipt of the one turn answers the redelivery", owed)
		}
		if _, err := redelivered.CommittedEvent(ctx, settleDeliveryKey(notice)); err != nil {
			t.Fatalf("the settle fact of %s is unreadable beside the redelivery, so a turn that fired here would prove nothing: %v", notice.FactKey(), err)
		}
		if turns := driveSettleTurns(t, redelivered, addr); turns != 0 {
			t.Fatalf("the redelivery drove %d follow-up turn(s) for a notification the root already settled, want none", turns)
		}
		if keys := consumeReceiptKeys(t, ctx, redelivered.tier); len(keys) != 1 {
			t.Fatalf("the root holds %d consumption receipt(s) after the redelivery, want the one the first turn wrote", len(keys))
		}
		if _, err := redelivered.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
			t.Fatalf("close the redelivery process: %v", err)
		}
	})

	t.Run("cut_before_the_receipt", func(t *testing.T) {
		root := t.TempDir()
		notice := redeliveredNoticeOf("before-receipt")
		stopBesideTheCut(t, ctx, root, notice, true)
		stateTheOwedNotification(t, ctx, root, notice)

		booted := bootBesideTheCut(t, ctx, root)
		restored, held := seatOfTask(booted.store.Unconsumed(), notice.TaskID)
		if !held {
			t.Fatalf("the booted mailbox owes %+v, want the notification whose receipt the stop never wrote", booted.store.Unconsumed())
		}
		if _, err := booted.CommittedEvent(ctx, settleDeliveryKey(notice)); err != nil {
			t.Fatalf("the settle fact of %s is unreadable beside the boot, so a turn that fired here would prove nothing: %v", notice.FactKey(), err)
		}
		if turns := driveSettleTurns(t, booted, restored.Address); turns != 0 {
			t.Fatalf("the boot drove %d follow-up turn(s) for a notification whose attempt already landed, want none", turns)
		}
		if owed := booted.store.Unconsumed(); len(owed) != 0 {
			t.Fatalf("the booted mailbox still owes %+v, want the entry closed beside the receipt it wrote", owed)
		}
		if keys := consumeReceiptKeys(t, ctx, booted.tier); len(keys) != 1 {
			t.Fatalf("the root holds %d consumption receipt(s) after the boot, want the one receipt the landed attempt owes", len(keys))
		}
		if _, err := booted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
			t.Fatalf("close the process beside the receipt: %v", err)
		}
	})

	t.Run("redelivery_after_the_receipt", func(t *testing.T) {
		root := t.TempDir()
		notice := redeliveredNoticeOf("after-receipt")
		addr := stopBesideTheCut(t, ctx, root, notice, true)
		stateTheOwedNotification(t, ctx, root, notice)
		consumeTheOwedNotification(t, ctx, root, notice)

		booted := bootBesideTheCut(t, ctx, root)
		if owed := booted.store.Unconsumed(); len(owed) != 0 {
			t.Fatalf("the booted mailbox owes %+v, want nothing: the receipt landed past the cut answers the notification", owed)
		}
		if _, err := booted.CommittedEvent(ctx, settleDeliveryKey(notice)); err != nil {
			t.Fatalf("the settle fact of %s is unreadable beside the redelivery, so a turn that fired here would prove nothing: %v", notice.FactKey(), err)
		}
		if turns := driveSettleTurns(t, booted, addr); turns != 0 {
			t.Fatalf("the redelivery drove %d follow-up turn(s) for a notification the root already consumed, want none", turns)
		}
		if keys := consumeReceiptKeys(t, ctx, booted.tier); len(keys) != 1 {
			t.Fatalf("the root holds %d consumption receipt(s) after the redelivery, want the one the stopped process wrote", len(keys))
		}
		if _, err := booted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
			t.Fatalf("close the redelivery process: %v", err)
		}
	})
}

// refusalSliceName is the state slice every arm of
// TestCheckpointRefusalCostAcrossSessions mounts, so the arms differ by the refusal
// and never by the account a cut states.
const refusalSliceName = "assembly.refusal-cost"

// refusalArm names one condition an unapplied fact is measured under.
type refusalArm int

const (
	// noRefusal is the arm nothing refuses: the reading the other two are read beside.
	noRefusal refusalArm = iota
	// transientRefusal is the arm one refusal ends by the same identity landing again.
	transientRefusal
	// standingRefusal is the arm the refusal holds to the end of the arm.
	standingRefusal
)

// refusalArmName answers the arm as the readings name it.
func refusalArmName(arm refusalArm) string {
	switch arm {
	case noRefusal:
		return "no-refusal"
	case transientRefusal:
		return "transient-refusal"
	case standingRefusal:
		return "standing-refusal"
	default:
		return fmt.Sprintf("refusal-arm(%d)", int(arm))
	}
}

// refusalArmReading is what one arm measured: the cuts it attempted, the ones the
// refusal held over, and what the boot beside it read.
//   - history and journalBytes are the control of the comparison: the three arms commit the same facts, so a reading that differs between them differs by the refusal and not by the chain behind it
//   - decoded counts the bodies the boot beside the arm folded, valueReads the values the store answered it, and indexEntries plus the walk faces the resident index it published and what its commit-order walks named and read
//   - allocated is the bytes one assemble allocated, read from the runtime's own counter around the boot: it is reported beside the read faces and never asserted, because an allocation total moves with the collector's own work
type refusalArmReading struct {
	arm            refusalArm
	attempts       int
	refusals       int
	landed         int
	blocked        []event.SessionID
	blockedFor     time.Duration
	history        int
	journalBytes   int64
	decoded        int
	valueReads     int64
	indexEntries   int
	walkVisits     int
	walkValueReads int
	walkValueBytes int64
	allocated      uint64
}

// measureRefusalArm runs one arm over its own durable root and answers what it
// measured: the same commits in every arm, one cut of each session before the fact
// the consumer decides on, `rounds` cut attempts per session under the arm's
// refusal, and one boot beside the arm that files the read faces.
func measureRefusalArm(t *testing.T, ctx context.Context, arm refusalArm, rounds, turns int) refusalArmReading {
	t.Helper()
	name := refusalArmName(arm)
	reading := refusalArmReading{arm: arm}
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	named := event.Address{AgentID: "agent-a", SessionID: "session-refusal-named"}
	sessions := []event.Address{
		named,
		{AgentID: "agent-a", SessionID: "session-refusal-bystander"},
		{AgentID: "agent-a", SessionID: "session-refusal-third"},
	}
	refusedFact := retryFactOf("notice-refusal-cost", named)
	decided := refusedFact.Key
	if arm == noRefusal {
		decided = "notice-never-refused"
	}
	consumer := &refusingConsumer{foldRecorder: &foldRecorder{name: refusalSliceName}, refuse: decided}
	writer := wideWindowChainRun(t, ctx, storage, refusingBoot(consumer))
	writer.stopCheckpointFiler()

	for at, addr := range sessions {
		commitSessionTurns(t, ctx, writer, addr, fmt.Sprintf("arm-early-%d", at), 1)
		if key, filed, err := writer.CheckpointSession(ctx, addr); err != nil || !filed || key == "" {
			t.Fatalf("the %s arm files no cut of %v before the refusal = (%q, %t, %v), want it filed", name, addr, key, filed, err)
		}
	}
	if _, err := writer.CommitToSession(ctx, named, refusedFact); err != nil {
		t.Fatalf("the %s arm commits the fact the consumer decides on: %v", name, err)
	}
	for at, addr := range sessions {
		commitSessionTurns(t, ctx, writer, addr, fmt.Sprintf("arm-behind-%d", at), turns)
	}

	attempts := rounds
	if arm == noRefusal {
		attempts = 1
	}
	var firstRefusal time.Time
	for round := 0; round < attempts; round++ {
		for _, addr := range sessions {
			reading.attempts++
			key, filed, err := writer.CheckpointSession(ctx, addr)
			if arm == noRefusal {
				if err != nil || !filed || key == "" {
					t.Fatalf("the %s arm files the cut of %v = (%q, %t, %v), want the cut filed beside no refusal", name, addr, key, filed, err)
				}
				reading.landed++
				continue
			}
			if err == nil || filed {
				t.Fatalf("the %s arm files the cut of %v = (%q, %t), want it refused while a fact stands unapplied", name, addr, key, filed)
			}
			if !strings.Contains(err.Error(), string(refusedFact.Key)) || !strings.Contains(err.Error(), refusalSliceName) {
				t.Fatalf("the %s arm refuses the cut of %v as %q, want it to name %s and the slice %q", name, addr, err, refusedFact.Key, refusalSliceName)
			}
			reading.refusals++
			if firstRefusal.IsZero() {
				firstRefusal = time.Now()
			}
			if !slices.Contains(reading.blocked, addr.SessionID) {
				reading.blocked = append(reading.blocked, addr.SessionID)
			}
		}
	}
	if arm == transientRefusal {
		consumer.allow()
		if _, err := writer.CommitToSession(ctx, named, refusedFact); err != nil {
			t.Fatalf("the %s arm lands the refused identity again: %v", name, err)
		}
		for _, addr := range sessions {
			reading.attempts++
			key, filed, err := writer.CheckpointSession(ctx, addr)
			if err != nil || !filed || key == "" {
				t.Fatalf("the %s arm files no cut of %v after the same identity landed = (%q, %t, %v)", name, addr, key, filed, err)
			}
			reading.landed++
		}
		reading.blockedFor = time.Since(firstRefusal)
	}
	if _, err := writer.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(10 * time.Second)}); err != nil {
		if arm != standingRefusal || !strings.Contains(err.Error(), string(refusedFact.Key)) {
			t.Fatalf("the %s arm closes the writing process = %v, want the owed cut refused by name beside the standing arm alone", name, err)
		}
	}

	recovery, allocated := bootRefusalCost(t, ctx, storage, &foldRecorder{name: refusalSliceName})
	recovery.stopCheckpointFiler()
	reading.allocated = allocated
	reading.decoded = recovery.CommittedFacts()
	reading.history = recovery.tier.Segments.EventCount()
	reading.journalBytes = journalLogBytesOf(t, root)
	var err error
	if reading.valueReads, err = addressedValueReadsOf(recovery.tier); err != nil {
		t.Fatalf("the %s arm states the store reads of its boot: %v", name, err)
	}
	if reading.indexEntries, err = indexEntriesOf(recovery.tier); err != nil {
		t.Fatalf("the %s arm states the resident index of its boot: %v", name, err)
	}
	if reading.walkVisits, reading.walkValueBytes, reading.walkValueReads, err = indexWalkReadsOf(recovery.tier); err != nil {
		t.Fatalf("the %s arm states the walk reads of its boot: %v", name, err)
	}
	if _, err := recovery.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(10 * time.Second)}); err != nil {
		t.Fatalf("the %s arm closes the booted process: %v", name, err)
	}
	return reading
}

// bootRefusalCost assembles one arm's boot on the declaration wideWindowChainRun
// builds and answers the bytes the assemble allocated, so the three arms state
// their allocation beside their reads under one fixture.
func bootRefusalCost(t *testing.T, ctx context.Context, storage Option, recorder *foldRecorder) (*Run, uint64) {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 100000}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}}, WithGovernance(snapshotTestGovernance{}), storage, recordingBoot(recorder))
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("boot the arm over its root: %v", err)
	}
	return run, after.TotalAlloc - before.TotalAlloc
}

// TestCheckpointRefusalCostAcrossSessions pins what one unapplied fact costs the cuts beside it and what the boundary a boot restores reads.
//   - three arms over one fixture: no refusal, one refusal resolved by the same identity landing, and one refusal standing to the end of the arm
//   - the scope is the account, not the fact's session: a refusal holds the cut of every session the arm attempted
//   - the chain the refused fact names and the chains beside it are held too
//   - the two arms that filed their cut boot over one chain, and the arm whose refusal stood boots the shorter one
//   - because the cuts it never filed are absent from its log
//   - a resolved refusal pays the boot the reads of the arm that never refused
//   - a standing refusal pays more index walks and value reads over the shorter chain, which is what a cut that never landed leaves the next boot to replay
//   - control: every arm's boot answers store reads and holds events, so the counts state reads the store made and not a store nothing was committed to
//   - the boundary read costs one point read per survivor the restore path names, paid before the caller knows whether the range above the boundary is empty
//   - the steady boot folds nothing past the cut and still pays those point reads
//   - the survivors the chain never held answer no read, so the cost follows the survivors a payload names and no phantom key pays for them
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCheckpointRefusalCostAcrossSessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	const rounds, turns = 3, 2
	failureless := measureRefusalArm(t, ctx, noRefusal, rounds, turns)
	transient := measureRefusalArm(t, ctx, transientRefusal, rounds, turns)
	standing := measureRefusalArm(t, ctx, standingRefusal, rounds, turns)
	arms := []refusalArmReading{failureless, transient, standing}
	for _, arm := range arms {
		name := refusalArmName(arm.arm)
		if arm.history == 0 {
			t.Fatalf("the %s arm boots over a store holding no event, so the reads it reports state a store nothing was committed to", name)
		}
		if arm.journalBytes == 0 {
			t.Fatalf("the %s arm boots over no journal, so the log it opened states no history to read", name)
		}
		if arm.valueReads == 0 {
			t.Fatalf("the %s arm's boot answered no value read, so these counts do not state the store the chain is filed in", name)
		}
		t.Logf("%s: attempts=%d landed=%d refusals=%d blocked=%d blockedFor=%s history=%d journalBytes=%d decoded=%d valueReads=%d indexEntries=%d walkVisits=%d walkValueReads=%d walkValueBytes=%d allocated=%d",
			name, arm.attempts, arm.landed, arm.refusals, len(arm.blocked), arm.blockedFor, arm.history, arm.journalBytes,
			arm.decoded, arm.valueReads, arm.indexEntries, arm.walkVisits, arm.walkValueReads, arm.walkValueBytes, arm.allocated)
	}
	if failureless.history != transient.history {
		t.Fatalf("the no-refusal arm boots over %d event(s) and the transient arm over %d, want one chain: both filed the cuts they attempted",
			failureless.history, transient.history)
	}
	if standing.history >= failureless.history {
		t.Fatalf("the standing arm boots over %d event(s) beside the %d of the no-refusal arm, want the shorter chain: the cuts it never filed are the facts its refusals keep out of the log",
			standing.history, failureless.history)
	}
	if failureless.refusals != 0 || failureless.landed != failureless.attempts {
		t.Fatalf("the no-refusal arm refused %d cut(s) and landed %d of %d, want every attempted cut filed", failureless.refusals, failureless.landed, failureless.attempts)
	}
	for _, arm := range arms[1:] {
		name := refusalArmName(arm.arm)
		if arm.refusals != rounds*3 {
			t.Fatalf("the %s arm refused %d cut(s) of the %d the fixture attempted", name, arm.refusals, rounds*3)
		}
		if len(arm.blocked) != 3 {
			t.Fatalf("the %s arm held the cut of %v, want the three sessions it attempted: the refusal is held by the account and not by the chain the fact names", name, arm.blocked)
		}
	}
	if transient.landed != 3 {
		t.Fatalf("the transient arm landed %d cut(s) after the same identity landed, want the %d it attempted", transient.landed, 3)
	}
	if standing.landed != 0 {
		t.Fatalf("the standing arm landed %d cut(s), want none while the fact stands unapplied", standing.landed)
	}
	blockage := 3*turns*4 + 1
	if standing.decoded < blockage {
		t.Fatalf("the boot beside the standing refusal folded %d fact(s), want the %d the blockage committed under a cut that never landed", standing.decoded, blockage)
	}
	if transient.valueReads != failureless.valueReads || transient.walkVisits != failureless.walkVisits {
		t.Fatalf("the boot beside the transient refusal read %d value(s) in %d walk visit(s) beside the %d and %d of the no-refusal arm, want the reads of the cut that landed",
			transient.valueReads, transient.walkVisits, failureless.valueReads, failureless.walkVisits)
	}
	if standing.valueReads <= failureless.valueReads || standing.walkVisits <= failureless.walkVisits {
		t.Fatalf("the boot beside the standing refusal read %d value(s) in %d walk visit(s) beside the %d and %d of the no-refusal arm, want the replay of the window the refusal left open",
			standing.valueReads, standing.walkVisits, failureless.valueReads, failureless.walkVisits)
	}

	t.Run("boundary_reads", func(t *testing.T) {
		root := t.TempDir()
		storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
		addr := event.Address{AgentID: "agent-a", SessionID: "session-refusal-boundary"}
		writer := wideWindowChainRun(t, ctx, storage, refusingBoot(&refusingConsumer{foldRecorder: &foldRecorder{name: refusalSliceName}, refuse: "notice-never-decided"}))
		writer.stopCheckpointFiler()
		commitSessionTurns(t, ctx, writer, addr, "boundary", 2)
		if key, filed, err := writer.Checkpoint(ctx); err != nil || !filed || key == "" {
			t.Fatalf("file the cut at the log's end = (%q, %t, %v), want the steady state the boundary is read in", key, filed, err)
		}
		if _, err := writer.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(10 * time.Second)}); err != nil {
			t.Fatalf("close the writing process: %v", err)
		}

		recovery, _ := bootRefusalCost(t, ctx, storage, &foldRecorder{name: refusalSliceName})
		recovery.stopCheckpointFiler()
		presented := projectionKeysOf(t, recovery, addr)
		if len(presented) < 3 {
			t.Fatalf("the chain presents %d entr(ies), want the survivors the fixture committed to read the boundary over", len(presented))
		}
		survivors := make([]anchorEntry, 0, 3)
		for _, key := range presented[len(presented)-3:] {
			survivors = append(survivors, anchorEntry{Key: key})
		}
		partition := memory.PartitionIDFromName(string(addr.AgentID))
		warmReads, positions, newest, err := measureBoundaryReads(t, ctx, recovery, recovery.tier, survivors, partition)
		if err != nil {
			t.Fatalf("read the boundary of the survivors the payload names on the boot's own view: %v", err)
		}
		if len(positions) != len(survivors) {
			t.Fatalf("the boundary named %d survivor(s) of the %d the payload states, so the store does not hold the chain these reads are counted over", len(positions), len(survivors))
		}
		reader, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
		if err != nil {
			t.Fatalf("open the cold view a boot reads its boundary on: %v", err)
		}
		coldReads, coldPositions, coldNewest, err := measureBoundaryReads(t, ctx, recovery, reader, survivors, partition)
		if err != nil {
			t.Fatalf("read the boundary of the survivors the payload names on a cold view: %v", err)
		}
		if len(coldPositions) != len(survivors) || coldNewest != newest {
			t.Fatalf("the cold view named %d survivor(s) at seq %d beside the %d at seq %d the boot's view named, want the same chain read twice",
				len(coldPositions), coldNewest, len(positions), newest)
		}
		if coldReads < int64(len(survivors)) {
			t.Fatalf("the boundary read %d value(s) for %d survivor(s), want at least one point read per survivor the payload names", coldReads, len(survivors))
		}
		if warmReads > coldReads {
			t.Fatalf("the boundary read %d value(s) on the boot's view beside the %d of a cold one, want the resident entries the boot already read to cost no more", warmReads, coldReads)
		}
		_, newestSeq := seqOfFact(t, recovery, presented[len(presented)-1])
		if newest != newestSeq {
			t.Fatalf("the boundary stands at seq %d, want the %d of the newest survivor: the range above it is the one the steady cut leaves empty", newest, newestSeq)
		}
		if folded := recovery.CommittedFacts(); folded != len(presented) {
			t.Fatalf("the steady boot folded %d fact(s) of the %d the chain presents, want the survivors alone: the cut stands at the log's end, so the range above the boundary is empty and the boundary reads were paid anyway", folded, len(presented))
		}

		absented := []anchorEntry{{Key: "boundary-absent-0"}, {Key: "boundary-absent-1"}, {Key: "boundary-absent-2"}}
		absentReads, absentPositions, absentNewest, err := measureBoundaryReads(t, ctx, recovery, reader, absented, partition)
		if err != nil {
			t.Fatalf("read the boundary of the survivors the chain never held: %v", err)
		}
		if len(absentPositions) != 0 || absentNewest != 0 {
			t.Fatalf("the boundary named %d survivor(s) and stood at seq %d for keys the chain never held, want none and the unnamed boundary 0", len(absentPositions), absentNewest)
		}
		if absentReads != 0 {
			t.Fatalf("the boundary read %d value(s) for keys the chain never held, want none: a point read the store answers no entry for reads no value", absentReads)
		}
		t.Logf("boundary: survivors=%d coldReads=%d warmReads=%d newest=%d absentReads=%d history=%d bootValueReads=%d decoded=%d",
			len(survivors), coldReads, warmReads, newest, absentReads, recovery.tier.Segments.EventCount(), mustStoreReads(t, recovery), recovery.CommittedFacts())
		if err := reader.Close(); err != nil {
			t.Fatalf("close the cold view: %v", err)
		}
		if _, err := recovery.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(10 * time.Second)}); err != nil {
			t.Fatalf("close the booted process: %v", err)
		}
	})
}

// measureBoundaryReads answers what one boundary read costs: the value spans the
// store of view answered while r.survivorPositions named the survivors, the
// positions it answered and the newest sequence it named.
//   - The count is that view's own value read measure taken around the call, so a survivor the view answers states a read and a survivor it answers no entry for states none.
//   - A view that already read these entries serves them from its resident set, so the same call on the boot's own view beside a cold one is what states the hot layer apart from the store.
func measureBoundaryReads(t *testing.T, ctx context.Context, run *Run, view *ledger.Tier, survivors []anchorEntry, partition int) (int64, map[event.EventID]int64, int64, error) {
	t.Helper()
	before, err := addressedValueReadsOf(view)
	if err != nil {
		return 0, nil, 0, err
	}
	positions, newest, err := run.survivorPositions(ctx, view, survivors, partition)
	if err != nil {
		return 0, nil, 0, err
	}
	after, err := addressedValueReadsOf(view)
	if err != nil {
		return 0, nil, 0, err
	}
	return after - before, positions, newest, nil
}

// mustStoreReads states the value reads one boot's store answered, and fails the
// check beside a tier that keeps no such measure instead of answering a zero.
func mustStoreReads(t *testing.T, run *Run) int64 {
	t.Helper()
	reads, err := addressedValueReadsOf(run.tier)
	if err != nil {
		t.Fatalf("state the store reads of the boot: %v", err)
	}
	return reads
}
