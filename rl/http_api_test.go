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
