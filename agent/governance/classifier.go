// Package governance 承载 tagent 的有界自治与审计（T-G 常驻可靠性+治理）。
//
// 核心理念（报告 D3）：治理是「闸不是墙」——风险分级 + 预算 + goal 登记 + critical
// 异步人工批准，OS 降权（sudo -n -u）仍是最后防线。所有分级/裁决为纯函数（无 IO
// 无随机），规则表数据驱动，可表格测试；拒绝必记账（DenialLedger + governance 事件）。
//
// 契约 C5：RiskClassifier.Classify(RiskContext) → (level, ruleID, reason)，纯函数。
// 消费方：GovernanceGate（工具执行治理）。注：T-EVO 发布道用独立的 evolution.DiffRiskRouter
// 按 bundle diff 路由，**不复用**本分级器（分层独立，输入域不同：工具调用 vs 版本变更）。
package governance

import (
	"strings"
)

// RiskLevel 是四级风险（低→危急）。
type RiskLevel int

const (
	RiskLow RiskLevel = iota
	RiskMedium
	RiskHigh
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
	DispositionAllow  Disposition = iota // 放行（零开销）
	DispositionRecord                    // 记账放行（governance 事件 subtype=audit）
	DispositionHold                      // 挂起人工批准（critical，异步不阻塞 loop）
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
	ToolName      string // 工具名（exec/save_file/...）
	ArgsJSON      string // 工具参数 JSON（规则做子串匹配，如 exec 的 command）
	TriggerSource string // user/meditation/task/tmux/subagent/inject
}

// Rule 是一条数据驱动的风险规则（纯函数匹配）。
type Rule struct {
	ID     string                     // 规则标识（记账/审计引用）
	Level  RiskLevel                  // 命中时的风险级别
	Reason string                     // 人类可读理由
	Match  func(ctx RiskContext) bool // 匹配谓词（纯函数，无 IO 无随机）
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

// Disposition 由风险级别派生处置（默认策略；可被 Policy 覆盖）。
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
		// === critical：不可逆/系统级破坏 ===
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
					"git push --force", "git push -f") {
					return true
				}
				// 下载并管道执行远程脚本（curl/wget ... | sh/bash）——精确检测，
				// 避免 "| shasum" 之类误命中。
				lower := strings.ToLower(c.ArgsJSON)
				downloader := strings.Contains(lower, "curl") || strings.Contains(lower, "wget")
				pipesToShell := strings.Contains(lower, "| sh") || strings.Contains(lower, "|sh") ||
					strings.Contains(lower, "| bash") || strings.Contains(lower, "|bash")
				return downloader && pipesToShell
			},
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
		// === high：外部副作用/权限提升/数据删除 ===
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
		// === medium：写入/修改（可逆但有副作用）===
		{
			ID: "refine.rollback", Level: RiskCritical,
			Reason: "refine rollback 直接切换 active bundle（绕过发布道评估，最高权限自我修改）",
			Match: func(c RiskContext) bool {
				return c.ToolName == "refine" && argsContains(c, `"op":"rollback"`, `"op": "rollback"`)
			},
		},
		{
			ID: "refine.propose", Level: RiskMedium,
			Reason: "refine propose 提案自我修改（经发布道裁决，占预算 + 记账）",
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
		// === low：只读/无副作用 ===
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
