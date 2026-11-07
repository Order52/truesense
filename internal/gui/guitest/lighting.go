package guitest

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var hex6 = regexp.MustCompile(`^[0-9a-f]{6}$`)

func hexCells(a args) ([]string, error) {
	raw := a.list("cells")
	if len(raw) != 128 {
		return nil, fmt.Errorf("cells: give exactly 128 colours")
	}
	out := make([]string, 128)
	for i, v := range raw {
		s, _ := v.(string)
		if !hex6.MatchString(s) {
			return nil, fmt.Errorf("cell %d: bad colour %q, use rrggbb", i, v)
		}
		out[i] = s
	}
	return out, nil
}

func (l *Laptop) keyboard(verb string) handlerFunc {
	return func(a args) (any, error) {
		switch verb {
		case "status":
			names := []any{}
			for n := range l.keySets {
				names = append(names, n)
			}
			sort.Slice(names, func(i, j int) bool { return names[i].(string) < names[j].(string) })
			return map[string]any{"look": l.kbLook, "brightness": l.kbBrightness, "max": 50, "backlight_timeout": l.kbTimeout, "lighting_profiles": names}, nil
		case "on":
			if l.kbBrightness == 0 {
				l.kbBrightness = 25
			}
			return "keyboard light: on", nil
		case "off":
			l.kbBrightness = 0
			return "keyboard light: off", nil
		case "brightness":
			v := int(a.num("level", -1))
			if v < 0 || v > 50 {
				return nil, fmt.Errorf("brightness 0..50")
			}
			l.kbBrightness = v
			return v, nil
		case "effect":
			name, color := a.str("name"), a.str("color")
			if color == "" {
				color = "00aec7"
			}
			if !hex6.MatchString(color) {
				return nil, fmt.Errorf("bad colour %q, use rrggbb", color)
			}
			if b := int(a.num("brightness", float64(l.kbBrightness))); b >= 0 && b <= 50 {
				l.kbBrightness = b
			}
			l.kbLook = map[string]any{"mode": "effect", "name": name, "color": color, "speed": int(a.num("speed", 5)), "brightness": l.kbBrightness}
			return "sent", nil
		case "color":
			c := a.str("color")
			if !hex6.MatchString(c) {
				return nil, fmt.Errorf("bad colour %q, use rrggbb", c)
			}
			if l.kbLook["mode"] != "effect" {
				return nil, fmt.Errorf("no effect is running: pick one first")
			}
			l.kbLook["color"] = c
			return "sent", nil
		case "speed":
			v := int(a.num("level", 0))
			if v < 1 || v > 12 {
				return nil, fmt.Errorf("speed 1..12")
			}
			l.kbLook["speed"] = v
			return fmt.Sprintf("keyboard effect speed: %d / 11", v), nil
		case "operating":
			return l.boolVerb(&l.operating)(a)
		case "timeout":
			return l.boolVerb(&l.kbTimeout)(a)
		case "restore":
			l.cells = stockCells()
			l.kbLook = map[string]any{"mode": "perkey", "brightness": 12}
			l.kbBrightness = 12
			l.fx = nil
			return "sent", nil
		case "layout":
			keys := map[string]any{}
			for n, cs := range l.layout {
				list := make([]any, len(cs))
				for i, c := range cs {
					list[i] = map[string]any{"cell": c, "col": c / 6, "row": c % 6}
				}
				keys[n] = list
			}
			return map[string]any{"keys": keys, "groups": []any{"all", "arrows", "fkeys", "letters", "numpad", "numrow", "wasd"}}, nil
		case "cells":
			return map[string]any{"cells": toAny(l.cells)}, nil
		case "set-cells":
			c, err := hexCells(a)
			if err != nil {
				return nil, err
			}
			l.cells = c
			l.kbLook = map[string]any{"mode": "perkey", "brightness": l.kbBrightness}
			return "sent", nil
		case "fx":
			mode := a.str("mode")
			if sp := a.num("speed", 5); sp < 1 || sp > 10 {
				return nil, fmt.Errorf("speed 1..10")
			}
			if mode == "off" {
				l.fx = nil
				return map[string]any{"mode": "off"}, nil
			}
			l.fx = map[string]any{"mode": mode, "speed": int(a.num("speed", 5)), "keys": a.list("cells"), "base": a.list("base")}
			return l.fx, nil
		case "design":
			return l.design(a)
		default: // profile
			return l.keySet(a)
		}
	}
}

func (l *Laptop) design(a args) (any, error) {
	switch a.str("action") {
	case "list":
		out := map[string]any{}
		for _, n := range sortedKeys(l.designs) {
			d := l.designs[n]
			out[n] = map[string]any{"info": d["info"], "source": d["source"]}
		}
		return out, nil
	case "show":
		d, ok := l.designs[a.str("name")]
		if !ok {
			return nil, fmt.Errorf("no design '%s' (see: truesense keyboard design list)", a.str("name"))
		}
		return map[string]any{"info": d["info"], "source": d["source"], "cells": d["cells"]}, nil
	case "save":
		n := a.str("name")
		if err := validName(n); err != nil || strings.Contains(n, ".") {
			return nil, fmt.Errorf("design name: letters, digits, - and _")
		}
		cells := toAny(l.cells)
		if a.has("cells") {
			c, err := hexCells(a)
			if err != nil {
				return nil, err
			}
			cells = toAny(c)
		}
		l.designs[n] = map[string]any{"info": "saved from the keyboard", "source": "yours", "cells": cells}
		return "/home/sloppy/.config/truesense/designs/" + n + ".json", nil
	}
	return nil, fmt.Errorf("design: list | show | save")
}

func (l *Laptop) keySet(a args) (any, error) {
	n := a.str("name")
	switch a.str("action") {
	case "list", "":
		out := []any{}
		for k := range l.keySets {
			out = append(out, k)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].(string) < out[j].(string) })
		return out, nil
	case "load":
		c, ok := l.keySets[n]
		if !ok {
			return nil, fmt.Errorf("no profile '%s' (truesense keyboard profile list)", n)
		}
		l.cells = append([]string(nil), c...)
		return "loaded lighting profile '" + n + "'", nil
	case "save":
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`).MatchString(n) {
			return nil, fmt.Errorf("profile names: letters, digits, - and _")
		}
		c := l.cells
		if a.has("cells") {
			var err error
			if c, err = hexCells(a); err != nil {
				return nil, err
			}
		}
		l.keySets[n] = append([]string(nil), c...)
		return "saved lighting profile '" + n + "'", nil
	case "delete":
		if _, ok := l.keySets[n]; !ok {
			return nil, fmt.Errorf("no lighting profile '%s'", n)
		}
		delete(l.keySets, n)
		return "deleted lighting profile '" + n + "'", nil
	}
	return nil, fmt.Errorf("profile list | load NAME | save NAME | delete NAME")
}

func (l *Laptop) rearState() map[string]any {
	r := cloneMap(l.rear)
	r["raw"] = "00000300000100aec703000000000000"
	return r
}

var rearModes = map[string]bool{"static": true, "breathing": true, "neon": true, "wave": true, "twinkling": true}

func (l *Laptop) rearVerb(verb string) handlerFunc {
	return func(a args) (any, error) {
		switch verb {
		case "status":
			return l.rearState(), nil
		case "on":
			if l.rear["brightness"] == 0 {
				l.rear["brightness"] = 100
			}
			return "rear light: on", nil
		case "off":
			l.rear["brightness"] = 0
			return "rear light: off", nil
		}
		eff := a.str("effect")
		if !rearModes[eff] {
			return nil, fmt.Errorf("effect: static, breathing, neon, wave, twinkling")
		}
		color := a.str("color")
		if color == "" {
			color = "ffffff"
		}
		if !hex6.MatchString(color) {
			return nil, fmt.Errorf("color must be RRGGBB hex")
		}
		l.rear = map[string]any{"mode": eff, "speed": int(a.num("speed", 3)), "brightness": int(a.num("brightness", 100)), "direction": int(a.num("direction", 1)), "color": color, "zones": nil}
		if z := a.strings("zones"); eff == "static" && len(z) == 3 {
			l.rear["zones"] = toAny(z)
			l.rear["color"] = z[0]
		}
		return l.rearState(), nil
	}
}

func (l *Laptop) lookVerb(verb string) handlerFunc {
	return func(a args) (any, error) {
		n := a.str("name")
		switch verb {
		case "list":
			return l.looks.names(), nil
		case "show":
			return l.looks.show(n)
		case "save":
			look := a.obj("look")
			if look == nil {
				look = map[string]any{"keyboard": map[string]any{"mode": "perkey", "brightness": l.kbBrightness, "cells": toAny(l.cells)}, "rear": l.rearState()}
			}
			return look, l.looks.save(n, look)
		case "apply":
			p, err := l.looks.show(n)
			if err != nil {
				return nil, err
			}
			done := []any{}
			if k, ok := p["keyboard"].(map[string]any); ok {
				if cells, ok := k["cells"].([]any); ok && len(cells) == 128 {
					l.cells = nil
					for _, c := range cells {
						l.cells = append(l.cells, c.(string))
					}
				}
				done = append(done, "keyboard: replayed")
			}
			if r, ok := p["rear"].(map[string]any); ok {
				l.rear = cloneMap(r)
				done = append(done, "rear: "+fmt.Sprint(r["mode"]))
			}
			return done, nil
		case "delete":
			return n, l.looks.delete(n)
		default:
			return l.looks.copy(n, a.str("new"))
		}
	}
}

func (l *Laptop) startupVerb(verb string) handlerFunc {
	return func(a args) (any, error) {
		switch verb {
		case "show":
			return l.startup, nil
		case "set":
			t := a.str("target")
			if t != "keyboard" && t != "rear" && t != "profile" {
				return nil, fmt.Errorf("target: keyboard | rear | profile")
			}
			v := a.str("value")
			if v == "none" && t == "profile" {
				v = ""
			}
			l.startup[t] = v
			return v, nil
		case "apply":
			return []any{"keyboard: restored the last look", "rear: restored the last look"}, nil
		}
		if l.NoDaemon {
			return nil, errNoDaemon
		}
		switch w := a.str("what"); w {
		case "":
			if l.bootLook == nil {
				return nil, nil
			}
			return l.bootLook, nil
		case "clear":
			l.bootLook = nil
			return nil, nil
		case "current":
			l.bootLook = map[string]any{"keyboard": map[string]any{"mode": "perkey", "brightness": l.kbBrightness, "cells": toAny(l.cells)}, "rear": l.rearState()}
		default:
			p, err := l.looks.show(w)
			if err != nil {
				return nil, err
			}
			delete(p, "builtin")
			l.bootLook = p
		}
		return l.bootLook, nil
	}
}

func (l *Laptop) keysProfile(a args) (any, error) {
	n := a.str("name")
	gestures := []string{"tap", "double", "triple", "long", "fn_esc", "long_ms", "multi_ms"}
	switch a.str("action") {
	case "current":
		c := cloneMap(l.keysCurrent)
		c["profile"] = l.keysProfileName
		return c, nil
	case "use":
		p, err := l.keys.show(n)
		if err != nil {
			return nil, err
		}
		l.keysCurrent, l.keysProfileName = p, n
		return n, nil
	case "save":
		p := map[string]any{"description": ""}
		if old, ok := l.keys.data[n]; ok && l.keys.user[n] {
			p = cloneMap(old)
		} else if base, ok := l.keys.data["default"]; ok {
			p = cloneMap(base)
			p["description"] = ""
		}
		for _, g := range gestures {
			if a.has(g) {
				p[g] = a[g]
			}
		}
		if err := l.keys.save(n, p); err != nil {
			return nil, err
		}
		return p, nil
	}
	return libVerb(l.keys, a, func(string) {}, l.keysProfileName, func(args) map[string]any { return nil })
}

func (l *Laptop) profileVerb(verb string) handlerFunc {
	return func(a args) (any, error) {
		n := a.str("name")
		switch verb {
		case "list":
			out := []any{}
			for _, k := range sortedKeys(l.profiles) {
				src := "shipped"
				if l.profileUser[k] {
					src = "yours"
				}
				out = append(out, map[string]any{"name": k, "source": src, "description": l.profiles[k]["description"]})
			}
			return out, nil
		case "show":
			p, ok := l.profiles[n]
			if !ok {
				return nil, fmt.Errorf("no profile '%s'", n)
			}
			return p, nil
		case "apply", "next":
			if verb == "next" {
				names := sortedKeys(l.profiles)
				i := sort.SearchStrings(names, l.lastProfile)
				n = names[(i+1)%len(names)]
				if l.lastProfile == "" {
					n = names[0]
				}
			}
			p, ok := l.profiles[n]
			if !ok {
				return nil, fmt.Errorf("no profile '%s'", n)
			}
			if pw, ok := p["power"].(map[string]any); ok {
				if m, ok := pw["mode"].(string); ok {
					l.mode = m
				}
			}
			l.lastProfile = n
			return fmt.Sprintf("profile '%s' applied", n), nil
		case "save":
			if err := validName(n); err != nil {
				return nil, fmt.Errorf("profile names: letters, digits, - and _")
			}
			if l.profiles[n] != nil && !l.profileUser[n] {
				return nil, fmt.Errorf("'%s' is a shipped profile: save under another name", n)
			}
			l.profiles[n] = map[string]any{"description": a.str("description"), "power": map[string]any{"mode": l.mode}, "fan": map[string]any{"mode": l.fanMode}}
			l.profileUser[n] = true
			return "/home/sloppy/.config/truesense/profiles/" + n + ".toml", nil
		case "delete":
			if !l.profileUser[n] {
				return nil, fmt.Errorf("no profile of yours called '%s'", n)
			}
			delete(l.profiles, n)
			delete(l.profileUser, n)
			return "deleted " + n, nil
		default:
			var name any
			if l.lastProfile != "" {
				name = l.lastProfile
			}
			return map[string]any{"name": name}, nil
		}
	}
}

func builtinDesigns(layout map[string][]int) map[string]map[string]any {
	paint := func(base string, groups map[string]string) []any {
		cells := make([]string, 128)
		for i := range cells {
			cells[i] = base
			if i >= 126 {
				cells[i] = "000000"
			}
		}
		for g, c := range groups {
			var names []string
			switch g {
			case "wasd":
				names = []string{"w", "a", "s", "d"}
			case "arrows":
				names = []string{"up", "down", "left", "right"}
			case "fkeys":
				for i := 1; i <= 12; i++ {
					names = append(names, fmt.Sprintf("f%d", i))
				}
			case "letters":
				for r := 'a'; r <= 'z'; r++ {
					names = append(names, string(r))
				}
			default:
				names = []string{g}
			}
			for _, n := range names {
				for _, cell := range layout[n] {
					cells[cell] = c
				}
			}
		}
		return toAny(cells)
	}
	mk := func(info string, cells []any) map[string]any {
		return map[string]any{"info": info, "source": "built-in", "cells": cells}
	}
	return map[string]map[string]any{
		"gaming":  mk("WASD and the keys around it lit for play", paint("101018", map[string]string{"wasd": "ff6a00", "lshift": "ff3300", "space": "ff3300", "ctrl": "ff3300", "esc": "ff0066", "arrows": "ffd000"})),
		"coding":  mk("dark board, function keys and Esc picked out", paint("08080f", map[string]string{"letters": "1d6f8a", "fkeys": "ff9900", "esc": "ff2244", "enter": "55ee66"})),
		"arrows":  mk("only the arrow keys and WASD", paint("000000", map[string]string{"wasd": "ffffff", "arrows": "ffffff"})),
		"ice":     mk("cool blue to white", paint("0a2a50", map[string]string{"letters": "3ab0ff", "fkeys": "c8ecff", "esc": "ffffff"})),
		"ember":   mk("warm red and orange", paint("3a0600", map[string]string{"letters": "ff4a00", "fkeys": "ffb030", "esc": "ffe080"})),
		"stealth": mk("almost off, just the letters", paint("000000", map[string]string{"letters": "14323c"})),
	}
}
