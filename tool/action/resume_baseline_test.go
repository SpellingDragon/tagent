package action

import "testing"

// TestTrimToLineOffset: the resume output baseline yields this round's
// increment; a shifted scrollback (fewer lines than baseline) degrades to the
// full capture rather than losing output.
func TestTrimToLineOffset(t *testing.T) {
	full := "line1\nline2\nline3\nline4"
	if got := trimToLineOffset(full, 2); got != "line3\nline4" {
		t.Errorf("increment view wrong: %q", got)
	}
	if got := trimToLineOffset(full, 0); got != full {
		t.Errorf("zero baseline must return input, got %q", got)
	}
	if got := trimToLineOffset("only\ntwo", 5); got != "only\ntwo" {
		t.Errorf("shifted scrollback must degrade to full capture, got %q", got)
	}
}
