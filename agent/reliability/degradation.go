// Package reliability 承载 tagent 的常驻可靠性子系统（T-G）：依赖失效的优雅退化、
// 可靠投递、冥想锚点持久化。核心理念（报告 D3）：at-least-once 而非 exactly-once；
// 每个外部依赖失效有明确定义的「检测→降级→恢复」三段式路径，无静默丢失、无 panic、
// 无死循环；失败是一等资产（退化状态可查询、可观测、入 governance 事件）。
package reliability

import (
	"sync"
	"time"
)

// Dependency 是受监控的外部依赖。
type Dependency string

const (
	DepMemory     Dependency = "memory"     // MemoryStore 写失败
	DepRustViking Dependency = "rustviking" // rustviking CLI fork 失败（file 型 store）
	DepMCP        Dependency = "mcp"        // MCP server 连续失败
	DepModel      Dependency = "model"      // RunFlow 连续失败
	DepDisk       Dependency = "disk"       // ENOSPC 磁盘满
)

// DepState 是依赖健康状态（normal → degraded → recovering → normal）。
type DepState string

const (
	StateNormal     DepState = "normal"
	StateDegraded   DepState = "degraded"
	StateRecovering DepState = "recovering"
)

// DepConfig 是单依赖的退化参数。
type DepConfig struct {
	FailThreshold    int           // 连续失败 N 次 → degraded（默认 3）
	RecoverSuccesses int           // recovering 中连续成功 M 次 → normal（默认 2）
	ProbeBackoff     time.Duration // 探测退避基准（默认 30s，指数退避封顶 5m）
	BackoffMax       time.Duration // 退避封顶（默认 5m）
}

func (c DepConfig) withDefaults() DepConfig {
	if c.FailThreshold <= 0 {
		c.FailThreshold = 3
	}
	if c.RecoverSuccesses <= 0 {
		c.RecoverSuccesses = 2
	}
	if c.ProbeBackoff <= 0 {
		c.ProbeBackoff = 30 * time.Second
	}
	if c.BackoffMax <= 0 {
		c.BackoffMax = 5 * time.Minute
	}
	return c
}

type depEntry struct {
	state        DepState
	failCount    int
	successCount int
	cfg          DepConfig
	since        time.Time // 进入当前状态的时刻
	backoff      time.Duration
	lastProbe    time.Time
}

// DegradationManager 管理五依赖的退化状态机。并发安全（mu 保护）。
// onChange 在每次状态迁移时回调（默认实现由调用方注入：写 governance 事件 + 日志）。
type DegradationManager struct {
	mu       sync.Mutex
	states   map[Dependency]*depEntry
	onChange func(dep Dependency, from, to DepState)
	now      func() time.Time // 可注入时钟（测试）
}

// NewDegradationManager 构建退化管理器。onChange 可为 nil（仅内部状态）。
func NewDegradationManager(onChange func(dep Dependency, from, to DepState)) *DegradationManager {
	return &DegradationManager{
		states:   make(map[Dependency]*depEntry),
		onChange: onChange,
		now:      time.Now,
	}
}

// Configure 为某依赖设置退化参数（未配置的用默认）。
func (d *DegradationManager) Configure(dep Dependency, cfg DepConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e := d.entryLocked(dep)
	e.cfg = cfg.withDefaults()
}

// ReportFailure 上报一次依赖失败。达阈值 → 进入 degraded；recovering 中失败 → 退回 degraded。
func (d *DegradationManager) ReportFailure(dep Dependency, _ error) {
	d.mu.Lock()
	e := d.entryLocked(dep)
	e.cfg = e.cfg.withDefaults()
	from := e.state
	switch e.state {
	case StateNormal:
		e.failCount++
		if e.failCount >= e.cfg.FailThreshold {
			e.state = StateDegraded
			e.since = d.now()
			e.backoff = e.cfg.ProbeBackoff
			e.failCount = 0
		}
	case StateRecovering:
		// 恢复中任一失败 → 退回 degraded，退避翻倍（指数）。
		e.state = StateDegraded
		e.since = d.now()
		e.successCount = 0
		e.backoff = d.minDuration(e.backoff*2, e.cfg.BackoffMax)
	case StateDegraded:
		// 已降级，仅刷新退避。
		e.backoff = d.minDuration(e.backoff*2, e.cfg.BackoffMax)
	}
	to := e.state
	d.mu.Unlock()
	if from != to && d.onChange != nil {
		d.onChange(dep, from, to)
	}
}

// ReportSuccess 上报一次依赖成功。normal 重置失败计数；degraded（探测窗口到）→ recovering；
// recovering 连续成功达阈值 → normal。
func (d *DegradationManager) ReportSuccess(dep Dependency) {
	d.mu.Lock()
	e := d.entryLocked(dep)
	e.cfg = e.cfg.withDefaults()
	from := e.state
	switch e.state {
	case StateNormal:
		e.failCount = 0
	case StateDegraded:
		e.state = StateRecovering
		e.since = d.now()
		e.successCount = 1
		if e.successCount >= e.cfg.RecoverSuccesses {
			e.state = StateNormal
			e.backoff = 0
			e.successCount = 0
		}
	case StateRecovering:
		e.successCount++
		if e.successCount >= e.cfg.RecoverSuccesses {
			e.state = StateNormal
			e.backoff = 0
			e.successCount = 0
		}
	}
	to := e.state
	d.mu.Unlock()
	if from != to && d.onChange != nil {
		d.onChange(dep, from, to)
	}
}

// State 返回依赖当前状态（未监控的依赖视为 normal）。
func (d *DegradationManager) State(dep Dependency) DepState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if e, ok := d.states[dep]; ok {
		return e.state
	}
	return StateNormal
}

// IsDegraded 报告依赖是否处于非正常态（业务侧据此选择降级行为）。
func (d *DegradationManager) IsDegraded(dep Dependency) bool {
	return d.State(dep) != StateNormal
}

// ShouldProbe 报告 degraded 依赖是否到了探测时机（退避窗口已过）——供主动探测型
// 依赖（如 mcp 半开）判断；上报型依赖（memory/disk）不需调用（真实操作即探针）。
func (d *DegradationManager) ShouldProbe(dep Dependency) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.states[dep]
	if !ok || e.state != StateDegraded {
		return false
	}
	if d.now().Sub(e.lastProbe) >= e.backoff {
		e.lastProbe = d.now()
		return true
	}
	return false
}

// Snapshot 返回全部依赖状态快照（诊断/可观测）。
func (d *DegradationManager) Snapshot() map[Dependency]DepState {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[Dependency]DepState, len(d.states))
	for dep, e := range d.states {
		out[dep] = e.state
	}
	return out
}

func (d *DegradationManager) entryLocked(dep Dependency) *depEntry {
	if e, ok := d.states[dep]; ok {
		return e
	}
	e := &depEntry{state: StateNormal, cfg: DepConfig{}.withDefaults(), since: d.now()}
	d.states[dep] = e
	return e
}

func (d *DegradationManager) minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
