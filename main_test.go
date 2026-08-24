package main

import (
	"strings"
	"testing"
)

func TestAnnounceKeepsNotesOffTheDraft(t *testing.T) {
	back := readBack(
		"Launch is Wednesday.\n",
		"Launch is Thursday.\n>> check that with ops\n",
	)

	if want := "Launch is Thursday.\n"; back.text != want {
		t.Errorf("text = %q, want %q — the note belongs on stderr", back.text, want)
	}

	var out strings.Builder
	if code := announce(&out, back, true); code != exitEdited {
		t.Errorf("announce() = %d, want %d", code, exitEdited)
	}
	if !strings.Contains(out.String(), ">> check that with ops") {
		t.Errorf("stderr = %q, want the note reported", out.String())
	}
}

// A note points at part of the draft, and its own line is not in the text the
// agent acts on, so the report quotes the numbered draft around it. Without that
// the agent gets an instruction and no idea what it is about.
func TestAnnouncePlacesANoteAgainstTheDraft(t *testing.T) {
	back := readBack(
		"Hey team.\n\nLaunch is Wednesday.\nRead the runbook.\n",
		"Hey team.\n\nLaunch is Thursday.\n>> check that with ops\nRead the runbook.\n",
	)

	var out strings.Builder
	announce(&out, back, true)

	want := "notes:\n   3  Launch is Thursday.\n>> check that with ops\n   4  Read the runbook.\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", out.String(), want)
	}
}

// A note about the whole draft has no line to be placed against, so it sits above
// the quoted draft rather than in it. Dropping it among whatever lines it was typed
// beside would send the agent to fix one sentence when the note was about all of
// them.
func TestAnnounceReportsAWholeDraftNoteWithoutLines(t *testing.T) {
	back := readBack(
		"Hey team.\n\nLaunch is Wednesday.\n",
		"Hey team.\n\nLaunch is Thursday.\n>>> this reads too formally throughout\n",
	)

	var out strings.Builder
	announce(&out, back, true)

	report := out.String()[strings.Index(out.String(), "notes:"):]
	if report != "notes:\n>>> this reads too formally throughout\n" {
		t.Errorf("notes = %q, want the note alone with no draft quoted around it", report)
	}
}

// A note written across several marker lines is one instruction, and every line of
// it keeps the marker. A later line left in the margin the draft is indented past
// would read as the draft rather than as what was said about it.
func TestAnnounceMarksEveryLineOfAJoinedNote(t *testing.T) {
	back := readBack(
		"Launch is Wednesday.\n",
		"Launch is Thursday.\n>> check that with ops\n>> they asked twice\n",
	)

	var out strings.Builder
	announce(&out, back, true)

	want := ">> check that with ops\n>> they asked twice\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", out.String(), want)
	}
}

// The count of notes belongs with the outcome. At the head of the notes section a
// number would sit directly above the draft line numbers and read as one of them.
func TestAnnounceCountsTheNotesWithTheOutcome(t *testing.T) {
	back := readBack(
		"One.\nTwo.\nThree.\n",
		"One.\n>> fix this\nTwo.\n>> and this\nThree.\n",
	)

	var out strings.Builder
	announce(&out, back, true)

	if !strings.Contains(out.String(), "draft text unchanged, 2 notes\n") {
		t.Errorf("stderr = %q, want the note count on the outcome line", out.String())
	}
	if strings.Contains(out.String(), "2 notes:") {
		t.Errorf("stderr = %q, want no count above the numbered lines", out.String())
	}
}

// A note is an instruction, so a draft nobody touched otherwise still has
// something in it for the agent to do — reporting that as approved-as-is would
// send the message the note asked to change.
func TestAnnounceCountsANoteOnlyEditAsEdited(t *testing.T) {
	back := readBack("Launch is Thursday.\n", "Launch is Thursday.\n>> make it shorter\n")

	var out strings.Builder
	code := announce(&out, back, true)

	if code != exitEdited {
		t.Errorf("announce() = %d, want %d for a draft carrying a note", code, exitEdited)
	}
	if !strings.Contains(out.String(), "draft text unchanged, 1 note") {
		t.Errorf("stderr = %q, want it to say the text itself did not change", out.String())
	}
}

func TestAnnounceReportsAnUntouchedDraftAsUnchanged(t *testing.T) {
	back := readBack("Launch is Thursday.\n", "Launch is Thursday.\n")

	var out strings.Builder
	if code := announce(&out, back, true); code != exitUnchanged {
		t.Errorf("announce() = %d, want %d", code, exitUnchanged)
	}
	if !strings.Contains(out.String(), "saved unchanged") {
		t.Errorf("stderr = %q, want the unchanged message", out.String())
	}
}

// The change report is about the draft, so a note added beside untouched prose
// must not show up in it as an added line.
func TestAnnounceReportsChangesToTheDraftOnly(t *testing.T) {
	back := readBack(
		"Launch is Wednesday.\nRead the runbook.\n",
		"Launch is Thursday.\n>> check that with ops\nRead the runbook.\n",
	)

	var out strings.Builder
	announce(&out, back, true)

	if strings.Contains(out.String(), "+ >>") {
		t.Errorf("stderr = %q, want the note out of the change report", out.String())
	}
	if !strings.Contains(out.String(), "Thursday") {
		t.Errorf("stderr = %q, want the change to the draft reported", out.String())
	}
}

// A note the human left in the draft when it was handed over is theirs from an
// earlier round; it is not a change they just made.
func TestAnnounceIgnoresANoteThatWasAlreadyThere(t *testing.T) {
	back := readBack(">> left over\nLaunch is Thursday.\n", ">> left over\nLaunch is Thursday.\n")

	var out strings.Builder
	if code := announce(&out, back, true); code != exitEdited {
		t.Errorf("announce() = %d, want %d — the note still needs acting on", code, exitEdited)
	}
	if strings.Contains(out.String(), "@@") {
		t.Errorf("stderr = %q, want no change reported", out.String())
	}
}

func TestAnnounceSuppressesTheReportButNotTheNotes(t *testing.T) {
	back := readBack("Launch is Wednesday.\n", "Launch is Thursday.\n>> check with ops\n")

	var out strings.Builder
	announce(&out, back, false)

	if strings.Contains(out.String(), "@@") {
		t.Errorf("stderr = %q, want no change report under -diff=false", out.String())
	}
	if !strings.Contains(out.String(), ">> check with ops") {
		t.Errorf("stderr = %q, want notes reported regardless of -diff", out.String())
	}
}

func TestAnnounceSaysWhereAnUnprintedEditLives(t *testing.T) {
	back := readBack("hey team\n", "hey team, shipping today\n")
	back.textAt = "/Users/someone/project/README.md"

	var out strings.Builder
	announce(&out, back, true)

	if !strings.Contains(out.String(), "saved in /Users/someone/project/README.md") {
		t.Errorf("report does not say where the text lives:\n%s", out.String())
	}
}
