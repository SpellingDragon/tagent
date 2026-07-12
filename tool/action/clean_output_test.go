package action

import (
	"strings"
	"testing"
)

func TestCleanTmuxOutput_StripTrailingBlankLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strip trailing blanks",
			input:    "line1\nline2\n\n\n\n",
			expected: "line1\nline2",
		},
		{
			name:     "collapse consecutive blanks",
			input:    "line1\n\n\n\nline2\n\n\n",
			expected: "line1\n\nline2",
		},
		{
			name:     "preserve single blank lines",
			input:    "line1\n\nline2\n\nline3",
			expected: "line1\n\nline2\n\nline3",
		},
		{
			name:     "handle pane is dead",
			input:    "output\n\n\n\nPane is dead",
			expected: "output\n\nPane is dead",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "only blank lines",
			input:    "\n\n\n",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanTmuxOutput(tt.input)
			if result != tt.expected {
				t.Errorf("cleanTmuxOutput() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCleanTmuxOutput_RealWorldExample(t *testing.T) {
	// Simulate the actual issue: output with many blank lines from capture-pane -S -1000
	input := "27\n" + strings.Repeat("\n", 50) + "Pane is dead (status 0)"
	result := cleanTmuxOutput(input)

	// Should collapse the 50 blank lines into just 1
	if strings.Count(result, "\n\n") > 1 {
		t.Errorf("cleanTmuxOutput() should collapse blank lines, got: %q", result)
	}

	// Should not have trailing blank lines
	if strings.HasSuffix(result, "\n\n") {
		t.Errorf("cleanTmuxOutput() should strip trailing blank lines, got: %q", result)
	}

	// Should preserve "27" and "Pane is dead"
	if !strings.Contains(result, "27") || !strings.Contains(result, "Pane is dead") {
		t.Errorf("cleanTmuxOutput() should preserve content, got: %q", result)
	}
}
