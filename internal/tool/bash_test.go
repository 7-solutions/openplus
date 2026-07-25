package tool

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBashRunsCommand(t *testing.T) {
	out, err := (Bash{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"command": "echo hello",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("output = %q", out)
	}
}

func TestBashExitCodeIsOutputNotError(t *testing.T) {
	// Non-zero exit is informative, not a tool mechanism failure: the model
	// should see the output and the exit code, and Execute must NOT error.
	out, err := (Bash{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"command": "echo to-stderr 1>&2; exit 3",
	}))
	if err != nil {
		t.Fatalf("non-zero exit should not be an error: %v", err)
	}
	if !strings.Contains(out, "to-stderr") {
		t.Errorf("stderr not merged into output: %q", out)
	}
	if !strings.Contains(out, "exit") || !strings.Contains(out, "3") {
		t.Errorf("exit code not reported: %q", out)
	}
}

func TestBashCwd(t *testing.T) {
	dir := t.TempDir()
	out, err := (Bash{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"command": "pwd", "cwd": dir,
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(strings.TrimSpace(out), dir) {
		t.Fatalf("pwd output %q does not contain cwd %q", out, dir)
	}
}

func TestBashContextCancellationKills(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := (Bash{}).Execute(ctx, jsonInput(t, map[string]any{
		"command": "sleep 5",
	}))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error on cancelled ctx")
	}
	// Must return promptly after the deadline, not wait for sleep 5.
	if elapsed > 2*time.Second {
		t.Fatalf("cancellation not responsive: %v", elapsed)
	}
}

func TestBashPerCallTimeout(t *testing.T) {
	start := time.Now()
	_, err := (Bash{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"command": "sleep 5", "timeout_seconds": 1,
	}))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error on per-call timeout")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("timeout not enforced: %v", elapsed)
	}
}

func TestBashEmptyCommandErrors(t *testing.T) {
	_, err := (Bash{}).Execute(context.Background(), jsonInput(t, map[string]any{
		"command": "",
	}))
	if err == nil {
		t.Fatal("want error for empty command")
	}
}

// guard: ensure a context-cancelled error is recognizable by the caller.
func TestBashCtxErrorIsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	_, err := (Bash{}).Execute(ctx, jsonInput(t, map[string]any{"command": "sleep 5"}))
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want wraps context.Canceled", err)
	}
}
