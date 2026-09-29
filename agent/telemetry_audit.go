package agent

import (
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

const (
	auditWindow       = 2 * time.Hour
	auditRatio        = 0.40
	auditMinSamples   = 20
	auditDwellPerStep = 30 * time.Minute
)

// auditLineageInternal are the settle lineages that are agent-self-managed
// (their reclaim output is not a user-originated interaction): the same
// negative list the delivery gate withholds on.
var auditLineageInternal = map[string]bool{
	"meditation":   true,
	"task-retired": true,
	"unknown":      true,
}

// SelfTelemetryAuditor 按滑动窗口样本判定自身遥测的可见性该升到哪一档：窗口时长、
// 负例占比、最少样本数与每档驻留时间都是命名常量，避免"看一眼就永久外显"或"长期沉默
// 无人察觉"。它只统计自管谱系（冥想、任务退役、未知）——这些产出不是用户发起的交互，
// 与投递门使用同一份负例清单。
type SelfTelemetryAuditor struct {
	mu       sync.Mutex
	samples  []auditSample
	level    int
	levelAt  time.Time
	onAction func(level int, ratio float64, samples int, frozen bool)

	// now is injectable for deterministic tests.
	now func() time.Time
}

type auditSample struct {
	ts          time.Time
	selfManaged bool
}

// NewSelfTelemetryAuditor creates an auditor; onAction receives every level
// transition AND periodic evaluation (for fact-chain recording) — may be nil.
func NewSelfTelemetryAuditor(onAction func(level int, ratio float64, samples int, frozen bool)) *SelfTelemetryAuditor {
	return &SelfTelemetryAuditor{onAction: onAction, now: time.Now}
}

// ObserveSettle classifies one settle event by its Origin-courier lineage and
// records the sample. self-managed = internal lineage or absent lineage
// (unknown stays conservative per the withhold philosophy). Accepts the bus
// event's map[string]any metadata (values are written as strings by every
// producer).
func (a *SelfTelemetryAuditor) ObserveSettle(metadata map[string]any) {
	if a == nil {
		return
	}
	str := func(k string) string {
		if v, ok := metadata[k]; ok {
			if s, is := v.(string); is {
				return s
			}
		}
		return ""
	}
	ts := str("meta_trigger_source")
	if ts == "" {
		ts = str("trigger_source")
	}
	selfManaged := str("lineage_absent") == "true" || ts == "" || auditLineageInternal[ts]
	a.observe(selfManaged)
}

// ObserveInput records a non-settle environmental input (user injection) as
// an external sample — it dilutes the self-managed ratio honestly.
func (a *SelfTelemetryAuditor) ObserveInput() {
	if a == nil {
		return
	}
	a.observe(false)
}

// ObserveInputFor records an injected input classified by its source
// (meditation-derived injections are self-managed traffic, user/API are not).
func (a *SelfTelemetryAuditor) ObserveInputFor(source string) {
	if a == nil {
		return
	}
	a.observe(auditLineageInternal[source])
}

func (a *SelfTelemetryAuditor) observe(selfManaged bool) {
	a.mu.Lock()
	now := a.now()
	a.samples = append(a.samples, auditSample{ts: now, selfManaged: selfManaged})
	cut := now.Add(-auditWindow)
	head := 0
	for head < len(a.samples) && a.samples[head].ts.Before(cut) {
		head++
	}
	if head > 0 {
		a.samples = append(a.samples[:0], a.samples[head:]...)
	}
	before := a.level
	a.evaluateLocked(now)
	transitioned := a.level != before
	action := a.onAction
	ratio, n := a.ratioLocked()
	a.mu.Unlock()
	if action != nil && (transitioned || ratio > 0) {
		action(a.level, ratio, n, a.Level() >= auditFreeze)
	}
}

const auditFreeze = 3

// evaluateLocked escalates/de-escalates against the current window ratio.
// Caller holds a.mu.
func (a *SelfTelemetryAuditor) evaluateLocked(now time.Time) {
	ratio, n := a.ratioLocked()
	if n < auditMinSamples {
		return
	}
	if ratio < auditRatio {
		if a.level > 0 && ratio < auditRatio/2 {
			a.level = 0
			a.levelAt = now
		}
		return
	}
	if a.level == 0 {
		a.level = 1
		a.levelAt = now
		return
	}
	if now.Sub(a.levelAt) < auditDwellPerStep {
		return
	}
	if a.level < auditFreeze {
		a.level++
		a.levelAt = now
	}
}

func (a *SelfTelemetryAuditor) ratioLocked() (float64, int) {
	n := len(a.samples)
	if n == 0 {
		return 0, 0
	}
	self := 0
	for _, s := range a.samples {
		if s.selfManaged {
			self++
		}
	}
	return float64(self) / float64(n), n
}

// Level reports the current action level (0 normal … 3 frozen).
func (a *SelfTelemetryAuditor) Level() int {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.level
}

// Snapshot exposes the audit state for reflection-layer consumers (meditation
// digest): current ladder level, window ratio and sample count.
func (a *SelfTelemetryAuditor) Snapshot() (level int, ratio float64, samples int) {
	if a == nil {
		return 0, 0, 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	ratio, samples = a.ratioLocked()
	return a.level, ratio, samples
}

// DigestLine renders the deterministic self-state digest row (trajectory
// statistics belong to the reflection layer, not the resident context).
// Empty when the auditor has no samples.
func (a *SelfTelemetryAuditor) DigestLine() string {
	level, ratio, samples := a.Snapshot()
	if samples == 0 {
		return ""
	}
	return fmt.Sprintf("- 自管遥测审计：窗口 %d 样本，自管占比 %.0f%%，当前动作级别 L%d", samples, ratio*100, level)
}

// GateReason implements the task layer's AuditGate contract. L2 converge
// refuses SELF-MANAGED spawns only (internal-lineage Origin — the chores that
// feed the spin); L3 freeze refuses every non-protected spawn. Protected
// specs pass both steps: the durability defense is never withdrawn for
// attention governance.
func (a *SelfTelemetryAuditor) GateReason(spec task.TaskSpec) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	level := a.level
	ratio, n := a.ratioLocked()
	a.mu.Unlock()
	switch {
	case level >= auditFreeze && !spec.Protected:
		return fmt.Sprintf("自管遥测占比 %.0f%%（窗口样本 %d）持续超阈且频率收敛无效 — 冻结新任务纳管（保护类豁免；在飞任务不受影响）", ratio*100, n)
	case level == 2 && !spec.Protected:
		if lineage := spec.Origin["meta_trigger_source"]; lineage == "" || auditLineageInternal[lineage] {
			return fmt.Sprintf("自管遥测占比 %.0f%%（窗口样本 %d）超阈 — 收敛自管任务频率（L2，用户派生与保护任务不受影响）", ratio*100, n)
		}
	}
	return ""
}
