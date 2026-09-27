package tagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §3.3（A2A 路径）：远端委派必须以「真实声明 + 真实线上请求 + 真实工具返回」作证，
// 不能只查 wrapper 字段。本文件同时钉住校验域与构建域一致——一个**只有 remote 端点、
// 没有本地定义**的 agent 引用必须能加载并构建（构建域一直支持；校验域曾误要求本地
// 定义，见 config.go 的 isRemoteRef 单一谓词）。

// remoteAnswer is the distinctive payload the stand-in service returns. Finding it
// back in the parent's tool-result message proves the delegation really crossed
// the wire and came back — nothing local could have produced this string.
const remoteAnswer = "REMOTE-A2A-ANSWER::42"

// remoteService is a protocol-faithful A2A endpoint stand-in: agent card at
// /.well-known/agent*.json, JSON-RPC on every other path. Each RPC body is
// recorded verbatim, so assertions run against what ACTUALLY went over the wire
// (endpoint usage, message text, transferred state) rather than an internal field.
type remoteService struct {
	mu       sync.Mutex
	rpcs     []map[string]any
	cardHits int
	// failFirstRPC answers this many RPC calls with a transport error before it
	// starts succeeding — the shape the A2A retry branch exists for.
	failFirstRPC int
	// onFailure runs inside the handler of a TRANSIENT (503) attempt, before the
	// error is answered. A test uses it to publish exactly DURING the retry
	// window, so the window is synchronous to the failure rather than a sleep.
	onFailure func()
	srv       *httptest.Server
}

func newRemoteService(t *testing.T) *remoteService {
	t.Helper()
	r := &remoteService{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "well-known") {
			r.mu.Lock()
			r.cardHits++
			r.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":                 "knowledge",
				"description":          "remote knowledge service",
				"url":                  r.srv.URL,
				"version":              "1.0.0",
				"capabilities":         map[string]any{"streaming": false},
				"defaultInputModes":    []string{"text/plain"},
				"defaultOutputModes":   []string{"text/plain"},
				"skills":               []any{},
				"preferredTransport":   "JSONRPC",
				"protocolVersion":      "1.0",
				"additionalInterfaces": []any{},
			})
			return
		}
		var rpc map[string]any
		if err := json.NewDecoder(req.Body).Decode(&rpc); err != nil {
			http.Error(w, "bad rpc", http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		r.rpcs = append(r.rpcs, rpc)
		n := len(r.rpcs)
		transient := n <= r.failFirstRPC
		r.mu.Unlock()
		if transient {
			// Transport-level failure: the client's Run must surface it (that is
			// the trigger the remote retry branch waits for), not a silent answer.
			if hook := r.failureHook(); hook != nil {
				hook() // publish while THIS attempt is being retried
			}
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      rpc["id"],
			"result": map[string]any{
				"kind":      "message",
				"messageId": "remote-msg-1",
				"role":      "agent",
				"parts":     []any{map[string]any{"kind": "text", "text": remoteAnswer}},
			},
		})
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// failureHook reads the armed transient-failure hook under the same lock the
// handler uses for its bookkeeping (the mutex is already released by the time the
// caller is about to answer 503, so the hook must not be invoked while holding it).
func (r *remoteService) failureHook() func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.onFailure
}

func (r *remoteService) rpcCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rpcs)
}

func (r *remoteService) snapshotRPCs() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any(nil), r.rpcs...)
}

// wireModel scripts ONE real turn: the first call delegates to whatever tool the
// entry was actually offered (so the target is chosen by the published
// declaration, exactly as a real model chooses), later calls close the turn. Every
// request is captured — that is where the tool result comes back into view.
type wireModel struct {
	mu       sync.Mutex
	requests []*model.Request
	args     string
}

func (m *wireModel) snapshot() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*model.Request(nil), m.requests...)
}

func (m *wireModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	n := len(m.requests)
	args := m.args
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if n == 1 {
		var names []string
		for _, tl := range req.Tools {
			if d := tl.Declaration(); d != nil {
				names = append(names, d.Name)
			}
		}
		if len(names) > 0 {
			ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{Type: "function", ID: "call-1", Function: model.FunctionDefinitionParam{
					Name: names[0], Arguments: []byte(args)}}},
			}}}}
			close(ch)
			return ch, nil
		}
	}
	ch <- &model.Response{Done: true, Choices: []model.Choice{{
		Message: model.NewAssistantMessage("entry-final")}}}
	close(ch)
	return ch, nil
}

func (m *wireModel) Info() model.Info { return model.Info{Name: "wire-model"} }

func offeredToolNames(req *model.Request) []string {
	var out []string
	for _, tl := range req.Tools {
		if d := tl.Declaration(); d != nil {
			out = append(out, d.Name)
		}
	}
	return out
}

// toolResultsOf returns the tool-role message contents of a request — the parent's
// view of what the delegation actually returned.
func toolResultsOf(req *model.Request) []string {
	var out []string
	for _, msg := range req.Messages {
		if msg.Role == model.RoleTool && msg.Content != "" {
			out = append(out, msg.Content)
		}
	}
	return out
}

// remoteYAML renders an entry that delegates to exactly one agent, declared ONLY
// as a remote endpoint. `extra` is appended to the tool block (event_params etc.).
func remoteYAML(endpoint, extra string) string {
	return fmt.Sprintf(`entry: a
agents:
  a:
    system_prompt:
      inline: "ENTRY-A-PROMPT"
    max_tool_iterations: 2
    memory:
      type: memory
    tools:
      - kind: agent
        agent: knowledge
        description: delegate-knowledge
        async: false
%s
        remote:
          url: %q
`, extra, endpoint)
}

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tagent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// driveOneTurn starts the loop, injects one request and waits for the entry turn
// to close (two entry model calls: the delegation and the final).
func driveOneTurn(t *testing.T, entry *agent.TagentAgent, m *wireModel) {
	t.Helper()
	out, err := entry.StartLoop("u", "a2a-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("go"))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(m.snapshot()) >= 2 }, 20*time.Second, 20*time.Millisecond,
		"the entry turn must close after the delegation returned")
}

// TestRemoteRefDelegatesWithRealDeclarationEndpointAndReturn is §3.3's A2A row at
// the deployment level: a config whose ONLY sub-agent is remote (no local
// definition anywhere) must load, expose the delegation in a real model request,
// really reach the declared endpoint, and hand the remote answer back to the
// parent turn as the tool result.
func TestRemoteRefDelegatesWithRealDeclarationEndpointAndReturn(t *testing.T) {
	svc := newRemoteService(t)
	yamlPath := writeYAML(t, remoteYAML(svc.srv.URL, ""))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err, "a remote-only reference must load without a local definition")
	require.NotContains(t, cfg.Agents, "knowledge", "the config really has NO local definition")

	m := &wireModel{args: `{"request":"what does the remote know?"}`}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	driveOneTurn(t, entry, m)

	// 真实声明：entry 的模型请求里出现的就是这个远端委派工具。
	require.Contains(t, offeredToolNames(m.snapshot()[0]), "knowledge",
		"the published face must offer the remote delegation to the model")

	// 真实端点：调用确实落到声明的 URL 上。
	require.Eventually(t, func() bool { return svc.rpcCount() >= 1 }, 20*time.Second, 20*time.Millisecond,
		"the delegation must actually reach the declared endpoint")

	// 真实工具返回：远端答案以 tool 结果回到父 turn（本地没有任何东西能产出这个串）。
	var sawAnswer bool
	for _, req := range m.snapshot() {
		for _, got := range toolResultsOf(req) {
			if strings.Contains(got, remoteAnswer) {
				sawAnswer = true
			}
		}
	}
	require.True(t, sawAnswer, "the remote answer must come back as the parent's tool result")

	// 参数一致：父请求的原文确实随委派送出。
	body, _ := json.Marshal(svc.snapshotRPCs()[0])
	require.Contains(t, string(body), "what does the remote know?",
		"the delegated request text must ride to the endpoint")
}

// TestRemoteRefCarriesEventContextOverTheWire pins the parameter half of §3.3 for
// the remote shape: an event_key resolved from the PARENT store must reach the
// remote endpoint as transferred state, so the remote agent sees the same context
// a local sub-agent would — the parent binding, not a fresh one, supplies it.
func TestRemoteRefCarriesEventContextOverTheWire(t *testing.T) {
	svc := newRemoteService(t)
	yamlPath := writeYAML(t, remoteYAML(svc.srv.URL, "        event_params: [event_keys]\n"))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &wireModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	// A real event in the parent's own store, referenced by key from the model's
	// tool call (the same plumbing a local sub-agent consumes).
	const key = int64(0x0a2a000000001)
	require.NoError(t, entry.MemStore().StoreEvent(key, memory.FullEvent{
		EventKey: key, EventType: "external_input",
		EventSummary: "远端上下文注入事件", Content: "远端上下文注入事件",
	}))
	m.args = fmt.Sprintf(`{"request":"analyze","event_keys":[%q]}`, tagentevent.FormatEventKey(key))

	driveOneTurn(t, entry, m)
	require.Eventually(t, func() bool { return svc.rpcCount() >= 1 }, 20*time.Second, 20*time.Millisecond,
		"the delegation must reach the endpoint before the parent can be answered")
	body, _ := json.Marshal(svc.snapshotRPCs())
	require.Contains(t, string(body), "远端上下文注入事件",
		"the parent-resolved event context must cross the wire to the remote target")
	require.Contains(t, string(body), agent.ExternalContextKey,
		"and it must travel under the transferred-state key the remote maps back")
}

// TestRemoteRefRejectsURLLessDeclaration pins the mirrored mismatch: a reference
// that DECLARES remote but carries no endpoint must not be silently built as a
// local agent — the build would then run a different runtime than the config says.
func TestRemoteRefRejectsURLLessDeclaration(t *testing.T) {
	yamlPath := writeYAML(t, remoteYAML("", "")+"\n  knowledge:\n    system_prompt:\n      inline: \"SUB-K-PROMPT\"\n    memory:\n      type: memory\n")
	_, err := LoadConfig(yamlPath)
	require.Error(t, err, "a remote declaration without an endpoint must be refused, not built local")
	require.Contains(t, err.Error(), "requires a url")
}

// TestRemoteDelegationRetriesAgainstTheSameDeclaredTarget pins §3.3's transport-retry
// row for the remote shape: a transport failure must be retried against the SAME
// declared endpoint with the SAME delegation payload, and the parent must receive
// the real answer rather than an error event. Re-resolving the target on retry is
// what D5 forbids (「传输重试：继承发起调用租约」) — and what a global-table lookup
// on the retry path would silently do.
func TestRemoteDelegationRetriesAgainstTheSameDeclaredTarget(t *testing.T) {
	svc := newRemoteService(t)
	svc.mu.Lock()
	svc.failFirstRPC = 1
	svc.mu.Unlock()

	yamlPath := writeYAML(t, remoteYAML(svc.srv.URL, ""))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &wireModel{args: `{"request":"retry this delegation"}`}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	driveOneTurn(t, entry, m)

	require.Eventually(t, func() bool { return svc.rpcCount() >= 2 }, 20*time.Second, 20*time.Millisecond,
		"the transport failure must drive the remote retry branch, not a silent failure")
	rpcs := svc.snapshotRPCs()
	for i, rpc := range rpcs[:2] {
		body, _ := json.Marshal(rpc)
		require.Contains(t, string(body), "retry this delegation",
			"attempt %d must carry the same delegation payload", i)
	}
	var sawAnswer bool
	for _, req := range m.snapshot() {
		for _, got := range toolResultsOf(req) {
			if strings.Contains(got, remoteAnswer) {
				sawAnswer = true
			}
		}
	}
	require.True(t, sawAnswer, "the retried attempt's answer must reach the parent turn")
}

// TestRemoteRetryAcrossPublishKeepsTheDeclaredEndpoint is §3.4's「远端重试过程中
// 发布」row at the production entry (D6/J10: transport retry fixes the endpoint
// AND the payload; local turns do not retry). A G2 that re-points the SAME agent
// name at a DIFFERENT endpoint is published synchronously inside the transient
// 503 attempt, so by the time the client retries, the new generation is already
// in force — the window is caused by the failure, not by a sleep.
//
// What would falsify the rule is a retry that re-resolves its target: it would
// reach the successor endpoint. So the discriminator is causal and does not count
// global totals: until the parent is handed the answer, the SUCCESSOR service must
// not have been contacted at all, while every attempt recorded on the ORIGINAL
// endpoint carries the same delegation payload.
func TestRemoteRetryAcrossPublishKeepsTheDeclaredEndpoint(t *testing.T) {
	original := newRemoteService(t)
	original.mu.Lock()
	original.failFirstRPC = 1
	original.mu.Unlock()
	successor := newRemoteService(t)

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	crossWrite(t, yamlPath, remoteYAML(original.srv.URL, ""), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &wireModel{args: `{"request":"retry across a publish"}`}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	// Publish exactly during the retry window: re-point `knowledge` elsewhere.
	var published sync.Once
	original.mu.Lock()
	original.onFailure = func() {
		published.Do(func() {
			// Well beyond the initial write's +2s bump: an mtime that is not strictly
			// newer would make CheckOrgReload return early and the publish be a no-op.
			hookTick := time.Now().Add(90 * time.Second)
			require.NoError(t, os.WriteFile(yamlPath, []byte(remoteYAML(successor.srv.URL, "")), 0o644))
			require.NoError(t, os.Chtimes(yamlPath, hookTick, hookTick))
			entry.CheckOrgReload()
		})
	}
	original.mu.Unlock()

	// The generation in flight is the one live BEFORE the publication (captured here,
	// which runs before the retry hook can publish).
	inflightGen := entryGeneration(t, entry)
	require.GreaterOrEqual(t, inflightGen, int64(0), "precondition: the entry has an active generation")

	out, err := entry.StartLoop("u", "a2a-retry-publish-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("go"))
	require.NoError(t, err)

	// Wait for the parent to receive the answer, failing the moment the successor
	// endpoint is touched: that would mean the retry re-resolved onto G2.
	var answered bool
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && !answered {
		require.Zero(t, successor.rpcCount(),
			"§3.4：重试期间发布后，重投仍须打在发起时声明的端点——后继端点被联系即说明重试改道了")
		for _, req := range m.snapshot() {
			for _, got := range toolResultsOf(req) {
				if strings.Contains(got, remoteAnswer) {
					answered = true
				}
			}
		}
		if !answered {
			time.Sleep(20 * time.Millisecond)
		}
	}
	require.True(t, answered, "the retried attempt's real answer must reach the parent turn")

	// 资源尾部（同一条调用链内，不由 agent 层见证拼凑）：the generation that carried
	// this in-flight remote call is retired by the publication mid-retry and must be
	// reclaimed once the call lands — the row it occupies disappears from the books.
	require.Eventuallyf(t, func() bool { return !hasGeneration(entry, inflightGen) },
		20*time.Second, 20*time.Millisecond,
		"the superseded generation %d that held the in-flight remote call must be reclaimed after the call lands: %+v",
		inflightGen, entry.ContextManager().ExecutorRefs().Generations)

	// The publication really happened (otherwise the row above is vacuous), and
	// both attempts went to the ORIGINAL endpoint with the SAME payload.
	require.Greater(t, di64(t, entry.OrgDiagnostics(), "generation"), int64(0),
		"precondition: a new generation was published during the retry")
	require.GreaterOrEqual(t, original.rpcCount(), 2,
		"the transient failure must drive a retry against the declared endpoint")
	for i, rpc := range original.snapshotRPCs() {
		body, _ := json.Marshal(rpc)
		require.Contains(t, string(body), "retry across a publish",
			"attempt %d must carry the same delegation payload across the publish", i)
	}
}
