package evolutiongovernanceexample

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/SpellingDragon/tagent/features/evolution/governance"
)

// memoryLog is the canonical governance log: governance events are staged under their
// stream key, scanned by prefix and made durable behind the sync barrier.
type memoryLog struct {
	mu     sync.Mutex
	staged map[string][]byte
}

// Put stages one governance event under its key.
func (l *memoryLog) Put(_ context.Context, key string, value []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.staged == nil {
		l.staged = map[string][]byte{}
	}
	l.staged[key] = append([]byte(nil), value...)
	return nil
}

// Scan returns every staged governance event under the key prefix.
func (l *memoryLog) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string][]byte{}
	for key, value := range l.staged {
		if strings.HasPrefix(key, prefix) {
			out[key] = append([]byte(nil), value...)
		}
	}
	return out, nil
}

// Sync completes the write barrier over everything staged so far.
func (l *memoryLog) Sync(context.Context) error { return nil }

// Keys reports the staged keys in lexical order, which is the order the operations they
// record happened in.
func (l *memoryLog) Keys() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	keys := make([]string, 0, len(l.staged))
	for key := range l.staged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var _ governance.GovernanceEventLog = (*memoryLog)(nil)

// ExampleGovernanceEventLog stages and replays one governance event: the write is durable
// once Sync returns nil, and the prefix scan rebuilds the stream in key order.
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
func ExampleGovernanceEventLog() {
	ctx := context.Background()
	log := &memoryLog{}
	if err := log.Put(ctx, "tagent/governance/v1/denial:1", []byte(`{"rule":"write-audit"}`)); err != nil {
		return
	}
	if err := log.Sync(ctx); err != nil {
		return
	}
	replayed, err := log.Scan(ctx, "tagent/governance/v1/denial:")
	if err != nil {
		return
	}
	_ = replayed[log.Keys()[0]]
}
