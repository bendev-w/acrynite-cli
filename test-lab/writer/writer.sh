#!/bin/sh
# Inserts one row into events every WRITE_INTERVAL seconds, so a restore to a chosen
# minute has changing data to prove. Survives the restart that `wal enable` needs, and
# takes seq from the table (not a shell counter) so the sequence never breaks.
#
# POSIX sh only; nothing beyond what the postgres:17 image ships.
set -u

: "${WRITE_INTERVAL:=3}"
export PGTZ=UTC
# psql connection comes from PGHOST/PGUSER/PGDATABASE/PGPASSWORD in the environment.

# Exit cleanly on SIGTERM so `docker compose stop writer` is instant.
trap 'exit 0' TERM INT

echo "writer: starting (interval ${WRITE_INTERVAL}s)"

while true; do
  row="$(psql -X -v ON_ERROR_STOP=1 -Atq -c \
    "INSERT INTO events (seq, note)
       SELECT COALESCE(MAX(seq), 0) + 1, 'lab write' FROM events
     RETURNING seq,
       to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"');" 2>/dev/null)"

  if [ -z "$row" ]; then
    echo "waiting for postgres..."
    sleep 2
    continue
  fi

  seq="${row%%|*}"
  ts="${row##*|}"
  echo "$ts seq=$seq"

  # Sleep in the background and wait, so a SIGTERM during the pause is handled at once.
  sleep "$WRITE_INTERVAL" &
  wait "$!"
done
