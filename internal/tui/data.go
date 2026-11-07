package tui

import (
	"context"
	"sync"
	"time"
)

// historyLen is how many readings the sparklines remember (one per second).
const historyLen = 160

// History keys of the sparklines.
const (
	histTCPU = "tcpu"
	histTGPU = "tgpu"
	histCPU  = "cpu"
	histGPU  = "gpu"
	histRPM1 = "rpm1"
	histRPM2 = "rpm2"
)

// ring is a fixed-size history of readings, oldest first.
type ring struct {
	vals []int
}

func (r *ring) push(v int) {
	if len(r.vals) == historyLen {
		copy(r.vals, r.vals[1:])
		r.vals = r.vals[:historyLen-1]
	}
	r.vals = append(r.vals, v)
}

// Read is one slow read of the open page: Fn runs in the background and its result is
// kept under Key.
type Read struct {
	Key string
	Fn  func(ctx context.Context) (any, error)
}

// Data holds the latest readings. Fast (every second) is what the dashboard shows; slow
// reads cover the settings of the page that is open and never run twice at once. Every
// accessor is safe from any goroutine.
type Data struct {
	b      Backend
	onTick func() // called after every change of the readings; must not block or take the App lock

	mu      sync.Mutex
	svc     *ServiceState
	sens    Sensors
	hasSens bool
	fan     *FanState
	mode    string
	ac      Opt[bool]
	batt    Opt[Battery]
	err     string
	hist    map[string]*ring
	x       map[string]any
	errs    map[string]string

	slowBusy  bool
	slowAgain []Read
	wake      chan struct{}
}

// NewData makes an empty Data reading from b; onChange is called after each update.
func NewData(b Backend, onChange func()) *Data {
	if onChange == nil {
		onChange = func() {}
	}
	d := &Data{b: b, onTick: onChange, hist: map[string]*ring{}, x: map[string]any{}, errs: map[string]string{}, wake: make(chan struct{}, 1)}
	for _, k := range []string{histTCPU, histTGPU, histCPU, histGPU, histRPM1, histRPM2} {
		d.hist[k] = &ring{}
	}
	return d
}

// errText is an error as shown to the user.
func errText(err error) string {
	if s := err.Error(); s != "" {
		return s
	}
	return "error"
}

// Fast reads the dashboard's data once. The service's readings are preferred (it already
// read them this second, so the hardware is asked only for the fan duty); a failing read
// keeps the previous values and sets the "No device" message.
func (d *Data) Fast(ctx context.Context) {
	svc, err := d.b.ServiceState(ctx)
	if err != nil {
		svc = nil
	}
	d.mu.Lock()
	sens, hasSens, fan, mode, ac := d.sens, d.hasSens, d.fan, d.mode, d.ac
	d.mu.Unlock()

	var rerr error
	switch {
	case svc != nil && svc.Sensors != nil:
		sens, hasSens = *svc.Sensors, true
	default:
		var s Sensors
		if s, rerr = d.b.Sensors(ctx); rerr == nil {
			sens, hasSens = s, true
		}
	}
	if rerr == nil {
		var f FanState
		if f, rerr = d.b.FanState(ctx); rerr == nil {
			fan = &f
		}
	}
	if rerr == nil {
		if svc != nil && validMode(svc.PowerMode) {
			mode = svc.PowerMode
		} else {
			mode, rerr = d.b.PowerMode(ctx)
		}
	}
	if rerr == nil {
		if svc != nil && svc.AC.OK {
			ac = svc.AC
		} else {
			var v bool
			if v, rerr = d.b.OnAC(ctx); rerr == nil {
				ac = Some(v)
			}
		}
	}
	batt, battOK := d.b.Battery()

	d.mu.Lock()
	d.svc, d.sens, d.hasSens, d.fan, d.mode, d.ac = svc, sens, hasSens, fan, mode, ac
	d.batt = Opt[Battery]{V: batt, OK: battOK}
	d.err = ""
	if rerr != nil {
		d.err = errText(rerr)
	} else {
		d.hist[histTCPU].push(sens.CPUTemp)
		d.hist[histTGPU].push(sens.GPUTemp)
		d.hist[histRPM1].push(sens.CPUFanRPM)
		d.hist[histRPM2].push(sens.GPUFanRPM)
		cpu, gpu := 0, 0
		if svc != nil {
			cpu, gpu = svc.CPULoad.V, svc.GPULoad.V
		}
		d.hist[histCPU].push(cpu)
		d.hist[histGPU].push(gpu)
	}
	d.mu.Unlock()
	d.onTick()
}

// Loop reads every second (or when woken) until ctx ends.
func (d *Data) Loop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		d.Fast(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-d.wake:
		}
	}
}

// Wake asks the Loop to read again now.
func (d *Data) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Slow starts the reads of a page in the background; if a batch is already running, the
// newest request runs after it (reads never overlap).
func (d *Data) Slow(ctx context.Context, reads []Read) {
	d.mu.Lock()
	if d.slowBusy {
		d.slowAgain = reads
		d.mu.Unlock()
		return
	}
	d.slowBusy = true
	d.mu.Unlock()
	go func() {
		for reads != nil {
			d.SlowSync(ctx, reads)
			d.mu.Lock()
			reads, d.slowAgain = d.slowAgain, nil
			if reads == nil {
				d.slowBusy = false
			}
			d.mu.Unlock()
		}
	}()
}

// SlowSync runs the reads on the caller's goroutine (the tests).
func (d *Data) SlowSync(ctx context.Context, reads []Read) {
	for _, r := range reads {
		v, err := r.Fn(ctx)
		d.mu.Lock()
		if err != nil {
			d.errs[r.Key] = errText(err)
		} else {
			d.x[r.Key] = v
			delete(d.errs, r.Key)
		}
		d.mu.Unlock()
		d.onTick()
	}
}

// Set stores a result of an action (the health check) under key.
func (d *Data) Set(key string, v any) {
	d.mu.Lock()
	d.x[key] = v
	d.mu.Unlock()
	d.onTick()
}

// Get returns a slow reading.
func (d *Data) Get(key string) (any, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.x[key]
	return v, ok
}

// ReadError returns the error of the last slow read of key.
func (d *Data) ReadError(key string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.errs[key]
	return e, ok
}

// view is a consistent copy of the fast readings for one drawing.
type view struct {
	svc     *ServiceState
	sens    Sensors
	hasSens bool
	fan     *FanState
	mode    string
	ac      Opt[bool]
	batt    Opt[Battery]
	err     string
	hist    map[string][]int
}

// View copies the fast readings.
func (d *Data) view() view {
	d.mu.Lock()
	defer d.mu.Unlock()
	v := view{svc: d.svc, sens: d.sens, hasSens: d.hasSens, fan: d.fan, mode: d.mode, ac: d.ac, batt: d.batt, err: d.err, hist: map[string][]int{}}
	for k, r := range d.hist {
		v.hist[k] = append([]int(nil), r.vals...)
	}
	return v
}

// daemon is true when the service answered.
func (d *Data) daemon() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.svc != nil
}

// service returns a copy of the service state (zero value when none).
func (d *Data) service() ServiceState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.svc == nil {
		return ServiceState{}
	}
	return *d.svc
}

func (d *Data) fanState() *FanState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fan == nil {
		return nil
	}
	f := *d.fan
	return &f
}

func (d *Data) powerMode() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.mode
}

func (d *Data) sensors() (Sensors, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sens, d.hasSens
}

// getAs returns a slow reading of type T.
func getAs[T any](d *Data, key string) (T, bool) {
	v, ok := d.Get(key)
	if !ok {
		var zero T
		return zero, false
	}
	t, ok := v.(T)
	return t, ok
}
