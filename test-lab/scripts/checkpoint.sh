#!/bin/sh
# Prints the current UTC time (to the second) and the newest seq, for choosing a restore
# target time. Saves the time to work/checkpoint so verify-restore.sh can reuse it.
# Run from the host, in test-lab/ (or anywhere — it finds its own root).
set -eu

cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

psql_src() {
  docker compose exec -T postgres psql -U postgres -d lab_app -Atq -c "$1"
}

NOW="$(psql_src "SELECT to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"')")"
SEQ="$(psql_src "SELECT COALESCE(MAX(seq), 0) FROM events")"

printf '%s\n' "$NOW" > work/checkpoint

echo "T=$NOW (seq $SEQ)"
echo
echo "Saved T to work/checkpoint."
echo "Wait ~1 more minute, then: ./scripts/break-things.sh drop"
echo "Later, restore with: --target-time $NOW"
