package location

import "time"

// Type identifies the kind of backend a location writes backups to.
type Type string

const (
	TypeLocal  Type = "local"
	TypeGDrive Type = "gdrive"
	TypeS3     Type = "s3"
)

// Source identifies how a location was created.
type Source string

const (
	SourceSchedule Source = "schedule"
	SourceManual   Source = "manual"
)

// Config is a durable handle to "a specific folder/bucket within a provider,
// tied to one DB". It survives a schedule being deleted or recreated, which
// is why restore operates on locations rather than schedule IDs directly.
type Config struct {
	Name         string    `json:"name"`
	Database     string    `json:"database"`
	Type         Type      `json:"type"`
	Provider     string    `json:"provider,omitempty"`  // provider name, for gdrive/s3
	Path         string    `json:"path,omitempty"`      // local backup directory
	FolderID     string    `json:"folder_id,omitempty"` // resolved gdrive destination folder
	Prefix       string    `json:"prefix,omitempty"`    // s3 key prefix (defaults to the database name)
	Source       Source    `json:"source"`
	ScheduleID   string    `json:"schedule_id,omitempty"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastBackupAt time.Time `json:"last_backup_at,omitempty"`
}

// BackupRef identifies a single backup within a location: Name is the human-readable
// filename (used for chronological sorting/display), and Identifier is whatever the
// backend needs to fetch it again (a local path, an S3 object key, or a Drive file ID).
type BackupRef struct {
	Name       string `json:"name"`
	Identifier string `json:"identifier"`
}

// Store is the on-disk representation of all locations, keyed by "database/name".
type Store struct {
	Locations map[string]*Config `json:"locations"`
	Version   string             `json:"version"`
}

// Key returns the storage key for a database/name pair.
func Key(database, name string) string {
	return database + "/" + name
}
