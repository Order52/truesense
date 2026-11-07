package curves

import (
	"encoding/json"
	"sort"

	"github.com/Order52/truesense/internal/firmware"
)

// Validate checks a curve that arrived as decoded JSON (a control-socket request, a file) and
// returns it with the defaults filled in. Anything that could hurt (a curve that is too cool
// for the load) or is malformed is an error. Unknown keys are ignored, as the Python
// implementation did.
func Validate(v any) (Curve, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Curve{}, firmware.Errorf("a curve is an object with 'cpu' and 'gpu' points")
	}
	return Parse(data)
}

// Parse is Validate for JSON text.
func Parse(data []byte) (Curve, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return Curve{}, firmware.Errorf("a curve is an object with 'cpu' and 'gpu' points")
	}
	c := Defaults()
	if s, ok := rawString(obj["description"]); ok {
		c.Description = s
	}
	if b, ok := rawBool(obj["native"]); ok {
		c.Native = b
	}
	for _, side := range []Side{SideCPU, SideGPU} {
		pts, err := decodePoints(side, obj[side.String()])
		if err != nil {
			return Curve{}, err
		}
		if side == SideCPU {
			c.CPU = pts
		} else {
			c.GPU = pts
		}
	}
	params := []struct {
		key string
		dst *float64
	}{{"hysteresis", &c.Hysteresis}, {"down_rate", &c.DownRate}, {"predict", &c.Predict}, {"couple", &c.Couple}, {"battery_shift", &c.BatteryShift}}
	for _, p := range params {
		raw, present := obj[p.key]
		if !present {
			continue
		}
		f, ok := rawNumber(raw)
		if !ok {
			return Curve{}, paramError(p.key)
		}
		*p.dst = f
	}
	return c.Validated()
}

// decodePoints reads one side's points, in the order of the Python checks: first that it is a
// list of 2..12 items, then that every item is a [temperature, percent] pair of numbers.
func decodePoints(side Side, raw json.RawMessage) ([]Point, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil || len(items) < MinPoints || len(items) > MaxPoints {
		return nil, firmware.Errorf("%s: 2 to 12 points", side)
	}
	pts := make([]Point, 0, len(items))
	for _, it := range items {
		var pair []*float64
		if err := json.Unmarshal(it, &pair); err != nil || len(pair) != 2 || pair[0] == nil || pair[1] == nil {
			return nil, firmware.Errorf("%s: every point is [temperature, percent]", side)
		}
		pts = append(pts, Point{*pair[0], *pair[1]})
	}
	return pts, nil
}

type paramRange struct {
	key    string
	lo, hi float64
}

var paramRanges = [...]paramRange{{"hysteresis", 0, 15}, {"down_rate", 0.2, 20}, {"predict", 0, 15}, {"couple", 0, 50}, {"battery_shift", 0, 15}}

func paramError(key string) error {
	for _, r := range paramRanges {
		if r.key == key {
			return firmware.Errorf("%s: %g..%g", key, r.lo, r.hi)
		}
	}
	return firmware.Errorf("%s: out of range", key)
}

// Validated checks a typed curve and returns a normalised copy: points sorted by
// temperature. It enforces the safety rules: temperatures 20..110 degC, duties 0 or 20..100,
// the duty never falls as the temperature rises, no two points share a temperature, and each
// side reaches 100 % by the temperature where that chip starts to throttle.
func (c Curve) Validated() (Curve, error) {
	out := c
	for _, side := range []struct {
		s    Side
		name string
		full float64
	}{{SideCPU, "cpu", cpuFullByC}, {SideGPU, "gpu", gpuFullByC}} {
		pts := append([]Point(nil), c.Points(side.s)...)
		if len(pts) < MinPoints || len(pts) > MaxPoints {
			return Curve{}, firmware.Errorf("%s: 2 to 12 points", side.name)
		}
		sort.SliceStable(pts, func(i, j int) bool {
			if pts[i][0] != pts[j][0] {
				return pts[i][0] < pts[j][0]
			}
			return pts[i][1] < pts[j][1]
		})
		for _, p := range pts {
			t, d := p.Temp(), p.Duty()
			// The negated comparisons also reject NaN.
			if !(MinTempC <= t && t <= MaxTempC) || !(d == 0 || (MinDutyPct <= d && d <= MaxDutyPct)) {
				return Curve{}, firmware.Errorf("%s: temperature 20..110 C, percent 0 (firmware auto) or 20..100", side.name)
			}
		}
		for i := 0; i+1 < len(pts); i++ {
			if pts[i+1].Duty() < pts[i].Duty() {
				return Curve{}, firmware.Errorf("%s: the duty may only rise with temperature", side.name)
			}
		}
		for i := 0; i+1 < len(pts); i++ {
			if pts[i].Temp() == pts[i+1].Temp() {
				return Curve{}, firmware.Errorf("%s: two points share a temperature", side.name)
			}
		}
		if Interp(pts, side.full) < 100 {
			return Curve{}, firmware.Errorf("%s: must reach 100 %% by %g C (the CPU and GPU throttle above that)", side.name, side.full)
		}
		if side.s == SideCPU {
			out.CPU = pts
		} else {
			out.GPU = pts
		}
	}
	values := map[string]float64{"hysteresis": c.Hysteresis, "down_rate": c.DownRate, "predict": c.Predict, "couple": c.Couple, "battery_shift": c.BatteryShift}
	for _, r := range paramRanges {
		if v := values[r.key]; !(r.lo <= v && v <= r.hi) {
			return Curve{}, paramError(r.key)
		}
	}
	return out, nil
}

// rawNumber decodes a JSON number; null, text and booleans are not numbers.
func rawNumber(raw json.RawMessage) (float64, bool) {
	var f *float64
	if json.Unmarshal(raw, &f) != nil || f == nil {
		return 0, false
	}
	return *f, true
}

func rawString(raw json.RawMessage) (string, bool) {
	var s *string
	if json.Unmarshal(raw, &s) != nil || s == nil {
		return "", false
	}
	return *s, true
}

func rawBool(raw json.RawMessage) (bool, bool) {
	var b *bool
	if json.Unmarshal(raw, &b) != nil || b == nil {
		return false, false
	}
	return *b, true
}
