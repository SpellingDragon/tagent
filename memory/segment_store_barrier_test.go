package memory_test

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
)

// TestStoreEventDurableWithoutClose proves the EVENT-LEVEL durability barrier
// (resident-readiness-plan 2.1, delta spec event-segment-store「事件级成功不早
// 于屏障」): a single StoreEvent below the WAL flush threshold must be readable
// by a FRESH process even when the writer process terminates without Close.
//
// Child mode (env guard) writes one event and exits WITHOUT Close — the
// deferred-flush window (50 writes / 2s) means the pre-barrier implementation
// loses it; the parent then fails. With the StoreEvent barrier in place the
// child's acknowledged write is on disk and the parent reads it back.
func TestStoreEventDurableWithoutClose(t *testing.T) {
	const marker = "barrier-durable-marker"

	if os.Getenv("TAGENT_BARRIER_SUBPROC") == "1" {
		dir := os.Getenv("TAGENT_BARRIER_DIR")
		k, err := kv.NewLocalFileKV(dir) // fsync default ON
		if err != nil {
			fmt.Fprintln(os.Stderr, "child: open kv:", err)
			os.Exit(2)
		}
		store, err := memory.NewFileSegmentStore(k, nil, dir, 100)
		if err != nil {
			fmt.Fprintln(os.Stderr, "child: open store:", err)
			os.Exit(2)
		}
		pid := memory.PartitionIDFromName("barrier")
		key := memory.NewSnowflakeEventKey(pid, 0)
		evt := memory.FullEvent{
			EventKey:     key,
			PartitionID:  pid,
			EventType:    "external_input",
			EventSummary: marker,
			Content:      marker,
			Timestamp:    1700000000000,
			Metadata:     map[string]string{"m": "1"},
		}
		if err := store.StoreEvent(key, evt); err != nil {
			fmt.Fprintln(os.Stderr, "child: StoreEvent:", err)
			os.Exit(3)
		}
		// Acknowledged → terminate WITHOUT Close. No flush-tick, no final
		// flush: only a real barrier inside StoreEvent can persist this.
		os.Exit(0)
	}

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestStoreEventDurableWithoutClose$", "-test.v")
	cmd.Env = append(os.Environ(),
		"TAGENT_BARRIER_SUBPROC=1",
		"TAGENT_BARRIER_DIR="+dir,
	)
	if out, err := cmd.CombinedOutput(); err != nil || os.Getenv("TAGENT_BARRIER_DEBUG") != "" {
		t.Fatalf("child did not acknowledge the write (err=%v): %s", err, out)
	}

	// Fresh store over the same directory: the acknowledged event must be
	// readable with content AND index (GetEvent goes through the idx key).
	k, err := kv.NewLocalFileKV(dir)
	if err != nil {
		t.Fatalf("reopen kv: %v", err)
	}
	store, err := memory.NewFileSegmentStore(k, nil, dir, 100)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	pid := memory.PartitionIDFromName("barrier")
	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{pid},
		Keyword:      marker,
	})
	if err != nil || len(refs) == 0 {
		t.Fatalf("acknowledged event lost after unclean termination: refs=%d err=%v", len(refs), err)
	}
	evt, err := store.GetEvent(refs[0].EventKey)
	if err != nil {
		t.Fatalf("GetEvent via idx after reopen: %v", err)
	}
	if evt.Content != marker || evt.Metadata["m"] != "1" {
		t.Fatalf("event content corrupted across restart: %+v", evt)
	}
}
