// Package agent provides an optional HTTP API for RL integration (AReaL bridge).
//
// The HTTP API exposes tagent's persistent event loop to external callers
// (e.g., AReaL's Python adapter). It is optional — only needed when tagent
// is used as an RL rollout agent.
package rl

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

// ServeHTTP routes requests to the appropriate handler.
func (h *HTTPAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/task":
		h.handlePostTask(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		h.handleHealthz(w, r)
	default:
		writeJSONError(w, http.StatusNotFound, "not_found", fmt.Sprintf("no route for %s %s", r.Method, r.URL.Path))
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
