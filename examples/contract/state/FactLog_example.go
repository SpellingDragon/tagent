package stateexample

import (
	"context"
	"strings"

	"github.com/SpellingDragon/tagent/state"
)

// stagedLog is the canonical durable fact surface: it stages keys, answers prefix scans in
// full and only reports durable success behind a completed barrier.
type stagedLog struct {
	staged   map[string][]byte
	barriers int
}

// Put stages one key and its value without claiming it durable.
func (l *stagedLog) Put(_ context.Context, key string, value []byte) error {
	if l.staged == nil {
		l.staged = map[string][]byte{}
	}
	l.staged[key] = append([]byte(nil), value...)
	return nil
}

// Get returns the value staged under key and whether one is held.
func (l *stagedLog) Get(_ context.Context, key string) ([]byte, bool, error) {
	value, ok := l.staged[key]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), value...), true, nil
}

// Scan returns every staged pair under the key prefix.
func (l *stagedLog) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	found := map[string][]byte{}
	for key, value := range l.staged {
		if strings.HasPrefix(key, prefix) {
			found[key] = append([]byte(nil), value...)
		}
	}
	return found, nil
}

// Sync completes the write barrier over everything staged so far.
func (l *stagedLog) Sync(context.Context) error {
	l.barriers++
	return nil
}

var _ state.FactLog = (*stagedLog)(nil)

// ExampleFactLog stages two keys, reads one back, scans them all in full and completes the
// barrier before anything is reported durable.
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func ExampleFactLog() {
	ctx := context.Background()
	log := &stagedLog{}

	if err := log.Put(ctx, "task/1/fact", []byte("committed")); err != nil {
		return
	}
	if err := log.Put(ctx, "task/1/notice", []byte("delivered")); err != nil {
		return
	}
	if value, held, err := log.Get(ctx, "task/1/fact"); err != nil || !held || string(value) != "committed" {
		return
	}
	pairs, err := log.Scan(ctx, "task/1/")
	if err != nil || len(pairs) != 2 {
		return
	}
	if err := log.Sync(ctx); err != nil || log.barriers != 1 {
		return
	}
}
