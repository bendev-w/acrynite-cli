package location

import "testing"

func TestValidBackupName(t *testing.T) {
	tests := []struct {
		name     string
		database string
		filename string
		want     bool
	}{
		{name: "valid", database: "production", filename: "production_20260911-143005.sql", want: true},
		{name: "database with hyphen", database: "production-db", filename: "production-db_20260911-143005.sql", want: true},
		{name: "wrong database", database: "production", filename: "staging_20260911-143005.sql", want: false},
		{name: "invalid timestamp", database: "production", filename: "production_latest.sql", want: false},
		{name: "invalid date", database: "production", filename: "production_20261340-256199.sql", want: false},
		{name: "wrong extension", database: "production", filename: "production_20260911-143005.dump", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validBackupName(tt.database, tt.filename); got != tt.want {
				t.Fatalf("validBackupName(%q, %q) = %v, want %v", tt.database, tt.filename, got, tt.want)
			}
		})
	}
}
