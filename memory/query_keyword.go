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

// matchesKeyword reports whether a keyword query matches the text
// (case-insensitive).
//
// Term semantics (recall-oriented, fixes the 2026-08-26 wechat-bot incident
// where "最近对话 任务 讨论" returned zero while "任务" alone had hits):
//   - Single term: literal substring match — unchanged legacy semantics.
//   - Multiple terms (split on whitespace/punctuation): ANY term matching
//     counts as a hit. Models routinely send space-separated keyword lists
//     or full natural-language sentences; a literal whole-string match on
//     those silently returns zero.
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
