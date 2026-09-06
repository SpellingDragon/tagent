package rl

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TestPostFeedback (2.4, design-report-closeout): POST /feedback binds an
// external verdict to a produced event; parent-miss is an explicit 404;
// unwired store is 503. Does not require a live agent loop.
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

	// Unwired → 503.
	fresh := NewHTTPAPI(nil)
	w := httptest.NewRecorder()
	fresh.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/feedback", bytes.NewBufferString(`{}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired = %d, want 503", w.Code)
	}

	// Happy path → 201 + feedback stored with causal binding.
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

	// Ghost parent → 404 explicit.
	if w := post(`{"event_key":"7fffffffffff","verdict":"negative"}`); w.Code != http.StatusNotFound {
		t.Fatalf("ghost = %d, want 404", w.Code)
	}
	// Missing fields → 400.
	if w := post(`{"verdict":"positive"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing fields = %d, want 400", w.Code)
	}
}
