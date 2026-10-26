// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
//
// TierFacts is the durable fact surface of one opened tier: put, get, prefix
// scan and the sync barrier a commit reports its success behind. It commits
// through the same key-value backend the tier's segment store uses, so the
// replay and the durable task commit share one database with the events already
// stored there. Its signatures carry built-in types only, so it matches the
// state layer's fact-log port structurally and no package edge is required in
// either direction — the port is declared where it is consumed (see state's
// FactLog) and this tier names the one surface it offers.
package ledger

import (
	"context"
)

// TierFacts is the durable fact surface of one opened tier.
type TierFacts struct {
	kv kvPort
}

// TierFactLog returns the durable fact surface of one opened tier, so a durable
// commit and a replay run on the same backend the tier's events are committed
// through — no second database is introduced for either.
func TierFactLog(tier *Tier) *TierFacts {
	return &TierFacts{kv: tier.Segments.kv}
}

// Put stages one key and its value.
func (l TierFacts) Put(ctx context.Context, key string, value []byte) error {
	return l.kv.kvCommit(ctx, kvBatch{puts: []kvPair{{key: key, value: value}}})
}

// Get returns the value stored under key and whether one is held.
func (l TierFacts) Get(ctx context.Context, key string) ([]byte, bool, error) {
	pairs, err := l.kv.kvRead(ctx, kvFetch{key: key})
	if err != nil {
		return nil, false, err
	}
	if len(pairs) == 0 {
		return nil, false, nil
	}
	return pairs[0].value, true, nil
}

// Scan returns every stored pair under the key prefix as key and value.
func (l TierFacts) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	pairs, err := l.kv.kvRead(ctx, kvFetch{prefix: prefix})
	if err != nil {
		return nil, err
	}
	found := make(map[string][]byte, len(pairs))
	for _, pair := range pairs {
		found[pair.key] = pair.value
	}
	return found, nil
}

// Sync completes the write barrier over everything staged so far.
func (l TierFacts) Sync(ctx context.Context) error {
	return l.kv.kvCommit(ctx, kvBatch{})
}
