// Package host exercises the public runtime entry of the module root over a
// real assembly: the SDK constructor, start, close and diagnostic query entries
// beside the thin CLI shell that consumes them. The tests drive the runtime
// through the root package's public face only and never import the prototype.
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/query"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestPublicRuntimeEntry drives the public SDK and CLI entries of the module root over the sole assembly entry and checks the run every entry shares.
// - the public SDK constructs the runtime from the sole assembly entry and starts one real turn
// - the diagnostic query reads the live scene through the query projection under the caller's scope
// - the close refuses new input first and reports an honest drain of everything it settled
// - the thin CLI shell drives the same assembled run and builds no second runtime
// - each request ends on the material of its own drive while the session history already committed sits in front of that input
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestPublicRuntimeEntry(t *testing.T) {
	t.Run("the public SDK constructs the runtime from the sole assembly entry and starts one real turn", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, provider, tool := newEntryRun(t, ctx)
		addr := event.Address{AgentID: "agent-a", SessionID: "session-start"}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-start",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the assembled run: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want one completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		if turn.InvocationID != "inv-start" {
			t.Fatalf("turn invocation = %q, want the invocation the drive was admitted for", turn.InvocationID)
		}
		if tool.calls() != 1 {
			t.Fatalf("tool executions = %d, want the admitted call executed exactly once", tool.calls())
		}
		if requests := provider.recorded(); len(requests) != 2 {
			t.Fatalf("model requests = %d, want one request per decision", len(requests))
		}
	})

	t.Run("the diagnostic query reads the live scene through the query projection under the caller's scope", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, _, _ := newEntryRun(t, ctx)
		addr := event.Address{AgentID: "agent-a", SessionID: "session-query"}
		if _, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-query",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		}); err != nil {
			t.Fatalf("invoke the assembled run: %v", err)
		}
		scope := query.AuthorizationScope{
			Agents:   []event.AgentID{"agent-a"},
			Sessions: []event.Address{addr},
		}
		snapshot, err := run.Query(ctx, query.ListFilter{Scope: scope})
		if err != nil {
			t.Fatalf("query the live scene: %v", err)
		}
		if snapshot.Revision < 1 {
			t.Fatalf("snapshot revision = %d, want the projection to anchor every read at one", snapshot.Revision)
		}
		if len(snapshot.Watermarks) != 1 || snapshot.Watermarks[0].LagState != query.WatermarkLagUnknown {
			t.Fatalf("watermarks = %+v, want the single in-memory source beside its unmeasured lag", snapshot.Watermarks)
		}
		byKind := map[query.ObjectKind]int{}
		for _, detail := range snapshot.Details {
			byKind[detail.Ref.Kind]++
		}
		for _, kind := range []query.ObjectKind{query.ObjectAgent, query.ObjectSession, query.ObjectTask, query.ObjectNotice} {
			if byKind[kind] == 0 {
				t.Fatalf("detail counts = %v, want the %s dimension read from its owner", byKind, kind)
			}
		}
		leaseKinds := map[string]string{}
		for _, detail := range snapshot.Details {
			if detail.Ref.Kind == query.ObjectLease {
				leaseKinds[detail.Ref.Lease.Kind] = detail.Ref.Lease.Key
			}
		}
		wantLeases := map[string]string{"model": "model-a", "toolset": "agent-a", "filesource": "agent-a", "plugins": "agent-a"}
		if !reflect.DeepEqual(leaseKinds, wantLeases) {
			t.Fatalf("lease entries = %v, want the resource registrations of the generation in effect %v", leaseKinds, wantLeases)
		}
		if derived := len(snapshot.Details); derived != byKind[query.ObjectAgent]+byKind[query.ObjectSession]+byKind[query.ObjectInvocation]+byKind[query.ObjectTask]+byKind[query.ObjectNotice]+byKind[query.ObjectLease] {
			t.Fatalf("detail entries = %d, want every count derived from the entries themselves", derived)
		}
		tasks, err := run.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectTask}})
		if err != nil {
			t.Fatalf("query the task dimension: %v", err)
		}
		for _, detail := range tasks.Details {
			if detail.Ref.Kind != query.ObjectTask {
				t.Fatalf("filtered detail = %+v, want only the requested kind served", detail.Ref)
			}
		}
		if len(tasks.Details) != byKind[query.ObjectTask] {
			t.Fatalf("filtered tasks = %d, want the %d task entries of the same scene", len(tasks.Details), byKind[query.ObjectTask])
		}
		empty, err := run.Query(ctx, query.ListFilter{})
		if err != nil {
			t.Fatalf("query under an empty scope: %v", err)
		}
		if len(empty.Details) != 0 {
			t.Fatalf("details under an empty scope = %+v, want nothing readable without a grant", empty.Details)
		}
	})

	t.Run("the close refuses new input first and reports an honest drain of everything it settled", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, _, _ := newEntryRun(t, ctx)
		addr := event.Address{AgentID: "agent-a", SessionID: "session-close"}
		if _, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-close",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		}); err != nil {
			t.Fatalf("invoke the assembled run: %v", err)
		}
		report, err := run.Close(ctx, agent.CloseRequest{Address: addr, Deadline: time.Now().Add(5 * time.Second)})
		if err != nil || !report.Drained {
			t.Fatalf("close = (%+v, %v), want the drain reached real termination", report, err)
		}
		if len(report.Unsettled) != 0 {
			t.Fatalf("unsettled obligations = %+v, want every accepted obligation settled", report.Unsettled)
		}
		if _, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-after-close",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "late"}},
		}); !errors.Is(err, agent.ErrShutdownRefused) {
			t.Fatalf("invoke after the close = %v, want the named refusal of the closed gate", err)
		}
		repeat, err := run.Close(ctx, agent.CloseRequest{Address: addr, Deadline: time.Now().Add(5 * time.Second)})
		if err != nil {
			t.Fatalf("repeated close: %v", err)
		}
		if !reflect.DeepEqual(repeat, report) {
			t.Fatalf("repeated close = %+v, want the first result returned as it stands", repeat)
		}
	})

	t.Run("the thin CLI shell drives the same assembled run and builds no second runtime", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		run, provider, _ := newEntryRun(t, ctx)
		addr := event.Address{AgentID: "agent-a", SessionID: "session-cli"}
		if _, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-cli-sdk",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		}); err != nil {
			t.Fatalf("invoke the assembled run: %v", err)
		}
		scope := query.AuthorizationScope{
			Agents:   []event.AgentID{"agent-a"},
			Sessions: []event.Address{addr},
		}
		cli := tagent.NewCLI(run, scope, tagent.WithRequestCredential(entryCredential))

		// The query command reports the very reading the SDK serves.
		var stdout, stderr bytes.Buffer
		if code := cli.Main(ctx, []string{"query"}, &stdout, &stderr); code != 0 {
			t.Fatalf("cli query exit = %d, stderr = %s", code, stderr.String())
		}
		var served query.Snapshot
		if err := json.Unmarshal(stdout.Bytes(), &served); err != nil {
			t.Fatalf("decode the cli reading: %v", err)
		}
		sdk, err := run.Query(ctx, query.ListFilter{Scope: scope})
		if err != nil {
			t.Fatalf("query through the sdk: %v", err)
		}
		if got, want := kindCounts(served), kindCounts(sdk); !reflect.DeepEqual(got, want) {
			t.Fatalf("cli reading = %v, want the same derived counts as the sdk reading %v", got, want)
		}

		stdout.Reset()
		stderr.Reset()
		code := cli.Main(ctx, []string{
			"invoke", "-agent", "agent-a", "-session", "session-cli",
			"-invocation", "inv-cli", "-text", "hello again",
		}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("cli invoke exit = %d, stderr = %s", code, stderr.String())
		}
		var outcome struct {
			InvocationID event.InvocationID `json:"invocation_id"`
			Terminal     model.Kind         `json:"terminal"`
			Decisions    int                `json:"decisions"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &outcome); err != nil {
			t.Fatalf("decode the cli turn: %v", err)
		}
		if outcome.Terminal != model.KindCompleted || outcome.Decisions != 2 || outcome.InvocationID != "inv-cli" {
			t.Fatalf("cli turn = %+v, want one completed turn of two decisions under its own invocation", outcome)
		}

		requests := provider.recorded()
		seen := map[string]bool{}
		for _, req := range requests {
			if len(req.Messages) == 0 {
				t.Fatalf("request messages = %+v, want the drive's own material at the end of its request", req.Messages)
			}
			tail := req.Messages[len(req.Messages)-1]
			if tail.Role != model.RoleUser {
				t.Fatalf("request tail = %+v, want the invocation's own input or its settled observation at the end of its request", tail)
			}
			seen[tail.Content] = true
			if tail.Content == "hello again" {
				history := false
				for _, msg := range req.Messages[:len(req.Messages)-1] {
					if msg.Role == model.RoleUser && msg.Content == "hello" {
						history = true
					}
				}
				if !history {
					t.Fatalf("request messages = %+v, want the committed history of the session in front of its own input", req.Messages)
				}
			}
		}
		if !seen["hello"] || !seen["hello again"] {
			t.Fatalf("request tails = %v, want the input of each invocation at the end of its own request", seen)
		}

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		serveCtx, stopServe := context.WithCancel(ctx)
		serving := make(chan error, 1)
		go func() { serving <- cli.Serve(serveCtx, listener) }()
		base := "http://" + listener.Addr().String()
		if body := httpGet(t, base+"/healthz", entryCredential); body != "ok" {
			t.Fatalf("healthz = %q, want the liveness probe answered", body)
		}
		page := httpGet(t, base+"/query", entryCredential)
		var overHTTP query.Snapshot
		if err := json.Unmarshal([]byte(page), &overHTTP); err != nil {
			t.Fatalf("decode the http reading: %v", err)
		}
		sdk, err = run.Query(ctx, query.ListFilter{Scope: scope})
		if err != nil {
			t.Fatalf("query beside the http reading: %v", err)
		}
		if got, want := kindCounts(overHTTP), kindCounts(sdk); !reflect.DeepEqual(got, want) {
			t.Fatalf("http reading = %v, want the same derived counts as the sdk reading %v", got, want)
		}
		stopServe()
		if err := <-serving; err != nil {
			t.Fatalf("serve: %v", err)
		}
	})
}

// newEntryRun assembles one runtime through the sole assembly entry: a
// scripted native model beside one native echo tool, driven over a declaration
// naming the single agent.
func newEntryRun(t *testing.T, ctx context.Context) (*tagent.Run, *entryModel, *entryTool) {
	t.Helper()
	provider := &entryModel{name: "model-a", answer: func(req *model.Request) *model.Response {
		if last := req.Messages[len(req.Messages)-1]; last.Content == "hello" || last.Content == "hello again" {
			return entryToolTurn(entryCall("call-1", "echo", `{"x":1}`))
		}
		return entryFinalTurn("settled")
	}}
	tool := &entryTool{name: "echo", output: "out-a"}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run, provider, tool
}

// kindCounts derives the per-kind count of one reading from its detail
// entries, exactly the way a host page derives its numbers.
func kindCounts(snapshot query.Snapshot) map[query.ObjectKind]int {
	out := map[query.ObjectKind]int{}
	for _, detail := range snapshot.Details {
		out[detail.Ref.Kind]++
	}
	return out
}

// entryCredential is the request credential the HTTP face of the CLI shell
// under test authenticates its request subjects with.
const entryCredential = "entry-test-credential"

// httpGet reads one response body over the host HTTP face, authenticating the
// request subject with the configured credential, and fails the test when the
// endpoint does not answer.
func httpGet(t *testing.T, url, credential string) string {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatalf("build the request for %s: %v", url, err)
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get %s: status = %d, want 200", url, resp.StatusCode)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return buf.String()
}

// entryModel is the scripted native model stand-in: it records every request
// it is driven with and answers with the scripted response. It never reaches a
// real endpoint.
type entryModel struct {
	mu       sync.Mutex
	name     string
	answer   func(req *model.Request) *model.Response
	requests []model.Request
}

// GenerateContent records the native request and streams the scripted answer.
func (m *entryModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	answer := m.answer
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- answer(req)
	close(ch)
	return ch, nil
}

// Info reports the scripted model's descriptor.
func (m *entryModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// recorded reports the native requests the model was driven with.
func (m *entryModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// entryTool is the native echo tool stand-in: it counts its executions and
// answers with its output.
type entryTool struct {
	mu     sync.Mutex
	name   string
	output string
	count  int
}

// Declaration returns the declaration naming the echo tool.
func (t *entryTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call executes the echo tool once and reports its output.
func (t *entryTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.count++
	return t.output, nil
}

// calls reports how often the echo tool executed.
func (t *entryTool) calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// entryCall builds one native tool call of a scripted assistant message.
func entryCall(id, name, args string) model.ToolCall {
	return model.ToolCall{
		ID:   id,
		Type: "function",
		Function: model.FunctionDefinitionParam{
			Name:      name,
			Arguments: []byte(args),
		},
	}
}

// entryToolTurn builds the scripted response carrying one native tool call.
func entryToolTurn(calls ...model.ToolCall) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: calls}}},
		Done:    true,
	}
}

// entryFinalTurn builds the scripted response carrying the final answer.
func entryFinalTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}
