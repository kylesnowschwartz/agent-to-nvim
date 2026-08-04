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
	if !strings.Contains(out.String(), "note: check that with ops") {
		t.Errorf("stderr = %q, want the note reported", out.String())
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
	if !strings.Contains(out.String(), "unchanged, with notes") {
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
	if !strings.Contains(out.String(), "note: check with ops") {
		t.Errorf("stderr = %q, want notes reported regardless of -diff", out.String())
	}
}
