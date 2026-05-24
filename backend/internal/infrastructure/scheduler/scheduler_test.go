package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_RunsAtStartAndOnInterval(t *testing.T) {
	var runs int32
	fired := make(chan struct{}, 8)

	s := New()
	s.Add(Job{
		Name:       "tick",
		Interval:   15 * time.Millisecond,
		RunAtStart: true,
		Run: func(_ context.Context) error {
			atomic.AddInt32(&runs, 1)
			select {
			case fired <- struct{}{}:
			default:
			}
			return nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	// RunAtStart should fire almost immediately.
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("job did not run at start")
	}
	// And at least once more on the ticker.
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("job did not run on interval")
	}

	cancel()
	// Give the goroutine a moment to observe cancellation, then confirm it stops.
	time.Sleep(20 * time.Millisecond)
	before := atomic.LoadInt32(&runs)
	time.Sleep(60 * time.Millisecond)
	if after := atomic.LoadInt32(&runs); after != before {
		t.Fatalf("job kept running after cancel: %d -> %d", before, after)
	}
}

func TestScheduler_IgnoresInvalidJobs(t *testing.T) {
	s := New()
	s.Add(Job{Name: "no-interval", Run: func(context.Context) error { return nil }})
	s.Add(Job{Name: "no-runner", Interval: time.Second})
	if len(s.jobs) != 0 {
		t.Fatalf("expected invalid jobs to be ignored, got %d", len(s.jobs))
	}
}
