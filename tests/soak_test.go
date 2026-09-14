//go:build soak

// SOAK TEST SKELETON (implementation-hardening 8.3) — the first evidence
// generator for the headline promise "连续运行数天而不失忆". NOT part of the
// default suite: build with `-tags soak`; CI runs it via manual dispatch.
// Parameterized:
//
//	go test ./tests/ -tags soak -run TestSoak_Continuity -count=1 \
//	  -args -rounds=50 -events-per-round=30
//
// Each round = a full write/verify lifecycle over the SAME on-disk memory:
//   - write phase: fresh agent (localfile) injects its round's events, Close
//     (final flush = durability barrier)
//   - verify phase: FRESH agent rebuilds from the fact chain and asserts the
//     ROUND-0 marker is still recallable — "不失忆" after N rounds + N restarts.
package tagent_test

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

var (
	soakRounds         = flag.Int("rounds", 20, "soak lifecycle rounds (write+verify each)")
	soakEventsPerRound = flag.Int("events-per-round", 30, "injected events per round")
)

func soakAgent(t *testing.T, dir string) *agent.TagentAgent {
	t.Helper()
	ta, err := tagent.New(tagent.Config{
		Entry: "tagent",
		Agents: map[string]tagent.AgentConfig{
			"tagent": {
				SystemPrompt: tagent.PromptConfig{Inline: "soak agent"},
				Memory: tagent.MemoryConfig{
					Type:  "localfile",
					Path:  dir,
					FSync: boolPtr(false), // throughput: durability covered by WP2 tests
				},
			},
		},
	}, tagent.WithModel(soakModel{}))
	require.NoError(t, err)
	return ta
}

func soakWriteRound(t *testing.T, dir string, round int) {
	t.Helper()
	ta := soakAgent(t, dir)

	out, err := ta.StartLoop("soak", "soak-session")
	require.NoError(t, err)
	_ = out

	tail := fmt.Sprintf("round-%02d event %03d", round, *soakEventsPerRound-1) // merged-batch tail: proves the WHOLE batch stored
	round0 := "round-0-origin-marker"
	for i := 0; i < *soakEventsPerRound; i++ {
		body := fmt.Sprintf("round-%02d event %03d", round, i)
		if round == 0 && i == 0 {
			body = round0
		}
		ta.InjectMessage(model.Message{Role: model.RoleUser, Content: body})
	}

	// Wait until every round event is STORED (the persistent loop + plugin
	// pipeline are asynchronous — durability barrier needs the store, not the
	// inject call).
	deadline := time.Now().Add(60 * time.Second)
	for {
		pid := memory.PartitionIDFromName("tagent")
		refs, qerr := ta.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: tail, Limit: 10})
		require.NoError(t, qerr)
		if len(refs) >= 1 {
			break // batch tail stored → the whole merged batch is durable
		}
		if time.Now().After(deadline) {
			t.Fatalf("batch tail %q not stored within deadline (stored=%d)", tail, len(refs))
		}
		time.Sleep(50 * time.Millisecond)
	}

	require.NoError(t, ta.Close()) // StopLoop + final flush = durability barrier
}

func soakVerify(t *testing.T, dir, needle string, round int) {
	t.Helper()
	ta := soakAgent(t, dir)
	defer ta.Close()
	pid := memory.PartitionIDFromName("tagent")
	refs, err := ta.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: needle, Limit: 50})
	require.NoError(t, err)
	var got strings.Builder
	for _, ref := range refs {
		got.WriteString(ref.EventSummary)
		got.WriteString("\n")
	}
	require.Contains(t, got.String(), needle,
		"memory loss after %d rounds + restarts — the headline promise broke", round)
}

func TestSoak_Continuity(t *testing.T) {
	rounds := *soakRounds
	require.GreaterOrEqual(t, rounds, 2, "soak needs >= 2 rounds (write + survive-restart)")

	dir := t.TempDir()
	const origin = "round-0-origin-marker"

	// Round 0 writes the origin marker.
	soakWriteRound(t, dir, 0)

	for r := 1; r < rounds; r++ {
		t.Run(fmt.Sprintf("round-%02d", r), func(t *testing.T) {
			soakVerify(t, dir, origin, r) // restart + recall: round-0 must survive
			soakWriteRound(t, dir, r)     // then add this round's events
		})
	}
	// Final verify after the last write round.
	soakVerify(t, dir, origin, rounds)
}

func boolPtr(b bool) *bool { return &b }

// soakModel returns a productive final response every call — empty responses
// trip the unproductive-turn retry path and starve the inject queue.
type soakModel struct{}

func (soakModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: "ack",
	}}}}
	close(ch)
	return ch, nil
}

func (soakModel) Info() model.Info { return model.Info{Name: "soak-model"} }
