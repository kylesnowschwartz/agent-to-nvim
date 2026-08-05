# agent-to-nvim

Hand a draft an agent wrote to a human, get the edited version back.

It's annoying to go back and forth with agents when drafting content.
`agent-to-nvim` opens the draft in nvim in a tmux window instead, blocks until the
edit finishes, and prints the resulting text on stdout — so the agent continues
from what the human actually wants to send.

```
$ agent-to-nvim /tmp/slack-launch-announcement.md
# nvim opens in a new tmux window; you edit and :wq
Hey team — launch is Thursday, not Wednesday. Please review the runbook first.
$ echo $?
0
```

## Install

Two steps, because the plugin carries the wiring and the binary does the work.

```
go install github.com/kylesnowschwartz/agent-to-nvim@latest
```

```
/plugin marketplace add kylesnowschwartz/agent-to-nvim
/plugin install agent-to-nvim@agent-to-nvim
```

That registers the plan-review hook and the drafting skill together. Restart Claude
Code once and both are live.

Requires tmux and nvim on `PATH`, and the binary on `PATH` too — the hook runs
`agent-to-nvim plan` by name. Without it the hook fails and Claude Code falls back
to asking about the plan itself, so a missing binary costs the review rather than
the session.

Working on the plugin itself is the same two steps against the checkout:

```
make install
/plugin marketplace add .
/plugin install agent-to-nvim@agent-to-nvim
```

A marketplace on a path is read from a copy taken at install time, not from the
checkout, and `/plugin update` refuses to re-copy while the version is unchanged. So
a change to the hook or the skill reaches Claude Code on a reinstall:

```
/plugin uninstall agent-to-nvim@agent-to-nvim
/plugin install agent-to-nvim@agent-to-nvim
```

The Go side has no such gap — `make install` is live immediately.

## Usage

```
agent-to-nvim [flags] <file>
agent-to-nvim collect [flags] <id>
agent-to-nvim plan [flags]
```

`plan` is the Claude Code plan-review hook — see [Reviewing a
plan](#reviewing-a-plan). The rest of this describes handing a draft over.

The file is edited in place, so an edit to a real project file is saved where it
belongs. Agents should write the draft with a file tool and pass the path rather
than piping text in.

A draft that is not already a file goes in `~/.local/state/agent-to-nvim/drafts/`
under a name that says what it is. That path is fixed so an agent can write
straight to it rather than spending a command minting a temp directory first, and
a draft handed over from there is removed once its text has been handed back — so
the next handover under the same name starts clean. A discarded or abandoned edit
leaves the file alone, since those words went nowhere else. A draft outside that
directory is never removed.

| Flag | Default | Purpose |
| --- | --- | --- |
| `-deadline` | `8m` | How long to wait before handing back a collect id. `0` waits forever. |
| `-diff` | `true` | Report what changed on stderr. |
| `-focus` | `true` | Open the edit window in the foreground and restore the previous window afterward. |

`AGENT_TO_NVIM_EDITOR` overrides nvim. `XDG_STATE_HOME` moves the state directory
from its default `~/.local/state/agent-to-nvim/`.

## Exit codes

The exit code is the agent-facing API — it says what to do next without parsing
any output.

| Exit | Meaning |
| --- | --- |
| 0 | Saved with changes. The edited text is on stdout. |
| 10 | Saved unchanged — approved as-is. The text is still on stdout. |
| 20 | Discarded with `:cq`, or the window was killed. Nothing on stdout. Do not proceed. |
| 30 | Deadline passed and the draft is still open. The resume command is on stderr. |
| 1 | Could not run the edit. |

Changed and unchanged are told apart by comparing content, not modification times
— writing in nvim touches mtime even when nothing changed.

Only the draft text ever goes to stdout. Status lines, the change report, notes,
and the resume command go to stderr, so a caller can use stdout directly.

## Notes to the agent

Editing a draft answers in two registers at once: the text gets fixed, and asides
get left about it. A line started with `>>` is an aside — it is reported on stderr
and kept off stdout, so what stdout carries can be sent as it stands:

```
$ agent-to-nvim announcement.md
agent-to-nvim: draft edited, 2 notes
@@ line 1 @@
~ Launch is on [-Wednesday.-]{+Thursday.+}
  Read the runbook first.

notes:
   1  Launch is on Thursday.
>> check that with ops before you post it
   2  Read the runbook first.
>> link the runbook?
   3  Ping me if that clashes.
```

An aside is written about a particular part of the draft, so the draft around it
comes with it. Every stretch of the draft carrying asides is quoted once, numbered
as the text on stdout is — an aside's own line is not in that text, so counting it
would point at the wrong line. Each aside sits where it was written, marked `>>`
in the margin while draft lines are indented past it, so the margin alone tells the
two apart.

Asides written into the same stretch of the draft share one quote of it, and a line
between two of them is one line of the report. Asides written somewhere else start
another quote, separated by a blank line.

An aside usually follows the text it is about, so the line above it is the one it
most likely refers to. Blank lines are stepped over, since an aside written under a
paragraph usually has an empty line above it. The quoted lines say where an aside
was written, not how far it reaches — plenty of asides are about the whole draft.

The count of asides is on the outcome line rather than at the head of the section,
where a number would sit above the draft line numbers and read as one of them. What
an aside says is never abbreviated: it is the only place the instruction exists.

Some asides are about the draft as a whole rather than any line of it — "too
formal", "no rollback section anywhere". Those are written with `>>>` and come back
above the quote with no lines around them, since where they were typed says nothing
about what they are for:

```
notes:
>>> this reads too formally all the way through
```

A run of neighbouring note lines is one note, so a thought too long for one line
does not arrive as several. Every line of it keeps the marker, so the margin still
tells the note from the draft:

```
notes:
   1  Launch is on Thursday.
>> check that with ops
>> they asked for it twice
```

A blank line between two notes separates them, which is how to ask for two notes
about the same place rather than one. A blank marker line inside a run is a
paragraph break within the one note.

The marker only counts at the start of a line, so an indented `>>` — a nested
blockquote, a shell redirect in a code block — stays in the draft as text.

A draft left word for word alone but annotated exits 0, not 10: the note is the
edit, and there is something to act on.

## What changed

An agent handed an edited draft would otherwise have to work out the change by
comparing the result against whatever it remembers writing. That fails for a file
it never read and for a `collect` that runs in a later process, so the change is
reported on stderr instead:

```
$ agent-to-nvim announcement.md
agent-to-nvim: draft edited
@@ line 3 @@
~ Launch is on [-Wednesday,-]{+Thursday,+} please read the runbook first.
+ Ping me if that clashes with anything.
```

`~` is a replaced line with `[-removed-]` and `{+added+}` words marked, `-` and `+`
are lines removed or added outright, and a two-space prefix is unchanged text shown
to place the change. Marking words rather than lines keeps a one-word edit from
reading as a rewritten paragraph, which is what pushes an agent back to guessing.

The draft as handed over is kept in the state directory beside the session record,
so `collect` reports the same change the blocking run would have. Everything the
edit left there is dropped once it resolves.

## Long edits

A blocking command usually sits inside somebody else's timeout — Claude Code's
Bash tool caps at 600 seconds, and a human editing a message can take longer than
that. Rather than be killed mid-edit, `agent-to-nvim` gives up on its own terms
after `-deadline` and hands back a way to resume:

```
$ agent-to-nvim draft.md
agent-to-nvim: still being edited after 8m; the draft is safe —
wait for it with: agent-to-nvim collect 5ce4bf832a
$ echo $?
30

$ agent-to-nvim collect 5ce4bf832a
Hey team — launch is Thursday, not Wednesday.
```

nvim keeps running through all of this. `collect` reattaches to the same window and
finishes the edit, however many times it takes.

A tmux window rather than a `display-popup` because the window is owned by the tmux
server: an SSH drop or an expired VPN leaves nvim running, and reattaching brings
the edit back.

## Reviewing a plan

Claude Code asks before it acts on a plan, and it waits on a hook for the answer.
`agent-to-nvim plan` is that hook: the plan opens in nvim, and what the reader does
with it becomes the answer.

```
agent-to-nvim plan   # reads the request on stdin, writes the answer on stdout
```

Closing the window is the whole verdict, and every way to do it is spelled out
along the top so there is nothing to remember:

| Key | What it means |
| --- | --- |
| `<leader>a` | Carry this plan out. |
| `<leader>A` | Carry it out in auto mode, confirming nothing with you. |
| `<leader>c` | Answer in Claude Code's own dialog instead. Edits here are dropped. |
| `<leader>r` | Send it back to be revised. |
| `<leader>n` | Open a note about this part of the plan. |
| `<leader>N` | Open a note about the whole plan. |

`:Approve`, `:Approve!`, `:AnswerInCLI`, and `:Revise` do the same as the first
four. Under them they are just saving and quitting — `:wq` approves and `:cq` sends
the plan back — so the keys are a convenience rather than a requirement.

`<leader>A` switches the session to auto mode as well as approving, so the work runs
start to finish without stopping to have each step confirmed. It matches the "Yes,
and use auto mode" answer in Claude Code's own dialog. `<leader>a` leaves the session
in whatever mode it was already in.

The dialog's first answer — clearing the context as well — has no equivalent here.
Clearing the context is not something the answer to a plan request can carry: Claude
Code does it by turning the request down and starting a fresh turn with the plan in
it, which is not a decision a hook can return. There is a request open for one at
[anthropics/claude-code#84098](https://github.com/anthropics/claude-code/issues/84098).

`<leader>c` is for exactly that answer. Claude Code shows its own dialog while this
window is open and takes whichever answer arrives first, so a reader who wants the
context cleared has to give it there. Pressing `<leader>c` says so: the window
closes, the tool writes no answer, and the dialog is left to decide. The plan is
read in nvim and answered in Claude Code, which is why edits made in the window are
dropped — the dialog approves the plan as it was submitted.

Notes travel either way, and which key you pressed decides what they are for:

- **Approved with notes** — the notes go into the plan under a `## Notes from the
  review` heading, saying plainly that they are guidance for the work rather than a
  reason to plan again. An approval carries the plan and nothing else, so the plan
  is the only place a note can go and still be read.
- **Sent back with notes** — the notes become the revision brief, along with a
  word-marked account of anything you rewrote.
- **Sent back with nothing** — the answer says you turned the plan down without
  saying why, and asks rather than guessing at a new one.

Editing the plan and approving it carries out **your** version: the approval hands
back the text you left, not the text the agent submitted. So a plan that is nearly
right is faster to fix than to explain.

The plan is reviewed as a copy under `~/.local/state/agent-to-nvim/plans/`. Claude
Code writes the plan to a file of its own first, but an approved plan travels back
inside the answer rather than on disk, so there is nothing to gain by editing that
file and a directory belonging to another tool to keep out of. A copy is dropped
once its plan is approved and kept when the plan was sent back, since the wording
you wrote is in it and only an account of it went back.

Nothing is written on stdout unless there is an answer to give. A review that
cannot run — no tmux, no nvim — says why on stderr and exits 1, which leaves Claude
Code to ask about the plan its own way rather than acting on a verdict nobody gave.

### Wiring it up

Installing the plugin (see [Install](#install)) registers the hook against Claude
Code's plan-approval request. There is nothing to add to `settings.json` by hand,
and adding it there as well would answer the same request twice.

`hooks/hooks.json` gives Claude Code a day to wait, which is longer than any review
takes. Closing the window ends a review you have lost interest in, so only a review
nobody closes runs that time down.

Anything else answering the same request races this one — two windows, two answers,
and whichever lands first wins. Turn off any other plan-review tool before
installing.

## Claude Code skill

The `agent-to-nvim` skill teaches agents when to reach for this and how to read the
exit codes. Installing the plugin brings it along, reachable as
`/agent-to-nvim:agent-to-nvim`.

It hands over the last draft in the conversation, and naming one
(`/agent-to-nvim:agent-to-nvim the slack message`) picks that instead. Agents also
reach for it on their own whenever they write a draft headed somewhere outside the
conversation.

## Working on it

`make check` runs the tests, the linter, and a comparison of the version in
`.claude-plugin/plugin.json` against the one in the marketplace entry. Those two have
to agree: the first keys the install cache and the second is what a reader browsing
the catalogue sees, so a bump applied to one and not the other looks fine from either
side alone and breaks the release.
