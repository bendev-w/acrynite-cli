package wal

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"acrynite-cli/internal/location"
	"acrynite-cli/utils"
)

// DefaultContainerSpoolDir is where the shared spool folder is expected on the database server.
const DefaultContainerSpoolDir = "/wal_archive"

// BuildSpoolArchiveCommand builds an archive_command for a Linux database server (for example in
// Docker) that only copies each segment into a folder shared with the Acrynite host. The copy is
// atomic (temp name, then rename) so the uploader never sees partial files.
func BuildSpoolArchiveCommand(serverOS, containerDir string) string {
	if serverOS == "windows" {
		dir := strings.TrimRight(strings.ReplaceAll(containerDir, "/", `\`), `\`)
		return fmt.Sprintf(`copy "%%p" "%s\%%f.tmp" && move /Y "%s\%%f.tmp" "%s\%%f"`, dir, dir, dir)
	}
	dir := strings.TrimRight(containerDir, "/")
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", `'"'"'`) + "'" }
	return fmt.Sprintf("cp %%p %s/%%f.tmp && mv -f %s/%%f.tmp %s/%%f", quote(dir), quote(dir), quote(dir))
}

// Archive modes: how completed WAL segments get from PostgreSQL to the backup location.
const (
	// ModeDirect runs this executable as archive_command on the database host.
	ModeDirect = "direct"
	// ModeSpool has the server copy segments into a shared folder that this CLI uploads.
	ModeSpool = "spool"
)

// ResolveMode picks the archive mode. "auto" prefers the shared folder only when one was asked
// for, direct when this CLI can be the server's archive_command, and streaming otherwise.
func ResolveMode(requested string, spoolDirGiven bool, server *DetectedServer) (string, error) {
	switch requested {
	case "", "auto":
		if spoolDirGiven {
			return ModeSpool, nil
		}
		if server != nil && server.Mode == ModeDirect {
			return ModeDirect, nil
		}
		return ModeStream, nil
	case ModeStream, ModeSpool:
		return requested, nil
	case ModeDirect:
		if server != nil && server.Mode == ModeSpool {
			return "", fmt.Errorf("--wal-mode direct cannot work: %s; use --wal-mode stream", server.Reason)
		}
		return ModeDirect, nil
	}
	return "", fmt.Errorf("unknown --wal-mode '%s' (expected auto, stream, direct or spool)", requested)
}

// DefaultSpoolDir is the host folder used when a spool is needed and none was given.
func DefaultSpoolDir() string {
	abs, err := filepath.Abs(filepath.Join("storage", "wal-spool"))
	if err != nil {
		return filepath.Join("storage", "wal-spool")
	}
	return abs
}

// DetectedServer describes what the database server is running on.
type DetectedServer struct {
	OS      string // "linux", "windows", "darwin" or "unknown"
	Version string
	Mode    string // ModeDirect or ModeSpool
	Reason  string // why ModeSpool was chosen
}

// DetectServer inspects the server first and decides how WAL can be archived. archive_command is
// run by the server, so this executable can only be used when the server is the same OS as this
// CLI and its data directory lives on this machine; a different OS, a container or a remote
// host all need the shared-folder (spool) mode.
func DetectServer(db *sql.DB) (*DetectedServer, error) {
	var version string
	if err := db.QueryRow("SELECT version()").Scan(&version); err != nil {
		return nil, fmt.Errorf("failed to read server version: %w", err)
	}
	d := &DetectedServer{OS: serverOSFromVersion(version), Version: version, Mode: ModeDirect}

	if d.OS != "unknown" && d.OS != runtime.GOOS {
		d.Mode = ModeSpool
		d.Reason = fmt.Sprintf("the server runs on %s but this CLI runs on %s", d.OS, runtime.GOOS)
		return d, nil
	}
	var dataDir string
	if err := db.QueryRow("SHOW data_directory").Scan(&dataDir); err == nil && dataDir != "" {
		if _, statErr := os.Stat(dataDir); os.IsNotExist(statErr) {
			d.Mode = ModeSpool
			d.Reason = fmt.Sprintf("the server's data directory %s does not exist on this machine (container or remote host)", dataDir)
		}
	}
	return d, nil
}

func serverOSFromVersion(version string) string {
	lower := strings.ToLower(version)
	switch {
	case strings.Contains(lower, "windows") || strings.Contains(lower, "mingw") || strings.Contains(lower, "visual c++"):
		return "windows"
	case strings.Contains(lower, "apple") || strings.Contains(lower, "darwin"):
		return "darwin"
	case strings.Contains(lower, "linux"):
		return "linux"
	}
	return "unknown"
}

// UploadSpool uploads every finished file in dir to the location's WAL archive and deletes each
// one after a successful upload. Failed files stay in place and are retried on the next call.
func UploadSpool(cfg *location.Config, dir string) (uploaded, failed int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			utils.Error("[WAL] Cannot read spool directory %s: %v", dir, err)
		}
		return 0, 0
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") || strings.HasSuffix(e.Name(), ".partial") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		path := filepath.Join(dir, name)
		size := int64(0)
		if info, statErr := os.Stat(path); statErr == nil {
			size = info.Size()
		}
		utils.Info("[WAL] Uploading %s (%d bytes) to %s location '%s'", name, size, cfg.Type, cfg.Name)
		ref, err := ArchiveFile(cfg, path, name)
		if err != nil {
			utils.Error("[WAL] Upload of %s failed (will retry): %v", name, err)
			failed++
			continue
		}
		if err := os.Remove(path); err != nil {
			utils.Warning("[WAL] Uploaded %s but could not delete it from the spool: %v", name, err)
		}
		utils.Success("[WAL] Archived %s -> %s", name, ref.Identifier)
		uploaded++
	}
	return uploaded, failed
}
