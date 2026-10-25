package provider

import "time"

// Type identifies the kind of cloud storage provider
type Type string

const (
	TypeGDrive Type = "gdrive"
	TypeS3     Type = "s3"
)

// Status values for a provider connection
const (
	StatusConnected     = "connected"
	StatusNotConfigured = "not_configured"
	StatusUnverified    = "unverified"
	StatusError         = "error"
)

// Config holds the non-secret metadata for a registered provider.
// Secrets (tokens, access keys) are stored separately via the SecretStore.
type Config struct {
	Name      string    `json:"name"`
	Type      Type      `json:"type"`
	Account   string    `json:"account,omitempty"`  // e.g. gdrive email
	Region    string    `json:"region,omitempty"`   // s3
	Bucket    string    `json:"bucket,omitempty"`   // s3
	Prefix    string    `json:"prefix,omitempty"`   // s3
	Endpoint  string    `json:"endpoint,omitempty"` // s3-compatible services
	Status    string    `json:"status"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store is the on-disk representation of all providers.
type Store struct {
	Providers map[string]*Config `json:"providers"`
	Version   string             `json:"version"`
}
