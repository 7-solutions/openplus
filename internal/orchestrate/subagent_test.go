package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingIsolator is a fake Isolator: it hands out fake directories and
// records the lifecycle so tests can assert setup/teardown pairing.
type recordingIsolator struct {
	mu       sync.Mutex
	created  []string
	released []string
	failOn   string // task id whose isolation should fail
}

func (r *recordingIsolator) Isolate(ctx context.Context, id string) (string, func() error, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failOn == id {
		return "", nil, errors.New("isolate failed")
	}
	dir := "/fake/wt/" + id
	r.created = append(r.created, dir)
	release := func() error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.released = append(r.released, dir)
		return nil
	}
	return dir, release, nil
}

func TestRunAllExecutesEveryTask(t *testing.T) {
	iso := &recordingIsolator{}
	r := Runner{Isolator: iso, MaxParallel: 4}

	tasks := []Task{
		{ID: "a", Run: func(ctx context.Context, dir string) (string, error) { return "ra", nil }},
		{ID: "b", Run: func(ctx context.Context, dir string) (string, error) { return "rb", nil }},
		{ID: "c", Run: func(ctx context.Context, dir string) (string, error) { return "rc", nil }},
	}
	got, err := r.RunAll(context.Background(), tasks)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("results = %d, want 3", len(got))
	}
	for i, want := range []string{"ra", "rb", "rc"} {
		if got[i].Output != want {
			t.Errorf("results[%d].Output = %q, want %q (order must match input)", i, got[i].Output, want)
		}
		if got[i].Err != nil {
			t.Errorf("results[%d].Err = %v", i, got[i].Err)
		}
	}
}

// TestRunAllResultsAreDeterministic proves the spec requirement that parallel
// results "merge back deterministically" — output order follows input order,
// not completion order.
func TestRunAllResultsAreDeterministic(t *testing.T) {
	iso := &recordingIsolator{}
	r := Runner{Isolator: iso, MaxParallel: 4}

	// task "slow" finishes last but must still be reported first.
	tasks := []Task{
		{ID: "slow", Run: func(ctx context.Context, dir string) (string, error) {
			time.Sleep(60 * time.Millisecond)
			return "slow-done", nil
		}},
		{ID: "fast", Run: func(ctx context.Context, dir string) (string, error) {
			return "fast-done", nil
		}},
	}
	got, err := r.RunAll(context.Background(), tasks)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if got[0].ID != "slow" || got[1].ID != "fast" {
		t.Fatalf("order = %q,%q, want slow,fast", got[0].ID, got[1].ID)
	}
}

func TestRunAllRunsInParallel(t *testing.T) {
	iso := &recordingIsolator{}
	r := Runner{Isolator: iso, MaxParallel: 4}

	const n = 4
	const delay = 80 * time.Millisecond
	tasks := make([]Task, n)
	for i := 0; i < n; i++ {
		tasks[i] = Task{ID: fmt.Sprintf("t%d", i), Run: func(ctx context.Context, dir string) (string, error) {
			time.Sleep(delay)
			return "ok", nil
		}}
	}
	start := time.Now()
	if _, err := r.RunAll(context.Background(), tasks); err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	elapsed := time.Since(start)
	// serial execution would take n*delay; parallel should be far below that.
	if elapsed >= time.Duration(n)*delay {
		t.Fatalf("elapsed %v suggests serial execution (serial would be ~%v)", elapsed, time.Duration(n)*delay)
	}
}

func TestRunAllRespectsMaxParallel(t *testing.T) {
	iso := &recordingIsolator{}
	r := Runner{Isolator: iso, MaxParallel: 2}

	var mu sync.Mutex
	inFlight, peak := 0, 0
	tasks := make([]Task, 6)
	for i := range tasks {
		tasks[i] = Task{ID: fmt.Sprintf("t%d", i), Run: func(ctx context.Context, dir string) (string, error) {
			mu.Lock()
			inFlight++
			if inFlight > peak {
				peak = inFlight
			}
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
			return "ok", nil
		}}
	}
	if _, err := r.RunAll(context.Background(), tasks); err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if peak > 2 {
		t.Fatalf("peak concurrency = %d, want <= 2", peak)
	}
}

func TestRunAllIsolatesEachTaskAndReleases(t *testing.T) {
	iso := &recordingIsolator{}
	r := Runner{Isolator: iso, MaxParallel: 4}

	var seenDirs []string
	var mu sync.Mutex
	tasks := []Task{
		{ID: "a", Run: func(ctx context.Context, dir string) (string, error) {
			mu.Lock()
			seenDirs = append(seenDirs, dir)
			mu.Unlock()
			return "", nil
		}},
		{ID: "b", Run: func(ctx context.Context, dir string) (string, error) {
			mu.Lock()
			seenDirs = append(seenDirs, dir)
			mu.Unlock()
			return "", nil
		}},
	}
	if _, err := r.RunAll(context.Background(), tasks); err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	sort.Strings(seenDirs)
	if len(seenDirs) != 2 || seenDirs[0] == seenDirs[1] {
		t.Fatalf("each task needs its own dir, got %v", seenDirs)
	}
	// every created worktree must be released
	sort.Strings(iso.created)
	sort.Strings(iso.released)
	if len(iso.released) != len(iso.created) {
		t.Fatalf("released %v, created %v", iso.released, iso.created)
	}
}

func TestRunAllTaskErrorIsIsolated(t *testing.T) {
	iso := &recordingIsolator{}
	r := Runner{Isolator: iso, MaxParallel: 4}

	boom := errors.New("boom")
	tasks := []Task{
		{ID: "ok", Run: func(ctx context.Context, dir string) (string, error) { return "fine", nil }},
		{ID: "bad", Run: func(ctx context.Context, dir string) (string, error) { return "", boom }},
	}
	got, err := r.RunAll(context.Background(), tasks)
	// One failing subagent must not fail the whole fan-out.
	if err != nil {
		t.Fatalf("RunAll should not fail on a single task error: %v", err)
	}
	if got[0].Err != nil {
		t.Errorf("healthy task got err: %v", got[0].Err)
	}
	if !errors.Is(got[1].Err, boom) {
		t.Errorf("failing task err = %v, want boom", got[1].Err)
	}
	// its worktree is still cleaned up
	if len(iso.released) != 2 {
		t.Errorf("released = %v, want both worktrees released", iso.released)
	}
}

func TestRunAllIsolationFailureReported(t *testing.T) {
	iso := &recordingIsolator{failOn: "bad"}
	r := Runner{Isolator: iso, MaxParallel: 4}

	ran := false
	tasks := []Task{
		{ID: "bad", Run: func(ctx context.Context, dir string) (string, error) {
			ran = true
			return "", nil
		}},
	}
	got, err := r.RunAll(context.Background(), tasks)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if ran {
		t.Error("task must not run when isolation fails")
	}
	if got[0].Err == nil || !strings.Contains(got[0].Err.Error(), "isolate") {
		t.Fatalf("err = %v, want an isolation error", got[0].Err)
	}
}

func TestRunAllCancellation(t *testing.T) {
	iso := &recordingIsolator{}
	r := Runner{Isolator: iso, MaxParallel: 2}

	ctx, cancel := context.WithCancel(t.Context())
	tasks := make([]Task, 4)
	for i := range tasks {
		tasks[i] = Task{ID: fmt.Sprintf("t%d", i), Run: func(ctx context.Context, dir string) (string, error) {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(2 * time.Second):
				return "should-not-finish", nil
			}
		}}
	}
	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	got, err := r.RunAll(ctx, tasks)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("cancellation not responsive: %v", time.Since(start))
	}
	for _, res := range got {
		if res.Output == "should-not-finish" {
			t.Errorf("task %q completed despite cancellation", res.ID)
		}
	}
}

func TestRunAllNoTasks(t *testing.T) {
	r := Runner{Isolator: &recordingIsolator{}}
	got, err := r.RunAll(context.Background(), nil)
	if err != nil {
		t.Fatalf("RunAll(nil): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("results = %v, want empty", got)
	}
}

func TestRunAllDefaultsMaxParallel(t *testing.T) {
	// MaxParallel unset must still run (and not deadlock on a zero-size gate).
	r := Runner{Isolator: &recordingIsolator{}}
	tasks := []Task{
		{ID: "a", Run: func(ctx context.Context, dir string) (string, error) { return "ok", nil }},
	}
	got, err := r.RunAll(context.Background(), tasks)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(got) != 1 || got[0].Output != "ok" {
		t.Fatalf("results = %+v", got)
	}
}

func TestRunAllNilIsolatorRunsInPlace(t *testing.T) {
	// With no Isolator, tasks run in the current directory (dir == "").
	r := Runner{}
	var gotDir string
	tasks := []Task{
		{ID: "a", Run: func(ctx context.Context, dir string) (string, error) {
			gotDir = dir
			return "ok", nil
		}},
	}
	if _, err := r.RunAll(context.Background(), tasks); err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if gotDir != "" {
		t.Fatalf("dir = %q, want empty (in-place)", gotDir)
	}
}
