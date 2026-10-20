package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory"
	coreembedder "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/ledger"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
	ftrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestHostDocumentRefusesUnknownKey pins the startup gate: a key the host schema does not name stops the process with a locatable refusal.
// - the rejection wraps config.ErrUnknownConfigKey, so a caller separates it from a value of the wrong shape
// - it names the source file, the line and the full field path
// - the gate reaches a section nested under a declared agent, not only the root
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestHostDocumentRefusesUnknownKey(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		field string
		line  int
	}{
		{
			name:  "root key",
			body:  "entry: tagent\nlog_level: debug\n",
			field: "log_level",
			line:  2,
		},
		{
			name:  "section under a declared agent",
			body:  "entry: tagent\nagents:\n  tagent:\n    compress:\n      type: local\n",
			field: "agents.tagent.compress",
			line:  4,
		},
		{
			name:  "key under governance",
			body:  "governance:\n  budget_window_minutes: 60\n",
			field: "governance.budget_window_minutes",
			line:  2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tagent.yaml")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
			doc, err := loadHostDocument(path)
			if err == nil {
				t.Fatalf("load %s: accepted a key outside the host schema: %+v", path, doc)
			}
			if !errors.Is(err, config.ErrUnknownConfigKey) {
				t.Fatalf("load %s: error %v does not wrap config.ErrUnknownConfigKey", path, err)
			}
			for _, want := range []string{path, tc.field + ": unknown field", ":" + strconv.Itoa(tc.line) + ":"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("load %s: error %q does not name %q", path, err, want)
				}
			}
		})
	}
}

// TestHostDocumentReadsShippedDocuments pins the accepted side: both documents this example ships decode and yield a declaration carrying every declared agent.
// - tagent.yaml and tagent.rl.yaml both decode
// - each decoded agent names a model
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestHostDocumentReadsShippedDocuments(t *testing.T) {
	for _, path := range []string{"tagent.yaml", "tagent.rl.yaml"} {
		doc, err := loadHostDocument(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		if doc.Entry == "" || len(doc.Agents) == 0 {
			t.Fatalf("load %s: decoded %+v, want an entry agent beside declared agents", path, doc)
		}
		decl, err := doc.Declaration()
		if err != nil {
			t.Fatalf("derive the declaration of %s: %v", path, err)
		}
		if len(decl.Agents) != len(doc.Agents) {
			t.Fatalf("declaration of %s carries %d agents, want %d", path, len(decl.Agents), len(doc.Agents))
		}
		for _, spec := range decl.Agents {
			if spec.Model == "" {
				t.Errorf("%s: agent %q declares no model", path, spec.ID)
			}
		}
	}
}

// TestHostDocumentSkipsAbsentPromptFiles pins the accepted side of one absent file: the declaration carries the rest.
// - the absent file leaves the declaration short of it instead of stopping the assembly
// - the carried file keeps its prompt kind, so the turn still reads it
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestHostDocumentSkipsAbsentPromptFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("角色\n"), 0o600); err != nil {
		t.Fatalf("write the carried prompt: %v", err)
	}
	path := filepath.Join(dir, "tagent.yaml")
	body := "entry: tagent\nprompt_dir: " + dir + "\nagents:\n  tagent:\n    provider: p\n    model: m\n    system_prompt:\n      files: [AGENTS.md, USER.md]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	doc, err := loadHostDocument(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}

	decl, err := doc.Declaration()
	if err != nil {
		t.Fatalf("derive the declaration beside an absent prompt file: %v", err)
	}
	if len(decl.Agents) != 1 {
		t.Fatalf("declaration carries %d agents, want 1", len(decl.Agents))
	}
	files := decl.Agents[0].Files
	if len(files) != 1 {
		t.Fatalf("agent %q carries %d prompt files, want the one the checkout carries", decl.Agents[0].ID, len(files))
	}
	if files[0].Path != filepath.Join(dir, "AGENTS.md") || files[0].Kind != config.FileKindPrompt {
		t.Fatalf("prompt file = %+v, want the carried file of prompt kind", files[0])
	}
}

// TestHostToolRefsReadsTheBareAgentForm resolves the bare `- agent: <id>` reference to the delegation the written-out form names:
// - the bare form yields a tool reference carrying the declared target
// - a bare reference to an agent the document does not declare is refused by name
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestHostToolRefsReadsTheBareAgentForm(t *testing.T) {
	doc := &hostDocument{
		Entry: "tagent",
		Agents: map[string]agentEntry{
			"tagent":    {Model: "model-main", Tools: []hostToolRef{{Agent: "knowledge"}}},
			"knowledge": {Model: "model-helper"},
		},
	}

	refs, err := doc.toolRefs("tagent")
	if err != nil {
		t.Fatalf("resolve the bare agent reference: %v", err)
	}
	if len(refs) != 1 || refs[0].Name != "knowledge" || refs[0].Params["agent"] != "knowledge" {
		t.Fatalf("references = %+v, want the one delegation to the declared knowledge agent", refs)
	}

	doc.Agents["tagent"] = agentEntry{Model: "model-main", Tools: []hostToolRef{{Agent: "ghost"}}}
	if _, err := doc.toolRefs("tagent"); !errors.Is(err, ErrUnknownAgentReference) {
		t.Fatalf("resolve a bare reference to an undeclared agent: error %v, want ErrUnknownAgentReference", err)
	}
}

// TestHostBareAgentReferenceDelegatesTheTurn drives the bare `- agent: <id>` form through the assembly the process runs:
// - the entry model sees one tool named after the declared target and no generic delegation tool
// - the call admits the target's turn, which runs on the target's own model and surface beside the receipt
// - the admission receipt reaches the caller and the entry turn still delivers its answer
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestHostBareAgentReferenceDelegatesTheTurn(t *testing.T) {
	ctx := context.Background()
	doc := &hostDocument{
		Entry: "tagent",
		Agents: map[string]agentEntry{
			"tagent":    {Model: "model-main", Tools: []hostToolRef{{Agent: "knowledge"}}},
			"knowledge": {Model: "model-helper"},
		},
	}
	entry, target := &scriptModel{}, &scriptModel{}
	natives := map[event.AgentID]tagent.NativeAgent{
		"tagent":    {Model: namedModel{scriptModel: entry, name: "model-main"}},
		"knowledge": {Model: namedModel{scriptModel: target, name: "model-helper"}},
	}

	host, err := assembleHostFrom(ctx, doc, nil, NewSeenStore(t.TempDir(), slog.Default()), &hostSendSeam{}, natives, nil)
	if err != nil {
		t.Fatalf("assemble the host around the bare agent reference: %v", err)
	}
	out := &mockOutbound{}
	host.sender = &hostSender{
		bot: out,
		longText: func(_ context.Context, chatID, content, _ string) error {
			out.record("long", chatID, content)
			return nil
		},
		typingActive: &sync.Map{},
		router:       host.router,
		inj:          host.inbox,
		workspaceDir: t.TempDir(),
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("close the host: %v", err)
		}
	})

	entry.then(delegateTurn("knowledge", `{"input":"what is the deploy runbook"}`), finalTurn("answered through the knowledge agent"))
	target.then(finalTurn("the knowledge agent read the runbook"))

	if _, err := host.HandleInbound(ctx, inboundMessage{Key: "delegate", ChatID: "chat-a", Text: "what is the deploy runbook"}); err != nil {
		t.Fatalf("drive the delegating turn: %v", err)
	}

	requests := entry.recorded()
	if len(requests) == 0 {
		t.Fatal("the entry model was never driven")
	}
	if requests[0].Tools["knowledge"] == nil {
		t.Fatalf("entry tool names = %v, want the declared target", toolNamesOf(requests[0].Tools))
	}
	if requests[0].Tools["call_subagent"] != nil {
		t.Fatalf("entry tool names = %v, want no generic delegation tool", toolNamesOf(requests[0].Tools))
	}
	if !entryCarriesAdmission(requests) {
		t.Fatalf("no caller request carries the admission receipt: %+v", requests)
	}

	deadline := time.Now().Add(10 * time.Second)
	for target.callCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	delegated := target.recorded()
	if len(delegated) != 1 {
		t.Fatalf("delegated model steps = %d, want the one delegated turn", len(delegated))
	}
	if !requestCarries(delegated[0], "what is the deploy runbook") {
		t.Fatalf("the delegated request misses the request of the call: %+v", delegated[0].Messages)
	}
	if delegated[0].Tools["knowledge"] != nil {
		t.Fatalf("the delegated turn carries the caller's own target tool: %v", toolNamesOf(delegated[0].Tools))
	}
	if !out.has("text", "chat-a", "answered through the knowledge agent") {
		t.Fatalf("deliveries = %v, want the entry answer", out.recorded())
	}
}

// entryCarriesAdmission reports whether any request of the caller carries the
// admission receipt of the delegated call.
func entryCarriesAdmission(requests []trpcmodel.Request) bool {
	for _, req := range requests {
		if requestCarries(req, "accepted task-") {
			return true
		}
	}
	return false
}

// namedModel reports the declared model name of one scripted model, so the target
// tool resolves the model its own declaration names rather than a shared one.
type namedModel struct {
	*scriptModel
	name string
}

// Info reports the declared name of the scripted model.
func (m namedModel) Info() trpcmodel.Info { return trpcmodel.Info{Name: m.name} }

// toolNamesOf reports the names of one request's tool surface in sorted order, so
// a failing check prints exactly what the model was offered.
func toolNamesOf(tools map[string]trpctool.Tool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// delegateTurn builds the scripted response calling the named tool with the given arguments.
func delegateTurn(name, args string) *trpcmodel.Response {
	return &trpcmodel.Response{
		Choices: []trpcmodel.Choice{{Message: trpcmodel.Message{Role: trpcmodel.RoleAssistant, ToolCalls: []trpcmodel.ToolCall{{
			ID:   "call-delegate-1",
			Type: "function",
			Function: trpcmodel.FunctionDefinitionParam{
				Name:      name,
				Arguments: []byte(args),
			},
		}}}}},
		Done: true,
	}
}

// generationSpanName is the name prefix a generation span carries: the chat
// operation beside the model name the request landed on.
const generationSpanName = "chat"

// hostGenerationRecorder installs a real SDK tracer on the framework tracer the
// runtime's own loop and the framework flow both read, and answers the spans
// they ended, so one check reads the span a model exchange really exports.
func hostGenerationRecorder(tb testing.TB) *tracetest.SpanRecorder {
	tb.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	originalTracer, originalProvider := ftrace.Tracer, ftrace.TracerProvider
	ftrace.Tracer, ftrace.TracerProvider = provider.Tracer("wechat-bot-generation-check"), provider
	tb.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		ftrace.Tracer, ftrace.TracerProvider = originalTracer, originalProvider
	})
	return recorder
}

// mockProviderDocument is one host document whose two declared agents run
// against the given endpoint: the provider protocol the host wires and the key
// environment variable its model constructor reads.
func mockProviderDocument(t *testing.T, endpoint string) *hostDocument {
	t.Helper()
	const keyEnv = "WECHAT_BOT_MOCK_OPENAI_KEY"
	t.Setenv(keyEnv, "test-key")
	t.Setenv("TAGENT_API_ENDPOINT", "")
	return &hostDocument{
		Entry:    "main",
		Provider: "mock",
		Providers: map[string]providerEntry{
			"mock": {Provider: "openai", APIEndpoint: endpoint, APIKeyEnv: keyEnv},
		},
		Agents: map[string]agentEntry{
			"main":   {Model: "model-main"},
			"helper": {Model: "model-helper"},
		},
	}
}

// mockOpenAIServer answers one chat completion: the streamed chunks a model
// request asks for when it asks to stream, the plain completion otherwise.
func mockOpenAIServer(t *testing.T, answer string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "unreadable body", http.StatusBadRequest)
			return
		}
		if strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"id\":\"answer\",\"object\":\"chat.completion.chunk\",\"created\":1699200000,"+
				"\"model\":\"model-main\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%q},"+
				"\"finish_reason\":\"stop\"}]}\n\n", answer)
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"answer","object":"chat.completion","created":1699200000,"model":"model-main",`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`, answer)
	}))
	t.Cleanup(server.Close)
	return server
}

// generationSpans reports the ended spans of the generation family.
func generationSpans(recorder *tracetest.SpanRecorder) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if strings.HasPrefix(span.Name(), generationSpanName) {
			out = append(out, span)
		}
	}
	return out
}

// spanAttribute reports the rendered value of one attribute of a span.
func spanAttribute(span sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// driveRuntimeModelStep drives one model exchange the way the runtime's own
// loop drives it: the provider opens its stream and the single terminal
// reduction reads it.
func driveRuntimeModelStep(t *testing.T, ctx context.Context, provider trpcmodel.Model) {
	t.Helper()
	stream, err := provider.GenerateContent(ctx, &trpcmodel.Request{
		Messages:         []trpcmodel.Message{trpcmodel.NewUserMessage("hello")},
		GenerationConfig: trpcmodel.GenerationConfig{Stream: true},
	})
	if err != nil {
		t.Fatalf("open the model stream: %v", err)
	}
	if outcome := tagentmodel.ReduceChannel(ctx, stream, nil); outcome.Kind != tagentmodel.KindCompleted {
		t.Fatalf("terminal = %s, want the completed terminal of the exchange", outcome.Kind)
	}
}

// driveFrameworkModelStep runs one model step of a framework flow over the
// named model, the way a delegated turn reaches it, and answers every
// completion notice the flow asks for.
func driveFrameworkModelStep(t *testing.T, ctx context.Context, name string, provider trpcmodel.Model) {
	t.Helper()
	drive := llmagent.New(name, llmagent.WithModel(provider))
	invocation := trpcagent.NewInvocation(
		trpcagent.WithInvocationID("inv-"+name),
		trpcagent.WithInvocationMessage(trpcmodel.Message{Role: trpcmodel.RoleUser, Content: "hello"}),
	)
	events, err := drive.Run(ctx, invocation)
	if err != nil {
		t.Fatalf("run the framework model step: %v", err)
	}
	for evt := range events {
		if evt != nil && evt.RequiresCompletion {
			_ = invocation.NotifyCompletion(ctx, trpcagent.GetAppendEventNoticeKey(evt.ID))
		}
	}
}

// TestHostModelReportsOneGeneration pins where the generation span of the host's own model comes from:
// - 主循环: one model exchange over the entry agent's model lands exactly one generation span
// - 委派回合: the same host model driven by a delegated framework flow adds no second span beside the flow's own
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestHostModelReportsOneGeneration(t *testing.T) {
	ctx := context.Background()
	server := mockOpenAIServer(t, "settled")
	doc := mockProviderDocument(t, server.URL)

	t.Run("主循环一次交换一条 generation", func(t *testing.T) {
		recorder := hostGenerationRecorder(t)
		provider, err := doc.modelFor(doc.Entry, server.Client())
		if err != nil {
			t.Fatalf("build the entry model: %v", err)
		}

		driveRuntimeModelStep(t, ctx, provider)

		spans := generationSpans(recorder)
		if len(spans) != 1 {
			t.Fatalf("generation spans = %d, want the one chat span of the exchange", len(spans))
		}
		if got, want := spans[0].Name(), generationSpanName+" model-main"; got != want {
			t.Fatalf("generation span name = %q, want %q", got, want)
		}
		if got, ok := spanAttribute(spans[0], string(semconvtrace.KeyGenAIOperationName)); !ok || got.AsString() != "chat" {
			t.Fatalf("gen_ai.operation.name = %v (present %t), want chat", got.AsString(), ok)
		}
	})

	t.Run("委派回合不在框架 span 旁添第二条", func(t *testing.T) {
		recorder := hostGenerationRecorder(t)
		provider, err := doc.modelFor("helper", server.Client())
		if err != nil {
			t.Fatalf("build the delegated agent's model: %v", err)
		}

		driveFrameworkModelStep(t, ctx, "helper", provider)

		if got := len(generationSpans(recorder)); got != 1 {
			t.Fatalf("generation spans = %d, want the framework's one and no second", got)
		}
	})
}

// TestShippedMemoryEngineWiresTheSemanticRetrieval pins the shipped memory declaration as the one the host reads.
//   - the memory sections merge into one localfile tier over the existing data root
//   - the engine keeps the zhipu embedding-3 connection, its 512 width and the tuning
//   - the assembled embedder is the core provider-backed one, not the hash stand-in
//   - read_namespaces widen the recall authorization beside the agent's own namespace
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestShippedMemoryEngineWiresTheSemanticRetrieval(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "test-key")
	doc, err := loadHostDocument("tagent.yaml")
	if err != nil {
		t.Fatalf("load the shipped document: %v", err)
	}
	decl, err := doc.Declaration()
	if err != nil {
		t.Fatalf("derive the shipped declaration: %v", err)
	}
	settings, err := tagent.MemoryPlanOf(decl, doc.Storage.Type, doc.Storage.Root)
	if err != nil {
		t.Fatalf("merge the memory declarations: %v", err)
	}
	if settings == nil {
		t.Fatal("the shipped document declares memory but merged into none")
	}
	want := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: ".wechat-config/data"}
	storage := settings.Storage()
	if storage == nil {
		t.Fatal("the shipped document declares no storage tier")
	}
	if *storage != want {
		t.Fatalf("storage = %+v, want %+v", *storage, want)
	}
	engine := settings.Engine()
	if engine == nil {
		t.Fatal("the shipped document declares no semantic engine")
	}
	if engine.Backend != "memory" || engine.Embedding.Provider != "zhipu" ||
		engine.Embedding.Model != "embedding-3" || engine.Embedding.APIKeyEnv != "ZAI_API_KEY" ||
		engine.Embedding.Dimensions != 512 {
		t.Fatalf("engine = %+v, want the zhipu embedding-3 connection of 512 dimensions", engine)
	}
	if engine.VectorTopK != 20 || engine.KeywordTopK != 20 || engine.RRFK != 60 {
		t.Fatalf("engine tuning = %d/%d/%d, want 20/20/60", engine.VectorTopK, engine.KeywordTopK, engine.RRFK)
	}

	embedded, err := settings.Embedder()
	if err != nil {
		t.Fatalf("assemble the declared embedder: %v", err)
	}
	provider, ok := embedded.(*coreembedder.Embedder)
	if !ok {
		t.Fatalf("embedder = %T, want the core provider-backed embedder rather than the hash stand-in", embedded)
	}
	connection := provider.Connection()
	if connection.Model != "embedding-3" || connection.Dimensions != 512 || connection.Provider != "zhipu" {
		t.Fatalf("connection = %+v, want the declared zhipu embedding-3 binding", connection)
	}
	if want := "https://open.bigmodel.cn/api/paas/v4"; connection.APIEndpoint != want {
		t.Fatalf("embedder endpoint = %q, want the zhipu default %q", connection.APIEndpoint, want)
	}

	vectorTopK, keywordTopK, rrfK := settings.Tuning()
	if vectorTopK != 20 || keywordTopK != 20 || rrfK != 60 {
		t.Fatalf("retrieval tuning = %d/%d/%d, want 20/20/60", vectorTopK, keywordTopK, rrfK)
	}

	names := memory.PartitionsOf("recall", doc.Agents["recall"].Memory.ReadNamespaces)
	if len(names) != 2 || names[0] != "recall" || names[1] != "tagent" {
		t.Fatalf("recall read namespaces = %v, want the own namespace beside the declared tagent read", names)
	}
}

// TestMemoryEngineRefusesWithoutTheKey refuses an engine whose key variable holds nothing by name.
func TestMemoryEngineRefusesWithoutTheKey(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	doc, err := loadHostDocument("tagent.yaml")
	if err != nil {
		t.Fatalf("load the shipped document: %v", err)
	}
	decl, err := doc.Declaration()
	if err != nil {
		t.Fatalf("derive the shipped declaration: %v", err)
	}
	settings, err := tagent.MemoryPlanOf(decl, doc.Storage.Type, doc.Storage.Root)
	if err != nil {
		t.Fatalf("merge the memory declarations: %v", err)
	}
	_, err = settings.Embedder()
	if !errors.Is(err, coreembedder.ErrEmbeddingKey) {
		t.Fatalf("assemble the embedder beside an empty key: error %v, want ErrEmbeddingKey", err)
	}
	if !strings.Contains(err.Error(), "ZAI_API_KEY") {
		t.Fatalf("the refusal %q does not name the empty variable", err)
	}
}

// TestMemoryDeclarationRefusesInconsistentShapes pins the merge refusals:
// - two agents naming different storage tiers are refused by name
// - an engine beside no storage tier is refused by name
// - an unknown engine backend is refused by name
func TestMemoryDeclarationRefusesInconsistentShapes(t *testing.T) {
	cases := []struct {
		name string
		doc  *hostDocument
		want error
	}{
		{
			name: "two tiers",
			doc: &hostDocument{
				Entry: "a",
				Agents: map[string]agentEntry{
					"a": {Memory: config.MemorySpec{Type: "localfile", Path: "one"}},
					"b": {Memory: config.MemorySpec{Type: "localfile", Path: "two"}},
				},
			},
			want: tagent.ErrMemoryDeclaration,
		},
		{
			name: "engine beside no tier",
			doc: &hostDocument{
				Entry: "a",
				Agents: map[string]agentEntry{
					"a": {Memory: config.MemorySpec{Engine: config.EngineSpec{Backend: "memory"}}},
				},
			},
			want: tagent.ErrMemoryDeclaration,
		},
		{
			name: "unknown backend",
			doc: &hostDocument{
				Entry: "a",
				Agents: map[string]agentEntry{
					"a": {Memory: config.MemorySpec{
						Type: "localfile",
						Path: "one",
						Engine: config.EngineSpec{
							Backend:   "kv",
							Embedding: config.EmbeddingSpec{Provider: "zhipu", Model: "embedding-3", APIKeyEnv: "ZAI_API_KEY", Dimensions: 512},
						},
					}},
				},
			},
			want: tagent.ErrMemoryEngine,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decl, err := tc.doc.Declaration()
			if err != nil {
				t.Fatalf("derive the declaration: %v", err)
			}
			_, err = tagent.MemoryPlanOf(decl, tc.doc.Storage.Type, tc.doc.Storage.Root)
			if !errors.Is(err, tc.want) {
				t.Fatalf("merge the declarations: error %v, want %v", err, tc.want)
			}
		})
	}
}

// TestMemoryDeclarationZeroValueKeepsTheHostSilent wires no tier, embedder or tuning beside no declaration.
func TestMemoryDeclarationZeroValueKeepsTheHostSilent(t *testing.T) {
	doc := &hostDocument{Entry: "a", Agents: map[string]agentEntry{"a": {Model: "m"}}}
	decl, err := doc.Declaration()
	if err != nil {
		t.Fatalf("derive the declaration: %v", err)
	}
	plan, err := tagent.MemoryPlanOf(decl, doc.Storage.Type, doc.Storage.Root)
	if err != nil || plan != nil {
		t.Fatalf("plan = %v, %v, want nil beside no declaration", plan, err)
	}
	if cfg := plan.Storage(); cfg != nil {
		t.Fatalf("storage config = %+v, want nil beside no declaration", cfg)
	}
	if embedded, err := plan.Embedder(); err != nil || embedded != nil {
		t.Fatalf("embedder = %v, %v, want nil beside no declaration", embedded, err)
	}
	if v, k, r := plan.Tuning(); v != 0 || k != 0 || r != 0 {
		t.Fatalf("retrieval tuning = %d/%d/%d, want the zero tuning", v, k, r)
	}
}
