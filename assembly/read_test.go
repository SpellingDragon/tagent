package assembly

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// recordingEmbedder records every text the vector seam is asked to embed.
type recordingEmbedder struct {
	mu    sync.Mutex
	texts []string
}

// Embed records text and answers one fixed vector.
func (e *recordingEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.texts = append(e.texts, text)
	return []float32{1}, nil
}

// recorded reports the texts seen so far.
func (e *recordingEmbedder) recorded() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.texts...)
}

// TestVectorSeamNeverSeesMechanismWording pins the seam rule.
// - the retrieval supply embeds business facts only
// - a compression mechanism fact never submits its internal summary
//
// 契约: docs/wiki/memory/memory-architecture.md#embedder-contract
func TestVectorSeamNeverSeesMechanismWording(t *testing.T) {
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageMemory})
	if err != nil {
		t.Fatalf("open the tier: %v", err)
	}
	defer tier.Segments.Close()

	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	store := func(key event.EventID, typ string, summary, content string, seq int64) {
		t.Helper()
		evt := event.Event{
			Key:       key,
			Type:      typ,
			Timestamp: at,
			Meta:      map[string]string{event.MetaKeyEventSummary: summary},
			Message:   model.Message{Content: content},
			Owner:     event.Address{AgentID: "agent-a", SessionID: "session-seam"},
		}
		body, err := memory.BodyFromEvent(evt, "test", "test")
		if err != nil {
			t.Fatalf("encode %s: %v", key, err)
		}
		encoded, err := body.Encode()
		if err != nil {
			t.Fatalf("marshal %s: %v", key, err)
		}
		if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{
			PartitionID: 1,
			WindowTS:    at.UnixMilli(),
			Seq:         seq,
			EventKey:    key,
			EventTime:   at.UnixMilli(),
			Body:        encoded,
		}.WithChain(typ, evt.Owner.SessionID)); err != nil {
			t.Fatalf("store %s: %v", key, err)
		}
	}
	store("business-134000", event.TypeExternalInput, "ask", "the material of the landing walk", 1)
	store("cmpout-product", event.CompressProductType, "compress product -hist-internal", "", 2)
	store("cmp-request", event.CompressRequestType, "compress request -hist-internal", "", 3)

	embedder := &recordingEmbedder{}
	supply, err := supplyRetrieval(ctx, tier, embedder)
	if err != nil {
		t.Fatalf("wire the retrieval supply: %v", err)
	}
	defer supply.seam.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ready := supply.seam.Vector("business-134000"); ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the business fact never produced its vector")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, text := range embedder.recorded() {
		if strings.Contains(text, "compress product") || strings.Contains(text, "compress request") {
			t.Fatalf("the vector seam embedded internal mechanism wording %q", text)
		}
	}
	found := false
	for _, text := range embedder.recorded() {
		if text == "the material of the landing walk" {
			found = true
		}
	}
	if !found {
		t.Fatal("the business fact was never embedded")
	}
}

// TestTierQueryMatchesObservedRead pins durable queries to the existing semantics.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestTierQueryMatchesObservedRead(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	addrA := event.Address{AgentID: "agent-a", SessionID: "session-query-a"}
	addrB := event.Address{AgentID: "agent-b", SessionID: "session-query-b"}
	commitWindowTurns(t, ctx, run, addrA, 2)
	commitWindowTurns(t, ctx, run, addrB, 1)
	baseline := memory.NewMemoryEventQuery()
	if err := run.tier.Segments.EachEvent(ctx, func(stored ledger.StoredEvent) error {
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return err
		}
		if event.CompressionMechanism(body.Type) {
			return nil
		}
		return baseline.Store(ctx, body.Record(stored.PartitionID, stored.WindowTS))
	}); err != nil {
		t.Fatalf("build the query baseline: %v", err)
	}
	direct := NewTierQuerySource(run.tier.Segments)
	var mechanismKey event.EventID
	if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, typ string, _ event.SessionID) error {
		if mechanismKey == "" && event.CompressionMechanism(typ) {
			mechanismKey = key
		}
		return nil
	}); err != nil {
		t.Fatalf("find a mechanism fact for the refusal check: %v", err)
	}
	if mechanismKey == "" {
		t.Fatal("the query fixture filed no compression mechanism fact")
	}
	if _, held := direct.Event(mechanismKey); held {
		t.Fatal("the durable query served a compression mechanism event")
	}
	if _, held := direct.Reference(mechanismKey); held {
		t.Fatal("the durable query served a compression mechanism reference")
	}
	partitions := []int{memory.PartitionIDFromName(string(addrA.AgentID)), memory.PartitionIDFromName(string(addrB.AgentID))}
	queries := []memory.QueryOptions{
		{PartitionIDs: partitions},
		{PartitionIDs: partitions, EventTypes: []string{event.TypeExternalInput}, Keyword: "material", OrderBy: memory.QueryOrderByDesc},
		{PartitionIDs: partitions, StartTime: 1, EndTime: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC).UnixMilli(), Limit: 2, Offset: 1},
	}
	for i, opts := range queries {
		want, err := baseline.QueryEvents(ctx, opts)
		if err != nil {
			t.Fatalf("baseline query %d: %v", i, err)
		}
		got, err := direct.QueryEvents(ctx, opts)
		if err != nil {
			t.Fatalf("direct query %d: %v", i, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("direct query %d = %#v, want %#v", i, got, want)
		}
	}
	all, err := baseline.QueryEvents(ctx, memory.QueryOptions{PartitionIDs: partitions})
	if err != nil {
		t.Fatalf("list the baseline: %v", err)
	}
	for _, ref := range all {
		gotEvent, gotHeld := direct.Event(ref.Key)
		if !gotHeld || gotEvent != ref {
			t.Fatalf("Event(%s) = %#v/%v, want %#v/true", ref.Key, gotEvent, gotHeld, ref)
		}
		stored, held, err := run.tier.Segments.GetEvent(ctx, ref.Key)
		if err != nil || !held {
			t.Fatalf("read the expected reference of %s: %v/%v", ref.Key, held, err)
		}
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			t.Fatalf("decode the expected reference of %s: %v", ref.Key, err)
		}
		gotRef, gotHeld := direct.Reference(ref.Key)
		if !gotHeld || gotRef != body.Ref() {
			t.Fatalf("Reference(%s) = %#v/%v, want %#v/true", ref.Key, gotRef, gotHeld, body.Ref())
		}
	}
}

// TestRetrievalConstructionReadsNoEventBodies pins lazy query consumption.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestRetrievalConstructionReadsNoEventBodies(t *testing.T) {
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageMemory})
	if err != nil {
		t.Fatalf("open the tier: %v", err)
	}
	defer tier.Segments.Close()
	if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{PartitionID: 1, WindowTS: 1, Seq: 1, EventKey: "undecodable", EventTime: 1, Body: []byte("not-an-event")}.WithChain(event.TypeExternalInput, "session-lazy")); err != nil {
		t.Fatalf("store the undecodable body: %v", err)
	}
	supply, err := supplyRetrieval(ctx, tier, nil)
	if err != nil {
		t.Fatalf("constructing the read face decoded a body: %v", err)
	}
	defer supply.close()
	if _, err := supply.source.QueryEvents(ctx, memory.QueryOptions{PartitionIDs: []int{1}}); err == nil {
		t.Fatal("the first real query did not surface the unreadable body")
	}
}

// TestObservedEventsReadsBeyondHotWindow pins durable history over runtime state.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestObservedEventsReadsBeyondHotWindow(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-durable-query"}
	commitWindowTurns(t, ctx, run, addr, 12)
	first := event.EventID(timeStampKey(time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), addr, 0, 0))
	run.mu.Lock()
	_, held := run.facts[first]
	run.mu.Unlock()
	if held {
		t.Fatal("the fixture kept the first fact in the hot window")
	}
	refs, err := run.ObservedEvents(ctx, memory.QueryOptions{PartitionIDs: []int{memory.PartitionIDFromName(string(addr.AgentID))}})
	if err != nil {
		t.Fatalf("query the durable history: %v", err)
	}
	found := false
	for _, ref := range refs {
		if ref.Key == first {
			found = true
		}
	}
	if !found {
		t.Fatalf("the durable query lost %s after it left the hot window", first)
	}
}

// TestTierParentFollowsCommitOrder pins parent derivation without a relation journal.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestTierParentFollowsCommitOrder(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	addrA := event.Address{AgentID: "agent-a", SessionID: "session-parent-shared"}
	addrB := event.Address{AgentID: "agent-b", SessionID: "session-parent-shared"}
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	commit := func(addr event.Address, key event.EventID, stamp time.Time) {
		t.Helper()
		evt := event.Event{Key: key, Type: event.TypeExternalInput, Timestamp: stamp, Meta: map[string]string{event.MetaKeyEventSummary: "ask"}, Message: model.Message{Role: model.RoleUser, Content: "parent"}, Owner: addr}
		if _, err := run.CommitToSession(ctx, addr, evt); err != nil {
			t.Fatalf("commit %s: %v", key, err)
		}
	}
	if memory.PartitionIDFromName(string(addrA.AgentID)) == memory.PartitionIDFromName(string(addrB.AgentID)) {
		t.Fatal("the two parent-chain agents share one partition")
	}
	commit(addrA, "parent-a1", at.Add(2*time.Hour))
	commit(addrB, "parent-b1", at.Add(time.Hour))
	commit(addrA, "parent-a2", at)
	direct := NewTierQuerySource(run.tier.Segments)
	for _, check := range []struct {
		key    event.EventID
		parent event.EventID
	}{
		{key: "parent-a2", parent: "parent-a1"},
		{key: "parent-a1", parent: ""},
		{key: "parent-b1", parent: ""},
		{key: "parent-missing", parent: ""},
	} {
		parent, err := direct.GetParent(check.key)
		if err != nil {
			t.Fatalf("GetParent(%s): %v", check.key, err)
		}
		if parent != check.parent {
			t.Fatalf("GetParent(%s)=%q, want %q", check.key, parent, check.parent)
		}
	}
	reads := run.RecallReads()
	if reads.Trace == nil {
		t.Fatal("the recall read face has no durable parent reader")
	}
	parent, err := reads.Trace.GetParent("parent-a2")
	if err != nil || parent != "parent-a1" {
		t.Fatalf("the recall parent reader answered %q/%v, want parent-a1", parent, err)
	}
}

// TestRecallPortLateBindsParentReader pins construction before Run binding.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestRecallPortLateBindsParentReader(t *testing.T) {
	ctx := context.Background()
	port := NewRecallPort(nil)
	reads := port.Reads()
	if reads.Trace == nil {
		t.Fatal("the pre-bind recall port exposes no trace wrapper")
	}
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-late-parent"}
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for _, key := range []event.EventID{"late-a1", "late-a2"} {
		evt := event.Event{Key: key, Type: event.TypeExternalInput, Timestamp: at, Meta: map[string]string{event.MetaKeyEventSummary: "ask"}, Message: model.Message{Role: model.RoleUser, Content: "parent"}, Owner: addr}
		if _, err := run.CommitToSession(ctx, addr, evt); err != nil {
			t.Fatalf("commit %s: %v", key, err)
		}
	}
	port.Bind(run)
	parent, err := reads.Trace.GetParent("late-a2")
	if err != nil || parent != "late-a1" {
		t.Fatalf("the late-bound trace answered %q/%v, want late-a1", parent, err)
	}
}

// TestTierQueryHonorsScopeBeforeDecoding pins authorization ahead of body reads.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestTierQueryHonorsScopeBeforeDecoding(t *testing.T) {
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageMemory})
	if err != nil {
		t.Fatalf("open the tier: %v", err)
	}
	defer tier.Segments.Close()
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	good := event.Event{Key: "good-scoped", Type: event.TypeExternalInput, Timestamp: at, Meta: map[string]string{event.MetaKeyEventSummary: "ask"}, Message: model.Message{Role: model.RoleUser, Content: "scoped"}, Owner: event.Address{AgentID: "agent-a", SessionID: "session-scoped"}}
	body, err := memory.BodyFromEvent(good, "test", "test")
	if err != nil {
		t.Fatalf("encode the scoped fact: %v", err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("marshal the scoped fact: %v", err)
	}
	if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{PartitionID: 11, WindowTS: at.UnixMilli(), Seq: 1, EventKey: good.Key, EventTime: at.UnixMilli(), Body: encoded}.WithChain(good.Type, good.Owner.SessionID)); err != nil {
		t.Fatalf("store the scoped fact: %v", err)
	}
	if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{PartitionID: 12, WindowTS: at.UnixMilli(), Seq: 1, EventKey: "corrupt-outside", EventTime: at.UnixMilli(), Body: []byte("not-an-event")}.WithChain(event.TypeExternalInput, "session-outside")); err != nil {
		t.Fatalf("store the corrupt outside fact: %v", err)
	}
	direct := NewTierQuerySource(tier.Segments)
	for _, opts := range []memory.QueryOptions{{}, {PartitionIDs: []int{11}}, {PartitionID: 11}, {PartitionIDs: []int{11}, PartitionID: 12}} {
		refs, err := direct.QueryEvents(ctx, opts)
		if err != nil {
			t.Fatalf("scoped query %+v read an unauthorized body: %v", opts, err)
		}
		want := 1
		if opts.PartitionID == 0 && len(opts.PartitionIDs) == 0 {
			want = 0
		}
		if len(refs) != want {
			t.Fatalf("scoped query %+v returned %d refs, want %d", opts, len(refs), want)
		}
	}
	for _, check := range []struct {
		opts memory.QueryOptions
		want error
	}{
		{opts: memory.QueryOptions{OrderBy: "invalid"}, want: memory.ErrQueryOrder},
		{opts: memory.QueryOptions{Offset: -1}, want: memory.ErrQueryPaging},
	} {
		if _, err := direct.QueryEvents(ctx, check.opts); !errors.Is(err, check.want) {
			t.Fatalf("invalid query %+v returned %v, want %v before any read", check.opts, err, check.want)
		}
	}
}

// TestTierParentSurvivesDurableReopen pins parent derivation across a real restart.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestTierParentSurvivesDurableReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	first := windowChainRun(t, ctx, storage)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-durable-parent"}
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		key event.EventID
		at  time.Time
	}{
		{key: "durable-a1", at: at.Add(2 * time.Hour)},
		{key: "durable-a2", at: at},
	} {
		evt := event.Event{Key: item.key, Type: event.TypeExternalInput, Timestamp: item.at, Meta: map[string]string{event.MetaKeyEventSummary: "ask"}, Message: model.Message{Role: model.RoleUser, Content: "parent"}, Owner: addr}
		if _, err := first.CommitToSession(ctx, addr, evt); err != nil {
			t.Fatalf("commit %s: %v", item.key, err)
		}
	}
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("drain the first run: %v", err)
	}
	second := windowChainRun(t, ctx, storage)
	defer func() {
		if _, err := second.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
			t.Fatalf("drain the restarted run: %v", err)
		}
	}()
	parent, err := NewTierQuerySource(second.tier.Segments).GetParent("durable-a2")
	if err != nil || parent != "durable-a1" {
		t.Fatalf("the durable restart parent = %q/%v, want durable-a1", parent, err)
	}
	if _, err := os.Stat(filepath.Join(root, "relations")); !os.IsNotExist(err) {
		t.Fatalf("the durable restart created a relation journal directory: %v", err)
	}
}
