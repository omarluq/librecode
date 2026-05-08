local chat = require("librecode.chat")
local lc = require("librecode")

local M = {}

function M.buffer()
  return chat.buffer_for_role("status")
end

function M.get()
  return lc.buf.get_text(M.buffer())
end

function M.set(text)
  lc.buf.set_text(M.buffer(), text or "")
end

function M.clear()
  lc.buf.clear(M.buffer())
end

return M
