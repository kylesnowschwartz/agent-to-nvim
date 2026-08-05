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

-- Saving is approval and quitting with a failure is a request to revise. Both
-- write first: the notes are the answer either way, and an unwritten buffer would
-- send the plan back with nothing said about it.
vim.api.nvim_buf_create_user_command(plan, "Approve", function()
  vim.cmd("write")
  vim.cmd("quit")
end, { desc = "carry this plan out" })

vim.api.nvim_buf_create_user_command(plan, "Revise", function()
  vim.cmd("write")
  vim.cmd("cquit")
end, { desc = "send the plan back to be revised" })

local keys = {
  { "<leader>a", "<Cmd>Approve<CR>", "carry this plan out" },
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
  "<leader>r revise",
  "<leader>n note here",
  "<leader>N note on all of it",
}, "     ")

-- A note is not plan text, so it reads as set apart from it.
vim.fn.matchadd("Todo", "^>>>\\?.*$")
