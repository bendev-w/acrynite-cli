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
  --interval string    Backup frequency: hourly, daily, weekly, or duration (e.g., 30m, 2h, 24h) (required)
  --path string        Path to store backup files (required)

Example:
  acrynite backup schedule production-db --interval daily --path ./backups
  acrynite backup schedule dev-db --interval 6h --path /var/backups
  acrynite backup schedule test-db --interval weekly --path ./backups
```

**List Schedules:**
```bash
acrynite backup schedules
```

**Remove Schedule:**
```bash
acrynite backup remove-schedule [schedule-id]
```

#### Version

Display version information:

```bash
acrynite version
```

## Configuration

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

## Architecture

```
acrynite-cli/
├── cmd/                 # CLI command definitions
│   ├── backup.go       # Backup command logic
│   ├── command.go      # Root command setup
│   ├── db.go           # Database management commands
│   └── version.go      # Version command
├── internal/           # Internal packages
│   ├── backup/         # Backup implementation
│   ├── config/         # Configuration management
│   ├── cron/           # Scheduling logic
│   └── database/       # Database connectivity
├── storage/            # Local storage
│   ├── backups/        # Backup files
│   ├── configs/        # Configuration files
│   ├── logs/           # Application logs
│   └── schedules/      # Schedule metadata
├── utils/              # Utility functions
│   ├── filesystem.go   # File operations
│   ├── logger.go       # Logging utilities
│   ├── scheduler.go    # Scheduling utilities
│   └── timestamp.go    # Time utilities
└── main.go             # Entry point
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

## Common Use Cases

### Daily Backups
```bash
# Schedule a daily backup at the default time
acrynite backup schedule production-db --interval daily --path /backups/production
```

### Multiple Database Backups
```bash
# Register multiple databases
acrynite db register --name prod-main ...
acrynite db register --name prod-cache ...

# Schedule backups for each
acrynite backup schedule prod-main --interval daily --path /backups
acrynite backup schedule prod-cache --interval daily --path /backups
```

### Backup Migration
```bash
# Export configs from one machine
acrynite db export --file configs.json

# Import on another machine
acrynite db import --file configs.json
```

## Troubleshooting

**Connection Failed:**
- Verify database credentials with `acrynite db test [database-name]`
- Check if the database host and port are correct
- Ensure network connectivity to the database server

**Backup Failed:**
- Verify the backup path exists and has write permissions
- Check available disk space
- Review logs for detailed error messages

**Schedule Not Running:**
- Ensure the application remains running for scheduled tasks to execute
- Check system logs for errors
- Verify the schedule was created with `acrynite backup schedules`

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For issues, questions, or suggestions, please open an issue on GitHub.

---

Made with ❤️ by the Acrynite team
