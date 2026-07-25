package compress

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

func TestSessionProjection_AppendAndGetAll(t *testing.T) {
	p := NewSessionProjection()
	if p.Len() != 0 {
		t.Fatalf("expected empty projection, got len=%d", p.Len())
	}

	ref := memory.EventReference{
		EventKey:     123,
		PartitionID:  42,
		EventType:    "external_input",
		EventSummary: "hello",
		Timestamp:    1000,
	}
	p.Append(ref)

	if p.Len() != 1 {
		t.Fatalf("expected len=1, got %d", p.Len())
	}

	all := p.GetAll()
	if len(all) != 1 {
		t.Fatalf("expected GetAll len=1, got %d", len(all))
	}
	if all[0].EventKey != 123 {
		t.Fatalf("expected EventKey=123, got %d", all[0].EventKey)
	}

	// Mutating the returned slice must not affect the projection.
	all[0].EventKey = 999
	all = p.GetAll()
	if all[0].EventKey != 123 {
		t.Fatalf("projection should return defensive copy")
	}
}

func TestSessionProjection_Replace(t *testing.T) {
	p := NewSessionProjection()
	p.Append(memory.EventReference{EventKey: 1})
	p.Append(memory.EventReference{EventKey: 2})

	p.Replace([]memory.EventReference{
		{EventKey: 3},
	})

	if p.Len() != 1 {
		t.Fatalf("expected len=1 after Replace, got %d", p.Len())
	}
	if p.GetAll()[0].EventKey != 3 {
		t.Fatalf("expected EventKey=3, got %d", p.GetAll()[0].EventKey)
	}
}

func TestSessionProjection_Concurrent(t *testing.T) {
	p := NewSessionProjection()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			p.Append(memory.EventReference{EventKey: int64(i)})
		}
		done <- struct{}{}
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = p.GetAll()
		}
		done <- struct{}{}
	}()

	<-done
	<-done

	if p.Len() != 100 {
		t.Fatalf("expected len=100, got %d", p.Len())
	}
}
