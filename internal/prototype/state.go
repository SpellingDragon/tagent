// 职责：state.go 维护每个 agent id 的消费现场——AgentState 持定义、生效 system prompt、待处理
// 队列、唤醒信号、消费者占用、上下文、Memory 与 TaskManager；State 以 id 注册现场，按整份新配置
// 做架构级 Apply（校验 id 唯一与父关系、由 currentConfig 与候选构建 StatePlan、依序经
// Eval→Benchmark→ApplyGate 裁决，同 id 更新定义+prompt、缺 id 回收），并在演示检查点把可序列化
// 现场存成 JSON、跨进程 Load 重建（在途任务恢复 unknown，生效 prompt 一并回填）。
// 未实现：生产 WAL/事务/多代租约、任意时刻崩溃一致性、按年龄判僵尸、函数序列化、队列背压；
// 队列与上下文是内存态，只有显式 Save 才落盘，也不自动重执行任何副作用。
// 接线：BaseTAgent 经 Begin/Next/End 消费并 Publish 回流；TaskManager 完成通知进入所属队列，
// 插件经 Context/ReplaceContext 改写现场，config.go 装配时读取定义。
// 契约: docs/wiki/agent/prototype-skeleton.md#six-pieces
package prototype

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
)

// ErrOwnerRemoved 等现场级可见错误：拒绝而非静默丢弃。
var (
	ErrOwnerRemoved    = errors.New("prototype: owner 已移除，拒绝路由")
	ErrAlreadyConsumed = errors.New("prototype: 现场已被消费壳占用")
	ErrUnknownAgent    = errors.New("prototype: 未知 agent id")
	ErrForeignOwner    = errors.New("prototype: 事件归属与现场不符")
)

// AgentState 是一个 agent id 的完整消费现场。Tasks/Memory 只在创建时绑定，热加载同 id
// 不换指针、只更新定义与生效 prompt。mu 只保护队列/上下文/标志，绝不包住用户回调或 Tasks.Close。
type AgentState struct {
	ID     string
	Tasks  *TaskManager
	Memory *Memory

	mu      sync.Mutex
	def     AgentConfig
	prompt  string
	queue   []Event
	context []Event
	wake    chan struct{}
	busy    bool
	removed bool
}

// newAgentState 建立现场：Memory 与 TaskManager 只在此绑定，完成通知经 Publish 回流本队列；
// 条数聚合窗由定义 TaskWindow 编译进 TaskManager（0=即发）。
func newAgentState(def AgentConfig) *AgentState {
	a := &AgentState{ID: def.ID, Memory: NewMemory(), wake: make(chan struct{}, 1), def: def}
	a.Tasks = NewTaskManager(a, def.TaskWindow)
	return a
}

// Definition 返回当前生效定义（浅拷贝，调用方只读）。
func (a *AgentState) Definition() AgentConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.def
}

// SystemPrompt 返回当前生效的 system prompt（Apply 时经 resolve 写入；nil resolve 取声明原文）。
func (a *AgentState) SystemPrompt() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.prompt
}

// Publish 补全/校验归属后追加事件并唤醒消费者；owner 被回收或归属不符时拒绝。
// 唤醒是容量 1 的脉冲：已有未消费脉冲时跳过唤醒，事件已入队，不丢。
func (a *AgentState) Publish(e Event) error {
	if e.OwnerID == "" {
		e.OwnerID = a.ID
	} else if e.OwnerID != a.ID {
		return ErrForeignOwner
	}
	a.mu.Lock()
	if a.removed {
		a.mu.Unlock()
		return ErrOwnerRemoved
	}
	a.queue = append(a.queue, e)
	a.mu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return nil
}

// PublishBatch 批量原子发布：整批在单次锁内入队、一次唤醒——消费端 pull 无法切入批中间，
// 攒批通知因此"一次消费批次同时可见"。归属校验与 Publish 同规则（通知方已带 OwnerID，不代补）。
func (a *AgentState) PublishBatch(events []Event) error {
	a.mu.Lock()
	if a.removed {
		a.mu.Unlock()
		return ErrOwnerRemoved
	}
	for _, e := range events {
		if e.OwnerID != "" && e.OwnerID != a.ID {
			a.mu.Unlock()
			return ErrForeignOwner
		}
		if e.OwnerID == "" {
			e.OwnerID = a.ID
		}
		a.queue = append(a.queue, e)
	}
	a.mu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return nil
}

// Next 取走当前整批事件；ctx 取消或现场被回收时返回错误，空队列则等唤醒。
func (a *AgentState) Next(ctx context.Context) ([]Event, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a.mu.Lock()
		if a.removed {
			a.mu.Unlock()
			return nil, ErrOwnerRemoved
		}
		if len(a.queue) > 0 {
			batch := a.queue
			a.queue = nil
			a.mu.Unlock()
			return batch, nil
		}
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-a.wake:
		}
	}
}

// Append 把一条事件追加到上下文尾部（核心消费后记录），隔离 Meta 以免插件改动持久原文。
func (a *AgentState) Append(e Event) {
	a.mu.Lock()
	a.context = append(a.context, cloneEvent(e))
	a.mu.Unlock()
}

// Context 返回上下文快照（隔离 Meta）。
func (a *AgentState) Context() []Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneEvents(a.context)
}

// ReplaceContext 整份替换上下文（折叠刚用过的结果后回写），隔离 Meta。
func (a *AgentState) ReplaceContext(events []Event) {
	cloned := cloneEvents(events)
	a.mu.Lock()
	a.context = cloned
	a.mu.Unlock()
}

// cloneEvents 复制事件切片并隔离每条 Meta。
func cloneEvents(events []Event) []Event {
	out := make([]Event, len(events))
	for i, e := range events {
		out[i] = cloneEvent(e)
	}
	return out
}

// Begin 独占消费：现场已有消费壳或已被移除时拒绝，保证同一现场只有一个壳在消费。
func (a *AgentState) Begin() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.removed {
		return ErrOwnerRemoved
	}
	if a.busy {
		return ErrAlreadyConsumed
	}
	a.busy = true
	return nil
}

// End 释放消费占用，与 Begin 配对（重复调用幂等）。
func (a *AgentState) End() {
	a.mu.Lock()
	a.busy = false
	a.mu.Unlock()
}

// detach 标记现场被回收并唤醒可能在 Next 等待的消费壳；返回其 TaskManager 供锁外关闭。
func (a *AgentState) detach() *TaskManager {
	a.mu.Lock()
	a.removed = true
	tasks := a.Tasks
	a.mu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return tasks
}

// snapshotOf 采集现场可序列化字段（不含函数/nil 指针），锁外读 Tasks/Memory、锁内读队列与上下文，
// 避免与任务完成上行 Publish 形成锁序反转。
func (a *AgentState) snapshotOf() agentSnapshot {
	tasks := a.Tasks.Snapshot()
	memory := a.Memory.Snapshot()
	a.mu.Lock()
	defer a.mu.Unlock()
	return agentSnapshot{
		Definition: a.def,
		Prompt:     a.prompt,
		Queue:      cloneEvents(a.queue),
		Context:    cloneEvents(a.context),
		Tasks:      tasks,
		Memory:     memory,
	}
}

// restoreFrom 用快照字段填充现场的生效 prompt/队列/上下文/Memory（定义已在创建时给定）。
func (a *AgentState) restoreFrom(s agentSnapshot) {
	a.mu.Lock()
	a.prompt = s.Prompt
	a.queue = append([]Event(nil), s.Queue...)
	a.context = append([]Event(nil), s.Context...)
	a.mu.Unlock()
	a.Memory.Restore(s.Memory)
}

// State 以 agent id 注册消费现场；生效定义即各 AgentState.Definition，不另存镜像。
type State struct {
	mu     sync.Mutex
	agents map[string]*AgentState
}

// NewState 建立空的现场注册表。
func NewState() *State { return &State{agents: map[string]*AgentState{}} }

// Agent 按 id 取出消费现场。
func (s *State) Agent(id string) (*AgentState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownAgent, id)
	}
	return a, nil
}

// currentConfig 由现存现场的定义重建整份配置（按 id 升序），作为 Change.Before 的唯一来源。
func (s *State) currentConfig() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := Config{}
	for _, a := range s.agents {
		cfg.Agents = append(cfg.Agents, a.Definition())
	}
	sort.Slice(cfg.Agents, func(i, j int) bool { return cfg.Agents[i].ID < cfg.Agents[j].ID })
	return cfg
}

// Apply 以整份新配置重建架构：结构校验后，由当前生效定义（Before）与候选（After）构建
// StatePlan，依序交 Eval→Benchmark→ApplyGate 裁决（nil 段恒真）；通过后同 id 更新定义与生效
// prompt、新 id 建现场、旧 id 标记移除并回收。任一段拒绝即返回，定义、生效 prompt 与现场保持旧值。
// TaskManager.Close 会等在途任务，一律在 State 锁外执行。返回本次被回收的 owner id（升序）。
//
// - 同 id 换装只换定义与生效 prompt，现场不动。
// - 新建现场尚未发布，写入生效 prompt 不需要加锁。
// - 孤儿回收在锁外取消任务上下文并等待在途任务收尾。
func (s *State) Apply(cfg Config, resolve func(AgentConfig) string, evo Evolution) ([]string, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	plan := buildStatePlan(s.currentConfig(), cfg, resolve)
	if err := runEvolution(plan, evo); err != nil {
		return nil, err
	}

	s.mu.Lock()
	keep := make(map[string]bool, len(cfg.Agents))
	for _, def := range cfg.Agents {
		keep[def.ID] = true
		if a, ok := s.agents[def.ID]; ok {
			a.mu.Lock()
			a.def, a.prompt = def, plan.Prompts[def.ID]
			a.mu.Unlock()
			continue
		}
		a := newAgentState(def)
		a.prompt = plan.Prompts[def.ID]
		s.agents[def.ID] = a
	}
	var removed []*TaskManager
	var removedIDs []string
	for id, a := range s.agents {
		if keep[id] {
			continue
		}
		removed = append(removed, a.detach())
		delete(s.agents, id)
		removedIDs = append(removedIDs, id)
	}
	s.mu.Unlock()

	for _, tasks := range removed {
		tasks.Close()
	}
	sort.Strings(removedIDs)
	return removedIDs, nil
}

// buildStatePlan 由当前生效定义（Before）与候选（After）构造变更计划：Adds/Updates/Removes 按 id
// 升序，Prompts 对 Adds+Updates 经 resolve 计算下一版生效 prompt（resolve 为 nil 取声明原文）。
func buildStatePlan(before, after Config, resolve func(AgentConfig) string) StatePlan {
	plan := StatePlan{Change: Change{Before: before, After: after}, Prompts: map[string]string{}}
	existing := make(map[string]bool, len(before.Agents))
	for _, def := range before.Agents {
		existing[def.ID] = true
	}
	kept := make(map[string]bool, len(after.Agents))
	for _, def := range after.Agents {
		kept[def.ID] = true
		if existing[def.ID] {
			plan.Updates = append(plan.Updates, def)
		} else {
			plan.Adds = append(plan.Adds, def)
		}
		plan.Prompts[def.ID] = resolvePrompt(def, resolve)
	}
	for _, def := range before.Agents {
		if !kept[def.ID] {
			plan.Removes = append(plan.Removes, def.ID)
		}
	}
	sort.Slice(plan.Adds, func(i, j int) bool { return plan.Adds[i].ID < plan.Adds[j].ID })
	sort.Slice(plan.Updates, func(i, j int) bool { return plan.Updates[i].ID < plan.Updates[j].ID })
	sort.Strings(plan.Removes)
	return plan
}

// resolvePrompt 计算定义下一版生效 prompt：resolve 为 nil 时取声明原文。
func resolvePrompt(def AgentConfig, resolve func(AgentConfig) string) string {
	if resolve == nil {
		return def.SystemPrompt
	}
	return resolve(def)
}

// runEvolution 依序执行三段演进（Eval→Benchmark→ApplyGate）；nil 段视作恒真跳过，任一段返回
// error 即中止并上抛。
func runEvolution(plan StatePlan, evo Evolution) error {
	segments := []func(StatePlan) error{evo.Eval, evo.Benchmark, evo.ApplyGate}
	for _, seg := range segments {
		if seg == nil {
			continue
		}
		if err := seg(plan); err != nil {
			return err
		}
	}
	return nil
}

// Close 释放整个注册表：标记所有现场移除并在锁外关闭各 TaskManager（宿主关闭）。
func (s *State) Close() {
	s.mu.Lock()
	var tasks []*TaskManager
	for id, a := range s.agents {
		tasks = append(tasks, a.detach())
		delete(s.agents, id)
	}
	s.mu.Unlock()
	for _, t := range tasks {
		t.Close()
	}
}

// validateConfig 只做结构校验：id 非空且唯一、父引用存在、父链不成环；不引入通用图框架。
func validateConfig(cfg Config) error {
	parent := make(map[string]string, len(cfg.Agents))
	for _, def := range cfg.Agents {
		if def.ID == "" {
			return errors.New("prototype: agent 定义缺少 id")
		}
		if _, dup := parent[def.ID]; dup {
			return fmt.Errorf("prototype: agent id 重复：%q", def.ID)
		}
		parent[def.ID] = def.ParentID
	}
	for id, p := range parent {
		if p == "" {
			continue
		}
		if _, ok := parent[p]; !ok {
			return fmt.Errorf("prototype: agent %q 的父 %q 不存在", id, p)
		}
		seen := map[string]bool{}
		for cur := p; cur != "" && !seen[cur]; cur = parent[cur] {
			if cur == id {
				return fmt.Errorf("prototype: agent %q 的父关系成环", id)
			}
			seen[cur] = true
		}
	}
	return nil
}

// stateSnapshot / agentSnapshot 是快照的可序列化形态：定义、生效 prompt、待处理队列、上下文、
// 任务记录与 Memory；字段用 Go 默认名，不含任何函数。
type stateSnapshot struct {
	Agents []agentSnapshot
}

type agentSnapshot struct {
	Definition AgentConfig
	Prompt     string
	Queue      []Event
	Context    []Event
	Tasks      []Task
	Memory     map[string]Event
}

// Save 在演示检查点采集每个现场并写成 JSON 快照（按 id 排序便于比对）。
func (s *State) Save(path string) error {
	s.mu.Lock()
	dto := stateSnapshot{}
	for _, a := range s.agents {
		dto.Agents = append(dto.Agents, a.snapshotOf())
	}
	s.mu.Unlock()
	sort.Slice(dto.Agents, func(i, j int) bool {
		return dto.Agents[i].Definition.ID < dto.Agents[j].Definition.ID
	})
	data, err := json.MarshalIndent(dto, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadState 从 JSON 快照重建 State：建现场与 owner 映射、恢复生效 prompt/队列/上下文/Memory，再经
// TaskManager.Restore(01) 把在途任务恢复 unknown（可能发 notice）。无 owner 的回收留给随后
// ReloadConfig 的 Apply；不自动重执行副作用。
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var dto stateSnapshot
	if err := json.Unmarshal(data, &dto); err != nil {
		return nil, err
	}
	s := NewState()
	for _, snap := range dto.Agents {
		a := newAgentState(snap.Definition)
		a.restoreFrom(snap)
		s.agents[a.ID] = a
		a.Tasks.Restore(snap.Tasks)
	}
	return s, nil
}
