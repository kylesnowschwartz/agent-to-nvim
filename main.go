// Command agent-to-nvim hands a draft an agent wrote to a human for editing.
//
// It opens the draft in nvim in a new tmux window and blocks until the edit
// finishes, then prints the resulting text on stdout so the calling agent can
// continue with what the human actually wants to send. The exit code says which
// of the three outcomes happened — see the constants below.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/draft"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/editwindow"
)

// Exit codes are the agent-facing API: they are how the caller decides what to
// do next without parsing any output.
const (
	exitEdited    = 0  // saved with changes — use the printed text
	exitFailed    = 1  // could not run the edit at all
	exitUnchanged = 10 // saved byte-identical — the draft was approved as-is
	exitAborted   = 20 // discarded via :cq or a killed window — do not proceed
)

func main() {
	os.Exit(run())
}

func run() int {
	focus := flag.Bool("focus", true, "open the edit window in the foreground")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 1 {
		usage()
		return exitFailed
	}

	code, err := edit(flag.Arg(0), *focus)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-to-nvim: %v\n", err)
		return exitFailed
	}
	return code
}

func edit(path string, focus bool) (int, error) {
	handed, err := draft.Open(path)
	if err != nil {
		return 0, err
	}

	workDir, err := os.Getwd()
	if err != nil {
		return 0, fmt.Errorf("resolve working directory: %w", err)
	}

	session, err := editwindow.Start(editwindow.Request{
		Path:     handed.Path(),
		StartDir: workDir,
		Name:     "edit: " + filepath.Base(handed.Path()),
		Focus:    focus,
	})
	if err != nil {
		return 0, err
	}

	editorCode, err := session.Wait(context.Background())
	switch {
	case errors.Is(err, editwindow.ErrWindowClosed):
		fmt.Fprintln(os.Stderr, "agent-to-nvim: edit window closed without saving")
		return exitAborted, nil
	case err != nil:
		return 0, err
	case editorCode != 0:
		fmt.Fprintln(os.Stderr, "agent-to-nvim: draft discarded in the editor")
		return exitAborted, nil
	}

	text, edited, err := handed.Reread()
	if err != nil {
		return 0, err
	}

	fmt.Print(text)
	if !edited {
		fmt.Fprintln(os.Stderr, "agent-to-nvim: draft saved unchanged")
		return exitUnchanged, nil
	}
	fmt.Fprintln(os.Stderr, "agent-to-nvim: draft edited")
	return exitEdited, nil
}

func usage() {
	fmt.Fprint(os.Stderr, `agent-to-nvim — hand a draft to a human, get the edited version back.

usage: agent-to-nvim [flags] <file>

Opens <file> in nvim in a new tmux window, blocks until the edit finishes, and
prints the resulting text on stdout.

exit codes:
  0   saved with changes
  10  saved unchanged (approved as-is)
  20  discarded (:cq or the window was killed)
  1   could not run the edit

flags:
  -focus=false   open the edit window in the background

`)
}
