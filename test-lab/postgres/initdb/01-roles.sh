#!/bin/sh
# Runs once, on first database start (docker-entrypoint-initdb.d).
# Creates the two lab roles. Written as a .sh so it can read passwords from the
# environment (plain .sql init files cannot).
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  -v backup_pw="$ACRYNITE_BACKUP_PASSWORD" \
  -v writer_pw="$LAB_WRITER_PASSWORD" <<'EOSQL'
-- acrynite_backup: the least privilege a real setup would need, NOT superuser.
-- This also exercises Acrynite's preflight checks: a permission error the preflight
-- missed is a bug worth reporting.
CREATE ROLE acrynite_backup WITH LOGIN REPLICATION PASSWORD :'backup_pw';

-- ALTER SYSTEM rights on exactly the three WAL parameters Acrynite configures.
GRANT ALTER SYSTEM ON PARAMETER archive_mode    TO acrynite_backup;
GRANT ALTER SYSTEM ON PARAMETER archive_command TO acrynite_backup;
GRANT ALTER SYSTEM ON PARAMETER archive_timeout TO acrynite_backup;

-- Reload config after ALTER SYSTEM, and read all settings / data / stats.
GRANT EXECUTE ON FUNCTION pg_reload_conf() TO acrynite_backup;
GRANT pg_read_all_settings TO acrynite_backup;
GRANT pg_read_all_data     TO acrynite_backup;
GRANT pg_monitor           TO acrynite_backup;

-- lab_writer: owns the application database and its tables.
CREATE ROLE lab_writer WITH LOGIN PASSWORD :'writer_pw';
EOSQL
