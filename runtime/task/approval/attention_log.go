// The durable face of the attention chain: the fact one question, answer or
// clearance is filed as, written behind the store's barrier under the identity it
// stands for and folded back into the chain a restarted process reads. The chain
// is the truth of every presence and every ruling the approval channel answers,
// so it outlives the process instead of being the one record the cut happens to
// state. The write-and-read surface it fills is durable.FactLog, declared beside
// the other ledgers' surfaces; the rules below are this chain's own filing and
// folding obligations over that surface.
//
// - one fact is one key, so a repeated filing rewrites nothing and no sequence the stream skipped can sit beside it
// - a write the store refused is named to the caller and at ERROR, and the fact it refused is not reported as durable
// - a fold orders the stored facts by the landing stamp they carry, ties read by the identity, so a read never depends on the map the store answers in
//
// The redline list (2 rules):
// MUST refuse a stored pair whose body states another fact than its key names.
// MUST fold the stored facts into the chain beside presence and ruling it already holds, so a read answers both alike.
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/api/durable"
	"github.com/SpellingDragon/tagent/api/event"
	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
)

// attentionFactPrefix is the stream the attention facts are stored under. One
// fact is one key, so the stream has no numbering a refused write could skip.
const attentionFactPrefix = "tagent/attention/v1/fact:"

// approvalRequestPrefix is the stream the request records are stored under: one
// record per caller identity, so the identity an approver was handed survives a
// process that lost the verdict of its cut. The chain states which questions and
// rulings stand; this stream states what each identity was answered with.
const approvalRequestPrefix = "tagent/approval/v1/request:"

// factKey answers the durable key one fact identity is stored under.
func factKey(id event.EventID) string { return attentionFactPrefix + string(id) }

// approvalRequestKey answers the durable key one caller identity is seated under.
func approvalRequestKey(id string) string { return approvalRequestPrefix + id }

// approvalRequestRecord is the body one stored request record carries: the caller
// identity it stands for and the request that identity was answered with. The
// identity is stated in the body beside the key, so a pair storing another
// identity than its key names is refused rather than seated.
type approvalRequestRecord struct {
	// Asked is the caller identity the record stands under.
	Asked string `json:"asked"`
	// Request is the request that identity was answered with.
	Request ApprovalRequest `json:"request"`
}

// storeRequestRecord writes one request record behind the store's barrier, so the
// identity a registration answered with outlives the process instead of living in
// the cut alone. A write the store refused is reported to the caller that
// registered the request, and never as landed.
func (a *ApprovalManager) storeRequestRecord(ctx context.Context, log durable.FactLog, asked string, req ApprovalRequest) error {
	raw, err := json.Marshal(approvalRequestRecord{Asked: asked, Request: req})
	if err != nil {
		return fmt.Errorf("approval: render the request record %s: %w", asked, err)
	}
	key := approvalRequestKey(asked)
	if err := log.Put(ctx, key, raw); err != nil {
		return fmt.Errorf("approval: write the request record %s: %w", key, err)
	}
	if err := log.Sync(ctx); err != nil {
		return fmt.Errorf("approval: complete the write barrier at %s: %w", key, err)
	}
	return nil
}

// foldRequestRecords seats every stored request record in the evidence block, so
// a restarted book answers the identities the process before it handed an
// approver — beside the cut it restored, never instead of it. A pair storing
// another identity than its key names is refused by name: a book that seated it
// would answer an identity the store never held.
func (a *ApprovalManager) foldRequestRecords(ctx context.Context, log durable.FactLog) error {
	stored, err := log.Scan(ctx, approvalRequestPrefix)
	if err != nil {
		return fmt.Errorf("approval: read the request records: %w", err)
	}
	keys := make([]string, 0, len(stored))
	for key := range stored {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, key := range keys {
		var record approvalRequestRecord
		if err := json.Unmarshal(stored[key], &record); err != nil {
			return fmt.Errorf("approval: read the request record at %s: %w", key, err)
		}
		if record.Request.ID == "" || approvalRequestKey(record.Asked) != key {
			return fmt.Errorf("%w: %s states the identity %q", errApprovalFactConflict, key, record.Asked)
		}
		if _, held := a.evidence[record.Asked]; held {
			continue
		}
		seated := record.Request
		a.evidence[record.Asked] = &seated
	}
	return nil
}

// bind hands the chain its durable face and folds the facts it already holds back
// into it, so a restarted process answers the same presence and the same rulings
// the process before it answered. A pair whose body states another fact than its
// key names is refused by name: a stream this chain cannot read is not a stream it
// serves as empty.
func (l *attentionLedger) bind(ctx context.Context, log durable.FactLog) error {
	if l == nil || log == nil {
		return nil
	}
	folded, err := foldAttentionFacts(ctx, log)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, fact := range folded {
		if held, ok := l.held[fact.Key]; ok {
			if !sameFact(held, fact) {
				return fmt.Errorf("%w: %s", errApprovalFactConflict, fact.Key)
			}
			continue
		}
		l.held[fact.Key] = fact
		l.order = append(l.order, fact)
	}
	l.log = log
	return nil
}

// storeAttention writes one filed fact behind the store's barrier. A write the
// store refused is named here and reported to the caller that filed the fact, so
// a fact the chain holds in memory is never mistaken for one the store keeps.
func (l *attentionLedger) storeAttention(ctx context.Context, log durable.FactLog, fact event.Event) error {
	err := l.putAttentionFact(ctx, log, fact)
	if err != nil {
		attentionWriteFailure(fact, err)
	}
	return err
}

// putAttentionFact stages one fact and completes the barrier over it, naming the
// leg that failed.
func (l *attentionLedger) putAttentionFact(ctx context.Context, log durable.FactLog, fact event.Event) error {
	raw, err := json.Marshal(fact)
	if err != nil {
		return fmt.Errorf("approval: render the attention fact %s: %w", fact.Key, err)
	}
	key := factKey(fact.Key)
	if err := log.Put(ctx, key, raw); err != nil {
		return fmt.Errorf("approval: write the attention fact %s: %w", key, err)
	}
	if err := log.Sync(ctx); err != nil {
		return fmt.Errorf("approval: complete the write barrier at %s: %w", key, err)
	}
	return nil
}

// foldAttentionFacts reads every stored fact in landing order: the stamp the fact
// was filed at, ties read by the identity the key names, so two reads of one store
// answer the same order.
func foldAttentionFacts(ctx context.Context, log durable.FactLog) ([]event.Event, error) {
	stored, err := log.Scan(ctx, attentionFactPrefix)
	if err != nil {
		return nil, fmt.Errorf("approval: read the attention facts: %w", err)
	}
	keys := make([]string, 0, len(stored))
	for key := range stored {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	folded := make([]event.Event, 0, len(keys))
	for _, key := range keys {
		var fact event.Event
		if err := json.Unmarshal(stored[key], &fact); err != nil {
			return nil, fmt.Errorf("approval: read the attention fact at %s: %w", key, err)
		}
		if factKey(fact.Key) != key {
			return nil, fmt.Errorf("%w: %s states the fact %s", errApprovalFactConflict, key, fact.Key)
		}
		folded = append(folded, fact)
	}
	sort.SliceStable(folded, func(i, j int) bool { return folded[i].Timestamp.Before(folded[j].Timestamp) })
	return folded, nil
}

// sameFact reports whether two facts state one filing: the same role, the same
// identity and the same body, so a stored repeat reads as the fact already held.
func sameFact(one, other event.Event) bool {
	return one.Type == other.Type && one.Message.Content == other.Message.Content && sameMeta(one.Meta, other.Meta)
}

// attentionWriteFailure renders one attention fact the store did not keep at
// ERROR: the identity, the role and the store's own words, so a refused write is
// never a silent gap behind a chain a restart reads back.
func attentionWriteFailure(fact event.Event, err error) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Errorf("tagent: attention fact not persisted key=%s type=%s: %v", fact.Key, fact.Type, err)
}
