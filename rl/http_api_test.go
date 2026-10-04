package rl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// messageRecordingLoop 是 AgentLoop 的测试替身：不实现信封注入契约，逐条记录收到的消息。
type messageRecordingLoop struct {
	mu       sync.Mutex
	injected []model.Message
	active   bool
}

func (l *messageRecordingLoop) InjectMessage(msg model.Message) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.injected = append(l.injected, msg)
}

func (l *messageRecordingLoop) InjectMessageWithSource(_ string, msg model.Message) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.injected = append(l.injected, msg)
}

func (l *messageRecordingLoop) StartLoop(string, string) (<-chan *trpcEvent.Event, error) {
	return nil, nil
}

func (l *messageRecordingLoop) StopLoop()          {}
func (l *messageRecordingLoop) IsLoopActive() bool { return l.active }

func (l *messageRecordingLoop) snapshot() []model.Message {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]model.Message(nil), l.injected...)
}

func sendTask(t *testing.T, h *HTTPAPI, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(body)))
	return rec
}

func errorType(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body["error"]
}

// TestHTTPAPI_Healthz_ReportsLoopState 钉住 GET /healthz 的回报形状：status 恒为 ok，loop_active 镜像 IsLoopActive。
//
// 契约: docs/wiki/rl/rl-architecture.md#http-api
func TestHTTPAPI_Healthz_ReportsLoopState(t *testing.T) {
	for _, active := range []bool{false, true} {
		h := NewHTTPAPI(&messageRecordingLoop{active: active})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		require.Equal(t, http.StatusOK, rec.Code, "active=%v", active)

		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "ok", body["status"], "active=%v", active)
		assert.Equal(t, active, body["loop_active"], "loop_active must mirror IsLoopActive")
	}
}

// TestHTTPAPI_PostTask_LoopInactive 钉住循环未激活时 POST /task 拒绝为 503 loop_not_active，且不得注入任何消息。
func TestHTTPAPI_PostTask_LoopInactive(t *testing.T) {
	l := &messageRecordingLoop{active: false}
	rec := sendTask(t, NewHTTPAPI(l), `{"messages":[{"role":"user","content":"hi"}]}`)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	assert.Equal(t, "loop_not_active", errorType(t, rec))
	assert.Empty(t, l.snapshot(), "rejected request must not inject")
}

// TestHTTPAPI_PostTask_EmptyMessages 钉住 messages 数组为空是请求错误 400，不与超限的 413 混用状态码。
func TestHTTPAPI_PostTask_EmptyMessages(t *testing.T) {
	l := &messageRecordingLoop{active: true}
	rec := sendTask(t, NewHTTPAPI(l), `{"messages":[]}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Equal(t, "request_rejected", errorType(t, rec))
	assert.Empty(t, l.snapshot())
}

// TestHTTPAPI_PostTask_MalformedBody 钉住请求体不是合法 JSON 时返回 400，不泄漏为 5xx。
func TestHTTPAPI_PostTask_MalformedBody(t *testing.T) {
	l := &messageRecordingLoop{active: true}
	rec := sendTask(t, NewHTTPAPI(l), "not json")
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Equal(t, "request_rejected", errorType(t, rec))
	assert.Empty(t, l.snapshot())
}

// TestHTTPAPI_PostTask_WithoutEnvelopeSupport 钉住未实现信封注入的 AgentLoop 走逐条注入通路：整批按序到达、空 role 归一为 user、回执不含批次身份。
//
// 契约: docs/wiki/rl/rl-architecture.md#http-api
func TestHTTPAPI_PostTask_WithoutEnvelopeSupport(t *testing.T) {
	l := &messageRecordingLoop{active: true}
	rec := sendTask(t, NewHTTPAPI(l), `{"messages":[{"role":"system","content":"s"},{"content":"u"}]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	got := l.snapshot()
	require.Len(t, got, 2, "every message in the batch must be injected")
	assert.Equal(t, model.RoleSystem, got[0].Role)
	assert.Equal(t, "s", got[0].Content)
	assert.Equal(t, model.RoleUser, got[1].Role, "empty role must normalize to user")
	assert.Equal(t, "u", got[1].Content)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "accepted", body["status"])
	assert.NotContains(t, rec.Body.String(), "request_id", "per-message path has no batch identity")
}

// TestHTTPAPI_UnregisteredRoutes 钉住未注册的路由与方法一律 404 not_found；轨迹读取路径 /trajectories 与 /trajectory/{key} 不提供服务。
func TestHTTPAPI_UnregisteredRoutes(t *testing.T) {
	h := NewHTTPAPI(&messageRecordingLoop{active: true})
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/unknown"},
		{http.MethodGet, "/trajectories"},
		{http.MethodGet, "/trajectory/some-key"},
		{http.MethodDelete, "/task"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, http.StatusNotFound, rec.Code, "%s %s", tc.method, tc.path)
		assert.Equal(t, "not_found", errorType(t, rec), "%s %s", tc.method, tc.path)
	}
}

// fakeLoop satisfies AgentLoop for auth-path tests (only routing past the
// auth point matters — handlers get a live loop).
type fakeLoop struct{}

func (fakeLoop) InjectMessage(model.Message)                   {}
func (fakeLoop) InjectMessageWithSource(string, model.Message) {}
func (fakeLoop) StartLoop(string, string) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, 1)
	return ch, nil
}
func (fakeLoop) StopLoop()          {}
func (fakeLoop) IsLoopActive() bool { return true }

// TestHTTPAPI_Auth_401WithoutOrWrongToken 钉住鉴权单点：无 token 或 token 不符一律 401，只读端点也无豁免。
//
// 契约: docs/wiki/rl/rl-architecture.md#http-api
func TestHTTPAPI_Auth_401WithoutOrWrongToken(t *testing.T) {
	api := NewHTTPAPI(nil)
	api.SetAuthToken("secret")

	for name, header := range map[string]string{
		"no header":    "",
		"wrong scheme": "Basic secret",
		"wrong token":  "Bearer wrong",
		"prefix only":  "Bearer",
	} {
		req := httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(`{}`))
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", name, rec.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["error"] != "unauthorized" {
			t.Fatalf("%s: body error = %v, want unauthorized", name, body["error"])
		}
	}
}

func TestHTTPAPI_Auth_CorrectTokenReachesHandler(t *testing.T) {
	api := NewHTTPAPI(fakeLoop{})
	api.SetAuthToken("secret")

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("correct token must not be 401 (got %d)", rec.Code)
	}
}

func TestHTTPAPI_Auth_HealthzNotExempt(t *testing.T) {
	api := NewHTTPAPI(nil)
	api.SetAuthToken("secret")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/healthz must NOT be exempt: status = %d, want 401", rec.Code)
	}
}

// TestValidateListenAddr 钉住 loopback fail-closed：无 token 时非回环地址拒绝启动且错误列出出路。
func TestValidateListenAddr(t *testing.T) {
	cases := []struct {
		name    string
		addr    string
		token   string
		wantErr bool
	}{
		{"token set allows any addr", "0.0.0.0:8089", "tok", false},
		{"loopback ipv4 ok", "127.0.0.1:8089", "", false},
		{"loopback ipv6 ok", "[::1]:8089", "", false},
		{"localhost ok", "localhost:8089", "", false},
		{"all interfaces no token", ":8089", "", true},
		{"explicit wildcard no token", "0.0.0.0:8089", "", true},
		{"lan ip no token", "192.168.1.5:8089", "", true},
	}
	for _, tc := range cases {
		err := ValidateListenAddr(tc.addr, tc.token)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tc.name, err, tc.wantErr)
		}
		if tc.wantErr && !strings.Contains(err.Error(), "TAGENT_RL_AUTH_TOKEN") {
			t.Fatalf("%s: error must list the fix paths, got: %v", tc.name, err)
		}
	}
}

// TestAuthTokenFromEnv 钉住 token 的环境变量读取通路。
func TestAuthTokenFromEnv(t *testing.T) {
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "env-tok")
	if got := AuthTokenFromEnv(); got != "env-tok" {
		t.Fatalf("AuthTokenFromEnv = %q", got)
	}
}

// TestDiagnostics_WiredAndNotWired 钉住注入构造器则 200＋JSON、未注入则 404 显式；本文件承载该接口关闭期回归（诊断、长轮询与桥接、上限校验、信封回执、端点策略与逐跳守卫）。
//
// 契约: docs/wiki/rl/rl-architecture.md#http-api
func TestDiagnostics_WiredAndNotWired(t *testing.T) {
	h := NewHTTPAPI(nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/diagnostics", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)

	h2 := NewHTTPAPI(nil)
	h2.SetDiagnosticsFn(func() any {
		return map[string]any{"engine_ready": false}
	})
	rec2 := httptest.NewRecorder()
	h2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/diagnostics", nil))
	require.Equal(t, http.StatusOK, rec2.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &body))
	require.Contains(t, body, "engine_ready")
}

// TestFeedbackWait_ImmediateWake 钉住：入队后 wait 立即返回而不等满超时（通知通道若为 nil 这条通路就是死代码）。
func TestFeedbackWait_ImmediateWake(t *testing.T) {
	h := NewHTTPAPI(nil)
	h.fbEnqueue(map[string]any{"verdict": "negative"})

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/feedback/wait?timeout=5", nil))
		done <- rec
	}()
	select {
	case rec := <-done:
		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Items, 1)
		require.Equal(t, "negative", body.Items[0]["verdict"])
	case <-time.After(3 * time.Second):
		t.Fatal("F1 regression: wait 未被唤醒（fbNotify nil 或入队未通知）——立即返回语义失效")
	}
}

// TestFeedbackBridge_PostWakesWait 钉住走真链路（落库＋因果边＋入队通知）能唤醒长轮询并带回该条，区别于直注入。
func TestFeedbackBridge_PostWakesWait(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("rl-bridge")
	parentKey := memory.NewSnowflakeEventKey(pid, 0)
	if err := store.StoreEvent(parentKey, memory.FullEvent{
		EventKey: parentKey, PartitionID: pid,
		EventType: tagentevent.TypeExternalInput, Timestamp: 1700000000000,
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}

	h := NewHTTPAPI(nil)
	h.SetFeedbackStore(store)

	waitDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/feedback/wait?timeout=5", nil))
		waitDone <- rec
	}()
	time.Sleep(50 * time.Millisecond)

	body := fmt.Sprintf(`{"event_key":%q,"verdict":"negative","note":"bridge regression"}`,
		tagentevent.FormatEventKey(parentKey))
	post := httptest.NewRequest(http.MethodPost, "/feedback", strings.NewReader(body))
	postRec := httptest.NewRecorder()
	h.ServeHTTP(postRec, post)
	if postRec.Code != http.StatusCreated {
		t.Fatalf("POST /feedback = %d: %s", postRec.Code, postRec.Body.String())
	}

	select {
	case rec := <-waitDone:
		if rec.Code != http.StatusOK {
			t.Fatalf("wait = %d", rec.Code)
		}
		var out struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(out.Items) != 1 || out.Items[0]["verdict"] != "negative" {
			t.Fatalf("wait items = %+v, want 1 negative", out.Items)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("POST 未唤醒 wait——桥接断路(F1 回归)")
	}
}

// envelopeInjectingLoop 是 AgentLoop 加信封注入契约的测试替身，并记录收到的信封。
type envelopeInjectingLoop struct {
	mu          sync.Mutex
	envelopes   [][]model.Message
	lastSource  string
	lastAttrs   []map[string]any
	injectErr   error
	injectCalls int
	active      bool
}

func (l *envelopeInjectingLoop) InjectMessage(model.Message)                   {}
func (l *envelopeInjectingLoop) InjectMessageWithSource(string, model.Message) {}
func (l *envelopeInjectingLoop) StartLoop(string, string) (<-chan *trpcEvent.Event, error) {
	return nil, nil
}
func (l *envelopeInjectingLoop) StopLoop()          {}
func (l *envelopeInjectingLoop) IsLoopActive() bool { return l.active }

func (l *envelopeInjectingLoop) InjectEnvelope(_ context.Context, source string, msgs []model.Message, attrs ...map[string]any) (string, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.injectCalls++
	if l.injectErr != nil {
		return "", false, l.injectErr
	}
	l.envelopes = append(l.envelopes, msgs)
	l.lastSource = source
	l.lastAttrs = attrs
	return "req-wp4-1", true, nil
}

// declaredLineage returns the trigger_source the HTTP layer passed down as an
// envelope attr ("" when none was stamped).
func (l *envelopeInjectingLoop) declaredLineage() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, a := range l.lastAttrs {
		if v, ok := a["trigger_source"].(string); ok {
			return v
		}
	}
	return ""
}

func postEnvelopeBody(t *testing.T, h *HTTPAPI, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestTask_TriggerSourceDeclaration 钉住 /task 意图声明的受理与盖章形态。
// - 声明 user 经认证端点受理：血统作为信封 attr 下传，机械通道标签 Source 仍为 "http"；
// - 缺省不声明：不盖章，行为与声明能力引入前同形；
// - 非 "user" 值与未认证声明分别 400，被拒声明不得注入。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestTask_TriggerSourceDeclaration(t *testing.T) {
	t.Run("accepted and stamped", func(t *testing.T) {
		l := &envelopeInjectingLoop{active: true}
		h := NewHTTPAPI(l)
		h.SetAuthToken("tok")
		req := httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}],"trigger_source":"user"}`))
		req.Header.Set("Authorization", "Bearer tok")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		require.Equal(t, http.StatusAccepted, rec.Code)
		require.Equal(t, "user", l.declaredLineage(), "declared intent must reach the envelope attrs")
		require.Equal(t, "http", l.lastSource, "the mechanical channel label must survive")
	})
	t.Run("absent means no stamp", func(t *testing.T) {
		l := &envelopeInjectingLoop{active: true}
		h := NewHTTPAPI(l)
		rec := postEnvelopeBody(t, h, `{"messages":[{"role":"user","content":"hello"}]}`)
		require.Equal(t, http.StatusAccepted, rec.Code)
		require.Equal(t, "", l.declaredLineage())
	})
	t.Run("non-user value rejected", func(t *testing.T) {
		l := &envelopeInjectingLoop{active: true}
		h := NewHTTPAPI(l)
		h.SetAuthToken("tok")
		req := httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}],"trigger_source":"meditation"}`))
		req.Header.Set("Authorization", "Bearer tok")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, 0, l.injectCalls, "a rejected declaration must not inject")
	})
	t.Run("declaration without auth rejected", func(t *testing.T) {
		l := &envelopeInjectingLoop{active: true}
		h := NewHTTPAPI(l)
		rec := postEnvelopeBody(t, h, `{"messages":[{"role":"user","content":"hi"}],"trigger_source":"user"}`)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "declaration_requires_auth")
		require.Equal(t, 0, l.injectCalls)
	})
}

// TestTask_DeclarationUnsupportedBuildRejects 钉住 缺信封能力的 agent 构建下声明被拒而非静默丢弃。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestTask_DeclarationUnsupportedBuildRejects(t *testing.T) {
	h := NewHTTPAPI(&fakeLoop{})
	h.SetAuthToken("tok")
	req := httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}],"trigger_source":"user"}`))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotImplemented, rec.Code)
	require.Contains(t, rec.Body.String(), "declaration_unsupported")
}

// TestTask_Limits_SingleValidationPoint 钉住：请求上限集中一处校验，负值配置被拒绝。
func TestTask_Limits_SingleValidationPoint(t *testing.T) {
	l := &envelopeInjectingLoop{active: true}
	h := NewHTTPAPI(l)
	require.NoError(t, h.SetLimits(HTTPAPILimits{MaxBodyBytes: 1024, MaxMessages: 2, MaxContentBytes: 64, MaxFeedbackQueue: 4}))
	require.Error(t, h.SetLimits(HTTPAPILimits{MaxMessages: -1}), "negative limit must be rejected")

	big := `{"messages":[{"role":"user","content":"` + strings.Repeat("x", 2048) + `"}]}`
	rec := postEnvelopeBody(t, h, big)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())

	rec = postEnvelopeBody(t, h, `{"messages":[{"role":"user","content":"a"},{"role":"user","content":"b"},{"role":"user","content":"c"}]}`)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())

	rec = postEnvelopeBody(t, h, `{"messages":[{"role":"user","content":"`+strings.Repeat("y", 65)+`"}]}`)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())

	rec = postEnvelopeBody(t, h, `{"messages":[{"role":"assistant","content":"hi"}]}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	rec = postEnvelopeBody(t, h, `{"messages":[{"content":"hi"}]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}

// TestTask_EnvelopeReceipt 钉住：整批是一个信封回执（带批次身份与持久性），注入失败不得返回 202。
func TestTask_EnvelopeReceipt(t *testing.T) {
	l := &envelopeInjectingLoop{active: true}
	h := NewHTTPAPI(l)
	rec := postEnvelopeBody(t, h, `{"messages":[{"role":"user","content":"a"},{"role":"user","content":"b"}]}`)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Contains(t, rec.Body.String(), `"request_id":"req-wp4-1"`)
	require.Contains(t, rec.Body.String(), `"durable":true`)
	require.Len(t, l.envelopes, 1)
	require.Len(t, l.envelopes[0], 2, "whole batch in one envelope")

	l.injectErr = errors.New("inbox full")
	rec = postEnvelopeBody(t, h, `{"messages":[{"role":"user","content":"a"}]}`)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.NotContains(t, rec.Body.String(), `"status":"accepted"`)
}

// TestFeedback_OverflowDroppedCount 钉住：队列溢出按最旧丢弃并计数，供等待方回报响应不完整。
func TestFeedback_OverflowDroppedCount(t *testing.T) {
	l := &envelopeInjectingLoop{active: true}
	h := NewHTTPAPI(l)
	store := memory.NewInMemoryStore()
	h.SetFeedbackStore(store)
	require.NoError(t, h.SetLimits(HTTPAPILimits{MaxFeedbackQueue: 2}))
	for i := 0; i < 5; i++ {
		pid := memory.PartitionIDFromName(fmt.Sprintf("fbtest%d", i))
		pk := memory.NewSnowflakeEventKey(pid, int64(1758000000000+i*1000))
		require.NoError(t, store.StoreEvent(pk, memory.FullEvent{
			EventKey: pk, PartitionID: pid,
			EventType: tagentevent.TypeAgentOutput, Timestamp: int64(1758000000000 + i*1000),
		}))
		key := tagentevent.FormatEventKey(pk)
		req := httptest.NewRequest(http.MethodPost, "/feedback", strings.NewReader(`{"event_key":"`+key+`","verdict":"good"}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
	require.EqualValues(t, 3, h.fbDropped.Load(), "oldest-first eviction counted")
}

// TestFeedbackWait_ClientCancel 钉住：客户端取消立即返回，不占住 handler 到超时。
func TestFeedbackWait_ClientCancel(t *testing.T) {
	l := &envelopeInjectingLoop{active: true}
	h := NewHTTPAPI(l)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/feedback/wait?timeout=30", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(rec, req); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled wait did not return promptly")
	}
}

// TestEndpoint_PolicyAndSerialization 钉住：端点重定向默认关闭、白名单外拒绝、重建失败整批 502 且不注入。
func TestEndpoint_PolicyAndSerialization(t *testing.T) {
	l := &envelopeInjectingLoop{active: true}
	h := NewHTTPAPI(l)
	body := `{"messages":[{"role":"user","content":"hi"}],"llm_base_url":"http://proxy.example:9/v1"}`

	rec := postEnvelopeBody(t, h, body)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// handler installed but redirect disabled by default → 400
	var updated string
	require.NoError(t, h.SetLimits(DefaultHTTPAPILimits()))
	h.SetModelUpdateFnE(func(u string) error { updated = u; return nil })
	rec = postEnvelopeBody(t, h, body)
	require.Equal(t, http.StatusBadRequest, rec.Code, "redirect disabled by default")
	require.Empty(t, updated)
	require.Equal(t, 0, l.injectCalls, "rejected batch must not inject")

	h.SetEndpointPolicy(true, []string{"proxy.allowed.example"})
	rec = postEnvelopeBody(t, h, body)
	require.Equal(t, http.StatusBadRequest, rec.Code, "host not allowlisted")
	require.Empty(t, updated)

	ok := `{"messages":[{"role":"user","content":"hi"}],"llm_base_url":"http://proxy.allowed.example:9/v1"}`
	rec = postEnvelopeBody(t, h, ok)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Equal(t, "http://proxy.allowed.example:9/v1", updated)
	require.Equal(t, 1, l.injectCalls)

	h.SetModelUpdateFnE(func(u string) error { return errors.New("provider 500") })
	rec = postEnvelopeBody(t, h, ok)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, 1, l.injectCalls, "failed endpoint update must not inject")
}

// TestPostFeedback 钉住反馈绑定的三种回报：未接线 503、父缺失 404、成功 201 并留下因果边。
func TestPostFeedback(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("tagent")
	parentKey := memory.NewSnowflakeEventKey(pid, 1750000000000)
	_ = store.StoreEvent(parentKey, memory.FullEvent{EventKey: parentKey, PartitionID: pid})

	api := NewHTTPAPI(nil)
	api.SetFeedbackStore(store)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/feedback", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		api.ServeHTTP(w, req)
		return w
	}

	fresh := NewHTTPAPI(nil)
	w := httptest.NewRecorder()
	fresh.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/feedback", bytes.NewBufferString(`{}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired = %d, want 503", w.Code)
	}

	body, _ := json.Marshal(map[string]any{
		"event_key": tagentevent.FormatEventKey(parentKey), "verdict": "positive", "note": "good",
	})
	if w := post(string(body)); w.Code != http.StatusCreated {
		t.Fatalf("happy = %d: %s", w.Code, w.Body.String())
	}
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Limit: 10})
	if err != nil || len(refs) != 2 {
		t.Fatalf("expected parent+feedback, got %d refs err=%v", len(refs), err)
	}

	if w := post(`{"event_key":"7fffffffffff","verdict":"negative"}`); w.Code != http.StatusNotFound {
		t.Fatalf("ghost = %d, want 404", w.Code)
	}
	if w := post(`{"verdict":"positive"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing fields = %d, want 400", w.Code)
	}
}

func redirectReq(t *testing.T, target string) (*http.Request, []*http.Request) {
	t.Helper()
	initial := httptest.NewRequest("POST", "http://proxy.allowed.example/v1/chat/completions", nil)
	req := httptest.NewRequest("GET", target, nil)
	return req, []*http.Request{initial}
}

func TestEndpointRedirectPolicy_HopSemantics(t *testing.T) {
	pol := EndpointRedirectPolicy([]string{"Proxy.Allowed.Example"})

	req, via := redirectReq(t, "http://proxy.allowed.example:8443/v1/chat")
	if err := pol(req, via); err != nil {
		t.Errorf("allowlisted hop must pass, got %v", err)
	}

	req, via = redirectReq(t, "http://internal.metadata.host/latest/meta-data")
	err := pol(req, via)
	if err == nil {
		t.Fatal("out-of-allowlist hop must be rejected")
	}
	if !strings.Contains(err.Error(), "internal.metadata.host") || !strings.Contains(err.Error(), "allowlist") {
		t.Errorf("rejection must name host + allowlist reason: %v", err)
	}

	off := EndpointRedirectPolicy(nil)
	req, via = redirectReq(t, "http://proxy.allowed.example/next")
	if err := off(req, via); err == nil {
		t.Error("empty allowlist must reject all redirects")
	}
}

func TestGuardedClient_EmptyAllowlistRefusesHopWithoutRequest(t *testing.T) {
	var hops int32
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hops, 1)
		if n == 1 {
			http.Redirect(w, r, "http://tagent-redirect-canary.invalid/stage", http.StatusFound)
			return
		}
		w.Write([]byte("served"))
	}))
	defer front.Close()

	client := NewEndpointGuardedClient(nil)
	_, err := client.Get(front.URL)
	if err == nil {
		t.Fatal("expected redirect refusal error")
	}
	if !strings.Contains(err.Error(), "not in endpoint allowlist") {
		t.Errorf("must fail with the policy error, got: %v", err)
	}
	if atomic.LoadInt32(&hops) != 1 {
		t.Errorf("front must be hit exactly once (hop never followed), got %d", hops)
	}
}

func TestGuardedClient_AllowlistedHopChainSucceeds(t *testing.T) {
	var finalHits int32
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&finalHits, 1)
		w.Write([]byte("done"))
	}))
	defer final.Close()
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer front.Close()

	client := NewEndpointGuardedClient([]string{"127.0.0.1"})
	resp, err := client.Get(front.URL)
	if err != nil {
		t.Fatalf("allowlisted hop must succeed: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 8)
	n, _ := resp.Body.Read(buf)
	if string(buf[:n]) != "done" || atomic.LoadInt32(&finalHits) != 1 {
		t.Errorf("redirect must land on final server, body=%q hits=%d", buf[:n], finalHits)
	}
}

// TestEndpointRedirectPolicy_HopCap 钉住：自定义 CheckRedirect 顶掉了标准库的跳数上界，策略必须重施并对 ping-pong 显式失败。
func TestEndpointRedirectPolicy_HopCap(t *testing.T) {
	pol := EndpointRedirectPolicy([]string{"proxy.allowed.example"})
	initial := httptest.NewRequest("POST", "http://proxy.allowed.example/v1/chat", nil)
	req := httptest.NewRequest("GET", "http://proxy.allowed.example/again", nil)

	via := make([]*http.Request, 0, 12)
	for i := 0; i < maxRedirectHops-1; i++ {
		via = append(via, initial)
	}
	if err := pol(req, via); err != nil {
		t.Errorf("hop %d (below cap) must pass, got %v", len(via), err)
	}
	via = append(via, initial)
	err := pol(req, via)
	if err == nil {
		t.Fatal("hop at the cap must be rejected even for an allowlisted host")
	}
	if !strings.Contains(err.Error(), "hop cap exceeded") {
		t.Errorf("rejection must name the cap reason: %v", err)
	}
}

func TestNormalizeRedirectHost(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Proxy.Example.COM:8443", "proxy.example.com"},
		{"proxy.example.com", "proxy.example.com"},
		{"[::1]:9000", "::1"},
		{"[::1]", "::1"},
	} {
		if got := normalizeRedirectHost(tc.in); got != tc.want {
			t.Errorf("normalizeRedirectHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
