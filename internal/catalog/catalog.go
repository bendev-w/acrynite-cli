// Package catalog persists the durable records that make a backup usable after a
// schedule has changed or been deleted.  It deliberately contains no secrets.
package catalog

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"acrynite-cli/internal/config"
)

const (
	TypeLogical     = "logical"
	TypePhysical    = "physical"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

type StorageLocator struct {
	Profile  string `json:"profile,omitempty"`
	Provider string `json:"provider"`
	Bucket   string `json:"bucket,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Artifact string `json:"artifact"`
	Manifest string `json:"manifest,omitempty"`
}

// Manifest is the source of truth for one backup run.  It is also written next
// to the artifact; the local catalog is an index, not the only copy.
type Manifest struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	Status            string          `json:"status"`
	Database          string          `json:"database"`
	Label             string          `json:"label,omitempty"`
	Schedule          string          `json:"schedule,omitempty"`
	StartedAt         time.Time       `json:"started_at"`
	CompletedAt       time.Time       `json:"completed_at,omitempty"`
	SizeBytes         int64           `json:"size_bytes,omitempty"`
	PostgresVersion   string          `json:"postgres_version,omitempty"`
	ClusterIdentifier string          `json:"cluster_identifier,omitempty"`
	Timeline          string          `json:"timeline,omitempty"`
	Storage           StorageLocator  `json:"storage"`
	WALStorage        *StorageLocator `json:"wal_storage,omitempty"`
	Error             string          `json:"error,omitempty"`
}

type Schedule struct {
	Name      string          `json:"name"`
	Database  string          `json:"database"`
	Type      string          `json:"type"`
	Interval  time.Duration   `json:"interval"`
	Storage   *StorageLocator `json:"storage,omitempty"`
	Path      string          `json:"path,omitempty"`
	Enabled   bool            `json:"enabled"`
	CreatedAt time.Time       `json:"created_at"`
	LastRunAt time.Time       `json:"last_run_at,omitempty"`
	NextRunAt time.Time       `json:"next_run_at"`
	LastError string          `json:"last_error,omitempty"`
}

type WALConfig struct {
	Database  string         `json:"database"`
	Storage   StorageLocator `json:"storage"`
	EnabledAt time.Time      `json:"enabled_at"`
}

type store struct {
	Version   string                `json:"version"`
	Backups   map[string]*Manifest  `json:"backups"`
	Schedules map[string]*Schedule  `json:"schedules"`
	WAL       map[string]*WALConfig `json:"wal"`
}

type Manager struct {
	config *config.Manager
	data   store
	mu     sync.RWMutex
}

func NewManager() (*Manager, error) {
	c, err := config.NewManagerWithPath("./storage/configs", "catalog.json")
	if err != nil {
		return nil, err
	}
	m := &Manager{config: c, data: store{Version: "2.0.0", Backups: map[string]*Manifest{}, Schedules: map[string]*Schedule{}, WAL: map[string]*WALConfig{}}}
	if c.Exists() {
		if err := c.Load(&m.data); err != nil {
			return nil, fmt.Errorf("load backup catalog: %w", err)
		}
	}
	m.ensureMaps()
	return m, nil
}

func (m *Manager) ensureMaps() {
	if m.data.Backups == nil {
		m.data.Backups = map[string]*Manifest{}
	}
	if m.data.Schedules == nil {
		m.data.Schedules = map[string]*Schedule{}
	}
	if m.data.WAL == nil {
		m.data.WAL = map[string]*WALConfig{}
	}
}

func (m *Manager) saveLocked() error { return m.config.Save(&m.data) }

func (m *Manager) PutBackup(manifest *Manifest) error {
	if manifest == nil || manifest.ID == "" {
		return fmt.Errorf("backup manifest ID is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data.Backups[manifest.ID] = manifest
	return m.saveLocked()
}

func (m *Manager) GetBackup(id string) (*Manifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data.Backups[id]
	if !ok {
		return nil, fmt.Errorf("backup '%s' not found", id)
	}
	copy := *v
	return &copy, nil
}

func (m *Manager) ListBackups(database, backupType, status, storage string, limit int) []*Manifest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Manifest, 0)
	for _, v := range m.data.Backups {
		if (database != "" && v.Database != database) || (backupType != "" && v.Type != backupType) || (status != "" && v.Status != status) || (storage != "" && v.Storage.Profile != storage) {
			continue
		}
		copy := *v
		result = append(result, &copy)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartedAt.After(result[j].StartedAt) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}

func (m *Manager) PutSchedule(s *Schedule) error {
	if s == nil || s.Name == "" {
		return fmt.Errorf("schedule name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.data.Schedules[s.Name]; exists {
		return fmt.Errorf("schedule '%s' already exists", s.Name)
	}
	m.data.Schedules[s.Name] = s
	return m.saveLocked()
}

func (m *Manager) GetSchedule(name string) (*Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data.Schedules[name]
	if !ok {
		return nil, fmt.Errorf("schedule '%s' not found", name)
	}
	copy := *v
	return &copy, nil
}

func (m *Manager) UpdateSchedule(s *Schedule) error {
	if s == nil || s.Name == "" {
		return fmt.Errorf("schedule name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data.Schedules[s.Name]; !ok {
		return fmt.Errorf("schedule '%s' not found", s.Name)
	}
	m.data.Schedules[s.Name] = s
	return m.saveLocked()
}

func (m *Manager) ListSchedules(database, status string) []*Schedule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Schedule, 0)
	for _, v := range m.data.Schedules {
		if database != "" && v.Database != database {
			continue
		}
		if status != "" && ((status == "enabled") != v.Enabled) {
			continue
		}
		copy := *v
		result = append(result, &copy)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (m *Manager) GetWAL(database string) (*WALConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data.WAL[database]
	if !ok {
		return nil, os.ErrNotExist
	}
	copy := *v
	return &copy, nil
}

func (m *Manager) PutWAL(v *WALConfig) error {
	if v == nil || v.Database == "" {
		return fmt.Errorf("WAL database is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data.WAL[v.Database] = v
	return m.saveLocked()
}
