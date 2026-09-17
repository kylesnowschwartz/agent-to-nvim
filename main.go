// Command agent-to-nvim hands a draft an agent wrote to a human for editing.
//
// It opens the draft in nvim in a new tmux window and blocks until the edit
// finishes, then prints the resulting text on stdout so the calling agent can
// continue with what the human actually wants to send. The exit code says which
// outcome happened — see the constants below.
//
// Stdout carries the draft and nothing else, so a caller can send it as it
// stands. A line the human starts with ">>" is an aside to the agent rather than
// draft text; it is reported on stderr instead of being handed on, sitting in a
// quote of the numbered draft where it was written, so the caller can tell which
// part of the draft it is about. ">>>" is an aside about the draft as a whole,
// reported above the quote because it has no one part to point at.
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
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/draft"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/editwindow"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/notereport"
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
	exitSent      = 40 // the user sent the text themselves — acknowledge and stop

	// exitAnswered is the plan review's only success. It answers Claude Code in
	// what it writes rather than in how it exits, so the verdict is not here.
	exitAnswered = 0
)

// version names the release this binary was built from. A release build sets it
// with -ldflags; a plain `go build` reports dev.
var version = "dev"

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
	deadline := flags.Duration("deadline", 10*time.Minute,
		"how long to wait before handing back a collect id; 0 waits indefinitely")
	diff := flags.Bool("diff", true, "report what changed on stderr")
	showVersion := flags.Bool("version", false, "print the version and exit")

	command := ""
	if len(args) > 0 && (args[0] == "collect" || args[0] == "plan" || args[0] == "plan-answered") {
		command, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	if *showVersion {
		fmt.Println(version)
		return 0
	}

	set := settings{focus: *focus, deadline: *deadline, diff: *diff}
	if command == "plan" {
		if flags.NArg() != 0 {
			usage()
			return exitFailed
		}
		return reviewPlan(set)
	}
	if command == "plan-answered" {
		if flags.NArg() != 0 {
			usage()
			return exitFailed
		}
		return notePlanAnswered()
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
		EditorArgs:   handoverArgs(),
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

// handoverArgs binds :Sent and its alias :Done inside the handed-over nvim to
// saving the draft and quitting with exitSent, so a user who delivers the text
// themselves — pasting it into Slack, say — has a way to say so that the
// waiting agent reads as done rather than as a discard.
func handoverArgs() []string {
	sent := fmt.Sprintf("command! Sent write | cquit %d", exitSent)
	done := fmt.Sprintf("command! Done write | cquit %d", exitSent)
	return []string{"-c", sent, "-c", done}
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
// The wait has no deadline of its own. The answer has to be written by this
// process, so unlike a draft handover there is nobody to hand a deadline over to
// — no collect can answer on its behalf. It ends in one of three ways: the human
// closes the window, Claude Code's own dialog answers first (see
// errAnsweredInClaudeCode), or Claude Code's hook timeout gives up on it and
// Claude Code asks about the plan itself.
func reviewPlan(set settings) int {
	event, err := planhook.ReadEvent(os.Stdin)
	if errors.Is(err, planhook.ErrNothingAsked) {
		return exitAnswered
	}
	if err != nil {
		return standDown(err)
	}

	review, err := readPlan(event, set)
	if errors.Is(err, errAnsweredInClaudeCode) {
		fmt.Fprintln(os.Stderr, "agent-to-nvim: the plan was answered in Claude Code; closed the review window")
		return exitAnswered
	}
	if err != nil {
		return standDown(err)
	}
	if err := review.Answer(event.ToolInput).Send(os.Stdout); err != nil {
		return standDown(err)
	}
	return exitAnswered
}

// errAnsweredInClaudeCode reports that Claude Code's own dialog answered the plan
// while it was open here, so this review has no verdict to give and its window
// has been closed.
//
// Claude Code asks about a plan in its dialog and through this hook at once. An
// answer given in the dialog reaches this process two ways: a refusal ends the
// hook with SIGTERM, and an approval fires a second hook, `plan-answered`, which
// leaves a mark in the store for the session. Either way the plan is settled and
// the window is only in the way.
var errAnsweredInClaudeCode = errors.New("the plan was answered in Claude Code's own dialog")

// notePlanAnswered is the `plan-answered` hook: it marks the session's plan
// request as answered so the review holding that plan closes its window. It
// answers nothing itself, so stdout stays empty.
func notePlanAnswered() int {
	sessionID, err := planhook.ReadAnswered(os.Stdin)
	if err != nil {
		return standDown(err)
	}
	if sessionID == "" {
		return exitAnswered
	}
	store, err := pending.OpenStore()
	if err != nil {
		return standDown(err)
	}
	if err := store.MarkPlanAnswered(sessionID); err != nil {
		return standDown(err)
	}
	return exitAnswered
}

// errHandedToTheDialog reports that the reviewer wants Claude Code's own approval
// dialog to answer this plan, so this review has no verdict to give.
var errHandedToTheDialog = errors.New("the reviewer left this plan to Claude Code's own dialog")

// standDown leaves the answer unwritten, so Claude Code asks about the plan its own
// way rather than acting on a verdict nobody gave. It covers both a review that
// could not run and one the reviewer handed over on purpose, because either way
// there is no verdict and the reason belongs on stderr.
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

	// Anything a review nobody finished left behind has to go before this one
	// opens: a stale exit code reads as this review being over the instant it
	// starts, and a stale mark widens what the session may do off somebody else's
	// answer.
	recorded, askedForAuto := path+".rc", path+".auto"
	_ = os.Remove(recorded)
	_ = os.Remove(askedForAuto)
	store.ClearPlanAnswered(event.SessionID)

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

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	go endWhenAnsweredInClaudeCode(ctx, cancel, store, event.SessionID)

	editorCode, err := session.Wait(ctx)
	if errors.Is(err, editwindow.ErrDeadline) && errors.Is(context.Cause(ctx), errAnsweredInClaudeCode) {
		session.Close()
		_ = os.Remove(askedForAuto)
		_ = os.Remove(path)
		store.ClearPlanAnswered(event.SessionID)
		return planhook.Review{}, errAnsweredInClaudeCode
	}
	if err != nil && !errors.Is(err, editwindow.ErrWindowClosed) {
		return planhook.Review{}, err
	}
	// Handing the plan to Claude Code's dialog ends the review without a verdict.
	// Nothing reads the copy after that, and the plan travels in the dialog's own
	// answer, so the copy goes with the window.
	if err == nil && editorCode == planwindow.HandedToTheDialog {
		_ = os.Remove(askedForAuto)
		_ = os.Remove(path)
		return planhook.Review{}, errHandedToTheDialog
	}

	// A killed window is not an approval, but anything saved before it went is
	// still the reviewer's and still worth reading.
	saved := err == nil && editorCode == 0

	reviewed, err := os.ReadFile(path)
	if err != nil {
		return planhook.Review{}, fmt.Errorf("read the reviewed plan: %w", err)
	}
	plan, left := notes.Split(string(reviewed))

	// The editor writes this down only when the reviewer said so outright, so its
	// absence is the narrower answer — which is what an editor that died before
	// saying anything leaves behind.
	_, marked := os.Stat(askedForAuto)
	auto := saved && marked == nil
	_ = os.Remove(askedForAuto)

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
		Auto:      auto,
	}, nil
}

// answeredPollInterval is how often the store is checked for the mark that
// `plan-answered` leaves.
const answeredPollInterval = 250 * time.Millisecond

// endWhenAnsweredInClaudeCode cancels the wait, with errAnsweredInClaudeCode as
// the cause, once Claude Code's own dialog has answered the plan: on the SIGTERM
// a refusal sends this hook, or on the mark an approval's `plan-answered` hook
// leaves for the session. It returns when the wait ends for any other reason.
func endWhenAnsweredInClaudeCode(ctx context.Context, cancel context.CancelCauseFunc, store *pending.Store, sessionID string) {
	ended := make(chan os.Signal, 1)
	signal.Notify(ended, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(ended)

	var marked <-chan time.Time
	if sessionID != "" {
		tick := time.NewTicker(answeredPollInterval)
		defer tick.Stop()
		marked = tick.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ended:
			cancel(errAnsweredInClaudeCode)
			return
		case <-marked:
			if store.PlanAnswered(sessionID) {
				cancel(errAnsweredInClaudeCode)
				return
			}
		}
	}
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

// resumeCommand is the command line that picks a still-open edit back up. The
// Claude Code plugin keeps the binary in its own directory rather than on PATH,
// so the caller is handed the path of the binary it is already running.
func resumeCommand(id string) string {
	self, err := os.Executable()
	if err != nil {
		self = "agent-to-nvim"
	}
	return fmt.Sprintf("%s collect %s", self, id)
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
			"agent-to-nvim: still being edited after %s; the draft is safe — tell the user to say when they're done, then run: %s\n",
			set.deadline, resumeCommand(edit.ID))
		return exitStillOpen, nil
	case errors.Is(err, editwindow.ErrWindowClosed):
		store.Forget(edit.ID)
		fmt.Fprintln(os.Stderr, "agent-to-nvim: edit window closed without saving")
		return exitAborted, nil
	case err != nil:
		return 0, err
	}

	store.Forget(edit.ID)
	if editorCode == exitSent {
		fmt.Fprintln(os.Stderr, "agent-to-nvim: the user sent the draft themselves; nothing left to do")
		store.DropScratch(handed.Path())
		return exitSent, nil
	}
	if editorCode != 0 {
		fmt.Fprintln(os.Stderr, "agent-to-nvim: draft discarded in the editor")
		return exitAborted, nil
	}

	current, err := handed.Reread()
	if err != nil {
		return 0, err
	}

	back := readBack(string(handed.Original()), current)
	if len(back.notes) > 0 {
		// The notes were instructions to the agent, not draft text: strip them
		// out of the file itself so a real file edited in place never keeps
		// them, and keep a durable copy so the report surviving only on stderr
		// is not the last of them.
		if err := handed.WriteBack(back.text); err != nil {
			return 0, err
		}
		kept, err := store.KeepNotes(edit.ID, notereport.Render(back.notes))
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-to-nvim: %v\n", err)
		} else {
			back.keptAt = kept
		}
	}
	// A scratch draft's text exists nowhere else once the file is dropped, so it
	// goes to stdout whole. A file edited in place already holds the text — the
	// diff and notes say everything that happened, and printing a large file
	// again only overflows the caller's output cap.
	if store.IsScratch(handed.Path()) {
		fmt.Print(back.text)
	} else {
		back.textAt = handed.Path()
	}
	// Only once the text is safely in hand. An abandoned or discarded edit leaves
	// the file where it is: a human who saved and then closed the window still has
	// their words on disk, and nothing has printed them anywhere else.
	store.DropScratch(handed.Path())

	return announce(os.Stderr, back, set.diff), nil
}

// handback is what came back from the human: the draft text to hand on, the
// notes they left for the agent, and the draft as it went over so the change can
// be reported.
type handback struct {
	text   string
	notes  []notes.Note
	before string
	keptAt string // where the durable copy of the notes lives, "" when none
	textAt string // where the edited text lives when not printed, "" when printed
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
	if back.text == back.before && len(back.notes) == 0 {
		say(w, "agent-to-nvim: draft saved unchanged\n")
		sayScratchRemoved(w, back)
		return exitUnchanged
	}

	say(w, "agent-to-nvim: %s\n", outcome(back))
	sayScratchRemoved(w, back)
	if back.textAt != "" {
		say(w, "the edited text is saved in %s and is not printed — the report below is the whole change, so there is no need to read the file back\n", back.textAt)
	}
	if back.text != back.before && showDiff {
		say(w, "%s", textdiff.Unified(back.before, back.text))
	}
	if len(back.notes) > 0 {
		say(w, "\n%s", notereport.Render(back.notes))
		if back.keptAt != "" {
			say(w, "\na copy of the notes is kept at %s\n", back.keptAt)
		}
	}
	return exitEdited
}

// sayScratchRemoved warns that a scratch draft no longer exists on disk. The
// caller still holds the path it wrote, and a path that looks valid invites reuse:
// handing it to another agent as a brief, or reading it back. The text on stdout
// is the only copy.
func sayScratchRemoved(w io.Writer, back handback) {
	if back.textAt != "" {
		return
	}
	say(w, "the scratch file is removed; the text on stdout is the only copy\n")
}

// outcome says what came back, counting the notes. The count sits with the outcome
// rather than at the head of the notes section, where a number would read as one
// of the draft line numbers under it.
func outcome(back handback) string {
	what := "draft edited"
	if back.text == back.before {
		what = "draft text unchanged"
	}
	switch len(back.notes) {
	case 0:
		return what
	case 1:
		return what + ", 1 note"
	default:
		return fmt.Sprintf("%s, %d notes", what, len(back.notes))
	}
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
       agent-to-nvim plan-answered

Opens <file> in nvim in a new tmux window, blocks until the edit finishes, and
prints the resulting text on stdout. If the deadline passes first, nvim keeps
running and the printed id resumes the same edit. Type :Sent in nvim once you
have sent the text yourself — :Done works the same way — and there is nothing
left for the agent to do with it.

"plan" is Claude Code's plan-review hook. It reads the request to leave plan mode
on stdin, opens the plan in nvim, and writes the answer on stdout. In that window
<leader>a carries the plan out, <leader>A carries it out in auto mode so no step waits
to be confirmed, <leader>c leaves the answer to Claude Code's own dialog and drops
any edits made here, <leader>r sends it back to be revised, and <leader>n and
<leader>N open a note about this part of the plan or all of it. The window says so
along the top.
Waiting has no deadline of its own there — Claude Code's hook timeout bounds it.

"plan-answered" is the companion hook, run once Claude Code has acted on a plan.
It tells an open review that the plan was answered in Claude Code's own dialog,
so the window closes on its own. It reads the event on stdin and prints nothing.

What the human changed is reported on stderr, marked word by word:

  @@ line 4 @@
    Hand a draft an agent wrote to a human, get the edited version back.
  ~ Launch is on [-Wednesday-]{+Thursday+}, please read the runbook.
  + Ping me if that clashes with anything.

  ~ replaced, marked [-removed-]{+added+}    - removed    + added    (blank) unchanged

A line the human starts with ">>" is a note to the agent, not part of the draft.
It is reported on stderr and kept off stdout, so what stdout carries can be sent
as it stands. Each note is reported inside a quote of the draft around it, marked
">>" in the margin and sitting where it was written. The quoted lines are numbered
as the text on stdout is. A note usually follows the text it is about, so the line
above it is the likelier referent:

  notes:
     3  Launch is on Thursday, please read the runbook.
  >> make this shorter
     4  Ping me if that clashes with anything.

The lines say where a note was written, not how far it reaches — plenty of notes
are about the whole draft.

">>>" is a note about the whole draft. It has no one part to point at, so it comes
back without any lines:

  note (whole draft): this reads too formally all the way through

A run of note lines is one note. Later lines are indented under the first.

exit codes:
  0   saved with changes
  10  saved unchanged (approved as-is)
  20  discarded (:cq or the window was killed)
  30  deadline passed, still being edited — run the printed collect command
      once the user says they are done; do not re-run it in a wait loop
  40  sent by the user themselves — acknowledge and stop
  1   could not run the edit

flags:
  -deadline=10m  how long to wait before handing back a collect id (0 waits forever)
  -diff=false    do not report what changed
  -focus=false   open the edit window in the background
  -version       print the version and exit

`)
}
