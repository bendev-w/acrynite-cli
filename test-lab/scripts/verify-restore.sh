#!/bin/sh
# Compares the recovered container to the expected cut-off and prints PASS/WARN/FAIL for
# each acceptance check. T defaults to work/checkpoint.
#
#   ./scripts/verify-restore.sh [T]
#
# The recovered cluster is queried directly inside its container (docker exec), so no
# psql client is needed on the host; the source is queried via docker compose exec.
set -u

cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

REC="acrynite-lab-recovery"

T="${1:-}"
if [ -z "$T" ] && [ -f work/checkpoint ]; then T="$(cat work/checkpoint)"; fi
[ -n "$T" ] || { echo "No target time given and work/checkpoint is missing. Run ./scripts/checkpoint.sh first."; exit 2; }

echo "Verifying recovery container '$REC' against T=$T"
echo

# Recovered cluster (read-only, in recovery) and source database query helpers.
rq() { docker exec "$REC" psql -U postgres -d lab_app -Atq -c "$1" 2>/dev/null; }
sq() { docker compose exec -T postgres psql -U postgres -d lab_app -Atq -c "$1" 2>/dev/null; }

# Wait for the recovery container to accept connections.
ok=0; i=0
while [ "$i" -lt 60 ]; do
  if docker exec "$REC" pg_isready -U postgres >/dev/null 2>&1; then ok=1; break; fi
  sleep 1; i=$((i + 1))
done
if [ "$ok" != 1 ]; then
  echo "FAIL: recovery container '$REC' is not accepting connections."
  docker logs --tail 20 "$REC" 2>/dev/null || echo "(no such container — run restore run first)"
  exit 1
fi

pass=0; warn=0; fail=0

# 1 — Dropped table is back.
c="$(rq "SELECT count(*) FROM customers")"
if [ "${c:-x}" = "100" ]; then
  echo "PASS  #1 dropped table restored: customers = 100"; pass=$((pass+1))
else
  echo "FAIL  #1 customers = ${c:-<missing>} (expected 100)"; fail=$((fail+1))
fi

# 2 — Recovery stopped at T (recovered max(seq) == source max(seq) WHERE created_at <= T).
rmax="$(rq "SELECT COALESCE(MAX(seq), 0) FROM events")"
smax="$(sq "SELECT COALESCE(MAX(seq), 0) FROM events WHERE created_at <= TIMESTAMPTZ '$T'")"
if [ "${rmax:-x}" = "${smax:-y}" ]; then
  echo "PASS  #2 recovery stopped at T: recovered max(seq)=$rmax == source cut-off $smax"; pass=$((pass+1))
elif [ "${rmax:-}" = "$((smax - 1))" ] 2>/dev/null; then
  echo "WARN  #2 recovered max(seq)=$rmax is one below source cut-off $smax (a row committed just after T)"; warn=$((warn+1))
else
  echo "FAIL  #2 recovered max(seq)=$rmax, source cut-off=$smax"; fail=$((fail+1))
fi

# 3 — Nothing after T + 1 second.
after="$(rq "SELECT count(*) FROM events WHERE created_at > TIMESTAMPTZ '$T' + INTERVAL '1 second'")"
if [ "${after:-0}" = "0" ]; then
  echo "PASS  #3 no recovered rows after T+1s"; pass=$((pass+1))
else
  echo "FAIL  #3 $after recovered rows exist after T+1s"; fail=$((fail+1))
fi

# 4 — Source untouched: customers still missing, writer still advancing.
smissing="$(sq "SELECT to_regclass('public.customers') IS NULL")"
if [ "${smissing:-}" = "t" ]; then
  echo "PASS  #4a source still missing customers (restore never touched the source)"; pass=$((pass+1))
else
  echo "FAIL  #4a source customers is present — did the restore touch the source?"; fail=$((fail+1))
fi
s1="$(sq "SELECT COALESCE(MAX(seq), 0) FROM events")"; sleep 4
s2="$(sq "SELECT COALESCE(MAX(seq), 0) FROM events")"
if [ "${s2:-0}" -gt "${s1:-0}" ] 2>/dev/null; then
  echo "PASS  #4b source writer still adding rows ($s1 -> $s2)"; pass=$((pass+1))
else
  echo "FAIL  #4b source writer not advancing ($s1 -> $s2)"; fail=$((fail+1))
fi

echo
echo "-----------------------------------------"
echo "PASS=$pass  WARN=$warn  FAIL=$fail"
if [ "$fail" = 0 ]; then
  echo "RESULT: PASS${warn:+ (with $warn warning[s])}"
  exit 0
else
  echo "RESULT: FAIL"
  exit 1
fi
