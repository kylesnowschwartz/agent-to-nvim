package notereport

import (
	"strings"
	"testing"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/notes"
)

func TestRenderIsEmptyWithoutNotes(t *testing.T) {
	if got := Render(nil); got != "" {
		t.Errorf("Render() = %q, want empty when there are no notes", got)
	}
}

// The case the layout exists for: notes on neighbouring lines. Each draft line is
// one line of the report, so no line is quoted twice and no note claims a stretch
// of the draft the next note is about.
func TestRenderQuotesASharedLineOnce(t *testing.T) {
	got := Render([]notes.Note{
		{
			Said:  "are you sure?",
			Above: notes.Line{Num: 3, Text: "Nobody reports answer quality."},
			Below: notes.Line{Num: 4, Text: "The framing is not new."},
		},
		{
			Said:  "spike this?",
			Above: notes.Line{Num: 4, Text: "The framing is not new."},
			Below: notes.Line{Num: 5, Text: "Nobody built the code links."},
		},
	})

	want := "notes:\n" +
		"   3  Nobody reports answer quality.\n" +
		">> are you sure?\n" +
		"   4  The framing is not new.\n" +
		">> spike this?\n" +
		"   5  Nobody built the code links.\n"
	if got != want {
		t.Errorf("Render() =\n%s\nwant\n%s", got, want)
	}
	if n := strings.Count(got, "The framing is not new."); n != 1 {
		t.Errorf("the shared line appears %d times, want 1", n)
	}
}

// Notes written far apart are about different parts of the draft, so the report
// quotes each stretch separately rather than running two places together.
func TestRenderSeparatesNotesWrittenFarApart(t *testing.T) {
	got := Render([]notes.Note{
		{
			Said:  "near the top",
			Above: notes.Line{Num: 1, Text: "Hey team."},
			Below: notes.Line{Num: 2, Text: "Launch is Thursday."},
		},
		{
			Said:  "near the bottom",
			Above: notes.Line{Num: 9, Text: "Ping me if that clashes."},
			Below: notes.Line{Num: 10, Text: "Thanks."},
		},
	})

	want := "notes:\n" +
		"    1  Hey team.\n" +
		">> near the top\n" +
		"    2  Launch is Thursday.\n" +
		"\n" +
		"    9  Ping me if that clashes.\n" +
		">> near the bottom\n" +
		"   10  Thanks.\n"
	if got != want {
		t.Errorf("Render() =\n%s\nwant\n%s", got, want)
	}
}

// The column is sized for the whole report, not for each stretch of the draft, so
// a two-digit line further down does not shift the numbers above it.
func TestRenderKeepsOneNumberColumnThroughout(t *testing.T) {
	got := Render([]notes.Note{
		{Said: "up here", Above: notes.Line{Num: 1, Text: "First."}},
		{Said: "down there", Above: notes.Line{Num: 12, Text: "Twelfth."}},
	})

	if !strings.Contains(got, "    1  First.") {
		t.Errorf("Render() = %q, want the single digit padded to the widest number", got)
	}
	if !strings.Contains(got, "   12  Twelfth.") {
		t.Errorf("Render() = %q, want the widest number setting the column", got)
	}
}

// A note at the very top has no line above it, so it opens the quote rather than
// being dropped or attached to the line it precedes.
func TestRenderPutsANoteAtTheTopFirst(t *testing.T) {
	got := Render([]notes.Note{{
		Said:  "wrong greeting",
		Below: notes.Line{Num: 1, Text: "Hey team."},
	}})

	want := "notes:\n>> wrong greeting\n   1  Hey team.\n"
	if got != want {
		t.Errorf("Render() =\n%s\nwant\n%s", got, want)
	}
}

// A note at the very bottom has no line below it, and the one line it does have
// still places it.
func TestRenderPutsANoteAtTheBottomLast(t *testing.T) {
	got := Render([]notes.Note{{
		Said:  "cut this",
		Above: notes.Line{Num: 2, Text: "Thanks."},
	}})

	want := "notes:\n   2  Thanks.\n>> cut this\n"
	if got != want {
		t.Errorf("Render() =\n%s\nwant\n%s", got, want)
	}
}

// Two notes typed on consecutive lines are two instructions about one place, so
// they follow the same quoted line instead of quoting it twice.
func TestRenderKeepsNeighbouringNotesTogether(t *testing.T) {
	got := Render([]notes.Note{
		{Said: "first thing", Above: notes.Line{Num: 2, Text: "Launch is Thursday."}},
		{Said: "second thing", Above: notes.Line{Num: 2, Text: "Launch is Thursday."}},
	})

	want := "notes:\n   2  Launch is Thursday.\n>> first thing\n>> second thing\n"
	if got != want {
		t.Errorf("Render() =\n%s\nwant\n%s", got, want)
	}
}

// The instruction only exists in the note, so a long one is reported whole.
func TestRenderReportsALongNoteWhole(t *testing.T) {
	said := strings.Repeat("this is the whole instruction and every word of it counts. ", 6)
	got := Render([]notes.Note{{Said: said, Above: notes.Line{Num: 1, Text: "Hey team."}}})

	if !strings.Contains(got, said) {
		t.Errorf("Render() = %q, want the note reported without being cut", got)
	}
}

// A draft that is nothing but notes has no lines to quote, and the notes are still
// the point of the report.
func TestRenderReportsANoteWithNoDraftAroundIt(t *testing.T) {
	got := Render([]notes.Note{{Said: "start over"}})

	if want := "notes:\n>> start over\n"; got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

// A note about the draft as a whole belongs above the quote. Dropped in among the
// lines it happened to be typed beside, it would read as a note about those lines.
func TestRenderPutsAWholeDraftNoteAboveTheQuote(t *testing.T) {
	got := Render([]notes.Note{
		{
			Said:  "trim this line",
			Above: notes.Line{Num: 2, Text: "Launch is Thursday."},
		},
		{Said: "too formal throughout", About: notes.Whole},
	})

	want := "notes:\n" +
		">>> too formal throughout\n" +
		"\n" +
		"   2  Launch is Thursday.\n" +
		">> trim this line\n"
	if got != want {
		t.Errorf("Render() =\n%s\nwant\n%s", got, want)
	}
}

// Only whole-draft notes means no quote at all, and no blank line waiting for one.
func TestRenderQuotesNothingForWholeDraftNotesAlone(t *testing.T) {
	got := Render([]notes.Note{{Said: "too formal throughout", About: notes.Whole}})

	if want := "notes:\n>>> too formal throughout\n"; got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

// Every line of a note keeps the marker. A later line left in the margin the draft
// is indented past would read as the draft rather than as what was said about it.
func TestRenderMarksEveryLineOfANoteThatRunsOn(t *testing.T) {
	got := Render([]notes.Note{{
		Said:  "check that with ops\n\nthey asked for it twice",
		Above: notes.Line{Num: 1, Text: "Launch is Thursday."},
	}})

	want := "notes:\n" +
		"   1  Launch is Thursday.\n" +
		">> check that with ops\n" +
		">>\n" +
		">> they asked for it twice\n"
	if got != want {
		t.Errorf("Render() =\n%s\nwant\n%s", got, want)
	}
}
