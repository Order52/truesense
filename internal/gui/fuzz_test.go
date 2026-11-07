package gui

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzCommandRequest throws arbitrary bodies, content types, paths and tokens at the command
// bus: it must never panic, never answer 5xx for a bad request, never pass anything that is
// not a JSON object to the command, and never run a command without the token.
func FuzzCommandRequest(f *testing.F) {
	f.Add("/api/power/set", "application/json", `{"mode":"turbo"}`, true)
	f.Add("/api/power/set", "application/json", `[1,2]`, true)
	f.Add("/api/power/set", "text/plain", `{}`, true)
	f.Add("/api/power/set", "application/json", `{"a":`, true)
	f.Add("/api/../../etc/passwd", "application/json", `{}`, true)
	f.Add("/api/power/set", "application/json", `{}`, false)
	f.Add("/api/gui/session", "application/json", `{"code":"x"}`, false)
	f.Add("/api/a/b", "application/json;charset=utf-8", "\x00\xff{", true)

	var sawBad bool
	srv := newServer("127.0.0.1:1", testToken, Options{
		CommandTimeout: 200_000_000, Log: slog.New(slog.DiscardHandler),
		Commands: stub{do: func(_ context.Context, area, verb string, args json.RawMessage) (any, error) {
			var obj map[string]any
			if json.Unmarshal(args, &obj) != nil {
				sawBad = true
			}
			return "ok", nil
		}},
	}, func() {})
	f.Fuzz(func(t *testing.T, path, contentType, body string, withToken bool) {
		if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, " \r\n\x00") {
			return // not a request a client could make
		}
		req, err := http.NewRequestWithContext(t.Context(), "POST", "http://127.0.0.1:1"+path, strings.NewReader(body))
		if err != nil {
			return
		}
		req.Host = "127.0.0.1:1"
		req.Header.Set("Content-Type", contentType)
		if withToken {
			req.Header.Set("Authorization", "Bearer "+testToken)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code >= 500 {
			t.Fatalf("%d for %q %q %q", rec.Code, path, contentType, body)
		}
		if !withToken && strings.HasPrefix(path, "/api/") && path != "/api/gui/session" && rec.Code == 200 {
			t.Fatalf("a command ran without a token: %q", path)
		}
		if sawBad {
			t.Fatalf("a body that is not a JSON object reached the command: %q", body)
		}
		_, _ = io.Copy(io.Discard, rec.Body)
	})
}

// FuzzLockFile: whatever is in the lock file, reading it never panics and a file without a
// holder is never reported as a running server.
func FuzzLockFile(f *testing.F) {
	f.Add(`{"port":4321,"token":"abc","pid":1}`)
	f.Add(`{"port":-1}`)
	f.Add(``)
	f.Add(`not json`)
	f.Add(`{"port":99999999999999999999,"token":""}`)
	f.Fuzz(func(t *testing.T, content string) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, lockName), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := readLive(dir); ok {
			t.Fatalf("a lock file nobody holds counted as live: %q", content)
		}
		inst, err := acquire(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer inst.release()
		if err := inst.f.Truncate(0); err != nil {
			t.Fatal(err)
		}
		if _, err := inst.f.WriteAt([]byte(content), 0); err != nil {
			t.Fatal(err)
		}
		info, ok := readLive(dir) // held, with arbitrary content: valid only if it parses to a usable port and token
		if ok && (info.Port <= 0 || info.Token == "") {
			t.Fatalf("unusable info accepted: %+v from %q", info, content)
		}
	})
}

// FuzzStreamState: states from Commands.Stream are untrusted text as far as the SSE framing
// goes. Whatever arrives must reach listeners as one line or not at all.
func FuzzStreamState(f *testing.F) {
	f.Add(`{"a":1}`)
	f.Add("{\n\"a\":\n1}")
	f.Add("{\"a\":\"line\\nbreak\"}")
	f.Add("data: x\n\n")
	f.Add(`{`)
	f.Fuzz(func(t *testing.T, state string) {
		h := newHub(stub{}, slog.New(slog.DiscardHandler))
		ch, unsub := h.subscribe()
		defer unsub()
		h.publish(context.Background(), json.RawMessage(state))
		select {
		case msg := <-ch:
			if strings.ContainsAny(string(msg), "\r\n") || !json.Valid(msg) {
				t.Fatalf("framing broken: %q from %q", msg, state)
			}
		default:
			if json.Valid([]byte(state)) {
				t.Fatalf("a valid state was dropped: %q", state)
			}
		}
	})
}
