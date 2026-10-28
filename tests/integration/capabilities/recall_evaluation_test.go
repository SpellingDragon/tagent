package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
	memoryembedder "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/search"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

const (
	// evalKeywordText is a keyword substring of exactly one corpus event, so
	// the keyword leg alone recovers it and a hybrid retrieval adds no
	// expected hit over that baseline.
	evalKeywordText = "灰度演练记录"
	// evalFilterText is a keyword substring of two corpus events of different
	// frozen types, so the type filter of the query face visibly narrows the
	// recall.
	evalFilterText = "演练"
	// evalReportDirPattern names the run record directory the recall
	// evaluation report is written under, outside the repository.
	evalReportDirPattern = "tagent-recall-eval-"
)

// evalVerdictGain, evalVerdictZeroGain and evalVerdictFailed are the exhaustive
// conclusion values of one recall evaluation row: a row keeps the verdict it
// earned — a zero-gain row is never upgraded to a gain and a failed row is
// never answered as a hit (Eval 保留零增益与失败, 00.19 语义).
const (
	evalVerdictGain     = "gain"
	evalVerdictZeroGain = "zero-gain"
	evalVerdictFailed   = "failed"
)

// evalRow is one conclusion row of the recall evaluation report: the query the
// row ran, the expected keys it was judged against, what the hybrid retrieval
// answered beside its keyword-only baseline, the gain that difference earned,
// the verdict the row keeps, the named failure of a failed row, and the cost
// the row paid. A failed row carries its named failure and no fabricated hit.
type evalRow struct {
	Name           string          `json:"name"`
	Query          string          `json:"query"`
	Strategy       string          `json:"strategy"`
	Want           []event.EventID `json:"want"`
	Got            []event.EventID `json:"got"`
	BaselineGot    []event.EventID `json:"baseline_got"`
	Gain           int             `json:"gain"`
	Verdict        string          `json:"verdict"`
	Failure        string          `json:"failure,omitempty"`
	Hits           int             `json:"hits"`
	Pages          int             `json:"pages"`
	DurationMillis int64           `json:"duration_millis"`
}

// evalTotals is the honest tally of the report: the rows it holds counted by
// verdict, exactly as they are written, so a zero-gain or failed conclusion
// can never vanish from the totals.
type evalTotals struct {
	Rows           int   `json:"rows"`
	Gain           int   `json:"gain"`
	ZeroGain       int   `json:"zero_gain"`
	Failed         int   `json:"failed"`
	Hits           int   `json:"hits"`
	DurationMillis int64 `json:"duration_millis"`
}

// evalReport is the recall quality and cost report of one evaluation run: the
// conclusion rows preserved as they were earned beside the totals tallying
// them, written to the run record directory outside the repository.
type evalReport struct {
	Rows   []evalRow  `json:"rows"`
	Totals evalTotals `json:"totals"`
}

// evalRowSpec is one evaluation row as planned: the query text, the keys the
// row is judged against, and whether the row is planned to fail on a foreign
// continuation cursor.
type evalRowSpec struct {
	name       string
	text       string
	want       []event.EventID
	cursor     search.Cursor
	expectFail bool
}

// evalCountingEmbedder counts the embedding calls the retrieval face makes
// beside a real embedder underneath, so "no embedding is ever called" is
// observed rather than assumed.
type evalCountingEmbedder struct {
	inner memory.Embedder
	mu    sync.Mutex
	calls int
}

// Embed counts one embedding call and answers through the inner embedder.
func (e *evalCountingEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	return e.inner.Embed(ctx, text)
}

// count reports how many embedding calls were made so far.
func (e *evalCountingEmbedder) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// seedEvalCorpus commits the real corpus into a fresh localfile tier under the
// given root and returns the tier, so the formal assembly opened over the same
// root reads exactly what this process committed. The returned tier keeps the
// scoped read face the original bytes are read back through.
func seedEvalCorpus(t *testing.T, root string) *ledger.Tier {
	t.Helper()
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("OpenTier(localfile): %v", err)
	}
	for _, ev := range realCorpusOf() {
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
	return tier
}

// evalAssemble builds the formal assembly over the given storage root — or
// beside no storage when the root is empty — with the given embedder binding,
// or with no embedding at all when it names nothing. The recall face of the
// returned run is the production wiring under test; the run drives no model.
func evalAssemble(t *testing.T, storeRoot string, embedder memory.Embedder) *assembly.Run {
	t.Helper()
	ctx := context.Background()
	declRoot := t.TempDir()
	writePromptFile(t, declRoot, "prompts/system.md", "eval system prompt\n")
	writeSkillFolder(t, declRoot, "greet", "greet body\n")
	provider := &ntModel{name: "model-a", answer: func(*model.Request) *model.Response {
		t.Error("the recall face must drive no model")
		return &model.Response{}
	}}
	opts := []assembly.Option{assembly.WithGovernance(passPolicy{})}
	if storeRoot != "" {
		opts = append(opts, assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: storeRoot}))
	}
	if embedder != nil {
		opts = append(opts, assembly.WithEmbedder(embedder))
	}
	run, err := assembly.Assemble(ctx, declarationOf(declRoot), map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, opts...)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() {
		if _, err := run.Close(context.Background(), agent.CloseRequest{}); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return run
}

// evalRecallPort binds the recall port of one assembled run, so the test drives
// the same recall face the production tools read through.
func evalRecallPort(run *assembly.Run) *assembly.RecallPort {
	port := assembly.NewRecallPort(nil, nil)
	port.Bind(run)
	return port
}

// awaitEvalRecall waits until the production recall of q answers every wanted
// key, failing on a bounded wait so a vector generation that never becomes
// ready fails the case instead of hanging the run.
func awaitEvalRecall(t *testing.T, run *assembly.Run, q search.Query, want []event.EventID) search.Result {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(realAwaitLimit)
	var last []event.EventID
	for {
		result, err := evalRecallPort(run).Search(ctx, q)
		if err != nil {
			t.Fatalf("Recall(%q): %v", q.Text, err)
		}
		last = originalKeysOf(result.Refs)
		covered := true
		for _, key := range want {
			if !containsEventKey(last, key) {
				covered = false
			}
		}
		if covered {
			return result
		}
		if time.Now().After(deadline) {
			t.Fatalf("recall of %q never reached %v (last %v): the retrieval never answered the expected keys", q.Text, want, last)
		}
		time.Sleep(time.Millisecond)
	}
}

// awaitEvalVectorCount waits until the counted embedding has generated exactly
// want event vectors — the seam generates asynchronously, so the count is
// awaited on a bounded wait instead of sampled at assembly return — failing the
// case when the generation never runs to completion or runs past the corpus it
// was fed.
func awaitEvalVectorCount(t *testing.T, counter *evalCountingEmbedder, want int) int {
	t.Helper()
	deadline := time.Now().Add(realAwaitLimit)
	for {
		got := counter.count()
		if got == want {
			return got
		}
		if got > want {
			t.Fatalf("the assembly generated %d event vectors, want %d: one event embedded more than once", got, want)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the assembly generated %d event vectors in the wait, want %d: the vector generation never ran to completion", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// containsEventKey reports whether the key appears in the set.
func containsEventKey(keys []event.EventID, key event.EventID) bool {
	for _, held := range keys {
		if held == key {
			return true
		}
	}
	return false
}

// countRecovered counts how many wanted keys the answer recovered.
func countRecovered(want, got []event.EventID) int {
	recovered := 0
	for _, key := range want {
		if containsEventKey(got, key) {
			recovered++
		}
	}
	return recovered
}

// mintEvalCursor mints a continuation cursor under one retrieval text so a row
// running under another text carries it into the named refusal. The keyword leg
// mints it over the stored corpus alone — two hits under a page of one, cursor
// guaranteed — so the mint never depends on how many hits an embedding
// endpoint happens to produce.
func mintEvalCursor(t *testing.T, run *assembly.Run, partitions []string) search.Cursor {
	t.Helper()
	paged, err := evalRecallPort(run).Search(context.Background(), search.Query{Text: evalFilterText, Strategy: search.StrategyKeywordOnly, Partitions: partitions, Limit: 1})
	if err != nil {
		t.Fatalf("mint the foreign cursor: %v", err)
	}
	if paged.NextCursor == "" {
		t.Fatalf("mint the foreign cursor: keyword recall of %q answered %v, want more hits than the page holds", evalFilterText, originalKeysOf(paged.Refs))
	}
	return paged.NextCursor
}

// runEvalRow runs one evaluation row through the production recall face: the
// hybrid answer of the query beside the keyword-only baseline of the same
// query, the gain measured as expected keys the hybrid answer recovered over
// that baseline, and the verdict the row keeps. A row planned to fail is
// recorded under its named failure with no fabricated hit, never silently
// dropped from the report.
func runEvalRow(t *testing.T, run *assembly.Run, spec evalRowSpec, partitions []string) evalRow {
	t.Helper()
	ctx := context.Background()
	row := evalRow{
		Name:     spec.name,
		Query:    spec.text,
		Strategy: string(search.StrategyHybrid),
		Want:     spec.want,
	}
	start := time.Now()
	result, err := evalRecallPort(run).Search(ctx, search.Query{
		Text:       spec.text,
		Strategy:   search.StrategyHybrid,
		Cursor:     spec.cursor,
		Partitions: partitions,
	})
	row.DurationMillis = time.Since(start).Milliseconds()
	row.Pages = 1
	if spec.expectFail {
		if err == nil {
			t.Fatalf("eval row %s answered %v, want the named refusal: a failing query is never answered as a hit", spec.name, originalKeysOf(result.Refs))
		}
		row.Failure = err.Error()
		row.Verdict = evalVerdictFailed
		return row
	}
	if err != nil {
		t.Fatalf("eval row %s: %v", spec.name, err)
	}
	row.Got = originalKeysOf(result.Refs)
	row.Hits = len(row.Got)
	baseline, err := evalRecallPort(run).Search(ctx, search.Query{
		Text:       spec.text,
		Strategy:   search.StrategyKeywordOnly,
		Partitions: partitions,
	})
	if err != nil {
		t.Fatalf("eval row %s baseline: %v", spec.name, err)
	}
	row.BaselineGot = originalKeysOf(baseline.Refs)
	row.Gain = countRecovered(spec.want, row.Got) - countRecovered(spec.want, row.BaselineGot)
	if row.Gain > 0 {
		row.Verdict = evalVerdictGain
	} else {
		row.Verdict = evalVerdictZeroGain
	}
	return row
}

// buildEvalReport tallies the rows into the report: every row counted under
// the verdict it kept, exactly once, so the totals never read healthier than
// the rows they summarize.
func buildEvalReport(rows []evalRow) evalReport {
	report := evalReport{Rows: rows}
	for _, row := range rows {
		report.Totals.Rows++
		switch row.Verdict {
		case evalVerdictGain:
			report.Totals.Gain++
		case evalVerdictZeroGain:
			report.Totals.ZeroGain++
		case evalVerdictFailed:
			report.Totals.Failed++
		}
		report.Totals.Hits += row.Hits
		report.Totals.DurationMillis += row.DurationMillis
	}
	return report
}

// writeEvalReport writes the report to the run record directory outside the
// repository and logs its path and content beside it, so the evidence is
// readable both from the artifact and from the run log.
func writeEvalReport(t *testing.T, report evalReport) string {
	t.Helper()
	dir, err := os.MkdirTemp("", evalReportDirPattern)
	if err != nil {
		t.Fatalf("create the run record directory: %v", err)
	}
	path := filepath.Join(dir, "recall-eval.json")
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal the recall evaluation report: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write the recall evaluation report: %v", err)
	}
	t.Logf("recall evaluation report: %s\n%s", path, string(payload))
	return path
}

// TestRecallEvaluation drives the recall quality and cost evaluation over the production retrieval path of the formal assembly.
//   - 检索路径生产接线: the formal entry consumes StoreQuerySource/StoreSearcher and answers the vector leg, the narrowed query face and paged continuations
//   - 负锚: a run beside no storage refuses recall by name and an assembly beside no embedding answers through the keyword leg alone with zero embedding calls
//   - 评测语义保留零增益与失败: the report keeps its zero-gain and failed conclusion rows as they were earned, never upgraded or dropped
//   - 真实检索资源与启用门: the real embedding endpoint is resource-gated — absent is skipped, half-configured is refused, provisioned carries the same evaluation
//
// 契约: docs/wiki/memory/memory-architecture.md#inmemory-retrieval
func TestRecallEvaluation(t *testing.T) {
	ctx := context.Background()

	t.Run("检索路径生产接线: 正式入口消费 StoreQuerySource/StoreSearcher", func(t *testing.T) {
		storeRoot := t.TempDir()
		tier := seedEvalCorpus(t, storeRoot)
		run := evalAssemble(t, storeRoot, memoryembedder.NewHashEmbedder(0))
		alpha := []string{realAlphaPartition}

		gain := search.Query{Text: realRecallText, Strategy: search.StrategyHybrid, Partitions: alpha, Limit: 20}
		recalled := awaitEvalRecall(t, run, gain, []event.EventID{"0a01", "0a02", "0a03"})
		if want := []event.EventID{"0a01", "0a02", "0a03"}; !reflect.DeepEqual(originalKeysOf(recalled.Refs), want) {
			t.Fatalf("production hybrid recall keys = %v, want %v: the vector leg recalls the context first, then the weaker fingerprint neighbours", originalKeysOf(recalled.Refs), want)
		}

		for _, ref := range recalled.Refs {
			if ref.Owner != realOwnerOf() || ref.Format != realEventFormat || ref.RetentionPolicy != realEventRetention {
				t.Fatalf("recall reference of %s = %+v, want the reference its body was landed with", ref.Key, ref)
			}
		}
		scoped, ok := tier.Originals.(ledger.ScopedOriginalStore)
		if !ok {
			t.Fatalf("the real file tier serves no scoped read face: %T", tier.Originals)
		}
		exact, _ := scopedReadBack(t, scoped, ledger.ReadScope{
			Owner:           realOwnerOf(),
			Format:          realEventFormat,
			RetentionPolicy: realEventRetention,
		}, "0a01")
		if want := []byte(realCorpusOf()[0].original); !reflect.DeepEqual(exact, want) {
			t.Fatalf("scoped recall read %q, want the committed original byte for byte", exact)
		}

		unfiltered, err := evalRecallPort(run).Search(ctx, search.Query{Text: evalFilterText, Strategy: search.StrategyKeywordOnly, Partitions: alpha})
		if err != nil {
			t.Fatalf("Recall(%q): %v", evalFilterText, err)
		}
		if want := []event.EventID{"0a03", "0a02"}; !reflect.DeepEqual(originalKeysOf(unfiltered.Refs), want) {
			t.Fatalf("keyword recall keys = %v, want %v (newest first): the query face orders exactly as the read face states", originalKeysOf(unfiltered.Refs), want)
		}
		typed, err := evalRecallPort(run).Search(ctx, search.Query{Text: evalFilterText, Strategy: search.StrategyKeywordOnly, Types: []string{event.TypeAgentOutput}, Partitions: alpha})
		if err != nil {
			t.Fatalf("Recall(typed): %v", err)
		}
		if want := []event.EventID{"0a02"}; !reflect.DeepEqual(originalKeysOf(typed.Refs), want) {
			t.Fatalf("typed recall keys = %v, want %v: the frozen type filter narrows the query face", originalKeysOf(typed.Refs), want)
		}

		if containsEventKey(originalKeysOf(recalled.Refs), "0b05") {
			t.Fatalf("alpha recall carried 0b05, want none: fusion never widens a query past its authorized partitions")
		}
		wider := awaitEvalRecall(t, run, search.Query{Text: realRecallText, Strategy: search.StrategyHybrid, Partitions: []string{realAlphaPartition, realBetaPartition}, Limit: 20}, []event.EventID{"0b05"})
		if !containsEventKey(originalKeysOf(wider.Refs), "0b05") {
			t.Fatalf("both-partition recall keys = %v, want 0b05 among them once its partition is authorized", originalKeysOf(wider.Refs))
		}

		first, err := evalRecallPort(run).Search(ctx, search.Query{Text: realRecallText, Strategy: search.StrategyHybrid, Partitions: alpha, Limit: 2})
		if err != nil {
			t.Fatalf("Recall(page 1): %v", err)
		}
		if want := []event.EventID{"0a01", "0a02"}; !reflect.DeepEqual(originalKeysOf(first.Refs), want) || first.NextCursor == "" {
			t.Fatalf("page 1 = (%v, cursor %q), want %v beside a continuation", originalKeysOf(first.Refs), first.NextCursor, want)
		}
		second, err := evalRecallPort(run).Search(ctx, search.Query{Text: realRecallText, Strategy: search.StrategyHybrid, Cursor: first.NextCursor, Partitions: alpha, Limit: 2})
		if err != nil {
			t.Fatalf("Recall(page 2): %v", err)
		}
		if want := []event.EventID{"0a03"}; !reflect.DeepEqual(originalKeysOf(second.Refs), want) || second.NextCursor != "" {
			t.Fatalf("page 2 = (%v, cursor %q), want %v as the last page", originalKeysOf(second.Refs), second.NextCursor, want)
		}

		_, err = evalRecallPort(run).Search(ctx, search.Query{Text: evalKeywordText, Strategy: search.StrategyHybrid, Cursor: first.NextCursor, Partitions: alpha})
		if !errors.Is(err, search.ErrCursorUnbound) {
			t.Fatalf("foreign-cursor recall = %v, want the named refusal ErrCursorUnbound", err)
		}
	})

	t.Run("负锚: 无存储具名拒绝与无 embedding 零行为变化", func(t *testing.T) {
		bare := evalAssemble(t, "", nil)
		if _, err := evalRecallPort(bare).Search(ctx, search.Query{Text: realRecallText, Strategy: search.StrategyHybrid, Partitions: []string{realAlphaPartition}}); !errors.Is(err, assembly.ErrRetrievalUnwired) {
			t.Fatalf("recall beside no storage = %v, want the named refusal ErrRetrievalUnwired", err)
		}

		storeRoot := t.TempDir()
		seedEvalCorpus(t, storeRoot)
		plain := evalAssemble(t, storeRoot, nil)
		vectorOnly, err := evalRecallPort(plain).Search(ctx, search.Query{Text: realRecallText, Strategy: search.StrategyHybrid, Partitions: []string{realAlphaPartition}, Limit: 20})
		if err != nil {
			t.Fatalf("Recall(no embedder): %v", err)
		}
		if len(vectorOnly.Refs) != 0 {
			t.Fatalf("unconfigured recall answered %v, want none: without an embedding there is no vector leg to speak", originalKeysOf(vectorOnly.Refs))
		}
		keyword, err := evalRecallPort(plain).Search(ctx, search.Query{Text: evalKeywordText, Strategy: search.StrategyKeywordOnly, Partitions: []string{realAlphaPartition}})
		if err != nil {
			t.Fatalf("Recall(keyword, no embedder): %v", err)
		}
		if want := []event.EventID{"0a02"}; !reflect.DeepEqual(originalKeysOf(keyword.Refs), want) {
			t.Fatalf("keyword recall beside no embedding = %v, want %v: the keyword leg is unchanged", originalKeysOf(keyword.Refs), want)
		}

		counter := &evalCountingEmbedder{inner: memoryembedder.NewHashEmbedder(0)}
		counted := evalAssemble(t, storeRoot, counter)
		generated := awaitEvalVectorCount(t, counter, len(realCorpusOf()))
		if _, err := evalRecallPort(counted).Search(ctx, search.Query{Text: evalFilterText, Strategy: search.StrategyKeywordOnly, Partitions: []string{realAlphaPartition}}); err != nil {
			t.Fatalf("Recall(keyword-only): %v", err)
		}
		if got := counter.count(); got != generated {
			t.Fatalf("keyword-only recall made %d embedding calls, want %d: the keyword leg never reaches the embedding", got-generated, 0)
		}
		if _, err := evalRecallPort(counted).Search(ctx, search.Query{Text: realRecallText, Strategy: search.StrategyHybrid, Partitions: []string{realAlphaPartition}, Limit: 20}); err != nil {
			t.Fatalf("Recall(hybrid): %v", err)
		}
		if got := counter.count(); got <= generated {
			t.Fatalf("hybrid recall made %d embedding calls, want at least one: the vector leg embeds the retrieval text", got-generated)
		}
	})

	t.Run("评测语义保留零增益与失败: 不美化", func(t *testing.T) {
		storeRoot := t.TempDir()
		seedEvalCorpus(t, storeRoot)
		run := evalAssemble(t, storeRoot, memoryembedder.NewHashEmbedder(0))
		alpha := []string{realAlphaPartition}
		gain := search.Query{Text: realRecallText, Strategy: search.StrategyHybrid, Partitions: alpha, Limit: 20}
		awaitEvalRecall(t, run, gain, []event.EventID{"0a01"})

		foreign := mintEvalCursor(t, run, alpha)
		specs := []evalRowSpec{
			{name: "vector leg gain", text: realRecallText, want: []event.EventID{"0a01"}},
			{name: "keyword baseline parity", text: evalKeywordText, want: []event.EventID{"0a02"}},
			{name: "foreign cursor refusal", text: evalKeywordText, want: []event.EventID{"0a02"}, cursor: foreign, expectFail: true},
		}
		rows := make([]evalRow, 0, len(specs))
		for _, spec := range specs {
			rows = append(rows, runEvalRow(t, run, spec, alpha))
		}
		report := buildEvalReport(rows)

		if len(report.Rows) != 3 {
			t.Fatalf("report holds %d rows, want 3: a zero-gain and a failed conclusion are never dropped", len(report.Rows))
		}
		gainRow, zeroRow, failedRow := report.Rows[0], report.Rows[1], report.Rows[2]
		if gainRow.Verdict != evalVerdictGain || gainRow.Gain <= 0 {
			t.Fatalf("gain row = (verdict %q, gain %d), want the vector leg's recovery recorded as a gain", gainRow.Verdict, gainRow.Gain)
		}
		if !containsEventKey(gainRow.Got, "0a01") || containsEventKey(gainRow.BaselineGot, "0a01") {
			t.Fatalf("gain row got %v over baseline %v, want 0a01 recalled only by the hybrid answer", gainRow.Got, gainRow.BaselineGot)
		}
		if zeroRow.Verdict != evalVerdictZeroGain || zeroRow.Gain != 0 || zeroRow.Failure != "" {
			t.Fatalf("zero-gain row = (verdict %q, gain %d, failure %q), want the honest zero gain kept un-upgraded", zeroRow.Verdict, zeroRow.Gain, zeroRow.Failure)
		}
		if !containsEventKey(zeroRow.Got, "0a02") || !containsEventKey(zeroRow.BaselineGot, "0a02") {
			t.Fatalf("zero-gain row got %v over baseline %v, want the expected key recovered on both legs", zeroRow.Got, zeroRow.BaselineGot)
		}
		if failedRow.Verdict != evalVerdictFailed || failedRow.Failure == "" || !strings.Contains(failedRow.Failure, "cursor") {
			t.Fatalf("failed row = (verdict %q, failure %q), want the named cursor refusal preserved", failedRow.Verdict, failedRow.Failure)
		}
		if len(failedRow.Got) != 0 {
			t.Fatalf("failed row answered %v, want no fabricated hit beside a failure", failedRow.Got)
		}

		if report.Totals.Rows != len(report.Rows) || report.Totals.Gain != 1 || report.Totals.ZeroGain != 1 || report.Totals.Failed != 1 {
			t.Fatalf("totals = %+v, want one row per verdict over %d rows", report.Totals, len(report.Rows))
		}
		wantHits := gainRow.Hits + zeroRow.Hits + failedRow.Hits
		if report.Totals.Hits != wantHits {
			t.Fatalf("totals hits = %d, want the honest sum %d", report.Totals.Hits, wantHits)
		}

		path := writeEvalReport(t, report)
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read back the report: %v", err)
		}
		var restored evalReport
		if err := json.Unmarshal(payload, &restored); err != nil {
			t.Fatalf("decode the report: %v", err)
		}
		if !reflect.DeepEqual(restored, report) {
			t.Fatalf("report artifact = %+v, want the written conclusion verbatim", restored)
		}
	})

	t.Run("真实检索资源与启用门: R24 三态", func(t *testing.T) {
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
			t.Skip("real embedding endpoint not provisioned (resource gate R24, 0/3): set TAGENT_REAL_EMBEDDING_BASE_URL, TAGENT_REAL_EMBEDDING_API_KEY and TAGENT_REAL_EMBEDDING_MODEL — 合法 Skip 注明:本项不以 Skip 结项,确定性 HashEmbedder 承载的检索与评测已在其余子测试实跑")
		}
		if provisioned != 3 {
			t.Fatalf("the real retrieval gate is half-configured (%d of 3): a partial endpoint is refused instead of silently answered under the deterministic stand-in", provisioned)
		}
		embedder := &realEmbeddingEndpoint{base: base, key: key, model: modelName}
		storeRoot := t.TempDir()
		seedEvalCorpus(t, storeRoot)
		run := evalAssemble(t, storeRoot, embedder)
		alpha := []string{realAlphaPartition}
		gain := search.Query{Text: realRecallText, Strategy: search.StrategyHybrid, Partitions: alpha, Limit: 20}
		recalled := awaitEvalRecall(t, run, gain, []event.EventID{"0a01"})
		if len(recalled.Refs) == 0 {
			t.Fatal("the provisioned embedding endpoint recalled nothing for the retrieval text: an empty answer names a zero-output endpoint")
		}

		foreign := mintEvalCursor(t, run, alpha)
		specs := []evalRowSpec{
			{name: "vector leg gain", text: realRecallText, want: []event.EventID{"0a01"}},
			{name: "keyword baseline parity", text: evalKeywordText, want: []event.EventID{"0a02"}},
			{name: "foreign cursor refusal", text: evalKeywordText, want: []event.EventID{"0a02"}, cursor: foreign, expectFail: true},
		}
		rows := make([]evalRow, 0, len(specs))
		for _, spec := range specs {
			rows = append(rows, runEvalRow(t, run, spec, alpha))
		}
		report := buildEvalReport(rows)
		if len(report.Rows) != 3 || report.Totals.Rows != 3 {
			t.Fatalf("real-path report holds %d rows tallying %d, want 3 preserved rows", len(report.Rows), report.Totals.Rows)
		}
		if zeroRow := report.Rows[1]; zeroRow.Verdict != evalVerdictZeroGain || zeroRow.Gain != 0 {
			t.Fatalf("real-path zero-gain row = (verdict %q, gain %d), want the zero gain preserved on the real path too", zeroRow.Verdict, zeroRow.Gain)
		}
		if failedRow := report.Rows[2]; failedRow.Verdict != evalVerdictFailed || !strings.Contains(failedRow.Failure, "cursor") {
			t.Fatalf("real-path failed row = (verdict %q, failure %q), want the named refusal preserved on the real path too", failedRow.Verdict, failedRow.Failure)
		}
		writeEvalReport(t, report)
	})
}
