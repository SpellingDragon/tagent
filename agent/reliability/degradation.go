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
	// DepMemory 是存储栈这一受监控依赖：写路径失败由最外层的错误追踪存储上报。
	DepMemory Dependency = "memory"
	// DepRustViking 是外部向量/存储后端依赖，与 DepMemory 分开计状态，
	// 以便后端抖动只降级到它实际影响的能力，而不连带拖垮整条存储栈。
	DepRustViking Dependency = "rustviking"
	// DepMCP 是 MCP 工具来源依赖：处于 degraded 时对 mcp_call 熔断，按探测窗口放行。
	DepMCP Dependency = "mcp"
	// DepModel 是模型调用依赖：degraded 时回合之间按退避暂停，而不是把失败当作正常答复往下走。
	DepModel Dependency = "model"
	// DepDisk 是磁盘写入依赖：写入失败会影响需要落盘的委派路径，因此单独计状态。
	DepDisk Dependency = "disk"
)

// DepState 是依赖健康状态（normal → degraded → recovering → normal）。
type DepState string

const (
	// StateNormal 表示依赖可用：连续失败达到阈值才转入 degraded，并在此刻设起探测退避；
	// 期间的成功只把失败计数清零（避免偶发抖动误判）。
	StateNormal DepState = "normal"
	// StateDegraded 表示依赖不可用，消费方须按各自策略绕行。此态下失败只加倍退避
	// （封顶 BackoffMax）；一次成功即转入 recovering，探测窗口是否到由调用方把门。
	StateDegraded DepState = "degraded"
	// StateRecovering 是"正在确认恢复"的中间态：连续成功累计到 RecoverSuccesses 才回到
	// normal；期间任何一次失败立即退回 degraded，并把退避加倍——恢复必须是可证伪的。
	StateRecovering DepState = "recovering"
)

// DepConfig 是单依赖的退化参数。
type DepConfig struct {
	FailThreshold    int
	RecoverSuccesses int
	ProbeBackoff     time.Duration
	BackoffMax       time.Duration
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
	since        time.Time
	backoff      time.Duration
	lastProbe    time.Time
}

// DegradationManager 管理五依赖的退化状态机。并发安全（mu 保护）。
// onChange 在每次状态迁移时回调（默认实现由调用方注入：写 governance 事件 + 日志）。
type DegradationManager struct {
	mu       sync.Mutex
	states   map[Dependency]*depEntry
	onChange func(dep Dependency, from, to DepState)
	now      func() time.Time
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
		e.state = StateDegraded
		e.since = d.now()
		e.successCount = 0
		e.backoff = d.minDuration(e.backoff*2, e.cfg.BackoffMax)
	case StateDegraded:
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

// ShouldProbe 报告 degraded 依赖是否到了探测时机（退避窗口已过）——供**主动探测型**依赖
// 判断（半开熔断：degraded 期间只在探测窗放行真实调用）。当前五依赖均为**上报型**（真实操作
// 即探针：memory/disk/rustviking 经存储栈、model 经 RunFlow、mcp 经 mcp_call 直连后上报），
// 故 ShouldProbe/backoff 是预留能力（生产路径未接半开熔断，S-2）；接入主动探测型依赖时启用。
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
