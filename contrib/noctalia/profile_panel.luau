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
  local kind = st.auto_mode or (st.auto == "1" and "smart" or "off")
  local rows = {
    ui.label({ text = "Mode control", color = "on_surface_variant" }),
    ui.row({ gap = 4 }, {
      ui.button({ text = "Manual", flexGrow = 1, controlSize = "sm", variant = kind == "off" and "default" or "ghost", onClick = "onAuto_off" }),
      ui.button({ text = "Native", flexGrow = 1, controlSize = "sm", variant = kind == "native" and "default" or "ghost", onClick = "onAuto_native" }),
      ui.button({ text = "Smart", flexGrow = 1, controlSize = "sm", variant = kind == "smart" and "default" or "ghost", onClick = "onAuto_smart" }),
    }),
    ui.label({ text = kind == "smart" and "Switches by heat, load and battery" or kind == "native" and "Balanced on power, Eco on battery" or "You choose the mode", fontSize = 12, color = "on_surface_variant" }),
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
local function setKind(k) noctalia.runAsync("PATH=$HOME/.local/bin:$PATH truesense-fast auto " .. k); panel.close() end
function onAuto_off() setKind("off") end
function onAuto_native() setKind("native") end
function onAuto_smart() setKind("smart") end
function onOpen() want = nil; render() end
