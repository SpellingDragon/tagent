package action

// 走私入舱引导（cognitive-asset-guard D4·脚手架）：
// 远端观察到 agent 因异步执行失败「静默、无归因」而绕开框架托管——用 `nohup … &`
// 或嵌套 `tmux new-session` 把长作业走私进无人看的通道（这正是 79 通知零失败极性的
// 起点）。本脚手架在命令文本命中走私形态时，于结果尾部附一行引导：说明该形态不受
// 框架托管（无结算通知、无 TTL 保护），并指向合法形态（mode:resident + 大 ttl）。
//
// 硬约束：纯提示——只读命令文本、不改命令、零延迟、零拦截。
// 脚手架性质：走私的存在理由 = 失败极性缺失造成的归因真空。failure-polarity-passthrough
// 接通极性后动机自然消退。拆除条件见 design.md 拆除账本（fp 上线 + 观察期走私未复发）。

import "regexp"

// 走私形态正则（两类）：
//   - 后台化：nohup 且命令尾部有 & (可选 disown)，或重定向到文件后 2>&1 & 后台化。
//   - 嵌套 tmux：在内层再开 tmux 会话（new-session / new -s），脱离本框架的 session 登记。
var (
	smuggleNohup      = regexp.MustCompile(`\bnohup\b\s`)
	smuggleAmpersand  = regexp.MustCompile(`&\s*(?:disown\b)?`)
	smuggleRedirectBg = regexp.MustCompile(`>\s*\S+\s+2>&1\s*&`)
	smuggleNestedTmux = regexp.MustCompile(`\btmux\s+(?:new-session\b|new\s+-s\b)`)
)

// smuggleCategory 标注命中的走私形态类别（供文案与测试区分）。
type smuggleCategory string

const (
	smuggleNone        smuggleCategory = ""
	smuggleBackground  smuggleCategory = "background"
	smuggleNestedTmuxC smuggleCategory = "nested-tmux"
)

// detectSmuggle 只读命令文本，返回首个命中的走私类别（无命中为 smuggleNone）。
// 不做拦截判定、不解析语义——形态匹配即止（脚手架的教育定位）。
func detectSmuggle(command string) smuggleCategory {
	if smuggleNestedTmux.MatchString(command) {
		return smuggleNestedTmuxC
	}
	// nohup 需与后台化标记 &/disown 配对才算走私（`nohup` 单独出现在 echo 字符串不误报）。
	if smuggleNohup.MatchString(command) && smuggleAmpersand.MatchString(command) {
		return smuggleBackground
	}
	if smuggleRedirectBg.MatchString(command) {
		return smuggleBackground
	}
	return smuggleNone
}

// smuggleHint 是走私形态命中时追加到结果尾部的单行引导（零配置、可忽略）。
// 指向合法形态：mode:resident + 足够大的 ttl；退出码由框架捕获。
func smuggleHint(cat smuggleCategory) string {
	switch cat {
	case smuggleNestedTmuxC:
		return "\n[框架提示] 检测到内嵌 tmux 新会话：该会话不受本框架托管（无结算通知、无 TTL 保护、失败静默）。" +
			"托管长作业请用 mode:resident + 足够大的 ttl，退出码与输出由框架捕获。若这是刻意的一次性实验，可忽略本提示。"
	case smuggleBackground:
		return "\n[框架提示] 检测到后台化走私（nohup/& disown）：该进程不受框架托管（无结算通知、无 TTL 保护、失败静默）。" +
			"托管长作业请用 mode:resident + 足够大的 ttl，退出码与输出由框架捕获。若这是刻意的一次性实验，可忽略本提示。"
	default:
		return ""
	}
}

// smuggleHintFor 组合检测与文案：命中返回引导行，未命中返回空串（结果零改动）。
func smuggleHintFor(command string) string {
	return smuggleHint(detectSmuggle(command))
}
