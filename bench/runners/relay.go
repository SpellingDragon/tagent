// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package runners

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// relayTemperature and relayMaxTokens are the sampling contract every runner
// reaches the model under: the baseline sets them on its own request, the
// subprocess runners get them injected at this relay, so the three parties
// sample the same model under the same knobs.
const (
	relayTemperature = 0
	relayMaxTokens   = 8192
	relayFlushBytes  = 32 * 1024
)

// relaySample is the timing and usage of one model request the relay forwarded.
type relaySample struct {
	firstTokenMS     float64
	completeMS       float64
	promptTokens     int
	completionTokens int
}

// modelRelay is a loopback OpenAI-compatible relay: it forwards each request to
// upstream, normalizes the sampling knobs, and records per-request first-token
// and completion timing beside the usage the provider reports.
type modelRelay struct {
	upstream *url.URL
	server   *http.Server
	listener net.Listener
	client   *http.Client

	mu       sync.Mutex
	samples  []relaySample
	prompt   int
	complete int
	failed   string
}

// NewModelRelay starts a loopback relay in front of upstream.
func NewModelRelay(upstream string) (*modelRelay, error) {
	parsed, err := url.Parse(strings.TrimSuffix(upstream, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("bench relay: upstream %q is not an absolute URL", upstream)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bench relay: listen: %w", err)
	}
	relay := &modelRelay{
		upstream: parsed,
		listener: listener,
		client:   &http.Client{Transport: http.DefaultTransport},
	}
	relay.server = &http.Server{Handler: http.HandlerFunc(relay.handle), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = relay.server.Serve(listener) }()
	return relay, nil
}

// URL is the base URL a model client is pointed at.
func (r *modelRelay) URL() string { return "http://" + r.listener.Addr().String() }

// Close stops the relay.
func (r *modelRelay) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.server.Shutdown(ctx)
}

// Reset drops the samples of the previous task.
func (r *modelRelay) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples = nil
	r.prompt = 0
	r.complete = 0
	r.failed = ""
}

// StatsJSON reports the request count, token sum and per-request timings.
func (r *modelRelay) StatsJSON() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	samples := make([]map[string]float64, 0, len(r.samples))
	for _, sample := range r.samples {
		samples = append(samples, map[string]float64{
			"first_token_ms": sample.firstTokenMS,
			"complete_ms":    sample.completeMS,
		})
	}
	return json.Marshal(map[string]any{
		"api_calls":         len(r.samples),
		"prompt_tokens":     r.prompt,
		"completion_tokens": r.complete,
		"requests":          samples,
		"error":             r.failed,
	})
}

func (r *modelRelay) handle(w http.ResponseWriter, req *http.Request) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "bench relay: read request: "+err.Error(), http.StatusBadGateway)
		return
	}
	raw = normalizeModelRequest(raw)
	target := *r.upstream
	target.Path = strings.TrimSuffix(r.upstream.Path, "/") + req.URL.Path
	target.RawQuery = req.URL.RawQuery
	upstreamReq, err := http.NewRequestWithContext(req.Context(), req.Method, target.String(), bytes.NewReader(raw))
	if err != nil {
		http.Error(w, "bench relay: build request: "+err.Error(), http.StatusBadGateway)
		return
	}
	copyRelayHeaders(upstreamReq.Header, req.Header)
	upstreamReq.Header.Set("Content-Length", fmt.Sprint(len(raw)))
	start := time.Now()
	resp, err := r.client.Do(upstreamReq)
	if err != nil {
		r.finish(start, time.Time{}, relayUsage{}, err)
		http.Error(w, "bench relay: upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for key, values := range resp.Header {
		if strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Transfer-Encoding") {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	streaming := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	var pending []byte
	var whole []byte
	var usage relayUsage
	first := time.Time{}
	for {
		buf := make([]byte, relayFlushBytes)
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				r.finish(start, first, usage, writeErr)
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			if streaming {
				pending = consumeSSE(pending, buf[:n], &usage, &first)
			} else {
				whole = append(whole, buf[:n]...)
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				r.finish(start, first, usage, readErr)
				return
			}
			break
		}
	}
	if !streaming {
		consumeCompletionJSON(whole, &usage, &first)
	}
	r.finish(start, first, usage, nil)
}

// finish records one forwarded request; a failed request is still an api call.
func (r *modelRelay) finish(start, first time.Time, usage relayUsage, err error) {
	now := time.Now()
	sample := relaySample{
		promptTokens:     usage.promptTokens,
		completionTokens: usage.completionTokens,
		completeMS:       milliseconds(now.Sub(start)),
	}
	if !first.IsZero() {
		sample.firstTokenMS = milliseconds(first.Sub(start))
	} else {
		sample.firstTokenMS = sample.completeMS
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples = append(r.samples, sample)
	r.prompt += usage.promptTokens
	r.complete += usage.completionTokens
	if err != nil && r.failed == "" {
		r.failed = err.Error()
	}
}

// relayUsage is the usage block one response reports.
type relayUsage struct {
	promptTokens     int
	completionTokens int
}

// relayChunk is the OpenAI response surface the relay reads.
type relayChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// consumeSSE parses complete SSE lines from pending+chunk and returns the
// unconsumed tail, recording the first content token and the latest usage.
func consumeSSE(pending, chunk []byte, usage *relayUsage, first *time.Time) []byte {
	pending = append(pending, chunk...)
	for {
		index := bytes.IndexByte(pending, '\n')
		if index < 0 {
			return pending
		}
		line := bytes.TrimRight(pending[:index], "\r")
		pending = pending[index+1:]
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		var parsed relayChunk
		if err := json.Unmarshal(payload, &parsed); err != nil {
			continue
		}
		absorbChunk(&parsed, usage, first)
	}
}

// consumeCompletionJSON reads one non-streaming completion body.
func consumeCompletionJSON(body []byte, usage *relayUsage, first *time.Time) {
	var parsed relayChunk
	if err := json.Unmarshal(body, &parsed); err != nil {
		return
	}
	absorbChunk(&parsed, usage, first)
}

// absorbChunk records usage and the first emitted token of one response chunk.
func absorbChunk(parsed *relayChunk, usage *relayUsage, first *time.Time) {
	if parsed.Usage != nil {
		usage.promptTokens = parsed.Usage.PromptTokens
		usage.completionTokens = parsed.Usage.CompletionTokens
	}
	for _, choice := range parsed.Choices {
		if choice.Delta.Content != "" || choice.Delta.ReasoningContent != "" ||
			choice.Message.Content != "" || choice.Message.ReasoningContent != "" {
			if first.IsZero() {
				*first = time.Now()
			}
			break
		}
	}
}

// normalizeModelRequest rewrites the sampling knobs of one chat request.
func normalizeModelRequest(raw []byte) []byte {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return raw
	}
	if _, ok := body["messages"]; !ok {
		return raw
	}
	body["temperature"] = json.RawMessage(fmt.Sprint(relayTemperature))
	body["max_tokens"] = json.RawMessage(fmt.Sprint(relayMaxTokens))
	normalized, err := json.Marshal(body)
	if err != nil {
		return raw
	}
	return normalized
}

// copyRelayHeaders forwards request headers except hop-by-hop framing.
func copyRelayHeaders(dst, src http.Header) {
	for key, values := range src {
		switch strings.ToLower(key) {
		case "host", "content-length", "connection", "transfer-encoding":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
