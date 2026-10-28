//go:build smoke

// Local smoke on the four recall subtools: retrieval, exact read, recent page and causal walk.
//
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/tool/recall"
)

const (
	// smokeRecallQueryTurn drives recall_query and then recall_get.
	smokeRecallQueryTurn = "SMOKE-R600"
	// smokeRecallRecentTurn drives recall_recent.
	smokeRecallRecentTurn = "SMOKE-R601"
	// smokeRecallTraceTurn drives recall_trace.
	smokeRecallTraceTurn = "SMOKE-R602"
	// smokeRecallMissingTurn drives recall_get against a key nothing holds.
	smokeRecallMissingTurn = "SMOKE-R603"
	// smokeRecallParentKey is the seeded parent event.
	smokeRecallParentKey = event.EventID("a1b2c3d4e5f60001")
	// smokeRecallChildKey is the seeded child event the retrieval answers with.
	smokeRecallChildKey = event.EventID("a1b2c3d4e5f60002")
	// smokeRecallMissingKey names no event in the seeded tier.
	smokeRecallMissingKey = "ffffffffffffffff"
	// smokeRecallNeedle is the retrieval text only the child matches.
	smokeRecallNeedle = "rollout ledger"
	// smokeRecallOriginal is the child original the exact read serves back.
	smokeRecallOriginal = "SMOKE-RECALL-ORIGINAL signed rollout ledger body"
	// smokeRecallParentText is the parent content the keyword leg must not match.
	smokeRecallParentText = "SMOKE-RECALL-PARENT signing decision recorded"
	// smokeRecallChildText is the child content the keyword leg matches.
	smokeRecallChildText = "SMOKE-RECALL-CHILD rollout ledger signed"
	// smokeRecallParentNote is the rendered summary of the parent event.
	smokeRecallParentNote = "earlier decision"
	// smokeRecallChildNote is the rendered summary of the child event.
	smokeRecallChildNote = "rollout ledger signed"
	// smokeRecallFormat is the original format every seeded reference registers under.
	smokeRecallFormat = "text/plain"
	// smokeRecallRetention keeps every seeded original readable.
	smokeRecallRetention = "keep"
	// smokeRecallDimensions is the vector width the declared engine expects.
	smokeRecallDimensions = 8
	// smokeRecallVector is the scripted embedding answer, one fixed vector for every text.
	smokeRecallVector = "1,0,0,0,0,0,0,0"
)

// TestLocalSmokeWave2Recall drives the four recall tools through the production host wiring.
func TestLocalSmokeWave2Recall(t *testing.T) {
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	embeddings := newSmokeRecallEmbeddings(t)
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.Doc = func(c *smokeConfig) string { return smokeRecallDocument(c, embeddings.endpoint()) }
	cfg.write(t)
	smokeRecallSeed(t, cfg.StorageRoot)
	rig := bootSmokeRig(t, cfg, provider)
	smokeRecallScript(provider)

	chat := smokeChats[0]

	rig.drive(chat, smokeRecallQueryTurn+" recall query then get")
	rig.waitText(chat, "recall get body "+smokeRecallOriginal, 30*time.Second)

	query, queryRaw := smokeRecallAnswerOf(t, rig, smokeRecallQueryTurn, recall.QueryToolName)
	if got := query["tool"]; got != recall.QueryToolName {
		t.Fatalf("recall_query tool = %v, want %q: %s", got, recall.QueryToolName, queryRaw)
	}
	partitions, ok := query["partitions"].([]any)
	if !ok || len(partitions) != 1 || partitions[0] != smokeSessionTag {
		t.Fatalf("recall_query partitions = %v, want [%s]: %s", query["partitions"], smokeSessionTag, queryRaw)
	}
	if got := smokeRecallInt(query, "count"); got < 1 {
		t.Fatalf("recall_query count = %d, want at least 1: %s", got, queryRaw)
	}
	if got := smokeRecallInt(query, "dropped"); got != 0 {
		t.Fatalf("recall_query dropped = %d, want 0: %s", got, queryRaw)
	}
	queryItems := smokeRecallItemPage(t, query)
	child, held := smokeRecallItemOf(queryItems, smokeRecallChildKey)
	if !held {
		t.Fatalf("recall_query items = %v, want the seeded child %s: %s",
			smokeRecallItemKeys(queryItems), smokeRecallChildKey, queryRaw)
	}
	if got := child["summary"]; got != smokeRecallChildNote {
		t.Fatalf("recall_query child summary = %v, want %q: %s", got, smokeRecallChildNote, queryRaw)
	}
	if strings.Contains(queryRaw, "wires no read face") {
		t.Fatalf("recall_query was refused unwired: %s", queryRaw)
	}

	get, getRaw := smokeRecallAnswerOf(t, rig, smokeRecallQueryTurn, recall.GetToolName)
	if found, _ := get["found"].(bool); !found {
		t.Fatalf("recall_get found = %v, reason %v, want the original: %s", get["found"], get["reason"], getRaw)
	}
	if got := get["key"]; got != string(smokeRecallChildKey) {
		t.Fatalf("recall_get key = %v, want %s: %s", got, smokeRecallChildKey, getRaw)
	}
	if got := get["body"]; got != smokeRecallOriginal {
		t.Fatalf("recall_get body = %v, want %q: %s", got, smokeRecallOriginal, getRaw)
	}
	if eof, _ := get["eof"].(bool); !eof {
		t.Fatalf("recall_get eof = %v, want true: %s", get["eof"], getRaw)
	}
	parent, ok := get["parent"].(map[string]any)
	if !ok {
		t.Fatalf("recall_get parent = %v, want the parent summary: %s", get["parent"], getRaw)
	}
	if got := parent["key"]; got != string(smokeRecallParentKey) {
		t.Fatalf("recall_get parent key = %v, want %s: %s", got, smokeRecallParentKey, getRaw)
	}
	if got := parent["summary"]; got != smokeRecallParentNote {
		t.Fatalf("recall_get parent summary = %v, want %q: %s", got, smokeRecallParentNote, getRaw)
	}
	if got := smokeRecallLastRequestText(t, rig, smokeRecallQueryTurn); !strings.Contains(got, smokeRecallOriginal) {
		t.Fatalf("the decision after recall_get carried no original text:\n%s", got)
	}
	if got := embeddings.calls.Load(); got == 0 {
		t.Fatal("the scripted embedding endpoint answered no request")
	}

	rig.drive(chat, smokeRecallRecentTurn+" recall recent please")
	rig.waitText(chat, "recall recent count=", 30*time.Second)
	recent, recentRaw := smokeRecallAnswerOf(t, rig, smokeRecallRecentTurn, recall.RecentToolName)
	if got := smokeRecallInt(recent, "count"); got < 2 {
		t.Fatalf("recall_recent count = %d, want at least the two seeded facts (the runtime's own events commit beside them, so the count is a lower bound): %s", got, recentRaw)
	}
	recentItems := smokeRecallItemPage(t, recent)
	childAt, parentAt := -1, -1
	for i, item := range recentItems {
		switch item["key"] {
		case string(smokeRecallChildKey):
			childAt = i
		case string(smokeRecallParentKey):
			parentAt = i
		}
	}
	if childAt < 0 || parentAt < 0 || childAt > parentAt {
		t.Fatalf("recall_recent items = %v, want both seeded facts with the child newer than the parent: %s",
			smokeRecallItemKeys(recentItems), recentRaw)
	}

	rig.drive(chat, smokeRecallTraceTurn+" recall trace please")
	rig.waitText(chat, "recall trace count=2 complete=true", 30*time.Second)
	trace, traceRaw := smokeRecallAnswerOf(t, rig, smokeRecallTraceTurn, recall.TraceToolName)
	if got := smokeRecallInt(trace, "count"); got != 2 {
		t.Fatalf("recall_trace count = %d, want 2: %s", got, traceRaw)
	}
	if complete, _ := trace["complete"].(bool); !complete {
		t.Fatalf("recall_trace complete = %v, want true: %s", trace["complete"], traceRaw)
	}
	if capped, _ := trace["capped"].(bool); capped {
		t.Fatalf("recall_trace capped = true, want false: %s", traceRaw)
	}
	traceItems := smokeRecallItemPage(t, trace)
	if len(traceItems) != 2 || traceItems[0]["key"] != string(smokeRecallChildKey) || traceItems[1]["key"] != string(smokeRecallParentKey) {
		t.Fatalf("recall_trace items = %v, want the child then its parent: %s",
			smokeRecallItemKeys(traceItems), traceRaw)
	}

	rig.drive(chat, smokeRecallMissingTurn+" recall missing get please")
	rig.waitText(chat, "recall missing reason=missing", 30*time.Second)
	missing, missingRaw := smokeRecallAnswerOf(t, rig, smokeRecallMissingTurn, recall.GetToolName)
	if found, _ := missing["found"].(bool); found {
		t.Fatalf("recall_get(%s) found = true, want a named miss: %s", smokeRecallMissingKey, missingRaw)
	}
	if got := missing["reason"]; got != "missing" {
		t.Fatalf("recall_get(%s) reason = %v, want missing: %s", smokeRecallMissingKey, got, missingRaw)
	}
	if body, _ := missing["body"].(string); body != "" {
		t.Fatalf("recall_get(%s) body = %q, want empty: %s", smokeRecallMissingKey, body, missingRaw)
	}

	t.Logf("[wave2-recall] query -> get: %s answered %q in one page (eof)",
		smokeRecallChildKey, smokeRecallOriginal)
	t.Logf("[wave2-recall] recent: 2 events newest first; trace: child -> parent complete")
	t.Logf("[wave2-recall] negative anchor: recall_get(%s) answered reason=missing with no body", smokeRecallMissingKey)
}

// smokeRecallEmbeddings is the scripted embedding endpoint the declared engine calls.
type smokeRecallEmbeddings struct {
	server *httptest.Server
	calls  atomic.Int64
}

// newSmokeRecallEmbeddings starts the scripted embedding endpoint and closes it with the test.
func newSmokeRecallEmbeddings(t *testing.T) *smokeRecallEmbeddings {
	t.Helper()
	edge := &smokeRecallEmbeddings{}
	edge.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/embeddings") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		edge.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[%s]}],"model":"smoke-embedding","usage":{"prompt_tokens":1,"total_tokens":1}}`, smokeRecallVector)
	}))
	t.Cleanup(edge.server.Close)
	return edge
}

// endpoint reports the base URL the engine declaration names.
func (e *smokeRecallEmbeddings) endpoint() string { return e.server.URL + "/v1/" }

// smokeRecallDocument renders the host document: one entry agent with the four recall tools and a localfile memory tier.
func smokeRecallDocument(c *smokeConfig, embeddingEndpoint string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "entry: %s\n", smokeSessionTag)
	b.WriteString("provider: local\n")
	fmt.Fprintf(&b, "model: %s\n", c.Model)
	fmt.Fprintf(&b, "prompt_dir: %s\n", c.PromptDir)
	b.WriteString("request_timeout_seconds: 60\n")
	fmt.Fprintf(&b, "providers:\n  local: { provider: openai, api_endpoint: %q, api_key_env: SMOKE_API_KEY }\n", c.Endpoint)
	fmt.Fprintf(&b, "agents:\n  %s:\n    model: %s\n", smokeSessionTag, c.Model)
	b.WriteString("    tools:\n")
	for _, name := range []string{recall.QueryToolName, recall.GetToolName, recall.RecentToolName, recall.TraceToolName} {
		fmt.Fprintf(&b, "      - { kind: tool, id: %s }\n", name)
	}
	b.WriteString("    max_tokens: 2000\n")
	b.WriteString("    compress_threshold: 0.3\n")
	b.WriteString("    keep_recent_tasks: 1\n")
	b.WriteString("    max_tool_iterations: 3\n")
	b.WriteString("    memory:\n      type: localfile\n")
	fmt.Fprintf(&b, "      path: %s\n", c.StorageRoot)
	b.WriteString("      engine:\n        backend: memory\n        embedding:\n")
	b.WriteString("          provider: openai\n          model: smoke-embedding\n          api_key_env: SMOKE_API_KEY\n")
	fmt.Fprintf(&b, "          dimensions: %d\n", smokeRecallDimensions)
	fmt.Fprintf(&b, "          api_endpoint: %q\n", embeddingEndpoint)
	b.WriteString("app:\n  wechat:\n")
	fmt.Fprintf(&b, "    config_dir: %s\n", c.ConfigDir)
	fmt.Fprintf(&b, "    token_file: %s\n", filepath.Join(c.ConfigDir, "token.json"))
	fmt.Fprintf(&b, "    context_token_dir: %s\n", c.ContextDir)
	fmt.Fprintf(&b, "    workspace_dir: %s\n", c.WorkspaceDir)
	return b.String()
}

// smokeRecallSeed commits the fixed parent and child events into the tier before Boot observes it.
func smokeRecallSeed(t *testing.T, root string) {
	t.Helper()
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("open the seed tier: %v", err)
	}
	owner := event.Address{AgentID: event.AgentID(smokeSessionTag), SessionID: event.SessionID(smokeChats[0])}
	partition := memory.PartitionIDFromName(smokeSessionTag)
	seeds := []struct {
		key      event.EventID
		kind     string
		summary  string
		content  string
		original string
		at       int64
	}{
		{smokeRecallParentKey, event.TypeExternalInput, smokeRecallParentNote, smokeRecallParentText, "SMOKE-RECALL-PARENT-ORIGINAL signing decision recorded", 1000},
		{smokeRecallChildKey, event.TypeAgentOutput, smokeRecallChildNote, smokeRecallChildText, smokeRecallOriginal, 2000},
	}
	for _, seed := range seeds {
		fact := event.Event{
			Key:       seed.key,
			Type:      seed.kind,
			Timestamp: time.UnixMilli(seed.at),
			Meta:      map[string]string{event.MetaKeyEventSummary: seed.summary},
			Message:   tagentmodel.Message{Role: tagentmodel.RoleUser, Content: seed.content},
			Owner:     owner,
		}
		body, err := memory.BodyFromEvent(fact, smokeRecallFormat, smokeRecallRetention)
		if err != nil {
			t.Fatalf("derive the body of %s: %v", seed.key, err)
		}
		encoded, err := body.Encode()
		if err != nil {
			t.Fatalf("encode the body of %s: %v", seed.key, err)
		}
		if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{
			PartitionID: partition,
			WindowTS:    seed.at,
			Seq:         0,
			EventKey:    seed.key,
			EventTime:   seed.at,
			Body:        encoded,
		}); err != nil {
			t.Fatalf("store the event %s: %v", seed.key, err)
		}
		if err := tier.Originals.Put(ctx, body.Ref(), []byte(seed.original)); err != nil {
			t.Fatalf("store the original %s: %v", seed.key, err)
		}
	}
	relations, err := replay.OpenMemoryRelations(filepath.Join(root, "relations"))
	if err != nil {
		t.Fatalf("open the seed relations: %v", err)
	}
	if err := relations.SetParent(smokeRecallChildKey, smokeRecallParentKey); err != nil {
		t.Fatalf("link %s under %s: %v", smokeRecallChildKey, smokeRecallParentKey, err)
	}
}

// smokeRecallScript answers the entry model with the fixed call sequence of every recall turn.
func smokeRecallScript(provider *smokeProvider) {
	provider.hook = func(w http.ResponseWriter, _ int, modelName string, messages []smokeModelMessage) bool {
		if modelName != smokeEntryModel {
			return false
		}
		switch smokeMarkerIn(messages) {
		case smokeRecallQueryTurn:
			return smokeRecallDriveQuery(w, messages)
		case smokeRecallRecentTurn:
			return smokeRecallDriveRecent(w, messages)
		case smokeRecallTraceTurn:
			return smokeRecallDriveTrace(w, messages)
		case smokeRecallMissingTurn:
			return smokeRecallDriveMissing(w, messages)
		}
		return false
	}
}

// smokeRecallDriveQuery answers the retrieval call, then the exact read of the key the page answered, then the final text.
func smokeRecallDriveQuery(w http.ResponseWriter, messages []smokeModelMessage) bool {
	if _, _, held := smokeRecallToolAnswer(messages, recall.QueryToolName); !held {
		fmt.Fprint(w, smokeToolCallBody("call-recall-query-r600", recall.QueryToolName,
			`{"text":"`+smokeRecallNeedle+`","limit":5}`))
		return true
	}
	answer, _, held := smokeRecallToolAnswer(messages, recall.GetToolName)
	if !held {
		fmt.Fprint(w, smokeToolCallBody("call-recall-get-r600", recall.GetToolName,
			`{"key":"`+string(smokeRecallChildKey)+`","include_parent":true}`))
		return true
	}
	body, _ := answer["body"].(string)
	fmt.Fprint(w, smokeAnswerBody("recall get body "+body))
	return true
}

// smokeRecallDriveRecent answers the recent page call, then the final text carrying its count.
func smokeRecallDriveRecent(w http.ResponseWriter, messages []smokeModelMessage) bool {
	answer, _, held := smokeRecallToolAnswer(messages, recall.RecentToolName)
	if !held {
		fmt.Fprint(w, smokeToolCallBody("call-recall-recent-r601", recall.RecentToolName, `{"limit":12}`))
		return true
	}
	fmt.Fprint(w, smokeAnswerBody(fmt.Sprintf("recall recent count=%d", smokeRecallInt(answer, "count"))))
	return true
}

// smokeRecallDriveTrace answers the causal walk call, then the final text carrying its count and completeness.
func smokeRecallDriveTrace(w http.ResponseWriter, messages []smokeModelMessage) bool {
	answer, _, held := smokeRecallToolAnswer(messages, recall.TraceToolName)
	if !held {
		fmt.Fprint(w, smokeToolCallBody("call-recall-trace-r602", recall.TraceToolName,
			`{"key":"`+string(smokeRecallChildKey)+`","max_steps":5}`))
		return true
	}
	fmt.Fprint(w, smokeAnswerBody(fmt.Sprintf("recall trace count=%d complete=%t",
		smokeRecallInt(answer, "count"), smokeRecallBool(answer, "complete"))))
	return true
}

// smokeRecallDriveMissing answers the exact read of a key nothing holds, then the final text carrying the named reason.
func smokeRecallDriveMissing(w http.ResponseWriter, messages []smokeModelMessage) bool {
	answer, _, held := smokeRecallToolAnswer(messages, recall.GetToolName)
	if !held {
		fmt.Fprint(w, smokeToolCallBody("call-recall-missing-r603", recall.GetToolName,
			`{"key":"`+smokeRecallMissingKey+`"}`))
		return true
	}
	fmt.Fprint(w, smokeAnswerBody("recall missing reason="+smokeRecallString(answer, "reason")))
	return true
}

// smokeRecallToolAnswer returns the decoded tool result the current turn carries, if any.
func smokeRecallToolAnswer(messages []smokeModelMessage, tool string) (map[string]any, string, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == string(tagentmodel.RoleUser) && smokeMarkerRE.MatchString(msg.Content) {
			return nil, "", false
		}
		var answer map[string]any
		if err := json.Unmarshal([]byte(msg.Content), &answer); err != nil {
			continue
		}
		if name, _ := answer["tool"].(string); name == tool {
			return answer, msg.Content, true
		}
	}
	return nil, "", false
}

// smokeRecallAnswerOf returns the last answer of one tool that reached the model in the named turn.
func smokeRecallAnswerOf(t *testing.T, rig *smokeRig, marker, tool string) (map[string]any, string) {
	t.Helper()
	var answer map[string]any
	var raw string
	for _, req := range rig.provider.recorded() {
		if req.Model != smokeEntryModel || smokeMarkerIn(req.Messages) != marker {
			continue
		}
		if got, text, held := smokeRecallToolAnswer(req.Messages, tool); held {
			answer, raw = got, text
		}
	}
	if answer == nil {
		t.Fatalf("%s: no %s answer reached the model (requests=%d)\nrecent requests:\n%s",
			marker, tool, rig.provider.count(), rig.recentRequests(8))
	}
	if strings.Contains(raw, "wires no read face") {
		t.Fatalf("%s: %s was refused unwired: %s", marker, tool, raw)
	}
	return answer, raw
}

// smokeRecallLastRequestText returns the text of the last entry-model request of the named turn.
func smokeRecallLastRequestText(t *testing.T, rig *smokeRig, marker string) string {
	t.Helper()
	var text string
	for _, req := range rig.provider.recorded() {
		if req.Model != smokeEntryModel || smokeMarkerIn(req.Messages) != marker {
			continue
		}
		text = req.text()
	}
	if text == "" {
		t.Fatalf("%s: no entry-model request was recorded", marker)
	}
	return text
}

// smokeRecallItemPage reads the items page of one answer.
func smokeRecallItemPage(t *testing.T, answer map[string]any) []map[string]any {
	t.Helper()
	raw, ok := answer["items"].([]any)
	if !ok {
		t.Fatalf("answer carries no items page: %v", answer)
	}
	items := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		item, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("answer item = %v, want one event rendering", entry)
		}
		items = append(items, item)
	}
	return items
}

// smokeRecallItemOf finds the rendered event under one key.
func smokeRecallItemOf(items []map[string]any, key event.EventID) (map[string]any, bool) {
	for _, item := range items {
		if item["key"] == string(key) {
			return item, true
		}
	}
	return nil, false
}

// smokeRecallItemKeys renders the keys of a page for failure diagnostics.
func smokeRecallItemKeys(items []map[string]any) []any {
	keys := make([]any, 0, len(items))
	for _, item := range items {
		keys = append(keys, item["key"])
	}
	return keys
}

// smokeRecallInt reads one numeric answer field.
func smokeRecallInt(answer map[string]any, field string) int {
	value, _ := answer[field].(float64)
	return int(value)
}

// smokeRecallBool reads one boolean answer field.
func smokeRecallBool(answer map[string]any, field string) bool {
	value, _ := answer[field].(bool)
	return value
}

// smokeRecallString reads one string answer field.
func smokeRecallString(answer map[string]any, field string) string {
	value, _ := answer[field].(string)
	return value
}
