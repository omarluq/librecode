local mode = "insert"
local pending = ""
local register = {}
local undo = {}
local redo = {}
local undo_limit = 100

local function utf8_chars(text)
  local chars = {}
  if text == nil or text == "" then
    return chars
  end
  if utf8 ~= nil and utf8.codes ~= nil and utf8.char ~= nil then
    for _, code in utf8.codes(text) do
      chars[#chars + 1] = utf8.char(code)
    end
    return chars
  end
  for char in string.gmatch(text, "[%z\1-\127\194-\244][\128-\191]*") do
    chars[#chars + 1] = char
  end
  return chars
end

local function copy_chars(chars)
  local out = {}
  for i = 1, #chars do
    out[i] = chars[i]
  end
  return out
end

local function join(chars)
  return table.concat(chars, "")
end

local function clamp(value, low, high)
  if value < low then
    return low
  end
  if value > high then
    return high
  end
  return value
end

local function is_space(char)
  return char ~= nil and string.match(char, "%s") ~= nil
end

local function label()
  local text = "vim:" .. string.upper(mode)
  if pending ~= "" then
    return text .. " " .. pending
  end
  return text
end

local function result(handled, chars, cursor)
  return {
    handled = handled,
    chars = chars,
    cursor = cursor,
    label = label(),
  }
end

local function current_line_start(chars, cursor)
  for i = cursor, 1, -1 do
    if chars[i] == "\n" then
      return i
    end
  end
  return 0
end

local function current_line_end(chars, cursor)
  for index = cursor + 1, #chars do
    if chars[index] == "\n" then
      return index - 1
    end
  end
  return #chars
end

local function line_end_from(chars, start)
  for index = start + 1, #chars do
    if chars[index] == "\n" then
      return index - 1
    end
  end
  return #chars
end

local function previous_line_start(chars, start)
  local index = clamp(start - 1, 0, #chars)
  while index >= 1 do
    if chars[index] == "\n" then
      return index
    end
    index = index - 1
  end
  return 0
end

local function line_first_non_blank(chars, cursor)
  local start = current_line_start(chars, cursor)
  local line_end = current_line_end(chars, cursor)
  for index = start + 1, line_end do
    if not is_space(chars[index]) then
      return index - 1
    end
  end
  return start
end

local function word_left(chars, cursor)
  local index = clamp(cursor, 0, #chars)
  while index > 0 and is_space(chars[index]) do
    index = index - 1
  end
  while index > 0 and not is_space(chars[index]) do
    index = index - 1
  end
  return index
end

local function word_right(chars, cursor)
  local index = clamp(cursor, 0, #chars)
  while index < #chars and is_space(chars[index + 1]) do
    index = index + 1
  end
  while index < #chars and not is_space(chars[index + 1]) do
    index = index + 1
  end
  return index
end

local function word_end(chars, cursor)
  if #chars == 0 then
    return 0
  end
  local index = clamp(cursor, 0, #chars - 1)
  if not is_space(chars[index + 1]) then
    while index + 1 < #chars and not is_space(chars[index + 2]) do
      index = index + 1
    end
    return index
  end
  while index < #chars and is_space(chars[index + 1]) do
    index = index + 1
  end
  if index >= #chars then
    return #chars - 1
  end
  while index + 1 < #chars and not is_space(chars[index + 2]) do
    index = index + 1
  end
  return index
end

local function previous_word_end(chars, cursor)
  if #chars == 0 then
    return 0
  end
  local index = clamp(cursor - 1, 0, #chars - 1)
  while index > 0 and is_space(chars[index + 1]) do
    index = index - 1
  end
  while index > 0 and not is_space(chars[index]) do
    index = index - 1
  end
  return word_end(chars, index)
end

local function push_undo(chars, cursor)
  undo[#undo + 1] = { chars = copy_chars(chars), cursor = cursor }
  if #undo > undo_limit then
    table.remove(undo, 1)
  end
end

local function clear_redo()
  redo = {}
end

local function ordered_range(start_pos, end_pos)
  if start_pos <= end_pos then
    return start_pos, end_pos
  end
  return end_pos, start_pos
end

local function slice(chars, start_pos, end_pos)
  local out = {}
  for index = start_pos + 1, end_pos do
    out[#out + 1] = chars[index]
  end
  return out
end

local function delete_range(chars, cursor, start_pos, end_pos)
  start_pos, end_pos = ordered_range(start_pos, end_pos)
  start_pos = clamp(start_pos, 0, #chars)
  end_pos = clamp(end_pos, 0, #chars)
  if start_pos >= end_pos then
    return chars, cursor
  end

  push_undo(chars, cursor)
  register = slice(chars, start_pos, end_pos)

  local next_chars = {}
  for index = 1, start_pos do
    next_chars[#next_chars + 1] = chars[index]
  end
  for index = end_pos + 1, #chars do
    next_chars[#next_chars + 1] = chars[index]
  end

  local next_cursor = 0
  if #next_chars > 0 then
    if mode == "insert" then
      next_cursor = clamp(start_pos, 0, #next_chars)
    else
      next_cursor = clamp(start_pos, 0, #next_chars - 1)
    end
  end
  clear_redo()

  return next_chars, next_cursor
end

local function enter_insert()
  pending = ""
  mode = "insert"
end

local function clamp_normal_cursor(chars, cursor)
  if #chars == 0 then
    return 0
  end
  return clamp(cursor, 0, #chars - 1)
end

local function enter_normal(chars, cursor)
  pending = ""
  mode = "normal"
  if cursor > 0 then
    cursor = cursor - 1
  end
  return clamp_normal_cursor(chars, cursor)
end

local function move_after_cursor(chars, cursor)
  if #chars == 0 then
    return 0
  end
  return clamp(cursor + 1, 0, #chars)
end

local function move_left(chars, cursor)
  if cursor > current_line_start(chars, cursor) then
    return cursor - 1
  end
  return cursor
end

local function move_right(chars, cursor)
  local line_start = current_line_start(chars, cursor)
  local line_end = current_line_end(chars, cursor)
  if line_end > line_start then
    line_end = line_end - 1
  end
  if cursor < line_end then
    return cursor + 1
  end
  return cursor
end

local function move_line_last_char(chars, cursor)
  local line_start = current_line_start(chars, cursor)
  local line_end = current_line_end(chars, cursor)
  if line_end > line_start then
    return line_end - 1
  end
  return line_start
end

local function move_buffer_end(chars)
  if #chars == 0 then
    return 0
  end
  return #chars - 1
end

local function move_word_forward(chars, cursor)
  return clamp(word_right(chars, cursor), 0, math.max(0, #chars - 1))
end

local function move_raw_line(chars, cursor, delta)
  local start = current_line_start(chars, cursor)
  local column = cursor - start
  local target_start = 0
  if delta > 0 then
    local line_end = current_line_end(chars, cursor)
    if line_end >= #chars then
      return cursor
    end
    target_start = line_end + 1
  else
    if start == 0 then
      return cursor
    end
    target_start = previous_line_start(chars, start)
  end

  local target_end = line_end_from(chars, target_start)
  if target_end > target_start then
    target_end = target_end - 1
  end
  return math.min(target_start + column, target_end)
end

local function replace_char(chars, cursor, key)
  pending = ""
  if #key ~= 1 or #chars == 0 or cursor >= #chars then
    return chars, cursor
  end
  push_undo(chars, cursor)
  chars[cursor + 1] = key
  clear_redo()
  return chars, cursor
end

local function open_line_below(chars, cursor)
  push_undo(chars, cursor)
  local insert_at = current_line_end(chars, cursor)
  table.insert(chars, insert_at + 1, "\n")
  clear_redo()
  enter_insert()
  return chars, insert_at + 1
end

local function open_line_above(chars, cursor)
  push_undo(chars, cursor)
  local insert_at = current_line_start(chars, cursor)
  table.insert(chars, insert_at + 1, "\n")
  clear_redo()
  enter_insert()
  return chars, insert_at
end

local function current_line_delete_range(chars, cursor)
  local start = current_line_start(chars, cursor)
  local line_end = current_line_end(chars, cursor)
  if line_end < #chars then
    return start, line_end + 1
  end
  if start > 0 then
    return start - 1, line_end
  end
  return start, line_end
end

local function current_line_yank_range(chars, cursor)
  local start = current_line_start(chars, cursor)
  local line_end = current_line_end(chars, cursor)
  if line_end < #chars then
    line_end = line_end + 1
  end
  return start, line_end
end

local function change_current_line(chars, cursor)
  local start = current_line_start(chars, cursor)
  local line_end = current_line_end(chars, cursor)
  chars, cursor = delete_range(chars, cursor, start, line_end)
  enter_insert()
  return chars, cursor
end

local function paste(chars, cursor, after)
  if #register == 0 then
    return chars, cursor
  end
  push_undo(chars, cursor)

  local insert_at = cursor
  if after and #chars > 0 then
    insert_at = insert_at + 1
  end
  insert_at = clamp(insert_at, 0, #chars)

  local insert_chars = copy_chars(register)
  for index = #insert_chars, 1, -1 do
    table.insert(chars, insert_at + 1, insert_chars[index])
  end
  if #insert_chars > 0 then
    cursor = insert_at + #insert_chars - 1
  end
  clear_redo()
  return chars, cursor
end

local function apply_history(chars, cursor, source, destination)
  if #source == 0 then
    return chars, cursor
  end
  destination[#destination + 1] = { chars = copy_chars(chars), cursor = cursor }
  local snapshot = table.remove(source)
  chars = copy_chars(snapshot.chars)
  cursor = clamp_normal_cursor(chars, math.min(snapshot.cursor, #chars))
  return chars, cursor
end

local function undo_edit(chars, cursor)
  chars, cursor = apply_history(chars, cursor, undo, redo)
  return chars, cursor
end

local function redo_edit(chars, cursor)
  chars, cursor = apply_history(chars, cursor, redo, undo)
  return chars, cursor
end

local function motion_range(chars, cursor, key)
  if key == "h" or key == "left" then
    return cursor - 1, cursor, cursor > 0
  end
  if key == "l" or key == "right" then
    return cursor, cursor + 1, cursor < #chars
  end
  if key == "w" then
    return cursor, word_right(chars, cursor), true
  end
  if key == "b" then
    return word_left(chars, cursor), cursor, true
  end
  if key == "e" then
    return cursor, word_end(chars, cursor) + 1, true
  end
  if key == "0" or key == "home" then
    return current_line_start(chars, cursor), cursor, true
  end
  if key == "^" then
    return line_first_non_blank(chars, cursor), cursor, true
  end
  if key == "$" or key == "end" then
    return cursor, current_line_end(chars, cursor), true
  end
  return nil, nil, false
end

local function apply_line_operator(chars, cursor, operator)
  if operator == "d" then
    local start, line_end = current_line_delete_range(chars, cursor)
    return delete_range(chars, cursor, start, line_end)
  end
  if operator == "c" then
    return change_current_line(chars, cursor)
  end
  if operator == "y" then
    local start, line_end = current_line_yank_range(chars, cursor)
    register = slice(chars, start, line_end)
  end
  return chars, cursor
end

local function apply_range_operator(chars, cursor, operator, start, line_end)
  if operator == "d" then
    return delete_range(chars, cursor, start, line_end)
  end
  if operator == "c" then
    chars, cursor = delete_range(chars, cursor, start, line_end)
    enter_insert()
    return chars, cursor
  end
  if operator == "y" then
    start, line_end = ordered_range(start, line_end)
    register = slice(chars, start, line_end)
  end
  return chars, cursor
end

local function handle_operator(chars, cursor, operator, key)
  pending = ""
  if key == operator then
    return apply_line_operator(chars, cursor, operator)
  end
  local start, line_end, ok = motion_range(chars, cursor, key)
  if not ok then
    return chars, cursor
  end
  return apply_range_operator(chars, cursor, operator, start, line_end)
end

local function handle_g(chars, cursor, key)
  pending = ""
  if key == "g" then
    return chars, 0
  end
  if key == "e" then
    return chars, previous_word_end(chars, cursor)
  end
  return chars, cursor
end

local function handle_pending(chars, cursor, key)
  if pending == "d" or pending == "c" or pending == "y" then
    return handle_operator(chars, cursor, pending, key)
  end
  if pending == "g" then
    return handle_g(chars, cursor, key)
  end
  if pending == "r" then
    return replace_char(chars, cursor, key)
  end
  pending = ""
  return chars, cursor
end

local function is_pending_command(key)
  return key == "g" or key == "d" or key == "c" or key == "y" or key == "r"
end

local function handle_normal_command(chars, cursor, key)
  if is_pending_command(key) then
    pending = key
    return chars, cursor
  end
  if key == "i" then
    enter_insert()
  elseif key == "a" then
    cursor = move_after_cursor(chars, cursor)
    enter_insert()
  elseif key == "I" then
    cursor = line_first_non_blank(chars, cursor)
    enter_insert()
  elseif key == "A" then
    cursor = current_line_end(chars, cursor)
    enter_insert()
  elseif key == "o" then
    chars, cursor = open_line_below(chars, cursor)
  elseif key == "O" then
    chars, cursor = open_line_above(chars, cursor)
  elseif key == "h" or key == "left" then
    cursor = move_left(chars, cursor)
  elseif key == "l" or key == "right" then
    cursor = move_right(chars, cursor)
  elseif key == "j" or key == "down" then
    cursor = move_raw_line(chars, cursor, 1)
  elseif key == "k" or key == "up" then
    cursor = move_raw_line(chars, cursor, -1)
  elseif key == "0" or key == "home" then
    cursor = current_line_start(chars, cursor)
  elseif key == "^" then
    cursor = line_first_non_blank(chars, cursor)
  elseif key == "$" or key == "end" then
    cursor = move_line_last_char(chars, cursor)
  elseif key == "w" then
    cursor = move_word_forward(chars, cursor)
  elseif key == "b" then
    cursor = word_left(chars, cursor)
  elseif key == "e" then
    cursor = word_end(chars, cursor)
  elseif key == "G" then
    cursor = move_buffer_end(chars)
  elseif key == "x" then
    chars, cursor = delete_range(chars, cursor, cursor, cursor + 1)
  elseif key == "X" then
    chars, cursor = delete_range(chars, cursor, cursor - 1, cursor)
  elseif key == "D" then
    chars, cursor = delete_range(chars, cursor, cursor, current_line_end(chars, cursor))
  elseif key == "C" then
    chars, cursor = delete_range(chars, cursor, cursor, current_line_end(chars, cursor))
    enter_insert()
  elseif key == "S" then
    chars, cursor = change_current_line(chars, cursor)
  elseif key == "p" then
    chars, cursor = paste(chars, cursor, true)
  elseif key == "P" then
    chars, cursor = paste(chars, cursor, false)
  elseif key == "u" then
    chars, cursor = undo_edit(chars, cursor)
  elseif key == "ctrl+r" then
    chars, cursor = redo_edit(chars, cursor)
  end
  return chars, cursor
end

local function action_result(name)
  return {
    handled = true,
    action = name,
  }
end

local function sync_state(state, chars, cursor)
  if chars ~= nil then
    state.chars = chars
    state.text = join(chars)
  end
  if cursor ~= nil then
    state.cursor = cursor
  end
  state.metadata = state.metadata or {}
  state.metadata.mode = mode
  state.label = label()
  return state
end

local function insert_text(chars, cursor, text)
  if text == nil or text == "" then
    return chars, cursor
  end
  push_undo(chars, cursor)
  local insert_at = clamp(cursor, 0, #chars)
  local inserted = utf8_chars(text)
  for index = #inserted, 1, -1 do
    table.insert(chars, insert_at + 1, inserted[index])
  end
  clear_redo()
  return chars, insert_at + #inserted
end

local function delete_backward(chars, cursor)
  if cursor <= 0 then
    return chars, cursor
  end
  return delete_range(chars, cursor, cursor - 1, cursor)
end

local function delete_forward(chars, cursor)
  if cursor >= #chars then
    return chars, cursor
  end
  return delete_range(chars, cursor, cursor, cursor + 1)
end

local function has_multiline(chars)
  for i = 1, #chars do
    if chars[i] == "\n" then
      return true
    end
  end
  return false
end

local function handle_insert_key(event, chars, cursor)
  if event.alt and event.key == "enter" then
    return action_result("followup.queue")
  end
  if event.alt and event.key == "up" then
    return action_result("followup.dequeue")
  end
  if event.key == "escape" then
    if event.working or event.auth_working then
      return action_result("interrupt")
    end
    cursor = enter_normal(chars, cursor)
    return result(true, chars, cursor)
  end
  if event.shift and event.key == "enter" then
    chars, cursor = insert_text(chars, cursor, "\n")
    return result(true, chars, cursor)
  end
  if event.key == "enter" then
    return action_result("submit")
  end
  if event.key == "tab" then
    return action_result("autocomplete.accept")
  end
  if event.key == "up" then
    if has_multiline(chars) then
      return result(true, chars, move_raw_line(chars, cursor, -1))
    end
    return action_result("history.prev")
  end
  if event.key == "down" then
    if has_multiline(chars) then
      return result(true, chars, move_raw_line(chars, cursor, 1))
    end
    return action_result("history.next")
  end
  if event.key == "left" or event.key == "ctrl+b" then
    return result(true, chars, clamp(cursor - 1, 0, #chars))
  end
  if event.key == "right" or event.key == "ctrl+f" then
    return result(true, chars, clamp(cursor + 1, 0, #chars))
  end
  if event.key == "home" or event.key == "ctrl+a" then
    return result(true, chars, current_line_start(chars, cursor))
  end
  if event.key == "end" or event.key == "ctrl+e" then
    return result(true, chars, current_line_end(chars, cursor))
  end
  if event.key == "backspace" then
    chars, cursor = delete_backward(chars, cursor)
    return result(true, chars, cursor)
  end
  if event.key == "delete" then
    chars, cursor = delete_forward(chars, cursor)
    return result(true, chars, cursor)
  end
  if event.key == "ctrl+w" then
    chars, cursor = delete_range(chars, cursor, word_left(chars, cursor), cursor)
    return result(true, chars, cursor)
  end
  if event.key == "ctrl+u" then
    chars, cursor = delete_range(chars, cursor, current_line_start(chars, cursor), cursor)
    return result(true, chars, cursor)
  end
  if event.key == "ctrl+k" then
    chars, cursor = delete_range(chars, cursor, cursor, current_line_end(chars, cursor))
    return result(true, chars, cursor)
  end
  if event.text ~= "" and not event.ctrl and not event.alt then
    chars, cursor = insert_text(chars, cursor, event.text)
    return result(true, chars, cursor)
  end

  return { handled = false }
end

local function on_key(event, state)
  local chars = copy_chars(state.chars)
  local cursor = clamp(state.cursor, 0, #chars)

  if mode == "insert" then
    return handle_insert_key(event, chars, cursor)
  end

  if event.key == "enter" then
    if pending ~= "" then
      pending = ""
      return result(true, chars, cursor)
    end
    return action_result("submit")
  end

  if event.key == "escape" then
    if pending ~= "" then
      pending = ""
      return result(true, chars, cursor)
    end
    if event.working or event.auth_working then
      return action_result("interrupt")
    end
    mode = "normal"
    cursor = clamp_normal_cursor(chars, cursor)
    return result(true, chars, cursor)
  end

  if pending ~= "" then
    chars, cursor = handle_pending(chars, cursor, event.key)
    return result(true, chars, cursor)
  end

  chars, cursor = handle_normal_command(chars, cursor, event.key)
  return result(true, chars, cursor)
end

local function composer_window_name()
  return librecode.win.find({ role = "composer" })
end

local function composer_window_buffer()
  local win = composer_window_name()
  if win == nil then
    return "composer"
  end
  local buf = librecode.win.get_buf(win)
  if buf == nil or buf == "" then
    return "composer"
  end
  return buf
end

local function rune_count(text)
  if text == nil or text == "" then
    return 0
  end
  return #utf8_chars(text)
end

local function rune_slice(text, width)
  if width <= 0 then
    return ""
  end
  local chars = utf8_chars(text)
  local out = {}
  for index = 1, math.min(width, #chars) do
    out[index] = chars[index]
  end
  return table.concat(out, "")
end

local function pad_right(text, width)
  local length = rune_count(text)
  if length >= width then
    return rune_slice(text, width)
  end
  return text .. string.rep(" ", width - length)
end

local function split_render_lines(chars, inner_width, cursor)
  local lines = {}
  local current = ""
  local cursor_row = 0
  local cursor_col = 0
  local row = 0
  local col = 0
  local saw_cursor = false

  for index = 0, #chars - 1 do
    if index == cursor then
      cursor_row = row
      cursor_col = col
      saw_cursor = true
    end

    local char = chars[index + 1]
    if char == "\n" then
      lines[#lines + 1] = current
      current = ""
      row = row + 1
      col = 0
    else
      if col >= inner_width then
        lines[#lines + 1] = current
        current = ""
        row = row + 1
        col = 0
      end
      current = current .. char
      col = col + 1
    end
  end

  if not saw_cursor then
    cursor_row = row
    cursor_col = col
  end
  lines[#lines + 1] = current
  if #lines == 0 then
    lines[1] = ""
  end

  return lines, cursor_row, cursor_col
end

local function top_border(width)
  if width <= 1 then
    return ""
  end
  local inner = math.max(1, width - 2)
  local suffix = rune_slice(label() .. "──", inner)
  return "╭" .. string.rep("─", math.max(0, inner - rune_count(suffix))) .. suffix .. "╮"
end

local function bottom_border(width)
  if width <= 1 then
    return ""
  end
  return "╰" .. string.rep("─", math.max(1, width - 2)) .. "╯"
end

local function render_composer_window()
  local win_name = composer_window_name()
  if win_name == nil then
    return
  end
  local win = librecode.win.get(win_name)
  if win == nil or not win.visible or win.width <= 0 or win.height <= 0 then
    return
  end
  local buffer_name = librecode.win.get_buf(win_name)
  if buffer_name == nil or buffer_name == "" then
    return
  end
  local state = librecode.buf.get(buffer_name)
  local chars = copy_chars(state.chars)
  local inner_width = math.max(1, win.width - 4)
  local body_rows = math.max(1, win.height - 2)
  local lines, cursor_row, cursor_col = split_render_lines(chars, inner_width, state.cursor)
  local start = math.max(0, cursor_row - body_rows + 1)
  if start + body_rows > #lines then
    start = math.max(0, #lines - body_rows)
  end

  win.renderer = "extension"
  librecode.win.set(win_name, win)
  librecode.ui.clear_window(win_name)
  librecode.ui.draw_text(win_name, 0, 0, top_border(win.width), { fg = "border" })
  for row = 1, body_rows do
    local line = lines[start + row] or ""
    librecode.ui.draw_text(win_name, row, 0, "│", { fg = "border" })
    librecode.ui.draw_text(win_name, row, 1, " " .. pad_right(line, inner_width) .. " ", { fg = "text" })
    librecode.ui.draw_text(win_name, row, win.width - 1, "│", { fg = "border" })
  end
  if win.height >= 2 then
    librecode.ui.draw_text(win_name, win.height - 1, 0, bottom_border(win.width), { fg = "border" })
  end
  librecode.ui.set_cursor(win_name, 1 + cursor_row - start, 2 + cursor_col)
end

librecode.on("startup", function()
  mode = "insert"
  pending = ""
  register = {}
  undo = {}
  redo = {}

  local buffer_name = composer_window_buffer()
  local state = librecode.buf.get(buffer_name)
  librecode.buf.set(buffer_name, sync_state(state, state.chars, state.cursor))
end)

librecode.on("render", function()
  render_composer_window()
end)

librecode.keymap.set({ role = "composer" }, "*", function(event)
  local buffer_name = composer_window_buffer()
  local state = librecode.buf.get(buffer_name)
  local outcome = on_key(event, state)

  if outcome.action ~= nil and outcome.action ~= "" then
    librecode.action.run(outcome.action)
  end

  state = sync_state(state, outcome.chars, outcome.cursor)
  librecode.buf.set(buffer_name, state)
  if outcome.handled then
    librecode.event.consume()
  end
end, { priority = 100, desc = "vim composer dispatcher" })
