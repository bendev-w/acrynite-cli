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

Set up recurring backups:

```bash
acrynite backup schedule production-db --interval daily --path ./backups
```

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

Example:
  acrynite backup run production-db --path ./backups
  acrynite backup run dev-db --path /var/backups/databases
```

**Schedule Recurring Backups:**
```bash
acrynite backup schedule [database-name] [flags]

Flags:
  --interval string    Backup frequency: hourly, daily, weekly, or custom duration (e.g., 30m, 2h, 24h) (default: daily)
  --path string        Path to store backup files (default: ./backups)

Example:
  acrynite backup schedule production-db --interval daily --path ./backups
  acrynite backup schedule dev-db --interval 6h --path /var/backups
  acrynite backup schedule test-db --interval weekly --path ./backups
```

✅ **Note:** When you schedule a backup, the scheduler daemon automatically starts in the background and executes backups according to the interval.

**List Scheduled Backups:**
```bash
acrynite backup list
```

**Cancel a Scheduled Backup:**
```bash
acrynite backup cancel [schedule-id]

Example:
  acrynite backup cancel backup-production-db-20260614_094714
```

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
│   ├── backup.go          # Backup command logic
│   ├── command.go         # Root command setup
│   ├── db.go              # Database management commands
│   └── version.go         # Version command
├── internal/              # Internal packages
│   ├── backup/            # Backup implementation
│   │   └── full.go        # Full backup operations
│   ├── schedule/          # Schedule management
│   │   ├── persistence.go # Schedule persistence layer
│   │   └── manager.go     # Schedule manager & daemon
│   ├── config/            # Configuration management
│   ├── database/          # Database connectivity
│   │   ├── connect.go     # Connection handling
│   │   ├── register.go    # Registration logic
│   │   └── types.go       # Data types
│   └── cron/              # (Legacy scheduling)
├── storage/               # Local storage
│   ├── backups/           # Backup files
│   ├── configs/           # Configuration files (databases.json)
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
