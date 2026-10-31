-- Keyboard + rear light RGB (TrueSense). Click opens the panel · Middle click: keyboard on/off · Scroll: keyboard brightness
-- IPC: noctalia msg plugin YOUR-PLUGIN-ID:rgb all <kb-on|kb-off|rear-on|rear-off>
local PANEL = "YOUR-PLUGIN-ID:rgb_panel"
local level = 25

local function run(cmd) noctalia.runAsync(cmd) end

function update()
  noctalia.setUpdateInterval(60000)
  barWidget.setGlyph("keyboard") barWidget.setText("RGB")
  barWidget.setColor("on_surface_variant") barWidget.setGlyphColor("tertiary")
  barWidget.setTooltip({ { "TrueSense lighting", "keyboard + rear light" }, { "Click", "RGB panel" }, { "Scroll", "keyboard brightness" }, { "Middle click", "keyboard on/off" } })
end

function onScroll(axis, steps)
  if axis ~= "vertical" or steps == 0 then return end
  level = math.max(0, math.min(50, level + (steps > 0 and -5 or 5)))
  run("truesense keyboard brightness " .. level)
  run("noctalia msg osd-show keyboard 'Keyboard brightness: " .. level .. "/50'")
end

function onClick() noctalia.togglePanel(PANEL) end
function onRightClick() noctalia.togglePanel(PANEL) end
function onMiddleClick() run("truesense keyboard toggle") end
function onIpc(event)
  if event == "kb-on" then run("truesense keyboard on")
  elseif event == "kb-off" then run("truesense keyboard off")
  elseif event == "rear-on" then run("truesense rear on")
  elseif event == "rear-off" then run("truesense rear off") end
end
update()
