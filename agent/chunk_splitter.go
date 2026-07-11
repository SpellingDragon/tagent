package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// ChunkSplitter — 语义感知切分器
//
// 将大段工具输出按内容结构切分为多个 chunk，每个 chunk 有独立摘要。
// 这是 SmartCompressor 压缩时的子组件，遵循原型不变量 2：
// Compact 只修改投影（生成新的 EventReference），不修改事件流。
//
// 切分策略（启发式，无 LLM 调用）：
//   - Markdown/HTML: 按标题边界 (#, ##, <h1>, <h2>) 切分
//   - JSON: 按顶层 key 切分
//   - Log: 按时间戳模式或分隔符 (---, ===) 切分
//   - Plain text: 按段落（双换行）切分，超限时在句号/分号处断开
// ---------------------------------------------------------------------------

// ContentType represents the detected content type of a tool result.
type ContentType int

const (
	ContentTypeMarkdown ContentType = iota
	ContentTypeJSON
	ContentTypeLog
	ContentTypePlain
)

func (ct ContentType) String() string {
	switch ct {
	case ContentTypeMarkdown:
		return "markdown"
	case ContentTypeJSON:
		return "json"
	case ContentTypeLog:
		return "log"
	default:
		return "plain"
	}
}

// Chunk represents a single piece of split content with its summary.
type Chunk struct {
	Content string
	Summary string
}

// ChunkSplitter splits large text content into semantic chunks.
type ChunkSplitter struct {
	chunkSize       int
	chunkSummaryLen int
}

// NewChunkSplitter creates a ChunkSplitter with the given parameters.
func NewChunkSplitter(chunkSize, chunkSummaryLen int) *ChunkSplitter {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if chunkSummaryLen <= 0 {
		chunkSummaryLen = 150
	}
	return &ChunkSplitter{
		chunkSize:       chunkSize,
		chunkSummaryLen: chunkSummaryLen,
	}
}

// Split detects content type and splits accordingly.
// Returns nil if content is shorter than chunkSize (no splitting needed).
func (cs *ChunkSplitter) Split(content string) []Chunk {
	if len(content) <= cs.chunkSize {
		return nil
	}

	contentType := detectContentType(content)
	var parts []string

	switch contentType {
	case ContentTypeMarkdown:
		parts = splitMarkdown(content, cs.chunkSize)
	case ContentTypeJSON:
		parts = splitJSON(content, cs.chunkSize)
	case ContentTypeLog:
		parts = splitLog(content, cs.chunkSize)
	default:
		parts = splitPlainText(content, cs.chunkSize)
	}

	if len(parts) <= 1 {
		return nil
	}

	chunks := make([]Chunk, 0, len(parts))
	for _, part := range parts {
		summary := truncate(part, cs.chunkSummaryLen)
		chunks = append(chunks, Chunk{
			Content: part,
			Summary: summary,
		})
	}
	return chunks
}

// detectContentType examines content and returns the detected type.
func detectContentType(content string) ContentType {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ContentTypePlain
	}

	// JSON: starts with { or [
	if (trimmed[0] == '{' || trimmed[0] == '[') && isValidJSON(trimmed) {
		return ContentTypeJSON
	}

	// Markdown: contains heading markers
	if hasMarkdownHeadings(trimmed) {
		return ContentTypeMarkdown
	}

	// Log: contains timestamp patterns or separator lines
	if hasLogPattern(trimmed) {
		return ContentTypeLog
	}

	return ContentTypePlain
}

// isValidJSON checks if content is valid JSON.
func isValidJSON(s string) bool {
	var v any
	return json.Unmarshal([]byte(s), &v) == nil
}

// hasMarkdownHeadings checks if content contains markdown heading markers.
func hasMarkdownHeadings(s string) bool {
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") ||
			strings.HasPrefix(trimmed, "## ") ||
			strings.HasPrefix(trimmed, "### ") {
			return true
		}
	}
	return false
}

// logTimestampPattern matches common log timestamp formats.
var logTimestampPattern = regexp.MustCompile(`^\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}`)

// hasLogPattern checks if content looks like log output.
func hasLogPattern(s string) bool {
	lines := strings.Split(s, "\n")
	timestampCount := 0
	separatorCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if logTimestampPattern.MatchString(trimmed) {
			timestampCount++
		}
		if trimmed == "---" || trimmed == "===" || strings.HasPrefix(trimmed, "=====") {
			separatorCount++
		}
	}
	return timestampCount >= 2 || separatorCount >= 2
}

// splitMarkdown splits content at heading boundaries (#, ##).
func splitMarkdown(content string, chunkSize int) []string {
	lines := strings.Split(content, "\n")
	var chunks []string
	var current strings.Builder

	flushCurrent := func() {
		if current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Start a new chunk at top-level headings
		isHeading := strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ")

		if isHeading && current.Len() > 0 {
			flushCurrent()
		}

		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)

		// If current chunk exceeds chunkSize, flush it
		if current.Len() >= chunkSize {
			flushCurrent()
		}
	}
	flushCurrent()

	return chunks
}

// splitJSON splits a JSON object by top-level keys.
func splitJSON(content string, chunkSize int) []string {
	trimmed := strings.TrimSpace(content)

	// Try to parse as a JSON object
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		// Not an object (maybe an array), fall back to plain text
		return splitPlainText(content, chunkSize)
	}

	var chunks []string
	for key, value := range obj {
		// Re-marshal each key-value pair
		entry := map[string]json.RawMessage{key: value}
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		chunks = append(chunks, string(data))
	}

	return chunks
}

// splitLog splits content at separator lines or timestamp boundaries.
func splitLog(content string, chunkSize int) []string {
	lines := strings.Split(content, "\n")
	var chunks []string
	var current strings.Builder

	flushCurrent := func() {
		if current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Separator lines start a new chunk
		isSeparator := trimmed == "---" || trimmed == "===" ||
			strings.HasPrefix(trimmed, "=====")

		if isSeparator && current.Len() > 0 {
			flushCurrent()
			continue // Skip separator lines themselves
		}

		// Timestamp lines may start a new chunk if current is large enough
		if logTimestampPattern.MatchString(trimmed) && current.Len() > chunkSize/2 {
			flushCurrent()
		}

		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)

		if current.Len() >= chunkSize {
			flushCurrent()
		}
	}
	flushCurrent()

	return chunks
}

// splitPlainText splits content at paragraph boundaries (double newline).
// If a single paragraph exceeds chunkSize, it breaks at sentence boundaries.
func splitPlainText(content string, chunkSize int) []string {
	paragraphs := strings.Split(content, "\n\n")
	var chunks []string
	var current strings.Builder

	flushCurrent := func() {
		if current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
	}

	for _, para := range paragraphs {
		// If paragraph itself exceeds chunkSize, split by sentences
		if len(para) > chunkSize {
			if current.Len() > 0 {
				flushCurrent()
			}
			sentenceChunks := splitBySentences(para, chunkSize)
			chunks = append(chunks, sentenceChunks...)
			continue
		}

		// If adding this paragraph would exceed chunkSize, flush first
		if current.Len()+len(para)+2 > chunkSize && current.Len() > 0 {
			flushCurrent()
		}

		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
	}
	flushCurrent()

	return chunks
}

// splitBySentences splits text at sentence boundaries (。.!?；;).
func splitBySentences(text string, chunkSize int) []string {
	var chunks []string
	var current strings.Builder

	runes := []rune(text)
	i := 0
	for i < len(runes) {
		// Find next sentence boundary
		j := i
		for j < len(runes) {
			c := runes[j]
			if c == '。' || c == '.' || c == '!' || c == '?' || c == '；' || c == ';' {
				j++ // Include the punctuation
				break
			}
			j++
		}

		sentence := string(runes[i:j])

		// If adding this sentence would exceed chunkSize, flush first
		if current.Len()+len(sentence) > chunkSize && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}

		current.WriteString(sentence)
		i = j
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	return chunks
}
