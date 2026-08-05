package planwindow

import (
	"os"
	"path/filepath"
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
// it has to bind both and say so where they will see it.
func TestTheSetupOffersBothVerdictsAndBothNoteMarkers(t *testing.T) {
	for _, want := range []string{"Approve", "Revise", "cquit", ">>", ">>>", "winbar"} {
		if !strings.Contains(setup, want) {
			t.Errorf("the setup is missing %q", want)
		}
	}
}
