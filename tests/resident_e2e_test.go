package tagent_test

// RESIDENT 30-ROUND E2E (resident-remaining-hardening 3.2, archived 7.2):
// per-accepted-ID reconciliation across the FULL chain
//
//	receipt → store → projection → actual model request → recall → host delivery
//
// plus the view-honesty surfaces: Content ≠ Summary rendering (a folded card
// keeps only the first line — the tail survives ONLY through store recall),
// no-anchor → anchor transition of the rolling summary, a REAL restart whose
// rebuild outcome (full/partial) is asserted honestly, and TTL expiry made
// invisible through a synchronous lifecycle sweep. Default suite, seconds.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
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
	// fsync stays at its DEFAULT (on); the compress budget is tiny on purpose
	// so 30 rounds genuinely trigger the compaction/anchor path.
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

	// ---- phase 1: 30 rounds, per-accepted-ID reconciliation ----------------
	receiptIDs := make(map[string]bool, e2eRounds)
	firstLines := make([]string, e2eRounds)
	for r := 0; r < e2eRounds; r++ {
		first := fmt.Sprintf("e2e-%02d first-segment %s", r, strings.Repeat("data", 30))
		tail := fmt.Sprintf("e2e-%02d TAIL-SEGMENT-UNIQUE", r) // second line: dies out of the card view, lives in the store
		firstLines[r] = first
		body := first + "\n" + tail

		rec, err := ta.InjectMessageContext(context.Background(), "user", model.Message{Role: model.RoleUser, Content: body})
		require.NoError(t, err, "round %d acceptance", r)
		require.NotEmpty(t, rec.RequestID)
		require.False(t, receiptIDs[rec.RequestID], "duplicate accepted request id %q", rec.RequestID)
		receiptIDs[rec.RequestID] = true

		waitReq(t, m, r, first) // projection → ACTUAL model request (round r drives request #r; no bootstrap turn)
	}

	// ---- host delivery gate: every output carries user lineage --------------
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

	// ---- view honesty: no-anchor first request vs anchored latest ----------
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

	// ---- store side: Content ≠ Summary, full text recallable by key --------
	pid := memory.PartitionIDFromName("tagent")
	refs, err := ta.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: "e2e-00 TAIL-SEGMENT-UNIQUE", Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, refs, "the folded-away tail line must still be findable in the fact chain")
	evt, err := ta.MemStore().GetEvent(refs[0].EventKey)
	require.NoError(t, err)
	require.Contains(t, evt.Content, "e2e-00 TAIL-SEGMENT-UNIQUE", "recall of the ORIGINAL event must return the full multi-line Content")
	require.NotEqual(t, evt.Content, firstLines[0], "sanity: stored Content is the whole two-line body, not the card-visible first line")

	require.NoError(t, ta.Close()) // StopLoop + flush barrier; closes the out channel
	<-consumerDone

	// ---- phase 2: REAL restart → honest rebuild outcome --------------------
	m2 := &e2eModel{}
	ta2 := e2eAgent(t, dir, m2)
	rec := ta2.RecoveryResult()
	require.NotNil(t, rec, "startup must record a rebuild outcome")
	require.NotEqual(t, "failed", rec.Status, "rebuild must not swallow an unreadable chain: %+v", rec)
	require.Contains(t, []string{"full", "partial"}, rec.Status, "status must be an honest verdict: %+v", rec)
	if rec.Status == "partial" {
		// partial must self-report WHERE it lost ground — never claim full.
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
	// The pre-restart chain is still recallable through the reopened store.
	refs, err = ta2.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: "e2e-15 first-segment", Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, refs, "round-15 must survive the process restart in the fact chain")

	// ---- phase 3: TTL expiry through the real lifecycle sweep --------------
	fs, ok := ta2.MemStore().(*memory.FileSegmentStore)
	require.True(t, ok, "e2e must reach the bare FileSegmentStore for the lifecycle hook")
	require.NotNil(t, fs.Lifecycle(), "wiring must inject the lifecycle manager")
	oldTs := time.Now().Add(-31 * 24 * time.Hour).UnixMilli() // external_input TTL = 30d
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
