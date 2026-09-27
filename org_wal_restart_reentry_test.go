package tagent

// 轮九十九（evidence §5.55）：4.2 的最后一条腿——**WAL 重建入口的多级重入**。
//
// 轮九十四已证「生产 Spawn 入口」四态（§5.50），当时如实记：重启入口缺 org 级
// harness。本测补的就是那一句：任务落在 **b 自己的 board** 上且**未终结**时进程
// 结束（崩溃形状的 WAL：只有 task_spawned，没有终态 settle），随后由**独立进程**
// 在同一批持久 store 上重启，再从 b 的重建 board 上重放 `relaunch_task`。
//
// 为什么必须是跨进程：一次 boot 只有真实进程启动才算证据（§8 xproc 纪律），
// 同进程内多轮 New/Close 翻动并不是生产路径。崩溃形状靠**不调用 Close 直接退出**
// 得到——这正是「上次运行留下未终结任务」的物理形态，而不是我手搓一条记录。
//
// 判定的锋利处在负面子进程：重启后把 c 从拓扑里摘掉，重建出的存量任务必须**按名
// 拒绝**而不是被旧代绑定静默复活——wireAgent 的 redispatch 注释点名的就是这件事
// （「若传快照，后续代移除的目标仍会被旧代绑定静默复活」）。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/task"
	tasktool "github.com/SpellingDragon/tagent/tool/task"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	wal42PhaseEnv = "TAGENT_WAL42_PHASE"
	wal42YamlEnv  = "TAGENT_WAL42_YAML"
	wal42Filter   = "TestWAL42_RelaunchAfterRestartResolvesOnTheCurrentFace$"
)

// wal42YAML renders a→b→c with EVERY agent on its own localfile store, so the
// fact chain physically survives the process that wrote it. b carries the
// PRODUCTION task tools (relaunch/resume) and delegates to c asynchronously
// (default), which is what puts the subagent task on b's own board.
func wal42YAML(root string, dropC bool) string {
	mem := func(name string) string {
		return fmt.Sprintf("    memory:\n      type: localfile\n      path: %q\n", filepath.Join(root, "store-"+name))
	}
	cRef := "      - kind: agent\n        agent: c\n        description: delegate-c\n"
	cDef := "  c:\n    system_prompt:\n      inline: \"SUB-C\"\n" + mem("c")
	if dropC {
		cRef, cDef = "", ""
	}
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A\"\n" + mem("a") +
		"    tools:\n      - kind: agent\n        agent: b\n        description: delegate-b\n" +
		"  b:\n    system_prompt:\n      inline: \"SUB-B\"\n" + mem("b") +
		"    tools:\n" + cRef +
		"      - kind: tool\n        id: relaunch_task\n      - kind: tool\n        id: resume_task\n" + cDef
}

// wal42Model delegates to a named target at every level (never tools[0], §6.20)
// and can park c's FIRST call so the task is still unfinished when the process dies.
type wal42Model struct {
	mu      sync.Mutex
	served  []delegServed
	perCall map[string]int
	prefer  []string
	gate    chan struct{}
	gated   bool
}

func (m *wal42Model) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)

	m.mu.Lock()
	if m.perCall == nil {
		m.perCall = map[string]int{}
	}
	m.perCall[label]++
	n := m.perCall[label]
	offer := ""
	if n%2 == 1 {
		for _, want := range m.prefer {
			for _, t := range tools {
				if t == want {
					offer = t
					break
				}
			}
			if offer != "" {
				break
			}
		}
	}
	park := m.gate
	if label == "SUB-C" && park != nil && !m.gated {
		m.gated = true
	} else {
		park = nil
	}
	m.served = append(m.served, delegServed{System: label, Tools: tools, Answered: offer})
	m.mu.Unlock()

	if park != nil { // hold the producer open: the task must NOT reach a terminal settle
		select {
		case <-park:
		case <-time.After(60 * time.Second):
		}
	}

	ch := make(chan *model.Response, 1)
	if offer != "" {
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-" + offer,
				Function: model.FunctionDefinitionParam{Name: offer, Arguments: []byte(`{"request":"work"}`)}}},
		}}}}
	} else {
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant, Content: "served:" + label}}}}
	}
	close(ch)
	return ch, nil
}

func (m *wal42Model) Info() model.Info { return model.Info{Name: "wal42-model"} }

func (m *wal42Model) snapshot() []delegServed {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]delegServed(nil), m.served...)
}

// firstSubagentTask reads the subagent task off the owner's OWN board.
func firstSubagentTask(b *agent.TagentAgent) *task.Task {
	for _, tk := range b.TaskManager().List() {
		if tk.Spec.Kind == "subagent" {
			return tk
		}
	}
	return nil
}

func wal42Boot(t *testing.T, yamlPath string, m *wal42Model) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry
}

// TestWAL42_RelaunchAfterRestartResolvesOnTheCurrentFace is the parent: it hands
// each boot to its own process and only orchestrates the durable state between them.
func TestWAL42_RelaunchAfterRestartResolvesOnTheCurrentFace(t *testing.T) {
	if phase := os.Getenv(wal42PhaseEnv); phase != "" {
		wal42Child(t, phase)
		return
	}

	// Each scenario gets its OWN durable root: the negative leg must restart over
	// the crash state itself, not over a board the positive leg already settled
	// (a settled task correctly folds to nothing — sharing a root would have tested
	// that instead of the intended refusal).
	runScenario := func(dropOnRestart bool) {
		dir := t.TempDir()
		yamlPath := filepath.Join(dir, "tagent.yaml")
		env := append(os.Environ(), wal42YamlEnv+"="+yamlPath)

		// BOOT 1 — leave an UNFINISHED subagent task on b's board, then die abruptly
		// (no Close): the crash-shaped fact chain the next boot has to fold.
		runBootChild(t, env, wal42PhaseEnv+"=spawn", wal42Filter)

		if dropOnRestart {
			// BOOT 2' — the operator removed c before the restart: the rebuilt task
			// must be refused BY NAME, not resurrected through a snapshot of the old face.
			require.NoError(t, os.WriteFile(yamlPath, []byte(wal42YAML(dir, true)), 0o644))
			runBootChild(t, env, wal42PhaseEnv+"=restart_drop_c", wal42Filter)
			return
		}
		// BOOT 2 — restart over that WAL with c still routed: the rebuilt task must
		// really re-run, resolving through b's rebuilt owner face at depth 2.
		runBootChild(t, env, wal42PhaseEnv+"=restart", wal42Filter)
	}
	runScenario(false)
	runScenario(true)
}

func wal42Child(t *testing.T, phase string) {
	yamlPath := os.Getenv(wal42YamlEnv)
	require.NotEmpty(t, yamlPath, "the parent must hand the shared config path through the env")
	dir := filepath.Dir(yamlPath)

	switch phase {
	case "spawn":
		require.NoError(t, os.WriteFile(yamlPath, []byte(wal42YAML(dir, false)), 0o644))
		gate := make(chan struct{})
		m := &wal42Model{prefer: []string{"b", "c"}, gate: gate}
		entry := wal42Boot(t, yamlPath, m)
		out, err := entry.StartLoop("u", "wal42-spawn")
		require.NoError(t, err)
		go func() {
			for range out {
			}
		}()
		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first"))
		require.NoError(t, err)
		waitFor(t, "the nested delegation entered and is still running", func() bool {
			return countServed(m.snapshot(), "SUB-C") >= 1
		})
		b := residentCacheForTest(entry)["b"]
		require.NotNil(t, b, "b is a resident owner")
		require.NotNil(t, firstSubagentTask(b),
			"precondition: the unfinished subagent task sits on b's OWN board")
		// Give the spawn record time to reach the durable path, then exit WITHOUT
		// Close. The task must be left un-settled — that is the whole state under test.
		time.Sleep(2 * time.Second)
		os.Exit(0)

	case "restart":
		m := &wal42Model{prefer: []string{"b", "c"}}
		entry := wal42Boot(t, yamlPath, m)
		defer func() { _ = entry.Close() }()
		b := residentCacheForTest(entry)["b"]
		require.NotNil(t, b)

		tk := firstSubagentTask(b)
		require.NotNilf(t, tk,
			"§4.2 WAL 重建入口：重启后 b 自己的 board 必须从事实链折回那个未终结的子 agent 任务（实得 board=%v）", len(b.TaskManager().List()))
		before := countServed(m.snapshot(), "SUB-C")

		// No initiating call exists after a restart: this is the 无发起者 branch,
		// driven through the PRODUCTION relaunch tool against b's own controller.
		ctx := task.WithTaskSpawner(context.Background(), b.TaskManager())
		res, err := tasktool.NewRelaunchTaskTool().Call(ctx, relaunchArgs(t, tk.ID))
		require.NoError(t, err)
		require.NotContains(t, res.(string), "失败",
			"重建出的合法任务在重启入口必须能重放：%v", res)
		waitFor(t, "the relaunched depth-2 delegation really ran on the rebuilt face", func() bool {
			return countServed(m.snapshot(), "SUB-C") > before
		})

	case "restart_drop_c":
		m := &wal42Model{prefer: []string{"b", "c"}}
		entry := wal42Boot(t, yamlPath, m)
		defer func() { _ = entry.Close() }()
		b := residentCacheForTest(entry)["b"]
		require.NotNil(t, b)
		tk := firstSubagentTask(b)
		require.NotNil(t, tk, "precondition: the rebuilt board still carries the durable task")
		require.Nil(t, b.ContextManager().SubagentWrapper("c"),
			"precondition: the restarted face no longer routes c")

		before := countServed(m.snapshot(), "SUB-C")
		ctx := task.WithTaskSpawner(context.Background(), b.TaskManager())
		res, err := tasktool.NewRelaunchTaskTool().Call(ctx, relaunchArgs(t, tk.ID))
		if err == nil {
			require.Contains(t, res.(string), "失败",
				"§4.2：重启后目标已被摘路由，存量任务必须**按名拒绝**而不是静默复活旧代绑定：%v", res)
		}
		require.Zero(t, countServed(m.snapshot(), "SUB-C")-before,
			"a refused re-entry must not execute the removed target")

	default:
		t.Fatalf("unknown phase %q", phase)
	}
	os.Exit(0)
}
