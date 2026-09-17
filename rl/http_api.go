// Package agent provides an optional HTTP API for RL integration (AReaL bridge).
//
// The HTTP API exposes tagent's persistent event loop to external callers
// (e.g., AReaL's Python adapter). It is optional — only needed when tagent
// is used as an RL rollout agent.
package rl

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// modelUpdateFnE (5.3): error-returning variant — a failed endpoint rebuild
	// must reject the WHOLE /task request (fail-closed), not fire-and-forget.
	modelUpdateFnE func(baseURL string) error
	// endpointPolicy (5.3): dynamic llm_base_url redirect is DISABLED by
	// default; when enabled, hosts must be allowlisted (exact host, any port).
	endpointEnabled   bool
	endpointAllowlist map[string]bool
	// authToken (implementation-hardening 3.1): when non-empty, every request
	// MUST carry `Authorization: Bearer <token>` — enforced at the top of
	// ServeHTTP, before routing, so no endpoint (including /healthz) executes
	// any side effect unauthenticated. Set via SetAuthToken / AuthTokenFromEnv.
	authToken     string
	feedbackStore memory.MemoryStore
	diagnosticsFn func() any

	// fbMu/fbPending/fbNotify（3.3 backlog-final-closeout）：long-poll 反馈通道——
	// POST /feedback 成功后入队+通知；GET /feedback/wait 阻塞至超时或新事件。
	// 内存态重启清空=接受丢失（C7）；wait 是增量通知，全量靠事件库。
	fbMu      sync.Mutex
	fbPending []map[string]any
	fbNotify  chan struct{}
	fbDropped atomic.Int64 // 5.4: oldest-first overflow counter, surfaced in /feedback/wait

	limits HTTPAPILimits // 5.1: single-point validation bounds
}

// HTTPAPILimits (5.1): single-point request validation bounds. Zero fields
// use the documented defaults; negative values are rejected by SetLimits.
type HTTPAPILimits struct {
	MaxBodyBytes     int64 // request body cap (default 1 MiB)
	MaxMessages      int   // /task messages array cap (default 32)
	MaxContentBytes  int   // per-message content cap (default 256 KiB)
	MaxFeedbackQueue int   // feedback long-poll queue cap (default 1024)
}

// DefaultHTTPAPILimits returns the documented defaults.
func DefaultHTTPAPILimits() HTTPAPILimits {
	return HTTPAPILimits{
		MaxBodyBytes:     1 << 20,
		MaxMessages:      32,
		MaxContentBytes:  256 << 10,
		MaxFeedbackQueue: 1024,
	}
}

// NewHTTPAPI creates a new HTTPAPI for the given agent.
func NewHTTPAPI(agent AgentLoop) *HTTPAPI {
	// F1（哲学审查）：fbNotify 必须 make——nil channel 接收恒阻塞，long-poll
	// 唤醒通路会是死代码（等待者只能等满 30s）。
	return &HTTPAPI{agent: agent, fbNotify: make(chan struct{}, 1), limits: DefaultHTTPAPILimits()}
}

// SetLimits installs request bounds (5.1): zero fields keep the defaults,
// negative fields are rejected — a limit of "reject everything" is a
// misconfiguration, not a feature.
func (h *HTTPAPI) SetLimits(l HTTPAPILimits) error {
	def := DefaultHTTPAPILimits()
	if l.MaxBodyBytes < 0 || l.MaxMessages < 0 || l.MaxContentBytes < 0 || l.MaxFeedbackQueue < 0 {
		return errors.New("httpapi: limits must not be negative")
	}
	if l.MaxBodyBytes == 0 {
		l.MaxBodyBytes = def.MaxBodyBytes
	}
	if l.MaxMessages == 0 {
		l.MaxMessages = def.MaxMessages
	}
	if l.MaxContentBytes == 0 {
		l.MaxContentBytes = def.MaxContentBytes
	}
	if l.MaxFeedbackQueue == 0 {
		l.MaxFeedbackQueue = def.MaxFeedbackQueue
	}
	h.limits = l
	return nil
}

// NewHTTPServer (5.5): server construction with explicit timeouts — the
// :80 regression taught that zero-value http.Server silently binds :80 and
// runs without deadlines; hosts must get a hardened constructor.
func NewHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// SetModelUpdateFn sets the callback for runtime LLM endpoint updates.
// When POST /task includes "llm_base_url", the callback is invoked
// with that URL, allowing the application to redirect LLM requests
// to AReaL's proxy (which captures logprobs for RL training).
func (h *HTTPAPI) SetModelUpdateFn(fn ModelUpdateFn) {
	h.modelUpdateFn = fn
}

// SetModelUpdateFnE installs the error-returning endpoint callback (5.3):
// the update and the message acceptance share the endpoint mutex, and a
// rebuild failure rejects the request with 502 — the old URL keeps serving.
func (h *HTTPAPI) SetModelUpdateFnE(fn func(baseURL string) error) {
	h.modelUpdateFnE = fn
}

// SetEndpointPolicy (5.3): dynamic endpoint redirect defaults to disabled;
// enabling requires a host allowlist (exact host match, any port).
func (h *HTTPAPI) SetEndpointPolicy(enabled bool, allowedHosts []string) {
	h.endpointEnabled = enabled
	h.endpointAllowlist = make(map[string]bool, len(allowedHosts))
	for _, hst := range allowedHosts {
		h.endpointAllowlist[strings.ToLower(hst)] = true
	}
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

// SetAuthToken enables bearer-token authentication for every endpoint
// (implementation-hardening 3.1). With a token set, requests without a
// matching `Authorization: Bearer` header get 401 before any routing or side
// effect. Pair with ValidateListenAddr for the loopback fail-closed guard.
func (h *HTTPAPI) SetAuthToken(token string) { h.authToken = token }

// AuthTokenFromEnv reads TAGENT_RL_AUTH_TOKEN — the host-side convenience for
// wiring SetAuthToken (the rl package has no config section of its own; the
// listen address and token provisioning belong to the host app).
func AuthTokenFromEnv() string { return os.Getenv("TAGENT_RL_AUTH_TOKEN") }

// ValidateListenAddr is the loopback fail-closed guard (implementation-hardening
// 3.2): WITHOUT a token, the API must only listen on loopback — any reachable
// caller could otherwise InjectMessage (steer the agent) or redirect the LLM
// endpoint via llm_base_url (full prompt exfiltration). Hosts MUST call this
// before ListenAndServe; a non-loopback address without a token returns an
// error listing the three ways out. With a token set, any address is allowed.
func ValidateListenAddr(addr, token string) error {
	if token != "" {
		return nil
	}
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	if host == "" {
		host = "0.0.0.0" // ":8089" binds all interfaces
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("RL HTTP API refuses to listen on %q without authentication: it can inject messages into the agent and redirect the LLM endpoint (prompt exfiltration). Fix one of three ways: (1) set TAGENT_RL_AUTH_TOKEN and call SetAuthToken; (2) listen on loopback (127.0.0.1); (3) if you fully accept the risk, bind via your own http.ListenAndServe bypassing this guard", addr)
}

// authorized reports whether the request carries the configured bearer token
// (constant-time compare; scheme matched case-insensitively per RFC 9110).
// An empty configured token disables auth entirely at this layer — the
// security loop for that case is ValidateListenAddr's loopback guard on the
// host side (the single enforcement point for the no-token deployment shape).
func (h *HTTPAPI) authorized(r *http.Request) bool {
	if h.authToken == "" {
		return false
	}
	const prefix = "Bearer "
	got := r.Header.Get("Authorization")
	if len(got) <= len(prefix) || !strings.EqualFold(got[:len(prefix)], prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got[len(prefix):]), []byte(h.authToken)) == 1
}

// ServeHTTP routes requests to the appropriate handler.
func (h *HTTPAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Single auth enforcement point (implementation-hardening 3.1): before
	// routing, so no endpoint — /task, /feedback, /diagnostics, /healthz —
	// executes any side effect unauthenticated (fail-closed consistency; no
	// exemptions by design).
	if h.authToken != "" && !h.authorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid Authorization: Bearer header")
		return
	}

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
		case <-r.Context().Done(): // 5.5: client cancel must not hold the handler
			return
		}
		h.fbMu.Lock()
		pending := h.fbPending
		h.fbPending = nil
		h.fbMu.Unlock()
		dropped := h.fbDropped.Swap(0)
		if pending == nil {
			pending = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items":         pending,
			"dropped_count": dropped, // 5.4: evicted since last wait
			// cold-eyes R2 Minor 8: ANY eviction means this response is
			// incomplete — the trainer must do a full re-query, whether or
			// not newer items were also delivered.
			"partial": dropped > 0,
		})
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
	// cold-eyes Major 4：/feedback 与 /task 同受 limits 单点约束——大 body
	// 直接 413，不允许持有 token 的客户端以单个请求撑爆进程内存。
	body, err := io.ReadAll(io.LimitReader(r.Body, h.limits.MaxBodyBytes+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read_body_error", err.Error())
		return
	}
	if int64(len(body)) > h.limits.MaxBodyBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "body_too_large",
			"request body exceeds limit")
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
			// F2（哲学审查）：事件已落库即入队通知——与「POST 成功后入队」语义一致。
			h.fbEnqueue(map[string]any{
				"feedback_key": tagentevent.FormatEventKey(fbKey),
				"parent":       req.EventKey, "verdict": req.Verdict, "source": "api", "warning": err.Error(),
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
	// 5.4: oldest-first overflow — a slow trainer must not make the bot lose
	// newer verdicts silently; eviction is counted and surfaced via
	// dropped_count in /feedback/wait.
	for len(h.fbPending) > h.limits.MaxFeedbackQueue {
		h.fbPending = h.fbPending[1:]
		h.fbDropped.Add(1)
	}
	h.fbMu.Unlock()
	select {
	case h.fbNotify <- struct{}{}:
	default:
	}
}

// taskRequest is the body for POST /task.
// taskResponse (5.2): 202 carries the batch's stable identity and durability
// so the caller can correlate feedback and detect volatile fallback.
type taskResponse struct {
	Status    string `json:"status"`
	RequestID string `json:"request_id,omitempty"`
	Durable   bool   `json:"durable,omitempty"`
}

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

// handlePostTask submits a task to tagent's persistent event loop via InjectMessage.
// Returns 202 Accepted immediately — the adapter waits separately for processing
// to complete (AReaL proxy captures all LLM interactions during the wait).
//
// If llm_base_url is provided, the model is swapped to use that URL before
// injecting messages. This redirects LLM requests to AReaL's proxy, which
// captures logprobs + completion_ids for RL training.
// endpointMu (5.3): endpoint update and message acceptance are serialized so
// a batch never straddles two endpoint generations.
var endpointMu sync.Mutex

// validateEndpointURL (5.3): scheme must be http/https; userinfo and fragment
// are rejected; the host must be on the allowlist (exact host, any port).
func (h *HTTPAPI) validateEndpointURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("unparseable URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (http/https only)", u.Scheme)
	}
	if u.User != nil || u.Fragment != "" {
		return errors.New("userinfo/fragment not allowed in endpoint URL")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return errors.New("missing host")
	}
	if !h.endpointEnabled {
		return errors.New("dynamic llm_base_url redirect is disabled on this deployment")
	}
	if !h.endpointAllowlist[host] {
		return fmt.Errorf("host %q not in endpoint allowlist", host)
	}
	return nil
}

// applyEndpointUpdate (5.3): prefer the error-returning callback; the legacy
// void callback is wrapped as always-success for compatibility.
func (h *HTTPAPI) applyEndpointUpdate(baseURL string) error {
	if h.modelUpdateFnE != nil {
		return h.modelUpdateFnE(baseURL)
	}
	if h.modelUpdateFn != nil {
		h.modelUpdateFn(baseURL)
	}
	return nil
}

// validateTaskRequest (5.1): the SINGLE validation point for POST /task —
// body cap (via ContentLength pre-check + capped read), message count,
// per-message content size, and role normalization.
func (h *HTTPAPI) validateTaskRequest(r *http.Request) (*taskRequest, int, string) {
	if r.ContentLength > h.limits.MaxBodyBytes {
		return nil, http.StatusRequestEntityTooLarge, "body exceeds limit"
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, h.limits.MaxBodyBytes+1))
	if err != nil {
		return nil, http.StatusBadRequest, err.Error()
	}
	if int64(len(body)) > h.limits.MaxBodyBytes {
		return nil, http.StatusRequestEntityTooLarge, "body exceeds limit"
	}
	var req taskRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, http.StatusBadRequest, err.Error()
	}
	if len(req.Messages) == 0 {
		return nil, http.StatusBadRequest, "messages array is empty"
	}
	if len(req.Messages) > h.limits.MaxMessages {
		return nil, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("messages count %d exceeds limit %d", len(req.Messages), h.limits.MaxMessages)
	}
	for i, m := range req.Messages {
		switch model.Role(m.Role) {
		case "", model.RoleUser, model.RoleSystem:
		default:
			return nil, http.StatusBadRequest,
				fmt.Sprintf("message %d: role %q not allowed (user/system)", i, m.Role)
		}
		if int64(len(m.Content)) > int64(h.limits.MaxContentBytes) {
			return nil, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("message %d content exceeds %d bytes", i, h.limits.MaxContentBytes)
		}
	}
	return &req, 0, ""
}

func (h *HTTPAPI) handlePostTask(w http.ResponseWriter, r *http.Request) {
	if !h.agent.IsLoopActive() {
		writeJSONError(w, http.StatusServiceUnavailable, "loop_not_active",
			"persistent event loop is not running; call StartLoop first")
		return
	}

	req, code, msg := h.validateTaskRequest(r)
	if req == nil {
		writeJSONError(w, code, "request_rejected", msg)
		return
	}

	// 5.3: endpoint update and acceptance share the mutex — a rejected
	// endpoint rebuild rejects the whole batch (fail-closed), and an accepted
	// batch can never straddle two endpoint generations.
	endpointMu.Lock()
	defer endpointMu.Unlock()
	if req.LLMBaseURL != "" {
		if h.modelUpdateFn == nil && h.modelUpdateFnE == nil {
			writeJSONError(w, http.StatusBadRequest, "endpoint_redirect_unconfigured",
				"llm_base_url provided but no endpoint update handler is installed")
			return
		}
		if err := h.validateEndpointURL(req.LLMBaseURL); err != nil {
			writeJSONError(w, http.StatusBadRequest, "endpoint_rejected", err.Error())
			return
		}
		if err := h.applyEndpointUpdate(req.LLMBaseURL); err != nil {
			log.Errorf("[HTTPAPI] endpoint update FAILED — batch rejected, previous endpoint keeps serving: %v", err)
			writeJSONError(w, http.StatusBadGateway, "endpoint_update_failed",
				"endpoint rebuild failed; previous endpoint keeps serving: "+err.Error())
			return
		}
		// cold-eyes Minor 4: URL may embed credentials — log host:port only.
		if u, perr := url.Parse(req.LLMBaseURL); perr == nil {
			log.Infof("[HTTPAPI] LLM base URL updated to %s://%s", u.Scheme, u.Host)
		}
	}

	// 5.2: the WHOLE batch is one acceptance unit — single envelope receipt.
	injector, ok := h.agent.(interface {
		InjectEnvelope(ctx context.Context, source string, msgs []model.Message) (string, bool, error)
	})
	if !ok {
		// Legacy agents without envelope support: per-message fallback.
		for _, m := range req.Messages {
			role := model.Role(m.Role)
			if role == "" {
				role = model.RoleUser
			}
			h.agent.InjectMessage(model.Message{Role: role, Content: m.Content})
		}
		writeJSON(w, http.StatusAccepted, taskResponse{Status: "accepted"})
		return
	}
	msgs := make([]model.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := model.Role(m.Role)
		if role == "" {
			role = model.RoleUser
		}
		msgs = append(msgs, model.Message{Role: role, Content: m.Content})
	}
	requestID, durable, err := injector.InjectEnvelope(r.Context(), "http", msgs)
	if err != nil {
		log.Errorf("[HTTPAPI] batch rejected: %v", err)
		writeJSONError(w, http.StatusServiceUnavailable, "enqueue_rejected", err.Error())
		return
	}
	status := "accepted"
	if !durable {
		status = "accepted_volatile"
	}
	writeJSON(w, http.StatusAccepted, taskResponse{
		Status:    status,
		RequestID: requestID,
		Durable:   durable,
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
