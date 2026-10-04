// 本文件承载世代状态类型：状态/失败/存活债/关闭态/应用记录的外向形状。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
package org

import (
	"time"

	"github.com/SpellingDragon/tagent/agent"
)

// OrgFailure / OrgStatus 是代际诊断的有界形状（D9：成功与失败给同一语义检查的
// 明确可诊断结果，不新增抓取协议、不新增历史）。
//
// Fingerprint/Desired 只能当**不透明诊断标签**：按 resident-continuity 的声明，
// 指纹/序号不是应用可见 identity，任何执行路径不得据它选版（本仓也无这样的
// 读取点）。Desired 是“磁盘上那份配置算出来的指纹”：它与 Fingerprint 不等才是
// 运维真正要看到的事实（“我改了，为什么没生效”）；解析/指纹本身失败时为空。
type OrgFailure struct {
	// Generation 失败时所在代（候选被拒，代不前进）
	Generation int `json:"generation"`
	// Desired 被拒候选的 desired 指纹前缀（可为空）
	Desired string    `json:"desired"`
	Error   string    `json:"error"`
	At      time.Time `json:"at"`
}

// OrgStatus 是一次原子读出的代际诊断快照：已发布代、两个时间戳、最后一次被拒候选，
// 以及本轮逐 agent 回执。
type OrgStatus struct {
	Generation int64 `json:"generation"`
	// Revision 完整应用计数（含 numeric-only），非路由源
	Revision    int64  `json:"revision"`
	Fingerprint string `json:"fingerprint"`
	Desired     string `json:"desired"`
	// LastApplied 完整配置最近成功应用时间（含 numeric-only）
	LastApplied time.Time `json:"lastAppliedAt"`
	// LastPublished 最近结构发布／回滚时间
	LastPublished time.Time   `json:"lastPublishedAt"`
	LastFailure   *OrgFailure `json:"lastFailure,omitempty"`
	// Agents 本轮逐 agent 回执（整块替换，无历史）
	Agents []OrgAgentApply `json:"agents"`
}

// OrgLiveDebt is the LIVE half of the diagnostics payload. Unlike
// OrgStatus — which one `coord.status()` call produces as one atomic read — the
// figures below are read off the running reference accounting AT THE MOMENT the
// payload was asked for, so they describe "debt right now", not "what the
// committed record says". They are therefore grouped apart from the record
// fields and carry their own capture instant, which is what lets a reader tell
// the two kinds apart instead of mistaking a stitched-together view for an
// atomic success snapshot. No execution path reads any of it.
type OrgLiveDebt struct {
	CapturedAt         time.Time          `json:"capturedAt"`
	Executors          agent.ExecutorRefs `json:"executors"`
	PendingRetirements []map[string]any   `json:"pendingRetirements"`
}

// OrgCloseState keeps 「关闭已发起」and「资源已退出」as the two distinct facts they
// are. Initiated flips on the first
// Close call; ResourcesExited turns true only once every deferred exit has run
// — or immediately when nothing was ever deferred (an inline close that took
// every exit is not "incomplete"). Collapsing the two would let a bounded return
// be read as a finished teardown, which is exactly the misreading refused.
type OrgCloseState struct {
	Initiated       bool `json:"initiated"`
	ResourcesExited bool `json:"resourcesExited"`
}

// OrgAgentApply 是一个 agent 在本轮数值热更中的回执，Outcome 只有两种真实结果。
//
// - applied：它属于本代可路由拓扑，参数已下发到它的真实对象。
// - draining：本代不路由它（被移除或已降级为旧 owner），不碰它，数值字段保持零值，含义是本轮未评估。
//
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
type OrgAgentApply struct {
	Name            string  `json:"name"`
	Outcome         string  `json:"outcome"`
	ThresholdPct    float64 `json:"thresholdPct,omitempty"`
	MaxTokens       int     `json:"maxTokens,omitempty"`
	KeepRecentTasks int     `json:"keepRecentTasks,omitempty"`
}
