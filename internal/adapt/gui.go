package adapt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/assembly"
	"github.com/Order52/truesense/internal/gui"
	"github.com/Order52/truesense/internal/ipc"
)

// GUITick is the interval of the window's live stream.
const GUITick = time.Second

// guiBackend is gui.Commands over one API: every row of internal/gui/COMMANDS.md is one handler,
// written on the same api methods the command line and the terminal UI use, so the three can
// never drift apart. A command that stops a fan, applies an undervolt or sets a power limit
// beyond the danger line is refused by the api layer unless it carries the confirmation, whatever
// the page asked the user first.
type guiBackend struct {
	a        *api.API
	handlers map[string]handler
	tick     time.Duration
}

type handler func(ctx context.Context, a args) (any, error)

// GUI returns the commands of the web window.
func GUI(a *api.API, _ *assembly.Services) gui.Commands {
	b := &guiBackend{a: a, handlers: map[string]handler{}, tick: GUITick}
	b.registerStatus()
	b.registerPower()
	b.registerFan()
	b.registerHardware()
	b.registerKeyboard()
	b.registerLooks()
	b.registerKeys()
	b.registerAudio()
	b.registerSystem()
	return b
}

// Do runs one command of the window.
func (b *guiBackend) Do(ctx context.Context, area, verb string, raw json.RawMessage) (any, error) {
	h, ok := b.handlers[area+"/"+verb]
	if !ok {
		return nil, fmt.Errorf("%w: %s %s", gui.ErrUnknownCommand, area, verb)
	}
	a, err := parseArgs(raw)
	if err != nil {
		return nil, err
	}
	res, err := h(ctx, a)
	if v := reflect.ValueOf(res); v.Kind() == reflect.Pointer && v.IsNil() {
		res = nil // a nil pointer is JSON null: say so plainly
	}
	return res, coded(err)
}

// Stream sends the live values about once a second until ctx ends.
func (b *guiBackend) Stream(ctx context.Context, emit func(json.RawMessage)) {
	t := time.NewTicker(b.tick)
	defer t.Stop()
	for {
		if msg, err := json.Marshal(b.a.Live(ctx)); err == nil {
			emit(msg)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// codedError carries a machine-readable code next to the message, so the page can explain what
// to do ("needs-daemon": start the service).
type codedError struct {
	error
	code string
}

func (e codedError) Code() string  { return e.code }
func (e codedError) Unwrap() error { return e.error }

func coded(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ipc.ErrNotRunning):
		return codedError{err, "needs-daemon"}
	case errors.Is(err, ipc.ErrDenied):
		return codedError{err, "no-permission"}
	}
	return err
}

// ---- arguments: a JSON object whose members are named like the command line's long options

type args struct{ m map[string]json.RawMessage }

func parseArgs(raw json.RawMessage) (args, error) {
	a := args{m: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return a, nil
	}
	if err := json.Unmarshal(raw, &a.m); err != nil {
		return a, fmt.Errorf("bad arguments: %w", err)
	}
	return a, nil
}

// has reports whether a member is present and not null.
func (a args) has(key string) bool {
	v, ok := a.m[key]
	return ok && !bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}

func (a args) decode(key string, out any) error {
	if err := json.Unmarshal(a.m[key], out); err != nil {
		return fmt.Errorf("%s: wrong kind of value", key)
	}
	return nil
}

func (a args) str(key string) (string, error) {
	if !a.has(key) {
		return "", nil
	}
	var s string
	return s, a.decode(key, &s)
}

// mustStr is a string member that has to be there.
func (a args) mustStr(key string) (string, error) {
	s, err := a.str(key)
	if err == nil && s == "" {
		err = fmt.Errorf("%s is needed", key)
	}
	return s, err
}

func (a args) intOpt(key string) (*int, error) {
	if !a.has(key) {
		return nil, nil
	}
	var f float64
	if err := a.decode(key, &f); err != nil {
		return nil, err
	}
	if f != float64(int(f)) {
		return nil, fmt.Errorf("%s must be a whole number", key)
	}
	n := int(f)
	return &n, nil
}

func (a args) mustInt(key string) (int, error) {
	n, err := a.intOpt(key)
	if err == nil && n == nil {
		err = fmt.Errorf("%s is needed", key)
	}
	if err != nil {
		return 0, err
	}
	return *n, nil
}

func (a args) floatOpt(key string) (*float64, error) {
	if !a.has(key) {
		return nil, nil
	}
	var f float64
	if err := a.decode(key, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (a args) boolean(key string) (bool, error) {
	if !a.has(key) {
		return false, nil
	}
	var v bool
	return v, a.decode(key, &v)
}

func (a args) strings(key string) ([]string, error) {
	if !a.has(key) {
		return nil, nil
	}
	var v []string
	return v, a.decode(key, &v)
}

// action is the command line's positional action, with its default.
func (a args) action(def string) (string, error) {
	s, err := a.str("action")
	if err != nil || s == "" {
		return def, err
	}
	return s, nil
}

// onOff is the on / off / toggle / status pattern of every boolean setting: the answer is the
// new state.
func onOff(a args, get func() (bool, error), set func(bool) (bool, error), words ...string) (any, error) {
	action, err := a.action("status")
	if err != nil {
		return nil, err
	}
	if len(words) == 0 {
		words = []string{"on", "off", "toggle", "status"}
	}
	known := false
	for _, w := range words {
		known = known || w == action
	}
	if !known {
		return nil, fmt.Errorf("action: %s", joinWords(words))
	}
	cur, err := get()
	if err != nil {
		return nil, err
	}
	switch action {
	case "status":
		return cur, nil
	case "toggle":
		return set(!cur)
	}
	return set(action == "on")
}

func joinWords(w []string) string {
	out := ""
	for i, s := range w {
		if i > 0 {
			out += " | "
		}
		out += s
	}
	return out
}

// withBuiltin adds "builtin" to the JSON object of a profile.
func withBuiltin(v any, builtin bool) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	m["builtin"] = builtin
	return m, nil
}

func names(n []string) []string {
	if n == nil {
		return []string{}
	}
	return n
}
