package api_test

import (
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/apitest"
)

// machine is a private laptop and the API over it.
func machine(t *testing.T) (*apitest.Fixture, *api.API) {
	t.Helper()
	f := apitest.New(t)
	return f, f.API()
}

// wantErr fails unless err contains text.
func wantErr(t *testing.T, err error, text string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), text) {
		t.Fatalf("error = %v, want one containing %q", err, text)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }
