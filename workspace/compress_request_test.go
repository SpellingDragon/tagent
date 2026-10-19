package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestCompressionRequestIsAFact pins the tool-result judgment face: an over-threshold result files the request, and the preview still answers the same call.
// - an over-threshold result files one request naming who judged, why and the original it judged
// - the preview and the kept original answer the same call, so the judgment waits for no consumer
// - a result inside the bound, a nil result and a tool error file nothing
// - a repeated judgment over the same body keeps the request already on record
// - a filing that failed is reported beside the preview the kept original answers
// - a tool with no filing wired judges nothing and answers as it did before
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestCompressionRequestIsAFact(t *testing.T) {
	ctx := context.Background()
	declaration := &trpctool.Declaration{Name: "shell", Description: "run a command"}
	scope := invariantResultScope()
	result := strings.Repeat("oversized output ", 10)
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("serialize the oversized result: %v", err)
	}

	t.Run("an over-threshold result files the request it judged", func(t *testing.T) {
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		requests := memory.NewCompressRequests()
		bounded := invariantBoundedTool(t, invariantFakeTool{decl: declaration, result: result},
			invariantExternalizer(t, store, WithMaxTokens(2)), scope, requests)

		got, err := bounded.Call(ctx, []byte(`{}`))
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		preview, ok := got.(string)
		if !ok || !strings.Contains(preview, "externalized result:") {
			t.Fatalf("result = %v, want the preview of the kept original", got)
		}
		ref := OriginalRefFor(scope, body)
		if recalled := store.GetScoped(ctx, ref.Key, invariantScope()); recalled.Outcome != memory.RecallFound {
			t.Fatalf("recall of the judged original = %v, want it kept by the same call", recalled.Outcome)
		}

		pending, err := requests.Pending(ctx)
		if err != nil {
			t.Fatalf("read the filed requests: %v", err)
		}
		if len(pending) != 1 {
			t.Fatalf("filed requests = %+v, want exactly the one judgment", pending)
		}
		request := pending[0]
		if request.Producer != CompressProducerToolOverflow || request.Reason != CompressReasonToolOverflow {
			t.Fatalf("request = %+v, want the tool-overflow producer and reason", request)
		}
		if request.Subject != string(ref.Key) {
			t.Fatalf("request subject = %q, want the identity of the judged original %q", request.Subject, ref.Key)
		}
		if request.FactType() != memory.CompressRequestType || request.FactKey() != string(request.ID) {
			t.Fatalf("request fact = (%q, %q), want its identity read as %q", request.FactKey(), request.FactType(), memory.CompressRequestType)
		}
		if request.At.IsZero() {
			t.Fatal("the request carries no filing moment, want the moment it was filed")
		}
	})

	t.Run("a result inside the bound files nothing", func(t *testing.T) {
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		requests := memory.NewCompressRequests()
		bounded := invariantBoundedTool(t, invariantFakeTool{decl: declaration, result: "ok"},
			invariantExternalizer(t, store, WithMaxTokens(2)), scope, requests)
		if got, err := bounded.Call(ctx, nil); err != nil || got != "ok" {
			t.Fatalf("call inside the bound = (%v, %v), want the tool's own result", got, err)
		}
		if pending, err := requests.Pending(ctx); err != nil || len(pending) != 0 {
			t.Fatalf("filed requests = (%v, %v), want none inside the bound", pending, err)
		}
	})

	t.Run("a repeated judgment keeps the request on record", func(t *testing.T) {
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		requests := memory.NewCompressRequests()
		bounded := invariantBoundedTool(t, invariantFakeTool{decl: declaration, result: result},
			invariantExternalizer(t, store, WithMaxTokens(2)), scope, requests)
		for round := 0; round < 3; round++ {
			if _, err := bounded.Call(ctx, nil); err != nil {
				t.Fatalf("call %d: %v", round, err)
			}
		}
		pending, err := requests.Pending(ctx)
		if err != nil || len(pending) != 1 {
			t.Fatalf("filed requests after three judgments = (%v, %v), want the one on record", pending, err)
		}
	})

	t.Run("a filing that failed is reported beside the preview", func(t *testing.T) {
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		refused := errors.New("the request log is unreachable")
		bounded := invariantBoundedTool(t, invariantFakeTool{decl: declaration, result: result},
			invariantExternalizer(t, store, WithMaxTokens(2)), scope, refused)

		got, err := bounded.Call(ctx, nil)
		if !errors.Is(err, ErrCompressRequestUnfiled) {
			t.Fatalf("call with a failing filing = %v, want %v", err, ErrCompressRequestUnfiled)
		}
		preview, ok := got.(string)
		if !ok || !strings.Contains(preview, "externalized result:") {
			t.Fatalf("result = %v, want the preview the kept original still answers", got)
		}
		ref := OriginalRefFor(scope, body)
		if recalled := store.GetScoped(ctx, ref.Key, invariantScope()); recalled.Outcome != memory.RecallFound {
			t.Fatalf("recall of the judged original = %v, want it kept despite the unfiled judgment", recalled.Outcome)
		}
	})

	t.Run("a tool with no filing wired judges nothing", func(t *testing.T) {
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		bounded, err := NewBoundedTool(invariantFakeTool{decl: declaration, result: result},
			invariantExternalizer(t, store, WithMaxTokens(2)), scope)
		if err != nil {
			t.Fatalf("assemble the bounded tool: %v", err)
		}
		if _, err := bounded.Call(ctx, nil); err != nil {
			t.Fatalf("call without a filing: %v", err)
		}
		if store.commits() != 1 {
			t.Fatalf("commits = %d, want the oversized result still externalized", store.commits())
		}
	})
}

// invariantBoundedTool assembles the bounded tool over one judgment filing: the
// request log when a log is named, or a filing that always fails with the given
// cause.
func invariantBoundedTool(t *testing.T, inner trpctool.CallableTool, externalizer *ResultExternalizer, scope ResultScope, filing any) *BoundedTool {
	t.Helper()
	var file memory.CompressRequestFiling
	switch named := filing.(type) {
	case *memory.CompressRequests:
		file = named.Produce
	case error:
		file = func(context.Context, memory.CompressRequest) (memory.CompressRequest, error) {
			return memory.CompressRequest{}, named
		}
	default:
		t.Fatalf("unsupported filing %T", filing)
	}
	bounded, err := NewBoundedTool(inner, externalizer, scope, WithCompressRequestFiling(file))
	if err != nil {
		t.Fatalf("assemble the bounded tool: %v", err)
	}
	return bounded
}
