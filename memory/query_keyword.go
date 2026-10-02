package memory

import "strings"

// keywordSeparator reports whether a rune separates keyword terms in a query.
// Whitespace, CJK punctuation and a few clearly-separating ASCII marks.
// Deliberately excludes '-', '_', '.', '@', '#', '$' so identifiers, paths,
// emails and hex keys stay intact as single terms.
func keywordSeparator(r rune) bool {
	switch r {
	case ' ', '\t', '\r', '\n',
		'，', '。', '：', '；', '、', '！', '？', '·', '…', '—',
		'（', '）', '【', '】', '《', '》', '“', '”', '‘', '’',
		',', ':', ';', '!', '?', '/', '|':
		return true
	}
	return false
}

// matchesKeyword 判断关键词查询是否命中文本（大小写无关）。单条词按整串子串匹配（沿用历史
// 语义）；切出多条词时任一条命中即算命中——模型常把查询写成空格分隔的词列或整句自然语言，
// 对其做整串字面匹配会静默返回零结果。分隔符集合刻意不含 - _ . @ # $，以免拆碎标识符、路径
// 与 hex 票据。空关键词视为全部匹配。语义与事故形状见文档。
//
// 契约: docs/wiki/memory/memory-architecture.md#typed-errors
func matchesKeyword(text, keyword string) bool {
	if keyword == "" {
		return true
	}
	terms := strings.FieldsFunc(keyword, keywordSeparator)
	if len(terms) <= 1 {
		return containsIgnoreCase(text, keyword)
	}
	for _, term := range terms {
		if containsIgnoreCase(text, term) {
			return true
		}
	}
	return false
}
