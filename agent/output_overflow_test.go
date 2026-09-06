package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestRunFlow_SlowConsumer_OverflowsToDisk is the F2 regression
// (design-report-closeout), exercised at the deliverEvent layer that RunFlow
// uses for every outputCh send. A consumer that never reads must not block
// the loop past the grace period: the full event is persisted under
// tool-output/output-overflow/ and a ticket excerpt is attempted
// non-blocking. Fail-before: the previous select blocked forever on send.
func TestRunFlow_SlowConsumer_OverflowsToDisk(t *testing.T) {
	dir := t.TempDir()
	cm := &ContextManager{
		outputCh:    make(chan *event.Event, 1),
		overflowDir: filepath.Join(dir, "output-overflow"),
	}
	cm.outputCh <- &event.Event{} // pre-fill: consumer stalled, buffer full

	evt := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{{Message: model.Message{
				Role:    "assistant",
				Content: longContent(),
			}}},
		},
	}
	done := make(chan bool, 1)
	go func() { done <- cm.deliverEvent(context.Background(), evt) }()

	select {
	case <-done:
		// Loop NOT blocked past the grace period: pass.
	case <-time.After(outputSendGrace + 2*time.Second):
		t.Fatal("F2 regression: deliverEvent blocked on a stalled consumer")
	}

	// Full event persisted and retrievable.
	matches, err := filepath.Glob(filepath.Join(dir, "output-overflow", "overflow-*.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("overflow file not persisted: matches=%v err=%v", matches, err)
	}
	// Ticket attempted: the pre-filled junk event occupies the buffer, so the
	// ticket goes to the default branch — the file is the durable record.
	if len(cm.outputCh) != 1 {
		t.Fatalf("ticket must not block: channel len=%d", len(cm.outputCh))
	}
}

// TestOverflowTicketExcerpt bounds the ticket: head+tail kept, middle
// replaced by the persisted marker, StateDelta carries the retrieval path.
func TestOverflowTicketExcerpt(t *testing.T) {
	evt := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{{Message: model.Message{Content: longContent()}}},
		},
	}
	ticket := overflowTicket(evt, "/tmp/x.json")
	got := ticket.Response.Choices[0].Message.Content
	if len(got) >= len(longContent()) {
		t.Fatalf("ticket not excerpted: len=%d", len(got))
	}
	if string(ticket.StateDelta["output_overflow_path"]) != "/tmp/x.json" {
		t.Fatal("ticket missing output_overflow_path")
	}
	// Original event untouched (clone semantics).
	if evt.Response.Choices[0].Message.Content != longContent() {
		t.Fatal("original event mutated by ticket build")
	}
}

func longContent() string {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return string(b)
}

// TestDumpOverflowEvent writes a JSON file on demand (dir created).
func TestDumpOverflowEvent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "output-overflow")
	path, err := dumpOverflowEvent(dir, &event.Event{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("overflow file missing: %v", err)
	}
}
