#!/bin/sh
# Runs once, on first start. Appends host-based auth rules for the lab bridge network.
# `samenet` allows the toolbox and writer (same subnet as the server's interfaces)
# without hard-coding a Docker subnet that could change.
set -e

cat >> "$PGDATA/pg_hba.conf" <<'EOF'

# --- Acrynite lab: access from the lab bridge network ---
host replication acrynite_backup samenet scram-sha-256
host all         all             samenet scram-sha-256
EOF
