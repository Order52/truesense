-- RGB panel (TrueSense): keyboard looks, per-key colours, and the rear light with its three parts.
local KB = { { "static", "Static" }, { "breathing", "Breathe" }, { "wave", "Wave" }, { "snake", "Snake" }, { "ripple", "Ripple" },
  { "neon", "Neon" }, { "raindrop", "Rain" }, { "lightning", "Bolt" }, { "spot", "Spot" }, { "star", "Stars" },
  { "fireball", "Fire" }, { "snow", "Snow" }, { "heartbeat", "Pulse" } }
local REAR = { { "static", "Static" }, { "breathing", "Breathe" }, { "neon", "Neon" }, { "wave", "Wave" }, { "twinkling", "Twinkle" } }
local GROUPS = { { "all", "All keys" }, { "wasd", "WASD" }, { "arrows", "Arrows" }, { "fkeys", "F1-F12" }, { "numrow", "1-0" },
  { "letters", "Letters" }, { "numpad", "Numpad" }, { "esc", "Esc" }, { "space", "Space" }, { "enter", "Enter" }, { "predator", "PREDATOR" }, { "lshift", "Shift" } }
local KEYROWS = {
  { { "esc", "Esc" }, { "f1", "F1" }, { "f2", "F2" }, { "f3", "F3" }, { "f4", "F4" }, { "f5", "F5" }, { "f6", "F6" }, { "f7", "F7" }, { "f8", "F8" }, { "f9", "F9" }, { "f10", "F10" }, { "f11", "F11" }, { "f12", "F12" } },
  { { "1", "1" }, { "2", "2" }, { "3", "3" }, { "4", "4" }, { "5", "5" }, { "6", "6" }, { "7", "7" }, { "8", "8" }, { "9", "9" }, { "0", "0" }, { "backspace", "Bksp" } },
  { { "tab", "Tab" }, { "q", "Q" }, { "w", "W" }, { "e", "E" }, { "r", "R" }, { "t", "T" }, { "y", "Y" }, { "u", "U" }, { "i", "I" }, { "o", "O" }, { "p", "P" } },
  { { "caps", "Caps" }, { "a", "A" }, { "s", "S" }, { "d", "D" }, { "f", "F" }, { "g", "G" }, { "h", "H" }, { "j", "J" }, { "k", "K" }, { "l", "L" }, { "enter", "Enter" } },
  { { "lshift", "Shift" }, { "z", "Z" }, { "x", "X" }, { "c", "C" }, { "v", "V" }, { "b", "B" }, { "n", "N" }, { "m", "M" }, { "rshift", "Shift" } },
  { { "ctrl", "Ctrl" }, { "win", "Win" }, { "lalt", "Alt" }, { "space", "Space" }, { "ralt", "Alt" }, { "left", "←" }, { "up", "↑" }, { "down", "↓" }, { "right", "→" } },
}
local ZONES = { { "all", "All" }, { "left", "Left" }, { "middle", "Middle" }, { "right", "Right" } }
local SW = { "#FF0000", "#FF6A00", "#FFD000", "#00FF55", "#00FFD0", "#00AAFF", "#3A3AFF", "#B400FF", "#FF00AA", "#FFFFFF" }
local st = { tab = "kb", hex = "00AAFF", level = 25, kspeed = 5, target = "wasd", khex = "FF0000",
             rear = "static", rhex = "00AEC7", rlevel = 100, speed = 3, zone = 1, z = { "00AEC7", "00AEC7", "00AEC7" } }

local function run(cmd) noctalia.runAsync(cmd) end

-- Live updates while a colour or slider is being dragged: one command at a time, and when it finishes the newest value is sent
-- (so the keyboard follows the drag without a backlog of processes).
local busy = {}
local function live(key, build)
  if busy[key] then busy[key] = "again"; return end
  busy[key] = true
  noctalia.runStream(build() .. "; echo done", function()
    local again = busy[key] == "again"; busy[key] = nil
    if again then live(key, build) end
  end)
end
local function clean(h) return (tostring(h):gsub("[^%x]", ""):sub(1, 6)) end

local function tabs()
  local function tab(id, glyph, text) return ui.button({ glyph = glyph, text = text, flexGrow = 1, controlSize = "sm", variant = st.tab == id and "default" or "ghost", onClick = "onTab_" .. id }) end
  return ui.row({ gap = 4 }, { tab("kb", "keyboard", "Looks"), tab("keys", "palette", "Per key"), tab("rear", "bulb", "Rear") })
end

local function chips(list, prefix, selected)
  local rows, row = {}, {}
  for i, e in ipairs(list) do
    row[#row + 1] = ui.button({ text = e[2], controlSize = "sm", flexGrow = 1, variant = (selected == i) and "default" or "ghost", onClick = prefix .. i })
    if #row == 4 or i == #list then rows[#rows + 1] = ui.row({ gap = 4 }, row); row = {} end
  end
  return ui.column({ gap = 4 }, rows)
end

local function cap(t) return ui.label({ text = t, fontSize = 12, color = "on_surface_variant" }) end

local function rearCmd()
  local zs = st.z[1] .. "," .. st.z[2] .. "," .. st.z[3]
  if st.rear == "static" then return "truesense rear set static --zones " .. zs .. " --brightness " .. st.rlevel .. " --speed " .. st.speed end
  return "truesense rear set " .. st.rear .. " --color " .. st.rhex .. " --brightness " .. st.rlevel .. " --speed " .. st.speed
end
local function rearApply() live("rear", rearCmd) end

function render()
  local body
  if st.tab == "kb" then
    body = {
      tabs(),
      ui.row({ gap = 4 }, {
        ui.button({ glyph = "power", text = "On", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onKbOn" }),
        ui.button({ glyph = "power-off", text = "Off", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onKbOff" }),
        ui.button({ glyph = "refresh", text = "Stock", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onKbRestore" }),
      }),
      cap("Effect"),
      chips(KB, "onKb", nil),
      cap("Colour of the current effect (stays in the same effect)"),
      ui.colorPicker({ value = "#" .. st.hex, swatches = SW, onChange = "onKbColorLive", onChangeEnd = "onKbColor" }),
      cap("Brightness  " .. st.level .. " / 50  (Fn+F11 / Fn+F12 also work)"),
      ui.slider({ min = 0, max = 50, step = 5, value = st.level, controlSize = "md", onChange = "onKbLevel", onDragEnd = "onKbLevelEnd" }),
      cap("Effect speed  " .. st.kspeed .. " / 11"),
      ui.slider({ min = 1, max = 11, step = 1, value = st.kspeed, controlSize = "md", onChange = "onKbSpeed", onDragEnd = "onKbSpeedEnd" }),
      ui.row({ gap = 4 }, {
        ui.button({ text = "Fill all keys", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onKbFill" }),
        ui.button({ text = "Rainbow", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onKbRainbow" }),
      }),
    }
  elseif st.tab == "keys" then
    body = {
      tabs(),
      cap("Pick keys on a picture of the keyboard, then choose their colour."),
      ui.button({ glyph = "keyboard", text = "Open the keyboard editor", flexGrow = 1, variant = "default", onClick = "onEditor" }),
      cap("Quick looks"),
      ui.row({ gap = 4 }, {
        ui.button({ text = "Rainbow", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onKbRainbow" }),
        ui.button({ text = "Fill with colour", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onKbFill" }),
        ui.button({ text = "Stock", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onKbRestore" }),
      }),
      ui.colorPicker({ value = "#" .. st.hex, swatches = SW, onChange = "onKbColorLive", onChangeEnd = "onKbColor" }),
    }
  else
    body = {
      tabs(),
      ui.row({ gap = 4 }, {
        ui.button({ glyph = "bulb", text = "On", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onRearOn" }),
        ui.button({ glyph = "bulb-off", text = "Off", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onRearOff" }),
      }),
      cap("Effect"),
      chips(REAR, "onRear", nil),
      cap(st.rear == "static" and "Part of the light bar (static can colour left, middle and right separately)" or "Colour (one colour for the whole bar)"),
    }
    if st.rear == "static" then body[#body + 1] = chips(ZONES, "onZone", st.zone) end
    body[#body + 1] = ui.colorPicker({ value = "#" .. (st.rear == "static" and (st.zone == 1 and st.z[1] or st.z[st.zone - 1]) or st.rhex), swatches = SW, onChange = "onRearColorLive", onChangeEnd = "onRearColor" })
    body[#body + 1] = cap("Speed  " .. st.speed .. " / 5")
    body[#body + 1] = ui.slider({ min = 1, max = 5, step = 1, value = st.speed, controlSize = "md", onChange = "onRearSpeed", onDragEnd = "onRearSpeedEnd" })
    body[#body + 1] = cap("Brightness  " .. st.rlevel .. " %")
    body[#body + 1] = ui.slider({ min = 10, max = 100, step = 10, value = st.rlevel, controlSize = "md", onChange = "onRearLevel", onDragEnd = "onRearLevelEnd" })
  end
  panel.render(ui.column({ gap = 10, padding = 16 }, body))
end

function onTab_kb() st.tab = "kb"; render() end
function onTab_keys() st.tab = "keys"; render() end
function onTab_rear() st.tab = "rear"; render() end

-- Looks
function onKbOn() run("truesense keyboard on") end
function onKbOff() run("truesense keyboard off") end
function onKbRestore() run("truesense keyboard restore") end
local function kbEffect(i) run("truesense keyboard effect " .. KB[i][1] .. " --color " .. st.hex .. " --speed " .. st.kspeed .. " --brightness " .. math.max(st.level, 10)) end
function onKb1() kbEffect(1) end function onKb2() kbEffect(2) end function onKb3() kbEffect(3) end function onKb4() kbEffect(4) end
function onKb5() kbEffect(5) end function onKb6() kbEffect(6) end function onKb7() kbEffect(7) end function onKb8() kbEffect(8) end
function onKb9() kbEffect(9) end function onKb10() kbEffect(10) end function onKb11() kbEffect(11) end function onKb12() kbEffect(12) end
function onKb13() kbEffect(13) end
function onKbSpeed(v) v = tonumber(v); if v then st.kspeed = math.floor(v); live("kbspeed", function() return "truesense keyboard speed " .. st.kspeed end) end end
function onKbSpeedEnd(v) st.kspeed = math.floor(tonumber(v) or st.kspeed); live("kbspeed", function() return "truesense keyboard speed " .. st.kspeed end); render() end
function onKbColorLive(v) st.hex = clean(v); live("kbcolor", function() return "truesense keyboard color " .. st.hex end) end
function onKbColor(v) st.hex = clean(v); live("kbcolor", function() return "truesense keyboard color " .. st.hex end) end
function onKbFill() run("truesense keyboard fill " .. st.hex) end
function onKbRainbow() run("truesense keyboard rainbow") end
function onKbLevel(v) v = tonumber(v); if v then st.level = math.floor(v); live("kblevel", function() return "truesense keyboard brightness " .. st.level end) end end
function onKbLevelEnd(v) st.level = math.floor(tonumber(v) or st.level); live("kblevel", function() return "truesense keyboard brightness " .. st.level end); render() end

-- Per key
local function paint(hex) run("truesense keyboard paint " .. st.target .. "=" .. hex) end
local function group(i) st.target = GROUPS[i][1]; render() end
function onGroup1() group(1) end function onGroup2() group(2) end function onGroup3() group(3) end function onGroup4() group(4) end
function onGroup5() group(5) end function onGroup6() group(6) end function onGroup7() group(7) end function onGroup8() group(8) end
function onGroup9() group(9) end function onGroup10() group(10) end function onGroup11() group(11) end function onGroup12() group(12) end
function onKey_esc() st.target = "esc"; render() end
function onKey_f1() st.target = "f1"; render() end
function onKey_f2() st.target = "f2"; render() end
function onKey_f3() st.target = "f3"; render() end
function onKey_f4() st.target = "f4"; render() end
function onKey_f5() st.target = "f5"; render() end
function onKey_f6() st.target = "f6"; render() end
function onKey_f7() st.target = "f7"; render() end
function onKey_f8() st.target = "f8"; render() end
function onKey_f9() st.target = "f9"; render() end
function onKey_f10() st.target = "f10"; render() end
function onKey_f11() st.target = "f11"; render() end
function onKey_f12() st.target = "f12"; render() end
function onKey_1() st.target = "1"; render() end
function onKey_2() st.target = "2"; render() end
function onKey_3() st.target = "3"; render() end
function onKey_4() st.target = "4"; render() end
function onKey_5() st.target = "5"; render() end
function onKey_6() st.target = "6"; render() end
function onKey_7() st.target = "7"; render() end
function onKey_8() st.target = "8"; render() end
function onKey_9() st.target = "9"; render() end
function onKey_0() st.target = "0"; render() end
function onKey_backspace() st.target = "backspace"; render() end
function onKey_tab() st.target = "tab"; render() end
function onKey_q() st.target = "q"; render() end
function onKey_w() st.target = "w"; render() end
function onKey_e() st.target = "e"; render() end
function onKey_r() st.target = "r"; render() end
function onKey_t() st.target = "t"; render() end
function onKey_y() st.target = "y"; render() end
function onKey_u() st.target = "u"; render() end
function onKey_i() st.target = "i"; render() end
function onKey_o() st.target = "o"; render() end
function onKey_p() st.target = "p"; render() end
function onKey_caps() st.target = "caps"; render() end
function onKey_a() st.target = "a"; render() end
function onKey_s() st.target = "s"; render() end
function onKey_d() st.target = "d"; render() end
function onKey_f() st.target = "f"; render() end
function onKey_g() st.target = "g"; render() end
function onKey_h() st.target = "h"; render() end
function onKey_j() st.target = "j"; render() end
function onKey_k() st.target = "k"; render() end
function onKey_l() st.target = "l"; render() end
function onKey_enter() st.target = "enter"; render() end
function onKey_lshift() st.target = "lshift"; render() end
function onKey_z() st.target = "z"; render() end
function onKey_x() st.target = "x"; render() end
function onKey_c() st.target = "c"; render() end
function onKey_v() st.target = "v"; render() end
function onKey_b() st.target = "b"; render() end
function onKey_n() st.target = "n"; render() end
function onKey_m() st.target = "m"; render() end
function onKey_rshift() st.target = "rshift"; render() end
function onKey_ctrl() st.target = "ctrl"; render() end
function onKey_win() st.target = "win"; render() end
function onKey_lalt() st.target = "lalt"; render() end
function onKey_space() st.target = "space"; render() end
function onKey_ralt() st.target = "ralt"; render() end
function onKey_left() st.target = "left"; render() end
function onKey_up() st.target = "up"; render() end
function onKey_down() st.target = "down"; render() end
function onKey_right() st.target = "right"; render() end
function onPaintLive(v) st.khex = clean(v); live("paint", function() return "truesense keyboard paint " .. st.target .. "=" .. st.khex end) end
function onPaint(v) st.khex = clean(v); live("paint", function() return "truesense keyboard paint " .. st.target .. "=" .. st.khex end) end
function onPaintNow() paint(st.khex) end
function onPaintBlack() paint("000000") end

-- Rear light
function onRearOn() run("truesense rear on") end
function onRearOff() run("truesense rear off") end
local function rearEffect(i) st.rear = REAR[i][1]; rearApply(); render() end
function onRear1() rearEffect(1) end function onRear2() rearEffect(2) end function onRear3() rearEffect(3) end
function onRear4() rearEffect(4) end function onRear5() rearEffect(5) end
local function zone(i) st.zone = i; render() end
function onZone1() zone(1) end function onZone2() zone(2) end function onZone3() zone(3) end function onZone4() zone(4) end
local function setRearColor(h)
  st.rhex = h
  if st.zone == 1 then st.z = { h, h, h } else st.z[st.zone - 1] = h end
end
function onRearColorLive(v) setRearColor(clean(v)); rearApply() end
function onRearColor(v) setRearColor(clean(v)); rearApply() end
function onRearSpeed(v) v = tonumber(v); if v then st.speed = math.floor(v); rearApply() end end
function onRearSpeedEnd(v) st.speed = math.floor(tonumber(v) or st.speed); rearApply(); render() end
function onRearLevel(v) v = tonumber(v); if v then st.rlevel = math.floor(v); rearApply() end end
function onRearLevelEnd(v) st.rlevel = math.floor(tonumber(v) or st.rlevel); rearApply(); render() end
function onEditor() run("sh -c 'truesense gui --page keys || truesense gui'"); panel.close() end
function onOpen() render() end
