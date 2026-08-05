package notes

import (
	"reflect"
	"testing"
)

func TestSplitLeavesTextWithoutNotesAlone(t *testing.T) {
	text := "Hey team — launch is Thursday.\nRead the runbook first.\n"

	draft, left := Split(text)
	if draft != text {
		t.Errorf("draft = %q, want the text unchanged", draft)
	}
	if left != nil {
		t.Errorf("left = %v, want no notes", left)
	}
}

func TestSplitTakesNoteLinesOutOfTheDraft(t *testing.T) {
	draft, left := Split("Hey team — launch is Thursday.\n>> make this shorter\nRead the runbook.\n")

	if want := "Hey team — launch is Thursday.\nRead the runbook.\n"; draft != want {
		t.Errorf("draft = %q, want %q", draft, want)
	}
	want := []Note{{
		Said:  "make this shorter",
		Above: Line{Num: 1, Text: "Hey team — launch is Thursday."},
		Below: Line{Num: 2, Text: "Read the runbook."},
	}}
	if !reflect.DeepEqual(left, want) {
		t.Errorf("left = %v, want %v", left, want)
	}
}

// The lines a note is placed against are numbered as the draft that comes back,
// not as the file the human saved — the note lines are not in what the agent
// reads, so counting them would point at the wrong text.
func TestSplitNumbersLinesWithoutCountingNotes(t *testing.T) {
	_, left := Split(">> one\nFirst.\n>> two\nSecond.\nThird.\n>> three\n")

	want := []Note{
		{Said: "one", Below: Line{Num: 1, Text: "First."}},
		{
			Said:  "two",
			Above: Line{Num: 1, Text: "First."},
			Below: Line{Num: 2, Text: "Second."},
		},
		{Said: "three", Above: Line{Num: 3, Text: "Third."}},
	}
	if !reflect.DeepEqual(left, want) {
		t.Errorf("left = %v, want %v", left, want)
	}
}

// A note under a paragraph normally has an empty line above it, and an empty line
// says nothing about what the note is for, so the nearest line with text on it is
// what comes back.
func TestSplitStepsOverBlankLinesToPlaceANote(t *testing.T) {
	_, left := Split("Launch is Thursday.\n\n>> make this shorter\n\nRead the runbook.\n")

	want := []Note{{
		Said:  "make this shorter",
		Above: Line{Num: 1, Text: "Launch is Thursday."},
		Below: Line{Num: 4, Text: "Read the runbook."},
	}}
	if !reflect.DeepEqual(left, want) {
		t.Errorf("left = %v, want %v", left, want)
	}
}

// A note at the end of a draft that never got a trailing newline still comes
// out, and the text before it keeps its own line ending.
func TestSplitTakesANoteOnTheLastLine(t *testing.T) {
	draft, left := Split("Hey team.\n>> check the date")

	if want := "Hey team.\n"; draft != want {
		t.Errorf("draft = %q, want %q", draft, want)
	}
	want := []Note{{Said: "check the date", Above: Line{Num: 1, Text: "Hey team."}}}
	if !reflect.DeepEqual(left, want) {
		t.Errorf("left = %v, want %v", left, want)
	}
}

func TestSplitKeepsEveryNoteInOrder(t *testing.T) {
	_, left := Split(">> first\nHey team.\n>>second\n>>   third   \n")

	var said []string
	for _, note := range left {
		said = append(said, note.Said)
	}
	if want := []string{"first", "second", "third"}; !reflect.DeepEqual(said, want) {
		t.Errorf("said = %v, want %v", said, want)
	}
}

// The marker only counts at the start of a line, so a nested blockquote or a
// shell heredoc inside the draft is text rather than an aside.
func TestSplitKeepsAnIndentedMarkerAsText(t *testing.T) {
	text := "Quoting them:\n    >> not a note\n"

	draft, left := Split(text)
	if draft != text {
		t.Errorf("draft = %q, want the indented marker kept", draft)
	}
	if left != nil {
		t.Errorf("left = %v, want no notes", left)
	}
}

func TestSplitKeepsAMarkerFoundMidLine(t *testing.T) {
	text := "Run cat >> log.txt to append.\n"

	draft, left := Split(text)
	if draft != text {
		t.Errorf("draft = %q, want the mid-line marker kept", draft)
	}
	if left != nil {
		t.Errorf("left = %v, want no notes", left)
	}
}

func TestSplitReportsAnEmptyNote(t *testing.T) {
	draft, left := Split("Hey team.\n>>\n")

	if want := "Hey team.\n"; draft != want {
		t.Errorf("draft = %q, want %q", draft, want)
	}
	want := []Note{{Said: "", Above: Line{Num: 1, Text: "Hey team."}}}
	if !reflect.DeepEqual(left, want) {
		t.Errorf("left = %v, want %v", left, want)
	}
}
