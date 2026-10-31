-- Power mode panel (TrueSense).
local STATE = "/tmp/truesense-state"
local LIST = {
  { "eco",         "battery-eco", "Eco",         "Lowest power, cool and quiet" },
  { "quiet",       "moon",        "Quiet",       "Low fan noise" },
  { "balanced",    "scale",       "Balanced",    "Everyday default" },
  { "performance", "gauge",       "Performance", "More power, still sensible" },
  { "turbo",       "bolt",        "Turbo",       "Full power, loud fans" },
}
local want

local function readState()
  local s = {}
  for k, v in (noctalia.readFile(STATE) or ""):gmatch("([%w_]+)=([^\n]*)") do s[k] = v end
  return s
end

local function set(p)
  noctalia.runAsync("PATH=$HOME/.local/bin:$PATH truesense-fast power " .. p)
  want = p
  panel.close()
end

function render()
  local st = readState()
  local cur = st.power_mode
  if want ~= nil then if cur == want then want = nil else cur = want end end
  local rows = {
    ui.button({ glyph = "refresh", text = "Auto   battery · power · load", flexGrow = 1,
      variant = st.auto == "1" and "default" or "ghost", onClick = "onToggleAuto" }),
    ui.label({ text = "Power mode", color = "on_surface_variant" }),
  }
  for _, it in ipairs(LIST) do
    rows[#rows + 1] = ui.button({
      glyph = it[2], text = it[3] .. "   " .. it[4], flexGrow = 1,
      variant = (it[1] == cur) and "default" or "ghost",
      onClick = "onPick_" .. it[1],
    })
  end
  panel.render(ui.column({ gap = 6, padding = 14 }, rows))
end

function onPick_eco() set("eco") end
function onPick_quiet() set("quiet") end
function onPick_balanced() set("balanced") end
function onPick_performance() set("performance") end
function onPick_turbo() set("turbo") end
function onToggleAuto()
  noctalia.runAsync("PATH=$HOME/.local/bin:$PATH truesense-fast auto " .. (readState().auto == "1" and "off" or "on"))
  panel.close()
end
function onOpen() want = nil; render() end
