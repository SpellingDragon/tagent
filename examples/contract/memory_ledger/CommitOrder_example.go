package memoryledgerexample

import (
	"context"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// commitRecord is one commit's statement about one key: the value it leaves under the
// key, or the deletion that ends the key's history at its newest commit.
type commitRecord struct {
	key     string
	value   []byte
	deleted bool
}

// committedLog is the canonical commit-order face: it keeps the records its commits
// landed in the order they landed and answers each key once, at its newest commit.
type committedLog struct {
	records []commitRecord
}

// EachCommittedNewestFirst names each key once at its newest commit, from the newest
// record to the oldest, and ends the walk where visit answers false.
func (l *committedLog) EachCommittedNewestFirst(_ context.Context, visit func(key string, value []byte) (bool, error)) error {
	named := map[string]struct{}{}
	for index := len(l.records) - 1; index >= 0; index-- {
		record := l.records[index]
		if _, seen := named[record.key]; seen {
			continue
		}
		named[record.key] = struct{}{}
		if record.deleted {
			continue
		}
		keep, err := visit(record.key, record.value)
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}
	return nil
}

var _ ledger.CommitOrder = (*committedLog)(nil)

// ExampleCommitOrder walks the records five commits landed: it names each key once at
// its newest commit from the newest record to the oldest, skips the key whose newest
// commit deleted it, and ends a second walk where the visit answers false.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func ExampleCommitOrder() {
	ctx := context.Background()
	log := &committedLog{records: []commitRecord{
		{key: "task:1", value: []byte("first")},
		{key: "task:2", value: []byte("only")},
		{key: "task:1", value: []byte("second")},
		{key: "task:3", value: []byte("gone")},
		{key: "task:3", deleted: true},
	}}

	walked := []string{}
	if err := log.EachCommittedNewestFirst(ctx, func(key string, value []byte) (bool, error) {
		walked = append(walked, key+"="+string(value))
		return true, nil
	}); err != nil {
		return
	}
	if len(walked) != 2 || walked[0] != "task:1=second" || walked[1] != "task:2=only" {
		return
	}

	bounded := []string{}
	if err := log.EachCommittedNewestFirst(ctx, func(key string, value []byte) (bool, error) {
		bounded = append(bounded, key)
		return false, nil
	}); err != nil {
		return
	}
	if len(bounded) != 1 || bounded[0] != "task:1" {
		return
	}
}
