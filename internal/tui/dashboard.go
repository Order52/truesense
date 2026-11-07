package tui

import (
	"fmt"
	"strings"
)

// tempStyle colours a temperature.
func tempStyle(t Opt[int]) Style {
	switch {
	case !t.OK:
		return StDim
	case t.V >= 85:
		return StBad
	case t.V >= 72:
		return StWarn
	}
	return StGood
}

// spark renders the last width readings as a sparkline scaled between lo and hi.
func spark(g *Glyphs, values []int, width, lo, hi int) string {
	if len(values) > width {
		values = values[len(values)-width:]
	}
	out := make([]rune, 0, width)
	for _, v := range values {
		f := 0.0
		if hi > lo {
			f = min(1.0, max(0.0, float64(v-lo)/float64(hi-lo)))
		}
		out = append(out, g.Spark[roundEven(f*float64(len(g.Spark)-1))])
	}
	return strings.Repeat(" ", max(0, width-len(out))) + string(out)
}

func rjust(s string, n int) string {
	if k := width(s); k < n {
		return strings.Repeat(" ", n-k) + s
	}
	return s
}

// drawDashboard draws the read-only panels in priority order: temperatures, fans, load,
// power. Two columns when wide; whatever does not fit is left out, never squeezed.
func (a *App) drawDashboard(y, h, w int) {
	cv := a.cv
	g := cv.Glyphs()
	v := a.data.view()
	var svc ServiceState
	if v.svc != nil {
		svc = *v.svc
	}
	two := w >= 96
	colw := w - 2
	if two {
		colw = (w - 3) / 2
	}

	type gaugeOpts struct {
		hist   []int
		lo, hi int
		width  int
	}
	gauge := func(yy, xx, ww int, label string, val Opt[int], vmax int, unit string, st Style, o *gaugeOpts) {
		cv.Put(yy, xx, fmt.Sprintf("%-4s", label), StDim)
		wd := 6
		if o != nil && o.width > 0 {
			wd = o.width
		}
		text := "…"
		if val.OK {
			text = fmt.Sprintf("%d%s", val.V, unit)
		}
		text = rjust(text, wd)
		sp := o != nil && ww >= 40
		extra := 0
		if sp {
			extra = 12
		}
		bw := max(6, ww-5-width(text)-2-extra)
		f := 0.0
		if val.OK && val.V != 0 {
			f = min(1.0, max(0.0, float64(val.V)/float64(vmax)))
		}
		n := roundEven(f * float64(bw))
		cv.Put(yy, xx+5, strings.Repeat(g.Full, n), st)
		cv.Put(yy, xx+5+n, strings.Repeat(g.Empty, bw-n), StBarOff)
		tst := st
		if !val.OK {
			tst = StDim
		}
		cv.Put(yy, xx+6+bw, text, tst)
		if sp {
			cv.Put(yy, xx+7+bw+width(text), spark(g, o.hist, 10, o.lo, o.hi), st)
		}
	}
	sens := func(f func(Sensors) int) Opt[int] {
		if !v.hasSens {
			return Opt[int]{}
		}
		return Some(f(v.sens))
	}
	tc, tg, ts := sens(func(s Sensors) int { return s.CPUTemp }), sens(func(s Sensors) int { return s.GPUTemp }), sens(func(s Sensors) int { return s.SysTemp })

	temps := func(y, x, w int) int {
		drawBox(cv, y, x, 6, w, "Temperatures", StDim, StAccent)
		gauge(y+1, x+2, w-4, "CPU", tc, 100, " °C", tempStyle(tc), &gaugeOpts{v.hist[histTCPU], 30, 100, 0})
		gauge(y+2, x+2, w-4, "GPU", tg, 100, " °C", tempStyle(tg), &gaugeOpts{v.hist[histTGPU], 30, 100, 0})
		gauge(y+3, x+2, w-4, "SYS", ts, 100, " °C", tempStyle(ts), nil)
		cv.Put(y+4, x+2, "Now  ", StDim)
		now, st := svc.ModeReason, StDim
		if svc.Activity != "" {
			st = StAccent
		}
		if now == "" {
			now = svc.Activity
		}
		if now == "" {
			now = "…"
			if v.svc == nil {
				now = "needs the TrueSense service"
			}
		}
		cv.Put(y+4, x+8, clip(now, max(0, w-12)), st)
		return 6
	}
	fans := func(y, x, w int) int {
		drawBox(cv, y, x, 5, w, "Fans", StDim, StAccent)
		gauge(y+1, x+2, w-4, "CPU", sens(func(s Sensors) int { return s.CPUFanRPM }), 6500, " rpm", StBar, &gaugeOpts{v.hist[histRPM1], 0, 6500, 8})
		gauge(y+2, x+2, w-4, "GPU", sens(func(s Sensors) int { return s.GPUFanRPM }), 6500, " rpm", StBar, &gaugeOpts{v.hist[histRPM2], 0, 6500, 8})
		cd, gd, mode := "…", "…", "…"
		if v.fan != nil {
			cd, gd, mode = fmt.Sprint(v.fan.CPUDuty), fmt.Sprint(v.fan.GPUDuty), v.fan.Mode
		}
		cv.Put(y+3, x+2, clip(fmt.Sprintf("duty CPU %s %%  GPU %s %%  ·  %s", cd, gd, mode), w-4), StDim)
		return 5
	}
	load := func(y, x, w int) int {
		drawBox(cv, y, x, 5, w, "Load", StDim, StAccent)
		gauge(y+1, x+2, w-4, "CPU", svc.CPULoad, 100, " %", StBar, &gaugeOpts{v.hist[histCPU], 0, 100, 0})
		gauge(y+2, x+2, w-4, "GPU", svc.GPULoad, 100, " %", StBar, &gaugeOpts{v.hist[histGPU], 0, 100, 0})
		watts := "…"
		if svc.GPUWatts.OK {
			watts = fmt.Sprintf("%d W", svc.GPUWatts.V)
		}
		cv.Put(y+3, x+2, "GPU power "+watts, StDim)
		return 5
	}
	power := func(y, x, w int) int {
		drawBox(cv, y, x, 5, w, "Power", StDim, StAccent)
		cv.Put(y+1, x+2, "Mode", StDim)
		label, ok := modeLabel[v.mode]
		short := label
		if !ok {
			label = v.mode
			if label == "" {
				label = "…"
			}
			short = "…"
		}
		mst, ok := modeStyle[v.mode]
		if !ok {
			mst = StText
		}
		cv.Put(y+1, x+9, label, mst)
		src, sst := "…", StWarn
		switch {
		case v.ac.OK && v.ac.V:
			src, sst = "on charger", StGood
		case v.ac.OK:
			src = "on battery"
		}
		cv.Put(y+1, x+10+width(short)+2, src, sst)
		auto, prof := orDots(svc.AutoMode), orDots(svc.SmartProfile)
		cv.Put(y+2, x+2, "Auto", StDim)
		cv.Put(y+2, x+9, clip(fmt.Sprintf("%s · profile %s", auto, prof), w-12), StText)
		if v.batt.OK {
			pct := v.batt.V.Percent
			st := StBad
			if pct > 30 {
				st = StGood
			}
			gauge(y+3, x+2, w-4, "Batt", Some(pct), 100, " %", st, nil)
		}
		return 5
	}
	activity := func(y, x, w, room int) {
		n := max(1, min(room-2, 12, len(a.log)))
		drawBox(cv, y, x, n+2, w, "What you did", StDim, StAccent)
		if len(a.log) == 0 {
			cv.Put(y+1, x+2, "Nothing yet: your changes are listed here.", StDim)
		}
		recent := a.log
		if len(recent) > n {
			recent = recent[len(recent)-n:]
		}
		for i, e := range recent {
			cv.Put(y+1+i, x+2, e.at, StDim)
			st := StBad
			if e.style == StGood {
				st = StGood
			}
			cv.Put(y+1+i, x+11, clip(e.text, w-14), st)
		}
	}

	type panel struct {
		h  int
		fn func(y, x, w int) int
	}
	panels := []panel{{6, temps}, {5, fans}, {5, load}, {5, power}}
	yy := y
	if two {
		for _, pair := range [][2]panel{{panels[0], panels[1]}, {panels[2], panels[3]}} {
			ph := max(pair[0].h, pair[1].h)
			if yy+ph > y+h {
				break
			}
			pair[0].fn(yy, 1, colw)
			pair[1].fn(yy, 1+colw+1, colw)
			yy += ph
		}
	} else {
		for _, p := range panels {
			if yy+p.h > y+h {
				break
			}
			p.fn(yy, 1, colw)
			yy += p.h
		}
	}
	if y+h-yy >= 5 {
		activity(yy, 1, w-2, y+h-yy)
	}
}

func orDots(s string) string {
	if s == "" {
		return "…"
	}
	return s
}
