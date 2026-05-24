package database

import (
	"fmt"
	"time"
)

// DBConfig represents a database configuration
type DBConfig struct {
	Name       string    `json:"name"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	User       string    `json:"user"`
	Password   string    `json:"password"`
	Database   string    `json:"database"`
	SSLMode    string    `json:"ssl_mode"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastTested time.Time `json:"last_tested"`
	IsActive   bool      `json:"is_active"`
}

// NewDBConfig creates a new database configuration
func NewDBConfig(name, host string, port int, user, password, database, sslMode string) *DBConfig {
	now := time.Now()
	if sslMode == "" {
		sslMode = "disable"
	}
	return &DBConfig{
		Name:      name,
		Host:      host,
		Port:      port,
		User:      user,
		Password:  password,
		Database:  database,
		SSLMode:   sslMode,
		CreatedAt: now,
		UpdatedAt: now,
		IsActive:  false,
	}
}

// Validate checks if the configuration is valid
func (db *DBConfig) Validate() error {
	if db.Name == "" {
		return fmt.Errorf("database name is required")
	}
	if db.Host == "" {
		return fmt.Errorf("host is required")
	}
	if db.Port <= 0 || db.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if db.User == "" {
		return fmt.Errorf("user is required")
	}
	if db.Database == "" {
		return fmt.Errorf("database name is required")
	}
	validSSLModes := map[string]bool{
		"disable":     true,
		"require":     true,
		"verify-ca":   true,
		"verify-full": true,
	}
	if !validSSLModes[db.SSLMode] {
		return fmt.Errorf("invalid SSL mode: %s (valid: disable, require, verify-ca, verify-full)", db.SSLMode)
	}
	return nil
}

// ConnectionString generates a PostgreSQL connection string
func (db *DBConfig) ConnectionString() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		db.Host, db.Port, db.User, db.Password, db.Database, db.SSLMode)
}

// SafeConnectionString returns a connection string with password masked
func (db *DBConfig) SafeConnectionString() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=*** dbname=%s sslmode=%s",
		db.Host, db.Port, db.User, db.Database, db.SSLMode)
}
