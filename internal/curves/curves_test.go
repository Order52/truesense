package curves

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestBuiltinsAreValidAndNamedInOrder(t *testing.T) {
	want := []string{"native", "truesense", "quiet", "performance", "max-cooling"}
	if got := BuiltinNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("BuiltinNames = %v, want %v", got, want)
	}
	for _, n := range want {
		c, ok := Builtin(n)
		if !ok {
			t.Fatalf("%s: not built in", n)
		}
		v, err := c.Validated()
		if err != nil {
			t.Fatalf("%s: built-in curve fails its own validation: %v", n, err)
		}
		if !reflect.DeepEqual(v, c) {
			t.Errorf("%s: validation changed a built-in: %v vs %v", n, v, c)
		}
	}
	if c, _ := Builtin("native"); !c.Native {
		t.Error("native curve must be marked native")
	}
	if c, _ := Builtin("quiet"); c.Predict != 3 || c.DownRate != 1 || c.Hysteresis != 4 {
		t.Errorf("quiet parameters: %+v", c)
	}
}

func TestBuiltinReturnsACopy(t *testing.T) {
	a, _ := Builtin("truesense")
	a.CPU[0] = Point{99, 99}
	b, _ := Builtin("truesense")
	if b.CPU[0] == (Point{99, 99}) {
		t.Fatal("mutating a returned built-in changed the next one")
	}
}

func TestInterp(t *testing.T) {
	c, _ := Builtin("truesense")
	tests := []struct {
		name string
		pts  []Point
		t    float64
		want float64
	}{
		{"truesense cpu at 80 C (Python test)", c.CPU, 80, 70},
		{"above the last point clamps", c.CPU, 100, 100},
		{"below the first point clamps", c.CPU, 10, 0},
		{"exactly on the first point", c.CPU, 52, 0},
		{"midway between 56:30 and 62:38", c.CPU, 59, 34},
		{"exactly on an inner point", c.CPU, 68, 48},
		{"empty list is firmware auto", nil, 50, 0},
		{"single point", []Point{{50, 40}}, 70, 40},
		{"equal temperatures do not divide by zero", []Point{{50, 20}, {50, 60}, {60, 100}}, 50, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Interp(tt.pts, tt.t); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("Interp(%v) = %v, want %v", tt.t, got, tt.want)
			}
		})
	}
}

func TestParsePoints(t *testing.T) {
	tests := []struct {
		in      string
		want    []Point
		wantErr bool
	}{
		{"52:0,60:35,70:55,88:100", []Point{{52, 0}, {60, 35}, {70, 55}, {88, 100}}, false},
		{" 52 : 0 , 60:35.5 ,", []Point{{52, 0}, {60, 35.5}}, false},
		{"", nil, false},
		{",,", nil, false},
		{"52", nil, true},
		{"52:0:1", nil, true},
		{"a:b", nil, true},
		{"52:", nil, true},
		{"nan:0", nil, true},
		{"50:inf", nil, true},
		{"1e999:1", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParsePoints(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "points look like") {
				t.Errorf("unhelpful message %q", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func okCurve() map[string]any {
	return map[string]any{
		"cpu": []any{[]any{50, 0}, []any{95, 100}},
		"gpu": []any{[]any{50, 0}, []any{85, 100}},
	}
}

func with(m map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func TestValidate(t *testing.T) {
	pts := func(p ...[2]float64) []any {
		var out []any
		for _, x := range p {
			out = append(out, []any{x[0], x[1]})
		}
		return out
	}
	many := make([][2]float64, 13)
	for i := range many {
		many[i] = [2]float64{float64(40 + i*5), 100}
	}
	tests := []struct {
		name    string
		in      any
		wantErr string // substring; empty = valid
	}{
		{"minimal valid curve", okCurve(), ""},
		{"not an object", []any{1, 2}, "a curve is an object"},
		{"null", nil, "a curve is an object"},
		{"text", "curve", "a curve is an object"},
		{"weak curve from the Python test", map[string]any{"cpu": pts([2]float64{50, 0}, [2]float64{70, 50}), "gpu": pts([2]float64{50, 0}, [2]float64{78, 100})}, "cpu: must reach 100 % by 95 C"},
		{"gpu must reach 100 by 85", with(okCurve(), "gpu", pts([2]float64{50, 0}, [2]float64{90, 100})), "gpu: must reach 100 % by 85 C"},
		{"missing cpu", map[string]any{"gpu": okCurve()["gpu"]}, "cpu: 2 to 12 points"},
		{"cpu not a list", with(okCurve(), "cpu", "x"), "cpu: 2 to 12 points"},
		{"one point", with(okCurve(), "cpu", pts([2]float64{90, 100})), "cpu: 2 to 12 points"},
		{"thirteen points", with(okCurve(), "cpu", pts(many...)), "cpu: 2 to 12 points"},
		{"twelve points are fine", with(okCurve(), "cpu", pts(many[:12]...)), ""},
		{"point with three items", with(okCurve(), "cpu", []any{[]any{50, 0, 1}, []any{95, 100}}), "cpu: every point is [temperature, percent]"},
		{"point with text", with(okCurve(), "cpu", []any{[]any{"hot", 0}, []any{95, 100}}), "cpu: every point is [temperature, percent]"},
		{"point not a list", with(okCurve(), "gpu", []any{5, []any{85, 100}}), "gpu: every point is [temperature, percent]"},
		{"temperature below 20", with(okCurve(), "cpu", pts([2]float64{19, 0}, [2]float64{95, 100})), "temperature 20..110"},
		{"temperature above 110", with(okCurve(), "cpu", pts([2]float64{50, 0}, [2]float64{111, 100})), "temperature 20..110"},
		{"duty 10 is not allowed (fans run at 0 or 20..100)", with(okCurve(), "cpu", pts([2]float64{50, 10}, [2]float64{95, 100})), "percent 0 (firmware auto) or 20..100"},
		{"duty 19", with(okCurve(), "cpu", pts([2]float64{50, 19}, [2]float64{95, 100})), "percent 0 (firmware auto) or 20..100"},
		{"duty 20 is the minimum", with(okCurve(), "cpu", pts([2]float64{50, 20}, [2]float64{95, 100})), ""},
		{"duty above 100", with(okCurve(), "cpu", pts([2]float64{50, 0}, [2]float64{95, 101})), "percent 0 (firmware auto) or 20..100"},
		{"duty falls with temperature", with(okCurve(), "cpu", pts([2]float64{50, 50}, [2]float64{60, 40}, [2]float64{95, 100})), "may only rise"},
		{"duplicate temperature", with(okCurve(), "cpu", pts([2]float64{50, 20}, [2]float64{50, 40}, [2]float64{95, 100})), "share a temperature"},
		{"unsorted points are sorted", with(okCurve(), "cpu", pts([2]float64{95, 100}, [2]float64{50, 0})), ""},
		{"hysteresis too big", with(okCurve(), "hysteresis", 16), "hysteresis: 0..15"},
		{"hysteresis negative", with(okCurve(), "hysteresis", -1), "hysteresis: 0..15"},
		{"hysteresis as text", with(okCurve(), "hysteresis", "4"), "hysteresis: 0..15"},
		{"hysteresis as bool", with(okCurve(), "hysteresis", true), "hysteresis: 0..15"},
		{"hysteresis null", with(okCurve(), "hysteresis", nil), "hysteresis: 0..15"},
		{"down_rate too small", with(okCurve(), "down_rate", 0.1), "down_rate: 0.2..20"},
		{"down_rate too big", with(okCurve(), "down_rate", 21), "down_rate: 0.2..20"},
		{"predict too big", with(okCurve(), "predict", 15.5), "predict: 0..15"},
		{"couple too big", with(okCurve(), "couple", 51), "couple: 0..50"},
		{"battery_shift too big", with(okCurve(), "battery_shift", 16), "battery_shift: 0..15"},
		{"all limits at their edge", with(okCurve(), "hysteresis", 15, "down_rate", 0.2, "predict", 15, "couple", 50, "battery_shift", 15), ""},
		{"unknown keys are ignored", with(okCurve(), "bogus", 1), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Validate(tt.in)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				for _, side := range []Side{SideCPU, SideGPU} {
					p := got.Points(side)
					for i := 1; i < len(p); i++ {
						if p[i][0] <= p[i-1][0] {
							t.Errorf("%s points not strictly sorted: %v", side, p)
						}
					}
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateFillsDefaultsAndKeepsGivenValues(t *testing.T) {
	c, err := Validate(with(okCurve(), "description", "mine", "predict", 7.5))
	if err != nil {
		t.Fatal(err)
	}
	want := Curve{Description: "mine", CPU: []Point{{50, 0}, {95, 100}}, GPU: []Point{{50, 0}, {85, 100}}, Hysteresis: 4, DownRate: 1.5, Predict: 7.5, Couple: 15, BatteryShift: 3}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v\nwant %+v", c, want)
	}
}

func TestJSONKeysMatchPython(t *testing.T) {
	c, _ := Builtin("quiet")
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"description", "cpu", "gpu", "hysteresis", "down_rate", "predict", "couple", "battery_shift"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in %s", k, b)
		}
	}
	if _, ok := m["native"]; ok {
		t.Error("native must be omitted for ordinary curves")
	}
	if !strings.Contains(string(b), `"cpu":[[58,0],[66,30]`) {
		t.Errorf("points must marshal as whole numbers: %s", b)
	}
	n, _ := Builtin("native")
	if b, _ := json.Marshal(n); !strings.Contains(string(b), `"native":true`) {
		t.Errorf("native curve lost its flag: %s", b)
	}
}

func FuzzParsePoints(f *testing.F) {
	for _, s := range []string{"52:0,60:35,70:55,88:100", "", " 1 : 2 ,", "a:b", "1:2:3", "nan:1", "1e400:1", "-1:-1", ":", ",", "0x10:1", "1_0:5"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		pts, err := ParsePoints(s)
		if err != nil {
			return
		}
		for _, p := range pts {
			if math.IsNaN(p[0]) || math.IsInf(p[0], 0) || math.IsNaN(p[1]) || math.IsInf(p[1], 0) {
				t.Fatalf("ParsePoints(%q) returned a non-finite point %v", s, p)
			}
		}
		// What parses must survive a round trip through the JSON the daemon sends.
		b, err := json.Marshal(pts)
		if err != nil {
			t.Fatalf("points %v do not marshal: %v", pts, err)
		}
		var back []Point
		if err := json.Unmarshal(b, &back); err != nil || (len(pts) > 0 && !reflect.DeepEqual(back, pts)) {
			t.Fatalf("round trip %v -> %s -> %v (%v)", pts, b, back, err)
		}
	})
}

func FuzzValidate(f *testing.F) {
	for _, s := range []string{
		`{"cpu":[[50,0],[95,100]],"gpu":[[50,0],[85,100]]}`,
		`{"cpu":[[50,0],[95,100]],"gpu":[[50,0],[85,100]],"hysteresis":3,"down_rate":2,"predict":1,"couple":10,"battery_shift":2,"description":"x","native":true}`,
		`null`, `[]`, `{}`, `{"cpu":"x"}`, `{"cpu":[[1,2,3]]}`, `{"cpu":[[50,0],[95,100]],"gpu":[[50,0],[85,100]],"hysteresis":"4"}`,
		`{"cpu":[[1e999,0],[95,100]],"gpu":[]}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := Parse(data)
		if err != nil {
			return
		}
		// Whatever is accepted must satisfy every safety rule, and validating the result again
		// must change nothing.
		for _, side := range []struct {
			s    Side
			full float64
		}{{SideCPU, 95}, {SideGPU, 85}} {
			pts := c.Points(side.s)
			if len(pts) < 2 || len(pts) > 12 {
				t.Fatalf("accepted %d points", len(pts))
			}
			for _, p := range pts {
				if p[0] < 20 || p[0] > 110 || !(p[1] == 0 || (p[1] >= 20 && p[1] <= 100)) {
					t.Fatalf("accepted point %v", p)
				}
			}
			if Interp(pts, side.full) < 100 {
				t.Fatalf("accepted a curve that is not at 100%% by %v C: %v", side.full, pts)
			}
		}
		again, err := c.Validated()
		if err != nil || !reflect.DeepEqual(again, c) {
			t.Fatalf("not idempotent: %+v -> %+v (%v)", c, again, err)
		}
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		round, err := Parse(b)
		if err != nil || !reflect.DeepEqual(round, c) {
			t.Fatalf("JSON round trip: %+v -> %s -> %+v (%v)", c, b, round, err)
		}
	})
}
