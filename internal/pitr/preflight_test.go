package pitr

import (
	"errors"
	"strings"
	"testing"

	"acrynite-cli/internal/location"
)

func TestReportErrOnlyForFailures(t *testing.T) {
	r := &Report{}
	r.add(Pass, "a", "ok", "")
	r.add(Warn, "b", "meh", "")
	if err := r.Err(); err != nil {
		t.Fatalf("warnings must not fail the report: %v", err)
	}

	r.add(Fail, "replication role", "permission denied: role 'bk' lacks REPLICATION", `run: ALTER ROLE "bk" WITH REPLICATION;`)
	err := r.Err()
	var pfErr *PreflightError
	if !errors.As(err, &pfErr) || len(pfErr.Failures) != 1 {
		t.Fatalf("expected one failure, got %v", err)
	}
	for _, want := range []string{"replication role", "permission denied", "ALTER ROLE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestReportAccessMapsErrors(t *testing.T) {
	r := &Report{}
	reportAccess(r, "ok", nil, "fine")
	reportAccess(r, "unsupported", location.ErrWriteCheckUnsupported, "")
	reportAccess(r, "denied", &location.AccessError{Target: "s3://b/", Reason: "access denied (s3:PutObject)", Hint: "allow it", Denied: true}, "")

	want := []Status{Pass, Warn, Fail}
	for i, c := range r.Checks {
		if c.Status != want[i] {
			t.Errorf("%s: status %s, want %s", c.Name, c.Status, want[i])
		}
	}
	if r.Checks[2].Hint != "allow it" {
		t.Errorf("hint not propagated: %q", r.Checks[2].Hint)
	}
}

func TestIsLocalHost(t *testing.T) {
	for host, want := range map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true, "": true, "db.example.com": false, "10.1.2.3": false} {
		if got := isLocalHost(host); got != want {
			t.Errorf("isLocalHost(%q) = %v, want %v", host, got, want)
		}
	}
}
