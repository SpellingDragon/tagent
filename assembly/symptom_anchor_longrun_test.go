// 契约: docs/wiki/memory/memory-architecture.md#gaps
package assembly

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// longRunGeneration is what one generation of the long run did and what each part of it cost.
//   - boot is the assemble that booted the process over the history the generations before it left
//   - commits, settling, sessions, frame and closing are the parts of the work span in the order the generation spends it
//   - work is the span from the first commit to the close, so those five parts partition it
//   - walkVisits and walkValueBytes are what the boot's index walks cost: the entries they named and the value bytes they read to name them
//   - restored is the facts the boot restored, committed the facts the generation committed, settled the tasks it settled through the registry, and tasks the account it restored held
//   - history is every task identity the log holds a fact for once the generation settled its own, which is the growth the chain carries whatever the account states
//   - integrityBytes is the log span the boot's open verified and slotScanBytes the slot table bytes its enumerations read, both from the engine's own measures
//   - addressedValueReads is the values the boot read back by address, from the engine's own measure: one per key a lookup answers, whatever family it belongs to
//   - journalLogBytes is the log the boot opened over, read from the file before the boot opens it, the denominator the integrity face tracks one to one
type longRunGeneration struct {
	boot                time.Duration
	commits             time.Duration
	settling            time.Duration
	sessions            time.Duration
	frame               time.Duration
	closing             time.Duration
	work                time.Duration
	walkVisits          int
	walkValueBytes      int64
	walkValueReads      int
	restored            int
	committed           int
	settled             []event.TaskID
	tasks               int
	history             int
	integrityBytes      int64
	slotScanBytes       int64
	addressedValueReads int64
	journalLogBytes     int64
}

// longRunStage is one part of a generation judged on its own: the stage of a growing
// chain that grows is a different defect from the stage that stays flat beside it, so
// each one is held to the bound instead of the sum of all of them.
type longRunStage struct {
	name string
	cost func(longRunGeneration) time.Duration
}

// longRunStages are the parts one generation is stated in: the assemble beside the
// five parts of the work span, in the order the generation spends them. The work
// stages are judged flat on their own windows; the assemble is judged by the counted
// faces and the paired noise band, because its declared reads grow with the history.
var longRunStages = []longRunStage{
	{name: "boot", cost: func(g longRunGeneration) time.Duration { return g.boot }},
	{name: "commits", cost: func(g longRunGeneration) time.Duration { return g.commits }},
	{name: "settling", cost: func(g longRunGeneration) time.Duration { return g.settling }},
	{name: "sessions", cost: func(g longRunGeneration) time.Duration { return g.sessions }},
	{name: "frame", cost: func(g longRunGeneration) time.Duration { return g.frame }},
	{name: "closing", cost: func(g longRunGeneration) time.Duration { return g.closing }},
}

// stageCost answers the cost of one stage over every generation of the walk.
func stageCost(run []longRunGeneration, stage longRunStage) []time.Duration {
	out := make([]time.Duration, 0, len(run))
	for _, entry := range run {
		out = append(out, stage.cost(entry))
	}
	return out
}

// total answers the whole cost of one generation: the assemble that booted the process beside the work the generation did over the history it opened.
func (g longRunGeneration) total() time.Duration {
	return g.boot + g.work
}

// walkLongRunGenerations boots the named number of generations over one durable root, each over the history the generations before it committed.
//   - each generation boots the run through the assemble that restores the framework checkpoint and seeds the anchored replay, commits the turns of its session through CommitToSession, settles its tasks through the registry, files the session and the framework checkpoints, and closes the process
//   - the counted probes are read between the timed spans, so no span carries them
//   - the first generation boots a store nothing was seated on, and the cost judgement holds it out of both windows
func walkLongRunGenerations(t *testing.T, ctx context.Context, root string, addr event.Address, generations, turns, perGeneration int) []longRunGeneration {
	t.Helper()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	run := make([]longRunGeneration, generations)
	for generation := 0; generation < generations; generation++ {
		logBytes := journalLogBytesOf(t, root)
		bootStarted := time.Now()
		boot := noTrimChainRun(t, ctx, storage)
		entry := longRunGeneration{boot: time.Since(bootStarted), journalLogBytes: logBytes}
		boot.stopCheckpointFiler()
		segments := boot.tier.Segments
		entry.walkVisits, entry.walkValueBytes, entry.walkValueReads = segments.IndexWalkReads()
		probe, held := boot.tier.Store.(interface {
			IntegrityBytesScanned() int64
			SlotTableBytesScanned() int64
			ValueReads() int64
		})
		if !held {
			t.Fatalf("generation %d booted the engine %T, which states no scan measures, so the integrity and slot faces are not measured", generation, boot.tier.Store)
		}
		entry.integrityBytes, entry.slotScanBytes = probe.IntegrityBytesScanned(), probe.SlotTableBytesScanned()
		entry.addressedValueReads = probe.ValueReads()
		entry.restored = segments.EventCount()
		durable, held := boot.commit.inner.(*state.DurableCommit)
		if !held {
			t.Fatalf("generation %d booted the commit %T, which keeps no durable record, so the account it restored is not measured", generation, boot.commit.inner)
		}
		entry.tasks = len(durable.TaskIDs())
		workStarted := time.Now()
		step := time.Now()
		commitSessionTurns(t, ctx, boot, addr, fmt.Sprintf("long-%d", generation), turns)
		entry.commits = time.Since(step)
		entry.committed = segments.EventCount() - entry.restored
		step = time.Now()
		entry.settled = settleTasks(t, ctx, boot, taskCheckpointCalls(fmt.Sprintf("long-%d", generation), perGeneration))
		entry.settling = time.Since(step)
		history, err := durable.HistoryTaskIDs(ctx)
		if err != nil {
			t.Fatalf("read the history of generation %d: %v", generation, err)
		}
		entry.history = len(history)
		step = time.Now()
		if _, _, err := boot.CheckpointSession(ctx, addr); err != nil {
			t.Fatalf("file the session checkpoint of generation %d: %v", generation, err)
		}
		entry.sessions = time.Since(step)
		step = time.Now()
		if _, _, err := boot.Checkpoint(ctx); err != nil {
			t.Fatalf("file the framework checkpoint of generation %d: %v", generation, err)
		}
		entry.frame = time.Since(step)
		step = time.Now()
		if _, err := boot.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
			t.Fatalf("close the process of generation %d: %v", generation, err)
		}
		entry.closing = time.Since(step)
		entry.work = time.Since(workStarted)
		run[generation] = entry
	}
	return run
}

// judgeLongRunFixture answers why the walk proves nothing about a per-generation cost, and nil beside a walk whose chain really grew.
//   - the first generation boots a store nothing was seated on, so it restores no fact
//   - every later boot restores more facts than the boot before it, so each generation is held against a growing history
//   - every generation settles the tasks the fixture admits, under identities no other generation settles
//   - every generation leaves the tasks it settled in the log, so the history a later boot opens over really grew
//   - the account a later boot restores owes none of them: this fixture settles its tasks and files the framework checkpoint before it closes, so neither the open set nor the tail past that cut holds one
//   - the account a boot restores states the tasks it still owes and the ones the tail past its cut names, never a task an earlier generation finished, so the restore the cost is paid on is the one measured
//   - a walk that fails any of these is refused by name, so an accepted cost series is never a series over a fixture that did nothing
func judgeLongRunFixture(run []longRunGeneration, generations, perGeneration int) error {
	if len(run) != generations {
		return fmt.Errorf("the walk booted %d generation(s), want the %d it was asked for", len(run), generations)
	}
	if run[0].restored != 0 {
		return fmt.Errorf("the first generation boots a store holding %d fact(s), want the empty root a walk starts on", run[0].restored)
	}
	seen := make(map[event.TaskID]int, generations*perGeneration)
	for generation, entry := range run {
		if len(entry.settled) != perGeneration {
			return fmt.Errorf("generation %d settled %d task(s), want the %d the fixture admits", generation, len(entry.settled), perGeneration)
		}
		if entry.committed == 0 {
			return fmt.Errorf("generation %d committed no fact, so the work the generation did is not measured", generation)
		}
		if entry.tasks != 0 {
			return fmt.Errorf("the boot of generation %d restored an account of %d task(s), want the tasks it still owes beside the tail past its cut, which this fixture leaves empty: the generations before it finished their tasks inside the generation that settled them", generation, entry.tasks)
		}
		if want := perGeneration * (generation + 1); entry.history != want {
			return fmt.Errorf("the log of generation %d holds %d task(s), want the %d the generation and the ones before it settled, so the history the account is bounded against really grew", generation, entry.history, want)
		}
		for _, taskID := range entry.settled {
			if prior, held := seen[taskID]; held {
				return fmt.Errorf("generation %d settled the task %s generation %d already settled, want an account that grows rather than one task rewritten", generation, taskID, prior)
			}
			seen[taskID] = generation
		}
		if generation > 0 && entry.restored <= run[generation-1].restored {
			return fmt.Errorf("the boot of generation %d restored %d fact(s) beside the %d of generation %d, want a chain that grew so the cost is held against a growing history", generation, entry.restored, run[generation-1].restored, generation-1)
		}
	}
	if want := generations * perGeneration; len(seen) != want {
		return fmt.Errorf("the walk settled %d distinct task(s), want the %d it admits", len(seen), want)
	}
	return nil
}

// perGenerationCostBoundTenths is the ratio the middle of the last window of generations may reach beside the middle of the first window that opens a store with history.
//   - the ratio is a candidate: the calibration that fixes a threshold measures the stages apart and holds a regression counter-example, and no definition of done names a number here
//   - it sits above the dispersion the fixture measures between two independent roots of one shape, so identical work never breaks it
//   - it sits below the growth this tree shows at the depth the walk covers, so a cost that carries the history is refused with the reading named
const perGenerationCostBoundTenths = 13

// judgePerGenerationCost answers why the per-generation cost of a long run grew with the chain it walked, and nil beside a cost that stayed flat.
//   - the reference is the middle of the first window of generations that open a store with history, and the held cost is the middle of the last window, so one scheduler hiccup states neither
//   - the first generation is held out of both windows: its boot restores nothing, so it states the cost of a generation over no history
//   - the refusal names the generation the held window starts at, the two readings, the bound and the bound in tenths
func judgePerGenerationCost(cost []time.Duration, window, boundTenths int) error {
	if window < 1 {
		return fmt.Errorf("the judgement holds %d generation(s) per window, want at least one", window)
	}
	if len(cost) < 2*window+1 {
		return fmt.Errorf("the walk measured %d generation(s), want at least the %d a reference window and a held window take between them", len(cost), 2*window+1)
	}
	reference := medianDuration(cost[1 : window+1])
	first := len(cost) - window
	held := medianDuration(cost[first:])
	if reference <= 0 {
		return fmt.Errorf("the reference generations cost %s, want a measured cost", reference)
	}
	if int64(held)*10 > int64(reference)*int64(boundTenths) {
		return fmt.Errorf("generation %d beside generations 1-%d: the middle generation of the last %d costs %s beside the %s of the first %d, over the bound of %s (%d tenths of the reference), so the whole cost of a generation grows with the chain it walked",
			first, window, window, held, reference, window, scaleDuration(reference, boundTenths), boundTenths)
	}
	return nil
}

// judgeGenerationStages answers why one work stage of a generation grew with the chain
// it walked, and nil beside a walk whose every work stage stayed inside the bound.
//   - the boot is held out of the flat bound: its declared intrinsic reads are pinned by the counted faces and its wall clock by the paired noise band, so a flat window bound on it would judge the declaration rather than a regression
//   - each stage is judged on its own window pair, so a stage that grew beside one that stayed flat is refused by name and never hidden in a sum
//   - every stage is judged before the refusal is stated, so one red anchor names all the stages that carry the history instead of the first one it reached
//   - the refusal names the stage, so the part of the generation that carries the history is stated rather than the generation alone
func judgeGenerationStages(run []longRunGeneration, window, boundTenths int) error {
	refused := make([]string, 0, len(longRunStages))
	for _, stage := range longRunStages {
		if stage.name == "boot" {
			continue
		}
		if err := judgePerGenerationCost(stageCost(run, stage), window, boundTenths); err != nil {
			refused = append(refused, fmt.Sprintf("the %s stage of the generation grows with the chain: %v", stage.name, err))
		}
	}
	if len(refused) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(refused, "\n"))
}

// scaleDuration answers a duration scaled by a number of tenths.
func scaleDuration(base time.Duration, tenths int) time.Duration {
	return time.Duration(int64(base) * int64(tenths) / 10)
}

// middleCount answers the middle count of one window of a counted series.
func middleCount(series []int64) int64 {
	if len(series) == 0 {
		return 0
	}
	ordered := append([]int64(nil), series...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered[len(ordered)/2]
}

// judgePerGenerationCount answers why one counted face of a generation grew with the chain it walked, and nil beside a count that stayed flat.
//   - the reference is the middle count of the first window of generations that open a store with history, and the held count is the middle of the last window
//   - a counted face carries no scheduler noise, so the bound reads the count itself and never a measurement of the machine
//   - the refusal names the face, the generation, the two counts and the bound, so a boot that walked the history is stated rather than the generation alone
func judgePerGenerationCount(name string, series []int64, window, boundTenths int) error {
	if window < 1 {
		return fmt.Errorf("the judgement holds %d generation(s) per window, want at least one", window)
	}
	if len(series) < 2*window+1 {
		return fmt.Errorf("the walk measured %d generation(s), want at least the %d a reference window and a held window take between them", len(series), 2*window+1)
	}
	reference := middleCount(series[1 : window+1])
	first := len(series) - window
	held := middleCount(series[first:])
	if reference <= 0 {
		return fmt.Errorf("the %s of the reference generations reads %d, want a measured count", name, reference)
	}
	if held*10 > reference*int64(boundTenths) {
		return fmt.Errorf("generation %d beside generations 1-%d: the %s of the middle generation of the last %d reads %d beside the %d of the first %d, over the bound of %d (%d tenths of the reference), so the boot reads the history the chain holds",
			first, window, name, window, held, reference, window, reference*int64(boundTenths)/10, boundTenths)
	}
	return nil
}

// countedFace names one counted part of a boot for the judgement that holds it.
//   - a face with neither flag is held flat to the bound beside the reference window
//   - a zero face is held at absolute zero from the first seated generation, because a multiple of zero accepts any reading and the empty-root generation rederives its index beside no seat
//   - a linear face is held one to one to the denominator it declares and must grow with it: the intrinsic O(history) is pinned linear rather than pretended bounded
type countedFace struct {
	name        string
	series      func(longRunGeneration) int64
	zero        bool
	denominator func(longRunGeneration) int64
}

// longRunCountedFaces are the counted parts of one boot judged beside the wall clock: the entries its index walks named, the value bytes they read to name them, the slot table bytes its enumerations read, the log span its integrity pass verified and the values it read back by address.
//   - the boot stops its walk at the cut the newest framework checkpoint states, so the walk faces follow the work committed past that cut and never the chain
//   - the boot addresses the keys it names, so the slot scan face reads none of the table whatever the history behind it
//   - the addressed value face is what one restart's chain load pays: a restart that adopts the generation its chain records reads the facts of that chain and appends none, so the face states one count at every depth
//   - the integrity pass verifies the whole log whatever the watermark states, so its face tracks the log's own length one to one, the declared intrinsic of the open
var longRunCountedFaces = []countedFace{
	{name: "index walk visits", series: func(g longRunGeneration) int64 { return int64(g.walkVisits) }},
	{name: "index walk value bytes", series: func(g longRunGeneration) int64 { return g.walkValueBytes }},
	{name: "index walk value reads", series: func(g longRunGeneration) int64 { return int64(g.walkValueReads) }},
	{name: "slot table bytes scanned", series: func(g longRunGeneration) int64 { return g.slotScanBytes }, zero: true},
	{name: "addressed value reads", series: func(g longRunGeneration) int64 { return g.addressedValueReads }},
	{name: "integrity bytes scanned", series: func(g longRunGeneration) int64 { return g.integrityBytes }, denominator: func(g longRunGeneration) int64 { return g.journalLogBytes }},
}

// judgeCountedFaces answers why one counted face of a boot grew with the chain it opened over, and nil beside a walk whose every face stayed inside the judgement it declares.
//   - each face is judged on its own window pair, so a face that grew beside a face that stayed flat is refused by name
//   - the values a walk reads are additionally held to the entries it named, which is where a walk that reads a value per held key is told apart from one that reads the index family alone
//   - a zero face refuses any enumeration from the first seated generation by name, which is where a boot that scans the table instead of addressing its keys fails
//   - a linear face refuses a reading that leaves its denominator and a series that stood still, so the declared intrinsic is pinned to the history it follows
//   - a flat face refuses a count whose last window carries the first window's, so a boot that reads the facts of the history behind its chain is refused by name whatever depth that chain reached
func judgeCountedFaces(run []longRunGeneration, window, boundTenths int) error {
	for _, face := range longRunCountedFaces {
		series := make([]int64, 0, len(run))
		for _, entry := range run {
			series = append(series, face.series(entry))
		}
		if face.zero {
			for generation := 1; generation < len(run); generation++ {
				if series[generation] != 0 {
					return fmt.Errorf("generation %d read %d %s, want none: the boot addresses the keys it names, and a full-table enumeration pays the capacity of every family the log stores whatever the chain holds",
						generation, series[generation], face.name)
				}
			}
			continue
		}
		if face.denominator != nil {
			for generation, entry := range run {
				if series[generation] != face.denominator(entry) {
					return fmt.Errorf("generation %d read %d %s beside the %d its denominator states, want one to one: the face is the declared intrinsic of the open and tracks the history itself",
						generation, series[generation], face.name, face.denominator(entry))
				}
			}
			if series[len(run)-1] <= series[1] {
				return fmt.Errorf("the %s read %d at the last generation beside the %d of the second, want a face that follows the growing history: the linear reading is a claim only beside a chain that grew",
					face.name, series[len(run)-1], series[1])
			}
			continue
		}
		if err := judgePerGenerationCount(face.name, series, window, boundTenths); err != nil {
			return err
		}
	}
	for generation, entry := range run {
		if entry.walkValueReads > 2*entry.walkVisits {
			return fmt.Errorf("generation %d read %d value(s) to name the %d entr(ies) of its walks, over twice the entries it named: the walk reads the value of a key it never names, which is the event, window-meta, seat and original families committed beside the index",
				generation, entry.walkValueReads, entry.walkVisits)
		}
	}
	return nil
}

// historyProportionalCost answers the cost series a generation that carries the history it opens over would show: every generation costs what the middle reference generation costs, scaled by the facts its own boot restored.
//   - the scale is the restored facts this walk measured, so the control is built from the walk's own denominator rather than from an invented series
//   - the walk restores strictly more facts every generation, so the middle of a window is its middle generation
func historyProportionalCost(run []longRunGeneration, cost []time.Duration, window int) []time.Duration {
	out := make([]time.Duration, len(run))
	middle := 1 + (window-1)/2
	base := int64(run[middle].restored)
	if base <= 0 || cost[middle] <= 0 {
		return out
	}
	for generation := range run {
		out[generation] = time.Duration(int64(cost[middle]) * int64(run[generation].restored) / base)
	}
	return out
}

// dispersionBootPairs is the number of back-to-back boot pairs the dispersion guard reads.
//   - the value is the paired method's sample bound: one sample cannot tell a scheduler hiccup from a cost that changed, and the median of fewer pairs drifts with the machine's load
//   - odd pairs reverse the order, so a drift in the load lands on both sides of the series instead of one root
const dispersionBootPairs = 21

// judgeFixtureDispersion answers why a bound cannot decide this fixture on this machine, and nil beside a noise band whose middle stays inside the bound.
//   - the ratios pair two independent roots of one shape at the same depth, boot against boot, so the series is what the machine adds to identical work on the axis the bound judges
//   - the middle of the pair ratios is the statistic the bound reads: a median over the bound decides the machine rather than the walk, which the refusal states
//   - the p10 to p90 of the series is the explicit noise band the growth separation reads, so a held growth clears the machine only above the control p90
//   - the series takes at least dispersionBootPairs pairs, so a thin sample never stands in for the band
func judgeFixtureDispersion(ratios []float64, boundTenths int) error {
	if len(ratios) < dispersionBootPairs {
		return fmt.Errorf("the dispersion guard read %d pair(s), want at least the %d the band takes: one sample cannot tell the machine from the walk", len(ratios), dispersionBootPairs)
	}
	middle := bootRatioPercentile(ratios, 50)
	if middle*10 > float64(boundTenths) {
		return fmt.Errorf("two independent roots of one shape paid a boot ratio median of %.3f for identical work, over the bound of %d tenths, so the bound sits inside the dispersion of this machine on the boot axis and a series over it decides nothing",
			middle, boundTenths)
	}
	return nil
}

// bootRatioPercentile answers the p-th percentile of one paired ratio series, ordered.
func bootRatioPercentile(ratios []float64, p int) float64 {
	ordered := append([]float64(nil), ratios...)
	sort.Float64s(ordered)
	return ordered[(p*len(ordered))/100]
}

// pairedBootRatios boots two closed roots back to back the named number of pairs, odd pairs reversed, and answers the ratio of the first root's boot to the second's.
//   - each boot is the real assemble beside no work, so the pair measures the open and the restore alone
//   - the order alternates so a drift in the machine's load lands on both sides of the series
//   - each boot adopts the publication record the chain already holds, so the pair is identical work: the difference the depth carries is the whole ratio
func pairedBootRatios(t *testing.T, ctx context.Context, first, second string, pairs int) []float64 {
	t.Helper()
	boot := func(root string) time.Duration {
		t.Helper()
		storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
		started := time.Now()
		run := noTrimChainRun(t, ctx, storage)
		elapsed := time.Since(started)
		run.stopCheckpointFiler()
		if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
			t.Fatalf("close the paired boot of %s: %v", root, err)
		}
		return elapsed
	}
	ratios := make([]float64, 0, pairs)
	for p := 0; p < pairs; p++ {
		var firstCost, secondCost time.Duration
		if p%2 == 0 {
			firstCost, secondCost = boot(first), boot(second)
		} else {
			secondCost, firstCost = boot(second), boot(first)
		}
		ratios = append(ratios, float64(firstCost)/float64(secondCost))
	}
	return ratios
}

// longRunCostSeries renders the whole cost of every generation beside the parts it spent it on and the history it opened over, in generation order.
func longRunCostSeries(run []longRunGeneration) string {
	total := make([]time.Duration, 0, len(run))
	boot := make([]time.Duration, 0, len(run))
	commits := make([]time.Duration, 0, len(run))
	settling := make([]time.Duration, 0, len(run))
	sessions := make([]time.Duration, 0, len(run))
	frame := make([]time.Duration, 0, len(run))
	closing := make([]time.Duration, 0, len(run))
	visits := make([]int, 0, len(run))
	valueBytes := make([]int64, 0, len(run))
	valueReads := make([]int, 0, len(run))
	restored := make([]int, 0, len(run))
	history := make([]int, 0, len(run))
	integrity := make([]int64, 0, len(run))
	slotScan := make([]int64, 0, len(run))
	addressed := make([]int64, 0, len(run))
	logBytes := make([]int64, 0, len(run))
	for _, entry := range run {
		total = append(total, entry.total())
		boot = append(boot, entry.boot)
		commits = append(commits, entry.commits)
		settling = append(settling, entry.settling)
		sessions = append(sessions, entry.sessions)
		frame = append(frame, entry.frame)
		closing = append(closing, entry.closing)
		visits = append(visits, entry.walkVisits)
		valueBytes = append(valueBytes, entry.walkValueBytes)
		valueReads = append(valueReads, entry.walkValueReads)
		restored = append(restored, entry.restored)
		history = append(history, entry.history)
		integrity = append(integrity, entry.integrityBytes)
		slotScan = append(slotScan, entry.slotScanBytes)
		addressed = append(addressed, entry.addressedValueReads)
		logBytes = append(logBytes, entry.journalLogBytes)
	}
	return fmt.Sprintf("total=%v boot=%v commits=%v settling=%v sessions=%v frame=%v closing=%v indexWalkVisits=%v indexWalkValueBytes=%v indexWalkValueReads=%v restored=%v history=%v integrityBytesScanned=%v slotTableBytesScanned=%v addressedValueReads=%v journalLogBytes=%v",
		total, boot, commits, settling, sessions, frame, closing, visits, valueBytes, valueReads, restored, history, integrity, slotScan, addressed, logBytes)
}

// TestSymptomAnchorPerGenerationCostFlatOverALongRun is the long-run anchor: work costs the same at every depth; the boot's declared reads follow the history.
//   - fixture: one durable root booted three times the generations the timed-out long run reached, one generation of history behind each
//   - each generation boots through the real assemble, commits through CommitToSession, settles through the registry, files both checkpoints and closes
//   - the work stages are judged flat apart: the commits, the settlements, the two checkpoints and the close, each on its own window pair
//   - the boot is judged by its counted faces: the integrity pass one to one, the slot scan face at zero, the addressed reads flat over the chain it serves
//   - one work stage carrying the history is a defect of that stage, and a sum of the stages hides it behind a stage that stayed flat
//   - the chain is the denominator: every boot restores more facts than the last, and every generation adds tasks to a history the account never restates
//   - assertion: the middle of the last window stays inside the bound of the middle of the first window that opens a store with history, stage by stage
//   - the empty-root generation is held out of both windows, because it restores nothing and so costs a generation over no history
//   - negative: a work stage that carries the history fails the bound naming the stage and the generation, and a boot that enumerates the table fails the zero face
//   - control one: an even series is accepted, so the judgement is not a refusal of every series
//   - control two: the measured series scaled by the facts each boot restored is refused, so the bound can fail on a series that stays flat here
//   - control three: the median of 21 paired boots over two independent roots of one shape stays inside the bound on the boot axis, odd pairs reversed
//   - control four: a paired series whose median carries the bound is refused by name, so the guard still refuses a machine the bound cannot decide
//   - the noise band is explicit: the control pairs' p10 to p90 is what the machine adds to identical work, and growth is separated only when the held p10 clears it
//   - the bound is a candidate: the calibration that fixes a threshold owns the number, and no definition of done names one
//   - depth: three times the generations the timed-out long run reached, where the ratio reads what dispersion reads and past it clears the bound
//   - proxy: the boot alone is not the whole cost, because a commit reads the journal a growing history left and a checkpoint relocates it
//   - the boot stage states its index walks beside its wall clock: the entries they named and the value bytes they read
//   - the counted faces are judged first and on their own windows: a walk that followed the history is refused by the count it named, whatever the machine added
//   - the values a boot reads are held to the entries it named, which is the face a walk that reads a value per held key breaks
//
// 契约: docs/wiki/memory/memory-architecture.md#gaps
func TestSymptomAnchorPerGenerationCostFlatOverALongRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-long-run-cost"}
	const generations, window, turns, perGeneration = 252, 5, 1, 1
	deepRoot := t.TempDir()
	run := walkLongRunGenerations(t, ctx, deepRoot, addr, generations, turns, perGeneration)
	if err := judgeLongRunFixture(run, generations, perGeneration); err != nil {
		t.Fatalf("the long run proves nothing, so no bound below is a claim about the chain: %v", err)
	}
	cost := make([]time.Duration, 0, len(run))
	for _, entry := range run {
		cost = append(cost, entry.total())
	}
	series := longRunCostSeries(run)
	t.Logf("the generations paid %s", series)

	flat := make([]time.Duration, len(cost))
	for generation := range flat {
		flat[generation] = cost[window]
	}
	if err := judgePerGenerationCost(flat, window, perGenerationCostBoundTenths); err != nil {
		t.Fatalf("the judgement refused a cost that stays flat, so it decides nothing: %v", err)
	}
	carried := historyProportionalCost(run, cost, window)
	if err := judgePerGenerationCost(carried, window, perGenerationCostBoundTenths); err == nil {
		t.Fatalf("the judgement accepted a per-generation cost that carries the history it opens over, want it refused by name (the measured series is indexed by generation, 0..%d): %s", generations-1, series)
	} else if !strings.Contains(err.Error(), "grows with the chain") {
		t.Fatalf("the judgement refused the history-carrying control with %v, want the growing cost named", err)
	}
	biased := make([]float64, dispersionBootPairs)
	for at := range biased {
		biased[at] = 1.5
	}
	if err := judgeFixtureDispersion(biased, perGenerationCostBoundTenths); err == nil {
		t.Fatal("the dispersion guard accepted a boot ratio median of 1.5 for identical work, want it refused by name: the guard must still refuse a machine whose noise carries the bound")
	} else if !strings.Contains(err.Error(), "dispersion of this machine") {
		t.Fatalf("the dispersion guard refused the biased series with %v, want the machine dispersion named", err)
	}
	if err := judgeCountedFaces(run, window, perGenerationCostBoundTenths); err != nil {
		t.Fatalf("a counted face of the boot grows with the chain: %v (the series below is indexed by generation, 0..%d)\n%s",
			err, generations-1, series)
	}
	firstShallowRoot, secondShallowRoot := t.TempDir(), t.TempDir()
	firstShallow := walkLongRunGenerations(t, ctx, firstShallowRoot, addr, window+1, turns, perGeneration)
	secondShallow := walkLongRunGenerations(t, ctx, secondShallowRoot, addr, window+1, turns, perGeneration)
	t.Logf("two independent roots of one shape paid boot %s and %s at the reference depth",
		medianDuration(stageCost(firstShallow, longRunStages[0])), medianDuration(stageCost(secondShallow, longRunStages[0])))
	controlRatios := pairedBootRatios(t, ctx, firstShallowRoot, secondShallowRoot, dispersionBootPairs)
	t.Logf("the control boot ratios paid median %.3f p10 %.3f p90 %.3f",
		bootRatioPercentile(controlRatios, 50), bootRatioPercentile(controlRatios, 10), bootRatioPercentile(controlRatios, 90))
	if err := judgeFixtureDispersion(controlRatios, perGenerationCostBoundTenths); err != nil {
		t.Fatalf("the bound decides the dispersion of this machine rather than the walk: %v", err)
	}
	heldShallowRoot := t.TempDir()
	walkLongRunGenerations(t, ctx, heldShallowRoot, addr, window+1, turns, perGeneration)
	heldRatios := pairedBootRatios(t, ctx, deepRoot, heldShallowRoot, dispersionBootPairs)
	heldP10, controlP90 := bootRatioPercentile(heldRatios, 10), bootRatioPercentile(controlRatios, 90)
	if heldP10 > controlP90 {
		t.Logf("the boot growth at depth %d is separated from the machine: held p10 %.3f over control p90 %.3f, and the counted faces hold it to the declared reads",
			generations, heldP10, controlP90)
	} else {
		t.Logf("the boot growth at depth %d is not separated from the machine: held p10 %.3f inside control p90 %.3f, and the counted faces still pin the reads",
			generations, heldP10, controlP90)
	}
	if err := judgeGenerationStages(run, window, perGenerationCostBoundTenths); err != nil {
		t.Fatalf("a stage of the generation grows with the chain: %v (the series below is indexed by generation, 0..%d)\n%s",
			err, generations-1, series)
	}
}
