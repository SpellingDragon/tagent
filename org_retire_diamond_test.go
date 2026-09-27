package tagent

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
)

// retDiamondYAML renders the diamond: main → {s1, s2}, and BOTH of them route the
// shared leaf s3. `routeS1/routeS2` switch the entry's routes, which is how one
// generation can drop a branch while the other branch stays in force.
func retDiamondYAML(t testing.TB, routeS1, routeS2 bool) string {
	t.Helper()
	var mainTools string
	if routeS1 {
		mainTools += `      - kind: agent
        agent: s1
        description: "s1"
`
	}
	if routeS2 {
		mainTools += `      - kind: agent
        agent: s2
        description: "s2"
`
	}
	branch := func(name string) string {
		return fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"PROMPT-%s\"\n    memory:\n      type: memory\n      path: %q\n    tools:\n      - kind: agent\n        agent: s3\n        description: \"s3\"\n",
			name, name, testStore(t, "retire-diamond-"+name))
	}
	return "entry: main\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    memory:\n      type: memory\n      path: " +
		fmt.Sprintf("%q\n    tools:\n%s", testStore(t, "retire-diamond-main"), mainTools) +
		branch("s1") + branch("s2") +
		fmt.Sprintf("  s3:\n    system_prompt:\n      inline: \"PROMPT-s3\"\n    memory:\n      type: memory\n      path: %q\n", testStore(t, "retire-diamond-s3"))
}

// TestRetire_DiamondSharedDependencyWaitsForAllBorrowers is §4.3's named
// acceptance「菱形共享依赖」at RETIREMENT time (the build-side diamond is anchored
// once in build_cycle_test): main → {s1, s2} → s3.
//
// What must hold when a generation drops BOTH entry routes:
//  1. a branch with nothing left on it (s1) converges, while
//  2. the shared leaf (s3) is NOT retired just because it has no work of its own —
//     it is still inside the callable closure of s2's live generation, which has an
//     execution in flight and may delegate at any moment (D8's usage right, §5.37);
//     retiring it here would silently break a legal later call;
//  3. once that last borrower drains, s3 exits on its own — no extra business turn
//     (§5.38), and the cascade (s2 then s3) converges inside one drain pass instead
//     of waiting for an event that may never come.
func TestRetire_DiamondSharedDependencyWaitsForAllBorrowers(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retDiamondYAML(t, true, true))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	owners := residentCacheForTest(entry)
	s1, s2, s3 := owners["s1"], owners["s2"], owners["s3"]
	require.NotNil(t, s3, "precondition: the shared leaf is resident")
	require.NotNil(t, s1.ContextManager().SubagentWrapper("s3"), "both branches route s3…")
	require.NotNil(t, s2.ContextManager().SubagentWrapper("s3"), "…from their own faces")

	// s2 has an execution in flight; it has NOT delegated to s3 yet, but its
	// generation legitimately may.
	inFlight := s2.ContextManager().AcquireLease(agent.LeaseSubCall)

	// One generation drops BOTH entry routes: s1, s2 and (transitively) s3 leave the
	// reachable set.
	write(retDiamondYAML(t, false, false))
	entry.CheckOrgReload()

	require.Eventually(t, func() bool {
		return residentCacheForTest(entry)["s1"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"没有引用也没有保有的分支（s1）应收敛")

	require.NotNil(t, residentCacheForTest(entry)["s2"], "在途执行所在的分支不得被提前关掉")
	require.NotNil(t, residentCacheForTest(entry)["s3"],
		"§4.3 菱形锚：共享叶子被别的存活代保有时不得退役——它自己没有义务不代表可关")
	require.False(t, s3.CloseStarted(), "and its close must not even have begun")

	// The last borrower draining releases the leaf too; the cascade must converge
	// without any further traffic.
	inFlight.Release()
	require.Eventually(t, func() bool {
		o := residentCacheForTest(entry)
		return o["s2"] == nil && o["s3"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"最后借用者退出后，s2 与共享的 s3 须在同一个排空里依次收敛（不等待新 turn）")
}
