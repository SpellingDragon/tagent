// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// Package history holds the integration contract tests of the runtime history
// side: the originals a reference names, the references that scope them, and the
// reads that must name what they cannot serve.
package history

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TestOriginalReferenceIsolation checks the original-text contract end to end on the in-memory tier:
// - identical bytes serve references of different owners and formats without any of them overwriting another
// - one key keeps every scope registered on it
// - exact recall returns the committed bytes byte for byte, and a missing or expired recall names why
// - a read runs inside the scope it is bound to, and a read that cannot serve names its failure
// - segmented reading hands out whole lines that concatenate back to the exact bytes
// - byte bounded segments stay bounded and resume exactly
// - a protection hold is cleared only by the release its holder makes and outlives one holder
func TestOriginalReferenceIsolation(t *testing.T) {
	ctx := context.Background()
	original := []byte("first line\nsecond line\n")

	t.Run("same blob keeps one reference per owner and format", func(t *testing.T) {
		store := memory.NewMemoryOriginalStore(memory.WithRetentionWindow("keep", 24*time.Hour))
		wants := []memory.OriginalRef{
			{Key: "0a01", Owner: event.Address{AgentID: "writer-a", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"},
			{Key: "0b02", Owner: event.Address{AgentID: "writer-b", SessionID: "s2"}, Format: "text/markdown", RetentionPolicy: "keep"},
		}
		for _, want := range wants {
			if err := store.Put(ctx, want, original); err != nil {
				t.Fatalf("Put(%s): %v", want.Key, err)
			}
		}
		if n := store.BlobCount(); n != 1 {
			t.Fatalf("BlobCount() = %d, want 1 stored blob for identical bytes", n)
		}
		for _, want := range wants {
			got := store.References(want.Key)
			if len(got) != 1 || got[0].Owner != want.Owner || got[0].Format != want.Format || got[0].RetentionPolicy != want.RetentionPolicy {
				t.Fatalf("References(%s) = %+v, want only the reference registered as owner=%s format=%s policy=%s", want.Key, got, want.Owner.AgentID, want.Format, want.RetentionPolicy)
			}
			recalled := store.Get(ctx, want.Key)
			if recalled.Outcome != memory.RecallFound || !bytes.Equal(recalled.Body, original) {
				t.Fatalf("Get(%s) = outcome %v body %q, want found with %q", want.Key, recalled.Outcome, recalled.Body, original)
			}
		}
	})

	t.Run("one key keeps every scope registered on it", func(t *testing.T) {
		store := memory.NewMemoryOriginalStore(memory.WithRetentionWindow("keep", 24*time.Hour))
		first := memory.OriginalRef{Key: "0c03", Owner: event.Address{AgentID: "writer-a", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"}
		second := memory.OriginalRef{Key: "0c03", Owner: event.Address{AgentID: "writer-b", SessionID: "s2"}, Format: "application/json", RetentionPolicy: "keep"}
		for _, ref := range []memory.OriginalRef{first, second} {
			if err := store.Put(ctx, ref, original); err != nil {
				t.Fatalf("Put(%s/%s): %v", ref.Owner.AgentID, ref.Format, err)
			}
		}
		got := store.References("0c03")
		if len(got) != 2 {
			t.Fatalf("References(0c03) = %+v, want the two scopes registered side by side", got)
		}
		for _, want := range []memory.OriginalRef{first, second} {
			matched := false
			for _, ref := range got {
				if ref.Owner == want.Owner && ref.Format == want.Format && ref.RetentionPolicy == want.RetentionPolicy {
					matched = true
				}
			}
			if !matched {
				t.Fatalf("References(0c03) = %+v, want the scope owner=%s format=%s policy=%s kept beside the other", got, want.Owner.AgentID, want.Format, want.RetentionPolicy)
			}
		}
		if n := store.BlobCount(); n != 1 {
			t.Fatalf("BlobCount() = %d, want 1 stored blob for identical bytes", n)
		}
		recalled := store.Get(ctx, "0c03")
		if recalled.Outcome != memory.RecallFound || !bytes.Equal(recalled.Body, original) {
			t.Fatalf("Get(0c03) = outcome %v body %q, want found with %q", recalled.Outcome, recalled.Body, original)
		}
	})

	t.Run("missing and expired recall are named", func(t *testing.T) {
		store := memory.NewMemoryOriginalStore(
			memory.WithRetentionWindow("transient", 0),
			memory.WithRetentionWindow("keep", 24*time.Hour),
		)
		missing := store.Get(ctx, "0d04")
		if missing.Outcome != memory.RecallMissing || missing.Body != nil || missing.Err != nil {
			t.Fatalf("Get(0d04) = %+v, want named missing without body or error", missing)
		}
		transient := memory.OriginalRef{Key: "0e05", Owner: event.Address{AgentID: "writer-a", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "transient"}
		if err := store.Put(ctx, transient, original); err != nil {
			t.Fatalf("Put(0e05): %v", err)
		}
		expired := store.Get(ctx, "0e05")
		if expired.Outcome != memory.RecallExpired || expired.Body != nil {
			t.Fatalf("Get(0e05) = outcome %v body %q, want named expired without body", expired.Outcome, expired.Body)
		}
		if _, err := store.ReadSegment(ctx, "0d04", 1, 1); !errors.Is(err, memory.ErrOriginalMissing) {
			t.Fatalf("ReadSegment(0d04) error = %v, want %v", err, memory.ErrOriginalMissing)
		}
		if _, err := store.ReadSegment(ctx, "0e05", 1, 1); !errors.Is(err, memory.ErrOriginalExpired) {
			t.Fatalf("ReadSegment(0e05) error = %v, want %v", err, memory.ErrOriginalExpired)
		}
		kept := memory.OriginalRef{Key: "0e05", Owner: event.Address{AgentID: "writer-b", SessionID: "s2"}, Format: "text/markdown", RetentionPolicy: "keep"}
		if err := store.Put(ctx, kept, original); err != nil {
			t.Fatalf("Put(0e05 kept scope): %v", err)
		}
		recalled := store.Get(ctx, "0e05")
		if recalled.Outcome != memory.RecallFound || !bytes.Equal(recalled.Body, original) {
			t.Fatalf("Get(0e05) = outcome %v body %q, want found while one scope is inside its window", recalled.Outcome, recalled.Body)
		}
	})

	t.Run("a read runs inside the scope it is bound to", func(t *testing.T) {
		store := memory.NewMemoryOriginalStore(
			memory.WithRetentionWindow("transient", 0),
			memory.WithRetentionWindow("keep", 24*time.Hour),
		)
		key := event.EventID("0j10")
		short := memory.OriginalRef{Key: key, Owner: event.Address{AgentID: "writer-a", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "transient"}
		kept := memory.OriginalRef{Key: key, Owner: event.Address{AgentID: "writer-b", SessionID: "s2"}, Format: "text/markdown", RetentionPolicy: "keep"}
		for _, ref := range []memory.OriginalRef{short, kept} {
			if err := store.Put(ctx, ref, original); err != nil {
				t.Fatalf("Put(%s/%s): %v", ref.Owner.AgentID, ref.RetentionPolicy, err)
			}
		}
		if recalled := store.Get(ctx, key); recalled.Outcome != memory.RecallFound {
			t.Fatalf("Get(%s) = outcome %v, want found while one reference is inside its window", key, recalled.Outcome)
		}
		keptReader, err := memory.NewScopedReader(store, kept.ReadScope())
		if err != nil {
			t.Fatalf("NewScopedReader(kept scope): %v", err)
		}
		if recalled := keptReader.Get(ctx, key); recalled.Outcome != memory.RecallFound || !bytes.Equal(recalled.Body, original) {
			t.Fatalf("scoped Get(%s) = outcome %v, want found inside the kept scope", key, recalled.Outcome)
		}
		shortReader, err := memory.NewScopedReader(store, short.ReadScope())
		if err != nil {
			t.Fatalf("NewScopedReader(transient scope): %v", err)
		}
		if recalled := shortReader.Get(ctx, key); recalled.Outcome != memory.RecallExpired || recalled.Body != nil {
			t.Fatalf("scoped Get(%s) = outcome %v body %q, want expired inside the transient scope alone", key, recalled.Outcome, recalled.Body)
		}
		stranger := memory.ReadScope{Owner: event.Address{AgentID: "writer-c", SessionID: "s3"}, Format: "text/plain", RetentionPolicy: "keep"}
		strangerReader, err := memory.NewScopedReader(store, stranger)
		if err != nil {
			t.Fatalf("NewScopedReader(stranger scope): %v", err)
		}
		if recalled := strangerReader.Get(ctx, key); recalled.Outcome != memory.RecallMissing || recalled.Body != nil || recalled.Err != nil {
			t.Fatalf("scoped Get(%s) = %+v, want named missing without body or error for a foreign scope", key, recalled)
		}
		if _, err := memory.NewScopedReader(store, memory.ReadScope{}); !errors.Is(err, memory.ErrScopeRequired) {
			t.Fatalf("NewScopedReader(zero scope) error = %v, want %v", err, memory.ErrScopeRequired)
		}
	})

	t.Run("a read that fails is named", func(t *testing.T) {
		store := memory.NewMemoryOriginalStore()
		ref := memory.OriginalRef{Key: "0f06", Owner: event.Address{AgentID: "writer-a", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"}
		if err := store.Put(ctx, ref, original); err != nil {
			t.Fatalf("Put(0f06): %v", err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		recalled := store.Get(cancelled, "0f06")
		if recalled.Outcome != memory.RecallFailed || recalled.Err == nil {
			t.Fatalf("Get(cancelled, 0f06) = %+v, want named failure carrying its cause", recalled)
		}
	})

	t.Run("segmented reading keeps whole lines byte identical", func(t *testing.T) {
		body := []byte("第一行：中文与 English 混排\nsecond line\n\n第四行末尾不换行")
		store := memory.NewMemoryOriginalStore()
		ref := memory.OriginalRef{Key: "0g07", Owner: event.Address{AgentID: "writer-a", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"}
		if err := store.Put(ctx, ref, body); err != nil {
			t.Fatalf("Put(0g07): %v", err)
		}
		whole, err := store.ReadSegment(ctx, "0g07", 1, 100)
		if err != nil || !bytes.Equal(whole, body) {
			t.Fatalf("ReadSegment(0g07, 1, 100) = %q, %v, want the whole original", whole, err)
		}
		var assembled []byte
		for line := 1; ; line++ {
			segment, err := store.ReadSegment(ctx, "0g07", line, 1)
			if errors.Is(err, memory.ErrSegmentPastEnd) {
				break
			}
			if err != nil {
				t.Fatalf("ReadSegment(0g07, %d, 1): %v", line, err)
			}
			if !utf8.Valid(segment) {
				t.Fatalf("ReadSegment(0g07, %d, 1) = %q, cut a multi-byte character", line, segment)
			}
			assembled = append(assembled, segment...)
		}
		if !bytes.Equal(assembled, body) {
			t.Fatalf("line segments concatenate to %q, want %q", assembled, body)
		}
		tail, err := store.ReadSegment(ctx, "0g07", 2, math.MaxInt)
		if err != nil || !bytes.Equal(tail, body[len("第一行：中文与 English 混排\n"):]) {
			t.Fatalf("ReadSegment(0g07, 2, MaxInt) = %q, %v, want the lines after the first", tail, err)
		}
		if _, err := store.ReadSegment(ctx, "0g07", 0, 1); !errors.Is(err, memory.ErrSegmentRange) {
			t.Fatalf("ReadSegment(0g07, 0, 1) error = %v, want %v", err, memory.ErrSegmentRange)
		}
		if _, err := store.ReadSegment(ctx, "0g07", 1, 0); !errors.Is(err, memory.ErrSegmentRange) {
			t.Fatalf("ReadSegment(0g07, 1, 0) error = %v, want %v", err, memory.ErrSegmentRange)
		}
		if _, err := store.ReadSegment(ctx, "0g07", 5, 1); !errors.Is(err, memory.ErrSegmentPastEnd) {
			t.Fatalf("ReadSegment(0g07, 5, 1) error = %v, want %v", err, memory.ErrSegmentPastEnd)
		}
	})

	t.Run("byte bounded segments stay bounded and resume exactly", func(t *testing.T) {
		body := append(bytes.Repeat([]byte("a"), 3*int(memory.DefaultSegmentMaxBytes)), []byte("\r\n混排 ✓")...)
		store := memory.NewMemoryOriginalStore()
		ref := memory.OriginalRef{Key: "0k11", Owner: event.Address{AgentID: "writer-a", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"}
		if err := store.Put(ctx, ref, body); err != nil {
			t.Fatalf("Put(0k11): %v", err)
		}
		reader, err := memory.NewScopedReader(store, ref.ReadScope())
		if err != nil {
			t.Fatalf("NewScopedReader: %v", err)
		}
		first, err := reader.ReadSegment(ctx, ref.Key, memory.SegmentRequest{})
		if err != nil {
			t.Fatalf("ReadSegment(0k11, zero MaxBytes): %v", err)
		}
		if len(first.Body) != int(memory.DefaultSegmentMaxBytes) || first.EOF {
			t.Fatalf("zero MaxBytes segment = %d bytes eof=%v, want %d bytes with more to come", len(first.Body), first.EOF, memory.DefaultSegmentMaxBytes)
		}
		// Walk on with an explicit bound that cuts multi-byte characters, each
		// request resuming from the Next of the result before it.
		const bound = 5
		assembled := append([]byte(nil), first.Body...)
		at := memory.SegmentRequest{From: first.Next, MaxBytes: bound}
		for {
			segment, err := reader.ReadSegment(ctx, ref.Key, at)
			if err != nil {
				t.Fatalf("ReadSegment(0k11, %+v): %v", at, err)
			}
			if len(segment.Body) > bound {
				t.Fatalf("segment of %d bytes exceeds the bound %d", len(segment.Body), bound)
			}
			assembled = append(assembled, segment.Body...)
			if segment.EOF {
				if segment.Next != (memory.SegmentPosition{}) {
					t.Fatalf("EOF segment carries resumption point %+v, want the zero position", segment.Next)
				}
				break
			}
			if segment.Next.Offset != at.From.Offset+int64(len(segment.Body)) {
				t.Fatalf("segment Next = %+v, want offset %d", segment.Next, at.From.Offset+int64(len(segment.Body)))
			}
			at = memory.SegmentRequest{From: segment.Next, MaxBytes: bound}
		}
		if !bytes.Equal(assembled, body) {
			t.Fatalf("byte segments concatenate to %d bytes, want the exact %d original bytes", len(assembled), len(body))
		}
		if _, err := reader.ReadSegment(ctx, ref.Key, memory.SegmentRequest{MaxBytes: -1}); !errors.Is(err, memory.ErrSegmentLimit) {
			t.Fatalf("negative MaxBytes error = %v, want %v", err, memory.ErrSegmentLimit)
		}
		if _, err := reader.ReadSegment(ctx, ref.Key, memory.SegmentRequest{From: memory.SegmentPosition{Offset: -1}}); !errors.Is(err, memory.ErrSegmentOffset) {
			t.Fatalf("negative offset error = %v, want %v", err, memory.ErrSegmentOffset)
		}
		if _, err := reader.ReadSegment(ctx, ref.Key, memory.SegmentRequest{From: memory.SegmentPosition{Offset: int64(len(body)) + 1}}); !errors.Is(err, memory.ErrSegmentPastEnd) {
			t.Fatalf("offset past end error = %v, want %v", err, memory.ErrSegmentPastEnd)
		}
	})

	t.Run("protection holds outlive one holder", func(t *testing.T) {
		guard := memory.NewMemoryGuard()
		holderA := memory.OriginalRef{Key: "0h08", Owner: event.Address{AgentID: "keeper-a", SessionID: "s1"}}
		holderB := memory.OriginalRef{Key: "0h08", Owner: event.Address{AgentID: "keeper-b", SessionID: "s2"}}
		if _, err := guard.Hold(ctx, holderA, ""); !errors.Is(err, memory.ErrHolderRequired) {
			t.Fatalf("Hold(unnamed holder) error = %v, want %v", err, memory.ErrHolderRequired)
		}
		leaseA, err := guard.Hold(ctx, holderA, memory.HolderID("keeper-a"))
		if err != nil {
			t.Fatalf("Hold(keeper-a): %v", err)
		}
		assertProtected(t, guard, holderA, true)
		retried, err := guard.Hold(ctx, holderA, memory.HolderID("keeper-a"))
		if err != nil {
			t.Fatalf("Hold(keeper-a) repeated: %v", err)
		}
		if retried != leaseA {
			t.Fatalf("Hold(keeper-a) repeated = lease %q, want the idempotent %q", retried, leaseA)
		}
		if err := guard.Release(ctx, holderA, memory.HolderID("keeper-a"), leaseA); err != nil {
			t.Fatalf("Release(keeper-a): %v", err)
		}
		assertProtected(t, guard, holderA, false)
		freshA, err := guard.Hold(ctx, holderA, memory.HolderID("keeper-a"))
		if err != nil {
			t.Fatalf("Hold(keeper-a) re-held: %v", err)
		}
		if freshA == leaseA {
			t.Fatalf("Hold(keeper-a) after release = lease %q, want a fresh lease distinct from %q", freshA, leaseA)
		}
		if err := guard.Release(ctx, holderA, memory.HolderID("keeper-a"), leaseA); !errors.Is(err, memory.ErrLeaseMismatch) {
			t.Fatalf("Release(keeper-a, stale lease) error = %v, want %v", err, memory.ErrLeaseMismatch)
		}
		assertProtected(t, guard, holderA, true)
		leaseB, err := guard.Hold(ctx, holderB, memory.HolderID("keeper-b"))
		if err != nil {
			t.Fatalf("Hold(keeper-b): %v", err)
		}
		if err := guard.Release(ctx, holderA, memory.HolderID("keeper-a"), freshA); err != nil {
			t.Fatalf("Release(keeper-a, current lease): %v", err)
		}
		assertProtected(t, guard, holderB, true)
		if err := guard.Release(ctx, holderB, memory.HolderID("keeper-b"), freshA); !errors.Is(err, memory.ErrLeaseMismatch) {
			t.Fatalf("Release(keeper-b, foreign lease) error = %v, want %v", err, memory.ErrLeaseMismatch)
		}
		assertProtected(t, guard, holderB, true)
		if err := guard.Release(ctx, holderB, memory.HolderID("keeper-b"), leaseB); err != nil {
			t.Fatalf("Release(keeper-b): %v", err)
		}
		assertProtected(t, guard, holderB, false)
		if err := guard.Release(ctx, holderB, memory.HolderID("keeper-b"), leaseB); err != nil {
			t.Fatalf("Release(keeper-b) repeated: %v", err)
		}
		assertProtected(t, guard, memory.OriginalRef{Key: "0i09", Owner: holderB.Owner}, false)
	})
}

// assertProtected fails the test when the protection state of the original does
// not match the expected one.
func assertProtected(t *testing.T, guard memory.Guard, ref memory.OriginalRef, want bool) {
	t.Helper()
	got, err := guard.Protected(context.Background(), ref)
	if err != nil {
		t.Fatalf("Protected(%s): %v", ref.Key, err)
	}
	if got != want {
		t.Fatalf("Protected(%s) = %v, want %v", ref.Key, got, want)
	}
}
