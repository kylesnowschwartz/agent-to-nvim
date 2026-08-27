---
name: agent-to-nvim
description: This skill should be used when a draft has been written that the user will want to edit before it goes anywhere — a Slack message, an email, a commit message, a PR body, an announcement, release notes — or when the user asks to "let me edit that", "open that in nvim", "hand that to me", "I want to tweak it first", or runs "/agent-to-nvim". Opens the draft in nvim in a tmux window, waits for the edit, and continues from the edited text.
argument-hint: "[file path, or which draft to hand over]"
---

# Hand a draft to the user in nvim

Printing a draft into the transcript and asking "does this look right?" makes the
user retype their edits as a comment. Handing the draft to nvim instead lets them
edit it directly, and returns the text they actually want.

Use this the moment a draft exists that is headed somewhere outside the
conversation. Do not ask permission first — hand it over, then act on what comes
back.

## Resolve what to hand over

`$ARGUMENTS` decides the source:

- **Empty** — use the most recent draft written in this conversation: the message,
  email, commit body, or document the user asked for. If more than one is in play,
  hand over the one the user last talked about. If no draft exists yet, write one
  first, then hand it over.
- **A path to an existing file** — hand that file over directly. It is edited in
  place, so an edit to a real project file is already saved where it belongs.
- **Anything else** — read it as which draft is meant ("the slack message", "the
  PR description"), then hand that one over.

For a draft that is not already a file, write it to the drafts directory under
the user's home:

```
<home>/.local/state/agent-to-nvim/drafts/<name>.md
```

Spell the home directory out. A file tool takes an absolute path and does not
expand `~`, so a literal `~/...` lands in a directory called `~` beside wherever
you are working. The home directory is the leading part of the paths you already
have — `/Users/someone/Code/project` makes it `/Users/someone`.

Name the file for what it is — `slack-launch-announcement.md`, `pr-body.md`. The
name shows in the tmux window title and gives nvim its filetype. Write it with the
Write tool; do not try to pipe multiline text into the command. Do not run
`mktemp` first — the path above is fixed on purpose, and the draft is removed once
the edit resolves, so writing there needs no cleanup and no prior Read.

Pick a name you have not already used this session. If you have, add a word that
tells the two apart rather than overwriting.

## Run it

```
agent-to-nvim ~/.local/state/agent-to-nvim/drafts/slack-launch-announcement.md
```

**Run it as a background task** (`run_in_background: true` on the Bash tool). The
command waits while the user edits — up to ten minutes by default — and a
foreground call would block the whole session for that long. In the background it
costs nothing: the harness re-invokes you when the command exits, whether that is
the finished edit or the ten-minute deadline.

While it runs, carry on with other work, or end your turn if there is none — do
NOT poll the task, sleep, or wait in a loop. The exit is delivered to you. When
the notification arrives, read the task's exit code and output, then act on the
exit code exactly as the table below says.

The tmux window opens focused, and the user's previous window is restored when
they finish. Add `-focus=false` to open it in the background instead — worth doing
only when the user asked not to be interrupted.

## Act on the exit code

The exit code is the whole result. Read it before anything else.

| Exit | Meaning | What to do |
| --- | --- | --- |
| 0 | Saved with changes, or notes left | Scratch draft: use the printed text. In-place file: the file holds the text; use the diff and notes, do not re-read it. Act on any `>>` lines first — instructions get done, questions get discussed. |
| 10 | Saved unchanged | The draft was approved as-is. Continue with it. |
| 20 | Discarded | **Stop.** Do not send, commit, or post anything. Do not re-run. The file is left on disk in case they want it back. |
| 30 | Deadline passed, still being edited | Tell the user nothing was lost and wait for them to say they're done. See below. |
| 1 | Could not run the edit | Report the error. |

Two failure modes to avoid:

- **Exit 20 is not a failure to retry.** The user closed the draft on purpose.
  Reopening it overrides a decision they just made. Say the draft was discarded and
  ask what they want instead.
- **Exit 30 is not a timeout to give up on — and not one to poll on either.**
  nvim is still open and the draft is safe on disk; only the waiting process gave
  up after its ten-minute deadline. Do NOT re-run anything in a wait loop.
  Instead:

  1. Note the `collect` command printed on stderr (e.g. `agent-to-nvim collect
     5ce4bf832a`).
  2. Tell the user, briefly: "The process waiting on your edit timed out —
     that's harmless, your draft and edits are safe. Just message me when you're
     done in nvim." Then end your turn and wait for their reply.
  3. When they reply, run the noted `collect` command — as a background task,
     like the first run — and act on its exit code as normal. If it returns 30
     again, they weren't actually done — repeat step 2, don't loop.

On 0 or 10, where the text lands depends on what was handed over:

- **A scratch draft** (written to the drafts directory) comes back whole on
  stdout. Use exactly that text. Do not merge it with the original draft or
  re-apply wording that was edited out — an edit that removed something meant to
  remove it.
- **A real file edited in place** prints nothing on stdout — the file already
  holds the edited text, notes stripped. The diff and notes on stderr are the
  complete record of what happened. **Do not read the file back to "check", and
  do not open any truncated tool-output file the harness points at** — everything
  the edit changed is in the report, and re-reading a large file just spends the
  tokens the quiet stdout saved.

## Do what the notes say

A line the user starts with `>>` is a note to you, not draft text. There are two
kinds, and the number of angle brackets says the scope:

- `>>` is about the line or section it sits next to — a local note.
- `>>>` is about the whole draft — tone, structure, length, whether to send it at
  all. It has no anchor in the text, so never hunt for "the line it means".

The command keeps both off stdout and reports them on stderr instead:

```
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

So stdout is always safe to send as it stands — you never have to strip anything
out of it yourself. The file on disk is rewritten the same way: a real file edited
in place comes back holding only the draft text, never the notes. Each note report
is also saved to a file, and its path is printed after the report (`a copy of the
notes is kept at ...`) — if the report ever scrolls out of reach, read the notes
back from there instead of asking the user to retype them.

Under `notes:` is the draft quoted around the notes, numbered as the text on stdout
is. A `>>` line is what the user said; a numbered line is their draft. Each note
sits where they typed it, so the line above a note is the one it most likely refers
to — read "drop this" or "this line is wrong" against the numbered line directly
above it.

The quoted lines say where a note was written, not how far it reaches. Take the
scope from what the note says and use the lines only to place it. A note at the very
top or bottom shows only the one line it has beside it.

A `>>>` line, being about the whole draft, sits above the quote with no lines
around it:

```
notes:
>>> this reads too formally all the way through
```

A blank line means the next quote is from somewhere else in the draft. A run of
`>>` lines with nothing between them is one note running to several lines; a blank
line between two of them makes them two notes about the same place.

Each note is either an instruction or an opening for discussion, and telling the
two apart is your job:

- **A clear instruction** ("make this shorter", "drop the last para") is yours to
  carry out. Make the change and hand the draft back with `agent-to-nvim` rather
  than sending it.
- **A question, doubt, or open point** ("is this the right channel?", "check that
  with ops", "not sure about this framing") wants a conversation, not a rewrite.
  Answer it, or do the investigating it asks for, in the conversation — then wait
  for the user's take before revising and handing the draft back. Rewriting past
  an unanswered question guesses at the answer and hands the user another edit to
  redo.
- **A note that only comments** ("nice") needs nothing.

When one report mixes both kinds, hold the whole draft: raise the open points
first, fold the answers in together with the instructed changes, and hand it back
once. If a note could read either way, treat it as the open kind — a wasted
question costs one reply, a wrong rewrite costs a redo.

Exit 0 with `draft text unchanged, 2 notes` means they left the wording alone and
told you something instead. There is still work to do — do not read it as
approval.

## Read what changed

On exit 0, stderr carries the change itself, so there is no need to work it out by
comparing the result against the draft from memory:

```
agent-to-nvim: draft edited
@@ line 3 @@
~ Launch is on [-Wednesday,-]{+Thursday,+} please read the runbook first.
+ Ping me if that clashes with anything.
```

| Marker | Meaning |
| --- | --- |
| `~` | Replaced. `[-this-]` came out, `{+this+}` went in. |
| `-` | Removed outright. |
| `+` | Added outright. |
| two spaces | Unchanged, shown to place the change. |

This is the authority on what the user did — trust it over any recollection of the
draft. Two things it settles:

- **Anything inside `[- -]` is gone on purpose.** Do not restore it, and do not
  reword it back in on the grounds that it read better.
- **A `~` line changed only the marked words.** The rest of that line was left
  alone, so do not treat the whole line as rewritten.

Worth saying out loud to the user when the edit was substantive: name what they
changed rather than replaying the whole text back at them. Say it in plain words —
`[-` and `{+` are markers for reading the report, not notation to repeat at
somebody who just made the edit by hand.

Pass `-diff=false` to suppress the report, which is only worth doing when the draft
is large and the change does not matter to what happens next.

## Requirements

The command needs tmux (it opens a real tmux window, not a popup, so the edit
survives a disconnect) and nvim on `PATH`. It exits 1 with a clear message when
either is missing.

Install with `go install github.com/kylesnowschwartz/agent-to-nvim@latest`.
