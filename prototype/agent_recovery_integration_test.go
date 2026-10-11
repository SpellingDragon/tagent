// 职责：用真实子进程（os.Args[0]）验证快照跨进程 Save/Load、owner 恢复与孤儿回收（一级 design 场景⑤）。
// 覆盖：writer 经真实 agent 用 archive 低密度结果写入 Memory 并折叠为票据；reader 从恢复上下文的真实票据
// 经模型发 recall ToolCall 取回全文，与 pending 输入、旧上下文、Complete 重挂结果一起形成最终答复。
// 未实现：崩溃一致性/事务/自动重执行；外部资源重挂用 TaskManager.Complete 显式模拟，是明确的 mock 边界。
// 接线：writer 停壳后 Save（pending 未消费）再 Close 清本进程 mock 任务；reader LoadState →
// ReloadConfig(删 orphan) → NewAgent/Run 回原队列续跑。仅模拟检查点跨进程，不声称崩溃一致。
package prototype

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	phaseEnv = "TAGENT_RECOVERY_PHASE"
	dirEnv   = "TAGENT_RECOVERY_DIR"
)

var ticketRe = regexp.MustCompile(`\[memory:([^\]]+)\]`)

// TestAgentRecoverySubprocess 是子进程入口：父测试用环境变量选择 writer/reader 阶段。
func TestAgentRecoverySubprocess(t *testing.T) {
	phase := os.Getenv(phaseEnv)
	if phase == "" {
		t.Skip("仅在子进程中执行")
	}
	switch dir := os.Getenv(dirEnv); phase {
	case "writer":
		recoveryWriter(t, dir)
	case "reader":
		recoveryReader(t, dir)
	default:
		t.Fatalf("未知恢复阶段 %q", phase)
	}
	t.Logf("RECOVERY_PHASE_DONE=%s", phase) // -test.v 下父进程据此确认该阶段确实执行而非被跳过
}

// TestAgentRecoveryCrossProcess 派两个真实子进程串行执行 write/read 阶段（不得同进程 Clone 冒充）。
// 子进程用 CommandContext + 超时只防挂；父进程按 -test.v 标记断言两阶段都真实跑过。
func TestAgentRecoveryCrossProcess(t *testing.T) {
	if os.Getenv(phaseEnv) != "" {
		t.Skip("子进程模式")
	}
	dir := t.TempDir()
	for _, p := range []string{"writer", "reader"} {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentRecoverySubprocess$", "-test.v")
		cmd.Env = append(os.Environ(), phaseEnv+"="+p, dirEnv+"="+dir)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("子进程 %s 失败: %v\n%s", p, err, out)
		}
		if !strings.Contains(string(out), "RECOVERY_PHASE_DONE="+p) {
			t.Fatalf("子进程 %s 未实际执行该阶段（可能被跳过）:\n%s", p, out)
		}
	}
}

// recoveryWriter：经真实 agent 让 archive 低密度结果进 Memory 并折成票据，再派一个 running slow 任务；
// 停消费壳后放待处理 input，Save 退出，最后 Close 清本进程 mock 任务（快照已落盘，不重写）。
func recoveryWriter(t *testing.T, dir string) {
	long := "ARCHIVE-" + strings.Repeat("y", 200) + bodyMark
	cfg := writeCfg(t, dir, "w.json", Config{Agents: []AgentConfig{
		{ID: "owner", Model: "m", Tools: []string{"archive", "slow"}, Plugins: []string{"memo"}},
		{ID: "orphan", Model: "om", Tools: []string{"noop"}},
	}})
	held := make(chan struct{}) // writer 不放闸：slow 保持 running 至快照保存
	started := make(chan struct{}, 1)
	sawFull := make(chan struct{}, 1)
	noop := make(chan struct{}, 1)
	registry := Registry{
		Models: map[string]ModelFunc{
			"m": func(_ context.Context, req ModelRequest) (ModelOutput, error) {
				switch j := joined(req); {
				case strings.Contains(j, bodyMark): // 归档全文这一轮确实进了模型输入
					select {
					case sawFull <- struct{}{}:
					default:
					}
					return ModelOutput{}, nil
				case strings.Contains(j, "slow-kick"):
					return toolcall("slow", ""), nil
				case strings.Contains(j, "seed"):
					return toolcall("archive", ""), nil
				}
				return ModelOutput{}, nil
			},
			"om": func(_ context.Context, req ModelRequest) (ModelOutput, error) {
				j := joined(req)
				if strings.Contains(j, "noop-out") {
					return ModelOutput{}, nil
				}
				if strings.Contains(j, "orphan-seed") {
					return toolcall("noop", ""), nil
				}
				return ModelOutput{}, nil
			},
		},
		Tools: map[string]ToolFunc{
			"archive": constTool(long),
			"slow": func(ctx context.Context, _ string) (string, error) {
				select {
				case started <- struct{}{}:
				default:
				}
				select {
				case <-held:
					return "slow-result", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			},
			"noop": func(context.Context, string) (string, error) {
				select {
				case noop <- struct{}{}:
				default:
				}
				return "noop-out", nil
			},
		},
		Plugins: map[string]func(*AgentState) Plugin{
			"memo": func(o *AgentState) Plugin {
				return ResultMemoryPlugin(o.Memory, func(e Event) bool { return e.Call.Name == "archive" })
			},
		},
	}

	state := NewState()
	if _, err := ReloadConfig(cfg, state, registry, Evolution{}); err != nil {
		t.Fatalf("writer reload: %v", err)
	}
	owner, err := state.Agent("owner") // 现场句柄：读 Context/Memory/Tasks
	if err != nil {
		t.Fatalf("writer owner: %v", err)
	}
	if sp := owner.SystemPrompt(); !strings.Contains(sp, "[tools] archive,slow") {
		t.Fatalf("writer 生效 prompt 异常: %q", sp)
	}
	agent, err := NewAgent("owner", state, registry, nil)
	if err != nil {
		t.Fatalf("writer agent: %v", err)
	}
	co, do := launch(t, agent)
	if err := agent.Input(context.Background(), "seed"); err != nil {
		t.Fatalf("writer seed: %v", err)
	}
	await(t, sawFull) // 模型确已看到归档全文，再驱动慢任务
	if err := agent.Input(context.Background(), "slow-kick"); err != nil {
		t.Fatalf("writer slow-kick: %v", err)
	}
	await(t, started)
	halt(t, co, do) // 停消费壳；在途任务作为 owner 资源保留

	es := owner.Context()
	var folded bool
	for _, e := range es {
		if e.Type == EventToolResult && e.Meta["memory_key"] != "" && strings.Contains(e.Data, "[memory:") {
			folded = true
		}
	}
	if !hasData(es, "seed") || !folded {
		t.Fatalf("writer 上下文未保留旧输入/折叠票据: %+v", es)
	}
	if len(owner.Memory.Snapshot()) == 0 {
		t.Fatal("writer Memory 为空，跨进程 recall 无源")
	}
	if !anyTask(owner.Tasks.Snapshot(), func(tk Task) bool { return tk.Call.Name == "slow" && tk.Status == "running" }) {
		t.Fatalf("writer slow 应仍在途: %+v", owner.Tasks.Snapshot())
	}
	if err := agent.Input(context.Background(), "pending"); err != nil { // 停壳后放待处理 input：尚未消费
		t.Fatalf("writer pending: %v", err)
	}

	// orphan 也建出现场（context + 一个已完成任务），随后由 reader 的配置回收。
	orphan, err := NewAgent("orphan", state, registry, nil)
	if err != nil {
		t.Fatalf("writer orphan: %v", err)
	}
	cp, dp := launch(t, orphan)
	if err := orphan.Input(context.Background(), "orphan-seed"); err != nil {
		t.Fatalf("writer orphan-seed: %v", err)
	}
	await(t, noop)
	halt(t, cp, dp)
	if err := orphan.Input(context.Background(), "orphan-pending"); err != nil {
		t.Fatalf("writer orphan-pending: %v", err)
	}
	if err := state.Save(filepath.Join(dir, "snap.json")); err != nil {
		t.Fatalf("writer save: %v", err)
	}
	state.Close() // 清理本进程 mock 任务；快照已落盘，不重写
}

// recoveryReader：在另一进程 LoadState，保留有 owner 的现场、回收无 owner 的；再从恢复上下文的真实
// 票据经模型发 recall（非测试直调 Memory），实际 Recall 结果回流模型，与 pending/旧上下文/重挂结果
// 合成最终答复。慢任务 id 按 Call.Name 定位，不假定为 1。
func recoveryReader(t *testing.T, dir string) {
	state, err := LoadState(filepath.Join(dir, "snap.json"))
	if err != nil {
		t.Fatalf("reader load: %v", err)
	}
	owner, err := state.Agent("owner")
	if err != nil {
		t.Fatalf("reader owner: %v", err)
	}
	orphan, err := state.Agent("orphan")
	if err != nil {
		t.Fatalf("reader orphan: %v", err)
	}
	// 生效 prompt 随快照恢复：此刻尚未提供 registry，读到快照值即证明 state 自持、无 registry 读取。
	if sp := owner.SystemPrompt(); !strings.Contains(sp, "[tools] archive,slow") {
		t.Fatalf("恢复现场未带出生效 system prompt: %q", sp)
	}
	es := owner.Context()
	if !hasData(es, "seed") {
		t.Fatalf("owner 旧上下文未恢复: %+v", es)
	}
	var ticket string
	for _, e := range es {
		if e.Type == EventToolResult {
			if m := ticketRe.FindStringSubmatch(e.Data); m != nil {
				ticket = m[1]
			}
		}
	}
	if ticket == "" {
		t.Fatalf("恢复上下文未带 memory 票据: %+v", es)
	}
	tasks := owner.Tasks.Snapshot()
	var slow *Task
	for i := range tasks {
		if tasks[i].Call.Name == "slow" {
			slow = &tasks[i]
		}
	}
	if slow == nil || slow.Status != "unknown" || slow.OwnerID != "owner" {
		t.Fatalf("running 未恢复为 unknown 且保留归属: %+v", tasks)
	}
	unknownID := slow.ID
	if len(orphan.Tasks.Snapshot()) == 0 {
		t.Fatalf("orphan 现场应带任务恢复: %+v", orphan.Tasks.Snapshot())
	}

	cfg := writeCfg(t, dir, "r.json", Config{Agents: []AgentConfig{{ID: "owner", Model: "m", Tools: []string{"recall", "slow"}}}})
	var slowCalls int32
	registry := Registry{
		Models: map[string]ModelFunc{"m": func(_ context.Context, req ModelRequest) (ModelOutput, error) {
			j := joined(req)
			tk := ticketRe.FindStringSubmatch(j)
			full := strings.Contains(j, bodyMark)
			switch {
			case full && tk != nil && strings.Contains(j, "reattached") && lastInput(req) != "":
				// 投递任务化后，Reply 会 spawn 一条 deliver 任务，其空结果（Data=""）会再驱动一次模型；
				// 用 lastInput != "" 排除该空驱动（召回全文/重挂结果都是非空），避免无限重投。
				var parts []string // 最终答复直接由模型所见拼成，自证消费了旧上下文/待处理/召回/重挂
				if hasStr(req.Inputs, "seed") {
					parts = append(parts, "seed")
				}
				if hasStr(req.Inputs, "pending") {
					parts = append(parts, "pending")
				}
				return out("final[" + strings.Join(append(parts, "recall", "reattach"), "+") + "]"), nil
			case tk != nil && !full: // 票据在（来自快照），全文未在 → 经真实 recall 工具取回
				return toolcall("recall", tk[1]), nil
			}
			return ModelOutput{}, nil
		}},
		Tools: map[string]ToolFunc{"slow": func(context.Context, string) (string, error) {
			atomic.AddInt32(&slowCalls, 1)
			return "should-not-run", nil
		}},
	}

	removed, err := ReloadConfig(cfg, state, registry, Evolution{})
	if err != nil {
		t.Fatalf("reader reload: %v", err)
	}
	if len(removed) != 1 || removed[0] != "orphan" {
		t.Fatalf("孤儿回收 id 不符: %v", removed)
	}
	if _, err := state.Agent("orphan"); err == nil {
		t.Fatal("orphan 的 queue/context/tasks 未被回收")
	}
	if _, err := state.Agent("owner"); err != nil {
		t.Fatalf("owner 被误回收（非按年龄判僵尸）: %v", err)
	}

	replies := make(chan string, 8)
	agent, err := NewAgent("owner", state, registry, deliver(replies))
	if err != nil {
		t.Fatalf("reader agent: %v", err)
	}
	cancel, done := launch(t, agent)
	defer halt(t, cancel, done)

	owner.Tasks.Complete(unknownID, "reattached", nil) // 模拟外部资源重挂回报（明确 mock 边界）
	if got := recv(t, replies); got != "final[seed+pending+recall+reattach]" {
		t.Fatalf("恢复续跑最终答复不符: %q", got)
	}
	if n := atomic.LoadInt32(&slowCalls); n != 0 {
		t.Fatalf("unknown 任务被自动重执行: slow 被调 %d 次", n)
	}
}
