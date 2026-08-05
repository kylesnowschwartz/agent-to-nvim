-- Set the editor up for reading a plan.
--
-- Reviewing a plan answers two questions at once: is this the plan to carry out,
-- and what about it needs saying. Saving answers the first and the notes answer
-- the second, so the keys here are a way to do both without knowing that saving
-- is what approval means.
--
-- Every mapping is local to the plan's own buffer, so nothing here follows the
-- reader into the rest of their session.

local plan = vim.api.nvim_get_current_buf()

-- note opens a line for an aside under the cursor. An aside is written under the
-- text it is about, which is the side the tool reads it against.
local function note(marker)
  local line = vim.api.nvim_win_get_cursor(0)[1]
  vim.api.nvim_buf_set_lines(plan, line, line, false, { marker .. " " })
  vim.api.nvim_win_set_cursor(0, { line + 1, 0 })
  vim.cmd("startinsert!")
end

-- Approving into auto mode is a second answer, and it is written down beside the
-- plan rather than carried in the exit code. An editor that crashes exits how it
-- likes, and none of those ways may read as "approve and stop asking me to confirm
-- each step" — so the wider answer is the one that has to be said explicitly, and
-- its absence is the narrower one.
local function leave_auto_mark()
  local marked = io.open(vim.api.nvim_buf_get_name(plan) .. ".auto", "w")
  if not marked then return end
  marked:write("yes")
  marked:close()
end

-- Saving is approval and quitting with a failure is a request to revise. Both
-- write first: the notes are the answer either way, and an unwritten buffer would
-- send the plan back with nothing said about it.
local function approve(auto)
  if auto then leave_auto_mark() end
  vim.cmd("write")
  vim.cmd("quit")
end

vim.api.nvim_buf_create_user_command(plan, "Approve", function(cmd)
  approve(cmd.bang)
end, { bang = true, desc = "carry this plan out; with ! carry it out in auto mode" })

vim.api.nvim_buf_create_user_command(plan, "Revise", function()
  vim.cmd("write")
  vim.cmd("cquit")
end, { desc = "send the plan back to be revised" })

-- Claude Code puts the same plan in front of the reader twice, here and in its own
-- approval dialog, and takes whichever answer comes first. Only the dialog can
-- offer to clear the conversation along with the approval, so a reader who wants
-- that has to answer there. This code says so outright, and the tool closes this
-- window and writes no answer of its own. It has to match
-- planwindow.HandedToTheDialog. Nothing here is read afterwards, so edits made in
-- this buffer are dropped rather than saved.
local handed_to_the_dialog = 7

vim.api.nvim_buf_create_user_command(plan, "AnswerInCLI", function()
  vim.cmd("cquit " .. handed_to_the_dialog)
end, { desc = "answer in Claude Code's own dialog instead; edits here are dropped" })

local keys = {
  { "<leader>a", "<Cmd>Approve<CR>", "carry this plan out" },
  { "<leader>A", "<Cmd>Approve!<CR>", "carry it out in auto mode, confirming nothing" },
  { "<leader>c", "<Cmd>AnswerInCLI<CR>", "answer in Claude Code's own dialog; edits here are dropped" },
  { "<leader>r", "<Cmd>Revise<CR>", "send the plan back to be revised" },
  { "<leader>n", function() note(">>") end, "note about this part of the plan" },
  { "<leader>N", function() note(">>>") end, "note about the whole plan" },
}
for _, key in ipairs(keys) do
  vim.keymap.set("n", key[1], key[2], { buffer = plan, desc = key[3] })
end

vim.opt_local.winbar = table.concat({
  "  plan review",
  "<leader>a approve",
  "<leader>A approve, auto mode",
  "<leader>c answer in the CLI",
  "<leader>r revise",
  "<leader>n note here",
  "<leader>N note on all of it",
}, "    ")

-- A note is not plan text, so it reads as set apart from it.
vim.fn.matchadd("Todo", "^>>>\\?.*$")
