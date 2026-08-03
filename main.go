// Command agent-to-nvim hands a draft an agent wrote to a human for editing.
//
// It opens the draft in nvim in a new tmux window and blocks until the edit
// finishes, then prints the resulting text on stdout so the calling agent can
// continue with what the human actually wants to send. The exit code says which
// outcome happened — see the constants below.
//
// Waiting is bounded so the caller exits on its own terms rather than being
// killed by whatever timeout wraps it. When the deadline passes, nvim keeps
// running and `agent-to-nvim collect <id>` picks the same edit back up.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/draft"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/editwindow"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/pending"
)

// Exit codes are the agent-facing API: they are how the caller decides what to
// do next without parsing any output.
const (
	exitEdited    = 0  // saved with changes — use the printed text
	exitFailed    = 1  // could not run the edit at all
	exitUnchanged = 10 // saved byte-identical — the draft was approved as-is
	exitAborted   = 20 // discarded via :cq or a killed window — do not proceed
	exitStillOpen = 30 // deadline passed, still being edited — collect it
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("agent-to-nvim", flag.ContinueOnError)
	flags.Usage = usage
	focus := flags.Bool("focus", true, "open the edit window in the foreground")
	deadline := flags.Duration("deadline", 8*time.Minute,
		"how long to wait before handing back a collect id; 0 waits indefinitely")

	if len(args) > 0 && args[0] == "collect" {
		if err := flags.Parse(args[1:]); err != nil {
			return exitFailed
		}
		if flags.NArg() != 1 {
			usage()
			return exitFailed
		}
		return report(collect(flags.Arg(0), *deadline))
	}

	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	if flags.NArg() != 1 {
		usage()
		return exitFailed
	}
	return report(hand(flags.Arg(0), *focus, *deadline))
}

func report(code int, err error) int {
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-to-nvim: %v\n", err)
		return exitFailed
	}
	return code
}

// hand opens the draft for editing and waits for the outcome.
func hand(path string, focus bool, deadline time.Duration) (int, error) {
	handed, err := draft.Open(path)
	if err != nil {
		return 0, err
	}

	store, err := pending.OpenStore()
	if err != nil {
		return 0, err
	}
	edit, err := store.Begin(handed.Path())
	if err != nil {
		return 0, err
	}

	workDir, err := os.Getwd()
	if err != nil {
		return 0, fmt.Errorf("resolve working directory: %w", err)
	}

	session, err := editwindow.Start(editwindow.Request{
		Path:         handed.Path(),
		StartDir:     workDir,
		Name:         "edit: " + filepath.Base(handed.Path()),
		ExitCodeFile: edit.Window.ExitCodeFile,
		Focus:        focus,
	})
	if err != nil {
		store.Forget(edit.ID)
		return 0, err
	}

	// Record the edit before waiting, so a caller killed mid-wait still leaves
	// something to collect.
	edit.Window = session.Handle()
	if err := store.Remember(edit, handed.Original()); err != nil {
		return 0, err
	}

	return settle(store, edit, handed, session, deadline)
}

// collect resumes waiting on an edit a previous run handed back.
func collect(id string, deadline time.Duration) (int, error) {
	store, err := pending.OpenStore()
	if err != nil {
		return 0, err
	}
	edit, original, err := store.Find(id)
	if err != nil {
		return 0, err
	}

	session, err := editwindow.Reattach(edit.Window)
	if err != nil {
		return 0, err
	}
	return settle(store, edit, draft.Reopen(edit.DraftPath, original), session, deadline)
}

// settle waits for the edit to finish and turns the result into an exit code.
func settle(
	store *pending.Store,
	edit pending.Edit,
	handed *draft.Draft,
	session *editwindow.Session,
	deadline time.Duration,
) (int, error) {
	ctx := context.Background()
	if deadline > 0 {
		timed, cancel := context.WithTimeout(ctx, deadline)
		defer cancel()
		ctx = timed
	}

	editorCode, err := session.Wait(ctx)
	switch {
	case errors.Is(err, editwindow.ErrDeadline):
		fmt.Fprintf(os.Stderr,
			"agent-to-nvim: still being edited after %s; the draft is safe — wait for it with: agent-to-nvim collect %s\n",
			deadline, edit.ID)
		return exitStillOpen, nil
	case errors.Is(err, editwindow.ErrWindowClosed):
		store.Forget(edit.ID)
		fmt.Fprintln(os.Stderr, "agent-to-nvim: edit window closed without saving")
		return exitAborted, nil
	case err != nil:
		return 0, err
	}

	store.Forget(edit.ID)
	if editorCode != 0 {
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
       agent-to-nvim collect [flags] <id>

Opens <file> in nvim in a new tmux window, blocks until the edit finishes, and
prints the resulting text on stdout. If the deadline passes first, nvim keeps
running and the printed id resumes the same edit.

exit codes:
  0   saved with changes
  10  saved unchanged (approved as-is)
  20  discarded (:cq or the window was killed)
  30  deadline passed, still being edited — run the printed collect command
  1   could not run the edit

flags:
  -deadline=8m   how long to wait before handing back a collect id (0 waits forever)
  -focus=false   open the edit window in the background

`)
}
