package rl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// exportProbeStore delegates every read to a real InMemoryStore and records what
// the export actually asked for. Any write API is a hard failure: the exporter
// must never touch TTL, tombstones, retention leases or stored facts.
type exportProbeStore struct {
	inner *memory.InMemoryStore

	queries   []memory.QueryOptions
	readKeys  []int64
	writeAPIs []string

	// queryErrAtPage fails the Nth (1-based) QueryEvents call.
	queryErrAtPage int
	// spoofPages replaces QueryEvents results page by page.
	spoofPages [][]memory.EventReference
	// overrideEvents returns bodies verbatim, bypassing partition routing.
	overrideEvents map[int64]memory.FullEvent
	// unreadableKeys makes GetEvent fail as if the fact were gone.
	unreadableKeys map[int64]bool
}

func newExportProbeStore() *exportProbeStore {
	return &exportProbeStore{
		inner:          memory.NewInMemoryStore(),
		overrideEvents: map[int64]memory.FullEvent{},
		unreadableKeys: map[int64]bool{},
	}
}

func (p *exportProbeStore) markWrite(api string) {
	p.writeAPIs = append(p.writeAPIs, api)
	panic("ExportTrainingFacts must be read-only, called " + api)
}

func (p *exportProbeStore) StoreEvent(key int64, event memory.FullEvent) error {
	p.markWrite("StoreEvent")
	return nil
}

func (p *exportProbeStore) StoreEventWithEmbedding(key int64, event memory.FullEvent, emb []float32) error {
	p.markWrite("StoreEventWithEmbedding")
	return nil
}

func (p *exportProbeStore) DeleteEvent(key int64) error {
	p.markWrite("DeleteEvent")
	return nil
}

func (p *exportProbeStore) GetEvent(key int64) (*memory.FullEvent, error) {
	p.readKeys = append(p.readKeys, key)
	if evt, ok := p.overrideEvents[key]; ok {
		return &evt, nil
	}
	if p.unreadableKeys[key] {
		return nil, fmt.Errorf("event %d unavailable: %w", key, memory.ErrKeyNotFound)
	}
	return p.inner.GetEvent(key)
}

func (p *exportProbeStore) GetEvents(keys []int64) ([]memory.FullEvent, error) {
	return p.inner.GetEvents(keys)
}

func (p *exportProbeStore) QueryEvents(q memory.QueryOptions) ([]memory.EventReference, error) {
	p.queries = append(p.queries, q)
	page := len(p.queries)
	if p.queryErrAtPage == page {
		return nil, errors.New("probe: paging read failed")
	}
	if page <= len(p.spoofPages) {
		return p.spoofPages[page-1], nil
	}
	return p.inner.QueryEvents(q)
}

func (p *exportProbeStore) SearchByEmbedding(query []float32, topK int) ([]memory.EventReference, error) {
	return p.inner.SearchByEmbedding(query, topK)
}

func (p *exportProbeStore) SupportsVectorSearch() bool { return false }

func (p *exportProbeStore) GetStats() memory.StoreStats { return p.inner.GetStats() }

// RelationStore exposes the causal edge store, so the exporter resolves
// parent_key through the same optional seam the production writer used.
func (p *exportProbeStore) RelationStore() memory.RelationStore { return p.inner.RelationStore() }

// exportBaseMs is deliberately after the snowflake epoch (1704067200 s) so
// seeded keys are positive and ordered the way production keys are.
const exportBaseMs = int64(1_750_000_000_000)

func seedFact(t *testing.T, p *exportProbeStore, pid int, eventType, callID string, tsMs int64, content string) int64 {
	t.Helper()
	key := memory.NewSnowflakeEventKey(pid, tsMs)
	evt := memory.FullEvent{
		EventKey:     key,
		PartitionID:  pid,
		EventType:    eventType,
		EventSummary: fmt.Sprintf("%s@%d", eventType, pid),
		Timestamp:    tsMs,
		Content:      content,
		ToolResults:  map[string]interface{}{},
		Metadata:     map[string]string{},
	}
	if callID != "" {
		evt.Metadata["call_id"] = callID
	}
	require.NoError(t, p.inner.StoreEvent(key, evt))
	return key
}

func seedFeedback(t *testing.T, p *exportProbeStore, parentKey int64, verdict string) int64 {
	t.Helper()
	key, err := memory.BindFeedback(p.inner, parentKey, memory.FeedbackPayload{
		Verdict: verdict, Source: "user", Note: "explicit rating",
	})
	require.NoError(t, err)
	return key
}

// seedFeedbackWithoutParent writes a feedback event whose Content carries no
// parent_key at all (task-level feedback that was never precisely linked).
func seedFeedbackWithoutParent(t *testing.T, p *exportProbeStore, pid int, tsMs int64) int64 {
	t.Helper()
	content, err := json.Marshal(map[string]any{"verdict": "bad", "source": "user"})
	require.NoError(t, err)
	return seedFact(t, p, pid, tagentevent.TypeFeedback, "", tsMs, string(content))
}

// exportLines decodes every JSONL line with UseNumber, because an EventKey is a
// 63-bit number and float64 decoding would silently round it.
func exportLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var obj map[string]any
		require.NoError(t, dec.Decode(&obj), "every exported line must be one JSON object")
		lines = append(lines, obj)
	}
	return lines
}

func int64FromLine(t *testing.T, line map[string]any, field string) int64 {
	t.Helper()
	raw, ok := line[field]
	require.True(t, ok, "missing field %q", field)
	switch v := raw.(type) {
	case json.Number:
		n, err := v.Int64()
		require.NoError(t, err)
		return n
	case float64:
		t.Fatalf("field %q decoded through float64: precision loss", field)
		return 0
	default:
		t.Fatalf("field %q is %T, want a number", field, raw)
		return 0
	}
}

// TestTrainingExport_PartitionAuthorization pins export partition authorization.
//   - a foreign partition is refused by name, never silently filtered away
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestTrainingExport_PartitionAuthorization(t *testing.T) {
	t.Run("empty_allowlist_is_refused_before_any_read", func(t *testing.T) {
		p := newExportProbeStore()
		seedFact(t, p, 3, "assistant", "run1-1", exportBaseMs, "hello")
		var buf bytes.Buffer

		_, err := ExportTrainingFacts(context.Background(), p, ExportOptions{}, &buf)
		require.ErrorIs(t, err, ErrExportAuthorization)
		assert.Empty(t, p.queries, "no authorization must not turn into a whole-store scan")
		assert.Zero(t, buf.Len(), "nothing may be written out")
	})

	t.Run("page_limit_over_hard_cap_is_refused", func(t *testing.T) {
		p := newExportProbeStore()
		_, err := ExportTrainingFacts(context.Background(), p,
			ExportOptions{PartitionIDs: []int{3}, PageLimit: exportMaxPageLimit + 1}, &bytes.Buffer{})
		require.ErrorIs(t, err, ErrExportPageLimitExceeded)

		_, err = ExportTrainingFacts(context.Background(), p,
			ExportOptions{PartitionIDs: []int{3}, PageLimit: -1}, &bytes.Buffer{})
		require.ErrorIs(t, err, ErrExportPageLimitExceeded)
	})

	t.Run("nil_store_and_nil_writer_are_refused", func(t *testing.T) {
		p := newExportProbeStore()
		_, err := ExportTrainingFacts(context.Background(), nil, ExportOptions{PartitionIDs: []int{3}}, &bytes.Buffer{})
		require.ErrorIs(t, err, ErrExportNilStore)
		_, err = ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{3}}, nil)
		require.ErrorIs(t, err, ErrExportNilWriter)
	})

	t.Run("every_query_is_scoped_to_one_allowed_partition", func(t *testing.T) {
		p := newExportProbeStore()
		seedFact(t, p, 3, "assistant", "run1-1", exportBaseMs, "hi")
		seedFact(t, p, 5, "assistant", "run1-2", exportBaseMs+1000, "yo")
		seedFact(t, p, 9, "assistant", "run1-3", exportBaseMs+2000, "elsewhere")

		_, err := ExportTrainingFacts(context.Background(), p, ExportOptions{
			PartitionIDs: []int{3, 5, 3},
			Since:        exportBaseMs - 1000,
			Until:        exportBaseMs + 999_000,
		}, &bytes.Buffer{})
		require.NoError(t, err)
		require.NotEmpty(t, p.queries)
		for _, q := range p.queries {
			require.Len(t, q.PartitionIDs, 1, "an un-scoped query would be a whole-store scan")
			assert.Contains(t, []int{3, 5}, q.PartitionIDs[0])
			assert.Equal(t, exportBaseMs-1000, q.StartTime)
			assert.Equal(t, exportBaseMs+999_000, q.EndTime)
		}
		assert.Equal(t, []int{3, 5}, manifestPartitions(t, p), "duplicated partitions collapse")
	})

	t.Run("foreign_partition_body_is_counted_forbidden_and_never_written", func(t *testing.T) {
		p := newExportProbeStore()
		foreign := memory.NewSnowflakeEventKey(7, exportBaseMs)
		p.spoofPages = [][]memory.EventReference{{
			{EventKey: foreign, PartitionID: 3, EventType: "assistant"},
		}}
		p.overrideEvents[foreign] = memory.FullEvent{EventKey: foreign, PartitionID: 7, EventType: "assistant"}

		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{3}}, &buf)
		require.NoError(t, err)
		assert.Equal(t, 1, m.Forbidden)
		assert.Zero(t, m.EventsWritten)
		assert.Zero(t, buf.Len(), "an unauthorized body must never reach the snapshot")
	})

	t.Run("body_partition_disagreeing_with_its_key_is_forbidden", func(t *testing.T) {
		p := newExportProbeStore()
		key := memory.NewSnowflakeEventKey(3, exportBaseMs)
		p.spoofPages = [][]memory.EventReference{{
			{EventKey: key, PartitionID: 3, EventType: "assistant"},
		}}
		p.overrideEvents[key] = memory.FullEvent{EventKey: key, PartitionID: 42, EventType: "assistant"}

		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{3}}, &buf)
		require.NoError(t, err)
		assert.Equal(t, 1, m.Forbidden, "identity/body mismatch is an authorization failure, not a warning")
		assert.Zero(t, buf.Len())
	})

	t.Run("paging_walks_every_page", func(t *testing.T) {
		p := newExportProbeStore()
		for i := 0; i < 5; i++ {
			seedFact(t, p, 3, "assistant", fmt.Sprintf("run1-%d", i), exportBaseMs+int64(i)*1000, fmt.Sprintf("m%d", i))
		}
		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p,
			ExportOptions{PartitionIDs: []int{3}, PageLimit: 2}, &buf)
		require.NoError(t, err)
		assert.Equal(t, 5, m.EventsWritten)
		assert.Equal(t, 3, m.Pages, "5 facts at page size 2 need 3 pages")
		assert.True(t, m.Complete)
		assert.Len(t, exportLines(t, &buf), 5)
	})

	t.Run("paging_failure_returns_error_and_complete_false", func(t *testing.T) {
		p := newExportProbeStore()
		for i := 0; i < 5; i++ {
			seedFact(t, p, 3, "assistant", fmt.Sprintf("run1-%d", i), exportBaseMs+int64(i)*1000, "body")
		}
		p.queryErrAtPage = 2

		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p,
			ExportOptions{PartitionIDs: []int{3}, PageLimit: 2}, &buf)
		require.Error(t, err, "a partial read must not be reported as a complete snapshot")
		assert.False(t, m.Complete)
		assert.NotZero(t, m.Errs)
		assert.Equal(t, 1, m.ReadErrors)
		assert.Equal(t, 2, m.EventsWritten, "lines already written stay written; only completeness degrades")
		assert.Equal(t, 2, m.Pages)
	})

	t.Run("unreadable_body_is_a_read_error_not_a_silent_drop", func(t *testing.T) {
		p := newExportProbeStore()
		key := seedFact(t, p, 3, "assistant", "run1-1", exportBaseMs, "hi")
		p.unreadableKeys[key] = true

		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{3}}, &buf)
		require.Error(t, err)
		assert.False(t, m.Complete)
		assert.Equal(t, 1, m.ReadErrors)
		assert.Zero(t, m.EventsWritten)
	})

	t.Run("cancelled_context_stops_without_claiming_completeness", func(t *testing.T) {
		p := newExportProbeStore()
		seedFact(t, p, 3, "assistant", "run1-1", exportBaseMs, "hi")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		m, err := ExportTrainingFacts(ctx, p, ExportOptions{PartitionIDs: []int{3}}, &bytes.Buffer{})
		require.Error(t, err)
		assert.False(t, m.Complete)
		assert.Zero(t, m.Pages)
	})
}

// manifestPartitions reads back the partition allowlist the export echoed by
// re-running it once; used only to assert de-duplication.
func manifestPartitions(t *testing.T, p *exportProbeStore) []int {
	t.Helper()
	m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{3, 5, 3}}, &bytes.Buffer{})
	require.NoError(t, err)
	return m.Partitions
}

func TestTrainingExport_FactLineShapeAndManifest(t *testing.T) {
	p := newExportProbeStore()
	userKey := seedFact(t, p, 3, "user", "", exportBaseMs-1000, "question")
	assistantKey := seedFact(t, p, 3, "assistant", "run1-7", exportBaseMs, "answer")
	require.NoError(t, p.inner.RelationStore().SetParent(assistantKey, userKey))

	var buf bytes.Buffer
	m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{3}}, &buf)
	require.NoError(t, err)
	require.True(t, m.Complete)

	lines := exportLines(t, &buf)
	require.Len(t, lines, 2)
	assert.Equal(t, 1, m.ParentKeyMissing, "the root user turn has no predecessor; that is reported, not invented")

	for _, raw := range bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")) {
		var fact ExportedFact
		require.NoError(t, json.Unmarshal(raw, &fact))
		original, err := p.inner.GetEvent(fact.EventKey)
		require.NoError(t, err)
		assert.Equal(t, original.EventKey, fact.EventKey)
		assert.Equal(t, original.PartitionID, fact.PartitionID)
		assert.Equal(t, original.EventType, fact.EventType)
		assert.Equal(t, original.EventSummary, fact.EventSummary)
		assert.Equal(t, original.Content, fact.Content)
		assert.Equal(t, original.Timestamp, fact.Timestamp)
		assert.Equal(t, original.Metadata["call_id"], fact.Metadata["call_id"])
	}
	assert.Equal(t, assistantKey, int64FromLine(t, lines[1], "event_key"))
	assert.Equal(t, tagentevent.FormatEventKey(userKey), lines[1]["parent_key"])

	obj := lines[0]
	for _, field := range []string{
		"event_key", "partition_id", "event_type", "event_summary", "timestamp",
		"content", "tool_calls", "tool_results", "metadata", "parent_key",
	} {
		assert.Contains(t, obj, field, "the snapshot must keep the FullEvent field set")
	}

	sum := sha256.Sum256(buf.Bytes())
	assert.Equal(t, hex.EncodeToString(sum[:]), m.SourceSHA256, "digest covers exactly the written lines")

	assert.Equal(t, []int{3}, m.Partitions)
	assert.Equal(t, exportDefaultPageLimit, m.PageLimit, "unset page size uses the documented default")
	assert.NotZero(t, m.GeneratedAt)
	assert.Equal(t, m.EventsWritten, len(lines))

	raw, err := json.Marshal(m)
	require.NoError(t, err)
	var flat map[string]any
	require.NoError(t, json.Unmarshal(raw, &flat))
	for _, key := range []string{"generated_at", "partitions", "events_written", "forbidden",
		"feedback_total", "join_bound", "join_missing", "join_expired_or_missing",
		"join_forbidden_parent", "join_ambiguous", "parent_key_missing", "read_errors",
		"source_sha256", "cutoff", "complete", "errs"} {
		assert.Contains(t, flat, key, "counters stay flat: a nested object reads as zero downstream")
	}
	assert.Empty(t, p.writeAPIs)
}

func TestTrainingExport_CallIDFilter(t *testing.T) {
	p := newExportProbeStore()
	seedFact(t, p, 3, "assistant", "run1-1", exportBaseMs, "kept")
	seedFact(t, p, 3, "assistant", "run1-2", exportBaseMs+1000, "dropped")

	var buf bytes.Buffer
	m, err := ExportTrainingFacts(context.Background(), p,
		ExportOptions{PartitionIDs: []int{3}, CallIDs: []string{"run1-1"}}, &buf)
	require.NoError(t, err)
	assert.Equal(t, 1, m.EventsWritten)
	assert.Equal(t, 1, m.Filtered)
	assert.Contains(t, buf.String(), "run1-1")
	assert.NotContains(t, buf.String(), "run1-2")
	assert.True(t, m.Complete, "an explicit selection is a complete snapshot of that selection")
}

func TestTrainingExport_FeedbackAssociation(t *testing.T) {
	const pid = 3

	t.Run("parent_call_id_binds_and_multiple_feedbacks_agree", func(t *testing.T) {
		p := newExportProbeStore()
		parentKey := seedFact(t, p, pid, "assistant", "run1-1", exportBaseMs, "the answer")
		seedFeedback(t, p, parentKey, "good")
		seedFeedback(t, p, parentKey, "good")

		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{pid}}, &buf)
		require.NoError(t, err)
		assert.Equal(t, 2, m.FeedbackTotal)
		assert.Equal(t, 2, m.JoinBound, "two feedbacks on one call are both bound, not an error")
		assert.Zero(t, m.JoinAmbiguous)
		assert.Zero(t, m.JoinMissing)
		assert.True(t, m.Complete)

		lines := exportLines(t, &buf)
		require.Len(t, lines, 3)
		for _, line := range lines {
			kind, _ := line["event_type"].(string)
			if kind != tagentevent.TypeFeedback {
				continue
			}
			assert.Equal(t, tagentevent.FormatEventKey(parentKey), line["parent_key"],
				"feedback line keeps the exact parent it points at")
		}
	})

	t.Run("parent_without_call_id_counts_missing", func(t *testing.T) {
		p := newExportProbeStore()
		parentKey := seedFact(t, p, pid, "assistant", "", exportBaseMs, "no attribution evidence")
		seedFeedback(t, p, parentKey, "good")

		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{pid}}, &buf)
		require.NoError(t, err)
		assert.Equal(t, 1, m.FeedbackTotal)
		assert.Zero(t, m.JoinBound)
		assert.Equal(t, 1, m.JoinMissing)
		assert.True(t, m.Complete, "a precise join is impossible, but the read itself was complete")
	})

	t.Run("feedback_without_parent_key_counts_missing", func(t *testing.T) {
		p := newExportProbeStore()
		seedFeedbackWithoutParent(t, p, pid, exportBaseMs)

		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{pid}}, &buf)
		require.NoError(t, err)
		assert.Equal(t, 1, m.FeedbackTotal)
		assert.Equal(t, 1, m.JoinMissing)
		assert.Equal(t, 1, m.ParentKeyMissing)
	})

	t.Run("unreadable_parent_counts_expired_or_missing_without_guessing", func(t *testing.T) {
		p := newExportProbeStore()
		parentKey := seedFact(t, p, pid, "assistant", "run1-1", exportBaseMs, "gone by TTL")
		fbKey := seedFeedback(t, p, parentKey, "good")
		p.spoofPages = [][]memory.EventReference{{
			{EventKey: fbKey, PartitionID: pid, EventType: tagentevent.TypeFeedback},
		}}
		p.unreadableKeys[parentKey] = true

		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{pid}}, &buf)
		require.NoError(t, err, "a missing parent is a fact to report, not a read failure of the snapshot")
		assert.Equal(t, 1, m.JoinExpiredOrMissing)
		assert.Zero(t, m.JoinBound)
		assert.Zero(t, m.ReadErrors)
		assert.True(t, m.Complete)

		lines := exportLines(t, &buf)
		require.Len(t, lines, 1)
		assert.Equal(t, fbKey, int64FromLine(t, lines[0], "event_key"))
		assert.Equal(t, tagentevent.FormatEventKey(parentKey), lines[0]["parent_key"],
			"the pointer survives even when the parent is unreadable")
	})

	t.Run("cross_partition_parent_is_forbidden_and_never_read", func(t *testing.T) {
		p := newExportProbeStore()
		otherKey := seedFact(t, p, 11, "assistant", "run1-1", exportBaseMs, "not authorized")
		seedFeedback(t, p, otherKey, "good")

		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{pid}}, &buf)
		require.NoError(t, err)
		assert.Zero(t, m.EventsWritten, "the feedback lives in an unauthorized partition")
		assert.Zero(t, m.FeedbackTotal)
		assert.Zero(t, m.Forbidden, "a foreign partition is never even surfaced for a write")
		for _, key := range p.readKeys {
			assert.NotEqual(t, otherKey, key, "parent identity must be checked before any read")
		}
	})

	t.Run("authorized_feedback_with_unauthorized_parent_is_forbidden_parent", func(t *testing.T) {
		p := newExportProbeStore()
		parentKey := seedFact(t, p, 11, "assistant", "run1-1", exportBaseMs, "not authorized")
		fbKey := memory.NewSnowflakeEventKey(pid, exportBaseMs+1000)
		content, err := json.Marshal(memory.FeedbackPayload{
			Verdict: "good", Source: "user", ParentKey: tagentevent.FormatEventKey(parentKey),
		})
		require.NoError(t, err)
		require.NoError(t, p.inner.StoreEvent(fbKey, memory.FullEvent{
			EventKey: fbKey, PartitionID: pid, EventType: tagentevent.TypeFeedback,
			Timestamp: exportBaseMs + 1000, Content: string(content), Metadata: map[string]string{},
		}))

		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{pid}}, &buf)
		require.NoError(t, err)
		assert.Equal(t, 1, m.FeedbackTotal)
		assert.Equal(t, 1, m.JoinForbiddenParent)
		assert.Zero(t, m.JoinBound)
		assert.Zero(t, m.JoinExpiredOrMissing, "unauthorized and absent are different columns; never merge them")
		for _, key := range p.readKeys {
			assert.NotEqual(t, parentKey, key, "unauthorized parent must be refused before the read")
		}
		assert.Equal(t, tagentevent.FormatEventKey(parentKey),
			exportLines(t, &buf)[0]["parent_key"], "the line still carries the pointer it had")
	})

	t.Run("same_call_id_claimed_by_contradictory_parents_is_ambiguous", func(t *testing.T) {
		p := newExportProbeStore()
		parentA := seedFact(t, p, pid, "assistant", "run1-9", exportBaseMs, "answer A")
		parentB := seedFact(t, p, pid, "assistant", "run1-9", exportBaseMs+1000, "answer B")
		seedFeedback(t, p, parentA, "good")
		seedFeedback(t, p, parentB, "bad")

		var buf bytes.Buffer
		m, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{pid}}, &buf)
		require.NoError(t, err)
		assert.Equal(t, 2, m.JoinAmbiguous, "one call_id, two different parents: report, never guess")
		assert.Zero(t, m.JoinBound)
		assert.True(t, m.Complete)
	})

	t.Run("export_is_read_only_and_leaves_the_store_untouched", func(t *testing.T) {
		p := newExportProbeStore()
		parentKey := seedFact(t, p, pid, "assistant", "run1-1", exportBaseMs, "answer")
		seedFeedback(t, p, parentKey, "good")

		_, err := ExportTrainingFacts(context.Background(), p, ExportOptions{PartitionIDs: []int{pid}}, &bytes.Buffer{})
		require.NoError(t, err)
		assert.Empty(t, p.writeAPIs, "no store write API may be called")
		assert.Equal(t, 2, p.inner.GetStats().TotalEvents, "facts before and after are the same count")
	})
}
