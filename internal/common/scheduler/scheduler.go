package scheduler

import (
	"log"
	"sync"
	"time"
)

// Task is a named scheduled job (replaces XXL-JOB handler).
type Task struct {
	Name     string
	Interval time.Duration // how often to run
	Handler  func()        // the job function
}

// Scheduler manages periodic tasks (lightweight replacement for XXL-JOB).
type Scheduler struct {
	mu      sync.Mutex
	tasks   []*Task
	running bool
	stopCh  chan struct{}
}

// New creates a new Scheduler.
func New() *Scheduler {
	return &Scheduler{
		stopCh: make(chan struct{}),
	}
}

// Register adds a task to the scheduler.
func (s *Scheduler) Register(task *Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, task)
	log.Printf("[Scheduler] registered task: %s (interval: %s)", task.Name, task.Interval)
}

// Start begins executing all registered tasks in their own goroutines.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}
	s.running = true

	for _, task := range s.tasks {
		go s.runTask(task)
	}
	log.Printf("[Scheduler] started %d tasks", len(s.tasks))
}

// Stop halts all tasks gracefully.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}
	s.running = false
	close(s.stopCh)
	log.Println("[Scheduler] stopped")
}

func (s *Scheduler) runTask(task *Task) {
	ticker := time.NewTicker(task.Interval)
	defer ticker.Stop()

	log.Printf("[Scheduler] task %s started", task.Name)

	for {
		select {
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[Scheduler] task %s panicked: %v", task.Name, r)
					}
				}()
				start := time.Now()
				task.Handler()
				elapsed := time.Since(start)
				log.Printf("[Scheduler] task %s completed in %s", task.Name, elapsed)
			}()
		case <-s.stopCh:
			log.Printf("[Scheduler] task %s stopped", task.Name)
			return
		}
	}
}
