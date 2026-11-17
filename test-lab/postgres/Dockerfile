# The "production" database. Plain PostgreSQL 17 — the lab adds nothing but a sanity check.
# WAL settings are left at their PostgreSQL 17 defaults (wal_level=replica,
# max_wal_senders=10, max_replication_slots=10), which are already enough for
# pg_basebackup and `acrynite wal enable`. archive_mode/command/timeout are configured
# at runtime by `acrynite wal enable` via ALTER SYSTEM, never on the command line.
FROM postgres:17

# Sanity check only: fail the build early if the base image is not PostgreSQL 17.
RUN postgres --version | grep -q 'PostgreSQL 17' \
    && echo "postgres:17 confirmed"
