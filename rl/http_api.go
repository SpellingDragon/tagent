// Package agent provides an optional HTTP API for RL integration (AReaL bridge).
//
// The HTTP API exposes tagent's persistent event loop to external callers
// (e.g., AReaL's Python adapter). It is optional — only needed when tagent
// is used as an RL rollout agent.
package rl

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ModelUpdateFn is called when POST /task includes llm_base_url.
// The application layer (main.go) sets this callback to create a new
// model with the given base URL and swap it into the active SwappableModel.
// This allows AReaL's dynamically-allocated proxy URL to be used
// without changing the event mechanism.
type ModelUpdateFn func(baseURL string)

// HTTPAPI exposes tagent's persistent loop via HTTP.
// It enables external callers (e.g., AReaL Python adapter) to submit tasks.
type HTTPAPI struct {
	agent         AgentLoop
	modelUpdateFn ModelUpdateFn // optional: set by main.go for AReaL proxy support
	// feedbackStore (D1 design-report-closeout 2.4): optional MemoryStore for
	// POST /feedback — binds an external verdict to a produced event via the
	// feedback causal edge. nil → 503 (endpoint disabled).
	feedbackStore memory.MemoryStore
	diagnosticsFn func() any

	// fbMu/fbPending/fbNotify（3.3 backlog-final-closeout）：long-poll 反馈通道——
	// POST /feedback 成功后入队+通知；GET /feedback/wait 阻塞至超时或新事件。
	// 内存态重启清空=接受丢失（C7）；wait 是增量通知，全量靠事件库。
	fbMu      sync.Mutex
	fbPending []map[string]any
	fbNotify  chan struct{}
}

// NewHTTPAPI creates a new HTTPAPI for the given agent.
func NewHTTPAPI(agent AgentLoop) *HTTPAPI {
	return &HTTPAPI{agent: agent}
}

// SetModelUpdateFn sets the callback for runtime LLM endpoint updates.
// When POST /task includes "llm_base_url", the callback is invoked
// with that URL, allowing the application to redirect LLM requests
// to AReaL's proxy (which captures logprobs for RL training).
func (h *HTTPAPI) SetModelUpdateFn(fn ModelUpdateFn) {
	h.modelUpdateFn = fn
}

// SetDiagnosticsFn 注入诊断快照构造器（R2 backlog-final-closeout：诊断快照获得消费
// 面——GET /diagnostics 输出 JSON）。fn 为 nil 时不注册端点（404）。
func (h *HTTPAPI) SetDiagnosticsFn(fn func() any) {
	h.diagnosticsFn = fn
}

// SetFeedbackStore enables POST /feedback (D1 design-report-closeout): the
// store receives feedback events bound to produced events by hex event_key.
func (h *HTTPAPI) SetFeedbackStore(store memory.MemoryStore) {
	h.feedbackStore = store
}

// ServeHTTP routes requests to the appropriate handler.
func (h *HTTPAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/task":
		h.handlePostTask(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/diagnostics":
		if h.diagnosticsFn == nil {
			writeJSONError(w, http.StatusNotFound, "diagnostics_disabled", "diagnostics not wired")
			return
		}
		writeJSON(w, http.StatusOK, h.diagnosticsFn())
	case r.Method == http.MethodGet && r.URL.Path == "/feedback/wait":
		// 3.3（backlog-final-closeout）：long-poll——阻塞至超时或新 feedback。
		// 内存态队列重启清空=接受丢失（C7）；wait 是增量通知，全量靠事件库。
		timeout := 30 * time.Second
		if v := r.URL.Query().Get("timeout"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 && secs <= 30 {
				timeout = time.Duration(secs) * time.Second
			}
		}
		select {
		case <-h.fbNotify:
		case <-time.After(timeout):
		}
		h.fbMu.Lock()
		pending := h.fbPending
		h.fbPending = nil
		h.fbMu.Unlock()
		if pending == nil {
			pending = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": pending})
	case r.Method == http.MethodPost && r.URL.Path == "/feedback":
		h.handlePostFeedback(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		h.handleHealthz(w, r)
	default:
		writeJSONError(w, http.StatusNotFound, "not_found", fmt.Sprintf("no route for %s %s", r.Method, r.URL.Path))
	}
}

// feedbackRequest is the body for POST /feedback.
type feedbackRequest struct {
	EventKey string  `json:"event_key"` // hex event key of the produced event to bind
	Verdict  string  `json:"verdict"`   // positive / negative / neutral
	Note     string  `json:"note,omitempty"`
	Rating   float64 `json:"rating,omitempty"`
}

// handlePostFeedback binds an external verdict to a produced event
// (D1 design-report-closeout 2.4). Missing parent → explicit 404 (no
// feedback on hallucinated keys); disabled → 503.
func (h *HTTPAPI) handlePostFeedback(w http.ResponseWriter, r *http.Request) {
	if h.feedbackStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "feedback_disabled", "no feedback store wired")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read_body_error", err.Error())
		return
	}
	var req feedbackRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.EventKey == "" || req.Verdict == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_fields", "event_key and verdict are required")
		return
	}
	key, err := tagentevent.ParseEventKey(req.EventKey)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_event_key", err.Error())
		return
	}
	fbKey, err := memory.BindFeedback(h.feedbackStore, key, memory.FeedbackPayload{
		Verdict: req.Verdict, Rating: req.Rating, Note: req.Note, Source: "api",
	})
	if err != nil {
		// 8.5（review §8）：按 sentinel 分类——parent-miss=确定性 404（勿重试）；
		// edge-partial=事件已落库仅因果边失败 → 201+warning（重试会写重复 feedback）；
		// 其余=500。
		switch {
		case errors.Is(err, memory.ErrFeedbackParentNotFound):
			writeJSONError(w, http.StatusNotFound, "parent_not_found", err.Error())
		case errors.Is(err, memory.ErrFeedbackEdgePartial):
			log.Warnf("[HTTPAPI] feedback stored but edge partial: %v", err)
			writeJSON(w, http.StatusCreated, map[string]any{
				"status": "bound_with_warning", "feedback_key": tagentevent.FormatEventKey(fbKey),
				"warning": err.Error(),
			})
		default:
			writeJSONError(w, http.StatusInternalServerError, "bind_failed", err.Error())
		}
		return
	}
	log.Infof("[HTTPAPI] feedback bound: parent=%s verdict=%s feedback=%s",
		req.EventKey, req.Verdict, tagentevent.FormatEventKey(fbKey))
	writeJSON(w, http.StatusCreated, map[string]any{
		"status":       "bound",
		"feedback_key": tagentevent.FormatEventKey(fbKey),
	})
	// 3.3（backlog-final-closeout）：成功登记 → 通知 long-poll 等待者并入队。
	h.fbEnqueue(map[string]any{
		"feedback_key": tagentevent.FormatEventKey(fbKey),
		"parent":       req.EventKey, "verdict": req.Verdict, "source": "api",
	})
}

// fbEnqueue（3.3）：feedback 成功入队并通知 long-poll 等待者（非阻塞，容量1）。
func (h *HTTPAPI) fbEnqueue(item map[string]any) {
	h.fbMu.Lock()
	h.fbPending = append(h.fbPending, item)
	h.fbMu.Unlock()
	select {
	case h.fbNotify <- struct{}{}:
	default:
	}
}

// taskRequest is the body for POST /task.
type taskRequest struct {
	Messages   []taskMessage `json:"messages"`
	UserID     string        `json:"user_id"`
	SessionID  string        `json:"session_id"`
	LLMBaseURL string        `json:"llm_base_url,omitempty"` // AReaL proxy URL (dynamic port)
}

type taskMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// taskResponse is the response for POST /task.
type taskResponse struct {
	Status string `json:"status"`
}

// handlePostTask submits a task to tagent's persistent event loop via InjectMessage.
// Returns 202 Accepted immediately — the adapter waits separately for processing
// to complete (AReaL proxy captures all LLM interactions during the wait).
//
// If llm_base_url is provided, the model is swapped to use that URL before
// injecting messages. This redirects LLM requests to AReaL's proxy, which
// captures logprobs + completion_ids for RL training.
func (h *HTTPAPI) handlePostTask(w http.ResponseWriter, r *http.Request) {
	if !h.agent.IsLoopActive() {
		writeJSONError(w, http.StatusServiceUnavailable, "loop_not_active",
			"persistent event loop is not running; call StartLoop first")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read_body_error", err.Error())
		return
	}

	var req taskRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if len(req.Messages) == 0 {
		writeJSONError(w, http.StatusBadRequest, "no_messages", "messages array is empty")
		return
	}

	// Update LLM endpoint if requested (AReaL proxy URL — dynamically allocated)
	if req.LLMBaseURL != "" && h.modelUpdateFn != nil {
		h.modelUpdateFn(req.LLMBaseURL)
		log.Infof("[HTTPAPI] LLM base URL updated to %s", req.LLMBaseURL)
	}

	// Submit each message to the mailbox via InjectMessage
	for _, msg := range req.Messages {
		role := model.Role(msg.Role)
		if role == "" {
			role = model.RoleUser
		}
		h.agent.InjectMessage(model.Message{
			Role:    role,
			Content: msg.Content,
		})
	}

	writeJSON(w, http.StatusAccepted, taskResponse{
		Status: "accepted",
	})
}

// handleHealthz returns health status.
func (h *HTTPAPI) handleHealthz(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"status":      "ok",
		"loop_active": h.agent.IsLoopActive(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, errorType, message string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   errorType,
		"message": message,
	})
}
