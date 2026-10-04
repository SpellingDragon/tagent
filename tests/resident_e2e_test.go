package tagent_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const e2eRounds = 30

// e2eModel records every request it is shown and answers one productive turn.
type e2eModel struct {
	mu   sync.Mutex
	n    int
	reqs [][]model.Message
}

func (m *e2eModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.n++
	snap := make([]model.Message, len(req.Messages))
	copy(snap, req.Messages)
	m.reqs = append(m.reqs, snap)
	n := m.n
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: fmt.Sprintf("ack-%d\nsecond line %d", n, n),
	}}}}
	close(ch)
	return ch, nil
}

func (m *e2eModel) Info() model.Info { return model.Info{Name: "e2e-model"} }

func (m *e2eModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.reqs)
}

func (m *e2eModel) contains(idx int, sub string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if idx >= len(m.reqs) {
		return false
	}
	for _, msg := range m.reqs[idx] {
		if strings.Contains(msg.Content, sub) {
			return true
		}
	}
	return false
}

func (m *e2eModel) latestText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.reqs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, msg := range m.reqs[len(m.reqs)-1] {
		b.WriteString(msg.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func e2eAgent(t *testing.T, dir string, m *e2eModel) *agent.TagentAgent {
	t.Helper()
	ta, err := tagent.New(tagent.Config{
		Entry: "tagent",
		Agents: map[string]tagent.AgentConfig{
			"tagent": {
				SystemPrompt:      tagent.PromptConfig{Inline: "e2e agent"},
				MaxTokens:         400,
				CompressThreshold: 0.8,
				KeepRecentTasks:   2,
				Memory:            tagent.MemoryConfig{Type: "localfile", Path: dir},
			},
		},
	}, tagent.WithModel(m))
	require.NoError(t, err)
	return ta
}

// waitReq polls until request #idx exists and contains sub.
func waitReq(t *testing.T, m *e2eModel, idx int, sub string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if m.contains(idx, sub) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("request #%d never contained %q (requests so far: %d)", idx, sub, m.count())
}

func TestResidentE2E_30RoundDeliveryChain(t *testing.T) {
	dir := t.TempDir()
	m := &e2eModel{}
	ta := e2eAgent(t, dir, m)

	out, err := ta.StartLoop("e2e-user", "e2e-session")
	require.NoError(t, err)

	var mu sync.Mutex
	var sources []string
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for evt := range out {
			mu.Lock()
			if v, ok := evt.StateDelta["trigger_source"]; ok {
				sources = append(sources, fmt.Sprintf("%s", v))
			}
			mu.Unlock()
		}
	}()

	receiptIDs := make(map[string]bool, e2eRounds)
	firstLines := make([]string, e2eRounds)
	for r := 0; r < e2eRounds; r++ {
		first := fmt.Sprintf("e2e-%02d first-segment %s", r, strings.Repeat("data", 30))
		tail := fmt.Sprintf("e2e-%02d TAIL-SEGMENT-UNIQUE", r)
		firstLines[r] = first
		body := first + "\n" + tail

		rec, err := ta.InjectMessageContext(context.Background(), "user", model.Message{Role: model.RoleUser, Content: body})
		require.NoError(t, err, "round %d acceptance", r)
		require.NotEmpty(t, rec.RequestID)
		require.False(t, receiptIDs[rec.RequestID], "duplicate accepted request id %q", rec.RequestID)
		receiptIDs[rec.RequestID] = true

		waitReq(t, m, r, first)
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		n := len(sources)
		mu.Unlock()
		if n >= e2eRounds {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery gate: only %d/%d output events observed", n, e2eRounds)
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	for i, s := range sources[:e2eRounds] {
		require.Equal(t, "user", s, "output %d must carry user lineage for delivery", i)
	}
	mu.Unlock()

	require.NotContains(t, func() string {
		var b strings.Builder
		for _, msg := range firstRequest(t, m) {
			b.WriteString(msg.Content)
		}
		return b.String()
	}(), "〔历史归档〕", "the first request must run without any compaction anchor")
	latest := m.latestText()
	require.Contains(t, latest, "〔历史归档〕", "30 rounds at budget 400 must fold the oldest turns into the rolling anchor")
	require.NotContains(t, latest, "e2e-00 TAIL-SEGMENT-UNIQUE",
		"a folded card keeps ONLY its first line — the second line must leave the projection view")

	pid := memory.PartitionIDFromName("tagent")
	refs, err := ta.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: "e2e-00 TAIL-SEGMENT-UNIQUE", Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, refs, "the folded-away tail line must still be findable in the fact chain")
	evt, err := ta.MemStore().GetEvent(refs[0].EventKey)
	require.NoError(t, err)
	require.Contains(t, evt.Content, "e2e-00 TAIL-SEGMENT-UNIQUE", "recall of the ORIGINAL event must return the full multi-line Content")
	require.NotEqual(t, evt.Content, firstLines[0], "sanity: stored Content is the whole two-line body, not the card-visible first line")

	require.NoError(t, ta.Close())
	<-consumerDone

	m2 := &e2eModel{}
	ta2 := e2eAgent(t, dir, m2)
	rec := ta2.RecoveryResult()
	require.NotNil(t, rec, "startup must record a rebuild outcome")
	require.NotEqual(t, "failed", rec.Status, "rebuild must not swallow an unreadable chain: %+v", rec)
	require.Contains(t, []string{"full", "partial"}, rec.Status, "status must be an honest verdict: %+v", rec)
	if rec.Status == "partial" {
		require.Positive(t, rec.Truncated+len(rec.MissingKeys)+rec.PagesFailed+rec.BatchErrors+rec.PayloadErrors,
			"partial without any loss counters would be a mislabeled full: %+v", rec)
	}
	out2, err := ta2.StartLoop("e2e-user", "e2e-session")
	require.NoError(t, err)
	dropped := make(chan struct{})
	go func() {
		defer close(dropped)
		for range out2 {
		}
	}()

	_, err = ta2.InjectMessageContext(context.Background(), "user", model.Message{Role: model.RoleUser, Content: "post-restart-round"})
	require.NoError(t, err)
	waitReq(t, m2, 0, "post-restart-round")
	refs, err = ta2.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: "e2e-15 first-segment", Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, refs, "round-15 must survive the process restart in the fact chain")

	fs, ok := ta2.MemStore().(*memory.FileSegmentStore)
	require.True(t, ok, "e2e must reach the bare FileSegmentStore for the lifecycle hook")
	require.NotNil(t, fs.Lifecycle(), "wiring must inject the lifecycle manager")
	oldTs := time.Now().Add(-31 * 24 * time.Hour).UnixMilli()
	oldKey := memory.NewSnowflakeEventKey(1, oldTs)
	require.NoError(t, fs.StoreEvent(oldKey, memory.FullEvent{
		EventKey: oldKey, PartitionID: pid, EventType: "external_input",
		EventSummary: "ttl-victim-9x7", Content: "ttl-victim-9x7", Timestamp: oldTs,
	}))
	refs, err = fs.QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: "ttl-victim-9x7", Limit: 5})
	require.NoError(t, err)
	require.NotEmpty(t, refs, "the aged event must be visible before the sweep")

	fs.Lifecycle().SweepOnce()

	refs, err = fs.QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: "ttl-victim-9x7", Limit: 5})
	require.NoError(t, err)
	require.Empty(t, refs, "TTL-expired event must be tombstoned out of queries after the sweep")

	require.NoError(t, ta2.Close())
	<-dropped
}

// firstRequest returns the very first model request the loop made.
func firstRequest(t *testing.T, m *e2eModel) []model.Message {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	require.NotEmpty(t, m.reqs, "expected at least one request")
	return m.reqs[0]
}

// durableE2EAgent wires the full composition root with BOTH durable surfaces
// on: localfile store + reliable bus (inbox-v2 spill), entry name "tagent".
func durableE2EAgent(t *testing.T, storeDir, spillDir string, m *e2eModel) *agent.TagentAgent {
	t.Helper()
	ta, err := tagent.New(tagent.Config{
		Entry: "tagent",
		Agents: map[string]tagent.AgentConfig{
			"tagent": {
				SystemPrompt:      tagent.PromptConfig{Inline: "durable e2e agent"},
				MaxTokens:         4000,
				CompressThreshold: 0.8,
				Memory:            tagent.MemoryConfig{Type: "localfile", Path: storeDir},
			},
		},
		Reliability: tagent.ReliabilityConfig{BusSpillDir: spillDir},
	}, tagent.WithModel(m))
	require.NoError(t, err)
	return ta
}

// TestResidentDurableE2E_FiveSurfaceReconciliation 钉住 同一输入在受理态、请求身份、输出血统、事实链可召回与事件类型五个面互相对账，绝不以前态冒充后态。
//
// 契约: docs/wiki/platform/platform-subsystems.md#reliability-switches
func TestResidentDurableE2E_FiveSurfaceReconciliation(t *testing.T) {
	root := t.TempDir()
	storeDir, spillDir := filepath.Join(root, "store"), filepath.Join(root, "spill")
	m := &e2eModel{}
	ta := durableE2EAgent(t, storeDir, spillDir, m)

	out, err := ta.StartLoop("du", "dur-session")
	require.NoError(t, err)
	var mu sync.Mutex
	var sources []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range out {
			mu.Lock()
			if v, ok := evt.StateDelta["trigger_source"]; ok {
				sources = append(sources, fmt.Sprintf("%s", v))
			}
			mu.Unlock()
		}
	}()

	msgA := model.Message{Role: model.RoleUser, Content: "dur-A first line"}
	msgB := model.Message{Role: model.RoleUser, Content: "dur-B second input"}
	msgC := model.Message{Role: model.RoleUser, Content: "dur-C later single"}

	reqAB, durableAB, err := ta.InjectEnvelope(context.Background(), "user", []model.Message{msgA, msgB})
	require.NoError(t, err)
	require.True(t, durableAB, "with BusSpillDir set, acceptance MUST be durable (inbox-v2)")
	require.NotEmpty(t, reqAB)
	waitReq(t, m, 0, "dur-A first line")
	require.True(t, m.contains(0, "dur-B second input"),
		"A+B must merge into ONE business turn (batch slots are not compacted away)")

	recC, err := ta.InjectMessageContext(context.Background(), "user", msgC)
	require.NoError(t, err)
	require.True(t, recC.Durable)
	require.NotEmpty(t, recC.RequestID)
	require.NotEqual(t, reqAB, recC.RequestID, "each acceptance keeps its own stable identity")

	waitReq(t, m, 1, "dur-C later single")

	deadline := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		n := len(sources)
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery: only %d/2 turn outputs observed", n)
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	for i, s := range sources[:2] {
		require.Equal(t, "user", s, "output %d carries user lineage (sources=%v)", i, sources)
	}
	mu.Unlock()

	pid := memory.PartitionIDFromName("tagent")
	store := ta.MemStore()
	for _, body := range []string{"dur-A first line", "dur-B second input", "dur-C later single"} {
		refs, qerr := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: body, Limit: 10})
		require.NoError(t, qerr)
		require.NotEmpty(t, refs, "the ORIGINAL input %q must be recallable from the fact chain", body)
		full, gerr := store.GetEvent(refs[0].EventKey)
		require.NoError(t, gerr)
		require.Equal(t, tagentevent.TypeExternalInput, full.EventType)
		require.Contains(t, full.Content, body, "stored content is the original, not a digest")
	}

	receiptCount := func() int {
		refs, qerr := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Limit: 200})
		if qerr != nil {
			return -1
		}
		n := 0
		for _, r := range refs {
			if r.EventType == tagentevent.TypeInboxReceipt {
				n++
			}
		}
		return n
	}
	deadline = time.Now().Add(20 * time.Second)
	for receiptCount() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("expected 2 inbox receipts (A+B turn, C turn), got %d", receiptCount())
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.Equal(t, 2, receiptCount(), "exactly one receipt per business turn — no per-envelope double receipt")

	require.NoError(t, ta.Close())

	inbox, err := reliability.NewInbox(filepath.Join(spillDir, "tagent"), 0)
	require.NoError(t, err)
	var outstanding []reliability.OutstandingEnvelope
	require.Eventually(t, func() bool {
		outstanding, err = inbox.Outstanding()
		return err == nil && len(outstanding) == 0
	}, 20*time.Second, 20*time.Millisecond,
		"after ack every envelope is unlinked — accepted IDs are all in the processed-cleaned state")
	entries, err := os.ReadDir(filepath.Join(spillDir, "tagent"))
	require.NoError(t, err)
	envLeft := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			envLeft++
		}
	}
	require.Equal(t, 0, envLeft, "no envelope residue in the inbox directory")
	<-done
}
