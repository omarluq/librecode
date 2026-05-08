local M = {}

local function stringify(value)
  if value == nil then
    return ""
  end
  return tostring(value)
end

local function display_width(text)
  local ok, chars = pcall(function()
    local count = 0
    for _ in string.gmatch(text or "", "[%z\1-\127\194-\244][\128-\191]*") do
      count = count + 1
    end
    return count
  end)
  if ok then
    return chars
  end
  return #(text or "")
end

function M.truncate(text, width)
  text = stringify(text)
  width = width or 0
  if width <= 0 then
    return ""
  end
  if display_width(text) <= width then
    return text
  end

  local out = {}
  local count = 0
  for char in string.gmatch(text, "[%z\1-\127\194-\244][\128-\191]*") do
    if count >= width then
      break
    end
    out[#out + 1] = char
    count = count + 1
  end
  return table.concat(out, "")
end

function M.current_status(event)
  local status = event.buffers and event.buffers.status
  if status == nil then
    return ""
  end

  local message = status.metadata and status.metadata.message
  if message ~= nil and message ~= "" then
    return stringify(message)
  end

  return stringify(status.text)
end

function M.lines(event)
  local context = event.context or {}
  local data = event.data or {}
  local status = M.current_status(event)
  local lines = {}

  local cwd = stringify(context.cwd)
  if cwd ~= "" then
    local session = stringify(context.session_id)
    if session ~= "" then
      cwd = cwd .. " • " .. session
    end
    lines[#lines + 1] = cwd
  end

  local model = stringify(data.model_label)
  if model ~= "" then
    local thinking = stringify(data.thinking_level)
    if thinking ~= "" then
      model = model .. " • " .. thinking
    end
    lines[#lines + 1] = model
  end

  if status ~= "" then
    lines[#lines + 1] = status
  end

  return lines
end

function M.buffer()
  local lc = require("librecode")
  local win = lc.win.find({ role = "status" })
  if win ~= nil then
    local buffer = lc.win.get_buf(win)
    if buffer ~= nil and buffer ~= "" then
      return buffer
    end
  end
  return "status"
end

function M.get()
  local lc = require("librecode")
  return lc.buf.get_text(M.buffer())
end

function M.set(text)
  local lc = require("librecode")
  lc.buf.set_text(M.buffer(), text or "")
end

function M.clear()
  local lc = require("librecode")
  lc.buf.clear(M.buffer())
end

return M
