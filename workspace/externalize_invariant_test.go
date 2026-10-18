package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// invariantCountingStore counts the original commits one externalize path makes.
type invariantCountingStore struct {
	*memory.MemoryOriginalStore
	mu     sync.Mutex
	puts   int
	putErr error
}

// Put records the commit and reports the configured failure.
func (s *invariantCountingStore) Put(ctx context.Context, ref memory.OriginalRef, body []byte) error {
	s.mu.Lock()
	s.puts++
	failure := s.putErr
	s.mu.Unlock()
	if failure != nil {
		return failure
	}
	return s.MemoryOriginalStore.Put(ctx, ref, body)
}

// commits reports how many original commits were attempted.
func (s *invariantCountingStore) commits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

// invariantResultScope is the result scope oversized results are committed under.
func invariantResultScope() ResultScope {
	return ResultScope{
		Owner:           event.Address{AgentID: "agent-a", SessionID: "session-1"},
		Format:          FormatToolResult,
		RetentionPolicy: "keep",
	}
}

// invariantExternalizer builds one externalize path over the given store.
func invariantExternalizer(t *testing.T, originals memory.OriginalStore, options ...ExternalizerOption) *ResultExternalizer {
	t.Helper()
	externalizer, err := NewResultExternalizer(originals, options...)
	if err != nil {
		t.Fatalf("assemble the externalizer: %v", err)
	}
	return externalizer
}

// invariantFakeTool is one native callable tool answering a fixed result.
type invariantFakeTool struct {
	decl   *trpctool.Declaration
	result any
	err    error
}

// Declaration answers the declaration the tool was built with.
func (t invariantFakeTool) Declaration() *trpctool.Declaration { return t.decl }

// Call answers the fixed result the tool was built with.
func (t invariantFakeTool) Call(context.Context, []byte) (any, error) { return t.result, t.err }

// invariantPreviewHead reports the leading run of a preview before its ticket.
func invariantPreviewHead(preview string) string {
	head, _, _ := strings.Cut(preview, "\n[externalized result:")
	return head
}

// TestExternalizeThresholdInvariants pins the one bound every producer decides on.
// - the threshold is half the model window in tokens times four characters and never unbounded
// - a window at or below zero is read as the default window
// - only a character count strictly above the threshold takes the externalize path
// - the preview keeps at most the named runes and is narrowed to a quarter of the threshold
// - a preview is cut at a rune boundary whatever the body holds
// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
func TestExternalizeThresholdInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("the threshold follows the one formula", func(t *testing.T) {
		cases := []struct {
			maxTokens int
			want      int
		}{
			{maxTokens: 8000, want: 16000},
			{maxTokens: 100, want: 200},
			{maxTokens: 3, want: 4},
			{maxTokens: 1, want: 0},
			{maxTokens: 0, want: ThresholdForMaxTokens(DefaultMaxTokens)},
			{maxTokens: -5, want: ThresholdForMaxTokens(DefaultMaxTokens)},
		}
		for _, tc := range cases {
			if got := ThresholdForMaxTokens(tc.maxTokens); got != tc.want {
				t.Fatalf("ThresholdForMaxTokens(%d) = %d, want %d", tc.maxTokens, got, tc.want)
			}
		}
		if CharsPerToken != 4 {
			t.Fatalf("CharsPerToken = %d, want the one character estimate", CharsPerToken)
		}
		if DefaultMaxTokens != 8000 {
			t.Fatalf("DefaultMaxTokens = %d, want the declared fallback", DefaultMaxTokens)
		}
	})

	t.Run("only a count above the threshold is externalized", func(t *testing.T) {
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		externalizer := invariantExternalizer(t, store, WithMaxTokens(100))
		if externalizer.Threshold() != 200 {
			t.Fatalf("threshold = %d, want 200", externalizer.Threshold())
		}
		if externalizer.Exceeds(200) {
			t.Fatalf("Exceeds(threshold) = true, want a result at the bound left alone")
		}
		if !externalizer.Exceeds(201) {
			t.Fatalf("Exceeds(threshold+1) = false, want the overflow externalized")
		}
		if externalizer.Exceeds(0) {
			t.Fatalf("Exceeds(0) = true, want an empty result left alone")
		}
		decide, err := externalizer.DecideExternalize(ctx, "event-1", 201)
		if err != nil || !decide {
			t.Fatalf("decide = (%v, %v), want the same bound on the decision face", decide, err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := externalizer.DecideExternalize(cancelled, "event-1", 201); !errors.Is(err, context.Canceled) {
			t.Fatalf("decide under a cancelled context = %v, want the cancellation", err)
		}
	})

	t.Run("the preview is bounded by the named runes and the threshold quarter", func(t *testing.T) {
		store := memory.NewMemoryOriginalStore()
		body := []byte(strings.Repeat("界", 300))

		narrow := invariantExternalizer(t, store, WithMaxTokens(40), WithPreviewRunes(10))
		ref := OriginalRefFor(invariantResultScope(), body)
		externalized, err := narrow.Externalize(ctx, ref, body)
		if err != nil {
			t.Fatalf("externalize beside a narrow preview: %v", err)
		}
		head := invariantPreviewHead(externalized.Preview)
		if head != strings.Repeat("界", 10) {
			t.Fatalf("preview head = %q, want the ten leading runes", head)
		}
		if runes := []rune(head); len(runes) != 10 {
			t.Fatalf("preview head runes = %d, want exactly the ten leading runes", len(runes))
		}

		quarter := invariantExternalizer(t, store, WithMaxTokens(200))
		externalized, err = quarter.Externalize(ctx, ref, body)
		if err != nil {
			t.Fatalf("externalize beside the quarter bound: %v", err)
		}
		head = invariantPreviewHead(externalized.Preview)
		if runes := []rune(head); len(runes) != 100 {
			t.Fatalf("preview head runes = %d, want the threshold quarter of 100", len(runes))
		}
		if !strings.Contains(externalized.Preview, fmt.Sprintf("%d bytes in total", len(body))) {
			t.Fatalf("preview = %q, want the whole binary size named in its ticket", externalized.Preview)
		}
		if !strings.Contains(externalized.Preview, string(ref.Key)) {
			t.Fatalf("preview = %q, want the committed original named", externalized.Preview)
		}
	})

	t.Run("a missing original store is refused", func(t *testing.T) {
		if _, err := NewResultExternalizer(nil); !errors.Is(err, ErrOriginalStoreRequired) {
			t.Fatalf("assembling without a store = %v, want %v", err, ErrOriginalStoreRequired)
		}
	})
}

// TestExternalizeOrderInvariants pins the commit order one oversized result takes.
// - the original is committed before any preview exists, and a failed commit answers no preview
// - the segment entry names a byte offset and a byte bound instead of a line count
// - a reference outside the materializer scope is refused before any original is committed
// - a replica write failure keeps the effective original and answers its preview beside the error
// - two scopes of one body share the content key while their reference metadata stays apart
// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
func TestExternalizeOrderInvariants(t *testing.T) {
	ctx := context.Background()
	scope := invariantScope()
	resultScope := invariantResultScope()

	t.Run("the original is committed before any preview exists", func(t *testing.T) {
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		externalizer := invariantExternalizer(t, store)
		body := []byte("a result body below the default preview bound")
		ref := OriginalRefFor(resultScope, body)

		externalized, err := externalizer.Externalize(ctx, ref, body)
		if err != nil {
			t.Fatalf("externalize: %v", err)
		}
		if externalized.Original != ref {
			t.Fatalf("original = %+v, want the committed reference", externalized.Original)
		}
		if store.commits() != 1 {
			t.Fatalf("commits = %d, want the original committed exactly once", store.commits())
		}
		if recalled := store.GetScoped(ctx, ref.Key, scope); recalled.Outcome != memory.RecallFound {
			t.Fatalf("recall = %v, want the original committed before the preview answered", recalled.Outcome)
		}
		if externalized.Segment.From != (memory.SegmentPosition{}) {
			t.Fatalf("segment start = %+v, want the first byte", externalized.Segment.From)
		}
		if externalized.Segment.MaxBytes != memory.DefaultSegmentMaxBytes {
			t.Fatalf("segment bound = %d, want %d bytes", externalized.Segment.MaxBytes, memory.DefaultSegmentMaxBytes)
		}
		if !strings.Contains(externalized.Preview, string(body)) {
			t.Fatalf("preview = %q, want the short body presented whole", externalized.Preview)
		}
	})

	t.Run("a failed commit answers no preview", func(t *testing.T) {
		refused := errors.New("the original store refused the bytes")
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore(), putErr: refused}
		externalizer := invariantExternalizer(t, store)
		body := []byte("bytes that were never committed")
		externalized, err := externalizer.Externalize(ctx, OriginalRefFor(resultScope, body), body)
		if !errors.Is(err, refused) {
			t.Fatalf("externalize = %v, want the commit failure", err)
		}
		if externalized != (Externalized{}) {
			t.Fatalf("externalized = %+v, want no preview beside an original that was never kept", externalized)
		}
		if store.commits() != 1 {
			t.Fatalf("commits = %d, want the failed commit attempted once", store.commits())
		}
	})

	t.Run("a reference outside the materializer scope is refused before the commit", func(t *testing.T) {
		root := t.TempDir()
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		materializer, err := NewMaterializer(root, store, scope)
		if err != nil {
			t.Fatalf("assemble the materializer: %v", err)
		}
		externalizer := invariantExternalizer(t, store, WithResultMaterializer(materializer))
		outside := OriginalRefFor(ResultScope{
			Owner:           event.Address{AgentID: "agent-b", SessionID: "session-9"},
			Format:          FormatToolResult,
			RetentionPolicy: "keep",
		}, []byte("body"))
		if _, err := externalizer.Externalize(ctx, outside, []byte("body")); !errors.Is(err, ErrReplicaUnauthorized) {
			t.Fatalf("unauthorized externalize = %v, want %v", err, ErrReplicaUnauthorized)
		}
		if store.commits() != 0 {
			t.Fatalf("commits = %d, want the scope refused before any original is kept", store.commits())
		}
		if files := invariantReplicaFiles(t, root); len(files) != 0 {
			t.Fatalf("replica files = %v, want nothing written", files)
		}
	})

	t.Run("a replica write failure keeps the original and its preview", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "replica")); err != nil {
			t.Fatalf("plant the linked parent: %v", err)
		}
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		materializer, err := NewMaterializer(root, store, scope)
		if err != nil {
			t.Fatalf("assemble the materializer: %v", err)
		}
		externalizer := invariantExternalizer(t, store, WithResultMaterializer(materializer))
		body := []byte("bytes whose replica cannot be written")
		ref := OriginalRefFor(resultScope, body)

		externalized, err := externalizer.Externalize(ctx, ref, body)
		if !errors.Is(err, ErrReplicaWriteFailed) {
			t.Fatalf("externalize = %v, want %v", err, ErrReplicaWriteFailed)
		}
		if !errors.Is(err, ErrReplicaParentLinked) {
			t.Fatalf("externalize = %v, want the refusal that caused the failed replica named", err)
		}
		if externalized.Original != ref || externalized.Preview == "" {
			t.Fatalf("externalized = %+v, want the effective reference and its preview kept beside the failure", externalized)
		}
		if recalled := store.GetScoped(ctx, ref.Key, scope); recalled.Outcome != memory.RecallFound {
			t.Fatalf("recall = %v, want the original still effective after the replica failure", recalled.Outcome)
		}
		entries, err := os.ReadDir(outside)
		if err != nil {
			t.Fatalf("read the link target: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("link target entries = %v, want no replica written through the link", entries)
		}
	})

	t.Run("one body keeps one content key across scopes", func(t *testing.T) {
		body := []byte("the very same bytes")
		first := OriginalRefFor(resultScope, body)
		other := resultScope
		other.Format = FormatTaskResult
		other.RetentionPolicy = "short"
		second := OriginalRefFor(other, body)
		if first.Key != second.Key {
			t.Fatalf("keys = (%q, %q), want one content identity for one body", first.Key, second.Key)
		}
		if first.Format == second.Format || first.RetentionPolicy == second.RetentionPolicy {
			t.Fatalf("references = (%+v, %+v), want their metadata kept apart", first, second)
		}
		digest := sha256.Sum256(body)
		if first.Key != event.EventID(hex.EncodeToString(digest[:])) {
			t.Fatalf("key = %q, want the SHA-256 digest of the body", first.Key)
		}
		if _, err := hex.DecodeString(string(first.Key)); err != nil || len(first.Key) != 64 {
			t.Fatalf("key = %q, want a 64 character hex digest: %v", first.Key, err)
		}
		if OriginalRefFor(resultScope, []byte("other bytes")).Key == first.Key {
			t.Fatalf("different bodies share one key, want the content identity kept apart")
		}
	})

	t.Run("the replica of a successful externalization answers the original", func(t *testing.T) {
		root := t.TempDir()
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		materializer, err := NewMaterializer(root, store, scope)
		if err != nil {
			t.Fatalf("assemble the materializer: %v", err)
		}
		externalizer := invariantExternalizer(t, store, WithResultMaterializer(materializer))
		body := []byte("bytes that reach their replica")
		ref := OriginalRefFor(resultScope, body)
		externalized, err := externalizer.Externalize(ctx, ref, body)
		if err != nil {
			t.Fatalf("externalize: %v", err)
		}
		path := materializer.replicaPath(ref, externalizer.consumer)
		written, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the replica: %v", err)
		}
		if string(written) != string(body) {
			t.Fatalf("replica = %q, want the original bytes", written)
		}
		if externalized.Original != ref {
			t.Fatalf("original = %+v, want %+v", externalized.Original, ref)
		}
	})
}

// TestBoundedToolInvariants pins the native wrapper that routes an oversized result.
// - a result at or below the threshold and a nil result reach the model unchanged
// - a result that cannot be serialized is handed back unchanged because no size bounds it
// - an oversized result is committed and answered with the preview of the kept original
// - a tool error passes through without committing anything
// - a replica write failure answers the preview beside the named failure
// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
func TestBoundedToolInvariants(t *testing.T) {
	ctx := context.Background()
	declaration := &trpctool.Declaration{Name: "shell", Description: "run a command"}
	scope := invariantResultScope()

	t.Run("the construction refusals are named", func(t *testing.T) {
		store := memory.NewMemoryOriginalStore()
		externalizer := invariantExternalizer(t, store)
		if _, err := NewBoundedTool(nil, externalizer, scope); !errors.Is(err, ErrToolRequired) {
			t.Fatalf("missing tool = %v, want %v", err, ErrToolRequired)
		}
		tool := invariantFakeTool{decl: declaration, result: "ok"}
		if _, err := NewBoundedTool(tool, nil, scope); !errors.Is(err, ErrOriginalStoreRequired) {
			t.Fatalf("missing externalizer = %v, want %v", err, ErrOriginalStoreRequired)
		}
		if _, err := NewBoundedTool(tool, externalizer, ResultScope{}); !errors.Is(err, ErrResultScopeRequired) {
			t.Fatalf("empty scope = %v, want %v", err, ErrResultScopeRequired)
		}
	})

	t.Run("a result inside the bound reaches the model unchanged", func(t *testing.T) {
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		externalizer := invariantExternalizer(t, store, WithMaxTokens(2))
		bounded, err := NewBoundedTool(invariantFakeTool{decl: declaration, result: "ok"}, externalizer, scope)
		if err != nil {
			t.Fatalf("assemble the bounded tool: %v", err)
		}
		if bounded.Declaration() != declaration {
			t.Fatalf("declaration = %p, want the wrapped declaration passed through", bounded.Declaration())
		}
		got, err := bounded.Call(ctx, nil)
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		if got != "ok" {
			t.Fatalf("result = %v, want the tool's own result", got)
		}
		if store.commits() != 0 {
			t.Fatalf("commits = %d, want a result at the bound never externalized", store.commits())
		}
	})

	t.Run("a nil result and an unserializable result are not intercepted", func(t *testing.T) {
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		externalizer := invariantExternalizer(t, store, WithMaxTokens(2))

		empty, err := NewBoundedTool(invariantFakeTool{decl: declaration}, externalizer, scope)
		if err != nil {
			t.Fatalf("assemble the nil-result tool: %v", err)
		}
		got, err := empty.Call(ctx, nil)
		if got != nil || err != nil {
			t.Fatalf("nil result = (%v, %v), want (nil, nil)", got, err)
		}

		channel := make(chan int)
		unserializable, err := NewBoundedTool(invariantFakeTool{decl: declaration, result: channel}, externalizer, scope)
		if err != nil {
			t.Fatalf("assemble the unserializable tool: %v", err)
		}
		got, err = unserializable.Call(ctx, nil)
		if err != nil {
			t.Fatalf("unserializable result = %v, want it handed back without an error", err)
		}
		if !reflect.DeepEqual(got, channel) {
			t.Fatalf("unserializable result = %v, want the tool's own result", got)
		}
		if store.commits() != 0 {
			t.Fatalf("commits = %d, want no original for a result no size bounds", store.commits())
		}
	})

	t.Run("an oversized result is committed and answered with its preview", func(t *testing.T) {
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		externalizer := invariantExternalizer(t, store, WithMaxTokens(2))
		result := strings.Repeat("oversized output ", 10)
		bounded, err := NewBoundedTool(invariantFakeTool{decl: declaration, result: result}, externalizer, scope)
		if err != nil {
			t.Fatalf("assemble the bounded tool: %v", err)
		}
		got, err := bounded.Call(ctx, []byte(`{}`))
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		preview, ok := got.(string)
		if !ok {
			t.Fatalf("result = %T, want the preview text", got)
		}
		if preview == result {
			t.Fatalf("result = %q, want the oversized result presented as a bounded preview", preview)
		}
		if !strings.Contains(preview, "externalized result:") {
			t.Fatalf("result = %q, want the preview ticket", preview)
		}
		body, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("serialize the result: %v", err)
		}
		ref := OriginalRefFor(scope, body)
		recalled := store.GetScoped(ctx, ref.Key, invariantScope())
		if recalled.Outcome != memory.RecallFound {
			t.Fatalf("recall = %v, want the oversized body committed as the original", recalled.Outcome)
		}
		if string(recalled.Body) != string(body) {
			t.Fatalf("original = %q, want the serialized result byte for byte", recalled.Body)
		}
	})

	t.Run("a tool error passes through without a commit", func(t *testing.T) {
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		externalizer := invariantExternalizer(t, store, WithMaxTokens(2))
		refused := errors.New("the wrapped tool failed")
		bounded, err := NewBoundedTool(invariantFakeTool{decl: declaration, result: "partial", err: refused}, externalizer, scope)
		if err != nil {
			t.Fatalf("assemble the failing tool: %v", err)
		}
		got, err := bounded.Call(ctx, nil)
		if !errors.Is(err, refused) {
			t.Fatalf("call = %v, want the tool failure passed through", err)
		}
		if got != "partial" {
			t.Fatalf("result = %v, want the wrapped result beside the failure", got)
		}
		if store.commits() != 0 {
			t.Fatalf("commits = %d, want no original committed for a failed call", store.commits())
		}
	})

	t.Run("a replica failure answers the preview beside the named failure", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "replica")); err != nil {
			t.Fatalf("plant the linked parent: %v", err)
		}
		store := &invariantCountingStore{MemoryOriginalStore: memory.NewMemoryOriginalStore()}
		materializer, err := NewMaterializer(root, store, invariantScope())
		if err != nil {
			t.Fatalf("assemble the materializer: %v", err)
		}
		externalizer := invariantExternalizer(t, store, WithMaxTokens(2), WithResultMaterializer(materializer))
		result := strings.Repeat("oversized output ", 10)
		bounded, err := NewBoundedTool(invariantFakeTool{decl: declaration, result: result}, externalizer, scope)
		if err != nil {
			t.Fatalf("assemble the bounded tool: %v", err)
		}
		got, err := bounded.Call(ctx, nil)
		if !errors.Is(err, ErrReplicaWriteFailed) {
			t.Fatalf("call = %v, want %v", err, ErrReplicaWriteFailed)
		}
		preview, ok := got.(string)
		if !ok || !strings.Contains(preview, "externalized result:") {
			t.Fatalf("result = %v, want the preview of the still effective original", got)
		}
	})
}
