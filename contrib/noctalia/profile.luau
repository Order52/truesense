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

local AUTO = { "off", "native", "smart" }
local AUTOTXT = { off = "Manual", native = "Native auto", smart = "Smart auto" }
local function setAuto(kind)
  noctalia.runAsync("PATH=$HOME/.local/bin:$PATH truesense-fast auto " .. kind)
  noctalia.runAsync("noctalia msg osd-show " .. (kind == "off" and "hand-stop" or "refresh") .. " 'Mode control: " .. AUTOTXT[kind] .. "'")
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
  local kind = st.auto_mode or (st.auto == "1" and "smart" or "off"); local auto = kind ~= "off"
  barWidget.setGlyph(m[2]) barWidget.setText((kind == "smart" and "S·" or kind == "native" and "N·" or "") .. m[1])
  barWidget.setColor(m[3]) barWidget.setGlyphColor(m[3])
  barWidget.setTooltip({
    { "Power mode", m[4] },
    { "Mode control", auto and (AUTOTXT[kind] .. " · " .. (ACT[st.activity] or "…")) or "Manual" },
    { "Power", st.ac == "1" and "AC" or "Battery" },
    { "Click", "power menu" }, { "Middle click", "cycle Manual / Native / Smart" } })
end

function onClick() noctalia.togglePanel(PANEL) end
function onRightClick() noctalia.togglePanel(PANEL) end
function onMiddleClick() local cur = readState().auto_mode or "off"; for i, k in ipairs(AUTO) do if k == cur then setAuto(AUTO[i % 3 + 1]) return end end setAuto("smart") end
function onIpc(event, payload)
  payload = payload and tostring(payload) or ""
  if event == "set" and INFO[payload] then set(payload)
  elseif event == "auto" then setAuto((payload == "native" or payload == "off") and payload or "smart") end
end
update()
