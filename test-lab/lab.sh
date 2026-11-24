#!/bin/sh
# Acrynite Test Lab control script. POSIX sh. Run from test-lab/.
#
#   ./lab.sh up          Build + start the lab (generates .env on first run)
#   ./lab.sh shell       Open a shell in the toolbox (run acrynite here)
#   ./lab.sh status      Containers, recent writer rows, WAL status
#   ./lab.sh restart-db  Restart PostgreSQL (needed after `wal enable`)
#   ./lab.sh down        Stop containers, keep data volumes
#   ./lab.sh reset       Remove containers + data (pgdata, S3 state, work/{storage,tmp,home})
#   ./lab.sh destroy     reset + remove the installed binary and built images
set -eu

cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

COMPOSE="docker compose"
ENV_FILE=".env"
EXAMPLE=".env.example"

# ---------- helpers ----------

die() { echo "error: $*" >&2; exit 1; }

need_docker() {
  command -v docker >/dev/null 2>&1 || die "Docker is not installed or not on PATH."
  docker info >/dev/null 2>&1 || die "Docker is installed but the daemon is not reachable. Start Docker and retry."
  $COMPOSE version >/dev/null 2>&1 || die "Docker Compose v2 is required (the 'docker compose' subcommand)."
}

gen() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 16
  else
    # 16 random bytes as hex, POSIX fallback.
    od -An -N16 -tx1 /dev/urandom | tr -d ' \n'
  fi
}

# set_env KEY VALUE — replace KEY's line in .env, or append it.
set_env() {
  _k="$1"; _v="$2"; _tmp="$ENV_FILE.tmp"
  if [ -f "$ENV_FILE" ] && grep -q "^${_k}=" "$ENV_FILE"; then
    grep -v "^${_k}=" "$ENV_FILE" > "$_tmp"
    printf '%s=%s\n' "$_k" "$_v" >> "$_tmp"
    mv "$_tmp" "$ENV_FILE"
  else
    printf '%s=%s\n' "$_k" "$_v" >> "$ENV_FILE"
  fi
}

get_env() { grep "^$1=" "$ENV_FILE" 2>/dev/null | head -n1 | cut -d= -f2-; }

ensure_env() {
  if [ ! -f "$ENV_FILE" ]; then
    [ -f "$EXAMPLE" ] || die "$EXAMPLE is missing."
    cp "$EXAMPLE" "$ENV_FILE"
    echo "Created $ENV_FILE from $EXAMPLE; generating random passwords..."
    set_env POSTGRES_PASSWORD "$(gen)"
    set_env ACRYNITE_BACKUP_PASSWORD "$(gen)"
    set_env LAB_WRITER_PASSWORD "$(gen)"
  fi
  # LAB_WORK must be an absolute host path, refreshed every up (the repo may have moved).
  set_env LAB_WORK "$(pwd)/work"
}

wait_pg_healthy() {
  printf 'Waiting for PostgreSQL'
  i=0
  while [ "$i" -lt 60 ]; do
    if $COMPOSE exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
      echo ' ready.'; return 0
    fi
    printf '.'; sleep 2; i=$((i + 1))
  done
  echo; die "PostgreSQL did not become healthy in time. See: ./lab.sh status"
}

wait_bucket() {
  bucket="$(get_env S3_BUCKET)"; [ -n "$bucket" ] && bucket="$bucket" || bucket="acrynite-lab"
  printf 'Waiting for the S3 bucket'
  i=0
  while [ "$i" -lt 60 ]; do
    if $COMPOSE exec -T localstack awslocal s3 ls "s3://$bucket" >/dev/null 2>&1; then
      echo ' ready.'; return 0
    fi
    printf '.'; sleep 2; i=$((i + 1))
  done
  echo; echo "warning: S3 bucket '$bucket' not confirmed yet; it is created when LocalStack finishes starting."
}

# ---------- commands ----------

cmd_up() {
  need_docker
  ensure_env
  mkdir -p work/bin work/storage work/tmp work/home
  echo "Building and starting the lab..."
  $COMPOSE up -d --build
  wait_pg_healthy
  wait_bucket
  echo
  echo "Lab is up."
  echo "  S3 (LocalStack): http://localstack:4566  (in-network)  ·  127.0.0.1:$(get_env S3_HOST_PORT) (host)"
  echo "  PostgreSQL:      127.0.0.1:$(get_env PG_HOST_PORT)"
  echo "  Inspect S3:      docker compose exec localstack awslocal s3 ls s3://$(get_env S3_BUCKET) --recursive"
  echo
  echo "Next: ./lab.sh shell"
}

cmd_shell() {
  need_docker
  cat <<'BANNER'
--------------------------------------------------------------------
 Acrynite toolbox. Register everything, then enable WAL and back up:

   acrynite database register lab --host postgres --port 5432 \
     --username acrynite_backup --database lab_app \
     --password "$ACRYNITE_BACKUP_PASSWORD" --ssl-mode disable
   acrynite database test-connection lab

   acrynite storage register s3 --provider s3 --bucket acrynite-lab \
     --prefix lab --region us-east-1 --endpoint http://localstack:4566

   acrynite wal enable lab --storage s3
   exit            # then on the host: ./lab.sh restart-db

 Full walkthrough: test-lab/README.md
--------------------------------------------------------------------
BANNER
  $COMPOSE exec toolbox bash
}

cmd_status() {
  need_docker
  echo "=== containers ==="
  $COMPOSE ps
  echo
  echo "=== last 5 writer rows ==="
  $COMPOSE logs --tail 5 writer 2>/dev/null || true
  echo
  echo "=== acrynite wal status lab ==="
  $COMPOSE exec -T toolbox sh -c 'acrynite wal status lab 2>&1' || echo "(WAL not configured yet, or 'lab' not registered)"
}

cmd_restart_db() {
  need_docker
  echo "Restarting PostgreSQL to activate archive_mode..."
  $COMPOSE restart postgres
  wait_pg_healthy
  tmo="$(get_env ARCHIVE_TIMEOUT_LAB || true)"
  if [ -n "${tmo:-}" ]; then
    echo "Applying lab-only archive_timeout=$tmo ..."
    $COMPOSE exec -T postgres psql -U postgres -v ON_ERROR_STOP=1 -c "ALTER SYSTEM SET archive_timeout = '$tmo'" -c "SELECT pg_reload_conf()" >/dev/null 2>&1 \
      || echo "(could not set archive_timeout; enable WAL first, then re-run ./lab.sh restart-db)"
  fi
  echo "archive_mode is now:"
  $COMPOSE exec -T postgres psql -U postgres -Atc "SHOW archive_mode"
  echo
  echo "Next: in the toolbox, run  acrynite wal status lab  until a WAL segment appears."
}

cmd_down() {
  need_docker
  $COMPOSE down
  echo "Stopped. Data volumes kept. Bring it back with ./lab.sh up"
}

remove_recovery_containers() {
  ids="$(docker ps -aq --filter 'name=acrynite-lab-recovery' 2>/dev/null || true)"
  [ -n "$ids" ] && docker rm -f $ids >/dev/null 2>&1 || true
}

cmd_reset() {
  need_docker
  remove_recovery_containers
  $COMPOSE down -v
  rm -rf work/storage work/tmp work/home
  mkdir -p work/storage work/tmp work/home
  echo "Reset done: containers and data removed (binary in work/bin kept)."
  echo "Run ./lab.sh up for a fresh lab."
}

cmd_destroy() {
  need_docker
  remove_recovery_containers
  $COMPOSE down -v --rmi local
  rm -rf work
  echo "Destroyed: data, binary, and built images removed."
}

case "${1:-}" in
  up)         cmd_up ;;
  shell)      cmd_shell ;;
  status)     cmd_status ;;
  restart-db) cmd_restart_db ;;
  down)       cmd_down ;;
  reset)      cmd_reset ;;
  destroy)    cmd_destroy ;;
  *)
    echo "usage: ./lab.sh {up|shell|status|restart-db|down|reset|destroy}" >&2
    exit 2 ;;
esac
