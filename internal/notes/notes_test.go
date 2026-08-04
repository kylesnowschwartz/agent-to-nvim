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
	if want := []string{"make this shorter"}; !reflect.DeepEqual(left, want) {
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
	if want := []string{"check the date"}; !reflect.DeepEqual(left, want) {
		t.Errorf("left = %v, want %v", left, want)
	}
}

func TestSplitKeepsEveryNoteInOrder(t *testing.T) {
	_, left := Split(">> first\nHey team.\n>>second\n>>   third   \n")

	want := []string{"first", "second", "third"}
	if !reflect.DeepEqual(left, want) {
		t.Errorf("left = %v, want %v", left, want)
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
	if want := []string{""}; !reflect.DeepEqual(left, want) {
		t.Errorf("left = %v, want %v", left, want)
	}
}
