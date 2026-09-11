# Acrynite CLI

[![Go Version](https://img.shields.io/badge/go-1.24.0-blue)](https://golang.org/dl/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

A powerful command-line tool for managing database backups with ease. Acrynite CLI provides automated backup scheduling, database connection management, and comprehensive backup utilities for PostgreSQL and MySQL databases.

## Features

✨ **Core Capabilities:**
- 📦 **One-Command Backups** - Run full database backups with a single command
- ⏰ **Automated Scheduling** - Schedule backups at regular intervals (hourly, daily, weekly, or custom)
- 🗄️ **Multi-Database Support** - Support for PostgreSQL and MySQL
- 🔧 **Easy Database Management** - Register, list, and manage database connections
- 📍 **Durable Backup Locations** - Every schedule/manual backup writes to a named location that survives the schedule being deleted or recreated
- ☁️ **Cloud Storage** - Upload, discover, and restore backups from Google Drive, Amazon S3, and S3-compatible services
- ♻️ **Smart Restore** - Restore by database name; the CLI figures out which location/file to use (or asks when it's ambiguous), never guesses
- 💾 **Local Configuration** - Store database configurations securely on your system
- 📊 **Version Tracking** - Version management for your CLI

## Installation

### Prerequisites
- Go 1.24.0 or higher
- PostgreSQL or MySQL installed (for database operations)
- pg_dump (included with PostgreSQL) for taking backups

### From Source

```bash
git clone https://github.com/yourusername/acrynite-cli.git
cd acrynite-cli
go build -o acrynite
```

Then, optionally add the binary to your PATH:

```bash
# On Linux/macOS
sudo mv acrynite /usr/local/bin/

# On Windows
# Move acrynite.exe to a directory in your PATH
```

## Quick Start

### 1. Register a Database

Register a PostgreSQL database with Acrynite:

```bash
acrynite db register \
  --name production-db \
  --type postgres \
  --host localhost \
  --port 5432 \
  --user postgres \
  --password your_password \
  --database app_db
```

Or use interactive mode for a guided setup:

```bash
acrynite db register
```

### 2. List Registered Databases

View all your registered databases:

```bash
acrynite db list
```

### 3. Run a Backup

Create an immediate backup of your database:

```bash
acrynite backup run production-db --path ./backups
```

### 4. Schedule Automated Backups

Set up recurring backups. This also registers a named **location** for the database, which is what `backup restore` uses later:

```bash
acrynite backup schedule production-db --interval daily --path ./backups --name nightly
```

### 5. Restore a Backup

Restore the newest backup without having to remember a file path:

```bash
acrynite backup restore production-db
```

If the database has more than one location, you'll be prompted to pick one - the CLI never guesses which backup to restore.

## Usage

### Global Flags

```bash
acrynite [command] [flags]

Flags:
  -v, --verbose          Enable verbose output for detailed information
  --config string        Path to config file (default: $HOME/.acrynite-cli.yaml)
  -h, --help             Show help information
```

### Commands

#### Database Management (`db`)

**Register a Database:**
```bash
acrynite db register [flags]

Flags:
  --name string         Database name (required)
  --type string         Database type: postgres or mysql (required)
  --host string         Database host (default: localhost)
  --port int            Database port
  --user string         Database username (required)
  --password string     Database password (required)
  --database string     Database name to backup
  --ssl-mode string     SSL mode for connections (default: disable)
  -f, --force          Force registration, overwriting existing
```

**List Databases:**
```bash
acrynite db list
```

**Test Database Connection:**
```bash
acrynite db test [database-name]
```

**Export Configuration:**
```bash
acrynite db export --file ./databases.json
```

**Import Configuration:**
```bash
acrynite db import --file ./databases.json
```

#### Backup Operations (`backup`)

**Run an Immediate Backup:**
```bash
acrynite backup run [database-name] [flags]

Flags:
  --path string        Path to store backup files (required)
  --location string    Upload to this registered location instead of keeping the backup locally

Example:
  acrynite backup run production-db --path ./backups
  acrynite backup run dev-db --path /var/backups/databases
```

**Schedule Recurring Backups:**
```bash
acrynite backup schedule [database-name] [flags]

Flags:
  --interval string    Backup frequency: hourly, daily, weekly, or custom duration (e.g., 30m, 2h, 24h) (default: daily)
  --path string        Path to store backup files (local locations only; default: ./backups)
  --name string        Label for the location this schedule writes to (default: auto-generated)
  --type string        Location type: local, s3, or gdrive (default: local)
  --provider string    Registered provider name (required for s3/gdrive)
  --prefix string      S3 key prefix (optional)

Example:
  acrynite backup schedule production-db --interval daily --path ./backups --name nightly
  acrynite backup schedule dev-db --interval 6h --type=s3 --provider=work-bucket --name dev-6h
  acrynite backup schedule test-db --interval weekly --type=gdrive --provider=personal-drive --name weekly
```

✅ **Note:** When you schedule a backup, the scheduler daemon automatically starts in the background and executes backups according to the interval. It also creates (or updates) a **location** entry for the database/name pair, which `backup restore` uses to find backups later.

**Stop a Schedule, Keep the Location Restorable:**
```bash
acrynite backup unschedule [database-name] --name [location-name]

Example:
  acrynite backup unschedule production-db --name nightly
```
This cancels the recurring job but keeps the location (and its existing backups) around so you can still restore from it later.

**Manually Register a Location (no schedule):**
```bash
acrynite backup register [database-name] --name [location-name] --path [dir] [flags]

Flags:
  --name string        Label for this location (required)
  --type string        Location type: local, s3, or gdrive (default: local)
  --path string        Path where backups are stored (local locations only; default: ./backups)
  --provider string    Registered provider name (required for s3/gdrive)
  --prefix string      S3 key prefix (optional)

Example:
  acrynite backup register production-db --name imported --path ./old-backups
```
Useful for inherited backups or one-off imports where you don't want a recurring job.

**List Scheduled Backups, or Locations for a Database:**
```bash
acrynite backup list               # all active schedules
acrynite backup list [database-name]  # locations + latest backup for that database
```

**Cancel a Scheduled Backup (by schedule ID):**
```bash
acrynite backup cancel [schedule-id]

Example:
  acrynite backup cancel backup-production-db-20260614_094714
```

**Restore a Database:**
```bash
acrynite backup restore [database-name] [flags]

Flags:
  -f, --file string       Restore this exact backup file, bypassing location discovery entirely
      --schedule string   Restore from this named location only, or 'any' to combine all locations (alias: --location)
      --location string   Alias for --schedule
      --source string     Filter candidate locations by origin: auto|manual|all (default: all)
      --dry-run           Test restore pre-conditions without applying changes
      --force              Skip the confirmation prompt
      --custom-path string Custom path to the psql or mysql client executable

Example:
  acrynite backup restore production-db                       # newest backup; asks if ambiguous
  acrynite backup restore production-db --schedule nightly    # newest backup from that location only
  acrynite backup restore production-db --schedule any        # newest backup across all locations
  acrynite backup restore production-db --file ./backups/production-db_20260907-235743.sql
```
Every restore first tests the target connection and validates the backup file before touching the database. When no `--file`/`--schedule` is given and more than one location exists for the database, you're prompted to choose - restore never silently guesses.

**Verify a Backup File:**
```bash
acrynite backup verify [file-path] [flags]

Flags:
  -f, --file string    Path to SQL backup file
  --type string        Expected database type (postgres or mysql)

Example:
  acrynite backup verify ./backups/production-db_20260907.sql
  acrynite backup validate ./backups/test.sql --type mysql
```

#### Cloud Providers (`provider`)

Register cloud storage accounts once, then reference them by name from any S3 or Google Drive location.

**Add a Provider:**
```bash
acrynite provider add gdrive --name personal-drive
acrynite provider add s3 --name work-bucket
```
- `gdrive` opens the system browser, requests the narrow `drive.file` scope, receives the callback on a temporary local port, and saves the refresh token. Before running it, create a Google Cloud "Desktop app" OAuth client with the Drive API enabled and set `ACRYNITE_GDRIVE_CLIENT_ID` and `ACRYNITE_GDRIVE_CLIENT_SECRET`.
- `s3` prompts interactively for access key, secret key, region, bucket, and an optional S3-compatible endpoint. It runs `HeadBucket` before saving anything. Command-line flags are accepted but warned about because secrets can land in shell history.

**List / Check / Remove:**
```bash
acrynite provider list
acrynite provider status              # actively checks Drive authorization / S3 bucket access
acrynite provider reauth [name]       # re-runs the Google OAuth flow
acrynite provider remove [name] [--force]
```
`provider remove` is blocked if an active location still references the provider, unless `--force` is passed.

#### Version

Display version information:

```bash
acrynite version
```

## Configuration & Storage

### Database Configuration
Database configurations are stored locally in your system's configuration directory:

- **Linux/macOS:** `~/.acrynite-cli/databases.json`
- **Windows:** `%APPDATA%\acrynite-cli\databases.json`

You can also export and import configurations:

```bash
# Export current configurations
acrynite db export --file backup-configs.json

# Import configurations on another machine
acrynite db import --file backup-configs.json
```

### Schedule Management
Scheduled backups are stored in:
- **Linux/macOS:** `~/.acrynite-cli/storage/schedules/schedules.json`
- **Windows:** `%APPDATA%\acrynite-cli\storage\schedules\schedules.json`

### Locations & Providers
Backup locations (the durable per-database handle used by `backup restore`) and registered cloud providers are stored in:
- `./storage/configs/locations.json`
- `./storage/configs/providers.json`

Provider secrets (S3 access/secret keys) are encrypted at rest and kept separately under `~/.acrynite-cli/secrets.enc.json`, with the encryption key in `~/.acrynite-cli/.secret.key`. This is a local fallback until OS keychain integration is added - it is not a substitute for a real keychain.

### Backup Files
Backup files are stored in the path you specify during scheduling:
```bash
# Default location
./backups/

# Files are named: [database-name]_[timestamp].sql
# Example: production-db_20260614-093729.sql
```

## Architecture

```
acrynite-cli/
├── cmd/                    # CLI command definitions
│   ├── backup.go          # Backup, schedule, location & restore command logic
│   ├── provider.go        # Cloud provider registration commands
│   ├── command.go         # Root command setup
│   ├── db.go               # Database management commands
│   └── version.go         # Version command
├── internal/              # Internal packages
│   ├── backup/             # Backup implementation
│   │   └── full.go        # Full backup operations
│   ├── restore/            # Restore & backup file validation
│   │   ├── restore.go      # Restore execution (psql/mysql)
│   │   └── validate.go    # Backup file inspection & validation
│   ├── location/           # Durable per-database backup locations
│   │   ├── types.go        # Location config & storage types
│   │   ├── manager.go     # Location CRUD (locations.json)
│   │   └── discover.go    # Finding the latest backup file in a location
│   ├── provider/            # Cloud storage provider credentials
│   │   ├── types.go        # Provider config & storage types
│   │   ├── manager.go     # Provider CRUD (providers.json)
│   │   └── secrets.go     # Local encrypted secret store (AES-GCM)
│   ├── schedule/           # Schedule management
│   │   ├── persistence.go # Schedule persistence layer
│   │   └── manager.go     # Schedule manager & daemon
│   ├── config/              # Generic configuration manager
│   └── database/            # Database connectivity
│       ├── connect.go     # Connection handling
│       ├── register.go     # Registration logic
│       └── types.go       # Data types
├── storage/               # Local storage
│   ├── backups/           # Backup files
│   ├── configs/           # Configuration files (databases.json, locations.json, providers.json)
│   ├── logs/              # Application logs
│   └── schedules/         # Schedule metadata & PID tracking
├── utils/                 # Utility functions
│   ├── filesystem.go      # File operations
│   ├── logger.go          # Logging utilities
│   ├── scheduler.go       # In-memory task scheduling
│   └── timestamp.go       # Time utilities
└── main.go                # Entry point
```

## Development

### Prerequisites
- Go 1.24.0+
- PostgreSQL or MySQL for testing

### Building

```bash
go build -o acrynite
```

### Running

```bash
./acrynite --help
```

### Project Dependencies

- `github.com/spf13/cobra` - CLI framework
- `github.com/AlecAivazis/survey` - Interactive prompts
- `github.com/lib/pq` - PostgreSQL driver
- `github.com/go-sql-driver/mysql` - MySQL driver
- `github.com/aws/aws-sdk-go-v2` - Amazon S3 and S3-compatible storage
- `golang.org/x/oauth2` / `google.golang.org/api` - Google OAuth and Drive API

## Scheduler Daemon

When you schedule a backup using `acrynite backup schedule`, a background scheduler daemon automatically starts. The daemon:

- ✅ Runs continuously in the background
- ✅ Checks for overdue schedules every 30 seconds
- ✅ Automatically executes backups when they're due
- ✅ Updates backup history and error tracking
- ✅ Persists all schedule data to disk

The daemon runs independently of the CLI process, so scheduled backups will execute even if you close the terminal.

## Common Use Cases

### Schedule Daily Backups
```bash
# Register a production database
acrynite db register --name production-db --type postgres \
  --host db.example.com --port 5432 \
  --user postgres --password secret --database myapp

# Schedule daily backups (daemon starts automatically)
acrynite backup schedule production-db --interval daily --path /backups/production
```

### Schedule Backups at Custom Intervals
```bash
# Every 6 hours
acrynite backup schedule production-db --interval 6h --path /backups/production

# Every 30 minutes
acrynite backup schedule dev-db --interval 30m --path /backups/dev

# Every week
acrynite backup schedule backup-db --interval weekly --path /backups/weekly
```

### Multiple Database Backups
```bash
# Register multiple databases
acrynite db register --name prod-main --type postgres ...
acrynite db register --name prod-cache --type mysql ...

# Schedule backups for each (daemons start automatically)
acrynite backup schedule prod-main --interval daily --path /backups
acrynite backup schedule prod-cache --interval daily --path /backups

# List all scheduled backups
acrynite backup list
```

### Backup Configuration Migration
```bash
# Export configs from one machine
acrynite db export --file configs.json

# Import on another machine
acrynite db import --file configs.json

# Re-schedule if needed on the new machine
acrynite backup schedule prod-db --interval daily --path /backups
```

### Restore From the Right Location
```bash
# One schedule ("nightly") plus one manual import ("imported") for the same db
acrynite backup schedule production-db --interval daily --path /backups/nightly --name nightly
acrynite backup register production-db --name imported --path /backups/legacy

# See what's available
acrynite backup list production-db

# Ambiguous - two locations exist, so you'll be prompted to pick one
acrynite backup restore production-db

# Be explicit instead
acrynite backup restore production-db --schedule nightly
acrynite backup restore production-db --schedule any --source auto

# Stop the recurring job but keep the location (and its backups) restorable
acrynite backup unschedule production-db --name nightly
```

### Back Up to S3 or Google Drive
```bash
# Authenticate/register providers once
acrynite provider add s3 --name work-bucket
acrynite provider add gdrive --name personal-drive

# Schedule cloud backups
acrynite backup schedule production-db --name nightly-s3 --interval daily \
  --type s3 --provider work-bucket --prefix databases/production
acrynite backup schedule production-db --name weekly-drive --interval weekly \
  --type gdrive --provider personal-drive

# Upload one backup immediately to an existing cloud location
acrynite backup run production-db --location nightly-s3

# Restore by location; cloud files are downloaded to a temporary SQL file
acrynite backup restore production-db --location nightly-s3
```

## Troubleshooting

### Connection Failed
- Verify database credentials with `acrynite db test [database-name]`
- Check if the database host and port are correct
- Ensure network connectivity to the database server
- Confirm username and password are correct

### Backup Failed
- Verify the backup path exists and has write permissions: `mkdir -p /backups`
- Check available disk space: `df -h`
- Verify database credentials with `acrynite db test`
- Review error messages with `--verbose` flag for details

### Schedule Not Executing
- The scheduler daemon runs automatically, no manual action needed
- Verify the schedule was created: `acrynite backup list`
- Check the PID file exists: `~/.acrynite-cli/storage/schedules/.daemon.pid`
- View recent backups to confirm execution occurred
- On Windows, ensure the process has necessary permissions

### Missing Backup Files
- Verify the correct path was specified: `acrynite backup list`
- Check if the directory has write permissions
- Ensure `pg_dump` (PostgreSQL) or `mysqldump` (MySQL) is installed and in PATH

### Schedule Takes Too Long
- The scheduler checks every 30 seconds, so backups start within 30 seconds of their scheduled time
- Large databases may take several minutes to backup
- Use `acrynite backup list` to check `Last run` time and status

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For issues, questions, or suggestions, please open an issue on GitHub.

---

Made with ❤️ by the Acrynite team
