// Simple task scheduler for running tasks at intervals
package utils

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TaskStatus represents the status of a scheduled task
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// Task represents a scheduled task
type Task struct {
	ID          string
	Name        string
	Description string
	Fn          func() error
	Schedule    time.Duration
	Repeat      bool
	Status      TaskStatus
	LastRun     time.Time
	NextRun     time.Time
	RunCount    int
	ErrorCount  int
	LastError   error
	CreatedAt   time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
}

// NewTask creates a new task
func NewTask(id, name string, fn func() error) *Task {
	ctx, cancel := context.WithCancel(context.Background())
	return &Task{
		ID:        id,
		Name:      name,
		Fn:        fn,
		Status:    TaskPending,
		CreatedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// SetSchedule sets the task schedule
func (t *Task) SetSchedule(duration time.Duration, repeat bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	t.Schedule = duration
	t.Repeat = repeat
	t.NextRun = time.Now().Add(duration)
}

// Run executes the task
func (t *Task) Run() error {
	t.mu.Lock()
	t.Status = TaskRunning
	t.LastRun = time.Now()
	t.RunCount++
	t.mu.Unlock()

	err := t.Fn()

	t.mu.Lock()
	defer t.mu.Unlock()

	if err != nil {
		t.Status = TaskFailed
		t.LastError = err
		t.ErrorCount++
		return err
	}

	if t.Repeat {
		t.Status = TaskPending
		t.NextRun = time.Now().Add(t.Schedule)
	} else {
		t.Status = TaskCompleted
	}

	return nil
}

// Cancel cancels the task
func (t *Task) Cancel() {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	t.cancel()
	t.Status = TaskCancelled
}

// GetStatus returns the current task status
func (t *Task) GetStatus() TaskStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status
}

// String returns a string representation of the task
func (t *Task) String() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	return fmt.Sprintf("Task{ID:%s, Name:%s, Status:%s, RunCount:%d, ErrorCount:%d}",
		t.ID, t.Name, t.Status, t.RunCount, t.ErrorCount)
}

// Scheduler manages scheduled tasks
type Scheduler struct {
	tasks   map[string]*Task
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
	ticker  *time.Ticker
}

// NewScheduler creates a new scheduler
func NewScheduler(tickerDuration time.Duration) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		tasks:  make(map[string]*Task),
		ctx:    ctx,
		cancel: cancel,
		ticker: time.NewTicker(tickerDuration),
	}
}

// AddTask adds a task to the scheduler
func (s *Scheduler) AddTask(task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[task.ID]; exists {
		return fmt.Errorf("task with ID '%s' already exists", task.ID)
	}

	s.tasks[task.ID] = task
	Info("Task added: %s", task.Name)
	return nil
}

// RemoveTask removes a task from the scheduler
func (s *Scheduler) RemoveTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[id]
	if !exists {
		return fmt.Errorf("task with ID '%s' not found", id)
	}

	task.Cancel()
	delete(s.tasks, id)
	Info("Task removed: %s", task.Name)
	return nil
}

// GetTask retrieves a task by ID
func (s *Scheduler) GetTask(id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[id]
	if !exists {
		return nil, fmt.Errorf("task with ID '%s' not found", id)
	}
	return task, nil
}

// ListTasks returns all tasks
func (s *Scheduler) ListTasks() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// Start starts the scheduler
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	Info("Scheduler started")

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-s.ticker.C:
				s.runDueTasks()
			}
		}
	}()
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	s.cancel()
	s.ticker.Stop()
	s.wg.Wait()

	Info("Scheduler stopped")
}

// runDueTasks runs all tasks that are due
func (s *Scheduler) runDueTasks() {
	s.mu.RLock()
	tasks := make([]*Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}
	s.mu.RUnlock()

	now := time.Now()
	for _, task := range tasks {
		if task.GetStatus() == TaskPending && now.After(task.NextRun) {
			s.wg.Add(1)
			go func(t *Task) {
				defer s.wg.Done()
				if err := t.Run(); err != nil {
					Error("Task failed: %s - %v", t.Name, err)
				} else {
					Success("Task completed: %s", t.Name)
				}
			}(task)
		}
	}
}

// ScheduleTask is a convenience method to create and add a task
func (s *Scheduler) ScheduleTask(id, name string, fn func() error, duration time.Duration, repeat bool) error {
	task := NewTask(id, name, fn)
	task.SetSchedule(duration, repeat)
	return s.AddTask(task)
}

// ScheduleRecurring schedules a recurring task
func (s *Scheduler) ScheduleRecurring(id, name string, fn func() error, interval time.Duration) error {
	return s.ScheduleTask(id, name, fn, interval, true)
}

// ScheduleOnce schedules a one-time task
func (s *Scheduler) ScheduleOnce(id, name string, fn func() error, delay time.Duration) error {
	return s.ScheduleTask(id, name, fn, delay, false)
}

// PrintTaskStatus prints the status of all tasks
func (s *Scheduler) PrintTaskStatus() {
	tasks := s.ListTasks()
	if len(tasks) == 0 {
		Info("No scheduled tasks")
		return
	}

	Info("Scheduled Tasks (%d):", len(tasks))
	for _, task := range tasks {
		task.mu.RLock()
		fmt.Printf("  • %s [%s] - Runs: %d, Errors: %d\n",
			task.Name, task.Status, task.RunCount, task.ErrorCount)
		if !task.NextRun.IsZero() && task.Status == TaskPending {
			fmt.Printf("    Next run: %s (%s)\n",
				FormatTimestamp(task.NextRun, TimestampLog),
				TimeAgo(task.NextRun))
		}
		task.mu.RUnlock()
	}
}

