# agent-to-nvim

Hand a draft an agent wrote to a human in nvim, get the edited version back.

The agent runs `agent-to-nvim <file>`. nvim opens in a new tmux window, the command
blocks until the edit ends, and the exit code says what happened.

```
$ agent-to-nvim ~/.local/state/agent-to-nvim/drafts/slack-launch.md
# nvim opens; edit and :wq
Hey team — launch is Thursday, not Wednesday.
```

## Install

Requires `tmux` and `nvim` on `PATH`.

```
go install github.com/kylesnowschwartz/agent-to-nvim@latest
```

Then in Claude Code, which adds the plan-review hook and the drafting skill:

```
/plugin marketplace add kylesnowschwartz/agent-to-nvim
/plugin install agent-to-nvim@agent-to-nvim
```

Restart Claude Code once. The binary must stay on `PATH`: the hook calls it by name.

## Handing over a draft

```
agent-to-nvim [flags] <file>
agent-to-nvim collect [flags] <id>
```

- **Files are edited in place.** Write the draft with a file tool and pass the path.
- **Scratch drafts** go in `~/.local/state/agent-to-nvim/drafts/`. They are removed
  after handback and their text is printed on stdout. Files elsewhere are never
  removed and print nothing: the file already holds the result.
- **stdout carries only draft text.** The change report, notes, and status go to
  stderr.

| Flag | Default | Purpose |
| --- | --- | --- |
| `-deadline` | `10m` | How long to wait before handing back a collect id. `0` waits forever. |
| `-diff` | `true` | Report what changed on stderr. |
| `-focus` | `true` | Focus the edit window, then restore the previous one. |

`AGENT_TO_NVIM_EDITOR` overrides nvim. `XDG_STATE_HOME` moves the state directory.

### Exit codes

| Exit | Meaning |
| --- | --- |
| 0 | Saved with changes or notes. |
| 10 | Saved unchanged. Approved as-is. |
| 20 | Discarded with `:cq` or the window was killed. Do not proceed. |
| 30 | Deadline passed. The draft is still open; the resume command is on stderr. |
| 40 | The user sent it themselves with `:Sent` or `:Done`. Stop. |
| 1 | Could not run the edit. |

### Notes to the agent

A line starting with `>>` is a note for the agent, not part of the draft. It is
reported on stderr with the surrounding draft lines and stripped from the text.
`>>>` marks a note about the whole draft. Neighbouring note lines form one note.
Indented markers stay in the draft as text.

```
notes:
   1  Launch is on Thursday.
>> check that with ops before you post it
>>> this reads too formally all the way through
```

### What changed

The diff on stderr marks words, not lines: `~` is a changed line with
`[-removed-]` and `{+added+}`, `-` and `+` are whole lines, two spaces is context.

### Long edits

After `-deadline` the command exits 30 and prints a resume command. nvim keeps
running. Tell the user to say when they are done, then run it once:

```
agent-to-nvim collect 5ce4bf832a
```

## Reviewing a plan

The plugin registers `agent-to-nvim plan` as Claude Code's plan-approval hook. The
plan opens in nvim and closing the window is the answer. The keys are listed at the
top of the buffer:

| Key | Command | Meaning |
| --- | --- | --- |
| `<leader>a` | `:Approve` (`:wq`) | Carry out the plan as it reads now, including your edits. |
| `<leader>A` | `:Approve!` | Approve and switch the session to auto mode. |
| `<leader>r` | `:Revise` (`:cq`) | Send it back. Notes and edits become the revision brief. |
| `<leader>c` | `:AnswerInCLI` | Answer in Claude Code's own dialog instead. Edits are dropped. |
| `<leader>n` / `<leader>N` | | Add a note about this part, or the whole plan. |

Notes on an approved plan are appended under `## Notes from the review`. Answering
in Claude Code's dialog closes the window. "Clear context and approve" has no
equivalent here; use `<leader>c` for it
([anthropics/claude-code#84098](https://github.com/anthropics/claude-code/issues/84098)).

Turn off any other plan-review hook first: two answers to one request race.

## Claude Code skill

The plugin also installs `/agent-to-nvim:agent-to-nvim`, which hands over the last
draft in the conversation, or the one named. Agents use it on their own for drafts
headed outside the conversation.

## Working on it

`make install` builds the binary. `make check` runs tests, lint, and the version
consistency check. Hook or skill changes reach Claude Code only after
`/plugin uninstall` and `/plugin install` again.
