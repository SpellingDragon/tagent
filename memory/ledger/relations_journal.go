// 契约: docs/wiki/memory/memory-architecture.md#causal-chain
//
// The relation journal: the append-only record of one relation ledger's links
// and the replay that rebuilds them. A record is durable exactly when Append
// returns success, and the rebuilt chain names exactly the records it covers.
package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
)

// RelationsJournalName is the file name the relation journal appends to under
// the root it is opened over.
const RelationsJournalName = "relations.journal"

var (
	// ErrRelationsJournalCorrupt reports journal content that cannot be replayed
	// as the records it claims to hold, so the ledger refuses to open instead of
	// serving a shortened chain.
	ErrRelationsJournalCorrupt = errors.New("memory: relations journal holds a record it cannot replay")
	// ErrRelationsJournalBarrier reports an append whose barrier did not
	// complete, so the record is not part of the journal.
	ErrRelationsJournalBarrier = errors.New("memory: relations journal barrier failed")
)

// RelationRecord is one committed link: the child fact and its parent, with an
// empty parent recording that the child holds no parent.
type RelationRecord struct {
	// Child is the fact whose parent the record names.
	Child event.EventID `json:"child"`
	// Parent is the parent the record names, empty when the child holds none.
	Parent event.EventID `json:"parent,omitempty"`
}

// RelationsJournal is the append-only relation record under one root: one JSON
// record per line, replayed in append order so the last record of a child names
// its parent. A record whose write or barrier fails is not part of the journal,
// and a later replay never reads it back.
type RelationsJournal struct {
	mu   sync.Mutex
	path string
}

// OpenRelationsJournal opens the relation journal under root, creating the root
// and the journal file when they do not exist. A journal that exists keeps every
// complete record it already holds; bytes after the last newline are an append
// that never returned success and are dropped before the journal is used.
func OpenRelationsJournal(root string) (*RelationsJournal, error) {
	if root == "" {
		return nil, ErrRootRequired
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(root, RelationsJournalName)
	if err := trimTornJournalTail(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRelationsJournalBarrier, err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRelationsJournalBarrier, err)
	}
	return &RelationsJournal{path: path}, nil
}

// trimTornJournalTail drops the bytes after the last newline of the journal, so
// an append that never returned success cannot prefix the next committed record.
func trimTornJournalTail(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%w: %w", ErrRelationsJournalBarrier, err)
	}
	if len(data) == 0 || data[len(data)-1] == '\n' {
		return nil
	}
	size := int64(bytes.LastIndexByte(data, '\n') + 1)
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRelationsJournalBarrier, err)
	}
	defer func() { _ = file.Close() }()
	if err := file.Truncate(size); err != nil {
		return fmt.Errorf("%w: %w", ErrRelationsJournalBarrier, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("%w: %w", ErrRelationsJournalBarrier, err)
	}
	return nil
}

// Path returns the file the journal appends to.
func (j *RelationsJournal) Path() string { return j.path }

// Append commits one record behind the journal's barrier: a success covers the
// record a later replay reads back, and a failure leaves the journal as it was.
func (j *RelationsJournal) Append(record RelationRecord) error {
	if record.Child == "" {
		return ErrKeyRequired
	}
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRelationsJournalBarrier, err)
	}
	line = append(line, '\n')
	j.mu.Lock()
	defer j.mu.Unlock()
	file, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRelationsJournalBarrier, err)
	}
	defer func() { _ = file.Close() }()
	size, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRelationsJournalBarrier, err)
	}
	if _, err := file.Write(line); err != nil {
		return j.rollback(file, size, err)
	}
	if err := file.Sync(); err != nil {
		return j.rollback(file, size, err)
	}
	return nil
}

// rollback restores the journal to the length it held before a failed append, so
// a failed record leaves no bytes a replay could read.
func (j *RelationsJournal) rollback(file *os.File, size int64, cause error) error {
	if err := file.Truncate(size); err != nil {
		return fmt.Errorf("%w: %w (rollback: %v)", ErrRelationsJournalBarrier, cause, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("%w: %w (rollback: %v)", ErrRelationsJournalBarrier, cause, err)
	}
	return fmt.Errorf("%w: %w", ErrRelationsJournalBarrier, cause)
}

// Records returns the records the journal holds, in append order. Bytes after
// the last newline are an append that never returned success and are not a
// record; a complete line that cannot be read back fails the read by name.
func (j *RelationsJournal) Records(ctx context.Context) ([]RelationRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(j.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %w", ErrRelationsJournalCorrupt, err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	records := make([]RelationRecord, 0, len(lines))
	for index, line := range lines[:len(lines)-1] {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record RelationRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("%w: line %d: %w", ErrRelationsJournalCorrupt, index+1, err)
		}
		if record.Child == "" {
			return nil, fmt.Errorf("%w: line %d names no child", ErrRelationsJournalCorrupt, index+1)
		}
		records = append(records, record)
	}
	return records, nil
}
