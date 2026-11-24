# Acrynite Test Lab — testing reference

This is the detailed companion to [README.md](README.md): architecture, acceptance checks,
edge cases to try on purpose, and the tester report form. It targets **physical PITR only**.

## Architecture

Four services on one bridge network (`lab`):

| Service | Image | Purpose |
| --- | --- | --- |
| `localstack` | `localstack/localstack:4` | S3 stand-in; API at `http://localstack:4566`. An init hook creates the `acrynite-lab` bucket when it is ready |
| `postgres` | built `FROM postgres:17` | The "production" database |
| `writer` | `postgres:17` | Inserts one `events` row every `WRITE_INTERVAL` seconds |
| `toolbox` | built (Debian + `postgresql-client-17` + Docker CLI) | Where the tester runs `acrynite` |

**Why LocalStack and not MinIO.** The spec was written for MinIO, but MinIO's Docker Hub
and quay images now require authentication (`unauthorized: authentication required` on an
anonymous pull), which breaks the "only Docker installed, no account" promise. LocalStack
is anonymously pullable and emulates S3 faithfully (including the multipart uploads that
`pg_basebackup` tars and WAL segments use). Override `S3_IMAGE` in `.env` to swap it.

**The shared-path trick.** `LAB_WORK` is an absolute host path (`$(pwd)/work`, written into
`.env` by `lab.sh`) bind-mounted at that same absolute path in `postgres` and `toolbox`.
`acrynite wal enable` writes an `archive_command` containing the binary path
(`os.Executable()`) and working directory (`os.Getwd()`); because the path is identical in
both containers, PostgreSQL (uid 999) can execute the exact binary the tester ran. The same
mount lets the recovery container's bind mounts (created by `restore run` under `TMPDIR`)
resolve on the host.

**Why these environment choices:**

- `HOME=${LAB_WORK}/home` on **both** `postgres` and `toolbox`. Acrynite reads its catalog
  and storage profile from `$HOME/.acrynite-cli`. `gosu` preserves the environment, so the
  `archive_command` that PostgreSQL runs sees the same `HOME` and finds the config the
  toolbox wrote. (This resolves the spec's open question about credentials for
  `archive_command`.)
- `TMPDIR=${LAB_WORK}/tmp` on `toolbox`. `restore run` uses `os.MkdirTemp("", …)`; the
  recovery folder must exist on the host for the bind mount, so `TMPDIR` points inside the
  shared folder.
- AWS variables on `postgres` and `toolbox`. Storage is registered **without** keys, so the
  AWS SDK default chain uses `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`/`AWS_REGION`
  (LocalStack ignores the values but the SDK requires them set). If you instead register
  with `--access-key`/`--secret-key`, the keys land only in the toolbox's secret store and
  WAL pushes from PostgreSQL fail auth — don't do that in this lab.

**Compose rules honoured:** `archive_mode` / `archive_command` / `archive_timeout` are never
passed with `-c` on the postgres command line (that would override `ALTER SYSTEM` and make
`wal enable` silently ineffective). All published ports bind to `127.0.0.1`.

## PostgreSQL roles

| Role | Attributes | Used by |
| --- | --- | --- |
| `acrynite_backup` | `LOGIN REPLICATION`; `ALTER SYSTEM` on `archive_mode`/`archive_command`/`archive_timeout`; `EXECUTE` on `pg_reload_conf()`; member of `pg_read_all_settings`, `pg_read_all_data`, `pg_monitor` | Acrynite (backups, WAL setup, status) |
| `lab_writer` | `LOGIN`, owns `lab_app` and its tables | The writer service |

`acrynite_backup` is deliberately **not** a superuser, so the lab also exercises Acrynite's
preflight checks. A permission error the preflight missed is a bug worth reporting.

## Acceptance checks

### Restore checks (`verify-restore.sh`)

| # | Check | Pass when |
| --- | --- | --- |
| 1 | Dropped table is back | `SELECT count(*) FROM customers` on the recovered container returns 100 |
| 2 | Recovery stopped at T | Recovered `max(seq)` equals the source's `max(seq) WHERE created_at <= T`. One less is a **WARN**, not a FAIL (a row created just before T can commit just after it) |
| 3 | Nothing after T | No recovered row has `created_at > T + 1 second` |
| 4 | Source untouched | Source `customers` is still missing and the writer is still adding rows (so the restore never touched the source) |

### Lab checks

- From a clean clone, `./lab.sh up` finishes in a few minutes with no manual steps.
- `up` run twice changes nothing; `reset` then `up` gives a fresh lab with no old backups in S3.
- Nothing listens on `0.0.0.0`: every published port binds to `127.0.0.1`.
- Host ports come from `.env` (`PG_HOST_PORT`, `MINIO_CONSOLE_PORT`, `RECOVERY_PORT`).

## Edge cases to try on purpose

Each makes a good tester report.

- **T earlier than the end of the base backup** — `restore plan`/`run` should refuse or warn
  clearly (the code rejects a target that predates the selected base backup).
- **T in the future, or after the last archived WAL** — `restore run` should stop at the
  latest point and say so. `--target-time` in the future is rejected outright; a target past
  the last archived WAL produces a clear "WAL archive ends before the requested target"
  message with the last committed time.
- **Archiving broken on purpose** — `docker compose stop localstack` for a minute:
  `wal status` should show a failed count, and archiving should catch up once it is back.
- **Writer running through `restart-db`** — no gaps in `seq` once it recovers.
- **A second restore while `acrynite-lab-recovery` already exists** — the error should name
  the clash and how to fix it.

## Known limitations (verified against the code)

- **`restore plan` does not yet check WAL coverage** for the requested T — it only checks
  that a WAL destination exists. `break-things.sh` therefore forces a WAL segment switch and
  waits for it to archive, guaranteeing the WAL covering T is in S3 before you restore.
- **No `--recovery-dir` flag** — `restore run` uses `os.MkdirTemp("", …)`, so the lab relies
  on `TMPDIR` pointing inside `LAB_WORK`.
- **Direct WAL mode only** on the public `wal enable`. Stream mode exists in the codebase but
  only behind the legacy schedule-based flow; the lab uses the shared-path trick to make
  direct mode work with containerised PostgreSQL. (See the build spec, item 1.)
- **Physical PITR only.** Logical backup/restore (`--type logical`, `--target-database`) is
  out of scope for this lab.

## Tester report

Copy this into a lab issue after a run.

```
### Environment
- OS / arch:                 (e.g. macOS 15 arm64 / Ubuntu 24.04 amd64 / Windows 11 WSL2)
- Docker:                    (docker version)
- Acrynite:                  (released <version>, or local build <commit>)

### Result
- ./lab.sh up clean:         PASS / FAIL
- wal enable + restart-db:   PASS / FAIL
- backup run (physical):     PASS / FAIL   backup id: __________
- restore run --target-time: PASS / FAIL
- verify-restore.sh:         #1 ___  #2 ___  #3 ___  #4 ___   overall ___

### Timings
- ./lab.sh up:               ____  (target: a few minutes)
- full walkthrough:          ____  (target: ~20 minutes)

### Edge cases tried
-

### Notes / unexpected behaviour / suspected bugs
-
```

## CI reuse

Once stable, a headless CI restore test can reuse this lab: `./lab.sh up`, then the flow and
`verify-restore.sh` non-interactively, across PostgreSQL 15–18 (bump the `postgres` image tag
and `postgresql-client-NN` to match).
