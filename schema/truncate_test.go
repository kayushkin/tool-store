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

// TruncateList used to compute the total it printed from len(items), which is
// the number of items the caller HANDED IT and not the number that exist. A
// caller that stopped early hands over a slice indistinguishable from a
// complete one, so the footer stated the caller's cap as the size of the
// collection for every list large enough to reach it.
func TestATruncatedListNeverStatesACapAsATotal(t *testing.T) {
	items := make([]string, 1000)
	for i := range items {
		items[i] = "entry"
	}

	stopped := TruncateList(items, 50, PopulationStoppedEarly(1000))
	if strings.Contains(stopped, "Total:") {
		t.Errorf("a list the caller gave up enumerating still reports a total:\n%s", stopped)
	}
	if !strings.Contains(stopped, "1000") || !strings.Contains(stopped, "more than this") {
		t.Errorf("the footer does not say the collection is larger than the 1000 items seen:\n%s", stopped)
	}

	// The same slice, from a caller that really did count it all, must still
	// report the total. Without this the fix could be "never say Total".
	complete := TruncateList(items, 50, CompletePopulation(1000))
	if !strings.Contains(complete, "Total: 1000 items") {
		t.Errorf("a fully enumerated 1000-item list did not report its total:\n%s", complete)
	}
	if strings.Contains(complete, "more than this") {
		t.Errorf("a fully enumerated list was described as incomplete:\n%s", complete)
	}
}

// The disclosure has to be driven by the population and not by whether the
// display cut fired. A caller that stopped at 40 items and shows all 40 is
// still showing a partial answer, and the old shape — one early return on
// len(items) <= maxItems — could not say so.
func TestAnIncompleteScanIsDisclosedEvenWhenEveryItemFits(t *testing.T) {
	items := make([]string, 40)
	for i := range items {
		items[i] = "entry"
	}

	out := TruncateList(items, 50, PopulationStoppedEarly(40))
	if !strings.Contains(out, "stopped after 40 items") {
		t.Errorf("40 items shown out of a scan that gave up at 40 reads as a complete listing:\n%s", out)
	}
	// Nothing was cut from what it was given, so it must not claim otherwise.
	if strings.Contains(out, "more items...") {
		t.Errorf("a list with nothing omitted reports omitted items:\n%s", out)
	}
}

// Control: the complete-and-fits case is the overwhelmingly common one and its
// output is unchanged — a bare newline-joined list with no footer of any kind.
func TestACompleteListThatFitsIsReturnedUntouched(t *testing.T) {
	items := []string{"a.go", "b.go", "c.go"}

	out := TruncateList(items, 50, CompletePopulation(len(items)))
	if out != "a.go\nb.go\nc.go" {
		t.Errorf("a short complete list was decorated:\n%q", out)
	}
}

// The footer names a remediation, and it named one the tool does not have:
// list_files takes a path and a recursive flag and no pattern of any kind.
func TestTheFooterAdviceNamesSomethingTheToolActuallyOffers(t *testing.T) {
	items := make([]string, 100)
	for i := range items {
		items[i] = "entry"
	}

	for _, out := range []string{
		TruncateList(items, 50, CompletePopulation(100)),
		TruncateList(items, 50, PopulationStoppedEarly(100)),
	} {
		if strings.Contains(out, "pattern") {
			t.Errorf("the footer tells the reader to filter by pattern, an argument list_files does not accept:\n%s", out)
		}
		if !strings.Contains(out, "subdirectory") {
			t.Errorf("the footer offers no way out of a truncated listing:\n%s", out)
		}
	}
}
