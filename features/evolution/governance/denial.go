package governance

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// GovernanceEventLog is the durable face one governance double is written to
// and replayed from: put, prefix scan and the sync barrier a durable write
// reports its success behind. Its signatures carry built-in types only, so any
// durable backend — the memory tier's fact log among them — satisfies it
// structurally and no package edge is required in either direction.
//
// The redline list (3 rules):
// MUST key every governance event under the stream key its family numbers.
// MUST NOT report a staged write as durable before Sync returns nil.
// MUST NOT rewrite or drop a governance event already stored.
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
type GovernanceEventLog interface {
	// Put stages one governance event under its key.
	Put(ctx context.Context, key string, value []byte) error
	// Scan returns every stored governance event under the key prefix, keyed by
	// the event key it was written under.
	Scan(ctx context.Context, prefix string) (map[string][]byte, error)
	// Sync completes the write barrier over everything staged so far.
	Sync(ctx context.Context) error
}

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
// The ledger is concurrent safe, and the durable face is late-bound and nil
// safe: without a store it degrades to memory state instead of failing, and
// binding one folds the stored governance events that landed past the state cut
// this ledger holds, so a restart keeps the records it already made. The cut is
// seated by the framework state slice (see StateSeat); a ledger beside no seat
// holds the zero cut, so the events past it are the whole stream.
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
type DenialLedger struct {
	mu      sync.RWMutex
	records []DenialRecord
	log     GovernanceEventLog
	evtSeq  int64
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
func (l *DenialLedger) Bind(ctx context.Context, log GovernanceEventLog) error {
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
// here, and an event write that fails never blocks the operation — the ledger
// entry stands and the record keeps its meaning.
func (l *DenialLedger) Record(rec DenialRecord) {
	if l == nil {
		return
	}
	if rec.Timestamp == 0 {
		rec.Timestamp = time.Now().UnixMilli()
	}
	l.mu.Lock()
	l.records = append(l.records, rec)
	key := l.eventKeyLocked()
	log := l.log
	l.mu.Unlock()

	if log != nil {
		l.writeGovernanceEvent(log, key, rec)
	}
}

// Query returns the newest records, at most limit of them. A limit of zero or
// less returns all of them. The records are copies, so a reader never observes
// a concurrent write.
func (l *DenialLedger) Query(limit int) []DenialRecord {
	if l == nil {
		return nil
	}
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

// Count reports how many records the ledger holds.
func (l *DenialLedger) Count() int {
	if l == nil {
		return 0
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.records)
}

// eventKeyLocked allocates the ordered identity of the next governance event.
// The caller holds the lock, so the key order is the operation order and a
// replay reconstructs it exactly.
func (l *DenialLedger) eventKeyLocked() string {
	l.evtSeq++
	return fmt.Sprintf("%s%019d", denialEventPrefix, l.evtSeq)
}

// seat states the ledger the cut and the index one framework state slice
// restored, so a restarted ledger continues its numbering where the stated
// stream ended and reads the records the cut already folded.
func (l *DenialLedger) seat(records []DenialRecord, cut int64) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append([]DenialRecord(nil), records...)
	l.evtSeq = cut
}

// stated reports the cut and the index the framework state slice states for this
// ledger: the sequence the stored stream ended at and the records folded by then.
func (l *DenialLedger) stated() (int64, []DenialRecord) {
	if l == nil {
		return 0, nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.evtSeq, append([]DenialRecord(nil), l.records...)
}

// foldTail folds the stored governance events that landed past one cut into the
// index, in the order the operations happened, and continues the stream
// numbering from the newest event it read. A malformed event is skipped rather
// than failing the fold; the sequence its key names still counts, so a body the
// store could not parse never hands its key to a later write.
func (l *DenialLedger) foldTail(ctx context.Context, log GovernanceEventLog, cut int64) error {
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
// requires, for the host and the observation face to see. A write that fails is
// dropped beside the standing ledger entry rather than failing the record.
func (l *DenialLedger) writeGovernanceEvent(log GovernanceEventLog, key string, rec DenialRecord) {
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
	evt := event.Event{
		Key:       event.EventID(key),
		Type:      event.TypeGovernance,
		Timestamp: time.UnixMilli(rec.Timestamp),
		Meta:      meta,
		Message:   contentMessage(content),
	}
	putGovernanceEvent(context.Background(), log, key, evt)
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

// putGovernanceEvent writes one governance event behind the log's barrier. A
// failure never blocks the operation that produced the event: the governance
// double keeps its memory half and the durable half is best effort.
func putGovernanceEvent(ctx context.Context, log GovernanceEventLog, key string, evt event.Event) {
	raw, err := json.Marshal(evt)
	if err != nil {
		return
	}
	if err := log.Put(ctx, key, raw); err != nil {
		return
	}
	_ = log.Sync(ctx)
}

// governanceEventsPast returns the stored governance events of one stream that
// landed past a cut, ordered by the key they were written under — the order the
// operations happened in — beside the newest sequence those keys name. A body
// that does not parse is skipped rather than failing the read.
//
// The read pays for the tail and never for the stream behind it: past a stated
// cut it reads the one key each next sequence names and stops at the first the
// store does not hold, so a boot that is behind by a handful of events reads a
// handful. A cut at zero states that no state was ever seated, so the tail past
// it is the whole stream and the read is the one scan that stream needs.
func governanceEventsPast(ctx context.Context, log GovernanceEventLog, prefix string, cut int64) ([]event.Event, int64, error) {
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
		if seq := eventSeqOf(prefix, key); seq > newest {
			newest = seq
		}
		var evt event.Event
		if err := json.Unmarshal(stored[key], &evt); err != nil {
			continue
		}
		out = append(out, evt)
	}
	return out, newest, nil
}

// governanceTail reads the stored pairs of one stream that landed past a cut: the
// stream prefix beside no cut, and the single key each next sequence names past
// one. The two readings state the same set, and only the second one leaves the
// keys behind the cut unread.
//
// The walk ends at the first sequence the store does not hold. A governance write
// the store lost ends the tail there, and the events past it are still folded by
// the next cut's index, because the writing process keeps the record beside the
// lost durable half.
func governanceTail(ctx context.Context, log GovernanceEventLog, prefix string, cut int64) (map[string][]byte, error) {
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
