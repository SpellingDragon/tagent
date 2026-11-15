// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
package assembly

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
//   - journalBytes is the journal the open replayed, read from the engine's own
//     open measure: never the depth the log grew to, which is a property of the
//     writes and not of the boot
//   - indexEntries is the resident index the load published, read from the store:
//     never EventCount, which is the whole committed history and answers the same
//     number for a bounded load as for an unbounded one
//   - durableLoadCount is the durable prefix keys the task load scanned, read from
//     the load stats the commit already keeps
//   - wallClock is the time the assemble that did all three took
//
// None of them is a decoded-body count: a body count states how much material a
// rebuild presented, never how much the boot had to read to present it.
type bootQuantities struct {
	journalBytes     int64
	indexEntries     int
	durableLoadCount int
	wallClock        time.Duration
}

// series renders the four quantities of every generation on one line, so a failed
// bound names the whole series beside the quantity that broke it.
func bootQuantitySeries(observed []generationBoot) string {
	journal := make([]int64, 0, len(observed))
	index := make([]int, 0, len(observed))
	durable := make([]int, 0, len(observed))
	wall := make([]time.Duration, 0, len(observed))
	for _, entry := range observed {
		journal = append(journal, entry.quantities.journalBytes)
		index = append(index, entry.quantities.indexEntries)
		durable = append(durable, entry.quantities.durableLoadCount)
		wall = append(wall, entry.quantities.wallClock)
	}
	return fmt.Sprintf("journalBytes=%v indexEntries=%v durableLoadCount=%v wallClock=%v", journal, index, durable, wall)
}

// generationBoot is what one generation of the boot fixture measured before it
// worked: the four quantities its boot paid, the chain index the boot opened over
// as the denominator those quantities are held against, the tasks it settled
// through the registry and the tasks the durable record held afterwards.
type generationBoot struct {
	quantities bootQuantities
	indexed    int
	settled    []event.TaskID
	accounted  int
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

// indexEntriesOf answers the resident index one boot's load published.
func indexEntriesOf(tier *ledger.Tier) (int, error) {
	if tier == nil || tier.Segments == nil {
		return 0, fmt.Errorf("the boot holds no segment store, so the probe read no entry of the resident index")
	}
	return tier.Segments.IndexEntries(), nil
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
	quantities.journalBytes = journal
	quantities.indexEntries = index
	quantities.durableLoadCount = durable
	return quantities, nil
}

// judgeBootFixture answers why the boot fixture proves nothing, and nil beside a
// fixture that settled its tasks across every generation over a chain that grew.
// The judgements are the fixture's own: a flat boot quantity is only a claim about
// recovery if the tasks the boot reads really accumulated and the chain behind them
// really grew.
func judgeBootFixture(observed []generationBoot, generations, perGeneration int) error {
	if len(observed) != generations {
		return fmt.Errorf("the fixture booted %d generation(s), want the %d it was asked for", len(observed), generations)
	}
	seen := make(map[event.TaskID]int, generations*perGeneration)
	for generation, entry := range observed {
		if len(entry.settled) == 0 {
			return fmt.Errorf("the fixture settled no task, so the durable load face is not measured (generation %d)", generation)
		}
		if len(entry.settled) != perGeneration {
			return fmt.Errorf("generation %d settled %d task(s), want the %d the fixture admits", generation, len(entry.settled), perGeneration)
		}
		for _, taskID := range entry.settled {
			if prior, held := seen[taskID]; held {
				return fmt.Errorf("generation %d settled the task %s generation %d already settled, want an account that grows rather than one task rewritten", generation, taskID, prior)
			}
			seen[taskID] = generation
		}
	}
	if want := generations * perGeneration; len(seen) != want {
		return fmt.Errorf("the fixture settled %d distinct task(s), want the %d it admits", len(seen), want)
	}
	if accounted := observed[generations-1].accounted; accounted != len(seen) {
		return fmt.Errorf("the durable record held %d task(s) at the last generation, want the %d the fixture settled", accounted, len(seen))
	}
	if last, first := observed[generations-1].accounted, observed[0].accounted; last <= first {
		return fmt.Errorf("the durable record grew from %d to %d task(s) across the generations, want an account that grows so a flat load is a claim about recovery", first, last)
	}
	if observed[generations-1].quantities.journalBytes == 0 {
		return fmt.Errorf("the boot of the last generation replayed no journal byte, so the tail the open pays is not measured")
	}
	// The durable face is measured where a boot pays it, and a boot pays the
	// durable families of the tasks a checkpoint does not cover: this fixture files
	// its checkpoint last, so the account it restores is stated whole by that
	// checkpoint and costs no durable prefix key. That is the bound this domain
	// states, so the fixture judges the probes that must pay every generation — the
	// journal tail and the resident index — and leaves the durable face to the
	// anchors that settle tasks past a cut (TestTaskRestoreBoundedByCheckpoint).
	if last, second := observed[generations-1].indexed, observed[1].indexed; last <= second {
		return fmt.Errorf("the chain indexed %d facts at the last generation and %d at the second, want a chain that grew so the boot quantities are held against a growing history", last, second)
	}
	return nil
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

// walkBootGenerations boots one durable root the named number of generations. Each
// generation measures what its boot paid, then commits the turns of the session,
// settles the tasks through the registry, files the session and the framework
// checkpoints and closes the process, so the next generation opens over a storage
// that grew by one generation of facts and tasks.
func walkBootGenerations(t *testing.T, ctx context.Context, storage Option, addr event.Address, generations, turns, perGeneration int) []generationBoot {
	t.Helper()
	observed := make([]generationBoot, 0, generations)
	for generation := 0; generation < generations; generation++ {
		started := time.Now()
		boot := noTrimChainRun(t, ctx, storage)
		wallClock := time.Since(started)
		boot.stopCheckpointFiler()
		quantities, err := measureBootQuantities(boot.tier, boot.commit.inner, wallClock)
		if err != nil {
			t.Fatalf("measure the quantities of the boot of generation %d: %v", generation, err)
		}
		entry := generationBoot{quantities: quantities, indexed: chainIndexSize(t, ctx, boot)}
		commitSessionTurns(t, ctx, boot, addr, fmt.Sprintf("gen-%d", generation), turns)
		entry.settled = settleTasks(t, ctx, boot, taskCheckpointCalls(fmt.Sprintf("gen-%d", generation), perGeneration))
		if durable, held := boot.commit.inner.(*state.DurableCommit); held {
			entry.accounted = len(durable.TaskIDs())
		}
		if _, _, err := boot.CheckpointSession(ctx, addr); err != nil {
			t.Fatalf("file the session checkpoint of generation %d: %v", generation, err)
		}
		if _, _, err := boot.Checkpoint(ctx); err != nil {
			t.Fatalf("file the framework checkpoint of generation %d: %v", generation, err)
		}
		if _, err := boot.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
			t.Fatalf("close the process of generation %d: %v", generation, err)
		}
		observed = append(observed, entry)
	}
	return observed
}

// TestBootFixtureGrowsTasksAcrossGenerations pins the fixture the boot anchors are
// measured on, so an anchor that reads flat never reads flat because the fixture
// did nothing.
//   - every generation settles its tasks through the registry and the durable
//     record holds more of them than the generation before
//   - the task identities of one generation never repeat another's, so the
//     account the boot reads is a growing account rather than one task rewritten
//   - the durable load face is measured at all: a boot that scanned no durable
//     prefix key fails by name
//   - the chain index grows across the generations, the denominator the boot
//     quantities are held against
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestBootFixtureGrowsTasksAcrossGenerations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-boot-fixture"}
	const generations, turns, perGeneration = 5, 6, 4
	observed := walkBootGenerations(t, ctx, storage, addr, generations, turns, perGeneration)
	t.Logf("the fixture booted %d generations over %s", generations, bootQuantitySeries(observed))
	if err := judgeBootFixture(observed, generations, perGeneration); err != nil {
		t.Fatalf("the boot fixture proves nothing: %v", err)
	}
	// The negative of the fixture judgement: a fixture that settled nothing is
	// refused by name instead of reading as a flat cost, so the anchors measured
	// over this fixture can never be green because nothing happened.
	quiet := make([]generationBoot, generations)
	for at := range quiet {
		quiet[at] = generationBoot{}
	}
	if err := judgeBootFixture(quiet, generations, perGeneration); err == nil {
		t.Fatal("the fixture judgement accepted a fixture that settled no task, want it refused by name")
	} else if !strings.Contains(err.Error(), "the fixture settled no task") {
		t.Fatalf("the fixture judgement refused the empty fixture with %v, want the settled count named", err)
	}
}

// TestBootQuantityProbesReadTheRealBoot pins the four probes a boot cost is
// measured with, on one real boot.
//   - the journal probe answers the bytes the open replayed, which is the file the
//     open read: a boot that replays the whole journal states the whole journal,
//     and a probe reading the append offset instead would answer the same number
//     only by coincidence
//   - the index probe answers the resident index the load published, so it states
//     the commits a boot keeps in memory rather than the history it committed
//   - the durable probe answers the load stats the commit already keeps, and the
//     boot scanned the prefixes of the tasks it held
//   - the wall clock is the assemble the fixture timed
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
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
	if quantities.journalBytes == 0 {
		t.Fatal("the probe read no journal byte of the boot")
	}
	// The read is the tail past the engine's index cut, so the log size bounds it and
	// never states it: an open that places the cut pays the records the rewrite filed
	// after them, and an open that reads the whole log pays all of it. The positive
	// statement of the cut itself is the engine's own anchor
	// (TestOpenReplaysOnlyPastTheIndexCut), which pins the read against the cut.
	if want := info.Size(); quantities.journalBytes > want {
		t.Fatalf("the probe read %d journal byte(s) of a boot over a %d byte log, want at most the bytes the log holds",
			quantities.journalBytes, want)
	}
	if quantities.indexEntries == 0 {
		t.Fatal("the probe read no entry of the resident index the load published")
	}
	// The durable face is the one a bounded boot pays only for the tasks its
	// checkpoint does not cover. This fixture checkpoints last, so the boot it
	// measures restores a covered account and pays no durable prefix key — the bound
	// this face is held to, not a missing measurement. The face itself is pinned by
	// the negative below (a commit that keeps no load stats fails by name) and
	// positively by TestTaskRestoreBoundedByCheckpoint, whose boot reads the chains
	// that landed past its cut by identity.
	if quantities.wallClock <= 0 {
		t.Fatalf("the fixture timed %s around the assemble, want a measured wall clock", quantities.wallClock)
	}

	// The negatives: every probe that cannot read its face answers the failure by
	// name, so a zero is never mistaken for a boot that paid nothing. The tier of a
	// memory run holds an engine that keeps no journal and a commit that answers no
	// load stats, which is exactly the pair these failures name.
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
}

// TestSymptomAnchorBootCostFlatAcrossGenerations is the S2 symptom anchor
// (runtime-recovery-ideal design.md §六): the real quantities one boot pays SHALL
// stay flat across generations while the chain grows.
//   - fixture: one session over one durable root, five generations; each boot
//     reopens the process, measures what it paid, then the generation commits its
//     turns, settles its tasks and files its session and framework checkpoints
//     (walkBootGenerations, the same fixture the full-history anchor reads)
//   - positive: the three certain quantities a boot pays — the journal bytes the
//     open replays, the resident index the load publishes, the durable prefix keys
//     the task load scans — stay bounded beside the second boot, whatever the chain
//     behind it
//   - wall clock: the assemble of a boot is the fourth quantity, held to a loose
//     bound because it is noise beside the three measured reads
//   - negative: today the open replays the whole journal, the load publishes the
//     whole history into the resident index and the task load scans every prefix
//     whole, so all three grow in step with the history and the bound fails with
//     the quantity, the generation and the whole series named
//   - proxy: the decoded-body count the earlier anchor measured cannot be the
//     judgement — a boot that reads the whole history still decodes its checkpoint
//     and its tail alone, so the body count states what a rebuild presented and
//     never what a boot had to read. TestSymptomAnchorSessionBootCostFlatAcrossGenerations
//     is the sibling that keeps measuring that proxy, and it stays green over the
//     same growth this anchor turns red on
//   - self-evidence: the indexed chain grows across the generations and the tasks
//     settle through the registry, so a red anchor is a claim about boot cost, not
//     about a chain that stood still or a fixture that settled nothing
//   - consumer: every boot goes through the real boot entry
//     (restoreCheckpointState → seedAnchoredReplay) of noTrimChainRun; the journal
//     face is the engine's own open measure, the index face the index the load
//     published, and the durable face the load stats the commit already keeps
func TestSymptomAnchorBootCostFlatAcrossGenerations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-boot-cost-real"}
	const generations, turns, perGeneration = 5, 6, 4
	observed := walkBootGenerations(t, ctx, storage, addr, generations, turns, perGeneration)
	series := bootQuantitySeries(observed)
	t.Logf("the boots paid %s", series)

	// The denominators: the chain grew and the tasks settled, so the bounds below
	// are about boot cost and not about a store that stood still.
	if last, second := observed[generations-1].indexed, observed[1].indexed; last <= second {
		t.Fatalf("the chain indexed %d facts at the last generation and %d at the second, want a chain that grew so the flat cost is a claim about recovery rather than about a chain that stood still: the index is the denominator the recovery is measured against",
			last, second)
	}
	for generation, entry := range observed {
		if len(entry.settled) != perGeneration {
			t.Fatalf("generation %d settled %d task(s), want the %d the fixture admits, so the durable load face is measured", generation, len(entry.settled), perGeneration)
		}
	}
	if observed[1].quantities.journalBytes == 0 {
		t.Fatalf("the journal holds no byte before the second boot, want a durable root the open reads: %s", series)
	}

	// The three certain quantities: bounded recovery pays the second boot's cost
	// again at every later generation. Each bound names the quantity it judged, the
	// generation that broke it and the series it stands in.
	for generation := 2; generation < generations; generation++ {
		held := observed[generation].quantities
		if bound := observed[1].quantities.journalBytes * 2; held.journalBytes > bound {
			t.Fatalf("the boot of generation %d replays %d journal bytes beside the %d of generation 1, over the bound %d: the open read grows with the history instead of staying bounded (%s)",
				generation, held.journalBytes, observed[1].quantities.journalBytes, bound, series)
		}
		if bound := observed[1].quantities.indexEntries * 2; held.indexEntries > bound {
			t.Fatalf("the boot of generation %d publishes a resident index of %d entries beside the %d of generation 1, over the bound %d: the load publishes the history instead of staying bounded, and the chain indexed %d facts behind it (%s)",
				generation, held.indexEntries, observed[1].quantities.indexEntries, bound, observed[generation].indexed, series)
		}
		if bound := observed[1].quantities.durableLoadCount * 2; held.durableLoadCount > bound {
			t.Fatalf("the boot of generation %d scans %d durable prefix keys beside the %d of generation 1, over the bound %d: the task load scans the history instead of the account (%s)",
				generation, held.durableLoadCount, observed[1].quantities.durableLoadCount, bound, series)
		}
	}

	// The wall clock is the fourth quantity and the one that is noise: it is held
	// to an order-of-magnitude bound and logged, never the judgement.
	const wallClockTolerance = 8
	if second, last := observed[1].quantities.wallClock, observed[generations-1].quantities.wallClock; last > second*wallClockTolerance {
		t.Fatalf("the last boot took %s beside the %s of the second, over %d times the second boot: the wall clock of the assemble grows with the history (%s)",
			last, second, wallClockTolerance, series)
	}
}
