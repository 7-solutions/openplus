package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Bash runs a shell command (sh -c) with merged stdout/stderr, streaming into
// the returned output. Cancellation (ctx) and a per-call timeout both kill the
// process. A non-zero exit is reported in the output (not as an error) so the
// model can react to it; only mechanism failures and cancellation error.
type Bash struct{}

func (Bash) Name() string { return "bash" }

func (Bash) Description() string {
	return "Run a shell command (sh -c) and return its merged stdout/stderr. " +
		"A non-zero exit code is included in the output. Supports pipes and " +
		"redirections. Optional cwd sets the working directory; " +
		"timeout_seconds caps the run (the caller's context can also cancel it)."
}

func (Bash) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "command":         {"type": "string", "description": "shell command to run"},
    "cwd":             {"type": "string", "description": "optional working directory"},
    "timeout_seconds": {"type": "integer", "description": "optional per-call timeout in seconds"}
  },
  "required": ["command"]
}`)
}

type bashInput struct {
	Command        string `json:"command"`
	Cwd            string `json:"cwd"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (Bash) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in bashInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bash: bad input: %w", err)
	}
	if strings.TrimSpace(in.Command) == "" {
		return "", errors.New("bash: command is required")
	}

	// Layer a per-call timeout onto the caller's context (shorter deadline wins).
	runCtx := ctx
	if in.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	// CommandContext kills the process when runCtx is cancelled/timed out.
	cmd := exec.CommandContext(runCtx, "sh", "-c", in.Command)
	if in.Cwd != "" {
		cmd.Dir = in.Cwd
	}
	// Run the command in its own process group so cancellation can kill the
	// whole tree — otherwise `sh -c "sleep 5"` leaves the `sleep` grandchild
	// holding the stdout pipe open and cmd.Run blocks until it exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		// Kill the entire process group, then the leader.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return cmd.Process.Kill()
	}
	// Force-close inherited pipes shortly after the kill so Run returns even if
	// a grandchild is slow to die.
	cmd.WaitDelay = 500 * time.Millisecond
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf // merge

	runErr := cmd.Run()
	out := buf.String()

	// Cancellation/timeout is a mechanism error the caller must see.
	if runCtx.Err() != nil {
		return out, runCtx.Err()
	}
	if runErr != nil {
		// Non-zero exit: surface as informative output, not an error.
		if exitErr := (&exec.ExitError{}); errors.As(runErr, &exitErr) {
			return fmt.Sprintf("%s\n[exit code %d]", out, exitErr.ExitCode()), nil
		}
		// Other failure (e.g. sh missing) — genuine error.
		return out, fmt.Errorf("bash: run: %w", runErr)
	}
	return out, nil
}
