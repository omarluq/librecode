local lc = require("librecode")

local M = {}

local function role_window(role)
  if role == nil or role == "" then
    return nil
  end
  return lc.win.find({ role = role })
end

function M.buffer_for_role(role)
  local win = role_window(role)
  if win ~= nil then
    local buffer = lc.win.get_buf(win)
    if buffer ~= nil and buffer ~= "" then
      return buffer
    end
  end
  return role
end

function M.append_block(buffer, block)
  lc.buf.append(buffer, block or {})
end

function M.append_message(role, text, metadata)
  M.append_block(M.buffer_for_role("transcript"), {
    kind = "message",
    role = role or "custom",
    text = text or "",
    metadata = metadata or {},
  })
end

function M.clear_transcript()
  lc.buf.clear(M.buffer_for_role("transcript"))
end

function M.set_status(text)
  lc.buf.set_text(M.buffer_for_role("status"), text or "")
end

return M
