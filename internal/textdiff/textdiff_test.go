package textdiff

import (
	"strings"
	"testing"
)

func TestUnifiedIsEmptyForIdenticalText(t *testing.T) {
	if got := Unified("hey team\n", "hey team\n"); got != "" {
		t.Errorf("Unified() = %q, want empty for unchanged text", got)
	}
}

// The point of the package: a one-word edit must read as one word, not as a
// rewritten line.
func TestUnifiedMarksOnlyTheWordsThatMoved(t *testing.T) {
	got := Unified(
		"Launch is on Wednesday, please review the runbook.\n",
		"Launch is on Thursday, please review the runbook.\n",
	)

	if !strings.Contains(got, "[-Wednesday,-]{+Thursday,+}") {
		t.Errorf("Unified() = %q, want the changed word marked", got)
	}
	if strings.Contains(got, "[-please-]") || strings.Contains(got, "{+runbook+}") {
		t.Errorf("Unified() = %q, want the unchanged words left alone", got)
	}
}

// A word and the space beside it are separate tokens, so an inserted phrase must
// still come back as one pair of markers rather than one per token.
func TestUnifiedJoinsNeighbouringMarkers(t *testing.T) {
	got := Unified(
		"Unit tests cannot verify this.\n",
		"Unit tests cannot really ever verify this.\n",
	)

	if !strings.Contains(got, "{+really ever +}") {
		t.Errorf("Unified() = %q, want the insertion in one pair of markers", got)
	}
	if strings.Contains(got, "+}{+") {
		t.Errorf("Unified() = %q, want adjacent markers joined", got)
	}
}

func TestUnifiedReportsAPureInsertion(t *testing.T) {
	got := Unified("hey team\n", "hey team\nand one more thing\n")

	if !strings.Contains(got, "+ and one more thing") {
		t.Errorf("Unified() = %q, want the added line marked with +", got)
	}
	if strings.Contains(got, "[-") || strings.Contains(got, "{+") {
		t.Errorf("Unified() = %q, want no word markup when nothing was replaced", got)
	}
}

func TestUnifiedReportsAPureDeletion(t *testing.T) {
	got := Unified("hey team\nand one more thing\n", "hey team\n")

	if !strings.Contains(got, "- and one more thing") {
		t.Errorf("Unified() = %q, want the removed line marked with -", got)
	}
}

func TestUnifiedNumbersHunksByTheEditedVersion(t *testing.T) {
	before := "one\ntwo\nthree\nfour\nfive\n"
	after := "one\ntwo\nthree\nFOUR\nfive\n"

	got := Unified(before, after)
	if !strings.Contains(got, "@@ line 4 @@") {
		t.Errorf("Unified() = %q, want a hunk header at line 4", got)
	}
}

func TestUnifiedShowsContextAroundAChange(t *testing.T) {
	before := "one\ntwo\nthree\nfour\nfive\n"
	after := "one\ntwo\nTHREE\nfour\nfive\n"

	got := Unified(before, after)
	if !strings.Contains(got, "  two") || !strings.Contains(got, "  four") {
		t.Errorf("Unified() = %q, want one unchanged line either side", got)
	}
	if strings.Contains(got, "  one") || strings.Contains(got, "  five") {
		t.Errorf("Unified() = %q, want context held to one line", got)
	}
}

// Distant changes are separate hunks; nearby ones share one, so a paragraph
// rewrite does not come back as a header per line.
func TestUnifiedSeparatesDistantChanges(t *testing.T) {
	before := "a\nb\nc\nd\ne\nf\ng\nh\ni\n"
	after := "A\nb\nc\nd\ne\nf\ng\nh\nI\n"

	if got, want := strings.Count(Unified(before, after), "@@ line"), 2; got != want {
		t.Errorf("hunk headers = %d, want %d", got, want)
	}

	adjacent := Unified("a\nb\nc\nd\n", "A\nB\nc\nd\n")
	if got, want := strings.Count(adjacent, "@@ line"), 1; got != want {
		t.Errorf("hunk headers = %d, want %d for adjacent changes", got, want)
	}
}

func TestUnifiedHandlesAnEmptyOriginal(t *testing.T) {
	got := Unified("", "written from scratch\n")
	if !strings.Contains(got, "+ written from scratch") {
		t.Errorf("Unified() = %q, want the whole text reported as added", got)
	}
}

func TestUnifiedTruncatesAWholesaleRewrite(t *testing.T) {
	before := strings.Repeat("old line\n", maxRenderedLines*2)
	after := strings.Repeat("new line\n", maxRenderedLines*2)

	got := Unified(before, after)
	if !strings.Contains(got, "diff truncated") {
		t.Errorf("Unified() did not truncate a rewrite of %d lines", maxRenderedLines*2)
	}
	if lines := strings.Count(got, "\n"); lines > maxRenderedLines+3 {
		t.Errorf("rendered %d lines, want it held near %d", lines, maxRenderedLines)
	}
}

// A paragraph replaced outright shares too few words for markers to read well,
// so it comes back as the plain before and after — with nothing lost.
func TestRewrittenParagraphFallsBackToWholeLines(t *testing.T) {
	got := Unified(
		"An agent prints the draft into the transcript and asks whether it looks right.\n",
		"It's annoying to go back and forth with agents when drafting content.\n",
	)

	if strings.Contains(got, "[-") || strings.Contains(got, "{+") {
		t.Errorf("Unified() = %q, want no word markup for a wholesale rewrite", got)
	}
	if !strings.Contains(got, "- An agent prints") || !strings.Contains(got, "+ It's annoying") {
		t.Errorf("Unified() = %q, want the old and new lines shown whole", got)
	}
	for _, word := range []string{"transcript", "annoying", "drafting", "asks"} {
		if !strings.Contains(got, word) {
			t.Errorf("Unified() = %q, missing %q", got, word)
		}
	}
}

// A light edit stays above the similarity threshold and keeps word marking.
func TestLightEditStaysMarked(t *testing.T) {
	got := Unified(
		"Launch is on Wednesday, please review the runbook before then.\n",
		"Launch is on Thursday, please read the runbook before then.\n",
	)

	if !strings.Contains(got, "~ ") || !strings.Contains(got, "{+Thursday,+}") {
		t.Errorf("Unified() = %q, want the light edit marked word by word", got)
	}
}

func TestTokenizeKeepsSpacingAndMultibyteText(t *testing.T) {
	tokens := tokenize("hey  team — shipping")

	if joined := strings.Join(tokens, ""); joined != "hey  team — shipping" {
		t.Errorf("tokens rejoin to %q, want the input unchanged", joined)
	}
	want := []string{"hey", "  ", "team", " ", "—", " ", "shipping"}
	if len(tokens) != len(want) {
		t.Fatalf("tokens = %q, want %q", tokens, want)
	}
	for i, token := range tokens {
		if token != want[i] {
			t.Errorf("token %d = %q, want %q", i, token, want[i])
		}
	}
}

// Formatters rewrite whitespace on save. None of it is the human's edit, so none
// of it is reported and none of it counts as a change.
func TestWhitespaceOnlyChangesAreNotChanges(t *testing.T) {
	tests := []struct {
		name          string
		before, after string
	}{
		{"trailing spaces trimmed", "Launch is Thursday.   \nRead the runbook. \n", "Launch is Thursday.\nRead the runbook.\n"},
		{"re-indented", "- one\n  - two\n", "- one\n    - two\n"},
		{"blank line added", "one\ntwo\n", "one\n\ntwo\n"},
		{"blank lines collapsed", "one\n\n\n\ntwo\n", "one\n\ntwo\n"},
		{
			"paragraph re-wrapped",
			"The release goes out on Thursday after\nthe migration finishes and the dashboards\nare green.\n",
			"The release goes out on Thursday after the migration finishes\nand the dashboards are green.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !(Comparison{}).Same(tt.before, tt.after) {
				t.Errorf("Same() = false, want true for a whitespace-only change")
			}
			if got := (Comparison{}).Report(tt.before, tt.after); got != "" {
				t.Errorf("Report() = %q, want empty for a whitespace-only change", got)
			}
			if (Comparison{WhitespaceCounts: true}).Same(tt.before, tt.after) {
				t.Errorf("Same() = true with WhitespaceCounts, want the bytes compared")
			}
			if got := (Comparison{WhitespaceCounts: true}).Report(tt.before, tt.after); got == "" {
				t.Errorf("Report() is empty with WhitespaceCounts, want the line diff")
			}
		})
	}
}

func TestWordReports(t *testing.T) {
	tests := []struct {
		name          string
		before, after string
		want          string
	}{
		{
			name:   "one word changed in a re-wrapped paragraph",
			before: "# Launch\n\nThe release goes out on Wednesday after\nthe migration finishes and the dashboards\nare green.\n",
			after:  "# Launch\n\nThe release goes out on Thursday after the migration finishes\nand the dashboards are green.\n",
			want: "@@ line 3 @@\n" +
				"  \n" +
				"~ The release goes out on [-Wednesday-]{+Thursday+} after the migration finishes\n" +
				"  and the dashboards are green.\n",
		},
		{
			name:   "word changed on an indented line",
			before: "- one\n  - two items\n- three\n",
			after:  "- one\n    - four items\n- three\n",
			want:   "@@ line 2 @@\n  - one\n~     - [-two-]{+four+} items\n  - three\n",
		},
		{
			name:   "whole line deleted",
			before: "one\ntwo\ndrop this line\nthree\nfour\n",
			after:  "one\ntwo\nthree\nfour\n",
			want:   "@@ line 3 @@\n  two\n- drop this line\n  three\n",
		},
		{
			name:   "whole line added",
			before: "one\ntwo\nthree\n",
			after:  "one\ntwo\nand a new line\nthree\n",
			want:   "@@ line 3 @@\n  two\n+ and a new line\n  three\n",
		},
		{
			name:   "line deleted at the end",
			before: "one\ntwo\nthe last line\n",
			after:  "one\ntwo\n",
			want:   "@@ line 3 @@\n  two\n- the last line\n",
		},
		{
			name:   "first word changed",
			before: "Wednesday is launch day.\n",
			after:  "Thursday is launch day.\n",
			want:   "@@ line 1 @@\n~ [-Wednesday-]{+Thursday+} is launch day.\n",
		},
		{
			name:   "last word removed",
			before: "Launch is on Thursday morning\n",
			after:  "Launch is on Thursday\n",
			want:   "@@ line 1 @@\n~ Launch is on Thursday [-morning-]\n",
		},
		{
			name:   "line replaced outright",
			before: "intro\nold words here\noutro\n",
			after:  "intro\nnew text entirely\noutro\n",
			want:   "@@ line 2 @@\n  intro\n- old words here\n+ new text entirely\n  outro\n",
		},
		{
			name:   "empty before",
			before: "",
			after:  "written from scratch\n",
			want:   "@@ line 1 @@\n+ written from scratch\n",
		},
		{
			name:   "empty after",
			before: "all of this\ngoes away\n",
			after:  "",
			want:   "@@ line 1 @@\n- all of this\n- goes away\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Comparison{}).Report(tt.before, tt.after); got != tt.want {
				t.Errorf("Report() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// A line rewritten around one coincidental shared word falls below the
// similarity threshold, so it comes back as the old words and the new line.
func TestWordReportFallsBackForAHeavyRewrite(t *testing.T) {
	got := (Comparison{}).Report(
		"An agent prints the draft into the transcript and asks whether it looks right.\n",
		"It's annoying to go back and forth with agents when drafting the content.\n",
	)

	if strings.Contains(got, "[-") || strings.Contains(got, "{+") {
		t.Errorf("Report() = %q, want no word markup for a heavy rewrite", got)
	}
	if !strings.Contains(got, "- An agent prints") || !strings.Contains(got, "+ It's annoying") {
		t.Errorf("Report() = %q, want the old and new lines shown whole", got)
	}
}

func TestWordReportTruncatesAWholesaleRewrite(t *testing.T) {
	before := strings.Repeat("old line\n", maxRenderedLines*2)
	after := strings.Repeat("new text\n", maxRenderedLines*2)

	got := (Comparison{}).Report(before, after)
	if !strings.Contains(got, "diff truncated") {
		t.Errorf("Report() did not truncate a rewrite of %d lines", maxRenderedLines*2)
	}
	if lines := strings.Count(got, "\n"); lines > maxRenderedLines+3 {
		t.Errorf("rendered %d lines, want it held near %d", lines, maxRenderedLines)
	}
}

// Markup formatters break lines between tags that had no whitespace between
// them. Punctuation is its own token, so the words are the same either way.
func TestReformattedMarkupIsNotAChange(t *testing.T) {
	before := `<section id="s1"><h2>Section 1</h2><ul><li>Read the runbook</li><li>Check the dashboards</li></ul></section>` + "\n"
	after := "<section id=\"s1\">\n\t<h2>Section 1</h2>\n\t<ul>\n\t\t<li>Read the runbook</li>\n\t\t<li>Check the dashboards</li>\n\t</ul>\n</section>\n"

	if !(Comparison{}).Same(before, after) {
		t.Errorf("Same() = false, want true for reformatted markup")
	}
	if got := (Comparison{}).Report(before, after); got != "" {
		t.Errorf("Report() = %q, want empty for reformatted markup", got)
	}

	edited := strings.Replace(after, "Check the dashboards", "Check the alerts", 1)
	want := "@@ line 5 @@\n" +
		"  \t\t<li>Read the runbook</li>\n" +
		"~ \t\t<li>Check the [-dashboards-]{+alerts+}</li>\n" +
		"  \t</ul>\n"
	if got := (Comparison{}).Report(before, edited); got != want {
		t.Errorf("Report() =\n%s\nwant\n%s", got, want)
	}
}

// Whitespace inside a word splits it, which is an edit and not formatting.
func TestJoiningTwoWordsIsAChange(t *testing.T) {
	if (Comparison{}).Same("Read it on line.\n", "Read it online.\n") {
		t.Errorf("Same() = true, want joining two words to count as a change")
	}
	want := "@@ line 1 @@\n~ Read it [-on line-]{+online+}.\n"
	if got := (Comparison{}).Report("Read it on line.\n", "Read it online.\n"); got != want {
		t.Errorf("Report() = %q, want %q", got, want)
	}
}

// The marked line reads as the edited text with markers in it: punctuation sits
// where the edit put it, and tags without spaces stay without them.
func TestWordReportKeepsTheEditedSpacing(t *testing.T) {
	got := (Comparison{}).Report(
		"<p>Launch is on Wednesday, ask <b>person 1</b> (or ops).</p>\n",
		"<p>Launch is on Thursday, ask <b>person 2</b> (or ops).</p>\n",
	)
	want := "@@ line 1 @@\n~ <p>Launch is on [-Wednesday-]{+Thursday+}, ask <b>person [-1-]{+2+}</b> (or ops).</p>\n"
	if got != want {
		t.Errorf("Report() = %q, want %q", got, want)
	}
}

func TestTruncatedReportNamesNoPlaceForTheRest(t *testing.T) {
	got := (Comparison{}).Report(strings.Repeat("old line\n", 500), strings.Repeat("new text\n", 500))
	if !strings.Contains(got, TruncatedNote) || strings.Contains(got, "stdout") {
		t.Errorf("Report() ends %q, want the neutral truncation note", got[len(got)-80:])
	}
}
