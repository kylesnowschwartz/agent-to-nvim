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
