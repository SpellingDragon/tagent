//go:build hotupdate_anchor

// 症状锚：运行中 Host.Reload 移动某 agent 的 exec 段且不重启。本文件只在显式带
// hotupdate_anchor 标签时编译，默认 go test ./assembly/ 不受影响。
//
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
package assembly

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
)

// anchorDriveInput is the one input both anchor invocations carry: the scripted
// provider answers its first decision with the exec_run call and its later
// decisions with a terminal, so each invocation drives exactly one execution.
const anchorDriveInput = "drive the execution resource"

// anchorProvider is the OpenAI-compatible endpoint the anchor boots against:
// the first decision of a transcript calls exec_run on the anchor session, and
// every decision after the tool observation ends the turn. Every request body
// is recorded, so the receipt the execution produced is read from what the
// provider actually served.
type anchorProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	bodies []map[string]json.RawMessage
	key    string
}

// newAnchorProvider starts the endpoint and closes it with the test.
func newAnchorProvider(t *testing.T, key string) *anchorProvider {
	t.Helper()
	p := &anchorProvider{key: key}
	p.server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.server.Close)
	return p
}

// serve answers one chat completion and records the body the request carried.
func (p *anchorProvider) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body map[string]json.RawMessage
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	p.mu.Lock()
	p.bodies = append(p.bodies, body)
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if anchorTranscriptSawTool(body) {
		fmt.Fprint(w, `{"id":"anchor","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
		return
	}
	args, err := json.Marshal(map[string]any{
		"command":  "sleep",
		"args":     []string{"120"},
		"kind":     tagentexec.KindTmux,
		"resource": p.key,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, `{"id":"anchor","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"run","tool_calls":[{"id":"call-anchor","type":"function","function":{"name":"exec_run","arguments":%s}}]},"finish_reason":"tool_calls"}]}`, args)
}

// anchorTranscriptSawTool reports whether the transcript the request carries
// already holds the observation of a tool call, so the next decision ends the
// turn instead of calling the execution a second time.
func anchorTranscriptSawTool(body map[string]json.RawMessage) bool {
	raw, ok := body["messages"]
	if !ok {
		return false
	}
	var messages []struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
	}
	if err := json.Unmarshal(raw, &messages); err != nil {
		return false
	}
	for _, message := range messages {
		if message.Role == "tool" {
			return true
		}
	}
	return false
}

// messageContents reports the message contents the provider served, in request
// order: the settled observation of a call rides the next request as user input
// material (the live observation renders under its call identity, not as a
// second tool answer), so the receipt of an execution is read from the whole
// transcript the provider actually saw.
func (p *anchorProvider) messageContents() []string {
	p.mu.Lock()
	bodies := append([]map[string]json.RawMessage(nil), p.bodies...)
	p.mu.Unlock()
	var out []string
	for _, body := range bodies {
		raw, ok := body["messages"]
		if !ok {
			continue
		}
		var messages []struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &messages); err != nil {
			continue
		}
		for _, message := range messages {
			var text string
			if err := json.Unmarshal(message.Content, &text); err == nil {
				out = append(out, text)
				continue
			}
			var parts []struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(message.Content, &parts); err == nil {
				for _, part := range parts {
					out = append(out, part.Text)
				}
			}
		}
	}
	return out
}

// anchorDocument renders the host document of the anchor: one entry agent
// grants the exec face under the given monitor interval.
func anchorDocument(root, endpoint, interval string) string {
	return "entry: user\n" +
		"provider: local\n" +
		"model: model-a\n" +
		"prompt_dir: " + root + "\n" +
		"request_timeout_seconds: 60\n" +
		"providers:\n" +
		"  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n" +
		"agents:\n" +
		"  user:\n" +
		"    tools:\n" +
		"      - { kind: tool, id: exec }\n" +
		"    exec:\n" +
		"      monitor:\n" +
		"        interval: " + interval + "\n" +
		"storage: { type: localfile, root: " + filepath.Join(root, "storage") + " }\n"
}

// anchorInvoke drives one invocation of the anchor agent at its own session.
func anchorInvoke(t *testing.T, ctx context.Context, host *Host, session string) {
	t.Helper()
	turn, err := host.Run().Invoke(ctx, InvokeRequest{
		InvocationID: event.InvocationID("inv-" + session),
		Address:      event.Address{AgentID: "user", SessionID: event.SessionID(session)},
		Input:        []model.Message{{Role: model.RoleUser, Content: anchorDriveInput}},
	})
	if err != nil {
		t.Fatalf("invoke the anchor agent: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("anchor turn terminal = %q, want completed", turn.Terminal)
	}
}

// TestHotReloadExecNoRestart is the exec symptom anchor: a running host moves one agent's exec section through the public Reload entry and does not restart.
//   - the reload publishes a new generation (published=true)
//   - the new generation's exec face serves the newly declared monitor rhythm
//   - a request already under way keeps the generation it borrowed, and the old generation's exec ledger keeps its resource until that hold releases
//   - the retired generation's exclusive exec resource releases exactly once after the drain, while the external tmux session survives
//   - the new generation re-probes the surviving session instead of rebuilding it
//
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func TestHotReloadExecNoRestart(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	if !tagentexec.IsTmuxAvailable() {
		t.Skip("the anchor drives the real tmux execution face; tmux is not on PATH")
	}
	key := fmt.Sprintf("hupc-anchor-%d", time.Now().UnixNano())
	provider := newAnchorProvider(t, key)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(anchorDocument(root, provider.server.URL+"/v1", "2s")), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	host, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer closeCancel()
		if err := host.Close(closeCtx); err != nil {
			t.Errorf("close the anchor host: %v", err)
		}
	})
	run := host.Run()
	resource := tagentexec.ResourceKey{Kind: tagentexec.KindTmux, Key: key}

	anchorInvoke(t, ctx, host, "session-anchor-start")
	explanation, err := run.ExecState(resource)
	if err != nil || !explanation.Held {
		t.Fatalf("the first generation holds no execution resource: (%+v, %v)", explanation, err)
	}
	if explanation.Rhythm.Interval != 2*time.Second {
		t.Fatalf("first-generation rhythm = %v, want the declared 2s", explanation.Rhythm.Interval)
	}

	held, err := run.publisher.Borrow(ctx, "user")
	if err != nil {
		t.Fatalf("borrow the first generation: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(anchorDocument(root, provider.server.URL+"/v1", "3s")), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	report, err := host.Reload(ctx)
	if err != nil {
		t.Fatalf("reload the moved exec section answered %v, want the change published without a restart", err)
	}
	if !report.Publication.Published || report.Publication.Generation != 2 {
		t.Fatalf("reload publication = %+v, want the second generation published", report.Publication)
	}
	registered := false
	for _, ref := range run.current.Leases {
		if ref.Kind == "gen" && ref.Key == "gen:2:exec" {
			registered = true
		}
	}
	if !registered {
		t.Fatalf("the published generation carries no gen:2:exec exclusive registration: %+v", run.current.Leases)
	}
	if held.Ref.Generation != 1 {
		t.Fatalf("the request under way borrowed generation %d, want the first generation it took", held.Ref.Generation)
	}
	explanation, err = run.ExecState(resource)
	if err != nil || !explanation.Held {
		t.Fatalf("the held generation released its resource before the drain: (%+v, %v)", explanation, err)
	}
	if explanation.Rhythm.Interval != 2*time.Second {
		t.Fatalf("the held generation serves rhythm %v, want its own declared 2s", explanation.Rhythm.Interval)
	}

	if err := held.Release(ctx); err != nil {
		t.Fatalf("release the held generation: %v", err)
	}
	if _, err := run.ExecState(resource); err == nil {
		t.Fatal("the retired generation still holds its resource after the drain, want the retirement release")
	}

	anchorInvoke(t, ctx, host, "session-anchor-reattach")
	reattached := false
	for _, observation := range provider.messageContents() {
		if strings.Contains(observation, "reattached "+resource.String()) {
			reattached = true
		}
	}
	if !reattached {
		t.Fatalf("no reattach receipt for %s, want the surviving session re-probed: %v", resource, provider.messageContents())
	}
	explanation, err = run.ExecState(resource)
	if err != nil || !explanation.Held {
		t.Fatalf("the new generation holds no re-attached resource: (%+v, %v)", explanation, err)
	}
	if explanation.Rhythm.Interval != 3*time.Second {
		t.Fatalf("new-generation rhythm = %v, want the newly declared 3s", explanation.Rhythm.Interval)
	}
}

// TestHotReloadExecFailClosed is the negative anchor of the exec probe.
//   - a candidate generation whose consumer build fails leaves the old generation in service
//   - the publication is refused and the old exec face keeps serving its resource
//   - the refused candidate releases its exclusive resource
//   - a preparation that fails after the consumers were built keeps the serving generation and puts back the faces it installed
//   - the restored face still serves: a new call re-probes the surviving session under the serving generation's own rhythm
//
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestHotReloadExecFailClosed(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	if !tagentexec.IsTmuxAvailable() {
		t.Skip("the anchor drives the real tmux execution face; tmux is not on PATH")
	}
	key := fmt.Sprintf("hupc-failclosed-%d", time.Now().UnixNano())
	provider := newAnchorProvider(t, key)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(anchorDocument(root, provider.server.URL+"/v1", "2s")), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	host, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer closeCancel()
		if err := host.Close(closeCtx); err != nil {
			t.Errorf("close the fail-closed host: %v", err)
		}
	})
	run := host.Run()
	resource := tagentexec.ResourceKey{Kind: tagentexec.KindTmux, Key: key}
	anchorInvoke(t, ctx, host, "session-failclosed-start")
	before := run.publisher.Current()

	if err := os.WriteFile(configPath, []byte(anchorDocument(root, provider.server.URL+"/v1", "-1s")), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	if _, err := host.Reload(ctx); err == nil {
		t.Fatal("the reload of an unbuildable exec candidate answered success, want the fail-closed refusal")
	}
	if got := run.publisher.Current(); got.Generation != before.Generation || got.Fingerprint != before.Fingerprint {
		t.Fatalf("publication after the refused candidate = %+v, want the generation %+v kept", got, before)
	}
	explanation, err := run.ExecState(resource)
	if err != nil || !explanation.Held {
		t.Fatalf("the old generation lost its resource after the refused candidate: (%+v, %v)", explanation, err)
	}
	if explanation.Rhythm.Interval != 2*time.Second {
		t.Fatalf("the old generation serves rhythm %v, want its declared 2s kept", explanation.Rhythm.Interval)
	}

	if err := os.WriteFile(configPath, []byte(anchorDocument(root, provider.server.URL+"/v1", "4s")), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	report, err := host.Reload(ctx)
	if err != nil {
		t.Fatalf("reload the repaired declaration: %v", err)
	}
	if !report.Publication.Published || report.Publication.Generation != before.Generation+1 {
		t.Fatalf("repaired reload publication = %+v, want the next generation published", report.Publication)
	}

	repaired := run.publisher.Current()
	broken := strings.Replace(anchorDocument(root, provider.server.URL+"/v1", "5s"),
		"storage:", "  other:\n    file:\n      base_dir: "+filepath.Join(root, "missing-base-dir")+"\nstorage:", 1)
	if err := os.WriteFile(configPath, []byte(broken), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
	if _, err := host.Reload(ctx); err == nil {
		t.Fatal("the reload of an unbuildable preparation answered success, want the fail-closed refusal")
	}
	if got := run.publisher.Current(); got.Generation != repaired.Generation || got.Fingerprint != repaired.Fingerprint {
		t.Fatalf("publication after the failed preparation = %+v, want the generation %+v kept", got, repaired)
	}
	anchorInvoke(t, ctx, host, "session-failclosed-verify")
	explanation, err = run.ExecState(resource)
	if err != nil || !explanation.Held {
		t.Fatalf("the serving generation holds no resource after the failed preparation: (%+v, %v)", explanation, err)
	}
	if explanation.Rhythm.Interval != 4*time.Second {
		t.Fatalf("the serving generation serves rhythm %v, want its declared 4s kept", explanation.Rhythm.Interval)
	}
}
