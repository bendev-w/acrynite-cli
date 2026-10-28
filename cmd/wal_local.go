package cmd

// Helpers that let 'wal enable' finish the setup itself when it runs as root on the PostgreSQL
// host: granting the registered role what it needs, checking that the postgres OS user can run
// archive_command, and restarting the server. Every change is confirmed first.

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"acrynite-cli/internal/database"

	"github.com/spf13/cobra"
)

var walAssumeYes bool

// postgresSocketDir is where the server's Unix socket lives for cfg.
func postgresSocketDir(cfg *database.DBConfig) string {
	if strings.HasPrefix(cfg.Host, "/") {
		return cfg.Host
	}
	return "/var/run/postgresql"
}

// localServer reports whether cfg points at a PostgreSQL server running on this Linux machine.
func localServer(cfg *database.DBConfig) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	switch cfg.Host {
	case "", "localhost", "127.0.0.1", "::1":
	default:
		if !strings.HasPrefix(cfg.Host, "/") {
			return false
		}
	}
	_, err := os.Stat(filepath.Join(postgresSocketDir(cfg), fmt.Sprintf(".s.PGSQL.%d", cfg.Port)))
	return err == nil
}

// localAdmin reports whether this process can act as the postgres superuser for cfg: root on the
// database host, connecting through peer authentication on the local socket.
func localAdmin(cfg *database.DBConfig) bool {
	if !localServer(cfg) || os.Geteuid() != 0 {
		return false
	}
	for _, tool := range []string{"runuser", "psql", "systemctl"} {
		if _, err := exec.LookPath(tool); err != nil {
			return false
		}
	}
	return true
}

// psqlAsPostgres runs sql in cfg's database as the postgres OS user and superuser.
func psqlAsPostgres(cfg *database.DBConfig, sql string) (string, error) {
	out, err := exec.Command("runuser", "-u", "postgres", "--", "psql", "-X", "-q", "-At", "-v", "ON_ERROR_STOP=1",
		"-h", postgresSocketDir(cfg), "-p", strconv.Itoa(cfg.Port), "-d", cfg.Database, "-c", sql).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("psql as postgres: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// confirm asks a yes/no question; without a terminal (and without --yes) the answer is no.
func confirm(cmd *cobra.Command, question string) bool {
	out := cmd.OutOrStdout()
	if walAssumeYes {
		fmt.Fprintf(out, "%s [y/N] y (--yes)\n", question)
		return true
	}
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok {
		if info, err := f.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	fmt.Fprintf(out, "%s [y/N] ", question)
	return yesAnswer(in)
}

func yesAnswer(in io.Reader) bool {
	line, _ := bufio.NewReader(in).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// walGrantSQL lists the grants a non-superuser role needs for 'wal enable'. ALTER SYSTEM grants are
// cluster-wide; the pg_reload_conf grant only applies to the database it is run in.
func walGrantSQL(role string, serverVersionNum int) (string, error) {
	if serverVersionNum < 150000 {
		return "", fmt.Errorf("PostgreSQL %d cannot grant ALTER SYSTEM to other roles (15 or newer is required); register the database with a superuser to enable WAL archiving", serverVersionNum/10000)
	}
	r := quoteIdent(role)
	return "GRANT ALTER SYSTEM ON PARAMETER archive_mode, archive_command, archive_timeout TO " + r + "; " +
		"GRANT EXECUTE ON FUNCTION pg_catalog.pg_reload_conf() TO " + r + ";", nil
}

// offerWALGrants asks to grant cfg.User the privileges 'wal enable' needs and applies them as postgres.
func offerWALGrants(cmd *cobra.Command, db *sql.DB, cfg *database.DBConfig) error {
	var versionNum int
	if err := db.QueryRow("SHOW server_version_num").Scan(&versionNum); err != nil {
		return fmt.Errorf("read server version: %w", err)
	}
	grants, err := walGrantSQL(cfg.User, versionNum)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Role %q is not allowed to configure WAL archiving. Acrynite can run, as the local postgres superuser:\n  %s\n", cfg.User, grants)
	fmt.Fprintln(cmd.OutOrStdout(), "Note: a role that can set archive_command can run commands as the postgres OS user.")
	if !confirm(cmd, "Grant these permissions now?") {
		return fmt.Errorf("permissions were not granted; run the statements above as a superuser in database %q, then retry", cfg.Database)
	}
	if _, err := psqlAsPostgres(cfg, grants); err != nil {
		return fmt.Errorf("grant permissions: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Granted WAL archiving permissions to %q.\n", cfg.User)
	return nil
}

// postgresCan reports whether the postgres OS user passes test(1) check flag on path.
func postgresCan(flag, path string) bool {
	return exec.Command("runuser", "-u", "postgres", "--", "test", flag, path).Run() == nil
}

// archiverAccessProblems lists what the postgres OS user cannot reach when archive_command runs.
func archiverAccessProblems(exe, workDir, walDir string) []string {
	checks := []struct{ flag, path, problem string }{
		{"-x", workDir, "cannot enter the Acrynite working directory"},
		{"-x", exe, "cannot execute the Acrynite binary"},
		{"-r", filepath.Join(workDir, "storage", "configs", "catalog.json"), "cannot read the Acrynite catalog"},
	}
	if walDir != "" {
		checks = append(checks, struct{ flag, path, problem string }{"-w", walDir, "cannot write to the WAL directory"})
	}
	var problems []string
	for _, c := range checks {
		if !postgresCan(c.flag, c.path) {
			problems = append(problems, c.problem+" "+c.path)
		}
	}
	return problems
}

// prepareArchiverAccess offers to give postgres the WAL directory, then reports what remains unreachable.
func prepareArchiverAccess(cmd *cobra.Command, exe, workDir, walDir string) []string {
	out := cmd.OutOrStdout()
	if walDir != "" && !postgresCan("-w", walDir) && confirm(cmd, fmt.Sprintf("The postgres user cannot write to %s. Make postgres its owner?", walDir)) {
		if res, err := exec.Command("chown", "-R", "postgres:postgres", walDir).CombinedOutput(); err != nil {
			fmt.Fprintf(out, "Warning: chown %s: %v: %s\n", walDir, err, strings.TrimSpace(string(res)))
		}
	}
	problems := archiverAccessProblems(exe, workDir, walDir)
	for _, p := range problems {
		fmt.Fprintf(out, "Warning: the postgres user %s\n", p)
	}
	if len(problems) > 0 {
		fmt.Fprintln(out, "archive_command runs as the postgres OS user; until it can reach these paths, WAL will pile up in pg_wal instead of being archived.")
		fmt.Fprintln(out, "Run Acrynite from a directory postgres can access (for example /var/lib/acrynite) with a --path it can write to, then rerun 'wal enable'.")
	}
	return problems
}

var debianCluster = regexp.MustCompile(`^(\d+)/([A-Za-z0-9_.-]+)$`)

// postgresServiceName maps Debian/Ubuntu cluster names such as "18/main" to their systemd unit.
func postgresServiceName(clusterName string) string {
	if m := debianCluster.FindStringSubmatch(clusterName); m != nil {
		return "postgresql@" + m[1] + "-" + m[2]
	}
	return "postgresql"
}

// restartPostgres restarts the service and waits until the server reports archive_mode on.
func restartPostgres(cfg *database.DBConfig, service string) error {
	if out, err := exec.Command("systemctl", "restart", service).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart %s: %w: %s", service, err, strings.TrimSpace(string(out)))
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		mode, err := psqlAsPostgres(cfg, "SHOW archive_mode")
		if err == nil {
			if mode != "on" && mode != "always" {
				return fmt.Errorf("PostgreSQL restarted but archive_mode is %q", mode)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("PostgreSQL did not come back within 30s after restart: %w", err)
		}
		time.Sleep(time.Second)
	}
}
