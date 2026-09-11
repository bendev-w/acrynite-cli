package location

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"acrynite-cli/utils"
)

// FindLatestBackup returns the newest backup within this location for a database.
func (cfg *Config) FindLatestBackup(dbName string) (BackupRef, error) {
	refs, err := cfg.ListBackups(dbName)
	if err != nil {
		return BackupRef{}, err
	}
	if len(refs) == 0 {
		return BackupRef{}, fmt.Errorf("no backup files found for '%s' in location '%s'", dbName, cfg.Name)
	}
	return refs[len(refs)-1], nil
}

// ListBackups returns all backups for a database within this location, sorted oldest
// to newest by filename (the backup timestamp format sorts lexicographically in
// chronological order).
func (cfg *Config) ListBackups(dbName string) ([]BackupRef, error) {
	switch cfg.Type {
	case TypeLocal:
		return cfg.listLocalBackups(dbName)
	case TypeS3:
		return cfg.listS3Backups(dbName)
	case TypeGDrive:
		return cfg.listGDriveBackups(dbName)
	default:
		return nil, fmt.Errorf("backup discovery for location type '%s' is not implemented", cfg.Type)
	}
}

// UploadBackup makes a freshly created local dump file available at this location.
// For local locations the dump is already written in place, so this is a no-op.
func (cfg *Config) UploadBackup(localPath, dbName string) (BackupRef, error) {
	switch cfg.Type {
	case TypeLocal:
		return BackupRef{Name: baseName(localPath), Identifier: localPath}, nil
	case TypeS3:
		return cfg.uploadS3Backup(localPath, dbName)
	case TypeGDrive:
		return cfg.uploadGDriveBackup(localPath, dbName)
	default:
		return BackupRef{}, fmt.Errorf("backup upload for location type '%s' is not implemented", cfg.Type)
	}
}

// DownloadBackup makes ref available at destPath and returns the local path to use
// (for local locations this is just ref.Identifier, unchanged).
func (cfg *Config) DownloadBackup(ref BackupRef, destPath string) (string, error) {
	switch cfg.Type {
	case TypeLocal:
		return ref.Identifier, nil
	case TypeS3:
		return destPath, cfg.downloadS3Backup(ref.Identifier, destPath)
	case TypeGDrive:
		return destPath, cfg.downloadGDriveBackup(ref.Identifier, destPath)
	default:
		return "", fmt.Errorf("backup download for location type '%s' is not implemented", cfg.Type)
	}
}

func baseName(path string) string {
	if idx := strings.LastIndexAny(path, `/\`); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func validBackupName(dbName, name string) bool {
	prefix := dbName + "_"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".sql") {
		return false
	}
	timestamp := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".sql")
	_, err := time.Parse("20060102-150405", timestamp)
	return err == nil
}

func (cfg *Config) listLocalBackups(dbName string) ([]BackupRef, error) {
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
	var matches []BackupRef
	for _, f := range all {
		base := baseName(f)
		if strings.HasPrefix(base, prefix) && strings.HasSuffix(base, ".sql") && !validBackupName(dbName, base) {
			utils.Warning("Skipping malformed backup filename: %s", base)
			continue
		}
		if validBackupName(dbName, base) {
			matches = append(matches, BackupRef{Name: base, Identifier: f})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Name < matches[j].Name })
	return matches, nil
}
