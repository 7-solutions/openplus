// Package orchestrate implements subagents, the Go-native Workflow engine, and
// goal-driven stopping (ADR-0006, ADR-0002).
//
// Subagents fan out in parallel, each optionally in its own isolated working
// directory (a git worktree in production, a fake in tests) behind the Isolator
// port. Results merge back in input order so a fan-out is reproducible
// regardless of completion timing.
package orchestrate

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// Isolator provides an isolated working directory for one subagent. The
// returned release func tears the isolation down and is always called, even
// when the task fails.
type Isolator interface {
	Isolate(ctx context.Context, id string) (dir string, release func() error, err error)
}

// Task is one unit of subagent work. Run receives the isolated directory (empty
// when no Isolator is configured, meaning "work in place").
type Task struct {
	ID  string
	Run func(ctx context.Context, dir string) (string, error)
}

// Result is one task's outcome. Err is the task's own failure (or an isolation
// failure) and never aborts sibling tasks.
type Result struct {
	ID     string
	Output string
	Err    error
}

// Runner fans tasks out across goroutines, bounded by MaxParallel.
type Runner struct {
	// Isolator isolates each task's working directory. Nil runs in place.
	Isolator Isolator
	// MaxParallel bounds concurrency. Zero defaults to GOMAXPROCS.
	MaxParallel int
}

// RunAll runs every task, at most MaxParallel at a time, and returns results in
// input order. A task's error is captured in its Result rather than aborting the
// fan-out: one failing subagent must not lose the others' work. RunAll itself
// only errors on a programming fault (a task with no Run func).
func (r Runner) RunAll(ctx context.Context, tasks []Task) ([]Result, error) {
	if len(tasks) == 0 {
		return nil, nil
	}
	for _, t := range tasks {
		if t.Run == nil {
			return nil, fmt.Errorf("orchestrate: task %q has no Run func", t.ID)
		}
	}

	limit := r.MaxParallel
	if limit <= 0 {
		limit = runtime.GOMAXPROCS(0)
	}
	if limit > len(tasks) {
		limit = len(tasks)
	}

	results := make([]Result, len(tasks))
	gate := make(chan struct{}, limit)
	var wg sync.WaitGroup

	for i, task := range tasks {
		i, task := i, task
		wg.Go(func() {
			// Acquire a slot, honoring cancellation while queued.
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
			case <-ctx.Done():
				results[i] = Result{ID: task.ID, Err: ctx.Err()}
				return
			}

			results[i] = r.runOne(ctx, task)
		})
	}
	wg.Wait()

	return results, nil
}

// runOne isolates, runs, and releases a single task.
func (r Runner) runOne(ctx context.Context, task Task) Result {
	dir := ""
	if r.Isolator != nil {
		d, release, err := r.Isolator.Isolate(ctx, task.ID)
		if err != nil {
			return Result{ID: task.ID, Err: fmt.Errorf("orchestrate: isolate %q: %w", task.ID, err)}
		}
		if release != nil {
			defer release() //nolint:errcheck // teardown failure must not mask the task result
		}
		dir = d
	}

	out, err := task.Run(ctx, dir)
	return Result{ID: task.ID, Output: out, Err: err}
}
