// 职责：owner 任务表——登记工具/子 agent 调用，自持可取消 context，启动和完成时分别发送状态事件。
// 未实现：重试、TTL、强杀、跨进程重挂；不复制任务 ID 序列。
// 接线：AgentState 构造 TaskManager，通知一律经 owner.Publish 投回所属管线（单点关系，不另持映射）；
// Close 取消并等待在途执行，再 flush 条数窗残余。
package prototype

import (
	"context"
	"errors"
	"strconv"
	"sync"
)

// TaskManager 自持 owner 的任务与可取消 context；普通停止壳不关任务，只有 Close 取消并等待。
// 任务与管线的映射只有 owner 一份真值；window>0 时完成通知按条数攒批投递。
type TaskManager struct {
	owner   *AgentState
	window  int
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	tasks   []Task
	pending []Event
	wg      sync.WaitGroup
}

// NewTaskManager 建立 owner 任务表并自持可取消 context；window 为完成通知条数聚合窗（0=即发）。
func NewTaskManager(owner *AgentState, window int) *TaskManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &TaskManager{owner: owner, window: window, ctx: ctx, cancel: cancel}
}

// Spawn 登记任务并发 running 通知，返回新任务 ID；run 为 nil 视为未知工具失败，
// owner 已关闭（ctx 取消）时登记为取消而不开工。
func (m *TaskManager) Spawn(call ToolCall, run ToolFunc, source string) int {
	m.mu.Lock()
	id := m.nextID()
	m.tasks = append(m.tasks, Task{ID: id, OwnerID: m.owner.ID, Source: source, Call: call, Status: "running"})
	closed := m.ctx.Err() != nil
	m.mu.Unlock()
	m.notice(id, source, call, "running")
	if run == nil {
		m.Complete(id, "", errors.New("prototype: unknown tool"))
		return id
	}
	if closed {
		m.Complete(id, "", context.Canceled)
		return id
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		out, err := run(m.ctx, call.Args)
		m.Complete(id, out, err)
	}()
	return id
}

// nextID 从现存最大任务 ID 递增（Restore 过滤后编号可能稀疏）。调用方须持锁。
func (m *TaskManager) nextID() int {
	max := 0
	for i := range m.tasks {
		if m.tasks[i].ID > max {
			max = m.tasks[i].ID
		}
	}
	return max + 1
}

// Complete 结算 running/unknown 任务再发结果事件；terminal 记录不重复改写旧事实。
func (m *TaskManager) Complete(id int, output string, err error) {
	m.mu.Lock()
	var result Event
	ok := false
	for i := range m.tasks {
		t := &m.tasks[i]
		if t.ID != id {
			continue
		}
		if t.Status != "running" && t.Status != "unknown" {
			break // terminal：保留旧事实。
		}
		t.Output = output
		if err != nil {
			t.Status, t.Error = "failed", err.Error()
		} else {
			t.Status = "completed"
		}
		result = Event{Type: EventToolResult, OwnerID: m.owner.ID, Source: t.Source, Call: t.Call, TaskID: t.ID, Status: t.Status, Data: output, Error: t.Error}
		ok = true
		break
	}
	m.mu.Unlock()
	if ok {
		m.notify(result)
	}
}

// Snapshot 返回任务记录拷贝，供 JSON 快照。
func (m *TaskManager) Snapshot() []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Task(nil), m.tasks...)
}

// Restore 载入任务记录：保留归属，running 转 unknown 并发通知，丢弃 owner 不匹配的任务。
func (m *TaskManager) Restore(tasks []Task) {
	m.mu.Lock()
	restored := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if t.OwnerID != m.owner.ID {
			continue
		}
		if t.Status == "running" {
			t.Status = "unknown"
		}
		restored = append(restored, t)
	}
	m.tasks = restored
	m.mu.Unlock()
	for _, t := range restored {
		if t.Status == "unknown" {
			m.notice(t.ID, t.Source, t.Call, "unknown")
		}
	}
}

// Close 取消本任务表所有任务、等待在途执行结束，再 flush 条数窗残余通知（孤儿回收/宿主关闭）。
func (m *TaskManager) Close() {
	m.cancel()
	m.wg.Wait()
	m.mu.Lock()
	rest := m.pending
	m.pending = nil
	m.mu.Unlock()
	m.flush(rest)
}

// notify 把一条任务通知交给 owner：window<=0 即发；否则攒入待发缓冲，满窗批量 flush。
func (m *TaskManager) notify(e Event) {
	m.mu.Lock()
	if m.window <= 0 {
		m.mu.Unlock()
		_ = m.owner.Publish(e)
		return
	}
	m.pending = append(m.pending, e)
	if len(m.pending) < m.window {
		m.mu.Unlock()
		return
	}
	batch := m.pending
	m.pending = nil
	m.mu.Unlock()
	m.flush(batch)
}

// flush 把攒满的一批通知经 PublishBatch 原子投回所属管线——逐条 Publish 会让消费端
// 在批中间被唤醒 pull，破坏"一次消费批次同时可见"（全量 race 下实测切入缝隙）。
func (m *TaskManager) flush(batch []Event) {
	_ = m.owner.PublishBatch(batch)
}

// notice 发一条任务状态通知（不驱动模型）。
func (m *TaskManager) notice(id int, source string, call ToolCall, status string) {
	m.notify(Event{Type: EventTaskNotice, OwnerID: m.owner.ID, Source: source, Call: call, TaskID: id, Status: status, Data: "task " + strconv.Itoa(id) + " " + status})
}

// AgentTask 把 `agent:<id>` 委派适配成父可用工具：子用同一 NewAgent/Run 循环执行，
// 首个 Reply 作为父任务结果；取到后停子消费壳，但子 owner 资源（TaskManager）由 State 保有。
func AgentTask(state *State, id string, registry Registry) ToolFunc {
	return func(ctx context.Context, args string) (string, error) {
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()
		replies := make(chan string, 1)
		child, err := NewAgent(id, state, registry, func(_ context.Context, data string) (string, error) {
			select {
			case replies <- data:
			default:
			}
			return "", nil
		})
		if err != nil {
			return "", err
		}
		done := make(chan error, 1)
		go func() { done <- child.Run(cctx) }()
		if err := child.Input(cctx, args); err != nil {
			<-done
			return "", err
		}
		select {
		case out := <-replies:
			cancel() // 首个 Reply 即结果，停子消费壳（不关子 owner 资源）。
			<-done
			return out, nil
		case err := <-done:
			return "", err
		case <-cctx.Done():
			<-done
			return "", cctx.Err()
		}
	}
}
