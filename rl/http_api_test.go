package rl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

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
