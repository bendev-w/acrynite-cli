package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// BaseURL is the Acrynite API base URL.
	// Override at build time: -ldflags "-X main.BaseURL=https://api.example.com"
	BaseURL    = "http://localhost:8000"
	configDir  = ".acrynite"
	configFile = "config.json"
)

// Config holds the persisted agent configuration stored locally.
type Config struct {
	Token       string `json:"token"`
	AgentID     int64  `json:"agent_id"`
	AgentName   string `json:"agent_name"`
	ProjectID   int64  `json:"project_id"`
	ProjectName string `json:"project_name"`
	UserEmail   string `json:"user_email"`
}

// configPath returns the full path to the config file (~/.acrynite/config.json).
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, configDir, configFile), nil
}

// SaveConfig persists cfg to disk.
func SaveConfig(cfg Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("could not write config file: %w", err)
	}

	return nil
}

// LoadConfig reads the persisted config from disk.
func LoadConfig() (Config, error) {
	path, err := configPath()
	if err != nil {
		return Config{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config not found — run 'acrynite init' first: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("could not parse config: %w", err)
	}

	return cfg, nil
}
