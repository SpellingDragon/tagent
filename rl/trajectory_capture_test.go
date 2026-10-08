package rl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// capTool is a declaration-only tool stub; capture reads Declaration() once and
// never invokes it (S1 is a read-only declaration view).
type capTool struct{ decl *tool.Declaration }

func (t capTool) Declaration() *tool.Declaration { return t.decl }

// capStreamModel replays a fixed response sequence and then closes, optionally
// holding the stream open until its ctx is done (cancel/inflight cases).
type capStreamModel struct {
	info      model.Info
	responses []*model.Response
	callErr   error
	// hold keeps the channel open after emitting responses until ctx is done,
	// modelling a stream that is still in flight at flush time.
	hold bool
	// emitDelay paces a long stream.
	emitDelay time.Duration

	mu    sync.Mutex
	calls int
}

func (m *capStreamModel) Info() model.Info {
	if m.info.Name == "" {
		return model.Info{Name: "cap-model"}
	}
	return m.info
}

func (m *capStreamModel) GenerateContent(ctx context.Context, _ *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	if m.callErr != nil {
		return nil, m.callErr
	}
	out := make(chan *model.Response, len(m.responses)+1)
	for _, r := range m.responses {
		out <- r
		if m.emitDelay > 0 {
			time.Sleep(m.emitDelay)
		}
	}
	if m.hold {
		go func() {
			<-ctx.Done()
			close(out)
		}()
		return out, nil
	}
	close(out)
	return out, nil
}

// capIDByRequest answers with a stable response id only for the prompts listed,
// so ONE recorder sees both a bound call and an identity-free call in one file.
type capIDByRequest struct{ withID map[string]bool }

func (capIDByRequest) Info() model.Info { return model.Info{Name: "cap-by-request"} }

func (m capIDByRequest) GenerateContent(_ context.Context, r *model.Request) (<-chan *model.Response, error) {
	prompt := ""
	if len(r.Messages) > 0 {
		prompt = r.Messages[0].Content
	}
	id := ""
	if m.withID[prompt] {
		id = "resp-u"
	}
	out := make(chan *model.Response, 1)
	out <- termResp(id, "pong", "stop")
	close(out)
	return out, nil
}

// capErrorModel returns (nil, nil): the legal-but-anomalous upstream shape.
type capErrorModel struct{ err error }

func (capErrorModel) Info() model.Info { return model.Info{Name: "cap-err"} }
func (m capErrorModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	return nil, m.err
}

// stallSink wraps the disk sink but blocks every write until released, which
// is how a stalled disk looks to the capture queue.
type stallSink struct {
	inner   captureSink
	gate    chan struct{}
	once    sync.Once
	blocked atomic.Int32
}

func (s *stallSink) MkdirAll(dir string) error { return s.inner.MkdirAll(dir) }

func (s *stallSink) Open(path string) (captureFileHandle, error) {
	f, err := s.inner.Open(path)
	if err != nil {
		return nil, err
	}
	return &stalledHandle{captureFileHandle: f, sink: s}, nil
}

func (s *stallSink) block() { s.blocked.Store(1) }

func (s *stallSink) release() {
	s.once.Do(func() {
		s.blocked.Store(0)
		close(s.gate)
	})
}

func (s *stallSink) wait(t *testing.T) {
	t.Helper()
	select {
	case <-s.gate:
	case <-time.After(10 * time.Second):
		t.Fatal("sink gate never released")
	}
}

type stalledHandle struct {
	captureFileHandle
	sink *stallSink
}

func (h *stalledHandle) Write(p []byte) (int, error) {
	if h.sink.blocked.Load() == 1 {
		<-h.sink.gate
	}
	return h.captureFileHandle.Write(p)
}

// failWriteSink fails every write; failSyncSink fails every sync.
type failWriteSink struct{ inner captureSink }

func (s failWriteSink) MkdirAll(dir string) error { return s.inner.MkdirAll(dir) }
func (s failWriteSink) Open(path string) (captureFileHandle, error) {
	f, err := s.inner.Open(path)
	if err != nil {
		return nil, err
	}
	return failWriteHandle{captureFileHandle: f}, nil
}

type failWriteHandle struct{ captureFileHandle }

func (h failWriteHandle) Write([]byte) (int, error) { return 0, errors.New("disk write refused") }

type failSyncSink struct{ inner captureSink }

func (s failSyncSink) MkdirAll(dir string) error { return s.inner.MkdirAll(dir) }
func (s failSyncSink) Open(path string) (captureFileHandle, error) {
	f, err := s.inner.Open(path)
	if err != nil {
		return nil, err
	}
	return failSyncHandle{captureFileHandle: f}, nil
}

type failSyncHandle struct{ captureFileHandle }

func (h failSyncHandle) Sync() error { return errors.New("fsync not supported") }

func captureEnabled() CaptureConfig { return CaptureConfig{Enabled: true} }

func newCaptureRecorder(t *testing.T, inner model.Model, cfg CaptureConfig, opts ...RecorderOption) *TrajectoryRecorder {
	t.Helper()
	all := append([]RecorderOption{WithCapture(cfg)}, opts...)
	tr, err := NewTrajectoryRecorderWithOptions(inner, t.TempDir(), "https://cap.example.com/v1", all...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

func capRequest(text string) *model.Request {
	return &model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: text}}}
}

func drainChan(t *testing.T, ch <-chan *model.Response) []*model.Response {
	t.Helper()
	var got []*model.Response
	for r := range ch {
		got = append(got, r)
	}
	return got
}

func readCaptureLines(t *testing.T, dir, sessionID string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, sessionID+".jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
	}
	return splitJSONL(data)
}

func readCaptureRecords(t *testing.T, dir, sessionID string) []*CaptureRecord {
	t.Helper()
	lines := readCaptureLines(t, dir, sessionID)
	recs := make([]*CaptureRecord, 0, len(lines))
	for i, line := range lines {
		var r CaptureRecord
		require.NoErrorf(t, json.Unmarshal(line, &r), "line %d", i)
		recs = append(recs, &r)
	}
	return recs
}

func termResp(id, content, finish string) *model.Response {
	fr := finish
	return &model.Response{
		ID: id, Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}, FinishReason: &fr}},
	}
}

func partialResp(id, delta string) *model.Response {
	return &model.Response{
		ID: id, IsPartial: true, Object: model.ObjectTypeChatCompletionChunk,
		Choices: []model.Choice{{Delta: model.Message{Role: model.RoleAssistant, Content: delta}}},
	}
}

// TestTrajectoryCapture_RequestSnapshot pins the v2 request-snapshot contract.
//   - frozen request view and per-call identity
//   - byte bounds, loss accounting and the seal manifest
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestTrajectoryCapture_RequestSnapshot(t *testing.T) {
	t.Run("configuration_defaults_and_validation", func(t *testing.T) {
		var zero CaptureConfig
		require.False(t, zero.Enabled, "capture must be off unless explicitly enabled")
		require.NoError(t, zero.Validate(false), "off + no dump is the shipped default")

		require.ErrorIs(t, CaptureConfig{Enabled: true}.Validate(false), ErrCaptureRequiresDump,
			"enabled capture requires trajectory_dump=true")
		require.NoError(t, CaptureConfig{Enabled: true}.Validate(true))

		for name, cfg := range map[string]CaptureConfig{
			"max_record_bytes":  {Enabled: true, MaxRecordBytes: -1},
			"max_pending_bytes": {Enabled: true, MaxPendingBytes: -8},
			"max_run_bytes":     {Enabled: true, MaxRunBytes: -1},
			"max_open_files":    {Enabled: true, MaxOpenFiles: -2},
			"queue_size":        {Enabled: true, QueueSize: -3},
		} {
			require.Errorf(t, cfg.Validate(true), "negative %s must be rejected", name)
			_, nerr := cfg.normalize()
			require.Errorf(t, nerr, "negative %s must be rejected at normalize", name)
		}

		d, err := CaptureConfig{Enabled: true}.normalize()
		require.NoError(t, err)
		assert.EqualValues(t, DefaultCaptureMaxRecordBytes, d.MaxRecordBytes)
		assert.EqualValues(t, DefaultCaptureMaxPendingBytes, d.MaxPendingBytes)
		assert.EqualValues(t, DefaultCaptureMaxRunBytes, d.MaxRunBytes)
		assert.Equal(t, MaxCaptureOpenFiles, d.MaxOpenFiles)
		assert.Equal(t, captureQueueBufferSize, d.QueueSize)

		clamped, err := CaptureConfig{Enabled: true, MaxOpenFiles: 1024}.normalize()
		require.NoError(t, err)
		assert.LessOrEqual(t, clamped.MaxOpenFiles, MaxCaptureOpenFiles,
			"simultaneous open handles are capped at 16, a larger request is clamped not honoured")

		tr := newCaptureRecorder(t, &capStreamModel{}, CaptureConfig{Enabled: false})
		require.False(t, tr.CaptureEnabled())
		ch, err := tr.GenerateContent(context.Background(), capRequest("off"))
		require.NoError(t, err)
		drainChan(t, ch)
		lines := readCaptureLines(t, tr.dir, "unused")
		assert.Nil(t, lines, "no capture file is created while capture is off")
	})

	t.Run("snapshot_is_frozen_before_the_inner_call", func(t *testing.T) {
		cfg := captureEnabled()
		tr := newCaptureRecorder(t, &capStreamModel{responses: []*model.Response{termResp("resp-1", "pong", "stop")}}, cfg)
		tr.SetSessionInfo("user-cap", "sess-cap")

		decl := &tool.Declaration{
			Name:        "search",
			Description: "web search",
			InputSchema: &tool.Schema{Type: "object", Properties: map[string]*tool.Schema{
				"query": {Type: "string", Description: "the query"},
			}, Required: []string{"query"}},
		}
		req := &model.Request{
			Messages: []model.Message{{Role: model.RoleSystem, Content: "sys"}, {Role: model.RoleUser, Content: "go"}},
			Tools:    map[string]tool.Tool{"search-key": capTool{decl}},
		}
		req.GenerationConfig.Stream = true
		maxTok := 128
		req.GenerationConfig.MaxTokens = &maxTok

		ch, err := tr.GenerateContent(context.Background(), req)
		require.NoError(t, err)
		drainChan(t, ch)

		req.Messages[0].Content = "MUTATED-SYS"
		req.Messages[1].ContentParts = []model.ContentPart{{Type: model.ContentTypeText}}
		decl.Description = "MUTATED-DESC"
		decl.InputSchema.Properties["query"].Description = "MUTATED-PARAM"
		delete(req.Tools, "search-key")

		require.NoError(t, tr.Close())
		recs := readCaptureRecords(t, tr.dir, "sess-cap")
		require.Len(t, recs, 1)
		rec := recs[0]

		assert.Equal(t, "sys", rec.LLMCall.Request.Messages[0].Content, "frozen messages must not follow later mutation")
		assert.Equal(t, "go", rec.LLMCall.Request.Messages[1].Content)
		assert.Empty(t, rec.LLMCall.Request.Messages[1].ContentParts)
		require.Len(t, rec.LLMCall.Request.Tools, 1)
		assert.Equal(t, "web search", rec.LLMCall.Request.Tools[0].Description, "frozen declaration must not follow later mutation")
		assert.Equal(t, "the query", rec.LLMCall.Request.Tools[0].InputSchema.Properties["query"].Description)
		assert.True(t, rec.LLMCall.Request.GenerationConfig.Stream)
		require.NotNil(t, rec.LLMCall.Request.GenerationConfig.MaxTokens)
		assert.EqualValues(t, 128, *rec.LLMCall.Request.GenerationConfig.MaxTokens)
	})

	t.Run("v2_field_contract", func(t *testing.T) {
		scope := NewCaptureScope("inv-7")
		scope.Owner = OwnerAttrs{
			"capture_namespace": "p-3",
			"root_session_id":   "root-1",
			"agent_name":        "resident",
			"partition_id":      "3",
			"task_id":           "task-9",
			"attempt_id":        "attempt-2",
			"generation_id":     "gen-5",
			"bundle_id":         "bundle-v1",
			"trigger_source":    "user",
			"purpose":           "policy",
			"input_event_keys":  "1a,1b",
			"parent_invocation": "inv-0",
			"user_id":           "user-owner",
			"session_id":        "sess-owner",
		}
		ctx := WithCaptureScope(context.Background(), scope)

		cfg := captureEnabled()
		tr := newCaptureRecorder(t,
			&capStreamModel{responses: []*model.Response{termResp("resp-42", "pong", "tool_calls")}}, cfg)
		tr.SetSessionInfo("user-cap", "sess-cap")

		req := &model.Request{
			Messages: []model.Message{{Role: model.RoleUser, Content: "go"}},
			Tools: map[string]tool.Tool{
				"zeta": capTool{&tool.Declaration{Name: "z", Description: "z tool"}},
				"alpha": capTool{&tool.Declaration{Name: "a", Description: "a tool",
					InputSchema:  &tool.Schema{Type: "object", Properties: map[string]*tool.Schema{"q": {Type: "string"}}},
					OutputSchema: &tool.Schema{Type: "string"}}},
			},
		}
		ch, err := tr.GenerateContent(ctx, req)
		require.NoError(t, err)
		drainChan(t, ch)
		require.NoError(t, tr.Close())

		lines := readCaptureLines(t, tr.dir, "sess-cap")
		require.Len(t, lines, 1)

		// Key-level assertions against the raw JSON: the O6a consumer reads these
		// exact spellings, so they are pinned here rather than via Go field names.
		var raw map[string]any
		require.NoError(t, json.Unmarshal(lines[0], &raw))
		for _, key := range []string{
			"timestamp", "session_id", "user_id", "batch_index", "llm_call", "metadata",
			"schema_version", "run_id", "call_id", "capture_scope", "owner",
			"binding_status", "missing_reasons", "request_digest",
		} {
			assert.Contains(t, raw, key, "v2 record must carry %q", key)
		}
		assert.EqualValues(t, 2, raw["schema_version"])
		assert.Equal(t, CaptureScopeSDKRequest, raw["capture_scope"])
		assert.Equal(t, "bound", raw["binding_status"], "invocation + response id evidence binds the call")

		owner, ok := raw["owner"].(map[string]any)
		require.True(t, ok)
		for _, key := range []string{
			"capture_namespace", "root_session_id", "agent_name", "partition_id", "session_id",
			"user_id", "invocation_id", "parent_invocation_id", "task_id", "attempt_id",
			"generation_id", "bundle_id", "trigger_source", "purpose", "input_event_keys",
		} {
			assert.Contains(t, owner, key, "owner must expose %q (empty when unknown)", key)
		}
		assert.Equal(t, "p-3", owner["capture_namespace"])
		assert.Equal(t, "root-1", owner["root_session_id"])
		assert.Equal(t, "inv-7", owner["invocation_id"])
		assert.Equal(t, "inv-0", owner["parent_invocation_id"])
		assert.Equal(t, []any{"1a", "1b"}, owner["input_event_keys"])

		call, ok := raw["llm_call"].(map[string]any)
		require.True(t, ok)
		request, ok := call["request"].(map[string]any)
		require.True(t, ok)
		for _, key := range []string{"messages", "model", "generation_config", "tools"} {
			assert.Contains(t, request, key, "llm_call.request must expose %q", key)
		}
		tools, ok := request["tools"].([]any)
		require.True(t, ok, "frozen declarations must serialise as a list")
		require.Len(t, tools, 2)
		first, ok := tools[0].(map[string]any)
		require.True(t, ok)
		for _, key := range []string{"registry_key", "name", "description", "inputSchema", "outputSchema"} {
			assert.Contains(t, first, key, "tool declaration must expose %q", key)
		}
		assert.Equal(t, "alpha", first["registry_key"], "declarations are sorted by registry key, not map order")
		assert.Equal(t, "zeta", tools[1].(map[string]any)["registry_key"])
		require.Contains(t, call, "response", "v1-compatible terminal response stays")
		require.Contains(t, call, "response_fragments")
		require.Contains(t, call, "terminal_status")
		frags := call["response_fragments"].([]any)
		require.Len(t, frags, 1)
		f0 := frags[0].(map[string]any)
		assert.EqualValues(t, 0, f0["seq"])
		require.Contains(t, f0, "response", "fragment carries the whole model.Response, not a delta summary")
		assert.Equal(t, "resp-42", f0["response"].(map[string]any)["id"])

		meta, ok := raw["metadata"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, meta, "duration_ms")
		assert.Contains(t, meta, "model_endpoint")

		assert.NotEmpty(t, raw["request_digest"])
		var decoded CaptureRecord
		require.NoError(t, json.Unmarshal(lines[0], &decoded))
		assert.Len(t, decoded.RequestDigest, 64)
		assert.Equal(t, decoded.RequestDigest, computeRequestDigest(decoded.LLMCall.Request),
			"digest must recompute from the serialised request")
	})

	t.Run("missing_owner_items_are_named_not_fabricated", func(t *testing.T) {
		tr := newCaptureRecorder(t,
			&capStreamModel{responses: []*model.Response{termResp("resp-1", "pong", "stop")}}, captureEnabled())
		tr.SetSessionInfo("user-x", "sess-x")
		ch, err := tr.GenerateContent(context.Background(), capRequest("bare"))
		require.NoError(t, err)
		drainChan(t, ch)
		require.NoError(t, tr.Close())

		recs := readCaptureRecords(t, tr.dir, "sess-x")
		require.Len(t, recs, 1)
		rec := recs[0]

		assert.Empty(t, rec.Owner.AgentName, "an unknown attribution stays empty; no turn system is invented")
		assert.Empty(t, rec.Owner.CaptureNamespace)
		assert.Empty(t, rec.Owner.RootSessionID)
		assert.Equal(t, "sess-x", rec.Owner.SessionID, "the recorder's own session is still the session")
		assert.Equal(t, "user-x", rec.Owner.UserID)
		assert.Equal(t, BindingUnbound, rec.BindingStatus, "no invocation evidence ⇒ unbound, never a nearest-call guess")
		assert.Contains(t, rec.MissingReasons, MissingOwnerCaptureNamespace)
		assert.Contains(t, rec.MissingReasons, MissingOwnerAgentName)
		assert.Contains(t, rec.MissingReasons, MissingOwnerInvocationID)
		assert.Contains(t, rec.MissingReasons, MissingOwnerPurpose)
		assert.Equal(t, CaptureScopeSDKRequest, rec.CaptureScope)
		assert.True(t, strings.HasPrefix(rec.CallID, rec.RunID[:6]) || strings.Contains(rec.CallID, "-"),
			"call_id = run prefix + sequence: %q", rec.CallID)
	})

	t.Run("call_ids_are_unique_and_monotonic_within_a_run", func(t *testing.T) {
		tr := newCaptureRecorder(t,
			&capStreamModel{responses: []*model.Response{termResp("resp-1", "pong", "stop")}}, captureEnabled())
		tr.SetSessionInfo("u", "sess-seq")
		seen := map[string]bool{}
		for i := 0; i < 5; i++ {
			ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
			require.NoError(t, err)
			drainChan(t, ch)
		}
		require.NoError(t, tr.Close())
		recs := readCaptureRecords(t, tr.dir, "sess-seq")
		require.Len(t, recs, 5)
		for i, r := range recs {
			require.NotContains(t, seen, r.CallID, "call_id reused at index %d", i)
			seen[r.CallID] = true
			assert.Equal(t, i, r.BatchIndex, "v1 batch_index is retained")
			assert.Equal(t, r.RunID, recs[0].RunID, "one recorder shares one run_id")
		}
	})

	t.Run("endpoint_credentials_are_never_recorded", func(t *testing.T) {
		tr, err := NewTrajectoryRecorderWithOptions(&capStreamModel{responses: []*model.Response{termResp("r", "p", "stop")}},
			t.TempDir(), "https://user:secret@cap.example.com/v1/chat?api_key=SK-LEAK&x=1#ref-ZZZTOKEN", WithCapture(captureEnabled()))
		require.NoError(t, err)
		tr.SetSessionInfo("u", "sess-url")
		ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
		require.NoError(t, err)
		drainChan(t, ch)
		require.NoError(t, tr.Close())

		line := string(readCaptureLines(t, tr.dir, "sess-url")[0])
		assert.NotContains(t, line, "SK-LEAK")
		assert.NotContains(t, line, "secret")
		assert.NotContains(t, line, "ZZZTOKEN")
		assert.NotContains(t, line, "api_key")
		assert.Contains(t, line, "cap.example.com/v1/chat", "the endpoint stays useful without its query")
	})

	t.Run("capture_off_is_byte_identical_to_v1", func(t *testing.T) {
		mk := func(dir string, opts []RecorderOption) string {
			tr, err := NewTrajectoryRecorderWithOptions(
				&capStreamModel{responses: []*model.Response{termResp("resp-9", "pong", "stop")}},
				dir, "https://x.example.com/v1", opts...)
			require.NoError(t, err)
			tr.SetSessionInfo("u1", "s1")
			req := &model.Request{
				Messages: []model.Message{{Role: model.RoleUser, Content: "hi", ToolCalls: []model.ToolCall{
					{ID: "tc-1", Function: model.FunctionDefinitionParam{Name: "f", Arguments: []byte(`{"a":1}`)}},
				}}},
			}
			ch, err := tr.GenerateContent(context.Background(), req)
			require.NoError(t, err)
			drainChan(t, ch)
			require.NoError(t, tr.Close())
			data, rerr := os.ReadFile(filepath.Join(dir, "s1.jsonl"))
			require.NoError(t, rerr)
			return normalizeVolatile(string(data))
		}
		v1 := mk(t.TempDir(), nil)
		off := mk(t.TempDir(), []RecorderOption{WithCapture(CaptureConfig{Enabled: false})})
		assert.Equal(t, v1, off, "trajectory_capture off must not change the recorded bytes")
		assert.NotContains(t, off, "schema_version")
		assert.NotContains(t, off, "response_fragments")
	})
}

// timestampRe, durationRe and createdRe mask the volatile fields of a
// trajectory line: wall-clock timestamps and measured durations differ between
// two runs of the same scenario, everything else must match byte for byte.
var (
	timestampRe = regexp.MustCompile(`"timestamp":"[^"]*"`)
	durationRe  = regexp.MustCompile(`"duration_ms":[0-9]+`)
	createdRe   = regexp.MustCompile(`"created":[0-9]+`)
)

// normalizeVolatile replaces exactly those fields.
func normalizeVolatile(s string) string {
	out := timestampRe.ReplaceAllString(s, `"timestamp":"X"`)
	out = durationRe.ReplaceAllString(out, `"duration_ms":0`)
	out = createdRe.ReplaceAllString(out, `"created":0`)
	return out
}

func TestTrajectoryCapture_StreamFidelity(t *testing.T) {
	t.Run("fragments_are_in_receive_order_and_deep_copied", func(t *testing.T) {
		orig := []*model.Response{
			partialResp("resp-s", "Hello"),
			partialResp("resp-s", ", wor"),
			{ID: "resp-s", Object: model.ObjectTypeChatCompletionChunk, IsPartial: true, Choices: []model.Choice{{
				Delta: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
					ID: "call-1", Function: model.FunctionDefinitionParam{Name: "search", Arguments: []byte(`{"q":"a"}`)}}},
				}}}},
			termResp("resp-s", "done", "stop"),
		}
		tr := newCaptureRecorder(t, &capStreamModel{responses: orig}, captureEnabled())
		tr.SetSessionInfo("u", "sess-stream")
		ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
		require.NoError(t, err)
		got := drainChan(t, ch)
		require.Len(t, got, 4, "the consumer still receives every response")

		for _, r := range orig {
			if len(r.Choices) > 0 {
				r.Choices[0].Message.Content = "MUTATED"
				r.Choices[0].Delta.Content = "MUTATED"
				for i := range r.Choices[0].Message.ToolCalls {
					r.Choices[0].Message.ToolCalls[i].Function.Arguments[0] = 'X'
				}
				for i := range r.Choices[0].Delta.ToolCalls {
					r.Choices[0].Delta.ToolCalls[i].Function.Arguments[0] = 'X'
				}
			}
			r.ID = "MUTATED-ID"
		}

		require.NoError(t, tr.Close())
		recs := readCaptureRecords(t, tr.dir, "sess-stream")
		require.Len(t, recs, 1, "one call produces exactly one capture record")
		rec := recs[0]
		require.Len(t, rec.LLMCall.ResponseFragments, 4)
		assert.Equal(t, "Hello", rec.LLMCall.ResponseFragments[0].Response.Choices[0].Delta.Content)
		assert.Equal(t, ", wor", rec.LLMCall.ResponseFragments[1].Response.Choices[0].Delta.Content)
		require.Len(t, rec.LLMCall.ResponseFragments[2].Response.Choices[0].Delta.ToolCalls, 1)
		assert.Equal(t, []byte(`{"q":"a"}`), rec.LLMCall.ResponseFragments[2].Response.Choices[0].Delta.ToolCalls[0].Function.Arguments,
			"tool-call argument bytes are deep-copied, not aliased")
		assert.Equal(t, "resp-s", rec.LLMCall.ResponseFragments[3].Response.ID)
		for i, f := range rec.LLMCall.ResponseFragments {
			assert.Equal(t, i, f.Seq, "seq is the receive order")
		}
		assert.False(t, rec.ResponseIncomplete, "a terminal done response was observed")
		assert.Equal(t, TerminalDone, rec.LLMCall.TerminalStatus.Kind)
		assert.Equal(t, "stop", rec.LLMCall.TerminalStatus.FinishReason)
		assert.Equal(t, "resp-s", rec.LLMCall.TerminalStatus.ResponseID)
		assert.Equal(t, "done", rec.LLMCall.Response.Choices[0].Message.Content)
		assert.Equal(t, "stop", rec.LLMCall.Response.FinishReason)
	})

	t.Run("delta_only_stream_never_reconstructs_an_answer", func(t *testing.T) {
		tr := newCaptureRecorder(t, &capStreamModel{responses: []*model.Response{
			partialResp("resp-p", "tofu"), partialResp("resp-p", "fish"),
		}}, captureEnabled())
		tr.SetSessionInfo("u", "sess-delta")
		ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
		require.NoError(t, err)
		drainChan(t, ch)
		require.NoError(t, tr.Close())

		recs := readCaptureRecords(t, tr.dir, "sess-delta")
		require.Len(t, recs, 1)
		rec := recs[0]
		assert.True(t, rec.ResponseIncomplete)
		assert.True(t, rec.LLMCall.ResponseIncomplete)
		assert.Equal(t, TerminalClosedWithoutTerminal, rec.LLMCall.TerminalStatus.Kind)
		assert.Contains(t, rec.MissingReasons, MissingResponseTerminal)
		assert.Equal(t, 2, rec.LLMCall.TerminalStatus.Fragments)
		require.Len(t, rec.LLMCall.ResponseFragments, 2)
		assert.Equal(t, "fish", rec.LLMCall.Response.Choices[0].Delta.Content,
			"response stays the last received object; deltas are not concatenated")
		assert.NotContains(t, rec.LLMCall.Response.Choices[0].Message.Content, "tofufish")
	})

	t.Run("response_level_error_is_a_named_terminal", func(t *testing.T) {
		errResp := &model.Response{ID: "resp-e", Object: model.ObjectTypeError,
			Error: &model.ResponseError{Message: "rate limited", Type: model.ErrorTypeAPIError}}
		tr := newCaptureRecorder(t, &capStreamModel{responses: []*model.Response{
			partialResp("resp-e", "partial"), errResp}}, captureEnabled())
		tr.SetSessionInfo("u", "sess-rerr")
		ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
		require.NoError(t, err)
		drainChan(t, ch)
		require.NoError(t, tr.Close())

		recs := readCaptureRecords(t, tr.dir, "sess-rerr")
		require.Len(t, recs, 1)
		rec := recs[0]
		assert.Equal(t, TerminalError, rec.LLMCall.TerminalStatus.Kind)
		assert.Equal(t, "rate limited", rec.LLMCall.TerminalStatus.Error)
		assert.True(t, rec.ResponseIncomplete, "an errored stream has no complete answer")
		assert.Equal(t, "rate limited", rec.LLMCall.Response.Error, "v1 error text is preserved")
		require.Len(t, rec.LLMCall.ResponseFragments, 2, "the error object is still a received response")
	})

	t.Run("call_error_and_nil_channel_are_recorded_once", func(t *testing.T) {
		cases := map[string]model.Model{
			"call_error":  &capErrorModel{err: errors.New("connection refused")},
			"nil_channel": &capErrorModel{},
		}
		for name, inner := range cases {
			tr := newCaptureRecorder(t, inner, captureEnabled())
			tr.SetSessionInfo("u", "sess-"+name)
			ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
			if name == "call_error" {
				require.Error(t, err, "the model error is handed straight back")
				assert.Nil(t, ch)
			} else {
				require.NoError(t, err)
				assert.Nil(t, ch, "a nil channel is never ranged over")
			}
			require.NoError(t, tr.Close())

			recs := readCaptureRecords(t, tr.dir, "sess-"+name)
			require.Len(t, recs, 1, "%s: the anomaly is visible in the capture, not swallowed", name)
			rec := recs[0]
			assert.True(t, rec.ResponseIncomplete)
			assert.Empty(t, rec.LLMCall.ResponseFragments, "nothing was received, so nothing is claimed")
			switch name {
			case "call_error":
				assert.Equal(t, TerminalCallError, rec.LLMCall.TerminalStatus.Kind)
				assert.Contains(t, rec.LLMCall.TerminalStatus.Error, "connection refused")
			case "nil_channel":
				assert.Equal(t, TerminalNilChannel, rec.LLMCall.TerminalStatus.Kind)
			}
		}
	})

	t.Run("cancelled_stream_is_named_cancelled", func(t *testing.T) {
		m := &capStreamModel{responses: []*model.Response{partialResp("resp-c", "half")}, hold: true}
		tr := newCaptureRecorder(t, m, captureEnabled())
		tr.SetSessionInfo("u", "sess-cancel")
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := tr.GenerateContent(ctx, capRequest("go"))
		require.NoError(t, err)
		<-ch
		cancel()
		drainChan(t, ch)
		require.NoError(t, tr.Close())

		recs := readCaptureRecords(t, tr.dir, "sess-cancel")
		require.Len(t, recs, 1)
		rec := recs[0]
		assert.Equal(t, TerminalCancelled, rec.LLMCall.TerminalStatus.Kind)
		assert.True(t, rec.ResponseIncomplete)
		assert.Contains(t, rec.MissingReasons, MissingResponseTerminal)
	})

	t.Run("iter_entry_shares_the_channel_record_path", func(t *testing.T) {
		resp := []*model.Response{partialResp("resp-i", "a"), partialResp("resp-i", "b"), termResp("resp-i", "ab", "stop")}
		base := &iterCapableBase{responses: resp}
		tr := newCaptureRecorder(t, base, captureEnabled())
		tr.SetSessionInfo("u", "sess-iter-cap")
		seq, err := tr.GenerateContentIter(context.Background(), capRequest("go"))
		require.NoError(t, err)
		var n int
		seq(func(*model.Response) bool { n++; return true })
		require.NoError(t, tr.Close())

		recs := readCaptureRecords(t, tr.dir, "sess-iter-cap")
		require.Len(t, recs, 1, "the iterator path writes exactly one capture record")
		assert.Equal(t, 3, recs[0].LLMCall.TerminalStatus.Fragments)
		assert.Equal(t, TerminalDone, recs[0].LLMCall.TerminalStatus.Kind)
		assert.Equal(t, n, len(recs[0].LLMCall.ResponseFragments), "same fragments through either entry")

		tr2 := newCaptureRecorder(t, &capStreamModel{responses: resp}, captureEnabled())
		tr2.SetSessionInfo("u", "sess-chan-cap")
		ch, err := tr2.GenerateContent(context.Background(), capRequest("go"))
		require.NoError(t, err)
		drainChan(t, ch)
		require.NoError(t, tr2.Close())
		recs2 := readCaptureRecords(t, tr2.dir, "sess-chan-cap")
		require.Len(t, recs2, 1)
		assert.Equal(t, fragContents(recs[0]), fragContents(recs2[0]), "one record path, two entries")
		assert.Equal(t, recs[0].LLMCall.TerminalStatus, recs2[0].LLMCall.TerminalStatus)
	})

	t.Run("iter_consumer_break_still_records_once", func(t *testing.T) {
		base := &iterCapableBase{responses: []*model.Response{
			partialResp("resp-b", "1"), partialResp("resp-b", "2"), termResp("resp-b", "12", "stop")}}
		tr := newCaptureRecorder(t, base, captureEnabled())
		tr.SetSessionInfo("u", "sess-break")
		seq, err := tr.GenerateContentIter(context.Background(), capRequest("go"))
		require.NoError(t, err)
		seq(func(*model.Response) bool { return false })
		require.Eventually(t, func() bool { return tr.CaptureStats().Inflight == 0 }, 5*time.Second, 10*time.Millisecond,
			"the abandoned stream must still be drained and recorded")
		_, ferr := tr.FlushAndWait(context.Background())
		require.NoError(t, ferr)

		recs := readCaptureRecords(t, tr.dir, "sess-break")
		require.Len(t, recs, 1, "one call, one record — the break does not lose the observation")
		assert.Len(t, recs[0].LLMCall.ResponseFragments, 3)
	})

	t.Run("oversized_stream_truncates_and_releases_memory", func(t *testing.T) {
		big := strings.Repeat("x", 512)
		var resp []*model.Response
		for i := 0; i < 60; i++ {
			resp = append(resp, partialResp("resp-big", big))
		}
		resp = append(resp, termResp("resp-big", big, "stop"))
		cfg := CaptureConfig{Enabled: true, MaxRecordBytes: 4096}
		tr := newCaptureRecorder(t, &capStreamModel{responses: resp}, cfg)
		tr.SetSessionInfo("u", "sess-big")

		ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
		require.NoError(t, err)
		var seen int
		for range ch {
			seen++
		}
		assert.Equal(t, len(resp), seen, "the model path is not clipped: the consumer still gets every response")
		require.NoError(t, tr.Close())

		recs := readCaptureRecords(t, tr.dir, "sess-big")
		require.Len(t, recs, 1)
		rec := recs[0]
		assert.Less(t, len(rec.LLMCall.ResponseFragments), len(resp), "accumulation stops at the byte cap")
		assert.True(t, rec.LLMCall.TerminalStatus.Truncated)
		assert.True(t, rec.ResponseIncomplete)
		assert.Contains(t, rec.MissingReasons, MissingRecordTruncated)
		st := tr.CaptureStats()
		assert.GreaterOrEqual(t, st.Oversized, int64(1))
		assert.EqualValues(t, 0, st.PendingBytes, "the abandoned tail is released, never cached unboundedly")
		assert.EqualValues(t, 0, st.Inflight)
	})

	t.Run("oversized_request_stubbed_after_response_truncation", func(t *testing.T) {
		huge := strings.Repeat("r", 16<<10)
		req := capRequest(huge)
		cfg := CaptureConfig{Enabled: true, MaxRecordBytes: 4096}
		tr := newCaptureRecorder(t, &capStreamModel{responses: []*model.Response{termResp("resp-req", "pong", "stop")}}, cfg)
		tr.SetSessionInfo("u", "sess-req-big")

		ch, err := tr.GenerateContent(context.Background(), req)
		require.NoError(t, err)
		drainChan(t, ch)
		require.NoError(t, tr.Close())

		recs := readCaptureRecords(t, tr.dir, "sess-req-big")
		require.Len(t, recs, 1, "the record still lands after the request stub")
		rec := recs[0]
		assert.Empty(t, rec.LLMCall.Request.Messages, "the request side is stubbed, not silently kept over-cap")
		assert.Contains(t, rec.MissingReasons, MissingRequestStubbed)
		assert.Contains(t, rec.MissingReasons, MissingRecordTruncated)
		st := tr.CaptureStats()
		assert.GreaterOrEqual(t, st.Oversized, int64(1))
		assert.EqualValues(t, 0, st.OversizedDropped)
	})

	t.Run("record_above_cap_even_after_stub_is_dropped_and_counted", func(t *testing.T) {
		req := capRequest(strings.Repeat("r", 64<<10))
		cfg := CaptureConfig{Enabled: true, MaxRecordBytes: 256}
		tr := newCaptureRecorder(t, &capStreamModel{responses: []*model.Response{termResp("resp-drop", "pong", "stop")}}, cfg)
		tr.SetSessionInfo("u", "sess-req-drop")

		ch, err := tr.GenerateContent(context.Background(), req)
		require.NoError(t, err)
		drainChan(t, ch)
		require.NoError(t, tr.Close())

		recs := readCaptureRecords(t, tr.dir, "sess-req-drop")
		assert.Empty(t, recs, "a skeleton still above the cap is dropped, never written over-cap")
		st := tr.CaptureStats()
		assert.EqualValues(t, 1, st.OversizedDropped)
		mani, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		assert.False(t, mani.Complete, "a dropped record keeps the run partial")
	})

	t.Run("nested_wrap_claims_once", func(t *testing.T) {
		base := &capStreamModel{responses: []*model.Response{termResp("resp-n", "pong", "stop")}}
		tr := newCaptureRecorder(t, base, captureEnabled())
		tr.SetSessionInfo("u", "sess-nest")
		w := NewTrajectoryRecorderModelWrapper(tr, tr)
		req := capRequest("go")
		ch, err := w.GenerateContent(context.Background(), req)
		require.NoError(t, err)
		drainChan(t, ch)
		require.NoError(t, tr.Close())

		recs := readCaptureRecords(t, tr.dir, "sess-nest")
		assert.Len(t, recs, 1, "the nested pass claims the same request instead of recording it twice")
	})
}

func fragContents(rec *CaptureRecord) []string {
	out := make([]string, 0, len(rec.LLMCall.ResponseFragments))
	for _, f := range rec.LLMCall.ResponseFragments {
		if f.Response == nil || len(f.Response.Choices) == 0 {
			out = append(out, "")
			continue
		}
		c := f.Response.Choices[0]
		out = append(out, c.Message.Content+c.Delta.Content)
	}
	return out
}

func TestTrajectoryCapture_LossAccounting(t *testing.T) {
	t.Run("healthy_run_counts_and_seals_complete", func(t *testing.T) {
		m := &capStreamModel{responses: []*model.Response{termResp("resp-h", "pong", "stop")}}
		tr := newCaptureRecorder(t, m, captureEnabled())
		tr.SetSessionInfo("u", "sess-ledger")
		for i := 0; i < 3; i++ {
			ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
			require.NoError(t, err)
			drainChan(t, ch)
		}
		st := tr.CaptureStats()
		assert.EqualValues(t, 3, st.Started)
		assert.EqualValues(t, 3, st.Enqueued)
		assert.EqualValues(t, 0, st.DroppedFull)
		assert.EqualValues(t, 0, st.DroppedClosed)
		assert.EqualValues(t, 0, st.Oversized)

		man, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		assert.EqualValues(t, 3, man.Written)
		assert.Equal(t, tr.CaptureRunID(), man.RunID)
		assert.EqualValues(t, 3, man.CutoffSeq)
		assert.True(t, man.Synchronized, "the writer confirmed fsync")
		assert.True(t, man.Complete)
		assert.Equal(t, CaptureStatusComplete, man.Status)
		require.Len(t, man.Files, 1)
		assert.Equal(t, "sess-ledger", man.Files[0].SessionID)

		data, err := os.ReadFile(man.Files[0].Path)
		require.NoError(t, err)
		sum := sha256.Sum256(data)
		assert.Equal(t, hex.EncodeToString(sum[:]), man.Files[0].SHA256,
			"a sealed run's file digest re-verifies from disk")
		assert.EqualValues(t, len(data), man.Files[0].RunBytes)

		require.NotNil(t, tr.capture)
		assert.EqualValues(t, 3, tr.capture.counters.started.Load())
	})

	t.Run("stalled_writer_drops_and_reports_partial", func(t *testing.T) {
		dir := t.TempDir()
		base := newDiskSink()
		sink := &stallSink{inner: base, gate: make(chan struct{})}
		sink.block()
		m := &capStreamModel{responses: []*model.Response{termResp("resp-f", "pong", "stop")}}
		tr, err := NewTrajectoryRecorderWithOptions(m, dir, "https://x/v1",
			WithCapture(CaptureConfig{Enabled: true}), withCaptureSink(sink))
		require.NoError(t, err)
		tr.SetSessionInfo("u", "sess-full")

		// Model path must keep going while the writer is stuck.
		const calls = captureQueueBufferSize + 60
		for i := 0; i < calls; i++ {
			ch, gerr := tr.GenerateContent(context.Background(), capRequest("go"))
			require.NoError(t, gerr)
			got := drainChan(t, ch)
			require.Len(t, got, 1, "a stalled disk never blocks the model")
		}

		st := tr.CaptureStats()
		assert.Greater(t, st.DroppedFull, int64(0), "queue-full drops are counted, not silent")
		assert.EqualValues(t, calls, st.Started)
		assert.Equal(t, int64(calls), st.Enqueued+st.DroppedFull+st.DroppedDiskLimit,
			"every started call lands in exactly one counter")

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, ferr := tr.FlushAndWait(ctx)
		cancel()
		require.Error(t, ferr, "a stuck writer yields an explicit failure, never a fake manifest")

		sink.release()
		man, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		assert.Greater(t, man.Written, int64(0))
		assert.False(t, man.Complete, "dropped evidence cannot be reported as a complete capture")
		assert.Equal(t, CaptureStatusPartial, man.Status)
		require.NoError(t, tr.Close())
		assert.Greater(t, len(readCaptureLines(t, dir, "sess-full")), 0)
		sink.wait(t)
	})

	t.Run("records_emitted_after_close_count_as_dropped_closed", func(t *testing.T) {
		tr := newCaptureRecorder(t, &capStreamModel{responses: []*model.Response{termResp("r", "p", "stop")}}, captureEnabled())
		tr.SetSessionInfo("u", "sess-closed")
		require.NoError(t, tr.Close())
		ch, err := tr.GenerateContent(context.Background(), capRequest("late"))
		require.NoError(t, err)
		drainChan(t, ch)
		assert.GreaterOrEqual(t, tr.CaptureStats().DroppedClosed, int64(1),
			"post-close loss is named, not swallowed")
	})

	t.Run("serialize_failure_is_counted_and_writes_no_line", func(t *testing.T) {
		tr := newCaptureRecorder(t, &capStreamModel{responses: []*model.Response{termResp("r", "p", "stop")}}, captureEnabled())
		tr.SetSessionInfo("u", "sess-serial")
		bad := &model.Request{
			Messages: []model.Message{{Role: model.RoleUser, Content: "go"}},
			Tools: map[string]tool.Tool{"weird": capTool{&tool.Declaration{
				Name: "weird", InputSchema: &tool.Schema{Type: "object", AdditionalProperties: make(chan int)},
			}}},
		}
		ch, err := tr.GenerateContent(context.Background(), bad)
		require.NoError(t, err)
		drainChan(t, ch)
		_, ferr := tr.FlushAndWait(context.Background())
		require.NoError(t, ferr)

		st := tr.CaptureStats()
		assert.EqualValues(t, 1, st.SerializeFailed)
		assert.EqualValues(t, 0, st.Written)
		assert.False(t, st.Complete())
		assert.Empty(t, readCaptureLines(t, tr.dir, "sess-serial"), "a record that cannot be serialised is not half-written")
	})

	t.Run("write_and_sync_failures_are_visible_in_the_manifest", func(t *testing.T) {
		dir := t.TempDir()
		tr, err := NewTrajectoryRecorderWithOptions(
			&capStreamModel{responses: []*model.Response{termResp("r", "p", "stop")}}, dir, "https://x/v1",
			WithCapture(captureEnabled()), withCaptureSink(failWriteSink{inner: newDiskSink()}))
		require.NoError(t, err)
		tr.SetSessionInfo("u", "sess-wfail")
		ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
		require.NoError(t, err)
		drainChan(t, ch)
		man, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		assert.EqualValues(t, 1, man.WriteFailed)
		assert.EqualValues(t, 0, man.Written)
		assert.False(t, man.Complete)
		assert.Equal(t, CaptureStatusPartial, man.Status)
		require.NoError(t, tr.Close())

		tr2, err := NewTrajectoryRecorderWithOptions(
			&capStreamModel{responses: []*model.Response{termResp("r", "p", "stop")}}, t.TempDir(), "https://x/v1",
			WithCapture(captureEnabled()), withCaptureSink(failSyncSink{inner: newDiskSink()}))
		require.NoError(t, err)
		tr2.SetSessionInfo("u", "sess-sfail")
		ch2, err := tr2.GenerateContent(context.Background(), capRequest("go"))
		require.NoError(t, err)
		drainChan(t, ch2)
		man2, err := tr2.FlushAndWait(context.Background())
		require.NoError(t, err)
		assert.GreaterOrEqual(t, man2.SyncFailed, int64(1))
		assert.False(t, man2.Synchronized)
		assert.False(t, man2.Complete, "an unconfirmed fsync is not a synchronized capture")
		require.NoError(t, tr2.Close())
	})

	t.Run("run_bytes_cap_seals_partial_and_keeps_history", func(t *testing.T) {
		dir := t.TempDir()
		m := &capStreamModel{responses: []*model.Response{termResp("r", strings.Repeat("y", 2048), "stop")}}
		tr, err := NewTrajectoryRecorderWithOptions(m, dir, "https://x/v1",
			WithCapture(CaptureConfig{Enabled: true, MaxRunBytes: 8192}))
		require.NoError(t, err)
		tr.SetSessionInfo("u", "sess-disk")
		var calls, delivered int
		for i := 0; i < 40; i++ {
			ch, gerr := tr.GenerateContent(context.Background(), capRequest("go"))
			require.NoError(t, gerr)
			calls++
			if len(drainChan(t, ch)) == 1 {
				delivered++
			}
		}
		_, ferr := tr.FlushAndWait(context.Background())
		require.NoError(t, ferr)
		man, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		assert.EqualValues(t, calls, man.Started)
		assert.Equal(t, delivered, calls, "hitting the run budget never touches the model path")
		assert.Greater(t, man.DroppedDiskLimit, int64(0), "the budget stop is counted")
		assert.LessOrEqual(t, man.RunBytes, int64(8192))
		assert.False(t, man.Complete)
		assert.Equal(t, CaptureStatusPartial, man.Status)

		before := len(readCaptureLines(t, dir, "sess-disk"))
		require.Greater(t, before, 0)
		require.NoError(t, tr.Close())
		after := len(readCaptureLines(t, dir, "sess-disk"))
		assert.Equal(t, before, after, "the cap stops new data; it never deletes or rotates history")
	})

	t.Run("open_handles_are_capped_and_files_survive_eviction", func(t *testing.T) {
		dir := t.TempDir()
		m := &capStreamModel{responses: []*model.Response{termResp("r", "p", "stop")}}
		tr, err := NewTrajectoryRecorderWithOptions(m, dir, "https://x/v1", WithCapture(captureEnabled()))
		require.NoError(t, err)
		for i := 0; i < MaxCaptureOpenFiles+6; i++ {
			sid := fmt.Sprintf("sess-handle-%02d", i)
			tr.SetSessionInfo("u", sid)
			ch, gerr := tr.GenerateContent(context.Background(), capRequest("go"))
			require.NoError(t, gerr)
			drainChan(t, ch)
		}
		man, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		require.NoError(t, tr.Close())

		assert.LessOrEqual(t, tr.capture.maxOpenHandles.Load(), int32(MaxCaptureOpenFiles),
			"at most 16 files are open at once")
		assert.Len(t, man.Files, MaxCaptureOpenFiles+6, "every session is still accounted in the manifest")
		for i := 0; i < MaxCaptureOpenFiles+6; i++ {
			p := filepath.Join(dir, fmt.Sprintf("sess-handle-%02d.jsonl", i))
			data, rerr := os.ReadFile(p)
			require.NoErrorf(t, rerr, "evicting a handle must never delete the file (%d)", i)
			assert.Len(t, splitJSONL(data), 1)
		}
	})

	t.Run("inflight_blocks_completeness_until_the_stream_ends", func(t *testing.T) {
		m := &capStreamModel{responses: []*model.Response{partialResp("r", "half")}, hold: true}
		tr := newCaptureRecorder(t, m, captureEnabled())
		tr.SetSessionInfo("u", "sess-inflight")
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := tr.GenerateContent(ctx, capRequest("go"))
		require.NoError(t, err)
		<-ch

		require.Eventually(t, func() bool { return tr.CaptureStats().Inflight > 0 }, 3*time.Second, 10*time.Millisecond)
		man, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		assert.Greater(t, man.Inflight, int64(0))
		assert.False(t, man.Complete, "an in-flight call is not a complete capture")

		cancel()
		drainChan(t, ch)
		require.Eventually(t, func() bool { return tr.CaptureStats().Inflight == 0 }, 5*time.Second, 10*time.Millisecond)
		man2, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		assert.EqualValues(t, 0, man2.Inflight)
		assert.True(t, man2.Complete, "the same run becomes complete once it truly quiesced")
	})

	t.Run("pending_bytes_stay_within_the_bound", func(t *testing.T) {
		payload := strings.Repeat("z", 4096)
		m := &capStreamModel{responses: []*model.Response{termResp("r", payload, "stop")}}
		cfg := CaptureConfig{Enabled: true, MaxPendingBytes: 32 * 1024}
		tr := newCaptureRecorder(t, m, cfg)
		tr.SetSessionInfo("u", "sess-pend")
		for i := 0; i < 200; i++ {
			ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
			require.NoError(t, err)
			drainChan(t, ch)
		}
		st := tr.CaptureStats()
		assert.LessOrEqual(t, st.MaxPendingBytes, cfg.MaxPendingBytes,
			"copies + accumulated fragments + queued records + serialize buffers share one bound")
		require.NoError(t, tr.Close())
		assert.EqualValues(t, 0, tr.CaptureStats().PendingBytes, "closing releases every byte")
	})

	t.Run("final_seal_happens_exactly_once", func(t *testing.T) {
		tr := newCaptureRecorder(t, &capStreamModel{responses: []*model.Response{termResp("r", "p", "stop")}}, captureEnabled())
		tr.SetSessionInfo("u", "sess-seal")
		ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
		require.NoError(t, err)
		drainChan(t, ch)

		mid, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		assert.False(t, mid.Sealed, "an intermediate flush is not the final seal")

		require.NoError(t, tr.Close())
		require.EqualValues(t, 1, tr.capture.sealCount.Load(), "one run seals exactly once")
		first, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		second, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		assert.True(t, first.Sealed)
		assert.Equal(t, CaptureStatusComplete, first.Status)
		assert.Equal(t, first.CutoffSeq, second.CutoffSeq, "post-close flushes replay the sealed manifest")
		assert.Equal(t, first.Files[0].SHA256, second.Files[0].SHA256)
		require.EqualValues(t, 1, tr.capture.sealCount.Load())
	})

	t.Run("unbound_and_ambiguous_are_counted", func(t *testing.T) {
		tr := newCaptureRecorder(t, &capIDByRequest{withID: map[string]bool{"a": true}}, captureEnabled())
		tr.SetSessionInfo("u", "sess-bind")

		idScope := NewCaptureScope("inv-with-id")
		idScope.Owner = OwnerAttrs{"capture_namespace": "p1", "root_session_id": "s1", "agent_name": "child", "purpose": "policy"}
		noIDSlice := NewCaptureScope("inv-without-id")
		noIDSlice.Owner = OwnerAttrs{"capture_namespace": "p1", "root_session_id": "s1", "agent_name": "child", "purpose": "policy"}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			ch, err := tr.GenerateContent(WithCaptureScope(context.Background(), idScope), capRequest("a"))
			if err != nil {
				return
			}
			drainChan(t, ch)
		}()
		go func() {
			defer wg.Done()
			ch, err := tr.GenerateContent(WithCaptureScope(context.Background(), noIDSlice), capRequest("b"))
			if err != nil {
				return
			}
			drainChan(t, ch)
		}()
		wg.Wait()
		require.NoError(t, tr.Close())

		byInvocation := map[string]*CaptureRecord{}
		for _, r := range readCaptureRecords(t, tr.dir, "sess-bind") {
			byInvocation[r.Owner.InvocationID] = r
		}
		bound, ok := byInvocation["inv-with-id"]
		require.True(t, ok)
		assert.Equal(t, BindingBound, bound.BindingStatus)
		assert.NotEmpty(t, bound.Owner.CaptureNamespace)
		assert.Empty(t, bound.MissingReasons)

		loose := byInvocation["inv-without-id"]
		require.NotNil(t, loose, "the id-less sibling is in the same shared file")
		assert.Equal(t, BindingUnbound, loose.BindingStatus,
			"no stable response id ⇒ unbound; the nearest call is never assumed")
		assert.Contains(t, loose.MissingReasons, MissingBindingNoResponseID)
		assert.EqualValues(t, 1, tr.CaptureStats().UnboundNoResponseID)
		assert.Equal(t, loose.RunID, bound.RunID, "one recorder shares one run_id")
		assert.NotEqual(t, loose.CallID, bound.CallID)

		dup := NewCaptureScope("inv-dup")
		dup.Owner = OwnerAttrs{"capture_namespace": "p1", "root_session_id": "s1", "agent_name": "child", "purpose": "policy"}
		tr3 := newCaptureRecorder(t, &capStreamModel{responses: []*model.Response{termResp("resp-dup", "p", "stop")}}, captureEnabled())
		tr3.SetSessionInfo("u", "sess-dup")
		for i := 0; i < 2; i++ {
			ch, err := tr3.GenerateContent(WithCaptureScope(context.Background(), dup), capRequest("go"))
			require.NoError(t, err)
			drainChan(t, ch)
		}
		require.NoError(t, tr3.Close())
		recs := readCaptureRecords(t, tr3.dir, "sess-dup")
		require.Len(t, recs, 2)
		assert.Equal(t, BindingBound, recs[0].BindingStatus)
		assert.Equal(t, BindingAmbiguous, recs[1].BindingStatus, "a conflicting link is ambiguous")
		assert.GreaterOrEqual(t, tr3.CaptureStats().Ambiguous, int64(1))
	})

	t.Run("scope_capacity_overflow_is_named_not_guessed", func(t *testing.T) {
		scope := NewCaptureScope("inv-full")
		for i := 0; i < captureScopeCapacity; i++ {
			scope.Link(fmt.Sprintf("resp:%d", i), fmt.Sprintf("call-%d", i))
		}
		_, res := scope.Link("resp:overflow", "call-overflow")
		assert.Equal(t, LinkCapacity, res, "an active mapping is never evicted to make room for a guess")
		st := scope.Stats()
		assert.Equal(t, captureScopeCapacity, st.Entries)
		assert.EqualValues(t, 1, st.Overflow)

		tr := newCaptureRecorder(t, &capStreamModel{responses: []*model.Response{termResp("resp-x", "p", "stop")}}, captureEnabled())
		tr.SetSessionInfo("u", "sess-cap")
		over := NewCaptureScope("inv-overflow")
		for i := 0; i < captureScopeCapacity; i++ {
			over.Link(fmt.Sprintf("seed:%d", i), fmt.Sprintf("seed-call-%d", i))
		}
		ch, err := tr.GenerateContent(WithCaptureScope(context.Background(), over), capRequest("go"))
		require.NoError(t, err)
		drainChan(t, ch)
		require.NoError(t, tr.Close())
		recs := readCaptureRecords(t, tr.dir, "sess-cap")
		require.Len(t, recs, 1)
		assert.Equal(t, BindingUnbound, recs[0].BindingStatus)
		assert.Contains(t, recs[0].MissingReasons, MissingBindingCapacity)
		assert.GreaterOrEqual(t, tr.CaptureStats().UnboundCapacity, int64(1))
	})

	t.Run("private_permissions_for_capture_output", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cap", "nested")
		tr, err := NewTrajectoryRecorderWithOptions(
			&capStreamModel{responses: []*model.Response{termResp("r", "p", "stop")}}, dir, "https://x/v1",
			WithCapture(captureEnabled()))
		require.NoError(t, err)
		tr.SetSessionInfo("u", "sess-perm")
		ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
		require.NoError(t, err)
		drainChan(t, ch)
		require.NoError(t, tr.Close())

		di, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), di.Mode().Perm(), "capture output dirs are private")
		fi, err := os.Stat(filepath.Join(dir, "sess-perm.jsonl"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "capture files are private")
	})

	t.Run("manifest_document_is_consumer_shaped", func(t *testing.T) {
		tr := newCaptureRecorder(t, &capStreamModel{responses: []*model.Response{termResp("r", "p", "stop")}}, captureEnabled())
		tr.SetSessionInfo("u", "sess-doc")
		ch, err := tr.GenerateContent(context.Background(), capRequest("go"))
		require.NoError(t, err)
		drainChan(t, ch)
		man, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)

		doc, err := MarshalCaptureManifest([]CaptureManifest{man})
		require.NoError(t, err)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal(doc, &parsed))
		require.Contains(t, parsed, "runs")
		runs, ok := parsed["runs"].(map[string]any)
		require.True(t, ok)
		entry, ok := runs[man.RunID].(map[string]any)
		require.True(t, ok)
		for _, key := range []string{"complete", "synchronized", "inflight", "pending",
			"dropped_full", "dropped_closed", "serialize_failed", "write_failed", "sync_failed"} {
			assert.Contains(t, entry, key, "manifest run entry must expose flat %q", key)
		}
		assert.Equal(t, true, entry["complete"])
	})
}

func BenchmarkTrajectoryCapture(b *testing.B) {
	resp := []*model.Response{termResp("resp-bench", strings.Repeat("q", 256), "stop")}
	for _, enabled := range []bool{false, true} {
		name := "capture=off"
		if enabled {
			name = "capture=on"
		}
		b.Run(name, func(b *testing.B) {
			dir := b.TempDir()
			opts := []RecorderOption{}
			if enabled {
				opts = append(opts, WithCapture(CaptureConfig{Enabled: true}))
			}
			tr, err := NewTrajectoryRecorderWithOptions(&capStreamModel{responses: resp}, dir, "https://x/v1", opts...)
			if err != nil {
				b.Fatal(err)
			}
			tr.SetSessionInfo("bench-u", "bench-s")
			req := &model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ch, err := tr.GenerateContent(context.Background(), req)
				if err != nil {
					b.Fatal(err)
				}
				for range ch {
				}
			}
			b.StopTimer()
			if enabled {
				if _, err := tr.FlushAndWait(context.Background()); err != nil {
					b.Fatal(err)
				}
				st := tr.CaptureStats()
				b.ReportMetric(float64(st.MaxPendingBytes), "pending_bytes")
				b.ReportMetric(float64(st.DroppedFull+st.DroppedClosed+st.Oversized), "dropped")
			}
			if err := tr.Close(); err != nil {
				b.Fatal(err)
			}
		})
	}
}
