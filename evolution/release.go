package evolution

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ==================== ReleaseManager（T-EVO · 风险分级发布道）====================
//
// 调和 T-E（后验评估+模型回滚）与 T-F（refine 无直接生效、门后生效）的矛盾：
// 二者作用在不同层，经「风险分级发布道」统一为一台状态机的两条道——
//
//	fast（低风险+可逆）: validate → canary 激活 → 后验评估(Evaluator) → 通过则 active / 劣化则回滚
//	slow（高风险/protected）: validate → replay(cassette) → shadow → canary → approve(人工) → active
//
// 铁律保持（报告 D1）：agent 永不直接激活——refine 只 propose（无 activate op），激活必经
// 本状态机；快道激活是策略自动（低风险），非 agent 直接意志。指令4 放松的是「评估通过才
// 生效」（快道先生效后验），未放松「agent 不能直接生效」。
//
// 双回滚触发（canary 阶段共用）：Guardrail（确定性闸，快、廉价）+ Evaluator（LLM-judge，
// 慢、质性）。回滚 = BundleStore.Rollback 到父 bundle（原子切 active 指针，热配置回合边界生效）。

// Lane 是发布道。
type Lane string

const (
	LaneFast Lane = "fast" // 低风险+可逆：先生效后验
	LaneSlow Lane = "slow" // 高风险/protected：门后生效
)

// ReleaseStage 是发布状态机阶段。
type ReleaseStage string

const (
	StageDraft      ReleaseStage = "draft"
	StageRejected   ReleaseStage = "rejected"
	StageCanary     ReleaseStage = "canary"
	StageActive     ReleaseStage = "active"
	StageRolledBack ReleaseStage = "rolled_back"
)

// GateFunc 是一道确定性门（validate/replay/shadow/approve）：返回 (通过, 理由)。
type GateFunc func(ctx context.Context, draft *Bundle) (pass bool, reason string)

// EvalResult 是后验评估结果（T-E）。
type EvalResult struct {
	Score  float64
	Pass   bool
	Reason string
}

// Evaluator 是后验评估器（T-E）：对一个已激活（canary）bundle 的实际表现打分。
// 实现可为 LLM-judge / 程序化评分器 / 指标阈值。nil = 跳过后验（仅靠 guardrail）。
type Evaluator interface {
	Evaluate(ctx context.Context, bundleID string) (EvalResult, error)
}

// Guardrail 是 canary 实时监控闸（确定性、快）：breach 则立即回滚。nil = 不监控。
type Guardrail interface {
	Breach(bundleID string) (breached bool, reason string)
}

// LaneRouter 决定一个 bundle 变更走哪条发布道（按 diff 内容判定，实现见 DiffLaneRouter）。
// **与 governance.RiskClassifier(C5) 分层独立**：后者管运行时工具调用风险（输入 tool+args），
// 本接口管版本发布道选择（输入 bundle diff）；evolution 不 import governance（见 eval.go 声明）。
// nil = 一律走 slow（保守）。命名 LaneRouter（非 RiskRouter）使 "Risk" 词根在仓库内专属 C5。
type LaneRouter interface {
	Route(diff BundleDiff) Lane
}

// ReleaseConfig 配置发布状态机。
type ReleaseConfig struct {
	// SkipApprovalGate 是反义字段：零值(false) = 慢道需人工批准门（保守默认，报告 D1）；
	// true = 跳过批准门。用反义使 Go 零值 == 保守行为，根除「文档说默认 true 但 bool 零值
	// 是 false」的默认反转（A4：用户开 evolution 不显式配置却得到无批准门）。
	SkipApprovalGate bool
	CanaryHoldMs     int64    // canary 激活后到后验评估的观察窗（默认 0 = 立即评估）
	ProtectedPrompts []string // 受保护提示词（改动强制走 slow 道）
}

// ReleaseRecord 是一次发布的留痕（审计）。
type ReleaseRecord struct {
	BundleID  string       `json:"bundle_id"`
	ParentID  string       `json:"parent_id"`
	Lane      Lane         `json:"lane"`
	Stage     ReleaseStage `json:"stage"`
	Reason    string       `json:"reason"`
	EvalScore float64      `json:"eval_score,omitempty"`
	Timestamp int64        `json:"ts"`
}

// ReleaseManager 是风险分级发布状态机。并发安全。
type ReleaseManager struct {
	store     *BundleStore
	router    LaneRouter
	evaluator Evaluator
	guardrail Guardrail
	// 门（可注入；nil 门视为通过——便于渐进接线）。
	validateGate GateFunc
	replayGate   GateFunc
	shadowGate   GateFunc
	approveGate  GateFunc
	cfg          ReleaseConfig

	// activationLog 记录 canary 激活时刻（W4）：供 StoreEvidenceSource 以后验评估窗口起点，
	// 而非固定回看（CanaryHold=0 时固定窗全是旧 bundle 数据，对新 bundle 无判别力）。
	activationLog *ActivationLog

	submitMu sync.Mutex // 序列化 Submit：activate+evaluate+rollback 临界区原子（防并发 Submit 竞争 SetActive）
	mu       sync.Mutex // 保护 history
	history  []ReleaseRecord
}

// ReleaseDeps 是构建 ReleaseManager 的依赖集。
type ReleaseDeps struct {
	Store        *BundleStore
	Router       LaneRouter
	Evaluator    Evaluator
	Guardrail    Guardrail
	ValidateGate GateFunc
	ReplayGate   GateFunc
	ShadowGate   GateFunc
	ApproveGate  GateFunc
	Config       ReleaseConfig
}

// NewReleaseManager 构建发布状态机。store 必填。
func NewReleaseManager(deps ReleaseDeps) (*ReleaseManager, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("evolution: ReleaseManager requires BundleStore")
	}
	return &ReleaseManager{
		store: deps.Store, router: deps.Router, evaluator: deps.Evaluator, guardrail: deps.Guardrail,
		validateGate: deps.ValidateGate, replayGate: deps.ReplayGate,
		shadowGate: deps.ShadowGate, approveGate: deps.ApproveGate,
		cfg:           deps.Config,
		activationLog: NewActivationLog(), // W4：内部建，经 getter 共享给 StoreEvidenceSource
	}, nil
}

// ActivationLog 暴露 canary 激活时刻表（W4）：buildAgent 接线时共享给 StoreEvidenceSource，
// 使后验评估证据窗口以 bundle 激活时刻为起点（否则 CanaryHold=0 时对刚激活的 bundle 无判别力）。
func (rm *ReleaseManager) ActivationLog() *ActivationLog { return rm.activationLog }

// Submit 提交一个 draft bundle 走发布状态机。返回最终 ReleaseRecord。
// agent 只能 propose（调本方法），激活由状态机按策略决定——agent 无直接激活权。
func (rm *ReleaseManager) Submit(ctx context.Context, draft *Bundle) (ReleaseRecord, error) {
	if draft == nil {
		return ReleaseRecord{}, fmt.Errorf("evolution: nil draft bundle")
	}
	// 序列化整个发布流程（validate→activate→evaluate→promote/rollback 为原子临界区），
	// 防并发 Submit 竞争 SetActive 造成 active 指针错乱。Submit 稀疏（自进化提案），
	// 持锁跨越评估器（可能慢）可接受。
	rm.submitMu.Lock()
	defer rm.submitMu.Unlock()
	active := rm.store.Active()
	lane := rm.route(active, draft)

	// gate1: validate（恒过——廉价确定性：schema/可加载/diff 限额）。
	if pass, reason := rm.runGate(rm.validateGate, ctx, draft); !pass {
		return rm.record(draft, lane, StageRejected, "validate 失败: "+reason, 0), nil
	}

	if lane == LaneSlow {
		return rm.runSlowLane(ctx, draft, lane)
	}
	return rm.runFastLane(ctx, draft, active, lane)
}

// runFastLane：低风险+可逆 → 先激活（canary）→ 后验评估 → 通过则 active / 劣化则回滚。
func (rm *ReleaseManager) runFastLane(ctx context.Context, draft, active *Bundle, lane Lane) (ReleaseRecord, error) {
	// 激活（canary）：热配置原子切换，回合边界生效。
	if err := rm.store.SetActive(draft.ID); err != nil {
		return rm.record(draft, lane, StageRejected, "canary 激活失败: "+err.Error(), 0), nil
	}
	rm.activationLog.Record(draft.ID, time.Now().UnixMilli()) // W4：记录激活时刻（后验窗口起点）
	if rm.cfg.CanaryHoldMs > 0 {
		select {
		case <-ctx.Done():
			// canary 观察被取消：诚实停留 canary（不回滚也不提升 active），下次 Submit/重启后
			// 重评——绝不把未评估的变更记为「后验通过」（Major：ctx 取消经 judge 保守 Pass:true
			// 会落到假通过路径，与「evaluator error 不许当通过」的意图矛盾）。
			return rm.record(draft, lane, StageCanary, "canary 观察被取消，保持 canary 待重评: "+ctx.Err().Error(), 0), nil
		case <-time.After(time.Duration(rm.cfg.CanaryHoldMs) * time.Millisecond):
		}
	}
	// 双回滚触发①：guardrail 确定性闸（快）。
	if rm.guardrail != nil {
		if breached, reason := rm.guardrail.Breach(draft.ID); breached {
			rm.rollback(active)
			return rm.record(draft, lane, StageRolledBack, "guardrail 违约回滚: "+reason, 0), nil
		}
	}
	// 双回滚触发②：后验评估（LLM-judge/程序化，模型决策回滚）。
	if rm.evaluator != nil {
		res, err := rm.evaluator.Evaluate(ctx, draft.ID)
		if err != nil {
			// 评估失败（ctx 取消/LLM 不可用/评分器异常）：保守回滚——无法确认安全则
			// 不留在 active（修复：绝不把 evaluator error 当「通过」造成假激活）。
			rm.rollback(active)
			return rm.record(draft, lane, StageRolledBack, "后验评估失败(错误)，保守回滚: "+err.Error(), 0), nil
		}
		if !res.Pass {
			rm.rollback(active)
			return rm.record(draft, lane, StageRolledBack, "后验评估劣化回滚: "+res.Reason, res.Score), nil
		}
		// M3（§8.4）：保留 judge 的评估原因到发布留痕——保守通过（judge 不可用/样本不足/解析
		// 失败）时 res.Reason 说明"为何保守通过"，固定文案"后验通过"会覆盖这一关键审计信息
		// （运维无法区分「真评估通过」与「judge 缺席保守放行」）。
		passReason := "后验通过，正式生效"
		if res.Reason != "" {
			passReason += "（judge: " + res.Reason + "）"
		}
		return rm.record(draft, lane, StageActive, passReason, res.Score), nil
	}
	// 无评估器：canary 即 active（仅 guardrail 守护）。
	return rm.record(draft, lane, StageActive, "canary 生效（无后验评估器）", 0), nil
}

// runSlowLane：高风险/protected → replay → shadow → canary → approve → active（门后生效）。
func (rm *ReleaseManager) runSlowLane(ctx context.Context, draft *Bundle, lane Lane) (ReleaseRecord, error) {
	if pass, reason := rm.runGate(rm.replayGate, ctx, draft); !pass {
		return rm.record(draft, lane, StageRejected, "replay(cassette) 门失败: "+reason, 0), nil
	}
	if pass, reason := rm.runGate(rm.shadowGate, ctx, draft); !pass {
		return rm.record(draft, lane, StageRejected, "shadow 门失败: "+reason, 0), nil
	}
	// canary 激活 + guardrail 监控。
	if err := rm.store.SetActive(draft.ID); err != nil {
		return rm.record(draft, lane, StageRejected, "canary 激活失败: "+err.Error(), 0), nil
	}
	rm.activationLog.Record(draft.ID, time.Now().UnixMilli()) // W4：记录激活时刻（后验窗口起点）
	if rm.guardrail != nil {
		if breached, reason := rm.guardrail.Breach(draft.ID); breached {
			rm.rollbackTo(draft.ParentID)
			return rm.record(draft, lane, StageRolledBack, "canary guardrail 违约: "+reason, 0), nil
		}
	}
	// 人工批准门（默认需要；SkipApprovalGate=true 才跳过——反义字段使零值保守）。
	// E2（§8.3）：approveGate 为 nil 且未显式 SkipApprovalGate → **reject**（不得空转通过）。
	// runGate 的"nil 门视为通过"仅适用于 validate/replay/shadow 等渐进接线的非安全门；审批门是
	// 安全关键——nil 空转会使 protected 提示词零审批即激活，违反"高风险必经人工批准"铁律。
	// 要求运维显式注入 approveGate 或显式声明 SkipApprovalGate（实验环境）。
	if !rm.cfg.SkipApprovalGate {
		if rm.approveGate == nil {
			rm.rollbackTo(draft.ParentID)
			return rm.record(draft, lane, StageRejected, "慢道需人工批准但 approveGate 未接线且未显式 SkipApprovalGate——拒绝空转通过（E2：要求显式注入门或显式跳过）", 0), nil
		}
		if pass, reason := rm.runGate(rm.approveGate, ctx, draft); !pass {
			rm.rollbackTo(draft.ParentID)
			return rm.record(draft, lane, StageRolledBack, "人工批准拒绝，回滚: "+reason, 0), nil
		}
	}
	return rm.record(draft, lane, StageActive, "slow 道全门通过，正式生效", 0), nil
}

// route 决定发布道：protected 提示词改动强制 slow；否则委托 LaneRouter；无 router 走 slow。
func (rm *ReleaseManager) route(active, draft *Bundle) Lane {
	diff := Diff(active, draft)
	if rm.touchesProtected(diff) {
		return LaneSlow
	}
	if rm.router == nil {
		return LaneSlow // 保守：无路由器一律慢道
	}
	return rm.router.Route(diff)
}

func (rm *ReleaseManager) touchesProtected(diff BundleDiff) bool {
	if len(rm.cfg.ProtectedPrompts) == 0 {
		return false
	}
	changed := map[string]bool{}
	for _, k := range diff.PromptsChanged {
		changed[k] = true
	}
	for _, k := range diff.PromptsRemoved {
		changed[k] = true
	}
	for _, p := range rm.cfg.ProtectedPrompts {
		if changed[p] {
			return true
		}
	}
	return false
}

func (rm *ReleaseManager) runGate(g GateFunc, ctx context.Context, draft *Bundle) (bool, string) {
	if g == nil {
		return true, "" // nil 门视为通过（渐进接线）
	}
	return g(ctx, draft)
}

func (rm *ReleaseManager) rollback(to *Bundle) {
	if to != nil {
		_ = rm.store.Rollback(to.ID)
	}
}

func (rm *ReleaseManager) rollbackTo(id string) {
	if id != "" {
		_ = rm.store.Rollback(id)
	}
}

func (rm *ReleaseManager) record(draft *Bundle, lane Lane, stage ReleaseStage, reason string, score float64) ReleaseRecord {
	rec := ReleaseRecord{
		BundleID: draft.ID, ParentID: draft.ParentID, Lane: lane,
		Stage: stage, Reason: reason, EvalScore: score, Timestamp: time.Now().UnixMilli(),
	}
	rm.mu.Lock()
	rm.history = append(rm.history, rec)
	rm.mu.Unlock()
	return rec
}

// History 返回发布留痕（审计）。
func (rm *ReleaseManager) History() []ReleaseRecord {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	out := make([]ReleaseRecord, len(rm.history))
	copy(out, rm.history)
	return out
}

// wasActive 报告 bundleID 是否在发布历史中曾达到 Stage=active（E1：refine rollback 目标白名单，
// 只允许回滚到曾正式生效的版本，防直接激活被拒 draft 绕过发布道）。rm 为 nil 时返回 false（保守）。
func (rm *ReleaseManager) wasActive(bundleID string) bool {
	if rm == nil {
		return false
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	for _, rec := range rm.history {
		if rec.BundleID == bundleID && rec.Stage == StageActive {
			return true
		}
	}
	return false
}

// BindPosterior 延迟绑定后验评估器（Evaluator）+ 指标闸（Guardrail）。二者的 EvidenceSource
// 需 entry agent 的 memStore（New 构造 ReleaseManager 时尚未就绪，per-agent 在 buildAgent
// 构造），故延迟到 memStore 就绪后绑定。持 submitMu 与 Submit 互斥（字段读写序列化）。
func (rm *ReleaseManager) BindPosterior(eval Evaluator, guard Guardrail) {
	rm.submitMu.Lock()
	defer rm.submitMu.Unlock()
	if eval != nil {
		rm.evaluator = eval
	}
	if guard != nil {
		rm.guardrail = guard
	}
}
