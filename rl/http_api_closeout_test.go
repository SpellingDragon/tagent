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
	"github.com/stretchr/testify/require"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

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
		return map[string]any{"wal_quarantined": int64(0), "engine_ready": false}
	})
	rec2 := httptest.NewRecorder()
	h2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/diagnostics", nil))
	require.Equal(t, http.StatusOK, rec2.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &body))
	require.Contains(t, body, "wal_quarantined")
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

func (l *envelopeInjectingLoop) InjectEnvelope(_ context.Context, _ string, msgs []model.Message) (string, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.injectCalls++
	if l.injectErr != nil {
		return "", false, l.injectErr
	}
	l.envelopes = append(l.envelopes, msgs)
	return "req-wp4-1", true, nil
}

func postEnvelopeBody(t *testing.T, h *HTTPAPI, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
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
