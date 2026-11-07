package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Result is one line of the report. The JSON names are the 0.3.x ones.
type Result struct {
	Check  string `json:"check"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

// Report is the whole health check.
type Report struct {
	Version  string   `json:"version"`
	Kernel   string   `json:"kernel"`
	OK       bool     `json:"ok"`
	Failures int      `json:"failures"`
	Warnings int      `json:"warnings"`
	Checks   []Result `json:"checks"`
}

// ExitCode is 1 if anything FAILed (WARN does not fail), else 0.
func (r Report) ExitCode() int {
	if r.Failures > 0 {
		return 1
	}
	return 0
}

// Run executes the checks side by side, at most e.Workers at a time (they only read and mostly
// wait for other programs), and reports them in their usual order. A check that panics is a
// WARN that says so, never a crashed doctor.
func Run(ctx context.Context, e *Env, checks []Check) Report {
	results := make([]Result, len(checks))
	sem := make(chan struct{}, e.workers())
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = runOne(ctx, e, c)
		}()
	}
	wg.Wait()
	rep := Report{Version: e.Version, Kernel: e.Kernel, Checks: results}
	for _, r := range results {
		switch r.Status {
		case FAIL:
			rep.Failures++
		case WARN:
			rep.Warnings++
		}
	}
	rep.OK = rep.Failures == 0
	return rep
}

func runOne(ctx context.Context, e *Env, c Check) (res Result) {
	defer func() {
		if p := recover(); p != nil {
			res = Result{Check: c.Title, Status: WARN, Detail: fmt.Sprintf("check crashed: %v", p)}
		}
	}()
	o := c.Run(ctx, e)
	return Result{Check: c.Title, Status: o.Status, Detail: o.Detail, Fix: o.Fix}
}

// RenderJSON writes the report as indented JSON.
func RenderJSON(w io.Writer, r Report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

var ansi = map[Status]string{OK: "32", WARN: "33", FAIL: "31", INFO: "36"}

// RenderText writes the report for a person; colour adds ANSI colours to the status tags.
func RenderText(w io.Writer, r Report, colour bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "TrueSense %s doctor (%s)\n", r.Version, r.Kernel)
	for _, c := range r.Checks {
		tag := fmt.Sprintf("%-4s", c.Status)
		if colour {
			tag = "\033[" + ansi[c.Status] + "m" + tag + "\033[0m"
		}
		fmt.Fprintf(&b, "  [%s] %-16s %s\n", tag, c.Check, c.Detail)
		if c.Fix != "" && (c.Status == WARN || c.Status == FAIL) {
			fmt.Fprintf(&b, "         fix: %s\n", c.Fix)
		}
	}
	verdict := "PROBLEMS FOUND"
	switch {
	case r.Failures == 0 && r.Warnings == 0:
		verdict = "ALL GOOD"
	case r.Failures == 0:
		verdict = "OK with warnings"
	}
	fmt.Fprintf(&b, "\n%s: %d failure(s), %d warning(s)\n", verdict, r.Failures, r.Warnings)
	_, _ = io.WriteString(w, b.String())
}
