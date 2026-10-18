package rl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/model"
)

// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
//
// Capture is the request-boundary observation port through which one model
// request is remembered: it fixes the view a request actually runs on, admits the
// attempts observed for it under a bound, and seals what it holds with a manifest.
//
// The redline list (4 rules):
// MUST fix the model, the generation parameters and the visible prompt, skill and tool set before the request starts.
// MUST keep a header, cookie or URL credential out of every persisted record.
// MUST NOT let admission wait on the caller or cross its bound.
// MUST NOT report a capture as complete while a record is in flight or no writer confirmed the seal.
type Capture interface {
	// Snapshot fixes the model, the generation parameters and the visible prompt,
	// skill and tool set of req before the request starts, and returns the
	// reference a decision records. A view is never reconstructed from the options
	// of a finished call.
	Snapshot(ctx context.Context, req model.Request) (ModelViewRef, error)

	// Record admits one observed attempt and reports the admission bound that
	// refused it when it is not admitted.
	Record(ctx context.Context, attempt AttemptRecord) (CaptureAdmission, error)

	// Seal closes the capture and returns the manifest a real writer confirmed. A
	// capture that still holds records in flight, or that no writer sealed, comes
	// back with Partial set.
	Seal(ctx context.Context) (CaptureManifest, error)
}

// CaptureBound names the admission bound that refused a record.
type CaptureBound string

const (
	// BoundNone marks an admitted record: no bound refused it.
	BoundNone CaptureBound = ""
	// BoundRecordSize marks a record that exceeded the per-record byte bound.
	BoundRecordSize CaptureBound = "record_size"
	// BoundInFlight marks a record refused because the in-flight count bound was
	// reached.
	BoundInFlight CaptureBound = "in_flight"
	// BoundBytes marks a record refused because the run byte budget was reached.
	BoundBytes CaptureBound = "bytes"
	// BoundClosed marks a record refused because the capture was already closed.
	BoundClosed CaptureBound = "closed"
)

// CaptureAdmission states whether a capture admitted one record, and which bound
// refused it when it did not.
//
// An unadmitted record is a counted loss and not a silent one: the bound that
// refused it stays readable on the admission.
type CaptureAdmission struct {
	// Admitted reports whether the record was admitted.
	Admitted bool
	// Bound names the bound that refused the record; it is BoundNone for an
	// admitted record.
	Bound CaptureBound
	// Reason states why the bound refused the record.
	Reason string
}

// CaptureFileDigest is the seal entry of one file a capture wrote: the name it is
// accounted under, the digest of the bytes actually written and what those bytes
// hold.
//
// The entry is the unit a seal is rechecked at: a recheck that finds a differing
// file names that file rather than reporting a mismatch on the capture as a whole.
type CaptureFileDigest struct {
	// Name identifies the file within the capture.
	Name string
	// SHA256 is the content digest of the bytes actually written to the file.
	SHA256 string
	// Bytes is the count of bytes written to the file.
	Bytes int64
	// Records counts the records the file holds.
	Records int64
}

// WriterSyncResult is the writer's own report at seal time: whether the written
// bytes were synced to durable storage, and why not when they were not.
//
// The result is evidence of what reached storage rather than what was handed to
// the writer: a manifest whose writer reported no sync is partial, and a sync
// result is never inferred from record counts or file digests.
type WriterSyncResult struct {
	// Synced reports whether the writer confirmed the written bytes reached
	// durable storage.
	Synced bool
	// Reason states why the sync did not complete; it is empty for a completed
	// sync.
	Reason string
	// At is the time the writer reported the result.
	At time.Time
}

// CaptureManifest is the seal of one capture: what it holds file by file, whether
// a real writer confirmed and synced it, and what it lost.
//
// A capture without a writer seal is partial. A partial capture is never reported
// as complete, and the reason says what was still in flight. The seal is accounted
// per file: an aggregate hash alone is not a seal account, because a recheck must
// name the file that differs. A manifest whose writer reported no sync is partial
// as well — the seal covers what reached storage, never what was only written to.
type CaptureManifest struct {
	// CaptureID identifies the capture the manifest seals.
	CaptureID string
	// Sealed reports whether a real writer confirmed the seal after the records
	// were flushed.
	Sealed bool
	// Partial reports a capture that closed with records in flight, refused
	// records, write losses, without a writer seal or without a completed sync.
	Partial bool
	// Records counts the records the capture wrote.
	Records int64
	// Dropped counts the records the capture refused under an admission bound.
	Dropped int64
	// InFlight counts the records still unflushed when the capture closed.
	InFlight int64
	// Files lists the seal entry of every file the capture wrote, so the seal is
	// rechecked per file.
	Files []CaptureFileDigest
	// Sync is the sync result the writer itself reported at seal time.
	Sync WriterSyncResult
	// Reasons lists why the capture is partial.
	Reasons []string
}

// RedactedValue is what every credential carrier is projected to: a header
// name, a query key or a field name stays readable while the credential itself
// is never persisted.
//
// - Safe snapshot face: one snapshot is fixed from the request before the request starts, projected so no header, cookie or URL credential reaches a persisted record.
// - A finished call's options never rebuild a request snapshot, so the snapshot records what the request boundary carried and replays nothing.
const RedactedValue = "[redacted]"

// ErrRebuildRefused and the other named errors of the snapshot production face: each names the way a safe
// snapshot or its reference was refused, so a caller can route it by name.
var (
	// ErrRebuildRefused reports an attempt to rebuild a request snapshot by
	// replaying the options of a finished call. The snapshot is fixed before the
	// request starts and is never reconstructed afterwards.
	ErrRebuildRefused = errors.New("rl: a request snapshot is never rebuilt from the options of a finished call")
	// ErrSnapshotUnreadable reports a snapshot reference that cannot read its
	// content back: an empty reference is not a snapshot reference.
	ErrSnapshotUnreadable = errors.New("rl: the snapshot reference cannot read its content back")
	// ErrSnapshotMismatch reports snapshot content that does not recheck against
	// the digest of the reference it is read back under.
	ErrSnapshotMismatch = errors.New("rl: the snapshot content does not match its reference digest")
)

// SnapshotPolicy is the configurable safe projection one snapshot is taken
// under. The zero policy is the default projection: credential-named extra
// field keys and URL credentials are redacted and everything else keeps its
// bytes. A projection replaces values; it never edits the request itself.
type SnapshotPolicy struct {
	// ProjectExtraFields projects the provider extra fields before they are
	// persisted. It receives the JSON-normalized deep copy of the request's
	// extra fields and returns the projected map.
	ProjectExtraFields func(map[string]any) map[string]any
	// ProjectURL projects one URL carrier of a message content part before it is
	// persisted.
	ProjectURL func(string) string
}

// urlProjector reports the URL projection this policy applies.
func (p SnapshotPolicy) urlProjector() func(string) string {
	if p.ProjectURL != nil {
		return p.ProjectURL
	}
	return SanitizeURL
}

// extraProjector reports the extra field projection this policy applies.
func (p SnapshotPolicy) extraProjector() func(map[string]any) map[string]any {
	if p.ProjectExtraFields != nil {
		return p.ProjectExtraFields
	}
	return RedactExtraFields
}

// RequestSnapshot is the safe request boundary snapshot of one model request:
// the model view it was declared against and the request content it carried,
// projected so no header, cookie or URL credential is persisted.
//
// The snapshot is fixed before the request starts and holds the request as it
// stood then: the messages, tool schemas, multimodal parts, reasoning and
// actual parameters are kept verbatim while a credential carrier is projected,
// so the projection never rewrites business content. The headers are kept as
// their names with their values projected to RedactedValue, because a header
// value is a transport credential carrier rather than request content.
type RequestSnapshot struct {
	// Model names the model the request was declared against.
	Model string `json:"model,omitempty"`
	// ModelVersion is the version of that model; empty when it ran unversioned.
	ModelVersion string `json:"model_version,omitempty"`
	// PromptVersion is the version of the system prompt content the request was
	// assembled from.
	PromptVersion string `json:"prompt_version,omitempty"`
	// VisibleTools lists the tools visible to the request, each with the version
	// of its definition the request ran with.
	VisibleTools []VersionedName `json:"visible_tools,omitempty"`
	// VisibleSkills lists the skills visible to the request.
	VisibleSkills []VersionedName `json:"visible_skills,omitempty"`
	// Parameters is the generation configuration the request was declared with,
	// kept as the actual parameters rather than a digest of them.
	Parameters json.RawMessage `json:"parameters,omitempty"`
	// StructuredOutput is the structured output declaration of the request.
	StructuredOutput json.RawMessage `json:"structured_output,omitempty"`
	// Messages holds the conversation the request carried, projected at its URL
	// carriers and verbatim everywhere else.
	Messages []model.Message `json:"messages"`
	// ToolSchemas holds the declaration of every tool visible to the request,
	// in tool name order so the snapshot bytes are deterministic.
	ToolSchemas []json.RawMessage `json:"tool_schemas,omitempty"`
	// ExtraFields holds the projected provider extra fields of the request.
	ExtraFields map[string]any `json:"extra_fields,omitempty"`
	// Headers holds the header names the request carried with their values
	// projected to RedactedValue: a header value never reaches a persisted
	// record.
	Headers map[string]string `json:"headers,omitempty"`
}

// SnapshotRequest fixes the safe snapshot of req as it stands before the
// request starts, under the projection policy. The request is observed and
// never edited: the projection runs over deep copies, so the caller's request
// keeps every original value including its credentials.
func SnapshotRequest(view ModelViewRef, req model.Request, policy SnapshotPolicy) (RequestSnapshot, error) {
	messages, err := deepCopyMessages(req.Messages)
	if err != nil {
		return RequestSnapshot{}, err
	}
	projectURLs(messages, policy.urlProjector())
	extra, err := deepCopyExtraFields(req.ExtraFields)
	if err != nil {
		return RequestSnapshot{}, err
	}
	parameters, err := json.Marshal(req.GenerationConfig)
	if err != nil {
		return RequestSnapshot{}, err
	}
	schemas, err := toolSchemas(req)
	if err != nil {
		return RequestSnapshot{}, err
	}
	snapshot := RequestSnapshot{
		Model:         view.Model,
		ModelVersion:  view.ModelVersion,
		PromptVersion: view.PromptVersion,
		VisibleTools:  append([]VersionedName(nil), view.VisibleTools...),
		VisibleSkills: append([]VersionedName(nil), view.VisibleSkills...),
		Parameters:    parameters,
		Messages:      messages,
		ToolSchemas:   schemas,
		ExtraFields:   policy.extraProjector()(extra),
		Headers:       redactHeaders(req.Headers),
	}
	if req.StructuredOutput != nil {
		structured, err := json.Marshal(req.StructuredOutput)
		if err != nil {
			return RequestSnapshot{}, err
		}
		snapshot.StructuredOutput = structured
	}
	return snapshot, nil
}

// SnapshotFromOptions is the refused rebuild path: a request snapshot is fixed
// before the request starts and is never rebuilt by replaying the options of a
// finished call. The call is refused before any option runs and before any
// message is read, so the options of a finished call are never re-run here.
func SnapshotFromOptions(messages []model.Message, opts ...func(*model.Request)) (RequestSnapshot, error) {
	return RequestSnapshot{}, fmt.Errorf("%w: %d request options over %d messages would reconstruct a finished call", ErrRebuildRefused, len(opts), len(messages))
}

// SanitizeURL projects one URL carrier to its safe form: a URL that carries no
// credential keeps its bytes exactly as they stand, while a URL carrying a
// userinfo or a credential query value loses that credential to RedactedValue
// and keeps its readable structure. A URL that cannot be parsed is projected to
// empty rather than persisted as it stands.
func SanitizeURL(raw string) string {
	return sanitizeURL(raw, 0)
}

// sanitizeURL is SanitizeURL bounded by the nesting depth of credential URLs
// embedded in query values.
//
// - A query value that embeds a credential URL is projected in turn, re-escaped so the outer query keeps its own shape.
func sanitizeURL(raw string, depth int) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	dirty := false
	if parsed.User != nil {
		parsed.User = nil
		dirty = true
	}
	if parsed.RawQuery != "" {
		pairs := strings.Split(parsed.RawQuery, "&")
		for i, pair := range pairs {
			cut := strings.IndexByte(pair, '=')
			if cut < 0 {
				continue
			}
			key, value := pair[:cut], pair[cut+1:]
			decodedKey, err := url.QueryUnescape(key)
			if err != nil {
				continue
			}
			if credentialKey(decodedKey) {
				pairs[i] = key + "=" + RedactedValue
				dirty = true
				continue
			}
			if depth >= 2 {
				continue
			}
			decodedValue, err := url.QueryUnescape(value)
			if err != nil || !strings.Contains(decodedValue, "://") {
				continue
			}
			if projected := sanitizeURL(decodedValue, depth+1); projected != decodedValue {
				pairs[i] = key + "=" + url.QueryEscape(projected)
				dirty = true
			}
		}
		if dirty {
			parsed.RawQuery = strings.Join(pairs, "&")
		}
	}
	if !dirty {
		return raw
	}
	return parsed.String()
}

// RedactExtraFields is the default safe projection of the provider extra
// fields: a credential-named key keeps its name and loses its value to
// RedactedValue, a string value carrying URL credentials is projected through
// SanitizeURL, and everything else keeps its value as it stood.
func RedactExtraFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	redacted := make(map[string]any, len(fields))
	for key, value := range fields {
		if credentialKey(key) {
			redacted[key] = RedactedValue
			continue
		}
		redacted[key] = redactValue(value)
	}
	return redacted
}

// redactValue projects one non-keyed extra field value recursively.
func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return RedactExtraFields(typed)
	case []any:
		projected := make([]any, len(typed))
		for i, item := range typed {
			projected[i] = redactValue(item)
		}
		return projected
	case string:
		if strings.Contains(typed, "://") {
			if projected := SanitizeURL(typed); projected != typed {
				return projected
			}
		}
		return typed
	default:
		return typed
	}
}

// credentialKeys names the keys whose value is a credential: a key matching one
// of these never persists its value.
var credentialKeys = []string{
	"key", "api_key", "api-key", "apikey", "access_token", "token", "auth",
	"authorization", "secret", "password", "passwd", "credential", "signature",
	"sig", "private_key", "access_key", "cookie", "x-api-key", "x-amz-signature",
	"x-goog-api-key",
}

// credentialKey reports whether one key names a credential carrier.
func credentialKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, known := range credentialKeys {
		if normalized == known {
			return true
		}
	}
	return false
}

// redactHeaders projects every header value to RedactedValue and keeps the
// header names: a header value is a transport credential carrier and never
// reaches a persisted record, while the names stay readable.
func redactHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	redacted := make(map[string]string, len(headers))
	for name := range headers {
		redacted[name] = RedactedValue
	}
	return redacted
}

// deepCopyMessages returns the JSON deep copy of the request messages: the
// snapshot is detached from the request, so a later change to the request never
// moves the snapshot.
func deepCopyMessages(messages []model.Message) ([]model.Message, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return nil, err
	}
	var cloned []model.Message
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

// deepCopyExtraFields returns the JSON-normalized deep copy of the provider
// extra fields, detached from the request.
func deepCopyExtraFields(fields map[string]any) (map[string]any, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

// projectURLs projects every URL carrier of the message content parts in place.
// A text body, a tool call and a reasoning field are request content and are
// never projected here.
func projectURLs(messages []model.Message, project func(string) string) {
	for i := range messages {
		for j := range messages[i].ContentParts {
			part := &messages[i].ContentParts[j]
			switch {
			case part.Image != nil:
				part.Image.URL = project(part.Image.URL)
			case part.Audio != nil:
				part.Audio.URL = project(part.Audio.URL)
			case part.Video != nil:
				part.Video.URL = project(part.Video.URL)
			case part.File != nil:
				part.File.URL = project(part.File.URL)
			}
		}
	}
}

// toolSchemas reports the declaration of every tool the request carried, in
// tool name order so the snapshot bytes are deterministic across map iteration.
func toolSchemas(req model.Request) ([]json.RawMessage, error) {
	names := make([]string, 0, len(req.Tools))
	for name := range req.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	var schemas []json.RawMessage
	for _, name := range names {
		declared := req.Tools[name]
		if declared == nil {
			continue
		}
		decl := declared.Declaration()
		if decl == nil {
			continue
		}
		raw, err := json.Marshal(decl)
		if err != nil {
			return nil, err
		}
		schemas = append(schemas, raw)
	}
	return schemas, nil
}

// MarshalSnapshot encodes one safe snapshot for persistence.
func MarshalSnapshot(snapshot RequestSnapshot) ([]byte, error) {
	return json.Marshal(snapshot)
}

// ParseSnapshot decodes one safe snapshot from its persisted bytes.
func ParseSnapshot(content []byte) (RequestSnapshot, error) {
	var snapshot RequestSnapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		return RequestSnapshot{}, fmt.Errorf("%w: %v", ErrSnapshotUnreadable, err)
	}
	return snapshot, nil
}

// Ref reports the readable reference to this snapshot under the named read
// entry: the digest covers the exact bytes MarshalSnapshot writes, and a reader
// rechecks it against what it reads back.
func (s RequestSnapshot) Ref(snapshotID, readEntry string) (SnapshotRef, error) {
	content, err := MarshalSnapshot(s)
	if err != nil {
		return SnapshotRef{}, err
	}
	sum := sha256.Sum256(content)
	return SnapshotRef{SnapshotID: snapshotID, Digest: hex.EncodeToString(sum[:]), ReadEntry: readEntry}, nil
}

// VerifySnapshot rechecks snapshot content against the reference it is read
// back under: an empty reference cannot read content back and is refused as
// unreadable, and content that does not match its digest is refused as a
// mismatch rather than read as the referenced snapshot.
func VerifySnapshot(ref SnapshotRef, content []byte) error {
	if ref.SnapshotID == "" || ref.Digest == "" {
		return fmt.Errorf("%w: reference (%q, %q)", ErrSnapshotUnreadable, ref.SnapshotID, ref.Digest)
	}
	sum := sha256.Sum256(content)
	if got := hex.EncodeToString(sum[:]); got != ref.Digest {
		return fmt.Errorf("%w: reference %s, content %s", ErrSnapshotMismatch, ref.Digest, got)
	}
	return nil
}
