package agent

import (
	"strings"
	"testing"
)

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    ContentType
	}{
		{"empty", "", ContentTypePlain},
		{"markdown headings", "# Title\n\nSome content\n## Section\nMore", ContentTypeMarkdown},
		{"json object", `{"key1":"value1","key2":"value2"}`, ContentTypeJSON},
		{"json array", `[1,2,3]`, ContentTypeJSON},
		{"log with timestamps", "2026-07-07T10:00:00 INFO start\n2026-07-07T10:00:01 INFO done", ContentTypeLog},
		{"log with separators", "line1\n---\nline2\n---\nline3", ContentTypeLog},
		{"plain text", "Just some plain text without any special structure.", ContentTypePlain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectContentType(tt.content)
			if got != tt.want {
				t.Errorf("detectContentType() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSplitMarkdown(t *testing.T) {
	content := "# Title\n\nIntro paragraph.\n\n## Section 1\n\nContent of section 1.\n\n## Section 2\n\nContent of section 2."
	chunks := splitMarkdown(content, 50)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	// Each chunk should contain a heading
	for i, chunk := range chunks {
		if !strings.Contains(chunk, "#") {
			t.Errorf("chunk %d does not contain a heading: %q", i, chunk)
		}
	}
}

func TestSplitJSON(t *testing.T) {
	content := `{"key1":"value1","key2":"value2","key3":"value3"}`
	chunks := splitJSON(content, 100)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	// Each chunk should be valid JSON with one key
	for i, chunk := range chunks {
		if !strings.Contains(chunk, "key") {
			t.Errorf("chunk %d does not contain a key: %q", i, chunk)
		}
	}
}

func TestSplitLog(t *testing.T) {
	content := "2026-07-07T10:00:00 INFO Starting\n2026-07-07T10:00:01 INFO Processing\n---\n2026-07-07T10:00:02 INFO Done"
	chunks := splitLog(content, 50)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
}

func TestSplitPlainText(t *testing.T) {
	content := strings.Repeat("This is a paragraph. ", 100) + "\n\n" + strings.Repeat("Second paragraph. ", 100)
	chunks := splitPlainText(content, 200)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if len(chunk) > 300 {
			t.Errorf("chunk %d is too large: %d chars", i, len(chunk))
		}
	}
}

func TestSplitBySentences(t *testing.T) {
	content := strings.Repeat("This is a sentence. ", 50)
	chunks := splitBySentences(content, 100)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
}

func TestChunkSplitter_Split_ShortContent(t *testing.T) {
	cs := NewChunkSplitter(1000, 150)
	chunks := cs.Split("short content")
	if chunks != nil {
		t.Errorf("expected nil for short content, got %d chunks", len(chunks))
	}
}

func TestChunkSplitter_Split_LongContent(t *testing.T) {
	cs := NewChunkSplitter(100, 30)

	// Create markdown content longer than chunkSize
	content := "# Section 1\n\n" + strings.Repeat("Content ", 20) + "\n\n# Section 2\n\n" + strings.Repeat("More ", 20)
	chunks := cs.Split(content)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		if chunk.Content == "" {
			t.Errorf("chunk %d has empty content", i)
		}
		if chunk.Summary == "" {
			t.Errorf("chunk %d has empty summary", i)
		}
		if len(chunk.Summary) > 34 { // 30 + "..."
			t.Errorf("chunk %d summary too long: %d", i, len(chunk.Summary))
		}
	}
}

func TestChunkSplitter_Defaults(t *testing.T) {
	cs := NewChunkSplitter(0, 0)
	if cs.chunkSize != 1000 {
		t.Errorf("expected default chunkSize 1000, got %d", cs.chunkSize)
	}
	if cs.chunkSummaryLen != 150 {
		t.Errorf("expected default chunkSummaryLen 150, got %d", cs.chunkSummaryLen)
	}
}
