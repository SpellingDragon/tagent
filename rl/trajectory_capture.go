package rl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/modelutil"
)

// CaptureSchemaVersion and CaptureScopeSDKRequest pin the version and the
// observation boundary of the v2 record.
const (
	// CaptureSchemaVersion is written into every v2 record; the offline
	// converter refuses anything that is not exactly this value.
	CaptureSchemaVersion = 2
	// CaptureScopeSDKRequest names the only boundary this layer observes: the
	// model.Model SDK call. It is deliberately not called "wire".
	CaptureScopeSDKRequest = "sdk_request"
)

// DefaultCaptureMaxRecordBytes and its siblings are construction-time resource
// bounds: read once at construction, never hot-reloaded.
const (
	// DefaultCaptureMaxRecordBytes bounds ONE serialised record.
	DefaultCaptureMaxRecordBytes int64 = 8 << 20
	// DefaultCaptureMaxPendingBytes bounds the capture-owned total: in-flight
	// request copies + accumulated response fragments + queued records + the
	// serialisation buffer, across all concurrent calls.
	DefaultCaptureMaxPendingBytes int64 = 64 << 20
	// DefaultCaptureMaxRunBytes bounds how much one capture run writes to disk.
	DefaultCaptureMaxRunBytes int64 = 512 << 20
	// MaxCaptureOpenFiles is the hard ceiling on simultaneously open capture
	// files; evicting a handle never deletes or rotates the file.
	MaxCaptureOpenFiles = 16
	// captureQueueBufferSize keeps the v1 queue depth (S3: "队列继续 256").
	captureQueueBufferSize = 256
	// captureScopeCapacity bounds one attempt's association object (S2).
	captureScopeCapacity = 4096

	captureDirPerm  os.FileMode = 0o700
	captureFilePerm os.FileMode = 0o600
)

// BindingBound, BindingUnbound and BindingAmbiguous are the whole binding
// vocabulary of a captured call; there is no fourth "guessed" value.
const (
	BindingBound     = "bound"
	BindingUnbound   = "unbound"
	BindingAmbiguous = "ambiguous"
)

// TerminalDone and its siblings are the states a capture can honestly name for
// a response stream.
const (
	// TerminalDone: a response with Done and a finish reason was received.
	TerminalDone = "done"
	// TerminalError: the stream ended on a Response.Error object.
	TerminalError = "response_error"
	// TerminalCancelled: the call ctx was cancelled before a terminal response.
	TerminalCancelled = "cancelled"
	// TerminalClosedWithoutTerminal: the stream closed with only partials; the
	// deltas are NOT concatenated into a fabricated answer.
	TerminalClosedWithoutTerminal = "closed_without_terminal"
	// TerminalCallError: GenerateContent itself returned an error.
	TerminalCallError = "call_error"
	// TerminalNilChannel: the legal-but-anomalous (nil channel, nil error).
	TerminalNilChannel = "nil_channel"
)

// CaptureStatusComplete is reserved for a quiesced, synchronised, loss-free
// run; an absent or failed manifest is "unknown".
const (
	CaptureStatusComplete = "complete"
	CaptureStatusPartial  = "partial"
	CaptureStatusUnknown  = "unknown"
)

// MissingOwnerCaptureNamespace and its siblings are the record's way of saying
// "this is absent, and here is why" instead of leaving a consumer to infer it.
const (
	MissingOwnerCaptureNamespace = "owner.capture_namespace"
	MissingOwnerRootSessionID    = "owner.root_session_id"
	MissingOwnerAgentName        = "owner.agent_name"
	MissingOwnerInvocationID     = "owner.invocation_id"
	MissingOwnerPurpose          = "owner.purpose"
	MissingBindingNoInvocation   = "binding.no_invocation_evidence"
	MissingBindingNoResponseID   = "binding.no_response_id"
	MissingBindingCapacity       = "binding.unbound_capacity"
	MissingBindingConflict       = "binding.conflicting_response_id"
	MissingBindingReleased       = "binding.scope_released"
	MissingRequestDigest         = "request.digest_failed"
	MissingRequestSnapshot       = "request.snapshot_dropped"
	MissingResponseTerminal      = "response.no_terminal"
	MissingRecordTruncated       = "record.truncated_max_bytes"
	// MissingRequestStubbed marks a record whose request side was emptied after
	// even the response-stubbed serialization exceeded the per-record cap.
	MissingRequestStubbed   = "record.request_stubbed_after_oversize"
	MissingPendingExhausted = "record.pending_bytes_exhausted"
)

// ErrCaptureRequiresDump and its siblings surface at construction or flush time.
var (
	// ErrCaptureRequiresDump reports the precondition of the v2 layer: it only
	// exists on top of a recorder that already writes trajectories.
	ErrCaptureRequiresDump = errors.New("rl: trajectory_capture.enabled requires trajectory_dump=true")
	// ErrCaptureNegativeLimit rejects a nonsensical bound instead of silently
	// turning it into "unlimited".
	ErrCaptureNegativeLimit = errors.New("rl: trajectory_capture limit must not be negative")
	// ErrCaptureNotEnabled is returned when a consumer asks a recorder that has
	// no capture layer for a manifest.
	ErrCaptureNotEnabled = errors.New("rl: trajectory_capture is not enabled")
	// ErrCaptureClosed reports that the run is finished, so no new manifest can
	// be produced; the sealed one is replayed instead.
	ErrCaptureClosed = errors.New("rl: capture run is closed")
)

// CaptureConfig configures the v2 layer. Every field is construction-time.
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
type CaptureConfig struct {
	// Enabled defaults to false: nothing is captured and nothing is written
	// beyond the v1 recorder.
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// MaxRecordBytes bounds one serialised record. 0 ⇒ default.
	MaxRecordBytes int64 `json:"max_record_bytes,omitempty" yaml:"max_record_bytes,omitempty"`
	// MaxPendingBytes bounds the capture-owned total across concurrent calls.
	MaxPendingBytes int64 `json:"max_pending_bytes,omitempty" yaml:"max_pending_bytes,omitempty"`
	// MaxRunBytes bounds how much this run writes to disk; hitting it stops new
	// data but never deletes or rotates history (manifest becomes partial).
	MaxRunBytes int64 `json:"max_run_bytes,omitempty" yaml:"max_run_bytes,omitempty"`
	// MaxOpenFiles bounds simultaneously open capture files. Values above
	// MaxCaptureOpenFiles are clamped, not honoured. 0 ⇒ default.
	MaxOpenFiles int `json:"max_open_files,omitempty" yaml:"max_open_files,omitempty"`
	// QueueSize is the capture queue depth. 0 ⇒ the v1 depth (256).
	QueueSize int `json:"queue_size,omitempty" yaml:"queue_size,omitempty"`
}

// Validate checks the config the way the composition root must: enabled
// capture without trajectory_dump is a contradiction, and a negative bound is
// never interpreted as "no limit".
func (c CaptureConfig) Validate(trajectoryDump bool) error {
	if err := c.validateLimits(); err != nil {
		return err
	}
	if c.Enabled && !trajectoryDump {
		return ErrCaptureRequiresDump
	}
	return nil
}

func (c CaptureConfig) validateLimits() error {
	limits := []struct {
		name string
		v    int64
	}{
		{"max_record_bytes", c.MaxRecordBytes},
		{"max_pending_bytes", c.MaxPendingBytes},
		{"max_run_bytes", c.MaxRunBytes},
		{"max_open_files", int64(c.MaxOpenFiles)},
		{"queue_size", int64(c.QueueSize)},
	}
	for _, l := range limits {
		if l.v < 0 {
			return fmt.Errorf("%w: %s = %d", ErrCaptureNegativeLimit, l.name, l.v)
		}
	}
	return nil
}

// normalize applies the documented defaults and clamps. It rejects negatives
// even when the layer is off, so a typo in a config file is never ignored.
func (c CaptureConfig) normalize() (CaptureConfig, error) {
	if err := c.validateLimits(); err != nil {
		return c, err
	}
	if c.MaxRecordBytes == 0 {
		c.MaxRecordBytes = DefaultCaptureMaxRecordBytes
	}
	if c.MaxPendingBytes == 0 {
		c.MaxPendingBytes = DefaultCaptureMaxPendingBytes
	}
	if c.MaxRunBytes == 0 {
		c.MaxRunBytes = DefaultCaptureMaxRunBytes
	}
	if c.MaxOpenFiles == 0 || c.MaxOpenFiles > MaxCaptureOpenFiles {
		c.MaxOpenFiles = MaxCaptureOpenFiles
	}
	if c.QueueSize == 0 {
		c.QueueSize = captureQueueBufferSize
	}
	return c, nil
}

// OwnerAttrs is the flat attribution surface a capture record can read for one
// call. Key spelling follows the event metadata keys the agent already stamps
// (agent_name / rollout_id / bundle_id / trigger_source / partition_id ...),
// plus the invocation/task identity used by the runner.
type OwnerAttrs map[string]string

// OwnerResolver maps the model-call ctx onto OwnerAttrs. It is injected by the
// composition root (which may read plugin.Attribution) so that rl stays a leaf
// and never imports plugin/agent/root.
type OwnerResolver func(ctx context.Context) OwnerAttrs

// CaptureOwner is the per-call attribution block. Values that could not be
// obtained stay empty and are named in missing_reasons; nothing here invents a
// turn or session system.
type CaptureOwner struct {
	CaptureNamespace   string   `json:"capture_namespace"`
	RootSessionID      string   `json:"root_session_id"`
	AgentName          string   `json:"agent_name"`
	PartitionID        string   `json:"partition_id"`
	SessionID          string   `json:"session_id"`
	UserID             string   `json:"user_id"`
	InvocationID       string   `json:"invocation_id"`
	ParentInvocationID string   `json:"parent_invocation_id"`
	TaskID             string   `json:"task_id"`
	AttemptID          string   `json:"attempt_id,omitempty"`
	GenerationID       string   `json:"generation_id,omitempty"`
	BundleID           string   `json:"bundle_id,omitempty"`
	TriggerSource      string   `json:"trigger_source,omitempty"`
	Purpose            string   `json:"purpose"`
	InputEventKeys     []string `json:"input_event_keys"`
}

// CaptureToolDeclaration is one frozen declaration from the S1 snapshot. The
// schema keys keep the framework spelling (inputSchema/outputSchema) because
// that is what the offline converter reads.
type CaptureToolDeclaration struct {
	RegistryKey  string       `json:"registry_key"`
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	InputSchema  *tool.Schema `json:"inputSchema,omitempty"`
	OutputSchema *tool.Schema `json:"outputSchema,omitempty"`
}

// CaptureRequest is the frozen SDK request. It keeps the v1 request field names
// and adds the independent declaration list.
type CaptureRequest struct {
	Messages         []model.Message          `json:"messages"`
	Model            string                   `json:"model"`
	GenerationConfig model.GenerationConfig   `json:"generation_config,omitempty"`
	Tools            []CaptureToolDeclaration `json:"tools,omitempty"`
}

// CaptureFragment is ONE received *model.Response, deep-copied, in receive
// order. Fragments are never merged or re-concatenated.
type CaptureFragment struct {
	Seq      int             `json:"seq"`
	Response *model.Response `json:"response"`
}

// CaptureTerminalStatus is the reduced description of how the stream ended.
type CaptureTerminalStatus struct {
	Kind         string `json:"kind"`
	FinishReason string `json:"finish_reason,omitempty"`
	ResponseID   string `json:"response_id,omitempty"`
	Fragments    int    `json:"fragments"`
	Truncated    bool   `json:"truncated,omitempty"`
	Error        string `json:"error,omitempty"`
}

// CaptureLLMCall is the v2 llm_call block. Response keeps the v1 shape so an
// old consumer can still read the terminal message, usage and finish reason.
type CaptureLLMCall struct {
	Request            CaptureRequest        `json:"request"`
	Response           LLMResponseRecord     `json:"response"`
	ResponseFragments  []CaptureFragment     `json:"response_fragments,omitempty"`
	TerminalStatus     CaptureTerminalStatus `json:"terminal_status"`
	ResponseIncomplete bool                  `json:"response_incomplete,omitempty"`
	// TraceID/SpanID remain optional observation labels; an absent trace never
	// affects call_id or binding.
	TraceID string `json:"trace_id,omitempty"`
	SpanID  string `json:"span_id,omitempty"`
}

// CaptureRecord is one JSONL line of the v2 format.
type CaptureRecord struct {
	// Timestamp and the rest of the v1 fields keep their v1 name and meaning.
	Timestamp  string             `json:"timestamp"`
	SessionID  string             `json:"session_id"`
	UserID     string             `json:"user_id"`
	BatchIndex int                `json:"batch_index"`
	LLMCall    CaptureLLMCall     `json:"llm_call"`
	Metadata   TrajectoryMetadata `json:"metadata"`

	// SchemaVersion starts the v2-only block of the record.
	SchemaVersion      int          `json:"schema_version"`
	RunID              string       `json:"run_id"`
	CallID             string       `json:"call_id"`
	CaptureScope       string       `json:"capture_scope"`
	Owner              CaptureOwner `json:"owner"`
	BindingStatus      string       `json:"binding_status"`
	MissingReasons     []string     `json:"missing_reasons"`
	RequestDigest      string       `json:"request_digest"`
	ResponseIncomplete bool         `json:"response_incomplete,omitempty"`
	// ResponseID is the SDK's own id for the terminal response, when there was
	// one. The offline reader takes it as the event key tying this call to a
	// committed event; it is never synthesised or guessed.
	ResponseID string `json:"response_id,omitempty"`
}

// CaptureStats is the loss ledger. It is read from atomic counters, never from
// the data queue, so it stays available when the queue is jammed (which is
// exactly when a consumer needs it). The JSON is flat because the offline
// reader looks up these keys directly on the manifest run entry; a nested
// "counts" object would read as zero and mark an unsealed run sealed.
type CaptureStats struct {
	Started          int64 `json:"started"`
	Enqueued         int64 `json:"enqueued"`
	Written          int64 `json:"written"`
	DroppedFull      int64 `json:"dropped_full"`
	DroppedClosed    int64 `json:"dropped_closed"`
	DroppedDiskLimit int64 `json:"dropped_disk_limit"`
	Oversized        int64 `json:"oversized"`
	PendingExhausted int64 `json:"pending_exhausted"`
	OversizedDropped int64 `json:"oversized_dropped"`
	SerializeFailed  int64 `json:"serialize_failed"`
	WriteFailed      int64 `json:"write_failed"`
	SyncFailed       int64 `json:"sync_failed"`

	UnboundNoEvidence   int64 `json:"unbound_no_evidence"`
	UnboundNoResponseID int64 `json:"unbound_no_response_id"`
	UnboundCapacity     int64 `json:"unbound_capacity"`
	UnboundReleased     int64 `json:"unbound_released"`
	Ambiguous           int64 `json:"ambiguous"`

	Inflight        int64 `json:"inflight"`
	Pending         int64 `json:"pending"`
	PendingBytes    int64 `json:"pending_bytes"`
	MaxPendingBytes int64 `json:"max_pending_bytes_observed"`
	RunBytes        int64 `json:"run_bytes"`
	OpenFiles       int64 `json:"open_files"`
}

// Quiesced reports a loss-free, fully drained ledger. CaptureManifest.Complete
// adds the synchronisation and disk-limit conditions on top of it.
func (s CaptureStats) Quiesced() bool {
	return s.Inflight == 0 && s.Pending == 0 &&
		s.DroppedFull == 0 && s.DroppedClosed == 0 && s.DroppedDiskLimit == 0 &&
		s.Oversized == 0 && s.PendingExhausted == 0 && s.OversizedDropped == 0 &&
		s.SerializeFailed == 0 && s.WriteFailed == 0 && s.SyncFailed == 0
}

// Complete is the loss/queue view used by callers that only have Stats.
func (s CaptureStats) Complete() bool { return s.Quiesced() }

// CaptureFileDigest describes one capture file as this run wrote it. SHA256
// covers the bytes this run appended, so a consumer can re-verify the tail of
// the file against the manifest.
type CaptureFileDigest struct {
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
	RunBytes  int64  `json:"run_bytes"`
	Records   int64  `json:"records"`
	SHA256    string `json:"sha256"`
	CutoffSeq int64  `json:"cutoff_seq"`
}

// CaptureManifest is what FlushAndWait returns after the writer confirmed the
// fsync. It embeds the ledger so every field the offline reader needs is flat.
type CaptureManifest struct {
	CaptureStats

	RunID        string              `json:"run_id"`
	CutoffSeq    int64               `json:"cutoff_seq"`
	Status       string              `json:"status"`
	Synchronized bool                `json:"synchronized"`
	Sealed       bool                `json:"sealed"`
	Files        []CaptureFileDigest `json:"files"`
	// Complete is the manifest-level verdict: quiesced AND synchronised AND not
	// stopped by the run budget AND produced from a real writer confirmation.
	Complete bool `json:"complete"`
}

type captureScopeCtxKey struct{}

// LinkResult explains why a link was or was not stored.
type LinkResult int

const (
	// LinkStored: newly stored, or the same call_id was already there.
	LinkStored LinkResult = iota
	// LinkConflict: an active mapping for this key points at a different call.
	LinkConflict
	// LinkCapacity: the scope is full; no active mapping was evicted.
	LinkCapacity
	// LinkReleased: the scope was released before this arrival.
	LinkReleased
)

// CaptureScope is the bounded, per-runner-attempt association object. It exists
// only while capture is enabled, holds at most captureScopeCapacity entries,
// and is released by its owner when the real producer stopped and the event
// callbacks drained — never at the first assistant message or channel close.
type CaptureScope struct {
	// InvocationID is the call this scope belongs to.
	InvocationID string
	// Owner carries the attribution the installer already resolved (agent,
	// session, task, input keys ...). Capture never re-derives it.
	Owner OwnerAttrs

	mu       sync.Mutex
	links    map[string]string
	overflow int64
	released bool
}

// NewCaptureScope returns an empty bounded scope for one invocation.
func NewCaptureScope(invocationID string) *CaptureScope {
	return &CaptureScope{
		InvocationID: invocationID,
		links:        make(map[string]string, 8),
	}
}

// WithCaptureScope installs the association object on ctx for one attempt.
func WithCaptureScope(ctx context.Context, s *CaptureScope) context.Context {
	if ctx == nil || s == nil {
		return ctx
	}
	return context.WithValue(ctx, captureScopeCtxKey{}, s)
}

// CaptureScopeFrom returns the scope installed on ctx, if any.
func CaptureScopeFrom(ctx context.Context) (*CaptureScope, bool) {
	if ctx == nil {
		return nil, false
	}
	s, ok := ctx.Value(captureScopeCtxKey{}).(*CaptureScope)
	return s, ok && s != nil
}

// Link records that key (an exact "resp:<id>" or "toolcall:<id>" identity) is
// owned by callID. It never overwrites a live mapping: a second claim on the
// same key reports LinkConflict so the consumer sees ambiguous, not a guess.
func (s *CaptureScope) Link(key, callID string) (string, LinkResult) {
	if s == nil || key == "" || callID == "" {
		return "", LinkCapacity
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.released {
		s.overflow++
		return "", LinkReleased
	}
	if s.links == nil {
		s.links = make(map[string]string, 8)
	}
	if prev, ok := s.links[key]; ok {
		if prev == callID {
			return prev, LinkStored
		}
		s.overflow++
		return prev, LinkConflict
	}
	if len(s.links) >= captureScopeCapacity {
		s.overflow++
		return "", LinkCapacity
	}
	s.links[key] = callID
	return "", LinkStored
}

// ResponseKeyPrefix and ToolCallKeyPrefix namespace the association table: a
// consumer builds the same key from an identity it already holds, so a match is
// exact rather than a similarity.
const (
	ResponseKeyPrefix = "resp:"
	ToolCallKeyPrefix = "toolcall:"
)

// ResponseKey is the association key of one SDK response id.
func ResponseKey(responseID string) string { return ResponseKeyPrefix + responseID }

// ToolCallKey is the association key of one SDK tool-call id.
func ToolCallKey(toolCallID string) string { return ToolCallKeyPrefix + toolCallID }

// CallIDForResponse resolves a response identity to the call_id captured for it.
// No entry means false: the caller must report unbound, never take the nearest
// call. This is the read seam plugin/agent use without rl depending on them.
func CallIDForResponse(ctx context.Context, responseID string) (string, bool) {
	s, ok := CaptureScopeFrom(ctx)
	if !ok || responseID == "" {
		return "", false
	}
	return s.Lookup(ResponseKey(responseID))
}

// CallIDForToolCall is the same exact-key lookup for a tool-call identity.
func CallIDForToolCall(ctx context.Context, toolCallID string) (string, bool) {
	s, ok := CaptureScopeFrom(ctx)
	if !ok || toolCallID == "" {
		return "", false
	}
	return s.Lookup(ToolCallKey(toolCallID))
}

// Lookup resolves an exact identity key to a call_id.
func (s *CaptureScope) Lookup(key string) (string, bool) {
	if s == nil || key == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.links[key]
	return v, ok
}

// captureLayerCtxKey marks ctx with the call_id of the capture layer that
// already owns this exact model call.
type captureLayerCtxKey struct{}

// claimCaptureLayer annotates the ctx handed to the inner model with callID. The
// mark lives only for that call, so a layer of the same recorder nested inside
// the same chain forwards instead of recording twice, while a real sub-agent
// (its own ctx, its own request) is still recorded and a retry of the same
// request pointer is a fresh call rather than a swallowed one.
func claimCaptureLayer(ctx context.Context, callID string) context.Context {
	if ctx == nil || callID == "" {
		return ctx
	}
	return context.WithValue(ctx, captureLayerCtxKey{}, callID)
}

// captureLayerClaimed reports the call_id already recorded for this chain.
func captureLayerClaimed(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(captureLayerCtxKey{}).(string)
	return id, ok && id != ""
}

// Release drops the scope's memory once the owning attempt truly ended.
func (s *CaptureScope) Release() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links = nil
	s.released = true
}

// CaptureScopeStats is the scope's own occupancy view.
type CaptureScopeStats struct {
	Entries  int
	Overflow int64
	Released bool
}

// Stats reports scope occupancy without exposing its maps.
func (s *CaptureScope) Stats() CaptureScopeStats {
	if s == nil {
		return CaptureScopeStats{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return CaptureScopeStats{
		Entries:  len(s.links),
		Overflow: s.overflow,
		Released: s.released,
	}
}

// captureCanonicalRequest renders the deterministic request payload the digest
// is taken over. encoding/json sorts map keys and the S1 declaration list is
// already normalised by registry key, so the same request always hashes alike.
func captureCanonicalRequest(req CaptureRequest) ([]byte, error) {
	return json.Marshal(req)
}

// computeRequestDigest is the sha256 (hex) over the canonical request. It is
// exported-through-tests so a consumer can prove a record's digest re-derives
// from the record alone; an empty result means the payload could not be
// serialised at all.
func computeRequestDigest(req CaptureRequest) string {
	b, err := captureCanonicalRequest(req)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func estimateMessageBytes(m *model.Message) int64 {
	if m == nil {
		return 0
	}
	n := int64(len(m.Content)+len(m.ReasoningContent)+len(m.ReasoningSignature)+len(m.ToolID)+len(m.ToolName)+len(string(m.Role))) + 32
	for i := range m.ContentParts {
		p := &m.ContentParts[i]
		if p.Text != nil {
			n += int64(len(*p.Text))
		}
		n += int64(len(p.Type)) + 16
		if p.Image != nil {
			n += int64(len(p.Image.URL) + len(p.Image.Data) + len(p.Image.Detail) + len(p.Image.Format))
		}
		if p.Audio != nil {
			n += int64(len(p.Audio.URL) + len(p.Audio.Data) + len(p.Audio.Format))
		}
		if p.Video != nil {
			n += int64(len(p.Video.URL))
		}
		if p.File != nil {
			n += int64(len(p.File.URL) + len(p.File.Data) + len(p.File.Name) + len(p.File.MimeType))
		}
		if p.ContentRef != nil {
			n += int64(len(p.ContentRef.ArtifactRef)+len(p.ContentRef.ArtifactName)+len(p.ContentRef.MimeType)) + 16
		}
	}
	for i := range m.ToolCalls {
		tc := &m.ToolCalls[i]
		n += int64(len(tc.ID)+len(tc.Type)+len(tc.Function.Name)+len(tc.Function.Description)) + 32
		n += int64(len(tc.Function.Arguments))
		for k := range tc.ExtraFields {
			n += int64(len(k)) + 16
		}
	}
	return n
}

func estimateLiveRequestBytes(r *model.Request) int64 {
	if r == nil {
		return 0
	}
	var n int64
	for i := range r.Messages {
		n += estimateMessageBytes(&r.Messages[i])
	}
	for k := range r.Tools {
		n += int64(len(k)) + 64
	}
	return n
}

func estimateResponseBytes(r *model.Response) int64 {
	if r == nil {
		return 16
	}
	n := int64(len(r.ID)+len(r.Object)+len(r.Model)) + 48
	for i := range r.Choices {
		c := &r.Choices[i]
		n += estimateMessageBytes(&c.Message) + estimateMessageBytes(&c.Delta) + 32
		if c.Logprobs != nil {
			n += int64(len(c.Logprobs.Content)) * 64
		}
	}
	if r.Usage != nil {
		n += 128
	}
	if r.Error != nil {
		n += int64(len(r.Error.Message)+len(r.Error.Type)) + 32
		if r.Error.Code != nil {
			n += int64(len(*r.Error.Code))
		}
	}
	if r.SystemFingerprint != nil {
		n += int64(len(*r.SystemFingerprint)) + 16
	}
	return n
}

// cloneCapturedResponse deep-copies one received response. The framework's own
// Response.Clone aliases Message.ContentParts/ToolCalls, which is not enough
// here: a fragment must survive later framework mutation of the same object.
func cloneCapturedResponse(r *model.Response) *model.Response {
	if r == nil {
		return nil
	}
	c := *r
	if len(r.Choices) > 0 {
		c.Choices = make([]model.Choice, len(r.Choices))
		for i := range r.Choices {
			ch := r.Choices[i]
			ch.Message = cloneCapturedMessage(&ch.Message)
			ch.Delta = cloneCapturedMessage(&ch.Delta)
			ch.FinishReason = cloneStringPtr(ch.FinishReason)
			ch.Logprobs = cloneLogprobs(ch.Logprobs)
			c.Choices[i] = ch
		}
	}
	if r.Usage != nil {
		u := *r.Usage
		if r.Usage.TimingInfo != nil {
			ti := *r.Usage.TimingInfo
			u.TimingInfo = &ti
		}
		c.Usage = &u
	}
	if r.Error != nil {
		e := *r.Error
		c.Error = &e
	}
	c.SystemFingerprint = cloneStringPtr(r.SystemFingerprint)
	return &c
}

func cloneCapturedMessage(m *model.Message) model.Message {
	c := *m
	if len(m.ContentParts) > 0 {
		c.ContentParts = make([]model.ContentPart, len(m.ContentParts))
		for i := range m.ContentParts {
			p := m.ContentParts[i]
			p.Text = cloneStringPtr(p.Text)
			p.Image = clonePtrValue(p.Image)
			p.Audio = clonePtrValue(p.Audio)
			p.Video = clonePtrValue(p.Video)
			p.File = clonePtrValue(p.File)
			p.ContentRef = clonePtrValue(p.ContentRef)
			c.ContentParts[i] = p
		}
	}
	if len(m.ToolCalls) > 0 {
		c.ToolCalls = make([]model.ToolCall, len(m.ToolCalls))
		for i := range m.ToolCalls {
			tc := m.ToolCalls[i]
			tc.Function.Arguments = cloneBytesValue(m.ToolCalls[i].Function.Arguments)
			tc.Index = clonePtrValue(tc.Index)
			tc.ExtraFields = cloneAnyMap(tc.ExtraFields)
			c.ToolCalls[i] = tc
		}
	}
	return c
}

func cloneLogprobs(l *model.Logprobs) *model.Logprobs {
	if l == nil {
		return nil
	}
	out := &model.Logprobs{}
	if len(l.Content) > 0 {
		out.Content = make([]model.TokenLogprob, len(l.Content))
		for i := range l.Content {
			t := l.Content[i]
			t.Bytes = cloneIntSlice(t.Bytes)
			if len(t.TopLogprobs) > 0 {
				t.TopLogprobs = make([]model.TopLogprob, len(t.TopLogprobs))
				for j := range t.TopLogprobs {
					tp := t.TopLogprobs[j]
					tp.Bytes = cloneIntSlice(tp.Bytes)
					t.TopLogprobs[j] = tp
				}
			}
			out.Content[i] = t
		}
	}
	return out
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func clonePtrValue[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneBytesValue(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte(nil), b...)
}

func cloneIntSlice(s []int) []int {
	if s == nil {
		return nil
	}
	return append([]int(nil), s...)
}

func cloneAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// sanitizeCaptureEndpoint keeps the endpoint useful while stripping anything
// credential-shaped: no userinfo, no query, no fragment. v1 metadata is
// untouched; the v2 record never writes a bearer token or key.
func sanitizeCaptureEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		if i := strings.IndexAny(endpoint, "?#"); i >= 0 {
			return endpoint[:i]
		}
		return endpoint
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// captureFileHandle is the byte sink one capture file offers the writer.
type captureFileHandle interface {
	Write(p []byte) (int, error)
	Sync() error
	Close() error
}

// captureSink is the storage seam behind the writer. The production sink is the
// private-permission disk; tests substitute a stalled or failing one so a write
// or fsync fault is observed as a counted loss instead of an assumption.
type captureSink interface {
	MkdirAll(dir string) error
	Open(path string) (captureFileHandle, error)
}

type diskCaptureSink struct{}

func newDiskSink() captureSink { return diskCaptureSink{} }

func (diskCaptureSink) MkdirAll(dir string) error { return os.MkdirAll(dir, captureDirPerm) }

func (diskCaptureSink) Open(path string) (captureFileHandle, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, captureFilePerm)
}

// captureFile is one per-session file as this run sees it. `h` is nil once the
// handle was evicted by the open-file budget; the accounting (digest, bytes,
// records) survives eviction because the file itself is never deleted.
type captureFile struct {
	session   string
	path      string
	h         captureFileHandle
	hash      hash.Hash
	runBytes  int64
	records   int64
	cutoffSeq int64
}

type captureCounters struct {
	started          atomic.Int64
	enqueued         atomic.Int64
	written          atomic.Int64
	droppedFull      atomic.Int64
	droppedClosed    atomic.Int64
	droppedDiskLimit atomic.Int64
	oversized        atomic.Int64
	pendingExhausted atomic.Int64
	oversizedDropped atomic.Int64
	serializeFailed  atomic.Int64
	writeFailed      atomic.Int64
	syncFailed       atomic.Int64

	unboundNoEvidence   atomic.Int64
	unboundNoResponseID atomic.Int64
	unboundCapacity     atomic.Int64
	unboundReleased     atomic.Int64
	ambiguous           atomic.Int64
}

// captureEntry is one queue item: either a record to write, or a flush request
// that the writer must answer after everything queued ahead of it is durable.
type captureEntry struct {
	rec   *CaptureRecord
	bytes int64
	flush chan captureFlushReply
}

type captureFlushReply struct {
	files        []CaptureFileDigest
	cutoff       int64
	runBytes     int64
	syncFailed   bool
	synchronized bool
}

// capturePipeline owns the v2 recording layer: identity, byte bounds, the
// independent ledger, the writer and the seal manifest. A shared recorder
// shares exactly one pipeline (hence one writer and one set of counters), while
// identity still comes from each call's ctx.
type capturePipeline struct {
	cfg   CaptureConfig
	owner OwnerResolver
	sink  captureSink
	dir   string

	runPrefix string
	runID     string

	counters       captureCounters
	inflight       atomic.Int64
	pendingRecs    atomic.Int64
	pendingBytes   atomic.Int64
	maxPending     atomic.Int64
	callSeq        atomic.Int64
	seqWritten     atomic.Int64
	runBytes       atomic.Int64
	openHandles    atomic.Int32
	maxOpenHandles atomic.Int32
	diskLimited    atomic.Bool

	queue      chan captureEntry
	writerDone chan struct{}
	// sendMu is an RWMutex on purpose: senders take a read lock concurrently,
	// close() takes the write lock, so "close(queue)" can never race with a send
	// (which would panic).
	sendMu    sync.RWMutex
	closeOnce sync.Once
	isClosed  atomic.Bool

	mu           sync.Mutex
	fileSnapshot []CaptureFileDigest
	finalMani    *CaptureManifest
	sealCount    atomic.Int64
}

func newCapturePipeline(cfg CaptureConfig, dir string, owner OwnerResolver, sink captureSink) *capturePipeline {
	if sink == nil {
		sink = newDiskSink()
	}
	prefix := randomRunPrefix()
	c := &capturePipeline{
		cfg:        cfg,
		owner:      owner,
		sink:       sink,
		dir:        dir,
		runPrefix:  prefix,
		runID:      fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102T150405Z"), prefix),
		queue:      make(chan captureEntry, cfg.QueueSize),
		writerDone: make(chan struct{}),
	}
	return c
}

func randomRunPrefix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%012d", time.Now().UnixNano())))[:12]
	}
	return hex.EncodeToString(b[:])
}

// nextCallID is run-prefixed and monotonically increasing; it never depends on,
// and never collides because of, an optional trace.
func (c *capturePipeline) nextCallID() string {
	return fmt.Sprintf("%s-%06d", c.runPrefix, c.callSeq.Add(1))
}

func (c *capturePipeline) start() {
	go c.writeLoop()
}

func (c *capturePipeline) acquireBytes(n int64) bool {
	if n <= 0 {
		return true
	}
	for {
		cur := c.pendingBytes.Load()
		if cur+n > c.cfg.MaxPendingBytes {
			return false
		}
		if c.pendingBytes.CompareAndSwap(cur, cur+n) {
			for {
				seen := c.maxPending.Load()
				if seen >= cur+n || c.maxPending.CompareAndSwap(seen, cur+n) {
					break
				}
			}
			return true
		}
	}
}

func (c *capturePipeline) releaseBytes(n int64) {
	if n <= 0 {
		return
	}
	c.pendingBytes.Add(-n)
}

// resizeCharge moves a held allocation to a new size. ok=false means the larger
// size does not fit and the caller keeps (and later releases) what it holds.
func (c *capturePipeline) resizeCharge(held, want int64) (int64, bool) {
	switch {
	case want == held:
		return held, true
	case want < held:
		c.releaseBytes(held - want)
		return want, true
	default:
		if c.acquireBytes(want - held) {
			return want, true
		}
		return held, false
	}
}

func (c *capturePipeline) submit(rec *CaptureRecord, bytes int64) {
	c.sendMu.RLock()
	defer c.sendMu.RUnlock()
	if c.isClosed.Load() {
		c.counters.droppedClosed.Add(1)
		c.releaseBytes(bytes)
		log.Warnf("[trajectory-capture] DROPPED record after close: run=%s call=%s session=%s", c.runID, rec.CallID, rec.SessionID)
		return
	}
	select {
	case c.queue <- captureEntry{rec: rec, bytes: bytes}:
		c.counters.enqueued.Add(1)
		c.pendingRecs.Add(1)
	default:
		c.counters.droppedFull.Add(1)
		c.releaseBytes(bytes)
		log.Warnf("[trajectory-capture] queue full, dropping record: run=%s call=%s session=%s batch=%d bytes=%d",
			c.runID, rec.CallID, rec.SessionID, rec.BatchIndex, bytes)
	}
}

func (c *capturePipeline) writeLoop() {
	defer close(c.writerDone)

	files := make(map[string]*captureFile)
	order := make([]string, 0, c.cfg.MaxOpenFiles+1)

	for entry := range c.queue {
		if entry.flush != nil {
			entry.flush <- c.handleFlush(files, &order)
			continue
		}
		c.writeEntry(files, &order, entry)
	}
	c.finalize(files, &order)
}

// writeEntry serialises one record under the per-record byte cap. The cap is
// per SERIALIZED record, so a request-side payload that alone exceeds it
// survives the response stub: the record re-checks, stubs the request too,
// and only then decides whether it is unwritable.
func (c *capturePipeline) writeEntry(files map[string]*captureFile, order *[]string, entry captureEntry) {
	rec := entry.rec
	defer func() {
		c.pendingRecs.Add(-1)
		c.releaseBytes(entry.bytes)
	}()

	data, err := json.Marshal(rec)
	if err != nil {
		c.counters.serializeFailed.Add(1)
		log.Warnf("[trajectory-capture] record could not be serialised (dropped): run=%s call=%s err=%v", c.runID, rec.CallID, err)
		return
	}
	if int64(len(data))+1 > c.cfg.MaxRecordBytes {
		c.counters.oversized.Add(1)
		rec.LLMCall.ResponseFragments = nil
		rec.LLMCall.Response = LLMResponseRecord{}
		rec.LLMCall.TerminalStatus.Truncated = true
		rec.ResponseIncomplete = true
		rec.LLMCall.ResponseIncomplete = true
		addMissing(&rec.MissingReasons, MissingRecordTruncated)
		if data, err = json.Marshal(rec); err != nil {
			c.counters.serializeFailed.Add(1)
			return
		}
		if int64(len(data))+1 > c.cfg.MaxRecordBytes {
			rec.LLMCall.Request = CaptureRequest{Model: rec.LLMCall.Request.Model}
			rec.RequestDigest = ""
			addMissing(&rec.MissingReasons, MissingRequestStubbed)
			if data, err = json.Marshal(rec); err != nil {
				c.counters.serializeFailed.Add(1)
				return
			}
			if int64(len(data))+1 > c.cfg.MaxRecordBytes {
				c.counters.oversizedDropped.Add(1)
				log.Warnf("[trajectory-capture] record still above cap after request stub (dropped): run=%s call=%s bytes=%d cap=%d",
					c.runID, rec.CallID, len(data)+1, c.cfg.MaxRecordBytes)
				return
			}
		}
	}
	line := append(data, '\n')

	if !c.acquireBytes(int64(len(line))) {
		c.counters.pendingExhausted.Add(1)
		addMissing(&rec.MissingReasons, MissingPendingExhausted)
		return
	}
	defer c.releaseBytes(int64(len(line)))

	n := int64(len(line))
	if c.diskLimited.Load() || c.runBytes.Load()+n > c.cfg.MaxRunBytes {
		if !c.diskLimited.Swap(true) {
			log.Warnf("[trajectory-capture] run budget reached: run=%s bytes=%d cap=%d — accepting no further data; history is kept and the manifest reports partial",
				c.runID, c.runBytes.Load(), c.cfg.MaxRunBytes)
		}
		c.counters.droppedDiskLimit.Add(1)
		return
	}

	f, err := c.fileFor(files, order, rec.SessionID)
	if err != nil {
		c.counters.writeFailed.Add(1)
		log.Warnf("[trajectory-capture] open failed (record dropped): run=%s session=%s err=%v", c.runID, rec.SessionID, err)
		return
	}
	if _, err := f.h.Write(line); err != nil {
		c.counters.writeFailed.Add(1)
		log.Warnf("[trajectory-capture] write failed (record dropped): run=%s session=%s err=%v", c.runID, rec.SessionID, err)
		return
	}
	f.runBytes += n
	f.records++
	_, _ = f.hash.Write(line)
	f.cutoffSeq = c.seqWritten.Add(1)
	c.runBytes.Add(n)
	c.counters.written.Add(1)
}

// fileFor returns the file for a session, keeping at most MaxOpenFiles handles
// open at once. Eviction closes a handle and NEVER touches the file.
func (c *capturePipeline) fileFor(files map[string]*captureFile, order *[]string, session string) (*captureFile, error) {
	if session == "" {
		session = "default"
	}
	f, ok := files[session]
	if !ok {
		f = &captureFile{
			session: session,
			path:    filepath.Join(c.dir, session+".jsonl"),
			hash:    sha256.New(),
		}
		files[session] = f
	}
	if f.h != nil {
		touchOrder(order, session)
		return f, nil
	}
	for c.openHandles.Load() >= int32(c.cfg.MaxOpenFiles) {
		if !c.evictLRU(files, order) {
			break
		}
	}
	if err := c.sink.MkdirAll(c.dir); err != nil {
		return nil, err
	}
	h, err := c.sink.Open(f.path)
	if err != nil {
		return nil, err
	}
	f.h = h
	if n := c.openHandles.Add(1); n > c.maxOpenHandles.Load() {
		c.maxOpenHandles.Store(n)
	}
	touchOrder(order, session)
	return f, nil
}

// evictLRU closes the least recently used live handle. It returns false when
// nothing is open, so the caller cannot spin.
func (c *capturePipeline) evictLRU(files map[string]*captureFile, order *[]string) bool {
	for i := 0; i < len(*order); i++ {
		sid := (*order)[i]
		f, ok := files[sid]
		if !ok {
			*order = append((*order)[:i], (*order)[i+1:]...)
			continue
		}
		if f.h == nil {
			continue
		}
		if err := f.h.Sync(); err != nil {
			c.counters.syncFailed.Add(1)
		}
		if err := f.h.Close(); err != nil {
			log.Warnf("[trajectory-capture] closing evicted handle failed: %v", err)
		}
		f.h = nil
		c.openHandles.Add(-1)
		*order = append((*order)[:i], (*order)[i+1:]...)
		return true
	}
	return false
}

func touchOrder(order *[]string, session string) {
	for i := 0; i < len(*order); i++ {
		if (*order)[i] == session {
			*order = append((*order)[:i], (*order)[i+1:]...)
			break
		}
	}
	*order = append(*order, session)
}

func (c *capturePipeline) syncFiles(files map[string]*captureFile) bool {
	failed := false
	for _, f := range files {
		if f.h == nil {
			continue
		}
		if err := f.h.Sync(); err != nil {
			c.counters.syncFailed.Add(1)
			failed = true
			log.Warnf("[trajectory-capture] flush sync failed: run=%s session=%s err=%v", c.runID, f.session, err)
		}
	}
	return !failed
}

func snapshotFiles(files map[string]*captureFile) []CaptureFileDigest {
	out := make([]CaptureFileDigest, 0, len(files))
	for _, f := range files {
		out = append(out, CaptureFileDigest{
			SessionID: f.session,
			Path:      f.path,
			RunBytes:  f.runBytes,
			Records:   f.records,
			SHA256:    hex.EncodeToString(f.hash.Sum(nil)),
			CutoffSeq: f.cutoffSeq,
		})
	}
	sortDigests(out)
	return out
}

func sortDigests(d []CaptureFileDigest) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j-1].SessionID > d[j].SessionID; j-- {
			d[j-1], d[j] = d[j], d[j-1]
		}
	}
}

func (c *capturePipeline) handleFlush(files map[string]*captureFile, order *[]string) captureFlushReply {
	reply := captureFlushReply{
		cutoff:   c.seqWritten.Load(),
		runBytes: c.runBytes.Load(),
	}
	reply.synchronized = c.syncFiles(files)
	reply.syncFailed = !reply.synchronized
	reply.files = snapshotFiles(files)
	return reply
}

// finalize runs when the queue is closed and drained: it is the single seal
// point for the run.
func (c *capturePipeline) finalize(files map[string]*captureFile, order *[]string) {
	synchronized := c.syncFiles(files)
	snapshot := snapshotFiles(files)
	for _, f := range files {
		if f.h == nil {
			continue
		}
		if err := f.h.Close(); err != nil {
			log.Warnf("[trajectory-capture] close handle failed: %v", err)
		}
		f.h = nil
		c.openHandles.Add(-1)
	}

	c.mu.Lock()
	c.fileSnapshot = snapshot
	man := c.buildManifest(synchronized, snapshot, c.seqWritten.Load(), true)
	c.finalMani = &man
	c.mu.Unlock()
	c.sealCount.Add(1)
}

func (c *capturePipeline) stats() CaptureStats {
	return CaptureStats{
		Started:             c.counters.started.Load(),
		Enqueued:            c.counters.enqueued.Load(),
		Written:             c.counters.written.Load(),
		DroppedFull:         c.counters.droppedFull.Load(),
		DroppedClosed:       c.counters.droppedClosed.Load(),
		DroppedDiskLimit:    c.counters.droppedDiskLimit.Load(),
		Oversized:           c.counters.oversized.Load(),
		PendingExhausted:    c.counters.pendingExhausted.Load(),
		OversizedDropped:    c.counters.oversizedDropped.Load(),
		SerializeFailed:     c.counters.serializeFailed.Load(),
		WriteFailed:         c.counters.writeFailed.Load(),
		SyncFailed:          c.counters.syncFailed.Load(),
		UnboundNoEvidence:   c.counters.unboundNoEvidence.Load(),
		UnboundNoResponseID: c.counters.unboundNoResponseID.Load(),
		UnboundCapacity:     c.counters.unboundCapacity.Load(),
		UnboundReleased:     c.counters.unboundReleased.Load(),
		Ambiguous:           c.counters.ambiguous.Load(),
		Inflight:            c.inflight.Load(),
		Pending:             c.pendingRecs.Load(),
		PendingBytes:        c.pendingBytes.Load(),
		MaxPendingBytes:     c.maxPending.Load(),
		RunBytes:            c.runBytes.Load(),
		OpenFiles:           int64(c.openHandles.Load()),
	}
}

func (c *capturePipeline) buildManifest(synchronized bool, files []CaptureFileDigest, cutoff int64, sealed bool) CaptureManifest {
	st := c.stats()
	complete := synchronized && st.Quiesced() && !c.diskLimited.Load() && files != nil
	status := CaptureStatusPartial
	if complete {
		status = CaptureStatusComplete
	}
	return CaptureManifest{
		CaptureStats: st,
		RunID:        c.runID,
		CutoffSeq:    cutoff,
		Status:       status,
		Synchronized: synchronized,
		Sealed:       sealed,
		Files:        files,
		Complete:     complete,
	}
}

// unknownManifest is what a failed flush reports: never a claim of completeness.
func (c *capturePipeline) unknownManifest(err error) CaptureManifest {
	m := c.buildManifest(false, nil, c.seqWritten.Load(), false)
	m.Status = CaptureStatusUnknown
	m.Complete = false
	if err != nil {
		m.Files = nil
	}
	return m
}

// FlushAndWait waits for the writer to confirm every record accepted before
// this call, then returns the manifest. It is for the offline consumer: the
// model path never calls it and never waits for disk.
func (c *capturePipeline) FlushAndWait(ctx context.Context) (CaptureManifest, error) {
	if c.isClosed.Load() {
		if m := c.sealedManifest(); m != nil {
			return *m, nil
		}
		return c.unknownManifest(ErrCaptureClosed), ErrCaptureClosed
	}
	reply := make(chan captureFlushReply, 1)
	if err := c.sendFlush(ctx, captureEntry{flush: reply}); err != nil {
		if m := c.sealedManifest(); m != nil {
			return *m, nil
		}
		return c.unknownManifest(err), err
	}
	select {
	case rep := <-reply:
		return c.buildManifest(rep.synchronized, rep.files, rep.cutoff, false), nil
	case <-ctx.Done():
		return c.unknownManifest(ctx.Err()), ctx.Err()
	case <-c.writerDone:
		if m := c.sealedManifest(); m != nil {
			return *m, nil
		}
		return c.unknownManifest(ErrCaptureClosed), ErrCaptureClosed
	}
}

func (c *capturePipeline) sendFlush(ctx context.Context, entry captureEntry) error {
	c.sendMu.RLock()
	defer c.sendMu.RUnlock()
	if c.isClosed.Load() {
		return ErrCaptureClosed
	}
	select {
	case c.queue <- entry:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *capturePipeline) sealedManifest() *CaptureManifest {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finalMani == nil {
		return nil
	}
	m := *c.finalMani
	return &m
}

// close stops accepting, drains everything already accepted, and seals once.
func (c *capturePipeline) close() {
	c.sendMu.Lock()
	c.isClosed.Store(true)
	c.sendMu.Unlock()
	c.closeOnce.Do(func() {
		close(c.queue)
	})
	<-c.writerDone
}

// MarshalCaptureManifest renders the seal document the offline converter reads:
// {"runs": {run_id: {flat counters...}}, "complete":..., "synchronized":...}.
// The counters stay flat because a nested object would be read as zero and turn
// an unsealed run into a claimed-complete one.
func MarshalCaptureManifest(manifests []CaptureManifest) ([]byte, error) {
	runs := make(map[string]any, len(manifests))
	complete, synchronized := len(manifests) > 0, len(manifests) > 0
	for i := range manifests {
		m := manifests[i]
		b, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		var entry map[string]any
		if err := json.Unmarshal(b, &entry); err != nil {
			return nil, err
		}
		if m.RunID == "" {
			return nil, fmt.Errorf("rl: capture manifest entry has no run_id")
		}
		runs[m.RunID] = entry
		complete = complete && m.Complete
		synchronized = synchronized && m.Synchronized
	}
	return json.Marshal(map[string]any{
		"runs":         runs,
		"complete":     complete,
		"synchronized": synchronized,
	})
}

type recorderOptions struct {
	capture *CaptureConfig
	owner   OwnerResolver
	sink    captureSink
}

// RecorderOption configures optional layers on a TrajectoryRecorder at
// construction time. Nothing here is hot-reloadable.
type RecorderOption func(*recorderOptions)

// WithCapture installs the v2 capture layer. cfg.Enabled=false leaves the
// recorder byte-for-byte on the v1 path. The trajectory_dump precondition is
// structural here (this recorder only exists when dumping is on) and is checked
// by config.Validate/Validate at the composition root via CaptureConfig.Validate.
func WithCapture(cfg CaptureConfig) RecorderOption {
	c := cfg
	return func(o *recorderOptions) { o.capture = &c }
}

// WithCaptureOwnerResolver injects the ctx→attribution read seam. The
// composition root wires it to its own attribution carrier (e.g.
// plugin.AttributionFrom) so rl never imports plugin.
func WithCaptureOwnerResolver(r OwnerResolver) RecorderOption {
	return func(o *recorderOptions) { o.owner = r }
}

// withCaptureSink is the test-only storage seam (stalled / failing disk).
func withCaptureSink(s captureSink) RecorderOption {
	return func(o *recorderOptions) { o.sink = s }
}

// NewTrajectoryRecorderWithOptions builds a recorder and installs optional
// layers. Without WithCapture (or with Enabled=false) the result is the plain v1
// recorder: same queue, same writer, same bytes on disk. The capture layer is
// constructed and started before the first call, never lazily, so a config that
// cannot be honoured fails at construction instead of degrading silently later.
func NewTrajectoryRecorderWithOptions(inner model.Model, trajectoryDir, modelEndpoint string, opts ...RecorderOption) (*TrajectoryRecorder, error) {
	var o recorderOptions
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	enabled := o.capture != nil && o.capture.Enabled

	dirPerm := os.FileMode(0o755)
	if enabled {
		dirPerm = captureDirPerm
	}
	if err := os.MkdirAll(trajectoryDir, dirPerm); err != nil {
		return nil, err
	}

	tr := &TrajectoryRecorder{
		inner:    inner,
		dir:      trajectoryDir,
		endpoint: modelEndpoint,
		recordCh: make(chan *TrajectoryRecord, channelBufferSize),
	}

	var cfg CaptureConfig
	if enabled {
		var err error
		if cfg, err = o.capture.normalize(); err != nil {
			return nil, err
		}
		tr.capture = newCapturePipeline(cfg, trajectoryDir, o.owner, o.sink)
		tr.capture.start()
	}

	logTrajectoryInit(trajectoryDir, modelEndpoint)
	if enabled {
		log.Infof("[trajectory-capture] enabled: run=%s dir=%s max_record_bytes=%d max_pending_bytes=%d max_run_bytes=%d max_open_files=%d queue=%d",
			tr.capture.runID, trajectoryDir, cfg.MaxRecordBytes, cfg.MaxPendingBytes,
			cfg.MaxRunBytes, cfg.MaxOpenFiles, cfg.QueueSize)
	}

	tr.wg.Add(1)
	go tr.writeLoop()

	return tr, nil
}

// CaptureEnabled reports whether the v2 layer is active.
func (tr *TrajectoryRecorder) CaptureEnabled() bool { return tr.capture != nil }

// CaptureRunID is the identity every v2 record and manifest shares.
func (tr *TrajectoryRecorder) CaptureRunID() string {
	if tr.capture == nil {
		return ""
	}
	return tr.capture.runID
}

// CaptureStats reads the ledger without touching the data queue.
func (tr *TrajectoryRecorder) CaptureStats() CaptureStats {
	if tr.capture == nil {
		return CaptureStats{}
	}
	return tr.capture.stats()
}

// FlushAndWait returns the writer-confirmed manifest for this run's capture.
func (tr *TrajectoryRecorder) FlushAndWait(ctx context.Context) (CaptureManifest, error) {
	if tr.capture == nil {
		return CaptureManifest{Status: CaptureStatusUnknown}, ErrCaptureNotEnabled
	}
	return tr.capture.FlushAndWait(ctx)
}

func addMissing(dst *[]string, reason string) {
	for _, r := range *dst {
		if r == reason {
			return
		}
	}
	*dst = append(*dst, reason)
}

func splitKeyList(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c *capturePipeline) resolveOwnerAttrs(ctx context.Context, scope *CaptureScope) OwnerAttrs {
	out := OwnerAttrs{}
	if c.owner != nil {
		for k, v := range c.owner(ctx) {
			if v != "" {
				out[k] = v
			}
		}
	}
	if scope != nil {
		for k, v := range scope.Owner {
			if v != "" {
				out[k] = v
			}
		}
		if scope.InvocationID != "" {
			out["invocation_id"] = scope.InvocationID
		}
	}
	return out
}

// buildOwner maps the attribution surface onto the owner block. Anything not
// obtainable stays empty and is named in missing_reasons; no turn or session
// system is invented to fill it.
func buildOwner(attrs OwnerAttrs, sessionID, userID string) (CaptureOwner, []string) {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v := attrs[k]; v != "" {
				return v
			}
		}
		return ""
	}
	o := CaptureOwner{
		CaptureNamespace:   get("capture_namespace", "namespace"),
		RootSessionID:      get("root_session_id"),
		AgentName:          get("agent_name", "agent"),
		PartitionID:        get("partition_id"),
		SessionID:          get("session_id", "rollout_id"),
		UserID:             get("user_id"),
		InvocationID:       get("invocation_id", "invocation"),
		ParentInvocationID: get("parent_invocation_id", "parent_invocation"),
		TaskID:             get("task_id"),
		AttemptID:          get("attempt_id", "attempt_token"),
		GenerationID:       get("generation_id"),
		BundleID:           get("bundle_id"),
		TriggerSource:      get("trigger_source"),
		Purpose:            get("purpose"),
		InputEventKeys:     splitKeyList(get("input_event_keys")),
	}
	if o.SessionID == "" {
		o.SessionID = sessionID
	}
	if o.UserID == "" {
		o.UserID = userID
	}
	var missing []string
	if o.CaptureNamespace == "" {
		addMissing(&missing, MissingOwnerCaptureNamespace)
	}
	if o.RootSessionID == "" {
		addMissing(&missing, MissingOwnerRootSessionID)
	}
	if o.AgentName == "" {
		addMissing(&missing, MissingOwnerAgentName)
	}
	if o.PartitionID == "" && o.CaptureNamespace == "" {
		addMissing(&missing, "owner.partition_id")
	}
	if o.InvocationID == "" {
		addMissing(&missing, MissingOwnerInvocationID)
	}
	if o.Purpose == "" {
		addMissing(&missing, MissingOwnerPurpose)
	}
	return o, missing
}

// captureEvidence is one call's identity evidence, collected while the response
// stream arrives. It is never reconstructed afterwards, so a binding can only
// rest on identities the SDK actually returned for this call.
type captureEvidence struct {
	ResponseIDs []string
	ToolCallIDs []string
	// SawStored: at least one key of this call is active in the scope.
	SawStored bool
	// SawConflict: a key of this call already belongs to a different call_id.
	SawConflict bool
	SawCapacity bool
	SawReleased bool
}

func (e *captureEvidence) hasIdentity() bool {
	return len(e.ResponseIDs) > 0 || len(e.ToolCallIDs) > 0
}

// responseIdentity pulls the stable identities one received response carries.
func responseIdentity(r *model.Response) (string, []string) {
	if r == nil {
		return "", nil
	}
	var calls []string
	for i := range r.Choices {
		calls = append(calls, toolCallIDsOfMessage(&r.Choices[i].Message)...)
		calls = append(calls, toolCallIDsOfMessage(&r.Choices[i].Delta)...)
	}
	return r.ID, calls
}

func toolCallIDsOfMessage(m *model.Message) []string {
	var out []string
	for i := range m.ToolCalls {
		if id := m.ToolCalls[i].ID; id != "" {
			out = append(out, id)
		}
	}
	return out
}

func identityKeys(responseID string, toolCallIDs []string) []string {
	keys := make([]string, 0, len(toolCallIDs)+1)
	if responseID != "" {
		keys = append(keys, ResponseKey(responseID))
	}
	for _, id := range toolCallIDs {
		if id != "" {
			keys = append(keys, ToolCallKey(id))
		}
	}
	return keys
}

// bindCall classifies one call from exact evidence only: this call's invocation
// identity, then the response / tool-call identities registered for it. Every
// "no" is named, and the nearest call is never taken as a substitute.
func (c *capturePipeline) bindCall(scope *CaptureScope, owner CaptureOwner, ev *captureEvidence, missing *[]string) string {
	if owner.InvocationID == "" {
		addMissing(missing, MissingBindingNoInvocation)
		c.counters.unboundNoEvidence.Add(1)
		return BindingUnbound
	}
	switch {
	case ev.SawConflict:
		addMissing(missing, MissingBindingConflict)
		c.counters.ambiguous.Add(1)
		return BindingAmbiguous
	case scope == nil:
		if ev.hasIdentity() {
			return BindingBound
		}
		addMissing(missing, MissingBindingNoResponseID)
		c.counters.unboundNoResponseID.Add(1)
		return BindingUnbound
	case ev.SawStored:
		return BindingBound
	case ev.SawCapacity:
		addMissing(missing, MissingBindingCapacity)
		c.counters.unboundCapacity.Add(1)
		return BindingUnbound
	case ev.SawReleased:
		addMissing(missing, MissingBindingReleased)
		c.counters.unboundReleased.Add(1)
		return BindingUnbound
	default:
		addMissing(missing, MissingBindingNoResponseID)
		c.counters.unboundNoResponseID.Add(1)
		return BindingUnbound
	}
}

func toCaptureRequest(snap modelutil.RequestSnapshot, modelName string) CaptureRequest {
	req := CaptureRequest{
		Messages:         snap.Messages,
		Model:            modelName,
		GenerationConfig: snap.GenerationConfig,
	}
	if len(snap.Tools) > 0 {
		req.Tools = make([]CaptureToolDeclaration, 0, len(snap.Tools))
		for _, t := range snap.Tools {
			req.Tools = append(req.Tools, CaptureToolDeclaration{
				RegistryKey:  t.RegistryKey,
				Name:         t.Name,
				Description:  t.Description,
				InputSchema:  t.InputSchema,
				OutputSchema: t.OutputSchema,
			})
		}
	}
	return req
}

func llmResponseFromClone(r *model.Response) LLMResponseRecord {
	out := LLMResponseRecord{}
	if r == nil {
		return out
	}
	out.Choices = r.Choices
	out.Usage = r.Usage
	if len(r.Choices) > 0 && r.Choices[0].FinishReason != nil {
		out.FinishReason = *r.Choices[0].FinishReason
	}
	if r.Error != nil {
		out.Error = r.Error.Message
	}
	return out
}

func isTerminalResponse(r *model.Response) bool {
	if r == nil {
		return false
	}
	if r.Error != nil {
		return true
	}
	for i := range r.Choices {
		if fr := r.Choices[i].FinishReason; fr != nil && *fr != "" {
			return true
		}
	}
	return r.Done && !r.IsPartial
}

// captureAccumulator owns the response copies of one in-flight call. Copies are
// made before the response is handed to the consumer and stop the moment the
// record bound is hit — an abandoned tail is never cached without a bound.
type captureAccumulator struct {
	c         *capturePipeline
	scope     *CaptureScope
	callID    string
	ev        captureEvidence
	frags     []CaptureFragment
	charge    int64
	received  int
	truncated bool
	last      *model.Response
	terminal  *model.Response
	errText   string
}

// observe handles one received response. The identity registration happens
// BEFORE the response is handed on, so a consumer acting on this very response
// can already resolve it to this call_id (D14-S2: 在转交响应前登记).
func (a *captureAccumulator) observe(r *model.Response) {
	a.register(r)
	a.add(r)
}

// register records this response's exact identities in the call's scope and
// keeps what the scope reported. Nothing is guessed when a link fails.
func (a *captureAccumulator) register(r *model.Response) {
	id, calls := responseIdentity(r)
	if r == nil {
		return
	}
	if id != "" {
		a.ev.ResponseIDs = append(a.ev.ResponseIDs, id)
	}
	a.ev.ToolCallIDs = append(a.ev.ToolCallIDs, calls...)
	if a.scope == nil {
		return
	}
	for _, key := range identityKeys(id, calls) {
		_, res := a.scope.Link(key, a.callID)
		switch res {
		case LinkStored:
			a.ev.SawStored = true
		case LinkConflict:
			a.ev.SawConflict = true
		case LinkCapacity:
			a.ev.SawCapacity = true
		case LinkReleased:
			a.ev.SawReleased = true
		}
	}
}

func (a *captureAccumulator) add(r *model.Response) {
	a.received++
	if r == nil {
		return
	}
	if a.truncated {
		return
	}
	size := estimateResponseBytes(r)
	if size > a.c.cfg.MaxRecordBytes {
		a.truncated = true
		a.c.counters.oversized.Add(1)
		return
	}
	if !a.c.acquireBytes(size) {
		a.truncated = true
		a.c.counters.oversized.Add(1)
		return
	}
	clone := cloneCapturedResponse(r)
	a.charge += size
	a.frags = append(a.frags, CaptureFragment{Seq: a.received - 1, Response: clone})
	a.last = clone
	if clone.Error != nil {
		a.errText = clone.Error.Message
	}
	if isTerminalResponse(clone) {
		a.terminal = clone
	}
}

// reduce names how the stream ended. A partial-only stream is reported as
// incomplete; the deltas are never concatenated into an answer.
func (a *captureAccumulator) reduce(ctx context.Context) (CaptureTerminalStatus, bool, LLMResponseRecord) {
	kind := TerminalClosedWithoutTerminal
	incomplete := true
	st := CaptureTerminalStatus{Fragments: len(a.frags), Truncated: a.truncated}
	switch {
	case a.terminal != nil && a.terminal.Error != nil:
		kind = TerminalError
	case a.terminal != nil && finishReasonOf(a.terminal) != "" && !a.truncated:
		kind = TerminalDone
		incomplete = false
	case ctx.Err() != nil:
		kind = TerminalCancelled
	}
	if a.terminal != nil {
		st.FinishReason = finishReasonOf(a.terminal)
		st.ResponseID = a.terminal.ID
	}
	if st.ResponseID == "" {
		st.ResponseID = lastNonEmptyID(a.frags)
	}
	st.Kind = kind
	st.Error = a.errText
	resp := llmResponseFromClone(a.last)
	if a.terminal != nil {
		resp = llmResponseFromClone(a.terminal)
	}
	if incomplete && resp.Error == "" && a.errText != "" {
		resp.Error = a.errText
	}
	return st, incomplete, resp
}

func finishReasonOf(r *model.Response) string {
	if r == nil {
		return ""
	}
	if len(r.Choices) > 0 && r.Choices[0].FinishReason != nil {
		return *r.Choices[0].FinishReason
	}
	return ""
}

func lastNonEmptyID(frags []CaptureFragment) string {
	for i := len(frags) - 1; i >= 0; i-- {
		if frags[i].Response != nil && frags[i].Response.ID != "" {
			return frags[i].Response.ID
		}
	}
	return ""
}

func (tr *TrajectoryRecorder) newCaptureRecord(c *capturePipeline, callID string, owner CaptureOwner, missing []string,
	sessionID, userID string, batchIdx int, endpoint, modelName, traceID, spanID string, start time.Time) *CaptureRecord {
	if missing == nil {
		missing = []string{}
	}
	return &CaptureRecord{
		Timestamp:      time.Now().Format(time.RFC3339Nano),
		SessionID:      sessionID,
		UserID:         userID,
		BatchIndex:     batchIdx,
		Metadata:       TrajectoryMetadata{DurationMs: time.Since(start).Milliseconds(), ModelEndpoint: sanitizeCaptureEndpoint(endpoint)},
		SchemaVersion:  CaptureSchemaVersion,
		RunID:          c.runID,
		CallID:         callID,
		CaptureScope:   CaptureScopeSDKRequest,
		Owner:          owner,
		BindingStatus:  BindingUnbound,
		MissingReasons: missing,
		LLMCall: CaptureLLMCall{
			Request:            CaptureRequest{Model: modelName},
			TraceID:            traceID,
			SpanID:             spanID,
			TerminalStatus:     CaptureTerminalStatus{Kind: TerminalClosedWithoutTerminal},
			ResponseIncomplete: true,
		},
	}
}

// captureGenerateContent is the single v2 record path. Both the channel entry
// and the iterator entry reach it, so neither entry point can produce a
// differently shaped record.
func (tr *TrajectoryRecorder) captureGenerateContent(ctx context.Context, inner model.Model, request *model.Request,
	userID, sessionID string, batchIdx int, endpoint, modelName, traceID, spanID string, start time.Time) (<-chan *model.Response, error) {

	c := tr.capture

	scope, _ := CaptureScopeFrom(ctx)
	if _, claimed := captureLayerClaimed(ctx); claimed {
		return inner.GenerateContent(ctx, request)
	}
	c.counters.started.Add(1)

	c.inflight.Add(1)
	finished := false
	finish := func(rec *CaptureRecord, charge int64) {
		if finished {
			return
		}
		finished = true
		c.submit(rec, charge)
		c.inflight.Add(-1)
	}

	callID := c.nextCallID()
	attrs := c.resolveOwnerAttrs(ctx, scope)
	owner, missing := buildOwner(attrs, sessionID, userID)

	rec := tr.newCaptureRecord(c, callID, owner, missing, sessionID, userID, batchIdx, endpoint, modelName, traceID, spanID, start)

	charge := int64(0)
	reqPayload := CaptureRequest{Model: modelName}
	est := estimateLiveRequestBytes(request)
	if c.acquireBytes(est) {
		charge = est
		snap := modelutil.NewRequestSnapshot(request.Messages, request.Tools,
			modelutil.WithGenerationConfig(request.GenerationConfig),
			modelutil.WithStructuredOutput(request.StructuredOutput))
		reqPayload = toCaptureRequest(snap, modelName)
		exact := charge
		canonical, derr := captureCanonicalRequest(reqPayload)
		if derr == nil {
			exact = int64(len(canonical))
			sum := sha256.Sum256(canonical)
			rec.RequestDigest = hex.EncodeToString(sum[:])
		} else {
			addMissing(&rec.MissingReasons, MissingRequestDigest)
		}
		adj, ok := c.resizeCharge(charge, exact)
		if !ok {
			c.releaseBytes(charge)
			charge = 0
			c.counters.oversized.Add(1)
			addMissing(&rec.MissingReasons, MissingRequestSnapshot)
			reqPayload = CaptureRequest{Model: modelName}
		} else {
			charge = adj
		}
	} else {
		c.counters.oversized.Add(1)
		addMissing(&rec.MissingReasons, MissingRequestSnapshot)
	}
	rec.LLMCall.Request = reqPayload

	respCh, err := inner.GenerateContent(claimCaptureLayer(ctx, callID), request)
	var nilChannel bool
	if err == nil && respCh == nil {
		nilChannel = true
	}
	if err != nil || nilChannel {
		errText := "inner model returned nil channel with nil error"
		kind := TerminalCallError
		if err != nil {
			errText = err.Error()
		} else {
			kind = TerminalNilChannel
		}
		rec.LLMCall.TerminalStatus = CaptureTerminalStatus{Kind: kind, Error: errText}
		rec.LLMCall.Response = LLMResponseRecord{Error: errText}
		rec.LLMCall.ResponseIncomplete = true
		rec.ResponseIncomplete = true
		addMissing(&rec.MissingReasons, MissingResponseTerminal)
		rec.BindingStatus = c.bindCall(scope, owner, &captureEvidence{}, &rec.MissingReasons)
		finish(rec, charge)
		return nil, err
	}

	acc := &captureAccumulator{c: c, scope: scope, callID: callID}
	wrappedCh := make(chan *model.Response, 64)
	tr.gcWg.Add(1)
	go func() {
		defer tr.gcWg.Done()
		defer close(wrappedCh)
		for resp := range respCh {
			acc.observe(resp)
			wrappedCh <- resp
		}

		term, incomplete, resp := acc.reduce(ctx)
		rec.LLMCall.TerminalStatus = term
		rec.LLMCall.Response = resp
		rec.LLMCall.ResponseFragments = acc.frags
		rec.LLMCall.ResponseIncomplete = incomplete
		rec.ResponseIncomplete = incomplete
		if incomplete {
			addMissing(&rec.MissingReasons, MissingResponseTerminal)
		}
		rec.Metadata.DurationMs = time.Since(start).Milliseconds()
		rec.ResponseID = term.ResponseID
		rec.BindingStatus = c.bindCall(scope, owner, &acc.ev, &rec.MissingReasons)
		finish(rec, charge+acc.charge)
	}()

	return wrappedCh, nil
}
