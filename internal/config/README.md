# Configuration Manager

A reusable, generic configuration file management system for the Acrynite CLI.

## Overview

The `config.Manager` provides a thread-safe, generic way to manage JSON-based configuration files with automatic backups, import/export capabilities, and standardized storage locations.

## Features

✅ **Generic**: Works with any struct that can be JSON-marshaled  
✅ **Thread-safe**: All operations are protected by mutex locks  
✅ **Automatic backups**: Creates backups before saving changes  
✅ **Standardized location**: Uses `~/.acrynite-cli/` by default  
✅ **Import/Export**: Easy configuration sharing and migration  
✅ **Type-safe**: Works with Go structs  

## Usage

### Basic Usage

```go
import "acrynite-cli/internal/config"

// Define your configuration structure
type MyConfig struct {
    Setting1 string `json:"setting1"`
    Setting2 int    `json:"setting2"`
}

// Create a config manager
manager, err := config.NewManager("myconfig.json")
if err != nil {
    return err
}

// Create your config
myConfig := &MyConfig{
    Setting1: "value",
    Setting2: 42,
}

// Save configuration
if err := manager.Save(myConfig); err != nil {
    return err
}

// Load configuration
loadedConfig := &MyConfig{}
if err := manager.Load(loadedConfig); err != nil {
    return err
}
```

### Custom Path

```go
// Use a custom config directory
manager, err := config.NewManagerWithPath("/custom/path", "config.json")
```

### Export/Import

```go
// Export configuration
err := manager.Export(myConfig, "/path/to/export.json")

// Import configuration
importedConfig := &MyConfig{}
err := manager.Import(importedConfig, "/path/to/import.json")
```

### Check Existence

```go
if manager.Exists() {
    // Configuration file exists
}
```

### Get Paths

```go
configPath := manager.GetConfigPath()  // Full path to config file
configDir := manager.GetConfigDir()    // Config directory path
```

## Real-World Example: Database Configuration

Here's how the database module uses the config manager:

```go
package database

import "acrynite-cli/internal/config"

type DBConfigStore struct {
    Configs map[string]*DBConfig `json:"configs"`
    Version string               `json:"version"`
}

type ConfigManager struct {
    configMgr *config.Manager
    store     *DBConfigStore
    mu        sync.RWMutex
}

func NewConfigManager() (*ConfigManager, error) {
    // Create the generic config manager
    configMgr, err := config.NewManager("databases.json")
    if err != nil {
        return nil, err
    }

    manager := &ConfigManager{
        configMgr: configMgr,
        store: &DBConfigStore{
            Configs: make(map[string]*DBConfig),
            Version: "1.0.0",
        },
    }

    // Load existing data
    manager.load()
    return manager, nil
}

func (cm *ConfigManager) save() error {
    return cm.configMgr.Save(cm.store)
}

func (cm *ConfigManager) load() error {
    return cm.configMgr.Load(cm.store)
}
```

## Creating New Configuration Systems

You can easily create new configuration systems for different purposes:

### Example 1: API Keys Configuration

```go
package apikeys

import "acrynite-cli/internal/config"

type APIKeyStore struct {
    Keys    map[string]string `json:"keys"`
    Version string            `json:"version"`
}

type APIKeyManager struct {
    configMgr *config.Manager
    store     *APIKeyStore
}

func NewAPIKeyManager() (*APIKeyManager, error) {
    configMgr, err := config.NewManager("apikeys.json")
    if err != nil {
        return nil, err
    }

    mgr := &APIKeyManager{
        configMgr: configMgr,
        store: &APIKeyStore{
            Keys:    make(map[string]string),
            Version: "1.0.0",
        },
    }

    configMgr.Load(mgr.store)
    return mgr, nil
}

func (m *APIKeyManager) AddKey(name, key string) error {
    m.store.Keys[name] = key
    return m.configMgr.Save(m.store)
}

func (m *APIKeyManager) GetKey(name string) (string, error) {
    key, exists := m.store.Keys[name]
    if !exists {
        return "", fmt.Errorf("key '%s' not found", name)
    }
    return key, nil
}
```

### Example 2: User Preferences

```go
package preferences

import "acrynite-cli/internal/config"

type UserPreferences struct {
    Theme        string `json:"theme"`
    Language     string `json:"language"`
    AutoUpdate   bool   `json:"auto_update"`
    LogLevel     string `json:"log_level"`
}

type PreferencesManager struct {
    configMgr *config.Manager
    prefs     *UserPreferences
}

func NewPreferencesManager() (*PreferencesManager, error) {
    configMgr, err := config.NewManager("preferences.json")
    if err != nil {
        return nil, err
    }

    mgr := &PreferencesManager{
        configMgr: configMgr,
        prefs: &UserPreferences{
            Theme:      "dark",
            Language:   "en",
            AutoUpdate: true,
            LogLevel:   "info",
        },
    }

    // Load existing preferences or use defaults
    if configMgr.Exists() {
        configMgr.Load(mgr.prefs)
    } else {
        // Save defaults
        configMgr.Save(mgr.prefs)
    }

    return mgr, nil
}

func (m *PreferencesManager) SetTheme(theme string) error {
    m.prefs.Theme = theme
    return m.configMgr.Save(m.prefs)
}

func (m *PreferencesManager) GetTheme() string {
    return m.prefs.Theme
}
```

### Example 3: Server Configurations

```go
package servers

import "acrynite-cli/internal/config"

type ServerConfig struct {
    Name     string `json:"name"`
    Host     string `json:"host"`
    Port     int    `json:"port"`
    Protocol string `json:"protocol"`
}

type ServerStore struct {
    Servers map[string]*ServerConfig `json:"servers"`
    Version string                   `json:"version"`
}

func NewServerManager() (*ServerManager, error) {
    configMgr, err := config.NewManager("servers.json")
    if err != nil {
        return nil, err
    }

    mgr := &ServerManager{
        configMgr: configMgr,
        store: &ServerStore{
            Servers: make(map[string]*ServerConfig),
            Version: "1.0.0",
        },
    }

    configMgr.Load(mgr.store)
    return mgr, nil
}
```

## Benefits of Using Config Manager

1. **Consistency**: All configuration files use the same format and location
2. **Safety**: Automatic backups prevent data loss
3. **Maintainability**: Centralized config logic reduces code duplication
4. **Extensibility**: Easy to add new configuration types
5. **Testability**: Easy to mock or use custom paths for testing

## File Structure

All configurations are stored in:
- **Linux/macOS**: `~/.acrynite-cli/`
- **Windows**: `C:\Users\<username>\.acrynite-cli\`

Examples:
- `~/.acrynite-cli/databases.json` - Database configurations
- `~/.acrynite-cli/apikeys.json` - API keys
- `~/.acrynite-cli/preferences.json` - User preferences
- `~/.acrynite-cli/servers.json` - Server configurations

## Thread Safety

All operations in `config.Manager` are thread-safe using `sync.RWMutex`:
- **Read operations** (`Load`, `Exists`, `Export`) use read locks
- **Write operations** (`Save`, `Delete`) use write locks

## Automatic Backups

Before saving, the manager automatically creates a backup with timestamp:
```
~/.acrynite-cli/databases_20260523-103000.json
```

You can restore from backups by manually renaming the file.

## Best Practices

1. **Define clear struct types**: Use well-defined structs with JSON tags
2. **Version your configs**: Include a version field for future migrations
3. **Validate before saving**: Always validate data before calling `Save()`
4. **Handle errors**: Check for `os.ErrNotExist` when loading
5. **Use locks**: If you add custom operations, maintain thread safety
6. **Export regularly**: Use export functionality for backups

## Integration with Utils

The config manager integrates with the `utils` package:
- `utils.GetHomeDir()` - Get home directory
- `utils.EnsureDirExists()` - Create directories
- `utils.ReadFile()` / `utils.WriteFile()` - File operations
- `utils.BackupFile()` - Automatic backups
- `utils.Success()` / `utils.Warning()` - Logging
