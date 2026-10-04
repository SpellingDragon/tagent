// 契约: docs/wiki/platform/cognitive-asset-guard.md
package action

import "regexp"

var (
	smuggleNohup = regexp.MustCompile(`\bnohup\b\s`)
	// smuggleAmpCandidate is the word-shape half of the background-& judgement:
	// the & must be followed by whitespace, "disown", or end of line. The
	// positional half (hasBackgroundAmp) additionally rejects "&&" and ">&1"
	// neighbours. One regex cannot decide it: in " && " the first & is preceded
	// by a plain space, and RE2 has no lookahead.
	smuggleAmpCandidate = regexp.MustCompile(`&(?:\s|disown|$)`)
	smuggleRedirectBg   = regexp.MustCompile(`>\s*\S+\s+2>&1\s*&(?:\s|$)`)
	smuggleNestedTmux   = regexp.MustCompile(`\btmux\s+(?:new-session\b|new\s+-s\b)`)
)

// hasBackgroundAmp reports whether the command carries a background-& whose
// neighbours on both sides are not & — an independent job-control ampersand,
// rejecting either leg of a "&&" chain.
// Known miss (accepted boundary): an unspaced real backgrounding form like
// `nohup a &b` has the & followed by a non-space character, so the word-shape
// pass never nominates it — the false-positive-free guarantee for `&&` chains
// and URL query strings is bought with this deliberate narrowing. Backgrounding
// with no redirect and no space is indistinguishable from a URL `&param` by
// pure regex position, so the pair is kept on the safe side.
func hasBackgroundAmp(command string) bool {
	for _, loc := range smuggleAmpCandidate.FindAllStringIndex(command, -1) {
		ampAt := loc[0]
		if ampAt > 0 && command[ampAt-1] == '&' {
			continue
		}
		if ampAt+1 < len(command) && command[ampAt+1] == '&' {
			continue
		}
		return true
	}
	return false
}

// smuggleCategory 标注命中的走私形态类别（供文案与测试区分）。
type smuggleCategory string

const (
	smuggleNone        smuggleCategory = ""
	smuggleBackground  smuggleCategory = "background"
	smuggleNestedTmuxC smuggleCategory = "nested-tmux"
)

// detectSmuggle 只读命令文本，返回首个命中的走私类别（无命中为 smuggleNone）。
// 后台化走私要求 nohup 与 &/disown 配对，单独的 nohup 字样不误报。
func detectSmuggle(command string) smuggleCategory {
	if smuggleNestedTmux.MatchString(command) {
		return smuggleNestedTmuxC
	}
	if smuggleNohup.MatchString(command) && hasBackgroundAmp(command) {
		return smuggleBackground
	}
	if smuggleRedirectBg.MatchString(command) {
		return smuggleBackground
	}
	return smuggleNone
}

// smuggleHint 是走私形态命中时追加到结果尾部的单行引导（零配置、可忽略），
// 指向合法形态：mode:resident + 足够大的 ttl，退出码由框架捕获。
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
