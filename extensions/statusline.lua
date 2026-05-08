local lc = require("librecode")
local statusline = require("librecode.statusline")

lc.on("render", { priority = -100 }, function(event)
  local win_name = lc.win.find({ role = "status" })
  if win_name == nil then
    return
  end

  local win = lc.win.get(win_name)
  if win == nil or not win.visible or win.width <= 0 or win.height <= 0 then
    return
  end

  win.renderer = "extension"
  lc.win.set(win_name, win)
  lc.ui.clear_window(win_name)

  local lines = statusline.lines(event)
  local max_lines = math.min(#lines, win.height)
  for row = 1, max_lines do
    lc.ui.draw_text(
      win_name,
      row - 1,
      0,
      statusline.truncate(lines[row], win.width),
      { fg = "dim" }
    )
  end
end)
