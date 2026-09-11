package location

import (
	"fmt"
	"sort"
	"strings"

	"acrynite-cli/utils"
)

// FindLatestBackup returns the newest backup file path for a database within
// this location. Only local locations are supported today - cloud discovery
// (gdrive/s3) requires provider SDK integration that hasn't been added yet.
func (cfg *Config) FindLatestBackup(dbName string) (string, error) {
	files, err := cfg.ListBackups(dbName)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no backup files found for '%s' in location '%s'", dbName, cfg.Name)
	}
	return files[len(files)-1], nil
}

// ListBackups returns all backup file paths for a database within this location,
// sorted oldest to newest (the backup filename timestamp format sorts lexicographically
// in chronological order).
func (cfg *Config) ListBackups(dbName string) ([]string, error) {
	switch cfg.Type {
	case TypeLocal:
		if cfg.Path == "" {
			return nil, fmt.Errorf("location '%s' has no local path configured", cfg.Name)
		}
		if !utils.Exists(cfg.Path) {
			return nil, fmt.Errorf("backup directory '%s' does not exist for location '%s'", cfg.Path, cfg.Name)
		}
		all, err := utils.ListFiles(cfg.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to list backups in '%s': %w", cfg.Path, err)
		}
		prefix := dbName + "_"
		var matches []string
		for _, f := range all {
			base := f
			if idx := strings.LastIndexAny(f, `/\`); idx >= 0 {
				base = f[idx+1:]
			}
			if strings.HasPrefix(base, prefix) && strings.HasSuffix(base, ".sql") {
				matches = append(matches, f)
			}
		}
		sort.Strings(matches)
		return matches, nil
	default:
		return nil, fmt.Errorf("backup discovery for provider type '%s' is not implemented yet (requires cloud SDK integration)", cfg.Type)
	}
}
