package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/durable"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// MetaKeySubtype is the metadata key one governance event is filed under, and
// the subtype words below are the vocabulary the governance events speak. The
// words are kept exactly as the governance semantics fix them: a refusal is a
// denial, a privileged execution is an audit fact, and a parked critical call
// is an approval fact. Goal operations file under their own two words, the
// event vocabulary entry the goal contract fixes (see SubtypeGoalDeclared).
const (
	// MetaKeySubtype names the governance subtype entry of one event.
	MetaKeySubtype = "subtype"
	// SubtypeDenial is the subtype of a refused call's governance event.
	SubtypeDenial = "denial"
	// SubtypeApproval is the subtype of a held call's governance event.
	SubtypeApproval = "approval"
	// SubtypeAudit is the subtype of a recorded execution's governance event.
	SubtypeAudit = "audit"
)

// denialEventPrefix and goalEventPrefix are ordered identities over one durable
// log: one stream per ledger family, numbered from where the stored stream ends,
// so the lexicographic order of the keys is the order the operations happened in
// and a replay reconstructs exactly that order.
const (
	denialEventPrefix = "tagent/governance/v1/denial:"
	goalEventPrefix   = "tagent/governance/v1/goal:"
)

// DenialRecord is one governance record: the fact of a refused call or of a
// privileged execution, kept for the model to correct itself on and for the
// audit to read back. GoalID carries the goal context the record permeates:
// the goal the governed call was hanging on when the ruling was rendered.
type DenialRecord struct {
	// Subtype is the governance subtype the record is filed under.
	Subtype string `json:"subtype"`
	// ToolName is the tool the ruled call wanted to run.
	ToolName string `json:"tool"`
	// Level is the risk grade the ruling was rendered at.
	Level plugin.RiskLevel `json:"level"`
	// RuleID names the rule the ruling came from, when one matched.
	RuleID string `json:"rule_id"`
	// Reason is the readable reason behind the record.
	Reason string `json:"reason"`
	// ArgsDigest is the digest of the exact argument bytes the call carried.
	ArgsDigest string `json:"args_digest,omitempty"`
	// GoalID is the goal context the record permeates. Filling it is the
	// denial's goal permeation: a refusal records beside the goal the call was
	// hanging on, so the audit reads which autonomous scope it happened in.
	GoalID string `json:"goal_id,omitempty"`
	// AgentName names the agent the record came from, when the wiring knows one.
	AgentName string `json:"agent,omitempty"`
	// Timestamp is when the record was made, in Unix milliseconds.
	Timestamp int64 `json:"ts"`
}

// DenialLedger is the governance ledger: an in-memory index beside the durable
// governance events every record also writes. A refusal leaves both halves —
// the ledger record and the governance event — so the model reads the refusal
// and the host and the observation face see it too.
//
// - concurrent safe, with a late-bound and nil safe durable face: without a store it degrades to memory state, and binding one folds the events past the stated cut so a restart keeps the records it made
// - the write path is serialized and numbers behind a barrier it confirmed: a refused write hands its number back, and a barrier that did not confirm stops the stream numbering on
// - a hole is not the chain's end: the first writer continuing a seated cut resolves the tail from the highest committed sequence and names every missing sequence (see resolveContinuationTail), so a restart never re-keys over an event already stored
// - a write meets the store under its key before it lands: the same bytes replay as the commitment already held, and other bytes under that key are refused by name (see errGovernanceEventConflict), so an identity is never overwritten
// - the records a query reads are the derived index: a cut states the tail alone and the history behind it is read from the stream on demand
//
// The redline list (2 rules):
// MUST key every governance event under the stream key its family numbers.
// MUST NOT rewrite or drop a governance event already stored.
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
type DenialLedger struct {
	mu      sync.RWMutex
	records []DenialRecord
	log     durable.FactLog
	evtSeq  int64

	writeMu       sync.Mutex
	stopped       error
	seatedCut     int64
	historyFolded bool
	tailResolved  bool
}

// NewDenialLedger builds one ledger in memory state. Bind hands it the durable
// face whenever the wiring has one.
func NewDenialLedger() *DenialLedger {
	return &DenialLedger{}
}

// Bind hands the ledger its durable face and folds the stored governance events
// that landed past the state cut this ledger holds back into the index. It is
// nil safe, and it runs single threaded at construction — the same discipline
// the goal registry binds under.
//
// The error of a fold belongs to the caller that can stop the boot: a stream this
// ledger could not read leaves the index behind the chain it serves, and the
// caller that holds the cut decides whether that is a boot to stop. The
// framework state seam is that caller (see StateSeat.Bind).
func (l *DenialLedger) Bind(ctx context.Context, log durable.FactLog) error {
	if l == nil || log == nil {
		return nil
	}
	l.mu.Lock()
	l.log = log
	cut := l.evtSeq
	l.mu.Unlock()
	return l.foldTail(ctx, log, cut)
}

// Record writes the governance double of one record: the ledger entry and the
// governance event beside it. A record that carries no timestamp is stamped
// here. The durable face decides the entry: a landed event indexes the record,
// a replay of the event already held indexes nothing twice, and a key naming
// other content is refused by name and indexes nothing. A store-side failure
// never blocks the operation — the ledger entry stands and the failure is named
// at ERROR beside it, a gap in the stream the next writer must not close by
// re-keying over it.
func (l *DenialLedger) Record(rec DenialRecord) {
	if l == nil {
		return
	}
	if rec.Timestamp == 0 {
		rec.Timestamp = time.Now().UnixMilli()
	}
	l.mu.Lock()
	log := l.log
	l.mu.Unlock()
	if log == nil {
		l.appendIndex(rec)
		return
	}
	l.writeGovernanceEvent(log, rec)
}

// Query returns the newest records, at most limit of them. A limit of zero or
// less returns all of them. The records are copies, so a reader never observes
// a concurrent write. A ledger seated by a cut reads the history its cut covers
// from the stream on the first question, so the cut carries the live state and
// the history is paid for by the reader that asks for it.
func (l *DenialLedger) Query(limit int) []DenialRecord {
	if l == nil {
		return nil
	}
	l.foldHistory(context.Background())
	l.mu.RLock()
	defer l.mu.RUnlock()
	n := len(l.records)
	start := 0
	if limit > 0 && n > limit {
		start = n - limit
	}
	out := make([]DenialRecord, 0, n-start)
	for i := n - 1; i >= start; i-- {
		out = append(out, l.records[i])
	}
	return out
}

// Count reports how many records the ledger holds: the index it folded, which a
// seated cut derives from the stream it numbers on the first question. It never
// answers the tail sequence, because a cut numbers events a window may leave out
// of the index, and a count that reported those would over-report the records.
func (l *DenialLedger) Count() int {
	if l == nil {
		return 0
	}
	l.foldHistory(context.Background())
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.records)
}

// seat states the ledger the cut one framework state slice restored, so a
// restarted ledger continues its numbering where the stated stream ended. The cut
// states no record: the index a reader asks for is derived from the stream the cut
// numbers, so a restore seats the history of the stream when a reader first asks
// for it. A cut states a stream the store confirmed, so a restore clears the
// barrier that stopped the stream the process before it wrote on.
func (l *DenialLedger) seat(cut int64) {
	if l == nil {
		return
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = nil
	l.seatedCut = cut
	l.historyFolded = false
	l.tailResolved = false
	l.evtSeq = cut
	l.stopped = nil
}

// stated reports the cut the framework state slice states for this ledger: the
// newest sequence the stream has spent. A spent sequence is one the barrier
// confirmed or one that stopped the stream, so a restart continuing past it never
// re-keys over an event already stored. The records are not stated: they are the
// history the cut numbers, read back from the stream when a reader asks for them
// (see foldHistory), so one cut costs the live state instead of the history.
func (l *DenialLedger) stated() int64 {
	if l == nil {
		return 0
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.evtSeq
}

// foldHistory seats the records of the stream the seated cut numbers, so a
// restarted ledger answers the history its cut covers without the cut carrying it.
// The read is the whole stream behind the seated cut — the one read a history
// question costs, and the read a boot never pays for. It runs once per seated cut
// and the write path serializes it, while the seated cut bounds the walk, so a
// record this process made past that cut is never folded twice.
//
// The fold names every way its answer falls short of the store: a body it cannot
// parse, a sequence the window numbers but the store does not hold, and a stored
// event past the tail the restore reached. A history read has no error to hand a
// caller, so the records around a gap are kept and the gap is stated at ERROR
// (see governanceHistoryUnreadable) — a shorter history is a named boundary,
// never a silent one.
func (l *DenialLedger) foldHistory(ctx context.Context) {
	if l == nil {
		return
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	l.mu.Lock()
	log, folded, cut, tail := l.log, l.historyFolded, l.seatedCut, l.evtSeq
	l.mu.Unlock()
	if log == nil || folded || cut <= 0 {
		return
	}
	history := make([]DenialRecord, 0, cut)
	stored, err := log.Scan(ctx, denialEventPrefix)
	if err != nil {
		governanceHistoryUnreadable(denialEventPrefix, err)
		return
	}
	keys := make([]string, 0, len(stored))
	beyond := make([]string, 0)
	for key := range stored {
		seq := eventSeqOf(denialEventPrefix, key)
		switch {
		case seq > 0 && seq <= cut:
			keys = append(keys, key)
		case seq > tail:
			beyond = append(beyond, key)
		}
	}
	sort.Strings(keys)
	expected := int64(1)
	for _, key := range keys {
		seq := eventSeqOf(denialEventPrefix, key)
		for ; expected < seq; expected++ {
			governanceHistoryUnreadable(fmt.Sprintf("%s%019d", denialEventPrefix, expected), errWindowSequenceMissing)
		}
		expected = seq + 1
		var evt event.Event
		if err := json.Unmarshal(stored[key], &evt); err != nil {
			governanceHistoryUnreadable(key, err)
			continue
		}
		history = append(history, recordOfEvent(evt))
	}
	for ; expected <= cut; expected++ {
		governanceHistoryUnreadable(fmt.Sprintf("%s%019d", denialEventPrefix, expected), errWindowSequenceMissing)
	}
	sort.Strings(beyond)
	for _, key := range beyond {
		governanceHistoryUnreadable(key, errEventBeyondTheWindow)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.historyFolded {
		return
	}
	l.records = append(history, l.records...)
	l.historyFolded = true
}

// foldTail folds the stored governance events that landed past one cut into the
// index, in the order the operations happened, and continues the stream
// numbering from the newest event it read. A body the store cannot parse fails
// the fold by name instead of being skipped: the index and the tail a stream
// states must name the same events, so an event whose key numbers it without
// being folded is a corrupted stream the boot reads as one.
func (l *DenialLedger) foldTail(ctx context.Context, log durable.FactLog, cut int64) error {
	events, newest, err := governanceEventsPast(ctx, log, denialEventPrefix, cut)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, evt := range events {
		if evt.Type != event.TypeGovernance {
			continue
		}
		l.records = append(l.records, recordOfEvent(evt))
	}
	if newest > l.evtSeq {
		l.evtSeq = newest
	}
	return nil
}

// writeGovernanceEvent writes one record as the governance event its double
// requires, for the host and the observation face to see. The first write past a
// seated cut resolves the continuation tail from the highest committed sequence
// (see resolveContinuationTail). The write then meets the store under its key:
// the same bytes replay as the commitment already held and index nothing twice,
// other bytes under that key are refused by name, and an empty key lands behind
// the barrier. A read the store refuses leaves the judgment unable to answer, so
// the write lands as the live leg always did. A refused write hands its number
// back; a barrier that did not confirm stops the stream from numbering on. A
// store-side failure leaves the ledger entry standing, so a rendered ruling is
// never turned into something else.
func (l *DenialLedger) writeGovernanceEvent(log durable.FactLog, rec DenialRecord) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	l.resolveContinuationTail(log)
	l.mu.Lock()
	stopped := l.stopped
	seq := l.evtSeq + 1
	l.mu.Unlock()
	key := fmt.Sprintf("%s%019d", denialEventPrefix, seq)
	if stopped != nil {
		governanceWriteFailure(key, rec.Subtype, rec.ToolName, stopped)
		l.appendIndex(rec)
		return
	}
	evt := governanceEventOf(key, rec)
	raw, err := json.Marshal(evt)
	if err != nil {
		governanceWriteFailure(key, rec.Subtype, rec.ToolName, fmt.Errorf("governance: render the event at %s: %w", key, err))
		l.appendIndex(rec)
		return
	}
	held, scanErr := log.Scan(context.Background(), key)
	if scanErr == nil {
		if prior, ok := held[key]; ok {
			if bytes.Equal(prior, raw) {
				l.advanceSeq(seq)
				return
			}
			conflict := fmt.Errorf("%w: %s", errGovernanceEventConflict, key)
			governanceWriteFailure(key, rec.Subtype, rec.ToolName, conflict)
			l.stopStream(seq, conflict)
			return
		}
	}
	staged, err := putGovernanceRaw(context.Background(), log, key, raw)
	if err == nil {
		l.advanceSeq(seq)
		l.appendIndex(rec)
		return
	}
	if staged {
		l.stopStream(seq, fmt.Errorf("the write barrier at %s did not confirm: %w", key, err))
	}
	governanceWriteFailure(key, rec.Subtype, rec.ToolName, err)
	l.appendIndex(rec)
}

// resolveContinuationTail seats the numbering tail at the highest committed
// sequence of the stream before the first write past a seated cut, and names
// every sequence the window numbers but the store does not hold. A hole is not
// the chain's end: the truth is the highest committed sequence, so a restart
// continues past it instead of re-keying over the events stored behind a hole.
// The read is the one scan the first continuing writer pays for, the same form
// the history reader pays on its first question (see foldHistory); a boot never
// pays it. A read the store refuses is named and leaves the tail to retry.
func (l *DenialLedger) resolveContinuationTail(log durable.FactLog) {
	l.mu.Lock()
	cut, resolved := l.seatedCut, l.tailResolved
	l.mu.Unlock()
	if cut <= 0 || resolved {
		return
	}
	stored, err := log.Scan(context.Background(), denialEventPrefix)
	if err != nil {
		governanceHistoryUnreadable(denialEventPrefix, err)
		return
	}
	held := make(map[int64]bool, len(stored))
	highest := cut
	for key := range stored {
		seq := eventSeqOf(denialEventPrefix, key)
		if seq <= 0 {
			continue
		}
		held[seq] = true
		if seq > highest {
			highest = seq
		}
	}
	for seq := cut + 1; seq <= highest; seq++ {
		if !held[seq] {
			governanceHistoryUnreadable(fmt.Sprintf("%s%019d", denialEventPrefix, seq), errWindowSequenceMissing)
		}
	}
	l.mu.Lock()
	if highest > l.evtSeq {
		l.evtSeq = highest
	}
	l.tailResolved = true
	l.mu.Unlock()
}

// appendIndex files one record in the derived index a query reads.
func (l *DenialLedger) appendIndex(rec DenialRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, rec)
}

// advanceSeq seats the numbering tail at one sequence the store confirmed.
func (l *DenialLedger) advanceSeq(seq int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if seq > l.evtSeq {
		l.evtSeq = seq
	}
}

// stopStream stops the stream numbering on a write whose landing is unknown or
// whose key names other content: the number stays spent and no later event takes
// a number beside it, so a restart continues past it and an event already stored
// is never re-keyed over. The error names why the stream stopped.
func (l *DenialLedger) stopStream(seq int64, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if seq > l.evtSeq {
		l.evtSeq = seq
	}
	if l.stopped == nil {
		l.stopped = err
	}
}

// governanceEventOf renders one ledger record as the governance event it is
// stored as.
func governanceEventOf(key string, rec DenialRecord) event.Event {
	content := fmt.Sprintf("[governance:%s] tool=%s level=%s rule=%s reason=%s",
		rec.Subtype, rec.ToolName, rec.Level, rec.RuleID, rec.Reason)
	meta := map[string]string{
		MetaKeySubtype: rec.Subtype,
		"tool":         rec.ToolName,
		"level":        string(rec.Level),
		"rule_id":      rec.RuleID,
		"reason":       rec.Reason,
		"args_digest":  rec.ArgsDigest,
		"goal_id":      rec.GoalID,
	}
	if rec.AgentName != "" {
		meta["agent"] = rec.AgentName
	}
	return event.Event{
		Key:       event.EventID(key),
		Type:      event.TypeGovernance,
		Timestamp: time.UnixMilli(rec.Timestamp),
		Meta:      meta,
		Message:   contentMessage(content),
	}
}

// governanceWriteFailure renders one governance event the store did not keep at
// ERROR: the stream key, the event and the store's own words, so a refused write
// is a named gap instead of a silent one.
func governanceWriteFailure(key, subtype, subject string, err error) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Errorf("tagent: governance event not persisted key=%s subtype=%s subject=%s: %v",
		key, subtype, subject, err)
}

// errWindowSequenceMissing and errEventBeyondTheWindow name the two ways a history
// answer falls short of the store beside a readable body: a sequence the seated
// window numbers but the store does not hold, and a stored event past the tail the
// restore reached. Each is named per key at ERROR (see governanceHistoryUnreadable).
var (
	errWindowSequenceMissing = errors.New("the store does not hold this sequence of the seated window")
	errEventBeyondTheWindow  = errors.New("the tail read stopped below this stored event, so the history window does not answer it")
)

// errGovernanceEventConflict reports one governance event key offered for other
// content than the store already holds under it: the identity belongs to the
// stored event, so the offered write is refused by name and nothing is rewritten.
// It is the governance member of the same-key conflict family the approval and
// hold chains answer under (errApprovalFactConflict, errHoldFactConflict): the
// judgment reads the held bytes before the write and refuses other content, and
// the frozen governance boundary forbids the memory tier, so the name is declared
// here beside the stream it guards rather than imported across that boundary.
var errGovernanceEventConflict = errors.New("governance: the event key already names other content")

// governanceHistoryUnreadable renders one way the history read falls short of the
// store at ERROR: a read the store refused, a body it would not parse, a sequence
// the seated window numbers but the store does not hold, or a stored event the
// tail read never reached. A history read has no error to hand a caller, so the
// records around the gap are kept and the gap itself is named — a shorter history
// is a stated boundary instead of a silent one.
func governanceHistoryUnreadable(key string, err error) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Errorf("tagent: governance history unreadable key=%s: %v", key, err)
}

// recordOfEvent rebuilds one ledger record from the governance event it was
// written as. An entry the event does not model reads as the plain default.
func recordOfEvent(evt event.Event) DenialRecord {
	return DenialRecord{
		Subtype:    evt.Meta[MetaKeySubtype],
		ToolName:   evt.Meta["tool"],
		Level:      parseRiskLevel(evt.Meta["level"]),
		RuleID:     evt.Meta["rule_id"],
		Reason:     evt.Meta["reason"],
		ArgsDigest: evt.Meta["args_digest"],
		GoalID:     evt.Meta["goal_id"],
		AgentName:  evt.Meta["agent"],
		Timestamp:  evt.Timestamp.UnixMilli(),
	}
}

// parseRiskLevel reads one persisted risk grade back as its declared word; a
// word the vocabulary does not carry reads as the conservative medium grade.
func parseRiskLevel(s string) plugin.RiskLevel {
	switch s {
	case string(plugin.RiskLow):
		return plugin.RiskLow
	case string(plugin.RiskMedium):
		return plugin.RiskMedium
	case string(plugin.RiskHigh):
		return plugin.RiskHigh
	case string(plugin.RiskCritical):
		return plugin.RiskCritical
	default:
		return plugin.RiskMedium
	}
}

// contentMessage carries one governance event's readable body.
func contentMessage(content string) trpcmodel.Message {
	return trpcmodel.Message{Content: content}
}

// putGovernanceEvent renders one governance event and writes it behind the log's
// barrier, reporting what it confirmed: a Put the store refused staged nothing,
// while a barrier that failed leaves an event whose landing is unknown. A failure
// is never absorbed here — the caller names it and keeps its numbering off it.
func putGovernanceEvent(ctx context.Context, log durable.FactLog, key string, evt event.Event) (bool, error) {
	raw, err := json.Marshal(evt)
	if err != nil {
		return false, fmt.Errorf("governance: render the event at %s: %w", key, err)
	}
	return putGovernanceRaw(ctx, log, key, raw)
}

// putGovernanceRaw stages one rendered event and completes the barrier over it,
// naming the leg that failed. The caller holds the bytes it compared against the
// store, so the write commits exactly the body the identity judgment read.
func putGovernanceRaw(ctx context.Context, log durable.FactLog, key string, raw []byte) (bool, error) {
	if err := log.Put(ctx, key, raw); err != nil {
		return false, fmt.Errorf("governance: write the event at %s: %w", key, err)
	}
	if err := log.Sync(ctx); err != nil {
		return true, fmt.Errorf("governance: complete the write barrier at %s: %w", key, err)
	}
	return true, nil
}

// governanceEventsPast returns the stored governance events of one stream that
// landed past a cut, ordered by the key they were written under — the order the
// operations happened in — beside the newest sequence those keys name. A body
// that does not parse is reported by name and fails the whole read: the count a
// reader answers and the sequence a key numbers must name one set of events, so
// a key folded past by silence is exactly the divergence this read refuses.
//
// The read pays for the tail and never for the stream behind it: past a stated
// cut it reads the one key each next sequence names and stops at the first the
// store does not hold, so a boot that is behind by a handful of events reads a
// handful. A cut at zero states that no state was ever seated, so the tail past
// it is the whole stream and the read is the one scan that stream needs.
func governanceEventsPast(ctx context.Context, log durable.FactLog, prefix string, cut int64) ([]event.Event, int64, error) {
	if log == nil {
		return nil, cut, nil
	}
	stored, err := governanceTail(ctx, log, prefix, cut)
	if err != nil {
		return nil, cut, err
	}
	keys := make([]string, 0, len(stored))
	for key := range stored {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	newest := cut
	out := make([]event.Event, 0, len(keys))
	for _, key := range keys {
		var evt event.Event
		if err := json.Unmarshal(stored[key], &evt); err != nil {
			return nil, cut, fmt.Errorf("governance: read the stored event at %s: %w", key, err)
		}
		if seq := eventSeqOf(prefix, key); seq > newest {
			newest = seq
		}
		out = append(out, evt)
	}
	return out, newest, nil
}

// governanceTail reads the stored pairs of one stream that landed past a cut: the
// stream prefix beside no cut, and the single key each next sequence names past
// one. The two readings state the same set, and only the second one leaves the
// keys behind the cut unread. The walk stops at the first sequence the store does
// not hold, which is the cheap tail a boot reads; a hole left by a refused write
// is not the chain's end, so the first writer continuing past the cut resolves the
// highest committed sequence and names the hole (see resolveContinuationTail)
// rather than treating the events behind it as unwritten.
func governanceTail(ctx context.Context, log durable.FactLog, prefix string, cut int64) (map[string][]byte, error) {
	if cut <= 0 {
		stored, err := log.Scan(ctx, prefix)
		if err != nil {
			return nil, fmt.Errorf("read the %s events: %w", prefix, err)
		}
		return stored, nil
	}
	tail := map[string][]byte{}
	for seq := cut + 1; ; seq++ {
		key := fmt.Sprintf("%s%019d", prefix, seq)
		stored, err := log.Scan(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("read the %s event: %w", key, err)
		}
		if len(stored) == 0 {
			return tail, nil
		}
		for held, raw := range stored {
			tail[held] = raw
		}
	}
}

// eventSeqOf reads the sequence one governance event key names, and zero for a
// key the stream does not number.
func eventSeqOf(prefix, key string) int64 {
	seq, err := strconv.ParseInt(strings.TrimPrefix(key, prefix), 10, 64)
	if err != nil {
		return 0
	}
	return seq
}
