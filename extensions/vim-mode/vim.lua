local text = require("text")

local state = {
  mode = "insert",
  pending = nil,
  visual_start = nil,
  yank = {},
  undo = {},
  redo = {},
  replace = false,
}

local lc

local clamp = text.clamp
local copy_chars = text.copy_chars
local chars_from_text = text.chars_from_text
local is_space = text.is_space
local is_word = text.is_word
local line_start = text.line_start
local line_end = text.line_end
local line_bounds = text.line_bounds
local first_nonblank = text.first_nonblank

local function chars_from_buffer(buf)
  if buf.chars and #buf.chars > 0 then
    return copy_chars(buf.chars)
  end

  return chars_from_text(buf.text or "")
end
local function get_composer()
  local buf = lc.buf.get("composer") or {}
  local chars = chars_from_buffer(buf)
  local cursor = clamp(tonumber(buf.cursor or 0) or 0, 0, #chars)
  return buf, chars, cursor
end

local function mode_label()
  if state.mode == "visual" then
    return "visual"
  end
  if state.mode == "normal" then
    if state.pending then
      return "normal " .. state.pending
    end
    if state.replace then
      return "normal r"
    end
    return "normal"
  end
  return "insert"
end

local function set_composer(chars, cursor)
  cursor = clamp(cursor or 0, 0, #chars)
  local buf = lc.buf.get("composer") or {}
  buf.name = "composer"
  buf.text = table.concat(chars)
  buf.chars = copy_chars(chars)
  buf.cursor = cursor
  buf.label = mode_label()
  lc.buf.set("composer", buf)
end

local function sync_label()
  local _, chars, cursor = get_composer()
  set_composer(chars, cursor)
end

local function snapshot(chars, cursor)
  return { chars = copy_chars(chars), cursor = cursor }
end

local function push_undo(chars, cursor)
  state.undo[#state.undo + 1] = snapshot(chars, cursor)
  if #state.undo > 100 then
    table.remove(state.undo, 1)
  end
  state.redo = {}
end

local function restore(snap)
  if not snap then
    return
  end
  set_composer(copy_chars(snap.chars), snap.cursor)
end

local function move_left(chars, cursor)
  return clamp(cursor - 1, 0, #chars)
end

local function move_right(chars, cursor)
  return clamp(cursor + 1, 0, #chars)
end

local function move_line_delta(chars, cursor, delta)
  local current_start = line_start(chars, cursor)
  local current_col = cursor - current_start
  local target_start = current_start

  if delta < 0 then
    if current_start == 0 then
      return cursor
    end
    target_start = line_start(chars, current_start - 1)
  elseif delta > 0 then
    local current_end = line_end(chars, cursor)
    if current_end >= #chars then
      return cursor
    end
    target_start = current_end + 1
  end

  local target_end = line_end(chars, target_start)
  return clamp(target_start + current_col, target_start, target_end)
end

local function move_word_forward(chars, cursor)
  local i = clamp(cursor, 0, #chars)
  local current = chars[i + 1]
  if current ~= nil and not is_space(current) then
    local word_kind = is_word(current)
    while i < #chars and chars[i + 1] ~= nil and not is_space(chars[i + 1]) and is_word(chars[i + 1]) == word_kind do
      i = i + 1
    end
  end
  while i < #chars and is_space(chars[i + 1]) do
    i = i + 1
  end
  return clamp(i, 0, #chars)
end

local function move_word_back(chars, cursor)
  local i = clamp(cursor, 0, #chars)
  while i > 0 and is_space(chars[i]) do
    i = i - 1
  end
  local current = chars[i]
  if current == nil then
    return 0
  end
  local word_kind = is_word(current)
  while i > 0 and chars[i] ~= nil and not is_space(chars[i]) and is_word(chars[i]) == word_kind do
    i = i - 1
  end
  return clamp(i, 0, #chars)
end

local function move_word_end(chars, cursor)
  local i = clamp(cursor, 0, #chars)
  if i < #chars then
    i = i + 1
  end
  while i <= #chars and is_space(chars[i]) do
    i = i + 1
  end
  local current = chars[i]
  if current == nil then
    return #chars
  end
  local word_kind = is_word(current)
  while i <= #chars and chars[i] ~= nil and not is_space(chars[i]) and is_word(chars[i]) == word_kind do
    i = i + 1
  end
  return clamp(i - 1, 0, #chars)
end

local function motion_range(chars, cursor, key)
  local target = cursor
  if key == "h" or key == "left" then
    target = move_left(chars, cursor)
  elseif key == "l" or key == "right" then
    target = move_right(chars, cursor)
  elseif key == "j" or key == "down" then
    target = move_line_delta(chars, cursor, 1)
  elseif key == "k" or key == "up" then
    target = move_line_delta(chars, cursor, -1)
  elseif key == "0" or key == "home" then
    target = line_start(chars, cursor)
  elseif key == "^" then
    target = first_nonblank(chars, cursor)
  elseif key == "$" or key == "end" then
    target = line_end(chars, cursor)
  elseif key == "w" then
    target = move_word_forward(chars, cursor)
  elseif key == "b" then
    target = move_word_back(chars, cursor)
  elseif key == "e" then
    target = move_word_end(chars, cursor)
  elseif key == "G" then
    target = #chars
  end

  local start_pos = math.min(cursor, target)
  local end_pos = math.max(cursor, target)
  if target > cursor and end_pos < #chars then
    end_pos = end_pos + 1
  end
  return start_pos, end_pos, target
end

local function delete_range(chars, start_pos, end_pos)
  start_pos = clamp(start_pos, 0, #chars)
  end_pos = clamp(end_pos, start_pos, #chars)
  local removed = {}
  for i = start_pos + 1, end_pos do
    removed[#removed + 1] = chars[i]
  end
  local out = {}
  for i = 1, start_pos do
    out[#out + 1] = chars[i]
  end
  for i = end_pos + 1, #chars do
    out[#out + 1] = chars[i]
  end
  return out, removed, start_pos
end

local function insert_at(chars, cursor, text)
  local inserted = chars_from_text(text or "")
  local out = {}
  for i = 1, cursor do
    out[#out + 1] = chars[i]
  end
  for i = 1, #inserted do
    out[#out + 1] = inserted[i]
  end
  for i = cursor + 1, #chars do
    out[#out + 1] = chars[i]
  end
  return out, cursor + #inserted
end

local function set_mode(mode)
  state.mode = mode
  state.pending = nil
  state.replace = false
  if mode ~= "visual" then
    state.visual_start = nil
  end
  sync_label()
end

local function yank_range(chars, start_pos, end_pos)
  state.yank = {}
  for i = start_pos + 1, end_pos do
    state.yank[#state.yank + 1] = chars[i]
  end
end

local function paste(after)
  local _, chars, cursor = get_composer()
  if #state.yank == 0 then
    return true
  end
  push_undo(chars, cursor)
  local pos = cursor
  if after then
    pos = clamp(cursor + 1, 0, #chars)
  end
  local out = {}
  for i = 1, pos do
    out[#out + 1] = chars[i]
  end
  for i = 1, #state.yank do
    out[#out + 1] = state.yank[i]
  end
  for i = pos + 1, #chars do
    out[#out + 1] = chars[i]
  end
  set_composer(out, pos + #state.yank)
  return true
end

local function handle_insert(ev)
  local _, chars, cursor = get_composer()
  local key = ev.key

  if key == "escape" then
    set_mode("normal")
    return true
  elseif key == "enter" then
    lc.action.run("submit")
    return true
  elseif key == "tab" then
    lc.action.run("autocomplete.accept")
    return true
  elseif key == "left" then
    set_composer(chars, move_left(chars, cursor))
    return true
  elseif key == "right" then
    set_composer(chars, move_right(chars, cursor))
    return true
  elseif key == "up" then
    set_composer(chars, move_line_delta(chars, cursor, -1))
    return true
  elseif key == "down" then
    set_composer(chars, move_line_delta(chars, cursor, 1))
    return true
  elseif key == "home" or key == "ctrl+a" then
    set_composer(chars, line_start(chars, cursor))
    return true
  elseif key == "end" or key == "ctrl+e" then
    set_composer(chars, line_end(chars, cursor))
    return true
  elseif key == "backspace" then
    if cursor > 0 then
      push_undo(chars, cursor)
      local next_chars = delete_range(chars, cursor - 1, cursor)
      set_composer(next_chars, cursor - 1)
    end
    return true
  elseif key == "delete" then
    if cursor < #chars then
      push_undo(chars, cursor)
      local next_chars = delete_range(chars, cursor, cursor + 1)
      set_composer(next_chars, cursor)
    end
    return true
  elseif key == "ctrl+c" then
    -- Let librecode's native Ctrl+C clear/exit behavior run.
    return false
  end

  if ev.text and ev.text ~= "" and not ev.ctrl and not ev.alt then
    push_undo(chars, cursor)
    local next_chars, next_cursor = insert_at(chars, cursor, ev.text)
    set_composer(next_chars, next_cursor)
    return true
  end

  return false
end

local function apply_operator(op, motion)
  local _, chars, cursor = get_composer()
  local start_pos, end_pos, target = motion_range(chars, cursor, motion)
  if start_pos == end_pos and motion ~= "$" then
    return true
  end
  if op == "y" then
    yank_range(chars, start_pos, end_pos)
    set_mode("normal")
    return true
  end

  push_undo(chars, cursor)
  local next_chars, removed, next_cursor = delete_range(chars, start_pos, end_pos)
  state.yank = removed
  set_composer(next_chars, next_cursor)
  if op == "c" then
    set_mode("insert")
  else
    set_mode("normal")
  end
  return true
end

local function apply_line_operator(op)
  local _, chars, cursor = get_composer()
  local start_pos, end_pos = line_bounds(chars, cursor, true)
  if op == "y" then
    yank_range(chars, start_pos, end_pos)
    set_mode("normal")
    return true
  end

  push_undo(chars, cursor)
  local next_chars, removed, next_cursor = delete_range(chars, start_pos, end_pos)
  state.yank = removed
  set_composer(next_chars, next_cursor)
  if op == "c" then
    set_mode("insert")
  else
    set_mode("normal")
  end
  return true
end

local function undo()
  local _, chars, cursor = get_composer()
  local snap = table.remove(state.undo)
  if not snap then
    return true
  end
  state.redo[#state.redo + 1] = snapshot(chars, cursor)
  restore(snap)
  return true
end

local function redo()
  local _, chars, cursor = get_composer()
  local snap = table.remove(state.redo)
  if not snap then
    return true
  end
  state.undo[#state.undo + 1] = snapshot(chars, cursor)
  restore(snap)
  return true
end

local function handle_normal(ev)
  local _, chars, cursor = get_composer()
  local key = ev.key
  if ev.text == "G" then
    key = "G"
  end

  if state.replace then
    if key == "escape" then
      state.replace = false
      sync_label()
      return true
    end
    if ev.text and ev.text ~= "" and not ev.ctrl and not ev.alt and cursor < #chars then
      push_undo(chars, cursor)
      chars[cursor + 1] = ev.text
      state.replace = false
      set_composer(chars, move_right(chars, cursor))
      return true
    end
    return true
  end

  if state.pending == "g" then
    state.pending = nil
    if key == "g" then
      set_composer(chars, 0)
      return true
    end
    sync_label()
    return true
  elseif state.pending == "d" or state.pending == "y" or state.pending == "c" then
    local op = state.pending
    state.pending = nil
    if key == op then
      return apply_line_operator(op)
    end
    return apply_operator(op, key)
  end

  if key == "escape" then
    set_mode("normal")
    return true
  elseif key == "i" then
    set_mode("insert")
    return true
  elseif key == "a" then
    set_composer(chars, move_right(chars, cursor))
    set_mode("insert")
    return true
  elseif key == "I" then
    set_composer(chars, first_nonblank(chars, cursor))
    set_mode("insert")
    return true
  elseif key == "A" then
    set_composer(chars, line_end(chars, cursor))
    set_mode("insert")
    return true
  elseif key == "o" or key == "O" then
    push_undo(chars, cursor)
    local pos = line_end(chars, cursor)
    if key == "O" then
      pos = line_start(chars, cursor)
      local next_chars, next_cursor = insert_at(chars, pos, "\n")
      set_composer(next_chars, next_cursor - 1)
    else
      local next_chars, next_cursor = insert_at(chars, pos, "\n")
      set_composer(next_chars, next_cursor)
    end
    set_mode("insert")
    return true
  elseif key == "v" then
    state.mode = "visual"
    state.visual_start = cursor
    sync_label()
    return true
  elseif key == "g" or key == "d" or key == "y" or key == "c" then
    state.pending = key
    sync_label()
    return true
  elseif key == "r" then
    state.replace = true
    sync_label()
    return true
  elseif key == "u" then
    return undo()
  elseif key == "ctrl+r" then
    return redo()
  elseif key == "p" then
    return paste(true)
  elseif key == "P" then
    return paste(false)
  elseif key == "x" then
    if cursor < #chars then
      push_undo(chars, cursor)
      local next_chars, removed, next_cursor = delete_range(chars, cursor, cursor + 1)
      state.yank = removed
      set_composer(next_chars, next_cursor)
    end
    return true
  elseif key == "X" then
    if cursor > 0 then
      push_undo(chars, cursor)
      local next_chars, removed, next_cursor = delete_range(chars, cursor - 1, cursor)
      state.yank = removed
      set_composer(next_chars, next_cursor)
    end
    return true
  elseif key == "D" then
    push_undo(chars, cursor)
    local next_chars, removed, next_cursor = delete_range(chars, cursor, line_end(chars, cursor))
    state.yank = removed
    set_composer(next_chars, next_cursor)
    return true
  elseif key == "C" then
    push_undo(chars, cursor)
    local next_chars, removed, next_cursor = delete_range(chars, cursor, line_end(chars, cursor))
    state.yank = removed
    set_composer(next_chars, next_cursor)
    set_mode("insert")
    return true
  elseif key == "enter" then
    lc.action.run("submit")
    return true
  elseif key == "ctrl+c" then
    return false
  end

  local motions = {
    h = true,
    j = true,
    k = true,
    l = true,
    left = true,
    right = true,
    up = true,
    down = true,
    ["0"] = true,
    ["^"] = true,
    ["$"] = true,
    home = true,
    ["end"] = true,
    w = true,
    b = true,
    e = true,
    G = true,
  }
  if motions[key] then
    local _, _, target = motion_range(chars, cursor, key)
    set_composer(chars, target)
    return true
  end

  return true
end

local function handle_visual(ev)
  local _, chars, cursor = get_composer()
  local key = ev.key
  if ev.text == "G" then
    key = "G"
  end

  if key == "escape" or key == "v" then
    set_mode("normal")
    return true
  end

  local motions = {
    h = true,
    j = true,
    k = true,
    l = true,
    left = true,
    right = true,
    up = true,
    down = true,
    ["0"] = true,
    ["^"] = true,
    ["$"] = true,
    home = true,
    ["end"] = true,
    w = true,
    b = true,
    e = true,
    G = true,
  }
  if motions[key] then
    local _, _, target = motion_range(chars, cursor, key)
    set_composer(chars, target)
    return true
  end

  if key == "y" or key == "d" or key == "c" then
    local start_pos = math.min(state.visual_start or cursor, cursor)
    local end_pos = math.max(state.visual_start or cursor, cursor)
    if end_pos < #chars then
      end_pos = end_pos + 1
    end
    if key == "y" then
      yank_range(chars, start_pos, end_pos)
      set_mode("normal")
      return true
    end
    push_undo(chars, cursor)
    local next_chars, removed, next_cursor = delete_range(chars, start_pos, end_pos)
    state.yank = removed
    set_composer(next_chars, next_cursor)
    if key == "c" then
      set_mode("insert")
    else
      set_mode("normal")
    end
    return true
  end

  return true
end

local function setup(api)
  lc = api
  lc.on("startup", function()
    state.mode = "insert"
    sync_label()
  end)

  lc.keymap.set({ role = "composer" }, "*", function(ev)
    if state.mode == "insert" then
      return handle_insert(ev)
    elseif state.mode == "visual" then
      return handle_visual(ev)
    end
    return handle_normal(ev)
  end, { priority = 1000, desc = "Vim composer mode" })
end

return setup
