// Package pitr verifies that everything needed for point-in-time recovery (pg_basebackup
// plus continuous WAL archiving) is permitted before a schedule is created or run.
package pitr

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"acrynite-cli/internal/backup"
	"acrynite-cli/internal/database"
	"acrynite-cli/internal/location"
	"acrynite-cli/internal/wal"
	"acrynite-cli/utils"

	"github.com/lib/pq"
)

// Status is the outcome of one check.
type Status string

const (
	Pass Status = "pass"
	Warn Status = "warn"
	Fail Status = "fail"
)

// Check is a single pre-flight result. Hint tells the user how to fix a Warn or Fail.
type Check struct {
	Name    string
	Status  Status
	Message string
	Hint    string
}

// Report collects all check results in the order they ran.
type Report struct {
	Checks []Check
}

func (r *Report) add(status Status, name, message, hint string) {
	r.Checks = append(r.Checks, Check{Name: name, Status: status, Message: message, Hint: hint})
}

// Failures returns only the checks that must be fixed before backups can work.
func (r *Report) Failures() []Check {
	var failed []Check
	for _, c := range r.Checks {
		if c.Status == Fail {
			failed = append(failed, c)
		}
	}
	return failed
}

// Err returns a *PreflightError when any check failed, or nil.
func (r *Report) Err() error {
	if failed := r.Failures(); len(failed) > 0 {
		return &PreflightError{Failures: failed}
	}
	return nil
}

// Log prints every check result through the CLI logger (and therefore the log file).
func (r *Report) Log() {
	for _, c := range r.Checks {
		switch c.Status {
		case Pass:
			utils.Success("[PASS] %s: %s", c.Name, c.Message)
		case Warn:
			utils.Warning("[WARN] %s: %s", c.Name, c.Message)
		default:
			utils.Error("[FAIL] %s: %s", c.Name, c.Message)
		}
		if c.Hint != "" && c.Status != Pass {
			utils.Info("       fix: %s", c.Hint)
		}
	}
}

// PreflightError lists every failed check, each with its fix hint.
type PreflightError struct {
	Failures []Check
}

func (e *PreflightError) Error() string {
	var b strings.Builder
	b.WriteString("point-in-time recovery pre-flight checks failed:")
	for _, c := range e.Failures {
		fmt.Fprintf(&b, "\n  - %s: %s", c.Name, c.Message)
		if c.Hint != "" {
			fmt.Fprintf(&b, " (fix: %s)", c.Hint)
		}
	}
	return b.String()
}

// Options describes where backups go and what must be verified.
type Options struct {
	// Location receives base backups and (under "wal") archived WAL segments. May be nil.
	Location *location.Config
	// StagingDir is where pg_basebackup output is written before upload.
	StagingDir string
	// RequireArchive is true when WAL archiving must be (or be made) operational, i.e.
	// when creating a schedule; missing archiving privileges then fail instead of warn.
	RequireArchive bool
	// WALSpoolDir/WALContainerDir describe a host folder shared with a containerised server.
	WALSpoolDir      string
	WALContainerDir  string
	// WALMode is auto, stream, direct or spool.
	WALMode string
}

// checkStreaming verifies what pg_receivewal needs: the binary, and enough replication capacity
// (a base backup streams WAL on one connection while the receiver holds another).
func checkStreaming(r *Report, db *sql.DB, cfg *database.DBConfig) {
	exe, err := backup.ResolveReceiveWALExecutable(cfg.DumpPath)
	if err != nil {
		r.add(Fail, "pg_receivewal", err.Error(), "install the PostgreSQL client tools (pg_receivewal ships with pg_basebackup) or set ACRYNITE_PG_RECEIVEWAL_PATH")
		return
	}
	if out, runErr := exec.Command(exe, "--version").CombinedOutput(); runErr != nil {
		r.add(Fail, "pg_receivewal", fmt.Sprintf("cannot execute %s: %s", exe, strings.TrimSpace(string(out))+" "+runErr.Error()), "check the file exists and this user may execute it")
	} else {
		r.add(Pass, "pg_receivewal", fmt.Sprintf("%s (%s)", exe, strings.TrimSpace(string(out))), "")
	}

	var senders, slots int
	if err := db.QueryRow("SELECT current_setting('max_wal_senders')::int, current_setting('max_replication_slots')::int").Scan(&senders, &slots); err != nil {
		r.add(Warn, "replication capacity", "could not read max_wal_senders/max_replication_slots: "+err.Error(), "")
		return
	}
	if senders < 3 {
		r.add(Fail, "replication capacity", fmt.Sprintf("max_wal_senders is %d; a base backup plus a WAL receiver need at least 3", senders), "ALTER SYSTEM SET max_wal_senders = 10; then restart PostgreSQL")
	} else if slots < 1 {
		r.add(Fail, "replication capacity", "max_replication_slots is 0; a replication slot is needed so WAL is kept until it is received", "ALTER SYSTEM SET max_replication_slots = 10; then restart PostgreSQL")
	} else {
		r.add(Pass, "replication capacity", fmt.Sprintf("max_wal_senders=%d, max_replication_slots=%d", senders, slots), "")
	}
}

var versionPattern = regexp.MustCompile(`(\d+)(?:\.\d+)*`)

// Run executes all pre-flight checks. It only stops early when later checks depend on a
// failed one (no database connection), so a single run reports every problem it can find.
func Run(cfg *database.DBConfig, opts Options) *Report {
	r := &Report{}

	if cfg.Type != "postgres" {
		r.add(Fail, "database type", fmt.Sprintf("'%s' is %s; pg_basebackup and WAL archiving need PostgreSQL", cfg.Name, cfg.Type), "use --method dump for non-PostgreSQL databases")
		return r
	}

	clientMajor := checkBaseBackupTool(r, cfg)
	checkDestination(r, opts)

	db, err := database.ConnectPostgresDB(cfg.ConnectionString())
	if err != nil {
		r.add(Fail, "database connection", classifyConnectError(err), "verify host, port, user, password and ssl mode with 'acrynite db show'")
		return r
	}
	defer db.Close()
	r.add(Pass, "database connection", fmt.Sprintf("connected to %s:%d as '%s'", cfg.Host, cfg.Port, cfg.User), "")

	server, detectErr := wal.DetectServer(db)
	mode := opts.WALMode
	if detectErr == nil {
		var modeErr error
		mode, modeErr = wal.ResolveMode(opts.WALMode, opts.WALSpoolDir != "", server)
		if modeErr != nil {
			r.add(Fail, "WAL archive mode", modeErr.Error(), "use --wal-mode stream")
			mode = wal.ModeStream
		}
	}
	switch {
	case detectErr != nil:
		r.add(Warn, "server OS", detectErr.Error(), "")
		checkHostAndExecutable(r, cfg)
	case mode == wal.ModeStream:
		r.add(Pass, "WAL archive mode", fmt.Sprintf("stream: pg_receivewal over the replication connection (server runs on %s); nothing is installed on or mounted into the server", server.OS), "")
		checkStreaming(r, db, cfg)
	case mode == wal.ModeSpool:
		if opts.WALSpoolDir == "" {
			opts.WALSpoolDir = wal.DefaultSpoolDir()
		}
		r.add(Pass, "WAL archive mode", fmt.Sprintf("spool: shared folder %s (server runs on %s)", opts.WALSpoolDir, server.OS), "")
		checkSpool(r, db, opts)
	default:
		r.add(Pass, "WAL archive mode", fmt.Sprintf("direct: server runs on %s, the same as this CLI, with its data directory on this machine; Acrynite will be used as archive_command", server.OS), "")
		checkHostAndExecutable(r, cfg)
	}

	checkServerVersion(r, db, clientMajor)
	isSuper := checkReplicationRole(r, db)
	checkReplicationHBA(r, db)
	checkWALSettings(r, db)
	checkArchiving(r, db, isSuper, opts.RequireArchive)
	checkWALDirectoryAccess(r, db)
	return r
}

func checkBaseBackupTool(r *Report, cfg *database.DBConfig) int {
	exe, err := backup.ResolveBaseBackupExecutable(cfg.DumpPath)
	if err != nil {
		r.add(Fail, "pg_basebackup", err.Error(), "install the PostgreSQL client tools or set ACRYNITE_PG_BASEBACKUP_PATH")
		return 0
	}
	out, err := exec.Command(exe, "--version").CombinedOutput()
	if err != nil {
		reason := strings.TrimSpace(string(out))
		if reason == "" {
			reason = err.Error()
		}
		hint := "check the file exists and is a working pg_basebackup binary"
		lower := strings.ToLower(reason + err.Error())
		if strings.Contains(lower, "permission denied") || strings.Contains(lower, "access is denied") || strings.Contains(lower, "application control") {
			hint = "this user is not allowed to execute pg_basebackup; fix file permissions or the execution policy"
		}
		r.add(Fail, "pg_basebackup", fmt.Sprintf("cannot execute %s: %s", exe, reason), hint)
		return 0
	}
	major := 0
	if m := versionPattern.FindStringSubmatch(string(out)); m != nil {
		major, _ = strconv.Atoi(m[1])
	}
	r.add(Pass, "pg_basebackup", fmt.Sprintf("%s (%s)", exe, strings.TrimSpace(string(out))), "")
	return major
}

func checkDestination(r *Report, opts Options) {
	if opts.StagingDir != "" {
		staging := &location.Config{Name: "staging", Type: location.TypeLocal, Path: opts.StagingDir}
		reportAccess(r, "staging directory", staging.CheckWriteAccess(""), "write test succeeded in "+opts.StagingDir)
	}
	if opts.Location == nil {
		return
	}
	loc := opts.Location
	reportAccess(r, "backup location", loc.CheckWriteAccess(""), fmt.Sprintf("can write base backups to %s location '%s'", loc.Type, loc.Name))
	reportAccess(r, "WAL archive location", loc.CheckWriteAccess("wal"), fmt.Sprintf("can write WAL segments to %s location '%s' (wal/)", loc.Type, loc.Name))
}

func reportAccess(r *Report, name string, err error, okMessage string) {
	var accessErr *location.AccessError
	switch {
	case err == nil:
		r.add(Pass, name, okMessage, "")
	case errors.Is(err, location.ErrWriteCheckUnsupported):
		r.add(Warn, name, "write access could not be verified for this location type", "")
	case errors.As(err, &accessErr):
		r.add(Fail, name, fmt.Sprintf("%s: %s", accessErr.Target, accessErr.Reason), accessErr.Hint)
	default:
		r.add(Fail, name, err.Error(), "")
	}
}

// checkSpool verifies the shared folder: writable on the host, and writable by the PostgreSQL
// server at its container path, and that both paths are the same folder.
func checkSpool(r *Report, db *sql.DB, opts Options) {
	hostDir, err := filepath.Abs(opts.WALSpoolDir)
	if err != nil {
		r.add(Fail, "WAL spool", "invalid --wal-spool-dir: "+err.Error(), "")
		return
	}
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		r.add(Fail, "WAL spool", fmt.Sprintf("cannot create %s: %v", hostDir, err), "choose a folder this user can write to")
		return
	}
	hostCfg := &location.Config{Name: "wal-spool", Type: location.TypeLocal, Path: hostDir}
	if err := hostCfg.CheckWriteAccess(""); err != nil {
		reportAccess(r, "WAL spool (host)", err, "")
		return
	}
	r.add(Pass, "WAL spool (host)", "host folder is writable: "+hostDir, "")

	containerDir := strings.TrimRight(opts.WALContainerDir, "/")
	if containerDir == "" {
		containerDir = wal.DefaultContainerSpoolDir
	}
	probe := fmt.Sprintf(".acrynite-probe-%d", time.Now().UnixNano())
	serverPath := containerDir + "/" + probe
	_, err = db.Exec("COPY (SELECT 'acrynite') TO '" + strings.ReplaceAll(serverPath, "'", "''") + "'")
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "42501" {
			r.add(Warn, "WAL spool (server)", "cannot verify the server can write to "+containerDir+" (needs superuser or pg_write_server_files)", "")
			return
		}
		r.add(Fail, "WAL spool (server)", fmt.Sprintf("PostgreSQL cannot write to %s: %v", containerDir, err),
			fmt.Sprintf("mount the host folder into the container at %s (e.g. -v \"%s:%s\") and make it writable by the postgres user", containerDir, hostDir, containerDir))
		return
	}
	hostProbe := filepath.Join(hostDir, probe)
	if _, statErr := os.Stat(hostProbe); statErr != nil {
		r.add(Fail, "WAL spool (server)", fmt.Sprintf("PostgreSQL wrote to %s but the file did not appear in %s", containerDir, hostDir),
			fmt.Sprintf("the container path is not the host folder; check the volume mapping %s:%s", hostDir, containerDir))
		return
	}
	_ = os.Remove(hostProbe)
	r.add(Pass, "WAL spool (server)", fmt.Sprintf("PostgreSQL can write to %s, which maps to %s", containerDir, hostDir), "")
}

// checkHostAndExecutable covers the parts of WAL archiving that run on the database host:
// PostgreSQL executes archive_command itself, as its own OS user.
func checkHostAndExecutable(r *Report, cfg *database.DBConfig) {
	if !isLocalHost(cfg.Host) {
		r.add(Warn, "archive host", fmt.Sprintf("database host '%s' is not this machine; archive_command runs on the database server", cfg.Host),
			"install Acrynite and its storage configuration on the database host, or archive WAL another way")
	}

	exe, err := os.Executable()
	if err != nil {
		r.add(Warn, "archive_command executable", "cannot resolve the Acrynite executable path: "+err.Error(), "")
		return
	}
	workDir, _ := os.Getwd()
	if runtime.GOOS == "windows" {
		r.add(Pass, "archive_command access", fmt.Sprintf("executable %s; the PostgreSQL service account needs read/execute on it and read access to %s", exe, workDir), "")
		return
	}
	info, err := os.Stat(exe)
	if err == nil && info.Mode().Perm()&0o001 == 0 {
		r.add(Warn, "archive_command access", fmt.Sprintf("%s is not executable by other users, so the postgres OS user may not be able to run archive_command", exe), "chmod o+x on the binary and its parent directories")
		return
	}
	if dirInfo, dirErr := os.Stat(workDir); dirErr == nil && dirInfo.Mode().Perm()&0o001 == 0 {
		r.add(Warn, "archive_command access", fmt.Sprintf("working directory %s is not traversable by other users, so the postgres OS user may not read the Acrynite config", workDir), "chmod o+x on the directory and its parents")
		return
	}
	r.add(Pass, "archive_command access", fmt.Sprintf("executable %s is accessible to other users", exe), "")
}

func isLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	}
	if name, err := os.Hostname(); err == nil && strings.EqualFold(name, host) {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}

func classifyConnectError(err error) string {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Code {
		case "28P01", "28000":
			return "authentication failed: " + pqErr.Message
		case "3D000":
			return "database does not exist: " + pqErr.Message
		case "42501":
			return "permission denied: " + pqErr.Message
		}
	}
	return err.Error()
}

func checkServerVersion(r *Report, db *sql.DB, clientMajor int) {
	var num int
	if err := db.QueryRow("SELECT current_setting('server_version_num')::int").Scan(&num); err != nil {
		r.add(Warn, "server version", "could not read server version: "+err.Error(), "")
		return
	}
	serverMajor := num / 10000
	if serverMajor < 10 {
		r.add(Fail, "server version", fmt.Sprintf("PostgreSQL %d is too old", serverMajor), "PostgreSQL 10 or newer is required")
		return
	}
	if clientMajor > 0 && clientMajor < serverMajor {
		r.add(Fail, "server version", fmt.Sprintf("pg_basebackup %d is older than server %d", clientMajor, serverMajor), "install a pg_basebackup of the same or newer major version")
		return
	}
	r.add(Pass, "server version", fmt.Sprintf("PostgreSQL %d", serverMajor), "")
}

func checkReplicationRole(r *Report, db *sql.DB) bool {
	var user string
	var isSuper, hasReplication bool
	err := db.QueryRow("SELECT current_user, rolsuper, rolreplication FROM pg_roles WHERE rolname = current_user").Scan(&user, &isSuper, &hasReplication)
	if err != nil {
		r.add(Warn, "replication role", "could not inspect the role's attributes: "+err.Error(), "")
		return false
	}
	if !isSuper && !hasReplication {
		r.add(Fail, "replication role", fmt.Sprintf("permission denied: role '%s' is neither superuser nor has the REPLICATION attribute, so pg_basebackup cannot connect", user),
			fmt.Sprintf(`run: ALTER ROLE "%s" WITH REPLICATION;`, user))
		return false
	}
	if isSuper {
		r.add(Pass, "replication role", fmt.Sprintf("role '%s' is a superuser", user), "")
	} else {
		r.add(Pass, "replication role", fmt.Sprintf("role '%s' has the REPLICATION attribute", user), "")
	}
	return isSuper
}

func checkReplicationHBA(r *Report, db *sql.DB) {
	var matches int
	var clientAddr sql.NullString
	_ = db.QueryRow(`SELECT host(inet_client_addr())`).Scan(&clientAddr)

	query := `SELECT count(*) FROM pg_hba_file_rules
		WHERE error IS NULL AND 'replication' = ANY(database)
		AND (user_name && ARRAY[current_user::text, 'all'])
		AND auth_method <> 'reject'`
	var args []interface{}
	if clientAddr.Valid && clientAddr.String != "" {
		// Only rules that cover the address the server actually sees for this client
		query += ` AND type LIKE 'host%' AND (
			address IN ('all', 'samehost', 'samenet')
			OR (address ~ '^[0-9a-fA-F:.]+(/[0-9]+)?$' AND
				CASE WHEN family(address::inet) <> family($1::inet) THEN false
				     WHEN netmask IS NOT NULL THEN ($1::inet & netmask::inet) = address::inet
				     ELSE $1::inet <<= address::inet END))`
		args = append(args, clientAddr.String)
	}
	err := db.QueryRow(query, args...).Scan(&matches)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "42501" {
			r.add(Warn, "pg_hba.conf", "cannot read pg_hba_file_rules (requires superuser); replication access will be verified when pg_basebackup runs", "")
			return
		}
		r.add(Warn, "pg_hba.conf", "could not inspect pg_hba.conf rules: "+err.Error(), "")
		return
	}
	if matches == 0 {
		hint := "add 'host replication <user> <client-ip>/32 scram-sha-256' to pg_hba.conf and run SELECT pg_reload_conf();"
		msg := "permission denied: pg_hba.conf has no 'replication' entry for this user"
		if clientAddr.Valid && clientAddr.String != "" {
			msg += fmt.Sprintf(" and client address %s (the address the server sees)", clientAddr.String)
			hint = fmt.Sprintf("add 'host replication all %s/32 scram-sha-256' (or the whole subnet) to pg_hba.conf and run SELECT pg_reload_conf();", clientAddr.String)
		}
		r.add(Fail, "pg_hba.conf", msg, hint)
		return
	}
	r.add(Pass, "pg_hba.conf", fmt.Sprintf("%d replication rule(s) match this user", matches), "")
}


func checkWALSettings(r *Report, db *sql.DB) {
	var walLevel string
	if err := db.QueryRow("SHOW wal_level").Scan(&walLevel); err != nil {
		r.add(Warn, "wal_level", "could not read wal_level: "+err.Error(), "")
	} else if walLevel == "minimal" {
		r.add(Fail, "wal_level", "wal_level is 'minimal'; base backups and WAL archiving are not possible", "set wal_level = replica and restart PostgreSQL")
	} else {
		r.add(Pass, "wal_level", walLevel, "")
	}

	var senders int
	if err := db.QueryRow("SELECT current_setting('max_wal_senders')::int").Scan(&senders); err != nil {
		r.add(Warn, "max_wal_senders", "could not read max_wal_senders: "+err.Error(), "")
		return
	}
	if senders < 2 {
		r.add(Fail, "max_wal_senders", fmt.Sprintf("max_wal_senders is %d; pg_basebackup streams WAL over a second connection and needs at least 2", senders), "set max_wal_senders = 10 and restart PostgreSQL")
		return
	}
	r.add(Pass, "max_wal_senders", strconv.Itoa(senders), "")
}

func checkArchiving(r *Report, db *sql.DB, isSuper, requireArchive bool) {
	var mode, command string
	if err := db.QueryRow("SHOW archive_mode").Scan(&mode); err != nil {
		r.add(Warn, "archive_mode", "could not read archive_mode: "+err.Error(), "")
		return
	}
	if err := db.QueryRow("SHOW archive_command").Scan(&command); err != nil {
		r.add(Warn, "archive_command", "could not read archive_command: "+err.Error(), "")
		return
	}
	enabled := mode == "on" || mode == "always"
	ours := strings.Contains(command, "backup wal push")
	ready := enabled && ours

	switch {
	case ready:
		r.add(Pass, "WAL archiving", "archive_mode is on and archive_command pushes to Acrynite", "")
	case enabled && command != "" && command != "(disabled)":
		r.add(Warn, "WAL archiving", "archive_mode is on but archive_command is not Acrynite's; enabling PITR will replace it", "back up the existing archive_command first if another tool depends on it")
	default:
		r.add(Warn, "WAL archiving", "archive_mode/archive_command are not configured yet; 'backup wal enable' will set them", "")
	}

	if !ready {
		checkAlterSystemPrivilege(r, db, isSuper, requireArchive)
		return
	}
	checkArchiverHealth(r, db)
}

func checkAlterSystemPrivilege(r *Report, db *sql.DB, isSuper, requireArchive bool) {
	canConfigure := isSuper
	if !isSuper {
		var num int
		if err := db.QueryRow("SELECT current_setting('server_version_num')::int").Scan(&num); err == nil && num >= 150000 {
			var alterMode, alterCommand bool
			if err := db.QueryRow(`SELECT has_parameter_privilege(current_user, 'archive_mode', 'ALTER SYSTEM'),
				has_parameter_privilege(current_user, 'archive_command', 'ALTER SYSTEM')`).Scan(&alterMode, &alterCommand); err == nil {
				canConfigure = alterMode && alterCommand
			}
		}
	}
	var canReload bool
	if err := db.QueryRow("SELECT has_function_privilege(current_user, 'pg_reload_conf()', 'EXECUTE')").Scan(&canReload); err != nil {
		canReload = isSuper
	}

	if canConfigure && canReload {
		r.add(Pass, "WAL archiving privilege", "role may run ALTER SYSTEM for archive settings and reload the configuration", "")
		return
	}
	status := Warn
	if requireArchive {
		status = Fail
	}
	r.add(status, "WAL archiving privilege",
		"permission denied: this role cannot ALTER SYSTEM archive_mode/archive_command or run pg_reload_conf(), so WAL archiving cannot be enabled automatically",
		"connect as a superuser and run 'acrynite backup wal enable <schedule-id>', or GRANT ALTER SYSTEM ON PARAMETER archive_mode, archive_command (PostgreSQL 15+) and EXECUTE ON FUNCTION pg_reload_conf()")
}

// checkArchiverHealth reads pg_stat_archiver, which reveals whether archive_command is
// really succeeding (e.g. the postgres OS user can run Acrynite and write to the location).
func checkArchiverHealth(r *Report, db *sql.DB) {
	var archived, failed int64
	var lastArchived, lastFailed sql.NullTime
	var lastFailedWAL sql.NullString
	err := db.QueryRow(`SELECT archived_count, failed_count, last_archived_time, last_failed_time, last_failed_wal FROM pg_stat_archiver`).
		Scan(&archived, &failed, &lastArchived, &lastFailed, &lastFailedWAL)
	if err != nil {
		r.add(Warn, "WAL archiver", "could not read pg_stat_archiver: "+err.Error(), "")
		return
	}
	if lastFailed.Valid && (!lastArchived.Valid || lastFailed.Time.After(lastArchived.Time)) {
		r.add(Fail, "WAL archiver", fmt.Sprintf("archiving is failing: last failure %s for WAL segment %s (%d failed, %d archived)",
			lastFailed.Time.Format(time.RFC3339), lastFailedWAL.String, failed, archived),
			"see the PostgreSQL server log; the postgres OS user must be able to run the Acrynite executable, read its storage config and write to the WAL location")
		return
	}
	r.add(Pass, "WAL archiver", fmt.Sprintf("%d segment(s) archived, %d failure(s)", archived, failed), "")
}

func checkWALDirectoryAccess(r *Report, db *sql.DB) {
	var segments int
	err := db.QueryRow("SELECT count(*) FROM pg_ls_waldir()").Scan(&segments)
	if err == nil {
		r.add(Pass, "WAL directory access", fmt.Sprintf("role can list the WAL directory (%d file(s))", segments), "")
		return
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "42501" {
		r.add(Warn, "WAL directory access", "permission denied: role cannot list the WAL directory (pg_ls_waldir needs pg_monitor); archiving still works, but WAL backlog cannot be monitored",
			"GRANT pg_monitor TO <user>;")
		return
	}
	r.add(Warn, "WAL directory access", "could not list the WAL directory: "+err.Error(), "")
}
