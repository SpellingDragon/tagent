package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/workspace"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// largeResultTool is one native callable tool with a fixed result: the generic tool
// families and the agent-kind tools are bounded on the same path, so the wrapper is
// exercised over this one native call face rather than over a test-only protocol.
type largeResultTool struct {
	declaration *trpctool.Declaration
	result      any
	err         error
	calls       int
}

// Declaration returns the declaration this tool was built with.
func (t *largeResultTool) Declaration() *trpctool.Declaration { return t.declaration }

// Call answers with the fixed result of this tool and counts the calls it served.
func (t *largeResultTool) Call(_ context.Context, _ []byte) (any, error) {
	t.calls++
	return t.result, t.err
}

// refusingOriginalStore is an original store whose commit barrier always fails, so the
// externalize path is observed when no original could be kept at all.
type refusingOriginalStore struct {
	cause error
	puts  int
}

// Put refuses every commit with its cause.
func (s *refusingOriginalStore) Put(_ context.Context, _ ledger.OriginalRef, _ []byte) error {
	s.puts++
	return s.cause
}

// Get reports a failed read, so no refusal is read as a missing original.
func (s *refusingOriginalStore) Get(_ context.Context, key event.EventID) ledger.RecallResult {
	return ledger.RecallResult{Key: key, Outcome: ledger.RecallFailed, Err: s.cause}
}

// ReadSegment refuses the segmented read with its cause.
func (s *refusingOriginalStore) ReadSegment(context.Context, event.EventID, int, int) ([]byte, error) {
	return nil, s.cause
}

var _ ledger.OriginalStore = (*refusingOriginalStore)(nil)

// TestLargeResultRestart proves the unified externalize path on the frozen 03.8 face.
//
//   - 大结果先存原文再有界预览: the original is committed whole before the preview and entry exist
//   - 阈值与 MaxTokens 对齐: one MaxTokens/2*4 threshold, a zero window read as 8000
//   - 清副本后独立进程经原文恢复: a reopened process reads the original and rebuilds the replica
//   - nil 结果不拦截: a nil result is handed back untouched and commits nothing
//   - 原文未提交不发预览 / 副本写失败仍保留原文: both refusals are reported by name
//   - 正式链消费者锚: the assembled runtime hands the model the preview as its settled observation
//
// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
func TestLargeResultRestart(t *testing.T) {
	ctx := context.Background()
	owner := event.Address{AgentID: event.AgentID("agent-large"), SessionID: event.SessionID("session-large")}
	scope := workspace.ResultScope{Owner: owner, Format: workspace.FormatToolResult, RetentionPolicy: "keep"}
	readScope := ledger.ReadScope{Owner: owner, Format: workspace.FormatToolResult, RetentionPolicy: "keep"}

	externalizer := func(t *testing.T, originals ledger.OriginalStore, options ...workspace.ExternalizerOption) *workspace.ResultExternalizer {
		t.Helper()
		built, err := workspace.NewResultExternalizer(originals, options...)
		if err != nil {
			t.Fatalf("NewResultExternalizer: %v", err)
		}
		return built
	}
	boundedTool := func(t *testing.T, ext *workspace.ResultExternalizer, result any) *workspace.BoundedTool {
		t.Helper()
		tool, err := workspace.NewBoundedTool(&largeResultTool{
			declaration: &trpctool.Declaration{Name: "shell", Description: "run one shell command"},
			result:      result,
		}, ext, scope)
		if err != nil {
			t.Fatalf("NewBoundedTool: %v", err)
		}
		return tool
	}
	serialized := func(t *testing.T, result any) []byte {
		t.Helper()
		body, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("json.Marshal(result): %v", err)
		}
		return body
	}
	requirePreview := func(t *testing.T, got any, ref ledger.OriginalRef) string {
		t.Helper()
		preview, ok := got.(string)
		if !ok {
			t.Fatalf("bounded result = %T, want the preview string of an externalized result", got)
		}
		if !strings.Contains(preview, string(ref.Key)) {
			t.Fatalf("preview does not carry the ticket of original %s: %q", ref.Key, preview[:min(len(preview), 200)])
		}
		if !strings.Contains(preview, fmt.Sprintf("at most %d bytes from offset 0", ledger.DefaultSegmentMaxBytes)) {
			t.Fatalf("preview does not name the segment entry: %q", preview[min(len(preview), 160):])
		}
		return preview
	}
	walkSegments := func(t *testing.T, reader *workspace.ReplicaReader, ref ledger.OriginalRef, entry ledger.SegmentRequest) ([]byte, int) {
		t.Helper()
		var out []byte
		segments := 0
		req := entry
		for {
			result, err := reader.ReadSegment(ctx, ref, "externalize", req)
			if err != nil {
				t.Fatalf("ReadSegment at offset %d: %v", req.From.Offset, err)
			}
			segments++
			out = append(out, result.Body...)
			if result.EOF {
				return out, segments
			}
			req.From = result.Next
		}
	}

	t.Run("G2.1 大结果先存原文再有界预览: the original is kept whole before the preview and its entry exist", func(t *testing.T) {
		store := ledger.NewMemoryOriginalStore()
		ext := externalizer(t, store, workspace.WithMaxTokens(16000))
		big := strings.Repeat("大结果-", 12000)
		tool := boundedTool(t, ext, big)
		got, err := tool.Call(ctx, []byte(`{"command":"cat","args":["huge.log"]}`))
		if err != nil {
			t.Fatalf("Call on an oversized result = %v, want the preview of the kept original", err)
		}
		body := serialized(t, big)
		if len(body) <= ext.Threshold() {
			t.Fatalf("serialized result = %d characters, want it over the threshold %d", len(body), ext.Threshold())
		}
		ref := workspace.OriginalRefFor(scope, body)
		preview := requirePreview(t, got, ref)
		if len(preview) >= len(body) {
			t.Fatalf("preview holds %d bytes of a %d byte result, want a bounded preview", len(preview), len(body))
		}
		if head := []rune(string(body))[:1000]; !strings.HasPrefix(preview, string(head)) {
			t.Fatalf("preview does not start with the original bytes")
		}
		recalled := store.Get(ctx, ref.Key)
		if recalled.Outcome != ledger.RecallFound {
			t.Fatalf("Get(%s) = outcome %v, want the committed original", ref.Key, recalled.Outcome)
		}
		assertSameBytes(t, "Get("+string(ref.Key)+")", recalled.Body, body)
		if declaration := tool.Declaration(); declaration.Name != "shell" {
			t.Fatalf("Declaration() = %+v, want the wrapped tool declaration passed through", declaration)
		}
	})

	t.Run("G2.2 阈值与 MaxTokens 对齐: one formula decides for the wrapper and for every producer", func(t *testing.T) {
		if got := workspace.ThresholdForMaxTokens(16000); got != 32000 {
			t.Fatalf("ThresholdForMaxTokens(16000) = %d, want 16000/2*4 = 32000", got)
		}
		if got := workspace.ThresholdForMaxTokens(0); got != 16000 {
			t.Fatalf("ThresholdForMaxTokens(0) = %d, want the 8000 default window to give 16000", got)
		}
		if got := workspace.ThresholdForMaxTokens(-5); got != 16000 {
			t.Fatalf("ThresholdForMaxTokens(-5) = %d, want the default window to give 16000", got)
		}

		store := ledger.NewMemoryOriginalStore()
		ext := externalizer(t, store, workspace.WithMaxTokens(16000))
		if ext.Threshold() != 32000 {
			t.Fatalf("externalizer threshold = %d, want 32000", ext.Threshold())
		}
		atThreshold := strings.Repeat("a", 31998)
		atTool := boundedTool(t, ext, atThreshold)
		got, err := atTool.Call(ctx, []byte(`{}`))
		if err != nil {
			t.Fatalf("Call at the threshold = %v, want the result returned untouched", err)
		}
		if returned, ok := got.(string); !ok || returned != atThreshold {
			t.Fatalf("Call at the threshold returned %T, want the original string untouched", got)
		}
		if recalled := store.Get(ctx, workspace.OriginalRefFor(scope, serialized(t, atThreshold)).Key); recalled.Outcome != ledger.RecallMissing {
			t.Fatalf("a result at the threshold was externalized: outcome %v", recalled.Outcome)
		}

		overThreshold := strings.Repeat("a", 31999)
		overTool := boundedTool(t, ext, overThreshold)
		got, err = overTool.Call(ctx, []byte(`{}`))
		if err != nil {
			t.Fatalf("Call over the threshold = %v, want the preview of the kept original", err)
		}
		requirePreview(t, got, workspace.OriginalRefFor(scope, serialized(t, overThreshold)))

		defaultExt := externalizer(t, ledger.NewMemoryOriginalStore())
		if defaultExt.Threshold() != 16000 {
			t.Fatalf("default externalizer threshold = %d, want 16000", defaultExt.Threshold())
		}
		insideDefault := strings.Repeat("b", 15998)
		got, err = boundedTool(t, defaultExt, insideDefault).Call(ctx, []byte(`{}`))
		if err != nil {
			t.Fatalf("Call inside the default threshold = %v, want the result returned untouched", err)
		}
		if returned, ok := got.(string); !ok || returned != insideDefault {
			t.Fatalf("Call inside the default threshold returned %T, want the original string untouched", got)
		}
		overDefault := strings.Repeat("b", 15999)
		got, err = boundedTool(t, defaultExt, overDefault).Call(ctx, []byte(`{}`))
		if err != nil {
			t.Fatalf("Call over the default threshold = %v, want the preview of the kept original", err)
		}
		requirePreview(t, got, workspace.OriginalRefFor(scope, serialized(t, overDefault)))
	})

	t.Run("G2.3 清副本后独立进程经原文恢复: an independent reopen reads the original and materialises the replica again", func(t *testing.T) {
		root := t.TempDir()
		memoryRoot := filepath.Join(root, "memory")
		scratch := filepath.Join(root, "scratch")
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: memoryRoot}
		firstTier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("OpenTier(localfile): %v", err)
		}
		firstScoped, ok := firstTier.Originals.(ledger.ScopedOriginalStore)
		if !ok {
			t.Fatalf("localfile originals do not implement the scoped read face")
		}
		firstMaterializer, err := workspace.NewMaterializer(scratch, firstScoped, readScope)
		if err != nil {
			t.Fatalf("NewMaterializer: %v", err)
		}
		firstExt := externalizer(t, firstTier.Originals,
			workspace.WithMaxTokens(16000),
			workspace.WithResultMaterializer(firstMaterializer),
		)
		big := strings.Repeat("line-中文-", 10000)
		got, err := boundedTool(t, firstExt, big).Call(ctx, []byte(`{}`))
		if err != nil {
			t.Fatalf("Call with a materializer = %v, want the preview of the kept original", err)
		}
		body := serialized(t, big)
		ref := workspace.OriginalRefFor(scope, body)
		requirePreview(t, got, ref)

		firstReplica, err := firstMaterializer.Materialize(ctx, ref, "externalize")
		if err != nil {
			t.Fatalf("Materialize: %v", err)
		}
		replicaBytes, err := os.ReadFile(firstReplica.Path)
		if err != nil {
			t.Fatalf("read replica %s: %v", firstReplica.Path, err)
		}
		assertSameBytes(t, "replica content", replicaBytes, body)

		if err := os.RemoveAll(filepath.Join(scratch, "replica")); err != nil {
			t.Fatalf("reclaim the replica area: %v", err)
		}
		if _, err := os.Stat(firstReplica.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("replica %s still stands after reclamation: %v", firstReplica.Path, err)
		}
		if rebuildable, err := firstMaterializer.Rebuildable(ctx, ref.Key); err != nil || !rebuildable {
			t.Fatalf("Rebuildable(%s) = %v, %v, want the original to stay effective after the replica was reclaimed", ref.Key, rebuildable, err)
		}

		secondTier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen OpenTier(localfile): %v", err)
		}
		secondScoped, ok := secondTier.Originals.(ledger.ScopedOriginalStore)
		if !ok {
			t.Fatalf("reopened localfile originals do not implement the scoped read face")
		}
		secondMaterializer, err := workspace.NewMaterializer(scratch, secondScoped, readScope)
		if err != nil {
			t.Fatalf("reopen NewMaterializer: %v", err)
		}
		secondExt := externalizer(t, secondTier.Originals,
			workspace.WithMaxTokens(16000),
			workspace.WithResultMaterializer(secondMaterializer),
		)
		if secondExt.Threshold() != firstExt.Threshold() {
			t.Fatalf("reopened threshold = %d, want the same %d", secondExt.Threshold(), firstExt.Threshold())
		}
		recalled := secondTier.Originals.Get(ctx, ref.Key)
		if recalled.Outcome != ledger.RecallFound {
			t.Fatalf("reopened Get(%s) = outcome %v, want the committed original", ref.Key, recalled.Outcome)
		}
		assertSameBytes(t, "reopened Get("+string(ref.Key)+")", recalled.Body, body)

		externalized, err := secondExt.Externalize(ctx, ref, body)
		if err != nil {
			t.Fatalf("reopened Externalize of the kept original = %v", err)
		}
		recovered, segments := walkSegments(t, secondMaterializer.Reader(), externalized.Original, externalized.Segment)
		assertSameBytes(t, "reopened segmented read", recovered, body)
		if segments < 2 {
			t.Fatalf("reopened segmented read served %d segments for %d bytes, want a byte-bounded walk", segments, len(body))
		}
		rebuilt, err := secondMaterializer.Materialize(ctx, ref, "externalize")
		if err != nil {
			t.Fatalf("reopened Materialize after reclamation: %v", err)
		}
		if rebuilt.Path != firstReplica.Path {
			t.Fatalf("rebuilt replica path = %s, want the stable path %s across the restart", rebuilt.Path, firstReplica.Path)
		}
		rebuiltBytes, err := os.ReadFile(rebuilt.Path)
		if err != nil {
			t.Fatalf("read rebuilt replica %s: %v", rebuilt.Path, err)
		}
		assertSameBytes(t, "rebuilt replica content", rebuiltBytes, body)
	})

	t.Run("G2.4 统一外置路径: tool, task, tmux and overflow results take one path and one threshold", func(t *testing.T) {
		store := ledger.NewMemoryOriginalStore()
		ext := externalizer(t, store, workspace.WithMaxTokens(16000))
		big := strings.Repeat("统一外置-", 8000)
		body := serialized(t, big)
		if !ext.Exceeds(len(body)) {
			t.Fatalf("the shared body of %d characters is not over the shared threshold %d", len(body), ext.Threshold())
		}

		var previews []string
		kinds := []string{workspace.FormatToolResult, workspace.FormatTaskResult, workspace.FormatTmuxOutput, workspace.FormatOverflow}
		for _, format := range kinds {
			kindScope := scope
			kindScope.Format = format
			ref := workspace.OriginalRefFor(kindScope, body)
			var externalized workspace.Externalized
			var err error
			if format == workspace.FormatToolResult {
				got, callErr := boundedTool(t, ext, big).Call(ctx, []byte(`{}`))
				if callErr != nil {
					t.Fatalf("tool result Call = %v", callErr)
				}
				previews = append(previews, requirePreview(t, got, ref))
				continue
			}
			externalized, err = ext.Externalize(ctx, ref, body)
			if err != nil {
				t.Fatalf("Externalize(%s) = %v", format, err)
			}
			previews = append(previews, requirePreview(t, externalized.Preview, ref))
			recalled := store.Get(ctx, ref.Key)
			if recalled.Outcome != ledger.RecallFound {
				t.Fatalf("Get(%s) after Externalize(%s) = outcome %v, want found", ref.Key, format, recalled.Outcome)
			}
			assertSameBytes(t, "Externalize("+format+") original", recalled.Body, body)
			if externalized.Segment.MaxBytes != ledger.DefaultSegmentMaxBytes {
				t.Fatalf("Externalize(%s) segment entry = %+v, want the shared byte bound", format, externalized.Segment)
			}
		}
		for _, preview := range previews[1:] {
			if preview != previews[0] {
				t.Fatalf("producers rendered different previews for one body:\n%q\n%q", previews[0], preview)
			}
		}
		sharedKey := workspace.OriginalRefFor(scope, body).Key
		references := store.References(sharedKey)
		if len(references) != len(kinds) {
			t.Fatalf("four producers left %d references on one body, want %d: the formats must not overwrite one another", len(references), len(kinds))
		}
		formats := map[string]bool{}
		for _, reference := range references {
			formats[reference.Format] = true
		}
		for _, format := range kinds {
			if !formats[format] {
				t.Fatalf("the %s reference of the shared body is missing: references = %+v", format, references)
			}
		}
		if blobs := store.BlobCount(); blobs != 1 {
			t.Fatalf("BlobCount() = %d for one body shared by four producers, want the blob deduplicated once", blobs)
		}

		if decided, err := ext.DecideExternalize(ctx, "result-1", len(body)); err != nil || !decided {
			t.Fatalf("DecideExternalize(oversized) = %v, %v, want the overflow result externalized", decided, err)
		}
		if decided, err := ext.DecideExternalize(ctx, "result-2", ext.Threshold()); err != nil || decided {
			t.Fatalf("DecideExternalize(at threshold) = %v, %v, want it kept inside the bound", decided, err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if decided, err := ext.DecideExternalize(cancelled, "result-3", len(body)); err == nil || decided {
			t.Fatalf("DecideExternalize(cancelled) = %v, %v, want the cancellation reported instead of a decision", decided, err)
		}
	})

	t.Run("G2.5 nil 结果不拦截: a nil result is handed back and commits nothing", func(t *testing.T) {
		store := ledger.NewMemoryOriginalStore()
		ext := externalizer(t, store, workspace.WithMaxTokens(16000))
		before := store.BlobCount()
		got, err := boundedTool(t, ext, nil).Call(ctx, []byte(`{}`))
		if err != nil || got != nil {
			t.Fatalf("Call(nil result) = %v, %v, want nil, nil", got, err)
		}
		if after := store.BlobCount(); after != before {
			t.Fatalf("BlobCount() = %d after a nil result, want the %d it started with", after, before)
		}
	})

	t.Run("原文未提交不发预览: a refused commit yields an error and no ticket", func(t *testing.T) {
		cause := errors.New("commit barrier failed")
		store := &refusingOriginalStore{cause: cause}
		ext := externalizer(t, store, workspace.WithMaxTokens(16000))
		tool := boundedTool(t, ext, strings.Repeat("c", 40000))
		got, err := tool.Call(ctx, []byte(`{}`))
		if !errors.Is(err, cause) {
			t.Fatalf("Call over a refused commit = %v, want the commit cause %v", err, cause)
		}
		if got != nil {
			t.Fatalf("Call over a refused commit returned %T, want no preview of an original that was never kept", got)
		}
		if store.puts != 1 {
			t.Fatalf("the refusing store served %d commits, want exactly one refused commit", store.puts)
		}
	})

	t.Run("副本写失败仍保留原文: a replica write failure is reported beside the effective original", func(t *testing.T) {
		store := ledger.NewMemoryOriginalStore()
		big := strings.Repeat("d", 40000)
		blockedRoot := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blockedRoot, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write the blocking file: %v", err)
		}
		blockedMaterializer, err := workspace.NewMaterializer(blockedRoot, store, readScope)
		if err != nil {
			t.Fatalf("NewMaterializer(blocked root): %v", err)
		}
		blockedExt := externalizer(t, store, workspace.WithMaxTokens(16000), workspace.WithResultMaterializer(blockedMaterializer))
		got, err := boundedTool(t, blockedExt, big).Call(ctx, []byte(`{}`))
		if !errors.Is(err, workspace.ErrReplicaWriteFailed) {
			t.Fatalf("Call with a failing replica write = %v, want %v", err, workspace.ErrReplicaWriteFailed)
		}
		body := serialized(t, big)
		ref := workspace.OriginalRefFor(scope, body)
		requirePreview(t, got, ref)
		recalled := store.Get(ctx, ref.Key)
		if recalled.Outcome != ledger.RecallFound {
			t.Fatalf("Get(%s) = outcome %v, want the original kept beside the failed replica", ref.Key, recalled.Outcome)
		}
		assertSameBytes(t, "Get("+string(ref.Key)+") after a failed replica write", recalled.Body, body)
	})

	t.Run("G2.6 正式链消费者锚: the assembled run hands the model the preview of the kept original", func(t *testing.T) {
		store := ledger.NewMemoryOriginalStore()
		ext := externalizer(t, store, workspace.WithMaxTokens(16000))
		big := strings.Repeat("大结果-", 12000)
		inner := &largeResultTool{
			declaration: &trpctool.Declaration{Name: "shell", Description: "run one shell command"},
			result:      big,
		}
		wrapped, err := workspace.NewBoundedTool(inner, ext, scope)
		if err != nil {
			t.Fatalf("NewBoundedTool: %v", err)
		}
		provider := &consumerModel{name: "model-a"}
		run, err := tagent.Assemble(ctx, &config.Declaration{
			Entry:  "agent-large",
			Agents: []config.AgentSpec{{ID: "agent-large", Model: "model-a"}},
		}, map[event.AgentID]tagent.NativeAgent{
			"agent-large": {Model: provider, Tools: []trpctool.CallableTool{wrapped}},
		}, tagent.WithGovernance(consumerPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-large-result",
			Address:      owner,
			Input:        []model.Message{{Role: model.RoleUser, Content: "run the huge command"}},
		})
		if err != nil {
			t.Fatalf("invoke the assembled runtime: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want one completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		if inner.calls != 1 {
			t.Fatalf("the bounded tool served %d calls, want the one admitted call", inner.calls)
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("model requests = %d, want one request per decision", len(requests))
		}
		body := serialized(t, big)
		ref := workspace.OriginalRefFor(scope, body)
		var observation *model.Message
		for i := range requests[1].Messages {
			message := &requests[1].Messages[i]
			if message.ToolID == "call-1" && message.ToolName == "shell" && message.Role == model.RoleUser {
				observation = message
			}
			if len(message.Content) >= len(body) {
				t.Fatalf("the second request carries %d bytes of a %d byte result, want the bounded preview", len(message.Content), len(body))
			}
		}
		if observation == nil {
			t.Fatalf("the second request carries no settled observation of call-1: %+v", requests[1].Messages)
		}
		if !strings.Contains(observation.Content, string(ref.Key)) {
			t.Fatalf("the settled observation omits the ticket of original %s", ref.Key)
		}
		if !strings.Contains(observation.Content, fmt.Sprintf("at most %d bytes from offset 0", ledger.DefaultSegmentMaxBytes)) {
			t.Fatalf("the settled observation omits the segment entry")
		}
		recalled := store.Get(ctx, ref.Key)
		if recalled.Outcome != ledger.RecallFound {
			t.Fatalf("Get(%s) = outcome %v, want the original committed by the assembled run", ref.Key, recalled.Outcome)
		}
		assertSameBytes(t, "assembled Get("+string(ref.Key)+")", recalled.Body, body)
	})
}

// consumerModel is the scripted native provider of the assembled runtime: its
// first answer calls the bounded tool and every later answer settles.
type consumerModel struct {
	mu       sync.Mutex
	name     string
	turns    int
	requests []model.Request
}

// GenerateContent records the native request and streams the scripted answer.
func (m *consumerModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.turns++
	turn := m.turns
	m.requests = append(m.requests, *req)
	m.mu.Unlock()
	answer := &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
		Done:    true,
	}
	if turn == 1 {
		answer = &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: model.FunctionDefinitionParam{
					Name:      "shell",
					Arguments: []byte(`{}`),
				},
			}}}}},
			Done: true,
		}
	}
	ch := make(chan *model.Response, 1)
	ch <- answer
	close(ch)
	return ch, nil
}

// Info reports the scripted provider's descriptor.
func (m *consumerModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// recorded reports the native requests the scripted provider was driven with.
func (m *consumerModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// consumerPolicy is the passing governance stand-in of this assembly.
type consumerPolicy struct{}

// Check reports a passing disposition for each required mechanism check.
func (consumerPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       {Name: "eval", Verdict: state.CheckPass},
		"benchmark":  {Name: "benchmark", Verdict: state.CheckPass},
		"apply-gate": {Name: "apply-gate", Verdict: state.CheckPass},
	}, nil
}
