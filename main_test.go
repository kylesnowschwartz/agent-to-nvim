package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/draft"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/editwindow"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/pending"
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
	if !strings.Contains(out.String(), "scratch file is removed") {
		t.Errorf("stderr = %q, want it to say the scratch file is gone", out.String())
	}
}

// A file edited in place is still on disk, so it must not be reported as removed.
func TestAnnounceDoesNotReportAnInPlaceFileAsRemoved(t *testing.T) {
	back := readBack("Launch is Thursday.\n", "Launch is Thursday.\n")
	back.textAt = "/home/someone/notes/brief.md"

	var out strings.Builder
	announce(&out, back, true)
	if strings.Contains(out.String(), "removed") {
		t.Errorf("stderr = %q, must not call an in-place file removed", out.String())
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

// The handed-over nvim needs :Sent and :Done bound to the same outcome — save,
// then quit with exitSent — so a user who delivers the text themselves has a way
// to say so that reads as done rather than as a discard.
func TestHandoverArgsBindSentAndDoneToSavingAndExitSent(t *testing.T) {
	args := handoverArgs()

	want := fmt.Sprintf("cquit %d", exitSent)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "command! Sent write | "+want) {
		t.Errorf("handoverArgs() = %v, want :Sent bound to write then cquit %d", args, exitSent)
	}
	if !strings.Contains(joined, "command! Done write | "+want) {
		t.Errorf("handoverArgs() = %v, want :Done bound to write then cquit %d", args, exitSent)
	}
}

// requireTmuxAndFakeEditor points AGENT_TO_NVIM_EDITOR at a script the test
// controls, so settle can be driven through a real editwindow.Session without a
// real nvim. It skips when no tmux server is reachable, matching how the
// editwindow package tests the same thing.
func requireTmuxAndFakeEditor(t *testing.T, exitCode int) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux not on PATH: %v", err)
	}
	if err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Run(); err != nil {
		t.Skipf("no tmux session: %v", err)
	}

	script := filepath.Join(t.TempDir(), "fake-editor")
	if err := os.WriteFile(script, []byte(fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	t.Setenv("AGENT_TO_NVIM_EDITOR", script)
}

// TestSettleAcknowledgesEditorExit40 checks that an editor exit of exitSent is
// treated like the user having sent the text themselves: nothing prints on
// stdout, the pending edit is forgotten, and a scratch draft is dropped —
// without running the diff/notes machinery a real edit would trigger.
func TestSettleAcknowledgesEditorExit40(t *testing.T) {
	requireTmuxAndFakeEditor(t, exitSent)

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	store, err := pending.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	draftPath := filepath.Join(store.DraftsDir(), "sent-draft.md")
	if err := os.WriteFile(draftPath, []byte("hey team\n"), 0o600); err != nil {
		t.Fatalf("write draft: %v", err)
	}
	handed, err := draft.Open(draftPath)
	if err != nil {
		t.Fatalf("draft.Open: %v", err)
	}

	edit, err := store.Begin(handed.Path())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	session, err := editwindow.Start(editwindow.Request{
		Path:         handed.Path(),
		StartDir:     t.TempDir(),
		Name:         "a2n-exit40-test",
		ExitCodeFile: edit.Window.ExitCodeFile,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	edit.Window = session.Handle()
	if err := store.Remember(edit, handed.Original()); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	var stdout strings.Builder
	restoreStdout := redirectStdout(t, &stdout)
	defer restoreStdout()

	code, err := settle(store, edit, handed, session, settings{deadline: 15 * time.Second})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if code != exitSent {
		t.Errorf("settle() = %d, want %d", code, exitSent)
	}
	if stdout.String() != "" {
		t.Errorf("stdout = %q, want nothing printed on exitSent", stdout.String())
	}
	if _, _, err := store.Find(edit.ID); err == nil {
		t.Errorf("Find(%s) succeeded, want the edit forgotten", edit.ID)
	}
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		t.Errorf("scratch draft still exists after exitSent: %v", err)
	}
}

// redirectStdout captures os.Stdout for the duration of the test, so settle's
// print-on-exitSent behavior (or lack of it) can be asserted without depending
// on a terminal.
func redirectStdout(t *testing.T, into *strings.Builder) func() {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = w

	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				into.Write(buf[:n])
			}
			if err != nil {
				close(done)
				return
			}
		}
	}()

	return func() {
		os.Stdout = original
		_ = w.Close()
		<-done
		_ = r.Close()
	}
}
