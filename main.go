// Command agent-to-nvim hands a draft an agent wrote to a human for editing.
//
// It opens the draft in nvim in a new tmux window and blocks until the edit
// finishes, then prints the resulting text on stdout so the calling agent can
// continue with what the human actually wants to send. The exit code says which
// outcome happened — see the constants below.
//
// Stdout carries the draft and nothing else, so a caller can send it as it
// stands. A line the human starts with ">>" is an aside to the agent rather than
// draft text; it is reported on stderr instead of being handed on, under the
// numbered draft lines it sits between so the caller can tell which part of the
// draft it is about. ">>>" is an aside about the draft as a whole, reported
// without any lines because it has no one part to point at.
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
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/draft"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/editwindow"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/notes"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/pending"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/planhook"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/planwindow"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/textdiff"
)

// Exit codes are the agent-facing API: they are how the caller decides what to
// do next without parsing any output.
const (
	exitEdited    = 0  // saved with changes — use the printed text
	exitFailed    = 1  // could not run the edit at all
	exitUnchanged = 10 // saved byte-identical — the draft was approved as-is
	exitAborted   = 20 // discarded via :cq or a killed window — do not proceed
	exitStillOpen = 30 // deadline passed, still being edited — collect it

	// exitAnswered is the plan review's only success. It answers Claude Code in
	// what it writes rather than in how it exits, so the verdict is not here.
	exitAnswered = 0
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// settings are the run's choices, gathered so they can be carried down to the
// wait without growing a parameter list at every step.
type settings struct {
	focus    bool
	deadline time.Duration
	diff     bool
}

func run(args []string) int {
	flags := flag.NewFlagSet("agent-to-nvim", flag.ContinueOnError)
	flags.Usage = usage
	focus := flags.Bool("focus", true, "open the edit window in the foreground")
	deadline := flags.Duration("deadline", 8*time.Minute,
		"how long to wait before handing back a collect id; 0 waits indefinitely")
	diff := flags.Bool("diff", true, "report what changed on stderr")

	command := ""
	if len(args) > 0 && (args[0] == "collect" || args[0] == "plan") {
		command, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}

	set := settings{focus: *focus, deadline: *deadline, diff: *diff}
	if command == "plan" {
		if flags.NArg() != 0 {
			usage()
			return exitFailed
		}
		return reviewPlan(set)
	}

	if flags.NArg() != 1 {
		usage()
		return exitFailed
	}
	if command == "collect" {
		return report(collect(flags.Arg(0), set))
	}
	return report(hand(flags.Arg(0), set))
}

func report(code int, err error) int {
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-to-nvim: %v\n", err)
		return exitFailed
	}
	return code
}

// hand opens the draft for editing and waits for the outcome.
func hand(path string, set settings) (int, error) {
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
		Focus:        set.focus,
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

	return settle(store, edit, handed, session, set)
}

// collect resumes waiting on an edit a previous run handed back.
func collect(id string, set settings) (int, error) {
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
	return settle(store, edit, draft.Reopen(edit.DraftPath, original), session, set)
}

// reviewPlan answers Claude Code's request to leave plan mode: it hands the plan
// to a human and turns what they did with it into the answer.
//
// Waiting is unbounded. The answer has to be written by this process, so unlike a
// draft handover there is nobody to hand a deadline over to — no collect can
// answer on its behalf. Claude Code's own hook timeout bounds it instead, and a
// review it gives up waiting for falls back to Claude Code asking about the plan
// itself.
func reviewPlan(set settings) int {
	event, err := planhook.ReadEvent(os.Stdin)
	if errors.Is(err, planhook.ErrNothingAsked) {
		return exitAnswered
	}
	if err != nil {
		return standDown(err)
	}

	review, err := readPlan(event, set)
	if err != nil {
		return standDown(err)
	}
	if err := review.Answer(event.ToolInput).Send(os.Stdout); err != nil {
		return standDown(err)
	}
	return exitAnswered
}

// standDown reports why a review could not run and leaves the answer unwritten, so
// Claude Code asks about the plan its own way rather than acting on a verdict
// nobody gave.
//
// Exit 1 rather than 2: a blocking error would turn the plan down on the reviewer's
// behalf, and a plan nobody managed to look at has not been turned down.
func standDown(err error) int {
	fmt.Fprintf(os.Stderr, "agent-to-nvim: %v\n", err)
	return exitFailed
}

// readPlan holds the plan open until the human is done with it, and reports what
// they left behind.
//
// The plan is read back whether they saved or not. Quitting with a failure is how a
// plan gets sent back to be revised, and the notes saying why are in the file — so
// unlike a discarded draft, which is simply not used, a discarded plan still has
// something in it to answer.
func readPlan(event planhook.Event, set settings) (planhook.Review, error) {
	store, err := pending.OpenStore()
	if err != nil {
		return planhook.Review{}, err
	}
	path, err := store.HoldPlan(event.PlanFile(), event.Plan())
	if err != nil {
		return planhook.Review{}, err
	}
	keys, err := planwindow.Args(store.PlansDir())
	if err != nil {
		return planhook.Review{}, err
	}

	// A code left behind by a review nobody finished would read as this one being
	// over the instant it opens.
	recorded := path + ".rc"
	_ = os.Remove(recorded)

	session, err := editwindow.Start(editwindow.Request{
		Path:         path,
		EditorArgs:   keys,
		StartDir:     startDir(event),
		Name:         "plan: " + strings.TrimSuffix(filepath.Base(path), ".md"),
		ExitCodeFile: recorded,
		Focus:        set.focus,
	})
	if err != nil {
		return planhook.Review{}, err
	}

	editorCode, err := session.Wait(context.Background())
	if err != nil && !errors.Is(err, editwindow.ErrWindowClosed) {
		return planhook.Review{}, err
	}
	// A killed window is not an approval, but anything saved before it went is
	// still the reviewer's and still worth reading.
	saved := err == nil && editorCode == 0

	reviewed, err := os.ReadFile(path)
	if err != nil {
		return planhook.Review{}, fmt.Errorf("read the reviewed plan: %w", err)
	}
	plan, left := notes.Split(string(reviewed))

	if saved {
		// An approved plan travels back inside the answer, so the copy has nothing
		// left to hold. A plan sent back keeps its copy: the reviewer's own wording
		// is in it and only an account of it goes back.
		_ = os.Remove(path)
	}

	return planhook.Review{
		Approved:  saved,
		Plan:      plan,
		Submitted: event.Plan(),
		Notes:     left,
	}, nil
}

// startDir is where the editor opens, so a file named in the plan can be followed
// straight out of it.
func startDir(event planhook.Event) string {
	if event.Cwd != "" {
		return event.Cwd
	}
	here, err := os.Getwd()
	if err != nil {
		return ""
	}
	return here
}

// settle waits for the edit to finish and turns the result into an exit code.
func settle(
	store *pending.Store,
	edit pending.Edit,
	handed *draft.Draft,
	session *editwindow.Session,
	set settings,
) (int, error) {
	ctx := context.Background()
	if set.deadline > 0 {
		timed, cancel := context.WithTimeout(ctx, set.deadline)
		defer cancel()
		ctx = timed
	}

	editorCode, err := session.Wait(ctx)
	switch {
	case errors.Is(err, editwindow.ErrDeadline):
		fmt.Fprintf(os.Stderr,
			"agent-to-nvim: still being edited after %s; the draft is safe — wait for it with: agent-to-nvim collect %s\n",
			set.deadline, edit.ID)
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

	current, err := handed.Reread()
	if err != nil {
		return 0, err
	}
	// Only once the text is safely in hand. An abandoned or discarded edit leaves
	// the file where it is: a human who saved and then closed the window still has
	// their words on disk, and nothing has printed them anywhere else.
	store.DropScratch(handed.Path())

	back := readBack(string(handed.Original()), current)
	fmt.Print(back.text)
	return announce(os.Stderr, back, set.diff), nil
}

// handback is what came back from the human: the draft text to hand on, the
// notes they left for the agent, and the draft as it went over so the change can
// be reported.
type handback struct {
	text   string
	notes  []notes.Note
	before string
}

// readBack separates notes from draft text on both sides, so the change reported
// is a change to the draft rather than to the asides written about it.
func readBack(original, current string) handback {
	text, left := notes.Split(current)
	before, _ := notes.Split(original)
	return handback{text: text, notes: left, before: before}
}

// announce reports the outcome and returns the exit code for it. Unchanged means
// nothing came back to act on, so a draft left word for word alone but annotated
// counts as edited: the notes are the edit.
func announce(w io.Writer, back handback, showDiff bool) int {
	switch {
	case back.text == back.before && len(back.notes) == 0:
		say(w, "agent-to-nvim: draft saved unchanged\n")
		return exitUnchanged
	case back.text == back.before:
		say(w, "agent-to-nvim: draft text unchanged, with notes\n")
	default:
		say(w, "agent-to-nvim: draft edited\n")
		if showDiff {
			say(w, "%s", textdiff.Unified(back.before, back.text))
		}
	}
	for _, note := range back.notes {
		sayNote(w, note)
	}
	return exitEdited
}

// noteIndent lines a note's later lines up under its first, so a note written
// across several marker lines reads as one instruction rather than as loose text
// beneath it. It is the width of the "note: " label.
const noteIndent = "      "

// sayNote reports one note. A note about part of the draft comes with the lines
// it sits between, numbered as the draft on stdout is: the note's own line is
// gone from that draft, so without them nothing ties the instruction to the text
// it is about. A note about the whole draft has no such lines, and saying so is
// what keeps it from being read as being about wherever it was typed.
func sayNote(w io.Writer, note notes.Note) {
	if note.About == notes.Whole {
		say(w, "note (whole draft): %s\n", continued(note.Said))
		return
	}

	say(w, "note: %s\n", continued(note.Said))
	width := len(strconv.Itoa(max(note.Above.Num, note.Below.Num)))
	for _, line := range []notes.Line{note.Above, note.Below} {
		if line.Num == 0 {
			continue
		}
		say(w, "  %*d  %s\n", width, line.Num, line.Text)
	}
}

// continued indents everything after a note's first line.
func continued(said string) string {
	return strings.ReplaceAll(said, "\n", "\n"+noteIndent)
}

// say reports on the outcome. Reporting is best effort: a run whose stderr has
// gone away has no better outcome left to fall back to, and the text itself has
// already gone to stdout.
func say(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func usage() {
	fmt.Fprint(os.Stderr, `agent-to-nvim — hand a draft to a human, get the edited version back.

usage: agent-to-nvim [flags] <file>
       agent-to-nvim collect [flags] <id>
       agent-to-nvim plan [flags]

Opens <file> in nvim in a new tmux window, blocks until the edit finishes, and
prints the resulting text on stdout. If the deadline passes first, nvim keeps
running and the printed id resumes the same edit.

"plan" is Claude Code's plan-review hook. It reads the request to leave plan mode
on stdin, opens the plan in nvim, and writes the answer on stdout. In that window
<leader>a carries the plan out, <leader>r sends it back to be revised, and
<leader>n and <leader>N open a note about this part of the plan or all of it. The
window says so along the top. Waiting is unbounded there — Claude Code's own hook
timeout is what bounds it.

What the human changed is reported on stderr, marked word by word:

  @@ line 4 @@
    Hand a draft an agent wrote to a human, get the edited version back.
  ~ Launch is on [-Wednesday-]{+Thursday+}, please read the runbook.
  + Ping me if that clashes with anything.

  ~ replaced, marked [-removed-]{+added+}    - removed    + added    (blank) unchanged

A line the human starts with ">>" is a note to the agent, not part of the draft.
It is reported on stderr and kept off stdout, so what stdout carries can be sent
as it stands. The lines either side of where it was written come with it, numbered
as the text on stdout is. A note usually follows the text it is about, so the first
of the two is the likelier referent:

  note: make this shorter
    3  Launch is on Thursday, please read the runbook.
    4  Ping me if that clashes with anything.

">>>" is a note about the whole draft. It has no one part to point at, so it comes
back without any lines:

  note (whole draft): this reads too formally all the way through

A run of note lines is one note. Later lines are indented under the first.

exit codes:
  0   saved with changes
  10  saved unchanged (approved as-is)
  20  discarded (:cq or the window was killed)
  30  deadline passed, still being edited — run the printed collect command
  1   could not run the edit

flags:
  -deadline=8m   how long to wait before handing back a collect id (0 waits forever)
  -diff=false    do not report what changed
  -focus=false   open the edit window in the background

`)
}
