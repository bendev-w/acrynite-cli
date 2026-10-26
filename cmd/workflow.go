package cmd

// This file contains the supported v2 command surface.  The older command
// implementations remain in the source tree only for migration safety; they
// are intentionally not attached to the root command.

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"acrynite-cli/internal/backup"
	"acrynite-cli/internal/catalog"
	"acrynite-cli/internal/database"
	"acrynite-cli/internal/location"
	"acrynite-cli/internal/provider"
	"acrynite-cli/internal/restore"
	"acrynite-cli/internal/wal"

	"github.com/spf13/cobra"
)

var (
	flowHost, flowUser, flowDatabase, flowPassword, flowSSLMode, flowDumpPath string
	flowPort                                                                  int
	flowFormat                                                                string
)

var databaseCmd = &cobra.Command{Use: "database", Short: "Manage registered PostgreSQL databases"}
var databaseRegisterCmd = &cobra.Command{
	Use: "register <name>", Short: "Register a PostgreSQL connection profile", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if flowHost == "" || flowUser == "" || flowDatabase == "" {
			return fmt.Errorf("--host, --username, and --database are required")
		}
		if flowPort == 0 {
			flowPort = 5432
		}
		cfg := database.NewDBConfig(args[0], "postgres", flowHost, flowPort, flowUser, flowPassword, flowDatabase, flowSSLMode)
		cfg.DumpPath = flowDumpPath
		m, err := database.GetConfigManager()
		if err != nil {
			return err
		}
		if err := m.Register(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Registered PostgreSQL database %q. Test it with: acrynite database test-connection %s\n", args[0], args[0])
		return nil
	},
}
var databaseListCmd = &cobra.Command{Use: "list", Short: "List registered PostgreSQL databases", RunE: func(cmd *cobra.Command, args []string) error {
	m, err := database.GetConfigManager()
	if err != nil {
		return err
	}
	items := m.List()
	if flowFormat == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tHOST\tPORT\tDATABASE\tSTATUS")
	for _, item := range items {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", item.Name, item.Host, item.Port, item.Database, databaseStatus(item.IsActive))
	}
	return w.Flush()
}}
var databaseShowCmd = &cobra.Command{Use: "show <name>", Short: "Show safe database profile details", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	m, err := database.GetConfigManager()
	if err != nil {
		return err
	}
	item, err := m.Get(args[0])
	if err != nil {
		return err
	}
	if flowFormat == "json" {
		safe := struct {
			Name, Host, User, Database, SSLMode string
			Port                                int
		}{item.Name, item.Host, item.User, item.Database, item.SSLMode, item.Port}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(safe)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Name: %s\nHost: %s\nPort: %d\nUsername: %s\nDatabase: %s\nSSL mode: %s\n", item.Name, item.Host, item.Port, item.User, item.Database, item.SSLMode)
	return nil
}}
var databaseTestCmd = &cobra.Command{Use: "test-connection <name>", Short: "Test a PostgreSQL connection", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	m, err := database.GetConfigManager()
	if err != nil {
		return err
	}
	if err = m.TestConnection(args[0]); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Connection successful")
	return nil
}}

var (
	storageProvider, storageBucket, storagePrefix, storageRegion, storageEndpoint, storageAccessKey, storageSecretKey string
)
var storageCmd = &cobra.Command{Use: "storage", Short: "Manage registered S3 storage profiles"}
var storageRegisterCmd = &cobra.Command{Use: "register <name>", Short: "Register an S3 destination", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	if storageProvider != "s3" {
		return fmt.Errorf("only --provider s3 is supported")
	}
	if storageBucket == "" {
		return fmt.Errorf("--bucket is required")
	}
	if storageRegion == "" {
		storageRegion = "us-east-1"
	}
	if (storageAccessKey == "") != (storageSecretKey == "") {
		return fmt.Errorf("--access-key and --secret-key must be supplied together")
	}
	m, err := provider.GetConfigManager()
	if err != nil {
		return err
	}
	if m.Exists(args[0]) {
		return fmt.Errorf("storage profile %q already exists", args[0])
	}
	if storageAccessKey != "" {
		secrets, err := provider.NewSecretStore()
		if err != nil {
			return err
		}
		if err = secrets.Set(args[0]+":access_key", storageAccessKey); err != nil {
			return err
		}
		if err = secrets.Set(args[0]+":secret_key", storageSecretKey); err != nil {
			return err
		}
	}
	cfg := &provider.Config{Name: args[0], Type: provider.TypeS3, Region: storageRegion, Bucket: storageBucket, Prefix: strings.Trim(storagePrefix, "/"), Endpoint: storageEndpoint, Status: provider.StatusUnverified}
	if err = m.Register(cfg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Registered S3 storage %q at s3://%s/%s\n", args[0], storageBucket, strings.Trim(storagePrefix, "/"))
	return nil
}}
var storageListCmd = &cobra.Command{Use: "list", Short: "List registered S3 storage", RunE: func(cmd *cobra.Command, args []string) error {
	m, err := provider.GetConfigManager()
	if err != nil {
		return err
	}
	list := m.List()
	filtered := make([]*provider.Config, 0, len(list))
	for _, v := range list {
		if v.Type == provider.TypeS3 {
			filtered = append(filtered, v)
		}
	}
	if flowFormat == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(filtered)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tBUCKET\tPREFIX\tREGION\tSTATUS")
	for _, v := range filtered {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", v.Name, v.Bucket, v.Prefix, v.Region, v.Status)
	}
	return w.Flush()
}}

var backupDatabase, backupType, backupStorage, backupPathV2, backupLabel, backupListType, backupListStorage, backupListStatus string
var backupLimit int
var backupCmd = &cobra.Command{Use: "backup", Short: "Run and inspect PostgreSQL backups"}
var backupRunV2Cmd = &cobra.Command{Use: "run", Short: "Run a one-time backup", RunE: runBackupV2}
var backupListV2Cmd = &cobra.Command{Use: "list", Short: "List individual backup runs", RunE: runBackupListV2}
var backupShowV2Cmd = &cobra.Command{Use: "show <backup-id>", Short: "Show one backup manifest", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	c, err := catalog.NewManager()
	if err != nil {
		return err
	}
	v, err := c.GetBackup(args[0])
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(v)
}}

func runBackupV2(cmd *cobra.Command, _ []string) error {
	if backupDatabase == "" || backupType == "" {
		return fmt.Errorf("--database and --type are required")
	}
	if backupType != catalog.TypeLogical && backupType != catalog.TypePhysical {
		return fmt.Errorf("--type must be physical or logical")
	}
	if (backupStorage == "") == (backupPathV2 == "") {
		return fmt.Errorf("provide exactly one of --storage or --path")
	}
	dbMgr, err := database.GetConfigManager()
	if err != nil {
		return err
	}
	dbCfg, err := dbMgr.Get(backupDatabase)
	if err != nil {
		return err
	}
	if dbCfg.Type != "postgres" {
		return fmt.Errorf("database %q is not PostgreSQL", backupDatabase)
	}
	c, err := catalog.NewManager()
	if err != nil {
		return err
	}
	id := backupID()
	manifest := &catalog.Manifest{ID: id, Type: backupType, Status: catalog.StatusRunning, Database: backupDatabase, Label: backupLabel, StartedAt: time.Now().UTC()}
	var loc *location.Config
	var destDir string
	if backupStorage != "" {
		var locator catalog.StorageLocator
		loc, locator, err = storageLocation(backupStorage)
		if err != nil {
			return err
		}
		manifest.Storage = locator
		destDir = filepath.Join(os.TempDir(), "acrynite", id)
	} else {
		destDir = backupPathV2
		manifest.Storage = catalog.StorageLocator{Provider: "local", Artifact: ""}
	}
	if err = os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create backup staging directory: %w", err)
	}
	if err = c.PutBackup(manifest); err != nil {
		return err
	}
	fail := func(e error) error {
		manifest.Status = catalog.StatusFailed
		manifest.Error = e.Error()
		manifest.CompletedAt = time.Now().UTC()
		_ = c.PutBackup(manifest)
		return e
	}
	if db, openErr := database.ConnectPostgresDB(dbCfg.ConnectionString()); openErr == nil {
		_ = db.QueryRow("SHOW server_version").Scan(&manifest.PostgresVersion)
		_ = db.QueryRow("SELECT system_identifier::text FROM pg_control_system()").Scan(&manifest.ClusterIdentifier)
		_ = db.Close()
	}
	var artifact string
	if backupType == catalog.TypeLogical {
		artifact = filepath.Join(destDir, id+".sql")
		err = backup.PerformPostgresDump(dbCfg, artifact)
	} else {
		artifact, err = backup.PerformBaseBackup(dbCfg, backupDatabase, destDir)
		if err == nil {
			renamed := filepath.Join(destDir, id+".tar")
			err = os.Rename(artifact, renamed)
			artifact = renamed
		}
	}
	if err != nil {
		return fail(err)
	}
	info, err := os.Stat(artifact)
	if err != nil {
		return fail(err)
	}
	manifest.SizeBytes = info.Size()
	if loc != nil {
		ref, uploadErr := loc.UploadBackup(artifact, backupDatabase)
		if uploadErr != nil {
			return fail(uploadErr)
		}
		manifest.Storage.Artifact = ref.Identifier
		_ = os.Remove(artifact)
		_ = os.Remove(destDir)
	} else {
		manifest.Storage.Artifact = artifact
	}
	if walCfg, walErr := c.GetWAL(backupDatabase); walErr == nil {
		manifest.WALStorage = &walCfg.Storage
	}
	manifest.Status = catalog.StatusCompleted
	manifest.CompletedAt = time.Now().UTC()
	if err = writeManifest(manifest, loc); err != nil {
		return err
	}
	if err = c.PutBackup(manifest); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Backup completed: %s (%s)\n", id, backupType)
	return nil
}

func runBackupListV2(cmd *cobra.Command, _ []string) error {
	if backupDatabase == "" {
		return fmt.Errorf("--database is required")
	}
	c, err := catalog.NewManager()
	if err != nil {
		return err
	}
	items := c.ListBackups(backupDatabase, backupListType, backupListStatus, backupListStorage, backupLimit)
	if flowFormat == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "BACKUP ID\tTYPE\tCREATED\tSTORAGE\tSTATUS\tSIZE")
	for _, v := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n", v.ID, v.Type, v.StartedAt.Format(time.RFC3339), displayStorage(v.Storage), v.Status, v.SizeBytes)
	}
	return w.Flush()
}

var scheduleDatabase, scheduleType, scheduleInterval, scheduleStorage, schedulePath string
var scheduleCmd = &cobra.Command{Use: "schedule", Short: "Manage PostgreSQL backup schedules"}
var scheduleCreateCmd = &cobra.Command{Use: "create <schedule-name>", Short: "Create a recurring backup schedule", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	if scheduleDatabase == "" || scheduleType == "" || scheduleInterval == "" {
		return fmt.Errorf("--database, --type, and --interval are required")
	}
	if scheduleType != catalog.TypeLogical && scheduleType != catalog.TypePhysical {
		return fmt.Errorf("--type must be physical or logical")
	}
	if (scheduleStorage == "") == (schedulePath == "") {
		return fmt.Errorf("provide exactly one of --storage or --path")
	}
	d, err := time.ParseDuration(scheduleInterval)
	if err != nil || d <= 0 {
		return fmt.Errorf("--interval must be a positive Go duration such as 24h or 168h")
	}
	m, err := catalog.NewManager()
	if err != nil {
		return err
	}
	s := &catalog.Schedule{Name: args[0], Database: scheduleDatabase, Type: scheduleType, Interval: d, Path: schedulePath, Enabled: true, CreatedAt: time.Now().UTC(), NextRunAt: time.Now().UTC().Add(d)}
	if scheduleStorage != "" {
		_, l, err := storageLocation(scheduleStorage)
		if err != nil {
			return err
		}
		s.Storage = &l
	}
	if err = m.PutSchedule(s); err != nil {
		return err
	}
	// The daemon executes due schedules in a separate process so schedules keep
	// running after this command exits.
	if exe, exeErr := os.Executable(); exeErr == nil {
		daemon := exec.Command(exe, "schedule", "daemon")
		if startErr := daemon.Start(); startErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: schedule daemon was not started: %v\n", startErr)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created schedule %q. The scheduler daemon will run it every %s.\n", args[0], d)
	return nil
}}
var scheduleListDatabase, scheduleListStatus string
var scheduleListCmd = &cobra.Command{Use: "list", Short: "List recurring schedules", RunE: func(cmd *cobra.Command, args []string) error {
	m, err := catalog.NewManager()
	if err != nil {
		return err
	}
	list := m.ListSchedules(scheduleListDatabase, scheduleListStatus)
	if flowFormat == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(list)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tDATABASE\tTYPE\tINTERVAL\tDESTINATION\tSTATUS\tNEXT RUN")
	for _, s := range list {
		where := s.Path
		if s.Storage != nil {
			where = displayStorage(*s.Storage)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.Name, s.Database, s.Type, s.Interval, where, databaseStatus(s.Enabled), s.NextRunAt.Format(time.RFC3339))
	}
	return w.Flush()
}}
var scheduleDaemonCmd = &cobra.Command{Use: "daemon", Hidden: true, RunE: runScheduleDaemonV2}

func runScheduleDaemonV2(cmd *cobra.Command, _ []string) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := runDueSchedules(cmd); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "schedule daemon:", err)
		}
		<-ticker.C
	}
}

func runDueSchedules(cmd *cobra.Command) error {
	m, err := catalog.NewManager()
	if err != nil {
		return err
	}
	for _, s := range m.ListSchedules("", "enabled") {
		if time.Now().UTC().Before(s.NextRunAt) {
			continue
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		args := []string{"backup", "run", "--database", s.Database, "--type", s.Type, "--name", s.Name}
		if s.Storage != nil {
			args = append(args, "--storage", s.Storage.Profile)
		} else {
			args = append(args, "--path", s.Path)
		}
		output, runErr := exec.Command(exe, args...).CombinedOutput()
		s.LastRunAt = time.Now().UTC()
		s.NextRunAt = s.LastRunAt.Add(s.Interval)
		if runErr != nil {
			s.LastError = strings.TrimSpace(string(output))
		} else {
			s.LastError = ""
		}
		if err := m.UpdateSchedule(s); err != nil {
			return err
		}
	}
	return nil
}

var walStorage, walPath, walPushFile, walPushName string
var walCmd = &cobra.Command{Use: "wal", Short: "Configure PostgreSQL WAL archiving"}
var walEnableCmd = &cobra.Command{Use: "enable <registered-database-or-cluster>", Short: "Enable cluster-level WAL archiving to S3", Args: cobra.ExactArgs(1), RunE: runWALEnableV2}
var walStatusCmd = &cobra.Command{Use: "status <registered-database-or-cluster>", Short: "Show WAL archive status", Args: cobra.ExactArgs(1), RunE: runWALStatusV2}
var walPushCmd = &cobra.Command{Use: "push <registered-database-or-cluster>", Hidden: true, Args: cobra.ExactArgs(1), RunE: runWALPushV2}

func runWALEnableV2(cmd *cobra.Command, args []string) error {
	if (walStorage == "") == (walPath == "") {
		return fmt.Errorf("exactly one of --storage or --path is required")
	}
	var locator catalog.StorageLocator
	if walPath != "" {
		abs, absErr := filepath.Abs(walPath)
		if absErr != nil {
			return absErr
		}
		if mkErr := os.MkdirAll(filepath.Join(abs, "wal"), 0755); mkErr != nil {
			return fmt.Errorf("create WAL directory: %w", mkErr)
		}
		locator = catalog.StorageLocator{Provider: "local", Artifact: abs}
	} else {
		var locErr error
		_, locator, locErr = storageLocation(walStorage)
		if locErr != nil {
			return locErr
		}
	}
	dbm, err := database.GetConfigManager()
	if err != nil {
		return err
	}
	cfg, err := dbm.Get(args[0])
	if err != nil {
		return err
	}
	db, err := database.ConnectPostgresDB(cfg.ConnectionString())
	if err != nil {
		return err
	}
	defer db.Close()
	var dataDir string
	if err = db.QueryRow("SHOW data_directory").Scan(&dataDir); err != nil {
		return fmt.Errorf("read PostgreSQL data directory: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	archiveCommand := buildDatabaseArchiveCommand(exe, wd, dataDir, args[0])
	restart, err := wal.EnableArchive(db, archiveCommand, "5min")
	if err != nil {
		return err
	}
	c, err := catalog.NewManager()
	if err != nil {
		return err
	}
	if err = c.PutWAL(&catalog.WALConfig{Database: args[0], Storage: locator, EnabledAt: time.Now().UTC()}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "WAL archiving configured for %s to %s", args[0], displayStorage(locator))
	if restart {
		fmt.Fprint(cmd.OutOrStdout(), ". Restart PostgreSQL to activate archive_mode")
	}
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
func runWALPushV2(cmd *cobra.Command, args []string) error {
	if walPushFile == "" || walPushName == "" {
		return fmt.Errorf("--file and --name are required")
	}
	c, err := catalog.NewManager()
	if err != nil {
		return err
	}
	wcfg, err := c.GetWAL(args[0])
	if err != nil {
		return fmt.Errorf("WAL is not configured for %q: %w", args[0], err)
	}
	loc, err := locationFromLocator(wcfg.Storage)
	if err != nil {
		return err
	}
	loc.Database = args[0]
	_, err = wal.ArchiveFile(loc, walPushFile, walPushName)
	return err
}
func runWALStatusV2(cmd *cobra.Command, args []string) error {
	dbm, err := database.GetConfigManager()
	if err != nil {
		return err
	}
	cfg, err := dbm.Get(args[0])
	if err != nil {
		return err
	}
	db, err := database.ConnectPostgresDB(cfg.ConnectionString())
	if err != nil {
		return err
	}
	defer db.Close()
	var mode, command, lastOK, lastFailed string
	var count int64
	if err = db.QueryRow("SHOW archive_mode").Scan(&mode); err != nil {
		return err
	}
	_ = db.QueryRow("SHOW archive_command").Scan(&command)
	_ = db.QueryRow("SELECT COALESCE(last_archived_wal,''), COALESCE(last_failed_wal,''), failed_count FROM pg_stat_archiver").Scan(&lastOK, &lastFailed, &count)
	fmt.Fprintf(cmd.OutOrStdout(), "Archive mode: %s\nArchive command: %s\nLast successful WAL: %s\nLast failed WAL: %s\nFailed count: %d\n", mode, command, lastOK, lastFailed, count)
	c, err := catalog.NewManager()
	if err == nil {
		if w, err := c.GetWAL(args[0]); err == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Configured destination: %s\n", displayStorage(w.Storage))
		}
	}
	return nil
}

var recoveryDatabase string
var recoveryPointsCmd = &cobra.Command{Use: "recovery-points", Short: "Inspect physical-backup recovery coverage"}
var recoveryPointsListCmd = &cobra.Command{Use: "list", Short: "List known physical recovery points", RunE: func(cmd *cobra.Command, args []string) error {
	if recoveryDatabase == "" {
		return fmt.Errorf("--database is required")
	}
	c, err := catalog.NewManager()
	if err != nil {
		return err
	}
	items := c.ListBackups(recoveryDatabase, catalog.TypePhysical, catalog.StatusCompleted, "", 0)
	if len(items) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No completed physical backups are catalogued.")
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), "BASE BACKUP\tCONSISTENT AT\tWAL DESTINATION\tRECOVERY STATUS")
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	for _, v := range items {
		walDest := "not configured"
		state := "base backup only; WAL continuity has not been verified"
		if v.WALStorage != nil {
			walDest = displayStorage(*v.WALStorage)
			state = "WAL destination configured; use restore plan before recovery"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", v.ID, v.CompletedAt.Format(time.RFC3339), walDest, state)
	}
	return w.Flush()
}}

var restoreBackupID, restoreDatabase, restoreTarget, restoreTargetTime, restoreTargetDatabase, restoreContainerName, restorePublishPort string
var restoreLatest bool
var restoreCmd = &cobra.Command{Use: "restore", Short: "Plan and run safe PostgreSQL restores"}
var restorePlanCmd = &cobra.Command{Use: "plan", Short: "Preview a restore plan", RunE: runRestorePlan}
var restoreRunCmd = &cobra.Command{Use: "run", Short: "Run a logical restore or prepare a physical Docker recovery", RunE: runRestoreRun}

func selectRestoreBackup() (*catalog.Manifest, error) {
	c, err := catalog.NewManager()
	if err != nil {
		return nil, err
	}
	if restoreBackupID != "" && restoreLatest {
		return nil, fmt.Errorf("--backup-id and --latest are mutually exclusive")
	}
	if restoreBackupID != "" {
		return c.GetBackup(restoreBackupID)
	}
	if restoreDatabase == "" {
		return nil, fmt.Errorf("--database is required when --backup-id is not supplied")
	}
	if !restoreLatest && restoreTargetTime == "" {
		return nil, fmt.Errorf("provide --latest or --target-time when selecting a physical backup automatically")
	}
	items := c.ListBackups(restoreDatabase, catalog.TypePhysical, catalog.StatusCompleted, "", 0)
	if len(items) == 0 {
		return nil, fmt.Errorf("no completed physical backups found for %q", restoreDatabase)
	}
	if restoreTargetTime != "" {
		target, err := time.Parse(time.RFC3339, restoreTargetTime)
		if err != nil {
			return nil, fmt.Errorf("--target-time must be ISO 8601/RFC3339: %w", err)
		}
		for _, v := range items {
			if !v.CompletedAt.After(target) {
				return v, nil
			}
		}
		return nil, fmt.Errorf("no physical base backup exists at or before %s", target.Format(time.RFC3339))
	}
	return items[0], nil
}
func runRestorePlan(cmd *cobra.Command, _ []string) error {
	if restoreTarget != "docker" {
		return fmt.Errorf("physical restore requires --target docker")
	}
	if restoreLatest && restoreTargetTime != "" {
		return fmt.Errorf("--latest and --target-time are mutually exclusive")
	}
	m, err := selectRestoreBackup()
	if err != nil {
		return err
	}
	if m.Type != catalog.TypePhysical {
		return fmt.Errorf("backup %q is logical; use restore run --backup-id %s --target-database <name>", m.ID, m.ID)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Restore plan\nBackup: %s\nSource database: %s\nTarget: Docker\nBase backup: %s\n", m.ID, m.Database, displayStorage(m.Storage))
	if restoreTargetTime != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Requested recovery time: %s\n", restoreTargetTime)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Requested recovery time: latest available WAL")
	}
	if m.WALStorage == nil {
		if restoreTargetTime != "" {
			return fmt.Errorf("backup %q has no recorded WAL destination; --target-time is unavailable", m.ID)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "WAL destination: none (base backup state only, no point-in-time recovery)")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "WAL destination: %s\n", displayStorage(*m.WALStorage))
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("Docker is required for --target docker: %w", err)
	}
	return nil
}
func runRestoreRun(cmd *cobra.Command, _ []string) error {
	if restoreBackupID == "" {
		return fmt.Errorf("--backup-id is required for restore run")
	}
	c, err := catalog.NewManager()
	if err != nil {
		return err
	}
	m, err := c.GetBackup(restoreBackupID)
	if err != nil {
		return err
	}
	if m.Status != catalog.StatusCompleted {
		return fmt.Errorf("backup %q is not completed", m.ID)
	}
	if m.Type == catalog.TypeLogical {
		return runLogicalRestore(cmd, m)
	}
	return runPhysicalRestore(cmd, m)
}
func runLogicalRestore(cmd *cobra.Command, m *catalog.Manifest) error {
	if restoreTargetDatabase == "" {
		return fmt.Errorf("logical restore requires --target-database")
	}
	if restoreTargetDatabase == m.Database {
		return fmt.Errorf("refusing to restore a logical backup into its source database without an explicit destructive workflow")
	}
	file, cleanup, err := materializeArtifact(m)
	if err != nil {
		return err
	}
	defer cleanup()
	if err = restore.PerformRestore(restoreTargetDatabase, file, restore.RestoreOptions{DatabaseName: restoreTargetDatabase, FilePath: file}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Logical backup %s restored into %s\n", m.ID, restoreTargetDatabase)
	return nil
}
func runPhysicalRestore(cmd *cobra.Command, m *catalog.Manifest) error {
	if restoreTarget != "docker" {
		return fmt.Errorf("physical restore requires --target docker")
	}
	if restoreLatest && restoreTargetTime != "" {
		return fmt.Errorf("--latest and --target-time are mutually exclusive")
	}
	hasWAL := m.WALStorage != nil
	if !hasWAL && restoreTargetTime != "" {
		return fmt.Errorf("physical backup %q has no WAL destination; point-in-time recovery (--target-time) is unavailable, restore without it to recover the base backup state", m.ID)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("Docker is required: %w", err)
	}
	if restoreTargetTime != "" {
		target, err := time.Parse(time.RFC3339, restoreTargetTime)
		if err != nil {
			return fmt.Errorf("--target-time must be ISO 8601/RFC3339: %w", err)
		}
		if target.Before(m.CompletedAt) {
			return fmt.Errorf("requested recovery time predates the selected base backup (%s)", m.CompletedAt.Format(time.RFC3339))
		}
	}
	archive, cleanup, err := materializeArtifact(m)
	if err != nil {
		return err
	}
	defer cleanup()
	recoveryRoot, err := os.MkdirTemp("", "acrynite-recovery-"+m.ID+"-")
	if err != nil {
		return err
	}
	dataDir := filepath.Join(recoveryRoot, "data")
	if err := extractBaseBackup(archive, dataDir); err != nil {
		_ = os.RemoveAll(recoveryRoot)
		return err
	}
	walDir := filepath.Join(recoveryRoot, "wal_archive")
	if hasWAL {
		walLoc, err := locationFromLocator(*m.WALStorage)
		if err != nil {
			_ = os.RemoveAll(recoveryRoot)
			return err
		}
		walLoc.Prefix = strings.Trim(walLoc.Prefix, "/")
		if walLoc.Prefix != "" {
			walLoc.Prefix += "/"
		}
		walLoc.Prefix += "wal"
		segments, err := walLoc.DownloadPrefix(walDir)
		if err != nil {
			_ = os.RemoveAll(recoveryRoot)
			return fmt.Errorf("download WAL archive: %w", err)
		}
		if len(segments) == 0 {
			_ = os.RemoveAll(recoveryRoot)
			return fmt.Errorf("no archived WAL segments were found; refusing an unsafe physical restore")
		}
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "No WAL destination recorded: restoring the base backup state only (no point-in-time recovery).")
		if err := os.MkdirAll(walDir, 0o755); err != nil {
			_ = os.RemoveAll(recoveryRoot)
			return err
		}
	}
	if err := configureRecovery(dataDir, restoreTargetTime, hasWAL); err != nil {
		_ = os.RemoveAll(recoveryRoot)
		return err
	}
	container := restoreContainerName
	if container == "" {
		container = "acrynite-" + strings.ReplaceAll(m.ID, "_", "-")
	}
	if err := removeStaleContainer(container); err != nil {
		_ = os.RemoveAll(recoveryRoot)
		return err
	}
	pgData := pgDataPath(m.PostgresVersion)
	args := []string{"run", "-d", "--name", container, "--label", recoveryLabel + "=" + m.ID, "-e", "PGDATA=" + pgData, "-v", dataDir + ":" + pgData, "-v", walDir + ":/wal_archive:ro"}
	if err := prepareRecoveryOwnership(dataDir, &args); err != nil {
		_ = os.RemoveAll(recoveryRoot)
		return err
	}
	if restorePublishPort != "" {
		port, err := strconv.Atoi(restorePublishPort)
		if err != nil || port < 1 || port > 65535 {
			_ = os.RemoveAll(recoveryRoot)
			return fmt.Errorf("--publish-port must be between 1 and 65535")
		}
		args = append(args, "-p", "127.0.0.1:"+restorePublishPort+":5432")
	}
	args = append(args, postgresImage(m.PostgresVersion))
	output, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start recovery container: %w: %s\nRecovery data kept at: %s", err, strings.TrimSpace(string(output)), recoveryRoot)
	}
	if err := waitForContainer(container); err != nil {
		return fmt.Errorf("%w\nRecovery data kept at: %s", err, recoveryRoot)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Started isolated recovery container %q from %s. Recovery data: %s\n", container, m.ID, recoveryRoot)
	return nil
}

// prepareRecoveryOwnership makes the restored cluster usable by the postgres user inside the container.
func prepareRecoveryOwnership(dataDir string, args *[]string) error {
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	uid, gid := os.Getuid(), os.Getgid()
	if uid == 0 {
		uid, gid = 999, 999
		err := filepath.Walk(dataDir, func(p string, _ os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			return os.Lchown(p, uid, gid)
		})
		if err != nil {
			return fmt.Errorf("set recovery data ownership: %w", err)
		}
	}
	// Insert --user right after "run" so it precedes the image name.
	a := *args
	*args = append([]string{a[0], "--user", fmt.Sprintf("%d:%d", uid, gid)}, a[1:]...)
	return nil
}

// waitForContainer fails if the recovery container exits shortly after start.
func waitForContainer(container string) error {
	for i := 0; i < 5; i++ {
		time.Sleep(time.Second)
		out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", container).Output()
		if err != nil || strings.TrimSpace(string(out)) != "true" {
			logs, _ := exec.Command("docker", "logs", "--tail", "20", container).CombinedOutput()
			return fmt.Errorf("recovery container %q exited during startup:\n%s", container, strings.TrimSpace(string(logs)))
		}
	}
	return nil
}

func extractBaseBackup(archivePath, dataDir string) error {
	in, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer in.Close()
	outer := tar.NewReader(in)
	found := false
	for {
		header, err := outer.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read physical backup archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || (filepath.Base(header.Name) != "base.tar.gz" && filepath.Base(header.Name) != "pg_wal.tar.gz") {
			continue
		}
		// pg_wal.tar holds bare segment files, so it belongs under pg_wal/.
		dest := dataDir
		if filepath.Base(header.Name) == "pg_wal.tar.gz" {
			dest = filepath.Join(dataDir, "pg_wal")
		}
		if err := extractGzipTar(outer, dest); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return fmt.Errorf("physical backup does not contain pg_basebackup tar files")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "PG_VERSION")); err != nil {
		return fmt.Errorf("extracted backup is not a PostgreSQL data directory (PG_VERSION missing): %w", err)
	}
	return nil
}

func extractGzipTar(source io.Reader, destination string) error {
	zr, err := gzip.NewReader(source)
	if err != nil {
		return fmt.Errorf("open PostgreSQL base archive: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(h.Name)
		if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
			return fmt.Errorf("unsafe path in physical backup: %q", h.Name)
		}
		path := filepath.Join(destination, clean)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(h.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
}

func configureRecovery(dataDir, targetTime string, hasWAL bool) error {
	if err := ensureServerConfigFiles(dataDir); err != nil {
		return err
	}
	config := "\nrecovery_target_action = 'promote'\n"
	if !hasWAL {
		// Base backup only: stop at the first consistent point using the WAL bundled in the backup.
		config += "recovery_target = 'immediate'\n"
	} else {
		config += "restore_command = 'cp /wal_archive/%f %p'\n"
	}
	if targetTime != "" {
		config += "recovery_target_time = '" + strings.ReplaceAll(targetTime, "'", "''") + "'\n"
	}
	if err := os.WriteFile(filepath.Join(dataDir, "recovery.signal"), []byte{}, 0o600); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "postgresql.auto.conf"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(config)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

const recoveryLabel = "acrynite.recovery-backup"

// removeStaleContainer removes a previous Acrynite recovery container so the attempt can be repeated.
func removeStaleContainer(name string) error {
	out, err := exec.Command("docker", "inspect", "-f", `{{index .Config.Labels "`+recoveryLabel+`"}}`, name).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "No such") {
			return nil
		}
		return fmt.Errorf("inspect container %q: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("container %q exists and was not created by Acrynite; remove it or use --container-name", name)
	}
	if out, err := exec.Command("docker", "rm", "-f", name).CombinedOutput(); err != nil {
		return fmt.Errorf("remove previous recovery container %q: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func postgresMajor(version string) int {
	for _, field := range strings.Fields(version) {
		if n, err := strconv.Atoi(strings.Split(field, ".")[0]); err == nil {
			return n
		}
	}
	return 0
}

// pgDataPath follows the official image layout: PostgreSQL 18+ keeps data under /var/lib/postgresql/<major>/docker.
func pgDataPath(version string) string {
	if major := postgresMajor(version); major >= 18 {
		return fmt.Sprintf("/var/lib/postgresql/%d/docker", major)
	}
	return "/var/lib/postgresql/data"
}

// ensureServerConfigFiles supplies configs that live outside PGDATA on Debian/Ubuntu and are absent from the backup.
func ensureServerConfigFiles(dataDir string) error {
	files := []struct{ name, content string }{
		{"postgresql.conf", "listen_addresses = '*'\nport = 5432\nmax_connections = 100\n"},
		{"pg_hba.conf", "local all all trust\nhost all all 0.0.0.0/0 scram-sha-256\n"},
		{"pg_ident.conf", ""},
	}
	for _, f := range files {
		path := filepath.Join(dataDir, f.name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(path, []byte(f.content), 0o600); err != nil {
			return fmt.Errorf("write default %s: %w", f.name, err)
		}
	}
	return nil
}

func postgresImage(version string) string {
	for _, field := range strings.Fields(version) {
		if parts := strings.Split(field, "."); len(parts) > 0 {
			if _, err := strconv.Atoi(parts[0]); err == nil {
				return "postgres:" + parts[0]
			}
		}
	}
	return "postgres:latest"
}

func init() {
	databaseCmd.AddCommand(databaseRegisterCmd, databaseListCmd, databaseShowCmd, databaseTestCmd)
	databaseRegisterCmd.Flags().StringVar(&flowHost, "host", "", "PostgreSQL host")
	databaseRegisterCmd.Flags().IntVar(&flowPort, "port", 5432, "PostgreSQL port")
	databaseRegisterCmd.Flags().StringVar(&flowUser, "username", "", "PostgreSQL username")
	databaseRegisterCmd.Flags().StringVar(&flowDatabase, "database", "", "PostgreSQL database")
	databaseRegisterCmd.Flags().StringVar(&flowPassword, "password", "", "Password (prefer PGPASSWORD or a secret store)")
	databaseRegisterCmd.Flags().StringVar(&flowSSLMode, "ssl-mode", "disable", "PostgreSQL SSL mode")
	databaseRegisterCmd.Flags().StringVar(&flowDumpPath, "dump-path", "", "Path to pg_dump")
	databaseListCmd.Flags().StringVar(&flowFormat, "format", "table", "table or json")
	databaseShowCmd.Flags().StringVar(&flowFormat, "format", "table", "table or json")
	storageCmd.AddCommand(storageRegisterCmd, storageListCmd)
	storageRegisterCmd.Flags().StringVar(&storageProvider, "provider", "", "Storage provider (s3)")
	storageRegisterCmd.Flags().StringVar(&storageBucket, "bucket", "", "S3 bucket")
	storageRegisterCmd.Flags().StringVar(&storagePrefix, "prefix", "", "S3 prefix")
	storageRegisterCmd.Flags().StringVar(&storageRegion, "region", "us-east-1", "S3 region")
	storageRegisterCmd.Flags().StringVar(&storageEndpoint, "endpoint", "", "S3-compatible endpoint")
	storageRegisterCmd.Flags().StringVar(&storageAccessKey, "access-key", "", "S3 access key")
	storageRegisterCmd.Flags().StringVar(&storageSecretKey, "secret-key", "", "S3 secret key")
	storageListCmd.Flags().StringVar(&flowFormat, "format", "table", "table or json")
	backupCmd.AddCommand(backupRunV2Cmd, backupListV2Cmd, backupShowV2Cmd)
	backupRunV2Cmd.Flags().StringVar(&backupDatabase, "database", "", "Registered source database")
	backupRunV2Cmd.Flags().StringVar(&backupType, "type", "", "physical or logical")
	backupRunV2Cmd.Flags().StringVar(&backupStorage, "storage", "", "Registered S3 storage")
	backupRunV2Cmd.Flags().StringVar(&backupPathV2, "path", "", "Direct local destination")
	backupRunV2Cmd.Flags().StringVar(&backupLabel, "name", "", "Human-readable run label")
	backupListV2Cmd.Flags().StringVar(&backupDatabase, "database", "", "Registered source database")
	backupListV2Cmd.Flags().StringVar(&backupListType, "type", "", "physical or logical")
	backupListV2Cmd.Flags().StringVar(&backupListStorage, "storage", "", "Registered S3 storage")
	backupListV2Cmd.Flags().StringVar(&backupListStatus, "status", "", "completed or failed")
	backupListV2Cmd.Flags().IntVar(&backupLimit, "limit", 0, "Maximum results")
	backupListV2Cmd.Flags().StringVar(&flowFormat, "format", "table", "table or json")
	scheduleCmd.AddCommand(scheduleCreateCmd, scheduleListCmd, scheduleDaemonCmd)
	scheduleCreateCmd.Flags().StringVar(&scheduleDatabase, "database", "", "Registered source database")
	scheduleCreateCmd.Flags().StringVar(&scheduleType, "type", "", "physical or logical")
	scheduleCreateCmd.Flags().StringVar(&scheduleInterval, "interval", "", "Interval (for example 24h)")
	scheduleCreateCmd.Flags().StringVar(&scheduleStorage, "storage", "", "Registered S3 storage")
	scheduleCreateCmd.Flags().StringVar(&schedulePath, "path", "", "Direct local destination")
	scheduleListCmd.Flags().StringVar(&scheduleListDatabase, "database", "", "Filter database")
	scheduleListCmd.Flags().StringVar(&scheduleListStatus, "status", "", "enabled or paused")
	scheduleListCmd.Flags().StringVar(&flowFormat, "format", "table", "table or json")
	walCmd.AddCommand(walEnableCmd, walStatusCmd, walPushCmd)
	walEnableCmd.Flags().StringVar(&walStorage, "storage", "", "Registered S3 storage")
	walEnableCmd.Flags().StringVar(&walPath, "path", "", "Local directory for WAL files (must be writable by PostgreSQL)")
	walPushCmd.Flags().StringVar(&walPushFile, "file", "", "WAL source file")
	walPushCmd.Flags().StringVar(&walPushName, "name", "", "WAL segment name")
	recoveryPointsCmd.AddCommand(recoveryPointsListCmd)
	recoveryPointsListCmd.Flags().StringVar(&recoveryDatabase, "database", "", "Registered source database")
	restoreCmd.AddCommand(restorePlanCmd, restoreRunCmd)
	for _, v := range []*cobra.Command{restorePlanCmd, restoreRunCmd} {
		v.Flags().StringVar(&restoreBackupID, "backup-id", "", "Backup ID")
		v.Flags().StringVar(&restoreDatabase, "database", "", "Source database")
		v.Flags().StringVar(&restoreTarget, "target", "", "Physical target (docker)")
		v.Flags().StringVar(&restoreTargetTime, "target-time", "", "RFC3339 recovery timestamp")
		v.Flags().BoolVar(&restoreLatest, "latest", false, "Recover latest verified state")
	}
	restoreRunCmd.Flags().StringVar(&restoreTargetDatabase, "target-database", "", "Registered logical-restore target database")
	restoreRunCmd.Flags().StringVar(&restoreContainerName, "container-name", "", "Recovery container name")
	restoreRunCmd.Flags().StringVar(&restorePublishPort, "publish-port", "", "Local PostgreSQL port")
}

func storageLocation(name string) (*location.Config, catalog.StorageLocator, error) {
	m, err := provider.GetConfigManager()
	if err != nil {
		return nil, catalog.StorageLocator{}, err
	}
	p, err := m.Get(name)
	if err != nil {
		return nil, catalog.StorageLocator{}, err
	}
	if p.Type != provider.TypeS3 {
		return nil, catalog.StorageLocator{}, fmt.Errorf("storage %q is not an S3 profile", name)
	}
	prefix := strings.Trim(p.Prefix, "/")
	loc := &location.Config{Name: name, Type: location.TypeS3, Provider: name, Prefix: prefix}
	return loc, catalog.StorageLocator{Profile: name, Provider: "s3", Bucket: p.Bucket, Prefix: prefix}, nil
}
func locationFromLocator(v catalog.StorageLocator) (*location.Config, error) {
	if v.Provider == "local" && v.Artifact != "" {
		return &location.Config{Type: location.TypeLocal, Path: v.Artifact}, nil
	}
	if v.Provider != "s3" || v.Profile == "" {
		return nil, fmt.Errorf("unsupported storage locator %q", v.Provider)
	}
	loc, _, err := storageLocation(v.Profile)
	return loc, err
}
func writeManifest(m *catalog.Manifest, loc *location.Config) error {
	if loc == nil {
		m.Storage.Manifest = m.Storage.Artifact + ".manifest.json"
	} else {
		m.Storage.Manifest = strings.Trim(loc.Prefix, "/")
		if m.Storage.Manifest != "" {
			m.Storage.Manifest += "/"
		}
		m.Storage.Manifest += m.ID + ".manifest.json"
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if loc == nil {
		return os.WriteFile(m.Storage.Manifest, data, 0600)
	}
	dir, err := os.MkdirTemp("", "acrynite-manifest-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, m.ID+".manifest.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	_, err = loc.UploadBackup(path, m.Database)
	return err
}
func materializeArtifact(m *catalog.Manifest) (string, func(), error) {
	if m.Storage.Provider == "local" {
		return m.Storage.Artifact, func() {}, nil
	}
	loc, err := locationFromLocator(m.Storage)
	if err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp("", "acrynite-restore-")
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, filepath.Base(m.Storage.Artifact))
	out, err := loc.DownloadBackup(location.BackupRef{Identifier: m.Storage.Artifact}, path)
	if err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	return out, func() { _ = os.RemoveAll(dir) }, nil
}
func backupID() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		b = []byte{0, 0}
	}
	return "bkp_" + time.Now().UTC().Format("20060102_150405") + "_" + hex.EncodeToString(b)
}
func displayStorage(v catalog.StorageLocator) string {
	if v.Provider == "local" {
		return v.Artifact
	}
	return fmt.Sprintf("s3://%s/%s", v.Bucket, strings.Trim(v.Prefix, "/"))
}
func databaseStatus(ok bool) string {
	if ok {
		return "active"
	}
	return "not tested"
}
func buildDatabaseArchiveCommand(executable, workingDir, dataDirectory, databaseName string) string {
	source := filepath.Join(dataDirectory, "%p")
	if strings.HasSuffix(strings.ToLower(executable), ".exe") {
		q := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }
		return fmt.Sprintf("cd /d %s && %s wal push %s --file %s --name %s", q(workingDir), q(executable), q(databaseName), q(source), q("%f"))
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }
	return fmt.Sprintf("cd %s && %s wal push %s --file %s --name %s", q(workingDir), q(executable), q(databaseName), q(source), q("%f"))
}
