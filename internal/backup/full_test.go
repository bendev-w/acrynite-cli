package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseInterval(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"hourly", time.Hour, false},
		{"daily", 24 * time.Hour, false},
		{"weekly", 7 * 24 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"2h", 2 * time.Hour, false},
		{"invalid", 0, true},
		{"10s", 0, true}, // under 1 minute limit
	}

	for _, tt := range tests {
		got, err := ParseInterval(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseInterval(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.expected {
			t.Errorf("ParseInterval(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestResolveDumpExecutable_CustomPath(t *testing.T) {
	tempDir := t.TempDir()
	dummyExecutable := filepath.Join(tempDir, "mysqldump_dummy")
	_ = os.WriteFile(dummyExecutable, []byte("#!/bin/sh\nexit 0"), 0755)

	resolved, err := resolveDumpExecutable("mysql", dummyExecutable)
	if err != nil {
		t.Fatalf("unexpected error resolving custom dump path: %v", err)
	}

	if resolved != dummyExecutable {
		t.Errorf("expected '%s', got '%s'", dummyExecutable, resolved)
	}
}
