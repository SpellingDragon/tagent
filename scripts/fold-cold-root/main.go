// Command fold-cold-root states the facts a root holds across its hot and cold
// journals in a new root, in the commit order the index entries of those facts
// carry. A build that reads one journal cannot serve a root whose cold journal
// holds part of that history, so an operator folds such a root before this build
// serves it: the hot journal is `kv/journal.log` under the root and the cold one is
// `cold/kv/journal.log`.
//
// The fold is a one-time move outside the framework: the source root is read-only,
// the target root states no key, and a source the fold cannot read whole is refused
// rather than read as complete.
//
//   - scan: without --restore the run writes nothing, stating the keys the folded root would hold and naming the ones it leaves behind
//   - restore: the run holds the write rights of -root, states at most foldedBatchKeys original keys per commit, and reads every identity back through a freshly opened engine
//   - refusals: ledger.ErrMigrateSourceUnchanged, ErrSourceUnstated, ErrSourceConflict, ErrSourceIncomplete, ledger.ErrRootWriteLocked, ledger.ErrBatchNotAtomic
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

const (
	// kvDirName is the directory under a storage root that holds a journal log.
	kvDirName = "kv"
	// coldDirName is the directory under a storage root that holds the journal of
	// the facts that left it.
	coldDirName = "cold"
	// journalFileName is the one file a journal directory holds.
	journalFileName = "journal.log"
	// segmentIndexPrefix names the stored index entry of one event key inside its
	// partition namespace.
	segmentIndexPrefix = "idx:"
	// storeSeatKey names the key the segment store states its event count and
	// commit sequence under.
	storeSeatKey = "store:seat"
	// settledCommitKey names the key a root states the greatest commit of the cold
	// journal under.
	settledCommitKey = "settled:commit"
	// settledEventsKey names the key a root states the number of events that left
	// it under.
	settledEventsKey = "settled:events"
	// blobPrefix names the independent body of one original.
	blobPrefix = "blob:"
	// refPrefix names the reference record of one original.
	refPrefix = "ref:"
	// foldedBatchKeys bounds how many original keys one commit carries, so folding
	// a large root costs bounded memory.
	foldedBatchKeys = 256
)

// ErrSourceUnstated reports a source root that holds no hot journal, the one
// directory every durable root of this build writes.
var ErrSourceUnstated = errors.New("fold-cold-root: the source root holds no hot journal")

// ErrSourceConflict reports a source root that states one identity two ways: one key
// the two journals hold with different bytes, one commit two events claim, or a value
// the key's own record cannot decode.
var ErrSourceConflict = errors.New("fold-cold-root: the source states one identity two ways")

// ErrSourceIncomplete reports a source root that holds part of an event: an index
// entry with no body or no window meta, a body or a window meta no index entry
// names, or a cold journal beside no statement of the commits it holds.
var ErrSourceIncomplete = errors.New("fold-cold-root: the source holds part of an event")

// sourceTier is one journal directory a source root may hold.
type sourceTier struct {
	// Name is the place the report gives the journal.
	Name string
	// Dir is the directory under the source root that holds the log.
	Dir string
}

// sourceTiers are the journal directories a source root may hold, in the order the
// report names them.
var sourceTiers = []sourceTier{
	{Name: "hot", Dir: kvDirName},
	{Name: "cold", Dir: filepath.Join(coldDirName, kvDirName)},
}

// sourcePair is one key a source root states, beside the journal that holds it.
type sourcePair struct {
	ledger.Pair
	// Tier names the journal the key was read from.
	Tier string
}

// carriedCommit is the part of a segment index entry the fold orders by: the
// store-wide commit sequence the commit of that entry was assigned.
type carriedCommit struct {
	Commit int64 `json:"commit"`
}

// storedEntry is one `{pid}:idx:{event}` entry as a source root states it.
type storedEntry struct {
	// Event is the event identity the entry names.
	Event string
	// Partition is the partition the entry was filed under.
	Partition int
	// Window is the time window the event was committed into.
	Window int64
	// Seq is the sequence slot the event takes inside that window.
	Seq int64
	// Commit is the store-wide commit sequence the entry states.
	Commit int64
	// Value is the entry's bytes, kept as the source states them.
	Value []byte
	// Tier names the journal that holds the entry.
	Tier string
}

// foldedEvent is one event the fold states: the body, the index entry and the window
// meta of one commit, in the bytes the source holds them in.
type foldedEvent struct {
	// Key is the event identity the index entry names.
	Key event.EventID
	// Commit is the store-wide commit sequence the index entry carries.
	Commit int64
	// Body is the event body the source holds under the entry's window and slot.
	Body []byte
	// Tier names the journal that holds the index entry.
	Tier string
	// Puts are the three keys one commit of the segment store writes.
	Puts []ledger.Pair
}

// foldPlan is what one source root states as the facts to fold.
type foldPlan struct {
	// Events holds one entry per event, at the commit sequence it was given,
	// oldest first.
	Events []foldedEvent
	// Originals holds the blob: and ref: keys, in key order.
	Originals []ledger.Pair
	// Seat is the store:seat pair the source states.
	Seat ledger.Pair
	// Seated reports whether the source states a seat.
	Seated bool
	// Left holds the keys the folded root does not state, with the value the source
	// holds for each.
	Left []ledger.Pair
	// Superseded names the window metas the hot journal states otherwise than the
	// cold copy of the same window.
	Superseded []string
	// Tiers names the journals the source root holds.
	Tiers []string
}

// foldReport is what one restore committed.
type foldReport struct {
	// Folded is the number of events committed.
	Folded int
	// Written is the number of original keys committed.
	Written int
}

// keyCount answers how many distinct keys the fold states, one window meta being
// one key however many events of its window carry it.
func (p foldPlan) keyCount() int {
	keys := make(map[string]struct{}, len(p.Originals)+3*len(p.Events))
	for _, evt := range p.Events {
		for _, put := range evt.Puts {
			keys[put.Key] = struct{}{}
		}
	}
	for _, pair := range p.Originals {
		keys[pair.Key] = struct{}{}
	}
	if p.Seated {
		keys[p.Seat.Key] = struct{}{}
	}
	return len(keys)
}

func main() {
	source := flag.String("source", "", "root the fold reads, holding kv/ and cold/kv/")
	root := flag.String("root", "", "new root the fold states the facts in; -restore writes it")
	restore := flag.Bool("restore", false, "commit the facts into the new root; without it the scan writes nothing")
	flag.Parse()
	if *source == "" || *root == "" {
		fail(fmt.Errorf("-source and -root are required"))
	}
	plan, folded, err := foldRoot(context.Background(), *source, *root, *restore)
	if err != nil {
		fail(err)
	}
	printReport(*source, *root, *restore, plan, folded)
}

// foldRoot reads the journals of sourceRoot as the facts they state and, when
// restore is set, commits them into targetRoot and reads them back.
//   - The plan holds the events of both journals at the commit sequence their index entries carry, oldest first, so a newest-first walk of the folded root states the order the source's commits landed in
//   - restore=false writes nothing: the source root is only read and targetRoot is not opened
//   - restore=true declares the write rights of targetRoot, is refused by name while another writer holds that root, and commits into a root that holds no key
//   - The source and the target must name two directories, so the fold never writes the root it reads
//   - The settled: keys state how far the cold journal reaches, and the folded root states the facts alone: the plan names them as keys it does not hold
func foldRoot(ctx context.Context, sourceRoot, targetRoot string, restore bool) (foldPlan, foldReport, error) {
	if filepath.Clean(sourceRoot) == filepath.Clean(targetRoot) {
		return foldPlan{}, foldReport{}, fmt.Errorf("%w: the source and the target are one directory %s",
			ledger.ErrMigrateTargetNotEmpty, targetRoot)
	}
	pairs, tiers, err := readSource(ctx, sourceRoot)
	if err != nil {
		return foldPlan{}, foldReport{}, err
	}
	plan, err := classify(pairs)
	if err != nil {
		return foldPlan{}, foldReport{}, err
	}
	plan.Tiers = tiers
	if !restore {
		return plan, foldReport{}, nil
	}
	if err := commitFold(ctx, targetRoot, plan); err != nil {
		return plan, foldReport{}, err
	}
	if err := verifyFold(ctx, targetRoot, plan); err != nil {
		return plan, foldReport{}, err
	}
	return plan, foldReport{Folded: len(plan.Events), Written: len(plan.Originals)}, nil
}

// readSource reads every key a source root states across its journal directories,
// beside the names of the journals it holds.
//   - The hot journal is required and the cold one is optional, so a root that moved nothing reads as its own facts
//   - A log is read where it lies, through a view that holds no write rights: it creates no file, cuts no torn tail, rebuilds no index file
//   - An index this build cannot place costs that view a replay of the whole log, so the root keeps every byte it holds
//   - A log whose size or modification time moved under the read is refused, so a fold never states half of a root that is still being written
func readSource(ctx context.Context, sourceRoot string) ([]sourcePair, []string, error) {
	var pairs []sourcePair
	var tiers []string
	for _, tier := range sourceTiers {
		held, err := readTier(ctx, sourceRoot, tier)
		if err != nil {
			return nil, nil, err
		}
		if len(held) > 0 {
			tiers = append(tiers, tier.Name)
		}
		pairs = append(pairs, held...)
	}
	return pairs, tiers, nil
}

// readTier reads the keys one journal log of a source root holds, through a view that
// holds no write rights over the directory stating that journal.
func readTier(ctx context.Context, sourceRoot string, tier sourceTier) ([]sourcePair, error) {
	dir := filepath.Join(sourceRoot, tier.Dir)
	logPath := filepath.Join(dir, journalFileName)
	before, err := os.Stat(logPath)
	if errors.Is(err, os.ErrNotExist) {
		if tier.Name != sourceTiers[0].Name {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrSourceUnstated, dir)
	}
	if err != nil {
		return nil, fmt.Errorf("fold-cold-root: read the log of %s: %w", dir, err)
	}
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{
		Type:        ledger.StorageJournal,
		Root:        filepath.Dir(dir),
		WriteRights: false,
	})
	if err != nil {
		return nil, fmt.Errorf("fold-cold-root: open the journal of %s: %w", dir, err)
	}
	held, readErr := store.Read(ctx, ledger.Fetch{})
	closeErr := closeStore(store)
	if readErr != nil {
		return nil, fmt.Errorf("fold-cold-root: read the journal of %s: %w", dir, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("fold-cold-root: close the journal of %s: %w", dir, closeErr)
	}
	after, err := os.Stat(logPath)
	if err != nil {
		return nil, fmt.Errorf("fold-cold-root: read the log of %s: %w", dir, err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("%w: %s", ledger.ErrMigrateSourceUnchanged, logPath)
	}
	pairs := make([]sourcePair, 0, len(held))
	for _, pair := range held {
		pairs = append(pairs, sourcePair{Pair: pair, Tier: tier.Name})
	}
	return pairs, nil
}

// closeStore releases the engine view behind an atomic store.
func closeStore(store ledger.AtomicStore) error {
	closer, held := store.(interface{ Close() error })
	if !held {
		return nil
	}
	return closer.Close()
}

// classify answers the keys of one source read as the plan the fold states.
//   - One key both journals state must carry the same bytes, and two events must not claim one commit
//   - Every index entry names a body and a window meta the source holds, every body and window meta is named by an index entry, and every ref: key has its blob: body
//   - settled:commit states the greatest commit of the cold journal, and a cold journal beside no such statement is refused
//   - A window meta the hot journal restates takes the hot statement, and the plan names the key it replaced
//   - A key of no family the segment store writes is refused rather than dropped
func classify(pairs []sourcePair) (foldPlan, error) {
	var plan foldPlan
	held, err := mergePairs(pairs, &plan.Superseded)
	if err != nil {
		return foldPlan{}, err
	}
	entries := map[string]storedEntry{}
	bodies := map[string]sourcePair{}
	metas := map[string]sourcePair{}
	for _, key := range sortedKeys(held) {
		pair := held[key]
		switch {
		case key == storeSeatKey:
			plan.Seat, plan.Seated = pair.Pair, true
		case key == settledCommitKey || key == settledEventsKey:
			plan.Left = append(plan.Left, pair.Pair)
		case strings.HasPrefix(key, blobPrefix), strings.HasPrefix(key, refPrefix):
			plan.Originals = append(plan.Originals, pair.Pair)
		case isBodyKey(key):
			bodies[key] = pair
		case isMetaKey(key):
			metas[key] = pair
		default:
			entry, named, err := indexEntryOf(pair)
			if err != nil {
				return foldPlan{}, err
			}
			if !named {
				return foldPlan{}, fmt.Errorf("%w: %q belongs to no key family the segment store writes", ErrSourceIncomplete, key)
			}
			entries[entry.Event] = entry
		}
	}
	usedBodies := map[string]struct{}{}
	usedMetas := map[string]struct{}{}
	for _, eventKey := range sortedKeys(entries) {
		entry := entries[eventKey]
		bodyKey := bodyKeyOf(entry)
		body, heldBody := bodies[bodyKey]
		if !heldBody {
			return foldPlan{}, fmt.Errorf("%w: %q names the body %s, which no journal holds", ErrSourceIncomplete, eventKey, bodyKey)
		}
		metaKey := metaKeyOf(entry)
		meta, heldMeta := metas[metaKey]
		if !heldMeta {
			return foldPlan{}, fmt.Errorf("%w: %q names the window meta %s, which no journal holds", ErrSourceIncomplete, eventKey, metaKey)
		}
		usedBodies[bodyKey] = struct{}{}
		usedMetas[metaKey] = struct{}{}
		plan.Events = append(plan.Events, foldedEvent{
			Key:    event.EventID(eventKey),
			Commit: entry.Commit,
			Body:   body.Value,
			Tier:   entry.Tier,
			Puts:   []ledger.Pair{body.Pair, {Key: partitionIndexKeyOf(entry), Value: entry.Value}, meta.Pair},
		})
	}
	for _, bodyKey := range sortedKeys(bodies) {
		if _, used := usedBodies[bodyKey]; !used {
			return foldPlan{}, fmt.Errorf("%w: %s is an event body no index entry names", ErrSourceIncomplete, bodyKey)
		}
	}
	for _, metaKey := range sortedKeys(metas) {
		if _, used := usedMetas[metaKey]; !used {
			return foldPlan{}, fmt.Errorf("%w: %s is a window meta whose window holds no event", ErrSourceIncomplete, metaKey)
		}
	}
	if err := orderEvents(&plan); err != nil {
		return foldPlan{}, err
	}
	if err := checkSettled(held, plan); err != nil {
		return foldPlan{}, err
	}
	if err := checkOriginals(plan); err != nil {
		return foldPlan{}, err
	}
	return plan, nil
}

// mergePairs answers one pair per key the source read holds, and refuses one key the
// two journals state with different bytes.
//   - A window meta the two journals state two ways takes the hot journal's statement, because a window meta is an aggregate the hot log restates as its events land while the cold copy stays at the value it was copied with
//   - Every other key the two journals state two ways is refused: one identity carries one immutable fact
func mergePairs(pairs []sourcePair, superseded *[]string) (map[string]sourcePair, error) {
	held := make(map[string]sourcePair, len(pairs))
	for _, pair := range pairs {
		prior, seen := held[pair.Key]
		if !seen {
			held[pair.Key] = pair
			continue
		}
		if bytes.Equal(prior.Value, pair.Value) {
			continue
		}
		if !isMetaKey(pair.Key) {
			return nil, fmt.Errorf("%w: %q reads one way in the %s journal and the other in the %s journal",
				ErrSourceConflict, pair.Key, prior.Tier, pair.Tier)
		}
		*superseded = append(*superseded, pair.Key)
		if pair.Tier == sourceTiers[0].Name {
			held[pair.Key] = pair
		}
	}
	return held, nil
}

// orderEvents puts the plan's events at their commit sequence, oldest first, and
// refuses two events that claim one commit.
func orderEvents(plan *foldPlan) error {
	sort.SliceStable(plan.Events, func(i, j int) bool { return plan.Events[i].Commit < plan.Events[j].Commit })
	for at := 1; at < len(plan.Events); at++ {
		prior, evt := plan.Events[at-1], plan.Events[at]
		if prior.Commit == evt.Commit {
			return fmt.Errorf("%w: %q and %q both state commit %d", ErrSourceConflict, prior.Key, evt.Key, evt.Commit)
		}
	}
	return nil
}

// checkSettled verifies that the commit statement of the hot journal agrees with the
// greatest commit the cold journal states.
func checkSettled(held map[string]sourcePair, plan foldPlan) error {
	greatest := int64(0)
	for _, evt := range plan.Events {
		if evt.Tier != sourceTiers[1].Name {
			continue
		}
		if evt.Commit > greatest {
			greatest = evt.Commit
		}
	}
	stated, heldKey := held[settledCommitKey]
	if !heldKey {
		if greatest > 0 {
			return fmt.Errorf("%w: the cold journal holds commit %d beside no %s statement", ErrSourceIncomplete, greatest, settledCommitKey)
		}
		return nil
	}
	commits, err := strconv.ParseInt(strings.TrimSpace(string(stated.Value)), 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %s states %q", ErrSourceConflict, settledCommitKey, stated.Value)
	}
	if commits != greatest {
		return fmt.Errorf("%w: %s states %d beside a cold journal whose greatest commit is %d",
			ErrSourceConflict, settledCommitKey, commits, greatest)
	}
	return nil
}

// checkOriginals refuses a reference whose independent body the source does not hold.
func checkOriginals(plan foldPlan) error {
	bodies := make(map[string]struct{}, len(plan.Originals))
	for _, pair := range plan.Originals {
		if name, named := strings.CutPrefix(pair.Key, blobPrefix); named {
			bodies[name] = struct{}{}
		}
	}
	for _, pair := range plan.Originals {
		name, named := strings.CutPrefix(pair.Key, refPrefix)
		if !named {
			continue
		}
		if _, paired := bodies[name]; !paired {
			return fmt.Errorf("%w: %s names a body the source does not hold", ledger.ErrSnapshotCorrupt, pair.Key)
		}
	}
	return nil
}

// indexEntryOf answers one stored index entry, and false beside a key of another
// family.
//   - The value states the window and the sequence slot, then the material the commit carried as JSON
func indexEntryOf(pair sourcePair) (storedEntry, bool, error) {
	partition, rest, held := strings.Cut(pair.Key, ":")
	if !held {
		return storedEntry{}, false, nil
	}
	eventKey, named := strings.CutPrefix(rest, segmentIndexPrefix)
	if !named || eventKey == "" {
		return storedEntry{}, false, nil
	}
	pid, err := strconv.Atoi(partition)
	if err != nil {
		return storedEntry{}, false, fmt.Errorf("%w: %q names partition %q", ErrSourceConflict, pair.Key, partition)
	}
	fields := strings.SplitN(string(pair.Value), ":", 3)
	if len(fields) != 3 {
		return storedEntry{}, false, fmt.Errorf("%w: the value of %q states no window, slot and material", ErrSourceConflict, pair.Key)
	}
	window, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return storedEntry{}, false, fmt.Errorf("%w: the value of %q names the window %q", ErrSourceConflict, pair.Key, fields[0])
	}
	seq, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return storedEntry{}, false, fmt.Errorf("%w: the value of %q names the slot %q", ErrSourceConflict, pair.Key, fields[1])
	}
	var carried carriedCommit
	if err := json.Unmarshal([]byte(fields[2]), &carried); err != nil {
		return storedEntry{}, false, fmt.Errorf("%w: the material of %q: %w", ErrSourceConflict, pair.Key, err)
	}
	return storedEntry{Event: eventKey, Partition: pid, Window: window, Seq: seq,
		Commit: carried.Commit, Value: pair.Value, Tier: pair.Tier}, true, nil
}

// isBodyKey reports a key naming one event body: its partition, the body marker and
// the window and slot inside it.
func isBodyKey(key string) bool {
	fields := strings.Split(key, ":")
	return len(fields) == 4 && fields[1] == "evt"
}

// isMetaKey reports a key naming one window meta: its partition, the meta marker and
// the window.
func isMetaKey(key string) bool {
	fields := strings.Split(key, ":")
	return len(fields) == 3 && fields[1] == "meta"
}

// bodyKeyOf answers the body key of one index entry.
func bodyKeyOf(entry storedEntry) string {
	return fmt.Sprintf("%d:evt:%d:%d", entry.Partition, entry.Window, entry.Seq)
}

// metaKeyOf answers the window meta key of one index entry.
func metaKeyOf(entry storedEntry) string {
	return fmt.Sprintf("%d:meta:%d", entry.Partition, entry.Window)
}

// partitionIndexKeyOf answers the stored index key of one index entry.
func partitionIndexKeyOf(entry storedEntry) string {
	return fmt.Sprintf("%d:%s%s", entry.Partition, segmentIndexPrefix, entry.Event)
}

// commitFold states the plan in targetRoot, one frame per event at the commit
// sequence the source stated, so the frame order of the folded root is that
// sequence.
//   - The target must hold no key: the fold writes a root rather than merging into one
//   - The original keys are committed in bounded batches after the events, and a batch that does not land names its first key
func commitFold(ctx context.Context, targetRoot string, plan foldPlan) (err error) {
	store, openErr := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: targetRoot, WriteRights: true})
	if openErr != nil {
		return fmt.Errorf("fold-cold-root: open the new root %s: %w", targetRoot, openErr)
	}
	defer func() {
		err = errors.Join(err, closeStore(store))
	}()
	held, err := store.Keys(ctx, "")
	if err != nil {
		return fmt.Errorf("fold-cold-root: enumerate the new root %s: %w", targetRoot, err)
	}
	if len(held) > 0 {
		return fmt.Errorf("%w: %s holds %d keys", ledger.ErrMigrateTargetNotEmpty, targetRoot, len(held))
	}
	for _, evt := range plan.Events {
		if err := store.Commit(ctx, ledger.Batch{Puts: evt.Puts}); err != nil {
			return fmt.Errorf("fold-cold-root: commit the event %q: %w", evt.Key, err)
		}
	}
	if plan.Seated {
		if err := store.Commit(ctx, ledger.Batch{Puts: []ledger.Pair{plan.Seat}}); err != nil {
			return fmt.Errorf("fold-cold-root: commit the seat of %s: %w", targetRoot, err)
		}
	}
	batch := ledger.Batch{Puts: make([]ledger.Pair, 0, foldedBatchKeys)}
	commitBatch := func() error {
		if len(batch.Puts) == 0 {
			return nil
		}
		if err := store.Commit(ctx, batch); err != nil {
			return fmt.Errorf("fold-cold-root: commit the original %q: %w", batch.Puts[0].Key, err)
		}
		batch = ledger.Batch{Puts: make([]ledger.Pair, 0, foldedBatchKeys)}
		return nil
	}
	for _, original := range plan.Originals {
		batch.Puts = append(batch.Puts, original)
		if len(batch.Puts) < foldedBatchKeys {
			continue
		}
		if err := commitBatch(); err != nil {
			return err
		}
	}
	if err := commitBatch(); err != nil {
		return err
	}
	return nil
}

// verifyFold reads the folded root through a freshly opened tier and names the first
// identity whose bytes, count or commit order the fold did not state.
func verifyFold(ctx context.Context, targetRoot string, plan foldPlan) error {
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: targetRoot})
	if err != nil {
		return fmt.Errorf("fold-cold-root: open the folded root %s: %w", targetRoot, err)
	}
	defer tier.Segments.Close()
	for _, evt := range plan.Events {
		stored, held, err := tier.Segments.GetEvent(ctx, evt.Key)
		if err != nil {
			return fmt.Errorf("fold-cold-root: read the event %q of %s: %w", evt.Key, targetRoot, err)
		}
		if !held {
			return fmt.Errorf("fold-cold-root: the folded root does not hold the event %q", evt.Key)
		}
		if !bytes.Equal(stored.Body, evt.Body) {
			return fmt.Errorf("fold-cold-root: the folded root holds other bytes for the event %q", evt.Key)
		}
	}
	if count := tier.Segments.EventCount(); count != len(plan.Events) {
		return fmt.Errorf("fold-cold-root: the folded root counts %d events beside the %d the source stated", count, len(plan.Events))
	}
	if err := verifyOrder(ctx, tier, plan); err != nil {
		return err
	}
	bodies := make(map[string][]byte, len(plan.Originals))
	for _, original := range plan.Originals {
		pairs, err := tier.Store.Read(ctx, ledger.Fetch{Key: original.Key})
		if err != nil {
			return fmt.Errorf("fold-cold-root: read %q of %s: %w", original.Key, targetRoot, err)
		}
		if len(pairs) != 1 || !bytes.Equal(pairs[0].Value, original.Value) {
			return fmt.Errorf("fold-cold-root: the folded root holds other bytes for %q", original.Key)
		}
		if name, named := strings.CutPrefix(original.Key, blobPrefix); named {
			bodies[name] = original.Value
		}
	}
	return verifyOriginals(ctx, tier, plan, bodies)
}

// verifyOrder walks the index of the folded root newest first and names the first
// event whose place differs from the commit order the source stated.
func verifyOrder(ctx context.Context, tier *ledger.Tier, plan foldPlan) error {
	var walked []string
	visit := func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) error {
		walked = append(walked, string(key))
		return nil
	}
	err := tier.Segments.EachIndexNewestFirst(ctx, visit)
	if err != nil {
		return fmt.Errorf("fold-cold-root: walk the commit order of the folded root: %w", err)
	}
	if len(walked) != len(plan.Events) {
		return fmt.Errorf("fold-cold-root: the folded root states %d index entries beside the %d events of the source",
			len(walked), len(plan.Events))
	}
	for at, key := range walked {
		want := plan.Events[len(plan.Events)-1-at].Key
		if key != string(want) {
			return fmt.Errorf("fold-cold-root: the folded root states %q at place %d of its commit order beside the source's %q",
				key, at, want)
		}
	}
	return nil
}

// verifyOriginals reads every reference the fold stated through the original store
// and names the first one the store does not answer with its body.
func verifyOriginals(ctx context.Context, tier *ledger.Tier, plan foldPlan, bodies map[string][]byte) error {
	for _, original := range plan.Originals {
		name, named := strings.CutPrefix(original.Key, refPrefix)
		if !named {
			continue
		}
		recalled := tier.Originals.Get(ctx, event.EventID(name))
		if recalled.Outcome != ledger.RecallFound {
			return fmt.Errorf("fold-cold-root: the folded root does not answer the original %q: %+v", name, recalled)
		}
		if !bytes.Equal(recalled.Body, bodies[name]) {
			return fmt.Errorf("fold-cold-root: the folded root answers other bytes for the original %q", name)
		}
	}
	return nil
}

// sortedKeys answers the keys of one map in key order.
func sortedKeys[V any](held map[string]V) []string {
	keys := make([]string, 0, len(held))
	for key := range held {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// printReport writes the summary of one fold: what the source states, what the scan
// or the restore did with it, the commit order it carries, and the keys the folded
// root does not state.
func printReport(sourceRoot, targetRoot string, restore bool, plan foldPlan, folded foldReport) {
	fmt.Printf("fold-cold-root: %s: journals %s; %d event(s), %d original key(s), %d key(s) to state\n",
		sourceRoot, strings.Join(plan.Tiers, ","), len(plan.Events), len(plan.Originals), plan.keyCount())
	printCommitRange(plan)
	printSuperseded(plan.Superseded)
	if !restore {
		fmt.Printf("fold-cold-root: dry run, nothing written; rerun with -restore to state those keys in %s\n", targetRoot)
		fmt.Println("fold-cold-root: the new root must be the one this build serves: this build reads one journal," +
			" so a root whose facts sit in cold/kv is folded before it serves")
	} else {
		fmt.Printf("fold-cold-root: %s: %d event(s) and %d original key(s) committed,"+
			" read back by identity in the commit order of the source\n", targetRoot, folded.Folded, folded.Written)
	}
	printLeft(plan.Left)
}

// printCommitRange names the oldest and the newest event of the plan, so an operator
// reads the span the fold states without the whole list.
func printCommitRange(plan foldPlan) {
	if len(plan.Events) == 0 {
		return
	}
	oldest, newest := plan.Events[0], plan.Events[len(plan.Events)-1]
	fmt.Printf("fold-cold-root: commit %d is the oldest and %d the newest: %q .. %q\n",
		oldest.Commit, newest.Commit, oldest.Key, newest.Key)
}

// printLeft names the keys the folded root does not state, with the value the source
// holds for each.
func printLeft(left []ledger.Pair) {
	if len(left) == 0 {
		return
	}
	fmt.Println("fold-cold-root: left behind: the folded root states its facts and none of these")
	for _, pair := range left {
		fmt.Printf("fold-cold-root:   %s=%s\n", pair.Key, string(pair.Value))
	}
}

// printSuperseded names the window metas the hot journal states otherwise than the
// cold copy, which the fold states as the hot journal has them.
func printSuperseded(superseded []string) {
	for _, key := range superseded {
		fmt.Printf("fold-cold-root: the hot journal restates %s, and the fold states that copy\n", key)
	}
}

// fail reports one fold failure to stderr and exits nonzero.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "fold-cold-root:", err)
	os.Exit(1)
}
