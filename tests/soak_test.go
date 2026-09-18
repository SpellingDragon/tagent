//go:build soak

// SOAK TEST — subprocess-per-phase continuity evidence (resident-remaining-
// hardening 3.1, archived 7.1). Refactored away from the old in-process
// skeleton: every write and verify phase now runs in a FRESH OS process
// (re-execing this very test binary), so "restart" is a real process
// death + reopen — no shared registry, no shared caches, no goroutine
// inheritance between rounds. Durability is the default setting (real
// localfile + fsync on), and every write phase forces at least one REAL
// segment compaction (seal → L1→L2 sweep via the harness hook) whose
// artifacts the next verify process must read through.
//
// The parent additionally checks that every phase ran under a DIFFERENT pid
// (process identity crossing is the evidence that recovery instances are
// genuinely fresh, not the same heap re-entered).
//
// Not part of the default suite: build with `-tags soak`; CI runs via manual
// dispatch:
//
//	go test ./tests/ -tags soak -run TestSoak_Continuity -count=1 \
//	  -args -rounds=50 -events-per-round=30
package tagent_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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

const (
	soakOrigin = "round-0-origin-marker"
	soakPidTag = "SOAK_PID="
)

// --- parent orchestration ---------------------------------------------------

func TestSoak_Continuity(t *testing.T) {
	rounds := *soakRounds
	require.GreaterOrEqual(t, rounds, 2, "soak needs >= 2 rounds (write + survive-restart)")

	dir := t.TempDir()
	var seenPids []int

	runPhase := func(mode string, round int) {
		pid := soakChild(t, dir, mode, round)
		for _, p := range seenPids {
			require.NotEqual(t, p, pid, "phase %s/%d reused process id %d — recovery instance must be fresh", mode, round, pid)
		}
		seenPids = append(seenPids, pid)
		require.NotEqual(t, os.Getpid(), pid, "a phase must never run in the orchestrating process")
	}

	runPhase("write", 0)
	for r := 1; r < rounds; r++ {
		t.Run(fmt.Sprintf("round-%02d", r), func(t *testing.T) {
			pid := soakChild(t, dir, "verify", r)
			for _, p := range seenPids {
				require.NotEqual(t, p, pid, "verify reused process id %d", pid)
			}
			seenPids = append(seenPids, pid)
			runPhase("write", r)
		})
	}
	runPhase("verify", rounds)
	t.Logf("soak continuity: %d rounds, %d distinct processes (events/round=%d)", rounds*2, len(seenPids), *soakEventsPerRound)
}

// soakChild re-execs this test binary for one phase and returns the child pid.
func soakChild(t *testing.T, dir, mode string, round int) int {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err, "resolve own test binary for re-exec")

	cmd := exec.Command(exe,
		"-test.run=TestSoakChildPhase$",
		"-test.timeout=10m",
	)
	cmd.Env = append(os.Environ(),
		"SOAK_CHILD=1",
		"SOAK_DIR="+dir,
		"SOAK_MODE="+mode,
		"SOAK_ROUND="+strconv.Itoa(round),
		"SOAK_EVENTS="+strconv.Itoa(*soakEventsPerRound),
	)
	out, err := cmd.CombinedOutput()
	stdout := string(out)
	if err != nil {
		t.Fatalf("soak child %s/%d failed: %v\n--- child output ---\n%s", mode, round, err, stdout)
	}
	pidLine := ""
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, soakPidTag) {
			pidLine = line
		}
	}
	require.NotEmpty(t, pidLine, "child %s/%d must report its pid\n--- output ---\n%s", mode, round, stdout)
	pid, err := strconv.Atoi(strings.TrimPrefix(pidLine, soakPidTag))
	require.NoError(t, err)
	t.Logf("phase %s/%d completed in child pid=%d (%d bytes output)", mode, round, pid, len(stdout))
	return pid
}

// --- child phase (selected via SOAK_CHILD + -test.run=TestSoakChildPhase) ----

func TestSoakChildPhase(t *testing.T) {
	if os.Getenv("SOAK_CHILD") != "1" {
		t.Skip("soak child phase: only runs when re-executed by TestSoak_Continuity")
	}
	fmt.Printf("%s%d\n", soakPidTag, os.Getpid())

	dir := os.Getenv("SOAK_DIR")
	mode := os.Getenv("SOAK_MODE")
	round, err := strconv.Atoi(os.Getenv("SOAK_ROUND"))
	require.NoError(t, err)
	events, err := strconv.Atoi(os.Getenv("SOAK_EVENTS"))
	require.NoError(t, err)
	require.NotEmpty(t, dir, "SOAK_DIR required")

	ta := soakAgent(t, dir)
	switch mode {
	case "write":
		soakWritePhase(t, ta, round, events)
	case "verify":
		soakVerifyPhase(t, ta, round)
	default:
		t.Fatalf("unknown SOAK_MODE %q", mode)
	}
	require.NoError(t, ta.Close()) // final flush = durability barrier
}

func soakAgent(t *testing.T, dir string) *agent.TagentAgent {
	t.Helper()
	// Durability defaults are NOT relaxed (3.1): real localfile, fsync default
	// (on). The old skeleton's FSync=false throughput knob is retired — the
	// point of soak is the durable path.
	ta, err := tagent.New(tagent.Config{
		Entry: "tagent",
		Agents: map[string]tagent.AgentConfig{
			"tagent": {
				SystemPrompt: tagent.PromptConfig{Inline: "soak agent"},
				Memory: tagent.MemoryConfig{
					Type: "localfile",
					Path: dir,
				},
			},
		},
	}, tagent.WithModel(soakModel{}))
	require.NoError(t, err)
	return ta
}

func soakWritePhase(t *testing.T, ta *agent.TagentAgent, round, events int) {
	t.Helper()
	_, err := ta.StartLoop("soak", "soak-session")
	require.NoError(t, err)

	tail := fmt.Sprintf("round-%02d event %03d", round, events-1)
	for i := 0; i < events; i++ {
		body := fmt.Sprintf("round-%02d event %03d", round, i)
		if round == 0 && i == 0 {
			body = soakOrigin
		}
		ta.InjectMessage(model.Message{Role: model.RoleUser, Content: body})
	}

	// Wait until every round event is STORED (the persistent loop + plugin
	// pipeline are asynchronous — the barrier is the store, not the inject).
	pid := memory.PartitionIDFromName("tagent")
	deadline := time.Now().Add(60 * time.Second)
	for {
		refs, qerr := ta.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: tail, Limit: 10})
		require.NoError(t, qerr)
		if len(refs) >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("batch tail %q not stored within deadline", tail)
		}
		time.Sleep(50 * time.Millisecond)
	}
	ta.StopLoop() // quiesce the loop before sealing (Close is the flush barrier)

	// Force at least ONE real compaction before this process dies: seal the
	// active segment, retune thresholds (harness hook — a soak run never
	// accumulates 24 hourly segments), run the synchronous sweep. Then prove
	// in-process that the merged segment still answers the tail query.
	fs, ok := ta.MemStore().(*memory.FileSegmentStore)
	require.True(t, ok, "soak must run on the bare FileSegmentStore (no engine decorator) to reach the compactor")
	require.NotNil(t, fs.Compactor(), "wiring must inject the background compactor")
	require.NoError(t, fs.SealCurrent(pid))
	fs.Compactor().SetThresholds(1, 1)
	fs.Compactor().CompactOnce()

	compacted := false
	windows, err := fs.ListSegments(pid)
	require.NoError(t, err)
	for _, w := range windows {
		meta, err := fs.GetSegmentMeta(pid, w)
		require.NoError(t, err)
		if meta != nil && meta.Layer >= 2 {
			compacted = true
		}
	}
	require.True(t, compacted, "round %d: CompactOnce must leave at least one L2+ segment (windows=%v)", round, windows)

	refs, err := fs.QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: tail, Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, refs, "compacted history must still answer the round tail query in-process")
}

func soakVerifyPhase(t *testing.T, ta *agent.TagentAgent, round int) {
	t.Helper()
	pid := memory.PartitionIDFromName("tagent")
	refs, err := ta.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: soakOrigin, Limit: 50})
	require.NoError(t, err)
	var got strings.Builder
	for _, ref := range refs {
		got.WriteString(ref.EventSummary)
		got.WriteString("\n")
	}
	require.Contains(t, got.String(), soakOrigin,
		"memory loss after %d rounds + fresh-process restarts — the headline promise broke", round)

	// The reopen must ALSO read through the compaction artifacts the previous
	// write process left (L2+ segments exist and serve the origin recall).
	fs, ok := ta.MemStore().(*memory.FileSegmentStore)
	require.True(t, ok)
	windows, err := fs.ListSegments(pid)
	require.NoError(t, err)
	layers := 0
	for _, w := range windows {
		meta, err := fs.GetSegmentMeta(pid, w)
		require.NoError(t, err)
		if meta != nil && meta.Layer >= 2 {
			layers++
		}
	}
	require.Greater(t, layers, 0, "verify round %d: reopened store must expose compacted (L2+) segments", round)
}

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
