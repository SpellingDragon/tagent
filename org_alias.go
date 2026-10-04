// 本文件是世代状态类型与簿记的根包再导出面：实体在 agent/org，别名不加语义。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
package tagent

import "github.com/SpellingDragon/tagent/agent/org"

// OrgFailure 是一次世代失败的不可变记录：指纹、代序、原因与各 agent 的应用面。
type OrgFailure = org.OrgFailure

// OrgStatus 是热更协调器的诊断快照。
type OrgStatus = org.OrgStatus

// OrgLiveDebt 是仍在旧代存活的 agent 清单（存活债）。
type OrgLiveDebt = org.OrgLiveDebt

// OrgCloseState 是关闭路径的阶段性事实。
type OrgCloseState = org.OrgCloseState

// OrgAgentApply 是单个 agent 在一次世代应用中的结果记录。
type OrgAgentApply = org.OrgAgentApply
