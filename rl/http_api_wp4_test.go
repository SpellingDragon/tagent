package rl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// wp4Loop implements AgentLoop plus the envelope injector contract (5.2).
type wp4Loop struct {
	mu          sync.Mutex
	envelopes   [][]model.Message
	injectErr   error
	injectCalls int
	active      bool
}

func (l *wp4Loop) InjectMessage(model.Message)                           {}
func (l *wp4Loop) InjectMessageWithSource(string, model.Message)         {}
func (l *wp4Loop) StartLoop(string, string) (<-chan *event.Event, error) { return nil, nil }
func (l *wp4Loop) StopLoop()                                             {}
func (l *wp4Loop) IsLoopActive() bool                                    { return l.active }

func (l *wp4Loop) InjectEnvelope(_ context.Context, _ string, msgs []model.Message) (string, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.injectCalls++
	if l.injectErr != nil {
		return "", false, l.injectErr
	}
	l.envelopes = append(l.envelopes, msgs)
	return "req-wp4-1", true, nil
}

func wp4Post(t *testing.T, h *HTTPAPI, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestTask_Limits_SingleValidationPoint（5.1）：body/messages/content 超限 413，
// 非法 role 400，负值 limits 被 SetLimits 拒绝。
func TestTask_Limits_SingleValidationPoint(t *testing.T) {
	l := &wp4Loop{active: true}
	h := NewHTTPAPI(l)
	require.NoError(t, h.SetLimits(HTTPAPILimits{MaxBodyBytes: 1024, MaxMessages: 2, MaxContentBytes: 64, MaxFeedbackQueue: 4}))
	require.Error(t, h.SetLimits(HTTPAPILimits{MaxMessages: -1}), "negative limit must be rejected")

	// body cap
	big := `{"messages":[{"role":"user","content":"` + strings.Repeat("x", 2048) + `"}]}`
	rec := wp4Post(t, h, big)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())

	// messages count cap
	rec = wp4Post(t, h, `{"messages":[{"role":"user","content":"a"},{"role":"user","content":"b"},{"role":"user","content":"c"}]}`)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())

	// per-message content cap
	rec = wp4Post(t, h, `{"messages":[{"role":"user","content":"`+strings.Repeat("y", 65)+`"}]}`)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())

	// illegal role
	rec = wp4Post(t, h, `{"messages":[{"role":"assistant","content":"hi"}]}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	// empty role normalizes to user — accepted
	rec = wp4Post(t, h, `{"messages":[{"content":"hi"}]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}

// TestTask_EnvelopeReceipt（5.2）：整批单 envelope——202 带 request_id/durable；
// 注入失败（closed/背压）必须非 202。
func TestTask_EnvelopeReceipt(t *testing.T) {
	l := &wp4Loop{active: true}
	h := NewHTTPAPI(l)
	rec := wp4Post(t, h, `{"messages":[{"role":"user","content":"a"},{"role":"user","content":"b"}]}`)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Contains(t, rec.Body.String(), `"request_id":"req-wp4-1"`)
	require.Contains(t, rec.Body.String(), `"durable":true`)
	require.Len(t, l.envelopes, 1)
	require.Len(t, l.envelopes[0], 2, "whole batch in one envelope")

	// rejection → non-202
	l.injectErr = errors.New("inbox full")
	rec = wp4Post(t, h, `{"messages":[{"role":"user","content":"a"}]}`)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.NotContains(t, rec.Body.String(), `"status":"accepted"`)
}

// TestFeedback_OverflowDroppedCount（5.4）：队列溢出丢最旧，wait 返回
// dropped_count；全量被逐出时 partial=true 提示全量重查。
func TestFeedback_OverflowDroppedCount(t *testing.T) {
	l := &wp4Loop{active: true}
	h := NewHTTPAPI(l)
	store := memory.NewInMemoryStore()
	h.SetFeedbackStore(store)
	require.NoError(t, h.SetLimits(HTTPAPILimits{MaxFeedbackQueue: 2}))
	for i := 0; i < 5; i++ {
		// Parent event must exist in the store or the feedback bind 404s;
		// per-item partition keeps feedback keys collision-free.
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

// TestFeedbackWait_ClientCancel（5.5）：客户端断开不得占住 handler——
// 取消的请求立即返回（不等到 30s 超时）。
func TestFeedbackWait_ClientCancel(t *testing.T) {
	l := &wp4Loop{active: true}
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

// TestEndpoint_PolicyAndSerialization（5.3）：默认禁用动态端点；allowlist 外
// 拒绝；FnE 失败 → 502 且整批不注入；成功 → 注入。旧 void 回调仍兼容。
func TestEndpoint_PolicyAndSerialization(t *testing.T) {
	l := &wp4Loop{active: true}
	h := NewHTTPAPI(l)
	body := `{"messages":[{"role":"user","content":"hi"}],"llm_base_url":"http://proxy.example:9/v1"}`

	// no handler installed → explicit 400
	rec := wp4Post(t, h, body)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// handler installed but redirect disabled by default → 400
	var updated string
	require.NoError(t, h.SetLimits(DefaultHTTPAPILimits()))
	h.SetModelUpdateFnE(func(u string) error { updated = u; return nil })
	rec = wp4Post(t, h, body)
	require.Equal(t, http.StatusBadRequest, rec.Code, "redirect disabled by default")
	require.Empty(t, updated)
	require.Equal(t, 0, l.injectCalls, "rejected batch must not inject")

	// enabled + allowlist miss → 400
	h.SetEndpointPolicy(true, []string{"proxy.allowed.example"})
	rec = wp4Post(t, h, body)
	require.Equal(t, http.StatusBadRequest, rec.Code, "host not allowlisted")
	require.Empty(t, updated)

	// allowlisted → update then accept
	ok := `{"messages":[{"role":"user","content":"hi"}],"llm_base_url":"http://proxy.allowed.example:9/v1"}`
	rec = wp4Post(t, h, ok)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Equal(t, "http://proxy.allowed.example:9/v1", updated)
	require.Equal(t, 1, l.injectCalls)

	// rebuild failure → 502, batch rejected (fail-closed)
	h.SetModelUpdateFnE(func(u string) error { return errors.New("provider 500") })
	rec = wp4Post(t, h, ok)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, 1, l.injectCalls, "failed endpoint update must not inject")
}
