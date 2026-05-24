package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"acrynite-cli/utils"
)

// Manager provides generic configuration file management
type Manager struct {
	configDir  string
	configFile string
	mu         sync.RWMutex
}

// NewManager creates a new configuration manager
func NewManager(configFileName string) (*Manager, error) {
	home, err := utils.GetHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(home, ".acrynite-cli")
	if err := utils.EnsureDirExists(configDir); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	return &Manager{
		configDir:  configDir,
		configFile: filepath.Join(configDir, configFileName),
	}, nil
}

// NewManagerWithPath creates a manager with a custom config directory
func NewManagerWithPath(configDir, configFileName string) (*Manager, error) {
	if err := utils.EnsureDirExists(configDir); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	return &Manager{
		configDir:  configDir,
		configFile: filepath.Join(configDir, configFileName),
	}, nil
}

// Load loads configuration from file into the provided struct
func (m *Manager) Load(v interface{}) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !utils.Exists(m.configFile) {
		return os.ErrNotExist
	}

	data, err := utils.ReadFile(m.configFile)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return nil
}

// Save saves configuration to file
func (m *Manager) Save(v interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := utils.WriteFile(m.configFile, data); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Exists checks if the configuration file exists
func (m *Manager) Exists() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return utils.Exists(m.configFile)
}

// GetConfigPath returns the full path to the configuration file
func (m *Manager) GetConfigPath() string {
	return m.configFile
}

// GetConfigDir returns the configuration directory path
func (m *Manager) GetConfigDir() string {
	return m.configDir
}

// Delete removes the configuration file
func (m *Manager) Delete() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !utils.Exists(m.configFile) {
		return nil
	}

	return utils.DeleteFile(m.configFile)
}

// Export exports configuration to a specific file
func (m *Manager) Export(v interface{}, exportPath string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := utils.WriteFile(exportPath, data); err != nil {
		return fmt.Errorf("failed to export config: %w", err)
	}

	utils.Success("Exported configuration to: %s", exportPath)
	return nil
}

// Import imports configuration from a specific file
func (m *Manager) Import(v interface{}, importPath string) error {
	data, err := utils.ReadFile(importPath)
	if err != nil {
		return fmt.Errorf("failed to read import file: %w", err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("failed to parse import file: %w", err)
	}

	utils.Success("Imported configuration from: %s", importPath)
	return nil
}
