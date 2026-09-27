package tagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// armGate parks every FUTURE call of the agent whose prompt starts with `label`.
// Safe for concurrent use because GenerateContent reads the map under m.mu.
func (m *delegModel) armGate(label string, ch chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gates == nil {
		m.gates = map[string]chan struct{}{}
	}
	m.gates[label] = ch
}

// disarmGate frees a parked call and is idempotent, so a bail-out can never hang the
// package on a gate nobody will close.
func disarmGate(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// chainYAML is A→B→C with C's declaration text parameterized: changing C's prompt is a
// structural change, so applying it republishes C into a new generation.
func chainYAML(cPrompt string) string {
	return strings.Replace(nestedYAML(), `inline: "SUB-C-PROMPT"`, fmt.Sprintf("inline: %q", cPrompt), 1)
}

// allLevelsChangedYAML is the same chain with a new declaration at EVERY level.
func allLevelsChangedYAML() string {
	return strings.NewReplacer(
		`inline: "ENTRY-A-PROMPT"`, `inline: "ENTRY-A-PROMPT-G2"`,
		`inline: "SUB-B-PROMPT"`, `inline: "SUB-B-PROMPT-G2"`,
		`inline: "SUB-C-PROMPT"`, `inline: "SUB-C-PROMPT-G2"`,
	).Replace(nestedYAML())
}

// TestOrgDelegation_AllLevelsRepublishedReachTheNewLeaf pins defect D-b of evidence
// §5.44: when EVERY level changes, the transitional-carrier loop walked the changed set
// in MAP order, so a parent's shell could be assembled before its child's — and the DFS
// cache-hit the stale resident child, baking the old target into the new face. The outcome
// therefore flipped run to run (measured: the new leaf was reached in 1 of 6 identical
// publishes). Deterministic delivery is what a structural publish owes the caller: once a
// generation is in force, the next turn must run that generation's declarations at EVERY
// depth, not only where the build order happened to cooperate.
//
// Unlike the nested-hop contract above (which also needs D-a, the trunk), this case is
// reachable today: the entry's own face is rebuilt on every publish.
func TestOrgDelegation_AllLevelsRepublishedReachTheNewLeaf(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(chainYAML("SUB-C-PROMPT"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "all-levels-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("warm"))
	require.NoError(t, err)
	waitFor(t, "the chain ran on G1", func() bool { return countServed(m.snapshot(), "SUB-C-PROMPT") >= 1 })

	// Every level changes: entry, the middle agent, and the leaf.
	write(allLevelsChangedYAML())
	entry.CheckOrgReload()

	before := countServed(m.snapshot(), "SUB-C-PROMPT-G2")
	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("after"))
	require.NoError(t, err)
	waitFor(t, "the next turn reaches the new leaf at depth 3", func() bool {
		return countServed(m.snapshot(), "SUB-C-PROMPT-G2") > before
	})
}

// TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget is 3.2's acceptance row
// 「A→B→C 各见本代自身声明」for the facet no existing anchor reaches: the NESTED hop
// across a publication.
//
// Coverage so far: NestedLevelsServeFromTheirOwnBindings proves each LEVEL sees its own
// declarations in steady state; InFlightDelegationKeepsItsOwnGenerationTarget proves a
// pinned initiator keeps its generation's target at ONE hop. Neither exercises B — pinned
// to G1 while parked — delegating DOWN to C after G2 replaced C.
//
// The pinned-face rule (resident-continuity「重试也不改路由」, subagent-turn-execution
// 「仍持 G1 租约的发起者不因 G2 删除目标而丢失其合法 G1 绑定」, D5) says that hop must run
// on the C that B's G1 face declared. The third turn must then run the new C — which is
// what makes this anchor self-discriminating: if the publish never landed, the first
// assertion would pass for the wrong reason.
func TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget(t *testing.T) {
	// Round 90: UN-SKIPPED as the trunk DoD (user-approved holding expansion). Red was
	// re-measured before implementation; it must be GREEN when the trunk lands.
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(chainYAML("SUB-C-PROMPT"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "nested-hop-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	// Turn 1: the full chain runs on G1, C included.
	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first request"))
	require.NoError(t, err)
	waitFor(t, "the leaf served on G1", func() bool { return countServed(m.snapshot(), "SUB-C-PROMPT") >= 1 })

	// Turn 2: park B mid-call, so its execution is pinned to G1 while G2 lands.
	bGate := make(chan struct{})
	m.armGate("SUB-B-PROMPT", bGate)
	t.Cleanup(func() { disarmGate(bGate) })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("second request"))
	require.NoError(t, err)
	waitFor(t, "B parked mid-call", func() bool {
		for _, s := range m.snapshot() {
			if strings.HasPrefix(s.System, "SUB-B-PROMPT") && len(s.Tools) > 0 {
				return true
			}
		}
		return false
	})

	write(chainYAML("SUB-C-PROMPT-G2"))
	entry.CheckOrgReload() // publish G2 while B is parked

	entriesBefore := countServed(m.snapshot(), "SUB-C-PROMPT")
	g2Before := countServed(m.snapshot(), "SUB-C-PROMPT-G2")
	disarmGate(bGate)

	//〔轮九十断言迁移（显式，非静默改测）〕The previous formulation — "no
	// SUB-C-PROMPT-G2 serve before the injected third turn" — conflated "the
	// pinned hop is not re-routed" with "no fresh turn runs at all". The second
	// half held only under the D-a defect, where NO fresh turn could reach the
	// new C; with the trunk landed, the settle of this very delegation legitimately
	// drives a fresh turn that takes the NEW generation (correct per D6: a
	// task_settled re-entry uses the current effective face). So the assertion now
	// pins the hop's own answer: wait for a NEW B post-tool record (turn 1 already
	// produced one) and require ITS result to be the OLD C's answer, quoted-exact
	// so the -G2 variant cannot match as a prefix.
	bHopAnswers := func() []string {
		var out []string
		for _, s := range m.snapshot() {
			if s.System == "SUB-B-PROMPT" && len(s.ToolResults) > 0 {
				out = append(out, strings.Join(s.ToolResults, "\n"))
			}
		}
		return out
	}
	hopsBefore := len(bHopAnswers())
	waitFor(t, "the pinned G1 hop returned its answer to B", func() bool {
		return len(bHopAnswers()) > hopsBefore
	})
	answers := bHopAnswers()
	// The (hopsBefore+1)-th B-with-results record IS the pinned hop's: the only
	// later source of another one is the settle of this very delegation, which
	// cannot fire before the hop's producer returned — so indexing (not "latest")
	// keeps the witness on the hop even when both land between two polls.
	pinned := answers[hopsBefore]
	require.Contains(t, pinned, `"served:SUB-C-PROMPT"`,
		"§3.2：被钉跳的回执必须是 G1 之 C 的回答（D5「派生前继承发起调用租约」）")
	require.NotContains(t, pinned, `"served:SUB-C-PROMPT-G2"`,
		"§3.2：B 的 G1 代执行不得因为 G2 换了 C 就被改道到新目标——被钉跳的回执不能来自新代目标")
	require.Greater(t, countServed(m.snapshot(), "SUB-C-PROMPT"), entriesBefore,
		"the pinned hop really executed on the old C (its answer came from somewhere)")

	// …and the NEW generation must really be reachable — otherwise the assertion
	// above would only prove the publish never took effect.
	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("third request"))
	require.NoError(t, err)
	waitFor(t, "a fresh turn serves the new C", func() bool {
		return countServed(m.snapshot(), "SUB-C-PROMPT-G2") > g2Before
	})
}
