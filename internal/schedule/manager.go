package schedule

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"acrynite-cli/internal/backup"
	"acrynite-cli/internal/database"
	"acrynite-cli/internal/location"
	"acrynite-cli/internal/wal"
	"acrynite-cli/utils"
)

// Manager handles schedule execution and persistence
type Manager struct {
	persistence *SchedulePersistence
	storagePath string
	walMu       sync.Mutex
	receivers   map[string]*wal.Receiver
}

// NewManager creates a new schedule manager
func NewManager(storagePath string) *Manager {
	return &Manager{
		persistence: NewSchedulePersistence(storagePath),
		storagePath: storagePath,
	}
}

// ExecuteOverdueSchedules checks and executes any overdue schedules
func (m *Manager) ExecuteOverdueSchedules() error {
	schedules, err := m.persistence.LoadSchedules()
	if err != nil {
		return fmt.Errorf("failed to load schedules: %w", err)
	}

	now := time.Now()
	for scheduleID, scheduleData := range schedules {
		utils.Info("Inspecting schedule '%s' for database '%s'", scheduleID, scheduleData.Database)
		// Calculate next run time
		nextRunTime := scheduleData.CreatedAt
		if !scheduleData.LastRunTime.IsZero() {
			nextRunTime = scheduleData.LastRunTime.Add(scheduleData.Interval)
		}
		utils.Info("Next run time for '%s': %s", scheduleID, nextRunTime.Format(time.RFC3339))

		// Check if schedule is overdue
		if now.After(nextRunTime) {
			utils.Info("Executing overdue backup: %s (Schedule ID: %s)", scheduleData.Database, scheduleID)
			err := m.executeScheduledBackup(scheduleData)

			// Update schedule with run results
			runCount := scheduleData.RunCount + 1
			errorCount := scheduleData.ErrorCount
			if err != nil {
				utils.Error("Backup failed: %v", err)
				errorCount++
			} else {
				utils.Success("Backup completed for %s", scheduleData.Database)
			}

			// Update persistence
			if updateErr := m.persistence.UpdateSchedule(scheduleID, now, runCount, errorCount); updateErr != nil {
				utils.Warning("Failed to update schedule %s: %v", scheduleID, updateErr)
			}
		}
	}

	return nil
}

// SetWALSpool records the WAL archiving mode and folders used by a schedule
func (m *Manager) SetWALSpool(scheduleID, mode, slot, hostDir, containerDir string) error {
	return m.persistence.SetWALSpool(scheduleID, mode, slot, hostDir, containerDir)
}

// ServiceWAL keeps WAL flowing for every schedule that archives through this process: it starts
// and restarts pg_receivewal in stream mode, then uploads finished segments from the spool/stream
// folder. It must only be called from the scheduler daemon, independently of base backups.
func (m *Manager) ServiceWAL() {
	m.walMu.Lock()
	defer m.walMu.Unlock()

	schedules, err := m.persistence.LoadSchedules()
	if err != nil {
		utils.Error("[WAL] Cannot load schedules: %v", err)
		return
	}
	if m.receivers == nil {
		m.receivers = map[string]*wal.Receiver{}
	}
	for id, r := range m.receivers {
		if _, ok := schedules[id]; !ok {
			r.Stop()
			delete(m.receivers, id)
		}
	}
	for _, s := range schedules {
		if s.WALMode == wal.ModeStream {
			m.ensureReceiver(s)
		}
		m.drainWALSpool(s)
	}
}

func (m *Manager) ensureReceiver(s *ScheduleData) {
	if r := m.receivers[s.ID]; r != nil && r.Running() {
		return
	}
	dbMgr, err := database.GetConfigManager()
	if err != nil {
		utils.Error("[WAL] Cannot load databases: %v", err)
		return
	}
	cfg, err := dbMgr.Get(s.Database)
	if err != nil {
		utils.Error("[WAL] Database '%s' not found for schedule '%s': %v", s.Database, s.ID, err)
		return
	}
	exe, err := backup.ResolveReceiveWALExecutable(cfg.DumpPath)
	if err != nil {
		utils.Error("[WAL] pg_receivewal not available for schedule '%s': %v", s.ID, err)
		return
	}
	r, err := wal.StartReceiver(exe, cfg, s.ID, s.WALSlot, s.WALSpoolDir)
	if err != nil {
		utils.Error("[WAL] %v", err)
		return
	}
	m.receivers[s.ID] = r
}

// drainWALSpool uploads WAL segments that a containerised PostgreSQL copied into the shared folder
func (m *Manager) drainWALSpool(scheduleData *ScheduleData) {
	if scheduleData.WALSpoolDir == "" {
		return
	}
	locMgr, err := location.GetConfigManager()
	if err != nil {
		utils.Error("[WAL] Cannot load locations: %v", err)
		return
	}
	for _, loc := range locMgr.ListByDatabase(scheduleData.Database) {
		if loc.ScheduleID == scheduleData.ID && loc.Active {
			wal.UploadSpool(loc, scheduleData.WALSpoolDir)
			return
		}
	}
	utils.Warning("[WAL] No active location linked to schedule '%s'; spool %s not uploaded", scheduleData.ID, scheduleData.WALSpoolDir)
}

func (m *Manager) executeScheduledBackup(scheduleData *ScheduleData) error {
	locMgr, err := location.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get location manager: %w", err)
	}

	var target *location.Config
	for _, loc := range locMgr.ListByDatabase(scheduleData.Database) {
		if loc.ScheduleID == scheduleData.ID {
			target = loc
			break
		}
	}

	if scheduleData.Method == backup.MethodBaseBackup {
		return m.executeBaseBackup(scheduleData, target, locMgr)
	}

	if target == nil || target.Type == location.TypeLocal {
		if target == nil {
			utils.Warning("No scheduled location found for database '%s'; falling back to local backup only", scheduleData.Database)
		} else {
			utils.Info("Schedule '%s' targets local location '%s'", scheduleData.ID, target.Name)
		}
		return backup.PerformBackup(scheduleData.Database, scheduleData.Path)
	}

	utils.Info("Schedule '%s' targets %s location '%s'", scheduleData.ID, target.Type, target.Name)
	utils.Info("Creating staged backup in %s", scheduleData.Path)
	if err := backup.PerformBackup(scheduleData.Database, scheduleData.Path); err != nil {
		return err
	}

	staged := &location.Config{
		Name:     target.Name + "-staging",
		Database: scheduleData.Database,
		Type:     location.TypeLocal,
		Path:     scheduleData.Path,
	}
	ref, err := staged.FindLatestBackup(scheduleData.Database)
	if err != nil {
		return fmt.Errorf("failed to find staged backup: %w", err)
	}
	utils.Info("Found staged backup: %s", ref.Identifier)

	uploaded, err := target.UploadBackup(ref.Identifier, scheduleData.Database)
	if err != nil {
		// Don't leave multi-GB staged dumps behind after a failed upload
		if rmErr := os.Remove(ref.Identifier); rmErr != nil {
			utils.Warning("Failed to delete staged backup '%s' after upload failure: %v", ref.Identifier, rmErr)
		} else {
			utils.Info("Deleted staged backup after upload failure: %s", ref.Identifier)
		}
		return err
	}
	utils.Info("Deleting staged backup: %s", ref.Identifier)
	if err := os.Remove(ref.Identifier); err != nil {
		utils.Warning("Failed to delete staged backup '%s': %v", ref.Identifier, err)
	} else {
		utils.Success("Deleted staged backup: %s", ref.Identifier)
	}
	_ = locMgr.TouchLastBackup(scheduleData.Database, target.Name, time.Now())
	utils.Success("Uploaded scheduled backup '%s' to %s location '%s'", uploaded.Name, target.Type, target.Name)
	return nil
}

// executeBaseBackup runs pg_basebackup for a PITR schedule. WAL segments are archived
// continuously by PostgreSQL itself (see 'backup wal enable'), not by this job.
func (m *Manager) executeBaseBackup(scheduleData *ScheduleData, target *location.Config, locMgr *location.ConfigManager) error {
	dbMgr, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get database manager: %w", err)
	}
	dbConfig, err := dbMgr.Get(scheduleData.Database)
	if err != nil {
		return fmt.Errorf("database '%s' not found: %w", scheduleData.Database, err)
	}
	if target == nil {
		utils.Warning("No scheduled location found for database '%s'; keeping base backup in %s", scheduleData.Database, scheduleData.Path)
	} else {
		utils.Info("Schedule '%s' takes a base backup into %s location '%s'", scheduleData.ID, target.Type, target.Name)
	}

	ref, err := backup.PerformBaseBackupToLocation(dbConfig, scheduleData.Database, scheduleData.Path, target)
	if err != nil {
		return err
	}
	if target != nil {
		_ = locMgr.TouchLastBackup(scheduleData.Database, target.Name, time.Now())
		utils.Success("Base backup '%s' stored in %s location '%s'", ref.Name, target.Type, target.Name)
	}
	return nil
}

// GetSchedules returns all loaded schedules
func (m *Manager) GetSchedules() (map[string]*ScheduleData, error) {
	return m.persistence.LoadSchedules()
}

// AddSchedule adds a new schedule
func (m *Manager) AddSchedule(scheduleID, database string, interval time.Duration, backupPath, method string) error {
	return m.persistence.AddSchedule(scheduleID, database, interval, backupPath, method)
}

// RemoveSchedule removes a schedule
func (m *Manager) RemoveSchedule(scheduleID string) error {
	return m.persistence.RemoveSchedule(scheduleID)
}

// StartDaemon starts the backup scheduler daemon in the background
func (m *Manager) StartDaemon() error {
	// Get path to the executable
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start the daemon in the background
	// Using os/exec with proper detachment
	cmd := exec.Command(exePath, "backup", "scheduler")

	// Logger output already goes to the log file; stderr (e.g. pg_dump errors) would be lost otherwise
	if err := os.MkdirAll(filepath.Dir(utils.LogFilePath), 0755); err == nil {
		if lf, err := os.OpenFile(utils.LogFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
			defer lf.Close()
			cmd.Stderr = lf
		}
	}

	// Detach from current process so it runs independently
	// On Windows, this is handled by cmd.SysProcAttr
	// On Unix, we use cmd.Start() without Wait
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start scheduler daemon: %w", err)
	}

	// Write PID to file for tracking
	pidFile := filepath.Join(m.storagePath, ".daemon.pid")
	if err := os.MkdirAll(m.storagePath, 0755); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		utils.Warning("Failed to write PID file: %v", err)
	}

	return nil
}

// IsDaemonRunning checks whether the process recorded in the pid file is alive
func (m *Manager) IsDaemonRunning() bool {
	pidFile := filepath.Join(m.storagePath, ".daemon.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	return processAlive(pid)
}

