// build functions for database connection
package database

import (
	"database/sql"
	"fmt"	
	_ "github.com/lib/pq" // Postgres driver
)

// ConnectPostgresDB establishes a connection to the PostgreSQL database using the provided connection string
func ConnectPostgresDB(connStr string) (*sql.DB, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Verify the connection is valid
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	return db, nil
}


