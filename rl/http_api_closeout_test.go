package rl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
