package provider

import (
	"fmt"
	"os"
	"sync"
	"time"

	"acrynite-cli/internal/config"
	"acrynite-cli/utils"
)

// ConfigManager manages registered cloud storage providers (credentials/metadata).
type ConfigManager struct {
	configMgr *config.Manager
	store     *Store
	mu        sync.RWMutex
}

var (
	globalManager *ConfigManager
	managerOnce   sync.Once
)

// GetConfigManager returns the global provider configuration manager.
func GetConfigManager() (*ConfigManager, error) {
	var initErr error
	managerOnce.Do(func() {
		globalManager, initErr = NewConfigManager()
	})
	return globalManager, initErr
}

// NewConfigManager creates a new provider configuration manager backed by providers.json.
func NewConfigManager() (*ConfigManager, error) {
	configMgr, err := config.NewManagerWithPath("./storage/configs", "providers.json")
	if err != nil {
		return nil, fmt.Errorf("failed to create config manager: %w", err)
	}

	manager := &ConfigManager{
		configMgr: configMgr,
		store: &Store{
			Providers: make(map[string]*Config),
			Version:   "1.0.0",
		},
	}

	if err := manager.load(); err != nil && !os.IsNotExist(err) {
		utils.Warning("Failed to load existing providers: %v", err)
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

	if cm.store.Providers == nil {
		cm.store.Providers = make(map[string]*Config)
	}
	return nil
}

func (cm *ConfigManager) save() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.configMgr.Save(cm.store)
}

// Register adds a new provider. Fails if a provider with the same name already exists.
func (cm *ConfigManager) Register(cfg *Config) error {
	cm.mu.Lock()
	if _, exists := cm.store.Providers[cfg.Name]; exists {
		cm.mu.Unlock()
		return fmt.Errorf("provider '%s' already exists", cfg.Name)
	}
	cfg.CreatedAt = time.Now()
	cfg.UpdatedAt = time.Now()
	cm.store.Providers[cfg.Name] = cfg
	cm.mu.Unlock()

	if err := cm.save(); err != nil {
		cm.mu.Lock()
		delete(cm.store.Providers, cfg.Name)
		cm.mu.Unlock()
		return fmt.Errorf("failed to save provider: %w", err)
	}
	return nil
}

// Get retrieves a provider by name.
func (cm *ConfigManager) Get(name string) (*Config, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	cfg, exists := cm.store.Providers[name]
	if !exists {
		return nil, fmt.Errorf("provider '%s' not found", name)
	}
	return cfg, nil
}

// List returns all registered providers.
func (cm *ConfigManager) List() []*Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	list := make([]*Config, 0, len(cm.store.Providers))
	for _, cfg := range cm.store.Providers {
		list = append(list, cfg)
	}
	return list
}

// UpdateStatus updates the status/note of a provider and persists it.
func (cm *ConfigManager) UpdateStatus(name, status, note string) error {
	cm.mu.Lock()
	cfg, exists := cm.store.Providers[name]
	if !exists {
		cm.mu.Unlock()
		return fmt.Errorf("provider '%s' not found", name)
	}
	cfg.Status = status
	cfg.Note = note
	cfg.UpdatedAt = time.Now()
	cm.mu.Unlock()

	return cm.save()
}

// Remove deletes a provider. Callers are responsible for checking whether
// active locations still reference it before calling this.
func (cm *ConfigManager) Remove(name string) error {
	cm.mu.Lock()
	if _, exists := cm.store.Providers[name]; !exists {
		cm.mu.Unlock()
		return fmt.Errorf("provider '%s' not found", name)
	}
	delete(cm.store.Providers, name)
	cm.mu.Unlock()

	return cm.save()
}

// Exists checks whether a provider with the given name is registered.
func (cm *ConfigManager) Exists(name string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	_, exists := cm.store.Providers[name]
	return exists
}
