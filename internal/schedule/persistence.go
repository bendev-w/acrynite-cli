package schedule

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ScheduleData represents schedule information that can be persisted
type ScheduleData struct {
	ID          string        `json:"id"`
	Database    string        `json:"database"`
	Interval    time.Duration `json:"interval"`
	Path        string        `json:"path"`
	CreatedAt   time.Time     `json:"created_at"`
	LastRunTime time.Time     `json:"last_run_time"`
	RunCount    int           `json:"run_count"`
	ErrorCount  int           `json:"error_count"`
}

// SchedulePersistence handles saving and loading schedules from disk
type SchedulePersistence struct {
	filePath string
	mu       sync.RWMutex
}

// NewSchedulePersistence creates a new schedule persistence manager
func NewSchedulePersistence(storagePath string) *SchedulePersistence {
	return &SchedulePersistence{
		filePath: filepath.Join(storagePath, "schedules.json"),
	}
}

// LoadSchedules loads all schedules from disk
func (sp *SchedulePersistence) LoadSchedules() (map[string]*ScheduleData, error) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	schedules := make(map[string]*ScheduleData)

	// If file doesn't exist, return empty schedules
	if _, err := os.Stat(sp.filePath); os.IsNotExist(err) {
		return schedules, nil
	}

	// Read file
	data, err := os.ReadFile(sp.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schedules file: %w", err)
	}

	// Parse JSON
	var fileData map[string]*ScheduleData
	if err := json.Unmarshal(data, &fileData); err != nil {
		return nil, fmt.Errorf("failed to parse schedules file: %w", err)
	}

	return fileData, nil
}

// SaveSchedules saves all schedules to disk
func (sp *SchedulePersistence) SaveSchedules(schedules map[string]*ScheduleData) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Ensure directory exists
	dir := filepath.Dir(sp.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create schedules directory: %w", err)
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(schedules, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schedules: %w", err)
	}

	// Write to file
	if err := os.WriteFile(sp.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write schedules file: %w", err)
	}

	return nil
}

// AddSchedule adds a new schedule
func (sp *SchedulePersistence) AddSchedule(scheduleID, database string, interval time.Duration, backupPath string) error {
	schedules, err := sp.LoadSchedules()
	if err != nil {
		return err
	}

	schedules[scheduleID] = &ScheduleData{
		ID:        scheduleID,
		Database:  database,
		Interval:  interval,
		Path:      backupPath,
		CreatedAt: time.Now(),
	}

	return sp.SaveSchedules(schedules)
}

// UpdateSchedule updates an existing schedule
func (sp *SchedulePersistence) UpdateSchedule(scheduleID string, lastRunTime time.Time, runCount, errorCount int) error {
	schedules, err := sp.LoadSchedules()
	if err != nil {
		return err
	}

	schedule, exists := schedules[scheduleID]
	if !exists {
		return fmt.Errorf("schedule '%s' not found", scheduleID)
	}

	schedule.LastRunTime = lastRunTime
	schedule.RunCount = runCount
	schedule.ErrorCount = errorCount

	return sp.SaveSchedules(schedules)
}

// RemoveSchedule removes a schedule
func (sp *SchedulePersistence) RemoveSchedule(scheduleID string) error {
	schedules, err := sp.LoadSchedules()
	if err != nil {
		return err
	}

	if _, exists := schedules[scheduleID]; !exists {
		return fmt.Errorf("schedule '%s' not found", scheduleID)
	}

	delete(schedules, scheduleID)
	return sp.SaveSchedules(schedules)
}

// GetSchedule gets a single schedule
func (sp *SchedulePersistence) GetSchedule(scheduleID string) (*ScheduleData, error) {
	schedules, err := sp.LoadSchedules()
	if err != nil {
		return nil, err
	}

	schedule, exists := schedules[scheduleID]
	if !exists {
		return nil, fmt.Errorf("schedule '%s' not found", scheduleID)
	}

	return schedule, nil
}
