# Acrynite CLI

Acrynite is a PostgreSQL backup and recovery CLI. It registers PostgreSQL
connection profiles and S3 destinations once, creates logical or physical
backups, records a manifest for every run, and supports WAL-backed recovery in
an isolated Docker container.

This version supports PostgreSQL and S3/S3-compatible storage only. The older
`db`, `provider`, and `backup schedule` command flows are not public commands.

## Install (no Go required)

Linux / macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/bendev-w/acrynite-cli/main/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/bendev-w/acrynite-cli/main/install.ps1 | iex
```

Verify with `acrynite version`.

## Requirements

- Go 1.26+ only if building from source.
- PostgreSQL client tools in `PATH`: `pg_dump` for logical backups and
  `pg_basebackup` for physical backups.
- AWS credentials from the normal AWS chain (environment, shared profile, or
  IAM role), or supplied during storage registration.
- Docker running for physical restores to `--target docker`.

Physical backup and WAL setup require a replication-capable PostgreSQL role and
server-level access. An application role normally does not have these rights.

## Other install options

Download an archive for your platform from the
[Releases](https://github.com/bendev-w/acrynite-cli/releases) page. Set
`ACRYNITE_VERSION` or `ACRYNITE_INSTALL_DIR` to pin a version or change the
install location.

Releases are published by pushing a tag: `git tag v0.1.0 && git push origin v0.1.0`.

## Build

```powershell
go build -o acrynite.exe .
.\acrynite.exe --help
```

## Public commands

```text
acrynite database register|list|show|test-connection
acrynite storage register|list
acrynite backup run|list|show
acrynite schedule create|list
acrynite wal enable|status
acrynite recovery-points list
acrynite restore plan|run
```

Local configuration and the backup catalog live under `storage/configs/` in the
working directory. A manifest is also stored beside each backup artifact, so
completed runs remain discoverable after their schedule changes.

## Quick start

```sh
# Register a source PostgreSQL profile.
acrynite database register production --host db.example.com --port 5432 --username acrynite_backup --database app_db --ssl-mode require

# Register an S3 backup destination.
acrynite storage register s3-prod-backups --provider s3 --bucket company-backups --prefix acrynite/production --region eu-west-1

acrynite database test-connection production
acrynite backup run --database production --type logical --storage s3-prod-backups
acrynite backup run --database production --type physical --storage s3-prod-backups
acrynite wal enable production --storage s3-prod-backups
```

## Database profiles

```sh
acrynite database register <name> --host <hostname> --username <username> --database <database-name>
```

Optional flags: `--port` (default `5432`), `--ssl-mode`, `--dump-path`, and
`--password`.

Avoid passing `--password` where shell history is retained. `database show`
never displays it.

```powershell
acrynite database list
acrynite database list --format json
acrynite database show production
acrynite database test-connection production
```

## S3 storage profiles

Registered storage is S3-only:

```sh
acrynite storage register <name> --provider s3 --bucket <bucket> --prefix <optional-prefix> --region <region> --endpoint <optional-s3-compatible-endpoint>
```

The preferred authentication method is the AWS credential chain. For a local
setup, `--access-key` and `--secret-key` can be supplied together during
registration; avoid those flags in shared shell history.

```powershell
acrynite storage list
acrynite storage list --format json
```

## One-time backups

```sh
acrynite backup run --database <registered-database> --type <physical|logical> (--storage <registered-s3-storage> | --path <local-directory>)
```

`--storage` and `--path` are mutually exclusive. `--path` is a one-off local
destination, not a registered storage provider. `--name` optionally labels a
run.

```powershell
# Logical PostgreSQL dump to S3.
acrynite backup run --database production --type logical --storage s3-prod-backups

# Physical PostgreSQL cluster backup to S3.
acrynite backup run --database production --type physical --storage s3-prod-backups

# Local, one-off logical dump.
acrynite backup run --database production --type logical --path .\backups\production
```

Every run gets a unique ID such as `bkp_20261023_030000_a1b2`. Its manifest
records the type, status, timestamps, artifact size, actual destination,
PostgreSQL version/cluster identifier when available, and the associated WAL
destination for physical backups. Failed runs are catalogued with their error.

```powershell
acrynite backup list --database production
acrynite backup list --database production --type physical --status completed
acrynite backup list --database production --storage s3-prod-backups --limit 10
acrynite backup list --database production --format json
acrynite backup show bkp_20261023_030000_a1b2
```

## Scheduled backups

```sh
acrynite schedule create <schedule-name> --database <registered-database> --type <physical|logical> --interval <duration> (--storage <registered-s3-storage> | --path <local-directory>)
```

Intervals use Go duration syntax: `24h`, `6h`, or `168h`.

```sh
acrynite schedule create production-daily --database production --type physical --interval 24h --storage s3-prod-backups

acrynite schedule list
acrynite schedule list --database production
acrynite schedule list --status enabled
```

Creating a schedule starts Acrynite's scheduler daemon. It executes the same
validated `backup run` flow when jobs are due. Each occurrence is an independent
backup ID and manifest; schedules never own or delete prior backup history.

## WAL archiving and recovery points

WAL archiving is configured once per PostgreSQL cluster, not per schedule:

```powershell
acrynite wal enable <registered-database-or-cluster> --storage <registered-s3-storage>
acrynite wal status <registered-database-or-cluster>
acrynite recovery-points list --database production
```

`wal enable` configures PostgreSQL's `archive_command` to call Acrynite's
internal WAL uploader. The Acrynite executable and its configuration must be
available to the PostgreSQL server process. PostgreSQL may need a restart to
activate `archive_mode`; Acrynite reports that requirement.

Ensure `pg_hba.conf`, `wal_level`, `max_wal_senders`, and
`max_replication_slots` are compatible with physical backup and replication.
Many managed PostgreSQL services do not permit `ALTER SYSTEM` or custom archive
commands.

`recovery-points list` reports known physical base backups and their configured
WAL destination. A configured destination is not proof of a gap-free WAL chain;
PostgreSQL recovery validates actual WAL continuity.

## Restore

Preview a physical restore before moving artifacts or starting Docker:

```sh
acrynite restore plan --database production --target-time "2026-10-23T14:30:00Z" --target docker

acrynite restore plan --backup-id bkp_20261022_030000_c3d4 --target docker
acrynite restore plan --database production --latest --target docker
```

`--latest` and `--target-time` are mutually exclusive. If no backup ID is
provided, Acrynite chooses a completed physical backup for `--database`.
`--backup-id` and `--latest` are mutually exclusive.

### Physical recovery to Docker

```sh
# Recover as far forward as available WAL permits.
acrynite restore run --backup-id bkp_20261022_030000_c3d4 --target docker

# Recover to a specific UTC point in time.
acrynite restore run --backup-id bkp_20261022_030000_c3d4 --target docker --target-time "2026-10-23T14:30:00Z"
```

Acrynite downloads the base backup and recorded S3 WAL archive into a new local
recovery directory, creates `recovery.signal`, and starts an isolated Docker
container. It never overwrites the source cluster and publishes no port unless
requested.

Use `--container-name` for a predictable container name. `--publish-port <n>`
binds the recovered PostgreSQL server to `127.0.0.1:<n>`; it does not expose it
publicly. PostgreSQL stops recovery safely if it finds a WAL gap or incompatible
timeline.

### Logical restore

```sh
acrynite restore run --backup-id bkp_20261020_030000_a7b8 --target-database staging
```

Logical backups are imported through `psql` into a registered target profile.
For safety, Acrynite rejects a restore directly into the backup's source
profile; register a staging or recovery target instead.

## Operational guidance

- Preserve physical backup artifacts and their matching WAL archive together.
  A base backup alone is not point-in-time recovery.
- Do not remove completed manifests when changing schedules or S3 prefixes.
- Acrynite chooses `postgres:<major>` for a physical recovery container when
  the source PostgreSQL version is known.
- Test restores regularly: take a physical backup, archive WAL while changing
  data, recover to a chosen timestamp in Docker, and verify the data.

## Development checks

```powershell
go test ./...
go build -o acrynite.exe .
.\acrynite.exe --help
```
