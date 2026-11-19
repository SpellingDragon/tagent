package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/SpellingDragon/tagent/memory/search"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/tool/recall"

	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// recordingEmbedder records every text the query-time embedder is asked to embed.
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

// TestCandidateEmbeddingNeverSeesMechanismWording pins query-time embedding.
// - supply construction performs no embedding; only a real query embeds candidates
// - candidate text comes from the original body and never from mechanism wording
//
// 契约: docs/wiki/memory/memory-architecture.md#embedder-contract
func TestCandidateEmbeddingNeverSeesMechanismWording(t *testing.T) {
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
			PartitionID: memory.PartitionIDFromName("agent-a"),
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
	deadline := time.Now().Add(50 * time.Millisecond)
	for len(embedder.recorded()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := len(embedder.recorded()); got != 0 {
		t.Fatalf("supply construction embedded %d text(s), want query-time only", got)
	}
	if _, err := supply.searcher.Search(ctx, search.Query{Text: "material", Strategy: search.StrategyHybrid, Partitions: []string{"agent-a"}, Limit: 3}); err != nil {
		t.Fatalf("search through the query-time candidates: %v", err)
	}
	for _, text := range embedder.recorded() {
		if strings.Contains(text, "compress product") || strings.Contains(text, "compress request") {
			t.Fatalf("candidate embedding included internal mechanism wording %q", text)
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
// - four closed turns put a band older than the recent window beside the newest one, which the fold mints a product for
// - a shorter walk leaves every band new, so the fold judges nothing and the fixture files no mechanism fact
// - the checkpoint filer ends before the baseline is read, so the tier the two reads compare stands still
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestTierQueryMatchesObservedRead(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	run.stopCheckpointFiler()
	addrA := event.Address{AgentID: "agent-a", SessionID: "session-query-a"}
	addrB := event.Address{AgentID: "agent-b", SessionID: "session-query-b"}
	commitWindowTurns(t, ctx, run, addrA, 4)
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
		if want == 1 && refs[0].Key != "good-scoped" {
			t.Fatalf("scoped query %+v returned %s, want good-scoped", opts, refs[0].Key)
		}
	}
	if _, err := direct.QueryEvents(ctx, memory.QueryOptions{PartitionIDs: []int{12}}); err == nil {
		t.Fatal("the admitted corrupt partition did not surface its decode error")
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

// TestTierParentFallsBackToBodiesForMissingSession pins the index-empty fallback.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestTierParentFallsBackToBodiesForMissingSession(t *testing.T) {
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageMemory})
	if err != nil {
		t.Fatalf("open the tier: %v", err)
	}
	defer tier.Segments.Close()
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	store := func(addr event.Address, key event.EventID, seq int64, stamp time.Time) {
		t.Helper()
		evt := event.Event{Key: key, Type: event.TypeExternalInput, Timestamp: stamp, Meta: map[string]string{event.MetaKeyEventSummary: "ask"}, Message: model.Message{Role: model.RoleUser, Content: "parent"}, Owner: addr}
		body, err := memory.BodyFromEvent(evt, "test", "test")
		if err != nil {
			t.Fatalf("encode %s: %v", key, err)
		}
		encoded, err := body.Encode()
		if err != nil {
			t.Fatalf("marshal %s: %v", key, err)
		}
		if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{PartitionID: 11, WindowTS: stamp.UnixMilli(), Seq: seq, EventKey: key, EventTime: stamp.UnixMilli(), Body: encoded}); err != nil {
			t.Fatalf("store %s: %v", key, err)
		}
	}
	addrA := event.Address{AgentID: "agent-a", SessionID: "session-index-empty-a"}
	addrB := event.Address{AgentID: "agent-b", SessionID: "session-index-empty-b"}
	store(addrA, "empty-index-a1", 1, at.Add(2*time.Hour))
	store(addrB, "empty-index-b1", 2, at.Add(time.Hour))
	store(addrA, "empty-index-a2", 3, at)
	if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{PartitionID: 12, WindowTS: at.UnixMilli(), Seq: 1, EventKey: "empty-index-corrupt", EventTime: at.UnixMilli(), Body: []byte("not-an-event")}); err != nil {
		t.Fatalf("store the out-of-partition corrupt body: %v", err)
	}
	direct := NewTierQuerySource(tier.Segments)
	for _, check := range []struct {
		key    event.EventID
		parent event.EventID
	}{
		{key: "empty-index-a2", parent: "empty-index-a1"},
		{key: "empty-index-a1", parent: ""},
		{key: "empty-index-b1", parent: ""},
	} {
		parent, err := direct.GetParent(check.key)
		if err != nil {
			t.Fatalf("GetParent(%s) decoded an out-of-partition body: %v", check.key, err)
		}
		if parent != check.parent {
			t.Fatalf("GetParent(%s)=%q, want %q", check.key, parent, check.parent)
		}
	}
}

// TestReadCorruptionNamedError pins the durable read face naming corruption rather than folding it into absence.
//   - a body that cannot be decoded is named by ChainFailure, while a readable or absent key names no failure at all
//   - the declared Event face keeps answering held-or-not, so no consumer of it changes behavior
//   - the recall port forwards the naming to the tools it binds
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestReadCorruptionNamedError(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-corrupt-named"}
	readable := event.Event{
		Key:       "readable-named",
		Type:      event.TypeExternalInput,
		Timestamp: at,
		Meta:      map[string]string{event.MetaKeyEventSummary: "ask"},
		Message:   model.Message{Role: model.RoleUser, Content: "the readable fact"},
		Owner:     addr,
	}
	if _, err := run.CommitToSession(ctx, addr, readable); err != nil {
		t.Fatalf("commit the readable fact: %v", err)
	}
	if err := run.tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{
		PartitionID: memory.PartitionIDFromName("agent-a"),
		WindowTS:    at.UnixMilli(),
		Seq:         7,
		EventKey:    "corrupt-named",
		EventTime:   at.UnixMilli(),
		Body:        []byte("not-an-event"),
	}.WithChain(event.TypeExternalInput, addr.SessionID)); err != nil {
		t.Fatalf("store the corrupt body: %v", err)
	}
	direct := NewTierQuerySource(run.tier.Segments)
	if err := direct.ChainFailure("corrupt-named"); !errors.Is(err, memory.ErrEventBody) {
		t.Fatalf("ChainFailure over the corrupt body = %v, want the decode cause", err)
	}
	if _, held := direct.Event("corrupt-named"); held {
		t.Fatal("the declared face answered a corrupt body as a held event")
	}
	for _, key := range []event.EventID{"readable-named", "absent-named"} {
		if err := direct.ChainFailure(key); err != nil {
			t.Fatalf("ChainFailure(%s) = %v, want no failure beside a readable or absent key", key, err)
		}
	}
	port := NewRecallPort(nil)
	port.Bind(run)
	if err := port.ChainFailure("corrupt-named"); !errors.Is(err, memory.ErrEventBody) {
		t.Fatalf("the recall port answered %v over the corrupt body, want the decode cause forwarded", err)
	}
	if err := port.ChainFailure("absent-named"); err != nil {
		t.Fatalf("the recall port answered %v over an absent key, want no failure", err)
	}
}

// commitParentFact commits one fact of a parent-lookup fixture into one
// partition, stamped by that partition's own running sequence so the chain's
// order is the commit order and a chain can stand among facts of other chains.
func commitParentFact(t *testing.T, ctx context.Context, tier *ledger.Tier, seq map[int]int64, partition int, agent event.AgentID, session event.SessionID, key event.EventID) {
	t.Helper()
	seq[partition]++
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	stamp := at.Add(time.Duration(seq[partition]) * time.Millisecond)
	evt := event.Event{
		Key:       key,
		Type:      event.TypeExternalInput,
		Timestamp: stamp,
		Meta:      map[string]string{event.MetaKeyEventSummary: "ask"},
		Message:   model.Message{Role: model.RoleUser, Content: "parent"},
		Owner:     event.Address{AgentID: agent, SessionID: session},
	}
	body, err := memory.BodyFromEvent(evt, "test", "test")
	if err != nil {
		t.Fatalf("encode %s: %v", key, err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("marshal %s: %v", key, err)
	}
	record := ledger.SegmentRecord{PartitionID: partition, WindowTS: stamp.UnixMilli(), Seq: seq[partition], EventKey: key, EventTime: stamp.UnixMilli(), Body: encoded}
	if err := tier.Segments.StoreEvent(ctx, record.WithChain(event.TypeExternalInput, session)); err != nil {
		t.Fatalf("store %s: %v", key, err)
	}
}

// parentLookupTier opens a journal store holding one named background history
// beside a six-fact chain, committed so the chain's entries stand among the
// history's own tail and a lookup steps over entries it does not answer.
func parentLookupTier(t *testing.T, ctx context.Context, history int) *ledger.Tier {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir(), WriteRights: true})
	if err != nil {
		t.Fatalf("open the journal store: %v", err)
	}
	seq := map[int]int64{}
	for i := 1; i <= history; i++ {
		commitParentFact(t, ctx, tier, seq, 10, "agent-history", "session-history", event.EventID(fmt.Sprintf("history-%d", i)))
	}
	for i := 1; i <= 6; i++ {
		commitParentFact(t, ctx, tier, seq, 11, "agent-chain", "session-chain", event.EventID(fmt.Sprintf("chain-%d", i)))
		commitParentFact(t, ctx, tier, seq, 10, "agent-history", "session-history", event.EventID(fmt.Sprintf("lag-%d", i)))
	}
	return tier
}

// parentOldTargetTier opens a journal store holding a three-fact chain whose
// target stands one fact from its own predecessor, beside the newer history the
// lookup from the newest record must step over before it reaches that target.
func parentOldTargetTier(t *testing.T, ctx context.Context, after int) *ledger.Tier {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir(), WriteRights: true})
	if err != nil {
		t.Fatalf("open the journal store: %v", err)
	}
	seq := map[int]int64{}
	for _, key := range []event.EventID{"chain-root", "chain-mid", "chain-target"} {
		commitParentFact(t, ctx, tier, seq, 11, "agent-chain", "session-chain", key)
	}
	for i := 1; i <= after; i++ {
		commitParentFact(t, ctx, tier, seq, 10, "agent-history", "session-history", event.EventID(fmt.Sprintf("later-%d", i)))
	}
	return tier
}

// TestParentLookupCostsItsOwnChainNotTheHistory pins what one parent lookup pays.
//   - the same chain shape stands past 400, 1600 and 6400 background facts, so a lookup that grew with the history answers four times the smallest at the largest
//   - the chain's root answers no parent, and that lookup names every background entry: the fixture states a lookup the bound cannot answer
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestParentLookupCostsItsOwnChainNotTheHistory(t *testing.T) {
	ctx := context.Background()
	depths := []int{400, 1600, 6400}
	bounded := make([]int, 0, len(depths))
	controls := make([]int, 0, len(depths))
	for _, history := range depths {
		tier := parentLookupTier(t, ctx, history)
		direct := NewTierQuerySource(tier.Segments)
		before, _, _ := tier.Segments.IndexWalkReads()
		parent, err := direct.GetParent("chain-6")
		if err != nil || parent != "chain-5" {
			t.Fatalf("the parent of chain-6 = %q/%v, want chain-5 beside %d background facts", parent, err, history)
		}
		after, _, _ := tier.Segments.IndexWalkReads()
		bounded = append(bounded, after-before)
		before, _, _ = tier.Segments.IndexWalkReads()
		root, err := direct.GetParent("chain-1")
		if err != nil || root != "" {
			t.Fatalf("the root lookup = %q/%v, want no parent beside %d background facts", root, err, history)
		}
		after, _, _ = tier.Segments.IndexWalkReads()
		controls = append(controls, after-before)
		t.Logf("background=%d parent-lookup-named=%d root-lookup-named=%d", history, bounded[len(bounded)-1], controls[len(controls)-1])
		if err := tier.Segments.Close(); err != nil {
			t.Fatalf("close the store of %d background facts: %v", history, err)
		}
	}
	if bounded[len(bounded)-1] > 4*bounded[0] {
		t.Fatalf("the newest lookup named %d index entries beside %d background facts and %d beside %d: the chain costs the history",
			bounded[len(bounded)-1], depths[len(depths)-1], bounded[0], depths[0])
	}
	if controls[len(controls)-1] < 4*controls[0] {
		t.Fatalf("the root lookup named %d index entries beside %d background facts and %d beside %d: the fixture shows no lookup that walks the history",
			controls[len(controls)-1], depths[len(depths)-1], controls[0], depths[0])
	}
	for at, history := range depths {
		if bounded[at] > 8 {
			t.Fatalf("the newest lookup named %d index entries beside %d background facts, want the chain's own entries", bounded[at], history)
		}
	}
}

// TestParentLookupCountsTheSearchBeforeTheTarget pins both segments one parent lookup pays.
//   - the target stands one fact from its own predecessor at every depth, so only the history committed after it changes
//   - the lookup names one more index entry per fact of that newer history, leaving the chain's own two counted entries
//   - a measurement counting only the target-to-predecessor segment would answer the same entries at every depth
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestParentLookupCountsTheSearchBeforeTheTarget(t *testing.T) {
	ctx := context.Background()
	depths := []int{16, 64, 256}
	named := make([]int, 0, len(depths))
	newest := make([]int, 0, len(depths))
	for _, after := range depths {
		tier := parentOldTargetTier(t, ctx, after)
		direct := NewTierQuerySource(tier.Segments)
		before, _, _ := tier.Segments.IndexWalkReads()
		parent, err := direct.GetParent("chain-target")
		if err != nil || parent != "chain-mid" {
			t.Fatalf("the parent of chain-target = %q/%v, want chain-mid beside %d facts newer than it", parent, err, after)
		}
		counted, _, _ := tier.Segments.IndexWalkReads()
		named = append(named, counted-before)
		before, _, _ = tier.Segments.IndexWalkReads()
		if parent, err := direct.GetParent(event.EventID(fmt.Sprintf("later-%d", after))); err != nil || parent != event.EventID(fmt.Sprintf("later-%d", after-1)) {
			t.Fatalf("the parent of the newest fact = %q/%v, want the fact beside it", parent, err)
		}
		counted, _, _ = tier.Segments.IndexWalkReads()
		newest = append(newest, counted-before)
		t.Logf("newer-history=%d old-target-named=%d newest-named=%d", after, named[len(named)-1], newest[len(newest)-1])
		if err := tier.Segments.Close(); err != nil {
			t.Fatalf("close the store of %d newer facts: %v", after, err)
		}
	}
	for at, after := range depths {
		if named[at] != after+2 || newest[at] != 2 {
			t.Fatalf("beside %d facts newer than the target the lookup named %d index entries and the newest lookup named %d, want %d and 2: the entries of the search before the target are not counted",
				after, named[at], newest[at], after+2)
		}
	}
	if named[len(named)-1]-named[0] != depths[len(depths)-1]-depths[0] {
		t.Fatalf("the lookup named %d entries beside %d newer facts and %d beside %d: the counted cost does not follow the history committed after the target, so the first segment of the walk went unmeasured",
			named[len(named)-1], depths[len(depths)-1], named[0], depths[0])
	}
}

// TestParentLookupKeepsSessionIsolation pins what a lookup answers while it steps over other chains.
//   - the target's nearest older entries stand in another session of its partition and in another partition of its session, and neither is answered
//   - the walk names those skipped entries, so the cost of the isolation is counted beside the answer it keeps
//   - an index entry carrying no session falls back to the bodies, which separate the chains as the index does
//   - negative: a body the fallback cannot decode is named as corruption, never answered as a parent
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestParentLookupKeepsSessionIsolation(t *testing.T) {
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir(), WriteRights: true})
	if err != nil {
		t.Fatalf("open the journal store: %v", err)
	}
	defer func() {
		if err := tier.Segments.Close(); err != nil {
			t.Fatalf("close the store: %v", err)
		}
	}()
	seq := map[int]int64{}
	for _, fact := range []struct {
		partition int
		agent     event.AgentID
		session   event.SessionID
		key       event.EventID
	}{
		{partition: 11, agent: "agent-chain", session: "session-iso-a", key: "iso-a1"},
		{partition: 11, agent: "agent-chain", session: "session-iso-b", key: "iso-b1"},
		{partition: 10, agent: "agent-other", session: "session-iso-a", key: "iso-p1"},
		{partition: 11, agent: "agent-chain", session: "session-iso-b", key: "iso-b2"},
		{partition: 11, agent: "agent-chain", session: "session-iso-a", key: "iso-a2"},
	} {
		commitParentFact(t, ctx, tier, seq, fact.partition, fact.agent, fact.session, fact.key)
	}
	direct := NewTierQuerySource(tier.Segments)
	before, _, _ := tier.Segments.IndexWalkReads()
	parent, err := direct.GetParent("iso-a2")
	if err != nil {
		t.Fatalf("the lookup over foreign chains answered %v", err)
	}
	counted, _, _ := tier.Segments.IndexWalkReads()
	named := counted - before
	t.Logf("isolated-lookup-named=%d answer=%s", named, parent)
	if parent != "iso-a1" {
		t.Fatalf("the lookup joined %q to iso-a2: a foreign session of its own partition or a foreign partition of its own session is never the parent", parent)
	}
	if named != 5 {
		t.Fatalf("the isolated lookup named %d index entries, want the target, the three entries of other chains it stepped over and its parent", named)
	}
	if parent, err := direct.GetParent("iso-p1"); err != nil || parent != "" {
		t.Fatalf("the lookup of a key whose chain stands in another partition answered %q/%v, want no parent", parent, err)
	}
	for _, fact := range []struct {
		session event.SessionID
		key     event.EventID
	}{
		{session: "session-bare-a", key: "bare-a1"},
		{session: "session-bare-b", key: "bare-b1"},
		{session: "session-bare-a", key: "bare-a2"},
	} {
		at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
		seq[12]++
		stamp := at.Add(time.Duration(seq[12]) * time.Millisecond)
		evt := event.Event{Key: fact.key, Type: event.TypeExternalInput, Timestamp: stamp, Meta: map[string]string{event.MetaKeyEventSummary: "ask"}, Message: model.Message{Role: model.RoleUser, Content: "parent"}, Owner: event.Address{AgentID: "agent-bare", SessionID: fact.session}}
		body, err := memory.BodyFromEvent(evt, "test", "test")
		if err != nil {
			t.Fatalf("encode %s: %v", fact.key, err)
		}
		encoded, err := body.Encode()
		if err != nil {
			t.Fatalf("marshal %s: %v", fact.key, err)
		}
		if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{PartitionID: 12, WindowTS: stamp.UnixMilli(), Seq: seq[12], EventKey: fact.key, EventTime: stamp.UnixMilli(), Body: encoded}); err != nil {
			t.Fatalf("store %s without an index session: %v", fact.key, err)
		}
	}
	before, _, _ = tier.Segments.IndexWalkReads()
	if parent, err := direct.GetParent("bare-a2"); err != nil || parent != "bare-a1" {
		t.Fatalf("the body walk over an index carrying no session answered %q/%v, want bare-a1", parent, err)
	}
	counted, _, _ = tier.Segments.IndexWalkReads()
	t.Logf("fallback-lookup-named=%d", counted-before)
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	seq[12]++
	stamp := at.Add(time.Duration(seq[12]) * time.Millisecond)
	if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{PartitionID: 12, WindowTS: stamp.UnixMilli(), Seq: seq[12], EventKey: "bare-corrupt", EventTime: stamp.UnixMilli(), Body: []byte("not-an-event")}); err != nil {
		t.Fatalf("store the undecodable body: %v", err)
	}
	if parent, err := direct.GetParent("bare-corrupt"); !errors.Is(err, memory.ErrEventBody) || parent != "" {
		t.Fatalf("the body walk over an undecodable body answered %q/%v, want the decode cause named and no parent", parent, err)
	}
}

// TestAPointQueryThatWalksTheWholeHistoryIsRed pins the bound the parent lookup rests on.
//   - the entries the newest lookup names are the chain's, and a lookup naming the history instead answers a count the chain cannot explain
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestAPointQueryThatWalksTheWholeHistoryIsRed(t *testing.T) {
	ctx := context.Background()
	const history = 512
	tier := parentLookupTier(t, ctx, history)
	defer func() {
		if err := tier.Segments.Close(); err != nil {
			t.Fatalf("close the store: %v", err)
		}
	}()
	direct := NewTierQuerySource(tier.Segments)
	before, _, _ := tier.Segments.IndexWalkReads()
	if _, err := direct.GetParent("chain-6"); err != nil {
		t.Fatalf("the newest lookup answered %v", err)
	}
	after, _, _ := tier.Segments.IndexWalkReads()
	named := after - before
	t.Logf("history=%d newest-lookup-named=%d", history, named)
	if named > 8 {
		t.Fatalf("the newest lookup named %d index entries beside a history of %d entries: the walk reads the history behind the chain and not the chain alone",
			named, history)
	}
}

// TestRecallTraceThroughTierCountsOldTargetLookup pins what the causal walk pays on the shipped surface.
//   - the granted partition holds a three-fact chain committed behind a history its partition never named
//   - the engine names that newer history once per step, while the newest chain of the same store names its own two
//   - negative: a parent reader answering a constant would leave the walk paying nothing
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestRecallTraceThroughTierCountsOldTargetLookup(t *testing.T) {
	ctx := context.Background()
	const history = 128
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	run := windowChainRun(t, ctx, storage)
	run.stopCheckpointFiler()
	defer endRun(t, run)
	seq := map[int]int64{}
	chain := memory.PartitionIDFromName("agent-a")
	for _, key := range []event.EventID{"old-root", "old-mid", "old-target"} {
		commitParentFact(t, ctx, run.tier, seq, chain, "agent-a", "session-old", key)
	}
	for i := 1; i <= history; i++ {
		commitParentFact(t, ctx, run.tier, seq, memory.PartitionIDFromName("agent-history"), "agent-history", "session-history", event.EventID(fmt.Sprintf("later-%d", i)))
	}
	for _, key := range []event.EventID{"new-older", "new-newest"} {
		commitParentFact(t, ctx, run.tier, seq, chain, "agent-a", "session-new", key)
	}
	port := NewRecallPort(nil)
	port.Bind(run)
	reads := port.Reads()
	reads.Table = port
	reads.Originals = port
	var trace trpctool.CallableTool
	for _, tool := range recall.New(reads, memory.PartitionsOf("agent-a", nil)) {
		if tool.Declaration().Name == recall.TraceToolName {
			trace = tool
		}
	}
	if trace == nil {
		t.Fatal("the recall surface contributed no causal chain tool")
	}
	before, _, _ := run.tier.Segments.IndexWalkReads()
	answered, err := trace.Call(ctx, []byte(`{"key":"old-target","max_steps":3}`))
	if err != nil {
		t.Fatalf("the causal walk over the old target failed: %v", err)
	}
	counted, _, _ := run.tier.Segments.IndexWalkReads()
	oldNamed := counted - before
	text, encoded := answered.(string)
	if !encoded {
		t.Fatalf("the causal walk answered %T, want the encoded answer the tool declares", answered)
	}
	var walk struct {
		Count    int  `json:"count"`
		Complete bool `json:"complete"`
		Items    []struct {
			Key event.EventID `json:"key"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(text), &walk); err != nil {
		t.Fatalf("decode the causal walk answer: %v", err)
	}
	t.Logf("newer-history=%d old-target-trace-named=%d answer=%s", history, oldNamed, text)
	if !walk.Complete || walk.Count != 3 {
		t.Fatalf("the causal walk answered count=%d complete=%v over three facts, want the whole chain", walk.Count, walk.Complete)
	}
	wantKeys := []event.EventID{"old-target", "old-mid", "old-root"}
	for at, item := range walk.Items {
		if item.Key != wantKeys[at] {
			t.Fatalf("the causal walk answered %v, want %v: the walk follows the chain and not the history around it", walk.Items, wantKeys)
		}
	}
	if wantNamed := 3*(history+2) + 8; oldNamed != wantNamed {
		t.Fatalf("the old target's walk of three steps named %d index entries beside %d facts newer than it, want %d: each step pays the search from the newest record beside its own chain entries, two of them on the first step and three on the two behind it",
			oldNamed, history, wantNamed)
	}
	before, _, _ = run.tier.Segments.IndexWalkReads()
	if _, err := trace.Call(ctx, []byte(`{"key":"new-newest","max_steps":1}`)); err != nil {
		t.Fatalf("the causal walk over the newest fact failed: %v", err)
	}
	counted, _, _ = run.tier.Segments.IndexWalkReads()
	newNamed := counted - before
	t.Logf("newest-trace-named=%d", newNamed)
	if newNamed != 2 {
		t.Fatalf("the newest fact's walk named %d index entries beside the same history, want its own two: the counted cost of the old target is attributable to the search before it and not to a constant the store adds to every walk", newNamed)
	}
}
