package wal

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/lib/pq"
)

// EnableArchive configures PostgreSQL to invoke archiveCommand for completed WAL
// segments. It returns true when PostgreSQL must be restarted for archive_mode.
func EnableArchive(db *sql.DB, archiveCommand, archiveTimeout string) (bool, error) {
	var archiveMode string
	if err := db.QueryRow("SHOW archive_mode").Scan(&archiveMode); err != nil {
		return false, fmt.Errorf("failed to read archive_mode: %w", err)
	}

	statements := archiveConfigurationStatements(archiveMode, archiveCommand)
	if archiveTimeout != "" {
		if err := ValidateArchiveTimeout(archiveTimeout); err != nil {
			return false, err
		}
		statements = append(statements, "ALTER SYSTEM SET archive_timeout = "+quoteSQLLiteral(archiveTimeout))
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return false, archiveError("failed to configure WAL archiving", err)
		}
	}

	var reloaded bool
	if err := db.QueryRow("SELECT pg_reload_conf()").Scan(&reloaded); err != nil {
		return false, archiveError("failed to reload PostgreSQL configuration", err)
	}
	if !reloaded {
		return false, fmt.Errorf("PostgreSQL declined to reload its configuration")
	}

	return archiveMode != "on" && archiveMode != "always", nil
}

// archiveError adds an actionable hint when PostgreSQL rejected the statement for lack of
// privileges (SQLSTATE 42501 insufficient_privilege).
func archiveError(action string, err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "42501" {
		return fmt.Errorf("%s: permission denied (%s); run this as a superuser, or on PostgreSQL 15+ grant ALTER SYSTEM ON PARAMETER archive_mode, archive_command and EXECUTE ON FUNCTION pg_reload_conf(): %w", action, pqErr.Message, err)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func archiveConfigurationStatements(archiveMode, archiveCommand string) []string {
	statements := []string{
		"ALTER SYSTEM SET archive_command = " + quoteSQLLiteral(archiveCommand),
	}
	if archiveMode != "on" && archiveMode != "always" {
		statements = append(statements, "ALTER SYSTEM SET archive_mode = 'on'")
	}
	return statements
}

var archiveTimeoutPattern = regexp.MustCompile(`^\d+(ms|s|min|h|d)?$`)

// ValidateArchiveTimeout accepts PostgreSQL time values such as 30s, 5min, 1h; "0" disables forced switches.
func ValidateArchiveTimeout(value string) error {
	if !archiveTimeoutPattern.MatchString(value) {
		return fmt.Errorf("invalid archive timeout '%s': use a number with an optional unit (s, min, h, d), e.g. 5min", value)
	}
	return nil
}

func quoteSQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
