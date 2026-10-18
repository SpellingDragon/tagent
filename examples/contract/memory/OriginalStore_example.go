package memoryexample

import (
	"context"
	"strings"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// byteStore is the canonical original-text port: it commits the bytes a reference names
// and returns them byte for byte, whole or as segments.
type byteStore struct {
	bodies map[event.EventID][]byte
}

// Put commits the original bytes of ref.
func (s *byteStore) Put(_ context.Context, ref memory.OriginalRef, body []byte) error {
	if s.bodies == nil {
		s.bodies = map[event.EventID][]byte{}
	}
	s.bodies[ref.Key] = append([]byte(nil), body...)
	return nil
}

// Get returns the exact recall result for key: the original bytes, or a named missing
// classification.
func (s *byteStore) Get(_ context.Context, key event.EventID) memory.RecallResult {
	body, ok := s.bodies[key]
	if !ok {
		return memory.RecallResult{Key: key, Outcome: memory.RecallMissing}
	}
	return memory.RecallResult{Key: key, Outcome: memory.RecallFound, Body: append([]byte(nil), body...)}
}

// ReadSegment returns at most numLines lines of the original starting at line startLine.
func (s *byteStore) ReadSegment(_ context.Context, key event.EventID, startLine, numLines int) ([]byte, error) {
	body, ok := s.bodies[key]
	if !ok {
		return nil, memory.ErrOriginalMissing
	}
	lines := strings.Split(string(body), "\n")
	if startLine >= len(lines) {
		return nil, nil
	}
	end := startLine + numLines
	if numLines < 0 || end > len(lines) {
		end = len(lines)
	}
	return []byte(strings.Join(lines[startLine:end], "\n")), nil
}

var _ memory.OriginalStore = (*byteStore)(nil)

// ExampleOriginalStore commits one original, reads it back byte for byte, reads its first
// line as a segment and reports an uncommitted key as missing by name.
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func ExampleOriginalStore() {
	ctx := context.Background()
	store := &byteStore{}
	ref := memory.OriginalRef{
		Key:             "event-1",
		Owner:           event.Address{AgentID: "assistant", SessionID: "session-1"},
		Format:          "text",
		RetentionPolicy: "durable",
	}

	if err := store.Put(ctx, ref, []byte("first line\nsecond line")); err != nil {
		return
	}
	if result := store.Get(ctx, ref.Key); result.Outcome != memory.RecallFound || string(result.Body) != "first line\nsecond line" {
		return
	}
	segment, err := store.ReadSegment(ctx, ref.Key, 1, 1)
	if err != nil || string(segment) != "second line" {
		return
	}
	if result := store.Get(ctx, "event-missing"); result.Outcome != memory.RecallMissing {
		return
	}
}
