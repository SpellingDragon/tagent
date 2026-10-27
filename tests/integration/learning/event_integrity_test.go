package learning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// integrityEntryModel, integrityWorkerModel, integrityWorkerAgent, integrityMaxBody and integrityOversized name the model identities, the worker agent and the size bounds.
const (
	integrityEntryModel  = "model-entry"
	integrityWorkerModel = "model-worker"
	integrityWorkerAgent = "worker"
	// integrityMaxBody bounds one decoded provider request: the oversized-worker
	// scenario carries a two-megabyte result back into the follow-up request.
	integrityMaxBody = 16 << 20
	// integrityOversized is the answer size that exceeds the sink's default
	// per-record bound (one megabyte) without changing the model protocol.
	integrityOversized = 2 << 20
)

// TestEventIntegrityCapture drives the capture a booted host mounts over its storage root and reads back what it recorded:
// - every real model request of a turn has one attempt record, including the delegated worker call
// - a failed attempt is recorded as failed and keeps the provider error text
// - a record the capture refused at admission is counted in the run's recording-loss account
// - a refused worker record stays counted in the capture ledger instead of disappearing
// - a turn that never happened writes no record
// - the offline export correlates samples with the seeded facts and attempts without inventing a reward or dropping an unrated turn
// - Boot's capture is read by the public export API after a real turn, and the sealed manifest accounts every attempt the export consumed
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestEventIntegrityCapture(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "integrity-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	t.Run("requests_and_attempts_match_records", runIntegrityRequestsAndAttempts)
	t.Run("failed_attempt_recorded_as_failure", runIntegrityFailedAttempt)
	t.Run("rejected_admission_in_loss_ledger", runIntegrityRejectedAdmission)
	t.Run("no_request_no_record", runIntegrityNoRequest)
	t.Run("export_correlation_is_not_reward", runIntegrityExportCorrelationIsNotReward)
	t.Run("formal_capture_then_offline_export", runIntegrityFormalCaptureThenOfflineExport)
}

// integrityProvider is the OpenAI-compatible endpoint the booted hosts run
// against: it records every decoded chat completion beside the scripted decision
// that answers it. A request outside the script is refused instead of answered
// with a default terminal turn, so a missing or extra request is visible.
type integrityProvider struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []integrityRequest
	decide   func(integrityRequest) (integrityReply, error)
}

// newIntegrityProvider starts the endpoint and closes it with the test.
func newIntegrityProvider(t *testing.T) *integrityProvider {
	t.Helper()
	provider := &integrityProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// serve answers one chat completion and records the request it carried.
func (p *integrityProvider) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, integrityMaxBody)).Decode(&body); err != nil {
		http.Error(w, "decode the chat completion: "+err.Error(), http.StatusBadRequest)
		return
	}
	request := integrityDecodeRequest(body)
	p.mu.Lock()
	request.Index = len(p.requests)
	p.requests = append(p.requests, request)
	decide := p.decide
	p.mu.Unlock()

	reply := integrityReply{status: http.StatusOK, body: integrityCompletion(map[string]any{
		"role":    "assistant",
		"content": "ok",
	})}
	if decide != nil {
		decided, err := decide(request)
		if err != nil {
			reply = integrityFailureReply(err.Error())
		} else {
			reply = decided
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(reply.status)
	_, _ = io.WriteString(w, reply.body)
}

// recordedRequests reports the decoded requests the endpoint answered, as a copy the caller owns.
func (p *integrityProvider) recordedRequests() []integrityRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]integrityRequest(nil), p.requests...)
}

// endpoint reports the base URL the host document's provider entry names.
func (p *integrityProvider) endpoint() string { return p.server.URL + "/v1" }

// integrityRequest is one chat completion request as the endpoint decoded it.
type integrityRequest struct {
	Index    int
	Model    string
	Messages []integrityMessage
	Tools    []string
}

// integrityMessage is one message a request carried.
type integrityMessage struct {
	Role       string
	Content    string
	ToolCallID string
}

// describe renders the request as the line a mismatch reads, without dumping
// the oversized bodies the refusal scenarios carry.
func (r integrityRequest) describe() string {
	roles := make([]string, 0, len(r.Messages))
	for _, message := range r.Messages {
		roles = append(roles, message.Role)
	}
	return fmt.Sprintf("model=%q messages=%v tools=%v tail=%q",
		r.Model, roles, r.Tools, integrityTruncate(integrityTail(r)))
}

// integrityDecodeRequest reads one recorded body into the shape a script
// selects on.
func integrityDecodeRequest(body map[string]json.RawMessage) integrityRequest {
	var request integrityRequest
	if raw, ok := body["model"]; ok {
		_ = json.Unmarshal(raw, &request.Model)
	}
	if raw, ok := body["messages"]; ok {
		var messages []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
		}
		if err := json.Unmarshal(raw, &messages); err == nil {
			for _, message := range messages {
				request.Messages = append(request.Messages, integrityMessage{
					Role:       message.Role,
					Content:    integrityContent(message.Content),
					ToolCallID: message.ToolCallID,
				})
			}
		}
	}
	if raw, ok := body["tools"]; ok {
		var tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &tools); err == nil {
			for _, tool := range tools {
				request.Tools = append(request.Tools, tool.Function.Name)
			}
		}
	}
	return request
}

// integrityContent reads one message content the protocol may render as a plain
// string or as text parts.
func integrityContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// integrityTail reports the content of the request's last message.
func integrityTail(request integrityRequest) string {
	if len(request.Messages) == 0 {
		return ""
	}
	return request.Messages[len(request.Messages)-1].Content
}

// integrityTruncate bounds one diagnostic string.
func integrityTruncate(text string) string {
	if len(text) <= 160 {
		return text
	}
	return text[:160] + "..."
}

// integrityCarriesToolResult reports whether the request carries the result
// recorded under one native call identity.
func integrityCarriesToolResult(request integrityRequest, callID string) bool {
	for _, message := range request.Messages {
		if message.ToolCallID == callID {
			return true
		}
	}
	return false
}

// integrityCarriesNotice reports whether the request carries the settled notice
// of the named body: the material a settle follow-up reads is the task, its
// terminal and the result body.
func integrityCarriesNotice(request integrityRequest, body string) bool {
	for _, message := range request.Messages {
		if message.Role != string(model.RoleUser) {
			continue
		}
		if strings.Contains(message.Content, "tagent: task ") &&
			strings.Contains(message.Content, " settled") &&
			strings.Contains(message.Content, body) {
			return true
		}
	}
	return false
}

// integrityCarriesFailedNotice reports whether the request carries any failed
// task notice, whose body a failed delegated turn may leave empty.
func integrityCarriesFailedNotice(request integrityRequest) bool {
	for _, message := range request.Messages {
		if message.Role != string(model.RoleUser) {
			continue
		}
		if strings.Contains(message.Content, "tagent: task ") &&
			strings.Contains(message.Content, " failed") {
			return true
		}
	}
	return false
}

// integrityHasTool reports whether one request declares the named native tool.
func integrityHasTool(request integrityRequest, name string) bool {
	for _, tool := range request.Tools {
		if tool == name {
			return true
		}
	}
	return false
}

// integrityReply is one scripted endpoint reply: the status and body the
// endpoint writes back.
type integrityReply struct {
	status int
	body   string
}

// integrityCompletion renders one chat completion carrying the named message.
func integrityCompletion(message map[string]any) string {
	payload := map[string]any{
		"id":      "scripted",
		"object":  "chat.completion",
		"created": 1699200000,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic("learning: encode the scripted completion: " + err.Error())
	}
	return string(encoded)
}

// integrityFinalReply renders the completion that ends a turn with the named
// assistant body.
func integrityFinalReply(content string) integrityReply {
	return integrityReply{status: http.StatusOK, body: integrityCompletion(map[string]any{
		"role":    "assistant",
		"content": content,
	})}
}

// integrityToolCallReply renders the completion that issues one native tool
// call under the named call identity.
func integrityToolCallReply(name, callID, arguments string) integrityReply {
	return integrityReply{status: http.StatusOK, body: integrityCompletion(map[string]any{
		"role":    "assistant",
		"content": "calling",
		"tool_calls": []any{map[string]any{
			"id":   callID,
			"type": "function",
			"function": map[string]any{
				"name":      name,
				"arguments": arguments,
			},
		}},
	})}
}

// integrityFailureReply renders the provider refusal the endpoint answers a
// scripted failing call with; the SDK error text carries the raw body, so a
// sentinel in the message reaches the attempt record.
func integrityFailureReply(message string) integrityReply {
	encoded, err := json.Marshal(message)
	if err != nil {
		panic("learning: encode the scripted refusal: " + err.Error())
	}
	return integrityReply{
		status: http.StatusBadRequest,
		body:   `{"error":{"message":` + string(encoded) + `,"type":"invalid_request_error"}}`,
	}
}

// integritySteps counts the scripted decisions a provider closure answered. A
// step reached twice is refused at the endpoint and audited at the end, so a
// duplicate or missing request never reads as a healthy drive.
type integritySteps struct {
	mu   sync.Mutex
	seen map[string]int
}

// newIntegritySteps returns an empty step account.
func newIntegritySteps() *integritySteps {
	return &integritySteps{seen: make(map[string]int)}
}

// reply accounts one reached step and answers it with the scripted reply.
func (s *integritySteps) reply(name string, reply integrityReply) (integrityReply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[name]++
	if s.seen[name] > 1 {
		return integrityReply{}, fmt.Errorf("the scripted step %q was reached %d times", name, s.seen[name])
	}
	return reply, nil
}

// audit reports every step whose reach count differs from the declared one.
func (s *integritySteps) audit(t *testing.T, want map[string]int) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, expected := range want {
		if s.seen[name] != expected {
			t.Errorf("scripted step %q reached %d times, want %d", name, s.seen[name], expected)
		}
	}
	for name, got := range s.seen {
		if _, declared := want[name]; !declared {
			t.Errorf("scripted step %q reached %d times without a declaration", name, got)
		}
	}
}

// integritySender is the outbound face of the booted host: every accepted body
// is recorded beside its target, so a check reads what the host really sent.
type integritySender struct {
	mu      sync.Mutex
	targets []event.Address
	bodies  []string
}

// newIntegritySender returns the recording outbound face.
func newIntegritySender() *integritySender { return &integritySender{} }

// Send records one delivered body beside its target address.
func (s *integritySender) Send(_ context.Context, target event.Address, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets = append(s.targets, target)
	s.bodies = append(s.bodies, content)
	return nil
}

// delivered counts the bodies the host delivered to one address carrying the
// named text.
func (s *integritySender) delivered(addr event.Address, text string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for i := range s.bodies {
		if s.targets[i] == addr && strings.Contains(s.bodies[i], text) {
			count++
		}
	}
	return count
}

// await waits until the named body reached the address at least once, so a
// check reads a delivery the host really made instead of racing it.
func (s *integritySender) await(t *testing.T, addr event.Address, text string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		if s.delivered(addr, text) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the body %q was never delivered to %+v", text, addr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// integrityDocument renders one host document over the scripted endpoint. The
// worker agent is declared only when the check drives a delegated call.
func integrityDocument(root, endpoint string, worker bool) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + integrityEntryModel + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  user:\n")
	b.WriteString("    model: " + integrityEntryModel + "\n")
	b.WriteString("    max_tokens: 32000\n")
	if worker {
		b.WriteString("    tools:\n")
		b.WriteString("      - { kind: agent, agent: " + integrityWorkerAgent + " }\n")
		b.WriteString("  " + integrityWorkerAgent + ":\n")
		b.WriteString("    model: " + integrityWorkerModel + "\n")
		b.WriteString("    max_tokens: 32000\n")
	}
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// integrityBoot boots one host over a fresh storage root and returns its run,
// the recording outbound face and the root the capture lands under.
func integrityBoot(t *testing.T, endpoint string, worker bool) (*tagent.Host, *integritySender, string) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(integrityDocument(root, endpoint, worker)), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	sender := newIntegritySender()
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = host.Close(ctx)
	})
	return host, sender, root
}

// integrityInvoke drives one turn through the formal host entry.
func integrityInvoke(t *testing.T, ctx context.Context, host *tagent.Host, addr event.Address, invocation event.InvocationID, episode event.EpisodeID, input string) agent.Turn {
	t.Helper()
	turn, err := host.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: invocation,
		Episode:      episode,
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	})
	if err != nil {
		t.Fatalf("invoke %s: %v", invocation, err)
	}
	return turn
}

// integrityClose closes the host and fails the check when the drain reports an
// error.
func integrityClose(t *testing.T, ctx context.Context, host *tagent.Host) {
	t.Helper()
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// integrityDir reports the trajectory directory Boot opened under the storage
// root.
func integrityDir(root string) string { return filepath.Join(root, "trajectories") }

// integritySeal reads the capture seal under the storage root.
func integritySeal(t *testing.T, root string) rl.CaptureManifest {
	t.Helper()
	manifest, err := rl.ReadCaptureSeal(integrityDir(root))
	if err != nil {
		t.Fatalf("read the capture seal: %v", err)
	}
	return manifest
}

// integrityAttempts reads every stored attempt line of the boot capture.
func integrityAttempts(t *testing.T, root string) []rl.AttemptRecord {
	t.Helper()
	path := filepath.Join(integrityDir(root), rl.SinkRecordsFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read the capture records: %v", err)
	}
	var attempts []rl.AttemptRecord
	for _, raw := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		line, err := rl.ParseLine(raw)
		if err != nil {
			t.Fatalf("parse the stored record %q: %v", integrityTruncate(string(raw)), err)
		}
		if line.Kind != rl.RecordKindAttempt || line.Attempt == nil {
			t.Fatalf("stored record kind = %q, want the attempt line the sink writes", line.Kind)
		}
		attempts = append(attempts, *line.Attempt)
	}
	return attempts
}

// integrityRequireCleanSeal checks the sealed manifest accounts exactly the
// named number of records and rechecks the file digest against the bytes on
// disk. An empty capture must leave no record file behind.
func integrityRequireCleanSeal(t *testing.T, root string, records int) rl.CaptureManifest {
	t.Helper()
	manifest := integritySeal(t, root)
	if manifest.CaptureID != "host-trajectory" {
		t.Fatalf("manifest capture id = %q, want the boot capture identity", manifest.CaptureID)
	}
	if !manifest.Sealed || manifest.Partial {
		t.Fatalf("manifest = %+v, want the writer-confirmed seal of a complete capture", manifest)
	}
	if manifest.Records != int64(records) || manifest.Dropped != 0 || manifest.InFlight != 0 {
		t.Fatalf("manifest = %+v, want %d records without loss or in-flight residue", manifest, records)
	}
	path := filepath.Join(integrityDir(root), rl.SinkRecordsFile)
	if records == 0 {
		if len(manifest.Files) != 0 {
			t.Fatalf("manifest files = %+v, want none for an empty capture", manifest.Files)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("record file stat error = %v, want the file absent for an empty capture", err)
		}
		return manifest
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("manifest files = %+v, want the one record file", manifest.Files)
	}
	entry := manifest.Files[0]
	if entry.Name != rl.SinkRecordsFile || entry.Records != int64(records) {
		t.Fatalf("manifest file entry = %+v, want %s with %d records", entry, rl.SinkRecordsFile, records)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the sealed record file: %v", err)
	}
	if entry.Bytes != int64(len(data)) {
		t.Fatalf("manifest file bytes = %d, want the %d bytes on disk", entry.Bytes, len(data))
	}
	if entry.SHA256 != fileDigest(t, path) {
		t.Fatalf("manifest file digest = %q, want the digest of the bytes on disk", entry.SHA256)
	}
	return manifest
}

// integrityReasonsContain reports whether one manifest reason carries the named
// text.
func integrityReasonsContain(reasons []string, text string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, text) {
			return true
		}
	}
	return false
}

// integrityAttemptOfDecision reports the one attempt recorded under the decision
// identity.
func integrityAttemptOfDecision(t *testing.T, attempts []rl.AttemptRecord, decisionID event.DecisionID) rl.AttemptRecord {
	t.Helper()
	var found []rl.AttemptRecord
	for _, attempt := range attempts {
		if attempt.DecisionID == decisionID {
			found = append(found, attempt)
		}
	}
	if len(found) != 1 {
		t.Fatalf("attempts under decision %q = %d, want exactly one", decisionID, len(found))
	}
	return found[0]
}

// integrityAttemptByContent reports the one attempt whose response carries the
// named assistant body.
func integrityAttemptByContent(t *testing.T, attempts []rl.AttemptRecord, content string) rl.AttemptRecord {
	t.Helper()
	var found []rl.AttemptRecord
	for _, attempt := range attempts {
		if integrityAttemptContent(attempt) == content {
			found = append(found, attempt)
		}
	}
	if len(found) != 1 {
		t.Fatalf("attempts carrying %q = %d, want exactly one", integrityTruncate(content), len(found))
	}
	return found[0]
}

// integrityAttemptByDecisionPrefix reports the one attempt whose decision
// identity starts with the named prefix.
func integrityAttemptByDecisionPrefix(t *testing.T, attempts []rl.AttemptRecord, prefix string) rl.AttemptRecord {
	t.Helper()
	var found []rl.AttemptRecord
	for _, attempt := range attempts {
		if strings.HasPrefix(string(attempt.DecisionID), prefix) {
			found = append(found, attempt)
		}
	}
	if len(found) != 1 {
		t.Fatalf("attempts under decision prefix %q = %d, want exactly one", prefix, len(found))
	}
	return found[0]
}

// integrityAttemptContent reports the assistant body one attempt response
// carries, or the empty string for an error response with no choice.
func integrityAttemptContent(attempt rl.AttemptRecord) string {
	if len(attempt.Response.Choices) == 0 {
		return ""
	}
	return attempt.Response.Choices[0].Message.Content
}

// integrityAttemptHasToolCall reports whether one attempt response carries the
// named native tool call.
func integrityAttemptHasToolCall(attempt rl.AttemptRecord, callID string) bool {
	for _, choice := range attempt.Response.Choices {
		for _, call := range choice.Message.ToolCalls {
			if call.ID == callID {
				return true
			}
		}
	}
	return false
}

// integrityFactWithContent reports the one fact of the named type carrying the
// named content in the session facts.
func integrityFactWithContent(t *testing.T, facts []event.Event, kind, content string) event.Event {
	t.Helper()
	var found []event.Event
	for _, fact := range facts {
		if fact.Type == kind && fact.Message.Content == content {
			found = append(found, fact)
		}
	}
	if len(found) != 1 {
		t.Fatalf("facts of type %q carrying %q = %d, want exactly one", kind, integrityTruncate(content), len(found))
	}
	return found[0]
}

// integritySampleKeys renders the samples through the public JSONL writer and
// reads every line back as its raw key set.
func integritySampleKeys(t *testing.T, samples []rl.SFTSample) []map[string]json.RawMessage {
	t.Helper()
	var buffer bytes.Buffer
	if err := rl.WriteSamples(&buffer, samples); err != nil {
		t.Fatalf("write the samples: %v", err)
	}
	var rows []map[string]json.RawMessage
	for _, raw := range bytes.Split(buffer.Bytes(), []byte("\n")) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var row map[string]json.RawMessage
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatalf("parse the written sample %q: %v", integrityTruncate(string(raw)), err)
		}
		rows = append(rows, row)
	}
	return rows
}

// runIntegrityRequestsAndAttempts drives one delegated turn and checks every real model request has exactly one attempt record:
// - the entry decisions, the delegated worker decision and the settle follow-up each produced one request
// - the worker attempt is captured beside the entry attempts and names no episode, because the delegated turn carries no application episode
// - every attempt identity is derived from its decision, and the sealed manifest accounts all four attempts
func runIntegrityRequestsAndAttempts(t *testing.T) {
	provider := newIntegrityProvider(t)
	steps := newIntegritySteps()
	provider.decide = func(request integrityRequest) (integrityReply, error) {
		switch {
		case request.Model == integrityEntryModel && integrityCarriesNotice(request, "worker final answer"):
			return steps.reply("follow-up", integrityFinalReply("follow-up acknowledged"))
		case request.Model == integrityEntryModel && integrityCarriesToolResult(request, "call-worker-1"):
			return steps.reply("entry-accept", integrityFinalReply("entry accepted the delegation"))
		case request.Model == integrityEntryModel && integrityTail(request) == "delegate the worker task":
			return steps.reply("entry-delegate", integrityToolCallReply(integrityWorkerAgent, "call-worker-1", `{"input":"worker input text"}`))
		case request.Model == integrityWorkerModel && integrityTail(request) == "worker input text":
			return steps.reply("worker", integrityFinalReply("worker final answer"))
		}
		return integrityReply{}, fmt.Errorf("unscripted request: %s", request.describe())
	}
	host, sender, root := integrityBoot(t, provider.endpoint(), true)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-integrity-delegate"}

	turn := integrityInvoke(t, ctx, host, addr, "inv-integrity-delegate", "episode-integrity-delegate", "delegate the worker task")
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
		t.Fatalf("entry turn = (%v, %d decisions), want the completed two-decision turn", turn.Terminal, len(turn.Decisions))
	}
	sender.await(t, addr, "follow-up acknowledged")
	steps.audit(t, map[string]int{
		"entry-delegate": 1,
		"entry-accept":   1,
		"worker":         1,
		"follow-up":      1,
	})

	requests := provider.recordedRequests()
	if len(requests) != 4 {
		t.Fatalf("model requests = %d, want one per scripted decision", len(requests))
	}
	models := map[string]int{}
	for _, request := range requests {
		models[request.Model]++
	}
	if models[integrityEntryModel] != 3 || models[integrityWorkerModel] != 1 {
		t.Fatalf("request models = %v, want three entry and one worker request", models)
	}
	declaredWorker := false
	for _, request := range requests {
		if request.Model == integrityEntryModel && integrityHasTool(request, integrityWorkerAgent) {
			declaredWorker = true
		}
	}
	if !declaredWorker {
		t.Fatalf("no entry request declared the worker tool %q", integrityWorkerAgent)
	}

	integrityClose(t, ctx, host)
	attempts := integrityAttempts(t, root)
	if len(attempts) != 4 {
		t.Fatalf("attempt records = %d, want one per real model request", len(attempts))
	}
	entryOne := integrityAttemptOfDecision(t, attempts, turn.Decisions[0].DecisionID)
	entryTwo := integrityAttemptOfDecision(t, attempts, turn.Decisions[1].DecisionID)
	if !integrityAttemptHasToolCall(entryOne, "call-worker-1") {
		t.Fatalf("first entry attempt response = %+v, want the worker call it issued", entryOne.Response)
	}
	if integrityAttemptContent(entryTwo) != "entry accepted the delegation" {
		t.Fatalf("second entry attempt content = %q, want the accepted delegation", integrityAttemptContent(entryTwo))
	}
	worker := integrityAttemptByContent(t, attempts, "worker final answer")
	if worker.EpisodeID != "" {
		t.Fatalf("worker attempt episode = %q, want none for a delegated turn", worker.EpisodeID)
	}
	if !strings.HasPrefix(string(worker.DecisionID), "worker-"+integrityWorkerAgent+"-") {
		t.Fatalf("worker decision = %q, want the delegated decision identity", worker.DecisionID)
	}
	if !strings.HasPrefix(string(worker.ID), "attempt-worker-"+integrityWorkerAgent+"-") {
		t.Fatalf("worker attempt id = %q, want the attempt identity derived from its decision", worker.ID)
	}
	followUp := integrityAttemptByContent(t, attempts, "follow-up acknowledged")
	if followUp.EpisodeID != "" {
		t.Fatalf("follow-up attempt episode = %q, want none for the host notice turn", followUp.EpisodeID)
	}
	if followUp.DecisionID == turn.Decisions[0].DecisionID || followUp.DecisionID == turn.Decisions[1].DecisionID {
		t.Fatalf("follow-up decision = %q, want a range beside the entry decisions", followUp.DecisionID)
	}

	seen := map[event.DecisionID]bool{}
	entryAttempts, workerAttempts := 0, 0
	for _, attempt := range attempts {
		if attempt.Terminal != model.KindCompleted || attempt.Error != "" {
			t.Fatalf("attempt %s = (terminal %v, error %q), want a completed attempt without error", attempt.ID, attempt.Terminal, attempt.Error)
		}
		if attempt.ID != event.AttemptID("attempt-"+string(attempt.DecisionID)) {
			t.Fatalf("attempt id = %q, want the identity derived from decision %q", attempt.ID, attempt.DecisionID)
		}
		if seen[attempt.DecisionID] {
			t.Fatalf("decision %q recorded twice, want one attempt per decision", attempt.DecisionID)
		}
		seen[attempt.DecisionID] = true
		if strings.HasPrefix(string(attempt.DecisionID), "worker-"+integrityWorkerAgent+"-") {
			workerAttempts++
			if attempt.EpisodeID != "" {
				t.Fatalf("worker attempt %s episode = %q, want none for a delegated turn", attempt.ID, attempt.EpisodeID)
			}
			continue
		}
		entryAttempts++
		if attempt.EpisodeID != "" && attempt.EpisodeID != event.EpisodeID("episode-integrity-delegate") {
			t.Fatalf("entry attempt %s episode = %q, want the application episode or none for the follow-up", attempt.ID, attempt.EpisodeID)
		}
	}
	if entryAttempts != 3 || workerAttempts != 1 {
		t.Fatalf("attempts = (%d entry, %d worker), want three entry and one delegated attempt", entryAttempts, workerAttempts)
	}
	if losses := host.Run().RecordingLosses(); len(losses) != 0 {
		t.Fatalf("recording losses = %+v, want none for the admitted capture", losses)
	}
	integrityRequireCleanSeal(t, root, 4)
}

// runIntegrityFailedAttempt drives one entry failure and one delegated worker failure and checks both are recorded as failures:
// - the failed entry attempt keeps the provider refusal text
// - the failed worker attempt is recorded by the capture wrapper with its provider refusal text
// - the delegated observation does not swallow the worker refusal
// - the failed tasks fire their follow-up ranges, and every recorded failure stays an attempt record rather than a recording loss
func runIntegrityFailedAttempt(t *testing.T) {
	const (
		entrySentinel  = "integrity-entry-refusal-sentinel"
		workerSentinel = "integrity-worker-refusal-sentinel"
	)
	provider := newIntegrityProvider(t)
	steps := newIntegritySteps()
	provider.decide = func(request integrityRequest) (integrityReply, error) {
		switch {
		case request.Model == integrityEntryModel && integrityTail(request) == "fail the entry attempt":
			return steps.reply("entry-fail", integrityFailureReply(entrySentinel))
		case request.Model == integrityEntryModel && integrityCarriesFailedNotice(request):
			return steps.reply("follow-up-fail", integrityFinalReply("follow-up after worker failure"))
		case request.Model == integrityEntryModel && integrityCarriesToolResult(request, "call-worker-fail"):
			return steps.reply("accept-fail", integrityFinalReply("entry accepted the failing delegation"))
		case request.Model == integrityEntryModel && integrityTail(request) == "delegate the failing worker":
			return steps.reply("delegate-fail", integrityToolCallReply(integrityWorkerAgent, "call-worker-fail", `{"input":"failing worker input"}`))
		case request.Model == integrityWorkerModel && integrityTail(request) == "failing worker input":
			return steps.reply("worker-fail", integrityFailureReply(workerSentinel))
		}
		return integrityReply{}, fmt.Errorf("unscripted request: %s", request.describe())
	}
	host, sender, root := integrityBoot(t, provider.endpoint(), true)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	entryAddr := event.Address{AgentID: "user", SessionID: "session-integrity-fail-entry"}
	entryTurn := integrityInvoke(t, ctx, host, entryAddr, "inv-integrity-fail-entry", "episode-integrity-fail-entry", "fail the entry attempt")
	if entryTurn.Terminal != model.KindFailed || len(entryTurn.Decisions) != 1 {
		t.Fatalf("entry failure turn = (%v, %d decisions), want one failed decision", entryTurn.Terminal, len(entryTurn.Decisions))
	}

	workerAddr := event.Address{AgentID: "user", SessionID: "session-integrity-fail-worker"}
	workerTurn := integrityInvoke(t, ctx, host, workerAddr, "inv-integrity-fail-worker", "episode-integrity-fail-worker", "delegate the failing worker")
	if workerTurn.Terminal != model.KindCompleted || len(workerTurn.Decisions) != 2 {
		t.Fatalf("delegated turn = (%v, %d decisions), want the completed two-decision turn", workerTurn.Terminal, len(workerTurn.Decisions))
	}
	sender.await(t, workerAddr, "follow-up after worker failure")
	steps.audit(t, map[string]int{
		"entry-fail":     1,
		"delegate-fail":  1,
		"accept-fail":    1,
		"worker-fail":    1,
		"follow-up-fail": 1,
	})

	integrityClose(t, ctx, host)
	attempts := integrityAttempts(t, root)
	if len(attempts) != 5 {
		t.Fatalf("attempt records = %d, want the five real model requests", len(attempts))
	}
	entryFailure := integrityAttemptOfDecision(t, attempts, entryTurn.Decisions[0].DecisionID)
	if entryFailure.Terminal != model.KindFailed {
		t.Fatalf("entry failure terminal = %v, want the failed terminal of the reduction", entryFailure.Terminal)
	}
	if !strings.Contains(entryFailure.Error, entrySentinel) {
		t.Fatalf("entry failure error = %q, want the provider refusal %q", entryFailure.Error, entrySentinel)
	}
	if entryFailure.EpisodeID != event.EpisodeID("episode-integrity-fail-entry") {
		t.Fatalf("entry failure episode = %q, want the application episode", entryFailure.EpisodeID)
	}

	workerFailure := integrityAttemptByDecisionPrefix(t, attempts, "worker-"+integrityWorkerAgent+"-")
	if workerFailure.Terminal != model.KindFailed {
		t.Fatalf("worker failure terminal = %v, want the failed terminal the capture wrapper recorded", workerFailure.Terminal)
	}
	if !strings.Contains(workerFailure.Error, workerSentinel) {
		t.Fatalf("worker failure error = %q, want the provider refusal %q", workerFailure.Error, workerSentinel)
	}
	if workerFailure.EpisodeID != "" {
		t.Fatalf("worker failure episode = %q, want none for a delegated turn", workerFailure.EpisodeID)
	}

	for content, wantEpisode := range map[string]event.EpisodeID{
		"calling":                               "episode-integrity-fail-worker",
		"entry accepted the failing delegation": "episode-integrity-fail-worker",
		"follow-up after worker failure":        "",
	} {
		attempt := integrityAttemptByContent(t, attempts, content)
		if attempt.Terminal != model.KindCompleted || attempt.Error != "" {
			t.Fatalf("attempt carrying %q = (terminal %v, error %q), want a completed attempt", integrityTruncate(content), attempt.Terminal, attempt.Error)
		}
		if attempt.EpisodeID != wantEpisode {
			t.Fatalf("attempt carrying %q episode = %q, want %q", integrityTruncate(content), attempt.EpisodeID, wantEpisode)
		}
	}
	failures := 0
	for _, attempt := range attempts {
		if attempt.ID != event.AttemptID("attempt-"+string(attempt.DecisionID)) {
			t.Fatalf("attempt id = %q, want the identity derived from decision %q", attempt.ID, attempt.DecisionID)
		}
		if attempt.Terminal == model.KindFailed {
			failures++
			if attempt.Error == "" {
				t.Fatalf("failed attempt %s carries no error text", attempt.ID)
			}
		}
	}
	if failures != 2 {
		t.Fatalf("failed attempts = %d, want the entry refusal and the worker refusal", failures)
	}
	if losses := host.Run().RecordingLosses(); len(losses) != 0 {
		t.Fatalf("recording losses = %+v, want the failures recorded as attempts instead", losses)
	}
	integrityRequireCleanSeal(t, root, 5)
}

// runIntegrityRejectedAdmission checks a record the capture refused at admission is a counted loss and never a silent disappearance:
// - an entry record past the per-record bound reaches the run's RecordingLosses account with the refused attempt identity and bound
// - a worker record past the same bound is refused by the same sink, and the sealed manifest counts it under Dropped
// - the delegated wrapper never surfaces the refusal to the run's loop-level loss account
func runIntegrityRejectedAdmission(t *testing.T) {
	integrityRejectedEntryScenario(t)
	integrityRejectedWorkerScenario(t)
}

// integrityRejectedEntryScenario drives one entry answer past the sink's
// per-record bound and checks the loop counted the refused attempt.
func integrityRejectedEntryScenario(t *testing.T) {
	provider := newIntegrityProvider(t)
	steps := newIntegritySteps()
	provider.decide = func(request integrityRequest) (integrityReply, error) {
		if request.Model == integrityEntryModel && integrityTail(request) == "oversize the entry attempt" {
			return steps.reply("entry-oversized", integrityFinalReply("oversized entry answer "+strings.Repeat("x", integrityOversized)))
		}
		return integrityReply{}, fmt.Errorf("unscripted request: %s", request.describe())
	}
	host, _, root := integrityBoot(t, provider.endpoint(), false)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-integrity-oversized-entry"}

	turn := integrityInvoke(t, ctx, host, addr, "inv-integrity-oversized-entry", "episode-integrity-oversized-entry", "oversize the entry attempt")
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 1 {
		t.Fatalf("oversized turn = (%v, %d decisions), want the completed single-decision turn", turn.Terminal, len(turn.Decisions))
	}
	steps.audit(t, map[string]int{"entry-oversized": 1})

	losses := host.Run().RecordingLosses()
	if len(losses) != 1 {
		t.Fatalf("recording losses = %+v, want the one refused attempt", losses)
	}
	wantID := "attempt-" + string(turn.Decisions[0].DecisionID)
	if losses[0].Kind != "capture-attempt" || losses[0].ID != wantID {
		t.Fatalf("recording loss = (%q, %q), want the refused attempt %q", losses[0].Kind, losses[0].ID, wantID)
	}
	if !errors.Is(losses[0].Err, rl.ErrCaptureRefused) {
		t.Fatalf("recording loss error = %v, want ErrCaptureRefused", losses[0].Err)
	}
	if !strings.Contains(losses[0].Err.Error(), string(rl.BoundRecordSize)) {
		t.Fatalf("recording loss error = %q, want the record-size bound named", losses[0].Err)
	}

	integrityClose(t, ctx, host)
	if attempts := integrityAttempts(t, root); len(attempts) != 0 {
		t.Fatalf("stored attempts = %d, want none for the refused record", len(attempts))
	}
	manifest := integritySeal(t, root)
	if !manifest.Sealed || !manifest.Partial {
		t.Fatalf("manifest = %+v, want a sealed but partial capture", manifest)
	}
	if manifest.Records != 0 || manifest.Dropped != 1 || manifest.InFlight != 0 {
		t.Fatalf("manifest = %+v, want the one refused record counted and no stored record", manifest)
	}
	if len(manifest.Files) != 0 {
		t.Fatalf("manifest files = %+v, want none for a capture that stored no record", manifest.Files)
	}
	if !integrityReasonsContain(manifest.Reasons, "refused under an admission bound") {
		t.Fatalf("manifest reasons = %v, want the admission refusal named", manifest.Reasons)
	}
	if _, err := os.Stat(filepath.Join(integrityDir(root), rl.SinkRecordsFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record file stat error = %v, want the file absent when every record was refused", err)
	}
}

// integrityRejectedWorkerScenario drives one delegated answer past the same
// bound: the worker wrapper does not surface the refusal to the loop, but the
// capture ledger and the sealed manifest still count it.
func integrityRejectedWorkerScenario(t *testing.T) {
	provider := newIntegrityProvider(t)
	steps := newIntegritySteps()
	oversizedWorker := "oversized worker answer " + strings.Repeat("y", integrityOversized)
	provider.decide = func(request integrityRequest) (integrityReply, error) {
		switch {
		case request.Model == integrityEntryModel && integrityCarriesNotice(request, "oversized worker answer"):
			return steps.reply("follow-up", integrityFinalReply("follow-up after oversized worker"))
		case request.Model == integrityEntryModel && integrityCarriesToolResult(request, "call-oversized-worker"):
			return steps.reply("accept", integrityFinalReply("entry accepted the oversized delegation"))
		case request.Model == integrityEntryModel && integrityTail(request) == "delegate the oversized worker":
			return steps.reply("delegate", integrityToolCallReply(integrityWorkerAgent, "call-oversized-worker", `{"input":"oversized worker input"}`))
		case request.Model == integrityWorkerModel && integrityTail(request) == "oversized worker input":
			return steps.reply("worker", integrityFinalReply(oversizedWorker))
		}
		return integrityReply{}, fmt.Errorf("unscripted request: %s", request.describe())
	}
	host, sender, root := integrityBoot(t, provider.endpoint(), true)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-integrity-oversized-worker"}

	turn := integrityInvoke(t, ctx, host, addr, "inv-integrity-oversized-worker", "episode-integrity-oversized-worker", "delegate the oversized worker")
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
		t.Fatalf("oversized delegated turn = (%v, %d decisions), want the completed two-decision turn", turn.Terminal, len(turn.Decisions))
	}
	sender.await(t, addr, "follow-up after oversized worker")
	steps.audit(t, map[string]int{
		"delegate":  1,
		"accept":    1,
		"worker":    1,
		"follow-up": 1,
	})
	if losses := host.Run().RecordingLosses(); len(losses) != 0 {
		t.Fatalf("recording losses = %+v, want the delegated refusal counted at the capture ledger, not the loop", losses)
	}

	integrityClose(t, ctx, host)
	attempts := integrityAttempts(t, root)
	if len(attempts) != 3 {
		t.Fatalf("stored attempts = %d, want the three entry attempts beside the refused worker attempt", len(attempts))
	}
	for _, attempt := range attempts {
		if strings.HasPrefix(string(attempt.DecisionID), "worker-") {
			t.Fatalf("stored attempt %s carries a worker decision, want the refused worker record absent", attempt.ID)
		}
		if attempt.EpisodeID != "" && attempt.EpisodeID != event.EpisodeID("episode-integrity-oversized-worker") {
			t.Fatalf("stored attempt %s episode = %q, want the entry episode or none for the follow-up", attempt.ID, attempt.EpisodeID)
		}
	}
	manifest := integritySeal(t, root)
	if !manifest.Sealed || !manifest.Partial {
		t.Fatalf("manifest = %+v, want a sealed but partial capture", manifest)
	}
	if manifest.Records != 3 || manifest.Dropped != 1 || manifest.InFlight != 0 {
		t.Fatalf("manifest = %+v, want three stored entry attempts and the one refused worker attempt", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Name != rl.SinkRecordsFile || manifest.Files[0].Records != 3 {
		t.Fatalf("manifest files = %+v, want the record file with three attempts", manifest.Files)
	}
	if !integrityReasonsContain(manifest.Reasons, "refused under an admission bound") {
		t.Fatalf("manifest reasons = %v, want the worker admission refusal named", manifest.Reasons)
	}
}

// runIntegrityNoRequest checks a turn that never happened writes no capture
// record:
//   - the incomplete address is refused by name before any fact or request
//   - no model request reaches the provider
//   - the sealed capture reports an empty account and leaves no record file
func runIntegrityNoRequest(t *testing.T) {
	provider := newIntegrityProvider(t)
	provider.decide = func(request integrityRequest) (integrityReply, error) {
		return integrityReply{}, fmt.Errorf("unexpected model request: %s", request.describe())
	}
	host, _, root := integrityBoot(t, provider.endpoint(), false)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	_, err := host.Invoke(ctx, tagent.InvokeRequest{
		Address: event.Address{AgentID: "user"},
		Input:   []model.Message{{Role: model.RoleUser, Content: "never sent"}},
	})
	if !errors.Is(err, tagent.ErrHostAddressMissing) {
		t.Fatalf("incomplete address error = %v, want ErrHostAddressMissing", err)
	}
	if requests := provider.recordedRequests(); len(requests) != 0 {
		t.Fatalf("model requests = %d, want none for the refused turn", len(requests))
	}
	if losses := host.Run().RecordingLosses(); len(losses) != 0 {
		t.Fatalf("recording losses = %+v, want none for a turn that never ran", losses)
	}
	integrityClose(t, ctx, host)
	integrityRequireCleanSeal(t, root, 0)
}

// runIntegrityExportCorrelationIsNotReward checks the offline export is a correlation read and not a reward mapping:
// - the two samples pair each input with the output of its own invocation
// - the public writer emits no reward, rating or score key, and the sample shape carries exactly the five correlation fields
// - both turns are exported although no fact carries a rating, so an unrated turn is retained rather than filtered
// - reading the chain mutates no fact, and the sealed manifest accounts both attempts the samples correlate with
func runIntegrityExportCorrelationIsNotReward(t *testing.T) {
	provider := newIntegrityProvider(t)
	steps := newIntegritySteps()
	provider.decide = func(request integrityRequest) (integrityReply, error) {
		if request.Model != integrityEntryModel {
			return integrityReply{}, fmt.Errorf("unscripted model request: %s", request.describe())
		}
		switch integrityTail(request) {
		case "rewards question one":
			return steps.reply("one", integrityFinalReply("verdict one"))
		case "rewards question two":
			return steps.reply("two", integrityFinalReply("verdict two"))
		}
		return integrityReply{}, fmt.Errorf("unscripted request: %s", request.describe())
	}
	host, _, root := integrityBoot(t, provider.endpoint(), false)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-integrity-reward"}

	first := integrityInvoke(t, ctx, host, addr, "inv-integrity-reward-1", "episode-integrity-reward", "rewards question one")
	second := integrityInvoke(t, ctx, host, addr, "inv-integrity-reward-2", "episode-integrity-reward", "rewards question two")
	if first.Terminal != model.KindCompleted || second.Terminal != model.KindCompleted {
		t.Fatalf("turns = (%v, %v), want both completed", first.Terminal, second.Terminal)
	}
	steps.audit(t, map[string]int{"one": 1, "two": 1})

	before := host.Run().SessionFacts(addr)
	exporter := rl.NewChainExporter(host.Run())
	samples, err := exporter.Export(ctx, rl.ExportOptions{SessionIDs: []event.SessionID{addr.SessionID}})
	if err != nil {
		t.Fatalf("export the chain: %v", err)
	}
	want := []rl.SFTSample{
		{SessionID: string(addr.SessionID), Turn: 1, Input: "rewards question one", Label: "verdict one", EpisodeID: "episode-integrity-reward"},
		{SessionID: string(addr.SessionID), Turn: 2, Input: "rewards question two", Label: "verdict two", EpisodeID: "episode-integrity-reward"},
	}
	if !reflect.DeepEqual(samples, want) {
		t.Fatalf("samples = %+v, want %+v", samples, want)
	}
	after := host.Run().SessionFacts(addr)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("the export changed the session facts, want a read-only correlation")
	}

	typ := reflect.TypeOf(rl.SFTSample{})
	fields := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		fields[typ.Field(i).Name] = true
	}
	wantFields := map[string]bool{"SessionID": true, "Turn": true, "Input": true, "Label": true, "EpisodeID": true}
	if !reflect.DeepEqual(fields, wantFields) {
		t.Fatalf("SFTSample fields = %v, want exactly the correlation fields %v", fields, wantFields)
	}
	allowed := map[string]bool{"session_id": true, "turn": true, "input": true, "label": true, "episode_id": true}
	forbidden := []string{"reward", "rewards", "rating", "score", "preference", "labeler", "weight", "advantage"}
	rows := integritySampleKeys(t, samples)
	if len(rows) != 2 {
		t.Fatalf("written sample rows = %d, want both unrated turns retained", len(rows))
	}
	for _, row := range rows {
		for key := range row {
			if !allowed[key] {
				t.Fatalf("sample row carries key %q, want only the correlation keys %v", key, allowed)
			}
		}
		for _, key := range forbidden {
			if _, held := row[key]; held {
				t.Fatalf("sample row carries the reward key %q, want no automatic reward mapping", key)
			}
		}
	}

	integrityClose(t, ctx, host)
	attempts := integrityAttempts(t, root)
	if len(attempts) != 2 {
		t.Fatalf("attempt records = %d, want the two attempts the samples correlate with", len(attempts))
	}
	for _, sample := range samples {
		sessionAddr := event.Address{AgentID: "user", SessionID: event.SessionID(sample.SessionID)}
		output := integrityFactWithContent(t, host.Run().SessionFacts(sessionAddr), event.TypeAgentOutput, sample.Label)
		attempt := integrityAttemptOfDecision(t, attempts, output.DecisionID)
		if attempt.EpisodeID != event.EpisodeID(sample.EpisodeID) {
			t.Fatalf("attempt %s episode = %q, want the sample episode %q", attempt.ID, attempt.EpisodeID, sample.EpisodeID)
		}
	}
	integrityRequireCleanSeal(t, root, 2)
}

// runIntegrityFormalCaptureThenOfflineExport drives two real turns through Boot, exports the chain with the public API and correlates every sample:
// - the per-turn seal stays a no-op while the run serves, so the capture is open when the export reads it
// - exporting one session emits one row, and exporting both emits the two rows with turn numbers restarting per episode
// - each sample's input and label are facts of the same invocation and episode, and the output decision names exactly one stored attempt
// - the sealed boot manifest accounts every exported attempt and rechecks the record file digest
func runIntegrityFormalCaptureThenOfflineExport(t *testing.T) {
	provider := newIntegrityProvider(t)
	steps := newIntegritySteps()
	provider.decide = func(request integrityRequest) (integrityReply, error) {
		if request.Model != integrityEntryModel {
			return integrityReply{}, fmt.Errorf("unscripted model request: %s", request.describe())
		}
		switch integrityTail(request) {
		case "export question a":
			return steps.reply("a", integrityFinalReply("export answer a"))
		case "export question b":
			return steps.reply("b", integrityFinalReply("export answer b"))
		}
		return integrityReply{}, fmt.Errorf("unscripted request: %s", request.describe())
	}
	host, _, root := integrityBoot(t, provider.endpoint(), false)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addrA := event.Address{AgentID: "user", SessionID: "session-integrity-export-a"}
	addrB := event.Address{AgentID: "user", SessionID: "session-integrity-export-b"}

	first := integrityInvoke(t, ctx, host, addrA, "inv-integrity-export-a", "episode-integrity-export", "export question a")
	second := integrityInvoke(t, ctx, host, addrB, "inv-integrity-export-b", "episode-integrity-export", "export question b")
	if first.Terminal != model.KindCompleted || second.Terminal != model.KindCompleted {
		t.Fatalf("turns = (%v, %v), want both completed", first.Terminal, second.Terminal)
	}
	steps.audit(t, map[string]int{"a": 1, "b": 1})

	beforeClose := integritySeal(t, root)
	if beforeClose.Sealed {
		t.Fatalf("capture seal before the close = %+v, want the run-wide account still open", beforeClose)
	}

	exporter := rl.NewChainExporter(host.Run())
	onlyA, err := exporter.Export(ctx, rl.ExportOptions{SessionIDs: []event.SessionID{addrA.SessionID}})
	if err != nil {
		t.Fatalf("export the first session: %v", err)
	}
	if want := []rl.SFTSample{{SessionID: string(addrA.SessionID), Turn: 1, Input: "export question a", Label: "export answer a", EpisodeID: "episode-integrity-export"}}; !reflect.DeepEqual(onlyA, want) {
		t.Fatalf("first-session samples = %+v, want %+v", onlyA, want)
	}
	both, err := exporter.Export(ctx, rl.ExportOptions{SessionIDs: []event.SessionID{addrA.SessionID, addrB.SessionID}})
	if err != nil {
		t.Fatalf("export both sessions: %v", err)
	}
	wantBoth := []rl.SFTSample{
		{SessionID: string(addrA.SessionID), Turn: 1, Input: "export question a", Label: "export answer a", EpisodeID: "episode-integrity-export"},
		{SessionID: string(addrB.SessionID), Turn: 2, Input: "export question b", Label: "export answer b", EpisodeID: "episode-integrity-export"},
	}
	if !reflect.DeepEqual(both, wantBoth) {
		t.Fatalf("both-session samples = %+v, want %+v", both, wantBoth)
	}

	type correlation struct {
		sample   rl.SFTSample
		decision event.DecisionID
	}
	correlations := make([]correlation, 0, len(both))
	for _, sample := range both {
		sessionAddr := event.Address{AgentID: "user", SessionID: event.SessionID(sample.SessionID)}
		facts := host.Run().SessionFacts(sessionAddr)
		input := integrityFactWithContent(t, facts, event.TypeExternalInput, sample.Input)
		output := integrityFactWithContent(t, facts, event.TypeAgentOutput, sample.Label)
		if input.InvocationID != output.InvocationID {
			t.Fatalf("sample %q input invocation = %q, output invocation = %q, want the same range",
				sample.Input, input.InvocationID, output.InvocationID)
		}
		wantInvocation := map[event.SessionID]event.InvocationID{
			addrA.SessionID: "inv-integrity-export-a",
			addrB.SessionID: "inv-integrity-export-b",
		}[event.SessionID(sample.SessionID)]
		if input.InvocationID != wantInvocation {
			t.Fatalf("sample %q invocation = %q, want the formal invocation %q", sample.Input, input.InvocationID, wantInvocation)
		}
		if input.EpisodeID != event.EpisodeID(sample.EpisodeID) || output.EpisodeID != event.EpisodeID(sample.EpisodeID) {
			t.Fatalf("sample %q facts = (input episode %q, output episode %q), want %q",
				sample.Input, input.EpisodeID, output.EpisodeID, sample.EpisodeID)
		}
		if output.DecisionID == "" {
			t.Fatalf("sample %q output fact carries no decision identity", sample.Input)
		}
		correlations = append(correlations, correlation{sample: sample, decision: output.DecisionID})
	}

	integrityClose(t, ctx, host)
	attempts := integrityAttempts(t, root)
	if len(attempts) != 2 {
		t.Fatalf("attempt records = %d, want the two exported attempts", len(attempts))
	}
	consumed := map[event.DecisionID]bool{}
	for _, held := range correlations {
		attempt := integrityAttemptOfDecision(t, attempts, held.decision)
		if attempt.EpisodeID != event.EpisodeID(held.sample.EpisodeID) {
			t.Fatalf("attempt %s episode = %q, want the sample episode %q", attempt.ID, attempt.EpisodeID, held.sample.EpisodeID)
		}
		if integrityAttemptContent(attempt) != held.sample.Label {
			t.Fatalf("attempt %s content = %q, want the sample label %q", attempt.ID, integrityAttemptContent(attempt), held.sample.Label)
		}
		if attempt.Terminal != model.KindCompleted || attempt.Error != "" {
			t.Fatalf("attempt %s = (terminal %v, error %q), want the completed attempt the export consumed", attempt.ID, attempt.Terminal, attempt.Error)
		}
		consumed[held.decision] = true
	}
	if len(consumed) != len(attempts) {
		t.Fatalf("attempts consumed by samples = %d, want every one of the %d stored attempts", len(consumed), len(attempts))
	}
	if losses := host.Run().RecordingLosses(); len(losses) != 0 {
		t.Fatalf("recording losses = %+v, want none for the exported capture", losses)
	}
	integrityRequireCleanSeal(t, root, 2)
}
