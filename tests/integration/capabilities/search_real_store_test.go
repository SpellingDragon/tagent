package capabilities

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory"
	memoryembedder "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/search"
)

// realStoreEvent is one corpus event: the fact its body derives from and the
// original text its reference names.
type realStoreEvent struct {
	key       event.EventID
	partition string
	kind      string
	summary   string
	content   string
	original  string
	at        int64
	seq       int64
}

// realStoreChain is the real store retrieval chain one case drives: the file
// tier its events are committed into, the observation table its query face
// reads, and the searcher and scoped reader standing on them.
type realStoreChain struct {
	tier     *ledger.Tier
	scoped   ledger.ScopedOriginalStore
	store    *ledger.FileSegmentStore
	source   *memory.StoreQuerySource
	seam     *memory.VectorSeam
	searcher *search.StoreSearcher
	events   []realStoreEvent
}

const (
	realAlphaPartition = "tenant-alpha"
	realBetaPartition  = "tenant-beta"
	realEventFormat    = "text/plain"
	realEventRetention = "keep"
	realRecallText     = "迁移阶段回填索引顺序"
	realFusionText     = "回填演练"
	realAwaitLimit     = 5 * time.Second
)

// realOwnerOf is the address every corpus reference is registered under.
func realOwnerOf() event.Address {
	return event.Address{AgentID: "agent-recall", SessionID: "session-real"}
}

// realCorpusOf is the five-event corpus: four events under the authorized
// partition and one under another partition, with the retrieval text appearing
// in no summary and no content so no keyword leg can reach the context.
func realCorpusOf() []realStoreEvent {
	return []realStoreEvent{
		{key: "0a01", partition: realAlphaPartition, kind: event.TypeExternalInput, summary: "迁移计划上文", content: "迁移阶段回填索引和顺序定稿", original: "【上文】数据库迁移进入第二阶段，迁移阶段回填索引和顺序定稿，双写校验随后展开。\n", at: 1000, seq: 0},
		{key: "0a02", partition: realAlphaPartition, kind: event.TypeAgentOutput, summary: "灰度演练记录", content: "索引顺序的灰度演练记录", original: "索引顺序的灰度演练记录", at: 2000, seq: 0},
		{key: "0a03", partition: realAlphaPartition, kind: event.TypeThinkingPlan, summary: "复盘记录", content: "回填演练复盘", original: "回填演练复盘", at: 3000, seq: 0},
		{key: "0a04", partition: realAlphaPartition, kind: event.TypeActionCommand, summary: "登录改版", content: "微信登录改版上线", original: "微信登录改版上线", at: 4000, seq: 0},
		{key: "0b05", partition: realBetaPartition, kind: event.TypeExternalInput, summary: "次序清单", content: "迁移阶段回填索引次序", original: "迁移阶段回填索引次序", at: 5000, seq: 0},
	}
}

// realBodyOf derives and encodes the body one corpus event commits under.
func realBodyOf(t *testing.T, ev realStoreEvent) []byte {
	t.Helper()
	fact := event.Event{
		Key:       ev.key,
		Type:      ev.kind,
		Timestamp: time.UnixMilli(ev.at),
		Meta:      map[string]string{event.MetaKeyEventSummary: ev.summary},
		Message:   model.Message{Role: model.RoleUser, Content: ev.content},
		Owner:     realOwnerOf(),
	}
	body, err := memory.BodyFromEvent(fact, realEventFormat, realEventRetention)
	if err != nil {
		t.Fatalf("BodyFromEvent(%s): %v", ev.key, err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("Encode(%s): %v", ev.key, err)
	}
	return encoded
}

// newRealStoreChain commits the corpus into a real file tier, registers every
// original under the reference its body carries, and generates every event
// vector through the given embedder binding.
func newRealStoreChain(t *testing.T, embedder memory.Embedder) *realStoreChain {
	t.Helper()
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	if err != nil {
		t.Fatalf("OpenTier(localfile): %v", err)
	}
	scoped, ok := tier.Originals.(ledger.ScopedOriginalStore)
	if !ok {
		t.Fatalf("the real file tier serves no scoped read face: %T", tier.Originals)
	}
	events := realCorpusOf()
	for _, ev := range events {
		encoded := realBodyOf(t, ev)
		body, err := memory.DecodeEventBody(encoded)
		if err != nil {
			t.Fatalf("DecodeEventBody(%s): %v", ev.key, err)
		}
		if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{
			PartitionID: memory.PartitionIDFromName(ev.partition),
			WindowTS:    ev.at,
			Seq:         ev.seq,
			EventKey:    ev.key,
			EventTime:   ev.at,
			Body:        encoded,
		}); err != nil {
			t.Fatalf("StoreEvent(%s): %v", ev.key, err)
		}
		if err := tier.Originals.Put(ctx, body.Ref(), []byte(ev.original)); err != nil {
			t.Fatalf("Put(original %s): %v", ev.key, err)
		}
	}
	seam := memory.NewVectorSeam(0)
	t.Cleanup(seam.Close)
	seam.Bind(embedder)
	for _, ev := range events {
		if !seam.Submit(ev.key, ev.content) {
			t.Fatalf("Submit(%s) refused: the seam is bound and the queue is empty", ev.key)
		}
		awaitStoreVector(t, seam, ev.key)
	}
	source := memory.NewStoreQuerySource()
	return &realStoreChain{
		tier:     tier,
		scoped:   scoped,
		store:    tier.Segments,
		source:   source,
		seam:     seam,
		searcher: search.NewStoreSearcher(source, seam, embedder),
		events:   events,
	}
}

// awaitStoreVector waits until the vector of key is ready, failing on a bounded
// wait so a stalled generation fails the case instead of hanging the run.
func awaitStoreVector(t *testing.T, seam *memory.VectorSeam, key event.EventID) {
	t.Helper()
	deadline := time.Now().Add(realAwaitLimit)
	for time.Now().Before(deadline) {
		if _, ready := seam.Vector(key); ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("vector of %s never became ready", key)
}

// landEvent lands one committed event into the observation table by name.
func (c *realStoreChain) landEvent(t *testing.T, key event.EventID) {
	t.Helper()
	stored, held, err := c.store.GetEvent(context.Background(), key)
	if err != nil {
		t.Fatalf("GetEvent(%s): %v", key, err)
	}
	if !held {
		t.Fatalf("GetEvent(%s) held nothing: the commit never published", key)
	}
	if err := c.source.LandEvent(context.Background(), stored); err != nil {
		t.Fatalf("LandEvent(%s): %v", key, err)
	}
}

// landStore lands every event the real store has published into the table.
func (c *realStoreChain) landStore(t *testing.T) {
	t.Helper()
	if err := c.source.ObserveStore(context.Background(), c.store); err != nil {
		t.Fatalf("ObserveStore: %v", err)
	}
}

// recall runs one retrieval over the landed table inside the named partition
// scope, under the page bound and continuation cursor the caller carries.
func (c *realStoreChain) recall(t *testing.T, text, partition, strategy string, limit int, cursor search.Cursor) search.Result {
	t.Helper()
	result, err := c.searcher.Search(context.Background(), search.Query{
		Text:       text,
		Strategy:   search.Strategy(strategy),
		Cursor:     cursor,
		Partitions: []string{partition},
		Limit:      limit,
	})
	if err != nil {
		t.Fatalf("Search(%q in %s): %v", text, partition, err)
	}
	return result
}

// originalKeysOf returns the event keys of the reference set in order.
func originalKeysOf(refs []ledger.OriginalRef) []event.EventID {
	keys := make([]event.EventID, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, ref.Key)
	}
	return keys
}

// eventKeysOf returns the event keys of the query references in order.
func eventKeysOf(refs []memory.EventReference) []event.EventID {
	keys := make([]event.EventID, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, ref.Key)
	}
	return keys
}

// scopedReadBack returns the exact recall of key inside scope and the bytes its
// segmented walk concatenates back to.
func scopedReadBack(t *testing.T, scoped ledger.ScopedOriginalStore, scope ledger.ReadScope, key event.EventID) ([]byte, []byte) {
	t.Helper()
	ctx := context.Background()
	reader, err := ledger.NewScopedReader(scoped, scope)
	if err != nil {
		t.Fatalf("NewScopedReader: %v", err)
	}
	result := reader.Get(ctx, key)
	if result.Outcome != ledger.RecallFound {
		t.Fatalf("Get(%s) outcome = %v, want RecallFound: the scoped read serves the committed original", key, result.Outcome)
	}
	var walked []byte
	position := ledger.SegmentPosition{}
	for {
		segment, err := reader.ReadSegment(ctx, key, ledger.SegmentRequest{From: position, MaxBytes: 8})
		if err != nil {
			t.Fatalf("ReadSegment(%s at %d): %v", key, position.Offset, err)
		}
		walked = append(walked, segment.Body...)
		if segment.EOF {
			break
		}
		position = segment.Next
	}
	return result.Body, walked
}

// TestSearchRecallsFromRealStore drives the real store retrieval chain: a committed event reaches the query face and its original reads back byte for byte.
//   - a committed event is consumed only after the observation table lands it, so the read face never serves unobserved bytes
//   - the retrieval text appears in no stored summary or content, so a recalled context is the vector leg speaking
//   - hybrid fusion leads with the hit both legs carry, on the reciprocal rank rule the fusion states
//   - an event under an unauthorized partition takes no part, and a reader outside the scope is refused by name
//   - the pages of one retrieval union to its full result, with no hit counted twice
//   - a real embedding endpoint is resource-gated: absent it is skipped, half-configured it is refused
//
// 契约: docs/wiki/memory/memory-architecture.md#inmemory-retrieval
func TestSearchRecallsFromRealStore(t *testing.T) {
	ctx := context.Background()

	t.Run("a committed event is consumed only after the observation table lands it", func(t *testing.T) {
		chain := newRealStoreChain(t, memoryembedder.NewHashEmbedder(0))
		if got := chain.store.EventCount(); got != len(chain.events) {
			t.Fatalf("real store holds %d committed events, want %d", got, len(chain.events))
		}
		if got := chain.source.ObservedCount(); got != 0 {
			t.Fatalf("observation table holds %d events before any landing, want 0", got)
		}
		landed, err := chain.source.QueryEvents(ctx, memory.QueryOptions{
			PartitionIDs: []int{memory.PartitionIDFromName(realAlphaPartition)},
			OrderBy:      memory.QueryOrderByDesc,
		})
		if err != nil {
			t.Fatalf("QueryEvents(unlanded): %v", err)
		}
		if len(landed) != 0 {
			t.Fatalf("QueryEvents answered %v before any landing, want none: a query consumes an event after the table observes it", eventKeysOf(landed))
		}
		unseen := chain.recall(t, realRecallText, realAlphaPartition, string(search.StrategyHybrid), 10, "")
		if len(unseen.Refs) != 0 {
			t.Fatalf("Search answered %v before any landing, want none: the retrieval reads through the query face", originalKeysOf(unseen.Refs))
		}

		chain.landEvent(t, "0a01")
		if got := chain.source.ObservedCount(); got != 1 {
			t.Fatalf("observation table holds %d events after one landing, want 1", got)
		}
		if !chain.source.Observed("0a01") {
			t.Fatalf("Observed(0a01) = false after its landing")
		}
		partial, err := chain.source.QueryEvents(ctx, memory.QueryOptions{
			PartitionIDs: []int{memory.PartitionIDFromName(realAlphaPartition)},
			OrderBy:      memory.QueryOrderByDesc,
		})
		if err != nil {
			t.Fatalf("QueryEvents(one landed): %v", err)
		}
		if got := eventKeysOf(partial); !reflect.DeepEqual(got, []event.EventID{"0a01"}) {
			t.Fatalf("QueryEvents keys = %v, want [0a01]: only the landed event is visible", got)
		}
		seen := chain.recall(t, realRecallText, realAlphaPartition, string(search.StrategyHybrid), 10, "")
		if got := originalKeysOf(seen.Refs); !reflect.DeepEqual(got, []event.EventID{"0a01"}) {
			t.Fatalf("Search keys = %v, want [0a01]: the retrieval consumes exactly what the table observed", got)
		}

		chain.landStore(t)
		if got := chain.source.ObservedCount(); got != len(chain.events) {
			t.Fatalf("observation table holds %d events after observing the store, want %d", got, len(chain.events))
		}
		chain.landStore(t)
		if got := chain.source.ObservedCount(); got != len(chain.events) {
			t.Fatalf("observation table holds %d events after observing twice, want %d: a repeat moves nothing", got, len(chain.events))
		}
		all, err := chain.source.QueryEvents(ctx, memory.QueryOptions{
			PartitionIDs: []int{memory.PartitionIDFromName(realAlphaPartition)},
			OrderBy:      memory.QueryOrderByDesc,
		})
		if err != nil {
			t.Fatalf("QueryEvents(landed): %v", err)
		}
		if got := eventKeysOf(all); !reflect.DeepEqual(got, []event.EventID{"0a04", "0a03", "0a02", "0a01"}) {
			t.Fatalf("QueryEvents keys = %v, want newest first over the four landed partition events", got)
		}
	})

	t.Run("the retrieval recalls the context and reads its original back byte for byte", func(t *testing.T) {
		chain := newRealStoreChain(t, memoryembedder.NewHashEmbedder(0))
		chain.landStore(t)
		for _, ev := range chain.events {
			if strings.Contains(strings.ToLower(ev.summary), strings.ToLower(realRecallText)) || strings.Contains(strings.ToLower(ev.content), strings.ToLower(realRecallText)) {
				t.Fatalf("corpus event %s carries the retrieval text in its query fields, so the keyword leg could reach it", ev.key)
			}
		}
		keyword := chain.recall(t, realRecallText, realAlphaPartition, string(search.StrategyKeywordOnly), 10, "")
		if len(keyword.Refs) != 0 {
			t.Fatalf("keyword-only recall returned %v, want none: the retrieval text is no substring of any stored query field", originalKeysOf(keyword.Refs))
		}
		hybrid := chain.recall(t, realRecallText, realAlphaPartition, string(search.StrategyHybrid), 10, "")
		want := []event.EventID{"0a01", "0a02", "0a03"}
		if got := originalKeysOf(hybrid.Refs); !reflect.DeepEqual(got, want) {
			t.Fatalf("hybrid recall keys = %v, want %v: the vector leg recalls the context first, then the weaker fingerprint neighbours", got, want)
		}
		exact, walked := scopedReadBack(t, chain.scoped, ledger.ReadScope{
			Owner:           realOwnerOf(),
			Format:          realEventFormat,
			RetentionPolicy: realEventRetention,
		}, "0a01")
		if !bytes.Equal(exact, []byte(chain.events[0].original)) {
			t.Fatalf("exact recall read %q, want the committed original byte for byte", exact)
		}
		if !bytes.Equal(walked, []byte(chain.events[0].original)) {
			t.Fatalf("segmented walk concatenated %q, want the committed original byte for byte", walked)
		}
	})

	t.Run("hybrid fusion leads with the hit both legs carry", func(t *testing.T) {
		chain := newRealStoreChain(t, memoryembedder.NewHashEmbedder(0))
		chain.landStore(t)
		keywordLeg, err := chain.source.QueryEvents(ctx, memory.QueryOptions{
			PartitionIDs: []int{memory.PartitionIDFromName(realAlphaPartition)},
			Keyword:      realFusionText,
			OrderBy:      memory.QueryOrderByDesc,
		})
		if err != nil {
			t.Fatalf("QueryEvents(keyword leg): %v", err)
		}
		if got := eventKeysOf(keywordLeg); !reflect.DeepEqual(got, []event.EventID{"0a03"}) {
			t.Fatalf("keyword leg = %v, want [0a03]: the fusion text sits in one stored content", got)
		}
		keyword := chain.recall(t, realFusionText, realAlphaPartition, string(search.StrategyKeywordOnly), 10, "")
		if got := originalKeysOf(keyword.Refs); !reflect.DeepEqual(got, []event.EventID{"0a03"}) {
			t.Fatalf("keyword-only keys = %v, want [0a03]", got)
		}
		fused := chain.recall(t, realFusionText, realAlphaPartition, string(search.StrategyHybrid), 10, "")
		want := []event.EventID{"0a03", "0a02", "0a01", "0a04"}
		if got := originalKeysOf(fused.Refs); !reflect.DeepEqual(got, want) {
			t.Fatalf("fused keys = %v, want %v: the hit both legs carry scores 1/(%d+0) twice and leads every single-leg hit, and the tail keeps the vector leg order of the deterministic fingerprint", got, want, search.RRFRankConstant)
		}
		if fused.Refs[0].Key != "0a03" {
			t.Fatalf("fused head = %s, want 0a03: a reference both legs hit outranks a reference one leg hits", fused.Refs[0].Key)
		}
		again := chain.recall(t, realFusionText, realAlphaPartition, string(search.StrategyHybrid), 10, "")
		if !reflect.DeepEqual(fused, again) {
			t.Fatalf("two identical retrievals differ: %v vs %v, want position-by-position identity", originalKeysOf(fused.Refs), originalKeysOf(again.Refs))
		}
		for _, ref := range fused.Refs {
			if ref.Key == "0b05" {
				t.Fatalf("fused set carries 0b05, want none: the event of the other partition takes no part")
			}
		}
	})

	t.Run("an event outside the authorized partition never appears", func(t *testing.T) {
		chain := newRealStoreChain(t, memoryembedder.NewHashEmbedder(0))
		chain.landStore(t)
		scoped := chain.recall(t, realRecallText, realAlphaPartition, string(search.StrategyHybrid), 10, "")
		for _, ref := range scoped.Refs {
			if ref.Key == "0b05" {
				t.Fatalf("authorized-scope recall carries 0b05, want none: that event shares the most fingerprint mass with the retrieval text and would lead the vector leg if the scope leaked")
			}
		}
		if got := originalKeysOf(scoped.Refs); !reflect.DeepEqual(got, []event.EventID{"0a01", "0a02", "0a03"}) {
			t.Fatalf("authorized-scope keys = %v, want [0a01 0a02 0a03]", got)
		}
		other := chain.recall(t, realRecallText, realBetaPartition, string(search.StrategyHybrid), 10, "")
		if got := originalKeysOf(other.Refs); !reflect.DeepEqual(got, []event.EventID{"0b05"}) {
			t.Fatalf("other-scope keys = %v, want [0b05]: the scope cut runs in both directions", got)
		}
		empty, err := chain.searcher.Search(ctx, search.Query{Text: realRecallText, Strategy: search.StrategyHybrid, Limit: 10})
		if err != nil {
			t.Fatalf("Search(no partitions): %v", err)
		}
		if len(empty.Refs) != 0 {
			t.Fatalf("unscoped recall returned %v, want none: an empty authorized set authorizes no partition", originalKeysOf(empty.Refs))
		}
	})

	t.Run("a reader outside the reference scope is refused by name", func(t *testing.T) {
		chain := newRealStoreChain(t, memoryembedder.NewHashEmbedder(0))
		chain.landStore(t)
		inside := ledger.ReadScope{Owner: realOwnerOf(), Format: realEventFormat, RetentionPolicy: realEventRetention}
		exact, _ := scopedReadBack(t, chain.scoped, inside, "0a01")
		if !bytes.Equal(exact, []byte(chain.events[0].original)) {
			t.Fatalf("in-scope recall read %q, want the committed original byte for byte", exact)
		}
		outside := ledger.ReadScope{
			Owner:           event.Address{AgentID: "agent-other", SessionID: "session-other"},
			Format:          realEventFormat,
			RetentionPolicy: realEventRetention,
		}
		stranger, err := ledger.NewScopedReader(chain.scoped, outside)
		if err != nil {
			t.Fatalf("NewScopedReader(outside): %v", err)
		}
		hidden := stranger.Get(ctx, "0a01")
		if hidden.Outcome != ledger.RecallMissing {
			t.Fatalf("out-of-scope Get(0a01) outcome = %v, want RecallMissing: a key whose references lie outside the scope reads as missing and leaks nothing", hidden.Outcome)
		}
		if _, err := stranger.ReadSegment(ctx, "0a01", ledger.SegmentRequest{MaxBytes: 8}); !errors.Is(err, ledger.ErrOriginalMissing) {
			t.Fatalf("out-of-scope ReadSegment(0a01) = %v, want the named refusal ErrOriginalMissing", err)
		}
		if _, err := ledger.NewScopedReader(chain.scoped, ledger.ReadScope{}); !errors.Is(err, ledger.ErrScopeRequired) {
			t.Fatalf("NewScopedReader(zero scope) = %v, want the named refusal ErrScopeRequired", err)
		}
	})

	t.Run("continuation pages union to the full result", func(t *testing.T) {
		chain := newRealStoreChain(t, memoryembedder.NewHashEmbedder(0))
		chain.landStore(t)
		full := chain.recall(t, realRecallText, realAlphaPartition, string(search.StrategyHybrid), 20, "")
		want := []event.EventID{"0a01", "0a02", "0a03"}
		if got := originalKeysOf(full.Refs); !reflect.DeepEqual(got, want) {
			t.Fatalf("full recall keys = %v, want %v", got, want)
		}
		var (
			pages   [][]ledger.OriginalRef
			union   []ledger.OriginalRef
			cursor  search.Cursor
			pageSet = map[event.EventID]bool{}
		)
		for page := 0; ; page++ {
			if page > 8 {
				t.Fatalf("continuation did not terminate after %d pages over %d references", page, len(full.Refs))
			}
			result := chain.recall(t, realRecallText, realAlphaPartition, string(search.StrategyHybrid), 2, cursor)
			if len(result.Refs) == 0 {
				t.Fatalf("page %d carried no reference while a continuation cursor was handed out", page)
			}
			pages = append(pages, result.Refs)
			for _, ref := range result.Refs {
				if pageSet[ref.Key] {
					t.Fatalf("page %d repeats hit %s, want each hit counted once across pages", page, ref.Key)
				}
				pageSet[ref.Key] = true
			}
			union = append(union, result.Refs...)
			cursor = result.NextCursor
			if cursor == "" {
				break
			}
		}
		if len(pages) != 2 {
			t.Fatalf("continuation produced %d pages, want 2: %d references under a page bound of 2", len(pages), len(full.Refs))
		}
		if !reflect.DeepEqual(union, full.Refs) {
			t.Fatalf("page union = %v, want the full result %v", originalKeysOf(union), originalKeysOf(full.Refs))
		}
	})

	t.Run("the real embedding endpoint carries the same recall", func(t *testing.T) {
		base := os.Getenv("TAGENT_REAL_EMBEDDING_BASE_URL")
		key := os.Getenv("TAGENT_REAL_EMBEDDING_API_KEY")
		modelName := os.Getenv("TAGENT_REAL_EMBEDDING_MODEL")
		provisioned := 0
		for _, value := range []string{base, key, modelName} {
			if value != "" {
				provisioned++
			}
		}
		if provisioned == 0 {
			t.Skip("real embedding endpoint not provisioned: set TAGENT_REAL_EMBEDDING_BASE_URL, TAGENT_REAL_EMBEDDING_API_KEY and TAGENT_REAL_EMBEDDING_MODEL (resource gate)")
		}
		if provisioned != 3 {
			t.Fatalf("the real embedding gate is half-configured (%d of 3): a partial endpoint is refused instead of silently answered under the deterministic stand-in", provisioned)
		}
		embedder := &realEmbeddingEndpoint{base: base, key: key, model: modelName}
		vector, err := embedder.Embed(ctx, realRecallText)
		if err != nil {
			t.Fatalf("the provisioned embedding endpoint failed the retrieval text: %v", err)
		}
		if len(vector) == 0 {
			t.Fatalf("the provisioned embedding endpoint produced no vector, want a non-empty one: zero output is refused")
		}
		chain := newRealStoreChain(t, embedder)
		chain.landStore(t)
		recalled := chain.recall(t, realRecallText, realAlphaPartition, string(search.StrategyHybrid), 10, "")
		if len(recalled.Refs) == 0 || recalled.Refs[0].Key != "0a01" {
			t.Fatalf("endpoint recall keys = %v, want the context 0a01 first: the retrieval text is no stored substring, so an empty answer names a zero-output endpoint", originalKeysOf(recalled.Refs))
		}
		exact, _ := scopedReadBack(t, chain.scoped, ledger.ReadScope{
			Owner:           realOwnerOf(),
			Format:          realEventFormat,
			RetentionPolicy: realEventRetention,
		}, "0a01")
		if !bytes.Equal(exact, []byte(chain.events[0].original)) {
			t.Fatalf("endpoint recall read %q, want the committed original byte for byte", exact)
		}
	})
}

// realEmbeddingEndpoint is the embedding client of the provisioned endpoint:
// one embedding call per text, refusing an answer that carries no vector.
type realEmbeddingEndpoint struct {
	base  string
	key   string
	model string
}

// Embed returns the embedding vector the endpoint answers for text, or a named
// failure when the call fails or the answer carries no vector.
func (e *realEmbeddingEndpoint) Embed(ctx context.Context, text string) ([]float32, error) {
	payload, err := json.Marshal(map[string]string{"model": e.model, "input": text})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.base, "/")+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+e.key)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding endpoint answered %d: %s", response.StatusCode, string(body))
	}
	var decoded struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Data) == 0 || len(decoded.Data[0].Embedding) == 0 {
		return nil, errors.New("embedding endpoint produced no vector")
	}
	return decoded.Data[0].Embedding, nil
}
