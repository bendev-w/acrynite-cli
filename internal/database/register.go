package database

import (
	"fmt"
	"os"
	"sync"
	"time"

	"acrynite-cli/internal/config"
	"acrynite-cli/utils"
)

// DBConfigStore represents the database configuration storage structure
type DBConfigStore struct {
	Configs map[string]*DBConfig `json:"configs"`
	Version string               `json:"version"`
}

// ConfigManager manages database configurations
type ConfigManager struct {
	configMgr *config.Manager
	store     *DBConfigStore
	mu        sync.RWMutex
}

var (
	globalManager *ConfigManager
	managerOnce   sync.Once
)

// GetConfigManager returns the global configuration manager
func GetConfigManager() (*ConfigManager, error) {
	var initErr error
	managerOnce.Do(func() {
		globalManager, initErr = NewConfigManager()
	})
	return globalManager, initErr
}

// NewConfigManager creates a new configuration manager
func NewConfigManager() (*ConfigManager, error) {
	// Use local storage path instead of home directory
	configDir := "./storage/configs"

	configMgr, err := config.NewManagerWithPath(configDir, "databases.json")
	if err != nil {
		return nil, fmt.Errorf("failed to create config manager: %w", err)
	}

	manager := &ConfigManager{
		configMgr: configMgr,
		store: &DBConfigStore{
			Configs: make(map[string]*DBConfig),
			Version: "1.0.0",
		},
	}

	// Load existing configurations
	if err := manager.load(); err != nil && !os.IsNotExist(err) {
		utils.Warning("Failed to load existing configurations: %v", err)
	}

	return manager, nil
}

// load loads configurations from disk
func (cm *ConfigManager) load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.configMgr.Exists() {
		return os.ErrNotExist
	}

	if err := cm.configMgr.Load(cm.store); err != nil {
		return err
	}

	if cm.store.Configs == nil {
		cm.store.Configs = make(map[string]*DBConfig)
	}

	return nil
}

// save saves configurations to disk
func (cm *ConfigManager) save() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.configMgr.Save(cm.store)
}

// Register registers a new database configuration
func (cm *ConfigManager) Register(config *DBConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := config.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if _, exists := cm.store.Configs[config.Name]; exists {
		return fmt.Errorf("database '%s' already exists", config.Name)
	}

	cm.store.Configs[config.Name] = config

	cm.mu.Unlock()
	err := cm.save()
	cm.mu.Lock()

	if err != nil {
		delete(cm.store.Configs, config.Name)
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// Get retrieves a database configuration
func (cm *ConfigManager) Get(name string) (*DBConfig, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	config, exists := cm.store.Configs[name]
	if !exists {
		return nil, fmt.Errorf("database '%s' not found", name)
	}
	return config, nil
}

// List returns all database configurations
func (cm *ConfigManager) List() []*DBConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	configs := make([]*DBConfig, 0, len(cm.store.Configs))
	for _, config := range cm.store.Configs {
		configs = append(configs, config)
	}
	return configs
}

// Update updates an existing database configuration
func (cm *ConfigManager) Update(name string, updatedConfig *DBConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if database exists
	if _, exists := cm.store.Configs[name]; !exists {
		return fmt.Errorf("database '%s' not found", name)
	}

	// Validate the updated configuration
	if err := updatedConfig.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// If name changed, check for duplicates
	if updatedConfig.Name != name {
		if _, exists := cm.store.Configs[updatedConfig.Name]; exists {
			return fmt.Errorf("database '%s' already exists", updatedConfig.Name)
		}
		// Remove old entry
		delete(cm.store.Configs, name)
	}

	// Update timestamps
	updatedConfig.UpdatedAt = time.Now()

	// Store the updated configuration
	cm.store.Configs[updatedConfig.Name] = updatedConfig

	cm.mu.Unlock()
	err := cm.save()
	cm.mu.Lock()

	if err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// Remove removes a database configuration
func (cm *ConfigManager) Remove(name string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.store.Configs[name]; !exists {
		return fmt.Errorf("database '%s' not found", name)
	}

	delete(cm.store.Configs, name)

	cm.mu.Unlock()
	err := cm.save()
	cm.mu.Lock()

	if err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// Exists checks if a database configuration exists
func (cm *ConfigManager) Exists(name string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	_, exists := cm.store.Configs[name]
	return exists
}

// TestConnection tests a database connection
func (cm *ConfigManager) TestConnection(name string) error {
	config, err := cm.Get(name)
	if err != nil {
		return err
	}

	utils.Info("Testing connection to '%s'...", name)

	db, err := ConnectPostgresDB(config.ConnectionString())
	cm.mu.Lock()
	config.LastTested = time.Now()
	if err != nil {
		config.IsActive = false
		cm.mu.Unlock()
		cm.save()
		return fmt.Errorf("connection test failed: %w", err)
	}
	defer db.Close()

	config.IsActive = true
	cm.mu.Unlock()
	cm.save()

	return nil
}

// GetConfigPath returns the path to the configuration file
func (cm *ConfigManager) GetConfigPath() string {
	return cm.configMgr.GetConfigPath()
}

// Export exports configurations to a file
func (cm *ConfigManager) Export(filePath string) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.configMgr.Export(cm.store, filePath)
}

// Import imports configurations from a file
func (cm *ConfigManager) Import(filePath string) error {
	var importedStore DBConfigStore
	if err := cm.configMgr.Import(&importedStore, filePath); err != nil {
		return err
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	imported := 0
	skipped := 0
	for name, config := range importedStore.Configs {
		if _, exists := cm.store.Configs[name]; exists {
			utils.Warning("Skipping existing database: %s", name)
			skipped++
			continue
		}
		cm.store.Configs[name] = config
		imported++
	}

	cm.mu.Unlock()
	err := cm.save()
	cm.mu.Lock()

	if err != nil {
		return err
	}

	utils.Success("Imported %d database(s), skipped %d existing", imported, skipped)
	return nil
}
