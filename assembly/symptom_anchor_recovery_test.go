// 契约: docs/wiki/memory/memory-architecture.md#overview
package assembly

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// bootQuantities is the four real quantities one boot of the recovery fixture
// pays, and the four faces a bounded recovery is stated against:
//   - journalBytes is the journal the open replayed, read from the engine's own open measure: never the depth the log grew to, which is a property of the writes and not of the boot
//   - indexEntries is the resident index the load published, read from the store: never EventCount, which is the whole committed history and answers the same number for a bounded load as for an unbounded one
//   - durableLoadCount is the durable prefix keys the task load scanned, read from the load stats the commit already keeps
//   - wallClock is the time the assemble that did all three took
//   - indexWalkVisits and indexWalkValueBytes are the fourth face: the entries the boot's commit-order walks named and the value bytes they read to name them, which is what tells a walk reading a value per held key apart from one reading the index family alone
//   - integrityBytesScanned is the log span the open's integrity pass verified, read from the engine's own measure: the whole log whatever the watermark states, the declared intrinsic O(history) of the open, never the replay window journalBytes states
//   - slotTableBytesScanned is the slot table bytes the boot's enumerations read, read from the engine's own measure: a boot that addresses its own keys reads none, and a boot that enumerates pays the table capacity whatever the prefix answers
//   - addressedValueReads is the values the boot read back by address, read from the engine's own measure: one per key a lookup answers, so a boot that walks a chain's every fact pays the chain, and a boot that reads only what it serves pays the keys it serves
//
// None of them is a decoded-body count: a body count states how much material a
// rebuild presented, never how much the boot had to read to present it.
type bootQuantities struct {
	journalBytes          int64
	indexEntries          int
	durableLoadCount      int
	wallClock             time.Duration
	indexWalkVisits       int
	indexWalkValueBytes   int64
	indexWalkValueReads   int
	integrityBytesScanned int64
	slotTableBytesScanned int64
	addressedValueReads   int64
}

// bootQuantitySeries renders the four quantities of every generation beside the
// wall clock of the generation each boot opened, so a failed bound names the whole
// series beside the quantity that broke it.
//   - the account is rendered as the count each generation seats, and the history beside it as the one count of the whole chain, so a bounded account is read beside the history it leaves behind
func bootQuantitySeries(observed []generationBoot, history []event.TaskID) string {
	journal := make([]int64, 0, len(observed))
	index := make([]int, 0, len(observed))
	durable := make([]int, 0, len(observed))
	wall := make([]time.Duration, 0, len(observed))
	generation := make([]time.Duration, 0, len(observed))
	account := make([]int, 0, len(observed))
	log := make([]int64, 0, len(observed))
	visits := make([]int, 0, len(observed))
	valueBytes := make([]int64, 0, len(observed))
	valueReads := make([]int, 0, len(observed))
	integrity := make([]int64, 0, len(observed))
	slotScan := make([]int64, 0, len(observed))
	addressed := make([]int64, 0, len(observed))
	for _, entry := range observed {
		journal = append(journal, entry.quantities.journalBytes)
		index = append(index, entry.quantities.indexEntries)
		durable = append(durable, entry.quantities.durableLoadCount)
		wall = append(wall, entry.quantities.wallClock)
		generation = append(generation, entry.generationWallClock)
		account = append(account, len(entry.accounted))
		log = append(log, entry.journalLogBytes)
		visits = append(visits, entry.quantities.indexWalkVisits)
		valueBytes = append(valueBytes, entry.quantities.indexWalkValueBytes)
		valueReads = append(valueReads, entry.quantities.indexWalkValueReads)
		integrity = append(integrity, entry.quantities.integrityBytesScanned)
		slotScan = append(slotScan, entry.quantities.slotTableBytesScanned)
		addressed = append(addressed, entry.quantities.addressedValueReads)
	}
	return fmt.Sprintf("journalBytes=%v indexEntries=%v durableLoadCount=%v wallClock=%v generationWallClock=%v durableAccount=%v historyTasks=%d journalLogBytes=%v indexWalkVisits=%v indexWalkValueBytes=%v indexWalkValueReads=%v integrityBytesScanned=%v slotTableBytesScanned=%v addressedValueReads=%v",
		journal, index, durable, wall, generation, account, len(history), log, visits, valueBytes, valueReads, integrity, slotScan, addressed)
}

// generationBoot is what one generation of the boot fixture measured before it
// worked: the four quantities its boot paid, the chain index the boot opened over
// as the denominator those quantities are held against, the tasks it settled
// through the registry and the tasks the account it seated holds.
//   - generationWallClock is the time the whole generation took: its boot beside the turns it committed, the tasks it settled, the checkpoints it filed and the process it closed
//   - the reads a growing history charges are paid on that span as well as at the boot, because a commit reads the journal to answer a duplicate
//   - journalLogBytes is the log the boot opened over, read from the file itself before the boot opens it, so the boot's own append is never the history it faced
//   - that length is what states the history a boot faces: a settled root states a watermark at the log's end, so a correct boot reads none of those bytes
//   - accounted is the tasks the account seats at that generation: the open work the checkpoint states beside the tasks its cut left past
//   - the tasks the generations before it settled stand on their chains and no account holds them
//   - so this set never grows with the history, and HistoryTaskIDs is the read that names those tasks
type generationBoot struct {
	quantities          bootQuantities
	generationWallClock time.Duration
	journalLogBytes     int64
	indexed             int
	settled             []event.TaskID
	accounted           []event.TaskID
}

// journalBytesOf answers the journal bytes one boot's open replayed, read from the
// engine's own measure through the structural interface it states. An engine that
// keeps no journal answers the named failure, never a zero a flat bound would read
// as a boot that read nothing.
func journalBytesOf(tier *ledger.Tier) (int64, error) {
	if tier == nil {
		return 0, fmt.Errorf("the boot holds no storage tier, so the probe read no journal byte of the boot")
	}
	probe, held := tier.Store.(interface{ JournalBytesOpen() int64 })
	if !held {
		return 0, fmt.Errorf("the tier holds the engine %T, which states no journal open measure, so the probe read no journal byte of the boot", tier.Store)
	}
	return probe.JournalBytesOpen(), nil
}

// indexEntriesOf answers the index entries one boot's load enumerated.
func indexEntriesOf(tier *ledger.Tier) (int, error) {
	if tier == nil || tier.Segments == nil {
		return 0, fmt.Errorf("the boot holds no segment store, so the probe read no entry of the resident index")
	}
	return tier.Segments.IndexEntriesRead(), nil
}

// integrityBytesScannedOf answers the log span one boot's open verified, read from
// the engine's own measure through the structural interface it states. An engine
// that keeps no journal answers the named failure, never a zero a linear reading
// would read as a pass that verified nothing.
func integrityBytesScannedOf(tier *ledger.Tier) (int64, error) {
	if tier == nil {
		return 0, fmt.Errorf("the boot holds no storage tier, so the probe read no integrity byte of the boot")
	}
	probe, held := tier.Store.(interface{ IntegrityBytesScanned() int64 })
	if !held {
		return 0, fmt.Errorf("the tier holds the engine %T, which states no integrity scan measure, so the probe read no integrity byte of the boot", tier.Store)
	}
	return probe.IntegrityBytesScanned(), nil
}

// slotTableBytesScannedOf answers the slot table bytes one boot's enumerations
// read, read from the engine's own measure through the structural interface it
// states. An engine that keeps no slot table answers the named failure, never a
// zero a flat bound would read as a boot that enumerated nothing.
func slotTableBytesScannedOf(tier *ledger.Tier) (int64, error) {
	if tier == nil {
		return 0, fmt.Errorf("the boot holds no storage tier, so the probe read no slot scan byte of the boot")
	}
	probe, held := tier.Store.(interface{ SlotTableBytesScanned() int64 })
	if !held {
		return 0, fmt.Errorf("the tier holds the engine %T, which states no slot scan measure, so the probe read no slot scan byte of the boot", tier.Store)
	}
	return probe.SlotTableBytesScanned(), nil
}

// indexWalkReadsOf answers what the commit-order walks of one boot cost: the index
// entries they named, the value bytes they read to name them and how many values
// those bytes were.
func indexWalkReadsOf(tier *ledger.Tier) (int, int64, int, error) {
	if tier == nil || tier.Segments == nil {
		return 0, 0, 0, fmt.Errorf("the boot holds no segment store, so the probe read no walk of the index")
	}
	visits, valueBytes, valueReads := tier.Segments.IndexWalkReads()
	return visits, valueBytes, valueReads, nil
}

// addressedValueReadsOf answers the values one boot read back by address, read from
// the engine's own measure through the structural interface it states. An engine
// that keeps no addressable log answers the named failure, never a zero a flat
// bound would read as a boot that read no value.
func addressedValueReadsOf(tier *ledger.Tier) (int64, error) {
	if tier == nil {
		return 0, fmt.Errorf("the boot holds no storage tier, so the probe read no addressed value of the boot")
	}
	probe, held := tier.Store.(interface{ ValueReads() int64 })
	if !held {
		return 0, fmt.Errorf("the tier holds the engine %T, which states no value read measure, so the probe read no addressed value of the boot", tier.Store)
	}
	return probe.ValueReads(), nil
}

// durableLoadCountOf answers the durable prefix keys one boot's task load scanned,
// read from the load stats the commit already keeps. A commit that keeps no
// durable record answers the named failure instead of a zero.
func durableLoadCountOf(commit task.Commit) (int, error) {
	durable, held := commit.(*state.DurableCommit)
	if !held {
		return 0, fmt.Errorf("the booted run holds the commit %T, which answers no load stats, so the probe read no durable load scan", commit)
	}
	count := 0
	for _, scan := range durable.LoadStats().Scans {
		count += scan.Keys
	}
	return count, nil
}

// journalLogBytesOf answers the journal bytes one durable root holds, which is the
// history a boot opens over: a root that grew states a longer log whether or not the
// open read any of it. A root no writer has created holds no log and answers zero.
func journalLogBytesOf(t *testing.T, root string) int64 {
	t.Helper()
	stat, err := os.Stat(filepath.Join(root, "kv", "journal.log"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("state the journal of %s: %v", root, err)
	}
	return stat.Size()
}

// judgeSeatedHistory answers why the history a boot could name its tasks from is
// no evidence of a bounded recovery, and nil for a chain that holds tasks.
//   - A boot that pays no durable prefix key is the bound this domain states, and that zero is a claim about recovery only beside a chain something was seated on: over a store nothing was seated on the same zero states that there was nothing to read.
//   - The history is the read that answers it, because the tasks a boot settled before its newest cut are named on their chains and by no account.
func judgeSeatedHistory(held int, where string) error {
	if held == 0 {
		return fmt.Errorf("the chain holds no task %s, so a boot that reads no durable prefix key is a store nothing was seated over, never a recovery that stayed bounded", where)
	}
	return nil
}

// measureBootQuantities measures the four quantities of one booted run: the three
// reads the boot paid beside the wall clock the fixture took around the assemble
// that produced it. Every face answers the failure of the probe that could not
// read it, so a zero never stands for a boot that paid nothing.
func measureBootQuantities(tier *ledger.Tier, commit task.Commit, wallClock time.Duration) (bootQuantities, error) {
	quantities := bootQuantities{wallClock: wallClock}
	journal, err := journalBytesOf(tier)
	if err != nil {
		return bootQuantities{}, err
	}
	index, err := indexEntriesOf(tier)
	if err != nil {
		return bootQuantities{}, err
	}
	durable, err := durableLoadCountOf(commit)
	if err != nil {
		return bootQuantities{}, err
	}
	visits, valueBytes, valueReads, err := indexWalkReadsOf(tier)
	if err != nil {
		return bootQuantities{}, err
	}
	integrity, err := integrityBytesScannedOf(tier)
	if err != nil {
		return bootQuantities{}, err
	}
	slotScan, err := slotTableBytesScannedOf(tier)
	if err != nil {
		return bootQuantities{}, err
	}
	addressed, err := addressedValueReadsOf(tier)
	if err != nil {
		return bootQuantities{}, err
	}
	quantities.journalBytes = journal
	quantities.indexEntries = index
	quantities.durableLoadCount = durable
	quantities.indexWalkVisits = visits
	quantities.indexWalkValueBytes = valueBytes
	quantities.indexWalkValueReads = valueReads
	quantities.integrityBytesScanned = integrity
	quantities.slotTableBytesScanned = slotScan
	quantities.addressedValueReads = addressed
	return quantities, nil
}

// judgeBootFixture answers why the boot fixture proves nothing, and nil beside a
// fixture that settled its tasks across every generation over a chain that grew.
// The judgements are the fixture's own: a flat boot quantity is only a claim about
// recovery if the tasks the boot reads really accumulated and the chain behind them
// really grew.
//   - The durable face is judged where a boot pays it, and a boot pays the durable families of the tasks a checkpoint does not cover: this fixture files its checkpoint last, so the account it restores is stated whole by that checkpoint and costs no durable prefix key. That is the bound this domain states, so the fixture judges the probes that must pay every generation — the journal tail and the resident index — and leaves the durable face to the anchors that settle tasks past a cut (TestTaskRestoreBoundedByCheckpoint).
//   - The history that face is judged over holds tasks at every generation (judgeSeatedHistory): a boot that pays no durable prefix key is the bound, and that zero is a claim about recovery only beside a chain something was seated on.
//   - the account each generation seats holds the tasks that generation settled and none the generations before it settled, because a task settled before the newest cut stands on its chain alone
//   - the chain holds every task the fixture settled, read through HistoryTaskIDs, so a task no account seats is still stated and read back by identity
//   - the history the boot faces is judged by the length of the log, never by the journal bytes its open read
//   - a settled root states a watermark at the log's end, so a correct open reads none of those bytes
//   - the log's own length is what states there was something behind it
func judgeBootFixture(observed []generationBoot, history []event.TaskID, generations, perGeneration int) error {
	if len(observed) != generations {
		return fmt.Errorf("the fixture booted %d generation(s), want the %d it was asked for", len(observed), generations)
	}
	seen := make(map[event.TaskID]int, generations*perGeneration)
	for generation, entry := range observed {
		if len(entry.settled) == 0 {
			return fmt.Errorf("the fixture settled no task, so the durable load face is not measured (generation %d)", generation)
		}
		if len(entry.settled) != perGeneration {
			return fmt.Errorf("generation %d settled %d task(s), want the %d the fixture admits, so the durable load face is measured", generation, len(entry.settled), perGeneration)
		}
		if !sameTaskSet(entry.accounted, entry.settled) {
			return fmt.Errorf("generation %d seats %d task(s), want the %d it settled and none the generations before it settled: the account is the open work a checkpoint states beside the tasks its cut left past",
				generation, len(entry.accounted), len(entry.settled))
		}
		for _, taskID := range entry.settled {
			if prior, held := seen[taskID]; held {
				return fmt.Errorf("generation %d settled the task %s generation %d already settled, want the fixture to settle a new task rather than rewrite one", generation, taskID, prior)
			}
			seen[taskID] = generation
		}
	}
	if want := generations * perGeneration; len(seen) != want {
		return fmt.Errorf("the fixture settled %d distinct task(s), want the %d it admits", len(seen), want)
	}
	if len(history) != len(seen) {
		return fmt.Errorf("the chain holds %d task(s), want the %d the fixture settled: a task the account leaves behind is stated on its chain and read back by identity", len(history), len(seen))
	}
	if seated := len(observed[generations-1].accounted); len(history) <= seated {
		return fmt.Errorf("the chain holds %d task(s) beside the %d the last boot seats, want a history that grew past the account so a bounded account is a claim about recovery", len(history), seated)
	}
	if observed[generations-1].journalLogBytes == 0 {
		return fmt.Errorf("the log of the last generation holds no byte, so the window its open leaves unstated is measured over no history and the boot quantities claim nothing about recovery")
	}
	if last, second := observed[generations-1].journalLogBytes, observed[1].journalLogBytes; last <= second {
		return fmt.Errorf("the log grew from %d to %d byte(s) between the second generation and the last, want a log that grew so the window a boot leaves unstated is held beside a history that grew behind it", second, last)
	}
	if last, second := observed[generations-1].indexed, observed[1].indexed; last <= second {
		return fmt.Errorf("the chain indexed %d facts at the last generation and %d at the second, want a chain that grew so the boot quantities are held against a growing history", last, second)
	}
	return nil
}

// sameTaskSet reports whether two statements of task identities name the same
// tasks, with the order they were stated in left outside the comparison: an
// account is read in identity order and a settle answers in admission order.
func sameTaskSet(left, right []event.TaskID) bool {
	if len(left) != len(right) {
		return false
	}
	one := append([]event.TaskID(nil), left...)
	two := append([]event.TaskID(nil), right...)
	sort.Slice(one, func(i, j int) bool { return one[i] < one[j] })
	sort.Slice(two, func(i, j int) bool { return two[i] < two[j] })
	for at := range one {
		if one[at] != two[at] {
			return false
		}
	}
	return true
}

// storelessBootRun assembles one run beside no storage tier, so the boot holds the
// in-memory commit and the engine that keeps no journal: the pair a probe that
// reads no quantity is stated against.
func storelessBootRun(t *testing.T, ctx context.Context) *Run {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	run, err := assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}}, false, WithGovernance(snapshotTestGovernance{}))
	if err != nil {
		t.Fatalf("assemble the run beside no storage tier: %v", err)
	}
	return run
}

// walkBootGenerations boots the named number of independent chains of one shape,
// each a durable root of its own under root, the named number of generations. Each
// generation measures what its boot paid, then commits the turns of the session,
// settles the tasks through the registry, files the session and the framework
// checkpoints and closes the process, so the next generation opens over a storage
// that grew by one generation of facts and tasks.
//   - the wall clock of a generation is the middle of the trials' generations, because one generation caught by a scheduler hiccup states neither the cost of a generation at that depth nor the growth between two depths
//   - a generation is timed from the assemble that boots it to the close that ends it, so the span judges the boot beside the commits which read the journal
//   - a bound over the boot alone reads the axis the boot bounded rather than the cost it carries
//   - the three read quantities are the boot of the first trial, which states the same number on every chain of the shape
//   - the history is read once after the walk, over the root its last generation closed, so the read that names every settled task is never paid inside a generation a bound judges
func walkBootGenerations(t *testing.T, ctx context.Context, root string, addr event.Address, trials, generations, turns, perGeneration int) ([]generationBoot, []event.TaskID) {
	t.Helper()
	observed := make([]generationBoot, generations)
	boots := make([][]time.Duration, generations)
	walked := make([][]time.Duration, generations)
	for trial := 0; trial < trials; trial++ {
		durableRoot := filepath.Join(root, fmt.Sprintf("trial-%d", trial))
		storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: durableRoot})
		for generation := 0; generation < generations; generation++ {
			logBytes := journalLogBytesOf(t, durableRoot)
			started := time.Now()
			boot := noTrimChainRun(t, ctx, storage)
			wallClock := time.Since(started)
			boot.stopCheckpointFiler()
			quantities, err := measureBootQuantities(boot.tier, boot.commit.inner, wallClock)
			if err != nil {
				t.Fatalf("measure the quantities of the boot of generation %d of trial %d: %v", generation, trial, err)
			}
			entry := generationBoot{quantities: quantities, journalLogBytes: logBytes, indexed: chainIndexSize(t, ctx, boot)}
			commitSessionTurns(t, ctx, boot, addr, fmt.Sprintf("gen-%d", generation), turns)
			entry.settled = settleTasks(t, ctx, boot, taskCheckpointCalls(fmt.Sprintf("gen-%d", generation), perGeneration))
			if durable, held := boot.commit.inner.(*state.DurableCommit); held {
				entry.accounted = durable.TaskIDs()
			}
			if _, _, err := boot.CheckpointSession(ctx, addr); err != nil {
				t.Fatalf("file the session checkpoint of generation %d of trial %d: %v", generation, trial, err)
			}
			if _, _, err := boot.Checkpoint(ctx); err != nil {
				t.Fatalf("file the framework checkpoint of generation %d of trial %d: %v", generation, trial, err)
			}
			if _, err := boot.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
				t.Fatalf("close the process of generation %d of trial %d: %v", generation, trial, err)
			}
			entry.generationWallClock = time.Since(started)
			boots[generation] = append(boots[generation], wallClock)
			walked[generation] = append(walked[generation], entry.generationWallClock)
			if trial == 0 {
				observed[generation] = entry
			}
		}
	}
	for generation := range observed {
		observed[generation].quantities.wallClock = medianDuration(boots[generation])
		observed[generation].generationWallClock = medianDuration(walked[generation])
	}
	closed := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: filepath.Join(root, "trial-0")})
	return observed, historyTasksOf(t, ctx, closed)
}

// historyTasksOf answers every task identity the chain of one root states, read
// through the face a boot seats and then names the whole history through. The boot
// it takes is measured by no bound: the read is O(history) and the price of naming
// the tasks an account leaves behind.
func historyTasksOf(t *testing.T, ctx context.Context, storage Option) []event.TaskID {
	t.Helper()
	boot := noTrimChainRun(t, ctx, storage)
	boot.stopCheckpointFiler()
	durable, held := boot.commit.inner.(*state.DurableCommit)
	if !held {
		t.Fatalf("the boot that reads the history holds the commit %T, which keeps no durable record to name", boot.commit.inner)
	}
	history, err := durable.HistoryTaskIDs(ctx)
	if err != nil {
		t.Fatalf("read the history of the chain by identity: %v", err)
	}
	if _, err := boot.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the boot that read the history: %v", err)
	}
	return history
}

// medianDuration answers the middle of the measured wall clocks, so the growth an
// anchor judges is the cost of the walk rather than the slowest hiccup a scheduler
// dealt to one of its steps.
func medianDuration(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered[len(ordered)/2]
}

// TestBootFixtureGrowsTasksAcrossGenerations pins the fixture the boot anchors are measured on.
//   - every generation settles its tasks through the registry and the chain keeps every task the fixture ever settled
//   - the account a generation seats holds the tasks that generation settled and none the generations before it settled
//   - the task identities of one generation never repeat another's, so the chain is a growing history rather than one task rewritten
//   - the durable load face is measured at all: a boot that scanned no durable prefix key fails by name
//   - the chain index grows across the generations, the denominator the boot quantities are held against
//   - an anchor that reads flat therefore never reads flat because the fixture did nothing
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestBootFixtureGrowsTasksAcrossGenerations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-boot-fixture"}
	const trials, generations, turns, perGeneration = 1, 5, 6, 4
	observed, history := walkBootGenerations(t, ctx, t.TempDir(), addr, trials, generations, turns, perGeneration)
	t.Logf("the fixture booted %d generations over %s", generations, bootQuantitySeries(observed, history))
	if err := judgeBootFixture(observed, history, generations, perGeneration); err != nil {
		t.Fatalf("the boot fixture proves nothing: %v", err)
	}
	quiet := make([]generationBoot, generations)
	for at := range quiet {
		quiet[at] = generationBoot{}
	}
	if err := judgeBootFixture(quiet, nil, generations, perGeneration); err == nil {
		t.Fatal("the fixture judgement accepted a fixture that settled no task, want it refused by name, so the anchors measured over this fixture are never green because nothing happened")
	} else if !strings.Contains(err.Error(), "the fixture settled no task") {
		t.Fatalf("the fixture judgement refused the empty fixture with %v, want the settled count named", err)
	}
}

// TestBootQuantityProbesReadTheRealBoot pins the four probes a boot cost is measured with, on one real boot.
//   - the journal probe answers the journal bytes the open replayed: the window past the watermark, never the length of the log
//   - beside a root the writing view closed that watermark stands at the log's end, so the probe answers nothing although the log holds bytes
//   - beside a root whose index files state no watermark the open replays every frame, and the probe answers exactly the log the open read
//   - the two readings keep the probe lit: one that answered the log length fails the closed root, and one that answered nothing fails the root no index states
//   - a probe reading the append offset instead would answer the same number only by coincidence
//   - the index probe answers the resident index the load published, so it states the commits a boot keeps in memory rather than the history it committed
//   - the durable probe answers the load stats the commit already keeps, and the boot scanned the prefixes of the tasks it held
//   - the wall clock is the assemble the fixture timed
//   - the integrity probe answers the whole log the open verified beside the zero replay window, so a probe answering one measure for the other fails
//   - the slot scan probe answers zero for the boot and moves beside a real prefix enumeration, so the zero is a claim about the boot and never a dead face
//   - the addressed value probe answers the boot's reads and moves one to one beside a point read on a fresh view, so a flat face is never a dead counter
//   - the durable face is paid only for the tasks a checkpoint does not cover, and this fixture checkpoints last, so the measured boot pays no durable prefix key
//   - that face is pinned by the negative below and by TestTaskRestoreBoundedByCheckpoint, whose boot reads the chains that landed past its cut by identity
//   - the zero that face pays is a claim about recovery only over a chain something was seated on, so the probe reads the tasks that chain states
//   - the account the checkpoint covers states none of them, and the history names them all, which is the pair the two readings below pin
//   - a chain that holds no task is refused by name, which is the third negative below
//   - the negatives: every probe that cannot read its face answers the failure by name, so a zero is never mistaken for a boot that paid nothing
//   - the tier of a memory run holds an engine that keeps no journal and a commit that answers no load stats, which is exactly the pair these failures name
//   - a durable root nothing was seated on is the third, and the seated judgement refuses it by name
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestBootQuantityProbesReadTheRealBoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-boot-quantity-probes"}
	source := noTrimChainRun(t, ctx, storage)
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, "probe", 3)
	settleTasks(t, ctx, source, taskCheckpointCalls("probe", 2))
	if _, _, err := source.Checkpoint(ctx); err != nil {
		t.Fatalf("file the checkpoint of the probes fixture: %v", err)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the probes fixture: %v", err)
	}
	journal := filepath.Join(root, "kv", "journal.log")
	info, err := os.Stat(journal)
	if err != nil {
		t.Fatalf("stat the journal of the probes fixture: %v", err)
	}

	started := time.Now()
	boot := noTrimChainRun(t, ctx, storage)
	wallClock := time.Since(started)
	boot.stopCheckpointFiler()
	quantities, err := measureBootQuantities(boot.tier, boot.commit.inner, wallClock)
	if err != nil {
		t.Fatalf("measure the quantities of the probe boot: %v", err)
	}
	t.Logf("the probe boot paid journalBytes=%d indexEntries=%d durableLoadCount=%d wallClock=%s",
		quantities.journalBytes, quantities.indexEntries, quantities.durableLoadCount, quantities.wallClock)
	if quantities.journalBytes != 0 {
		t.Fatalf("the probe read %d journal byte(s) of a boot over a %d byte log its writing view closed, want none: the read states the window past the watermark the index files state, and that watermark stands at the log's end",
			quantities.journalBytes, info.Size())
	}
	degraded := t.TempDir()
	degradedSource := noTrimChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: degraded}))
	degradedSource.stopCheckpointFiler()
	commitSessionTurns(t, ctx, degradedSource, addr, "probe-degraded", 3)
	if _, err := degradedSource.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the degraded root of the probes fixture: %v", err)
	}
	degradedLog := journalLogBytesOf(t, degraded)
	if err := os.Remove(filepath.Join(degraded, "kv", "slots")); err != nil {
		t.Fatalf("discard the slot table of the degraded root: %v", err)
	}
	degradedBoot := noTrimChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: degraded}))
	degradedBoot.stopCheckpointFiler()
	degradedRead, err := journalBytesOf(degradedBoot.tier)
	if err != nil {
		t.Fatalf("measure the journal bytes of the boot over the degraded root: %v", err)
	}
	if degradedRead != degradedLog {
		t.Fatalf("the probe read %d journal byte(s) of a boot over a %d byte log whose index files state no watermark, want the whole log: an index nobody stated costs the read and never the answer",
			degradedRead, degradedLog)
	}
	if _, err := degradedBoot.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the boot over the degraded root: %v", err)
	}
	if quantities.indexEntries != 0 {
		t.Fatalf("the probe enumerated %d index entr(ies) of a chain stating its seat, want none: the seat states the count, the commit sequence and the partitions in one key, so the load reads no index entry whatever the history behind it",
			quantities.indexEntries)
	}
	seatless := t.TempDir()
	seatlessSource := noTrimChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: seatless}))
	seatlessSource.stopCheckpointFiler()
	commitSessionTurns(t, ctx, seatlessSource, addr, "probe-seatless", 3)
	if _, err := seatlessSource.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the seatless root of the probes fixture: %v", err)
	}
	seatlessWriter := journalRootWriter(t, ctx, seatless)
	if err := seatlessWriter.Store.Commit(ctx, ledger.Batch{Deletes: []string{storeSeatKeySpelling}}); err != nil {
		t.Fatalf("discard the seat of the seatless root: %v", err)
	}
	if err := seatlessWriter.Close(); err != nil {
		t.Fatalf("close the writing tier of the seatless root: %v", err)
	}
	seatlessBoot := noTrimChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: seatless}))
	seatlessBoot.stopCheckpointFiler()
	seatlessEntries, err := indexEntriesOf(seatlessBoot.tier)
	if err != nil {
		t.Fatalf("measure the index entries of the boot over the seatless root: %v", err)
	}
	if seatlessEntries == 0 {
		t.Fatalf("the probe enumerated no index entry of a chain stating no seat, want the index it derives the count from: the zero above is only a bound if the index face can read what it does not state")
	}
	if _, err := seatlessBoot.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the boot over the seatless root: %v", err)
	}
	if quantities.wallClock <= 0 {
		t.Fatalf("the fixture timed %s around the assemble, want a measured wall clock", quantities.wallClock)
	}
	if quantities.indexWalkVisits == 0 {
		t.Fatal("the boot's index walks named no entry, so the fourth face measured no walk of the chain it opened over")
	}
	if quantities.indexWalkValueBytes == 0 {
		t.Fatalf("the boot's index walks read no value byte beside the %d entries they named, so the byte face of the walk is not measured", quantities.indexWalkVisits)
	}
	if quantities.integrityBytesScanned != info.Size() {
		t.Fatalf("the probe boot verified %d integrity byte(s) of a %d byte log its writer closed, want the whole log: the integrity pass reads the history behind the watermark, and the zero journal face above is the replay window alone",
			quantities.integrityBytesScanned, info.Size())
	}
	if quantities.slotTableBytesScanned != 0 {
		t.Fatalf("the probe boot enumerated %d slot table byte(s), want none: the boot addresses the keys it names and reads no table whatever the history behind it", quantities.slotTableBytesScanned)
	}
	if _, err := boot.tier.Store.Read(ctx, ledger.Fetch{Prefix: "0:evt:"}); err != nil {
		t.Fatalf("read the event prefix of the probe boot: %v", err)
	}
	if lit, err := slotTableBytesScannedOf(boot.tier); err != nil || lit == 0 {
		t.Fatalf("the slot scan probe read %d byte(s) (err=%v) after a prefix enumeration, want the table the enumeration walked: the zero above is a claim about the boot only beside a probe a real scan moves", lit, err)
	}
	if quantities.addressedValueReads == 0 {
		t.Fatalf("the probe boot read no value by address, want the values a boot reads back: the flat bound on that face is a claim only beside a measured read")
	}
	reader, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("open the reading tier of the probes root: %v", err)
	}
	named, err := reader.Store.Keys(ctx, configChainPrefix)
	if err != nil || len(named) == 0 {
		t.Fatalf("name the configuration chain keys of the probes root: %d key(s) (err=%v), want the facts the fixture committed", len(named), err)
	}
	before, err := addressedValueReadsOf(reader)
	if err != nil {
		t.Fatalf("measure the values the reading view read at its open: %v", err)
	}
	if _, err := reader.Store.Read(ctx, ledger.Fetch{Key: named[len(named)-1]}); err != nil {
		t.Fatalf("read one chain value by the key its index names: %v", err)
	}
	after, err := addressedValueReadsOf(reader)
	if err != nil {
		t.Fatalf("measure the values the reading view read after one point read: %v", err)
	}
	if after != before+1 {
		t.Fatalf("the reading view read %d value(s) at its open and %d after one point read, want the one value the lookup read: the boot reading above is a measured count only beside a probe a real lookup moves", before, after)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close the reading tier of the probes root: %v", err)
	}
	seated, keeps := boot.commit.inner.(*state.DurableCommit)
	if !keeps {
		t.Fatalf("the probe boot holds the commit %T, which keeps no durable record, so the account its boot reads is not measured", boot.commit.inner)
	}
	if held := len(seated.TaskIDs()); held != 0 {
		t.Fatalf("the probe boot seats %d task(s), want none: the checkpoint covers every task the probe settled, so the account is the open work past its cut and those tasks stand on their chains alone", held)
	}
	seatedHistory, err := seated.HistoryTaskIDs(ctx)
	if err != nil {
		t.Fatalf("read the history of the probe boot by identity: %v", err)
	}
	if err := judgeSeatedHistory(len(seatedHistory), "of the probe boot"); err != nil {
		t.Fatalf("the durable face of the probe boot proves nothing: %v", err)
	}

	storeless := storelessBootRun(t, ctx)
	storeless.stopCheckpointFiler()
	if _, err := journalBytesOf(storeless.tier); err == nil {
		t.Fatal("the journal probe answered a boot whose engine keeps no journal, want the failure named")
	} else if !strings.Contains(err.Error(), "read no journal byte") {
		t.Fatalf("the journal probe refused the memory engine with %v, want the missing journal byte named", err)
	}
	if _, err := durableLoadCountOf(storeless.commit.inner); err == nil {
		t.Fatal("the durable probe answered a commit that keeps no durable record, want the failure named")
	} else if !strings.Contains(err.Error(), "read no durable load scan") {
		t.Fatalf("the durable probe refused the memory commit with %v, want the missing load scan named", err)
	}
	if entries, err := indexEntriesOf(storeless.tier); err == nil {
		t.Fatalf("the index probe answered %d entry(ies) of a boot that holds no segment store, want the failure named", entries)
	} else if !strings.Contains(err.Error(), "read no entry of the resident index") {
		t.Fatalf("the index probe refused the store-less boot with %v, want the missing resident index entry named", err)
	}
	if visits, _, _, err := indexWalkReadsOf(storeless.tier); err == nil {
		t.Fatalf("the walk probe answered %d visit(s) of a boot that holds no segment store, want the failure named", visits)
	} else if !strings.Contains(err.Error(), "read no walk of the index") {
		t.Fatalf("the walk probe refused the store-less boot with %v, want the missing index walk named", err)
	}
	if _, err := integrityBytesScannedOf(storeless.tier); err == nil {
		t.Fatal("the integrity probe answered a boot whose engine keeps no journal, want the failure named")
	} else if !strings.Contains(err.Error(), "read no integrity byte") {
		t.Fatalf("the integrity probe refused the memory engine with %v, want the missing integrity byte named", err)
	}
	if _, err := slotTableBytesScannedOf(storeless.tier); err == nil {
		t.Fatal("the slot scan probe answered a boot whose engine keeps no slot table, want the failure named")
	} else if !strings.Contains(err.Error(), "read no slot scan byte") {
		t.Fatalf("the slot scan probe refused the memory engine with %v, want the missing slot scan byte named", err)
	}
	if _, err := addressedValueReadsOf(storeless.tier); err == nil {
		t.Fatal("the addressed value probe answered a boot whose engine keeps no addressable log, want the failure named")
	} else if !strings.Contains(err.Error(), "read no addressed value") {
		t.Fatalf("the addressed value probe refused the memory engine with %v, want the missing addressed value named", err)
	}
	unseated := noTrimChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	unseated.stopCheckpointFiler()
	empty, keeps := unseated.commit.inner.(*state.DurableCommit)
	if !keeps {
		t.Fatalf("the boot over a root nothing was seated on holds the commit %T, which keeps no durable record", unseated.commit.inner)
	}
	emptyHistory, err := empty.HistoryTaskIDs(ctx)
	if err != nil {
		t.Fatalf("read the history of a root nothing was seated on by identity: %v", err)
	}
	if err := judgeSeatedHistory(len(emptyHistory), "of a root nothing was seated on"); err == nil {
		t.Fatalf("a durable root nothing was seated on states %d task(s) and the seated judgement accepted it, want it refused by name, so the zero the durable probe answers is never read as a bounded recovery", len(emptyHistory))
	} else if !strings.Contains(err.Error(), "nothing was seated over") {
		t.Fatalf("the seated judgement refused an empty chain with %v, want the empty chain named", err)
	}
}

// storeSeatKeySpelling is the chain key the segment store states its seat under: the
// store states the key to its own package alone, and this fixture discards it to
// measure what a chain stating no seat costs.
const storeSeatKeySpelling = "store:seat"

// journalWindowBoundBytes is the journal bytes one boot replays: the window one
// barrier covers of the log bytes the index files leave unstated.
//   - the value is the log budget of memory/ledger (journalLogSyncBytes, one chunk) and not a number chosen here
//   - its trigger rule is what bounds the window: a commit that crosses either budget states the watermark behind itself
//   - at a commit boundary the bytes a later open replays therefore stay below that budget
//   - the engine derives the budget from the widest window the index budget leaves: 41,500 bytes beside five 40-byte values a commit, 49,608 beside thirty-six
//   - the engine states the budget to its own package alone, so the value is restated here with the constant it restates named
//   - TestJournalWindowStaysWithinTheLogBudget holds the engine to that symbol itself, so a budget that moves leaves a red anchor behind
const journalWindowBoundBytes = 64 << 10

// journalRootWriter opens one root as the view that holds its write rights.
func journalRootWriter(t *testing.T, ctx context.Context, root string) *ledger.Tier {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the writing tier of %s: %v", root, err)
	}
	return tier
}

// journalRootCommits writes one batch through the writing tier of one root.
func journalRootCommits(t *testing.T, ctx context.Context, tier *ledger.Tier, puts ...ledger.Pair) {
	t.Helper()
	if err := tier.Store.Commit(ctx, ledger.Batch{Puts: puts}); err != nil {
		t.Fatalf("commit %d pair(s) of the window fixture: %v", len(puts), err)
	}
}

// journalReadFaceOpen answers the journal bytes the open of one root read, through the
// measure the engine states and the face that holds no write rights: the face the
// recovery tool reads beside a writer, which creates nothing and cuts nothing.
func journalReadFaceOpen(t *testing.T, ctx context.Context, root string) int64 {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("open the reading tier of %s: %v", root, err)
	}
	defer func() {
		if err := tier.Close(); err != nil {
			t.Fatalf("close the reading tier of %s: %v", root, err)
		}
	}()
	read, err := journalBytesOf(tier)
	if err != nil {
		t.Fatalf("measure the journal bytes of %s: %v", root, err)
	}
	return read
}

// TestJournalOpenReadStatesTheWindowPastTheWatermark pins the journal one boot reads on the three roots a recovery meets.
//   - settled root: a writer committed and closed, so the watermark stands at the log's end and the boot reads none of the bytes that log holds
//   - header behind: a commit crossed the log budget and stated the watermark, then two frames landed past it, so the boot reads exactly those frames
//   - the log behind that window holds far more than the window, which is what states the read is the bytes past the watermark rather than the history
//   - large history: a root of megabytes read after its writer closed answers nothing, the reading that states this cost is a window
//   - the header-behind window stays below journalWindowBoundBytes, the budget whose crossing states a watermark
//   - all three are read through the face that holds no write rights, which is the production read face of a root a writer owns
func TestJournalOpenReadStatesTheWindowPastTheWatermark(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	settled := t.TempDir()
	settledWriter := journalRootWriter(t, ctx, settled)
	journalRootCommits(t, ctx, settledWriter, ledger.Pair{Key: "0:evt:settled", Value: []byte("settled")})
	if err := settledWriter.Close(); err != nil {
		t.Fatalf("close the writing tier of the settled root: %v", err)
	}
	settledLog := journalLogBytesOf(t, settled)
	settledRead := journalReadFaceOpen(t, ctx, settled)
	if settledRead != 0 {
		t.Fatalf("the boot over the %d byte log its writer closed reads %d journal byte(s), want none: the watermark the index files state stands at the log's end",
			settledLog, settledRead)
	}

	behind := t.TempDir()
	behindWriter := journalRootWriter(t, ctx, behind)
	journalRootCommits(t, ctx, behindWriter, ledger.Pair{Key: "0:evt:big", Value: make([]byte, journalWindowBoundBytes+8<<10)})
	journalRootCommits(t, ctx, behindWriter, ledger.Pair{Key: "0:evt:small-1", Value: []byte("small")})
	journalRootCommits(t, ctx, behindWriter, ledger.Pair{Key: "0:evt:small-2", Value: []byte("small")})
	behindLog := journalLogBytesOf(t, behind)
	behindRead := journalReadFaceOpen(t, ctx, behind)
	if behindRead <= 0 {
		t.Fatalf("the boot beside a writer that committed past the watermark it stated reads %d journal byte(s), want the frames it left unstated", behindRead)
	}
	if behindRead >= journalWindowBoundBytes {
		t.Fatalf("the boot reads %d journal byte(s) beside a %d byte log, over the %d a barrier covers of the bytes the index files leave unstated",
			behindRead, behindLog, int64(journalWindowBoundBytes))
	}
	if behindRead >= behindLog {
		t.Fatalf("the boot reads %d journal byte(s) of a %d byte log, want fewer than the log holds: the window is the bytes past the watermark and never the history",
			behindRead, behindLog)
	}
	if err := behindWriter.Close(); err != nil {
		t.Fatalf("close the writing tier of the header-behind root: %v", err)
	}
	if closedRead := journalReadFaceOpen(t, ctx, behind); closedRead != 0 {
		t.Fatalf("the boot over the header-behind root reads %d journal byte(s) after its writer closed, want none: the close states the watermark at the log's end", closedRead)
	}

	large := t.TempDir()
	largeWriter := journalRootWriter(t, ctx, large)
	for at := 0; at < 18; at++ {
		journalRootCommits(t, ctx, largeWriter, ledger.Pair{Key: fmt.Sprintf("0:evt:large-%02d", at), Value: make([]byte, 300<<10)})
	}
	if err := largeWriter.Close(); err != nil {
		t.Fatalf("close the writing tier of the large root: %v", err)
	}
	largeLog := journalLogBytesOf(t, large)
	largeRead := journalReadFaceOpen(t, ctx, large)
	if largeLog < 3<<20 {
		t.Fatalf("the large root holds %d byte(s), want the megabytes the reading is stated over", largeLog)
	}
	if largeRead != 0 {
		t.Fatalf("the boot over the %d byte log its writer closed reads %d journal byte(s), want none: the open cost is the window past the watermark and never the history",
			largeLog, largeRead)
	}
	t.Logf("openBytes settled=%d behind=%d beside=%d large=%d largeLog=%d bound=%d", settledRead, behindRead, behindLog, largeRead, largeLog, int64(journalWindowBoundBytes))
}

// TestSymptomAnchorBootCostFlatAcrossGenerations is the recovery-cost symptom anchor: what one generation of a growing chain costs stays flat.
//   - fixture: five independent chains of one shape, each a session over its own durable root booted five generations (walkBootGenerations)
//   - each generation reopens the process, measures what its boot paid, then commits its turns, settles its tasks, files its checkpoints and closes
//   - the wall clock of a generation is the middle of the five chains' generations, so the bound reads the cost of a generation, not one hiccup
//   - positive: the wall clock of the generation and the two index faces stay bounded beside the second generation, and the journal face under the budget
//   - that bound is a claim about recovery whatever the chain behind them, which is what the fixture judgement checked before any bound ran
//   - those three are the journal bytes the open replays, the index entries the load enumerates and the durable prefix keys the task load scans
//   - the index face is judged at zero rather than at a multiple of the second generation
//   - the seat of the chain states the count, the commit sequence and the partitions in one key, so an open that reads no index entry is the bound itself
//   - a multiple of zero instead would accept any reading
//   - TestBootQuantityProbesReadTheRealBoot holds that zero beside the root that pays it: a chain stating no seat enumerates its whole index
//   - wall clock: judged beside the three reads at the same 2x
//   - that 2 is an uncalibrated candidate, not a frozen ruling, and no definition of done names a threshold here
//   - leaf 09.R2 fixes the threshold by same-condition calibration with a regression counter-example
//   - the face it is judged on is the whole generation, not the boot alone: a commit reads the journal to answer a duplicate
//   - the reads a growing history charges therefore land on the commits a generation carries
//   - a bound over the boot alone measures the axis the boot bounded — the one open of a rebuilt process — and reads flat
//   - the cost it was meant to bound grows in the commits the generation carries, which is the face this anchor judges
//   - negative: the open that replays the history behind the watermark, the load that publishes the whole history, the task load that scans every prefix
//   - so those grow in step with the history and the bound fails with the quantity, the generation and the whole series named
//   - the journal face is judged against the window one barrier covers, not against a multiple of a generation
//   - a settled root states a watermark at the log's end, so a boot over it reads no journal byte and a bound relative to that zero accepts any reading
//   - that window is bounded by the trigger of the index files: a commit that crosses either budget states the watermark behind itself
//   - a later open therefore replays less than that budget, which is journalWindowBoundBytes
//   - proxy: the decoded-body count cannot be the judgement, because a boot that reads the whole history still decodes its checkpoint and its tail alone
//   - the body count states what a rebuild presented and never what a boot had to read
//   - TestSymptomAnchorSessionBootCostFlatAcrossGenerations keeps measuring that proxy, and it stays green over the same growth this anchor turns red on
//   - self-evidence: the indexed chain grows and the tasks settle through the registry, so a red anchor claims recovery cost
//   - seated history: the fixture judgement holds the chain to every task it settled while the account a generation seats stays that generation's own
//   - so the zero that face pays is bounded recovery and never a store nothing was seated on
//   - over a chain nothing was seated on the same zero states that there was nothing to read, which is why judgeSeatedHistory refuses it by name
//   - assertion: each bound names the quantity it judged, the generation that broke it and the series it stands in
//   - consumer: every boot goes through the real boot entry (restoreCheckpointState → seedAnchoredReplay) of noTrimChainRun
//   - the commits go through CommitToSession, the task registry and the two checkpoints of the run
//   - the journal face is the engine's own open measure, the index face the index the load published
//   - the durable face is the load stats the commit already keeps, and the walk face the entries and value bytes the commit-order walks read
//   - the walk face is held flat beside the second generation: the boot stops its walk at the cut its newest checkpoint states, so it names the work past it
//   - the values the walk reads are held to the entries it named, which is where a value per held key is told apart from the index family alone
//   - the integrity face tracks the log one to one and grows with it: the declared intrinsic O(history) of the open, pinned linear, never pretended bounded
//   - the slot scan face is held at zero from the first seated generation: a boot that enumerates the table pays the capacity and fails by name
//   - the addressed value face is held flat beside the second generation, and a boot that walks a chain reads one value per fact it walks
//   - the empty-root generation states no seat and rederives its index, the fallback the probe test pins, so the zero face is judged past it
func TestSymptomAnchorBootCostFlatAcrossGenerations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-boot-cost-real"}
	const trials, generations, turns, perGeneration = 5, 5, 6, 4
	observed, history := walkBootGenerations(t, ctx, t.TempDir(), addr, trials, generations, turns, perGeneration)
	series := bootQuantitySeries(observed, history)
	t.Logf("the generations paid %s", series)

	if last, second := observed[generations-1].indexed, observed[1].indexed; last <= second {
		t.Fatalf("the chain indexed %d facts at the last generation and %d at the second, want a chain that grew so the flat cost is a claim about recovery rather than about a chain that stood still: the index is the denominator the recovery is measured against",
			last, second)
	}
	if err := judgeBootFixture(observed, history, generations, perGeneration); err != nil {
		t.Fatalf("the boot fixture proves nothing, so no bound below is a claim about recovery: %v", err)
	}
	if observed[1].journalLogBytes == 0 {
		t.Fatalf("the log holds no byte at the second boot, want a durable root whose history the boot leaves a window over: %s", series)
	}
	if last, second := observed[generations-1].journalLogBytes, observed[1].journalLogBytes; last <= second {
		t.Fatalf("the log grew from %d to %d byte(s) between the second boot and the last, want a history that grew so the window a boot leaves unstated is held beside the bytes behind it: %s",
			second, last, series)
	}

	// The fourth face is what the boot's index walks cost, held flat beside the
	// generation that opened a store with history: the newest framework checkpoint
	// states the cut the walk stops at, so the entries a boot names are the work
	// committed past that cut rather than the entries the chain holds.
	//   - the count is judged before the wall clock below: a walk that followed the history is refused by the count it named, whatever the machine added
	//   - the visit count and the bytes its values cost are both held to the same 2x of the second generation, and the chain they stand beside grew
	//   - the value count is where a walk that reads a value per held key is told apart from one that reads the index family alone: both name the same entries, and only the values read differ
	//   - a reference generation that named no entry fails by name, because 2x of zero accepts any reading
	const walkVisitBound = 2
	const walkValueReadBound = 2
	if observed[1].quantities.indexWalkVisits == 0 || observed[1].quantities.indexWalkValueBytes == 0 {
		t.Fatalf("the boot of generation 1 named %d index entr(ies) and read %d value byte(s), want both measured: the flat bound below is taken beside that reading (%s)",
			observed[1].quantities.indexWalkVisits, observed[1].quantities.indexWalkValueBytes, series)
	}
	for generation := 2; generation < generations; generation++ {
		held := observed[generation].quantities
		if bound := observed[1].quantities.indexWalkVisits * walkVisitBound; held.indexWalkVisits > bound {
			t.Fatalf("the boot of generation %d named %d index entr(ies) in its walks beside the %d generation 1 named, over the bound %d: the walk follows the history the chain holds instead of stopping at the cut its newest checkpoint states, and the chain indexed %d facts behind it (%s)",
				generation, held.indexWalkVisits, observed[1].quantities.indexWalkVisits, bound, observed[generation].indexed, series)
		}
		if bound := observed[1].quantities.indexWalkValueBytes * walkVisitBound; held.indexWalkValueBytes > bound {
			t.Fatalf("the boot of generation %d read %d value byte(s) in its walks beside the %d generation 1 read, over the bound %d: the walk reads the values of the history instead of the values of the entries it names (%s)",
				generation, held.indexWalkValueBytes, observed[1].quantities.indexWalkValueBytes, bound, series)
		}
		if bound := held.indexWalkVisits * walkValueReadBound; held.indexWalkValueReads > bound {
			t.Fatalf("the boot of generation %d read %d value(s) to name the %d index entr(ies) of its walks, over the bound %d: the walk reads the value of a key it never names, which is the event, window-meta, seat and original families committed beside the index (%s)",
				generation, held.indexWalkValueReads, held.indexWalkVisits, bound, series)
		}
	}

	// The wall clock of the generation is judged at the same 2x, the uncalibrated
	// candidate named above: it states the cost of a whole step of the walk, where
	// the work the fixture does beside the boot is bounded.
	const generationCostBound = 2
	for generation := 2; generation < generations; generation++ {
		secondGenerationWallClock := observed[1].generationWallClock
		heldGenerationWallClock := observed[generation].generationWallClock
		if bound := secondGenerationWallClock * generationCostBound; heldGenerationWallClock > bound {
			t.Fatalf("the generation cost grows with the history: the whole generation of %d — its boot, its commits, its settlements, its checkpoints and its close — took %s beside the %s of generation 1, over the bound %s of %d times the second generation (%s)",
				generation, heldGenerationWallClock, secondGenerationWallClock, bound, generationCostBound, series)
		}
	}

	// The boot alone is judged beside it at the same bound: the assemble of a
	// process is the axis the boot bounded, and the generation is where the cost
	// beside it is paid.
	const wallClockBound = 2
	for generation := 2; generation < generations; generation++ {
		secondWallClock := observed[1].quantities.wallClock
		heldWallClock := observed[generation].quantities.wallClock
		if bound := secondWallClock * wallClockBound; heldWallClock > bound {
			t.Fatalf("the wall clock of the assemble grows with the history: the boot of generation %d took %s beside the %s of generation 1, over the bound %s of %d times the second boot (%s)",
				generation, heldWallClock, secondWallClock, bound, wallClockBound, series)
		}
	}

	// The durable face is judged beside the same 2x over a seated history: the
	// fixture judgement above holds the chain to every settled task while the account
	// each generation seats stays that generation's own, so the zero this boot pays by
	// identity is bounded recovery and never an empty store.
	const durableLoadBound = 2
	for generation := 2; generation < generations; generation++ {
		held := observed[generation].quantities
		if held.journalBytes > journalWindowBoundBytes {
			t.Fatalf("the boot of generation %d replays %d journal bytes, over the %d one barrier covers of the log bytes the index files leave unstated: the open reads the history behind the watermark instead of the window past it (the log holds %d byte(s) at that generation) (%s)",
				generation, held.journalBytes, int64(journalWindowBoundBytes), observed[generation].journalLogBytes, series)
		}
		if held.indexEntries != 0 {
			t.Fatalf("the boot of generation %d enumerates %d index entr(ies) beside the %d of a store that opens over a chain stating its seat and its partitions: the load reads no index entry whatever the history, and the chain indexed %d facts behind it (%s)",
				generation, held.indexEntries, observed[0].quantities.indexEntries, observed[generation].indexed, series)
		}
		if bound := observed[1].quantities.durableLoadCount * durableLoadBound; held.durableLoadCount > bound {
			t.Fatalf("the boot of generation %d scans %d durable prefix keys beside the %d of generation 1, over the bound %d: the task load scans the history instead of the account (%s)",
				generation, held.durableLoadCount, observed[1].quantities.durableLoadCount, bound, series)
		}
	}

	for generation := 1; generation < generations; generation++ {
		held := observed[generation].quantities
		if held.integrityBytesScanned != observed[generation].journalLogBytes {
			t.Fatalf("the boot of generation %d verified %d integrity byte(s) beside a log of %d byte(s), want the whole log one to one: the integrity pass is the declared O(history) of the open, and a reading that tracks neither the log nor the window states no cost (%s)",
				generation, held.integrityBytesScanned, observed[generation].journalLogBytes, series)
		}
		if held.slotTableBytesScanned != 0 {
			t.Fatalf("the boot of generation %d enumerated %d slot table byte(s), want none: the boot reads the keys it names by address, and a full-table scan pays the capacity of every family the log stores whatever the chain holds (%s)",
				generation, held.slotTableBytesScanned, series)
		}
	}
	if last, second := observed[generations-1].quantities.integrityBytesScanned, observed[1].quantities.integrityBytesScanned; last <= second {
		t.Fatalf("the integrity pass verified %d byte(s) at the last generation beside the %d of the second, want a pass that follows the growing log: the linear reading is a claim about the open only beside a history that grew (%s)",
			last, second, series)
	}

	// The fifth face is the values the boot read back by address, held to the count
	// the second generation paid: a boot that adopts the generation its chain records
	// reads the facts of that chain and appends none, so the face states one count at
	// every depth, while a boot that publishes a fact per restart grows by it.
	//   - the reference generation must have read a value at all, because a flat face of zero accepts any reading
	//   - the log length above states the history the boot faces grew, so a flat face here is a claim about the boot and not about a chain that stopped growing
	const addressedValueReference = 1
	if observed[addressedValueReference].quantities.addressedValueReads == 0 {
		t.Fatalf("the boot of generation %d read no value by address, want a measured read: the flat face below is taken beside that reading (%s)", addressedValueReference, series)
	}
	for generation := 2; generation < generations; generation++ {
		held := observed[generation].quantities
		if held.addressedValueReads != observed[addressedValueReference].quantities.addressedValueReads {
			t.Fatalf("the boot of generation %d read %d value(s) by address beside the %d generation %d read, want the same count: the boot reads the facts of the history behind it instead of the facts of the chain it serves, and the log holds %d byte(s) at that generation (%s)",
				generation, held.addressedValueReads, observed[addressedValueReference].quantities.addressedValueReads, addressedValueReference, observed[generation].journalLogBytes, series)
		}
	}
}
