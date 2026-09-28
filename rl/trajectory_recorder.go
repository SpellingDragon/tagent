package rl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

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
	// TraceID 关联产生这次调用的 turn span（取自 ctx，未启用导出时为空）；omitempty ⇒
	// 旧 RL 消费者向后兼容，RL 训练投影与 OTel 运维投影共用同一锚点互链。
	TraceID string `json:"trace_id,omitempty"`
	SpanID  string `json:"span_id,omitempty"`
}

// traceIDsFromCtx 从 ctx 提取当前 span 的 trace_id/span_id（hex）；noop/无 span 返回空。
func traceIDsFromCtx(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
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

// TrajectoryRecorder 包装 model.Model，把每次 LLM 调用异步落成 JSONL 记录。
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
type TrajectoryRecorder struct {
	mu         sync.Mutex
	inner      model.Model
	dir        string
	userID     string
	sessionID  string
	batchIndex int
	endpoint   string

	recordCh chan *TrajectoryRecord
	// wg 等后台写协程退出。
	wg sync.WaitGroup
	// gcWg 是在途 GenerateContent 协程组：Close 先等它归零，才不会漏掉仍在写的记录。
	gcWg    sync.WaitGroup
	closed  bool
	closeMu sync.Mutex
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

// GenerateContentIter 暴露迭代入口，使偏好 model.IterModel 的流程不被本装饰器降级。它复用
// recordGenerateContent（分配批次号、持 gcWg 租约、流结束时落记录），但只在调用方真正开始
// 迭代时才触发：构造迭代器既不占批次号也不调用模型。录制器本要观察每条响应，因此保留的是
// 迭代的契约与能力面，而非把它们藏在 GenerateContent 之后。
func (tr *TrajectoryRecorder) GenerateContentIter(ctx context.Context, request *model.Request) (model.Seq[*model.Response], error) {
	return func(yield func(*model.Response) bool) {
		ch, err := tr.recordGenerateContent(ctx, tr.inner, request)
		if err != nil || ch == nil {
			return
		}
		for resp := range ch {
			if !yield(resp) {
				go func() {
					for range ch {
					}
				}()
				return
			}
		}
	}, nil
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
	select {
	case tr.recordCh <- nil:
	default:
	}
	close(tr.recordCh)
	tr.closeMu.Unlock()
	tr.wg.Wait()
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
	traceID, spanID := traceIDsFromCtx(ctx)

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
				TraceID:  traceID,
				SpanID:   spanID,
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
				TraceID: traceID,
				SpanID:  spanID,
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
		if lastResp != nil && lastResp.Error == nil && len(lastResp.Choices) == 0 {
			log.Errorf("[trajectory] EMPTY-CHOICES response: model=%s n_msgs=%d duration_ms=%d -- provider served 200 with no choices",
				modelName, len(request.Messages), record.Metadata.DurationMs)
		}
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
		if err := f.Sync(); err != nil {
			log.Warnf("[TrajectoryRecorder] final sync failed: %v", err)
		}
		f.Close()
	}
	fileMu.Unlock()
}

// TrajectoryRecorderModelWrapper 包装另一个 model.Model 实例，但共用同一 TrajectoryRecorder
// 的 record 通道与写协程：用于包住子 agent 的模型，使其调用同样进入 RL 训练数据。
type TrajectoryRecorderModelWrapper struct {
	inner model.Model
	tr    *TrajectoryRecorder
}

// NewTrajectoryRecorderModelWrapper 构造共享同一录制器的模型包装器。
func NewTrajectoryRecorderModelWrapper(inner model.Model, tr *TrajectoryRecorder) *TrajectoryRecorderModelWrapper {
	return &TrajectoryRecorderModelWrapper{inner: inner, tr: tr}
}

// GenerateContent 委托内层模型，并经共享的录制路径落一条记录。
func (w *TrajectoryRecorderModelWrapper) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	return w.tr.recordGenerateContent(ctx, w.inner, request)
}

// GenerateContentIter 与 TrajectoryRecorder.GenerateContentIter 同构：暴露惰性的迭代入口，
// 使被子 agent 包装的内层 IterModel 能力不对流程隐藏。
func (w *TrajectoryRecorderModelWrapper) GenerateContentIter(ctx context.Context, request *model.Request) (model.Seq[*model.Response], error) {
	return func(yield func(*model.Response) bool) {
		ch, err := w.tr.recordGenerateContent(ctx, w.inner, request)
		if err != nil || ch == nil {
			return
		}
		for resp := range ch {
			if !yield(resp) {
				go func() {
					for range ch {
					}
				}()
				return
			}
		}
	}, nil
}

// Info 委托内层模型。
func (w *TrajectoryRecorderModelWrapper) Info() model.Info {
	return w.inner.Info()
}
