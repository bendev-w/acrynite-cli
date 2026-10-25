package wal

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"acrynite-cli/internal/database"
	"acrynite-cli/utils"
)

// Archive mode "stream" runs pg_receivewal against the server's replication connection. It works
// for any server OS, container or remote host that pg_basebackup can reach: nothing has to be
// installed on, or mounted into, the database server and no restart is needed.
const ModeStream = "stream"

var slotUnsafe = regexp.MustCompile(`[^a-z0-9_]`)

// SlotName returns the physical replication slot name used for a schedule.
func SlotName(scheduleID string) string {
	name := "acrynite_" + slotUnsafe.ReplaceAllString(strings.ToLower(scheduleID), "_")
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// StreamDir is the host folder pg_receivewal writes into for a schedule.
func StreamDir(scheduleID string) (string, error) {
	return filepath.Abs(filepath.Join("storage", "wal-stream", scheduleID))
}

// EnableStreaming prepares the server for pg_receivewal: it creates the replication slot (so no
// WAL is recycled before it has been received) and sets archive_timeout so idle databases still
// switch segments. None of this needs a restart.
func EnableStreaming(db *sql.DB, slot, archiveTimeout string) error {
	var exists bool
	if err := db.QueryRow("SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = $1)", slot).Scan(&exists); err != nil {
		return archiveError("failed to inspect replication slots", err)
	}
	if !exists {
		if _, err := db.Exec("SELECT pg_create_physical_replication_slot($1, true)", slot); err != nil {
			return archiveError("failed to create replication slot '"+slot+"' (needs the REPLICATION attribute and max_replication_slots > 0)", err)
		}
	}
	if archiveTimeout != "" {
		if err := ValidateArchiveTimeout(archiveTimeout); err != nil {
			return err
		}
		if _, err := db.Exec("ALTER SYSTEM SET archive_timeout = " + quoteSQLLiteral(archiveTimeout)); err != nil {
			return archiveError("failed to set archive_timeout", err)
		}
		if _, err := db.Exec("SELECT pg_reload_conf()"); err != nil {
			return archiveError("failed to reload PostgreSQL configuration", err)
		}
	}
	return nil
}

// Receiver supervises one pg_receivewal process.
type Receiver struct {
	ScheduleID string
	cmd        *exec.Cmd
	done       chan struct{}
	mu         sync.Mutex
}

// Running reports whether the process is still alive.
func (r *Receiver) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done == nil {
		return false
	}
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}

// Stop terminates the process.
func (r *Receiver) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
}

// StartReceiver launches pg_receivewal for the schedule. Output is written to the Acrynite log.
func StartReceiver(exe string, cfg *database.DBConfig, scheduleID, slot, dir string) (*Receiver, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create WAL stream directory %s: %w", dir, err)
	}
	args := []string{
		"-h", cfg.Host, "-p", fmt.Sprint(cfg.Port), "-U", cfg.User,
		"-D", dir, "--slot", slot, "-w",
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+cfg.Password, "PGCONNECT_TIMEOUT=30")
	if cfg.SSLMode != "" {
		cmd.Env = append(cmd.Env, "PGSSLMODE="+cfg.SSLMode)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start pg_receivewal (%s): %w", exe, err)
	}

	r := &Receiver{ScheduleID: scheduleID, cmd: cmd, done: make(chan struct{})}
	utils.Info("[WAL] Started pg_receivewal (pid %d) for schedule '%s': slot %s -> %s", cmd.Process.Pid, scheduleID, slot, dir)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			utils.Info("[WAL] pg_receivewal: %s", scanner.Text())
		}
	}()
	go func() {
		err := cmd.Wait()
		if err != nil {
			utils.Error("[WAL] pg_receivewal for schedule '%s' exited: %v (will be restarted)", scheduleID, err)
		}
		close(r.done)
	}()
	return r, nil
}
