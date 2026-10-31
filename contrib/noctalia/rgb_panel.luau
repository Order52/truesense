-- RGB panel (TrueSense): keyboard effects/colour/brightness and the rear light.
local KB = { { "static", "Static" }, { "breathing", "Breathe" }, { "wave", "Wave" }, { "snake", "Snake" }, { "ripple", "Ripple" },
  { "neon", "Neon" }, { "raindrop", "Rain" }, { "lightning", "Bolt" }, { "spot", "Spot" }, { "star", "Stars" },
  { "fireball", "Fire" }, { "snow", "Snow" }, { "heartbeat", "Pulse" } }
local REAR = { { "static", "Static" }, { "breathing", "Breathe" }, { "neon", "Neon" }, { "wave", "Wave" }, { "twinkling", "Twinkle" } }
local SW = { "#FF0000", "#FF6A00", "#FFD000", "#00FF55", "#00FFD0", "#00AAFF", "#3A3AFF", "#B400FF", "#FF00AA", "#FFFFFF" }
local st = { tab = "kb", hex = "00AAFF", level = 25, rear = "static", rhex = "00AEC7", rlevel = 100, speed = 3 }

local function run(cmd) noctalia.runAsync(cmd) end
local function clean(h) return (tostring(h):gsub("[^%x]", ""):sub(1, 6)) end

local function tabs()
  return ui.row({ gap = 4 }, {
    ui.button({ glyph = "keyboard", text = "Keyboard", flexGrow = 1, controlSize = "sm", variant = st.tab == "kb" and "default" or "ghost", onClick = "onTabKb" }),
    ui.button({ glyph = "bulb", text = "Rear light", flexGrow = 1, controlSize = "sm", variant = st.tab == "rear" and "default" or "ghost", onClick = "onTabRear" }),
  })
end

local function chips(list, prefix)
  local rows, row = {}, {}
  for i, e in ipairs(list) do
    row[#row + 1] = ui.button({ text = e[2], controlSize = "sm", flexGrow = 1, variant = "ghost", onClick = prefix .. i })
    if #row == 4 or i == #list then rows[#rows + 1] = ui.row({ gap = 4 }, row); row = {} end
  end
  return ui.column({ gap = 4 }, rows)
end

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
      ui.label({ text = "Effect (uses the colour below)", fontSize = 12, color = "on_surface_variant" }),
      chips(KB, "onKb"),
      ui.label({ text = "Colour", fontSize = 12, color = "on_surface_variant" }),
      ui.colorPicker({ value = "#" .. st.hex, swatches = SW, onChange = "onKbColorLive", onChangeEnd = "onKbColor" }),
      ui.row({ gap = 4 }, {
        ui.button({ text = "Fill keys", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onKbFill" }),
        ui.button({ text = "Rainbow", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onKbRainbow" }),
      }),
      ui.label({ text = "Brightness  " .. st.level .. " / 50", fontSize = 12, color = "on_surface_variant" }),
      ui.slider({ min = 0, max = 50, step = 5, value = st.level, controlSize = "md", onChange = "onKbLevel", onDragEnd = "onKbLevelEnd" }),
    }
  else
    body = {
      tabs(),
      ui.row({ gap = 4 }, {
        ui.button({ glyph = "bulb", text = "On", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onRearOn" }),
        ui.button({ glyph = "bulb-off", text = "Off", flexGrow = 1, controlSize = "sm", variant = "ghost", onClick = "onRearOff" }),
      }),
      ui.label({ text = "Effect (uses the colour below)", fontSize = 12, color = "on_surface_variant" }),
      chips(REAR, "onRear"),
      ui.label({ text = "Colour", fontSize = 12, color = "on_surface_variant" }),
      ui.colorPicker({ value = "#" .. st.rhex, swatches = SW, onChange = "onRearColorLive", onChangeEnd = "onRearColor" }),
      ui.label({ text = "Brightness  " .. st.rlevel .. " %", fontSize = 12, color = "on_surface_variant" }),
      ui.slider({ min = 10, max = 100, step = 10, value = st.rlevel, controlSize = "md", onChange = "onRearLevel", onDragEnd = "onRearLevelEnd" }),
    }
  end
  panel.render(ui.column({ gap = 10, padding = 16 }, body))
end

function onTabKb() st.tab = "kb"; render() end
function onTabRear() st.tab = "rear"; render() end
function onKbOn() run("truesense keyboard on") end
function onKbOff() run("truesense keyboard off") end
function onKbRestore() run("truesense keyboard restore") end
local function kbEffect(i) run("truesense keyboard effect " .. KB[i][1] .. " --color " .. st.hex .. " --brightness " .. math.max(st.level, 10)) end
function onKb1() kbEffect(1) end
function onKb2() kbEffect(2) end
function onKb3() kbEffect(3) end
function onKb4() kbEffect(4) end
function onKb5() kbEffect(5) end
function onKb6() kbEffect(6) end
function onKb7() kbEffect(7) end
function onKb8() kbEffect(8) end
function onKb9() kbEffect(9) end
function onKb10() kbEffect(10) end
function onKb11() kbEffect(11) end
function onKb12() kbEffect(12) end
function onKb13() kbEffect(13) end
function onKbColorLive(v) st.hex = clean(v) end
function onKbColor(v) st.hex = clean(v); run("truesense keyboard fill " .. st.hex) end
function onKbFill() run("truesense keyboard fill " .. st.hex) end
function onKbRainbow() run("truesense keyboard rainbow") end
function onKbLevel(v) v = tonumber(v); if v then st.level = math.floor(v) end end
function onKbLevelEnd(v) st.level = math.floor(tonumber(v) or st.level); run("truesense keyboard brightness " .. st.level); render() end
function onRearOn() run("truesense rear on") end
function onRearOff() run("truesense rear off") end
local function rearEffect(i) st.rear = REAR[i][1]; run("truesense rear set " .. st.rear .. " --color " .. st.rhex .. " --speed " .. st.speed .. " --brightness " .. st.rlevel) end
function onRear1() rearEffect(1) end
function onRear2() rearEffect(2) end
function onRear3() rearEffect(3) end
function onRear4() rearEffect(4) end
function onRear5() rearEffect(5) end
function onRearColorLive(v) st.rhex = clean(v) end
function onRearColor(v) st.rhex = clean(v); run("truesense rear set " .. st.rear .. " --color " .. st.rhex .. " --speed " .. st.speed .. " --brightness " .. st.rlevel) end
function onRearLevel(v) v = tonumber(v); if v then st.rlevel = math.floor(v) end end
function onRearLevelEnd(v) st.rlevel = math.floor(tonumber(v) or st.rlevel); run("truesense rear set " .. st.rear .. " --color " .. st.rhex .. " --speed " .. st.speed .. " --brightness " .. st.rlevel); render() end
function onOpen() render() end
