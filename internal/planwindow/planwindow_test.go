package planwindow

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestArgsWritesTheSetupOutAndPointsTheEditorAtIt(t *testing.T) {
	dir := t.TempDir()

	args, err := Args(dir)
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	if len(args) != 2 || args[0] != "-S" {
		t.Fatalf("args = %v, want the editor told to source one file", args)
	}
	if filepath.Dir(args[1]) != dir {
		t.Errorf("args[1] = %q, want it under %q", args[1], dir)
	}

	written, err := os.ReadFile(args[1])
	if err != nil {
		t.Fatalf("the setup was not written: %v", err)
	}
	if string(written) != setup {
		t.Error("the written setup differs from the embedded one")
	}
}

// Rewriting on every review is what keeps the keys from being a version behind
// the tool that reads what they produce.
func TestArgsRewritesTheSetupOverAStaleOne(t *testing.T) {
	dir := t.TempDir()
	args, err := Args(dir)
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	if err := os.WriteFile(args[1], []byte("-- left over from an older version\n"), 0o600); err != nil {
		t.Fatalf("could not stale the setup: %v", err)
	}

	if _, err := Args(dir); err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	written, err := os.ReadFile(args[1])
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(written) != setup {
		t.Error("a stale setup survived, so the keys could be older than the tool")
	}
}

// The setup is the only place a reader is told what saving and quitting mean, so
// it has to bind every answer and say so where they will see it.
func TestTheSetupOffersEveryAnswerAndBothNoteMarkers(t *testing.T) {
	for _, want := range []string{
		"Approve", "Revise", "AnswerInCLI", "cquit", ">>", ">>>", "winbar", "auto mode",
	} {
		if !strings.Contains(setup, want) {
			t.Errorf("the setup is missing %q", want)
		}
	}
}

// The mark is what widens the session, and the name has to be the one the tool
// looks for — a rename on one side only would silently stop the wider answer
// reaching anybody.
func TestTheSetupMarksTheFileTheToolLooksFor(t *testing.T) {
	if !strings.Contains(setup, `.. ".auto"`) {
		t.Error("the setup does not write the mark beside the plan as <plan>.auto")
	}
}

// The reader says "let the dialog answer" in the code the editor exits with, and
// the tool recognises that answer by the same number. It is written down in two
// languages, so a change to one that misses the other turns a deliberate handover
// into what reads as a crash.
func TestTheEditorHandsOverWithTheCodeTheToolReads(t *testing.T) {
	declared := regexp.MustCompile(`local handed_to_the_dialog = (\d+)`).FindStringSubmatch(setup)
	if declared == nil {
		t.Fatal("the setup no longer declares handed_to_the_dialog, so the tool's exit code has nothing to agree with")
	}

	inTheEditor, err := strconv.Atoi(declared[1])
	if err != nil {
		t.Fatalf("the setup declares a handed_to_the_dialog that is not a number: %v", err)
	}
	if inTheEditor != HandedToTheDialog {
		t.Errorf("the setup exits with %d, the tool reads %d", inTheEditor, HandedToTheDialog)
	}
}

// The top of the window is the only place a reader is told an answer exists, so an
// answer missing from it is one nobody will give.
func TestTheTopOfTheWindowNamesEveryKey(t *testing.T) {
	top := setup[strings.Index(setup, "winbar"):]

	for _, key := range []string{
		"<leader>a", "<leader>A", "<leader>c", "<leader>r", "<leader>n", "<leader>N",
	} {
		if !strings.Contains(top, key) {
			t.Errorf("the top of the window does not say what %s does", key)
		}
	}
}
