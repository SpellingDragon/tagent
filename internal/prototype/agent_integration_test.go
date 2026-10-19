// 职责：以配置 JSON → NewAgent/Run/Input → 真实插件/任务/模型调用 → Reply 的集成路径验证一级 design 场景①②③④⑥⑦⑧。
// 未实现：真实 provider/工具/网络；模型与工具均为标准库 mock，不 mock 核心管线，也不为 helper 写逐函数单测。
// 接线：ReloadConfig/NewAgent 装配 → BaseTAgent.Run 消费事件 → 观察 Deliver 任务、Registry.Record 轨迹与 State.Context。
// 契约: docs/wiki/agent/prototype-skeleton.md#six-pieces
package prototype

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

var bg = context.Background()

func writeCfg(t *testing.T, dir, name string, cfg Config) string {
	t.Helper()
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	return p
}

func launch(t *testing.T, a *BaseTAgent) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(bg)
	d := make(chan error, 1)
	go func() { d <- a.Run(ctx) }()
	return cancel, d
}

func halt(t *testing.T, cancel context.CancelFunc, d <-chan error) {
	t.Helper()
	cancel()
	select {
	case <-d:
	case <-time.After(3 * time.Second):
		t.Fatal("Run 未在超时内返回")
	}
}

// deliver 构造 ToolFunc 形态的投递执行体：EventReply 经 owner 任务表 Spawn 成 deliver 任务后调用它。
func deliver(ch chan<- string) ToolFunc {
	return func(_ context.Context, data string) (string, error) { ch <- data; return "", nil }
}

func recv(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("等待通道超时")
		return ""
	}
}

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("等待信号超时")
	}
}

func waitErr(t *testing.T, d <-chan error) error {
	t.Helper()
	select {
	case e := <-d:
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("等待 Run 结束超时")
		return nil
	}
}

func joined(r ModelRequest) string     { return strings.Join(r.Inputs, "\n") }
func out(s string) ModelOutput         { return ModelOutput{Output: s} }
func toolcall(n, a string) ModelOutput { return ModelOutput{ToolCalls: []ToolCall{{Name: n, Args: a}}} }
func constTool(v string) ToolFunc {
	return func(context.Context, string) (string, error) { return v, nil }
}
func hasStr(xs []string, w string) bool { return anyStr(xs, func(s string) bool { return s == w }) }
func anyStr(xs []string, f func(string) bool) bool {
	for _, x := range xs {
		if f(x) {
			return true
		}
	}
	return false
}
func anyTask(ts []Task, f func(Task) bool) bool {
	for _, tk := range ts {
		if f(tk) {
			return true
		}
	}
	return false
}
func hasData(es []Event, d string) bool {
	for _, e := range es {
		if e.Data == d {
			return true
		}
	}
	return false
}

// lastInput 取模型请求里最后一条上下文事件的数据，供 mock 对“本轮触发”收敛：投递任务结果的空串再驱动一次模型时
// 不命中任何触发词，重投不会自激。
func lastInput(r ModelRequest) string {
	if len(r.Inputs) == 0 {
		return ""
	}
	return r.Inputs[len(r.Inputs)-1]
}

// waitFor 轮询等待条件成立（3s 超时），替代对事件送达时序的固定 sleep 断言。
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待条件超时")
}

// TestAgentPipelineAsyncDispatch 验证异步管线：派发即回、快慢任务互不阻塞，插件在 Before/After 看到真实派发。
//
// - 慢任务在途时新输入与快结果仍驱动模型 Reply，同批收齐门不存在。
// - mock 按“新出现的触发词”收敛：递送任务结果的空串与触发词同批到达时 lastInput 会丢失触发语义，seen 集合对任意批序稳定。
func TestAgentPipelineAsyncDispatch(t *testing.T) {
	cfg := writeCfg(t, t.TempDir(), "p.json", Config{Agents: []AgentConfig{{
		ID: "main", Model: "m", Tools: []string{"slow", "fast"}, Plugins: []string{"probe"},
	}}})
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	probe := make(chan string, 8)
	var mu sync.Mutex
	var beforeReg, afterReg bool
	var seenMu sync.Mutex
	seen := map[string]bool{}
	reg := Registry{
		Models: map[string]ModelFunc{"m": func(_ context.Context, r ModelRequest) (ModelOutput, error) {
			seenMu.Lock()
			pick := ""
			for _, in := range r.Inputs {
				switch in {
				case "start", "ping", "kickfast", "slow-result", "fast-result":
					if !seen[in] {
						seen[in] = true
						pick = in
					}
				}
			}
			seenMu.Unlock()
			switch pick {
			case "slow-result":
				return out("slow-done"), nil
			case "fast-result":
				return out("fast-done"), nil
			case "kickfast":
				return toolcall("fast", ""), nil
			case "ping":
				return out("pong"), nil
			case "start":
				return toolcall("slow", ""), nil
			}
			return ModelOutput{}, nil
		}},
		Tools: map[string]ToolFunc{
			"slow": func(ctx context.Context, _ string) (string, error) {
				select {
				case started <- struct{}{}:
				default:
				}
				select {
				case <-release:
					return "slow-result", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			},
			"fast": constTool("fast-result"),
		},
		Plugins: map[string]func(*AgentState) Plugin{"probe": func(o *AgentState) Plugin {
			rec := func(before bool, e Event) {
				found := anyTask(o.Tasks.Snapshot(), func(tk Task) bool { return tk.Call.Name == e.Call.Name })
				if e.Call.Name == "slow" {
					mu.Lock()
					if before {
						beforeReg = beforeReg || !found
					} else {
						afterReg = afterReg || found
					}
					mu.Unlock()
				}
				if !before {
					select {
					case probe <- e.Call.Name:
					default:
					}
				}
			}
			return Plugin{Kinds: []EventType{EventToolCall},
				Before: func(_ context.Context, e Event) (Event, error) { rec(true, e); return e, nil },
				After:  func(_ context.Context, e Event) (Event, error) { rec(false, e); return e, nil }}
		}},
	}

	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(cfg, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	replies := make(chan string, 16)
	a, err := NewAgent("main", state, reg, deliver(replies))
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	cancel, done := launch(t, a)
	defer halt(t, cancel, done)

	if err := a.Input(bg, "start"); err != nil {
		t.Fatal(err)
	}
	if n := recv(t, probe); n != "slow" {
		t.Fatalf("期望派发 slow，实得 %q", n)
	}
	await(t, started)
	if err := a.Input(bg, "ping"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, replies); got != "pong" {
		t.Fatalf("慢任务期间期望 pong，实得 %q", got)
	}
	if err := a.Input(bg, "kickfast"); err != nil {
		t.Fatal(err)
	}
	if n := recv(t, probe); n != "fast" {
		t.Fatalf("期望派发 fast，实得 %q", n)
	}
	if got := recv(t, replies); got != "fast-done" {
		t.Fatalf("快结果期望 fast-done，实得 %q", got)
	}
	owner, _ := state.Agent("main")
	if !anyTask(owner.Tasks.Snapshot(), func(tk Task) bool { return tk.Call.Name == "slow" && tk.Status == "running" }) {
		t.Fatal("slow 应仍在途（快慢互不阻塞）")
	}
	close(release)
	if got := recv(t, replies); got != "slow-done" {
		t.Fatalf("放闸后期望 slow-done，实得 %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if !beforeReg || !afterReg {
		t.Fatalf("插件未观察到真实派发（Before 未登记=%v / After 已登记=%v）", beforeReg, afterReg)
	}
}

const bodyMark = "-BODY-END"

// TestAgentSelectiveAdapters 验证选择性插件与低密度结果票据化：插件按 Kinds 闭包变换，大结果外置后经 recall 取回。
func TestAgentSelectiveAdapters(t *testing.T) {
	cfg := writeCfg(t, t.TempDir(), "a.json", Config{Agents: []AgentConfig{{
		ID: "main", Model: "m", Tools: []string{"big", "recall"}, Plugins: []string{"memo", "audit", "post"},
	}}})
	long := "BEGIN-" + strings.Repeat("x", 200) + bodyMark
	ticket := regexp.MustCompile(`\[memory:([^\]]+)\]`)
	sawFull := make(chan struct{}, 1)
	var mu sync.Mutex
	var traces []ModelTrace
	reg := Registry{
		Models: map[string]ModelFunc{"m": func(_ context.Context, r ModelRequest) (ModelOutput, error) {
			j := joined(r)
			tk := ticket.FindStringSubmatch(j)
			full := strings.Contains(j, bodyMark)
			switch {
			case tk != nil && full && !strings.Contains(j, "recovered"):
				return out("recovered"), nil
			case tk != nil && !full:
				return toolcall("recall", tk[1]), nil
			case full:
				select {
				case sawFull <- struct{}{}:
				default:
				}
				return ModelOutput{}, nil
			case strings.Contains(j, "fetch"):
				return toolcall("big", ""), nil
			}
			return ModelOutput{}, nil
		}},
		Tools: map[string]ToolFunc{"big": constTool(long)},
		Plugins: map[string]func(*AgentState) Plugin{
			"memo": func(o *AgentState) Plugin {
				return ResultMemoryPlugin(o.Memory, func(e Event) bool { return e.Call.Name == "big" })
			},
			"audit": func(*AgentState) Plugin {
				return Plugin{Kinds: []EventType{EventInput},
					Before: func(_ context.Context, e Event) (Event, error) {
						e.Meta = map[string]string{"audit": "b"}
						return e, nil
					},
					After: func(_ context.Context, e Event) (Event, error) { e.Meta["audit"] = "a"; return e, nil }}
			},
			"post": func(*AgentState) Plugin {
				return Plugin{Kinds: []EventType{EventReply},
					Before: func(_ context.Context, e Event) (Event, error) { e.Data += "|b"; return e, nil },
					After:  func(_ context.Context, e Event) (Event, error) { e.Data += "|a"; return e, nil }}
			},
		},
		Record: func(tr ModelTrace) { mu.Lock(); traces = append(traces, tr); mu.Unlock() },
	}

	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(cfg, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	replies := make(chan string, 16)
	a, err := NewAgent("main", state, reg, deliver(replies))
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	cancel, done := launch(t, a)
	defer halt(t, cancel, done)
	if err := a.Input(bg, "fetch"); err != nil {
		t.Fatal(err)
	}
	await(t, sawFull)
	if err := a.Input(bg, "again"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, replies); got != "recovered|b|a" {
		t.Fatalf("票据 recall 链路期望 recovered|b|a，实得 %q", got)
	}

	mu.Lock()
	tr := append([]ModelTrace(nil), traces...)
	mu.Unlock()
	var first, tick, back bool
	for _, x := range tr {
		j := joined(x.Request)
		has := strings.Contains(j, "[memory:")
		tick = tick || has
		if hasStr(x.Request.Inputs, long) {
			first = first || (!has && x.Request.Inputs[0] == "fetch")
			back = back || has
		}
	}
	if !first || !tick || !back {
		t.Fatalf("选择性适配失败 first=%v ticket=%v recall=%v", first, tick, back)
	}

	owner, _ := state.Agent("main")
	es := owner.Context()
	var fe, bigR, recR *Event
	for i := range es {
		switch {
		case es[i].Type == EventInput && es[i].Data == "fetch":
			fe = &es[i]
		case es[i].Type == EventToolResult && es[i].Meta["memory_key"] != "":
			bigR = &es[i]
		case es[i].Type == EventToolResult && es[i].Call.Name == "recall":
			recR = &es[i]
		}
	}
	if fe == nil || fe.Meta["audit"] != "a" || fe.Meta["memory_key"] != "" {
		t.Fatalf("输入应原样保留且未被内存插件标记: %+v", fe)
	}
	if bigR == nil || !strings.Contains(bigR.Data, "[memory:") || strings.Contains(bigR.Data, bodyMark) {
		t.Fatalf("低密度结果未折叠为短预览+票据: %+v", bigR)
	}
	if recR == nil || recR.Data != long {
		t.Fatalf("recall 应取回全文: %+v", recR)
	}
}

// TestAgentEvolutionOnlyOnConfigChange 验证演进只在架构变更时被调：Reload 过三段，普通工具执行不过。
func TestAgentEvolutionOnlyOnConfigChange(t *testing.T) {
	dir := t.TempDir()
	one := func(m string) Config {
		return Config{Agents: []AgentConfig{{ID: "main", Model: m, Tools: []string{"rm"}}}}
	}
	v1 := writeCfg(t, dir, "v1.json", one("m1"))
	v2 := writeCfg(t, dir, "v2.json", one("m2"))

	rmDone := make(chan struct{}, 1)
	model := func(_ context.Context, r ModelRequest) (ModelOutput, error) {
		switch lastInput(r) {
		case "rm-ok":
			return out("rm-seen"), nil
		case "go":
			return toolcall("rm", ""), nil
		}
		return ModelOutput{}, nil
	}
	reg := Registry{
		Models: map[string]ModelFunc{"m1": model, "m2": model},
		Tools: map[string]ToolFunc{"rm": func(context.Context, string) (string, error) {
			select {
			case rmDone <- struct{}{}:
			default:
			}
			return "rm-ok", nil
		}},
	}
	var mu sync.Mutex
	var plans []StatePlan
	evo := func(reject bool) Evolution {
		return Evolution{Eval: func(p StatePlan) error {
			mu.Lock()
			plans = append(plans, p)
			mu.Unlock()
			if reject {
				return errors.New("演进拒绝")
			}
			return nil
		}}
	}

	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(v1, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload v1: %v", err)
	}
	owner, _ := state.Agent("main")
	if owner.Definition().Model != "m1" {
		t.Fatalf("初始模型应为 m1，实得 %q", owner.Definition().Model)
	}
	if _, err := ReloadConfig(v2, state, reg, evo(true)); err == nil {
		t.Fatal("演进拒绝应使 ReloadConfig 报错")
	}
	if owner.Definition().Model != "m1" {
		t.Fatal("拒绝后定义被改动")
	}
	mu.Lock()
	c1 := append([]StatePlan(nil), plans...)
	mu.Unlock()
	if len(c1) != 1 || c1[0].Change.Before.Agents[0].Model != "m1" || c1[0].Change.After.Agents[0].Model != "m2" {
		t.Fatalf("演进段未取得 Before/After: %+v", c1)
	}
	if _, err := ReloadConfig(v2, state, reg, evo(false)); err != nil {
		t.Fatalf("reload v2: %v", err)
	}
	if owner.Definition().Model != "m2" {
		t.Fatal("替换未生效")
	}
	mu.Lock()
	base := len(plans)
	mu.Unlock()

	replies := make(chan string, 8)
	a, err := NewAgent("main", state, reg, deliver(replies))
	if err != nil {
		t.Fatal(err)
	}
	cancel, done := launch(t, a)
	defer halt(t, cancel, done)
	if err := a.Input(bg, "go"); err != nil {
		t.Fatal(err)
	}
	await(t, rmDone)
	if got := recv(t, replies); got != "rm-seen" {
		t.Fatalf("rm 结果未回流模型: %q", got)
	}
	mu.Lock()
	now := len(plans)
	mu.Unlock()
	if now != base {
		t.Fatalf("普通工具执行触发了演进: base=%d now=%d", base, now)
	}
	if snap := owner.Tasks.Snapshot(); !anyTask(snap, func(tk Task) bool { return tk.Call.Name == "rm" && tk.Status == "completed" }) {
		t.Fatalf("rm 任务未真实执行: %+v", snap)
	}
}

// twinRegistry 提供共用的 model/tool/skill，让一体双生管道测试的配置通过 validateRegistry。
func twinRegistry(rec func(ModelTrace)) Registry {
	model := func(context.Context, ModelRequest) (ModelOutput, error) { return ModelOutput{}, nil }
	return Registry{
		Models: map[string]ModelFunc{"m1": model, "m2": model, "m3": model},
		Tools:  map[string]ToolFunc{"t": constTool("t-out")},
		Skills: map[string]string{"sk": "SKILL"},
		Record: rec,
	}
}

// twinConfigs 写两份配置：v1（keep+gone），v2 更新 keep、新增 added、移除 gone。
func twinConfigs(t *testing.T, dir string) (v1, v2 string, cfgV1, cfgV2 Config) {
	t.Helper()
	cfgV1 = Config{Agents: []AgentConfig{
		{ID: "keep", Model: "m1", SystemPrompt: "keep-sys", Tools: []string{"t"}, Skills: []string{"sk"}},
		{ID: "gone", Model: "m1", Tools: []string{"t"}},
	}}
	cfgV2 = Config{Agents: []AgentConfig{
		{ID: "keep", Model: "m2", SystemPrompt: "keep-new", Tools: []string{"t"}, Skills: []string{"sk"}},
		{ID: "added", Model: "m3", SystemPrompt: "added-sys", Tools: []string{"t"}},
	}}
	v1 = writeCfg(t, dir, "v1.json", cfgV1)
	v2 = writeCfg(t, dir, "v2.json", cfgV2)
	return v1, v2, cfgV1, cfgV2
}

// TestAgentTwinTruthyPipeline：空 Evolution{} 三段恒真，合法变更提交后 state 按计划更新。
func TestAgentTwinTruthyPipeline(t *testing.T) {
	dir := t.TempDir()
	reg := twinRegistry(nil)
	v1, v2, _, _ := twinConfigs(t, dir)

	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(v1, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload v1: %v", err)
	}
	removed, err := ReloadConfig(v2, state, reg, Evolution{})
	if err != nil {
		t.Fatalf("恒真管道提交失败: %v", err)
	}
	if len(removed) != 1 || removed[0] != "gone" {
		t.Fatalf("回收 id 不符: %v", removed)
	}
	keep, err := state.Agent("keep")
	if err != nil {
		t.Fatal(err)
	}
	if keep.Definition().Model != "m2" {
		t.Fatalf("同 id 定义未更新: %+v", keep.Definition())
	}
	if _, err := state.Agent("added"); err != nil {
		t.Fatalf("新 id 未建立现场: %v", err)
	}
	if _, err := state.Agent("gone"); err == nil {
		t.Fatal("旧 id 未被回收")
	}
}

// TestAgentTwinEvolutionRejectKeepsState 验证任一段拒绝时变更整体不生效。
//
// - Eval/Benchmark/ApplyGate 任一拒绝均使定义、生效 prompt、现场（context/任务）与 Removes 保持不变，错误上抛。
func TestAgentTwinEvolutionRejectKeepsState(t *testing.T) {
	dir := t.TempDir()
	reg := twinRegistry(nil)
	v1, v2, _, _ := twinConfigs(t, dir)
	reject := errors.New("演进拒绝")

	cases := []struct {
		name string
		evo  Evolution
	}{
		{"Eval", Evolution{Eval: func(StatePlan) error { return reject }}},
		{"Benchmark", Evolution{Benchmark: func(StatePlan) error { return reject }}},
		{"ApplyGate", Evolution{ApplyGate: func(StatePlan) error { return reject }}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := NewState()
			defer state.Close()
			if _, err := ReloadConfig(v1, state, reg, Evolution{}); err != nil {
				t.Fatalf("reload v1: %v", err)
			}
			keep, _ := state.Agent("keep")
			keep.Append(Event{Type: EventInput, Data: "scene"})
			keep.Tasks.Spawn(ToolCall{Name: "t"}, constTool("t-out"), "user")
			defBefore := keep.Definition()
			promptBefore := keep.SystemPrompt()
			ctxBefore := len(keep.Context())
			tasksBefore := len(keep.Tasks.Snapshot())

			_, err := ReloadConfig(v2, state, reg, tc.evo)
			if err == nil {
				t.Fatal("演进拒绝应使 ReloadConfig 报错")
			}
			if !errors.Is(err, reject) {
				t.Fatalf("错误未上抛原始拒绝: %v", err)
			}
			if got := keep.Definition(); got.Model != defBefore.Model || got.SystemPrompt != defBefore.SystemPrompt {
				t.Fatalf("拒绝后定义被改动: %+v", got)
			}
			if keep.SystemPrompt() != promptBefore {
				t.Fatalf("拒绝后生效 prompt 被改动: %q -> %q", promptBefore, keep.SystemPrompt())
			}
			if len(keep.Context()) != ctxBefore || len(keep.Tasks.Snapshot()) != tasksBefore {
				t.Fatalf("拒绝后现场被改动: ctx %d->%d tasks %d->%d",
					ctxBefore, len(keep.Context()), tasksBefore, len(keep.Tasks.Snapshot()))
			}
			if _, err := state.Agent("gone"); err != nil {
				t.Fatalf("被拒绝的移除仍发生: %v", err)
			}
			if _, err := state.Agent("added"); err == nil {
				t.Fatal("被拒绝的新增仍发生")
			}
		})
	}
}

// TestAgentTwinEvolutionSegmentOrderAndPlan 验证三段调用顺序固定为 Eval→Benchmark→ApplyGate，且各段收到一致的 StatePlan。
//
// - 各段看到的 Adds/Updates/Removes/Prompts（含新 prompt）一致正确。
// - 声明顺序打乱不影响执行顺序。
func TestAgentTwinEvolutionSegmentOrderAndPlan(t *testing.T) {
	dir := t.TempDir()
	reg := twinRegistry(nil)
	v1, v2, _, cfgV2 := twinConfigs(t, dir)

	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(v1, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload v1: %v", err)
	}

	var mu sync.Mutex
	var order []string
	seen := map[string]StatePlan{}
	capture := func(name string) func(StatePlan) error {
		return func(p StatePlan) error {
			mu.Lock()
			order = append(order, name)
			seen[name] = p
			mu.Unlock()
			return nil
		}
	}
	evo := Evolution{ApplyGate: capture("ApplyGate"), Benchmark: capture("Benchmark"), Eval: capture("Eval")}
	if _, err := ReloadConfig(v2, state, reg, evo); err != nil {
		t.Fatalf("reload v2: %v", err)
	}

	mu.Lock()
	got := append([]string(nil), order...)
	plansBySeg := map[string]StatePlan{"Eval": seen["Eval"], "Benchmark": seen["Benchmark"], "ApplyGate": seen["ApplyGate"]}
	mu.Unlock()

	if strings.Join(got, ",") != "Eval,Benchmark,ApplyGate" {
		t.Fatalf("段序不符: %v", got)
	}
	expectPrompts := map[string]string{}
	for _, def := range cfgV2.Agents {
		expectPrompts[def.ID] = buildSystem(def, reg)
	}
	if !strings.Contains(expectPrompts["keep"], "keep-new") || !strings.Contains(expectPrompts["keep"], "SKILL") {
		t.Fatalf("预期 prompt 未含新值: %q", expectPrompts["keep"])
	}
	for _, name := range []string{"Eval", "Benchmark", "ApplyGate"} {
		p := plansBySeg[name]
		if len(p.Adds) != 1 || p.Adds[0].ID != "added" {
			t.Fatalf("%s Adds 不符: %+v", name, p.Adds)
		}
		if len(p.Updates) != 1 || p.Updates[0].ID != "keep" || p.Updates[0].Model != "m2" {
			t.Fatalf("%s Updates 不符: %+v", name, p.Updates)
		}
		if len(p.Removes) != 1 || p.Removes[0] != "gone" {
			t.Fatalf("%s Removes 不符: %+v", name, p.Removes)
		}
		for id, want := range expectPrompts {
			if p.Prompts[id] != want {
				t.Fatalf("%s Prompts[%s] 不符: %q != %q", name, id, p.Prompts[id], want)
			}
		}
		if len(p.Change.Before.Agents) != 2 || len(p.Change.After.Agents) != 2 {
			t.Fatalf("%s Change 声明面不符: %+v", name, p.Change)
		}
	}
}

// TestAgentSystemPromptOwnedByState 验证生效 prompt 由 state 持有，是唯一真源。
//
// - Reload 后 owner.SystemPrompt() 含 skill 内容与 tools 行，且与模型 trace 一致。
// - 仅改 registry.Skills 不 Reload 时请求仍用旧值；再次 Reload 后用新值。
func TestAgentSystemPromptOwnedByState(t *testing.T) {
	dir := t.TempDir()
	cfg := writeCfg(t, dir, "p.json", Config{Agents: []AgentConfig{{
		ID: "main", Model: "m", SystemPrompt: "SYS", Skills: []string{"sk"}, Tools: []string{"t"},
	}}})

	var mu sync.Mutex
	var traces []ModelTrace
	reg := Registry{
		Models: map[string]ModelFunc{"m": func(context.Context, ModelRequest) (ModelOutput, error) {
			return out("ok"), nil
		}},
		Tools:  map[string]ToolFunc{"t": constTool("t-out")},
		Skills: map[string]string{"sk": "SKILL-OLD"},
		Record: func(tr ModelTrace) { mu.Lock(); traces = append(traces, tr); mu.Unlock() },
	}

	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(cfg, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	owner, _ := state.Agent("main")
	wantOld := buildSystem(owner.Definition(), reg)
	if owner.SystemPrompt() != wantOld {
		t.Fatalf("生效 prompt 不符: %q != %q", owner.SystemPrompt(), wantOld)
	}
	if !strings.Contains(owner.SystemPrompt(), "SKILL-OLD") || !strings.Contains(owner.SystemPrompt(), "[tools] t") {
		t.Fatalf("生效 prompt 未含 skill/tools: %q", owner.SystemPrompt())
	}
	agent, err := NewAgent("main", state, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func() string {
		t.Helper()
		if _, err := agent.ModelCompletion(bg, []string{"hello"}); err != nil {
			t.Fatalf("model: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		return traces[len(traces)-1].Request.SystemPrompt
	}

	if got := call(); got != owner.SystemPrompt() {
		t.Fatalf("模型 trace 的 SystemPrompt 与 state 不符: %q != %q", got, owner.SystemPrompt())
	}
	reg.Skills["sk"] = "SKILL-NEW"
	if got := call(); got != wantOld {
		t.Fatalf("未 Reload 却改变了请求 prompt: %q", got)
	}
	if _, err := ReloadConfig(cfg, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload again: %v", err)
	}
	wantNew := owner.SystemPrompt()
	if !strings.Contains(wantNew, "SKILL-NEW") {
		t.Fatalf("再次 Reload 后生效 prompt 未更新: %q", wantNew)
	}
	if got := call(); got != wantNew {
		t.Fatalf("再次 Reload 后模型 trace 未随 state 更新: %q != %q", got, wantNew)
	}
}

// TestAgentHotReloadKeepsLiveState 验证同 id 热加载复用现场：定义与生效 prompt 换新，队列/上下文/任务不动。
//
// - 停旧壳不收敛在途任务；换装后晚到结果由新壳消费。
// - m2 mock 只认 slow-result：递送任务结果的空串再驱动时不命中，避免重复投递。
func TestAgentHotReloadKeepsLiveState(t *testing.T) {
	dir := t.TempDir()
	mk := func(m, sys, skill string, temp float64) Config {
		return Config{Agents: []AgentConfig{{ID: "main", Model: m, SystemPrompt: sys, Skills: []string{skill},
			Params: map[string]float64{"temperature": temp}, Tools: []string{"slow"}}}}
	}
	v1 := writeCfg(t, dir, "v1.json", mk("m1", "old-sys", "s1", 0.1))
	v2 := writeCfg(t, dir, "v2.json", mk("m2", "new-sys", "s2", 0.9))

	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var mu sync.Mutex
	var reqs []ModelRequest
	capture := func(f func(ModelRequest) ModelOutput) ModelFunc {
		return func(_ context.Context, r ModelRequest) (ModelOutput, error) {
			mu.Lock()
			reqs = append(reqs, r)
			mu.Unlock()
			return f(r), nil
		}
	}
	reg := Registry{
		Models: map[string]ModelFunc{
			"m1": capture(func(r ModelRequest) ModelOutput {
				if lastInput(r) == "start" {
					return toolcall("slow", "")
				}
				return ModelOutput{}
			}),
			"m2": capture(func(r ModelRequest) ModelOutput {
				if lastInput(r) == "slow-result" {
					return out("m2-done")
				}
				return ModelOutput{}
			}),
		},
		Tools: map[string]ToolFunc{"slow": func(ctx context.Context, _ string) (string, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			select {
			case <-release:
				return "slow-result", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}},
		Skills: map[string]string{"s1": "SKILL-ONE", "s2": "SKILL-TWO"},
	}

	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(v1, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload v1: %v", err)
	}
	owner, _ := state.Agent("main")
	replies := make(chan string, 8)
	s1, err := NewAgent("main", state, reg, deliver(replies))
	if err != nil {
		t.Fatal(err)
	}
	c1, d1 := launch(t, s1)
	if err := s1.Input(bg, "start"); err != nil {
		t.Fatal(err)
	}
	await(t, started)
	halt(t, c1, d1)

	if _, err := ReloadConfig(v2, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload v2: %v", err)
	}
	if owner.Definition().Model != "m2" {
		t.Fatal("热更未生效")
	}
	s2, err := NewAgent("main", state, reg, deliver(replies))
	if err != nil {
		t.Fatal(err)
	}
	if s2.State != s1.State {
		t.Fatal("同 id 热更应复用同一现场")
	}
	c2, d2 := launch(t, s2)
	defer halt(t, c2, d2)
	close(release)
	if got := recv(t, replies); got != "m2-done" {
		t.Fatalf("晚到结果未被新壳消费: %q", got)
	}

	mu.Lock()
	rs := append([]ModelRequest(nil), reqs...)
	mu.Unlock()
	var r2 *ModelRequest
	for i := range rs {
		if rs[i].ModelName == "m2" {
			r2 = &rs[i]
		}
	}
	if r2 == nil {
		t.Fatalf("新配置模型未被调用: %+v", rs)
	}
	if r2.AgentID != "main" || !strings.Contains(r2.SystemPrompt, "new-sys") ||
		!strings.Contains(r2.SystemPrompt, "SKILL-TWO") || !strings.Contains(r2.SystemPrompt, "[tools] slow") ||
		r2.Params["temperature"] != 0.9 {
		t.Fatalf("新 model/params/system 未生效: %+v", r2)
	}
	if !hasStr(r2.Inputs, "start") || !hasStr(r2.Inputs, "slow-result") {
		t.Fatalf("新模型未见旧上下文/晚到结果: %+v", r2.Inputs)
	}
	if !anyTask(owner.Tasks.Snapshot(), func(tk Task) bool { return tk.Call.Name == "slow" && tk.Status == "completed" }) {
		t.Fatalf("在途任务未被复用现场结算: %+v", owner.Tasks.Snapshot())
	}
}

// TestAgentModelProxySuccessAndFailure 验证无状态模型代理：成功与失败都经真实 Run 记录完整请求与错误。
//
// - echo mock 仅在本轮输入为 hello 时产出；递送任务结果再驱动时收敛为空。
func TestAgentModelProxySuccessAndFailure(t *testing.T) {
	cfg := writeCfg(t, t.TempDir(), "p.json", Config{Agents: []AgentConfig{
		{ID: "ok", Model: "echo", SystemPrompt: "SYS", Skills: []string{"sk"}, Tools: []string{"t1"}, Params: map[string]float64{"temperature": 0.7}},
		{ID: "boom", Model: "boom"},
	}})
	var mu sync.Mutex
	var traces []ModelTrace
	reg := Registry{
		Models: map[string]ModelFunc{
			"echo": func(_ context.Context, r ModelRequest) (ModelOutput, error) {
				if lastInput(r) != "hello" {
					return ModelOutput{}, nil
				}
				return out("agent=" + r.AgentID + ";model=" + r.ModelName + ";sys=" + r.SystemPrompt +
					";in=" + strings.Join(r.Inputs, ",")), nil
			},
			"boom": func(context.Context, ModelRequest) (ModelOutput, error) {
				return ModelOutput{}, errors.New("boom-model-error")
			},
		},
		Tools:  map[string]ToolFunc{"t1": constTool("t1-out")},
		Skills: map[string]string{"sk": "SKILL-TEXT"},
		Record: func(tr ModelTrace) { mu.Lock(); traces = append(traces, tr); mu.Unlock() },
	}

	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(cfg, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	replies := make(chan string, 8)
	okA, err := NewAgent("ok", state, reg, deliver(replies))
	if err != nil {
		t.Fatal(err)
	}
	co, do := launch(t, okA)
	if err := okA.Input(bg, "hello"); err != nil {
		t.Fatal(err)
	}
	got := recv(t, replies)
	halt(t, co, do)
	if !strings.Contains(got, "agent=ok") || !strings.Contains(got, "model=echo") || !strings.Contains(got, "in=hello") {
		t.Fatalf("成功模型未收到准备好的请求: %q", got)
	}
	boomA, err := NewAgent("boom", state, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	cb, db := launch(t, boomA)
	defer cb()
	if err := boomA.Input(bg, "fail"); err != nil {
		t.Fatal(err)
	}
	if waitErr(t, db) == nil {
		t.Fatal("失败模型应使 Run 返回错误")
	}

	mu.Lock()
	tr := append([]ModelTrace(nil), traces...)
	mu.Unlock()
	var okT, boomT *ModelTrace
	for i := range tr {
		switch tr[i].Request.AgentID {
		case "ok":
			if tr[i].Response.Output != "" {
				okT = &tr[i]
			}
		case "boom":
			boomT = &tr[i]
		}
	}
	if okT == nil || okT.Error != "" || okT.Response.Output == "" || okT.Request.ModelName != "echo" ||
		okT.Request.Params["temperature"] != 0.7 || !strings.Contains(okT.Request.SystemPrompt, "SYS") ||
		!strings.Contains(okT.Request.SystemPrompt, "SKILL-TEXT") || !strings.Contains(okT.Request.SystemPrompt, "[tools] t1") ||
		!hasStr(okT.Request.Inputs, "hello") {
		t.Fatalf("成功轨迹未记录完整 prepared request: %+v", okT)
	}
	if boomT == nil || boomT.Error == "" || boomT.Request.ModelName != "boom" || !hasStr(boomT.Request.Inputs, "fail") {
		t.Fatalf("失败轨迹未记录错误与请求: %+v", boomT)
	}
}

// TestAgentParentChildDelegation 验证父 agent 经同一管线委派子 agent：父子各自持有现场，结果经任务回流。
func TestAgentParentChildDelegation(t *testing.T) {
	cfg := writeCfg(t, t.TempDir(), "f.json", Config{Agents: []AgentConfig{
		{ID: "P", Model: "pm", SystemPrompt: "parent", Tools: []string{"agent:C"}},
		{ID: "C", ParentID: "P", Model: "cm", Tools: []string{"work"}},
	}})
	reg := Registry{
		Models: map[string]ModelFunc{
			"pm": func(_ context.Context, r ModelRequest) (ModelOutput, error) {
				switch lastInput(r) {
				case "child-done-2":
					return out("parent-done-2"), nil
				case "child-done-1":
					return toolcall("agent:C", "second"), nil
				case "delegate":
					return toolcall("agent:C", "first"), nil
				}
				return ModelOutput{}, nil
			},
			"cm": func(_ context.Context, r ModelRequest) (ModelOutput, error) {
				switch lastInput(r) {
				case "result:second":
					return out("child-done-2"), nil
				case "result:first":
					return out("child-done-1"), nil
				case "second":
					return toolcall("work", "second"), nil
				case "first":
					return toolcall("work", "first"), nil
				}
				return ModelOutput{}, nil
			},
		},
		Tools: map[string]ToolFunc{"work": func(_ context.Context, args string) (string, error) {
			return "result:" + args, nil
		}},
	}

	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(cfg, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	replies := make(chan string, 8)
	parent, err := NewAgent("P", state, reg, deliver(replies))
	if err != nil {
		t.Fatal(err)
	}
	cancel, done := launch(t, parent)
	defer halt(t, cancel, done)
	if err := parent.Input(bg, "delegate"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, replies); got != "parent-done-2" {
		t.Fatalf("父子回流期望 parent-done-2，实得 %q", got)
	}

	pState, _ := state.Agent("P")
	cState, _ := state.Agent("C")
	if pState == cState {
		t.Fatal("父子应各自拥有现场")
	}
	pt, ct := pState.Tasks.Snapshot(), cState.Tasks.Snapshot()
	for _, tk := range pt {
		if tk.OwnerID != "P" {
			t.Fatalf("父任务归属错误: %+v", tk)
		}
	}
	for _, tk := range ct {
		if tk.OwnerID != "C" {
			t.Fatalf("子任务归属错误: %+v", tk)
		}
	}
	if !anyTask(pt, func(tk Task) bool {
		return tk.Call.Name == "agent:C" && tk.Output == "child-done-1" && tk.Status == "completed"
	}) ||
		!anyTask(pt, func(tk Task) bool {
			return tk.Call.Name == "agent:C" && tk.Output == "child-done-2" && tk.Status == "completed"
		}) {
		t.Fatalf("父任务未回流子回复: %+v", pt)
	}
	if !anyTask(ct, func(tk Task) bool {
		return tk.Call.Name == "work" && tk.Output == "result:first" && tk.Status == "completed"
	}) ||
		!anyTask(ct, func(tk Task) bool {
			return tk.Call.Name == "work" && tk.Output == "result:second" && tk.Status == "completed"
		}) {
		t.Fatalf("子任务未执行各自工具: %+v", ct)
	}
	if !anyTask(pt, func(tk Task) bool { return tk.Call.Name == "deliver" }) {
		t.Fatalf("父投递未任务化: %+v", pt)
	}
	if !anyTask(ct, func(tk Task) bool { return tk.Call.Name == "deliver" }) {
		t.Fatalf("子投递未任务化: %+v", ct)
	}
}

// TestAgentDeliverFailureKeepsLoopAliveAndConverges 验证投递失败不终止循环：失败成为 failed 任务通知，模型据以收敛。
//
// - 空 Data 回驱动模型时只计数收敛；再次输入仍能驱动模型并再次投递。
func TestAgentDeliverFailureKeepsLoopAliveAndConverges(t *testing.T) {
	cfg := writeCfg(t, t.TempDir(), "d.json", Config{Agents: []AgentConfig{{ID: "main", Model: "m", Tools: []string{"t"}}}})
	converged := make(chan struct{}, 4)
	reg := Registry{
		Models: map[string]ModelFunc{"m": func(_ context.Context, r ModelRequest) (ModelOutput, error) {
			switch lastInput(r) {
			case "go":
				return out("reply-1"), nil
			case "ping":
				return out("reply-2"), nil
			case "":
				select {
				case converged <- struct{}{}:
				default:
				}
			}
			return ModelOutput{}, nil
		}},
		Tools: map[string]ToolFunc{"t": constTool("t-out")},
	}
	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(cfg, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	attempts := make(chan string, 8)
	deliverFn := func(_ context.Context, data string) (string, error) {
		attempts <- data
		return "", errors.New("deliver boom")
	}
	a, err := NewAgent("main", state, reg, deliverFn)
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := state.Agent("main")
	cancel, done := launch(t, a)
	defer halt(t, cancel, done)

	if err := a.Input(bg, "go"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, attempts); got != "reply-1" {
		t.Fatalf("期望投递 reply-1，实得 %q", got)
	}
	await(t, converged)
	if !anyTask(owner.Tasks.Snapshot(), func(tk Task) bool { return tk.Call.Name == "deliver" && tk.Status == "failed" }) {
		t.Fatalf("投递失败未成为 failed 任务: %+v", owner.Tasks.Snapshot())
	}
	if err := a.Input(bg, "ping"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, attempts); got != "reply-2" {
		t.Fatalf("投递失败后循环未存活：期望 reply-2，实得 %q", got)
	}
	select {
	case <-done:
		t.Fatal("投递失败使 Run 提前终止")
	default:
	}
}

// TestAgentModelDeltaIntermediateNotDelivered 验证中间叙述进上下文、不进投递：带工具调用时 Output/Reasoning 不外投。
func TestAgentModelDeltaIntermediateNotDelivered(t *testing.T) {
	cfg := writeCfg(t, t.TempDir(), "md.json", Config{Agents: []AgentConfig{{ID: "main", Model: "m", Tools: []string{"t"}}}})
	reg := Registry{
		Models: map[string]ModelFunc{"m": func(_ context.Context, r ModelRequest) (ModelOutput, error) {
			switch lastInput(r) {
			case "go":
				return ModelOutput{Output: "thinking-1", Reasoning: "reason-1", ToolCalls: []ToolCall{{Name: "t", Args: ""}}}, nil
			case "t-out":
				return out("final-answer"), nil
			}
			return ModelOutput{}, nil
		}},
		Tools: map[string]ToolFunc{"t": constTool("t-out")},
	}
	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(cfg, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	replies := make(chan string, 8)
	a, err := NewAgent("main", state, reg, deliver(replies))
	if err != nil {
		t.Fatal(err)
	}
	cancel, done := launch(t, a)
	defer halt(t, cancel, done)
	if err := a.Input(bg, "go"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, replies); got != "final-answer" {
		t.Fatalf("仅 final 应外投，实得 %q", got)
	}
	owner, _ := state.Agent("main")
	var sawOut, sawReason bool
	for _, e := range owner.Context() {
		if e.Type != EventModelDelta || e.Source != "model" {
			continue
		}
		switch e.Data {
		case "thinking-1":
			sawOut = true
		case "reason-1":
			sawReason = true
		}
	}
	if !sawOut || !sawReason {
		t.Fatalf("中间叙述未进上下文投影: output=%v reasoning=%v ctx=%+v", sawOut, sawReason, owner.Context())
	}
	select {
	case extra := <-replies:
		t.Fatalf("投递通道出现额外事件（中间叙述泄漏）: %q", extra)
	default:
	}
}

// TestAgentChildIntermediateNotCollected 验证子 agent 的中间叙述不进父任务结果：父只收子 Reply。
func TestAgentChildIntermediateNotCollected(t *testing.T) {
	cfg := writeCfg(t, t.TempDir(), "ci.json", Config{Agents: []AgentConfig{
		{ID: "P", Model: "pm", Tools: []string{"agent:C"}},
		{ID: "C", ParentID: "P", Model: "cm", Tools: []string{"work"}},
	}})
	reg := Registry{
		Models: map[string]ModelFunc{
			"pm": func(_ context.Context, r ModelRequest) (ModelOutput, error) {
				switch lastInput(r) {
				case "child-final":
					return out("parent-got"), nil
				case "delegate":
					return toolcall("agent:C", "task"), nil
				}
				return ModelOutput{}, nil
			},
			"cm": func(_ context.Context, r ModelRequest) (ModelOutput, error) {
				switch lastInput(r) {
				case "work-out":
					return out("child-final"), nil
				case "task":
					return ModelOutput{Output: "child-thinking", ToolCalls: []ToolCall{{Name: "work", Args: ""}}}, nil
				}
				return ModelOutput{}, nil
			},
		},
		Tools: map[string]ToolFunc{"work": constTool("work-out")},
	}
	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(cfg, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	replies := make(chan string, 8)
	parent, err := NewAgent("P", state, reg, deliver(replies))
	if err != nil {
		t.Fatal(err)
	}
	cancel, done := launch(t, parent)
	defer halt(t, cancel, done)
	if err := parent.Input(bg, "delegate"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, replies); got != "parent-got" {
		t.Fatalf("子中间叙述被误当收口结果（父应得 parent-got）: %q", got)
	}
	cState, _ := state.Agent("C")
	if !hasData(cState.Context(), "child-thinking") {
		t.Fatalf("子中间叙述未进子上下文: %+v", cState.Context())
	}
}

// TestAgentDenylistPluginZeroExecution 验证治理插件零执行：拒绝事实经呈现闭包进 Data，工具从未被调用。
//
// - 核心不统一拼接事件属性，由插件闭包把 Status/Error 投影给模型。
func TestAgentDenylistPluginZeroExecution(t *testing.T) {
	cfg := writeCfg(t, t.TempDir(), "dn.json", Config{Agents: []AgentConfig{{
		ID: "main", Model: "m", Tools: []string{"danger"}, Plugins: []string{"denylist", "present"},
	}}})
	dangerHit := make(chan struct{}, 1)
	var mu sync.Mutex
	var reqs []string
	reg := Registry{
		Models: map[string]ModelFunc{"m": func(_ context.Context, r ModelRequest) (ModelOutput, error) {
			j := joined(r)
			mu.Lock()
			reqs = append(reqs, j)
			mu.Unlock()
			switch {
			case strings.Contains(lastInput(r), "denied"):
				return out("aborted"), nil
			case lastInput(r) == "go":
				return toolcall("danger", ""), nil
			}
			return ModelOutput{}, nil
		}},
		Tools: map[string]ToolFunc{"danger": func(context.Context, string) (string, error) {
			select {
			case dangerHit <- struct{}{}:
			default:
			}
			return "danger-out", nil
		}},
		Plugins: map[string]func(*AgentState) Plugin{
			"denylist": func(o *AgentState) Plugin { return DenylistPlugin(o, "danger") },
			"present": func(*AgentState) Plugin {
				return Plugin{Kinds: []EventType{EventToolResult},
					Before: func(_ context.Context, e Event) (Event, error) {
						if e.Status == "denied" {
							e.Data = e.Call.Name + " " + e.Status + ": " + e.Error
						}
						return e, nil
					}}
			},
		},
	}
	state := NewState()
	defer state.Close()
	if _, err := ReloadConfig(cfg, state, reg, Evolution{}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	replies := make(chan string, 8)
	a, err := NewAgent("main", state, reg, deliver(replies))
	if err != nil {
		t.Fatal(err)
	}
	cancel, done := launch(t, a)
	defer halt(t, cancel, done)
	if err := a.Input(bg, "go"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, replies); got != "aborted" {
		t.Fatalf("模型应据 denied 事实放弃，实得 %q", got)
	}
	select {
	case <-dangerHit:
		t.Fatal("被拒绝的工具仍被执行")
	default:
	}
	owner, _ := state.Agent("main")
	if anyTask(owner.Tasks.Snapshot(), func(tk Task) bool { return tk.Call.Name == "danger" }) {
		t.Fatalf("被拒绝的工具仍被派发: %+v", owner.Tasks.Snapshot())
	}
	mu.Lock()
	joinedReq := strings.Join(reqs, "\n---\n")
	mu.Unlock()
	if !strings.Contains(joinedReq, "denied by governance plugin") {
		t.Fatalf("模型未见拒绝事实: %s", joinedReq)
	}
}

// TestAgentTaskWindowBatching 验证条数聚合窗：0=即发，满窗一次可见，Close 前 flush 残余。
//
// - Restore 产生的 unknown 通知同样入攒批。
func TestAgentTaskWindowBatching(t *testing.T) {
	nextBatch := func(t *testing.T, a *AgentState) []Event {
		t.Helper()
		b, err := a.Next(bg)
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		return b
	}

	t.Run("window0 即发", func(t *testing.T) {
		release := make(chan struct{})
		tool := func(ctx context.Context, _ string) (string, error) {
			select {
			case <-release:
				return "done", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		a := newAgentState(AgentConfig{ID: "a", TaskWindow: 0})
		defer a.Tasks.Close()
		a.Tasks.Spawn(ToolCall{Name: "t"}, tool, "user")
		b1 := nextBatch(t, a)
		if len(b1) != 1 || b1[0].Type != EventTaskNotice || b1[0].Status != "running" {
			t.Fatalf("window0 running 应即发单条: %+v", b1)
		}
		close(release)
		b2 := nextBatch(t, a)
		if len(b2) != 1 || b2[0].Type != EventToolResult || b2[0].Status != "completed" {
			t.Fatalf("window0 result 应即发单条: %+v", b2)
		}
	})

	t.Run("window2 攒批", func(t *testing.T) {
		release := make(chan struct{})
		tool := func(ctx context.Context, _ string) (string, error) {
			select {
			case <-release:
				return "done", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		a := newAgentState(AgentConfig{ID: "a", TaskWindow: 2})
		defer a.Tasks.Close()
		a.Tasks.Spawn(ToolCall{Name: "t1"}, tool, "user")
		select {
		case <-a.wake:
			t.Fatal("未满窗却已投递")
		default:
		}
		a.Tasks.Spawn(ToolCall{Name: "t2"}, tool, "user")
		b1 := nextBatch(t, a)
		if len(b1) != 2 || b1[0].Status != "running" || b1[1].Status != "running" {
			t.Fatalf("window2 应攒满两条一批投递: %+v", b1)
		}
		close(release)
		b2 := nextBatch(t, a)
		if len(b2) != 2 || b2[0].Type != EventToolResult || b2[1].Type != EventToolResult {
			t.Fatalf("window2 结果应攒满一批: %+v", b2)
		}
	})

	t.Run("Close 前 flush 残余", func(t *testing.T) {
		a := newAgentState(AgentConfig{ID: "a", TaskWindow: 3})
		a.Tasks.Restore([]Task{{ID: 1, OwnerID: "a", Source: "x", Call: ToolCall{Name: "t"}, Status: "running"}})
		a.Tasks.Close()
		b := nextBatch(t, a)
		if len(b) != 1 || b[0].Type != EventTaskNotice || b[0].Status != "unknown" {
			t.Fatalf("Close 未 flush 残余 unknown 通知: %+v", b)
		}
	})
}
