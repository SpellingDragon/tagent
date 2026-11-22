// 契约: docs/wiki/memory/memory-architecture.md#gaps
package assembly

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
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
//   - integritySpan is how long that verification took, read from the pair of clock reads the open itself took: it is the one linear term of the boot the wall clock attributes, and nothing else is exempted by it
//   - rebindSpan is how long the fold of the same boot took, read from the pair that call took: a stage sampled on its own rather than derived from the boot
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
	integritySpan       time.Duration
	rebindSpan          time.Duration
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

// bootBeyondVerification answers the boot with the one linear term it declares taken out: the complete-frame check reads every byte the log holds, so its own span is attributed and the rest of the assemble is what the wall clock judges.
func (g longRunGeneration) bootBeyondVerification() time.Duration {
	return g.boot - g.integritySpan
}

// bootUnattributed answers the part of the boot no sampled stage states: the whole assemble less the verification and the fold that run inside it. The remainder is named for what it is, because it belongs to no stage by construction.
func (g longRunGeneration) bootUnattributed() time.Duration {
	return g.boot - g.integritySpan - g.rebindSpan
}

// walkLongRunGenerations boots the named number of generations over one durable root, each over the history the generations before it committed.
//   - each generation boots the run through the assemble that restores the framework checkpoint and seeds the anchored replay, commits the turns of its session through CommitToSession, settles its tasks through the registry, files the session and the framework checkpoints, and closes the process
//   - the counted probes are read between the timed spans, so no span carries them
//   - the first generation boots a store nothing was seated on, and the cost judgement holds it out of both windows
func walkLongRunGenerations(t *testing.T, ctx context.Context, root string, addr event.Address, generations, turns, perGeneration int) []longRunGeneration {
	t.Helper()
	return walkLongRunGenerationsOver(t, ctx, root, addr, generations, turns, perGeneration, nil)
}

// walkLongRunGenerationsOver is the same walk with the engine of every generation opened through the work the named function states for that root: it repeats those steps inside every read it answers, and opens the engine the tier names unchanged beside no function.
//   - the decorator wraps the real durable engine of the root, so the boot, the commits and the closes stay the calls the host makes and the injection rides a real call rather than a stand-in
//   - the steps are read from the log length of the root before the generation boots it, so the repeated work is proportional to the history that generation opens over
func walkLongRunGenerationsOver(t *testing.T, ctx context.Context, root string, addr event.Address, generations, turns, perGeneration int, steps func(logBytes int64) int) []longRunGeneration {
	t.Helper()
	run := make([]longRunGeneration, generations)
	for generation := 0; generation < generations; generation++ {
		logBytes := journalLogBytesOf(t, root)
		storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
		if steps != nil {
			storage = WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root, Engine: decoratedEngine(root, steps(logBytes))})
		}
		bootStarted := time.Now()
		boot := noTrimChainRun(t, ctx, storage)
		entry := longRunGeneration{boot: time.Since(bootStarted), journalLogBytes: logBytes}
		boot.stopCheckpointFiler()
		verifiedBegin, verifiedEnd, err := integrityPassSpanOf(boot.tier)
		if err != nil {
			t.Fatalf("generation %d read the verification span of its boot: %v", generation, err)
		}
		if verifiedBegin.IsZero() != verifiedEnd.IsZero() {
			t.Fatalf("the boot of generation %d states the verification opened at %v and closed at %v, want both reads or neither", generation, verifiedBegin, verifiedEnd)
		}
		if !verifiedBegin.IsZero() {
			entry.integritySpan = verifiedEnd.Sub(verifiedBegin)
		}
		fold := boot.restoreStageSpan()
		if fold.begin.IsZero() != fold.end.IsZero() {
			t.Fatalf("the boot of generation %d states the fold opened at %v and closed at %v, want both reads or neither", generation, fold.begin, fold.end)
		}
		if !fold.begin.IsZero() {
			entry.rebindSpan = fold.end.Sub(fold.begin)
		}
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

// judgeStageSpansAccounted answers why the boot's one declared linear term and its fold are not both stated by the boot they belong to, and nil beside a walk whose every boot states them.
//   - the verification span and the fold are read from the pair of clock reads their own calls took, so a boot that reports neither is refused by name rather than attributed from a difference
//   - a span longer than the assemble it runs inside means the pair belongs to another call, so the containment is refused instead of subtracted
//   - this is the accounting the table of the domain's design states as 未归因: the row states what is left over after the named stages, and never renames it after one of them
func judgeStageSpansAccounted(run []longRunGeneration) error {
	for generation, entry := range run {
		if generation > 0 && entry.integritySpan <= 0 {
			return fmt.Errorf("the boot of generation %d states no verification span beside the %d byte(s) its log holds, so the one linear term of the boot is unmeasured", generation, entry.journalLogBytes)
		}
		for _, stage := range []struct {
			name string
			span time.Duration
		}{{name: "completeness check", span: entry.integritySpan}, {name: "fold", span: entry.rebindSpan}} {
			if stage.span > entry.boot {
				return fmt.Errorf("the boot of generation %d states its %s for %s inside an assemble of %s: a stage runs inside the boot it belongs to, never beside it", generation, stage.name, stage.span, entry.boot)
			}
		}
	}
	return nil
}

// judgeCountedFacesUnmoved answers why one walk states a different count from another over the same work, and nil beside two walks whose every counted read agrees generation by generation.
//   - the count a boot paid is the side no machine noise moves, so a run that repeats work inside a real call leaves every read face of this list exactly where the control left it
//   - the integrity bytes are held to the log the same generation opened over, because the log length is what the fixture wrote while the check is one pass over it: two roots of one shape write frames whose length follows their own timing, so the pair states the same reading of its own log rather than the same absolute byte count
//   - the faces are compared generation by generation, so a count that agrees at the windows and differs elsewhere is refused by name
func judgeCountedFacesUnmoved(control, held []longRunGeneration) error {
	if len(control) != len(held) {
		return fmt.Errorf("the control walked %d generation(s) beside the %d of the run it is held against, so no two generations are the same work", len(control), len(held))
	}
	faces := []struct {
		name   string
		series func(longRunGeneration) int64
	}{
		{name: "restored facts", series: func(g longRunGeneration) int64 { return int64(g.restored) }},
		{name: "index walk visits", series: func(g longRunGeneration) int64 { return int64(g.walkVisits) }},
		{name: "index walk value bytes", series: func(g longRunGeneration) int64 { return g.walkValueBytes }},
		{name: "index walk value reads", series: func(g longRunGeneration) int64 { return int64(g.walkValueReads) }},
		{name: "slot table bytes scanned", series: func(g longRunGeneration) int64 { return g.slotScanBytes }},
		{name: "addressed value reads", series: func(g longRunGeneration) int64 { return g.addressedValueReads }},
	}
	for generation := range control {
		for _, face := range faces {
			if got, want := face.series(held[generation]), face.series(control[generation]); got != want {
				return fmt.Errorf("generation %d read %d %s beside the %d the control read over the same work: the injected work names no key, verifies no byte and commits nothing, so no counted face may move",
					generation, got, face.name, want)
			}
		}
		for _, walk := range []struct {
			name  string
			entry longRunGeneration
		}{{name: "held", entry: held[generation]}, {name: "control", entry: control[generation]}} {
			if walk.entry.integrityBytes != walk.entry.journalLogBytes {
				return fmt.Errorf("the %s boot of generation %d verified %d byte(s) of a log holding %d, want one pass over the whole log: the injected work scans no byte of its own",
					walk.name, generation, walk.entry.integrityBytes, walk.entry.journalLogBytes)
			}
		}
	}
	return nil
}

// repeatedWorkSteps answers how many fixed steps one read repeats for the history a root holds: one step per sixteen bytes of the log a generation opens over, so a longer history repeats more work inside the same call.
//   - the size is what separates the held arm from the control at the depth the counter-example walks: a step per kibibyte paid a few tenths of the boot, so the ratio it produced sat inside the machine's own dispersion
//   - the steps stay fixed arithmetic over one field, so the compiler keeps the loop and the work names nothing the engine can answer from
func repeatedWorkSteps(logBytes int64) int {
	return int(logBytes / 16)
}

// noRepeatedWork is the steps of a pair whose roots both boot the plain engine: the control band and the restored shape of the counter-example read through it.
func noRepeatedWork(string, int64) int {
	return 0
}

// engineProbe is the measure face the durable engine states to this anchor: the bytes each pass scanned and the values it read back, beside the pair of clock reads the verification took.
type engineProbe interface {
	IntegrityBytesScanned() int64
	SlotTableBytesScanned() int64
	ValueReads() int64
	IntegrityPassSpan() (time.Time, time.Time)
}

// repeatWorkStore decorates one engine with work proportional to the history its root holds, opening the engine the tier names on its first use.
//   - every read and every walk it answers repeats the steps its root names, and the repeated work names no key, verifies no byte and commits nothing, so no counted face of the boot states it
//   - the engine opens on the first call the boot makes, so the open runs inside the timed assemble exactly as the undecorated open does
//   - the committed order is handed through, so the boot's walks take the path the undecorated engine takes and their counts stay the control's counts
//   - the measure face and the close are handed through, so the boot states the same bytes, values and verification span the undecorated engine states
type repeatWorkStore struct {
	mu     sync.Mutex
	engine ledger.AtomicStore
	root   string
	steps  int
	sink   uint64
}

// opened answers the decorated engine, opening the durable engine of its root on the first call.
func (s *repeatWorkStore) opened(ctx context.Context) (ledger.AtomicStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine != nil {
		return s.engine, nil
	}
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: s.root, WriteRights: true})
	if err != nil {
		return nil, err
	}
	s.engine = store
	return store, nil
}

// probe answers the measure face of the decorated engine, which a boot that opened it states.
func (s *repeatWorkStore) probe() engineProbe {
	s.mu.Lock()
	defer s.mu.Unlock()
	face, held := s.engine.(engineProbe)
	if !held {
		return nil
	}
	return face
}

// repeated spends the steps one read repeats: fixed arithmetic over a field of the decorator, so the work survives and states nothing but itself.
func (s *repeatWorkStore) repeated() {
	for step := 0; step < s.steps; step++ {
		s.sink = s.sink*6364136223846793005 + 1442695040888963407
	}
}

// Read answers the pairs the fetch names after repeating the work this root's history states.
func (s *repeatWorkStore) Read(ctx context.Context, fetch ledger.Fetch) ([]ledger.Pair, error) {
	s.repeated()
	engine, err := s.opened(ctx)
	if err != nil {
		return nil, err
	}
	return engine.Read(ctx, fetch)
}

// Keys answers the keys the prefix names after repeating the work this root's history states.
func (s *repeatWorkStore) Keys(ctx context.Context, prefix string) ([]string, error) {
	s.repeated()
	engine, err := s.opened(ctx)
	if err != nil {
		return nil, err
	}
	return engine.Keys(ctx, prefix)
}

// Namespaces answers the key namespaces the store holds, through the engine this open wraps.
func (s *repeatWorkStore) Namespaces(ctx context.Context) ([]string, error) {
	engine, err := s.opened(ctx)
	if err != nil {
		return nil, err
	}
	return engine.Namespaces(ctx)
}

// Commit applies the batch through the engine this open wraps, committing nothing of its own.
func (s *repeatWorkStore) Commit(ctx context.Context, batch ledger.Batch) error {
	engine, err := s.opened(ctx)
	if err != nil {
		return err
	}
	return engine.Commit(ctx, batch)
}

// EachCommittedNewestFirst hands the walk to the engine's own committed order after repeating the work this root's history states.
func (s *repeatWorkStore) EachCommittedNewestFirst(ctx context.Context, visit func(key string, value []byte) (bool, error)) error {
	engine, err := s.opened(ctx)
	if err != nil {
		return err
	}
	ordered, held := engine.(ledger.CommitOrder)
	if !held {
		return fmt.Errorf("the engine %T states no committed order, so a decorated walk states none", engine)
	}
	s.repeated()
	return ordered.EachCommittedNewestFirst(ctx, visit)
}

// IntegrityBytesScanned answers the bytes the engine's complete-frame check scanned, and a view no boot opened scans none.
func (s *repeatWorkStore) IntegrityBytesScanned() int64 {
	if face := s.probe(); face != nil {
		return face.IntegrityBytesScanned()
	}
	return 0
}

// SlotTableBytesScanned answers the slot table bytes the engine's enumerations read, and a view no boot opened reads none.
func (s *repeatWorkStore) SlotTableBytesScanned() int64 {
	if face := s.probe(); face != nil {
		return face.SlotTableBytesScanned()
	}
	return 0
}

// ValueReads answers the values the engine read back by address, and a view no boot opened reads none.
func (s *repeatWorkStore) ValueReads() int64 {
	if face := s.probe(); face != nil {
		return face.ValueReads()
	}
	return 0
}

// IntegrityPassSpan answers the pair of clock reads the engine's verification took, and a view no boot opened states none.
func (s *repeatWorkStore) IntegrityPassSpan() (time.Time, time.Time) {
	if face := s.probe(); face != nil {
		return face.IntegrityPassSpan()
	}
	return time.Time{}, time.Time{}
}

// Close ends the decorated engine's view, so a boot that opened the engine hands the close to it and a boot that never opened it closes nothing.
func (s *repeatWorkStore) Close() error {
	s.mu.Lock()
	engine := s.engine
	s.engine = nil
	s.mu.Unlock()
	if engine == nil {
		return nil
	}
	closer, held := engine.(interface{ Close() error })
	if !held {
		return nil
	}
	return closer.Close()
}

// decoratedEngine answers the engine the tier of one root opens over: the durable engine of that root, opened on the first call the boot makes, beside the number of steps every read repeats.
func decoratedEngine(root string, steps int) ledger.AtomicStore {
	return &repeatWorkStore{root: root, steps: steps}
}

// TestBootWallClockRejectsUnattributedGrowth is the performance counter-example: a boot repeating work inside a real call pays past the bound.
//   - three roots of one shape each boot the real assemble, commit, settle, file both checkpoints and close over one generation of history
//   - two hold the plain engine and are the control band the target anchor also reads: the same walk twice states this machine's own noise
//   - the held root repeats one fixed step per sixteen bytes of the log it opens, inside the reads the boot really makes, to that root alone
//   - the injected work names no key, verifies no byte and commits nothing: every counted face of every generation reads what the control read
//   - the judgement that accepts the control pair refuses the held pair by name, so identical work is held to a bound a paying boot breaks
//   - the refusal is physical: the held boot pays more beside the control's boot at one depth, with no count and no byte moved
//
// 契约: docs/wiki/memory/memory-architecture.md#gaps
func TestBootWallClockRejectsUnattributedGrowth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-unattributed-growth"}
	const generations, turns, perGeneration = 24, 1, 1
	controlRoot, mirrorRoot, heldRoot := t.TempDir(), t.TempDir(), t.TempDir()
	restored := walkLongRunGenerationsOver(t, ctx, controlRoot, addr, generations, turns, perGeneration, func(int64) int { return 0 })
	mirror := walkLongRunGenerationsOver(t, ctx, mirrorRoot, addr, generations, turns, perGeneration, func(int64) int { return 0 })
	held := walkLongRunGenerationsOver(t, ctx, heldRoot, addr, generations, turns, perGeneration, repeatedWorkSteps)
	for _, walk := range []struct {
		name string
		run  []longRunGeneration
	}{{name: "control", run: restored}, {name: "mirror", run: mirror}, {name: "held", run: held}} {
		if err := judgeLongRunFixture(walk.run, generations, perGeneration); err != nil {
			t.Fatalf("the %s walk proves nothing, so no bound below is a claim about it: %v", walk.name, err)
		}
	}
	if err := judgeCountedFacesUnmoved(restored, held); err != nil {
		t.Fatalf("the injected work moved a counted face of the boot: %v", err)
	}
	t.Logf("the control walk paid: %s", longRunCostSeries(restored))
	t.Logf("the held walk paid: %s", longRunCostSeries(held))
	controlPair := pairedBootRatios(t, ctx, controlRoot, mirrorRoot, dispersionBootPairs, noRepeatedWork)
	heldPair := pairedBootRatios(t, ctx, heldRoot, controlRoot, dispersionBootPairs, func(root string, logBytes int64) int {
		if root != heldRoot {
			return 0
		}
		return repeatedWorkSteps(logBytes)
	})
	t.Logf("the control pair read median %.3f p10 %.3f p90 %.3f and the held pair read median %.3f p10 %.3f p90 %.3f",
		bootRatioPercentile(controlPair, 50), bootRatioPercentile(controlPair, 10), bootRatioPercentile(controlPair, 90),
		bootRatioPercentile(heldPair, 50), bootRatioPercentile(heldPair, 10), bootRatioPercentile(heldPair, 90))
	if err := judgePairedBootGrowth("control pair", "the bound sits inside the dispersion of this machine on the boot axis", controlPair, perGenerationCostBoundTenths); err != nil {
		t.Fatalf("the judgement refused the boot of two roots of one shape walking the same work, so it decides nothing about the held one: %v", err)
	}
	err := judgePairedBootGrowth("held pair", "the boot that repeats work inside its reads pays more than the work the counts state, so the wall clock is not subsumed by the counts", heldPair, perGenerationCostBoundTenths)
	if err == nil {
		t.Fatal("the judgement accepted a boot that repeats work with the history while every counted face stood still, want it refused by name: the wall clock must not be subsumed by the counts")
	}
	if !strings.Contains(err.Error(), "not subsumed by the counts") {
		t.Fatalf("the judgement refused the held pair with %v, want the boot that pays more than its counts named", err)
	}
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

// judgePairedBootGrowth answers why the boot of one root cost more beside its pair than the bound allows, and nil beside a pair whose middle stays inside it.
//   - the ratios pair two roots at the same depth whose walk is the same work, boot against boot, so the series is what the machine adds to identical work on the axis the bound judges
//   - the subject and the consequence are stated by the caller, because two pairs of the same work are refused for two different reasons: two plain roots state the dispersion of the machine, while a root whose engine repeats work inside every read states a boot that pays more than its work
//   - the middle of the pair ratios is the statistic the bound reads: a median over the bound decides the machine rather than the walk
//   - the series takes at least dispersionBootPairs pairs, so a thin sample never stands in for the band
func judgePairedBootGrowth(subject, consequence string, ratios []float64, boundTenths int) error {
	if len(ratios) < dispersionBootPairs {
		return fmt.Errorf("the %s read %d pair(s), want at least the %d the band takes: one sample cannot tell the machine from the walk", subject, len(ratios), dispersionBootPairs)
	}
	middle := bootRatioPercentile(ratios, 50)
	if middle*10 > float64(boundTenths) {
		return fmt.Errorf("the %s paid a boot ratio median of %.3f (p10 %.3f, p90 %.3f) over the bound of %d tenths: %s",
			subject, middle, bootRatioPercentile(ratios, 10), bootRatioPercentile(ratios, 90), boundTenths, consequence)
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
//   - the steps the caller states are handed to the engine of the root they name and to no other, so one arm of the pair carries repeated work while the other stays the plain engine
//   - the order alternates so a drift in the machine's load lands on both sides of the series
//   - each boot adopts the publication record the chain already holds, so the pair is identical work: the difference the pair carries is the whole ratio
func pairedBootRatios(t *testing.T, ctx context.Context, first, second string, pairs int, steps func(root string, logBytes int64) int) []float64 {
	t.Helper()
	boot := func(root string) time.Duration {
		t.Helper()
		storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
		if count := steps(root, journalLogBytesOf(t, root)); count > 0 {
			storage = WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root, Engine: decoratedEngine(root, count)})
		}
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
	integritySpan := make([]time.Duration, 0, len(run))
	rebindSpan := make([]time.Duration, 0, len(run))
	beyond := make([]time.Duration, 0, len(run))
	unattributed := make([]time.Duration, 0, len(run))
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
		integritySpan = append(integritySpan, entry.integritySpan)
		rebindSpan = append(rebindSpan, entry.rebindSpan)
		beyond = append(beyond, entry.bootBeyondVerification())
		unattributed = append(unattributed, entry.bootUnattributed())
	}
	return fmt.Sprintf("total=%v boot=%v commits=%v settling=%v sessions=%v frame=%v closing=%v indexWalkVisits=%v indexWalkValueBytes=%v indexWalkValueReads=%v restored=%v history=%v integrityBytesScanned=%v slotTableBytesScanned=%v addressedValueReads=%v journalLogBytes=%v integritySpan=%v rebindSpan=%v bootBeyondVerification=%v bootUnattributed=%v",
		total, boot, commits, settling, sessions, frame, closing, visits, valueBytes, valueReads, restored, history, integrity, slotScan, addressed, logBytes, integritySpan, rebindSpan, beyond, unattributed)
}

// TestSymptomAnchorPerGenerationCostFlatOverALongRun is the long-run anchor: work costs the same at every depth; the boot's declared reads follow the history.
//   - fixture: one durable root booted three times the generations the timed-out long run reached, one generation of history behind each
//   - each generation boots through the real assemble, commits through CommitToSession, settles through the registry, files both checkpoints and closes
//   - the work stages are judged flat apart: the commits, the settlements, the two checkpoints and the close, each on its own window pair
//   - the boot is judged by its counted faces: the integrity pass one to one, the slot scan face at zero, the addressed reads flat over the chain it serves
//   - the boot's wall clock is judged on the paired axis: two independent roots of one shape pay the same boot inside the bound
//   - that is the reading a boot repeating work in a real call breaks, which TestBootWallClockRejectsUnattributedGrowth holds red
//   - the boot's depth separation is reported beside the account it restores rather than asserted flat: a restart reads the account the chain states
//   - so the boot is held to its counted faces and never to a constant
//   - the boot states the spans of the two stages whose calls take their own pair of clock reads, and each one is held inside the assemble it belongs to
//   - one work stage carrying the history is a defect of that stage, and a sum of the stages hides it behind a stage that stayed flat
//   - the chain is the denominator: every boot restores more facts than the last, and every generation adds tasks to a history the account never restates
//   - assertion: the middle of the last window stays inside the bound of the middle of the first window that opens a store with history, stage by stage
//   - the empty-root generation is held out of both windows, because it restores nothing and so costs a generation over no history
//   - negative: a work stage that carries the history fails the bound naming the stage and the generation; a boot that enumerates the table fails the zero face
//   - control one: an even series is accepted, so the judgement is not a refusal of every series
//   - control two: the measured series scaled by the facts each boot restored is refused, so the bound can fail on a series that stays flat here
//   - control three: the median of 21 paired boots over two independent roots of one shape stays inside the bound on the boot axis, odd pairs reversed
//   - control four: a paired series whose median carries the bound is refused by name, so the guard still refuses a machine the bound cannot decide
//   - the noise band is explicit: the control pairs' p10 to p90 is what the machine adds to identical work, and growth clears it only above that
//   - the bound is a candidate: the calibration that fixes a threshold owns the number, and no definition of done names one
//   - depth: three times the generations the timed-out long run reached, where the ratio reads what dispersion reads and past it clears the bound
//   - proxy: the boot alone is not the whole cost, because a commit reads the journal a growing history left and a checkpoint states its account over it
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
	if err := judgePairedBootGrowth("control pair", "the bound sits inside the dispersion of this machine on the boot axis and a series over it decides nothing", biased, perGenerationCostBoundTenths); err == nil {
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
	controlRatios := pairedBootRatios(t, ctx, firstShallowRoot, secondShallowRoot, dispersionBootPairs, noRepeatedWork)
	t.Logf("the control boot ratios paid median %.3f p10 %.3f p90 %.3f",
		bootRatioPercentile(controlRatios, 50), bootRatioPercentile(controlRatios, 10), bootRatioPercentile(controlRatios, 90))
	if err := judgePairedBootGrowth("control pair", "the bound sits inside the dispersion of this machine on the boot axis and a series over it decides nothing", controlRatios, perGenerationCostBoundTenths); err != nil {
		t.Fatalf("the bound decides the dispersion of this machine rather than the walk: %v", err)
	}
	heldShallowRoot := t.TempDir()
	walkLongRunGenerations(t, ctx, heldShallowRoot, addr, window+1, turns, perGeneration)
	heldRatios := pairedBootRatios(t, ctx, deepRoot, heldShallowRoot, dispersionBootPairs, noRepeatedWork)
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
	if err := judgeStageSpansAccounted(run); err != nil {
		t.Fatalf("a stage the boot states is not accounted inside it: %v (the series below is indexed by generation, 0..%d)\n%s",
			err, generations-1, series)
	}
}

// boundaryWork is what one stage of a recovery generation really spent at the engine
// boundary: the calls the process made and the value bytes those calls carried,
// counted by the view that answered them rather than by a measure the engine keeps.
type boundaryWork struct {
	reads        int
	readPairs    int
	readBytes    int
	enumerations int
	enumerated   int
	walkEntries  int
	walkBytes    int
	commits      int
	commitPairs  int
}

// unmeteredJournal is one root's durable engine behind a view that keeps no measure of its own, counting the calls it really answered.
//   - it states the zero of every count and no verification span
//   - repeat makes every read run the named number of extra times against the engine the view wraps, so a root pays the work of a second read while no measure moves
//   - the duplication traverses and drops its answer, so the caller's own visitor runs once and the boot states the same facts beside the repeated work
//   - the engine opens on the first call, so the open runs inside the stage that pays it exactly as the undecorated open does
type unmeteredJournal struct {
	mu     sync.Mutex
	root   string
	engine ledger.AtomicStore
	repeat int
	stage  string
	work   map[string]*boundaryWork
}

// opened answers the engine the view wraps, opening the durable engine of its root on
// the first call.
func (s *unmeteredJournal) opened(ctx context.Context) (ledger.AtomicStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine != nil {
		return s.engine, nil
	}
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: s.root, WriteRights: true})
	if err != nil {
		return nil, err
	}
	s.engine = store
	return store, nil
}

// begin states the stage the walk is spending, so the calls below are counted there.
func (s *unmeteredJournal) begin(stage string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stage = stage
}

// stages answers the work every stage spent, read after the stage closed.
func (s *unmeteredJournal) stages() map[string]boundaryWork {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]boundaryWork, len(s.work))
	for name, work := range s.work {
		out[name] = *work
	}
	return out
}

// counted answers the work record of the stage the walk named, created on first use.
// The caller holds the lock.
func (s *unmeteredJournal) counted() *boundaryWork {
	work, held := s.work[s.stage]
	if !held {
		work = &boundaryWork{}
		s.work[s.stage] = work
	}
	return work
}

// Read answers the pairs the fetch names after the view counted the call it really made.
func (s *unmeteredJournal) Read(ctx context.Context, fetch ledger.Fetch) ([]ledger.Pair, error) {
	engine, err := s.opened(ctx)
	if err != nil {
		return nil, err
	}
	var pairs []ledger.Pair
	for pass := 0; pass <= s.repeat; pass++ {
		answer, err := engine.Read(ctx, fetch)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		work := s.counted()
		work.reads++
		work.readPairs += len(answer)
		for _, pair := range answer {
			work.readBytes += len(pair.Value)
		}
		s.mu.Unlock()
		pairs = answer
	}
	return pairs, nil
}

// Keys answers the keys the prefix names after the view counted the enumeration.
func (s *unmeteredJournal) Keys(ctx context.Context, prefix string) ([]string, error) {
	engine, err := s.opened(ctx)
	if err != nil {
		return nil, err
	}
	var keys []string
	for pass := 0; pass <= s.repeat; pass++ {
		answer, err := engine.Keys(ctx, prefix)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		work := s.counted()
		work.enumerations++
		work.enumerated += len(answer)
		s.mu.Unlock()
		keys = answer
	}
	return keys, nil
}

// Namespaces answers the namespaces the store holds, counted as an enumeration.
func (s *unmeteredJournal) Namespaces(ctx context.Context) ([]string, error) {
	engine, err := s.opened(ctx)
	if err != nil {
		return nil, err
	}
	names, err := engine.Namespaces(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.counted().enumerations++
	s.mu.Unlock()
	return names, nil
}

// Commit applies the batch through the engine the view wraps, counted as the one write
// the process asked for: the repeated work below is read work, never a second write.
func (s *unmeteredJournal) Commit(ctx context.Context, batch ledger.Batch) error {
	engine, err := s.opened(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	work := s.counted()
	work.commits++
	work.commitPairs += len(batch.Puts)
	s.mu.Unlock()
	return engine.Commit(ctx, batch)
}

// EachCommittedNewestFirst hands the walk to the engine's own committed order after
// the view counted the entries it named, repeating the traversal alone so the visitor
// this process handed in runs once.
func (s *unmeteredJournal) EachCommittedNewestFirst(ctx context.Context, visit func(key string, value []byte) (bool, error)) error {
	engine, err := s.opened(ctx)
	if err != nil {
		return err
	}
	ordered, held := engine.(ledger.CommitOrder)
	if !held {
		return fmt.Errorf("the engine %T states no committed order, so a view beside it states none", engine)
	}
	for pass := 0; pass <= s.repeat; pass++ {
		last := pass == s.repeat
		if err := ordered.EachCommittedNewestFirst(ctx, func(key string, value []byte) (bool, error) {
			s.mu.Lock()
			work := s.counted()
			work.walkEntries++
			work.walkBytes += len(value)
			s.mu.Unlock()
			if !last {
				return true, nil
			}
			return visit(key, value)
		}); err != nil {
			return err
		}
	}
	return nil
}

// IntegrityBytesScanned answers the zero this view states: the engine's own count is
// what a deployment removing it states nowhere.
func (s *unmeteredJournal) IntegrityBytesScanned() int64 { return 0 }

// SlotTableBytesScanned answers the zero this view states.
func (s *unmeteredJournal) SlotTableBytesScanned() int64 { return 0 }

// ValueReads answers the zero this view states.
func (s *unmeteredJournal) ValueReads() int64 { return 0 }

// IntegrityPassSpan answers the empty pair this view states: the verification runs
// behind it, and the pair it took is the measure a deployment switched off.
func (s *unmeteredJournal) IntegrityPassSpan() (time.Time, time.Time) {
	return time.Time{}, time.Time{}
}

// Close ends the view over the engine it wrapped, and closes nothing beside no engine.
func (s *unmeteredJournal) Close() error {
	s.mu.Lock()
	engine := s.engine
	s.engine = nil
	s.mu.Unlock()
	if engine == nil {
		return nil
	}
	closer, held := engine.(interface{ Close() error })
	if !held {
		return nil
	}
	return closer.Close()
}

// unmeteredGeneration is what one generation of the metrics-off walk did and the
// boundary work each stage of it really spent.
type unmeteredGeneration struct {
	restored        int
	committed       int
	journalLogBytes int64
	stages          map[string]boundaryWork
}

// recoveryStages names the stages one generation of the metrics-off walk spends, in
// the order it spends them.
var recoveryStages = []string{"boot", "commits", "closing"}

// walkUnmeteredGenerations boots the named number of generations over one durable root
// through an engine that states no measure of its own, and answers the boundary work
// every stage really spent.
func walkUnmeteredGenerations(t *testing.T, ctx context.Context, root string, addr event.Address, generations, turns int) []unmeteredGeneration {
	t.Helper()
	walk := make([]unmeteredGeneration, generations)
	for generation := 0; generation < generations; generation++ {
		view := &unmeteredJournal{root: root, work: map[string]*boundaryWork{}}
		logBytes := journalLogBytesOf(t, root)
		view.begin("boot")
		boot := noTrimChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root, Engine: view}))
		restored := boot.tier.Segments.EventCount()
		boot.stopCheckpointFiler()
		view.begin("commits")
		commitSessionTurns(t, ctx, boot, addr, fmt.Sprintf("unmetered-%d", generation), turns)
		committed := boot.tier.Segments.EventCount() - restored
		view.begin("closing")
		if _, err := boot.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
			t.Fatalf("close the process of generation %d: %v", generation, err)
		}
		walk[generation] = unmeteredGeneration{
			restored:        restored,
			committed:       committed,
			journalLogBytes: logBytes,
			stages:          view.stages(),
		}
	}
	return walk
}

// unmeteredBoot boots one root through a view that states no measure of its own,
// answers the work its boot stage really spent beside the log it opened, and closes
// without committing, so two boots of one log are the same work.
func unmeteredBoot(t *testing.T, ctx context.Context, root string, repeat int) (boundaryWork, int64) {
	t.Helper()
	view := &unmeteredJournal{root: root, repeat: repeat, work: map[string]*boundaryWork{}}
	logBytes := journalLogBytesOf(t, root)
	view.begin("boot")
	run := noTrimChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root, Engine: view}))
	work := view.stages()["boot"]
	if integrity, slot, values, span, stated := engineMeasures(run.tier); !stated {
		t.Fatalf("the boot holds the engine %T, which states no measure face, so the switched-off state below is not the one judged", run.tier.Store)
	} else if integrity != 0 || slot != 0 || values != 0 || span {
		t.Fatalf("the boot states %d, %d and %d byte(s) beside a stated verification span %t, want the zero of a measure a deployment switched off", integrity, slot, values, span)
	}
	run.stopCheckpointFiler()
	endRun(t, run)
	return work, logBytes
}

// engineMeasures answers what one boot's engine states about its own work: the bytes a
// complete-frame check scanned, the slot table bytes the enumerations read, the values
// read back by address, and whether a verification span stands at all.
func engineMeasures(tier *ledger.Tier) (integrity, slot, values int64, span bool, stated bool) {
	probe, held := tier.Store.(interface {
		IntegrityBytesScanned() int64
		SlotTableBytesScanned() int64
		ValueReads() int64
		IntegrityPassSpan() (time.Time, time.Time)
	})
	if !held {
		return 0, 0, 0, false, false
	}
	begin, end := probe.IntegrityPassSpan()
	return probe.IntegrityBytesScanned(), probe.SlotTableBytesScanned(), probe.ValueReads(), !begin.IsZero() || !end.IsZero(), true
}

// judgeUnmeteredFixture answers why one walk proves nothing about recovery over a
// growing chain, and nil beside a walk whose chain really grew.
func judgeUnmeteredFixture(walk []unmeteredGeneration, generations int) error {
	if len(walk) != generations {
		return fmt.Errorf("the walk booted %d generation(s), want the %d it was asked for", len(walk), generations)
	}
	if walk[0].restored != 0 {
		return fmt.Errorf("the first generation boots a store holding %d fact(s), want the empty root a walk starts on", walk[0].restored)
	}
	for generation := 1; generation < len(walk); generation++ {
		if walk[generation].restored <= walk[generation-1].restored {
			return fmt.Errorf("the boot of generation %d restored %d fact(s) beside the %d its predecessor restored, want a chain that grew", generation, walk[generation].restored, walk[generation-1].restored)
		}
	}
	if last := walk[len(walk)-1].journalLogBytes; last <= walk[0].journalLogBytes {
		return fmt.Errorf("the last generation opened a log of %d byte(s) beside the %d the first opened, want a log that grew", last, walk[0].journalLogBytes)
	}
	return nil
}

// judgeRecoveryWorkUnmoved answers why one stage really spent more at the engine boundary than another over the same work, and nil beside two readings whose every count agrees.
//   - the counts are what the process asked the engine for, so a repeated read moves them whether or not any measure the engine keeps states it
//   - every face is compared, so a reading that hides its growth behind an unchanged total is refused by name
func judgeRecoveryWorkUnmoved(name string, control, held map[string]boundaryWork) error {
	faces := []struct {
		face  string
		count func(boundaryWork) int
	}{
		{face: "reads", count: func(w boundaryWork) int { return w.reads }},
		{face: "read pairs", count: func(w boundaryWork) int { return w.readPairs }},
		{face: "read bytes", count: func(w boundaryWork) int { return w.readBytes }},
		{face: "enumerations", count: func(w boundaryWork) int { return w.enumerations }},
		{face: "enumerated keys", count: func(w boundaryWork) int { return w.enumerated }},
		{face: "walk entries", count: func(w boundaryWork) int { return w.walkEntries }},
		{face: "walk bytes", count: func(w boundaryWork) int { return w.walkBytes }},
		{face: "commits", count: func(w boundaryWork) int { return w.commits }},
		{face: "commit pairs", count: func(w boundaryWork) int { return w.commitPairs }},
	}
	stages := make([]string, 0, len(control))
	for stage := range control {
		stages = append(stages, stage)
	}
	sort.Strings(stages)
	for _, stage := range stages {
		want, held := held[stage]
		if !held {
			return fmt.Errorf("the %s reading states no work for the stage %q the control states, want the calls that stage really made", name, stage)
		}
		for _, face := range faces {
			if got := face.count(want); got != face.count(control[stage]) {
				return fmt.Errorf("the %s stage %q asked the engine for %d %s beside the %d of the control over the same work: the work the process really spent grew while no measure states it",
					name, stage, got, face.face, face.count(control[stage]))
			}
		}
	}
	return nil
}

// judgeRecoveryStagesRan answers why one generation states no work for a stage the walk really spends, and nil beside a reading that names the calls of every stage.
//   - a stage the record does not hold at all is a stage the account dropped, so it is refused by name instead of read as a stage the process skipped
//   - a boot over a log holding bytes reads, enumerates or walks, so a boot stating none of them restored nothing
//   - a generation that committed asked for at least one write
func judgeRecoveryStagesRan(walk []unmeteredGeneration) error {
	for generation, entry := range walk {
		for _, stage := range recoveryStages {
			work, stated := entry.stages[stage]
			if !stated {
				return fmt.Errorf("the generation %d states no work for the stage %q the walk spends, want the calls that stage made: a stage an account drops is no stage the process skipped", generation, stage)
			}
			if stage == "boot" && entry.journalLogBytes > 0 && work.reads == 0 && work.enumerations == 0 && work.walkEntries == 0 {
				return fmt.Errorf("the boot of generation %d states no read, no enumeration and no walk over a log holding %d byte(s), want the facts it restored", generation, entry.journalLogBytes)
			}
			if stage == "commits" && entry.committed > 0 && work.commits == 0 {
				return fmt.Errorf("the commits of generation %d state no write beside the %d fact(s) the chain grew, want the commits that stage made", generation, entry.committed)
			}
		}
	}
	return nil
}

// withoutStage answers one walk with the work of one stage dropped from one generation,
// which is the shape of an account that stops stating a stage it really spends.
func withoutStage(walk []unmeteredGeneration, at int, stage string) []unmeteredGeneration {
	out := slices.Clone(walk)
	stages := make(map[string]boundaryWork, len(out[at].stages))
	for name, work := range out[at].stages {
		if name == stage {
			continue
		}
		stages[name] = work
	}
	out[at].stages = stages
	return out
}

// TestRecoveryCostDetectsGrowthWithInstrumentationDisabled is the growth counter-example of a deployment that keeps no measure of its own.
//   - fixture: three generations boot, commit and close over one durable root, so every boot restores more facts than the one before it
//   - every generation is booted through a view that states the zero of every count and no verification span, so no measure below can be what catches a growth
//   - the view counts the calls the process really made: the reads, the enumerations and the entries of every walk, stage by stage
//   - two boots of one log ask for the same work, so the judgement holds them equal and is no blanket refusal
//   - a third boot of that log repeats every read: the judgement refuses it by name
//   - the counts the process asked for are the face a switched-off measure leaves
//   - a reading that drops a stage is refused by name rather than read as a stage the process skipped, so no walked stage is folded into "took no time"
//   - the guarantee the switch does not touch: an engine stating its measures still verifies the whole log it opens, and the log it opened is the one on disk
//
// 契约: docs/wiki/memory/memory-architecture.md#gaps
func TestRecoveryCostDetectsGrowthWithInstrumentationDisabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-unmetered-cost"}
	const generations, turns = 3, 1
	root := t.TempDir()
	walk := walkUnmeteredGenerations(t, ctx, root, addr, generations, turns)
	if err := judgeUnmeteredFixture(walk, generations); err != nil {
		t.Fatalf("the walk proves nothing, so no reading below is a claim about a growing chain: %v", err)
	}
	if err := judgeRecoveryStagesRan(walk); err != nil {
		t.Fatalf("a stage the walk really spends states no work: %v", err)
	}
	t.Logf("the metrics-off walk read %d value(s) and walked %d entry(ies) at the deepest boot",
		walk[len(walk)-1].stages["boot"].reads, walk[len(walk)-1].stages["boot"].walkEntries)

	plain, logBytes := unmeteredBoot(t, ctx, root, 0)
	mirror, mirrorBytes := unmeteredBoot(t, ctx, root, 0)
	if mirrorBytes != logBytes {
		t.Fatalf("the second boot opened a log of %d byte(s) beside the %d the first opened, want one boot to commit nothing", mirrorBytes, logBytes)
	}
	control := map[string]boundaryWork{"boot": plain}
	if err := judgeRecoveryWorkUnmoved("control", control, map[string]boundaryWork{"boot": mirror}); err != nil {
		t.Fatalf("the judgement refused two boots of one log, so it decides nothing about the work a boot repeats: %v", err)
	}
	held, heldBytes := unmeteredBoot(t, ctx, root, 1)
	if heldBytes != logBytes {
		t.Fatalf("the repeated boot opened a log of %d byte(s) beside the %d the control opened, want the same log", heldBytes, logBytes)
	}
	err := judgeRecoveryWorkUnmoved("held", control, map[string]boundaryWork{"boot": held})
	if err == nil {
		t.Fatal("the judgement accepted a boot that read the log twice beside every measure switched off, want it refused by name: the counts the process asks for are the only face left")
	}
	if !strings.Contains(err.Error(), "no measure states it") {
		t.Fatalf("the judgement refused the repeated boot with %v, want the work the process repeated named", err)
	}
	t.Logf("the repeated boot was refused with: %v", err)

	dropped := withoutStage(walk, len(walk)-1, "commits")
	if err := judgeRecoveryStagesRan(dropped); err == nil {
		t.Fatal("the judgement accepted a generation stating no work for a stage it spends, want it refused by name")
	} else if !strings.Contains(err.Error(), "states no work for the stage") {
		t.Fatalf("the judgement refused the dropped stage with %v, want the dropped stage named", err)
	} else {
		t.Logf("the dropped stage was refused with: %v", err)
	}

	guard := journalLogBytesOf(t, root)
	instrumented := noTrimChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	integrity, slot, values, span, stated := engineMeasures(instrumented.tier)
	if !stated {
		t.Fatalf("the boot holds the engine %T, which states no measure face, so the guarantee below is read on no engine", instrumented.tier.Store)
	}
	if integrity != guard {
		t.Fatalf("the engine verified %d byte(s) of a log holding %d, want one pass over the whole log: switching the measures off is no licence to skip the check (%d slot byte(s), %d value(s), span stated %t)",
			integrity, guard, slot, values, span)
	}
	instrumented.stopCheckpointFiler()
	endRun(t, instrumented)
}
