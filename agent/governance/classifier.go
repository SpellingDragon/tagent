// Package governance 承载 tagent 的有界自治与审计：治理是闸不是墙，OS 降权仍是最后防线。
//
// - 风险分级 + 预算 + goal 登记 + critical 异步人工批准构成闸面；所有分级与裁决为纯函数（无 IO 无随机），规则表数据驱动，拒绝必记账。
//
// - 契约 C5：`RiskClassifier.Classify(RiskContext) → (level, ruleID, reason)`，消费方是 GovernanceGate。
//
// - evolution 的后验评估（guardrail/judge）独立于本管线：评估对象是改进窗口的表现证据，不是工具调用风险。
// 契约: docs/wiki/agent/governance-enforcement.md#disposition-and-risk
package governance

import (
	"strings"
)

// RiskLevel 是四级风险（低→危急）。
type RiskLevel int

const (
	// RiskLow 低风险：默认放行，不额外要求批准。
	RiskLow RiskLevel = iota
	// RiskMedium 中风险：由规则决定是否只记账放行。
	RiskMedium
	// RiskHigh 高风险：需批准后方可执行。
	RiskHigh
	// RiskCritical 最高风险：恒走异步批准流程，绝不因放行策略而跳过。
	RiskCritical
)

// String 返回风险级别名（记账/日志/事件用）。
func (l RiskLevel) String() string {
	switch l {
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	case RiskCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Disposition 是三档处置（由风险级别 + 策略派生）。
type Disposition int

const (
	// DispositionAllow 放行内层工具，无额外留痕要求。
	DispositionAllow Disposition = iota
	// DispositionRecord 放行，但要求把这次执行记入审计账本。
	DispositionRecord
	// DispositionHold 拦下：交给批准流程，内层工具不执行；放行后才委托内层。
	DispositionHold
)

// String 返回处置名。
func (d Disposition) String() string {
	switch d {
	case DispositionAllow:
		return "allow"
	case DispositionRecord:
		return "record"
	case DispositionHold:
		return "hold"
	default:
		return "unknown"
	}
}

// RiskContext 是分级输入（纯数据，无 IO）——GovernanceTool 装饰器从工具调用构造。
type RiskContext struct {
	ToolName      string
	ArgsJSON      string
	TriggerSource string
}

// Rule 是一条数据驱动的风险规则（纯函数匹配）。
type Rule struct {
	ID     string
	Level  RiskLevel
	Reason string
	Match  func(ctx RiskContext) bool
}

// RiskClassifier 是纯函数风险分级器（契约 C5）。规则按序匹配，首中即返回；
// 全不中返回默认级别（保守 medium）。无状态、并发安全（规则只读）。
type RiskClassifier struct {
	rules        []Rule
	defaultLevel RiskLevel
}

// NewRiskClassifier 构建分级器。rules 为空则用 DefaultRules()；defaultLevel<=0 取 RiskMedium。
func NewRiskClassifier(rules []Rule, defaultLevel RiskLevel) *RiskClassifier {
	if len(rules) == 0 {
		rules = DefaultRules()
	}
	if defaultLevel <= 0 {
		defaultLevel = RiskMedium
	}
	return &RiskClassifier{rules: rules, defaultLevel: defaultLevel}
}

// Classify 分级（契约 C5）：返回 (级别, 规则ID, 理由)。纯函数——同输入同输出。
func (c *RiskClassifier) Classify(ctx RiskContext) (RiskLevel, string, string) {
	for _, r := range c.rules {
		if r.Match != nil && r.Match(ctx) {
			return r.Level, r.ID, r.Reason
		}
	}
	return c.defaultLevel, "default", "无规则命中，保守默认分级"
}

// DispositionFor Disposition 由风险级别派生处置（默认策略；可被 Policy 覆盖）。
// critical → 挂起批准；high/medium → 记账放行；low → 直接放行。
func DispositionFor(level RiskLevel) Disposition {
	switch level {
	case RiskCritical:
		return DispositionHold
	case RiskHigh, RiskMedium:
		return DispositionRecord
	default:
		return DispositionAllow
	}
}

// argsContains 是规则谓词助手：ArgsJSON 含任一子串（大小写不敏感）即真。
func argsContains(ctx RiskContext, substrs ...string) bool {
	lower := strings.ToLower(ctx.ArgsJSON)
	for _, s := range substrs {
		if strings.Contains(lower, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// DefaultRules 返回 tagent 工具集的默认风险规则表（数据驱动，按序匹配，危急优先）。
// 设计：exec（shell）是主风险面，按命令内容分级；文件写/删中危；只读工具低危。
func DefaultRules() []Rule {
	return []Rule{
		{
			ID: "govface.readonly", Level: RiskLow,
			Reason: "治理面登记/查询工具（goal/denial/approval 只读或登记，无执行副作用）",
			Match: func(c RiskContext) bool {
				switch c.ToolName {
				case "goal_declare", "goal_list", "goal_resolve", "denial_query", "approval_list":
					return true
				}
				return false
			},
		},
		{
			ID: "exec.destructive", Level: RiskCritical,
			Reason: "不可逆破坏性命令（rm -rf / mkfs / dd / fork炸弹 / 关机 / 下载并管道执行远程脚本）",
			Match: func(c RiskContext) bool {
				if c.ToolName != "exec" {
					return false
				}
				if argsContains(c,
					"rm -rf", "rm -fr", "rm -r -f", "rm -f -r", "rm --recursive", "mkfs", "dd if=", ":(){", "shutdown", "reboot",
					"halt", "poweroff", "chmod -r 777", "> /dev/sda", "mv /* ",
					"git push --force", "git push -f",
					"git reset --hard", "git checkout -- .", "git checkout .", "git clean -f", "git clean -fd") {
					return true
				}
				lower := strings.ToLower(c.ArgsJSON)
				downloader := strings.Contains(lower, "curl") || strings.Contains(lower, "wget")
				pipesToShell := strings.Contains(lower, "| sh") || strings.Contains(lower, "|sh") ||
					strings.Contains(lower, "| bash") || strings.Contains(lower, "|bash")
				return downloader && pipesToShell
			},
		},
		{
			ID: "exec.cognitive-asset-write", Level: RiskCritical,
			Reason: "写入认知资产真源（默认清单 resources/prompts、skills、scripts 命中重定向/tee/sed -i/cp/mv/rm/python open(w) 写形态）——执行权在人，须人工批准。refine 登记不豁免",
			Match:  matchCognitiveAssetWrite,
		},
		{
			ID: "exec.privilege", Level: RiskCritical,
			Reason: "提权/系统配置改动（sudo 写系统路径、修改启动项）",
			Match: func(c RiskContext) bool {
				return c.ToolName == "exec" && argsContains(c,
					"sudo rm", "sudo dd", "sudo mkfs", "sudo shutdown", "sudo systemctl",
					"sudo passwd", "sudo useradd", "sudo userdel", "/etc/passwd", "/etc/shadow")
			},
		},
		{
			ID: "exec.sudo", Level: RiskHigh,
			Reason: "sudo 提权执行",
			Match: func(c RiskContext) bool {
				return c.ToolName == "exec" && argsContains(c, "sudo ")
			},
		},
		{
			ID: "exec.delete", Level: RiskHigh,
			Reason: "删除/移动/覆盖文件",
			Match: func(c RiskContext) bool {
				return c.ToolName == "exec" && argsContains(c, "rm ", "rm\t", "rmdir", "mv ", "shred", "unlink ")
			},
		},
		{
			ID: "exec.network-mutate", Level: RiskHigh,
			Reason: "外部写副作用（网络提交/部署/容器/发布）",
			Match: func(c RiskContext) bool {
				return c.ToolName == "exec" && argsContains(c,
					"git push", "docker rm", "docker rmi", "docker system prune", "kubectl delete",
					"npm publish", "cargo publish", "gh release", "scp ", "rsync ")
			},
		},
		{
			ID: "file.delete", Level: RiskHigh,
			Reason: "文件删除工具",
			Match: func(c RiskContext) bool {
				return c.ToolName == "delete_file" || c.ToolName == "remove_file"
			},
		},
		{
			ID: "refine.rollback", Level: RiskCritical,
			Reason: "refine rollback revert 改进 commit（直接修改受控产物，最高权限自我修改）",
			Match: func(c RiskContext) bool {
				return c.ToolName == "refine" && argsContains(c, `"op":"rollback"`, `"op": "rollback"`)
			},
		},
		{
			ID: "refine.ops", Level: RiskLow,
			Reason: "refine register 登记（git 留痕+开评估窗口）/ status 台账查询（无执行副作用）",
			Match: func(c RiskContext) bool {
				return c.ToolName == "refine"
			},
		},
		{
			ID: "file.write", Level: RiskMedium,
			Reason: "文件写入/修改",
			Match: func(c RiskContext) bool {
				switch c.ToolName {
				case "save_file", "replace_content", "write_file", "edit_file":
					return true
				}
				return false
			},
		},
		{
			ID: "exec.default", Level: RiskMedium,
			Reason: "shell 命令执行（默认中危，未命中更具体规则）",
			Match: func(c RiskContext) bool {
				return c.ToolName == "exec"
			},
		},
		{
			ID: "mcp.call", Level: RiskMedium,
			Reason: "MCP 外部工具调用（副作用未知，保守中危）",
			Match: func(c RiskContext) bool {
				return c.ToolName == "mcp_call"
			},
		},
		{
			ID: "readonly", Level: RiskLow,
			Reason: "只读工具（读文件/检索/召回/列目录），无副作用",
			Match: func(c RiskContext) bool {
				switch c.ToolName {
				case "read_file", "list_file", "search_file", "search_content",
					"read_multiple_files", "recall", "memory_query", "memory_recall",
					"skill_search", "skill_load", "mcp_discover", "list_tasks":
					return true
				}
				return false
			},
		},
	}
}
