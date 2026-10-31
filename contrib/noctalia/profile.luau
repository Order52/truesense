-- TrueSense power mode (Acer firmware: eco / quiet / balanced / performance / turbo). State comes from the TrueSense daemon.
-- Click: profile panel · Middle: toggle automatic profile · IPC: set <mode> | auto <on|off>
local STATE = "/tmp/truesense-state"
local PANEL = "YOUR-PLUGIN-ID:profile_panel"
local INFO = {
  eco         = { "ECO",   "battery-eco", "tertiary",           "Eco" },
  quiet       = { "QUIET", "moon",        "secondary",          "Quiet" },
  balanced    = { "BAL",   "scale",       "on_surface_variant", "Balanced" },
  performance = { "PERF",  "gauge",       "primary",            "Performance" },
  turbo       = { "TURBO", "bolt",        "error",              "Turbo" },
}
local ACT = { idle = "idle", browsing = "browsing", gaming = "gaming", heavy = "heavy load", hot = "hot" }

local function readState()
  local s = {}
  for k, v in (noctalia.readFile(STATE) or ""):gmatch("([%w_]+)=([^\n]*)") do s[k] = v end
  return s
end

local function set(p)
  local m = INFO[p]
  if not m then return end
  noctalia.runAsync("PATH=$HOME/.local/bin:$PATH truesense-fast power " .. p)
  noctalia.runAsync("noctalia msg osd-show " .. m[2] .. " 'Power mode: " .. m[4] .. "'")
end

local function setAuto(on)
  noctalia.runAsync("PATH=$HOME/.local/bin:$PATH truesense-fast auto " .. (on and "on" or "off"))
  noctalia.runAsync("noctalia msg osd-show " .. (on and "refresh" or "hand-stop") .. " 'Automatic profile: " .. (on and "on" or "off") .. "'")
end

function update()
  noctalia.setUpdateInterval(2000)
  local st = readState()
  local m = INFO[st.power_mode or ""]
  if not m then
    barWidget.setGlyph("gauge") barWidget.setText("N/A")
    barWidget.setColor("on_surface_variant") barWidget.setGlyphColor("on_surface_variant")
    barWidget.setTooltip("TrueSense daemon not running")
    return
  end
  local auto = st.auto == "1"
  barWidget.setGlyph(m[2]) barWidget.setText((auto and "A·" or "") .. m[1])
  barWidget.setColor(m[3]) barWidget.setGlyphColor(m[3])
  barWidget.setTooltip({
    { "Power mode", m[4] },
    { "Mode", auto and ("Auto · " .. (ACT[st.activity] or "…")) or "Manual" },
    { "Power", st.ac == "1" and "AC" or "Battery" },
    { "Click", "power menu" }, { "Middle click", "toggle auto" } })
end

function onClick() noctalia.togglePanel(PANEL) end
function onRightClick() noctalia.togglePanel(PANEL) end
function onMiddleClick() setAuto(readState().auto ~= "1") end
function onIpc(event, payload)
  payload = payload and tostring(payload) or ""
  if event == "set" and INFO[payload] then set(payload)
  elseif event == "auto" then setAuto(payload ~= "off") end
end
update()
