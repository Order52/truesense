package gui_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/gui"
	"github.com/Order52/truesense/internal/gui/guitest"
)

// documented returns the (area, verb) pairs of the table in COMMANDS.md.
func documented(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open("COMMANDS.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck // read-only
	row := regexp.MustCompile("^\\| `([a-z-]+)` \\| `([a-z-]+)`(?: / `([a-z-]+)`)? \\|")
	out := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if m := row.FindStringSubmatch(sc.Text()); m != nil {
			out[m[1]+"/"+m[2]] = true
			if m[3] != "" {
				out[m[1]+"/"+m[3]] = true
			}
		}
	}
	return out
}

// used returns the (area, verb) pairs the front-end calls, from its source.
func used(t *testing.T) map[string]bool {
	t.Helper()
	call := regexp.MustCompile(`\b(?:call|tryCall)\(\s*"([a-z-]+)",\s*"([a-z-]+)"`)
	out := map[string]bool{}
	err := filepath.WalkDir("web/js", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".js") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range call.FindAllStringSubmatch(string(b), -1) {
			out[m[1]+"/"+m[2]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// handledByTheServer are documented commands that never reach Commands.
var handledByTheServer = map[string]bool{"gui/ping": true, "gui/quit": true}

func TestEveryCommandTheFrontEndUsesIsDocumentedAndSimulated(t *testing.T) {
	doc, calls := documented(t), used(t)
	if len(calls) < 60 {
		t.Fatalf("only %d commands found in the front-end source: the pattern is broken", len(calls))
	}
	laptop := guitest.New()
	for _, c := range keys(calls) {
		if !doc[c] {
			t.Errorf("the front-end calls %s, which COMMANDS.md does not document", c)
		}
		if handledByTheServer[c] {
			continue
		}
		area, verb, _ := strings.Cut(c, "/")
		if _, err := laptop.Do(context.Background(), area, verb, json.RawMessage(`{}`)); errors.Is(err, gui.ErrUnknownCommand) {
			t.Errorf("the simulated laptop does not know %s", c)
		}
	}
	for _, c := range keys(doc) {
		if !calls[c] && c != "gui/ping" {
			t.Errorf("COMMANDS.md documents %s but the front-end never calls it: the table must stay minimal", c)
		}
	}
}

func TestWholeStackWithTheSimulatedLaptop(t *testing.T) {
	laptop := guitest.New()
	laptop.Tick = 20 * time.Millisecond
	urls := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- gui.Run(ctx, gui.Options{
			Commands: laptop, RuntimeDir: t.TempDir(), Log: slog.New(slog.DiscardHandler),
			Open: func(u string) error { urls <- u; return nil },
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	u, err := url.Parse(<-urls)
	if err != nil {
		t.Fatal(err)
	}
	frag, _ := url.ParseQuery(u.Fragment)
	base := "http://" + u.Host
	exchReq, _ := http.NewRequestWithContext(t.Context(), "POST", base+"/api/gui/session", strings.NewReader(`{"code":"`+frag.Get("code")+`"}`))
	exchReq.Header.Set("Content-Type", "application/json")
	exch, err := http.DefaultClient.Do(exchReq)
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		Result struct {
			Token string `json:"token"`
		} `json:"result"`
	}
	if err := json.NewDecoder(exch.Body).Decode(&session); err != nil || session.Result.Token == "" {
		t.Fatalf("exchanging the launch code: %v", err)
	}
	_ = exch.Body.Close()
	token := session.Result.Token

	post := func(area, verb, body string) (int, map[string]any) {
		req, _ := http.NewRequestWithContext(t.Context(), "POST", base+"/api/"+area+"/"+verb, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close() //nolint:errcheck // test
		var out map[string]any
		b, _ := io.ReadAll(res.Body)
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("%s/%s: %v (%s)", area, verb, err, b)
		}
		return res.StatusCode, out
	}

	status, out := post("power", "set", `{"mode":"turbo"}`)
	if status != 200 || out["result"] != "turbo" {
		t.Fatalf("power set: %d %v", status, out)
	}
	status, out = post("status", "get", `{}`)
	if status != 200 || out["result"].(map[string]any)["power_mode"] != "turbo" {
		t.Fatalf("status after the change: %d %v", status, out)
	}
	if status, out = post("fan", "set", `{"cpu":10,"gpu":10}`); status != 422 || !strings.Contains(out["error"].(map[string]any)["message"].(string), "20..100") {
		t.Fatalf("bad fan duty: %d %v", status, out)
	}
	if status, _ = post("nonsense", "verb", `{}`); status != 404 {
		t.Fatalf("unknown command: %d", status)
	}

	req, _ := http.NewRequestWithContext(t.Context(), "GET", base+"/api/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test
	sc := bufio.NewScanner(res.Body)
	deadline := time.After(3 * time.Second)
	lines := make(chan string)
	go func() {
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("stream ended")
			}
			if data, ok := strings.CutPrefix(l, "data: "); ok {
				var msg map[string]any
				if err := json.Unmarshal([]byte(data), &msg); err != nil {
					t.Fatal(err)
				}
				for _, k := range []string{"sensors", "power_mode", "ac", "battery_pct", "fans", "load", "power", "daemon"} {
					if _, ok := msg[k]; !ok {
						t.Errorf("stream message lacks %q: %s", k, data)
					}
				}
				if msg["power_mode"] != "turbo" {
					t.Errorf("stream shows %v, want the mode the page just set", msg["power_mode"])
				}
				return
			}
		case <-deadline:
			t.Fatal("no stream event")
		}
	}
}
