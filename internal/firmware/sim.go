package firmware

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Simulator is an in-memory firmware that answers the module's protocol and rejects what the
// kernel would reject. It backs the unit tests and TRUESENSE_SIM=1 (trying the tools without
// hardware), so everything above runs unchanged.
type Simulator struct {
	mu sync.Mutex
	s  simState
}

type simState struct {
	mode, gpu, boot, overdrive, usb, kbtimeout int
	fan                                        string
	duty                                       [2]int
	battery                                    map[int]int
	rear                                       []byte
	zones                                      map[int]string
	temps                                      [3]int // cpu, sys, gpu
	rpm                                        [2]int
}

var (
	simOnce sync.Once
	simInst *Simulator
)

// Sim returns the process-wide simulator (state persists across calls, like real hardware).
func Sim() *Simulator {
	simOnce.Do(func() { simInst = NewSimulator() })
	return simInst
}

// NewSimulator returns a fresh simulator in the power-on state.
func NewSimulator() *Simulator { s := &Simulator{}; s.Reset(); return s }

// Reset puts the simulated firmware back to its power-on state.
func (m *Simulator) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	rear, _ := hex.DecodeString("00000300000100aec703000000000000")
	m.s = simState{mode: 6, gpu: 2, boot: 0, overdrive: 1, usb: 30, kbtimeout: 1, fan: "auto", duty: [2]int{30, 30},
		battery: map[int]int{1: 0, 2: 0}, rear: rear, zones: map[int]string{}, temps: [3]int{50, 52, 48}}
}

// SetSensors lets a test choose the temperatures (cpu, sys, gpu) and fan speeds (cpu, gpu) the simulator reports.
func (m *Simulator) SetSensors(temps [3]int, rpm [2]int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.s.temps, m.s.rpm = temps, rpm
}

// Snapshot is what tests read to check what was written.
type Snapshot struct {
	Mode, GPU, Boot, Overdrive, USB, KBTimeout int
	Fan                                        string
	Duty                                       [2]int
	Battery                                    map[int]int
	Zones                                      map[int]string
	Rear                                       []byte
}

// Snapshot returns a copy of the current simulated state.
func (m *Simulator) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := map[int]int{}
	for k, v := range m.s.battery {
		b[k] = v
	}
	z := map[int]string{}
	for k, v := range m.s.zones {
		z[k] = v
	}
	return Snapshot{Mode: m.s.mode, GPU: m.s.gpu, Boot: m.s.boot, Overdrive: m.s.overdrive, USB: m.s.usb, KBTimeout: m.s.kbtimeout,
		Fan: m.s.fan, Duty: m.s.duty, Battery: b, Zones: z, Rear: append([]byte(nil), m.s.rear...)}
}

const (
	errInvalid    = "err invalid argument\n"
	errMissing    = "err missing argument\n"
	errUnexpected = "err unexpected argument\n"
	errUnknown    = "err unknown command\n"
)

var takesArg = map[string]bool{"set_mode": true, "set_gpu": true, "set_boot": true, "get_misc": true, "set_overdrive": true, "set_fan_manual": true,
	"set_rear": true, "set_color": true, "set_usb": true, "set_kbtimeout": true, "get_battery": true, "set_battery": true}

// Run implements Device.
func (m *Simulator) Run(command string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, arg, _ := strings.Cut(strings.TrimSpace(command), " ")
	arg = strings.TrimSpace(arg)
	if takesArg[name] && arg == "" {
		return errMissing, nil
	}
	if !takesArg[name] && arg != "" {
		return errUnexpected, nil
	}
	return m.dispatch(name, arg), nil
}

func atoi(s string) (int, bool) { n, err := strconv.Atoi(s); return n, err == nil }

func oneOf(n int, set ...int) bool {
	for _, v := range set {
		if n == v {
			return true
		}
	}
	return false
}

func (m *Simulator) dispatch(name, a string) string {
	s := &m.s
	switch name {
	case "version":
		return "ok version=sim\n"
	case "caps":
		return "ok game=1 apge=1 battery=1\n"
	case "sensors":
		return fmt.Sprintf("ok cpu=%d sys=%d gpu=%d cpu_rpm=%d gpu_rpm=%d\n", s.temps[0], s.temps[1], s.temps[2], s.rpm[0], s.rpm[1])
	case "get_mode":
		return fmt.Sprintf("ok mode=%d\n", s.mode)
	case "get_modes":
		return "ok mask=0x73\n"
	case "set_mode":
		n, ok := atoi(a)
		if !ok || !oneOf(n, 0, 1, 4, 5, 6) {
			return errInvalid
		}
		s.mode = n
		return fmt.Sprintf("ok mode=%d\n", n)
	case "get_gpu":
		return fmt.Sprintf("ok gpu=%d\n", s.gpu)
	case "set_gpu":
		n, ok := atoi(a)
		if !ok || !oneOf(n, 1, 2, 3) {
			return errInvalid
		}
		s.gpu = n
		return fmt.Sprintf("ok gpu=%d\n", n)
	case "get_boot":
		return fmt.Sprintf("ok boot=%d\n", s.boot)
	case "set_boot":
		n, ok := atoi(a)
		if !ok || !oneOf(n, 0, 1) {
			return errInvalid
		}
		s.boot = n
		return fmt.Sprintf("ok boot=%d\n", n)
	case "get_overdrive":
		return fmt.Sprintf("ok overdrive=%d raw=0x0\n", s.overdrive)
	case "set_overdrive":
		n, ok := atoi(a)
		if !ok || !oneOf(n, 0, 1) {
			return errInvalid
		}
		s.overdrive = n
		return fmt.Sprintf("ok overdrive=%d\n", n)
	case "get_misc":
		n, ok := atoi(a)
		if !ok || n < 1 || n > 16 {
			return errInvalid
		}
		return fmt.Sprintf("ok idx=%s status=0 value=0\n", a)
	case "get_fan":
		beh := map[string]int{"auto": 0x41, "max": 0x82, "manual": 0xC3}[s.fan]
		return fmt.Sprintf("ok behavior=0x%x cpu=%d gpu=%d\n", beh, s.duty[0], s.duty[1])
	case "set_fan_auto":
		s.fan = "auto"
		return "ok fan=auto\n"
	case "set_fan_max":
		s.fan = "max"
		return "ok fan=max\n"
	case "set_fan_manual":
		p := strings.Fields(a)
		if len(p) != 2 {
			return errInvalid
		}
		c, ok1 := atoi(p[0])
		g, ok2 := atoi(p[1])
		if !ok1 || !ok2 || !validDuty(c) || !validDuty(g) {
			return errInvalid
		}
		s.fan, s.duty = "manual", [2]int{c, g}
		return fmt.Sprintf("ok fan=manual cpu=%d gpu=%d\n", c, g)
	case "get_rear":
		return "ok raw=" + hex.EncodeToString(s.rear) + "\n"
	case "get_slot":
		return "ok raw=00000564000100aec700000000000000\n"
	case "set_rear":
		b, err := hex.DecodeString(a)
		if err != nil || len(b) != 16 || !(b[0] <= 3 || b[0] == 7) || b[1] < 1 || b[1] > 5 || b[2] > 100 || b[3] != 0 || b[4] > 1 || b[8] != 3 || b[9] != 2 || anyNonZero(b[10:]) {
			return errInvalid
		}
		s.rear = append([]byte{0}, b[:15]...)
		return "ok\n"
	case "set_color":
		p := strings.Split(a, " ")
		if len(p[0]) != 6 || len(p) > 2 || (len(p) == 2 && p[1] != "1" && p[1] != "2" && p[1] != "4") {
			return errInvalid
		}
		if _, err := hex.DecodeString(p[0]); err != nil {
			return errInvalid
		}
		zone := 1
		if len(p) == 2 {
			zone, _ = strconv.Atoi(p[1])
		}
		s.zones[zone] = p[0]
		return "ok\n"
	case "get_usb":
		return fmt.Sprintf("ok usb=%d raw=0\n", s.usb)
	case "set_usb":
		n, ok := atoi(a)
		if !ok || !oneOf(n, 0, 10, 20, 30) {
			return errInvalid
		}
		s.usb = n
		return fmt.Sprintf("ok usb=%d\n", n)
	case "get_kbtimeout":
		return fmt.Sprintf("ok kbtimeout=%d raw=0x0\n", s.kbtimeout)
	case "set_kbtimeout":
		n, ok := atoi(a)
		if !ok || !oneOf(n, 0, 1) {
			return errInvalid
		}
		s.kbtimeout = n
		return fmt.Sprintf("ok kbtimeout=%d\n", n)
	case "get_battery":
		n, ok := atoi(a)
		if !ok || !oneOf(n, 1, 2) {
			return errInvalid
		}
		return fmt.Sprintf("ok battery=%d list=3\n", s.battery[n])
	case "set_battery":
		p := strings.Fields(a)
		if len(p) != 2 {
			return errInvalid
		}
		w, ok1 := atoi(p[0])
		v, ok2 := atoi(p[1])
		if !ok1 || !ok2 || !oneOf(w, 1, 2) || !oneOf(v, 0, 1) {
			return errInvalid
		}
		s.battery[w] = v
		return fmt.Sprintf("ok battery=%d value=%d\n", w, v)
	}
	return errUnknown
}

func validDuty(v int) bool { return v == 0 || (v >= 20 && v <= 100) }

func anyNonZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return true
		}
	}
	return false
}
