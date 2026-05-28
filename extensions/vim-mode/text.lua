local M = {}

function M.clamp(value, low, high)
  if value < low then
    return low
  end
  if value > high then
    return high
  end
  return value
end

function M.copy_chars(chars)
  local out = {}
  for i = 1, #chars do
    out[i] = chars[i]
  end
  return out
end

function M.chars_from_text(text)
  local chars = {}
  text = text or ""
  for ch in text:gmatch("[%z\1-\127\194-\244][\128-\191]*") do
    chars[#chars + 1] = ch
  end
  return chars
end

local concat_chunk_size = 1024

function M.join_chars(chars)
  local count = #chars
  if count == 0 then
    return ""
  end

  local chunks = chars
  while #chunks > concat_chunk_size do
    local next_chunks = {}
    for start_index = 1, #chunks, concat_chunk_size do
      local end_index = math.min(start_index + concat_chunk_size - 1, #chunks)
      next_chunks[#next_chunks + 1] = table.concat(chunks, "", start_index, end_index)
    end
    chunks = next_chunks
  end

  return table.concat(chunks)
end

function M.is_space(ch)
  return ch == nil or ch:match("^%s$") ~= nil
end

function M.is_word(ch)
  return ch ~= nil and ch:match("^[%w_]$") ~= nil
end

function M.line_start(chars, cursor)
  local i = M.clamp(cursor, 0, #chars)
  while i > 0 and chars[i] ~= "\n" do
    i = i - 1
  end
  return i
end

function M.line_end(chars, cursor)
  local i = M.clamp(cursor, 0, #chars)
  while i < #chars and chars[i + 1] ~= "\n" do
    i = i + 1
  end
  return i
end

function M.line_bounds(chars, cursor, include_newline)
  local start_pos = M.line_start(chars, cursor)
  local end_pos = M.line_end(chars, cursor)
  if include_newline and end_pos < #chars and chars[end_pos + 1] == "\n" then
    end_pos = end_pos + 1
  end
  return start_pos, end_pos
end

function M.first_nonblank(chars, cursor)
  local start_pos, end_pos = M.line_bounds(chars, cursor, false)
  local i = start_pos
  while i < end_pos and M.is_space(chars[i + 1]) do
    i = i + 1
  end
  return i
end

return M
