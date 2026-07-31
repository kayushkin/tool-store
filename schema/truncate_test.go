package schema

import (
	"strings"
	"testing"
)

func TestFileLinesDropsTheSplitFragment(t *testing.T) {
	cases := []struct {
		content string
		want    int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"\n", 1},
		{"a\nb\nc", 3},
		{"a\nb\nc\n", 3},
		{"a\n\nc\n", 3},
	}
	for _, tc := range cases {
		if got := len(FileLines(tc.content)); got != tc.want {
			t.Errorf("FileLines(%q) = %d lines, want %d", tc.content, got, tc.want)
		}
	}
}

func TestTruncateFileReadReportsWhatItKept(t *testing.T) {
	short := strings.Repeat("x\n", 100)
	kept, cut := TruncateFileRead(short)
	if kept != short {
		t.Errorf("a short file was altered")
	}
	if cut.Truncated() {
		t.Errorf("a 100-line file reported as truncated: %+v", cut)
	}
	if cut.TotalLines != 100 {
		t.Errorf("TotalLines = %d, want 100", cut.TotalLines)
	}

	var b strings.Builder
	for i := 1; i <= 3000; i++ {
		b.WriteString("line\n")
	}
	kept, cut = TruncateFileRead(b.String())
	if !cut.Truncated() {
		t.Fatalf("a 3000-line file reported as whole: %+v", cut)
	}
	if cut.TotalLines != 3000 || cut.KeptFirst != 500 || cut.KeptLast != 50 {
		t.Errorf("cut = %+v, want {3000 500 50}", cut)
	}
	// The report has to match the bytes: 550 content lines survive, and the
	// last one of them must be a real line rather than the empty fragment a
	// trailing newline leaves behind.
	if strings.HasSuffix(kept, "\n") {
		t.Errorf("kept window ends in the empty fragment, so the last kept line is not content")
	}
}
