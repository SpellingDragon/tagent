package tagent

// 轮一百（evidence §5.56）：3.3「有状态工具」的最后一腿——**org 级端到端锚**，
// 用真实 tmux 会话证明「已纳管任务不因工具换代失监视」。
//
// 为什么走重启而不是热更窗口：`quiet_timeout` 的下限被钉在稳定窗（60s，TUI 90s），
// 也就是说热更之后再靠「静默→suspect」把任务推进裁决区，需要 60s 以上真实静默——
// 那属既有负载敏感 tmux 族（§5.36/§5.40），钉成常驻锚只会变成偶发红灯。而重启入口
// 有一个**确定**的同款裁决：`RestoreTask` 把 running 语义降级为 suspect，随后
// `build_agent.go:761-787` 的 TaskID 桥先裁孤儿、再把「被当前 monitor 跟踪」的任务
// 提升回 running——一个存活会话要被判「未跟踪」，等价于监视信号在某一代工具手里丢了。
// 这条链与热更窗口检验的是**同一处 seam**（换代装配新工具后，管理器读的必须是当前代
// 的 monitor），且它顺带把本子句另一句「声明构造与恢复重挂/monitor 激活分离」钉住：
// 重挂必须先于裁决与提升，否则合法活会话会被自己裁成 reincarnation orphan。
//
// 崩溃形状与轮九十九同法：spawn 子进程起一个真实长命令会话，等记录过 durable 路径后
// **不调 Close 直接退出**——tmux 会话属于 server 不属于本进程，因此它活到下一次 boot。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	mon33PhaseEnv = "TAGENT_MON33_PHASE"
	mon33YamlEnv  = "TAGENT_MON33_YAML"
	mon33Filter   = "TestMonitor33_LiveSessionStaysWatchedAcrossToolGeneration$"
	mon33Command  = "sleep 150"
	mon33SvcName  = "mon33svc"
)

// mon33YAML renders an entry that owns the REAL exec tool (the ActionTool), on a
// localfile store so the task record outlives the process. No model/providers
// section: the host-injected mock then serves the agent (resolveAgentModel order 2).
func mon33YAML(root string) string { return mon33YAMLWithExtra(root, "") }

// mon33YAMLAfterPublish adds a routed sub-agent: a declaration the current face
// does not have, so CheckOrgReload really publishes a NEW generation (and with it
// a freshly assembled ActionTool for the owner).
func mon33YAMLAfterPublish(root string) string {
	return mon33YAMLWithExtra(root, "      - kind: agent\n        agent: helper\n        description: delegate-helper\n"+"  helper:\n    system_prompt:\n      inline: \"SUB-HELPER\"\n    memory:\n      type: memory\n")
}

func mon33YAMLWithExtra(root, extra string) string {
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A\"\n    max_tool_iterations: 2\n    memory:\n      type: localfile\n      path: " +
		fmt.Sprintf("%q", filepath.Join(root, "store-a")) +
		"\n    tools:\n      - kind: tool\n        id: exec\n" + extra
}

// mon33Model asks for the action tool once, with a command that keeps its tmux
// session alive well past the restart, then closes the turn on the ack. The
// registry id is "exec" while the tool's own DECLARATION name is "action"
// (action_tool.go:287) — the model only ever sees the declaration name, so
// targeting the id from YAML would silently offer nothing.
type mon33Model struct {
	mu    sync.Mutex
	calls int
	// offered records which call numbers actually issued a tool call, so the
	// post-publish assertion cannot be satisfied by a turn that never delegated.
	offered  []int
	requests []*model.Request
}

// count reads the call total under the same mutex the model writes under — the
// polling predicates below must never touch the field directly (a plain read
// against the model's locked write is a data race, caught by -race here).
func (m *mon33Model) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mon33Model) offeredCalls() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]int(nil), m.offered...)
}

func (m *mon33Model) snapshot() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*model.Request(nil), m.requests...)
}

func (m *mon33Model) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)

	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.calls++
	n := m.calls
	offer := ""
	// calls 1 and 3 each attempt a spawn under the SAME logical name
	if n == 1 || n == 3 {
		for _, t := range tools {
			if t == "action" {
				offer = t
			}
		}
	}
	if offer != "" {
		m.offered = append(m.offered, n)
	}
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if offer != "" {
		// `mode: resident` is what makes the session survive the restart at all:
		// startup reaps prefix-matched LEFTOVER sessions (they would hold ptys
		// forever), and only sessions recorded in the persisted resident metadata
		// are re-attached afterwards (action_tool.go:201-219, D1). Using the
		// default oneshot mode would have the second boot correctly kill it.
		// `name` is what makes it addressable ACROSS the restart: a named session
		// becomes `n-<logical>` and is spared by the startup orphan reap (only
		// generated `prefix-<ts>` names are reaped), and the tool's own schema
		// recommends exactly this pairing for mode=resident services.
		args, err := json.Marshal(map[string]any{
			"command": mon33Command, "ttl": 600, "mode": "resident", "name": mon33SvcName,
		})
		if err != nil {
			return nil, err
		}
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-action", Function: model.FunctionDefinitionParam{
				Name: offer, Arguments: args}}},
		}}}}
	} else {
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage("served:" + label)}}}
	}
	close(ch)
	return ch, nil
}

func (m *mon33Model) Info() model.Info { return model.Info{Name: "mon33-model"} }

// commandTask finds the exec task and its backing tmux session name.
func commandTask(ta *agent.TagentAgent) (*task.Task, string) {
	for _, tk := range ta.TaskManager().List() {
		if tk.Spec.Kind == "command" && tk.Spec.Declarative != nil && tk.Spec.Declarative.TaskID != "" {
			return tk, tk.Spec.Declarative.TaskID
		}
	}
	return nil, ""
}

// tmuxSessionAlive asks the tmux server itself — the ground truth the monitor's
// answer has to agree with, so a promoted task cannot be an accident of a dead session.
func tmuxSessionAlive(session string) bool {
	if session == "" {
		return false
	}
	return exec.Command("tmux", "has-session", "-t", session).Run() == nil
}

// killOwnSession releases ONLY a session this test created (name guard), leaving
// anything else on the operator's tmux server untouched.
func killOwnSession(t *testing.T, session string) {
	t.Helper()
	if session != "n-"+mon33SvcName {
		t.Logf("refusing to kill a session this test did not create: %q", session)
		return
	}
	if out, err := exec.Command("tmux", "kill-session", "-t", session).CombinedOutput(); err != nil {
		t.Logf("kill-session %s: %v %s", session, err, out)
	}
}

func TestMonitor33_LiveSessionStaysWatchedAcrossToolGeneration(t *testing.T) {
	if phase := os.Getenv(mon33PhaseEnv); phase != "" {
		mon33Child(t, phase)
		return
	}
	root := t.TempDir()
	yamlPath := filepath.Join(root, "tagent.yaml")
	env := append(os.Environ(), mon33YamlEnv+"="+yamlPath)

	// BOOT 1 — start a real session and die WITHOUT Close: the task stays un-settled
	// in the durable chain while its tmux session lives on in the server.
	// the session name this scenario owns is fixed, so the phase log is traceable
	runBootChild(t, env, mon33PhaseEnv+"=spawn", mon33Filter)

	// BOOT 2 — a fresh process with a freshly assembled ActionTool must still watch
	// that live session: adjudication first, then promotion of the tracked task.
	runBootChild(t, env, mon33PhaseEnv+"=restart", mon33Filter)

	// BOOT 3 — the HOT-RELOAD window, in its OWN durable root: everything happens
	// inside one process (spawn → publish → re-attempt), and reusing the root above
	// would hand it a leftover task whose session is long dead (measured).
	hotRoot := t.TempDir()
	hotYaml := filepath.Join(hotRoot, "tagent.yaml")
	runBootChild(t, append(os.Environ(), mon33YamlEnv+"="+hotYaml), mon33PhaseEnv+"=hotreload", mon33Filter)
}

func mon33Boot(t *testing.T, yamlPath string) (*agent.TagentAgent, *mon33Model) {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &mon33Model{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry, m
}

func mon33Child(t *testing.T, phase string) {
	yamlPath := os.Getenv(mon33YamlEnv)
	require.NotEmpty(t, yamlPath)
	root := filepath.Dir(yamlPath)

	switch phase {
	case "spawn":
		require.NoError(t, os.WriteFile(yamlPath, []byte(mon33YAML(root)), 0o644))
		// Idempotent preflight: a named session survives a CRASHED earlier run of
		// this test the same way it survives a restart, and the tool refuses a
		// duplicate name — without this the anchor would stop being re-runnable
		// after any failed run (observed while probing). Only ever our own name.
		killOwnSession(t, "n-"+mon33SvcName)
		entry, _ := mon33Boot(t, yamlPath)
		out, err := entry.StartLoop("u", "mon33-spawn")
		require.NoError(t, err)
		go func() {
			for range out {
			}
		}()
		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start a long job"))
		require.NoError(t, err)

		var session string
		waitFor(t, "the exec task exists with a backing session", func() bool {
			_, session = commandTask(entry)
			return session != ""
		})
		require.Truef(t, tmuxSessionAlive(session),
			"precondition: the tmux server must really be running session %s", session)
		// Let the spawn record reach the durable path, then leave WITHOUT Close so
		// nothing reaps the session and the task never settles.
		time.Sleep(2 * time.Second)
		os.Exit(0)

	case "hotreload":
		require.NoError(t, os.WriteFile(yamlPath, []byte(mon33YAML(root)), 0o644))
		killOwnSession(t, "n-"+mon33SvcName) // idempotent preflight, own name only
		entry, m := mon33Boot(t, yamlPath)
		tick := time.Now().Add(2 * time.Second)
		out, err := entry.StartLoop("u", "mon33-hot")
		require.NoError(t, err)
		go func() {
			for range out {
			}
		}()
		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start the resident service"))
		require.NoError(t, err)

		var session string
		waitFor(t, "the resident session is up under the first generation", func() bool {
			_, session = commandTask(entry)
			return session != ""
		})
		require.Truef(t, tmuxSessionAlive(session), "precondition: %s must be live", session)

		// Publish a NEW generation: the owner's ActionTool is re-assembled.
		require.NoError(t, os.WriteFile(yamlPath, []byte(mon33YAMLAfterPublish(root)), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
		before := m.count()
		entry.CheckOrgReload()
		require.Greater(t, di64(t, entry.OrgDiagnostics(), "generation"), int64(0),
			"precondition: the publish really advanced the generation")

		// The CURRENT generation must still own the live session: asking for the
		// same logical name has to be refused as a duplicate, not answered by a
		// second session nobody was tracking.
		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start it again"))
		require.NoError(t, err)
		waitFor(t, "the second attempt was answered", func() bool { return m.count() > before+1 })

		// Non-vacuity: the post-publish attempt must really have REACHED the tool
		// (two spawns attempted), or the count check below would be satisfiable by
		// a turn that never delegated at all.
		require.Contains(t, m.offeredCalls(), 3,
			"precondition: the current generation must really have been asked to spawn again")

		tasks := 0
		for _, tk := range entry.TaskManager().List() {
			if tk.Spec.Kind == "command" {
				tasks++
			}
		}
		require.Equalf(t, 1, tasks,
			"§3.3「已纳管任务不因工具换代失监视」：换代后的当前代仍须认得那个存活会话（同名第二次派生被当成新会话＝monitor 空、监视已失），实得 command 任务数=%d", tasks)
		require.Truef(t, tmuxSessionAlive(session), "the original session %s must be the one still alive", session)

		killOwnSession(t, session)
		require.NoError(t, entry.Close())
		os.Exit(0)

	case "restart":
		entry, _ := mon33Boot(t, yamlPath)
		tk, session := commandTask(entry)
		require.NotNilf(t, tk,
			"§3.3 端到端：重启必须从事实链折回那个 exec 任务（board=%v）", len(entry.TaskManager().List()))
		require.Truef(t, tmuxSessionAlive(session),
			"precondition: session %s must still be alive for the monitoring claim to mean anything", session)

		// The generation that just booted assembled a NEW ActionTool and reattached
		// the live session BEFORE adjudication; the TaskID bridge therefore promotes
		// the restored suspect back to running. A monitoring signal left on the
		// superseded tool would instead report "untracked" and retire it as a
		// reincarnation orphan (or at best leave it suspect forever).
		//
		// Bounded wait (2026-09-27 audit): under a full `go test ./...` run the
		// machine hosts every package binary at once and the boot's first tmux
		// verification can miss the window; List() re-runs reconcileDetached, so
		// re-fetching exercises the designed re-adjudication path rather than mere
		// patience. Alone / whole-package the promotion lands before the first
		// fetch; a session that never promotes still fails with the same message.
		deadline := time.Now().Add(30 * time.Second)
		for tk.Status() != task.TaskRunning && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
			tk, _ = commandTask(entry)
		}
		require.Equalf(t, task.TaskRunning, tk.Status(),
			"§3.3「已纳管任务不因工具换代失监视」：存活会话 %s 在换代后的新代里必须仍被监视并提升回 running（实得 status=%s）",
			session, string(tk.Status()))

		killOwnSession(t, session)
		require.NoError(t, entry.Close())
		os.Exit(0)

	default:
		t.Fatalf("unknown phase %q", phase)
	}
}
