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

```
go install github.com/kylesnowschwartz/agent-to-nvim@latest
```

Requires tmux and nvim on `PATH`.

## Usage

```
agent-to-nvim [flags] <file>
agent-to-nvim collect [flags] <id>
```

The file is edited in place, so an edit to a real project file is saved where it
belongs. Agents should write the draft with a file tool and pass the path rather
than piping text in.

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

Only the draft text ever goes to stdout. Status lines, the change report, and the
resume command go to stderr, so a caller can use stdout directly.

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

## Claude Code skill

The `agent-to-nvim` skill teaches agents when to reach for this and how to read the
exit codes. Link it in as a personal skill:

```
make install-skill
```

`/agent-to-nvim` then hands over the last draft in the conversation, and
`/agent-to-nvim the slack message` picks a specific one. Agents also reach for it on
their own whenever they write a draft headed somewhere outside the conversation.

The symlink points back at this checkout, so editing `skills/agent-to-nvim/SKILL.md`
takes effect after `/reload-skills`. `make uninstall-skill` removes the link.

The same skill also loads as a plugin with `claude --plugin-dir .`, where it is
reachable as `/agent-to-nvim:agent-to-nvim` — use that to try it without touching
`~/.claude`.
