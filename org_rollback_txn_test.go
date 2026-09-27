package tagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// 轮九十二（evidence §5.48）：§2.4「回滚＝旧完整配置发布新代，删除专用重建分支」。
//
// 正向热更自 S-B/S-C 起就是「私有候选 overlay → 唯一提交点 Add → 未发布即整体逆序
// 回退」；回滚却留着专用分支：先把重建出的缺失 owner **提前**并入常驻表，再进
// stage/激活。于是回滚在**后段**失败时没有任何回退——owner 已对并发读者可见、其
// store 租约被持有，而有效代纹丝不动（半改）。三行验收各钉一测：
//
//	(d) 回滚最后阶段失败不半改      ← 本轮红锚（现制：泄漏的 owner 留在在线清册）
//	(b) 热增→数值更新→真实在途调用→回滚 ← 宿主结果／所属 agent 的真实 TTL／单一 owner
//	(c) 移除父保留共享子后回滚      ← 共享子仍恰一 owner（二次获取会 fail-closed）

const (
	g24Tool   = "g24_flaky_tool" // unique: RegisterPlainTool panics on a duplicate id
	g24Leaf   = "g24_leaf"
	g24Mid    = "g24_mid"
	g24B      = "g24_b"
	g24P1     = "g24_p1"
	g24P2     = "g24_p2"
	g24Shared = "g24_shared"
)

// g24Gate is the injected REAL failure: a plain-tool factory that cannot serve
// past a call budget. Nothing in the product path knows about it.
//
// The registration is process-global and duplicate ids panic (§0 口径②), so the
// gate is a package-level singleton armed once and RESET per run — which keeps
// this file usable under a `-count>1` repetition gate instead of needing an
// exemption.
type g24Gate struct {
	limit atomic.Int64
	calls atomic.Int64
}

var (
	g24GateOnce sync.Once
	g24Flaky    = &g24Gate{}
)

func g24Arm(t *testing.T) *g24Gate {
	t.Helper()
	g24GateOnce.Do(func() {
		agent.RegisterPlainTool(g24Tool, g24Flaky.tool)
	})
	g24Flaky.calls.Store(0)
	g24Flaky.limit.Store(1 << 40)
	return g24Flaky
}

func (g *g24Gate) tool(_ agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
	if g.calls.Add(1) > g.limit.Load() {
		return nil, errors.New("injected: tool factory cannot serve")
	}
	return &mockCallableTool{name: g24Tool}, nil
}

func g24Write(t *testing.T, path, content string, tick time.Time) time.Time {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, tick, tick))
	return tick
}

// g24LeafYAML: main → mid → leaf. leaf declares the flaky tool and owns its own
// localfile store, so a leaked owner is observable twice over: in the resident
// table, and as a still-held writer lock. withLeaf=false drops leaf from mid's
// routing (structural: the routing shape is fingerprinted).
func g24LeafYAML(withLeaf bool, leafStore string) string {
	midTail := "    tools: []\n"
	leafDef := ""
	if withLeaf {
		midTail = fmt.Sprintf("    tools:\n      - kind: agent\n        agent: %s\n        description: delegate-leaf\n", g24Leaf)
		leafDef = fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"LEAF\"\n    memory:\n      type: localfile\n      path: %q\n    tools:\n      - kind: tool\n        id: %s\n", g24Leaf, leafStore, g24Tool)
	}
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
    tools:
      - kind: agent
        agent: %s
        description: delegate-mid
  %s:
    system_prompt:
      inline: "MID"
    memory:
      type: memory
%s%s`, g24Mid, g24Mid, midTail, leafDef)
}

// TestRollback24_LateStageFailureLeavesNoOwnerPublished is acceptance row (d).
//
// Sequence: cold (leaf resident, tool call #1) → structural removal of leaf
// (publish; leaf retires idle) → Rollback restores the generation that routes it,
// so the leaf must be re-acquired (tool call #2 succeeds) — and then the very
// LAST stage, assembling the new faces, fails on that same tool (call #3).
// The budget is calibrated from the observed cold-start count, so this measures
// the rollback's own ordering rather than a guessed constant.
//
// After a refused rollback the online topology must be exactly what it was:
// current generation unchanged, the leaf NOT resident, and its store writer slot
// handed back. Today the leaf is merged into the resident table BEFORE the
// candidate completes and nothing unwinds it, so both owner and lease leak.
func TestRollback24_LateStageFailureLeavesNoOwnerPublished(t *testing.T) {
	gate := g24Arm(t) // cold start succeeds unconditionally; the budget is set below

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	store := filepath.Join(dir, "store-g24-leaf")
	tick := g24Write(t, yamlPath, g24LeafYAML(true, store), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	leafBefore := residentCacheForTest(entry)[g24Leaf]
	require.NotNil(t, leafBefore, "precondition: leaf is resident on the startup generation")

	// Let exactly ONE more tool construction succeed — the rollback's re-acquisition —
	// and fail the face build that follows it.
	coldCalls := gate.calls.Load()
	gate.limit.Store(coldCalls + 1)

	// Structural removal: leaf becomes unrouted and retires.
	tick = g24Write(t, yamlPath, g24LeafYAML(false, store), tick)
	entry.CheckOrgReload()
	genAfterRemoval := di64(t, entry.OrgDiagnostics(), "generation")
	waitFor(t, "the unrouted leaf retired", func() bool {
		return residentCacheForTest(entry)[g24Leaf] == nil
	})
	// The removal itself rebuilt no leaf tools; note the call count so the
	// rollback's first tool construction is the re-acquisition.
	gate.limit.Store(gate.calls.Load() + 1)

	// Roll back to the generation routing leaf: re-acquisition succeeds, the face
	// build fails — the last stage before publishing.
	entry.Rollback()

	require.Equal(t, genAfterRemoval, di64(t, entry.OrgDiagnostics(), "generation"),
		"a refused rollback must not publish")
	require.Nil(t, residentCacheForTest(entry)[g24Leaf],
		"§2.4(d)：后段失败的回滚不得把未发布的 owner 留在在线清册里（现制：提前 Add、无回退）")
	midNow := residentCacheForTest(entry)[g24Mid]
	require.NotNil(t, midNow)
	require.Nil(t, midNow.ContextManager().SubagentWrapper(g24Leaf),
		"and the serving face must still not route it")
	require.NoError(t, takeOverStore(t, store),
		"§2.4(d)：被拒候选为 leaf 取的 store 租约必须随回退归还，否则该路径永久占住写者名额")
}

// g24BYAML routes main→b when withB, with b's OWN hot numerics; changing those
// numerics is numeric-only (fingerprint-excluded), changing the routing is not.
func g24BYAML(withB bool, keep int, terminal string) string {
	mainTail := "    tools: []\n"
	bDef := ""
	if withB {
		mainTail = fmt.Sprintf("    tools:\n      - kind: agent\n        agent: %s\n        description: delegate-b\n        async: false\n", g24B)
		bDef = fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"SUB-B\"\n    memory:\n      type: memory\n    keep_recent_tasks: %d\n    task_terminal_ttl: %q\n", g24B, keep, terminal)
	}
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
%s%s`, mainTail, bDef)
}

// TestRollback24_HotAddNumericAndInFlightRollback is acceptance row (b): hot-add
// B → numeric-only update → a real (parked, in-flight) B call AND a fresh one
// across the rollback → rollback. The assertions read B's OWN consumers and the
// host-visible answer, not a fingerprint: the ring source values must return,
// B must stay ONE owner throughout, and a call already in flight must still
// complete rather than get torn down by the rollback republish.
func TestRollback24_HotAddNumericAndInFlightRollback(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := g24Write(t, yamlPath, g24BYAML(false, 2, "1m"), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "g24-b-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	// (1) hot-add B → structural publish.
	tick = g24Write(t, yamlPath, g24BYAML(true, 2, "1m"), tick)
	entry.CheckOrgReload()
	ownerB := residentCacheForTest(entry)[g24B]
	require.NotNil(t, ownerB, "B became a resident owner")

	// (2) numeric-only update on B (structure identical).
	tick = g24Write(t, yamlPath, g24BYAML(true, 7, "5m"), tick)
	entry.CheckOrgReload()
	require.Equal(t, 7, ownerB.OrgKeepRecent(), "B's own compressor consumer took the update")
	require.Equal(t, 5*time.Minute, ownerB.TaskManager().TerminalTTL(), "B's own manager took the update")

	// (3) a real delegation across the rollback: park B mid-call, roll back,
	// release; the pinned call must still be served.
	bGate := make(chan struct{})
	m.armGate("SUB-B", bGate)
	t.Cleanup(func() { disarmGate(bGate) })
	if _, err := entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("in-flight")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "B parked mid-call", func() bool {
		for _, s := range m.snapshot() {
			if s.System == "SUB-B" {
				return true
			}
		}
		return false
	})

	entry.Rollback() // publishes a new generation while B's call is in flight
	servedBefore := countServed(m.snapshot(), "SUB-B")
	disarmGate(bGate)
	waitFor(t, "the in-flight B call completed", func() bool {
		return countServed(m.snapshot(), "SUB-B") > servedBefore
	})

	// (4) the rollback restored the ring source through the same record: B's OWN
	// consumers moved back, and B is still exactly one owner instance.
	require.Equal(t, 2, ownerB.OrgKeepRecent(), "§2.4(b)：回滚把 B 自身的 keepRecent 恢复到环源值")
	require.Equal(t, time.Minute, ownerB.TaskManager().TerminalTTL(), "§2.4(b)：回滚把 B 自身的 terminal TTL 恢复到环源值")
	require.Same(t, ownerB, residentCacheForTest(entry)[g24B],
		"§2.4(b)：回滚推进执行面，不另造第二个 B owner（单一 owner＝实例与 store 身份不动）")

	// (5) and a FRESH call after the rollback is served too (the new generation is
	// live, not a half-published face).
	if _, err := entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("after")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a post-rollback call is served", func() bool {
		return countServed(m.snapshot(), "SUB-B") > servedBefore+1
	})
}

// g24DiamondYAML routes main→{p1,p2} (or only p2), both to the SAME shared child
// which owns its own localfile store.
func g24DiamondYAML(withP1 bool, sharedStore string) string {
	mainTools := fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: d-p2\n", g24P2)
	if withP1 {
		mainTools = fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: d-p1\n%s", g24P1, mainTools)
	}
	routes := func(parent string) string {
		return fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"P-%s\"\n    memory:\n      type: memory\n    tools:\n      - kind: agent\n        agent: %s\n        description: shared-child\n", parent, parent, g24Shared)
	}
	p1 := ""
	if withP1 {
		p1 = routes(g24P1)
	}
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
    tools:
%s%s%s  %s:
    system_prompt:
      inline: "SHARED"
    memory:
      type: localfile
      path: %q
`, mainTools, p1, routes(g24P2), g24Shared, sharedStore)
}

// TestRollback24_RemovedParentRollbackKeepsSharedChildSingleOwner is acceptance
// row (c): a parent is removed while the child it shares with a surviving parent
// stays routed; rolling back must re-acquire ONLY the parent and adopt the ONE
// existing child owner. A second acquisition of the child's store would fail
// closed (single writer), so "the rollback took effect at all" is itself the
// witness — plus the child's owner identity must be unchanged.
func TestRollback24_RemovedParentRollbackKeepsSharedChildSingleOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	store := filepath.Join(dir, "store-g24-shared")
	tick := g24Write(t, yamlPath, g24DiamondYAML(true, store), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	shared := residentCacheForTest(entry)[g24Shared]
	require.NotNil(t, shared, "precondition: the shared child is resident")

	tick = g24Write(t, yamlPath, g24DiamondYAML(false, store), tick)
	entry.CheckOrgReload()
	genRemoved := di64(t, entry.OrgDiagnostics(), "generation")
	waitFor(t, "the removed parent retired", func() bool {
		return residentCacheForTest(entry)[g24P1] == nil
	})
	require.Same(t, shared, residentCacheForTest(entry)[g24Shared],
		"the surviving route keeps the SAME child owner (still declared by p2's face)")

	entry.Rollback()

	require.Greater(t, di64(t, entry.OrgDiagnostics(), "generation"), genRemoved,
		"§2.4(c)：回滚须真正发布新代（若它二次获取了共享子的 store，单写者门会把它 fail-closed 在此）")
	require.NotNil(t, residentCacheForTest(entry)[g24P1], "the parent came back")
	require.Same(t, shared, residentCacheForTest(entry)[g24Shared],
		"§2.4(c)：共享子仍是同一个 owner，不因回滚被再造/重取")
}
