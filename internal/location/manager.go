package location

import (
	"fmt"
	"os"
	"sync"
	"time"

	"acrynite-cli/internal/config"
	"acrynite-cli/utils"
)

// ConfigManager manages backup locations (per-database durable backup handles).
type ConfigManager struct {
	configMgr *config.Manager
	store     *Store
	mu        sync.RWMutex
}

var (
	globalManager *ConfigManager
	managerOnce   sync.Once
)

// GetConfigManager returns the global location configuration manager.
func GetConfigManager() (*ConfigManager, error) {
	var initErr error
	managerOnce.Do(func() {
		globalManager, initErr = NewConfigManager()
	})
	return globalManager, initErr
}

// NewConfigManager creates a new location configuration manager backed by locations.json.
func NewConfigManager() (*ConfigManager, error) {
	configMgr, err := config.NewManagerWithPath("./storage/configs", "locations.json")
	if err != nil {
		return nil, fmt.Errorf("failed to create config manager: %w", err)
	}

	manager := &ConfigManager{
		configMgr: configMgr,
		store: &Store{
			Locations: make(map[string]*Config),
			Version:   "1.0.0",
		},
	}

	if err := manager.load(); err != nil && !os.IsNotExist(err) {
		utils.Warning("Failed to load existing locations: %v", err)
	}

	return manager, nil
}

func (cm *ConfigManager) load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.configMgr.Exists() {
		return os.ErrNotExist
	}

	if err := cm.configMgr.Load(cm.store); err != nil {
		return err
	}

	if cm.store.Locations == nil {
		cm.store.Locations = make(map[string]*Config)
	}
	return nil
}

func (cm *ConfigManager) save() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.configMgr.Save(cm.store)
}

// Register adds a new location. Fails if one with the same database/name already exists.
func (cm *ConfigManager) Register(cfg *Config) error {
	key := Key(cfg.Database, cfg.Name)

	cm.mu.Lock()
	if _, exists := cm.store.Locations[key]; exists {
		cm.mu.Unlock()
		return fmt.Errorf("location '%s' already exists for database '%s'", cfg.Name, cfg.Database)
	}
	cfg.CreatedAt = time.Now()
	cfg.UpdatedAt = time.Now()
	cm.store.Locations[key] = cfg
	cm.mu.Unlock()

	if err := cm.save(); err != nil {
		cm.mu.Lock()
		delete(cm.store.Locations, key)
		cm.mu.Unlock()
		return fmt.Errorf("failed to save location: %w", err)
	}
	return nil
}

// Upsert creates the location if it doesn't exist, or updates it in place if it does.
// Used by `backup schedule` to auto-create/update the location tied to a schedule.
func (cm *ConfigManager) Upsert(cfg *Config) error {
	key := Key(cfg.Database, cfg.Name)

	cm.mu.Lock()
	if existing, exists := cm.store.Locations[key]; exists {
		cfg.CreatedAt = existing.CreatedAt
	} else {
		cfg.CreatedAt = time.Now()
	}
	cfg.UpdatedAt = time.Now()
	cm.store.Locations[key] = cfg
	cm.mu.Unlock()

	return cm.save()
}

// Get retrieves a location by database and name.
func (cm *ConfigManager) Get(database, name string) (*Config, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	cfg, exists := cm.store.Locations[Key(database, name)]
	if !exists {
		return nil, fmt.Errorf("location '%s' not found for database '%s'", name, database)
	}
	return cfg, nil
}

// ListByDatabase returns all locations registered for a given database.
func (cm *ConfigManager) ListByDatabase(database string) []*Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	var list []*Config
	for _, cfg := range cm.store.Locations {
		if cfg.Database == database {
			list = append(list, cfg)
		}
	}
	return list
}

// ListAll returns every registered location across all databases.
func (cm *ConfigManager) ListAll() []*Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	list := make([]*Config, 0, len(cm.store.Locations))
	for _, cfg := range cm.store.Locations {
		list = append(list, cfg)
	}
	return list
}

// SetActive marks a location active/inactive without removing it (used by `backup unschedule`).
func (cm *ConfigManager) SetActive(database, name string, active bool) error {
	key := Key(database, name)

	cm.mu.Lock()
	cfg, exists := cm.store.Locations[key]
	if !exists {
		cm.mu.Unlock()
		return fmt.Errorf("location '%s' not found for database '%s'", name, database)
	}
	cfg.Active = active
	cfg.UpdatedAt = time.Now()
	cm.mu.Unlock()

	return cm.save()
}

// TouchLastBackup updates the last-backup timestamp for a location.
func (cm *ConfigManager) TouchLastBackup(database, name string, at time.Time) error {
	key := Key(database, name)

	cm.mu.Lock()
	cfg, exists := cm.store.Locations[key]
	if !exists {
		cm.mu.Unlock()
		return fmt.Errorf("location '%s' not found for database '%s'", name, database)
	}
	cfg.LastBackupAt = at
	cfg.UpdatedAt = time.Now()
	cm.mu.Unlock()

	return cm.save()
}

// Remove deletes a location entry entirely.
func (cm *ConfigManager) Remove(database, name string) error {
	key := Key(database, name)

	cm.mu.Lock()
	if _, exists := cm.store.Locations[key]; !exists {
		cm.mu.Unlock()
		return fmt.Errorf("location '%s' not found for database '%s'", name, database)
	}
	delete(cm.store.Locations, key)
	cm.mu.Unlock()

	return cm.save()
}
