#!/bin/sh
# Simulates data loss, then forces a WAL segment switch and waits for it to reach MinIO,
# so the WAL covering the chosen target time T is archived before any restore.
# (restore plan cannot yet check WAL coverage itself, so this guarantees it.)
#
#   ./scripts/break-things.sh drop     DROP TABLE customers   (default)
#   ./scripts/break-things.sh delete   DELETE even-seq rows from events
set -eu

cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

mode="${1:-drop}"
case "$mode" in
  drop)   sql="DROP TABLE customers;"                  ; desc="DROP TABLE customers" ;;
  delete) sql="DELETE FROM events WHERE seq % 2 = 0;"  ; desc="DELETE even-seq events rows" ;;
  *) echo "usage: break-things.sh drop|delete" >&2; exit 2 ;;
esac

psql_su() { docker compose exec -T postgres psql -U postgres -d lab_app -Atq "$@"; }

echo "Breaking things: $desc"
psql_su -v ON_ERROR_STOP=1 -c "$sql" >/dev/null
echo "Done: $desc"

before="$(psql_su -c "SELECT COALESCE(last_archived_wal, '') FROM pg_stat_archiver")"
psql_su -c "SELECT pg_switch_wal()" >/dev/null

printf 'Forcing WAL switch and waiting for archive'
i=0
while [ "$i" -lt 60 ]; do
  after="$(psql_su -c "SELECT COALESCE(last_archived_wal, '') FROM pg_stat_archiver")"
  if [ -n "$after" ] && [ "$after" != "$before" ]; then
    echo " done."
    echo "Last archived WAL: $after"
    echo
    echo "Next (in the toolbox): acrynite restore plan / restore run  (see README step 9)."
    exit 0
  fi
  printf '.'; sleep 1; i=$((i + 1))
done

echo
echo "WARNING: last_archived_wal did not advance. Is WAL archiving enabled and MinIO up?"
echo "Check: ./lab.sh status"
exit 1
