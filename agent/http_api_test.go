package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

type mockHTTPRunner struct {
	mu       sync.Mutex
	calls    int
	messages []model.Message
}

func (m *mockHTTPRunner) Run(ctx context.Context, userID string, sessionID string, message model.Message, opts ...agent.RunOption) (<-chan *event.Event, error) {
	m.mu.Lock()
	m.calls++
	m.messages = append(m.messages, message)
	m.mu.Unlock()

	ch := make(chan *event.Event, 2)
	rsp := &model.Response{
		ID:    "comp-test",
		Model: "test-model",
		Done:  true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: "task completed successfully",
			},
		}},
		Usage: &model.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
		},
	}
	ch <- event.NewResponseEvent("inv", "author", rsp)
	close(ch)
	return ch, nil
}

func (m *mockHTTPRunner) Close() error { return nil }

func createTestAgent(t *testing.T) *TagentAgent {
	t.Helper()
	bus := NewEventBus()
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := NewDefaultTokenCounter()
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)
	outputCh := make(chan *event.Event, 100)
	agentLoop := NewAgentLoop(AgentLoopConfig{
		Bus:          bus,
		Preprocessor: preproc,
		Model:        &mockModel{},
		OutputCh:     outputCh,
		Name:         "test",
		MaxToolIters: 10,
	})
	return &TagentAgent{
		bus:          bus,
		agentLoop:    agentLoop,
		preprocessor: preproc,
		runner:       &mockHTTPRunner{},
		config:       &TagentConfig{},
		outputCh:     outputCh,
		name:         "test",
		description:  "test",
	}
}

// startTestLoop sets up loop state and starts the AgentLoop goroutine
// for tests that need an active loop. Returns a cleanup function.
func startTestLoop(ta *TagentAgent) func() {
	ta.loopCtx, ta.loopCancel = context.WithCancel(context.Background())
	ta.loopActive.Store(true)
	ta.loopWg.Add(1)
	go func() {
		defer ta.loopWg.Done()
		ta.agentLoop.Run(ta.loopCtx)
	}()
	return func() {
		ta.loopActive.Store(false)
		ta.loopCancel()
		ta.loopWg.Wait()
	}
}

// ---------------------------------------------------------------------------
// HTTP API tests
// ---------------------------------------------------------------------------

func TestHTTPAPI_Healthz_LoopInactive(t *testing.T) {
	ta := createTestAgent(t)
	api := NewHTTPAPI(ta)
	srv := httptest.NewServer(api)
	defer srv.Close()

	resp, body := doRequest(t, srv, http.MethodGet, "/healthz", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	json.Unmarshal(body, &result)
	assert.Equal(t, "ok", result["status"])
	assert.Equal(t, false, result["loop_active"])
}

func TestHTTPAPI_Healthz_LoopActive(t *testing.T) {
	ta := createTestAgent(t)
	cleanup := startTestLoop(ta)
	defer cleanup()

	api := NewHTTPAPI(ta)
	srv := httptest.NewServer(api)
	defer srv.Close()

	resp, body := doRequest(t, srv, http.MethodGet, "/healthz", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	json.Unmarshal(body, &result)
	assert.Equal(t, true, result["loop_active"])
}

func TestHTTPAPI_PostTask_LoopInactive(t *testing.T) {
	ta := createTestAgent(t)
	api := NewHTTPAPI(ta)
	srv := httptest.NewServer(api)
	defer srv.Close()

	resp, _ := doRequest(t, srv, http.MethodPost, "/task", map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestHTTPAPI_PostTask_Success(t *testing.T) {
	ta := createTestAgent(t)
	cleanup := startTestLoop(ta)
	defer cleanup()

	api := NewHTTPAPI(ta)
	srv := httptest.NewServer(api)
	defer srv.Close()

	resp, _ := doRequest(t, srv, http.MethodPost, "/task", map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "do something"}},
	})
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
}

func TestHTTPAPI_PostTask_NoMessages(t *testing.T) {
	ta := createTestAgent(t)
	cleanup := startTestLoop(ta)
	defer cleanup()

	api := NewHTTPAPI(ta)
	srv := httptest.NewServer(api)
	defer srv.Close()

	resp, _ := doRequest(t, srv, http.MethodPost, "/task", map[string]any{
		"messages": []map[string]string{},
	})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHTTPAPI_PostTask_WithLLMBaseURL(t *testing.T) {
	ta := createTestAgent(t)
	cleanup := startTestLoop(ta)
	defer cleanup()

	api := NewHTTPAPI(ta)
	var updatedURL string
	api.SetModelUpdateFn(func(baseURL string) {
		updatedURL = baseURL
	})

	srv := httptest.NewServer(api)
	defer srv.Close()

	resp, _ := doRequest(t, srv, http.MethodPost, "/task", map[string]any{
		"messages":     []map[string]string{{"role": "user", "content": "rl task"}},
		"llm_base_url": "http://localhost:12345/v1",
	})
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Equal(t, "http://localhost:12345/v1", updatedURL)
}

func TestHTTPAPI_PostTask_NoCallback_NoError(t *testing.T) {
	ta := createTestAgent(t)
	cleanup := startTestLoop(ta)
	defer cleanup()

	api := NewHTTPAPI(ta)
	srv := httptest.NewServer(api)
	defer srv.Close()

	resp, _ := doRequest(t, srv, http.MethodPost, "/task", map[string]any{
		"messages":     []map[string]string{{"role": "user", "content": "task"}},
		"llm_base_url": "http://localhost:9999/v1",
	})
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
}

func TestHTTPAPI_PostTask_InvalidJSON(t *testing.T) {
	ta := createTestAgent(t)
	cleanup := startTestLoop(ta)
	defer cleanup()

	api := NewHTTPAPI(ta)
	srv := httptest.NewServer(api)
	defer srv.Close()

	resp, _ := doRequestRaw(t, srv, http.MethodPost, "/task", "not json")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHTTPAPI_NotFound(t *testing.T) {
	ta := createTestAgent(t)
	api := NewHTTPAPI(ta)
	srv := httptest.NewServer(api)
	defer srv.Close()

	resp, _ := doRequest(t, srv, http.MethodGet, "/unknown", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHTTPAPI_TrajectoryEndpoints_Removed(t *testing.T) {
	ta := createTestAgent(t)
	api := NewHTTPAPI(ta)
	srv := httptest.NewServer(api)
	defer srv.Close()

	resp, _ := doRequest(t, srv, http.MethodGet, "/trajectories", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp, _ = doRequest(t, srv, http.MethodGet, "/trajectory/test", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// SwappableModel tests
// ---------------------------------------------------------------------------

type mockModel struct {
	info model.Info
}

func (m *mockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		ID:    "test",
		Model: m.info.Name,
		Done:  true,
		Choices: []model.Choice{{
			Message: model.Message{Role: model.RoleAssistant, Content: "mock response"},
		}},
	}
	close(ch)
	return ch, nil
}

func (m *mockModel) Info() model.Info { return m.info }

func TestSwappableModel_Swap(t *testing.T) {
	original := &mockModel{info: model.Info{Name: "original-model"}}
	swapped := &mockModel{info: model.Info{Name: "swapped-model"}}

	sm := NewSwappableModel(original)
	assert.Equal(t, "original-model", sm.Info().Name)

	sm.Swap(swapped)
	assert.Equal(t, "swapped-model", sm.Info().Name)

	sm.Swap(original)
	assert.Equal(t, "original-model", sm.Info().Name)
}

func TestSwappableModel_GenerateContent(t *testing.T) {
	m := &mockModel{info: model.Info{Name: "test-model"}}
	sm := NewSwappableModel(m)

	ctx := context.Background()
	req := &model.Request{}
	ch, err := sm.GenerateContent(ctx, req)
	require.NoError(t, err)

	resp := <-ch
	assert.Equal(t, "test-model", resp.Model)
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func doRequest(t *testing.T, srv *httptest.Server, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		bodyBytes, _ = json.Marshal(body)
	}
	return doRequestRaw(t, srv, method, path, string(bodyBytes))
}

func doRequestRaw(t *testing.T, srv *httptest.Server, method, path, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if body != "" {
		req.Body = io.NopCloser(strings.NewReader(body))
	}
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp, respBody
}
