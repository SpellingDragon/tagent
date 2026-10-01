package governance

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
	assetRedirect  *regexp.Regexp
	assetVerbWrite *regexp.Regexp
	assetOpenWrite *regexp.Regexp
	assetPrefixes  = cognitiveAssetPrefixes()
)

func init() {
	alt := assetPathAlt()
	if alt == "" {
		return
	}
	assetRedirect = regexp.MustCompile(`>>?\s*["'\\]?(?:\./)?(?:[^\s"'\\|&;]*/)?(?:` + alt + `)`)
	assetVerbWrite = regexp.MustCompile(
		`\b(?:mv|cp|tee|unlink|rmdir)\b[^\n]*(?:` + alt + `)` +
			`|(?:` + alt + `)[^\n]*\b(?:mv|cp|tee|unlink|rmdir)\b` +
			`|\brm\b(?:\s+-\w+)*\s[^\n]*(?:` + alt + `)` +
			`|(?:` + alt + `)[^\n]*\brm\b(?:\s+-\w+)*` +
			`|\bsed\b[^\n]*(?:-i|--in-place)[^\n]*(?:` + alt + `)` +
			`|\bsed\b[^\n]*(?:` + alt + `)[^\n]*(?:-i|--in-place)`)
	assetOpenWrite = regexp.MustCompile(`open\s*\([^\n]*(?:` + alt + `)[^\n]*["'\\][wa]`)
}

// matchCognitiveAssetWrite 判定 exec 命令是否含「写认知资产」意图。
func matchCognitiveAssetWrite(c RiskContext) bool {
	if c.ToolName != "exec" || assetRedirect == nil {
		return false
	}
	lower := strings.ToLower(c.ArgsJSON)
	if !containsAnyAssetPrefix(lower) {
		return false
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
