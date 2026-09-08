package rl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"

	"github.com/stretchr/testify/require"
)

// TestDiagnostics_WiredAndNotWired（B1/哲学审查——兑现 tasks 1.2 回归）：
// 注入 fn → 200+JSON；未注入 → 404 显式（端点存在但未接线）。
func TestDiagnostics_WiredAndNotWired(t *testing.T) {
	// 未装配：404 diagnostics_disabled。
	h := NewHTTPAPI(nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/diagnostics", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)

	// 装配：200 + JSON（含 wal_quarantined 键——F3 计数可达）。
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

// TestFeedbackWait_ImmediateWake（F1/哲学审查——兑现 tasks 3.3 回归）：
// POST 成功入队后，wait 立即返回（不等满 30s）——fbNotify nil-channel 死代码的回归。
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

// TestFeedbackBridge_PostWakesWait（2026-09-08 桥接回归）：POST /feedback 真链路
// （store 落库+因果边+入队通知）→ GET /feedback/wait 被唤醒并返回该条——RL 协议
// 契约(rl/README)的端到端守护；区别于 TestFeedbackWait_ImmediateWake 的直注入。
func TestFeedbackBridge_PostWakesWait(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("rl-bridge")
	// 准备 parent 产出事件（feedback 绑定目标）。
	parentKey := memory.NewSnowflakeEventKey(pid, 0)
	if err := store.StoreEvent(parentKey, memory.FullEvent{
		EventKey: parentKey, PartitionID: pid,
		EventType: tagentevent.TypeExternalInput, Timestamp: 1700000000000,
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}

	h := NewHTTPAPI(nil)
	h.SetFeedbackStore(store)

	// 等待者先挂起（long-poll）。
	waitDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/feedback/wait?timeout=5", nil))
		waitDone <- rec
	}()
	time.Sleep(50 * time.Millisecond) // 等待 goroutine 进入 select

	// POST 真链路：落库+入队+通知。
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
