package database

import (
	"fmt"
	"time"
)

// DBConfig represents a database configuration
type DBConfig struct {
	Name       string    `json:"name"`
	Type       string    `json:"type"`        // postgres or mysql
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	User       string    `json:"user"`
	Password   string    `json:"password"`
	Database   string    `json:"database"`
	SSLMode    string    `json:"ssl_mode"`    // For PostgreSQL only
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastTested time.Time `json:"last_tested"`
	IsActive   bool      `json:"is_active"`
}

// NewDBConfig creates a new database configuration
func NewDBConfig(name, dbType, host string, port int, user, password, database, sslMode string) *DBConfig {
	now := time.Now()
	// Set defaults based on database type
	if dbType == "" {
		dbType = "postgres"
	}
	if sslMode == "" && dbType == "postgres" {
		sslMode = "disable"
	}
	return &DBConfig{
		Name:      name,
		Type:      dbType,
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
	// Validate database type
	if db.Type != "postgres" && db.Type != "mysql" {
		return fmt.Errorf("database type must be 'postgres' or 'mysql'")
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
	// Validate SSL mode for PostgreSQL
	if db.Type == "postgres" {
		validSSLModes := map[string]bool{
			"disable":     true,
			"require":     true,
			"verify-ca":   true,
			"verify-full": true,
		}
		if !validSSLModes[db.SSLMode] {
			return fmt.Errorf("invalid SSL mode: %s (valid: disable, require, verify-ca, verify-full)", db.SSLMode)
		}
	}
	return nil
}

// ConnectionString generates a connection string based on database type
func (db *DBConfig) ConnectionString() string {
	if db.Type == "mysql" {
		// MySQL DSN format: user:password@tcp(host:port)/database
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
			db.User, db.Password, db.Host, db.Port, db.Database)
	}
	// PostgreSQL connection string
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		db.Host, db.Port, db.User, db.Password, db.Database, db.SSLMode)
}

// SafeConnectionString returns a connection string with password masked
func (db *DBConfig) SafeConnectionString() string {
	if db.Type == "mysql" {
		// MySQL DSN format with masked password
		return fmt.Sprintf("%s:***@tcp(%s:%d)/%s",
			db.User, db.Host, db.Port, db.Database)
	}
	// PostgreSQL connection string with masked password
	return fmt.Sprintf("host=%s port=%d user=%s password=*** dbname=%s sslmode=%s",
		db.Host, db.Port, db.User, db.Database, db.SSLMode)
}
