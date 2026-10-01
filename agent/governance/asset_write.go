package governance

// 认知资产写审批规则（cognitive-asset-guard D2·脚手架）：
// 默认清单（与 evolution.DefaultProtectedPaths 同源）内的路径被写形态命中时判
// critical，走既有 ApprovalManager 异步批准链（零 gate 改动）。
//
// 脚手架性质：文本匹配非密封边界——变量拼路径/base64/编码均可绕过。规则在追赶
// 症状，其终态是权限域分离（资产目录对 exec 物理只读）。拆除条件见 design.md 拆除账本。
// 真正的不变量兜底由 D1 漂移审计（hash）承担，不依赖本规则。
//
// 判定原则：路径 + 写意图「两条件与」。为避免误伤只读（`cat asset > /tmp/out` 是读
// 资产、写他处），重定向类要求资产路径紧跟 `>`/`>>` 之后（写目标）；命令动词类
// （cp/mv/tee/rm/sed -i/open(w)）要求动词与资产路径同时出现。保守取向——宁可多判
// 待批（人在环）不可漏判。

import (
	"regexp"
	"strings"

	"github.com/SpellingDragon/tagent/evolution"
)

// cognitiveAssetPrefixes 从同源清单派生资产目录前缀（`/**` 目录模式转为带尾斜杠的
// 前缀；精确文件模式原样保留）。
func cognitiveAssetPrefixes() []string {
	var out []string
	for _, pat := range evolution.DefaultProtectedPaths {
		if p := strings.TrimSuffix(pat, "/**"); p != pat {
			if p != "" {
				out = append(out, p+"/")
			}
			continue
		}
		if !strings.ContainsAny(pat, "*?") && pat != "" {
			out = append(out, pat)
		}
	}
	return out
}

// assetPathAlt 是资产路径前缀的正则交替（转义），如 `resources/prompts/|skills/|scripts/`。
func assetPathAlt() string {
	alts := make([]string, 0, 4)
	for _, p := range cognitiveAssetPrefixes() {
		alts = append(alts, regexp.QuoteMeta(strings.ToLower(p)))
	}
	return strings.Join(alts, "|")
}

var (
	// 三张写形态正则：RE2 语法（无环视）；空清单时留 nil，判定短路为不命中。
	assetRedirect  *regexp.Regexp // 重定向写入资产：> / >> 紧跟资产路径
	assetVerbWrite *regexp.Regexp // 命令动词作用于资产：cp/mv/tee/rm/unlink/sed -i
	assetOpenWrite *regexp.Regexp // python 写模式：open(path,'w')
	assetPrefixes  = cognitiveAssetPrefixes()
)

func init() {
	alt := assetPathAlt()
	if alt == "" {
		return // 空清单：防线规则无对象（对齐终态拆除语义）
	}
	// `>`/`>>` 后（可含空格/引号/相对 ./ 前缀/中间目录）紧跟资产路径。
	assetRedirect = regexp.MustCompile(`>>?\s*["'\\]?(?:\./)?(?:[^\s"'\\|&;]*/)?(?:` + alt + `)`)
	// 写动词与资产路径同现（任一顺序）。
	assetVerbWrite = regexp.MustCompile(
		`\b(?:mv|cp|tee|unlink|rmdir)\b[^\n]*(?:` + alt + `)` +
			`|(?:` + alt + `)[^\n]*\b(?:mv|cp|tee|unlink|rmdir)\b` +
			`|\brm\b(?:\s+-\w+)*\s[^\n]*(?:` + alt + `)` +
			`|(?:` + alt + `)[^\n]*\brm\b(?:\s+-\w+)*` +
			`|\bsed\b[^\n]*(?:-i|--in-place)[^\n]*(?:` + alt + `)` +
			`|\bsed\b[^\n]*(?:` + alt + `)[^\n]*(?:-i|--in-place)`)
	// open( 内资产路径 + 写模式 w/a（JSON 转义引号 \\\" 亦覆盖）。
	assetOpenWrite = regexp.MustCompile(`open\s*\([^\n]*(?:` + alt + `)[^\n]*["'\\][wa]`)
}

// matchCognitiveAssetWrite 判定 exec 命令是否含「写认知资产」意图。
func matchCognitiveAssetWrite(c RiskContext) bool {
	if c.ToolName != "exec" || assetRedirect == nil {
		return false
	}
	lower := strings.ToLower(c.ArgsJSON)
	if !containsAnyAssetPrefix(lower) {
		return false // 无资产路径线索，快速否定
	}
	return assetRedirect.MatchString(lower) ||
		assetVerbWrite.MatchString(lower) ||
		assetOpenWrite.MatchString(lower)
}

// containsAnyAssetPrefix 判定文本是否引用任一资产目录（去尾斜杠匹配）。
func containsAnyAssetPrefix(lower string) bool {
	for _, p := range assetPrefixes {
		if strings.Contains(lower, strings.ToLower(strings.TrimSuffix(p, "/"))) {
			return true
		}
	}
	return false
}
