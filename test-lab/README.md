# Acrynite Test Lab

Take a physical backup, archive WAL to S3, lose data, and restore to a chosen minute in
a throwaway container — in about 20 minutes, with **only Docker installed**. No Go, no
PostgreSQL client, no AWS account, no real database touched.

The lab proves four things:

1. **Backup** — `acrynite backup run --type physical` uploads a base backup to S3 (LocalStack).
2. **Archiving** — `acrynite wal enable` makes PostgreSQL push WAL segments to S3 continuously.
3. **Recovery** — `acrynite restore run --target-time` recovers into a Docker container whose newest row is at or just before the chosen time.
4. **Safety** — the source database is untouched throughout.

> **S3 backend:** the lab uses **LocalStack** (a faithful, anonymously pullable S3
> emulator). MinIO's images now require a Docker Hub / quay login, which would break the
> "no account needed" promise. To use a different backend, override `S3_IMAGE` in `.env`.

## Prerequisites

- Docker Engine 24+ or Docker Desktop with Compose v2
- ~3 GB free disk (the LocalStack image is ~1 GB)
- Ports **55432**, **4566**, and **55433** free (all bound to `127.0.0.1`; change them in `.env`)
- On Windows: run everything from **WSL2** so `LAB_WORK` is a Linux path Docker Desktop can mount.

## How it works (the one trick)

`acrynite wal enable` writes an `archive_command` that points at the Acrynite binary's
absolute path and working directory. For PostgreSQL to run that same binary, the lab
mounts one host folder, `work/`, at an **identical absolute path** (`LAB_WORK`) inside
both the `postgres` and `toolbox` containers. The same mount lets the recovery container's
bind mounts resolve on the host. See [TESTING.md](TESTING.md) for the full architecture.

## Walkthrough

Use **two terminals**, both in `test-lab/`:

- **Terminal A** — inside the toolbox (`./lab.sh shell`), for every `acrynite` command.
- **Terminal B** — on the host, for `./lab.sh` and `./scripts/…`.

### 1. Start the lab — Terminal B

```sh
./lab.sh up        # builds images, starts 4 services, generates .env with random passwords
./lab.sh shell     # opens Terminal A in the toolbox
```

### 2. Register and enable WAL — Terminal A

```sh
acrynite database register lab --host postgres --port 5432 \
  --username acrynite_backup --database lab_app \
  --password "$ACRYNITE_BACKUP_PASSWORD" --ssl-mode disable
acrynite database test-connection lab

acrynite storage register s3 --provider s3 --bucket acrynite-lab \
  --prefix lab --region us-east-1 --endpoint http://localstack:4566

acrynite wal enable lab --storage s3
exit               # leave the toolbox shell
```

> Storage is registered **without** `--access-key`/`--secret-key` on purpose: Acrynite
> then uses the AWS environment variables, which are present in both the toolbox and the
> `postgres` container, so WAL pushes from PostgreSQL authenticate correctly. (LocalStack
> ignores the credential values, but they must be set for the AWS SDK.)

### 3. Activate archiving — Terminal B

```sh
./lab.sh restart-db     # archive_mode=on needs a restart
./lab.sh shell          # back into Terminal A
```

### 4. Confirm WAL and take a backup — Terminal A

```sh
acrynite wal status lab          # repeat until a successful WAL segment appears
acrynite backup run --database lab --type physical --storage s3
# note the backup ID it prints
```

### 5. Choose a target time and lose data — Terminal B

```sh
# wait ~2 minutes so WAL accumulates past the backup
./scripts/checkpoint.sh          # prints T=... and saves it to work/checkpoint
# wait ~1 more minute
./scripts/break-things.sh drop   # DROP TABLE customers, then forces WAL to archive
```

### 6. Restore to the chosen minute — Terminal A

```sh
acrynite restore plan --backup-id <id> --target-time <T> --target docker
acrynite restore run  --backup-id <id> --target-time <T> --target docker \
  --container-name acrynite-lab-recovery --publish-port 55433
```

(`<T>` is the time `checkpoint.sh` printed, e.g. `2026-11-17T14:05:00Z`.)

### 7. Verify — Terminal B

```sh
./scripts/verify-restore.sh      # PASS/WARN/FAIL for each acceptance check
```

Then fill in the **Tester report** in [TESTING.md](TESTING.md#tester-report).

## lab.sh reference

| Command | Does |
| --- | --- |
| `up` | Build + start; generate `.env` on first run; wait until healthy |
| `shell` | Open a toolbox shell (prints the registration commands) |
| `status` | Containers, last 5 writer rows, `acrynite wal status lab` |
| `restart-db` | Restart PostgreSQL (needed after `wal enable`); shows `archive_mode` |
| `down` | Stop containers, keep data volumes |
| `reset` | Remove containers + data (pgdata, S3 state, work/{storage,tmp,home}); keep the binary |
| `destroy` | `reset` + remove the installed binary and built images |

## Testing an unreleased build

By default the toolbox downloads the latest released Linux binary. To test local code,
build a Linux binary into the shared folder and point `ACRYNITE_BINARY` at it (in `.env`):

```sh
# from the repo root
GOOS=linux GOARCH=amd64 go build -o test-lab/work/bin/acrynite .
# in test-lab/.env
ACRYNITE_BINARY=${LAB_WORK}/bin/acrynite
./lab.sh up
```

## Troubleshooting

- **`wal status` shows failures** — make sure you ran `./lab.sh restart-db` after `wal enable`, and that LocalStack is up (`./lab.sh status`).
- **Nothing archives on an idle DB** — the lab sets `archive_timeout=30s` (via `ARCHIVE_TIMEOUT_LAB`); the writer also keeps WAL flowing.
- **`restore run` says the container already exists** — remove it (`docker rm -f acrynite-lab-recovery`) or pass a different `--container-name`.
- **Port clash** — change `PG_HOST_PORT`, `MINIO_CONSOLE_PORT`, or `RECOVERY_PORT` in `.env`, then `./lab.sh up`.
