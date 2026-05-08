local lc = require("librecode")

local M = {}

function M.window()
  return lc.win.find({ role = "composer" })
end

function M.buffer()
  local win = M.window()
  if win ~= nil then
    local buffer = lc.win.get_buf(win)
    if buffer ~= nil and buffer ~= "" then
      return buffer
    end
  end
  return "composer"
end

function M.get()
  return lc.buf.get(M.buffer())
end

function M.set(value)
  lc.buf.set(M.buffer(), value)
end

function M.text()
  return lc.buf.get_text(M.buffer())
end

function M.set_text(text)
  lc.buf.set_text(M.buffer(), text or "")
end

function M.cursor()
  return lc.buf.get_cursor(M.buffer())
end

function M.set_cursor(cursor)
  lc.buf.set_cursor(M.buffer(), cursor or 0)
end

function M.insert(text)
  lc.buf.insert(M.buffer(), M.cursor(), text or "")
end

function M.clear()
  lc.buf.clear(M.buffer())
end

function M.submit()
  lc.action.run("submit")
end

function M.history_prev()
  lc.action.run("history.prev")
end

function M.history_next()
  lc.action.run("history.next")
end

return M
