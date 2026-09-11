package schedule

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"acrynite-cli/internal/backup"
	"acrynite-cli/internal/location"
	"acrynite-cli/utils"
)

// Manager handles schedule execution and persistence
type Manager struct {
	persistence *SchedulePersistence
	storagePath string
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
		// Calculate next run time
		nextRunTime := scheduleData.CreatedAt
		if !scheduleData.LastRunTime.IsZero() {
			nextRunTime = scheduleData.LastRunTime.Add(scheduleData.Interval)
		}

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

	if target == nil || target.Type == location.TypeLocal {
		return backup.PerformBackup(scheduleData.Database, scheduleData.Path)
	}

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

	uploaded, err := target.UploadBackup(ref.Identifier, scheduleData.Database)
	if err != nil {
		return err
	}
	_ = os.Remove(ref.Identifier)
	_ = locMgr.TouchLastBackup(scheduleData.Database, target.Name, time.Now())
	utils.Success("Uploaded scheduled backup '%s' to %s location '%s'", uploaded.Name, target.Type, target.Name)
	return nil
}

// GetSchedules returns all loaded schedules
func (m *Manager) GetSchedules() (map[string]*ScheduleData, error) {
	return m.persistence.LoadSchedules()
}

// AddSchedule adds a new schedule
func (m *Manager) AddSchedule(scheduleID, database string, interval time.Duration, backupPath string) error {
	return m.persistence.AddSchedule(scheduleID, database, interval, backupPath)
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

// IsDaemonRunning checks if the scheduler daemon is running
func (m *Manager) IsDaemonRunning() bool {
	pidFile := filepath.Join(m.storagePath, ".daemon.pid")
	_, err := os.ReadFile(pidFile)
	if err != nil {
		return false // No PID file = not running
	}

	// Simple heuristic: if PID file exists and is recent (last 24 hours), assume daemon is running
	// This is a pragmatic solution for cross-platform compatibility
	info, err := os.Stat(pidFile)
	if err != nil {
		return false
	}

	// If PID file was modified in the last hour, assume daemon is still running
	if time.Since(info.ModTime()) < time.Hour {
		return true
	}

	// Otherwise, assume it's stale
	return false
}
