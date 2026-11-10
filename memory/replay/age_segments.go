// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// The segment-age axis of the mapped sequence: a projection is partitioned into
// task-turn segments that an agent output closes — the inputs its fact records
// as consumed beside the process detail, with the inputs it did not name rolling
// into the next segment — exactly the way the deterministic compress level reads
// them, and each segment is placed in one of three bands by its folding
// generation. A history summary covers the turns one earlier fold consumed and
// belongs to no single turn, so it stands as a complete segment of its own and
// the turns beside it keep their own material. The segments inside the recent
// window are the new band and stay whole; a segment older than that window that
// no fold has consumed is the middle band, whose process detail folds into one
// segment product; a segment that already carries a product of an earlier fold
// is the oldest band, which folds into the one history summary. Every fold
// promotes the material outside the window one band, so the promotion follows
// the folds and never the absolute distance a segment sits from the newest one.
//
// The fold is segment-grained while AssembleBudget is entry-grained: the
// assembly decides how one entry is presented under its budget and never
// removes an entry, and a fold decides which material a segment still carries
// and replaces the consumed entries with the product it minted. Both read the
// same projection and neither writes the fact chain.
package replay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
)

// Band names one band of the segment-age axis a segment belongs to.
type Band int

const (
	// BandNew is the newest band: the segments still inside the recent window,
	// which the projection keeps whole.
	BandNew Band = iota
	// BandMid is the middle band: the segments that left the recent window and
	// that no fold has consumed yet, whose process detail is what folds.
	BandMid
	// BandOld is the oldest band: the segments already carrying a product of an
	// earlier fold, which read as one history summary.
	BandOld
)

// String reports the band as the age axis names it.
func (b Band) String() string {
	switch b {
	case BandMid:
		return "mid"
	case BandOld:
		return "old"
	default:
		return "new"
	}
}

const (
	// DefaultAgeKeepRecent is the number of recent segments the age rules keep
	// whole when a plan names no window, the same default the deterministic
	// compress level reads under.
	DefaultAgeKeepRecent = 2
	// DefaultAgeVolumeThreshold is the rendered volume a projection may carry
	// before an age fold is due, in bytes.
	DefaultAgeVolumeThreshold = 4096

	// ProjectionTypeSegmentFold names the product entry the process detail of
	// one new-band segment folds into.
	ProjectionTypeSegmentFold = "segment_fold"
	// ProjectionTypeHistorySummary names the product entry the middle and
	// oldest bands fold into.
	ProjectionTypeHistorySummary = "history_summary"

	// ageSegmentKeyPrefix and ageHistoryKeyPrefix are the prefixes the two
	// product keys are minted under: the prefix is what a product fact states
	// about the kind of entry it restores.
	ageSegmentKeyPrefix = "seg"
	ageHistoryKeyPrefix = "hist"

	// AgeProducerSegmentAge names the mechanism that judges a projection due for
	// an age fold.
	AgeProducerSegmentAge = "memory.replay.segment-age"
	// AgeReasonSegmentFold states why the middle band is folded.
	AgeReasonSegmentFold = "the process detail of the middle band is over its volume threshold"
	// AgeReasonHistorySummary states why the oldest band is folded away.
	AgeReasonHistorySummary = "the oldest band is over its volume threshold"

	// DefaultAgeSummaryTextLimit bounds the text one history summary carries, in
	// bytes. The summary is a derived view of material that stays on the chain,
	// so it carries the newest reads while they fit and states the older ones by
	// count: the text is bounded by this limit whatever the length of the history
	// it compresses, and folding one summary a second time answers the same bound
	// instead of copying the text it stands for.
	DefaultAgeSummaryTextLimit = 2048
	// ageSummaryDropReserve is the room the covered-by-key marker takes inside
	// DefaultAgeSummaryTextLimit: the count it states is the only part of the
	// dropped material the summary keeps.
	ageSummaryDropReserve = 64
	// ageSummaryLineJoin is the separator two folded lines are joined under, in
	// bytes.
	ageSummaryLineJoin = len("; ")
)

// ageSummaryDropMarker renders the line a history summary states the material it
// stopped spelling: the entries are covered by key, and the keys the product
// fact carries are what reads their original text back off the chain.
func ageSummaryDropMarker(dropped int) string {
	return fmt.Sprintf("+%d earlier entries covered by key", dropped)
}

// LevelOfAge reports the compress level the age of one closed segment reads
// under the deterministic rule the age axis is shared with: the recent window
// whole, the next window dropping tool commands, the window after that keeping
// the skeleton only, and everything older archived. The ladder is recorded on
// the segment as the diagnostic read of its distance from the newest segment;
// which band a segment folds in is decided by the fold generations and never by
// this distance.
func LevelOfAge(age, keepRecent int) int {
	if keepRecent < 1 {
		keepRecent = DefaultAgeKeepRecent
	}
	switch {
	case age < keepRecent:
		return 0
	case age < keepRecent*2:
		return 1
	case age < keepRecent*3:
		return 2
	default:
		return 3
	}
}

// BandOfLevel reports the band one compress level belongs to under the age
// ladder: L0 the newest band, L1 and L2 the middle band, L3 the oldest band. The
// answer states where the ladder places material of a given distance; a fold
// bands its segments by their fold generation instead, so this reading is the
// ladder's own and never the fold's decision.
func BandOfLevel(level int) Band {
	switch {
	case level <= 0:
		return BandNew
	case level <= 2:
		return BandMid
	default:
		return BandOld
	}
}

// BandOfAge reports the band a closed segment of the given distance from the
// newest one belongs to under the age ladder.
func BandOfAge(age, keepRecent int) Band {
	return BandOfLevel(LevelOfAge(age, keepRecent))
}

// Segment is one task-turn segment of a projection: the entries it spans, the
// age and ladder level it reads, whether an agent output closed it, and the band
// its fold generation places it in. A segment no agent output closed is still in
// progress, reads L0 and is never folded.
type Segment struct {
	// Index is the position of the segment in the projection, oldest first.
	Index int
	// Age is the distance of the segment from the newest one, zero for it.
	Age int
	// Level is the ladder level the age reads, zero for a segment in progress.
	Level int
	// Band is the band the fold generation places the segment in.
	Band Band
	// Complete reports whether an agent output closed the segment.
	Complete bool
	// Entries are the projection entries the segment spans, in projection order.
	Entries []ProjectionEntry
}

// Legacy reports whether the segment was closed by an agent output whose fact
// recorded no readable input relation, so the entries before it were cut at the
// positional boundary a chain predating the relation is interpreted under. The
// answer is read from the closing output the segment spans, so it never drifts
// from the entries: a segment still in progress answers false because no output
// closed it, and a segment closed by an output that named its inputs answers
// false whatever those inputs were. The mark is diagnostic only — the segment
// rule, the bands and the folds read the same segments either way.
func (s Segment) Legacy() bool {
	if !s.Complete || len(s.Entries) == 0 {
		return false
	}
	closing := s.Entries[len(s.Entries)-1]
	if closing.Type != event.TypeAgentOutput {
		return false
	}
	_, held := parseInputEvents(closing.inputEvents)
	return !held
}

// SegmentsOf partitions the entries of one projection into its task-turn
// segments, oldest first: an agent output closes the segment of the inputs its
// fact records as consumed, an input it leaves unnamed rolls into the segment
// the next output closes, and the process entries between stay with the closing
// output. An output beside no readable relation closes every entry before it at
// the positional boundary. A history summary stands as one complete segment of
// its own. A tail no agent output closed stays its own segment in progress.
//
// Each segment is banded by its fold generation: the newest keepRecent segments
// are the new band, a segment already carrying a product of an earlier fold is
// the oldest band, and the rest left the window without being folded and are the
// middle band. A segment still in progress is the newest one and is never folded.
func SegmentsOf(entries []ProjectionEntry, keepRecent int) []Segment {
	if keepRecent < 1 {
		keepRecent = DefaultAgeKeepRecent
	}
	runs := splitSegments(entries)
	out := make([]Segment, 0, len(runs))
	for i, run := range runs {
		segment := Segment{Index: i, Age: len(runs) - 1 - i, Complete: run.complete, Entries: run.entries}
		if run.complete {
			segment.Level = LevelOfAge(segment.Age, keepRecent)
		}
		segment.Band = bandOfSegment(segment, len(runs), keepRecent)
		out = append(out, segment)
	}
	return out
}

// bandOfSegment reports the band one segment folds in under the fold generations:
// a segment inside the recent window and a segment still in progress are the new
// band, a segment already carrying a fold product has been through one fold and
// is the oldest band, and the rest left the window without being folded and are
// the middle band. The answer reads where the folds have been, so the material
// moves one band per fold instead of by counting segments.
func bandOfSegment(segment Segment, total, keepRecent int) Band {
	if !segment.Complete || segment.Index >= total-keepRecent {
		return BandNew
	}
	if carriesFoldProduct(segment.Entries) {
		return BandOld
	}
	return BandMid
}

// carriesFoldProduct reports whether one segment already carries a product of an
// earlier age fold, which is the mark that the segment's own fold is spent and
// the material beside it is the oldest band. A history summary segment carries
// the summary it stands for and is the oldest band from the round it is placed.
func carriesFoldProduct(entries []ProjectionEntry) bool {
	for _, entry := range entries {
		if entry.Type == ProjectionTypeSegmentFold || entry.Type == ProjectionTypeHistorySummary {
			return true
		}
	}
	return false
}

// segmentRun is one partition of a projection before its age is read: the
// entries it spans beside whether an agent output closed it.
type segmentRun struct {
	entries  []ProjectionEntry
	complete bool
}

// WithInputEvents returns the entry beside the input relation its fact recorded:
// the input_events metadata entry, a JSON array of the input fact identities the
// output consumed. A missing, empty, identity-less or unparseable value is no
// relation — the same rule the chain-authority answer rebuild reads one under —
// so the segment rule closes the output positionally instead of guessing inputs
// it never named.
func WithInputEvents(entry ProjectionEntry, raw string) ProjectionEntry {
	entry.inputEvents = raw
	return entry
}

// parseInputEvents reads one recorded relation: a JSON array of input fact
// identities with empty and repeated identities dropped. A relation naming no
// identity is not a relation at all, so it answers unheld rather than every
// input.
func parseInputEvents(raw string) ([]event.EventID, bool) {
	if raw == "" {
		return nil, false
	}
	var decoded []event.EventID
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, false
	}
	inputs := make([]event.EventID, 0, len(decoded))
	seen := make(map[event.EventID]bool, len(decoded))
	for _, key := range decoded {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		inputs = append(inputs, key)
	}
	if len(inputs) == 0 {
		return nil, false
	}
	return inputs, true
}

// splitSegments cuts the projection at every agent output. An output that
// records the input relation its fact carried closes only the inputs it names
// beside the process entries and rolls the inputs it leaves unnamed into the
// run the next segment opens with; an output beside no readable relation keeps
// the positional boundary and closes the whole run, so a chain without the
// relation is never re-cut. A tail no output closed stays one run in progress.
// A history summary covers the turns one earlier fold consumed and belongs to no
// single turn, so it closes as one complete segment of its own and the material
// either side of it keeps its own segment; a segment product stays inside the
// turn that minted it, because replacing that turn's process detail is what the
// product records.
//
// Segments group by arrival order by default; an entry that carries a
// consumption relation folds along that relation instead. The relation is
// wired through assembly mappedEntryOf, so live outputs fold by what they
// consumed while facts without it keep the positional bracket.
func splitSegments(entries []ProjectionEntry) []segmentRun {
	driving := drivingInputsOf(entries)
	var runs []segmentRun
	var current []ProjectionEntry
	for _, entry := range entries {
		if entry.Type == ProjectionTypeHistorySummary {
			if len(current) > 0 {
				runs = append(runs, segmentRun{entries: current})
				current = nil
			}
			runs = append(runs, segmentRun{entries: []ProjectionEntry{entry}, complete: true})
			continue
		}
		current = append(current, entry)
		if entry.Type != event.TypeAgentOutput {
			continue
		}
		consumed, held := parseInputEvents(entry.inputEvents)
		if !held {
			runs = append(runs, segmentRun{entries: current, complete: true})
			current = nil
			continue
		}
		closed, carried := closeByConsumption(current, consumed, driving)
		runs = append(runs, segmentRun{entries: closed, complete: true})
		current = carried
	}
	if len(current) > 0 {
		runs = append(runs, segmentRun{entries: current})
	}
	return runs
}

// drivingInputsOf reports the identities any readable relation of the sequence
// names: a feedback notice is read as an input only where a relation proves the
// notice drove a turn, so a notice that is only material of a turn never rolls
// out of the segment it landed in. An external input is an input by its own
// type, because an input no output carried yet has no relation to be named
// under and still belongs to the segment in progress.
func drivingInputsOf(entries []ProjectionEntry) map[event.EventID]bool {
	driving := map[event.EventID]bool{}
	for _, entry := range entries {
		if entry.Type != event.TypeAgentOutput {
			continue
		}
		inputs, held := parseInputEvents(entry.inputEvents)
		if !held {
			continue
		}
		for _, key := range inputs {
			driving[key] = true
		}
	}
	return driving
}

// closeByConsumption splits the accumulated run at the output that closes it:
// the inputs the output names stay in the closing segment beside every process
// entry, and the inputs it leaves unnamed roll into the run the next segment
// opens with. The projection order is kept on both sides.
func closeByConsumption(entries []ProjectionEntry, consumed []event.EventID, driving map[event.EventID]bool) (closed, carried []ProjectionEntry) {
	named := make(map[event.EventID]bool, len(consumed))
	for _, key := range consumed {
		named[key] = true
	}
	for _, entry := range entries {
		if isSegmentInput(entry, driving) && !named[entry.Key] {
			carried = append(carried, entry)
			continue
		}
		closed = append(closed, entry)
	}
	return closed, carried
}

// isSegmentInput reports whether one entry is an input a segment can close: an
// external input always is one, while a feedback notice is one only where a
// relation names it as consumed — an unconsumed notice is material of the turn
// beside it, not an input waiting for a segment of its own.
func isSegmentInput(entry ProjectionEntry, driving map[event.EventID]bool) bool {
	switch entry.Type {
	case event.TypeExternalInput:
		return true
	case event.TypeFeedback:
		return driving[entry.Key]
	default:
		return false
	}
}

// AgeFoldPlan shapes one age-band fold: the window the age rule keeps whole
// and the rendered volume a projection may carry before a fold is due. A plan
// naming no window or no threshold reads the defaults.
type AgeFoldPlan struct {
	// KeepRecent is the number of recent segments kept whole; zero reads
	// DefaultAgeKeepRecent.
	KeepRecent int
	// VolumeThreshold is the rendered volume a projection may carry, in bytes;
	// zero reads DefaultAgeVolumeThreshold.
	VolumeThreshold int
}

// AgeFoldReport is the structured result of one age-band fold: the volume the
// projection carried beside the threshold it was judged under, how many entries
// each fold consumed, and the products and compress requests it minted.
type AgeFoldReport struct {
	// Volume is the rendered volume the projection carried, in bytes.
	Volume int
	// Threshold is the volume the fold was judged under, in bytes.
	Threshold int
	// Triggered reports whether the fold minted a product to write.
	Triggered bool
	// NewFolded is how many process-detail entries the middle-band fold consumed:
	// the material that left the recent window this round and folded its process
	// detail into a segment product. The field keeps the name the report was
	// first placed under; the band it counts is the middle band of the
	// generational rule.
	NewFolded int
	// MidFolded is how many entries the oldest band contributed to the history
	// summary: the material an earlier fold already consumed, which this fold
	// compresses into the one bounded summary.
	MidFolded int
	// Products lists the product entries the fold minted, in projection order:
	// one segment fold per folded middle-band segment, then the history summary.
	Products []ProjectionEntry
	// Requests lists the compress requests the fold filed, one per product, in
	// the order the products were minted.
	Requests []event.CompressRequest
}

// FoldByAge folds the projection along its segment-age bands once its rendered
// volume passes the plan's threshold: the process detail of every middle-band
// segment is replaced by one segment-fold product, the oldest band is replaced by
// one history summary placed first, and the new band is left exactly as it
// stands. The bands follow the folds — a band is defined by the material an
// earlier fold consumed — so every fold promotes the material outside the recent
// window one band, and a projection whose volume stays inside the threshold, or
// whose volume passes it without a band to work on, is left as it stands.
//
// A middle-band segment carrying no process detail is summarized in the same
// round instead of waiting for the next one: nothing of it folds, and leaving it
// in the middle band would grow that band with every turn.
//
// - The entries a product replaces leave the projection with their per-key material (the kept original, the dedup key and the consumed mark), so a fold of the same sequence twice finds nothing left to drop.
//
// - The fold is deterministic over the sequence it reads and files the judgment as a compress request rather than rewriting any fact: the products enter the projection under content-addressed synthetic keys, so folding the same sequence twice mints the same keys and appends no second product.
//
// - A history summary whose key already answers the projection mints no second product: the oldest band leaves with the summary that covers it.
func (p *ReplayProjection) FoldByAge(ctx context.Context, plan AgeFoldPlan) (AgeFoldReport, error) {
	if err := ctx.Err(); err != nil {
		return AgeFoldReport{}, err
	}
	keepRecent := plan.KeepRecent
	if keepRecent < 1 {
		keepRecent = DefaultAgeKeepRecent
	}
	threshold := plan.VolumeThreshold
	if threshold <= 0 {
		threshold = DefaultAgeVolumeThreshold
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	report := AgeFoldReport{Volume: p.volume, Threshold: threshold}
	if len(p.entries) == 0 || report.Volume <= threshold {
		return report, nil
	}
	kept := make([]ProjectionEntry, 0, len(p.entries))
	held := heldKeysOf(p.entries)
	var oldest []ProjectionEntry
	for _, segment := range SegmentsOf(p.entries, keepRecent) {
		switch segment.Band {
		case BandOld:
			oldest = append(oldest, segment.Entries...)
		case BandMid:
			folded, product := foldProcessDetail(segment.Entries, segment.Complete)
			report.NewFolded += folded
			if product == nil {
				oldest = append(oldest, segment.Entries...)
				continue
			}
			kept = append(kept, segmentWithProduct(segment.Entries, *product)...)
			report.Products = append(report.Products, *product)
			report.Requests = append(report.Requests, ageRequest(AgeReasonSegmentFold, *product))
		default:
			kept = append(kept, segment.Entries...)
		}
	}
	report.MidFolded = len(oldest)
	if len(oldest) > 0 {
		summary := historySummaryOf(oldest)
		kept = append([]ProjectionEntry{summary}, kept...)
		if _, stands := held[summary.Key]; !stands {
			report.Products = append(report.Products, summary)
			report.Requests = append(report.Requests, ageRequest(AgeReasonHistorySummary, summary))
		}
	}
	if len(report.Products) == 0 {
		return report, nil
	}
	report.Triggered = true
	p.entries = kept
	p.recomputeVolumeLocked()
	p.forgetDroppedLocked()
	return report, nil
}

// foldProcessDetail reports how many entries of one closed segment are process
// detail beside the product they fold into: the thinking plan and the tool
// commands between the input and the output of the turn collapse into one
// product, while a segment in progress, a segment carrying no process detail
// and a segment already folded mint no second product. Every source line opens
// with its own provenance anchor and the product content opens with the first
// one, so a later fold reads the source lines' moments and generations from the
// head of the product it consumes.
func foldProcessDetail(entries []ProjectionEntry, complete bool) (int, *ProjectionEntry) {
	if !complete {
		return 0, nil
	}
	var process []ProjectionEntry
	var lines []string
	for _, entry := range entries {
		if !isProcessDetail(entry) {
			continue
		}
		process = append(process, entry)
		lines = append(lines, foldLineOf(entry))
	}
	if len(process) == 0 {
		return 0, nil
	}
	product := ProjectionEntry{
		Key:     ageKey(ageSegmentKeyPrefix, process),
		Type:    ProjectionTypeSegmentFold,
		Summary: "segment fold",
		Content: strings.Join(lines, "; "),
		Time:    process[0].Time,
	}
	return len(process), &product
}

// isProcessDetail reports whether one entry is the process detail an age fold
// consumes: the reasoning and the tool commands of a turn. A synthetic key
// names a product of an earlier fold and is never folded a second time.
func isProcessDetail(entry ProjectionEntry) bool {
	if SyntheticKey(entry.Key) {
		return false
	}
	return entry.Type == event.TypeThinkingPlan || entry.Type == event.TypeActionCommand
}

// segmentWithProduct replaces the process detail of one segment with the
// product it folded into, keeping everything else of the segment in order: the
// product stands where the first folded entry stood, so the input that opened
// the segment stays in front of it and the output that closed it behind.
func segmentWithProduct(entries []ProjectionEntry, product ProjectionEntry) []ProjectionEntry {
	out := make([]ProjectionEntry, 0, len(entries))
	placed := false
	for _, entry := range entries {
		if !isProcessDetail(entry) {
			out = append(out, entry)
			continue
		}
		if !placed {
			out = append(out, product)
			placed = true
		}
	}
	return out
}

// historySummaryOf folds the middle and oldest bands into the one history
// summary the age axis reads them as. The content spells the one-line reads of
// the turns it folded — each opening with its own provenance anchor, a folded
// product among them contributing the bounded reference it is — and carries no
// type label of its own: what kind of product an entry is stays its Type, and a
// presentation that needs a label renders it from there instead of the fold
// writing it into the material.
//
// The text is bounded by DefaultAgeSummaryTextLimit whatever it folds: the newest
// reads are kept while they fit and the older material is stated by the count of
// entries the summary covers. A standing history summary is its own summary, so
// folding one a second time answers the entry it already is — the fold is
// idempotent and a summary never re-carries the text it stands for.
func historySummaryOf(entries []ProjectionEntry) ProjectionEntry {
	if len(entries) == 1 && entries[0].Type == ProjectionTypeHistorySummary {
		return entries[0]
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, historySummaryLine(entry))
	}
	return ProjectionEntry{
		Key:     ageKey(ageHistoryKeyPrefix, entries),
		Type:    ProjectionTypeHistorySummary,
		Summary: "history summary",
		Content: ageSummaryTextOf(lines),
		Time:    entries[0].Time,
	}
}

// historySummaryLine renders the one-line read one folded entry contributes to
// a history summary, opening with the provenance anchor of the entry itself. A
// product of an earlier fold — a history summary or a segment fold — is judged
// by its Type and never by what its content happens to spell, and contributes a
// bounded reference to the product under its key: the text that product
// recorded stays on the chain, so a second fold reads where the material is
// instead of copying it. The original turns therefore survive every later fold
// with their moment and recorder generation still readable at the head of each
// line, while a folded product answers one line however long its own text is.
func historySummaryLine(entry ProjectionEntry) string {
	if entry.Type == ProjectionTypeHistorySummary || entry.Type == ProjectionTypeSegmentFold {
		return provenancePrefixOf(entry) + entry.Summary + " (" + string(entry.Key) + ")"
	}
	return foldLineOf(entry)
}

// ageSummaryTextOf joins the folded lines under the summary text bound: the
// newest lines keep their one-line read while they fit under it, and the older
// ones are stated as the count of entries the summary covers by key. The answer
// is deterministic over the lines it reads and never runs past the bound, so a
// summary compressed twice answers the same text a third time would.
func ageSummaryTextOf(lines []string) string {
	joined := strings.Join(lines, "; ")
	if len(joined) <= DefaultAgeSummaryTextLimit {
		return joined
	}
	room := DefaultAgeSummaryTextLimit - ageSummaryDropReserve
	if room < 0 {
		room = 0
	}
	kept := make([]string, 0, len(lines))
	used := 0
	for at := len(lines) - 1; at >= 0; at-- {
		size := len(lines[at])
		if used > 0 {
			size += ageSummaryLineJoin
		}
		if used+size > room {
			break
		}
		kept = append(kept, lines[at])
		used += size
	}
	if len(kept) == 0 {
		kept = append(kept, cutBytes(lines[len(lines)-1], room))
	}
	dropped := len(lines) - len(kept)
	slices.Reverse(kept)
	out := make([]string, 0, len(kept)+1)
	if dropped > 0 {
		out = append(out, ageSummaryDropMarker(dropped))
	}
	out = append(out, kept...)
	return cutBytes(strings.Join(out, "; "), DefaultAgeSummaryTextLimit)
}

// foldLineOf renders the one-line read one folded entry contributes to a fold
// product: the provenance anchor of the entry followed by its type and summary.
// The line is one shape shared by the history summary and the segment fold, so
// a later fold reads a product's source lines exactly as the product wrote them.
func foldLineOf(entry ProjectionEntry) string {
	return provenancePrefixOf(entry) + entry.Type + ": " + entry.Summary
}

// provenancePrefixOf renders the anchor a folded line opens with:
// [MM-DD HH:MM|gen:<generation>]. The moment is the entry's own recorded time,
// formatted in the timezone the fact stated and never converted to the fold's
// clock. The generation is the full runtime instance identity of the fact and is
// never truncated — recorder identities share prefixes — while an entry whose
// fact named no generation reads the explicit gen:? placeholder instead of a
// fabricated identity.
func provenancePrefixOf(entry ProjectionEntry) string {
	generation := string(entry.Generation())
	if generation == "" {
		generation = "?"
	}
	return "[" + entry.Time.Format("01-02 15:04") + "|gen:" + generation + "] "
}

// heldKeysOf reports the identities one entry sequence carries, so a fold reads
// whether the product it minted already stands in the projection instead of
// placing it a second time.
func heldKeysOf(entries []ProjectionEntry) map[event.EventID]struct{} {
	held := make(map[event.EventID]struct{}, len(entries))
	for _, entry := range entries {
		if entry.Key != "" {
			held[entry.Key] = struct{}{}
		}
	}
	return held
}

// ageKey reports the content-addressed synthetic key one product is filed
// under: the digest of the material it was folded from, so the same material
// always folds under the same key and a second fold appends no second product.
func ageKey(prefix string, entries []ProjectionEntry) event.EventID {
	digest := sha256.New()
	for _, entry := range entries {
		digest.Write([]byte(entry.Key))
		digest.Write([]byte{0})
	}
	return event.EventID("-" + prefix + "-" + hex.EncodeToString(digest.Sum(nil)[:8]))
}

// ageRequest files the judgment one product was minted under as a compress
// request: the material judged is the product's own content-addressed
// identity, so a re-judgment of the same material answers the same request.
func ageRequest(reason string, product ProjectionEntry) event.CompressRequest {
	subject := strings.TrimPrefix(string(product.Key), "-")
	return event.CompressRequestFor(AgeProducerSegmentAge, subject, reason)
}
