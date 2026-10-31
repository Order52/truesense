-- Fan panel (TrueSense): Smart curve, firmware Auto, Max, or a manual 20-100 % duty.
local STATE = "/tmp/truesense-state"
local PRESETS = {
  { "auto", "Auto (firmware)", "propeller" }, { 30, "Quiet 30%", "leaf" }, { 50, "Balanced 50%", "wind" },
  { 70, "Performance 70%", "rocket" }, { "max", "Max", "flame" },
}
local ACT = { idle = "Idle", browsing = "Browsing", gaming = "Gaming", heavy = "Heavy", hot = "Hot!" }
local drag, shownVal, wantMode

local function readState()
  local s = {}
  for k, v in (noctalia.readFile(STATE) or ""):gmatch("([%w_]+)=([^\n]*)") do s[k] = v end
  return s
end

local function apply(what)
  local cmd
  if what == "smart" then cmd = "PATH=$HOME/.local/bin:$PATH truesense-fast fan smart"
  elseif what == "auto" then cmd = "PATH=$HOME/.local/bin:$PATH truesense-fast fan auto"
  elseif what == "max" then cmd = "PATH=$HOME/.local/bin:$PATH truesense-fast fan max"
  else cmd = "PATH=$HOME/.local/bin:$PATH truesense-fast fan " .. math.max(20, math.min(100, math.floor(tonumber(what) or 50))) end
  noctalia.runAsync(cmd)
  drag, wantMode = nil, what
  render()
end

function render()
  local st = readState()
  if st.hw_ok ~= "1" then
    panel.render(ui.column({ gap = 8, padding = 16 }, { ui.label({ text = "Fan control unavailable", color = "error" }),
      ui.label({ text = "TrueSense daemon not running", color = "on_surface_variant" }) }))
    return
  end
  local smart = st.mode == "smart"
  local cur = tonumber((st.fan or ""):match("^(%d+)")) or 0
  local top = math.max(tonumber(st.rpm1) or 0, tonumber(st.rpm2) or 0)
  local title, sub, glyph, col
  if drag then title, sub, glyph, col = drag .. "%", "release to apply", "wind", "secondary"
  elseif smart then title, sub, glyph, col = "Smart · " .. (ACT[st.activity] or "…"), "Curve follows temperature and load", "sparkles", (st.activity == "hot") and "error" or "primary"
  elseif st.fan == "auto" then title, sub, glyph, col = "Auto", "Firmware control (0 rpm when cool)", "propeller", "on_surface_variant"
  else title, sub, glyph, col = cur .. "%", "Manual duty", "wind", "secondary" end
  local chips = { ui.button({ glyph = "sparkles", tooltip = "Smart curve", flexGrow = 1, variant = (smart and not drag) and "default" or "ghost", onClick = "onSmart" }) }
  for i, m in ipairs(PRESETS) do
    chips[#chips + 1] = ui.button({ glyph = m[3], tooltip = m[2], flexGrow = 1, variant = "ghost", onClick = "onPreset" .. i })
  end
  shownVal = drag or (cur > 0 and cur or 20)
  panel.render(ui.column({ gap = 14, padding = 16 }, {
    ui.row({ gap = 10, align = "center" }, {
      ui.glyph({ name = glyph, size = 28, color = col }),
      ui.column({ gap = 0, flexGrow = 1 }, {
        ui.label({ text = title, fontSize = 18, color = col }),
        ui.label({ text = sub, fontSize = 12, color = "on_surface_variant" }) }),
      ui.column({ gap = 0 }, {
        ui.label({ text = "CPU " .. (st.tcpu or "?") .. " °C", fontSize = 12, color = "on_surface_variant" }),
        ui.label({ text = "GPU " .. (st.tgpu or "?") .. " °C", fontSize = 12, color = "on_surface_variant" }) }),
    }),
    ui.slider({ min = 20, max = 100, step = 5, value = shownVal, controlSize = "md", onChange = "onSlide", onDragEnd = "onSlideEnd" }),
    ui.row({ gap = 4 }, chips),
    ui.row({ gap = 8, justify = "space_between" }, {
      ui.label({ text = "CPU fan  " .. (st.rpm1 or "-") .. " rpm", fontSize = 12, color = "on_surface_variant" }),
      ui.label({ text = "GPU fan  " .. (st.rpm2 or "-") .. " rpm", fontSize = 12, color = "on_surface_variant" }) }),
  }))
end

function onSlide(v)
  v = tonumber(v)
  if v == nil or (drag == nil and v == shownVal) then return end
  drag = v; render()
end
function onSlideEnd(v) if drag ~= nil then apply(tonumber(v) or drag) end end
function onSmart() apply("smart") end
function onPreset1() apply("auto") end
function onPreset2() apply(30) end
function onPreset3() apply(50) end
function onPreset4() apply(70) end
function onPreset5() apply("max") end

local gen = 0
function onOpen()
  drag, wantMode = nil, nil
  gen = gen + 1
  local mine = gen
  noctalia.runStream("for i in $(seq 120); do echo; sleep 1; done", function() if mine == gen and not drag then render() end end)
  render()
end
function onClose() gen = gen + 1 end
