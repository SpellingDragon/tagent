package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/search"
)

// TestRecallConsumer drives the recall query consumer face end to end over the QueryEvents read face:
// - normal: the time, type and keyword legs filter on the millisecond axis and results keep the total order
// - normal: the retrieval query maps onto QueryEvents, hybrid fuses both legs and an unscoped query scans nothing
// - failure: hybrid without a vector leg answers byte-identical to keyword_only and the vector-only strategy is refused by name
// - failure: one event key keeps its higher-window version
// - restart: the read face replays an identical sequence on a fresh instance over the same records
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestRecallConsumer(t *testing.T) {
	ctx := context.Background()

	t.Run("normal", func(t *testing.T) {
		t.Run("the read face filters by time window, event type and keyword", func(t *testing.T) {
			source := seededQuerySource(t, ctx,
				memory.EventRecord{Key: "e1", PartitionID: 1, Type: event.TypeExternalInput, Summary: "数据库迁移讨论", Content: "第一步", Timestamp: 1000, WindowTS: 100},
				memory.EventRecord{Key: "e2", PartitionID: 1, Type: event.TypeAgentOutput, Summary: "微信登录", Content: "第二步", Timestamp: 2000, WindowTS: 100},
				memory.EventRecord{Key: "e3", PartitionID: 1, Type: event.TypeExternalInput, Summary: "hello", Content: "DATABASE migration", Timestamp: 3000, WindowTS: 100},
				memory.EventRecord{Key: "e4", PartitionID: 2, Type: event.TypeExternalInput, Summary: "数据库迁移讨论", Content: "别处", Timestamp: 1500, WindowTS: 100},
				memory.EventRecord{Key: "e5", PartitionID: 1, Type: event.TypeThinkingPlan, Summary: "计划", Content: "围绕数据库", Timestamp: 2500, WindowTS: 100},
			)
			refs, err := source.QueryEvents(ctx, memory.QueryOptions{
				PartitionIDs: []int{1},
				StartTime:    1000,
				EndTime:      2500,
				EventTypes:   []string{event.TypeExternalInput, event.TypeThinkingPlan},
				Keyword:      "数据库",
				OrderBy:      memory.QueryOrderByAsc,
			})
			if err != nil {
				t.Fatalf("QueryEvents: %v", err)
			}
			if got := keysOf(refs); !reflect.DeepEqual(got, []event.EventID{"e1", "e5"}) {
				t.Fatalf("filtered keys = %v, want [e1 e5]: window bounds are inclusive, the type and keyword legs narrow, and partition 2 is out of scope", got)
			}

			folded, err := source.QueryEvents(ctx, memory.QueryOptions{
				PartitionIDs: []int{1},
				Keyword:      "database",
				OrderBy:      memory.QueryOrderByAsc,
			})
			if err != nil {
				t.Fatalf("QueryEvents(keyword folded case): %v", err)
			}
			if got := keysOf(folded); !reflect.DeepEqual(got, []event.EventID{"e3"}) {
				t.Fatalf("case-folded keyword keys = %v, want [e3]: keyword matching is case-insensitive over summary and content", got)
			}
		})

		t.Run("results carry the deterministic total order (timestamp, key)", func(t *testing.T) {
			source := seededQuerySource(t, ctx,
				memory.EventRecord{Key: "0a", PartitionID: 1, Type: event.TypeActionCommand, Summary: "a", Timestamp: 5000, WindowTS: 100},
				memory.EventRecord{Key: "0b", PartitionID: 1, Type: event.TypeActionCommand, Summary: "b", Timestamp: 5000, WindowTS: 100},
				memory.EventRecord{Key: "0c", PartitionID: 1, Type: event.TypeActionCommand, Summary: "c", Timestamp: 4000, WindowTS: 100},
			)
			desc, err := source.QueryEvents(ctx, memory.QueryOptions{PartitionIDs: []int{1}, OrderBy: memory.QueryOrderByDesc})
			if err != nil {
				t.Fatalf("QueryEvents(desc): %v", err)
			}
			if got := keysOf(desc); !reflect.DeepEqual(got, []event.EventID{"0b", "0a", "0c"}) {
				t.Fatalf("descending keys = %v, want [0b 0a 0c]: same-millisecond events are decided by event key and the order reverses both keys together", got)
			}
			asc, err := source.QueryEvents(ctx, memory.QueryOptions{PartitionIDs: []int{1}, OrderBy: memory.QueryOrderByAsc})
			if err != nil {
				t.Fatalf("QueryEvents(asc): %v", err)
			}
			if got := keysOf(asc); !reflect.DeepEqual(got, []event.EventID{"0c", "0a", "0b"}) {
				t.Fatalf("ascending keys = %v, want [0c 0a 0b]", got)
			}
			again, err := source.QueryEvents(ctx, memory.QueryOptions{PartitionIDs: []int{1}, OrderBy: memory.QueryOrderByDesc})
			if err != nil {
				t.Fatalf("QueryEvents(desc again): %v", err)
			}
			if !reflect.DeepEqual(desc, again) {
				t.Fatalf("two identical queries differ: %v vs %v, want position-by-position identity", desc, again)
			}
			page, err := source.QueryEvents(ctx, memory.QueryOptions{PartitionIDs: []int{1}, OrderBy: memory.QueryOrderByDesc, Offset: 1, Limit: 1})
			if err != nil {
				t.Fatalf("QueryEvents(page): %v", err)
			}
			if got := keysOf(page); !reflect.DeepEqual(got, []event.EventID{"0a"}) {
				t.Fatalf("offset 1 limit 1 keys = %v, want [0a]: the page is cut from the ordered result", got)
			}
		})

		t.Run("the retrieval query maps its time and type axes onto QueryEvents", func(t *testing.T) {
			since := time.UnixMilli(1000)
			until := time.UnixMilli(2500)
			query := search.Query{
				Text:   "数据库",
				Types:  []string{event.TypeExternalInput},
				Since:  since,
				Until:  until,
				Limit:  7,
				Cursor: "",
			}
			mapped := query.QueryOptions()
			if mapped.StartTime != 1000 || mapped.EndTime != 2500 {
				t.Fatalf("mapped window = [%d, %d], want the Unix milliseconds of Since/Until [1000, 2500]", mapped.StartTime, mapped.EndTime)
			}
			if !reflect.DeepEqual(mapped.EventTypes, []string{event.TypeExternalInput}) {
				t.Fatalf("mapped EventTypes = %v, want the Types carried unchanged", mapped.EventTypes)
			}
			if mapped.Keyword != "数据库" {
				t.Fatalf("mapped Keyword = %q, want the retrieval text carried as the keyword leg", mapped.Keyword)
			}
			if mapped.Limit != 7 || mapped.OrderBy != memory.QueryOrderByDesc {
				t.Fatalf("mapped page = limit %d order %q, want 7 / %q", mapped.Limit, mapped.OrderBy, memory.QueryOrderByDesc)
			}
			if mapped.PartitionID != 0 || len(mapped.PartitionIDs) != 0 {
				t.Fatalf("mapped partitions = %v/%v, want none: the read seam injects the authorized scope, the mapping never widens it", mapped.PartitionID, mapped.PartitionIDs)
			}

			zero := (search.Query{Text: "x"}).QueryOptions()
			if zero.StartTime != 0 || zero.EndTime != 0 {
				t.Fatalf("mapped zero bounds = [%d, %d], want open bounds [0, 0]", zero.StartTime, zero.EndTime)
			}

			source := seededQuerySource(t, ctx,
				memory.EventRecord{Key: "m1", PartitionID: 1, Type: event.TypeExternalInput, Summary: "数据库迁移", Timestamp: 1000, WindowTS: 100},
				memory.EventRecord{Key: "m2", PartitionID: 1, Type: event.TypeExternalInput, Summary: "数据库迁移二", Timestamp: 2500, WindowTS: 100},
				memory.EventRecord{Key: "m3", PartitionID: 1, Type: event.TypeExternalInput, Summary: "数据库迁移三", Timestamp: 2501, WindowTS: 100},
			)
			mapped.PartitionIDs = []int{1}
			refs, err := source.QueryEvents(ctx, mapped)
			if err != nil {
				t.Fatalf("QueryEvents(mapped): %v", err)
			}
			if got := keysOf(refs); !reflect.DeepEqual(got, []event.EventID{"m2", "m1"}) {
				t.Fatalf("mapped query keys = %v, want [m2 m1]: one time axis — the mapped millisecond window answers exactly the events the time.Time window names", got)
			}
		})

		t.Run("hybrid fusion merges both legs by reciprocal rank", func(t *testing.T) {
			refA := ledger.OriginalRef{Key: "0a", Owner: event.Address{AgentID: "agent", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"}
			refB := ledger.OriginalRef{Key: "0b", Owner: event.Address{AgentID: "agent", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"}
			refC := ledger.OriginalRef{Key: "0c", Owner: event.Address{AgentID: "agent", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"}
			fused, err := search.NewRRFFusion().Fuse(ctx, search.StrategyHybrid, []ledger.OriginalRef{refA, refB}, []ledger.OriginalRef{refB, refC})
			if err != nil {
				t.Fatalf("Fuse(hybrid): %v", err)
			}
			if !reflect.DeepEqual(fused.Refs, []ledger.OriginalRef{refB, refA, refC}) {
				t.Fatalf("fused order = %v, want [B A C] by reciprocal rank k=%d", fused.Refs, search.RRFRankConstant)
			}
			if fused.NextCursor != "" {
				t.Fatalf("NextCursor = %q on a fused set, want zero: a fused set is complete and hands out no continuation", fused.NextCursor)
			}

			tieA := ledger.OriginalRef{Key: "0a", Owner: event.Address{AgentID: "agent", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"}
			tieB := ledger.OriginalRef{Key: "0b", Owner: event.Address{AgentID: "agent", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"}
			tied, err := search.NewRRFFusion().Fuse(ctx, search.StrategyHybrid, []ledger.OriginalRef{tieA}, []ledger.OriginalRef{tieB})
			if err != nil {
				t.Fatalf("Fuse(hybrid tie): %v", err)
			}
			if !reflect.DeepEqual(tied.Refs, []ledger.OriginalRef{tieB, tieA}) {
				t.Fatalf("tie order = %v, want the larger event key first [B A]", tied.Refs)
			}
		})

		t.Run("an unscoped query scans nothing", func(t *testing.T) {
			source := seededQuerySource(t, ctx,
				memory.EventRecord{Key: "s1", PartitionID: 1, Type: event.TypeExternalInput, Summary: "数据库", Timestamp: 1000, WindowTS: 100},
			)
			refs, err := source.QueryEvents(ctx, memory.QueryOptions{Keyword: "数据库"})
			if err != nil {
				t.Fatalf("QueryEvents(no partitions): %v", err)
			}
			if len(refs) != 0 {
				t.Fatalf("unscoped query returned %d refs %v, want none: the partition scope is default-nothing and never a blind scan", len(refs), refs)
			}
			scoped, err := source.QueryEvents(ctx, memory.QueryOptions{PartitionID: 1, Keyword: "数据库"})
			if err != nil {
				t.Fatalf("QueryEvents(PartitionID): %v", err)
			}
			if got := keysOf(scoped); !reflect.DeepEqual(got, []event.EventID{"s1"}) {
				t.Fatalf("scoped keys = %v, want [s1]: the injected partition is the only one scanned", got)
			}
		})
	})

	t.Run("failure", func(t *testing.T) {
		t.Run("hybrid without a vector leg answers byte-identical to keyword_only", func(t *testing.T) {
			keyword := []ledger.OriginalRef{
				{Key: "0a", Owner: event.Address{AgentID: "agent", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"},
				{Key: "0b", Owner: event.Address{AgentID: "agent", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"},
			}
			fusion := search.NewRRFFusion()
			degraded, err := fusion.Fuse(ctx, search.StrategyHybrid, nil, keyword)
			if err != nil {
				t.Fatalf("Fuse(hybrid, empty vector leg): %v", err)
			}
			plain, err := fusion.Fuse(ctx, search.StrategyKeywordOnly, nil, keyword)
			if err != nil {
				t.Fatalf("Fuse(keyword_only): %v", err)
			}
			degradedBytes, err := json.Marshal(degraded)
			if err != nil {
				t.Fatalf("marshal degraded: %v", err)
			}
			plainBytes, err := json.Marshal(plain)
			if err != nil {
				t.Fatalf("marshal keyword_only: %v", err)
			}
			if string(degradedBytes) != string(plainBytes) {
				t.Fatalf("hybrid fallback = %s, keyword_only = %s, want byte-identical results", degradedBytes, plainBytes)
			}
		})

		t.Run("the vector-only strategy is refused by name", func(t *testing.T) {
			_, err := search.NewRRFFusion().Fuse(ctx, search.StrategyVectorOnly, nil, nil)
			if !errors.Is(err, search.ErrStrategyUnwired) {
				t.Fatalf("Fuse(vector_only) = %v, want the named refusal ErrStrategyUnwired: the strategy has no consumer and is not implemented", err)
			}
			_, err = search.NewRRFFusion().Fuse(ctx, search.Strategy("cosine"), nil, nil)
			if !errors.Is(err, search.ErrStrategyUnknown) {
				t.Fatalf("Fuse(unknown) = %v, want the named refusal ErrStrategyUnknown", err)
			}
		})

		t.Run("one event key keeps its higher-window version", func(t *testing.T) {
			source := memory.NewMemoryEventQuery()
			for _, rec := range []memory.EventRecord{
				{Key: "w1", PartitionID: 1, Type: event.TypeContextCompress, Summary: "源层版本", Timestamp: 1000, WindowTS: 100},
				{Key: "w1", PartitionID: 1, Type: event.TypeContextCompress, Summary: "目标层版本", Timestamp: 1000, WindowTS: 200},
				{Key: "w1", PartitionID: 1, Type: event.TypeContextCompress, Summary: "回放重复", Timestamp: 1000, WindowTS: 200},
				{Key: "w1", PartitionID: 1, Type: event.TypeContextCompress, Summary: "迟到低层", Timestamp: 1000, WindowTS: 150},
			} {
				if err := source.Store(ctx, rec); err != nil {
					t.Fatalf("Store(%s window %d): %v", rec.Key, rec.WindowTS, err)
				}
			}
			refs, err := source.QueryEvents(ctx, memory.QueryOptions{PartitionIDs: []int{1}})
			if err != nil {
				t.Fatalf("QueryEvents: %v", err)
			}
			if len(refs) != 1 {
				t.Fatalf("query returned %d refs %v, want exactly 1: one event key is answered once", len(refs), refs)
			}
			if refs[0].Summary != "目标层版本" {
				t.Fatalf("kept version summary = %q, want %q: the higher window is the canonical compaction layer", refs[0].Summary, "目标层版本")
			}
		})
	})

	t.Run("restart", func(t *testing.T) {
		t.Run("the read face replays identically on a fresh instance", func(t *testing.T) {
			records := []memory.EventRecord{
				{Key: "0a", PartitionID: 1, Type: event.TypeExternalInput, Summary: "数据库", Timestamp: 1000, WindowTS: 100},
				{Key: "0b", PartitionID: 1, Type: event.TypeAgentOutput, Summary: "微信", Timestamp: 1000, WindowTS: 100},
				{Key: "0c", PartitionID: 1, Type: event.TypeExternalInput, Summary: "数据库二", Timestamp: 2000, WindowTS: 100},
			}
			first := seededQuerySource(t, ctx, records...)
			reversed := make([]memory.EventRecord, 0, len(records))
			for i := len(records) - 1; i >= 0; i-- {
				reversed = append(reversed, records[i])
			}
			second := seededQuerySource(t, ctx, reversed...)
			opts := memory.QueryOptions{PartitionIDs: []int{1}, OrderBy: memory.QueryOrderByDesc}
			fromFirst, err := first.QueryEvents(ctx, opts)
			if err != nil {
				t.Fatalf("QueryEvents(first instance): %v", err)
			}
			fromSecond, err := second.QueryEvents(ctx, opts)
			if err != nil {
				t.Fatalf("QueryEvents(second instance): %v", err)
			}
			if !reflect.DeepEqual(fromFirst, fromSecond) {
				t.Fatalf("fresh instance answers %v, want %v: the read face is deterministic across process instances", fromSecond, fromFirst)
			}
		})
	})
}

// seededQuerySource returns a query source holding the given records.
func seededQuerySource(t *testing.T, ctx context.Context, records ...memory.EventRecord) *memory.MemoryEventQuery {
	t.Helper()
	source := memory.NewMemoryEventQuery()
	for _, rec := range records {
		if err := source.Store(ctx, rec); err != nil {
			t.Fatalf("Store(%s): %v", rec.Key, err)
		}
	}
	return source
}

// keysOf returns the event keys of the references in order.
func keysOf(refs []memory.EventReference) []event.EventID {
	keys := make([]event.EventID, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, ref.Key)
	}
	return keys
}
