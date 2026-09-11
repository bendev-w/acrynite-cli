package location

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"acrynite-cli/internal/provider"
	"acrynite-cli/utils"

	"google.golang.org/api/drive/v3"
)

// driveService resolves the provider referenced by this location into a ready Drive client.
func (cfg *Config) driveService() (*drive.Service, error) {
	if cfg.Provider == "" {
		return nil, fmt.Errorf("location '%s' has no provider configured", cfg.Name)
	}

	providerMgr, err := provider.GetConfigManager()
	if err != nil {
		return nil, fmt.Errorf("failed to get provider manager: %w", err)
	}
	pCfg, err := providerMgr.Get(cfg.Provider)
	if err != nil {
		return nil, err
	}

	secrets, err := provider.NewSecretStore()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize secret store: %w", err)
	}

	return provider.NewDriveService(pCfg, secrets)
}

// EnsureGDriveFolder resolves (creating if necessary) the destination folder for this
// location's database backups and persists the resulting folder ID onto the config.
// Called once, the first time a gdrive location is used by a schedule.
func (cfg *Config) EnsureGDriveFolder() error {
	if cfg.FolderID != "" {
		return nil
	}

	svc, err := cfg.driveService()
	if err != nil {
		return err
	}

	folderName := fmt.Sprintf("Acrynite Backups - %s - %s", cfg.Database, cfg.Name)

	query := fmt.Sprintf("name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", escapeDriveQueryValue(folderName))
	res, err := svc.Files.List().Q(query).Fields("files(id, name)").Do()
	if err != nil {
		return fmt.Errorf("failed to search for backup folder: %w", err)
	}
	if len(res.Files) > 0 {
		cfg.FolderID = res.Files[0].Id
		return nil
	}

	folder, err := svc.Files.Create(&drive.File{
		Name:     folderName,
		MimeType: "application/vnd.google-apps.folder",
	}).Fields("id").Do()
	if err != nil {
		return fmt.Errorf("failed to create backup folder '%s': %w", folderName, err)
	}

	cfg.FolderID = folder.Id
	return nil
}

func escapeDriveQueryValue(v string) string {
	return strings.ReplaceAll(v, "'", `\'`)
}

func (cfg *Config) listGDriveBackups(dbName string) ([]BackupRef, error) {
	if cfg.FolderID == "" {
		return nil, fmt.Errorf("location '%s' has no resolved Drive folder yet", cfg.Name)
	}

	svc, err := cfg.driveService()
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf("'%s' in parents and name contains '%s' and trashed = false", cfg.FolderID, escapeDriveQueryValue(dbName+"_"))

	var refs []BackupRef
	pageToken := ""
	for {
		call := svc.Files.List().Q(query).Fields("nextPageToken, files(id, name)")
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		res, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to list backups in Drive folder: %w", err)
		}
		for _, f := range res.Files {
			if strings.HasSuffix(f.Name, ".sql") && !validBackupName(dbName, f.Name) {
				utils.Warning("Skipping malformed Google Drive backup filename: %s", f.Name)
				continue
			}
			if validBackupName(dbName, f.Name) {
				refs = append(refs, BackupRef{Name: f.Name, Identifier: f.Id})
			}
		}
		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
	}

	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

func (cfg *Config) uploadGDriveBackup(localPath, dbName string) (BackupRef, error) {
	if err := cfg.EnsureGDriveFolder(); err != nil {
		return BackupRef{}, err
	}

	svc, err := cfg.driveService()
	if err != nil {
		return BackupRef{}, err
	}

	filename := localPath
	if idx := strings.LastIndexAny(localPath, `/\`); idx >= 0 {
		filename = localPath[idx+1:]
	}

	file, err := os.Open(localPath)
	if err != nil {
		return BackupRef{}, fmt.Errorf("failed to open backup file for upload: %w", err)
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	created, err := svc.Files.Create(&drive.File{
		Name:    filename,
		Parents: []string{cfg.FolderID},
	}).Media(file).Context(ctx).Do()
	if err != nil {
		return BackupRef{}, fmt.Errorf("failed to upload backup to Google Drive: %w", err)
	}

	return BackupRef{Name: filename, Identifier: created.Id}, nil
}

func (cfg *Config) downloadGDriveBackup(fileID, destPath string) error {
	svc, err := cfg.driveService()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	resp, err := svc.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return fmt.Errorf("failed to download backup from Google Drive: %w", err)
	}
	defer resp.Body.Close()

	dest, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create local file for download: %w", err)
	}
	defer dest.Close()

	if _, err := io.Copy(dest, resp.Body); err != nil {
		return fmt.Errorf("failed to write downloaded backup: %w", err)
	}
	return nil
}
