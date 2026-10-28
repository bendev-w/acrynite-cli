package cmd

import (
	"strings"
	"testing"
)

func TestPostgresServiceName(t *testing.T) {
	for cluster, want := range map[string]string{
		"18/main":    "postgresql@18-main",
		"16/reports": "postgresql@16-reports",
		"":           "postgresql",
		"prod":       "postgresql",
	} {
		if got := postgresServiceName(cluster); got != want {
			t.Errorf("postgresServiceName(%q) = %q, want %q", cluster, got, want)
		}
	}
}

func TestWALGrantSQL(t *testing.T) {
	got, err := walGrantSQL(`acrynite"user`, 180006)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`GRANT ALTER SYSTEM ON PARAMETER archive_mode, archive_command, archive_timeout TO "acrynite""user";`,
		`GRANT EXECUTE ON FUNCTION pg_catalog.pg_reload_conf() TO "acrynite""user";`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if _, err := walGrantSQL("acrynite_user", 140010); err == nil {
		t.Fatal("expected an error before PostgreSQL 15")
	}
}

func TestYesAnswer(t *testing.T) {
	for input, want := range map[string]bool{"y\n": true, "YES\n": true, " yes \n": true, "\n": false, "n\n": false, "": false, "maybe\n": false} {
		if got := yesAnswer(strings.NewReader(input)); got != want {
			t.Errorf("yesAnswer(%q) = %v, want %v", input, got, want)
		}
	}
}
