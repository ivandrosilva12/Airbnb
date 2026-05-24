// Package scheduler provides a tiny in-process job runner: registered jobs run
// on a fixed interval in their own goroutine and stop when the context is
// cancelled. It avoids an external scheduler/dependency for simple periodic
// maintenance tasks (e.g. pruning the webhook dedupe table).
package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// Job is a periodic task.
type Job struct {
	Name     string
	Interval time.Duration
	// RunAtStart runs the job once shortly after Start, before the first tick.
	RunAtStart bool
	Run        func(ctx context.Context) error
}

// Scheduler runs jobs on their intervals until the start context is cancelled.
type Scheduler struct {
	jobs []Job
}

// New returns an empty Scheduler.
func New() *Scheduler { return &Scheduler{} }

// Add registers a job. Jobs with a non-positive interval are ignored.
func (s *Scheduler) Add(j Job) {
	if j.Interval <= 0 || j.Run == nil {
		slog.Warn("scheduler: ignoring job with no interval or runner", "job", j.Name)
		return
	}
	s.jobs = append(s.jobs, j)
}

// Start launches each job in its own goroutine; they exit when ctx is done.
func (s *Scheduler) Start(ctx context.Context) {
	for _, j := range s.jobs {
		go s.run(ctx, j)
	}
}

func (s *Scheduler) run(ctx context.Context, j Job) {
	if j.RunAtStart {
		s.invoke(ctx, j)
	}
	ticker := time.NewTicker(j.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.invoke(ctx, j)
		}
	}
}

func (s *Scheduler) invoke(ctx context.Context, j Job) {
	// Recover so a panic in one job is logged and the job keeps ticking, rather
	// than taking down the goroutine (and, on an unrecovered panic, the process).
	defer func() {
		if r := recover(); r != nil {
			slog.Error("scheduler: job panicked", "job", j.Name, "panic", r)
		}
	}()
	if err := j.Run(ctx); err != nil {
		slog.Error("scheduler: job failed", "job", j.Name, "error", err)
	}
}
