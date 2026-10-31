-- TrueSense fan button: shows Smart / Auto / Max / manual, with live RPM.
-- Click / right-click: fan panel · Middle click: Smart curve
-- IPC: noctalia msg plugin sloppy/toggles:fan all set <smart|auto|max|20-100>
local STATE = "/tmp/truesense-state"
local PANEL = "sloppy/toggles:fan_panel"
local ACT   = { idle = "Idle", browsing = "Browsing", gaming = "Gaming", heavy = "Heavy load", hot = "Hot!" }

local function readState()
  local s = {}
  for k, v in (noctalia.readFile(STATE) or ""):gmatch("([%w_]+)=([^\n]*)") do s[k] = v end
  return s
end
local function kfmt(n) return n >= 1000 and string.format("%.1fk", n / 1000) or tostring(n) end

local function osd(glyph, text) noctalia.runAsync("noctalia msg osd-show " .. glyph .. " '" .. text .. "'") end
local function apply(what)
  if what == "smart" then noctalia.runAsync("PATH=$HOME/.local/bin:$PATH truesense-fast fan smart"); osd("sparkles", "Fan: Smart curve")
  elseif what == "auto" then noctalia.runAsync("PATH=$HOME/.local/bin:$PATH truesense-fast fan auto"); osd("propeller", "Fan: firmware auto")
  elseif what == "max" then noctalia.runAsync("PATH=$HOME/.local/bin:$PATH truesense-fast fan max"); osd("flame", "Fan: MAX")
  else
    local n = math.max(20, math.min(100, math.floor(tonumber(what) or 50)))
    noctalia.runAsync("PATH=$HOME/.local/bin:$PATH truesense-fast fan " .. n); osd("wind", "Fan: " .. n .. "%")
  end
end

function update()
  noctalia.setUpdateInterval(1000)
  local st = readState()
  if st.hw_ok ~= "1" then
    barWidget.setGlyph("propeller") barWidget.setText("N/A") barWidget.setColor("on_surface_variant")
    barWidget.setTooltip("TrueSense daemon not running or device unavailable")
    return
  end
  local top = math.max(tonumber(st.rpm1) or 0, tonumber(st.rpm2) or 0)
  local smart = st.mode == "smart"
  local name, glyph, col
  if smart then name, glyph, col = "SMART", "sparkles", (st.activity == "hot") and "error" or "primary"
  elseif st.fan == "auto" then name, glyph, col = "AUTO", "propeller", "on_surface_variant"
  else name, glyph, col = (st.fan or "?"):match("^(%d+)") and ((st.fan:match("^(%d+)")) .. "%") or "MAN", "wind", "secondary" end
  barWidget.setGlyph(glyph)
  barWidget.setText(top > 0 and (name .. " " .. kfmt(top)) or name)
  barWidget.setColor(col) barWidget.setGlyphColor(col)
  barWidget.setTooltip({
    { "Fan", smart and ("Smart · " .. (ACT[st.activity] or "…")) or (st.fan == "auto" and "Firmware auto" or ("Manual " .. (st.fan or "?"))) },
    { "Fans", (st.rpm1 or "?") .. " / " .. (st.rpm2 or "?") .. " rpm" },
    { "Temps", "CPU " .. (st.tcpu or "?") .. " °C · GPU " .. (st.tgpu or "?") .. " °C" },
    { "Load", "CPU " .. (st.cpu or "?") .. "% · GPU " .. (st.gpu or "?") .. "%" },
    { "Click", "fan menu" }, { "Middle click", "Smart curve" } })
end

function onClick() noctalia.togglePanel(PANEL) end
function onRightClick() noctalia.togglePanel(PANEL) end
function onMiddleClick() apply("smart") end
function onIpc(event, payload)
  if event ~= "set" or payload == nil then return end
  local p = tostring(payload):lower()
  if p == "smart" or p == "auto" or p == "max" then apply(p) return end
  local n = tonumber(p:match("^(%d+)"))
  if n then apply(n) end
end
update()
