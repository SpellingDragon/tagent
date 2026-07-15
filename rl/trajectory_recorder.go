package rl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ---------------------------------------------------------------------------
// TrajectoryRecorder — 记录 LLM 调用轨迹的 model.Model 包装器
//
// 在任何运行模式（智谱AI / AReaL proxy）下，包装内部 model.Model，
// 异步将每次 LLM 调用的 request/response 记录为 JSONL 文件。
// 与 SwappableModel 可组合：TrajectoryRecorder(SwappableModel(model))。
//
// 设计要点：
//   - 实现 model.Model 接口，不侵入 runner/llmagent 内部逻辑
//   - 异步写入：buffered channel + 后台 goroutine，不阻塞 LLM 调用
//   - channel 满时丢弃记录并打 warning log
//   - 按 session 组织文件：{trajectory_dir}/{session_id}.jsonl
// ---------------------------------------------------------------------------

// TrajectoryRecord 是 JSONL 文件中每行的 JSON 结构。
type TrajectoryRecord struct {
	Timestamp  string             `json:"timestamp"`
	SessionID  string             `json:"session_id"`
	UserID     string             `json:"user_id"`
	BatchIndex int                `json:"batch_index"`
	LLMCall    LLMCallRecord      `json:"llm_call"`
	Metadata   TrajectoryMetadata `json:"metadata"`
}

// LLMCallRecord 记录单次 LLM 调用的 request 和 response。
type LLMCallRecord struct {
	Request  LLMRequestRecord  `json:"request"`
	Response LLMResponseRecord `json:"response"`
}

// LLMRequestRecord 记录 LLM 请求。
type LLMRequestRecord struct {
	Messages         []model.Message        `json:"messages"`
	Model            string                 `json:"model"`
	GenerationConfig model.GenerationConfig `json:"generation_config,omitempty"`
}

// LLMResponseRecord 记录 LLM 响应。
type LLMResponseRecord struct {
	Choices      []model.Choice `json:"choices,omitempty"`
	Usage        *model.Usage   `json:"usage,omitempty"`
	FinishReason string         `json:"finish_reason,omitempty"`
	Error        string         `json:"error,omitempty"`
}

// TrajectoryMetadata 记录调用元数据。
type TrajectoryMetadata struct {
	DurationMs    int64  `json:"duration_ms"`
	ModelEndpoint string `json:"model_endpoint"`
}

// TrajectoryRecorder wraps a model.Model and records every LLM call
// to a JSONL file asynchronously.
type TrajectoryRecorder struct {
	mu         sync.Mutex
	inner      model.Model
	dir        string
	userID     string
	sessionID  string
	batchIndex int
	endpoint   string

	recordCh chan *TrajectoryRecord
	wg       sync.WaitGroup // writeLoop goroutine
	gcWg     sync.WaitGroup // in-flight GenerateContent goroutines
	closed   bool
	closeMu  sync.Mutex
}

// channelBufferSize controls how many records can be buffered before dropping.
const channelBufferSize = 256

// NewTrajectoryRecorder creates a TrajectoryRecorder wrapping the given model.
// The trajectoryDir will be created if it does not exist.
func NewTrajectoryRecorder(inner model.Model, trajectoryDir, modelEndpoint string) (*TrajectoryRecorder, error) {
	if err := os.MkdirAll(trajectoryDir, 0o755); err != nil {
		return nil, err
	}

	tr := &TrajectoryRecorder{
		inner:    inner,
		dir:      trajectoryDir,
		endpoint: modelEndpoint,
		recordCh: make(chan *TrajectoryRecord, channelBufferSize),
	}

	log.Infof("[TrajectoryRecorder] initialized: dir=%s endpoint=%s", trajectoryDir, modelEndpoint)

	tr.wg.Add(1)
	go tr.writeLoop()

	return tr, nil
}

// SetSessionInfo updates the current session context for trajectory recording.
// This should be called when a new session starts (e.g., from StartLoop).
func (tr *TrajectoryRecorder) SetSessionInfo(userID, sessionID string) {
	tr.mu.Lock()
	tr.userID = userID
	tr.sessionID = sessionID
	tr.batchIndex = 0
	tr.mu.Unlock()
}

// SetModelEndpoint updates the model endpoint recorded in metadata.
// Useful when SwappableModel swaps to a new endpoint.
func (tr *TrajectoryRecorder) SetModelEndpoint(endpoint string) {
	tr.mu.Lock()
	tr.endpoint = endpoint
	tr.mu.Unlock()
}

// GenerateContent implements model.Model.
func (tr *TrajectoryRecorder) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	return tr.recordGenerateContent(ctx, tr.inner, request)
}

// Info implements model.Model.
func (tr *TrajectoryRecorder) Info() model.Info {
	return tr.inner.Info()
}

// Close flushes pending records and shuts down the writer goroutine.
// Close sends a flush sentinel before closing the channel, ensuring
// the writeLoop syncs all pending data to disk before exiting.
func (tr *TrajectoryRecorder) Close() error {
	tr.gcWg.Wait()
	tr.closeMu.Lock()
	if tr.closed {
		tr.closeMu.Unlock()
		return nil
	}
	tr.closed = true
	// Send flush sentinel before closing channel so writeLoop syncs files.
	// Non-blocking: if channel is full, writeLoop will drain + sync on close.
	select {
	case tr.recordCh <- nil:
	default:
	}
	close(tr.recordCh)
	tr.closeMu.Unlock()
	tr.wg.Wait() // writeLoop drains all records, syncs, and closes files
	return nil
}

// Flush forces all buffered records to be written to disk.
// This is idempotent — calling multiple times has no side effects.
// Flush sends a sentinel nil record through the channel; the writeLoop
// processes all pending records before the sentinel, then syncs the file.
//
// Thread-safety: Flush holds closeMu for the entire operation, consistent
// with the record() method, preventing races with Close().
func (tr *TrajectoryRecorder) Flush() {
	tr.closeMu.Lock()
	defer tr.closeMu.Unlock()
	if tr.closed {
		return
	}

	// Send a flush sentinel (nil record). writeLoop handles nil as a flush signal.
	// Use non-blocking send; if channel is full, records are already being processed.
	select {
	case tr.recordCh <- nil:
	default:
	}
}

// record pushes a record to the async channel. Non-blocking; drops on full or closed.
func (tr *TrajectoryRecorder) record(r *TrajectoryRecord) {
	tr.closeMu.Lock()
	defer tr.closeMu.Unlock()
	if tr.closed {
		return
	}
	select {
	case tr.recordCh <- r:
	default:
		log.Warnf("[TrajectoryRecorder] channel full, dropping record for session=%s batch=%d", r.SessionID, r.BatchIndex)
	}
}

// recordGenerateContent is the shared implementation for both TrajectoryRecorder
// and TrajectoryRecorderModelWrapper. It forwards to inner, captures session
// context from tr, and records the interaction asynchronously.
func (tr *TrajectoryRecorder) recordGenerateContent(ctx context.Context, inner model.Model, request *model.Request) (<-chan *model.Response, error) {
	start := time.Now()

	tr.mu.Lock()
	userID := tr.userID
	sessionID := tr.sessionID
	batchIdx := tr.batchIndex
	tr.batchIndex++
	endpoint := tr.endpoint
	tr.mu.Unlock()

	modelName := inner.Info().Name

	respCh, err := inner.GenerateContent(ctx, request)
	if err != nil {
		tr.record(&TrajectoryRecord{
			Timestamp:  time.Now().Format(time.RFC3339Nano),
			SessionID:  sessionID,
			UserID:     userID,
			BatchIndex: batchIdx,
			LLMCall: LLMCallRecord{
				Request: LLMRequestRecord{
					Messages:         request.Messages,
					Model:            modelName,
					GenerationConfig: request.GenerationConfig,
				},
				Response: LLMResponseRecord{Error: err.Error()},
			},
			Metadata: TrajectoryMetadata{
				DurationMs:    time.Since(start).Milliseconds(),
				ModelEndpoint: endpoint,
			},
		})
		return nil, err
	}

	wrappedCh := make(chan *model.Response, 64)
	tr.gcWg.Add(1)
	go func() {
		defer tr.gcWg.Done()
		defer close(wrappedCh)
		var lastResp *model.Response
		for resp := range respCh {
			wrappedCh <- resp
			if resp != nil {
				lastResp = resp
			}
		}

		record := &TrajectoryRecord{
			Timestamp:  time.Now().Format(time.RFC3339Nano),
			SessionID:  sessionID,
			UserID:     userID,
			BatchIndex: batchIdx,
			LLMCall: LLMCallRecord{
				Request: LLMRequestRecord{
					Messages:         request.Messages,
					Model:            modelName,
					GenerationConfig: request.GenerationConfig,
				},
			},
			Metadata: TrajectoryMetadata{
				DurationMs:    time.Since(start).Milliseconds(),
				ModelEndpoint: endpoint,
			},
		}

		if lastResp != nil {
			record.LLMCall.Response = LLMResponseRecord{
				Choices: lastResp.Choices,
				Usage:   lastResp.Usage,
			}
			if len(lastResp.Choices) > 0 && lastResp.Choices[0].FinishReason != nil {
				record.LLMCall.Response.FinishReason = *lastResp.Choices[0].FinishReason
			}
			if lastResp.Error != nil {
				record.LLMCall.Response.Error = lastResp.Error.Message
			}
		}

		record.Metadata.DurationMs = time.Since(start).Milliseconds()
		tr.record(record)
	}()

	return wrappedCh, nil
}

// writeLoop is the background goroutine that writes records to JSONL files.
func (tr *TrajectoryRecorder) writeLoop() {
	defer tr.wg.Done()

	fileMu := sync.Mutex{}
	openFiles := make(map[string]*os.File)

	getFile := func(sessionID string) (*os.File, error) {
		fileMu.Lock()
		defer fileMu.Unlock()
		if f, ok := openFiles[sessionID]; ok {
			return f, nil
		}
		if sessionID == "" {
			sessionID = "default"
		}
		path := filepath.Join(tr.dir, sessionID+".jsonl")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, err
		}
		openFiles[sessionID] = f
		log.Infof("[TrajectoryRecorder] writing to %s", path)
		return f, nil
	}

	for r := range tr.recordCh {
		// nil record is a flush sentinel — sync all open files
		if r == nil {
			fileMu.Lock()
			for _, f := range openFiles {
				if err := f.Sync(); err != nil {
					log.Warnf("[TrajectoryRecorder] flush sync failed: %v", err)
				}
			}
			fileMu.Unlock()
			continue
		}
		f, err := getFile(r.SessionID)
		if err != nil {
			log.Warnf("[TrajectoryRecorder] failed to open file for session %s: %v", r.SessionID, err)
			continue
		}
		data, err := json.Marshal(r)
		if err != nil {
			log.Warnf("[TrajectoryRecorder] failed to marshal record: %v", err)
			continue
		}
		data = append(data, '\n')
		if _, err := f.Write(data); err != nil {
			log.Warnf("[TrajectoryRecorder] failed to write record: %v", err)
		}
	}

	fileMu.Lock()
	for _, f := range openFiles {
		f.Close()
	}
	fileMu.Unlock()
}

// TrajectoryRecorderModelWrapper wraps a different model.Model instance
// but shares the same TrajectoryRecorder's record channel and writeLoop.
// Used to wrap sub-agent models (e.g., knowledge agent's glm-4.7) so
// their LLM calls are also recorded for RL training data.
type TrajectoryRecorderModelWrapper struct {
	inner model.Model
	tr    *TrajectoryRecorder
}

func NewTrajectoryRecorderModelWrapper(inner model.Model, tr *TrajectoryRecorder) *TrajectoryRecorderModelWrapper {
	return &TrajectoryRecorderModelWrapper{inner: inner, tr: tr}
}

func (w *TrajectoryRecorderModelWrapper) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	return w.tr.recordGenerateContent(ctx, w.inner, request)
}

func (w *TrajectoryRecorderModelWrapper) Info() model.Info {
	return w.inner.Info()
}
