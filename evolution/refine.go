package evolution

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ==================== refine 工具（git 原生自我改进通道）====================
//
// 三操作（Q 系裁决）：register（登记：git commit+improvement 事件+开评估窗口）/
// status（改进台账+窗口结论+未登记提醒）/ rollback（安全 git revert，终态不评估）。
// diff 已删——exec git diff 可达。哲学：改文件即生效（P2），登记是评估与回滚保护的
// 前提而非生效前提；劣化只出建议（P4）。

// refineArgs 是 refine 工具入参。
type refineArgs struct {
	Op    string   `json:"op" jsonschema:"description=操作,enum=register,enum=status,enum=rollback"`
	Paths []string `json:"paths,omitempty" jsonschema:"description=register:改进产物路径(受控清单内)"`
	Note  string   `json:"note,omitempty" jsonschema:"description=register:痛点→产物→预期收益"`
	Sha   string   `json:"sha,omitempty" jsonschema:"description=rollback:目标改进 commit sha(可前缀)"`
}

// refineResult 是 refine 工具输出。
type refineResult struct {
	Op      string   `json:"op"`
	OK      bool     `json:"ok"`
	Message string   `json:"message"`
	Sha     string   `json:"sha,omitempty"`
	Items   []string `json:"items,omitempty"` // status: 台账行
}

// NewRefineTool 构建 git 原生 refine 工具（entry only，装配层先于治理包裹追加——A3）。
func NewRefineTool(g *GitEvolution) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args refineArgs) (refineResult, error) {
			switch args.Op {
			case "register":
				return refineRegister(g, args)
			case "status":
				return refineStatus(g)
			case "rollback":
				return refineRollback(g, args)
			default:
				return refineResult{Op: args.Op, OK: false},
					fmt.Errorf("未知 op %q（白名单：register/status/rollback）", args.Op)
			}
		},
		function.WithName("refine"),
		function.WithDescription("git 原生自我改进通道（默认生效哲学：改文件即生效，本工具负责登记/台账/回滚）。"+
			"op：① register——产物落盘后登记留痕（paths+note），开评估窗口，劣化将有回滚建议；"+
			"未登记的改进没有评估保护也无法安全回滚；② status——改进历史+各窗口评估结论+未登记提醒；"+
			"③ rollback——安全回滚指定改进 commit（仅 [self-improve] 标记，防误 revert 用户提交）。"),
	)
}

func refineRegister(g *GitEvolution, args refineArgs) (refineResult, error) {
	if len(args.Paths) == 0 || args.Note == "" {
		return refineResult{Op: "register", OK: false}, fmt.Errorf("register 需 paths 与 note(痛点→产物→预期收益)")
	}
	sha, msg, err := g.Register(args.Paths, args.Note)
	if err != nil {
		return refineResult{Op: "register", OK: false, Message: err.Error()}, err
	}
	return refineResult{Op: "register", OK: true, Sha: sha, Message: msg}, nil
}

func refineStatus(g *GitEvolution) (refineResult, error) {
	res := refineResult{Op: "status", OK: true}
	infos, err := GitLogFiltered(g.cfg.WorkDir, 20)
	if err != nil {
		return res, fmt.Errorf("改进台账读取失败（需 git 仓）: %w", err)
	}
	evals := g.Evaluations()
	for _, c := range infos {
		line := fmt.Sprintf("%s %s %s", shortSha(c.Sha), c.Time, c.Note)
		if ev, ok := evals[c.Sha]; ok {
			line += " ｜评估:" + ev.Verdict
			if ev.Reason != "" {
				line += "（" + ev.Reason + "）"
			}
			if ev.Advice != "" {
				line += " ｜" + ev.Advice
			}
		} else {
			line += " ｜评估:未到期"
		}
		res.Items = append(res.Items, line)
	}
	if un := g.Unregistered(); len(un) > 0 {
		res.Items = append(res.Items, fmt.Sprintf("⚠ 未登记产物（已改动未 register，无评估保护）：%v", un))
	}
	res.Message = fmt.Sprintf("改进 %d 条", len(infos))
	return res, nil
}

func refineRollback(g *GitEvolution, args refineArgs) (refineResult, error) {
	if args.Sha == "" {
		return refineResult{Op: "rollback", OK: false}, fmt.Errorf("rollback 需 sha（改进 commit，可前缀）")
	}
	out, err := GitRevertSafe(g.cfg.WorkDir, args.Sha)
	if err != nil {
		// 冲突/校验失败以 result 渗透详情（不 error 打断——失败渗透原则）
		return refineResult{Op: "rollback", OK: false, Message: err.Error() + "｜git 输出:" + out}, nil
	}
	// 裁决 Q3：回滚是终态——不写事件、不开窗口（信任执行者）。
	return refineResult{Op: "rollback", OK: true,
		Message: "已回滚 " + shortSha(args.Sha) + "（revert commit 已生成；文件即真源，热重载即时生效）"}, nil
}
